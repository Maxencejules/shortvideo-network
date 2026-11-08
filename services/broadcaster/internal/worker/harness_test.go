package worker

// Test harness. Every test runs the real worker and the real store against
// a real PostgreSQL 18 cluster (internal/pgtest, one per test binary, behind
// the machine-wide test lock; one database per test), over the real chain
// client. The node is fakeNode and the signer process is fakeSigner: both
// are unit-test fakes, labelled as such, and never end-to-end evidence
// (plan 13.2 "worker over real PostgreSQL with a fake RPC").

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/lib/pq"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/keys"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgdev"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgtest"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

const testChainID = "shortvideo-dev-workertest"

var (
	srv         *pgtest.Server
	ownerPass   string
	runtimePass string
	dbSeq       atomic.Int64
)

func TestMain(m *testing.M) {
	if h := pgtest.Helper(); h != "" {
		os.Exit(runHelper(h))
	}
	os.Exit(runTests(m))
}

func runTests(m *testing.M) int {
	ctx := context.Background()
	s, err := pgtest.Start(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "worker tests: start PostgreSQL:", err)
		return 1
	}
	srv = s
	fmt.Println("worker tests: server version:", s.Cluster.Version())
	if ownerPass, err = s.Cluster.CreateLoginRole(ctx, store.OwnerRole); err == nil {
		runtimePass, err = s.Cluster.CreateLoginRole(ctx, store.RuntimeRole)
	}
	code := 1
	if err != nil {
		fmt.Fprintln(os.Stderr, "worker tests: create roles:", err)
	} else {
		code = m.Run()
	}
	if err := s.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "worker tests: remove cluster:", err)
		code = 1
	}
	return code
}

func storeLimits() store.Limits {
	return store.Limits{TransitionsPerIntent: 256, ObservationBytes: 2048, AckLogBytes: 512, AttemptsPerIntent: 16,
		AttemptsPerBusinessID: 8, BusinessAttempts: 4, AuthContextBytes: 2048, MinLifetime: 5 * time.Second,
		MaxLifetime: 24 * time.Hour, EnqueueLockTimeout: 2 * time.Second, EnqueueStatementTimeout: 5 * time.Second}
}

func outboxLimits() outbox.Limits {
	return outbox.Limits{IndexingMargin: 2, AttemptsPerBusinessID: 8, BusinessAttempts: 4, AttemptsPerIntent: 16, Sdk4Retries: 3}
}

// testNumbers are the section 11 PROPOSED values at their lower bounds
// where a test waits on them.
func testNumbers() Numbers {
	gas := map[policy.Kind]uint64{}
	for _, k := range keySignedKinds {
		gas[k] = 2_000_000
	}
	return Numbers{TimeoutOffset: 5, PollInterval: 100 * time.Millisecond, StallWindow: 3 * time.Second,
		BackoffBase: 100 * time.Millisecond, BackoffCap: time.Second, DeclaredGas: gas, WithdrawMarginPercent: 30,
		DeadlinePages: 2, AuthContextBytes: 2048, MinLifetime: 5 * time.Second, MaxLifetime: 24 * time.Hour,
		RelayLocalExpiry: time.Hour, ForwardWindow: 100}
}

// keyFixture is one development-only secp256k1 key of the fake signer.
type keyFixture struct {
	key  *secp256k1.PrivKey
	info signer.KeyInfo
}

func newKeyFixture(name string, class policy.Class) keyFixture {
	key := secp256k1.GenPrivKeyFromSecret([]byte("svn broadcaster worker test " + name))
	return keyFixture{key: key, info: signer.KeyInfo{Name: name, Class: class, Algorithm: keys.Secp256k1,
		Address: key.PubKey().Address(), PubKey: key.PubKey()}}
}

// fakeSigner stands in for the signer process (a unit-test fake): it runs
// the same allowlist check as the signer client and signs the outer
// transaction with the fixture key. refuse, when set, refuses every
// request.
type fakeSigner struct {
	codec  *chain.Codec
	keys   map[string]keyFixture
	mu     sync.Mutex
	calls  int
	refuse error
	// tamper, when set, changes each request before it is signed: a
	// signer that returns another transaction than the one asked for.
	tamper func(req signer.SignRequest) signer.SignRequest
}

