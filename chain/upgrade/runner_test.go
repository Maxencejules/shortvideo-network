package upgrade_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"sort"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// The runner unit tests use fakes for the app's stores (x/policy, the
// consensus param store, x/authority, the module layout keys), each over
// a real IAVL store of one msgkittest context. The fakes are test doubles
// of the interfaces in runner.go, not the app's implementations.

func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

func readU64(t testing.TB, raw []byte) uint64 {
	t.Helper()
	if len(raw) != 8 {
		t.Fatalf("value of %d bytes", len(raw))
	}
	return binary.BigEndian.Uint64(raw)
}

// fakeVersions is the x/policy stand-in: app version at 'a', version map
// rows at 'm'|name, the plan at 'p', applied upgrades at 'x'|name.
// RecordUpgrade checks as the policy keeper does before it writes.
type fakeVersions struct {
	t         testing.TB
	key       *storetypes.KVStoreKey
	recordErr error
	// skipAppVersion leaves the app version row unwritten; wrongMap writes
	// a version map with alpha off by one. Both exist for the runner's
	// read-back checks.
	skipAppVersion bool
	wrongMap       bool
}

func (f *fakeVersions) AppVersion(ctx sdk.Context) uint64 {
	raw := ctx.KVStore(f.key).Get([]byte("a"))
	if raw == nil {
		return 0
	}
	return readU64(f.t, raw)
}

func (f *fakeVersions) VersionMap(ctx sdk.Context) module.VersionMap {
	out := module.VersionMap{}
	it := storetypes.KVStorePrefixIterator(ctx.KVStore(f.key), []byte("m"))
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out[string(it.Key()[1:])] = readU64(f.t, it.Value())
	}
	return out
}

func (f *fakeVersions) RecordUpgrade(ctx sdk.Context, name string, height uint64, vm module.VersionMap, appVersion uint64) error {
	if f.recordErr != nil {
		return f.recordErr
	}
	store := ctx.KVStore(f.key)
	if height != uint64(ctx.BlockHeight()) {
		return fmt.Errorf("height %d, block %d", height, ctx.BlockHeight())
	}
	if store.Has(append([]byte("x"), name...)) {
		return fmt.Errorf("%s already applied", name)
	}
	if plan, found, _ := (&fakePlans{t: f.t, key: f.key}).Plan(ctx); found && (plan.Name != name || plan.Height != height) {
		return fmt.Errorf("stored plan %s at %d", plan.Name, plan.Height)
	}
	if f.AppVersion(ctx)+1 != appVersion {
		return fmt.Errorf("app version %d after %d", appVersion, f.AppVersion(ctx))
	}
	for old := range f.VersionMap(ctx) {
		store.Delete(append([]byte("m"), old...))
	}
	for name, v := range vm {
		if f.wrongMap && name == "alpha" {
			v++
		}
		store.Set(append([]byte("m"), name...), u64(v))
	}
	if !f.skipAppVersion {
		store.Set([]byte("a"), u64(appVersion))
	}
	store.Set(append([]byte("x"), name...), u64(height))
	store.Delete([]byte("p"))
	return nil
}

// fakePlans reads the plan row of fakeVersions: height 8 bytes, digest 32
// bytes, then the name. stale, when set, keeps reporting that plan after
// the row is deleted (a source that breaks the PlanSource contract).
type fakePlans struct {
	t     testing.TB
	key   *storetypes.KVStoreKey
	err   error
	stale *upgrade.Plan
}

func encodePlan(p upgrade.Plan) []byte {
	return append(append(u64(p.Height), p.Digest...), p.Name...)
}

func (f *fakePlans) Plan(ctx sdk.Context) (upgrade.Plan, bool, error) {
	if f.err != nil {
		return upgrade.Plan{}, false, f.err
	}
	raw := ctx.KVStore(f.key).Get([]byte("p"))
	if raw == nil {
		if f.stale != nil {
			return *f.stale, true, nil
		}
		return upgrade.Plan{}, false, nil
	}
	return upgrade.Plan{Height: readU64(f.t, raw[:8]), Digest: append([]byte(nil), raw[8:40]...), Name: string(raw[40:])}, true, nil
}

// fakeParams stores the consensus params at 0x03 as the app's param store
// does. setErr fails Set; dropSet makes Set write nothing; tamperSet makes
// Set also change Block.MaxGas.
type fakeParams struct {
	key       *storetypes.KVStoreKey
	getErr    error
	setErr    error
	dropSet   bool
	tamperSet bool
}

func (f *fakeParams) Get(ctx context.Context) (cmtproto.ConsensusParams, error) {
	if f.getErr != nil {
		return cmtproto.ConsensusParams{}, f.getErr
	}
	var params cmtproto.ConsensusParams
	raw := sdk.UnwrapSDKContext(ctx).KVStore(f.key).Get([]byte{0x03})
	if raw == nil {
		return params, errors.New("consensus params are not stored")
	}
	err := params.Unmarshal(raw)
	return params, err
}

func (f *fakeParams) Set(ctx context.Context, params cmtproto.ConsensusParams) error {
	switch {
	case f.setErr != nil:
		return f.setErr
	case f.dropSet:
		return nil
	case f.tamperSet:
		params.Block = &cmtproto.BlockParams{MaxBytes: params.Block.MaxBytes, MaxGas: params.Block.MaxGas - 1}
	}
	raw, err := params.Marshal()
	if err != nil {
		return err
	}
	sdk.UnwrapSDKContext(ctx).KVStore(f.key).Set([]byte{0x03}, raw)
	return nil
}

