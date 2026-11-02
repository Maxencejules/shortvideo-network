package authority

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// TestFuzzCorpusReach runs every FuzzStateTransitions seed and 2,000
// sequences from a fixed-seed PRNG through the same harness. Each named seed
// must produce its exact per-operation trace (traceToken), so a seed named
// for a refusal fails when that refusal disappears or changes code, and must
// reach its own minimum counts. The whole run must reach QUEUED,
// EXECUTED, REJECTED, EXPIRED and an ApplyDue that changes the set. The
// random sequences alone must reach the first four too, so the decoder, not
// only the hand-made seeds, gets there. The round-2 harness reached none of
// QUEUED, EXECUTED or a changed set: only seats 1 and 3 ever approved, so no
// proposal could queue.
func TestFuzzCorpusReach(t *testing.T) {
	var seeds, random fuzzCounters
	for _, seed := range fuzzStateSeeds() {
		got, trace := runFuzzTrace(t, seed.encode())
		if want := strings.Fields(seed.trace); !slices.Equal(trace, want) {
			t.Errorf("seed %q trace\n got  %s\n want %s", seed.name, strings.Join(trace, " "), strings.Join(want, " "))
		}
		if missing := seed.want.missing(got); missing != "" {
			t.Errorf("seed %q reached %+v; short of%s", seed.name, got, missing)
		}
		seeds.add(got)
	}
	rng := splitMix64(0x06a75eed00000006)
	for i := 0; i < 2000; i++ {
		raw := make([]byte, rng.next()%(2*fuzzMaxOps+1))
		for j := range raw {
			raw[j] = byte(rng.next() >> 56)
		}
		random.add(runFuzzSequence(t, raw))
	}
	total := seeds
	total.add(random)
	t.Logf("seeds:  %+v", seeds)
	t.Logf("random: %+v", random)
	t.Logf("total:  %+v", total)
	want := fuzzCounters{Queued: 1, Executed: 1, Rejected: 1, Expired: 1, ApplyDueChanged: 1}
	if missing := want.missing(total); missing != "" {
		t.Errorf("the corpus does not reach%s", missing)
	}
	if missing := (fuzzCounters{Queued: 1, Executed: 1, Rejected: 1, Expired: 1}).missing(random); missing != "" {
		t.Errorf("the random sequences do not reach%s", missing)
	}
}

// splitMix64 is a fixed-seed PRNG written out here, so the sequences do not
// depend on a library's stream across Go releases.
type splitMix64 uint64

func (s *splitMix64) next() uint64 {
	*s += 0x9e3779b97f4a7c15
	z := uint64(*s)
	z = (z ^ z>>30) * 0xbf58476d1ce4e5b9
	z = (z ^ z>>27) * 0x94d049bb133111eb
	return z ^ z>>31
}

// missing names each nonzero field of c (a minimum) that got does not reach.
func (c fuzzCounters) missing(got fuzzCounters) string {
	out := ""
	check := func(name string, min, have int) {
		if have < min {
			out += fmt.Sprintf(" %s %d<%d", name, have, min)
		}
	}
	check("queued", c.Queued, got.Queued)
	check("executed", c.Executed, got.Executed)
	check("rejected", c.Rejected, got.Rejected)
	check("expired", c.Expired, got.Expired)
	check("applyDueChanged", c.ApplyDueChanged, got.ApplyDueChanged)
	check("bounds", c.Bounds, got.Bounds)
	check("idempotent", c.Idempotent, got.Idempotent)
	return out
}

// fuzzStep is one operation of a hand-made seed (see the encoding on
// FuzzStateTransitions).
type fuzzStep struct{ op, seat, flags, v byte }

// fuzzSeed is a named hand-made sequence, its exact trace (one traceToken
// per step, space separated) and the counts it must reach.
type fuzzSeed struct {
	name  string
	steps []fuzzStep
	trace string
	want  fuzzCounters
}

