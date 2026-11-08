package difftest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// recorder collects Errorf calls instead of failing, so a test can show a
// check fires. Fatalf and everything else go to the real test.
type recorder struct {
	testing.TB
	errors []string
}

func (r *recorder) Errorf(format string, args ...any) {
	r.errors = append(r.errors, fmt.Sprintf(format, args...))
}

func (r *recorder) Failed() bool { return len(r.errors) > 0 || r.TB.Failed() }

// fixture is a hand-built trace on a fixed setup: providers p0-p2 (accounts
// 0-2, ADMITTED, 1,000,000 bytes), contents c0..c<k-1> of 1000 bytes (k at
// least 4), and one held lease sl<i> per given deadline (provider
// p<i mod 3>, content c<i>), all accepted at startTime.
type fixture struct {
	cfg    *config
	now    int64
	n      int
	steps  []*step
	leases map[string]*leasesv1.Lease
}

func newFixture(governed map[string]uint64, deadlines ...int64) *fixture {
	if governed == nil {
		governed = map[string]uint64{}
	}
	f := &fixture{
		cfg:    &config{governed: governed, trialCheck: true, invariants: true},
		now:    startTime,
		leases: map[string]*leasesv1.Lease{},
	}
	for i := 0; i < setupProviders; i++ {
		id := fmt.Sprintf("p%d", i)
		f.add(buildRegisterProvider(providerAccount(i), "reg-"+id, id, id+"-k1", 1_000_000), admitStep(id))
	}
	for i := 0; i < max(4, len(deadlines)); i++ {
		id := fmt.Sprintf("c%d", i)
		f.add(buildRegisterContent("content-"+id, id, contentBytes))
	}
	for i, deadline := range deadlines {
		p := offerParams{
			business: fmt.Sprintf("setup-o%d", i), offerID: fmt.Sprintf("so%d", i), contentID: fmt.Sprintf("c%d", i),
			providerID: fmt.Sprintf("p%d", i%setupProviders), size: contentBytes, deadline: deadline, expiry: startTime + 600,
		}
		leaseID := fmt.Sprintf("sl%d", i)
		f.add(buildOffer(p), buildAccept(fmt.Sprintf("setup-a%d", i), offerRecord(p), leaseID, p.providerID+"-k1"))
		f.leases[leaseID] = &leasesv1.Lease{
			LeaseId: leaseID, OfferId: p.offerID, ContentId: p.contentID, ContentVersion: 1, ProviderId: p.providerID,
			PackageBytes: contentBytes, ManifestDigest: digest(manifestFor(p.contentID)),
			DeadlineUnixSeconds: deadline, TermVersion: 1,
		}
	}
	return f
}

func (f *fixture) add(steps ...*step) { f.steps = append(f.steps, steps...) }

func (f *fixture) fresh(prefix string) string {
	f.n++
	return fmt.Sprintf("%s%d", prefix, f.n)
}

// item is a passing item of leaseID by signer: its own evidence id, the
// lease's content, the lease's manifest digest (RECORDED), expiry
// now + lifetime.
func (f *fixture) item(signer []byte, leaseID string, lifetime int64) auditItem {
	lease := f.leases[leaseID]
	return auditItem{
		evidenceID: prefixOf(signer) + f.fresh("e"), kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
		leaseID: leaseID, contentID: lease.GetContentId(), contentVersion: 1,
		digest: lease.GetManifestDigest().GetValue(), expiry: f.now + lifetime, attestation: true,
	}
}

// params is a batch of items by signer with a valid signature, a fresh
// business id, a fresh batch id with the signer's prefix, and the current
// epoch.
func (f *fixture) params(signer []byte, items ...auditItem) auditParams {
	return auditParams{
		signer: signer, business: f.fresh("bf"), batchID: prefixOf(signer) + f.fresh("batch"),
		epoch: epochOf(f.now), items: items, variant: sigValid,
	}
}

// audit appends f.params(signer, items...) and returns its index.
func (f *fixture) audit(signer []byte, items ...auditItem) int {
	return f.auditWith(f.params(signer, items...))
}

func (f *fixture) auditWith(p auditParams) int {
	f.add(p.build(""))
	return len(f.steps) - 1
}

func (f *fixture) block(t int64) int {
	f.now = t
	f.add(blockStep(t))
	return len(f.steps) - 1
}

// end appends a provider end of a setup lease at its first term.
func (f *fixture) end(leaseID string) int {
	lease := f.leases[leaseID]
	f.add(buildEnd(f.fresh("bf"), lease, lease.GetProviderId()+"-k1"))
	return len(f.steps) - 1
}

// renew appends a renewal of a setup lease at its first term to its next
// term, naming evidenceID.
func (f *fixture) renew(leaseID, evidenceID string) int {
	lease := f.leases[leaseID]
	f.add(buildRenew(f.fresh("bf"), lease, lease.GetProviderId()+"-k1", evidenceID))
	return len(f.steps) - 1
}

