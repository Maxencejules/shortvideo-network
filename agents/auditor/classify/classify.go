// Package classify maps what the producer observed to an audit result,
// PASS, FAIL, or INCOMPLETE, under the owner's ruling on Q3, "Provider-
// answered only" (decision request DR-09-09; reason set interim under
// DR-09-22).
//
// FAIL (submitted; the chain stores it as REJECTED) only when the
// provider's certificate-verified server answered the exact request with
// wrong or missing bytes:
//
//   - SEGMENT_DIGEST_MISMATCH: a segment received in full has another
//     SHA-256 than the manifest's
//   - PACKAGE_LENGTH_MISMATCH: fewer bytes than expected under the
//     provider's own authenticated framing (a declared Content-Length below
//     the expected size, or a chunked body whose last chunk came early)
//   - TOO_LARGE: a declared length above the expected size, or bytes past it
//   - HTTP_STATUS: 404 or 410 at once; any other 4xx or 5xx only when it is
//     still the answer after the configured retries (Class.Retry), except
//     the three below
//
// Every other outcome is INCOMPLETE and stays off chain: connection, TLS,
// timeout, reset, DNS, redirect, compression, range, protocol, policy, and
// input problems, chain-read problems, and lease or content changes. Three
// statuses belong to those groups, not to HTTP_STATUS:
//
//   - 408 (Request Timeout) is TIMEOUT: the server says the request did not
//     arrive in time, so it did not answer the request
//   - 416 and 501 in answer to a range request (every request of a
//     SAMPLED_SEGMENTS audit) are RANGE_UNSUPPORTED: range requests are a
//     goal-09 extension, protocol v0 defines only the 200 answer to a
//     whole-package request, and a refused range does not say which bytes
//     the provider holds. A truncated package therefore fails a
//     WHOLE_PACKAGE audit (PACKAGE_LENGTH_MISMATCH), not a sampled one
//
// The report of an INCOMPLETE audit is kept like any other. A kind, rule, or
// state this package does not know is INCOMPLETE (UNCLASSIFIED), never FAIL.
//
// A FAIL reason wins over INCOMPLETE reasons in the same audit: a segment
// received in full over the verified connection that has the wrong digest is
// the provider's answer even when the connection was cut later.
//
// The endpoint must be one the provider account declared (interim choice
// N2, 2026-11-06). Q3 names "the provider's verified server". A certificate
// shows only that the server holds the host name; it does not show that the
// host is the provider's. The producer takes a server's answer as the
// provider's only when its endpoint comes from a capability manifest that
// names the provider and its current signing key id and hashes to the
// endpoint_manifest_digest the provider account committed on chain
// (Source CapabilityManifest; the producer checks the manifest before any
// request). That shows the provider account declared the endpoint, not that
// the provider controls the host. An endpoint the operator typed into the
// configuration (Source OperatorConfig) is bound to nothing. So Observation and Reasons, the
// functions that decide an audit's result, take the endpoint source, and
// every result that would be FAIL from any source other than
// CapabilityManifest is INCOMPLETE with the added reason ENDPOINT_UNBOUND.
// The provider-answered reasons stay in the report, so the operator still
// sees what the server answered; the result is not FAIL, so no REJECTED
// record follows. PASS is not changed by the source: a PASS from an
// operator-configured endpoint is still PASS and can be signed (RECORDED on
// chain until its expiry, at most 86400 s, PROPOSED); its report says
// OPERATOR_CONFIG. That asymmetry is a documented residual risk.
// batch.NewItem refuses to sign a FAIL report whose source is not
// CAPABILITY_MANIFEST, and the reviewer reports such a signed item as
// RECORD_MISMATCH.
package classify

import (
	"fmt"
	"net/http"
	"sort"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/verify"
)

// Reason is one report reason.
type Reason string

