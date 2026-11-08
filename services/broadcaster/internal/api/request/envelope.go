package request

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/netip"
	"regexp"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/config/strictjson"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// Envelope is the body of POST /v1/intents:
//
//	{"kind": "REGISTER_CONTENT", "idempotency_key": "...",
//	 "expires_in_seconds": 600, "auth_context": {...}, "request": {...}}
//
// The SQL path (broadcaster.enqueue_intent) carries the same kind, request
// object, authorization context and idempotency key as arguments.
type Envelope struct {
	Kind             string          `json:"kind"`
	IdempotencyKey   string          `json:"idempotency_key"`
	ExpiresInSeconds int64           `json:"expires_in_seconds"`
	AuthContext      json.RawMessage `json:"auth_context"`
	Request          json.RawMessage `json:"request"`
}

// DecodeEnvelope decodes a request body strictly. The kind is only checked
// for its spelling here; LookupKind refuses one that is not routed.
func DecodeEnvelope(body []byte) (Envelope, error) {
	var env Envelope
	if err := strictjson.Decode(body, &env); err != nil {
		return Envelope{}, malformed("", "body is not the intent request shape")
	}
	switch {
	case !kindRE.MatchString(env.Kind):
		return Envelope{}, malformed("kind", "missing or not an upper-case kind name")
	case !msgkit.ValidID(env.IdempotencyKey):
		return Envelope{}, malformed("idempotency_key", "missing or outside the id pattern")
	case env.ExpiresInSeconds < 1:
		return Envelope{}, malformed("expires_in_seconds", "missing or not positive")
	case len(env.AuthContext) == 0 || !isObject(env.AuthContext):
		return Envelope{}, malformed("auth_context", "missing or not a JSON object")
	case len(env.Request) == 0 || !isObject(env.Request):
		return Envelope{}, malformed("request", "missing or not a JSON object")
	}
	return env, nil
}

// isObject reports whether raw starts like a JSON object.
func isObject(raw json.RawMessage) bool {
	t := bytes.TrimLeft(raw, " \t\r\n")
	return len(t) > 0 && t[0] == '{'
}

// LookupKind returns the routing of kind; an unknown kind is UnknownKind.
func LookupKind(kind string) (policy.Spec, error) {
	if !kindRE.MatchString(kind) {
		return policy.Spec{}, &Error{Class: UnknownKind, Field: "kind", Rule: "not a routed kind"}
	}
	spec, err := policy.SpecOf(policy.Kind(kind))
	if err != nil {
		return policy.Spec{}, &Error{Class: UnknownKind, Field: "kind", Rule: "not a routed kind"}
	}
	return spec, nil
}

// dottedQuadRE matches an IPv4 address in any digit spelling, including
// leading zeros that netip.ParseAddr refuses.
var dottedQuadRE = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){3}$`)

// MaxAuthContextBytes is the hard upper bound of an authorization context
// (plan section 11: 256 to 8,192 bytes; the configured cap is lower).
const MaxAuthContextBytes = 8192

// DecodeAuthContext decodes the authorized request context (plan 5.5,
// owner answer Q4 "Minimal + clear unsent (Recommended)") of at most
// maxBytes (1 to MaxAuthContextBytes) bytes and returns it with its
// canonical JSON, the only form that is stored and digested.
//
// The context has exactly four fields: principal_kind (DEV_FIXTURE),
// principal_id and policy_ref (protocol ids), and permissions (one to four
// distinct permission names). Every other key is refused, so an email, an
// IP address, a user agent, a token, a cookie, a header copy or free text
// cannot be stored under another name; the id pattern refuses '@', ':',
// '/', spaces and upper case, and an id that parses as an IP address is
// refused too. Opaque ids are still linkable (REQ-OFF-004). Every failure
// is Malformed.
func DecodeAuthContext(raw []byte, maxBytes int) (policy.AuthContext, []byte, error) {
	if maxBytes < 1 || maxBytes > MaxAuthContextBytes {
		return policy.AuthContext{}, nil, errors.New("request: auth context cap outside [1, 8192]")
	}
	if len(raw) == 0 || len(raw) > maxBytes {
		return policy.AuthContext{}, nil, malformed("auth_context", "empty or above the size cap")
	}
	var a policy.AuthContext
	if err := strictjson.Decode(raw, &a); err != nil {
		return policy.AuthContext{}, nil, malformed("auth_context", "not the four-field authorization context")
	}
	for field, id := range map[string]string{"auth_context.principal_id": a.PrincipalID, "auth_context.policy_ref": a.PolicyRef} {
		if _, err := netip.ParseAddr(id); err == nil || dottedQuadRE.MatchString(id) {
			return policy.AuthContext{}, nil, malformed(field, "an IP address is not an opaque id")
		}
	}
	if err := a.Validate(); err != nil {
		return policy.AuthContext{}, nil, malformed("auth_context", "fields break the authorization context rules")
	}
	canonical := a.Canonical()
	out, err := CanonicalAuthJSON(canonical)
	if err != nil {
		return policy.AuthContext{}, nil, err
	}
	return canonical, out, nil
}

// CanonicalAuthJSON returns the JSON of a's canonical form: the four fields
// in declaration order and the permissions sorted.
func CanonicalAuthJSON(a policy.AuthContext) ([]byte, error) {
	out, err := json.Marshal(a.Canonical())
	if err != nil {
		return nil, errors.New("request: auth context does not encode")
	}
	return out, nil
}
