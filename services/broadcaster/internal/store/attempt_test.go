package store

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// signerState reads a signer's next sequence and halt reason.
func (e *env) signerState(addr []byte) (int64, string) {
	e.t.Helper()
	var next int64
	var halted *string
	if err := e.super.QueryRowContext(e.ctx, `SELECT next_sequence, halted_reason FROM broadcaster.signers WHERE address = $1`,
		addr).Scan(&next, &halted); err != nil {
		e.t.Fatal(err)
	}
	if halted == nil {
		return next, ""
	}
	return next, *halted
}

// killAttempt marks a live attempt dead by (d3) and moves its intent to
// RETRY_WAIT in one SignerTx, as reconciliation would after finding no
// effect.
func killAttempt(t *testing.T, ss *SignerSession, intentID, attemptID string, from Expect) {
	t.Helper()
	ctx := context.Background()
	tx, err := ss.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.MarkDead(ctx, attemptID, DeadD3DeadlinePassed); err != nil {
		t.Fatal(err)
	}
	if err := tx.RetryWait(ctx, intentID, from, 0, "DEAD_NO_EFFECT"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
}

// TestPersistAttemptConditions (plan 8.3 step 1): each failed condition
// refuses the draft and writes nothing; a malformed draft is ErrInvalid.
func TestPersistAttemptConditions(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 64)
	a := e.signer(1)
	b := e.signer(2)
	ssA := e.lock(a)
	defer ssA.Close()
	ssB := e.lock(b)
	defer ssB.Close()

	written := func() int {
		return e.count(`SELECT count(*) FROM broadcaster.attempts`) + e.count(`SELECT coalesce(sum(next_sequence), 0) FROM broadcaster.signers`)
	}
	refused := func(ss *SignerSession, d AttemptDraft, want PersistRefusal) {
		t.Helper()
		before := written()
		res, err := ss.PersistAttempt(e.ctx, d)
		if err != nil || res.Refusal != want || res.AttemptID != "" {
			t.Fatalf("want refusal %s, got %+v %v", want, res, err)
		}
		if written() != before {
			t.Fatalf("refusal %s wrote rows", want)
		}
	}

	i1 := e.enqueue("p", "i1", a, time.Hour)
	refused(ssB, draft(i1.IntentID, 0, 100), RefusedWrongSigner)
	refused(ssA, draft(i1.IntentID, 0, 100).withAccount(8), RefusedAccountMismatch)
	refused(ssA, draft(i1.IntentID, 1, 100), RefusedSequenceMoved)
	att1 := e.persist(ssA, i1.IntentID, 0, 100)
	if next, _ := e.signerState(a); next != 1 {
		t.Fatalf("next_sequence %d after persist, want 1", next)
	}
	refused(ssA, draft(i1.IntentID, 1, 101), RefusedNotRequested)
	i2 := e.enqueue("p", "i2", a, time.Hour)
	refused(ssA, draft(i2.IntentID, 1, 100), RefusedSignerBusy)
	killAttempt(t, ssA, i1.IntentID, att1, Expect{StateInFlight, PhaseSigned})
	if err := ssA.SyncSequence(e.ctx, 7, 0, 5); err != nil {
		t.Fatal(err)
	}
	refused(ssA, draft(i1.IntentID, 0, 100), RefusedTimeoutNotAbove)
	refused(ssA, draft(i1.IntentID, 0, 99), RefusedTimeoutNotAbove)

	if _, err := e.st.OpenStall(e.ctx, StallHeight, 5); err != nil {
		t.Fatal(err)
	}
	refused(ssA, draft(i1.IntentID, 0, 101), RefusedStalled)
	if ok, err := e.st.CloseStall(e.ctx, 6); err != nil || !ok {
		t.Fatalf("close stall: %v %v", ok, err)
	}
	if err := ssA.Halt(e.ctx, "INTEGRITY_ALERT"); err != nil {
		t.Fatal(err)
	}
	refused(ssA, draft(i1.IntentID, 0, 101), RefusedSignerHalted)

	c := e.signer(3)
	ssC := e.lock(c)
	defer ssC.Close()
	unsynced := make([]byte, 20)
	unsynced[0] = 0xbb
	if err := e.st.RegisterSigner(e.ctx, SignerKey{Address: unsynced, Class: ClassService, KeyName: "unsynced",
		LockKey: 4}); err != nil {
		t.Fatal(err)
	}
	ssU := e.lock(unsynced)
	defer ssU.Close()
	iu := e.enqueue("p", "iu", unsynced, time.Hour)
	refused(ssU, draft(iu.IntentID, 0, 100), RefusedSequenceUnknown)

	callerDB := e.caller("p-sql", 4)
	sqlID, _, err := sqlEnqueue(e.ctx, callerDB, "unvalidated", `{}`)
	if err != nil {
		t.Fatal(err)
	}
	refused(ssC, draft(sqlID, 0, 100), RefusedNotValidated)

	lim := testLimits()
	lim.AttemptsPerIntent = 1
	capped, err := New(e.runtime, lim, "capped")
	if err != nil {
		t.Fatal(err)
	}
	ic := e.enqueue("p", "ic", c, time.Hour)
	attC := e.persist(ssC, ic.IntentID, 0, 100)
	killAttempt(t, ssC, ic.IntentID, attC, Expect{StateInFlight, PhaseSigned})
	if err := ssC.SyncSequence(e.ctx, 7, 0, 5); err != nil {
		t.Fatal(err)
	}
	ssCapped, err := capped.LockSigner(e.ctx, c)
	if !errors.Is(err, ErrSignerLockHeld) {
		t.Fatalf("second lock on a held signer: want ErrSignerLockHeld, got %v", err)
	}
	ssC.Close()
	ssCapped, err = capped.LockSigner(e.ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer ssCapped.Close()
	refused(ssCapped, draft(ic.IntentID, 0, 101), RefusedAttemptCap)

	lim = testLimits()
	lim.AttemptsPerBusinessID = 1
	bizCapped, err := New(e.runtime, lim, "biz-capped")
	if err != nil {
		t.Fatal(err)
	}
	ssCapped.Close()
	ssBiz, err := bizCapped.LockSigner(e.ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	defer ssBiz.Close()
	refused(ssBiz, draft(ic.IntentID, 0, 101), RefusedBusinessIDCap)
	tx, err := ssBiz.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.OpenBusinessAttempt(e.ctx, ic.IntentID, Expect{StateRequested, PhaseRetryWait}, "ID_TAKEN"); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	res, err := ssBiz.PersistAttempt(e.ctx, draft(ic.IntentID, 0, 101))
	if err != nil || res.Refusal != RefusedNone {
		t.Fatalf("persist under business attempt 2: %+v %v", res, err)
	}
	atts, err := e.st.Attempts(e.ctx, ic.IntentID)
	if err != nil || len(atts) != 2 || atts[1].BusinessN != 2 || atts[1].N != 2 {
		t.Fatalf("attempts %+v %v", atts, err)
	}

	d := draft(i2.IntentID, 0, 100)
	bad := d
	bad.TxHash[0] ^= 1
	for name, x := range map[string]AttemptDraft{
		"hash":       bad,
		"gas zero":   func() AttemptDraft { v := d; v.Gas = 0; return v }(),
		"gas high":   func() AttemptDraft { v := d; v.Gas = maxGas + 1; return v }(),
		"no timeout": func() AttemptDraft { v := d; v.TimeoutHeight = 0; return v }(),
		"empty tx":   func() AttemptDraft { v := d; v.TxBytes = nil; v.TxHash = sha256.Sum256(nil); return v }(),
		"seq max":    func() AttemptDraft { v := d; v.Sequence = 1<<63 - 1; return v }(),
		"seq huge":   func() AttemptDraft { v := d; v.Sequence = 1 << 63; return v }(),
		"bad id":     func() AttemptDraft { v := d; v.IntentID = "x"; return v }(),
	} {
		if _, err := ssB.PersistAttempt(e.ctx, x); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
}

// withAccount returns d with another account number.
func (d AttemptDraft) withAccount(n uint64) AttemptDraft {
	d.AccountNumber = n
	return d
}

// TestOneLiveAttemptPerSequence (plan 13.2): the partial unique index allows
// one live attempt per (signer, sequence); identical bytes collide on
// tx_hash; after the attempt dies the sequence is reused with a strictly
// higher timeout height (8.2, m6).
func TestOneLiveAttemptPerSequence(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	i1 := e.enqueue("p", "i1", signer, time.Hour)
	i2 := e.enqueue("p", "i2", signer, time.Hour)
	att := e.persist(ss, i1.IntentID, 0, 100)

	other, otherHash := txBytes("direct", 0, 100)
	_, err := e.runtime.ExecContext(e.ctx, `INSERT INTO broadcaster.attempts (intent_id, n, business_n, signer_address,
			account_number, sequence, timeout_height, gas, tx_bytes, tx_hash, phase, worker_instance)
		VALUES ($1, 1, 1, $2, 7, 0, 100, 1, $3, $4, 'SIGNED', 'direct')`, i2.IntentID, signer, other, otherHash[:])
	wantCode(t, err, "23505")
	d := draft(i1.IntentID, 0, 100)
	_, err = e.runtime.ExecContext(e.ctx, `INSERT INTO broadcaster.attempts (intent_id, n, business_n, signer_address,
			account_number, sequence, timeout_height, gas, tx_bytes, tx_hash, phase, worker_instance, dead_reason, dead_at)
		VALUES ($1, 1, 1, $2, 7, 5, 100, 1, $3, $4, 'DEAD', 'direct', 'D3_DEADLINE_PASSED', now())`, i2.IntentID, signer, d.TxBytes, d.TxHash[:])
	wantCode(t, err, "23505")
	_, err = e.runtime.ExecContext(e.ctx, `INSERT INTO broadcaster.attempts (intent_id, n, business_n, signer_address,
			account_number, sequence, timeout_height, gas, tx_bytes, tx_hash, phase, worker_instance)
		VALUES ($1, 1, 1, $2, 7, 9, 100, 1, '\x01', '\x02', 'SIGNED', 'direct')`, i2.IntentID, signer)
	wantCode(t, err, "23514")

	killAttempt(t, ss, i1.IntentID, att, Expect{StateInFlight, PhaseSigned})
	if err := ss.SyncSequence(e.ctx, 7, 0, 50); err != nil {
		t.Fatal(err)
	}
	if res, err := ss.PersistAttempt(e.ctx, draft(i1.IntentID, 0, 100)); err != nil || res.Refusal != RefusedTimeoutNotAbove {
		t.Fatalf("same timeout: %+v %v", res, err)
	}
	att2 := e.persist(ss, i1.IntentID, 0, 101)
	atts, err := e.st.Attempts(e.ctx, i1.IntentID)
	if err != nil || len(atts) != 2 || atts[0].Phase != AttemptDead || atts[0].DeadReason != "D3_DEADLINE_PASSED" ||
		atts[1].ID != att2 || atts[1].Sequence != 0 || atts[1].TimeoutHeight != 101 {
		t.Fatalf("attempts %+v %v", atts, err)
	}
}

// TestSendCountingAndPhases (plan 8.3 steps 2 and 4, 9.2): every send is
// counted before it happens; outcomes raise the attempt and intent phase to
// the highest knowledge seen and never lower it; refusals are counted only
// when the classifier says so; a transitions row marks each change of phase
// or class; a CheckTx log is clipped to valid UTF-8 without NUL.
func TestSendCountingAndPhases(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	in := e.enqueue("p", "send", signer, time.Hour)
	att := e.persist(ss, in.IntentID, 0, 100)

	if _, err := ss.RecordSendOutcome(e.ctx, att, SendOutcome{Class: SendAck}); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("outcome without a started send: want ErrStateChanged, got %v", err)
	}
	send := func(o SendOutcome) {
		t.Helper()
		if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
			t.Fatalf("begin send: %v %v", ok, err)
		}
		if ok, err := ss.RecordSendOutcome(e.ctx, att, o); err != nil || !ok {
			t.Fatalf("record %s: %v %v", o.Class, ok, err)
		}
	}
	check := func(phase AttemptPhase, intentPhase Phase, started, recorded, unknown, cache, refusals int, acked bool) {
		t.Helper()
		atts, err := e.st.Attempts(e.ctx, in.IntentID)
		if err != nil || len(atts) != 1 {
			t.Fatal(err)
		}
		a := atts[0]
		if a.Phase != phase || a.SendsStarted != started || a.SendsRecorded != recorded || a.UnknownSends != unknown ||
			a.CacheAnswers != cache || a.CountedRefusals != refusals || a.Acked != acked {
			t.Fatalf("attempt %+v", a)
		}
		if _, p, _ := e.state(in.IntentID); p != intentPhase {
			t.Fatalf("intent phase %s, want %s", p, intentPhase)
		}
	}
	send(SendOutcome{Class: SendMempoolRejected, Code: 32, Codespace: "sdk", Log: "account sequence mismatch"})
	check(AttemptSigned, PhaseSigned, 1, 1, 0, 0, 0, false)
	send(SendOutcome{Class: SendNodeRefused, CountsAsRefusal: true})
	check(AttemptSigned, PhaseSigned, 2, 2, 0, 0, 1, false)
	send(SendOutcome{Class: SendUnknown})
	check(AttemptUncertain, PhaseUncertain, 3, 3, 1, 0, 1, false)
	send(SendOutcome{Class: SendAck, Code: 0, Codespace: ""})
	check(AttemptAcknowledged, PhaseAcknowledged, 4, 4, 1, 0, 1, true)
	send(SendOutcome{Class: SendCache})
	check(AttemptAcknowledged, PhaseAcknowledged, 5, 5, 1, 1, 1, true)
	send(SendOutcome{Class: SendUnknown})
	check(AttemptAcknowledged, PhaseAcknowledged, 6, 6, 2, 1, 1, true)

	if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatal(err)
	}
	check(AttemptAcknowledged, PhaseAcknowledged, 7, 6, 2, 1, 1, true)

	if ok, err := ss.RecordSendOutcome(e.ctx, att, SendOutcome{Class: SendMempoolRejected, CountsAsRefusal: true, Code: 13,
		Codespace: "sdk", Log: "bad\x00log " + strings.Repeat("é", 400) + "\xff"}); err != nil || !ok {
		t.Fatal(err)
	}
	var log string
	var code int64
	if err := e.super.QueryRowContext(e.ctx, `SELECT ack_log, ack_code FROM broadcaster.attempts WHERE attempt_id = $1`, att).Scan(&log, &code); err != nil {
		t.Fatal(err)
	}
	if len(log) > 512 || strings.ContainsRune(log, 0) || !strings.HasPrefix(log, "badlog ") || code != 13 {
		t.Fatalf("ack_log %d bytes %q code %d", len(log), log[:20], code)
	}

	trs, err := e.st.Transitions(e.ctx, in.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	var reasons []string
	for _, tr := range trs {
		reasons = append(reasons, tr.Reason)
	}
	want := "CREATED ATTEMPT_PERSISTED SEND_MEMPOOL_REJECTED SEND_NODE_REFUSED SEND_UNKNOWN SEND_ACK SEND_CACHE SEND_UNKNOWN SEND_MEMPOOL_REJECTED"
	if strings.Join(reasons, " ") != want {
		t.Fatalf("transitions %v", reasons)
	}
	if trs[5].To != (Expect{StateInFlight, PhaseAcknowledged}) || *trs[5].From != (Expect{StateInFlight, PhaseUncertain}) {
		t.Fatalf("ACK transition %+v", trs[5])
	}

	if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := ss.RecordSendOutcome(e.ctx, att, SendOutcome{Class: SendMempoolRejected, CountsAsRefusal: true, Code: 13,
		Codespace: "sdk"}); err != nil || !ok {
		t.Fatal(err)
	}
	var suppressed int
	if err := e.super.QueryRowContext(e.ctx, `SELECT transitions_suppressed FROM broadcaster.intents WHERE intent_id = $1`,
		in.IntentID).Scan(&suppressed); err != nil || suppressed != 1 {
		t.Fatalf("repeated class: suppressed=%d %v", suppressed, err)
	}

	// The store checks each outcome against its class and the (d2) rule of
	// plan 8.4, so a classifier error cannot let an attempt die by (d2).
	for _, o := range []SendOutcome{
		{Class: SendAck, CountsAsRefusal: true},
		{Class: SendUnknown, CountsAsRefusal: true},
		{Class: SendCache, CountsAsRefusal: true},
		{Class: "OTHER"},
		{Class: SendAck, Codespace: "two words"},
		{Class: SendAck, Code: 13, Codespace: "sdk"},
		{Class: SendMempoolRejected, CountsAsRefusal: true, Code: 32, Codespace: "sdk"},
		{Class: SendMempoolRejected, Code: 13, Codespace: "sdk"},
		{Class: SendMempoolRejected, CountsAsRefusal: true, Code: 0, Codespace: "sdk"},
		{Class: SendMempoolRejected, CountsAsRefusal: true, Code: 0},
		{Class: SendMempoolRejected, CountsAsRefusal: true, Code: 13},
		{Class: SendNodeRefused},
		{Class: SendNodeRefused, CountsAsRefusal: true, Code: 13, Codespace: "sdk"},
		{Class: SendUnknown, Code: 13, Codespace: "sdk"},
		{Class: SendCache, Codespace: "sdk"},
	} {
		if _, err := ss.RecordSendOutcome(e.ctx, att, o); !errors.Is(err, ErrInvalid) {
			t.Errorf("%+v: want ErrInvalid, got %v", o, err)
		}
	}
	_, err = e.owner.ExecContext(e.ctx, `UPDATE broadcaster.attempts SET phase = 'SIGNED' WHERE attempt_id = $1`, att)
	wantCode(t, err, "SV001")
	_, err = e.owner.ExecContext(e.ctx, `UPDATE broadcaster.attempts SET sends_started = 0, sends_recorded = 0,
		unknown_sends = 0, cache_answers = 0, counted_refusals = 0 WHERE attempt_id = $1`, att)
	wantCode(t, err, "SV001")
}

// TestDeadRuleD2NeedsCountedRefusals (plan 8.5, M1): the database refuses a
// (d2) death with zero started sends, with a started send not recorded, or
// with any recorded outcome that is not a counted refusal (UNKNOWN, CACHE,
// ACK or the uncounted sdk 32); it accepts all counted refusals.
func TestDeadRuleD2NeedsCountedRefusals(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 16)
	round := 0
	// Each try uses its own signer: a refused death leaves the attempt live,
	// and a live attempt keeps its signer busy.
	try := func(outcomes []SendOutcome, extraStarted bool) error {
		t.Helper()
		round++
		signer := e.signer(int32(round))
		ss := e.lock(signer)
		defer ss.Close()
		in := e.enqueue("p", "d2-"+strings.Repeat("x", round), signer, time.Hour)
		att := e.persist(ss, in.IntentID, 0, 100)
		for _, o := range outcomes {
			if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
				t.Fatal(err)
			}
			if ok, err := ss.RecordSendOutcome(e.ctx, att, o); err != nil || !ok {
				t.Fatal(err)
			}
		}
		if extraStarted {
			if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
				t.Fatal(err)
			}
		}
		tx, err := ss.Begin(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := tx.MarkDead(e.ctx, att, DeadD2Refused); err != nil {
			return err
		}
		if err := tx.FailIntent(e.ctx, in.IntentID, Expect{StateInFlight, PhaseSigned}, "REJECTED", &ChainCode{Code: 13, Codespace: "sdk"}); err != nil {
			t.Fatal(err)
		}
		return tx.Commit()
	}
	refusal := SendOutcome{Class: SendMempoolRejected, CountsAsRefusal: true, Code: 13, Codespace: "sdk"}
	nodeRefusal := SendOutcome{Class: SendNodeRefused, CountsAsRefusal: true}
	for name, c := range map[string]struct {
		outcomes []SendOutcome
		extra    bool
	}{
		"zero sends":         {nil, false},
		"unrecorded send":    {[]SendOutcome{refusal}, true},
		"unknown":            {[]SendOutcome{refusal, {Class: SendUnknown}}, false},
		"cache":              {[]SendOutcome{{Class: SendCache}}, false},
		"sdk 32 not counted": {[]SendOutcome{{Class: SendMempoolRejected, Code: 32, Codespace: "sdk"}}, false},
	} {
		if err := try(c.outcomes, c.extra); !errors.Is(err, ErrInvalid) || SQLState(err) != "23514" {
			t.Errorf("%s: want the d2 check violation, got %v", name, err)
		}
	}
	if err := try([]SendOutcome{refusal, nodeRefusal}, false); err != nil {
		t.Fatalf("all counted refusals: %v", err)
	}
}

