package devnet_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	cmttypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client/tx"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func TestDevnetValidatorLifecycle(t *testing.T) {
	if os.Getenv("SVN_SKIP_DEVNET") == "1" {
		t.Skip()
	}
	home := t.TempDir()
	layout, err := devnet.Init(devnet.Options{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	stops := make([]func() error, 4)
	for i := range stops {
		stop, err := launch(layout.Topology.Validators[i].Home)
		if err != nil {
			t.Fatal(err)
		}
		stops[i] = stop
	}
	t.Cleanup(func() {
		for _, stop := range stops {
			if stop != nil {
				_ = stop()
			}
		}
	})
	rpcs := rpcList(layout)
	if _, _, err := devnet.WaitEqual(rpcs, 3, 45*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	enc := mustEncoding(t)
	signers := loadSigners(t, home)
	numbers := genesisAccountNumbers(t, enc, layout.Genesis)
	seq := map[string]uint64{}
	rpc := rpcs[0]

	heightNow := mustHeight(t, rpc)
	activation := uint64(heightNow + 40)
	addKey := sdked.GenPrivKey().PubKey().Bytes()
	fmt.Printf("phase=plan-add height=%d activation=%d effective=%d\n", heightNow, activation, int64(activation)+authority.CometEffectiveDelay)
	submitPower(t, enc, layout.Topology.ChainID, rpc, signers, numbers, seq, "add-v5", 1, "v5", addKey, 1, activation)
	effective := int64(activation) + authority.CometEffectiveDelay
	if _, _, err := devnet.WaitEqual(rpcs, effective, 60*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	before := validatorSet(t, rpc, int64(activation)-1)
	atActivation := validatorSet(t, rpc, int64(activation))
	oneLater := validatorSet(t, rpc, effective-1)
	landed := validatorSet(t, rpc, effective)
	fmt.Printf("phase=add h-1=%d count=%d h=%d count=%d h+1=%d count=%d h+2=%d count=%d\n", int64(activation)-1, len(before), activation, len(atActivation), effective-1, len(oneLater), effective, len(landed))
	if len(before) != 4 || len(atActivation) != 4 || len(oneLater) != 4 || hasPower(oneLater, addKey, 1) || len(landed) != 5 || !hasPower(landed, addKey, 1) {
		t.Fatalf("add sets h-1 %d h %d h+1 %d h+2 %d", len(before), len(atActivation), len(oneLater), len(landed))
	}

	// v5 has power and no process. Stopping one running validator leaves 3/5
	// of the power online. Three processes are still up. That is not quorum.
	beforeStop, err := devnet.HighWater(rpcs)
	if err != nil {
		t.Fatal(err)
	}
	if err := stops[3](); err != nil {
		t.Fatal(err)
	}
	stops[3] = nil
	stall := observeStall(t, "power-one-down", rpcs[:3])
	fmt.Printf("phase=power-stall height=%d threshold=%d\n", stall, beforeStop.Height)
	stop, err := launch(layout.Topology.Validators[3].Home)
	if err != nil {
		t.Fatal(err)
	}
	stops[3] = stop
	up, _, err := devnet.WaitEqual(rpcs, stall+1, 45*time.Second)
	if err != nil {
		t.Fatal(tail(layout, err))
	}
	resumed, _, err := devnet.WaitAbove(rpcs, up+1, 45*time.Second)
	if err != nil || resumed < up+2 {
		t.Fatalf("recovery after power stall up %d resumed %d err %v", up, resumed, err)
	}
	fmt.Printf("phase=power-recovery up=%d reached=%d\n", up, resumed)

	heightNow = mustHeight(t, rpc)
	rotateAt := uint64(heightNow + 40)
	rotated := sdked.GenPrivKey().PubKey().Bytes()
	fmt.Printf("phase=plan-rotate height=%d activation=%d\n", heightNow, rotateAt)
	submitPower(t, enc, layout.Topology.ChainID, rpc, signers, numbers, seq, "rot-v5", 2, "v5", rotated, 1, rotateAt)
	rotEffective := int64(rotateAt) + authority.CometEffectiveDelay
	if _, _, err := devnet.WaitEqual(rpcs, rotEffective, 60*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	rotateOld := validatorSet(t, rpc, rotEffective-1)
	rotatedSet := validatorSet(t, rpc, rotEffective)
	fmt.Printf("phase=rotate h+1=%d count=%d oldkey=%t h+2=%d count=%d newkey=%t\n", rotEffective-1, len(rotateOld), hasPower(rotateOld, addKey, 1), rotEffective, len(rotatedSet), hasPower(rotatedSet, rotated, 1))
	if !hasPower(rotateOld, addKey, 1) || hasPower(rotateOld, rotated, 1) || hasPower(rotatedSet, addKey, 1) || !hasPower(rotatedSet, rotated, 1) || len(rotatedSet) != 5 {
		t.Fatalf("rotation old %d new %d", len(rotateOld), len(rotatedSet))
	}

	heightNow = mustHeight(t, rpc)
	removeAt := uint64(heightNow + 40)
	fmt.Printf("phase=plan-remove height=%d activation=%d\n", heightNow, removeAt)
	submitPower(t, enc, layout.Topology.ChainID, rpc, signers, numbers, seq, "rm-v5", 3, "v5", rotated, 0, removeAt)
	rmEffective := int64(removeAt) + authority.CometEffectiveDelay
	if _, _, err := devnet.WaitEqual(rpcs, rmEffective, 60*time.Second); err != nil {
		t.Fatal(tail(layout, err))
	}
	removeOld := validatorSet(t, rpc, rmEffective-1)
	removed := validatorSet(t, rpc, rmEffective)
	fmt.Printf("phase=remove h+1=%d count=%d still=%t h+2=%d count=%d\n", rmEffective-1, len(removeOld), hasPower(removeOld, rotated, 1), rmEffective, len(removed))
	if !hasPower(removeOld, rotated, 1) || len(removeOld) != 5 || len(removed) != 4 || hasPower(removed, rotated, 1) {
		t.Fatalf("removal old %d new %d", len(removeOld), len(removed))
	}
}

type loaded struct {
	name string
	addr string
	key  cryptotypes.PrivKey
}

func loadSigners(t *testing.T, home string) []loaded {
	t.Helper()
	out := []loaded{
		loadSecp(t, home, "authority"),
		loadSecp(t, home, "service"),
	}
	for i := 1; i <= 4; i++ {
		out = append(out, loadEd(t, home, fmt.Sprintf("seat-%d", i)))
	}
	return out
}

func loadSecp(t *testing.T, home, name string) loaded {
	t.Helper()
	raw := readHexFile(t, home+"/accounts/"+name+".hex")
	key := &sdksecp.PrivKey{Key: raw}
	return loaded{name: name, key: key, addr: hex.EncodeToString(key.PubKey().Address())}
}

func loadEd(t *testing.T, home, name string) loaded {
	t.Helper()
	raw := readHexFile(t, home+"/accounts/"+name+".hex")
	key := &sdked.PrivKey{Key: raw}
	return loaded{name: name, key: key, addr: hex.EncodeToString(key.PubKey().Address())}
}

func readHexFile(t *testing.T, path string) []byte {
	t.Helper()
	text, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := hex.DecodeString(strings.TrimSpace(string(text)))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// genesisAccountNumbers reads the account number of every genesis account
// (lowercase hex address -> number) from the typed auth genesis state. The
// devnet genesis holds more accounts than any one test signs with.
func genesisAccountNumbers(t *testing.T, enc app.Encoding, genesisPath string) map[string]uint64 {
	t.Helper()
	genDoc, err := cmttypes.GenesisDocFromFile(genesisPath)
	if err != nil {
		t.Fatal(err)
	}
	var modules map[string]json.RawMessage
	if err := json.Unmarshal(genDoc.AppState, &modules); err != nil {
		t.Fatal(err)
	}
	var state authtypes.GenesisState
	if err := enc.Codec.UnmarshalJSON(modules[authtypes.ModuleName], &state); err != nil {
		t.Fatal(err)
	}
	accounts, err := authtypes.UnpackAccounts(state.GetAccounts())
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]uint64{}
	for _, account := range accounts {
		out[hex.EncodeToString(account.GetAddress())] = account.GetAccountNumber()
	}
	if len(out) == 0 {
		t.Fatal("genesis has no accounts")
	}
	return out
}

func mustEncoding(t *testing.T) app.Encoding {
	t.Helper()
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	return enc
}

func submitPower(t *testing.T, enc app.Encoding, chainID, rpc string, signers []loaded, numbers map[string]uint64, seq map[string]uint64, id string, base uint64, node string, pub []byte, power int64, height uint64) {
	t.Helper()
	body := &authorityv1.ValidatorPowerChange{
		ConsensusPubkey: pub,
		Power:           power,
		NodeId:          node,
		ReasonDigest:    &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes.Repeat([]byte{7}, 32)},
		Activation:      &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: height}},
	}
	payload := &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: body}}
	digest, out := authority.PayloadDigest(payload)
	if !out.Success() {
		t.Fatal(out.Code)
	}
	seat := func(n int) loaded {
		for _, row := range signers {
			if row.name == fmt.Sprintf("seat-%d", n) {
				return row
			}
		}
		t.Fatal("missing seat")
		return loaded{}
	}
	authoritySigner := loaded{}
	for _, row := range signers {
		if row.name == "authority" {
			authoritySigner = row
		}
	}
	proposer := seat(1)
	msg := &authorityv1.MsgProposeChange{
		Signer: proposer.key.PubKey().Address(), BusinessId: "b-" + id, ProposalId: id,
		ParameterKey: "validator-" + node, BasePolicyVersion: base, Payload: payload,
		PayloadDigest:     &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: digest},
		ExpiryUnixSeconds: time.Now().Add(24 * time.Hour).Unix(),
	}
	broadcastSigned(t, enc, chainID, rpc, proposer, numbers, seq, msg)
	for _, n := range []int{1, 2, 3} {
		who := seat(n)
		broadcastSigned(t, enc, chainID, rpc, who, numbers, seq, &authorityv1.MsgApproveChange{
			Signer: who.key.PubKey().Address(), BusinessId: fmt.Sprintf("%s-ap-%d", id, n), ProposalId: id,
		})
	}
	broadcastSigned(t, enc, chainID, rpc, authoritySigner, numbers, seq, &authorityv1.MsgExecuteChange{
		Signer: authoritySigner.key.PubKey().Address(), BusinessId: "ex-" + id, ProposalId: id,
	})
}

