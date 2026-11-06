package evidence_test

import (
	"errors"
	"fmt"
	"math"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// A fault ladder proves the SubmitAuditBatch check order and that every
// reject row reaches its own branch. faults lists one fault per check, in
// check order (README "Check order").
//
//   - cumulative step i applies faults i..n together (the later ones first,
//     so an earlier fault on the same field wins) and expects the code of
//     fault i: every later fault is present, so a handler that ran a later
//     check first would return that check's code. A fault marked solo (it
//     replaces the item list) is left out of the cumulative steps of the
//     rows before it.
//   - isolated step i applies fault i alone and expects the same code: a
//     handler without check i would accept the message, so the row reaches
//     the branch that rejects it even when a later check shares its code.
//   - the last step applies no fault and expects success.
//
// The message is signed by the auditor's key after the faults are applied
// (unless a fault removed the signature), so a fault on a signed field does
// not by itself break the signature; signature faults edit it after
// signing. Every step runs on a cached branch of the context: a rejected
// step must leave the evidence, leases, and content stores byte-identical
// and emit no event.

type call struct {
	ctx    sdk.Context
	msg    *evidencev1.MsgSubmitAuditBatch
	signAs string
	// sig edits the signature after signing, in order.
	sig []func(*typesv1.InnerSignature)
	// unsigned skips signing (the fault removed the signature).
	unsigned bool
}

type fault struct {
	name  string
	want  msgkit.Code
	solo  bool
	apply func(e *env, c *call)
	// item is the edit of an item fault (item0) on its own, so
	// TestItemFaultsOnLastItem can apply it to another item.
	item func(it *evidencev1.EvidenceItem)
}

func edit(name string, want msgkit.Code, f func(m *evidencev1.MsgSubmitAuditBatch)) fault {
	return fault{name: name, want: want, apply: func(_ *env, c *call) { f(c.msg) }}
}

func item0(name string, want msgkit.Code, f func(it *evidencev1.EvidenceItem)) fault {
	return fault{name: name, want: want, item: f, apply: func(_ *env, c *call) { f(c.msg.Items[0]) }}
}

func envFault(name string, want msgkit.Code, f func(e *env, c *call)) fault {
	return fault{name: name, want: want, apply: f}
}

func sigEdit(name string, want msgkit.Code, f func(s *typesv1.InnerSignature)) fault {
	return fault{name: name, want: want, apply: func(_ *env, c *call) { c.sig = append(c.sig, f) }}
}

// ladderBase is a two-item batch of auditor-1 over l1 and l2.
func ladderBase(e *env) *evidencev1.MsgSubmitAuditBatch {
	return batch(auditor1, "1", e.item(auditor1, "e1", "l1"), e.item(auditor1, "e2", "l2"))
}

func runStep(t *testing.T, faults []fault, apply []int, want msgkit.Code) {
	t.Helper()
	e := newEnv(t)
	c := &call{ctx: e.fresh(), msg: ladderBase(e), signAs: auditor1}
	for j := len(apply) - 1; j >= 0; j-- {
		faults[apply[j]].apply(e, c)
	}
	if !c.unsigned {
		sign(c.msg, c.signAs)
		for _, f := range c.sig {
			f(c.msg.AuditorSignature)
		}
	}
	r := e.submit(c.ctx, c.msg)
	if want == msgkit.OK {
		e.requireOK(r, "no fault")
		return
	}
	e.requireReject(r, want, t.Name())
}

func runLadder(t *testing.T, faults []fault) {
	t.Helper()
	for i := range faults {
		f := faults[i]
		cumulative := []int{i}
		for j := i + 1; j < len(faults); j++ {
			if !faults[j].solo {
				cumulative = append(cumulative, j)
			}
		}
		t.Run("cumulative/"+f.name, func(t *testing.T) { runStep(t, faults, cumulative, f.want) })
		t.Run("isolated/"+f.name, func(t *testing.T) { runStep(t, faults, []int{i}, f.want) })
	}
	t.Run("no-fault", func(t *testing.T) { runStep(t, faults, nil, msgkit.OK) })
}

// oversized replaces the items with maximal items, one at a time, until the
// call's canonical payload exceeds 65536 bytes, and sets the transaction
// size to the encoded message plus room for the signature.
func oversized(e *env, c *call) {
	c.msg.Items = nil
	for i := 0; ; i++ {
		c.msg.Items = append(c.msg.Items, maximalItem(auditor1, i))
		if _, err := evidence.AuditCanonical(c.msg); errors.Is(err, evidence.ErrPayloadTooLarge) {
			break
		} else if err != nil || i > 200 {
			e.t.Fatalf("no oversized payload after %d items: %v", i+1, err)
		}
	}
	size := proto.Size(c.msg) + 256
	if size >= 65536 {
		e.t.Fatalf("oversized message is %d bytes", size)
	}
	c.ctx = c.ctx.WithTxBytes(make([]byte, size))
}

// checkOrderFaults is the full ladder, one fault per row of the README check
// order table, in check order.
func checkOrderFaults() []fault {
	return []fault{
		// 1 Enforce
		envFault("enforce-gas-limit-0", msgkit.Bounds, func(_ *env, c *call) { c.ctx = c.ctx.WithGasMeter(storetypes.NewGasMeter(0)) }),
		envFault("enforce-tx-65537-bytes", msgkit.Bounds, func(_ *env, c *call) { c.ctx = c.ctx.WithTxBytes(make([]byte, 65537)) }),
		edit("enforce-signer-19-bytes", msgkit.Malformed, func(m *evidencev1.MsgSubmitAuditBatch) { m.Signer = m.Signer[:19] }),
		// 2 presence
		edit("presence-business-id", msgkit.Malformed, func(m *evidencev1.MsgSubmitAuditBatch) { m.BusinessId = "" }),
		edit("presence-batch-id", msgkit.Malformed, func(m *evidencev1.MsgSubmitAuditBatch) { m.BatchId = "" }),
		envFault("presence-auditor-signature", msgkit.Malformed, func(_ *env, c *call) { c.msg.AuditorSignature, c.unsigned = nil, true }),
		edit("presence-item-nil", msgkit.Malformed, func(m *evidencev1.MsgSubmitAuditBatch) { m.Items[0] = nil }),
		item0("presence-evidence-id", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.EvidenceId = "" }),
		item0("presence-lease-id", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.LeaseId = "" }),
		item0("presence-content-id", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.ContentId = "" }),
		item0("presence-observation-digest", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.ObservationDigest = nil }),
		// 3 role
		edit("role-none", msgkit.Unauthorized, func(m *evidencev1.MsgSubmitAuditBatch) { m.Signer = outsiderAcct }),
		edit("role-service", msgkit.Forbidden, func(m *evidencev1.MsgSubmitAuditBatch) { m.Signer = serviceAcct }),
		edit("role-provider", msgkit.Forbidden, func(m *evidencev1.MsgSubmitAuditBatch) { m.Signer = providerAcct }),
		edit("role-broadcaster-relay", msgkit.Forbidden, func(m *evidencev1.MsgSubmitAuditBatch) { m.Signer = broadcasterAcct }),
		edit("role-authority", msgkit.Forbidden, func(m *evidencev1.MsgSubmitAuditBatch) { m.Signer = authorityAcct }),
		// 4 schema version (D-5)
		edit("schema-version-0", msgkit.Malformed, func(m *evidencev1.MsgSubmitAuditBatch) { m.SchemaVersion = 0 }),
		edit("schema-version-2", msgkit.OutOfRange, func(m *evidencev1.MsgSubmitAuditBatch) { m.SchemaVersion = 2 }),
		// 5 observation digest
		item0("digest-algorithm-0", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.ObservationDigest.Algorithm = 0 }),
		item0("digest-algorithm-7", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.ObservationDigest.Algorithm = 7 }),
		item0("digest-31-bytes", msgkit.Bounds, func(it *evidencev1.EvidenceItem) { it.ObservationDigest.Value = it.ObservationDigest.Value[:31] }),
		item0("digest-33-bytes", msgkit.Bounds, func(it *evidencev1.EvidenceItem) {
			it.ObservationDigest.Value = append(it.ObservationDigest.Value, 1)
		}),
		// 6 signature structure
		sigEdit("sig-domain-unspecified", msgkit.Malformed, func(s *typesv1.InnerSignature) {
			s.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_UNSPECIFIED
		}),
		sigEdit("sig-domain-unknown-99", msgkit.Malformed, func(s *typesv1.InnerSignature) { s.Domain = 99 }),
		sigEdit("sig-wrong-domain", msgkit.WrongDomain, func(s *typesv1.InnerSignature) {
			s.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT
		}),
		sigEdit("sig-profile-0", msgkit.Malformed, func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 0 }),
		sigEdit("sig-profile-2", msgkit.OutOfRange, func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 2 }),
		sigEdit("sig-key-id-empty", msgkit.Malformed, func(s *typesv1.InnerSignature) { s.KeyId = "" }),
		sigEdit("sig-key-id-pattern", msgkit.Bounds, func(s *typesv1.InnerSignature) { s.KeyId = "KEY-A1" }),
		sigEdit("sig-public-key-31", msgkit.Bounds, func(s *typesv1.InnerSignature) { s.PublicKey = s.PublicKey[:31] }),
		sigEdit("sig-signature-63", msgkit.Bounds, func(s *typesv1.InnerSignature) { s.Signature = s.Signature[:63] }),
		// 7 key binding (D-2, Q1): a valid signature by a key that does not
		// derive the signer's address
		envFault("binding-other-auditor-key", msgkit.Unauthorized, func(_ *env, c *call) {
			c.msg.AuditorSignature.PublicKey, c.signAs = pub(auditor2), auditor2
		}),
		envFault("binding-provider-key", msgkit.Unauthorized, func(_ *env, c *call) {
			c.msg.AuditorSignature.PublicKey, c.signAs = pub("provider-p1"), "provider-p1"
		}),
		// 8 payload (D-1, D-6)
		item0("payload-content-version-2^53", msgkit.Unauthorized, func(it *evidencev1.EvidenceItem) { it.ContentVersion = 1 << 53 }),
		item0("payload-content-version-2^63", msgkit.Unauthorized, func(it *evidencev1.EvidenceItem) { it.ContentVersion = 1 << 63 }),
		item0("payload-content-version-max-uint64", msgkit.Unauthorized, func(it *evidencev1.EvidenceItem) { it.ContentVersion = math.MaxUint64 }),
		edit("payload-epoch-2^53", msgkit.Unauthorized, func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = 1 << 53 }),
		item0("payload-expiry-min-int64", msgkit.Unauthorized, func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = math.MinInt64 }),
		{name: "payload-above-65536-bytes", want: msgkit.Bounds, solo: true, apply: oversized},
		sigEdit("payload-ed25519-failure", msgkit.Unauthorized, func(s *typesv1.InnerSignature) { s.Signature[0] ^= 0xff }),
		// 9 ids
		edit("batch-id-pattern", msgkit.Bounds, func(m *evidencev1.MsgSubmitAuditBatch) { m.BatchId = "B1" }),
		item0("evidence-id-pattern", msgkit.Bounds, func(it *evidencev1.EvidenceItem) { it.EvidenceId = "E1" }),
		item0("lease-id-pattern", msgkit.Bounds, func(it *evidencev1.EvidenceItem) { it.LeaseId = "-l1" }),
		item0("content-id-pattern", msgkit.Bounds, func(it *evidencev1.EvidenceItem) { it.ContentId = "c1/x" }),
		// 10 kinds
		item0("kind-0", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.Kind = 0 }),
		item0("kind-3-reserved-storage-proof", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.Kind = 3 }),
		item0("kind-4-reserved-unique-replica", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.Kind = 4 }),
		item0("kind-5-reserved-future-delivery", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.Kind = 5 }),
		item0("kind-7-unknown", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.Kind = 7 }),
		// 11 count
		{name: "count-0", want: msgkit.Bounds, solo: true, apply: func(_ *env, c *call) { c.msg.Items = nil }},
		envFault("count-33", msgkit.Bounds, func(e *env, c *call) {
			for i := len(c.msg.Items); i < 33; i++ {
				c.msg.Items = append(c.msg.Items, e.item(auditor1, fmt.Sprintf("x%d", i), "l3"))
			}
		}),
		envFault("count-above-governed-2", msgkit.Bounds, func(e *env, c *call) {
			e.auth.limits[msgkit.LimitMaxEvidenceBatch] = 2
			c.msg.Items = append(c.msg.Items, e.item(auditor1, "g3", "l3"))
		}),
		// 12 attestation
		item0("attestation-false", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.Attestation = false }),
		// 13 epoch (D-3)
		edit("epoch-expired-round", msgkit.StaleVersion, func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = epochNow - 1 }),
		edit("epoch-future-round", msgkit.StaleVersion, func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = epochNow + 1 }),
		edit("epoch-0", msgkit.StaleVersion, func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch = 0 }),
		// 14 expiry range
		item0("expiry-0", msgkit.Bounds, func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = 0 }),
		item0("expiry-negative", msgkit.Bounds, func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = -1 }),
		item0("expiry-above-9999", msgkit.Bounds, func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = genesis.UnixSecondsMax + 1 }),
		// 15 business gate
		edit("gate-business-id-pattern", msgkit.Bounds, func(m *evidencev1.MsgSubmitAuditBatch) { m.BusinessId = "B1" }),
		// A business id that is not UTF-8 is off the id pattern too: BOUNDS
		// from the id rule, before the payload digest (whose encoder would
		// answer MALFORMED). A transaction cannot carry such a string
		// (protobuf-go refuses invalid UTF-8 in proto3 strings); the keeper
		// path can.
		edit("gate-business-id-not-utf8", msgkit.Bounds, func(m *evidencev1.MsgSubmitAuditBatch) { m.BusinessId = "\xff" }),
		envFault("gate-replay-other-module", msgkit.Replay, func(e *env, c *call) { e.peer.digests[c.msg.GetBusinessId()] = make([]byte, 32) }),
		// 16 duplicates
		envFault("duplicate-batch-id-used", msgkit.DuplicateID, func(e *env, c *call) {
			seeded := e.recordFor(c.msg, 0)
			seeded.EvidenceId = prefix(auditor1) + "seeded"
			e.k.SeedRecordForTest(e.unmetered(), seeded)
		}),
		envFault("duplicate-evidence-id-stored", msgkit.DuplicateID, func(e *env, c *call) {
			seeded := e.recordFor(c.msg, 0)
			seeded.BatchId = prefix(auditor1) + "b-seeded"
			e.k.SeedRecordForTest(e.unmetered(), seeded)
		}),
		edit("duplicate-evidence-id-in-batch", msgkit.DuplicateID, func(m *evidencev1.MsgSubmitAuditBatch) {
			m.Items[1].EvidenceId = m.Items[0].EvidenceId
		}),
		// 17 expired item
		item0("item-expiry-at-committed-time", msgkit.Expired, func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = now }),
		// 18 lease
		item0("lease-not-found", msgkit.NotFound, func(it *evidencev1.EvidenceItem) { it.LeaseId = "l-none" }),
		// T1 id prefix (D-17, Q2)
		edit("t1-batch-id-foreign-prefix", msgkit.Forbidden, func(m *evidencev1.MsgSubmitAuditBatch) { m.BatchId = prefix(auditor2) + "b-1" }),
		item0("t1-evidence-id-foreign-prefix", msgkit.Forbidden, func(it *evidencev1.EvidenceItem) { it.EvidenceId = prefix(auditor2) + "e1" }),
		item0("t1-evidence-id-no-prefix", msgkit.Forbidden, func(it *evidencev1.EvidenceItem) { it.EvidenceId = "e1" }),
		// T2 provider receipt (D-8)
		item0("t2-kind-provider-receipt", msgkit.Forbidden, func(it *evidencev1.EvidenceItem) {
			it.Kind = evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT
		}),
		// T3 lifetime (D-9, Q4)
		item0("t3-expiry-now-plus-86401", msgkit.Bounds, func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = now + 86401 }),
		// T4 binding (D-10, Q4)
		item0("t4-content-id-differs", msgkit.Conflict, func(it *evidencev1.EvidenceItem) { it.ContentId = "c3" }),
		item0("t4-content-version-differs", msgkit.Conflict, func(it *evidencev1.EvidenceItem) { it.ContentVersion = 2 }),
		// T5 lease held (D-11, Q4)
		envFault("t5-lease-ended", msgkit.Conflict, func(e *env, _ *call) {
			e.editLease("l1", func(l *leasesv1.Lease) { l.State, l.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_ENDED, false })
		}),
		envFault("t5-lease-expired", msgkit.Conflict, func(e *env, _ *call) {
			e.editLease("l1", func(l *leasesv1.Lease) { l.State, l.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_EXPIRED, false })
		}),
		envFault("t5-reservation-not-held", msgkit.Conflict, func(e *env, _ *call) {
			e.editLease("l1", func(l *leasesv1.Lease) { l.ReservationHeld = false })
		}),
		// T6 deadline (D-12, Q4)
		envFault("t6-lease-deadline-at-committed-time", msgkit.Expired, func(e *env, _ *call) {
			e.editLease("l1", func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = now })
		}),
		// T7 expiry within the lease deadline (D-18, Q4)
		envFault("t7-expiry-after-lease-deadline", msgkit.Conflict, func(e *env, _ *call) {
			e.editLease("l1", func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = now + itemLife - 1 })
		}),
		// T8 content (D-13, Q4)
		envFault("t8-content-withdrawn", msgkit.Withdrawn, func(e *env, _ *call) {
			e.putContent("c1", contentv1.ContentState_CONTENT_STATE_WITHDRAWN)
		}),
		envFault("t8-content-not-found", msgkit.NotFound, func(e *env, _ *call) {
			e.store(contentStore).Delete(contentKey("c1"))
		}),
		// T9 per-auditor epoch cap (D-15)
		envFault("t9-auditor-epoch-cap", msgkit.Bounds, func(e *env, _ *call) {
			e.k.SetWindowForTest(e.unmetered(), addr(auditor1), epochNow, evidence.MaxEvidencePerAuditorPerEpoch-1)
		}),
	}
}

