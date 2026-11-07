package authority

import (
	"bytes"
	"math"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
)

// Keeper-level tests of the schema version 2 payloads through the keeper
// handlers on real IAVL stores, with the real x/policy keeper behind
// WithPolicy (its version and decision sources are this module's read
// surface, as in the app) and the storeAccounts stand-in behind
// WithAccounts. Each refusal compares the authority, registry (accounts) and
// x/policy stores before and after (keeperEnv.refused).

// policyState is x/policy's Query/Policy state at the env's block.
func (e *keeperEnv) policyState(t *testing.T) *policyv1.PolicyState {
	t.Helper()
	resp, err := e.policyKeeper.Policy(e.blockCtx(), &policyv1.QueryPolicyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	return resp.GetState()
}

// applyDueAt runs x/policy's BeginBlock work at height and commits nothing
// else (the env writes straight to the multistore).
func (e *keeperEnv) applyDueAt(t *testing.T, height int64) {
	t.Helper()
	ctx := e.blockCtx().WithBlockHeight(height)
	if err := e.policyKeeper.ApplyDue(ctx); err != nil {
		t.Fatal(err)
	}
}

// TestActivationHeightEffectiveD4 (D-4): an executed governed limit with an
// activation height a is stored by x/policy as a pending change carrying
// the proposal id and the policy version the execute produced; the value
// is not in force before a (x/policy BeginBlock at a-1 leaves it pending)
// and is in force from a. The execute is OUT_OF_RANGE at height a, with all
// three stores unchanged and the proposal QUEUED.
func TestActivationHeightEffectiveD4(t *testing.T) {
	e := readyKeeper(t)
	k := e.keeper
	key := msgkit.LimitMaxPageLimit
	a := h + 3
	e.queue(t, "d4", scheduledMsg(key, 10, a))
	e.height = int64(a)
	e.refused(t, "execute at the activation height", CodeOutOfRange, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-d4-late", "d4")
	})
	e.height = testHeight
	if _, err := e.executeAs(t, k, e.authority, "ex-d4", "d4"); err != nil {
		t.Fatal(err)
	}
	state := e.policyState(t)
	if len(state.GetPendingLimitChanges()) != 1 {
		t.Fatalf("pending %v", state.GetPendingLimitChanges())
	}
	if p := state.GetPendingLimitChanges()[0]; p.GetLimitKey() != key || p.GetValue() != 10 || p.GetActivationHeight() != a ||
		p.GetProposalId() != "d4" || p.GetPolicyVersion() != 2 || e.policy(t) != 2 {
		t.Fatalf("pending %v, policy version %d", p, e.policy(t))
	}
	if _, found := e.governed(key); found {
		t.Fatal("a scheduled change is in force at execute")
	}
	e.applyDueAt(t, int64(a)-1)
	if _, found := e.governed(key); found {
		t.Fatal("in force at activation-1")
	}
	e.applyDueAt(t, int64(a))
	if value, found := e.governed(key); !found || value != 10 || len(e.policyState(t).GetPendingLimitChanges()) != 0 {
		t.Fatalf("at activation: %d %t, pending %v", value, found, e.policyState(t).GetPendingLimitChanges())
	}
}

// TestImmediateChangeSupersedesPending (DR-10-32, I3): a change scheduled at
// a, then an immediate change of the same key executed before a: the
// immediate value is in force at a-1 and stays in force at a, because the
// immediate change deleted the pending row in the same execute.
func TestImmediateChangeSupersedesPending(t *testing.T) {
	e := readyKeeper(t)
	key := msgkit.LimitMaxOpenOffers
	a := h + 5
	e.queue(t, "sched", scheduledMsg(key, 10, a))
	if _, err := e.executeAs(t, e.keeper, e.authority, "ex-sched", "sched"); err != nil {
		t.Fatal(err)
	}
	e.queue(t, "now", governedMsg(key, 20))
	ctx := e.txCtx()
	if _, err := e.keeper.Execute(ctx, execute(e.authority, "ex-now", "now")); err != nil {
		t.Fatal(err)
	}
	if eventCount(ctx.EventManager().Events(), policy.EventPolicyPending) != 1 {
		t.Fatal("the superseding execute did not emit policy_pending")
	}
	if len(e.policyState(t).GetPendingLimitChanges()) != 0 {
		t.Fatal("the pending row survived the immediate change")
	}
	for _, at := range []int64{int64(a) - 1, int64(a)} {
		e.applyDueAt(t, at)
		if value, found := e.governed(key); !found || value != 20 {
			t.Fatalf("height %d: %d %t", at, value, found)
		}
	}
}

