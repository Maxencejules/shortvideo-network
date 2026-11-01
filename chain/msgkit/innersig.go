package msgkit

import (
	"bytes"
	"crypto/ed25519"

	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// domainStrings pins the ASCII domain of every SignatureDomain value. They
// are docs/protocol/bounds.json "domains"; TestBoundsDrift compares them.
// Verifiers sign and check the string, never the enum number alone.
var domainStrings = map[typesv1.SignatureDomain]string{
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT:        "SVN-HOSTING-INTENT-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT:       "SVN-PROVIDER-ACCEPT-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW:        "SVN-PROVIDER-RENEW-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END:          "SVN-PROVIDER-END-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_CAPABILITY:            "SVN-CAPABILITY-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_MEDIA_MANIFEST:        "SVN-MEDIA-MANIFEST-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION:  "SVN-EVIDENCE-ATTESTATION-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_PUBLIC_RECORD:         "SVN-PUBLIC-RECORD-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_TRANSFER_RECEIPT:      "SVN-TRANSFER-RECEIPT-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_INVENTORY:             "SVN-INVENTORY-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN:        "SVN-PROVIDER-DRAIN-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE:       "SVN-PROVIDER-ROTATE-1",
}

// DomainString returns the pinned ASCII domain. UNSPECIFIED and unknown
// numbers return false.
func DomainString(d typesv1.SignatureDomain) (string, bool) {
	s, ok := domainStrings[d]
	return s, ok
}

// SignBytes is UTF-8(domain) || 0x00 || UTF-8(canonical), the inner
// signature preimage from protocol.md. An unknown domain returns nil; no
// verifier accepts a signature over nil.
func SignBytes(d typesv1.SignatureDomain, canonical string) []byte {
	domain, ok := DomainString(d)
	if !ok {
		return nil
	}
	out := make([]byte, 0, len(domain)+1+len(canonical))
	out = append(out, domain...)
	out = append(out, 0)
	out = append(out, canonical...)
	return out
}

// CheckShape is the structural part of an inner signature check, without
// the expected-domain comparison:
//
//  1. nil signature: MALFORMED
//  2. domain UNSPECIFIED or unknown: MALFORMED
//  3. canonical_profile_version 0: MALFORMED; any other value but 1: OUT_OF_RANGE
//  4. key_id empty: MALFORMED; outside the id pattern: BOUNDS;
//     public_key not 32 bytes: BOUNDS; signature not 64 bytes: BOUNDS
//
// The hosting-intent path uses it because it must recognise a delegate
// signature before it compares the domain.
func CheckShape(sig *typesv1.InnerSignature) Code {
	if sig == nil {
		return Malformed
	}
	if _, ok := DomainString(sig.GetDomain()); !ok {
		return Malformed
	}
	return checkShapeAfterDomain(sig)
}

// CheckInnerSignature is CheckShape with the expected domain compared at
// step 2: a known domain other than want is WRONG_DOMAIN. want itself must be
// a known domain; an unknown want is MALFORMED (a wiring error).
func CheckInnerSignature(sig *typesv1.InnerSignature, want typesv1.SignatureDomain) Code {
	if sig == nil {
		return Malformed
	}
	if _, ok := DomainString(sig.GetDomain()); !ok {
		return Malformed
	}
	if _, ok := DomainString(want); !ok {
		return Malformed
	}
	if sig.GetDomain() != want {
		return WrongDomain
	}
	return checkShapeAfterDomain(sig)
}

func checkShapeAfterDomain(sig *typesv1.InnerSignature) Code {
	switch sig.GetCanonicalProfileVersion() {
	case 0:
		return Malformed
	case CanonicalProfileVersion:
	default:
		return OutOfRange
	}
	if code := CheckID(sig.GetKeyId()); code != OK {
		return code
	}
	if len(sig.GetPublicKey()) != PublicKeyBytes {
		return Bounds
	}
	if len(sig.GetSignature()) != SignatureBytes {
		return Bounds
	}
	return OK
}

// VerifyInner runs the full inner signature order against one stored key:
// CheckInnerSignature (steps 1-4), then key_id != keyID: UNAUTHORIZED,
// public_key != pub: UNAUTHORIZED, and an ed25519 failure over
// SignBytes(want, canonical): UNAUTHORIZED. Retired keys are never passed
// here, so an old key cannot sign.
func VerifyInner(sig *typesv1.InnerSignature, want typesv1.SignatureDomain, keyID string, pub []byte, canonical string) Code {
	if code := CheckInnerSignature(sig, want); code != OK {
		return code
	}
	if sig.GetKeyId() != keyID {
		return Unauthorized
	}
	if !bytes.Equal(sig.GetPublicKey(), pub) {
		return Unauthorized
	}
	if !VerifyWithKey(pub, want, canonical, sig.GetSignature()) {
		return Unauthorized
	}
	return OK
}

// VerifyWithKey reports whether signature is a valid ed25519 signature by
// pub over SignBytes(d, canonical). Wrong key or signature sizes and unknown
// domains return false; it never panics.
func VerifyWithKey(pub []byte, d typesv1.SignatureDomain, canonical string, signature []byte) bool {
	if len(pub) != ed25519.PublicKeySize || len(signature) != ed25519.SignatureSize {
		return false
	}
	message := SignBytes(d, canonical)
	if message == nil {
		return false
	}
	return ed25519.Verify(ed25519.PublicKey(pub), message, signature)
}
