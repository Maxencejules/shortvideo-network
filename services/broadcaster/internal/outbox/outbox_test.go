package outbox_test

// Unit tests of the state machine (plan 13.1, outbox). Every input is a
// hand-built typed observation; nothing here talks to a node or a database.

import (
	"errors"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
)

// lim holds the PROPOSED values of plan section 11.
var lim = outbox.Limits{
	IndexingMargin:        2,
	AttemptsPerBusinessID: 8,
	BusinessAttempts:      4,
	AttemptsPerIntent:     16,
	Sdk4Retries:           3,
}

var (
	okGuard = outbox.Guard{Due: true, SignerIdle: true, LockHeld: true}
	noEff   = outbox.Effect{Verdict: outbox.EffectNone}
	codeOK  = outbox.Code{}

	forbidden = outbox.Code{Codespace: outbox.CodespaceSVN, Number: 2}
	bounds    = outbox.Code{Codespace: outbox.CodespaceSVN, Number: 19}
	feeRej    = outbox.Code{Codespace: outbox.CodespaceSVN, Number: 21}
	sdkDecode = outbox.Code{Codespace: outbox.CodespaceSDK, Number: 2}
	notFound  = outbox.Code{Codespace: outbox.CodespaceSVN, Number: 16}
)

func hash(b byte) [32]byte {
	var h [32]byte
	h[0], h[31] = b, 0x5a
	return h
}

func newAttempt(b byte, seq, timeout uint64) outbox.Attempt {
	return outbox.Attempt{Hash: hash(b), Sequence: seq, TimeoutHeight: timeout, Phase: outbox.AttemptSigned}
}

func apply(t *testing.T, in outbox.Intent, ev outbox.Event) outbox.Step {
	t.Helper()
	st, err := outbox.Apply(in, ev, lim)
	if err != nil {
		t.Fatalf("Apply(%v/%v, %T): %v", in.State, in.Phase, ev, err)
	}
	return st
}

func judge(t *testing.T, a outbox.Attempt, v outbox.ChainView) outbox.Verdict {
	t.Helper()
	verdict, err := outbox.Judge(a, v, lim)
	if err != nil {
		t.Fatalf("Judge: %v", err)
	}
	return verdict
}

func queued(t *testing.T) outbox.Intent {
	t.Helper()
	return apply(t, outbox.Intent{}, outbox.Create{Validated: true}).After
}

// inFlight returns an intent with one persisted attempt (hash 1, sequence
// 7, timeout height 100) and that attempt.
func inFlight(t *testing.T) (outbox.Intent, outbox.Attempt) {
	t.Helper()
	st := apply(t, queued(t), outbox.Persist{Attempt: newAttempt(1, 7, 100), Effect: noEff, Guard: okGuard})
	if st.ID != outbox.T8 {
		t.Fatalf("persist gave %v", st.ID)
	}
	return st.After, st.Attempt
}

// sendOutcome counts and records one send of the live attempt.
func sendOutcome(t *testing.T, in outbox.Intent, a outbox.Attempt, s outbox.Send) (outbox.Intent, outbox.Attempt) {
	t.Helper()
	st := apply(t, in, outbox.StartSend{Attempt: a})
	if !st.SendStarted() {
		t.Fatal("send not counted")
	}
	st = apply(t, st.After, outbox.RecordSend{Attempt: st.Attempt, Send: s})
	return st.After, st.Attempt
}

func committedTx(code outbox.Code, acc outbox.AccSeq, typed, idem bool) outbox.TxResult {
	return outbox.TxResult{Found: true, HashVerified: true, Height: 90, Code: code, AccSeq: acc, TypedEvent: typed, Idempotent: idem}
}

var (
	ack     = outbox.Send{Outcome: outbox.SendAck}
	unknown = outbox.Send{Outcome: outbox.SendUnknown}
	cache   = outbox.Send{Outcome: outbox.SendCache}
	refused = outbox.Send{Outcome: outbox.SendNodeRefused}
	seq32   = outbox.Send{Outcome: outbox.SendMempoolRejected, Code: outbox.CodeSDKWrongSequence}
)

func rejected(c outbox.Code) outbox.Send {
	return outbox.Send{Outcome: outbox.SendMempoolRejected, Code: c}
}

// effects is one effect check result of each verdict, with variations.
func effects(attemptHash [32]byte) []outbox.Effect {
	return []outbox.Effect{
		{},
		noEff,
		{Verdict: outbox.EffectCommitted, Canonical: hash(200), Own: false},
		{Verdict: outbox.EffectCommitted, Canonical: hash(201), Own: true},
		{Verdict: outbox.EffectCommitted, Canonical: hash(202), DeadAttempt: true},
		{Verdict: outbox.EffectCommitted, Canonical: attemptHash, Own: true},
		{Verdict: outbox.EffectTaken},
		{Verdict: outbox.EffectStateChanged},
		{Verdict: outbox.EffectSignerConflict},
	}
}

// txResults is every combination of /tx observation fields.
func txResults() []outbox.TxResult {
	out := []outbox.TxResult{{}}
	for _, hv := range []bool{false, true} {
		for _, c := range []outbox.Code{codeOK, outbox.CodeSVNReplay, outbox.CodeSDKTimeoutHeight} {
			for _, acc := range []outbox.AccSeq{outbox.AccSeqAbsent, outbox.AccSeqThis, outbox.AccSeqOther} {
				for _, typed := range []bool{false, true} {
					for _, idem := range []bool{false, true} {
						out = append(out, outbox.TxResult{Found: true, HashVerified: hv, Height: 90, Code: c,
							AccSeq: acc, TypedEvent: typed, Idempotent: idem})
					}
				}
			}
		}
	}
	return out
}

// TestNoCommittedWithoutCommittedResult applies every /tx observation, with
// every effect check result, to every IN_FLIGHT phase, and every effect
// check to a REQUESTED intent. An intent becomes COMMITTED only through T11
// (a hash-checked code 0 result with this attempt's acc_seq and the typed
// event), T12 (an idempotent replay with the first commitment found) or T5
// (an effect check that found a commitment, not a DEAD attempt's).
func TestNoCommittedWithoutCommittedResult(t *testing.T) {
	base, a := inFlight(t)
	commits := 0
	for _, phase := range []outbox.Phase{outbox.PhaseSigned, outbox.PhaseUncertain, outbox.PhaseAcknowledged, outbox.PhaseAwaitingDeadline} {
		in := base
		in.Phase = phase
		live := a
		switch phase {
		case outbox.PhaseUncertain:
			live.Phase, live.Sends = outbox.AttemptUncertain, outbox.Sends{Started: 1, Recorded: 1, Unknown: 1, Last: unknown}
		case outbox.PhaseAcknowledged:
			live.Phase, live.Sends = outbox.AttemptAcknowledged, outbox.Sends{Started: 1, Recorded: 1, Acked: true, Last: ack}
		}
		for _, tx := range txResults() {
			// Height 95 is below timeout_height + L (102) and 102 is at it;
			// an ante failure resolves only at 102.
			for _, height := range []uint64{95, 102} {
				view := outbox.ChainView{Tx: tx, Height: height}
				v, err := outbox.Judge(live, view, lim)
				if err != nil {
					t.Fatalf("Judge(%+v): %v", tx, err)
				}
				for _, eff := range effects(live.Hash) {
					for _, expired := range []bool{false, true} {
						st, err := outbox.Apply(in, outbox.Settle{Verdict: v, Effect: eff, Expired: expired}, lim)
						if err != nil {
							if !errors.Is(err, outbox.ErrNeedEffect) {
								t.Fatalf("Settle: %v", err)
							}
							continue
						}
						if st.After.State != outbox.StateCommitted {
							continue
						}
						commits++
						okT11 := st.ID == outbox.T11 && tx.Found && tx.HashVerified && tx.Code.OK() &&
							tx.AccSeq == outbox.AccSeqThis && tx.TypedEvent && !tx.Idempotent &&
							st.After.Canonical == live.Hash && st.After.Own
						effFound := eff.Verdict == outbox.EffectCommitted && !eff.DeadAttempt &&
							st.After.Canonical == eff.Canonical && st.After.Own == eff.Own && eff.Canonical != live.Hash
						okT12 := st.ID == outbox.T12 && tx.Found && tx.HashVerified && tx.Code.OK() &&
							tx.AccSeq == outbox.AccSeqThis && tx.Idempotent && !tx.TypedEvent && effFound
						okT5 := st.ID == outbox.T5 && tx.Found && tx.HashVerified && !tx.Code.OK() && effFound
						if !okT11 && !okT12 && !okT5 {
							t.Fatalf("COMMITTED by %v from tx %+v effect %+v", st.ID, tx, eff)
						}
					}
				}
			}
		}
	}
	for _, eff := range effects(hash(250)) {
		st, err := outbox.Apply(queued(t), outbox.CheckEffect{Effect: eff}, lim)
		if err != nil {
			if eff.Verdict != 0 {
				t.Fatalf("CheckEffect(%+v): %v", eff, err)
			}
			continue
		}
		if st.After.State == outbox.StateCommitted {
			commits++
			if st.ID != outbox.T5 || eff.Verdict != outbox.EffectCommitted || eff.DeadAttempt || st.After.Canonical != eff.Canonical {
				t.Fatalf("REQUESTED COMMITTED by %v from %+v", st.ID, eff)
			}
		}
	}
	// Sends, expiry, validation, cancel and conflicts never commit.
	in, live := inFlight(t)
	for _, s := range []outbox.Send{ack, unknown, cache, refused, seq32, rejected(forbidden)} {
		after, _ := sendOutcome(t, in, live, s)
		if after.State == outbox.StateCommitted {
			t.Fatalf("send %+v committed", s)
		}
	}
	for _, ev := range []outbox.Event{outbox.Expire{}, outbox.SignerConflict{}} {
		if st := apply(t, in, ev); st.After.State == outbox.StateCommitted {
			t.Fatalf("%T committed", ev)
		}
	}
	if commits == 0 {
		t.Fatal("no path committed; the test checks nothing")
	}
}