// fakeLayouts reads 'L'|name in the module store for each name in modules;
// an absent key is 0. extra names are reported too.
type fakeLayouts struct {
	t       testing.TB
	key     *storetypes.KVStoreKey
	modules []string
	err     error
}

func (f *fakeLayouts) LayoutVersions(ctx sdk.Context) (map[string]uint64, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := map[string]uint64{}
	for _, name := range f.modules {
		raw := ctx.KVStore(f.key).Get(append([]byte("L"), name...))
		if raw == nil {
			out[name] = 0
			continue
		}
		out[name] = readU64(f.t, raw)
	}
	return out, nil
}

// fakeCounter is the policy version at 0x04 of the authority store
// (absent means 1). bumpErr fails the bump; bumpTwice adds two.
type fakeCounter struct {
	t         testing.TB
	key       *storetypes.KVStoreKey
	bumpErr   error
	bumpTwice bool
}

func (f *fakeCounter) PolicyVersion(ctx sdk.Context) uint64 {
	raw := ctx.KVStore(f.key).Get([]byte{0x04})
	if raw == nil {
		return 1
	}
	return readU64(f.t, raw)
}

func (f *fakeCounter) BumpPolicyVersionForUpgrade(ctx sdk.Context) error {
	if f.bumpErr != nil {
		return f.bumpErr
	}
	v := f.PolicyVersion(ctx)
	if v == math.MaxUint64 {
		return errors.New("policy version overflow")
	}
	if f.bumpTwice {
		v++
	}
	ctx.KVStore(f.key).Set([]byte{0x04}, u64(v+1))
	return nil
}

// fakeMigrate stands in for the module manager's RunMigrations: for each
// layout module whose binary version is above the stored one it writes the
// layout key at the binary version, and it returns the binary's map.
func fakeMigrate(layoutKey *storetypes.KVStoreKey, target module.VersionMap) upgrade.Migrate {
	return func(ctx sdk.Context, from module.VersionMap) (module.VersionMap, error) {
		names := make([]string, 0, len(from))
		for name := range from {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			key := append([]byte("L"), name...)
			if target[name] > from[name] && ctx.KVStore(layoutKey).Has(key) {
				ctx.KVStore(layoutKey).Set(key, u64(target[name]))
			}
		}
		return vm(target), nil
	}
}

// storeSnapshot encodes every key and value of the named stores.
func storeSnapshot(ctx sdk.Context, keys map[string]*storetypes.KVStoreKey) []byte {
	names := make([]string, 0, len(keys))
	for name := range keys {
		names = append(names, name)
	}
	sort.Strings(names)
	var out []byte
	for _, name := range names {
		out = append(out, name...)
		out = append(out, 0xff)
		it := ctx.KVStore(keys[name]).Iterator(nil, nil)
		for ; it.Valid(); it.Next() {
			out = binary.BigEndian.AppendUint32(out, uint32(len(it.Key())))
			out = append(out, it.Key()...)
			out = binary.BigEndian.AppendUint32(out, uint32(len(it.Value())))
			out = append(out, it.Value()...)
		}
		it.Close()
	}
	return out
}

// prefixSnapshot is an Unchanged snapshot of every row of key except the
// listed keys.
func prefixSnapshot(key *storetypes.KVStoreKey, prefix []byte, except ...[]byte) func(sdk.Context, module.VersionMap) ([]byte, error) {
	return func(ctx sdk.Context, _ module.VersionMap) ([]byte, error) {
		var out []byte
		it := storetypes.KVStorePrefixIterator(ctx.KVStore(key), prefix)
		defer it.Close()
	rows:
		for ; it.Valid(); it.Next() {
			for _, skip := range except {
				if bytes.Equal(it.Key(), skip) {
					continue rows
				}
			}
			out = append(append(append(out, it.Key()...), 0), it.Value()...)
		}
		return out, nil
	}
}

const planHeight = msgkittest.BlockHeight // 7

var (
	runnerVM     = module.VersionMap{"alpha": 2, "beta": 2, "gamma": 1}
	runnerOldVM  = module.VersionMap{"alpha": 1, "beta": 2, "gamma": 1}
	runnerParams = cmtproto.ConsensusParams{
		Block:   &cmtproto.BlockParams{MaxBytes: 22_020_096, MaxGas: 20_000_000},
		Version: &cmtproto.VersionParams{App: 1},
	}
)

// runnerEnv is the state one block before the upgrade "up-1" (app version
// 1 -> 2, alpha 1 -> 2) and the runner of the app version 2 binary.
type runnerEnv struct {
	t        *testing.T
	ctx      sdk.Context
	keys     map[string]*storetypes.KVStoreKey
	versions *fakeVersions
	plans    *fakePlans
	params   *fakeParams
	layouts  *fakeLayouts
	counter  *fakeCounter
	binary   upgrade.Binary
	deps     upgrade.Deps
	// handler is the handler the binary's upgrade calls; nil means
	// migrateThrough. handlerCalls counts its calls.
	handler      upgrade.Handler
	handlerCalls int
}

