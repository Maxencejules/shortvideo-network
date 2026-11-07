package difftest

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Step op names: the spec op of a transaction, or a fixture name.
const (
	opAudit            = "SubmitAuditBatch"
	opOffer            = "OfferLease"
	opAccept           = "AcceptLease"
	opRenew            = "RenewLease"
	opEnd              = "EndLease"
	opRegisterProvider = "RegisterProvider"
	opRegisterContent  = "RegisterContent"
	opWithdraw         = "WithdrawContent"
	fxBlock            = "Block"
	fxAdmit            = "CouncilAdmit"
	fxGoverned         = "GovernedLimit"
)

// unixMax is the largest protocol time (bounds.json unix_seconds_max).
const unixMax int64 = 253402300799

// Key labels name exactly one key: the key is derived from the label. No
// private key is written anywhere; nothing here is a real key.
const (
	auditorKeyA = "auditor-a"
	auditorKeyB = "auditor-b"
	// auditorKeyID is the key_id label every auditor signature carries. On
	// the chain it is a label checked only for the id pattern (deviation
	// D-2); it is also spec_machine.py's one auditor key id (AUDITOR_KEY),
	// so the abstract signature of a valid auditor signature names it.
	auditorKeyID = "key-auditor"
	intentKeyID  = "intent-k1"
)

func keyFor(label string) ed25519.PrivateKey {
	seed := sha256.Sum256([]byte("svn-difftest-key:" + label))
	return ed25519.NewKeyFromSeed(seed[:])
}

func pubFor(label string) []byte { return []byte(keyFor(label).Public().(ed25519.PublicKey)) }

// auditorKeyOf is the key label of an auditor account, or "" for any other
// account.
func auditorKeyOf(addr []byte) string {
	switch {
	case bytes.Equal(addr, auditorA):
		return auditorKeyA
	case bytes.Equal(addr, auditorB):
		return auditorKeyB
	}
	return ""
}

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

// Signature domain strings (bounds.json domains), held by the harness so
// its own encoding does not use the chain's.
var domainStrings = map[typesv1.SignatureDomain]string{
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION: "SVN-EVIDENCE-ATTESTATION-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT:      "SVN-PROVIDER-ACCEPT-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW:       "SVN-PROVIDER-RENEW-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END:         "SVN-PROVIDER-END-1",
	typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT:       "SVN-HOSTING-INTENT-1",
}

func domainName(d typesv1.SignatureDomain) string {
	name, ok := domainStrings[d]
	if !ok {
		panic(fmt.Sprintf("difftest: unknown domain %v", d))
	}
	return name
}

// signBytes is UTF-8(domain) || 0x00 || text.
func signBytes(d typesv1.SignatureDomain, text string) []byte {
	return append(append([]byte(domainName(d)), 0x00), text...)
}

// signWith signs a lease or content payload (sign bytes that already start
// with the domain string and 0x00) with keyID's key and names keyID.
func signWith(keyID string, domain typesv1.SignatureDomain, payload []byte) *typesv1.InnerSignature {
	return &typesv1.InnerSignature{
		Domain:                  domain,
		PublicKey:               pubFor(keyID),
		Signature:               ed25519.Sign(keyFor(keyID), payload),
		KeyId:                   keyID,
		CanonicalProfileVersion: 1,
	}
}

// abstractSig is the spec machine's view of a lease or content signature.
func abstractSig(domain typesv1.SignatureDomain, keyID string) map[string]any {
	return map[string]any{"domain": domainName(domain), "key_id": keyID, "valid": true}
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
	// headerTime (Block); id and value (CouncilAdmit, GovernedLimit).
	headerTime int64
	id         string
	value      uint64
}

// comparesEvents is false for council fixtures: the spec's
// apply_proposal_payload emits nothing, x/providers does.
func (s *step) comparesEvents() bool { return s.op != fxAdmit && s.op != fxGoverned }

func (s *step) businessID() string {
	if id, ok := s.spec["business_id"].(string); ok {
		return id
	}
	return ""
}

func (s *step) signer() []byte {
	switch m := s.msg.(type) {
	case *evidencev1.MsgSubmitAuditBatch:
		return m.GetSigner()
	case *leasesv1.MsgOfferLease:
		return m.GetSigner()
	}
	return nil
}

