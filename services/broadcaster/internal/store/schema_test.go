package store

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lib/pq"
)

// TestMigrationsChecksumGapForwardOnly (plan 13.2): the runner applies the
// embedded migrations once, records their checksums, and refuses a changed
// checksum, a gap in the files or in the database, and a database newer than
// the binary; a failing migration leaves nothing; two runners at once apply
// each migration exactly once.
func TestMigrationsChecksumGapForwardOnly(t *testing.T) {
	e := newRawEnv(t)
	ctx := e.ctx
	if err := CheckSchema(ctx, e.owner); !errors.Is(err, ErrSchemaBehind) {
		t.Fatalf("CheckSchema on an empty database: want ErrSchemaBehind, got %v", err)
	}

	var wg sync.WaitGroup
	results := make([][]int, 2)
	errs := make([]error, 2)
	for i := range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = Migrate(ctx, e.owner)
		}()
	}
	wg.Wait()
	if errs[0] != nil || errs[1] != nil {
		t.Fatalf("concurrent migrate: %v %v", errs[0], errs[1])
	}
	if len(results[0])+len(results[1]) != 1 {
		t.Fatalf("concurrent migrate applied %v and %v, want version 1 exactly once", results[0], results[1])
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.schema_migrations`); got != 1 {
		t.Fatalf("%d schema_migrations rows", got)
	}
	if again, err := Migrate(ctx, e.owner); err != nil || len(again) != 0 {
		t.Fatalf("second migrate: %v %v", again, err)
	}
	if err := CheckSchema(ctx, e.runtime); err != nil {
		t.Fatalf("CheckSchema as runtime: %v", err)
	}
	embedded, err := loadMigrations(migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	var sum []byte
	if err := e.super.QueryRowContext(ctx, `SELECT sha256 FROM broadcaster.schema_migrations WHERE version = 1`).Scan(&sum); err != nil {
		t.Fatal(err)
	}
	if string(sum) != string(embedded[0].sum[:]) {
		t.Fatal("recorded checksum differs from the embedded file")
	}

	orig := embedded[0].sql
	file := func(body string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(body)} }

	changed := fstest.MapFS{"migrations/0001_outbox.pgsql": file(orig + "\n-- changed\n")}
	if _, err := migrate(ctx, e.owner, changed); !errors.Is(err, ErrMigrationChecksum) {
		t.Fatalf("changed checksum: want ErrMigrationChecksum, got %v", err)
	}
	renamed := fstest.MapFS{"migrations/0001_other.pgsql": file(orig)}
	if _, err := migrate(ctx, e.owner, renamed); !errors.Is(err, ErrMigrationChecksum) {
		t.Fatalf("renamed migration: want ErrMigrationChecksum, got %v", err)
	}
	gapFiles := fstest.MapFS{
		"migrations/0001_outbox.pgsql": file(orig),
		"migrations/0003_next.pgsql":   file("SELECT 1"),
	}
	if _, err := migrate(ctx, e.owner, gapFiles); !errors.Is(err, ErrMigrationFiles) {
		t.Fatalf("file gap: want ErrMigrationFiles, got %v", err)
	}
	stray := fstest.MapFS{
		"migrations/0001_outbox.pgsql": file(orig),
		"migrations/README":            file("x"),
	}
	if _, err := migrate(ctx, e.owner, stray); !errors.Is(err, ErrMigrationFiles) {
		t.Fatalf("stray file: want ErrMigrationFiles, got %v", err)
	}
	if _, err := migrate(ctx, e.owner, fstest.MapFS{"migrations/x": file("")}); !errors.Is(err, ErrMigrationFiles) {
		t.Fatalf("no migration: want ErrMigrationFiles, got %v", err)
	}

	failing := fstest.MapFS{
		"migrations/0001_outbox.pgsql": file(orig),
		"migrations/0002_bad.pgsql":    file("CREATE TABLE broadcaster.half (v integer); SELECT no_such_function();"),
	}
	if _, err := migrate(ctx, e.owner, failing); err == nil {
		t.Fatal("failing migration succeeded")
	}
	if got := e.count(`SELECT count(*) FROM pg_tables WHERE schemaname = 'broadcaster' AND tablename = 'half'`); got != 0 {
		t.Fatal("failing migration left its table")
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.schema_migrations`); got != 1 {
		t.Fatalf("failing migration recorded: %d rows", got)
	}

	next := fstest.MapFS{
		"migrations/0001_outbox.pgsql": file(orig),
		"migrations/0002_next.pgsql":   file("CREATE TABLE broadcaster.next_table (v integer);"),
	}
	if done, err := migrate(ctx, e.owner, next); err != nil || len(done) != 1 || done[0] != 2 {
		t.Fatalf("apply 0002: %v %v", done, err)
	}
	if err := CheckSchema(ctx, e.owner); !errors.Is(err, ErrDatabaseNewer) {
		t.Fatalf("binary older than database: want ErrDatabaseNewer, got %v", err)
	}
	if _, err := Migrate(ctx, e.owner); !errors.Is(err, ErrDatabaseNewer) {
		t.Fatalf("Migrate with an older binary: want ErrDatabaseNewer, got %v", err)
	}

	mustExec(t, e.owner, `INSERT INTO broadcaster.schema_migrations (version, name, sha256) VALUES (4, 'later', $1)`,
		make([]byte, 32))
	if _, err := migrate(ctx, e.owner, next); !errors.Is(err, ErrMigrationGap) {
		t.Fatalf("database gap: want ErrMigrationGap, got %v", err)
	}
}

