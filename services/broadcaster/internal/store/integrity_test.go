package store

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// TestSignerConflictClearedIsNotHaltedAgain (plan 8.1, DR-11-15): once an
// operator cleared a foreign use of a sequence, detecting the same hash at
// that sequence again is ErrConflictCleared and leaves the signer running,
// because no uncleared event would let ClearConflict lift that halt. Halt
// refuses SIGNER_CONFLICT, which only HaltForConflict sets with its event.
// Conflict reports NONE, OPEN and CLEARED.
func TestSignerConflictClearedIsNotHaltedAgain(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	foreign := sha256.Sum256([]byte("foreign transaction"))
	status := func(seq uint64, want ConflictStatus) {
		t.Helper()
		got, err := e.st.Conflict(e.ctx, signer, seq, foreign)
		if err != nil || got != want {
			t.Fatalf("conflict at %d: %q %v, want %q", seq, got, err, want)
		}
	}
	status(3, ConflictNone)
	if err := ss.HaltForConflict(e.ctx, 3, foreign); err != nil {
		t.Fatal(err)
	}
	status(3, ConflictOpen)
	if err := ss.ClearConflict(e.ctx, 3, foreign, "signer-clear"); err != nil {
		t.Fatal(err)
	}
	status(3, ConflictCleared)
	if err := ss.HaltForConflict(e.ctx, 3, foreign); !errors.Is(err, ErrConflictCleared) {
		t.Fatalf("cleared event detected again: want ErrConflictCleared, got %v", err)
	}
	if _, halted := e.signerState(signer); halted != "" {
		t.Fatalf("halted %q for an event an operator cleared", halted)
	}
	if err := ss.Halt(e.ctx, "SIGNER_CONFLICT"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("SIGNER_CONFLICT without an event: want ErrInvalid, got %v", err)
	}
	if _, halted := e.signerState(signer); halted != "" {
		t.Fatalf("halted %q by Halt", halted)
	}
	in := e.enqueue("p", "after-clear", signer, time.Hour)
	e.persist(ss, in.IntentID, 0, 100)

	// The same hash at another sequence is another event: it halts, and its
	// own clear lifts the halt.
	if err := ss.HaltForConflict(e.ctx, 4, foreign); err != nil {
		t.Fatal(err)
	}
	status(4, ConflictOpen)
	if _, halted := e.signerState(signer); halted != "SIGNER_CONFLICT" {
		t.Fatalf("halt reason %q", halted)
	}
	if err := ss.ClearConflict(e.ctx, 4, foreign, "signer-clear"); err != nil {
		t.Fatal(err)
	}
	if _, halted := e.signerState(signer); halted != "" {
		t.Fatalf("still halted %q", halted)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.signer_events`); got != 2 {
		t.Fatalf("%d signer events, want 2", got)
	}
	if _, err := e.st.Conflict(e.ctx, signer[:19], 3, foreign); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short signer address: want ErrInvalid, got %v", err)
	}
}

// TestResolveBindsHashAndObservedHeight (plan 8.4, 8.1 m2): a committed
// result is stored only on the attempt whose tx_hash it was read for, so the
// result of another attempt at the same signer and sequence is ErrInvalid
// with nothing written; a stored result raises the signer's observed height,
// so a sequence read answered below the committed height is
// ErrStaleObservation.
func TestResolveBindsHashAndObservedHeight(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	in := e.enqueue("p", "hash", signer, time.Hour)
	att := e.persist(ss, in.IntentID, 0, 100)
	sibling := draft(in.IntentID, 0, 101).TxHash

	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	err = tx.ResolveCommitted(e.ctx, att, CommittedResult{TxHash: sibling, Height: 77, Code: 9, Codespace: "svn", AccSeqPresent: true})
	if !errors.Is(err, ErrInvalid) {
		t.Fatalf("result of another transaction: want ErrInvalid, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if atts, err := e.st.Attempts(e.ctx, in.IntentID); err != nil || atts[0].Phase != AttemptSigned {
		t.Fatalf("attempt after a refused resolve: %+v %v", atts, err)
	}

	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.ResolveCommitted(e.ctx, att, CommittedResult{TxHash: draft(in.IntentID, 0, 100).TxHash, Height: 77, Code: 9,
		Codespace: "svn", AccSeqPresent: true}); err != nil {
		t.Fatal(err)
	}
	if err := tx.RetryWait(e.ctx, in.IntentID, Expect{StateInFlight, PhaseSigned}, 0, "EXECUTION_FAILED_RETRY"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got := e.count(`SELECT observed_height FROM broadcaster.signers WHERE address = $1`, signer); got != 77 {
		t.Fatalf("observed height %d, want 77", got)
	}
	if err := ss.SyncSequence(e.ctx, 7, 0, 76); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("sync below the committed height: want ErrStaleObservation, got %v", err)
	}
	if err := ss.SyncSequence(e.ctx, 7, 1, 77); err != nil {
		t.Fatalf("sync at the committed height: %v", err)
	}
}

// TestEvidenceAgreesWithAttempts (plan 9.3, 9.4, 8.5, M2): at commit the
// database refuses an intent that is not COMMITTED while one of its attempts
// is resolved COMMITTED_OK, a COMMITTED intent whose canonical hash is an
// attempt of its own that is DEAD, an own_transaction that is true for a
// hash none of its attempts resolved COMMITTED_OK or false for one that did,
// and an idempotent replay read from an attempt not resolved
// COMMITTED_IDEMPOTENT. A COMMITTED intent keeps no reason.
func TestEvidenceAgreesWithAttempts(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	in := e.enqueue("p", "evidence", signer, time.Hour)
	att := e.persist(ss, in.IntentID, 0, 100)
	own := draft(in.IntentID, 0, 100).TxHash
	foreign := sha256.Sum256([]byte("first commit by another account"))
	inFlight := Expect{StateInFlight, PhaseSigned}
	events := json.RawMessage(`[{"index":0,"type":"svn.events.v1.Test"}]`)
	commitment := func(txHash, first [32]byte) Commitment {
		return Commitment{Height: 9, BlockHash: [32]byte{1}, TxHash: txHash, FirstCommitTxHash: first, GasWanted: 1,
			GasUsed: 1, Events: events, CommittingSigner: signer}
	}
	ok := CommittedResult{TxHash: own, Height: 9, Code: 0, AccSeqPresent: true}
	replay := CommittedResult{TxHash: own, Height: 9, Code: 0, AccSeqPresent: true, Idempotent: true}

	refused := map[string]func(*SignerTx) error{
		"committed attempt, intent FAILED": func(tx *SignerTx) error {
			if err := tx.ResolveCommitted(e.ctx, att, ok); err != nil {
				return err
			}
			return tx.FailIntent(e.ctx, in.IntentID, inFlight, "OBJECT_TAKEN", nil)
		},
		"committed attempt, intent RETRY_WAIT": func(tx *SignerTx) error {
			if err := tx.ResolveCommitted(e.ctx, att, ok); err != nil {
				return err
			}
			return tx.RetryWait(e.ctx, in.IntentID, inFlight, 0, "RETRY")
		},
		"replayed attempt, intent FAILED": func(tx *SignerTx) error {
			if err := tx.ResolveCommitted(e.ctx, att, replay); err != nil {
				return err
			}
			return tx.FailIntent(e.ctx, in.IntentID, inFlight, "OBJECT_TAKEN", nil)
		},
		"canonical is our DEAD attempt": func(tx *SignerTx) error {
			if err := tx.MarkDead(e.ctx, att, DeadD3SequenceUsed); err != nil {
				return err
			}
			return tx.CommitIntent(e.ctx, in.IntentID, inFlight, commitment(own, own), false)
		},
		"own_transaction for a foreign hash": func(tx *SignerTx) error {
			if err := tx.MarkDead(e.ctx, att, DeadD3SequenceUsed); err != nil {
				return err
			}
			return tx.CommitIntent(e.ctx, in.IntentID, inFlight, commitment(foreign, foreign), true)
		},
		"own commitment reported as someone else's": func(tx *SignerTx) error {
			if err := tx.ResolveCommitted(e.ctx, att, ok); err != nil {
				return err
			}
			return tx.CommitIntent(e.ctx, in.IntentID, inFlight, commitment(own, own), false)
		},
		"replay read from a DEAD attempt": func(tx *SignerTx) error {
			if err := tx.MarkDead(e.ctx, att, DeadD3SequenceUsed); err != nil {
				return err
			}
			return tx.CommitIntent(e.ctx, in.IntentID, inFlight, commitment(own, foreign), false)
		},
		"replay read from a committed attempt": func(tx *SignerTx) error {
			if err := tx.ResolveCommitted(e.ctx, att, ok); err != nil {
				return err
			}
			return tx.CommitIntent(e.ctx, in.IntentID, inFlight, commitment(own, foreign), false)
		},
	}
	for name, steps := range refused {
		tx, err := ss.Begin(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		if err := steps(tx); err != nil {
			t.Fatalf("%s: step: %v", name, err)
		}
		if err := tx.Commit(); !errors.Is(err, ErrEvidence) {
			t.Fatalf("%s: want ErrEvidence at commit, got %v", name, err)
		}
		if st, phase, _ := e.state(in.IntentID); st != StateInFlight || phase != PhaseSigned {
			t.Fatalf("%s: intent %s/%s after a refused commit", name, st, phase)
		}
	}

	// T13/T14 then T5: the attempt dies, the intent waits with a reason, and
	// the effect check finds another account's first commitment.
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.MarkDead(e.ctx, att, DeadD3SequenceUsed); err != nil {
		t.Fatal(err)
	}
	if err := tx.RetryWait(e.ctx, in.IntentID, inFlight, 0, "DEAD_NO_EFFECT"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if _, _, reason := e.state(in.IntentID); reason != "DEAD_NO_EFFECT" {
		t.Fatalf("RETRY_WAIT reason %q", reason)
	}
	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.CommitIntent(e.ctx, in.IntentID, Expect{StateRequested, PhaseRetryWait}, commitment(foreign, foreign), false); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("T5 by another account's commitment: %v", err)
	}
	if st, _, reason := e.state(in.IntentID); st != StateCommitted || reason != "" {
		t.Fatalf("COMMITTED intent %s keeps reason %q", st, reason)
	}
	// The column CHECK itself, with the terminal trigger disabled.
	stx, err := e.super.BeginTx(e.ctx, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer stx.Rollback()
	mustExec(t, stx, `SET LOCAL session_replication_role = replica`)
	_, err = stx.ExecContext(e.ctx, `UPDATE broadcaster.intents SET reason = 'RETRY' WHERE intent_id = $1`, in.IntentID)
	wantCode(t, err, "23514")
}

// TestSQLReplayNeedsSameKindAndContext (plan 5.4, 6.3): an SQL-path replay
// of an idempotency key returns the existing intent only when the request,
// the kind and the authorization context are the same (key order aside);
// another kind or another context is SV006, as the HTTP path answers 409.
func TestSQLReplayNeedsSameKindAndContext(t *testing.T) {
	e := newEnv(t)
	callerDB := e.caller("p-replay", 8)
	enq := func(kind, auth string) (string, bool, error) {
		var (
			id      string
			created bool
		)
		err := callerDB.QueryRowContext(e.ctx, `SELECT intent_id, created FROM broadcaster.enqueue_intent($1, $2::jsonb,
			$3::jsonb, $4, $5::interval)`, kind, `{"a":1}`, auth, "replay-key", "1 hour").Scan(&id, &created)
		return id, created, err
	}
	id, created, err := enq(KindOfferLease, `{"principal_kind":"DEV_FIXTURE","principal_id":"p1"}`)
	if err != nil || !created {
		t.Fatalf("first enqueue: %v %v", created, err)
	}
	again, created, err := enq(KindOfferLease, `{"principal_id":"p1","principal_kind":"DEV_FIXTURE"}`)
	if err != nil || created || again != id {
		t.Fatalf("replay with reordered context keys: %s %v %v", again, created, err)
	}
	_, _, err = enq(KindWithdrawContent, `{"principal_kind":"DEV_FIXTURE","principal_id":"p1"}`)
	wantCode(t, err, "SV006")
	_, _, err = enq(KindOfferLease, `{"principal_kind":"DEV_FIXTURE","principal_id":"p2"}`)
	wantCode(t, err, "SV006")
	if got := e.count(`SELECT count(*) FROM broadcaster.intents`); got != 1 {
		t.Fatalf("%d intents", got)
	}
}

// TestHTTPAuthContextDatabaseChecks: an HTTP-path auth context that passes
// the raw-length and JSON-object check but that the database refuses (a jsonb
// text beyond the 8,192-byte column CHECK, a \u0000 escape, a number outside
// the numeric range) is ErrInvalid, and nothing is written.
func TestHTTPAuthContextDatabaseChecks(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	for name, c := range map[string]struct {
		ctx   string
		state string
	}{
		"jsonb text above the CHECK": {`{"n":1e9000}`, "23514"},
		"NUL escape":                 {`{"a":"\u0000"}`, "22P05"},
		"number out of range":        {`{"n":1e200000}`, "22003"},
	} {
		req, digest := request(name)
		_, err := e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: "p", IdempotencyKey: "auth-check", Kind: KindOfferLease, Object: testOffer,
			Request: req, RequestDigest: digest, AuthContext: json.RawMessage(c.ctx), SignerAddress: signer, ExpiresIn: time.Hour})
		if !errors.Is(err, ErrInvalid) || SQLState(err) != c.state {
			t.Errorf("%s: want ErrInvalid with SQLSTATE %s, got %q %v", name, c.state, SQLState(err), err)
		}
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.intents`); got != 0 {
		t.Fatalf("%d intents written", got)
	}
}

