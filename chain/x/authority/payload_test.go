package authority

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
)

// ParsePayloadJSON parses one bounded SVN-JCS-1 object. The vector and fuzz
// tests use it; the chain parses payloads from proto, not from JSON.
// Unknown fields and a second alternative are malformed.
func ParsePayloadJSON(raw []byte) (string, Outcome) {
	if len(raw) == 0 || len(raw) > 8192 {
		return "", fail(CodeBounds)
	}
	canon, err := jcs.ParseAndCanonicalize(string(raw))
	if err != nil {
		return "", fail(CodeMalformed)
	}
	return canon, ok()
}

func TestPayloadVectorDigest(t *testing.T) {
	canon, out := ParsePayloadJSON([]byte(`{"validatorPower":{"power":"1","nodeId":"v5","reasonDigest":{"value":"CX2wA0prEclni4NHxA3ZzCI+No6UaDVlor0CjMc0+FA=","algorithm":"DIGEST_ALGORITHM_SHA256"},"consensusPubkey":"6KCHbf6Mj7qib+NE/UMKju7jD63veJqJ+OatCH9lUKs=","activation":{"height":"3"}}}`))
	if !out.Success() {
		t.Fatal(out)
	}
	sum := sha256.Sum256([]byte(canon))
	got := hex.EncodeToString(sum[:])
	if got != "be9df594c416ecbaf50512e298a73fecf9f7a56e7ac83a0cf4edf87a6b522c48" {
		t.Fatalf("digest %s canonical %s", got, canon)
	}
}

func TestFuzzBoundedPayload(t *testing.T) {
	var seed int64 = 6
	next := func() byte {
		seed = seed*6364136223846793005 + 1
		return byte(seed >> 33)
	}
	for i := 0; i < 200; i++ {
		n := int(next()%64) + 1
		raw := make([]byte, n)
		for j := range raw {
			raw[j] = next()
		}
		_, _ = ParsePayloadJSON(raw)
	}
	if _, out := ParsePayloadJSON(bytes.Repeat([]byte{'a'}, 9000)); out.Code != CodeBounds {
		t.Fatal(out)
	}
}

func FuzzParsePayload(f *testing.F) {
	f.Add([]byte(`{"validatorPower":{"nodeId":"v5","activation":{"height":"3"}}}`))
	f.Fuzz(func(t *testing.T, raw []byte) {
		if len(raw) > 4096 {
			raw = raw[:4096]
		}
		_, _ = ParsePayloadJSON(raw)
	})
}
