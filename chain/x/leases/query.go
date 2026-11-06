package leases

import (
	"bytes"
	"context"
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Queries of svn.leases.v1.Query. They read committed state at the query
// height and never write. A protocol failure is Code.QueryErr: the svnerr
// error with a gRPC status that carries the protocol code as a
// google.rpc.ErrorInfo detail (msgkit.QueryCode reads it). A stored record
// or index row that breaks a store invariant is a plain error.
//
// Page sizes: the cap is the effective max_page_limit (governed value, at
// most the protocol ceiling 100); a nil page or limit 0 uses the cap; a
// limit above the cap is BOUNDS. Every page item is one constant-size
// record (store layout), so a page is at most cap records whatever the
// store holds. The work of one call is at most limit+1 iterator steps per
// index walked plus one record read per item.

// ActiveLeases is svn.leases.v1.Query/ActiveLeases: the held leases (ACTIVE
// or RENEWED) of one content id, of one provider, or of one (provider,
// content) pair.
//
//   - content_id and provider_id both empty: MALFORMED (decision request
//     DR-08-08: the protocol has no unfiltered listing)
//   - a non-empty id outside the id pattern: BOUNDS (content_id first)
//   - page: msgkit.CheckPage (limit above the cap, a page_key above 64
//     bytes or outside the id pattern: BOUNDS)
//   - content only / provider only: leases in lease id byte order;
//     page_key is the inclusive lease id to start at; next_page_key is the
//     id of the first lease after the page, empty on the last page
//   - both: the live lease of the pair (v0 content records have version 1,
//     so a pair has at most one); a non-empty page_key is BOUNDS
func (k Keeper) ActiveLeases(goCtx context.Context, req *leasesv1.QueryActiveLeasesRequest) (*leasesv1.QueryActiveLeasesResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	contentID, providerID := req.GetContentId(), req.GetProviderId()
	if contentID == "" && providerID == "" {
		return nil, msgkit.Malformed.QueryErr()
	}
	for _, id := range []string{contentID, providerID} {
		if id != "" && !msgkit.ValidID(id) {
			return nil, msgkit.Bounds.QueryErr()
		}
	}
	limit, key, code := msgkit.CheckPage(req.GetPage(), k.pageLimit(ctx))
	if code != msgkit.OK {
		return nil, code.QueryErr()
	}
	out := &leasesv1.QueryActiveLeasesResponse{Page: &typesv1.PageResponse{}}
	if contentID != "" && providerID != "" {
		if len(key) != 0 {
			return nil, msgkit.Bounds.QueryErr()
		}
		lease, found, err := k.pairLease(ctx, providerID, contentID)
		if err != nil {
			return nil, err
		}
		if found {
			out.Leases = append(out.Leases, lease)
		}
		return out, nil
	}
	prefix, scope := scopePrefix(ContentLeasePrefix, contentID), contentID
	if contentID == "" {
		prefix, scope = scopePrefix(ProviderLeasePrefix, providerID), providerID
	}
	next, err := msgkit.Paginate(k.kv(ctx), prefix, key, limit, func(storeKey, value []byte) error {
		id := string(storeKey[len(prefix):])
		if !bytes.Equal(value, Marker) {
			return fmt.Errorf("leases: index row %x holds %x", storeKey, value)
		}
		lease, err := k.heldLease(ctx, id)
		if err != nil {
			return err
		}
		if (contentID != "" && lease.GetContentId() != scope) || (contentID == "" && lease.GetProviderId() != scope) {
			return fmt.Errorf("leases: index row %x names lease %q of another scope", storeKey, id)
		}
		out.Leases = append(out.Leases, lease)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Page.NextPageKey = next
	return out, nil
}

// pairLease reads the live lease of one (provider, content) pair. It reads
// at most two pair rows; a second row is an invariant violation (v0 content
// records have content_version 1).
func (k Keeper) pairLease(ctx sdk.Context, providerID, contentID string) (lease *leasesv1.Lease, found bool, err error) {
	prefix := pairScope(providerID, contentID)
	it := k.kv(ctx).Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			lease, found, err = nil, false, fmt.Errorf("leases: close iterator: %w", closeErr)
		}
	}()
	for ; it.Valid(); it.Next() {
		if found {
			return nil, false, fmt.Errorf("leases: pair (%q, %q) has more than one live lease", providerID, contentID)
		}
		lease, err = k.heldLease(ctx, string(it.Value()))
		if err != nil {
			return nil, false, err
		}
		want := PairKey(lease.GetProviderId(), lease.GetContentId(), lease.GetContentVersion())
		if !bytes.Equal(it.Key(), want) {
			return nil, false, fmt.Errorf("leases: pair row %x names lease %q of pair %x", it.Key(), lease.GetLeaseId(), want)
		}
		found = true
	}
	return lease, found, nil
}

// heldLease loads a lease an index row names. It must exist and hold its
// reservation.
func (k Keeper) heldLease(ctx sdk.Context, id string) (*leasesv1.Lease, error) {
	lease, found, err := k.loadLease(ctx, id)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fmt.Errorf("leases: index row names absent lease %q", id)
	}
	if !held(lease.GetState()) || !lease.GetReservationHeld() {
		return nil, fmt.Errorf("leases: index row names lease %q in state %s", id, lease.GetState())
	}
	return lease, nil
}

