package verify

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch/fetchtest"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
)

const (
	testContent = "c1"
	testVersion = 1
)

// manifestJSON builds an svn-media-manifest-v1 for the given renditions.
func manifestJSON(t testing.TB, contentID string, renditions ...[]fetchtest.Segment) []byte {
	t.Helper()
	var total uint64
	var list []any
	for i, segments := range renditions {
		var items []any
		for _, s := range segments {
			items = append(items, map[string]any{
				"segment_id": s.ID,
				"sha256":     hex.EncodeToString(s.SHA256[:]),
				"byte_count": fmt.Sprint(len(s.Bytes)),
			})
			total += uint64(len(s.Bytes))
		}
		list = append(list, map[string]any{"rendition_id": fmt.Sprintf("r%d", i), "codec": "avc1", "segments": items})
	}
	text, err := jcs.Canonicalize(map[string]any{
		"schema_version": int64(1), "profile": "SVN-JCS-1", "manifest_id": "m1",
		"content_id": contentID, "content_version": int64(testVersion),
		"package_bytes": fmt.Sprint(total), "renditions": list,
	})
	if err != nil {
		t.Fatal(err)
	}
	return []byte(text)
}

func leaseFor(t testing.TB, manifest []byte) Lease {
	t.Helper()
	m, err := ParseManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	return Lease{ContentID: m.ContentID, ContentVersion: m.ContentVersion, ManifestSHA256: m.SHA256, PackageBytes: m.PackageBytes}
}

type endpoint struct {
	srv     *fetchtest.Server
	session *fetch.Session
}

// serve starts a loopback endpoint for pkg and opens an allowlisted
// session to it through the real fetch path.
func serve(t *testing.T, pkg []byte, b fetchtest.Behavior) endpoint {
	t.Helper()
	ca := fetchtest.NewCA(t)
	srv := fetchtest.Start(t, fetchtest.Config{CA: ca, ContentID: testContent, ContentVersion: testVersion, Package: pkg, Behavior: b})
	pol, err := policy.New([]policy.Entry{{ProviderID: "p1", Host: "127.0.0.1", IP: srv.Addr.Addr(), Port: srv.Addr.Port()}})
	if err != nil {
		t.Fatal(err)
	}
	c, err := fetch.New(fetch.Options{Policy: pol, RootCAs: ca.Pool, Limits: fetch.Limits{
		Connect: 2 * time.Second, TLSHandshake: 2 * time.Second, ResponseHeader: 2 * time.Second,
		IdleRead: 2 * time.Second, Object: 10 * time.Second,
	}})
	if err != nil {
		t.Fatal(err)
	}
	s, out := c.Open(context.Background(), "p1", srv.URL)
	if out.Kind != fetch.OK {
		t.Fatalf("open: %+v", out)
	}
	t.Cleanup(s.Close)
	t.Logf("%s at %s", fetchtest.Label, srv.URL)
	return endpoint{srv: srv, session: s}
}

func audit(t *testing.T, e endpoint, req Request) Observation {
	t.Helper()
	obs, err := Audit(context.Background(), e.session, req)
	if err != nil {
		t.Fatal(err)
	}
	return obs
}

func states(results []SegmentResult) map[SegmentState]int {
	out := map[SegmentState]int{}
	for _, r := range results {
		out[r.State]++
	}
	return out
}

func TestParseManifestValidFixture(t *testing.T) {
	raw, err := os.ReadFile("../../../docs/protocol/fixtures/manifest-valid.json")
	if err != nil {
		t.Fatal(err)
	}
	m, err := ParseManifest(raw)
	if err != nil {
		t.Fatal(err)
	}
	if m.ContentID != "c1" || m.PackageBytes != 4 || len(m.Renditions) != 1 || m.SHA256 != sha256.Sum256([]byte(m.Canonical)) {
		t.Fatalf("%+v", m)
	}
}

