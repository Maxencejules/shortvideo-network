package difftest

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Scale tests: real keepers over IAVL stores, keeper level. Store gas is
// the gas the SDK's store gas meter charges (storetypes.KVGasConfig); the
// numbers are logged for the evidence phase to capture, not quoted here.
const (
	scaleProviders     = 8
	scaleOpenOffers    = 256
	scalePackageBytes  = 1000
	scaleCapacity      = 1_000_000_000
	expiryBudgetCeil   = 128 // bounds.json safety_max_expiry_steps_per_block
	withdrawalGasLimit = 10_000_000
)

// scaleChain is a harness with eight ADMITTED providers and no per-step
// trial snapshot or invariant check (both are O(state) per step; the
// tests check invariants at their own checkpoints).
type scaleChain struct {
	t *testing.T
	h *harness
	n int
}

func newScaleChain(t *testing.T) *scaleChain {
	t.Helper()
	c := &scaleChain{t: t, h: newHarness(t, &config{term: uint64(termSeconds), governed: map[string]uint64{}})}
	for i := 0; i < scaleProviders; i++ {
		id := fmt.Sprintf("p%d", i)
		c.ok(buildRegisterProvider(providerAccount(i), "reg-"+id, id, id+"-k1", scaleCapacity))
		c.ok(admitStep(id))
	}
	return c
}

func (c *scaleChain) business() string {
	c.n++
	return fmt.Sprintf("bs%06d", c.n)
}

func (c *scaleChain) ok(s *step) outcome {
	c.t.Helper()
	out := c.h.apply(s)
	if out.code != msgkit.OK {
		c.t.Fatalf("%s: %s", s.op, out.code)
	}
	return out
}

// offer submits a valid 1000-byte offer by serviceAcct and returns the
// stored record.
func (c *scaleChain) offer(offerID, contentID, providerID string, deadline, expiry int64) (*leasesv1.Offer, outcome) {
	c.t.Helper()
	return c.offerBy(serviceAcct, offerID, contentID, providerID, deadline, expiry)
}

// offerBy is offer with another scheduler.
func (c *scaleChain) offerBy(scheduler []byte, offerID, contentID, providerID string, deadline, expiry int64) (*leasesv1.Offer, outcome) {
	c.t.Helper()
	p := offerParams{
		signer: scheduler, business: c.business(), offerID: offerID, contentID: contentID, providerID: providerID,
		version: 1, size: scalePackageBytes, termVersion: 1, manifest: manifestFor(contentID), terms: termsFor(1),
		start: deadline - termSeconds, deadline: deadline, expiry: expiry,
	}
	out := c.h.apply(buildOffer(p, ""))
	return &leasesv1.Offer{
		OfferId: offerID, ContentId: contentID, ContentVersion: 1, ProviderId: providerID,
		ManifestDigest: digest(p.manifest), PackageBytes: p.size, TermsDigest: digest(p.terms),
		StartUnixSeconds: p.start, DeadlineUnixSeconds: deadline, ExpiryUnixSeconds: expiry,
		TermVersion: 1, Scheduler: append([]byte(nil), scheduler...),
	}, out
}

func (c *scaleChain) accept(offer *leasesv1.Offer, leaseID string) {
	c.t.Helper()
	keyID := offer.GetProviderId() + "-k1"
	s := providerSig(sigValid, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT,
		typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, acceptSignBytes(offer, leaseID), nil, keyID, "", "")
	c.ok(buildAccept(broadcasterAcct, c.business(), offer.GetOfferId(), leaseID, s))
}

func (c *scaleChain) reservedTotal() uint64 {
	var total uint64
	ctx := c.h.unmetered()
	for i := 0; i < scaleProviders; i++ {
		p, found := c.h.prov.Provider(ctx, fmt.Sprintf("p%d", i))
		if !found {
			c.t.Fatalf("provider p%d missing", i)
		}
		total += p.GetReservedBytes()
	}
	return total
}

// expiryRun is the per-block record of one drained catalogue.
type expiryRun struct {
	counts []int
	gas    []uint64
	// leaseOnly is true for a block that expired only leases.
	leaseOnly []bool
}

