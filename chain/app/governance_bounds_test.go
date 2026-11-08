package app_test

import (
	"bytes"
	"fmt"
	"strconv"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// nextHeight is the height of the next block c commits: the height a
// transaction signed now runs at when it is delivered in its own block.
func nextHeight(c *cluster) uint64 { return uint64(c.height()) + 1 }

// blockOf delivers one block that carries one transaction per message, the
// i-th signed by whos[i] at its tracked sequence, and moves each signer's
// tracked sequence once per transaction whose ante committed.
func (n *regNet) blockOf(t *testing.T, c *cluster, whos []*actor, msgs []sdk.Msg) *abci.ResponseFinalizeBlock {
	t.Helper()
	raws := make([][]byte, len(msgs))
	for i, msg := range msgs {
		raws[i] = n.signed(t, c, whos[i], registryGas, nil, msg)
		n.seq[whos[i].hexAddr()]++
	}
	res := c.block(t, raws...)
	for i, result := range res.TxResults {
		if !anteCommitted(result) {
			n.seq[whos[i].hexAddr()]--
		}
	}
	return res
}

// pendingOf is the pending change of key that Query/Policy reports, or nil.
func pendingOf(t *testing.T, chainApp *app.App, key string) *policyv1.PendingLimitChange {
	t.Helper()
	for _, row := range queryPolicy(t, chainApp).GetPendingLimitChanges() {
		if row.GetLimitKey() == key {
			return row
		}
	}
	return nil
}

// activations are the policy_activated events of one finalized block.
func activations(res *abci.ResponseFinalizeBlock) []abci.Event {
	var out []abci.Event
	for _, ev := range res.Events {
		if ev.Type == policy.EventPolicyActivated {
			out = append(out, ev)
		}
	}
	return out
}

// splitBeginBlock returns the expiry items of one finalized block
// (beginBlockExpiry over every event but policy_activated) and the index
// in res.Events of its first policy_activated event and of its first
// expiry event (-1 when there is none).
func splitBeginBlock(t *testing.T, res *abci.ResponseFinalizeBlock) (items []expiryItem, firstActivation, firstExpiry int) {
	t.Helper()
	firstActivation, firstExpiry = -1, -1
	var rest []abci.Event
	for i, ev := range res.Events {
		if ev.Type == policy.EventPolicyActivated {
			if firstActivation < 0 {
				firstActivation = i
			}
			continue
		}
		if firstExpiry < 0 && len(ev.Attributes) > 0 && ev.Attributes[len(ev.Attributes)-1].Key == msgkit.ModeKey {
			firstExpiry = i
		}
		rest = append(rest, ev)
	}
	items, _ = beginBlockExpiry(t, &abci.ResponseFinalizeBlock{Events: rest})
	return items, firstActivation, firstExpiry
}

// TestInvalidBounds (acceptance check "invalid bounds"): every governed
// key at value 0 and at its ceiling + 1, applied at execute or scheduled,
// and a key that is not a governed limit key (an unknown name, the
// safety-cap names, compile-time quota names) is ERROR_CODE_BOUNDS at
// propose (D-1) with the state unchanged. Activation heights at propose
// (block height h): h is ERROR_CODE_OUT_OF_RANGE, h + 1 and h +
// MaxActivationLeadBlocks are accepted, h + MaxActivationLeadBlocks + 1
// and 2^53 are ERROR_CODE_BOUNDS. Upgrade plan heights at propose: 0, h,
// h + 1, and h + MinActivationLeadBlocks - 1 are OUT_OF_RANGE, h +
// MinActivationLeadBlocks and h + MaxActivationLeadBlocks are accepted, h
// + MaxActivationLeadBlocks + 1 and 2^53 are BOUNDS (both lead numbers are
// PROPOSED, owner question Q3, "3 of 4 + min notice (Recommended)"). At
// execute (block height e) the window is checked again: an activation
// height e and a plan height e + MinActivationLeadBlocks - 1, both
// accepted at propose, are OUT_OF_RANGE and the proposal stays QUEUED; e
// + 1 and e + MinActivationLeadBlocks execute and store the pending change
// and the plan. The window's upper end cannot be crossed between propose
// and execute (the lead only shrinks), so its execute check is covered at
// keeper level (x/authority TestKeeperLeadBoundaries). At the end the
// safety caps Query/Policy reports are the protocol caps, and the only
// value in force below its ceiling is the executed scheduled change.
func TestInvalidBounds(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	s1 := n.seats[0]
	row := 0
	refuse := func(label, key string, payload *authorityv1.ProposalPayload, want msgkit.Code) {
		t.Helper()
		row++
		n.refuseCode(t, c, label, s1, n.anyProposal(t, s1, fmt.Sprintf("ib-%03d", row), key, payload), want)
	}
	accept := func(label, key string, payload *authorityv1.ProposalPayload) string {
		t.Helper()
		row++
		id := fmt.Sprintf("ib-%03d", row)
		wantCode(t, label, n.send(t, c, s1, n.proposal(t, id, key, payload)), msgkit.OK)
		wantProposalState(t, chainApp, id, authorityv1.ProposalState_PROPOSAL_STATE_OPEN)
		return id
	}
	for _, key := range msgkit.LimitKeys() {
		ceiling, _ := msgkit.Ceiling(key)
		for _, value := range []uint64{0, ceiling + 1} {
			refuse(fmt.Sprintf("%s = %d", key, value), key, governedPayload(key, value), msgkit.Bounds)
			refuse(fmt.Sprintf("%s = %d scheduled", key, value), key, scheduledPayload(key, value, nextHeight(c)+10), msgkit.Bounds)
		}
	}
	for _, key := range []string{
		"max_unknown_limit", "max_tx_bytes", "max_gas_per_tx", "min_gas_per_tx", "max_renditions", "max_endpoints",
		"max_open_proposals_per_seat", "max_open_offers_per_content", "max_pending_changes",
	} {
		refuse("not a governed key: "+key, key, governedPayload(key, 1), msgkit.Bounds)
		refuse("not a governed key, scheduled: "+key, key, scheduledPayload(key, 1, nextHeight(c)+10), msgkit.Bounds)
	}
	const safeAbove = uint64(genesis.SafeIntegerMax) + 1
	limit := msgkit.LimitMaxOpenOffers
	refuse("activation at the block height", limit, scheduledPayload(limit, 5, nextHeight(c)), msgkit.OutOfRange)
	refuse("activation one above the max lead", limit, scheduledPayload(limit, 5, nextHeight(c)+msgkit.MaxActivationLeadBlocks+1), msgkit.Bounds)
	refuse("activation above safe_integer_max", limit, scheduledPayload(limit, 5, safeAbove), msgkit.Bounds)
	accept("activation one above the block height", limit, scheduledPayload(limit, 5, nextHeight(c)+1))
	accept("activation at the max lead", limit, scheduledPayload(limit, 5, nextHeight(c)+msgkit.MaxActivationLeadBlocks))
	for i, height := range []func() uint64{
		func() uint64 { return 0 },
		func() uint64 { return nextHeight(c) },
		func() uint64 { return nextHeight(c) + 1 },
		func() uint64 { return nextHeight(c) + msgkit.MinActivationLeadBlocks - 1 },
	} {
		name := fmt.Sprintf("ib-plan-low-%d", i)
		refuse(fmt.Sprintf("plan height below the min lead (%d)", i), "upgrade", upgradePayload(name, height(), "release "+name), msgkit.OutOfRange)
	}
	refuse("plan height one above the max lead", "upgrade", upgradePayload("ib-plan-high", nextHeight(c)+msgkit.MaxActivationLeadBlocks+1, "release high"), msgkit.Bounds)
	refuse("plan height above safe_integer_max", "upgrade", upgradePayload("ib-plan-safe", safeAbove, "release safe"), msgkit.Bounds)
	accept("plan height at the min lead", "upgrade", upgradePayload("ib-plan-min", nextHeight(c)+msgkit.MinActivationLeadBlocks, "release min"))
	accept("plan height at the max lead", "upgrade", upgradePayload("ib-plan-max", nextHeight(c)+msgkit.MaxActivationLeadBlocks, "release max"))

	// queueFor proposes and approves a payload built for execute height e,
	// the block right after the three approvals, and returns the id.
	queueFor := func(id, key string, payload func(e uint64) *authorityv1.ProposalPayload) {
		t.Helper()
		e := nextHeight(c) + 4
		n.councilReady(t, c, id, key, payload(e))
		if nextHeight(c) != e {
			t.Fatalf("%s: execute height %d, want %d", id, nextHeight(c), e)
		}
	}
	queueFor("ib-ex-act-low", limit, func(e uint64) *authorityv1.ProposalPayload { return scheduledPayload(limit, 5, e) })
	n.refuseCode(t, c, "execute: activation at the execute height", n.authority, execution(n.authority, "ex-ib-ex-act-low", "ib-ex-act-low"), msgkit.OutOfRange)
	wantProposalState(t, chainApp, "ib-ex-act-low", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	var activation uint64
	queueFor("ib-ex-act-ok", limit, func(e uint64) *authorityv1.ProposalPayload {
		activation = e + 1
		return scheduledPayload(limit, 5, activation)
	})
	wantCode(t, "execute: activation one above the execute height", n.execute(t, c, "ib-ex-act-ok"), msgkit.OK)
	n.policy++
	if p := pendingOf(t, chainApp, limit); p == nil || p.GetActivationHeight() != activation || p.GetValue() != 5 || p.GetProposalId() != "ib-ex-act-ok" {
		t.Fatalf("pending change after the execute: %v", p)
	}
	queueFor("ib-ex-plan-low", "upgrade", func(e uint64) *authorityv1.ProposalPayload {
		return upgradePayload("ib-ex-plan", e+msgkit.MinActivationLeadBlocks-1, "release ex")
	})
	n.refuseCode(t, c, "execute: plan height one below the min lead", n.authority, execution(n.authority, "ex-ib-ex-plan-low", "ib-ex-plan-low"), msgkit.OutOfRange)
	wantProposalState(t, chainApp, "ib-ex-plan-low", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	var planHeight uint64
	queueFor("ib-ex-plan-ok", "upgrade", func(e uint64) *authorityv1.ProposalPayload {
		planHeight = e + msgkit.MinActivationLeadBlocks
		return upgradePayload("ib-ex-plan", planHeight, "release ex")
	})
	wantCode(t, "execute: plan height at the min lead", n.execute(t, c, "ib-ex-plan-ok"), msgkit.OK)
	n.policy++
	plan := queryPolicy(t, chainApp).GetUpgradePlan()
	release := sha([]byte("release ex"))
	if plan.GetName() != "ib-ex-plan" || plan.GetHeight() != planHeight || plan.GetProposalId() != "ib-ex-plan-ok" ||
		plan.GetReleaseDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 || !bytes.Equal(plan.GetReleaseDigest().GetValue(), release) {
		t.Fatalf("plan after the execute: %v", plan)
	}
	state := queryPolicy(t, chainApp)
	if !proto.Equal(state.GetSafetyCaps(), policy.DefaultSafetyCaps()) {
		t.Fatalf("safety caps changed: %v", state.GetSafetyCaps())
	}
	if uint64(c.height()) < activation {
		t.Fatalf("height %d is below the activation height %d", c.height(), activation)
	}
	for _, key := range msgkit.LimitKeys() {
		want, _ := msgkit.Ceiling(key)
		if key == limit {
			want = 5
		}
		if v := governedValue(t, chainApp, key); v != want {
			t.Fatalf("%s in force %d at the end, want %d: only the executed change applies", key, v, want)
		}
	}
}

// TestActivationTiming (acceptance check "activation timing"): with a
// labelled DEC-008 review delay of 100 seconds, a scheduled governed change
// of max_package_bytes to 1000 at activation height a:
//
//   - its execute at header time execute_after - 1 is ERROR_CODE_OUT_OF_RANGE
//     (the proposal stays QUEUED, state unchanged) and commits at
//     execute_after;
//   - at a - 1 Query/Policy reports the ceiling in force and the pending
//     change (value, height, proposal, and the policy version its execute
//     produced), and a 1001-byte content registration commits;
//   - block a emits policy_activated in BeginBlock, Query/Policy reports
//     1000 in force and no pending change, the policy version is the same
//     as at a - 1 (it moved at execute, not at activation), a 1001-byte
//     registration is ERROR_CODE_BOUNDS and a 1000-byte one commits;
//   - the execute block, the activation block, and the two blocks after
//     each return no validator update and Query/Governance reports the
//     genesis validators (a governed change is not a validator change;
//     CometBFT applies validator updates two heights later).
func TestActivationTiming(t *testing.T) {
	n := newRegNet()
	for i := range n.doc.DevnetTestParameters {
		if n.doc.DevnetTestParameters[i].DecisionID == "DEC-008" {
			n.doc.DevnetTestParameters[i].DelaySeconds = 100
		}
	}
	c := n.start(t, 1)
	chainApp := c.app()
	validators := governanceValidators(t, chainApp)
	key := msgkit.LimitMaxPackageBytes
	ceiling, _ := msgkit.Ceiling(key)
	start := nextHeight(c)
	a := start + 10
	n.councilReady(t, c, "at-sched", key, scheduledPayload(key, 1000, a))
	approved := c.now()
	c.setUnix(approved + 99)
	n.refuseCode(t, c, "execute at execute_after - 1", n.authority, execution(n.authority, "ex-at-sched", "at-sched"), msgkit.OutOfRange)
	wantProposalState(t, chainApp, "at-sched", authorityv1.ProposalState_PROPOSAL_STATE_QUEUED)
	c.setUnix(approved + 100)
	executed := n.execute(t, c, "at-sched")
	wantCode(t, "execute at execute_after", executed, msgkit.OK)
	n.policy++
	quiet := []*abci.ResponseFinalizeBlock{c.blocks[len(c.blocks)-1], c.block(t), c.block(t)}
	for uint64(c.height()) < a-2 {
		c.block(t)
	}
	before := queryPolicy(t, chainApp)
	res := n.blockOf(t, c, []*actor{n.service}, []sdk.Msg{n.registerContent(t, n.service, "reg-at-1", "at-1", 1, packageManifest("at-1"), 1001)})
	if uint64(c.height()) != a-1 {
		t.Fatalf("height %d, want a-1 = %d", c.height(), a-1)
	}
	wantCode(t, "1001 bytes at a-1", res.TxResults[0], msgkit.OK)
	if len(activations(res)) != 0 {
		t.Fatalf("block a-1 activated %v", activations(res))
	}
	if v := governedValue(t, chainApp, key); v != ceiling {
		t.Fatalf("max_package_bytes in force at a-1: %d", v)
	}
	if p := pendingOf(t, chainApp, key); p == nil || p.GetValue() != 1000 || p.GetActivationHeight() != a || p.GetProposalId() != "at-sched" || p.GetPolicyVersion() != n.policy {
		t.Fatalf("pending change at a-1: %v (tracked version %d)", p, n.policy)
	}
	res = n.blockOf(t, c, []*actor{n.service, n.service}, []sdk.Msg{
		n.registerContent(t, n.service, "reg-at-2", "at-2", 1, packageManifest("at-2"), 1001),
		n.registerContent(t, n.service, "reg-at-3", "at-3", 1, packageManifest("at-3"), 1000),
	})
	wantCode(t, "1001 bytes at a", res.TxResults[0], msgkit.Bounds)
	wantCode(t, "1000 bytes at a", res.TxResults[1], msgkit.OK)
	got := activations(res)
	if len(got) != 1 || eventAttr(got, policy.EventPolicyActivated, policy.AttrParameterKey) != key ||
		eventAttr(got, policy.EventPolicyActivated, policy.AttrValue) != "1000" ||
		eventAttr(got, policy.EventPolicyActivated, policy.AttrActivationHeight) != strconv.FormatUint(a, 10) ||
		eventAttr(got, policy.EventPolicyActivated, policy.AttrProposalID) != "at-sched" ||
		eventAttr(got, policy.EventPolicyActivated, msgkit.ModeKey) != "BeginBlock" {
		t.Fatalf("block a activations %v", got)
	}
	after := queryPolicy(t, chainApp)
	if v := governedValue(t, chainApp, key); v != 1000 || len(after.GetPendingLimitChanges()) != 0 || after.GetPolicyVersion() != before.GetPolicyVersion() {
		t.Fatalf("at a: in force %d, pending %v, policy version %d (a-1: %d)", v, after.GetPendingLimitChanges(), after.GetPolicyVersion(), before.GetPolicyVersion())
	}
	quiet = append(quiet, res, c.block(t), c.block(t))
	for i, block := range quiet {
		if len(block.ValidatorUpdates) != 0 {
			t.Fatalf("quiet block %d: validator updates %v", i, block.ValidatorUpdates)
		}
	}
	if got := governanceValidators(t, chainApp); !proto.Equal(got, validators) {
		t.Fatalf("validators changed: %v", got)
	}
}

// TestImmediateChangeSupersedesPending (DR-10-32, invariant I3; app
// level): a scheduled change of max_page_limit to 40 at height a, then an
// immediate change to 50 executed before a: the immediate execute emits
// policy_pending with activation height 0 naming the superseded proposal,
// Query/Policy reports 50 in force and no pending change, and at a - 1, a,
// and a + 1 the value in force stays 50 (a Providers page of 50 is
// accepted and one of 51 is ERROR_CODE_BOUNDS) with no policy_activated
// event. The reverse order also ends with the latest decision: an
// immediate change to 30, then a scheduled change to 45 at a2: 30 at a2 -
// 1, 45 at a2.
func TestImmediateChangeSupersedesPending(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	key := msgkit.LimitMaxPageLimit
	a := nextHeight(c) + 2*councilBlocks + 6
	n.council(t, c, "is-sched", key, scheduledPayload(key, 40, a))
	res := n.council(t, c, "is-imm", key, governedPayload(key, 50))
	if eventAttr(res.Events, policy.EventPolicyPending, policy.AttrSupersededProposalID) != "is-sched" ||
		eventAttr(res.Events, policy.EventPolicyPending, policy.AttrActivationHeight) != "0" ||
		eventAttr(res.Events, policy.EventPolicyPending, policy.AttrValue) != "50" {
		t.Fatalf("the immediate execute's policy_pending event: %v", res.Events)
	}
	if v := governedValue(t, chainApp, key); v != 50 || pendingOf(t, chainApp, key) != nil {
		t.Fatalf("after the immediate change: in force %d, pending %v", v, pendingOf(t, chainApp, key))
	}
	for uint64(c.height()) < a-2 {
		c.block(t)
	}
	for _, at := range []string{"a-1", "a", "a+1"} {
		block := c.block(t)
		if len(activations(block)) != 0 {
			t.Fatalf("%s: superseded change activated: %v", at, activations(block))
		}
		if v := governedValue(t, chainApp, key); v != 50 {
			t.Fatalf("%s: max_page_limit in force %d", at, v)
		}
		if code, _ := providersPage(t, c, 50); code != 0 {
			t.Fatalf("%s: a page of 50 is refused with %d", at, code)
		}
		assertQueryCode(t, chainApp, "/svn.providers.v1.Query/Providers", &providersv1.QueryProvidersRequest{Page: &typesv1.PageRequest{Limit: 51}}, msgkit.Bounds)
	}
	if uint64(c.height()) != a+1 {
		t.Fatalf("height %d, want a+1 = %d", c.height(), a+1)
	}

	n.council(t, c, "is-imm-2", key, governedPayload(key, 30))
	a2 := nextHeight(c) + councilBlocks + 3
	n.council(t, c, "is-sched-2", key, scheduledPayload(key, 45, a2))
	for uint64(c.height()) < a2-1 {
		c.block(t)
	}
	if v := governedValue(t, chainApp, key); v != 30 {
		t.Fatalf("a2-1: max_page_limit in force %d", v)
	}
	if block := c.block(t); len(activations(block)) != 1 {
		t.Fatalf("a2: activations %v", activations(block))
	}
	if v := governedValue(t, chainApp, key); v != 45 {
		t.Fatalf("a2: max_page_limit in force %d", v)
	}
}

// TestLeaseAtActivationMinusOne (plan 5.2, finding SS-12): the policy
// version names the executed decisions, not the limits in force. A
// scheduled change of max_open_offers to 1 at height a moves the policy
// version from v to v+1 at execute. In block a - 1 p1 holds one open
// offer, a second offer on p1 commits (the old limit governs), and the
// acceptance of the first offer creates lease l1 that records policy
// version v+1. In block a p1 holds one open offer again and a further
// offer on p1 is ERROR_CODE_BOUNDS (the new limit 1 governs), an offer on
// p2 (no open offer) commits, and lease l2 accepted in block a records the
// same version v+1: activation does not move the policy version.
func TestLeaseAtActivationMinusOne(t *testing.T) {
	n := newRegNet()
	c := n.start(t, 1)
	chainApp := c.app()
	leaseBasics(t, n, c)
	now := c.now()
	deadline, expiry := now+1800, now+600
	o1 := n.offerMsg(n.service, "of-o1", "o1", "c1", "p1", sizeSmall, deadline, expiry)
	wantCode(t, "offer o1", n.send(t, c, n.service, o1), msgkit.OK)
	key := msgkit.LimitMaxOpenOffers
	before := n.policy
	a := nextHeight(c) + councilBlocks + 3
	n.council(t, c, "lam-sched", key, scheduledPayload(key, 1, a))
	if n.policy != before+1 {
		t.Fatalf("policy version %d after the execute, was %d", n.policy, before)
	}
	for uint64(c.height()) < a-2 {
		c.block(t)
	}
	o2 := n.offerMsg(n.service, "of-o2", "o2", "c2", "p1", sizeSmall, deadline, expiry)
	res := n.blockOf(t, c, []*actor{n.service, n.broadcaster}, []sdk.Msg{o2, n.acceptMsg(t, n.broadcaster, "ac-l1", o1, "l1")})
	if uint64(c.height()) != a-1 {
		t.Fatalf("height %d, want a-1 = %d", c.height(), a-1)
	}
	wantCode(t, "a-1: second offer on p1", res.TxResults[0], msgkit.OK)
	wantCode(t, "a-1: accept o1", res.TxResults[1], msgkit.OK)
	if v := governedValue(t, chainApp, key); v != 1024 {
		t.Fatalf("max_open_offers in force at a-1: %d", v)
	}
	if l1 := heldLease(t, chainApp, "p1", "l1", 0); l1 == nil || l1.GetPolicyVersion() != n.policy {
		t.Fatalf("l1 at a-1: %v, want policy version %d", l1, n.policy)
	}
	o3 := n.offerMsg(n.service, "of-o3", "o3", "c3", "p1", sizeSmall, deadline, expiry)
	o4 := n.offerMsg(n.service, "of-o4", "o4", "c1", "p2", sizeSmall, deadline, expiry)
	res = n.blockOf(t, c, []*actor{n.service, n.service, n.broadcaster}, []sdk.Msg{o3, o4, n.acceptMsg(t, n.broadcaster, "ac-l2", o2, "l2")})
	wantCode(t, "a: another offer on p1", res.TxResults[0], msgkit.Bounds)
	wantCode(t, "a: an offer on p2", res.TxResults[1], msgkit.OK)
	wantCode(t, "a: accept o2", res.TxResults[2], msgkit.OK)
	if len(activations(res)) != 1 {
		t.Fatalf("block a activations %v", activations(res))
	}
	if l2 := heldLease(t, chainApp, "p1", "l2", 0); l2 == nil || l2.GetPolicyVersion() != n.policy {
		t.Fatalf("l2 at a: %v, want policy version %d", l2, n.policy)
	}
	if v := queryPolicy(t, chainApp).GetPolicyVersion(); v != n.policy {
		t.Fatalf("policy version %d at a, tracked %d", v, n.policy)
	}
	c.checkInvariants(t, "after the activation")
}

// TestEveryGovernedKeyAtFloorAndCeiling: each governed key set by an
// executed council change to 1, then back to its ceiling, governs the
// module that reads it, and blocks keep committing:
//
//   - max_package_bytes 1: a 2-byte content is ERROR_CODE_BOUNDS, a 1-byte
//     content commits; at the ceiling a 2-byte content commits;
//   - max_page_limit 1: Providers and Query/Governance pages of 2 are
//     BOUNDS, a Providers page of 1 (and of 0, the default) holds one
//     provider; at the ceiling a page of 100 is accepted;
//   - max_open_offers 1: a second open offer on p1 is BOUNDS; at the
//     ceiling it commits;
//   - max_evidence_batch 1: a batch of two items is BOUNDS, a batch of one
//     commits; at the ceiling a batch of two commits;
//   - max_expiry_steps 1: the expiry backlog (eight leases and two open
//     offers due at one time) drains one item per block, then the ceiling
//     is set again.
//
// Every refused message leaves the state unchanged.
func TestEveryGovernedKeyAtFloorAndCeiling(t *testing.T) {
	e := newEvNet()
	n := e.regNet
	c := n.start(t, 1)
	chainApp := c.app()
	fx := evidenceSetup(t, e, c)
	set := func(key string, value uint64) {
		t.Helper()
		n.council(t, c, fmt.Sprintf("fc-%s-%d", key, value), key, governedPayload(key, value))
		if v := governedValue(t, chainApp, key); v != value {
			t.Fatalf("%s in force %d after setting %d", key, v, value)
		}
		c.block(t)
	}
	ceiling := func(key string) uint64 {
		v, _ := msgkit.Ceiling(key)
		return v
	}

	set(msgkit.LimitMaxPackageBytes, 1)
	n.refuseCode(t, c, "2-byte content at max_package_bytes 1", n.service, n.registerContent(t, n.service, "reg-fc-2", "fc-2", 1, packageManifest("fc-2"), 2), msgkit.Bounds)
	wantCode(t, "1-byte content at max_package_bytes 1", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-fc-1", "fc-1", 1, packageManifest("fc-1"), 1)), msgkit.OK)
	set(msgkit.LimitMaxPackageBytes, ceiling(msgkit.LimitMaxPackageBytes))
	wantCode(t, "2-byte content at the ceiling", n.send(t, c, n.service, n.registerContent(t, n.service, "reg-fc-2b", "fc-2b", 1, packageManifest("fc-2b"), 2)), msgkit.OK)

	set(msgkit.LimitMaxPageLimit, 1)
	assertQueryCode(t, chainApp, "/svn.providers.v1.Query/Providers", &providersv1.QueryProvidersRequest{Page: &typesv1.PageRequest{Limit: 2}}, msgkit.Bounds)
	assertQueryCode(t, chainApp, "/svn.authority.v1.Query/Governance", &authorityv1.QueryGovernanceRequest{Page: &typesv1.PageRequest{Limit: 2}}, msgkit.Bounds)
	for _, limit := range []uint32{1, 0} {
		if code, page := providersPage(t, c, limit); code != 0 || len(page.GetProviders()) != 1 || len(page.GetPage().GetNextPageKey()) == 0 {
			t.Fatalf("page limit %d at max_page_limit 1: code %d, %d providers", limit, code, len(page.GetProviders()))
		}
	}
	set(msgkit.LimitMaxPageLimit, ceiling(msgkit.LimitMaxPageLimit))
	if code, page := providersPage(t, c, 100); code != 0 || len(page.GetProviders()) != 2 {
		t.Fatalf("page limit 100 at the ceiling: code %d, %d providers", code, len(page.GetProviders()))
	}

	now := c.now()
	set(msgkit.LimitMaxOpenOffers, 1)
	wantCode(t, "first open offer at max_open_offers 1", n.send(t, c, n.service, n.offerMsg(n.service, "of-fc-o1", "fc-o1", "fc-1", "p1", 1, now+1800, now+600)), msgkit.OK)
	n.refuseCode(t, c, "second open offer at max_open_offers 1", n.service, n.offerMsg(n.service, "of-fc-o2", "fc-o2", "fc-2b", "p1", 2, now+1800, now+600), msgkit.Bounds)
	set(msgkit.LimitMaxOpenOffers, ceiling(msgkit.LimitMaxOpenOffers))
	wantCode(t, "second open offer at the ceiling", n.send(t, c, n.service, n.offerMsg(n.service, "of-fc-o2", "fc-o2", "fc-2b", "p1", 2, now+1800, now+600)), msgkit.OK)

	auditor := prefix(e.auditor1)
	item := func(name, lease, content string) *evidencev1.EvidenceItem {
		return auditItem(auditor+name, lease, content, manifestOf(content), now+600)
	}
	batch := func(bid string, items ...*evidencev1.EvidenceItem) sdk.Msg {
		return e.audit(t, e.auditor1, bid, auditor+bid, c.epoch(), items...)
	}
	set(msgkit.LimitMaxEvidenceBatch, 1)
	n.refuseCode(t, c, "two items at max_evidence_batch 1", e.auditor1, batch("fc-b2", item("fc-e1", "la", "c1"), item("fc-e2", "lb", "c2")), msgkit.Bounds)
	wantCode(t, "one item at max_evidence_batch 1", n.send(t, c, e.auditor1, batch("fc-b1", item("fc-e3", "la", "c1"))), msgkit.OK)
	set(msgkit.LimitMaxEvidenceBatch, ceiling(msgkit.LimitMaxEvidenceBatch))
	wantCode(t, "two items at the ceiling", n.send(t, c, e.auditor1, batch("fc-b3", item("fc-e4", "lb", "c2"), item("fc-e5", "lc", "c3"))), msgkit.OK)

	set(msgkit.LimitMaxExpirySteps, 1)
	c.setUnix(fx.deadline)
	total := 0
	for blocks := 1; ; blocks++ {
		items, _, _ := splitBeginBlock(t, c.block(t))
		if len(items) > 1 {
			t.Fatalf("drain block %d closed %d items at max_expiry_steps 1", blocks, len(items))
		}
		if len(items) == 0 {
			if total != len(fixtureLeases)+2 {
				t.Fatalf("drained %d items, want %d", total, len(fixtureLeases)+2)
			}
			break
		}
		total++
		c.checkInvariants(t, fmt.Sprintf("drain block %d", blocks))
	}
	set(msgkit.LimitMaxExpirySteps, ceiling(msgkit.LimitMaxExpirySteps))
	for _, key := range msgkit.LimitKeys() {
		if v := governedValue(t, chainApp, key); v != ceiling(key) {
			t.Fatalf("%s in force %d at the end", key, v)
		}
	}
}

// TestActivationGovernsSameBlockExpiryD8 (x/policy deviation D-8): x/policy
// BeginBlock runs before x/leases BeginBlock, so a governed change that
// activates at height a governs the lease expiry of block a. Eight items
// (six leases and two open offers) fall due at header time T. On one
// chain max_expiry_steps 2 activates at the block whose header time is T:
// that block emits policy_activated before its first expiry event and
// closes exactly two items, and the backlog drains two per block. On a
// second chain the same change activates one block later: the block at T
// still runs with the ceiling and closes all eight. Block a - 1 (header
// time T - 1) closes nothing on either chain.
func TestActivationGovernsSameBlockExpiryD8(t *testing.T) {
	for _, tc := range []struct {
		name   string
		offset uint64
		counts []int
	}{
		{"activation at the due block", 0, []int{2, 2, 2, 2}},
		{"activation one block later", 1, []int{8}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n := newRegNet()
			c := n.start(t, 1)
			due := c.now() + 3000
			leaseExpirySetup(t, n, c, due)
			dueHeight := nextHeight(c) + councilBlocks + 2
			a := dueHeight + tc.offset
			n.council(t, c, "d8-sched", msgkit.LimitMaxExpirySteps, scheduledPayload(msgkit.LimitMaxExpirySteps, 2, a))
			c.setUnix(due - 1)
			for nextHeight(c) < dueHeight {
				if items, _, _ := splitBeginBlock(t, c.block(t)); len(items) != 0 {
					t.Fatalf("height %d (header time T-1) closed %v", c.height(), items)
				}
			}
			c.setUnix(due)
			var counts []int
			for len(counts) < 10 {
				res := c.block(t)
				items, firstActivation, firstExpiry := splitBeginBlock(t, res)
				if uint64(c.height()) == a {
					if firstActivation < 0 || (firstExpiry >= 0 && firstActivation > firstExpiry) {
						t.Fatalf("block a: activation event at %d, first expiry event at %d", firstActivation, firstExpiry)
					}
				} else if firstActivation >= 0 {
					t.Fatalf("height %d (a = %d) emitted policy_activated", c.height(), a)
				}
				if len(items) == 0 {
					break
				}
				counts = append(counts, len(items))
				c.checkInvariants(t, fmt.Sprintf("height %d", c.height()))
			}
			if fmt.Sprint(counts) != fmt.Sprint(tc.counts) {
				t.Fatalf("items closed per block %v, want %v", counts, tc.counts)
			}
			if v := governedValue(t, c.app(), msgkit.LimitMaxExpirySteps); v != 2 {
				t.Fatalf("max_expiry_steps in force %d", v)
			}
		})
	}
}