func TestParseManifestRejectsGoal02InvalidFixtures(t *testing.T) {
	for _, name := range []string{
		"manifest-invalid-boundary.json", "manifest-invalid-unknown.json",
		"manifest-invalid-wrong-domain.json", "manifest-invalid-duplicate-id.json",
	} {
		raw, err := os.ReadFile(filepath.Join("../../../docs/protocol/fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		if _, err := ParseManifest(raw); !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}

func TestParseManifestStrict(t *testing.T) {
	valid := `{"schema_version":1,"profile":"SVN-JCS-1","manifest_id":"m1","content_id":"c1","content_version":1,"package_bytes":"4","renditions":[{"rendition_id":"r1","codec":"avc1","segments":[{"segment_id":"s1","sha256":"` + strings.Repeat("a", 64) + `","byte_count":"4"}]}]}`
	if _, err := ParseManifest([]byte(valid)); err != nil {
		t.Fatalf("valid: %v", err)
	}
	seg := func(id, digest, count string) string {
		return `{"segment_id":` + id + `,"sha256":` + digest + `,"byte_count":` + count + `}`
	}
	good := `"` + strings.Repeat("a", 64) + `"`
	with := func(old, new string) string {
		if !strings.Contains(valid, old) {
			t.Fatalf("fixture lacks %q", old)
		}
		return strings.Replace(valid, old, new, 1)
	}
	many := func(n int) string {
		var parts []string
		for i := 0; i < n; i++ {
			parts = append(parts, seg(fmt.Sprintf(`"s%d"`, i), good, `"0"`))
		}
		return strings.Join(parts, ",")
	}
	rendition := `{"rendition_id":"r1","codec":"avc1","segments":[` + seg(`"s1"`, good, `"4"`) + `]}`
	cases := map[string]string{
		"unknown top field":       with(`"manifest_id":"m1",`, `"manifest_id":"m1","extra":1,`),
		"unknown rendition field": with(`"codec":"avc1",`, `"codec":"avc1","x":"y",`),
		"unknown segment field":   with(`"byte_count":"4"}`, `"byte_count":"4","merkle_root":"x"}`),
		"case variant key":        with(`"schema_version"`, `"Schema_version"`),
		"duplicate key":           with(`"manifest_id":"m1",`, `"manifest_id":"m1","manifest_id":"m2",`),
		"missing field":           with(`"manifest_id":"m1",`, ``),
		"null":                    with(`"codec":"avc1"`, `"codec":null`),
		"fraction":                with(`"content_version":1,`, `"content_version":1.0,`),
		"schema_version 2":        with(`"schema_version":1`, `"schema_version":2`),
		"schema_version string":   with(`"schema_version":1`, `"schema_version":"1"`),
		"profile":                 with(`"profile":"SVN-JCS-1"`, `"profile":"JCS"`),
		"content_version 0":       with(`"content_version":1,`, `"content_version":0,`),
		"content_version 2^53":    with(`"content_version":1,`, `"content_version":9007199254740992,`),
		"content_id pattern":      with(`"content_id":"c1"`, `"content_id":"C1"`),
		"manifest_id pattern":     with(`"manifest_id":"m1"`, `"manifest_id":"-m"`),
		"package_bytes leading 0": with(`"package_bytes":"4"`, `"package_bytes":"04"`),
		"package_bytes negative":  with(`"package_bytes":"4"`, `"package_bytes":"-4"`),
		"package_bytes number":    with(`"package_bytes":"4"`, `"package_bytes":4`),
		"package_bytes overflow":  with(`"package_bytes":"4"`, `"package_bytes":"18446744073709551616"`),
		"package_bytes 21 chars":  with(`"package_bytes":"4"`, `"package_bytes":"`+strings.Repeat("1", 21)+`"`),
		"no renditions":           with(`"renditions":[`+rendition+`]`, `"renditions":[]`),
		"nine renditions":         with(`"renditions":[`+rendition+`]`, `"renditions":[`+strings.Repeat(rendition+",", 8)+rendition+`]`),
		"no segments":             with(`"segments":[`+seg(`"s1"`, good, `"4"`)+`]`, `"segments":[]`),
		"4097 segments":           with(seg(`"s1"`, good, `"4"`), many(4097)),
		"empty segment id":        with(`"segment_id":"s1"`, `"segment_id":""`),
		"65-char segment id":      with(`"segment_id":"s1"`, `"segment_id":"`+strings.Repeat("s", 65)+`"`),
		"upper-case digest":       with(good, `"`+strings.Repeat("A", 64)+`"`),
		"short digest":            with(good, `"`+strings.Repeat("a", 63)+`"`),
		"byte_count leading 0":    with(`"byte_count":"4"`, `"byte_count":"04"`),
		"33-char codec":           with(`"codec":"avc1"`, `"codec":"`+strings.Repeat("c", 33)+`"`),
		"65-char rendition id":    with(`"rendition_id":"r1"`, `"rendition_id":"`+strings.Repeat("r", 65)+`"`),
		"duplicate segment id":    with(`"renditions":[`+rendition+`]`, `"renditions":[`+rendition+`,`+rendition+`]`),
		"trailing data":           valid + "x",
		"top-level array":         `[` + valid + `]`,
	}
	for name, raw := range cases {
		_, err := ParseManifest([]byte(raw))
		if !errors.Is(err, ErrManifestInvalid) {
			t.Fatalf("%s: %v", name, err)
		}
		t.Logf("%s: %v", name, err)
	}
	if _, err := ParseManifest(bytes.Repeat([]byte(" "), MaxManifestBytes+1)); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("oversized input: %v", err)
	}
}

func TestManifestDigestIsCanonical(t *testing.T) {
	a := `{"schema_version":1,"profile":"SVN-JCS-1","manifest_id":"m1","content_id":"c1","content_version":1,"package_bytes":"4","renditions":[{"rendition_id":"r1","codec":"avc1","segments":[{"segment_id":"s1","sha256":"` + strings.Repeat("b", 64) + `","byte_count":"4"}]}]}`
	b := "{\n  \"renditions\": [{\"segments\": [{\"byte_count\": \"4\", \"sha256\": \"" + strings.Repeat("b", 64) + "\", \"segment_id\": \"s1\"}], \"codec\": \"avc1\", \"rendition_id\": \"r1\"}],\n  \"package_bytes\": \"4\", \"content_version\": 1, \"content_id\": \"c1\", \"manifest_id\": \"m1\", \"profile\": \"SVN-JCS-1\", \"schema_version\": 1\n}\n"
	ma, err := ParseManifest([]byte(a))
	if err != nil {
		t.Fatal(err)
	}
	mb, err := ParseManifest([]byte(b))
	if err != nil {
		t.Fatal(err)
	}
	if ma.SHA256 != mb.SHA256 || ma.Canonical != mb.Canonical {
		t.Fatal("digest depends on whitespace or key order")
	}
}

func TestReadManifestFile(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "m.json")
	if err := os.WriteFile(good, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if raw, err := ReadManifestFile(good); err != nil || string(raw) != "{}" {
		t.Fatalf("%q %v", raw, err)
	}
	big := filepath.Join(dir, "big.json")
	if err := os.WriteFile(big, bytes.Repeat([]byte(" "), MaxManifestBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadManifestFile(big); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("oversized: %v", err)
	}
	if _, err := ReadManifestFile(dir); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("directory: %v", err)
	}
	if _, err := ReadManifestFile(filepath.Join(dir, "missing")); err == nil {
		t.Fatal("missing file accepted")
	}
}

func TestNewLayout(t *testing.T) {
	segments, _ := fetchtest.Synthetic("layout", []int{3, 0, 5})
	m, err := ParseManifest(manifestJSON(t, testContent, segments))
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewLayout(m)
	if err != nil {
		t.Fatal(err)
	}
	want := []Span{{"s0000", 0, 3, segments[0].SHA256}, {"s0001", 3, 0, segments[1].SHA256}, {"s0002", 3, 5, segments[2].SHA256}}
	if l.PackageBytes != 8 || fmt.Sprint(l.Spans) != fmt.Sprint(want) {
		t.Fatalf("%+v", l)
	}
	m.PackageBytes = 9
	if _, err := NewLayout(m); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("sum mismatch: %v", err)
	}
	zero := &Manifest{Renditions: []Rendition{{Segments: []Segment{{ID: "a"}}}}}
	if _, err := NewLayout(zero); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("zero package: %v", err)
	}
	huge := &Manifest{PackageBytes: MaxPackageBytes + 1, Renditions: []Rendition{{Segments: []Segment{{ID: "a", ByteCount: MaxPackageBytes}, {ID: "b", ByteCount: 1}}}}}
	if _, err := NewLayout(huge); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("above the package cap: %v", err)
	}
	// A zero-length segment must carry the SHA-256 of empty input; any other
	// digest could never match and would be judged without provider bytes.
	emptyWrong := &Manifest{PackageBytes: 4, Renditions: []Rendition{{Segments: []Segment{
		{ID: "a", ByteCount: 0, SHA256: sha256.Sum256([]byte("x"))}, {ID: "b", ByteCount: 4},
	}}}}
	if _, err := NewLayout(emptyWrong); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("zero-length segment with a non-empty digest: %v", err)
	}
	emptyWrong.Renditions[0].Segments[0].SHA256 = sha256.Sum256(nil)
	if _, err := NewLayout(emptyWrong); err != nil {
		t.Fatalf("zero-length segment with the empty digest: %v", err)
	}
}

