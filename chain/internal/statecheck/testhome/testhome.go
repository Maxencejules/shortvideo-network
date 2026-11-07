// Package testhome is test support for the state check: it builds real
// svnd node homes in process, with no network. New runs devnet.Init (the
// code of `svnd devnet init`) in a test directory and opens validator 0's
// application database (data/application.db, goleveldb) with app.New, as
// svnd start does; it then runs InitChain from the home's CometBFT genesis
// file the way the CometBFT handshake does, and commits blocks with
// FinalizeBlock and Commit, delivering transactions signed with the devnet
// account and council keys of the home. CometBFT itself does not run, so
// the home has no block store: only its application state is real.
//
// It is imported only by _test.go files; svnd never links it.
package testhome

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"

	"github.com/cosmos/cosmos-sdk/client/tx"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// ChainID is the chain id of every test home: a devnet chain id, so the
// rehearsal upgrade handlers run on it.
const ChainID = "shortvideo-dev-statecheck"

// GenesisUnix is the genesis time of every test home (2026-01-01T00:00:00Z).
// Block n has time GenesisUnix + n seconds.
const GenesisUnix int64 = 1_767_225_600

// txGas is the gas limit of every transaction (an authority message uses
// about 200,000; chain/app/authority_flow_test.go).
const txGas = 2_000_000

// proposalExpiry is the expiry of every proposal: about one year after
// GenesisUnix, so no proposal expires in a test.
const proposalExpiry int64 = 1_800_000_000

// Home is one validator home with its application open, as a running node
// holds it: the application database is locked until Stop.
type Home struct {
	// Dir is validator 0's home directory (what svnd start --home takes).
	Dir string

	devnetDir string
	genesis   *cmttypes.GenesisDoc
	app       *app.App
	upgrades  []upgrade.Upgrade
	height    int64
	keys      map[string]cryptotypes.PrivKey
	numbers   map[string]uint64
	seq       map[string]uint64
	policy    uint64
}

// New creates a four-validator devnet under dir (which must not exist or be
// empty) and initializes validator 0's application from the genesis file.
// No block is committed yet.
func New(tb testing.TB, dir string) *Home {
	tb.Helper()
	layout, err := devnet.Init(devnet.Options{Home: dir, ChainID: ChainID, GenesisTime: time.Unix(GenesisUnix, 0).UTC()})
	if err != nil {
		tb.Fatalf("devnet init: %v", err)
	}
	h := &Home{
		Dir:       layout.Topology.Validators[0].Home,
		devnetDir: dir,
		keys:      map[string]cryptotypes.PrivKey{},
		numbers:   map[string]uint64{},
		seq:       map[string]uint64{},
		policy:    1,
	}
	if h.genesis, err = cmttypes.GenesisDocFromFile(filepath.Join(h.Dir, "config", "genesis.json")); err != nil {
		tb.Fatalf("genesis: %v", err)
	}
	h.loadKeys(tb)
	h.open(tb, nil)
	validators := make([]*cmttypes.Validator, len(h.genesis.Validators))
	for i, val := range h.genesis.Validators {
		validators[i] = cmttypes.NewValidator(val.PubKey, val.Power)
	}
	params := h.genesis.ConsensusParams.ToProto()
	if _, err := h.app.InitChain(&abci.RequestInitChain{
		Time:            h.genesis.GenesisTime,
		ChainId:         h.genesis.ChainID,
		InitialHeight:   h.genesis.InitialHeight,
		ConsensusParams: &params,
		Validators:      cmttypes.TM2PB.ValidatorUpdates(cmttypes.NewValidatorSet(validators)),
		AppStateBytes:   h.genesis.AppState,
	}); err != nil {
		tb.Fatalf("init chain: %v", err)
	}
	return h
}

// loadKeys reads the account and seat keys devnet.Init wrote under
// accounts/ and the account numbers of the auth genesis accounts.
func (h *Home) loadKeys(tb testing.TB) {
	tb.Helper()
	read := func(name string) []byte {
		raw, err := os.ReadFile(filepath.Join(h.devnetDir, "accounts", name+".hex"))
		if err != nil {
			tb.Fatal(err)
		}
		key, err := hex.DecodeString(string(bytes.TrimSpace(raw)))
		if err != nil {
			tb.Fatal(err)
		}
		return key
	}
	h.keys["authority"] = &sdksecp.PrivKey{Key: read("authority")}
	for i := 1; i <= authority.ApprovedSeats; i++ {
		name := fmt.Sprintf("seat-%d", i)
		h.keys[name] = &sdked.PrivKey{Key: read(name)}
	}
	var state map[string]json.RawMessage
	if err := json.Unmarshal(h.genesis.AppState, &state); err != nil {
		tb.Fatal(err)
	}
	var auth struct {
		Accounts []struct {
			Address       string `json:"address"`
			AccountNumber uint64 `json:"account_number,string"`
		} `json:"accounts"`
	}
	if err := json.Unmarshal(state["auth"], &auth); err != nil {
		tb.Fatalf("auth genesis: %v", err)
	}
	numbers := map[string]uint64{}
	for _, account := range auth.Accounts {
		numbers[account.Address] = account.AccountNumber
	}
	for name, key := range h.keys {
		number, ok := numbers[sdk.AccAddress(key.PubKey().Address()).String()]
		if !ok {
			tb.Fatalf("account %s has no genesis account", name)
		}
		h.numbers[name] = number
	}
}

