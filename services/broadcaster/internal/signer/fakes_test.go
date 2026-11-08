package signer

// Test fakes and fixtures. fakeNode is an in-process chain.RPC for unit
// tests only: it answers /status and Query/Governance from memory so the
// signer's own height bound and start-up role check run through the real
// internal/chain client. It is a fake; it is never end-to-end evidence.

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtbytes "github.com/cometbft/cometbft/libs/bytes"
	"github.com/cometbft/cometbft/p2p"
	rpcclient "github.com/cometbft/cometbft/rpc/client"
	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	cmttypes "github.com/cometbft/cometbft/types"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/logx"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

const testChainID = "shortvideo-dev-0011223344556677"

// fakeNode serves /status and Query/Governance.
type fakeNode struct {
	mu         sync.Mutex
	chainID    string
	height     int64
	statusErr  error
	submitters []*typesv1.Submitter
	seats      []*authorityv1.DevnetTestSeat
	statuses   int
	// queryLag is how many blocks below the /status height an ABCI query
	// answers: 1 is CometBFT while the reported block executes
	// (chain.Status.AppFloor).
	queryLag int64
}

func (n *fakeNode) setHeight(h int64) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.height = h
}

func (n *fakeNode) setStatusErr(err error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.statusErr = err
}

func (n *fakeNode) statusCalls() int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.statuses
}

func (n *fakeNode) Status(context.Context) (*coretypes.ResultStatus, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.statuses++
	if n.statusErr != nil {
		return nil, n.statusErr
	}
	return &coretypes.ResultStatus{NodeInfo: p2p.DefaultNodeInfo{Network: n.chainID},
		SyncInfo: coretypes.SyncInfo{LatestBlockHeight: n.height, LatestBlockTime: time.Unix(1_900_000_000, 0).UTC()}}, nil
}

func (n *fakeNode) ABCIQueryWithOptions(_ context.Context, path string, _ cmtbytes.HexBytes, _ rpcclient.ABCIQueryOptions) (*coretypes.ResultABCIQuery, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if path != "/svn.authority.v1.Query/Governance" {
		return nil, fmt.Errorf("fake node: no handler for %s", path)
	}
	value, err := proto.Marshal(&authorityv1.QueryGovernanceResponse{
		State: &authorityv1.AuthorityState{Submitters: n.submitters, DevnetTestSeats: n.seats}, Page: &typesv1.PageResponse{},
	})
	if err != nil {
		return nil, err
	}
	return &coretypes.ResultABCIQuery{Response: abci.ResponseQuery{Value: value, Height: n.height - n.queryLag}}, nil
}

func (n *fakeNode) BroadcastTxSync(context.Context, cmttypes.Tx) (*coretypes.ResultBroadcastTx, error) {
	return nil, errors.New("fake node: the signer never broadcasts")
}

func (n *fakeNode) Tx(context.Context, []byte, bool) (*coretypes.ResultTx, error) {
	return nil, errors.New("fake node: the signer never reads transactions")
}

func (n *fakeNode) TxSearch(context.Context, string, bool, *int, *int, string) (*coretypes.ResultTxSearch, error) {
	return nil, errors.New("fake node: the signer never searches")
}

func (n *fakeNode) Commit(context.Context, *int64) (*coretypes.ResultCommit, error) {
	return nil, errors.New("fake node: the signer never reads commits")
}

// fixture is one signer setup: generated keys, a registry, a socket path
// and a fake node that gives the keys their roles.
type fixture struct {
	dir         string
	keyDir      string
	service     cryptotypes.PrivKey
	broadcaster cryptotypes.PrivKey
	intentPub   ed25519.PublicKey
	intentPriv  ed25519.PrivateKey
	strayPriv   ed25519.PrivateKey
	node        *fakeNode
	cfg         Config
}

// writeKey writes key in the devnet format with mode 0600.
func writeKey(t testing.TB, dir, name string, key cryptotypes.PrivKey) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(hex.EncodeToString(key.Bytes())+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// shortTempDir returns a directory directly under the system temp
// directory, short enough for a Unix socket path, removed at cleanup.
func shortTempDir(t testing.TB, mode os.FileMode) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "svn-sig-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, mode); err != nil {
		t.Fatal(err)
	}
	return dir
}

