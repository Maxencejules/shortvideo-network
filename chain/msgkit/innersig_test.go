package msgkit

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"testing"

	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	domainRotate  = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE
	domainDrain   = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN
	domainHosting = typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT
)

func freshKey(t testing.TB) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return pub, priv
}

func rotateCanonical(t testing.TB, newPub []byte) string {
	t.Helper()
	text, err := Canonical(map[string]any{
		"expectedGeneration": Dec(1),
		"newSigningKeyId":    "key-2",
		"newSigningPubkey":   Hex(newPub),
		"providerId":         "provider-1",
	})
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func signInner(priv ed25519.PrivateKey, d typesv1.SignatureDomain, keyID, canonical string) *typesv1.InnerSignature {
	return &typesv1.InnerSignature{
		Domain:                  d,
		PublicKey:               append([]byte(nil), priv.Public().(ed25519.PublicKey)...),
		Signature:               ed25519.Sign(priv, SignBytes(d, canonical)),
		KeyId:                   keyID,
		CanonicalProfileVersion: CanonicalProfileVersion,
	}
}

func TestSignBytesLayout(t *testing.T) {
	got := SignBytes(domainRotate, `{"a":"b"}`)
	want := append([]byte("SVN-PROVIDER-ROTATE-1\x00"), `{"a":"b"}`...)
	if !bytes.Equal(got, want) {
		t.Fatalf("SignBytes = %q, want %q", got, want)
	}
	if SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_UNSPECIFIED, "x") != nil || SignBytes(99, "x") != nil {
		t.Fatal("unknown domain produced sign bytes")
	}
}

// Every step of the inner-signature order, one fault at a time, and the
// multi-fault cases that pin which step wins.
func TestInnerSignatureCheckOrder(t *testing.T) {
	pub, priv := freshKey(t)
	canonical := rotateCanonical(t, bytes.Repeat([]byte{7}, 32))
	base := func() *typesv1.InnerSignature { return signInner(priv, domainRotate, "key-1", canonical) }
	with := func(edit func(*typesv1.InnerSignature)) *typesv1.InnerSignature {
		s := base()
		edit(s)
		return s
	}
	cases := []struct {
		name  string
		sig   *typesv1.InnerSignature
		shape Code // CheckShape
		check Code // CheckInnerSignature(sig, rotate)
	}{
		{"valid", base(), OK, OK},
		{"nil", nil, Malformed, Malformed},
		{"domain 0", with(func(s *typesv1.InnerSignature) { s.Domain = 0 }), Malformed, Malformed},
		{"domain 13 unknown", with(func(s *typesv1.InnerSignature) { s.Domain = 13 }), Malformed, Malformed},
		{"other known domain", with(func(s *typesv1.InnerSignature) { s.Domain = domainDrain }), OK, WrongDomain},
		{"profile version 0", with(func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 0 }), Malformed, Malformed},
		{"profile version 2", with(func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 2 }), OutOfRange, OutOfRange},
		{"key id empty", with(func(s *typesv1.InnerSignature) { s.KeyId = "" }), Malformed, Malformed},
		{"key id pattern", with(func(s *typesv1.InnerSignature) { s.KeyId = "Key-1" }), Bounds, Bounds},
		{"key id 65 bytes", with(func(s *typesv1.InnerSignature) { s.KeyId = repeat('k', 65) }), Bounds, Bounds},
		{"public key empty", with(func(s *typesv1.InnerSignature) { s.PublicKey = nil }), Bounds, Bounds},
		{"public key 31", with(func(s *typesv1.InnerSignature) { s.PublicKey = s.PublicKey[:31] }), Bounds, Bounds},
		{"public key 33", with(func(s *typesv1.InnerSignature) { s.PublicKey = append(s.PublicKey, 0) }), Bounds, Bounds},
		{"signature empty", with(func(s *typesv1.InnerSignature) { s.Signature = nil }), Bounds, Bounds},
		{"signature 63", with(func(s *typesv1.InnerSignature) { s.Signature = s.Signature[:63] }), Bounds, Bounds},
		{"signature 65", with(func(s *typesv1.InnerSignature) { s.Signature = append(s.Signature, 0) }), Bounds, Bounds},
		// Order pins.
		{"unknown domain beats version 0", with(func(s *typesv1.InnerSignature) { s.Domain = 0; s.CanonicalProfileVersion = 0 }), Malformed, Malformed},
		{"wrong domain beats version 2", with(func(s *typesv1.InnerSignature) { s.Domain = domainDrain; s.CanonicalProfileVersion = 2 }), OutOfRange, WrongDomain},
		{"wrong domain beats empty key id", with(func(s *typesv1.InnerSignature) { s.Domain = domainDrain; s.KeyId = "" }), Malformed, WrongDomain},
		{"version 2 beats empty key id", with(func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 2; s.KeyId = "" }), OutOfRange, OutOfRange},
		{"version 0 beats bad public key", with(func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 0; s.PublicKey = nil }), Malformed, Malformed},
		{"empty key id beats bad public key", with(func(s *typesv1.InnerSignature) { s.KeyId = ""; s.PublicKey = nil }), Malformed, Malformed},
	}
	for _, tc := range cases {
		if got := CheckShape(tc.sig); got != tc.shape {
			t.Errorf("%s: CheckShape = %q, want %q", tc.name, got, tc.shape)
		}
		if got := CheckInnerSignature(tc.sig, domainRotate); got != tc.check {
			t.Errorf("%s: CheckInnerSignature = %q, want %q", tc.name, got, tc.check)
		}
		// VerifyInner returns the structural code first.
		if tc.check != OK {
			if got := VerifyInner(tc.sig, domainRotate, "key-1", pub, canonical); got != tc.check {
				t.Errorf("%s: VerifyInner = %q, want %q", tc.name, got, tc.check)
			}
		}
	}
	if got := CheckInnerSignature(base(), 0); got != Malformed {
		t.Errorf("unknown expected domain: %q", got)
	}
}