// TestRuntimeRoleHasNoDDL (plan 13.2): the runtime role cannot create, alter,
// drop or truncate anything, cannot delete rows, cannot create temporary
// tables, and cannot change the columns it only reads.
func TestRuntimeRoleHasNoDDL(t *testing.T) {
	e := newEnv(t)
	for _, q := range []string{
		`CREATE TABLE broadcaster.x (v integer)`,
		`CREATE TABLE public.x (v integer)`,
		`CREATE TEMP TABLE x (v integer)`,
		`CREATE FUNCTION broadcaster.f() RETURNS integer LANGUAGE sql AS 'SELECT 1'`,
		`CREATE SCHEMA other`,
		`ALTER TABLE broadcaster.intents ADD COLUMN x integer`,
		`ALTER TABLE broadcaster.intents DISABLE TRIGGER intents_terminal_immutable`,
		`DROP TABLE broadcaster.transitions`,
		`DROP FUNCTION broadcaster.enqueue_intent(text, jsonb, jsonb, text, interval)`,
		`TRUNCATE broadcaster.intents`,
		`DELETE FROM broadcaster.intents`,
		`DELETE FROM broadcaster.attempts`,
		`DELETE FROM broadcaster.transitions`,
		`UPDATE broadcaster.caller_limits SET open_intent_cap = 4096`,
		`UPDATE broadcaster.signers SET lock_key = 99`,
		`UPDATE broadcaster.business_attempts SET business_id = 'x'`,
	} {
		_, err := e.runtime.ExecContext(e.ctx, q)
		if c := SQLState(err); c != "42501" {
			t.Errorf("%s: want 42501, got %q (%v)", q, c, err)
		}
	}
	// Without the grant option, GRANT only warns and grants nothing.
	mustExec(t, e.runtime, `GRANT SELECT ON broadcaster.intents TO PUBLIC`)
	var granted bool
	if err := e.super.QueryRowContext(e.ctx, `SELECT has_table_privilege('public', 'broadcaster.intents', 'SELECT')`).Scan(&granted); err != nil {
		t.Fatal(err)
	}
	if granted {
		t.Fatal("the runtime role granted SELECT on intents to PUBLIC")
	}
}

