package leases

import (
	"bytes"
	"context"
	"math"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The four committed lease messages. Each handler runs msgkit.Enforce
// first, then the presence rule (an empty string, empty bytes, or a nil
// message field the spec requires is MALFORMED; an integer 0 is a value and
// goes through its range check), then the check order of
// docs/evidence/goal-02/spec_machine.py, then the chain-only checks
// (deviations D-1 to D-6, README), and writes only after every check
// passed. A failure returns the protocol code as an svnerr error (codespace
// "svn", code = svn.types.v1.ErrorCode) and leaves every store untouched. A
// business-id replay of the same payload is an idempotent success: no
// write, no typed event, one leases_result idempotent=true event. Committed
// time is the block header time, in whole seconds; no other clock exists
// in this module.

// OfferLease is op_offer (spec_machine.py:583).
func (k Keeper) OfferLease(goCtx context.Context, msg *leasesv1.MsgOfferLease) (*leasesv1.MsgOfferLeaseResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	if err := k.result(ctx, k.offerLease(ctx, msg)); err != nil {
		return nil, err
	}
	return &leasesv1.MsgOfferLeaseResponse{}, nil
}

// AcceptLease is op_accept (spec_machine.py:685).
func (k Keeper) AcceptLease(goCtx context.Context, msg *leasesv1.MsgAcceptLease) (*leasesv1.MsgAcceptLeaseResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	if err := k.result(ctx, k.acceptLease(ctx, msg)); err != nil {
		return nil, err
	}
	return &leasesv1.MsgAcceptLeaseResponse{}, nil
}

// RenewLease is op_renew (spec_machine.py:765).
func (k Keeper) RenewLease(goCtx context.Context, msg *leasesv1.MsgRenewLease) (*leasesv1.MsgRenewLeaseResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	if err := k.result(ctx, k.renewLease(ctx, msg)); err != nil {
		return nil, err
	}
	return &leasesv1.MsgRenewLeaseResponse{}, nil
}

// EndLease is op_end (spec_machine.py:858).
func (k Keeper) EndLease(goCtx context.Context, msg *leasesv1.MsgEndLease) (*leasesv1.MsgEndLeaseResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	if err := k.result(ctx, k.endLease(ctx, msg)); err != nil {
		return nil, err
	}
	return &leasesv1.MsgEndLeaseResponse{}, nil
}

// sdkContext unwraps the SDK context the message router and the query
// router pass. A plain context is MALFORMED instead of a panic.
func sdkContext(goCtx context.Context) (sdk.Context, error) {
	if ctx, ok := goCtx.(sdk.Context); ok {
		return ctx, nil
	}
	if ctx, ok := goCtx.Value(sdk.SdkContextKey).(sdk.Context); ok {
		return ctx, nil
	}
	return sdk.Context{}, svnerr.ErrMalformed.Wrap("leases: no SDK context")
}

// result turns an outcome into the handler result.
func (k Keeper) result(ctx sdk.Context, out msgkit.Outcome) error {
	if out.Code != msgkit.OK {
		return out.Code.Err()
	}
	if out.Idempotent {
		msgkit.EmitIdempotent(ctx, ModuleName)
	}
	return nil
}

func fail(code msgkit.Code) msgkit.Outcome { return msgkit.Outcome{Code: code} }

// checkRole is check_role (spec_machine.py:225): no role is UNAUTHORIZED,
// another role is FORBIDDEN.
func checkRole(role, want typesv1.SubmitterRole) msgkit.Code {
	if role == typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
		return msgkit.Unauthorized
	}
	if role != want {
		return msgkit.Forbidden
	}
	return msgkit.OK
}

// gate is business_gate (spec_machine.py:257): the id rule, then the
// digest of the spec's field set, then the ledger lookup across modules.
func (k Keeper) gate(ctx sdk.Context, businessID string, fields map[string]any) (msgkit.Outcome, []byte) {
	if code := msgkit.CheckID(businessID); code != msgkit.OK {
		return fail(code), nil
	}
	digest, code := msgkit.BusinessDigest(fields)
	if code != msgkit.OK {
		return fail(code), nil
	}
	return k.ledger.Gate(ctx, businessID, digest), digest
}