// TestCommitNeedsEvidence (plan 9.3, 9.4): COMMITTED needs the commitment
// row (checked at commit), a commitment needs a COMMITTED intent, and the
// canonical evidence of an idempotent replay is the first commitment.
func TestCommitNeedsEvidence(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()

	in := e.enqueue("p", "no-evidence", signer, time.Hour)
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	mustExec(t, tx.tx, `UPDATE broadcaster.intents SET state = 'COMMITTED', phase = NULL, final_at = now(),
		committed_tx_hash = decode(repeat('ab', 32), 'hex'), own_transaction = true WHERE intent_id = $1`, in.IntentID)
	if err := tx.Commit(); !errors.Is(err, ErrEvidence) {
		t.Fatalf("COMMITTED without a commitment: want ErrEvidence, got %v", err)
	}
	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	mustExec(t, tx.tx, `INSERT INTO broadcaster.commitments (intent_id, chain_id, business_id, height, block_hash, tx_hash,
			first_commit_tx_hash, tx_index, msg_index, code, codespace, gas_wanted, gas_used, events, idempotent_replay,
			committing_signer)
		VALUES ($1, $2, $3, 1, decode(repeat('01', 32), 'hex'), decode(repeat('02', 32), 'hex'),
			decode(repeat('02', 32), 'hex'), 0, 0, 0, '', 1, 1, '[]', false, $4)`, in.IntentID, testChainID, in.BusinessID, signer)
	if err := tx.Commit(); !errors.Is(err, ErrEvidence) {
		t.Fatalf("commitment without a COMMITTED intent: want ErrEvidence, got %v", err)
	}
	if st, _, _ := e.state(in.IntentID); st != StateRequested {
		t.Fatalf("intent %s after refused commits", st)
	}

	att := e.persist(ss, in.IntentID, 0, 100)
	if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := ss.RecordSendOutcome(e.ctx, att, SendOutcome{Class: SendUnknown}); err != nil || !ok {
		t.Fatal(err)
	}
	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	own := draft(in.IntentID, 0, 100).TxHash
	if err := tx.ResolveCommitted(e.ctx, att, CommittedResult{TxHash: own, Height: 77, Index: 3, Code: 0, AccSeqPresent: true,
		Idempotent: true, GasUsed: 10}); err != nil {
		t.Fatal(err)
	}
	first := sha256.Sum256([]byte("first commit by another account"))
	events := json.RawMessage(`[{"index":0,"type":"svn.events.v1.LeaseAccepted","attributes":{"lease_id":"l1"}}]`)
	if err := tx.CommitIntent(e.ctx, in.IntentID, Expect{StateInFlight, PhaseUncertain}, Commitment{
		Height: 70, BlockHash: [32]byte{9}, TxHash: own, FirstCommitTxHash: first, TxIndex: 1, Codespace: "",
		GasWanted: 2_000_000, GasUsed: 99, Events: events, CommittingSigner: signer,
	}, false); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	var (
		height     int64
		txHash     []byte
		firstHash  []byte
		replay     bool
		committed  []byte
		businessID string
		chain      string
	)
	if err := e.super.QueryRowContext(e.ctx, `SELECT c.height, c.tx_hash, c.first_commit_tx_hash, c.idempotent_replay,
			i.committed_tx_hash, c.business_id, c.chain_id
		FROM broadcaster.commitments c JOIN broadcaster.intents i USING (intent_id) WHERE intent_id = $1`, in.IntentID).Scan(
		&height, &txHash, &firstHash, &replay, &committed, &businessID, &chain); err != nil {
		t.Fatal(err)
	}
	if height != 70 || string(txHash) != string(own[:]) || string(firstHash) != string(first[:]) || !replay ||
		string(committed) != string(first[:]) || businessID != in.BusinessID || chain != testChainID {
		t.Fatalf("commitment height=%d replay=%v business=%s chain=%s", height, replay, businessID, chain)
	}
	got, err := e.st.GetIntent(e.ctx, "p", in.IntentID)
	if err != nil || got.State != StateCommitted || got.Phase != PhaseNone || got.OwnTransaction == nil || *got.OwnTransaction || got.FinalAt == nil {
		t.Fatalf("committed intent %+v %v", got, err)
	}

	in2 := e.enqueue("p", "bad-commit", signer, time.Hour)
	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.CommitIntent(e.ctx, in2.IntentID, Expect{StateCommitted, PhaseNone}, Commitment{}, true); !errors.Is(err, ErrInvalid) {
		t.Fatalf("COMMITTED from COMMITTED: want ErrInvalid, got %v", err)
	}
	if err := tx.CommitIntent(e.ctx, in2.IntentID, Expect{StateRequested, PhaseQueued}, Commitment{Height: 1,
		CommittingSigner: signer, Events: []byte(`[]`)}, false); err != nil {
		t.Fatalf("T5 from REQUESTED: %v", err)
	}
	if err := tx.ResolveCommitted(e.ctx, att, CommittedResult{TxHash: own, Height: 1, Code: 0}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("code 0 without acc_seq: want ErrInvalid, got %v", err)
	}
	if err := tx.ResolveCommitted(e.ctx, att, CommittedResult{TxHash: own, Height: 1, Code: 5, AccSeqPresent: true, Idempotent: true}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("idempotent non-zero code: want ErrInvalid, got %v", err)
	}
}

