package policy_test

import (
	"bytes"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
)

const h = 7 // msgkittest.BlockHeight

func pendingOf(t testing.TB, e *env, key string) (policy.Pending, bool) {
	t.Helper()
	raw := e.store().Get(policy.PendingKey(key))
	if raw == nil {
		return policy.Pending{}, false
	}
	p, err := policy.DecodePending(raw)
	if err != nil {
		t.Fatal(err)
	}
	return p, true
}

// TestGovernedLimitReadSurface: absent is not found (callers fall back to
// the ceiling through msgkit.Effective); a written value is found; a
// damaged value panics instead of reporting no limit, with the same
// contract as the authority read it replaces.
func TestGovernedLimitReadSurface(t *testing.T) {
	e := newEnv(t)
	for _, key := range msgkit.LimitKeys() {
		if v, found := e.k.GovernedLimit(e.ctx, key); found || v != 0 {
			t.Fatalf("%s: (%d, %t) before any write", key, v, found)
		}
	}
	if code := e.k.SetLimit(e.fresh(), msgkit.LimitMaxPageLimit, 40, "p1"); code != msgkit.OK {
		t.Fatal(code)
	}
	v, found := e.k.GovernedLimit(e.ctx, msgkit.LimitMaxPageLimit)
	if !found || v != 40 || msgkit.Effective(msgkit.LimitMaxPageLimit, v, found) != 40 {
		t.Fatalf("governed max_page_limit (%d, %t)", v, found)
	}
	ceiling, _ := msgkit.Ceiling(msgkit.LimitMaxPageLimit)
	damage := map[string][]byte{
		"3 bytes":     {1, 2, 3},
		"value 0":     policy.EncodeU64(0),
		"ceiling + 1": policy.EncodeU64(ceiling + 1),
		"9 bytes":     make([]byte, 9),
		"1 byte":      {0},
	}
	for name, raw := range damage {
		t.Run(name, func(t *testing.T) {
			d := newEnv(t)
			d.store().Set(policy.GovernedKey(msgkit.LimitMaxPageLimit), raw)
			mustPanic(t, `policy: governed limit "max_page_limit" is not an 8-byte value in 1..ceiling`, func() {
				d.k.GovernedLimit(d.ctx, msgkit.LimitMaxPageLimit)
			})
		})
	}
	t.Run("unknown key row", func(t *testing.T) {
		d := newEnv(t)
		d.store().Set(policy.GovernedKey("max_tx_bytes"), policy.EncodeU64(1))
		mustPanic(t, `policy: governed limit "max_tx_bytes"`, func() { d.k.GovernedLimit(d.ctx, "max_tx_bytes") })
	})
}