// TestRuntimeRoleCannotWriteCallers (plan 13.2): callers, caller caps and the
// chain binding are owner-written; the runtime role cannot write them, cannot
// call register_caller, and cannot call the SQL-path enqueue function.
func TestRuntimeRoleCannotWriteCallers(t *testing.T) {
	e := newEnv(t)
	e.principal("p-one", 4)
	for _, q := range []string{
		`INSERT INTO broadcaster.callers (role_name, principal) VALUES ('svn_broadcaster', 'p-one')`,
		`UPDATE broadcaster.callers SET principal = 'p-one'`,
		`DELETE FROM broadcaster.callers`,
		`INSERT INTO broadcaster.caller_limits (principal, open_intent_cap) VALUES ('p-two', 4096)`,
		`INSERT INTO broadcaster.chain_binding (chain_id) VALUES ('other')`,
		`UPDATE broadcaster.chain_binding SET chain_id = 'other'`,
		`SELECT broadcaster.register_caller('p-two', 4096, NULL)`,
		`SELECT * FROM broadcaster.enqueue_intent('K', '{}'::jsonb, '{}'::jsonb, 'k', interval '1 hour')`,
		`INSERT INTO broadcaster.schema_migrations (version, name, sha256) VALUES (9, 'x', decode(repeat('00', 32), 'hex'))`,
	} {
		_, err := e.runtime.ExecContext(e.ctx, q)
		wantCode(t, err, "42501")
	}
	if err := RegisterCaller(e.ctx, e.runtime, "p-two", 1, ""); SQLState(err) != "42501" {
		t.Fatalf("RegisterCaller as runtime: want 42501, got %v", err)
	}
	if err := BindChain(e.ctx, e.runtime, "other"); SQLState(err) != "42501" {
		t.Fatalf("BindChain as runtime: want 42501, got %v", err)
	}
}