func newRunnerEnv(t *testing.T) *runnerEnv {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, "policy", "svn", "authority", "mod")
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithEventManager(sdk.NewEventManager())
	e := &runnerEnv{t: t, ctx: ctx, keys: keys}
	e.versions = &fakeVersions{t: t, key: keys["policy"]}
	e.plans = &fakePlans{t: t, key: keys["policy"]}
	e.params = &fakeParams{key: keys["svn"]}
	e.layouts = &fakeLayouts{t: t, key: keys["mod"], modules: []string{"alpha", "beta"}}
	e.counter = &fakeCounter{t: t, key: keys["authority"]}

	policy := ctx.KVStore(keys["policy"])
	policy.Set([]byte("a"), u64(1))
	for name, v := range runnerOldVM {
		policy.Set(append([]byte("m"), name...), u64(v))
	}
	policy.Set([]byte("p"), encodePlan(upgrade.Plan{Name: "up-1", Height: uint64(planHeight), Digest: digest(5)}))
	if err := e.params.Set(ctx, runnerParams); err != nil {
		t.Fatal(err)
	}
	authority := ctx.KVStore(keys["authority"])
	authority.Set([]byte{0x04}, u64(5))
	authority.Set([]byte{0x10, 'p'}, []byte("proposal"))
	authority.Set([]byte{0x01}, []byte("config"))
	mod := ctx.KVStore(keys["mod"])
	mod.Set([]byte("Lalpha"), u64(1))
	mod.Set([]byte("Lbeta"), u64(2))
	mod.Set([]byte("k1"), []byte("protected"))
	mod.Set([]byte("d1"), []byte("module data"))

	e.binary = upgrade.Binary{AppVersion: 2, Modules: vm(runnerVM), Upgrades: []upgrade.Upgrade{{
		Name: "up-1", FromAppVersion: 1, ToAppVersion: 2,
		Handler: func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
			e.handlerCalls++
			if e.handler != nil {
				return e.handler(ctx, from, migrate)
			}
			return migrate(ctx, from)
		},
	}}}
	e.deps = upgrade.Deps{
		Plans: e.plans, Versions: e.versions, Params: e.params, Layouts: e.layouts, Counter: e.counter,
		Migrate: fakeMigrate(keys["mod"], runnerVM),
		Unchanged: []upgrade.Unchanged{
			{Name: "authority rows", Snapshot: prefixSnapshot(keys["authority"], nil, []byte{0x04})},
			{Name: "protected rows", Snapshot: prefixSnapshot(keys["mod"], []byte("k"))},
		},
	}
	return e
}

func (e *runnerEnv) runner() *upgrade.Runner {
	e.t.Helper()
	r, err := upgrade.NewRunner(e.binary, e.deps)
	if err != nil {
		e.t.Fatal(err)
	}
	return r
}

func (e *runnerEnv) snapshot() []byte { return storeSnapshot(e.ctx, e.keys) }

func (e *runnerEnv) store(name string) storetypes.KVStore { return e.ctx.KVStore(e.keys[name]) }

// failsUnchanged runs PreBlock and asserts an error of kind containing
// want, every store byte-identical, no event, and false.
func (e *runnerEnv) failsUnchanged(name string, kind error, want string) {
	e.t.Helper()
	before := e.snapshot()
	ctx := e.ctx.WithEventManager(sdk.NewEventManager())
	changed, err := e.runner().PreBlock(ctx)
	wantKind(e.t, name, err, kind, want)
	if changed {
		e.t.Fatalf("%s: a failed PreBlock reported changed consensus params", name)
	}
	if !bytes.Equal(before, e.snapshot()) {
		e.t.Fatalf("%s: a failed PreBlock changed the stores", name)
	}
	if n := len(ctx.EventManager().Events()); n != 0 {
		e.t.Fatalf("%s: a failed PreBlock emitted %d events", name, n)
	}
}

