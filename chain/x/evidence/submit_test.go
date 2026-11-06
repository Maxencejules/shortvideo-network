package evidence_test

import (
	"bytes"
	"fmt"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence/evidencetest"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestRecordFieldsAndDerivedState (REQ-MODU-005; spec cases
// evidence-digest-recorded and evidence-rejected): an accepted batch writes
// exactly one constant-size record per item, RECORDED when the observation
// digest equals the lease's manifest digest and REJECTED for any other
// well-formed digest, with the batch marker, the lease index rows, the
// auditor window, and the business id; it changes no lease or content
// byte. The records read back through StoredRecord and Query/Evidence.
func TestRecordFieldsAndDerivedState(t *testing.T) {
	e := newEnv(t)
	other := e.item(auditor1, "e2", "l2")
	other.ObservationDigest = digestOf("bytes the provider served")
	msg := signed(auditor1, "1", e.item(auditor1, "e1", "l1"), other)
	before := evidencetestSnapshot(e)
	r := e.submit(e.fresh(), msg)
	e.requireOK(r, "submit")
	if !bytes.Equal(before, evidencetestSnapshot(e)) {
		t.Fatal("an accepted batch changed the lease or content store")
	}
	want := []*evidencev1.EvidenceRecord{
		{
			EvidenceId: prefix(auditor1) + "e1", BatchId: msg.GetBatchId(), Kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
			State: evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED, SignerPubkey: pub(auditor1), LeaseId: "l1", ContentId: "c1",
			ContentVersion: 1, ObservationDigest: manifestOf("c1"), Epoch: epochNow, ExpiryUnixSeconds: now + itemLife,
			SchemaVersion: 1, Attestation: true,
		},
		{
			EvidenceId: prefix(auditor1) + "e2", BatchId: msg.GetBatchId(), Kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
			State: evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED, SignerPubkey: pub(auditor1), LeaseId: "l2", ContentId: "c2",
			ContentVersion: 1, ObservationDigest: digestOf("bytes the provider served"), Epoch: epochNow, ExpiryUnixSeconds: now + itemLife,
			SchemaVersion: 1, Attestation: true,
		},
	}
	for _, w := range want {
		if got := e.stored(w.GetEvidenceId()); !proto.Equal(got, w) {
			t.Fatalf("stored record\n got %v\nwant %v", got, w)
		}
		resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{EvidenceId: w.GetEvidenceId()})
		if err != nil || len(resp.GetRecords()) != 1 || !proto.Equal(resp.GetRecords()[0], w) {
			t.Fatalf("query %v %v", resp, err)
		}
	}
	store := e.store(evidence.ModuleName)
	if !bytes.Equal(store.Get(evidence.BatchKey(msg.GetBatchId())), evidence.Marker) ||
		!bytes.Equal(store.Get(evidence.LeaseIndexKey("l1", want[0].GetEvidenceId())), evidence.Marker) ||
		!bytes.Equal(store.Get(evidence.LeaseIndexKey("l2", want[1].GetEvidenceId())), evidence.Marker) {
		t.Fatal("derived rows missing")
	}
	if epoch, count := e.k.WindowForTest(e.unmetered(), addr(auditor1)); epoch != epochNow || count != 2 {
		t.Fatalf("window (%d, %d)", epoch, count)
	}
	if _, found := e.k.BusinessDigest(e.unmetered(), msg.GetBusinessId()); !found {
		t.Fatal("business id not committed")
	}
	e.requireInvariants()
}

// TestRecordStateNeedsTheDigestAlgorithm (DR-09-14): RECORDED needs the
// algorithm AND the value of the lease's manifest digest. A lease whose
// stored digest has the same 32-byte value under another algorithm
// (UNSPECIFIED) gets REJECTED. x/leases stores only SHA-256 manifest digests,
// so only the keeper path with an edited lease reaches this case.
func TestRecordStateNeedsTheDigestAlgorithm(t *testing.T) {
	e := newEnv(t)
	item := e.item(auditor1, "e1", "l1")
	e.editLease("l1", func(l *leasesv1.Lease) {
		l.ManifestDigest = &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED, Value: l.GetManifestDigest().GetValue()}
	})
	if !bytes.Equal(item.GetObservationDigest().GetValue(), e.lease("l1").GetManifestDigest().GetValue()) {
		t.Fatal("the item's observation value must equal the lease's manifest digest value")
	}
	e.requireOK(e.submit(e.fresh(), signed(auditor1, "1", item)), "submit")
	if got := e.stored(prefix(auditor1) + "e1").GetState(); got != evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED {
		t.Fatalf("state %v, want REJECTED: the algorithm differs", got)
	}
	e.requireInvariants()
}

// evidencetestSnapshot is the leases and content stores.
func evidencetestSnapshot(e *env) []byte {
	return evidencetest.StoreSnapshot(e.unmetered(), e.otherKeys()...)
}

// TestEvidenceEvents (D-16): per item, in item order, EvidenceAccepted
// {evidence_id, batch_id, kind short name, attestation true} and right after
// it evidence_state {evidence_id, lease_id, state}; nothing else. The typed
// event is emitted for REJECTED records too: "accepted" means written to the
// ledger, not that the check passed.
func TestEvidenceEvents(t *testing.T) {
	e := newEnv(t)
	other := e.item(auditor1, "e2", "l2")
	other.ObservationDigest = digestOf("other")
	msg := signed(auditor1, "1", e.item(auditor1, "e1", "l1"), other)
	r := e.submit(e.fresh(), msg)
	e.requireOK(r, "submit")
	if len(r.events) != 4 {
		t.Fatalf("events %v", r.events)
	}
	states := []string{"RECORDED", "REJECTED"}
	for i, it := range msg.GetItems() {
		typed, err := msgkit.ParseTyped(r.events[2*i])
		if err != nil {
			t.Fatal(err)
		}
		want := &eventsv1.EvidenceAccepted{EvidenceId: it.GetEvidenceId(), BatchId: msg.GetBatchId(), Kind: "AUDITOR_ATTESTATION", Attestation: true}
		if !proto.Equal(typed, want) {
			t.Fatalf("event %d: %v, want %v", i, typed, want)
		}
		state := r.events[2*i+1]
		if state.Type != evidence.EventState || len(state.Attributes) != 3 ||
			attr(state, evidence.AttrEvidenceID) != it.GetEvidenceId() || attr(state, evidence.AttrLeaseID) != it.GetLeaseId() ||
			attr(state, evidence.AttrState) != states[i] {
			t.Fatalf("state event %d: %v", i, state)
		}
	}
	if names := r.typedNames(); len(names) != 2 || names[0] != "EvidenceAccepted" {
		t.Fatalf("typed events %v", names)
	}
}