// Deadlines cursor. A page key of Deadlines is opaque, at most 64 bytes:
//
//   - phase 1 (held leases by deadline): the id of the next lease
//   - phase 2 (open offers by expiry): the id of the next offer with the
//     high bit of its first byte set
//
// An id's first byte is [0-9a-z], so the high bit marks phase 2 without
// growing the key. The keeper resumes from the record the cursor names, at
// its current time. Decision request DR-08-08 asks for a structured cursor.
const phaseTwoBit byte = 0x80

// cursorPhase is the list a Deadlines cursor resumes.
type cursorPhase int

const (
	phaseLeases cursorPhase = 1
	phaseOffers cursorPhase = 2
)

// encodeCursor is the page key that resumes phase at id.
func encodeCursor(phase cursorPhase, id string) []byte {
	out := []byte(id)
	if phase == phaseOffers && len(out) > 0 {
		out[0] |= phaseTwoBit
	}
	return out
}

// decodeCursor reads a Deadlines page key. ok is false for a key that is
// not a phase-1 or phase-2 cursor of an id in the id pattern.
func decodeCursor(raw []byte) (phase cursorPhase, id string, ok bool) {
	if len(raw) == 0 || len(raw) > msgkit.PageKeyMaxBytes {
		return 0, "", false
	}
	b := append([]byte(nil), raw...)
	phase = phaseLeases
	if b[0]&phaseTwoBit != 0 {
		phase = phaseOffers
		b[0] &^= phaseTwoBit
	}
	if !msgkit.ValidID(string(b)) {
		return 0, "", false
	}
	return phase, string(b), true
}

