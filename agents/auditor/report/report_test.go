package report

import (
	"bytes"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
)

func seed(b byte) *[32]byte {
	var s [32]byte
	for i := range s {
		s[i] = b
	}
	return &s
}

// sampledReport and wholeReport are the golden inputs. Every value is
// synthetic.
func sampledReport() Report {
	return Report{
		AuditorKeyID:           "key-auditor-1",
		AuditorAddress:         "0123456789abcdef0123456789abcdef01234567",
		LeaseID:                "lease-1",
		ContentID:              "c1",
		ContentVersion:         1,
		ProviderID:             "p1",
		EndpointIndex:          0,
		EndpointNameSHA256:     sha256.Sum256([]byte("primary")),
		ExpectedManifestSHA256: sha256.Sum256([]byte("manifest")),
		Mode:                   ModeSampledSegments,
		SampleSeed:             seed(7),
		CheckedSegments:        4,
		FailedSegments:         []FailedSegment{{SegmentID: "s0002", Reason: "SEGMENT_DIGEST_MISMATCH"}},
		FailedSegmentCount:     1,
		BytesRead:              65536,
		StartedUnixSeconds:     1767225600,
		FinishedUnixSeconds:    1767225601,
		Result:                 ResultFail,
		Reasons:                []string{"SEGMENT_DIGEST_MISMATCH"},
		Limitations:            []string{"A sampled check covers only the sampled segments."},
	}
}

func wholeReport() Report {
	r := sampledReport()
	r.Mode, r.SampleSeed = ModeWholePackage, nil
	r.CheckedSegments, r.FailedSegments, r.FailedSegmentCount = 64, nil, 0
	r.BytesRead = 1048576
	r.Result, r.Reasons = ResultPass, nil
	r.Limitations = nil
	return r
}

func TestEncodeIsCanonicalAndRoundTrips(t *testing.T) {
	for _, r := range []Report{sampledReport(), wholeReport()} {
		encoded, err := Encode(r)
		if err != nil {
			t.Fatal(err)
		}
		again, err := jcs.ParseAndCanonicalize(string(encoded))
		if err != nil || again != string(encoded) {
			t.Fatalf("re-canonicalizing changed the bytes: %v", err)
		}
		parsed, err := Parse(encoded)
		if err != nil {
			t.Fatal(err)
		}
		if r.FailedSegments == nil {
			r.FailedSegments = parsed.FailedSegments
		}
		if r.Reasons == nil {
			r.Reasons = []string{}
		}
		if r.Limitations == nil {
			r.Limitations = []string{}
		}
		if !reflect.DeepEqual(parsed, r) {
			t.Fatalf("round trip:\n%+v\n%+v", parsed, r)
		}
	}
}

func TestGoldenReports(t *testing.T) {
	for file, r := range map[string]Report{
		"../testdata/report/sampled-fail.json": sampledReport(),
		"../testdata/report/whole-pass.json":   wholeReport(),
	} {
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		got, err := Encode(r)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, bytes.TrimSuffix(want, []byte("\n"))) {
			t.Fatalf("%s differs:\n%s", file, got)
		}
	}
}

// The worst case stays within 65536 bytes: 64-character ids, 64 listed
// failures whose segment ids escape to 6 bytes per character, 16 reasons of
// 64 characters, and 8 limitations of 512 bytes that escape to 2 bytes each.
func TestWorstCaseFitsTheBound(t *testing.T) {
	long := strings.Repeat("a", 64)
	r := sampledReport()
	r.AuditorKeyID, r.LeaseID, r.ContentID, r.ProviderID = long, long, long, long
	r.AuditorAddress = strings.Repeat("a", 90)
	r.ContentVersion = 9007199254740991
	r.EndpointIndex = MaxEndpoint
	r.CheckedSegments, r.FailedSegmentCount = MaxSegments, MaxSegments
	r.FailedSegments = nil
	reason := "R" + strings.Repeat("A", 63)
	for i := 0; i < MaxListedFailures; i++ {
		id := strings.Repeat("\x01", 63) + string(rune('A'+i%26))
		r.FailedSegments = append(r.FailedSegments, FailedSegment{SegmentID: id, Reason: reason})
	}
	r.Reasons = nil
	for i := 0; i < MaxReasons; i++ {
		r.Reasons = append(r.Reasons, "R"+strings.Repeat("A", 61)+string(rune('A'+i/26))+string(rune('A'+i%26)))
	}
	r.Limitations = nil
	for i := 0; i < MaxLimitations; i++ {
		r.Limitations = append(r.Limitations, strings.Repeat(`"`, MaxLimitationBytes))
	}
	r.BytesRead = 268435457
	r.StartedUnixSeconds, r.FinishedUnixSeconds = 253402300799, 253402300799
	encoded, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if len(encoded) > MaxBytes {
		t.Fatalf("worst case is %d bytes", len(encoded))
	}
	t.Logf("worst-case report: %d bytes of %d", len(encoded), MaxBytes)
}

