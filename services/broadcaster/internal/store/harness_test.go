package store

// Test harness. Every test in this package runs against a real PostgreSQL 18
// cluster started by internal/pgtest in TestMain (one cluster per test
// binary, behind the machine-wide test lock). Each test gets its own
// database, migrated with the embedded migrations and bound to testChainID,
// and drops it at the end. Transaction bytes in these tests are synthetic
// byte strings with their real SHA-256: the store never decodes them.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgdev"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgtest"
)

const testChainID = "shortvideo-dev-storetest"

// testOffer is the object of the synthetic OFFER_LEASE intents of these
// tests.
var testOffer = ObjectRef{Type: ObjectOffer, ID: "test-offer"}

var (
	srv         *pgtest.Server
	ownerPass   string
	runtimePass string
	dbSeq       atomic.Int64
	roleSeq     atomic.Int64
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
		fmt.Fprintln(os.Stderr, "store tests: start PostgreSQL:", err)
		return 1
	}
	srv = s
	fmt.Println("store tests: server version:", s.Cluster.Version())
	if ownerPass, err = s.Cluster.CreateLoginRole(ctx, OwnerRole); err == nil {
		runtimePass, err = s.Cluster.CreateLoginRole(ctx, RuntimeRole)
	}
	code := 1
	if err != nil {
		fmt.Fprintln(os.Stderr, "store tests: create roles:", err)
	} else {
		code = m.Run()
	}
	if err := s.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "store tests: remove cluster:", err)
		code = 1
	}
	return code
}

// testLimits are the section 11 PROPOSED values, except MinLifetime, which is
// the 5 s hard lower bound so expiry tests wait as little as possible.
func testLimits() Limits {
	return Limits{
		TransitionsPerIntent:    256,
		ObservationBytes:        2048,
		AckLogBytes:             512,
		AttemptsPerIntent:       16,
		AttemptsPerBusinessID:   8,
		BusinessAttempts:        4,
		AuthContextBytes:        2048,
		MinLifetime:             5 * time.Second,
		MaxLifetime:             24 * time.Hour,
		EnqueueLockTimeout:      2 * time.Second,
		EnqueueStatementTimeout: 5 * time.Second,
	}
}

// connConfig is the socket connection to database as user.
func connConfig(database, user, password string) ConnConfig {
	return ConnConfig{
		SocketDir:       srv.Cluster.SocketDir(),
		Port:            pgdev.Port,
		Database:        database,
		User:            user,
		Password:        password,
		ApplicationName: "store-test",
		ConnectTimeout:  10 * time.Second,
	}
}

// env is one test's database.
type env struct {
	t       *testing.T
	ctx     context.Context
	name    string
	owner   *sql.DB
	runtime *sql.DB
	super   *sql.DB
	st      *Store
}

// newEnv creates, migrates and binds a fresh database.
func newEnv(t *testing.T) *env {
	t.Helper()
	e := newRawEnv(t)
	if _, err := Migrate(e.ctx, e.owner); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := BindChain(e.ctx, e.owner, testChainID); err != nil {
		t.Fatalf("bind chain: %v", err)
	}
	return e
}

// newRawEnv creates a fresh database without migrating it. The runtime
// Store is created but unusable until the schema exists.
func newRawEnv(t *testing.T) *env {
	t.Helper()
	ctx := context.Background()
	name := fmt.Sprintf("bt_%d", dbSeq.Add(1))
	if err := srv.NewDatabase(ctx, name, OwnerRole, RuntimeRole); err != nil {
		t.Fatalf("create database: %v", err)
	}
	e := &env{t: t, ctx: ctx, name: name}
	t.Cleanup(e.drop)
	e.owner = e.open(OwnerRole, ownerPass)
	e.runtime = e.open(RuntimeRole, runtimePass)
	e.runtime.SetMaxOpenConns(60)
	super, err := srv.Cluster.SuperuserDB(name)
	if err != nil {
		t.Fatal(err)
	}
	e.super = super
	st, err := New(e.runtime, testLimits(), "test-instance")
	if err != nil {
		t.Fatal(err)
	}
	e.st = st
	return e
}

