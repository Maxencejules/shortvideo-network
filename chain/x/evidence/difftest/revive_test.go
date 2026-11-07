package difftest

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// leaseRow is a lease of a projection, or the zero value when absent.
func leaseRow(p projection, id string) leaseView {
	for _, l := range p.Leases {
		if l.LeaseID == id {
			return l
		}
	}
	return leaseView{}
}

// TestEvidenceUsesNoExpiryBudget is the deviation D-4 trace
// (noBudgetFixture): max_expiry_steps 1, a RECORDED record of the long
// lease sl2 expires at the same time T as the deadline of sl0, and a block
// at T. On the real keepers the block releases sl0 (x/evidence has no
// sweep and takes no part of the expiry budget); the record keeps its
// stored state RECORDED and reads EXPIRED from T on. With every adapter
// rule on, spec_machine.py agrees at every step. With D-4 off,
// spec_machine.py alone sweeps the record first (op_expire sorts evidence
// before a lease at an equal time, spec_machine.py:1545-1549), spends the
// budget on it, and keeps sl0 held: the block is the first divergence.
func TestEvidenceUsesNoExpiryBudget(t *testing.T) {
	o := getOracle(t)
	f, block, recorded := noBudgetFixture()
	on := mustAgree(t, f, o)
	out, proj := on.outs[block], on.projections[block]
	if got := on.specs[block].deviation(); got != "evidence D-4" {
		t.Fatalf("the block reports deviation %q, want evidence D-4", got)
	}
	sl0 := leaseRow(proj, "sl0")
	if out.released != 1 || sl0.ReservationHeld || sl0.State != "EXPIRED" {
		t.Fatalf("chain block: released %d, sl0 held %t state %s; want sl0 released", out.released, sl0.ReservationHeld, sl0.State)
	}
	if reported, stored := evidenceState(proj, recorded); reported != "EXPIRED" || stored != "RECORDED" {
		t.Fatalf("chain record %s: reported %s, stored %s; want EXPIRED read from a stored RECORDED", recorded, reported, stored)
	}

	rec := &recorder{TB: t}
	off := runFixture(t, rec, f, o, []string{"evidence D-4"}, nil)
	if off.firstMismatch != block || len(rec.errors) == 0 {
		t.Fatalf("with D-4 off: first divergence at step %d, want the block at step %d", off.firstMismatch, block)
	}
	spec := off.specs[block].Projection
	if _, stored := evidenceState(spec, recorded); stored != "EXPIRED" {
		t.Fatalf("with D-4 off: spec stores %s for %s, want EXPIRED (swept)", stored, recorded)
	}
	if !leaseRow(spec, "sl0").ReservationHeld || len(off.specs[block].Events) != 0 {
		t.Fatalf("with D-4 off: spec released sl0 (events %v); the record should have used the budget", off.specs[block].Events)
	}
	t.Logf("block at step %d: chain releases sl0 and stores %s as RECORDED (reads EXPIRED); spec_machine.py alone sweeps the record and keeps sl0 held",
		block, recorded)
}