func (f *fakeSigner) sign(_ context.Context, req signer.SignRequest) (signer.Signed, error) {
	f.mu.Lock()
	f.calls++
	refuse, tamper := f.refuse, f.tamper
	f.mu.Unlock()
	if refuse != nil {
		return signer.Signed{}, refuse
	}
	if tamper != nil {
		req = tamper(req)
	}
	k, ok := f.keys[req.KeyName]
	if !ok {
		return signer.Signed{}, &policy.Refusal{Code: signer.CodeUnknownKey}
	}
	if err := policy.CheckSignable(k.info.Class, k.info.Address, req.Msg); err != nil {
		return signer.Signed{}, err
	}
	u, err := f.codec.Prepare(req.Msg, k.key.PubKey(), chain.TxInput{AccountNumber: req.AccountNumber, Sequence: req.Sequence,
		Gas: req.Gas, TimeoutHeight: req.TimeoutHeight})
	if err != nil {
		return signer.Signed{}, err
	}
	sig, err := k.key.Sign(u.SignBytes())
	if err != nil {
		return signer.Signed{}, err
	}
	raw, hash, err := u.Assemble(sig)
	if err != nil {
		return signer.Signed{}, err
	}
	return signer.Signed{Bytes: raw, Hash: hash}, nil
}

func (f *fakeSigner) callCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls
}

// env is one test's database, fake node, fake signer and worker.
type env struct {
	t         *testing.T
	ctx       context.Context
	name      string
	owner     *sql.DB
	runtime   *sql.DB
	super     *sql.DB
	st        *store.Store
	codec     *chain.Codec
	node      *fakeNode
	service   keyFixture
	relayer   keyFixture
	intentKey ed25519.PrivateKey
	signer    *fakeSigner
	cfg       Config
	w         *Worker
	logs      *syncBuffer
}

// syncBuffer is a log sink safe for concurrent writes.
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

const (
	principalApp     = "app-1"
	principalLimited = "app-2"
	// principalSQL and principalSQLLimited are SQL-path callers; a test
	// that uses them registers their database roles.
	principalSQL        = "sql-app"
	principalSQLLimited = "sql-limited"
	creator             = "creator-1"
)

// newEnv creates, migrates and binds a fresh database, the fake node with
// the two key accounts (numbers 7 and 8), the callers, the intent-key
// registry and the worker.
func newEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("wt_%d", dbSeq.Add(1))
	if err := srv.NewDatabase(ctx, name, store.OwnerRole, store.RuntimeRole); err != nil {
		t.Fatalf("create database: %v", err)
	}
	e := &env{t: t, ctx: ctx, name: name, logs: &syncBuffer{}}
	t.Cleanup(e.drop)
	e.owner = e.open(store.OwnerRole, ownerPass)
	e.runtime = e.open(store.RuntimeRole, runtimePass)
	super, err := srv.Cluster.SuperuserDB(name)
	if err != nil {
		t.Fatal(err)
	}
	e.super = super
	if _, err := store.Migrate(ctx, e.owner); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := store.BindChain(ctx, e.owner, testChainID); err != nil {
		t.Fatalf("bind chain: %v", err)
	}
	for _, p := range []string{principalApp, principalLimited} {
		if err := store.RegisterCaller(ctx, e.owner, p, 64, ""); err != nil {
			t.Fatal(err)
		}
	}
	if e.st, err = store.New(e.runtime, storeLimits(), "worker-test"); err != nil {
		t.Fatal(err)
	}
	if e.codec, err = chain.NewCodec(testChainID); err != nil {
		t.Fatal(err)
	}
	e.node = newFakeNode(t, e.codec)
	client, err := chain.New(e.node, e.codec)
	if err != nil {
		t.Fatal(err)
	}
	e.service = newKeyFixture("service", policy.ClassService)
	e.relayer = newKeyFixture("broadcaster", policy.ClassBroadcaster)
	e.node.addAccount(e.service.info.Address, 7)
	e.node.addAccount(e.relayer.info.Address, 8)
	e.intentKey = ed25519.NewKeyFromSeed(bytes.Repeat([]byte{7}, ed25519.SeedSize))
	registry, err := policy.ParseRegistry([]byte(fmt.Sprintf(`{"entries":[{"principal_id":%q,"public_key_hex":%q,"label":"test creator"}]}`,
		creator, hex.EncodeToString(e.intentKey.Public().(ed25519.PublicKey)))))
	if err != nil {
		t.Fatal(err)
	}
	e.signer = &fakeSigner{codec: e.codec, keys: map[string]keyFixture{e.service.info.Name: e.service, e.relayer.info.Name: e.relayer}}
	all := policy.Kinds()
	e.cfg = Config{Store: e.st, Chain: client, RequestSignature: e.signer.sign, Keys: []signer.KeyInfo{e.service.info, e.relayer.info},
		Registry: registry, Callers: []policy.Caller{{Principal: principalApp, Kinds: all, BoundProviderID: "p1"},
			{Principal: principalLimited, Kinds: []policy.Kind{policy.KindRegisterContent}},
			{Principal: principalSQL, Kinds: all}, {Principal: principalSQLLimited, Kinds: []policy.Kind{policy.KindRegisterContent}}},
		Outbox: outboxLimits(), Numbers: testNumbers(), Logger: slog.New(slog.NewJSONHandler(e.logs, nil))}
	if e.w, err = New(e.cfg); err != nil {
		t.Fatal(err)
	}
	if err := e.w.registerSigners(ctx); err != nil {
		t.Fatal(err)
	}
	return e
}

