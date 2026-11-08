package outbox

import "errors"

// Pass is everything one reconciliation pass of plan 8.6 observed about an
// intent.
type Pass struct {
	Intent Intent
	// Live is the intent's live attempt; set exactly when the intent is
	// IN_FLIGHT.
	Live *Attempt
	// Chain is what the node said about Live.
	Chain ChainView
	// Effect is the effect check's result; zero unless the caller ran it.
	Effect Effect
	// Guard holds the database and observer facts of this pass. For an
	// IN_FLIGHT intent only Guard.Expired is read.
	Guard Guard
}

// Decision is the outcome of one reconciliation pass.
type Decision struct {
	// Steps are the table applications to store, in order, in one
	// database transaction. A pass that changes nothing has none.
	Steps []Step
	// Need lists the observations to fetch before the next pass.
	Need Need
	// Rebroadcast: send the live attempt's stored bytes again (never
	// re-signed bytes), through Apply(StartSend) first. Never set after
	// expiry or while the signer must halt. The caller paces it, at most
	// once per backoff step.
	Rebroadcast bool
	// Start: the effect check found no effect and every guard of T8 holds;
	// the caller may sign a new attempt and store it with Apply(Persist),
	// which checks the guards again under the row locks.
	Start bool
}

// Reconcile runs one reconciliation pass (plan 8.6) over an intent. It is
// the only path from chain observations to a new attempt:
//
//   - an IN_FLIGHT intent's live attempt is judged (Judge). A live attempt is
//     rebroadcast before expiry; at expiry the intent moves to
//     AWAITING_DEADLINE (T15) and nothing more is sent. A committed success
//     settles at once (T11). Every other committed result and every dead
//     attempt waits for the effect check, which decides first (T5, T6, T7)
//     before the retry rules (T13, T14). A hold (integrity alert or signer
//     conflict) keeps the attempt live and sends nothing in this pass; only
//     T15 may still apply.
//   - a REQUESTED intent expires (T4), or waits for validation, or waits
//     for the effect check, which settles it (T5, T6, T7) or halts the
//     signer (T16); only when the check found no effect and the T8 guards
//     hold may a new attempt start.
//   - a COMMITTED or FAILED intent absorbs the pass (T17).
func Reconcile(p Pass, limits Limits) (Decision, error) {
	if err := limits.Validate(); err != nil {
		return Decision{}, err
	}
	if err := p.Intent.validate(); err != nil {
		return Decision{}, err
	}
	if (p.Live != nil) != (p.Intent.State == StateInFlight) {
		return Decision{}, errors.New("outbox: a live attempt is passed exactly for an IN_FLIGHT intent")
	}
	switch p.Intent.State {
	case StateInFlight:
		return reconcileInFlight(p, limits)
	case StateRequested:
		return reconcileRequested(p, limits)
	}
	step, err := Apply(p.Intent, Expire{}, limits)
	if err != nil {
		return Decision{}, err
	}
	return Decision{Steps: []Step{step}}, nil
}

func reconcileInFlight(p Pass, limits Limits) (Decision, error) {
	v, err := Judge(*p.Live, p.Chain, limits)
	if err != nil {
		return Decision{}, err
	}
	var d Decision
	needsEffect := (v.kind == VerdictResolved && v.attempt.Phase != AttemptCommittedOK) || v.kind == VerdictDead
	if needsEffect && p.Effect.Verdict == 0 {
		d.Need = NeedEffect
		return d, nil
	}
	step, err := Apply(p.Intent, Settle{Verdict: v, Effect: p.Effect, Expired: p.Guard.Expired}, limits)
	if err != nil {
		return Decision{}, err
	}
	if v.kind != VerdictLive && v.kind != VerdictHold {
		d.Steps = append(d.Steps, step)
		return d, nil
	}
	// The attempt stays live: a live verdict or a hold.
	if step.Changed() || step.Alert != 0 || step.Halt != 0 || (step.HasAttempt && step.Attempt != *p.Live) {
		d.Steps = append(d.Steps, step)
	}
	cur := step.After
	if p.Guard.Expired && cur.Phase != PhaseAwaitingDeadline {
		step, err := Apply(cur, Expire{}, limits)
		if err != nil {
			return Decision{}, err
		}
		d.Steps = append(d.Steps, step)
		cur = step.After
	}
	d.Need = v.need
	d.Rebroadcast = v.kind == VerdictLive && !p.Guard.Expired && cur.Phase != PhaseAwaitingDeadline
	return d, nil
}

func reconcileRequested(p Pass, limits Limits) (Decision, error) {
	if p.Guard.Expired {
		step, err := Apply(p.Intent, Expire{}, limits)
		if err != nil {
			return Decision{}, err
		}
		return Decision{Steps: []Step{step}}, nil
	}
	if !p.Intent.Validated {
		return Decision{Need: NeedValidation}, nil
	}
	if p.Effect.Verdict == 0 {
		return Decision{Need: NeedEffect}, nil
	}
	step, err := Apply(p.Intent, CheckEffect{Effect: p.Effect}, limits)
	if err != nil {
		return Decision{}, err
	}
	if step.ID != TNone || step.Alert != 0 || step.Halt != 0 {
		return Decision{Steps: []Step{step}}, nil
	}
	return Decision{Start: persistGuard(p.Intent, p.Effect, p.Guard, limits) == nil}, nil
}