// TestSoftwareUpgradePayload (Q2 (a), Q3 (a)): through the keeper and the
// real x/policy store. The plan height lead is checked at execute
// (h+MinLead executes at h, is OUT_OF_RANGE one block later); a plan of
// another name replaces the pending plan; the same name with the same
// digest is accepted again; the same name with another digest is CONFLICT
// whether its plan is pending, replaced or cancelled; an applied name is
// DUPLICATE_ID. Each refusal leaves all three stores unchanged and the
// proposal QUEUED.
func TestSoftwareUpgradePayload(t *testing.T) {
	e := readyKeeper(t)
	k := e.keeper
	// The versions InitChain writes, so x/policy can record an applied
	// upgrade below.
	vm := module.VersionMap{ModuleName: ConsensusVersion, policy.ModuleName: policy.ConsensusVersion}
	if err := e.policyKeeper.InitVersions(e.blockCtx(), vm, 1); err != nil {
		t.Fatal(err)
	}
	exec := func(id string) {
		t.Helper()
		if _, err := e.executeAs(t, k, e.authority, "ex-"+id, id); err != nil {
			t.Fatalf("%s: %v", id, err)
		}
	}
	plan := func() policy.Plan {
		t.Helper()
		p, found := e.policyKeeper.Plan(e.blockCtx())
		if !found {
			t.Fatal("no plan")
		}
		return p
	}

	e.queue(t, "u1", upgradeMsg("up-1", h+minLead, upgradeDigest))
	e.height = testHeight + 1
	e.refused(t, "lead min-1 at execute", CodeOutOfRange, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-u1-late", "u1")
	})
	e.height = testHeight
	exec("u1")
	if p := plan(); p.Name != "up-1" || p.Height != h+minLead || !bytes.Equal(p.Digest, upgradeDigest) || p.ProposalID != "u1" {
		t.Fatalf("plan %+v", p)
	}
	e.queue(t, "u2", upgradeMsg("up-2", h+maxLead, otherDigest))
	exec("u2")
	if p := plan(); p.Name != "up-2" || p.Height != h+maxLead {
		t.Fatalf("replacement %+v", p)
	}
	e.queue(t, "u1-again", upgradeMsg("up-1", h+minLead+1, upgradeDigest))
	exec("u1-again")
	e.queue(t, "u1-other", upgradeMsg("up-1", h+minLead, otherDigest))
	e.refused(t, "pending name with another digest", CodeConflict, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-u1-other", "u1-other")
	})
	e.queue(t, "c1", cancelMsg("up-1"))
	exec("c1")
	if _, found := e.policyKeeper.Plan(e.blockCtx()); found {
		t.Fatal("plan after the cancel")
	}
	e.queue(t, "u1-cancelled", upgradeMsg("up-1", h+minLead, otherDigest))
	e.refused(t, "cancelled name with another digest", CodeConflict, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-u1-cancelled", "u1-cancelled")
	})
	e.queue(t, "u2-replaced", upgradeMsg("up-2", h+minLead, upgradeDigest))
	e.refused(t, "replaced name with another digest", CodeConflict, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-u2-replaced", "u2-replaced")
	})

	// Apply up-3 at its height through x/policy's upgrade record, then a
	// plan of the same name is DUPLICATE_ID, with either digest.
	e.queue(t, "u3", upgradeMsg("up-3", h+minLead, upgradeDigest))
	exec("u3")
	if err := e.policyKeeper.RecordUpgrade(e.blockCtx().WithBlockHeight(int64(h+minLead)), "up-3", h+minLead, vm, 2); err != nil {
		t.Fatal(err)
	}
	for i, digest := range [][]byte{upgradeDigest, otherDigest} {
		id := "u3-again-" + string(rune('a'+i))
		e.queue(t, id, upgradeMsg("up-3", h+minLead, digest))
		e.refused(t, "applied name", CodeDuplicate, func() ([]string, error) {
			return e.executeAs(t, k, e.authority, "ex-"+id, id)
		})
	}
	for _, id := range []string{"u1-other", "u1-cancelled", "u2-replaced", "u3-again-a", "u3-again-b"} {
		if got := recordOf(t, e.kv(), id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			t.Fatalf("%s is %v", id, got)
		}
	}
}

