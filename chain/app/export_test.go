package app

import (
	"encoding/json"
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
)

// RunMigrationsForTest runs the module manager's in-place migrations with
// the configurator New registered the module services on.
func (app *App) RunMigrationsForTest(ctx sdk.Context, from module.VersionMap) (module.VersionMap, error) {
	return app.mm.RunMigrations(ctx, app.configurator, from)
}

// VersionMapForTest is the consensus version of every registered module.
func (app *App) VersionMapForTest() module.VersionMap {
	return app.mm.GetVersionMap()
}

// versionedProviders and versionedContent report a test-only consensus
// version for the app's own registry module, so the manager has a step to
// run.
type versionedProviders struct {
	providers.AppModule
	version uint64
}

func (v versionedProviders) ConsensusVersion() uint64 { return v.version }

type versionedContent struct {
	content.AppModule
	version uint64
}

func (v versionedContent) ConsensusVersion() uint64 { return v.version }

// RunRegistryMigrationForTest registers step as the migration from the
// current ConsensusVersion v to v+1 of the registry module name on the
// configurator New built (the one every module registered its services and
// migrations on), wraps the app's own module of that name so it reports
// v+1, and runs the manager's RunMigrations from v. A duplicate
// registration (a version-v migration already registered by the module) is
// an error.
func (app *App) RunRegistryMigrationForTest(ctx sdk.Context, name string, step module.MigrationHandler) (module.VersionMap, error) {
	var wrapped module.AppModule
	var from uint64
	switch m := app.mm.Modules[name].(type) {
	case providers.AppModule:
		from = m.ConsensusVersion()
		wrapped = versionedProviders{AppModule: m, version: from + 1}
	case content.AppModule:
		from = m.ConsensusVersion()
		wrapped = versionedContent{AppModule: m, version: from + 1}
	default:
		return nil, fmt.Errorf("%s is not a registry module (%T)", name, m)
	}
	if err := app.configurator.RegisterMigration(name, from, step); err != nil {
		return nil, err
	}
	return module.NewManager(wrapped).RunMigrations(ctx, app.configurator, module.VersionMap{name: from})
}

// WriteScaffoldLayoutForTest rewrites the committed store name the way the
// goal-05/06 scaffold slot left it: no layout version key, and the module
// genesis JSON at key [0x01]. It then commits a new version outside ABCI.
// The registry store then holds what a goal-06 binary left in it.
func (app *App) WriteScaffoldLayoutForTest(name string, genesisJSON []byte) {
	store := app.CommitMultiStore().GetKVStore(app.keys[name])
	store.Delete(msgkit.LayoutVersionKey)
	store.Set([]byte{0x01}, genesisJSON)
	app.CommitMultiStore().Commit()
}

// WriteLayoutVersionForTest overwrites the layout version of the committed
// store name and commits a new version outside ABCI.
func (app *App) WriteLayoutVersionForTest(name string, version uint64) {
	msgkit.WriteLayoutVersion(app.CommitMultiStore().GetKVStore(app.keys[name]), version)
	app.CommitMultiStore().Commit()
}

// CorruptGovernedLimitForTest overwrites the governed limit key (key 0x13 ||
// key of the authority store) with 3 bytes, which are not an 8-byte value,
// and commits a new version outside ABCI. It fails if no value is set, so a
// wrong key cannot pass silently. Authority EndBlock does not read 0x13, so
// blocks still commit.
func (app *App) CorruptGovernedLimitForTest(key string) error {
	store := app.CommitMultiStore().GetKVStore(app.keys[authority.ModuleName])
	raw := append([]byte{0x13}, key...)
	if store.Get(raw) == nil {
		return fmt.Errorf("no governed limit %q at %x", key, raw)
	}
	store.Set(raw, []byte{1, 2, 3})
	app.CommitMultiStore().Commit()
	return nil
}

// ExportModulesForTest is the module manager's ExportGenesis for the named
// modules only, without the App.Export checks. Tests use it for the registry
// modules after council messages, when App.Export refuses (DR-06-01).
func (app *App) ExportModulesForTest(names ...string) (map[string]json.RawMessage, error) {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	return app.mm.ExportGenesisForModules(ctx, app.encoding.Codec, names)
}

// StoreKeyForTest is the mounted store key of one module.
func (app *App) StoreKeyForTest(name string) *storetypes.KVStoreKey {
	return app.keys[name]
}
