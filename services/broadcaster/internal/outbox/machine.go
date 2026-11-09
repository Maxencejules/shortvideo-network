package outbox

import (
	"errors"
	"fmt"
)

// Intent is the state machine's view of one intent row. An IN_FLIGHT intent
// has exactly one live attempt and every other intent has none, as the
// outbox schema checks at commit; so an intent leaves IN_FLIGHT in the same
// step that ends its attempt, and a step that keeps an IN_FLIGHT intent
// keeps its attempt live.
type Intent struct {
	State State
	Phase Phase
	// Reason and Code describe a FAILED intent. Code is set exactly for
	// EXECUTION_FAILED and REJECTED.
	Reason Reason
	Code   Code
	// Validated: the request passed validation (HTTP path at create, SQL
	// path by the worker).
	Validated bool
	// Attempts counts the attempts persisted across all business ids
	// (attempt_count).
	Attempts int
	// Business is the current business attempt, from 1; BusinessTries
	// counts the attempts persisted under its business id.
	Business      int
	BusinessTries int
	// Sdk4Deaths counts this intent's attempts that died by (d2) with a
	// last refusal of sdk code 4. The store derives it from the attempts
	// rows.
	Sdk4Deaths int
	// Canonical and Own describe a COMMITTED intent: the first
	// commitment's hash, and whether it is one of this intent's attempts
	// (own_transaction).
	Canonical [32]byte
	Own       bool
}

func (in Intent) validate() error {
	switch {
	case in.Business < 1 || in.Business > MaxBusinessAttempts:
		return fmt.Errorf("outbox: business attempt %d outside [1, %d]", in.Business, MaxBusinessAttempts)
	case in.Attempts < 0 || in.Attempts > MaxAttemptsPerIntent:
		return fmt.Errorf("outbox: attempt count %d outside [0, %d]", in.Attempts, MaxAttemptsPerIntent)
	case in.BusinessTries < 0 || in.BusinessTries > MaxAttemptsPerBusinessID || in.BusinessTries > in.Attempts:
		return fmt.Errorf("outbox: business tries %d outside [0, min(%d, attempts)]", in.BusinessTries, MaxAttemptsPerBusinessID)
	case in.Sdk4Deaths < 0 || in.Sdk4Deaths > in.Attempts:
		return fmt.Errorf("outbox: sdk 4 deaths %d outside [0, attempts]", in.Sdk4Deaths)
	case in.Business-1 > in.Attempts:
		return errors.New("outbox: a business attempt opened without an earlier attempt")
	}
	if err := in.Code.validate(); err != nil {
		return err
	}
	if in.State != StateFailed && (in.Reason != 0 || in.Code != Code{}) {
		return errors.New("outbox: only a FAILED intent has a reason or code")
	}
	if in.State != StateCommitted && (in.Canonical != [32]byte{} || in.Own) {
		return errors.New("outbox: only a COMMITTED intent has a commitment")
	}
	switch in.State {
	case StateRequested:
		if in.Phase != PhaseQueued && in.Phase != PhaseRetryWait {
			return fmt.Errorf("outbox: REQUESTED intent in phase %v", in.Phase)
		}
		if (in.Phase == PhaseQueued) != (in.Attempts == 0) {
			return errors.New("outbox: a REQUESTED intent is QUEUED exactly when no attempt was persisted")
		}
	case StateInFlight:
		if in.Phase < PhaseSigned || in.Phase > PhaseAwaitingDeadline {
			return fmt.Errorf("outbox: IN_FLIGHT intent in phase %v", in.Phase)
		}
		if !in.Validated || in.BusinessTries < 1 {
			return errors.New("outbox: an IN_FLIGHT intent is validated and has an attempt under its business id")
		}
	case StateCommitted:
		if in.Phase != 0 || in.Canonical == ([32]byte{}) {
			return errors.New("outbox: a COMMITTED intent has no phase and has its commitment")
		}
	case StateFailed:
		if in.Phase != 0 || !in.Reason.failure() || in.Reason.withCode() == in.Code.OK() {
			return errors.New("outbox: a FAILED intent has no phase, a failure reason, and a code exactly for EXECUTION_FAILED or REJECTED")
		}
	default:
		return fmt.Errorf("outbox: intent state %d unknown", in.State)
	}
	return nil
}