// TestSetLimitAndSchedulePending: the key and range checks, the activation
// window (OUT_OF_RANGE at the block height, accepted one above it; BOUNDS
// one above the lead and above 2^53-1, accepted at the lead), replace
// semantics with one row per key, the stored policy version, and the
// version order check. Every reject writes nothing and emits nothing.
func TestSetLimitAndSchedulePending(t *testing.T) {
	e := newEnv(t)
	e.auth.version = 9
	key := msgkit.LimitMaxOpenOffers
	ceiling, _ := msgkit.Ceiling(key)
	set := func(k string, v uint64, id string) func(sdk.Context) msgkit.Code {
		return func(ctx sdk.Context) msgkit.Code { return e.k.SetLimit(ctx, k, v, id) }
	}
	schedule := func(k string, v, a uint64, id string, pv uint64) func(sdk.Context) msgkit.Code {
		return func(ctx sdk.Context) msgkit.Code { return e.k.SchedulePending(ctx, k, v, a, id, pv) }
	}
	e.rejects("set unknown key", msgkit.Bounds, set("max_unknown", 1, "p1"))
	e.rejects("set safety cap max_tx_bytes", msgkit.Bounds, set("max_tx_bytes", 1, "p1"))
	e.rejects("set safety cap max_gas_per_tx", msgkit.Bounds, set("max_gas_per_tx", 1, "p1"))
	e.rejects("set value 0", msgkit.Bounds, set(key, 0, "p1"))
	e.rejects("set above ceiling", msgkit.Bounds, set(key, ceiling+1, "p1"))
	e.rejects("set bad proposal id", msgkit.Bounds, set(key, 1, "P1"))
	e.rejects("set empty proposal id", msgkit.Bounds, set(key, 1, ""))
	e.rejects("schedule unknown key", msgkit.Bounds, schedule("max_unknown", 1, h+1, "p1", 2))
	e.rejects("schedule value 0", msgkit.Bounds, schedule(key, 0, h+1, "p1", 2))
	e.rejects("schedule above ceiling", msgkit.Bounds, schedule(key, ceiling+1, h+1, "p1", 2))
	e.rejects("schedule bad proposal id", msgkit.Bounds, schedule(key, 1, h+1, "p 1", 2))
	e.rejects("activation at height (effective-1)", msgkit.OutOfRange, schedule(key, 1, h, "p1", 2))
	e.rejects("activation 0", msgkit.OutOfRange, schedule(key, 1, 0, "p1", 2))
	e.rejects("activation above the lead", msgkit.Bounds, schedule(key, 1, h+policy.MaxActivationLeadBlocks+1, "p1", 2))
	e.rejects("policy version 1", msgkit.OutOfRange, schedule(key, 1, h+1, "p1", 1))
	e.rejects("policy version 0", msgkit.OutOfRange, schedule(key, 1, h+1, "p1", 0))
	high := e.ctx.WithBlockHeight(genesis.SafeIntegerMax).WithEventManager(sdk.NewEventManager())
	before := e.snapshot()
	if code := e.k.SchedulePending(high, key, 1, uint64(genesis.SafeIntegerMax)+1, "p1", 2); code != msgkit.Bounds {
		t.Fatalf("activation above 2^53-1: %q", code)
	}
	if !bytes.Equal(before, e.snapshot()) || len(high.EventManager().Events()) != 0 {
		t.Fatal("activation above 2^53-1 wrote or emitted")
	}

	// Floor and ceiling of every key are accepted.
	for i, k := range msgkit.LimitKeys() {
		c, _ := msgkit.Ceiling(k)
		if code := e.k.SetLimit(e.fresh(), k, 1, "floor"); code != msgkit.OK {
			t.Fatalf("%s at 1: %q", k, code)
		}
		if code := e.k.SchedulePending(e.fresh(), k, c, h+1+uint64(i), "ceil", 2); code != msgkit.OK {
			t.Fatalf("%s pending at ceiling: %q", k, code)
		}
	}
	e.mustCheck()

	// Activation one above the height (effective) and exactly at the lead.
	e = newEnv(t)
	e.auth.version = 9
	if code := e.k.SchedulePending(e.fresh(), key, 8, h+1, "p1", 2); code != msgkit.OK {
		t.Fatal(code)
	}
	if code := e.k.SchedulePending(e.fresh(), msgkit.LimitMaxPageLimit, 8, h+policy.MaxActivationLeadBlocks, "p2", 3); code != msgkit.OK {
		t.Fatalf("activation at the lead: %q", code)
	}
	p, found := pendingOf(t, e, key)
	if !found || p != (policy.Pending{Activation: h + 1, Value: 8, PolicyVersion: 2, ProposalID: "p1"}) {
		t.Fatalf("pending %+v %t", p, found)
	}
	if _, set := e.k.GovernedLimit(e.ctx, key); set {
		t.Fatal("a scheduled change wrote the governed value")
	}

	// A later scheduled change replaces the row of its key.
	ctx := e.fresh()
	if code := e.k.SchedulePending(ctx, key, 5, h+30, "p3", 4); code != msgkit.OK {
		t.Fatal(code)
	}
	p, _ = pendingOf(t, e, key)
	if p != (policy.Pending{Activation: h + 30, Value: 5, PolicyVersion: 4, ProposalID: "p3"}) {
		t.Fatalf("replaced pending %+v", p)
	}
	got := attrs(t, ctx.EventManager().Events(), policy.EventPolicyPending)
	want := map[string]string{
		"parameter_key": key, "value": "5", "activation_height": "37", "proposal_id": "p3", "superseded_proposal_id": "p1",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("event %v, want %v", got, want)
		}
	}
	rows := 0
	it := e.store().Iterator([]byte{policy.PendingPrefix}, []byte{policy.PendingPrefix + 1})
	for ; it.Valid(); it.Next() {
		rows++
	}
	if err := it.Close(); err != nil {
		t.Fatal(err)
	}
	if rows != 2 {
		t.Fatalf("%d pending rows, want one per scheduled key (2)", rows)
	}

	// A first scheduled change names no superseded proposal.
	ctx = e.fresh()
	if code := e.k.SchedulePending(ctx, msgkit.LimitMaxEvidenceBatch, 4, h+2, "p4", 5); code != msgkit.OK {
		t.Fatal(code)
	}
	if got := attrs(t, ctx.EventManager().Events(), policy.EventPolicyPending); got["superseded_proposal_id"] != "" {
		t.Fatalf("first pending event %v", got)
	}

	// A replacement must carry a higher policy version.
	e.rejects("equal policy version", msgkit.Conflict, schedule(key, 6, h+40, "p5", 4))
	e.rejects("lower policy version", msgkit.Conflict, schedule(key, 6, h+40, "p5", 3))
	e.mustCheck()
}

