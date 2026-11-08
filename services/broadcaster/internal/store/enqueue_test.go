package store

import (
	"bufio"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lib/pq"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgtest"
)

// helperEnqueueThenHang is the helper process of
// TestClientDeathMidTransaction.
const helperEnqueueThenHang = "enqueue-then-hang"

// helperInput is what a helper process reads from its standard input.
type helperInput struct {
	Conn ConnConfig `json:"conn"`
	Key  string     `json:"key"`
}

// runHelper runs a helper process and returns its exit code.
func runHelper(name string) int {
	if name != helperEnqueueThenHang {
		fmt.Fprintln(os.Stderr, "unknown helper", name)
		return 2
	}
	var in helperInput
	if err := json.NewDecoder(os.Stdin).Decode(&in); err != nil {
		fmt.Fprintln(os.Stderr, "helper input:", err)
		return 1
	}
	db, err := OpenDB(in.Conn)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper open:", err)
		return 1
	}
	ctx := context.Background()
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		fmt.Fprintln(os.Stderr, "helper begin:", err)
		return 1
	}
	var pid int
	if err := tx.QueryRowContext(ctx, `SELECT pg_backend_pid()`).Scan(&pid); err != nil {
		fmt.Fprintln(os.Stderr, "helper pid:", err)
		return 1
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO app_fixture.orders (order_id) VALUES ($1)`, in.Key); err != nil {
		fmt.Fprintln(os.Stderr, "helper app row:", err)
		return 1
	}
	if _, _, err := sqlEnqueue(ctx, tx, in.Key, `{"order":"`+in.Key+`"}`); err != nil {
		fmt.Fprintln(os.Stderr, "helper enqueue:", err)
		return 1
	}
	fmt.Printf("READY %d\n", pid)
	select {}
}

// appFixture creates the test-only application schema app_fixture with one
// table the caller role may insert into, standing in for a goal 13+ service
// that writes its own row next to the enqueue (plan 6.3).
func appFixture(t *testing.T, e *env, callerRole string) {
	t.Helper()
	mustExec(t, e.super, `CREATE SCHEMA app_fixture`)
	mustExec(t, e.super, `CREATE TABLE app_fixture.orders (order_id text PRIMARY KEY)`)
	mustExec(t, e.super, `GRANT USAGE ON SCHEMA app_fixture TO `+pq.QuoteIdentifier(callerRole))
	mustExec(t, e.super, `GRANT SELECT, INSERT ON app_fixture.orders TO `+pq.QuoteIdentifier(callerRole))
}

// roleOf returns the role mapped to principal.
func (e *env) roleOf(principal string) string {
	e.t.Helper()
	var role string
	if err := e.super.QueryRowContext(e.ctx, `SELECT role_name FROM broadcaster.callers WHERE principal = $1`,
		principal).Scan(&role); err != nil {
		e.t.Fatal(err)
	}
	return role
}

// TestEnqueueAtomicWithApplicationRow (plan 13.2, 6.3): a caller service
// writes its application row and calls enqueue_intent in one transaction;
// after commit both rows exist, with the first transitions row, and the
// worker channel was notified at commit.
func TestEnqueueAtomicWithApplicationRow(t *testing.T) {
	e := newEnv(t)
	callerDB := e.caller("p-app", 4)
	appFixture(t, e, e.roleOf("p-app"))

	listener := pq.NewListener(func() string {
		c := connConfig(e.name, RuntimeRole, runtimePass)
		return "host=" + c.SocketDir + " port=5432 dbname=" + c.Database + " user=" + c.User + " password=" + c.Password + " sslmode=disable"
	}(), time.Second, time.Second, nil)
	defer listener.Close()
	if err := listener.Listen(WorkChannel); err != nil {
		t.Fatal(err)
	}

	tx, err := callerDB.BeginTx(e.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, tx, `INSERT INTO app_fixture.orders (order_id) VALUES ('order-1')`)
	id, created, err := sqlEnqueue(e.ctx, tx, "order-1", `{"order":"order-1"}`)
	if err != nil || !created {
		t.Fatalf("enqueue: %v", err)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.intents WHERE intent_id = $1`, id); got != 0 {
		t.Fatal("uncommitted intent visible to another session")
	}
	select {
	case n := <-listener.Notify:
		t.Fatalf("notified before commit: %+v", n)
	case <-time.After(200 * time.Millisecond):
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	select {
	case n := <-listener.Notify:
		if n == nil || n.Channel != WorkChannel || n.Extra != "" {
			t.Fatalf("notification %+v", n)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no notification after commit")
	}
	if got := e.count(`SELECT count(*) FROM app_fixture.orders WHERE order_id = 'order-1'`); got != 1 {
		t.Fatal("application row missing")
	}
	st, phase, _ := e.state(id)
	if st != StateRequested || phase != PhaseQueued {
		t.Fatalf("intent %s/%s", st, phase)
	}
	var validated bool
	var entry string
	var digest []byte
	if err := e.super.QueryRowContext(e.ctx, `SELECT validated, entry, sql_digest FROM broadcaster.intents WHERE intent_id = $1`,
		id).Scan(&validated, &entry, &digest); err != nil {
		t.Fatal(err)
	}
	if validated || entry != "SQL" || len(digest) != 32 {
		t.Fatalf("validated=%v entry=%s digest=%x", validated, entry, digest)
	}
	trs, err := e.st.Transitions(e.ctx, id)
	if err != nil || len(trs) != 1 || trs[0].Reason != "CREATED" || trs[0].WorkerInstance != "sql-enqueue" {
		t.Fatalf("transitions %+v %v", trs, err)
	}
}

// TestEnqueueRollbackLeavesNothing (plan 13.2): a rollback after the enqueue,
// and an enqueue that fails after the application row was written, leave
// neither row; a failing HTTP-path enqueue writes nothing either.
func TestEnqueueRollbackLeavesNothing(t *testing.T) {
	e := newEnv(t)
	callerDB := e.caller("p-app", 1)
	appFixture(t, e, e.roleOf("p-app"))

	tx, err := callerDB.BeginTx(e.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, tx, `INSERT INTO app_fixture.orders (order_id) VALUES ('order-rb')`)
	if _, _, err := sqlEnqueue(e.ctx, tx, "order-rb", `{"order":"rb"}`); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := sqlEnqueue(e.ctx, callerDB, "fills-cap", `{"order":"cap"}`); err != nil {
		t.Fatal(err)
	}
	tx, err = callerDB.BeginTx(e.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, tx, `INSERT INTO app_fixture.orders (order_id) VALUES ('order-cap')`)
	_, _, err = sqlEnqueue(e.ctx, tx, "order-cap", `{"order":"over"}`)
	wantCode(t, err, "SV005")
	if err := tx.Commit(); err == nil {
		t.Fatal("commit after a failed statement succeeded")
	}
	if got := e.count(`SELECT count(*) FROM app_fixture.orders`); got != 0 {
		t.Fatalf("%d application rows left", got)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.intents`); got != 1 {
		t.Fatalf("%d intents, want only the cap filler", got)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.transitions`); got != 1 {
		t.Fatalf("%d transitions, want only the cap filler's", got)
	}

	e.principal("p-http", 1)
	signer := e.signer(1)
	e.enqueue("p-http", "first", signer, time.Hour)
	before := e.count(`SELECT count(*) FROM broadcaster.intents`) + e.count(`SELECT count(*) FROM broadcaster.business_attempts`)
	req, digest := request("second")
	_, err = e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: "p-http", IdempotencyKey: "second", Kind: KindOfferLease,
		Request: req, RequestDigest: digest, AuthContext: []byte(`{}`), SignerAddress: signer,
		Object: ObjectRef{Type: ObjectOffer, ID: "second"}, ExpiresIn: time.Hour})
	wantErr(t, err, ErrCapReached)
	unknownSigner := make([]byte, 20)
	e.principal("p-http2", 4)
	_, err = e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: "p-http2", IdempotencyKey: "third", Kind: KindOfferLease,
		Request: req, RequestDigest: digest, AuthContext: []byte(`{}`), SignerAddress: unknownSigner,
		Object: ObjectRef{Type: ObjectOffer, ID: "third"}, ExpiresIn: time.Hour})
	wantCode(t, err, "23503")
	after := e.count(`SELECT count(*) FROM broadcaster.intents`) + e.count(`SELECT count(*) FROM broadcaster.business_attempts`)
	if after != before {
		t.Fatalf("failed HTTP enqueues wrote rows: %d -> %d", before, after)
	}
}

