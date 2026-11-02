package leases

import (
	"bytes"
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
)

// ContentWithdrawn is the content withdrawal hook (content.WithdrawalHook).
// x/content calls it inside WithdrawContent and inside a council
// force-withdraw, after the tombstone is written and before the withdrawal
// commits (withdraw_content in spec_machine.py:536).
//
// It voids every open offer of contentID, whatever its content version (the
// spec matches on content_id only; version is not consulted): it deletes the
// offer, its expiry and content index rows, decrements the provider and
// content open-offer counters (the content counter reaches 0 and its row is
// deleted), and emits leases_offer_closed{offer_id, reason "WITHDRAWN"} per
// offer in offer id byte order. The offer ids stay used (deviation D-4). It
// releases nothing and changes no lease: an ACTIVE or RENEWED lease of the
// withdrawn content keeps its reservation until EndLease or expiry, and its
// renewal is WITHDRAWN.
//
// The work is at most MaxOpenOffersPerContent offers (deviation D-6), so no
// set of offers can push a withdrawal past the transaction gas limit.
//
// The writes go to a cached branch of ctx that is written back, with its
// events, only on success. An id outside the id pattern, more rows than
// MaxOpenOffersPerContent, a row without its offer, an offer of another
// content, or a counter that disagrees with the rows is an invariant
// violation: the hook returns an svn MALFORMED error and writes nothing,
// and x/content then aborts the withdrawal (fail closed).
func (k Keeper) ContentWithdrawn(ctx sdk.Context, contentID string, version uint64) error {
	if err := k.voidOffers(ctx, contentID); err != nil {
		return svnerr.ErrMalformed.Wrapf("leases: withdrawal of %q (version %d): %v", contentID, version, err)
	}
	return nil
}

func (k Keeper) voidOffers(ctx sdk.Context, contentID string) error {
	if !msgkit.ValidID(contentID) {
		return fmt.Errorf("content id %q outside the id pattern", contentID)
	}
	ids, err := k.contentOfferIDs(ctx, contentID)
	if err != nil {
		return err
	}
	counted, err := k.count(ctx, ContentOfferCountKey(contentID))
	if err != nil {
		return err
	}
	if counted != uint64(len(ids)) {
		return fmt.Errorf("content %q counts %d open offers, its index holds %d", contentID, counted, len(ids))
	}
	if len(ids) == 0 {
		return nil
	}
	cached, write := ctx.CacheContext()
	for _, id := range ids {
		offer, found, err := k.loadOffer(cached, id)
		if err != nil {
			return err
		}
		if !found {
			return fmt.Errorf("content index row of %q has no open offer", id)
		}
		if offer.GetContentId() != contentID {
			return fmt.Errorf("content index row of %q under %q names content %q", id, contentID, offer.GetContentId())
		}
		if err := k.removeOffer(cached, offer); err != nil {
			return err
		}
		emitOfferClosed(cached, id, ReasonWithdrawn)
	}
	write()
	return nil
}

// contentOfferIDs reads the open-offer ids of one content id in byte order.
// It reads at most MaxOpenOffersPerContent+1 rows; one more row than the
// bound is an invariant violation.
func (k Keeper) contentOfferIDs(ctx sdk.Context, contentID string) (ids []string, err error) {
	prefix := scopePrefix(ContentOfferPrefix, contentID)
	it := k.kv(ctx).Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			ids, err = nil, fmt.Errorf("close iterator: %w", closeErr)
		}
	}()
	for ; it.Valid(); it.Next() {
		if len(ids) == MaxOpenOffersPerContent {
			return nil, fmt.Errorf("content %q has more than %d open offers", contentID, MaxOpenOffersPerContent)
		}
		id := string(it.Key()[len(prefix):])
		if !msgkit.ValidID(id) || !bytes.Equal(it.Value(), Marker) {
			return nil, fmt.Errorf("content index row %x is malformed", it.Key())
		}
		ids = append(ids, id)
	}
	// Iterator.Error is not consulted: a cachekv merge iterator reports an
	// exhausted range as an error ("invalid cacheMergeIterator").
	return ids, nil
}

// The keeper has the method set of content.WithdrawalHook (declared in
// x/content, which this package does not import).
var _ interface {
	ContentWithdrawn(ctx sdk.Context, contentID string, version uint64) error
} = Keeper{}
