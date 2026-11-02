package difftest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Step op names: the spec op of a transaction, or a fixture name.
const (
	opOffer            = "OfferLease"
	opAccept           = "AcceptLease"
	opRenew            = "RenewLease"
	opEnd              = "EndLease"
	opRegisterProvider = "RegisterProvider"
	opUpdateProvider   = "UpdateProvider"
	opRotate           = "RotateProviderKey"
	opDrain            = "DrainProvider"
	opRegisterContent  = "RegisterContent"
	opWithdraw         = "WithdrawContent"
	fxBlock            = "Block"
	fxAdmit            = "CouncilAdmit"
	fxDisable          = "CouncilDisable"
	fxForceWithdraw    = "CouncilForceWithdraw"
	fxSeed             = "SeedProvider"
)

// hugeInt is above 2^53-1: the spec machine cannot canonicalize it in a
// business payload, so the business gate is MALFORMED on both sides.
const hugeInt uint64 = 1 << 60

// unixMax is the largest protocol time (bounds.json unix_seconds_max).
const unixMax int64 = 253402300799

// Key ids name exactly one key: the key is derived from the id. No private
// key is written anywhere; nothing here is a real key.
const (
	serviceKeyID = "svc-k1"
	intentKeyID  = "intent-k1"
)

func keyFor(keyID string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("svn-difftest-key:" + keyID))
	return ed25519.NewKeyFromSeed(seed[:])
}

func pubFor(keyID string) []byte { return []byte(keyFor(keyID).Public().(ed25519.PublicKey)) }

func sha(label string) []byte {
	sum := sha256.Sum256([]byte(label))
	return sum[:]
}

func digest(value []byte) *typesv1.Digest {
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: append([]byte(nil), value...)}
}

func manifestFor(contentID string) []byte  { return sha("manifest:" + contentID) }
func endpointFor(providerID string) []byte { return sha("endpoint:" + providerID) }
func reasonFor(contentID string) []byte    { return sha("reason:" + contentID) }
func termsFor(n int) []byte                { return sha(fmt.Sprintf("terms:v%d", n)) }

func domainName(d typesv1.SignatureDomain) string {
	name, ok := msgkit.DomainString(d)
	if !ok {
		panic(fmt.Sprintf("difftest: unknown domain %v", d))
	}
	return name
}

// sign signs signBytes (which already start with the domain string and
// 0x00) with keyID's key and names keyID.
func sign(keyID string, domain typesv1.SignatureDomain, signBytes []byte) *typesv1.InnerSignature {
	return &typesv1.InnerSignature{
		Domain:                  domain,
		PublicKey:               pubFor(keyID),
		Signature:               ed25519.Sign(keyFor(keyID), signBytes),
		KeyId:                   keyID,
		CanonicalProfileVersion: 1,
	}
}

// redomain moves sign bytes to another domain: the canonical text after the
// first 0x00 is kept.
func redomain(signBytes []byte, to typesv1.SignatureDomain) []byte {
	cut := bytes.IndexByte(signBytes, 0x00)
	if cut < 0 {
		panic("difftest: sign bytes without a domain separator")
	}
	return msgkit.SignBytes(to, string(signBytes[cut+1:]))
}

// abstractSig is the spec machine's view of an inner signature: its domain,
// the key id it names, and whether it is valid, meaning it verifies under
// the public key it carries, over the payload the chain checks, and that
// public key is the key its key id names.
func abstractSig(domain typesv1.SignatureDomain, keyID string, valid bool) map[string]any {
	return map[string]any{"domain": domainName(domain), "key_id": keyID, "valid": valid}
}

// intentSpec is the hosting intent of every content step: the devnet role,
// a valid signature by the intent key.
func intentSpec() map[string]any {
	return map[string]any{
		"domain":    domainName(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT),
		"key_id":    intentKeyID,
		"valid":     true,
		"role_name": content.DevnetRoleName,
	}
}

