package authority

import (
	"fmt"
	"slices"
	"strconv"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Keeper-level council acceptance tests (goal 10 acceptance: duplicate
// votes, insufficient approval, invalid bounds, concurrent proposals, expiry
// and review-delay boundaries, unauthorized writes). Each runs the keeper
// handlers on a real IAVL store with the SDK gas store, in finalize mode, so
// every refusal happens at execution, and compares the whole authority store
// (and the registry hook store where a hook is wired) before and after each
// refusal. The app-level tests through FinalizeBlock are separate.

// refused runs one keeper call that must fail with code, and checks that
// the authority store, the registry store and the x/policy store are
// unchanged and that no typed event was emitted.
func (e *keeperEnv) refused(t *testing.T, name, code string, call func() ([]string, error)) {
	t.Helper()
	before, beforeRegistry, beforePolicy := dumpKV(e.kv()), dumpKV(e.registryKV()), dumpKV(e.policyKV())
	typed, err := call()
	assertCode(t, err, code)
	wantUnchanged(t, e.kv(), before, name)
	wantUnchanged(t, e.registryKV(), beforeRegistry, name+" (registry store)")
	wantUnchanged(t, e.policyKV(), beforePolicy, name+" (policy store)")
	if len(typed) != 0 {
		t.Fatalf("%s emitted typed events %v", name, typed)
	}
}

// typedNames lists the full names of decoded typed events.
func typedNames(events []proto.Message) []string {
	out := make([]string, 0, len(events))
	for _, ev := range events {
		out = append(out, string(proto.MessageName(ev)))
	}
	return out
}

func (e *keeperEnv) proposeAs(t *testing.T, k Keeper, signer []byte, id string, msg *authorityv1.MsgProposeChange) ([]string, error) {
	ctx := e.txCtx()
	_, err := k.Propose(ctx, propose(signer, id, e.policy(t), msg))
	typed, _ := typedEvents(t, ctx.EventManager().Events())
	return typedNames(typed), err
}

func (e *keeperEnv) approveAs(t *testing.T, k Keeper, signer []byte, business, id string) ([]string, error) {
	ctx := e.txCtx()
	_, err := k.Approve(ctx, approve(signer, business, id))
	typed, _ := typedEvents(t, ctx.EventManager().Events())
	return typedNames(typed), err
}

func (e *keeperEnv) executeAs(t *testing.T, k Keeper, signer []byte, business, id string) ([]string, error) {
	ctx := e.txCtx()
	_, err := k.Execute(ctx, execute(signer, business, id))
	typed, _ := typedEvents(t, ctx.EventManager().Events())
	return typedNames(typed), err
}

// TestKeeperDuplicateVotes: a seat approves once. The same seat with a new
// business id is REPLAY; the same seat with the same business id is an
// idempotent retry that writes nothing; another seat reusing that business
// id is REPLAY; after the seat's key rotates, the new key's approval is
// still REPLAY (approvals are counted by seat id). The proposal queues at
// exactly three distinct seats.
func TestKeeperDuplicateVotes(t *testing.T) {
	e := readyKeeper(t)
	k := e.keeper
	if _, err := e.proposeAs(t, k, e.seats[0].Signer, "dv", governedMsg(msgkit.LimitMaxPageLimit, 10)); err != nil {
		t.Fatal(err)
	}
	if _, err := e.approveAs(t, k, e.seats[1].Signer, "dv-1", "dv"); err != nil {
		t.Fatal(err)
	}
	e.refused(t, "same seat, new business id", CodeReplay, func() ([]string, error) {
		return e.approveAs(t, k, e.seats[1].Signer, "dv-1-again", "dv")
	})
	e.refused(t, "another seat reusing the business id", CodeReplay, func() ([]string, error) {
		return e.approveAs(t, k, e.seats[2].Signer, "dv-1", "dv")
	})
	before := dumpKV(e.kv())
	ctx := e.txCtx()
	if _, err := k.Approve(ctx, approve(e.seats[1].Signer, "dv-1", "dv")); err != nil {
		t.Fatal(err)
	}
	if _, idempotent := resultCode(t, ctx); !idempotent {
		t.Fatal("the same approval again is not an idempotent retry")
	}
	wantUnchanged(t, e.kv(), before, "idempotent approval")

	st := loadAt(t, e.kv(), e.height, e.unix)
	if out := st.RotateCouncilSeat(e.seats[1].ID, pub(70)); !out.Success() {
		t.Fatal(out)
	}
	e.refused(t, "rotated seat key approves again", CodeReplay, func() ([]string, error) {
		return e.approveAs(t, k, SeatAddress(pub(70)), "dv-1-rotated", "dv")
	})
	if rec := recordOf(t, e.kv(), "dv"); !slices.Equal(rec.Approvers, []string{e.seats[1].ID}) || rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
		t.Fatalf("after duplicates: %v %v", rec.Approvers, rec.state())
	}
	if _, err := e.approveAs(t, k, e.seats[2].Signer, "dv-2", "dv"); err != nil {
		t.Fatal(err)
	}
	if rec := recordOf(t, e.kv(), "dv"); rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
		t.Fatalf("two seats queued the proposal: %v", rec.state())
	}
	if _, err := e.approveAs(t, k, e.seats[3].Signer, "dv-3", "dv"); err != nil {
		t.Fatal(err)
	}
	if rec := recordOf(t, e.kv(), "dv"); rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED || len(rec.Approvers) != ApprovedThreshold {
		t.Fatalf("three seats: %v %v", rec.Approvers, rec.state())
	}
}