func attr(ev abci.Event, key string) string {
	for _, a := range ev.Attributes {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}

// TestBatchCheckMajorOrderD6: a step finishes over every item before the
// next step starts, so an item that fails an earlier step decides even when
// an earlier item fails a later step.
func TestBatchCheckMajorOrderD6(t *testing.T) {
	cases := []struct {
		name   string
		first  func(*evidencev1.EvidenceItem)
		second func(*evidencev1.EvidenceItem)
		want   msgkit.Code
	}{
		{"id-pattern-vs-digest", func(it *evidencev1.EvidenceItem) { it.LeaseId = "L1" },
			func(it *evidencev1.EvidenceItem) { it.ObservationDigest.Algorithm = 0 }, msgkit.Malformed},
		{"expired-vs-kind", func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = now },
			func(it *evidencev1.EvidenceItem) { it.Kind = 4 }, msgkit.Malformed},
		{"lease-absent-vs-attestation", func(it *evidencev1.EvidenceItem) { it.LeaseId = "l-none" },
			func(it *evidencev1.EvidenceItem) { it.Attestation = false }, msgkit.Malformed},
		{"tail-binding-vs-expiry-range", func(it *evidencev1.EvidenceItem) { it.ContentId = "c3" },
			func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = 0 }, msgkit.Bounds},
		{"tail-provider-receipt-vs-lease-absent", func(it *evidencev1.EvidenceItem) {
			it.Kind = evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT
		}, func(it *evidencev1.EvidenceItem) { it.LeaseId = "l-none" }, msgkit.NotFound},
		// Steps 2, 9, and 17 on the second item against a later step on
		// the first.
		{"presence-second-vs-digest-bounds-first", func(it *evidencev1.EvidenceItem) {
			it.ObservationDigest.Value = it.ObservationDigest.Value[:31]
		}, func(it *evidencev1.EvidenceItem) { it.LeaseId = "" }, msgkit.Malformed},
		{"id-pattern-second-vs-kind-first", func(it *evidencev1.EvidenceItem) { it.Kind = 4 },
			func(it *evidencev1.EvidenceItem) { it.LeaseId = "L2" }, msgkit.Bounds},
		{"expired-second-vs-lease-absent-first", func(it *evidencev1.EvidenceItem) { it.LeaseId = "l-none" },
			func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = now }, msgkit.Expired},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			first, second := e.item(auditor1, "e1", "l1"), e.item(auditor1, "e2", "l2")
			tc.first(first)
			tc.second(second)
			e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", first, second)), tc.want, tc.name)
		})
	}
}

// TestBatchAtomicD6: a 32-item batch whose LAST item fails one tail check
// writes nothing for any item.
func TestBatchAtomicD6(t *testing.T) {
	tails := []struct {
		name  string
		want  msgkit.Code
		apply func(e *env, it *evidencev1.EvidenceItem)
	}{
		{"t1-prefix", msgkit.Forbidden, func(_ *env, it *evidencev1.EvidenceItem) { it.EvidenceId = prefix(auditor2) + "last" }},
		{"t2-provider-receipt", msgkit.Forbidden, func(_ *env, it *evidencev1.EvidenceItem) {
			it.Kind = evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT
		}},
		{"t3-lifetime", msgkit.Bounds, func(_ *env, it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds = now + 86401 }},
		{"t4-binding", msgkit.Conflict, func(_ *env, it *evidencev1.EvidenceItem) { it.ContentVersion = 2 }},
		{"t5-not-held", msgkit.Conflict, func(e *env, it *evidencev1.EvidenceItem) {
			e.editLease("l3", func(l *leasesv1.Lease) { l.ReservationHeld = false })
		}},
		{"t6-deadline", msgkit.Expired, func(e *env, it *evidencev1.EvidenceItem) {
			e.editLease("l3", func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = now })
		}},
		{"t7-expiry-after-deadline", msgkit.Conflict, func(e *env, it *evidencev1.EvidenceItem) {
			e.editLease("l3", func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = now + 10 })
		}},
		{"t8-withdrawn", msgkit.Withdrawn, func(e *env, it *evidencev1.EvidenceItem) { e.withdraw("c3") }},
	}
	for _, tc := range tails {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			items := e.items(auditor1, "a", "l1", 31)
			last := e.item(auditor1, "last", "l3")
			tc.apply(e, last)
			items = append(items, last)
			e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", items...)), tc.want, tc.name)
		})
	}
	t.Run("t9-window", func(t *testing.T) {
		e := newEnv(t)
		e.k.SetWindowForTest(e.unmetered(), addr(auditor1), epochNow, evidence.MaxEvidencePerAuditorPerEpoch-31)
		e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", e.items(auditor1, "a", "l1", 32)...)), msgkit.Bounds, "window")
		e.requireOK(e.submit(e.fresh(), signed(auditor1, "2", e.items(auditor1, "b", "l1", 31)...)), "fits")
	})
}

// TestDuplicateInBatchD6: an evidence id repeated inside one batch is
// DUPLICATE_ID; one lease may appear under several evidence ids.
func TestDuplicateInBatchD6(t *testing.T) {
	e := newEnv(t)
	first, second := e.item(auditor1, "e1", "l1"), e.item(auditor1, "e1", "l2")
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", first, second)), msgkit.DuplicateID, "repeated id")
	e.requireOK(e.submit(e.fresh(), signed(auditor1, "2", e.item(auditor1, "e1", "l1"), e.item(auditor1, "e2", "l1"))), "same lease twice")
	e.requireInvariants()
}

// TestBatchSizeAndGovernedLimit (spec case governed-limit-read): 1 and 32
// items are accepted, 0 and 33 are BOUNDS; a governed max_evidence_batch of
// 2 admits 2 and refuses 3; a governed 0 or a value above the ceiling falls
// back to the ceiling 32 (msgkit.Effective).
func TestBatchSizeAndGovernedLimit(t *testing.T) {
	e := newEnv(t)
	e.requireOK(e.submit(e.fresh(), signed(auditor1, "one", e.item(auditor1, "one", "l1"))), "1 item")
	e.requireOK(e.submit(e.fresh(), signed(auditor1, "max", e.items(auditor1, "max", "l2", 32)...)), "32 items")
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "none")), msgkit.Bounds, "0 items")
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "over", e.items(auditor1, "over", "l2", 33)...)), msgkit.Bounds, "33 items")
	e.auth.limits[msgkit.LimitMaxEvidenceBatch] = 2
	e.requireOK(e.submit(e.fresh(), signed(auditor1, "g2", e.items(auditor1, "g2", "l3", 2)...)), "governed 2: 2 items")
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "g3", e.items(auditor1, "g3", "l3", 3)...)), msgkit.Bounds, "governed 2: 3 items")
	for _, governed := range []uint64{0, 33} {
		e.auth.limits[msgkit.LimitMaxEvidenceBatch] = governed
		e.requireOK(e.submit(e.fresh(), signed(auditor1, fmt.Sprintf("c%d", governed), e.items(auditor1, fmt.Sprintf("c%d", governed), "l3", 32)...)),
			fmt.Sprintf("governed %d falls back to 32", governed))
	}
	e.requireInvariants()
}