// open opens the application database of Dir with app.New, as svnd start
// does, with the upgrade handlers upgrades.
func (h *Home) open(tb testing.TB, upgrades []upgrade.Upgrade) {
	tb.Helper()
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, filepath.Join(h.Dir, "data"))
	if err != nil {
		tb.Fatal(err)
	}
	chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: h.genesis.ChainID, Upgrades: upgrades})
	if err != nil {
		_ = db.Close()
		tb.Fatalf("open %s: %v", h.Dir, err)
	}
	h.app, h.upgrades = chainApp, upgrades
}

// App is the open application.
func (h *Home) App() *app.App { return h.app }

// Height is the last committed height.
func (h *Home) Height() int64 { return h.height }

// Applied records that the block just committed applied an upgrade, which
// moves the policy version by one (DR-10-38); the next proposal names the
// new version as its base.
func (h *Home) Applied() { h.policy++ }

// Block finalizes and commits one block with txs and returns the results.
func (h *Home) Block(tb testing.TB, txs ...[]byte) *abci.ResponseFinalizeBlock {
	tb.Helper()
	height := h.height + 1
	res, err := h.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: height,
		Time:   time.Unix(GenesisUnix+height, 0).UTC(),
		Hash:   bytes.Repeat([]byte{byte(height)}, 32),
		Txs:    txs,
	})
	if err != nil {
		tb.Fatalf("finalize block %d: %v", height, err)
	}
	if _, err := h.app.Commit(); err != nil {
		tb.Fatalf("commit block %d: %v", height, err)
	}
	h.height = height
	return res
}

// BlocksTo commits empty blocks until the last committed height is height.
func (h *Home) BlocksTo(tb testing.TB, height int64) {
	tb.Helper()
	for h.height < height {
		h.Block(tb)
	}
}

// send signs msg with the key of account name at its next sequence,
// delivers it alone in a block, and fails the test unless it commits with
// code 0 and no authority keep.
func (h *Home) send(tb testing.TB, name string, msg sdk.Msg) {
	tb.Helper()
	res := h.Block(tb, h.sign(tb, name, msg))
	result := res.TxResults[0]
	if result.Code != 0 {
		tb.Fatalf("%s from %s: code %s/%d log %s", sdk.MsgTypeURL(msg), name, result.Codespace, result.Code, result.Log)
	}
	h.seq[name]++
	for _, ev := range result.Events {
		if ev.Type != "authority_result" {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == "code" && attr.Value != "" {
				tb.Fatalf("%s from %s kept with %s", sdk.MsgTypeURL(msg), name, attr.Value)
			}
		}
	}
}

func (h *Home) sign(tb testing.TB, name string, msg sdk.Msg) []byte {
	tb.Helper()
	key := h.keys[name]
	cfg := h.app.EncodingConfig().TxConfig
	builder := cfg.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		tb.Fatal(err)
	}
	builder.SetGasLimit(txGas)
	sequence := h.seq[name]
	if err := builder.SetSignatures(signingtypes.SignatureV2{
		PubKey: key.PubKey(), Sequence: sequence,
		Data: &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
	}); err != nil {
		tb.Fatal(err)
	}
	signer := authsigning.SignerData{
		Address: sdk.AccAddress(key.PubKey().Address()).String(), ChainID: h.genesis.ChainID,
		AccountNumber: h.numbers[name], Sequence: sequence, PubKey: key.PubKey(),
	}
	signature, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, key, cfg, sequence)
	if err != nil {
		tb.Fatal(err)
	}
	if err := builder.SetSignatures(signature); err != nil {
		tb.Fatal(err)
	}
	raw, err := cfg.TxEncoder()(builder.GetTx())
	if err != nil {
		tb.Fatal(err)
	}
	return raw
}

// Propose commits a proposal of payload from seat-1 at the current policy
// version. The proposal stays OPEN.
func (h *Home) Propose(tb testing.TB, id, parameterKey string, payload *authorityv1.ProposalPayload) {
	tb.Helper()
	digest, out := authority.PayloadDigest(payload)
	if !out.Success() {
		tb.Fatalf("payload digest of %s: %s", id, out.Code)
	}
	h.send(tb, "seat-1", &authorityv1.MsgProposeChange{
		Signer:            h.keys["seat-1"].PubKey().Address(),
		BusinessId:        "pb-" + id,
		ProposalId:        id,
		ParameterKey:      parameterKey,
		BasePolicyVersion: h.policy,
		Payload:           payload,
		PayloadDigest:     &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: digest},
		ExpiryUnixSeconds: proposalExpiry,
	})
}