// drainCatalogue builds leaseCount held leases and scaleOpenOffers open
// offers, all due at the same header time due, across eight providers, and
// then advances the chain one block at a time. It asserts the work of every
// block and returns the per-block counts and store gas.
func drainCatalogue(t *testing.T, leaseCount int) expiryRun {
	c := newScaleChain(t)
	now := c.h.now
	due := now + 1000
	contents := leaseCount / scaleProviders
	for i := 0; i < contents; i++ {
		id := fmt.Sprintf("c%05d", i)
		c.ok(buildRegisterContent(serviceAcct, c.business(), id, scalePackageBytes))
	}
	for i := 0; i < scaleOpenOffers/scaleProviders; i++ {
		id := fmt.Sprintf("x%03d", i)
		c.ok(buildRegisterContent(serviceAcct, c.business(), id, scalePackageBytes))
	}
	var want []string
	// Lease k: provider k%8, content k/8, so each (provider, content) pair
	// holds one lease. Every deadline is due.
	for k := 0; k < leaseCount; k++ {
		offer, out := c.offer(fmt.Sprintf("o%05d", k), fmt.Sprintf("c%05d", k/scaleProviders),
			fmt.Sprintf("p%d", k%scaleProviders), due, now+600)
		if out.code != msgkit.OK {
			t.Fatalf("offer %d: %s", k, out.code)
		}
		leaseID := fmt.Sprintf("l%05d", k)
		c.accept(offer, leaseID)
		want = append(want, "lease:"+leaseID)
	}
	// Open offers that expire at the same time, on contents without leases.
	for k := 0; k < scaleOpenOffers; k++ {
		offerID := fmt.Sprintf("q%05d", k)
		if _, out := c.offer(offerID, fmt.Sprintf("x%03d", k/scaleProviders), fmt.Sprintf("p%d", k%scaleProviders),
			due+termSeconds, due); out.code != msgkit.OK {
			t.Fatalf("open offer %d: %s", k, out.code)
		}
		want = append(want, "offer:"+offerID)
	}
	c.h.checkInvariants("catalogue setup")
	if got := c.reservedTotal(); got != uint64(leaseCount)*scalePackageBytes {
		t.Fatalf("reserved %d after setup, want %d", got, leaseCount*scalePackageBytes)
	}

	// effective-1: the block before the due time expires nothing.
	before := c.h.apply(blockStep(due - 1))
	if len(before.released) != 0 || c.reservedTotal() != uint64(leaseCount)*scalePackageBytes {
		t.Fatalf("block at due-1 released %d items", len(before.released))
	}
	if c.h.view().heldDue() != 0 {
		t.Fatalf("items due at due-1")
	}

	// effective and after: each block processes min(budget, remaining) in
	// (time, kind, id) order: every lease (kind "lease") before every offer.
	run := expiryRun{}
	pos := 0
	for block := int64(0); pos < len(want); block++ {
		reserved := c.reservedTotal()
		out := c.h.apply(blockStep(due + block))
		expect := want[pos:min(pos+expiryBudgetCeil, len(want))]
		if !slices.Equal(out.released, expect) {
			t.Fatalf("block %d at due+%d processed %d items %v..., want %d items %v...",
				len(run.counts), block, len(out.released), head(out.released), len(expect), head(expect))
		}
		leasesReleased := 0
		for _, item := range out.released {
			if strings.HasPrefix(item, "lease:") {
				leasesReleased++
			}
		}
		if dropped := reserved - c.reservedTotal(); dropped != uint64(leasesReleased)*scalePackageBytes {
			t.Fatalf("block %d released %d reserved bytes for %d leases", len(run.counts), dropped, leasesReleased)
		}
		c.h.checkInvariants(fmt.Sprintf("expiry block %d", len(run.counts)))
		run.counts = append(run.counts, len(out.released))
		run.gas = append(run.gas, out.gas)
		run.leaseOnly = append(run.leaseOnly, leasesReleased == len(out.released))
		pos += len(out.released)
	}
	if blocks, wantBlocks := len(run.counts), (len(want)+expiryBudgetCeil-1)/expiryBudgetCeil; blocks != wantBlocks {
		t.Fatalf("drained in %d blocks, want ceil(%d/%d) = %d", blocks, len(want), expiryBudgetCeil, wantBlocks)
	}
	if after := c.h.apply(blockStep(c.h.now + 1)); len(after.released) != 0 || c.reservedTotal() != 0 {
		t.Fatalf("a block after the drain released %d items; reserved %d", len(after.released), c.reservedTotal())
	}
	v := c.h.view()
	for _, id := range v.leaseIDs {
		if v.leases[id].GetState() != leasesv1.LeaseState_LEASE_STATE_EXPIRED {
			t.Fatalf("lease %s is %s after the drain", id, v.leases[id].GetState())
		}
	}
	if len(v.offerIDs) != 0 {
		t.Fatalf("%d offers open after the drain", len(v.offerIDs))
	}
	return run
}

func head(items []string) []string { return items[:min(3, len(items))] }