// TestCancelUpgradePayload: a cancel with no pending plan, or of a plan of
// another name, is NOT_FOUND with all stores unchanged and no upgrade_plan
// event; a cancel of the pending plan deletes it and x/policy emits
// upgrade_plan (cancelled) before the typed execute events.
func TestCancelUpgradePayload(t *testing.T) {
	e := readyKeeper(t)
	k := e.keeper
	e.queue(t, "c0", cancelMsg("up-1"))
	e.refused(t, "no pending plan", CodeNotFound, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-c0", "c0")
	})
	e.queue(t, "u1", upgradeMsg("up-1", h+minLead, upgradeDigest))
	if _, err := e.executeAs(t, k, e.authority, "ex-u1", "u1"); err != nil {
		t.Fatal(err)
	}
	e.queue(t, "c2", cancelMsg("up-2"))
	e.refused(t, "plan of another name", CodeNotFound, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-c2", "c2")
	})
	e.queue(t, "c1", cancelMsg("up-1"))
	ctx := e.txCtx()
	if _, err := k.Execute(ctx, execute(e.authority, "ex-c1", "c1")); err != nil {
		t.Fatal(err)
	}
	events := ctx.EventManager().Events()
	_, at := typedEvents(t, events)
	planAt := -1
	for i, ev := range events {
		if ev.Type == policy.EventUpgradePlan && attr(ev, policy.AttrAction) == policy.PlanActionCancelled {
			planAt = i
		}
	}
	if planAt < 0 || len(at) != 2 || planAt > at[0] {
		t.Fatalf("upgrade_plan at %d, typed events at %v", planAt, at)
	}
	if _, found := e.policyKeeper.Plan(e.blockCtx()); found {
		t.Fatal("plan after the cancel")
	}
}

// attr is the value of one attribute of ev, or "".
func attr(ev sdk.Event, key string) string {
	for _, a := range ev.Attributes {
		if a.Key == key {
			return a.Value
		}
	}
	return ""
}

// TestCouncilSeatPayload (Q1 (a), DR-10-35): through the keeper. A refusal
// leaves the authority store, the accounts and the policy store unchanged;
// a success replaces the key, retires the old key, creates the new key's
// account in the same nested cache, and moves ante admission to the new
// key. An account created by a seat execute that then fails a later engine
// check (OVERFLOW at policy version 2^64-1) is dropped with the nested
// cache.
func TestCouncilSeatPayload(t *testing.T) {
	e := readyKeeper(t)
	k := e.keeper
	e.queue(t, "absent", seatMsg("seat-9", pub(80)))
	e.refused(t, "absent seat", CodeNotFound, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-absent", "absent")
	})
	e.queue(t, "dup", seatMsg(e.seats[0].ID, e.seats[1].Pub))
	e.refused(t, "another seat's key", CodeDuplicate, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-dup", "dup")
	})
	e.queue(t, "valkey", seatMsg(e.seats[0].ID, pub(1)))
	e.refused(t, "a validator key (D-7)", CodeDuplicate, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-valkey", "valkey")
	})
	if e.registryKV().Has(accountKey(SeatAddress(pub(80)))) {
		t.Fatal("a refused seat change created an account")
	}

	e.queue(t, "replace", seatMsg(e.seats[3].ID, pub(80)))
	typed, err := e.executeAs(t, k, e.authority, "ex-replace", "replace")
	if err != nil || len(typed) != 2 {
		t.Fatalf("replace: %v %v", err, typed)
	}
	if !e.registryKV().Has(accountKey(SeatAddress(pub(80)))) {
		t.Fatal("the new key's account was not created")
	}
	if k.Admission(e.blockCtx(), e.seats[3].Signer) != NotAdmitted || k.Admission(e.blockCtx(), SeatAddress(pub(80))) != AdmittedSeat {
		t.Fatal("admission did not move to the new key")
	}
	if got := e.kv().Get(retiredCouncilKey(e.seats[3].Pub)); string(got) != e.seats[3].ID {
		t.Fatalf("retired row %q", got)
	}

	// A late OVERFLOW drops the account the hook created.
	o := readyKeeper(t)
	o.kv().Set([]byte{policyKey}, encodeU64(math.MaxUint64))
	o.queue(t, "late", seatMsg(o.seats[0].ID, pub(81)))
	o.refused(t, "seat change at policy version 2^64-1", CodeOverflow, func() ([]string, error) {
		return o.executeAs(t, o.keeper, o.authority, "ex-late", "late")
	})
	if o.registryKV().Has(accountKey(SeatAddress(pub(81)))) {
		t.Fatal("the account of a failed seat change survived")
	}
}

