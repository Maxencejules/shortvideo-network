package msgkittest

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// boundsRelPath is docs/protocol/bounds.json relative to the repository root.
var boundsRelPath = filepath.Join("docs", "protocol", "bounds.json")

// BoundsFile returns the path of docs/protocol/bounds.json, found by walking
// up from the test's working directory (the package directory under
// go test).
func BoundsFile(t testing.TB) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("msgkittest: working directory: %v", err)
	}
	for range 16 {
		candidate := filepath.Join(dir, boundsRelPath)
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("msgkittest: %s not found above the working directory", boundsRelPath)
	return ""
}

// ForbiddenNamePatterns loads forbidden_name_patterns and
// forbidden_name_exemptions from bounds.json in file order.
func ForbiddenNamePatterns(t testing.TB) (patterns, exemptions []string) {
	t.Helper()
	raw, err := os.ReadFile(BoundsFile(t))
	if err != nil {
		t.Fatalf("msgkittest: read bounds: %v", err)
	}
	var doc struct {
		Patterns   []string `json:"forbidden_name_patterns"`
		Exemptions []string `json:"forbidden_name_exemptions"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatalf("msgkittest: parse bounds: %v", err)
	}
	if len(doc.Patterns) == 0 {
		t.Fatal("msgkittest: bounds.json has no forbidden_name_patterns")
	}
	return doc.Patterns, doc.Exemptions
}

var (
	ipToken   = regexp.MustCompile(`(?:^|_)ip(?:_|$)`)
	addrToken = regexp.MustCompile(`(?:^|_)addr(?:_|$)`)
)

// NameHits returns the patterns that match name, in pattern order, with the
// goal-02 rule (docs/evidence/goal-02/check_protocol.py pattern_matches):
// an exempt name matches nothing; matching is case-insensitive; "ip" matches
// only as an underscore-delimited token or inside "ipv4", "ipv6", or
// "ipaddress"; "addr" matches only as an underscore-delimited token; every
// other pattern matches as a substring. Case folding is ASCII lower-casing,
// which equals Python casefold for the ASCII names checked here.
func NameHits(name string, patterns, exemptions []string) []string {
	for _, exempt := range exemptions {
		if name == exempt {
			return nil
		}
	}
	folded := strings.ToLower(name)
	var hits []string
	for _, pattern := range patterns {
		if patternMatches(folded, strings.ToLower(pattern)) {
			hits = append(hits, pattern)
		}
	}
	return hits
}

func patternMatches(folded, needle string) bool {
	switch needle {
	case "ip":
		if ipToken.MatchString(folded) {
			return true
		}
		return strings.Contains(folded, "ipv4") || strings.Contains(folded, "ipv6") || strings.Contains(folded, "ipaddress")
	case "addr":
		return addrToken.MatchString(folded)
	default:
		return strings.Contains(folded, needle)
	}
}
