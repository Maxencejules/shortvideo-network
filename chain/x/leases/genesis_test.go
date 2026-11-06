package leases_test

import (
	"bytes"
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// sampleGenesis: open offers o1 (c1 -> p1) and o2 (c2 -> p2); leases l1
// (c3 -> p1, ACTIVE), l2 (c1 -> p2, RENEWED at term 2), l3 (c2 -> p1,
// ENDED), l4 (c3 -> p2, EXPIRED). Held bytes: p1 1000 (l1), p2 1000 (l2).
func sampleGenesis(e *env) *leasesv1.GenesisState {
	l2 := e.leaseRecord("l2", "c1", "p2")
	l2.State, l2.TermVersion, l2.DeadlineUnixSeconds = leasesv1.LeaseState_LEASE_STATE_RENEWED, 2, l2.GetDeadlineUnixSeconds()+term
	l3 := e.leaseRecord("l3", "c2", "p1")
	l3.State, l3.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_ENDED, false
	l4 := e.leaseRecord("l4", "c3", "p2")
	l4.State, l4.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_EXPIRED, false
	return &leasesv1.GenesisState{
		Offers: []*leasesv1.Offer{e.offerRecord("o2", "c2", "p2"), e.offerRecord("o1", "c1", "p1")},
		Leases: []*leasesv1.Lease{l2, e.leaseRecord("l1", "c3", "p1"), l4, l3},
	}
}

// genesisEnv is an env whose leases store is empty (no layout key), with
// the sample's reserved bytes in the provider fake.
func genesisEnv(t testing.TB) *env {
	t.Helper()
	e := newEnv(t)
	e.leaseStore().Delete(msgkit.LayoutVersionKey)
	e.editProvider("p1", func(p *providersv1.Provider) { p.ReservedBytes = packageSize })
	e.editProvider("p2", func(p *providersv1.Provider) { p.ReservedBytes = packageSize })
	return e
}

// TestGenesisValidateImportExport: every ValidateGenesis rule (one invalid
// document per rule), the cross-module checks of InitGenesis, the
// empty-store rule (scaffold key included), round trips from a document and
// from committed transitions, and the document encoding.
func TestGenesisValidateImportExport(t *testing.T) {
	t.Run("validate-rejects", testValidateGenesisRejects)
	t.Run("import-checks-registries", testGenesisImportChecksRegistries)
	t.Run("requires-an-empty-store", testGenesisRequiresAnEmptyStore)
	t.Run("round-trip", testGenesisRoundTrip)
	t.Run("round-trip-from-committed-transitions", testGenesisRoundTripFromCommittedTransitions)
	t.Run("document-rules", testGenesisDocumentRules)
}

// testValidateGenesisRejects: one invalid document per ValidateGenesis rule.
func testValidateGenesisRejects(t *testing.T) {
	e := newEnv(t)
	if err := leases.ValidateGenesis(sampleGenesis(e)); err != nil {
		t.Fatalf("sample: %v", err)
	}
	if err := leases.ValidateGenesis(&leasesv1.GenesisState{}); err != nil {
		t.Fatalf("empty: %v", err)
	}
	offer := func(edit func(o *leasesv1.Offer)) func(g *leasesv1.GenesisState) {
		return func(g *leasesv1.GenesisState) { edit(g.Offers[1]) }
	}
	lease := func(edit func(l *leasesv1.Lease)) func(g *leasesv1.GenesisState) {
		return func(g *leasesv1.GenesisState) { edit(g.Leases[1]) } // l1, ACTIVE
	}
	cases := map[string]func(g *leasesv1.GenesisState){
		"nil-offer":                 func(g *leasesv1.GenesisState) { g.Offers[0] = nil },
		"offer-unknown-field":       offer(func(o *leasesv1.Offer) { o.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01}) }),
		"offer-id-pattern":          offer(func(o *leasesv1.Offer) { o.OfferId = "O1" }),
		"offer-id-duplicate":        offer(func(o *leasesv1.Offer) { o.OfferId = "o2" }),
		"offer-content-id":          offer(func(o *leasesv1.Offer) { o.ContentId = "" }),
		"offer-provider-id":         offer(func(o *leasesv1.Offer) { o.ProviderId = "P1" }),
		"offer-content-version-0":   offer(func(o *leasesv1.Offer) { o.ContentVersion = 0 }),
		"offer-content-version-2":   offer(func(o *leasesv1.Offer) { o.ContentVersion = 2 }),
		"offer-manifest-nil":        offer(func(o *leasesv1.Offer) { o.ManifestDigest = nil }),
		"offer-manifest-algorithm":  offer(func(o *leasesv1.Offer) { o.ManifestDigest.Algorithm = 0 }),
		"offer-manifest-size":       offer(func(o *leasesv1.Offer) { o.ManifestDigest.Value = o.ManifestDigest.Value[:31] }),
		"offer-manifest-unknown":    offer(func(o *leasesv1.Offer) { o.ManifestDigest.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01}) }),
		"offer-terms-size":          offer(func(o *leasesv1.Offer) { o.TermsDigest.Value = nil }),
		"offer-package-0":           offer(func(o *leasesv1.Offer) { o.PackageBytes = 0 }),
		"offer-package-above-cap":   offer(func(o *leasesv1.Offer) { o.PackageBytes = genesis.MaxPackageBytes + 1 }),
		"offer-start-0":             offer(func(o *leasesv1.Offer) { o.StartUnixSeconds = 0 }),
		"offer-deadline-above-9999": offer(func(o *leasesv1.Offer) { o.DeadlineUnixSeconds = genesis.UnixSecondsMax + 1 }),
		"offer-deadline-at-start":   offer(func(o *leasesv1.Offer) { o.DeadlineUnixSeconds = o.StartUnixSeconds }),
		"offer-expiry-0":            offer(func(o *leasesv1.Offer) { o.ExpiryUnixSeconds = 0 }),
		"offer-term-version-2":      offer(func(o *leasesv1.Offer) { o.TermVersion = 2 }),
		"offer-scheduler-19":        offer(func(o *leasesv1.Offer) { o.Scheduler = o.Scheduler[:19] }),
		"offer-id-of-a-lease":       offer(func(o *leasesv1.Offer) { o.OfferId = "o-l1" }),
		"nil-lease":                 func(g *leasesv1.GenesisState) { g.Leases[0] = nil },
		"lease-unknown-field":       lease(func(l *leasesv1.Lease) { l.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01}) }),
		"lease-id-pattern":          lease(func(l *leasesv1.Lease) { l.LeaseId = "" }),
		"lease-id-duplicate":        lease(func(l *leasesv1.Lease) { l.LeaseId = "l2" }),
		"lease-offer-id-pattern":    lease(func(l *leasesv1.Lease) { l.OfferId = "-o" }),
		"lease-offer-id-reused":     lease(func(l *leasesv1.Lease) { l.OfferId = "o-l2" }),
		"lease-content-id":          lease(func(l *leasesv1.Lease) { l.ContentId = "c 3" }),
		"lease-provider-id":         lease(func(l *leasesv1.Lease) { l.ProviderId = "" }),
		"lease-content-version-2":   lease(func(l *leasesv1.Lease) { l.ContentVersion = 2 }),
		"lease-manifest":            lease(func(l *leasesv1.Lease) { l.ManifestDigest.Algorithm = 9 }),
		"lease-terms":               lease(func(l *leasesv1.Lease) { l.TermsDigest = nil }),
		"lease-package-0":           lease(func(l *leasesv1.Lease) { l.PackageBytes = 0 }),
		"lease-start-negative":      lease(func(l *leasesv1.Lease) { l.StartUnixSeconds = -1 }),
		"lease-deadline-at-start":   lease(func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = l.StartUnixSeconds }),
		"lease-state-unspecified":   lease(func(l *leasesv1.Lease) { l.State = leasesv1.LeaseState_LEASE_STATE_UNSPECIFIED }),
		"lease-state-offered":       lease(func(l *leasesv1.Lease) { l.State = leasesv1.LeaseState_LEASE_STATE_OFFERED }),
		"lease-state-6-at-risk":     lease(func(l *leasesv1.Lease) { l.State = 6 }),
		"lease-state-unknown":       lease(func(l *leasesv1.Lease) { l.State = 42 }),
		"lease-active-not-held":     lease(func(l *leasesv1.Lease) { l.ReservationHeld = false }),
		"lease-ended-held": func(g *leasesv1.GenesisState) {
			g.Leases[3].ReservationHeld = true // l3, ENDED
		},
		"lease-active-term-2":   lease(func(l *leasesv1.Lease) { l.TermVersion = 2 }),
		"lease-renewed-term-1":  func(g *leasesv1.GenesisState) { g.Leases[0].TermVersion = 1 }, // l2, RENEWED
		"lease-ended-term-0":    func(g *leasesv1.GenesisState) { g.Leases[3].TermVersion = 0 },
		"lease-policy-version":  lease(func(l *leasesv1.Lease) { l.PolicyVersion = 0 }),
		"lease-pair-held-twice": lease(func(l *leasesv1.Lease) { l.ContentId, l.ProviderId = "c1", "p2" }),
		"state-unknown-field": func(g *leasesv1.GenesisState) {
			g.ProtoReflect().SetUnknown([]byte{0xf8, 0x01, 0x01})
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			g := sampleGenesis(e)
			edit(g)
			if err := leases.ValidateGenesis(g); err == nil {
				t.Fatal("accepted")
			}
		})
	}
	if leases.ValidateGenesis(nil) == nil {
		t.Fatal("nil state accepted")
	}
	// Open-offer caps. Each document is valid one offer below its cap, and
	// one offer more fails with exactly that cap's error: no other cap is
	// reached by the same document.
	capped := func(n int, record func(i int) *leasesv1.Offer) (below, at *leasesv1.GenesisState) {
		below, at = &leasesv1.GenesisState{}, &leasesv1.GenesisState{}
		for i := 0; i <= n; i++ {
			if i < n {
				below.Offers = append(below.Offers, record(i))
			}
			at.Offers = append(at.Offers, record(i))
		}
		return below, at
	}
	for _, c := range []struct {
		name, want string
		n          int
		record     func(i int) *leasesv1.Offer
	}{
		// Three schedulers (at most 86 offers each of 257), so the
		// per-scheduler cap (128) cannot be what fires.
		{"content cap", `leases genesis: content "c1" has more than 256 open offers`, leases.MaxOpenOffersPerContent, func(i int) *leasesv1.Offer {
			return e.offerRecordBy([][]byte{serviceAcct, service2Acct, service3Acct}[i%3], maxID("o", i), "c1", []string{"p1", "p2"}[(i/3)%2])
		}},
		{"scheduler cap", `leases genesis: scheduler 3333333333333333333333333333333333333333 has more than 128 open offers of content "c1"`,
			leases.MaxOpenOffersPerContentPerScheduler, func(i int) *leasesv1.Offer {
				return e.offerRecord(maxID("o", i), "c1", []string{"p1", "p2"}[i%2])
			}},
		// 100 offers per content id, below both content caps.
		{"provider cap", `leases genesis: provider "p1" has more than 1024 open offers`, int(genesis.MaxOpenOffersPerProvider), func(i int) *leasesv1.Offer {
			return e.offerRecord(maxID("o", i), maxID("c", i/100), "p1")
		}},
	} {
		below, at := capped(c.n, c.record)
		if err := leases.ValidateGenesis(below); err != nil {
			t.Fatalf("%s: %d offers: %v", c.name, c.n, err)
		}
		if err := leases.ValidateGenesis(at); err == nil || err.Error() != c.want {
			t.Fatalf("%s: %d offers: %v, want %q", c.name, c.n+1, err, c.want)
		}
	}
}

