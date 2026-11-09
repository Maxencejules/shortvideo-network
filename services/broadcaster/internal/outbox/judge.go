package outbox

import (
	"errors"
	"fmt"
)

// Attempt is the state machine's view of one stored attempt.
type Attempt struct {
	// Hash is SHA-256 of the attempt's transaction bytes.
	Hash          [32]byte
	Sequence      uint64
	TimeoutHeight uint64
	Phase         AttemptPhase
	// Dead is set exactly when Phase is AttemptDead.
	Dead  DeadReason
	Sends Sends
}

func (a Attempt) validate() error {
	if a.Hash == ([32]byte{}) {
		return errors.New("outbox: attempt without a hash")
	}
	if a.Sequence > MaxSequence {
		return errors.New("outbox: attempt sequence above its bound")
	}
	if a.TimeoutHeight == 0 || a.TimeoutHeight > MaxHeight {
		return errors.New("outbox: attempt timeout height outside [1, MaxHeight]")
	}
	if a.Phase < AttemptSigned || a.Phase > AttemptDead {
		return fmt.Errorf("outbox: attempt phase %d unknown", a.Phase)
	}
	if (a.Phase == AttemptDead) != (a.Dead != 0) {
		return errors.New("outbox: a dead reason is set exactly for a DEAD attempt")
	}
	if a.Dead > DeadSequenceUsed {
		return fmt.Errorf("outbox: dead reason %d unknown", a.Dead)
	}
	if a.Dead == DeadRefused && !a.Sends.AllRefused() {
		return errors.New("outbox: dead by (d2) without every started send recorded as a refusal")
	}
	if err := a.Sends.validate(); err != nil {
		return err
	}
	// A live phase is the most knowledge its send outcomes give (plan 9.2);
	// an unrecorded send counts as UNKNOWN.
	uncertain := a.Sends.Unknown+a.Sends.Cache+a.Sends.Unrecorded() > 0
	switch {
	case a.Phase == AttemptSigned && (a.Sends.Acked || a.Sends.Unknown+a.Sends.Cache > 0),
		a.Phase == AttemptUncertain && (a.Sends.Acked || !uncertain),
		a.Phase == AttemptAcknowledged && !a.Sends.Acked:
		return fmt.Errorf("outbox: attempt phase %v disagrees with its send outcomes", a.Phase)
	}
	return nil
}

// AccSeq is what a committed result's tx.acc_seq attribute says about the
// attempt's signer and sequence (plan 1.1, 8.4).
type AccSeq uint8

// AccSeq observations.
const (
	// AccSeqAbsent: no tx.acc_seq attribute; the ante failed and the
	// sequence was not consumed.
	AccSeqAbsent AccSeq = iota + 1
	// AccSeqThis: tx.acc_seq names this attempt's signer and sequence; the
	// ante passed and the sequence was consumed.
	AccSeqThis
	// AccSeqOther: tx.acc_seq names another account or sequence.
	AccSeqOther
)

// TxResult is the answer of /tx for one attempt's hash.
type TxResult struct {
	Found bool
	// HashVerified: SHA-256 of the returned bytes equals the attempt's
	// hash.
	HashVerified bool
	Height       uint64
	Code         Code
	AccSeq       AccSeq
	// TypedEvent: the result carries the typed svn.events.v1 event of the
	// attempt's kind for the intent's object.
	TypedEvent bool
	// Idempotent: the result carries "<module>_result idempotent=true".
	Idempotent bool
}

// HashOwner says which stored attempt, if any, has a transaction hash.
type HashOwner uint8

// Hash owners.
const (
	// OwnerForeign: no stored attempt has the hash and no operator
	// acknowledged it.
	OwnerForeign HashOwner = iota + 1
	// OwnerForeignCleared: no stored attempt has the hash; an operator
	// acknowledged it with signer-clear (plan 8.1).
	OwnerForeignCleared
	// OwnerOurs: a stored attempt not marked DEAD.
	OwnerOurs
	// OwnerOursDead: a stored attempt marked DEAD.
	OwnerOursDead
)

