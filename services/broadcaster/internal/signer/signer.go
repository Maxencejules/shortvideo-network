// Package signer is the broadcaster's development signing boundary (plan
// 7.2, owner answer Q1 "Both keys, separate signer (Recommended)"): the
// `svn-broadcaster signer` process, the only process that reads key files,
// and the client serve uses to reach it.
//
// The signer holds at most one ADMITTED_SERVICE key and one BROADCASTER
// key (devnet only). It listens on a Unix socket in a directory it owns,
// accepts a connection only when the socket's peer credentials report the
// configured serve uid, and answers one request per connection. For a sign
// request it checks, in order: the key name; the message type is
// registered by the application and in the key class's allowlist; the
// message decodes from its canonical encoding; the message's signer is
// the key's address and an EndLease has authority_end false
// (policy.CheckSignable); the chain id is its own; the gas is within the
// chain's bounds; the timeout height is not 0 and not above the height it
// read itself from its node plus K_max, and it refuses while that height
// is not fresh; and for a register or withdraw, the hosting intent
// verifies for the signer's own address and its key is in the operator's
// intent-key registry. Only then does it build the one-message outer
// transaction (SIGN_MODE_DIRECT, no fee, no memo), sign it, decode it again
// with the strict decoder, and return the bytes and the hash. It signs
// nothing else: no inner-signature domain, no arbitrary bytes. It keeps no
// state, so its death mid-request leaves no attempt and uses no sequence.
//
// Process boundary (plan 4.1, DR-11-35). Under policy.DistinctUIDs the
// signer refuses to run as root or with the serve uid; key files and their
// directory must be owned by its uid with no group or other bit; and the
// intent-key registry and its directory must be writable neither by its
// own uid nor by the serve uid (policy.CheckRegistryForServe). Under
// policy.SameUID (a launcher that is not root) the split separates key
// bytes in memory only: a compromised serve can read the key files. A
// same-uid run does not verify the boundary.
//
// These are development keys on a shortvideo-dev- chain id. Production
// custody (KMS, HSM, remote signer) is not available here and is not
// claimed.
package signer

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/netip"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/keys"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/logx"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// Bounds of the signer's configuration. The poll, freshness, timeout and
// K_max values are the hard bounds of plan section 11 (PROPOSED numbers,
// DR-11-10); the configured values come from the operator.
const (
	MinRPCTimeout     = time.Second
	MaxRPCTimeout     = 60 * time.Second
	MinPollInterval   = 100 * time.Millisecond
	MaxPollInterval   = 10 * time.Second
	MinFreshnessPolls = 2
	MaxFreshnessPolls = 20
	// MaxConnections bounds the connections served at once; a connection
	// beyond it is answered OVERLOADED and closed.
	MaxConnections = 16
	// startupFloorTries is how many times start reads governance before it
	// refuses. PROPOSED. The tries are one poll interval apart, so the wait
	// is at most (startupFloorTries-1) poll intervals: 400 ms to 40 s, and
	// 2 s at the PROPOSED poll interval of 500 ms. A try that is not a stale
	// read (another chain id, a refused response, a cancelled context) fails
	// at once.
	startupFloorTries = 5
	// maxSocketPath is the Unix socket path limit (sun_path, 108 bytes
	// with the terminating zero).
	maxSocketPath = 107
)

// KeyConfig names one key file and its class.
type KeyConfig struct {
	Name  string
	Class policy.Class
	Path  string
}

// Config is the signer's configuration. Every field is required except
// Logger and BeforeReply.
type Config struct {
	// ChainID is the chain the signer signs for; it must start with
	// shortvideo-dev- (development keys only, docs/protocol.md:136).
	ChainID string
	// RPCURL is the configured node's CometBFT RPC base URL; it must be
	// http on a loopback address. The signer reads /status (its own
	// timeout bound) and Query/Governance (the start-up role check).
	RPCURL     string
	RPCTimeout time.Duration
	// PollInterval is the least time between two /status reads;
	// FreshnessPolls intervals after the last successful read the height
	// is no longer fresh and signing stops.
	PollInterval   time.Duration
	FreshnessPolls int
	// MaxTimeoutOffset is K_max: the highest timeout height the signer
	// signs is its fresh height plus K_max (chain.MinTimeoutOffset to
	// chain.MaxTimeoutOffset).
	MaxTimeoutOffset uint64
	// SocketPath is the Unix socket; its directory must belong to the
	// signer's uid with no group or other write bit.
	SocketPath string
	// Boundary is the process boundary; ServeUID is the only peer uid
	// accepted on the socket.
	Boundary policy.Boundary
	ServeUID uint32
	// Keys are the key files: at most one per class.
	Keys []KeyConfig
	// RegistryPath is the operator's intent-key registry (public keys).
	RegistryPath string
	// Logger receives one redacted record per request; nil discards.
	Logger *slog.Logger
	// BeforeReply, when set, runs after a sign request is signed and
	// before the reply is written. The build-tagged fault-point build
	// sets it to the after-decode-before-reply point (plan 8.3); every
	// other build leaves it nil.
	BeforeReply func()
}