// TestKeeperInsufficientApproval: for every v0 payload kind, an execute
// with zero, one or two approvals is CONFLICT and writes nothing, the hook
// is not called, and the proposal stays OPEN; the third approval queues it
// and the execute succeeds.
func TestKeeperInsufficientApproval(t *testing.T) {
	e, _ := registryKeeper(t)
	hooks := &storeHooks{key: e.registryKey, code: msgkit.OK}
	k := e.keeper.WithRegistry(hooks, hooks)
	rows := []struct {
		id  string
		msg *authorityv1.MsgProposeChange
	}{
		{"ia-power", powerMsg("v5", pub(5), 1, uint64(e.height)+20)},
		{"ia-limit", governedMsg(msgkit.LimitMaxExpirySteps, 64)},
		{"ia-admit", admitProposal("prov-1")},
		{"ia-disable", disableProposal("prov-1")},
		{"ia-withdraw", withdrawProposal("content-1", 1, reasonValue)},
	}
	for _, row := range rows {
		if _, err := e.proposeAs(t, k, e.seats[0].Signer, row.id, row.msg); err != nil {
			t.Fatalf("%s: %v", row.id, err)
		}
		for approvals := 0; approvals < ApprovedThreshold; approvals++ {
			calls := len(hooks.calls)
			e.refused(t, fmt.Sprintf("%s with %d approvals", row.id, approvals), CodeConflict, func() ([]string, error) {
				return e.executeAs(t, k, e.authority, "ex-"+row.id+"-"+strconv.Itoa(approvals), row.id)
			})
			if len(hooks.calls) != calls || recordOf(t, e.kv(), row.id).state() != authorityv1.ProposalState_PROPOSAL_STATE_OPEN {
				t.Fatalf("%s with %d approvals: hook called or state changed", row.id, approvals)
			}
			if _, err := e.approveAs(t, k, e.seats[approvals].Signer, "ap-"+row.id+"-"+strconv.Itoa(approvals), row.id); err != nil {
				t.Fatal(err)
			}
		}
		if typed, err := e.executeAs(t, k, e.authority, "ex-"+row.id, row.id); err != nil || len(typed) != 2 {
			t.Fatalf("%s with three approvals: %v %v", row.id, err, typed)
		}
	}
}