// TestAlteredObservationD1: the signature covers every envelope field of
// every item, the item order, the batch id, the epoch, the schema version,
// the key label, and the signer. Any change after signing (or a signature
// made over another value of a field) is UNAUTHORIZED with the store
// unchanged, before the field's own check.
func TestAlteredObservationD1(t *testing.T) {
	type alteration struct {
		name string
		// before edits the message before signing; after edits it after.
		before, after func(e *env, m *evidencev1.MsgSubmitAuditBatch)
	}
	itemEdit := func(f func(*evidencev1.EvidenceItem)) func(*env, *evidencev1.MsgSubmitAuditBatch) {
		return func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { f(m.Items[1]) }
	}
	cases := []alteration{
		{name: "attestation", after: itemEdit(func(it *evidencev1.EvidenceItem) { it.Attestation = false })},
		{name: "content_id", after: itemEdit(func(it *evidencev1.EvidenceItem) { it.ContentId = "c1" })},
		{name: "content_version", after: itemEdit(func(it *evidencev1.EvidenceItem) { it.ContentVersion = 2 })},
		{name: "evidence_id", after: itemEdit(func(it *evidencev1.EvidenceItem) { it.EvidenceId += "x" })},
		{name: "expiry_unix_seconds", after: itemEdit(func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds++ })},
		{name: "kind", after: itemEdit(func(it *evidencev1.EvidenceItem) {
			it.Kind = evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT
		})},
		{name: "lease_id", after: itemEdit(func(it *evidencev1.EvidenceItem) { it.LeaseId = "l3" })},
		{name: "observation_sha256", after: itemEdit(func(it *evidencev1.EvidenceItem) { it.ObservationDigest = digestOf("altered") })},
		{name: "epoch", after: func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { m.Epoch++ }},
		{name: "schema_version-signed-as-2", before: func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { m.SchemaVersion = 2 },
			after: func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { m.SchemaVersion = 1 }},
		{name: "signer_key_id", after: func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { m.AuditorSignature.KeyId = "key-other" }},
		{name: "batch_id", after: func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { m.BatchId += "x" }},
		{name: "signer-signed-for-another-address", before: func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { m.Signer = outsiderAcct },
			after: func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { m.Signer = addr(auditor1) }},
		{name: "item-order-swapped", after: func(_ *env, m *evidencev1.MsgSubmitAuditBatch) {
			m.Items[0], m.Items[1] = m.Items[1], m.Items[0]
		}},
		{name: "item-removed", after: func(_ *env, m *evidencev1.MsgSubmitAuditBatch) { m.Items = m.Items[:1] }},
		{name: "item-added", after: func(e *env, m *evidencev1.MsgSubmitAuditBatch) {
			m.Items = append(m.Items, e.item(auditor1, "e3", "l3"))
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			msg := batch(auditor1, "1", e.item(auditor1, "e1", "l1"), e.item(auditor1, "e2", "l2"))
			if tc.before != nil {
				tc.before(e, msg)
			}
			sign(msg, auditor1)
			tc.after(e, msg)
			e.requireReject(e.submit(e.fresh(), msg), msgkit.Unauthorized, tc.name)
		})
	}
	// Control: the same message without an alteration commits.
	e := newEnv(t)
	e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1"), e.item(auditor1, "e2", "l2")))
}

// TestSpecIntegrityCaseIsUnauthorizedD1 reproduces spec case
// evidence-signature-unbound (cases.py:1716): the signature binds another
// observation digest than the message carries. The spec answers INTEGRITY;
// a real ed25519 signature cannot tell which field changed, so the chain
// answers UNAUTHORIZED (D-1, DR-09-15) and stores nothing.
func TestSpecIntegrityCaseIsUnauthorizedD1(t *testing.T) {
	e := newEnv(t)
	msg := batch(auditor1, "unbound", e.item(auditor1, "ev-unbound", "l1"))
	msg.Items[0].ObservationDigest = digestOf("other-observation")
	sign(msg, auditor1)
	msg.Items[0].ObservationDigest = manifestOf("c1")
	e.requireReject(e.submit(e.fresh(), msg), msgkit.Unauthorized, "unbound observation")
	if _, found := e.k.StoredRecord(e.unmetered(), prefix(auditor1)+"ev-unbound"); found {
		t.Fatal("unbound observation was stored")
	}
}

// TestAuditorKeyBindingD2 (owner ruling on Q1): the signature's key must
// derive the outer signer. Auditor-2's valid signature in a batch sent by
// auditor-1 is UNAUTHORIZED; auditor-1's exact message (same business id
// and payload) sent by auditor-2 is UNAUTHORIZED, not idempotent; a key
// whose address has no role cannot be used through an auditor account.
func TestAuditorKeyBindingD2(t *testing.T) {
	e := newEnv(t)
	msg := batch(auditor1, "1", e.item(auditor1, "e1", "l1"))
	msg.AuditorSignature.PublicKey = pub(auditor2)
	sign(msg, auditor2)
	e.requireReject(e.submit(e.fresh(), msg), msgkit.Unauthorized, "auditor-2 key for auditor-1")

	original := signed(auditor1, "2", e.item(auditor1, "e2", "l1"))
	e.mustSubmit(original)
	copied := proto.Clone(original).(*evidencev1.MsgSubmitAuditBatch)
	copied.Signer = addr(auditor2)
	e.requireReject(e.submit(e.fresh(), copied), msgkit.Unauthorized, "auditor-1's message sent by auditor-2")
	e.requireIdempotent(e.submit(e.fresh(), original), "auditor-1's own retry")
}

// TestBroadcasterRelayForbidden (owner ruling on Q1: no BROADCASTER relay):
// a valid batch signed by auditor-1's key and sent by a BROADCASTER account
// is FORBIDDEN at the role check, as for every non-AUDITOR role.
func TestBroadcasterRelayForbidden(t *testing.T) {
	e := newEnv(t)
	msg := batch(auditor1, "1", e.item(auditor1, "e1", "l1"))
	msg.Signer = broadcasterAcct
	sign(msg, auditor1)
	e.requireReject(e.submit(e.fresh(), msg), msgkit.Forbidden, "broadcaster relay")
	msg.Signer = addr(auditor1)
	sign(msg, auditor1)
	e.mustSubmit(msg)
}

