package difftest

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
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

// fixture is a hand-built trace on a fixed setup: provider p0 (account 0,
// ADMITTED) and contents c0, c1, ... (newFixture: capacity 1,000,000 and
// c0..c3 of 1000 bytes; newSizedFixture: a given capacity and sizes).
type fixture struct {
	cfg   *config
	now   int64
	n     int
	steps []*step
}

func newFixture(governed map[string]uint64) *fixture {
	return newSizedFixture(governed, 1_000_000, 1000, 1000, 1000, 1000)
}

// newSizedFixture registers and admits p0 with the given capacity and
// registers content c<i> of sizes[i] bytes for each size.
func newSizedFixture(governed map[string]uint64, capacity uint64, sizes ...uint64) *fixture {
	f := &fixture{
		cfg: &config{term: uint64(termSeconds), governed: governed, trialCheck: true, invariants: true},
		now: startTime,
	}
	f.add(buildRegisterProvider(providerAccount(0), "reg-p0", "p0", "p0-k1", capacity), admitStep("p0"))
	for i, size := range sizes {
		id := fmt.Sprintf("c%d", i)
		f.add(buildRegisterContent(serviceAcct, "content-"+id, id, size))
	}
	return f
}

func (f *fixture) add(steps ...*step) { f.steps = append(f.steps, steps...) }

func (f *fixture) business() string {
	f.n++
	return fmt.Sprintf("bf%d", f.n)
}

// offer appends a valid OfferLease of contentID on p0 by serviceAcct and
// returns the record the chain stores for it.
func (f *fixture) offer(offerID, contentID string, deadline, expiry int64) *leasesv1.Offer {
	return f.offerBy(serviceAcct, f.business(), offerID, contentID, deadline, expiry)
}

// offerBy is offer with a given scheduler and business id.
func (f *fixture) offerBy(scheduler []byte, business, offerID, contentID string, deadline, expiry int64) *leasesv1.Offer {
	return f.offerSized(scheduler, business, offerID, contentID, 1000, deadline, expiry)
}

// offerSized is offerBy for a content of size bytes.
func (f *fixture) offerSized(scheduler []byte, business, offerID, contentID string, size uint64, deadline, expiry int64) *leasesv1.Offer {
	p := offerParams{
		signer: scheduler, business: business, offerID: offerID, contentID: contentID, providerID: "p0",
		version: 1, size: size, termVersion: 1, manifest: manifestFor(contentID), terms: termsFor(1),
		start: deadline - termSeconds, deadline: deadline, expiry: expiry,
	}
	f.add(buildOffer(p, ""))
	return &leasesv1.Offer{
		OfferId: offerID, ContentId: contentID, ContentVersion: 1, ProviderId: "p0",
		ManifestDigest: digest(p.manifest), PackageBytes: p.size, TermsDigest: digest(p.terms),
		StartUnixSeconds: p.start, DeadlineUnixSeconds: deadline, ExpiryUnixSeconds: expiry,
		TermVersion: 1, Scheduler: append([]byte(nil), scheduler...),
	}
}

// accept appends a broadcaster-relayed acceptance signed by p0's key.
func (f *fixture) accept(offer *leasesv1.Offer, leaseID string) {
	f.acceptBy(broadcasterAcct, f.business(), offer, leaseID)
}

// acceptBy is accept with a given signer and business id.
func (f *fixture) acceptBy(signer []byte, business string, offer *leasesv1.Offer, leaseID string) {
	signBytes := acceptSignBytes(offer, leaseID)
	s := providerSig(sigValid, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT,
		typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, signBytes, nil, "p0-k1", "", "")
	f.add(buildAccept(signer, business, offer.GetOfferId(), leaseID, s))
}

// end appends a broadcaster-relayed end of a p0 lease, signed by p0's key
// for termVersion.
func (f *fixture) end(leaseID string, termVersion uint64) {
	signBytes := endSignBytes(leaseID, "p0", termVersion)
	s := providerSig(sigValid, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END,
		typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, signBytes, nil, "p0-k1", "", "")
	f.add(buildEnd(broadcasterAcct, f.business(), leaseID, false, s, termVersion))
}

