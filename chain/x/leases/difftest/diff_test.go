package difftest

import (
	"flag"
	"fmt"
	"maps"
	"strings"
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
// adapter rules are the chain deviations of deviationRules (D-2 to D-6,
// D-10 to D-13; chain/x/leases README). The generator also avoids known
// divergences that are not lease rules, so the run does not compare them:
// D-1 and D-8 (lease deviations the adapter cannot express), provider
// capacities above 2^53-1 (DR-08-10), a provider registration retried by
// another account (DR-07-08), more key rotations than
// MaxRetiredKeysPerProvider (DR-07-10), and hosting intents with a role
// other than DEVNET_TEST_ONLY. TestGeneratorRestrictionsHideKnownDivergences
// shows the first two diverge. Keeper level, not end to end.
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
	for _, seed := range fuzzSeeds(f) {
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
	for _, seed := range fuzzSeeds(f) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		runFuzzInput(t, data, nil)
	})
}

// searchReach makes TestFuzzReachesStates also search seeds
// 0..reachSeeds-1 of every family and log a greedy cover for reachCorpus.
var searchReach = flag.Bool("difftest.search-reach", false, "log a greedy reachCorpus cover")

// TestFuzzReachesStates shows the fuzz seed corpus reaches the interesting
// states on its own: fuzzSeeds, run exactly as the fuzz targets run an
// input (runFuzzInput: byteChooser, fuzzOps operations, Go keepers only),
// must reach every required class. Each recorded stream must also replay
// its seeded sequence exactly (the same class counts), so the recording is
// faithful. A -fuzz run starts from this corpus; mutation can only add
// inputs.
func TestFuzzReachesStates(t *testing.T) {
	if *searchReach {
		logReachCover(t)
	}
	if len(reachCorpus) == 0 {
		t.Fatal("reachCorpus is empty")
	}
	classes := map[string]int{}
	for _, seed := range fuzzSeeds(t) {
		merge(classes, runFuzzInput(t, seed, nil))
	}
	for _, r := range reachCorpus {
		rec := record(t, r)
		if got := runFuzzInput(t, rec.input, nil); !maps.Equal(got, rec.classes) {
			t.Errorf("family %s seed %d: the recorded input replays %v, the seeded run reached %v",
				familyNames[r.family], r.seed, got, rec.classes)
		}
	}
	checkCoverage(t, fmt.Sprintf("fuzz seed corpus (%d inputs)", 8+len(reachCorpus)), classes)
}

// logReachCover records seeds 0..reachSeeds-1 of every family and logs a
// greedy cover of requiredClasses: repeatedly the sequence that reaches
// the most classes not yet covered.
func logReachCover(t *testing.T) {
	type candidate struct {
		seed    reachSeed
		classes map[string]int
	}
	var all []candidate
	for fam := range familyNames {
		for seed := 0; seed < reachSeeds; seed++ {
			r := reachSeed{family: fam, seed: seed}
			all = append(all, candidate{r, record(t, r).classes})
		}
	}
	missing := map[string]bool{}
	for _, class := range requiredClasses {
		missing[class] = true
	}
	var cover strings.Builder
	for len(missing) > 0 {
		best, gain := -1, 0
		for i, c := range all {
			n := 0
			for class := range missing {
				if c.classes[class] > 0 {
					n++
				}
			}
			if n > gain {
				best, gain = i, n
			}
		}
		if best < 0 {
			t.Fatalf("no seed reaches %v", missing)
		}
		for class := range all[best].classes {
			delete(missing, class)
		}
		fmt.Fprintf(&cover, "\t{family: %d, seed: %d}, // %s, +%d classes\n",
			all[best].seed.family, all[best].seed.seed, familyNames[all[best].seed.family], gain)
	}
	t.Logf("reachCorpus cover:\n%s", cover.String())
}