// patternMatches is docs/evidence/goal-02/check_protocol.py pattern_matches:
// "ip" and "addr" match as underscore-separated tokens ("ip" also inside
// ipv4, ipv6, ipaddress); every other pattern matches as a substring.
func patternMatches(name, pattern string) bool {
	folded, needle := strings.ToLower(name), strings.ToLower(pattern)
	switch needle {
	case "ip":
		return regexp.MustCompile(`(?:^|_)ip(?:_|$)`).MatchString(folded) ||
			strings.Contains(folded, "ipv4") || strings.Contains(folded, "ipv6") || strings.Contains(folded, "ipaddress")
	case "addr":
		return regexp.MustCompile(`(?:^|_)addr(?:_|$)`).MatchString(folded)
	}
	return strings.Contains(folded, needle)
}

func TestFieldNamesAvoidForbiddenPatterns(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/protocol/bounds.json")
	if err != nil {
		t.Fatal(err)
	}
	var bounds struct {
		Patterns []string `json:"forbidden_name_patterns"`
	}
	if err := json.Unmarshal(raw, &bounds); err != nil || len(bounds.Patterns) == 0 {
		t.Fatalf("bounds: %v", err)
	}
	if !patternMatches("ip_address", "ip") || !patternMatches("addr", "addr") || patternMatches("auditor_address", "addr") {
		t.Fatal("pattern port disagrees with check_protocol.py")
	}
	names := append(slices.Clone(Keys), SampledKey, "segment_id", "reason")
	for _, name := range names {
		for _, pattern := range bounds.Patterns {
			if patternMatches(name, pattern) {
				t.Fatalf("field %q matches forbidden pattern %q", name, pattern)
			}
		}
	}
}

// The report has no field for an endpoint name, a URL, an address, an id of
// evidence or a batch, or key bytes: its keys are exactly Keys (plus
// sample_seed), and the only byte arrays are the two digests and the seed.
func TestReportCarriesNoNameURLOrKey(t *testing.T) {
	r := sampledReport()
	r.EndpointNameSHA256 = sha256.Sum256([]byte("primary endpoint name"))
	encoded, err := Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte("primary endpoint name")) {
		t.Fatal("endpoint name copied")
	}
	var o map[string]any
	if err := json.Unmarshal(encoded, &o); err != nil {
		t.Fatal(err)
	}
	var keys []string
	for k := range o {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	want := append(slices.Clone(Keys), SampledKey)
	slices.Sort(want)
	if !slices.Equal(keys, want) {
		t.Fatalf("keys %v", keys)
	}
	typ := reflect.TypeOf(Report{})
	arrays := map[string]bool{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		switch kind := f.Type.Kind(); {
		case kind == reflect.Array, kind == reflect.Pointer:
			arrays[f.Name] = true
		case kind == reflect.Slice && f.Type.Elem().Kind() == reflect.Uint8:
			t.Fatalf("%s is a byte slice", f.Name)
		}
		lower := strings.ToLower(f.Name)
		for _, bad := range []string{"url", "evidence", "batch", "secret", "private", "signature"} {
			if strings.Contains(lower, bad) {
				t.Fatalf("field %s", f.Name)
			}
		}
	}
	if !reflect.DeepEqual(arrays, map[string]bool{"EndpointNameSHA256": true, "ExpectedManifestSHA256": true, "SampleSeed": true}) {
		t.Fatalf("byte arrays %v", arrays)
	}
}

