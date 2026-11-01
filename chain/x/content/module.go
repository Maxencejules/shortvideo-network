package content

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

	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
)

// AppModule is the x/content module. It has no BeginBlock or EndBlock and
// returns no validator updates; x/authority stays the only writer of the
// validator set.
type AppModule struct {
	keeper Keeper
	order  InitRecorder
}

// NewAppModule returns the module. order may be nil.
func NewAppModule(keeper Keeper, order InitRecorder) AppModule {
	return AppModule{keeper: keeper, order: order}
}

func (AppModule) IsOnePerModuleType() {}

func (AppModule) IsAppModule() {}

func (AppModule) Name() string { return ModuleName }

func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

// RegisterInterfaces registers nothing: the application registers every
// protocol message with the interface registry in one place.
func (AppModule) RegisterInterfaces(codectypes.InterfaceRegistry) {}

// RegisterGRPCGatewayRoutes registers nothing: the application serves no
// REST gateway. Queries are served by ABCI query and gRPC.
func (AppModule) RegisterGRPCGatewayRoutes(client.Context, *gwruntime.ServeMux) {}

// DefaultGenesis is the empty catalogue.
func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	raw, err := MarshalGenesis(&contentv1.GenesisState{})
	if err != nil {
		panic(err)
	}
	return raw
}

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	state, err := ParseGenesis(bz)
	if err != nil {
		return err
	}
	return ValidateGenesis(state)
}

// InitGenesis records the call, then validates and writes the catalogue.
// Invalid genesis panics, which stops InitChain.
func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	if am.order != nil {
		am.order.Record(ModuleName)
	}
	state, err := ParseGenesis(bz)
	if err != nil {
		panic(err)
	}
	if err := am.keeper.InitGenesis(ctx, state); err != nil {
		panic(err)
	}
}

func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	raw, err := MarshalGenesis(am.keeper.ExportGenesis(ctx))
	if err != nil {
		panic(err)
	}
	return raw
}

// RegisterServices registers the Msg service on the message router, the
// Query service on the query router, and the in-place migrations. The
// configurator's own RegisterService routes by the cosmos.msg.v1.service
// option, which the frozen v0 protos do not carry, so each service goes to
// its router explicitly. A registration failure panics at startup.
func (am AppModule) RegisterServices(cfg module.Configurator) {
	cfg.MsgServer().RegisterService(MsgServiceDesc(am.keeper), am.keeper)
	cfg.QueryServer().RegisterService(QueryServiceDesc(am.keeper), am.keeper)
	if err := registerMigrations(cfg, am.migrationSteps()); err != nil {
		panic(err)
	}
}

// ConsensusVersion is 1, the first state version.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }

// migrationSteps maps a from-version to the in-place store migration that
// moves state to from+1. v1 is the first version, so there is no step.
// A ConsensusVersion bump must add the step for the old version;
// TestMigrationStepsCoverConsensusVersion fails until it does.
func (am AppModule) migrationSteps() map[uint64]module.MigrationHandler {
	return map[uint64]module.MigrationHandler{}
}

// registerMigrations registers every step in from-version order.
func registerMigrations(cfg module.Configurator, steps map[uint64]module.MigrationHandler) error {
	froms := make([]uint64, 0, len(steps))
	for from := range steps {
		froms = append(froms, from)
	}
	sort.Slice(froms, func(i, j int) bool { return froms[i] < froms[j] })
	for _, from := range froms {
		if steps[from] == nil {
			return fmt.Errorf("content: nil migration from version %d", from)
		}
		if err := cfg.RegisterMigration(ModuleName, from, steps[from]); err != nil {
			return fmt.Errorf("content: register migration from version %d: %w", from, err)
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
