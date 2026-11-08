// Package onchain_test runs the load gate, the per-block rule, and the
// runner of chain/upgrade against the production plan source,
// upgrade.CouncilPlans, over a real x/policy keeper and IAVL store (owner
// question Q2, option "Council plan, fail closed (Recommended)"). Plans are
// stored, replaced, and cancelled by the x/policy writer methods that an
// executed council software_upgrade or cancel_upgrade payload calls, and
// recorded by the keeper's real RecordUpgrade through the runner. The
// x/authority keeper is real as well: it is the policy version counter and
// x/policy's VersionSource.
//
// The consensus param store, the layout reader, the migration, and the
// adapter from the x/policy keeper to upgrade.PlanRows are unit-test
// stand-ins for the app's implementations (chain/app wires the real ones).
// This package is separate from chain/upgrade's own tests so that package
// builds and tests without the module keepers.
package onchain_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
)

const (
	// scheduledAt is the height of the block whose council execute stores
	// the plan "up-1".
	scheduledAt uint64 = 10
	// planHeight is the height of "up-1": 90 blocks above the minimum lead
	// (msgkit.MinActivationLeadBlocks, a PROPOSED constant).
	planHeight = scheduledAt + msgkit.MinActivationLeadBlocks + 90
	// replacedAt is the height of a later council execute that replaces or
	// cancels "up-1", before its height.
	replacedAt = scheduledAt + 50
	// replanHeight is the height of the replacing plan "up-2".
	replanHeight = replacedAt + msgkit.MinActivationLeadBlocks + 140
)

// authorityPolicyVersionKey is the x/authority store key of the policy
// version counter.
var authorityPolicyVersionKey = []byte{0x04}

var (
	oldVM     = module.VersionMap{"mod": 1, policy.ModuleName: policy.ConsensusVersion, "svn": 1}
	newVM     = module.VersionMap{"mod": 2, policy.ModuleName: policy.ConsensusVersion, "svn": 1}
	digestUp1 = sha("release up-1")
	digestUp2 = sha("release up-2")
	params    = cmtproto.ConsensusParams{
		Block:   &cmtproto.BlockParams{MaxBytes: 22_020_096, MaxGas: 20_000_000},
		Version: &cmtproto.VersionParams{App: 1},
	}
)

func sha(label string) []byte {
	sum := sha256.Sum256([]byte(label))
	return sum[:]
}

func clone(vm module.VersionMap) module.VersionMap {
	out := module.VersionMap{}
	for name, v := range vm {
		out[name] = v
	}
	return out
}

func migrateThrough(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
	return migrate(ctx, from)
}

// b1 is the binary at app version 1, with no handler.
func b1() upgrade.Binary { return upgrade.Binary{AppVersion: 1, Modules: clone(oldVM)} }

// b2 is the binary at app version 2 with the handler for "up-1" (1 -> 2),
// which migrates mod from 1 to 2.
func b2() upgrade.Binary {
	return upgrade.Binary{AppVersion: 2, Modules: clone(newVM), Upgrades: []upgrade.Upgrade{
		{Name: "up-1", FromAppVersion: 1, ToAppVersion: 2, Handler: migrateThrough},
	}}
}

// withModules returns b with its module map replaced.
func withModules(b upgrade.Binary, vm module.VersionMap) upgrade.Binary {
	b.Modules = clone(vm)
	return b
}

// policyRows is the adapter the app places in chain/app (chain/upgrade may
// not import x/policy, plan section 3): StoredPlan converts the x/policy
// plan row to upgrade.Plan, and UpgradeApplied is the keeper's own method.
type policyRows struct{ policy.Keeper }

func (r policyRows) StoredPlan(ctx sdk.Context) (upgrade.Plan, bool) {
	p, found := r.Plan(ctx)
	return upgrade.Plan{Name: p.Name, Height: p.Height, Digest: p.Digest}, found
}

// paramStore stands in for the app's consensus param store
// (chain/app/params.go): the params as protobuf under one key of the svn
// store.
type paramStore struct{ key *storetypes.KVStoreKey }

func (p paramStore) Get(ctx context.Context) (cmtproto.ConsensusParams, error) {
	raw := sdk.UnwrapSDKContext(ctx).KVStore(p.key).Get([]byte{0x03})
	if len(raw) == 0 {
		return cmtproto.ConsensusParams{}, errors.New("consensus params are not stored")
	}
	var out cmtproto.ConsensusParams
	err := out.Unmarshal(raw)
	return out, err
}

func (p paramStore) Set(ctx context.Context, cp cmtproto.ConsensusParams) error {
	raw, err := cp.Marshal()
	if err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).KVStore(p.key).Set([]byte{0x03}, raw)
	return nil
}

// layoutReader stands in for the app's layout adapter: the layout key of
// the mod and policy stores; an absent key is 0.
type layoutReader struct {
	keys map[string]*storetypes.KVStoreKey
}

func (l layoutReader) LayoutVersions(ctx sdk.Context) (map[string]uint64, error) {
	out := map[string]uint64{}
	for _, name := range []string{"mod", policy.ModuleName} {
		v, _, err := msgkit.ReadLayoutVersion(ctx.KVStore(l.keys[name]))
		if err != nil {
			return nil, err
		}
		out[name] = v
	}
	return out, nil
}

