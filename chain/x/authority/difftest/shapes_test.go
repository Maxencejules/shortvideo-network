package difftest

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The v0 payload shape space of TestUnnamedParseDivergences.
//
// A field shape class is a set of values of one payload field on which the
// chain's payload parse (chain/x/authority/payload.go), spec_machine.py
// parse_proposal_payload, and the oracle's G-9, G-10 and G-11 conditions
// (spec_oracle.py validator_parse_rule, shape_condition) all take the same
// branch. Each reads a field only through the tests below, so any value of
// a class gives the same answers as its representative:
//
//   - an id (consensus key, node id, content id, limit key, provider id):
//     the chain tests the id pattern or the 32-byte length (the content id
//     parse also tests the empty value apart); the spec tests the same
//     pattern or length on the JSON, where proto3 omits the empty value.
//     Classes: in the pattern (32 bytes), not empty and outside it (31
//     bytes), empty.
//   - a reason digest: the chain tests nil, the algorithm against SHA-256,
//     then the value length against 32; the spec's digest_message needs
//     exactly the algorithm SHA-256 and a canonical 32-byte value, and
//     proto3 JSON omits an UNSPECIFIED algorithm and an empty value and
//     writes an algorithm outside the enum as its number. Classes: SHA-256
//     with 32 bytes, omitted, algorithm omitted, SHA-256 with a value of
//     another length, SHA-256 with the value omitted, an algorithm outside
//     the enum.
//   - an activation: the chain tests nil, then the point (a height in
//     [1, 2^53-1] or a unix time in [unix_seconds_min, unix_seconds_max]),
//     MALFORMED for an absent point or a height outside, BOUNDS for a time
//     outside; the spec's parse_activation accepts exactly the same points,
//     and proto3 JSON always writes a set oneof member. Classes: a height
//     inside, a height outside (an absent point is in this class: both
//     parses and the oracle answer the same for it), a time inside, a time
//     outside, omitted.
//   - a validator power: the chain tests below 0; the spec reads a
//     canonical unsigned decimal, and proto3 JSON omits 0. Classes:
//     positive, 0, negative.
//   - a content version or a governed value: the chain tests below 1; the
//     spec reads a canonical unsigned decimal, and proto3 JSON omits 0.
//     Classes: at least 1, 0.
//
// parseShapes builds one payload for every combination of classes of each
// v0 payload alternative, and the payload without an alternative. Schema
// version 2 alternatives and fields are deviations D-4 and D-5
// (TestSchemaTwoPayloadsD5), not part of this space. Every v0 payload a
// client or the generator can send falls in exactly one combination, so if
// every combination agrees or carries a named rule, apart from the pinned
// open classes, no v0 payload has another unnamed parse divergence. That
// step rests on the class argument above, which is read from the code; the
// test checks one representative per combination, not every value.

// parseShape is one payload of the shape space: a label naming its
// classes, the payload, the parameter key its propose carries, and the
// open divergence class it falls in ("" for none).
type parseShape struct {
	label   string
	payload payload
	key     string
	open    string
}

// fieldClass is one class of a field and the label of its representative.
type fieldClass[T any] struct {
	label string
	value T
}

// The open divergence classes the oracle names no rule for (pinned by
// TestUnnamedParseDivergences). Each is chain BOUNDS where the spec says
// MALFORMED, at propose; neither side changes state.
const (
	openShortKeyNoNode       = "validator consensus key not 32 bytes, reason digest present, node id omitted"
	openShortKeyNoActivation = "validator consensus key not 32 bytes, reason digest and node id present, activation omitted"
	openWithdrawAlgorithm    = "withdraw content id outside the pattern, reason digest algorithm outside the enum"
)

// unknownAlgorithm is a Digest algorithm number outside the enum.
const unknownAlgorithm typesv1.DigestAlgorithm = 7

