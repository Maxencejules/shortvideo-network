package content

import (
	"context"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
)

// routers builds the SDK message and query routers over an interface
// registry holding the two content messages, and a real configurator.
func routers(t *testing.T) (*baseapp.MsgServiceRouter, *baseapp.GRPCQueryRouter, module.Configurator) {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	registry.RegisterImplementations((*sdk.Msg)(nil), &contentv1.MsgRegisterContent{}, &contentv1.MsgWithdrawContent{})
	msgRouter := baseapp.NewMsgServiceRouter()
	msgRouter.SetInterfaceRegistry(registry)
	queryRouter := baseapp.NewGRPCQueryRouter()
	queryRouter.SetInterfaceRegistry(registry)
	cfg := module.NewConfigurator(codec.NewProtoCodec(registry), msgRouter, queryRouter)
	return msgRouter, queryRouter, cfg
}

// TestServicesThroughSDKRouters registers the module services through
// RegisterServices and drives them through the SDK routers: a message by
// type URL and a query by its ABCI path, including typed errors.
func TestServicesThroughSDKRouters(t *testing.T) {
	f := newFixture(t)
	msgRouter, queryRouter, cfg := routers(t)
	am := NewAppModule(f.keeper, nil)
	am.RegisterServices(cfg)
	if err := cfg.Error(); err != nil {
		t.Fatal(err)
	}

	register := msgRouter.HandlerByTypeURL("/svn.content.v1.MsgRegisterContent")
	withdraw := msgRouter.HandlerByTypeURL("/svn.content.v1.MsgWithdrawContent")
	if register == nil || withdraw == nil {
		t.Fatal("content messages are not routed")
	}
	ctx := f.tx()
	res, err := register(ctx, f.registerMsg("reg-c1", "c1"))
	if err != nil {
		t.Fatal(err)
	}
	var registered bool
	for _, ev := range res.GetEvents() {
		if ev.Type == "svn.events.v1.ContentRegistered" {
			registered = true
		}
	}
	if !registered {
		t.Fatalf("routed register emitted %v", res.GetEvents())
	}
	_, err = register(f.tx(), f.registerMsg("reg-c1-dup", "c1"))
	requireCode(t, err, msgkit.DuplicateID)
	if _, err := withdraw(f.tx(), f.withdrawMsg("wd-c1", "c1")); err != nil {
		t.Fatal(err)
	}

	route := queryRouter.Route("/svn.content.v1.Query/Content")
	if route == nil {
		t.Fatal("no ABCI route for svn.content.v1.Query/Content")
	}
	data, err := proto.Marshal(&contentv1.QueryContentRequest{ContentId: "c1", ContentVersion: 1})
	if err != nil {
		t.Fatal(err)
	}
	answer, err := route(f.ctx, &abci.RequestQuery{Data: data, Path: "/svn.content.v1.Query/Content"})
	if err != nil {
		t.Fatal(err)
	}
	var out contentv1.QueryContentResponse
	if err := proto.Unmarshal(answer.GetValue(), &out); err != nil {
		t.Fatal(err)
	}
	direct, _ := f.keeper.Content(f.ctx, &contentv1.QueryContentRequest{ContentId: "c1", ContentVersion: 1})
	if !proto.Equal(&out, direct) || out.GetTombstone() == nil {
		t.Fatalf("ABCI answer %v, keeper answer %v", &out, direct)
	}
	data, _ = proto.Marshal(&contentv1.QueryContentRequest{ContentId: "c9", ContentVersion: 1})
	_, err = route(f.ctx, &abci.RequestQuery{Data: data})
	requireCode(t, err, msgkit.NotFound)
}