// Hit is one transaction the acc_seq search returned.
type Hit struct {
	Hash  [32]byte
	Owner HashOwner
}

// MaxSearchHits bounds the hits a search observation may carry. One
// sequence can be consumed by at most one transaction, so two hits are
// already an inconsistency; the bound only keeps the input finite.
const MaxSearchHits = 100

// AccountRead is the signer's committed account sequence and the height
// the node answered at.
type AccountRead struct {
	Sequence uint64
	Height   uint64
}

// ChainView is what the node said about one live attempt in this pass.
// Account and Search are nil when not read.
type ChainView struct {
	Tx TxResult
	// Height is the node's latest committed height, read before Tx: a
	// transaction committed at or below timeout_height is indexed by
	// timeout_height + L, so a /tx read after a height of at least
	// timeout_height + L sees it.
	Height uint64
	// Account is the signer's account sequence; it counts for (d3) only
	// when read at a height of at least timeout_height + L.
	Account *AccountRead
	// Search is the result of tx_search "tx.acc_seq='<signer>/<sequence>'".
	Search []Hit
	// Searched reports whether the search ran (Search may be empty).
	Searched bool
}

func (v ChainView) validate() error {
	if v.Height > MaxHeight {
		return errors.New("outbox: chain height above its bound")
	}
	if v.Tx.Found {
		if v.Tx.Height == 0 || v.Tx.Height > MaxHeight {
			return errors.New("outbox: committed height outside [1, MaxHeight]")
		}
		if v.Tx.AccSeq < AccSeqAbsent || v.Tx.AccSeq > AccSeqOther {
			return fmt.Errorf("outbox: acc_seq observation %d unknown", v.Tx.AccSeq)
		}
		if err := v.Tx.Code.validate(); err != nil {
			return err
		}
	} else if v.Tx != (TxResult{}) {
		return errors.New("outbox: a /tx result that was not found carries fields")
	}
	if v.Account != nil && (v.Account.Sequence > MaxSequence || v.Account.Height > MaxHeight) {
		return errors.New("outbox: account read above its bounds")
	}
	if len(v.Search) > MaxSearchHits {
		return fmt.Errorf("outbox: more than %d search hits", MaxSearchHits)
	}
	if !v.Searched && len(v.Search) > 0 {
		return errors.New("outbox: search hits without a search")
	}
	for _, h := range v.Search {
		if h.Hash == ([32]byte{}) {
			return errors.New("outbox: search hit without a hash")
		}
		if h.Owner < OwnerForeign || h.Owner > OwnerOursDead {
			return fmt.Errorf("outbox: hash owner %d unknown", h.Owner)
		}
	}
	return nil
}

// VerdictKind is the class of Judge's verdict on a live attempt.
type VerdictKind uint8

// Verdict kinds.
const (
	// VerdictLive: the attempt may still commit.
	VerdictLive VerdictKind = iota + 1
	// VerdictResolved: /tx found a committed result; Phase is its terminal
	// phase (COMMITTED_OK, COMMITTED_IDEMPOTENT, EXECUTION_FAILED or
	// INCLUDED_ANTE_FAILED). EXECUTION_FAILED and INCLUDED_ANTE_FAILED are
	// (d1); INCLUDED_ANTE_FAILED only once the height reaches
	// timeout_height + L.
	VerdictResolved
	// VerdictDead: the attempt can no longer commit by (d2) or (d3).
	VerdictDead
	// VerdictHold: an integrity alert or a signer conflict. Neither the
	// attempt nor the intent changes; Alert and Halt say what to report.
	VerdictHold
)

// Need says which observation a decision waits for. The caller fetches it
// and runs the decision again.
type Need uint8

