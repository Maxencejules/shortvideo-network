package app_test

import (
	"bytes"
	"strings"
	"testing"

	errorsmod "cosmossdk.io/errors"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// registrySetup commits p1 (ADMITTED), p2 (APPLIED), c1 (PENDING), and c9
// (WITHDRAWN). It is the starting state of the reject and replay tests.
func registrySetup(t *testing.T, n *regNet, c *cluster) {
	t.Helper()
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)
	wantCode(t, "register p2", n.send(t, c, n.provider2, n.registerProvider(t, n.provider2, "reg-p2", "p2", groupDevnet, capacity1)), msgkit.OK)
	n.council(t, c, "prop-admit-p1", "admit-p1", admitPayload("p1"))
	wantCode(t, "register c1", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1)), msgkit.OK)
	wantCode(t, "register c9", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c9", "c9", 1, packageManifest("c9"), sizeC1)), msgkit.OK)
	wantCode(t, "withdraw c9", n.send(t, c, n.service, n.withdrawContent(t, n.service, "wd-c9", "c9", 1, reasonDigest("c9"))), msgkit.OK)
}

// unchanged asserts that the full non-account state is byte-identical and
// that the rejected transaction emitted no typed registry event.
func unchanged(t *testing.T, label string, chainApp *app.App, before []byte, res *abci.ExecTxResult) {
	t.Helper()
	if after := chainApp.FullSnapshot(); !bytes.Equal(before, after) {
		t.Fatalf("%s: full state changed (%d -> %d bytes)", label, len(before), len(after))
	}
	if typed := typedEvents(res.Events); len(typed) != 0 {
		t.Fatalf("%s: rejected tx emitted %d typed events", label, len(typed))
	}
}

