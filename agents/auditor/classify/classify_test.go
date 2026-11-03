package classify

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"testing"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/verify"
)

// constants returns the values of every constant of the named type declared
// in the Go files matching glob (non-test files only). It reads the source,
// so a constant added to fetch, policy, or verify shows up here without a
// list to update.
func constants(t *testing.T, glob, typeName string) []string {
	t.Helper()
	files, err := filepath.Glob(glob)
	if err != nil || len(files) == 0 {
		t.Fatalf("%s: %v", glob, err)
	}
	var out []string
	fset := token.NewFileSet()
	for _, path := range files {
		if matched, _ := filepath.Match("*_test.go", filepath.Base(path)); matched {
			continue
		}
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range f.Decls {
			gen, ok := decl.(*ast.GenDecl)
			if !ok || gen.Tok != token.CONST {
				continue
			}
			for _, spec := range gen.Specs {
				vs := spec.(*ast.ValueSpec)
				ident, ok := vs.Type.(*ast.Ident)
				if !ok || ident.Name != typeName {
					continue
				}
				for _, v := range vs.Values {
					lit, ok := v.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						t.Fatalf("%s: a %s constant is not a string literal", path, typeName)
					}
					s, _ := strconv.Unquote(lit.Value)
					out = append(out, s)
				}
			}
		}
	}
	if len(out) == 0 {
		t.Fatalf("no %s constants in %s", typeName, glob)
	}
	sort.Strings(out)
	return out
}

// TestEveryFetchKindIsMapped fails when fetch gains an outcome kind that
// this package does not map: every kind except OK, HTTP_STATUS, and
// POLICY_REFUSED is in kindReasons, and those three have their own cases.
func TestEveryFetchKindIsMapped(t *testing.T) {
	kinds := constants(t, "../fetch/*.go", "Kind")
	special := map[fetch.Kind]bool{fetch.OK: true, fetch.HTTPStatus: true, fetch.PolicyRefused: true}
	for _, k := range kinds {
		kind := fetch.Kind(k)
		if special[kind] {
			continue
		}
		if _, ok := kindReasons[kind]; !ok {
			t.Errorf("fetch kind %s is not mapped", k)
		}
		if c := Outcome(fetch.Outcome{Kind: kind}); c.Reason == Unclassified {
			t.Errorf("fetch kind %s classifies as UNCLASSIFIED", k)
		}
	}
	if len(kinds) != len(kindReasons)+len(special) {
		t.Fatalf("%d kinds in fetch, %d mapped + %d special", len(kinds), len(kindReasons), len(special))
	}
	t.Logf("%d fetch kinds mapped: %v", len(kinds), kinds)
}

func TestEveryPolicyRuleIsMapped(t *testing.T) {
	rules := constants(t, "../policy/*.go", "Rule")
	for _, r := range rules {
		if _, ok := ruleReasons[policy.Rule(r)]; !ok {
			t.Errorf("policy rule %s is not mapped", r)
		}
	}
	if len(rules) != len(ruleReasons) {
		t.Fatalf("%d rules, %d mapped", len(rules), len(ruleReasons))
	}
}

func TestEveryVerifyStateIsMapped(t *testing.T) {
	inputs := constants(t, "../verify/*.go", "InputState")
	for _, s := range inputs {
		c := Input(verify.InputState(s))
		if s == string(verify.InputOK) {
			if c.Result != Pass {
				t.Fatalf("InputOK: %+v", c)
			}
			continue
		}
		if c.Reason == Unclassified || c.Result != Incomplete {
			t.Errorf("input state %s: %+v", s, c)
		}
	}
	segments := constants(t, "../verify/*.go", "SegmentState")
	known := map[verify.SegmentState]bool{verify.Match: true, verify.DigestMismatch: true, verify.NotReceived: true}
	for _, s := range segments {
		if !known[verify.SegmentState(s)] {
			t.Errorf("segment state %s is not handled by Observation", s)
		}
	}
	if len(segments) != len(known) {
		t.Fatalf("segment states %v", segments)
	}
}