// businessUint is a raw integer of a business payload. The spec machine
// canonicalizes raw integers, and SVN-JCS-1 rejects any integer above
// 2^53-1, so such a value makes the gate MALFORMED. A value above
// math.MaxInt64 is returned as uint64, a type msgkit.Canonical rejects; a
// value in (2^53-1, MaxInt64] fails its safe-range check. Both are
// MALFORMED, as in the spec (decision request DR-08-10).
func businessUint(v uint64) any {
	if v > math.MaxInt64 {
		return v
	}
	return int64(v)
}

// checkPackageBytes is check_package (spec_machine.py:216): 1..effective
// max_package_bytes.
func (k Keeper) checkPackageBytes(ctx sdk.Context, n uint64) msgkit.Code {
	if n < 1 || n > k.limit(ctx, msgkit.LimitMaxPackageBytes) {
		return msgkit.Bounds
	}
	return msgkit.OK
}

// checkTerm is sponsorship_seconds (spec_machine.py:384): the DEC-005
// devnet test number must exist and be positive (else DECISION_BLOCKED),
// and seconds must equal it (else OUT_OF_RANGE). On a chain without the
// labelled devnet parameter every offer and renewal is DECISION_BLOCKED.
func (k Keeper) checkTerm(ctx sdk.Context, seconds int64) msgkit.Code {
	expected, found := k.deps.Params.DevnetNumeric(ctx, TermDecisionID)
	if !found || expected == 0 {
		return msgkit.DecisionBlocked
	}
	if seconds <= 0 || uint64(seconds) != expected {
		return msgkit.OutOfRange
	}
	return msgkit.OK
}

// verify checks a provider inner signature over a payload. A payload with
// no SVN-JCS-1 form (canonErr) cannot carry a valid signature: after the
// structural checks it is UNAUTHORIZED, the code every later step of
// msgkit.VerifyInner returns.
func (k Keeper) verify(ctx sdk.Context, providerID string, domain typesv1.SignatureDomain, canonical string, canonErr error, sig *typesv1.InnerSignature) msgkit.Code {
	if canonErr != nil {
		if code := msgkit.CheckInnerSignature(sig, domain); code != msgkit.OK {
			return code
		}
		return msgkit.Unauthorized
	}
	return k.deps.Providers.VerifyProviderSignature(ctx, providerID, domain, canonical, sig)
}

// providerAccount returns the 20-byte account of a provider record, or nil
// when there is no record. Callers compare it with a 20-byte signer, which
// never equals nil. The callers run after VerifyProviderSignature passed,
// so the record exists.
func (k Keeper) providerAccount(ctx sdk.Context, providerID string) []byte {
	record, _ := k.deps.Providers.Provider(ctx, providerID)
	return record.GetAccount()
}