// FAIL reasons: the provider's verified server answered with wrong or
// missing bytes.
const (
	SegmentDigestMismatch Reason = "SEGMENT_DIGEST_MISMATCH"
	PackageLengthMismatch Reason = "PACKAGE_LENGTH_MISMATCH"
	TooLarge              Reason = "TOO_LARGE"
	HTTPStatus            Reason = "HTTP_STATUS"
)

// INCOMPLETE reasons of a request.
const (
	ConnectFailed        Reason = "CONNECT_FAILED"
	TLSFailed            Reason = "TLS_FAILED"
	Timeout              Reason = "TIMEOUT"
	ConnectionReset      Reason = "CONNECTION_RESET"
	ProtocolError        Reason = "PROTOCOL_ERROR"
	DNSFailure           Reason = "DNS_FAILURE"
	EndpointNotPermitted Reason = "ENDPOINT_NOT_PERMITTED"
	RedirectRefused      Reason = "REDIRECT_REFUSED"
	EncodingRefused      Reason = "ENCODING_REFUSED"
	// RangeUnsupported is a range request answered with 200, with 206 and
	// another Content-Range, or with 416 or 501.
	RangeUnsupported Reason = "RANGE_UNSUPPORTED"
	// UnexpectedStatus is a status that is neither 200/206 nor 4xx/5xx
	// (for example 204). The ruling names only 4xx and 5xx as failures.
	UnexpectedStatus Reason = "HTTP_STATUS_UNEXPECTED"
	PolicyScheme     Reason = "POLICY_SCHEME"
	PolicyPort       Reason = "POLICY_PORT"
	// PolicyURL is any other URL rule: length, syntax, userinfo,
	// fragment, path, host.
	PolicyURL Reason = "POLICY_URL"
	// NotRequested marks sampled segments after the request that ended the
	// audit; it is a segment reason only.
	NotRequested Reason = "NOT_REQUESTED"
	// Unclassified is an outcome kind, rule, or state this package does not
	// know. It is never FAIL.
	Unclassified Reason = "UNCLASSIFIED"
)

// INCOMPLETE reasons of inputs, chain reads, and eligibility. Nothing is
// requested from a provider when one of these is the result.
const (
	InputManifestInvalid   Reason = "INPUT_MANIFEST_INVALID"
	InputManifestMismatch  Reason = "INPUT_MANIFEST_MISMATCH"
	InputCapabilityInvalid Reason = "INPUT_CAPABILITY_INVALID"
	InputContentWithdrawn  Reason = "INPUT_CONTENT_WITHDRAWN"
	InputContentNotFound   Reason = "INPUT_CONTENT_NOT_FOUND"
	LeaseNotHeld           Reason = "LEASE_NOT_HELD"
	LeaseNearDeadline      Reason = "LEASE_NEAR_DEADLINE"
	LeaseChanged           Reason = "LEASE_CHANGED"
	ConfigInvalid          Reason = "CONFIG_INVALID"
	ChainReadFailed        Reason = "CHAIN_READ_FAILED"
	RunBudgetExhausted     Reason = "RUN_BUDGET_EXHAUSTED"
)

// EndpointUnbound is added to an audit that would be FAIL when its endpoint
// did not come from a capability manifest bound to the provider's committed
// endpoint_manifest_digest: the endpoint was not declared by the provider
// account, so its answer stays off chain (N2). The provider-answered reasons stay
// beside it.
const EndpointUnbound Reason = "ENDPOINT_UNBOUND"

// Source is where an audit took its endpoint from: the report's
// endpoint_source.
type Source string

// Endpoint sources.
const (
	// CapabilityManifest: a capability manifest file that names the provider
	// and its current signing key id and hashes to the provider record's
	// endpoint_manifest_digest. Only this source allows FAIL.
	CapabilityManifest Source = report.EndpointSourceCapabilityManifest
	// OperatorConfig: an endpoint the operator typed into the configuration.
	OperatorConfig Source = report.EndpointSourceOperatorConfig
)

// Results are the report results.
const (
	Pass       = report.ResultPass
	Fail       = report.ResultFail
	Incomplete = report.ResultIncomplete
)

