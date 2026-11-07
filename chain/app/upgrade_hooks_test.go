package app

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"strconv"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
)

// Goal 10 test hooks (plan section 15). This is a _test.go file, so none of
// these symbols is linked into svnd (TestLinkedBinaryOmitsTestHooks).

// FinalizeFullSnapshotForTest is FullSnapshot over the finalize-block
// branch: for every store of SnapshotStores, the committed store plus every
// write the current block has made. After a FinalizeBlock that returned an
// error it shows what that block wrote before failing (a committed
// snapshot cannot change while no Commit runs). It panics before the first
// FinalizeBlock.
func (app *App) FinalizeFullSnapshotForTest() []byte {
	ctx := app.GetContextForFinalizeBlock(nil)
	var out []byte
	for _, name := range SnapshotStores() {
		part := snapshotStore(ctx.KVStore(app.keys[name]))
		out = binary.BigEndian.AppendUint64(out, uint64(len(name)))
		out = append(out, name...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(part)))
		out = append(out, part...)
	}
	return out
}

// AccountExistsForTest reports whether addr has an auth account in the
// committed state.
func (app *App) AccountExistsForTest(addr []byte) bool {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	return app.accounts.HasAccount(ctx, sdk.AccAddress(addr))
}

// WriteQueuedRecordForTest rewrites the decoded governed value of the
// QUEUED governed limit proposal id (authority key 0x10 || id) to value in
// the working state: it is not committed and is not a consensus message.
// The next FinalizeBlock reads it. Execute uses the record's decoded
// payload, not the canonical JSON, so an execute of that proposal then
// reaches its own key and ceiling recheck with value: no message can store
// a value outside 1..ceiling, because propose refuses it (D-1). It fails
// unless the record exists, is QUEUED, and is a governed limit record whose
// decoded value appears exactly once.
func (app *App) WriteQueuedRecordForTest(id string, value uint64) error {
	store := app.CommitMultiStore().GetKVStore(app.keys[authority.ModuleName])
	key := append([]byte{0x10}, id...)
	raw := store.Get(key)
	if raw == nil {
		return fmt.Errorf("no proposal record %s", id)
	}
	state, found, err := app.ProposalStateForTest(id)
	if err != nil || !found {
		return fmt.Errorf("proposal %s state: found %t: %v", id, found, err)
	}
	if state != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {
		return fmt.Errorf("proposal %s is %s, not QUEUED", id, state)
	}
	if !bytes.Contains(raw, []byte(`"kind":"governed_limit"`)) {
		return fmt.Errorf("proposal %s is not a governed limit proposal", id)
	}
	start := bytes.Index(raw, []byte(`"limit_value":`))
	if start < 0 || bytes.Count(raw, []byte(`"limit_value":`)) != 1 {
		return fmt.Errorf("proposal %s record has no single limit_value field", id)
	}
	start += len(`"limit_value":`)
	end := start
	for end < len(raw) && raw[end] >= '0' && raw[end] <= '9' {
		end++
	}
	if end == start {
		return fmt.Errorf("proposal %s limit_value is not a number", id)
	}
	rewritten := append(append(append([]byte(nil), raw[:start]...), strconv.FormatUint(value, 10)...), raw[end:]...)
	store.Set(key, rewritten)
	return nil
}

// PolicySnapshotForTest is ModuleSnapshot(ModulePolicy) over the committed
// state: every x/policy row in key order.
func (app *App) PolicySnapshotForTest() []byte {
	return app.ModuleSnapshot(ModulePolicy)
}

// PolicyKeeperForTest is the x/policy keeper the app runs. Tests use it to
// read committed state through an uncached context.
func (app *App) PolicyKeeperForTest() policy.Keeper {
	return app.policy
}

// CommittedContextForTest is an uncached context over the committed state
// at the last committed height, with the app's chain id.
func (app *App) CommittedContextForTest() sdk.Context {
	return app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID(), Height: app.LastBlockHeight()})
}

// GateDecisionForTest is the load gate decision New took on the committed
// state: row 1 or row 3 (Window). It is the zero decision when no block was
// committed or the app was built with SkipLoad and not loaded.
func (app *App) GateDecisionForTest() upgrade.Decision {
	return app.gate
}

// PostConditionNamesForTest are the names of the upgrade runner's I13
// checks, in check order.
func (app *App) PostConditionNamesForTest() []string {
	checks := app.postConditions()
	names := make([]string, len(checks))
	for i, check := range checks {
		names[i] = check.Name
	}
	return names
}

// LoadForTest runs the load step New runs when SkipLoad is false: it loads
// the latest committed version and applies the load gate
// (checkLayoutsAndVersions). Rehearsal tests build an app with SkipLoad,
// change its module set with the hooks below, then call it.
func (app *App) LoadForTest() error {
	return app.load()
}

