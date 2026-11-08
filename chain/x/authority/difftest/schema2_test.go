package difftest

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// TestSchemaTwoPayloadsD5 is the one place the differential sends a schema
// version 2 payload (owner ruling Q1 "Minimal additive (Recommended)"): a
// governed limit with an activation height (D-4), a software upgrade, a
// cancel and a council seat replacement (D-5). spec_machine.py has no
// activationHeight field and no such alternative, so it answers MALFORMED
// at the payload parse; the oracle maps none of them (no rule), which is
// why the generator never sends them.
//
//   - Accepted on the chain (a well-formed propose): the chain stores an
//     OPEN proposal. The step differs from the spec in its code and in
//     exactly two projection fields, the proposal list (that one record)
//     and the business ids (its id); every other compared field (policy
//     version, governed limits, executed parameters, validators, pending
//     changes, registries, height, time) agrees, and no policy row changes
//     (I12, checked by apply).
//   - Refused on the chain by its own rule (a height below the window, a
//     value of 0, an id or key of the wrong shape): both sides reject with
//     different codes and no projection field differs; the trial check
//     shows the chain wrote nothing.
//   - Chain only, no oracle: the four kinds executed through the whole
//     council flow on the real keepers, with the harness invariants after
//     every step: the pending row of a scheduled limit (I3, state and exact
//     form), its activation at its height by the x/policy BeginBlock, the
//     plan and plan-name rows (I4), a cancel, and "scheduled change, then
//     immediate change of the same key" (the immediate change deletes the
//     pending row). The council seat execute is not run: the harness has no
//     account keeper (the app's adapter is chain/app's); x/authority's
//     keeper tests cover it.
func TestSchemaTwoPayloadsD5(t *testing.T) {
	release := digest(sha("release:up-1"))
	newKey := pubFor("seat-key-replacement")
	propose := func(p payload) *step {
		return buildPropose(proposeParams{
			signer: seatAcct(1), business: "bp-d5", proposalID: "d5", base: 1, expiry: startTime + 100, payload: p,
		}, "")
	}
	accepted := []struct {
		name    string
		payload payload
	}{
		{"scheduled governed limit", scheduledGovernedPayload(msgkit.LimitMaxPageLimit, 40, 500)},
		{"software upgrade", upgradePayload("up-1", 500, release)},
		{"cancel upgrade", cancelPayload("up-1")},
		{"council seat", seatPayload(seatID(4), newKey)},
	}
	for _, c := range accepted {
		t.Run("chain accepts/"+c.name, func(t *testing.T) {
			h := newHarness(t, defaultConfig())
			o := getOracle(t)
			o.reset(t, h.cfg, false)
			s := propose(c.payload)
			out := h.apply(s)
			spec := o.step(t, s, true)
			if out.code != msgkit.OK || out.idempotent || len(out.events) != 0 {
				t.Fatalf("chain: %s (idempotent %t, events %v), want a fresh success", out.codeName(), out.idempotent, out.events)
			}
			if spec.Code != string(msgkit.Malformed) || spec.Deviation != nil || spec.Keep || len(spec.Events) != 0 {
				t.Fatalf("spec: %s (deviation %q, keep %t, events %v), want MALFORMED with no rule", spec.Code, spec.deviation(), spec.Keep, spec.Events)
			}
			goProj := h.projection()
			fields := diffFields(projectionDiff(goProj, *spec.Projection))
			if want := []string{"BusinessIDs", "Council.Proposals"}; !reflect.DeepEqual(fields, want) {
				t.Fatalf("differing projection fields %v, want %v", fields, want)
			}
			if got := goProj.Council.Proposals; len(got) != 1 || got[0].ProposalID != "d5" || got[0].State != "OPEN" || got[0].ParameterKey != c.payload.key {
				t.Fatalf("chain proposals %+v, want one OPEN d5 on %s", got, c.payload.key)
			}
			if len(spec.Projection.Council.Proposals) != 0 {
				t.Fatalf("spec proposals %+v, want none", spec.Projection.Council.Proposals)
			}
			if want := append([]string{"bp-d5"}, spec.Projection.BusinessIDs...); !sameSet(goProj.BusinessIDs, want) {
				t.Fatalf("chain business ids %v, spec %v plus bp-d5", goProj.BusinessIDs, spec.Projection.BusinessIDs)
			}
		})
	}
	refused := []struct {
		name    string
		payload payload
		chain   msgkit.Code
	}{
		{"scheduled governed limit at the current height", scheduledGovernedPayload(msgkit.LimitMaxPageLimit, 40, uint64(startHeight)), msgkit.OutOfRange},
		{"scheduled governed limit of value 0", scheduledGovernedPayload(msgkit.LimitMaxPageLimit, 0, 500), msgkit.Bounds},
		{"software upgrade one block below the minimum lead (PROPOSED)", upgradePayload("up-1", uint64(startHeight)+msgkit.MinActivationLeadBlocks-1, release), msgkit.OutOfRange},
		{"cancel of a name outside the id pattern", cancelPayload("-bad"), msgkit.Bounds},
		{"council seat key of 31 bytes", seatPayload(seatID(4), newKey[:31]), msgkit.Bounds},
	}
	for _, c := range refused {
		t.Run("chain refuses/"+c.name, func(t *testing.T) {
			h := newHarness(t, defaultConfig())
			o := getOracle(t)
			o.reset(t, h.cfg, false)
			s := propose(c.payload)
			out := h.apply(s)
			spec := o.step(t, s, true)
			if out.code != c.chain || spec.Code != string(msgkit.Malformed) || spec.Deviation != nil {
				t.Fatalf("chain %s, spec %s (deviation %q); want chain %s, spec MALFORMED with no rule", out.codeName(), spec.Code, spec.deviation(), c.chain)
			}
			if diffs := projectionDiff(h.projection(), *spec.Projection); len(diffs) != 0 {
				t.Fatalf("both reject, yet the states differ:\n%s", strings.Join(diffs, "\n"))
			}
		})
	}
	t.Run("chain only", func(t *testing.T) {
		q := newSequence()
		q.pass("g1", scheduledGovernedPayload(msgkit.LimitMaxPageLimit, 40, 8), 1, 10)
		q.pass("u1", upgradePayload("up-1", 300, release), 2, 10)
		q.pass("c1", cancelPayload("up-1"), 3, 10)
		q.pass("g2", scheduledGovernedPayload(msgkit.LimitMaxOpenOffers, 7, 20), 4, 10)
		q.pass("g3", governedPayload(msgkit.LimitMaxOpenOffers, 9), 5, 10)
		h := newHarness(t, defaultConfig())
		for i, s := range q.steps {
			if out := h.apply(s); out.code != msgkit.OK {
				t.Fatalf("step %d (%s): %s", i, s.op, out.codeName())
			}
			if s.op == opExecute && s.spec["proposal_id"] == "u1" {
				plan, found := h.pol.Plan(h.unmetered())
				if !found || plan.Name != "up-1" || plan.Height != 300 || plan.ProposalID != "u1" {
					t.Fatalf("after u1: plan %+v (found %t)", plan, found)
				}
			}
		}
		ctx := h.unmetered()
		if _, found := h.pol.Plan(ctx); found {
			t.Fatalf("the cancel left a plan")
		}
		if v, found := h.pol.GovernedLimit(ctx, msgkit.LimitMaxOpenOffers); !found || v != 9 {
			t.Fatalf("max_open_offers %d (%t) after the immediate change, want 9", v, found)
		}
		if h.height != 6 || h.last.version != 6 {
			t.Fatalf("height %d, policy version %d; want 6 and 6", h.height, h.last.version)
		}
		if _, found := h.pol.GovernedLimit(ctx, msgkit.LimitMaxPageLimit); found {
			t.Fatalf("max_page_limit took effect before its activation height")
		}
		for height := int64(7); height <= 8; height++ {
			if out := h.apply(blockStep(h.now + 5)); out.code != msgkit.OK {
				t.Fatalf("block %d: %s", height, out.codeName())
			}
			v, found := h.pol.GovernedLimit(h.unmetered(), msgkit.LimitMaxPageLimit)
			if took := found && v == 40; took != (height == 8) {
				t.Fatalf("height %d: max_page_limit %d (%t); it takes effect at 8", height, v, found)
			}
		}
		if len(h.decisions) != 2 {
			t.Fatalf("decisions %v, want the two governed keys", h.decisions)
		}
	})
}

// diffFields is the sorted field names of projectionDiff lines.
func diffFields(diffs []string) []string {
	fields := make([]string, 0, len(diffs))
	for _, line := range diffs {
		name, _, _ := strings.Cut(strings.TrimPrefix(line, "projection."), ":")
		fields = append(fields, name)
	}
	sort.Strings(fields)
	return fields
}

// sameSet reports whether a and b hold the same strings, in any order.
func sameSet(a, b []string) bool {
	x, y := append([]string(nil), a...), append([]string(nil), b...)
	sort.Strings(x)
	sort.Strings(y)
	return reflect.DeepEqual(x, y)
}
