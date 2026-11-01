package providers

import (
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// Store layout. Values are deterministic protobuf, never protojson.
//
//	0x01 | provider_id  -> svn.providers.v1.Provider
//	0x02 | business_id  -> 32-byte business digest (msgkit.Ledger)
//
// Ids match ^[a-z0-9][a-z0-9._-]{0,63}$, so a key never contains 0x00 and
// key byte order is id byte order. No record is ever deleted.
const (
	ProviderKeyPrefix byte = 0x01
	BusinessKeyPrefix byte = 0x02
)

// ProviderKey is the store key of one provider record.
func ProviderKey(id string) []byte {
	out := make([]byte, 0, 1+len(id))
	out = append(out, ProviderKeyPrefix)
	return append(out, id...)
}

var deterministic = proto.MarshalOptions{Deterministic: true}

// load reads one provider. An id outside the id pattern cannot have a
// record, so it returns not found without a store read. A stored value that
// does not decode, or whose id does not match its key, is an error.
func (k Keeper) load(ctx sdk.Context, id string) (*providersv1.Provider, bool, error) {
	if !msgkit.ValidID(id) {
		return nil, false, nil
	}
	raw := ctx.KVStore(k.key).Get(ProviderKey(id))
	if raw == nil {
		return nil, false, nil
	}
	record, err := decodeProvider(ProviderKey(id), raw)
	if err != nil {
		return nil, false, err
	}
	return record, true, nil
}

// mustLoad is load for message paths. The module writes only records it
// validated, so a record that does not decode is an invariant violation. It
// panics; the SDK turns a handler panic into a failed transaction on every
// node alike, and nothing of that transaction is written.
func (k Keeper) mustLoad(ctx sdk.Context, id string) (*providersv1.Provider, bool) {
	record, found, err := k.load(ctx, id)
	if err != nil {
		panic(err.Error())
	}
	return record, found
}

// save writes one provider record. The record is a validated state message,
// so marshaling cannot fail; a failure is an invariant violation and panics.
func (k Keeper) save(ctx sdk.Context, record *providersv1.Provider) {
	raw, err := deterministic.Marshal(record)
	if err != nil {
		panic(fmt.Sprintf("providers: marshal %q: %v", record.GetProviderId(), err))
	}
	ctx.KVStore(k.key).Set(ProviderKey(record.GetProviderId()), raw)
}

func decodeProvider(key, raw []byte) (*providersv1.Provider, error) {
	var record providersv1.Provider
	if err := proto.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("providers: stored record %q does not decode: %w", key[1:], err)
	}
	if record.GetProviderId() != string(key[1:]) {
		return nil, fmt.Errorf("providers: stored record under %q names %q", key[1:], record.GetProviderId())
	}
	return &record, nil
}

// iterateProviders visits every provider in id byte order. Each record is a
// fresh decode, so visit may keep it. The first error stops the walk.
func (k Keeper) iterateProviders(ctx sdk.Context, visit func(*providersv1.Provider) error) (err error) {
	prefix := []byte{ProviderKeyPrefix}
	it := ctx.KVStore(k.key).Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			err = fmt.Errorf("providers: close iterator: %w", closeErr)
		}
	}()
	for ; it.Valid(); it.Next() {
		record, decodeErr := decodeProvider(it.Key(), it.Value())
		if decodeErr != nil {
			return decodeErr
		}
		if visitErr := visit(record); visitErr != nil {
			return visitErr
		}
	}
	// Iterator.Error is not consulted after the loop: inside a transaction
	// or query the store is a cachekv merge iterator, which reports an
	// exhausted range as an error ("invalid cacheMergeIterator").
	return nil
}