// Needed observations.
const (
	// NeedAccount: the signer's account sequence read at a height of at
	// least timeout_height + L.
	NeedAccount Need = 1 << iota
	// NeedSearch: tx_search by the attempt's tx.acc_seq.
	NeedSearch
	// NeedEffect: the effect check of plan 8.6 step 3.
	NeedEffect
	// NeedValidation: the intent's request has not been validated.
	NeedValidation
)

// Verdict is Judge's decision about one live attempt. Only Judge creates a
// Verdict, so the T11 and T12 transitions, which take one, cannot run on a
// result that Judge did not read from /tx.
type Verdict struct {
	kind    VerdictKind
	attempt Attempt // the attempt after the verdict
	code    Code
	alert   Alert
	halt    HaltReason
	need    Need
	other   [32]byte
}

// Kind is the verdict's class.
func (v Verdict) Kind() VerdictKind { return v.kind }

// Attempt is the judged attempt after the verdict: its phase, and its dead
// reason for VerdictDead. For VerdictLive the phase may have risen to
// UNCERTAIN because of unrecorded sends.
func (v Verdict) Attempt() Attempt { return v.attempt }

// Code is the committed code of a VerdictResolved failure, or the CheckTx
// code of the last refusal of a (d2) death (zero for a node refusal).
func (v Verdict) Code() Code { return v.code }

// Alert is the alert the verdict raises, if any.
func (v Verdict) Alert() Alert { return v.alert }

// Halt is the signer halt the verdict requires, if any.
func (v Verdict) Halt() HaltReason { return v.halt }

// Need lists the observations a VerdictLive waits for.
func (v Verdict) Need() Need { return v.need }

// Other is, for a (d3) death by the acc_seq search, the hash of another
// stored attempt that used the sequence; the caller resolves it by its own
// /tx. The second result is false otherwise.
func (v Verdict) Other() ([32]byte, bool) { return v.other, v.other != ([32]byte{}) }

// Judge applies the committed-result table of plan 8.4 and the dead rule
// of plan 8.5 to one live attempt and what the node said about it. It reads
// no clock and no expiry: an attempt dies only from chain observations and
// recorded send outcomes.
//
// Found by /tx:
//   - bytes that do not hash to the attempt's hash, or an acc_seq of
//     another account or sequence, hold everything and halt the signer;
//   - code 0 with this attempt's acc_seq and the typed event is
//     COMMITTED_OK; with the idempotent marker instead it is
//     COMMITTED_IDEMPOTENT; anything else with code 0 is an integrity hold;
//   - a failure code with this attempt's acc_seq is EXECUTION_FAILED (the
//     sequence was consumed);
//   - a failure code without acc_seq (the ante failed, the sequence was not
//     consumed) keeps the attempt live until the height is at least
//     timeout_height + L, and is INCLUDED_ANTE_FAILED only then. CometBFT
//     removes a failed transaction from the mempool cache so that it can be
//     resubmitted, and its indexer lets a later success of the same bytes
//     replace the failure, so these bytes can still commit until their
//     timeout_height.
//
// Not found:
//   - (d2) every started send recorded as a counted refusal: DEAD;
//   - (d3) only once the height is at least timeout_height + L: a search
//     hit by another stored attempt or by an acknowledged foreign
//     transaction is DEAD (sequence used); a foreign hit nobody acknowledged
//     is a signer conflict (hold, halt); a hit by a DEAD attempt is a fatal
//     integrity hold; otherwise an account sequence read at that height or
//     later that is at most the attempt's sequence is DEAD (deadline
//     passed); a larger account sequence with an empty search keeps the
//     attempt live with an alert;
//   - otherwise live. Unrecorded sends count as UNKNOWN, so a SIGNED
//     attempt with one is reported as UNCERTAIN.
func Judge(a Attempt, view ChainView, limits Limits) (Verdict, error) {
	if err := limits.Validate(); err != nil {
		return Verdict{}, err
	}
	if err := a.validate(); err != nil {
		return Verdict{}, err
	}
	if !a.Phase.Live() {
		return Verdict{}, fmt.Errorf("outbox: Judge needs a live attempt, not %v", a.Phase)
	}
	if err := view.validate(); err != nil {
		return Verdict{}, err
	}
	live := Verdict{kind: VerdictLive, attempt: a}
	if a.Phase == AttemptSigned && a.Sends.Unrecorded() > 0 {
		live.attempt.Phase = AttemptUncertain
	}
	deadline := a.TimeoutHeight + limits.IndexingMargin
	if view.Tx.Found {
		return judgeCommitted(a, view.Tx, view.Height >= deadline, live), nil
	}
	if a.Sends.AllRefused() {
		a.Phase, a.Dead = AttemptDead, DeadRefused
		return Verdict{kind: VerdictDead, attempt: a, code: a.Sends.Last.Code}, nil
	}
	if view.Height < deadline {
		return live, nil
	}
	if view.Searched {
		if len(view.Search) > 1 {
			live.alert = AlertSearchInconsistent
			return live, nil
		}
		if len(view.Search) == 1 {
			return judgeHit(a, view.Search[0], live), nil
		}
	}
	if view.Account == nil || view.Account.Height < deadline {
		live.need = NeedAccount
		return live, nil
	}
	if view.Account.Sequence <= a.Sequence {
		a.Phase, a.Dead = AttemptDead, DeadDeadlinePassed
		return Verdict{kind: VerdictDead, attempt: a}, nil
	}
	if !view.Searched {
		live.need = NeedSearch
		return live, nil
	}
	live.alert = AlertSequenceAdvancedNoHit
	return live, nil
}

