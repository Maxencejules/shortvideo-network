package authority

import (
	"context"
	"encoding/json"
	"fmt"

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

// AppModule is the authority scaffold. InitGenesis is the only call that
// returns validator updates. EndBlock returns none until Goal 06.
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

func (AppModule) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	var state authorityv1.GenesisState
	if err := protojson.Unmarshal(bz, &state); err != nil {
		return fmt.Errorf("authority genesis: %w", err)
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

func (AppModule) ConsensusVersion() uint64 { return 1 }

var (
	_ module.AppModule       = AppModule{}
	_ module.HasABCIGenesis  = AppModule{}
	_ module.HasABCIEndBlock = AppModule{}
	_ appmodule.AppModule    = AppModule{}
)
