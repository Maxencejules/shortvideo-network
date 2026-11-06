package leasestest_test

import (
	"bytes"
	"crypto/sha256"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases/leasestest"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Minimal unit-test fakes: in-memory provider and content records and a
// read surface that answers nothing. The self-tests build a store with
// InitGenesis and then break one invariant at a time.

type registries struct {
	providers map[string]*providersv1.Provider
	manifests map[string][]byte
}

func (r *registries) Role(sdk.Context, []byte) typesv1.SubmitterRole { return 0 }
func (r *registries) GovernedLimit(sdk.Context, string) (uint64, bool) {
	return 0, false
}
func (r *registries) DevnetNumeric(sdk.Context, string) (uint64, bool) { return 0, false }
func (r *registries) PolicyVersion(sdk.Context) uint64                 { return 1 }

func (r *registries) Provider(_ sdk.Context, id string) (*providersv1.Provider, bool) {
	p, ok := r.providers[id]
	if !ok {
		return nil, false
	}
	return proto.Clone(p).(*providersv1.Provider), true
}

func (r *registries) CheckNewObligation(ctx sdk.Context, id string) (*providersv1.Provider, msgkit.Code) {
	p, ok := r.Provider(ctx, id)
	if !ok {
		return nil, msgkit.NotFound
	}
	return p, msgkit.OK
}

func (r *registries) VerifyProviderSignature(sdk.Context, string, typesv1.SignatureDomain, string, *typesv1.InnerSignature) msgkit.Code {
	return msgkit.Unauthorized
}
func (r *registries) ReserveProbe(sdk.Context, string, uint64) msgkit.Code { return msgkit.Capacity }
func (r *registries) Reserve(sdk.Context, string, uint64) msgkit.Code      { return msgkit.Capacity }
func (r *registries) Release(sdk.Context, string, uint64) msgkit.Code      { return msgkit.Underflow }

func (r *registries) CheckOfferable(_ sdk.Context, id string, version uint64, manifest []byte, _ uint64) msgkit.Code {
	want, ok := r.manifests[id]
	if !ok {
		return msgkit.NotFound
	}
	if version != 1 {
		return msgkit.Conflict
	}
	if !bytes.Equal(want, manifest) {
		return msgkit.Integrity
	}
	return msgkit.OK
}

func (r *registries) CheckPackage(_ sdk.Context, id string, version uint64, manifest []byte, _ uint64) msgkit.Code {
	want, ok := r.manifests[id]
	if !ok || version != 1 {
		return msgkit.NotFound
	}
	if !bytes.Equal(want, manifest) {
		return msgkit.Integrity
	}
	return msgkit.OK
}

func (r *registries) CheckNotWithdrawn(_ sdk.Context, id string) msgkit.Code {
	if _, ok := r.manifests[id]; !ok {
		return msgkit.NotFound
	}
	return msgkit.OK
}

func digest(label string) *typesv1.Digest {
	sum := sha256.Sum256([]byte(label))
	return &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: sum[:]}
}

const t0 = msgkittest.BlockUnixSeconds

func offer(id, content, provider string) *leasesv1.Offer {
	return &leasesv1.Offer{
		OfferId: id, ContentId: content, ContentVersion: 1, ProviderId: provider, ManifestDigest: digest("m:" + content),
		PackageBytes: 1000, TermsDigest: digest("t:" + id), StartUnixSeconds: t0 + 100, DeadlineUnixSeconds: t0 + 3700,
		ExpiryUnixSeconds: t0 + 600, TermVersion: 1, Scheduler: append([]byte(nil), scheduler...),
	}
}

func lease(id, content, provider string, state leasesv1.LeaseState) *leasesv1.Lease {
	o := offer("o-"+id, content, provider)
	tv := uint64(1)
	if state == leasesv1.LeaseState_LEASE_STATE_RENEWED {
		tv = 2
	}
	return &leasesv1.Lease{
		LeaseId: id, OfferId: o.GetOfferId(), ContentId: content, ContentVersion: 1, ProviderId: provider,
		ManifestDigest: o.GetManifestDigest(), PackageBytes: o.GetPackageBytes(), TermsDigest: o.GetTermsDigest(),
		StartUnixSeconds: o.GetStartUnixSeconds(), DeadlineUnixSeconds: o.GetDeadlineUnixSeconds(), State: state,
		TermVersion: tv, PolicyVersion: 1,
		ReservationHeld: state == leasesv1.LeaseState_LEASE_STATE_ACTIVE || state == leasesv1.LeaseState_LEASE_STATE_RENEWED,
	}
}

