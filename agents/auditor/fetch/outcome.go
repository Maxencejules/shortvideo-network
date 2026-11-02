package fetch

import "github.com/Maxencejules/shortvideo-network/agents/auditor/policy"

// Kind is the typed outcome of one provider request. A Kind is an
// observation. It does not say PASS, FAIL, or INCOMPLETE; that mapping
// depends on owner question Q3 and is not in this package.
type Kind string

// Outcome kinds.
const (
	// OK: status 200 (or 206 with the exact Content-Range), identity
	// encoding, and exactly the expected number of body bytes followed by a
	// clean end of body.
	OK Kind = "OK"
	// PolicyRefused: the URL or a resolved address failed the policy;
	// nothing was dialed. Outcome.Refusal names the rule.
	PolicyRefused Kind = "POLICY_REFUSED"
	// DNSFailure: the resolver returned an error or no address.
	DNSFailure Kind = "DNS_FAILURE"
	// ConnectFailed: no vetted address accepted a TCP connection.
	ConnectFailed Kind = "CONNECT_FAILED"
	// TLSFailed: the TLS handshake failed (untrusted certificate, wrong
	// host name, protocol version below TLS 1.2, ...).
	TLSFailed Kind = "TLS_FAILED"
	// Timeout: a connect, handshake, response-header, or idle-read timeout,
	// or the per-object budget, expired.
	Timeout Kind = "TIMEOUT"
	// ConnectionReset: the connection ended without a complete response:
	// before any response byte, inside a declared Content-Length or a chunk,
	// or, for a body with neither a Content-Length nor chunked framing,
	// before the expected size. The last case includes a TLS close_notify:
	// the client cannot tell it from a TCP close at a record boundary, which
	// an on-path party can cause.
	ConnectionReset Kind = "CONNECTION_RESET"
	// ProtocolError: response bytes arrived but were not a valid HTTP/1.1
	// response, or the headers exceeded MaxResponseHeaderBytes.
	ProtocolError Kind = "PROTOCOL_ERROR"
	// RedirectRefused: a 3xx answer. It is never followed. Only the
	// SHA-256 of its Location value is kept.
	RedirectRefused Kind = "REDIRECT_REFUSED"
	// HTTPStatus: a status other than 200 to a whole-object request, or
	// other than 200 and 206 to a range request.
	HTTPStatus Kind = "HTTP_STATUS"
	// EncodingRefused: a Content-Encoding other than identity.
	EncodingRefused Kind = "ENCODING_REFUSED"
	// RangeUnsupported: a range request answered with 200, or with 206 and
	// a Content-Range that is not exactly the requested range of the
	// expected total.
	RangeUnsupported Kind = "RANGE_UNSUPPORTED"
	// TooLarge: a declared Content-Length above the expected size, or a
	// body that went on past the expected size (read stops at expected+1).
	TooLarge Kind = "TOO_LARGE"
	// LengthMismatch: a declared Content-Length below the expected size, or
	// a chunked body whose last chunk came before the expected size. Both
	// are the provider's own framing, inside TLS.
	LengthMismatch Kind = "LENGTH_MISMATCH"
)

// Outcome is the result of Open or Get.
type Outcome struct {
	Kind Kind
	// Status is the HTTP status code when a response arrived, else 0.
	Status int
	// BytesRead counts body bytes read, including the one byte past the
	// expected size that detects TooLarge.
	BytesRead uint64
	// Refusal is the policy refusal (PolicyRefused only).
	Refusal *policy.Refusal
	// LocationSHA256 is the SHA-256 of the first Location header value
	// (RedirectRefused with a Location header only). The value itself is
	// provider text and is not kept.
	LocationSHA256 *[32]byte
	// NotFound is true when the resolver reported that the name does not
	// exist (DNSFailure only).
	NotFound bool
	// Detail is fixed text from this package or, for PolicyRefused, the
	// policy refusal text. It never contains provider-supplied text, a URL,
	// or an address.
	Detail string
}
