package authority

import (
	"context"
	"encoding/binary"
	"fmt"

	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

const (
	// ModuleName is the only module that may return validator updates.
	ModuleName = "authority"

	stateKey byte = 0x01
	admitKey byte = 0x02
)

// Keeper stores authority state. InitGenesis writes the genesis set.
// EndBlock writes a scheduled power or key change when its activation
// point is due. No other module returns validator updates.
//
// providers and content are the council hooks (WithRegistry). peers joins
// the business-id ledgers of the other modules (WithPeers). Both are wiring,
// not state: a Keeper value without them still reads and writes authority
// state, and executes registry payloads as MALFORMED.
type Keeper struct {
	storeKey  *storetypes.KVStoreKey
	providers ProviderAdmission
	content   ContentRemoval
	peers     *msgkit.Peers
}

func NewKeeper(storeKey *storetypes.KVStoreKey) Keeper {
	return Keeper{storeKey: storeKey}
}

func (k Keeper) StoreKey() *storetypes.KVStoreKey {
	return k.storeKey
}

// ConsensusSnapshot is the state key and the side document, length-prefixed.
// Tests compare it with bytes.Equal. A nil side document is a zero length.
func (k Keeper) ConsensusSnapshot(ctx sdk.Context) []byte {
	store := ctx.KVStore(k.storeKey)
	state := store.Get([]byte{stateKey})
	side := store.Get([]byte{sideKey})
	out := make([]byte, 8+len(state)+len(side))
	binary.BigEndian.PutUint32(out[0:4], uint32(len(state)))
	binary.BigEndian.PutUint32(out[4:8], uint32(len(side)))
	copy(out[8:], state)
	copy(out[8+len(state):], side)
	return out
}

// InstallMismatchedRemoval writes a due removal whose consensus key is not
// the key that validator holds. It is not a message and not an RPC. The
// app halt test uses it. EndBlock must refuse to commit that block.
func (k Keeper) InstallMismatchedRemoval(ctx sdk.Context) error {
	st, err := k.load(ctx)
	if err != nil {
		return err
	}
	if len(st.Validators) == 0 {
		return fmt.Errorf("no validator")
	}
	current := st.Validators[0]
	wrong := make([]byte, len(current.GetConsensusPubkey()))
	copy(wrong, current.GetConsensusPubkey())
	wrong[0] ^= 0xff
	st.Pending = append(st.Pending, &authorityv1.PendingValidatorPower{
		ProposalId:      "commit-bad-key",
		NodeId:          current.GetNodeId(),
		ConsensusPubkey: wrong,
		Power:           0,
		Activation:      &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: 1}},
	})
	return k.save(ctx, st)
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