// TestClientDeathMidTransaction (plan 13.2): a real client process writes its
// application row, calls enqueue_intent and is killed with SIGKILL before
// commit. Once its backend is gone neither row exists and the idempotency
// key is free.
func TestClientDeathMidTransaction(t *testing.T) {
	e := newEnv(t)
	role, rolePass := e.callerCreds("p-dying", 4)
	appFixture(t, e, role)
	in, err := json.Marshal(helperInput{Conn: connConfig(e.name, role, rolePass), Key: "dying-1"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(e.ctx, 2*time.Minute)
	defer cancel()
	cmd := pgtest.HelperCommand(ctx, helperEnqueueThenHang)
	cmd.Stdin = strings.NewReader(string(in))
	cmd.Stderr = os.Stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	ready := make(chan int, 1)
	go func() {
		s := bufio.NewScanner(stdout)
		for s.Scan() {
			if v, ok := strings.CutPrefix(s.Text(), "READY "); ok {
				if pid, err := strconv.Atoi(v); err == nil {
					ready <- pid
					return
				}
			}
		}
		close(ready)
	}()
	var backend int
	select {
	case pid, ok := <-ready:
		if !ok {
			t.Fatal("helper exited before READY")
		}
		backend = pid
	case <-ctx.Done():
		t.Fatal("helper did not report READY")
	}
	var state string
	if err := e.super.QueryRowContext(e.ctx, `SELECT state FROM pg_stat_activity WHERE pid = $1`, backend).Scan(&state); err != nil {
		t.Fatal(err)
	}
	if state != "idle in transaction" {
		t.Fatalf("helper backend state %q, want idle in transaction", state)
	}
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	if err := cmd.Wait(); err == nil {
		t.Fatal("helper exited cleanly after SIGKILL")
	}
	deadline := time.Now().Add(30 * time.Second)
	for e.count(`SELECT count(*) FROM pg_stat_activity WHERE pid = $1`, backend) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("helper backend still present")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if got := e.count(`SELECT count(*) FROM app_fixture.orders`); got != 0 {
		t.Fatalf("%d application rows after the client died", got)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.intents`); got != 0 {
		t.Fatalf("%d intents after the client died", got)
	}
	rerun, err := OpenDB(connConfig(e.name, role, rolePass))
	if err != nil {
		t.Fatal(err)
	}
	defer rerun.Close()
	if _, created, err := sqlEnqueue(e.ctx, rerun, "dying-1", `{"order":"dying-1"}`); err != nil || !created {
		t.Fatalf("key not free after the client died: %v", err)
	}
}

// TestSQLMetacharactersAreData (plan 13.2, TM-INJECTION): quotes, semicolons
// and comment markers in JSON values and authorization contexts are stored
// verbatim; in an idempotency key or kind they are refused with a code and
// nothing is written; the schema is intact afterwards.
func TestSQLMetacharactersAreData(t *testing.T) {
	e := newEnv(t)
	callerDB := e.caller("p-meta", 16)
	payload := `{"a":"'); DROP TABLE broadcaster.intents; --","b":"\"quoted\" \\ back\\slash","c":"$fn$ $$ ; /* x */","d":"☃ é"}`
	id, created, err := sqlEnqueue(e.ctx, callerDB, "meta-1", payload)
	if err != nil || !created {
		t.Fatal(err)
	}
	var same bool
	if err := e.super.QueryRowContext(e.ctx, `SELECT request_json = $2::jsonb FROM broadcaster.intents WHERE intent_id = $1`,
		id, payload).Scan(&same); err != nil || !same {
		t.Fatalf("payload not stored verbatim: %v %v", same, err)
	}
	for _, key := range []string{"k'; DROP TABLE x; --", "k\"q", "K", "k k", strings.Repeat("k", 65)} {
		_, _, err := sqlEnqueue(e.ctx, callerDB, key, `{}`)
		wantCode(t, err, "SV004")
	}
	var dummy string
	err = callerDB.QueryRowContext(e.ctx, `SELECT intent_id FROM broadcaster.enqueue_intent($1, '{}'::jsonb, '{}'::jsonb, 'k-ok', interval '1 hour')`,
		"KIND'); --").Scan(&dummy)
	wantCode(t, err, "SV004")
	// A listed kind, so each refusal below is for its own argument.
	err = callerDB.QueryRowContext(e.ctx, `SELECT intent_id FROM broadcaster.enqueue_intent('OFFER_LEASE', '[1]'::jsonb, '{}'::jsonb, 'k-arr', interval '1 hour')`).Scan(&dummy)
	wantMessage(t, err, "SV004", "SVN_BAD_REQUEST")
	err = callerDB.QueryRowContext(e.ctx, `SELECT intent_id FROM broadcaster.enqueue_intent('OFFER_LEASE', '{}'::jsonb, '{}'::jsonb, 'k-exp', interval '4 seconds')`).Scan(&dummy)
	wantMessage(t, err, "SV004", "SVN_BAD_EXPIRY")
	err = callerDB.QueryRowContext(e.ctx, `SELECT intent_id FROM broadcaster.enqueue_intent('OFFER_LEASE', '{}'::jsonb, '{}'::jsonb, 'k-exp', interval '8 days')`).Scan(&dummy)
	wantMessage(t, err, "SV004", "SVN_BAD_EXPIRY")
	big := `{"x":"` + strings.Repeat("y", 65536) + `"}`
	err = callerDB.QueryRowContext(e.ctx, `SELECT intent_id FROM broadcaster.enqueue_intent('OFFER_LEASE', $1::jsonb, '{}'::jsonb, 'k-big', interval '1 hour')`, big).Scan(&dummy)
	wantMessage(t, err, "SV004", "SVN_BAD_REQUEST")
	for _, kind := range []string{"K", "FORWARD_AUDIT_BATCH", "TEST_KIND"} {
		err = callerDB.QueryRowContext(e.ctx, `SELECT intent_id FROM broadcaster.enqueue_intent($1, '{}'::jsonb, '{}'::jsonb,
			'k-kind', interval '1 hour')`, kind).Scan(&dummy)
		wantMessage(t, err, "SV004", "SVN_BAD_KIND")
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.intents`); got != 1 {
		t.Fatalf("%d intents, want 1", got)
	}

	e.principal("p-http", 4)
	signer := e.signer(1)
	authCtx := `{"principal_id":"x'); DROP TABLE broadcaster.callers; --","policy_ref":"$$"}`
	req, digest := request("meta")
	res, err := e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: "p-http", IdempotencyKey: "meta-http", Kind: KindOfferLease, Object: testOffer,
		Request: req, RequestDigest: digest, AuthContext: []byte(authCtx), SignerAddress: signer, ExpiresIn: time.Hour})
	if err != nil {
		t.Fatal(err)
	}
	if err := e.super.QueryRowContext(e.ctx, `SELECT auth_context = $2::jsonb FROM broadcaster.intents WHERE intent_id = $1`,
		res.IntentID, authCtx).Scan(&same); err != nil || !same {
		t.Fatalf("auth context not stored verbatim: %v %v", same, err)
	}
	// 13 tables of 0001_outbox, plus kinds, forwarded_txs and
	// content_owners of 0002_answers.
	if got := e.count(`SELECT count(*) FROM pg_tables WHERE schemaname = 'broadcaster'`); got != 16 {
		t.Fatalf("broadcaster has %d tables, want 16", got)
	}
}

