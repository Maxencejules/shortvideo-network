package authority

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	abci "github.com/cometbft/cometbft/abci/types"
	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"
	"google.golang.org/protobuf/encoding/protojson"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// ConsensusVersion is the store layout version (layout key 0x00). Version 2
// (goal 10) added the layout key, retired the governed-value prefix 0x13
// (governed values are x/policy state) and added the schema version 2
// fields to every proposal record. No migration from version 1 is
// registered: homes from goal 09 or earlier are re-initialized (DR-10-04,
// interim).
const ConsensusVersion uint64 = 2

// Order records InitGenesis calls for tests. Production leaves it nil.
type Order struct {
	Names []string
}

func (o *Order) Record(name string) {
	if o == nil {
		return
	}
	o.Names = append(o.Names, name)
}

// AppModule owns validator updates. InitGenesis returns the genesis set.
// EndBlock returns a changeset when a scheduled change is due.
type AppModule struct {
	keeper Keeper
	order  *Order
}

func NewAppModule(keeper Keeper, order *Order) AppModule {
	return AppModule{keeper: keeper, order: order}
}

func (AppModule) IsOnePerModuleType() {}

func (AppModule) IsAppModule() {}

func (AppModule) Name() string { return ModuleName }

func (AppModule) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

func (AppModule) RegisterInterfaces(codectypes.InterfaceRegistry) {}

func (AppModule) RegisterGRPCGatewayRoutes(client.Context, *gwruntime.ServeMux) {}

func (AppModule) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	return json.RawMessage(`{}`)
}

// ValidateGenesis runs ValidateGenesisState without a chain start point; the
// app InitChainer checks the start point.
func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var state authorityv1.GenesisState
	if err := protojson.Unmarshal(bz, &state); err != nil {
		return fmt.Errorf("authority genesis: %w", err)
	}
	if err := ValidateGenesisState(&state, nil); err != nil {
		return err
	}
	if _, err := InitialUpdates(state.GetAuthority().GetValidators()); err != nil {
		return err
	}
	return nil
}

func (am AppModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) []abci.ValidatorUpdate {
	am.order.Record(ModuleName)
	var state authorityv1.GenesisState
	if err := protojson.Unmarshal(bz, &state); err != nil {
		panic(err)
	}
	if err := am.keeper.Init(ctx, &state); err != nil {
		panic(err)
	}
	updates, err := InitialUpdates(state.GetAuthority().GetValidators())
	if err != nil {
		panic(err)
	}
	return updates
}

// ExportGenesis panics with the ExportState error (wrapping ErrExportRefused)
// while the store holds state the genesis proto cannot carry. The SDK
// interface has no error result, so a caller that needs the error calls
// Keeper.Export before the module manager export.
func (am AppModule) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	state, err := am.keeper.Export(ctx)
	if err != nil {
		panic(err)
	}
	raw, err := protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true}.Marshal(state)
	if err != nil {
		panic(err)
	}
	return raw
}

// EndBlock returns the authority changeset. No other module implements
// HasABCIEndBlock, so nothing else can overwrite the set.
func (am AppModule) EndBlock(ctx context.Context) ([]abci.ValidatorUpdate, error) {
	return am.keeper.EndBlock(ctx)
}

// ConsensusVersion is the store layout version.
func (AppModule) ConsensusVersion() uint64 { return ConsensusVersion }

// RegisterServices registers svn.authority.v1.Query (QueryServiceDesc) on the
// gRPC query router and the store migrations. svn.authority.v1.Msg is not
// registered here: the app registers it on the message router with its own
// service description (chain/app registerAuthority).
func (am AppModule) RegisterServices(cfg module.Configurator) {
	cfg.QueryServer().RegisterService(QueryServiceDesc(am.keeper), am.keeper)
	if err := registerMigrations(cfg); err != nil {
		panic(err)
	}
}

// migrations maps a from-version to the in-place migration to from+1.
// Version 2 is the first layout with a layout key, so the table is empty.
// Version 1 has no step on purpose: RunMigrations from it fails, and the
// app refuses such a home at load. A layout change bumps ConsensusVersion
// and adds the entry here; a test checks the table covers 2 ..
// ConsensusVersion-1 and nothing else.
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
			return fmt.Errorf("authority: register migration from %d: %w", from, err)
		}
	}
	return nil
}

var (
	_ module.AppModule           = AppModule{}
	_ module.HasABCIGenesis      = AppModule{}
	_ module.HasABCIEndBlock     = AppModule{}
	_ module.HasServices         = AppModule{}
	_ module.HasConsensusVersion = AppModule{}
	_ appmodule.AppModule        = AppModule{}
)
