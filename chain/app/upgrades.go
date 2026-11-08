package app

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"sort"

	dbm "github.com/cosmos/cosmos-db"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
)

// buildRunner builds the upgrade runner of this binary: its app version
// (binaryAppVersion), the module manager's version map, and its upgrade
// handlers, over these stores:
//
//   - plans: the council plan row of x/policy (upgrade.CouncilPlans over
//     policyPlanRows; owner question Q2, option "Council plan, fail closed
//     (Recommended)")
//   - versions: the x/policy keeper (app version, version map,
//     RecordUpgrade)
//   - params: the consensus param store of the svn store (Version.App)
//   - layouts: the layout keys of layoutStores (layoutReader)
//   - counter: the x/authority keeper (policy version, the +1 bump of an
//     applied upgrade, DR-10-38)
//   - migrate: the module manager's RunMigrations on the configurator every
//     module registered its migrations on
//   - env: the handler environment over the mounted store keys
//     (handlerEnv), through which a handler reads and writes module stores
//   - unchanged: the I13 row classes (postConditions)
func (app *App) buildRunner() error {
	plans, err := upgrade.NewCouncilPlans(policyPlanRows{keeper: app.policy})
	if err != nil {
		return err
	}
	runner, err := upgrade.NewRunner(upgrade.Binary{
		AppVersion: binaryAppVersion(app.upgradeList),
		Modules:    app.mm.GetVersionMap(),
		Upgrades:   app.upgradeList,
	}, upgrade.Deps{
		Plans:     plans,
		Versions:  app.policy,
		Params:    newConsensusParamsStore(app.keys[ModuleSVN]),
		Layouts:   layoutReader{keys: app.keys},
		Counter:   app.authority,
		Migrate:   app.runMigrations,
		Env:       newHandlerEnv(app.keys),
		Unchanged: app.postConditions(),
	})
	if err != nil {
		return err
	}
	app.upgrades = runner
	return nil
}

// runMigrations is the runner's Migrate: the module manager's in-place
// migrations from the version map from, in InitOrder.
func (app *App) runMigrations(ctx sdk.Context, from module.VersionMap) (module.VersionMap, error) {
	return app.mm.RunMigrations(ctx, app.configurator, from)
}

// handlerEnv is upgrade.Env over the app's mounted store keys: an upgrade
// handler reads and writes a module store by its name, in the cache branch
// the runner gave it. Every store is reachable, including x/authority and
// x/policy, so a handler can read them; the runner's checks after the
// handler (its own rows and the I13 classes of postConditions) refuse the
// upgrade on any change to a protected row, as they do for a module
// migration's writes.
type handlerEnv struct {
	keys  map[string]*storetypes.KVStoreKey
	names []string
}

// newHandlerEnv returns the handler environment over a copy of keys.
func newHandlerEnv(keys map[string]*storetypes.KVStoreKey) handlerEnv {
	env := handlerEnv{keys: make(map[string]*storetypes.KVStoreKey, len(keys)), names: make([]string, 0, len(keys))}
	for name, key := range keys {
		env.keys[name] = key
		env.names = append(env.names, name)
	}
	sort.Strings(env.names)
	return env
}

func (e handlerEnv) StoreNames() []string {
	return append([]string(nil), e.names...)
}

func (e handlerEnv) KVStore(ctx sdk.Context, name string) (storetypes.KVStore, bool) {
	key, ok := e.keys[name]
	if !ok {
		return nil, false
	}
	return ctx.KVStore(key), true
}

// Binary is what this binary declares: its app version, the consensus
// version of every registered module, and its upgrade handlers.
func (app *App) Binary() upgrade.Binary {
	return app.upgrades.Binary()
}

// DeclaredBinary returns what a binary with these upgrade handlers declares
// (Binary) without a node home: it builds the application on an empty
// in-memory database and does not load it. svnd version prints it.
func DeclaredBinary(upgrades []upgrade.Upgrade) (upgrade.Binary, error) {
	built, err := New(Config{DB: dbm.NewMemDB(), ChainID: Name, SkipLoad: true, Upgrades: upgrades})
	if err != nil {
		return upgrade.Binary{}, err
	}
	declared := built.Binary()
	if err := built.Close(); err != nil {
		return upgrade.Binary{}, err
	}
	return declared, nil
}