// TestSQLDigestIgnoresKeyOrder (plan 13.2, 5.4): the SQL-path digest is the
// SHA-256 of the jsonb text, so the same request with reordered keys and
// other whitespace is the same intent; a changed value or array order is an
// idempotency conflict.
func TestSQLDigestIgnoresKeyOrder(t *testing.T) {
	e := newEnv(t)
	callerDB := e.caller("p-digest", 8)
	id1, created1, err := sqlEnqueue(e.ctx, callerDB, "dig", `{"b":1,"a":[1,2],"c":{"y":2,"x":1}}`)
	if err != nil || !created1 {
		t.Fatal(err)
	}
	id2, created2, err := sqlEnqueue(e.ctx, callerDB, "dig", " { \"c\" : {\"x\":1, \"y\":2},\n\"a\":[1, 2], \"b\":1 } ")
	if err != nil || created2 || id2 != id1 {
		t.Fatalf("reordered keys: id=%s created=%v err=%v", id2, created2, err)
	}
	var text string
	var digest []byte
	if err := e.super.QueryRowContext(e.ctx, `SELECT request_json::text, sql_digest FROM broadcaster.intents WHERE intent_id = $1`,
		id1).Scan(&text, &digest); err != nil {
		t.Fatal(err)
	}
	if sum := sha256.Sum256([]byte(text)); string(sum[:]) != string(digest) {
		t.Fatal("sql_digest is not the SHA-256 of the jsonb text")
	}
	_, _, err = sqlEnqueue(e.ctx, callerDB, "dig", `{"b":1,"a":[2,1],"c":{"y":2,"x":1}}`)
	wantCode(t, err, "SV006")
	_, _, err = sqlEnqueue(e.ctx, callerDB, "dig", `{"b":2,"a":[1,2],"c":{"y":2,"x":1}}`)
	wantCode(t, err, "SV006")
	if got := e.count(`SELECT count(*) FROM broadcaster.intents`); got != 1 {
		t.Fatalf("%d intents", got)
	}
}