// ---------------------------------------------------------------------------
// Audit batches.

// kindName is the harness's own spelling of an evidence kind: the enum name
// without EVIDENCE_KIND_ for the three named values, the decimal number for
// any other value (3, 4, and 5 are reserved numbers without a name).
func kindName(k evidencev1.EvidenceKind) string {
	switch k {
	case evidencev1.EvidenceKind_EVIDENCE_KIND_UNSPECIFIED:
		return "UNSPECIFIED"
	case evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT:
		return "PROVIDER_RECEIPT"
	case evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION:
		return "AUDITOR_ATTESTATION"
	}
	return strconv.FormatInt(int64(k), 10)
}

// auditItem is one item of a generated batch.
type auditItem struct {
	evidenceID     string
	kind           evidencev1.EvidenceKind
	leaseID        string
	contentID      string
	contentVersion uint64
	digest         []byte
	expiry         int64
	attestation    bool
}

func (it auditItem) proto() *evidencev1.EvidenceItem {
	return &evidencev1.EvidenceItem{
		EvidenceId: it.evidenceID, Kind: it.kind, LeaseId: it.leaseID, ContentId: it.contentID,
		ContentVersion: it.contentVersion, ObservationDigest: digest(it.digest),
		ExpiryUnixSeconds: it.expiry, Attestation: it.attestation,
	}
}

func (it auditItem) spec() map[string]any {
	return map[string]any{
		"evidence_id": it.evidenceID, "kind": kindName(it.kind), "lease_id": it.leaseID,
		"content_id": it.contentID, "content_version": it.contentVersion,
		"expiry_unix_seconds": it.expiry, "attestation": it.attestation,
		"observation_digest": hexOf(it.digest),
	}
}

// Signature variants of an audit batch. Only sigValid by an auditor makes a
// signature the chain accepts; every other variant is invalid for the
// chain (deviations D-1 and D-2) except sigWrongDomain, which is valid in
// its own domain and fails the domain check.
const (
	sigValid          = "valid"
	sigForeignAuditor = "foreign auditor key"
	sigProviderKey    = "provider key"
	sigWrongDomain    = "wrong domain"
	sigNonCanonical   = "non-canonical payload"
	sigReordered      = "reordered items"
	// sigOther + field: the signature covers a payload in which that one
	// field differs from the message (a field changed after signing).
	sigOther = "signed other "
)

// signedFields are the fields of the signed batch object a mutated
// signature changes: the 13 envelope fields, the batch id, and the signer.
var signedFields = []string{
	"attestation", "claim_limit", "content_id", "content_version", "epoch", "evidence_id",
	"expiry_unix_seconds", "kind", "lease_id", "observation_sha256", "profile", "schema_version",
	"signer_key_id", "batch_id", "signer",
}

// batchFields are the signed fields outside the envelopes. A batch without
// items signs only these; every other field lives in an envelope.
var batchFields = []string{"batch_id", "signer"}

// auditParams is one generated batch.
type auditParams struct {
	signer   []byte
	business string
	batchID  string
	epoch    uint64
	items    []auditItem
	variant  string
}

// signedEnvelope and signedBatch are the attestation payload as the harness
// encodes it itself (chain/x/evidence/README.md "Attestation payload";
// docs/protocol/schemas/evidence-envelope.schema.json), independent of
// evidence.AuditCanonical.
type signedEnvelope struct {
	attestation    bool
	contentID      string
	contentVersion uint64
	evidenceID     string
	expiry         int64
	kind           string
	leaseID        string
	observation    string
}

type signedBatch struct {
	batchID    string
	signer     []byte
	epoch      uint64
	schema     uint32
	keyID      string
	claimLimit string
	profile    string
	envelopes  []signedEnvelope
}