func TestRejectedRegistryMessagesLeaveTheFullState(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	registrySetup(t, n, c)
	chainApp := c.app()

	wrongKey := newSigningKey("p1-key-1")
	delegateIntent := func() *contentv1.MsgRegisterContent {
		msg := n.registerContent(t, n.service, "reg-c5", "c5", 1, packageManifest("c5"), sizeC1)
		signBytes, err := content.RegisterIntentSignBytes(n.service.addr, "c5", 1, sha(packageManifest("c5")), sizeC1, content.DelegateRoleName)
		if err != nil {
			t.Fatal(err)
		}
		msg.HostingIntent = innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, n.intent, signBytes)
		return msg
	}
	cases := []struct {
		name string
		who  *actor
		msg  func() sdk.Msg
		want msgkit.Code
	}{
		{"cross-owner update", n.provider2, func() sdk.Msg { return n.updateProvider(t, n.provider2, "x-upd-1", "p1", capacity2, "x") }, msgkit.Forbidden},
		{"service account updates a provider", n.service, func() sdk.Msg { return n.updateProvider(t, n.service, "x-upd-2", "p1", capacity2, "x") }, msgkit.Forbidden},
		{"update of an unknown provider", n.provider1, func() sdk.Msg { return n.updateProvider(t, n.provider1, "x-upd-3", "p404", capacity2, "x") }, msgkit.NotFound},
		{"update with capacity 0", n.provider1, func() sdk.Msg {
			msg := n.updateProvider(t, n.provider1, "x-upd-4", "p1", capacity2, "x")
			msg.DeclaredCapacityBytes = 0
			return msg
		}, msgkit.Bounds},
		{"service account registers a provider", n.service, func() sdk.Msg { return n.registerProvider(t, n.service, "x-reg-1", "p7", groupDevnet, capacity1) }, msgkit.Forbidden},
		{"duplicate provider id", n.provider2, func() sdk.Msg { return n.registerProvider(t, n.provider2, "x-reg-2", "p1", groupDevnet, capacity1) }, msgkit.DuplicateID},
		// The same business id and payload as p1's committed registration,
		// signed by another provider account: not an idempotent success.
		{"copy of another account's registration", n.provider2, func() sdk.Msg {
			return n.registerProvider(t, n.provider2, "reg-p1", "p1", groupDevnet, capacity1)
		}, msgkit.Forbidden},
		{"65-byte provider id", n.provider1, func() sdk.Msg {
			return n.registerProvider(t, n.provider1, "x-reg-3", strings.Repeat("p", 65), groupDevnet, capacity1)
		}, msgkit.Bounds},
		{"31-byte endpoint digest", n.provider1, func() sdk.Msg {
			msg := n.registerProvider(t, n.provider1, "x-reg-4", "p8", groupDevnet, capacity1)
			msg.EndpointManifestDigest.Value = msg.EndpointManifestDigest.Value[:31]
			return msg
		}, msgkit.Bounds},
		{"digest algorithm 0", n.provider1, func() sdk.Msg {
			msg := n.registerProvider(t, n.provider1, "x-reg-5", "p8", groupDevnet, capacity1)
			msg.EndpointManifestDigest.Algorithm = typesv1.DigestAlgorithm_DIGEST_ALGORITHM_UNSPECIFIED
			return msg
		}, msgkit.Malformed},
		{"missing business id", n.provider1, func() sdk.Msg { return n.registerProvider(t, n.provider1, "", "p8", groupDevnet, capacity1) }, msgkit.Malformed},
		{"replayed business id with another payload", n.provider1, func() sdk.Msg { return n.registerProvider(t, n.provider1, "reg-p1", "p8", groupDevnet, capacity1) }, msgkit.Replay},
		{"stale rotation generation", n.provider1, func() sdk.Msg { return n.rotate(t, n.provider1, "x-rot-1", "p1", 2, newSigningKey("p1-key-2")) }, msgkit.StaleVersion},
		{"rotation signed by another key", n.provider1, func() sdk.Msg {
			msg := n.rotate(t, n.provider1, "x-rot-2", "p1", 1, newSigningKey("p1-key-2"))
			signBytes, err := providers.RotateSignBytes("p1", msg.GetNewSigningKeyId(), msg.GetNewSigningPubkey(), msg.GetExpectedGeneration())
			if err != nil {
				t.Fatal(err)
			}
			msg.CurrentKeySignature = innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE, wrongKey, signBytes)
			return msg
		}, msgkit.Unauthorized},
		{"drain signature in the rotate domain", n.provider1, func() sdk.Msg {
			return n.drain(t, n.provider1, "x-drain-1", "p1", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE)
		}, msgkit.WrongDomain},
		{"drain of an APPLIED provider", n.provider2, func() sdk.Msg {
			return n.drain(t, n.provider2, "x-drain-2", "p2", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN)
		}, msgkit.Conflict},
		{"duplicate content id with a new business id", n.service, func() sdk.Msg {
			return n.registerContent(t, n.service, "x-c-1", "c1", 1, packageManifest("c1"), sizeC1)
		}, msgkit.DuplicateID},
		{"withdrawn content is not revived", n.service, func() sdk.Msg {
			return n.registerContent(t, n.service, "x-c-2", "c9", 1, packageManifest("c9"), sizeC1)
		}, msgkit.DuplicateID},
		{"content version 2", n.service, func() sdk.Msg {
			return n.registerContent(t, n.service, "x-c-3", "c5", 2, packageManifest("c5"), sizeC1)
		}, msgkit.OutOfRange},
		{"package bytes above the cap", n.service, func() sdk.Msg {
			return n.registerContent(t, n.service, "x-c-4", "c5", 1, packageManifest("c5"), genesis.MaxPackageBytes+1)
		}, msgkit.Bounds},
		{"package bytes 0", n.service, func() sdk.Msg {
			return n.registerContent(t, n.service, "x-c-5", "c5", 1, packageManifest("c5"), 0)
		}, msgkit.Bounds},
		{"33-byte manifest digest", n.service, func() sdk.Msg {
			msg := n.registerContent(t, n.service, "x-c-6", "c5", 1, packageManifest("c5"), sizeC1)
			msg.ManifestDigest.Value = append(msg.ManifestDigest.Value, 0)
			return msg
		}, msgkit.Bounds},
		{"provider account registers content", n.provider1, func() sdk.Msg {
			return n.registerContent(t, n.provider1, "x-c-7", "c5", 1, packageManifest("c5"), sizeC1)
		}, msgkit.Forbidden},
		{"host-delegate hosting intent", n.service, func() sdk.Msg { return delegateIntent() }, msgkit.Forbidden},
		{"hosting intent in the drain domain", n.service, func() sdk.Msg {
			msg := n.registerContent(t, n.service, "x-c-8", "c5", 1, packageManifest("c5"), sizeC1)
			msg.HostingIntent.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN
			return msg
		}, msgkit.WrongDomain},
		{"cross-module business id", n.service, func() sdk.Msg {
			return n.registerContent(t, n.service, "reg-p1", "c5", 1, packageManifest("c5"), sizeC1)
		}, msgkit.Replay},
		{"cross-owner withdraw", n.service2, func() sdk.Msg { return n.withdrawContent(t, n.service2, "x-wd-1", "c1", 1, reasonDigest("x")) }, msgkit.Forbidden},
		{"second withdraw", n.service, func() sdk.Msg { return n.withdrawContent(t, n.service, "x-wd-2", "c9", 1, reasonDigest("x")) }, msgkit.Withdrawn},
		{"withdraw of an unknown id", n.service, func() sdk.Msg { return n.withdrawContent(t, n.service, "x-wd-3", "c404", 1, reasonDigest("x")) }, msgkit.NotFound},
		{"withdraw of another version", n.service, func() sdk.Msg { return n.withdrawContent(t, n.service, "x-wd-4", "c1", 2, reasonDigest("x")) }, msgkit.StaleVersion},
	}
	// rows counts every reject row of this test: 30 handler rejects, 5 ante
	// rejects, 1 undecodable transaction, 4 council executions whose
	// registry call fails and 1 registry FORBIDDEN keep.
	rows := 0
	for _, tc := range cases {
		rows++
		before := chainApp.FullSnapshot()
		res := n.send(t, c, tc.who, tc.msg())
		wantCode(t, tc.name, res, tc.want)
		if !anteCommitted(res) {
			t.Fatalf("%s: rejected in the ante handler, not by the registry", tc.name)
		}
		unchanged(t, tc.name, chainApp, before, res)
	}

	// Ante rejections: nothing reaches a handler, and the account sequence
	// does not move either.
	accounts := func() []byte { return chainApp.ModuleSnapshot("acc") }
	// Each case names its exact code: a protocol code (svn) or an SDK
	// ante error (codespace sdk).
	ante := []struct {
		name   string
		raw    func() []byte
		svn    msgkit.Code
		sdkErr *errorsmod.Error
	}{
		{"outsider", func() []byte {
			outsider := newSecp("outsider")
			return signFull(t, chainApp, n.doc.ChainID, outsider.key, 0, 0, registryGas, nil, n.registerProvider(t, outsider, "x-out", "p9", groupDevnet, capacity1))
		}, "", sdkerrors.ErrUnknownAddress},
		{"corrupt outer signature", func() []byte {
			raw := n.signed(t, c, n.provider1, registryGas, nil, n.registerProvider(t, n.provider1, "x-sig", "p9", groupDevnet, capacity1))
			raw[len(raw)-1] ^= 0xff
			return raw
		}, "", sdkerrors.ErrUnauthorized},
		{"fee", func() []byte {
			return n.signed(t, c, n.provider1, registryGas, sdk.NewCoins(sdk.NewInt64Coin("reject", 1)), n.registerProvider(t, n.provider1, "x-fee", "p9", groupDevnet, capacity1))
		}, msgkit.FeeRejected, nil},
		{"gas limit 0", func() []byte {
			return n.signed(t, c, n.provider1, 0, nil, n.registerProvider(t, n.provider1, "x-gas", "p9", groupDevnet, capacity1))
		}, msgkit.Bounds, nil},
		{"70 KiB transaction", func() []byte {
			msg := n.registerProvider(t, n.provider1, "x-big", "p9", groupDevnet, capacity1)
			msg.OperatorGroupId = strings.Repeat("g", 70*1024)
			return n.signed(t, c, n.provider1, registryGas, nil, msg)
		}, msgkit.Bounds, nil},
	}
	for _, tc := range ante {
		rows++
		before := chainApp.FullSnapshot()
		accBefore := accounts()
		res := n.deliverRaw(t, c, n.provider1, tc.raw())
		if res.Code == 0 || anteCommitted(res) {
			t.Fatalf("%s: code %d ante committed %t", tc.name, res.Code, anteCommitted(res))
		}
		if tc.sdkErr != nil {
			if res.Codespace != sdkerrors.RootCodespace || res.Code != tc.sdkErr.ABCICode() {
				t.Fatalf("%s: got %s/%d want sdk/%d (%s)", tc.name, res.Codespace, res.Code, tc.sdkErr.ABCICode(), tc.sdkErr)
			}
		} else {
			wantCode(t, tc.name, res, tc.svn)
		}
		unchanged(t, tc.name, chainApp, before, res)
		if !bytes.Equal(accBefore, accounts()) {
			t.Fatalf("%s: account store changed", tc.name)
		}
	}
	undecodable := directAdminTx(t, "/svn.providers.v1.MsgAdmitProvider", &providersv1.MsgRegisterProvider{Signer: n.provider1.addr})
	rows++
	before := chainApp.FullSnapshot()
	res := n.deliverRaw(t, c, nil, undecodable)
	if res.Codespace != sdkerrors.RootCodespace || res.Code != sdkerrors.ErrTxDecode.ABCICode() {
		t.Fatalf("undecodable tx: %s/%d", res.Codespace, res.Code)
	}
	unchanged(t, "undecodable", chainApp, before, res)

	// Council executions whose registry call fails: the handler fails, the
	// proposal stays QUEUED, and the full state is unchanged.
	for _, tc := range []struct {
		id, key string
		payload *authorityv1.ProposalPayload
		want    msgkit.Code
	}{
		{"prop-admit-p404", "admit-p404", admitPayload("p404"), msgkit.NotFound},
		{"prop-disable-p2", "disable-p2", disablePayload("p2"), msgkit.Conflict},
		{"prop-withdraw-c9", "withdraw-c9", forceWithdrawPayload("c9", 1, reasonDigest("c9")), msgkit.Withdrawn},
		{"prop-withdraw-c1v2", "withdraw-c1", forceWithdrawPayload("c1", 2, reasonDigest("c1")), msgkit.StaleVersion},
	} {
		n.councilReady(t, c, tc.id, tc.key, tc.payload)
		rows++
		before := chainApp.FullSnapshot()
		res := n.execute(t, c, tc.id)
		wantCode(t, "execute "+tc.id, res, tc.want)
		unchanged(t, "execute "+tc.id, chainApp, before, res)
		if state := proposalState(t, chainApp, tc.id); state != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			t.Fatalf("%s: proposal %v after a failed execution", tc.id, state)
		}
	}

	// A registry FORBIDDEN is the spec keep path: the proposal is REJECTED
	// and saved, the tx succeeds with the code on authority_result, and the
	// registries are unchanged.
	wantCode(t, "register p3", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p3", "p3", "other-group", capacity1)), msgkit.OK)
	n.councilReady(t, c, "prop-admit-p3", "admit-p3", admitPayload("p3"))
	rows++
	provBefore, contBefore, authBefore := chainApp.ModuleSnapshot(app.ModuleProviders), chainApp.ModuleSnapshot(app.ModuleContent), chainApp.AuthoritySnapshotForTest()
	res = n.execute(t, c, "prop-admit-p3")
	wantCode(t, "execute admit p3", res, msgkit.OK)
	if authorityResultCode(res) != string(msgkit.Forbidden) {
		t.Fatalf("admit p3 authority_result %q", authorityResultCode(res))
	}
	if !bytes.Equal(provBefore, chainApp.ModuleSnapshot(app.ModuleProviders)) || !bytes.Equal(contBefore, chainApp.ModuleSnapshot(app.ModuleContent)) {
		t.Fatal("a rejected admission changed a registry store")
	}
	if bytes.Equal(authBefore, chainApp.AuthoritySnapshotForTest()) {
		t.Fatal("the REJECTED mark was not saved")
	}
	if state := proposalState(t, chainApp, "prop-admit-p3"); state != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED {
		t.Fatalf("admit p3 proposal %v", state)
	}
	if rows != 41 {
		t.Fatalf("%d reject rows, want 41", rows)
	}
}

