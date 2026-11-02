package app_test

import (
	"bytes"
	"testing"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestSimulateEstimatesGas: App.Simulate (the handler behind
// /cosmos.tx.v1beta1.Service/Simulate) runs every registry message and the
// council messages to the end. Simulation runs on the SDK's infinite gas
// meter; before the fix, the handlers' meter-limit check rejected every
// simulated message with ERROR_CODE_BOUNDS. For each step the same signed
// bytes are simulated and then committed: simulation changes no committed
// state, and its gas_used equals the committed gas_used. The delivered-tx
// bounds are unchanged: a declared gas limit of 0 is accepted only in
// simulation (the SDK gas=auto flow) and is BOUNDS in a block, and a
// limit above the cap is rejected in both.
func TestSimulateEstimatesGas(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	step := func(label string, who *actor, msg sdk.Msg) {
		t.Helper()
		raw := n.signed(t, c, who, registryGas, nil, msg)
		before := chainApp.FullSnapshot()
		info, result, err := chainApp.Simulate(raw)
		if err != nil {
			t.Fatalf("%s: simulate: %v", label, err)
		}
		if result == nil {
			t.Fatalf("%s: simulate returned no result", label)
		}
		if !bytes.Equal(before, chainApp.FullSnapshot()) {
			t.Fatalf("%s: simulation changed committed state", label)
		}
		res := n.deliverRaw(t, c, who, raw)
		wantCode(t, label, res, msgkit.OK)
		if info.GasUsed != uint64(res.GasUsed) || info.GasUsed < msgkit.FixedMessageGas {
			t.Fatalf("%s: simulated gas %d, committed gas %d", label, info.GasUsed, res.GasUsed)
		}
		t.Logf("%s: simulated gas %d, committed gas %d", label, info.GasUsed, res.GasUsed)
	}

	step("register p1", n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1))
	step("propose admit p1", n.seats[0], n.proposal(t, "prop-admit-p1", "admit-p1", admitPayload("p1")))
	for i, seat := range n.seats[:3] {
		step("approve admit p1", seat, &authorityv1.MsgApproveChange{
			Signer: seat.addr, BusinessId: "ap-admit-p1-" + string(rune('1'+i)), ProposalId: "prop-admit-p1",
		})
	}
	step("execute admit p1", n.authority, &authorityv1.MsgExecuteChange{Signer: n.authority.addr, BusinessId: "ex-admit-p1", ProposalId: "prop-admit-p1"})
	n.policy++
	step("update p1", n.provider1, n.updateProvider(t, n.provider1, "upd-p1", "p1", capacity1+1, "sim"))
	next := newSigningKey("p1-key-2")
	step("rotate p1", n.provider1, n.rotate(t, n.provider1, "rot-p1", "p1", 1, next))
	n.signing["p1"] = next
	step("drain p1", n.provider1, n.drain(t, n.provider1, "drain-p1", "p1", 2, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN))
	step("register c1", n.service, n.registerContent(t, n.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1))
	step("withdraw c1", n.service, n.withdrawContent(t, n.service, "wd-c1", "c1", 1, reasonDigest("c1")))

	// The SDK client's gas=auto flow: simulate a transaction that declares
	// gas 0, then sign the real one with the adjusted estimate. Gas 0 is
	// accepted only in simulation; the same bytes are BOUNDS in CheckTx and
	// in a block.
	update := n.updateProvider(t, n.provider1, "upd-auto", "p1", capacity1+2, "auto")
	estimateTx := n.signed(t, c, n.provider1, 0, nil, update)
	info, _, err := chainApp.Simulate(estimateTx)
	if err != nil {
		t.Fatalf("simulate with gas 0: %v", err)
	}
	checked, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: estimateTx})
	if err != nil {
		t.Fatal(err)
	}
	if checked.Codespace != svnerr.ModuleName || checked.Code != uint32(typesv1.ErrorCode_ERROR_CODE_BOUNDS) {
		t.Fatalf("CheckTx with gas 0: %s/%d %s", checked.Codespace, checked.Code, checked.Log)
	}
	before := chainApp.FullSnapshot()
	res := n.deliverRaw(t, c, n.provider1, estimateTx)
	wantCode(t, "deliver with gas 0", res, msgkit.Bounds)
	unchanged(t, "deliver with gas 0", chainApp, before, res)
	adjusted := info.GasUsed * 13 / 10
	res = n.deliverRaw(t, c, n.provider1, n.signed(t, c, n.provider1, adjusted, nil, update))
	wantCode(t, "deliver with the adjusted estimate", res, msgkit.OK)
	if uint64(res.GasUsed) > adjusted {
		t.Fatalf("committed gas %d above the adjusted estimate %d", res.GasUsed, adjusted)
	}

	// A declared gas limit above the cap is rejected in simulation as in a
	// block (the SDK's block max gas check, 20,000,000 in this genesis,
	// runs before the beta bounds).
	over := n.signed(t, c, n.provider1, genesis.MaxGasPerTx+1, nil, n.updateProvider(t, n.provider1, "upd-over", "p1", capacity1+3, "over"))
	_, _, err = chainApp.Simulate(over)
	if codespace, code, _ := errorsmod.ABCIInfo(err, false); codespace != sdkerrors.RootCodespace || code != sdkerrors.ErrInvalidGasLimit.ABCICode() {
		t.Fatalf("simulate above the gas cap: %s/%d %v", codespace, code, err)
	}
	before = chainApp.FullSnapshot()
	res = n.deliverRaw(t, c, n.provider1, over)
	if res.Codespace != sdkerrors.RootCodespace || res.Code != sdkerrors.ErrInvalidGasLimit.ABCICode() {
		t.Fatalf("deliver above the gas cap: %s/%d", res.Codespace, res.Code)
	}
	unchanged(t, "deliver above the gas cap", chainApp, before, res)
}