// storeEnv stands in for the app's handler environment: the test
// context's stores by name.
type storeEnv struct {
	keys map[string]*storetypes.KVStoreKey
}

func (e storeEnv) StoreNames() []string {
	names := make([]string, 0, len(e.keys))
	for name := range e.keys {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func (e storeEnv) KVStore(ctx sdk.Context, name string) (storetypes.KVStore, bool) {
	key, ok := e.keys[name]
	if !ok {
		return nil, false
	}
	return ctx.KVStore(key), true
}

// migrateTo stands in for the module manager's RunMigrations toward the
// binary's map target: mod 1 -> 2 rewrites mod's layout key and its data
// row. It returns target.
func migrateTo(key *storetypes.KVStoreKey, target module.VersionMap) upgrade.Migrate {
	return func(ctx sdk.Context, from module.VersionMap) (module.VersionMap, error) {
		if from["mod"] == 1 && target["mod"] == 2 {
			store := ctx.KVStore(key)
			msgkit.WriteLayoutVersion(store, 2)
			store.Set([]byte("data"), []byte("v2"))
		}
		return clone(target), nil
	}
}

// rowsSnapshot encodes the rows of key, or only those under the prefixes
// when given, skipping the keys in except.
func rowsSnapshot(key *storetypes.KVStoreKey, prefixes []byte, except ...[]byte) func(sdk.Context, module.VersionMap) ([]byte, error) {
	return func(ctx sdk.Context, _ module.VersionMap) ([]byte, error) {
		var out []byte
		it := ctx.KVStore(key).Iterator(nil, nil)
		defer it.Close()
	rows:
		for ; it.Valid(); it.Next() {
			k := it.Key()
			if len(prefixes) > 0 && !bytes.Contains(prefixes, k[:1]) {
				continue
			}
			for _, skip := range except {
				if bytes.Equal(k, skip) {
					continue rows
				}
			}
			out = append(append(append(out, k...), 0xff), it.Value()...)
		}
		return out, nil
	}
}

// chain is a policy store at app version 1 (genesis and InitVersions run,
// the mod store at layout 1) with an x/authority store, consensus params at
// Version.App 1, and the production plan source.
type chain struct {
	t         *testing.T
	ctx       sdk.Context
	keys      map[string]*storetypes.KVStoreKey
	policy    policy.Keeper
	authority authority.Keeper
	plans     *upgrade.CouncilPlans
	params    paramStore
}

func newChain(t *testing.T) *chain {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, authority.ModuleName, "mod", policy.ModuleName, "svn")
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager())
	ak := authority.NewKeeper(keys[authority.ModuleName])
	pk := policy.NewKeeper(keys[policy.ModuleName], policy.Deps{Versions: ak, Decisions: ak})
	plans, err := upgrade.NewCouncilPlans(policyRows{pk})
	if err != nil {
		t.Fatal(err)
	}
	c := &chain{t: t, ctx: ctx, keys: keys, policy: pk, authority: ak, plans: plans, params: paramStore{key: keys["svn"]}}
	if err := pk.InitGenesis(ctx, policy.DefaultGenesisState()); err != nil {
		t.Fatal(err)
	}
	mod := ctx.KVStore(keys["mod"])
	msgkit.WriteLayoutVersion(mod, 1)
	mod.Set([]byte("data"), []byte("v1"))
	if err := pk.InitVersions(ctx, oldVM, 1); err != nil {
		t.Fatal(err)
	}
	if err := c.params.Set(ctx, params); err != nil {
		t.Fatal(err)
	}
	return c
}

// at is the context of the block at height h, with a fresh event manager.
func (c *chain) at(h uint64) sdk.Context {
	return c.ctx.WithBlockHeight(int64(h)).WithEventManager(sdk.NewEventManager())
}

func (c *chain) store(name string) storetypes.KVStore { return c.ctx.KVStore(c.keys[name]) }

// schedule stores a plan as an executed council software_upgrade payload
// does in the block at height h.
func (c *chain) schedule(h uint64, name string, height uint64, digest []byte, proposal string) {
	c.t.Helper()
	if code := c.policy.ScheduleUpgrade(c.at(h), name, height, digest, proposal); code != msgkit.OK {
		c.t.Fatalf("ScheduleUpgrade %s at %d: %q", name, height, code)
	}
}

// cancel deletes the plan as an executed council cancel_upgrade payload
// does in the block at height h.
func (c *chain) cancel(h uint64, name string) {
	c.t.Helper()
	if code := c.policy.CancelUpgrade(c.at(h), name); code != msgkit.OK {
		c.t.Fatalf("CancelUpgrade %s: %q", name, code)
	}
}

func (c *chain) deps(b upgrade.Binary) upgrade.Deps {
	return upgrade.Deps{
		Plans: c.plans, Versions: c.policy, Params: c.params, Layouts: layoutReader{keys: c.keys}, Counter: c.authority,
		Migrate: migrateTo(c.keys["mod"], b.Modules),
		Env:     storeEnv{keys: c.keys},
		Unchanged: []upgrade.Unchanged{
			{Name: "authority rows except the policy version", Snapshot: rowsSnapshot(c.keys[authority.ModuleName], nil, authorityPolicyVersionKey)},
			{Name: "policy caps, governed values, pending changes, plan names", Snapshot: rowsSnapshot(c.keys[policy.ModuleName],
				[]byte{policy.SafetyCapsKey()[0], policy.GovernedPrefix, policy.PendingPrefix, policy.PlanNamePrefix})},
		},
	}
}

