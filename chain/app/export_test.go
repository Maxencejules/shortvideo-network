package app

import (
	"fmt"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases/leasestest"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
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

type versionedLeases struct {
	leases.AppModule
	version uint64
}

func (v versionedLeases) ConsensusVersion() uint64 { return v.version }

// RunRegistryMigrationForTest registers step as the migration from the
// current ConsensusVersion v to v+1 of the registry or leases module name on the
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
	case leases.AppModule:
		from = m.ConsensusVersion()
		wrapped = versionedLeases{AppModule: m, version: from + 1}
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

// leaseReadersForTest builds read-only keepers over the app's own stores:
// the authority read surface, x/providers, x/content (without the
// withdrawal hook), and x/leases. Tests use them only to read committed
// state; the keepers the app runs are the ones New wired.
func (app *App) leaseReadersForTest() (leases.Keeper, providers.Keeper, content.Keeper) {
	read := authority.NewKeeper(app.keys[authority.ModuleName])
	prov := providers.NewKeeper(app.keys[ModuleProviders], providers.Deps{Roles: read, Limits: read})
	cont := content.NewKeeper(app.keys[ModuleContent], content.Deps{Roles: read, Params: read, Limits: read})
	lease := leases.NewKeeper(app.keys[ModuleLeases], leases.Deps{
		Roles: read, Limits: read, Params: read, Policy: read, Providers: prov, Content: cont,
	})
	return lease, prov, cont
}

// CheckLeaseInvariantsForTest runs leasestest.CheckInvariants (I1-I7, I10:
// reserved bytes per provider equal the held leases' bytes, every index
// row matches a record) and leasestest.CheckOfferIntegrity (I9) over the
// committed state.
func (app *App) CheckLeaseInvariantsForTest() error {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: app.ChainID()})
	lease, prov, cont := app.leaseReadersForTest()
	if err := leasestest.CheckInvariants(ctx, lease, prov, nil); err != nil {
		return err
	}
	return leasestest.CheckOfferIntegrity(ctx, lease, cont)
}

// DevnetNumericForTest reads the devnet numeric parameter decisionID with
// the x/authority read surface x/leases uses, on the committed state, with
// the given chain id in the context (the rule x/authority applies).
func (app *App) DevnetNumericForTest(chainID, decisionID string) (uint64, bool) {
	ctx := app.NewUncachedContext(false, cmtproto.Header{ChainID: chainID})
	return authority.NewKeeper(app.keys[authority.ModuleName]).DevnetNumeric(ctx, decisionID)
}

// WriteProviderReservedForTest overwrites reserved_bytes of one provider
// record in the working state: it is not committed and is not a
// consensus message. The next FinalizeBlock reads it. Tests use it to
// break invariant I1 so that expiry must halt.
func (app *App) WriteProviderReservedForTest(providerID string, reserved uint64) error {
	store := app.CommitMultiStore().GetKVStore(app.keys[ModuleProviders])
	key := providers.ProviderKey(providerID)
	raw := store.Get(key)
	if raw == nil {
		return fmt.Errorf("no provider record %s", providerID)
	}
	var record providersv1.Provider
	if err := proto.Unmarshal(raw, &record); err != nil {
		return err
	}
	record.ReservedBytes = reserved
	out, err := proto.MarshalOptions{Deterministic: true}.Marshal(&record)
	if err != nil {
		return err
	}
	store.Set(key, out)
	return nil
}