func (f *fixture) withdraw(contentID string) int {
	f.add(buildWithdraw(f.fresh("bf"), contentID))
	return len(f.steps) - 1
}

// fixtureRun is one run of a fixture against the oracle.
type fixtureRun struct {
	firstMismatch int
	outs          []outcome
	projections   []projection
	specs         []specResult
	// expected is the deviation rule expectedDeviation derives for each step.
	expected []string
}

// runFixture applies every step to fresh keepers and to the oracle, reset
// with disable (false, true, or a list of rule names). The harness and the
// oracle report through tb; comparisons report through cmp. It stops at the
// first step where the two sides differ, or where the oracle's deviation
// differs from the Go-side derivation. rewrite, when set, may replace a
// step's spec view before the oracle sees it (the D-6 mapping test).
func runFixture(tb, cmp testing.TB, f *fixture, o *oracleProc, disable any, rewrite func(i int, s *step) *step) fixtureRun {
	tb.Helper()
	return runFixtureFrom(tb, cmp, f, o, disable, rewrite, fixtureRun{}, 0)
}

// runFixtureFrom is runFixture whose first brief steps are compared without
// projections: the oracle sends none (stepBrief) and the harness builds no
// view. At those steps the Go code, idempotent flag, and events must equal
// the oracle's, and the oracle's deviation must equal ref's, a run of the
// same fixture with every rule on that agreed at every step: a rule that
// is off changes the oracle only at a step where it fires, and ref shows
// where each rule fired. The tests use it for the runs with rules off,
// whose first divergence is expected at step brief; the full comparison
// starts there.
func runFixtureFrom(tb, cmp testing.TB, f *fixture, o *oracleProc, disable any, rewrite func(i int, s *step) *step,
	ref fixtureRun, brief int) fixtureRun {
	tb.Helper()
	h := newHarness(tb, f.cfg)
	o.reset(tb, f.cfg, disable)
	run := fixtureRun{firstMismatch: -1}
	for i, s := range f.steps[:brief] {
		out := h.apply(s)
		spec := o.stepBrief(tb, s)
		run.outs = append(run.outs, out)
		run.projections = append(run.projections, projection{})
		run.specs = append(run.specs, spec)
		run.expected = append(run.expected, ref.expected[i])
		label := fmt.Sprintf("fixture step %d %s (no projection)", i, s.op)
		if !compareBrief(cmp, label, s, out, spec) {
			run.firstMismatch = i
			return run
		}
		if want := ref.specs[i].deviation(); want != spec.deviation() {
			cmp.Errorf("%s: the run with every rule on applied deviation %q, this run %q", label, want, spec.deviation())
			run.firstMismatch = i
			return run
		}
	}
	pre := h.view()
	for i := brief; i < len(f.steps); i++ {
		s := f.steps[i]
		out := h.apply(s)
		post := h.view()
		proj := post.projection()
		sent := s
		if rewrite != nil {
			sent = rewrite(i, s)
		}
		spec := o.step(tb, sent)
		want := expectedDeviation(s, pre, post, classify(s, pre, post, out))
		run.outs = append(run.outs, out)
		run.projections = append(run.projections, proj)
		run.specs = append(run.specs, spec)
		run.expected = append(run.expected, want)
		label := fmt.Sprintf("fixture step %d %s", i, s.op)
		if !compareStep(cmp, label, sent, out, proj, spec) {
			run.firstMismatch = i
			return run
		}
		if want != spec.deviation() {
			cmp.Errorf("%s: the Go side derives deviation %q, the oracle applied %q", label, want, spec.deviation())
			run.firstMismatch = i
			return run
		}
		pre = post
	}
	return run
}

// mustAgree runs f with every rule on and fails unless every step agrees.
func mustAgree(t *testing.T, f *fixture, o *oracleProc) fixtureRun {
	t.Helper()
	run := runFixture(t, t, f, o, false, nil)
	if run.firstMismatch != -1 {
		t.Fatalf("with every rule on, step %d diverged", run.firstMismatch)
	}
	return run
}

// requireCodes checks the chain code of the listed steps.
func (r fixtureRun) requireCodes(t *testing.T, want map[int]msgkit.Code) {
	t.Helper()
	for i, code := range want {
		if r.outs[i].code != code {
			t.Errorf("step %d: chain %s, want %s", i, r.outs[i].code, code)
		}
	}
}

// evidenceState is the reported and stored state of one record in a
// projection, or "" when absent.
func evidenceState(p projection, id string) (reported, stored string) {
	for _, r := range p.Evidence {
		if r.EvidenceID == id {
			return r.State, r.StoredState
		}
	}
	return "", ""
}

func joinErrors(errs []string) string { return strings.Join(errs, "\n") }
