// Package records verifies versioned public-record envelopes.
//
// The signed bytes are SVN-PUBLIC-RECORD-1, a NUL byte, and the SVN-JCS-1
// text of the envelope with the signature member omitted. This package does
// not implement a second canonicalizer. Callers pass the canonical text.
// Canonicalization remains the Goal 02 profile.
//
// A key reference is a public key and a key id. Retired keys still verify
// records they signed. A new sequence requires the active key. This package
// does not generate a key and does not hold a host signing key. Delegated
// host-signing custody is BLOCKED on DEC-001.
package records

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
)

const Domain = "SVN-PUBLIC-RECORD-1"

var (
	ErrSignature = errors.New("signature")
	ErrMalformed = errors.New("malformed")
	ErrReplay    = errors.New("replay")
	ErrPrevious  = errors.New("previous")
	ErrSequence  = errors.New("sequence")
	ErrKey       = errors.New("key")
)

// Key is a public key reference. Active is false once the key is retired.
type Key struct {
	Public ed25519.PublicKey
	Active bool
}

// Envelope is the unsigned public record.
type Envelope struct {
	SchemaVersion  int     `json:"schema_version"`
	Profile        string  `json:"profile"`
	PrivacyClass   string  `json:"privacy_class"`
	AuthorID       string  `json:"author_id"`
	RecordID       string  `json:"record_id"`
	Sequence       int64   `json:"sequence"`
	PreviousDigest string  `json:"previous_digest"`
	KeyID          string  `json:"key_id"`
	Payload        Payload `json:"payload"`
}

// Payload is the public body. Private classes are rejected.
type Payload struct {
	RecordType string `json:"record_type"`
	Body       string `json:"body"`
}

// History is the accepted sequence for one author and record id.
type History struct {
	seen map[string]head
}

type head struct {
	sequence int64
	digest   string
}

// Message is the byte string an ed25519 signature covers.
func Message(canonical string) []byte {
	msg := make([]byte, 0, len(Domain)+1+len(canonical))
	msg = append(msg, Domain...)
	msg = append(msg, 0)
	return append(msg, canonical...)
}

// Verify checks the signature and the envelope fields. It allows a retired key.
func Verify(canonical string, sig []byte, keys map[string]Key) (Envelope, error) {
	env, err := parse(canonical)
	if err != nil {
		return Envelope{}, err
	}
	key, ok := keys[env.KeyID]
	if !ok {
		return Envelope{}, ErrKey
	}
	if !ed25519.Verify(key.Public, Message(canonical), sig) {
		return Envelope{}, ErrSignature
	}
	return env, nil
}

// Accept verifies a new record and rejects replay, a gap, a bad previous
// digest, and a retired key. The digest of an accepted record is
// SHA-256 of its canonical text.
func Accept(h *History, canonical string, sig []byte, keys map[string]Key) (Envelope, error) {
	env, err := Verify(canonical, sig, keys)
	if err != nil {
		return Envelope{}, err
	}
	if h == nil {
		return Envelope{}, ErrMalformed
	}
	if h.seen == nil {
		h.seen = map[string]head{}
	}
	id := env.AuthorID + "\x00" + env.RecordID
	prev, ok := h.seen[id]
	sum := sha256.Sum256([]byte(canonical))
	digest := hex.EncodeToString(sum[:])
	if !ok {
		if env.Sequence != 0 || env.PreviousDigest != strings.Repeat("0", 64) {
			return Envelope{}, ErrSequence
		}
	} else {
		if env.Sequence <= prev.sequence {
			return Envelope{}, ErrReplay
		}
		if env.Sequence != prev.sequence+1 {
			return Envelope{}, ErrSequence
		}
		if env.PreviousDigest != prev.digest {
			return Envelope{}, ErrPrevious
		}
	}
	if !keys[env.KeyID].Active {
		return Envelope{}, ErrKey
	}
	h.seen[id] = head{sequence: env.Sequence, digest: digest}
	return env, nil
}

func parse(canonical string) (Envelope, error) {
	dec := json.NewDecoder(strings.NewReader(canonical))
	dec.DisallowUnknownFields()
	var env Envelope
	if err := dec.Decode(&env); err != nil {
		return Envelope{}, ErrMalformed
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return Envelope{}, ErrMalformed
	}
	if env.SchemaVersion != 1 || env.Profile != "SVN-JCS-1" || env.PrivacyClass != "PUBLIC_RECORD" {
		return Envelope{}, ErrMalformed
	}
	if env.Payload.RecordType != "post" && env.Payload.RecordType != "follow" && env.Payload.RecordType != "unfollow" {
		return Envelope{}, ErrMalformed
	}
	if len(env.PreviousDigest) != 64 {
		return Envelope{}, ErrMalformed
	}
	return env, nil
}
