package sign

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/cosmos/cosmos-sdk/client/tx"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// Input is what the outer signature binds besides the message.
type Input struct {
	ChainID       string
	AccountNumber uint64
	Sequence      uint64
	TimeoutHeight uint64
	Gas           uint64
}

var (
	encOnce sync.Once
	encVal  app.Encoding
	encErr  error
)

func encoding() (app.Encoding, error) {
	encOnce.Do(func() {
		encVal, encErr = app.MakeEncoding()
	})
	return encVal, encErr
}

// BuildTx signs MsgAcceptLease with the provider key. The mode is
// SIGN_MODE_DIRECT. There is no fee and no memo. The signer address is the
// key's address and must equal msg.Signer. Broadcasting the bytes is not a commit.
func BuildTx(msg *leasesv1.MsgAcceptLease, key cryptotypes.PrivKey, in Input) ([]byte, error) {
	if in.Gas < genesis.MinGasPerTx || in.Gas > genesis.MaxGasPerTx {
		return nil, fmt.Errorf("sign: gas out of range")
	}
	if in.ChainID == "" || in.TimeoutHeight == 0 {
		return nil, fmt.Errorf("sign: chain id or timeout height is empty")
	}
	if !bytes.Equal(key.PubKey().Address(), msg.GetSigner()) {
		return nil, fmt.Errorf("sign: key address is not the message signer")
	}
	enc, err := encoding()
	if err != nil {
		return nil, err
	}
	bech, err := addresscodec.NewBech32Codec(chainaddress.Account).BytesToString(key.PubKey().Address())
	if err != nil {
		return nil, err
	}
	cfg := enc.TxConfig
	builder := cfg.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		return nil, err
	}
	builder.SetGasLimit(in.Gas)
	builder.SetFeeAmount(sdk.NewCoins())
	builder.SetTimeoutHeight(in.TimeoutHeight)
	placeholder := signingtypes.SignatureV2{
		PubKey:   key.PubKey(),
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
		Sequence: in.Sequence,
	}
	if err := builder.SetSignatures(placeholder); err != nil {
		return nil, err
	}
	signer := authsigning.SignerData{
		Address: bech, ChainID: in.ChainID, AccountNumber: in.AccountNumber,
		Sequence: in.Sequence, PubKey: key.PubKey(),
	}
	sig, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, key, cfg, in.Sequence)
	if err != nil {
		return nil, err
	}
	if err := builder.SetSignatures(sig); err != nil {
		return nil, err
	}
	raw, err := cfg.TxEncoder()(builder.GetTx())
	if err != nil {
		return nil, err
	}
	if uint64(len(raw)) > genesis.MaxTxBytes {
		return nil, fmt.Errorf("sign: transaction is %d bytes", len(raw))
	}
	return raw, nil
}

// AcceptTx is a decoded provider acceptance transaction.
type AcceptTx struct {
	Msg    *leasesv1.MsgAcceptLease
	PubKey []byte
}

// DecodeAcceptTx decodes a transaction built by BuildTx.
func DecodeAcceptTx(raw []byte) (AcceptTx, error) {
	enc, err := encoding()
	if err != nil {
		return AcceptTx{}, err
	}
	decoded, err := enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		return AcceptTx{}, err
	}
	msgs := decoded.GetMsgs()
	if len(msgs) != 1 {
		return AcceptTx{}, fmt.Errorf("sign: %d messages", len(msgs))
	}
	msg, ok := msgs[0].(*leasesv1.MsgAcceptLease)
	if !ok {
		return AcceptTx{}, fmt.Errorf("sign: %T", msgs[0])
	}
	stx, ok := decoded.(authsigning.Tx)
	if !ok {
		return AcceptTx{}, fmt.Errorf("sign: transaction has no signatures")
	}
	sigs, err := stx.GetSignaturesV2()
	if err != nil || len(sigs) != 1 || sigs[0].PubKey == nil {
		return AcceptTx{}, fmt.Errorf("sign: signature set")
	}
	return AcceptTx{Msg: msg, PubKey: sigs[0].PubKey.Bytes()}, nil
}