// TestHandlersRejectWrongRequestType: a handler given another message type
// by an interceptor fails closed.
func TestHandlersRejectWrongRequestType(t *testing.T) {
	f := newFixture(t)
	desc := MsgServiceDesc(f.keeper)
	interceptor := func(ctx context.Context, _ any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		return h(ctx, &contentv1.MsgWithdrawContent{})
	}
	_, err := desc.Methods[0].Handler(nil, f.tx(), func(any) error { return nil }, interceptor)
	requireCode(t, err, msgkit.Malformed)
	_, err = f.keeper.RegisterContent(context.Background(), f.registerMsg("b", "c1"))
	requireCode(t, err, msgkit.Malformed)
}

// TestRunMigrationsAtCurrentVersionIsANoOp: the module manager runs
// migrations from ConsensusVersion to ConsensusVersion without touching
// the store, and the version map reports ConsensusVersion. The SDK returns
// before it looks for a step in that case.
func TestRunMigrationsAtCurrentVersionIsANoOp(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	_, _, cfg := routers(t)
	mm := module.NewManager(NewAppModule(f.keeper, nil))
	if err := mm.RegisterServices(cfg); err != nil {
		t.Fatal(err)
	}
	if got := mm.GetVersionMap()[ModuleName]; got != ConsensusVersion || ConsensusVersion != 2 {
		t.Fatalf("version map %d, ConsensusVersion %d, want 2", got, ConsensusVersion)
	}
	before := f.snapshot()
	vm, err := mm.RunMigrations(f.ctx, cfg, module.VersionMap{ModuleName: ConsensusVersion})
	if err != nil {
		t.Fatal(err)
	}
	if vm[ModuleName] != ConsensusVersion || len(vm) != 1 {
		t.Fatalf("migrated version map %v", vm)
	}
	requireSame(t, before, f.snapshot(), "no-op migration")
}

// TestNoMigrationFromScaffoldLayout: version 1 is the goal-05/06 scaffold
// layout (one key [0x01] holding the genesis JSON). No step leaves it, so
// RunMigrations from version 1 fails instead of treating that store as
// the registry layout, and the store is not touched.
func TestNoMigrationFromScaffoldLayout(t *testing.T) {
	f := newFixture(t)
	f.mustRegister("reg-c1", "c1")
	_, _, cfg := routers(t)
	mm := module.NewManager(NewAppModule(f.keeper, nil))
	if err := mm.RegisterServices(cfg); err != nil {
		t.Fatal(err)
	}
	before := f.snapshot()
	if _, err := mm.RunMigrations(f.ctx, cfg, module.VersionMap{ModuleName: msgkit.ScaffoldLayoutVersion}); err == nil {
		t.Fatal("RunMigrations from the scaffold layout succeeded")
	}
	requireSame(t, before, f.snapshot(), "migration from the scaffold layout")
}

// TestMigrationStepsCoverConsensusVersion: one step per registry version
// below ConsensusVersion (from 2) and none other; no step from the
// scaffold version 1. At version 2 the table is empty.
func TestMigrationStepsCoverConsensusVersion(t *testing.T) {
	f := newFixture(t)
	steps := NewAppModule(f.keeper, nil).migrationSteps()
	first := msgkit.ScaffoldLayoutVersion + 1
	if uint64(len(steps)) != ConsensusVersion-first {
		t.Fatalf("%d migration steps for consensus version %d", len(steps), ConsensusVersion)
	}
	for from := first; from < ConsensusVersion; from++ {
		if steps[from] == nil {
			t.Fatalf("no migration from version %d", from)
		}
	}
	if steps[msgkit.ScaffoldLayoutVersion] != nil {
		t.Fatal("a step from the scaffold layout")
	}
}

