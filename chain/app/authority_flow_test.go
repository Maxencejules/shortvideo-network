package app_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
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
	before := running.app.AuthoritySnapshot()
	result := running.deliver(t, raw)
	if result.Code == 0 {
		t.Fatal("corrupt signature was accepted")
	}
	if !bytes.Equal(before, running.app.AuthoritySnapshot()) {
		t.Fatal("corrupt signature changed the authority snapshot")
	}
	_ = time.Second
}

func TestRejectedMessagesLeaveTheFullAuthorityState(t *testing.T) {
	doc, seats, authorityKey := councilDocument()
	doc.DevnetTestParameters[0].DelaySeconds = 100
	running := boot(t, doc)
	running.deliver(t, nil)
	numbers := accountNumbers(doc)
	seq := map[string]uint64{}
	same := func(before []byte) {
		t.Helper()
		after := running.app.AuthoritySnapshot()
		if !bytes.Equal(before, after) {
			t.Fatalf("authority snapshot changed\n% x\n% x", before, after)
		}
	}
	bump := func(addr string, anteFailed bool) {
		if !anteFailed {
			seq[addr]++
		}
	}
	send := func(key cryptotypes.PrivKey, addr string, gas uint64, fee sdk.Coins, msg sdk.Msg) *abci.ExecTxResult {
		t.Helper()
		if gas == 0 {
			gas = 200000
		}
		raw := signFull(t, running.app, doc.ChainID, key, numbers[addr], seq[addr], gas, fee, msg)
		result := running.deliver(t, raw)
		return result
	}

	before := running.app.AuthoritySnapshot()
	outsider := sdksecp.GenPrivKey()
	rejected := send(outsider, hex.EncodeToString(outsider.PubKey().Address()), 0, nil, powerProposal(t, outsider, "out", 1, "v5", bytes32(9), 1, 20))
	if rejected.Code == 0 {
		t.Fatal("outsider was accepted")
	}
	same(before)

	bad := signFull(t, running.app, doc.ChainID, seats[0].key, numbers[seats[0].addr], 0, 200000, nil, powerProposal(t, seats[0].key, "bad-sig", 1, "v5", bytes32(9), 1, 20))
	bad[len(bad)-1] ^= 0xff
	if running.deliver(t, bad).Code == 0 {
		t.Fatal("corrupt signature was accepted")
	}
	same(before)

	if send(seats[0].key, seats[0].addr, 0, nil, &authorityv1.MsgProposeChange{Signer: seats[0].key.PubKey().Address()}).Code == 0 {
		t.Fatal("malformed proposal was accepted")
	}
	bump(seats[0].addr, false)
	same(before)

	if send(seats[1].key, seats[1].addr, 0, sdk.NewCoins(sdk.NewInt64Coin("reject", 1)), powerProposal(t, seats[1].key, "fee", 1, "v5", bytes32(9), 1, 20)).Code == 0 {
		t.Fatal("fee was accepted")
	}
	same(before)

	if send(seats[2].key, seats[2].addr, 0, nil, powerProposal(t, seats[2].key, "add-v5", 1, "v5", bytes32(9), 1, 30)).Code != 0 {
		t.Fatal("valid propose failed")
	}
	bump(seats[2].addr, false)
	opened := running.app.AuthoritySnapshot()
	if bytes.Equal(before, opened) {
		t.Fatal("valid propose did not change state")
	}

	dup := powerProposal(t, seats[3].key, "add-v5", 1, "v9", bytes32(11), 1, 30)
	if send(seats[3].key, seats[3].addr, 0, nil, dup).Code == 0 {
		t.Fatal("duplicate proposal id was accepted")
	}
	bump(seats[3].addr, false)
	same(opened)

	if send(authorityKey, hex.EncodeToString(authorityKey.PubKey().Address()), 0, nil, approveMsg(authorityKey, "nope", "missing")).Code == 0 {
		t.Fatal("missing approval was accepted")
	}
	bump(hex.EncodeToString(authorityKey.PubKey().Address()), false)
	same(opened)

	if send(seats[0].key, seats[0].addr, 0, nil, approveMsg(seats[0].key, "ap-1", "add-v5")).Code != 0 {
		t.Fatal("first approval failed")
	}
	bump(seats[0].addr, false)
	one := running.app.AuthoritySnapshot()
	if send(seats[0].key, seats[0].addr, 0, nil, approveMsg(seats[0].key, "ap-1b", "add-v5")).Code == 0 {
		t.Fatal("repeated approval was accepted")
	}
	bump(seats[0].addr, false)
	same(one)

	if send(authorityKey, hex.EncodeToString(authorityKey.PubKey().Address()), 0, nil, executeMsg(authorityKey, "too-soon", "add-v5")).Code == 0 {
		t.Fatal("execute of an open proposal was accepted")
	}
	bump(hex.EncodeToString(authorityKey.PubKey().Address()), false)
	same(one)

	for _, seat := range seats[1:3] {
		if send(seat.key, seat.addr, 0, nil, approveMsg(seat.key, "ap-"+seat.addr[:4], "add-v5")).Code != 0 {
			t.Fatal("approval failed")
		}
		bump(seat.addr, false)
	}
	queued := running.app.AuthoritySnapshot()
	if send(seats[3].key, seats[3].addr, 0, nil, approveMsg(seats[3].key, "stale", "add-v5")).Code == 0 {
		t.Fatal("stale approval was accepted")
	}
	bump(seats[3].addr, false)
	same(queued)

	if send(seats[1].key, seats[1].addr, 0, nil, executeMsg(seats[1].key, "not-auth", "add-v5")).Code == 0 {
		t.Fatal("non-authority execute was accepted")
	}
	bump(seats[1].addr, false)
	same(queued)

	if send(authorityKey, hex.EncodeToString(authorityKey.PubKey().Address()), 0, nil, executeMsg(authorityKey, "early", "add-v5")).Code == 0 {
		t.Fatal("early execute was accepted")
	}
	bump(hex.EncodeToString(authorityKey.PubKey().Address()), false)
	same(queued)
}

func TestRemovalBindHaltLeavesTheSnapshot(t *testing.T) {
	doc, _, _ := councilDocument()
	running := boot(t, doc)
	running.deliver(t, nil)
	if err := running.app.InstallMismatchedRemovalForTest(); err != nil {
		t.Fatal(err)
	}
	before := running.app.AuthoritySnapshot()
	running.height++
	_, err := running.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: running.height,
		Time:   running.when,
		Hash:   bytes32(byte(running.height)),
	})
	if err == nil {
		t.Fatal("mismatched removal was committed")
	}
	if !bytes.Equal(before, running.app.AuthoritySnapshot()) {
		t.Fatal("halt changed the authority snapshot")
	}
	_, err = running.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		Height: running.height,
		Time:   running.when,
		Hash:   bytes32(byte(running.height)),
	})
	if err == nil {
		t.Fatal("a second attempt committed the bad removal")
	}
	if !bytes.Equal(before, running.app.AuthoritySnapshot()) {
		t.Fatal("second halt changed the authority snapshot")
	}
}

func signFull(t *testing.T, chainApp *app.App, chainID string, key cryptotypes.PrivKey, number, sequence, gas uint64, fee sdk.Coins, msg sdk.Msg) []byte {
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