// validate checks the configuration for a process with effective uid
// euid, before any file is read.
func (c Config) validate(euid uint32) error {
	switch {
	case c.ChainID == "" || len(c.ChainID) > genesis.MaxChainIDLen:
		return fmt.Errorf("signer: chain id must be 1 to %d bytes", genesis.MaxChainIDLen)
	case !strings.HasPrefix(c.ChainID, genesis.DevnetChainIDPrefix):
		return fmt.Errorf("signer: development keys refuse a chain id without the %s prefix", genesis.DevnetChainIDPrefix)
	case c.RPCTimeout < MinRPCTimeout || c.RPCTimeout > MaxRPCTimeout:
		return fmt.Errorf("signer: rpc timeout must be %s to %s", MinRPCTimeout, MaxRPCTimeout)
	case c.PollInterval < MinPollInterval || c.PollInterval > MaxPollInterval:
		return fmt.Errorf("signer: poll interval must be %s to %s", MinPollInterval, MaxPollInterval)
	case c.FreshnessPolls < MinFreshnessPolls || c.FreshnessPolls > MaxFreshnessPolls:
		return fmt.Errorf("signer: freshness must be %d to %d poll intervals", MinFreshnessPolls, MaxFreshnessPolls)
	case c.MaxTimeoutOffset < chain.MinTimeoutOffset || c.MaxTimeoutOffset > chain.MaxTimeoutOffset:
		return fmt.Errorf("signer: K_max must be %d to %d blocks", chain.MinTimeoutOffset, chain.MaxTimeoutOffset)
	case !filepath.IsAbs(c.SocketPath) || len(c.SocketPath) > maxSocketPath || filepath.Clean(c.SocketPath) != c.SocketPath:
		return fmt.Errorf("signer: socket path must be a clean absolute path of at most %d bytes", maxSocketPath)
	case !filepath.IsAbs(c.RegistryPath):
		return errors.New("signer: intent-key registry path must be absolute")
	case len(c.Keys) == 0 || len(c.Keys) > 2:
		return errors.New("signer: configure one or two keys (one per class)")
	}
	if err := loopbackHTTP(c.RPCURL); err != nil {
		return err
	}
	switch c.Boundary {
	case policy.DistinctUIDs:
		if euid == 0 {
			return errors.New("signer: distinct uids are configured and the signer runs as root")
		}
		if c.ServeUID == 0 || c.ServeUID == euid {
			return errors.New("signer: distinct uids are configured and the serve uid is root or the signer's own uid")
		}
	case policy.SameUID:
		if c.ServeUID != euid {
			return errors.New("signer: the same-uid fallback needs the serve uid to equal the signer's uid")
		}
	default:
		return errors.New("signer: unknown process boundary")
	}
	for _, k := range c.Keys {
		if _, ok := policy.RequiredRole(k.Class); !ok || k.Name == "" || !filepath.IsAbs(k.Path) {
			return fmt.Errorf("signer: key %q needs a name, class SERVICE or BROADCASTER, and an absolute path", k.Name)
		}
	}
	return nil
}

// loopbackHTTP refuses an RPC URL that is not http on a loopback IP.
func loopbackHTTP(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "http" {
		return errors.New("signer: rpc URL must be http on a loopback address")
	}
	addr, err := netip.ParseAddr(u.Hostname())
	if err != nil || !addr.IsLoopback() {
		return errors.New("signer: rpc URL must name a loopback IP address")
	}
	return nil
}

// loadedKey is one key and its public identity.
type loadedKey struct {
	name    string
	class   policy.Class
	key     keys.Key
	address []byte
}

