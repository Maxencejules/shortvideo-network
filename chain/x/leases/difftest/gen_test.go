package difftest

import (
	"fmt"
	"math"
	"math/rand/v2"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
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
	return randChooser{r: rand.New(rand.NewPCG(0x5eed0008+family, seed))}
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

// Families of chain configuration (plan section 14.3).
var familyNames = []string{"default", "tight", "near-max", "no-dec005"}

const (
	famDefault = iota
	famTight
	famNearMax
	famNoTerm
)

// contentSizes are the package sizes new contents choose from.
var contentSizes = []uint64{800, 1000, 1200, 1500, 2000}

// family is one sequence's chain configuration and setup steps. seeds are
// the SeedProvider fixture steps: the Go side writes them at genesis
// (config.seeds), the oracle receives them right after its reset.
type family struct {
	cfg   *config
	seeds []*step
	setup []*step
}

func newFamily(index int, c chooser, trialCheck, invariants bool) *family {
	f := &family{
		cfg: &config{
			term: uint64(termSeconds), governed: map[string]uint64{},
			trialCheck: trialCheck, invariants: invariants,
		},
	}
	registered := []struct {
		id       string
		account  int
		capacity uint64
		admit    bool
	}{
		{"p0", 0, choose(c, []uint64{50_000, 1_000_000}), true},
		{"p1", 1, choose(c, []uint64{4000, 6000}), true},
		{"p2", 2, choose(c, []uint64{2500, 3000}), pick(c, 3, 1) == 0},
	}
	switch index {
	case famTight:
		f.cfg.governed[msgkit.LimitMaxExpirySteps] = uint64(1 + c.intn(2))
		f.cfg.governed[msgkit.LimitMaxOpenOffers] = uint64(2 + c.intn(2))
	case famNearMax:
		// Seeded counters near 2^64-1 (cases.py seed_admitted style): p0
		// overflows on its second 1000-byte lease, p1 runs out of capacity.
		f.cfg.seeds = []*providersv1.Provider{
			seedRecord("p0", providerAccount(0), math.MaxUint64, math.MaxUint64-1500),
			seedRecord("p1", providerAccount(1), math.MaxUint64-7500, math.MaxUint64-10_000),
		}
		registered = registered[2:]
		registered[0].admit = true
	case famNoTerm:
		f.cfg.term = 0
	}
	for _, seed := range f.cfg.seeds {
		f.seeds = append(f.seeds, seedStep(seed))
	}
	for _, p := range registered {
		f.setup = append(f.setup, buildRegisterProvider(providerAccount(p.account), "reg-"+p.id, p.id, p.id+"-k1", p.capacity))
		if p.admit {
			f.setup = append(f.setup, admitStep(p.id))
		}
	}
	contents := 4 + c.intn(2)
	for i := 0; i < contents; i++ {
		id := fmt.Sprintf("c%d", i)
		f.setup = append(f.setup, buildRegisterContent(serviceAcct, "content-"+id, id, choose(c, contentSizes)))
	}
	return f
}

// gen builds random operations from the committed view. The view only
// selects inputs; expected outcomes come from the oracle (or, in the
// Go-only fuzzer, from the invariants).
type gen struct {
	c chooser
	// short makes deadlines and block steps short (the tight family), so
	// held leases pile up behind a small expiry budget and stay unswept
	// past their deadlines.
	short    bool
	n        int
	steps    []*step
	offerIDs []string
	// keyIDs is every signing key id each provider has had, oldest first.
	keyIDs       map[string][]string
	spareAccount int
}

func newGen(c chooser, family int) *gen {
	return &gen{c: c, short: family == famTight, keyIDs: map[string][]string{}, spareAccount: 3}
}

func (g *gen) fresh(prefix string) string {
	g.n++
	return fmt.Sprintf("%s%d", prefix, g.n)
}

// observe records key rotations seen in the committed view.
func (g *gen) observe(v *view) {
	for _, id := range v.providerIDs {
		current := v.providers[id].GetSigningKeyId()
		history := g.keyIDs[id]
		if len(history) == 0 || history[len(history)-1] != current {
			g.keyIDs[id] = append(history, current)
		}
	}
}

func (g *gen) currentKey(v *view, providerID string) string {
	if p, ok := v.providers[providerID]; ok {
		return p.GetSigningKeyId()
	}
	return providerID + "-k1"
}

func (g *gen) retiredKey(v *view, providerID string) string {
	history := g.keyIDs[providerID]
	current := g.currentKey(v, providerID)
	var retired []string
	for _, id := range history {
		if id != current {
			retired = append(retired, id)
		}
	}
	if len(retired) == 0 {
		return ""
	}
	return choose(g.c, retired)
}

func (g *gen) otherKey(v *view, providerID string) string {
	var others []string
	for _, id := range v.providerIDs {
		if id != providerID {
			others = append(others, v.providers[id].GetSigningKeyId())
		}
	}
	if len(others) == 0 {
		return ""
	}
	return choose(g.c, others)
}

// pickProvider returns a registered provider id, an ADMITTED one four
// times in five (always when clean) when one exists, or "p-none" with
// unknownPercent.
func (g *gen) pickProvider(v *view, unknownPercent int, clean bool) string {
	if len(v.providerIDs) == 0 || (!clean && pick(g.c, 100-unknownPercent, unknownPercent) == 1) {
		return "p-none"
	}
	var admittedIDs []string
	for _, id := range v.providerIDs {
		if admitted(v, id) {
			admittedIDs = append(admittedIDs, id)
		}
	}
	if len(admittedIDs) > 0 && (clean || pick(g.c, 80, 20) == 0) {
		return choose(g.c, admittedIDs)
	}
	return choose(g.c, v.providerIDs)
}

// pickContent returns a registered content id, a PENDING one in most
// cases (always when clean) when one exists, or "c-none" with
// unknownPercent.
func (g *gen) pickContent(v *view, unknownPercent int, clean bool) string {
	if len(v.contentIDs) == 0 || (!clean && pick(g.c, 100-unknownPercent, unknownPercent) == 1) {
		return "c-none"
	}
	var pending []string
	for _, id := range v.contentIDs {
		if v.contents[id].GetState() == contentv1.ContentState_CONTENT_STATE_PENDING {
			pending = append(pending, id)
		}
	}
	if len(pending) > 0 && (clean || pick(g.c, 85, 15) == 0) {
		return choose(g.c, pending)
	}
	return choose(g.c, v.contentIDs)
}

func (g *gen) providerAccountOf(v *view, providerID string) []byte {
	if p, ok := v.providers[providerID]; ok {
		return p.GetAccount()
	}
	return providerAccount(0)
}

// otherProviderAccount is an ADMITTED_PROVIDER account that is not the
// provider's own.
func (g *gen) otherProviderAccount(v *view, providerID string) []byte {
	own := g.providerAccountOf(v, providerID)
	for i := 0; i < providerAccounts; i++ {
		candidate := providerAccount((i + g.c.intn(providerAccounts)) % providerAccounts)
		if string(candidate) != string(own) {
			return candidate
		}
	}
	return providerAccount(7)
}

// relaySigner is the outer signer of a provider-signed lease message. When
// clean it is the broadcaster or the provider's own account.
func (g *gen) relaySigner(v *view, providerID string, clean bool, weights ...int) []byte {
	if clean {
		weights = weights[:2]
	}
	switch pick(g.c, weights...) {
	case 0:
		return broadcasterAcct
	case 1:
		return g.providerAccountOf(v, providerID)
	case 2:
		return g.otherProviderAccount(v, providerID)
	case 3:
		return serviceAcct
	case 4:
		return auditorAcct
	case 5:
		return outsiderAcct
	default:
		return authorityAcct
	}
}

// Operation weights (plan section 14.3): offer, accept, renew, end, block,
// retry, update capacity, withdraw, drain or disable, rotate, register
// content, register provider, council admit. They sum to 100. The tight
// family renews more and commits blocks less often, so renewals meet held
// leases whose deadline passed while expiry had no budget left (D-2).
var (
	opWeights      = []int{27, 25, 10, 10, 10, 5, 3, 3, 2, 2, 1, 1, 1}
	tightOpWeights = []int{25, 25, 16, 8, 7, 5, 3, 3, 2, 2, 2, 1, 1}
)

// next returns the next random operation for the committed view v.
func (g *gen) next(v *view) *step {
	var s *step
	weights := opWeights
	if g.short {
		weights = tightOpWeights
	}
	op := pick(g.c, weights...)
	// An operation without an object to act on mostly becomes the step that
	// creates one (an accept without open offers becomes an offer, a renew
	// or end without leases an accept), so sequences reach deep states.
	if (op == 1 && len(v.offerIDs) == 0) || ((op == 2 || op == 3) && len(v.leaseIDs) == 0) {
		if pick(g.c, 70, 30) == 0 {
			op--
			if op == 2 {
				op = 1
			}
		}
	}
	switch op {
	case 0:
		s = g.offer(v, "")
	case 1:
		s = g.accept(v, "", false)
	case 2:
		s = g.renew(v, "")
	case 3:
		s = g.end(v, "")
	case 4:
		steps := []int64{1, 1, 1, 3, 3, 10, 10, 60, 60, 600, 600, termSeconds}
		if g.short {
			steps = []int64{1, 1, 1, 2, 2, 3, 5, 120, 600}
		}
		s = blockStep(v.now + choose(g.c, steps))
	case 5:
		s = g.retry(v)
	case 6:
		s = g.update(v)
	case 7:
		s = g.withdraw(v)
	case 8:
		s = g.drainOrDisable(v)
	case 9:
		s = g.rotate(v)
	case 10:
		s = buildRegisterContent(serviceAcct, g.fresh("b"), g.fresh("c"), choose(g.c, contentSizes))
	case 11:
		s = g.registerProvider()
	default:
		s = g.admit(v)
	}
	if s.msg != nil {
		g.steps = append(g.steps, s)
	}
	return s
}

func (g *gen) business(given string) string {
	if given != "" {
		return given
	}
	return g.fresh("b")
}

// cleanShare is the percentage of lease operations generated in clean
// mode: every field takes its passing value, so sequences build leases,
// renewals, and expiry backlogs deep enough to reach the later checks.
// The other operations perturb each field independently.
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

func (g *gen) offer(v *view, business string) *step {
	clean := pick(g.c, cleanShare, 100-cleanShare) == 0
	vary := g.varier(clean)
	p := offerParams{business: g.business(business), terms: termsFor(g.c.intn(2))}
	p.signer = [][]byte{serviceAcct, service2Acct, broadcasterAcct, auditorAcct, outsiderAcct}[vary(88, 4, 3, 2, 3)]
	if len(g.offerIDs) > 0 && vary(85, 15) == 1 {
		p.offerID = choose(g.c, g.offerIDs)
	} else {
		p.offerID = g.fresh("o")
		g.offerIDs = append(g.offerIDs, p.offerID)
	}
	p.providerID = g.pickProvider(v, 3, clean)
	p.contentID = g.pickContent(v, 3, clean)
	size, manifest := uint64(1000), manifestFor(p.contentID)
	if c, ok := v.contents[p.contentID]; ok {
		size, manifest = c.GetPackageBytes(), c.GetManifestDigest().GetValue()
	}
	tag := ""
	switch vary(94, 3, 3) {
	case 0:
		p.version = 1
	case 1:
		p.version = 2
	default:
		p.version, tag = hugeInt, "huge int"
	}
	switch vary(92, 5, 3) {
	case 0:
		p.size = size
	case 1:
		p.size = size + 1
	default:
		p.size = size - 1
	}
	p.manifest = manifest
	if vary(96, 4) == 1 {
		p.manifest = sha("manifest:other")
	}
	if g.short {
		// Deadlines on shared 60-second boundaries: several leases fall due
		// in the same block and wait behind the expiry budget.
		p.deadline = (v.now+5)/60*60 + 60*choose(g.c, []int64{1, 1, 2})
	} else {
		p.deadline = v.now + []int64{2, 6, 30, 600, termSeconds, 2 * termSeconds}[pick(g.c, 15, 15, 15, 20, 20, 15)]
	}
	switch vary(93, 3, 2, 2) {
	case 0:
		p.start = p.deadline - termSeconds
	case 1:
		p.start = p.deadline - termSeconds + 1
	case 2:
		p.start = p.deadline
	default:
		p.start = p.deadline + 5
	}
	if clean {
		p.expiry = v.now + choose(g.c, []int64{30, 600, termSeconds})
	} else {
		p.expiry = v.now + []int64{0, 1, 3, 30, 600, 86400, 86401}[pick(g.c, 3, 8, 8, 25, 30, 12, 4)]
	}
	switch vary(95, 3, 2) {
	case 0:
		p.termVersion = 1
	case 1:
		p.termVersion = 2
	default:
		p.termVersion, tag = hugeInt, "huge int"
	}
	return buildOffer(p, tag)
}

// accept builds an AcceptLease. validOnly keeps every signature variant
// whose abstract validity is true (used when the business id is reused:
// deviation D-1).
func (g *gen) accept(v *view, business string, validOnly bool) *step {
	clean := pick(g.c, cleanShare, 100-cleanShare) == 0
	vary := g.varier(clean)
	fresh := business == ""
	business = g.business(business)
	var offerID, lapsed = "", g.lapsedOffers(v)
	switch {
	case clean && len(v.offerIDs) > 0:
		offerID = choose(g.c, v.offerIDs)
	case len(lapsed) > 0 && pick(g.c, 75, 25) == 1:
		offerID = choose(g.c, lapsed)
	case len(v.offerIDs) > 0 && pick(g.c, 82, 18) == 0:
		offerID = choose(g.c, v.offerIDs)
	case len(g.offerIDs) > 0 && pick(g.c, 70, 30) == 0:
		offerID = choose(g.c, g.offerIDs)
	default:
		offerID = "o-none"
	}
	_, open := v.offers[offerID]
	providerID := "p0"
	if o, ok := v.offers[offerID]; ok {
		providerID = o.GetProviderId()
	} else if len(v.providerIDs) > 0 {
		providerID = choose(g.c, v.providerIDs)
	}
	leaseID := g.fresh("l")
	if len(v.leaseIDs) > 0 && vary(88, 12) == 1 {
		leaseID = choose(g.c, v.leaseIDs)
	}
	signer := g.relaySigner(v, providerID, clean, 60, 24, 5, 4, 2, 5)
	variant := []string{sigValid, sigRetired, sigOtherProvider, sigServiceOwn, sigServiceAsProvide, sigMutated, sigWrongDomain}[vary(66, 6, 5, 4, 5, 7, 7)]
	if (variant == sigServiceAsProvide || variant == sigMutated) && (validOnly || !fresh || !open) {
		variant = sigValid
	}
	offer := acceptOffer(v, offerID, providerID)
	signBytes := acceptSignBytes(offer, leaseID)
	mutatedOffer := cloneOffer(offer)
	switch g.c.intn(4) {
	case 0:
		mutatedOffer.DeadlineUnixSeconds++
	case 1:
		mutatedOffer.PackageBytes++
	case 2:
		mutatedOffer.TermsDigest = digest(termsFor(7))
	default:
		mutatedOffer.ExpiryUnixSeconds++
	}
	s := providerSig(variant, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT,
		typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, signBytes, acceptSignBytes(mutatedOffer, leaseID),
		g.currentKey(v, providerID), g.retiredKey(v, providerID), g.otherKey(v, providerID))
	mustAbstract(s, signBytes)
	return buildAccept(signer, business, offerID, leaseID, s)
}

// lapsedOffers is the open offers whose deadline or expiry is not after the
// view's time: acceptance is EXPIRED for them (expiry backlog or D-3).
func (g *gen) lapsedOffers(v *view) []string {
	var out []string
	for _, id := range v.offerIDs {
		o := v.offers[id]
		if o.GetDeadlineUnixSeconds() <= v.now || o.GetExpiryUnixSeconds() <= v.now {
			out = append(out, id)
		}
	}
	return out
}

func cloneOffer(o *leasesv1.Offer) *leasesv1.Offer { return proto.Clone(o).(*leasesv1.Offer) }

func mustAbstract(s signed, expected []byte) {
	if err := checkAbstract(s, expected); err != nil {
		panic("difftest generator: " + err.Error())
	}
}

// pickLease returns a held lease, any lease, or "l-none" by weight, falling
// back from held to any to none when a set is empty. A held lease whose
// deadline has passed but which expiry has not swept yet (a backlog) is
// chosen four times in five when one exists.
func (g *gen) pickLease(v *view, heldWeight, anyWeight, noneWeight int, clean bool) (string, *leasesv1.Lease) {
	var held, unswept []string
	for _, id := range v.leaseIDs {
		if l := v.leases[id]; l.GetReservationHeld() {
			held = append(held, id)
			if l.GetDeadlineUnixSeconds() <= v.now {
				unswept = append(unswept, id)
			}
		}
	}
	choice := 0
	if !clean {
		choice = pick(g.c, heldWeight, anyWeight, noneWeight)
	}
	if choice == 0 && len(unswept) > 0 && pick(g.c, 80, 20) == 0 {
		id := choose(g.c, unswept)
		return id, v.leases[id]
	}
	if choice == 0 && len(held) > 0 {
		id := choose(g.c, held)
		return id, v.leases[id]
	}
	if choice <= 1 && len(v.leaseIDs) > 0 {
		id := choose(g.c, v.leaseIDs)
		return id, v.leases[id]
	}
	return "l-none", nil
}

func (g *gen) renew(v *view, business string) *step {
	clean := pick(g.c, cleanShare, 100-cleanShare) == 0
	vary := g.varier(clean)
	leaseID, lease := g.pickLease(v, 70, 20, 10, clean)
	providerID, term, deadline, size, manifest := "p0", uint64(1), v.now+termSeconds, uint64(1000), manifestFor("c0")
	if lease != nil {
		providerID, term, deadline = lease.GetProviderId(), lease.GetTermVersion(), lease.GetDeadlineUnixSeconds()
		size, manifest = lease.GetPackageBytes(), lease.GetManifestDigest().GetValue()
	}
	p := renewParams{business: g.business(business), leaseID: leaseID, manifest: manifest}
	tag := ""
	switch vary(85, 5, 5, 5) {
	case 0:
		p.termVersion = term + 1
	case 1:
		p.termVersion = term
	case 2:
		p.termVersion = term + 2
	default:
		p.termVersion, tag = hugeInt, "huge int"
	}
	switch vary(80, 4, 4, 4, 4, 4) {
	case 0:
		p.deadline = deadline + termSeconds
	case 1:
		p.deadline = deadline + termSeconds + 1
	case 2:
		p.deadline = deadline + termSeconds - 1
	case 3:
		p.deadline = deadline + 2*termSeconds
	case 4:
		p.deadline, tag = int64(hugeInt), "huge int"
	default:
		p.deadline = unixMax + 1
	}
	switch vary(92, 5, 3) {
	case 0:
		p.size = size
	case 1:
		p.size = size + 1
	default:
		p.size, tag = hugeInt, "huge int"
	}
	if vary(95, 5) == 1 {
		p.manifest = sha("manifest:other")
	}
	if vary(94, 6) == 1 {
		p.evidenceID = "ev1"
	}
	p.signer = g.relaySigner(v, providerID, clean, 60, 24, 5, 4, 0, 7)
	signBytes := p.signBytes()
	mutated := p
	mutated.deadline++
	variant := []string{sigValid, sigRetired, sigOtherProvider, sigMutated, sigWrongDomain}[vary(78, 5, 5, 6, 6)]
	s := providerSig(variant, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW,
		typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, signBytes, mutated.signBytes(),
		g.currentKey(v, providerID), g.retiredKey(v, providerID), g.otherKey(v, providerID))
	mustAbstract(s, signBytes)
	return buildRenew(p, s, tag)
}

func (g *gen) end(v *view, business string) *step {
	clean := pick(g.c, cleanShare, 100-cleanShare) == 0
	vary := g.varier(clean)
	business = g.business(business)
	leaseID, lease := g.pickLease(v, 60, 30, 10, clean)
	providerID := "p0"
	if lease != nil {
		providerID = lease.GetProviderId()
	}
	signBytes := endSignBytes(leaseID, providerID)
	current := g.currentKey(v, providerID)
	if pick(g.c, 75, 25) == 1 {
		signer := [][]byte{authorityAcct, broadcasterAcct, outsiderAcct}[vary(80, 10, 10)]
		var s signed
		if vary(90, 10) == 1 {
			s = providerSig(sigValid, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END,
				typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, signBytes, nil, current, "", "")
			s.variant = "authority end with signature"
		}
		return buildEnd(signer, business, leaseID, true, s)
	}
	signer := g.relaySigner(v, providerID, clean, 60, 22, 6, 0, 0, 6, 6)
	if vary(97, 3) == 1 {
		return buildEnd(signer, business, leaseID, false, signed{variant: "no signature"})
	}
	variant := []string{sigValid, sigRetired, sigOtherProvider, sigMutated, sigWrongDomain}[vary(82, 4, 4, 5, 5)]
	s := providerSig(variant, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END,
		typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, signBytes, endSignBytes("l-other", providerID),
		current, g.retiredKey(v, providerID), g.otherKey(v, providerID))
	mustAbstract(s, signBytes)
	return buildEnd(signer, business, leaseID, false, s)
}

// retry resends an earlier transaction exactly (same business id and
// payload) or reuses its business id for a new lease operation (changed
// payload). An acceptance with an invalid signature is never resent, and a
// reused business id never carries one (deviation D-1).
func (g *gen) retry(v *view) *step {
	var candidates []*step
	for _, s := range g.steps {
		if !s.invalidSig && s.businessID() != "" {
			candidates = append(candidates, s)
		}
	}
	if len(candidates) == 0 {
		return g.offer(v, "")
	}
	prior := choose(g.c, candidates)
	if pick(g.c, 60, 40) == 0 {
		again := *prior
		again.tag = "retry"
		return &again
	}
	business := prior.businessID()
	switch pick(g.c, 40, 30, 15, 15) {
	case 0:
		return g.offer(v, business)
	case 1:
		return g.accept(v, business, true)
	case 2:
		return g.renew(v, business)
	default:
		return g.end(v, business)
	}
}

func (g *gen) update(v *view) *step {
	providerID := g.pickProvider(v, 3, false)
	signer := g.providerAccountOf(v, providerID)
	if pick(g.c, 90, 10) == 1 {
		signer = broadcasterAcct
	}
	var reserved uint64
	if p, ok := v.providers[providerID]; ok {
		reserved = p.GetReservedBytes()
	}
	const safeMax = 1<<53 - 1
	var capacity uint64
	switch {
	case reserved > safeMax:
		capacity = choose(g.c, []uint64{3000, safeMax})
	default:
		options := []uint64{reserved + 500, reserved + 3000, 1_000_000}
		if reserved >= 2 {
			options = append(options, reserved-1, reserved)
		}
		capacity = max(1, min(choose(g.c, options), safeMax))
	}
	endpoint := endpointFor(providerID)
	if g.c.intn(2) == 1 {
		endpoint = sha("endpoint:" + providerID + ":v2")
	}
	return buildUpdateProvider(signer, g.fresh("b"), providerID, capacity, endpoint)
}

func (g *gen) withdraw(v *view) *step {
	contentID := g.pickContent(v, 5, false)
	version := uint64(1)
	if pick(g.c, 92, 8) == 1 {
		version = 2
	}
	switch pick(g.c, 55, 10, 35) {
	case 0:
		return buildWithdraw(serviceAcct, g.fresh("b"), contentID, version)
	case 1:
		return buildWithdraw(service2Acct, g.fresh("b"), contentID, version)
	default:
		return forceWithdrawStep(contentID, version)
	}
}

func (g *gen) drainOrDisable(v *view) *step {
	providerID := g.pickProvider(v, 3, false)
	if g.c.intn(2) == 0 {
		return disableStep(providerID)
	}
	signer := g.providerAccountOf(v, providerID)
	if pick(g.c, 90, 10) == 1 {
		signer = broadcasterAcct
	}
	generation := uint64(1)
	if p, ok := v.providers[providerID]; ok {
		generation = p.GetKeyGeneration()
	}
	return buildDrain(signer, g.fresh("b"), providerID, g.currentKey(v, providerID), generation)
}

func (g *gen) rotate(v *view) *step {
	providerID := g.pickProvider(v, 3, false)
	current := g.currentKey(v, providerID)
	generation := uint64(1)
	if p, ok := v.providers[providerID]; ok {
		generation = p.GetKeyGeneration()
	}
	expected := generation
	if pick(g.c, 85, 15) == 1 {
		expected = generation + 1
	}
	newKeyID := g.fresh(providerID + "-k")
	newPub := pubFor(newKeyID)
	if pick(g.c, 95, 5) == 1 {
		newPub = pubFor(current)
	}
	return buildRotate(g.providerAccountOf(v, providerID), g.fresh("b"), providerID, current, newKeyID, newPub, expected)
}

func (g *gen) registerProvider() *step {
	acct := providerAccount(g.spareAccount)
	g.spareAccount++
	if g.spareAccount == providerAccounts {
		g.spareAccount = 3
	}
	id := g.fresh("p")
	return buildRegisterProvider(acct, g.fresh("b"), id, id+"-k1", choose(g.c, []uint64{3000, 10_000, 1_000_000}))
}

func (g *gen) admit(v *view) *step {
	var applied []string
	for _, id := range v.providerIDs {
		if v.providers[id].GetState() == providersv1.ProviderState_PROVIDER_STATE_APPLIED {
			applied = append(applied, id)
		}
	}
	if len(applied) > 0 && pick(g.c, 80, 20) == 0 {
		return admitStep(choose(g.c, applied))
	}
	return admitStep(g.pickProvider(v, 10, false))
}