// TestLiveAttemptInvariant (plan 9.3, M2): at commit, an IN_FLIGHT intent
// has exactly one live attempt and any other intent none. Resolving an
// attempt without moving its intent, failing an intent whose attempt is
// still live, and a retry with a live attempt are refused with nothing
// written.
func TestLiveAttemptInvariant(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	in := e.enqueue("p", "live", signer, time.Hour)
	att := e.persist(ss, in.IntentID, 0, 100)
	attHash := draft(in.IntentID, 0, 100).TxHash
	inFlight := Expect{StateInFlight, PhaseSigned}
	// A started send keeps the Q4 clearing out of FailIntent, so failing
	// with the attempt still live reaches the commit-time check;
	// TestFailIntentUnsentWithLiveAttempt covers the never-sent case, which
	// FailIntent refuses before commit.
	if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatalf("begin send: %v %v", ok, err)
	}

	steps := map[string]func(*SignerTx) error{
		"resolve without transition": func(tx *SignerTx) error {
			return tx.ResolveCommitted(e.ctx, att, CommittedResult{TxHash: attHash, Height: 5, Code: 7, Codespace: "svn", AccSeqPresent: true})
		},
		"dead without transition": func(tx *SignerTx) error {
			return tx.MarkDead(e.ctx, att, DeadD3SequenceUsed)
		},
		"fail with a live attempt": func(tx *SignerTx) error {
			return tx.FailIntent(e.ctx, in.IntentID, inFlight, "OBJECT_TAKEN", nil)
		},
		"retry with a live attempt": func(tx *SignerTx) error {
			return tx.RetryWait(e.ctx, in.IntentID, inFlight, time.Second, "RETRY")
		},
	}
	for name, step := range steps {
		tx, err := ss.Begin(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := step(tx); err != nil {
			t.Fatalf("%s: step: %v", name, err)
		}
		if err := tx.Commit(); !errors.Is(err, ErrLiveAttempt) {
			t.Fatalf("%s: want ErrLiveAttempt at commit, got %v", name, err)
		}
	}
	if st, phase, _ := e.state(in.IntentID); st != StateInFlight || phase != PhaseSigned {
		t.Fatalf("intent %s/%s after refused commits", st, phase)
	}
	if atts, _ := e.st.Attempts(e.ctx, in.IntentID); atts[0].Phase != AttemptSigned {
		t.Fatalf("attempt %s after refused commits", atts[0].Phase)
	}

	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.ResolveCommitted(e.ctx, att, CommittedResult{TxHash: attHash, Height: 5, Code: 7, Codespace: "svn", AccSeqPresent: true}); err != nil {
		t.Fatal(err)
	}
	if err := tx.FailIntent(e.ctx, in.IntentID, inFlight, "EXECUTION_FAILED", &ChainCode{Code: 7, Codespace: "svn"}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("resolve and fail together: %v", err)
	}
	got, err := e.st.GetIntent(e.ctx, "p", in.IntentID)
	if err != nil || got.State != StateFailed || got.Reason != "EXECUTION_FAILED" || got.Failure == nil ||
		*got.Failure != (ChainCode{Code: 7, Codespace: "svn"}) {
		t.Fatalf("failed intent %+v %v", got, err)
	}
	atts, err := e.st.Attempts(e.ctx, in.IntentID)
	if err != nil || atts[0].Phase != AttemptExecutionFailed || atts[0].CommittedHeight != 5 || atts[0].AccSeqPresent == nil || !*atts[0].AccSeqPresent {
		t.Fatalf("attempt %+v %v", atts, err)
	}
}

