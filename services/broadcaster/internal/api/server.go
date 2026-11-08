// Package api is the broadcaster's caller API (goal 11 plan sections 5.2
// to 5.8): HTTP/1.1 JSON over TLS 1.3 with a required client certificate,
// on a loopback address only. The caller's principal is the certificate's
// single URI SAN svn://principal/<id> (DR-11-32, reversible).
//
//	POST /v1/intents                     validate, then enqueue in one database transaction
//	GET  /v1/intents/{intent_id}         the status view of the caller's own intent
//	GET  /v1/intents                     the caller's own intents, a page at a time
//	POST /v1/intents/{intent_id}/cancel  only while nothing was ever signed
//	GET  /v1/signers                     public signer addresses and keys per class
//	GET  /v1/health                      chain id, observer and stall state, database reachability
//
// New is the one constructor: the svn-broadcaster binary and the tests
// build the server with it and serve it with Serve. The API never reports
// COMMITTED without the canonical commitment evidence the outbox stored
// from a committed result read from the chain; a broadcast acknowledgement
// is never a commit. Another caller's intent answers 404, like an unknown
// one, and a content id another caller holds gets the one uniform
// CONTENT_ID_UNAVAILABLE answer (DR-11-34). Answers carry digests of the
// stored request and authorization context, never the raw signatures, the
// hosting intent or the context itself (owner answer Q4).
package api

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"os"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/config/secretfile"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/logx"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// Transport bounds. These are implementation limits of the HTTP server,
// not policy numbers.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 15 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 60 * time.Second
	maxHeaderBytes    = 16 << 10
	// requestTimeout bounds the work of one request, chain reads included.
	requestTimeout = 25 * time.Second
	// maxInFlight bounds the requests handled at once; a request beyond it
	// is answered 503 at once.
	maxInFlight = 64
	// maxCertFileBytes bounds a certificate or CA file.
	maxCertFileBytes = 64 << 10
	// shutdownTimeout bounds the graceful shutdown when Serve's context
	// ends.
	shutdownTimeout = 5 * time.Second
)

// Config is the API's configuration (internal/config builds it from the
// operator's file; tests build it directly).
type Config struct {
	// ChainID is the configured chain id.
	ChainID string
	// ListenAddress is a loopback IP literal and a nonzero port; Serve also
	// refuses a listener on any other address.
	ListenAddress string
	// TLSCertFile and TLSKeyFile are the server certificate chain and key
	// (PEM); ClientCAFile holds the CA certificates of caller certificates.
	TLSCertFile  string
	TLSKeyFile   string
	ClientCAFile string
	// Callers are the caller principals, their kinds and provider bindings.
	Callers []policy.Caller
	// MaxBodyBytes caps a POST body (1,024 to 65,536).
	MaxBodyBytes int
	// MaxAuthContextBytes caps an authorization context (256 to 8,192).
	MaxAuthContextBytes int
	// PageSize is the largest page GET /v1/intents returns (1 to 100).
	PageSize int
	// MinLifetime and MaxLifetime bound expires_in_seconds.
	MinLifetime time.Duration
	MaxLifetime time.Duration
	// RelayLocalExpiry bounds the lifetime of a RENEW or END relay, whose
	// provider signature never expires on chain (owner answer Q4).
	RelayLocalExpiry time.Duration
	// ForwardWindow bounds a forwarded transaction's timeout height above
	// the observed height, in blocks.
	ForwardWindow uint64
	// DeadlinePages is the Query/Deadlines page budget of an accept's
	// pre-check.
	DeadlinePages int
	// ObserverFreshness is the age at which the observer's last record is
	// stale (poll interval times freshness polls).
	ObserverFreshness time.Duration
}

// SignerKey is the public identity of one signer key (from the signer
// process's key list): what the API publishes and pins to an intent.
type SignerKey struct {
	Class     policy.Class
	Address   []byte
	PublicKey []byte
	Algorithm string
}

// Store is the outbox the API reads and writes (*store.Store).
type Store interface {
	EnqueueHTTP(ctx context.Context, r store.EnqueueRequest) (store.EnqueueResult, error)
	GetIntent(ctx context.Context, callerPrincipal, intentID string) (store.Intent, error)
	ListIntents(ctx context.Context, callerPrincipal string, f store.IntentFilter) ([]store.Intent, error)
	Cancel(ctx context.Context, callerPrincipal, intentID string) error
	ContentOwnerFor(ctx context.Context, callerPrincipal, contentID string) (store.ContentOwner, error)
	CallerScheduledOffer(ctx context.Context, callerPrincipal, offerID string) (bool, error)
	ReadChainHealth(ctx context.Context) (store.ChainHealth, error)
	OpenStallEpisode(ctx context.Context) (store.StallEpisode, error)
	// IntentByIdempotencyKey returns the caller's intent under an
	// idempotency key, or store.ErrNotFound. The API reads it before the
	// policy checks, so a retry of an accepted request gets its intent
	// back even when the chain state the checks read has changed since
	// (for example after the intent committed).
	IntentByIdempotencyKey(ctx context.Context, callerPrincipal, idempotencyKey string) (store.Intent, error)
}