// proposalState is the committed state of one council proposal (its 0x10
// record in the authority store).
func proposalState(t *testing.T, chainApp *app.App, id string) authorityv1.ProposalState {
	t.Helper()
	state, found, err := chainApp.ProposalStateForTest(id)
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatalf("proposal %s not found", id)
	}
	return state
}

func TestDecisionBlockedRegistryPaths(t *testing.T) {
	// Without DEC-001 no hosting intent passes: content registration is
	// DECISION_BLOCKED on every chain without the devnet parameter.
	blocked := newRegNet("DEC-001")
	bc := blocked.start(t, 1)
	before := bc.app().FullSnapshot()
	res := blocked.send(t, bc, blocked.service, blocked.registerContent(t, blocked.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1))
	wantCode(t, "register without DEC-001", res, msgkit.DecisionBlocked)
	unchanged(t, "register without DEC-001", bc.app(), before, res)

	// Without DEC-002 and DEC-003 the council cannot admit or force-withdraw.
	n := newRegNet("DEC-002", "DEC-003")
	c := n.start(t, 1)
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)
	wantCode(t, "register c1", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1)), msgkit.OK)
	for _, tc := range []struct {
		id, key string
		payload *authorityv1.ProposalPayload
	}{
		{"prop-admit-p1", "admit-p1", admitPayload("p1")},
		{"prop-withdraw-c1", "withdraw-c1", forceWithdrawPayload("c1", 1, reasonDigest("c1"))},
	} {
		n.councilReady(t, c, tc.id, tc.key, tc.payload)
		before := c.app().FullSnapshot()
		res := n.execute(t, c, tc.id)
		wantCode(t, "execute "+tc.id, res, msgkit.DecisionBlocked)
		unchanged(t, "execute "+tc.id, c.app(), before, res)
		if state := proposalState(t, c.app(), tc.id); state != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			t.Fatalf("%s: %v", tc.id, state)
		}
	}
}

