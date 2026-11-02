package leases_test

import (
	"bytes"
	"fmt"
	"math"
	"slices"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// expiryT is the shared due time of the expiry fixture.
const expiryT = t0 + 5000

// expiryFixture seeds, all due at or before expiryT:
//
//	(T-10, lease, l0) (T-10, offer, o0)
//	(T, lease, la) (T, lease, lb) (T, lease, lz) (T, offer, oa) (T, offer, ob)
//
// and one lease (lx) and one offer (ox) due after T. Leases are held and
// their bytes reserved; contents d0..d9 are registered for them.
func expiryFixture(e *env) {
	for i := 0; i < 10; i++ {
		e.putContent(fmt.Sprintf("d%d", i), contentv1.ContentState_CONTENT_STATE_PENDING, packageSize)
	}
	lease := func(id, content string, deadline int64) {
		l := e.leaseRecord(id, content, "p1")
		l.StartUnixSeconds, l.DeadlineUnixSeconds = deadline-term, deadline
		e.seedLease(l)
	}
	offer := func(id, content string, expiry int64) {
		o := e.offerRecord(id, content, "p2")
		o.ExpiryUnixSeconds = expiry
		e.seedOffer(o)
	}
	lease("lz", "d0", expiryT)
	lease("la", "d1", expiryT)
	lease("lb", "d2", expiryT)
	lease("l0", "d3", expiryT-10)
	lease("lx", "d4", expiryT+1)
	offer("ob", "d5", expiryT)
	offer("oa", "d6", expiryT)
	offer("o0", "d7", expiryT-10)
	offer("ox", "d8", expiryT+1)
}

// expired is the (kind, id) sequence of the expiry events of one call.
func expired(t testing.TB, events []abci.Event) []string {
	t.Helper()
	var out []string
	for _, ev := range events {
		switch ev.Type {
		case "svn.events.v1.LeaseEnded":
			m, err := msgkit.ParseTyped(ev)
			if err != nil {
				t.Fatal(err)
			}
			ended := m.(*eventsv1.LeaseEnded)
			if ended.GetState() != "EXPIRED" || ended.GetReleasedBytes() != "1000" {
				t.Fatalf("expiry event %v", ended)
			}
			out = append(out, "lease:"+ended.GetLeaseId())
		case leases.EventOfferClosed:
			if len(ev.Attributes) != 2 || ev.Attributes[0].Key != leases.AttrOfferID || ev.Attributes[1].Key != leases.AttrReason ||
				ev.Attributes[1].Value != leases.ReasonExpired {
				t.Fatalf("offer closed event %v", ev)
			}
			out = append(out, "offer:"+ev.Attributes[0].Value)
		default:
			t.Fatalf("unexpected event %s", ev.Type)
		}
	}
	return out
}

// blockCtx is a BeginBlock context at header time now: the SDK runs
// BeginBlock on an infinite gas meter.
func (e *env) blockCtx(now int64) sdk.Context {
	return e.at(now).WithGasMeter(storetypes.NewInfiniteGasMeter())
}

// block runs ExpireDue at header time now and returns the expired items.
func (e *env) block(now int64) []string {
	e.t.Helper()
	ctx := e.blockCtx(now)
	if err := e.expire(ctx); err != nil {
		e.t.Fatal(err)
	}
	return expired(e.t, ctx.EventManager().ABCIEvents())
}

// TestExpiryOrderAndBudget (REQ-IDX-001): expiry processes due items in
// (time, kind, id) order with leases before offers at an equal time, at
// most the effective max_expiry_steps per block; the backlog continues in
// the next blocks; header time is the only clock.
func TestExpiryOrderAndBudget(t *testing.T) {
	order := []string{"lease:l0", "offer:o0", "lease:la", "lease:lb", "lease:lz", "offer:oa", "offer:ob"}
	for _, budget := range []uint64{1, 2, 3, 128} {
		t.Run(fmt.Sprintf("budget-%d", budget), func(t *testing.T) {
			e := newEnv(t)
			expiryFixture(e)
			if budget != 128 {
				e.auth.limits[msgkit.LimitMaxExpirySteps] = budget
			}
			// Effective-1: one second before the first due time nothing
			// expires.
			if got := e.block(expiryT - 11); len(got) != 0 {
				t.Fatalf("at T-11: %v", got)
			}
			// Effective: at T-10 only the T-10 items are due.
			var seen []string
			first := e.block(expiryT - 10)
			if want := order[:min(int(budget), 2)]; !slices.Equal(first, want) {
				t.Fatalf("at T-10: %v, want %v", first, want)
			}
			seen = append(seen, first...)
			if budget == 1 {
				seen = append(seen, e.block(expiryT-10)...)
			}
			if !slices.Equal(seen, order[:2]) {
				t.Fatalf("T-10 items %v", seen)
			}
			// T-1: nothing new is due.
			if got := e.block(expiryT - 1); len(got) != 0 {
				t.Fatalf("at T-1: %v", got)
			}
			// At T, each block expires exactly min(budget, remaining) items
			// in order until the backlog is empty.
			for len(seen) < len(order) {
				got := e.block(expiryT)
				if want := min(budget, uint64(len(order)-len(seen))); uint64(len(got)) != want {
					t.Fatalf("a block at T expired %d items, want %d", len(got), want)
				}
				seen = append(seen, got...)
			}
			if !slices.Equal(seen, order) {
				t.Fatalf("expiry order %v, want %v", seen, order)
			}
			if got := e.block(expiryT); len(got) != 0 {
				t.Fatalf("drained backlog expired %v", got)
			}
			// lx and ox are due at T+1, lease first.
			rest := e.block(expiryT + 1000)
			if !slices.Equal(rest, []string{"lease:lx", "offer:ox"}[:min(int(budget), 2)]) {
				t.Fatalf("late items %v", rest)
			}
			if e.provider("p1").GetReservedBytes() != e.heldBytes("p1") {
				t.Fatal("reserved bytes do not match the held leases")
			}
			e.requireInvariants(nil)
		})
	}
}

// heldBytes sums the package bytes of the provider's held leases.
func (e *env) heldBytes(providerID string) uint64 {
	state, err := e.k.ExportGenesis(e.unmetered())
	if err != nil {
		e.t.Fatal(err)
	}
	var sum uint64
	for _, l := range state.GetLeases() {
		if l.GetProviderId() == providerID && l.GetReservationHeld() {
			sum += l.GetPackageBytes()
		}
	}
	return sum
}

// TestExpiryUsesHeaderTimeOnly: the same store expires nothing under a
// header time before every due item and the same items under the same
// header time in two independent stores; the result depends on nothing but
// the header time and the store.
func TestExpiryUsesHeaderTimeOnly(t *testing.T) {
	a, b := newEnv(t), newEnv(t)
	expiryFixture(a)
	expiryFixture(b)
	for _, early := range []int64{expiryT - 11, 1, 0, -100, math.MinInt64 / 2} {
		if got := a.block(early); len(got) != 0 {
			t.Fatalf("header time %d expired %v", early, got)
		}
	}
	if !bytes.Equal(a.snapshot(a.unmetered()), b.snapshot(b.unmetered())) {
		t.Fatal("an early block changed the store")
	}
	ea, eb := a.block(expiryT), b.block(expiryT)
	if !slices.Equal(ea, eb) || len(ea) != 7 {
		t.Fatalf("expiry differs: %v vs %v", ea, eb)
	}
	if !bytes.Equal(a.snapshot(a.unmetered()), b.snapshot(b.unmetered())) {
		t.Fatal("stores differ after the same block")
	}
	for _, id := range []string{"l0", "la", "lb", "lz"} {
		l := a.storedLease(id)
		if l.GetState() != leasesv1.LeaseState_LEASE_STATE_EXPIRED || l.GetReservationHeld() {
			t.Fatalf("lease %s: %v", id, l)
		}
	}
	if a.storedLease("lx").GetState() != leasesv1.LeaseState_LEASE_STATE_ACTIVE {
		t.Fatal("lx expired early")
	}
	if got := a.provider("p1").GetReservedBytes(); got != packageSize {
		t.Fatalf("reserved %d, want one held lease", got)
	}
	a.requireInvariants(nil)
}

// TestExpiryInvariantErrors: a store that breaks an invariant the expiry
// relies on makes ExpireDue return an error and write nothing (the module
// BeginBlock then fails the block).
func TestExpiryInvariantErrors(t *testing.T) {
	cases := map[string]func(e *env){
		"deadline-row-without-lease": func(e *env) { e.leaseStore().Delete(leases.LeaseKey("la")) },
		"deadline-row-stale-time": func(e *env) {
			l := e.storedLease("la")
			l.DeadlineUnixSeconds = expiryT + 5
			e.k.SeedLeaseForTest(e.unmetered(), l)
		},
		"deadline-row-of-ended-lease": func(e *env) {
			l := e.storedLease("la")
			l.State, l.ReservationHeld = leasesv1.LeaseState_LEASE_STATE_ENDED, false
			e.leaseStore().Set(leases.LeaseKey("la"), mustMarshal(e.t, l))
		},
		"pair-row-missing": func(e *env) { e.leaseStore().Delete(leases.PairKey("p1", "d1", 1)) },
		"release-underflow": func(e *env) {
			e.editProvider("p1", func(p *providersv1.Provider) { p.ReservedBytes = 0 })
		},
		"deadline-row-value":       func(e *env) { e.leaseStore().Set(leases.LeaseDeadlineKey(expiryT, "la"), []byte{2}) },
		"expiry-row-without-offer": func(e *env) { e.leaseStore().Delete(leases.OfferKey("oa")) },
		"expiry-row-stale-time": func(e *env) {
			e.leaseStore().Set(leases.OfferExpiryKey(expiryT-20, "oa"), leases.Marker)
		},
		"counter-zero":      func(e *env) { e.leaseStore().Delete(leases.ProviderOfferCountKey("p2")) },
		"counter-malformed": func(e *env) { e.leaseStore().Set(leases.ContentOfferCountKey("d6"), []byte{1}) },
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			expiryFixture(e)
			corrupt(e)
			before := e.snapshot(e.unmetered())
			ctx := e.at(expiryT)
			if err := e.k.ExpireDue(ctx); err == nil {
				t.Fatal("ExpireDue accepted a broken store")
			}
			if !bytes.Equal(before, e.snapshot(e.unmetered())) {
				t.Fatal("a failed ExpireDue wrote")
			}
			if len(ctx.EventManager().ABCIEvents()) != 0 {
				t.Fatal("a failed ExpireDue emitted events")
			}
		})
	}
}

