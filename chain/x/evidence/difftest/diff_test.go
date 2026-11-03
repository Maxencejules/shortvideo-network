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
// TestEvidenceDifferentialAgainstSpecMachine and
// TestEvidenceDifferentialCoverage so the run happens once per test
// process.
type seededRun struct {
	classes   map[string]int
	sequences int
	failed    bool
}

var (
	seededMu  sync.Mutex
	seededOut *seededRun
)

// runSeeded runs diffSeeds sequences of diffOps operations per family of
// diffFamilies against spec_machine.py, once per test process. Each family
// is a subtest of the test that runs it first.
func runSeeded(t *testing.T) *seededRun {
	t.Helper()
	seededMu.Lock()
	defer seededMu.Unlock()
	if seededOut != nil {
		return seededOut
	}
	oracle := getOracle(t)
	run := &seededRun{classes: map[string]int{}}
	for _, fam := range diffFamilies {
		name := familyNames[fam]
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

// TestEvidenceDifferentialAgainstSpecMachine runs 2 families (default,
// tight limits) x 64 seeds x 80 random operations through the real
// providers, content, leases, and evidence keepers and through
// spec_machine.py, and requires, at every step, the same code, idempotent
// flag, typed events, and projected state (providers, contents, open
// offers, leases, the pair index, business ids, chain time, every evidence
// record with its reported and stored state, batch ids). The adapter rules
// are the x/evidence deviations D-3, D-4, D-6 (mapping), D-8 to D-13, D-15,
// D-17, D-18 and the x/leases rules the lease steps reach (D-2, D-3,
// D-12); every step's rule is also derived on the Go side
// (expectedDeviation) and must equal the oracle's. Audit batches carry one
// item, or a count-class number of items (0, 33, the governed cap + 1).
// The generator does not send what the spec machine cannot express or
// what a known divergence covers (gen.retry, TestGeneratorRestrictions-
// HideKnownDivergences). Keeper level, not end to end.
func TestEvidenceDifferentialAgainstSpecMachine(t *testing.T) {
	run := runSeeded(t)
	if run.failed {
		t.Fatalf("the seeded differential run diverged")
	}
	t.Logf("%d sequences, %d compared steps, 0 divergences", run.sequences, stepCount(run.classes))
}

// TestEvidenceDifferentialCoverage requires the seeded run to reach every
// class of diffClasses and signatureClasses and logs the class table.
func TestEvidenceDifferentialCoverage(t *testing.T) {
	run := runSeeded(t)
	if run.failed {
		t.Fatalf("the seeded differential run diverged; coverage is not evidence")
	}
	checkCoverage(t, "differential", run.classes, append(signatureClasses(), diffClasses...))
}

// FuzzEvidenceDifferential compares fuzz-chosen sequences with
// spec_machine.py (one persistent oracle subprocess per fuzz worker). The
// fuzz bytes are choices of the generator; every number the oracle reads is
// written by Go's encoding/json in ASCII digits.
func FuzzEvidenceDifferential(f *testing.F) {
	for _, seed := range fuzzSeeds(f, false) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 {
			return
		}
		runFuzzInput(t, data, getOracle(t))
	})
}

// FuzzEvidenceTransitions runs fuzz-chosen sequences on the Go keepers only,
// with audit batches of up to 32 items that may succeed: after every step
// the evidence invariants (evidencetest.CheckInvariants) and the lease
// invariants (leasestest.CheckInvariants, CheckOfferIntegrity) hold, a
// rejected or idempotent message left its trial branch of the authority,
// providers, content, leases, and evidence stores byte-identical, and an
// accepted batch left the authority, providers, content, and leases stores
// byte-identical (I9). Every EvidenceAccepted is followed by its
// evidence_state event (D-16).
func FuzzEvidenceTransitions(f *testing.F) {
	for _, seed := range fuzzSeeds(f, true) {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		runFuzzInput(t, data, nil)
	})
}

