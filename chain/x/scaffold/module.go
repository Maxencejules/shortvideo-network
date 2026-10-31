// Package scaffold holds the module slots that Goals 06-10 fill in.
//
// Each module stores its genesis document and rejects application messages.
// None of them returns validator updates. x/authority is the only writer.
package scaffold

import (
	"encoding/json"
	"fmt"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
)

const storeKeyByte = 0x01

// Module is one empty registry slot: svn header, providers, content, leases,
// evidence, or policy.
type Module struct {
	name     string
	storeKey *storetypes.KVStoreKey
	order    *authority.Order
	check    func(json.RawMessage) error
}

func New(name string, storeKey *storetypes.KVStoreKey, order *authority.Order, check func(json.RawMessage) error) Module {
	return Module{name: name, storeKey: storeKey, order: order, check: check}
}

func (Module) IsOnePerModuleType() {}

func (Module) IsAppModule() {}

func (m Module) Name() string { return m.name }

func (Module) RegisterLegacyAminoCodec(*codec.LegacyAmino) {}

func (Module) RegisterInterfaces(codectypes.InterfaceRegistry) {}

func (Module) RegisterGRPCGatewayRoutes(client.Context, *gwruntime.ServeMux) {}

func (Module) DefaultGenesis(codec.JSONCodec) json.RawMessage {
	return json.RawMessage(`{}`)
}

func (m Module) ValidateGenesis(_ codec.JSONCodec, _ client.TxEncodingConfig, bz json.RawMessage) error {
	if m.check == nil {
		return nil
	}
	if err := m.check(bz); err != nil {
		return fmt.Errorf("%s genesis: %w", m.name, err)
	}
	return nil
}

func (m Module) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, bz json.RawMessage) {
	m.order.Record(m.name)
	if m.check != nil {
		if err := m.check(bz); err != nil {
			panic(err)
		}
	}
	ctx.KVStore(m.storeKey).Set([]byte{storeKeyByte}, bz)
}

func (m Module) ExportGenesis(ctx sdk.Context, _ codec.JSONCodec) json.RawMessage {
	raw := ctx.KVStore(m.storeKey).Get([]byte{storeKeyByte})
	if raw == nil {
		panic(m.name + " genesis missing on export")
	}
	return append(json.RawMessage(nil), raw...)
}

func (Module) ConsensusVersion() uint64 { return 1 }

var (
	_ module.AppModule    = Module{}
	_ module.HasGenesis   = Module{}
	_ appmodule.AppModule = Module{}
)