// Server is a running signer.
type Server struct {
	cfg      Config
	codec    *chain.Codec
	enc      app.Encoding
	keys     map[string]loadedKey
	registry *policy.Registry
	heights  *heightSource
	listener *net.UnixListener
	log      *slog.Logger
	// peerUID is the only peer uid served (cfg.ServeUID).
	peerUID atomic.Uint32
	slots   chan struct{}
	wg      sync.WaitGroup
	once    sync.Once
}

// New validates cfg, loads the keys and the registry, checks every key's
// role against Query/Governance of the configured node, and listens on the
// socket. It refuses to start on any failure.
func New(ctx context.Context, cfg Config) (*Server, error) {
	rpc, err := chain.NewHTTP(cfg.RPCURL, cfg.RPCTimeout)
	if err != nil {
		return nil, err
	}
	return start(ctx, cfg, rpc, uint32(os.Geteuid()))
}

// start is New over a given RPC client and effective uid.
func start(ctx context.Context, cfg Config, rpc chain.RPC, euid uint32) (*Server, error) {
	if err := cfg.validate(euid); err != nil {
		return nil, err
	}
	if err := checkSocketDir(filepath.Dir(cfg.SocketPath), euid); err != nil {
		return nil, err
	}
	enc, err := app.MakeEncoding()
	if err != nil {
		return nil, fmt.Errorf("signer: encoding: %w", err)
	}
	codec, err := chain.NewCodec(cfg.ChainID)
	if err != nil {
		return nil, err
	}
	client, err := chain.New(rpc, codec)
	if err != nil {
		return nil, err
	}
	s := &Server{
		cfg: cfg, codec: codec, enc: enc, keys: map[string]loadedKey{},
		slots: make(chan struct{}, MaxConnections), log: cfg.Logger,
		heights: newHeightSource(client, cfg.PollInterval, time.Duration(cfg.FreshnessPolls)*cfg.PollInterval),
	}
	s.peerUID.Store(cfg.ServeUID)
	if s.log == nil {
		s.log = logx.Discard()
	}
	var ids []policy.KeyIdentity
	for _, kc := range cfg.Keys {
		k, err := keys.Load(kc.Path, euid)
		if err != nil {
			return nil, fmt.Errorf("signer: key %q: %w", kc.Name, err)
		}
		if _, dup := s.keys[kc.Name]; dup {
			return nil, fmt.Errorf("signer: key name %q is used twice", kc.Name)
		}
		s.keys[kc.Name] = loadedKey{name: kc.Name, class: kc.Class, key: k, address: k.Address()}
		ids = append(ids, policy.KeyIdentity{Name: kc.Name, Class: kc.Class, Address: k.Address()})
	}
	if s.registry, err = policy.LoadRegistry(cfg.RegistryPath, cfg.Boundary); err != nil {
		return nil, err
	}
	if cfg.Boundary == policy.DistinctUIDs {
		if err := policy.CheckRegistryForServe(cfg.RegistryPath, cfg.ServeUID); err != nil {
			return nil, err
		}
	}
	// The governance read must be answered at least at the status's read
	// floor (chain.Status.AppFloor): the node may report a block its
	// application is still executing, so an answer one block below the
	// status height is current. An answer below the floor is retried, up
	// to startupFloorTries, and then refused with the floor and the bound.
	view, err := readStartupGovernance(ctx, client, cfg.PollInterval)
	if err != nil {
		return nil, err
	}
	if err := policy.CheckKeyRoles(ids, view); err != nil {
		return nil, err
	}
	if s.listener, err = listen(cfg.SocketPath, euid); err != nil {
		return nil, err
	}
	s.log.Info("signer listening", "boundary", cfg.Boundary.String(), "signer_uid", int64(euid),
		"serve_uid", int64(cfg.ServeUID), "keys", int64(len(s.keys)), "governance_height", view.Height)
	return s, nil
}

