package msgkit

import (
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Protocol v0 sizes used by the registry messages. TestBoundsDrift compares
// each value with docs/protocol/bounds.json.
const (
	// IDPattern is the protocol id pattern. ValidID implements it without
	// a regular expression; a test checks both agree.
	IDPattern = genesis.IDPattern
	// IDMaxBytes is the longest id the pattern admits.
	IDMaxBytes = 64
	// PageKeyMaxBytes bounds PageRequest.page_key.
	PageKeyMaxBytes = 64
	// DigestBytes is the SHA-256 output size, the only v0 digest.
	DigestBytes = genesis.DigestBytes
	// PublicKeyBytes and SignatureBytes are the ed25519 sizes.
	PublicKeyBytes = 32
	SignatureBytes = 64
	// AccountBytes is the outer signer address size.
	AccountBytes = genesis.AccountBytes
	// CanonicalProfileVersion is the only accepted SVN-JCS-1 profile version.
	CanonicalProfileVersion = 1
)

// ValidID reports whether id matches ^[a-z0-9][a-z0-9._-]{0,63}$.
func ValidID(id string) bool {
	if len(id) == 0 || len(id) > IDMaxBytes {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case i > 0 && (c == '.' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return true
}

// CheckID is the protocol id rule: an empty id is a missing field
// (MALFORMED); any other id outside the pattern is BOUNDS.
func CheckID(id string) Code {
	if id == "" {
		return Malformed
	}
	if !ValidID(id) {
		return Bounds
	}
	return OK
}

// CheckDigest validates a protocol digest and returns a copy of its value.
// A missing digest, DIGEST_ALGORITHM_UNSPECIFIED, or an unknown algorithm is
// MALFORMED. Only SHA-256 is valid in v0. A value that is not 32 bytes is
// BOUNDS.
func CheckDigest(d *typesv1.Digest) ([]byte, Code) {
	if d == nil {
		return nil, Malformed
	}
	if d.GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {
		return nil, Malformed
	}
	if len(d.GetValue()) != DigestBytes {
		return nil, Bounds
	}
	return append([]byte(nil), d.GetValue()...), OK
}