// MountStoreForTest mounts one more KV store named name, which the
// committed root does not hold. Call it on an app built with SkipLoad,
// before LoadForTest; LoadForTest must then fail (a store absent from the
// root, case I5).
func (app *App) MountStoreForTest(name string) {
	app.MountStores(storetypes.NewKVStoreKey(name))
}

// versioned modules report a test-only consensus version for one of the
// app's own modules, so the module manager runs a registered migration step
// toward it.
type (
	versionedPolicy struct {
		policy.AppModule
		version uint64
	}
	versionedAuthority struct {
		authority.AppModule
		version uint64
	}
)

func (v versionedPolicy) ConsensusVersion() uint64    { return v.version }
func (v versionedAuthority) ConsensusVersion() uint64 { return v.version }

// BumpModuleVersionForTest makes the app's own module name report its
// consensus version v plus one and registers step as its migration from v
// to v+1 on the configurator New built, then rebuilds the upgrade runner,
// so the binary declares v+1 for that module. It is the rehearsal binary's
// module change (plan 12.2): call it on an app built with SkipLoad and
// with the rehearsal upgrade handlers in Config.Upgrades, before
// LoadForTest. name is one of providers, content, leases, evidence, policy,
// or authority. A module whose migration from v is already registered is
// an error.
func (app *App) BumpModuleVersionForTest(name string, step module.MigrationHandler) error {
	var wrapped module.AppModule
	var from uint64
	switch m := app.mm.Modules[name].(type) {
	case providers.AppModule:
		from = m.ConsensusVersion()
		wrapped = versionedProviders{AppModule: m, version: from + 1}
	case content.AppModule:
		from = m.ConsensusVersion()
		wrapped = versionedContent{AppModule: m, version: from + 1}
	case leases.AppModule:
		from = m.ConsensusVersion()
		wrapped = versionedLeases{AppModule: m, version: from + 1}
	case evidence.AppModule:
		from = m.ConsensusVersion()
		wrapped = versionedEvidence{AppModule: m, version: from + 1}
	case policy.AppModule:
		from = m.ConsensusVersion()
		wrapped = versionedPolicy{AppModule: m, version: from + 1}
	case authority.AppModule:
		from = m.ConsensusVersion()
		wrapped = versionedAuthority{AppModule: m, version: from + 1}
	default:
		return fmt.Errorf("%s is not a module this hook can version (%T)", name, m)
	}
	if err := app.configurator.RegisterMigration(name, from, step); err != nil {
		return err
	}
	app.mm.Modules[name] = wrapped
	return app.buildRunner()
}

// WriteGoal09LayoutForTest rewrites the committed policy and authority
// stores the way a goal-09 binary left them, and commits a new version
// outside ABCI: the policy store holds only the scaffold slot's key [0x01]
// with policyGenesisJSON (no layout key, no app version, no version map),
// and the authority store has no layout key.
func (app *App) WriteGoal09LayoutForTest(policyGenesisJSON []byte) {
	store := app.CommitMultiStore().GetKVStore(app.keys[ModulePolicy])
	var keys [][]byte
	it := store.Iterator(nil, nil)
	for ; it.Valid(); it.Next() {
		keys = append(keys, append([]byte(nil), it.Key()...))
	}
	it.Close()
	for _, key := range keys {
		store.Delete(key)
	}
	store.Set([]byte{0x01}, policyGenesisJSON)
	app.CommitMultiStore().GetKVStore(app.keys[authority.ModuleName]).Delete(msgkit.LayoutVersionKey)
	app.CommitMultiStore().Commit()
}

// FinalizeStoreRowsForTest is the number of keys of store name in the
// finalize-block branch (the committed store plus the writes of the current
// block, or of InitChain before the first block). It panics before
// InitChain or the first FinalizeBlock.
func (app *App) FinalizeStoreRowsForTest(name string) int {
	it := app.GetContextForFinalizeBlock(nil).KVStore(app.keys[name]).Iterator(nil, nil)
	defer it.Close()
	keys := 0
	for ; it.Valid(); it.Next() {
		keys++
	}
	return keys
}

// WriteCommittedRowForTest sets key to value in the committed store name,
// or deletes key when value is nil, and commits a new version outside ABCI.
// Load-gate tests use it to damage one committed upgrade row.
func (app *App) WriteCommittedRowForTest(name string, key, value []byte) {
	store := app.CommitMultiStore().GetKVStore(app.keys[name])
	if value == nil {
		store.Delete(key)
	} else {
		store.Set(key, value)
	}
	app.CommitMultiStore().Commit()
}

// WriteWorkingRowForTest sets key to value in store name in the working
// state: it is not committed and is not a consensus message. The next
// FinalizeBlock reads it, and the app can still run FinalizeBlock at its
// next height.
func (app *App) WriteWorkingRowForTest(name string, key, value []byte) {
	app.CommitMultiStore().GetKVStore(app.keys[name]).Set(key, value)
}
