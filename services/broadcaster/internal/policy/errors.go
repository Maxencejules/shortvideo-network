package policy

import (
	"errors"
	"fmt"
)

// Code is a stable refusal code. The API maps codes to HTTP answers, the
// signer returns them over its socket, and the worker records them as
// failure reasons. A code never carries a value from the request.
type Code string

// Refusal codes.
const (
	// Request shape and kind.
	CodeInvalid         Code = "INVALID"
	CodeUnknownKind     Code = "UNKNOWN_KIND"
	CodeKindNotAllowed  Code = "KIND_NOT_ALLOWED"
	CodeTypeNotAllowed  Code = "TYPE_NOT_ALLOWED"
	CodeSignerMismatch  Code = "SIGNER_MISMATCH"
	CodeAuthorityEnd    Code = "AUTHORITY_END"
	CodeChainID         Code = "CHAIN_ID"
	CodeGasBounds       Code = "GAS_BOUNDS"
	CodeTimeoutZero     Code = "TIMEOUT_ZERO"
	CodeTimeoutTooFar   Code = "TIMEOUT_TOO_FAR"
	CodeNoFreshHeight   Code = "NO_FRESH_HEIGHT"
	CodePermission      Code = "PERMISSION_MISSING"
	CodeNotVisible      Code = "NOT_VISIBLE"
	CodeContentIDTaken  Code = "CONTENT_ID_UNAVAILABLE"
	CodeRelayNotAllowed Code = "RELAY_NOT_ALLOWED"

	// Hosting intents (Q3).
	CodeIntentKeyUnregistered Code = "INTENT_KEY_UNREGISTERED"
	CodeIntentSignature       Code = "INTENT_SIGNATURE_INVALID"

	// Provider consent (Q2).
	CodeProviderSignature   Code = "PROVIDER_SIGNATURE_INVALID"
	CodeProviderNotFound    Code = "PROVIDER_NOT_FOUND"
	CodeProviderNotAdmitted Code = "PROVIDER_NOT_ADMITTED"
	CodeOfferNotFound       Code = "OFFER_NOT_FOUND"
	CodeOfferExpired        Code = "OFFER_EXPIRED"
	CodeLeaseNotFound       Code = "LEASE_NOT_FOUND"

	// Verbatim forwarding (Q2, P3).
	CodeForwardShape     Code = "FORWARD_SHAPE"
	CodeForwardDeadline  Code = "FORWARD_DEADLINE"
	CodeForwardSigner    Code = "FORWARD_SIGNER"
	CodeForwardSignature Code = "FORWARD_OUTER_SIGNATURE"
	CodeForwardSequence  Code = "FORWARD_SEQUENCE_USED"

	// Start-up.
	CodeRole Code = "ROLE"
)

// Refusal is a policy decision against a request: the request is not
// allowed as it stands, and retrying it unchanged gives the same answer.
// Detail names fields and types, never values.
type Refusal struct {
	Code   Code
	Detail string
}

func (r *Refusal) Error() string {
	if r.Detail == "" {
		return "policy: " + string(r.Code)
	}
	return fmt.Sprintf("policy: %s: %s", r.Code, r.Detail)
}

// refuse returns a Refusal.
func refuse(code Code, format string, args ...any) error {
	return &Refusal{Code: code, Detail: fmt.Sprintf(format, args...)}
}

// CodeOf returns the refusal code of err, or "" when err is not a
// Refusal (for example a chain read that failed: retry later).
func CodeOf(err error) Code {
	var r *Refusal
	if errors.As(err, &r) {
		return r.Code
	}
	return ""
}