// TestAckNeverCommits: an ACK on every live phase raises the phase to
// ACKNOWLEDGED (T9) and the intent stays SUBMITTED; without a /tx result the
// pass keeps the attempt live.
func TestAckNeverCommits(t *testing.T) {
	in, a := inFlight(t)
	for i := 0; i < 3; i++ {
		var phase outbox.Phase
		in, a = sendOutcome(t, in, a, ack)
		phase = in.Phase
		if in.State != outbox.StateInFlight || phase != outbox.PhaseAcknowledged || a.Phase != outbox.AttemptAcknowledged {
			t.Fatalf("after ACK %d: %v/%v attempt %v", i, in.State, phase, a.Phase)
		}
		view, err := outbox.Present(in, outbox.Overlay{ObserverFresh: true})
		if err != nil || view.Status != outbox.StatusSubmitted {
			t.Fatalf("ACKed intent shows %v (%v)", view.Status, err)
		}
	}
	d, err := outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: outbox.ChainView{Height: 50}}, lim)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range d.Steps {
		if st.After.State == outbox.StateCommitted {
			t.Fatal("an ACKed attempt with no /tx result committed")
		}
	}
	if !d.Rebroadcast {
		t.Fatal("a live ACKed attempt before expiry is not rebroadcast")
	}
	// T9 from UNCERTAIN.
	in2, a2 := inFlight(t)
	in2, a2 = sendOutcome(t, in2, a2, unknown)
	st := apply(t, in2, outbox.StartSend{Attempt: a2})
	st = apply(t, st.After, outbox.RecordSend{Attempt: st.Attempt, Send: ack})
	if st.ID != outbox.T9 || st.After.Phase != outbox.PhaseAcknowledged {
		t.Fatalf("ACK on UNCERTAIN gave %v %v", st.ID, st.After.Phase)
	}
}

// TestCommitNeedsTypedEvent: code 0 commits by T11 only with the typed
// event, this attempt's acc_seq and verified bytes; code 0 with neither the
// typed event nor the idempotent marker, or with both, is an integrity hold.
func TestCommitNeedsTypedEvent(t *testing.T) {
	in, a := inFlight(t)
	cases := []struct {
		name  string
		tx    outbox.TxResult
		kind  outbox.VerdictKind
		alert outbox.Alert
	}{
		{"typed event", committedTx(codeOK, outbox.AccSeqThis, true, false), outbox.VerdictResolved, 0},
		{"no event", committedTx(codeOK, outbox.AccSeqThis, false, false), outbox.VerdictHold, outbox.AlertResultInconsistent},
		{"event and marker", committedTx(codeOK, outbox.AccSeqThis, true, true), outbox.VerdictHold, outbox.AlertResultInconsistent},
		{"no acc_seq", committedTx(codeOK, outbox.AccSeqAbsent, true, false), outbox.VerdictHold, outbox.AlertResultInconsistent},
		{"foreign acc_seq", committedTx(codeOK, outbox.AccSeqOther, true, false), outbox.VerdictHold, outbox.AlertAccSeqForeign},
		{"bytes do not hash", outbox.TxResult{Found: true, Height: 90, AccSeq: outbox.AccSeqThis, TypedEvent: true}, outbox.VerdictHold, outbox.AlertHashMismatch},
	}
	for _, c := range cases {
		v := judge(t, a, outbox.ChainView{Tx: c.tx, Height: 95})
		if v.Kind() != c.kind || v.Alert() != c.alert {
			t.Fatalf("%s: verdict %v alert %v", c.name, v.Kind(), v.Alert())
		}
		st := apply(t, in, outbox.Settle{Verdict: v, Effect: noEff})
		switch c.kind {
		case outbox.VerdictResolved:
			if st.ID != outbox.T11 || st.After.State != outbox.StateCommitted || st.After.Canonical != a.Hash || !st.After.Own {
				t.Fatalf("%s: %v %v", c.name, st.ID, st.After.State)
			}
		default:
			if st.Changed() || st.HasAttempt || st.Halt != outbox.HaltIntegrity {
				t.Fatalf("%s: hold changed the intent or attempt, or did not halt", c.name)
			}
		}
	}
}

// TestIdempotentReplayNeedsFirstCommit: an idempotent replay commits by T12
// only after the effect check found the first commitment, which becomes the
// canonical evidence; until then the attempt and the intent hold.
func TestIdempotentReplayNeedsFirstCommit(t *testing.T) {
	in, a := inFlight(t)
	view := outbox.ChainView{Tx: committedTx(codeOK, outbox.AccSeqThis, false, true), Height: 95}
	v := judge(t, a, view)
	if v.Kind() != outbox.VerdictResolved || v.Attempt().Phase != outbox.AttemptCommittedIdempotent {
		t.Fatalf("verdict %v %v", v.Kind(), v.Attempt().Phase)
	}
	if _, err := outbox.Apply(in, outbox.Settle{Verdict: v}, lim); !errors.Is(err, outbox.ErrNeedEffect) {
		t.Fatalf("Settle without the effect check: %v", err)
	}
	d, err := outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: view}, lim)
	if err != nil || d.Need != outbox.NeedEffect || len(d.Steps) != 0 || d.Rebroadcast {
		t.Fatalf("Reconcile without the effect check: %+v %v", d, err)
	}
	holds := []struct {
		eff   outbox.Effect
		alert outbox.Alert
		halt  outbox.HaltReason
	}{
		{noEff, outbox.AlertFirstCommitNotFound, 0},
		{outbox.Effect{Verdict: outbox.EffectCommitted, Canonical: a.Hash, Own: true}, outbox.AlertEffectInconsistent, 0},
		{outbox.Effect{Verdict: outbox.EffectTaken}, outbox.AlertEffectInconsistent, 0},
		{outbox.Effect{Verdict: outbox.EffectStateChanged}, outbox.AlertEffectInconsistent, 0},
		{outbox.Effect{Verdict: outbox.EffectCommitted, Canonical: hash(9), DeadAttempt: true}, outbox.AlertDeadAttemptCommitted, outbox.HaltIntegrity},
		{outbox.Effect{Verdict: outbox.EffectSignerConflict}, outbox.AlertSignerConflict, outbox.HaltSignerConflict},
	}
	for _, h := range holds {
		st := apply(t, in, outbox.Settle{Verdict: v, Effect: h.eff})
		if st.Changed() || st.HasAttempt || st.Alert != h.alert || st.Halt != h.halt {
			t.Fatalf("effect %+v: %v changed=%v attempt=%v alert %v halt %v", h.eff, st.ID, st.Changed(), st.HasAttempt, st.Alert, st.Halt)
		}
	}
	for _, own := range []bool{false, true} {
		first := outbox.Effect{Verdict: outbox.EffectCommitted, Canonical: hash(77), Own: own}
		st := apply(t, in, outbox.Settle{Verdict: v, Effect: first})
		if st.ID != outbox.T12 || st.After.State != outbox.StateCommitted || st.After.Canonical != hash(77) || st.After.Own != own {
			t.Fatalf("first commitment found: %v %+v", st.ID, st.After)
		}
		if st.Attempt.Phase != outbox.AttemptCommittedIdempotent {
			t.Fatalf("attempt phase %v", st.Attempt.Phase)
		}
	}
}

// terminalIntents returns a COMMITTED and a FAILED intent.
func terminalIntents(t *testing.T) []outbox.Intent {
	t.Helper()
	in, a := inFlight(t)
	v := judge(t, a, outbox.ChainView{Tx: committedTx(codeOK, outbox.AccSeqThis, true, false), Height: 95})
	done := apply(t, in, outbox.Settle{Verdict: v}).After
	cancelled := apply(t, queued(t), outbox.Cancel{}).After
	return []outbox.Intent{done, cancelled}
}

// TestTerminalStatesAbsorb: every event on a COMMITTED or FAILED intent is
// T17: nothing changes, no attempt is written and no send is counted; a
// reconciliation pass absorbs too.
func TestTerminalStatesAbsorb(t *testing.T) {
	_, a := inFlight(t)
	v := judge(t, a, outbox.ChainView{Tx: committedTx(codeOK, outbox.AccSeqThis, true, false), Height: 95})
	events := []outbox.Event{
		outbox.FailValidation{Reason: outbox.ReasonInvalid}, outbox.Cancel{}, outbox.Expire{},
		outbox.CheckEffect{Effect: outbox.Effect{Verdict: outbox.EffectTaken}},
		outbox.Persist{Attempt: newAttempt(3, 8, 100), Effect: noEff, Guard: okGuard},
		outbox.StartSend{Attempt: a}, outbox.RecordSend{Attempt: a, Send: ack},
		outbox.Settle{Verdict: v}, outbox.SignerConflict{},
	}
	for _, in := range terminalIntents(t) {
		for _, ev := range events {
			st := apply(t, in, ev)
			if st.ID != outbox.T17 || st.Changed() || st.HasAttempt || st.SendStarted() {
				t.Fatalf("%v + %T: %v", in.State, ev, st.ID)
			}
		}
		if _, err := outbox.Apply(in, outbox.Create{}, lim); !errors.Is(err, outbox.ErrNotApplicable) {
			t.Fatalf("create over a terminal intent: %v", err)
		}
		d, err := outbox.Reconcile(outbox.Pass{Intent: in, Effect: noEff, Guard: okGuard}, lim)
		if err != nil || len(d.Steps) != 1 || d.Steps[0].ID != outbox.T17 || d.Start || d.Rebroadcast {
			t.Fatalf("Reconcile of %v: %+v %v", in.State, d, err)
		}
	}
}

// allIntents returns an intent in every state and phase.
func allIntents(t *testing.T) []outbox.Intent {
	t.Helper()
	in, a := inFlight(t)
	unc, _ := sendOutcome(t, in, a, unknown)
	acked, _ := sendOutcome(t, in, a, ack)
	awaiting := apply(t, in, outbox.Expire{}).After
	v := judge(t, a, outbox.ChainView{Height: 103, Account: &outbox.AccountRead{Sequence: 7, Height: 103}})
	retryWait := apply(t, in, outbox.Settle{Verdict: v, Effect: noEff}).After
	return append([]outbox.Intent{queued(t), retryWait, in, unc, acked, awaiting}, terminalIntents(t)...)
}