// TestReplacedSeatApprovalsBecomeStale (plan 5.4): proposals approved by a
// seat's old key before its replacement are on an older base once the
// replacement executes, so each is STALE_VERSION (or CONFLICT for the key
// the replacement itself executed) and writes nothing; the replaced seat can
// approve again only with its new key.
func TestReplacedSeatApprovalsBecomeStale(t *testing.T) {
	e := readyKeeper(t)
	k := e.keeper
	e.queue(t, "a", governedMsg(msgkit.LimitMaxPageLimit, 10))
	e.queue(t, "b", upgradeMsg("up-1", h+minLead, upgradeDigest))
	e.queue(t, "seat", seatMsg(e.seats[0].ID, pub(80)))
	e.queue(t, "seat-again", seatMsg(e.seats[0].ID, pub(81)))
	if _, err := e.executeAs(t, k, e.authority, "ex-seat", "seat"); err != nil {
		t.Fatal(err)
	}
	e.refused(t, "governed limit approved by the old key", CodeStale, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-a", "a")
	})
	e.refused(t, "upgrade approved by the old key", CodeStale, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-b", "b")
	})
	e.refused(t, "another change of the replaced seat", CodeConflict, func() ([]string, error) {
		return e.executeAs(t, k, e.authority, "ex-seat-again", "seat-again")
	})
	e.refused(t, "old key proposes", CodeUnauthorized, func() ([]string, error) {
		return e.proposeAs(t, k, e.seats[0].Signer, "old", governedMsg(msgkit.LimitMaxPageLimit, 11))
	})
	if _, err := e.proposeAs(t, k, SeatAddress(pub(80)), "new", governedMsg(msgkit.LimitMaxPageLimit, 11)); err != nil {
		t.Fatalf("new key proposes: %v", err)
	}
}

// TestPolicyWriterFailureWritesNothing: an x/policy refusal (DUPLICATE_ID,
// CONFLICT, NOT_FOUND), and a later engine failure after a successful
// writer call (OVERFLOW at policy version 2^64-1 after SetLimit and after
// ScheduleUpgrade), leave the authority store and the policy store
// byte-identical and emit no policy or typed event.
func TestPolicyWriterFailureWritesNothing(t *testing.T) {
	e := readyKeeper(t)
	e.kv().Set([]byte{policyKey}, encodeU64(math.MaxUint64))
	for _, row := range []struct {
		id  string
		msg *authorityv1.MsgProposeChange
	}{
		{"limit", governedMsg(msgkit.LimitMaxPageLimit, 10)},
		{"upgrade", upgradeMsg("up-1", h+minLead, upgradeDigest)},
	} {
		e.queue(t, row.id, row.msg)
		ctx := e.txCtx()
		before, beforePolicy := dumpKV(e.kv()), dumpKV(e.policyKV())
		_, err := e.keeper.Execute(ctx, execute(e.authority, "ex-"+row.id, row.id))
		assertCode(t, err, CodeOverflow)
		wantUnchanged(t, e.kv(), before, row.id)
		wantUnchanged(t, e.policyKV(), beforePolicy, row.id+" (policy store)")
		events := ctx.EventManager().Events()
		if eventCount(events, policy.EventPolicyPending)+eventCount(events, policy.EventUpgradePlan) != 0 {
			t.Fatalf("%s: a dropped policy write's event survived", row.id)
		}
		if typed, _ := typedEvents(t, events); len(typed) != 0 {
			t.Fatalf("%s: typed events %v", row.id, typed)
		}
	}
	if _, found := e.governed(msgkit.LimitMaxPageLimit); found {
		t.Fatal("a dropped SetLimit is in force")
	}
	if _, found := e.policyKeeper.Plan(e.blockCtx()); found {
		t.Fatal("a dropped plan is stored")
	}
}