// signedView is the payload a message's signature must cover.
func signedView(msg *evidencev1.MsgSubmitAuditBatch) signedBatch {
	b := signedBatch{
		batchID: msg.GetBatchId(), signer: msg.GetSigner(), epoch: msg.GetEpoch(),
		schema: msg.GetSchemaVersion(), keyID: msg.GetAuditorSignature().GetKeyId(),
		claimLimit: "ATTESTATION_NOT_STORAGE_PROOF", profile: "SVN-JCS-1",
	}
	for _, it := range msg.GetItems() {
		b.envelopes = append(b.envelopes, signedEnvelope{
			attestation: it.GetAttestation(), contentID: it.GetContentId(), contentVersion: it.GetContentVersion(),
			evidenceID: it.GetEvidenceId(), expiry: it.GetExpiryUnixSeconds(), kind: kindName(it.GetKind()),
			leaseID: it.GetLeaseId(), observation: hexOf(it.GetObservationDigest().GetValue()),
		})
	}
	return b
}

// jsonText is a JSON string of a value from the generator's character set
// (the id pattern's characters, upper-case letters, hex). Anything else is a
// generator bug: the hand-written encoding below does not escape.
func jsonText(s string) string {
	for _, r := range s {
		ok := r == '.' || r == '_' || r == '-' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
		if !ok {
			panic(fmt.Sprintf("difftest: %q is outside the generator's character set", s))
		}
	}
	return `"` + s + `"`
}

// text is the SVN-JCS-1 form of the batch object: keys in byte order, no
// whitespace, integers in decimal. The key order is written out by hand.
func (b signedBatch) text() string {
	var sb strings.Builder
	sb.WriteString(`{"batch_id":` + jsonText(b.batchID) + `,"envelopes":[`)
	for i, e := range b.envelopes {
		if i > 0 {
			sb.WriteByte(',')
		}
		fmt.Fprintf(&sb, `{"attestation":%t,"claim_limit":%s,"content_id":%s,"content_version":%d,"epoch":%d,`+
			`"evidence_id":%s,"expiry_unix_seconds":%d,"kind":%s,"lease_id":%s,"observation_sha256":%s,`+
			`"profile":%s,"schema_version":%d,"signer_key_id":%s}`,
			e.attestation, jsonText(b.claimLimit), jsonText(e.contentID), e.contentVersion, b.epoch,
			jsonText(e.evidenceID), e.expiry, jsonText(e.kind), jsonText(e.leaseID), jsonText(e.observation),
			jsonText(b.profile), b.schema, jsonText(b.keyID))
	}
	sb.WriteString(`],"signer":` + jsonText(hexOf(b.signer)) + `}`)
	return sb.String()
}

// mutate returns a copy of b with one signed field changed.
func (b signedBatch) mutate(field string) signedBatch {
	out := b
	out.envelopes = slices.Clone(b.envelopes)
	e := func() *signedEnvelope { return &out.envelopes[0] }
	switch field {
	case "attestation":
		e().attestation = !e().attestation
	case "claim_limit":
		out.claimLimit = "ATTESTATION_IS_STORAGE_PROOF"
	case "content_id":
		e().contentID += "x"
	case "content_version":
		e().contentVersion++
	case "epoch":
		out.epoch++
	case "evidence_id":
		e().evidenceID += "x"
	case "expiry_unix_seconds":
		e().expiry++
	case "kind":
		if e().kind == "PROVIDER_RECEIPT" {
			e().kind = "AUDITOR_ATTESTATION"
		} else {
			e().kind = "PROVIDER_RECEIPT"
		}
	case "lease_id":
		e().leaseID += "x"
	case "observation_sha256":
		e().observation = hexOf(sha("other observation:" + e().observation))
	case "profile":
		out.profile = "SVN-JCS-2"
	case "schema_version":
		out.schema++
	case "signer_key_id":
		out.keyID = "key-other"
	case "batch_id":
		out.batchID += "x"
	case "signer":
		if bytes.Equal(b.signer, auditorA) {
			out.signer = auditorB
		} else {
			out.signer = auditorA
		}
	default:
		panic("difftest: unknown signed field " + field)
	}
	return out
}

