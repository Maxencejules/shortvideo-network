package evidence

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Auditor inner-signature payload (owner ruling on Q2, "Envelopes + id
// prefix (Recommended)"; DR-09-02). The signed bytes are
//
//	UTF-8("SVN-EVIDENCE-ATTESTATION-1") || 0x00 || UTF-8(SVN-JCS-1 canonical JSON)
//
// (msgkit.SignBytes) of the batch object
//
//	{"batch_id": <msg.batch_id>,
//	 "envelopes": [<one svn-evidence-envelope-v1 object per item, in item order>],
//	 "signer": <lowercase hex of the 20-byte outer signer>}
//
// where the envelope of item i is the frozen 13-field object of
// docs/protocol/schemas/evidence-envelope.schema.json:
//
//	{"attestation": item.attestation, "claim_limit": "ATTESTATION_NOT_STORAGE_PROOF",
//	 "content_id": item.content_id, "content_version": item.content_version,
//	 "epoch": msg.epoch, "evidence_id": item.evidence_id,
//	 "expiry_unix_seconds": item.expiry_unix_seconds, "kind": KindName(item.kind),
//	 "lease_id": item.lease_id, "observation_sha256": <lowercase hex of the digest value>,
//	 "profile": "SVN-JCS-1", "schema_version": msg.schema_version,
//	 "signer_key_id": auditor_signature.key_id}
//
// Integers are raw JSON integers. The object binds every attested field of
// every item, the item order, the batch id, the epoch, the schema version,
// the key label, and the outer signer: any change after signing makes the
// signature fail (deviation D-1). v0 defines one envelope per item and one
// signature per batch, but no batch wrapper; the wrapper is the deviation
// from docs/protocol.md:95. No chain id is signed (the owner's Q2 ruling;
// the outer transaction signature binds the chain, as DR-07-01 and DR-08-04
// for the other inner domains) and no business id (a retry handle, not
// attested content).
//
// Under the owner's ruling on Q1 ("Account key signs (Recommended)") the
// signature's public key must derive the outer signer's address
// (AuditorAddress), so the auditor's ed25519 account key, which also signs
// its SDK transactions, signs attestations. The app enables only
// SIGN_MODE_DIRECT, whose preimage is a protobuf SignDoc beginning with a
// field tag (0x0a, 0x12, 0x1a, or 0x20); this preimage begins with "S"
// (0x53), so the two preimage sets are disjoint
// (TestAttestationPreimageIsNotASignDoc). That separation argument is
// unreviewed; the ruling limits it to the devnet beta.

const (
	// ClaimLimit is the envelope's fixed claim_limit.
	ClaimLimit = "ATTESTATION_NOT_STORAGE_PROOF"
	// Profile is the envelope's fixed canonical profile name.
	Profile = "SVN-JCS-1"
	// maxCanonicalBytes is the SVN-JCS-1 output cap.
	maxCanonicalBytes = 65536
)

// ErrNoCanonicalForm marks a message whose attestation payload has no
// SVN-JCS-1 form: a nil message, item, or signature, an observation digest
// that is not a 32-byte SHA-256 digest, an integer outside
// [-(2^53-1), 2^53-1], or a signed string (batch_id, evidence_id, lease_id,
// content_id, key_id) that is not valid UTF-8. No signature can verify over
// it (UNAUTHORIZED).
//
// The UTF-8 rule keeps the payload injective: the SVN-JCS-1 encoder writes
// every invalid byte as U+FFFD, so without it two messages whose ids differ
// only in invalid bytes would share one payload and one signature. A
// transaction never carries such a string (protobuf-go refuses invalid UTF-8
// in a proto3 string, and step 9 refuses it), but VerifyAuditSignature is
// also the producer's and the reviewer's check.
var ErrNoCanonicalForm = errors.New("evidence: attestation payload has no SVN-JCS-1 form")

// ErrPayloadTooLarge marks a message whose attestation payload exceeds the
// 65536-byte SVN-JCS-1 cap (BOUNDS, deviation D-6).
var ErrPayloadTooLarge = errors.New("evidence: attestation payload above 65536 canonical bytes")

// KindName is the one spelling of an evidence kind used in the signed
// envelope, the business payload, and events: the enum value name without
// the EVIDENCE_KIND_ prefix for a named value (UNSPECIFIED,
// PROVIDER_RECEIPT, AUDITOR_ATTESTATION), and the decimal number for any
// other value (3, 4, and 5 are reserved numbers with no Go name).
func KindName(kind evidencev1.EvidenceKind) string {
	if name, ok := evidencev1.EvidenceKind_name[int32(kind)]; ok {
		return strings.TrimPrefix(name, "EVIDENCE_KIND_")
	}
	return strconv.FormatInt(int64(kind), 10)
}

// AuditorAddress is the 20-byte account address of an ed25519 public key:
// the first 20 bytes of SHA-256(pub), as the SDK's ed25519 PubKey.Address
// computes it. A key that is not 32 bytes returns nil.
func AuditorAddress(pub []byte) []byte {
	if len(pub) != msgkit.PublicKeyBytes {
		return nil
	}
	sum := sha256.Sum256(pub)
	return append([]byte(nil), sum[:msgkit.AccountBytes]...)
}

// IDPrefix is the prefix every evidence id and batch id of the auditor at
// addr must start with (deviation D-17): the lowercase hex of the first
// IDPrefixBytes bytes of the 20-byte address, then ".". Any other length
// returns "".
func IDPrefix(addr []byte) string {
	if len(addr) != msgkit.AccountBytes {
		return ""
	}
	return hex.EncodeToString(addr[:IDPrefixBytes]) + "."
}

// safeUint converts an unsigned integer of the payload to int64, or reports
// that it has no SVN-JCS-1 form (above 2^53-1).
func safeUint(v uint64) (int64, bool) {
	if v > uint64(genesis.SafeIntegerMax) {
		return 0, false
	}
	return int64(v), true
}

// safeInt reports whether a signed integer of the payload has an SVN-JCS-1
// form (within [-(2^53-1), 2^53-1]).
func safeInt(v int64) bool { return v >= -genesis.SafeIntegerMax && v <= genesis.SafeIntegerMax }

// AuditEnvelope is the svn-evidence-envelope-v1 object of item i of msg, as
// signed. It returns an error wrapping ErrNoCanonicalForm for a nil message,
// an index out of range, a nil item or signature, an evidence_id, lease_id,
// content_id, or key_id that is not valid UTF-8, an observation digest that
// is not a 32-byte SHA-256 digest, or a content_version, epoch, or expiry
// outside the safe integer range. Any other value builds an envelope
// (kinds 0 and 3-5, attestation false, content_version 0, a negative expiry):
// the handler rejects those values at their own checks. The producer writes
// the SVN-JCS-1 text of this object as the item's envelope file.
func AuditEnvelope(msg *evidencev1.MsgSubmitAuditBatch, i int) (map[string]any, error) {
	if msg == nil {
		return nil, fmt.Errorf("%w: nil message", ErrNoCanonicalForm)
	}
	items := msg.GetItems()
	if i < 0 || i >= len(items) {
		return nil, fmt.Errorf("%w: item %d of %d", ErrNoCanonicalForm, i, len(items))
	}
	item := items[i]
	if item == nil {
		return nil, fmt.Errorf("%w: item %d is nil", ErrNoCanonicalForm, i)
	}
	sig := msg.GetAuditorSignature()
	if sig == nil {
		return nil, fmt.Errorf("%w: no auditor signature", ErrNoCanonicalForm)
	}
	for _, field := range []struct{ name, value string }{
		{"evidence_id", item.GetEvidenceId()}, {"lease_id", item.GetLeaseId()},
		{"content_id", item.GetContentId()}, {"key_id", sig.GetKeyId()},
	} {
		if !utf8.ValidString(field.value) {
			return nil, fmt.Errorf("%w: item %d %s is not UTF-8", ErrNoCanonicalForm, i, field.name)
		}
	}
	observation, code := msgkit.CheckDigest(item.GetObservationDigest())
	if code != msgkit.OK {
		return nil, fmt.Errorf("%w: item %d observation digest: %s", ErrNoCanonicalForm, i, code)
	}
	contentVersion, ok := safeUint(item.GetContentVersion())
	if !ok {
		return nil, fmt.Errorf("%w: item %d content_version %d", ErrNoCanonicalForm, i, item.GetContentVersion())
	}
	epoch, ok := safeUint(msg.GetEpoch())
	if !ok {
		return nil, fmt.Errorf("%w: epoch %d", ErrNoCanonicalForm, msg.GetEpoch())
	}
	if !safeInt(item.GetExpiryUnixSeconds()) {
		return nil, fmt.Errorf("%w: item %d expiry %d", ErrNoCanonicalForm, i, item.GetExpiryUnixSeconds())
	}
	return map[string]any{
		"attestation":         item.GetAttestation(),
		"claim_limit":         ClaimLimit,
		"content_id":          item.GetContentId(),
		"content_version":     contentVersion,
		"epoch":               epoch,
		"evidence_id":         item.GetEvidenceId(),
		"expiry_unix_seconds": item.GetExpiryUnixSeconds(),
		"kind":                KindName(item.GetKind()),
		"lease_id":            item.GetLeaseId(),
		"observation_sha256":  msgkit.Hex(observation),
		"profile":             Profile,
		"schema_version":      int64(msg.GetSchemaVersion()),
		"signer_key_id":       sig.GetKeyId(),
	}, nil
}

// AuditCanonical is the SVN-JCS-1 text of the batch object of msg.
//
// A batch_id that is not valid UTF-8 returns ErrNoCanonicalForm. The
// envelopes are built in item order. The first envelope that has no
// SVN-JCS-1 form returns its ErrNoCanonicalForm error. Each envelope is
// canonicalized as it is built and the running length is checked against
// the 65536-byte cap, so the work stops after at most about 65536 bytes of
// envelopes; a payload above the cap returns an error wrapping
// ErrPayloadTooLarge. Every value left after the envelope checks is a
// string, a bool, a safe int64, or an object or array of depth at most 3
// with fixed keys, so the cap is the only remaining SVN-JCS-1 failure.
func AuditCanonical(msg *evidencev1.MsgSubmitAuditBatch) (string, error) {
	if msg == nil {
		return "", fmt.Errorf("%w: nil message", ErrNoCanonicalForm)
	}
	if !utf8.ValidString(msg.GetBatchId()) {
		return "", fmt.Errorf("%w: batch_id is not UTF-8", ErrNoCanonicalForm)
	}
	items := msg.GetItems()
	envelopes := make([]any, 0, len(items))
	total := 0
	for i := range items {
		envelope, err := AuditEnvelope(msg, i)
		if err != nil {
			return "", err
		}
		text, err := msgkit.Canonical(envelope)
		if err != nil {
			return "", fmt.Errorf("%w: envelope %d: %v", ErrPayloadTooLarge, i, err)
		}
		total += len(text) + 1
		if total > maxCanonicalBytes {
			return "", fmt.Errorf("%w: %d envelope bytes after item %d", ErrPayloadTooLarge, total, i)
		}
		envelopes = append(envelopes, envelope)
	}
	text, err := msgkit.Canonical(map[string]any{
		"batch_id":  msg.GetBatchId(),
		"envelopes": envelopes,
		"signer":    msgkit.Hex(msg.GetSigner()),
	})
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrPayloadTooLarge, err)
	}
	return text, nil
}