// TestGovernanceQueryReadsPolicy: Query/Governance takes its page cap from
// the governed max_page_limit x/policy holds (a limit above it is BOUNDS),
// and a keeper without a policy keeper refuses the query instead of using
// the protocol ceiling.
func TestGovernanceQueryReadsPolicy(t *testing.T) {
	e := readyKeeper(t)
	if _, err := governanceQuery(t, NewKeeper(e.key), e.blockCtx(), 1, nil); err == nil {
		t.Fatal("a keeper without a policy keeper answered the query")
	}
	if _, err := governanceQuery(t, e.keeper, e.blockCtx(), 100, nil); err != nil {
		t.Fatalf("ceiling: %v", err)
	}
	e.queue(t, "cap", governedMsg(msgkit.LimitMaxPageLimit, 2))
	if _, err := e.executeAs(t, e.keeper, e.authority, "ex-cap", "cap"); err != nil {
		t.Fatal(err)
	}
	if _, err := governanceQuery(t, e.keeper, e.blockCtx(), 3, nil); err == nil || !bytes.Contains([]byte(err.Error()), []byte("BOUNDS")) {
		t.Fatalf("limit above the governed cap: %v", err)
	}
	page, err := governanceQuery(t, e.keeper, e.blockCtx(), 0, nil)
	if err != nil || len(page.GetState().GetValidators()) != 2 {
		t.Fatalf("limit 0 uses the governed cap 2: %v %v", page, err)
	}
}

// TestGovernanceDevnetTestParameters (D-9, schema version 2): every page of
// Query/Governance carries the devnet test rows that take effect on the
// chain, in stored order: on a shortvideo-dev- chain, the rows with label
// PROPOSED_TEST_PARAMETER and approved_policy false (not the approved row,
// not the row with another label); on any other chain id, none.
func TestGovernanceDevnetTestParameters(t *testing.T) {
	e, _ := registryKeeper(t)
	cfg, err := readConfig(e.kv())
	if err != nil {
		t.Fatal(err)
	}
	var want []string
	for _, row := range cfg.GetDevnetTestParameters() {
		if row.GetLabel() == "PROPOSED_TEST_PARAMETER" && !row.GetApprovedPolicy() {
			want = append(want, row.GetDecisionId())
		}
	}
	if len(want) != 3 || len(cfg.GetDevnetTestParameters()) != 5 {
		t.Fatalf("fixture rows %v of %d", want, len(cfg.GetDevnetTestParameters()))
	}
	var key []byte
	for pages := 0; ; pages++ {
		page, err := governanceQuery(t, e.keeper, e.blockCtx(), 3, key)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, row := range page.GetDevnetTestParameters() {
			got = append(got, row.GetDecisionId())
			if row.GetLabel() != "PROPOSED_TEST_PARAMETER" || row.GetApprovedPolicy() {
				t.Fatalf("page %d row %v", pages, row)
			}
		}
		if len(got) != len(want) {
			t.Fatalf("page %d rows %v, want %v", pages, got, want)
		}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("page %d rows %v, want %v", pages, got, want)
			}
		}
		key = page.GetPage().GetNextPageKey()
		if len(key) == 0 {
			if pages == 0 {
				t.Fatal("one page: the test does not cover a second page")
			}
			break
		}
	}
	page, err := governanceQuery(t, e.keeper, e.blockCtx().WithChainID("shortvideo-beta-1"), 3, nil)
	if err != nil || len(page.GetDevnetTestParameters()) != 0 {
		t.Fatalf("off devnet: %v %v", page.GetDevnetTestParameters(), err)
	}
}