// TestSetLimitSupersedesPending: an immediate change writes the value and
// deletes the key's pending row in the same write, so the latest executed
// decision wins (DR-10-32); policy_pending names the superseded proposal.
// Without a pending row SetLimit emits nothing and leaves other keys'
// pending rows alone.
func TestSetLimitSupersedesPending(t *testing.T) {
	e := newEnv(t)
	e.auth.version = 5
	key := msgkit.LimitMaxExpirySteps
	if code := e.k.SchedulePending(e.fresh(), key, 3, h+10, "older", 2); code != msgkit.OK {
		t.Fatal(code)
	}
	if code := e.k.SchedulePending(e.fresh(), msgkit.LimitMaxPageLimit, 9, h+10, "other", 3); code != msgkit.OK {
		t.Fatal(code)
	}
	ctx := e.fresh()
	if code := e.k.SetLimit(ctx, key, 50, "newer"); code != msgkit.OK {
		t.Fatal(code)
	}
	if v, found := e.k.GovernedLimit(e.ctx, key); !found || v != 50 {
		t.Fatalf("governed (%d, %t)", v, found)
	}
	if _, found := pendingOf(t, e, key); found {
		t.Fatal("the superseded pending row survived")
	}
	if _, found := pendingOf(t, e, msgkit.LimitMaxPageLimit); !found {
		t.Fatal("another key's pending row was deleted")
	}
	got := attrs(t, ctx.EventManager().Events(), policy.EventPolicyPending)
	want := map[string]string{
		"parameter_key": key, "value": "50", "activation_height": "0", "proposal_id": "newer", "superseded_proposal_id": "older",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("event %v, want %v", got, want)
		}
	}
	ctx = e.fresh()
	if code := e.k.SetLimit(ctx, key, 51, "again"); code != msgkit.OK {
		t.Fatal(code)
	}
	if n := len(ctx.EventManager().Events()); n != 0 {
		t.Fatalf("SetLimit without a pending row emitted %d events", n)
	}
	e.mustCheck()
}

// TestDamagedPendingRowPanics: a writer that reads a pending row which does
// not decode panics before it writes anything.
func TestDamagedPendingRowPanics(t *testing.T) {
	for name, op := range map[string]func(e *env) msgkit.Code{
		"SetLimit": func(e *env) msgkit.Code { return e.k.SetLimit(e.fresh(), msgkit.LimitMaxOpenOffers, 2, "p1") },
		"SchedulePending": func(e *env) msgkit.Code {
			return e.k.SchedulePending(e.fresh(), msgkit.LimitMaxOpenOffers, 2, h+1, "p1", 9)
		},
	} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.store().Set(policy.PendingKey(msgkit.LimitMaxOpenOffers), []byte{1})
			before := e.snapshot()
			mustPanic(t, `policy: pending change of "max_open_offers"`, func() { op(e) })
			if !bytes.Equal(before, e.snapshot()) {
				t.Fatal("the panicking writer wrote")
			}
		})
	}
}

