package app

import (
	"context"
	"encoding/json"
	"fmt"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
)

// authConsensusVersion matches x/auth ConsensusVersion. The module package is
// not imported: its simulation constructor links bank, staking, gov, mint, and
// authz. This type is the account module svnd actually runs.
const authConsensusVersion uint64 = 7

// authModule stores accounts and sequences. It has no simulation hook and it
// does not construct validator updates.
type authModule struct {
	keeper authkeeper.AccountKeeper
}

func newAuthModule(keeper authkeeper.AccountKeeper) authModule {
	return authModule{keeper: keeper}
}

func (authModule) IsOnePerModuleType() {}

func (authModule) IsAppModule() {}

func (authModule) Name() string { return authtypes.ModuleName }

func (authModule) RegisterLegacyAminoCodec(cdc *codec.LegacyAmino) {
	authtypes.RegisterLegacyAminoCodec(cdc)
}

func (authModule) RegisterInterfaces(registry codectypes.InterfaceRegistry) {
	authtypes.RegisterInterfaces(registry)
}

func (authModule) RegisterGRPCGatewayRoutes(client.Context, *gwruntime.ServeMux) {}

func (authModule) DefaultGenesis(cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(authtypes.DefaultGenesisState())
}

func (authModule) ValidateGenesis(cdc codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var data authtypes.GenesisState
	if err := cdc.UnmarshalJSON(bz, &data); err != nil {
		return fmt.Errorf("auth genesis: %w", err)
	}
	return authtypes.ValidateGenesis(data)
}

func (m authModule) InitGenesis(ctx sdk.Context, cdc codec.JSONCodec, bz json.RawMessage) {
	var data authtypes.GenesisState
	cdc.MustUnmarshalJSON(bz, &data)
	m.keeper.InitGenesis(ctx, data)
}

func (m authModule) ExportGenesis(ctx sdk.Context, cdc codec.JSONCodec) json.RawMessage {
	return cdc.MustMarshalJSON(m.keeper.ExportGenesis(ctx))
}

func (m authModule) RegisterServices(cfg module.Configurator) {
	authtypes.RegisterMsgServer(cfg.MsgServer(), authkeeper.NewMsgServerImpl(m.keeper))
	authtypes.RegisterQueryServer(cfg.QueryServer(), authkeeper.NewQueryServer(m.keeper))
}

func (m authModule) PreBlock(ctx context.Context) (appmodule.ResponsePreBlock, error) {
	if m.keeper.UnorderedTransactionsEnabled() {
		if err := m.keeper.RemoveExpiredUnorderedNonces(sdk.UnwrapSDKContext(ctx)); err != nil {
			return nil, err
		}
	}
	return &sdk.ResponsePreBlock{ConsensusParamsChanged: false}, nil
}

func (authModule) ConsensusVersion() uint64 { return authConsensusVersion }

var (
	_ module.AppModule           = authModule{}
	_ module.HasGenesis          = authModule{}
	_ module.HasServices         = authModule{}
	_ module.HasConsensusVersion = authModule{}
	_ appmodule.AppModule        = authModule{}
	_ appmodule.HasPreBlocker    = authModule{}
)
