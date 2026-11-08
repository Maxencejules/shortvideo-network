package scenario_test

import (
	"go/build"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal/scenario"
)

func index(i scenario.Instant) int {
	for n, at := range scenario.Instants() {
		if at == i {
			return n
		}
	}
	return -1
}

// TestStepsAreConsistent checks the scenario's own rules, which both levels
// rely on:
//
//   - every step has a unique label, and every council step 2 or 3
//     approvals (3 when executed)
//   - an offer names registered content; an accept, renewal, end, and audit
//     name an offer or lease of an earlier step
//   - no time of a step falls in the window [AtPlan, AtResume] except
//     AtPlan itself as a block time or an offer expiry of an offer accepted
//     in the same phase, and every crossing time is after AtResume
//   - the plan is the last executed council step, so the QUEUED and OPEN
//     proposals stand on the policy version the upgrade bumps
func TestStepsAreConsistent(t *testing.T) {
	labels := map[string]bool{}
	contents := map[string]bool{}
	offers := map[string]bool{}
	leases := map[string]bool{}
	planSeen := false
	plan, resume := index(scenario.AtPlan), index(scenario.AtResume)
	outsideWindow := func(label string, i scenario.Instant) {
		if n := index(i); n < 0 || (n > plan && n <= resume) {
			t.Errorf("%s: instant %d lies in the halt window", label, i)
		}
	}
	for _, step := range scenario.Steps() {
		if labels[step.Label()] || step.Label() == "" {
			t.Errorf("label %q is empty or repeated", step.Label())
		}
		labels[step.Label()] = true
		switch s := step.(type) {
		case scenario.RegisterContent:
			contents[s.ContentID] = true
		case scenario.Offer:
			if !contents[s.ContentID] {
				t.Errorf("%s: content %s is not registered", s.Name, s.ContentID)
			}
			outsideWindow(s.Name, s.Deadline)
			outsideWindow(s.Name, s.Expiry)
			offers[s.OfferID] = true
		case scenario.Accept:
			if !offers[s.OfferID] {
				t.Errorf("%s: offer %s is not earlier", s.Name, s.OfferID)
			}
			leases[s.LeaseID] = true
		case scenario.Renew:
			if !leases[s.LeaseID] {
				t.Errorf("%s: lease %s is not earlier", s.Name, s.LeaseID)
			}
		case scenario.End:
			if !leases[s.LeaseID] {
				t.Errorf("%s: lease %s is not earlier", s.Name, s.LeaseID)
			}
		case scenario.Audit:
			if !leases[s.LeaseID] {
				t.Errorf("%s: lease %s is not earlier", s.Name, s.LeaseID)
			}
			outsideWindow(s.Name, s.Expiry)
		case scenario.Council:
			if s.Approvals < 2 || s.Approvals > 3 || (s.Execute && s.Approvals != 3) {
				t.Errorf("%s: %d approvals, execute %t", s.Name, s.Approvals, s.Execute)
			}
			if s.Execute && planSeen {
				t.Errorf("%s executes after the plan", s.Name)
			}
			if _, ok := s.Change.(scenario.SoftwareUpgrade); ok {
				planSeen = true
			}
			if s.Expiry != 0 {
				outsideWindow(s.Name, s.Expiry)
			}
		}
	}
	if !planSeen {
		t.Fatal("no software upgrade step")
	}
	for _, tr := range scenario.Transitions() {
		switch {
		case tr.Effective != 0 && index(tr.Effective) <= resume:
			t.Errorf("transition %s at instant %d is not after AtResume", tr.Object, tr.Effective)
		case tr.Effective == 0 && tr.At <= 2:
			t.Errorf("transition %s at h+%d is not after h+2", tr.Object, tr.At)
		}
	}
}

// TestWantCountsEveryClass: before and after the upgrade every class is
// above 0 and the two censuses are equal (the upgrade moves no object);
// after the transitions each transition moved exactly its object, and the
// governed activation added one governed value.
func TestWantCountsEveryClass(t *testing.T) {
	before, after := scenario.Want(scenario.BeforeUpgrade), scenario.Want(scenario.AfterUpgrade)
	for _, class := range scenario.Classes() {
		if before[class] < 1 || before[class] != after[class] {
			t.Errorf("%s: before %d, after %d", class, before[class], after[class])
		}
	}
	if err := before.Check(before, scenario.BeforeUpgrade); err != nil {
		t.Fatal(err)
	}
	final := scenario.Want(scenario.AfterTransitions)
	for _, tr := range scenario.Transitions() {
		if final[tr.Class] != before[tr.Class]-1 {
			t.Errorf("transition %s: class %s %d -> %d", tr.Object, tr.Class, before[tr.Class], final[tr.Class])
		}
	}
	if final[scenario.GovernedValues] != before[scenario.GovernedValues]+1 {
		t.Errorf("governed values %d -> %d", before[scenario.GovernedValues], final[scenario.GovernedValues])
	}
}

// TestCensusCheckNamesEveryProblem: Check reports a class whose count
// differs and, before the transitions, a class the scenario does not fill;
// String prints every class.
func TestCensusCheckNamesEveryProblem(t *testing.T) {
	want := scenario.Want(scenario.BeforeUpgrade)
	got := scenario.Census{}
	for class, n := range want {
		got[class] = n
	}
	got[scenario.OffersOpen]--
	err := got.Check(want, scenario.BeforeUpgrade)
	if err == nil || !strings.Contains(err.Error(), "offers open: 1, want 2") {
		t.Fatalf("check: %v", err)
	}
	empty := scenario.Census{}
	if err := empty.Check(empty, scenario.BeforeUpgrade); err == nil || !strings.Contains(err.Error(), "the scenario requires at least one") {
		t.Fatalf("an empty census passes the BeforeUpgrade check: %v", err)
	}
	if err := empty.Check(empty, scenario.AfterTransitions); err != nil {
		t.Fatalf("AfterTransitions requires no minimum: %v", err)
	}
	if s := want.String(); strings.Count(s, "=") != len(scenario.Classes()) {
		t.Fatalf("String: %s", s)
	}
}

// TestScenarioImportsOnlyMsgkit: the package imports only the standard
// library and chain/msgkit, so chain/app tests import it without a cycle.
func TestScenarioImportsOnlyMsgkit(t *testing.T) {
	pkg, err := build.ImportDir(".", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range pkg.Imports {
		if strings.Contains(path, ".") && path != "github.com/Maxencejules/shortvideo-network/chain/msgkit" {
			t.Errorf("scenario imports %s", path)
		}
	}
}