// Event is an observation or a request the transition table reacts to.
type Event interface{ event() }

// Create is T1: the API or the SQL function committed a new intent.
// Validated is true on the HTTP path, which validates before it writes.
type Create struct{ Validated bool }

// FailValidation is T2: the worker's validation of a REQUESTED intent
// failed with Reason (UNAUTHORIZED, INVALID or PROVIDER_SIGNATURE_INVALID).
type FailValidation struct{ Reason Reason }

// Cancel is T3: the caller cancels an intent.
type Cancel struct{}

// Expire is T4 or T15: the database clock passed the intent's expires_at.
type Expire struct{}

// CheckEffect is T5, T6, T7 or T16 for a REQUESTED intent: the effect check
// that runs before a new attempt.
type CheckEffect struct{ Effect Effect }

// Guard holds the facts that 8.3 step 1 reads under the intent and signer
// row locks before an attempt is persisted.
type Guard struct {
	Expired      bool // the database clock passed expires_at
	Due          bool // the database clock passed next_attempt_at
	Stalled      bool // a stall episode is open
	SignerHalted bool // the signer row has a halted_reason
	SignerIdle   bool // the signer has no live attempt of any intent
	LockHeld     bool // the signer's advisory lock is held on this connection
}

// Persist is T8: a signed attempt is to be stored. Attempt is the new
// attempt (phase SIGNED, no sends). Effect is the effect check that ran
// immediately before (plan 8.6: always before signing a new attempt).
type Persist struct {
	Attempt Attempt
	Effect  Effect
	Guard   Guard
}

// StartSend counts one send of the live attempt's stored bytes before it
// happens (plan 8.3 step 2). Expired is the database clock's view of
// expires_at.
type StartSend struct {
	Attempt Attempt
	Expired bool
}

// RecordSend is T9 or T10: the outcome of one started send of the live
// attempt.
type RecordSend struct {
	Attempt Attempt
	Send    Send
}

// Settle is T11-T14 or T16: Judge's verdict on the intent's live attempt,
// with the effect check when the verdict needs one and the database
// clock's view of expires_at.
type Settle struct {
	Verdict Verdict
	Effect  Effect
	Expired bool
}

// SignerConflict is T16: the sequence allocation of plan 8.1 found a
// foreign use of the intent's signer.
type SignerConflict struct{}

func (Create) event()         {}
func (FailValidation) event() {}
func (Cancel) event()         {}
func (Expire) event()         {}
func (CheckEffect) event()    {}
func (Persist) event()        {}
func (StartSend) event()      {}
func (RecordSend) event()     {}
func (Settle) event()         {}
func (SignerConflict) event() {}

// Step is one application of the transition table.
type Step struct {
	// ID is the table row applied; TNone when no row applies (a send
	// counter update, a hold, an effect check that found nothing).
	ID     TransitionID
	Before Intent
	After  Intent
	// Attempt is the attempt the step wrote, when HasAttempt is set: the
	// new attempt of T8, the counted or recorded send, the resolution or
	// death a Settle stores, or a live attempt whose phase rose.
	Attempt    Attempt
	HasAttempt bool
	Halt       HaltReason
	Alert      Alert
	// NewBusinessAttempt: a committed REPLAY with no effect opened business
	// attempt After.Business; the caller draws its new business id.
	NewBusinessAttempt bool
	// WaitBlocks is the number of blocks the next attempt waits after
	// this step (DR-11-18: sdk 4 retries are at least two blocks apart).
	WaitBlocks  uint64
	sendStarted bool
}

// SendStarted reports whether a StartSend step counted a send. The caller
// sends only then.
func (s Step) SendStarted() bool { return s.sendStarted }

// Changed reports whether the step changes the intent.
func (s Step) Changed() bool { return s.Before != s.After }

// Sdk4RetryGapBlocks is the least number of blocks between an attempt that
// died with a CheckTx refusal of sdk code 4 and the next one (DR-11-18).
const Sdk4RetryGapBlocks = 2

// ErrNotApplicable is returned for an event that does not apply to the
// intent's state, such as a send outcome for a REQUESTED intent.
var ErrNotApplicable = errors.New("outbox: event does not apply to the intent's state")

