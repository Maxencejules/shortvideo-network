package upgrade_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"

	gwruntime "github.com/grpc-ecosystem/grpc-gateway/runtime"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// testModule is a minimal module for the SDK's real module manager: a
// consensus version, a layout key 'L'|name in the shared module store, and
// an InitGenesis that writes 'g'|name. It is test support, not a chain
// module.
type testModule struct {
	name    string
	version uint64
	key     *storetypes.KVStoreKey
}

func (testModule) IsOnePerModuleType()                                           {}
func (testModule) IsAppModule()                                                  {}
func (m testModule) Name() string                                                { return m.name }
func (testModule) RegisterLegacyAminoCodec(*codec.LegacyAmino)                   {}
func (testModule) RegisterInterfaces(codectypes.InterfaceRegistry)               {}
func (testModule) RegisterGRPCGatewayRoutes(client.Context, *gwruntime.ServeMux) {}
func (m testModule) ConsensusVersion() uint64                                    { return m.version }
func (testModule) DefaultGenesis(codec.JSONCodec) json.RawMessage                { return json.RawMessage(`{}`) }
func (testModule) ValidateGenesis(codec.JSONCodec, client.TxEncodingConfig, json.RawMessage) error {
	return nil
}

func (m testModule) InitGenesis(ctx sdk.Context, _ codec.JSONCodec, _ json.RawMessage) {
	ctx.KVStore(m.key).Set(append([]byte("g"), m.name...), []byte("init"))
}

func (testModule) ExportGenesis(sdk.Context, codec.JSONCodec) json.RawMessage {
	return json.RawMessage(`{}`)
}

