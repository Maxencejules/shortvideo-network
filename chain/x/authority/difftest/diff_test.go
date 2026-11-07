package difftest

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// councilSeeds and councilSteps size TestCouncilDifferential: seeded random
// sequences of proposes, approves, executes, blocks, and registrations,
// compared with spec_machine.py after every step (code, idempotent flag,
// keep flag, council events, and the whole projection).
const (
	councilSeeds = 12
	councilSteps = 160
)

func TestCouncilDifferential(t *testing.T) {
	total := newStats()
	for seed := uint64(1); seed <= councilSeeds; seed++ {
		total.add(runSequence(t, seed, defaultConfig(), runOptions{steps: councilSteps, compare: true}))
		if t.Failed() {
			t.Fatalf("seed %d diverged", seed)
		}
	}
	t.Logf("TestCouncilDifferential reached:\n%s", total)
}

// reachSeeds are the seeds of TestDifferentialReachability, disjoint from
// TestCouncilDifferential's.
const (
	reachFirstSeed = 101
	reachSeeds     = 12
	reachSteps     = 200
)

// TestDifferentialReachability runs its own seeded sequences against the
// oracle and requires that they reach every proposal state and transition
// the spec defines for the council flow: QUEUED, EXECUTED, EXPIRED from
// OPEN (approve) and from QUEUED (execute), REJECTED, the execute's stale
// branch with both codes, the EXECUTED -> EXPIRED relabel, an applied due
// validator change, and the chain-only rules a message can reach in random
// sequences (authority D-1, D-2, D-7, G-1, G-7, G-9, G-10, G-11).
func TestDifferentialReachability(t *testing.T) {
	total := newStats()
	for seed := uint64(reachFirstSeed); seed < reachFirstSeed+reachSeeds; seed++ {
		total.add(runSequence(t, seed, defaultConfig(), runOptions{steps: reachSteps, compare: true}))
		if t.Failed() {
			t.Fatalf("seed %d diverged", seed)
		}
	}
	t.Logf("reached:\n%s", total)
	for _, want := range []string{
		"OPEN -> QUEUED", "QUEUED -> EXECUTED", "OPEN -> EXPIRED", "QUEUED -> EXPIRED", "QUEUED -> REJECTED",
		"EXECUTED -> EXPIRED", "execute from QUEUED: ERROR_CODE_STALE_VERSION", "execute from QUEUED: ERROR_CODE_CONFLICT",
	} {
		if total.transitions[want] == 0 {
			t.Errorf("transition %q never reached", want)
		}
	}
	for _, rule := range []string{"authority D-1", "authority D-2", "authority D-7", "G-1", "G-7", "G-9", "G-10", "G-11"} {
		if total.rules[rule] == 0 {
			t.Errorf("rule %s never applied", rule)
		}
	}
	if total.applied == 0 {
		t.Errorf("no due validator change was applied")
	}
	// Authority D-1 maps whatever later code the spec returns: the
	// sequences must combine D-1 payloads with expired times and with
	// replayed or reused ids.
	for _, variant := range []string{"expired", "id"} {
		found := false
		for combo := range total.combos {
			found = found || (strings.HasPrefix(combo, "authority D-1 on ") && strings.Contains(combo, variant))
		}
		if !found {
			t.Errorf("no authority D-1 propose carried a %q variant", variant)
		}
	}
}

// TestStaleBranchDifferential is the execute's stale branch (spec
// op_execute :1509-1513): after one execute bumps the policy version, a
// QUEUED proposal on the old base is CONFLICT when its parameter key last
// executed another digest and STALE_VERSION otherwise (another key, or the
// same digest). A propose on the old base is STALE_VERSION.
func TestStaleBranchDifferential(t *testing.T) {
	q := newSequence()
	limit := msgkit.LimitMaxPageLimit
	q.propose(1, "a1", governedPayload(limit, 40), 1)
	q.propose(2, "a2", governedPayload(limit, 41), 1)
	q.propose(3, "a3", governedPayload(msgkit.LimitMaxOpenOffers, 5), 1)
	q.propose(4, "a4", governedPayload(limit, 40), 1)
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		for seat := 1; seat <= 3; seat++ {
			q.approve(seat, id)
		}
	}
	q.block(10)
	executes := len(q.steps)
	for _, id := range []string{"a1", "a2", "a3", "a4"} {
		q.execute(id)
	}
	q.propose(1, "a5", governedPayload(limit, 42), 1)
	answers := directed(t, defaultConfig(), q.steps)
	want := []string{"ok", string(msgkit.Conflict), string(msgkit.StaleVersion), string(msgkit.StaleVersion), string(msgkit.StaleVersion)}
	for i, code := range want {
		if got := answers[executes+i].Code; got != code {
			t.Errorf("step %d: %s, want %s", executes+i, got, code)
		}
	}
}

