package providers

import (
	"bytes"
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"google.golang.org/protobuf/reflect/protoregistry"

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// routers is a real SDK message router and gRPC query router with the
// provider messages registered in an interface registry, wired through a
// real configurator.
type routers struct {
	msg   *baseapp.MsgServiceRouter
	query *baseapp.GRPCQueryRouter
	cfg   module.Configurator
}

func newRouters(t testing.TB, am AppModule) routers {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&providersv1.MsgRegisterProvider{},
		&providersv1.MsgUpdateProvider{},
		&providersv1.MsgRotateProviderKey{},
		&providersv1.MsgDrainProvider{},
	)
	msgRouter := baseapp.NewMsgServiceRouter()
	msgRouter.SetInterfaceRegistry(registry)
	queryRouter := baseapp.NewGRPCQueryRouter()
	queryRouter.SetInterfaceRegistry(registry)
	cfg := module.NewConfigurator(codec.NewProtoCodec(registry), msgRouter, queryRouter)
	am.RegisterServices(cfg)
	if err := cfg.Error(); err != nil {
		t.Fatalf("register services: %v", err)
	}
	return routers{msg: msgRouter, query: queryRouter, cfg: cfg}
}

// TestServicesOnRealRouters: the hand-written descriptors register on the
// SDK routers through RegisterServices; a message routed by type URL runs
// the committed handler and returns the typed event; a failure carries the
// svn code; ABCI query paths answer from the store.
func TestServicesOnRealRouters(t *testing.T) {
	e := newEnv(t)
	r := newRouters(t, NewAppModule(e.k, nil))

	for _, url := range []string{
		"/svn.providers.v1.MsgRegisterProvider", "/svn.providers.v1.MsgUpdateProvider",
		"/svn.providers.v1.MsgRotateProviderKey", "/svn.providers.v1.MsgDrainProvider",
	} {
		if r.msg.HandlerByTypeURL(url) == nil {
			t.Fatalf("no message route for %s", url)
		}
	}
	msg := regMsg("p1")
	handler := r.msg.Handler(msg)
	res, err := handler(e.fresh(), msg)
	if err != nil {
		t.Fatalf("routed RegisterProvider: %v", err)
	}
	var registered bool
	for _, ev := range res.GetEvents() {
		if ev.Type == "svn.events.v1.ProviderRegistered" {
			parsed, err := msgkit.ParseTyped(abci.Event(ev))
			if err != nil {
				t.Fatal(err)
			}
			registered = proto.Equal(parsed, &eventsv1.ProviderRegistered{ProviderId: "p1", State: "APPLIED"})
		}
	}
	if !registered {
		t.Fatalf("routed result events %v", res.GetEvents())
	}
	dup := regMsg("p1")
	dup.BusinessId = "reg-dup"
	if _, err := handler(e.fresh(), dup); codeOf(t, err) != msgkit.DuplicateID {
		t.Fatalf("routed duplicate: %v", err)
	}

	route := r.query.Route("/svn.providers.v1.Query/Provider")
	if route == nil {
		t.Fatal("no ABCI route for Query/Provider")
	}
	data, err := proto.Marshal(&providersv1.QueryProviderRequest{ProviderId: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := route(e.fresh(), &abci.RequestQuery{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	var out providersv1.QueryProviderResponse
	if err := proto.Unmarshal(resp.Value, &out); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(out.GetProvider(), e.record("p1")) {
		t.Fatalf("ABCI query %v", out.GetProvider())
	}
	data, _ = proto.Marshal(&providersv1.QueryProviderRequest{ProviderId: "p2"})
	if _, err := route(e.fresh(), &abci.RequestQuery{Data: data}); codeOf(t, err) != msgkit.NotFound {
		t.Fatalf("ABCI query of an absent provider: %v", err)
	}
	list := r.query.Route("/svn.providers.v1.Query/Providers")
	if list == nil {
		t.Fatal("no ABCI route for Query/Providers")
	}
	resp, err = list(e.fresh(), &abci.RequestQuery{})
	if err != nil {
		t.Fatal(err)
	}
	var page providersv1.QueryProvidersResponse
	if err := proto.Unmarshal(resp.Value, &page); err != nil || len(page.GetProviders()) != 1 {
		t.Fatalf("ABCI Providers: %v %v", page.GetProviders(), err)
	}
	// The Msg service must not be served as a query.
	if r.query.Route("/svn.providers.v1.Msg/RegisterProvider") != nil {
		t.Fatal("Msg service registered on the query router")
	}
}

// TestServiceDescriptorsMatchTheProtocol: the hand-written descriptors list
// exactly the methods of the frozen proto services, in proto order. There
// is no admit, disable, or force-withdraw method (DEC-009).
func TestServiceDescriptorsMatchTheProtocol(t *testing.T) {
	e := newEnv(t)
	for _, desc := range []*grpc.ServiceDesc{MsgServiceDesc(e.k), QueryServiceDesc(e.k)} {
		found, err := protoregistry.GlobalFiles.FindDescriptorByName(protoreflect.FullName(desc.ServiceName))
		if err != nil {
			t.Fatalf("%s: %v", desc.ServiceName, err)
		}
		service := found.(protoreflect.ServiceDescriptor)
		if service.Methods().Len() != len(desc.Methods) {
			t.Fatalf("%s: %d proto methods, %d handlers", desc.ServiceName, service.Methods().Len(), len(desc.Methods))
		}
		for i, method := range desc.Methods {
			if string(service.Methods().Get(i).Name()) != method.MethodName {
				t.Fatalf("%s method %d: %s, want %s", desc.ServiceName, i, method.MethodName, service.Methods().Get(i).Name())
			}
			if strings.Contains(method.MethodName, "Admit") || strings.Contains(method.MethodName, "Disable") {
				t.Fatalf("%s has a single-signature council method %s", desc.ServiceName, method.MethodName)
			}
		}
		if string(service.ParentFile().Path()) != desc.Metadata {
			t.Fatalf("%s metadata %v, want %s", desc.ServiceName, desc.Metadata, service.ParentFile().Path())
		}
	}
	// The descriptors also register on a plain gRPC server (the node
	// gRPC server checks HandlerType).
	server := grpc.NewServer()
	server.RegisterService(QueryServiceDesc(e.k), e.k)
	server.Stop()
}

// TestUnaryHandlerInterceptor: with an interceptor (the node gRPC server
// and the message router pass one) the handler passes the decoded request
// and the full method name, and the interceptor's request reaches the
// keeper; a request of the wrong type is MALFORMED.
func TestUnaryHandlerInterceptor(t *testing.T) {
	e := newEnv(t)
	e.registered("p1")
	desc := QueryServiceDesc(e.k)
	var info *grpc.UnaryServerInfo
	interceptor := func(ctx context.Context, req any, i *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		info = i
		return handler(ctx, req)
	}
	dec := func(in any) error {
		in.(*providersv1.QueryProviderRequest).ProviderId = "p1"
		return nil
	}
	out, err := desc.Methods[0].Handler(e.k, e.fresh(), dec, interceptor)
	if err != nil {
		t.Fatal(err)
	}
	if out.(*providersv1.QueryProviderResponse).GetProvider().GetProviderId() != "p1" || info.FullMethod != "/svn.providers.v1.Query/Provider" {
		t.Fatalf("intercepted call %v %v", out, info)
	}
	wrong := func(ctx context.Context, _ any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, &providersv1.QueryProvidersRequest{})
	}
	if _, err := desc.Methods[0].Handler(e.k, e.fresh(), dec, wrong); codeOf(t, err) != msgkit.Malformed {
		t.Fatalf("wrong request type: %v", err)
	}
}

// TestRunMigrationsAtCurrentVersionIsANoOp: the module registers through a
// real configurator, the version map holds ConsensusVersion 2, and
// RunMigrations from 2 returns 2 and leaves the store byte-identical (the
// SDK returns before it looks for a step). Real migrations (and the auth
// 5 -> 7 follow-up) belong to goal 10.
func TestRunMigrationsAtCurrentVersionIsANoOp(t *testing.T) {
	e := newEnv(t)
	e.admitted("p1")
	am := NewAppModule(e.k, nil)
	mm := module.NewManager(am)
	r := newRouters(t, am)
	if got := mm.GetVersionMap()[ModuleName]; got != ConsensusVersion || ConsensusVersion != 2 {
		t.Fatalf("version map %d, ConsensusVersion %d, want 2", got, ConsensusVersion)
	}
	before := e.snapshot()
	vm, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{ModuleName: ConsensusVersion})
	if err != nil {
		t.Fatal(err)
	}
	if vm[ModuleName] != ConsensusVersion || len(vm) != 1 {
		t.Fatalf("version map after migrations %v", vm)
	}
	e.requireSameSnapshot(before, "RunMigrations")
}

// TestNoMigrationFromScaffoldLayout: version 1 is the goal-05/06 scaffold
// layout (one key [0x01] holding the genesis JSON). No step leaves it, so
// RunMigrations from version 1 fails instead of treating that store as the
// registry layout, and the store is not touched.
func TestNoMigrationFromScaffoldLayout(t *testing.T) {
	e := newEnv(t)
	e.admitted("p1")
	am := NewAppModule(e.k, nil)
	mm := module.NewManager(am)
	r := newRouters(t, am)
	before := e.snapshot()
	if _, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{ModuleName: msgkit.ScaffoldLayoutVersion}); err == nil {
		t.Fatal("RunMigrations from the scaffold layout succeeded")
	}
	e.requireSameSnapshot(before, "migration from the scaffold layout")
}

// TestScaffoldStoreIsNotAProvider: what the scaffold slot wrote, the
// genesis JSON at key [0x01], sits inside the record prefix 0x01. InitGenesis
// refuses such a store, so the registry layout is never written on top of
// it; the app refuses to load one (TestAppRefusesScaffoldLayout in
// chain/app).
func TestScaffoldStoreIsNotAProvider(t *testing.T) {
	e := newEnv(t)
	e.store(ModuleName).Set([]byte{ProviderKeyPrefix}, []byte(`{"providers":[]}`))
	before := e.snapshot()
	if err := e.k.InitGenesis(e.unmetered(), &providersv1.GenesisState{}); err == nil {
		t.Fatal("InitGenesis wrote the registry layout over the scaffold key")
	}
	e.requireSameSnapshot(before, "InitGenesis over the scaffold key")
}

// versionedModule reports a test-only consensus version, so the manager
// has a step to run.
type versionedModule struct {
	AppModule
	version uint64
}

func (v versionedModule) ConsensusVersion() uint64 { return v.version }

// TestMigrationTable: the table covers exactly 2 .. ConsensusVersion-1 (no
// step from the scaffold version 1), and registerMigrations registers each
// entry with the configurator, where the module manager runs it.
func TestMigrationTable(t *testing.T) {
	first := msgkit.ScaffoldLayoutVersion + 1
	for from := first; from < ConsensusVersion; from++ {
		if migrations[from] == nil {
			t.Fatalf("no migration from %d", from)
		}
	}
	if uint64(len(migrations)) != ConsensusVersion-first || migrations[msgkit.ScaffoldLayoutVersion] != nil {
		t.Fatalf("%d migrations for ConsensusVersion %d", len(migrations), ConsensusVersion)
	}
	saved := migrations
	defer func() { migrations = saved }()
	ran := 0
	migrations = map[uint64]module.MigrationHandler{ConsensusVersion: func(sdk.Context) error { ran++; return nil }}
	e := newEnv(t)
	r := newRouters(t, NewAppModule(e.k, nil))
	// newRouters ran registerMigrations with the temporary table, so a
	// second registration of the same version is a duplicate.
	if err := r.cfg.RegisterMigration(ModuleName, ConsensusVersion, func(sdk.Context) error { return nil }); err == nil {
		t.Fatalf("migration from %d was not registered", ConsensusVersion)
	}
	mm := module.NewManager(versionedModule{AppModule: NewAppModule(e.k, nil), version: ConsensusVersion + 1})
	vm, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{ModuleName: ConsensusVersion})
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 || vm[ModuleName] != ConsensusVersion+1 {
		t.Fatalf("step ran %d times, version map %v", ran, vm)
	}
}