// TestStallOverlayNeverCommits: the stall overlay shows PENDING_STALL for a
// non-terminal intent while an episode is open and the observer is fresh,
// keeps the phase as detail, and never shows COMMITTED for an intent that is
// not COMMITTED or hides a terminal state.
func TestStallOverlayNeverCommits(t *testing.T) {
	for _, in := range allIntents(t) {
		for _, o := range []outbox.Overlay{{}, {StallOpen: true}, {ObserverFresh: true}, {StallOpen: true, ObserverFresh: true}} {
			view, err := outbox.Present(in, o)
			if err != nil {
				t.Fatal(err)
			}
			if (view.Status == outbox.StatusCommitted) != (in.State == outbox.StateCommitted) {
				t.Fatalf("%v/%v with %+v shows %v", in.State, in.Phase, o, view.Status)
			}
			wantStall := o.StallOpen && o.ObserverFresh && !in.State.Terminal()
			if (view.Status == outbox.StatusPendingStall) != wantStall || view.Stall != wantStall {
				t.Fatalf("%v/%v with %+v: status %v stall %v", in.State, in.Phase, o, view.Status, view.Stall)
			}
			if view.Phase != in.Phase {
				t.Fatalf("detail phase %v, want %v", view.Phase, in.Phase)
			}
		}
	}
}

// TestObserverStaleNotStall: with a stale observer an open stall episode is
// not shown; the underlying status and OBSERVER_UNAVAILABLE are.
func TestObserverStaleNotStall(t *testing.T) {
	want := map[outbox.State]outbox.Status{
		outbox.StateRequested: outbox.StatusRequested,
		outbox.StateInFlight:  outbox.StatusSubmitted,
		outbox.StateCommitted: outbox.StatusCommitted,
		outbox.StateFailed:    outbox.StatusFailed,
	}
	for _, in := range allIntents(t) {
		view, err := outbox.Present(in, outbox.Overlay{StallOpen: true})
		if err != nil {
			t.Fatal(err)
		}
		if view.Status != want[in.State] || view.Observer != outbox.ObserverUnavailable || view.Stall {
			t.Fatalf("%v/%v: %+v", in.State, in.Phase, view)
		}
		fresh, err := outbox.Present(in, outbox.Overlay{ObserverFresh: true})
		if err != nil || fresh.Status != want[in.State] || fresh.Observer != outbox.ObserverFresh {
			t.Fatalf("%v/%v fresh, no stall: %+v %v", in.State, in.Phase, fresh, err)
		}
	}
}

// TestDeadRuleD1D2D3 checks each clause of the dead rule and its near
// misses. The attempt has sequence 7 and timeout height 100; L is 2. Judge
// takes no clock: only heights, sequences, searches and recorded sends.
func TestDeadRuleD1D2D3(t *testing.T) {
	base := newAttempt(1, 7, 100)
	withSends := func(sends ...outbox.Send) outbox.Attempt {
		in, a := inFlight(t)
		for _, s := range sends {
			in, a = sendOutcome(t, in, a, s)
		}
		return a
	}
	// withStarted is the attempt with one counted send whose outcome was
	// never recorded.
	withStarted := func() outbox.Attempt {
		in, a := inFlight(t)
		return apply(t, in, outbox.StartSend{Attempt: a}).Attempt
	}
	acct := func(seq, h uint64) *outbox.AccountRead { return &outbox.AccountRead{Sequence: seq, Height: h} }
	cases := []struct {
		name  string
		a     outbox.Attempt
		view  outbox.ChainView
		kind  outbox.VerdictKind
		dead  outbox.DeadReason
		phase outbox.AttemptPhase
		need  outbox.Need
		alert outbox.Alert
		halt  outbox.HaltReason
	}{
		{name: "d1 execution failed", a: base, view: outbox.ChainView{Tx: committedTx(forbidden, outbox.AccSeqThis, false, false), Height: 95},
			kind: outbox.VerdictResolved, phase: outbox.AttemptExecutionFailed},
		{name: "d1 ante failed at T+L", a: base, view: outbox.ChainView{Tx: committedTx(outbox.CodeSDKTimeoutHeight, outbox.AccSeqAbsent, false, false), Height: 102},
			kind: outbox.VerdictResolved, phase: outbox.AttemptIncludedAnteFailed},
		{name: "d1 ante failed at T+L-1 stays live", a: base, view: outbox.ChainView{Tx: committedTx(outbox.CodeSDKOutOfGas, outbox.AccSeqAbsent, false, false), Height: 101},
			kind: outbox.VerdictLive, phase: outbox.AttemptSigned},
		{name: "d1 ante failed below T stays live", a: withSends(ack), view: outbox.ChainView{Tx: committedTx(forbidden, outbox.AccSeqAbsent, false, false), Height: 95},
			kind: outbox.VerdictLive, phase: outbox.AttemptAcknowledged},
		{name: "d1 ante failed with an unrecorded send is uncertain", a: withStarted(), view: outbox.ChainView{Tx: committedTx(outbox.CodeSDKOutOfGas, outbox.AccSeqAbsent, false, false), Height: 95},
			kind: outbox.VerdictLive, phase: outbox.AttemptUncertain},
		{name: "d1 execution failed below T is final", a: base, view: outbox.ChainView{Tx: committedTx(outbox.CodeSDKOutOfGas, outbox.AccSeqThis, false, false), Height: 91},
			kind: outbox.VerdictResolved, phase: outbox.AttemptExecutionFailed},
		{name: "d1 failure with an event is inconsistent", a: base, view: outbox.ChainView{Tx: committedTx(forbidden, outbox.AccSeqThis, true, false), Height: 95},
			kind: outbox.VerdictHold, phase: outbox.AttemptSigned, alert: outbox.AlertResultInconsistent, halt: outbox.HaltIntegrity},
		{name: "d2 one refusal", a: withSends(refused), view: outbox.ChainView{Height: 50},
			kind: outbox.VerdictDead, dead: outbox.DeadRefused, phase: outbox.AttemptDead},
		{name: "d2 two refusals", a: withSends(rejected(forbidden), refused), view: outbox.ChainView{Height: 50},
			kind: outbox.VerdictDead, dead: outbox.DeadRefused, phase: outbox.AttemptDead},
		{name: "refusal after UNKNOWN stays live", a: withSends(unknown, refused), view: outbox.ChainView{Height: 50},
			kind: outbox.VerdictLive, phase: outbox.AttemptUncertain},
		{name: "refusal after CACHE stays live", a: withSends(cache, refused), view: outbox.ChainView{Height: 50},
			kind: outbox.VerdictLive, phase: outbox.AttemptUncertain},
		{name: "refusal after ACK stays live", a: withSends(ack, refused), view: outbox.ChainView{Height: 50},
			kind: outbox.VerdictLive, phase: outbox.AttemptAcknowledged},
		{name: "d3 height T+L-1 stays live", a: base, view: outbox.ChainView{Height: 101, Account: acct(7, 101), Searched: true},
			kind: outbox.VerdictLive, phase: outbox.AttemptSigned},
		{name: "d3 account read below T+L needs a read", a: base, view: outbox.ChainView{Height: 102, Account: acct(7, 101)},
			kind: outbox.VerdictLive, phase: outbox.AttemptSigned, need: outbox.NeedAccount},
		{name: "d3 no account read", a: base, view: outbox.ChainView{Height: 102},
			kind: outbox.VerdictLive, phase: outbox.AttemptSigned, need: outbox.NeedAccount},
		{name: "d3 deadline passed, sequence unused", a: base, view: outbox.ChainView{Height: 102, Account: acct(7, 102)},
			kind: outbox.VerdictDead, dead: outbox.DeadDeadlinePassed, phase: outbox.AttemptDead},
		{name: "d3 deadline passed, account behind", a: base, view: outbox.ChainView{Height: 140, Account: acct(5, 130)},
			kind: outbox.VerdictDead, dead: outbox.DeadDeadlinePassed, phase: outbox.AttemptDead},
		{name: "d3 sequence advanced, not searched", a: base, view: outbox.ChainView{Height: 102, Account: acct(8, 102)},
			kind: outbox.VerdictLive, phase: outbox.AttemptSigned, need: outbox.NeedSearch},
		{name: "d3 sequence advanced, no hit", a: base, view: outbox.ChainView{Height: 102, Account: acct(8, 102), Searched: true},
			kind: outbox.VerdictLive, phase: outbox.AttemptSigned, alert: outbox.AlertSequenceAdvancedNoHit},
		{name: "d3 used by our other attempt", a: base, view: outbox.ChainView{Height: 102, Searched: true,
			Search: []outbox.Hit{{Hash: hash(2), Owner: outbox.OwnerOurs}}},
			kind: outbox.VerdictDead, dead: outbox.DeadSequenceUsed, phase: outbox.AttemptDead},
		{name: "d3 used by a cleared foreign transaction", a: base, view: outbox.ChainView{Height: 102, Searched: true,
			Search: []outbox.Hit{{Hash: hash(3), Owner: outbox.OwnerForeignCleared}}},
			kind: outbox.VerdictDead, dead: outbox.DeadSequenceUsed, phase: outbox.AttemptDead},
		{name: "d3 used by a foreign transaction", a: base, view: outbox.ChainView{Height: 102, Searched: true,
			Search: []outbox.Hit{{Hash: hash(3), Owner: outbox.OwnerForeign}}},
			kind: outbox.VerdictHold, phase: outbox.AttemptSigned, alert: outbox.AlertSignerConflict, halt: outbox.HaltSignerConflict},
		{name: "d3 hit below T+L is ignored", a: base, view: outbox.ChainView{Height: 101, Searched: true,
			Search: []outbox.Hit{{Hash: hash(3), Owner: outbox.OwnerForeign}}},
			kind: outbox.VerdictLive, phase: outbox.AttemptSigned},
		{name: "d3 search names this attempt", a: base, view: outbox.ChainView{Height: 102, Searched: true,
			Search: []outbox.Hit{{Hash: hash(1), Owner: outbox.OwnerOurs}}},
			kind: outbox.VerdictLive, phase: outbox.AttemptSigned, alert: outbox.AlertSearchInconsistent},
		{name: "d3 two hits", a: base, view: outbox.ChainView{Height: 102, Searched: true,
			Search: []outbox.Hit{{Hash: hash(2), Owner: outbox.OwnerOurs}, {Hash: hash(3), Owner: outbox.OwnerForeign}}},
			kind: outbox.VerdictLive, phase: outbox.AttemptSigned, alert: outbox.AlertSearchInconsistent},
	}
	for _, c := range cases {
		v := judge(t, c.a, c.view)
		got := v.Attempt()
		if v.Kind() != c.kind || got.Dead != c.dead || got.Phase != c.phase || v.Need() != c.need ||
			v.Alert() != c.alert || v.Halt() != c.halt {
			t.Errorf("%s: kind %v dead %v phase %v need %v alert %v halt %v", c.name, v.Kind(), got.Dead, got.Phase,
				v.Need(), v.Alert(), v.Halt())
		}
	}
	other, ok := judge(t, base, outbox.ChainView{Height: 102, Searched: true,
		Search: []outbox.Hit{{Hash: hash(2), Owner: outbox.OwnerOurs}}}).Other()
	if !ok || other != hash(2) {
		t.Fatal("sequence used by our attempt: the other attempt is not named")
	}
	if _, ok := judge(t, base, outbox.ChainView{Height: 102, Account: acct(7, 102)}).Other(); ok {
		t.Fatal("deadline death names another attempt")
	}
}