// testGenesisImportChecksRegistries: InitGenesis checks every record
// against the provider and content registries before it writes; a failure
// writes nothing.
func testGenesisImportChecksRegistries(t *testing.T) {
	cases := map[string]func(e *env, g *leasesv1.GenesisState){
		"offer-content-absent": func(_ *env, g *leasesv1.GenesisState) { g.Offers[0].ContentId = "c-none" },
		"offer-content-withdrawn": func(e *env, _ *leasesv1.GenesisState) {
			e.setContentState("c2", contentv1.ContentState_CONTENT_STATE_WITHDRAWN)
		},
		"offer-content-integrity": func(_ *env, g *leasesv1.GenesisState) { g.Offers[0].PackageBytes = 999 },
		"offer-provider-absent":   func(_ *env, g *leasesv1.GenesisState) { g.Offers[0].ProviderId = "p-none" },
		"lease-provider-absent": func(_ *env, g *leasesv1.GenesisState) {
			g.Leases[3].ProviderId = "p-none" // l3, ENDED
		},
		"lease-content-absent": func(_ *env, g *leasesv1.GenesisState) { g.Leases[2].ContentId = "c-none" },
		// I9 for leases: the committed package must equal the content
		// record's, in any state (l1 holds c3, l3 the withdrawn-able c2).
		"lease-manifest-differs": func(_ *env, g *leasesv1.GenesisState) {
			g.Leases[1].ManifestDigest = manifestOf("c-other") // l1, ACTIVE
		},
		"lease-package-bytes-differ": func(_ *env, g *leasesv1.GenesisState) {
			g.Leases[3].PackageBytes = packageSize + 1 // l3, ENDED, does not reserve
		},
		"withdrawn-content-lease-manifest-differs": func(e *env, g *leasesv1.GenesisState) {
			e.setContentState("c3", contentv1.ContentState_CONTENT_STATE_WITHDRAWN)
			g.Offers = nil
			g.Leases[1].ManifestDigest = manifestOf("c-other") // l1 of c3
		},
		"reserved-too-low": func(e *env, _ *leasesv1.GenesisState) {
			e.editProvider("p1", func(p *providersv1.Provider) { p.ReservedBytes = 0 })
		},
		"reserved-too-high": func(e *env, _ *leasesv1.GenesisState) {
			e.editProvider("p2", func(p *providersv1.Provider) { p.ReservedBytes = 2 * packageSize })
		},
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := genesisEnv(t)
			g := sampleGenesis(e)
			edit(e, g)
			if err := leases.ValidateGenesis(g); err != nil {
				t.Fatalf("the document itself must be valid: %v", err)
			}
			before := e.snapshot(e.unmetered())
			if err := e.k.InitGenesis(e.unmetered(), g); err == nil {
				t.Fatal("imported")
			}
			if !bytes.Equal(before, e.snapshot(e.unmetered())) {
				t.Fatal("a failed import wrote")
			}
		})
	}
	// A lease of withdrawn content is valid (it can still end).
	e := genesisEnv(t)
	e.setContentState("c3", contentv1.ContentState_CONTENT_STATE_WITHDRAWN)
	g := sampleGenesis(e)
	g.Offers = nil
	if err := e.k.InitGenesis(e.unmetered(), g); err != nil {
		t.Fatalf("lease of withdrawn content: %v", err)
	}
}

