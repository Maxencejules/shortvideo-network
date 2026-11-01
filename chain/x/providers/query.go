package providers

import (
	"context"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Queries of svn.providers.v1.Query. They read committed state at the
// query height and never write. Errors are svnerr errors.

// QueryProvider is svn.providers.v1.Query/Provider.
//
//   - provider_id empty: MALFORMED; outside the id pattern: BOUNDS
//   - no record: NOT_FOUND
func (k Keeper) QueryProvider(goCtx context.Context, req *providersv1.QueryProviderRequest) (*providersv1.QueryProviderResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	if code := msgkit.CheckID(req.GetProviderId()); code != msgkit.OK {
		return nil, code.Err()
	}
	record, found, err := k.load(ctx, req.GetProviderId())
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, msgkit.NotFound.Err()
	}
	return &providersv1.QueryProviderResponse{Provider: record}, nil
}

// QueryProviders is svn.providers.v1.Query/Providers. It lists every
// provider in every state (history is never deleted), in id byte order.
//
//   - the page size cap is the effective max_page_limit (governed value, at
//     most the protocol ceiling 100); a nil page or limit 0 uses the cap
//   - limit above the cap: BOUNDS
//   - page_key above 64 bytes, or a non-empty page_key outside the id
//     pattern: BOUNDS
//   - page_key is inclusive: the page starts at that id or the next one
//   - next_page_key is the id of the first provider after the page, empty
//     on the last page
//
// The work is bounded by limit+1 iterator steps.
func (k Keeper) QueryProviders(goCtx context.Context, req *providersv1.QueryProvidersRequest) (*providersv1.QueryProvidersResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)
	limit, key, code := msgkit.CheckPage(req.GetPage(), k.pageLimit(ctx))
	if code != msgkit.OK {
		return nil, code.Err()
	}
	out := &providersv1.QueryProvidersResponse{}
	next, err := msgkit.Paginate(ctx.KVStore(k.key), []byte{ProviderKeyPrefix}, key, limit, func(storeKey, value []byte) error {
		record, err := decodeProvider(storeKey, value)
		if err != nil {
			return err
		}
		out.Providers = append(out.Providers, record)
		return nil
	})
	if err != nil {
		return nil, err
	}
	out.Page = &typesv1.PageResponse{NextPageKey: next}
	return out, nil
}