// TestNoValidatorUpdates: x/authority stays the only validator-set writer.
// The module has no BeginBlock or EndBlock and no ABCI genesis, and no
// non-test source names a validator update.
func TestNoValidatorUpdates(t *testing.T) {
	var am any = NewAppModule(newEnv(t).k, nil)
	if _, ok := am.(module.HasABCIEndBlock); ok {
		t.Fatal("module implements HasABCIEndBlock")
	}
	if _, ok := am.(module.HasABCIGenesis); ok {
		t.Fatal("module implements HasABCIGenesis")
	}
	if _, ok := am.(appmodule.HasEndBlocker); ok {
		t.Fatal("module implements HasEndBlocker")
	}
	if _, ok := am.(appmodule.HasBeginBlocker); ok {
		t.Fatal("module implements HasBeginBlocker")
	}
	if _, ok := am.(appmodule.HasPreBlocker); ok {
		t.Fatal("module implements HasPreBlocker")
	}
	for _, file := range parseSources(t, ".") {
		ast.Inspect(file.ast, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && strings.Contains(id.Name, "ValidatorUpdate") {
				t.Errorf("%s names %s", file.path, id.Name)
			}
			return true
		})
	}
}

// TestDeterministicSources: no clock, randomness, environment, file,
// network, process, or floating-point use in the module's non-test code.
func TestDeterministicSources(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
}