// TestKeeperLeadBoundaries (Q3 (a); MinActivationLeadBlocks and
// MaxActivationLeadBlocks are PROPOSED): every boundary of both height
// windows through the keeper handlers with the real x/policy store. At
// propose in a block at height h: a governed activation at h is
// OUT_OF_RANGE, at h+1 and h+max accepted, at h+max+1 BOUNDS; a plan height
// at h+min-1 is OUT_OF_RANGE, at h+min and h+max accepted, at h+max+1
// BOUNDS. At execute: an activation a executes at a-1 and is OUT_OF_RANGE at
// a; a plan height H executes at H-min and is OUT_OF_RANGE at H-min+1; both
// windows' upper edge (h+max) executes in the proposing block, and h+max+1
// at execute (a record written directly; no later block widens the window)
// is BOUNDS. Every refusal leaves the three stores unchanged, and a refused
// execute leaves the proposal QUEUED.
func TestKeeperLeadBoundaries(t *testing.T) {
	e := readyKeeper(t)
	k := e.keeper
	key := msgkit.LimitMaxOpenOffers
	for i, r := range []struct {
		name string
		msg  *authorityv1.MsgProposeChange
		want string
	}{
		{"activation h", scheduledMsg(key, 8, h), CodeOutOfRange},
		{"activation h+1", scheduledMsg(key, 8, h+1), ""},
		{"activation h+max", scheduledMsg(key, 8, h+maxLead), ""},
		{"activation h+max+1", scheduledMsg(key, 8, h+maxLead+1), CodeBounds},
		{"plan height h+min-1", upgradeMsg("up-a", h+minLead-1, upgradeDigest), CodeOutOfRange},
		{"plan height h+min", upgradeMsg("up-b", h+minLead, upgradeDigest), ""},
		{"plan height h+max", upgradeMsg("up-c", h+maxLead, upgradeDigest), ""},
		{"plan height h+max+1", upgradeMsg("up-d", h+maxLead+1, upgradeDigest), CodeBounds},
	} {
		id := "lb-" + string(rune('a'+i))
		if r.want != "" {
			e.refused(t, "propose "+r.name, r.want, func() ([]string, error) {
				return e.proposeAs(t, k, e.seats[0].Signer, id, r.msg)
			})
			continue
		}
		if _, err := e.proposeAs(t, k, e.seats[0].Signer, id, r.msg); err != nil {
			t.Fatalf("propose %s: %v", r.name, err)
		}
	}

	// Execute windows. Activation a = h+10: OUT_OF_RANGE at a, executes at
	// a-1. Plan height H = h+min+10: OUT_OF_RANGE at H-min+1, executes at
	// H-min. The upper edges execute in the proposing block.
	exec := func(id string, height int64) {
		t.Helper()
		e.height = height
		defer func() { e.height = testHeight }()
		if _, err := e.executeAs(t, k, e.authority, "ex-"+id, id); err != nil {
			t.Fatalf("execute %s at %d: %v", id, height, err)
		}
	}
	refusedAt := func(name, id string, height int64) {
		t.Helper()
		e.height = height
		defer func() { e.height = testHeight }()
		e.refused(t, name, CodeOutOfRange, func() ([]string, error) {
			return e.executeAs(t, k, e.authority, "ex-"+id+"-late", id)
		})
	}
	e.queue(t, "ga", scheduledMsg(key, 8, h+10))
	refusedAt("execute at the activation height", "ga", testHeight+10)
	exec("ga", testHeight+9)
	e.queue(t, "ua", upgradeMsg("up-1", h+minLead+10, upgradeDigest))
	refusedAt("execute at plan height - min + 1", "ua", testHeight+11)
	exec("ua", testHeight+10)
	e.queue(t, "gmax", scheduledMsg(msgkit.LimitMaxPageLimit, 8, h+maxLead))
	exec("gmax", testHeight)
	e.queue(t, "umax", upgradeMsg("up-2", h+maxLead, upgradeDigest))
	exec("umax", testHeight)
	version := e.policy(t)
	putQueuedPayload(t, e.kv(), e.seats, "gover", version, scheduledMsg(key, 8, h+maxLead+1))
	putQueuedPayload(t, e.kv(), e.seats, "uover", version, upgradeMsg("up-3", h+maxLead+1, upgradeDigest))
	for _, id := range []string{"gover", "uover"} {
		e.refused(t, id+": h+max+1 at execute", CodeBounds, func() ([]string, error) {
			return e.executeAs(t, k, e.authority, "ex-"+id, id)
		})
		if recordOf(t, e.kv(), id).state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
			t.Fatalf("%s is not QUEUED", id)
		}
	}
}