func TestSplitterZeroLengthSegments(t *testing.T) {
	segments, pkg := fetchtest.Synthetic("zero", []int{0, 7, 0, 0, 9, 0})
	m, err := ParseManifest(manifestJSON(t, testContent, segments))
	if err != nil {
		t.Fatal(err)
	}
	l, err := NewLayout(m)
	if err != nil {
		t.Fatal(err)
	}
	s := newSplitter(l.Spans)
	for _, b := range pkg {
		if _, err := s.Write([]byte{b}); err != nil {
			t.Fatal(err)
		}
	}
	if got := states(s.close()); got[Match] != 6 {
		t.Fatalf("%v", got)
	}
	s = newSplitter(l.Spans)
	if _, err := s.Write(append(append([]byte(nil), pkg...), 'x')); !errors.Is(err, errPastPackage) {
		t.Fatalf("bytes past the package: %v", err)
	}
}

func fixedSeed(b byte) [SeedBytes]byte {
	var s [SeedBytes]byte
	for i := range s {
		s[i] = b
	}
	return s
}

func TestSelectIsDeterministicPerSeed(t *testing.T) {
	sizes := make([]int, 64)
	for i := range sizes {
		sizes[i] = 16
	}
	segments, _ := fetchtest.Synthetic("select", sizes)
	m, err := ParseManifest(manifestJSON(t, testContent, segments))
	if err != nil {
		t.Fatal(err)
	}
	l, _ := NewLayout(m)
	a1, _ := Select(l, fixedSeed(1), 4)
	a2, _ := Select(l, fixedSeed(1), 4)
	b, _ := Select(l, fixedSeed(2), 4)
	if fmt.Sprint(a1) != fmt.Sprint(a2) {
		t.Fatal("same seed, different selection")
	}
	if fmt.Sprint(a1) == fmt.Sprint(b) {
		t.Fatal("different seeds, same selection")
	}
	seed := fixedSeed(1)
	for _, span := range a1 {
		key := sha256.Sum256(append(seed[:], span.SegmentID...))
		for _, other := range l.Spans {
			if contains(a1, other.SegmentID) {
				continue
			}
			if k := sha256.Sum256(append(seed[:], other.SegmentID...)); string(k[:]) < string(key[:]) {
				t.Fatalf("%s ranks before selected %s", other.SegmentID, span.SegmentID)
			}
		}
	}
	for _, k := range []int{0, -1, 65} {
		if _, err := Select(l, seed, k); err == nil {
			t.Fatalf("k=%d accepted", k)
		}
	}
	if all, err := Select(l, seed, 64); err != nil || len(all) != 64 {
		t.Fatalf("k=64: %v", err)
	}
}