// TestRegisterMigrationsPath drives registerMigrations with synthetic steps:
// they are registered in order and run by the manager; a nil step and a
// duplicate registration are errors.
func TestRegisterMigrationsPath(t *testing.T) {
	f := newFixture(t)
	_, _, cfg := routers(t)
	var ran []uint64
	steps := map[uint64]module.MigrationHandler{
		ConsensusVersion + 1: func(sdk.Context) error { ran = append(ran, ConsensusVersion+1); return nil },
		ConsensusVersion:     func(sdk.Context) error { ran = append(ran, ConsensusVersion); return nil },
	}
	if err := registerMigrations(cfg, steps); err != nil {
		t.Fatal(err)
	}
	mm := module.NewManager(versionedModule{AppModule: NewAppModule(f.keeper, nil), version: ConsensusVersion + 2})
	if _, err := mm.RunMigrations(f.ctx, cfg, module.VersionMap{ModuleName: ConsensusVersion}); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 2 || ran[0] != ConsensusVersion || ran[1] != ConsensusVersion+1 {
		t.Fatalf("ran %v, want [%d %d]", ran, ConsensusVersion, ConsensusVersion+1)
	}
	if err := registerMigrations(cfg, steps); err == nil {
		t.Fatal("duplicate migration registered")
	}
	_, _, fresh := routers(t)
	if err := registerMigrations(fresh, map[uint64]module.MigrationHandler{1: nil}); err == nil {
		t.Fatal("nil migration registered")
	}
}

type versionedModule struct {
	AppModule
	version uint64
}

func (v versionedModule) ConsensusVersion() uint64 { return v.version }

// TestNoValidatorUpdates: the module has no begin or end blocker and no
// ABCI genesis returning validator updates; no source file names
// ValidatorUpdate. x/authority stays the only validator-set writer.
func TestNoValidatorUpdates(t *testing.T) {
	var am any = AppModule{}
	if _, ok := am.(module.HasABCIEndBlock); ok {
		t.Fatal("content implements HasABCIEndBlock")
	}
	if _, ok := am.(module.HasABCIGenesis); ok {
		t.Fatal("content implements HasABCIGenesis")
	}
	if _, ok := am.(appmodule.HasEndBlocker); ok {
		t.Fatal("content implements HasEndBlocker")
	}
	if _, ok := am.(appmodule.HasBeginBlocker); ok {
		t.Fatal("content implements HasBeginBlocker")
	}
	for _, file := range sourceFiles(t) {
		raw, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(raw), "ValidatorUpdate") {
			t.Fatalf("%s mentions ValidatorUpdate", file)
		}
	}
}

// TestDeterministicSources scans the non-test sources for clock,
// randomness, environment, file, process, network, and float use.
func TestDeterministicSources(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
}

// TestModuleImportsStayNarrow: content never imports x/authority, the app,
// another registry module, or a network or file package. It meets
// authority through the Roles, DevnetParams, and Limits interfaces.
func TestModuleImportsStayNarrow(t *testing.T) {
	forbidden := []string{
		"github.com/Maxencejules/shortvideo-network/chain/x/authority",
		"github.com/Maxencejules/shortvideo-network/chain/x/providers",
		"github.com/Maxencejules/shortvideo-network/chain/app",
		"github.com/Maxencejules/shortvideo-network/chain/devnet",
		"net/http",
	}
	fset := token.NewFileSet()
	for _, file := range sourceFiles(t) {
		parsed, err := parser.ParseFile(fset, file, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, spec := range parsed.Imports {
			path, _ := strconv.Unquote(spec.Path.Value)
			for _, bad := range forbidden {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Fatalf("%s imports %s", file, path)
				}
			}
		}
	}
}

func sourceFiles(t *testing.T) []string {
	t.Helper()
	matches, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, m := range matches {
		if !strings.HasSuffix(m, "_test.go") {
			out = append(out, m)
		}
	}
	if len(out) == 0 {
		t.Fatal("no source files")
	}
	return out
}

func TestModuleIdentity(t *testing.T) {
	am := NewAppModule(newFixture(t).keeper, nil)
	if am.Name() != "content" || am.ConsensusVersion() != 2 {
		t.Fatalf("name %q version %d", am.Name(), am.ConsensusVersion())
	}
}
