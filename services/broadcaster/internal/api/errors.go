package api

import (
	"errors"
	"net/http"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// Answer codes of error responses. A code never carries a value from the
// request, and no answer echoes an internal error.
const (
	codeMalformed          = "MALFORMED"
	codeInvalid            = "INVALID_REQUEST"
	codeUnauthenticated    = "UNAUTHENTICATED"
	codeCallerUnknown      = "CALLER_UNKNOWN"
	codeNotFound           = "NOT_FOUND"
	codeIdempotency        = "IDEMPOTENCY_CONFLICT"
	codeContentUnavailable = string(policy.CodeContentIDTaken)
	codeNotCancellable     = "NOT_CANCELLABLE"
	codeTxTracked          = "TRANSACTION_ALREADY_TRACKED"
	codeBodyTooLarge       = "BODY_TOO_LARGE"
	codeCapReached         = "OPEN_INTENT_CAP"
	codeUnavailable        = "UNAVAILABLE"
	codeOverloaded         = "OVERLOADED"
	codeInternal           = "INTERNAL"
	codeIntegrity          = "INTEGRITY"
	codeLifetime           = "LIFETIME_OUTSIDE_BOUNDS"
)

// apiError is an answer: an HTTP status and a code.
type apiError struct {
	status int
	code   string
	// cause is logged (through the redacting logger), never answered.
	cause error
}

func (e *apiError) Error() string { return e.code }

func fail(status int, code string, cause error) *apiError {
	return &apiError{status: status, code: code, cause: cause}
}

// errorBody is the JSON of an error answer.
type errorBody struct {
	Error struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	} `json:"error"`
}

// requestError maps a request decoding failure: Malformed is 400, Invalid
// and UnknownKind are 422.
func requestError(err error) *apiError {
	switch request.ClassOf(err) {
	case request.Malformed:
		return fail(http.StatusBadRequest, codeMalformed, err)
	case request.Invalid:
		return fail(http.StatusUnprocessableEntity, codeInvalid, err)
	case request.UnknownKind:
		return fail(http.StatusUnprocessableEntity, string(policy.CodeUnknownKind), err)
	}
	return fail(http.StatusInternalServerError, codeInternal, err)
}

// policyError maps a policy decision. A refusal is answered with its code:
// a missing kind, permission, registered intent key or relay authority is
// 403; an object the caller may not see is the uniform 404; a content id
// that is unavailable is the uniform 409; every other refusal (shape,
// signatures, provider and offer state, forward rules) is 422. An error
// that is not a refusal is a failed read: 503, retry later.
func policyError(err error) *apiError {
	code := policy.CodeOf(err)
	switch code {
	case "":
		return fail(http.StatusServiceUnavailable, codeUnavailable, err)
	case policy.CodeKindNotAllowed, policy.CodePermission, policy.CodeIntentKeyUnregistered, policy.CodeRelayNotAllowed:
		return fail(http.StatusForbidden, string(code), err)
	case policy.CodeNotVisible:
		return fail(http.StatusNotFound, codeNotFound, err)
	case policy.CodeContentIDTaken:
		return fail(http.StatusConflict, codeContentUnavailable, err)
	case policy.CodeSignerMismatch, policy.CodeRole, policy.CodeChainID:
		// The broadcaster built the message with its own signer: a
		// mismatch here is a wiring fault, not the caller's.
		return fail(http.StatusInternalServerError, codeInternal, err)
	}
	return fail(http.StatusUnprocessableEntity, string(code), err)
}

// storeError maps an outbox failure.
func storeError(err error) *apiError {
	switch {
	case errors.Is(err, store.ErrNotFound):
		return fail(http.StatusNotFound, codeNotFound, err)
	case errors.Is(err, store.ErrIdempotencyConflict):
		return fail(http.StatusConflict, codeIdempotency, err)
	case errors.Is(err, store.ErrContentUnavailable):
		return fail(http.StatusConflict, codeContentUnavailable, err)
	case errors.Is(err, store.ErrTxTracked):
		return fail(http.StatusConflict, codeTxTracked, err)
	case errors.Is(err, store.ErrStateChanged):
		return fail(http.StatusConflict, codeNotCancellable, err)
	case errors.Is(err, store.ErrCapReached):
		return fail(http.StatusTooManyRequests, codeCapReached, err)
	case errors.Is(err, store.ErrCallerUnknown):
		return fail(http.StatusForbidden, codeCallerUnknown, err)
	case errors.Is(err, store.ErrInvalid):
		return fail(http.StatusUnprocessableEntity, codeInvalid, err)
	case errors.Is(err, store.ErrSignerClass), errors.Is(err, store.ErrForward), errors.Is(err, store.ErrContentOwner),
		errors.Is(err, store.ErrChainMismatch):
		return fail(http.StatusInternalServerError, codeInternal, err)
	}
	// Lock and statement timeouts, an unbound chain, a lost connection.
	return fail(http.StatusServiceUnavailable, codeUnavailable, err)
}

// writeError answers e and logs its cause through the redacting logger.
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, e *apiError) {
	if e.status >= 500 {
		s.log.Warn("intent request failed", "method", r.Method, "route", r.Pattern, "http_status", e.status,
			"code", e.code, "err", e.cause)
	} else {
		s.log.Info("intent request refused", "method", r.Method, "route", r.Pattern, "http_status", e.status,
			"code", e.code)
	}
	var body errorBody
	body.Error.Status, body.Error.Code = e.status, e.code
	s.writeJSON(w, r, e.status, body)
}
