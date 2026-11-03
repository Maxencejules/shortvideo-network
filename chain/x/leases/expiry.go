package leases

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// Begin-block expiry (protocol.md "Expiry and determinism", op_expire in
// spec_machine.py:1530).
//
// A held lease is due when its current deadline <= committed time; an open
// offer is due when its expiry <= committed time. Committed time is the
// header time of the block being executed, in whole seconds. Due items are
// processed in (time, kind, id) order with kind "lease" before "offer" at an
// equal time (the spec's string order; evidence, the spec's third kind, is
// not swept: x/evidence reports a record's state at read time and uses no
// expiry budget, x/evidence deviation D-4), at most the effective
// max_expiry_steps items per block (governed value, ceiling 128). Items
// left over wait for the next committed block.
// Nothing expires while the chain commits no blocks; the real-time deadline
// in the signed terms does not move.
//
// The work of one call is bounded whatever the backlog: at most budget+1
// rows are read from each of the two time indexes, and at most budget items
// are processed.

// dueKind is the kind of a due item. At an equal time a lease sorts before
// an offer (the spec's string order "lease" < "offer").
type dueKind int

const (
	dueLease dueKind = iota
	dueOffer
)

// dueItem is one index row whose time is not after committed time.
type dueItem struct {
	when int64
	kind dueKind
	id   string
}

// ExpireDue is the BeginBlock body. It expires due held leases (release the
// reservation once, state EXPIRED, drop the pair, deadline, content, and
// provider rows, emit LeaseEnded{state "EXPIRED"}) and closes due open
// offers (drop the offer and its rows, decrement the open-offer counters,
// emit leases_offer_closed{reason "EXPIRED"}).
//
// The writes go to a cached branch of ctx that is written back, with its
// events, only when every item succeeded. An index row without its record,
// a record whose time differs from its index row, a lease that is not held,
// a Release that is not OK, or a counter that is already 0 is an invariant
// violation: ExpireDue returns an error and writes nothing. The module's
// BeginBlock returns that error, so FinalizeBlock fails on every node alike
// and the block is not committed (fail closed, decision request DR-08-07)
// instead of releasing twice or skipping a reservation.
func (k Keeper) ExpireDue(ctx sdk.Context) error {
	now := ctx.BlockTime().Unix()
	items, err := k.dueItems(ctx, now, k.limit(ctx, msgkit.LimitMaxExpirySteps))
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return nil
	}
	cached, write := ctx.CacheContext()
	for _, item := range items {
		switch item.kind {
		case dueLease:
			err = k.expireLease(cached, item)
		default:
			err = k.expireOffer(cached, item)
		}
		if err != nil {
			return err
		}
	}
	write()
	return nil
}

// dueItems returns at most budget due items in (time, kind, id) order. It
// reads at most budget+1 rows of each time index and writes nothing.
func (k Keeper) dueItems(ctx sdk.Context, now int64, budget uint64) ([]dueItem, error) {
	if now < 1 {
		// Every stored time is at least 1 (store layout), so nothing is due.
		// The guard also keeps timeRangeEnd away from a negative time.
		return nil, nil
	}
	leases, err := k.readDue(ctx, LeaseDeadlinePrefix, dueLease, now, budget)
	if err != nil {
		return nil, err
	}
	offers, err := k.readDue(ctx, OfferExpiryPrefix, dueOffer, now, budget)
	if err != nil {
		return nil, err
	}
	// Each stream is in (time, id) order. A lease goes first unless the next
	// offer's time is strictly earlier: at an equal time leases sort first.
	out := make([]dueItem, 0, min(budget, uint64(len(leases)+len(offers))))
	i, j := 0, 0
	for uint64(len(out)) < budget && (i < len(leases) || j < len(offers)) {
		switch {
		case j == len(offers) || (i < len(leases) && leases[i].when <= offers[j].when):
			out = append(out, leases[i])
			i++
		default:
			out = append(out, offers[j])
			j++
		}
	}
	return out, nil
}

// readDue reads up to max rows of one time index whose time is not after
// now, in key order (time, then id): at most max+1 iterator steps.
func (k Keeper) readDue(ctx sdk.Context, prefix byte, kind dueKind, now int64, max uint64) ([]dueItem, error) {
	rows, _, err := k.walkTime(ctx, prefix, nil, now, max)
	if err != nil {
		return nil, err
	}
	items := make([]dueItem, 0, len(rows))
	for _, row := range rows {
		items = append(items, dueItem{when: row.when, kind: kind, id: row.id})
	}
	return items, nil
}

// expireLease releases one due lease. Every check runs before the first
// write of this item.
func (k Keeper) expireLease(ctx sdk.Context, item dueItem) error {
	lease, found, err := k.loadLease(ctx, item.id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("leases: deadline row of %q has no lease", item.id)
	}
	if !held(lease.GetState()) || !lease.GetReservationHeld() {
		return fmt.Errorf("leases: deadline row of %q names a lease in state %s, reservation_held %t",
			item.id, lease.GetState(), lease.GetReservationHeld())
	}
	if lease.GetDeadlineUnixSeconds() != item.when {
		return fmt.Errorf("leases: deadline row of %q is at %d, the lease deadline is %d",
			item.id, item.when, lease.GetDeadlineUnixSeconds())
	}
	pair := k.kv(ctx).Get(PairKey(lease.GetProviderId(), lease.GetContentId(), lease.GetContentVersion()))
	if string(pair) != lease.GetLeaseId() {
		return fmt.Errorf("leases: pair row of held lease %q holds %q", lease.GetLeaseId(), pair)
	}
	if code := k.deps.Providers.Release(ctx, lease.GetProviderId(), lease.GetPackageBytes()); code != msgkit.OK {
		return fmt.Errorf("leases: release of expiring lease %q on provider %q: %s", lease.GetLeaseId(), lease.GetProviderId(), code)
	}
	k.dropIndexes(ctx, lease)
	lease.State = leasesv1.LeaseState_LEASE_STATE_EXPIRED
	lease.ReservationHeld = false
	k.saveLease(ctx, lease)
	return msgkit.EmitTyped(ctx, endedEvent(lease))
}

// expireOffer closes one due offer.
func (k Keeper) expireOffer(ctx sdk.Context, item dueItem) error {
	offer, found, err := k.loadOffer(ctx, item.id)
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("leases: expiry row of %q has no open offer", item.id)
	}
	if offer.GetExpiryUnixSeconds() != item.when {
		return fmt.Errorf("leases: expiry row of %q is at %d, the offer expiry is %d",
			item.id, item.when, offer.GetExpiryUnixSeconds())
	}
	if err := k.removeOffer(ctx, offer); err != nil {
		return err
	}
	emitOfferClosed(ctx, offer.GetOfferId(), ReasonExpired)
	return nil
}
