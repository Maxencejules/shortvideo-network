package providers

import (
	"encoding/json"
	"fmt"
	"sort"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// InitRecorder records InitGenesis calls for tests. *authority.Order
// satisfies it. A nil recorder records nothing.
type InitRecorder interface {
	Record(name string)
}

// AppModule is x/providers. It has genesis, services, and a consensus
// version. It has no BeginBlock or EndBlock and returns no validator
// updates: x/authority stays the only validator-set writer.
type AppModule struct {
	keeper Keeper
	order  InitRecorder
}

// NewAppModule returns the module for keeper.
func NewAppModule(keeper Keeper, order InitRecorder) AppModule {
	return AppModule{keeper: keeper, order: order}
}

func (AppModule) IsOnePerModuleType() {}

func (AppModule) IsAppModule() {}

func (AppModule) Name() string { return ModuleName }

func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

// RegisterInterfaces is empty: the app's encoding registers the
// svn.providers.v1 messages with every other protocol message.
func (AppModule) RegisterInterfaces(codectypes.InterfaceRegistry) {}

// RegisterGRPCGatewayRoutes is empty: the app serves no REST API.
func (AppModule) RegisterGRPCGatewayRoutes(client.Context, *gwruntime.ServeMux) {}

// DefaultGenesis is the empty provider list.
func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	raw, err := MarshalGenesis(&providersv1.GenesisState{})
	if err != nil {
		panic(err)
	}
	return raw
}

// ValidateGenesis parses the document strictly and runs ValidateGenesis.
func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, raw json.RawMessage) error {
	state, err := ParseGenesis(raw)
	if err != nil {
		return err
	}
	return ValidateGenesis(state)
}

// InitGenesis records the call, then imports the document. An invalid
// document panics, which stops InitChain.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, raw json.RawMessage) {
	if am.order != nil {
		am.order.Record(ModuleName)
	}
	state, err := ParseGenesis(raw)
	if err != nil {
		panic(err)
	}
	if err := am.keeper.InitGenesis(ctx, state); err != nil {
		panic(err)
	}
}

// ExportGenesis writes every provider in id byte order.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	state, err := am.keeper.ExportGenesis(ctx)
	if err != nil {
		panic(err)
	}
	raw, err := MarshalGenesis(state)
	if err != nil {
		panic(err)
	}
	return raw
}

// RegisterServices registers svn.providers.v1.Msg on the message router,
// svn.providers.v1.Query on the gRPC query router, and the store
// migrations. The protocol Msg service has no cosmos.msg.v1.service option,
// so the configurator's own RegisterService would route it to the query
// router; this method uses the two routers directly.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	cfg.MsgServer().RegisterService(MsgServiceDesc(am.keeper), am.keeper)
	cfg.QueryServer().RegisterService(QueryServiceDesc(am.keeper), am.keeper)
	if err := registerMigrations(cfg); err != nil {
		panic(err)
	}
}

// ConsensusVersion is the store layout version.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }

// migrations maps a from-version to the in-place migration to from+1.
// Version 2 is the first registry layout, so the table is empty. Version 1
// is the scaffold layout and has no step on purpose: RunMigrations from it
// fails. A layout change bumps ConsensusVersion and adds the entry here; a
// test checks the table covers 2 .. ConsensusVersion-1 and nothing else.
var migrations = map[uint64]module.MigrationHandler{}

// registerMigrations registers every migration in from-version order.
func registerMigrations(cfg module.Configurator) error {
	versions := make([]uint64, 0, len(migrations))
	for from := range migrations {
		versions = append(versions, from)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i] < versions[j] })
	for _, from := range versions {
		if err := cfg.RegisterMigration(ModuleName, from, migrations[from]); err != nil {
			return fmt.Errorf("providers: register migration from %d: %w", from, err)
		}
	}
	return nil
}

var (
	_ module.AppModule           = AppModule{}
	_ module.HasGenesis          = AppModule{}
	_ module.HasServices         = AppModule{}
	_ module.HasConsensusVersion = AppModule{}
	_ appmodule.AppModule        = AppModule{}
)