// readStartupGovernance reads Query/Governance at the read floor of the
// latest status (chain.Status.AppFloor). It re-reads status and retries a
// stale answer until startupFloorTries have failed or ctx is done. The
// error names the floor, the status height and how long it waited.
func readStartupGovernance(ctx context.Context, client *chain.Client, poll time.Duration) (policy.GovernanceView, error) {
	var last error
	var floor, height int64
	for try := 1; try <= startupFloorTries; try++ {
		if try > 1 {
			timer := time.NewTimer(poll)
			select {
			case <-ctx.Done():
				timer.Stop()
				return policy.GovernanceView{}, fmt.Errorf("signer: start-up governance read at least at height %d (status height %d minus one) stopped after %d of %d tries: %w",
					floor, height, try-1, startupFloorTries, ctx.Err())
			case <-timer.C:
			}
		}
		st, err := client.Status(ctx)
		if err != nil {
			return policy.GovernanceView{}, fmt.Errorf("signer: read node status: %w", err)
		}
		floor, height = st.AppFloor(), st.Height
		view, err := policy.ReadGovernance(ctx, client, floor)
		if err == nil {
			return view, nil
		}
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return policy.GovernanceView{}, fmt.Errorf("signer: start-up governance read at least at height %d (status height %d minus one): %w",
				floor, height, err)
		}
		// A stale answer is below the floor. An answer without a height is
		// the same wait at height 1, where the floor is 1 and the
		// application may not have committed it yet (chain.ErrResponse).
		var stale *chain.StaleError
		if !errors.As(err, &stale) && !errors.Is(err, chain.ErrResponse) {
			return policy.GovernanceView{}, fmt.Errorf("signer: start-up governance read at least at height %d (status height %d minus one): %w",
				floor, height, err)
		}
		last = err
	}
	waited := time.Duration(startupFloorTries-1) * poll
	return policy.GovernanceView{}, fmt.Errorf("signer: start-up governance read at least at height %d (status height %d minus one) was not answered in %d tries (%s): %w",
		floor, height, startupFloorTries, waited, last)
}

// checkSocketDir refuses a socket directory that is not a real directory
// owned by euid, or that has a group or other write bit (plan 7.2: a
// directory owned by the signer uid, mode 0711).
func checkSocketDir(dir string, euid uint32) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("signer: socket directory: %w", err)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	switch {
	case info.Mode()&fs.ModeSymlink != 0 || !info.IsDir():
		return errors.New("signer: socket directory is not a real directory")
	case !ok || st.Uid != euid:
		return errors.New("signer: socket directory is not owned by the signer uid")
	case info.Mode().Perm()&0o022 != 0:
		return fmt.Errorf("signer: socket directory mode %04o has a group or other write bit", info.Mode().Perm())
	}
	return nil
}

// listen binds the socket. A stale socket of the signer's own uid (left by
// a killed signer) is removed first; any other file at the path is
// refused. The socket is mode 0666: the peer uid check is the access rule.
func listen(path string, euid uint32) (*net.UnixListener, error) {
	if info, err := os.Lstat(path); err == nil {
		st, ok := info.Sys().(*syscall.Stat_t)
		if info.Mode()&fs.ModeSocket == 0 || !ok || st.Uid != euid {
			return nil, errors.New("signer: a file that is not the signer's socket exists at the socket path")
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("signer: remove stale socket: %w", err)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("signer: socket path: %w", err)
	}
	l, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("signer: listen: %w", err)
	}
	l.SetUnlinkOnClose(true)
	if err := os.Chmod(path, 0o666); err != nil {
		_ = l.Close()
		return nil, fmt.Errorf("signer: socket mode: %w", err)
	}
	return l, nil
}

// Serve accepts connections until ctx ends or Close is called, then waits
// for the requests in progress.
func (s *Server) Serve(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() { _ = s.Close() })
	defer stop()
	for {
		conn, err := s.listener.AcceptUnix()
		if err != nil {
			s.wg.Wait()
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			return fmt.Errorf("signer: accept: %w", err)
		}
		select {
		case s.slots <- struct{}{}:
			s.wg.Add(1)
			go func() {
				defer s.wg.Done()
				defer func() { <-s.slots }()
				s.handle(ctx, conn)
			}()
		default:
			_ = conn.SetDeadline(time.Now().Add(time.Second))
			_ = writeFrame(conn, response{Refusal: &refusalBody{Code: CodeOverloaded}}, MaxResponseBytes)
			_ = conn.Close()
		}
	}
}

// Close stops accepting connections and removes the socket.
func (s *Server) Close() error {
	var err error
	s.once.Do(func() { err = s.listener.Close() })
	return err
}

// requestTimeout bounds one connection: a status read and the signing.
func (s *Server) requestTimeout() time.Duration { return s.cfg.RPCTimeout + 5*time.Second }

