package evidence_test

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// FuzzSubmitAuditBatchDecode: arbitrary bytes decoded as the message, then
// the handler on a cached branch. It never panics; a non-OK result leaves
// the trial stores byte-identical and emits nothing; an OK result keeps
// every invariant.
func FuzzSubmitAuditBatchDecode(f *testing.F) {
	seed := newEnv(f)
	valid := signed(auditor1, "1", seed.item(auditor1, "e1", "l1"), seed.item(auditor1, "e2", "l2"))
	raw, err := proto.Marshal(valid)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(raw)
	f.Add(raw[:len(raw)/2])
	f.Add([]byte{})
	f.Add([]byte{0x4a, 0x02, 0x08, 0x07})
	f.Fuzz(func(t *testing.T, data []byte) {
		var msg evidencev1.MsgSubmitAuditBatch
		if proto.Unmarshal(data, &msg) != nil {
			return
		}
		e := newEnv(t)
		r := e.submit(e.fresh(), &msg)
		if r.code != msgkit.OK {
			if !r.trialSame || len(r.events) != 0 {
				t.Fatalf("rejected (%s) message changed the trial state or emitted events", r.code)
			}
			return
		}
		e.requireInvariants()
	})
}

// FuzzAuditStateSequence is a keeper-level state fuzz over the fakes: a
// bounded sequence of operations (valid batches by either auditor, batches
// with one fault, idempotent and changed replays, time steps across epochs
// and expiries, lease ends and expiries, content withdrawal) driven by the
// input bytes. After every operation the invariants hold; every rejection
// leaves every store byte-identical and emits nothing; the lease and
// content stores change only by the test's own fixture edits. The
// differential fuzz against the spec machine with real keepers is
// chain/x/evidence/difftest.
func FuzzAuditStateSequence(f *testing.F) {
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 0, 9, 2, 200, 0, 1})
	f.Add([]byte{0, 0, 0, 0, 5, 5, 2, 255, 2, 255, 0, 3})
	f.Add([]byte{1, 2, 3, 1, 4, 1, 0, 2, 6, 0, 0, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 96 {
			data = data[:96]
		}
		e := newEnv(t)
		clock := int64(now)
		var last *evidencev1.MsgSubmitAuditBatch
		next := func() byte {
			if len(data) == 0 {
				return 0
			}
			b := data[0]
			data = data[1:]
			return b
		}
		labels := []string{auditor1, auditor2}
		leaseIDs := []string{"l1", "l2", "l3"}
		for step := 0; len(data) > 0; step++ {
			op, arg := next()%8, next()
			ctx := e.at(clock)
			others := evidencetestSnapshot(e)
			switch op {
			case 0, 1: // a valid batch, then optionally one fault
				label := labels[int(arg)%2]
				n := int(arg/2)%4 + 1
				items := make([]*evidencev1.EvidenceItem, n)
				for i := range items {
					l := e.lease(leaseIDs[(int(arg)+i)%3])
					items[i] = &evidencev1.EvidenceItem{
						EvidenceId: prefix(label) + fmt.Sprintf("s%d-%d", step, i), Kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
						LeaseId: l.GetLeaseId(), ContentId: l.GetContentId(), ContentVersion: 1,
						ObservationDigest: manifestOf(l.GetContentId()), ExpiryUnixSeconds: clock + int64(arg)*60 + 1, Attestation: true,
					}
					if (int(arg)+i)%5 == 0 {
						items[i].ObservationDigest = digestOf(fmt.Sprintf("other-%d", step))
					}
				}
				msg := batch(label, fmt.Sprintf("s%d", step), items...)
				msg.Epoch = evidence.EpochAt(clock)
				if op == 1 {
					c := &call{ctx: ctx, msg: msg, signAs: label}
					sequenceFaults[int(next())%len(sequenceFaults)].apply(e, c)
					sign(c.msg, c.signAs)
					for _, edit := range c.sig {
						edit(c.msg.AuditorSignature)
					}
					msg = c.msg
				} else {
					sign(msg, label)
				}
				r := e.submit(ctx, msg)
				if r.code == msgkit.OK {
					last = msg
				} else if !r.trialSame || len(r.events) != 0 {
					t.Fatalf("step %d: rejected (%s) batch changed the trial state", step, r.code)
				}
			case 2: // replay the last accepted batch
				if last != nil {
					r := e.submit(ctx, last)
					if r.code == msgkit.OK && (!r.idempotent || !r.trialSame) {
						t.Fatalf("step %d: replay wrote", step)
					}
					if r.code != msgkit.OK && (!r.trialSame || len(r.events) != 0) {
						t.Fatalf("step %d: rejected replay wrote", step)
					}
				}
			case 3: // advance committed time (up to about 4 hours)
				clock += int64(arg) * 60
			case 4: // end or expire a lease
				state := leasesv1.LeaseState_LEASE_STATE_ENDED
				if arg%2 == 1 {
					state = leasesv1.LeaseState_LEASE_STATE_EXPIRED
				}
				e.editLease(leaseIDs[int(arg)%3], func(l *leasesv1.Lease) { l.State, l.ReservationHeld = state, false })
				others = evidencetestSnapshot(e)
			case 5: // withdraw content
				e.withdraw(fmt.Sprintf("c%d", int(arg)%3+1))
				others = evidencetestSnapshot(e)
			default: // query by lease at the current time
				resp, err := e.query(clock, &evidencev1.QueryEvidenceRequest{LeaseId: leaseIDs[int(arg)%3]})
				if err != nil {
					t.Fatalf("step %d: query %v", step, err)
				}
				for _, r := range resp.GetRecords() {
					if r.GetState() != evidence.ReportedState(e.stored(r.GetEvidenceId()), clock) {
						t.Fatalf("step %d: query state of %s", step, r.GetEvidenceId())
					}
				}
			}
			if string(others) != string(evidencetestSnapshot(e)) {
				t.Fatalf("step %d: a lease or content byte changed", step)
			}
			e.ctx = e.at(clock)
			e.requireInvariants()
		}
	})
}