func reasonClasses() []fieldClass[*typesv1.Digest] {
	sha256 := typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256
	return []fieldClass[*typesv1.Digest]{
		{"reason SHA-256 32 bytes", digest(sha("r"))},
		{"reason omitted", nil},
		{"reason algorithm omitted", &typesv1.Digest{Value: sha("r")}},
		{"reason SHA-256 31 bytes", &typesv1.Digest{Algorithm: sha256, Value: sha("r")[:31]}},
		{"reason SHA-256 value omitted", &typesv1.Digest{Algorithm: sha256}},
		{"reason algorithm 7", &typesv1.Digest{Algorithm: unknownAlgorithm, Value: sha("r")}},
	}
}

func idClasses(valid string) []fieldClass[string] {
	return []fieldClass[string]{{"id " + valid, valid}, {"id -bad", "-bad"}, {"id omitted", ""}}
}

// activationClass is an activation point; omitted leaves the field unset.
type activationClass struct {
	at      activation
	omitted bool
}

// parseShapes is the shape space: one payload per class combination of
// every v0 alternative.
func parseShapes() []parseShape {
	var shapes []parseShape
	add := func(p payload, key, open string, labels ...string) {
		shapes = append(shapes, parseShape{label: strings.Join(labels, ", "), payload: p, key: key, open: open})
	}
	keys := []fieldClass[[]byte]{
		{"key 32 bytes", consensusKey("v9")}, {"key 31 bytes", consensusKey("v9")[:31]}, {"key omitted", nil},
	}
	activations := []fieldClass[activationClass]{
		{"height 9", activationClass{at: atHeight(9)}},
		{"height 0", activationClass{at: atHeight(0)}},
		{"unix in range", activationClass{at: atTime(startTime + 1000)}},
		{"unix 0", activationClass{at: atTime(0)}},
		{"activation omitted", activationClass{omitted: true}},
	}
	powers := []fieldClass[int64]{{"power 10", 10}, {"power 0", 0}, {"power -5", -5}}
	for _, key := range keys {
		for _, node := range idClasses("v9") {
			for _, reason := range reasonClasses() {
				for _, act := range activations {
					for _, power := range powers {
						p := validatorPayload(node.value, key.value, power.value, reason.value, act.value.at)
						if act.value.omitted {
							p.msg.GetValidatorPower().Activation = nil
							delete(p.json["validatorPower"].(map[string]any), "activation")
						}
						open := ""
						if key.label == "key 31 bytes" && reason.value != nil {
							switch {
							case node.value == "":
								open = openShortKeyNoNode
							case act.value.omitted:
								open = openShortKeyNoActivation
							}
						}
						add(p, "validator-v9", open, "validatorPower", key.label, node.label, reason.label, act.label, power.label)
					}
				}
			}
		}
	}
	for _, content := range idClasses("c1") {
		for _, version := range []fieldClass[uint64]{{"version 1", 1}, {"version 0", 0}} {
			for _, reason := range reasonClasses() {
				open := ""
				if content.value == "-bad" && reason.value.GetAlgorithm() == unknownAlgorithm {
					open = openWithdrawAlgorithm
				}
				add(withdrawWith(content.value, version.value, reason.value), "withdraw-c1", open,
					"forceWithdraw", content.label, version.label, reason.label)
			}
		}
	}
	for _, limit := range idClasses(msgkit.LimitMaxPageLimit) {
		for _, value := range []fieldClass[uint64]{{"value 5", 5}, {"value 0", 0}} {
			add(governedPayload(limit.value, value.value), msgkit.LimitMaxPageLimit, "", "governedLimit", limit.label, value.label)
		}
	}
	for _, provider := range idClasses("x1") {
		add(admitPayload(provider.value), "admit-x1", "", "admitProvider", provider.label)
		add(disablePayload(provider.value), "disable-x1", "", "disableProvider", provider.label)
	}
	add(payload{msg: &authorityv1.ProposalPayload{}, json: map[string]any{}}, msgkit.LimitMaxPageLimit, "", "no alternative")
	return shapes
}