// step is one generated step: a transaction message with the spec dict the
// oracle receives, or a fixture.
type step struct {
	op string
	// tag is the generator variant (for example a signature variant). It
	// only labels coverage classes; it never decides an expected outcome.
	tag  string
	msg  proto.Message
	spec map[string]any
	// headerTime (Block), id, version, reason (council fixtures, SeedProvider).
	headerTime int64
	id         string
	version    uint64
	reason     []byte
	// invalidSig marks an acceptance whose signature does not verify.
	// Deviation D-1 (the chain verifies after the business gate and the
	// offer lookup) means such an acceptance is generated only with a fresh
	// business id and an open offer, and is never retried.
	invalidSig bool
}

// comparesEvents is false for council fixtures: the spec's
// apply_proposal_payload emits nothing, x/providers and x/content do.
func (s *step) comparesEvents() bool {
	switch s.op {
	case fxAdmit, fxDisable, fxForceWithdraw:
		return false
	}
	return true
}

func (s *step) businessID() string {
	if id, ok := s.spec["business_id"].(string); ok {
		return id
	}
	return ""
}

type offerParams struct {
	signer                     []byte
	business, offerID          string
	contentID, providerID      string
	version, size, termVersion uint64
	manifest, terms            []byte
	start, deadline, expiry    int64
}

func buildOffer(p offerParams, tag string) *step {
	msg := &leasesv1.MsgOfferLease{
		Signer: p.signer, BusinessId: p.business, OfferId: p.offerID, ContentId: p.contentID,
		ContentVersion: p.version, ProviderId: p.providerID, ManifestDigest: digest(p.manifest),
		PackageBytes: p.size, TermsDigest: digest(p.terms), StartUnixSeconds: p.start,
		DeadlineUnixSeconds: p.deadline, ExpiryUnixSeconds: p.expiry, TermVersion: p.termVersion,
	}
	return &step{op: opOffer, tag: tag, msg: msg, spec: map[string]any{
		"op": opOffer, "signer": hexOf(p.signer), "business_id": p.business, "offer_id": p.offerID,
		"content_id": p.contentID, "content_version": p.version, "provider_id": p.providerID,
		"package_bytes": p.size, "manifest_digest": hexOf(p.manifest), "terms_digest": hexOf(p.terms),
		"start_unix_seconds": p.start, "deadline_unix_seconds": p.deadline,
		"expiry_unix_seconds": p.expiry, "term_version": p.termVersion,
	}}
}

// signed is a real inner signature with its spec view.
type signed struct {
	sig      *typesv1.InnerSignature
	abstract map[string]any
	invalid  bool
	variant  string
}

func buildAccept(signer []byte, business, offerID, leaseID string, s signed) *step {
	msg := &leasesv1.MsgAcceptLease{
		Signer: signer, BusinessId: business, OfferId: offerID, LeaseId: leaseID, ProviderAcceptance: s.sig,
	}
	return &step{op: opAccept, tag: s.variant, msg: msg, invalidSig: s.invalid, spec: map[string]any{
		"op": opAccept, "signer": hexOf(signer), "business_id": business, "offer_id": offerID,
		"lease_id": leaseID, "provider_acceptance": s.abstract,
	}}
}

type renewParams struct {
	signer            []byte
	business, leaseID string
	termVersion, size uint64
	deadline          int64
	manifest          []byte
	evidenceID        string
}

func (p renewParams) signBytes() []byte {
	out, err := leases.RenewSignBytes(p.leaseID, p.termVersion, p.deadline, digest(p.manifest), p.size, p.evidenceID)
	if err != nil {
		panic(fmt.Sprintf("difftest: renew sign bytes: %v", err))
	}
	return out
}

func buildRenew(p renewParams, s signed, tag string) *step {
	msg := &leasesv1.MsgRenewLease{
		Signer: p.signer, BusinessId: p.business, LeaseId: p.leaseID, TermVersion: p.termVersion,
		DeadlineUnixSeconds: p.deadline, ManifestDigest: digest(p.manifest), PackageBytes: p.size,
		ProviderSignature: s.sig, EvidenceId: p.evidenceID,
	}
	spec := map[string]any{
		"op": opRenew, "signer": hexOf(p.signer), "business_id": p.business, "lease_id": p.leaseID,
		"term_version": p.termVersion, "deadline_unix_seconds": p.deadline, "package_bytes": p.size,
		"manifest_digest": hexOf(p.manifest), "provider_signature": s.abstract,
	}
	if p.evidenceID != "" {
		spec["evidence_id"] = p.evidenceID
	}
	if tag == "" {
		tag = s.variant
	}
	return &step{op: opRenew, tag: tag, msg: msg, spec: spec}
}