// TestEveryReasonHasAResult reads every Reason constant of this package:
// each has a result, matches the report reason pattern, and is accepted by
// report.Encode in a report of its result.
func TestEveryReasonHasAResult(t *testing.T) {
	reasons := constants(t, "*.go", "Reason")
	pattern := regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	for _, r := range reasons {
		result, ok := Results[Reason(r)]
		if !ok {
			t.Errorf("reason %s has no result", r)
			continue
		}
		if !pattern.MatchString(r) {
			t.Errorf("reason %s outside the report pattern", r)
		}
		rep := sampleReport(result, []string{r})
		if _, err := report.Encode(rep); err != nil {
			t.Errorf("reason %s: report refused: %v", r, err)
		}
	}
	if len(reasons) != len(Results) {
		t.Fatalf("%d reasons declared, %d with a result", len(reasons), len(Results))
	}
}

// TestFailReasonsAreTheRuling: the FAIL reasons are exactly the four the
// owner's Q3 ruling names; everything else is INCOMPLETE.
func TestFailReasonsAreTheRuling(t *testing.T) {
	var fail []string
	for r, result := range Results {
		switch result {
		case Fail:
			fail = append(fail, string(r))
		case Incomplete:
		default:
			t.Fatalf("reason %s has result %s", r, result)
		}
	}
	sort.Strings(fail)
	want := []string{"HTTP_STATUS", "PACKAGE_LENGTH_MISMATCH", "SEGMENT_DIGEST_MISMATCH", "TOO_LARGE"}
	if !reflect.DeepEqual(fail, want) {
		t.Fatalf("FAIL reasons %v, want %v", fail, want)
	}
	if ResultOf("NOT_A_REASON") != Incomplete {
		t.Fatal("an unknown reason is not INCOMPLETE")
	}
}

func sampleReport(result string, reasons []string) report.Report {
	r := report.Report{
		AuditorKeyID: "key-auditor-1", AuditorAddress: "79b70b8d1f8f5606e9a823195b38538bbeb9f162",
		LeaseID: "l1", ContentID: "c1", ContentVersion: 1, ProviderID: "p1", Mode: report.ModeWholePackage,
		StartedUnixSeconds: 1767225600, FinishedUnixSeconds: 1767225601, Result: result, Reasons: reasons,
	}
	if result == Pass {
		r.Reasons = nil
	}
	return r
}