func newFixture(t testing.TB) *fixture {
	t.Helper()
	f := &fixture{dir: shortTempDir(t, 0o755), service: sdksecp.GenPrivKey(), broadcaster: sdksecp.GenPrivKey()}
	var err error
	if f.intentPub, f.intentPriv, err = ed25519.GenerateKey(rand.Reader); err != nil {
		t.Fatal(err)
	}
	if _, f.strayPriv, err = ed25519.GenerateKey(rand.Reader); err != nil {
		t.Fatal(err)
	}
	f.keyDir = filepath.Join(f.dir, "keys")
	sockDir := filepath.Join(f.dir, "sock")
	regDir := filepath.Join(f.dir, "registry")
	for path, mode := range map[string]os.FileMode{f.keyDir: 0o700, sockDir: 0o711, regDir: 0o755} {
		if err := os.Mkdir(path, mode); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(path, mode); err != nil {
			t.Fatal(err)
		}
	}
	regPath := filepath.Join(regDir, "intent-keys.json")
	reg := fmt.Sprintf(`{"entries":[{"principal_id":"alice","public_key_hex":%q,"label":"devnet fixture"}]}`, hex.EncodeToString(f.intentPub))
	if err := os.WriteFile(regPath, []byte(reg), 0o444); err != nil {
		t.Fatal(err)
	}
	f.node = &fakeNode{chainID: testChainID, height: 100, submitters: []*typesv1.Submitter{
		{Address: f.service.PubKey().Address(), Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE},
		{Address: f.broadcaster.PubKey().Address(), Role: typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER},
	}}
	f.cfg = Config{
		ChainID: testChainID, RPCURL: "http://127.0.0.1:26657", RPCTimeout: 2 * time.Second, PollInterval: 100 * time.Millisecond,
		FreshnessPolls: 4, MaxTimeoutOffset: 1000, SocketPath: filepath.Join(sockDir, "signer.sock"), Boundary: policy.SameUID,
		ServeUID: uint32(os.Geteuid()), RegistryPath: regPath,
		Keys: []KeyConfig{
			{Name: "service", Class: policy.ClassService, Path: writeKey(t, f.keyDir, "service.hex", f.service)},
			{Name: "broadcaster", Class: policy.ClassBroadcaster, Path: writeKey(t, f.keyDir, "broadcaster.hex", f.broadcaster)},
		},
	}
	return f
}

// run starts the signer on the fake node and returns it with a client.
func (f *fixture) run(t testing.TB) (*Server, *Client) {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	srv, err := start(ctx, f.cfg, f.node, uint32(os.Geteuid()))
	if err != nil {
		cancel()
		t.Fatalf("start: %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(ctx) }()
	t.Cleanup(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Serve: %v", err)
		}
	})
	codec, err := chain.NewCodec(testChainID)
	if err != nil {
		t.Fatal(err)
	}
	client, err := NewClient(f.cfg.SocketPath, codec, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return srv, client
}

func digest(label string) *typesv1.Digest {
	sum := sha256.Sum256([]byte(label))
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]}
}

// offer is a service-signed offer.
func (f *fixture) offer() *leasesv1.MsgOfferLease {
	return &leasesv1.MsgOfferLease{Signer: f.service.PubKey().Address(), BusinessId: "bcast-offer-1", OfferId: "offer-1",
		ContentId: "content-1", ContentVersion: 1, ProviderId: "provider-1", ManifestDigest: digest("m"), PackageBytes: 1000,
		TermsDigest: digest("t"), StartUnixSeconds: 1_900_000_000, DeadlineUnixSeconds: 1_900_000_600, ExpiryUnixSeconds: 1_900_003_600,
		TermVersion: 1}
}

