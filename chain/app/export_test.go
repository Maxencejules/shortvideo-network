package app

import (
	"fmt"

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

// CorruptAuthoritySideDocumentForTest overwrites the authority side
// document (key 0x20 of the authority store) with bytes that do not decode
// and commits a new version outside ABCI. It fails if the key is absent,
// so a wrong key cannot pass silently.
func (app *App) CorruptAuthoritySideDocumentForTest() error {
	store := app.CommitMultiStore().GetKVStore(app.keys[authority.ModuleName])
	key := []byte{0x20}
	if store.Get(key) == nil {
		return fmt.Errorf("no authority side document at %x", key)
	}
	store.Set(key, []byte("{"))
	app.CommitMultiStore().Commit()
	return nil
}

// StoreKeyForTest is the mounted store key of one module.
func (app *App) StoreKeyForTest(name string) *storetypes.KVStoreKey {
	return app.keys[name]
}