// Results maps every reason to the result it gives an audit on its own.
// TestEveryReasonHasAResult and TestFailReasonsAreTheRuling check it.
var Results = map[Reason]string{
	SegmentDigestMismatch:  Fail,
	PackageLengthMismatch:  Fail,
	TooLarge:               Fail,
	HTTPStatus:             Fail,
	ConnectFailed:          Incomplete,
	TLSFailed:              Incomplete,
	Timeout:                Incomplete,
	ConnectionReset:        Incomplete,
	ProtocolError:          Incomplete,
	DNSFailure:             Incomplete,
	EndpointNotPermitted:   Incomplete,
	RedirectRefused:        Incomplete,
	EncodingRefused:        Incomplete,
	RangeUnsupported:       Incomplete,
	UnexpectedStatus:       Incomplete,
	PolicyScheme:           Incomplete,
	PolicyPort:             Incomplete,
	PolicyURL:              Incomplete,
	NotRequested:           Incomplete,
	Unclassified:           Incomplete,
	InputManifestInvalid:   Incomplete,
	InputManifestMismatch:  Incomplete,
	InputCapabilityInvalid: Incomplete,
	InputContentWithdrawn:  Incomplete,
	InputContentNotFound:   Incomplete,
	LeaseNotHeld:           Incomplete,
	LeaseNearDeadline:      Incomplete,
	LeaseChanged:           Incomplete,
	ConfigInvalid:          Incomplete,
	ChainReadFailed:        Incomplete,
	RunBudgetExhausted:     Incomplete,
	EndpointUnbound:        Incomplete,
}

// ResultOf is the result one reason gives. An unknown reason is INCOMPLETE.
func ResultOf(r Reason) string {
	if result, ok := Results[r]; ok {
		return result
	}
	return Incomplete
}

// Class is the classification of one fetch outcome.
type Class struct {
	// Result is PASS (OK only), FAIL, or INCOMPLETE.
	Result string
	// Reason is empty for PASS.
	Reason Reason
	// Retry is true for a 4xx or 5xx status that is HTTP_STATUS other than
	// 404 and 410: it is FAIL only if it is still the answer after the
	// configured retries.
	Retry bool
}

// kindReasons maps every fetch outcome kind except OK, HTTPStatus, and
// PolicyRefused, which depend on the status code or the policy rule.
// TestEveryFetchKindIsMapped parses fetch/outcome.go and fails if a kind is
// missing here or from the special cases.
var kindReasons = map[fetch.Kind]Reason{
	fetch.DNSFailure:       DNSFailure,
	fetch.ConnectFailed:    ConnectFailed,
	fetch.TLSFailed:        TLSFailed,
	fetch.Timeout:          Timeout,
	fetch.ConnectionReset:  ConnectionReset,
	fetch.ProtocolError:    ProtocolError,
	fetch.RedirectRefused:  RedirectRefused,
	fetch.EncodingRefused:  EncodingRefused,
	fetch.RangeUnsupported: RangeUnsupported,
	fetch.TooLarge:         TooLarge,
	fetch.LengthMismatch:   PackageLengthMismatch,
}

// ruleReasons maps every policy refusal rule. TestEveryPolicyRuleIsMapped
// parses policy/policy.go and fails if a rule is missing.
var ruleReasons = map[policy.Rule]Reason{
	policy.RuleURLLength:        PolicyURL,
	policy.RuleURLSyntax:        PolicyURL,
	policy.RuleScheme:           PolicyScheme,
	policy.RuleUserinfo:         PolicyURL,
	policy.RuleFragment:         PolicyURL,
	policy.RulePath:             PolicyURL,
	policy.RuleHost:             PolicyURL,
	policy.RulePort:             PolicyPort,
	policy.RuleNoAddress:        EndpointNotPermitted,
	policy.RuleTooManyAddresses: EndpointNotPermitted,
	policy.RuleAddress:          EndpointNotPermitted,
}