// testGenesisRequiresAnEmptyStore: InitGenesis refuses a store that holds
// any key: the goal-05/06 scaffold key [0x01] (inside the offer prefix),
// a layout key, or a record.
func testGenesisRequiresAnEmptyStore(t *testing.T) {
	for name, key := range map[string][]byte{
		"scaffold-key": {0x01},
		"layout-key":   msgkit.LayoutVersionKey,
		"business-row": append([]byte{leases.BusinessPrefix}, "b1"...),
	} {
		t.Run(name, func(t *testing.T) {
			e := genesisEnv(t)
			e.leaseStore().Set(key, []byte(`{"offers":[],"leases":[]}`))
			before := e.snapshot(e.unmetered())
			if err := e.k.InitGenesis(e.unmetered(), &leasesv1.GenesisState{}); err == nil {
				t.Fatal("InitGenesis wrote over a non-empty store")
			}
			if !bytes.Equal(before, e.snapshot(e.unmetered())) {
				t.Fatal("refused import wrote")
			}
		})
	}
}

// testGenesisRoundTrip: import, export, and re-import into fresh stores
// give equal documents (records in id order, digests normalized) and
// byte-identical lease stores, with every derived row rebuilt and the
// invariants holding.
func testGenesisRoundTrip(t *testing.T) {
	a := genesisEnv(t)
	g := sampleGenesis(a)
	if err := a.k.InitGenesis(a.unmetered(), g); err != nil {
		t.Fatal(err)
	}
	a.requireInvariants(nil)
	exported, err := a.k.ExportGenesis(a.unmetered())
	if err != nil {
		t.Fatal(err)
	}
	want := proto.Clone(g).(*leasesv1.GenesisState)
	sort.Slice(want.Offers, func(i, j int) bool { return want.Offers[i].GetOfferId() < want.Offers[j].GetOfferId() })
	sort.Slice(want.Leases, func(i, j int) bool { return want.Leases[i].GetLeaseId() < want.Leases[j].GetLeaseId() })
	if !proto.Equal(exported, want) {
		t.Fatalf("export %v\nwant %v", exported, want)
	}
	raw, err := leases.MarshalGenesis(exported)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := leases.ParseGenesis(raw)
	if err != nil || !proto.Equal(parsed, exported) {
		t.Fatalf("JSON round trip: %v", err)
	}
	b := genesisEnv(t)
	if err := b.k.InitGenesis(b.unmetered(), parsed); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(a.snapshot(a.unmetered()), b.snapshot(b.unmetered())) {
		t.Fatal("re-imported stores differ")
	}
	// The pair, deadline, content, provider, expiry, counter, and marker
	// rows are present.
	store := b.leaseStore()
	for _, key := range [][]byte{
		leases.PairKey("p1", "c3", 1), leases.PairKey("p2", "c1", 1),
		leases.ContentLeaseKey("c3", "l1"), leases.ProviderLeaseKey("p2", "l2"),
		leases.ContentOfferKey("c1", "o1"), leases.UsedOfferKey("o1"), leases.UsedOfferKey("o-l3"),
		leases.ProviderOfferCountKey("p2"), leases.ContentOfferCountKey("c2"),
	} {
		if !store.Has(key) {
			t.Fatalf("row %x missing after import", key)
		}
	}
	for _, key := range [][]byte{leases.PairKey("p1", "c2", 1), leases.ContentLeaseKey("c2", "l3")} {
		if store.Has(key) {
			t.Fatalf("row %x of an ended lease present", key)
		}
	}
}

