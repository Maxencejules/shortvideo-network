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
	// MaxRecords is the deterministic write quota for proposals and pending
	// power changes. It matches the expiry-step cap. Provider and content
	// records live in x/providers and x/content, not here.
	MaxRecords = 128
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