// TestForeignAuditorCannotTakeEvidenceIDsD17 (owner ruling on Q2): auditor-2
// sends, before auditor-1, a batch signed by its own key that reuses
// auditor-1's pending batch id and evidence ids: FORBIDDEN, store unchanged;
// auditor-1's batch then commits. A batch whose batch id has the right
// prefix but one evidence id another prefix is FORBIDDEN. An id equal to
// the prefix alone is a valid id, accepted only for its own auditor.
func TestForeignAuditorCannotTakeEvidenceIDsD17(t *testing.T) {
	e := newEnv(t)
	honest := signed(auditor1, "1", e.item(auditor1, "e1", "l1"), e.item(auditor1, "e2", "l2"))
	squat := batch(auditor2, "squat", e.item(auditor2, "x", "l1"), e.item(auditor2, "y", "l2"))
	squat.BatchId = honest.GetBatchId()
	squat.Items[0].EvidenceId, squat.Items[1].EvidenceId = honest.Items[0].EvidenceId, honest.Items[1].EvidenceId
	sign(squat, auditor2)
	e.requireReject(e.submit(e.fresh(), squat), msgkit.Forbidden, "auditor-2 takes auditor-1's ids")
	e.mustSubmit(honest)

	mixed := signed(auditor1, "2", e.item(auditor1, "e3", "l1"), e.item(auditor2, "e4", "l2"))
	e.requireReject(e.submit(e.fresh(), mixed), msgkit.Forbidden, "one foreign evidence id")

	bare := batch(auditor2, "bare", e.item(auditor2, "", "l1"))
	if bare.Items[0].GetEvidenceId() != prefix(auditor2) || !msgkit.ValidID(prefix(auditor2)) {
		t.Fatalf("bare prefix id %q", bare.Items[0].GetEvidenceId())
	}
	stolen := proto.Clone(bare).(*evidencev1.MsgSubmitAuditBatch)
	stolen.Signer, stolen.AuditorSignature.PublicKey, stolen.AuditorSignature.KeyId = addr(auditor1), pub(auditor1), "key-auditor-1"
	stolen.BatchId = prefix(auditor1) + "b-bare"
	sign(stolen, auditor1)
	e.requireReject(e.submit(e.fresh(), stolen), msgkit.Forbidden, "auditor-1 uses auditor-2's bare prefix id")
	e.mustSubmit(sign(bare, auditor2))
	e.requireInvariants()
}

// TestEpochRuleD3 (owner ruling on Q4): only the current round is
// accepted. At k*3600-1 epoch k-1 is current and k is a future round; at
// k*3600 epoch k is current, k-1 an expired round, k+1 a future round;
// epoch 0 is a value and is STALE_VERSION. An idempotent retry after the
// boundary is STALE_VERSION (the epoch check runs before the gate,
// DR-09-19).
func TestEpochRuleD3(t *testing.T) {
	k := evidence.EpochAt(t0)
	boundary := int64(k) * evidence.EpochSeconds
	e := newEnv(t)
	at := func(unix int64, epoch uint64, suffix string) result {
		msg := batch(auditor1, suffix, e.item(auditor1, suffix, "l1"))
		msg.Epoch = epoch
		msg.Items[0].ExpiryUnixSeconds = unix + itemLife
		return e.submit(e.at(unix), sign(msg, auditor1))
	}
	e.requireOK(at(boundary-1, k-1, "a"), "epoch k-1 at k*3600-1")
	e.requireReject(at(boundary-1, k, "b"), msgkit.StaleVersion, "future round at k*3600-1")
	e.requireOK(at(boundary, k, "c"), "epoch k at k*3600")
	e.requireReject(at(boundary, k-1, "d"), msgkit.StaleVersion, "expired round at k*3600")
	e.requireReject(at(boundary, k+1, "e"), msgkit.StaleVersion, "future round at k*3600")
	e.requireReject(at(boundary, 0, "f"), msgkit.StaleVersion, "epoch 0")

	// Retry of the committed batch "c" in the next epoch.
	retry := batch(auditor1, "c", e.item(auditor1, "c", "l1"))
	retry.Epoch = k
	retry.Items[0].ExpiryUnixSeconds = boundary + itemLife
	sign(retry, auditor1)
	e.requireIdempotent(e.submit(e.at(boundary+evidence.EpochSeconds-1), retry), "retry inside the epoch")
	e.requireReject(e.submit(e.at(boundary+evidence.EpochSeconds), retry), msgkit.StaleVersion, "retry after the boundary")
}

// TestSchemaVersionD5: schema_version 0 is MALFORMED and any value but 1 is
// OUT_OF_RANGE, after the role check (a non-auditor learns nothing).
func TestSchemaVersionD5(t *testing.T) {
	e := newEnv(t)
	for version, want := range map[uint32]msgkit.Code{0: msgkit.Malformed, 2: msgkit.OutOfRange, 99: msgkit.OutOfRange} {
		msg := batch(auditor1, "1", e.item(auditor1, "e1", "l1"))
		msg.SchemaVersion = version
		e.requireReject(e.submit(e.fresh(), sign(msg, auditor1)), want, fmt.Sprintf("schema %d", version))
		msg.Signer = outsiderAcct
		e.requireReject(e.submit(e.fresh(), sign(msg, auditor1)), msgkit.Unauthorized, "schema check after the role")
	}
	e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1")))
	if e.stored(prefix(auditor1)+"e1").GetSchemaVersion() != 1 {
		t.Fatal("record schema version")
	}
}

// TestDuplicateIDs: a stored evidence id, a used batch id with new evidence
// ids, and a business id held by another module's ledger are rejected;
// batch and evidence ids are never reusable.
func TestDuplicateIDs(t *testing.T) {
	e := newEnv(t)
	first := signed(auditor1, "1", e.item(auditor1, "e1", "l1"))
	e.mustSubmit(first)
	again := signed(auditor1, "2", e.item(auditor1, "e1", "l2"))
	e.requireReject(e.submit(e.fresh(), again), msgkit.DuplicateID, "stored evidence id")
	sameBatch := batch(auditor1, "3", e.item(auditor1, "e3", "l1"))
	sameBatch.BatchId = first.GetBatchId()
	e.requireReject(e.submit(e.fresh(), sign(sameBatch, auditor1)), msgkit.DuplicateID, "used batch id")
	e.peer.digests["biz-4"] = make([]byte, 32)
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "4", e.item(auditor1, "e4", "l1"))), msgkit.Replay, "business id of another module")

	// The stored-id check covers every item, not only the first (I3): a
	// later item reusing a stored id is DUPLICATE_ID, and a stored REJECTED
	// record cannot be rewritten RECORDED under its id.
	wrong := e.item(auditor1, "r1", "l2")
	wrong.ObservationDigest = digestOf("bytes the provider served")
	e.mustSubmit(signed(auditor1, "5", wrong))
	before := e.snapshot(e.unmetered())
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "6", e.item(auditor1, "e6", "l1"), e.item(auditor1, "r1", "l2"))),
		msgkit.DuplicateID, "stored REJECTED id on the second item, now with the manifest digest")
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "7", e.item(auditor1, "e7", "l1"), e.item(auditor1, "e8", "l3"), e.item(auditor1, "e1", "l1"))),
		msgkit.DuplicateID, "stored id on the last item")
	if !bytes.Equal(before, e.snapshot(e.unmetered())) {
		t.Fatal("a refused batch changed the store")
	}
	if got := e.stored(prefix(auditor1) + "r1").GetState(); got != evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED {
		t.Fatalf("stored state %s", got)
	}
	e.requireInvariants()
}

