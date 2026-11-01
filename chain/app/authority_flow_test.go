package app_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/client/tx"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func TestCommittedAuthorityFlowAndReplay(t *testing.T) {
	doc, seats, authorityKey := councilDocument()
	first := boot(t, doc)
	second := boot(t, doc)
	numbers := accountNumbers(doc)
	activation := uint64(10)
	first.deliver(t, nil)
	second.deliver(t, nil)
	msg := powerProposal(t, seats[0].key, "add-v5", 1, "v5", bytes32(9), 1, activation)
	steps := []struct {
		key    cryptotypes.PrivKey
		number uint64
		msg    sdk.Msg
	}{
		{seats[0].key, numbers[seats[0].addr], msg},
		{seats[1].key, numbers[seats[1].addr], approveMsg(seats[1].key, "ap-1", "add-v5")},
		{seats[2].key, numbers[seats[2].addr], approveMsg(seats[2].key, "ap-2", "add-v5")},
		{seats[3].key, numbers[seats[3].addr], approveMsg(seats[3].key, "ap-3", "add-v5")},
		{authorityKey, numbers[hex.EncodeToString(authorityKey.PubKey().Address())], executeMsg(authorityKey, "ex-1", "add-v5")},
	}
	raws := make([][]byte, len(steps))
	for i, step := range steps {
		raws[i] = signAny(t, first.app, doc.ChainID, step.key, step.number, 0, step.msg)
		result := first.deliver(t, raws[i])
		if result.Code != 0 {
			t.Fatalf("step %d code %d log %s", i, result.Code, result.Log)
		}
		same := second.deliver(t, raws[i])
		if same.Code != 0 {
			t.Fatalf("replay step %d code %d log %s", i, same.Code, same.Log)
		}
	}
	for first.height < int64(activation) {
		if len(first.last.ValidatorUpdates) != 0 {
			t.Fatalf("updates at height %d before activation", first.height)
		}
		empty := first.deliver(t, nil)
		second.deliver(t, nil)
		_ = empty
	}
	if len(first.last.ValidatorUpdates) != 1 || first.last.ValidatorUpdates[0].Power != 1 {
		t.Fatalf("height %d updates %+v", first.height, first.last.ValidatorUpdates)
	}
	if first.height != int64(activation) {
		t.Fatalf("activation height %d", first.height)
	}
	// CometBFT v0.40 applies this changeset at height+2. The application
	// returns it on the activation block.
	if authority.CometEffectiveDelay != 2 {
		t.Fatal(authority.CometEffectiveDelay)
	}
	if !bytes.Equal(first.app.LastCommitID().Hash, second.app.LastCommitID().Hash) {
		t.Fatal("deterministic replay diverged")
	}
}

type seatKey struct {
	key  *sdked.PrivKey
	addr string
}

func councilDocument() (genesis.Document, []seatKey, *sdksecp.PrivKey) {
	doc := sampleDocument()
	authorityKey := sdksecp.GenPrivKey()
	serviceKey := sdksecp.GenPrivKey()
	doc.Submitters = []genesis.Submitter{
		{AddressHex: hex.EncodeToString(authorityKey.PubKey().Address()), Role: genesis.RoleAuthority},
		{AddressHex: hex.EncodeToString(serviceKey.PubKey().Address()), Role: genesis.RoleAdmittedService},
	}
	seats := make([]seatKey, 4)
	doc.DevnetTestSeats = make([]genesis.DevnetTestSeat, 4)
	for i := range seats {
		key := sdked.GenPrivKey()
		seats[i] = seatKey{key: key, addr: hex.EncodeToString(key.PubKey().Address())}
		doc.DevnetTestSeats[i] = genesis.DevnetTestSeat{
			SeatID:       "seat-" + string(rune('1'+i)),
			PublicKeyHex: hex.EncodeToString(key.PubKey().Bytes()),
		}
		doc.Submitters = append(doc.Submitters, genesis.Submitter{
			AddressHex: seats[i].addr,
			Role:       genesis.RoleBroadcaster,
		})
	}
	doc.DevnetTestParameters = []genesis.DevnetTestParameter{{
		Label: genesis.DevnetTestLabel, DecisionID: "DEC-008", NumericValue: 3,
	}}
	return doc, seats, authorityKey
}