// TestKeeperImports: the module never imports x/authority, the app, the
// other registry modules, or the off-chain capability tooling.
func TestKeeperImports(t *testing.T) {
	const root = "github.com/Maxencejules/shortvideo-network/chain/"
	forbidden := []string{
		root + "x/authority", root + "app", root + "x/content", root + "x/leases", root + "x/evidence",
		root + "x/scaffold", root + "devnet", root + "node", root + "x/providers/capability",
	}
	files := parseSources(t, ".")
	if len(files) == 0 {
		t.Fatal("no sources")
	}
	for _, file := range files {
		for _, spec := range file.ast.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			for _, bad := range forbidden {
				if path == bad || strings.HasPrefix(path, bad+"/") {
					t.Errorf("%s imports %s", file.path, path)
				}
			}
		}
	}
}

type source struct {
	path string
	ast  *ast.File
}

// parseSources parses the non-test Go files directly in dir.
func parseSources(t testing.TB, dir string) []source {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".go") && !strings.HasSuffix(entry.Name(), "_test.go") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	out := make([]source, 0, len(names))
	for _, name := range names {
		path := filepath.Join(dir, name)
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, source{path: path, ast: file})
	}
	return out
}

// TestNewKeeperRejectsMissingDependencies: wiring errors panic.
func TestNewKeeperRejectsMissingDependencies(t *testing.T) {
	e := newEnv(t)
	cases := map[string]func(){
		"nil-key":    func() { NewKeeper(nil, Deps{Roles: e.roles, Limits: e.limits}) },
		"nil-roles":  func() { NewKeeper(e.keys[ModuleName], Deps{Limits: e.limits}) },
		"nil-limits": func() { NewKeeper(e.keys[ModuleName], Deps{Roles: e.roles}) },
	}
	for name, fn := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			fn()
		})
	}
	// A nil Peers set is allowed: the gate then sees only this module.
	k := NewKeeper(e.keys[ModuleName], Deps{Roles: e.roles, Limits: e.limits})
	if k.StoreKey() != e.keys[ModuleName] {
		t.Fatal("store key")
	}
}

