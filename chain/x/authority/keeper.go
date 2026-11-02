package authority

import (
	"context"
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// ModuleName is the only module that may return validator updates.
const ModuleName = "authority"

// Keeper stores authority state in the per-object layout of store.go.
// InitGenesis writes the genesis set. EndBlock writes a scheduled power or
// key change when its activation point is due. No other module returns
// validator updates.
type Keeper struct {
	storeKey *storetypes.KVStoreKey
}

func NewKeeper(storeKey *storetypes.KVStoreKey) Keeper {
	return Keeper{storeKey: storeKey}
}

func (k Keeper) StoreKey() *storetypes.KVStoreKey {
	return k.storeKey
}

// Init writes a validated genesis state into the empty store (InitStore).
func (k Keeper) Init(ctx context.Context, state *authorityv1.GenesisState) error {
	if state == nil {
		return fmt.Errorf("authority genesis is nil")
	}
	return InitStore(sdk.UnwrapSDKContext(ctx).KVStore(k.storeKey), state)
}

// Export returns the genesis form of the store, or an error wrapping
// ErrExportRefused while the store holds state the genesis proto cannot carry.
func (k Keeper) Export(ctx context.Context) (*authorityv1.GenesisState, error) {
	return ExportState(sdk.UnwrapSDKContext(ctx).KVStore(k.storeKey))
}

// ConsensusBytes returns the bytes of the config key. They are
// proto.MarshalOptions{Deterministic: true}, never protojson.
func (k Keeper) ConsensusBytes(ctx context.Context) ([]byte, error) {
	raw := sdk.UnwrapSDKContext(ctx).KVStore(k.storeKey).Get([]byte{configKey})
	if raw == nil {
		return nil, errMissing
	}
	return append([]byte(nil), raw...), nil
}

// Admitted reports whether addr has an admission row (a genesis submitter).
func (k Keeper) Admitted(ctx context.Context, addr []byte) bool {
	if len(addr) != 20 {
		return false
	}
	return sdk.UnwrapSDKContext(ctx).KVStore(k.storeKey).Has(AdmissionKey(addr))
}