// accept is a broadcaster relay of an acceptance (the inner signature is
// not checked by the signer, only by the chain and serve's pre-check).
func (f *fixture) accept() *leasesv1.MsgAcceptLease {
	return &leasesv1.MsgAcceptLease{Signer: f.broadcaster.PubKey().Address(), BusinessId: "bcast-accept-1", OfferId: "offer-1",
		LeaseId: "lease-1", ProviderAcceptance: &typesv1.InnerSignature{Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT,
			PublicKey: make([]byte, 32), Signature: make([]byte, 64), KeyId: "p1-key-1", CanonicalProfileVersion: msgkit.CanonicalProfileVersion}}
}

// register is a service register whose hosting intent priv signed for
// host.
func (f *fixture) register(t testing.TB, priv ed25519.PrivateKey, host []byte) *contentv1.MsgRegisterContent {
	t.Helper()
	m := &contentv1.MsgRegisterContent{Signer: f.service.PubKey().Address(), BusinessId: "bcast-reg-1", ContentId: "content-1",
		ContentVersion: 1, ManifestDigest: digest("manifest"), PackageBytes: 4096}
	sb, err := content.RegisterIntentSignBytes(host, m.GetContentId(), 1, m.GetManifestDigest().GetValue(), 4096, content.DevnetRoleName)
	if err != nil {
		t.Fatal(err)
	}
	m.HostingIntent = &typesv1.InnerSignature{Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT,
		PublicKey: append([]byte(nil), priv.Public().(ed25519.PublicKey)...), Signature: ed25519.Sign(priv, sb), KeyId: "intent-1",
		CanonicalProfileVersion: msgkit.CanonicalProfileVersion}
	return m
}

// withdraw is a service withdraw whose hosting intent priv signed for host.
func (f *fixture) withdraw(t testing.TB, priv ed25519.PrivateKey, host []byte) *contentv1.MsgWithdrawContent {
	t.Helper()
	m := &contentv1.MsgWithdrawContent{Signer: f.service.PubKey().Address(), BusinessId: "bcast-wd-1", ContentId: "content-1",
		ContentVersion: 1, ReasonDigest: digest("reason")}
	sb, err := content.WithdrawIntentSignBytes(host, m.GetContentId(), 1, m.GetReasonDigest().GetValue(), content.DevnetRoleName)
	if err != nil {
		t.Fatal(err)
	}
	m.HostingIntent = &typesv1.InnerSignature{Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT,
		PublicKey: append([]byte(nil), priv.Public().(ed25519.PublicKey)...), Signature: ed25519.Sign(priv, sb), KeyId: "intent-1",
		CanonicalProfileVersion: msgkit.CanonicalProfileVersion}
	return m
}

// rawCall sends req to the signer and returns the decoded response, for
// requests the Client never sends. Like Client.call it reads the answer
// even after a failed write: a signer that refuses the peer answers without
// reading the request and closes, so the write can fail with a broken pipe
// while the refusal waits to be read.
func rawCall(t testing.TB, socket string, req any) response {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var d net.Dialer
	conn, err := d.DialContext(ctx, "unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if err := conn.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	writeErr := writeFrame(conn, req, MaxRequestBytes)
	var resp response
	if err := readFrame(conn, &resp, MaxResponseBytes); err != nil {
		t.Fatalf("read answer: %v (write: %v)", err, writeErr)
	}
	if writeErr != nil && resp.Refusal == nil {
		t.Fatalf("write failed (%v) and the answer is not a refusal", writeErr)
	}
	return resp
}

// syncBuffer is a goroutine-safe log sink.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newTestLogger(w *syncBuffer) *slog.Logger { return logx.New(w, slog.LevelDebug) }

// containsHexOrBase64 reports whether out holds secret in hex or base64.
func containsHexOrBase64(out string, secret []byte) bool {
	return strings.Contains(out, hex.EncodeToString(secret)) || strings.Contains(out, base64.StdEncoding.EncodeToString(secret)) ||
		strings.Contains(out, base64.RawURLEncoding.EncodeToString(secret))
}

// refusalCode returns the refusal code of a raw response.
func refusalCode(resp response) policy.Code {
	if resp.Refusal == nil {
		return ""
	}
	return resp.Refusal.Code
}
