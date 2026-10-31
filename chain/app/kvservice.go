package app

import (
	"context"

	"cosmossdk.io/core/store"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// newKVStoreService adapts an SDK store key to the core KV service.
//
// The SDK helper lives in cosmossdk.io's runtime package, and that package
// imports cosmos-sdk/server. svnd does not import server, because server
// imports the staking genesis types.
func newKVStoreService(key *storetypes.KVStoreKey) store.KVStoreService {
	return kvStoreService{key: key}
}

type kvStoreService struct {
	key *storetypes.KVStoreKey
}

func (k kvStoreService) OpenKVStore(ctx context.Context) store.KVStore {
	return coreKVStore{kvStore: sdk.UnwrapSDKContext(ctx).KVStore(k.key)}
}

type coreKVStore struct {
	kvStore storetypes.KVStore
}

func (s coreKVStore) Get(key []byte) ([]byte, error) {
	return s.kvStore.Get(key), nil
}

func (s coreKVStore) Has(key []byte) (bool, error) {
	return s.kvStore.Has(key), nil
}

func (s coreKVStore) Set(key, value []byte) error {
	s.kvStore.Set(key, value)
	return nil
}

func (s coreKVStore) Delete(key []byte) error {
	s.kvStore.Delete(key)
	return nil
}

func (s coreKVStore) Iterator(start, end []byte) (store.Iterator, error) {
	return s.kvStore.Iterator(start, end), nil
}

func (s coreKVStore) ReverseIterator(start, end []byte) (store.Iterator, error) {
	return s.kvStore.ReverseIterator(start, end), nil
}
