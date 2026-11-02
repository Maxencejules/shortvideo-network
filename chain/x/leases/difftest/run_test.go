package difftest

import (
	"fmt"
	"math/rand/v2"
	"sort"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Sequence sizes. The seeded run is diffSeeds sequences per family, each
// with diffOps random operations after its setup steps (plan section
// 14.3: 4 families x 64 seeds x 80 steps). A fuzz input runs at most
// fuzzOps operations. reachSeeds is the seeded batch per family of
// TestFuzzReachesStates: locally every required class was first reached
// by seed 15, so 32 leaves a factor-two margin (TestFuzzReachesStates
// fails if a class is missed).
const (
	diffSeeds  = 64
	diffOps    = 80
	fuzzOps    = 48
	reachSeeds = 32
)

// seqOptions configures one generated sequence.
type seqOptions struct {
	family int
	ops    int
	label  string
	// oracle is nil for the Go-only harness (FuzzLeaseTransitions).
	oracle            *oracleProc
	disableDeviations bool
}

// runSequence runs the family's setup steps and then up to opt.ops random
// operations on the real keepers. After every step the harness checks the
// trial branch of a rejected message and the invariants; with an oracle it
// also compares the step with spec_machine.py. It returns the coverage
// classes reached and false at the first failure (reported through tb).
func runSequence(tb testing.TB, c chooser, opt seqOptions) (map[string]int, bool) {
	tb.Helper()
	fam := newFamily(opt.family, c, true, true)
	h := newHarness(tb, fam.cfg)
	pre := h.view()
	if opt.oracle != nil {
		initial := opt.oracle.reset(tb, fam.cfg, opt.disableDeviations)
		for _, seed := range fam.seeds {
			res := opt.oracle.step(tb, seed)
			if res.Code != "ok" {
				tb.Fatalf("%s: oracle seed %s: %s", opt.label, seed.id, res.Code)
			}
			initial = res.Projection
		}
		if diffs := projectionDiff(pre.projection(), initial); len(diffs) > 0 {
			tb.Errorf("%s: genesis projections differ:\n%s", opt.label, strings.Join(diffs, "\n"))
			return nil, false
		}
	}
	g := newGen(c, opt.family)
	g.observe(pre)
	classes := map[string]int{}
	index := 0
	run := func(s *step) (outcome, bool) {
		out := h.apply(s)
		post := h.view()
		found := classify(s, pre, post, out)
		label := fmt.Sprintf("%s step %d %s [%s]", opt.label, index, s.op, s.tag)
		if opt.oracle != nil {
			spec := opt.oracle.step(tb, s)
			if !compareStep(tb, label, s, out, post.projection(), spec) {
				return out, false
			}
			if rule := classDeviation(found); rule != spec.deviation() {
				tb.Errorf("%s: classes %v name deviation %q, the oracle applied %q", label, found, rule, spec.deviation())
				return out, false
			}
		}
		for _, class := range found {
			classes[class]++
		}
		pre = post
		g.observe(post)
		index++
		return out, !tb.Failed()
	}
	for _, s := range fam.setup {
		out, ok := run(s)
		if !ok {
			return classes, false
		}
		if out.code != msgkit.OK {
			tb.Fatalf("%s: setup step %s returned %s", opt.label, s.op, out.code)
		}
	}
	for i := 0; i < opt.ops && !c.exhausted(); i++ {
		if _, ok := run(g.next(pre)); !ok {
			return classes, false
		}
	}
	return classes, true
}

// fuzzSeeds is the seed corpus of both fuzz targets: the first byte picks
// the family, the rest are choices.
func fuzzSeeds() [][]byte {
	var seeds [][]byte
	for i := 0; i < 8; i++ {
		r := rand.New(rand.NewPCG(0xf0220008, uint64(i)))
		data := make([]byte, 601)
		data[0] = byte(i % len(familyNames))
		for j := 1; j < len(data); j++ {
			data[j] = byte(r.Uint32())
		}
		seeds = append(seeds, data)
	}
	return seeds
}

// runFuzzInput runs one fuzz input. An empty input has no family byte and
// runs nothing.
func runFuzzInput(t *testing.T, data []byte, oracle *oracleProc) map[string]int {
	if len(data) == 0 {
		return nil
	}
	classes, _ := runSequence(t, &byteChooser{data: data[1:]}, seqOptions{
		family: int(data[0]) % len(familyNames),
		ops:    fuzzOps,
		label:  fmt.Sprintf("fuzz input (%d bytes, family byte %d)", len(data), data[0]),
		oracle: oracle,
	})
	return classes
}

func merge(into, from map[string]int) {
	for class, n := range from {
		into[class] += n
	}
}

// requiredClasses are the (operation, outcome) classes a run must reach
// (plan section 14.3, TestDifferentialCoverage). Accept WITHDRAWN is not
// listed: withdrawal voids the content's offers on both sides, so no open
// offer of withdrawn content exists to accept; the P2 ladder covers that
// branch with fakes. Offer BOUNDS(D-6) needs 256 open offers on one
// content: TestWithdrawalAtContentCapGas and TestDeviationRulesAreLoadBearing
// reach it.
var requiredClasses = []string{
	"OfferLease ok", "OfferLease idempotent", "OfferLease REPLAY", "OfferLease CAPACITY", "OfferLease OVERFLOW",
	"OfferLease STALE_VERSION", "OfferLease DUPLICATE_ID(open id)", "OfferLease DUPLICATE_ID(pair)",
	"OfferLease DUPLICATE_ID(D-4)", "OfferLease BOUNDS(provider cap)", "OfferLease BOUNDS(D-5)",
	"OfferLease EXPIRED", "OfferLease OUT_OF_RANGE", "OfferLease DECISION_BLOCKED", "OfferLease WITHDRAWN",
	"OfferLease FORBIDDEN", "OfferLease INTEGRITY", "OfferLease CONFLICT", "OfferLease NOT_FOUND",
	"OfferLease UNAUTHORIZED", "OfferLease MALFORMED(huge int)",

	"AcceptLease ok", "AcceptLease idempotent", "AcceptLease REPLAY",
	"AcceptLease UNAUTHORIZED(" + sigRetired + ")", "AcceptLease UNAUTHORIZED(" + sigOtherProvider + ")",
	"AcceptLease UNAUTHORIZED(" + sigServiceOwn + ")", "AcceptLease UNAUTHORIZED(" + sigServiceAsProvide + ")",
	"AcceptLease UNAUTHORIZED(" + sigMutated + ")", "AcceptLease UNAUTHORIZED(role)", "AcceptLease WRONG_DOMAIN",
	"AcceptLease FORBIDDEN(role)", "AcceptLease FORBIDDEN(account)", "AcceptLease FORBIDDEN(provider state)",
	"AcceptLease NOT_FOUND", "AcceptLease DUPLICATE_ID(lease)", "AcceptLease DUPLICATE_ID(pair)",
	"AcceptLease EXPIRED(backlog)", "AcceptLease EXPIRED(D-3)", "AcceptLease CAPACITY", "AcceptLease OVERFLOW",

	"RenewLease ok", "RenewLease ok(provider not admitted)", "RenewLease idempotent", "RenewLease STALE_VERSION",
	"RenewLease CONFLICT(bytes)", "RenewLease CONFLICT(state)", "RenewLease WITHDRAWN", "RenewLease OUT_OF_RANGE",
	"RenewLease BOUNDS", "RenewLease NOT_FOUND(lease)", "RenewLease NOT_FOUND(evidence)", "RenewLease UNAUTHORIZED",
	"RenewLease WRONG_DOMAIN", "RenewLease FORBIDDEN", "RenewLease MALFORMED(huge int)", "RenewLease EXPIRED(D-2)",

	"EndLease ok", "EndLease ok(provider not admitted)", "EndLease idempotent", "EndLease UNDERFLOW(repeat)",
	"EndLease UNDERFLOW(after expiry)", "EndLease FORBIDDEN", "EndLease UNAUTHORIZED", "EndLease UNKNOWN_FIELD",
	"EndLease MALFORMED", "EndLease NOT_FOUND",

	"Block backlog left", "Block drained",
}

// checkCoverage reports every required class that classes misses and logs
// the whole table (visible with -v).
func checkCoverage(t *testing.T, what string, classes map[string]int) {
	t.Helper()
	names := make([]string, 0, len(classes))
	for class := range classes {
		names = append(names, class)
	}
	sort.Strings(names)
	var table strings.Builder
	for _, class := range names {
		fmt.Fprintf(&table, "  %-60s %6d\n", class, classes[class])
	}
	t.Logf("%s coverage (%d classes):\n%s", what, len(names), table.String())
	for _, class := range requiredClasses {
		if classes[class] == 0 {
			t.Errorf("%s never reached class %q", what, class)
		}
	}
}

// classify names the coverage classes of one step from the pre-step view,
// the post-step view, and the Go outcome. The sub-labels in parentheses
// say which check produced a shared code; the (D-n) labels name the
// chain-only deviations and must agree with the oracle's deviation field.
func classify(s *step, pre, post *view, out outcome) []string {
	label := strings.TrimPrefix(out.codeName(), "ERROR_CODE_")
	if out.code == msgkit.OK && out.idempotent {
		label = "idempotent"
	}
	var extra []string
	switch s.op {
	case opOffer:
		label += offerDetail(s.msg.(*leasesv1.MsgOfferLease), pre, out.code, s.tag)
	case opAccept:
		label += acceptDetail(s, pre, out.code)
	case opRenew:
		m := s.msg.(*leasesv1.MsgRenewLease)
		label += renewDetail(m, pre, out.code, s.tag)
		if out.code == msgkit.OK && !out.idempotent && !admitted(pre, pre.leases[m.GetLeaseId()].GetProviderId()) {
			extra = append(extra, s.op+" ok"+notAdmitted)
		}
	case opEnd:
		m := s.msg.(*leasesv1.MsgEndLease)
		lease := pre.leases[m.GetLeaseId()]
		if out.code == msgkit.Underflow && lease != nil {
			switch lease.GetState() {
			case leasesv1.LeaseState_LEASE_STATE_ENDED:
				label += "(repeat)"
			case leasesv1.LeaseState_LEASE_STATE_EXPIRED:
				label += "(after expiry)"
			}
		}
		if out.code == msgkit.OK && !out.idempotent && !admitted(pre, lease.GetProviderId()) {
			extra = append(extra, s.op+" ok"+notAdmitted)
		}
	case fxBlock:
		return []string{s.op + " " + blockDetail(pre, post, out)}
	}
	return append([]string{s.op + " " + label}, extra...)
}

// notAdmitted marks the second class of a successful renewal or end whose
// provider is DRAINING or DISABLED (decision request DR-08-01). Every step
// has exactly one class without it.
const notAdmitted = "(provider not admitted)"

// stepCount is the number of classified steps in classes.
func stepCount(classes map[string]int) int {
	n := 0
	for class, count := range classes {
		if !strings.HasSuffix(class, notAdmitted) {
			n += count
		}
	}
	return n
}

func admitted(v *view, providerID string) bool {
	p, ok := v.providers[providerID]
	return ok && p.GetState() == providersv1.ProviderState_PROVIDER_STATE_ADMITTED
}

func offerDetail(m *leasesv1.MsgOfferLease, pre *view, code msgkit.Code, tag string) string {
	switch code {
	case msgkit.DuplicateID:
		switch {
		case pre.offers[m.GetOfferId()] != nil:
			return "(open id)"
		case pre.pairs[pairKey(m.GetProviderId(), m.GetContentId(), m.GetContentVersion())] != "":
			return "(pair)"
		case pre.used[m.GetOfferId()]:
			return "(D-4)"
		}
		return "(unexplained)"
	case msgkit.Bounds:
		switch {
		case uint64(pre.providerOpen[m.GetProviderId()]) >= pre.maxOpenOffers:
			return "(provider cap)"
		case m.GetExpiryUnixSeconds()-pre.now > leases.MaxOfferLifetimeSeconds:
			return "(D-5)"
		case pre.contentOpen[m.GetContentId()] >= leases.MaxOpenOffersPerContent:
			return "(D-6)"
		}
		return "(unexplained)"
	case msgkit.Malformed:
		if tag == "huge int" {
			return "(huge int)"
		}
	}
	return ""
}

func acceptDetail(s *step, pre *view, code msgkit.Code) string {
	m := s.msg.(*leasesv1.MsgAcceptLease)
	offer := pre.offers[m.GetOfferId()]
	role := roleTable()[string(m.GetSigner())]
	relay := role == typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER || role == typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER
	switch code {
	case msgkit.Unauthorized:
		if role == typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
			return "(role)"
		}
		return "(" + s.tag + ")"
	case msgkit.Forbidden:
		switch {
		case !relay:
			return "(role)"
		case role == typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER && offer != nil &&
			string(m.GetSigner()) != string(pre.providers[offer.GetProviderId()].GetAccount()):
			return "(account)"
		}
		return "(provider state)"
	case msgkit.DuplicateID:
		if pre.leases[m.GetLeaseId()] != nil {
			return "(lease)"
		}
		return "(pair)"
	case msgkit.Expired:
		if offer != nil && pre.now >= offer.GetExpiryUnixSeconds() {
			return "(backlog)"
		}
		return "(D-3)"
	}
	return ""
}