// buildEnd builds an EndLease. s.sig nil means no provider signature.
func buildEnd(signer []byte, business, leaseID string, authorityEnd bool, s signed) *step {
	msg := &leasesv1.MsgEndLease{
		Signer: signer, BusinessId: business, LeaseId: leaseID, ProviderSignature: s.sig, AuthorityEnd: authorityEnd,
	}
	spec := map[string]any{
		"op": opEnd, "signer": hexOf(signer), "business_id": business, "lease_id": leaseID,
		"authority_end": authorityEnd,
	}
	if s.sig != nil {
		spec["provider_signature"] = s.abstract
	}
	return &step{op: opEnd, tag: s.variant, msg: msg, spec: spec}
}

func buildRegisterProvider(signer []byte, business, providerID, keyID string, capacity uint64) *step {
	msg := &providersv1.MsgRegisterProvider{
		Signer: signer, BusinessId: business, ProviderId: providerID, SigningPubkey: pubFor(keyID),
		SigningKeyId: keyID, EndpointManifestDigest: digest(endpointFor(providerID)),
		DeclaredCapacityBytes: capacity, OperatorGroupId: providers.DevnetOperatorGroup,
	}
	return &step{op: opRegisterProvider, msg: msg, spec: map[string]any{
		"op": opRegisterProvider, "signer": hexOf(signer), "business_id": business, "provider_id": providerID,
		"signing_key_id": keyID, "signing_pubkey": hexOf(pubFor(keyID)),
		"endpoint_manifest_digest": hexOf(endpointFor(providerID)), "declared_capacity_bytes": capacity,
		"operator_group_id": providers.DevnetOperatorGroup,
	}}
}

func buildUpdateProvider(signer []byte, business, providerID string, capacity uint64, endpoint []byte) *step {
	msg := &providersv1.MsgUpdateProvider{
		Signer: signer, BusinessId: business, ProviderId: providerID,
		DeclaredCapacityBytes: capacity, EndpointManifestDigest: digest(endpoint),
	}
	return &step{op: opUpdateProvider, msg: msg, spec: map[string]any{
		"op": opUpdateProvider, "signer": hexOf(signer), "business_id": business, "provider_id": providerID,
		"declared_capacity_bytes": capacity, "endpoint_manifest_digest": hexOf(endpoint),
	}}
}