// build builds the batch message, signs it as p.variant asks, and returns
// the step with the spec view the oracle receives. The abstract signature
// is {domain, key_id "key-auditor", valid, observation_digest}: valid is
// the generator's intent (true only for sigValid by an auditor, and for
// sigWrongDomain by an auditor, which is valid in its own domain), and the
// digest is the first item's (the synthetic item's for an empty batch), so
// spec_machine.py's bound-digest check (INTEGRITY) never fires: deviation
// D-1. checkAudit recomputes valid from the signature.
func (p auditParams) build(tag string) *step {
	msg := &evidencev1.MsgSubmitAuditBatch{
		Signer: p.signer, BusinessId: p.business, BatchId: p.batchID, Epoch: p.epoch, SchemaVersion: 1,
	}
	for _, it := range p.items {
		msg.Items = append(msg.Items, it.proto())
	}
	own := auditorKeyOf(p.signer)
	key := own
	if key == "" {
		key = auditorKeyA
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_EVIDENCE_ATTESTATION
	msg.AuditorSignature = &typesv1.InnerSignature{Domain: domain, KeyId: auditorKeyID, CanonicalProfileVersion: 1}
	payload := signedView(msg)
	text := payload.text()
	valid := false
	variant := p.variant
	switch {
	case variant == sigValid:
		valid = own != ""
	case variant == sigForeignAuditor:
		if key == auditorKeyA {
			key = auditorKeyB
		} else {
			key = auditorKeyA
		}
	case variant == sigProviderKey:
		key = "p0-k1"
	case variant == sigWrongDomain:
		domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT
		msg.AuditorSignature.Domain = domain
		valid = own != ""
	case variant == sigNonCanonical:
		text = strings.Replace(text, `":`, `": `, 1)
	case variant == sigReordered && len(payload.envelopes) >= 2:
		reordered := payload
		reordered.envelopes = slices.Clone(payload.envelopes)
		slices.Reverse(reordered.envelopes)
		text = reordered.text()
	case strings.HasPrefix(variant, sigOther) && (len(payload.envelopes) > 0 || slices.Contains(batchFields, variant[len(sigOther):])):
		text = payload.mutate(variant[len(sigOther):]).text()
	default:
		// A variant this batch cannot carry (reordering one item, an
		// envelope field of an empty batch) falls back to a valid signature.
		variant = sigValid
		valid = own != ""
	}
	msg.AuditorSignature.PublicKey = pubFor(key)
	msg.AuditorSignature.Signature = ed25519.Sign(keyFor(key), signBytes(domain, text))
	first := syntheticDigest
	if len(p.items) > 0 {
		first = hexOf(p.items[0].digest)
	}
	items := make([]any, 0, len(p.items))
	for _, it := range p.items {
		items = append(items, it.spec())
	}
	s := &step{op: opAudit, tag: variant, msg: msg, spec: map[string]any{
		"op": opAudit, "signer": hexOf(p.signer), "business_id": p.business, "batch_id": p.batchID,
		"epoch": p.epoch, "items": items,
		"auditor_signature": map[string]any{
			"domain": domainName(domain), "key_id": auditorKeyID, "valid": valid, "observation_digest": first,
		},
	}}
	if tag != "" {
		s.tag = tag
	}
	if err := checkAudit(s); err != nil {
		panic("difftest generator: " + err.Error())
	}
	return s
}

// checkAudit recomputes the abstract validity of an audit batch's
// signature with crypto/ed25519 over the harness's own encoding of the
// message as sent: the signature's domain is a known domain, its public key
// is the key of the outer signer's address, and it verifies over that
// domain's sign bytes. A generator bug that labels a signature wrongly
// fails here rather than as a spurious divergence. It uses neither
// evidence.VerifyAuditSignature nor evidence.AuditSignBytes.
func checkAudit(s *step) error {
	msg := s.msg.(*evidencev1.MsgSubmitAuditBatch)
	sig := msg.GetAuditorSignature()
	_, known := domainStrings[sig.GetDomain()]
	valid := known && bytes.Equal(addressOf(sig.GetPublicKey()), msg.GetSigner()) &&
		ed25519.Verify(sig.GetPublicKey(), signBytes(sig.GetDomain(), signedView(msg).text()), sig.GetSignature())
	abstract := s.spec["auditor_signature"].(map[string]any)
	if valid != abstract["valid"] {
		return fmt.Errorf("audit signature variant %q: abstract valid %v, recomputed %v", s.tag, abstract["valid"], valid)
	}
	return nil
}

// resend is an exact copy of an earlier step (a retry of the same
// transaction) with another tag.
func resend(s *step, tag string) *step {
	again := *s
	again.tag = tag
	return &again
}

// ---------------------------------------------------------------------------
// Lease, content, and provider steps (valid signatures only: the lease
// differential of chain/x/leases/difftest covers their negative paths).

type offerParams struct {
	business, offerID     string
	contentID, providerID string
	size                  uint64
	deadline, expiry      int64
}

func buildOffer(p offerParams) *step {
	manifest, terms := manifestFor(p.contentID), termsFor(1)
	start := p.deadline - termSeconds
	msg := &leasesv1.MsgOfferLease{
		Signer: serviceAcct, BusinessId: p.business, OfferId: p.offerID, ContentId: p.contentID,
		ContentVersion: 1, ProviderId: p.providerID, ManifestDigest: digest(manifest),
		PackageBytes: p.size, TermsDigest: digest(terms), StartUnixSeconds: start,
		DeadlineUnixSeconds: p.deadline, ExpiryUnixSeconds: p.expiry, TermVersion: 1,
	}
	return &step{op: opOffer, msg: msg, spec: map[string]any{
		"op": opOffer, "signer": hexOf(serviceAcct), "business_id": p.business, "offer_id": p.offerID,
		"content_id": p.contentID, "content_version": 1, "provider_id": p.providerID,
		"package_bytes": p.size, "manifest_digest": hexOf(manifest), "terms_digest": hexOf(terms),
		"start_unix_seconds": start, "deadline_unix_seconds": p.deadline,
		"expiry_unix_seconds": p.expiry, "term_version": 1,
	}}
}

// offerRecord is the offer the chain stores for p (what an acceptance
// signs).
func offerRecord(p offerParams) *leasesv1.Offer {
	return &leasesv1.Offer{
		OfferId: p.offerID, ContentId: p.contentID, ContentVersion: 1, ProviderId: p.providerID,
		ManifestDigest: digest(manifestFor(p.contentID)), PackageBytes: p.size, TermsDigest: digest(termsFor(1)),
		StartUnixSeconds: p.deadline - termSeconds, DeadlineUnixSeconds: p.deadline,
		ExpiryUnixSeconds: p.expiry, TermVersion: 1, Scheduler: append([]byte(nil), serviceAcct...),
	}
}

// buildAccept is a broadcaster-relayed acceptance of offer, signed by its
// provider's current key keyID.
func buildAccept(business string, offer *leasesv1.Offer, leaseID, keyID string) *step {
	payload, err := leases.AcceptSignBytes(offer, leaseID)
	if err != nil {
		panic(fmt.Sprintf("difftest: accept sign bytes: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT
	msg := &leasesv1.MsgAcceptLease{
		Signer: broadcasterAcct, BusinessId: business, OfferId: offer.GetOfferId(), LeaseId: leaseID,
		ProviderAcceptance: signWith(keyID, domain, payload),
	}
	return &step{op: opAccept, msg: msg, spec: map[string]any{
		"op": opAccept, "signer": hexOf(broadcasterAcct), "business_id": business, "offer_id": offer.GetOfferId(),
		"lease_id": leaseID, "provider_acceptance": abstractSig(domain, keyID),
	}}
}

// buildRenew is a broadcaster-relayed renewal of lease to its next term
// (term version + 1, deadline + termSeconds), signed by the provider's
// current key keyID, naming evidenceID when it is not empty.
func buildRenew(business string, lease *leasesv1.Lease, keyID, evidenceID string) *step {
	term, deadline := lease.GetTermVersion()+1, lease.GetDeadlineUnixSeconds()+termSeconds
	manifest := lease.GetManifestDigest()
	payload, err := leases.RenewSignBytes(lease.GetLeaseId(), term, deadline, manifest, lease.GetPackageBytes(), evidenceID)
	if err != nil {
		panic(fmt.Sprintf("difftest: renew sign bytes: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW
	msg := &leasesv1.MsgRenewLease{
		Signer: broadcasterAcct, BusinessId: business, LeaseId: lease.GetLeaseId(), TermVersion: term,
		DeadlineUnixSeconds: deadline, ManifestDigest: digest(manifest.GetValue()), PackageBytes: lease.GetPackageBytes(),
		ProviderSignature: signWith(keyID, domain, payload), EvidenceId: evidenceID,
	}
	spec := map[string]any{
		"op": opRenew, "signer": hexOf(broadcasterAcct), "business_id": business, "lease_id": lease.GetLeaseId(),
		"term_version": term, "deadline_unix_seconds": deadline, "package_bytes": lease.GetPackageBytes(),
		"manifest_digest": hexOf(manifest.GetValue()), "provider_signature": abstractSig(domain, keyID),
	}
	if evidenceID != "" {
		spec["evidence_id"] = evidenceID
	}
	return &step{op: opRenew, msg: msg, spec: spec}
}

// buildEnd is a broadcaster-relayed provider end of lease, signed by the
// provider's current key keyID for the lease's current term version.
func buildEnd(business string, lease *leasesv1.Lease, keyID string) *step {
	term := lease.GetTermVersion()
	payload, err := leases.EndSignBytes(lease.GetLeaseId(), lease.GetProviderId(), term)
	if err != nil {
		panic(fmt.Sprintf("difftest: end sign bytes: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END
	msg := &leasesv1.MsgEndLease{
		Signer: broadcasterAcct, BusinessId: business, LeaseId: lease.GetLeaseId(),
		ProviderSignature: signWith(keyID, domain, payload),
	}
	abstract := abstractSig(domain, keyID)
	abstract["term_version"] = term
	return &step{op: opEnd, msg: msg, spec: map[string]any{
		"op": opEnd, "signer": hexOf(broadcasterAcct), "business_id": business, "lease_id": lease.GetLeaseId(),
		"authority_end": false, "provider_signature": abstract,
	}}
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

func buildRegisterContent(business, contentID string, size uint64) *step {
	manifest := manifestFor(contentID)
	payload, err := content.RegisterIntentSignBytes(serviceAcct, contentID, 1, manifest, size, content.DevnetRoleName)
	if err != nil {
		panic(fmt.Sprintf("difftest: register intent: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT
	msg := &contentv1.MsgRegisterContent{
		Signer: serviceAcct, BusinessId: business, ContentId: contentID, ContentVersion: 1, PackageBytes: size,
		ManifestDigest: digest(manifest), HostingIntent: signWith(intentKeyID, domain, payload),
	}
	return &step{op: opRegisterContent, msg: msg, spec: map[string]any{
		"op": opRegisterContent, "signer": hexOf(serviceAcct), "business_id": business, "content_id": contentID,
		"content_version": 1, "package_bytes": size, "manifest_digest": hexOf(manifest), "hosting_intent": intentSpec(),
	}}
}

func buildWithdraw(business, contentID string) *step {
	reason := reasonFor(contentID)
	payload, err := content.WithdrawIntentSignBytes(serviceAcct, contentID, 1, reason, content.DevnetRoleName)
	if err != nil {
		panic(fmt.Sprintf("difftest: withdraw intent: %v", err))
	}
	domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT
	msg := &contentv1.MsgWithdrawContent{
		Signer: serviceAcct, BusinessId: business, ContentId: contentID, ContentVersion: 1,
		ReasonDigest: digest(reason), HostingIntent: signWith(intentKeyID, domain, payload),
	}
	return &step{op: opWithdraw, msg: msg, spec: map[string]any{
		"op": opWithdraw, "signer": hexOf(serviceAcct), "business_id": business, "content_id": contentID,
		"content_version": 1, "hosting_intent": intentSpec(), "reason_digest": hexOf(reason),
	}}
}

func blockStep(headerTime int64) *step {
	return &step{op: fxBlock, headerTime: headerTime, spec: map[string]any{"op": fxBlock, "header_time": headerTime}}
}

func admitStep(providerID string) *step {
	return &step{op: fxAdmit, id: providerID, spec: map[string]any{"op": fxAdmit, "provider_id": providerID}}
}

// governedStep is an executed council GovernedLimitChange of key to value.
func governedStep(key string, value uint64) *step {
	return &step{op: fxGoverned, id: key, value: value, spec: map[string]any{
		"op": fxGoverned, "limit_key": key, "value": value,
	}}
}