// shapeSpaceResult counts what the shape space reached: the rule each step
// carried, and the steps of each open class.
type shapeSpaceResult struct {
	rules map[string]int
	open  map[string]int
}

// runShapeSpace proposes every shape on one harness against the oracle,
// with every rule on, on base version 2 (the current version is 1), so a
// payload both parses accept ends STALE_VERSION and every step is
// rejected. A step must agree with spec_machine.py (also by a named rule), or
// fall in its open class with chain BOUNDS, spec MALFORMED, no rule, and
// nothing else differing; a shape of an open class must diverge. After
// every step the harness checks that the rejected message left its trial
// branch byte-identical, and the projections are compared: neither side
// changed state.
func runShapeSpace(t *testing.T, shapes []parseShape) shapeSpaceResult {
	t.Helper()
	cfg := defaultConfig()
	cfg.invariants = false
	h := newHarness(t, cfg)
	o := getOracle(t)
	o.reset(t, cfg, false)
	res := shapeSpaceResult{rules: map[string]int{}, open: map[string]int{}}
	for i, sh := range shapes {
		s := buildPropose(proposeParams{
			signer: seatAcct(1), business: fmt.Sprintf("bs-%d", i), proposalID: fmt.Sprintf("s%d", i),
			parameterKey: sh.key, base: 2, expiry: startTime + 100, payload: sh.payload,
		}, "")
		out := h.apply(s)
		spec := o.step(t, s, true)
		if out.code == msgkit.OK {
			t.Fatalf("%s: the chain accepted the propose", sh.label)
		}
		if out.idempotent != spec.Idempotent || out.keep != spec.Keep || len(out.events) != 0 || len(spec.Events) != 0 {
			t.Errorf("%s: flags or events differ: go %+v, spec %+v", sh.label, out, spec)
		}
		for _, rule := range strings.Split(spec.deviation(), "+") {
			if rule != "" {
				res.rules[rule]++
			}
		}
		diverged := out.codeName() != spec.Code
		switch {
		case diverged && sh.open == "":
			t.Errorf("%s: divergence outside the open classes: chain %s, spec %s (deviation %q)",
				sh.label, out.codeName(), spec.Code, spec.deviation())
		case diverged && (out.code != msgkit.Bounds || spec.Code != string(msgkit.Malformed) || spec.Deviation != nil):
			t.Errorf("%s (%s): chain %s, spec %s (deviation %q); want chain BOUNDS, spec MALFORMED, no rule",
				sh.label, sh.open, out.codeName(), spec.Code, spec.deviation())
		case !diverged && sh.open != "":
			t.Errorf("%s: open class %q no longer diverges (chain %s, spec %s, deviation %q)",
				sh.label, sh.open, out.codeName(), spec.Code, spec.deviation())
		}
		if diverged {
			res.open[sh.open]++
		}
		if diffs := projectionDiff(h.projection(), *spec.Projection); len(diffs) > 0 {
			t.Fatalf("%s: the states differ:\n%s", sh.label, strings.Join(diffs, "\n"))
		}
	}
	if err := h.checkInvariants(h.unmetered()); err != nil {
		t.Fatalf("after the shape space: %v", err)
	}
	return res
}

func (r shapeSpaceResult) String() string {
	var b strings.Builder
	for _, part := range []struct {
		name string
		m    map[string]int
	}{{"rules", r.rules}, {"open classes", r.open}} {
		names := make([]string, 0, len(part.m))
		for name := range part.m {
			names = append(names, name)
		}
		sort.Strings(names)
		fmt.Fprintf(&b, "%s:", part.name)
		for _, name := range names {
			fmt.Fprintf(&b, " [%s]=%d", name, part.m[name])
		}
		b.WriteString("\n")
	}
	return b.String()
}