func TestEncodeRejects(t *testing.T) {
	cases := map[string]func(r *Report){
		"bad key id":               func(r *Report) { r.AuditorKeyID = "Key" },
		"bad lease id":             func(r *Report) { r.LeaseID = "" },
		"bad content id":           func(r *Report) { r.ContentID = "c/1" },
		"bad provider id":          func(r *Report) { r.ProviderID = strings.Repeat("p", 65) },
		"bad address":              func(r *Report) { r.AuditorAddress = "Svn1ABC" },
		"content version 0":        func(r *Report) { r.ContentVersion = 0 },
		"content version 2^53":     func(r *Report) { r.ContentVersion = 1 << 53 },
		"endpoint index 8":         func(r *Report) { r.EndpointIndex = 8 },
		"endpoint index -1":        func(r *Report) { r.EndpointIndex = -1 },
		"unknown mode":             func(r *Report) { r.Mode = "MERKLE" },
		"sampled without seed":     func(r *Report) { r.SampleSeed = nil },
		"whole with seed":          func(r *Report) { r.Mode = ModeWholePackage },
		"checked above max":        func(r *Report) { r.CheckedSegments = MaxSegments + 1 },
		"failed list short":        func(r *Report) { r.FailedSegmentCount = 2 },
		"failed list long":         func(r *Report) { r.FailedSegmentCount = 0 },
		"empty segment id":         func(r *Report) { r.FailedSegments[0].SegmentID = "" },
		"bad segment reason":       func(r *Report) { r.FailedSegments[0].Reason = "digest mismatch" },
		"bytes read above max":     func(r *Report) { r.BytesRead = 268435458 },
		"started 0":                func(r *Report) { r.StartedUnixSeconds = 0 },
		"finished before started":  func(r *Report) { r.FinishedUnixSeconds = r.StartedUnixSeconds - 1 },
		"finished above max":       func(r *Report) { r.FinishedUnixSeconds = 253402300800 },
		"unsorted reasons":         func(r *Report) { r.Reasons = []string{"B", "A"} },
		"duplicate reasons":        func(r *Report) { r.Reasons = []string{"A", "A"} },
		"lower-case reason":        func(r *Report) { r.Reasons = []string{"timeout"} },
		"17 reasons":               func(r *Report) { r.Reasons = strings.Split("A B C D E F G H I J K L M N O P Q", " ") },
		"fail without reason":      func(r *Report) { r.Reasons = nil },
		"pass with reason":         func(r *Report) { r.Result = ResultPass },
		"pass with failed segment": func(r *Report) { r.Result, r.Reasons = ResultPass, nil },
		"unknown result":           func(r *Report) { r.Result = "OK" },
		"9 limitations":            func(r *Report) { r.Limitations = make([]string, 9) },
		"empty limitation":         func(r *Report) { r.Limitations = []string{""} },
		"non-ASCII limitation":     func(r *Report) { r.Limitations = []string{"ünverified"} },
		"long limitation":          func(r *Report) { r.Limitations = []string{strings.Repeat("a", 513)} },
	}
	for name, mutate := range cases {
		r := sampledReport()
		r.FailedSegments = slices.Clone(r.FailedSegments)
		mutate(&r)
		_, err := Encode(r)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s: %v", name, err)
	}
}

func TestParseRejects(t *testing.T) {
	good, err := Encode(sampledReport())
	if err != nil {
		t.Fatal(err)
	}
	edit := func(old, new string) []byte {
		if !bytes.Contains(good, []byte(old)) {
			t.Fatalf("golden lacks %q", old)
		}
		return bytes.Replace(good, []byte(old), []byte(new), 1)
	}
	cases := map[string][]byte{
		"not canonical (space)":  append([]byte(" "), good...),
		"not canonical (order)":  edit(`{"auditor_address"`, `{"zzz":1,"auditor_address"`),
		"extra key":              edit(`"auditor_key_id"`, `"auditor_extra":"x","auditor_key_id"`),
		"missing key":            edit(`"claim_limit":"ATTESTATION_NOT_STORAGE_PROOF",`, ``),
		"claim limit changed":    edit(`ATTESTATION_NOT_STORAGE_PROOF`, `STORAGE_PROOF`),
		"format changed":         edit(Format, "svn-audit-report-v1"),
		"bytes_read as number":   edit(`"bytes_read":"65536"`, `"bytes_read":65536`),
		"upper-case digest":      bytes.ToUpper(good),
		"failed segment extra":   edit(`{"reason":"SEGMENT_DIGEST_MISMATCH","segment_id":"s0002"}`, `{"reason":"SEGMENT_DIGEST_MISMATCH","segment_id":"s0002","x":"y"}`),
		"failed segment missing": edit(`{"reason":"SEGMENT_DIGEST_MISMATCH","segment_id":"s0002"}`, `{"a":"b","reason":"SEGMENT_DIGEST_MISMATCH"}`),
		"seed removed":           edit(`"sample_seed":"0707070707070707070707070707070707070707070707070707070707070707",`, ``),
		"oversized":              bytes.Repeat([]byte(" "), MaxBytes+1),
		"null":                   edit(`"mode":"SAMPLED_SEGMENTS"`, `"mode":null`),
	}
	for name, raw := range cases {
		_, err := Parse(raw)
		if !errors.Is(err, ErrInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s: %v", name, err)
	}
}

func TestSHA256(t *testing.T) {
	encoded, err := Encode(wholeReport())
	if err != nil {
		t.Fatal(err)
	}
	if SHA256(encoded) != sha256.Sum256(encoded) {
		t.Fatal("digest")
	}
}
