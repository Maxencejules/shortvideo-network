package leases_test

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// withdraw marks content withdrawn in the content fake and runs the hook
// on a trial branch, as x/content does inside a withdrawal, under the
// transaction gas ceiling.
func (e *env) withdraw(contentID string) result {
	e.t.Helper()
	ctx := e.fresh().WithGasMeter(storetypes.NewGasMeter(genesis.MaxGasPerTx))
	return e.exec(ctx, func(ctx sdk.Context) error {
		e.content.save(ctx, withState(e, contentID, contentv1.ContentState_CONTENT_STATE_WITHDRAWN))
		return e.k.ContentWithdrawn(ctx, contentID, 1)
	})
}

func withState(e *env, id string, state contentv1.ContentState) *contentv1.Content {
	c, found := e.content.load(e.unmetered(), id)
	if !found {
		e.t.Fatalf("no content %q", id)
	}
	c.State = state
	return c
}

// closedOffers is the offer ids of leases_offer_closed events with reason.
func closedOffers(t testing.TB, r result, reason string) []string {
	t.Helper()
	var out []string
	for _, ev := range r.events {
		if ev.Type != leases.EventOfferClosed {
			t.Fatalf("unexpected event %s", ev.Type)
		}
		if len(ev.Attributes) != 2 || ev.Attributes[1].Value != reason {
			t.Fatalf("event %v", ev)
		}
		out = append(out, ev.Attributes[0].Value)
	}
	return out
}

// TestWithdrawalVoidsOffers: the withdrawal hook voids every open offer of
// the content (any provider) in offer id order, decrements the counters,
// keeps the used-id markers, and leaves leases and reservations alone. A
// voided offer cannot be accepted (NOT_FOUND), a new offer of the content
// is WITHDRAWN, and the held lease's renewal is WITHDRAWN while it can
// still end and release.
func TestWithdrawalVoidsOffers(t *testing.T) {
	e := newEnv(t)
	e.mustOffer(e.offerMsg("o-c", "c1", "p1"))
	e.mustOffer(e.offerMsg("o-a", "c1", "p1"))
	e.mustOffer(e.offerMsg("o-b", "c1", "p2"))
	e.mustOffer(e.offerMsg("o-d", "c2", "p1"))
	lease := e.mustLease("l1", "c1", "p2")
	before := e.storedLease("l1")

	r := e.withdraw("c1")
	if r.code != msgkit.OK {
		t.Fatalf("hook %s", r.code)
	}
	if got := closedOffers(t, r, leases.ReasonWithdrawn); !slices.Equal(got, []string{"o-a", "o-b", "o-c"}) {
		t.Fatalf("voided %v", got)
	}
	store := e.leaseStore()
	for _, id := range []string{"o-a", "o-b", "o-c"} {
		if store.Has(leases.OfferKey(id)) || store.Has(leases.ContentOfferKey("c1", id)) {
			t.Fatalf("offer %s still open", id)
		}
		if !bytes.Equal(store.Get(leases.UsedOfferKey(id)), serviceAcct) {
			t.Fatalf("offer %s lost its used marker", id)
		}
		if store.Has(leases.SchedulerOfferCountKey("c1", serviceAcct)) {
			t.Fatalf("the scheduler counter of voided offer %s remains", id)
		}
	}
	if store.Has(leases.ContentOfferCountKey("c1")) || store.Has(leases.ProviderOfferCountKey("p2")) {
		t.Fatal("counters of voided offers remain")
	}
	if !bytes.Equal(store.Get(leases.ProviderOfferCountKey("p1")), []byte{0, 0, 0, 0, 0, 0, 0, 1}) {
		t.Fatal("p1 counter must count o-d only")
	}
	if !proto.Equal(e.storedLease("l1"), before) || e.provider("p2").GetReservedBytes() != packageSize {
		t.Fatal("withdrawal changed a lease or a reservation")
	}

	accept := e.acceptMsgFor(e.offerRecord("o-a", "c1", "p1"), "l-a")
	e.requireReject(e.accept(e.fresh(), accept), msgkit.NotFound, "accept a voided offer")
	e.requireReject(e.offer(e.fresh(), e.offerMsg("o-new", "c1", "p1")), msgkit.Withdrawn, "offer withdrawn content")
	e.requireReject(e.renew(e.fresh(), e.renewMsg("l1", "ren-l1")), msgkit.Withdrawn, "renew withdrawn content")
	e.requireInvariants(nil)
	e.requireOK(e.end(e.fresh(), e.endMsg("l1", "end-l1")), "end of a withdrawn content lease")
	if e.provider("p2").GetReservedBytes() != 0 || lease.GetLeaseId() != "l1" {
		t.Fatal("end did not release")
	}

	// A content id without open offers: nothing happens.
	r = e.withdraw("c3")
	if r.code != msgkit.OK || len(r.events) != 0 {
		t.Fatalf("empty withdrawal %s %v", r.code, r.events)
	}
	e.requireInvariants(nil)
}