func renewDetail(m *leasesv1.MsgRenewLease, pre *view, code msgkit.Code, tag string) string {
	lease := pre.leases[m.GetLeaseId()]
	switch code {
	case msgkit.Conflict:
		if lease != nil && !lease.GetReservationHeld() {
			return "(state)"
		}
		return "(bytes)"
	case msgkit.NotFound:
		if lease == nil {
			return "(lease)"
		}
		return "(evidence)"
	case msgkit.Expired:
		if m.GetEvidenceId() != "" {
			return "(evidence)"
		}
		return "(D-2)"
	case msgkit.Malformed:
		if tag == "huge int" {
			return "(huge int)"
		}
	}
	return ""
}

// blockDetail classifies a block by the expiry work it did: a full budget
// that left due items ("backlog left"), work that emptied the due set
// ("drained"), other work ("partial"), or none ("idle").
func blockDetail(pre, post *view, out outcome) string {
	processed := len(out.released)
	remaining := post.heldDue()
	switch {
	case uint64(processed) == pre.maxExpiry && remaining > 0:
		return "backlog left"
	case processed > 0 && remaining == 0:
		return "drained"
	case processed > 0:
		return "partial"
	}
	return "idle"
}

// classDeviation is the deviation rule a step's classes name, or "".
func classDeviation(classes []string) string {
	for _, class := range classes {
		for _, rule := range []string{"D-2", "D-3", "D-4", "D-5", "D-6"} {
			if strings.HasSuffix(class, "("+rule+")") {
				return rule
			}
		}
	}
	return ""
}
