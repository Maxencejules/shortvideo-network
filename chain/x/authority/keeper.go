package authority

import (
	"context"
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// ModuleName is the only module that may return validator updates.
const ModuleName = "authority"

// Keeper stores authority state in the per-object layout of store.go.
// InitGenesis writes the genesis set. EndBlock writes a scheduled power or
// key change when its activation point is due. No other module returns
// validator updates.
//
// providers and content are the council registry hooks (WithRegistry);
// policy is x/policy, the store of governed values and upgrade plans
// (WithPolicy); accounts creates the account of a new council seat key
// (WithAccounts). peers joins the business-id ledgers of the other modules
// (WithPeers). All are wiring, not state: a Keeper value without them still
// reads and writes authority state, executes the payloads of a missing hook
// as MALFORMED, and refuses Query/Governance without a policy keeper.
type Keeper struct {
	storeKey  *storetypes.KVStoreKey
	providers ProviderAdmission
	content   ContentRemoval
	policy    PolicyKeeper
	accounts  AccountKeeper
	peers     *msgkit.Peers
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

// Admission is how the ante admits a transaction signer (DR-06-06).
type Admission int

const (
	// NotAdmitted: the address has no admission row and is not the address
	// of a council seat key. The ante refuses it (ERROR_CODE_UNAUTHORIZED).
	NotAdmitted Admission = iota
	// AdmittedSubmitter: the address has an admission row (0x02), so it is
	// a genesis submitter with a role.
	AdmittedSubmitter
	// AdmittedSeat: the address is the account of a council seat key in the
	// config (0x01) and has no admission row. It has no submitter role. The
	// ante lets it sign only council messages (propose and approve).
	AdmittedSeat
)

// Admission reports how the ante admits addr. See AdmissionOf.
func (k Keeper) Admission(ctx context.Context, addr []byte) Admission {
	return AdmissionOf(sdk.UnwrapSDKContext(ctx).KVStore(k.storeKey), addr)
}
