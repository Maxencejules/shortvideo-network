package difftest

import (
	"fmt"
	"math/rand/v2"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Sequence sizes. The seeded run is diffSeeds sequences per family of
// diffFamilies, each with diffOps random operations after its setup steps
// (plan section 21.2: default and tight-limits, 64 seeds, 80 steps). A fuzz
// input runs at most fuzzOps operations, as many as a seeded sequence, so a
// recorded seeded sequence replays whole as a fuzz input. reachSeeds is the
// number of PCG seeds per family that -difftest.search-reach searches for
// reachCorpus.
const (
	diffSeeds  = 64
	diffOps    = 80
	fuzzOps    = diffOps
	reachSeeds = 32
)

// seqOptions configures one generated sequence.
type seqOptions struct {
	family int
	ops    int
	label  string
	// oracle is nil for the Go-only harness (FuzzEvidenceTransitions).
	oracle *oracleProc
	// multi lets the generator send audit batches of 2 to 32 items that may
	// succeed. It is never set with an oracle (deviation D-6: the oracle
	// compares multi-item batches only in count classes).
	multi bool
	// intercept, when set, wraps the harness's message routing (mutant tests).
	intercept func(h *harness)
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
	if opt.intercept != nil {
		opt.intercept(h)
	}
	pre := h.view()
	if opt.oracle != nil {
		initial := opt.oracle.reset(tb, fam.cfg, false)
		if diffs := projectionDiff(pre.projection(), initial); len(diffs) > 0 {
			tb.Errorf("%s: genesis projections differ:\n%s", opt.label, strings.Join(diffs, "\n"))
			return nil, false
		}
	}
	if opt.multi && opt.oracle != nil {
		tb.Fatalf("%s: multi-item successes cannot be compared with the oracle", opt.label)
	}
	g := newGen(c, opt.family, opt.multi)
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
			if rule := expectedDeviation(s, pre, post, found); rule != spec.deviation() {
				tb.Errorf("%s: classes %v name deviation %q, the oracle applied %q", label, found, rule, spec.deviation())
				return out, false
			}
		}
		for _, class := range found {
			classes[class]++
		}
		pre = post
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

// recordingChooser draws from a seeded PCG and records every choice in
// byteChooser format (one byte when n <= 256, two big-endian bytes when n
// is larger), so a byteChooser over the record makes the same choices.
type recordingChooser struct {
	rand randChooser
	data []byte
}

func (c *recordingChooser) intn(n int) int {
	if n <= 1 {
		return 0
	}
	if n > 1<<16 {
		panic(fmt.Sprintf("difftest: intn(%d) has no byteChooser encoding", n))
	}
	v := c.rand.intn(n)
	if n > 256 {
		c.data = append(c.data, byte(v>>8))
	}
	c.data = append(c.data, byte(v))
	return v
}

func (*recordingChooser) exhausted() bool { return false }

// The fuzz seed corpora. Each holds the recorded choice streams of seeded
// (family, seed) sequences, so the corpus a fuzz target starts from reaches
// every required class by itself, run exactly as the target runs an input
// (TestEvidenceFuzzReachesStates). transitionCorpus is recorded with the
// Go-only generator (multi-item batches) and seeds FuzzEvidenceTransitions;
// differentialCorpus is recorded with the oracle-mode generator (one item
// or a count class) and seeds FuzzEvidenceDifferential. Each list is a
// greedy cover over seeds 0..reachSeeds-1 of every family; go test -run
// TestEvidenceFuzzReachesStates -difftest.search-reach logs new covers when
// the generator changes.
var (
	transitionCorpus = []reachSeed{
		{family: famDefault, seed: 2},
		{family: famDefault, seed: 4},
		{family: famTight, seed: 5},
		{family: famDefault, seed: 13},
		{family: famFlood, seed: 6},
	}
	differentialCorpus = []reachSeed{
		{family: famDefault, seed: 14},
		{family: famTight, seed: 25},
		{family: famDefault, seed: 15},
		{family: famTight, seed: 0},
		{family: famDefault, seed: 0},
		{family: famDefault, seed: 30},
	}
)

type reachSeed struct {
	family, seed int
	// multi is the generator mode the stream was recorded in.
	multi bool
}

// recorded is one recorded seeded sequence: the fuzz input that replays it
// and the classes the seeded run reached.
type recorded struct {
	input   []byte
	classes map[string]int
}

var (
	recordedMu    sync.Mutex
	recordedCache = map[reachSeed]recorded{}
)

// record runs seeded sequence r on the Go keepers (no oracle; the
// generator mode is r.multi) through a recordingChooser, once per test
// process, and returns it as a fuzz input (the family byte, then the
// recorded choices) with its classes.
func record(tb testing.TB, r reachSeed) recorded {
	tb.Helper()
	recordedMu.Lock()
	defer recordedMu.Unlock()
	if out, ok := recordedCache[r]; ok {
		return out
	}
	c := &recordingChooser{rand: newRandChooser(uint64(r.family), uint64(r.seed))}
	classes, ok := runSequence(tb, c, seqOptions{
		family: r.family, ops: diffOps, multi: r.multi,
		label: fmt.Sprintf("family %s seed %d multi %t (recorded)", familyNames[r.family], r.seed, r.multi),
	})
	if !ok {
		tb.Fatalf("recording family %s seed %d failed", familyNames[r.family], r.seed)
	}
	out := recorded{input: append([]byte{byte(r.family)}, c.data...), classes: classes}
	recordedCache[r] = out
	return out
}

// corpusOf is the recorded corpus of one generator mode.
func corpusOf(multi bool) []reachSeed {
	list := differentialCorpus
	if multi {
		list = transitionCorpus
	}
	out := make([]reachSeed, len(list))
	for i, r := range list {
		out[i] = reachSeed{family: r.family, seed: r.seed, multi: multi}
	}
	return out
}

// randomSeeds is the number of random inputs in each fuzz seed corpus.
const randomSeeds = 6

// fuzzSeeds is the seed corpus of the fuzz target of one generator mode:
// the first byte picks the family, the rest are choices. It holds
// randomSeeds random inputs and the recorded streams of that mode's corpus.
// Every byte is a choice, never a number the oracle parses as text: the
// oracle reads numbers that Go's encoding/json writes in ASCII digits.
func fuzzSeeds(tb testing.TB, multi bool) [][]byte {
	tb.Helper()
	var seeds [][]byte
	for i := 0; i < randomSeeds; i++ {
		r := rand.New(rand.NewPCG(0xf0220009, uint64(i)))
		data := make([]byte, 801)
		data[0] = byte(i % len(familyNames))
		for j := 1; j < len(data); j++ {
			data[j] = byte(r.Uint32())
		}
		seeds = append(seeds, data)
	}
	for _, r := range corpusOf(multi) {
		seeds = append(seeds, record(tb, r).input)
	}
	return seeds
}

// runFuzzInput runs one fuzz input: with the oracle as
// FuzzEvidenceDifferential does (one-item and count-class batches), without
// it as FuzzEvidenceTransitions does (multi-item batches too). An empty
// input has no family byte and runs nothing.
func runFuzzInput(t *testing.T, data []byte, oracle *oracleProc) map[string]int {
	if len(data) == 0 {
		return nil
	}
	classes, _ := runSequence(t, &byteChooser{data: data[1:]}, seqOptions{
		family: int(data[0]) % len(familyNames),
		ops:    fuzzOps,
		label:  fmt.Sprintf("fuzz input (%d bytes, family byte %d)", len(data), data[0]),
		oracle: oracle,
		multi:  oracle == nil,
	})
	return classes
}

func merge(into, from map[string]int) {
	for class, n := range from {
		into[class] += n
	}
}

// diffClasses are the (operation, outcome) classes the seeded differential
// must reach (plan section 21.2, TestEvidenceDifferentialCoverage). The
// sub-labels in parentheses say which check produced a shared code; the
// (D-n) labels name the x/evidence deviations. SubmitAuditBatch BOUNDS(D-15)
// is not listed: it needs 1024 items from one auditor in one epoch, which
// one-item batches in an 80-step sequence cannot reach;
// TestDeviationRulesAreLoadBearing and the Go-only fuzz corpus reach it
// (reachClasses).
var diffClasses = []string{
	"SubmitAuditBatch ok(RECORDED)", "SubmitAuditBatch ok(REJECTED)", "SubmitAuditBatch idempotent",
	"SubmitAuditBatch REPLAY",
	"SubmitAuditBatch FORBIDDEN(role ADMITTED_SERVICE)", "SubmitAuditBatch FORBIDDEN(role ADMITTED_PROVIDER)",
	"SubmitAuditBatch FORBIDDEN(role BROADCASTER)", "SubmitAuditBatch FORBIDDEN(role AUTHORITY)",
	"SubmitAuditBatch UNAUTHORIZED(role)",
	"SubmitAuditBatch UNAUTHORIZED(" + sigForeignAuditor + ")", "SubmitAuditBatch UNAUTHORIZED(" + sigProviderKey + ")",
	"SubmitAuditBatch UNAUTHORIZED(" + sigNonCanonical + ")", "SubmitAuditBatch UNAUTHORIZED(" + sigReordered + ")",
	"SubmitAuditBatch WRONG_DOMAIN",
	"SubmitAuditBatch MALFORMED(kind 0)", "SubmitAuditBatch MALFORMED(kind 3)", "SubmitAuditBatch MALFORMED(kind 4)",
	"SubmitAuditBatch MALFORMED(kind 5)", "SubmitAuditBatch MALFORMED(attestation)",
	"SubmitAuditBatch BOUNDS(id)", "SubmitAuditBatch BOUNDS(business id)", "SubmitAuditBatch BOUNDS(digest)",
	"SubmitAuditBatch BOUNDS(count 0)", "SubmitAuditBatch BOUNDS(count 33)", "SubmitAuditBatch BOUNDS(governed cap)",
	"SubmitAuditBatch BOUNDS(expiry)",
	"SubmitAuditBatch STALE_VERSION(lower)", "SubmitAuditBatch STALE_VERSION(higher)",
	"SubmitAuditBatch DUPLICATE_ID(evidence)", "SubmitAuditBatch DUPLICATE_ID(batch)",
	"SubmitAuditBatch EXPIRED(item)", "SubmitAuditBatch NOT_FOUND(lease)",
	"SubmitAuditBatch FORBIDDEN(D-17)", "SubmitAuditBatch FORBIDDEN(D-8)", "SubmitAuditBatch BOUNDS(D-9)",
	"SubmitAuditBatch CONFLICT(D-10)", "SubmitAuditBatch CONFLICT(D-11 ended)", "SubmitAuditBatch CONFLICT(D-11 expired)",
	"SubmitAuditBatch EXPIRED(D-12)", "SubmitAuditBatch CONFLICT(D-18)", "SubmitAuditBatch WITHDRAWN(D-13)",

	"RenewLease NOT_FOUND(evidence)", "RenewLease FORBIDDEN(live RECORDED)", "RenewLease FORBIDDEN(live REJECTED)",
	"RenewLease EXPIRED(evidence)", "RenewLease ok",

	"Block epoch crossed", "Block evidence due",
}

// reachClasses are the classes the FuzzEvidenceTransitions seed corpus must
// reach (TestEvidenceFuzzReachesStates): the differential classes plus the
// multi-item outcomes only the Go-only generator sends.
var reachClasses = append(slices.Clone(diffClasses),
	"SubmitAuditBatch ok(multi)", "SubmitAuditBatch DUPLICATE_ID(in batch)", "SubmitAuditBatch BOUNDS(D-15)")

// signatureClasses are the UNAUTHORIZED classes of the signatures that
// cover a payload with one signed field changed (deviation D-1). Both
// harnesses must reach each one.
func signatureClasses() []string {
	var out []string
	for _, field := range signedFields {
		out = append(out, "SubmitAuditBatch UNAUTHORIZED("+sigOther+field+")")
	}
	return out
}

// checkCoverage reports every required class that classes misses and logs
// the whole table (visible with -v).
func checkCoverage(t *testing.T, what string, classes map[string]int, required []string) {
	t.Helper()
	names := make([]string, 0, len(classes))
	for class := range classes {
		names = append(names, class)
	}
	sort.Strings(names)
	var table strings.Builder
	for _, class := range names {
		fmt.Fprintf(&table, "  %-66s %6d\n", class, classes[class])
	}
	t.Logf("%s coverage (%d classes):\n%s", what, len(names), table.String())
	for _, class := range required {
		if classes[class] == 0 {
			t.Errorf("%s never reached class %q", what, class)
		}
	}
}

// stepCount is the number of classified steps in classes (every step has
// exactly one class).
func stepCount(classes map[string]int) int {
	n := 0
	for _, count := range classes {
		n += count
	}
	return n
}

// idPattern is bounds.json id_pattern, held by the harness.
var idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// classify names the coverage class of one step from the pre-step view, the
// post-step view, and the Go outcome. The sub-labels in parentheses say
// which check produced a shared code. They are derived from the message
// and the pre-step state in the chain's check order, never from the
// oracle; the (D-n) labels must agree with the oracle's deviation field
// (expectedDeviation).
func classify(s *step, pre, post *view, out outcome) []string {
	label := strings.TrimPrefix(out.codeName(), "ERROR_CODE_")
	if out.code == msgkit.OK && out.idempotent {
		label = "idempotent"
	}
	switch s.op {
	case opAudit:
		label += auditDetail(s, pre, out)
	case opRenew:
		label += renewDetail(s.msg.(*leasesv1.MsgRenewLease), pre, out.code)
	case opAccept:
		m := s.msg.(*leasesv1.MsgAcceptLease)
		if offer := pre.offers[m.GetOfferId()]; out.code == msgkit.Expired && offer != nil {
			if pre.now >= offer.GetExpiryUnixSeconds() {
				label += "(backlog)"
			} else {
				label += "(D-3)"
			}
		}
	case opEnd:
		m := s.msg.(*leasesv1.MsgEndLease)
		lease := pre.leases[m.GetLeaseId()]
		signed := s.spec["provider_signature"].(map[string]any)["term_version"]
		if out.code == msgkit.Unauthorized && lease != nil && signed != lease.GetTermVersion() {
			label += "(D-12 lease)"
		}
	case fxBlock:
		return []string{s.op + " " + blockDetail(pre, post)}
	}
	return []string{s.op + " " + label}
}

func auditDetail(s *step, pre *view, out outcome) string {
	m := s.msg.(*evidencev1.MsgSubmitAuditBatch)
	items := m.GetItems()
	role := roleTable()[string(m.GetSigner())]
	now := pre.now
	leaseOf := func(it *evidencev1.EvidenceItem) *leasesv1.Lease { return pre.leases[it.GetLeaseId()] }
	some := func(test func(*evidencev1.EvidenceItem) bool) bool { return slices.ContainsFunc(items, test) }
	switch out.code {
	case msgkit.OK:
		switch {
		case out.idempotent:
			return ""
		case len(items) > 1:
			return "(multi)"
		}
		return "(" + out.states[0] + ")"
	case msgkit.Forbidden:
		prefix := prefixOf(m.GetSigner())
		switch {
		case role != typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR:
			return "(role " + roleName(role) + ")"
		case !strings.HasPrefix(m.GetBatchId(), prefix) ||
			some(func(it *evidencev1.EvidenceItem) bool { return !strings.HasPrefix(it.GetEvidenceId(), prefix) }):
			return "(D-17)"
		case some(func(it *evidencev1.EvidenceItem) bool {
			return it.GetKind() == evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT
		}):
			return "(D-8)"
		}
	case msgkit.Unauthorized:
		if role == typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED {
			return "(role)"
		}
		return "(" + s.tag + ")"
	case msgkit.Malformed:
		for _, it := range items {
			if k := it.GetKind(); k != evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT &&
				k != evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION {
				return fmt.Sprintf("(kind %d)", int32(k))
			}
		}
		return "(attestation)"
	case msgkit.Bounds:
		switch {
		case some(func(it *evidencev1.EvidenceItem) bool { return len(it.GetObservationDigest().GetValue()) != 32 }):
			return "(digest)"
		case !idPattern.MatchString(m.GetBatchId()) || some(func(it *evidencev1.EvidenceItem) bool {
			return !idPattern.MatchString(it.GetEvidenceId()) || !idPattern.MatchString(it.GetLeaseId()) ||
				!idPattern.MatchString(it.GetContentId())
		}):
			return "(id)"
		case len(items) == 0:
			return "(count 0)"
		case len(items) > 32:
			return fmt.Sprintf("(count %d)", len(items))
		case uint64(len(items)) > pre.maxBatch:
			return "(governed cap)"
		case some(func(it *evidencev1.EvidenceItem) bool {
			return it.GetExpiryUnixSeconds() < 1 || it.GetExpiryUnixSeconds() > unixMax
		}):
			return "(expiry)"
		case !idPattern.MatchString(m.GetBusinessId()):
			return "(business id)"
		case some(func(it *evidencev1.EvidenceItem) bool { return it.GetExpiryUnixSeconds()-now > 86400 }):
			return "(D-9)"
		case pre.windowCount(m.GetSigner())+uint64(len(items)) > 1024:
			return "(D-15)"
		}
	case msgkit.StaleVersion:
		if m.GetEpoch() < epochOf(now) {
			return "(lower)"
		}
		return "(higher)"
	case msgkit.DuplicateID:
		switch {
		case pre.batches[m.GetBatchId()]:
			return "(batch)"
		case some(func(it *evidencev1.EvidenceItem) bool { _, ok := pre.records[it.GetEvidenceId()]; return ok }):
			return "(evidence)"
		case hasDuplicateItem(items):
			return "(in batch)"
		}
	case msgkit.Expired:
		if some(func(it *evidencev1.EvidenceItem) bool { return it.GetExpiryUnixSeconds() <= now }) {
			return "(item)"
		}
		return "(D-12)"
	case msgkit.NotFound:
		return "(lease)"
	case msgkit.Conflict:
		// Check-major (deviation D-6): T4 over every item, then T5, then T7.
		switch {
		case some(func(it *evidencev1.EvidenceItem) bool {
			l := leaseOf(it)
			return it.GetContentId() != l.GetContentId() || it.GetContentVersion() != l.GetContentVersion()
		}):
			return "(D-10)"
		case some(func(it *evidencev1.EvidenceItem) bool { return !leaseOf(it).GetReservationHeld() }):
			for _, it := range items {
				if l := leaseOf(it); !l.GetReservationHeld() {
					if l.GetState() == leasesv1.LeaseState_LEASE_STATE_ENDED {
						return "(D-11 ended)"
					}
					return "(D-11 expired)"
				}
			}
		}
		return "(D-18)"
	case msgkit.Withdrawn:
		return "(D-13)"
	case msgkit.Replay, msgkit.WrongDomain:
		return ""
	}
	return "(unexplained)"
}

func renewDetail(m *leasesv1.MsgRenewLease, pre *view, code msgkit.Code) string {
	id := m.GetEvidenceId()
	switch code {
	case msgkit.Forbidden:
		if r, ok := pre.records[id]; ok {
			return "(live " + strings.TrimPrefix(r.stored.GetState().String(), "EVIDENCE_STATE_") + ")"
		}
	case msgkit.Expired:
		if id != "" {
			return "(evidence)"
		}
		return "(D-2)"
	case msgkit.NotFound:
		if pre.leases[m.GetLeaseId()] == nil {
			return "(lease)"
		}
		return "(evidence)"
	}
	return ""
}

// blockDetail classifies a block by what it crossed: an epoch boundary
// (evidence D-3), the expiry of a RECORDED record (evidence D-4), both, or
// neither.
func blockDetail(pre, post *view) string {
	var parts []string
	if epochCrossed(pre, post) {
		parts = append(parts, "epoch crossed")
	}
	if evidenceFallsDue(pre, post) {
		parts = append(parts, "evidence due")
	}
	if len(parts) == 0 {
		return "plain"
	}
	return strings.Join(parts, ", ")
}

func epochCrossed(pre, post *view) bool { return epochOf(pre.now) != epochOf(post.now) }

// evidenceFallsDue reports a stored RECORDED record with pre.now < expiry
// <= post.now: spec_machine.py's op_expire would sweep it in this block.
func evidenceFallsDue(pre, post *view) bool {
	for _, id := range pre.recordIDs {
		r := pre.records[id].stored
		if r.GetState() == evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED &&
			pre.now < r.GetExpiryUnixSeconds() && r.GetExpiryUnixSeconds() <= post.now {
			return true
		}
	}
	return false
}

// expectedDeviation is the deviation rule the oracle must report for a
// step, derived on the Go side: the evidence D-6 mapping for a batch of N
// != 1 items, "evidence D-n" for an audit class labelled (D-n), the lease
// rule of a lease class, and evidence D-3 and D-4 for a block that crosses
// an epoch boundary or a RECORDED record's expiry.
func expectedDeviation(s *step, pre, post *view, classes []string) string {
	if s.op == fxBlock {
		var rules []string
		if epochCrossed(pre, post) {
			rules = append(rules, "evidence D-3")
		}
		if evidenceFallsDue(pre, post) {
			rules = append(rules, "evidence D-4")
		}
		return strings.Join(rules, "+")
	}
	if s.op == opAudit && len(stepItems(s)) != 1 {
		return "evidence D-6"
	}
	for _, class := range classes {
		open := strings.LastIndex(class, "(D-")
		if open < 0 {
			continue
		}
		rule := strings.TrimSuffix(class[open+1:], ")")
		rule, _, _ = strings.Cut(rule, " ")
		if s.op == opAudit {
			return "evidence " + rule
		}
		return rule
	}
	return ""
}
