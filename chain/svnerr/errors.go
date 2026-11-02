// Package svnerr is the chain's ABCI error set.
//
// Numeric codes match svn.types.v1.ErrorCode. Code 0 is ABCI success and is
// not registered here.
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
	ErrConflict        = errors.Register(ModuleName, 14, "ERROR_CODE_CONFLICT")
	ErrWithdrawn       = errors.Register(ModuleName, 15, "ERROR_CODE_WITHDRAWN")
	ErrNotFound        = errors.Register(ModuleName, 16, "ERROR_CODE_NOT_FOUND")
	ErrDecisionBlocked = errors.Register(ModuleName, 17, "ERROR_CODE_DECISION_BLOCKED")
	ErrIntegrity       = errors.Register(ModuleName, 18, "ERROR_CODE_INTEGRITY")
	ErrBounds          = errors.Register(ModuleName, 19, "ERROR_CODE_BOUNDS")
	ErrQuorum          = errors.Register(ModuleName, 20, "ERROR_CODE_QUORUM")
	ErrFeeRejected     = errors.Register(ModuleName, 21, "ERROR_CODE_FEE_REJECTED")
)
