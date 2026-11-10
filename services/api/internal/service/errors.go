package service

import "errors"

var (
	errUnauthenticated = errors.New("unauthenticated")
	errRole            = errors.New("role")
	errNotFound        = errors.New("not found")
	errConflict        = errors.New("conflict")
	errDigest          = errors.New("digest")
	errInputQuota      = errors.New("input quota")
	errOutputQuota     = errors.New("output quota")
	errNoAllowance     = errors.New("no allowance")
	errLimit           = errors.New("limit")
	errRate            = errors.New("rate")
	errMalformed       = errors.New("malformed")
	errDuration        = errors.New("duration")
	errDimensions      = errors.New("dimensions")
	errState           = errors.New("state")
	errExpired         = errors.New("expired")
	errBusy            = errors.New("busy")
	errDone            = errors.New("done")
)

type apiError struct {
	Status  int
	Code    string
	Message string
}

func describe(err error) apiError {
	switch {
	case errors.Is(err, errUnauthenticated), errors.Is(err, errExpired):
		return apiError{401, "UNAUTHENTICATED", "authentication is required"}
	case errors.Is(err, errRole):
		return apiError{403, "ROLE", "role cannot perform this action"}
	case errors.Is(err, errNoAllowance):
		return apiError{403, "NO_ALLOWANCE", "account has no upload allowance"}
	case errors.Is(err, errInputQuota):
		return apiError{403, "QUOTA_INPUT", "input allowance exceeded"}
	case errors.Is(err, errOutputQuota):
		return apiError{403, "QUOTA_OUTPUT", "processed output budget exceeded"}
	case errors.Is(err, errRate):
		return apiError{429, "RATE_LIMIT", "rate limit exceeded"}
	case errors.Is(err, errLimit):
		return apiError{400, "LIMIT", "input exceeds the proposed limit"}
	case errors.Is(err, errMalformed):
		return apiError{422, "MALFORMED", "input structure was rejected"}
	case errors.Is(err, errDuration):
		return apiError{422, "DURATION", "duration exceeds the proposed limit"}
	case errors.Is(err, errDimensions):
		return apiError{422, "DIMENSIONS", "dimensions exceed the proposed limit"}
	case errors.Is(err, errDigest):
		return apiError{409, "DIGEST_MISMATCH", "completion digest does not match the stored bytes"}
	case errors.Is(err, errConflict):
		return apiError{409, "CONFLICT", "request conflicts with the stored upload"}
	case errors.Is(err, errState):
		return apiError{409, "STATE", "upload is not in a state that accepts this action"}
	case errors.Is(err, errBusy):
		return apiError{409, "BUSY", "upload probe is already in progress"}
	case errors.Is(err, errNotFound):
		return apiError{404, "NOT_FOUND", "upload not found"}
	default:
		return apiError{500, "STORE", "upload request failed"}
	}
}