// handle serves one connection: the peer uid check, one request, one
// response.
func (s *Server) handle(ctx context.Context, conn *net.UnixConn) {
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(s.requestTimeout()))
	uid, err := peerUID(conn)
	if err != nil || uid != s.peerUID.Load() {
		s.log.Warn("signer refused a peer", "peer_uid", int64(uid), "code", string(CodePeerRefused))
		_ = writeFrame(conn, response{Refusal: &refusalBody{Code: CodePeerRefused}}, MaxResponseBytes)
		return
	}
	var req request
	if err := readFrame(conn, &req, MaxRequestBytes); err != nil || req.Version != ProtocolVersion {
		_ = writeFrame(conn, response{Refusal: &refusalBody{Code: CodeMalformed}}, MaxResponseBytes)
		return
	}
	var resp response
	switch req.Op {
	case OpKeys:
		resp = response{Keys: s.keyBodies()}
	case OpSign:
		raw, hash, err := s.sign(ctx, req)
		if err != nil {
			code, detail := policy.CodeOf(err), ""
			if code == "" {
				code = CodeInternal
			} else {
				detail = err.Error()
			}
			s.log.Info("signer refused", "signer", req.KeyName, "type_url", req.TypeURL, "code", string(code))
			resp = response{Refusal: &refusalBody{Code: code, Detail: detail}}
			break
		}
		s.log.Info("signer signed", "signer", req.KeyName, "type_url", req.TypeURL, "tx_hash", hex.EncodeToString(hash[:]),
			"sequence", req.Sequence, "timeout_height", req.TimeoutHeight)
		if s.cfg.BeforeReply != nil {
			s.cfg.BeforeReply()
		}
		resp = response{TxBytes: raw, TxHash: hash[:]}
	default:
		resp = response{Refusal: &refusalBody{Code: CodeMalformed}}
	}
	_ = writeFrame(conn, resp, MaxResponseBytes)
}