// recordFor is the record item i of msg would write (RECORDED), for
// fixtures.
func (e *env) recordFor(msg *evidencev1.MsgSubmitAuditBatch, i int) *evidencev1.EvidenceRecord {
	it := msg.GetItems()[i]
	return &evidencev1.EvidenceRecord{
		EvidenceId: it.GetEvidenceId(), BatchId: msg.GetBatchId(), Kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
		State: evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED, SignerPubkey: pub(auditor1), LeaseId: it.GetLeaseId(),
		ContentId: it.GetContentId(), ContentVersion: 1, ObservationDigest: manifestOf(it.GetContentId()),
		Epoch: epochNow, ExpiryUnixSeconds: now + itemLife, SchemaVersion: 1, Attestation: true,
	}
}

// TestSubmitAuditBatchCheckOrder runs the ladder: every row of the check
// order, in order, cumulative and isolated, with its exact code, a
// byte-identical trial state of every store, and no event.
func TestSubmitAuditBatchCheckOrder(t *testing.T) {
	runLadder(t, checkOrderFaults())
}

// TestItemFaultsOnLastItem runs every item fault of the ladder alone on the
// LAST item of the two-item ladder batch, the first item valid: every step
// that names "each item" checks every item, not only the first (D-6), with
// the ladder's code, an unchanged trial state, and no event. A handler that
// checked only the first item would accept these messages (and record an
// off-pattern id or an expired item) or answer a later step's code. The
// stored-id half of step 16 is in TestDuplicateIDs; the tail checks T1-T8
// on the last of 32 items are TestBatchAtomicD6.
func TestItemFaultsOnLastItem(t *testing.T) {
	ran := 0
	for _, f := range checkOrderFaults() {
		if f.item == nil {
			continue
		}
		ran++
		t.Run(f.name, func(t *testing.T) {
			e := newEnv(t)
			msg := ladderBase(e)
			f.item(msg.Items[len(msg.Items)-1])
			e.requireReject(e.submit(e.fresh(), sign(msg, auditor1)), f.want, f.name)
		})
	}
	if ran < 30 {
		t.Fatalf("only %d item faults", ran)
	}
	extra := []struct {
		name string
		want msgkit.Code
		last func(e *env, m *evidencev1.MsgSubmitAuditBatch)
	}{
		{"item-nil", msgkit.Malformed, func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { m.Items[1] = nil }},
		// An off-pattern evidence id that still carries the signer prefix:
		// only step 9 refuses it.
		{"evidence-id-pattern-with-prefix", msgkit.Bounds, func(e *env, m *evidencev1.MsgSubmitAuditBatch) {
			m.Items[1] = e.item(auditor1, "E2", "l2")
		}},
		{"evidence-id-repeats-first", msgkit.DuplicateID, func(_ *env, m *evidencev1.MsgSubmitAuditBatch) {
			m.Items[1].EvidenceId = m.Items[0].EvidenceId
		}},
	}
	for _, tc := range extra {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			msg := ladderBase(e)
			tc.last(e, msg)
			e.requireReject(e.submit(e.fresh(), sign(msg, auditor1)), tc.want, tc.name)
		})
	}
}