// TestNonAuthorityExecuteDifferential: only the AUTHORITY submitter
// executes. Another submitter role is FORBIDDEN and an account that is not
// a submitter (a seat account included, DR-06-06) is UNAUTHORIZED, before
// the council check and the proposal lookup; the proposal stays QUEUED and
// the AUTHORITY execute then succeeds.
func TestNonAuthorityExecuteDifferential(t *testing.T) {
	q := newSequence()
	q.propose(1, "e1", governedPayload(msgkit.LimitMaxPageLimit, 50), 1)
	for seat := 1; seat <= 3; seat++ {
		q.approve(seat, "e1")
	}
	q.block(10)
	signers := []struct {
		acct []byte
		code msgkit.Code
	}{
		{serviceAcct, msgkit.Forbidden}, {broadcasterAcct, msgkit.Forbidden}, {providerAccount(0), msgkit.Forbidden},
		{auditorAcct, msgkit.Forbidden}, {seatAcct(1), msgkit.Unauthorized}, {outsiderAcct, msgkit.Unauthorized},
	}
	first := len(q.steps)
	for i, s := range signers {
		q.add(buildExecute(s.acct, fmt.Sprintf("bx-other-%d", i), "e1"))
		q.add(buildExecute(s.acct, fmt.Sprintf("bx-missing-%d", i), "missing"))
	}
	q.execute("e1")
	answers := directed(t, defaultConfig(), q.steps)
	for i, s := range signers {
		for j := 0; j < 2; j++ {
			if got := answers[first+2*i+j].Code; got != string(s.code) {
				t.Errorf("signer %d step %d: %s, want %s", i, j, got, s.code)
			}
		}
	}
	if last := answers[len(answers)-1]; last.Code != "ok" {
		t.Errorf("the AUTHORITY execute after the refusals: %s", last.Code)
	}
}

// TestCouncilRulesAreLoadBearing runs one directed trace per council rule a
// message can reach on the chain (one per condition for G-11, the eight
// payload field shapes of ORCH-1). With every oracle rule on, every step
// agrees and the rule step names its rule. With that one rule off, the
// first divergence is exactly the rule step. So each rule is needed, and it
// fires only where the chain differs. For G-11 the chain answers BOUNDS,
// spec_machine.py alone MALFORMED, and with the rule off no projection
// field differs: both sides reject and neither changes state.
func TestCouncilRulesAreLoadBearing(t *testing.T) {
	fixtures := ruleFixtures()
	names := make([]string, 0, len(fixtures))
	for name := range fixtures {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			f := fixtures[name]()
			rule := name
			if f.rule != "" {
				rule = f.rule
			}
			on := runFixture(t, t, f.cfg, f.steps, false, f.every)
			if on.firstMismatch != -1 {
				t.Fatalf("with every rule on, step %d diverged", on.firstMismatch)
			}
			if got := on.specs[f.at].deviation(); !namesRule(got, rule) {
				t.Fatalf("rule step %d: deviation %q does not name %s", f.at, got, rule)
			}
			rec := &recorder{TB: t}
			off := runFixture(t, rec, f.cfg, f.steps, []string{rule}, f.every)
			if off.firstMismatch != f.at || len(rec.errors) == 0 {
				t.Fatalf("with %s off, the first divergence is step %d, want %d", rule, off.firstMismatch, f.at)
			}
			if f.chain != "" {
				if got, spec := on.outs[f.at].code, off.specs[f.at]; got != f.chain || spec.Code != string(f.specOff) || spec.Deviation != nil {
					t.Fatalf("chain %s, spec with %s off %s (deviation %q); want chain %s, spec %s, no rule", got, rule, spec.Code, spec.deviation(), f.chain, f.specOff)
				}
				for _, report := range rec.errors {
					if strings.Contains(report, "projection.") {
						t.Fatalf("with %s off the states differ, not only the codes: %s", rule, report)
					}
				}
			}
			t.Logf("%s: chain %s at step %d; without the rule spec_machine.py says %s", name,
				on.outs[f.at].codeName(), f.at, off.specs[f.at].Code)
		})
	}
}

