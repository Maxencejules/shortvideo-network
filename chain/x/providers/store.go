package providers

import (
	"encoding/binary"
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// Store layout. Values are deterministic protobuf, never protojson.
//
//	0x01 | provider_id                         -> svn.providers.v1.Provider, without previous_signing_keys
//	0x02 | business_id                         -> 32-byte business digest (msgkit.Ledger)
//	0x03 | provider_id | 0x00 | generation (8B) -> svn.providers.v1.PreviousSigningKey
//
// Ids match ^[a-z0-9][a-z0-9._-]{0,63}$, so an id never contains 0x00 and
// key byte order is id byte order. The 0x00 after the id stops "p1" from
// matching "p10". The generation is big endian, so one provider's retired
// keys iterate in generation order. No record is ever deleted.
//
// Retired keys are separate rows, not a list inside the provider record.
// The record therefore has a constant size: a rotation, an update, a
// drain, and a council admit or disable each read and write the same
// number of bytes however many times the provider rotated. With the list
// inline, a provider could rotate until the record was too large for any
// later write to fit in the transaction gas limit, and the council could
// no longer disable it. Queries and genesis export put the retired keys
// back into previous_signing_keys.
//
// One provider holds at most MaxRetiredKeysPerProvider retired-key rows,
// so every read of one provider's history is bounded.
const (
	ProviderKeyPrefix byte = 0x01
	BusinessKeyPrefix byte = 0x02
	RetiredKeyPrefix  byte = 0x03
)

// MaxRetiredKeysPerProvider is the number of keys one provider id may
// retire. It is a chain safety bound, not a rule of the v0 spec machine
// (which has none): PROPOSED, decision request DR-07-10. A rotation that
// would retire one more key is ERROR_CODE_BOUNDS, after every spec check;
// a provider at the bound registers a new provider id to keep rotating.
// Under a capped id the provider can no longer rotate away from a
// compromised signing key: the remedies are drain, council disable, and a
// new id that needs council admission again.
// ValidateGenesis rejects a record above it, and a stored record above it
// is an invariant violation that queries and export report as an error.
//
// It is not above MaxRetiredKeysPerPage, so one provider's whole history
// fits in one Providers page: Query/Provider, every Providers page, and
// every exported record carry at most this many retired keys. Without it,
// rotations (which cost no fee) grew one provider's Query/Provider
// response, the Providers page that starts with it, and its export
// without limit.
const MaxRetiredKeysPerProvider = 1024

// ProviderKey is the store key of one provider record.
func ProviderKey(id string) []byte {
	out := make([]byte, 0, 1+len(id))
	out = append(out, ProviderKeyPrefix)
	return append(out, id...)
}

// retiredPrefix is the key prefix of one provider's retired keys.
func retiredPrefix(id string) []byte {
	out := make([]byte, 0, 2+len(id))
	out = append(out, RetiredKeyPrefix)
	out = append(out, id...)
	return append(out, 0x00)
}

// RetiredKeyKey is the store key of the key a provider retired at
// generation.
func RetiredKeyKey(id string, generation uint64) []byte {
	return binary.BigEndian.AppendUint64(retiredPrefix(id), generation)
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
// Retired keys are rows of their own (saveRetired); a record that carries
// them inline is a wiring error and panics.
func (k Keeper) save(ctx sdk.Context, record *providersv1.Provider) {
	if len(record.GetPreviousSigningKeys()) != 0 {
		panic(fmt.Sprintf("providers: record %q carries retired keys inline", record.GetProviderId()))
	}
	raw, err := deterministic.Marshal(record)
	if err != nil {
		panic(fmt.Sprintf("providers: marshal %q: %v", record.GetProviderId(), err))
	}
	ctx.KVStore(k.key).Set(ProviderKey(record.GetProviderId()), raw)
}

// saveRetired writes the row of one retired key.
func (k Keeper) saveRetired(ctx sdk.Context, providerID string, key *providersv1.PreviousSigningKey) {
	raw, err := deterministic.Marshal(key)
	if err != nil {
		panic(fmt.Sprintf("providers: marshal retired key %q/%d: %v", providerID, key.GetGeneration(), err))
	}
	ctx.KVStore(k.key).Set(RetiredKeyKey(providerID, key.GetGeneration()), raw)
}

func decodeProvider(key, raw []byte) (*providersv1.Provider, error) {
	var record providersv1.Provider
	if err := proto.Unmarshal(raw, &record); err != nil {
		return nil, fmt.Errorf("providers: stored record %q does not decode: %w", key[1:], err)
	}
	if record.GetProviderId() != string(key[1:]) {
		return nil, fmt.Errorf("providers: stored record under %q names %q", key[1:], record.GetProviderId())
	}
	if len(record.GetPreviousSigningKeys()) != 0 {
		return nil, fmt.Errorf("providers: stored record %q carries retired keys inline", key[1:])
	}
	return &record, nil
}

// retiredCount is the number of retired keys record claims
// (key_generation-1). A key_generation of 0, or a count above
// MaxRetiredKeysPerProvider, is an invariant violation: no message or
// genesis import writes one. Queries and export check it before they read
// any retired-key row, so their work stays bounded on any store.
func retiredCount(record *providersv1.Provider) (uint64, error) {
	generation := record.GetKeyGeneration()
	if generation < 1 || generation-1 > MaxRetiredKeysPerProvider {
		return 0, fmt.Errorf("providers: stored record %q has key_generation %d outside 1..%d",
			record.GetProviderId(), generation, MaxRetiredKeysPerProvider+1)
	}
	return generation - 1, nil
}

// history reads the retired keys of record in generation order. It checks
// the stored invariant: generations 1 to key_generation-1, one row each,
// at most MaxRetiredKeysPerProvider. Work is at most
// MaxRetiredKeysPerProvider+1 iterator steps: the walk stops at the first
// row past the claimed count. Message paths never call it.
func (k Keeper) history(ctx sdk.Context, record *providersv1.Provider) (keys []*providersv1.PreviousSigningKey, err error) {
	count, err := retiredCount(record)
	if err != nil {
		return nil, err
	}
	prefix := retiredPrefix(record.GetProviderId())
	it := ctx.KVStore(k.key).Iterator(prefix, storetypes.PrefixEndBytes(prefix))
	defer func() {
		if closeErr := it.Close(); closeErr != nil && err == nil {
			keys, err = nil, fmt.Errorf("providers: close iterator: %w", closeErr)
		}
	}()
	for ; it.Valid(); it.Next() {
		want := uint64(len(keys)) + 1
		if want > count {
			return nil, fmt.Errorf("providers: %q has a retired key row past key_generation %d", record.GetProviderId(), record.GetKeyGeneration())
		}
		var key providersv1.PreviousSigningKey
		if err := proto.Unmarshal(it.Value(), &key); err != nil {
			return nil, fmt.Errorf("providers: retired key %q/%d does not decode: %w", record.GetProviderId(), want, err)
		}
		suffix := it.Key()[len(prefix):]
		if len(suffix) != 8 || binary.BigEndian.Uint64(suffix) != want || key.GetGeneration() != want {
			return nil, fmt.Errorf("providers: retired key row %x of %q is not generation %d", it.Key(), record.GetProviderId(), want)
		}
		keys = append(keys, &key)
	}
	if uint64(len(keys)) != count {
		return nil, fmt.Errorf("providers: %q has %d retired keys at key_generation %d", record.GetProviderId(), len(keys), record.GetKeyGeneration())
	}
	return keys, nil
}

// withHistory returns record with previous_signing_keys filled from the
// retired-key rows. Queries and export use it.
func (k Keeper) withHistory(ctx sdk.Context, record *providersv1.Provider) (*providersv1.Provider, error) {
	keys, err := k.history(ctx, record)
	if err != nil {
		return nil, err
	}
	record.PreviousSigningKeys = keys
	return record, nil
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
