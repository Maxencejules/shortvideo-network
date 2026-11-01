// Package msgkit is the shared, deterministic message toolkit for the
// registry modules (x/providers, x/content) and the authority hooks.
//
// Everything here runs inside chain execution. It reads block data only
// from sdk.Context, iterates stores in key order, and never reads the local
// clock, randomness, the environment, files, or the network. A source scan
// test (msgkittest.AssertDeterministicSources) enforces that for this package.
package msgkit

import (
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Code is a protocol error name from svn.types.v1.ErrorCode. The empty code
// is success. Handlers return Code.Err() so ABCI results carry codespace
// "svn" and the numeric value of the protocol enum.
type Code string

// OK is success. It is ABCI code 0, not ERROR_CODE_UNSPECIFIED.
const OK Code = ""

// The 21 protocol failure codes, in enum order.
const (
	Unauthorized    Code = "ERROR_CODE_UNAUTHORIZED"
	Forbidden       Code = "ERROR_CODE_FORBIDDEN"
	Replay          Code = "ERROR_CODE_REPLAY"
	DuplicateID     Code = "ERROR_CODE_DUPLICATE_ID"
	StaleVersion    Code = "ERROR_CODE_STALE_VERSION"
	Expired         Code = "ERROR_CODE_EXPIRED"
	Malformed       Code = "ERROR_CODE_MALFORMED"
	UnknownField    Code = "ERROR_CODE_UNKNOWN_FIELD"
	OutOfRange      Code = "ERROR_CODE_OUT_OF_RANGE"
	Overflow        Code = "ERROR_CODE_OVERFLOW"
	Underflow       Code = "ERROR_CODE_UNDERFLOW"
	WrongDomain     Code = "ERROR_CODE_WRONG_DOMAIN"
	Capacity        Code = "ERROR_CODE_CAPACITY"
	Conflict        Code = "ERROR_CODE_CONFLICT"
	Withdrawn       Code = "ERROR_CODE_WITHDRAWN"
	NotFound        Code = "ERROR_CODE_NOT_FOUND"
	DecisionBlocked Code = "ERROR_CODE_DECISION_BLOCKED"
	Integrity       Code = "ERROR_CODE_INTEGRITY"
	Bounds          Code = "ERROR_CODE_BOUNDS"
	Quorum          Code = "ERROR_CODE_QUORUM"
	FeeRejected     Code = "ERROR_CODE_FEE_REJECTED"
)

// Err returns nil for OK. A protocol failure code returns the registered
// svnerr error wrapped with the code name. ERROR_CODE_UNSPECIFIED and any
// name outside the protocol enum are wiring errors; they return
// ERROR_CODE_MALFORMED so an unknown name can never become success.
func (c Code) Err() error {
	if c == OK {
		return nil
	}
	number, known := typesv1.ErrorCode_value[string(c)]
	if known && number > 0 {
		if registered, ok := svnerr.ByCode(uint32(number)); ok {
			return registered.Wrap(string(c))
		}
	}
	return svnerr.ErrMalformed.Wrapf("unknown error code %q", string(c))
}

// Outcome is the result of a gate such as Ledger.Gate. Idempotent means the
// same business id already committed the same payload: the handler returns
// success, writes nothing, and emits no typed event.
type Outcome struct {
	Code       Code
	Idempotent bool
}
