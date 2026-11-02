package difftest

import (
	"fmt"
	"sync"
	"testing"
)

// seededRun is the result of the seeded differential run, shared by
// TestDifferentialAgainstSpecMachine and TestDifferentialCoverage so the
// run happens once per test process.
type seededRun struct {
	classes   map[string]int
	sequences int
	failed    bool
}

var (
	seededMu  sync.Mutex
	seededOut *seededRun
)

// runSeeded runs diffSeeds sequences of diffOps operations per family
// against spec_machine.py, once per test process. Each family is a
// subtest of the test that runs it first.
func runSeeded(t *testing.T) *seededRun {
	t.Helper()
	seededMu.Lock()
	defer seededMu.Unlock()
	if seededOut != nil {
		return seededOut
	}
	oracle := getOracle(t)
	run := &seededRun{classes: map[string]int{}}
	for fam, name := range familyNames {
		ok := t.Run(name, func(t *testing.T) {
			for seed := 0; seed < diffSeeds; seed++ {
				classes, ok := runSequence(t, newRandChooser(uint64(fam), uint64(seed)), seqOptions{
					family: fam,
					ops:    diffOps,
					label:  fmt.Sprintf("family %s seed %d", name, seed),
					oracle: oracle,
				})
				run.sequences++
				merge(run.classes, classes)
				if !ok {
					t.Fatalf("family %s seed %d diverged from spec_machine.py (details above)", name, seed)
				}
			}
		})
		run.failed = run.failed || !ok
	}
	seededOut = run
	return run
}

// TestDifferentialAgainstSpecMachine runs 4 families x 64 seeds x 80 random
// operations through the real keepers and through spec_machine.py, and
// requires, at every step, the same code, idempotent flag, typed events,
// and projected state (providers with reserved bytes, contents, open
// offers, leases, the pair index, business ids, chain time). The only
// adapter rules are the chain deviations D-2 to D-6 (chain/x/leases
// README); the generator respects D-1 and D-8. Keeper level, not end to
// end.
func TestDifferentialAgainstSpecMachine(t *testing.T) {
	run := runSeeded(t)
	if run.failed {
		t.Fatalf("the seeded differential run diverged")
	}
	t.Logf("%d sequences, %d compared steps, 0 divergences", run.sequences, stepCount(run.classes))
}

// TestDifferentialCoverage requires the seeded run to reach every class of
// requiredClasses and logs the class table.
func TestDifferentialCoverage(t *testing.T) {
	run := runSeeded(t)
	if run.failed {
		t.Fatalf("the seeded differential run diverged; coverage is not evidence")
	}
	checkCoverage(t, "differential", run.classes)
}

// FuzzLeaseDifferential compares fuzz-chosen sequences with spec_machine.py
// (one persistent oracle subprocess per fuzz worker).
func FuzzLeaseDifferential(f *testing.F) {
	for _, seed := range fuzzSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		runFuzzInput(t, data, getOracle(t))
	})
}

// FuzzLeaseTransitions runs fuzz-chosen sequences on the Go keepers only:
// after every step the lease invariants I1-I10 hold
// (leasestest.CheckInvariants, CheckOfferIntegrity), a rejected or
// idempotent message left its trial branch of the authority, providers,
// content, and leases stores byte-identical, and no lease emitted
// LeaseEnded twice.
func FuzzLeaseTransitions(f *testing.F) {
	for _, seed := range fuzzSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		runFuzzInput(t, data, nil)
	})
}

// TestFuzzReachesStates shows the Go-only fuzz harness reaches the
// interesting states: its seed corpus plus a fixed seeded batch must reach
// every required class.
func TestFuzzReachesStates(t *testing.T) {
	classes := map[string]int{}
	for _, seed := range fuzzSeeds() {
		merge(classes, runFuzzInput(t, seed, nil))
	}
	t.Logf("fuzz seed corpus alone: %d classes", len(classes))
	for fam, name := range familyNames {
		for seed := 0; seed < reachSeeds; seed++ {
			found, ok := runSequence(t, newRandChooser(uint64(fam), uint64(seed)), seqOptions{
				family: fam, ops: diffOps, label: fmt.Sprintf("family %s seed %d (Go only)", name, seed),
			})
			merge(classes, found)
			if !ok {
				t.Fatalf("family %s seed %d failed", name, seed)
			}
		}
	}
	checkCoverage(t, "Go-only transitions", classes)
}