func (s fuzzSeed) encode() []byte {
	out := make([]byte, 0, 2*len(s.steps))
	for _, step := range s.steps {
		if step.op > 7 || step.seat > 3 || step.flags > 7 {
			panic(fmt.Sprintf("seed %q: step %+v does not fit one byte", s.name, step))
		}
		out = append(out, step.op|step.seat<<3|step.flags<<5, step.v)
	}
	return out
}

// Seed step builders.

// validatorV: node bits 0-5 (v1..v6), key pool 0-3 (fuzzKey), power 0-3,
// far activation.
func validatorV(node, pool, power byte, far bool) byte {
	v := node | pool<<3 | power<<5
	if far {
		v |= 0x80
	}
	return v
}

// removalV: a removal of node bound to the key it holds now.
func removalV(node byte) byte { return node }

// removalUnboundV: a removal of node with key pool entry pool.
func removalUnboundV(node, pool byte) byte { return node | 8 | pool<<4 }

// limitV: limit index 0-7 (5 unknown, 6 admit, 7 withdraw), value 1-16, or
// one above the ceiling.
func limitV(limit, value byte, over bool) byte {
	v := limit | (value-1)<<3
	if over {
		v |= 0x80
	}
	return v
}

func proposeValidator(seat, flags, v byte) fuzzStep {
	return fuzzStep{opProposeValidator, seat, flags, v}
}

func proposeRemoval(seat, flags, v byte) fuzzStep { return fuzzStep{opProposeRemoval, seat, flags, v} }

func proposeLimit(seat, flags, v byte) fuzzStep { return fuzzStep{opProposeGoverned, seat, flags, v} }

// approveLast and executeLast name the last accepted proposal.
func approveLast(seat byte) fuzzStep { return fuzzStep{opApprove, seat, 0, 0} }

func executeLast(seat byte) fuzzStep { return fuzzStep{opExecute, seat, 0, 0} }

// approveAt and executeAt name accepted proposal number i.
func approveAt(seat, i byte) fuzzStep { return fuzzStep{opApprove, seat, 0, 0x80 | i} }

func executeAt(i byte) fuzzStep { return fuzzStep{opExecute, 0, 0, 0x80 | i} }

func heightStep(blocks byte) fuzzStep { return fuzzStep{opHeightStep, 0, 0, blocks - 1} }

// timeStep moves time by fuzzTimeSteps[index].
func timeStep(index byte) fuzzStep { return fuzzStep{opTimeStep, 0, 0, index} }

func wrongRole(seat, action, extra byte) fuzzStep {
	return fuzzStep{opWrongRole, seat, 0, action | extra<<3}
}

// quorum approves the last proposal with three seats starting at first.
func quorum(first byte) []fuzzStep {
	return []fuzzStep{approveLast(first % 4), approveLast((first + 1) % 4), approveLast((first + 2) % 4)}
}

func steps(parts ...any) []fuzzStep {
	var out []fuzzStep
	for _, part := range parts {
		switch body := part.(type) {
		case fuzzStep:
			out = append(out, body)
		case []fuzzStep:
			out = append(out, body...)
		default:
			panic(fmt.Sprintf("steps: %T", part))
		}
	}
	return out
}