// seatAccounts is authority.AccountKeeper over the auth account keeper.
// x/authority calls it for an executed council seat payload, on the nested
// cache of that execute, after every check of the replacement: the new seat
// key's address gets an auth account, so its first council message passes
// the SDK signature check (DR-10-35; goal 06 follow-up).
type seatAccounts struct {
	keeper authkeeper.AccountKeeper
}

// EnsureAccount returns ERROR_CODE_BOUNDS, and writes nothing, for an
// address that is not 20 bytes. An address that has an account is OK and
// its account is not changed. Otherwise it creates the account (sequence 0,
// no public key, the account number the account keeper derives) and returns
// OK.
func (s seatAccounts) EnsureAccount(ctx sdk.Context, addr []byte) msgkit.Code {
	if len(addr) != genesis.AccountBytes {
		return msgkit.Bounds
	}
	if s.keeper.HasAccount(ctx, sdk.AccAddress(addr)) {
		return msgkit.OK
	}
	s.keeper.SetAccount(ctx, s.keeper.NewAccountWithAddress(ctx, sdk.AccAddress(addr)))
	return msgkit.OK
}

// policyPlanRows is upgrade.PlanRows over the x/policy keeper: StoredPlan is
// the plan row (0x06) converted to upgrade.Plan, and UpgradeApplied reads the
// applied rows (0x07). Both panic on a committed row that does not decode,
// as the keeper does; upgrade.CouncilPlans turns the panic into an error.
type policyPlanRows struct {
	keeper policy.Keeper
}

func (r policyPlanRows) StoredPlan(ctx sdk.Context) (upgrade.Plan, bool) {
	plan, found := r.keeper.Plan(ctx)
	return upgrade.Plan{Name: plan.Name, Height: plan.Height, Digest: plan.Digest}, found
}

func (r policyPlanRows) UpgradeApplied(ctx sdk.Context, name string) bool {
	return r.keeper.UpgradeApplied(ctx, name)
}

// layoutReader is upgrade.LayoutSource: the layout key of every store of
// layoutStores, 0 when the key is absent.
type layoutReader struct {
	keys map[string]*storetypes.KVStoreKey
}

func (l layoutReader) LayoutVersions(ctx sdk.Context) (map[string]uint64, error) {
	stores := layoutStores()
	out := make(map[string]uint64, len(stores))
	for _, store := range stores {
		version, _, err := msgkit.ReadLayoutVersion(ctx.KVStore(l.keys[store.name]))
		if err != nil {
			return nil, fmt.Errorf("%s store: %w", store.name, err)
		}
		out[store.name] = version
	}
	return out, nil
}

// rowClass is one class of rows the upgrade runner's self-check (I13)
// compares before and after an upgrade's handler: the rows of store whose
// first key byte is in prefixes, or, with other set, every row of store
// whose first key byte is in none of the listed prefixes of that store and
// whose key is not exactly one of except.
type rowClass struct {
	name     string
	store    string
	prefixes []byte
	other    bool
	except   [][]byte
}

// authorityRowPrefixes are the authority prefixes with a class of their own
// (chain/x/authority store.go). The policy version (0x04) moves by exactly
// one at an upgrade (DR-10-38) and the runner checks it itself.
var authorityRowPrefixes = []struct {
	name   string
	prefix byte
}{
	{"authority layout version", 0x00},
	{"authority config", 0x01},
	{"authority admission rows", 0x02},
	{"authority pending validator changes", 0x03},
	{"authority proposals", 0x10},
	{"authority business ids", 0x11},
	{"authority executed digests", 0x12},
	{"authority open counts", 0x14},
	{"authority retired seat keys", 0x15},
	{"authority retired service addresses", 0x16},
}

// authorityPolicyVersionKey is the authority policy version key. Only this
// exact key is left out of "authority other rows"; a longer key that starts
// with 0x04 is in that class.
var authorityPolicyVersionKey = []byte{0x04}

// policyRowPrefixes are the x/policy prefixes with a class of their own
// (chain/x/policy store.go): the safety caps, the governed values, the
// pending governed changes, the applied upgrades, and the plan names. The
// runner writes the applied row of an upgrade only in its record step,
// after these checks.
var policyRowPrefixes = []struct {
	name   string
	prefix byte
}{
	{"policy safety caps", 0x01},
	{"policy governed values", policy.GovernedPrefix},
	{"policy pending changes", policy.PendingPrefix},
	{"policy applied upgrades", policy.AppliedPrefix},
	{"policy plan names", policy.PlanNamePrefix},
}