// TestIdempotencyUniqueUnderConcurrency (plan 13.2): 50 concurrent enqueues
// with one caller and key create exactly one intent on each path; with two
// digests racing, exactly one wins and the losers get the existing intent or
// ErrIdempotencyConflict, never a second row.
func TestIdempotencyUniqueUnderConcurrency(t *testing.T) {
	e := newEnv(t)
	e.principal("p-race", 256)
	signer := e.signer(1)
	req, digest := request("race")
	other, otherDigest := request("race-other")
	enq := func(key string, r []byte, d [32]byte) (EnqueueResult, error) {
		return e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: "p-race", IdempotencyKey: key, Kind: KindOfferLease, Object: testOffer,
			Request: r, RequestDigest: d, AuthContext: []byte(`{}`), SignerAddress: signer, ExpiresIn: time.Hour})
	}

	const n = 50
	var wg sync.WaitGroup
	results := make([]EnqueueResult, n)
	errs := make([]error, n)
	start := make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			results[i], errs[i] = enq("same", req, digest)
		}()
	}
	close(start)
	wg.Wait()
	created := 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
		if results[i].IntentID != results[0].IntentID {
			t.Fatal("two intent ids for one key")
		}
		if results[i].Created {
			created++
		}
	}
	if created != 1 || e.count(`SELECT count(*) FROM broadcaster.intents WHERE idempotency_key = 'same'`) != 1 {
		t.Fatalf("created %d times", created)
	}

	start = make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if i%2 == 0 {
				results[i], errs[i] = enq("split", req, digest)
			} else {
				results[i], errs[i] = enq("split", other, otherDigest)
			}
		}()
	}
	close(start)
	wg.Wait()
	created = 0
	var winner string
	for i := range n {
		switch {
		case errs[i] == nil && results[i].Created:
			created++
			winner = results[i].IntentID
		case errs[i] == nil:
		case errors.Is(errs[i], ErrIdempotencyConflict):
		default:
			t.Fatalf("goroutine %d: %v", i, errs[i])
		}
	}
	if created != 1 || e.count(`SELECT count(*) FROM broadcaster.intents WHERE idempotency_key = 'split'`) != 1 {
		t.Fatalf("split digests created %d intents", created)
	}
	for i := range n {
		if errs[i] == nil && results[i].IntentID != winner {
			t.Fatal("a loser got another intent id")
		}
	}

	callerDB := e.caller("p-race-sql", 256)
	callerDB.SetMaxOpenConns(n)
	ids := make([]string, n)
	flags := make([]bool, n)
	start = make(chan struct{})
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			ids[i], flags[i], errs[i] = sqlEnqueue(e.ctx, callerDB, "sql-same", `{"v":1}`)
		}()
	}
	close(start)
	wg.Wait()
	created = 0
	for i := range n {
		if errs[i] != nil {
			t.Fatalf("SQL goroutine %d: %v", i, errs[i])
		}
		if ids[i] != ids[0] {
			t.Fatal("two SQL intent ids for one key")
		}
		if flags[i] {
			created++
		}
	}
	if created != 1 {
		t.Fatalf("SQL path created %d times", created)
	}
}