// TestZeroRecordedSendsNeverDeadByD2: with no started send, or with a
// started send whose outcome was never recorded, (d2) never holds, whatever
// the height.
func TestZeroRecordedSendsNeverDeadByD2(t *testing.T) {
	base := newAttempt(1, 7, 100)
	for _, h := range []uint64{1, 50, 101} {
		if v := judge(t, base, outbox.ChainView{Height: h}); v.Kind() != outbox.VerdictLive {
			t.Fatalf("height %d, no sends: %v", h, v.Kind())
		}
	}
	in, a := inFlight(t)
	st := apply(t, in, outbox.StartSend{Attempt: a})
	if v := judge(t, st.Attempt, outbox.ChainView{Height: 50}); v.Kind() != outbox.VerdictLive {
		t.Fatalf("one started, none recorded: %v", v.Kind())
	}
	if base.Sends.AllRefused() || st.Attempt.Sends.AllRefused() {
		t.Fatal("AllRefused with no recorded send")
	}
}

// TestUnrecordedSendCountsUnknown: a started send with no recorded outcome
// (a crash between sends_started and the record) blocks (d2) like an
// UNKNOWN answer and raises a SIGNED attempt and intent to UNCERTAIN.
func TestUnrecordedSendCountsUnknown(t *testing.T) {
	in, a := inFlight(t)
	in, a = sendOutcome(t, in, a, refused)
	st := apply(t, in, outbox.StartSend{Attempt: a}) // the record is lost
	in, a = st.After, st.Attempt
	if a.Sends.Unrecorded() != 1 || a.Sends.AllRefused() {
		t.Fatalf("sends %+v", a.Sends)
	}
	d, err := outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: outbox.ChainView{Height: 50}}, lim)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Steps) != 1 || d.Steps[0].ID != outbox.T10 || d.Steps[0].After.Phase != outbox.PhaseUncertain ||
		d.Steps[0].Attempt.Phase != outbox.AttemptUncertain || !d.Rebroadcast {
		t.Fatalf("decision %+v", d)
	}
	view, err := outbox.Present(d.Steps[0].After, outbox.Overlay{ObserverFresh: true})
	if err != nil || view.Status != outbox.StatusSubmitted || view.Phase != outbox.PhaseUncertain {
		t.Fatalf("view %+v %v", view, err)
	}
}

// TestSdk32AndCacheNeverDefinitive: sdk 32 and CACHE are not refusals, so a
// run of refusals with one of them never satisfies (d2).
func TestSdk32AndCacheNeverDefinitive(t *testing.T) {
	for _, s := range []outbox.Send{seq32, cache, unknown, ack} {
		if s.Refusal() {
			t.Fatalf("%+v counts as a refusal", s)
		}
	}
	for _, s := range []outbox.Send{refused, rejected(forbidden), rejected(outbox.CodeSDKUnauthorized), rejected(sdkDecode)} {
		if !s.Refusal() {
			t.Fatalf("%+v is not a refusal", s)
		}
	}
	for _, odd := range []outbox.Send{seq32, cache} {
		in, a := inFlight(t)
		in, a = sendOutcome(t, in, a, refused)
		in, a = sendOutcome(t, in, a, odd)
		_, a = sendOutcome(t, in, a, rejected(forbidden))
		if v := judge(t, a, outbox.ChainView{Height: 50}); v.Kind() != outbox.VerdictLive {
			t.Fatalf("refusals with %+v: %v", odd, v.Kind())
		}
	}
}

// TestDeadAttemptHashMatchIsIntegrityAlert: a commitment or sequence use by
// an attempt marked DEAD is a fatal integrity alert that halts the signer;
// it is never reported as a commitment by someone else.
func TestDeadAttemptHashMatchIsIntegrityAlert(t *testing.T) {
	dead := outbox.Effect{Verdict: outbox.EffectCommitted, Canonical: hash(9), DeadAttempt: true}
	st := apply(t, queued(t), outbox.CheckEffect{Effect: dead})
	if st.Changed() || st.Alert != outbox.AlertDeadAttemptCommitted || st.Halt != outbox.HaltIntegrity {
		t.Fatalf("REQUESTED: %v %v %v", st.ID, st.Alert, st.Halt)
	}
	in, a := inFlight(t)
	v := judge(t, a, outbox.ChainView{Height: 102, Account: &outbox.AccountRead{Sequence: 7, Height: 102}})
	st = apply(t, in, outbox.Settle{Verdict: v, Effect: dead})
	if st.Changed() || st.HasAttempt || st.Alert != outbox.AlertDeadAttemptCommitted || st.Halt != outbox.HaltIntegrity {
		t.Fatalf("Settle: %v %v %v", st.ID, st.Alert, st.Halt)
	}
	v = judge(t, a, outbox.ChainView{Height: 102, Searched: true, Search: []outbox.Hit{{Hash: hash(2), Owner: outbox.OwnerOursDead}}})
	if v.Kind() != outbox.VerdictHold || v.Alert() != outbox.AlertDeadAttemptCommitted || v.Halt() != outbox.HaltIntegrity {
		t.Fatalf("search hit by a DEAD attempt: %v %v", v.Kind(), v.Alert())
	}
	if st := apply(t, in, outbox.Settle{Verdict: v}); st.Changed() || st.HasAttempt {
		t.Fatal("hold changed something")
	}
}

// TestExpiryNeverAbandonsLiveAttempt: expiry moves an IN_FLIGHT intent to
// AWAITING_DEADLINE (T15) and keeps its attempt live; the attempt still
// commits (T11) or dies by chain proof (T14, EXPIRED_DEAD).
func TestExpiryNeverAbandonsLiveAttempt(t *testing.T) {
	in, a := inFlight(t)
	in, a = sendOutcome(t, in, a, ack)
	d, err := outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: outbox.ChainView{Height: 60}, Guard: outbox.Guard{Expired: true}}, lim)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Steps) != 1 || d.Steps[0].ID != outbox.T15 || d.Steps[0].After.State != outbox.StateInFlight || d.Rebroadcast {
		t.Fatalf("decision %+v", d)
	}
	awaiting := d.Steps[0].After
	view, err := outbox.Present(awaiting, outbox.Overlay{ObserverFresh: true})
	if err != nil || view.Status != outbox.StatusSubmitted || view.Reason != outbox.ReasonExpiredAwaitingChain {
		t.Fatalf("view %+v %v", view, err)
	}
	if st := apply(t, awaiting, outbox.Expire{}); st.ID != outbox.TNone || st.Changed() {
		t.Fatal("a second expiry changed the intent")
	}
	// Still live before T+L even though expired.
	d, err = outbox.Reconcile(outbox.Pass{Intent: awaiting, Live: &a, Chain: outbox.ChainView{Height: 101}, Guard: outbox.Guard{Expired: true}}, lim)
	if err != nil || len(d.Steps) != 0 || d.Rebroadcast {
		t.Fatalf("before T+L: %+v %v", d, err)
	}
	ok := outbox.Pass{Intent: awaiting, Live: &a, Chain: outbox.ChainView{Tx: committedTx(codeOK, outbox.AccSeqThis, true, false), Height: 101},
		Guard: outbox.Guard{Expired: true}}
	d, err = outbox.Reconcile(ok, lim)
	if err != nil || len(d.Steps) != 1 || d.Steps[0].ID != outbox.T11 {
		t.Fatalf("commit after expiry: %+v %v", d, err)
	}
	gone := outbox.Pass{Intent: awaiting, Live: &a, Chain: outbox.ChainView{Height: 102, Account: &outbox.AccountRead{Sequence: 7, Height: 102}},
		Effect: noEff, Guard: outbox.Guard{Expired: true}}
	d, err = outbox.Reconcile(gone, lim)
	if err != nil || len(d.Steps) != 1 || d.Steps[0].ID != outbox.T14 || d.Steps[0].After.Reason != outbox.ReasonExpiredDead {
		t.Fatalf("death after expiry: %+v %v", d, err)
	}
	// A hold keeps the attempt live; expiry still moves the intent to
	// AWAITING_DEADLINE and nothing is sent.
	conflict := outbox.Pass{Intent: in, Live: &a, Chain: outbox.ChainView{Height: 102, Searched: true,
		Search: []outbox.Hit{{Hash: hash(3), Owner: outbox.OwnerForeign}}}, Guard: outbox.Guard{Expired: true}}
	d, err = outbox.Reconcile(conflict, lim)
	if err != nil || len(d.Steps) != 2 || d.Steps[0].ID != outbox.T16 || d.Steps[0].HasAttempt ||
		d.Steps[1].ID != outbox.T15 || d.Rebroadcast {
		t.Fatalf("hold after expiry: %+v %v", d, err)
	}
}

