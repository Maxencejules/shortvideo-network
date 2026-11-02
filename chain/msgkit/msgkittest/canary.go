package msgkittest

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

var base64Encodings = []struct {
	name string
	enc  *base64.Encoding
}{
	{"base64-std", base64.StdEncoding},
	{"base64-url", base64.URLEncoding},
}

// FindCanary reports whether blob contains any canary as raw bytes, as
// lowercase or uppercase hex, or as standard or URL-safe base64 at any of
// the three byte alignments inside a larger encoded buffer. The string
// names the canary index and the form that matched. An empty canary is
// reported as found, so a test with an empty canary fails instead of
// passing vacuously.
func FindCanary(blob []byte, canaries ...[]byte) (string, bool) {
	for i, canary := range canaries {
		if len(canary) == 0 {
			return fmt.Sprintf("canary %d is empty", i), true
		}
		if bytes.Contains(blob, canary) {
			return fmt.Sprintf("canary %d (raw)", i), true
		}
		lower := []byte(hex.EncodeToString(canary))
		if bytes.Contains(blob, lower) {
			return fmt.Sprintf("canary %d (hex)", i), true
		}
		if bytes.Contains(blob, bytes.ToUpper(lower)) {
			return fmt.Sprintf("canary %d (HEX)", i), true
		}
		for _, encoding := range base64Encodings {
			for shift := 0; shift < 3; shift++ {
				core := base64Core(encoding.enc, canary, shift)
				if len(core) > 0 && bytes.Contains(blob, core) {
					return fmt.Sprintf("canary %d (%s, offset %d)", i, encoding.name, shift), true
				}
			}
		}
	}
	return "", false
}

// base64Core is the run of base64 characters that depend only on canary
// bytes when the canary starts shift bytes into an encoded buffer: the
// characters whose six bits lie wholly inside the canary.
func base64Core(enc *base64.Encoding, canary []byte, shift int) []byte {
	padded := make([]byte, shift+len(canary))
	copy(padded[shift:], canary)
	encoded := []byte(enc.EncodeToString(padded))
	first := (8*shift + 5) / 6
	last := (8 * (shift + len(canary))) / 6
	if last <= first {
		return nil
	}
	return encoded[first:last]
}
