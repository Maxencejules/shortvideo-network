package chainclient

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"

	"github.com/cosmos/cosmos-sdk/client/tx"
	addresscodec "github.com/cosmos/cosmos-sdk/codec/address"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	chainaddress "github.com/Maxencejules/shortvideo-network/chain/address"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// TxInput is what the outer transaction needs besides the message and key.
type TxInput struct {
	ChainID       string
	AccountNumber uint64
	Sequence      uint64
	// Gas is the declared gas limit, 1 to 20,000,000 (the chain's bounds).
	Gas uint64
}

// BuildTx builds and signs the outer transaction carrying msg:
// SIGN_MODE_DIRECT (the only mode the app enables), the app's own
// encoding, no fee, no memo, and the key's address as the only signer,
// which must be msg's signer. It returns the transaction bytes and their
// CometBFT hash (SHA-256 of the bytes).
func (c *Client) BuildTx(msg *evidencev1.MsgSubmitAuditBatch, key cryptotypes.PrivKey, in TxInput) ([]byte, [32]byte, error) {
	var none [32]byte
	if in.Gas < genesis.MinGasPerTx || in.Gas > genesis.MaxGasPerTx {
		return nil, none, fmt.Errorf("chainclient: gas limit must be %d to %d", genesis.MinGasPerTx, genesis.MaxGasPerTx)
	}
	if in.ChainID == "" {
		return nil, none, errors.New("chainclient: chain id is empty")
	}
	pub := key.PubKey()
	if string(pub.Address()) != string(msg.GetSigner()) {
		return nil, none, errors.New("chainclient: key address is not the message signer")
	}
	bech, err := addresscodec.NewBech32Codec(chainaddress.Account).BytesToString(pub.Address())
	if err != nil {
		return nil, none, fmt.Errorf("chainclient: address: %w", err)
	}
	cfg := c.enc.TxConfig
	builder := cfg.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		return nil, none, fmt.Errorf("chainclient: set message: %w", err)
	}
	builder.SetGasLimit(in.Gas)
	placeholder := signingtypes.SignatureV2{
		PubKey:   pub,
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
		Sequence: in.Sequence,
	}
	if err := builder.SetSignatures(placeholder); err != nil {
		return nil, none, fmt.Errorf("chainclient: signer info: %w", err)
	}
	signer := authsigning.SignerData{
		Address: bech, ChainID: in.ChainID, AccountNumber: in.AccountNumber, Sequence: in.Sequence, PubKey: pub,
	}
	sig, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, key, cfg, in.Sequence)
	if err != nil {
		return nil, none, fmt.Errorf("chainclient: sign: %w", err)
	}
	if err := builder.SetSignatures(sig); err != nil {
		return nil, none, fmt.Errorf("chainclient: set signature: %w", err)
	}
	raw, err := cfg.TxEncoder()(builder.GetTx())
	if err != nil {
		return nil, none, fmt.Errorf("chainclient: encode: %w", err)
	}
	if uint64(len(raw)) > genesis.MaxTxBytes {
		return nil, none, fmt.Errorf("chainclient: transaction of %d bytes above %d", len(raw), genesis.MaxTxBytes)
	}
	return raw, sha256.Sum256(raw), nil
}

// TxMessage decodes transaction bytes with the app's own decoder and
// returns the audit batch they carry, which must be their only message.
func (c *Client) TxMessage(raw []byte) (*evidencev1.MsgSubmitAuditBatch, error) {
	decoded, err := c.enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		return nil, fmt.Errorf("chainclient: decode transaction: %w", err)
	}
	msgs := decoded.GetMsgs()
	if len(msgs) != 1 {
		return nil, fmt.Errorf("chainclient: transaction carries %d messages", len(msgs))
	}
	msg, ok := msgs[0].(*evidencev1.MsgSubmitAuditBatch)
	if !ok {
		return nil, fmt.Errorf("chainclient: transaction carries %T", msgs[0])
	}
	return msg, nil
}