// open returns a pool to the test database as user.
func (e *env) open(user, password string) *sql.DB {
	e.t.Helper()
	db, err := OpenDB(connConfig(e.name, user, password))
	if err != nil {
		e.t.Fatal(err)
	}
	return db
}

// drop closes the pools and drops the database (FORCE ends other sessions).
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
	if _, err := super.ExecContext(context.Background(),
		"DROP DATABASE IF EXISTS "+pq.QuoteIdentifier(e.name)+" WITH (FORCE)"); err != nil {
		e.t.Errorf("drop %s: %v", e.name, err)
	}
}

// callerCreds creates a login role with CONNECT on the test database, maps
// it to principal with cap, and returns the role and its password.
func (e *env) callerCreds(principal string, cap int) (string, string) {
	e.t.Helper()
	role := fmt.Sprintf("caller_%d", roleSeq.Add(1))
	pass, err := srv.Cluster.CreateLoginRole(e.ctx, role)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := srv.Cluster.GrantConnect(e.ctx, e.name, role); err != nil {
		e.t.Fatal(err)
	}
	if err := RegisterCaller(e.ctx, e.owner, principal, cap, role); err != nil {
		e.t.Fatal(err)
	}
	return role, pass
}

// caller is callerCreds plus a pool connected as the role.
func (e *env) caller(principal string, cap int) *sql.DB {
	e.t.Helper()
	db := e.open(e.callerCreds(principal, cap))
	e.t.Cleanup(func() { db.Close() })
	return db
}

// principal registers an HTTP-path principal (no role) with cap.
func (e *env) principal(principal string, cap int) {
	e.t.Helper()
	if err := RegisterCaller(e.ctx, e.owner, principal, cap, ""); err != nil {
		e.t.Fatal(err)
	}
}

// signer registers a synthetic 20-byte SERVICE key signer "key-<n>" with
// lock key n and syncs it to account 7, next sequence 0, height 1.
func (e *env) signer(n int32) []byte {
	e.t.Helper()
	return e.keySigner(n, ClassService)
}

// keySigner is signer with the given key class.
func (e *env) keySigner(n int32, class SignerClass) []byte {
	e.t.Helper()
	addr := make([]byte, 20)
	addr[0], addr[19] = 0xaa, byte(n)
	addr[18] = byte(n >> 8)
	if err := e.st.RegisterSigner(e.ctx, SignerKey{Address: addr, Class: class, KeyName: fmt.Sprintf("key-%d", n),
		LockKey: n}); err != nil {
		e.t.Fatal(err)
	}
	ss := e.lock(addr)
	defer ss.Close()
	if err := ss.SyncSequence(e.ctx, 7, 0, 1); err != nil {
		e.t.Fatal(err)
	}
	return addr
}

// lock takes the signer session of addr.
func (e *env) lock(addr []byte) *SignerSession {
	e.t.Helper()
	ss, err := e.st.LockSigner(e.ctx, addr)
	if err != nil {
		e.t.Fatal(err)
	}
	return ss
}

// request returns a synthetic request and its digest.
func request(tag string) ([]byte, [32]byte) {
	b := []byte("synthetic-request:" + tag)
	return b, sha256.Sum256(b)
}

// enqueue creates an HTTP-path OFFER_LEASE intent of principal for a
// SERVICE signer, for offer id key.
func (e *env) enqueue(principal, key string, signer []byte, lifetime time.Duration) EnqueueResult {
	e.t.Helper()
	req, digest := request(key)
	res, err := e.st.EnqueueHTTP(e.ctx, EnqueueRequest{
		CallerPrincipal: principal,
		IdempotencyKey:  key,
		Kind:            KindOfferLease,
		Request:         req,
		RequestDigest:   digest,
		AuthContext:     json.RawMessage(`{"principal_kind":"DEV_FIXTURE","principal_id":"p1"}`),
		SignerAddress:   signer,
		Object:          ObjectRef{Type: ObjectOffer, ID: key},
		ExpiresIn:       lifetime,
	})
	if err != nil {
		e.t.Fatalf("enqueue %s: %v", key, err)
	}
	return res
}