// TestRunnerAppliesUpgrade: at the plan height the runner runs the handler
// once and writes exactly the record: app version 2, the migrated version
// map, the applied row at the plan height, no plan, Version.App 2 with
// every other param unchanged, policy version +1, and one upgrade_applied
// event; it returns true (consensus params changed).
func TestRunnerAppliesUpgrade(t *testing.T) {
	e := newRunnerEnv(t)
	ctx := e.ctx.WithEventManager(sdk.NewEventManager())
	changed, err := e.runner().PreBlock(ctx)
	if err != nil || !changed {
		t.Fatalf("PreBlock: %t %v", changed, err)
	}
	if e.handlerCalls != 1 {
		t.Fatalf("handler ran %d times", e.handlerCalls)
	}
	if got := e.versions.AppVersion(e.ctx); got != 2 {
		t.Fatalf("app version %d", got)
	}
	if got := e.versions.VersionMap(e.ctx); fmt.Sprint(got) != fmt.Sprint(runnerVM) {
		t.Fatalf("version map %v", got)
	}
	if got := readU64(t, e.store("policy").Get([]byte("xup-1"))); got != uint64(planHeight) {
		t.Fatalf("applied at %d", got)
	}
	if e.store("policy").Has([]byte("p")) {
		t.Fatal("plan still stored")
	}
	params, err := e.params.Get(e.ctx)
	if err != nil || params.Version.App != 2 || params.Block.MaxGas != runnerParams.Block.MaxGas || params.Block.MaxBytes != runnerParams.Block.MaxBytes {
		t.Fatalf("params %+v %v", params, err)
	}
	if got := e.counter.PolicyVersion(e.ctx); got != 6 {
		t.Fatalf("policy version %d, want 6", got)
	}
	if got := readU64(t, e.store("mod").Get([]byte("Lalpha"))); got != 2 {
		t.Fatalf("alpha layout %d", got)
	}
	events := ctx.EventManager().Events()
	if len(events) != 1 || events[0].Type != upgrade.EventUpgradeApplied {
		t.Fatalf("events %v", events)
	}
	attrs := map[string]string{}
	for _, a := range events[0].Attributes {
		attrs[a.Key] = a.Value
	}
	want := map[string]string{upgrade.AttributeName: "up-1", upgrade.AttributeHeight: "7", upgrade.AttributeAppVersion: "2"}
	if fmt.Sprint(attrs) != fmt.Sprint(want) || len(events[0].Attributes) != 3 {
		t.Fatalf("attributes %v, want %v", events[0].Attributes, want)
	}
	// The next block runs under the per-block rule with the new app version.
	next := e.ctx.WithBlockHeight(planHeight + 1).WithEventManager(sdk.NewEventManager())
	if changed, err := e.runner().PreBlock(next); err != nil || changed {
		t.Fatalf("block after the upgrade: %t %v", changed, err)
	}
	if e.handlerCalls != 1 {
		t.Fatalf("handler ran again: %d", e.handlerCalls)
	}
}

// TestRunnerHandlerSeesPreUpgradeState: the handler runs before the record:
// it sees app version 1, the old version map, the plan, Version.App 1, and
// the old policy version, and its writes reach the state only after every
// check passed.
func TestRunnerHandlerSeesPreUpgradeState(t *testing.T) {
	e := newRunnerEnv(t)
	e.handler = func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
		if got := e.versions.AppVersion(ctx); got != 1 {
			return nil, fmt.Errorf("handler saw app version %d", got)
		}
		if fmt.Sprint(from) != fmt.Sprint(runnerOldVM) || fmt.Sprint(e.versions.VersionMap(ctx)) != fmt.Sprint(runnerOldVM) {
			return nil, fmt.Errorf("handler saw version map %v / %v", from, e.versions.VersionMap(ctx))
		}
		if _, found, _ := e.plans.Plan(ctx); !found {
			return nil, errors.New("handler saw no plan")
		}
		if params, _ := e.params.Get(ctx); params.Version.App != 1 {
			return nil, fmt.Errorf("handler saw Version.App %d", params.Version.App)
		}
		if got := e.counter.PolicyVersion(ctx); got != 5 {
			return nil, fmt.Errorf("handler saw policy version %d", got)
		}
		from["alpha"] = 99 // the handler's copy; the runner keeps its own
		ctx.KVStore(e.keys["mod"]).Set([]byte("d2"), []byte("handler row"))
		if e.store("mod").Has([]byte("d2")) {
			return nil, errors.New("handler write reached the block state before the checks")
		}
		from["alpha"] = 1
		return migrate(ctx, from)
	}
	changed, err := e.runner().PreBlock(e.ctx)
	if err != nil || !changed {
		t.Fatalf("PreBlock: %t %v", changed, err)
	}
	if !bytes.Equal(e.store("mod").Get([]byte("d2")), []byte("handler row")) {
		t.Fatal("handler write was not kept after success")
	}
}

// TestRunnerPreconditions: each state the runner must refuse before the
// handler runs, including the three RunMigrations gaps (a stored module the
// binary lacks, a binary module the stored map lacks, a stored version
// above the binary's). Each leaves every store unchanged, emits nothing,
// and never calls the handler.
func TestRunnerPreconditions(t *testing.T) {
	rows := []struct {
		name string
		edit func(*runnerEnv)
		want string
	}{
		{"stored module the binary lacks", func(e *runnerEnv) { e.store("policy").Set([]byte("mzeta"), u64(1)) },
			"stored module zeta is not in this binary"},
		{"binary module the stored map lacks", func(e *runnerEnv) { e.store("policy").Delete([]byte("mgamma")) },
			"module gamma is in this binary but not in the stored version map"},
		{"stored version above the binary", func(e *runnerEnv) {
			e.store("policy").Set([]byte("mbeta"), u64(3))
			e.store("mod").Set([]byte("Lbeta"), u64(3))
		}, "module beta stored version 3 is outside 1..2"},
		{"stored version 0", func(e *runnerEnv) { e.store("policy").Set([]byte("mgamma"), u64(0)) },
			"module gamma stored version 0 is outside 1..1"},
		{"too many stored modules", func(e *runnerEnv) {
			for i := 0; i <= upgrade.MaxModules; i++ {
				e.store("policy").Set([]byte(fmt.Sprintf("mz%02d", i)), u64(1))
			}
		}, "more than 32"},
		{"layout key differs from the stored map", func(e *runnerEnv) { e.store("mod").Set([]byte("Lalpha"), u64(2)) },
			"alpha store has layout version 2; the version map says 1"},
		{"layout key absent", func(e *runnerEnv) { e.store("mod").Delete([]byte("Lbeta")) },
			"beta store has layout version 0; the version map says 2"},
		{"layout key of a module without an entry", func(e *runnerEnv) { e.layouts.modules = append(e.layouts.modules, "zeta") },
			"zeta store has a layout key, but the version map has no zeta entry"},
		{"layout read error", func(e *runnerEnv) { e.layouts.err = errors.New("layout damaged") }, "layout damaged"},
		{"consensus params without a version", func(e *runnerEnv) {
			p := runnerParams
			p.Version = nil
			if err := e.params.Set(e.ctx, p); err != nil {
				e.t.Fatal(err)
			}
		}, "stored consensus Version.App (absent) differs from the stored app version 1"},
		{"consensus Version.App differs", func(e *runnerEnv) {
			p := runnerParams
			p.Version = &cmtproto.VersionParams{App: 2}
			if err := e.params.Set(e.ctx, p); err != nil {
				e.t.Fatal(err)
			}
		}, "stored consensus Version.App 2 differs from the stored app version 1"},
		{"consensus params unreadable", func(e *runnerEnv) { e.params.getErr = errors.New("params damaged") }, "params damaged"},
		{"snapshot error", func(e *runnerEnv) {
			e.deps.Unchanged = append(e.deps.Unchanged, upgrade.Unchanged{Name: "broken", Snapshot: func(sdk.Context, module.VersionMap) ([]byte, error) {
				return nil, errors.New("snapshot damaged")
			}})
		}, "snapshot broken before upgrade up-1"},
	}
	for _, row := range rows {
		e := newRunnerEnv(t)
		row.edit(e)
		e.failsUnchanged(row.name, upgrade.ErrPrecondition, row.want)
		if e.handlerCalls != 0 {
			t.Fatalf("%s: the handler ran", row.name)
		}
	}
}