// renew appends a valid renewal of a 1000-byte lease of contentID.
func (f *fixture) renew(leaseID, contentID string, termVersion uint64, deadline int64) {
	p := renewParams{
		signer: broadcasterAcct, business: f.business(), leaseID: leaseID, termVersion: termVersion,
		deadline: deadline, manifest: manifestFor(contentID), size: 1000,
	}
	s := providerSig(sigValid, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW,
		typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, p.signBytes(), nil, "p0-k1", "", "")
	f.add(buildRenew(p, s, ""))
}

func (f *fixture) block(t int64) {
	f.now = t
	f.add(blockStep(t))
}

// fixtureRun is one run of a fixture against the oracle.
type fixtureRun struct {
	firstMismatch int
	outs          []outcome
	projections   []projection
	specs         []specResult
}

// runFixture applies every step to fresh keepers and to the oracle. The
// harness and the oracle report through tb; comparisons report through
// cmp. It stops at the first step where they differ.
func runFixture(tb, cmp testing.TB, f *fixture, o *oracleProc, disableDeviations bool) fixtureRun {
	tb.Helper()
	h := newHarness(tb, f.cfg)
	o.reset(tb, f.cfg, disableDeviations)
	run := fixtureRun{firstMismatch: -1}
	for i, s := range f.steps {
		out := h.apply(s)
		proj := h.view().projection()
		spec := o.step(tb, s)
		run.outs = append(run.outs, out)
		run.projections = append(run.projections, proj)
		run.specs = append(run.specs, spec)
		if !compareStep(cmp, fmt.Sprintf("fixture step %d", i), s, out, proj, spec) {
			run.firstMismatch = i
			return run
		}
	}
	return run
}

// ruleFixtures builds one trace per chain deviation rule and returns the
// index of the step the rule rejects.
func ruleFixtures() map[string]func() (*fixture, int) {
	return map[string]func() (*fixture, int){
		"D-2": func() (*fixture, int) {
			f := newFixture(map[string]uint64{msgkit.LimitMaxExpirySteps: 1})
			due := f.now + 10
			f.accept(f.offer("o1", "c0", due, f.now+600), "l1")
			f.accept(f.offer("o2", "c1", due, f.now+600), "l2")
			// l1 and l2 fall due together; a budget of 1 sweeps l1 only.
			f.block(due)
			f.renew("l2", "c1", 2, due+termSeconds)
			return f, len(f.steps) - 1
		},
		"D-3": func() (*fixture, int) {
			f := newFixture(nil)
			o := f.offer("o1", "c0", f.now+5, f.now+600)
			f.block(f.now + 5)
			f.accept(o, "l1")
			return f, len(f.steps) - 1
		},
		"D-4": func() (*fixture, int) {
			f := newFixture(nil)
			f.accept(f.offer("o1", "c0", f.now+termSeconds, f.now+600), "l1")
			f.offer("o1", "c1", f.now+termSeconds, f.now+600)
			return f, len(f.steps) - 1
		},
		"D-5": func() (*fixture, int) {
			f := newFixture(nil)
			f.offer("o1", "c0", f.now+termSeconds, f.now+leases.MaxOfferLifetimeSeconds+1)
			return f, len(f.steps) - 1
		},
		"D-6": func() (*fixture, int) {
			// Two schedulers fill c0 (one alone stops at D-10); the 257th
			// offer comes from a third one with no offer of c0, so D-10
			// cannot reject it and only D-6 does.
			f := newFixture(nil)
			for i := 0; i < leases.MaxOpenOffersPerContent; i++ {
				scheduler := [][]byte{serviceAcct, service2Acct}[i%2]
				f.offerBy(scheduler, f.business(), fmt.Sprintf("o%03d", i), "c0", f.now+termSeconds, f.now+600)
			}
			f.offerBy(service3Acct, f.business(), "o-third", "c0", f.now+termSeconds, f.now+600)
			return f, len(f.steps) - 1
		},
		"D-10": func() (*fixture, int) {
			f := newFixture(nil)
			for i := 0; i <= leases.MaxOpenOffersPerContentPerScheduler; i++ {
				f.offer(fmt.Sprintf("o%03d", i), "c0", f.now+termSeconds, f.now+600)
			}
			return f, len(f.steps) - 1
		},
		"D-11": func() (*fixture, int) {
			// service2 commits a copy of serviceAcct's offer first; the
			// original's identical message is then not idempotent.
			f := newFixture(nil)
			f.offerBy(service2Acct, "b-copied", "o1", "c0", f.now+termSeconds, f.now+600)
			f.offerBy(serviceAcct, "b-copied", "o1", "c0", f.now+termSeconds, f.now+600)
			return f, len(f.steps) - 1
		},
		"D-12": func() (*fixture, int) {
			// An end signed for term 1, relayed after the renewal to term 2.
			f := newFixture(nil)
			f.accept(f.offer("o1", "c0", f.now+termSeconds, f.now+600), "l1")
			f.renew("l1", "c0", 2, f.now+2*termSeconds)
			f.end("l1", 1)
			return f, len(f.steps) - 1
		},
		"D-13": func() (*fixture, int) {
			// p1's account replays the broadcaster's acceptance of a p0
			// offer under its business id and payload.
			f := newFixture(nil)
			f.add(buildRegisterProvider(providerAccount(1), "reg-p1", "p1", "p1-k1", 1_000_000), admitStep("p1"))
			offer := f.offer("o1", "c0", f.now+termSeconds, f.now+600)
			f.acceptBy(broadcasterAcct, "b-accepted", offer, "l1")
			f.acceptBy(providerAccount(1), "b-accepted", offer, "l1")
			return f, len(f.steps) - 1
		},
	}
}