// TestBusinessIDTakenFirst (residual risk): business ids form one
// namespace across modules and are not signed, so any admitted account
// (the audited provider among them) that commits another message under a
// pending batch's business id first makes the batch REPLAY, and the whole
// atomic batch fails. The auditor's retry needs only a new business id: the
// unchanged signature still verifies (DR-09-28 would bind business ids to
// the signer or to one module).
func TestBusinessIDTakenFirst(t *testing.T) {
	e := newEnv(t)
	msg := signed(auditor1, "1", e.item(auditor1, "e1", "l1"), e.item(auditor1, "e2", "l2"))
	e.peer.digests[msg.GetBusinessId()] = make([]byte, 32)
	e.requireReject(e.submit(e.fresh(), msg), msgkit.Replay, "business id taken by another module")
	retry := proto.Clone(msg).(*evidencev1.MsgSubmitAuditBatch)
	retry.BusinessId = "biz-retry"
	if !bytes.Equal(retry.GetAuditorSignature().GetSignature(), msg.GetAuditorSignature().GetSignature()) {
		t.Fatal("the retry was re-signed")
	}
	e.requireOK(e.submit(e.fresh(), retry), "retry under a new business id with the unchanged signature")
	e.stored(prefix(auditor1) + "e1")
	e.stored(prefix(auditor1) + "e2")
	e.requireInvariants()
}

// TestExpiryRangeEdges: step 14's range [1, 253402300799] is inclusive at
// both ends. Expiry 1 passes step 14 and is EXPIRED at step 17;
// 253402300799 passes steps 14 and 17 and, with an absent lease, is
// NOT_FOUND at step 18. These are the spec's codes for both edges
// (check_time, spec_machine.py:189).
func TestExpiryRangeEdges(t *testing.T) {
	e := newEnv(t)
	first := e.item(auditor1, "e1", "l1")
	first.ExpiryUnixSeconds = genesis.UnixSecondsMin
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", first)), msgkit.Expired, "expiry 1")
	last := e.item(auditor1, "e2", "l1")
	last.ExpiryUnixSeconds = genesis.UnixSecondsMax
	last.LeaseId = "l-none"
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "2", last)), msgkit.NotFound, "expiry 253402300799")
	for _, outside := range []int64{genesis.UnixSecondsMin - 1, genesis.UnixSecondsMax + 1} {
		it := e.item(auditor1, "e3", "l1")
		it.ExpiryUnixSeconds = outside
		it.LeaseId = "l-none"
		e.requireReject(e.submit(e.fresh(), signed(auditor1, "3", it)), msgkit.Bounds, fmt.Sprintf("expiry %d", outside))
	}
}

// TestIdempotentReplay: the same business id and payload is an idempotent
// success (code 0, one evidence_result event, no typed event, store
// byte-equal) in any later transaction of the same epoch, also after the
// lease ended and after the item's expiry (the gate runs before those
// checks, as the spec probe shows); a changed payload under the same
// business id is REPLAY; after the epoch boundary the retry is
// STALE_VERSION (the epoch check runs before the gate, DR-09-19).
func TestIdempotentReplay(t *testing.T) {
	e := newEnv(t)
	short := e.item(auditor1, "e1", "l1")
	short.ExpiryUnixSeconds = now + 60
	msg := signed(auditor1, "1", short)
	e.mustSubmit(msg)
	// Another transaction (other bytes, so another hash) carrying the same
	// message.
	replay := e.submit(e.fresh().WithTxBytes(bytes.Repeat([]byte{7}, 300)), msg)
	e.requireIdempotent(replay, "replay in another transaction")
	if len(replay.typedNames()) != 0 {
		t.Fatal("idempotent replay emitted a typed event")
	}
	changed := signed(auditor1, "1", e.item(auditor1, "e1", "l1"), e.item(auditor1, "e2", "l2"))
	e.requireReject(e.submit(e.fresh(), changed), msgkit.Replay, "changed payload")
	e.editLease("l1", func(l *leasesv1.Lease) { l.State, l.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_ENDED, false })
	e.requireIdempotent(e.submit(e.fresh(), msg), "replay after the lease ended")
	e.requireIdempotent(e.submit(e.at(now+60), msg), "replay at the item's expiry")
	e.requireIdempotent(e.submit(e.at(t0+evidence.EpochSeconds-1), msg), "replay at the last second of the epoch")
	e.requireReject(e.submit(e.at(t0+evidence.EpochSeconds), msg), msgkit.StaleVersion, "replay in the next epoch")
}

// TestSameObservationNewIDIsAccepted documents the reading of "duplicate
// evidence": a duplicate is a duplicate evidence id or batch id, not a
// repeated observation. The same (auditor, lease, epoch, digest) under a
// new evidence id is accepted (DR-09-06 (b) would refuse it).
func TestSameObservationNewIDIsAccepted(t *testing.T) {
	e := newEnv(t)
	e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1")))
	e.mustSubmit(signed(auditor1, "2", e.item(auditor1, "e2", "l1")))
	resp, err := e.query(now, &evidencev1.QueryEvidenceRequest{LeaseId: "l1"})
	if err != nil || len(resp.GetRecords()) != 2 {
		t.Fatalf("records of l1: %v %v", resp, err)
	}
}

// TestEvidenceKindHasNoProofValues (REQ-MODU-005, REQ-EVID-007): the
// generated kind enum has exactly 0, 1, 2 and reserves 3-5 with the
// storage-proof, unique-replica, and future-delivery names; kinds 3, 4, 5
// are MALFORMED with the store unchanged; a genesis record of kind 4 is
// invalid.
func TestEvidenceKindHasNoProofValues(t *testing.T) {
	enum := evidencev1.EvidenceKind(0).Descriptor()
	if enum.Values().Len() != 3 {
		t.Fatalf("%d kind values", enum.Values().Len())
	}
	for _, n := range []int32{3, 4, 5} {
		if enum.Values().ByNumber(protoreflect.EnumNumber(n)) != nil || !enum.ReservedRanges().Has(protoreflect.EnumNumber(n)) {
			t.Fatalf("kind %d is not reserved", n)
		}
	}
	for _, name := range []string{"EVIDENCE_KIND_STORAGE_PROOF", "EVIDENCE_KIND_UNIQUE_REPLICA_PROOF", "EVIDENCE_KIND_FUTURE_DELIVERY"} {
		if !enum.ReservedNames().Has(protoreflect.Name(name)) {
			t.Fatalf("%s is not reserved", name)
		}
	}
	e := newEnv(t)
	for _, kind := range []evidencev1.EvidenceKind{3, 4, 5} {
		it := e.item(auditor1, "e1", "l1")
		it.Kind = kind
		e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", it)), msgkit.Malformed, fmt.Sprintf("kind %d", kind))
	}
	g := validGenesis()
	g.Records[0].Kind = 4
	if evidence.ValidateGenesis(g) == nil {
		t.Fatal("a genesis record of kind 4 validated")
	}
}