// TestRunnerPostConditions: each handler result the runner must refuse
// after the handler: a returned map that is not the binary's, a layout key
// that does not match it, a write to a row the runner owns (app version,
// version map, plan, consensus params, policy version), and a change to
// each Unchanged class. Each leaves every store unchanged and emits
// nothing.
func TestRunnerPostConditions(t *testing.T) {
	then := func(edit func(e *runnerEnv, ctx sdk.Context)) func(*runnerEnv) upgrade.Handler {
		return func(e *runnerEnv) upgrade.Handler {
			return func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
				out, err := migrate(ctx, from)
				edit(e, ctx)
				return out, err
			}
		}
	}
	rows := []struct {
		name    string
		handler func(*runnerEnv) upgrade.Handler
		want    string
	}{
		{"handler returns the stored map", func(*runnerEnv) upgrade.Handler {
			return func(_ sdk.Context, from module.VersionMap, _ upgrade.Migrate) (module.VersionMap, error) {
				return from, nil
			}
		}, "upgrade up-1 returned module alpha at version 1; this binary declares 2"},
		{"handler returns an extra module", func(e *runnerEnv) upgrade.Handler {
			return func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
				out, err := migrate(ctx, from)
				out["zeta"] = 1
				return out, err
			}
		}, "returned module zeta at version 1; this binary declares (absent)"},
		{"handler returns a map without a module", func(e *runnerEnv) upgrade.Handler {
			return func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
				out, err := migrate(ctx, from)
				delete(out, "gamma")
				return out, err
			}
		}, "returned module gamma at version (absent); this binary declares 1"},
		{"handler skips the layout migration", func(*runnerEnv) upgrade.Handler {
			return func(sdk.Context, module.VersionMap, upgrade.Migrate) (module.VersionMap, error) {
				return vm(runnerVM), nil
			}
		}, "alpha store has layout version 1; the version map says 2"},
		{"handler writes the app version", then(func(e *runnerEnv, ctx sdk.Context) {
			ctx.KVStore(e.keys["policy"]).Set([]byte("a"), u64(2))
		}), "handler changed the stored app version from 1 to 2"},
		{"handler writes the version map", then(func(e *runnerEnv, ctx sdk.Context) {
			ctx.KVStore(e.keys["policy"]).Set([]byte("malpha"), u64(2))
		}), "handler changed the stored version map entry alpha"},
		{"handler deletes the plan", then(func(e *runnerEnv, ctx sdk.Context) {
			ctx.KVStore(e.keys["policy"]).Delete([]byte("p"))
		}), "handler changed or removed the upgrade plan"},
		{"handler changes the plan digest", then(func(e *runnerEnv, ctx sdk.Context) {
			ctx.KVStore(e.keys["policy"]).Set([]byte("p"), encodePlan(upgrade.Plan{Name: "up-1", Height: uint64(planHeight), Digest: digest(6)}))
		}), "handler changed or removed the upgrade plan"},
		{"handler changes a consensus param", then(func(e *runnerEnv, ctx sdk.Context) {
			p := runnerParams
			p.Block = &cmtproto.BlockParams{MaxBytes: 1, MaxGas: 1}
			_ = e.params.Set(ctx, p)
		}), "handler changed the consensus params"},
		{"handler sets Version.App itself", then(func(e *runnerEnv, ctx sdk.Context) {
			p := runnerParams
			p.Version = &cmtproto.VersionParams{App: 2}
			_ = e.params.Set(ctx, p)
		}), "handler changed the consensus params"},
		{"handler bumps the policy version", then(func(e *runnerEnv, ctx sdk.Context) {
			_ = e.counter.BumpPolicyVersionForUpgrade(ctx)
		}), "handler changed the policy version from 5 to 6"},
		{"handler changes an authority row", then(func(e *runnerEnv, ctx sdk.Context) {
			ctx.KVStore(e.keys["authority"]).Set([]byte{0x10, 'p'}, []byte("rewritten"))
		}), "upgrade up-1 changed authority rows"},
		{"handler adds an authority row", then(func(e *runnerEnv, ctx sdk.Context) {
			ctx.KVStore(e.keys["authority"]).Set([]byte{0x15, 'k'}, []byte("retired"))
		}), "upgrade up-1 changed authority rows"},
		{"handler deletes a protected row", then(func(e *runnerEnv, ctx sdk.Context) {
			ctx.KVStore(e.keys["mod"]).Delete([]byte("k1"))
		}), "upgrade up-1 changed protected rows"},
		{"handler returns an error", func(*runnerEnv) upgrade.Handler {
			return func(sdk.Context, module.VersionMap, upgrade.Migrate) (module.VersionMap, error) {
				return nil, errors.New("handler bug")
			}
		}, "handler bug"},
	}
	for _, row := range rows {
		e := newRunnerEnv(t)
		e.handler = row.handler(e)
		kind := upgrade.ErrPostcondition
		if row.name == "handler returns an error" {
			kind = upgrade.ErrHandler
		}
		e.failsUnchanged(row.name, kind, row.want)
		if e.handlerCalls != 1 {
			t.Fatalf("%s: handler ran %d times", row.name, e.handlerCalls)
		}
	}

	// A snapshot that fails only after the handler.
	e := newRunnerEnv(t)
	calls := 0
	e.deps.Unchanged = append(e.deps.Unchanged, upgrade.Unchanged{Name: "late", Snapshot: func(sdk.Context, module.VersionMap) ([]byte, error) {
		calls++
		if calls > 1 {
			return nil, errors.New("snapshot damaged after")
		}
		return nil, nil
	}})
	e.failsUnchanged("snapshot error after the handler", upgrade.ErrPostcondition, "snapshot late after upgrade up-1 handler")

	// The Unchanged snapshot receives the stored map before and the migrated
	// map after the handler.
	e = newRunnerEnv(t)
	var seen []string
	e.deps.Unchanged = append(e.deps.Unchanged, upgrade.Unchanged{Name: "versions", Snapshot: func(_ sdk.Context, vm module.VersionMap) ([]byte, error) {
		seen = append(seen, fmt.Sprint(vm))
		return nil, nil
	}})
	if _, err := e.runner().PreBlock(e.ctx); err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 || seen[0] != fmt.Sprint(runnerOldVM) || seen[1] != fmt.Sprint(runnerVM) {
		t.Fatalf("snapshot version maps %v", seen)
	}
}

