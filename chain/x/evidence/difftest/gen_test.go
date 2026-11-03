package difftest

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// chooser is the generator's source of choices: a seeded PCG for the
// seeded runs, or fuzz input bytes.
type chooser interface {
	// intn returns a value in [0, n); n <= 1 returns 0.
	intn(n int) int
	// exhausted reports that a byte stream is used up; the generator then
	// starts no further operation.
	exhausted() bool
}

type randChooser struct{ r *rand.Rand }

func newRandChooser(family, seed uint64) randChooser {
	return randChooser{r: rand.New(rand.NewPCG(0x5eed0009+family, seed))}
}

func (c randChooser) intn(n int) int {
	if n <= 1 {
		return 0
	}
	return c.r.IntN(n)
}

func (randChooser) exhausted() bool { return false }

// byteChooser reads choices from fuzz input. A value below 257 takes one
// byte, a larger one two. Past the end it returns 0.
type byteChooser struct {
	data []byte
	pos  int
}

func (c *byteChooser) next() int {
	if c.pos >= len(c.data) {
		c.pos++
		return 0
	}
	b := c.data[c.pos]
	c.pos++
	return int(b)
}

func (c *byteChooser) intn(n int) int {
	if n <= 1 {
		return 0
	}
	v := c.next()
	if n > 256 {
		v = v<<8 | c.next()
	}
	return v % n
}

func (c *byteChooser) exhausted() bool { return c.pos >= len(c.data) }

// pick returns an index with probability proportional to its weight.
func pick(c chooser, weights ...int) int {
	total := 0
	for _, w := range weights {
		total += w
	}
	r := c.intn(total)
	for i, w := range weights {
		if r < w {
			return i
		}
		r -= w
	}
	return len(weights) - 1
}

func choose[T any](c chooser, items []T) T { return items[c.intn(len(items))] }

// Families of chain configuration and generator emphasis (plan section
// 21.2). The seeded differential runs default and tight (diffFamilies);
// the fuzz targets also run flood, whose Go-only sequences fill an
// auditor's per-epoch window with 32-item batches (deviation D-15).
var familyNames = []string{"default", "tight", "flood"}

const (
	famDefault = iota
	famTight
	famFlood
)

var diffFamilies = []int{famDefault, famTight}

// contentBytes is the package size of every content.
const contentBytes uint64 = 1000

// setupProviders is the number of providers each family registers and
// admits (p0, p1, p2; provider accounts 0-2, keys "<id>-k1").
const setupProviders = 3

// family is one sequence's chain configuration and setup steps.
type family struct {
	cfg   *config
	setup []*step
}

func newFamily(index int, c chooser, trialCheck, invariants bool) *family {
	f := &family{cfg: &config{governed: map[string]uint64{}, trialCheck: trialCheck, invariants: invariants}}
	if index == famTight {
		f.cfg.governed[msgkit.LimitMaxExpirySteps] = 1
		f.cfg.governed[msgkit.LimitMaxEvidenceBatch] = uint64(2 + c.intn(3))
	}
	for i := 0; i < setupProviders; i++ {
		id := fmt.Sprintf("p%d", i)
		f.setup = append(f.setup, buildRegisterProvider(providerAccount(i), "reg-"+id, id, id+"-k1", 1_000_000), admitStep(id))
	}
	contents := 4 + c.intn(2)
	for i := 0; i < contents; i++ {
		id := fmt.Sprintf("c%d", i)
		f.setup = append(f.setup, buildRegisterContent("content-"+id, id, contentBytes))
	}
	// Three held leases sl0-sl2 (provider p<i>, content c<i>). Tight deadlines
	// share one boundary, so a budget of one leaves held leases past their
	// deadline (deviation D-12); flood deadlines outlive the sequence.
	deadlines := []int64{startTime + 2*termSeconds, startTime + 3*termSeconds, startTime + 6*termSeconds}
	switch index {
	case famTight:
		deadlines = []int64{startTime + 600, startTime + 600, startTime + 1200}
	case famFlood:
		deadlines = []int64{startTime + 20*termSeconds, startTime + 20*termSeconds, startTime + 20*termSeconds}
	}
	for i, deadline := range deadlines {
		p := offerParams{
			business: fmt.Sprintf("setup-o%d", i), offerID: fmt.Sprintf("so%d", i), contentID: fmt.Sprintf("c%d", i),
			providerID: fmt.Sprintf("p%d", i), size: contentBytes, deadline: deadline, expiry: startTime + 600,
		}
		f.setup = append(f.setup, buildOffer(p),
			buildAccept(fmt.Sprintf("setup-a%d", i), offerRecord(p), fmt.Sprintf("sl%d", i), p.providerID+"-k1"))
	}
	return f
}