// TestKeeperInvalidBounds: through Keeper.Propose, every out-of-range
// payload or field is refused with its code and writes nothing: a governed
// value of 0 or above its ceiling, a limit key that is not governed (safety
// caps and quotas included), a key outside the id pattern, a dec- key, a
// validator activation at the current height, a negative power, and an
// expiry outside the unix bounds. The boundary values beside them pass.
func TestKeeperInvalidBounds(t *testing.T) {
	e := readyKeeper(t)
	k := e.keeper
	n := 0
	try := func(name, code string, msg *authorityv1.MsgProposeChange, edit func(*authorityv1.MsgProposeChange)) {
		t.Helper()
		n++
		id := "ib-" + strconv.Itoa(n)
		full := propose(e.seats[n%4].Signer, id, e.policy(t), msg)
		if edit != nil {
			edit(full)
		}
		if code == "" {
			if _, err := k.Propose(e.txCtx(), full); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return
		}
		e.refused(t, name, code, func() ([]string, error) {
			ctx := e.txCtx()
			_, err := k.Propose(ctx, full)
			typed, _ := typedEvents(t, ctx.EventManager().Events())
			return typedNames(typed), err
		})
	}
	for _, key := range msgkit.LimitKeys() {
		ceiling, _ := msgkit.Ceiling(key)
		try(key+" at 0", CodeBounds, governedProposal(key, 0), nil)
		try(key+" at 1", "", governedMsg(key, 1), nil)
		try(key+" at its ceiling", "", governedMsg(key, ceiling), nil)
		try(key+" above its ceiling", CodeBounds, governedProposal(key, ceiling+1), nil)
	}
	for _, key := range nonGovernedKeys {
		try(key, CodeBounds, governedProposal(key, 1), nil)
	}
	try("key outside the id pattern", CodeBounds, governedProposal("Max_page_limit", 1), nil)
	try("dec- key", CodeForbidden, governedProposal("dec-008", 1), nil)
	h := uint64(e.height)
	try("activation at the current height", CodeOutOfRange, powerMsg("v5", pub(5), 1, h), nil)
	try("activation one block ahead", "", powerMsg("v5", pub(5), 1, h+1), nil)
	negative := validatorProposal(&authorityv1.ValidatorPowerChange{
		ConsensusPubkey: pub(6), Power: 1, NodeId: "v6", ReasonDigest: sha256Digest(pub(7)),
		Activation: &authorityv1.ActivationPoint{Point: &authorityv1.ActivationPoint_Height{Height: h + 5}},
	})
	negative.GetPayload().GetValidatorPower().Power = -1
	try("negative power", CodeBounds, negative, nil)
	try("expiry 0", CodeBounds, governedMsg(msgkit.LimitMaxPageLimit, 5), func(m *authorityv1.MsgProposeChange) { m.ExpiryUnixSeconds = 0 })
	try("expiry above the unix bound", CodeBounds, governedMsg(msgkit.LimitMaxPageLimit, 5), func(m *authorityv1.MsgProposeChange) { m.ExpiryUnixSeconds = unixMax + 1 })
	try("expiry at the unix bound", "", governedMsg(msgkit.LimitMaxPageLimit, 5), func(m *authorityv1.MsgProposeChange) { m.ExpiryUnixSeconds = unixMax })
}

// TestKeeperConcurrentProposalsOnOneKey: proposals on one base version
// serialize. The first execute wins; another value of the same key is then
// CONFLICT, the same payload is STALE_VERSION, a proposal of another key is
// STALE_VERSION, each writing nothing. A proposal on the new base executes,
// and the old proposals stop counting against their seats' quota.
func TestKeeperConcurrentProposalsOnOneKey(t *testing.T) {
	e := readyKeeper(t)
	k := e.keeper
	e.queue(t, "cp-a", governedMsg(msgkit.LimitMaxPageLimit, 10))
	e.queue(t, "cp-b", governedMsg(msgkit.LimitMaxPageLimit, 20))
	e.queue(t, "cp-c", governedMsg(msgkit.LimitMaxPageLimit, 10))
	e.queue(t, "cp-d", governedMsg(msgkit.LimitMaxOpenOffers, 5))
	if openAt(t, e.kv(), e.seats[0].ID, 1) != 4 {
		t.Fatalf("open count %d", openAt(t, e.kv(), e.seats[0].ID, 1))
	}
	if _, err := e.executeAs(t, k, e.authority, "ex-cp-a", "cp-a"); err != nil {
		t.Fatal(err)
	}
	if value, found := e.governed(msgkit.LimitMaxPageLimit); !found || value != 10 {
		t.Fatalf("value %d %v", value, found)
	}
	e.refused(t, "another value of the executed key", CodeConflict, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-cp-b", "cp-b")
	})
	e.refused(t, "the executed payload again", CodeStale, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-cp-c", "cp-c")
	})
	e.refused(t, "another key on the old base", CodeStale, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-cp-d", "cp-d")
	})
	if openAt(t, e.kv(), e.seats[0].ID, 2) != 0 {
		t.Fatal("old-base proposals count at the new version")
	}
	e.queue(t, "cp-e", governedMsg(msgkit.LimitMaxPageLimit, 20))
	if _, err := e.executeAs(t, k, e.authority, "ex-cp-e", "cp-e"); err != nil {
		t.Fatal(err)
	}
	if value, _ := e.governed(msgkit.LimitMaxPageLimit); value != 20 || e.policy(t) != 3 {
		t.Fatalf("value %d version %d", value, e.policy(t))
	}
	// cp-b carries the payload cp-e executed, on an older base.
	e.refused(t, "old-base proposal of the payload executed since", CodeStale, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-cp-b-2", "cp-b")
	})
	for _, id := range []string{"cp-b", "cp-c", "cp-d"} {
		if recordOf(t, e.kv(), id).state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			t.Fatalf("%s is not QUEUED", id)
		}
	}
}

