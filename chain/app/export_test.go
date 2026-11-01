package app

import (
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

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

// RunRegistryMigrationForTest registers step as the 1 -> 2 migration of the
// registry module name on the configurator New built (the one every module
// registered its services and migrations on), wraps the app's own module of
// that name so it reports version 2, and runs the manager's RunMigrations
// from version 1. A duplicate registration (a version-1 migration already
// registered by the module) is an error.
func (app *App) RunRegistryMigrationForTest(ctx sdk.Context, name string, step module.MigrationHandler) (module.VersionMap, error) {
	var wrapped module.AppModule
	switch m := app.mm.Modules[name].(type) {
	case providers.AppModule:
		wrapped = versionedProviders{AppModule: m, version: 2}
	case content.AppModule:
		wrapped = versionedContent{AppModule: m, version: 2}
	default:
		return nil, fmt.Errorf("%s is not a registry module (%T)", name, m)
	}
	if err := app.configurator.RegisterMigration(name, 1, step); err != nil {
		return nil, err
	}
	return module.NewManager(wrapped).RunMigrations(ctx, app.configurator, module.VersionMap{name: 1})
}

// StoreKeyForTest is the mounted store key of one module.
func (app *App) StoreKeyForTest(name string) *storetypes.KVStoreKey {
	return app.keys[name]
}