// TestRunnerWritesOnSuccessOnly: an error at each step after the handler
// (each record write and each read-back) leaves every store unchanged and
// emits nothing, so a failed attempt leaves the block state as it was and
// the same binary repeats the attempt; a control run afterwards on the same
// state applies.
func TestRunnerWritesOnSuccessOnly(t *testing.T) {
	rows := []struct {
		name string
		edit func(*runnerEnv)
		want string
	}{
		{"record refused", func(e *runnerEnv) { e.versions.recordErr = errors.New("record refused") }, "record upgrade up-1: record refused"},
		{"Version.App write refused", func(e *runnerEnv) { e.params.setErr = errors.New("params refused") }, "set consensus Version.App to 2: params refused"},
		{"policy version bump refused", func(e *runnerEnv) { e.counter.bumpErr = errors.New("bump refused") }, "bump the policy version for upgrade up-1: bump refused"},
		{"policy version at the maximum", func(e *runnerEnv) { e.store("authority").Set([]byte{0x04}, u64(math.MaxUint64)) }, "policy version overflow"},
		{"record leaves the app version", func(e *runnerEnv) { e.versions.skipAppVersion = true }, "stored app version is 1 after upgrade up-1, want 2"},
		{"record writes another version map", func(e *runnerEnv) { e.versions.wrongMap = true }, "stored version map entry alpha after upgrade up-1"},
		{"Version.App not written", func(e *runnerEnv) { e.params.dropSet = true }, "stored consensus Version.App is 1 after upgrade up-1, want 2"},
		{"another consensus param written", func(e *runnerEnv) { e.params.tamperSet = true }, "consensus params other than Version.App changed at upgrade up-1"},
		{"policy version moved by two", func(e *runnerEnv) { e.counter.bumpTwice = true }, "policy version is 7 after upgrade up-1, want 5 + 1"},
		{"plan source keeps reporting the upgrade", func(e *runnerEnv) {
			e.plans.stale = &upgrade.Plan{Name: "up-1", Height: uint64(planHeight), Digest: digest(5)}
		}, "the plan source still reports upgrade up-1 after it was recorded"},
	}
	for _, row := range rows {
		e := newRunnerEnv(t)
		row.edit(e)
		e.failsUnchanged(row.name, upgrade.ErrRecord, row.want)
		if row.name == "policy version at the maximum" {
			continue
		}
		// Undo the injected fault: the same state and binary now apply.
		*e.versions = fakeVersions{t: t, key: e.keys["policy"]}
		*e.params = fakeParams{key: e.keys["svn"]}
		*e.counter = fakeCounter{t: t, key: e.keys["authority"]}
		e.plans.stale = nil
		if changed, err := e.runner().PreBlock(e.ctx); err != nil || !changed {
			t.Fatalf("%s: retry after the fault: %t %v", row.name, changed, err)
		}
	}
}