// searchReach makes TestEvidenceFuzzReachesStates also search seeds
// 0..reachSeeds-1 of every family in both generator modes and log greedy
// covers for transitionCorpus and differentialCorpus.
var searchReach = flag.Bool("difftest.search-reach", false, "log greedy fuzz corpus covers")

// TestEvidenceFuzzReachesStates shows each fuzz seed corpus reaches the
// interesting states on its own, run exactly as its fuzz target runs an
// input (runFuzzInput, byteChooser, fuzzOps operations):
//
//   - transitions: fuzzSeeds(multi) on the Go keepers only must reach every
//     class of reachClasses and signatureClasses, including multi-item
//     successes, in-batch duplicates, and the per-epoch auditor cap (D-15);
//   - differential: fuzzSeeds(one-item) against spec_machine.py must reach
//     every class of diffClasses and signatureClasses with no divergence.
//
// Each recorded stream must also replay its seeded sequence exactly (the
// same class counts on the Go keepers), so the recording is faithful. A
// -fuzz run starts from these corpora; mutation can only add inputs.
func TestEvidenceFuzzReachesStates(t *testing.T) {
	modes := []struct {
		name     string
		multi    bool
		required []string
	}{
		{"transitions", true, append(signatureClasses(), reachClasses...)},
		{"differential", false, append(signatureClasses(), diffClasses...)},
	}
	if *searchReach {
		for _, mode := range modes {
			logReachCover(t, mode.name, mode.multi, mode.required)
		}
	}
	for _, mode := range modes {
		t.Run(mode.name, func(t *testing.T) {
			if len(corpusOf(mode.multi)) == 0 {
				t.Fatal("the recorded corpus is empty")
			}
			var oracle *oracleProc
			if !mode.multi {
				oracle = getOracle(t)
			}
			classes := map[string]int{}
			for _, seed := range fuzzSeeds(t, mode.multi) {
				merge(classes, runFuzzInput(t, seed, oracle))
			}
			if t.Failed() {
				t.Fatalf("a corpus input failed (details above)")
			}
			for _, r := range corpusOf(mode.multi) {
				rec := record(t, r)
				got, _ := runSequence(t, &byteChooser{data: rec.input[1:]}, seqOptions{
					family: int(rec.input[0]), ops: fuzzOps, multi: mode.multi, label: "replay",
				})
				if !maps.Equal(got, rec.classes) {
					t.Errorf("family %s seed %d: the recorded input replays %v, the seeded run reached %v",
						familyNames[r.family], r.seed, got, rec.classes)
				}
			}
			checkCoverage(t, fmt.Sprintf("%s fuzz seed corpus (%d inputs)", mode.name, randomSeeds+len(corpusOf(mode.multi))),
				classes, mode.required)
		})
	}
}

// logReachCover records seeds 0..reachSeeds-1 of every family in one
// generator mode and logs a greedy cover of required: repeatedly the
// sequence that reaches the most classes not yet covered.
func logReachCover(t *testing.T, name string, multi bool, required []string) {
	type candidate struct {
		seed    reachSeed
		classes map[string]int
	}
	var all []candidate
	for fam := range familyNames {
		for seed := 0; seed < reachSeeds; seed++ {
			r := reachSeed{family: fam, seed: seed, multi: multi}
			all = append(all, candidate{r, record(t, r).classes})
		}
	}
	missing := map[string]bool{}
	for _, class := range required {
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
			t.Fatalf("%s: no seed reaches %v", name, missing)
		}
		for class := range all[best].classes {
			delete(missing, class)
		}
		fmt.Fprintf(&cover, "\t{family: %s, seed: %d}, // +%d classes\n",
			familyConst[all[best].seed.family], all[best].seed.seed, gain)
	}
	t.Logf("%s corpus cover:\n%s", name, cover.String())
}

// familyConst is the Go name of each family constant, for logged covers.
var familyConst = []string{"famDefault", "famTight", "famFlood"}