// scheduler is the scheduler of every fixture offer.
var scheduler = bytes.Repeat([]byte{3}, 20)

type fixture struct {
	ctx   sdk.Context
	keys  map[string]*storetypes.KVStoreKey
	k     leases.Keeper
	reg   *registries
	store storetypes.KVStore
}

// newFixture imports open offers o1 (c1 -> p1) and o2 (c2 -> p2) and leases
// l1 (c3 -> p1 ACTIVE), l2 (c1 -> p2 RENEWED), l3 (c2 -> p1 ENDED).
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, leases.ModuleName, "other")
	reg := &registries{providers: map[string]*providersv1.Provider{}, manifests: map[string][]byte{}}
	for _, id := range []string{"p1", "p2"} {
		reg.providers[id] = &providersv1.Provider{ProviderId: id, AdmittedCapacityBytes: 1 << 20, ReservedBytes: 1000}
	}
	for _, id := range []string{"c1", "c2", "c3"} {
		reg.manifests[id] = digest("m:" + id).GetValue()
	}
	k := leases.NewKeeper(keys[leases.ModuleName], leases.Deps{
		Roles: reg, Limits: reg, Params: reg, Policy: reg, Providers: reg, Content: reg,
	})
	state := &leasesv1.GenesisState{
		Offers: []*leasesv1.Offer{offer("o1", "c1", "p1"), offer("o2", "c2", "p2")},
		Leases: []*leasesv1.Lease{
			lease("l1", "c3", "p1", leasesv1.LeaseState_LEASE_STATE_ACTIVE),
			lease("l2", "c1", "p2", leasesv1.LeaseState_LEASE_STATE_RENEWED),
			lease("l3", "c2", "p1", leasesv1.LeaseState_LEASE_STATE_ENDED),
		},
	}
	if err := k.InitGenesis(ctx, state); err != nil {
		t.Fatal(err)
	}
	return &fixture{ctx: ctx, keys: keys, k: k, reg: reg, store: ctx.MultiStore().GetKVStore(keys[leases.ModuleName])}
}

func (f *fixture) setLease(t *testing.T, l *leasesv1.Lease) {
	t.Helper()
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(l)
	if err != nil {
		t.Fatal(err)
	}
	f.store.Set(leases.LeaseKey(l.GetLeaseId()), raw)
}