// TestOutcomeTable is the exhaustive outcome table of the Q3 ruling
// ("Provider-answered only").
func TestOutcomeTable(t *testing.T) {
	refusal := func(rule policy.Rule) fetch.Outcome {
		return fetch.Outcome{Kind: fetch.PolicyRefused, Refusal: &policy.Refusal{Rule: rule}}
	}
	status := func(code int) fetch.Outcome { return fetch.Outcome{Kind: fetch.HTTPStatus, Status: code} }
	cases := []struct {
		name   string
		out    fetch.Outcome
		result string
		reason Reason
		retry  bool
	}{
		{"ok", fetch.Outcome{Kind: fetch.OK, Status: 200}, Pass, "", false},
		// FAIL: the provider's verified server answered with wrong or missing bytes.
		{"length mismatch under authenticated framing", fetch.Outcome{Kind: fetch.LengthMismatch, Status: 200}, Fail, PackageLengthMismatch, false},
		{"too many bytes", fetch.Outcome{Kind: fetch.TooLarge, Status: 200}, Fail, TooLarge, false},
		{"404", status(404), Fail, HTTPStatus, false},
		{"410", status(410), Fail, HTTPStatus, false},
		{"400 after retries", status(400), Fail, HTTPStatus, true},
		{"403 after retries", status(403), Fail, HTTPStatus, true},
		{"429 after retries", status(429), Fail, HTTPStatus, true},
		{"499 after retries", status(499), Fail, HTTPStatus, true},
		{"500 after retries", status(500), Fail, HTTPStatus, true},
		{"503 after retries", status(503), Fail, HTTPStatus, true},
		{"599 after retries", status(599), Fail, HTTPStatus, true},
		{"416 to a whole-package request after retries", status(416), Fail, HTTPStatus, true},
		{"501 to a whole-package request after retries", status(501), Fail, HTTPStatus, true},
		// INCOMPLETE: everything else.
		{"408 is the server's request timeout", status(408), Incomplete, Timeout, false},
		{"204", status(204), Incomplete, UnexpectedStatus, false},
		{"201", status(201), Incomplete, UnexpectedStatus, false},
		{"600", status(600), Incomplete, UnexpectedStatus, false},
		{"status 0", status(0), Incomplete, UnexpectedStatus, false},
		{"connect", fetch.Outcome{Kind: fetch.ConnectFailed}, Incomplete, ConnectFailed, false},
		{"tls", fetch.Outcome{Kind: fetch.TLSFailed}, Incomplete, TLSFailed, false},
		{"timeout", fetch.Outcome{Kind: fetch.Timeout}, Incomplete, Timeout, false},
		{"reset", fetch.Outcome{Kind: fetch.ConnectionReset, Status: 200}, Incomplete, ConnectionReset, false},
		{"protocol", fetch.Outcome{Kind: fetch.ProtocolError}, Incomplete, ProtocolError, false},
		{"dns", fetch.Outcome{Kind: fetch.DNSFailure}, Incomplete, DNSFailure, false},
		{"dns nxdomain", fetch.Outcome{Kind: fetch.DNSFailure, NotFound: true}, Incomplete, DNSFailure, false},
		{"redirect", fetch.Outcome{Kind: fetch.RedirectRefused, Status: 302}, Incomplete, RedirectRefused, false},
		{"compression", fetch.Outcome{Kind: fetch.EncodingRefused, Status: 200}, Incomplete, EncodingRefused, false},
		{"range ignored or wrong content-range total", fetch.Outcome{Kind: fetch.RangeUnsupported, Status: 206}, Incomplete, RangeUnsupported, false},
		{"policy scheme", refusal(policy.RuleScheme), Incomplete, PolicyScheme, false},
		{"policy port", refusal(policy.RulePort), Incomplete, PolicyPort, false},
		{"policy address", refusal(policy.RuleAddress), Incomplete, EndpointNotPermitted, false},
		{"policy no address", refusal(policy.RuleNoAddress), Incomplete, EndpointNotPermitted, false},
		{"policy too many addresses", refusal(policy.RuleTooManyAddresses), Incomplete, EndpointNotPermitted, false},
		{"policy url length", refusal(policy.RuleURLLength), Incomplete, PolicyURL, false},
		{"policy url syntax", refusal(policy.RuleURLSyntax), Incomplete, PolicyURL, false},
		{"policy userinfo", refusal(policy.RuleUserinfo), Incomplete, PolicyURL, false},
		{"policy fragment", refusal(policy.RuleFragment), Incomplete, PolicyURL, false},
		{"policy path", refusal(policy.RulePath), Incomplete, PolicyURL, false},
		{"policy host", refusal(policy.RuleHost), Incomplete, PolicyURL, false},
		{"policy unknown rule", refusal("NEW_RULE"), Incomplete, Unclassified, false},
		{"policy without refusal", fetch.Outcome{Kind: fetch.PolicyRefused}, Incomplete, Unclassified, false},
		{"unknown kind", fetch.Outcome{Kind: "NEW_KIND"}, Incomplete, Unclassified, false},
	}
	for _, c := range cases {
		got := Outcome(c.out)
		if got.Result != c.result || got.Reason != c.reason || got.Retry != c.retry {
			t.Errorf("%s: %+v, want %s %s retry=%v", c.name, got, c.result, c.reason, c.retry)
		}
		// A range request is classified the same way, except for the
		// statuses of TestRangeOutcomeTable.
		if c.out.Kind == fetch.HTTPStatus && (c.out.Status == 416 || c.out.Status == 501) {
			continue
		}
		if ranged := RangeOutcome(c.out); ranged != got {
			t.Errorf("%s: range request %+v, request without a range %+v", c.name, ranged, got)
		}
	}
}