// connConfig is the socket connection to the test database as user.
func connConfig(database, user, password string) store.ConnConfig {
	return store.ConnConfig{SocketDir: srv.Cluster.SocketDir(), Port: pgdev.Port, Database: database, User: user,
		Password: password, ApplicationName: "worker-test", ConnectTimeout: 10 * time.Second}
}

func (e *env) open(user, password string) *sql.DB {
	e.t.Helper()
	db, err := store.OpenDB(connConfig(e.name, user, password))
	if err != nil {
		e.t.Fatal(err)
	}
	return db
}

func (e *env) drop() {
	for _, db := range []*sql.DB{e.owner, e.runtime, e.super} {
		if db != nil {
			db.Close()
		}
	}
	super, err := srv.Cluster.SuperuserDB("postgres")
	if err != nil {
		e.t.Errorf("drop %s: %v", e.name, err)
		return
	}
	defer super.Close()
	if _, err := super.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(e.name)+" WITH (FORCE)"); err != nil {
		e.t.Errorf("drop %s: %v", e.name, err)
	}
}

// session returns a key signer session of the worker, closed at cleanup.
func (e *env) session(class policy.Class) *session {
	e.t.Helper()
	s := newSession(e.w, false, e.w.keys[class])
	e.t.Cleanup(s.close)
	return s
}

// forwarder returns the worker's forwarder session, closed at cleanup.
func (e *env) forwarder() *session {
	e.t.Helper()
	s := newSession(e.w, true, signer.KeyInfo{})
	e.t.Cleanup(s.close)
	return s
}

// pass runs one pass of s and fails the test on an error.
func (e *env) pass(s *session) {
	e.t.Helper()
	if err := s.pass(e.ctx); err != nil {
		e.t.Fatalf("%s pass: %v", s.name(), err)
	}
}

// fullAuth is a context that grants every permission to the registered
// creator principal.
func fullAuth() policy.AuthContext {
	return policy.AuthContext{PrincipalKind: policy.PrincipalKindDevFixture, PrincipalID: creator,
		Permissions: []policy.Permission{policy.PermPublishContent, policy.PermWithdrawContent, policy.PermScheduleHosting,
			policy.PermRelayProviderSigned}, PolicyRef: "policy-1"}
}

func sha(b []byte) []byte {
	sum := sha256.Sum256(b)
	return sum[:]
}

func digestOf(seed string) *typesv1.Digest {
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sha([]byte(seed))}
}

// registerMsg is a REGISTER_CONTENT request for contentID version 1 with a
// hosting intent of the registered creator key naming the service key as
// submitting host (signed here: test code).
func (e *env) registerMsg(contentID string) *contentv1.MsgRegisterContent {
	e.t.Helper()
	return e.registerMsgVersion(contentID, 1)
}

// registerMsgVersion is registerMsg for another content version.
func (e *env) registerMsgVersion(contentID string, version uint64) *contentv1.MsgRegisterContent {
	e.t.Helper()
	manifest := sha([]byte("manifest " + contentID))
	signBytes, err := content.RegisterIntentSignBytes(e.service.info.Address, contentID, version, manifest, 4096, content.DevnetRoleName)
	if err != nil {
		e.t.Fatal(err)
	}
	return &contentv1.MsgRegisterContent{ContentId: contentID, ContentVersion: version,
		ManifestDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: manifest},
		PackageBytes:   4096, HostingIntent: &typesv1.InnerSignature{Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT,
			PublicKey: e.intentKey.Public().(ed25519.PublicKey), Signature: ed25519.Sign(e.intentKey, signBytes),
			KeyId: "intent-key-1", CanonicalProfileVersion: 1}}
}

// offerMsg is an OFFER_LEASE request for contentID version 1.
func offerMsg(offerID, contentID string) *leasesv1.MsgOfferLease {
	return &leasesv1.MsgOfferLease{OfferId: offerID, ContentId: contentID, ContentVersion: 1, ProviderId: "p1",
		ManifestDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sha([]byte("manifest " + contentID))},
		PackageBytes:   4096, TermsDigest: digestOf("terms " + offerID), StartUnixSeconds: 1_900_000_000,
		DeadlineUnixSeconds: 1_900_003_600, ExpiryUnixSeconds: 1_900_000_600, TermVersion: 1}
}