func accountNumbers(doc genesis.Document) map[string]uint64 {
	addrs := make([]string, len(doc.Submitters))
	for i, row := range doc.Submitters {
		addrs[i] = row.AddressHex
	}
	return numbersIncluding(addrs)
}

func numbersIncluding(addrs []string) map[string]uint64 {
	sorted := append([]string(nil), addrs...)
	for i := 0; i < len(sorted); i++ {
		for j := i + 1; j < len(sorted); j++ {
			if sorted[j] < sorted[i] {
				sorted[i], sorted[j] = sorted[j], sorted[i]
			}
		}
	}
	out := map[string]uint64{}
	for i, addr := range sorted {
		out[addr] = uint64(i)
	}
	return out
}

func powerProposal(t *testing.T, key cryptotypes.PrivKey, id string, base uint64, node string, pub []byte, power int64, height uint64) sdk.Msg {
	t.Helper()
	body := &authorityv1.ValidatorPowerChange{
		ConsensusPubkey: pub,
		Power:           power,
		NodeId:          node,
		ReasonDigest:    &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: bytes32(7)},
		Activation:      &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: height}},
	}
	payload := &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: body}}
	digest, out := authority.PayloadDigest(payload)
	if !out.Success() {
		t.Fatal(out.Code)
	}
	return &authorityv1.MsgProposeChange{
		Signer:            key.PubKey().Address(),
		BusinessId:        "b-" + id,
		ProposalId:        id,
		ParameterKey:      "validator-" + node,
		BasePolicyVersion: base,
		Payload:           payload,
		PayloadDigest:     &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: digest},
		ExpiryUnixSeconds: 1_800_000_000,
	}
}

func approveMsg(key cryptotypes.PrivKey, id, proposal string) sdk.Msg {
	return &authorityv1.MsgApproveChange{Signer: key.PubKey().Address(), BusinessId: id, ProposalId: proposal}
}

func executeMsg(key cryptotypes.PrivKey, id, proposal string) sdk.Msg {
	return &authorityv1.MsgExecuteChange{Signer: key.PubKey().Address(), BusinessId: id, ProposalId: proposal}
}

func signAny(t *testing.T, chainApp *app.App, chainID string, key cryptotypes.PrivKey, number, sequence uint64, msg sdk.Msg) []byte {
	t.Helper()
	gas := uint64(200000)
	builder := chainApp.EncodingConfig().TxConfig.NewTxBuilder()
	if err := builder.SetMsgs(msg); err != nil {
		t.Fatal(err)
	}
	builder.SetGasLimit(gas)
	signer := authsigning.SignerData{
		Address:       sdk.AccAddress(key.PubKey().Address()).String(),
		ChainID:       chainID,
		AccountNumber: number,
		Sequence:      sequence,
		PubKey:        key.PubKey(),
	}
	placeholder := signingtypes.SignatureV2{
		PubKey:   key.PubKey(),
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_DIRECT},
		Sequence: sequence,
	}
	if err := builder.SetSignatures(placeholder); err != nil {
		t.Fatal(err)
	}
	signature, err := tx.SignWithPrivKey(context.Background(), signingtypes.SignMode_SIGN_MODE_DIRECT, signer, builder, key, chainApp.EncodingConfig().TxConfig, sequence)
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

func TestWrongSignatureDoesNotMutate(t *testing.T) {
	doc, seats, _ := councilDocument()
	running := boot(t, doc)
	running.deliver(t, nil)
	msg := powerProposal(t, seats[0].key, "sig", 1, "v5", bytes32(9), 1, 12)
	raw := signAny(t, running.app, doc.ChainID, seats[0].key, 0, 0, msg)
	raw[len(raw)-1] ^= 0xff
	before, err := running.app.AuthorityConsensusBytes()
	if err != nil {
		t.Fatal(err)
	}
	result := running.deliver(t, raw)
	if result.Code == 0 {
		t.Fatal("corrupt signature was accepted")
	}
	after, err := running.app.AuthorityConsensusBytes()
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("corrupt signature changed authority state")
	}
	_ = time.Second
}