func contains(spans []Span, id string) bool {
	for _, s := range spans {
		if s.SegmentID == id {
			return true
		}
	}
	return false
}

func TestNewSeed(t *testing.T) {
	a, err := NewSeed(nil)
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewSeed(nil)
	if err != nil {
		t.Fatal(err)
	}
	if a == b {
		t.Fatal("two fresh seeds are equal")
	}
	if _, err := NewSeed(bytes.NewReader(make([]byte, SeedBytes-1))); err == nil {
		t.Fatal("short seed source accepted")
	}
}

// A manifest that does not match the lease, or is invalid, refuses the
// audit before any request: the server's counter stays 0.
func TestInputProblemsFetchNothing(t *testing.T) {
	segments, pkg := fetchtest.Synthetic("inputs", []int{100, 200})
	manifest := manifestJSON(t, testContent, segments)
	e := serve(t, pkg, fetchtest.Correct)
	lease := leaseFor(t, manifest)
	// The same package with a zero-length segment in front whose digest is
	// not the SHA-256 of empty input. The lease matches this manifest.
	empty := fetchtest.Segment{ID: "s9999", SHA256: sha256.Sum256([]byte("not empty"))}
	emptyWrong := manifestJSON(t, testContent, append([]fetchtest.Segment{empty}, segments...))
	cases := []struct {
		name     string
		lease    Lease
		manifest []byte
		want     InputState
	}{
		{"digest", Lease{ContentID: lease.ContentID, ContentVersion: 1, PackageBytes: lease.PackageBytes, ManifestSHA256: sha256.Sum256([]byte("other"))}, manifest, InputManifestMismatch},
		{"content id", Lease{ContentID: "c2", ContentVersion: 1, PackageBytes: lease.PackageBytes, ManifestSHA256: lease.ManifestSHA256}, manifest, InputManifestMismatch},
		{"version", Lease{ContentID: lease.ContentID, ContentVersion: 2, PackageBytes: lease.PackageBytes, ManifestSHA256: lease.ManifestSHA256}, manifest, InputManifestMismatch},
		{"package bytes", Lease{ContentID: lease.ContentID, ContentVersion: 1, PackageBytes: lease.PackageBytes + 1, ManifestSHA256: lease.ManifestSHA256}, manifest, InputManifestMismatch},
		{"invalid manifest", lease, []byte(`{"schema_version":1}`), InputManifestInvalid},
		{"inconsistent layout", lease, bytes.Replace(manifest, []byte(`"package_bytes":"300"`), []byte(`"package_bytes":"301"`), 1), InputManifestInvalid},
		{"zero-length segment with a non-empty digest", leaseFor(t, emptyWrong), emptyWrong, InputManifestInvalid},
	}
	for _, tc := range cases {
		for _, mode := range []Mode{WholePackage, SampledSegments} {
			k := 0
			if mode == SampledSegments {
				k = 1
			}
			obs := audit(t, e, Request{Lease: tc.lease, Manifest: tc.manifest, Mode: mode, SampleCount: k})
			if obs.Input != tc.want || len(obs.Fetches) != 0 || obs.Seed != nil || obs.BytesRead != 0 {
				t.Fatalf("%s %s: %+v", tc.name, mode, obs)
			}
		}
	}
	if n := e.srv.Requests(); n != 0 {
		t.Fatalf("server saw %d requests", n)
	}
}

