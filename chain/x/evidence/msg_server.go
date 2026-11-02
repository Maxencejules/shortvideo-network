package evidence

import (
	"bytes"
	"context"
	"math"
	"strings"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// SubmitAuditBatch is op_audit (docs/evidence/goal-02/spec_machine.py:908)
// for a batch of items under one auditor signature. It runs msgkit.Enforce
// first, then the presence rule (an empty string or a nil message field the
// spec requires is MALFORMED; an integer 0 is a value and goes through its
// own check), then the spec checks in spec order, then the chain-only checks
// (T1-T9) at the tail, and writes only after every check passed. A step
// that names "each item" runs over the items in message order and the first
// failing item decides; it finishes over every item before the next step
// starts (check-major, deviation D-6). A failure returns the protocol code
// as an svnerr error and leaves the store untouched. A business-id replay of
// the same payload is an idempotent success: no write, no typed event, one
// evidence_result idempotent=true event. Committed time is the block header
// time, in whole seconds; no other clock exists in this module.
func (k Keeper) SubmitAuditBatch(goCtx context.Context, msg *evidencev1.MsgSubmitAuditBatch) (*evidencev1.MsgSubmitAuditBatchResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	if err := k.result(ctx, k.submitAuditBatch(ctx, msg)); err != nil {
		return nil, err
	}
	return &evidencev1.MsgSubmitAuditBatchResponse{}, nil
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
	return sdk.Context{}, svnerr.ErrMalformed.Wrap("evidence: no SDK context")
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

// businessUint is a raw integer of a business payload, as x/leases builds
// it (chain/x/leases/msg_server.go, businessUint; DR-08-10). A value above
// math.MaxInt64 stays uint64, a type msgkit.Canonical rejects, and a value
// in (2^53-1, MaxInt64] fails its safe-range check; both make the gate
// MALFORMED, as the spec's canonicalize does. Every integer of this
// message's business payload is also in the signed payload, so such a
// value is UNAUTHORIZED at step 8 before the gate runs.
func businessUint(v uint64) any {
	if v > math.MaxInt64 {
		return v
	}
	return int64(v)
}

// businessFields is the business payload (business_gate,
// spec_machine.py:257): every message field except the signer (spec :964),
// keyed by proto field name, items in message order.
func businessFields(msg *evidencev1.MsgSubmitAuditBatch) map[string]any {
	items := make([]any, 0, len(msg.GetItems()))
	for _, item := range msg.GetItems() {
		items = append(items, map[string]any{
			"attestation":         item.GetAttestation(),
			"content_id":          item.GetContentId(),
			"content_version":     businessUint(item.GetContentVersion()),
			"evidence_id":         item.GetEvidenceId(),
			"expiry_unix_seconds": item.GetExpiryUnixSeconds(),
			"kind":                KindName(item.GetKind()),
			"lease_id":            item.GetLeaseId(),
			"observation_digest":  msgkit.DigestObject(item.GetObservationDigest()),
		})
	}
	return map[string]any{
		"auditor_signature": msgkit.SignatureObject(msg.GetAuditorSignature()),
		"batch_id":          msg.GetBatchId(),
		"business_id":       msg.GetBusinessId(),
		"epoch":             businessUint(msg.GetEpoch()),
		"items":             items,
		"schema_version":    int64(msg.GetSchemaVersion()),
	}
}

// gate is business_gate: the id rule, the digest of the payload, then the
// ledger lookup across modules.
func (k Keeper) gate(ctx sdk.Context, msg *evidencev1.MsgSubmitAuditBatch) (msgkit.Outcome, []byte) {
	if code := msgkit.CheckID(msg.GetBusinessId()); code != msgkit.OK {
		return fail(code), nil
	}
	digest, code := msgkit.BusinessDigest(businessFields(msg))
	if code != msgkit.OK {
		return fail(code), nil
	}
	return k.ledger.Gate(ctx, msg.GetBusinessId(), digest), digest
}

// held reports whether a lease holds its reservation: state ACTIVE or
// RENEWED with reservation_held set.
func held(lease *leasesv1.Lease) bool {
	state := lease.GetState()
	return (state == leasesv1.LeaseState_LEASE_STATE_ACTIVE || state == leasesv1.LeaseState_LEASE_STATE_RENEWED) &&
		lease.GetReservationHeld()
}

// matchesManifest reports whether an observation digest value (a checked
// 32-byte SHA-256 value) equals the lease's manifest digest, algorithm and
// value (DEC-010).
func matchesManifest(observation []byte, lease *leasesv1.Lease) bool {
	manifest := lease.GetManifestDigest()
	return manifest.GetAlgorithm() == typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 &&
		bytes.Equal(observation, manifest.GetValue())
}

// submitAuditBatch checks, in order (README "Check order"):
//
//  1. msgkit.Enforce: gas limit and tx size BOUNDS, signer not 20 bytes MALFORMED
//  2. business_id, batch_id empty, auditor_signature nil; each item nil, or
//     its evidence_id, lease_id, content_id empty or observation_digest nil:
//     MALFORMED
//  3. signer role: none UNAUTHORIZED, not AUDITOR FORBIDDEN (a BROADCASTER
//     relay included; owner ruling on Q1)
//  4. schema_version 0 MALFORMED, not 1 OUT_OF_RANGE (D-5)
//  5. each item observation_digest: algorithm 0 or unknown MALFORMED, value
//     not 32 bytes BOUNDS
//  6. auditor_signature structure: MALFORMED, WRONG_DOMAIN, OUT_OF_RANGE, BOUNDS
//  7. AuditorAddress(public key) != signer: UNAUTHORIZED (D-2)
//  8. payload: no SVN-JCS-1 form UNAUTHORIZED, above 65536 bytes BOUNDS (D-6),
//     ed25519 failure UNAUTHORIZED (D-1)
//  9. batch_id, then each item evidence_id, lease_id, content_id outside the
//     id pattern: BOUNDS
//  10. each item kind not PROVIDER_RECEIPT or AUDITOR_ATTESTATION (0, the
//     reserved 3-5, any other number): MALFORMED
//  11. item count < 1 or > effective max_evidence_batch: BOUNDS
//  12. each item attestation false: MALFORMED
//  13. epoch != EpochAt(committed time): STALE_VERSION (D-3)
//  14. each item expiry outside [1, 253402300799]: BOUNDS
//  15. business gate: business_id outside the id pattern BOUNDS, MALFORMED
//     (no canonical form; unreachable after step 8), REPLAY, idempotent
//  16. batch_id used, or an item evidence_id stored or repeated earlier in
//     the batch (D-6): DUPLICATE_ID
//  17. each item expiry <= committed time: EXPIRED
//  18. each item lease absent: NOT_FOUND
//  19. T1 batch_id or an item evidence_id not starting with IDPrefix(signer): FORBIDDEN (D-17)
//  20. T2 each item kind PROVIDER_RECEIPT: FORBIDDEN (D-8)
//  21. T3 each item expiry - committed time > MaxEvidenceLifetimeSeconds: BOUNDS (D-9)
//  22. T4 each item content_id or content_version differs from its lease: CONFLICT (D-10)
//  23. T5 each item lease not held (not ACTIVE/RENEWED with reservation_held): CONFLICT (D-11)
//  24. T6 each item lease deadline <= committed time: EXPIRED (D-12)
//  25. T7 each item expiry > its lease deadline: CONFLICT (D-18)
//  26. T8 each item lease content withdrawn: WITHDRAWN; content absent NOT_FOUND (D-13)
//  27. T9 the signer's items in the current epoch + item count >
//     MaxEvidencePerAuditorPerEpoch: BOUNDS (D-15)
func (k Keeper) submitAuditBatch(ctx sdk.Context, msg *evidencev1.MsgSubmitAuditBatch) msgkit.Outcome {
	if code := msgkit.Enforce(ctx, msg.GetSigner()); code != msgkit.OK {
		return fail(code)
	}
	if msg.GetBusinessId() == "" || msg.GetBatchId() == "" || msg.GetAuditorSignature() == nil {
		return fail(msgkit.Malformed)
	}
	items := msg.GetItems()
	for _, item := range items {
		if item == nil || item.GetEvidenceId() == "" || item.GetLeaseId() == "" || item.GetContentId() == "" ||
			item.GetObservationDigest() == nil {
			return fail(msgkit.Malformed)
		}
	}
	if code := checkRole(k.deps.Roles.Role(ctx, msg.GetSigner()), typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR); code != msgkit.OK {
		return fail(code)
	}
	switch msg.GetSchemaVersion() {
	case 0:
		return fail(msgkit.Malformed)
	case SchemaVersion:
	default:
		return fail(msgkit.OutOfRange)
	}
	observations := make([][]byte, len(items))
	for i, item := range items {
		value, code := msgkit.CheckDigest(item.GetObservationDigest())
		if code != msgkit.OK {
			return fail(code)
		}
		observations[i] = value
	}
	if code := VerifyAuditSignature(msg); code != msgkit.OK {
		return fail(code)
	}
	if code := msgkit.CheckID(msg.GetBatchId()); code != msgkit.OK {
		return fail(code)
	}
	for _, item := range items {
		for _, id := range []string{item.GetEvidenceId(), item.GetLeaseId(), item.GetContentId()} {
			if code := msgkit.CheckID(id); code != msgkit.OK {
				return fail(code)
			}
		}
	}
	for _, item := range items {
		switch item.GetKind() {
		case evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT, evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION:
		default:
			return fail(msgkit.Malformed)
		}
	}
	if len(items) < 1 || uint64(len(items)) > k.limit(ctx, msgkit.LimitMaxEvidenceBatch) {
		return fail(msgkit.Bounds)
	}
	for _, item := range items {
		if !item.GetAttestation() {
			return fail(msgkit.Malformed)
		}
	}
	now := ctx.BlockTime().Unix()
	current := EpochAt(now)
	if msg.GetEpoch() != current {
		return fail(msgkit.StaleVersion)
	}
	for _, item := range items {
		if t := item.GetExpiryUnixSeconds(); t < genesis.UnixSecondsMin || t > genesis.UnixSecondsMax {
			return fail(msgkit.Bounds)
		}
	}
	gate, digest := k.gate(ctx, msg)
	if gate.Code != msgkit.OK || gate.Idempotent {
		return gate
	}
	if k.has(ctx, BatchKey(msg.GetBatchId())) {
		return fail(msgkit.DuplicateID)
	}
	seen := make(map[string]bool, len(items))
	for _, item := range items {
		id := item.GetEvidenceId()
		if seen[id] || k.has(ctx, RecordKey(id)) {
			return fail(msgkit.DuplicateID)
		}
		seen[id] = true
	}
	for _, item := range items {
		if item.GetExpiryUnixSeconds() <= now {
			return fail(msgkit.Expired)
		}
	}
	leases := make([]*leasesv1.Lease, len(items))
	for i, item := range items {
		lease, found := k.deps.Leases.Lease(ctx, item.GetLeaseId())
		if !found {
			return fail(msgkit.NotFound)
		}
		leases[i] = lease
	}

	// Chain-only checks (README deviations), after every spec check.
	prefix := IDPrefix(msg.GetSigner())
	if !strings.HasPrefix(msg.GetBatchId(), prefix) {
		return fail(msgkit.Forbidden) // T1, D-17: ids belong to the auditor whose prefix they carry
	}
	for _, item := range items {
		if !strings.HasPrefix(item.GetEvidenceId(), prefix) {
			return fail(msgkit.Forbidden)
		}
	}
	for _, item := range items {
		if item.GetKind() == evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT {
			return fail(msgkit.Forbidden) // T2, D-8: no provider signature exists on chain
		}
	}
	for _, item := range items {
		if item.GetExpiryUnixSeconds()-MaxEvidenceLifetimeSeconds > now {
			return fail(msgkit.Bounds) // T3, D-9: expiry <= committed time + lifetime
		}
	}
	for i, item := range items {
		if item.GetContentId() != leases[i].GetContentId() || item.GetContentVersion() != leases[i].GetContentVersion() {
			return fail(msgkit.Conflict) // T4, D-10: lease/content binding
		}
	}
	for i := range items {
		if !held(leases[i]) {
			return fail(msgkit.Conflict) // T5, D-11: no new record for an ended obligation
		}
	}
	for i := range items {
		if leases[i].GetDeadlineUnixSeconds() <= now {
			return fail(msgkit.Expired) // T6, D-12: held but past its deadline
		}
	}
	for i, item := range items {
		if item.GetExpiryUnixSeconds() > leases[i].GetDeadlineUnixSeconds() {
			return fail(msgkit.Conflict) // T7, D-18: a record never outlives its lease's deadline
		}
	}
	for i := range items {
		if code := k.deps.Content.CheckNotWithdrawn(ctx, leases[i].GetContentId()); code != msgkit.OK {
			return fail(code) // T8, D-13: no audit of withdrawn content
		}
	}
	windowEpoch, windowCount := k.mustLoadWindow(ctx, msg.GetSigner())
	if windowEpoch > current {
		// The header time never decreases and InitGenesis refuses a record
		// from a later epoch, so a stored window is never ahead of the
		// current epoch. An invariant violation panics.
		panic("evidence: auditor window is ahead of the current epoch")
	}
	if windowEpoch != current {
		windowCount = 0
	}
	if windowCount > MaxEvidencePerAuditorPerEpoch || uint64(len(items)) > MaxEvidencePerAuditorPerEpoch-windowCount {
		return fail(msgkit.Bounds) // T9, D-15: per-auditor per-epoch item cap
	}

	sig := msg.GetAuditorSignature()
	records := make([]*evidencev1.EvidenceRecord, len(items))
	for i, item := range items {
		state := evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED
		if matchesManifest(observations[i], leases[i]) {
			state = evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED
		}
		records[i] = &evidencev1.EvidenceRecord{
			EvidenceId:        item.GetEvidenceId(),
			BatchId:           msg.GetBatchId(),
			Kind:              item.GetKind(),
			State:             state,
			SignerPubkey:      append([]byte(nil), sig.GetPublicKey()...),
			LeaseId:           item.GetLeaseId(),
			ContentId:         item.GetContentId(),
			ContentVersion:    item.GetContentVersion(),
			ObservationDigest: sha256Digest(observations[i]),
			Epoch:             msg.GetEpoch(),
			ExpiryUnixSeconds: item.GetExpiryUnixSeconds(),
			SchemaVersion:     SchemaVersion,
			Attestation:       true,
		}
		k.addRecord(ctx, records[i])
	}
	k.markBatch(ctx, msg.GetBatchId())
	k.saveWindow(ctx, msg.GetSigner(), current, windowCount+uint64(len(items)))
	k.ledger.Commit(ctx, msg.GetBusinessId(), digest)
	for _, record := range records {
		emitAccepted(ctx, record)
	}
	return msgkit.Outcome{}
}