// TestNothingRevives shows on the real keepers that no audit and no renewal
// naming evidence brings back hosting that ended (goal spec :35; plan
// sections 7.3 and 7.4). Five setup leases sl0-sl4 (sl0 and sl1 due
// together at T with max_expiry_steps 1; sl2, sl3, sl4 long) get one
// passing (RECORDED) and one failing (REJECTED) audit each. Then sl2 ends
// (EndLease), the content c3 of sl3 is withdrawn, and a block at T
// expires sl0 and leaves sl1 held past its deadline. After that:
//
//   - a passing and a failing audit of sl0 (expired) and sl2 (ended) are
//     CONFLICT (D-11), of sl1 (held past its deadline) EXPIRED (D-12), of
//     sl3 (content withdrawn) WITHDRAWN (D-13);
//   - every renewal that names one of the earlier records is rejected:
//     the leases that ended, expired, or lost their content keep their
//     state, and the live lease sl4 is not renewed by its own live
//     RECORDED or REJECTED record nor by another lease's expired record;
//   - the authority, providers, content, and leases stores are
//     byte-identical before and after this phase: only the two accepted
//     audits of the live lease sl4 write, into the evidence store.
//
// spec_machine.py, through the adapter rules, agrees at every step.
func TestNothingRevives(t *testing.T) {
	o := getOracle(t)
	due := startTime + 900
	f := newFixture(map[string]uint64{msgkit.LimitMaxExpirySteps: 1}, due, due, long, long, long)
	passed, failed := map[string]string{}, map[string]string{}
	codes := map[int]msgkit.Code{}
	for _, leaseID := range []string{"sl0", "sl1", "sl2", "sl3", "sl4"} {
		pass := f.item(auditorA, leaseID, 600)
		fail := f.item(auditorA, leaseID, 600)
		fail.digest = sha("observed other bytes:" + leaseID)
		codes[f.audit(auditorA, pass)] = msgkit.OK
		codes[f.audit(auditorA, fail)] = msgkit.OK
		passed[leaseID], failed[leaseID] = pass.evidenceID, fail.evidenceID
	}
	codes[f.end("sl2")] = msgkit.OK
	codes[f.withdraw("c3")] = msgkit.OK
	codes[f.block(due)] = msgkit.OK
	phase := len(f.steps)

	// Audits after the end, the withdrawal, and the expiry.
	for _, r := range []struct {
		lease string
		code  msgkit.Code
	}{{"sl0", msgkit.Conflict}, {"sl1", msgkit.Expired}, {"sl2", msgkit.Conflict}, {"sl3", msgkit.Withdrawn}} {
		pass := f.item(auditorA, r.lease, 60)
		fail := f.item(auditorA, r.lease, 60)
		fail.digest = sha("observed other bytes:" + r.lease)
		codes[f.audit(auditorA, pass)] = r.code
		codes[f.audit(auditorA, fail)] = r.code
	}
	// sl4 is still live: new audits of it are accepted.
	live := f.item(auditorA, "sl4", 600)
	liveRejected := f.item(auditorA, "sl4", 600)
	liveRejected.digest = sha("observed other bytes: sl4")
	codes[f.audit(auditorA, live)] = msgkit.OK
	codes[f.audit(auditorA, liveRejected)] = msgkit.OK

	// Renewals naming evidence: every one is rejected.
	type renewal struct {
		at   int
		what string
	}
	var renewals []renewal
	for _, r := range []struct{ lease, evidenceID string }{
		{"sl0", passed["sl0"]}, {"sl1", passed["sl1"]}, {"sl2", passed["sl2"]}, {"sl3", passed["sl3"]},
		{"sl1", failed["sl1"]}, {"sl3", failed["sl3"]},
		{"sl4", live.evidenceID}, {"sl4", liveRejected.evidenceID}, {"sl4", passed["sl0"]}, {"sl4", failed["sl4"]},
	} {
		renewals = append(renewals, renewal{f.renew(r.lease, r.evidenceID), r.lease + " naming " + r.evidenceID})
	}

	run := mustAgree(t, f, o)
	run.requireCodes(t, codes)
	for _, r := range renewals {
		if run.outs[r.at].code == msgkit.OK {
			t.Errorf("step %d: renewal of %s was accepted", r.at, r.what)
		}
		t.Logf("step %d: renewal of %s: %s", r.at, r.what, run.outs[r.at].codeName())
	}

	// Replay on fresh keepers to compare the other modules' stores around
	// the phase.
	h := newHarness(t, f.cfg)
	for _, s := range f.steps[:phase] {
		h.apply(s)
	}
	others := func() []byte { return h.snapshot(h.unmetered(), h.keys[:keyEvidence]...) }
	before, leasesBefore := others(), h.view().leases
	for _, s := range f.steps[phase:] {
		h.apply(s)
	}
	if !bytes.Equal(before, others()) {
		t.Fatalf("the audits and renewals after the end, withdrawal, and expiry changed the authority, providers, content, or leases store")
	}
	after := h.view()
	for id, lease := range leasesBefore {
		if !proto.Equal(lease, after.leases[id]) {
			t.Errorf("lease %s changed: %v -> %v", id, lease, after.leases[id])
		}
	}
	for id, want := range map[string]leasesv1.LeaseState{
		"sl0": leasesv1.LeaseState_LEASE_STATE_EXPIRED, "sl2": leasesv1.LeaseState_LEASE_STATE_ENDED,
	} {
		if l := after.leases[id]; l.GetState() != want || l.GetReservationHeld() {
			t.Errorf("%s: state %v held %t, want %v and released", id, l.GetState(), l.GetReservationHeld(), want)
		}
	}
	for _, id := range []string{"sl1", "sl3"} {
		if l := after.leases[id]; l.GetTermVersion() != 1 || l.GetDeadlineUnixSeconds() != f.leases[id].GetDeadlineUnixSeconds() {
			t.Errorf("%s: term %d deadline %d; a lease was extended", id, l.GetTermVersion(), l.GetDeadlineUnixSeconds())
		}
	}
	t.Logf("%d steps after the end, withdrawal, and expiry: audits rejected as D-11, D-12, D-13; %d renewals naming evidence rejected; other stores unchanged",
		len(f.steps)-phase, len(renewals))
}