// policyRunnerKeys are the x/policy rows the runner checks itself (its
// post-conditions and layout check): the layout key, the app version, and
// the plan. Only these exact keys and the version map prefix (0x05, which
// the runner decodes and compares) are left out of "policy other rows".
var policyRunnerKeys = [][]byte{msgkit.LayoutVersionKey, policy.AppVersionKey(), policy.PlanKey()}

// rowClasses are the I13 classes, in check order: one class per authority
// prefix above, every other authority row except the policy version, one
// class per x/policy prefix above, and every other x/policy row except the
// version map and policyRunnerKeys. Every authority row except the policy
// version, and every x/policy row except the rows the runner checks itself,
// is in exactly one class.
func rowClasses() []rowClass {
	classes := make([]rowClass, 0, len(authorityRowPrefixes)+len(policyRowPrefixes)+2)
	listed := make([]byte, 0, len(authorityRowPrefixes))
	for _, row := range authorityRowPrefixes {
		classes = append(classes, rowClass{name: row.name, store: authority.ModuleName, prefixes: []byte{row.prefix}})
		listed = append(listed, row.prefix)
	}
	classes = append(classes, rowClass{name: "authority other rows", store: authority.ModuleName, prefixes: listed, other: true, except: [][]byte{authorityPolicyVersionKey}})
	policyListed := []byte{policy.VersionMapPrefix}
	for _, row := range policyRowPrefixes {
		classes = append(classes, rowClass{name: row.name, store: ModulePolicy, prefixes: []byte{row.prefix}})
		policyListed = append(policyListed, row.prefix)
	}
	return append(classes, rowClass{name: "policy other rows", store: ModulePolicy, prefixes: policyListed, other: true, except: policyRunnerKeys})
}

// postConditions are the runner's I13 checks (upgrade.Unchanged), one per
// row class. A snapshot compares rows byte for byte, which is right only
// while the store keeps the layout this binary reads: when the version map
// names another layout for the class's store (an upgrade that migrates
// x/authority or x/policy), the snapshot is an error and the upgrade is
// refused until the check decodes that layout. This self-check protects
// only against an honest binary's mistakes; the control against a wrong
// binary is the review of its source and digest before operators install
// it.
func (app *App) postConditions() []upgrade.Unchanged {
	classes := rowClasses()
	out := make([]upgrade.Unchanged, len(classes))
	for i, class := range classes {
		key := app.keys[class.store]
		readable := layoutVersionOf(class.store)
		out[i] = upgrade.Unchanged{
			Name: class.name,
			Snapshot: func(ctx sdk.Context, vm module.VersionMap) ([]byte, error) {
				if vm[class.store] != readable {
					return nil, fmt.Errorf("%s: the version map names %s layout %d, and this check reads layout %d only",
						class.name, class.store, vm[class.store], readable)
				}
				return class.snapshot(ctx.KVStore(key)), nil
			},
		}
	}
	return out
}

// layoutVersionOf is the layout version of name in layoutStores, 0 when the
// store keeps none.
func layoutVersionOf(name string) uint64 {
	for _, store := range layoutStores() {
		if store.name == name {
			return store.version
		}
	}
	return 0
}

// snapshot encodes every row of the class in key order: for each row, the
// key length (8 bytes big-endian), the key, the value length, the value.
func (c rowClass) snapshot(store storetypes.KVStore) []byte {
	var out []byte
	appendRow := func(key, value []byte) {
		out = binary.BigEndian.AppendUint64(out, uint64(len(key)))
		out = append(out, key...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(value)))
		out = append(out, value...)
	}
	if !c.other {
		for _, prefix := range c.prefixes {
			it := store.Iterator([]byte{prefix}, storetypes.PrefixEndBytes([]byte{prefix}))
			for ; it.Valid(); it.Next() {
				appendRow(it.Key(), it.Value())
			}
			it.Close()
		}
		return out
	}
	it := store.Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		if bytes.IndexByte(c.prefixes, it.Key()[0]) >= 0 || c.excepted(it.Key()) {
			continue
		}
		appendRow(it.Key(), it.Value())
	}
	return out
}

// excepted reports whether key is exactly one of the class's except keys.
func (c rowClass) excepted(key []byte) bool {
	for _, exact := range c.except {
		if bytes.Equal(key, exact) {
			return true
		}
	}
	return false
}