// realManager is the SDK module manager over alpha (version 2, with a
// registered 1 -> 2 migration that writes alpha's layout key 2 and counts
// its runs), beta (2), and gamma (1), and the configurator the migration
// is registered on: the shape of the app's manager and configurator.
func realManager(t *testing.T, key *storetypes.KVStoreKey, runs *int) (*module.Manager, module.Configurator) {
	t.Helper()
	mm := module.NewManager(
		testModule{name: "alpha", version: 2, key: key},
		testModule{name: "beta", version: 2, key: key},
		testModule{name: "gamma", version: 1, key: key},
	)
	mm.SetOrderMigrations("alpha", "beta", "gamma")
	cfg := module.NewConfigurator(codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), baseapp.NewMsgServiceRouter(), baseapp.NewGRPCQueryRouter())
	if err := cfg.RegisterMigration("alpha", 1, func(ctx sdk.Context) error {
		*runs++
		ctx.KVStore(key).Set([]byte("Lalpha"), u64(2))
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := mm.GetVersionMap(); fmt.Sprint(got) != fmt.Sprint(runnerVM) {
		t.Fatalf("manager version map %v, want %v", got, runnerVM)
	}
	return mm, cfg
}

// TestRunMigrationsGapsAreClosedByTheRunner: the SDK's RunMigrations
// returns no error when the stored map lacks a module (it runs that
// module's InitGenesis), names a module the binary lacks (it drops it), or
// holds a version above the binary's for a module that registered a
// migration or whose binary version is 1 (it runs nothing). The runner, with
// the same manager as its Migrate, refuses each of those states before
// the handler runs and writes nothing.
func TestRunMigrationsGapsAreClosedByTheRunner(t *testing.T) {
	gaps := []struct {
		name  string
		from  module.VersionMap
		check func(t *testing.T, e *runnerEnv, out module.VersionMap)
		edit  func(e *runnerEnv)
		want  string
	}{
		{"module missing from the stored map", module.VersionMap{"alpha": 1, "beta": 2},
			func(t *testing.T, e *runnerEnv, out module.VersionMap) {
				if !e.store("mod").Has([]byte("ggamma")) {
					t.Fatal("RunMigrations did not run gamma's InitGenesis")
				}
			},
			func(e *runnerEnv) { e.store("policy").Delete([]byte("mgamma")) },
			"module gamma is in this binary but not in the stored version map"},
		{"stored module the binary lacks", module.VersionMap{"alpha": 1, "beta": 2, "gamma": 1, "gone": 1},
			func(t *testing.T, _ *runnerEnv, out module.VersionMap) {
				if _, kept := out["gone"]; kept {
					t.Fatal("RunMigrations kept gone")
				}
			},
			func(e *runnerEnv) { e.store("policy").Set([]byte("mgone"), u64(1)) },
			"stored module gone is not in this binary"},
		// runModuleMigrations runs the steps from..to-1, so a stored version
		// above the binary's runs nothing for a module that registered a
		// migration (alpha), and anything stored for a binary version of 1
		// (gamma) is a no-op. (A module with version above 1 and no
		// registered migration at all fails with "no migrations found".)
		{"stored version above the binary, module with migrations", module.VersionMap{"alpha": 3, "beta": 2, "gamma": 1},
			func(t *testing.T, _ *runnerEnv, out module.VersionMap) {
				if out["alpha"] != 2 {
					t.Fatalf("RunMigrations returned alpha %d", out["alpha"])
				}
			},
			func(e *runnerEnv) {
				e.store("policy").Set([]byte("malpha"), u64(3))
				e.store("mod").Set([]byte("Lalpha"), u64(3))
			},
			"module alpha stored version 3 is outside 1..2"},
		{"stored version above a version 1 binary module", module.VersionMap{"alpha": 1, "beta": 2, "gamma": 2},
			func(t *testing.T, _ *runnerEnv, out module.VersionMap) {
				if out["gamma"] != 1 {
					t.Fatalf("RunMigrations returned gamma %d", out["gamma"])
				}
			},
			func(e *runnerEnv) { e.store("policy").Set([]byte("mgamma"), u64(2)) },
			"module gamma stored version 2 is outside 1..1"},
	}
	for _, gap := range gaps {
		// The SDK accepts the state.
		e := newRunnerEnv(t)
		runs := 0
		mm, cfg := realManager(t, e.keys["mod"], &runs)
		cc, _ := e.ctx.CacheContext()
		out, err := mm.RunMigrations(cc, cfg, gap.from)
		if err != nil {
			t.Fatalf("%s: RunMigrations: %v (the SDK gap is closed upstream; revisit the runner checks)", gap.name, err)
		}
		gap.check(t, &runnerEnv{t: t, ctx: cc, keys: e.keys}, out)

		// The runner refuses it.
		e = newRunnerEnv(t)
		runs = 0
		mm, cfg = realManager(t, e.keys["mod"], &runs)
		e.deps.Migrate = func(ctx sdk.Context, from module.VersionMap) (module.VersionMap, error) {
			return mm.RunMigrations(ctx, cfg, from)
		}
		gap.edit(e)
		e.failsUnchanged(gap.name, upgrade.ErrPrecondition, gap.want)
		if runs != 0 || e.handlerCalls != 0 {
			t.Fatalf("%s: migration ran %d times, handler %d times", gap.name, runs, e.handlerCalls)
		}
	}
}

// TestRunnerAppliesRealMigration: with the SDK's module manager as Migrate,
// the runner runs alpha's 1 -> 2 migration exactly once, records the
// manager's map, and the next block refuses to run any migration again.
// A failing migration is an ErrHandler failure with nothing written.
func TestRunnerAppliesRealMigration(t *testing.T) {
	e := newRunnerEnv(t)
	runs := 0
	mm, cfg := realManager(t, e.keys["mod"], &runs)
	e.deps.Migrate = func(ctx sdk.Context, from module.VersionMap) (module.VersionMap, error) {
		return mm.RunMigrations(ctx, cfg, from)
	}
	changed, err := e.runner().PreBlock(e.ctx)
	if err != nil || !changed {
		t.Fatalf("PreBlock: %t %v", changed, err)
	}
	if runs != 1 {
		t.Fatalf("migration ran %d times", runs)
	}
	if got := e.versions.VersionMap(e.ctx); fmt.Sprint(got) != fmt.Sprint(mm.GetVersionMap()) {
		t.Fatalf("stored map %v, manager %v", got, mm.GetVersionMap())
	}
	if e.store("mod").Has([]byte("ggamma")) || e.store("mod").Has([]byte("galpha")) {
		t.Fatal("a module was initialized from its default genesis")
	}

	failing := newRunnerEnv(t)
	cfg = module.NewConfigurator(codec.NewProtoCodec(codectypes.NewInterfaceRegistry()), baseapp.NewMsgServiceRouter(), baseapp.NewGRPCQueryRouter())
	if err := cfg.RegisterMigration("alpha", 1, func(ctx sdk.Context) error {
		ctx.KVStore(failing.keys["mod"]).Set([]byte("Lalpha"), u64(2))
		return errors.New("migration bug")
	}); err != nil {
		t.Fatal(err)
	}
	failingMM := module.NewManager(
		testModule{name: "alpha", version: 2, key: failing.keys["mod"]},
		testModule{name: "beta", version: 2, key: failing.keys["mod"]},
		testModule{name: "gamma", version: 1, key: failing.keys["mod"]},
	)
	failing.deps.Migrate = func(ctx sdk.Context, from module.VersionMap) (module.VersionMap, error) {
		return failingMM.RunMigrations(ctx, cfg, from)
	}
	failing.failsUnchanged("failing migration", upgrade.ErrHandler, "migration bug")
}