// TestPlanScheduleReplaceCancel: the plan writer's checks, replacement by a
// plan of another name or of the same name and digest, CONFLICT for the
// same name with another digest, DUPLICATE_ID for an applied name, cancel,
// and the upgrade_plan events.
func TestPlanScheduleReplaceCancel(t *testing.T) {
	e := newEnv(t)
	d1, d2 := digestOf("release-1"), digestOf("release-2")
	schedule := func(name string, height uint64, digest []byte, id string) func(sdk.Context) msgkit.Code {
		return func(ctx sdk.Context) msgkit.Code { return e.k.ScheduleUpgrade(ctx, name, height, digest, id) }
	}
	e.rejects("bad name", msgkit.Bounds, schedule("Up", h+5, d1, "p1"))
	e.rejects("empty name", msgkit.Bounds, schedule("", h+5, d1, "p1"))
	e.rejects("bad proposal id", msgkit.Bounds, schedule("up-1", h+5, d1, "-p"))
	e.rejects("short digest", msgkit.Bounds, schedule("up-1", h+5, d1[:31], "p1"))
	e.rejects("nil digest", msgkit.Bounds, schedule("up-1", h+5, nil, "p1"))
	e.rejects("height at block height", msgkit.OutOfRange, schedule("up-1", h, d1, "p1"))
	e.rejects("height above the lead", msgkit.Bounds, schedule("up-1", h+policy.MaxActivationLeadBlocks+1, d1, "p1"))
	e.rejects("cancel without a plan", msgkit.NotFound, func(ctx sdk.Context) msgkit.Code { return e.k.CancelUpgrade(ctx, "up-1") })
	e.rejects("cancel bad name", msgkit.Bounds, func(ctx sdk.Context) msgkit.Code { return e.k.CancelUpgrade(ctx, "UP") })

	ctx := e.fresh()
	if code := e.k.ScheduleUpgrade(ctx, "up-1", h+1, d1, "p1"); code != msgkit.OK {
		t.Fatalf("height one above the block: %q", code)
	}
	plan, found := e.k.Plan(e.ctx)
	if !found || !equalPlan(plan, policy.Plan{Name: "up-1", Height: h + 1, Digest: d1, ProposalID: "p1"}) {
		t.Fatalf("plan %+v %t", plan, found)
	}
	ev := attrs(t, ctx.EventManager().Events(), policy.EventUpgradePlan)
	if ev["name"] != "up-1" || ev["height"] != "8" || ev["release_digest"] != msgkit.Hex(d1) || ev["action"] != "scheduled" ||
		ev["proposal_id"] != "p1" || ev["replaced_name"] != "" {
		t.Fatalf("scheduled event %v", ev)
	}
	e.rejects("same name, other digest", msgkit.Conflict, schedule("up-1", h+9, d2, "p2"))

	ctx = e.fresh()
	if code := e.k.ScheduleUpgrade(ctx, "up-1", h+9, d1, "p3"); code != msgkit.OK {
		t.Fatalf("same name and digest: %q", code)
	}
	if plan, _ := e.k.Plan(e.ctx); plan.Height != h+9 || plan.ProposalID != "p3" {
		t.Fatalf("same-name replacement %+v", plan)
	}
	if ev := attrs(t, ctx.EventManager().Events(), policy.EventUpgradePlan); ev["action"] != "replaced" || ev["replaced_name"] != "up-1" {
		t.Fatalf("same-name replaced event %v", ev)
	}

	ctx = e.fresh()
	if code := e.k.ScheduleUpgrade(ctx, "up-2", h+policy.MaxActivationLeadBlocks, d2, "p4"); code != msgkit.OK {
		t.Fatalf("other name at the lead: %q", code)
	}
	if plan, _ := e.k.Plan(e.ctx); plan.Name != "up-2" || !bytes.Equal(plan.Digest, d2) {
		t.Fatalf("other-name replacement %+v", plan)
	}
	if ev := attrs(t, ctx.EventManager().Events(), policy.EventUpgradePlan); ev["action"] != "replaced" || ev["replaced_name"] != "up-1" {
		t.Fatalf("replaced event %v", ev)
	}
	e.mustCheck()

	e.rejects("cancel another name", msgkit.NotFound, func(ctx sdk.Context) msgkit.Code { return e.k.CancelUpgrade(ctx, "up-1") })
	ctx = e.fresh()
	if code := e.k.CancelUpgrade(ctx, "up-2"); code != msgkit.OK {
		t.Fatal(code)
	}
	if _, found := e.k.Plan(e.ctx); found {
		t.Fatal("plan survived its cancel")
	}
	if ev := attrs(t, ctx.EventManager().Events(), policy.EventUpgradePlan); ev["action"] != "cancelled" || ev["name"] != "up-2" || ev["height"] == "" {
		t.Fatalf("cancelled event %v", ev)
	}

	// An applied name can never be scheduled again.
	e.store().Set(policy.AppliedKey("done"), policy.EncodeU64(3))
	e.rejects("applied name", msgkit.DuplicateID, schedule("done", h+5, d1, "p5"))
	if !e.k.UpgradeApplied(e.ctx, "done") || e.k.UpgradeApplied(e.ctx, "up-1") || e.k.UpgradeApplied(e.ctx, "Not An Id") {
		t.Fatal("UpgradeApplied")
	}
	e.store().Set(policy.AppliedKey("bad"), policy.EncodeU64(0))
	mustPanic(t, `policy: applied upgrade "bad"`, func() { e.k.UpgradeApplied(e.ctx, "bad") })
	e.store().Set(policy.PlanKey(), []byte{1})
	mustPanic(t, "policy: plan value is 1 bytes", func() { e.k.Plan(e.ctx) })
}

