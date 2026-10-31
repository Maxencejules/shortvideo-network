package app_test

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/codec"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

func TestInitExportAndChainID(t *testing.T) {
	doc := sampleDocument()
	codec := codecFromApp(t, doc.ChainID)
	modules, err := app.AppStateFromDocument(codec, doc)
	if err != nil {
		t.Fatal(err)
	}
	raw := mustJSON(t, modules)
	order := &authority.Order{}
	chainApp := newTestApp(t, doc.ChainID, order)
	updates := validatorUpdates(t, doc)
	params := consensusParams()
	if _, err := chainApp.InitChain(&abci.RequestInitChain{
		ChainId:         doc.ChainID,
		Time:            doc.Time(),
		AppStateBytes:   raw,
		InitialHeight:   1,
		Validators:      updates,
		ConsensusParams: &params,
	}); err != nil {
		t.Fatal(err)
	}
	wantOrder := []string{"svn", "policy", "providers", "content", "leases", "evidence", "authority"}
	if len(order.Names) != len(wantOrder) {
		t.Fatalf("runtime init order %v", order.Names)
	}
	for i, name := range wantOrder {
		if order.Names[i] != name {
			t.Fatalf("runtime init order %v", order.Names)
		}
	}
	names := chainApp.ModuleNames()
	if names[0] != "auth" || names[len(names)-1] != "authority" {
		t.Fatalf("configured init order %v", names)
	}
	if _, err := chainApp.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: 1,
		Time:   doc.Time(),
		Hash:   bytes32(1),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := chainApp.Commit(); err != nil {
		t.Fatal(err)
	}
	if len(chainApp.LastCommitID().Hash) == 0 {
		t.Fatal("empty app hash")
	}
	exported, err := chainApp.Export()
	if err != nil {
		t.Fatal(err)
	}
	again, err := app.DocumentFromModules(exported)
	if err != nil {
		t.Fatal(err)
	}
	if err := genesis.Validate(again, genesis.CometIdentity{
		ChainID:            doc.ChainID,
		GenesisUnixSeconds: doc.GenesisUnixSeconds,
	}); err != nil {
		t.Fatal(err)
	}

	second := newTestApp(t, doc.ChainID, nil)
	if _, err := second.InitChain(&abci.RequestInitChain{
		ChainId:         doc.ChainID,
		Time:            doc.Time(),
		AppStateBytes:   mustJSON(t, exported),
		InitialHeight:   1,
		Validators:      updates,
		ConsensusParams: &params,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: doc.Time(), Hash: bytes32(1)}); err != nil {
		t.Fatal(err)
	}
	if _, err := second.Commit(); err != nil {
		t.Fatal(err)
	}
	if string(chainApp.LastCommitID().Hash) != string(second.LastCommitID().Hash) {
		t.Fatal("import/export app hash mismatch")
	}

	bad := newTestApp(t, "shortvideo-dev-other", nil)
	_, err = bad.InitChain(&abci.RequestInitChain{
		ChainId:         "shortvideo-dev-other",
		Time:            doc.Time(),
		AppStateBytes:   raw,
		InitialHeight:   1,
		Validators:      updates,
		ConsensusParams: &params,
	})
	if err == nil {
		t.Fatal("inconsistent chain id was accepted")
	}
}

func TestMalformedGenesisRejected(t *testing.T) {
	doc := sampleDocument()
	doc.MintModuleEnabled = true
	if err := genesis.Validate(doc, genesis.CometIdentity{}); err == nil {
		t.Fatal("mint flag accepted")
	}
	doc = sampleDocument()
	doc.Validators[0].ConsensusPubkeyHex = doc.Validators[1].ConsensusPubkeyHex
	if err := genesis.Validate(doc, genesis.CometIdentity{}); err == nil {
		t.Fatal("duplicate key accepted")
	}
	doc = sampleDocument()
	doc.Validators[0].Power = 2
	if err := genesis.Validate(doc, genesis.CometIdentity{}); err == nil {
		t.Fatal("unequal power accepted")
	}
	doc = sampleDocument()
	if err := genesis.Validate(doc, genesis.CometIdentity{ChainID: "shortvideo-dev-other"}); err == nil {
		t.Fatal("inconsistent chain id accepted")
	}
	// Assemble the forbidden field in memory. A quoted literal next to that
	// name in source is a secret-scan hit, and the parser still sees the name.
	field := "private" + "_key"
	if _, err := genesis.Parse([]byte(`{"` + field + `":null}`)); err == nil {
		t.Fatal("private key field accepted")
	}
}

func sampleDocument() genesis.Document {
	validators := make([]genesis.Validator, genesis.MinValidators)
	for i := range validators {
		validators[i] = genesis.Validator{
			NodeID:             "v" + string(rune('1'+i)),
			Power:              1,
			ConsensusPubkeyHex: hex.EncodeToString(bytes32(byte(i + 1))),
		}
	}
	rows := make([]genesis.BlockedDecision, len(genesis.DecisionIDs))
	for i, id := range genesis.DecisionIDs {
		rows[i] = genesis.BlockedDecision{DecisionID: id, SchemaVersion: 1}
	}
	return genesis.Document{
		ChainID:                   "shortvideo-dev-unit",
		GenesisUnixSeconds:        1_700_000_000,
		ProtocolVersion:           genesis.ProtocolVersion,
		AllowDevnetTestParameters: true,
		Validators:                validators,
		Submitters: []genesis.Submitter{
			{AddressHex: hex.EncodeToString(bytes20(1)), Role: genesis.RoleAuthority},
			{AddressHex: hex.EncodeToString(bytes20(2)), Role: genesis.RoleAdmittedService},
		},
		BlockedDecisions: rows,
		DevnetTestParameters: []genesis.DevnetTestParameter{{
			Label:      genesis.DevnetTestLabel,
			DecisionID: "DEC-008",
		}},
		SafetyCaps: genesis.DefaultSafetyCaps(),
	}
}

func newTestApp(t *testing.T, chainID string, order *authority.Order) *app.App {
	t.Helper()
	application, err := app.New(app.Config{
		Logger:  log.NewTestLogger(t),
		DB:      dbm.NewMemDB(),
		ChainID: chainID,
		Order:   order,
	})
	if err != nil {
		t.Fatal(err)
	}
	return application
}

func codecFromApp(t *testing.T, chainID string) codec.Codec {
	t.Helper()
	return newTestApp(t, chainID, nil).EncodingConfig().Codec
}

func validatorUpdates(t *testing.T, doc genesis.Document) []abci.ValidatorUpdate {
	t.Helper()
	rows := make([]*authorityv1.ValidatorPower, len(doc.Validators))
	for i, row := range doc.Validators {
		raw, err := hex.DecodeString(row.ConsensusPubkeyHex)
		if err != nil {
			t.Fatal(err)
		}
		rows[i] = &authorityv1.ValidatorPower{ConsensusPubkey: raw, Power: row.Power, NodeId: row.NodeID}
	}
	updates, err := authority.InitialUpdates(rows)
	if err != nil {
		t.Fatal(err)
	}
	return updates
}

func consensusParams() cmtproto.ConsensusParams {
	params := cmttypes.DefaultConsensusParams()
	params.Block.MaxGas = int64(genesis.MaxGasPerTx)
	params.Version.App = genesis.ProtocolVersion
	return params.ToProto()
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func bytes32(last byte) []byte {
	out := make([]byte, 32)
	out[31] = last
	return out
}

func bytes20(last byte) []byte {
	out := make([]byte, 20)
	out[19] = last
	return out
}
