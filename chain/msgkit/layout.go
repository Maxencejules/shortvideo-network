package msgkit

import (
	"encoding/binary"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
)

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