func mustMarshal(t testing.TB, m proto.Message) []byte {
	t.Helper()
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// TestExpiryWorkIsBounded: with a backlog far above the budget, one block
// processes exactly the budget and reads a bounded number of rows: its
// store gas is the same for a backlog of 200 and of 600 due leases.
func TestExpiryWorkIsBounded(t *testing.T) {
	gas := map[int]uint64{}
	for _, backlog := range []int{200, 600} {
		e := newEnv(t)
		e.editProvider("p1", func(p *providersv1.Provider) { p.AdmittedCapacityBytes = 1 << 40 })
		for i := 0; i < backlog; i++ {
			id := fmt.Sprintf("e%04d", i)
			e.putContent("x"+id, contentv1.ContentState_CONTENT_STATE_PENDING, packageSize)
			l := e.leaseRecord("l"+id, "x"+id, "p1")
			l.StartUnixSeconds, l.DeadlineUnixSeconds = expiryT-term, expiryT
			e.seedLease(l)
		}
		ctx := e.blockCtx(expiryT)
		if err := e.expire(ctx); err != nil {
			t.Fatal(err)
		}
		if n := len(expired(t, ctx.EventManager().ABCIEvents())); n != 128 {
			t.Fatalf("backlog %d: expired %d in one block, want 128", backlog, n)
		}
		gas[backlog] = ctx.GasMeter().GasConsumed()
		t.Logf("backlog %d: one block expired 128 leases with %d store gas", backlog, gas[backlog])
		blocks := 1
		for {
			c := e.blockCtx(expiryT + int64(blocks))
			if err := e.expire(c); err != nil {
				t.Fatal(err)
			}
			if len(c.EventManager().ABCIEvents()) == 0 {
				break
			}
			blocks++
		}
		if want := (backlog + 127) / 128; blocks != want {
			t.Fatalf("backlog %d drained in %d blocks, want %d", backlog, blocks, want)
		}
		if e.provider("p1").GetReservedBytes() != 0 {
			t.Fatal("drain did not release every lease")
		}
		e.requireInvariants(nil)
	}
	if gas[200] != gas[600] {
		t.Fatalf("block gas depends on the backlog: %d vs %d", gas[200], gas[600])
	}
}
