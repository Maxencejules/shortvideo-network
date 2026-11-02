package auditor

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// sources returns every non-test .go file of this module with its text.
func sources(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && d.Name() == "testdata" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[filepath.ToSlash(path)] = string(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 10 {
		t.Fatalf("found only %d source files; the walk is broken", len(out))
	}
	return out
}

// No non-test file turns TLS verification off or reaches a proxy-aware
// default client or transport.
func TestNoInsecureTLSOrProxyInSource(t *testing.T) {
	for path, text := range sources(t) {
		for _, token := range []string{"InsecureSkipVerify", "ProxyFromEnvironment", "ProxyURL", "DefaultTransport", "DefaultClient", "http.Get(", "http.Head(", "http.Post("} {
			if strings.Contains(text, token) {
				t.Errorf("%s contains %s", path, token)
			}
		}
	}
}

// No Merkle construction is written: nothing here is a storage proof.
func TestNoMerkleCode(t *testing.T) {
	for path, text := range sources(t) {
		if strings.Contains(strings.ToLower(text), "merkle") {
			t.Errorf("%s mentions a Merkle construction", path)
		}
	}
}

// fetch/fetchtest is a test helper: only tests import it.
func TestFetchtestOnlyInTests(t *testing.T) {
	for path, text := range sources(t) {
		if strings.HasPrefix(path, "fetch/fetchtest/") {
			continue
		}
		if strings.Contains(text, `/agents/auditor/fetch/fetchtest"`) {
			t.Errorf("%s imports fetchtest", path)
		}
	}
}

// The test seams are assigned only in their own package's test files: the
// fetch connect seam (Client.connect) and the verify sample seed source
// (Request.seedSource, so that no caller can fix the seed of a sampled
// audit). With TestSeamNotReachableThroughOptions (fetch) and
// TestConfigHasNoBypass (config), this shows no configuration value or
// exported API can enable either.
func TestTestSeamOnlySetInTests(t *testing.T) {
	for _, seam := range []struct{ field, tests string }{
		{"connect", "fetch/*_test.go"},
		{"seedSource", "verify/*_test.go"},
	} {
		// An assignment (x.field = ...) or a composite-literal key
		// (field: ...), not text inside a string.
		set := regexp.MustCompile(`(?m)\.` + seam.field + `\s*=[^=]|(^|[{,\s])` + seam.field + `\s*:\s*[^\s"]`)
		for path, text := range sources(t) {
			if loc := set.FindStringIndex(text); loc != nil {
				t.Errorf("%s sets the %s seam: %q", path, seam.field, text[loc[0]:loc[1]])
			}
		}
		tests, err := filepath.Glob(seam.tests)
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, path := range tests {
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			found = found || set.Match(raw)
		}
		if !found {
			t.Fatalf("no test in %s sets the %s seam; the pattern no longer matches it", seam.tests, seam.field)
		}
	}
}
