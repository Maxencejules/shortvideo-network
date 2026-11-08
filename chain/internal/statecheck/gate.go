package statecheck

import (
	"errors"
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/gogoproto/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
)

// layoutStores are the stores that keep a layout version key, as the app's
// load gate reads them (chain/app app.go layoutStores).
var layoutStores = []string{
	app.ModulePolicy,
	app.ModuleProviders,
	app.ModuleContent,
	app.ModuleLeases,
	app.ModuleEvidence,
	authority.ModuleName,
}

// consensusParamKey is the svn store key of the consensus params
// (chain/app params.go): the CometBFT ConsensusParams proto, which holds
// Version.App.
var consensusParamKey = []byte{0x03}

// readStoredState reads the state the load gate decides on, as the app's
// upgrade runner reads it (upgrade.Runner.StoredState over the x/policy
// keeper, the svn consensus params row, the layout keys of layoutStores,
// and the council plan source), with height as the last committed height.
// A keeper read of a committed row that does not decode panics; the panic
// is returned as an error, as the app's gate returns it as row 6.
func readStoredState(ctx sdk.Context, keys map[string]*storetypes.KVStoreKey, pol policy.Keeper, height uint64) (state upgrade.StoredState, err error) {
	defer func() {
		if p := recover(); p != nil {
			state, err = upgrade.StoredState{}, fmt.Errorf("committed state does not decode: %v", p)
		}
	}()
	state = upgrade.StoredState{
		LastHeight: height,
		AppVersion: pol.AppVersion(ctx),
		Modules:    pol.VersionMap(ctx),
		Layouts:    make(map[string]uint64, len(layoutStores)),
	}
	raw := ctx.KVStore(keys[app.ModuleSVN]).Get(consensusParamKey)
	if len(raw) == 0 {
		return upgrade.StoredState{}, errors.New("read consensus params: consensus params are not stored")
	}
	var params cmtproto.ConsensusParams
	if err := proto.Unmarshal(raw, &params); err != nil {
		return upgrade.StoredState{}, fmt.Errorf("read consensus params: %w", err)
	}
	if params.Version != nil {
		state.ConsensusAppVersion = params.Version.App
	}
	for _, name := range layoutStores {
		version, _, err := msgkit.ReadLayoutVersion(ctx.KVStore(keys[name]))
		if err != nil {
			return upgrade.StoredState{}, fmt.Errorf("read layout versions: %s store: %w", name, err)
		}
		state.Layouts[name] = version
	}
	plans, err := upgrade.NewCouncilPlans(planRows{keeper: pol})
	if err != nil {
		return upgrade.StoredState{}, err
	}
	plan, found, err := plans.Plan(ctx)
	if err != nil {
		return upgrade.StoredState{}, fmt.Errorf("read upgrade plan: %w", err)
	}
	if found {
		state.Plan = &plan
	}
	return state, nil
}

// planRows is upgrade.PlanRows over the x/policy keeper, as the app's
// policyPlanRows is: the plan row (0x06) and the applied rows (0x07).
type planRows struct {
	keeper policy.Keeper
}

func (r planRows) StoredPlan(ctx sdk.Context) (upgrade.Plan, bool) {
	plan, found := r.keeper.Plan(ctx)
	return upgrade.Plan{Name: plan.Name, Height: plan.Height, Digest: plan.Digest}, found
}

func (r planRows) UpgradeApplied(ctx sdk.Context, name string) bool {
	return r.keeper.UpgradeApplied(ctx, name)
}

// decide is the gate decision of binary on state. A state that could not
// be read is refused with row 6, as the app's load gate refuses it.
func decide(state upgrade.StoredState, stateErr error, binary upgrade.Binary) GateResult {
	if stateErr != nil {
		return GateResult{Row: 6, Refusal: stateErr.Error()}
	}
	decision, err := upgrade.Gate(state, binary)
	var refusal *upgrade.RefusalError
	switch {
	case errors.As(err, &refusal):
		return GateResult{Row: refusal.Row, Refusal: refusal.Msg}
	case err != nil:
		return GateResult{Row: 6, Refusal: err.Error()}
	}
	return GateResult{Row: decision.Row, Upgrade: decision.Upgrade}
}

// checkVersions is the state part of I5 and I6, independent of any binary:
// the stored app version (x/policy 0x04) is present and equals the stored
// ConsensusParams.Version.App, and every layout key is present and equals
// its stored version map entry. The x/policy checker checks the rest of
// the policy part (app version = 1 + applied upgrades); the gate compares
// the state with the binary.
func checkVersions(state upgrade.StoredState, stateErr error) error {
	if stateErr != nil {
		return stateErr
	}
	if state.AppVersion == 0 {
		return errors.New("I5: no stored app version")
	}
	if state.AppVersion != state.ConsensusAppVersion {
		return fmt.Errorf("I5: stored app version %d, stored consensus Version.App %d", state.AppVersion, state.ConsensusAppVersion)
	}
	for _, name := range layoutStores {
		stored, ok := state.Modules[name]
		switch layout := state.Layouts[name]; {
		case !ok:
			return fmt.Errorf("I6: the stored version map has no %s entry", name)
		case layout != stored:
			return fmt.Errorf("I6: %s layout key %d, stored version map entry %d", name, layout, stored)
		}
	}
	return nil
}