// offerLease checks, in order:
//
//  0. msgkit.Enforce: gas limit and tx size BOUNDS, signer size MALFORMED
//  1. business_id, offer_id, content_id, provider_id, manifest_digest,
//     terms_digest present: MALFORMED
//  2. signer role: none UNAUTHORIZED, not ADMITTED_SERVICE FORBIDDEN
//  3. offer_id, content_id, provider_id outside the id pattern: BOUNDS
//  4. manifest_digest, then terms_digest: MALFORMED (algorithm) / BOUNDS (size)
//  5. start, deadline, expiry outside [1, 253402300799]: BOUNDS
//  6. package_bytes outside 1..effective max_package_bytes: BOUNDS
//  7. business gate: business_id outside the id pattern BOUNDS, MALFORMED
//     (an integer above 2^53-1), REPLAY, idempotent
//  8. offer_id is an open offer: DUPLICATE_ID
//  9. content: NOT_FOUND, WITHDRAWN, CONFLICT, INTEGRITY (Content.CheckOfferable)
//  10. provider: NOT_FOUND, FORBIDDEN unless ADMITTED
//  11. expiry <= committed time: EXPIRED
//  12. deadline <= start: OUT_OF_RANGE
//  13. deadline - start: DECISION_BLOCKED without DEC-005, OUT_OF_RANGE unless equal
//  14. term_version != 1: STALE_VERSION
//  15. live pair (provider, content, version): DUPLICATE_ID
//  16. provider open offers >= effective max_open_offers: BOUNDS
//  17. Providers.ReserveProbe: OVERFLOW, CAPACITY (no reservation is taken)
//  18. D-4 offer_id used before: DUPLICATE_ID
//  19. D-5 expiry - committed time > MaxOfferLifetimeSeconds: BOUNDS
//  20. D-6 content open offers >= MaxOpenOffersPerContent: BOUNDS
func (k Keeper) offerLease(ctx sdk.Context, msg *leasesv1.MsgOfferLease) msgkit.Outcome {
	if code := msgkit.Enforce(ctx, msg.GetSigner()); code != msgkit.OK {
		return fail(code)
	}
	if msg.GetBusinessId() == "" || msg.GetOfferId() == "" || msg.GetContentId() == "" || msg.GetProviderId() == "" ||
		msg.GetManifestDigest() == nil || msg.GetTermsDigest() == nil {
		return fail(msgkit.Malformed)
	}
	if code := checkRole(k.deps.Roles.Role(ctx, msg.GetSigner()), typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_SERVICE); code != msgkit.OK {
		return fail(code)
	}
	for _, id := range []string{msg.GetOfferId(), msg.GetContentId(), msg.GetProviderId()} {
		if code := msgkit.CheckID(id); code != msgkit.OK {
			return fail(code)
		}
	}
	manifest, code := msgkit.CheckDigest(msg.GetManifestDigest())
	if code != msgkit.OK {
		return fail(code)
	}
	terms, code := msgkit.CheckDigest(msg.GetTermsDigest())
	if code != msgkit.OK {
		return fail(code)
	}
	start, deadline, expiry := msg.GetStartUnixSeconds(), msg.GetDeadlineUnixSeconds(), msg.GetExpiryUnixSeconds()
	for _, t := range []int64{start, deadline, expiry} {
		if code := checkTime(t); code != msgkit.OK {
			return fail(code)
		}
	}
	if code := k.checkPackageBytes(ctx, msg.GetPackageBytes()); code != msgkit.OK {
		return fail(code)
	}
	gate, digest := k.gate(ctx, msg.GetBusinessId(), map[string]any{
		"business_id":           msg.GetBusinessId(),
		"content_id":            msg.GetContentId(),
		"content_version":       businessUint(msg.GetContentVersion()),
		"deadline_unix_seconds": deadline,
		"expiry_unix_seconds":   expiry,
		"manifest_digest":       msgkit.DigestObject(msg.GetManifestDigest()),
		"offer_id":              msg.GetOfferId(),
		"package_bytes":         businessUint(msg.GetPackageBytes()),
		"provider_id":           msg.GetProviderId(),
		"start_unix_seconds":    start,
		"term_version":          businessUint(msg.GetTermVersion()),
		"terms_digest":          msgkit.DigestObject(msg.GetTermsDigest()),
	})
	if gate.Code != msgkit.OK || gate.Idempotent {
		return gate
	}
	if _, open := k.mustLoadOffer(ctx, msg.GetOfferId()); open {
		return fail(msgkit.DuplicateID)
	}
	if code := k.deps.Content.CheckOfferable(ctx, msg.GetContentId(), msg.GetContentVersion(), manifest, msg.GetPackageBytes()); code != msgkit.OK {
		return fail(code)
	}
	if _, code := k.deps.Providers.CheckNewObligation(ctx, msg.GetProviderId()); code != msgkit.OK {
		return fail(code)
	}
	now := ctx.BlockTime().Unix()
	if expiry <= now {
		return fail(msgkit.Expired)
	}
	if deadline <= start {
		return fail(msgkit.OutOfRange)
	}
	if code := k.checkTerm(ctx, deadline-start); code != msgkit.OK {
		return fail(code)
	}
	if msg.GetTermVersion() != 1 {
		return fail(msgkit.StaleVersion)
	}
	if k.has(ctx, PairKey(msg.GetProviderId(), msg.GetContentId(), msg.GetContentVersion())) {
		return fail(msgkit.DuplicateID)
	}
	providerOffers, err := k.count(ctx, ProviderOfferCountKey(msg.GetProviderId()))
	if err != nil {
		panic(err.Error())
	}
	if providerOffers >= k.limit(ctx, msgkit.LimitMaxOpenOffers) {
		return fail(msgkit.Bounds)
	}
	if code := k.deps.Providers.ReserveProbe(ctx, msg.GetProviderId(), msg.GetPackageBytes()); code != msgkit.OK {
		return fail(code)
	}
	// Chain-only checks (README deviations), after every spec check.
	if k.has(ctx, UsedOfferKey(msg.GetOfferId())) {
		return fail(msgkit.DuplicateID) // D-4: offer ids are never reused
	}
	if expiry-MaxOfferLifetimeSeconds > now {
		return fail(msgkit.Bounds) // D-5: open offers cannot be held indefinitely
	}
	contentOffers, err := k.count(ctx, ContentOfferCountKey(msg.GetContentId()))
	if err != nil {
		panic(err.Error())
	}
	if contentOffers >= MaxOpenOffersPerContent {
		return fail(msgkit.Bounds) // D-6: bounds the withdrawal hook's work
	}

	offer := &leasesv1.Offer{
		OfferId:             msg.GetOfferId(),
		ContentId:           msg.GetContentId(),
		ContentVersion:      msg.GetContentVersion(),
		ProviderId:          msg.GetProviderId(),
		ManifestDigest:      sha256Digest(manifest),
		PackageBytes:        msg.GetPackageBytes(),
		TermsDigest:         sha256Digest(terms),
		StartUnixSeconds:    start,
		DeadlineUnixSeconds: deadline,
		ExpiryUnixSeconds:   expiry,
		TermVersion:         msg.GetTermVersion(),
		Scheduler:           append([]byte(nil), msg.GetSigner()...),
	}
	if err := k.addOffer(ctx, offer); err != nil {
		panic(err.Error())
	}
	k.ledger.Commit(ctx, msg.GetBusinessId(), digest)
	emitOffered(ctx, offer)
	return msgkit.Outcome{}
}