// sequenceFaults are message and signature faults that apply to any batch
// of at least one item without touching a store, for the state fuzz.
var sequenceFaults = []fault{
	item0("kind-4", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.Kind = 4 }),
	item0("attestation-false", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.Attestation = false }),
	item0("digest-algorithm-0", msgkit.Malformed, func(it *evidencev1.EvidenceItem) { it.ObservationDigest.Algorithm = 0 }),
	item0("lease-not-found", msgkit.NotFound, func(it *evidencev1.EvidenceItem) { it.LeaseId = "l-none" }),
	item0("foreign-prefix", msgkit.Forbidden, func(it *evidencev1.EvidenceItem) { it.EvidenceId = "f" + it.EvidenceId }),
	item0("content-id-differs", msgkit.Conflict, func(it *evidencev1.EvidenceItem) { it.ContentId = "c9" }),
	item0("lifetime", msgkit.Bounds, func(it *evidencev1.EvidenceItem) { it.ExpiryUnixSeconds += 90000 }),
	item0("provider-receipt", msgkit.Forbidden, func(it *evidencev1.EvidenceItem) { it.Kind = 1 }),
	edit("epoch-previous", msgkit.StaleVersion, func(m *evidencev1.MsgSubmitAuditBatch) { m.Epoch-- }),
	edit("schema-2", msgkit.OutOfRange, func(m *evidencev1.MsgSubmitAuditBatch) { m.SchemaVersion = 2 }),
	sigEdit("signature-flip", msgkit.Unauthorized, func(sig *typesv1.InnerSignature) { sig.Signature[5] ^= 1 }),
	{name: "other-auditor-key", want: msgkit.Unauthorized, apply: func(_ *env, c *call) {
		other := auditor2
		if c.signAs == auditor2 {
			other = auditor1
		}
		c.msg.AuditorSignature.PublicKey, c.signAs = pub(other), other
	}},
}
