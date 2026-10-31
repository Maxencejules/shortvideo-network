package authority

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

const (
	// ModuleName is the only module that may return validator updates.
	ModuleName = "authority"

	stateKey byte = 0x01
	admitKey byte = 0x02
)

// Keeper stores the genesis authority set. Admitted accounts are an index
// over that state. Voting power is not written from anywhere except
// InitialUpdates during InitGenesis.
type Keeper struct {
	storeKey *storetypes.KVStoreKey
}

func NewKeeper(storeKey *storetypes.KVStoreKey) Keeper {
	return Keeper{storeKey: storeKey}
}

func (k Keeper) StoreKey() *storetypes.KVStoreKey {
	return k.storeKey
}

func (k Keeper) Init(ctx context.Context, state *authorityv1.GenesisState) error {
	if state == nil {
		return fmt.Errorf("authority genesis is nil")
	}
	raw, err := marshalState(state)
	if err != nil {
		return err
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	store := sdkCtx.KVStore(k.storeKey)
	store.Set([]byte{stateKey}, raw)
	for _, row := range state.GetAuthority().GetSubmitters() {
		addr := row.GetAddress()
		if len(addr) != 20 {
			return fmt.Errorf("submitter address length")
		}
		store.Set(admitKeyBytes(addr), []byte{byte(row.GetRole())})
	}
	return nil
}

func (k Keeper) Export(ctx context.Context) (*authorityv1.GenesisState, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	raw := sdkCtx.KVStore(k.storeKey).Get([]byte{stateKey})
	if raw == nil {
		return nil, fmt.Errorf("authority state is missing")
	}
	var state authorityv1.GenesisState
	if err := proto.Unmarshal(raw, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

// ConsensusBytes returns the bytes stored for consensus. Callers compare them
// to marshalState. ExportGenesis may encode the decoded state as protojson.
func (k Keeper) ConsensusBytes(ctx context.Context) ([]byte, error) {
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	raw := sdkCtx.KVStore(k.storeKey).Get([]byte{stateKey})
	if raw == nil {
		return nil, fmt.Errorf("authority state is missing")
	}
	return append([]byte(nil), raw...), nil
}

// marshalState is the only consensus encoding for authority state.
// protojson is not used here. Its output changes across builds.
func marshalState(state *authorityv1.GenesisState) ([]byte, error) {
	return proto.MarshalOptions{Deterministic: true}.Marshal(state)
}

// Admitted reports whether addr is an application account from genesis.
func (k Keeper) Admitted(ctx context.Context, addr []byte) bool {
	if len(addr) != 20 {
		return false
	}
	sdkCtx := sdk.UnwrapSDKContext(ctx)
	return sdkCtx.KVStore(k.storeKey).Has(admitKeyBytes(addr))
}

func admitKeyBytes(addr []byte) []byte {
	return append([]byte{admitKey}, addr...)
}
