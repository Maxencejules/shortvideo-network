package outbox

import (
	"errors"
	"fmt"
)

// EffectVerdict is the result of the effect check of plan 8.6 step 3: the
// object query and the typed event search for the intent's object, each
// committing transaction decoded with the hash check and compared with the
// intent's message under the signer rule (internal/chain does the
// comparison). The zero value means the check did not run.
type EffectVerdict uint8

// Effect verdicts.
const (
	// EffectNone: no committed transaction has the intent's effect and the
	// object did not change state.
	EffectNone EffectVerdict = iota + 1
	// EffectCommitted: the first commitment of the intent's effect exists,
	// its business fields equal the intent's and the signer rule holds.
	EffectCommitted
	// EffectTaken: the object was committed by another account under the
	// signer rule, or with other business fields.
	EffectTaken
	// EffectStateChanged: the object changed state with no committing
	// transaction of the intent's kind (begin-block expiry, a council
	// force-withdraw, a transaction of another kind).
	EffectStateChanged
	// EffectSignerConflict: a transaction signed by the intent's pinned key
	// that the broadcaster never stored committed the effect (DR-11-15).
	EffectSignerConflict
)

// Effect is one effect check's result.
type Effect struct {
	Verdict EffectVerdict
	// Canonical is, for EffectCommitted, the hash of the first commitment.
	Canonical [32]byte
	// Own is, for EffectCommitted, whether Canonical is one of this
	// intent's attempts (own_transaction).
	Own bool
	// DeadAttempt is, for EffectCommitted, whether Canonical is a stored
	// attempt marked DEAD (plan 8.5 integrity rule).
	DeadAttempt bool
}

func (e Effect) validate() error {
	if e.Verdict > EffectSignerConflict {
		return fmt.Errorf("outbox: effect verdict %d unknown", e.Verdict)
	}
	if e.Verdict == EffectCommitted {
		if e.Canonical == ([32]byte{}) {
			return errors.New("outbox: a committed effect without its first commitment's hash")
		}
		return nil
	}
	if e.Canonical != ([32]byte{}) || e.Own || e.DeadAttempt {
		return errors.New("outbox: only a committed effect names a commitment")
	}
	return nil
}
