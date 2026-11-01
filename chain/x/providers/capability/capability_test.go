package capability

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The fixture is synthetic: example.invalid is a reserved name.
const validManifest = `{
  "schema_version": 1,
  "profile": "SVN-JCS-1",
  "provider_id": "p1",
  "signing_key_id": "key-p1",
  "declared_capacity_bytes": "1000000",
  "endpoints": [
    {"url": "https://p1.example.invalid/svn", "name": "primary"},
    {"name": "backup", "url": "https://p1-b.example.invalid/svn"}
  ]
}`

// validCanonical is the SVN-JCS-1 text of validManifest, written by hand:
// keys sorted, no whitespace, array order kept.
const validCanonical = `{"declared_capacity_bytes":"1000000","endpoints":[{"name":"primary","url":"https://p1.example.invalid/svn"},{"name":"backup","url":"https://p1-b.example.invalid/svn"}],"profile":"SVN-JCS-1","provider_id":"p1","schema_version":1,"signing_key_id":"key-p1"}`

func TestParseValidManifest(t *testing.T) {
	result, err := Parse([]byte(validManifest))
	if err != nil {
		t.Fatal(err)
	}
	if result.Canonical != validCanonical {
		t.Fatalf("canonical\n%s\nwant\n%s", result.Canonical, validCanonical)
	}
	want := sha256.Sum256([]byte(validCanonical))
	if result.SHA256 != want {
		t.Fatalf("digest %x, want %x", result.SHA256, want)
	}
	digest := result.Digest()
	if digest.GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 || hex.EncodeToString(digest.GetValue()) != hex.EncodeToString(want[:]) {
		t.Fatalf("protocol digest %v", digest)
	}
	m := result.Manifest
	if m.ProviderID != "p1" || m.SigningKeyID != "key-p1" || m.DeclaredCapacityBytes != 1_000_000 || len(m.Endpoints) != 2 ||
		m.Endpoints[0] != (Endpoint{Name: "primary", URL: "https://p1.example.invalid/svn"}) {
		t.Fatalf("manifest %+v", m)
	}
	// Whitespace and key order do not change the digest.
	again, err := Parse([]byte(validCanonical))
	if err != nil || again.SHA256 != result.SHA256 {
		t.Fatalf("canonical input: %v", err)
	}
}

func TestEncodeMatchesParse(t *testing.T) {
	m := Manifest{
		ProviderID: "p1", SigningKeyID: "key-p1", DeclaredCapacityBytes: 1_000_000,
		Endpoints: []Endpoint{{Name: "primary", URL: "https://p1.example.invalid/svn"}, {Name: "backup", URL: "https://p1-b.example.invalid/svn"}},
	}
	result, err := Encode(m)
	if err != nil {
		t.Fatal(err)
	}
	if result.Canonical != validCanonical {
		t.Fatalf("Encode canonical %s", result.Canonical)
	}
	bad := m
	bad.Endpoints = nil
	if _, err := Encode(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Encode without endpoints: %v", err)
	}
	bad = m
	bad.DeclaredCapacityBytes = 0
	if _, err := Encode(bad); !errors.Is(err, ErrInvalid) {
		t.Fatalf("Encode with capacity 0: %v", err)
	}
}

func replace(old, new string) string { return strings.Replace(validManifest, old, new, 1) }

func endpoints(n int, url string) string {
	items := make([]string, n)
	for i := range items {
		items[i] = fmt.Sprintf(`{"name":"e%d","url":%q}`, i, url)
	}
	return "[" + strings.Join(items, ",") + "]"
}

func withEndpoints(list string) string {
	start := strings.Index(validManifest, `"endpoints": [`)
	return validManifest[:start] + `"endpoints": ` + list + "\n}"
}