// ErrGuard is returned when a guard of the requested transition does not
// hold. Nothing changes.
var ErrGuard = errors.New("outbox: transition guard does not hold")

// ErrNeedEffect is returned when a Settle needs the effect check of plan
// 8.6 step 3 and its Effect is unchecked.
var ErrNeedEffect = errors.New("outbox: the effect check must run first")

func guardFailed(id TransitionID, what string) error {
	return fmt.Errorf("%w: %v: %s", ErrGuard, id, what)
}

// Apply applies one event to an intent (plan 9.3). It returns the step and
// an error when the event is invalid, does not apply, or fails a guard; on
// error nothing changes. COMMITTED and FAILED absorb every event: the step
// is T17 with the intent unchanged and no attempt written.
func Apply(in Intent, ev Event, limits Limits) (Step, error) {
	if err := limits.Validate(); err != nil {
		return Step{}, err
	}
	if c, ok := ev.(Create); ok {
		if in != (Intent{}) {
			return Step{}, fmt.Errorf("%w: create of an existing intent", ErrNotApplicable)
		}
		after := Intent{State: StateRequested, Phase: PhaseQueued, Validated: c.Validated, Business: 1}
		return Step{ID: T1, Before: in, After: after}, nil
	}
	if err := in.validate(); err != nil {
		return Step{}, err
	}
	if in.State.Terminal() {
		return Step{ID: T17, Before: in, After: in}, nil
	}
	switch e := ev.(type) {
	case FailValidation:
		return applyFailValidation(in, e)
	case Cancel:
		if in.State != StateRequested {
			return Step{}, guardFailed(T3, "the intent is not REQUESTED")
		}
		if in.Attempts > 0 {
			return Step{}, guardFailed(T3, "an attempt was persisted")
		}
		return Step{ID: T3, Before: in, After: failed(in, ReasonCancelled, Code{})}, nil
	case Expire:
		return applyExpire(in), nil
	case CheckEffect:
		return applyCheckEffect(in, e.Effect)
	case Persist:
		return applyPersist(in, e, limits)
	case StartSend:
		return applyStartSend(in, e)
	case RecordSend:
		return applyRecordSend(in, e)
	case Settle:
		return applySettle(in, e, limits)
	case SignerConflict:
		return Step{ID: T16, Before: in, After: in, Halt: HaltSignerConflict, Alert: AlertSignerConflict}, nil
	case nil:
		return Step{}, errors.New("outbox: nil event")
	}
	return Step{}, fmt.Errorf("outbox: event %T unknown", ev)
}

func failed(in Intent, r Reason, c Code) Intent {
	in.State, in.Phase, in.Reason, in.Code = StateFailed, 0, r, c
	return in
}

func committed(in Intent, canonical [32]byte, own bool) Intent {
	in.State, in.Phase, in.Canonical, in.Own = StateCommitted, 0, canonical, own
	return in
}

func applyFailValidation(in Intent, e FailValidation) (Step, error) {
	switch e.Reason {
	case ReasonUnauthorized, ReasonInvalid, ReasonProviderSignatureInvalid:
	default:
		return Step{}, fmt.Errorf("outbox: %v is not a validation failure", e.Reason)
	}
	if in.State != StateRequested {
		return Step{}, guardFailed(T2, "the intent has a live attempt")
	}
	return Step{ID: T2, Before: in, After: failed(in, e.Reason, Code{})}, nil
}

func applyExpire(in Intent) Step {
	switch {
	case in.State == StateRequested && in.Attempts == 0:
		return Step{ID: T4, Before: in, After: failed(in, ReasonExpiredNotSent, Code{})}
	case in.State == StateRequested:
		return Step{ID: T4, Before: in, After: failed(in, ReasonExpiredDead, Code{})}
	case in.Phase == PhaseAwaitingDeadline:
		return Step{ID: TNone, Before: in, After: in}
	}
	after := in
	after.Phase = PhaseAwaitingDeadline
	return Step{ID: T15, Before: in, After: after}
}

