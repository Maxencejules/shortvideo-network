package app_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
)

// fakeReads also serves the x/leases read surface for the round trip:
// import reads neither the DEC-005 term nor the policy version.
func (fakeReads) DevnetNumeric(sdk.Context, string) (uint64, bool) { return 0, false }
func (fakeReads) PolicyVersion(sdk.Context) uint64                 { return 1 }

// snapshotPairs re-encodes the pairs of a msgkit.StoreSnapshot encoding
// whose key starts with prefix and is not in drop, and returns them with
// their keys in order.
func snapshotPairs(t *testing.T, snapshot []byte, prefix byte, drop map[string]bool) ([]byte, []string) {
	t.Helper()
	var out []byte
	var keys []string
	for len(snapshot) > 0 {
		keyLen := binary.BigEndian.Uint64(snapshot[:8])
		key := snapshot[8 : 8+keyLen]
		rest := snapshot[8+keyLen:]
		valueLen := binary.BigEndian.Uint64(rest[:8])
		pair := snapshot[:8+keyLen+8+valueLen]
		if len(key) > 0 && key[0] == prefix && !drop[string(key)] {
			out = append(out, pair...)
			keys = append(keys, string(key))
		}
		snapshot = snapshot[len(pair):]
	}
	return out, keys
}

// pairValues reads every (key, value) pair of a msgkit.StoreSnapshot.
func pairValues(t *testing.T, snapshot []byte) map[string][]byte {
	t.Helper()
	out := map[string][]byte{}
	for len(snapshot) > 0 {
		keyLen := binary.BigEndian.Uint64(snapshot[:8])
		key := snapshot[8 : 8+keyLen]
		rest := snapshot[8+keyLen:]
		valueLen := binary.BigEndian.Uint64(rest[:8])
		out[string(key)] = append([]byte(nil), rest[8:8+valueLen]...)
		snapshot = snapshot[8+keyLen+8+valueLen:]
	}
	return out
}