// acceptMsgFor signs an acceptance of a (possibly closed) offer.
func (e *env) acceptMsgFor(offer *leasesv1.Offer, leaseID string) *leasesv1.MsgAcceptLease {
	return &leasesv1.MsgAcceptLease{
		Signer: broadcasterAcct, BusinessId: "acc-" + leaseID, OfferId: offer.GetOfferId(), LeaseId: leaseID,
		ProviderAcceptance: acceptSig(e.t, offer, leaseID, offer.GetProviderId(), "key-"+offer.GetProviderId()),
	}
}

// TestWithdrawalAtContentCap: the hook voids MaxOpenOffersPerContent
// offers in one call; its work is bounded by that constant.
func TestWithdrawalAtContentCap(t *testing.T) {
	e := newEnv(t)
	// Two schedulers: one alone holds at most half (D-10).
	for i := 0; i < leases.MaxOpenOffersPerContent; i++ {
		scheduler := [][]byte{serviceAcct, service2Acct}[(i/2)%2]
		e.seedOffer(e.offerRecordBy(scheduler, fmt.Sprintf("o%03d", i), "c1", []string{"p1", "p2"}[i%2]))
	}
	e.requireInvariants(nil)
	r := e.withdraw("c1")
	if r.code != msgkit.OK || len(closedOffers(t, r, leases.ReasonWithdrawn)) != leases.MaxOpenOffersPerContent {
		t.Fatalf("withdrawal at the cap: %s, %d events", r.code, len(r.events))
	}
	t.Logf("withdrawal hook at %d open offers: %d gas (keeper level, fake content store)", leases.MaxOpenOffersPerContent, r.gas)
	// The hook alone stays below half the transaction gas ceiling; the
	// app-level test measures the whole withdrawal.
	if r.gas >= genesis.MaxGasPerTx/2 {
		t.Fatalf("hook gas %d at the cap", r.gas)
	}
	e.requireInvariants(nil)
}

// TestWithdrawalHookInvariantErrors: a store whose content index and
// counter disagree, a row without its offer, an offer of another content,
// or more rows than the cap makes the hook return an svn MALFORMED error
// and write nothing; x/content then aborts the withdrawal.
func TestWithdrawalHookInvariantErrors(t *testing.T) {
	cases := map[string]func(e *env){
		"counter-high": func(e *env) {
			e.leaseStore().Set(leases.ContentOfferCountKey("c1"), []byte{0, 0, 0, 0, 0, 0, 0, 3})
		},
		"counter-missing": func(e *env) { e.leaseStore().Delete(leases.ContentOfferCountKey("c1")) },
		"row-without-offer": func(e *env) {
			e.leaseStore().Delete(leases.OfferKey("o-a"))
		},
		"row-of-other-content": func(e *env) {
			e.leaseStore().Set(leases.ContentOfferKey("c1", "o-x"), leases.Marker)
			e.seedOffer(e.offerRecord("o-x", "c2", "p1"))
			e.leaseStore().Set(leases.ContentOfferCountKey("c1"), []byte{0, 0, 0, 0, 0, 0, 0, 3})
		},
		"row-value":         func(e *env) { e.leaseStore().Set(leases.ContentOfferKey("c1", "o-a"), []byte{7}) },
		"counter-malformed": func(e *env) { e.leaseStore().Set(leases.ContentOfferCountKey("c1"), []byte{2}) },
		"provider-counter-missing": func(e *env) {
			e.leaseStore().Delete(leases.ProviderOfferCountKey("p2"))
		},
		"scheduler-counter-missing": func(e *env) {
			e.leaseStore().Delete(leases.SchedulerOfferCountKey("c1", serviceAcct))
		},
		"above-cap": func(e *env) {
			for i := 0; i < leases.MaxOpenOffersPerContent; i++ {
				e.leaseStore().Set(leases.ContentOfferKey("c1", fmt.Sprintf("z%03d", i)), leases.Marker)
			}
		},
	}
	for name, corrupt := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.mustOffer(e.offerMsg("o-a", "c1", "p1"))
			e.mustOffer(e.offerMsg("o-b", "c1", "p2"))
			corrupt(e)
			before := e.snapshot(e.unmetered())
			ctx := e.fresh()
			err := e.k.ContentWithdrawn(ctx, "c1", 1)
			if codeOf(t, err) != msgkit.Malformed {
				t.Fatalf("hook error %v", err)
			}
			if !bytes.Equal(before, e.snapshot(e.unmetered())) || len(ctx.EventManager().ABCIEvents()) != 0 {
				t.Fatal("a failed hook wrote or emitted")
			}
		})
	}
	e := newEnv(t)
	if codeOf(t, e.k.ContentWithdrawn(e.fresh(), "C1", 1)) != msgkit.Malformed {
		t.Fatal("hook accepted an id outside the pattern")
	}
}