// applyCheckEffect is the effect check of a REQUESTED intent before a new
// attempt (8.6 steps 3 and 4).
func applyCheckEffect(in Intent, eff Effect) (Step, error) {
	if in.State != StateRequested {
		return Step{}, fmt.Errorf("%w: the effect of an IN_FLIGHT intent is settled with its attempt", ErrNotApplicable)
	}
	if err := eff.validate(); err != nil {
		return Step{}, err
	}
	step := Step{Before: in, After: in}
	switch eff.Verdict {
	case 0:
		return Step{}, ErrNeedEffect
	case EffectNone:
		step.ID = TNone
	case EffectCommitted:
		if eff.DeadAttempt {
			step.Alert, step.Halt = AlertDeadAttemptCommitted, HaltIntegrity
			return step, nil
		}
		step.ID, step.After = T5, committed(in, eff.Canonical, eff.Own)
	case EffectTaken:
		step.ID, step.After = T6, failed(in, ReasonObjectTaken, Code{})
	case EffectStateChanged:
		step.ID, step.After = T7, failed(in, ReasonObjectStateChanged, Code{})
	case EffectSignerConflict:
		step.ID, step.Alert, step.Halt = T16, AlertSignerConflict, HaltSignerConflict
	}
	return step, nil
}

// persistGuard checks the conditions of 8.3 step 1 and the per-signer
// window (DR-11-26) for a REQUESTED intent.
func persistGuard(in Intent, eff Effect, g Guard, limits Limits) error {
	switch {
	case in.State != StateRequested:
		return guardFailed(T8, "the intent has a live attempt")
	case !in.Validated:
		return guardFailed(T8, "the request is not validated")
	case g.Expired:
		return guardFailed(T8, "the intent expired")
	case !g.Due:
		return guardFailed(T8, "the next attempt is not due")
	case g.Stalled:
		return guardFailed(T8, "a stall episode is open")
	case g.SignerHalted:
		return guardFailed(T8, "the signer is halted")
	case !g.SignerIdle:
		return guardFailed(T8, "the signer has a live attempt")
	case !g.LockHeld:
		return guardFailed(T8, "the signer lock is not held")
	case eff.Verdict != EffectNone:
		return guardFailed(T8, "the effect check did not find the effect absent")
	case in.Attempts >= limits.AttemptsPerIntent:
		return guardFailed(T8, "the attempts of the intent are exhausted")
	case in.BusinessTries >= limits.AttemptsPerBusinessID:
		return guardFailed(T8, "the attempts of the business id are exhausted")
	}
	return nil
}

func applyPersist(in Intent, e Persist, limits Limits) (Step, error) {
	if err := e.Effect.validate(); err != nil {
		return Step{}, err
	}
	if err := e.Attempt.validate(); err != nil {
		return Step{}, err
	}
	if e.Attempt.Phase != AttemptSigned || e.Attempt.Sends != (Sends{}) {
		return Step{}, errors.New("outbox: a new attempt is SIGNED with no sends")
	}
	if err := persistGuard(in, e.Effect, e.Guard, limits); err != nil {
		return Step{}, err
	}
	after := in
	after.State, after.Phase = StateInFlight, PhaseSigned
	after.Attempts++
	after.BusinessTries++
	return Step{ID: T8, Before: in, After: after, Attempt: e.Attempt, HasAttempt: true}, nil
}

func liveAttempt(in Intent, a Attempt) error {
	if in.State != StateInFlight {
		return fmt.Errorf("%w: the intent has no live attempt", ErrNotApplicable)
	}
	if err := a.validate(); err != nil {
		return err
	}
	if !a.Phase.Live() {
		return fmt.Errorf("%w: the attempt is %v", ErrNotApplicable, a.Phase)
	}
	return nil
}

func applyStartSend(in Intent, e StartSend) (Step, error) {
	if err := liveAttempt(in, e.Attempt); err != nil {
		return Step{}, err
	}
	if e.Expired || in.Phase == PhaseAwaitingDeadline {
		return Step{}, guardFailed(TNone, "no send after expiry")
	}
	sends, err := e.Attempt.Sends.start()
	if err != nil {
		return Step{}, err
	}
	a := e.Attempt
	a.Sends = sends
	return Step{ID: TNone, Before: in, After: in, Attempt: a, HasAttempt: true, sendStarted: true}, nil
}