// TestNoSendAfterExpiry: no send is counted after expiry or in
// AWAITING_DEADLINE, and a pass after expiry never asks for a rebroadcast.
func TestNoSendAfterExpiry(t *testing.T) {
	in, a := inFlight(t)
	if _, err := outbox.Apply(in, outbox.StartSend{Attempt: a, Expired: true}, lim); !errors.Is(err, outbox.ErrGuard) {
		t.Fatalf("send with expired: %v", err)
	}
	awaiting := apply(t, in, outbox.Expire{}).After
	if _, err := outbox.Apply(awaiting, outbox.StartSend{Attempt: a}, lim); !errors.Is(err, outbox.ErrGuard) {
		t.Fatalf("send in AWAITING_DEADLINE: %v", err)
	}
	for _, intent := range []outbox.Intent{in, awaiting} {
		d, err := outbox.Reconcile(outbox.Pass{Intent: intent, Live: &a, Chain: outbox.ChainView{Height: 10}, Guard: outbox.Guard{Expired: true}}, lim)
		if err != nil || d.Rebroadcast {
			t.Fatalf("%v: rebroadcast after expiry (%v)", intent.Phase, err)
		}
	}
	d, err := outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: outbox.ChainView{Height: 10}}, lim)
	if err != nil || !d.Rebroadcast {
		t.Fatalf("before expiry: %+v %v", d, err)
	}
}

// TestReconcileBeforeResend: a new attempt starts only after the effect
// check found no effect, for a first attempt and for a retry.
func TestReconcileBeforeResend(t *testing.T) {
	in, a := inFlight(t)
	v := judge(t, a, outbox.ChainView{Height: 102, Account: &outbox.AccountRead{Sequence: 7, Height: 102}})
	if _, err := outbox.Apply(in, outbox.Settle{Verdict: v}, lim); !errors.Is(err, outbox.ErrNeedEffect) {
		t.Fatalf("dead attempt without the effect check: %v", err)
	}
	retryWait := apply(t, in, outbox.Settle{Verdict: v, Effect: noEff}).After
	if retryWait.Phase != outbox.PhaseRetryWait {
		t.Fatalf("phase %v", retryWait.Phase)
	}
	for _, r := range []outbox.Intent{queued(t), retryWait} {
		d, err := outbox.Reconcile(outbox.Pass{Intent: r, Guard: okGuard}, lim)
		if err != nil || d.Start || d.Need != outbox.NeedEffect {
			t.Fatalf("%v without the effect check: %+v %v", r.Phase, d, err)
		}
		for _, eff := range effects(hash(1)) {
			if eff.Verdict == outbox.EffectNone {
				continue
			}
			_, err := outbox.Apply(r, outbox.Persist{Attempt: newAttempt(4, 7, 120), Effect: eff, Guard: okGuard}, lim)
			if err == nil {
				t.Fatalf("%v persisted with effect %+v", r.Phase, eff)
			}
		}
		d, err = outbox.Reconcile(outbox.Pass{Intent: r, Effect: noEff, Guard: okGuard}, lim)
		if err != nil || !d.Start || len(d.Steps) != 0 {
			t.Fatalf("%v with no effect: %+v %v", r.Phase, d, err)
		}
		st := apply(t, r, outbox.Persist{Attempt: newAttempt(4, 7, 120), Effect: noEff, Guard: okGuard})
		if st.ID != outbox.T8 || st.After.Attempts != r.Attempts+1 {
			t.Fatalf("persist: %v %+v", st.ID, st.After)
		}
	}
	unvalidated := apply(t, outbox.Intent{}, outbox.Create{}).After
	d, err := outbox.Reconcile(outbox.Pass{Intent: unvalidated, Effect: noEff, Guard: okGuard}, lim)
	if err != nil || d.Start || d.Need != outbox.NeedValidation {
		t.Fatalf("unvalidated: %+v %v", d, err)
	}
}

// TestResendCodeNeverMapsToFirstAttempt: a later attempt's committed failure
// is read for that attempt only. When the effect exists (committed by an
// earlier transaction), the intent is COMMITTED with that transaction as
// canonical, not FAILED with the later attempt's code; a /tx answer whose
// bytes are another transaction's is never applied.
func TestResendCodeNeverMapsToFirstAttempt(t *testing.T) {
	in, a1 := inFlight(t)
	v1 := judge(t, a1, outbox.ChainView{Height: 102, Account: &outbox.AccountRead{Sequence: 7, Height: 102}})
	in = apply(t, in, outbox.Settle{Verdict: v1, Effect: noEff}).After
	st := apply(t, in, outbox.Persist{Attempt: newAttempt(2, 7, 140), Effect: noEff, Guard: okGuard})
	in, a2 := st.After, st.Attempt
	v2 := judge(t, a2, outbox.ChainView{Tx: committedTx(forbidden, outbox.AccSeqThis, false, false), Height: 130})
	first := outbox.Effect{Verdict: outbox.EffectCommitted, Canonical: hash(50)}
	st = apply(t, in, outbox.Settle{Verdict: v2, Effect: first})
	if st.ID != outbox.T5 || st.After.State != outbox.StateCommitted || st.After.Canonical != hash(50) || st.After.Own {
		t.Fatalf("effect exists: %v %+v", st.ID, st.After)
	}
	if st.Attempt.Hash != a2.Hash || st.Attempt.Phase != outbox.AttemptExecutionFailed {
		t.Fatalf("the resolution is not the later attempt's: %+v", st.Attempt)
	}
	mismatch := outbox.TxResult{Found: true, Height: 130, Code: forbidden, AccSeq: outbox.AccSeqThis}
	if v := judge(t, a2, outbox.ChainView{Tx: mismatch, Height: 130}); v.Kind() != outbox.VerdictHold || v.Alert() != outbox.AlertHashMismatch {
		t.Fatalf("other bytes: %v %v", v.Kind(), v.Alert())
	}
}

// TestCommittedFailureRunsEffectCheck: a committed failure waits for the
// effect check, which decides first (T5, T6, T7); with no effect the code
// decides (T13).
func TestCommittedFailureRunsEffectCheck(t *testing.T) {
	in, a := inFlight(t)
	view := outbox.ChainView{Tx: committedTx(forbidden, outbox.AccSeqThis, false, false), Height: 95}
	d, err := outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: view}, lim)
	if err != nil || d.Need != outbox.NeedEffect || len(d.Steps) != 0 || d.Rebroadcast {
		t.Fatalf("without the effect check: %+v %v", d, err)
	}
	cases := []struct {
		eff    outbox.Effect
		id     outbox.TransitionID
		state  outbox.State
		reason outbox.Reason
	}{
		{outbox.Effect{Verdict: outbox.EffectCommitted, Canonical: hash(60)}, outbox.T5, outbox.StateCommitted, 0},
		{outbox.Effect{Verdict: outbox.EffectTaken}, outbox.T6, outbox.StateFailed, outbox.ReasonObjectTaken},
		{outbox.Effect{Verdict: outbox.EffectStateChanged}, outbox.T7, outbox.StateFailed, outbox.ReasonObjectStateChanged},
		{noEff, outbox.T13, outbox.StateFailed, outbox.ReasonExecutionFailed},
	}
	for _, c := range cases {
		d, err := outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: view, Effect: c.eff}, lim)
		if err != nil || len(d.Steps) != 1 {
			t.Fatalf("%+v: %+v %v", c.eff, d, err)
		}
		st := d.Steps[0]
		if st.ID != c.id || st.After.State != c.state || st.After.Reason != c.reason || st.Attempt.Phase != outbox.AttemptExecutionFailed {
			t.Fatalf("%+v: %v %+v", c.eff, st.ID, st.After)
		}
		if c.id == outbox.T13 && st.After.Code != forbidden {
			t.Fatalf("T13 code %v", st.After.Code)
		}
	}
	// An ante failure leaves the bytes valid until their timeout_height:
	// below timeout_height + L the attempt stays live, nothing is stored,
	// no effect check is asked for, and the same bytes are rebroadcast.
	early := outbox.ChainView{Tx: committedTx(outbox.CodeSDKOutOfGas, outbox.AccSeqAbsent, false, false), Height: 95}
	d, err = outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: early}, lim)
	if err != nil || len(d.Steps) != 0 || d.Need != 0 || !d.Rebroadcast || d.Start {
		t.Fatalf("ante failure below T+L: %+v %v", d, err)
	}
	ante := outbox.ChainView{Tx: committedTx(outbox.CodeSDKTimeoutHeight, outbox.AccSeqAbsent, false, false), Height: 102}
	d, err = outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: ante, Effect: noEff}, lim)
	if err != nil || len(d.Steps) != 1 || d.Steps[0].ID != outbox.T13 || d.Steps[0].After.Phase != outbox.PhaseRetryWait ||
		d.Steps[0].Attempt.Phase != outbox.AttemptIncludedAnteFailed {
		t.Fatalf("ante failure sdk 30: %+v %v", d, err)
	}
}

// TestStateChangeWithoutTxIsObjectStateChanged: a state change with no
// committing transaction of the kind is FAILED(OBJECT_STATE_CHANGED) and
// never COMMITTED.
func TestStateChangeWithoutTxIsObjectStateChanged(t *testing.T) {
	changed := outbox.Effect{Verdict: outbox.EffectStateChanged}
	st := apply(t, queued(t), outbox.CheckEffect{Effect: changed})
	if st.ID != outbox.T7 || st.After.Reason != outbox.ReasonObjectStateChanged {
		t.Fatalf("REQUESTED: %v %+v", st.ID, st.After)
	}
	in, a := inFlight(t)
	v := judge(t, a, outbox.ChainView{Height: 102, Account: &outbox.AccountRead{Sequence: 7, Height: 102}})
	st = apply(t, in, outbox.Settle{Verdict: v, Effect: changed})
	if st.ID != outbox.T7 || st.After.Reason != outbox.ReasonObjectStateChanged || st.Attempt.Phase != outbox.AttemptDead {
		t.Fatalf("dead attempt: %v %+v", st.ID, st.After)
	}
	idem := judge(t, a, outbox.ChainView{Tx: committedTx(codeOK, outbox.AccSeqThis, false, true), Height: 95})
	if st := apply(t, in, outbox.Settle{Verdict: idem, Effect: changed}); st.After.State == outbox.StateCommitted || st.Changed() {
		t.Fatalf("idempotent replay with a state change: %v", st.ID)
	}
}