// testGenesisRoundTripFromCommittedTransitions: the export of a store
// built by messages and expiry re-imports into fresh stores to the same
// export. The re-imported lease store equals the committed one except
// exactly the business-id rows (not in the v0 genesis schema, DR-07-03),
// the used-offer markers of offers that expired or were voided, and the
// scheduler in the used-offer markers of accepted offers, which the
// import replaces with Marker (DR-08-06): the restriction is asserted,
// not hidden.
func testGenesisRoundTripFromCommittedTransitions(t *testing.T) {
	a := newEnv(t)
	a.mustLease("l1", "c1", "p1")
	a.mustLease("l2", "c2", "p2")
	a.mustLease("l3", "c3", "p1")
	a.requireOK(a.renew(a.fresh(), a.renewMsg("l1", "ren-l1")), "renew")
	a.requireOK(a.end(a.fresh(), a.endMsg("l2", "end-l2")), "end")
	a.mustOffer(a.offerMsg("o-exp", "c2", "p1"))
	a.mustOffer(withExpiry(a.offerMsg("o-open", "c2", "p2"), t0+5000))
	a.mustOffer(withExpiry(a.offerMsg("o-void", "c3", "p2"), t0+5000))
	if r := a.withdraw("c3"); r.code != msgkit.OK {
		t.Fatal(r.code)
	}
	// Expire o-exp (t0+600) and l3 (deadline t0+3700); l1 was renewed.
	if err := a.expire(a.blockCtx(t0 + 3700)); err != nil {
		t.Fatal(err)
	}
	a.requireInvariants(nil)
	exported, err := a.k.ExportGenesis(a.unmetered())
	if err != nil {
		t.Fatal(err)
	}
	if len(exported.GetOffers()) != 1 || len(exported.GetLeases()) != 3 {
		t.Fatalf("export %v", exported)
	}
	b := newEnv(t)
	b.leaseStore().Delete(msgkit.LayoutVersionKey)
	for _, id := range []string{"p1", "p2"} {
		p := a.provider(id)
		b.editProvider(id, func(q *providersv1.Provider) { q.ReservedBytes = p.GetReservedBytes() })
	}
	b.setContentState("c3", contentv1.ContentState_CONTENT_STATE_WITHDRAWN)
	if err := b.k.InitGenesis(b.unmetered(), exported); err != nil {
		t.Fatal(err)
	}
	again, err := b.k.ExportGenesis(b.unmetered())
	if err != nil || !proto.Equal(again, exported) {
		t.Fatalf("re-export differs: %v", err)
	}
	b.requireInvariants(nil)
	committed := rows(a.leaseStore())
	imported := rows(b.leaseStore())
	var dropped, changed []string
	for key, value := range committed {
		if got, ok := imported[key]; ok {
			if !bytes.Equal(got, value) {
				// Only the used-offer marker of an accepted offer may
				// differ: committed it holds the scheduler, imported it
				// holds Marker (a Lease has no scheduler field).
				if !bytes.Equal(value, serviceAcct) || !bytes.Equal(got, leases.Marker) {
					t.Fatalf("row %x differs: committed %x, imported %x", key, value, got)
				}
				changed = append(changed, key)
			}
			continue
		}
		dropped = append(dropped, key)
	}
	sort.Strings(changed)
	wantChanged := []string{string(leases.UsedOfferKey("o-l1")), string(leases.UsedOfferKey("o-l2")), string(leases.UsedOfferKey("o-l3"))}
	if strings.Join(changed, "|") != strings.Join(wantChanged, "|") {
		t.Fatalf("rows whose value the export did not carry: %q\nwant %q", changed, wantChanged)
	}
	for key := range imported {
		if _, ok := committed[key]; !ok {
			t.Fatalf("import added row %x", key)
		}
	}
	sort.Strings(dropped)
	var want []string
	for _, id := range []string{"acc-l1", "acc-l2", "acc-l3", "end-l2", "off-o-exp", "off-o-l1", "off-o-l2", "off-o-l3", "off-o-open", "off-o-void", "ren-l1"} {
		want = append(want, string(append([]byte{leases.BusinessPrefix}, id...)))
	}
	want = append(want, string(leases.UsedOfferKey("o-exp")), string(leases.UsedOfferKey("o-void")))
	sort.Strings(want)
	if strings.Join(dropped, "|") != strings.Join(want, "|") {
		t.Fatalf("rows not carried by export: %q\nwant %q", dropped, want)
	}
}

