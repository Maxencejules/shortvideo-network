package msgkit

import (
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// CheckPage validates a page request against max, the effective page limit
// (normally Effective(LimitMaxPageLimit, ...)). A max of 0 or above the
// protocol safety cap is replaced by the cap, the same rule Effective uses.
//
//   - nil page or limit 0: the page size is max
//   - limit above max: BOUNDS
//   - page_key above 64 bytes: BOUNDS
//   - a non-empty page_key outside the id pattern: BOUNDS
//
// The returned key is a copy; empty means the first page.
func CheckPage(p *typesv1.PageRequest, max uint32) (limit uint32, key []byte, c Code) {
	if max == 0 || max > genesis.MaxPageLimit {
		max = genesis.MaxPageLimit
	}
	limit = p.GetLimit()
	if limit == 0 {
		limit = max
	}
	if limit > max {
		return 0, nil, Bounds
	}
	raw := p.GetPageKey()
	if len(raw) > PageKeyMaxBytes {
		return 0, nil, Bounds
	}
	if len(raw) > 0 && !ValidID(string(raw)) {
		return 0, nil, Bounds
	}
	if len(raw) == 0 {
		return limit, nil, OK
	}
	return limit, append([]byte(nil), raw...), OK
}

// Paginate visits up to limit entries of store under prefix, in key byte
// order, starting at prefix||key inclusive (an empty key starts at the first
// entry). visit receives the full key and the value; it must not keep the
// slices. next is the key suffix (without prefix) of the first entry after
// the page, or nil when the page reached the end of the prefix. A limit of
// 0 is an error; CheckPage never returns it. The work is bounded by
// limit+1 iterator steps.
func Paginate(store storetypes.KVStore, prefix, key []byte, limit uint32, visit func(k, v []byte) error) (next []byte, err error) {
	if limit == 0 {
		return nil, fmt.Errorf("paginate: limit is 0")
	}
	if visit == nil {
		return nil, fmt.Errorf("paginate: nil visitor")
	}
	var start []byte
	if len(prefix)+len(key) > 0 {
		start = make([]byte, 0, len(prefix)+len(key))
		start = append(start, prefix...)
		start = append(start, key...)
	}
	end := storetypes.PrefixEndBytes(prefix)
	it := store.Iterator(start, end)
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			next, err = nil, fmt.Errorf("paginate: close iterator: %w", closeErr)
		}
	}()
	var visited uint32
	for ; it.Valid(); it.Next() {
		if visited == limit {
			return append([]byte(nil), it.Key()[len(prefix):]...), nil
		}
		if err := visit(it.Key(), it.Value()); err != nil {
			return nil, err
		}
		visited++
	}
	if err := it.Error(); err != nil {
		return nil, fmt.Errorf("paginate: iterator: %w", err)
	}
	return nil, nil
}