func TestIdempotentRegistryReplay(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	registrySetup(t, n, c)
	chainApp := c.app()
	replays := []struct {
		name   string
		who    *actor
		msg    sdk.Msg
		module string
	}{
		{"register provider", n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1), "providers"},
		{"register content", n.service, n.registerContent(t, n.service, "reg-c1", "c1", 1, packageManifest("c1"), sizeC1), "content"},
		{"withdraw content", n.service, n.withdrawContent(t, n.service, "wd-c9", "c9", 1, reasonDigest("c9")), "content"},
	}
	update := n.updateProvider(t, n.provider1, "upd-p1", "p1", capacity2, "u")
	wantCode(t, "update p1", n.send(t, c, n.provider1, update), msgkit.OK)
	replays = append(replays, struct {
		name   string
		who    *actor
		msg    sdk.Msg
		module string
	}{"update provider", n.provider1, proto.Clone(update).(sdk.Msg), "providers"})
	drain := n.drain(t, n.provider1, "drain-p1", "p1", 1, typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN)
	wantCode(t, "drain p1", n.send(t, c, n.provider1, drain), msgkit.OK)
	replays = append(replays, struct {
		name   string
		who    *actor
		msg    sdk.Msg
		module string
	}{"drain provider", n.provider1, proto.Clone(drain).(sdk.Msg), "providers"})

	for _, tc := range replays {
		before := chainApp.FullSnapshot()
		res := n.send(t, c, tc.who, tc.msg)
		wantCode(t, "replay "+tc.name, res, msgkit.OK)
		unchanged(t, "replay "+tc.name, chainApp, before, res)
		if !hasIdempotentEvent(res, tc.module) {
			t.Fatalf("replay %s: no %s_result idempotent event", tc.name, tc.module)
		}
	}

	// One business-id namespace across authority, providers, and content.
	before := chainApp.FullSnapshot()
	propose := n.proposal(t, "prop-x", "admit-p2", admitPayload("p2"))
	propose.BusinessId = "reg-c1"
	res := n.send(t, c, n.seats[0], propose)
	wantCode(t, "proposal reusing a content business id", res, msgkit.Replay)
	unchanged(t, "proposal reusing a content business id", chainApp, before, res)

	before = chainApp.FullSnapshot()
	res = n.send(t, c, n.provider2, n.registerProvider(t, n.provider2, "pb-prop-admit-p1", "p6", groupDevnet, capacity1))
	wantCode(t, "provider reusing a proposal business id", res, msgkit.Replay)
	unchanged(t, "provider reusing a proposal business id", chainApp, before, res)

	before = chainApp.FullSnapshot()
	res = n.send(t, c, n.service, n.registerContent(t, n.service, "upd-p1", "c6", 1, packageManifest("c6"), sizeC1))
	wantCode(t, "content reusing a provider business id", res, msgkit.Replay)
	unchanged(t, "content reusing a provider business id", chainApp, before, res)
}

