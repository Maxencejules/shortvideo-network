package difftest

import (
	"fmt"
	"sort"
	"strings"
	"testing"
)

// stats counts what a sequence reached: codes per op, oracle rules, step
// tags, proposal transitions, and applied validator changes.
type stats struct {
	steps       int
	codes       map[string]int
	rules       map[string]int
	tags        map[string]int
	transitions map[string]int
	// combos counts each rule a propose carried with the propose's
	// generator tag.
	combos  map[string]int
	applied int
}

func newStats() *stats {
	return &stats{
		codes: map[string]int{}, rules: map[string]int{}, tags: map[string]int{},
		transitions: map[string]int{}, combos: map[string]int{},
	}
}

func (s *stats) add(o *stats) {
	s.steps += o.steps
	s.applied += o.applied
	for _, pair := range []struct{ to, from map[string]int }{
		{s.codes, o.codes}, {s.rules, o.rules}, {s.tags, o.tags}, {s.transitions, o.transitions}, {s.combos, o.combos},
	} {
		for k, v := range pair.from {
			pair.to[k] += v
		}
	}
}

func (s *stats) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "steps %d, applied validator changes %d\n", s.steps, s.applied)
	for _, part := range []struct {
		name string
		m    map[string]int
	}{{"codes", s.codes}, {"rules", s.rules}, {"tags", s.tags}, {"transitions", s.transitions}, {"rule combinations", s.combos}} {
		keys := make([]string, 0, len(part.m))
		for k := range part.m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		fmt.Fprintf(&b, "%s:", part.name)
		for _, k := range keys {
			fmt.Fprintf(&b, " [%s]=%d", k, part.m[k])
		}
		b.WriteString("\n")
	}
	return b.String()
}

// runOptions selects how a generated sequence runs.
type runOptions struct {
	steps int
	// compare runs the oracle and compares every step; without it only the
	// invariants and the trial-state checks run (FuzzGovernanceTransitions).
	compare bool
}

// runSequence runs one seeded sequence on fresh keepers and returns what it
// reached. With compare it stops at the first divergence (reported through
// tb.Errorf).
func runSequence(tb testing.TB, seed uint64, cfg *config, opts runOptions) *stats {
	tb.Helper()
	h := newHarness(tb, cfg)
	st := newStats()
	var o *oracleProc
	if opts.compare {
		o = getOracle(tb)
		initial := o.reset(tb, cfg, false)
		goInitial := h.projection()
		if diffs := projectionDiff(goInitial, initial); len(diffs) > 0 {
			tb.Fatalf("seed %d: initial projections differ:\n%s", seed, strings.Join(diffs, "\n"))
		}
	}
	g := newGen(seed, h)
	for i := 0; i < opts.steps; i++ {
		s := g.next()
		before := proposalStates(h.last.records)
		validatorsBefore := h.last.gov.GetValidators()
		out := h.apply(s)
		st.steps++
		st.codes[s.op+" "+out.codeName()]++
		if s.tag != "" {
			st.tags[s.op+" "+s.tag]++
		}
		if t := transition(before, proposalStates(h.last.records), s); t != "" {
			st.transitions[t]++
		}
		if id, _ := s.spec["proposal_id"].(string); s.op == opExecute {
			if pre, had := before[id]; had {
				st.transitions["execute from "+stateName(pre)+": "+out.codeName()]++
			}
		}
		if s.op == fxBlock && validatorsChanged(validatorsBefore, h.last.gov.GetValidators()) {
			st.applied++
		}
		if !opts.compare {
			continue
		}
		spec := o.step(tb, s, true)
		for _, rule := range strings.Split(spec.deviation(), "+") {
			if rule != "" {
				st.rules[rule]++
				if s.op == opPropose {
					st.combos[rule+" on "+s.tag]++
				}
			}
		}
		goProj := h.projection()
		if !compareStep(tb, fmt.Sprintf("seed %d step %d (%s %s)", seed, i, s.op, s.tag), s, out, &goProj, spec) {
			return st
		}
	}
	return st
}

// directed runs a fixed list of steps on fresh keepers against the oracle
// (rules on) and compares every step. It returns the oracle answers.
func directed(tb testing.TB, cfg *config, steps []*step) []specResult {
	tb.Helper()
	h := newHarness(tb, cfg)
	o := getOracle(tb)
	o.reset(tb, cfg, false)
	answers := make([]specResult, 0, len(steps))
	for i, s := range steps {
		out := h.apply(s)
		spec := o.step(tb, s, true)
		goProj := h.projection()
		if !compareStep(tb, fmt.Sprintf("step %d (%s)", i, s.op), s, out, &goProj, spec) {
			tb.FailNow()
		}
		answers = append(answers, spec)
	}
	return answers
}

// sequence builds directed council steps with fresh business ids.
type sequence struct {
	n     int
	now   int64
	steps []*step
}

func newSequence() *sequence { return &sequence{now: startTime} }

func (q *sequence) business(prefix string) string {
	q.n++
	return fmt.Sprintf("%s-%d", prefix, q.n)
}

func (q *sequence) propose(seat int, id string, p payload, base uint64) *step {
	s := buildPropose(proposeParams{
		signer: seatAcct(seat), business: q.business("bp"), proposalID: id, base: base,
		expiry: q.now + 10_000, payload: p,
	}, "")
	q.steps = append(q.steps, s)
	return s
}

func (q *sequence) add(s *step) *step {
	q.steps = append(q.steps, s)
	return s
}

func (q *sequence) approve(seat int, id string) *step {
	return q.add(buildApprove(seatAcct(seat), q.business("ba"), id))
}

func (q *sequence) execute(id string) *step {
	return q.add(buildExecute(authorityAcct, q.business("bx"), id))
}

func (q *sequence) block(dt int64) *step {
	q.now += dt
	return q.add(blockStep(q.now))
}

// pass proposes id on base, approves it with seats 1-3, waits delay (a
// block when delay > 0), and executes it.
func (q *sequence) pass(id string, p payload, base uint64, delay int64) {
	q.propose(1, id, p, base)
	for seat := 1; seat <= 3; seat++ {
		q.approve(seat, id)
	}
	if delay > 0 {
		q.block(delay)
	}
	q.execute(id)
}