// TestRunnerHandlerPanicIsAnError: a panic in the handler, or in a
// migration the handler runs through Migrate, after it wrote to the cache
// context, is an ErrHandler error carrying the panic value: every store is
// unchanged, no event is emitted, and PreBlock reports no change. The plan
// stays due, so the same state with a handler that does not panic applies.
func TestRunnerHandlerPanicIsAnError(t *testing.T) {
	rows := []struct {
		name string
		edit func(*runnerEnv)
	}{
		{"handler panics", func(e *runnerEnv) {
			e.handler = func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
				ctx.KVStore(e.keys["mod"]).Set([]byte("d1"), []byte("half written"))
				panic("migration bug")
			}
		}},
		{"migration panics", func(e *runnerEnv) {
			e.deps.Migrate = func(ctx sdk.Context, from module.VersionMap) (module.VersionMap, error) {
				ctx.KVStore(e.keys["mod"]).Set([]byte("Lalpha"), u64(2))
				panic(errors.New("migration bug"))
			}
		}},
	}
	for _, row := range rows {
		e := newRunnerEnv(t)
		row.edit(e)
		e.failsUnchanged(row.name, upgrade.ErrHandler, "upgrade up-1 at height 7: panic: migration bug")
		e.handler = nil
		e.deps.Migrate = fakeMigrate(e.keys["mod"], runnerVM)
		if changed, err := e.runner().PreBlock(e.ctx); err != nil || !changed {
			t.Fatalf("%s: retry with a corrected handler: %t %v", row.name, changed, err)
		}
	}
}

