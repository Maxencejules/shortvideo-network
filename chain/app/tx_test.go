package app_test

import (
	"context"
	"encoding/hex"
	"strings"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	gogoproto "github.com/cosmos/gogoproto/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

func TestMessagesAreRejected(t *testing.T) {
	serviceKey := sdksecp.GenPrivKey()
	outsider := sdksecp.GenPrivKey()
	authorityKey := sdksecp.GenPrivKey()
	doc := sampleDocument()
	doc.Submitters = []genesis.Submitter{
		{AddressHex: hex.EncodeToString(authorityKey.PubKey().Address()), Role: genesis.RoleAuthority},
		{AddressHex: hex.EncodeToString(serviceKey.PubKey().Address()), Role: genesis.RoleAdmittedService},
	}
	chainApp := boot(t, doc)
	number := serviceAccountNumber(authorityKey.PubKey().Address(), serviceKey.PubKey().Address())
	msg := &providersv1.MsgRegisterProvider{Signer: serviceKey.PubKey().Address()}

	forbidden := chainApp.deliver(t, signedTx(t, chainApp.app, doc.ChainID, serviceKey, number, 200000, nil, msg))
	if forbidden.Code == 0 || !strings.Contains(forbidden.Log, "ERROR_CODE_FORBIDDEN") {
		t.Fatalf("admitted scaffolding message code %d log %s", forbidden.Code, forbidden.Log)
	}
	checked, err := chainApp.app.CheckTx(&abci.RequestCheckTx{Tx: unknownEnvelope(t)})
	if err != nil {
		t.Fatal(err)
	}
	if checked.Code == 0 {
		t.Fatal("unknown message passed check tx")
	}
	outsiderResult := chainApp.deliver(t, signedTx(t, chainApp.app, doc.ChainID, outsider, 0, 200000, nil, &providersv1.MsgRegisterProvider{
		Signer: outsider.PubKey().Address(),
	}))
	if outsiderResult.Code == 0 {
		t.Fatal("outsider was accepted")
	}
	feeResult := chainApp.deliver(t, signedTx(t, chainApp.app, doc.ChainID, serviceKey, number, 200000, sdk.NewCoins(sdk.NewInt64Coin("reject", 1)), msg))
	if feeResult.Code == 0 || !strings.Contains(feeResult.Log, "ERROR_CODE_FEE_REJECTED") {
		t.Fatalf("fee code %d log %s", feeResult.Code, feeResult.Log)
	}
	gasResult := chainApp.deliver(t, signedTx(t, chainApp.app, doc.ChainID, serviceKey, number, 0, nil, msg))
	if gasResult.Code == 0 || !strings.Contains(gasResult.Log, "ERROR_CODE_BOUNDS") {
		t.Fatalf("gas code %d log %s", gasResult.Code, gasResult.Log)
	}
	empty := chainApp.deliver(t, nil)
	if len(empty.Log) != 0 && empty.Code != 0 {
		t.Fatalf("empty block failed code %d", empty.Code)
	}
	if len(chainApp.last.ValidatorUpdates) != 0 {
		t.Fatalf("end block changed validator power: %v", chainApp.last.ValidatorUpdates)
	}
}

type running struct {
	app    *app.App
	params cmtproto.ConsensusParams
	when   time.Time
	height int64
	last   *abci.ResponseFinalizeBlock
}

func boot(t *testing.T, doc genesis.Document) *running {
	t.Helper()
	modules, err := app.AppStateFromDocument(codecFromApp(t, doc.ChainID), doc)
	if err != nil {
		t.Fatal(err)
	}
	chainApp := newTestApp(t, doc.ChainID, nil)
	params := consensusParams()
	if _, err := chainApp.InitChain(&abci.RequestInitChain{
		ChainId:         doc.ChainID,
		Time:            doc.Time(),
		AppStateBytes:   mustJSON(t, modules),
		InitialHeight:   1,
		Validators:      validatorUpdates(t, doc),
		ConsensusParams: &params,
	}); err != nil {
		t.Fatal(err)
	}
	return &running{app: chainApp, params: params, when: doc.Time()}
}

func (r *running) deliver(t *testing.T, raw []byte) *abci.ExecTxResult {
	t.Helper()
	r.height++
	var txs [][]byte
	if raw != nil {
		txs = [][]byte{raw}
	}
	res, err := r.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: r.height,
		Time:   r.when,
		Hash:   bytes32(byte(r.height)),
		Txs:    txs,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.app.Commit(); err != nil {
		t.Fatal(err)
	}
	r.last = res
	if raw == nil {
		return &abci.ExecTxResult{}
	}
	if len(res.TxResults) != 1 {
		t.Fatalf("tx results %d", len(res.TxResults))
	}
	return res.TxResults[0]
}

func serviceAccountNumber(authority, service []byte) uint64 {
	if hex.EncodeToString(authority) < hex.EncodeToString(service) {
		return 1
	}
	return 0
}

func signedTx(t *testing.T, chainApp *app.App, chainID string, key *sdksecp.PrivKey, number, gas uint64, fee sdk.Coins, msg sdk.Msg) []byte {
	t.Helper()
	builder := chainApp.EncodingConfig().TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		t.Fatal(err)
	}
	builder.SetGasLimit(gas)
	if fee != nil {
		builder.SetFeeAmount(fee)
	}
	signer := authsigning.SignerData{
		Address:       sdk.AccAddress(key.PubKey().Address()).String(),
		ChainID:       chainID,
		AccountNumber: number,
		Sequence:      0,
		PubKey:        key.PubKey(),
	}
	placeholder := signingtypes.SignatureV2{
		PubKey: key.PubKey(),
		Data: &signingtypes.SingleSignatureData{
			SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: 0,
	}
	if err := builder.SetSignatures(placeholder); err != nil {
		t.Fatal(err)
	}
	signature, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, key, chainApp.EncodingConfig().TxConfig, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.SetSignatures(signature); err != nil {
		t.Fatal(err)
	}
	raw, err := chainApp.EncodingConfig().TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func unknownEnvelope(t *testing.T) []byte {
	t.Helper()
	raw, err := gogoproto.Marshal(&txtypes.Tx{
		Body: &txtypes.TxBody{Messages: []*codectypes.Any{{
			TypeUrl: "/svn.not.v1.MsgMissing",
			Value:   []byte{0x0a},
		}}},
		AuthInfo: &txtypes.AuthInfo{Fee: &txtypes.Fee{GasLimit: 200000}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}