// TestRetryAndBusinessAttempts (plan 8.6 step 5, 8.7): a committed REPLAY
// with no effect retries under a new random business id, bounded by
// BusinessAttempts; the old id keeps its outcome; next_attempt_at follows the
// database clock.
func TestRetryAndBusinessAttempts(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	in := e.enqueue("p", "replay", signer, time.Hour)
	ids := map[string]bool{in.BusinessID: true}
	for n := 1; n <= 4; n++ {
		att := e.persist(ss, in.IntentID, uint64(n-1), uint64(100+n))
		tx, err := ss.Begin(e.ctx)
		if err != nil {
			t.Fatal(err)
		}
		defer tx.Rollback()
		if err := tx.ResolveCommitted(e.ctx, att, CommittedResult{TxHash: draft(in.IntentID, uint64(n-1), uint64(100+n)).TxHash,
			Height: uint64(10 + n), Code: 9, Codespace: "svn", AccSeqPresent: true}); err != nil {
			t.Fatal(err)
		}
		if err := tx.RetryWait(e.ctx, in.IntentID, Expect{StateInFlight, PhaseSigned}, 2*time.Second, "REPLAY_NO_EFFECT"); err != nil {
			t.Fatal(err)
		}
		bid, err := tx.OpenBusinessAttempt(e.ctx, in.IntentID, Expect{StateRequested, PhaseRetryWait}, "BUSINESS_ID_TAKEN")
		if n == 4 {
			if !errors.Is(err, ErrBusinessAttemptCap) {
				t.Fatalf("fifth business attempt: want ErrBusinessAttemptCap, got %v", err)
			}
			if err := tx.FailIntent(e.ctx, in.IntentID, Expect{StateRequested, PhaseRetryWait}, "BUSINESS_ID_TAKEN", nil); err != nil {
				t.Fatal(err)
			}
		} else {
			if err != nil || ids[bid] || !strings.HasPrefix(bid, businessIDPrefix) {
				t.Fatalf("business attempt %d: %q %v", n+1, bid, err)
			}
			ids[bid] = true
		}
		if err := tx.Commit(); err != nil {
			t.Fatal(err)
		}
		if err := ss.SyncSequence(e.ctx, 7, uint64(n), uint64(20+n)); err != nil {
			t.Fatal(err)
		}
		if n < 4 {
			if due, err := e.st.DueIntents(e.ctx, signer, 10); err != nil || len(due) != 0 {
				t.Fatalf("due before next_attempt_at: %v %v", due, err)
			}
		}
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.business_attempts WHERE intent_id = $1 AND outcome = 'BUSINESS_ID_TAKEN'`, in.IntentID); got != 3 {
		t.Fatalf("%d closed business attempts, want 3", got)
	}
	if st, _, reason := e.state(in.IntentID); st != StateFailed || reason != "BUSINESS_ID_TAKEN" {
		t.Fatalf("final %s %s", st, reason)
	}

	in2 := e.enqueue("p", "wait", signer, time.Hour)
	att := e.persist(ss, in2.IntentID, 4, 200)
	killAttempt(t, ss, in2.IntentID, att, Expect{StateInFlight, PhaseSigned})
	if err := ss.SyncSequence(e.ctx, 7, 4, 30); err != nil {
		t.Fatal(err)
	}
	if due, err := e.st.DueIntents(e.ctx, signer, 10); err != nil || len(due) != 1 || due[0] != in2.IntentID {
		t.Fatalf("due after a zero delay: %v %v", due, err)
	}
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.RetryWait(e.ctx, in2.IntentID, Expect{StateRequested, PhaseRetryWait}, maxBackoff+time.Second, "X"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("delay above the hard cap: want ErrInvalid, got %v", err)
	}
	if err := tx.RetryWait(e.ctx, in2.IntentID, Expect{StateRequested, PhaseQueued}, 0, "X"); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("wrong expected phase: want ErrStateChanged, got %v", err)
	}
}

// TestSignerConflictHaltAndClear (plan 8.1, DR-11-15): a foreign use of a
// signer sequence records a signer event and halts the signer; only
// signer-clear for that event lifts the halt; another halt reason is not
// lifted by it.
func TestSignerConflictHaltAndClear(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	in := e.enqueue("p", "conflict", signer, time.Hour)
	foreign := sha256.Sum256([]byte("foreign transaction"))
	if err := ss.HaltForConflict(e.ctx, 0, foreign); err != nil {
		t.Fatal(err)
	}
	if err := ss.HaltForConflict(e.ctx, 0, foreign); err != nil {
		t.Fatalf("repeated detection: %v", err)
	}
	if _, halted := e.signerState(signer); halted != "SIGNER_CONFLICT" {
		t.Fatalf("halt reason %q", halted)
	}
	if res, err := ss.PersistAttempt(e.ctx, draft(in.IntentID, 0, 100)); err != nil || res.Refusal != RefusedSignerHalted {
		t.Fatalf("halted signer persisted: %+v %v", res, err)
	}
	if err := ss.ClearConflict(e.ctx, 0, sha256.Sum256([]byte("other")), "signer-clear"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("clear of an unknown event: want ErrNotFound, got %v", err)
	}
	if err := ss.ClearConflict(e.ctx, 0, foreign, "Signer Clear"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad command name: want ErrInvalid, got %v", err)
	}
	if err := ss.ClearConflict(e.ctx, 0, foreign, "signer-clear"); err != nil {
		t.Fatal(err)
	}
	if err := ss.ClearConflict(e.ctx, 0, foreign, "signer-clear"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second clear: want ErrNotFound, got %v", err)
	}
	if _, halted := e.signerState(signer); halted != "" {
		t.Fatalf("still halted %q", halted)
	}
	_, err := e.owner.ExecContext(e.ctx, `UPDATE broadcaster.signer_events SET cleared_by_command = 'other'`)
	wantCode(t, err, "SV001")
	e.persist(ss, in.IntentID, 0, 100)

	if err := ss.Halt(e.ctx, "UNAUTHORIZED_SIGNER"); err != nil {
		t.Fatal(err)
	}
	second := sha256.Sum256([]byte("second foreign"))
	if err := ss.HaltForConflict(e.ctx, 1, second); err != nil {
		t.Fatal(err)
	}
	if err := ss.ClearConflict(e.ctx, 1, second, "signer-clear"); err != nil {
		t.Fatal(err)
	}
	if _, halted := e.signerState(signer); halted != "UNAUTHORIZED_SIGNER" {
		t.Fatalf("a non-conflict halt was lifted: %q", halted)
	}
	if err := ss.Halt(e.ctx, "bad reason"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad halt reason: want ErrInvalid, got %v", err)
	}
}

// TestSyncSequenceRules (plan 8.1, m2): a sync from older state than the
// stored observation is ErrStaleObservation; a sync while the signer has a
// live attempt is ErrSignerBusy; a sync may lower next_sequence when the
// chain did not use a persisted sequence.
func TestSyncSequenceRules(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	if err := ss.SyncSequence(e.ctx, 7, 0, 10); err != nil {
		t.Fatal(err)
	}
	if err := ss.SyncSequence(e.ctx, 7, 0, 9); !errors.Is(err, ErrStaleObservation) {
		t.Fatalf("older height: want ErrStaleObservation, got %v", err)
	}
	in := e.enqueue("p", "sync", signer, time.Hour)
	att := e.persist(ss, in.IntentID, 0, 100)
	if err := ss.SyncSequence(e.ctx, 7, 0, 11); !errors.Is(err, ErrSignerBusy) {
		t.Fatalf("live attempt: want ErrSignerBusy, got %v", err)
	}
	killAttempt(t, ss, in.IntentID, att, Expect{StateInFlight, PhaseSigned})
	if next, _ := e.signerState(signer); next != 1 {
		t.Fatalf("next %d", next)
	}
	if err := ss.SyncSequence(e.ctx, 7, 0, 11); err != nil {
		t.Fatal(err)
	}
	if next, _ := e.signerState(signer); next != 0 {
		t.Fatalf("next %d after sync, want 0", next)
	}
	if err := ss.SyncSequence(e.ctx, 7, 1<<63, 12); !errors.Is(err, ErrInvalid) {
		t.Fatalf("sequence above bigint: want ErrInvalid, got %v", err)
	}
}

// TestStallEpisodesAndHealth (section 10, DR-11-11): one open stall episode
// at a time; opening and closing it appends a row to every non-terminal
// intent; persist is refused while it is open; the observer's chain health
// is stored with the database clock.
func TestStallEpisodesAndHealth(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	open := e.enqueue("p", "open", signer, time.Hour)
	done := e.enqueue("p", "done", signer, time.Hour)
	if err := e.st.Cancel(e.ctx, "p", done.IntentID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.OpenStallEpisode(e.ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no episode: want ErrNotFound, got %v", err)
	}
	id, err := e.st.OpenStall(e.ctx, StallHeight, 50)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.OpenStall(e.ctx, StallUnreachable, 51); !errors.Is(err, ErrStallOpen) {
		t.Fatalf("second episode: want ErrStallOpen, got %v", err)
	}
	ep, err := e.st.OpenStallEpisode(e.ctx)
	if err != nil || ep.ID != id || ep.Reason != StallHeight || ep.StartedHeight != 50 {
		t.Fatalf("episode %+v %v", ep, err)
	}
	if _, err := e.st.CloseStall(e.ctx, 49); err == nil {
		t.Fatal("closed below the start height")
	}
	if ok, err := e.st.CloseStall(e.ctx, 52); err != nil || !ok {
		t.Fatalf("close: %v %v", ok, err)
	}
	if ok, err := e.st.CloseStall(e.ctx, 53); err != nil || ok {
		t.Fatalf("close with none open: %v %v", ok, err)
	}
	reasons := func(id string) string {
		trs, err := e.st.Transitions(e.ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		var r []string
		for _, tr := range trs {
			r = append(r, tr.Reason)
		}
		return strings.Join(r, " ")
	}
	if got := reasons(open.IntentID); got != "CREATED STALL_OPENED STALL_CLOSED" {
		t.Fatalf("open intent transitions %q", got)
	}
	if got := reasons(done.IntentID); got != "CREATED CANCELLED" {
		t.Fatalf("terminal intent transitions %q", got)
	}
	if _, err := e.st.OpenStall(e.ctx, "OTHER", 1); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad reason: want ErrInvalid, got %v", err)
	}

	if _, err := e.st.ReadChainHealth(e.ctx); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no health yet: want ErrNotFound, got %v", err)
	}
	blockTime := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	if err := e.st.RecordChainHealth(e.ctx, 60, blockTime); err != nil {
		t.Fatal(err)
	}
	if err := e.st.RecordChainHealth(e.ctx, 61, blockTime.Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	h, err := e.st.ReadChainHealth(e.ctx)
	if err != nil || h.LatestHeight != 61 || !h.LatestTime.Equal(blockTime.Add(time.Second)) || h.Observer != "test-instance" ||
		h.Age < 0 || h.Age > time.Minute {
		t.Fatalf("health %+v %v", h, err)
	}
}

// TestCancelRacesPersist (plan 13.2, M2): a cancel and the persist of a
// signed attempt race on one intent. Exactly one wins; the loser changes
// nothing: either FAILED(CANCELLED) with no attempt and the signer's
// sequence untouched, or IN_FLIGHT with one attempt and the cancel refused.
// Both orders run once deterministically; the concurrent rounds start the
// cancel 0 to 5 ms after the persist so either side can win.
func TestCancelRacesPersist(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 64)

	first := e.signer(1)
	ss := e.lock(first)
	persistFirst := e.enqueue("p", "persist-first", first, time.Hour)
	e.persist(ss, persistFirst.IntentID, 0, 100)
	if err := e.st.Cancel(e.ctx, "p", persistFirst.IntentID); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("cancel after persist: want ErrStateChanged, got %v", err)
	}
	if st, _, _ := e.state(persistFirst.IntentID); st != StateInFlight {
		t.Fatalf("cancel after persist changed the intent to %s", st)
	}
	ss.Close()
	second := e.signer(2)
	ss = e.lock(second)
	cancelFirst := e.enqueue("p", "cancel-first", second, time.Hour)
	if err := e.st.Cancel(e.ctx, "p", cancelFirst.IntentID); err != nil {
		t.Fatal(err)
	}
	if res, err := ss.PersistAttempt(e.ctx, draft(cancelFirst.IntentID, 0, 100)); err != nil || res.Refusal != RefusedNotRequested {
		t.Fatalf("persist after cancel: %+v %v", res, err)
	}
	if next, _ := e.signerState(second); next != 0 {
		t.Fatalf("persist after cancel moved the sequence to %d", next)
	}
	ss.Close()

	const rounds = 24
	wins := map[string]int{}
	for i := range rounds {
		signer := e.signer(int32(10 + i))
		ss := e.lock(signer)
		in := e.enqueue("p", "race-"+strings.Repeat("r", i+1), signer, time.Hour)
		start := make(chan struct{})
		var wg sync.WaitGroup
		var cancelErr, persistErr error
		var res PersistResult
		wg.Add(2)
		go func() {
			defer wg.Done()
			<-start
			time.Sleep(time.Duration(i%6) * time.Millisecond)
			cancelErr = e.st.Cancel(e.ctx, "p", in.IntentID)
		}()
		go func() {
			defer wg.Done()
			<-start
			res, persistErr = ss.PersistAttempt(e.ctx, draft(in.IntentID, 0, 100))
		}()
		close(start)
		wg.Wait()
		ss.Close()
		st, _, reason := e.state(in.IntentID)
		attempts := e.count(`SELECT count(*) FROM broadcaster.attempts WHERE intent_id = $1`, in.IntentID)
		next, _ := e.signerState(signer)
		switch {
		case cancelErr == nil:
			if persistErr != nil || res.Refusal != RefusedNotRequested || st != StateFailed || reason != "CANCELLED" || attempts != 0 || next != 0 {
				t.Fatalf("round %d cancel won: persist=%+v %v state=%s attempts=%d next=%d", i, res, persistErr, st, attempts, next)
			}
			wins["cancel"]++
		case errors.Is(cancelErr, ErrStateChanged):
			if persistErr != nil || res.Refusal != RefusedNone || st != StateInFlight || attempts != 1 || next != 1 {
				t.Fatalf("round %d persist won: persist=%+v %v state=%s attempts=%d next=%d", i, res, persistErr, st, attempts, next)
			}
			wins["persist"]++
		default:
			t.Fatalf("round %d: cancel %v", i, cancelErr)
		}
	}
	t.Logf("winners over %d rounds: %v", rounds, wins)
}

// TestAdvisoryLockFencesSecondWorker (plan 13.2, 8.1): a second session for
// a locked signer is refused, even from another pool; a session whose lock
// was released can neither count a send nor persist (ErrFenceLost), and the
// signer is then free for another session.
func TestAdvisoryLockFencesSecondWorker(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	first := e.lock(signer)
	otherPool := e.open(RuntimeRole, runtimePass)
	defer otherPool.Close()
	otherStore, err := New(otherPool, testLimits(), "second-worker")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := otherStore.LockSigner(e.ctx, signer); !errors.Is(err, ErrSignerLockHeld) {
		t.Fatalf("second worker: want ErrSignerLockHeld, got %v", err)
	}
	if _, err := otherStore.LockSigner(e.ctx, make([]byte, 20)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unregistered signer: want ErrNotFound, got %v", err)
	}
	in := e.enqueue("p", "fence", signer, time.Hour)
	att := e.persist(first, in.IntentID, 0, 100)

	mustExec(t, first.conn, `SELECT pg_advisory_unlock($1, $2)`, lockClassSigner, first.key)
	if ok, err := first.BeginSend(e.ctx, att); !errors.Is(err, ErrFenceLost) || ok {
		t.Fatalf("released lock: want ErrFenceLost, got %v %v", ok, err)
	}
	in2 := e.enqueue("p", "fence-2", signer, time.Hour)
	if _, err := first.PersistAttempt(e.ctx, draft(in2.IntentID, 1, 100)); !errors.Is(err, ErrFenceLost) {
		t.Fatalf("released lock persisted: want ErrFenceLost, got %v", err)
	}
	second, err := otherStore.LockSigner(e.ctx, signer)
	if err != nil {
		t.Fatalf("lock after release: %v", err)
	}
	defer second.Close()
	first.Close()
	if atts, err := e.st.Attempts(e.ctx, in.IntentID); err != nil || len(atts) != 1 || atts[0].SendsStarted != 0 {
		t.Fatalf("attempt after the fence was lost %+v %v", atts, err)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.attempts WHERE intent_id = $1`, in2.IntentID); got != 0 {
		t.Fatal("attempt written without the lock")
	}
}