// TestDeviationRulesAreLoadBearing runs one trace per adapter rule
// (deviationRules: D-2 to D-6, D-10 to D-13). With the oracle's rules on,
// every step agrees and the rule step carries its rule. With the rules off
// (disable_deviations), the first divergence is exactly the rule step,
// where spec_machine.py alone says ok. So each rule is needed, and no
// other adapter rule hides a difference. The generator's own restrictions
// (TestGeneratorRestrictionsHideKnownDivergences) are not adapter rules.
func TestDeviationRulesAreLoadBearing(t *testing.T) {
	o := getOracle(t)
	fixtures := ruleFixtures()
	for _, rule := range deviationRules {
		t.Run(rule, func(t *testing.T) {
			f, at := fixtures[rule]()
			on := runFixture(t, t, f, o, false)
			if on.firstMismatch != -1 {
				t.Fatalf("with the rules on, step %d diverged", on.firstMismatch)
			}
			if got := on.specs[at].deviation(); got != rule || on.outs[at].code == msgkit.OK {
				t.Fatalf("rule step %d: chain %s, oracle deviation %q, want a rejection under %s", at, on.outs[at].code, got, rule)
			}
			for i, spec := range on.specs {
				if i != at && spec.Deviation != nil {
					t.Errorf("step %d carries deviation %s", i, *spec.Deviation)
				}
			}
			rec := &recorder{TB: t}
			off := runFixture(t, rec, f, o, true)
			if off.firstMismatch != at {
				t.Fatalf("with the rules off, the first divergence is step %d, want %d", off.firstMismatch, at)
			}
			if off.specs[at].Code != "ok" || len(rec.errors) == 0 {
				t.Fatalf("with the rules off, the spec says %s at step %d (recorded %d errors)", off.specs[at].Code, at, len(rec.errors))
			}
			t.Logf("%s: chain %s at step %d; spec_machine.py alone: ok", rule, on.outs[at].code, at)
		})
	}
}

