// Package leasestest is test support for x/leases: an invariant checker
// over a lease store and a byte snapshot of several stores. It is imported
// only by _test.go files and never runs in chain execution.
//
// CheckInvariants checks the state-local invariants of chain/x/leases/README.md
// over one context (committed state or a cached branch):
//
//	I1  per provider: reserved_bytes == baseline + sum of package_bytes of its held leases
//	I2  per provider: reserved_bytes <= admitted_capacity_bytes
//	I3  reservation_held exactly when the state is ACTIVE or RENEWED; no other state is stored
//	I4  at most one held lease per (provider, content, version); the pair rows are exactly the held leases
//	I5  the deadline, content, and provider rows are exactly the held leases
//	I6  the expiry and content rows are exactly the open offers; the counters equal the
//	    per-provider, per-content, and per-(content, scheduler) open-offer counts; a
//	    content id has at most MaxOpenOffersPerContent open offers, and at most
//	    MaxOpenOffersPerContentPerScheduler from one scheduler
//	I7  (single-state part) a stored lease has a valid record: ids in the pattern,
//	    term_version >= 1, ACTIVE at term 1, RENEWED at term >= 2
//	I10 every open offer and every lease's offer_id has a used-offer marker; an open
//	    offer's marker holds its scheduler, every other marker a 20-byte scheduler or
//	    Marker; an open offer id is not a lease's offer_id; leases name distinct offer ids
//
// The layout key must hold version 2, and no row may sit under an unknown
// prefix. I8 (one release per lease) and the history part of I7 are
// properties of transitions; I11 and I12 are properties of the code and the
// business-id ledger: the tests that name them check them. CheckOfferIntegrity
// checks I9 (open offers and leases) against the content registry.
package leasestest

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"sort"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// ProviderReader reads one provider record. providers.Keeper satisfies it.
type ProviderReader interface {
	Provider(ctx sdk.Context, providerID string) (*providersv1.Provider, bool)
}

// CheckInvariants returns the first violated invariant, or nil. The error
// text starts with the invariant name (for example "I4:"). baseline holds,
// per provider id, reserved bytes that no lease explains (fixtures that
// seed a provider's counter); an absent entry is 0. The providers checked
// are those named by an offer, a lease, or baseline. The store is read
// without gas.
func CheckInvariants(ctx sdk.Context, k leases.Keeper, providers ProviderReader, baseline map[string]uint64) error {
	rows, err := readRows(ctx, k.StoreKey())
	if err != nil {
		return err
	}
	if err := checkLayout(rows); err != nil {
		return err
	}
	view, err := decodeRecords(rows)
	if err != nil {
		return err
	}
	if err := view.checkDerivedRows(rows); err != nil {
		return err
	}
	return view.checkAccounting(ctx, providers, baseline)
}

// CheckOfferIntegrity is I9: every open offer names a PENDING content
// record with the offer's version, manifest digest, and package bytes
// (content.CheckOfferable is OK), and every lease, in any state, names a
// content record of its version, in any state, with the lease's manifest
// digest and package bytes (content.CheckPackage is OK). Withdrawal voids
// the offers of the content, so no open offer may name withdrawn content;
// a lease of withdrawn content keeps its committed package.
func CheckOfferIntegrity(ctx sdk.Context, k leases.Keeper, content leases.Content) error {
	rows, err := readRows(ctx, k.StoreKey())
	if err != nil {
		return err
	}
	view, err := decodeRecords(rows)
	if err != nil {
		return err
	}
	for _, id := range sortedKeys(view.offers) {
		offer := view.offers[id]
		code := content.CheckOfferable(ctx, offer.GetContentId(), offer.GetContentVersion(), offer.GetManifestDigest().GetValue(), offer.GetPackageBytes())
		if code != msgkit.OK {
			return fmt.Errorf("I9: offer %q: content check %s", id, code)
		}
	}
	for _, id := range sortedKeys(view.leases) {
		lease := view.leases[id]
		code := content.CheckPackage(ctx, lease.GetContentId(), lease.GetContentVersion(), lease.GetManifestDigest().GetValue(), lease.GetPackageBytes())
		if code != msgkit.OK {
			return fmt.Errorf("I9: lease %q: content check %s", id, code)
		}
	}
	return nil
}