// TestEvidenceItemHasNoRejectFlag (DEC-010; spec case
// evidence-reject-flag-unknown, cases.py:1044): no submitter flag can mark
// an item rejected. EvidenceItem has exactly its eight fields and reserves
// field number 9 and the name "reject". The spec answers such a flag with
// UNKNOWN_FIELD at its step 1; on chain an item carrying field 9 fails the
// app's strict transaction decoding before any handler (SDK codespace sdk,
// code 2; docs/architecture/registries.md), which is INT's app test
// (TestEvidenceMessagesDecode).
func TestEvidenceItemHasNoRejectFlag(t *testing.T) {
	d := (&evidencev1.EvidenceItem{}).ProtoReflect().Descriptor()
	if !d.ReservedRanges().Has(9) || !d.ReservedNames().Has("reject") || d.Fields().ByName("reject") != nil {
		t.Fatal("EvidenceItem does not reserve the reject flag")
	}
	want := []protoreflect.Name{"evidence_id", "kind", "lease_id", "content_id", "content_version", "observation_digest",
		"expiry_unix_seconds", "attestation"}
	if d.Fields().Len() != len(want) {
		t.Fatalf("EvidenceItem has %d fields", d.Fields().Len())
	}
	for i, name := range want {
		if f := d.Fields().ByNumber(protoreflect.FieldNumber(i + 1)); f == nil || f.Name() != name {
			t.Fatalf("field %d is %v, want %s", i+1, f, name)
		}
	}
}

// TestProviderReceiptNotOnChainD8 (DR-09-07 interim): PROVIDER_RECEIPT is
// FORBIDDEN after every spec check, alone or next to an attestation.
func TestProviderReceiptNotOnChainD8(t *testing.T) {
	e := newEnv(t)
	receipt := e.item(auditor1, "r1", "l1")
	receipt.Kind = evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", receipt)), msgkit.Forbidden, "receipt")
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "2", e.item(auditor1, "a1", "l2"), receipt)), msgkit.Forbidden, "receipt in a batch")
}

// TestEvidenceLifetimeBoundD9 (owner ruling on Q4): expiry - committed time
// may be 86400, not 86401.
func TestEvidenceLifetimeBoundD9(t *testing.T) {
	e := newEnv(t)
	at := e.item(auditor1, "e1", "l1")
	at.ExpiryUnixSeconds = now + evidence.MaxEvidenceLifetimeSeconds
	e.requireOK(e.submit(e.fresh(), signed(auditor1, "1", at)), "now+86400")
	over := e.item(auditor1, "e2", "l1")
	over.ExpiryUnixSeconds = now + evidence.MaxEvidenceLifetimeSeconds + 1
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "2", over)), msgkit.Bounds, "now+86401")
}

// TestLeaseContentBindingD10 (owner ruling on Q4): the item's content id
// and version must be the lease's.
func TestLeaseContentBindingD10(t *testing.T) {
	e := newEnv(t)
	wrongID := e.item(auditor1, "e1", "l1")
	wrongID.ContentId = "c2"
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", wrongID)), msgkit.Conflict, "content id")
	wrongVersion := e.item(auditor1, "e2", "l1")
	wrongVersion.ContentVersion = 2
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "2", wrongVersion)), msgkit.Conflict, "content version")
	e.mustSubmit(signed(auditor1, "3", e.item(auditor1, "e3", "l1")))
}

// TestLeaseNotHeldD11 (owner ruling on Q4): no new record for an ENDED or
// EXPIRED lease, or one without its reservation.
func TestLeaseNotHeldD11(t *testing.T) {
	for _, state := range []leasesv1.LeaseState{leasesv1.LeaseState_LEASE_STATE_ENDED, leasesv1.LeaseState_LEASE_STATE_EXPIRED} {
		e := newEnv(t)
		e.editLease("l1", func(l *leasesv1.Lease) { l.State, l.ReservationHeld = state, false })
		e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", e.item(auditor1, "e1", "l1"))), msgkit.Conflict, state.String())
	}
	e := newEnv(t)
	e.editLease("l1", func(l *leasesv1.Lease) { l.State = leasesv1.LeaseState_LEASE_STATE_RENEWED; l.TermVersion = 2 })
	e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1")))
}

// TestLeaseDeadlinePassedD12 (owner ruling on Q4): a held lease whose
// deadline is not after committed time (not yet swept) is EXPIRED; one
// second later than committed time is accepted (with an expiry at most the
// deadline).
func TestLeaseDeadlinePassedD12(t *testing.T) {
	e := newEnv(t)
	e.editLease("l1", func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = now })
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", e.item(auditor1, "e1", "l1"))), msgkit.Expired, "deadline == now")
	e.editLease("l1", func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = now + 1 })
	it := e.item(auditor1, "e2", "l1")
	it.ExpiryUnixSeconds = now + 1
	e.mustSubmit(signed(auditor1, "2", it))
}

// TestEvidenceExpiryWithinLeaseD18 (owner ruling on Q4): expiry == lease
// deadline is accepted; one second later is CONFLICT.
func TestEvidenceExpiryWithinLeaseD18(t *testing.T) {
	e := newEnv(t)
	e.editLease("l1", func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = now + 500 })
	over := e.item(auditor1, "e1", "l1")
	over.ExpiryUnixSeconds = now + 501
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", over)), msgkit.Conflict, "deadline+1")
	at := e.item(auditor1, "e2", "l1")
	at.ExpiryUnixSeconds = now + 500
	e.mustSubmit(signed(auditor1, "2", at))
	e.requireInvariants()
}

// TestContentWithdrawnD13 (owner ruling on Q4): no audit of withdrawn
// content; content the lease names that is absent (an invariant violation)
// is NOT_FOUND.
func TestContentWithdrawnD13(t *testing.T) {
	e := newEnv(t)
	e.withdraw("c1")
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "1", e.item(auditor1, "e1", "l1"))), msgkit.Withdrawn, "withdrawn")
	e.store(contentStore).Delete(contentKey("c2"))
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "2", e.item(auditor1, "e2", "l2"))), msgkit.NotFound, "content absent")
	e.mustSubmit(signed(auditor1, "3", e.item(auditor1, "e3", "l3")))
}