func TestWholePackageMatches(t *testing.T) {
	segments, pkg := fetchtest.Synthetic("whole", []int{1000, 2000, 3000})
	manifest := manifestJSON(t, testContent, segments[:2], segments[2:])
	e := serve(t, pkg, fetchtest.Correct)
	obs := audit(t, e, Request{Lease: leaseFor(t, manifest), Manifest: manifest, Mode: WholePackage})
	if obs.Input != InputOK || len(obs.Fetches) != 1 || obs.Fetches[0].Kind != fetch.OK || obs.BytesRead != 6000 {
		t.Fatalf("%+v", obs)
	}
	if got := states(obs.Segments); got[Match] != 3 || len(obs.Segments) != 3 {
		t.Fatalf("%v", got)
	}
}

func TestWholePackageCorruptedSegment(t *testing.T) {
	segments, pkg := fetchtest.Synthetic("corrupt", []int{1000, 2000, 3000})
	manifest := manifestJSON(t, testContent, segments)
	corrupted := append([]byte(nil), pkg...)
	corrupted[1500] ^= 0xff
	e := serve(t, corrupted, fetchtest.Correct)
	obs := audit(t, e, Request{Lease: leaseFor(t, manifest), Manifest: manifest, Mode: WholePackage})
	if obs.Fetches[0].Kind != fetch.OK {
		t.Fatalf("%+v", obs)
	}
	want := []SegmentResult{{"s0000", Match}, {"s0001", DigestMismatch}, {"s0002", Match}}
	if fmt.Sprint(obs.Segments) != fmt.Sprint(want) {
		t.Fatalf("%v", obs.Segments)
	}
}