// TestRangeOutcomeTable: a range request answered 416 or 501 is
// RANGE_UNSUPPORTED (INCOMPLETE), never FAIL: the provider refused the
// goal-09 range extension, which says nothing about the bytes it holds.
// Other statuses keep their whole-package class.
func TestRangeOutcomeTable(t *testing.T) {
	status := func(code int) fetch.Outcome { return fetch.Outcome{Kind: fetch.HTTPStatus, Status: code} }
	cases := []struct {
		code   int
		result string
		reason Reason
		retry  bool
	}{
		{416, Incomplete, RangeUnsupported, false},
		{501, Incomplete, RangeUnsupported, false},
		{408, Incomplete, Timeout, false},
		{404, Fail, HTTPStatus, false},
		{410, Fail, HTTPStatus, false},
		{400, Fail, HTTPStatus, true},
		{415, Fail, HTTPStatus, true},
		{417, Fail, HTTPStatus, true},
		{500, Fail, HTTPStatus, true},
		{502, Fail, HTTPStatus, true},
		{204, Incomplete, UnexpectedStatus, false},
	}
	for _, c := range cases {
		got := RangeOutcome(status(c.code))
		if got.Result != c.result || got.Reason != c.reason || got.Retry != c.retry {
			t.Errorf("range request answered %d: %+v, want %s %s retry=%v", c.code, got, c.result, c.reason, c.retry)
		}
	}
}

func seg(id string, s verify.SegmentState) verify.SegmentResult {
	return verify.SegmentResult{SegmentID: id, State: s}
}

