package app_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	sdked "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// The goal-10 app-level governance tests (plan 16.6, non-rehearsal part).
// Every transaction is signed and goes straight into a FinalizeBlock block
// (CheckTx is not on the path), so every refusal below happens at
// execution: in the ante handler or in the message handler.

// govSnap is the committed full non-account state (app.FullSnapshot, every
// module store including x/policy and x/authority) and the signer-blind
// auth snapshot (SignerBlindAuthSnapshotForTest: every auth row, accounts
// without the sequence and public key the ante writes), so a refused
// council seat payload that created an account would show.
type govSnap struct {
	full []byte
	auth []byte
}

func takeGovSnap(t *testing.T, chainApp *app.App) govSnap {
	t.Helper()
	auth, err := chainApp.SignerBlindAuthSnapshotForTest()
	if err != nil {
		t.Fatal(err)
	}
	return govSnap{full: chainApp.FullSnapshot(), auth: auth}
}

// same fails unless the committed state still equals s and res, a refused
// transaction, emitted no typed svn.events.v1 event.
func (s govSnap) same(t *testing.T, label string, chainApp *app.App, res *abci.ExecTxResult) {
	t.Helper()
	now := takeGovSnap(t, chainApp)
	if !bytes.Equal(s.full, now.full) {
		t.Fatalf("%s: the full non-account state changed (%d -> %d bytes)", label, len(s.full), len(now.full))
	}
	if !bytes.Equal(s.auth, now.auth) {
		t.Fatalf("%s: the auth store changed beyond what the ante writes for a signer", label)
	}
	if res != nil {
		if typed := typedEvents(res.Events); len(typed) != 0 {
			t.Fatalf("%s: a refused transaction emitted %d typed events", label, len(typed))
		}
	}
}

// govProposal is n.proposal signed by seat instead of seat-1.
func (n *regNet) govProposal(t testing.TB, seat *actor, id, parameterKey string, payload *authorityv1.ProposalPayload) *authorityv1.MsgProposeChange {
	t.Helper()
	msg := n.proposal(t, id, parameterKey, payload)
	msg.Signer = seat.addr
	return msg
}

// refusedProposal is a ProposeChange from seat at the tracked policy
// version for a payload the chain refuses at parse: the canonical payload
// digest cannot be computed, so the message carries the SHA-256 of a fixed
// text instead (the parse check comes before the digest check).
func (n *regNet) refusedProposal(t testing.TB, seat *actor, id, parameterKey string, payload *authorityv1.ProposalPayload) *authorityv1.MsgProposeChange {
	t.Helper()
	if _, out := authority.PayloadDigest(payload); out.Success() {
		t.Fatalf("%s: the payload parses; use govProposal", id)
	}
	return &authorityv1.MsgProposeChange{
		Signer:            seat.addr,
		BusinessId:        "pb-" + id,
		ProposalId:        id,
		ParameterKey:      parameterKey,
		BasePolicyVersion: n.policy,
		Payload:           payload,
		PayloadDigest:     digestOf(sha([]byte("unparsed payload " + id))),
		ExpiryUnixSeconds: 1_800_000_000,
	}
}

// anyProposal is govProposal for a payload that parses and refusedProposal
// for one that does not.
func (n *regNet) anyProposal(t testing.TB, seat *actor, id, parameterKey string, payload *authorityv1.ProposalPayload) *authorityv1.MsgProposeChange {
	t.Helper()
	if _, out := authority.PayloadDigest(payload); !out.Success() {
		return n.refusedProposal(t, seat, id, parameterKey, payload)
	}
	return n.govProposal(t, seat, id, parameterKey, payload)
}

func approval(seat *actor, bid, id string) *authorityv1.MsgApproveChange {
	return &authorityv1.MsgApproveChange{Signer: seat.addr, BusinessId: bid, ProposalId: id}
}

func execution(who *actor, bid, id string) *authorityv1.MsgExecuteChange {
	return &authorityv1.MsgExecuteChange{Signer: who.addr, BusinessId: bid, ProposalId: id}
}

// governanceKind is one council payload kind of the goal-10 tests: the v0
// governed limit applied at execute and the four schema-version-2 forms.
type governanceKind struct {
	name    string
	key     string
	payload *authorityv1.ProposalPayload
}

// governanceKinds are the five payloads every per-kind test runs. far is a
// height inside both lead windows at propose and at execute; cancel names
// the plan the cancel kind cancels; seatKey is the new key of seat-4.
func governanceKinds(far uint64, plan, cancel string, seatKey []byte) []governanceKind {
	return []governanceKind{
		{"governed limit", msgkit.LimitMaxOpenOffers, governedPayload(msgkit.LimitMaxOpenOffers, 7)},
		{"scheduled limit", msgkit.LimitMaxPageLimit, scheduledPayload(msgkit.LimitMaxPageLimit, 40, far)},
		{"software upgrade", "upgrade", upgradePayload(plan, far, "release "+plan)},
		{"cancel upgrade", "upgrade", cancelPayload(cancel)},
		{"council seat", "seat-seat-4", seatPayload("seat-4", seatKey)},
	}
}

// refusal is one refused transaction: its exact codespace and code, a
// fragment its log must hold, and whether the ante handler refused it
// (the signer's sequence does not move) or the message handler did.
type refusal struct {
	space string
	code  uint32
	log   string
	ante  bool
}