// TestRecordUpgrade: RecordUpgrade replaces the version map, bumps the app
// version by one, records the applied height, and deletes the plan; each
// refusal writes nothing.
func TestRecordUpgrade(t *testing.T) {
	newVM := testVersionMap()
	newVM["leases"] = 3
	delete(newVM, "svn")
	newVM["extra"] = 1
	e := newEnv(t)
	if code := e.k.ScheduleUpgrade(e.fresh(), "up-1", 20, digestOf("r"), "p1"); code != msgkit.OK {
		t.Fatal(code)
	}
	at20 := e.at(20)
	refusals := []struct {
		name string
		ctx  sdk.Context
		up   string
		h    uint64
		vm   module.VersionMap
		app  uint64
		want string
	}{
		{"name pattern", at20, "Up", 20, newVM, 2, "outside the id pattern"},
		{"height not the block", at20, "up-1", 21, newVM, 2, "upgrade height 21"},
		{"height 0", e.at(0), "up-1", 0, newVM, 2, "upgrade height 0"},
		{"other plan name", at20, "up-2", 20, newVM, 2, "stored plan is"},
		{"app version skips", at20, "up-1", 20, newVM, 3, "app version 3 after stored app version 1"},
		{"app version same", at20, "up-1", 20, newVM, 1, "app version 1 after"},
		{"empty map", at20, "up-1", 20, module.VersionMap{}, 2, "0 modules"},
		{"bad module name", at20, "up-1", 20, module.VersionMap{policy.ModuleName: 2, "Bad": 1}, 2, "is not a module name"},
		{"module version 0", at20, "up-1", 20, module.VersionMap{policy.ModuleName: 2, "x": 0}, 2, "is not a module name"},
		{"map without policy", at20, "up-1", 20, module.VersionMap{"x": 1}, 2, "version map gives policy version 0"},
		{"policy entry not the layout", at20, "up-1", 20, module.VersionMap{policy.ModuleName: 3}, 2, "the store layout is 2"},
	}
	for _, r := range refusals {
		before := e.snapshot()
		err := e.k.RecordUpgrade(r.ctx, r.up, r.h, r.vm, r.app)
		if err == nil || !strings.Contains(err.Error(), r.want) {
			t.Fatalf("%s: %v, want %q", r.name, err, r.want)
		}
		if !bytes.Equal(before, e.snapshot()) {
			t.Fatalf("%s: a refused record wrote", r.name)
		}
	}
	if err := e.k.RecordUpgrade(at20, "up-1", 20, newVM, 2); err != nil {
		t.Fatal(err)
	}
	if e.k.AppVersion(e.ctx) != 2 || !e.k.UpgradeApplied(e.ctx, "up-1") {
		t.Fatal("app version or applied row not written")
	}
	if raw := e.store().Get(policy.AppliedKey("up-1")); !bytes.Equal(raw, policy.EncodeU64(20)) {
		t.Fatalf("applied height %x", raw)
	}
	if _, found := e.k.Plan(e.ctx); found {
		t.Fatal("plan survived the upgrade")
	}
	vm := e.k.VersionMap(e.ctx)
	if len(vm) != len(newVM) || vm["leases"] != 3 || vm["extra"] != 1 {
		t.Fatalf("version map %v", vm)
	}
	if _, kept := vm["svn"]; kept {
		t.Fatal("a module missing from the new map kept its row")
	}
	if err := policyCheckAt(e, 20); err != nil {
		t.Fatal(err)
	}
	before := e.snapshot()
	if err := e.k.RecordUpgrade(e.at(30), "up-1", 30, newVM, 3); err == nil || !strings.Contains(err.Error(), "already applied") {
		t.Fatalf("second record of one name: %v", err)
	}
	if !bytes.Equal(before, e.snapshot()) {
		t.Fatal("refused second record wrote")
	}
	// A compiled plan source has no plan row: the record still applies.
	if err := e.k.RecordUpgrade(e.at(30), "up-2", 30, newVM, 3); err != nil {
		t.Fatalf("record without a plan row: %v", err)
	}
	if e.k.AppVersion(e.ctx) != 3 {
		t.Fatal("app version after the second upgrade")
	}

	t.Run("no app version", func(t *testing.T) {
		b := newBareEnv(t)
		if err := b.k.InitGenesis(b.ctx, policy.DefaultGenesisState()); err != nil {
			t.Fatal(err)
		}
		if err := b.k.RecordUpgrade(b.at(20), "up-1", 20, newVM, 2); err == nil || !strings.Contains(err.Error(), "stored app version 0") {
			t.Fatalf("record before InitVersions: %v", err)
		}
	})
	t.Run("damaged app version", func(t *testing.T) {
		d := newEnv(t)
		d.store().Set(policy.AppVersionKey(), policy.EncodeU64(0))
		if err := d.k.RecordUpgrade(d.at(20), "up-1", 20, newVM, 1); err == nil || !strings.Contains(err.Error(), "app version is not") {
			t.Fatalf("record on a damaged app version: %v", err)
		}
		mustPanic(t, "policy: app version is not", func() { d.k.AppVersion(d.ctx) })
	})
}