// TestUnnamedParseDivergences pins the propose payloads on which the chain
// and spec_machine.py answer different reject codes and no G or D rule
// names the difference. The eight payload field shapes Wave 0 found here
// are now oracle rule G-11 (orchestrator decision ORCH-1) and are tested in
// TestCouncilRulesAreLoadBearing. The rows below are the same parse checks
// failing in combinations G-11's conditions do not include, found while
// G-11 was written down: the chain's first failing check is a BOUNDS check
// (the consensus key length, the node id pattern, the content id pattern)
// while the spec answers MALFORMED for an omitted field or an unparseable
// reason digest. The generator never sends them. Each is reported as an
// open divergence that needs a decision (a wider G-11, another rule, or a
// chain change); none changes state on either side, which the harness's
// trial check and the projection compare confirm here. The oracle applies
// no rule to them.
func TestUnnamedParseDivergences(t *testing.T) {
	short := &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sha("r")[:31]}
	cases := []struct {
		name        string
		payload     payload
		key         string
		chain, spec msgkit.Code
	}{
		{"validator consensus key of 31 bytes without a reason digest", validatorPayload("v9", consensusKey("v9")[:31], 10, nil, atHeight(9)), "", msgkit.Bounds, msgkit.Malformed},
		{"validator empty node id (proto3 default)", validatorPayload("", consensusKey("v9"), 10, digest(sha("r")), atHeight(9)), "validator-v9", msgkit.Bounds, msgkit.Malformed},
		{"withdraw content id outside the pattern without a reason digest", withdrawWith("-bad", 1, nil), "withdraw-x", msgkit.Bounds, msgkit.Malformed},
		{"withdraw content id outside the pattern with version 0 (proto3 default)", withdrawWith("-bad", 0, digest(sha("r"))), "withdraw-x", msgkit.Bounds, msgkit.Malformed},
		{"withdraw content id outside the pattern with a 31-byte reason digest", withdrawWith("-bad", 1, short), "withdraw-x", msgkit.Bounds, msgkit.Malformed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := buildPropose(proposeParams{
				signer: seatAcct(1), business: "bp-u", proposalID: "u1", parameterKey: c.key, base: 1,
				expiry: startTime + 100, payload: c.payload,
			}, "")
			rec := &recorder{TB: t}
			run := runFixture(t, rec, defaultConfig(), []*step{s}, false, 1)
			if run.firstMismatch != 0 {
				t.Fatalf("no divergence: chain %s, spec %s", run.outs[0].codeName(), run.specs[0].Code)
			}
			out, spec := run.outs[0], run.specs[0]
			if out.code != c.chain || spec.Code != string(c.spec) || spec.Deviation != nil {
				t.Fatalf("chain %s, spec %s (deviation %q); want chain %s, spec %s, no rule", out.code, spec.Code, spec.deviation(), c.chain, c.spec)
			}
			for _, report := range rec.errors {
				if strings.Contains(report, "projection.") {
					t.Fatalf("the states differ, not only the codes: %s", report)
				}
			}
			t.Logf("%s: chain %s, spec_machine.py %s", c.name, out.code, spec.Code)
		})
	}
}