func TestWholePackageLengthMismatch(t *testing.T) {
	segments, pkg := fetchtest.Synthetic("short", []int{1000, 1000, 2000})
	manifest := manifestJSON(t, testContent, segments)
	e := serve(t, pkg, fetchtest.TruncateClean)
	obs := audit(t, e, Request{Lease: leaseFor(t, manifest), Manifest: manifest, Mode: WholePackage})
	if obs.Fetches[0].Kind != fetch.LengthMismatch || obs.BytesRead != 2000 {
		t.Fatalf("%+v", obs.Fetches)
	}
	want := []SegmentResult{{"s0000", Match}, {"s0001", Match}, {"s0002", NotReceived}}
	if fmt.Sprint(obs.Segments) != fmt.Sprint(want) {
		t.Fatalf("%v", obs.Segments)
	}
}

func TestSampledSegments(t *testing.T) {
	sizes := []int{100, 200, 300, 400, 500, 600, 700, 800}
	segments, pkg := fetchtest.Synthetic("sampled", sizes)
	manifest := manifestJSON(t, testContent, segments)
	lease := leaseFor(t, manifest)
	t.Run("match", func(t *testing.T) {
		e := serve(t, pkg, fetchtest.Correct)
		seed := fixedSeed(7)
		obs := audit(t, e, Request{Lease: lease, Manifest: manifest, Mode: SampledSegments, SampleCount: 3, seedSource: bytes.NewReader(seed[:])})
		if obs.Seed == nil || *obs.Seed != seed || len(obs.Segments) != 3 || states(obs.Segments)[Match] != 3 {
			t.Fatalf("%+v", obs)
		}
		m, _ := ParseManifest(manifest)
		l, _ := NewLayout(m)
		picked, _ := Select(l, seed, 3)
		var want uint64
		for i, span := range picked {
			want += span.Length
			if obs.Segments[i].SegmentID != span.SegmentID {
				t.Fatalf("checked %v, selected %v", obs.Segments, picked)
			}
		}
		if obs.BytesRead != want || e.srv.RangeRequests() != 3 || e.srv.Requests() != 3 {
			t.Fatalf("bytes %d want %d, ranges %d", obs.BytesRead, want, e.srv.RangeRequests())
		}
	})
	t.Run("corrupted segment", func(t *testing.T) {
		corrupted := append([]byte(nil), pkg...)
		corrupted[100+200+5] ^= 0x01 // inside s0002
		e := serve(t, corrupted, fetchtest.Correct)
		obs := audit(t, e, Request{Lease: lease, Manifest: manifest, Mode: SampledSegments, SampleCount: len(sizes)})
		for _, r := range obs.Segments {
			if (r.SegmentID == "s0002") != (r.State == DigestMismatch) {
				t.Fatalf("%v", obs.Segments)
			}
		}
	})
	t.Run("range ignored stops the audit", func(t *testing.T) {
		e := serve(t, pkg, fetchtest.IgnoreRange)
		obs := audit(t, e, Request{Lease: lease, Manifest: manifest, Mode: SampledSegments, SampleCount: 4})
		if len(obs.Fetches) != 1 || obs.Fetches[0].Kind != fetch.RangeUnsupported || states(obs.Segments)[NotReceived] != 4 {
			t.Fatalf("%+v", obs)
		}
		if n := e.srv.Requests(); n != 1 {
			t.Fatalf("server saw %d requests", n)
		}
	})
}

