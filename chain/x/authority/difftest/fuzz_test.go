package difftest

import (
	"testing"
)

// fuzzConfig is the chain configuration of a fuzz input: the review delay
// is 0 or 10 seconds.
func fuzzConfig(delayByte uint8) *config {
	cfg := defaultConfig()
	if delayByte%2 == 0 {
		cfg.delay = 0
	}
	return cfg
}

// FuzzCouncilDifferential compares a generated council sequence with
// spec_machine.py after every step: the seed picks the sequence, the step
// byte its length (20 to 99 steps), the delay byte the review delay. Any
// divergence fails.
func FuzzCouncilDifferential(f *testing.F) {
	for _, seed := range []uint64{1, 7, 42, 101, 9001} {
		f.Add(seed, uint8(60), uint8(1))
	}
	f.Add(uint64(3), uint8(99), uint8(0))
	f.Fuzz(func(t *testing.T, seed uint64, steps, delay uint8) {
		runSequence(t, seed, fuzzConfig(delay), runOptions{steps: 20 + int(steps)%80, compare: true})
	})
}

// FuzzGovernanceTransitions runs a generated council sequence on the real
// keepers without the oracle (keeper level, v0 payload kinds) and checks,
// after every step, the council invariants (I1 exact, I2, I7, I8, I9, I10,
// I11; checkInvariants) and that every rejected or idempotent message left
// the trial branch of every store byte-identical. TestTrialSnapshotDetectsEarlyWrite
// shows the bytes check fails on a handler that writes before it rejects.
func FuzzGovernanceTransitions(f *testing.F) {
	for _, seed := range []uint64{2, 11, 23, 77, 512} {
		f.Add(seed, uint8(120), uint8(1))
	}
	f.Add(uint64(5), uint8(200), uint8(0))
	f.Fuzz(func(t *testing.T, seed uint64, steps, delay uint8) {
		runSequence(t, seed, fuzzConfig(delay), runOptions{steps: 40 + int(steps)})
	})
}

// TestGovernanceTransitionsReachability runs FuzzGovernanceTransitions's
// body on fixed seeds and requires that it reaches the interesting states:
// every proposal transition, both stale-branch codes, the relabel, a
// REJECTED keep, and an applied validator change. A fuzz target whose
// inputs never leave the propose path would check nothing.
func TestGovernanceTransitionsReachability(t *testing.T) {
	total := newStats()
	for seed := uint64(201); seed < 213; seed++ {
		total.add(runSequence(t, seed, fuzzConfig(uint8(seed)), runOptions{steps: 240}))
	}
	t.Logf("reached:\n%s", total)
	for _, want := range []string{
		"OPEN -> QUEUED", "QUEUED -> EXECUTED", "OPEN -> EXPIRED", "QUEUED -> EXPIRED", "QUEUED -> REJECTED",
		"EXECUTED -> EXPIRED", "execute from QUEUED: ERROR_CODE_STALE_VERSION", "execute from QUEUED: ERROR_CODE_CONFLICT",
		"execute from QUEUED: ERROR_CODE_DUPLICATE_ID",
	} {
		if total.transitions[want] == 0 {
			t.Errorf("transition %q never reached", want)
		}
	}
	if total.applied == 0 {
		t.Errorf("no due validator change was applied")
	}
}