func (c *chain) runner(b upgrade.Binary) *upgrade.Runner {
	c.t.Helper()
	r, err := upgrade.NewRunner(b, c.deps(b))
	if err != nil {
		c.t.Fatal(err)
	}
	return r
}

// gate is the load gate of b on the committed state with last height l.
func (c *chain) gate(b upgrade.Binary, l uint64) (upgrade.Decision, error) {
	return c.runner(b).Gate(c.ctx, l)
}

// preBlock runs the PreBlock step of b in the block at height h and returns
// whether it changed the consensus params, the block's events, and its
// error.
func (c *chain) preBlock(b upgrade.Binary, h uint64) (bool, sdk.Events, error) {
	ctx := c.at(h)
	changed, err := c.runner(b).PreBlock(ctx)
	return changed, ctx.EventManager().Events(), err
}

// apply schedules "up-1" and applies it with b at the plan height.
func (c *chain) apply(b upgrade.Binary) {
	c.t.Helper()
	c.schedule(scheduledAt, "up-1", planHeight, digestUp1, "proposal-1")
	if changed, _, err := c.preBlock(b, planHeight); err != nil || !changed {
		c.t.Fatalf("applying up-1: %t %v", changed, err)
	}
}

// snapshot encodes every row of every store.
func (c *chain) snapshot() []byte {
	names := make([]string, 0, len(c.keys))
	for name := range c.keys {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []byte
	for _, name := range names {
		raw, _ := rowsSnapshot(c.keys[name], nil)(c.ctx, nil)
		out = append(append(append(out, name...), 0x00), raw...)
		out = append(out, 0x00)
	}
	return out
}

// Setups shared by the tables.
var (
	planned = func(c *chain) { c.schedule(scheduledAt, "up-1", planHeight, digestUp1, "proposal-1") }
	applied = func(c *chain) { c.apply(b2()) }
	// appliedNoMigration: up-1 applied by a binary whose upgrade changes
	// only the app version (the old module map).
	appliedNoMigration = func(c *chain) { c.apply(withModules(b2(), oldVM)) }
	// cancelled: the council cancels "up-1" before its height.
	cancelled = func(c *chain) { planned(c); c.cancel(replacedAt, "up-1") }
	// replaced: the council replaces "up-1" by "up-2" before its height.
	replaced = func(c *chain) {
		planned(c)
		c.schedule(replacedAt, "up-2", replanHeight, digestUp2, "proposal-2")
	}
	// damaged: a plan row that does not decode.
	damaged = func(c *chain) { c.store(policy.ModuleName).Set(policy.PlanKey(), []byte{1, 2, 3}) }
	// replanted: after up-1 applied, a plan row naming up-1 again (written
	// directly; the council writers refuse an applied name).
	replanted = func(c *chain) {
		c.apply(b2())
		c.store(policy.ModuleName).Set(policy.PlanKey(), policy.EncodePlan(policy.Plan{
			Name: "up-1", Height: planHeight + 300, Digest: digestUp1, ProposalID: "proposal-1",
		}))
	}
)

// TestOnChainGateTable: every row of the load gate (plan 11.4) on state the
// x/policy writers produced, read through the production plan source by
// Runner.Gate. Each refusal asserts its row and reason, each start its row
// and window, and no gate read writes anything.
func TestOnChainGateTable(t *testing.T) {
	d1 := fmt.Sprintf("%x", digestUp1)
	d2 := fmt.Sprintf("%x", digestUp2)
	// b2NoMigration is b2 with the old module map: its upgrade changes
	// only the app version.
	b2NoMigration := func() upgrade.Binary { return withModules(b2(), oldVM) }
	b3 := func() upgrade.Binary {
		return upgrade.Binary{AppVersion: 3, Modules: module.VersionMap{"mod": 3, policy.ModuleName: policy.ConsensusVersion, "svn": 1},
			Upgrades: []upgrade.Upgrade{{Name: "up-3", FromAppVersion: 2, ToAppVersion: 3, Handler: migrateThrough}}}
	}
	b1Without := func() upgrade.Binary {
		vm := clone(oldVM)
		delete(vm, "svn")
		return withModules(b1(), vm)
	}
	b1Extra := func() upgrade.Binary {
		vm := clone(oldVM)
		vm["extra"] = 1
		return withModules(b1(), vm)
	}
	b1Newer := func() upgrade.Binary { return withModules(b1(), newVM) }
	goal09Home := func(c *chain) {
		store := c.store(policy.ModuleName)
		var keys [][]byte
		it := store.Iterator(nil, nil)
		for ; it.Valid(); it.Next() {
			keys = append(keys, append([]byte(nil), it.Key()...))
		}
		it.Close()
		for _, k := range keys {
			store.Delete(k)
		}
		store.Set([]byte{0x01}, []byte(`{"policy":{"policy_version":"1"}}`))
	}
	versionApp2 := func(c *chain) {
		cp := params
		cp.Version = &cmtproto.VersionParams{App: 2}
		if err := c.params.Set(c.ctx, cp); err != nil {
			c.t.Fatal(err)
		}
	}
	layoutAhead := func(c *chain) { msgkit.WriteLayoutVersion(c.store("mod"), 2) }

	rows := []struct {
		name   string
		setup  func(*chain)
		binary func() upgrade.Binary
		last   uint64
		row    int
		want   string
	}{
		{"row 1: no plan", nil, b1, 50, 1, ""},
		{"row 1: old binary, plan scheduled in the last block", planned, b1, scheduledAt, 1, ""},
		{"row 1: old binary, plan two blocks ahead", planned, b1, planHeight - 2, 1, ""},
		{"row 1: new binary after the upgrade applied", applied, b2, planHeight, 1, ""},
		{"row 1: old binary after a council cancel, at the cancelled height", cancelled, b1, planHeight - 1, 1, ""},
		{"row 1: old binary after a council replace, at the old height", replaced, b1, planHeight - 1, 1, ""},
		{"row 2: old binary after the halt", planned, b1, planHeight - 1, 2,
			fmt.Sprintf("upgrade up-1 is due at height %d; install the binary for it (release digest %s)", planHeight, d1)},
		{"row 2: old binary, the replacing plan is due", replaced, b1, replanHeight - 1, 2,
			fmt.Sprintf("upgrade up-2 is due at height %d; install the binary for it (release digest %s)", replanHeight, d2)},
		{"row 3: upgrade window", planned, b2, planHeight - 1, 3, ""},
		{"row 3: upgrade window without a module migration", planned, b2NoMigration, planHeight - 1, 3, ""},
		{"row 4: new binary one block too early", planned, b2, planHeight - 2, 4,
			fmt.Sprintf("too early: run the previous binary until height %d (upgrade up-1 applies at height %d)", planHeight-1, planHeight)},
		{"row 4: new binary right after the council execute", planned, b2, scheduledAt, 4,
			fmt.Sprintf("too early: run the previous binary until height %d", planHeight-1)},
		{"row 5: old binary after the upgrade applied", applied, b1, planHeight, 5, "state is newer than this binary (app version 2, binary 1)"},
		{"row 5: old binary after an upgrade without module migration", appliedNoMigration, b1, planHeight, 5,
			"state is newer than this binary (app version 2, binary 1)"},
		{"row 5: stored module version above the binary", applied, b2NoMigration, planHeight, 5, "module mod stored version 2, binary version 1"},
		{"row 5: stored module the binary lacks", nil, b1Without, 50, 5, "stored module svn is not in this binary"},
		{"row 6: new binary, no plan scheduled", nil, b2, 50, 6,
			"stored app version 1 is one behind this binary (2) and no upgrade plan is stored"},
		{"row 6: new binary after a council cancel", cancelled, b2, planHeight - 1, 6,
			"stored app version 1 is one behind this binary (2) and no upgrade plan is stored"},
		{"row 6: new binary after a council replace", replaced, b2, replanHeight - 1, 6,
			"this binary has no handler for the planned upgrade up-2"},
		{"row 6: plan at the last committed height", planned, b2, planHeight, 6,
			fmt.Sprintf("upgrade up-1 at height %d is not above the last committed height %d", planHeight, planHeight)},
		{"row 6: plan below the last committed height, old binary", planned, b1, planHeight + 5, 6, "is not above the last committed height"},
		{"row 6: more than one app version behind", planned, b3, planHeight - 1, 6, "stored app version 1 is more than one behind this binary (3)"},
		{"row 6: home from before goal 10", goal09Home, b1, 40, 6, "initialize a new home"},
		{"row 6: stored app version differs from Version.App", versionApp2, b1, 50, 6,
			"stored app version 1 differs from the stored consensus Version.App 2"},
		{"row 6: layout key differs from the stored map", layoutAhead, b1, 50, 6, "mod store has layout version 2; the stored version map says 1"},
		{"row 6: binary module missing from the stored map", nil, b1Extra, 50, 6, "module extra is in this binary but not in the stored version map"},
		{"row 6: same app version, older stored module", nil, b1Newer, 50, 6,
			"app version 1 matches this binary, but module mod stored version 1 differs from binary version 2"},
	}
	seen := map[int]bool{}
	for _, r := range rows {
		c := newChain(t)
		if r.setup != nil {
			r.setup(c)
		}
		before := c.snapshot()
		decision, err := c.gate(r.binary(), r.last)
		if !bytes.Equal(before, c.snapshot()) {
			t.Fatalf("%s: the gate changed the stores", r.name)
		}
		seen[r.row] = true
		if r.row == 1 || r.row == 3 {
			if err != nil {
				t.Fatalf("%s: refused: %v", r.name, err)
			}
			if decision.Row != r.row || decision.Window != (r.row == 3) {
				t.Fatalf("%s: decision %+v, want row %d", r.name, decision, r.row)
			}
			if r.row == 3 && (decision.Upgrade.Name != "up-1" || decision.Upgrade.Handler == nil) {
				t.Fatalf("%s: window upgrade %+v, want up-1", r.name, decision.Upgrade)
			}
			continue
		}
		var refusal *upgrade.RefusalError
		if !errors.As(err, &refusal) {
			t.Fatalf("%s: %v (%+v), want a refusal of row %d", r.name, err, decision, r.row)
		}
		if refusal.Row != r.row || !strings.Contains(refusal.Error(), r.want) {
			t.Fatalf("%s: row %d %q, want row %d containing %q", r.name, refusal.Row, refusal.Error(), r.row, r.want)
		}
		if decision.Row != 0 || decision.Window || decision.Upgrade.Name != "" || decision.Upgrade.Handler != nil {
			t.Fatalf("%s: a refusal returned decision %+v", r.name, decision)
		}
	}
	for want := 1; want <= 6; want++ {
		if !seen[want] {
			t.Fatalf("no row %d in the table", want)
		}
	}
}

// TestOnChainStoredState: Runner.StoredState reads, through the production
// plan source, exactly the plan the council stored (name, height, digest),
// the app version, Version.App, the version map, and the layout keys; after
// a council replace it reads the replacing plan, after a cancel and after
// the upgrade applied no plan.
func TestOnChainStoredState(t *testing.T) {
	c := newChain(t)
	planned(c)
	s, err := c.runner(b2()).StoredState(c.ctx, planHeight-1)
	if err != nil {
		t.Fatal(err)
	}
	wantLayouts := map[string]uint64{"mod": 1, policy.ModuleName: policy.ConsensusVersion}
	switch {
	case s.LastHeight != planHeight-1 || s.AppVersion != 1 || s.ConsensusAppVersion != 1:
		t.Fatalf("stored state %+v", s)
	case fmt.Sprint(s.Modules) != fmt.Sprint(oldVM) || fmt.Sprint(s.Layouts) != fmt.Sprint(wantLayouts):
		t.Fatalf("stored modules %v, layouts %v", s.Modules, s.Layouts)
	case s.Plan == nil || s.Plan.Name != "up-1" || s.Plan.Height != planHeight || !bytes.Equal(s.Plan.Digest, digestUp1):
		t.Fatalf("stored plan %+v", s.Plan)
	}

	for name, setup := range map[string]func(*chain){"replaced": replaced, "cancelled": cancelled, "applied": applied} {
		c := newChain(t)
		setup(c)
		plan, found, err := c.plans.Plan(c.ctx)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		switch name {
		case "replaced":
			if !found || plan.Name != "up-2" || plan.Height != replanHeight || !bytes.Equal(plan.Digest, digestUp2) {
				t.Fatalf("%s: plan %+v (%t)", name, plan, found)
			}
		default:
			if found {
				t.Fatalf("%s: the source still reports %+v", name, plan)
			}
		}
	}
}

// TestOnChainGateWithUndeclarableBinaries: the gate rows that need a
// binary NewRunner refuses (two handlers of the plan's name, a handler
// whose app versions do not fit), decided by Gate on the state the
// production source read.
func TestOnChainGateWithUndeclarableBinaries(t *testing.T) {
	c := newChain(t)
	planned(c)
	s, err := c.runner(b2()).StoredState(c.ctx, planHeight-1)
	if err != nil {
		t.Fatal(err)
	}
	withUpgrades := func(ups ...upgrade.Upgrade) upgrade.Binary {
		b := b2()
		b.Upgrades = ups
		return b
	}
	handler := func(from, to uint64) upgrade.Upgrade {
		return upgrade.Upgrade{Name: "up-1", FromAppVersion: from, ToAppVersion: to, Handler: migrateThrough}
	}
	for _, r := range []struct {
		name   string
		binary upgrade.Binary
		want   string
	}{
		{"two handlers of the plan's name", withUpgrades(handler(1, 2), handler(1, 2)), "this binary declares 2 handlers named up-1"},
		{"handler to another app version", withUpgrades(handler(1, 3)), "the handler for upgrade up-1 goes from app version 1 to 3; stored app version 1, binary 2"},
		{"handler from another app version", withUpgrades(handler(0, 2)), "the handler for upgrade up-1 goes from app version 0 to 2"},
	} {
		if err := r.binary.Validate(); err == nil {
			t.Fatalf("%s: Validate accepts the binary", r.name)
		}
		_, err := upgrade.Gate(s, r.binary)
		var refusal *upgrade.RefusalError
		if !errors.As(err, &refusal) || refusal.Row != 6 || !strings.Contains(refusal.Error(), r.want) {
			t.Fatalf("%s: %v, want row 6 containing %q", r.name, err, r.want)
		}
	}
}

// TestOnChainDamagedPlanRows: a plan row that does not decode, and a plan
// row naming an upgrade that already applied, make the load gate return an
// ErrIntegrity error (no decision, not a gate row; the app reports it as a
// refusal) and make PreBlock fail with ErrIntegrity before it writes
// anything, for the old and the new binary.
func TestOnChainDamagedPlanRows(t *testing.T) {
	for _, r := range []struct {
		name   string
		setup  func(*chain)
		last   uint64
		want   string
		binary func() upgrade.Binary
	}{
		{"undecodable plan row, old binary", damaged, 50, "read upgrade plan: read the committed upgrade plan rows: panic: policy: plan value is 3 bytes", b1},
		{"undecodable plan row, new binary", damaged, 50, "read the committed upgrade plan rows: panic", b2},
		{"plan of an applied upgrade, new binary", replanted, planHeight + 1, "the stored upgrade plan up-1 names an upgrade that has already applied", b2},
		{"plan of an applied upgrade, old binary", replanted, planHeight + 1, "names an upgrade that has already applied", b1},
	} {
		c := newChain(t)
		r.setup(c)
		decision, err := c.gate(r.binary(), r.last)
		if err == nil || !errors.Is(err, upgrade.ErrIntegrity) || !strings.Contains(err.Error(), r.want) {
			t.Fatalf("%s: gate %+v %v, want ErrIntegrity containing %q", r.name, decision, err, r.want)
		}
		var refusal *upgrade.RefusalError
		if errors.As(err, &refusal) || decision.Row != 0 || decision.Upgrade.Handler != nil {
			t.Fatalf("%s: gate returned a row: %+v %v", r.name, decision, err)
		}
		before := c.snapshot()
		changed, events, err := c.preBlock(r.binary(), r.last+1)
		if !errors.Is(err, upgrade.ErrIntegrity) || !strings.Contains(err.Error(), "read upgrade plan at height") {
			t.Fatalf("%s: PreBlock %v, want ErrIntegrity reading the plan", r.name, err)
		}
		if changed || len(events) != 0 || !bytes.Equal(before, c.snapshot()) {
			t.Fatalf("%s: a failed PreBlock changed something (changed %t, %d events)", r.name, changed, len(events))
		}
	}
}

// TestOnChainBlockRule: every case of the per-block rule (plan 11.5) run by
// Runner.PreBlock with the production source. A binary without the
// handler stops at the plan height, and an upgrade binary refuses every
// block below it, before anything of the block is written: every refusal,
// and every block without an upgrade, leaves every store byte-identical
// and emits no event.
func TestOnChainBlockRule(t *testing.T) {
	b3Up1 := func() upgrade.Binary {
		return upgrade.Binary{AppVersion: 3, Modules: module.VersionMap{"mod": 3, policy.ModuleName: policy.ConsensusVersion, "svn": 1},
			Upgrades: []upgrade.Upgrade{{Name: "up-1", FromAppVersion: 2, ToAppVersion: 3, Handler: migrateThrough}}}
	}
	needed := func(name string, h uint64, digest []byte) string {
		return fmt.Sprintf("UPGRADE %q NEEDED at height %d release_digest=%x", name, h, digest)
	}
	wrong := func(stored, binary, h uint64) string {
		return fmt.Sprintf("stored app version %d, binary %d: wrong binary for height %d", stored, binary, h)
	}
	rows := []struct {
		name    string
		setup   func(*chain)
		binary  func() upgrade.Binary
		height  uint64
		kind    error
		want    string
		applies bool
	}{
		{"old binary, no plan", nil, b1, 50, nil, "", false},
		{"old binary below the plan height", planned, b1, planHeight - 1, nil, "", false},
		{"old binary in the block of the council execute", planned, b1, scheduledAt + 1, nil, "", false},
		{"old binary at the plan height stops", planned, b1, planHeight, upgrade.ErrUpgradeNeeded, needed("up-1", planHeight, digestUp1), false},
		{"upgrade binary at the plan height applies", planned, b2, planHeight, nil, "", true},
		{"upgrade binary below the plan height", planned, b2, planHeight - 1, upgrade.ErrWrongBinary, wrong(1, 2, planHeight-1), false},
		{"upgrade binary replaying from the first block", planned, b2, 1, upgrade.ErrWrongBinary, wrong(1, 2, 1), false},
		{"upgrade binary with no plan", nil, b2, 50, upgrade.ErrWrongBinary, wrong(1, 2, 50), false},
		{"upgrade binary after a council cancel, at the cancelled height", cancelled, b2, planHeight, upgrade.ErrWrongBinary, wrong(1, 2, planHeight), false},
		{"old binary after a council cancel, at the cancelled height", cancelled, b1, planHeight, nil, "", false},
		{"old binary after a council replace, at the old height", replaced, b1, planHeight, nil, "", false},
		{"old binary at the replacing plan height stops", replaced, b1, replanHeight, upgrade.ErrUpgradeNeeded, needed("up-2", replanHeight, digestUp2), false},
		{"binary for the replaced plan at the replacing plan height stops", replaced, b2, replanHeight, upgrade.ErrUpgradeNeeded,
			needed("up-2", replanHeight, digestUp2), false},
		{"upgrade binary after the upgrade applied", applied, b2, planHeight + 1, nil, "", false},
		{"old binary after the upgrade applied", applied, b1, planHeight + 1, upgrade.ErrWrongBinary, wrong(2, 1, planHeight+1), false},
		{"plan below the block height", planned, b2, planHeight + 1, upgrade.ErrIntegrity,
			fmt.Sprintf("upgrade up-1 at height %d is below block height %d", planHeight, planHeight+1), false},
		{"handler from another app version at the plan height", planned, b3Up1, planHeight, upgrade.ErrWrongBinary,
			fmt.Sprintf("handler for upgrade up-1 goes from app version 2 to 3; stored app version 1, binary 3: wrong binary for height %d", planHeight), false},
		{"height 0", nil, b1, 0, upgrade.ErrIntegrity, "block height 0", false},
		{"undecodable plan row", damaged, b1, 50, upgrade.ErrIntegrity, "read upgrade plan at height 50", false},
		{"plan of an applied upgrade", replanted, b2, planHeight + 1, upgrade.ErrIntegrity, "names an upgrade that has already applied", false},
	}
	for _, r := range rows {
		c := newChain(t)
		if r.setup != nil {
			r.setup(c)
		}
		before := c.snapshot()
		changed, events, err := c.preBlock(r.binary(), r.height)
		if r.kind != nil {
			if !errors.Is(err, r.kind) || !strings.Contains(err.Error(), r.want) {
				t.Fatalf("%s: %v, want %v containing %q", r.name, err, r.kind, r.want)
			}
		} else if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		if changed != r.applies {
			t.Fatalf("%s: changed %t, want %t", r.name, changed, r.applies)
		}
		if r.applies {
			if bytes.Equal(before, c.snapshot()) || len(events) == 0 {
				t.Fatalf("%s: the upgrade wrote nothing", r.name)
			}
			continue
		}
		if !bytes.Equal(before, c.snapshot()) {
			t.Fatalf("%s: the block changed the stores", r.name)
		}
		if len(events) != 0 {
			t.Fatalf("%s: the block emitted %d events", r.name, len(events))
		}
	}
}

// TestOnChainCheckBlockUndeclarableBinaries: the block-rule cases that need
// a binary NewRunner refuses, decided by CheckBlock on the plan the
// production source read and the stored app version.
func TestOnChainCheckBlockUndeclarableBinaries(t *testing.T) {
	c := newChain(t)
	planned(c)
	plan, found, err := c.plans.Plan(c.ctx)
	if err != nil || !found {
		t.Fatalf("plan: %t %v", found, err)
	}
	stored := c.policy.AppVersion(c.ctx)
	handler := func(from, to uint64) upgrade.Upgrade {
		return upgrade.Upgrade{Name: "up-1", FromAppVersion: from, ToAppVersion: to, Handler: migrateThrough}
	}
	for _, r := range []struct {
		name string
		ups  []upgrade.Upgrade
		want string
	}{
		{"two handlers of the plan's name", []upgrade.Upgrade{handler(1, 2), handler(1, 2)}, "binary declares 2 handlers named up-1"},
		{"handler to another app version", []upgrade.Upgrade{handler(1, 3)}, "handler for upgrade up-1 goes from app version 1 to 3"},
		{"handler from another app version", []upgrade.Upgrade{handler(0, 2)}, "handler for upgrade up-1 goes from app version 0 to 2"},
	} {
		b := b2()
		b.Upgrades = r.ups
		got, apply, err := upgrade.CheckBlock(planHeight, stored, &plan, b)
		if !errors.Is(err, upgrade.ErrWrongBinary) || !strings.Contains(err.Error(), r.want) || apply || got.Name != "" {
			t.Fatalf("%s: %+v %t %v, want ErrWrongBinary containing %q", r.name, got, apply, err, r.want)
		}
	}
}

// TestOnChainUpgradeApplies: at the plan height the upgrade binary applies
// up-1 from the council plan and writes exactly the record: app version 2,
// the migrated version map and mod data, the applied row, no plan (the
// source stops reporting it, and the council cannot store it again),
// Version.App 2 with every other param unchanged, the policy version + 1
// (proposals approved on the old version become stale), the plan name row
// kept, and one upgrade_applied event. The gate then starts only the new
// binary.
func TestOnChainUpgradeApplies(t *testing.T) {
	c := newChain(t)
	planned(c)
	planNameRow := c.store(policy.ModuleName).Get(policy.PlanNameKey("up-1"))
	if !bytes.Equal(planNameRow, digestUp1) {
		t.Fatalf("plan name row %x", planNameRow)
	}
	if v := c.authority.PolicyVersion(c.ctx); v != 1 {
		t.Fatalf("policy version before %d", v)
	}
	changed, events, err := c.preBlock(b2(), planHeight)
	if err != nil || !changed {
		t.Fatalf("PreBlock: %t %v", changed, err)
	}
	if v := c.policy.AppVersion(c.ctx); v != 2 {
		t.Fatalf("app version %d", v)
	}
	if got := c.policy.VersionMap(c.ctx); fmt.Sprint(got) != fmt.Sprint(newVM) {
		t.Fatalf("version map %v", got)
	}
	if !c.policy.UpgradeApplied(c.ctx, "up-1") {
		t.Fatal("up-1 not recorded as applied")
	}
	if plan, found, err := c.plans.Plan(c.ctx); err != nil || found {
		t.Fatalf("the source still reports %+v (%t, %v)", plan, found, err)
	}
	if code := c.policy.ScheduleUpgrade(c.at(planHeight+1), "up-1", planHeight+1+msgkit.MinActivationLeadBlocks, digestUp1, "proposal-9"); code != msgkit.DuplicateID {
		t.Fatalf("the council could store the applied plan name again: %q", code)
	}
	cp, err := c.params.Get(c.ctx)
	if err != nil || cp.Version.GetApp() != 2 || cp.Block.MaxBytes != params.Block.MaxBytes || cp.Block.MaxGas != params.Block.MaxGas {
		t.Fatalf("consensus params %+v %v", cp, err)
	}
	if v := c.authority.PolicyVersion(c.ctx); v != 2 {
		t.Fatalf("policy version after %d, want 2", v)
	}
	if layout, _, _ := msgkit.ReadLayoutVersion(c.store("mod")); layout != 2 || string(c.store("mod").Get([]byte("data"))) != "v2" {
		t.Fatalf("mod layout %d data %q", layout, c.store("mod").Get([]byte("data")))
	}
	if !bytes.Equal(c.store(policy.ModuleName).Get(policy.PlanNameKey("up-1")), planNameRow) {
		t.Fatal("the plan name row changed")
	}
	abci := events.ToABCIEvents()
	if len(abci) != 1 || abci[0].Type != upgrade.EventUpgradeApplied {
		t.Fatalf("events %v", abci)
	}
	attrs := map[string]string{}
	for _, a := range abci[0].Attributes {
		attrs[a.Key] = a.Value
	}
	if attrs[upgrade.AttributeName] != "up-1" || attrs[upgrade.AttributeHeight] != fmt.Sprint(planHeight) || attrs[upgrade.AttributeAppVersion] != "2" {
		t.Fatalf("event attributes %v", attrs)
	}
	if d, err := c.gate(b2(), planHeight); err != nil || d.Row != 1 {
		t.Fatalf("gate of the new binary after the upgrade: %+v %v", d, err)
	}
	var refusal *upgrade.RefusalError
	if _, err := c.gate(b1(), planHeight); !errors.As(err, &refusal) || refusal.Row != 5 {
		t.Fatalf("gate of the old binary after the upgrade: %v", err)
	}
}

// TestOnChainHaltAndSwitch: the old binary runs every block up to the plan
// height; at the plan height it fails every attempt without writing, and a
// restart of it is refused by the gate (row 2); the new binary starts in
// the upgrade window (row 3), applies the upgrade at the plan height, and
// runs the following blocks, where the old binary fails.
func TestOnChainHaltAndSwitch(t *testing.T) {
	c := newChain(t)
	planned(c)
	for h := scheduledAt + 1; h < planHeight; h++ {
		if changed, events, err := c.preBlock(b1(), h); err != nil || changed || len(events) != 0 {
			t.Fatalf("old binary at %d: %t %v (%d events)", h, changed, err, len(events))
		}
	}
	before := c.snapshot()
	for attempt := 1; attempt <= 3; attempt++ {
		changed, events, err := c.preBlock(b1(), planHeight)
		if !errors.Is(err, upgrade.ErrUpgradeNeeded) || changed || len(events) != 0 || !bytes.Equal(before, c.snapshot()) {
			t.Fatalf("old binary at the plan height, attempt %d: %t %v (%d events)", attempt, changed, err, len(events))
		}
	}
	var refusal *upgrade.RefusalError
	if _, err := c.gate(b1(), planHeight-1); !errors.As(err, &refusal) || refusal.Row != 2 {
		t.Fatalf("restart of the old binary: %v", err)
	}
	if d, err := c.gate(b2(), planHeight-1); err != nil || d.Row != 3 || !d.Window {
		t.Fatalf("start of the new binary: %+v %v", d, err)
	}
	if changed, _, err := c.preBlock(b2(), planHeight); err != nil || !changed {
		t.Fatalf("new binary at the plan height: %t %v", changed, err)
	}
	after := c.snapshot()
	for h := planHeight + 1; h <= planHeight+5; h++ {
		if changed, events, err := c.preBlock(b2(), h); err != nil || changed || len(events) != 0 {
			t.Fatalf("new binary at %d: %t %v", h, changed, err)
		}
		if _, _, err := c.preBlock(b1(), h); !errors.Is(err, upgrade.ErrWrongBinary) {
			t.Fatalf("old binary at %d: %v", h, err)
		}
	}
	if !bytes.Equal(after, c.snapshot()) {
		t.Fatal("blocks after the upgrade changed the stores")
	}
}

// TestOnChainActivationIsHeightOnly: the block time never moves the
// switch. Below the plan height, a block time far in the future neither
// stops the old binary nor lets the new one run; at the plan height, a
// block time at the Unix epoch still stops the old binary and lets the new
// one apply the upgrade.
func TestOnChainActivationIsHeightOnly(t *testing.T) {
	future := time.Date(9999, 12, 31, 0, 0, 0, 0, time.UTC)
	epoch := time.Unix(0, 0).UTC()
	run := func(c *chain, b upgrade.Binary, h uint64, at time.Time) (bool, error) {
		return c.runner(b).PreBlock(c.at(h).WithBlockTime(at))
	}
	c := newChain(t)
	planned(c)
	if changed, err := run(c, b1(), planHeight-1, future); err != nil || changed {
		t.Fatalf("old binary below the plan height, future time: %t %v", changed, err)
	}
	if _, err := run(c, b2(), planHeight-1, future); !errors.Is(err, upgrade.ErrWrongBinary) {
		t.Fatalf("new binary below the plan height, future time: %v", err)
	}
	if _, err := run(c, b1(), planHeight, epoch); !errors.Is(err, upgrade.ErrUpgradeNeeded) {
		t.Fatalf("old binary at the plan height, epoch time: %v", err)
	}
	if changed, err := run(c, b2(), planHeight, epoch); err != nil || !changed {
		t.Fatalf("new binary at the plan height, epoch time: %t %v", changed, err)
	}
}