// TestKeeperExpiryAndReviewDelayBoundaries: with a labelled DEC-008 test
// delay of 60 seconds, execute at execute_after-1 is OUT_OF_RANGE and at
// execute_after succeeds; approve and execute at expiry-1 succeed and at the
// expiry are kept as EXPIRED; a propose whose expiry is the chain time is
// EXPIRED and writes nothing, one second later is accepted.
func TestKeeperExpiryAndReviewDelayBoundaries(t *testing.T) {
	gen, seats, authority := readyGenesis()
	gen.DevnetTestParameters[0].DelaySeconds = 60
	e := newKeeperEnv(t, gen, seats, authority)
	k := e.keeper
	at := func(unix int64) { e.unix = unix }

	msg := func(id string, expiry int64, value uint64) *authorityv1.MsgProposeChange {
		m := propose(e.seats[0].Signer, id, e.policy(t), governedMsg(msgkit.LimitMaxEvidenceBatch, value))
		m.ExpiryUnixSeconds = expiry
		return m
	}
	approveAll := func(id string) {
		t.Helper()
		for i := 0; i < ApprovedThreshold; i++ {
			if _, err := e.approveAs(t, k, e.seats[i].Signer, "ap-"+id+"-"+strconv.Itoa(i), id); err != nil {
				t.Fatalf("approve %s: %v", id, err)
			}
		}
	}

	// Review delay.
	if _, err := k.Propose(e.txCtx(), msg("rd", testTime+1000, 2)); err != nil {
		t.Fatal(err)
	}
	approveAll("rd")
	if rec := recordOf(t, e.kv(), "rd"); rec.ExecuteAfterUnixSeconds != testTime+60 {
		t.Fatalf("execute_after %d", rec.ExecuteAfterUnixSeconds)
	}
	at(testTime + 59)
	e.refused(t, "execute at execute_after-1", CodeOutOfRange, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-rd-early", "rd")
	})
	at(testTime + 60)
	if _, err := e.executeAs(t, k, e.authority, "ex-rd", "rd"); err != nil {
		t.Fatalf("execute at execute_after: %v", err)
	}

	// Approve expiry.
	at(testTime)
	expiry := testTime + 100
	for _, id := range []string{"ae-ok", "ae-late"} {
		if _, err := k.Propose(e.txCtx(), msg(id, expiry, 3)); err != nil {
			t.Fatal(err)
		}
	}
	at(expiry - 1)
	if _, err := e.approveAs(t, k, e.seats[1].Signer, "ap-ae-ok", "ae-ok"); err != nil {
		t.Fatalf("approve at expiry-1: %v", err)
	}
	at(expiry)
	ctx := e.txCtx()
	if _, err := k.Approve(ctx, approve(e.seats[1].Signer, "ap-ae-late", "ae-late")); err != nil {
		t.Fatal(err)
	}
	if code, _ := resultCode(t, ctx); code != CodeExpired || recordOf(t, e.kv(), "ae-late").state() != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED {
		t.Fatalf("approve at expiry: %q", code)
	}

	// Execute expiry (the delay has passed for both).
	at(testTime)
	expiry = testTime + 200
	for _, id := range []string{"ee-ok", "ee-late"} {
		if _, err := k.Propose(e.txCtx(), msg(id, expiry, 4)); err != nil {
			t.Fatal(err)
		}
		approveAll(id)
	}
	at(expiry - 1)
	if _, err := e.executeAs(t, k, e.authority, "ex-ee-ok", "ee-ok"); err != nil {
		t.Fatalf("execute at expiry-1: %v", err)
	}
	at(expiry)
	ctx = e.txCtx()
	if _, err := k.Execute(ctx, execute(e.authority, "ex-ee-late", "ee-late")); err != nil {
		t.Fatal(err)
	}
	if code, _ := resultCode(t, ctx); code != CodeExpired || recordOf(t, e.kv(), "ee-late").state() != authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED {
		t.Fatalf("execute at expiry: %q", code)
	}

	// Propose expiry.
	e.refused(t, "propose with expiry at the chain time", CodeExpired, func() ([]string, error) {
		ctx := e.txCtx()
		_, err := k.Propose(ctx, msg("pe-now", e.unix, 5))
		return nil, err
	})
	if _, err := k.Propose(e.txCtx(), msg("pe-next", e.unix+1, 5)); err != nil {
		t.Fatalf("propose with expiry one second ahead: %v", err)
	}
}

