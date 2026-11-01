// Package msgkittest is test support for the registry modules. It is
// imported only by _test.go files. Nothing here runs in chain execution.
package msgkittest

import (
	"testing"
	"time"

	"cosmossdk.io/log/v2"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"

	store "github.com/cosmos/cosmos-sdk/store/v2"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Fixed values of the test context. They are synthetic.
const (
	ChainID     = "shortvideo-dev-test"
	BlockHeight = int64(7)
	// BlockUnixSeconds is 2026-01-01T00:00:00Z.
	BlockUnixSeconds = int64(1_767_225_600)
	GasLimit         = uint64(200_000)
	TxBytesLen       = 256
)

// NewContext mounts one IAVL store per name on a real rootmulti store over
// an in-memory cosmos-db database and returns a FinalizeBlock-mode context with
// chain id ChainID, height BlockHeight, block time BlockUnixSeconds, a gas
// meter limited to GasLimit, and TxBytesLen transaction bytes. Writes go to
// the IAVL working trees, exactly as a message handler's writes do once the
// SDK writes its branch back. Names must be unique and non-empty.
func NewContext(t testing.TB, names ...string) (sdk.Context, map[string]*storetypes.KVStoreKey) {
	t.Helper()
	cms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger())
	keys := make(map[string]*storetypes.KVStoreKey, len(names))
	for _, name := range names {
		if name == "" {
			t.Fatal("msgkittest: empty store name")
		}
		if _, dup := keys[name]; dup {
			t.Fatalf("msgkittest: store %q mounted twice", name)
		}
		key := storetypes.NewKVStoreKey(name)
		keys[name] = key
		cms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, nil)
	}
	if err := cms.LoadLatestVersion(); err != nil {
		t.Fatalf("msgkittest: load stores: %v", err)
	}
	header := cmtproto.Header{
		ChainID: ChainID,
		Height:  BlockHeight,
		Time:    time.Unix(BlockUnixSeconds, 0).UTC(),
	}
	ctx := sdk.NewContext(cms, header, false, log.NewNopLogger()).
		WithGasMeter(storetypes.NewGasMeter(GasLimit)).
		WithTxBytes(make([]byte, TxBytesLen)).
		WithExecMode(sdk.ExecModeFinalize)
	return ctx, keys
}