// txBytes returns synthetic transaction bytes and their hash.
func txBytes(tag string, seq, timeout uint64) ([]byte, [32]byte) {
	b := []byte(fmt.Sprintf("synthetic-tx:%s:%d:%d", tag, seq, timeout))
	return b, sha256.Sum256(b)
}

// draft is an attempt draft for account 7.
func draft(intentID string, seq, timeout uint64) AttemptDraft {
	b, h := txBytes(intentID, seq, timeout)
	return AttemptDraft{IntentID: intentID, AccountNumber: 7, Sequence: seq, TimeoutHeight: timeout, Gas: 2_000_000,
		TxBytes: b, TxHash: h}
}

// persist persists a draft and fails the test on a refusal.
func (e *env) persist(ss *SignerSession, intentID string, seq, timeout uint64) string {
	e.t.Helper()
	res, err := ss.PersistAttempt(e.ctx, draft(intentID, seq, timeout))
	if err != nil || res.Refusal != RefusedNone {
		e.t.Fatalf("persist: %+v %v", res, err)
	}
	return res.AttemptID
}

// state reads an intent's state, phase and reason as the superuser.
func (e *env) state(intentID string) (State, Phase, string) {
	e.t.Helper()
	var (
		st            State
		phase, reason sql.NullString
	)
	if err := e.super.QueryRowContext(e.ctx, `SELECT state, phase, reason FROM broadcaster.intents WHERE intent_id = $1`,
		intentID).Scan(&st, &phase, &reason); err != nil {
		e.t.Fatal(err)
	}
	return st, Phase(phase.String), reason.String
}

// count returns the result of a count query run as the superuser.
func (e *env) count(query string, args ...any) int {
	e.t.Helper()
	var n int
	if err := e.super.QueryRowContext(e.ctx, query, args...).Scan(&n); err != nil {
		e.t.Fatalf("%s: %v", query, err)
	}
	return n
}

// wantCode fails unless err carries SQLSTATE code.
func wantCode(t *testing.T, err error, code string) {
	t.Helper()
	if got := SQLState(err); got != code {
		t.Fatalf("want SQLSTATE %s, got %q (%v)", code, got, err)
	}
}

// wantMessage fails unless err carries SQLSTATE code and primary message
// msg (the schema's RAISE messages are codes, never argument values).
func wantMessage(t *testing.T, err error, code, msg string) {
	t.Helper()
	var pe *pq.Error
	if !errors.As(err, &pe) || string(pe.Code) != code || pe.Message != msg {
		t.Fatalf("want %s %s, got %v", code, msg, err)
	}
}

// wantErr fails unless errors.Is(err, target).
func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("want %v, got %v", target, err)
	}
}

// sqlEnqueue calls enqueue_intent on db with bind parameters, kind
// OFFER_LEASE.
func sqlEnqueue(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, key, requestJSON string) (string, bool, error) {
	var (
		id      string
		created bool
	)
	err := q.QueryRowContext(ctx, `SELECT intent_id, created FROM broadcaster.enqueue_intent($1, $2::jsonb, $3::jsonb, $4, $5::interval)`,
		KindOfferLease, requestJSON, `{"principal_kind":"DEV_FIXTURE"}`, key, "1 hour").Scan(&id, &created)
	return id, created, err
}

// mustExec runs a statement and fails the test on error.
func mustExec(t *testing.T, db interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}, query string, args ...any) {
	t.Helper()
	if _, err := db.ExecContext(context.Background(), query, args...); err != nil {
		t.Fatalf("%s: %v", strings.SplitN(query, "\n", 2)[0], err)
	}
}