// TestKeeperUnauthorizedWritesFailAtExecution: every signer that is not a
// current seat is UNAUTHORIZED for propose and approve (outsider, service,
// provider, broadcaster, auditor, AUTHORITY, a retired seat key), and every
// signer without the AUTHORITY role fails execute (no role or a seat:
// UNAUTHORIZED; another role: FORBIDDEN). Each refusal writes nothing to the
// authority store or the registry store, calls no hook, and emits no typed
// event.
func TestKeeperUnauthorizedWritesFailAtExecution(t *testing.T) {
	gen, seats, authority := readyGenesis()
	provider, broadcaster, auditor := bytes20(4), bytes20(5), bytes20(6)
	gen.Authority.Submitters = append(gen.Authority.Submitters,
		&typesv1.Submitter{Address: provider, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_ADMITTED_PROVIDER},
		&typesv1.Submitter{Address: broadcaster, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_BROADCASTER},
		&typesv1.Submitter{Address: auditor, Role: typesv1.SubmitterRole_SUBMITTER_ROLE_AUDITOR},
	)
	gen.DevnetTestParameters = append(gen.DevnetTestParameters, devnetParam("DEC-002", "DEVNET_TEST_ONLY"))
	e := newKeeperEnv(t, gen, seats, authority)
	hooks := &storeHooks{key: e.registryKey, code: msgkit.OK}
	k := e.keeper.WithRegistry(hooks, hooks)
	retired := e.seats[3].Signer
	if out := loadAt(t, e.kv(), e.height, e.unix).RotateCouncilSeat(e.seats[3].ID, pub(70)); !out.Success() {
		t.Fatal(out)
	}
	e.queueWith(t, k, "uw-queued", admitProposal("prov-1"))
	if _, err := e.proposeAs(t, k, e.seats[0].Signer, "uw-open", governedMsg(msgkit.LimitMaxPageLimit, 7)); err != nil {
		t.Fatal(err)
	}
	notSeats := map[string][]byte{
		"outsider": bytes20(9), "service": bytes20(2), "provider": provider, "broadcaster": broadcaster,
		"auditor": auditor, "authority": authority, "retired seat key": retired,
	}
	for name, signer := range notSeats {
		e.refused(t, name+" proposes", CodeUnauthorized, func() ([]string, error) {
			return e.proposeAs(t, k, signer, "uw-p-"+name[:3], governedMsg(msgkit.LimitMaxPageLimit, 8))
		})
		e.refused(t, name+" approves", CodeUnauthorized, func() ([]string, error) {
			return e.approveAs(t, k, signer, "uw-a-"+name[:3], "uw-open")
		})
	}
	executors := []struct {
		name   string
		signer []byte
		code   string
	}{
		{"outsider", bytes20(9), CodeUnauthorized},
		{"seat", e.seats[0].Signer, CodeUnauthorized},
		{"retired seat key", retired, CodeUnauthorized},
		{"service", bytes20(2), CodeForbidden},
		{"provider", provider, CodeForbidden},
		{"broadcaster", broadcaster, CodeForbidden},
		{"auditor", auditor, CodeForbidden},
	}
	for _, r := range executors {
		e.refused(t, r.name+" executes", r.code, func() ([]string, error) {
			return e.executeAs(t, k, r.signer, "uw-x-"+r.name[:3], "uw-queued")
		})
	}
	if len(hooks.calls) != 0 {
		t.Fatalf("hook called %v", hooks.calls)
	}
	if typed, err := e.executeAs(t, k, authority, "uw-x-auth", "uw-queued"); err != nil || len(typed) != 2 || len(hooks.calls) != 1 {
		t.Fatalf("AUTHORITY execute: %v %v %v", err, typed, hooks.calls)
	}
}