func TestObservation(t *testing.T) {
	ok := fetch.Outcome{Kind: fetch.OK, Status: 200}
	cases := []struct {
		name    string
		obs     verify.Observation
		result  string
		reasons []string
		failed  []report.FailedSegment
		retry   bool
	}{
		{
			name:   "whole package, every segment matches",
			obs:    verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{ok}, Segments: []verify.SegmentResult{seg("s0", verify.Match), seg("s1", verify.Match)}},
			result: Pass, reasons: []string{},
		},
		{
			name:    "manifest invalid",
			obs:     verify.Observation{Input: verify.InputManifestInvalid, Mode: verify.WholePackage},
			result:  Incomplete,
			reasons: []string{"INPUT_MANIFEST_INVALID"},
		},
		{
			name:    "manifest does not match the lease",
			obs:     verify.Observation{Input: verify.InputManifestMismatch, Mode: verify.SampledSegments},
			result:  Incomplete,
			reasons: []string{"INPUT_MANIFEST_MISMATCH"},
		},
		{
			name:    "one corrupted segment",
			obs:     verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{ok}, Segments: []verify.SegmentResult{seg("s0", verify.Match), seg("s1", verify.DigestMismatch)}},
			result:  Fail,
			reasons: []string{"SEGMENT_DIGEST_MISMATCH"},
			failed:  []report.FailedSegment{{SegmentID: "s1", Reason: "SEGMENT_DIGEST_MISMATCH"}},
		},
		{
			name: "reset after matching segments",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{{Kind: fetch.ConnectionReset, Status: 200}},
				Segments: []verify.SegmentResult{seg("s0", verify.Match), seg("s1", verify.NotReceived)}},
			result:  Incomplete,
			reasons: []string{"CONNECTION_RESET"},
			failed:  []report.FailedSegment{{SegmentID: "s1", Reason: "CONNECTION_RESET"}},
		},
		{
			name: "a wrong segment received in full stays FAIL after a later reset",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{{Kind: fetch.ConnectionReset, Status: 200}},
				Segments: []verify.SegmentResult{seg("s0", verify.DigestMismatch), seg("s1", verify.NotReceived)}},
			result:  Fail,
			reasons: []string{"CONNECTION_RESET", "SEGMENT_DIGEST_MISMATCH"},
			failed:  []report.FailedSegment{{SegmentID: "s0", Reason: "SEGMENT_DIGEST_MISMATCH"}, {SegmentID: "s1", Reason: "CONNECTION_RESET"}},
		},
		{
			name: "declared short length",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{{Kind: fetch.LengthMismatch, Status: 200}},
				Segments: []verify.SegmentResult{seg("s0", verify.NotReceived)}},
			result:  Fail,
			reasons: []string{"PACKAGE_LENGTH_MISMATCH"},
			failed:  []report.FailedSegment{{SegmentID: "s0", Reason: "PACKAGE_LENGTH_MISMATCH"}},
		},
		{
			name: "500 is retried",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{{Kind: fetch.HTTPStatus, Status: 500}},
				Segments: []verify.SegmentResult{seg("s0", verify.NotReceived)}},
			result:  Fail,
			reasons: []string{"HTTP_STATUS"},
			failed:  []report.FailedSegment{{SegmentID: "s0", Reason: "HTTP_STATUS"}},
			retry:   true,
		},
		{
			name: "404 is not retried",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{{Kind: fetch.HTTPStatus, Status: 404}},
				Segments: []verify.SegmentResult{seg("s0", verify.NotReceived)}},
			result:  Fail,
			reasons: []string{"HTTP_STATUS"},
			failed:  []report.FailedSegment{{SegmentID: "s0", Reason: "HTTP_STATUS"}},
		},
		{
			name: "sampled audit stopped by an ignored range",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.SampledSegments, Fetches: []fetch.Outcome{ok, {Kind: fetch.RangeUnsupported, Status: 200}},
				Segments: []verify.SegmentResult{seg("s3", verify.Match), seg("s1", verify.NotReceived), seg("s0", verify.NotReceived)}},
			result:  Incomplete,
			reasons: []string{"RANGE_UNSUPPORTED"},
			failed:  []report.FailedSegment{{SegmentID: "s1", Reason: "RANGE_UNSUPPORTED"}, {SegmentID: "s0", Reason: "NOT_REQUESTED"}},
		},
		{
			name: "sampled audit stopped by a 416 to its range request",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.SampledSegments, Fetches: []fetch.Outcome{ok, {Kind: fetch.HTTPStatus, Status: 416}},
				Segments: []verify.SegmentResult{seg("s3", verify.Match), seg("s1", verify.NotReceived), seg("s0", verify.NotReceived)}},
			result:  Incomplete,
			reasons: []string{"RANGE_UNSUPPORTED"},
			failed:  []report.FailedSegment{{SegmentID: "s1", Reason: "RANGE_UNSUPPORTED"}, {SegmentID: "s0", Reason: "NOT_REQUESTED"}},
		},
		{
			name: "sampled audit stopped by a 501 to its range request",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.SampledSegments, Fetches: []fetch.Outcome{{Kind: fetch.HTTPStatus, Status: 501}},
				Segments: []verify.SegmentResult{seg("s1", verify.NotReceived)}},
			result:  Incomplete,
			reasons: []string{"RANGE_UNSUPPORTED"},
			failed:  []report.FailedSegment{{SegmentID: "s1", Reason: "RANGE_UNSUPPORTED"}},
		},
		{
			name: "whole-package request answered 416 is retried",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{{Kind: fetch.HTTPStatus, Status: 416}},
				Segments: []verify.SegmentResult{seg("s0", verify.NotReceived)}},
			result:  Fail,
			reasons: []string{"HTTP_STATUS"},
			failed:  []report.FailedSegment{{SegmentID: "s0", Reason: "HTTP_STATUS"}},
			retry:   true,
		},
		{
			name: "408 stops a sampled audit as a timeout",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.SampledSegments, Fetches: []fetch.Outcome{{Kind: fetch.HTTPStatus, Status: 408}},
				Segments: []verify.SegmentResult{seg("s1", verify.NotReceived)}},
			result:  Incomplete,
			reasons: []string{"TIMEOUT"},
			failed:  []report.FailedSegment{{SegmentID: "s1", Reason: "TIMEOUT"}},
		},
		{
			name: "sampled audit stopped by too many bytes",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.SampledSegments, Fetches: []fetch.Outcome{{Kind: fetch.TooLarge, Status: 206}},
				Segments: []verify.SegmentResult{seg("s2", verify.NotReceived), seg("s4", verify.NotReceived)}},
			result:  Fail,
			reasons: []string{"TOO_LARGE"},
			failed:  []report.FailedSegment{{SegmentID: "s2", Reason: "TOO_LARGE"}, {SegmentID: "s4", Reason: "NOT_REQUESTED"}},
		},
		{
			name: "a segment missing with no failed request is never FAIL",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{ok},
				Segments: []verify.SegmentResult{seg("s0", verify.NotReceived)}},
			result:  Incomplete,
			reasons: []string{"UNCLASSIFIED"},
			failed:  []report.FailedSegment{{SegmentID: "s0", Reason: "UNCLASSIFIED"}},
		},
		{
			name: "retryable status with a digest mismatch is not retried",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.SampledSegments, Fetches: []fetch.Outcome{ok, {Kind: fetch.HTTPStatus, Status: 503}},
				Segments: []verify.SegmentResult{seg("s0", verify.DigestMismatch), seg("s1", verify.NotReceived)}},
			result:  Fail,
			reasons: []string{"HTTP_STATUS", "SEGMENT_DIGEST_MISMATCH"},
			failed:  []report.FailedSegment{{SegmentID: "s0", Reason: "SEGMENT_DIGEST_MISMATCH"}, {SegmentID: "s1", Reason: "HTTP_STATUS"}},
		},
		{
			name: "unknown segment state",
			obs: verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{ok},
				Segments: []verify.SegmentResult{seg("s0", "NEW_STATE")}},
			result:  Incomplete,
			reasons: []string{"UNCLASSIFIED"},
			failed:  []report.FailedSegment{{SegmentID: "s0", Reason: "UNCLASSIFIED"}},
		},
	}
	for _, c := range cases {
		got := Observation(c.obs)
		reasons := append([]string{}, got.Reasons...)
		if got.Result != c.result || !reflect.DeepEqual(reasons, c.reasons) || got.Retry != c.retry ||
			!reflect.DeepEqual(got.FailedSegments, c.failed) || got.FailedSegmentCount != len(c.failed) || got.CheckedSegments != len(c.obs.Segments) {
			t.Errorf("%s: %+v", c.name, got)
		}
		// Every summary makes a valid report.
		r := sampleReport(got.Result, got.Reasons)
		r.FailedSegments, r.FailedSegmentCount, r.CheckedSegments = got.FailedSegments, got.FailedSegmentCount, got.CheckedSegments
		if c.obs.Mode == verify.SampledSegments {
			r.Mode = report.ModeSampledSegments
			r.SampleSeed = &[32]byte{1}
		}
		if _, err := report.Encode(r); err != nil {
			t.Errorf("%s: report refused: %v", c.name, err)
		}
	}
}