func applyRecordSend(in Intent, e RecordSend) (Step, error) {
	if err := liveAttempt(in, e.Attempt); err != nil {
		return Step{}, err
	}
	sends, err := e.Attempt.Sends.record(e.Send)
	if err != nil {
		return Step{}, err
	}
	a := e.Attempt
	a.Sends = sends
	a.Phase = RaisePhase(a.Phase, e.Send.Outcome)
	step := Step{ID: TNone, Before: in, After: in, Attempt: a, HasAttempt: true}
	switch {
	case e.Send.Outcome == SendAck && (in.Phase == PhaseSigned || in.Phase == PhaseUncertain):
		step.ID, step.After.Phase = T9, PhaseAcknowledged
	case (e.Send.Outcome == SendUnknown || e.Send.Outcome == SendCache) && in.Phase == PhaseSigned:
		step.ID, step.After.Phase = T10, PhaseUncertain
	}
	return step, nil
}

func applySettle(in Intent, e Settle, limits Limits) (Step, error) {
	v := e.Verdict
	if v.kind == 0 {
		return Step{}, errors.New("outbox: a Settle needs a verdict from Judge")
	}
	if in.State != StateInFlight {
		return Step{}, fmt.Errorf("%w: the intent has no live attempt", ErrNotApplicable)
	}
	if err := e.Effect.validate(); err != nil {
		return Step{}, err
	}
	step := Step{ID: TNone, Before: in, After: in, Alert: v.alert, Halt: v.halt}
	hold := func(alert Alert, halt HaltReason) (Step, error) {
		step.Alert, step.Halt = alert, halt
		if halt == HaltSignerConflict {
			step.ID = T16
		}
		return step, nil
	}
	switch v.kind {
	case VerdictHold:
		return hold(v.alert, v.halt)
	case VerdictLive:
		if v.attempt.Phase == AttemptUncertain && in.Phase == PhaseSigned {
			step.ID, step.After.Phase = T10, PhaseUncertain
		}
		step.Attempt, step.HasAttempt = v.attempt, true
		return step, nil
	}
	if v.kind == VerdictResolved && v.attempt.Phase == AttemptCommittedOK {
		step.ID, step.After = T11, committed(in, v.attempt.Hash, true)
		step.Attempt, step.HasAttempt = v.attempt, true
		return step, nil
	}
	eff := e.Effect
	switch eff.Verdict {
	case 0:
		return Step{}, ErrNeedEffect
	case EffectSignerConflict:
		return hold(AlertSignerConflict, HaltSignerConflict)
	case EffectCommitted:
		if eff.DeadAttempt {
			return hold(AlertDeadAttemptCommitted, HaltIntegrity)
		}
		if eff.Canonical == v.attempt.Hash {
			// An idempotent replay is never its own first commitment, and a
			// failed or dead attempt never commits the effect.
			return hold(AlertEffectInconsistent, 0)
		}
	}
	if v.kind == VerdictResolved && v.attempt.Phase == AttemptCommittedIdempotent {
		switch eff.Verdict {
		case EffectCommitted:
			step.ID, step.After = T12, committed(in, eff.Canonical, eff.Own)
			step.Attempt, step.HasAttempt = v.attempt, true
			return step, nil
		case EffectNone:
			return hold(AlertFirstCommitNotFound, 0)
		}
		return hold(AlertEffectInconsistent, 0)
	}
	// T13 (a committed failure) or T14 (a dead attempt): the effect check
	// decides first (T5, T6, T7), then the retry rules.
	step.Attempt, step.HasAttempt = v.attempt, true
	switch eff.Verdict {
	case EffectCommitted:
		step.ID, step.After = T5, committed(in, eff.Canonical, eff.Own)
		return step, nil
	case EffectTaken:
		step.ID, step.After = T6, failed(in, ReasonObjectTaken, Code{})
		return step, nil
	case EffectStateChanged:
		step.ID, step.After = T7, failed(in, ReasonObjectStateChanged, Code{})
		return step, nil
	}
	step.ID = T14
	if v.kind == VerdictResolved {
		step.ID = T13
	}
	return retry(step, v, e.Expired, limits), nil
}
