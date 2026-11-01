package msgkit

import (
	"encoding/binary"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
)

// StoreSnapshot serializes every key/value pair of store in iteration
// order as len(key) (8 bytes big-endian) || key || len(value) (8 bytes
// big-endian) || value. Two stores have equal snapshots exactly when they
// hold the same pairs. Tests use it to prove a rejected message left a
// module store byte-identical; it is not used in consensus paths. An
// iterator error panics, because a partial snapshot would compare as a
// false result.
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
	if err := it.Error(); err != nil {
		panic("msgkit: snapshot iterator: " + err.Error())
	}
	return out
}
