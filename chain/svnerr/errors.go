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
	ErrMalformed       = errors.Register(ModuleName, 7, "ERROR_CODE_MALFORMED")
	ErrUnknownField    = errors.Register(ModuleName, 8, "ERROR_CODE_UNKNOWN_FIELD")
	ErrOutOfRange      = errors.Register(ModuleName, 9, "ERROR_CODE_OUT_OF_RANGE")
	ErrConflict        = errors.Register(ModuleName, 14, "ERROR_CODE_CONFLICT")
	ErrDecisionBlocked = errors.Register(ModuleName, 17, "ERROR_CODE_DECISION_BLOCKED")
	ErrBounds          = errors.Register(ModuleName, 19, "ERROR_CODE_BOUNDS")
	ErrFeeRejected     = errors.Register(ModuleName, 21, "ERROR_CODE_FEE_REJECTED")
)
