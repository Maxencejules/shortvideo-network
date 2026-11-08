package devnet_test

// Database access of the broadcaster devnet tests. The test reads the
// outbox with its own role, harness_reader, which dev-db creates as a
// login role and the owner role grants SELECT on the outbox schema and
// nothing else: the test can observe every row but cannot write history.
// The owner connection is used once, at setup, for that grant and for the
// app_fixture schema of the SQL path; the SQL path writes through the
// caller role app_fixture_sql, which may only call
// broadcaster.enqueue_intent and insert into its own application table.

import (
	"context"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/lib/pq"
)

// bcOpenDB opens role's connection to the dev-db cluster with an explicit
// lib/pq configuration (no PG* variable, no password file lookup).
func bcOpenDB(t *testing.T, info bcDevDB, role, passwordFile string) *sql.DB {
	t.Helper()
	raw, err := os.ReadFile(passwordFile)
	if err != nil {
		t.Fatal(err)
	}
	connector, err := pq.NewConnectorConfig(pq.Config{
		Host: info.SocketDir, Port: uint16(info.Port), Database: info.Database, User: role,
		Password: strings.TrimSpace(string(raw)), ApplicationName: "devnet-test-" + strings.ReplaceAll(role, "_", "-"),
		SSLMode: pq.SSLModeDisable, ClientEncoding: "UTF8", Datestyle: "ISO, MDY",
		MinProtocolVersion: pq.ProtocolVersion30, MaxProtocolVersion: pq.ProtocolVersion30,
		ConnectTimeout: 10 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(4)
	db.SetConnMaxIdleTime(5 * time.Second)
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// bcCtx is the bound of one test database statement.
func bcCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// bcIntentRow is one outbox intent as the reader role sees it.
type bcIntentRow struct {
	ID              string
	Caller          string
	Kind            string
	Entry           string
	State           string
	Phase           string
	Reason          string
	Attempts        int
	BusinessID      string
	BusinessAttempt int
	CommittedHash   []byte
	Own             sql.NullBool
	FailureCode     sql.NullInt64
	FailureSpace    string
	Signer          []byte
	Validated       bool
}

func (r bcIntentRow) terminal() bool { return r.State == "COMMITTED" || r.State == "FAILED" }

// String is the row's one-line description for the test log.
func (r bcIntentRow) String() string {
	return fmt.Sprintf("intent=%s kind=%s state=%s phase=%s reason=%s attempts=%d business=%s/%d committed=%X",
		r.ID, r.Kind, r.State, r.Phase, r.Reason, r.Attempts, r.BusinessID, r.BusinessAttempt, r.CommittedHash)
}

const bcIntentColumns = `intent_id::text, caller_principal, kind, entry, state, coalesce(phase, ''), coalesce(reason, ''),
	attempt_count, coalesce(business_id, ''), coalesce(business_attempt, 0), committed_tx_hash, own_transaction,
	failure_code, coalesce(failure_codespace, ''), signer_address, validated`

func scanIntent(row interface{ Scan(...any) error }) (bcIntentRow, error) {
	var r bcIntentRow
	err := row.Scan(&r.ID, &r.Caller, &r.Kind, &r.Entry, &r.State, &r.Phase, &r.Reason, &r.Attempts, &r.BusinessID,
		&r.BusinessAttempt, &r.CommittedHash, &r.Own, &r.FailureCode, &r.FailureSpace, &r.Signer, &r.Validated)
	return r, err
}

// intentRow reads one intent.
func (n *bnet) intentRow(id string) bcIntentRow {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	r, err := scanIntent(n.reader.QueryRowContext(ctx, `SELECT `+bcIntentColumns+` FROM broadcaster.intents WHERE intent_id = $1`, id))
	if err != nil {
		n.t.Fatalf("intent %s: %v", id, err)
	}
	return r
}

// allIntents reads every intent, oldest first.
func (n *bnet) allIntents() []bcIntentRow {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	rows, err := n.reader.QueryContext(ctx, `SELECT `+bcIntentColumns+` FROM broadcaster.intents ORDER BY created_at, intent_id`)
	if err != nil {
		n.t.Fatal(err)
	}
	defer rows.Close()
	var out []bcIntentRow
	for rows.Next() {
		r, err := scanIntent(rows)
		if err != nil {
			n.t.Fatal(err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		n.t.Fatal(err)
	}
	return out
}

// bcAttemptRow is one attempt.
type bcAttemptRow struct {
	ID              string
	Intent          string
	N               int
	BusinessN       int
	Signer          []byte
	Sequence        int64
	TimeoutHeight   int64
	Hash            []byte
	Phase           string
	SendsStarted    int
	SendsRecorded   int
	UnknownSends    int
	CacheAnswers    int
	CountedRefusals int
	LastOutcome     string
	AckCode         sql.NullInt64
	AckSpace        string
	CommittedHeight sql.NullInt64
	CommittedCode   sql.NullInt64
	CommittedSpace  string
	DeadReason      string
	Worker          string
}

func (a bcAttemptRow) live() bool { return a.DeadReason == "" && !a.CommittedHeight.Valid }

func (a bcAttemptRow) String() string {
	return fmt.Sprintf("attempt=%d seq=%d timeout=%d hash=%X phase=%s sends=%d/%d unknown=%d cache=%d refusals=%d last=%s ack=%s/%d committed=%d:%s/%d dead=%s worker=%s",
		a.N, a.Sequence, a.TimeoutHeight, a.Hash, a.Phase, a.SendsStarted, a.SendsRecorded, a.UnknownSends, a.CacheAnswers,
		a.CountedRefusals, a.LastOutcome, a.AckSpace, a.AckCode.Int64, a.CommittedHeight.Int64, a.CommittedSpace,
		a.CommittedCode.Int64, a.DeadReason, a.Worker)
}

const bcAttemptColumns = `attempt_id::text, intent_id::text, n, business_n, signer_address, sequence, timeout_height, tx_hash, phase,
	sends_started, sends_recorded, unknown_sends, cache_answers, counted_refusals, coalesce(last_outcome, ''), ack_code,
	coalesce(ack_codespace, ''), committed_height, committed_code, coalesce(committed_codespace, ''),
	coalesce(dead_reason, ''), worker_instance`

func (n *bnet) attemptsWhere(where string, args ...any) []bcAttemptRow {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	rows, err := n.reader.QueryContext(ctx, `SELECT `+bcAttemptColumns+` FROM broadcaster.attempts WHERE `+where+` ORDER BY created_at, n`, args...)
	if err != nil {
		n.t.Fatal(err)
	}
	defer rows.Close()
	var out []bcAttemptRow
	for rows.Next() {
		var a bcAttemptRow
		if err := rows.Scan(&a.ID, &a.Intent, &a.N, &a.BusinessN, &a.Signer, &a.Sequence, &a.TimeoutHeight, &a.Hash, &a.Phase,
			&a.SendsStarted, &a.SendsRecorded, &a.UnknownSends, &a.CacheAnswers, &a.CountedRefusals, &a.LastOutcome,
			&a.AckCode, &a.AckSpace, &a.CommittedHeight, &a.CommittedCode, &a.CommittedSpace, &a.DeadReason, &a.Worker); err != nil {
			n.t.Fatal(err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		n.t.Fatal(err)
	}
	return out
}

// attempts reads one intent's attempts in order.
func (n *bnet) attempts(intentID string) []bcAttemptRow {
	n.t.Helper()
	return n.attemptsWhere(`intent_id = $1`, intentID)
}

// bcCommitmentRow is one intent's canonical commitment evidence.
type bcCommitmentRow struct {
	ChainID      string
	BusinessID   string
	Height       int64
	BlockHash    []byte
	TxHash       []byte
	FirstCommit  []byte
	TxIndex      int64
	Code         int64
	Idempotent   bool
	CommitSigner []byte
}

// commitment reads an intent's commitment row; ok is false without one.
func (n *bnet) commitment(intentID string) (bcCommitmentRow, bool) {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	var c bcCommitmentRow
	err := n.reader.QueryRowContext(ctx, `SELECT chain_id, business_id, height, block_hash, tx_hash, first_commit_tx_hash,
		tx_index, code, idempotent_replay, committing_signer FROM broadcaster.commitments WHERE intent_id = $1`, intentID).
		Scan(&c.ChainID, &c.BusinessID, &c.Height, &c.BlockHash, &c.TxHash, &c.FirstCommit, &c.TxIndex, &c.Code, &c.Idempotent, &c.CommitSigner)
	if err == sql.ErrNoRows {
		return bcCommitmentRow{}, false
	}
	if err != nil {
		n.t.Fatal(err)
	}
	return c, true
}

// bcTransition is one transitions row.
type bcTransition struct {
	ID          int64
	Intent      string
	Attempt     sql.NullString
	From        string
	To          string
	Reason      string
	Observation string
	Worker      string
}

// transitionsAfter reads every transitions row with an id above after.
func (n *bnet) transitionsAfter(after int64) []bcTransition {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	rows, err := n.reader.QueryContext(ctx, `SELECT id, intent_id::text, attempt_id::text,
		coalesce(from_state, '') || '/' || coalesce(from_phase, ''), to_state || '/' || coalesce(to_phase, ''), reason,
		coalesce(observation::text, ''), worker_instance FROM broadcaster.transitions WHERE id > $1 ORDER BY id`, after)
	if err != nil {
		n.t.Fatal(err)
	}
	defer rows.Close()
	var out []bcTransition
	for rows.Next() {
		var tr bcTransition
		if err := rows.Scan(&tr.ID, &tr.Intent, &tr.Attempt, &tr.From, &tr.To, &tr.Reason, &tr.Observation, &tr.Worker); err != nil {
			n.t.Fatal(err)
		}
		out = append(out, tr)
	}
	if err := rows.Err(); err != nil {
		n.t.Fatal(err)
	}
	return out
}

// transitionsOf reads one intent's transitions rows.
func (n *bnet) transitionsOf(intentID string) []bcTransition {
	n.t.Helper()
	var out []bcTransition
	for _, tr := range n.transitionsAfter(0) {
		if tr.Intent == intentID {
			out = append(out, tr)
		}
	}
	return out
}

// bcSignerRow is one signers row.
type bcSignerRow struct {
	Address        []byte
	Class          string
	AccountNumber  sql.NullInt64
	NextSequence   sql.NullInt64
	ObservedHeight int64
	Halted         string
}

// signerRow reads the signers row of address.
func (n *bnet) signerRow(address []byte) bcSignerRow {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	var s bcSignerRow
	err := n.reader.QueryRowContext(ctx, `SELECT address, coalesce(class, ''), account_number, next_sequence, observed_height,
		coalesce(halted_reason, '') FROM broadcaster.signers WHERE address = $1`, address).
		Scan(&s.Address, &s.Class, &s.AccountNumber, &s.NextSequence, &s.ObservedHeight, &s.Halted)
	if err != nil {
		n.t.Fatalf("signer %X: %v", address, err)
	}
	return s
}

// bcSignerEvent is one recorded foreign use of a signer sequence.
type bcSignerEvent struct {
	Signer   []byte
	Sequence int64
	Foreign  []byte
	Cleared  bool
}

// signerEvents reads every signer_events row.
func (n *bnet) signerEvents() []bcSignerEvent {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	rows, err := n.reader.QueryContext(ctx, `SELECT signer_address, sequence, foreign_tx_hash, cleared_at IS NOT NULL
		FROM broadcaster.signer_events ORDER BY id`)
	if err != nil {
		n.t.Fatal(err)
	}
	defer rows.Close()
	var out []bcSignerEvent
	for rows.Next() {
		var e bcSignerEvent
		if err := rows.Scan(&e.Signer, &e.Sequence, &e.Foreign, &e.Cleared); err != nil {
			n.t.Fatal(err)
		}
		out = append(out, e)
	}
	if err := rows.Err(); err != nil {
		n.t.Fatal(err)
	}
	return out
}

// openIntentsByCaller counts the non-terminal intents of each caller.
func (n *bnet) openIntentsByCaller() map[string]int {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	rows, err := n.reader.QueryContext(ctx, `SELECT caller_principal, count(*) FROM broadcaster.intents
		WHERE state IN ('REQUESTED', 'IN_FLIGHT') GROUP BY caller_principal`)
	if err != nil {
		n.t.Fatal(err)
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var caller string
		var count int
		if err := rows.Scan(&caller, &count); err != nil {
			n.t.Fatal(err)
		}
		out[caller] = count
	}
	if err := rows.Err(); err != nil {
		n.t.Fatal(err)
	}
	return out
}

// contentOwners reads every ownership row of contentID as status:caller.
func (n *bnet) contentOwners(contentID string) []string {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	rows, err := n.reader.QueryContext(ctx, `SELECT status, caller_principal FROM broadcaster.content_owners
		WHERE content_id = $1 ORDER BY id`, contentID)
	if err != nil {
		n.t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var status, caller string
		if err := rows.Scan(&status, &caller); err != nil {
			n.t.Fatal(err)
		}
		out = append(out, status+":"+caller)
	}
	if err := rows.Err(); err != nil {
		n.t.Fatal(err)
	}
	return out
}

// openStallEpisode returns the open stall episode's reason and start
// height; ok is false when none is open.
func (n *bnet) openStallEpisode() (string, int64, bool) {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	var reason string
	var height int64
	err := n.reader.QueryRowContext(ctx, `SELECT reason, started_height FROM broadcaster.stall_episodes WHERE ended_at IS NULL`).
		Scan(&reason, &height)
	if err == sql.ErrNoRows {
		return "", 0, false
	}
	if err != nil {
		n.t.Fatal(err)
	}
	return reason, height, true
}

// readerCannotWrite checks that the harness role is SELECT-only: an
// update of an intent row is refused with insufficient_privilege.
func (n *bnet) readerCannotWrite() {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	_, err := n.reader.ExecContext(ctx, `UPDATE broadcaster.intents SET reason = reason WHERE false`)
	var pqErr *pq.Error
	if err == nil || !errors.As(err, &pqErr) || pqErr.Code != "42501" {
		n.t.Fatalf("the harness role could write the outbox: %v", err)
	}
	fmt.Println("harness_role=harness_reader select_only=true (an UPDATE is refused with 42501)")
}

// hexOf is the upper-case hex of b, as the test log prints hashes.
func hexOf(b []byte) string { return strings.ToUpper(hex.EncodeToString(b)) }