// TestPerCallerCapNoGlobalLock (plan 13.2, m8): while one caller's enqueue
// transaction stays open, another caller enqueues on both paths and the
// worker persists an attempt for the waiting caller's own intent without
// waiting; only the same caller's next enqueue waits, and gives up after the
// lock timeout. The cap counts open intents, so a terminal intent frees a
// slot.
func TestPerCallerCapNoGlobalLock(t *testing.T) {
	e := newEnv(t)
	slow := e.caller("p-slow", 2)
	fast := e.caller("p-fast", 8)
	e.principal("p-slow", 2)
	e.principal("p-http", 8)
	signer := e.signer(1)
	existing := e.enqueue("p-slow", "existing", signer, time.Hour)

	tx, err := slow.BeginTx(e.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, _, err := sqlEnqueue(e.ctx, tx, "open-tx", `{"v":1}`); err != nil {
		t.Fatal(err)
	}

	quick, cancel := context.WithTimeout(e.ctx, 1500*time.Millisecond)
	defer cancel()
	if _, _, err := sqlEnqueue(quick, fast, "fast-1", `{"v":2}`); err != nil {
		t.Fatalf("other caller, SQL path, blocked: %v", err)
	}
	req, digest := request("http-1")
	if _, err := e.st.EnqueueHTTP(quick, EnqueueRequest{CallerPrincipal: "p-http", IdempotencyKey: "http-1", Kind: KindOfferLease, Object: testOffer,
		Request: req, RequestDigest: digest, AuthContext: []byte(`{}`), SignerAddress: signer, ExpiresIn: time.Hour}); err != nil {
		t.Fatalf("other caller, HTTP path, blocked: %v", err)
	}
	ss := e.lock(signer)
	defer ss.Close()
	if res, err := ss.PersistAttempt(quick, draft(existing.IntentID, 0, 100)); err != nil || res.Refusal != RefusedNone {
		t.Fatalf("worker blocked on the waiting caller's intent: %+v %v", res, err)
	}

	began := time.Now()
	req2, digest2 := request("slow-2")
	_, err = e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: "p-slow", IdempotencyKey: "slow-2", Kind: KindOfferLease, Object: testOffer,
		Request: req2, RequestDigest: digest2, AuthContext: []byte(`{}`), SignerAddress: signer, ExpiresIn: time.Hour})
	wantErr(t, err, ErrLockTimeout)
	if waited := time.Since(began); waited < 1500*time.Millisecond || waited > 4*time.Second {
		t.Fatalf("same-caller enqueue waited %v, want about the 2 s lock timeout", waited)
	}
	slow2, err := slow.Conn(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer slow2.Close()
	_, _, err = sqlEnqueue(e.ctx, slow2, "slow-sql-2", `{"v":3}`)
	wantCode(t, err, "55P03")

	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	_, _, err = sqlEnqueue(e.ctx, slow, "slow-3", `{"v":4}`)
	wantCode(t, err, "SV005")
	_, err = e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: "p-slow", IdempotencyKey: "slow-2", Kind: KindOfferLease, Object: testOffer,
		Request: req2, RequestDigest: digest2, AuthContext: []byte(`{}`), SignerAddress: signer, ExpiresIn: time.Hour})
	wantErr(t, err, ErrCapReached)

	var openTx string
	if err := e.super.QueryRowContext(e.ctx, `SELECT intent_id FROM broadcaster.intents WHERE idempotency_key = 'open-tx'`).Scan(&openTx); err != nil {
		t.Fatal(err)
	}
	if err := e.st.FailValidation(e.ctx, openTx, "INVALID"); err != nil {
		t.Fatal(err)
	}
	if _, created, err := sqlEnqueue(e.ctx, slow, "slow-3", `{"v":4}`); err != nil || !created {
		t.Fatalf("slot not freed by a terminal intent: %v", err)
	}
}