// TestGeneratorRestrictionsHideKnownDivergences shows that two generator
// restrictions hide real chain/spec differences that are not lease rules
// and have no adapter rule: with every adapter rule on, the first
// divergence of each trace is its last step.
//
//   - DR-08-10: RegisterProvider with a capacity of 2^53. x/providers
//     accepts it (decimal-string payload); spec_machine.py says MALFORMED.
//   - DR-07-08: a provider registration replayed by another provider
//     account. x/providers says FORBIDDEN; spec_machine.py says idempotent.
func TestGeneratorRestrictionsHideKnownDivergences(t *testing.T) {
	o := getOracle(t)
	for _, c := range []struct {
		name       string
		last       *step
		chain      msgkit.Code
		spec       string
		idempotent bool
	}{
		{"DR-08-10 capacity 2^53", buildRegisterProvider(providerAccount(1), "reg-p9", "p9", "p9-k1", 1<<53),
			msgkit.OK, string(msgkit.Malformed), false},
		{"DR-07-08 registration replay by another account", buildRegisterProvider(providerAccount(1), "reg-p0", "p0", "p0-k1", 1_000_000),
			msgkit.Forbidden, "ok", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(nil)
			f.add(c.last)
			at := len(f.steps) - 1
			rec := &recorder{TB: t}
			run := runFixture(t, rec, f, o, false)
			if run.firstMismatch != at || len(rec.errors) == 0 {
				t.Fatalf("first divergence at step %d, want %d (reports %v)", run.firstMismatch, at, rec.errors)
			}
			out, spec := run.outs[at], run.specs[at]
			if out.code != c.chain || spec.Code != c.spec || spec.Idempotent != c.idempotent {
				t.Fatalf("chain %s, spec %s idempotent %t; want chain %s, spec %s idempotent %t",
					out.code, spec.Code, spec.Idempotent, c.chain, c.spec, c.idempotent)
			}
			t.Logf("%s: chain %s, spec_machine.py %s (idempotent %t)", c.name, out.codeName(), spec.Code, spec.Idempotent)
		})
	}
}

// TestComparatorDetectsMismatch perturbs one field of a real oracle answer
// (or of the Go outcome) at a time and requires compareStep to report it.
func TestComparatorDetectsMismatch(t *testing.T) {
	o := getOracle(t)
	f := newFixture(nil)
	f.accept(f.offer("o1", "c0", f.now+termSeconds, f.now+600), "l1")
	run := runFixture(t, t, f, o, false)
	if run.firstMismatch != -1 {
		t.Fatalf("fixture diverged at step %d", run.firstMismatch)
	}
	at := len(f.steps) - 1
	s, out, proj, spec := f.steps[at], run.outs[at], run.projections[at], run.specs[at]
	if !compareStep(t, "unperturbed", s, out, proj, spec) {
		t.Fatalf("the unperturbed step must agree")
	}
	raw, err := json.Marshal(spec)
	if err != nil {
		t.Fatal(err)
	}
	mutants := []struct {
		name   string
		mutate func(*specResult)
	}{
		{"code", func(r *specResult) { r.Code = string(msgkit.Capacity) }},
		{"idempotent flag", func(r *specResult) { r.Idempotent = !r.Idempotent }},
		{"event id", func(r *specResult) { r.Events[0].ID = "l-other" }},
		{"event type", func(r *specResult) { r.Events[0].Type = "LeaseRenewed" }},
		{"event dropped", func(r *specResult) { r.Events = nil }},
		{"chain time", func(r *specResult) { r.Projection.ChainTime = "1" }},
		{"reserved bytes", func(r *specResult) { r.Projection.Providers[0].ReservedBytes = "999" }},
		{"capacity", func(r *specResult) { r.Projection.Providers[0].AdmittedCapacityBytes = "1" }},
		{"key generation", func(r *specResult) { r.Projection.Providers[0].KeyGeneration = "2" }},
		{"content state", func(r *specResult) { r.Projection.Contents[0].State = "WITHDRAWN" }},
		{"lease state", func(r *specResult) { r.Projection.Leases[0].State = "ENDED" }},
		{"reservation held", func(r *specResult) { r.Projection.Leases[0].ReservationHeld = false }},
		{"lease deadline", func(r *specResult) { r.Projection.Leases[0].DeadlineUnixSeconds = "1" }},
		{"lease policy version", func(r *specResult) { r.Projection.Leases[0].PolicyVersion = "2" }},
		{"pair lease id", func(r *specResult) { r.Projection.Pairs[0].LeaseID = "l-other" }},
		{"pair dropped", func(r *specResult) { r.Projection.Pairs = nil }},
		{"open offer added", func(r *specResult) { r.Projection.Offers = append(r.Projection.Offers, offerView{OfferID: "o-extra"}) }},
		{"business id dropped", func(r *specResult) { r.Projection.BusinessIDs = r.Projection.BusinessIDs[1:] }},
	}
	for _, m := range mutants {
		var mutated specResult
		if err := json.Unmarshal(raw, &mutated); err != nil {
			t.Fatal(err)
		}
		m.mutate(&mutated)
		rec := &recorder{TB: t}
		if compareStep(rec, m.name, s, out, proj, mutated) || len(rec.errors) == 0 {
			t.Errorf("mutant %q was not detected", m.name)
		}
	}
	goMutant := out
	goMutant.code = msgkit.Capacity
	rec := &recorder{TB: t}
	if compareStep(rec, "go code", s, goMutant, proj, spec) || len(rec.errors) == 0 {
		t.Errorf("a changed Go code was not detected")
	}
	t.Logf("%d spec mutants and 1 Go mutant detected", len(mutants))
}