// Approve commits the approvals of seats 1 to n (n at most four) of an
// OPEN proposal; with three it becomes QUEUED.
func (h *Home) Approve(tb testing.TB, id string, n int) {
	tb.Helper()
	for i := 1; i <= n; i++ {
		seat := fmt.Sprintf("seat-%d", i)
		h.send(tb, seat, &authorityv1.MsgApproveChange{
			Signer: h.keys[seat].PubKey().Address(), BusinessId: fmt.Sprintf("ap-%s-%d", id, i), ProposalId: id,
		})
	}
}

// Execute commits the execute of a QUEUED proposal from the authority
// account; the execute must apply (no keep). The policy version moves by
// one.
func (h *Home) Execute(tb testing.TB, id string) {
	tb.Helper()
	h.send(tb, "authority", &authorityv1.MsgExecuteChange{
		Signer: h.keys["authority"].PubKey().Address(), BusinessId: "ex-" + id, ProposalId: id,
	})
	h.policy++
}

// Council proposes payload, collects three approvals, and executes it.
func (h *Home) Council(tb testing.TB, id, parameterKey string, payload *authorityv1.ProposalPayload) {
	tb.Helper()
	h.Propose(tb, id, parameterKey, payload)
	h.Approve(tb, id, authority.ApprovedThreshold)
	h.Execute(tb, id)
}

// GovernedLimit is the payload of a governed limit change of key to value,
// applied at activation (0: at execute).
func GovernedLimit(key string, value, activation uint64) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_GovernedLimit{
		GovernedLimit: &authorityv1.GovernedLimitChange{LimitKey: key, Value: value, ActivationHeight: activation},
	}}
}

// SoftwareUpgrade is the payload of an upgrade plan name at height with a
// synthetic release digest.
func SoftwareUpgrade(name string, height uint64) *authorityv1.ProposalPayload {
	return &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_SoftwareUpgrade{
		SoftwareUpgrade: &authorityv1.SoftwareUpgradeChange{
			Name: name, Height: height,
			ReleaseDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes.Repeat([]byte{0x5a}, 32)},
		},
	}}
}

// UpgradeParameterKey is the parameter key of a software upgrade proposal
// (chain/x/authority payload.go).
const UpgradeParameterKey = "upgrade"

// Damage writes one row (a nil value deletes it) into the store named
// store of the stopped home and commits a new version of every store
// outside any block, as integrity damage of a committed home would look.
// It opens the database without the load gate, so it also works on a home
// the gate refuses. The height moves by one.
func (h *Home) Damage(tb testing.TB, store string, key, value []byte) {
	tb.Helper()
	h.raw(tb, func(cms storetypes.CommitMultiStore, kv storetypes.KVStore) {
		if value == nil {
			kv.Delete(key)
		} else {
			kv.Set(key, value)
		}
		h.height = cms.Commit().Version
	}, store)
}

// Row reads one committed row of the store named store of the stopped
// home, without the load gate; nil when the row is absent.
func (h *Home) Row(tb testing.TB, store string, key []byte) []byte {
	tb.Helper()
	var value []byte
	h.raw(tb, func(_ storetypes.CommitMultiStore, kv storetypes.KVStore) {
		value = bytes.Clone(kv.Get(key))
	}, store)
	return value
}

// raw opens the stopped home's application database with app.New and
// SkipLoad, loads the latest version, calls use with the store named store,
// and closes it.
func (h *Home) raw(tb testing.TB, use func(storetypes.CommitMultiStore, storetypes.KVStore), store string) {
	tb.Helper()
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, filepath.Join(h.Dir, "data"))
	if err != nil {
		tb.Fatal(err)
	}
	chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: h.genesis.ChainID, SkipLoad: true})
	if err != nil {
		_ = db.Close()
		tb.Fatal(err)
	}
	defer chainApp.Close()
	if err := chainApp.LoadLatestVersion(); err != nil {
		tb.Fatal(err)
	}
	cms := chainApp.CommitMultiStore()
	named, ok := cms.(interface {
		StoreKeysByName() map[string]storetypes.StoreKey
	})
	if !ok {
		tb.Fatalf("commit multistore %T does not name its stores", cms)
	}
	storeKey, ok := named.StoreKeysByName()[store]
	if !ok {
		tb.Fatalf("no mounted store %s", store)
	}
	use(cms, cms.GetKVStore(storeKey))
}

// Stop closes the application and its database, as a stopped node leaves
// its home.
func (h *Home) Stop(tb testing.TB) {
	tb.Helper()
	if err := h.app.Close(); err != nil {
		tb.Fatal(err)
	}
}

// Start opens the stopped home's application again with the handlers it
// last ran with.
func (h *Home) Start(tb testing.TB) {
	tb.Helper()
	h.open(tb, h.upgrades)
}

// StartWith opens the stopped home's application with upgrades, as a start
// of another binary does. app.New runs the load gate, so a binary it
// refuses fails the test.
func (h *Home) StartWith(tb testing.TB, upgrades []upgrade.Upgrade) {
	tb.Helper()
	h.open(tb, upgrades)
}