// TestEnqueueRequiresReadCommitted: enqueue_intent refuses a REPEATABLE READ
// or SERIALIZABLE caller transaction, whose snapshot could miss a concurrent
// enqueue of the same caller, and a session user with EXECUTE that is not a
// registered caller.
func TestEnqueueRequiresReadCommitted(t *testing.T) {
	e := newEnv(t)
	callerDB := e.caller("p-iso", 4)
	for _, level := range []sql.IsolationLevel{sql.LevelRepeatableRead, sql.LevelSerializable} {
		tx, err := callerDB.BeginTx(e.ctx, &sql.TxOptions{Isolation: level})
		if err != nil {
			t.Fatal(err)
		}
		_, _, err = sqlEnqueue(e.ctx, tx, "iso", `{}`)
		wantCode(t, err, "SV007")
		if err := tx.Rollback(); err != nil {
			t.Fatal(err)
		}
	}

	unmapped := fmt.Sprintf("unmapped_%d", roleSeq.Add(1))
	pass, err := srv.Cluster.CreateLoginRole(e.ctx, unmapped)
	if err != nil {
		t.Fatal(err)
	}
	if err := srv.Cluster.GrantConnect(e.ctx, e.name, unmapped); err != nil {
		t.Fatal(err)
	}
	mustExec(t, e.super, `GRANT USAGE ON SCHEMA broadcaster TO `+pq.QuoteIdentifier(unmapped))
	mustExec(t, e.super, `GRANT EXECUTE ON FUNCTION broadcaster.enqueue_intent(text, jsonb, jsonb, text, interval) TO `+
		pq.QuoteIdentifier(unmapped))
	db := e.open(unmapped, pass)
	defer db.Close()
	_, _, err = sqlEnqueue(e.ctx, db, "unmapped", `{}`)
	wantCode(t, err, "SV003")
	if got := e.count(`SELECT count(*) FROM broadcaster.intents`); got != 0 {
		t.Fatalf("%d intents written by refused calls", got)
	}
}

