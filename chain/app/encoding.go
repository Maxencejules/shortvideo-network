package app

import (
	"fmt"

	gogoproto "github.com/cosmos/gogoproto/proto"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/std"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	txsigning "github.com/cosmos/cosmos-sdk/x/tx/signing"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// Encoding carries the codecs used by the application and its transactions.
type Encoding struct {
	InterfaceRegistry codectypes.InterfaceRegistry
	Codec             codec.Codec
	TxConfig          client.TxConfig
	Amino             *codec.LegacyAmino
}

// MakeEncoding builds the codecs. Genesis construction uses it without a database.
func MakeEncoding() (Encoding, error) {
	chainaddress.Configure()
	options := txsigning.Options{
		AddressCodec:          addresscodec.NewBech32Codec(chainaddress.Account),
		ValidatorAddressCodec: addresscodec.NewBech32Codec(chainaddress.Validator),
	}
	for _, msg := range protoMessages() {
		options.DefineCustomGetSigners(protoreflect.FullName(proto.MessageName(msg)), signerBytes)
	}
	registry, err := codectypes.NewInterfaceRegistryWithOptions(codectypes.InterfaceRegistryOptions{
		ProtoFiles:     gogoproto.HybridResolver,
		SigningOptions: options,
	})
	if err != nil {
		return Encoding{}, err
	}
	std.RegisterInterfaces(registry)
	auth.AppModuleBasic{}.RegisterInterfaces(registry)
	registry.RegisterImplementations((*sdk.Msg)(nil), gogoMessages()...)

	amino := codec.NewLegacyAmino()
	std.RegisterLegacyAminoCodec(amino)
	cdc := codec.NewProtoCodec(registry)
	txConfig, err := authtx.NewTxConfigWithOptions(cdc, authtx.ConfigOptions{
		EnabledSignModes: []signingtypes.SignMode{signingtypes.SignMode_SIGN_MODE_DIRECT},
	})
	if err != nil {
		return Encoding{}, err
	}
	return Encoding{
		InterfaceRegistry: registry,
		Codec:             cdc,
		TxConfig:          txConfig,
		Amino:             amino,
	}, nil
}

func signerBytes(msg proto.Message) ([][]byte, error) {
	field := msg.ProtoReflect().Descriptor().Fields().ByName("signer")
	if field == nil {
		return nil, fmt.Errorf("message %s has no signer", proto.MessageName(msg))
	}
	raw := append([]byte(nil), msg.ProtoReflect().Get(field).Bytes()...)
	if len(raw) != 20 {
		return nil, fmt.Errorf("signer must be 20 bytes")
	}
	return [][]byte{raw}, nil
}

func protoMessages() []proto.Message {
	return []proto.Message{
		&authorityv1.MsgProposeChange{},
		&authorityv1.MsgApproveChange{},
		&authorityv1.MsgExecuteChange{},
		&providersv1.MsgRegisterProvider{},
		&providersv1.MsgUpdateProvider{},
		&providersv1.MsgRotateProviderKey{},
		&providersv1.MsgDrainProvider{},
		&contentv1.MsgRegisterContent{},
		&contentv1.MsgWithdrawContent{},
		&leasesv1.MsgOfferLease{},
		&leasesv1.MsgAcceptLease{},
		&leasesv1.MsgRenewLease{},
		&leasesv1.MsgEndLease{},
		&evidencev1.MsgSubmitAuditBatch{},
	}
}

func gogoMessages() []gogoproto.Message {
	out := make([]gogoproto.Message, 0, len(protoMessages()))
	for _, msg := range protoMessages() {
		gogo, ok := any(msg).(gogoproto.Message)
		if !ok {
			panic(fmt.Sprintf("%T does not implement the SDK message interface", msg))
		}
		out = append(out, gogo)
	}
	return out
}