// TestRegisterCallerRefuses: register_caller refuses a bad principal, a cap
// outside [1, 4096], an unknown role, a superuser, the runtime and owner
// roles, and a role already mapped; nothing is granted.
func TestRegisterCallerRefuses(t *testing.T) {
	e := newEnv(t)
	for name, c := range map[string]struct {
		principal string
		cap       int
		role      string
	}{
		"cap zero":      {"p-a", 0, ""},
		"cap high":      {"p-a", 4097, ""},
		"bad principal": {"P A", 1, ""},
		"bad role":      {"p-a", 1, "Bad-Role"},
	} {
		if err := RegisterCaller(e.ctx, e.owner, c.principal, c.cap, c.role); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
	for _, role := range []string{"no_such_role", "svn_super", RuntimeRole, OwnerRole} {
		err := RegisterCaller(e.ctx, e.owner, "p-a", 1, role)
		if !errors.Is(err, ErrInvalid) || SQLState(err) != "SV004" {
			t.Errorf("role %s: want SV004, got %v", role, err)
		}
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.callers`); got != 0 {
		t.Fatalf("%d callers registered by refused calls", got)
	}
	e.caller("p-a", 2)
	var role string
	if err := e.super.QueryRowContext(e.ctx, `SELECT role_name FROM broadcaster.callers WHERE principal = 'p-a'`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	if err := RegisterCaller(e.ctx, e.owner, "p-b", 2, role); err == nil {
		t.Fatal("a role mapped twice")
	}
	if err := RegisterCaller(e.ctx, e.owner, "p-a", 3, ""); err != nil {
		t.Fatalf("cap update: %v", err)
	}
	if got := e.count(`SELECT open_intent_cap FROM broadcaster.caller_limits WHERE principal = 'p-a'`); got != 3 {
		t.Fatalf("cap %d after update, want 3", got)
	}
}

// TestEnqueueFunctionSearchPathAndGrants (plan 13.2): enqueue_intent is
// SECURITY DEFINER owned by the owner role, fixes search_path and
// lock_timeout, and is executable by registered callers only; every function
// of the schema fixes search_path and none is executable by PUBLIC. A caller
// that plants temporary objects named like the schema's tables and
// functions does not change what the function writes.
func TestEnqueueFunctionSearchPathAndGrants(t *testing.T) {
	e := newEnv(t)
	var (
		secdef bool
		owner  string
		config []string
	)
	if err := e.super.QueryRowContext(e.ctx, `SELECT p.prosecdef, pg_get_userbyid(p.proowner), p.proconfig
		FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'broadcaster' AND p.proname = 'enqueue_intent'`).Scan(&secdef, &owner, pq.Array(&config)); err != nil {
		t.Fatal(err)
	}
	if !secdef || owner != OwnerRole {
		t.Fatalf("enqueue_intent prosecdef=%v owner=%s", secdef, owner)
	}
	if strings.Join(config, ";") != "search_path=pg_catalog, pg_temp;lock_timeout=2s" {
		t.Fatalf("enqueue_intent proconfig %q", config)
	}
	var unsafe []string
	rows, err := e.super.QueryContext(e.ctx, `SELECT p.proname FROM pg_proc p JOIN pg_namespace n ON n.oid = p.pronamespace
		WHERE n.nspname = 'broadcaster' AND (p.proconfig IS NULL
			OR NOT EXISTS (SELECT 1 FROM unnest(p.proconfig) c WHERE c = 'search_path=pg_catalog, pg_temp')
			OR has_function_privilege('public', p.oid, 'EXECUTE')
			OR pg_get_userbyid(p.proowner) <> 'svn_broadcaster_owner')`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		unsafe = append(unsafe, n)
	}
	rows.Close()
	if len(unsafe) != 0 {
		t.Fatalf("functions without a fixed search_path, executable by PUBLIC, or not owner-owned: %v", unsafe)
	}

	callerDB := e.caller("p-hijack", 8)
	var role string
	if err := e.super.QueryRowContext(e.ctx, `SELECT role_name FROM broadcaster.callers WHERE principal = 'p-hijack'`).Scan(&role); err != nil {
		t.Fatal(err)
	}
	e.caller("p-other", 8)
	for _, r := range []struct {
		role string
		want bool
	}{{role, true}, {RuntimeRole, false}, {"svn_super", true}} {
		var got bool
		if err := e.super.QueryRowContext(e.ctx, `SELECT has_function_privilege($1,
			'broadcaster.enqueue_intent(text, jsonb, jsonb, text, interval)', 'EXECUTE')`, r.role).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != r.want {
			t.Errorf("EXECUTE for %s = %v, want %v", r.role, got, r.want)
		}
	}

	mustExec(t, e.super, `GRANT TEMPORARY ON DATABASE `+pq.QuoteIdentifier(e.name)+` TO `+pq.QuoteIdentifier(role))
	conn, err := callerDB.Conn(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	for _, q := range []string{
		`CREATE TEMP TABLE callers (role_name text, principal text)`,
		`INSERT INTO pg_temp.callers VALUES (session_user, 'p-other')`,
		`CREATE TEMP TABLE intents (intent_id uuid)`,
		`CREATE TEMP TABLE chain_binding (chain_id text)`,
		`INSERT INTO pg_temp.chain_binding VALUES ('evil-chain')`,
		`CREATE FUNCTION pg_temp.sha256(bytea) RETURNS bytea LANGUAGE sql AS 'SELECT ''\x00''::bytea'`,
		`SET search_path = pg_temp, public`,
	} {
		mustExec(t, conn, q)
	}
	id, created, err := sqlEnqueue(e.ctx, conn, "hijack-1", `{"a":1}`)
	if err != nil || !created {
		t.Fatalf("enqueue with planted temp objects: %v", err)
	}
	var principal, chain string
	var digest []byte
	if err := e.super.QueryRowContext(e.ctx, `SELECT caller_principal, chain_id, sql_digest FROM broadcaster.intents
		WHERE intent_id = $1`, id).Scan(&principal, &chain, &digest); err != nil {
		t.Fatal(err)
	}
	if principal != "p-hijack" || chain != testChainID || len(digest) != 32 {
		t.Fatalf("planted objects changed the row: principal=%s chain=%s digest=%x", principal, chain, digest)
	}
}

// TestCallerSeesOnlyOwnRows (plan 13.2): a caller role reads its own intents
// through caller_intents only, never the base tables; GetIntent and Cancel
// answer ErrNotFound for another caller's intent and change nothing.
func TestCallerSeesOnlyOwnRows(t *testing.T) {
	e := newEnv(t)
	a := e.caller("p-a", 8)
	b := e.caller("p-b", 8)
	idA, _, err := sqlEnqueue(e.ctx, a, "same-key", `{"who":"a"}`)
	if err != nil {
		t.Fatal(err)
	}
	idB, _, err := sqlEnqueue(e.ctx, b, "same-key", `{"who":"b"}`)
	if err != nil {
		t.Fatal(err)
	}
	if idA == idB {
		t.Fatal("two callers share an intent")
	}
	for _, c := range []struct {
		db   *sql.DB
		want string
	}{{a, idA}, {b, idB}} {
		rows, err := c.db.QueryContext(e.ctx, `SELECT intent_id FROM broadcaster.caller_intents`)
		if err != nil {
			t.Fatal(err)
		}
		var ids []string
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				t.Fatal(err)
			}
			ids = append(ids, id)
		}
		rows.Close()
		if len(ids) != 1 || ids[0] != c.want {
			t.Fatalf("caller sees %v, want only %s", ids, c.want)
		}
		for _, q := range []string{
			`SELECT count(*) FROM broadcaster.intents`,
			`SELECT count(*) FROM broadcaster.transitions`,
			`SELECT count(*) FROM broadcaster.callers`,
			`SELECT count(*) FROM broadcaster.attempts`,
		} {
			var n int
			wantCode(t, c.db.QueryRowContext(e.ctx, q).Scan(&n), "42501")
		}
	}
	var cols []string
	rows, err := e.super.QueryContext(e.ctx, `SELECT column_name FROM information_schema.columns
		WHERE table_schema = 'broadcaster' AND table_name = 'caller_intents'`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatal(err)
		}
		cols = append(cols, c)
	}
	rows.Close()
	for _, c := range cols {
		if c == "request" || c == "request_json" || c == "auth_context" || c == "caller_principal" {
			t.Fatalf("caller_intents exposes %s", c)
		}
	}

	signer := e.signer(1)
	e.principal("p-http", 8)
	res := e.enqueue("p-http", "k1", signer, time.Hour)
	if _, err := e.st.GetIntent(e.ctx, "p-a", res.IntentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetIntent of another caller: want ErrNotFound, got %v", err)
	}
	if err := e.st.Cancel(e.ctx, "p-a", res.IntentID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Cancel of another caller: want ErrNotFound, got %v", err)
	}
	if st, _, _ := e.state(res.IntentID); st != StateRequested {
		t.Fatalf("another caller's cancel changed the intent to %s", st)
	}
	got, err := e.st.GetIntent(e.ctx, "p-http", res.IntentID)
	if err != nil || got.State != StateRequested || got.Phase != PhaseQueued || got.BusinessID != res.BusinessID {
		t.Fatalf("own GetIntent: %+v %v", got, err)
	}
	if _, err := e.st.GetIntent(e.ctx, "p-http", "not-a-uuid"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("malformed id: want ErrNotFound, got %v", err)
	}
}

// TestTerminalRowsImmutable (plan 13.2, T17): a FAILED or COMMITTED intent,
// a terminal attempt and a commitment refuse UPDATE and DELETE even from the
// owner role; identity columns of a live intent never change; a result for
// an attempt already DEAD is ErrImmutable and rewrites nothing (8.5).
func TestTerminalRowsImmutable(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 16)
	signer := e.signer(1)
	failed := e.enqueue("p", "failed", signer, time.Hour)
	if err := e.st.Cancel(e.ctx, "p", failed.IntentID); err != nil {
		t.Fatal(err)
	}
	committed := commitOne(t, e, signer, "committed")

	for _, id := range []string{failed.IntentID, committed.intentID} {
		for _, q := range []string{
			`UPDATE broadcaster.intents SET reason = 'X' WHERE intent_id = $1`,
			`UPDATE broadcaster.intents SET transitions_suppressed = transitions_suppressed + 1 WHERE intent_id = $1`,
			`DELETE FROM broadcaster.intents WHERE intent_id = $1`,
		} {
			_, err := e.owner.ExecContext(e.ctx, q, id)
			wantCode(t, err, "SV001")
		}
	}
	for _, q := range []string{
		`UPDATE broadcaster.attempts SET sends_started = sends_started + 1 WHERE attempt_id = $1`,
		`UPDATE broadcaster.attempts SET phase = 'DEAD', dead_reason = 'D3_DEADLINE_PASSED', dead_at = now(),
			committed_height = NULL, committed_index = NULL, committed_code = NULL, committed_codespace = NULL,
			acc_seq_present = NULL, gas_used = NULL WHERE attempt_id = $1`,
		`DELETE FROM broadcaster.attempts WHERE attempt_id = $1`,
	} {
		_, err := e.owner.ExecContext(e.ctx, q, committed.attemptID)
		wantCode(t, err, "SV001")
	}
	for _, q := range []string{
		`UPDATE broadcaster.commitments SET height = height + 1 WHERE intent_id = $1`,
		`DELETE FROM broadcaster.commitments WHERE intent_id = $1`,
	} {
		_, err := e.owner.ExecContext(e.ctx, q, committed.intentID)
		wantCode(t, err, "SV002")
	}
	_, err := e.owner.ExecContext(e.ctx, `TRUNCATE broadcaster.commitments CASCADE`)
	wantCode(t, err, "SV002")
	if err := e.st.FailValidation(e.ctx, failed.IntentID, "INVALID"); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("T2 on a FAILED intent: want ErrStateChanged, got %v", err)
	}

	live := e.enqueue("p", "live", signer, time.Hour)
	for _, q := range []string{
		`UPDATE broadcaster.intents SET kind = 'OTHER_KIND' WHERE intent_id = $1`,
		`UPDATE broadcaster.intents SET expires_at = expires_at + interval '1 day' WHERE intent_id = $1`,
		`UPDATE broadcaster.intents SET auth_context = '{}'::jsonb WHERE intent_id = $1`,
		`UPDATE broadcaster.intents SET request = '\x00'::bytea WHERE intent_id = $1`,
		`UPDATE broadcaster.intents SET validated = false, request = NULL, request_digest = NULL,
			signer_address = NULL, business_id = NULL, business_attempt = NULL WHERE intent_id = $1`,
	} {
		_, err := e.owner.ExecContext(e.ctx, q, live.IntentID)
		wantCode(t, err, "SV001")
	}

	dead := e.enqueue("p", "dead", signer, time.Hour)
	ss := e.lock(signer)
	defer ss.Close()
	// commitOne stored a result at height 42, which raised the signer's
	// observed height; a sequence read must be at least that recent.
	if err := ss.SyncSequence(e.ctx, 7, 1, 42); err != nil {
		t.Fatal(err)
	}
	attemptID := e.persist(ss, dead.IntentID, 1, 100)
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.MarkDead(e.ctx, attemptID, DeadD3DeadlinePassed); err != nil {
		t.Fatal(err)
	}
	if err := tx.RetryWait(e.ctx, dead.IntentID, Expect{StateInFlight, PhaseSigned}, 0, "DEAD_NO_EFFECT"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	err = tx.ResolveCommitted(e.ctx, attemptID, CommittedResult{TxHash: draft(dead.IntentID, 1, 100).TxHash, Height: 50,
		Code: 0, Codespace: "", AccSeqPresent: true})
	if !errors.Is(err, ErrImmutable) {
		t.Fatalf("result for a DEAD attempt: want ErrImmutable, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.attempts WHERE attempt_id = $1 AND phase = 'DEAD'`, attemptID); got != 1 {
		t.Fatal("DEAD attempt rewritten")
	}
}

// TestTransitionsAppendOnly (plan 13.2): transitions refuse UPDATE, DELETE
// and TRUNCATE from the owner role (trigger) and from the runtime role
// (privilege).
func TestTransitionsAppendOnly(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 4)
	signer := e.signer(1)
	res := e.enqueue("p", "k", signer, time.Hour)
	if got := e.count(`SELECT count(*) FROM broadcaster.transitions WHERE intent_id = $1`, res.IntentID); got != 1 {
		t.Fatalf("%d transitions after create", got)
	}
	for _, q := range []string{
		`UPDATE broadcaster.transitions SET reason = 'X'`,
		`DELETE FROM broadcaster.transitions`,
		`TRUNCATE broadcaster.transitions`,
	} {
		_, err := e.owner.ExecContext(e.ctx, q)
		wantCode(t, err, "SV002")
		_, err = e.runtime.ExecContext(e.ctx, q)
		wantCode(t, err, "42501")
	}
	trs, err := e.st.Transitions(e.ctx, res.IntentID)
	if err != nil || len(trs) != 1 || trs[0].Reason != "CREATED" || trs[0].From != nil ||
		trs[0].To != (Expect{StateRequested, PhaseQueued}) || trs[0].WorkerInstance != "test-instance" {
		t.Fatalf("transitions: %+v %v", trs, err)
	}
}