// TestStateNames: event state strings are the spec's short names.
func TestStateNames(t *testing.T) {
	want := map[providersv1.ProviderState]string{
		providersv1.ProviderState_PROVIDER_STATE_APPLIED:  "APPLIED",
		providersv1.ProviderState_PROVIDER_STATE_ADMITTED: "ADMITTED",
		providersv1.ProviderState_PROVIDER_STATE_DRAINING: "DRAINING",
		providersv1.ProviderState_PROVIDER_STATE_DISABLED: "DISABLED",
	}
	for state, name := range want {
		if got := StateName(state); got != name {
			t.Fatalf("%s: %q", state, got)
		}
	}
}

// TestSignBytes pins the inner signature preimages.
func TestSignBytes(t *testing.T) {
	rotate, err := RotateSignBytes("p1", "key-p1b", []byte{0xab, 0x01}, 7)
	if err != nil {
		t.Fatal(err)
	}
	wantRotate := "SVN-PROVIDER-ROTATE-1\x00" + `{"expectedGeneration":"7","newSigningKeyId":"key-p1b","newSigningPubkey":"ab01","providerId":"p1"}`
	if !bytes.Equal(rotate, []byte(wantRotate)) {
		t.Fatalf("rotate %q", rotate)
	}
	drain, err := DrainSignBytes("p1", 18446744073709551615)
	if err != nil {
		t.Fatal(err)
	}
	wantDrain := "SVN-PROVIDER-DRAIN-1\x00" + `{"keyGeneration":"18446744073709551615","providerId":"p1"}`
	if !bytes.Equal(drain, []byte(wantDrain)) {
		t.Fatalf("drain %q", drain)
	}
	if _, err := RotateSignBytes("p1", "k", bytes.Repeat([]byte{1}, 40<<10), 1); err == nil {
		t.Fatal("a payload above the SVN-JCS-1 size cap has a canonical form")
	}
}