func TestFreshSeedPerAudit(t *testing.T) {
	segments, pkg := fetchtest.Synthetic("fresh", []int{10, 10, 10, 10})
	manifest := manifestJSON(t, testContent, segments)
	e := serve(t, pkg, fetchtest.Correct)
	req := Request{Lease: leaseFor(t, manifest), Manifest: manifest, Mode: SampledSegments, SampleCount: 2}
	a := audit(t, e, req)
	b := audit(t, e, req)
	if a.Seed == nil || b.Seed == nil || *a.Seed == *b.Seed {
		t.Fatal("two audits used the same seed")
	}
}

func TestAuditRequestValidation(t *testing.T) {
	segments, pkg := fetchtest.Synthetic("validation", []int{10, 10})
	manifest := manifestJSON(t, testContent, segments)
	e := serve(t, pkg, fetchtest.Correct)
	lease := leaseFor(t, manifest)
	for name, req := range map[string]Request{
		"unknown mode":         {Lease: lease, Manifest: manifest, Mode: "MERKLE"},
		"whole with k":         {Lease: lease, Manifest: manifest, Mode: WholePackage, SampleCount: 1},
		"sampled without k":    {Lease: lease, Manifest: manifest, Mode: SampledSegments},
		"k above the segments": {Lease: lease, Manifest: manifest, Mode: SampledSegments, SampleCount: 3},
	} {
		if _, err := Audit(context.Background(), e.session, req); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if n := e.srv.Requests(); n != 0 {
		t.Fatalf("server saw %d requests", n)
	}
}

// TestProducerBytesRead is the producer-side measurement of goal 09:
// bytes read for a declared 1 MiB synthetic package of 64 segments, whole
// package versus k = 4 sampled segments. Per-fixture measurement on one
// machine over loopback; no throughput or network-capacity claim.
func TestProducerBytesRead(t *testing.T) {
	sizes := make([]int, 64)
	for i := range sizes {
		sizes[i] = 16384
	}
	segments, pkg := fetchtest.Synthetic("measure", sizes)
	manifest := manifestJSON(t, testContent, segments)
	lease := leaseFor(t, manifest)
	e := serve(t, pkg, fetchtest.Correct)
	whole := audit(t, e, Request{Lease: lease, Manifest: manifest, Mode: WholePackage})
	sampled := audit(t, e, Request{Lease: lease, Manifest: manifest, Mode: SampledSegments, SampleCount: 4})
	if whole.BytesRead != 1<<20 || states(whole.Segments)[Match] != 64 {
		t.Fatalf("whole: %d bytes, %v", whole.BytesRead, states(whole.Segments))
	}
	if sampled.BytesRead != 4*16384 || states(sampled.Segments)[Match] != 4 {
		t.Fatalf("sampled: %d bytes, %v", sampled.BytesRead, states(sampled.Segments))
	}
	t.Logf("per-fixture measurement on one machine; no throughput or network-capacity claim: package 1048576 bytes, 64 segments; WHOLE_PACKAGE bytes_read=%d requests=1; SAMPLED_SEGMENTS k=4 bytes_read=%d requests=4",
		whole.BytesRead, sampled.BytesRead)
}