var (
	unknownAddress = refusal{space: sdkerrors.RootCodespace, code: sdkerrors.ErrUnknownAddress.ABCICode(), log: "", ante: true}
	notSeat        = refusal{space: "svn", code: uint32(typesv1.ErrorCode_ERROR_CODE_UNAUTHORIZED), log: authority.CodeUnauthorized}
	notExecutor    = refusal{space: "svn", code: uint32(typesv1.ErrorCode_ERROR_CODE_FORBIDDEN), log: authority.CodeForbidden}
	seatExecute    = refusal{space: "svn", code: uint32(typesv1.ErrorCode_ERROR_CODE_FORBIDDEN), log: seatOnlyLog, ante: true}
	retiredKey     = refusal{space: "svn", code: uint32(typesv1.ErrorCode_ERROR_CODE_UNAUTHORIZED), log: "signer is not an admitted application account", ante: true}
)

// refuse signs msg from who, delivers it in its own block, and requires
// the refusal want with the committed state, the sequence-blind auth store
// included, byte-identical. A handler refusal moves who's sequence by one;
// an ante refusal moves nothing.
func (n *regNet) refuse(t *testing.T, c *cluster, label string, who *actor, msg sdk.Msg, want refusal) {
	t.Helper()
	chainApp := c.app()
	raw := n.signed(t, c, who, registryGas, nil, msg)
	before := takeGovSnap(t, chainApp)
	seqBefore, known := chainApp.AccountSequenceForTest(who.addr)
	res := n.deliverRaw(t, c, who, raw)
	if res.Codespace != want.space || res.Code != want.code || !strings.Contains(res.Log, want.log) {
		t.Fatalf("%s: got %s/%d log %q, want %s/%d with %q", label, res.Codespace, res.Code, res.Log, want.space, want.code, want.log)
	}
	if anteCommitted(res) == want.ante {
		t.Fatalf("%s: ante committed %t, want an ante refusal %t", label, anteCommitted(res), want.ante)
	}
	before.same(t, label, chainApp, res)
	seqAfter, knownAfter := chainApp.AccountSequenceForTest(who.addr)
	if knownAfter != known {
		t.Fatalf("%s: account existence changed", label)
	}
	if want.ante && seqAfter != seqBefore {
		t.Fatalf("%s: the ante refused but the sequence moved %d -> %d", label, seqBefore, seqAfter)
	}
}

// refuseCode is refuse for a message-handler refusal with protocol code
// want.
func (n *regNet) refuseCode(t *testing.T, c *cluster, label string, who *actor, msg sdk.Msg, want msgkit.Code) {
	t.Helper()
	n.refuse(t, c, label, who, msg, refusal{space: "svn", code: uint32(typesv1.ErrorCode_value[string(want)]), log: string(want)})
}

// adoptSeat records the replacement key of seat index i after an executed
// council seat payload: its account number (the execute created the
// account) and that it now signs for the seat.
func (n *regNet) adoptSeat(t *testing.T, chainApp *app.App, i int, replacement *actor) {
	t.Helper()
	number, exists := chainApp.AccountNumberForTest(replacement.addr)
	if !exists {
		t.Fatalf("%s: the executed council seat payload created no account for the new key", replacement.name)
	}
	n.numbers[replacement.hexAddr()] = number
	n.seats[i] = replacement
}

// wantState requires the committed state of proposal id.
func wantProposalState(t *testing.T, chainApp *app.App, id string, want authorityv1.ProposalState) {
	t.Helper()
	state, found, err := chainApp.ProposalStateForTest(id)
	if err != nil || !found || state != want {
		t.Fatalf("proposal %s: state %v found %t err %v, want %v", id, state, found, err, want)
	}
}