// TestExpiryWorkIndependentOfBacklog drains two catalogues, 1,024 and
// 4,096 held leases plus 256 open offers, all due at one header time T.
// At T-1 nothing expires (effective-1); from T on every block expires
// exactly min(128, remaining) items in (time, kind, id) order, reserved
// bytes drop by exactly the expired bytes, and the drain takes
// ceil(N/128) blocks. The store gas of the first full block (the same 128
// leases in both catalogues) is equal: per-block work does not depend on
// the backlog. Chain time comes only from the block header the harness
// sets.
func TestExpiryWorkIndependentOfBacklog(t *testing.T) {
	small := drainCatalogue(t, 1024)
	large := drainCatalogue(t, 4096)
	for _, r := range []struct {
		name string
		run  expiryRun
	}{{"1,024 leases + 256 offers", small}, {"4,096 leases + 256 offers", large}} {
		t.Logf("%s: %d blocks; items per block %v", r.name, len(r.run.counts), r.run.counts)
		t.Logf("%s: store gas per block %v", r.name, r.run.gas)
	}
	if small.gas[0] != large.gas[0] {
		t.Fatalf("first full block: store gas %d at backlog 1,280, %d at backlog 4,352", small.gas[0], large.gas[0])
	}
	t.Logf("first full block (128 lease expiries): store gas %d at backlog 1,280 and at backlog 4,352", small.gas[0])
	// Every full block of lease expiries costs within 1% of the first: the
	// only difference is the length of the reserved-bytes varint.
	for _, r := range []expiryRun{small, large} {
		for i, n := range r.counts {
			if n != expiryBudgetCeil || !r.leaseOnly[i] {
				continue
			}
			if diff := absDiff(r.gas[i], small.gas[0]); diff*100 > small.gas[0] {
				t.Errorf("full lease block %d: store gas %d differs from %d by more than 1%%", i, r.gas[i], small.gas[0])
			}
		}
	}
}

func absDiff(a, b uint64) uint64 {
	if a > b {
		return a - b
	}
	return b - a
}

// TestWithdrawalAtContentCapGas fills two contents with
// MaxOpenOffersPerContent (256) open offers across eight providers, from
// two schedulers (one alone stops at 128, D-10); the 257th offer, from a
// third scheduler with no offer of the content (so not D-10), is BOUNDS
// (deviation D-6). WithdrawContent (message path) and
// ForceWithdrawByCouncil (council path) then void all 256 offers each,
// with store gas below withdrawalGasLimit (half the 20,000,000 transaction
// gas limit).
func TestWithdrawalAtContentCapGas(t *testing.T) {
	c := newScaleChain(t)
	now := c.h.now
	for _, contentID := range []string{"wm", "wc"} {
		c.ok(buildRegisterContent(serviceAcct, c.business(), contentID, scalePackageBytes))
		for k := 0; k < leases.MaxOpenOffersPerContent; k++ {
			scheduler := [][]byte{serviceAcct, service2Acct}[k%2]
			if _, out := c.offerBy(scheduler, fmt.Sprintf("%s-%03d", contentID, k), contentID, fmt.Sprintf("p%d", k%scaleProviders),
				now+termSeconds, now+600); out.code != msgkit.OK {
				t.Fatalf("offer %d of %s: %s", k, contentID, out.code)
			}
		}
		if _, out := c.offerBy(service3Acct, contentID+"-extra", contentID, "p0", now+termSeconds, now+600); out.code != msgkit.Bounds {
			t.Fatalf("offer %d of %s: %s, want %s (D-6)", leases.MaxOpenOffersPerContent+1, contentID, out.code, msgkit.Bounds)
		}
	}
	c.h.checkInvariants("256 offers per content")

	message := c.ok(buildWithdraw(serviceAcct, c.business(), "wm", 1))
	council := c.ok(forceWithdrawStep("wc", 1))
	for _, r := range []struct {
		name string
		out  outcome
	}{{"WithdrawContent (message path)", message}, {"ForceWithdrawByCouncil (council path)", council}} {
		if len(r.out.closed) != leases.MaxOpenOffersPerContent {
			t.Fatalf("%s voided %d offers, want %d", r.name, len(r.out.closed), leases.MaxOpenOffersPerContent)
		}
		if r.out.gas >= withdrawalGasLimit {
			t.Fatalf("%s used %d gas, ceiling %d", r.name, r.out.gas, withdrawalGasLimit)
		}
		t.Logf("%s at %d open offers: gas %d (ceiling %d)", r.name, leases.MaxOpenOffersPerContent, r.out.gas, withdrawalGasLimit)
	}
	c.h.checkInvariants("after both withdrawals")
	if v := c.h.view(); len(v.offerIDs) != 0 || len(v.used) != 2*leases.MaxOpenOffersPerContent {
		t.Fatalf("after withdrawal: %d open offers, %d used offer ids", len(v.offerIDs), len(v.used))
	}
}
