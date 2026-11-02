package msgkit

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strconv"

	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// safeIntegerMax is the SVN-JCS-1 integer bound (bounds.json
// safe_integer_max). Registry payloads carry integers as decimal strings,
// so int64 values appear only in hand-built objects.
const safeIntegerMax int64 = 9007199254740991

// Canonical returns the SVN-JCS-1 text of one object.
//
// Values must be string, bool, int64 within the safe integer range,
// map[string]any, or []any. uint64 values go through Dec, bytes through Hex,
// digests through DigestObject, and inner signatures through
// SignatureObject. Any other type, a nil value, a key outside the JCS key
// rules, nesting deeper than 8, or output above 65536 bytes is an error.
func Canonical(fields map[string]any) (string, error) {
	if fields == nil {
		return "", fmt.Errorf("canonical: nil object")
	}
	if err := checkCanonicalValue(fields, 0); err != nil {
		return "", err
	}
	text, err := jcs.Canonicalize(fields)
	if err != nil {
		return "", fmt.Errorf("canonical: %w", err)
	}
	return text, nil
}

func checkCanonicalValue(value any, depth int) error {
	if depth > 8 {
		return fmt.Errorf("canonical: depth above 8")
	}
	switch typed := value.(type) {
	case string, bool:
		return nil
	case int64:
		if typed > safeIntegerMax || typed < -safeIntegerMax {
			return fmt.Errorf("canonical: integer outside the safe range")
		}
		return nil
	case map[string]any:
		if typed == nil {
			return fmt.Errorf("canonical: nil object")
		}
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			if err := checkCanonicalValue(typed[key], depth+1); err != nil {
				return fmt.Errorf("%s: %w", key, err)
			}
		}
		return nil
	case []any:
		if typed == nil {
			return fmt.Errorf("canonical: nil array")
		}
		for _, item := range typed {
			if err := checkCanonicalValue(item, depth+1); err != nil {
				return err
			}
		}
		return nil
	default:
		return fmt.Errorf("canonical: unsupported value type %T", value)
	}
}

// Dec is the decimal string of an unsigned integer.
func Dec(v uint64) string { return strconv.FormatUint(v, 10) }

// Hex is the lowercase hex of a byte string.
func Hex(b []byte) string { return hex.EncodeToString(b) }

// DigestObject is the business-encoding of a digest:
// {"algorithm": <enum name>, "value": <lowercase hex>}. A nil digest encodes
// as the UNSPECIFIED algorithm with an empty value, which no valid digest
// matches.
func DigestObject(d *typesv1.Digest) map[string]any {
	return map[string]any{
		"algorithm": d.GetAlgorithm().String(),
		"value":     Hex(d.GetValue()),
	}
}

// SignatureObject is the business-encoding of an inner signature, keyed by
// proto field names. A nil signature encodes as all-zero fields.
func SignatureObject(sig *typesv1.InnerSignature) map[string]any {
	return map[string]any{
		"canonical_profile_version": Dec(uint64(sig.GetCanonicalProfileVersion())),
		"domain":                    sig.GetDomain().String(),
		"key_id":                    sig.GetKeyId(),
		"public_key":                Hex(sig.GetPublicKey()),
		"signature":                 Hex(sig.GetSignature()),
	}
}

// BusinessDigest is SHA-256 over the SVN-JCS-1 text of the business fields
// of one message. An object that cannot be canonicalized is MALFORMED.
func BusinessDigest(fields map[string]any) ([]byte, Code) {
	text, err := Canonical(fields)
	if err != nil {
		return nil, Malformed
	}
	sum := sha256.Sum256([]byte(text))
	return sum[:], OK
}