// TestAuditorEpochCapD15 (DR-09-06 interim): 1024 items per auditor per
// epoch through the handler (32 batches of 32); the next item is BOUNDS;
// another auditor is unaffected; the next epoch resets the window (an
// open-window cap, not a lifetime cap).
func TestAuditorEpochCapD15(t *testing.T) {
	e := newEnv(t)
	perBatch := 32
	for b := 0; b < int(evidence.MaxEvidencePerAuditorPerEpoch)/perBatch; b++ {
		tag := fmt.Sprintf("b%02d", b)
		e.mustSubmit(signed(auditor1, tag, e.items(auditor1, tag, "l1", perBatch)...))
	}
	if epoch, count := e.k.WindowForTest(e.unmetered(), addr(auditor1)); epoch != epochNow || count != evidence.MaxEvidencePerAuditorPerEpoch {
		t.Fatalf("window (%d, %d)", epoch, count)
	}
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "over", e.item(auditor1, "over", "l1"))), msgkit.Bounds, "cap+1")
	e.mustSubmit(signed(auditor2, "other", e.item(auditor2, "other", "l1")))
	next := now + evidence.EpochSeconds
	msg := batch(auditor1, "next", e.item(auditor1, "next", "l1"))
	msg.Epoch = evidence.EpochAt(next)
	msg.Items[0].ExpiryUnixSeconds = next + itemLife
	e.requireOK(e.submit(e.at(next), sign(msg, auditor1)), "next epoch")
	if epoch, count := e.k.WindowForTest(e.unmetered(), addr(auditor1)); epoch != evidence.EpochAt(next) || count != 1 {
		t.Fatalf("window after the boundary (%d, %d)", epoch, count)
	}
	e.ctx = e.at(next)
	e.requireInvariants()
}

// TestReportedStateD4 (owner ruling on Q4, read-time expiry): a RECORDED
// record reports RECORDED at expiry-1 and EXPIRED at expiry; REJECTED stays
// REJECTED; stored bytes never change with time; RenewalEvidence returns
// (expiry, false, true) for message-created records and (expiry, true, true)
// for a genesis-imported EXPIRED record, and not found for an absent or
// off-pattern id.
func TestReportedStateD4(t *testing.T) {
	e := newEnv(t)
	rejected := e.item(auditor1, "e2", "l2")
	rejected.ObservationDigest = digestOf("other")
	e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1"), rejected))
	expiry := now + itemLife
	before := e.snapshot(e.unmetered())
	cases := []struct {
		at       int64
		id       string
		reported evidencev1.EvidenceState
	}{
		{expiry - 1, "e1", evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED},
		{expiry, "e1", evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED},
		{expiry + 86400*365, "e1", evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED},
		{expiry - 1, "e2", evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED},
		{expiry, "e2", evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED},
	}
	for _, tc := range cases {
		id := prefix(auditor1) + tc.id
		resp, err := e.query(tc.at, &evidencev1.QueryEvidenceRequest{EvidenceId: id})
		if err != nil || len(resp.GetRecords()) != 1 || resp.GetRecords()[0].GetState() != tc.reported {
			t.Fatalf("%s at %d: %v %v, want %s", tc.id, tc.at, resp, err, tc.reported)
		}
		if got := evidence.ReportedState(e.stored(id), tc.at); got != tc.reported {
			t.Fatalf("ReportedState %s at %d: %s", tc.id, tc.at, got)
		}
		gotExpiry, expired, found := e.k.RenewalEvidence(e.at(tc.at), id)
		if gotExpiry != expiry || expired || !found {
			t.Fatalf("RenewalEvidence %s: (%d, %t, %t)", id, gotExpiry, expired, found)
		}
	}
	if !bytes.Equal(before, e.snapshot(e.unmetered())) {
		t.Fatal("a read changed the store")
	}
	if e.stored(prefix(auditor1)+"e1").GetState() != evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED {
		t.Fatal("stored state changed")
	}
	for _, id := range []string{prefix(auditor1) + "absent", "BAD ID", ""} {
		if _, _, found := e.k.RenewalEvidence(e.unmetered(), id); found {
			t.Fatalf("RenewalEvidence found %q", id)
		}
	}
	// A genesis-imported EXPIRED record.
	g := genesisEnv(t)
	state := validGenesis()
	state.Records[0].State = evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED
	if err := g.k.InitGenesis(g.unmetered(), state); err != nil {
		t.Fatal(err)
	}
	gotExpiry, expired, found := g.k.RenewalEvidence(g.unmetered(), state.Records[0].GetEvidenceId())
	if gotExpiry != state.Records[0].GetExpiryUnixSeconds() || !expired || !found {
		t.Fatalf("imported EXPIRED: (%d, %t, %t)", gotExpiry, expired, found)
	}
}

// TestNoEvidenceRevivesAnything (goal spec: a stale or passing audit must
// not revive expired or withdrawn hosting): x/evidence has no write path to
// another module. An accepted batch, a rejected one, and an idempotent
// replay leave the lease and content stores byte-identical; a batch naming
// an ENDED or EXPIRED lease, a lease past its deadline, or withdrawn content
// is rejected and writes nothing; and RenewalEvidence never reports a
// record that x/leases could use to renew (every outcome there is a
// rejection, chain/x/leases/msg_server.go).
func TestNoEvidenceRevivesAnything(t *testing.T) {
	e := newEnv(t)
	e.editLease("l2", func(l *leasesv1.Lease) { l.State, l.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_EXPIRED, false })
	e.withdraw("c3")
	others := evidencetestSnapshot(e)
	msg := signed(auditor1, "1", e.item(auditor1, "e1", "l1"))
	e.mustSubmit(msg)
	e.requireIdempotent(e.submit(e.fresh(), msg), "replay")
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "2", e.item(auditor1, "e2", "l2"))), msgkit.Conflict, "stale: expired lease")
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "3", e.item(auditor1, "e3", "l3"))), msgkit.Withdrawn, "stale: withdrawn content")
	late := batch(auditor1, "4", e.item(auditor1, "e4", "l1"))
	late.Epoch = evidence.EpochAt(leaseDeadline)
	late.Items[0].ExpiryUnixSeconds = leaseDeadline + 10
	e.requireReject(e.submit(e.at(leaseDeadline), sign(late, auditor1)), msgkit.Expired, "stale: lease past its deadline")
	if !bytes.Equal(others, evidencetestSnapshot(e)) {
		t.Fatal("x/evidence changed a lease or content byte")
	}
	if e.lease("l2").GetState() != leasesv1.LeaseState_LEASE_STATE_EXPIRED || e.lease("l1").GetDeadlineUnixSeconds() != leaseDeadline {
		t.Fatal("a lease changed")
	}
}