// TestRunnerPreBlockRule: PreBlock applies CheckBlock before any write:
// with nothing due it writes nothing and returns false; an old binary at
// the plan height, an upgrade binary below it, a plan below the block
// height, a plan read error, and height 0 each fail with nothing written.
func TestRunnerPreBlockRule(t *testing.T) {
	e := newRunnerEnv(t)
	before := e.snapshot()
	early := e.ctx.WithBlockHeight(planHeight - 1).WithEventManager(sdk.NewEventManager())
	if _, err := e.runner().PreBlock(early); !errors.Is(err, upgrade.ErrWrongBinary) {
		t.Fatalf("upgrade binary below the plan height: %v", err)
	}
	old := upgrade.Binary{AppVersion: 1, Modules: vm(runnerOldVM)}
	oldRunner, err := upgrade.NewRunner(old, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	if changed, err := oldRunner.PreBlock(early); err != nil || changed {
		t.Fatalf("old binary below the plan height: %t %v", changed, err)
	}
	_, err = oldRunner.PreBlock(e.ctx)
	wantKind(t, "old binary at the plan height", err, upgrade.ErrUpgradeNeeded, `UPGRADE "up-1" NEEDED at height 7 release_digest=`)
	late := e.ctx.WithBlockHeight(planHeight + 1)
	_, err = e.runner().PreBlock(late)
	wantKind(t, "plan below the block height", err, upgrade.ErrIntegrity, "upgrade up-1 at height 7 is below block height 8")
	_, err = e.runner().PreBlock(e.ctx.WithBlockHeight(0))
	wantKind(t, "height 0", err, upgrade.ErrIntegrity, "block height 0")
	e.plans.err = errors.New("plan damaged")
	_, err = e.runner().PreBlock(e.ctx)
	wantKind(t, "plan read error", err, upgrade.ErrIntegrity, "plan damaged")
	if !bytes.Equal(before, e.snapshot()) || e.handlerCalls != 0 {
		t.Fatalf("a refused block changed the stores or ran the handler (%d calls)", e.handlerCalls)
	}
}

// TestRunnerGateReadsState: Runner.Gate reads the gate state through the
// runner's stores. With L one block before the plan, the upgrade binary
// starts in the window (row 3) and the old binary is refused (row 2); two
// blocks before, the upgrade binary is too early (row 4) and the old
// binary starts (row 1). After the upgrade applied at the plan height, the
// upgrade binary starts (row 1) and the old binary is refused as older
// than the state (row 5). A read error of each store is ErrIntegrity.
func TestRunnerGateReadsState(t *testing.T) {
	e := newRunnerEnv(t)
	r := e.runner()
	old, err := upgrade.NewRunner(upgrade.Binary{AppVersion: 1, Modules: vm(runnerOldVM)}, e.deps)
	if err != nil {
		t.Fatal(err)
	}
	gate := func(name string, r *upgrade.Runner, last uint64, wantRow int) {
		t.Helper()
		decision, err := r.Gate(e.ctx, last)
		var refusal *upgrade.RefusalError
		switch {
		case wantRow == 1 || wantRow == 3:
			if err != nil || decision.Row != wantRow {
				t.Fatalf("%s: %+v %v, want row %d", name, decision, err, wantRow)
			}
		case !errors.As(err, &refusal) || refusal.Row != wantRow:
			t.Fatalf("%s: %v, want row %d", name, err, wantRow)
		}
	}
	gate("upgrade binary one block before the plan", r, uint64(planHeight-1), 3)
	gate("old binary one block before the plan", old, uint64(planHeight-1), 2)
	gate("upgrade binary two blocks before the plan", r, uint64(planHeight-2), 4)
	gate("old binary two blocks before the plan", old, uint64(planHeight-2), 1)
	if _, err := r.PreBlock(e.ctx); err != nil {
		t.Fatal(err)
	}
	gate("upgrade binary after the upgrade", r, uint64(planHeight), 1)
	gate("old binary after the upgrade", old, uint64(planHeight), 5)

	s, err := r.StoredState(e.ctx, 9)
	if err != nil || s.LastHeight != 9 || s.AppVersion != 2 || s.ConsensusAppVersion != 2 || s.Plan != nil ||
		fmt.Sprint(s.Modules) != fmt.Sprint(runnerVM) || fmt.Sprint(s.Layouts) != fmt.Sprint(map[string]uint64{"alpha": 2, "beta": 2}) {
		t.Fatalf("stored state %+v %v", s, err)
	}
	for _, fault := range []struct {
		name string
		set  func()
		want string
	}{
		{"params", func() { e.params.getErr = errors.New("params damaged") }, "read consensus params: params damaged"},
		{"layouts", func() { e.layouts.err = errors.New("layout damaged") }, "read layout versions: layout damaged"},
		{"plan", func() { e.plans.err = errors.New("plan damaged") }, "read upgrade plan: plan damaged"},
	} {
		fault.set()
		_, err := r.Gate(e.ctx, 9)
		wantKind(t, fault.name, err, upgrade.ErrIntegrity, fault.want)
		e.params.getErr, e.layouts.err, e.plans.err = nil, nil, nil
	}
}

// TestNewRunnerRefusesBadDeps: NewRunner refuses an invalid binary, each
// missing dependency, and malformed post-condition checks; it keeps its own
// copy of the declaration.
func TestNewRunnerRefusesBadDeps(t *testing.T) {
	e := newRunnerEnv(t)
	bad := e.binary
	bad.AppVersion = 3
	if _, err := upgrade.NewRunner(bad, e.deps); !errors.Is(err, upgrade.ErrInvalidBinary) {
		t.Fatalf("invalid binary: %v", err)
	}
	noop := func(sdk.Context, module.VersionMap) ([]byte, error) { return nil, nil }
	var tooMany []upgrade.Unchanged
	for i := 0; i <= upgrade.MaxUnchanged; i++ {
		tooMany = append(tooMany, upgrade.Unchanged{Name: fmt.Sprintf("c%d", i), Snapshot: noop})
	}
	rows := []struct {
		name string
		edit func(*upgrade.Deps)
		want string
	}{
		{"no plan source", func(d *upgrade.Deps) { d.Plans = nil }, "plans false"},
		{"no version store", func(d *upgrade.Deps) { d.Versions = nil }, "versions false"},
		{"no param store", func(d *upgrade.Deps) { d.Params = nil }, "params false"},
		{"no layout source", func(d *upgrade.Deps) { d.Layouts = nil }, "layouts false"},
		{"no policy counter", func(d *upgrade.Deps) { d.Counter = nil }, "counter false"},
		{"no migrate", func(d *upgrade.Deps) { d.Migrate = nil }, "migrate false"},
		{"no post-condition check", func(d *upgrade.Deps) { d.Unchanged = nil }, "no post-condition checks (I13)"},
		{"empty post-condition list", func(d *upgrade.Deps) { d.Unchanged = []upgrade.Unchanged{} }, "no post-condition checks (I13)"},
		{"too many checks", func(d *upgrade.Deps) { d.Unchanged = tooMany }, "33 post-condition checks"},
		{"check without a name", func(d *upgrade.Deps) { d.Unchanged = []upgrade.Unchanged{{Snapshot: noop}} }, "name of 0 bytes"},
		{"check name too long", func(d *upgrade.Deps) {
			d.Unchanged = []upgrade.Unchanged{{Name: string(bytes.Repeat([]byte("n"), 65)), Snapshot: noop}}
		}, "name of 65 bytes"},
		{"check without a snapshot", func(d *upgrade.Deps) { d.Unchanged = []upgrade.Unchanged{{Name: "x"}} }, "check x has no snapshot"},
		{"two checks of one name", func(d *upgrade.Deps) {
			d.Unchanged = []upgrade.Unchanged{{Name: "x", Snapshot: noop}, {Name: "x", Snapshot: noop}}
		}, "check x is declared twice"},
	}
	for _, row := range rows {
		deps := e.deps
		row.edit(&deps)
		_, err := upgrade.NewRunner(e.binary, deps)
		wantKind(t, row.name, err, upgrade.ErrInvalidDeps, row.want)
	}

	r := e.runner()
	e.binary.Modules["alpha"] = 9
	e.binary.Upgrades[0].Name = "changed"
	if got := r.Binary(); got.Modules["alpha"] != 2 || got.Upgrades[0].Name != "up-1" {
		t.Fatalf("runner declaration follows the caller's maps: %+v", got)
	}
	got := r.Binary()
	got.Modules["alpha"] = 7
	if r.Binary().Modules["alpha"] != 2 {
		t.Fatal("Binary() returned the runner's own map")
	}
}