// The failed-segment list keeps the first 64; the count keeps them all.
func TestFailedSegmentsAreBounded(t *testing.T) {
	obs := verify.Observation{Input: verify.InputOK, Mode: verify.WholePackage, Fetches: []fetch.Outcome{{Kind: fetch.OK, Status: 200}}}
	for i := 0; i < 100; i++ {
		obs.Segments = append(obs.Segments, seg(fmt.Sprintf("s%03d", i), verify.DigestMismatch))
	}
	got := Observation(obs)
	if got.FailedSegmentCount != 100 || len(got.FailedSegments) != report.MaxListedFailures || got.FailedSegments[63].SegmentID != "s063" {
		t.Fatalf("%d listed, count %d", len(got.FailedSegments), got.FailedSegmentCount)
	}
}

func TestReasons(t *testing.T) {
	s := Reasons(InputContentWithdrawn)
	if s.Result != Incomplete || !reflect.DeepEqual(s.Reasons, []string{"INPUT_CONTENT_WITHDRAWN"}) || s.Retry {
		t.Fatalf("%+v", s)
	}
	s = Reasons(LeaseNearDeadline, ChainReadFailed, LeaseNearDeadline)
	if s.Result != Incomplete || !reflect.DeepEqual(s.Reasons, []string{"CHAIN_READ_FAILED", "LEASE_NEAR_DEADLINE"}) {
		t.Fatalf("%+v", s)
	}
}
