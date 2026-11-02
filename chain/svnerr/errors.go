// Package svnerr is the chain's ABCI error set.
//
// Numeric codes match svn.types.v1.ErrorCode. Code 0 is ABCI success and is
// not registered here. ERROR_CODE_UNSPECIFIED is never returned.
package svnerr

import "cosmossdk.io/errors"

const ModuleName = "svn"

var (
	ErrUnauthorized    = errors.Register(ModuleName, 1, "ERROR_CODE_UNAUTHORIZED")
	ErrForbidden       = errors.Register(ModuleName, 2, "ERROR_CODE_FORBIDDEN")
	ErrReplay          = errors.Register(ModuleName, 3, "ERROR_CODE_REPLAY")
	ErrDuplicate       = errors.Register(ModuleName, 4, "ERROR_CODE_DUPLICATE_ID")
	ErrStale           = errors.Register(ModuleName, 5, "ERROR_CODE_STALE_VERSION")
	ErrExpired         = errors.Register(ModuleName, 6, "ERROR_CODE_EXPIRED")
	ErrMalformed       = errors.Register(ModuleName, 7, "ERROR_CODE_MALFORMED")
	ErrUnknownField    = errors.Register(ModuleName, 8, "ERROR_CODE_UNKNOWN_FIELD")
	ErrOutOfRange      = errors.Register(ModuleName, 9, "ERROR_CODE_OUT_OF_RANGE")
	ErrOverflow        = errors.Register(ModuleName, 10, "ERROR_CODE_OVERFLOW")
	ErrUnderflow       = errors.Register(ModuleName, 11, "ERROR_CODE_UNDERFLOW")
	ErrWrongDomain     = errors.Register(ModuleName, 12, "ERROR_CODE_WRONG_DOMAIN")
	ErrCapacity        = errors.Register(ModuleName, 13, "ERROR_CODE_CAPACITY")
	ErrConflict        = errors.Register(ModuleName, 14, "ERROR_CODE_CONFLICT")
	ErrWithdrawn       = errors.Register(ModuleName, 15, "ERROR_CODE_WITHDRAWN")
	ErrNotFound        = errors.Register(ModuleName, 16, "ERROR_CODE_NOT_FOUND")
	ErrDecisionBlocked = errors.Register(ModuleName, 17, "ERROR_CODE_DECISION_BLOCKED")
	ErrIntegrity       = errors.Register(ModuleName, 18, "ERROR_CODE_INTEGRITY")
	ErrBounds          = errors.Register(ModuleName, 19, "ERROR_CODE_BOUNDS")
	ErrQuorum          = errors.Register(ModuleName, 20, "ERROR_CODE_QUORUM")
	ErrFeeRejected     = errors.Register(ModuleName, 21, "ERROR_CODE_FEE_REJECTED")
)

// registry lists every registered error by its numeric code. Index 0 is
// ABCI success and has no entry.
var registry = [...]*errors.Error{
	1:  ErrUnauthorized,
	2:  ErrForbidden,
	3:  ErrReplay,
	4:  ErrDuplicate,
	5:  ErrStale,
	6:  ErrExpired,
	7:  ErrMalformed,
	8:  ErrUnknownField,
	9:  ErrOutOfRange,
	10: ErrOverflow,
	11: ErrUnderflow,
	12: ErrWrongDomain,
	13: ErrCapacity,
	14: ErrConflict,
	15: ErrWithdrawn,
	16: ErrNotFound,
	17: ErrDecisionBlocked,
	18: ErrIntegrity,
	19: ErrBounds,
	20: ErrQuorum,
	21: ErrFeeRejected,
}

// MaxCode is the highest registered code.
const MaxCode uint32 = uint32(len(registry) - 1)

// ByCode returns the registered error for one numeric code. Code 0 and any
// code above MaxCode return false.
func ByCode(code uint32) (*errors.Error, bool) {
	if code == 0 || code > MaxCode {
		return nil, false
	}
	return registry[code], true
}