func broadcastSigned(t *testing.T, enc app.Encoding, chainID, rpc string, who loaded, numbers map[string]uint64, seq map[string]uint64, msg sdk.Msg) {
	t.Helper()
	raw := signDevnet(t, enc, chainID, who.key, numbers[who.addr], seq[who.addr], msg)
	code, log := broadcast(t, rpc, raw)
	if code != 0 {
		t.Fatalf("%s code %d log %s", who.name, code, log)
	}
	seq[who.addr]++
}

func signDevnet(t *testing.T, enc app.Encoding, chainID string, key cryptotypes.PrivKey, number, sequence uint64, msg sdk.Msg) []byte {
	t.Helper()
	builder := enc.TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		t.Fatal(err)
	}
	builder.SetGasLimit(2000000)
	signer := authsigning.SignerData{
		Address: sdk.AccAddress(key.PubKey().Address()).String(), ChainID: chainID,
		AccountNumber: number, Sequence: sequence, PubKey: key.PubKey(),
	}
	placeholder := signingtypes.SignatureV2{
		PubKey: key.PubKey(), Sequence: sequence,
		Data: &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
	}
	if err := builder.SetSignatures(placeholder); err != nil {
		t.Fatal(err)
	}
	signature, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, key, enc.TxConfig, sequence)
	if err != nil {
		t.Fatal(err)
	}
	if err := builder.SetSignatures(signature); err != nil {
		t.Fatal(err)
	}
	raw, err := enc.TxConfig.TxEncoder()(builder.GetTx())
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func broadcast(t *testing.T, rpc string, raw []byte) (uint32, string) {
	t.Helper()
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "broadcast_tx_commit",
		"params": map[string]string{"tx": base64.StdEncoding.EncodeToString(raw)},
	})
	resp, err := http.Post(strings.TrimRight(rpc, "/")+"/", "application/json", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var parsed struct {
		Error *struct {
			Message string `json:"message"`
		} `json:"error"`
		Result struct {
			CheckTx struct {
				Code uint32 `json:"code"`
				Log  string `json:"log"`
			} `json:"check_tx"`
			TxResult struct {
				Code uint32 `json:"code"`
				Log  string `json:"log"`
			} `json:"tx_result"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	if parsed.Error != nil {
		t.Fatal(parsed.Error.Message)
	}
	if parsed.Result.CheckTx.Code != 0 {
		return parsed.Result.CheckTx.Code, parsed.Result.CheckTx.Log
	}
	return parsed.Result.TxResult.Code, parsed.Result.TxResult.Log
}

func mustHeight(t *testing.T, rpc string) int64 {
	t.Helper()
	height, _, err := devnet.WaitEqual([]string{rpc}, 1, 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return height
}

type voted struct {
	pub   []byte
	power int64
}

func validatorSet(t *testing.T, rpc string, height int64) []voted {
	t.Helper()
	resp, err := http.Get(fmt.Sprintf("%s/validators?height=%d&per_page=100", strings.TrimRight(rpc, "/"), height))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var parsed struct {
		Result struct {
			Validators []struct {
				PubKey struct {
					Value string `json:"value"`
				} `json:"pub_key"`
				VotingPower string `json:"voting_power"`
			} `json:"validators"`
		} `json:"result"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		t.Fatal(err)
	}
	out := make([]voted, 0, len(parsed.Result.Validators))
	for _, row := range parsed.Result.Validators {
		pub, err := base64.StdEncoding.DecodeString(row.PubKey.Value)
		if err != nil {
			t.Fatal(err)
		}
		var power int64
		fmt.Sscanf(row.VotingPower, "%d", &power)
		out = append(out, voted{pub: pub, power: power})
	}
	return out
}

func hasPower(rows []voted, pub []byte, power int64) bool {
	for _, row := range rows {
		if bytes.Equal(row.pub, pub) && row.power == power {
			return true
		}
	}
	return false
}