func TestParseRejects(t *testing.T) {
	cases := map[string]string{
		"not-json":                 `{"schema_version":`,
		"top-level-array":          `[]`,
		"top-level-string":         `"x"`,
		"empty":                    ``,
		"bom":                      "\xef\xbb\xbf" + validManifest,
		"invalid-utf8":             replace(`"primary"`, "\"pri\xffmary\""),
		"duplicate-key":            replace(`"profile": "SVN-JCS-1",`, `"profile": "SVN-JCS-1", "profile": "SVN-JCS-1",`),
		"null-value":               replace(`"profile": "SVN-JCS-1"`, `"profile": null`),
		"trailing-data":            validManifest + `{}`,
		"extra-key":                replace(`"profile": "SVN-JCS-1",`, `"profile": "SVN-JCS-1", "contact_email": "ops@example.invalid",`),
		"case-variant-key":         replace(`"profile":`, `"Profile":`),
		"missing-key":              replace(`"signing_key_id": "key-p1",`, ``),
		"schema-version-2":         replace(`"schema_version": 1`, `"schema_version": 2`),
		"schema-version-string":    replace(`"schema_version": 1`, `"schema_version": "1"`),
		"schema-version-fraction":  replace(`"schema_version": 1`, `"schema_version": 1.0`),
		"profile-other":            replace(`"SVN-JCS-1"`, `"JCS"`),
		"provider-id-pattern":      replace(`"provider_id": "p1"`, `"provider_id": "P1"`),
		"provider-id-65":           replace(`"provider_id": "p1"`, `"provider_id": "a`+strings.Repeat("b", 64)+`"`),
		"provider-id-number":       replace(`"provider_id": "p1"`, `"provider_id": 1`),
		"signing-key-id-empty":     replace(`"signing_key_id": "key-p1"`, `"signing_key_id": ""`),
		"capacity-leading-zero":    replace(`"1000000"`, `"01000000"`),
		"capacity-zero":            replace(`"1000000"`, `"0"`),
		"capacity-number":          replace(`"1000000"`, `1000000`),
		"capacity-21-chars":        replace(`"1000000"`, `"100000000000000000000"`),
		"capacity-above-uint64":    replace(`"1000000"`, `"18446744073709551616"`),
		"capacity-sign":            replace(`"1000000"`, `"+1000000"`),
		"endpoints-empty":          withEndpoints(`[]`),
		"endpoints-9":              withEndpoints(endpoints(9, "https://e.example.invalid")),
		"endpoints-not-array":      withEndpoints(`{}`),
		"endpoint-not-object":      withEndpoints(`["https://e.example.invalid"]`),
		"endpoint-http":            withEndpoints(`[{"name":"a","url":"http://e.example.invalid"}]`),
		"endpoint-https-uppercase": withEndpoints(`[{"name":"a","url":"HTTPS://e.example.invalid"}]`),
		"endpoint-url-7-chars":     withEndpoints(`[{"name":"a","url":"https:/"}]`),
		"endpoint-url-2049-chars":  withEndpoints(endpoints(1, "https://"+strings.Repeat("a", 2041))),
		"endpoint-name-empty":      withEndpoints(`[{"name":"","url":"https://e.example.invalid"}]`),
		"endpoint-name-65-chars":   withEndpoints(`[{"name":"` + strings.Repeat("n", 65) + `","url":"https://e.example.invalid"}]`),
		"endpoint-extra-key":       withEndpoints(`[{"name":"a","url":"https://e.example.invalid","token":"x"}]`),
		"endpoint-missing-url":     withEndpoints(`[{"name":"a"}]`),
		"endpoint-url-number":      withEndpoints(`[{"name":"a","url":8}]`),
		"oversized-input":          validManifest + strings.Repeat(" ", 1<<20),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw)); !errors.Is(err, ErrInvalid) {
				t.Fatalf("Parse: %v, want ErrInvalid", err)
			}
		})
	}
}

func TestParseBoundaries(t *testing.T) {
	cases := map[string]string{
		"endpoints-1":             withEndpoints(endpoints(1, "https://e.example.invalid")),
		"endpoints-8":             withEndpoints(endpoints(8, "https://e.example.invalid")),
		"endpoint-url-8-chars":    withEndpoints(`[{"name":"a","url":"https://"}]`),
		"endpoint-url-2048-chars": withEndpoints(endpoints(1, "https://"+strings.Repeat("a", 2040))),
		"endpoint-name-64-chars":  withEndpoints(`[{"name":"` + strings.Repeat("n", 64) + `","url":"https://e.example.invalid"}]`),
		"endpoint-name-64-runes":  withEndpoints(`[{"name":"` + strings.Repeat("é", 64) + `","url":"https://e.example.invalid"}]`),
		"capacity-uint64-max":     replace(`"1000000"`, `"18446744073709551615"`),
		"provider-id-64":          replace(`"provider_id": "p1"`, `"provider_id": "a`+strings.Repeat("b", 63)+`"`),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Parse([]byte(raw)); err != nil {
				t.Fatalf("Parse: %v", err)
			}
		})
	}
}

// FuzzCapabilityParse: Parse never panics, and an accepted manifest
// re-encodes to the same canonical text and digest.
func FuzzCapabilityParse(f *testing.F) {
	f.Add([]byte(validManifest))
	f.Add([]byte(validCanonical))
	f.Add([]byte(`{}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		result, err := Parse(raw)
		if err != nil {
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("error outside ErrInvalid: %v", err)
			}
			return
		}
		again, err := Encode(result.Manifest)
		if err != nil {
			t.Fatalf("accepted manifest does not re-encode: %v", err)
		}
		if again.Canonical != result.Canonical || again.SHA256 != result.SHA256 {
			t.Fatal("re-encoding changed the canonical form")
		}
	})
}