// AuditSignBytes is the preimage the auditor's key signs for msg: the
// SVN-EVIDENCE-ATTESTATION-1 domain, 0x00, and AuditCanonical(msg). The
// producer, the reviewer, and tests use it; the handler verifies against
// it. Errors are those of AuditCanonical.
func AuditSignBytes(msg *evidencev1.MsgSubmitAuditBatch) ([]byte, error) {
	canonical, err := AuditCanonical(msg)
	if err != nil {
		return nil, err
	}
	return msgkit.SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION, canonical), nil
}

// VerifyAuditSignature is steps 6-8 of the SubmitAuditBatch check order. It
// is pure (no store access), so the producer and the reviewer run the same
// checks as the chain:
//
//  6. msgkit.CheckInnerSignature(sig, EVIDENCE_ATTESTATION): nil signature,
//     domain 0 or unknown MALFORMED; another known domain WRONG_DOMAIN;
//     profile 0 MALFORMED, not 1 OUT_OF_RANGE; key_id empty MALFORMED, off
//     the id pattern BOUNDS; public key not 32 bytes, signature not 64
//     bytes BOUNDS
//  7. AuditorAddress(public key) != outer signer: UNAUTHORIZED (D-2, the
//     Q1 key binding; key_id is a label checked only for the id pattern)
//  8. payload: no SVN-JCS-1 form (ErrNoCanonicalForm, invalid UTF-8
//     included) UNAUTHORIZED; above 65536 canonical bytes
//     BOUNDS (D-6); ed25519 over AuditSignBytes(msg) fails UNAUTHORIZED
//     (D-1: any field changed after signing)
func VerifyAuditSignature(msg *evidencev1.MsgSubmitAuditBatch) msgkit.Code {
	sig := msg.GetAuditorSignature()
	if code := msgkit.CheckInnerSignature(sig, typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION); code != msgkit.OK {
		return code
	}
	address := AuditorAddress(sig.GetPublicKey())
	if address == nil || !bytes.Equal(address, msg.GetSigner()) {
		return msgkit.Unauthorized
	}
	canonical, err := AuditCanonical(msg)
	if err != nil {
		if errors.Is(err, ErrPayloadTooLarge) {
			return msgkit.Bounds
		}
		return msgkit.Unauthorized
	}
	if !msgkit.VerifyWithKey(sig.GetPublicKey(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION, canonical, sig.GetSignature()) {
		return msgkit.Unauthorized
	}
	return msgkit.OK
}