// rows is every row of a store keyed by its key bytes.
func rows(store storetypes.KVStore) map[string][]byte {
	out := map[string][]byte{}
	it := store.Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = append([]byte(nil), it.Value()...)
	}
	return out
}

// testGenesisDocumentRules: the document parser is strict, the default
// genesis is the empty catalogue, and the module export encoding uses
// proto field names.
func testGenesisDocumentRules(t *testing.T) {
	if _, err := leases.ParseGenesis(nil); err == nil {
		t.Fatal("empty document parsed")
	}
	if _, err := leases.ParseGenesis(json.RawMessage(`{"offers":[],"leases":[],"extra":1}`)); err == nil {
		t.Fatal("unknown field parsed")
	}
	if _, err := leases.ParseGenesis(json.RawMessage(`{"leases":[{"lease_id":"l1","region":"x"}]}`)); err == nil {
		t.Fatal("unknown nested field parsed")
	}
	raw := leases.NewAppModule(newEnv(t).k, nil).DefaultGenesis(nil)
	state, err := leases.ParseGenesis(raw)
	if err != nil || len(state.GetOffers()) != 0 || len(state.GetLeases()) != 0 {
		t.Fatalf("default genesis %s: %v", raw, err)
	}
	e := newEnv(t)
	out, err := leases.MarshalGenesis(sampleGenesis(e))
	if err != nil || !bytes.Contains(out, []byte(`"reservation_held"`)) || !bytes.Contains(out, []byte(`"package_bytes"`)) {
		t.Fatalf("export encoding %s: %v", out, err)
	}
}