// TestUnauthorizedPolicyWritesFailAtExecution (acceptance check: "assert
// unauthorized policy writes fail at execution"): for each of the five
// governance payload kinds (governed limit at execute, scheduled governed
// limit, software upgrade, cancel upgrade, council seat), every signer
// class without the right is refused on every write path, with its exact
// code, in a committed block:
//
//   - propose and approve: an outsider with no account (sdk code 9 in the
//     ante), the admitted service, an admitted provider, the broadcaster,
//     an auditor, and the AUTHORITY account (ERROR_CODE_UNAUTHORIZED in the
//     handler: none is a council seat);
//   - execute: the outsider (ante), the service, the provider, the
//     broadcaster, and the auditor (ERROR_CODE_FORBIDDEN in the handler:
//     only the AUTHORITY submitter executes), and each of the four seats
//     (ERROR_CODE_FORBIDDEN in the ante, DR-06-06).
//
// After every row the full non-account state (x/policy and x/authority
// included) and the sequence-blind auth store are byte-identical, so no
// row created the seat key's account; the target proposals keep their
// states. A message type for a direct policy write does not exist: a
// transaction naming /svn.policy.v1.MsgUpdateLimits or another policy,
// upgrade, or governance type URL fails decoding (sdk code 2) in CheckTx
// and in the block. The auth module's MsgUpdateParams, signed by the
// AUTHORITY account with its own address as authority, is refused in the
// handler (sdk code 4: only the svn module account, which no key controls,
// may update auth params). Controls: the authority's execute of each QUEUED
// target simulates to success on the committed state, and a seat's
// approval of each OPEN target commits, so the rows were refused for their
// signer, not their content.
func TestUnauthorizedPolicyWritesFailAtExecution(t *testing.T) {
	e := newEvNet()
	n := e.regNet
	c := n.start(t, 1)
	chainApp := c.app()
	n.council(t, c, "up-0", "upgrade", upgradePayload("up-0", uint64(c.height())+5000, "release up-0"))
	seatKey := sdked.GenPrivKey().PubKey().Bytes()
	kinds := governanceKinds(uint64(c.height())+10_000, "up-1", "up-0", seatKey)
	for i, k := range kinds {
		open := fmt.Sprintf("uw-%d-open", i)
		wantCode(t, "propose "+open, n.send(t, c, n.seats[0], n.proposal(t, open, k.key, k.payload)), msgkit.OK)
		wantCode(t, "approve "+open, n.send(t, c, n.seats[1], approval(n.seats[1], open+"-ap-2", open)), msgkit.OK)
		n.councilReady(t, c, fmt.Sprintf("uw-%d-q", i), k.key, k.payload)
	}
	outsider := newSecp("outsider")
	wrong := []struct {
		who     *actor
		council refusal
		execute refusal
	}{
		{outsider, unknownAddress, unknownAddress},
		{n.service, notSeat, notExecutor},
		{n.provider1, notSeat, notExecutor},
		{n.broadcaster, notSeat, notExecutor},
		{e.auditor1, notSeat, notExecutor},
		{n.authority, notSeat, refusal{}},
	}
	rows := 0
	for i, k := range kinds {
		open, queued := fmt.Sprintf("uw-%d-open", i), fmt.Sprintf("uw-%d-q", i)
		for _, w := range wrong {
			label := fmt.Sprintf("%s: %s", k.name, w.who.name)
			n.refuse(t, c, label+" proposes", w.who, n.govProposal(t, w.who, fmt.Sprintf("uw-%d-%s", i, w.who.name), k.key, k.payload), w.council)
			n.refuse(t, c, label+" approves", w.who, approval(w.who, fmt.Sprintf("uw-%d-ap-%s", i, w.who.name), open), w.council)
			rows += 2
			if w.who == n.authority {
				continue
			}
			n.refuse(t, c, label+" executes", w.who, execution(w.who, fmt.Sprintf("uw-%d-ex-%s", i, w.who.name), queued), w.execute)
			rows++
		}
		for _, seat := range n.seats {
			n.refuse(t, c, fmt.Sprintf("%s: %s executes", k.name, seat.name), seat, execution(seat, fmt.Sprintf("uw-%d-ex-%s", i, seat.name), queued), seatExecute)
			rows++
		}
		wantProposalState(t, chainApp, open, authorityv1.ProposalState_PROPOSAL_STATE_OPEN)
		wantProposalState(t, chainApp, queued, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	}
	if chainApp.AccountExistsForTest(authority.SeatAddress(seatKey)) {
		t.Fatal("a refused row created the council seat key's account")
	}

	for _, typeURL := range []string{
		"/svn.policy.v1.MsgUpdateLimits",
		"/svn.policy.v1.MsgSetGovernedLimit",
		"/svn.policy.v1.MsgScheduleUpgrade",
		"/svn.policy.v1.MsgCancelUpgrade",
		"/svn.authority.v1.GovernedLimitChange",
		"/svn.authority.v1.SoftwareUpgradeChange",
		"/svn.authority.v1.CancelUpgradeChange",
		"/svn.authority.v1.CouncilSeatChange",
		"/cosmos.upgrade.v1beta1.MsgSoftwareUpgrade",
		"/cosmos.gov.v1.MsgSubmitProposal",
	} {
		if chainApp.MsgServiceRouter().HandlerByTypeURL(typeURL) != nil {
			t.Fatalf("%s has a message handler", typeURL)
		}
		raw := directAdminTx(t, typeURL, &authorityv1.GovernedLimitChange{LimitKey: msgkit.LimitMaxOpenOffers, Value: 1})
		checked, err := chainApp.CheckTx(&abci.RequestCheckTx{Tx: raw})
		if err != nil {
			t.Fatal(err)
		}
		wantDecodeError(t, typeURL+" check", checked.Codespace, checked.Code)
		before := takeGovSnap(t, chainApp)
		res := n.deliverRaw(t, c, nil, raw)
		wantDecodeError(t, typeURL+" deliver", res.Codespace, res.Code)
		before.same(t, typeURL, chainApp, res)
		rows++
	}
	n.refuse(t, c, "the AUTHORITY account updates the auth params", n.authority, &authtypes.MsgUpdateParams{
		Authority: sdk.AccAddress(n.authority.addr).String(), Params: authtypes.DefaultParams(),
	}, refusal{space: sdkerrors.RootCodespace, code: sdkerrors.ErrUnauthorized.ABCICode(), log: "invalid authority"})
	rows++
	if rows != 116 {
		t.Fatalf("%d refused rows, want 116", rows)
	}

	for i, k := range kinds {
		queued := fmt.Sprintf("uw-%d-q", i)
		raw := n.signed(t, c, n.authority, registryGas, nil, execution(n.authority, "uw-ctl-"+queued, queued))
		before := takeGovSnap(t, chainApp)
		if _, _, err := chainApp.Simulate(raw); err != nil {
			t.Fatalf("%s: the authority's execute of %s does not simulate to success: %v", k.name, queued, err)
		}
		before.same(t, "simulated execute of "+queued, chainApp, nil)
	}
	for i, k := range kinds {
		open := fmt.Sprintf("uw-%d-open", i)
		wantCode(t, k.name+": seat-3 approves "+open, n.send(t, c, n.seats[2], approval(n.seats[2], open+"-ap-3", open)), msgkit.OK)
	}
}

// TestDuplicateVotes: approvals are counted once per seat id, for every
// payload kind. A seat that approved and approves again with a new
// business id is ERROR_CODE_REPLAY with the state unchanged; the same
// approval again under its first business id (new transaction bytes, new
// sequence) is an idempotent success (authority_result idempotent=true, no
// state change); with a third seat's approval the proposal holds two
// distinct approvals, stays OPEN, and its execute is ERROR_CODE_CONFLICT.
// After an executed council seat payload replaces seat-2's key, the new
// key's approval of a proposal the old key approved is ERROR_CODE_REPLAY:
// approvals are deduplicated by seat id, not by key. The new key's
// approval of another proposal commits.
func TestDuplicateVotes(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	n.council(t, c, "up-0", "upgrade", upgradePayload("up-0", uint64(c.height())+5000, "release up-0"))
	kinds := governanceKinds(uint64(c.height())+10_000, "up-1", "up-0", sdked.GenPrivKey().PubKey().Bytes())
	s1, s2, s3 := n.seats[0], n.seats[1], n.seats[2]
	for i, k := range kinds {
		id := fmt.Sprintf("dv-%d", i)
		wantCode(t, k.name+": propose", n.send(t, c, s1, n.proposal(t, id, k.key, k.payload)), msgkit.OK)
		wantCode(t, k.name+": seat-2 approves", n.send(t, c, s2, approval(s2, id+"-ap-a", id)), msgkit.OK)
		n.refuseCode(t, c, k.name+": seat-2 approves again, new business id", s2, approval(s2, id+"-ap-b", id), msgkit.Replay)
		before := takeGovSnap(t, chainApp)
		res := n.send(t, c, s2, approval(s2, id+"-ap-a", id))
		wantCode(t, k.name+": seat-2 resends its approval", res, msgkit.OK)
		if !hasIdempotentEvent(res, authority.ModuleName) {
			t.Fatalf("%s: the resent approval is not marked idempotent: %v", k.name, res.Events)
		}
		before.same(t, k.name+": the resent approval", chainApp, res)
		wantCode(t, k.name+": seat-3 approves", n.send(t, c, s3, approval(s3, id+"-ap-c", id)), msgkit.OK)
		n.refuseCode(t, c, k.name+": execute with two distinct approvals", n.authority, execution(n.authority, "ex-"+id, id), msgkit.Conflict)
		wantProposalState(t, chainApp, id, authorityv1.ProposalState_PROPOSAL_STATE_OPEN)
	}

	wantCode(t, "propose dv-p", n.send(t, c, s1, n.proposal(t, "dv-p", msgkit.LimitMaxPackageBytes, governedPayload(msgkit.LimitMaxPackageBytes, 1000))), msgkit.OK)
	wantCode(t, "seat-2 approves dv-p", n.send(t, c, s2, approval(s2, "dv-p-ap-2", "dv-p")), msgkit.OK)
	replacement := newSeat("seat-2")
	n.council(t, c, "dv-seat-2", "seat-seat-2", seatPayload("seat-2", replacement.key.PubKey().Bytes()))
	n.adoptSeat(t, chainApp, 1, replacement)
	n.refuseCode(t, c, "seat-2's new key approves what its old key approved", replacement, approval(replacement, "dv-p-ap-new", "dv-p"), msgkit.Replay)
	wantCode(t, "propose dv-q", n.send(t, c, s1, n.proposal(t, "dv-q", msgkit.LimitMaxPackageBytes, governedPayload(msgkit.LimitMaxPackageBytes, 2000))), msgkit.OK)
	wantCode(t, "seat-2's new key approves dv-q", n.send(t, c, replacement, approval(replacement, "dv-q-ap-2", "dv-q")), msgkit.OK)
}

// TestInsufficientApproval: every payload kind needs three distinct seats
// of four (owner question Q3, "3 of 4 + min notice (Recommended)"; no
// per-kind threshold). The execute of a proposal with zero, one, or two
// approvals is ERROR_CODE_CONFLICT with the state unchanged and the
// proposal OPEN; the third approval queues it and the execute then
// commits, writes x/policy (or replaces the seat, whose new account the
// signer-blind auth snapshot shows), and moves the policy version by one.
func TestInsufficientApproval(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	seatKey := sdked.GenPrivKey().PubKey().Bytes()
	far := uint64(c.height()) + 10_000
	kinds := governanceKinds(far, "up-ia", "up-ia", seatKey)
	for i, k := range kinds {
		id := fmt.Sprintf("ia-%d", i)
		wantCode(t, k.name+": propose", n.send(t, c, n.seats[0], n.proposal(t, id, k.key, k.payload)), msgkit.OK)
		for approvals := 0; approvals < 3; approvals++ {
			n.refuseCode(t, c, fmt.Sprintf("%s: execute with %d approvals", k.name, approvals), n.authority,
				execution(n.authority, fmt.Sprintf("ex-%s-%d", id, approvals), id), msgkit.Conflict)
			wantProposalState(t, chainApp, id, authorityv1.ProposalState_PROPOSAL_STATE_OPEN)
			seat := n.seats[approvals]
			wantCode(t, k.name+": approve", n.send(t, c, seat, approval(seat, fmt.Sprintf("%s-ap-%d", id, approvals), id)), msgkit.OK)
		}
		wantProposalState(t, chainApp, id, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
		policyBefore := chainApp.PolicySnapshotForTest()
		authBefore := takeGovSnap(t, chainApp).auth
		res := n.execute(t, c, id)
		wantCode(t, k.name+": execute with three approvals", res, msgkit.OK)
		if code := authorityResultCode(res); code != "" {
			t.Fatalf("%s: execute kept with %s", k.name, code)
		}
		n.policy++
		if k.name == "council seat" {
			if !chainApp.AccountExistsForTest(authority.SeatAddress(seatKey)) {
				t.Fatal("the executed seat payload created no account")
			}
			if bytes.Equal(authBefore, takeGovSnap(t, chainApp).auth) {
				t.Fatal("the signer-blind auth snapshot does not show the created account")
			}
		} else if bytes.Equal(policyBefore, chainApp.PolicySnapshotForTest()) {
			t.Fatalf("%s: the execute did not write x/policy", k.name)
		}
	}
	state := queryPolicy(t, chainApp)
	if state.GetPolicyVersion() != n.policy || n.policy != 6 || state.GetUpgradePlan() != nil || len(state.GetPendingLimitChanges()) != 1 {
		t.Fatalf("policy after the five executes (tracked version %d): %v", n.policy, state)
	}
}

// TestRemovedCouncilSeat (acceptance check "removed council seats"; a seat
// is replaced, the council keeps four seats, DEC-008): seat-2's key is
// replaced through real signed transactions (propose, three approvals,
// execute of a council seat payload). The execute creates the new key's
// account and retires the old key. Then:
//
//   - the old key's approve and propose are ERROR_CODE_UNAUTHORIZED in the
//     ante (not admitted), its sequence and the state unchanged;
//   - the new key's first propose commits (sequence 0);
//   - a QUEUED proposal the old key approved is ERROR_CODE_STALE_VERSION at
//     execute (the seat execute moved the policy version) and stays QUEUED;
//   - the new key's approval of an OPEN proposal the old key approved is
//     ERROR_CODE_REPLAY (same seat id), and its execute is ERROR_CODE_FORBIDDEN
//     in the ante;
//   - putting the retired key back on seat-2, or on seat-3, is
//     ERROR_CODE_CONFLICT at execute, the proposal stays QUEUED, and no
//     account changes.
//
// Query/Governance reports four seats, seat-2 with the new key.
func TestRemovedCouncilSeat(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	old := n.seats[1]
	n.councilReady(t, c, "rs-a", msgkit.LimitMaxOpenOffers, governedPayload(msgkit.LimitMaxOpenOffers, 7))
	wantCode(t, "propose rs-b", n.send(t, c, n.seats[0], n.proposal(t, "rs-b", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 50))), msgkit.OK)
	wantCode(t, "old seat-2 key approves rs-b", n.send(t, c, old, approval(old, "rs-b-ap-2", "rs-b")), msgkit.OK)

	replacement := newSeat("seat-2")
	if chainApp.AccountExistsForTest(replacement.addr) {
		t.Fatal("the new key has an account before the execute")
	}
	n.council(t, c, "rs-seat", "seat-seat-2", seatPayload("seat-2", replacement.key.PubKey().Bytes()))
	n.adoptSeat(t, chainApp, 1, replacement)
	if row, role, admission := chainApp.SeatViewForTest(old.addr); row || role != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED || admission != authority.NotAdmitted {
		t.Fatalf("retired key: admission row %v, role %v, admission %v", row, role, admission)
	}
	if row, role, admission := chainApp.SeatViewForTest(replacement.addr); row || role != typesv1.SubmitterRole_SUBMITTER_ROLE_UNSPECIFIED || admission != authority.AdmittedSeat {
		t.Fatalf("new key: admission row %v, role %v, admission %v", row, role, admission)
	}
	seats := governanceSeats(t, chainApp)
	if len(seats) != 4 || !bytes.Equal(seats["seat-2"], replacement.key.PubKey().Bytes()) {
		t.Fatalf("council seats after the replacement: %x", seats)
	}

	n.refuse(t, c, "old key approves rs-b", old, approval(old, "rs-b-ap-old", "rs-b"), retiredKey)
	n.refuse(t, c, "old key proposes", old, n.govProposal(t, old, "rs-old", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 60)), retiredKey)
	if seq, _ := chainApp.AccountSequenceForTest(replacement.addr); seq != 0 {
		t.Fatalf("new key sequence %d before its first transaction", seq)
	}
	wantCode(t, "new key's first propose", n.send(t, c, replacement, n.govProposal(t, replacement, "rs-new", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 70))), msgkit.OK)
	wantProposalState(t, chainApp, "rs-new", authorityv1.ProposalState_PROPOSAL_STATE_OPEN)
	n.refuseCode(t, c, "execute of a proposal the old key approved", n.authority, execution(n.authority, "ex-rs-a", "rs-a"), msgkit.StaleVersion)
	wantProposalState(t, chainApp, "rs-a", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	n.refuseCode(t, c, "new key approves what the old key approved", replacement, approval(replacement, "rs-b-ap-new", "rs-b"), msgkit.Replay)
	n.refuse(t, c, "new key executes", replacement, execution(replacement, "ex-rs-new", "rs-new"), seatExecute)

	n.councilReady(t, c, "rs-back", "seat-seat-2", seatPayload("seat-2", old.key.PubKey().Bytes()))
	n.refuseCode(t, c, "execute: the retired key back on seat-2", n.authority, execution(n.authority, "ex-rs-back", "rs-back"), msgkit.Conflict)
	n.councilReady(t, c, "rs-move", "seat-seat-3", seatPayload("seat-3", old.key.PubKey().Bytes()))
	n.refuseCode(t, c, "execute: the retired key on seat-3", n.authority, execution(n.authority, "ex-rs-move", "rs-move"), msgkit.Conflict)
	wantProposalState(t, chainApp, "rs-back", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	wantProposalState(t, chainApp, "rs-move", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	if seats := governanceSeats(t, chainApp); len(seats) != 4 || !bytes.Equal(seats["seat-2"], replacement.key.PubKey().Bytes()) {
		t.Fatalf("council seats at the end: %x", seats)
	}
}

// governanceSeats are the council seats Query/Governance reports, by seat
// id.
func governanceSeats(t *testing.T, chainApp *app.App) map[string][]byte {
	t.Helper()
	raw, res := query(t, chainApp, "/svn.authority.v1.Query/Governance", &authorityv1.QueryGovernanceRequest{
		Page: &typesv1.PageRequest{Limit: 100},
	}, 0)
	if res.Code != 0 {
		t.Fatalf("Query/Governance: %s/%d %s", res.Codespace, res.Code, res.Log)
	}
	var out authorityv1.QueryGovernanceResponse
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	seats := map[string][]byte{}
	for _, seat := range out.GetState().GetDevnetTestSeats() {
		seats[seat.GetSeatId()] = seat.GetPublicKey()
	}
	return seats
}

// governedValue is the governed value in force for key that Query/Policy
// reports on the committed state.
func governedValue(t *testing.T, chainApp *app.App, key string) uint64 {
	t.Helper()
	for _, row := range queryPolicy(t, chainApp).GetGovernedLimits() {
		if row.GetLimitKey() == key {
			return row.GetValue()
		}
	}
	t.Fatalf("Query/Policy has no governed limit %s", key)
	return 0
}

// TestConcurrentProposals (acceptance check "concurrent proposals"):
//
//   - same key: two QUEUED governed changes of max_open_offers on one base.
//     The first execute commits; the second is ERROR_CODE_CONFLICT (its key
//     last executed another digest) and a QUEUED change of another key on
//     the old base is ERROR_CODE_STALE_VERSION. Both stay QUEUED.
//   - one block: two QUEUED changes of different keys executed in one block;
//     the first commits and the second, on the old base, is STALE.
//   - two scheduled changes of one key: the second execute replaces the
//     pending change of the first (policy_pending names the superseded
//     proposal); at the first activation height nothing changes, at the
//     second the second value applies.
//   - quota per seat: seat-4 holds MaxOpenProposalsPerSeat (32) open
//     proposals on the current base; its next propose is ERROR_CODE_BOUNDS
//     with the state unchanged, another seat still proposes, and after an
//     execute moves the policy version seat-4 proposes again (proposals on
//     an older base no longer hold a slot, DR-06-03).
func TestConcurrentProposals(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	n.councilReady(t, c, "cc-7", msgkit.LimitMaxOpenOffers, governedPayload(msgkit.LimitMaxOpenOffers, 7))
	n.councilReady(t, c, "cc-8", msgkit.LimitMaxOpenOffers, governedPayload(msgkit.LimitMaxOpenOffers, 8))
	n.councilReady(t, c, "cc-page", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 50))
	wantCode(t, "execute cc-7", n.execute(t, c, "cc-7"), msgkit.OK)
	n.policy++
	n.refuseCode(t, c, "execute cc-8: same key, another digest", n.authority, execution(n.authority, "ex-cc-8", "cc-8"), msgkit.Conflict)
	n.refuseCode(t, c, "execute cc-page: another key, older base", n.authority, execution(n.authority, "ex-cc-page", "cc-page"), msgkit.StaleVersion)
	wantProposalState(t, chainApp, "cc-8", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	wantProposalState(t, chainApp, "cc-page", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	if v := governedValue(t, chainApp, msgkit.LimitMaxOpenOffers); v != 7 {
		t.Fatalf("max_open_offers %d after the first execute", v)
	}

	n.councilReady(t, c, "cc-pkg", msgkit.LimitMaxPackageBytes, governedPayload(msgkit.LimitMaxPackageBytes, 1000))
	n.councilReady(t, c, "cc-batch", msgkit.LimitMaxEvidenceBatch, governedPayload(msgkit.LimitMaxEvidenceBatch, 4))
	first := n.signed(t, c, n.authority, registryGas, nil, execution(n.authority, "ex-cc-pkg", "cc-pkg"))
	n.seq[n.authority.hexAddr()]++
	second := n.signed(t, c, n.authority, registryGas, nil, execution(n.authority, "ex-cc-batch", "cc-batch"))
	n.seq[n.authority.hexAddr()]++
	res := c.block(t, first, second)
	wantCode(t, "same block: first execute", res.TxResults[0], msgkit.OK)
	wantCode(t, "same block: second execute, older base", res.TxResults[1], msgkit.StaleVersion)
	n.policy++
	if v := governedValue(t, chainApp, msgkit.LimitMaxEvidenceBatch); v != 32 {
		t.Fatalf("max_evidence_batch %d after the stale execute", v)
	}

	a1 := uint64(c.height()) + 20
	n.council(t, c, "cc-s1", msgkit.LimitMaxOpenOffers, scheduledPayload(msgkit.LimitMaxOpenOffers, 9, a1))
	a2 := a1 + 5
	res2 := n.council(t, c, "cc-s2", msgkit.LimitMaxOpenOffers, scheduledPayload(msgkit.LimitMaxOpenOffers, 10, a2))
	if superseded := eventAttr(res2.Events, policy.EventPolicyPending, policy.AttrSupersededProposalID); superseded != "cc-s1" {
		t.Fatalf("the second schedule superseded %q, want cc-s1", superseded)
	}
	pending := queryPolicy(t, chainApp).GetPendingLimitChanges()
	if len(pending) != 1 || pending[0].GetProposalId() != "cc-s2" || pending[0].GetActivationHeight() != a2 || pending[0].GetValue() != 10 || pending[0].GetPolicyVersion() != n.policy {
		t.Fatalf("pending changes after the replacement: %v", pending)
	}
	for uint64(c.height()) < a2 {
		c.block(t)
		want := uint64(7)
		if uint64(c.height()) >= a2 {
			want = 10
		}
		if v := governedValue(t, chainApp, msgkit.LimitMaxOpenOffers); v != want {
			t.Fatalf("height %d: max_open_offers %d, want %d", c.height(), v, want)
		}
	}

	s4 := n.seats[3]
	for start := 0; start < authority.MaxOpenProposalsPerSeat; start += 8 {
		msgs := make([]sdk.Msg, 0, 8)
		for i := start; i < start+8; i++ {
			msgs = append(msgs, n.govProposal(t, s4, fmt.Sprintf("cc-q-%02d", i), msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, uint64(10+i))))
		}
		n.sendMulti(t, c, s4, fmt.Sprintf("seat-4 proposals %d-%d", start, start+7), msgs)
	}
	n.refuseCode(t, c, "seat-4 at its open limit proposes", s4, n.govProposal(t, s4, "cc-q-over", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 99)), msgkit.Bounds)
	wantCode(t, "seat-3 proposes", n.send(t, c, n.seats[2], n.govProposal(t, n.seats[2], "cc-s3", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 60))), msgkit.OK)
	n.council(t, c, "cc-move", msgkit.LimitMaxPackageBytes, governedPayload(msgkit.LimitMaxPackageBytes, 2000))
	wantCode(t, "seat-4 proposes on the new base", n.send(t, c, s4, n.govProposal(t, s4, "cc-q-new", msgkit.LimitMaxPageLimit, governedPayload(msgkit.LimitMaxPageLimit, 61))), msgkit.OK)
}