// TestLockLossStopsSending (plan 13.2, 8.1): when the backend of a signer
// session is terminated, the session's lock is gone; the session can neither
// count a send nor persist an attempt, and a new session takes the signer.
func TestLockLossStopsSending(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	in := e.enqueue("p", "fence", signer, time.Hour)
	att := e.persist(ss, in.IntentID, 0, 100)

	var pid int
	if err := e.super.QueryRowContext(e.ctx, `SELECT pid FROM pg_locks WHERE locktype = 'advisory' AND granted
		AND classid = $1::bigint::oid AND objid = $2::bigint::oid AND objsubid = 2`, lockClassSigner, ss.key).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	var terminated bool
	if err := e.super.QueryRowContext(e.ctx, `SELECT pg_terminate_backend($1, 5000)`, pid).Scan(&terminated); err != nil || !terminated {
		t.Fatalf("terminate: %v %v", terminated, err)
	}
	if ok, err := ss.BeginSend(e.ctx, att); err == nil || ok {
		t.Fatalf("terminated backend counted a send: %v %v", ok, err)
	}
	in2 := e.enqueue("p", "fence-2", signer, time.Hour)
	if _, err := ss.PersistAttempt(e.ctx, draft(in2.IntentID, 1, 100)); err == nil {
		t.Fatal("terminated backend persisted an attempt")
	}
	ss.Close()
	atts, err := e.st.Attempts(e.ctx, in.IntentID)
	if err != nil || len(atts) != 1 || atts[0].SendsStarted != 0 {
		t.Fatalf("attempt after lock loss %+v %v", atts, err)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.attempts WHERE intent_id = $1`, in2.IntentID); got != 0 {
		t.Fatal("attempt written after lock loss")
	}
	third := e.lock(signer)
	defer third.Close()
	if ok, err := third.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatalf("new session: %v %v", ok, err)
	}
}

// TestImmediateStopKeepsCommittedAttempts (plan 13.2): committed attempt and
// send counts survive an immediate stop (SIGQUIT) and a SIGKILL of the
// postmaster with restart on the same data directory; an open uncommitted
// enqueue does not; the old signer session is fenced off.
func TestImmediateStopKeepsCommittedAttempts(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	callerDB := e.caller("p-sql", 8)
	signer := e.signer(1)
	ss := e.lock(signer)
	in := e.enqueue("p", "durable", signer, time.Hour)
	att := e.persist(ss, in.IntentID, 0, 100)
	if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := ss.RecordSendOutcome(e.ctx, att, SendOutcome{Class: SendAck}); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatal(err)
	}
	for round, crash := range []func() error{srv.ImmediateStop, srv.KillPostmaster} {
		tx, err := callerDB.BeginTx(e.ctx, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, _, err := sqlEnqueue(e.ctx, tx, "uncommitted-"+strings.Repeat("u", round+1), `{"v":1}`); err != nil {
			t.Fatal(err)
		}
		if err := crash(); err != nil {
			t.Fatalf("crash %d: %v", round, err)
		}
		tx.Rollback()
		if err := srv.Restart(e.ctx); err != nil {
			t.Fatalf("restart %d: %v", round, err)
		}
		atts, err := e.st.Attempts(e.ctx, in.IntentID)
		if err != nil || len(atts) != 1 || atts[0].Phase != AttemptAcknowledged || atts[0].SendsStarted != 2 ||
			atts[0].SendsRecorded != 1 || !atts[0].Acked {
			t.Fatalf("round %d attempt %+v %v", round, atts, err)
		}
		if st, phase, _ := e.state(in.IntentID); st != StateInFlight || phase != PhaseAcknowledged {
			t.Fatalf("round %d intent %s/%s", round, st, phase)
		}
		if got := e.count(`SELECT count(*) FROM broadcaster.intents WHERE entry = 'SQL'`); got != 0 {
			t.Fatalf("round %d: %d uncommitted SQL intents survived", round, got)
		}
		if got := e.count(`SELECT count(*) FROM broadcaster.transitions WHERE intent_id = $1`, in.IntentID); got != 3 {
			t.Fatalf("round %d: %d transitions", round, got)
		}
	}
	if ok, err := ss.BeginSend(e.ctx, att); err == nil || ok {
		t.Fatalf("pre-crash session counted a send: %v %v", ok, err)
	}
	ss.Close()
	fresh := e.lock(signer)
	defer fresh.Close()
	if ok, err := fresh.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatalf("fresh session after restart: %v %v", ok, err)
	}
}

// TestExpiryRacesPersist (plan 13.2, T4, T15, 8.3): expiry is read from the
// database clock. Before expiry Expire is refused; after it, persist is
// refused and Expire fails the intent EXPIRED_NOT_SENT, or EXPIRED_DEAD
// after an attempt died; an IN_FLIGHT intent past expiry keeps its live
// attempt, moves to AWAITING_DEADLINE, and sends no more. Expire and persist
// racing across the expiry instant never leave both an attempt and a FAILED
// row.
func TestExpiryRacesPersist(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	e.principal("p", 64)
	signer := e.signer(1)
	ss := e.lock(signer)
	defer ss.Close()
	lifetime := 5 * time.Second
	notSent := e.enqueue("p", "not-sent", signer, lifetime)
	deadOne := e.enqueue("p", "dead-one", signer, lifetime)
	inFlight := e.enqueue("p", "in-flight", signer, lifetime)
	if err := e.st.Expire(e.ctx, notSent.IntentID); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("expire before expiry: want ErrStateChanged, got %v", err)
	}
	att := e.persist(ss, deadOne.IntentID, 0, 100)
	killAttempt(t, ss, deadOne.IntentID, att, Expect{StateInFlight, PhaseSigned})
	if err := ss.SyncSequence(e.ctx, 7, 1, 10); err != nil {
		t.Fatal(err)
	}
	live := e.persist(ss, inFlight.IntentID, 1, 200)
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.AwaitDeadline(e.ctx, inFlight.IntentID, Expect{StateInFlight, PhaseSigned}); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("await deadline before expiry: want ErrStateChanged, got %v", err)
	}
	tx.Rollback()

	signers := make([][]byte, 8)
	racers := make([]EnqueueResult, 8)
	for i := range racers {
		signers[i] = e.signer(int32(100 + i))
		racers[i] = e.enqueue("p", "racer-"+strings.Repeat("r", i+1), signers[i], lifetime)
	}
	expiresAt := make([]time.Time, len(racers))
	for i := range racers {
		if err := e.super.QueryRowContext(e.ctx, `SELECT expires_at FROM broadcaster.intents WHERE intent_id = $1`,
			racers[i].IntentID).Scan(&expiresAt[i]); err != nil {
			t.Fatal(err)
		}
	}
	sessions := make([]*SignerSession, len(racers))
	for i := range racers {
		sessions[i] = e.lock(signers[i])
		defer sessions[i].Close()
	}
	// Racer i persists once at its expiry instant plus (i-4)*12 ms (from 48 ms
	// before to 36 ms after); an expirer retries Expire from 60 ms before.
	var wg sync.WaitGroup
	for i := range racers {
		wg.Add(2)
		go func() {
			defer wg.Done()
			time.Sleep(time.Until(expiresAt[i].Add(time.Duration(i-4) * 12 * time.Millisecond)))
			if _, err := sessions[i].PersistAttempt(e.ctx, draft(racers[i].IntentID, 0, 100)); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			time.Sleep(time.Until(expiresAt[i].Add(-60 * time.Millisecond)))
			deadline := time.Now().Add(500 * time.Millisecond)
			for time.Now().Before(deadline) {
				err := e.st.Expire(e.ctx, racers[i].IntentID)
				if err == nil {
					return
				}
				if !errors.Is(err, ErrStateChanged) {
					t.Error(err)
					return
				}
				time.Sleep(3 * time.Millisecond)
			}
		}()
	}
	wg.Wait()
	outcomes := map[string]int{}
	for i := range racers {
		st, _, reason := e.state(racers[i].IntentID)
		attempts := e.count(`SELECT count(*) FROM broadcaster.attempts WHERE intent_id = $1`, racers[i].IntentID)
		switch {
		case st == StateFailed && reason == "EXPIRED_NOT_SENT" && attempts == 0:
			outcomes["expired"]++
		case st == StateInFlight && attempts == 1:
			outcomes["persisted"]++
		default:
			t.Fatalf("racer %d: state %s reason %s attempts %d", i, st, reason, attempts)
		}
	}
	t.Logf("racers across the expiry instant: %v", outcomes)

	if res, err := ss.PersistAttempt(e.ctx, draft(notSent.IntentID, 2, 300)); err != nil || res.Refusal != RefusedExpired {
		t.Fatalf("persist after expiry: %+v %v", res, err)
	}
	if err := e.st.Expire(e.ctx, notSent.IntentID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.Expire(e.ctx, deadOne.IntentID); err != nil {
		t.Fatal(err)
	}
	if err := e.st.Expire(e.ctx, inFlight.IntentID); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("expire of an IN_FLIGHT intent: want ErrStateChanged, got %v", err)
	}
	for id, want := range map[string]string{notSent.IntentID: "EXPIRED_NOT_SENT", deadOne.IntentID: "EXPIRED_DEAD"} {
		if st, _, reason := e.state(id); st != StateFailed || reason != want {
			t.Fatalf("expired intent %s %s, want FAILED %s", st, reason, want)
		}
	}
	if ok, err := ss.BeginSend(e.ctx, live); err != nil || ok {
		t.Fatalf("send after expiry was allowed: %v %v", ok, err)
	}
	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.AwaitDeadline(e.ctx, inFlight.IntentID, Expect{StateInFlight, PhaseSigned}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if st, phase, reason := e.state(inFlight.IntentID); st != StateInFlight || phase != PhaseAwaitingDeadline || reason != "EXPIRED_AWAITING_CHAIN" {
		t.Fatalf("T15: %s/%s %s", st, phase, reason)
	}
	if atts, _ := e.st.Attempts(e.ctx, inFlight.IntentID); atts[0].Phase != AttemptSigned {
		t.Fatal("T15 abandoned the live attempt")
	}
	if ok, err := ss.BeginSend(e.ctx, live); err != nil || ok {
		t.Fatalf("send while AWAITING_DEADLINE: %v %v", ok, err)
	}
}

// TestSignerTxLifecycle: a session runs one transaction at a time. While a
// SignerTx is open, the session's other writes and a second Begin return
// ErrTxOpen instead of waiting on the connection, and Close rolls the open
// SignerTx back and releases the lock instead of blocking. Close ends the
// session's database backend, so no pooled connection keeps the lock, and a
// second Close is harmless.
func TestSignerTxLifecycle(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 4)
	signer := e.signer(1)
	in := e.enqueue("p", "life", signer, time.Hour)
	ss := e.lock(signer)
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ss.Begin(e.ctx); !errors.Is(err, ErrTxOpen) {
		t.Fatalf("second Begin: want ErrTxOpen, got %v", err)
	}
	if _, err := ss.PersistAttempt(e.ctx, draft(in.IntentID, 0, 100)); !errors.Is(err, ErrTxOpen) {
		t.Fatalf("PersistAttempt with an open SignerTx: want ErrTxOpen, got %v", err)
	}
	if err := ss.SyncSequence(e.ctx, 7, 0, 2); !errors.Is(err, ErrTxOpen) {
		t.Fatalf("SyncSequence with an open SignerTx: want ErrTxOpen, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("second Rollback: %v", err)
	}
	att := e.persist(ss, in.IntentID, 0, 100)
	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.MarkDead(e.ctx, att, DeadD3DeadlinePassed); err != nil {
		t.Fatal(err)
	}
	closed := make(chan error, 1)
	go func() { closed <- ss.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatalf("Close with an open SignerTx: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Close blocked on an open SignerTx")
	}
	if atts, err := e.st.Attempts(e.ctx, in.IntentID); err != nil || atts[0].Phase != AttemptSigned {
		t.Fatalf("the open SignerTx was not rolled back: %+v %v", atts, err)
	}
	next := e.lock(signer)
	var pid int
	if err := e.super.QueryRowContext(e.ctx, `SELECT pid FROM pg_locks WHERE locktype = 'advisory' AND granted
		AND classid = $1::bigint::oid AND objid = $2::bigint::oid AND objsubid = 2`, lockClassSigner, next.key).Scan(&pid); err != nil {
		t.Fatal(err)
	}
	if err := next.Close(); err != nil {
		t.Fatal(err)
	}
	// The session's backend ends: its connection is not returned to the pool.
	deadline := time.Now().Add(10 * time.Second)
	for e.count(`SELECT count(*) FROM pg_stat_activity WHERE pid = $1`, pid) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("signer session backend still present after Close")
		}
		time.Sleep(20 * time.Millisecond)
	}
	if err := next.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}