// TestReplayOpensBoundedBusinessAttempt: a committed REPLAY with no effect
// opens a new business attempt, up to the bound, then BUSINESS_ID_TAKEN.
func TestReplayOpensBoundedBusinessAttempt(t *testing.T) {
	in := queued(t)
	view := outbox.ChainView{Tx: committedTx(outbox.CodeSVNReplay, outbox.AccSeqThis, false, false), Height: 95}
	for n := 1; ; n++ {
		st := apply(t, in, outbox.Persist{Attempt: newAttempt(byte(n), uint64(n), 100), Effect: noEff, Guard: okGuard})
		in = st.After
		v := judge(t, st.Attempt, view)
		st = apply(t, in, outbox.Settle{Verdict: v, Effect: noEff})
		if n < lim.BusinessAttempts {
			if st.ID != outbox.T13 || st.After.Phase != outbox.PhaseRetryWait || !st.NewBusinessAttempt ||
				st.After.Business != n+1 || st.After.BusinessTries != 0 {
				t.Fatalf("replay %d: %v %+v", n, st.ID, st.After)
			}
			in = st.After
			continue
		}
		if st.After.State != outbox.StateFailed || st.After.Reason != outbox.ReasonBusinessIDTaken || st.NewBusinessAttempt {
			t.Fatalf("replay at the bound: %+v", st.After)
		}
		break
	}
	in, a := inFlight(t)
	v := judge(t, a, view)
	if st := apply(t, in, outbox.Settle{Verdict: v, Effect: noEff, Expired: true}); st.After.Reason != outbox.ReasonExpiredDead {
		t.Fatalf("replay after expiry: %+v", st.After)
	}
	one := lim
	one.AttemptsPerIntent = 1
	st, err := outbox.Apply(in, outbox.Settle{Verdict: v, Effect: noEff}, one)
	if err != nil || st.After.Reason != outbox.ReasonAttemptsExhausted {
		t.Fatalf("replay at the attempt cap: %+v %v", st.After, err)
	}
}

// TestCancelOnlyBeforeSigning: cancel is T3 only for a REQUESTED intent with
// no persisted attempt.
func TestCancelOnlyBeforeSigning(t *testing.T) {
	st := apply(t, queued(t), outbox.Cancel{})
	if st.ID != outbox.T3 || st.After.Reason != outbox.ReasonCancelled {
		t.Fatalf("queued: %v", st.ID)
	}
	in, a := inFlight(t)
	if _, err := outbox.Apply(in, outbox.Cancel{}, lim); !errors.Is(err, outbox.ErrGuard) {
		t.Fatalf("IN_FLIGHT: %v", err)
	}
	v := judge(t, a, outbox.ChainView{Height: 102, Account: &outbox.AccountRead{Sequence: 7, Height: 102}})
	retryWait := apply(t, in, outbox.Settle{Verdict: v, Effect: noEff}).After
	if _, err := outbox.Apply(retryWait, outbox.Cancel{}, lim); !errors.Is(err, outbox.ErrGuard) {
		t.Fatalf("RETRY_WAIT after an attempt: %v", err)
	}
}

// TestPhaseNeverLosesKnowledge: send outcomes only raise an attempt's live
// phase and never change a terminal one; an intent's phase follows (T9,
// T10) and never falls back.
func TestPhaseNeverLosesKnowledge(t *testing.T) {
	outcomes := []outbox.SendOutcome{outbox.SendUnknown, outbox.SendCache, outbox.SendNodeRefused, outbox.SendMempoolRejected, outbox.SendAck}
	for p := outbox.AttemptSigned; p <= outbox.AttemptDead; p++ {
		for _, o := range outcomes {
			got := outbox.RaisePhase(p, o)
			if !p.Live() && got != p {
				t.Fatalf("terminal %v became %v", p, got)
			}
			if p.Live() && (got < p || !got.Live()) {
				t.Fatalf("%v + %v = %v", p, o, got)
			}
		}
	}
	if outbox.RaisePhase(outbox.AttemptAcknowledged, outbox.SendCache) != outbox.AttemptAcknowledged ||
		outbox.RaisePhase(outbox.AttemptAcknowledged, outbox.SendUnknown) != outbox.AttemptAcknowledged ||
		outbox.RaisePhase(outbox.AttemptSigned, outbox.SendCache) != outbox.AttemptUncertain {
		t.Fatal("raise table")
	}
	in, a := inFlight(t)
	in, a = sendOutcome(t, in, a, ack)
	for _, s := range []outbox.Send{unknown, cache, refused, seq32} {
		st := apply(t, in, outbox.StartSend{Attempt: a})
		st = apply(t, st.After, outbox.RecordSend{Attempt: st.Attempt, Send: s})
		if st.After.Phase != outbox.PhaseAcknowledged || st.Attempt.Phase != outbox.AttemptAcknowledged || st.ID != outbox.TNone {
			t.Fatalf("%+v after ACK: %v %v %v", s, st.ID, st.After.Phase, st.Attempt.Phase)
		}
		in, a = st.After, st.Attempt
	}
	in2, a2 := inFlight(t)
	st := apply(t, in2, outbox.StartSend{Attempt: a2})
	st = apply(t, st.After, outbox.RecordSend{Attempt: st.Attempt, Send: cache})
	if st.ID != outbox.T10 || st.After.Phase != outbox.PhaseUncertain {
		t.Fatalf("CACHE on SIGNED: %v %v", st.ID, st.After.Phase)
	}
}

// TestStallBlocksNewAttempts: while a stall episode is open no new attempt
// starts, and a live attempt is still rebroadcast before expiry.
func TestStallBlocksNewAttempts(t *testing.T) {
	stalled := okGuard
	stalled.Stalled = true
	d, err := outbox.Reconcile(outbox.Pass{Intent: queued(t), Effect: noEff, Guard: stalled}, lim)
	if err != nil || d.Start {
		t.Fatalf("stalled: %+v %v", d, err)
	}
	if _, err := outbox.Apply(queued(t), outbox.Persist{Attempt: newAttempt(1, 7, 100), Effect: noEff, Guard: stalled}, lim); !errors.Is(err, outbox.ErrGuard) {
		t.Fatalf("persist while stalled: %v", err)
	}
	in, a := inFlight(t)
	d, err = outbox.Reconcile(outbox.Pass{Intent: in, Live: &a, Chain: outbox.ChainView{Height: 10}, Guard: stalled}, lim)
	if err != nil || !d.Rebroadcast {
		t.Fatalf("live attempt while stalled: %+v %v", d, err)
	}
}

// TestPersistGuards: each condition of 8.3 step 1 and the per-signer window
// refuses T8 on its own.
func TestPersistGuards(t *testing.T) {
	q := queued(t)
	att := newAttempt(1, 7, 100)
	unvalidated := apply(t, outbox.Intent{}, outbox.Create{}).After
	full := lim
	full.AttemptsPerIntent, full.AttemptsPerBusinessID = 1, 1
	in, _ := inFlight(t)
	cases := []struct {
		name   string
		in     outbox.Intent
		guard  outbox.Guard
		limits outbox.Limits
	}{
		{"expired", q, outbox.Guard{Expired: true, Due: true, SignerIdle: true, LockHeld: true}, lim},
		{"not due", q, outbox.Guard{SignerIdle: true, LockHeld: true}, lim},
		{"halted", q, outbox.Guard{Due: true, SignerHalted: true, SignerIdle: true, LockHeld: true}, lim},
		{"signer busy", q, outbox.Guard{Due: true, LockHeld: true}, lim},
		{"no lock", q, outbox.Guard{Due: true, SignerIdle: true}, lim},
		{"unvalidated", unvalidated, okGuard, lim},
		{"in flight", in, okGuard, lim},
	}
	for _, c := range cases {
		if _, err := outbox.Apply(c.in, outbox.Persist{Attempt: att, Effect: noEff, Guard: c.guard}, c.limits); !errors.Is(err, outbox.ErrGuard) {
			t.Errorf("%s: %v", c.name, err)
		}
	}
	// The caps: one attempt persisted, then the per-intent and per-business
	// caps of one refuse a second.
	retry := q
	retry.Phase, retry.Attempts, retry.BusinessTries = outbox.PhaseRetryWait, 1, 1
	if _, err := outbox.Apply(retry, outbox.Persist{Attempt: att, Effect: noEff, Guard: okGuard}, full); !errors.Is(err, outbox.ErrGuard) {
		t.Errorf("caps: %v", err)
	}
	perBusiness := lim
	perBusiness.AttemptsPerBusinessID = 1
	if _, err := outbox.Apply(retry, outbox.Persist{Attempt: att, Effect: noEff, Guard: okGuard}, perBusiness); !errors.Is(err, outbox.ErrGuard) {
		t.Errorf("business cap: %v", err)
	}
	sent := att
	sent.Sends = outbox.Sends{Started: 1}
	if _, err := outbox.Apply(q, outbox.Persist{Attempt: sent, Effect: noEff, Guard: okGuard}, lim); err == nil {
		t.Error("a new attempt with a counted send was persisted")
	}
}