// StoreSnapshot is msgkit.StoreSnapshot of each store, in argument order,
// each preceded by its store name and length (8-byte big-endian lengths).
// Two calls return equal bytes exactly when every store holds the same
// pairs. It reads through ctx's multistore without gas, so it works on a
// cached branch (ctx.CacheContext) to compare a trial state.
func StoreSnapshot(ctx sdk.Context, keys ...*storetypes.KVStoreKey) []byte {
	var out []byte
	for _, key := range keys {
		snapshot := msgkit.StoreSnapshot(ctx.MultiStore().GetKVStore(key))
		out = binary.BigEndian.AppendUint64(out, uint64(len(key.Name())))
		out = append(out, key.Name()...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(snapshot)))
		out = append(out, snapshot...)
	}
	return out
}

// readRows returns every row of the store keyed by the key bytes.
func readRows(ctx sdk.Context, key *storetypes.KVStoreKey) (map[string][]byte, error) {
	if key == nil {
		return nil, fmt.Errorf("leasestest: nil store key")
	}
	store := ctx.MultiStore().GetKVStore(key)
	it := store.Iterator(nil, nil)
	defer it.Close()
	rows := map[string][]byte{}
	for ; it.Valid(); it.Next() {
		rows[string(it.Key())] = append([]byte(nil), it.Value()...)
	}
	return rows, nil
}

func checkLayout(rows map[string][]byte) error {
	raw, found := rows[string(msgkit.LayoutVersionKey)]
	if !found {
		return fmt.Errorf("layout: no layout version key")
	}
	if len(raw) != 8 || binary.BigEndian.Uint64(raw) != leases.ConsensusVersion {
		return fmt.Errorf("layout: version row %x, want %d", raw, leases.ConsensusVersion)
	}
	return nil
}

// view is the decoded records of one store.
type view struct {
	offers map[string]*leasesv1.Offer
	leases map[string]*leasesv1.Lease
}

func decodeRecords(rows map[string][]byte) (*view, error) {
	v := &view{offers: map[string]*leasesv1.Offer{}, leases: map[string]*leasesv1.Lease{}}
	for _, key := range sortedKeys(rows) {
		value := rows[key]
		if len(key) == 0 {
			return nil, fmt.Errorf("layout: empty key")
		}
		if len(value) == 0 {
			return nil, fmt.Errorf("layout: row %x has an empty value", key)
		}
		id := key[1:]
		switch key[0] {
		case 0x00:
			if key != string(msgkit.LayoutVersionKey) {
				return nil, fmt.Errorf("layout: unknown row %x under 0x00", key)
			}
		case leases.OfferPrefix:
			var offer leasesv1.Offer
			if err := proto.Unmarshal(value, &offer); err != nil || offer.GetOfferId() != id || !msgkit.ValidID(id) {
				return nil, fmt.Errorf("I6: offer row %x does not decode to offer %q", key, id)
			}
			v.offers[id] = &offer
		case leases.LeasePrefix:
			var lease leasesv1.Lease
			if err := proto.Unmarshal(value, &lease); err != nil || lease.GetLeaseId() != id || !msgkit.ValidID(id) {
				return nil, fmt.Errorf("I7: lease row %x does not decode to lease %q", key, id)
			}
			if err := checkLeaseRecord(&lease); err != nil {
				return nil, err
			}
			v.leases[id] = &lease
		case leases.BusinessPrefix:
			if !msgkit.ValidID(id) || len(value) != msgkit.DigestBytes {
				return nil, fmt.Errorf("I12: business row %x is not an id and a 32-byte digest", key)
			}
		case leases.PairPrefix, leases.OfferExpiryPrefix, leases.LeaseDeadlinePrefix, leases.ContentOfferPrefix,
			leases.ContentLeasePrefix, leases.ProviderLeasePrefix, leases.ProviderOfferCountPrefix,
			leases.ContentOfferCountPrefix, leases.UsedOfferPrefix, leases.SchedulerOfferCountPrefix:
			// Compared with the derived rows below.
		default:
			return nil, fmt.Errorf("layout: row %x under unknown prefix 0x%02x", key, key[0])
		}
	}
	return v, nil
}