// TestCheckInvariantsDetectsEachViolation: a store built by InitGenesis
// passes; each injected violation fails with the invariant it breaks.
func TestCheckInvariantsDetectsEachViolation(t *testing.T) {
	base := newFixture(t)
	if err := leasestest.CheckInvariants(base.ctx, base.k, base.reg, nil); err != nil {
		t.Fatalf("clean store: %v", err)
	}
	if err := leasestest.CheckOfferIntegrity(base.ctx, base.k, base.reg); err != nil {
		t.Fatalf("clean store: %v", err)
	}
	// A baseline explains reserved bytes no lease holds.
	base.reg.providers["p1"].ReservedBytes += 5
	if err := leasestest.CheckInvariants(base.ctx, base.k, base.reg, map[string]uint64{"p1": 5}); err != nil {
		t.Fatalf("baseline: %v", err)
	}

	cases := []struct {
		rule    string
		corrupt func(t *testing.T, f *fixture)
	}{
		{"I1:", func(_ *testing.T, f *fixture) { f.reg.providers["p1"].ReservedBytes = 999 }},
		{"I1:", func(_ *testing.T, f *fixture) { delete(f.reg.providers, "p2") }},
		{"I2:", func(_ *testing.T, f *fixture) { f.reg.providers["p1"].AdmittedCapacityBytes = 999 }},
		{"I3:", func(t *testing.T, f *fixture) {
			l := lease("l1", "c3", "p1", leasesv1.LeaseState_LEASE_STATE_ACTIVE)
			l.ReservationHeld = false
			f.setLease(t, l)
		}},
		{"I3:", func(t *testing.T, f *fixture) {
			l := lease("l3", "c2", "p1", leasesv1.LeaseState_LEASE_STATE_ENDED)
			l.ReservationHeld = true
			f.setLease(t, l)
		}},
		{"I3:", func(t *testing.T, f *fixture) {
			f.setLease(t, lease("l3", "c2", "p1", leasesv1.LeaseState_LEASE_STATE_OFFERED))
		}},
		{"I4:", func(_ *testing.T, f *fixture) { f.store.Delete(leases.PairKey("p1", "c3", 1)) }},
		{"I4:", func(_ *testing.T, f *fixture) { f.store.Set(leases.PairKey("p1", "c2", 1), []byte("l3")) }},
		{"I4:", func(_ *testing.T, f *fixture) { f.store.Set(leases.PairKey("p1", "c3", 1), []byte("l9")) }},
		{"I4:", func(t *testing.T, f *fixture) {
			// Two held leases of one pair.
			l := lease("l4", "c3", "p1", leasesv1.LeaseState_LEASE_STATE_ACTIVE)
			f.setLease(t, l)
			f.store.Set(leases.UsedOfferKey("o-l4"), leases.Marker)
		}},
		{"I5:", func(_ *testing.T, f *fixture) { f.store.Delete(leases.LeaseDeadlineKey(t0+3700, "l1")) }},
		{"I5:", func(_ *testing.T, f *fixture) { f.store.Set(leases.LeaseDeadlineKey(t0+3701, "l1"), leases.Marker) }},
		{"I5:", func(_ *testing.T, f *fixture) { f.store.Delete(leases.ContentLeaseKey("c1", "l2")) }},
		{"I5:", func(_ *testing.T, f *fixture) { f.store.Set(leases.ProviderLeaseKey("p1", "l3"), leases.Marker) }},
		{"I6:", func(_ *testing.T, f *fixture) { f.store.Delete(leases.OfferExpiryKey(t0+600, "o1")) }},
		{"I6:", func(_ *testing.T, f *fixture) { f.store.Set(leases.ContentOfferKey("c3", "o1"), leases.Marker) }},
		{"I6:", func(_ *testing.T, f *fixture) { f.store.Delete(leases.ProviderOfferCountKey("p1")) }},
		{"I6:", func(_ *testing.T, f *fixture) {
			f.store.Set(leases.ContentOfferCountKey("c1"), []byte{0, 0, 0, 0, 0, 0, 0, 2})
		}},
		{"I6:", func(_ *testing.T, f *fixture) { f.store.Set(leases.OfferKey("o1"), []byte{0xff}) }},
		{"I6:", func(_ *testing.T, f *fixture) { f.store.Delete(leases.SchedulerOfferCountKey("c1", scheduler)) }},
		{"I6:", func(_ *testing.T, f *fixture) {
			f.store.Set(leases.SchedulerOfferCountKey("c2", scheduler), []byte{0, 0, 0, 0, 0, 0, 0, 2})
		}},
		{"I6:", func(_ *testing.T, f *fixture) {
			f.store.Set(leases.SchedulerOfferCountKey("c1", bytes.Repeat([]byte{4}, 20)), []byte{0, 0, 0, 0, 0, 0, 0, 1})
		}},
		{"I7:", func(t *testing.T, f *fixture) {
			l := lease("l1", "c3", "p1", leasesv1.LeaseState_LEASE_STATE_ACTIVE)
			l.TermVersion = 2
			f.setLease(t, l)
		}},
		{"I7:", func(t *testing.T, f *fixture) {
			l := lease("l2", "c1", "p2", leasesv1.LeaseState_LEASE_STATE_RENEWED)
			l.TermVersion = 1
			f.setLease(t, l)
		}},
		{"I7:", func(_ *testing.T, f *fixture) { f.store.Set(leases.LeaseKey("l1"), []byte{0xff, 0xff}) }},
		{"I10:", func(_ *testing.T, f *fixture) { f.store.Delete(leases.UsedOfferKey("o1")) }},
		{"I10:", func(_ *testing.T, f *fixture) { f.store.Delete(leases.UsedOfferKey("o-l3")) }},
		{"I10:", func(t *testing.T, f *fixture) {
			l := lease("l3", "c2", "p1", leasesv1.LeaseState_LEASE_STATE_ENDED)
			l.OfferId = "o-l1"
			f.setLease(t, l)
		}},
		{"I10:", func(t *testing.T, f *fixture) {
			l := lease("l3", "c2", "p1", leasesv1.LeaseState_LEASE_STATE_ENDED)
			l.OfferId = "o2"
			f.setLease(t, l)
		}},
		{"I10:", func(_ *testing.T, f *fixture) { f.store.Set(leases.UsedOfferKey("O"), leases.Marker) }},
		// An open offer's marker must hold its scheduler.
		{"I10:", func(_ *testing.T, f *fixture) { f.store.Set(leases.UsedOfferKey("o1"), leases.Marker) }},
		{"I10:", func(_ *testing.T, f *fixture) {
			f.store.Set(leases.UsedOfferKey("o2"), bytes.Repeat([]byte{4}, 20))
		}},
		// Any marker holds a 20-byte scheduler or Marker.
		{"I10:", func(_ *testing.T, f *fixture) { f.store.Set(leases.UsedOfferKey("o-l3"), []byte{1, 2}) }},
		{"I12:", func(_ *testing.T, f *fixture) {
			f.store.Set(append([]byte{leases.BusinessPrefix}, "b1"...), []byte{1})
		}},
		{"layout:", func(_ *testing.T, f *fixture) { f.store.Delete(msgkit.LayoutVersionKey) }},
		{"layout:", func(_ *testing.T, f *fixture) { f.store.Set(msgkit.LayoutVersionKey, []byte{0, 0, 0, 0, 0, 0, 0, 1}) }},
		{"layout:", func(_ *testing.T, f *fixture) { f.store.Set([]byte{0x0e, 'x'}, []byte{1}) }},
	}
	for _, tc := range cases {
		f := newFixture(t)
		tc.corrupt(t, f)
		err := leasestest.CheckInvariants(f.ctx, f.k, f.reg, nil)
		if err == nil || !strings.HasPrefix(err.Error(), tc.rule) {
			t.Fatalf("%s violation: %v", tc.rule, err)
		}
	}

	// I9: an open offer whose digest no longer matches the content record.
	f := newFixture(t)
	f.reg.manifests["c1"] = digest("other").GetValue()
	if err := leasestest.CheckOfferIntegrity(f.ctx, f.k, f.reg); err == nil || !strings.HasPrefix(err.Error(), "I9:") {
		t.Fatalf("I9 violation: %v", err)
	}
	delete(f.reg.manifests, "c1")
	if err := leasestest.CheckOfferIntegrity(f.ctx, f.k, f.reg); err == nil || !strings.HasPrefix(err.Error(), "I9:") {
		t.Fatalf("I9 absent content: %v", err)
	}
	// I9 for leases: l1 is the only record of c3; its committed package no
	// longer matches the content record.
	f = newFixture(t)
	f.reg.manifests["c3"] = digest("other").GetValue()
	if err := leasestest.CheckOfferIntegrity(f.ctx, f.k, f.reg); err == nil || !strings.Contains(err.Error(), `I9: lease "l1"`) {
		t.Fatalf("I9 lease violation: %v", err)
	}
}