// eventAttr is the value of attribute key of the first event of type typ,
// or "" when there is none.
func eventAttr(events []abci.Event, typ, key string) string {
	for _, ev := range events {
		if ev.Type != typ {
			continue
		}
		for _, attr := range ev.Attributes {
			if attr.Key == key {
				return attr.Value
			}
		}
	}
	return ""
}

// TestCouncilVotesAreNotConsensusVotes (REQ-BOUND-005): council approvals
// are application transactions signed by seat keys; validator commit
// signatures are not votes on a proposal. Every block here carries a
// DecidedLastCommit that every genesis validator signed. A proposal stays
// OPEN through such blocks; with three approvals it is QUEUED and stays
// QUEUED, not executed, through more of them (the full state unchanged);
// and no block of the flow, the execute of a governed limit, of an upgrade
// plan, and of a council seat change included, returns a validator update
// or consensus params other than the genesis params, so the commit signers
// of later heights are the genesis validators. Query/Governance reports the genesis validator
// set at the end.
func TestCouncilVotesAreNotConsensusVotes(t *testing.T) {
	n := newRegNet()
	node := boot(t, n.doc)
	votes := make([]abci.VoteInfo, len(n.doc.Validators))
	for i, v := range n.doc.Validators {
		pub, err := hex.DecodeString(v.ConsensusPubkeyHex)
		if err != nil {
			t.Fatal(err)
		}
		votes[i] = abci.VoteInfo{
			Validator:   abci.Validator{Address: cmted25519.PubKey(pub).Address(), Power: v.Power},
			BlockIdFlag: cmtproto.BlockIDFlagCommit,
		}
	}
	var genesisParams []byte
	block := func(who *actor, msg sdk.Msg) *abci.ExecTxResult {
		t.Helper()
		var txs [][]byte
		if msg != nil {
			txs = append(txs, signFull(t, node.app, n.doc.ChainID, who.key, n.numbers[who.hexAddr()], n.seq[who.hexAddr()], registryGas, nil, msg))
		}
		node.height++
		res, err := node.app.FinalizeBlock(&abci.RequestFinalizeBlock{
			Height: node.height, Time: node.when, Hash: bytes32(byte(node.height)), Txs: txs,
			DecidedLastCommit: abci.CommitInfo{Round: 0, Votes: votes},
		})
		if err != nil {
			t.Fatalf("height %d: %v", node.height, err)
		}
		if _, err := node.app.Commit(); err != nil {
			t.Fatal(err)
		}
		params, err := res.ConsensusParamUpdates.Marshal()
		if err != nil {
			t.Fatal(err)
		}
		if genesisParams == nil {
			genesisParams = params
		}
		if len(res.ValidatorUpdates) != 0 || !bytes.Equal(params, genesisParams) {
			t.Fatalf("height %d: validator updates %v, consensus params %v", node.height, res.ValidatorUpdates, res.ConsensusParamUpdates)
		}
		if msg == nil {
			return nil
		}
		if anteCommitted(res.TxResults[0]) {
			n.seq[who.hexAddr()]++
		}
		return res.TxResults[0]
	}
	block(nil, nil)
	validatorsBefore := governanceValidators(t, node.app)
	idle := func(blocks int) {
		t.Helper()
		before := takeGovSnap(t, node.app)
		for i := 0; i < blocks; i++ {
			block(nil, nil)
		}
		before.same(t, "blocks every validator signed", node.app, nil)
	}
	steps := []struct {
		id, key string
		payload *authorityv1.ProposalPayload
	}{
		{"cv-limit", msgkit.LimitMaxOpenOffers, governedPayload(msgkit.LimitMaxOpenOffers, 7)},
		{"cv-up", "upgrade", upgradePayload("cv-up", uint64(node.height)+5000, "release cv-up")},
		{"cv-seat", "seat-seat-4", seatPayload("seat-4", sdked.GenPrivKey().PubKey().Bytes())},
	}
	for _, step := range steps {
		wantCode(t, "propose "+step.id, block(n.seats[0], n.proposal(t, step.id, step.key, step.payload)), msgkit.OK)
		idle(5)
		wantProposalState(t, node.app, step.id, authorityv1.ProposalState_PROPOSAL_STATE_OPEN)
		for i := 0; i < 3; i++ {
			seat := n.seats[i]
			wantCode(t, "approve "+step.id, block(seat, approval(seat, fmt.Sprintf("%s-ap-%d", step.id, i), step.id)), msgkit.OK)
		}
		idle(5)
		wantProposalState(t, node.app, step.id, authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
		wantCode(t, "execute "+step.id, block(n.authority, execution(n.authority, "ex-"+step.id, step.id)), msgkit.OK)
		n.policy++
		wantProposalState(t, node.app, step.id, authorityv1.ProposalState_PROPOSAL_STATE_EXECUTED)
	}
	if after := governanceValidators(t, node.app); !proto.Equal(after, validatorsBefore) {
		t.Fatalf("validator set changed: %v -> %v", validatorsBefore, after)
	}
}

// governanceValidators is the validator set Query/Governance reports, as a
// list message for proto.Equal.
func governanceValidators(t *testing.T, chainApp *app.App) *authorityv1.QueryGovernanceResponse {
	t.Helper()
	raw, res := query(t, chainApp, "/svn.authority.v1.Query/Governance", &authorityv1.QueryGovernanceRequest{
		Page: &typesv1.PageRequest{Limit: 100},
	}, 0)
	if res.Code != 0 {
		t.Fatalf("Query/Governance: %s/%d %s", res.Codespace, res.Code, res.Log)
	}
	var out authorityv1.QueryGovernanceResponse
	if err := proto.Unmarshal(raw, &out); err != nil {
		t.Fatal(err)
	}
	if len(out.GetState().GetValidators()) == 0 {
		t.Fatal("Query/Governance reports no validator")
	}
	return &authorityv1.QueryGovernanceResponse{State: &authorityv1.AuthorityState{Validators: out.GetState().GetValidators()}}
}

// pinnedMsgs is every sdk.Msg type the app registers (REQ-CHAIN-015): the
// auth module's MsgUpdateParams, the council's three messages, and the
// registry, lease, and evidence messages. No message moves a balance,
// slashes, mints, delegates, votes in a governance module, or schedules an
// upgrade outside the council. MsgUpdateParams accepts only the svn module
// account address as its authority, which no key controls;
// TestUnauthorizedPolicyWritesFailAtExecution shows the AUTHORITY account
// refused.
var pinnedMsgs = []string{
	"/cosmos.auth.v1beta1.MsgUpdateParams",
	"/svn.authority.v1.MsgApproveChange",
	"/svn.authority.v1.MsgExecuteChange",
	"/svn.authority.v1.MsgProposeChange",
	"/svn.content.v1.MsgRegisterContent",
	"/svn.content.v1.MsgWithdrawContent",
	"/svn.evidence.v1.MsgSubmitAuditBatch",
	"/svn.leases.v1.MsgAcceptLease",
	"/svn.leases.v1.MsgEndLease",
	"/svn.leases.v1.MsgOfferLease",
	"/svn.leases.v1.MsgRenewLease",
	"/svn.providers.v1.MsgDrainProvider",
	"/svn.providers.v1.MsgRegisterProvider",
	"/svn.providers.v1.MsgRotateProviderKey",
	"/svn.providers.v1.MsgUpdateProvider",
}

// TestNoSlashingOrBalanceMessage (REQ-CHAIN-015): the registered sdk.Msg
// types are exactly pinnedMsgs, each with a message-router handler; the
// module set is exactly auth and the seven svn modules (no bank, staking,
// slashing, distribution, mint, gov, upgrade, feegrant, or authz module,
// and no store for one); the council payload alternatives are exactly the
// eight of schema version 2, none of which names a balance, a stake, a
// slash, or a mint; and the binary compiles no upgrade handler, so svnd
// runs no migration. The rehearsal binary's migration and its linked
// symbols are P5a's (plan 12.4).
func TestNoSlashingOrBalanceMessage(t *testing.T) {
	chainApp := newTestApp(t, "shortvideo-dev-msgs", nil)
	got := chainApp.EncodingConfig().InterfaceRegistry.ListImplementations(sdk.MsgInterfaceProtoName)
	sort.Strings(got)
	if strings.Join(got, "\n") != strings.Join(pinnedMsgs, "\n") {
		t.Fatalf("registered sdk.Msg types:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(pinnedMsgs, "\n"))
	}
	for _, typeURL := range got {
		if chainApp.MsgServiceRouter().HandlerByTypeURL(typeURL) == nil {
			t.Fatalf("%s has no message handler", typeURL)
		}
	}
	modules := []string{"auth", app.ModuleSVN, app.ModulePolicy, app.ModuleProviders, app.ModuleContent, app.ModuleLeases, app.ModuleEvidence, authority.ModuleName}
	if strings.Join(chainApp.ModuleNames(), ",") != strings.Join(modules, ",") {
		t.Fatalf("modules %v, want %v", chainApp.ModuleNames(), modules)
	}
	for _, name := range []string{"bank", "staking", "slashing", "distribution", "mint", "gov", "upgrade", "feegrant", "authz", "crisis"} {
		if chainApp.StoreKeyForTest(name) != nil {
			t.Fatalf("a %s store is mounted", name)
		}
	}
	fields := (&authorityv1.ProposalPayload{}).ProtoReflect().Descriptor().Oneofs().ByName("change").Fields()
	var alternatives []string
	for i := 0; i < fields.Len(); i++ {
		alternatives = append(alternatives, fmt.Sprintf("%d:%s", fields.Get(i).Number(), fields.Get(i).Name()))
	}
	want := "1:validator_power,2:governed_limit,3:admit_provider,4:disable_provider,5:force_withdraw,6:software_upgrade,7:cancel_upgrade,8:council_seat"
	if strings.Join(alternatives, ",") != want {
		t.Fatalf("payload alternatives %v, want %s", alternatives, want)
	}
	for i := 0; i < fields.Len(); i++ {
		walkNames(t, fields.Get(i).Message(), map[protoreflect.FullName]bool{})
	}
	if binary := chainApp.Binary(); binary.AppVersion != app.AppVersion || len(binary.Upgrades) != 0 {
		t.Fatalf("svnd binary app version %d with %d upgrade handlers", binary.AppVersion, len(binary.Upgrades))
	}
}

// walkNames fails when a field of desc, or of a message it contains, names
// a balance, coin, stake, slash, or mint.
func walkNames(t *testing.T, desc protoreflect.MessageDescriptor, seen map[protoreflect.FullName]bool) {
	t.Helper()
	if desc == nil || seen[desc.FullName()] {
		return
	}
	seen[desc.FullName()] = true
	for i := 0; i < desc.Fields().Len(); i++ {
		field := desc.Fields().Get(i)
		name := strings.ToLower(string(field.Name()))
		for _, word := range []string{"balance", "coin", "amount", "stake", "slash", "mint", "denom", "delegat"} {
			if strings.Contains(name, word) {
				t.Fatalf("%s.%s names %s", desc.FullName(), field.Name(), word)
			}
		}
		walkNames(t, field.Message(), seen)
	}
}