// TestTrialSnapshotDetectsEarlyWrite shows the reject check compares the
// trial branch: a handler wrapper that writes a key and then returns the
// real handler's result is reported for a rejected message and for an
// idempotent replay, while the unwrapped handler is not.
func TestTrialSnapshotDetectsEarlyWrite(t *testing.T) {
	f := newFixture(nil)
	offer := f.offer("o1", "c0", f.now+termSeconds, f.now+600)
	h := newHarness(t, f.cfg)
	for _, s := range f.steps {
		if out := h.apply(s); out.code != msgkit.OK {
			t.Fatalf("setup %s: %s", s.op, out.code)
		}
	}
	rejected := buildOffer(offerParams{
		signer: outsiderAcct, business: "b-outsider", offerID: "o2", contentID: "c1", providerID: "p0",
		version: 1, size: 1000, termVersion: 1, manifest: manifestFor("c1"), terms: termsFor(1),
		start: f.now, deadline: f.now + termSeconds, expiry: f.now + 600,
	}, "")
	replay := f.steps[len(f.steps)-1]
	if replay.msg.(*leasesv1.MsgOfferLease).GetOfferId() != offer.GetOfferId() {
		t.Fatalf("the last fixture step is not the offer")
	}

	rec := &recorder{TB: t}
	h.tb = rec
	if out := h.apply(rejected); out.code != msgkit.Unauthorized || len(rec.errors) != 0 {
		t.Fatalf("control: code %s, reports %v", out.code, rec.errors)
	}
	if out := h.apply(replay); !out.idempotent || len(rec.errors) != 0 {
		t.Fatalf("control replay: idempotent %t, reports %v", out.idempotent, rec.errors)
	}

	// The mutant writes into the authority store, which every trial
	// snapshot covers and no invariant reads.
	h.intercept = func(ctx sdk.Context, msg proto.Message, next func(sdk.Context, proto.Message) error) error {
		ctx.KVStore(h.keys[0]).Set([]byte("early-write"), []byte{1})
		return next(ctx, msg)
	}
	if out := h.apply(rejected); out.code != msgkit.Unauthorized {
		t.Fatalf("mutant reject: code %s", out.code)
	}
	if len(rec.errors) != 1 || !strings.Contains(rec.errors[0], "changed the trial state") {
		t.Fatalf("the early write of a rejected message was not reported: %v", rec.errors)
	}
	if out := h.apply(replay); !out.idempotent {
		t.Fatalf("mutant replay is not idempotent")
	}
	if len(rec.errors) != 2 || !strings.Contains(rec.errors[1], "idempotent") {
		t.Fatalf("the write of an idempotent replay was not reported: %v", rec.errors)
	}
}