// inputReasons maps every verify input state except InputOK.
var inputReasons = map[verify.InputState]Reason{
	verify.InputManifestInvalid:  InputManifestInvalid,
	verify.InputManifestMismatch: InputManifestMismatch,
}

func classOf(r Reason) Class { return Class{Result: ResultOf(r), Reason: r} }

// Outcome classifies the outcome of fetch.Client.Open or of a request
// without a Range header (the request of a WHOLE_PACKAGE audit).
func Outcome(o fetch.Outcome) Class { return outcome(o, false) }

// RangeOutcome classifies the outcome of a request with a Range header
// (every request of a SAMPLED_SEGMENTS audit).
func RangeOutcome(o fetch.Outcome) Class { return outcome(o, true) }

func outcome(o fetch.Outcome, ranged bool) Class {
	switch o.Kind {
	case fetch.OK:
		return Class{Result: Pass}
	case fetch.HTTPStatus:
		return status(o.Status, ranged)
	case fetch.PolicyRefused:
		if o.Refusal == nil {
			return classOf(Unclassified)
		}
		if r, ok := ruleReasons[o.Refusal.Rule]; ok {
			return classOf(r)
		}
		return classOf(Unclassified)
	}
	if r, ok := kindReasons[o.Kind]; ok {
		return classOf(r)
	}
	return classOf(Unclassified)
}

// status classifies an HTTP status the fetch package reported as
// HTTPStatus (any status other than 200, or other than 200 and 206 for a
// range request, outside 3xx). ranged says the request had a Range header.
func status(code int, ranged bool) Class {
	switch {
	case code == http.StatusRequestTimeout:
		return classOf(Timeout)
	case ranged && (code == http.StatusRequestedRangeNotSatisfiable || code == http.StatusNotImplemented):
		return classOf(RangeUnsupported)
	case code == http.StatusNotFound || code == http.StatusGone:
		return classOf(HTTPStatus)
	case code >= 400 && code <= 599:
		return Class{Result: Fail, Reason: HTTPStatus, Retry: true}
	default:
		return classOf(UnexpectedStatus)
	}
}

// Input classifies a verify input state. InputOK is PASS (nothing to
// report); an unknown state is INCOMPLETE.
func Input(state verify.InputState) Class {
	if state == verify.InputOK {
		return Class{Result: Pass}
	}
	if r, ok := inputReasons[state]; ok {
		return classOf(r)
	}
	return classOf(Unclassified)
}

// Summary is the classification of one audit, in report fields.
type Summary struct {
	Result string
	// EndpointSource is the source the audit was classified with; the
	// report records it as endpoint_source.
	EndpointSource Source
	// Reasons are sorted and unique.
	Reasons []string
	// FailedSegments lists the first report.MaxListedFailures segments that
	// did not match, in check order.
	FailedSegments     []report.FailedSegment
	FailedSegmentCount int
	CheckedSegments    int
	// Retry is true when the provider-answered failure of the audit is only
	// a retryable status: the caller retries it while retries remain. It is
	// decided before the endpoint-source rule, so an audit of an
	// operator-configured endpoint is retried the same way and its
	// HTTP_STATUS reason keeps its meaning ("still the answer after the
	// retries"), although its result is INCOMPLETE.
	Retry bool
}

// Reasons builds the summary of an audit that ended before any request,
// with the given reasons (at least one), for an endpoint from source. The
// result is the strongest of the reasons' results, as in Observation, and
// the endpoint-source rule applies the same way.
func Reasons(source Source, reasons ...Reason) Summary {
	s := Summary{}
	set := map[Reason]bool{}
	for _, r := range reasons {
		set[r] = true
	}
	s.finish(set, false, source)
	return s
}