var _ Store = (*store.Store)(nil)

// ChainReader is the chain state the API reads from the one trusted node
// (*chain.Client): the latest header, the content record, and the reads of
// the provider pre-check and the forward check.
type ChainReader interface {
	Status(ctx context.Context) (chain.Status, error)
	Content(ctx context.Context, contentID string, version uint64, minHeight int64) (*contentv1.QueryContentResponse, bool, int64, error)
	policy.ForwardReader
	policy.GovernanceReader
}

// Deps are the API's collaborators.
type Deps struct {
	Store Store
	Chain ChainReader
	// Codec is the chain codec of Config.ChainID (forward checks, bech32).
	Codec *chain.Codec
	// Registry is the operator's intent-key registry.
	Registry *policy.Registry
	// Signers are the signer process's keys: one SERVICE and one
	// BROADCASTER key (owner answer Q1).
	Signers []SignerKey
	// Logger receives redacted records; nil discards.
	Logger *slog.Logger
}

// Server is the caller API.
type Server struct {
	cfg      Config
	deps     Deps
	log      *slog.Logger
	tls      *tls.Config
	callers  map[string]policy.Caller
	signers  map[policy.Class]SignerKey
	addr     netip.AddrPort
	inFlight chan struct{}
	handler  http.Handler
}

// New checks the configuration and the collaborators, loads the TLS
// material, and returns the server. Development principals refuse a chain
// id without the shortvideo-dev- prefix (plan 5.6).
func New(cfg Config, deps Deps) (*Server, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	if deps.Store == nil || deps.Chain == nil || deps.Codec == nil || deps.Registry == nil {
		return nil, errors.New("api: store, chain reader, codec and registry are required")
	}
	if deps.Codec.ChainID() != cfg.ChainID {
		return nil, errors.New("api: codec is for another chain id")
	}
	callers := map[string]policy.Caller{}
	for _, c := range cfg.Callers {
		if !msgkit.ValidID(c.Principal) {
			return nil, errors.New("api: caller principal outside the id pattern")
		}
		if _, dup := callers[c.Principal]; dup {
			return nil, errors.New("api: caller principal listed twice")
		}
		for _, k := range c.Kinds {
			if _, err := policy.SpecOf(k); err != nil {
				return nil, errors.New("api: caller kind is not routed")
			}
		}
		if c.BoundProviderID != "" && !msgkit.ValidID(c.BoundProviderID) {
			return nil, errors.New("api: caller provider binding outside the id pattern")
		}
		callers[c.Principal] = c
	}
	signers, err := checkSigners(deps.Signers)
	if err != nil {
		return nil, err
	}
	tlsCfg, err := loadTLS(cfg)
	if err != nil {
		return nil, err
	}
	addr, err := netip.ParseAddrPort(cfg.ListenAddress)
	if err != nil {
		return nil, errors.New("api: listen address is not an IP literal and a port")
	}
	logger := deps.Logger
	if logger == nil {
		logger = logx.Discard()
	}
	s := &Server{
		cfg: cfg, deps: deps, log: logger, tls: tlsCfg, callers: callers, signers: signers, addr: addr,
		inFlight: make(chan struct{}, maxInFlight),
	}
	s.handler = s.routes()
	return s, nil
}

func (c Config) validate() error {
	switch {
	case c.ChainID == "" || len(c.ChainID) > genesis.MaxChainIDLen:
		return errors.New("api: chain id must be 1 to 50 bytes")
	case len(c.Callers) == 0:
		return errors.New("api: at least one caller is required")
	case !strings.HasPrefix(c.ChainID, genesis.DevnetChainIDPrefix):
		return fmt.Errorf("api: development principals refuse a chain id without the %s prefix", genesis.DevnetChainIDPrefix)
	case c.MaxBodyBytes < 1024 || uint64(c.MaxBodyBytes) > genesis.MaxTxBytes:
		return errors.New("api: body cap outside [1024, 65536]")
	case c.MaxAuthContextBytes < 256 || c.MaxAuthContextBytes > request.MaxAuthContextBytes:
		return errors.New("api: auth context cap outside [256, 8192]")
	case c.PageSize < 1 || c.PageSize > 100:
		return errors.New("api: page size outside [1, 100]")
	case c.MinLifetime < 5*time.Second || c.MaxLifetime > 7*24*time.Hour || c.MinLifetime > c.MaxLifetime:
		return errors.New("api: lifetime bounds outside [5s, 7d] or min above max")
	case c.RelayLocalExpiry < time.Minute || c.RelayLocalExpiry > 24*time.Hour:
		return errors.New("api: relay local expiry outside [60s, 86400s]")
	case c.ForwardWindow < policy.MinForwardWindow || c.ForwardWindow > policy.MaxForwardWindow:
		return errors.New("api: forward window outside its hard bounds")
	case c.DeadlinePages < policy.MinDeadlinePages || c.DeadlinePages > policy.MaxDeadlinePages:
		return errors.New("api: deadline page budget outside its hard bounds")
	case c.ObserverFreshness <= 0:
		return errors.New("api: observer freshness must be positive")
	}
	ap, err := netip.ParseAddrPort(c.ListenAddress)
	if err != nil || !ap.Addr().IsLoopback() || ap.Addr().Zone() != "" {
		return errors.New("api: listen address must be a loopback IP literal and a port")
	}
	return nil
}

