package msgkit

import (
	"bytes"
	"encoding/hex"
	"strings"
	"testing"

	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The rotate payload keeps the goal-06 canonical form byte for byte.
func TestCanonicalRotatePayloadText(t *testing.T) {
	got := rotateCanonical(t, bytes.Repeat([]byte{0x07}, 32))
	want := `{"expectedGeneration":"1","newSigningKeyId":"key-2","newSigningPubkey":"` +
		strings.Repeat("07", 32) + `","providerId":"provider-1"}`
	if got != want {
		t.Fatalf("canonical rotate\n got %s\nwant %s", got, want)
	}
}

func TestCanonicalRejectsNonCanonicalValues(t *testing.T) {
	deep := map[string]any{"v": "x"}
	for i := 0; i < 9; i++ {
		deep = map[string]any{"n": deep}
	}
	cases := map[string]map[string]any{
		"nil object":         nil,
		"uint64":             {"a": uint64(1)},
		"int":                {"a": 1},
		"float64":            {"a": 1.5},
		"bytes":              {"a": []byte{1}},
		"nil value":          {"a": nil},
		"nil nested object":  {"a": map[string]any(nil)},
		"nil array":          {"a": []any(nil)},
		"unsafe int64":       {"a": int64(9007199254740992)},
		"unsafe negative":    {"a": int64(-9007199254740992)},
		"key with space":     {"a b": "x"},
		"empty key":          {"": "x"},
		"key of 65 bytes":    {strings.Repeat("k", 65): "x"},
		"depth above 8":      deep,
		"text above 64 KiB":  {"a": strings.Repeat("x", 65536)},
		"bad value in array": {"a": []any{"x", uint32(2)}},
		"invalid UTF-8":      {"a": "x\xff"},
		"surrogate bytes":    {"a": "\xed\xa0\x80"},
		"nested bad UTF-8":   {"a": map[string]any{"b": []any{"ok", "\xfe"}}},
		"invalid UTF-8 key":  {"a\xff": "x"},
	}
	for name, fields := range cases {
		if text, err := Canonical(fields); err == nil {
			t.Errorf("%s: accepted as %q", name, text)
		}
		if digest, code := BusinessDigest(fields); code != Malformed || digest != nil {
			t.Errorf("%s: BusinessDigest %x %q", name, digest, code)
		}
	}
	ok, err := Canonical(map[string]any{"b": true, "a": int64(9007199254740991), "c": []any{"x", map[string]any{}}})
	if err != nil || ok != `{"a":9007199254740991,"b":true,"c":["x",{}]}` {
		t.Fatalf("valid object: %q %v", ok, err)
	}
}

// A string is never rewritten: "c1" + U+FFFD has its own canonical text and
// digest, and the invalid spellings that the SVN-JCS-1 writer used to turn
// into the same U+FFFD text have none (MALFORMED). The error names the field.
func TestCanonicalRefusesInvalidUTF8(t *testing.T) {
	replacement := map[string]any{"content_id": "c1\U0000FFFD"}
	text, err := Canonical(replacement)
	if err != nil || text != "{\"content_id\":\"c1\U0000FFFD\"}" {
		t.Fatalf("U+FFFD spelling: %q %v", text, err)
	}
	if digest, code := BusinessDigest(replacement); code != OK || len(digest) != 32 {
		t.Fatalf("U+FFFD digest: %x %q", digest, code)
	}
	for _, bad := range []string{"c1\xff", "c1\xfe", "c1\xed\xa0\x80"} {
		fields := map[string]any{"content_id": bad, "lease": map[string]any{"id": "l1"}}
		text, err := Canonical(fields)
		if err == nil || text != "" {
			t.Fatalf("%q accepted as %q", bad, text)
		}
		if !strings.Contains(err.Error(), "content_id: canonical: string is not valid UTF-8") {
			t.Fatalf("%q: error %q does not name the field", bad, err)
		}
		if digest, code := BusinessDigest(fields); code != Malformed || digest != nil {
			t.Fatalf("%q: BusinessDigest %x %q", bad, digest, code)
		}
	}
}

func TestEncoders(t *testing.T) {
	if Dec(0) != "0" || Dec(18446744073709551615) != "18446744073709551615" {
		t.Fatal("Dec")
	}
	if Hex([]byte{0xAB, 0x01}) != "ab01" || Hex(nil) != "" {
		t.Fatal("Hex")
	}
	digest := &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes.Repeat([]byte{0xab}, 32)}
	text, err := Canonical(map[string]any{"d": DigestObject(digest)})
	if err != nil || text != `{"d":{"algorithm":"DIGEST_ALGORITHM_SHA256","value":"`+strings.Repeat("ab", 32)+`"}}` {
		t.Fatalf("DigestObject: %q %v", text, err)
	}
	text, err = Canonical(map[string]any{"d": DigestObject(nil)})
	if err != nil || text != `{"d":{"algorithm":"DIGEST_ALGORITHM_UNSPECIFIED","value":""}}` {
		t.Fatalf("DigestObject(nil): %q %v", text, err)
	}
	sig := &typesv1.InnerSignature{
		Domain:                  domainRotate,
		PublicKey:               []byte{1, 2},
		Signature:               []byte{0xff},
		KeyId:                   "key-1",
		CanonicalProfileVersion: 1,
	}
	text, err = Canonical(map[string]any{"s": SignatureObject(sig)})
	want := `{"s":{"canonical_profile_version":"1","domain":"SIGNATURE_DOMAIN_PROVIDER_ROTATE","key_id":"key-1","public_key":"0102","signature":"ff"}}`
	if err != nil || text != want {
		t.Fatalf("SignatureObject: %q %v", text, err)
	}
	text, err = Canonical(map[string]any{"s": SignatureObject(nil)})
	want = `{"s":{"canonical_profile_version":"0","domain":"SIGNATURE_DOMAIN_UNSPECIFIED","key_id":"","public_key":"","signature":""}}`
	if err != nil || text != want {
		t.Fatalf("SignatureObject(nil): %q %v", text, err)
	}
}

// The vector was computed independently with Python json.dumps(sort_keys,
// compact separators) and hashlib.sha256 over the same object.
func TestBusinessDigestVector(t *testing.T) {
	fields := map[string]any{
		"provider_id":             "provider-1",
		"declared_capacity_bytes": Dec(1073741824),
		"endpoint_manifest_digest": DigestObject(&typesv1.Digest{
			Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
			Value:     bytes.Repeat([]byte{0xab}, 32),
		}),
		"signing_pubkey": Hex(bytes.Repeat([]byte{0x07}, 32)),
		"current_key_signature": SignatureObject(&typesv1.InnerSignature{
			Domain:                  domainRotate,
			PublicKey:               bytes.Repeat([]byte{0x01}, 32),
			Signature:               bytes.Repeat([]byte{0x02}, 64),
			KeyId:                   "key-1",
			CanonicalProfileVersion: 1,
		}),
	}
	digest, code := BusinessDigest(fields)
	if code != OK {
		t.Fatal(code)
	}
	if got := hex.EncodeToString(digest); got != "879b1ca2f6b3f5986512ba2af0d956a8ae0c6ad3eec64109f65a661ccef9c400" {
		t.Fatalf("business digest %s", got)
	}
	// Any field change changes the digest.
	fields["declared_capacity_bytes"] = Dec(1073741825)
	changed, _ := BusinessDigest(fields)
	if bytes.Equal(changed, digest) {
		t.Fatal("digest ignores a field")
	}
}