// fuzzStateSeeds are the hand-made seeds. Each one reaches a named branch.
func fuzzStateSeeds() []fuzzSeed {
	addV5 := steps(proposeValidator(0, 0, validatorV(4, 0, 1, false)), quorum(0), executeLast(0), heightStep(2))
	var quota []fuzzStep
	for i := 0; i < MaxOpenProposalsPerSeat+1; i++ {
		quota = append(quota, proposeLimit(2, 0, limitV(1, byte(i%16)+1, false)))
	}
	quota = append(quota, proposeLimit(3, 0, limitV(1, 1, false)))
	return []fuzzSeed{
		{name: "empty"},
		{
			name:  "add v5 by height, then apply it",
			trace: "ok ok ok ok ok apply",
			steps: addV5,
			want:  fuzzCounters{Queued: 1, Executed: 1, ApplyDueChanged: 1},
		},
		{
			name:  "add v5, then remove it",
			trace: "ok ok ok ok ok apply ok ok ok ok ok apply",
			steps: steps(addV5,
				proposeRemoval(1, 0, removalV(4)), quorum(1), executeLast(1), heightStep(2)),
			want: fuzzCounters{Queued: 2, Executed: 2, ApplyDueChanged: 2},
		},
		{
			name:  "rotate the consensus key of v1 at a unix time",
			trace: "ok ok ok ok ok apply",
			steps: steps(proposeValidator(2, flagTime, validatorV(0, 2, 1, false)), approveLast(0), approveLast(1), approveLast(3),
				executeLast(0), timeStep(2)),
			want: fuzzCounters{Queued: 1, Executed: 1, ApplyDueChanged: 1},
		},
		{
			name:  "a removal that leaves three validators is REJECTED and never applies",
			trace: "ok ok ok ok keep:FORBIDDEN idle",
			steps: steps(proposeRemoval(0, 0, removalV(0)), quorum(0), executeLast(0), heightStep(2)),
			want:  fuzzCounters{Queued: 1, Rejected: 1},
		},
		{
			name:  "a power near safeMax/4 breaks the share cap, is REJECTED and never applies",
			trace: "ok ok ok ok keep:FORBIDDEN idle",
			steps: steps(proposeValidator(3, flagExtra, validatorV(1, 0, 0, false)), quorum(0), executeLast(0), heightStep(2)),
			want:  fuzzCounters{Queued: 1, Rejected: 1},
		},
		{
			name:  "approve and execute after expiry are kept as EXPIRED",
			trace: "ok idle keep:EXPIRED keep:EXPIRED",
			steps: steps(proposeLimit(0, 0, limitV(1, 5, false)), timeStep(5), approveLast(1), executeLast(0)),
			want:  fuzzCounters{Expired: 2},
		},
		{
			name:  "a late execute relabels an EXECUTED proposal EXPIRED",
			trace: "ok ok ok ok ok idle keep:EXPIRED",
			steps: steps(proposeLimit(1, 0, limitV(0, 3, false)), quorum(0), executeLast(0), timeStep(5),
				executeLast(1)),
			want: fuzzCounters{Queued: 1, Executed: 1, Expired: 1},
		},
		{
			name:  "a time activation conflicts with a pending height entry, then executes",
			trace: "ok ok ok ok ok ok ok ok ok CONFLICT apply ok apply",
			steps: steps(addV5[:5],
				proposeValidator(1, flagTime, validatorV(5, 0, 1, false)), quorum(0), executeLast(1),
				heightStep(2), executeLast(2), timeStep(2)),
			want: fuzzCounters{Queued: 2, Executed: 2, ApplyDueChanged: 2},
		},
		{
			name:  "council and service rotations",
			trace: "ok ok UNAUTHORIZED ok ok ok ok CONFLICT DUPLICATE_ID ok ok FORBIDDEN BOUNDS ok CONFLICT UNAUTHORIZED",
			steps: steps(wrongRole(0, 5, 0), proposeLimit(0, 0, limitV(1, 4, false)), wrongRole(1, 4, 0),
				quorum(0), executeLast(0),
				wrongRole(0, 5, 0), wrongRole(1, 5, 0), wrongRole(1, 5, 1),
				wrongRole(0, 6, 0), fuzzStep{opExecute, 0, executeByService, 0}, wrongRole(0, 6, 0),
				wrongRole(0, 6, 1), wrongRole(0, 6, 0), wrongRole(2, 7, 0)),
			want: fuzzCounters{Queued: 1, Executed: 1, Bounds: 1},
		},
		{
			name:  "duplicate id, replay and idempotent retries",
			trace: "ok idem DUPLICATE_ID REPLAY ok idem REPLAY NOT_FOUND ok ok ok idem CONFLICT",
			steps: steps(proposeLimit(0, 0, limitV(1, 5, false)),
				proposeLimit(0, flagExtra, limitV(1, 5, false)),
				proposeLimit(1, flagExtra, limitV(1, 5, false)),
				proposeLimit(0, flagExtra, limitV(1, 6, false)),
				approveLast(1), approveLast(1), fuzzStep{opApprove, 1, flagTime, 0},
				fuzzStep{opApprove, 2, approveMissing, 0},
				approveLast(2), approveLast(3), executeLast(0), executeLast(0), executeLast(1)),
			want: fuzzCounters{Queued: 1, Executed: 1, Idempotent: 3},
		},
		{
			name:  "one seat at its open quota gets BOUNDS, another seat proposes",
			steps: quota,
			trace: strings.Repeat("ok ", MaxOpenProposalsPerSeat) + "BOUNDS ok",
			want:  fuzzCounters{Bounds: 1},
		},
		{
			name:  "stale and conflicting executes after a policy bump",
			trace: "ok ok ok ok ok ok ok ok ok ok ok ok ok CONFLICT STALE_VERSION",
			steps: steps(proposeLimit(0, 0, limitV(1, 5, false)), proposeLimit(1, 0, limitV(1, 6, false)),
				proposeLimit(2, 0, limitV(2, 2, false)),
				approveAt(0, 0), approveAt(1, 0), approveAt(2, 0),
				approveAt(0, 1), approveAt(1, 1), approveAt(2, 1),
				approveAt(0, 2), approveAt(1, 2), approveAt(2, 2),
				executeAt(0), executeAt(1), executeAt(2)),
			want: fuzzCounters{Queued: 3, Executed: 1},
		},
		{
			name:  "wrong-role actions get their exact codes",
			trace: "ok ok ok ok FORBIDDEN UNAUTHORIZED UNAUTHORIZED UNAUTHORIZED UNAUTHORIZED BOUNDS UNAUTHORIZED UNAUTHORIZED ok",
			steps: steps(proposeLimit(0, 0, limitV(1, 5, false)), quorum(0),
				fuzzStep{opExecute, 0, executeByService, 0}, fuzzStep{opExecute, 1, executeBySeat, 0},
				wrongRole(0, 0, 0), wrongRole(1, 1, 0), wrongRole(2, 2, 0), wrongRole(3, 3, 0),
				wrongRole(0, 4, 0), wrongRole(1, 7, 0), executeLast(0)),
			want: fuzzCounters{Queued: 1, Executed: 1, Bounds: 1},
		},
		{
			name:  "payloads that cannot execute stay QUEUED",
			trace: "ok ok ok ok BOUNDS ok ok ok ok NOT_FOUND ok ok ok ok NOT_FOUND ok ok ok ok BOUNDS",
			steps: steps(proposeLimit(0, 0, limitV(1, 1, true)), quorum(0), executeLast(0),
				proposeLimit(1, 0, limitV(6, 1, false)), quorum(1), executeLast(1),
				proposeLimit(2, 0, limitV(7, 1, false)), quorum(2), executeLast(2),
				proposeLimit(3, 0, limitV(5, 1, false)), quorum(3), executeLast(3)),
			want: fuzzCounters{Queued: 4, Bounds: 2},
		},
		{
			name:  "passed activation, unbound removal and duplicate key are refused",
			trace: "ok ok ok ok idle OUT_OF_RANGE ok ok ok ok CONFLICT ok ok ok ok DUPLICATE_ID",
			steps: steps(proposeValidator(0, 0, validatorV(4, 0, 1, false)), quorum(0), heightStep(3), executeLast(0),
				proposeRemoval(1, 0, removalUnboundV(0, 2)), quorum(1), executeLast(1),
				proposeValidator(2, 0, validatorV(5, 1, 1, false)), quorum(2), executeLast(2)),
			want: fuzzCounters{Queued: 3},
		},
		{
			name:  "a time jump passes a far expiry",
			trace: "ok idle keep:EXPIRED",
			steps: steps(proposeValidator(0, flagFar, validatorV(4, 0, 1, false)), timeStep(7), approveLast(1)),
			want:  fuzzCounters{Expired: 1},
		},
	}
}
