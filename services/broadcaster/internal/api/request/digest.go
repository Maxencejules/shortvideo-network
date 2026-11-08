package request

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
)

// digestLabel is the first frame of every request digest, so the digest
// cannot equal a SHA-256 the broadcaster computes over other framed data.
const digestLabel = "svn-broadcaster/request-digest/v1"

// Digest is the request digest of plan 5.4: SHA-256 over a length-prefixed
// framing (each frame an 8-byte big-endian length, then its bytes) of the
// label, the kind name, the stored request (Decoded.Stored: the
// deterministic protobuf of the chain message with signer and business_id
// empty, or the forwarded transaction) and the canonical JSON of the
// authorization context. JSON key order, whitespace and string escapes of
// the request body do not change it, because none of them reaches the
// stored request or the canonical context. The relay locators (provider
// id, content id) are not covered: they only name where the pre-check
// reads, and a wrong one fails that check.
func Digest(kind policy.Kind, stored []byte, auth policy.AuthContext) ([32]byte, error) {
	if _, err := LookupKind(string(kind)); err != nil {
		return [32]byte{}, err
	}
	if len(stored) == 0 {
		return [32]byte{}, errors.New("request: digest of an empty request")
	}
	authJSON, err := CanonicalAuthJSON(auth)
	if err != nil {
		return [32]byte{}, err
	}
	h := sha256.New()
	var length [8]byte
	for _, frame := range [][]byte{[]byte(digestLabel), []byte(kind), stored, authJSON} {
		binary.BigEndian.PutUint64(length[:], uint64(len(frame)))
		h.Write(length[:])
		h.Write(frame)
	}
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}