// TestStoreSnapshot: equal stores give equal bytes; one changed row, a
// cached branch with a write, and another store name change them; a
// cached branch without writes equals its parent.
func TestStoreSnapshot(t *testing.T) {
	a, b := newFixture(t), newFixture(t)
	snap := func(f *fixture, ctx sdk.Context) []byte {
		return leasestest.StoreSnapshot(ctx, f.keys[leases.ModuleName], f.keys["other"])
	}
	if !bytes.Equal(snap(a, a.ctx), snap(b, b.ctx)) {
		t.Fatal("equal stores differ")
	}
	cached, _ := a.ctx.CacheContext()
	if !bytes.Equal(snap(a, a.ctx), snap(a, cached)) {
		t.Fatal("an untouched branch differs from its parent")
	}
	cached.KVStore(a.keys["other"]).Set([]byte("k"), []byte("v"))
	if bytes.Equal(snap(a, a.ctx), snap(a, cached)) {
		t.Fatal("a branch write is invisible")
	}
	if !bytes.Equal(snap(a, a.ctx), snap(b, b.ctx)) {
		t.Fatal("a branch write reached the parent")
	}
	b.store.Set([]byte{leases.UsedOfferPrefix, 'z'}, leases.Marker)
	if bytes.Equal(snap(a, a.ctx), snap(b, b.ctx)) {
		t.Fatal("a changed row is invisible")
	}
	one := leasestest.StoreSnapshot(a.ctx, a.keys[leases.ModuleName])
	both := leasestest.StoreSnapshot(a.ctx, a.keys[leases.ModuleName], a.keys["other"])
	if bytes.Equal(one, both) || !bytes.HasPrefix(both, one) {
		t.Fatal("snapshots are not per-store, length-prefixed, in argument order")
	}
	// Reading does not consume gas from the context's meter.
	used := a.ctx.GasMeter().GasConsumed()
	_ = snap(a, a.ctx)
	if err := leasestest.CheckInvariants(a.ctx, a.k, a.reg, nil); err != nil {
		t.Fatal(err)
	}
	if a.ctx.GasMeter().GasConsumed() != used {
		t.Fatal("snapshot or invariant check consumed gas")
	}
}