// acceptLease checks, in order:
//
//  0. msgkit.Enforce
//  1. business_id, offer_id, lease_id, provider_acceptance present: MALFORMED
//  2. signer role: none UNAUTHORIZED, not BROADCASTER or ADMITTED_PROVIDER FORBIDDEN
//  3. offer_id, lease_id outside the id pattern: BOUNDS
//  4. acceptance structure (msgkit.CheckInnerSignature, PROVIDER_ACCEPT):
//     MALFORMED, WRONG_DOMAIN, OUT_OF_RANGE, BOUNDS
//  5. business gate: business_id outside the id pattern BOUNDS, REPLAY,
//     idempotent
//  6. no open offer: NOT_FOUND
//  7. acceptance by the provider's current key over the stored offer and
//     lease_id (key id, public key, ed25519): UNAUTHORIZED (D-1)
//  8. ADMITTED_PROVIDER signer that is not the provider's account: FORBIDDEN
//  9. lease_id exists in any state: DUPLICATE_ID
//  10. committed time >= offer expiry: EXPIRED
//  11. content WITHDRAWN: WITHDRAWN
//  12. provider not ADMITTED: FORBIDDEN
//  13. live pair: DUPLICATE_ID
//  14. Providers.ReserveProbe: OVERFLOW, CAPACITY
//  15. D-3 offer deadline <= committed time: EXPIRED
func (k Keeper) acceptLease(ctx sdk.Context, msg *leasesv1.MsgAcceptLease) msgkit.Outcome {
	if code := msgkit.Enforce(ctx, msg.GetSigner()); code != msgkit.OK {
		return fail(code)
	}
	if msg.GetBusinessId() == "" || msg.GetOfferId() == "" || msg.GetLeaseId() == "" || msg.GetProviderAcceptance() == nil {
		return fail(msgkit.Malformed)
	}
	role := k.deps.Roles.Role(ctx, msg.GetSigner())
	switch role {
	case typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED:
		return fail(msgkit.Unauthorized)
	case typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER, typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER:
	default:
		return fail(msgkit.Forbidden)
	}
	for _, id := range []string{msg.GetOfferId(), msg.GetLeaseId()} {
		if code := msgkit.CheckID(id); code != msgkit.OK {
			return fail(code)
		}
	}
	acceptance := msg.GetProviderAcceptance()
	if code := msgkit.CheckInnerSignature(acceptance, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT); code != msgkit.OK {
		return fail(code)
	}
	gate, digest := k.gate(ctx, msg.GetBusinessId(), map[string]any{
		"lease_id":            msg.GetLeaseId(),
		"offer_id":            msg.GetOfferId(),
		"provider_acceptance": msgkit.SignatureObject(acceptance),
	})
	if gate.Code != msgkit.OK || gate.Idempotent {
		return gate
	}
	offer, open := k.mustLoadOffer(ctx, msg.GetOfferId())
	if !open {
		return fail(msgkit.NotFound)
	}
	canonical, canonErr := acceptCanonical(offer, msg.GetLeaseId())
	if code := k.verify(ctx, offer.GetProviderId(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, canonical, canonErr, acceptance); code != msgkit.OK {
		return fail(code)
	}
	if role == typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER {
		if !bytes.Equal(msg.GetSigner(), k.providerAccount(ctx, offer.GetProviderId())) {
			return fail(msgkit.Forbidden)
		}
	}
	if _, exists := k.mustLoadLease(ctx, msg.GetLeaseId()); exists {
		return fail(msgkit.DuplicateID)
	}
	now := ctx.BlockTime().Unix()
	if now >= offer.GetExpiryUnixSeconds() {
		return fail(msgkit.Expired)
	}
	if code := k.deps.Content.CheckNotWithdrawn(ctx, offer.GetContentId()); code != msgkit.OK {
		return fail(code)
	}
	if _, code := k.deps.Providers.CheckNewObligation(ctx, offer.GetProviderId()); code != msgkit.OK {
		return fail(code)
	}
	if k.has(ctx, PairKey(offer.GetProviderId(), offer.GetContentId(), offer.GetContentVersion())) {
		return fail(msgkit.DuplicateID)
	}
	if code := k.deps.Providers.ReserveProbe(ctx, offer.GetProviderId(), offer.GetPackageBytes()); code != msgkit.OK {
		return fail(code)
	}
	if offer.GetDeadlineUnixSeconds() <= now {
		return fail(msgkit.Expired) // D-3: a lease is never born past its real-time deadline
	}

	// Reserve repeats the probe that just passed in this branch, so it
	// cannot fail; a failure is an invariant violation and panics.
	if code := k.deps.Providers.Reserve(ctx, offer.GetProviderId(), offer.GetPackageBytes()); code != msgkit.OK {
		panic("leases: Reserve failed after a passing probe: " + string(code))
	}
	lease := &leasesv1.Lease{
		LeaseId:             msg.GetLeaseId(),
		OfferId:             offer.GetOfferId(),
		ContentId:           offer.GetContentId(),
		ContentVersion:      offer.GetContentVersion(),
		ProviderId:          offer.GetProviderId(),
		ManifestDigest:      sha256Digest(offer.GetManifestDigest().GetValue()),
		PackageBytes:        offer.GetPackageBytes(),
		TermsDigest:         sha256Digest(offer.GetTermsDigest().GetValue()),
		StartUnixSeconds:    offer.GetStartUnixSeconds(),
		DeadlineUnixSeconds: offer.GetDeadlineUnixSeconds(),
		State:               leasesv1.LeaseState_LEASE_STATE_ACTIVE,
		TermVersion:         offer.GetTermVersion(),
		PolicyVersion:       k.deps.Policy.PolicyVersion(ctx),
		ReservationHeld:     true,
	}
	if err := k.removeOffer(ctx, offer); err != nil {
		panic(err.Error())
	}
	k.saveLease(ctx, lease)
	k.holdIndexes(ctx, lease)
	k.ledger.Commit(ctx, msg.GetBusinessId(), digest)
	emitAccepted(ctx, lease)
	return msgkit.Outcome{}
}

// renewLease checks, in order:
//
//  0. msgkit.Enforce
//  1. business_id, lease_id, manifest_digest, provider_signature present: MALFORMED
//  2. no lease: NOT_FOUND
//  3. provider signature (PROVIDER_RENEW, current key, over the message
//     fields): MALFORMED, WRONG_DOMAIN, OUT_OF_RANGE, BOUNDS, UNAUTHORIZED
//  4. signer neither BROADCASTER nor ADMITTED_PROVIDER: no role UNAUTHORIZED,
//     another role FORBIDDEN; ADMITTED_PROVIDER that is not the provider's
//     account: FORBIDDEN
//  5. manifest_digest shape: MALFORMED, BOUNDS
//  6. business gate: business_id outside the id pattern BOUNDS, MALFORMED
//     (an integer above 2^53-1), REPLAY, idempotent
//  7. lease not ACTIVE/RENEWED or reservation not held: CONFLICT
//  8. content WITHDRAWN: WITHDRAWN
//  9. term_version != current + 1: STALE_VERSION
//  10. package_bytes or manifest digest differs from the lease: CONFLICT
//  11. new deadline outside [1, 253402300799]: BOUNDS
//  12. new deadline - current deadline: DECISION_BLOCKED / OUT_OF_RANGE (DEC-005)
//  13. evidence_id set: no reader or no record NOT_FOUND; expired EXPIRED;
//     otherwise FORBIDDEN (evidence never extends a lease)
//  14. D-2 current deadline <= committed time: EXPIRED
//
// Renewal leaves the reservation untouched. It is allowed for a DRAINING or
// DISABLED provider (v0 spec, decision request DR-08-01).
func (k Keeper) renewLease(ctx sdk.Context, msg *leasesv1.MsgRenewLease) msgkit.Outcome {
	if code := msgkit.Enforce(ctx, msg.GetSigner()); code != msgkit.OK {
		return fail(code)
	}
	if msg.GetBusinessId() == "" || msg.GetLeaseId() == "" || msg.GetManifestDigest() == nil || msg.GetProviderSignature() == nil {
		return fail(msgkit.Malformed)
	}
	lease, found := k.mustLoadLease(ctx, msg.GetLeaseId())
	if !found {
		return fail(msgkit.NotFound)
	}
	canonical, canonErr := renewCanonical(msg.GetLeaseId(), msg.GetTermVersion(), msg.GetDeadlineUnixSeconds(),
		msg.GetManifestDigest(), msg.GetPackageBytes(), msg.GetEvidenceId())
	if code := k.verify(ctx, lease.GetProviderId(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, canonical, canonErr, msg.GetProviderSignature()); code != msgkit.OK {
		return fail(code)
	}
	if code := k.checkRelayer(ctx, msg.GetSigner(), lease.GetProviderId(), false); code != msgkit.OK {
		return fail(code)
	}
	manifest, code := msgkit.CheckDigest(msg.GetManifestDigest())
	if code != msgkit.OK {
		return fail(code)
	}
	fields := map[string]any{
		"deadline_unix_seconds": msg.GetDeadlineUnixSeconds(),
		"lease_id":              msg.GetLeaseId(),
		"manifest_digest":       msgkit.DigestObject(msg.GetManifestDigest()),
		"package_bytes":         businessUint(msg.GetPackageBytes()),
		"provider_signature":    msgkit.SignatureObject(msg.GetProviderSignature()),
		"term_version":          businessUint(msg.GetTermVersion()),
	}
	if msg.GetEvidenceId() != "" {
		fields["evidence_id"] = msg.GetEvidenceId()
	}
	gate, digest := k.gate(ctx, msg.GetBusinessId(), fields)
	if gate.Code != msgkit.OK || gate.Idempotent {
		return gate
	}
	if !held(lease.GetState()) || !lease.GetReservationHeld() {
		return fail(msgkit.Conflict)
	}
	if code := k.deps.Content.CheckNotWithdrawn(ctx, lease.GetContentId()); code != msgkit.OK {
		return fail(code)
	}
	if lease.GetTermVersion() == math.MaxUint64 || msg.GetTermVersion() != lease.GetTermVersion()+1 {
		return fail(msgkit.StaleVersion)
	}
	if msg.GetPackageBytes() != lease.GetPackageBytes() || !bytes.Equal(manifest, lease.GetManifestDigest().GetValue()) {
		return fail(msgkit.Conflict)
	}
	deadline := msg.GetDeadlineUnixSeconds()
	if code := checkTime(deadline); code != msgkit.OK {
		return fail(code)
	}
	if code := k.checkTerm(ctx, deadline-lease.GetDeadlineUnixSeconds()); code != msgkit.OK {
		return fail(code)
	}
	now := ctx.BlockTime().Unix()
	if evidenceID := msg.GetEvidenceId(); evidenceID != "" {
		if k.deps.Evidence == nil {
			return fail(msgkit.NotFound)
		}
		evidenceExpiry, expired, exists := k.deps.Evidence.RenewalEvidence(ctx, evidenceID)
		if !exists {
			return fail(msgkit.NotFound)
		}
		if expired || evidenceExpiry <= now {
			return fail(msgkit.Expired)
		}
		return fail(msgkit.Forbidden)
	}
	if lease.GetDeadlineUnixSeconds() <= now {
		return fail(msgkit.Expired) // D-2: an expired obligation cannot renew, swept or not
	}

	store := k.kv(ctx)
	store.Delete(LeaseDeadlineKey(lease.GetDeadlineUnixSeconds(), lease.GetLeaseId()))
	lease.TermVersion = msg.GetTermVersion()
	lease.DeadlineUnixSeconds = deadline
	lease.State = leasesv1.LeaseState_LEASE_STATE_RENEWED
	k.saveLease(ctx, lease)
	store.Set(LeaseDeadlineKey(deadline, lease.GetLeaseId()), marker())
	k.ledger.Commit(ctx, msg.GetBusinessId(), digest)
	emitRenewed(ctx, lease)
	return msgkit.Outcome{}
}

// checkRelayer is the signer rule of a provider-signed renewal or end: the
// signer is a BROADCASTER (relaying the provider's signature) or the
// provider's own ADMITTED_PROVIDER account. For a renewal a signer without a
// role is UNAUTHORIZED (spec_machine.py:816, the BROADCASTER check's code);
// for an end every other signer is FORBIDDEN (:893).
func (k Keeper) checkRelayer(ctx sdk.Context, signer []byte, providerID string, end bool) msgkit.Code {
	role := k.deps.Roles.Role(ctx, signer)
	switch role {
	case typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER:
		return msgkit.OK
	case typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER:
		if !bytes.Equal(signer, k.providerAccount(ctx, providerID)) {
			return msgkit.Forbidden
		}
		return msgkit.OK
	case typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED:
		if end {
			return msgkit.Forbidden
		}
		return msgkit.Unauthorized
	default:
		return msgkit.Forbidden
	}
}

// endLease checks, in order:
//
//  0. msgkit.Enforce
//  1. business_id, lease_id present: MALFORMED
//  2. no lease: NOT_FOUND
//  3. authority_end true: signer role none UNAUTHORIZED, not AUTHORITY
//     FORBIDDEN; a provider_signature is UNKNOWN_FIELD.
//     authority_end false: no provider_signature MALFORMED; the signature
//     (PROVIDER_END, current key): MALFORMED, WRONG_DOMAIN, OUT_OF_RANGE,
//     BOUNDS, UNAUTHORIZED; signer neither BROADCASTER nor the provider's
//     own ADMITTED_PROVIDER account: FORBIDDEN
//  4. business gate: business_id outside the id pattern BOUNDS, REPLAY,
//     idempotent
//  5. reservation not held: UNDERFLOW (a second end, or an end after expiry)
//  6. Providers.Release: UNDERFLOW (it writes only on success)
//
// Ending is allowed in every provider state and for withdrawn content. The
// lease record stays, ENDED and without a reservation.
func (k Keeper) endLease(ctx sdk.Context, msg *leasesv1.MsgEndLease) msgkit.Outcome {
	if code := msgkit.Enforce(ctx, msg.GetSigner()); code != msgkit.OK {
		return fail(code)
	}
	if msg.GetBusinessId() == "" || msg.GetLeaseId() == "" {
		return fail(msgkit.Malformed)
	}
	lease, found := k.mustLoadLease(ctx, msg.GetLeaseId())
	if !found {
		return fail(msgkit.NotFound)
	}
	fields := map[string]any{
		"authority_end": msg.GetAuthorityEnd(),
		"lease_id":      msg.GetLeaseId(),
	}
	if msg.GetAuthorityEnd() {
		if code := checkRole(k.deps.Roles.Role(ctx, msg.GetSigner()), typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY); code != msgkit.OK {
			return fail(code)
		}
		if msg.GetProviderSignature() != nil {
			return fail(msgkit.UnknownField)
		}
	} else {
		if msg.GetProviderSignature() == nil {
			return fail(msgkit.Malformed)
		}
		canonical, canonErr := endCanonical(msg.GetLeaseId(), lease.GetProviderId())
		if code := k.verify(ctx, lease.GetProviderId(), typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, canonical, canonErr, msg.GetProviderSignature()); code != msgkit.OK {
			return fail(code)
		}
		if code := k.checkRelayer(ctx, msg.GetSigner(), lease.GetProviderId(), true); code != msgkit.OK {
			return fail(code)
		}
		fields["provider_signature"] = msgkit.SignatureObject(msg.GetProviderSignature())
	}
	gate, digest := k.gate(ctx, msg.GetBusinessId(), fields)
	if gate.Code != msgkit.OK || gate.Idempotent {
		return gate
	}
	if !lease.GetReservationHeld() {
		return fail(msgkit.Underflow)
	}
	// Release is the last check and the first write: it changes reserved
	// bytes only when it succeeds, and nothing after it can fail.
	if code := k.deps.Providers.Release(ctx, lease.GetProviderId(), lease.GetPackageBytes()); code != msgkit.OK {
		return fail(code)
	}
	k.dropIndexes(ctx, lease)
	lease.State = leasesv1.LeaseState_LEASE_STATE_ENDED
	lease.ReservationHeld = false
	k.saveLease(ctx, lease)
	k.ledger.Commit(ctx, msg.GetBusinessId(), digest)
	emit(ctx, endedEvent(lease))
	return msgkit.Outcome{}
}

func sha256Digest(value []byte) *typesv1.Digest {
	return &typesv1.Digest{
		Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256,
		Value:     append([]byte(nil), value...),
	}
}