// TestRecordedEvidenceAfterEndWithdrawExpiry: a RECORDED record written
// while its lease was held describes one past observation. After a later
// EndLease, WithdrawContent, or lease expiry it still reports RECORDED until
// its own expiry (never past the lease deadline at recording), then
// EXPIRED. The query by lease reports exactly that; the label, not a state
// change, says it is a past observation.
func TestRecordedEvidenceAfterEndWithdrawExpiry(t *testing.T) {
	events := map[string]func(e *env){
		"end": func(e *env) {
			e.editLease("l1", func(l *leasesv1.Lease) { l.State, l.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_ENDED, false })
		},
		"withdraw": func(e *env) { e.withdraw("c1") },
		"expiry": func(e *env) {
			e.editLease("l1", func(l *leasesv1.Lease) { l.State, l.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_EXPIRED, false })
		},
	}
	for name, event := range events {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1")))
			event(e)
			for _, tc := range []struct {
				at   int64
				want evidencev1.EvidenceState
			}{
				{now + 1, evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED},
				{now + itemLife - 1, evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED},
				{now + itemLife, evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED},
			} {
				resp, err := e.query(tc.at, &evidencev1.QueryEvidenceRequest{LeaseId: "l1"})
				if err != nil || len(resp.GetRecords()) != 1 || resp.GetRecords()[0].GetState() != tc.want {
					t.Fatalf("at %d: %v %v, want %s", tc.at, resp, err, tc.want)
				}
			}
			e.requireReject(e.submit(e.fresh(), signed(auditor1, "2", e.item(auditor1, "e2", "l1"))),
				map[string]msgkit.Code{"end": msgkit.Conflict, "withdraw": msgkit.Withdrawn, "expiry": msgkit.Conflict}[name], "new audit after "+name)
		})
	}
}

// TestRecordSizeBound: a record and the rows one item adds have a constant
// maximal size. The test builds a maximal record (64-byte ids, maximal
// integers) and logs its encoded size and the logical bytes one item adds
// (record row, lease index row, and a 1/32 share of the batch marker plus a
// share of the window and business rows), which the evidence file
// multiplies by the yearly cap 1024 x 24 x 365 = 8,970,240 items per
// auditor. Logical KV bytes, not IAVL or disk bytes.
func TestRecordSizeBound(t *testing.T) {
	maxID := func(start string) string { return start + string(bytes.Repeat([]byte("z"), 64-len(start))) }
	record := &evidencev1.EvidenceRecord{
		EvidenceId: maxID(prefix(auditor1) + "e"), BatchId: maxID(prefix(auditor1) + "b"),
		Kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION, State: evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED,
		SignerPubkey: pub(auditor1), LeaseId: maxID("l"), ContentId: maxID("c"), ContentVersion: 1<<53 - 1,
		ObservationDigest: digestOf("x"), Epoch: evidence.EpochAt(253402300799), ExpiryUnixSeconds: 253402300799,
		SchemaVersion: 1, Attestation: true,
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	recordRow := len(evidence.RecordKey(record.GetEvidenceId())) + len(raw)
	indexRow := len(evidence.LeaseIndexKey(record.GetLeaseId(), record.GetEvidenceId())) + 1
	batchRow := len(evidence.BatchKey(record.GetBatchId())) + 1
	businessRow := 1 + 64 + 32
	windowRow := len(evidence.AuditorWindowKey(addr(auditor1))) + 16
	perItem := recordRow + indexRow
	perBatch := batchRow + businessRow
	if len(raw) > 400 || perItem > 600 {
		t.Fatalf("record %d bytes, per item %d bytes", len(raw), perItem)
	}
	t.Logf("maximal record value %d bytes; record row %d, lease index row %d (per item %d); per batch: batch marker %d, business row %d; window row %d (one per auditor, overwritten)",
		len(raw), recordRow, indexRow, perItem, batchRow, businessRow, windowRow)
	t.Logf("yearly bound per auditor at the cap (1024 items per epoch, 1-item batches, maximal ids): %d items x (%d + %d) bytes = %d logical KV bytes",
		uint64(8970240), perItem, perBatch, uint64(8970240)*uint64(perItem+perBatch))
}

// TestAuditLeavesOtherStoresUntouched (I9): batches by both auditors, in
// two epochs, RECORDED and REJECTED, and an idempotent replay leave the
// lease and content stores byte-identical: x/evidence has no write path to
// another module.
func TestAuditLeavesOtherStoresUntouched(t *testing.T) {
	e := newEnv(t)
	before := evidencetestSnapshot(e)
	rejected := e.item(auditor2, "r", "l3")
	rejected.ObservationDigest = digestOf("not the package")
	first := signed(auditor1, "1", e.item(auditor1, "a", "l1"), e.item(auditor1, "b", "l2"))
	e.mustSubmit(first)
	e.mustSubmit(signed(auditor2, "2", rejected))
	e.requireIdempotent(e.submit(e.fresh(), first), "replay")
	later := now + evidence.EpochSeconds
	next := batch(auditor1, "3", e.item(auditor1, "c", "l1"))
	next.Epoch = evidence.EpochAt(later)
	next.Items[0].ExpiryUnixSeconds = later + itemLife
	e.requireOK(e.submit(e.at(later), sign(next, auditor1)), "next epoch")
	if !bytes.Equal(before, evidencetestSnapshot(e)) {
		t.Fatal("an accepted batch changed a lease or content byte")
	}
}

// TestStoreInvariantViolationsFailClosed: a stored record that does not
// decode panics on every read path (the SDK turns a handler panic into a
// failed transaction); an auditor window that does not decode or is ahead
// of the current epoch panics in the handler; a window count above the cap
// is BOUNDS. None of these states is reachable through the handler.
func TestStoreInvariantViolationsFailClosed(t *testing.T) {
	mustPanic := func(what string, fn func()) {
		t.Helper()
		defer func() {
			if recover() == nil {
				t.Fatalf("%s did not panic", what)
			}
		}()
		fn()
	}
	e := newEnv(t)
	id := prefix(auditor1) + "broken"
	e.store(evidence.ModuleName).Set(evidence.RecordKey(id), []byte{0xff, 0xff})
	mustPanic("RenewalEvidence of an undecodable record", func() { e.k.RenewalEvidence(e.unmetered(), id) })
	mustPanic("StoredRecord of an undecodable record", func() { e.k.StoredRecord(e.unmetered(), id) })
	if _, err := e.query(now, &evidencev1.QueryEvidenceRequest{EvidenceId: id}); err == nil {
		t.Fatal("query served an undecodable record")
	}
	if _, err := e.k.ExportGenesis(e.unmetered()); err == nil {
		t.Fatal("export of an undecodable record")
	}

	e = newEnv(t)
	window := evidence.AuditorWindowKey(addr(auditor1))
	e.store(evidence.ModuleName).Set(window, make([]byte, 15))
	mustPanic("submit with an undecodable window", func() {
		_, _ = e.k.SubmitAuditBatch(e.fresh(), signed(auditor1, "1", e.item(auditor1, "e1", "l1")))
	})
	e.k.SetWindowForTest(e.unmetered(), addr(auditor1), epochNow+1, 1)
	mustPanic("submit with a window ahead of the epoch", func() {
		_, _ = e.k.SubmitAuditBatch(e.fresh(), signed(auditor1, "2", e.item(auditor1, "e2", "l1")))
	})
	e.k.SetWindowForTest(e.unmetered(), addr(auditor1), epochNow, evidence.MaxEvidencePerAuditorPerEpoch+5)
	e.requireReject(e.submit(e.fresh(), signed(auditor1, "3", e.item(auditor1, "e3", "l1"))), msgkit.Bounds, "window above the cap")
}