// TestEnqueueHTTPRefusals: malformed HTTP-path requests are ErrInvalid, an
// unregistered principal is ErrCallerUnknown, an unbound database is
// ErrChainUnbound, and none writes a row.
func TestEnqueueHTTPRefusals(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 4)
	signer := e.signer(1)
	req, digest := request("x")
	base := EnqueueRequest{CallerPrincipal: "p", IdempotencyKey: "k", Kind: KindOfferLease, Object: testOffer, Request: req,
		RequestDigest: digest, AuthContext: []byte(`{"a":1}`), SignerAddress: signer, ExpiresIn: time.Hour}
	mutate := map[string]func(*EnqueueRequest){
		"bad principal":   func(r *EnqueueRequest) { r.CallerPrincipal = "P" },
		"bad key":         func(r *EnqueueRequest) { r.IdempotencyKey = "k k" },
		"bad kind":        func(r *EnqueueRequest) { r.Kind = "kind" },
		"empty request":   func(r *EnqueueRequest) { r.Request = nil },
		"big request":     func(r *EnqueueRequest) { r.Request = make([]byte, 65537) },
		"short signer":    func(r *EnqueueRequest) { r.SignerAddress = signer[:19] },
		"short lifetime":  func(r *EnqueueRequest) { r.ExpiresIn = 4 * time.Second },
		"long lifetime":   func(r *EnqueueRequest) { r.ExpiresIn = 25 * time.Hour },
		"auth not object": func(r *EnqueueRequest) { r.AuthContext = []byte(`[1]`) },
		"auth bad json":   func(r *EnqueueRequest) { r.AuthContext = []byte(`{`) },
		"auth too big":    func(r *EnqueueRequest) { r.AuthContext = []byte(`{"a":"` + strings.Repeat("b", 2048) + `"}`) },
		"auth empty":      func(r *EnqueueRequest) { r.AuthContext = nil },
	}
	for name, m := range mutate {
		r := base
		m(&r)
		if _, err := e.st.EnqueueHTTP(e.ctx, r); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
	r := base
	r.CallerPrincipal = "unknown"
	if _, err := e.st.EnqueueHTTP(e.ctx, r); !errors.Is(err, ErrCallerUnknown) {
		t.Fatalf("unknown principal: want ErrCallerUnknown, got %v", err)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.intents`); got != 0 {
		t.Fatalf("%d intents written by refused requests", got)
	}

	raw := newRawEnv(t)
	if _, err := Migrate(raw.ctx, raw.owner); err != nil {
		t.Fatal(err)
	}
	if err := RegisterCaller(raw.ctx, raw.owner, "p", 4, ""); err != nil {
		t.Fatal(err)
	}
	if err := raw.st.RegisterSigner(raw.ctx, SignerKey{Address: signer, Class: ClassService, KeyName: "key-1",
		LockKey: 1}); err != nil {
		t.Fatal(err)
	}
	if _, err := raw.st.EnqueueHTTP(raw.ctx, base); !errors.Is(err, ErrChainUnbound) {
		t.Fatalf("unbound chain: want ErrChainUnbound, got %v", err)
	}
	if err := CheckChain(raw.ctx, raw.owner, testChainID); !errors.Is(err, ErrChainUnbound) {
		t.Fatalf("CheckChain unbound: %v", err)
	}
	if err := BindChain(raw.ctx, raw.owner, testChainID); err != nil {
		t.Fatal(err)
	}
	if err := BindChain(raw.ctx, raw.owner, testChainID); err != nil {
		t.Fatalf("rebinding the same chain: %v", err)
	}
	if err := BindChain(raw.ctx, raw.owner, "shortvideo-dev-other"); !errors.Is(err, ErrChainMismatch) {
		t.Fatalf("binding another chain: want ErrChainMismatch, got %v", err)
	}
	if err := BindChain(raw.ctx, raw.owner, "bad chain id"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad chain id: want ErrInvalid, got %v", err)
	}
}

// TestMarkValidatedSQLPath: an unvalidated SQL-path intent is listed for
// validation; MarkValidated stores the validated request, signer and a fresh
// business id with business attempt 1, once; then it is due for its signer.
// T2 fails another one. HTTP rows cannot be marked.
func TestMarkValidatedSQLPath(t *testing.T) {
	e := newEnv(t)
	callerDB := e.caller("p-sql", 8)
	signer := e.signer(1)
	id, _, err := sqlEnqueue(e.ctx, callerDB, "v1", `{"v":1}`)
	if err != nil {
		t.Fatal(err)
	}
	badID, _, err := sqlEnqueue(e.ctx, callerDB, "v2", `{"v":2}`)
	if err != nil {
		t.Fatal(err)
	}
	list, err := e.st.UnvalidatedIntents(e.ctx, 10)
	if err != nil || len(list) != 2 || list[0].ID != id || list[0].Kind != KindOfferLease || string(list[0].RequestJSON) != `{"v": 1}` {
		t.Fatalf("unvalidated list %+v %v", list, err)
	}
	if due, err := e.st.DueIntents(e.ctx, signer, 10); err != nil || len(due) != 0 {
		t.Fatalf("unvalidated intents due: %v %v", due, err)
	}
	req, digest := request("validated")
	v := Validation{Request: req, RequestDigest: digest, SignerAddress: signer, Object: ObjectRef{Type: ObjectOffer, ID: "o1"}}
	if err := e.st.MarkValidated(e.ctx, id, v); err != nil {
		t.Fatal(err)
	}
	if err := e.st.MarkValidated(e.ctx, id, v); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("second MarkValidated: want ErrStateChanged, got %v", err)
	}
	var bizID string
	if err := e.super.QueryRowContext(e.ctx, `SELECT i.business_id FROM broadcaster.intents i
		JOIN broadcaster.business_attempts b ON b.intent_id = i.intent_id AND b.n = 1 AND b.business_id = i.business_id
		WHERE i.intent_id = $1 AND i.validated AND i.business_attempt = 1`, id).Scan(&bizID); err != nil {
		t.Fatalf("validated row: %v", err)
	}
	if !idRE.MatchString(bizID) || !strings.HasPrefix(bizID, businessIDPrefix) {
		t.Fatalf("business id %q", bizID)
	}
	if due, err := e.st.DueIntents(e.ctx, signer, 10); err != nil || len(due) != 1 || due[0] != id {
		t.Fatalf("due after validation: %v %v", due, err)
	}
	if err := e.st.FailValidation(e.ctx, badID, "UNAUTHORIZED"); err != nil {
		t.Fatal(err)
	}
	if st, _, reason := e.state(badID); st != StateFailed || reason != "UNAUTHORIZED" {
		t.Fatalf("T2: %s %s", st, reason)
	}
	if err := e.st.FailValidation(e.ctx, id, "CANCELLED"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("T2 with a non-T2 reason: want ErrInvalid, got %v", err)
	}
	e.principal("p-http", 4)
	httpRes := e.enqueue("p-http", "h", signer, time.Hour)
	if err := e.st.MarkValidated(e.ctx, httpRes.IntentID, v); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("MarkValidated on an HTTP row: want ErrStateChanged, got %v", err)
	}
}