// gen builds random operations from the committed view. The view only
// selects inputs; expected outcomes come from the oracle (or, in the
// Go-only fuzzer, from the invariants).
type gen struct {
	c      chooser
	family int
	// multi allows audit batches of 2 to 32 items that may succeed. Only the
	// Go-only harness sets it: the oracle maps a batch to one spec step and
	// compares multi-item batches only in count classes (deviation D-6).
	multi bool
	n     int
	steps []*step
	// offers is every offer the generator made, for acceptances.
	offers map[string]offerParams
}

func newGen(c chooser, family int, multi bool) *gen {
	return &gen{c: c, family: family, multi: multi, offers: map[string]offerParams{}}
}

func (g *gen) fresh(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

// Operation weights: audit, retry, count-only batch, renew, end, withdraw,
// block, offer, accept, governed limit. They sum to 100 (plan section
// 21.2: audit 35, retry 8, count 3, renew 8 + 4 plain, end 5, withdraw 3,
// block 12, offer and accept 20, governed limit 2).
var (
	opWeights    = []int{35, 8, 3, 12, 5, 3, 12, 9, 11, 2}
	floodWeights = []int{75, 2, 1, 3, 1, 0, 6, 5, 7, 0}
)

// next returns the next random operation for the committed view v.
func (g *gen) next(v *view) *step {
	weights := opWeights
	if g.family == famFlood {
		weights = floodWeights
	}
	var s *step
	switch pick(g.c, weights...) {
	case 0:
		s = g.audit(v, "", nil)
	case 1:
		s = g.retry(v)
	case 2:
		s = g.countBatch(v)
	case 3:
		s = g.renew(v)
	case 4:
		s = g.end(v)
	case 5:
		s = g.withdraw(v)
	case 6:
		s = blockStep(g.blockTime(v))
	case 7:
		s = g.offer(v)
	case 8:
		s = g.accept(v)
	default:
		s = g.governed()
	}
	if s.msg != nil {
		g.steps = append(g.steps, s)
	}
	return s
}

// cleanShare is the percentage of audits generated in clean mode: every
// field takes a passing value, so sequences record evidence and reach the
// later checks. The other audits perturb each field independently.
const cleanShare = 35

// varier returns a pick that is always 0 (the passing choice) when clean.
func (g *gen) varier(clean bool) func(weights ...int) int {
	return func(weights ...int) int {
		if clean {
			return 0
		}
		return pick(g.c, weights...)
	}
}

// otherAuditor is the auditor that is not addr (auditor A for a non-auditor).
func otherAuditor(addr []byte) []byte {
	if string(addr) == string(auditorA) {
		return auditorB
	}
	return auditorA
}

// pickLease returns a lease to audit: a held lease whose deadline has not
// passed (always when clean and one exists), a held lease past its
// deadline, any lease, or "l-none".
func (g *gen) pickLease(v *view, clean bool) (string, *leasesv1.Lease) {
	var live, unswept []string
	for _, id := range v.heldLeases() {
		if v.leases[id].GetDeadlineUnixSeconds() > v.now {
			live = append(live, id)
		} else {
			unswept = append(unswept, id)
		}
	}
	choice := 0
	if !clean {
		choice = pick(g.c, 70, 8, 17, 5)
	}
	switch {
	case choice <= 1 && len(unswept) > 0 && (choice == 1 || len(live) == 0):
		id := choose(g.c, unswept)
		return id, v.leases[id]
	case choice <= 1 && len(live) > 0:
		id := choose(g.c, live)
		return id, v.leases[id]
	case choice <= 2 && len(v.leaseIDs) > 0:
		id := choose(g.c, v.leaseIDs)
		return id, v.leases[id]
	}
	return "l-none", nil
}

// auditSigner is the outer signer of an audit: an auditor when clean.
func (g *gen) auditSigner(clean bool) []byte {
	vary := g.varier(clean)
	switch vary(70, 18, 2, 2, 2, 2, 4) {
	case 0:
		return auditorA
	case 1:
		return auditorB
	case 2:
		return serviceAcct
	case 3:
		return providerAccount(1)
	case 4:
		return broadcasterAcct
	case 5:
		return authorityAcct
	}
	return outsiderAcct
}

// ownedID is an id with signer's prefix, or, by vary, a foreign prefix
// (D-17), an id outside the pattern, or an earlier id (duplicates).
func (g *gen) ownedID(signer []byte, letter string, vary func(...int) int, earlier []string) string {
	prefix := prefixOf(signer)
	switch vary(92, 3, 2, 3) {
	case 1:
		return prefixOf(otherAuditor(signer)) + g.fresh(letter)
	case 2:
		return prefix + strings.ToUpper(g.fresh(letter))
	case 3:
		if len(earlier) > 0 {
			return choose(g.c, earlier)
		}
	}
	return prefix + g.fresh(letter)
}

// item builds one audit item of leaseID (lease may be nil).
func (g *gen) item(v *view, signer []byte, leaseID string, lease *leasesv1.Lease, clean bool) auditItem {
	vary := g.varier(clean)
	it := auditItem{
		evidenceID: g.ownedID(signer, "e", vary, v.recordIDs), kind: evidencev1.EvidenceKind_EVIDENCE_KIND_AUDITOR_ATTESTATION,
		leaseID: leaseID, contentID: "c0", contentVersion: 1, digest: manifestFor("c0"), attestation: true,
	}
	deadline := v.now + termSeconds
	if lease != nil {
		it.contentID, it.contentVersion = lease.GetContentId(), lease.GetContentVersion()
		it.digest = lease.GetManifestDigest().GetValue()
		deadline = lease.GetDeadlineUnixSeconds()
	}
	switch vary(90, 5, 3, 2) {
	case 1:
		it.contentID = choose(g.c, []string{"c0", "c1", "c2", "c3"})
	case 2:
		it.contentVersion = 2
	case 3:
		it.contentVersion = 0
	}
	switch vary(65, 30, 3, 2) {
	case 1:
		it.digest = sha("observed other bytes:" + it.leaseID)
	case 2:
		it.digest = it.digest[:31]
	case 3:
		it.digest = append(append([]byte(nil), it.digest...), 0x00)
	}
	switch vary(92, 4, 1, 1, 1, 1) {
	case 1:
		it.kind = evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT
	case 2:
		it.kind = evidencev1.EvidenceKind_EVIDENCE_KIND_UNSPECIFIED
	case 3:
		it.kind = 3
	case 4:
		it.kind = 4
	case 5:
		it.kind = 5
	}
	if vary(97, 3) == 1 {
		it.attestation = false
	}
	// The clean expiry is after now and at most min(now + 86400, deadline);
	// when the deadline has passed no expiry passes, and now + 1 is used.
	ceiling := min(v.now+86400, deadline)
	clean1 := min(v.now+choose(g.c, []int64{1, 60, 600, 3600, 86400}), ceiling)
	if clean1 <= v.now {
		clean1 = v.now + 1
	}
	it.expiry = clean1
	switch vary(70, 4, 2, 4, 4, 4, 4, 2, 2, 2, 2) {
	case 1:
		it.expiry = v.now
	case 2:
		it.expiry = v.now - 10
	case 3:
		it.expiry = v.now + 86400
	case 4:
		it.expiry = v.now + 86401
	case 5:
		it.expiry = deadline
	case 6:
		it.expiry = deadline + 1
	case 7:
		it.expiry = 0
	case 8:
		it.expiry = unixMax + 1
	case 9:
		it.expiry = -5
	case 10:
		it.expiry = v.now + 1
	}
	return it
}

// signatureVariant picks the signature variant of an audit batch.
func (g *gen) signatureVariant(clean bool, items int) string {
	if clean {
		return sigValid
	}
	variants := []string{sigValid, sigForeignAuditor, sigProviderKey, sigWrongDomain, sigNonCanonical}
	weights := []int{150, 3, 3, 4, 3}
	for _, field := range signedFields {
		variants = append(variants, sigOther+field)
		weights = append(weights, 2)
	}
	if items >= 2 {
		variants = append(variants, sigReordered)
		weights = append(weights, 6)
	}
	return variants[pick(g.c, weights...)]
}

// audit builds a SubmitAuditBatch. business and signer are set by a retry
// with a changed payload (the same business id, by the same signer);
// otherwise both are fresh choices. It has one item, or, in the Go-only
// harness, often 2 to 32.
func (g *gen) audit(v *view, business string, signer []byte) *step {
	clean := pick(g.c, cleanShare, 100-cleanShare) == 0
	if g.family == famFlood && pick(g.c, 3, 1) == 0 {
		clean = true
	}
	vary := g.varier(clean)
	if signer == nil {
		signer = g.auditSigner(clean)
		if g.family == famFlood && pick(g.c, 19, 1) == 0 {
			signer = auditorA
		}
	}
	if business == "" {
		business = g.fresh("b")
		if vary(97, 3) == 1 {
			business = "B-" + business
		}
	}
	p := auditParams{
		signer: signer, business: business, batchID: g.ownedID(signer, "batch", vary, v.batchIDs),
		epoch: epochOf(v.now),
	}
	switch vary(92, 4, 4) {
	case 1:
		p.epoch--
	case 2:
		p.epoch++
	}
	count := 1
	switch {
	case g.multi && g.family == famFlood && pick(g.c, 1, 9) == 1:
		count = min(32, int(v.maxBatch))
	case g.multi && pick(g.c, 45, 55) == 1:
		count = min(2+g.c.intn(31), int(v.maxBatch))
	}
	// The items after the first are clean nine times in ten (in flood 49 in
	// 50), so a multi-item batch often passes; one batch in ten repeats an
	// earlier item's evidence id (deviation D-6, DUPLICATE_ID in the batch).
	dirty := 10
	if g.family == famFlood {
		dirty = 50
	}
	for i := 0; i < count; i++ {
		itemClean := clean || i > 0 && pick(g.c, dirty-1, 1) == 0
		leaseID, lease := g.pickLease(v, itemClean)
		p.items = append(p.items, g.item(v, signer, leaseID, lease, itemClean))
	}
	if count >= 2 && pick(g.c, 9, 1) == 1 {
		p.items[1+g.c.intn(count-1)].evidenceID = p.items[0].evidenceID
	}
	p.variant = g.signatureVariant(clean, count)
	return p.build("")
}

// countBatch is a batch whose item count fails the count check: 0, 33, or
// one above the effective max_evidence_batch. Item 0 is a generated item;
// the others pass every check before the count (presence, digest, ids,
// kind), so the chain and the oracle's one-step mapping (item 0 and
// item_count, or the synthetic item for an empty batch) reach the same
// check (deviation D-6).
func (g *gen) countBatch(v *view) *step {
	clean := pick(g.c, 70, 30) == 0
	vary := g.varier(clean)
	signer := g.auditSigner(clean)
	counts := []int{0, 33}
	if v.maxBatch < 32 {
		counts = append(counts, int(v.maxBatch)+1)
	}
	count := choose(g.c, counts)
	p := auditParams{
		signer: signer, business: g.fresh("b"), batchID: g.ownedID(signer, "batch", vary, v.batchIDs),
		epoch: epochOf(v.now),
	}
	for i := 0; i < count; i++ {
		leaseID, lease := g.pickLease(v, clean || i > 0)
		p.items = append(p.items, g.item(v, signer, leaseID, lease, clean || i > 0))
	}
	p.variant = g.signatureVariant(clean, count)
	return p.build("")
}

// retry resends an earlier transaction exactly (same business id and
// payload), or reuses its business id for a new audit by the same signer
// (an audit's signer, or auditor A for a lease or content step).
//
// Generator restriction (deviation D-2, not an adapter rule): a changed
// payload keeps an audit's signer. spec_machine.py has one auditor key, so
// another auditor's own valid signature over a batch whose spec fields
// equal the first one is an idempotent success there, while the chain's
// business payload carries the real signature and answers REPLAY.
// TestGeneratorRestrictionsHideKnownDivergences shows it.
func (g *gen) retry(v *view) *step {
	var candidates []*step
	for _, s := range g.steps {
		if s.businessID() != "" {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return g.audit(v, "", nil)
	}
	prior := choose(g.c, candidates)
	if pick(g.c, 60, 40) == 0 {
		return resend(prior, "retry")
	}
	signer := auditorA
	if prior.op == opAudit {
		signer = prior.signer()
	}
	return g.audit(v, prior.businessID(), signer)
}

// renew renews a lease naming an evidence id two times in three: a live
// RECORDED record, a live REJECTED record, an expired record, or an id no
// record has. Every such renewal is rejected (FORBIDDEN, EXPIRED,
// NOT_FOUND). A renewal without an evidence id moves the lease to its next
// term.
func (g *gen) renew(v *view) *step {
	held := v.heldLeases()
	if len(held) == 0 {
		return g.accept(v)
	}
	lease := v.leases[choose(g.c, held)]
	evidenceID := ""
	if pick(g.c, 1, 2) == 1 {
		var recorded, rejected, expired []string
		for _, id := range v.recordIDs {
			switch v.records[id].reported {
			case evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED:
				recorded = append(recorded, id)
			case evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED:
				rejected = append(rejected, id)
			default:
				expired = append(expired, id)
			}
		}
		evidenceID = prefixOf(auditorA) + g.fresh("absent")
		for _, set := range [][]string{recorded, rejected, expired}[pick(g.c, 1, 1, 1):] {
			if len(set) > 0 && pick(g.c, 4, 1) == 0 {
				evidenceID = choose(g.c, set)
				break
			}
		}
	}
	return buildRenew(g.fresh("b"), lease, lease.GetProviderId()+"-k1", evidenceID)
}

// end ends a held lease (or any lease) with its provider's signature.
func (g *gen) end(v *view) *step {
	ids := v.heldLeases()
	if len(ids) == 0 || pick(g.c, 9, 1) == 1 {
		ids = v.leaseIDs
	}
	if len(ids) == 0 {
		return g.accept(v)
	}
	lease := v.leases[choose(g.c, ids)]
	return buildEnd(g.fresh("b"), lease, lease.GetProviderId()+"-k1")
}

// withdraw withdraws a content that is not withdrawn (its leases stay held:
// deviation D-13 refuses audits of them).
func (g *gen) withdraw(v *view) *step {
	var live []string
	for _, id := range v.contentIDs {
		if v.contents[id].GetState() != contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
			live = append(live, id)
		}
	}
	if len(live) == 0 {
		return g.audit(v, "", nil)
	}
	return buildWithdraw(g.fresh("b"), choose(g.c, live))
}

// blockTime is the header time of the next block: a short step, exactly the
// next epoch boundary, exactly a record's expiry, or exactly a lease
// deadline (deviation D-3 and D-4 edges, plan section 21.2).
func (g *gen) blockTime(v *view) int64 {
	if g.family == famFlood {
		return v.now + choose(g.c, []int64{1, 1, 10, 60})
	}
	steps := []int64{1, 10, 60, 600, termSeconds}
	if g.family == famTight {
		steps = []int64{1, 2, 5, 60, 120, 600}
	}
	var expiries, deadlines []int64
	for _, id := range v.recordIDs {
		if t := v.records[id].stored.GetExpiryUnixSeconds(); t > v.now {
			expiries = append(expiries, t)
		}
	}
	for _, id := range v.leaseIDs {
		if t := v.leases[id].GetDeadlineUnixSeconds(); t > v.now {
			deadlines = append(deadlines, t)
		}
	}
	switch pick(g.c, 55, 15, 15, 15) {
	case 1:
		return (v.now/epochSeconds + 1) * epochSeconds
	case 2:
		if len(expiries) > 0 {
			return choose(g.c, expiries)
		}
	case 3:
		if len(deadlines) > 0 {
			return choose(g.c, deadlines)
		}
	}
	return v.now + choose(g.c, steps)
}

// offer is a valid offer on a (provider, content) pair without a held
// lease, of a content that is not withdrawn.
func (g *gen) offer(v *view) *step {
	type pair struct{ provider, content string }
	var free []pair
	for i := 0; i < setupProviders; i++ {
		providerID := fmt.Sprintf("p%d", i)
		for _, contentID := range v.contentIDs {
			if v.contents[contentID].GetState() == contentv1.ContentState_CONTENT_STATE_WITHDRAWN {
				continue
			}
			if v.pairs[pairKey(providerID, contentID, 1)] == "" {
				free = append(free, pair{providerID, contentID})
			}
		}
	}
	if len(free) == 0 {
		return g.audit(v, "", nil)
	}
	chosen := choose(g.c, free)
	deadline := v.now + choose(g.c, []int64{1800, termSeconds, 2 * termSeconds})
	switch g.family {
	case famTight:
		deadline = (v.now+5)/60*60 + 60*choose(g.c, []int64{1, 1, 2})
	case famFlood:
		deadline = v.now + 20*termSeconds
	}
	p := offerParams{
		business: g.fresh("b"), offerID: g.fresh("o"), contentID: chosen.content, providerID: chosen.provider,
		size: contentBytes, deadline: deadline, expiry: v.now + choose(g.c, []int64{30, 600}),
	}
	g.offers[p.offerID] = p
	return buildOffer(p)
}

// accept accepts an open offer the generator made (a fresh lease id).
func (g *gen) accept(v *view) *step {
	var open []string
	for _, id := range v.offerIDs {
		if _, ours := g.offers[id]; ours {
			open = append(open, id)
		}
	}
	if len(open) == 0 {
		return g.offer(v)
	}
	p := g.offers[choose(g.c, open)]
	return buildAccept(g.fresh("b"), offerRecord(p), g.fresh("l"), p.providerID+"-k1")
}

// governed is a council GovernedLimitChange of max_evidence_batch or
// max_expiry_steps to a value within [1, ceiling].
func (g *gen) governed() *step {
	if g.c.intn(2) == 0 {
		return governedStep(msgkit.LimitMaxEvidenceBatch, choose(g.c, []uint64{1, 2, 4, 32}))
	}
	return governedStep(msgkit.LimitMaxExpirySteps, choose(g.c, []uint64{1, 2, 128}))
}

// stepItems is the items of an audit step's message.
func stepItems(s *step) []*evidencev1.EvidenceItem {
	return s.msg.(*evidencev1.MsgSubmitAuditBatch).GetItems()
}

// hasDuplicateItem reports an evidence id repeated inside a batch.
func hasDuplicateItem(items []*evidencev1.EvidenceItem) bool {
	ids := make([]string, 0, len(items))
	for _, it := range items {
		if slices.Contains(ids, it.GetEvidenceId()) {
			return true
		}
		ids = append(ids, it.GetEvidenceId())
	}
	return false
}