// checkLeaseRecord is I3 and the single-state part of I7.
func checkLeaseRecord(l *leasesv1.Lease) error {
	switch l.GetState() {
	case leasesv1.LeaseState_LEASE_STATE_ACTIVE, leasesv1.LeaseState_LEASE_STATE_RENEWED:
		if !l.GetReservationHeld() {
			return fmt.Errorf("I3: lease %q is %s without a reservation", l.GetLeaseId(), l.GetState())
		}
	case leasesv1.LeaseState_LEASE_STATE_ENDED, leasesv1.LeaseState_LEASE_STATE_EXPIRED:
		if l.GetReservationHeld() {
			return fmt.Errorf("I3: lease %q is %s and holds a reservation", l.GetLeaseId(), l.GetState())
		}
	default:
		return fmt.Errorf("I3: lease %q has state %d", l.GetLeaseId(), int32(l.GetState()))
	}
	for _, id := range []string{l.GetOfferId(), l.GetContentId(), l.GetProviderId()} {
		if !msgkit.ValidID(id) {
			return fmt.Errorf("I7: lease %q names id %q outside the pattern", l.GetLeaseId(), id)
		}
	}
	switch {
	case l.GetTermVersion() < 1:
		return fmt.Errorf("I7: lease %q has term_version 0", l.GetLeaseId())
	case l.GetState() == leasesv1.LeaseState_LEASE_STATE_ACTIVE && l.GetTermVersion() != 1:
		return fmt.Errorf("I7: ACTIVE lease %q has term_version %d", l.GetLeaseId(), l.GetTermVersion())
	case l.GetState() == leasesv1.LeaseState_LEASE_STATE_RENEWED && l.GetTermVersion() < 2:
		return fmt.Errorf("I7: RENEWED lease %q has term_version %d", l.GetLeaseId(), l.GetTermVersion())
	}
	if l.GetDeadlineUnixSeconds() < genesis.UnixSecondsMin || l.GetDeadlineUnixSeconds() > genesis.UnixSecondsMax {
		return fmt.Errorf("I7: lease %q has deadline %d out of range", l.GetLeaseId(), l.GetDeadlineUnixSeconds())
	}
	return nil
}

func held(l *leasesv1.Lease) bool { return l.GetReservationHeld() }

func be(n uint64) []byte { return binary.BigEndian.AppendUint64(nil, n) }

// checkDerivedRows compares every index, counter, and marker row with the
// rows the records imply.
func (v *view) checkDerivedRows(rows map[string][]byte) error {
	pairs := map[string][]byte{}
	leaseRows := map[string][]byte{}
	usedByLeases := map[string]string{}
	for _, id := range sortedKeys(v.leases) {
		l := v.leases[id]
		if other, taken := usedByLeases[l.GetOfferId()]; taken {
			return fmt.Errorf("I10: leases %q and %q name offer %q", other, id, l.GetOfferId())
		}
		usedByLeases[l.GetOfferId()] = id
		if !held(l) {
			continue
		}
		pair := string(leases.PairKey(l.GetProviderId(), l.GetContentId(), l.GetContentVersion()))
		if other, taken := pairs[pair]; taken {
			return fmt.Errorf("I4: leases %q and %q hold the same pair", other, id)
		}
		pairs[pair] = []byte(id)
		leaseRows[string(leases.LeaseDeadlineKey(l.GetDeadlineUnixSeconds(), id))] = leases.Marker
		leaseRows[string(leases.ContentLeaseKey(l.GetContentId(), id))] = leases.Marker
		leaseRows[string(leases.ProviderLeaseKey(l.GetProviderId(), id))] = leases.Marker
	}
	if err := sameRows("I4", rows, []byte{leases.PairPrefix}, pairs); err != nil {
		return err
	}
	if err := sameRows("I5", rows, []byte{leases.LeaseDeadlinePrefix, leases.ContentLeasePrefix, leases.ProviderLeasePrefix}, leaseRows); err != nil {
		return err
	}

	offerRows := map[string][]byte{}
	perProvider := map[string]uint64{}
	perContent := map[string]uint64{}
	perScheduler := map[string]uint64{}
	for _, id := range sortedKeys(v.offers) {
		o := v.offers[id]
		if lease, taken := usedByLeases[id]; taken {
			return fmt.Errorf("I10: open offer %q is the offer of lease %q", id, lease)
		}
		offerRows[string(leases.OfferExpiryKey(o.GetExpiryUnixSeconds(), id))] = leases.Marker
		offerRows[string(leases.ContentOfferKey(o.GetContentId(), id))] = leases.Marker
		perProvider[o.GetProviderId()]++
		perContent[o.GetContentId()]++
		perScheduler[string(leases.SchedulerOfferCountKey(o.GetContentId(), o.GetScheduler()))]++
	}
	if err := sameRows("I6", rows, []byte{leases.OfferExpiryPrefix, leases.ContentOfferPrefix}, offerRows); err != nil {
		return err
	}
	counters := map[string][]byte{}
	for id, n := range perProvider {
		counters[string(leases.ProviderOfferCountKey(id))] = be(n)
	}
	for id, n := range perContent {
		if n > leases.MaxOpenOffersPerContent {
			return fmt.Errorf("I6: content %q has %d open offers, bound %d", id, n, leases.MaxOpenOffersPerContent)
		}
		counters[string(leases.ContentOfferCountKey(id))] = be(n)
	}
	for _, key := range sortedKeys(perScheduler) {
		n := perScheduler[key]
		if n > leases.MaxOpenOffersPerContentPerScheduler {
			return fmt.Errorf("I6: scheduler counter %x counts %d open offers, bound %d", key, n, leases.MaxOpenOffersPerContentPerScheduler)
		}
		counters[key] = be(n)
	}
	if err := sameRows("I6", rows, []byte{leases.ProviderOfferCountPrefix, leases.ContentOfferCountPrefix, leases.SchedulerOfferCountPrefix}, counters); err != nil {
		return err
	}

	for _, key := range sortedKeys(rows) {
		if key[0] != leases.UsedOfferPrefix {
			continue
		}
		value := rows[key]
		if !msgkit.ValidID(key[1:]) || (len(value) != msgkit.AccountBytes && !bytes.Equal(value, leases.Marker)) {
			return fmt.Errorf("I10: used-offer row %x is malformed", key)
		}
	}
	for _, id := range sortedKeys(v.offers) {
		marker, found := rows[string(leases.UsedOfferKey(id))]
		if !found {
			return fmt.Errorf("I10: offer id %q has no used-offer marker", id)
		}
		if !bytes.Equal(marker, v.offers[id].GetScheduler()) {
			return fmt.Errorf("I10: the used-offer marker of open offer %q holds %x, not its scheduler", id, marker)
		}
	}
	for _, id := range sortedKeys(usedByLeases) {
		if _, found := rows[string(leases.UsedOfferKey(id))]; !found {
			return fmt.Errorf("I10: offer id %q has no used-offer marker", id)
		}
	}
	return nil
}