// enqueue stores an HTTP-path intent as the API would after its checks:
// msg is the request's message without signer and business id, signed by
// the kind's key.
func (e *env) enqueue(principal, key string, kind policy.Kind, msg sdk.Msg, life time.Duration) string {
	e.t.Helper()
	stored, err := proto.MarshalOptions{Deterministic: true}.Marshal(msg.(proto.Message))
	if err != nil {
		e.t.Fatal(err)
	}
	auth := fullAuth()
	digest, err := request.Digest(kind, stored, auth)
	if err != nil {
		e.t.Fatal(err)
	}
	authJSON, err := request.CanonicalAuthJSON(auth)
	if err != nil {
		e.t.Fatal(err)
	}
	spec, err := policy.SpecOf(kind)
	if err != nil {
		e.t.Fatal(err)
	}
	signerAddr := e.service.info.Address
	if spec.Class == policy.ClassBroadcaster {
		signerAddr = e.relayer.info.Address
	}
	key0, err := chain.ObjectOf(withSigner(msg, signerAddr))
	if err != nil {
		e.t.Fatal(err)
	}
	object := store.ObjectRef{ID: key0.ID}
	switch m := msg.(type) {
	case *contentv1.MsgRegisterContent, *contentv1.MsgWithdrawContent:
		object.Type, object.Version = store.ObjectContent, key0.Version
	case *leasesv1.MsgOfferLease:
		object.Type = store.ObjectOffer
	case *leasesv1.MsgAcceptLease:
		object.Type, object.ID = store.ObjectOffer, m.GetOfferId()
	default:
		object.Type = store.ObjectLease
	}
	res, err := e.st.EnqueueHTTP(e.ctx, store.EnqueueRequest{CallerPrincipal: principal, IdempotencyKey: key, Kind: string(kind),
		Request: stored, RequestDigest: digest, AuthContext: json.RawMessage(authJSON), SignerAddress: signerAddr,
		Object: object, ExpiresIn: life})
	if err != nil {
		e.t.Fatalf("enqueue %s: %v", key, err)
	}
	return res.IntentID
}

// withSigner returns a copy of msg with signer set.
func withSigner(msg sdk.Msg, signerAddr []byte) sdk.Msg {
	c := proto.Clone(msg.(proto.Message))
	r := c.ProtoReflect()
	r.Set(r.Descriptor().Fields().ByName("signer"), protoreflect.ValueOfBytes(signerAddr))
	return c.(sdk.Msg)
}

// intentRow is an intent's state, phase and reason, read as the superuser.
type intentRow struct {
	state, phase, reason string
}

func (e *env) intent(id string) intentRow {
	e.t.Helper()
	var r intentRow
	var phase, reason sql.NullString
	if err := e.super.QueryRowContext(e.ctx, `SELECT state, phase, reason FROM broadcaster.intents WHERE intent_id = $1`, id).
		Scan(&r.state, &phase, &reason); err != nil {
		e.t.Fatal(err)
	}
	r.phase, r.reason = phase.String, reason.String
	return r
}

// attempts returns an intent's attempts.
func (e *env) attempts(id string) []store.Attempt {
	e.t.Helper()
	atts, err := e.st.Attempts(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return atts
}

// count runs a count query as the superuser.
func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.super.QueryRowContext(e.ctx, query, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}

// ownerStatus is the status of an intent's content owner row.
func (e *env) ownerStatus(id string) string {
	e.t.Helper()
	var s string
	if err := e.super.QueryRowContext(e.ctx, `SELECT status FROM broadcaster.content_owners WHERE intent_id = $1`, id).Scan(&s); err != nil {
		e.t.Fatal(err)
	}
	return s
}

// until runs step, then checks done, up to limit times, and fails the test
// if done never holds.
func (e *env) until(label string, limit int, step func(), done func() bool) {
	e.t.Helper()
	for i := 0; i < limit; i++ {
		step()
		if done() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	e.t.Fatalf("%s: not reached after %d steps", label, limit)
}

// wantState fails unless the intent has state and phase (and reason when
// given).
func (e *env) wantState(id, state, phase, reason string) {
	e.t.Helper()
	got := e.intent(id)
	if got.state != state || got.phase != phase || (reason != "" && got.reason != reason) {
		e.t.Fatalf("intent %s: %+v, want %s/%s %s\nworker log:\n%s", id, got, state, phase, reason, e.logs.String())
	}
}

// logged reports whether the worker's log holds s.
func (e *env) logged(s string) bool { return strings.Contains(e.logs.String(), s) }