// peerUID returns the uid of the process at the other end of conn
// (SO_PEERCRED).
func peerUID(conn *net.UnixConn) (uint32, error) {
	raw, err := conn.SyscallConn()
	if err != nil {
		return 0, err
	}
	var cred *syscall.Ucred
	var credErr error
	if err := raw.Control(func(fd uintptr) {
		cred, credErr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, err
	}
	if credErr != nil {
		return 0, credErr
	}
	return cred.Uid, nil
}

// keyBodies returns every key's public identity.
func (s *Server) keyBodies() []keyBody {
	out := make([]keyBody, 0, len(s.keys))
	for _, kc := range s.cfg.Keys {
		k := s.keys[kc.Name]
		out = append(out, keyBody{Name: k.name, Class: k.class, Algorithm: k.key.Algorithm(),
			Address: bytes.Clone(k.address), PublicKey: bytes.Clone(k.key.PubKey().Bytes())})
	}
	return out
}

// sign runs the checks of the package comment and signs. Every refusal is
// a *policy.Refusal.
func (s *Server) sign(ctx context.Context, req request) ([]byte, [32]byte, error) {
	var none [32]byte
	k, ok := s.keys[req.KeyName]
	if !ok {
		return nil, none, &policy.Refusal{Code: CodeUnknownKey, Detail: "no key with this name"}
	}
	msg, err := s.decodeMessage(k.class, req.TypeURL, req.Value)
	if err != nil {
		return nil, none, err
	}
	if err := policy.CheckSignable(k.class, k.address, msg); err != nil {
		return nil, none, err
	}
	if req.ChainID != s.cfg.ChainID {
		return nil, none, &policy.Refusal{Code: policy.CodeChainID, Detail: "request chain id is not the signer's"}
	}
	if req.Gas < genesis.MinGasPerTx || req.Gas > genesis.MaxGasPerTx {
		return nil, none, &policy.Refusal{Code: policy.CodeGasBounds, Detail: fmt.Sprintf("gas must be %d to %d", genesis.MinGasPerTx, genesis.MaxGasPerTx)}
	}
	if req.TimeoutHeight == 0 {
		return nil, none, &policy.Refusal{Code: policy.CodeTimeoutZero, Detail: "every transaction carries a timeout height"}
	}
	height, fresh := s.heights.fresh(ctx)
	if !fresh {
		return nil, none, &policy.Refusal{Code: policy.CodeNoFreshHeight, Detail: "the signer has no fresh height from its node"}
	}
	if req.TimeoutHeight > uint64(height)+s.cfg.MaxTimeoutOffset {
		return nil, none, &policy.Refusal{Code: policy.CodeTimeoutTooFar, Detail: fmt.Sprintf("timeout height above the signer's height plus %d", s.cfg.MaxTimeoutOffset)}
	}
	switch msg.(type) {
	case *contentv1.MsgRegisterContent, *contentv1.MsgWithdrawContent:
		if err := policy.CheckIntentRegistered(msg, k.address, s.registry); err != nil {
			return nil, none, err
		}
	}
	unsigned, err := s.codec.Prepare(msg, k.key.PubKey(), chain.TxInput{
		AccountNumber: req.AccountNumber, Sequence: req.Sequence, Gas: req.Gas, TimeoutHeight: req.TimeoutHeight,
	})
	if err != nil {
		return nil, none, fmt.Errorf("signer: prepare: %w", err)
	}
	signature, err := k.key.SDK().Sign(unsigned.SignBytes())
	if err != nil {
		return nil, none, fmt.Errorf("signer: sign: %w", err)
	}
	raw, hash, err := unsigned.Assemble(signature)
	if err != nil {
		return nil, none, fmt.Errorf("signer: assemble: %w", err)
	}
	if hash != sha256.Sum256(raw) {
		return nil, none, errors.New("signer: transaction hash mismatch")
	}
	return raw, hash, nil
}

// hasUnknown reports whether m or any message it holds carries unknown
// fields (protobuf keeps them and would encode them again).
func hasUnknown(m protoreflect.Message) bool {
	if len(m.GetUnknown()) > 0 {
		return true
	}
	found := false
	m.Range(func(fd protoreflect.FieldDescriptor, v protoreflect.Value) bool {
		switch {
		case fd.IsList() && fd.Kind() == protoreflect.MessageKind:
			list := v.List()
			for i := 0; i < list.Len() && !found; i++ {
				found = hasUnknown(list.Get(i).Message())
			}
		case fd.IsMap() && fd.MapValue().Kind() == protoreflect.MessageKind:
			v.Map().Range(func(_ protoreflect.MapKey, mv protoreflect.Value) bool {
				found = hasUnknown(mv.Message())
				return !found
			})
		case !fd.IsList() && !fd.IsMap() && fd.Kind() == protoreflect.MessageKind:
			found = hasUnknown(v.Message())
		}
		return !found
	})
	return found
}

// decodeMessage resolves typeURL with the application's interface
// registry, refuses a type outside class's allowlist, and decodes value,
// which must be the message's deterministic encoding (no unknown field, no
// duplicate, no other field order).
func (s *Server) decodeMessage(class policy.Class, typeURL string, value []byte) (sdk.Msg, error) {
	if len(value) == 0 || uint64(len(value)) > genesis.MaxTxBytes {
		return nil, &policy.Refusal{Code: policy.CodeInvalid, Detail: fmt.Sprintf("message must be 1 to %d bytes", genesis.MaxTxBytes)}
	}
	resolved, err := s.enc.InterfaceRegistry.Resolve(typeURL)
	if err != nil {
		return nil, &policy.Refusal{Code: policy.CodeTypeNotAllowed, Detail: "message type is not registered by the application"}
	}
	if !policy.Allowed(class, typeURL) {
		return nil, &policy.Refusal{Code: policy.CodeTypeNotAllowed, Detail: fmt.Sprintf("class %s may not sign %s", class, typeURL)}
	}
	m, ok := resolved.(proto.Message)
	if !ok {
		return nil, &policy.Refusal{Code: policy.CodeTypeNotAllowed, Detail: "message type is not a protocol message"}
	}
	if err := proto.Unmarshal(value, m); err != nil {
		return nil, &policy.Refusal{Code: policy.CodeInvalid, Detail: "message does not decode"}
	}
	if hasUnknown(m.ProtoReflect()) {
		return nil, &policy.Refusal{Code: policy.CodeInvalid, Detail: "message carries an unknown field"}
	}
	canonical, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil || !bytes.Equal(canonical, value) {
		return nil, &policy.Refusal{Code: policy.CodeInvalid, Detail: "message is not in its deterministic encoding"}
	}
	msg, ok := m.(sdk.Msg)
	if !ok {
		return nil, &policy.Refusal{Code: policy.CodeTypeNotAllowed, Detail: "message type is not an SDK message"}
	}
	return msg, nil
}