// sameRows compares the rows under the given prefixes with want.
func sameRows(rule string, rows map[string][]byte, prefixes []byte, want map[string][]byte) error {
	under := map[byte]bool{}
	for _, p := range prefixes {
		under[p] = true
	}
	for _, key := range sortedKeys(rows) {
		if !under[key[0]] {
			continue
		}
		expected, ok := want[key]
		if !ok {
			return fmt.Errorf("%s: row %x has no record", rule, key)
		}
		if !bytes.Equal(rows[key], expected) {
			return fmt.Errorf("%s: row %x holds %x, want %x", rule, key, rows[key], expected)
		}
	}
	for _, key := range sortedKeys(want) {
		if _, ok := rows[key]; !ok {
			return fmt.Errorf("%s: row %x is missing", rule, key)
		}
	}
	return nil
}

// checkAccounting is I1 and I2.
func (v *view) checkAccounting(ctx sdk.Context, providers ProviderReader, baseline map[string]uint64) error {
	if providers == nil {
		return fmt.Errorf("leasestest: nil provider reader")
	}
	expected := map[string]uint64{}
	for id, n := range baseline {
		expected[id] = n
	}
	for _, id := range sortedKeys(v.offers) {
		if p := v.offers[id].GetProviderId(); !hasKey(expected, p) {
			expected[p] = 0
		}
	}
	for _, id := range sortedKeys(v.leases) {
		l := v.leases[id]
		sum := expected[l.GetProviderId()]
		if held(l) {
			if l.GetPackageBytes() > math.MaxUint64-sum {
				return fmt.Errorf("I1: held bytes of provider %q overflow", l.GetProviderId())
			}
			sum += l.GetPackageBytes()
		}
		expected[l.GetProviderId()] = sum
	}
	for _, id := range sortedKeys(expected) {
		record, found := providers.Provider(ctx, id)
		if !found {
			return fmt.Errorf("I1: provider %q is not registered", id)
		}
		if record.GetReservedBytes() != expected[id] {
			return fmt.Errorf("I1: provider %q reserves %d bytes, want %d", id, record.GetReservedBytes(), expected[id])
		}
		if record.GetReservedBytes() > record.GetAdmittedCapacityBytes() {
			return fmt.Errorf("I2: provider %q reserves %d bytes above capacity %d", id, record.GetReservedBytes(), record.GetAdmittedCapacityBytes())
		}
	}
	return nil
}

func hasKey[V any](m map[string]V, key string) bool {
	_, ok := m[key]
	return ok
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