func TestVerifyInnerWithRealKeys(t *testing.T) {
	pub, priv := freshKey(t)
	otherPub, otherPriv := freshKey(t)
	canonical := rotateCanonical(t, otherPub)

	sig := signInner(priv, domainRotate, "key-1", canonical)
	if got := VerifyInner(sig, domainRotate, "key-1", pub, canonical); got != OK {
		t.Fatalf("valid signature: %q", got)
	}

	// Steps 5 and 6: identity and ed25519.
	if got := VerifyInner(sig, domainRotate, "key-2", pub, canonical); got != Unauthorized {
		t.Errorf("key id mismatch: %q", got)
	}
	if got := VerifyInner(sig, domainRotate, "key-1", otherPub, canonical); got != Unauthorized {
		t.Errorf("stored public key mismatch: %q", got)
	}
	// A valid signature by an attacker key that is not the stored key.
	forged := signInner(otherPriv, domainRotate, "key-1", canonical)
	if !VerifyWithKey(otherPub, domainRotate, canonical, forged.Signature) {
		t.Fatal("attacker signature should verify under the attacker key")
	}
	if got := VerifyInner(forged, domainRotate, "key-1", pub, canonical); got != Unauthorized {
		t.Errorf("signature by a key that is not stored: %q", got)
	}
	// The stored key's id and public key, but the signature of another key.
	swapped := signInner(priv, domainRotate, "key-1", canonical)
	swapped.Signature = forged.Signature
	if got := VerifyInner(swapped, domainRotate, "key-1", pub, canonical); got != Unauthorized {
		t.Errorf("signature bytes of another key: %q", got)
	}
	for _, index := range []int{0, 31, 63} {
		mutated := signInner(priv, domainRotate, "key-1", canonical)
		mutated.Signature[index] ^= 0x01
		if got := VerifyInner(mutated, domainRotate, "key-1", pub, canonical); got != Unauthorized {
			t.Errorf("mutated signature byte %d: %q", index, got)
		}
	}
	if got := VerifyInner(sig, domainRotate, "key-1", pub, canonical+" "); got != Unauthorized {
		t.Errorf("different payload: %q", got)
	}
	// A retired key: the stored key is now otherPub/key-2; a signature by
	// the old key with the old id is refused.
	if got := VerifyInner(sig, domainRotate, "key-2", otherPub, canonical); got != Unauthorized {
		t.Errorf("retired key signature: %q", got)
	}
}

// A signature made for one domain never verifies in another.
func TestCrossDomainReuse(t *testing.T) {
	pub, priv := freshKey(t)
	drainCanonical, err := Canonical(map[string]any{"keyGeneration": Dec(1), "providerId": "provider-1"})
	if err != nil {
		t.Fatal(err)
	}
	drain := signInner(priv, domainDrain, "key-1", drainCanonical)
	if got := VerifyInner(drain, domainDrain, "key-1", pub, drainCanonical); got != OK {
		t.Fatalf("drain signature in its own domain: %q", got)
	}
	// Presented where a rotate signature is expected.
	if got := VerifyInner(drain, domainRotate, "key-1", pub, drainCanonical); got != WrongDomain {
		t.Errorf("drain signature as rotate: %q, want WRONG_DOMAIN", got)
	}
	// Relabelled as rotate: the signed bytes carry the drain domain string.
	relabelled := signInner(priv, domainDrain, "key-1", drainCanonical)
	relabelled.Domain = domainRotate
	if got := VerifyInner(relabelled, domainRotate, "key-1", pub, drainCanonical); got != Unauthorized {
		t.Errorf("relabelled drain signature: %q, want UNAUTHORIZED", got)
	}
	values := typesv1.SignatureDomain(0).Descriptor().Values()
	for i := 1; i < values.Len(); i++ {
		signedIn := typesv1.SignatureDomain(values.Get(i).Number())
		signature := ed25519.Sign(priv, SignBytes(signedIn, drainCanonical))
		for j := 1; j < values.Len(); j++ {
			checkedIn := typesv1.SignatureDomain(values.Get(j).Number())
			if got := VerifyWithKey(pub, checkedIn, drainCanonical, signature); got != (i == j) {
				t.Errorf("signed in %s, checked in %s: %v", signedIn, checkedIn, got)
			}
		}
	}
}