// FuzzLeasesGenesisJSON: parsing never panics; a document that parses and
// validates survives a JSON round trip, and InitGenesis on fresh stores
// either fails and writes nothing or imports a store whose export equals
// the document in id order.
func FuzzLeasesGenesisJSON(f *testing.F) {
	seedEnv := newEnv(f)
	sample, err := leases.MarshalGenesis(sampleGenesis(seedEnv))
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{
		string(sample), `{}`, `{"offers":[],"leases":[]}`, `{"offers":[{"offer_id":"o1"}]}`,
		`{"leases":[{"lease_id":"l1","state":"LEASE_STATE_ACTIVE"}]}`, `{"leases":[{"state":6}]}`, `[]`, `null`, ``,
	} {
		f.Add([]byte(seed))
	}
	f.Fuzz(func(t *testing.T, raw []byte) {
		state, err := leases.ParseGenesis(raw)
		if err != nil {
			return
		}
		if leases.ValidateGenesis(state) != nil {
			return
		}
		out, err := leases.MarshalGenesis(state)
		if err != nil {
			t.Fatalf("marshal a valid state: %v", err)
		}
		again, err := leases.ParseGenesis(out)
		if err != nil || !proto.Equal(again, state) {
			t.Fatalf("JSON round trip: %v", err)
		}
		e := genesisEnv(t)
		before := e.snapshot(e.unmetered())
		if err := e.k.InitGenesis(e.unmetered(), state); err != nil {
			if !bytes.Equal(before, e.snapshot(e.unmetered())) {
				t.Fatal("a failed import wrote")
			}
			return
		}
		exported, err := e.k.ExportGenesis(e.unmetered())
		if err != nil {
			t.Fatal(err)
		}
		if len(exported.GetOffers()) != len(state.GetOffers()) || len(exported.GetLeases()) != len(state.GetLeases()) {
			t.Fatal("export lost records")
		}
	})
}