// TestLadderCoversEveryCode: the ladder exercises every failure code the
// handler returns, and its rows are uniquely named.
func TestLadderCoversEveryCode(t *testing.T) {
	seen := map[msgkit.Code]bool{}
	names := map[string]bool{}
	for _, f := range checkOrderFaults() {
		if names[f.name] {
			t.Fatalf("duplicate row %s", f.name)
		}
		names[f.name] = true
		seen[f.want] = true
	}
	for _, code := range []msgkit.Code{msgkit.Bounds, msgkit.Malformed, msgkit.Unauthorized, msgkit.Forbidden, msgkit.OutOfRange,
		msgkit.WrongDomain, msgkit.StaleVersion, msgkit.Replay, msgkit.DuplicateID, msgkit.Expired, msgkit.NotFound,
		msgkit.Conflict, msgkit.Withdrawn} {
		if !seen[code] {
			t.Fatalf("no ladder row returns %s", code)
		}
	}
}

// TestTrialSnapshotDetectsEarlyWrite: the reject check of the ladder sees a
// handler that writes to any mounted store, or emits an event, before it
// fails, and accepts one that fails cleanly.
func TestTrialSnapshotDetectsEarlyWrite(t *testing.T) {
	e := newEnv(t)
	for name, key := range e.keys {
		r := e.exec(e.fresh(), func(ctx sdk.Context) error {
			ctx.KVStore(key).Set([]byte{0x0e, 'x'}, []byte{1})
			return msgkit.Bounds.Err()
		})
		if r.code != msgkit.Bounds || r.trialSame {
			t.Fatalf("write to %s before a failure was not seen", name)
		}
	}
	r := e.exec(e.fresh(), func(ctx sdk.Context) error {
		ctx.EventManager().EmitEvent(sdk.NewEvent(evidence.EventState))
		return msgkit.Bounds.Err()
	})
	if len(r.events) != 1 {
		t.Fatal("event before a failure was not seen")
	}
	r = e.exec(e.fresh(), func(sdk.Context) error { return msgkit.Bounds.Err() })
	if !r.trialSame || len(r.events) != 0 || r.code != msgkit.Bounds {
		t.Fatal("a clean failure was reported as a write")
	}
}