func TestVerifyWithKeyRejectsBadSizesWithoutPanic(t *testing.T) {
	pub, priv := freshKey(t)
	signature := ed25519.Sign(priv, SignBytes(domainHosting, "{}"))
	if !VerifyWithKey(pub, domainHosting, "{}", signature) {
		t.Fatal("valid signature rejected")
	}
	for _, n := range []int{0, 31, 33, 64} {
		if VerifyWithKey(make([]byte, n), domainHosting, "{}", signature) {
			t.Errorf("public key of %d bytes verified", n)
		}
	}
	for _, n := range []int{0, 63, 65} {
		if VerifyWithKey(pub, domainHosting, "{}", make([]byte, n)) {
			t.Errorf("signature of %d bytes verified", n)
		}
	}
	if VerifyWithKey(pub, 0, "{}", signature) || VerifyWithKey(pub, 200, "{}", signature) {
		t.Error("unknown domain verified")
	}
}

// FuzzInnerSignatureCheck drives every field of an inner signature. The
// checks never panic, return only codes from the 4.4 order, and accept a
// signature only when it was constructed with the stored key over the
// expected domain and payload.
func FuzzInnerSignatureCheck(f *testing.F) {
	seed := bytes.Repeat([]byte{0x42}, ed25519.SeedSize)
	priv := ed25519.NewKeyFromSeed(seed)
	stored := priv.Public().(ed25519.PublicKey)
	f.Add(int32(12), int32(12), uint32(1), "key-1", []byte(stored), make([]byte, 64), `{"a":"b"}`, true, "key-1")
	f.Add(int32(11), int32(12), uint32(1), "key-1", []byte(stored), make([]byte, 64), `{"a":"b"}`, true, "key-1")
	f.Add(int32(12), int32(12), uint32(2), "key-1", []byte(stored), make([]byte, 64), `{}`, true, "key-1")
	f.Add(int32(0), int32(12), uint32(0), "", []byte{}, []byte{}, ``, false, "")
	f.Add(int32(1), int32(1), uint32(1), "k", make([]byte, 32), make([]byte, 64), `x`, false, "k")
	f.Add(int32(99), int32(-1), uint32(7), "Bad Key", make([]byte, 31), make([]byte, 65), "\x00\xff", false, "key-1")
	f.Fuzz(func(t *testing.T, domain, want int32, version uint32, keyID string, pub, signature []byte, canonical string, sign bool, storedKeyID string) {
		sig := &typesv1.InnerSignature{
			Domain:                  typesv1.SignatureDomain(domain),
			PublicKey:               pub,
			Signature:               signature,
			KeyId:                   keyID,
			CanonicalProfileVersion: version,
		}
		if sign {
			sig.PublicKey = append([]byte(nil), stored...)
			preimage := SignBytes(sig.Domain, canonical)
			if preimage == nil {
				preimage = []byte(canonical)
			}
			sig.Signature = ed25519.Sign(priv, preimage)
		}
		wantDomain := typesv1.SignatureDomain(want)
		shape := CheckShape(sig)
		check := CheckInnerSignature(sig, wantDomain)
		verify := VerifyInner(sig, wantDomain, storedKeyID, stored, canonical)

		allowed := map[Code]bool{OK: true, Malformed: true, WrongDomain: true, OutOfRange: true, Bounds: true, Unauthorized: true}
		if !allowed[shape] || !allowed[check] || !allowed[verify] {
			t.Fatalf("unexpected codes shape=%q check=%q verify=%q", shape, check, verify)
		}
		if shape == WrongDomain || check == Unauthorized || shape == Unauthorized {
			t.Fatalf("code from the wrong step: shape=%q check=%q", shape, check)
		}
		if check == OK && shape != OK {
			t.Fatalf("CheckInnerSignature passed but CheckShape is %q", shape)
		}
		if check != OK && verify != check {
			t.Fatalf("VerifyInner %q does not report the structural code %q", verify, check)
		}
		_, domainKnown := DomainString(sig.Domain)
		_, wantKnown := DomainString(wantDomain)
		if domainKnown && wantKnown && sig.Domain == wantDomain && shape != check {
			t.Fatalf("same domain: shape %q, check %q", shape, check)
		}
		if verify == OK {
			if !sign {
				t.Fatal("a signature that was not constructed verified")
			}
			if sig.Domain != wantDomain || version != CanonicalProfileVersion || !ValidID(keyID) || keyID != storedKeyID {
				t.Fatalf("verified with domain %d want %d version %d key %q stored %q", sig.Domain, wantDomain, version, keyID, storedKeyID)
			}
			if !VerifyWithKey(stored, wantDomain, canonical, sig.Signature) {
				t.Fatal("VerifyInner passed but VerifyWithKey fails")
			}
		}
		constructedValid := sign && domainKnown && sig.Domain == wantDomain && version == CanonicalProfileVersion &&
			ValidID(keyID) && keyID == storedKeyID
		if constructedValid && verify != OK {
			t.Fatalf("constructed signature rejected: %q", verify)
		}
		// VerifyWithKey tolerates any input.
		_ = VerifyWithKey(pub, sig.Domain, canonical, signature)
	})
}
