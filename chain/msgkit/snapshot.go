package msgkit

import (
	"encoding/binary"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
)

// StoreSnapshot serializes every key/value pair of store in iteration
// order as len(key) (8 bytes big-endian) || key || len(value) (8 bytes
// big-endian) || value. Two stores have equal snapshots exactly when they
// hold the same pairs. Tests use it to prove a rejected message left a
// module store byte-identical; it is not used in consensus paths.
// Iterator.Error is not consulted: a cachekv merge iterator reports an
// exhausted range as an error ("invalid cacheMergeIterator").
func StoreSnapshot(store storetypes.KVStore) []byte {
	it := store.Iterator(nil, nil)
	defer it.Close()
	var out []byte
	var size [8]byte
	for ; it.Valid(); it.Next() {
		binary.BigEndian.PutUint64(size[:], uint64(len(it.Key())))
		out = append(out, size[:]...)
		out = append(out, it.Key()...)
		binary.BigEndian.PutUint64(size[:], uint64(len(it.Value())))
		out = append(out, size[:]...)
		out = append(out, it.Value()...)
	}
	return out
}