// TestRetryRules checks the retry classification of plan 8.7 that T13 and
// T14 use.
func TestRetryRules(t *testing.T) {
	type outcome struct {
		state  outbox.State
		reason outbox.Reason
		code   outbox.Code
		halt   outbox.HaltReason
		alert  outbox.Alert
		wait   uint64
	}
	retry := outcome{state: outbox.StateRequested}
	committed := []struct {
		code outbox.Code
		acc  outbox.AccSeq
		want outcome
	}{
		{outbox.CodeSDKTimeoutHeight, outbox.AccSeqAbsent, retry},
		{outbox.CodeSDKTimeout, outbox.AccSeqAbsent, retry},
		{outbox.CodeSDKWrongSequence, outbox.AccSeqAbsent, retry},
		{outbox.CodeSDKOutOfGas, outbox.AccSeqAbsent, retry},
		{forbidden, outbox.AccSeqAbsent, outcome{state: outbox.StateFailed, reason: outbox.ReasonExecutionFailed, code: forbidden}},
		{outbox.CodeSDKOutOfGas, outbox.AccSeqThis, outcome{state: outbox.StateFailed, reason: outbox.ReasonExecutionFailed, code: outbox.CodeSDKOutOfGas}},
		{notFound, outbox.AccSeqThis, outcome{state: outbox.StateFailed, reason: outbox.ReasonExecutionFailed, code: notFound}},
	}
	for _, c := range committed {
		in, a := inFlight(t)
		v := judge(t, a, outbox.ChainView{Tx: committedTx(c.code, c.acc, false, false), Height: 102})
		st := apply(t, in, outbox.Settle{Verdict: v, Effect: noEff})
		got := outcome{st.After.State, st.After.Reason, st.After.Code, st.Halt, st.Alert, st.WaitBlocks}
		if st.ID != outbox.T13 || got != c.want {
			t.Errorf("committed %v %v: %v %+v", c.code, c.acc, st.ID, got)
		}
	}
	refusals := []struct {
		send outbox.Send
		want outcome
	}{
		{refused, retry},
		{rejected(outbox.CodeSDKUnauthorized), outcome{state: outbox.StateRequested, wait: outbox.Sdk4RetryGapBlocks}},
		{rejected(outbox.CodeSVNUnauthorized), outcome{state: outbox.StateFailed, reason: outbox.ReasonRejected, code: outbox.CodeSVNUnauthorized, halt: outbox.HaltUnauthorized}},
		{rejected(forbidden), outcome{state: outbox.StateFailed, reason: outbox.ReasonRejected, code: forbidden}},
		{rejected(bounds), outcome{state: outbox.StateFailed, reason: outbox.ReasonRejected, code: bounds}},
		{rejected(feeRej), outcome{state: outbox.StateFailed, reason: outbox.ReasonRejected, code: feeRej}},
		{rejected(sdkDecode), outcome{state: outbox.StateFailed, reason: outbox.ReasonRejected, code: sdkDecode}},
	}
	for _, c := range refusals {
		in, a := inFlight(t)
		in, a = sendOutcome(t, in, a, c.send)
		v := judge(t, a, outbox.ChainView{Height: 50})
		st := apply(t, in, outbox.Settle{Verdict: v, Effect: noEff})
		got := outcome{st.After.State, st.After.Reason, st.After.Code, st.Halt, st.Alert, st.WaitBlocks}
		if st.ID != outbox.T14 || got != c.want {
			t.Errorf("refusal %+v: %v %+v", c.send, st.ID, got)
		}
	}
	// sdk 4: Sdk4Retries deaths retry, the next fails with an alert.
	in := queued(t)
	for n := 1; ; n++ {
		st := apply(t, in, outbox.Persist{Attempt: newAttempt(byte(n), 7, 100), Effect: noEff, Guard: okGuard})
		cur, a := sendOutcome(t, st.After, st.Attempt, rejected(outbox.CodeSDKUnauthorized))
		st = apply(t, cur, outbox.Settle{Verdict: judge(t, a, outbox.ChainView{Height: 50}), Effect: noEff})
		if st.After.Sdk4Deaths != n {
			t.Fatalf("sdk 4 deaths %d after %d", st.After.Sdk4Deaths, n)
		}
		if n <= lim.Sdk4Retries {
			if st.After.Phase != outbox.PhaseRetryWait || st.WaitBlocks != outbox.Sdk4RetryGapBlocks {
				t.Fatalf("sdk 4 death %d: %+v", n, st.After)
			}
			in = st.After
			continue
		}
		if st.After.Reason != outbox.ReasonRejected || st.Alert != outbox.AlertSdk4Exhausted || st.WaitBlocks != 0 {
			t.Fatalf("sdk 4 past the bound: %+v alert %v", st.After, st.Alert)
		}
		break
	}
	// Caps and expiry for a retryable death.
	in2, a2 := inFlight(t)
	gone := judge(t, a2, outbox.ChainView{Height: 102, Account: &outbox.AccountRead{Sequence: 7, Height: 102}})
	if st := apply(t, in2, outbox.Settle{Verdict: gone, Effect: noEff, Expired: true}); st.After.Reason != outbox.ReasonExpiredDead {
		t.Fatalf("expired: %+v", st.After)
	}
	for _, capped := range []outbox.Limits{{IndexingMargin: 2, AttemptsPerBusinessID: 8, BusinessAttempts: 4, AttemptsPerIntent: 1, Sdk4Retries: 3},
		{IndexingMargin: 2, AttemptsPerBusinessID: 1, BusinessAttempts: 4, AttemptsPerIntent: 16, Sdk4Retries: 3}} {
		st, err := outbox.Apply(in2, outbox.Settle{Verdict: gone, Effect: noEff}, capped)
		if err != nil || st.After.Reason != outbox.ReasonAttemptsExhausted {
			t.Fatalf("cap %+v: %+v %v", capped, st.After, err)
		}
	}
}

// TestValidationFailureAndConflict covers T2 and the T16 event.
func TestValidationFailureAndConflict(t *testing.T) {
	for _, r := range []outbox.Reason{outbox.ReasonUnauthorized, outbox.ReasonInvalid, outbox.ReasonProviderSignatureInvalid} {
		st := apply(t, queued(t), outbox.FailValidation{Reason: r})
		if st.ID != outbox.T2 || st.After.Reason != r {
			t.Fatalf("%v: %v", r, st.ID)
		}
	}
	if _, err := outbox.Apply(queued(t), outbox.FailValidation{Reason: outbox.ReasonCancelled}, lim); err == nil {
		t.Fatal("CANCELLED accepted as a validation failure")
	}
	in, _ := inFlight(t)
	if _, err := outbox.Apply(in, outbox.FailValidation{Reason: outbox.ReasonInvalid}, lim); !errors.Is(err, outbox.ErrGuard) {
		t.Fatalf("validation failure with a live attempt: %v", err)
	}
	st := apply(t, in, outbox.SignerConflict{})
	if st.ID != outbox.T16 || st.Changed() || st.Halt != outbox.HaltSignerConflict {
		t.Fatalf("conflict: %v", st.ID)
	}
	st = apply(t, queued(t), outbox.CheckEffect{Effect: outbox.Effect{Verdict: outbox.EffectSignerConflict}})
	if st.ID != outbox.T16 || st.Changed() || st.Halt != outbox.HaltSignerConflict {
		t.Fatalf("effect conflict: %v", st.ID)
	}
	if _, err := outbox.Apply(in, outbox.CheckEffect{Effect: noEff}, lim); !errors.Is(err, outbox.ErrNotApplicable) {
		t.Fatalf("effect check on IN_FLIGHT: %v", err)
	}
	if _, err := outbox.Apply(queued(t), outbox.CheckEffect{}, lim); !errors.Is(err, outbox.ErrNeedEffect) {
		t.Fatalf("unchecked effect: %v", err)
	}
}

// TestEventsNeedTheirState: send and settle events need an IN_FLIGHT intent
// and a live attempt.
func TestEventsNeedTheirState(t *testing.T) {
	q := queued(t)
	in, a := inFlight(t)
	v := judge(t, a, outbox.ChainView{Height: 10})
	for _, ev := range []outbox.Event{outbox.StartSend{Attempt: a}, outbox.RecordSend{Attempt: a, Send: ack}, outbox.Settle{Verdict: v}} {
		if _, err := outbox.Apply(q, ev, lim); !errors.Is(err, outbox.ErrNotApplicable) {
			t.Fatalf("%T on REQUESTED: %v", ev, err)
		}
	}
	done := a
	done.Phase = outbox.AttemptCommittedOK
	if _, err := outbox.Apply(in, outbox.StartSend{Attempt: done}, lim); !errors.Is(err, outbox.ErrNotApplicable) {
		t.Fatalf("send of a resolved attempt: %v", err)
	}
	if _, err := outbox.Apply(in, outbox.RecordSend{Attempt: a, Send: ack}, lim); err == nil {
		t.Fatal("an outcome without a started send was recorded")
	}
	if _, err := outbox.Apply(in, outbox.Settle{}, lim); err == nil {
		t.Fatal("a Settle without a verdict was applied")
	}
	if _, err := outbox.Apply(in, nil, lim); err == nil {
		t.Fatal("nil event applied")
	}
	if _, err := outbox.Judge(done, outbox.ChainView{}, lim); err == nil {
		t.Fatal("Judge of a resolved attempt")
	}
	if _, err := outbox.Reconcile(outbox.Pass{Intent: in}, lim); err == nil {
		t.Fatal("Reconcile of an IN_FLIGHT intent without its attempt")
	}
	if _, err := outbox.Reconcile(outbox.Pass{Intent: q, Live: &a}, lim); err == nil {
		t.Fatal("Reconcile of a REQUESTED intent with a live attempt")
	}
}