// leasesRoundTrip exports the providers, content, and leases modules of
// chainApp's committed state (the module manager's ExportGenesis: App.Export
// refuses after council messages, DR-06-01, and App.Export itself must
// refuse here), imports them into fresh bare module stores (ValidateGenesis, then
// InitGenesis in genesis order), exports leases again, and compares the
// imported leases store row by row with the committed one. The rows may
// differ in exactly three ways, all recorded restrictions of the v0 genesis
// schema: business ids (prefix 0x03, DR-07-03) are not exported; the
// used-offer markers (prefix 0x0C, DR-08-06) of offers that expired or
// were voided are not exported; and the used-offer marker of an accepted
// offer holds its scheduler when committed but Marker when imported (a
// Lease has no scheduler field, DR-08-06). An open offer's marker holds
// its scheduler in both. closed is the offer ids whose markers must be
// missing. It returns the number of open offers and leases exported.
func leasesRoundTrip(t *testing.T, chainApp *app.App, closed []string) (int, int) {
	t.Helper()
	if _, err := chainApp.Export(); err == nil || !strings.HasPrefix(err.Error(), "export refused:") {
		t.Fatalf("App.Export with council state and a non-empty catalogue: %v", err)
	}
	exported, err := chainApp.ExportModulesForTest(app.ModuleProviders, app.ModuleContent, app.ModuleLeases)
	if err != nil {
		t.Fatal(err)
	}
	committed := chainApp.ModuleSnapshot(app.ModuleLeases)

	ctx, keys := msgkittest.NewContext(t, providers.ModuleName, content.ModuleName, leases.ModuleName)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	provState, err := providers.ParseGenesis(exported[app.ModuleProviders])
	if err != nil {
		t.Fatal(err)
	}
	provKeeper := providers.NewKeeper(keys[providers.ModuleName], providers.Deps{Roles: fakeReads{}, Limits: fakeReads{}})
	if err := provKeeper.InitGenesis(ctx, provState); err != nil {
		t.Fatal(err)
	}
	contState, err := content.ParseGenesis(exported[app.ModuleContent])
	if err != nil {
		t.Fatal(err)
	}
	contKeeper := content.NewKeeper(keys[content.ModuleName], content.Deps{Roles: fakeReads{}, Params: fakeReads{}, Limits: fakeReads{}})
	if err := contKeeper.InitGenesis(ctx, contState); err != nil {
		t.Fatal(err)
	}
	leaseState, err := leases.ParseGenesis(exported[app.ModuleLeases])
	if err != nil {
		t.Fatal(err)
	}
	if err := leases.ValidateGenesis(leaseState); err != nil {
		t.Fatal(err)
	}
	leaseKeeper := leases.NewKeeper(keys[leases.ModuleName], leases.Deps{
		Roles: fakeReads{}, Limits: fakeReads{}, Params: fakeReads{}, Policy: fakeReads{}, Providers: provKeeper, Content: contKeeper,
	})
	if err := leaseKeeper.InitGenesis(ctx, leaseState); err != nil {
		t.Fatal(err)
	}
	again, err := leaseKeeper.ExportGenesis(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(leaseState, again) {
		t.Fatal("leases export -> import -> export changed the state")
	}
	imported := msgkit.StoreSnapshot(ctx.KVStore(keys[leases.ModuleName]))

	for prefix := 0x00; prefix <= int(leases.SchedulerOfferCountPrefix); prefix++ {
		if prefix == int(leases.BusinessPrefix) || prefix == int(leases.UsedOfferPrefix) {
			continue
		}
		if !bytes.Equal(prefixPairs(t, committed, byte(prefix)), prefixPairs(t, imported, byte(prefix))) {
			t.Fatalf("imported rows under prefix %#x differ from the committed store", prefix)
		}
	}
	// Business ids are not in the v0 genesis schema (DR-07-03).
	if _, keys := snapshotPairs(t, committed, leases.BusinessPrefix, nil); len(keys) == 0 {
		t.Fatal("the committed store holds no business id")
	}
	if _, keys := snapshotPairs(t, imported, leases.BusinessPrefix, nil); len(keys) != 0 {
		t.Fatalf("the import holds %d business ids", len(keys))
	}
	// Used-offer markers: the import holds exactly the committed markers
	// minus those of the closed (expired or voided) offers (DR-08-06).
	drop := map[string]bool{}
	for _, id := range closed {
		drop[string(leases.UsedOfferKey(id))] = true
	}
	_, all := snapshotPairs(t, committed, leases.UsedOfferPrefix, nil)
	wantPairs, wantKeys := snapshotPairs(t, committed, leases.UsedOfferPrefix, drop)
	if len(all)-len(wantKeys) != len(closed) {
		t.Fatalf("committed markers %q do not hold every closed offer %v", all, closed)
	}
	_, gotKeys := snapshotPairs(t, imported, leases.UsedOfferPrefix, nil)
	if strings.Join(gotKeys, "|") != strings.Join(wantKeys, "|") {
		t.Fatalf("imported used-offer markers %q, want %q", gotKeys, wantKeys)
	}
	open := map[string]bool{}
	for _, o := range leaseState.GetOffers() {
		open[string(leases.UsedOfferKey(o.GetOfferId()))] = true
	}
	committedMarkers, importedMarkers := pairValues(t, wantPairs), pairValues(t, imported)
	for _, key := range wantKeys {
		was, now := committedMarkers[key], importedMarkers[key]
		if len(was) != 20 {
			t.Fatalf("committed marker %q holds %x, not a 20-byte scheduler", key, was)
		}
		if open[key] && !bytes.Equal(now, was) || !open[key] && !bytes.Equal(now, leases.Marker) {
			t.Fatalf("imported marker %q holds %x (open offer %t, committed %x)", key, now, open[key], was)
		}
	}
	return len(leaseState.GetOffers()), len(leaseState.GetLeases())
}

// TestLeasesGenesisRoundTripFromCommittedState is a module-level round
// trip of committed lease state, at two points of the lease flow: the
// height where l1, l4, and l7 are held (a fresh application replays the
// committed blocks up to it), and the end of the flow (every lease
// terminal, offer o13 open). It is not an app export/import: App.Export
// refuses after council messages (DR-06-01) and with a non-empty catalogue,
// and InitChain refuses a non-empty catalogue
// (TestLaunchGenesisRejectsNonEmptyCatalog), so the app-level round trip
// stays BLOCKED on DR-07-02, DR-07-03, and DR-08-06.
func TestLeasesGenesisRoundTripFromCommittedState(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	rec := runLeaseFlow(t, n, c)

	// The flow closed o6 by expiry and o7, o8 by withdrawal.
	closed := []string{"o6", "o7", "o8"}
	mid := c.replayFresh(t, n.doc, rec.queryHeight)
	offers, all := leasesRoundTrip(t, mid.app, closed)
	if offers != 0 || all != 7 {
		t.Fatalf("export at %d: %d offers, %d leases", rec.queryHeight, offers, all)
	}
	offers, all = leasesRoundTrip(t, c.app(), closed)
	if offers != 1 || all != 7 {
		t.Fatalf("export at the end: %d offers, %d leases", offers, all)
	}
}