// touchHooks is a PolicyKeeper and an AccountKeeper that, against the hook
// contract, writes a row into the hook test store and emits an event before
// it returns code. With code FORBIDDEN it drives an execute into the keep
// path (REJECTED) after a hook write.
type touchHooks struct {
	key  *storetypes.KVStoreKey
	code msgkit.Code
}

func (h touchHooks) touch(ctx sdk.Context, what string) msgkit.Code {
	ctx.KVStore(h.key).Set([]byte(what), []byte{1})
	ctx.EventManager().EmitEvent(sdk.NewEvent("fake_hook", sdk.NewAttribute("what", what)))
	return h.code
}

func (h touchHooks) SetLimit(ctx sdk.Context, key string, _ uint64, _ string) msgkit.Code {
	return h.touch(ctx, "limit/"+key)
}

func (h touchHooks) SchedulePending(ctx sdk.Context, key string, _, _ uint64, _ string, _ uint64) msgkit.Code {
	return h.touch(ctx, "pending/"+key)
}

func (h touchHooks) ScheduleUpgrade(ctx sdk.Context, name string, _ uint64, _ []byte, _ string) msgkit.Code {
	return h.touch(ctx, "plan/"+name)
}

func (h touchHooks) CancelUpgrade(ctx sdk.Context, name string) msgkit.Code {
	return h.touch(ctx, "cancel/"+name)
}

func (touchHooks) UpgradeApplied(sdk.Context, string) bool { return false }

func (touchHooks) GovernedLimit(sdk.Context, string) (uint64, bool) { return 0, false }

func (h touchHooks) EnsureAccount(ctx sdk.Context, addr []byte) msgkit.Code {
	return h.touch(ctx, "account/"+string(addr))
}

// TestHookWriteDroppedOnRejectedKeep: a policy or account hook that writes
// and then returns FORBIDDEN (which the x/policy writers and the app's
// account keeper never do) makes the execute a REJECTED keep: the message
// branch commits the REJECTED mark, and the hook's write and event are
// dropped with the nested hook cache, as for a registry hook.
func TestHookWriteDroppedOnRejectedKeep(t *testing.T) {
	e := readyKeeper(t)
	hooks := touchHooks{key: e.registryKey, code: msgkit.Forbidden}
	k := e.keeper.WithPolicy(hooks).WithAccounts(hooks)
	for _, row := range []struct {
		id  string
		msg *authorityv1.MsgProposeChange
	}{
		{"k-limit", governedMsg(msgkit.LimitMaxPageLimit, 10)},
		{"k-sched", scheduledMsg(msgkit.LimitMaxPageLimit, 10, h+5)},
		{"k-upgrade", upgradeMsg("up-1", h+minLead, upgradeDigest)},
		{"k-cancel", cancelMsg("up-1")},
		{"k-seat", seatMsg(e.seats[3].ID, pub(80))},
	} {
		e.queueWith(t, k, row.id, row.msg)
		beforeRegistry, beforePolicy := dumpKV(e.registryKV()), dumpKV(e.policyKV())
		ctx := e.txCtx()
		if _, err := k.Execute(ctx, execute(e.authority, "ex-"+row.id, row.id)); err != nil {
			t.Fatalf("%s: %v", row.id, err)
		}
		if code, _ := resultCode(t, ctx); code != CodeForbidden {
			t.Fatalf("%s: authority_result %q", row.id, code)
		}
		if got := recordOf(t, e.kv(), row.id).state(); got != authorityv1.ProposalState_PROPOSAL_STATE_REJECTED {
			t.Fatalf("%s is %v", row.id, got)
		}
		wantUnchanged(t, e.registryKV(), beforeRegistry, row.id+" (hook store)")
		wantUnchanged(t, e.policyKV(), beforePolicy, row.id+" (policy store)")
		if eventCount(ctx.EventManager().Events(), "fake_hook") != 0 {
			t.Fatalf("%s: a dropped hook's event survived", row.id)
		}
		if typed, _ := typedEvents(t, ctx.EventManager().Events()); len(typed) != 0 {
			t.Fatalf("%s: typed events on a keep %v", row.id, typed)
		}
	}
}