// TestCapSumAndStallBound (plan section 10, 11): the open-intent caps of all
// principals sum to at most 16,384, the most intents one stall transition
// marks; with more non-terminal intents than that (caps lowered below the
// intents already open), OpenStall is ErrOpenIntentsBound and writes nothing
// rather than leaving intents without their STALL_OPENED row.
func TestCapSumAndStallBound(t *testing.T) {
	e := newEnv(t)
	for _, p := range []string{"p-1", "p-2", "p-3", "p-4"} {
		e.principal(p, 4096)
	}
	if err := RegisterCaller(e.ctx, e.owner, "p-5", 1, ""); !errors.Is(err, ErrInvalid) || SQLState(err) != "SV004" {
		t.Fatalf("cap sum above 16,384: want SV004, got %v", err)
	}
	if err := RegisterCaller(e.ctx, e.owner, "p-1", 4096, ""); err != nil {
		t.Fatalf("cap update at the sum bound: %v", err)
	}
	e.principal("p-1", 4095)
	e.principal("p-5", 1)
	if got := e.count(`SELECT sum(open_intent_cap)::int FROM broadcaster.caller_limits`); got != 16384 {
		t.Fatalf("cap sum %d", got)
	}

	mustExec(t, e.super, `INSERT INTO broadcaster.intents (caller_principal, idempotency_key, kind, entry, sql_digest,
			request_json, auth_context, validated, chain_id, state, phase, expires_at, next_attempt_at)
		SELECT 'p-1', 'k' || g, 'OFFER_LEASE', 'SQL', sha256(int4send(g)), '{}'::jsonb, '{}'::jsonb, false, b.chain_id,
			'REQUESTED', 'QUEUED', clock_timestamp() + interval '1 hour', clock_timestamp()
		FROM generate_series(1, $1::int) g, broadcaster.chain_binding b`, maxStallIntents+1)
	if _, err := e.st.OpenStall(e.ctx, StallHeight, 10); !errors.Is(err, ErrOpenIntentsBound) {
		t.Fatalf("stall over %d open intents: want ErrOpenIntentsBound, got %v", maxStallIntents+1, err)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.stall_episodes`); got != 0 {
		t.Fatalf("%d stall episodes written", got)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.transitions WHERE reason = 'STALL_OPENED'`); got != 0 {
		t.Fatalf("%d STALL_OPENED rows written", got)
	}
}