// Observation classifies one audit.
//
//   - an input state other than OK: INCOMPLETE with its reason
//   - each fetch outcome other than OK adds its reason; the outcomes of a
//     SAMPLED_SEGMENTS audit are range requests (RangeOutcome), those of a
//     WHOLE_PACKAGE audit are not (Outcome)
//   - each segment received in full with another digest adds
//     SEGMENT_DIGEST_MISMATCH; a segment not received is listed with the
//     reason of the request that ended reception (whole package) or of the
//     request that stopped the sampled audit, or NOT_REQUESTED for sampled
//     segments after it
//   - the result is FAIL if any reason is FAIL, else INCOMPLETE if any
//     reason exists, else PASS
//   - a FAIL for an endpoint whose source is not CapabilityManifest is
//     INCOMPLETE, with ENDPOINT_UNBOUND added to its reasons
func Observation(obs verify.Observation, source Source) Summary {
	if obs.Input != verify.InputOK {
		c := Input(obs.Input)
		s := Summary{}
		s.finish(map[Reason]bool{c.Reason: true}, false, source)
		return s
	}
	set := map[Reason]bool{}
	retry := true
	var stop Reason
	classOfFetch := Outcome
	if obs.Mode == verify.SampledSegments {
		classOfFetch = RangeOutcome
	}
	for _, out := range obs.Fetches {
		c := classOfFetch(out)
		if c.Result == Pass {
			continue
		}
		set[c.Reason] = true
		if c.Result == Fail && !c.Retry {
			retry = false
		}
		if stop == "" {
			stop = c.Reason
		}
	}
	s := Summary{CheckedSegments: len(obs.Segments)}
	stopped := false
	for _, seg := range obs.Segments {
		var r Reason
		switch seg.State {
		case verify.Match:
			continue
		case verify.DigestMismatch:
			r = SegmentDigestMismatch
			set[r] = true
			retry = false
		case verify.NotReceived:
			r = notReceived(obs.Mode, stop, stopped)
			if obs.Mode == verify.SampledSegments {
				stopped = true
			}
			set[r] = true
		default:
			r = Unclassified
			set[r] = true
		}
		s.FailedSegmentCount++
		if len(s.FailedSegments) < report.MaxListedFailures {
			s.FailedSegments = append(s.FailedSegments, report.FailedSegment{SegmentID: seg.SegmentID, Reason: string(r)})
		}
	}
	s.finish(set, retry, source)
	return s
}

// notReceived is the reason listed for a segment that was not received.
func notReceived(mode verify.Mode, stop Reason, stopped bool) Reason {
	switch {
	case mode == verify.SampledSegments && stopped:
		return NotRequested
	case stop != "":
		return stop
	default:
		// No request failed, yet a segment is missing: never FAIL.
		return Unclassified
	}
}

// finish sets the result and the sorted reasons from set, then applies the
// endpoint-source rule: a FAIL for an endpoint that is not from a bound
// capability manifest is INCOMPLETE ENDPOINT_UNBOUND, its other reasons
// kept.
func (s *Summary) finish(set map[Reason]bool, retry bool, source Source) {
	s.EndpointSource = source
	s.Reasons = s.Reasons[:0]
	result := Pass
	for r := range set {
		if r == NotRequested {
			// A segment-only reason: the request that stopped the audit
			// is already in the set.
			continue
		}
		s.Reasons = append(s.Reasons, string(r))
		switch ResultOf(r) {
		case Fail:
			result = Fail
		case Incomplete:
			if result == Pass {
				result = Incomplete
			}
		}
	}
	sort.Strings(s.Reasons)
	if len(set) != 0 && len(s.Reasons) == 0 {
		// Only NOT_REQUESTED: cannot happen without a stopping request.
		s.Reasons = append(s.Reasons, string(Unclassified))
		result = Incomplete
	}
	s.Retry = result == Fail && retry
	if result == Fail && source != CapabilityManifest {
		if !set[EndpointUnbound] {
			s.Reasons = append(s.Reasons, string(EndpointUnbound))
			sort.Strings(s.Reasons)
		}
		result = Incomplete
	}
	s.Result = result
}

// String is a short description for logs.
func (s Summary) String() string {
	return fmt.Sprintf("%s %v", s.Result, s.Reasons)
}