// buildRotate signs the rotation with currentKeyID.
func buildRotate(signer []byte, business, providerID, currentKeyID, newKeyID string, newPub []byte, expected uint64) *step {
	signBytes, err := providers.RotateSignBytes(providerID, newKeyID, newPub, expected)
	if err != nil {
		panic(fmt.Sprintf("difftest: rotate sign bytes: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE
	msg := &providersv1.MsgRotateProviderKey{
		Signer: signer, BusinessId: business, ProviderId: providerID, NewSigningPubkey: newPub,
		NewSigningKeyId: newKeyID, ExpectedGeneration: expected, CurrentKeySignature: sign(currentKeyID, domain, signBytes),
	}
	return &step{op: opRotate, msg: msg, spec: map[string]any{
		"op": opRotate, "signer": hexOf(signer), "business_id": business, "provider_id": providerID,
		"new_signing_key_id": newKeyID, "new_signing_pubkey": hexOf(newPub), "expected_generation": expected,
		"current_key_signature": abstractSig(domain, currentKeyID, true),
	}}
}

// buildDrain signs the drain with currentKeyID at generation.
func buildDrain(signer []byte, business, providerID, currentKeyID string, generation uint64) *step {
	signBytes, err := providers.DrainSignBytes(providerID, generation)
	if err != nil {
		panic(fmt.Sprintf("difftest: drain sign bytes: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN
	msg := &providersv1.MsgDrainProvider{
		Signer: signer, BusinessId: business, ProviderId: providerID, ProviderSignature: sign(currentKeyID, domain, signBytes),
	}
	return &step{op: opDrain, msg: msg, spec: map[string]any{
		"op": opDrain, "signer": hexOf(signer), "business_id": business, "provider_id": providerID,
		"provider_signature": abstractSig(domain, currentKeyID, true),
	}}
}

func buildRegisterContent(signer []byte, business, contentID string, size uint64) *step {
	manifest := manifestFor(contentID)
	signBytes, err := content.RegisterIntentSignBytes(signer, contentID, 1, manifest, size, content.DevnetRoleName)
	if err != nil {
		panic(fmt.Sprintf("difftest: register intent: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT
	msg := &contentv1.MsgRegisterContent{
		Signer: signer, BusinessId: business, ContentId: contentID, ContentVersion: 1, PackageBytes: size,
		ManifestDigest: digest(manifest), HostingIntent: sign(intentKeyID, domain, signBytes),
	}
	return &step{op: opRegisterContent, msg: msg, spec: map[string]any{
		"op": opRegisterContent, "signer": hexOf(signer), "business_id": business, "content_id": contentID,
		"content_version": 1, "package_bytes": size, "manifest_digest": hexOf(manifest), "hosting_intent": intentSpec(),
	}}
}

func buildWithdraw(signer []byte, business, contentID string, version uint64) *step {
	reason := reasonFor(contentID)
	signBytes, err := content.WithdrawIntentSignBytes(signer, contentID, version, reason, content.DevnetRoleName)
	if err != nil {
		panic(fmt.Sprintf("difftest: withdraw intent: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT
	msg := &contentv1.MsgWithdrawContent{
		Signer: signer, BusinessId: business, ContentId: contentID, ContentVersion: version,
		ReasonDigest: digest(reason), HostingIntent: sign(intentKeyID, domain, signBytes),
	}
	return &step{op: opWithdraw, msg: msg, spec: map[string]any{
		"op": opWithdraw, "signer": hexOf(signer), "business_id": business, "content_id": contentID,
		"content_version": version, "hosting_intent": intentSpec(), "reason_digest": hexOf(reason),
	}}
}

func blockStep(headerTime int64) *step {
	return &step{op: fxBlock, headerTime: headerTime, spec: map[string]any{"op": fxBlock, "header_time": headerTime}}
}

func admitStep(providerID string) *step {
	return &step{op: fxAdmit, id: providerID, spec: map[string]any{"op": fxAdmit, "provider_id": providerID}}
}

func disableStep(providerID string) *step {
	return &step{op: fxDisable, id: providerID, spec: map[string]any{"op": fxDisable, "provider_id": providerID}}
}

func forceWithdrawStep(contentID string, version uint64) *step {
	reason := reasonFor(contentID)
	return &step{op: fxForceWithdraw, id: contentID, version: version, reason: reason, spec: map[string]any{
		"op": fxForceWithdraw, "content_id": contentID, "content_version": version, "reason_digest": hexOf(reason),
	}}
}

// seedRecord is an ADMITTED provider written at genesis (SeedProvider).
func seedRecord(providerID string, acct []byte, capacity, reserved uint64) *providersv1.Provider {
	keyID := providerID + "-k1"
	return &providersv1.Provider{
		ProviderId: providerID, Account: acct, SigningPubkey: pubFor(keyID), SigningKeyId: keyID,
		KeyGeneration: 1, EndpointManifestDigest: digest(endpointFor(providerID)),
		State: providersv1.ProviderState_PROVIDER_STATE_ADMITTED, AdmittedCapacityBytes: capacity,
		ReservedBytes: reserved, OperatorGroupId: providers.DevnetOperatorGroup,
	}
}

func seedStep(r *providersv1.Provider) *step {
	return &step{op: fxSeed, id: r.GetProviderId(), spec: map[string]any{
		"op": fxSeed, "provider_id": r.GetProviderId(), "account": hexOf(r.GetAccount()),
		"signing_key_id": r.GetSigningKeyId(), "signing_pubkey": hexOf(r.GetSigningPubkey()),
		"endpoint_manifest_digest": hexOf(r.GetEndpointManifestDigest().GetValue()), "state": "ADMITTED",
		"admitted_capacity_bytes": r.GetAdmittedCapacityBytes(), "reserved_bytes": r.GetReservedBytes(),
		"operator_group_id": r.GetOperatorGroupId(),
	}}
}

// Signature variants of a provider-signed message.
const (
	sigValid            = "valid"
	sigRetired          = "retired key"
	sigOtherProvider    = "other provider key"
	sigServiceOwn       = "service key, own key id"
	sigServiceAsProvide = "service key, provider key id"
	sigMutated          = "mutated terms"
	sigWrongDomain      = "wrong domain"
)

// providerSig signs signBytes in domain as variant asks. currentKeyID is
// the provider's current key; retiredKeyID and otherKeyID may be "" (the
// variant then falls back to valid). mutated is the sign bytes of a
// changed payload, signed by the current key for sigMutated. wrong is the
// domain of sigWrongDomain.
func providerSig(variant string, domain, wrong typesv1.SignatureDomain, signBytes, mutated []byte,
	currentKeyID, retiredKeyID, otherKeyID string) signed {
	switch {
	case variant == sigRetired && retiredKeyID != "":
		return signed{sig: sign(retiredKeyID, domain, signBytes), abstract: abstractSig(domain, retiredKeyID, true), variant: variant}
	case variant == sigOtherProvider && otherKeyID != "":
		return signed{sig: sign(otherKeyID, domain, signBytes), abstract: abstractSig(domain, otherKeyID, true), variant: variant}
	case variant == sigServiceOwn:
		return signed{sig: sign(serviceKeyID, domain, signBytes), abstract: abstractSig(domain, serviceKeyID, true), variant: variant}
	case variant == sigServiceAsProvide:
		sig := sign(serviceKeyID, domain, signBytes)
		sig.KeyId = currentKeyID
		return signed{sig: sig, abstract: abstractSig(domain, currentKeyID, false), invalid: true, variant: variant}
	case variant == sigMutated:
		return signed{sig: sign(currentKeyID, domain, mutated), abstract: abstractSig(domain, currentKeyID, false), invalid: true, variant: variant}
	case variant == sigWrongDomain:
		return signed{sig: sign(currentKeyID, wrong, redomain(signBytes, wrong)), abstract: abstractSig(wrong, currentKeyID, true), variant: variant}
	}
	return signed{sig: sign(currentKeyID, domain, signBytes), abstract: abstractSig(domain, currentKeyID, true), variant: sigValid}
}

// checkAbstract recomputes the abstract validity of a signature: it
// verifies under the public key it carries over expected (moved to the
// signature's own domain), and that key is the one its key id names. A
// generator bug that labels a signature wrongly fails the test here rather
// than as a spurious divergence.
func checkAbstract(s signed, expected []byte) error {
	if s.sig == nil {
		return nil
	}
	valid := bytes.Equal(pubFor(s.sig.GetKeyId()), s.sig.GetPublicKey()) &&
		ed25519.Verify(s.sig.GetPublicKey(), redomain(expected, s.sig.GetDomain()), s.sig.GetSignature())
	if valid != s.abstract["valid"] {
		return fmt.Errorf("signature variant %q: abstract valid %v, recomputed %v", s.variant, s.abstract["valid"], valid)
	}
	return nil
}

// acceptOffer returns the offer an acceptance signs: the stored offer, or a
// synthetic one for an id that names no open offer (the chain answers
// NOT_FOUND before it reads the signature, deviation D-1).
func acceptOffer(v *view, offerID, providerID string) *leasesv1.Offer {
	if o, ok := v.offers[offerID]; ok {
		return o
	}
	return &leasesv1.Offer{
		OfferId: offerID, ContentId: "c-none", ContentVersion: 1, ProviderId: providerID,
		ManifestDigest: digest(manifestFor("c-none")), PackageBytes: 1, TermsDigest: digest(termsFor(1)),
		StartUnixSeconds: v.now, DeadlineUnixSeconds: v.now + termSeconds, ExpiryUnixSeconds: v.now + 1,
		TermVersion: 1, Scheduler: serviceAcct,
	}
}

func acceptSignBytes(offer *leasesv1.Offer, leaseID string) []byte {
	out, err := leases.AcceptSignBytes(offer, leaseID)
	if err != nil {
		panic(fmt.Sprintf("difftest: accept sign bytes: %v", err))
	}
	return out
}

func endSignBytes(leaseID, providerID string) []byte {
	out, err := leases.EndSignBytes(leaseID, providerID)
	if err != nil {
		panic(fmt.Sprintf("difftest: end sign bytes: %v", err))
	}
	return out
}