func policyCheckAt(e *env, height int64) error {
	saved := e.ctx
	e.ctx = e.ctx.WithBlockHeight(height)
	defer func() { e.ctx = saved }()
	return e.check()
}

// TestInitVersions: InitVersions writes the version map and the app version
// once; every refusal writes nothing.
func TestInitVersions(t *testing.T) {
	b := newBareEnv(t)
	if b.k.AppVersion(b.ctx) != 0 {
		t.Fatal("app version of an empty store")
	}
	if err := b.k.InitVersions(b.ctx, testVersionMap(), 1); err == nil || !strings.Contains(err.Error(), "layout version row missing") {
		t.Fatalf("InitVersions before InitGenesis: %v", err)
	}
	if err := b.k.InitGenesis(b.ctx, policy.DefaultGenesisState()); err != nil {
		t.Fatal(err)
	}
	refusals := []struct {
		name string
		vm   module.VersionMap
		app  uint64
		want string
	}{
		{"app version 0", testVersionMap(), 0, "app version 0"},
		{"nil map", nil, 1, "0 modules"},
		{"too many modules", bigMap(policy.MaxVersionMapModules + 1), 1, "33 modules"},
		{"bad name", module.VersionMap{policy.ModuleName: 2, "": 1}, 1, "is not a module name"},
		{"policy entry not the layout", module.VersionMap{policy.ModuleName: 1}, 1, "the store layout is 2"},
	}
	for _, r := range refusals {
		before := b.snapshot()
		if err := b.k.InitVersions(b.ctx, r.vm, r.app); err == nil || !strings.Contains(err.Error(), r.want) {
			t.Fatalf("%s: %v, want %q", r.name, err, r.want)
		}
		if !bytes.Equal(before, b.snapshot()) {
			t.Fatalf("%s: a refused InitVersions wrote", r.name)
		}
	}
	if err := b.k.InitVersions(b.ctx, bigMap(policy.MaxVersionMapModules), 1); err != nil {
		t.Fatalf("map at the bound: %v", err)
	}
	if b.k.AppVersion(b.ctx) != 1 || len(b.k.VersionMap(b.ctx)) != policy.MaxVersionMapModules {
		t.Fatal("versions not written")
	}
	before := b.snapshot()
	if err := b.k.InitVersions(b.ctx, testVersionMap(), 1); err == nil || !strings.Contains(err.Error(), "already initialized") {
		t.Fatalf("second InitVersions: %v", err)
	}
	if !bytes.Equal(before, b.snapshot()) {
		t.Fatal("second InitVersions wrote")
	}
	for name, row := range map[string][]byte{"plan": policy.PlanKey(), "applied": policy.AppliedKey("x")} {
		t.Run("refuses a store with "+name, func(t *testing.T) {
			d := newBareEnv(t)
			if err := d.k.InitGenesis(d.ctx, policy.DefaultGenesisState()); err != nil {
				t.Fatal(err)
			}
			d.store().Set(row, []byte{1})
			if err := d.k.InitVersions(d.ctx, testVersionMap(), 1); err == nil || !strings.Contains(err.Error(), "already initialized") {
				t.Fatalf("%v", err)
			}
		})
	}
}