// TestNoPayloadApprovesDecision: executing every payload kind (the schema
// version 2 kinds included), and applying the scheduled validator change,
// leaves the blocked decision rows,
// the devnet test parameter rows, the approved council (empty) and the
// quorum-loss behaviour (UNSET) of the config exactly as at genesis.
func TestNoPayloadApprovesDecision(t *testing.T) {
	gen, seats, authority := readyGenesis()
	for i := 1; i <= 8; i++ {
		gen.BlockedDecisions = append(gen.BlockedDecisions, &typesv1.BlockedDecisionParameter{
			DecisionId: fmt.Sprintf("DEC-%03d", i), SchemaVersion: 1,
		})
	}
	gen.DevnetTestParameters = append(gen.DevnetTestParameters,
		devnetParam("DEC-002", "DEVNET_TEST_ONLY"), devnetParam("DEC-003", "DEVNET_TEST_ONLY"))
	e := newKeeperEnv(t, gen, seats, authority)
	hooks := &storeHooks{key: e.registryKey, code: msgkit.OK}
	k := e.keeper.WithRegistry(hooks, hooks)
	check := func(step string) {
		t.Helper()
		cfg, err := readConfig(e.kv())
		if err != nil {
			t.Fatal(err)
		}
		same := func(a, b []proto.Message) bool {
			return slices.EqualFunc(a, b, proto.Equal)
		}
		if !same(messages(cfg.GetBlockedDecisions()), messages(gen.GetBlockedDecisions())) ||
			!same(messages(cfg.GetDevnetTestParameters()), messages(gen.GetDevnetTestParameters())) ||
			len(cfg.GetAuthority().GetApprovedCouncil()) != 0 ||
			cfg.GetAuthority().GetQuorumLoss() != authorityv1.QuorumLossBehavior_QUORUM_LOSS_BEHAVIOR_UNSET {
			t.Fatalf("%s changed a decision row: %v", step, cfg)
		}
		for _, row := range cfg.GetBlockedDecisions() {
			if row.GetApproved() || row.GetValueVersion() != 0 {
				t.Fatalf("%s: %v", step, row)
			}
		}
	}
	check("genesis")
	activation := uint64(e.height) + 3
	for _, row := range []struct {
		id  string
		msg *authorityv1.MsgProposeChange
	}{
		{"nd-power", powerMsg("v5", pub(5), 1, activation)},
		{"nd-limit", governedMsg(msgkit.LimitMaxPageLimit, 50)},
		{"nd-admit", admitProposal("prov-1")},
		{"nd-disable", disableProposal("prov-1")},
		{"nd-withdraw", withdrawProposal("content-1", 1, reasonValue)},
		{"nd-scheduled", scheduledMsg(msgkit.LimitMaxOpenOffers, 7, activation)},
		{"nd-upgrade", upgradeMsg("up-nd", uint64(e.height)+msgkit.MinActivationLeadBlocks, pub(9))},
		{"nd-cancel", cancelMsg("up-nd")},
		{"nd-seat", seatMsg(e.seats[3].ID, pub(90))},
	} {
		e.queueWith(t, k, row.id, row.msg)
		if _, err := e.executeAs(t, k, e.authority, "ex-"+row.id, row.id); err != nil {
			t.Fatalf("%s: %v", row.id, err)
		}
		check(row.id)
	}
	e.height = int64(activation)
	updates, err := k.EndBlock(e.blockCtx())
	if err != nil || len(updates) != 1 {
		t.Fatalf("EndBlock: %v %v", updates, err)
	}
	check("validator change applied")
}

func messages[M proto.Message](rows []M) []proto.Message {
	out := make([]proto.Message, len(rows))
	for i, row := range rows {
		out[i] = row
	}
	return out
}