// Deadlines is svn.leases.v1.Query/Deadlines: the held leases whose current
// deadline is not after not_after_unix_seconds, in (deadline, lease id)
// order, then the open offers whose expiry is not after it, in (expiry,
// offer id) order. One page holds at most limit items across both lists.
//
//   - not_after_unix_seconds 0: MALFORMED; outside [1, 253402300799]: BOUNDS
//   - limit: as ActiveLeases (msgkit.CheckPage with the page_key removed)
//   - page_key: a Deadlines cursor. A key that does not decode, that names
//     an absent lease, a lease that no longer holds its reservation, or an
//     absent offer, or whose record's time is after not_after: BOUNDS. A
//     cursor is valid at the height that issued it.
//   - next_page_key: the cursor of the first item after the page, empty on
//     the last page
func (k Keeper) Deadlines(goCtx context.Context, req *leasesv1.QueryDeadlinesRequest) (*leasesv1.QueryDeadlinesResponse, error) {
	ctx, err := sdkContext(goCtx)
	if err != nil {
		return nil, err
	}
	notAfter := req.GetNotAfterUnixSeconds()
	if notAfter == 0 {
		return nil, msgkit.Malformed.QueryErr()
	}
	if code := checkTime(notAfter); code != msgkit.OK {
		return nil, code.QueryErr()
	}
	// The cursor is not an id (phase 2 sets a high bit), so CheckPage sees
	// only the limit; the cursor is checked below.
	limit, _, code := msgkit.CheckPage(&typesv1.PageRequest{Limit: req.GetPage().GetLimit()}, k.pageLimit(ctx))
	if code != msgkit.OK {
		return nil, code.QueryErr()
	}
	phase, leaseStart, offerStart := phaseLeases, []byte(nil), []byte(nil)
	if raw := req.GetPage().GetPageKey(); len(raw) != 0 {
		var id string
		var ok bool
		phase, id, ok = decodeCursor(raw)
		if !ok {
			return nil, msgkit.Bounds.QueryErr()
		}
		start, code, err := k.cursorStart(ctx, phase, id, notAfter)
		if err != nil {
			return nil, err
		}
		if code != msgkit.OK {
			return nil, code.QueryErr()
		}
		if phase == phaseLeases {
			leaseStart = start
		} else {
			offerStart = start
		}
	}

	out := &leasesv1.QueryDeadlinesResponse{Page: &typesv1.PageResponse{}}
	remaining := uint64(limit)
	if phase == phaseLeases {
		ids, next, err := k.walkTime(ctx, LeaseDeadlinePrefix, leaseStart, notAfter, remaining)
		if err != nil {
			return nil, err
		}
		for _, row := range ids {
			lease, err := k.heldLease(ctx, row.id)
			if err != nil {
				return nil, err
			}
			if lease.GetDeadlineUnixSeconds() != row.when {
				return nil, fmt.Errorf("leases: deadline row of %q is at %d, the lease deadline is %d", row.id, row.when, lease.GetDeadlineUnixSeconds())
			}
			out.Leases = append(out.Leases, lease)
		}
		if next != "" {
			out.Page.NextPageKey = encodeCursor(phaseLeases, next)
			return out, nil
		}
		remaining -= uint64(len(ids))
	}
	ids, next, err := k.walkTime(ctx, OfferExpiryPrefix, offerStart, notAfter, remaining)
	if err != nil {
		return nil, err
	}
	for _, row := range ids {
		offer, found, err := k.loadOffer(ctx, row.id)
		if err != nil {
			return nil, err
		}
		if !found || offer.GetExpiryUnixSeconds() != row.when {
			return nil, fmt.Errorf("leases: expiry row of %q at %d has no matching open offer", row.id, row.when)
		}
		out.Offers = append(out.Offers, offer)
	}
	if next != "" {
		out.Page.NextPageKey = encodeCursor(phaseOffers, next)
	}
	return out, nil
}

// cursorStart is the index key a cursor resumes at.
func (k Keeper) cursorStart(ctx sdk.Context, phase cursorPhase, id string, notAfter int64) ([]byte, msgkit.Code, error) {
	if phase == phaseLeases {
		lease, found, err := k.loadLease(ctx, id)
		if err != nil {
			return nil, msgkit.OK, err
		}
		if !found || !held(lease.GetState()) || !lease.GetReservationHeld() || lease.GetDeadlineUnixSeconds() > notAfter {
			return nil, msgkit.Bounds, nil
		}
		return LeaseDeadlineKey(lease.GetDeadlineUnixSeconds(), id), msgkit.OK, nil
	}
	offer, found, err := k.loadOffer(ctx, id)
	if err != nil {
		return nil, msgkit.OK, err
	}
	if !found || offer.GetExpiryUnixSeconds() > notAfter {
		return nil, msgkit.Bounds, nil
	}
	return OfferExpiryKey(offer.GetExpiryUnixSeconds(), id), msgkit.OK, nil
}

// timeRow is one row of a time index.
type timeRow struct {
	when int64
	id   string
}

// walkTime reads up to max rows of one time index whose time is not after
// notAfter, starting at start (inclusive; nil is the first row). next is the
// id of the row after them, or "" when there is none. It reads at most
// max+1 rows. max 0 reads one row, to report whether one exists.
func (k Keeper) walkTime(ctx sdk.Context, prefix byte, start []byte, notAfter int64, max uint64) (rows []timeRow, next string, err error) {
	if start == nil {
		start = []byte{prefix}
	}
	it := k.kv(ctx).Iterator(start, timeRangeEnd(prefix, notAfter))
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			rows, next, err = nil, "", fmt.Errorf("leases: close iterator: %w", closeErr)
		}
	}()
	for ; it.Valid(); it.Next() {
		when, id, splitErr := splitTimeKey(prefix, it.Key())
		if splitErr != nil {
			return nil, "", splitErr
		}
		if uint64(len(rows)) == max {
			return rows, id, nil
		}
		if !bytes.Equal(it.Value(), Marker) {
			return nil, "", fmt.Errorf("leases: index row %x holds %x", it.Key(), it.Value())
		}
		rows = append(rows, timeRow{when: when, id: id})
	}
	// Iterator.Error is not consulted: a cachekv merge iterator reports an
	// exhausted range as an error ("invalid cacheMergeIterator").
	return rows, "", nil
}