// checkSigners requires one SERVICE and one BROADCASTER key with 20-byte
// addresses that differ.
func checkSigners(keys []SignerKey) (map[policy.Class]SignerKey, error) {
	out := map[policy.Class]SignerKey{}
	for _, k := range keys {
		if _, ok := policy.RequiredRole(k.Class); !ok {
			return nil, errors.New("api: signer key of an unknown class")
		}
		if len(k.Address) != genesis.AccountBytes || len(k.PublicKey) == 0 || len(k.PublicKey) > 128 || k.Algorithm == "" {
			return nil, errors.New("api: signer key without an address, public key or algorithm")
		}
		if _, dup := out[k.Class]; dup {
			return nil, errors.New("api: two signer keys of one class")
		}
		out[k.Class] = k
	}
	svc, okS := out[policy.ClassService]
	bc, okB := out[policy.ClassBroadcaster]
	if !okS || !okB {
		return nil, errors.New("api: one SERVICE and one BROADCASTER key are required")
	}
	if bytes.Equal(svc.Address, bc.Address) {
		return nil, errors.New("api: the SERVICE and BROADCASTER keys have one address")
	}
	return out, nil
}

// loadTLS reads the server certificate and key and the client CA bundle
// and returns the TLS 1.3 configuration that requires and verifies a
// client certificate. The key file must pass the secret-file rules.
func loadTLS(cfg Config) (*tls.Config, error) {
	certPEM, err := readPublicFile(cfg.TLSCertFile)
	if err != nil {
		return nil, fmt.Errorf("api: server certificate: %w", err)
	}
	keyPEM, err := secretfile.Read(cfg.TLSKeyFile, maxCertFileBytes)
	if err != nil {
		return nil, fmt.Errorf("api: server certificate key: %w", err)
	}
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, errors.New("api: server certificate and key do not form a pair")
	}
	caPEM, err := readPublicFile(cfg.ClientCAFile)
	if err != nil {
		return nil, fmt.Errorf("api: client CA: %w", err)
	}
	pool := x509.NewCertPool()
	var n int
	for rest := caPEM; ; {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		if block.Type != "CERTIFICATE" {
			return nil, errors.New("api: client CA file holds a block that is not a certificate")
		}
		ca, err := x509.ParseCertificate(block.Bytes)
		if err != nil || !ca.IsCA {
			return nil, errors.New("api: client CA file holds a certificate that is not a CA")
		}
		pool.AddCert(ca)
		n++
	}
	if n == 0 {
		return nil, errors.New("api: client CA file holds no certificate")
	}
	return &tls.Config{
		MinVersion:   tls.VersionTLS13,
		Certificates: []tls.Certificate{cert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    pool,
		NextProtos:   []string{"http/1.1"},
	}, nil
}

// readPublicFile reads a certificate file: a regular file, at most
// maxCertFileBytes.
func readPublicFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxCertFileBytes {
		return nil, errors.New("not a regular file of 1 byte to 64 KiB")
	}
	return os.ReadFile(path)
}

// Handler returns the request handler (without TLS): the routes behind
// the caller authentication, the in-flight bound and the request timeout.
func (s *Server) Handler() http.Handler { return s.handler }

// TLSConfig returns a copy of the TLS configuration Serve uses.
func (s *Server) TLSConfig() *tls.Config { return s.tls.Clone() }

// Serve serves the API over TLS on ln until ctx ends, then shuts down
// gracefully within a bounded time. A listener on a non-loopback address
// is refused.
func (s *Server) Serve(ctx context.Context, ln net.Listener) error {
	tcp, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		return errors.New("api: listener is not a TCP listener")
	}
	ip, ok := netip.AddrFromSlice(tcp.IP)
	if !ok || !ip.Unmap().IsLoopback() {
		return errors.New("api: listener is not on a loopback address")
	}
	srv := &http.Server{
		Handler:           s.handler,
		TLSConfig:         s.tls.Clone(),
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(s.log.Handler(), slog.LevelWarn),
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(tls.NewListener(ln, srv.TLSConfig)) }()
	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	shutCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	shutErr := srv.Shutdown(shutCtx)
	if err := <-errc; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return errors.Join(err, shutErr)
	}
	return shutErr
}

// ListenAndServe binds Config.ListenAddress and runs Serve.
func (s *Server) ListenAndServe(ctx context.Context) error {
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.addr.String())
	if err != nil {
		return fmt.Errorf("api: listen: %w", err)
	}
	return s.Serve(ctx, ln)
}