func bigMap(n int) module.VersionMap {
	vm := module.VersionMap{policy.ModuleName: policy.ConsensusVersion}
	for i := 0; len(vm) < n; i++ {
		vm["m"+string(rune('a'+i/26))+string(rune('a'+i%26))] = 1
	}
	return vm
}

// TestWithStore: a keeper bound to a raw store reads the committed rows
// with a zero context, as the app's load-time gate does.
func TestWithStore(t *testing.T) {
	e := newEnv(t)
	if code := e.k.ScheduleUpgrade(e.fresh(), "up-1", 50, digestOf("r"), "p1"); code != msgkit.OK {
		t.Fatal(code)
	}
	if code := e.k.SetLimit(e.fresh(), msgkit.LimitMaxPageLimit, 10, "p2"); code != msgkit.OK {
		t.Fatal(code)
	}
	bound := e.k.WithStore(e.store())
	var zero sdk.Context
	if bound.AppVersion(zero) != 1 || len(bound.VersionMap(zero)) != len(testVersionMap()) {
		t.Fatal("versions through WithStore")
	}
	if plan, found := bound.Plan(zero); !found || plan.Name != "up-1" {
		t.Fatalf("plan through WithStore %+v", plan)
	}
	if v, found := bound.GovernedLimit(zero, msgkit.LimitMaxPageLimit); !found || v != 10 {
		t.Fatal("governed value through WithStore")
	}
	if caps, err := bound.SafetyCaps(zero); err != nil || caps.GetSafetyMaxPageLimit() != genesis.MaxPageLimit {
		t.Fatalf("caps through WithStore %v", err)
	}
	mustPanic(t, "WithStore needs a store", func() { e.k.WithStore(nil) })
}

// TestNewKeeperRequiresDeps: a nil store key or a missing dependency panics
// at wiring time.
func TestNewKeeperRequiresDeps(t *testing.T) {
	e := newBareEnv(t)
	auth := newFakeAuthority()
	mustPanic(t, "nil store key", func() { policy.NewKeeper(nil, policy.Deps{Versions: auth, Decisions: auth}) })
	mustPanic(t, "versions false", func() { policy.NewKeeper(e.key, policy.Deps{Decisions: auth}) })
	mustPanic(t, "decisions false", func() { policy.NewKeeper(e.key, policy.Deps{Versions: auth}) })
	if policy.NewKeeper(e.key, policy.Deps{Versions: auth, Decisions: auth}).StoreKey() != e.key {
		t.Fatal("StoreKey")
	}
}
