package authority

import (
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
)

// Protocol error names. ABCI success is the empty code.
const (
	CodeUnauthorized    = "ERROR_CODE_UNAUTHORIZED"
	CodeForbidden       = "ERROR_CODE_FORBIDDEN"
	CodeReplay          = "ERROR_CODE_REPLAY"
	CodeDuplicate       = "ERROR_CODE_DUPLICATE_ID"
	CodeStale           = "ERROR_CODE_STALE_VERSION"
	CodeExpired         = "ERROR_CODE_EXPIRED"
	CodeMalformed       = "ERROR_CODE_MALFORMED"
	CodeOutOfRange      = "ERROR_CODE_OUT_OF_RANGE"
	CodeOverflow        = "ERROR_CODE_OVERFLOW"
	CodeConflict        = "ERROR_CODE_CONFLICT"
	CodeWithdrawn       = "ERROR_CODE_WITHDRAWN"
	CodeNotFound        = "ERROR_CODE_NOT_FOUND"
	CodeDecisionBlocked = "ERROR_CODE_DECISION_BLOCKED"
	CodeIntegrity       = "ERROR_CODE_INTEGRITY"
	CodeBounds          = "ERROR_CODE_BOUNDS"
	CodeQuorum          = "ERROR_CODE_QUORUM"
)

// RemovalBindCommit is the Goal 06 decision for a due removal whose consensus
// key does not match the key that validator holds.
//
// The spec machine rolls the CommitBlock back (keep is false), so the pending
// entry remains and the next commit fails the same way. That is a halt.
// Dropping the entry would commit the block and forget the removal. Halt is
// the behavior: EndBlock returns an error and the block is not committed.
const RemovalBindCommit = "halt"

// Approved council shape from DEC-008. The devnet 3-seat / threshold-2
// fixture is not this policy.
const (
	ApprovedSeats     = 4
	ApprovedThreshold = 3
)

// Store quotas. The frozen spec machine has neither cap. Both are PROPOSED
// values recorded as decision requests, not approved policy.
const (
	// MaxOpenProposalsPerSeat is PROPOSED (DR-06-03). It counts the OPEN
	// and QUEUED proposals of one council seat on the current base policy
	// version. EXECUTED, EXPIRED and REJECTED proposals never count, and a
	// proposal on an older base, which can never execute, stops counting
	// when an execute bumps the version. One seat cannot fill the quota of
	// the others. A slot on the current version is freed when its proposal
	// executes, is REJECTED, or is marked EXPIRED: an approve marks an
	// expired OPEN proposal, and only an authority execute marks an expired
	// QUEUED one (approve of a QUEUED proposal is CONFLICT before the expiry
	// check, as in the spec). A current-version proposal that is never
	// approved, or whose execute fails with any code other than FORBIDDEN
	// (for example NOT_FOUND, OUT_OF_RANGE, CONFLICT, BOUNDS, DUPLICATE_ID),
	// stays OPEN or QUEUED and holds its slot until any execute bumps the
	// version, or until it is marked EXPIRED after its own expiry, which the
	// proposer may set as late as unix_seconds_max.
	MaxOpenProposalsPerSeat = 32
	// MaxPendingChanges is PROPOSED (DR-06-04). It caps scheduled validator
	// changes. It equals safety_max_expiry_steps_per_block (bounds.json), so
	// one EndBlock never applies more than 128 entries. No message cancels a
	// pending entry: 128 entries with far activation points, which take a
	// council majority to schedule, block every later validator change,
	// including a compromised-key replacement, until they apply.
	MaxPendingChanges = 128
)

// Outcome is one authority transition.
//
// Keep is set when the spec machine commits the trial even though the code
// is not success: expiry, and FORBIDDEN which marks the proposal REJECTED.
// Cosmos SDK v0.55 discards the message branch when the handler returns an
// error, so a Keep outcome is saved and the handler returns success with
// the code on an event. The payload is still not applied.
type Outcome struct {
	Code       string
	Keep       bool
	Idempotent bool
}

func ok() Outcome { return Outcome{} }

func fail(code string) Outcome { return Outcome{Code: code} }

func keep(code string) Outcome { return Outcome{Code: code, Keep: true} }

func (o Outcome) Success() bool { return o.Code == "" }

func sdkError(code string) error {
	switch code {
	case CodeUnauthorized:
		return svnerr.ErrUnauthorized.Wrap(code)
	case CodeForbidden:
		return svnerr.ErrForbidden.Wrap(code)
	case CodeReplay:
		return svnerr.ErrReplay.Wrap(code)
	case CodeDuplicate:
		return svnerr.ErrDuplicate.Wrap(code)
	case CodeStale:
		return svnerr.ErrStale.Wrap(code)
	case CodeExpired:
		return svnerr.ErrExpired.Wrap(code)
	case CodeMalformed:
		return svnerr.ErrMalformed.Wrap(code)
	case CodeOutOfRange:
		return svnerr.ErrOutOfRange.Wrap(code)
	case CodeOverflow:
		return svnerr.ErrOverflow.Wrap(code)
	case CodeConflict:
		return svnerr.ErrConflict.Wrap(code)
	case CodeWithdrawn:
		return svnerr.ErrWithdrawn.Wrap(code)
	case CodeNotFound:
		return svnerr.ErrNotFound.Wrap(code)
	case CodeDecisionBlocked:
		return svnerr.ErrDecisionBlocked.Wrap(code)
	case CodeIntegrity:
		return svnerr.ErrIntegrity.Wrap(code)
	case CodeBounds:
		return svnerr.ErrBounds.Wrap(code)
	case CodeQuorum:
		return svnerr.ErrQuorum.Wrap(code)
	default:
		// A registry hook can return a protocol code authority never
		// produces itself. msgkit maps it; an unknown name is MALFORMED.
		if err := msgkit.Code(code).Err(); err != nil {
			return err
		}
		return svnerr.ErrMalformed.Wrap(code)
	}
}