// TestTransitionsBounded (plan 13.2, m11): at most TransitionsPerIntent rows
// per intent; after that, and for a repeated observation class, only
// transitions_suppressed grows.
func TestTransitionsBounded(t *testing.T) {
	e := newEnv(t)
	lim := testLimits()
	lim.TransitionsPerIntent = 16
	st, err := New(e.runtime, lim, "bounded")
	if err != nil {
		t.Fatal(err)
	}
	e.principal("p", 4)
	signer := e.signer(1)
	res := e.enqueue("p", "k", signer, time.Hour)
	ss, err := st.LockSigner(e.ctx, signer)
	if err != nil {
		t.Fatal(err)
	}
	defer ss.Close()
	observe := func(reason string) {
		t.Helper()
		tx, err := ss.Begin(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := tx.RecordObservation(e.ctx, res.IntentID, reason, []byte(`{"poll":1}`)); err != nil {
			t.Fatal(err)
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	observe("NOT_FOUND_YET")
	observe("NOT_FOUND_YET")
	observe("NOT_FOUND_YET")
	if got := e.count(`SELECT count(*) FROM broadcaster.transitions WHERE intent_id = $1`, res.IntentID); got != 2 {
		t.Fatalf("repeated class wrote %d rows, want 2 (create + one)", got)
	}
	for i := range 40 {
		if i%2 == 0 {
			observe("CLASS_A")
		} else {
			observe("CLASS_B")
		}
	}
	var recorded, suppressed int
	if err := e.super.QueryRowContext(e.ctx, `SELECT transitions_recorded, transitions_suppressed FROM broadcaster.intents
		WHERE intent_id = $1`, res.IntentID).Scan(&recorded, &suppressed); err != nil {
		t.Fatal(err)
	}
	rows := e.count(`SELECT count(*) FROM broadcaster.transitions WHERE intent_id = $1`, res.IntentID)
	if rows != 16 || recorded != 16 || suppressed != 2+(42-16) {
		t.Fatalf("rows=%d recorded=%d suppressed=%d", rows, recorded, suppressed)
	}
	if err := st.Cancel(e.ctx, "p", res.IntentID); err != nil {
		t.Fatalf("cancel past the bound: %v", err)
	}
	if st, _, reason := e.state(res.IntentID); st != StateFailed || reason != "CANCELLED" {
		t.Fatalf("cancel past the bound: %s %s", st, reason)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.transitions WHERE intent_id = $1`, res.IntentID); got != 16 {
		t.Fatalf("rows grew past the bound: %d", got)
	}
}

// TestSQLPathErrorsNotLogged (plan 13.2, DR-11-37, m7): SQL-path errors
// raised with literal SQL (cap reached, idempotency conflict, bad JSON, a bad
// interval, a bad idempotency key) and an owner-side unique violation leave
// no trace of their argument values in the server log, while the log does
// record a FATAL login failure, so the check is not vacuous.
func TestSQLPathErrorsNotLogged(t *testing.T) {
	e := newEnv(t)
	const canary = "zz-log-canary-7f3e1b"
	callerDB := e.caller("p-log", 1)
	mustExec(t, callerDB, `SELECT * FROM broadcaster.enqueue_intent('TEST_KIND', '{"v":"first"}'::jsonb,
		'{}'::jsonb, 'log-key', interval '1 hour')`)
	cases := map[string]struct{ q, code string }{
		"cap reached": {`SELECT * FROM broadcaster.enqueue_intent('TEST_KIND', '{"v":"` + canary + `"}'::jsonb,
			'{}'::jsonb, 'other-key', interval '1 hour')`, "SV005"},
		"idempotency conflict": {`SELECT * FROM broadcaster.enqueue_intent('TEST_KIND', '{"v":"` + canary + `"}'::jsonb,
			'{}'::jsonb, 'log-key', interval '1 hour')`, "SV006"},
		"bad json": {`SELECT * FROM broadcaster.enqueue_intent('TEST_KIND', '{"v": ` + canary + `'::jsonb,
			'{}'::jsonb, 'k2', interval '1 hour')`, "22P02"},
		"bad interval": {`SELECT * FROM broadcaster.enqueue_intent('TEST_KIND', '{}'::jsonb,
			'{}'::jsonb, 'k3', interval '` + canary + `')`, "22007"},
		"bad key": {`SELECT * FROM broadcaster.enqueue_intent('TEST_KIND', '{}'::jsonb,
			'{}'::jsonb, 'BAD ` + canary + `', interval '1 hour')`, "SV004"},
	}
	for name, c := range cases {
		_, err := callerDB.ExecContext(e.ctx, c.q)
		if SQLState(err) != c.code {
			t.Errorf("%s: want %s, got %v", name, c.code, err)
		}
	}
	mustExec(t, e.owner, `INSERT INTO broadcaster.caller_limits (principal, open_intent_cap) VALUES ('`+canary+`', 1)`)
	_, err := e.owner.ExecContext(e.ctx, `INSERT INTO broadcaster.caller_limits (principal, open_intent_cap) VALUES ('`+canary+`', 1)`)
	wantCode(t, err, "23505")

	probe, err := OpenDB(connConfig(e.name, "svn_log_probe", "wrong-password"))
	if err != nil {
		t.Fatal(err)
	}
	if err := probe.PingContext(e.ctx); err == nil {
		t.Fatal("login as a missing role succeeded")
	}
	probe.Close()

	deadline := time.Now().Add(10 * time.Second)
	for {
		log, err := os.ReadFile(srv.Cluster.LogPath())
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(log), canary) {
			t.Fatal("server log contains the canary")
		}
		if strings.Contains(string(log), "svn_log_probe") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("server log never recorded the FATAL login failure; the check would be vacuous")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// committedOne is an intent driven to COMMITTED by commitOne.
type committedOne struct {
	intentID  string
	attemptID string
}

// commitOne enqueues, persists, sends, records an ACK, resolves the attempt
// as COMMITTED_OK and commits the intent (T11), as the worker would.
func commitOne(t *testing.T, e *env, signer []byte, key string) committedOne {
	t.Helper()
	ctx := context.Background()
	res := e.enqueue("p", key, signer, time.Hour)
	ss := e.lock(signer)
	defer ss.Close()
	var next int64
	if err := e.super.QueryRowContext(ctx, `SELECT next_sequence FROM broadcaster.signers WHERE address = $1`, signer).Scan(&next); err != nil {
		t.Fatal(err)
	}
	d := draft(res.IntentID, uint64(next), uint64(1000+next))
	persisted, err := ss.PersistAttempt(ctx, d)
	if err != nil || persisted.Refusal != RefusedNone {
		t.Fatalf("persist: %+v %v", persisted, err)
	}
	attemptID := persisted.AttemptID
	if ok, err := ss.BeginSend(ctx, attemptID); err != nil || !ok {
		t.Fatalf("begin send: %v %v", ok, err)
	}
	if ok, err := ss.RecordSendOutcome(ctx, attemptID, SendOutcome{Class: SendAck, Codespace: ""}); err != nil || !ok {
		t.Fatalf("record ack: %v %v", ok, err)
	}
	tx, err := ss.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.ResolveCommitted(ctx, attemptID, CommittedResult{TxHash: d.TxHash, Height: 42, Index: 0, Code: 0,
		AccSeqPresent: true, GasUsed: 1234}); err != nil {
		t.Fatal(err)
	}
	hash := d.TxHash
	if err := tx.CommitIntent(ctx, res.IntentID, Expect{StateInFlight, PhaseAcknowledged}, Commitment{
		Height: 42, BlockHash: [32]byte{1}, TxHash: hash, FirstCommitTxHash: hash, GasWanted: 2_000_000, GasUsed: 1234,
		Events: []byte(`[{"type":"svn.events.v1.Test","index":0}]`), CommittingSigner: signer,
	}, true); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return committedOne{intentID: res.IntentID, attemptID: attemptID}
}