// judgeCommitted classifies a committed result found by /tx (plan 8.4,
// second table). pastDeadline reports whether the node's height is at least
// timeout_height + L; live is the verdict that keeps the attempt live.
func judgeCommitted(a Attempt, tx TxResult, pastDeadline bool, live Verdict) Verdict {
	hold := func(alert Alert) Verdict {
		return Verdict{kind: VerdictHold, attempt: a, alert: alert, halt: HaltIntegrity}
	}
	if !tx.HashVerified {
		return hold(AlertHashMismatch)
	}
	if tx.AccSeq == AccSeqOther {
		return hold(AlertAccSeqForeign)
	}
	resolved := func(p AttemptPhase) Verdict {
		a.Phase = p
		return Verdict{kind: VerdictResolved, attempt: a, code: tx.Code}
	}
	if tx.Code.OK() {
		switch {
		case tx.AccSeq != AccSeqThis || tx.TypedEvent == tx.Idempotent:
			return hold(AlertResultInconsistent)
		case tx.TypedEvent:
			return resolved(AttemptCommittedOK)
		default:
			return resolved(AttemptCommittedIdempotent)
		}
	}
	if tx.TypedEvent || tx.Idempotent {
		return hold(AlertResultInconsistent)
	}
	if tx.AccSeq == AccSeqThis {
		return resolved(AttemptExecutionFailed)
	}
	if !pastDeadline {
		return live
	}
	return resolved(AttemptIncludedAnteFailed)
}

// judgeHit applies the search clause of (d3) to the one transaction that
// used the attempt's sequence.
func judgeHit(a Attempt, hit Hit, live Verdict) Verdict {
	if hit.Hash == a.Hash {
		// The search names this attempt while /tx does not find it.
		live.alert = AlertSearchInconsistent
		return live
	}
	switch hit.Owner {
	case OwnerForeign:
		return Verdict{kind: VerdictHold, attempt: a, alert: AlertSignerConflict, halt: HaltSignerConflict}
	case OwnerOursDead:
		return Verdict{kind: VerdictHold, attempt: a, alert: AlertDeadAttemptCommitted, halt: HaltIntegrity}
	}
	a.Phase, a.Dead = AttemptDead, DeadSequenceUsed
	v := Verdict{kind: VerdictDead, attempt: a}
	if hit.Owner == OwnerOurs {
		v.other = hit.Hash
	}
	return v
}
