package difftest

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// TestCapacityBoundaryAgainstSpecMachine (REQ-INV-002) runs the capacity
// boundary through the real x/providers and x/leases keepers and through
// spec_machine.py, one byte on each side. p0 declares capacity C = 2001;
// contents c0 and c1 are 1000 bytes, c2 and c3 one byte.
//
//   - accepts of c0, c1 and c2 bring reserved bytes to exactly C: the third
//     acceptance commits;
//   - an acceptance of c3 (C+1) is CAPACITY, and so is a one-byte offer;
//     each leaves the authority, providers, content and leases stores
//     byte-identical;
//   - a renewal at full capacity commits and keeps the reserved bytes;
//   - an end releases 1000 bytes; the rejected offer and acceptance, resent
//     unchanged with the same business ids, then commit.
//
// After every step the reserved and declared bytes read through the real
// x/providers handoff (Provider) are checked, the harness checks the lease
// invariants and the trial branch, and the step's code, events and
// projected state must equal spec_machine.py's. Keeper level, not end to
// end. The seeded run cannot reach the one-byte-over acceptance: every
// package size the generator (gen_test.go) registers is a multiple of 100
// bytes, and so is every gap between a capacity and the reserved bytes it
// can produce (registered and updated capacities, an update below the
// reserved bytes is CAPACITY, and the near-max seeds).
func TestCapacityBoundaryAgainstSpecMachine(t *testing.T) {
	const capacity = 2001
	f := newSizedFixture(nil, capacity, 1000, 1000, 1, 1)
	deadline, expiry := f.now+termSeconds, f.now+600
	o0 := f.offer("o0", "c0", deadline, expiry)
	o1 := f.offer("o1", "c1", deadline, expiry)
	o2 := f.offerSized(serviceAcct, f.business(), "o2", "c2", 1, deadline, expiry)
	o3 := f.offerSized(serviceAcct, f.business(), "o3", "c3", 1, deadline, expiry)

	type check struct {
		what     string
		code     msgkit.Code
		reserved uint64
	}
	checks := map[int]check{}
	mark := func(what string, code msgkit.Code, reserved uint64) {
		checks[len(f.steps)-1] = check{what: what, code: code, reserved: reserved}
	}
	f.accept(o0, "l0")
	mark("accept l0", msgkit.OK, 1000)
	f.accept(o1, "l1")
	mark("accept l1", msgkit.OK, 2000)
	f.accept(o2, "l2")
	mark("accept to exactly the capacity", msgkit.OK, capacity)
	f.acceptBy(broadcasterAcct, "b-over", o3, "l3")
	over := f.steps[len(f.steps)-1]
	mark("accept one byte over the capacity", msgkit.Capacity, capacity)
	f.offerSized(serviceAcct, "b-offer-over", "o4", "c3", 1, deadline, expiry)
	overOffer := f.steps[len(f.steps)-1]
	mark("offer one byte over the capacity", msgkit.Capacity, capacity)
	f.renew("l1", "c1", 2, deadline+termSeconds)
	mark("renewal at full capacity", msgkit.OK, capacity)
	f.end("l0", 1)
	mark("end l0", msgkit.OK, capacity-1000)
	f.add(overOffer)
	mark("the rejected offer, resent", msgkit.OK, capacity-1000)
	f.add(over)
	mark("the rejected acceptance, resent", msgkit.OK, capacity-1000+1)

	o := getOracle(t)
	h := newHarness(t, f.cfg)
	o.reset(t, f.cfg, false)
	for i, s := range f.steps {
		before := h.snapshot(h.unmetered())
		out := h.apply(s)
		spec := o.step(t, s)
		label := fmt.Sprintf("step %d (%s)", i, s.op)
		if !compareStep(t, label, s, out, h.view().projection(), spec) {
			t.Fatalf("%s: the keepers and spec_machine.py diverge", label)
		}
		c, checked := checks[i]
		if !checked {
			if out.code != msgkit.OK {
				t.Fatalf("setup %s: %s", label, out.code)
			}
			continue
		}
		if out.code != c.code || out.idempotent {
			t.Fatalf("%s: code %s idempotent %t, want %s", c.what, out.code, out.idempotent, c.code)
		}
		if c.code != msgkit.OK && !bytes.Equal(before, h.snapshot(h.unmetered())) {
			t.Fatalf("%s: the rejected message changed the stores", c.what)
		}
		record, found := h.prov.Provider(h.unmetered(), "p0")
		if !found || record.GetReservedBytes() != c.reserved || record.GetAdmittedCapacityBytes() != capacity {
			t.Fatalf("%s: p0 found %t reserved %d of %d, want %d of %d", c.what, found,
				record.GetReservedBytes(), record.GetAdmittedCapacityBytes(), c.reserved, capacity)
		}
		t.Logf("%s: %s, reserved %d of %d (spec_machine.py %s)", c.what, out.codeName(), c.reserved, capacity, spec.Code)
	}
}
