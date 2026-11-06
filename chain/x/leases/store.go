package leases

import (
	"encoding/binary"
	"fmt"
	"math"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// Store layout. Record values are deterministic protobuf, never protojson.
//
//	0x00                                          -> layout version 2, 8 bytes BE (msgkit.LayoutVersionKey)
//	0x01 | offer_id                               -> svn.leases.v1.Offer (open offers only)
//	0x02 | lease_id                               -> svn.leases.v1.Lease (every lease, every state; never deleted)
//	0x03 | business_id                            -> 32-byte business digest (msgkit.Ledger)
//	0x04 | provider_id 0x00 content_id 0x00 v(8B) -> lease_id (the live pair: leases holding a reservation)
//	0x05 | expiry(8B) | offer_id                  -> 0x01 (open offers by expiry)
//	0x06 | deadline(8B) | lease_id                -> 0x01 (held leases by current deadline)
//	0x07 | content_id 0x00 offer_id               -> 0x01 (open offers by content)
//	0x08 | content_id 0x00 lease_id               -> 0x01 (held leases by content)
//	0x09 | provider_id 0x00 lease_id              -> 0x01 (held leases by provider)
//	0x0A | provider_id                            -> open-offer count, 8 bytes BE (absent when 0)
//	0x0B | content_id                             -> open-offer count, 8 bytes BE (absent when 0)
//	0x0C | offer_id                               -> scheduler, 20 bytes (offer id used; never deleted);
//	                                                 0x01 when InitGenesis rebuilt it for a lease
//	0x0D | content_id 0x00 scheduler(20B)         -> open-offer count, 8 bytes BE (absent when 0)
//
// Ids match ^[a-z0-9][a-z0-9._-]{0,63}$, so an id never contains 0x00 and
// key byte order is id byte order; the 0x00 after a scope id stops "p1" from
// matching "p10". Times are validated into [1, 253402300799] before any
// write and encoded as the 8-byte big-endian uint64 of the Unix second, so
// key order is time order. Every value is non-empty: the goleveldb backend
// reports a key with an empty value as absent (msgkit.LayoutVersionKey).
//
// Records have a constant size: ids are at most 64 bytes, digests 32 bytes,
// the rest fixed scalars. Nothing grows inside a value. Index rows,
// counters, and used-offer markers are derived state: InitGenesis rebuilds
// them (a lease's marker without its scheduler) and ExportGenesis does not
// export them.
const (
	OfferPrefix               byte = 0x01
	LeasePrefix               byte = 0x02
	BusinessPrefix            byte = 0x03
	PairPrefix                byte = 0x04
	OfferExpiryPrefix         byte = 0x05
	LeaseDeadlinePrefix       byte = 0x06
	ContentOfferPrefix        byte = 0x07
	ContentLeasePrefix        byte = 0x08
	ProviderLeasePrefix       byte = 0x09
	ProviderOfferCountPrefix  byte = 0x0A
	ContentOfferCountPrefix   byte = 0x0B
	UsedOfferPrefix           byte = 0x0C
	SchedulerOfferCountPrefix byte = 0x0D
)

// Marker is the value of every index row that only needs to exist. It is
// for comparisons; do not modify it. Writes store a fresh copy (marker), so
// no store holds a slice that aliases this variable.
var Marker = []byte{0x01}

// marker is a fresh copy of Marker for one write.
func marker() []byte { return []byte{0x01} }

// timeKeyLen is the length of the big-endian time inside a time-ordered key.
const timeKeyLen = 8

func idKey(prefix byte, id string) []byte {
	out := make([]byte, 0, 1+len(id))
	out = append(out, prefix)
	return append(out, id...)
}

// scopePrefix is prefix | scope | 0x00: every row of one scope id.
func scopePrefix(prefix byte, scope string) []byte {
	out := make([]byte, 0, 2+len(scope))
	out = append(out, prefix)
	out = append(out, scope...)
	return append(out, 0x00)
}

func scopedKey(prefix byte, scope, id string) []byte {
	return append(scopePrefix(prefix, scope), id...)
}

// timeKey is prefix | uint64(t) big endian | id. Callers pass validated
// times only.
func timeKey(prefix byte, t int64, id string) []byte {
	out := make([]byte, 0, 1+timeKeyLen+len(id))
	out = append(out, prefix)
	out = binary.BigEndian.AppendUint64(out, uint64(t))
	return append(out, id...)
}

// OfferKey is the store key of one open offer.
func OfferKey(id string) []byte { return idKey(OfferPrefix, id) }

// LeaseKey is the store key of one lease.
func LeaseKey(id string) []byte { return idKey(LeasePrefix, id) }

// PairKey is the live-pair key of (provider, content, content version).
func PairKey(providerID, contentID string, version uint64) []byte {
	out := scopePrefix(PairPrefix, providerID)
	out = append(out, contentID...)
	out = append(out, 0x00)
	return binary.BigEndian.AppendUint64(out, version)
}

// pairScope is every live-pair row of one (provider, content).
func pairScope(providerID, contentID string) []byte {
	out := scopePrefix(PairPrefix, providerID)
	out = append(out, contentID...)
	return append(out, 0x00)
}

// OfferExpiryKey is the expiry index row of one open offer.
func OfferExpiryKey(expiry int64, id string) []byte { return timeKey(OfferExpiryPrefix, expiry, id) }

// LeaseDeadlineKey is the deadline index row of one held lease.
func LeaseDeadlineKey(deadline int64, id string) []byte {
	return timeKey(LeaseDeadlinePrefix, deadline, id)
}

// ContentOfferKey is the content index row of one open offer.
func ContentOfferKey(contentID, offerID string) []byte {
	return scopedKey(ContentOfferPrefix, contentID, offerID)
}

// ContentLeaseKey is the content index row of one held lease.
func ContentLeaseKey(contentID, leaseID string) []byte {
	return scopedKey(ContentLeasePrefix, contentID, leaseID)
}

// ProviderLeaseKey is the provider index row of one held lease.
func ProviderLeaseKey(providerID, leaseID string) []byte {
	return scopedKey(ProviderLeasePrefix, providerID, leaseID)
}

// ProviderOfferCountKey holds the open-offer count of one provider.
func ProviderOfferCountKey(id string) []byte { return idKey(ProviderOfferCountPrefix, id) }

// ContentOfferCountKey holds the open-offer count of one content id.
func ContentOfferCountKey(id string) []byte { return idKey(ContentOfferCountPrefix, id) }

// UsedOfferKey marks an offer id as used. It is never deleted, so an offer
// id is never reused (deviation D-4). Its value is the 20-byte scheduler
// that sent the offer: an idempotent OfferLease replay by another
// scheduler is FORBIDDEN (deviation D-11), and the sponsor of an accepted
// lease stays in committed state (lease.offer_id names the row). A marker
// InitGenesis rebuilds for a lease holds Marker instead: svn.leases.v1.Lease
// has no scheduler field, so an export does not carry it (DR-08-06).
func UsedOfferKey(id string) []byte { return idKey(UsedOfferPrefix, id) }

// SchedulerOfferCountKey holds the open-offer count of one scheduler for
// one content id (deviation D-10). The scheduler is a 20-byte account.
func SchedulerOfferCountKey(contentID string, scheduler []byte) []byte {
	return append(scopePrefix(SchedulerOfferCountPrefix, contentID), scheduler...)
}

// timeRangeEnd is the exclusive end of the time-ordered rows of prefix
// whose time is at most notAfter.
func timeRangeEnd(prefix byte, notAfter int64) []byte {
	if notAfter >= genesis.UnixSecondsMax {
		return storetypes.PrefixEndBytes([]byte{prefix})
	}
	out := make([]byte, 0, 1+timeKeyLen)
	out = append(out, prefix)
	return binary.BigEndian.AppendUint64(out, uint64(notAfter+1))
}

// splitTimeKey reads the time and the id of a time-ordered key.
func splitTimeKey(prefix byte, key []byte) (int64, string, error) {
	if len(key) < 1+timeKeyLen+1 || key[0] != prefix {
		return 0, "", fmt.Errorf("leases: index key %x is not a 0x%02x time key", key, prefix)
	}
	raw := binary.BigEndian.Uint64(key[1 : 1+timeKeyLen])
	id := string(key[1+timeKeyLen:])
	if raw < genesis.UnixSecondsMin || raw > genesis.UnixSecondsMax || !msgkit.ValidID(id) {
		return 0, "", fmt.Errorf("leases: index key %x has an invalid time or id", key)
	}
	return int64(raw), id, nil
}

// checkTime is check_time (spec_machine.py:189) for a decoded integer: a
// value outside [1, 253402300799] is BOUNDS.
func checkTime(t int64) msgkit.Code {
	if t < genesis.UnixSecondsMin || t > genesis.UnixSecondsMax {
		return msgkit.Bounds
	}
	return msgkit.OK
}

var deterministic = proto.MarshalOptions{Deterministic: true}

func (k Keeper) kv(ctx sdk.Context) storetypes.KVStore { return ctx.KVStore(k.key) }

func (k Keeper) has(ctx sdk.Context, key []byte) bool { return k.kv(ctx).Has(key) }

// loadOffer reads one open offer. An id outside the id pattern cannot have a
// record, so it returns not found without a store read. A stored value that
// does not decode, or whose id does not match its key, is an error.
func (k Keeper) loadOffer(ctx sdk.Context, id string) (*leasesv1.Offer, bool, error) {
	if !msgkit.ValidID(id) {
		return nil, false, nil
	}
	raw := k.kv(ctx).Get(OfferKey(id))
	if raw == nil {
		return nil, false, nil
	}
	offer, err := decodeOffer(OfferKey(id), raw)
	if err != nil {
		return nil, false, err
	}
	return offer, true, nil
}

// mustLoadOffer is loadOffer for message paths. The module writes only
// records it validated, so a record that does not decode is an invariant
// violation. It panics; the SDK turns a handler panic into a failed
// transaction on every node alike, and nothing of that transaction is
// written.
func (k Keeper) mustLoadOffer(ctx sdk.Context, id string) (*leasesv1.Offer, bool) {
	offer, found, err := k.loadOffer(ctx, id)
	if err != nil {
		panic(err.Error())
	}
	return offer, found
}

// loadLease reads one lease, with the same rules as loadOffer.
func (k Keeper) loadLease(ctx sdk.Context, id string) (*leasesv1.Lease, bool, error) {
	if !msgkit.ValidID(id) {
		return nil, false, nil
	}
	raw := k.kv(ctx).Get(LeaseKey(id))
	if raw == nil {
		return nil, false, nil
	}
	lease, err := decodeLease(LeaseKey(id), raw)
	if err != nil {
		return nil, false, err
	}
	return lease, true, nil
}

// mustLoadLease is loadLease for message paths; see mustLoadOffer.
func (k Keeper) mustLoadLease(ctx sdk.Context, id string) (*leasesv1.Lease, bool) {
	lease, found, err := k.loadLease(ctx, id)
	if err != nil {
		panic(err.Error())
	}
	return lease, found
}

func decodeOffer(key, raw []byte) (*leasesv1.Offer, error) {
	var offer leasesv1.Offer
	if err := proto.Unmarshal(raw, &offer); err != nil {
		return nil, fmt.Errorf("leases: stored offer %q does not decode: %w", key[1:], err)
	}
	if offer.GetOfferId() != string(key[1:]) {
		return nil, fmt.Errorf("leases: stored offer under %q names %q", key[1:], offer.GetOfferId())
	}
	return &offer, nil
}

func decodeLease(key, raw []byte) (*leasesv1.Lease, error) {
	var lease leasesv1.Lease
	if err := proto.Unmarshal(raw, &lease); err != nil {
		return nil, fmt.Errorf("leases: stored lease %q does not decode: %w", key[1:], err)
	}
	if lease.GetLeaseId() != string(key[1:]) {
		return nil, fmt.Errorf("leases: stored lease under %q names %q", key[1:], lease.GetLeaseId())
	}
	return &lease, nil
}

// marshal encodes a validated state message. Marshaling such a message
// cannot fail; a failure is an invariant violation and panics.
func marshal(m proto.Message, what string) []byte {
	raw, err := deterministic.Marshal(m)
	if err != nil {
		panic(fmt.Sprintf("leases: marshal %s: %v", what, err))
	}
	return raw
}

func (k Keeper) saveOffer(ctx sdk.Context, offer *leasesv1.Offer) {
	k.kv(ctx).Set(OfferKey(offer.GetOfferId()), marshal(offer, "offer "+offer.GetOfferId()))
}

func (k Keeper) saveLease(ctx sdk.Context, lease *leasesv1.Lease) {
	k.kv(ctx).Set(LeaseKey(lease.GetLeaseId()), marshal(lease, "lease "+lease.GetLeaseId()))
}

// count reads an open-offer counter. An absent row is 0. A value that is
// not 8 bytes, or a stored 0 (the row is deleted at 0), is an error.
func (k Keeper) count(ctx sdk.Context, key []byte) (uint64, error) {
	raw := k.kv(ctx).Get(key)
	if raw == nil {
		return 0, nil
	}
	if len(raw) != 8 {
		return 0, fmt.Errorf("leases: counter %x holds %d bytes", key, len(raw))
	}
	n := binary.BigEndian.Uint64(raw)
	if n == 0 {
		return 0, fmt.Errorf("leases: counter %x stores 0", key)
	}
	return n, nil
}

func (k Keeper) setCount(ctx sdk.Context, key []byte, n uint64) {
	if n == 0 {
		k.kv(ctx).Delete(key)
		return
	}
	k.kv(ctx).Set(key, binary.BigEndian.AppendUint64(nil, n))
}

func (k Keeper) increment(ctx sdk.Context, key []byte) error {
	n, err := k.count(ctx, key)
	if err != nil {
		return err
	}
	if n == math.MaxUint64 {
		return fmt.Errorf("leases: counter %x overflows", key)
	}
	k.setCount(ctx, key, n+1)
	return nil
}

func (k Keeper) decrement(ctx sdk.Context, key []byte) error {
	n, err := k.count(ctx, key)
	if err != nil {
		return err
	}
	if n == 0 {
		return fmt.Errorf("leases: counter %x is 0 before a decrement", key)
	}
	k.setCount(ctx, key, n-1)
	return nil
}

// addOffer writes an open offer, its expiry and content index rows, and its
// used-id marker (holding the scheduler), and increments the provider,
// content, and per-scheduler content open-offer counters.
func (k Keeper) addOffer(ctx sdk.Context, offer *leasesv1.Offer) error {
	id := offer.GetOfferId()
	k.saveOffer(ctx, offer)
	store := k.kv(ctx)
	store.Set(OfferExpiryKey(offer.GetExpiryUnixSeconds(), id), marker())
	store.Set(ContentOfferKey(offer.GetContentId(), id), marker())
	store.Set(UsedOfferKey(id), append([]byte(nil), offer.GetScheduler()...))
	for _, key := range offerCountKeys(offer) {
		if err := k.increment(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// removeOffer deletes an open offer and its expiry and content index rows,
// and decrements the provider, content, and per-scheduler content
// open-offer counters. The used-id marker stays.
func (k Keeper) removeOffer(ctx sdk.Context, offer *leasesv1.Offer) error {
	id := offer.GetOfferId()
	store := k.kv(ctx)
	store.Delete(OfferKey(id))
	store.Delete(OfferExpiryKey(offer.GetExpiryUnixSeconds(), id))
	store.Delete(ContentOfferKey(offer.GetContentId(), id))
	for _, key := range offerCountKeys(offer) {
		if err := k.decrement(ctx, key); err != nil {
			return err
		}
	}
	return nil
}

// offerCountKeys are the open-offer counters one open offer counts in.
func offerCountKeys(offer *leasesv1.Offer) [][]byte {
	return [][]byte{
		ProviderOfferCountKey(offer.GetProviderId()),
		ContentOfferCountKey(offer.GetContentId()),
		SchedulerOfferCountKey(offer.GetContentId(), offer.GetScheduler()),
	}
}

// holdIndexes writes the rows of a lease that holds a reservation: the
// live pair, the deadline index, and the content and provider indexes.
func (k Keeper) holdIndexes(ctx sdk.Context, lease *leasesv1.Lease) {
	id := lease.GetLeaseId()
	store := k.kv(ctx)
	store.Set(PairKey(lease.GetProviderId(), lease.GetContentId(), lease.GetContentVersion()), []byte(id))
	store.Set(LeaseDeadlineKey(lease.GetDeadlineUnixSeconds(), id), marker())
	store.Set(ContentLeaseKey(lease.GetContentId(), id), marker())
	store.Set(ProviderLeaseKey(lease.GetProviderId(), id), marker())
}

// dropIndexes deletes the rows holdIndexes wrote, at the lease's current
// deadline.
func (k Keeper) dropIndexes(ctx sdk.Context, lease *leasesv1.Lease) {
	id := lease.GetLeaseId()
	store := k.kv(ctx)
	store.Delete(PairKey(lease.GetProviderId(), lease.GetContentId(), lease.GetContentVersion()))
	store.Delete(LeaseDeadlineKey(lease.GetDeadlineUnixSeconds(), id))
	store.Delete(ContentLeaseKey(lease.GetContentId(), id))
	store.Delete(ProviderLeaseKey(lease.GetProviderId(), id))
}

// held reports whether a lease state holds a reservation.
func held(state leasesv1.LeaseState) bool {
	return state == leasesv1.LeaseState_LEASE_STATE_ACTIVE || state == leasesv1.LeaseState_LEASE_STATE_RENEWED
}

// iterate visits every row under one prefix in key byte order. The first
// error stops the walk. visit must not keep the slices.
func (k Keeper) iterate(ctx sdk.Context, prefix byte, visit func(key, value []byte) error) (err error) {
	start := []byte{prefix}
	it := k.kv(ctx).Iterator(start, storetypes.PrefixEndBytes(start))
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("leases: close iterator: %w", closeErr)
		}
	}()
	for ; it.Valid(); it.Next() {
		if visitErr := visit(it.Key(), it.Value()); visitErr != nil {
			return visitErr
		}
	}
	// Iterator.Error is not consulted: a cachekv merge iterator reports an
	// exhausted range as an error ("invalid cacheMergeIterator").
	return nil
}