// TestInputBounds: malformed or out-of-bound inputs are refused.
func TestInputBounds(t *testing.T) {
	good := newAttempt(1, 7, 100)
	view := outbox.ChainView{Height: 10}
	attempts := map[string]func(*outbox.Attempt){
		"zero hash":            func(a *outbox.Attempt) { a.Hash = [32]byte{} },
		"zero timeout":         func(a *outbox.Attempt) { a.TimeoutHeight = 0 },
		"timeout above bound":  func(a *outbox.Attempt) { a.TimeoutHeight = outbox.MaxHeight + 1 },
		"sequence above bound": func(a *outbox.Attempt) { a.Sequence = outbox.MaxSequence + 1 },
		"unknown phase":        func(a *outbox.Attempt) { a.Phase = 99 },
		"dead reason live":     func(a *outbox.Attempt) { a.Dead = outbox.DeadRefused },
		"recorded > started":   func(a *outbox.Attempt) { a.Sends = outbox.Sends{Recorded: 1, Last: refused, Refusals: 1} },
		"negative counter":     func(a *outbox.Attempt) { a.Sends = outbox.Sends{Started: -1} },
		"counter above bound":  func(a *outbox.Attempt) { a.Sends = outbox.Sends{Started: outbox.MaxSends + 1} },
		"classified > recorded": func(a *outbox.Attempt) {
			a.Sends = outbox.Sends{Started: 1, Recorded: 1, Unknown: 1, Refusals: 1, Last: unknown}
		},
		"last without record": func(a *outbox.Attempt) { a.Sends = outbox.Sends{Started: 1, Last: refused} },
		"ack on SIGNED":       func(a *outbox.Attempt) { a.Sends = outbox.Sends{Started: 1, Recorded: 1, Acked: true, Last: ack} },
		"ACKNOWLEDGED no ack": func(a *outbox.Attempt) {
			a.Phase, a.Sends = outbox.AttemptAcknowledged, outbox.Sends{Started: 1, Recorded: 1, Unknown: 1, Last: unknown}
		},
		"UNCERTAIN no unknown": func(a *outbox.Attempt) { a.Phase = outbox.AttemptUncertain },
		"rejection without code": func(a *outbox.Attempt) {
			a.Sends = outbox.Sends{Started: 1, Recorded: 1, Refusals: 1, Last: outbox.Send{Outcome: outbox.SendMempoolRejected}}
		},
		"long codespace": func(a *outbox.Attempt) {
			a.Sends = outbox.Sends{Started: 1, Recorded: 1, Refusals: 1, Last: rejected(outbox.Code{Codespace: strings.Repeat("x", 65), Number: 1})}
		},
		"codespace with space": func(a *outbox.Attempt) {
			a.Sends = outbox.Sends{Started: 1, Recorded: 1, Refusals: 1, Last: rejected(outbox.Code{Codespace: "s v", Number: 1})}
		},
		"code without codespace": func(a *outbox.Attempt) {
			a.Sends = outbox.Sends{Started: 1, Recorded: 1, Refusals: 1, Last: rejected(outbox.Code{Number: 1})}
		},
	}
	for name, mutate := range attempts {
		a := good
		mutate(&a)
		if _, err := outbox.Judge(a, view, lim); err == nil {
			t.Errorf("attempt %s accepted", name)
		}
	}
	hits := make([]outbox.Hit, outbox.MaxSearchHits+1)
	for i := range hits {
		hits[i] = outbox.Hit{Hash: hash(byte(i)), Owner: outbox.OwnerOurs}
	}
	views := map[string]outbox.ChainView{
		"height above bound":     {Height: outbox.MaxHeight + 1},
		"found at height 0":      {Tx: outbox.TxResult{Found: true, AccSeq: outbox.AccSeqThis}},
		"found, unknown acc_seq": {Tx: outbox.TxResult{Found: true, Height: 3}},
		"not found with fields":  {Tx: outbox.TxResult{Code: forbidden}},
		"too many hits":          {Searched: true, Search: hits},
		"hits without search":    {Search: []outbox.Hit{{Hash: hash(2), Owner: outbox.OwnerOurs}}},
		"hit without hash":       {Searched: true, Search: []outbox.Hit{{Owner: outbox.OwnerOurs}}},
		"hit owner unknown":      {Searched: true, Search: []outbox.Hit{{Hash: hash(2)}}},
		"account above bound":    {Account: &outbox.AccountRead{Sequence: outbox.MaxSequence + 1}},
	}
	for name, v := range views {
		if _, err := outbox.Judge(good, v, lim); err == nil {
			t.Errorf("view %s accepted", name)
		}
	}
	q := queued(t)
	intents := map[string]func(*outbox.Intent){
		"no business":           func(in *outbox.Intent) { in.Business = 0 },
		"business above bound":  func(in *outbox.Intent) { in.Business = outbox.MaxBusinessAttempts + 1 },
		"attempts above bound":  func(in *outbox.Intent) { in.Attempts = outbox.MaxAttemptsPerIntent + 1 },
		"tries above attempts":  func(in *outbox.Intent) { in.BusinessTries = 1 },
		"business w/o attempts": func(in *outbox.Intent) { in.Business = 3 },
		"sdk4 above attempts":   func(in *outbox.Intent) { in.Sdk4Deaths = 1 },
		"reason on REQUESTED":   func(in *outbox.Intent) { in.Reason = outbox.ReasonInvalid },
		"commit on REQUESTED":   func(in *outbox.Intent) { in.Canonical = hash(1) },
		"queued with attempts":  func(in *outbox.Intent) { in.Attempts = 1 },
		"in flight phase":       func(in *outbox.Intent) { in.Phase = outbox.PhaseSigned },
		"unknown state":         func(in *outbox.Intent) { in.State = 9 },
		"failed without reason": func(in *outbox.Intent) { in.State, in.Phase = outbox.StateFailed, 0 },
		"failed code missing": func(in *outbox.Intent) {
			in.State, in.Phase, in.Reason = outbox.StateFailed, 0, outbox.ReasonExecutionFailed
		},
		"committed without hash": func(in *outbox.Intent) { in.State, in.Phase = outbox.StateCommitted, 0 },
		"in flight unvalidated": func(in *outbox.Intent) {
			in.State, in.Phase, in.Validated, in.Attempts, in.BusinessTries = outbox.StateInFlight, outbox.PhaseSigned, false, 1, 1
		},
	}
	for name, mutate := range intents {
		in := q
		mutate(&in)
		if _, err := outbox.Apply(in, outbox.Expire{}, lim); err == nil {
			t.Errorf("intent %s accepted", name)
		}
		if _, err := outbox.Present(in, outbox.Overlay{}); err == nil {
			t.Errorf("intent %s presented", name)
		}
	}
	effectsBad := []outbox.Effect{
		{Verdict: 99},
		{Verdict: outbox.EffectCommitted},
		{Verdict: outbox.EffectNone, Own: true},
		{Verdict: outbox.EffectTaken, Canonical: hash(1)},
		{Verdict: outbox.EffectNone, DeadAttempt: true},
	}
	for _, e := range effectsBad {
		if _, err := outbox.Apply(q, outbox.CheckEffect{Effect: e}, lim); err == nil || errors.Is(err, outbox.ErrNeedEffect) {
			t.Errorf("effect %+v: %v", e, err)
		}
	}
	if _, err := outbox.Apply(q, outbox.Expire{}, outbox.Limits{}); err == nil {
		t.Error("zero limits accepted")
	}
}

// TestLimitsValidate checks each hard bound of section 11.
func TestLimitsValidate(t *testing.T) {
	if err := lim.Validate(); err != nil {
		t.Fatal(err)
	}
	low := outbox.Limits{IndexingMargin: outbox.MinIndexingMargin, AttemptsPerBusinessID: outbox.MinAttemptsPerBusinessID,
		BusinessAttempts: outbox.MinBusinessAttempts, AttemptsPerIntent: outbox.MinAttemptsPerIntent, Sdk4Retries: outbox.MinSdk4Retries}
	high := outbox.Limits{IndexingMargin: outbox.MaxIndexingMargin, AttemptsPerBusinessID: outbox.MaxAttemptsPerBusinessID,
		BusinessAttempts: outbox.MaxBusinessAttempts, AttemptsPerIntent: outbox.MaxAttemptsPerIntent, Sdk4Retries: outbox.MaxSdk4Retries}
	for _, l := range []outbox.Limits{low, high} {
		if err := l.Validate(); err != nil {
			t.Fatalf("%+v: %v", l, err)
		}
	}
	mutations := []func(*outbox.Limits, int){
		func(l *outbox.Limits, d int) { l.IndexingMargin = uint64(int(l.IndexingMargin) + d) },
		func(l *outbox.Limits, d int) { l.AttemptsPerBusinessID += d },
		func(l *outbox.Limits, d int) { l.BusinessAttempts += d },
		func(l *outbox.Limits, d int) { l.AttemptsPerIntent += d },
		func(l *outbox.Limits, d int) { l.Sdk4Retries += d },
	}
	for i, m := range mutations {
		below, above := low, high
		m(&below, -1)
		m(&above, 1)
		if below.Validate() == nil || above.Validate() == nil {
			t.Errorf("field %d: a value outside its bounds passed", i)
		}
	}
}

// TestNamesRoundTrip: every stored name parses back to its value; unknown
// and display-only names are refused.
func TestNamesRoundTrip(t *testing.T) {
	for s := outbox.StateRequested; s <= outbox.StateFailed; s++ {
		if got, err := outbox.ParseState(s.String()); err != nil || got != s {
			t.Fatalf("state %v: %v %v", s, got, err)
		}
	}
	for p := outbox.PhaseQueued; p <= outbox.PhaseAwaitingDeadline; p++ {
		if got, err := outbox.ParsePhase(p.String()); err != nil || got != p {
			t.Fatalf("phase %v: %v %v", p, got, err)
		}
	}
	for p := outbox.AttemptSigned; p <= outbox.AttemptDead; p++ {
		if got, err := outbox.ParseAttemptPhase(p.String()); err != nil || got != p {
			t.Fatalf("attempt phase %v: %v %v", p, got, err)
		}
	}
	for r := outbox.DeadRefused; r <= outbox.DeadSequenceUsed; r++ {
		if got, err := outbox.ParseDeadReason(r.String()); err != nil || got != r {
			t.Fatalf("dead reason %v: %v %v", r, got, err)
		}
	}
	for r := outbox.ReasonExecutionFailed; r <= outbox.ReasonCancelled; r++ {
		if got, err := outbox.ParseReason(r.String()); err != nil || got != r {
			t.Fatalf("reason %v: %v %v", r, got, err)
		}
	}
	for o := outbox.SendUnknown; o <= outbox.SendAck; o++ {
		if got, err := outbox.ParseSendOutcome(o.String()); err != nil || got != o {
			t.Fatalf("send outcome %v: %v %v", o, got, err)
		}
	}
	if _, err := outbox.ParseReason(outbox.ReasonExpiredAwaitingChain.String()); err == nil {
		t.Fatal("display reason parsed as a failure reason")
	}
	for _, bad := range []string{"", "requested", "IN FLIGHT", strings.Repeat("A", 41)} {
		if _, err := outbox.ParseState(bad); err == nil {
			t.Fatalf("state %q parsed", bad)
		}
	}
	if err := func() error { _, err := outbox.ParseState(strings.Repeat("A", 41)); return err }(); strings.Contains(err.Error(), "AAAA") {
		t.Fatal("a long name is quoted in the error")
	}
	names := map[string]string{
		outbox.T1.String(): "T1", outbox.T17.String(): "T17", outbox.TNone.String(): "NONE",
		outbox.StatusPendingStall.String(): "OPERATION_STATUS_PENDING_STALL", outbox.ObserverUnavailable.String(): "OBSERVER_UNAVAILABLE",
		outbox.HaltSignerConflict.String(): "SIGNER_CONFLICT", outbox.AlertSdk4Exhausted.String(): "SDK4_EXHAUSTED",
		outbox.State(9).String(): "State(9)", outbox.CodeSVNReplay.String(): "svn/3",
	}
	for got, want := range names {
		if got != want {
			t.Fatalf("name %q, want %q", got, want)
		}
	}
}