func hasIdempotentEvent(res *abci.ExecTxResult, module string) bool {
	for _, ev := range res.Events {
		if ev.Type != module+"_result" {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == "idempotent" && attr.Value == "true" {
				return true
			}
		}
	}
	return false
}

func TestProviderKeyRotationIsNotAConsensusChange(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	wantCode(t, "register p1", n.send(t, c, n.provider1, n.registerProvider(t, n.provider1, "reg-p1", "p1", groupDevnet, capacity1)), msgkit.OK)

	authBefore := chainApp.AuthoritySnapshotForTest()
	provBefore := chainApp.ModuleSnapshot(app.ModuleProviders)
	next := newSigningKey("p1-key-2")
	res := n.send(t, c, n.provider1, n.rotate(t, n.provider1, "rot-p1", "p1", 1, next))
	wantCode(t, "rotate p1", res, msgkit.OK)
	if len(c.blocks[len(c.blocks)-1].ValidatorUpdates) != 0 {
		t.Fatal("a provider key rotation returned validator updates")
	}
	if !bytes.Equal(authBefore, chainApp.AuthoritySnapshotForTest()) {
		t.Fatal("a provider key rotation changed authority state")
	}
	if bytes.Equal(provBefore, chainApp.ModuleSnapshot(app.ModuleProviders)) {
		t.Fatal("rotation did not change the providers store")
	}
	n.signing["p1"] = next

	// A validator power change touches authority only.
	provBefore = chainApp.ModuleSnapshot(app.ModuleProviders)
	contBefore := chainApp.ModuleSnapshot(app.ModuleContent)
	activation := uint64(c.height() + 12)
	body := &authorityv1.ValidatorPowerChange{
		ConsensusPubkey: bytes32(9),
		Power:           1,
		NodeId:          "v5",
		ReasonDigest:    digestOf(bytes32(7)),
		Activation:      &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: activation}},
	}
	n.council(t, c, "prop-add-v5", "validator-v5", &authorityv1.ProposalPayload{Change: &authorityv1.ProposalPayload_ValidatorPower{ValidatorPower: body}})
	for c.height() < int64(activation) {
		c.block(t)
	}
	if updates := c.blocks[len(c.blocks)-1].ValidatorUpdates; len(updates) != 1 || updates[0].Power != 1 {
		t.Fatalf("activation updates %v", updates)
	}
	if !bytes.Equal(provBefore, chainApp.ModuleSnapshot(app.ModuleProviders)) || !bytes.Equal(contBefore, chainApp.ModuleSnapshot(app.ModuleContent)) {
		t.Fatal("a validator power change touched a registry store")
	}
	if authority.CometEffectiveDelay != 2 {
		t.Fatal(authority.CometEffectiveDelay)
	}
}
