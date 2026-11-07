package verify

import (
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch/fetchtest"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
)

// manifestSeeds is the seed corpus of FuzzManifestParse: the goal-02 valid
// and invalid manifest fixtures, a built manifest with a zero-length
// segment among several, and edits that fail ParseManifest or NewLayout.
func manifestSeeds(t testing.TB) [][]byte {
	t.Helper()
	var out [][]byte
	for _, name := range []string{
		"manifest-valid.json", "manifest-invalid-boundary.json", "manifest-invalid-unknown.json",
		"manifest-invalid-wrong-domain.json", "manifest-invalid-duplicate-id.json",
	} {
		raw, err := os.ReadFile(filepath.Join("../../../docs/protocol/fixtures", name))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, raw)
	}
	segments, _ := fetchtest.Synthetic("fuzz", []int{3, 0, 5, 1, 2})
	built := string(manifestJSON(t, testContent, segments))
	out = append(out, []byte(built))
	for _, edit := range [][2]string{
		{`"package_bytes":"11"`, `"package_bytes":"12"`}, // NewLayout: sum mismatch
		{`"package_bytes":"11"`, `"package_bytes":"0"`},  // ParseManifest or NewLayout: zero package
		{`"schema_version":1`, `"schema_version":2`},
		{`"profile":"SVN-JCS-1"`, `"profile":"JCS"`},
		{`"codec":"avc1"`, `"codec":""`},
		{`{`, `{"extra":1,`},
	} {
		if !strings.Contains(built, edit[0]) {
			t.Fatalf("built manifest lacks %q", edit[0])
		}
		out = append(out, []byte(strings.Replace(built, edit[0], edit[1], 1)))
	}
	return append(out, []byte(""), []byte("[]"), []byte(`{"schema_version":1}`), []byte("{\"a\":\"\xff\"}"))
}

// TestManifestSeeds: the seed corpus reaches an accepted manifest with a
// layout, a manifest refused by ParseManifest, and a manifest that parses
// but whose layout NewLayout refuses.
func TestManifestSeeds(t *testing.T) {
	var laidOut, parseRefused, layoutRefused int
	for _, raw := range manifestSeeds(t) {
		m, err := ParseManifest(raw)
		if err != nil {
			if !errors.Is(err, ErrManifestInvalid) {
				t.Fatalf("%.80q: %v", raw, err)
			}
			parseRefused++
			continue
		}
		if _, err := NewLayout(m); err != nil {
			layoutRefused++
			continue
		}
		laidOut++
	}
	if laidOut < 2 || parseRefused < 5 || layoutRefused < 1 {
		t.Fatalf("seeds reach %d layouts, %d parse refusals, %d layout refusals", laidOut, parseRefused, layoutRefused)
	}
}

// FuzzManifestParse: ParseManifest and NewLayout never panic and refuse only
// with ErrManifestInvalid. An accepted manifest's Canonical text is SVN-JCS-1
// canonical, SHA256 is its digest, and it parses again to the same
// manifest. An accepted layout is the segments in manifest order, contiguous
// from offset 0, adding up to package_bytes (1 to MaxPackageBytes).
func FuzzManifestParse(f *testing.F) {
	for _, raw := range manifestSeeds(f) {
		f.Add(raw)
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		m, err := ParseManifest(raw)
		if err != nil {
			if !errors.Is(err, ErrManifestInvalid) {
				t.Fatalf("%q: %v", raw, err)
			}
			return
		}
		if canonical, err := jcs.ParseAndCanonicalize(m.Canonical); err != nil || canonical != m.Canonical {
			t.Fatalf("%q: Canonical is not canonical: %v", raw, err)
		}
		if m.SHA256 != sha256.Sum256([]byte(m.Canonical)) {
			t.Fatalf("%q: SHA256 is not the digest of Canonical", raw)
		}
		again, err := ParseManifest([]byte(m.Canonical))
		if err != nil || !reflect.DeepEqual(again, m) {
			t.Fatalf("%q: Canonical parses to %+v, %v", raw, again, err)
		}
		l, err := NewLayout(m)
		if err != nil {
			if !errors.Is(err, ErrManifestInvalid) {
				t.Fatalf("%q: layout %v", raw, err)
			}
			return
		}
		var offset uint64
		i := 0
		for _, r := range m.Renditions {
			for _, s := range r.Segments {
				if i >= len(l.Spans) {
					t.Fatalf("%q: %d spans for more segments", raw, len(l.Spans))
				}
				span := l.Spans[i]
				if span.SegmentID != s.ID || span.Offset != offset || span.Length != s.ByteCount || span.SHA256 != s.SHA256 {
					t.Fatalf("%q: span %d is %+v", raw, i, span)
				}
				offset += s.ByteCount
				i++
			}
		}
		if i != len(l.Spans) || offset != m.PackageBytes || l.PackageBytes != offset || offset == 0 || offset > MaxPackageBytes {
			t.Fatalf("%q: layout %d spans, %d bytes, package %d", raw, len(l.Spans), l.PackageBytes, m.PackageBytes)
		}
	})
}