// TestShapeRuleNearMisses sends the near misses of the G-11 conditions on
// which the chain and spec_machine.py agree (spec_oracle.py shape_controls,
// first two groups) and requires, with every rule on, that every step
// agrees and that no rule is named: six payloads both answer MALFORMED for
// (a reason digest without algorithm, 32 or 31 bytes, in a validator and in
// a withdraw payload; a withdraw version 0; a negative power with an
// activation height of 0, refused at the activation) and two both answer
// BOUNDS for. So G-11 fires only on its conditions.
func TestShapeRuleNearMisses(t *testing.T) {
	noAlgorithm := &typesv1.Digest{Value: sha("r")}
	shortNoAlgorithm := &typesv1.Digest{Value: sha("r")[:31]}
	cases := []struct {
		name    string
		payload payload
		key     string
		code    msgkit.Code
	}{
		{"validator reason without algorithm", validatorPayload("v9", consensusKey("v9"), 10, noAlgorithm, atHeight(9)), "", msgkit.Malformed},
		{"validator 31-byte reason without algorithm", validatorPayload("v9", consensusKey("v9"), 10, shortNoAlgorithm, atHeight(9)), "", msgkit.Malformed},
		{"withdraw reason without algorithm", withdrawWith("c1", 1, noAlgorithm), "", msgkit.Malformed},
		{"withdraw 31-byte reason without algorithm", withdrawWith("c1", 1, shortNoAlgorithm), "", msgkit.Malformed},
		{"withdraw version 0 (proto3 default)", withdrawWith("c1", 0, digest(sha("r"))), "", msgkit.Malformed},
		{"negative power with activation height 0", validatorPayload("v9", consensusKey("v9"), -5, digest(sha("r")), atHeight(0)), "", msgkit.Malformed},
		{"governed limit key outside the pattern", governedPayload("-bad", 5), msgkit.LimitMaxPageLimit, msgkit.Bounds},
		{"withdraw content id outside the pattern, well-formed otherwise", withdrawWith("-bad", 1, digest(sha("r"))), "withdraw-x", msgkit.Bounds},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := buildPropose(proposeParams{
				signer: seatAcct(1), business: "bp-n", proposalID: "n1", parameterKey: c.key, base: 1,
				expiry: startTime + 100, payload: c.payload,
			}, "")
			run := runFixture(t, t, defaultConfig(), []*step{s}, false, 1)
			if run.firstMismatch != -1 {
				t.Fatalf("diverged")
			}
			if got := run.specs[0]; got.Code != string(c.code) || got.Deviation != nil {
				t.Fatalf("spec %s (deviation %q), want %s with no rule", got.Code, got.deviation(), c.code)
			}
		})
	}
}

// TestExpiryAndDelayBoundariesDifferential walks every time boundary of the
// council flow one second at a time: an approve one second before the
// expiry succeeds and at the expiry is an EXPIRED keep; an execute one
// second before the execute-after time is OUT_OF_RANGE and at it succeeds;
// an execute at the expiry of a QUEUED proposal is an EXPIRED keep.
func TestExpiryAndDelayBoundariesDifferential(t *testing.T) {
	q := newSequence()
	expiry := func(id string, at int64) {
		q.add(buildPropose(proposeParams{
			signer: seatAcct(1), business: q.business("bp"), proposalID: id, base: 1, expiry: at,
			payload: governedPayload(msgkit.LimitMaxPageLimit, uint64(len(q.steps)+1)),
		}, ""))
	}
	expiry("b1", startTime+20)
	expiry("b2", startTime+20)
	expiry("b3", startTime+40)
	q.approve(1, "b2")
	q.approve(2, "b2")
	q.approve(3, "b2")
	q.block(19)
	q.approve(1, "b1")
	q.block(1)
	q.approve(2, "b1")
	q.execute("b2")
	for seat := 1; seat <= 3; seat++ {
		q.approve(seat, "b3")
	}
	q.block(9)
	q.execute("b3")
	q.block(1)
	q.execute("b3")
	answers := directed(t, defaultConfig(), q.steps)
	want := map[int]string{
		7:  "ok",                      // approve b1 at expiry-1
		9:  string(msgkit.Expired),    // approve b1 at expiry
		10: string(msgkit.Expired),    // execute queued b2 at expiry
		15: string(msgkit.OutOfRange), // execute b3 at execute-after-1
		17: "ok",                      // execute b3 at execute-after
	}
	for i, code := range want {
		if got := answers[i]; got.Code != code {
			t.Errorf("step %d (%s): %s, want %s", i, q.steps[i].op, got.Code, code)
		}
	}
	for _, i := range []int{9, 10} {
		if !answers[i].Keep {
			t.Errorf("step %d: an expiry is a keep outcome", i)
		}
	}
}
