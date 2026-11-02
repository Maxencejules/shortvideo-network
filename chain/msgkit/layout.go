package msgkit

import (
	"encoding/binary"
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
)

// ScaffoldLayoutVersion is the consensus version the goal-05/06 scaffold
// slots reported for the providers and content stores. A scaffold slot
// stored exactly one key, [0x01], holding the module genesis JSON (an
// empty catalogue), and never wrote LayoutVersionKey. The registry layouts
// start at version 2, so the two layouts never share a version number.
const ScaffoldLayoutVersion uint64 = 1

// LayoutVersionKey is the store key ([0x00]) where a module records its
// store layout version (its ConsensusVersion) at genesis. No record prefix
// of a registry module is 0x00.
//
// The key also keeps the module's IAVL tree from being empty. iavl v1.2.8
// saves the root of an empty tree as a key with an empty value, and the
// goleveldb backend of cosmos-db v1.1.3 reports a key with an empty value
// as absent. A store that has never held a key therefore fails to reload
// from a node's goleveldb database ("version does not exist"), and the
// node cannot restart. The in-memory database does not show this.
var LayoutVersionKey = []byte{0x00}

// WriteLayoutVersion stores version under LayoutVersionKey as 8 bytes,
// big endian. InitGenesis calls it, so every committed version of the
// module store holds at least this key.
func WriteLayoutVersion(store storetypes.KVStore, version uint64) {
	store.Set(LayoutVersionKey, binary.BigEndian.AppendUint64(nil, version))
}

// ReadLayoutVersion returns the version stored under LayoutVersionKey and
// whether the key exists. A value that is not 8 bytes is an error.
func ReadLayoutVersion(store storetypes.KVStore) (uint64, bool, error) {
	raw := store.Get(LayoutVersionKey)
	if raw == nil {
		return 0, false, nil
	}
	if len(raw) != 8 {
		return 0, true, fmt.Errorf("layout version value is %d bytes, want 8", len(raw))
	}
	return binary.BigEndian.Uint64(raw), true, nil
}
