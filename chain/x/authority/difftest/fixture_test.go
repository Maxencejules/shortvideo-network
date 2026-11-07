package difftest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// recorder collects Errorf reports instead of failing the test, so a test
// can require that a comparison or a trial check reports something.
type recorder struct {
	testing.TB
	errors []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recorder) Failed() bool { return len(r.errors) > 0 || r.TB.Failed() }

// fixtureRun is one fixture run: every Go outcome and oracle answer, and
// the index of the first step that diverged (-1: none).
type fixtureRun struct {
	outs          []outcome
	specs         []specResult
	firstMismatch int
}

// runFixture runs steps on fresh keepers against the oracle with disable
// (false, or a list of rule names) and compares every step, reporting
// through report. The projection is compared every projectionEvery steps
// and at the last step (1: every step). It stops at the first divergence.
func runFixture(tb testing.TB, report testing.TB, cfg *config, steps []*step, disable any, projectionEvery int) fixtureRun {
	tb.Helper()
	h := newHarness(tb, cfg)
	o := getOracle(tb)
	o.reset(tb, cfg, disable)
	run := fixtureRun{firstMismatch: -1}
	for i, s := range steps {
		withProjection := i == len(steps)-1 || i%projectionEvery == 0
		out := h.apply(s)
		spec := o.step(tb, s, withProjection)
		var goProj *projection
		if withProjection {
			p := h.projection()
			goProj = &p
		}
		run.outs = append(run.outs, out)
		run.specs = append(run.specs, spec)
		if !compareStep(report, fmt.Sprintf("step %d (%s)", i, s.op), s, out, goProj, spec) {
			run.firstMismatch = i
			return run
		}
	}
	if !cfg.invariants {
		if err := h.checkInvariants(h.unmetered()); err != nil {
			tb.Fatalf("after the last step: %v", err)
		}
	}
	return run
}

// names reports whether a deviation string names rule.
func names(deviation, rule string) bool {
	return strings.Contains("+"+deviation+"+", "+"+rule+"+")
}

// ruleFixture is a directed trace whose step at carries rule.
type ruleFixture struct {
	cfg   *config
	steps []*step
	at    int
	// every is the projection interval of the run.
	every int
}

func delayConfig(delay int64) *config {
	cfg := defaultConfig()
	cfg.delay = delay
	return cfg
}

// bulkConfig is delayConfig for the long G-3 and G-4 traces: the
// invariants run once after the last step instead of after every step
// (each check reads every proposal record).
func bulkConfig(delay int64) *config {
	cfg := delayConfig(delay)
	cfg.invariants = false
	return cfg
}

// ruleFixtures are the directed traces of every council rule a message can
// reach on the chain. G-2 (the Go message always carries both integers)
// and G-8 (every scheduled change was projected) cannot be reached by a
// message; the oracle self-test covers them.
func ruleFixtures() map[string]func() ruleFixture {
	return map[string]func() ruleFixture{
		"G-1": func() ruleFixture {
			q := newSequence()
			q.add(buildPropose(proposeParams{
				signer: seatAcct(1), business: "bp-g1", proposalID: "g1", base: 1, expiry: q.now + 100,
				payload: governedPayload(msgkit.LimitMaxPageLimit, 50), wrongAlgorithm: true,
			}, ""))
			return ruleFixture{cfg: defaultConfig(), steps: q.steps, at: 0, every: 1}
		},
		"G-3": func() ruleFixture {
			q := newSequence()
			for i := 0; i < 33; i++ {
				q.propose(1, fmt.Sprintf("q%d", i), governedPayload(msgkit.LimitMaxPageLimit, uint64(1+i)), 1)
			}
			return ruleFixture{cfg: bulkConfig(10), steps: q.steps, at: 32, every: 8}
		},
		"G-4": func() ruleFixture {
			q := newSequence()
			for i := 0; i < 129; i++ {
				node := fmt.Sprintf("a%d", i)
				q.pass(node, validatorPayload(node, consensusKey(node), validatorPower, digest(sha("r")), atHeight(uint64(1000+i))), uint64(1+i), 0)
			}
			return ruleFixture{cfg: bulkConfig(0), steps: q.steps, at: len(q.steps) - 1, every: 64}
		},
		"G-5": func() ruleFixture {
			q := newSequence()
			q.block(unixMax - 5 - startTime)
			q.add(buildPropose(proposeParams{
				signer: seatAcct(1), business: "bp-o1", proposalID: "o1", base: 1, expiry: unixMax,
				payload: governedPayload(msgkit.LimitMaxPageLimit, 50),
			}, ""))
			for seat := 1; seat <= 3; seat++ {
				q.approve(seat, "o1")
			}
			return ruleFixture{cfg: defaultConfig(), steps: q.steps, at: len(q.steps) - 1, every: 1}
		},
		"G-6": func() ruleFixture {
			cfg := defaultConfig()
			cfg.threshold = 2
			q := newSequence()
			q.propose(1, "s1", governedPayload(msgkit.LimitMaxPageLimit, 50), 1)
			return ruleFixture{cfg: cfg, steps: q.steps, at: 0, every: 1}
		},
		"G-9": func() ruleFixture {
			q := newSequence()
			q.propose(1, "u1", validatorPayload("v9", consensusKey("v9"), 10, digest(sha("r")), atTime(0)), 1)
			return ruleFixture{cfg: defaultConfig(), steps: q.steps, at: 0, every: 1}
		},
		"G-10": func() ruleFixture {
			q := newSequence()
			q.propose(1, "u2", validatorPayload("-bad", consensusKey("v9"), 10, nil, atHeight(9)), 1)
			return ruleFixture{cfg: defaultConfig(), steps: q.steps, at: 0, every: 1}
		},
		"authority D-1": func() ruleFixture {
			q := newSequence()
			q.propose(1, "d1", governedPayload(msgkit.LimitMaxPageLimit, 101), 1)
			return ruleFixture{cfg: defaultConfig(), steps: q.steps, at: 0, every: 1}
		},
		"authority D-2": func() ruleFixture {
			q := newSequence()
			q.pass("e1", governedPayload(msgkit.LimitMaxPageLimit, 50), 1, 10)
			return ruleFixture{cfg: defaultConfig(), steps: q.steps, at: len(q.steps) - 1, every: 1}
		},
		"authority D-7": func() ruleFixture {
			q := newSequence()
			q.pass("k1", validatorPayload("v9", seatPub(2), 10, digest(sha("r")), atHeight(50)), 1, 10)
			return ruleFixture{cfg: defaultConfig(), steps: q.steps, at: len(q.steps) - 1, every: 1}
		},
	}
}
