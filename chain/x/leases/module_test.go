package leases_test

import (
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
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// routers is a real SDK message router and gRPC query router with the
// lease messages registered in an interface registry, wired through a real
// configurator.
type routers struct {
	msg   *baseapp.MsgServiceRouter
	query *baseapp.GRPCQueryRouter
	cfg   module.Configurator
}

func newRouters(t testing.TB, am leases.AppModule) routers {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	registry.RegisterImplementations((*sdk.Msg)(nil),
		&leasesv1.MsgOfferLease{}, &leasesv1.MsgAcceptLease{}, &leasesv1.MsgRenewLease{}, &leasesv1.MsgEndLease{},
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

// TestModule: ConsensusVersion 2; the module has BeginBlock and genesis;
// it has no EndBlock, no ABCI genesis, no pre-blocker, and no source names
// a validator update (x/authority stays the only validator-set writer).
func TestModule(t *testing.T) {
	e := newEnv(t)
	am := leases.NewAppModule(e.k, nil)
	if am.Name() != leases.ModuleName || am.ConsensusVersion() != 2 || leases.ConsensusVersion != 2 {
		t.Fatalf("name %s version %d", am.Name(), am.ConsensusVersion())
	}
	var m any = am
	if _, ok := m.(appmodule.HasBeginBlocker); !ok {
		t.Fatal("module has no BeginBlock")
	}
	for name, has := range map[string]bool{
		"HasABCIEndBlock": implements[module.HasABCIEndBlock](m),
		"HasEndBlocker":   implements[appmodule.HasEndBlocker](m),
		"HasABCIGenesis":  implements[module.HasABCIGenesis](m),
		"HasPreBlocker":   implements[appmodule.HasPreBlocker](m),
	} {
		if has {
			t.Fatalf("module implements %s", name)
		}
	}
	for _, file := range parseSources(t, ".") {
		ast.Inspect(file.ast, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && strings.Contains(id.Name, "ValidatorUpdate") {
				t.Errorf("%s names %s", file.path, id.Name)
			}
			return true
		})
	}
	if mm := module.NewManager(am); mm.GetVersionMap()[leases.ModuleName] != 2 {
		t.Fatal("version map")
	}
}

func implements[I any](v any) bool {
	_, ok := v.(I)
	return ok
}

// TestBeginBlockRunsExpireDue: the module's BeginBlock is ExpireDue on the
// block context; an expiry invariant violation is returned as the
// BeginBlock error (the module manager fails FinalizeBlock with it).
func TestBeginBlockRunsExpireDue(t *testing.T) {
	e := newEnv(t)
	am := leases.NewAppModule(e.k, nil)
	l := e.leaseRecord("l1", "c1", "p1")
	e.seedLease(l)
	if err := am.BeginBlock(e.blockCtx(l.GetDeadlineUnixSeconds() - 1)); err != nil {
		t.Fatal(err)
	}
	if e.storedLease("l1").GetState() != leasesv1.LeaseState_LEASE_STATE_ACTIVE {
		t.Fatal("expired one second early")
	}
	ctx := e.blockCtx(l.GetDeadlineUnixSeconds())
	if err := am.BeginBlock(ctx); err != nil {
		t.Fatal(err)
	}
	if e.storedLease("l1").GetState() != leasesv1.LeaseState_LEASE_STATE_EXPIRED || e.provider("p1").GetReservedBytes() != 0 {
		t.Fatal("BeginBlock did not expire the lease")
	}
	if len(ctx.EventManager().ABCIEvents()) != 1 {
		t.Fatalf("events %v", ctx.EventManager().ABCIEvents())
	}
	// A broken store: BeginBlock returns the error.
	e.seedLease(e.leaseRecord("l2", "c2", "p1"))
	e.editProvider("p1", func(p *providersv1.Provider) { p.ReservedBytes = 0 })
	if err := am.BeginBlock(e.blockCtx(l.GetDeadlineUnixSeconds())); err == nil {
		t.Fatal("BeginBlock hid an invariant violation")
	}
	if err := am.BeginBlock(context.Background()); err == nil {
		t.Fatal("BeginBlock ran without an SDK context")
	}
}

// TestModuleGenesis: the module's genesis methods parse strictly, record
// the InitGenesis call, import, and export; an invalid document panics.
func TestModuleGenesis(t *testing.T) {
	e := genesisEnv(t)
	order := &recorder{}
	am := leases.NewAppModule(e.k, order)
	raw, err := leases.MarshalGenesis(sampleGenesis(e))
	if err != nil {
		t.Fatal(err)
	}
	if err := am.ValidateGenesis(nil, nil, raw); err != nil {
		t.Fatal(err)
	}
	if err := am.ValidateGenesis(nil, nil, []byte(`{"offers":[{"offer_id":"O"}]}`)); err == nil {
		t.Fatal("invalid document validated")
	}
	am.InitGenesis(e.unmetered(), nil, raw)
	if len(order.names) != 1 || order.names[0] != leases.ModuleName {
		t.Fatalf("order %v", order.names)
	}
	exported, err := leases.ParseGenesis(am.ExportGenesis(e.unmetered(), nil))
	if err != nil || len(exported.GetOffers()) != 2 || len(exported.GetLeases()) != 4 {
		t.Fatalf("export %v %v", exported, err)
	}
	defer func() {
		if recover() == nil {
			t.Fatal("InitGenesis over a non-empty store did not panic")
		}
	}()
	am.InitGenesis(e.unmetered(), nil, raw)
}

type recorder struct{ names []string }

func (r *recorder) Record(name string) { r.names = append(r.names, name) }

// TestServicesOnRealRouters: the hand-written descriptors register on the
// SDK routers through RegisterServices; a message routed by type URL runs
// the committed handler; a failure carries the svn code; ABCI query paths
// answer from the store; the Msg service is not a query service.
func TestServicesOnRealRouters(t *testing.T) {
	e := newEnv(t)
	r := newRouters(t, leases.NewAppModule(e.k, nil))
	for _, url := range []string{
		"/svn.leases.v1.MsgOfferLease", "/svn.leases.v1.MsgAcceptLease", "/svn.leases.v1.MsgRenewLease", "/svn.leases.v1.MsgEndLease",
	} {
		if r.msg.HandlerByTypeURL(url) == nil {
			t.Fatalf("no message route for %s", url)
		}
	}
	msg := e.offerMsg("o1", "c1", "p1")
	res, err := r.msg.Handler(msg)(e.fresh(), msg)
	if err != nil {
		t.Fatalf("routed OfferLease: %v", err)
	}
	var offered bool
	for _, ev := range res.GetEvents() {
		if ev.Type == "svn.events.v1.LeaseOffered" {
			parsed, err := msgkit.ParseTyped(abci.Event(ev))
			if err != nil {
				t.Fatal(err)
			}
			offered = proto.Equal(parsed, &eventsv1.LeaseOffered{OfferId: "o1", ProviderId: "p1", ContentId: "c1"})
		}
	}
	if !offered {
		t.Fatalf("routed result events %v", res.GetEvents())
	}
	dup := e.offerMsg("o1", "c2", "p1")
	dup.BusinessId = "off-dup"
	if _, err := r.msg.Handler(dup)(e.fresh(), dup); codeOf(t, err) != msgkit.DuplicateID {
		t.Fatalf("routed duplicate: %v", err)
	}
	e.requireOK(e.accept(e.fresh(), e.acceptMsg("o1", "l1")), "accept")

	route := r.query.Route("/svn.leases.v1.Query/ActiveLeases")
	if route == nil {
		t.Fatal("no ABCI route for Query/ActiveLeases")
	}
	data, _ := proto.Marshal(&leasesv1.QueryActiveLeasesRequest{ContentId: "c1"})
	resp, err := route(e.fresh(), &abci.RequestQuery{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	var active leasesv1.QueryActiveLeasesResponse
	if err := proto.Unmarshal(resp.Value, &active); err != nil || len(active.GetLeases()) != 1 || active.GetLeases()[0].GetLeaseId() != "l1" {
		t.Fatalf("ABCI ActiveLeases %v %v", active.GetLeases(), err)
	}
	data, _ = proto.Marshal(&leasesv1.QueryActiveLeasesRequest{})
	if _, err := route(e.fresh(), &abci.RequestQuery{Data: data}); codeOf(t, err) != msgkit.Malformed {
		t.Fatalf("ABCI query without a filter: %v", err)
	}
	deadlines := r.query.Route("/svn.leases.v1.Query/Deadlines")
	if deadlines == nil {
		t.Fatal("no ABCI route for Query/Deadlines")
	}
	data, _ = proto.Marshal(&leasesv1.QueryDeadlinesRequest{NotAfterUnixSeconds: t0 + 100_000})
	resp, err = deadlines(e.fresh(), &abci.RequestQuery{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	var due leasesv1.QueryDeadlinesResponse
	if err := proto.Unmarshal(resp.Value, &due); err != nil || len(due.GetLeases()) != 1 {
		t.Fatalf("ABCI Deadlines %v %v", due.GetLeases(), err)
	}
	if r.query.Route("/svn.leases.v1.Msg/OfferLease") != nil {
		t.Fatal("Msg service registered on the query router")
	}
}

// TestServiceDescriptorsMatchTheProtocol: the hand-written descriptors list
// exactly the methods of the frozen proto services, in proto order.
func TestServiceDescriptorsMatchTheProtocol(t *testing.T) {
	e := newEnv(t)
	for _, desc := range []*grpc.ServiceDesc{leases.MsgServiceDesc(e.k), leases.QueryServiceDesc(e.k)} {
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
		}
		if string(service.ParentFile().Path()) != desc.Metadata {
			t.Fatalf("%s metadata %v, want %s", desc.ServiceName, desc.Metadata, service.ParentFile().Path())
		}
	}
	server := grpc.NewServer()
	server.RegisterService(leases.QueryServiceDesc(e.k), e.k)
	server.Stop()
}

// TestUnaryHandlerInterceptor: with an interceptor the handler passes the
// decoded request and the full method name; a request of the wrong type
// and a plain context are MALFORMED.
func TestUnaryHandlerInterceptor(t *testing.T) {
	e := newEnv(t)
	e.seedLease(e.leaseRecord("l1", "c1", "p1"))
	desc := leases.QueryServiceDesc(e.k)
	var info *grpc.UnaryServerInfo
	interceptor := func(ctx context.Context, req any, i *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		info = i
		return handler(ctx, req)
	}
	dec := func(in any) error {
		in.(*leasesv1.QueryActiveLeasesRequest).ContentId = "c1"
		return nil
	}
	out, err := desc.Methods[0].Handler(e.k, e.fresh(), dec, interceptor)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.(*leasesv1.QueryActiveLeasesResponse).GetLeases()) != 1 || info.FullMethod != "/svn.leases.v1.Query/ActiveLeases" {
		t.Fatalf("intercepted call %v %v", out, info)
	}
	wrong := func(ctx context.Context, _ any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, &leasesv1.QueryDeadlinesRequest{})
	}
	if _, err := desc.Methods[0].Handler(e.k, e.fresh(), dec, wrong); codeOf(t, err) != msgkit.Malformed {
		t.Fatalf("wrong request type: %v", err)
	}
	if _, err := e.k.ActiveLeases(context.Background(), &leasesv1.QueryActiveLeasesRequest{ContentId: "c1"}); codeOf(t, err) != msgkit.Malformed {
		t.Fatalf("plain context: %v", err)
	}
	if _, err := e.k.OfferLease(context.Background(), e.offerMsg("o1", "c1", "p1")); codeOf(t, err) != msgkit.Malformed {
		t.Fatalf("plain context: %v", err)
	}
}

// TestRunMigrationsAtCurrentVersionIsANoOp: RunMigrations from 2 returns 2
// and leaves the store byte-identical.
func TestRunMigrationsAtCurrentVersionIsANoOp(t *testing.T) {
	e := newEnv(t)
	e.mustLease("l1", "c1", "p1")
	am := leases.NewAppModule(e.k, nil)
	mm := module.NewManager(am)
	r := newRouters(t, am)
	before := e.snapshot(e.unmetered())
	vm, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{leases.ModuleName: leases.ConsensusVersion})
	if err != nil {
		t.Fatal(err)
	}
	if vm[leases.ModuleName] != leases.ConsensusVersion || len(vm) != 1 {
		t.Fatalf("version map after migrations %v", vm)
	}
	if string(before) != string(e.snapshot(e.unmetered())) {
		t.Fatal("RunMigrations changed the store")
	}
}

// TestNoMigrationFromScaffoldLayout: version 1 is the goal-05/06 scaffold
// layout (one key [0x01] holding the genesis JSON, inside this layout's
// offer prefix). No step leaves it: RunMigrations from 1 fails and the
// store is untouched, and InitGenesis refuses a store holding that key
// (TestGenesisValidateImportExport/requires-an-empty-store).
func TestNoMigrationFromScaffoldLayout(t *testing.T) {
	e := newEnv(t)
	am := leases.NewAppModule(e.k, nil)
	mm := module.NewManager(am)
	r := newRouters(t, am)
	before := e.snapshot(e.unmetered())
	if _, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{leases.ModuleName: msgkit.ScaffoldLayoutVersion}); err == nil {
		t.Fatal("RunMigrations from the scaffold layout succeeded")
	}
	if string(before) != string(e.snapshot(e.unmetered())) {
		t.Fatal("a failed migration changed the store")
	}
}

type versionedModule struct {
	leases.AppModule
	version uint64
}

func (v versionedModule) ConsensusVersion() uint64 { return v.version }

// TestMigrationTable: the table covers exactly 2 .. ConsensusVersion-1 (no
// step from the scaffold version 1), and RegisterServices registers each
// entry with the configurator, where the module manager runs it.
func TestMigrationTable(t *testing.T) {
	table := leases.MigrationsForTest()
	first := msgkit.ScaffoldLayoutVersion + 1
	for from := first; from < leases.ConsensusVersion; from++ {
		if table[from] == nil {
			t.Fatalf("no migration from %d", from)
		}
	}
	if uint64(len(table)) != leases.ConsensusVersion-first || table[msgkit.ScaffoldLayoutVersion] != nil {
		t.Fatalf("%d migrations for ConsensusVersion %d", len(table), leases.ConsensusVersion)
	}
	ran := 0
	restore := leases.SetMigrationsForTest(map[uint64]module.MigrationHandler{
		leases.ConsensusVersion: func(sdk.Context) error { ran++; return nil },
	})
	defer restore()
	e := newEnv(t)
	r := newRouters(t, leases.NewAppModule(e.k, nil))
	if err := r.cfg.RegisterMigration(leases.ModuleName, leases.ConsensusVersion, func(sdk.Context) error { return nil }); err == nil {
		t.Fatal("RegisterServices did not register the migration")
	}
	mm := module.NewManager(versionedModule{AppModule: leases.NewAppModule(e.k, nil), version: leases.ConsensusVersion + 1})
	vm, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{leases.ModuleName: leases.ConsensusVersion})
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 || vm[leases.ModuleName] != leases.ConsensusVersion+1 {
		t.Fatalf("step ran %d times, version map %v", ran, vm)
	}
}

// TestNewKeeperRejectsMissingDependencies: wiring errors panic; Evidence
// and Peers may be nil.
func TestNewKeeperRejectsMissingDependencies(t *testing.T) {
	e := newEnv(t)
	full := leases.Deps{Roles: e.auth, Limits: e.auth, Params: e.auth, Policy: e.auth, Providers: e.providers, Content: e.content}
	cases := map[string]func(d *leases.Deps){
		"roles":     func(d *leases.Deps) { d.Roles = nil },
		"limits":    func(d *leases.Deps) { d.Limits = nil },
		"params":    func(d *leases.Deps) { d.Params = nil },
		"policy":    func(d *leases.Deps) { d.Policy = nil },
		"providers": func(d *leases.Deps) { d.Providers = nil },
		"content":   func(d *leases.Deps) { d.Content = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			d := full
			edit(&d)
			defer func() {
				if recover() == nil {
					t.Fatal("no panic")
				}
			}()
			leases.NewKeeper(e.keys[leases.ModuleName], d)
		})
	}
	t.Run("nil-key", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("no panic")
			}
		}()
		leases.NewKeeper(nil, full)
	})
	if k := leases.NewKeeper(e.keys[leases.ModuleName], full); k.StoreKey() != e.keys[leases.ModuleName] {
		t.Fatal("store key")
	}
}

// TestDeterministicSources: no clock, randomness, environment, file,
// network, process, or floating-point use in the module's non-test code
// (the leasestest helpers are test support and are not scanned).
func TestDeterministicSources(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
}

// TestKeeperImports: the module never imports x/providers, x/content,
// x/authority, x/evidence, the scaffold, the app, the devnet, or the node;
// it consumes their read surfaces through its own interfaces. The test
// files of this package obey the same rule (they use fakes).
func TestKeeperImports(t *testing.T) {
	const root = "github.com/Maxencejules/shortvideo-network/chain/"
	forbidden := []string{
		root + "x/authority", root + "app", root + "x/content", root + "x/providers", root + "x/evidence",
		root + "x/policy", root + "x/scaffold", root + "devnet", root + "node", root + "cmd",
	}
	for _, includeTests := range []bool{false, true} {
		files := parseGoFiles(t, ".", includeTests)
		files = append(files, parseGoFiles(t, "leasestest", includeTests)...)
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
}

type source struct {
	path string
	ast  *ast.File
}

// parseSources parses the non-test Go files directly in dir.
func parseSources(t testing.TB, dir string) []source { return parseGoFiles(t, dir, false) }

func parseGoFiles(t testing.TB, dir string, tests bool) []source {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") != tests {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	fset := token.NewFileSet()
	var out []source
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

// leaseProtoMessages are every message of svn/leases/v1.
func leaseProtoMessages(t testing.TB) []protoreflect.MessageDescriptor {
	t.Helper()
	var out []protoreflect.MessageDescriptor
	for _, path := range []string{"svn/leases/v1/state.proto", "svn/leases/v1/tx.proto", "svn/leases/v1/query.proto", "svn/leases/v1/genesis.proto"} {
		fd, err := protoregistry.GlobalFiles.FindFileByPath(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for i := 0; i < fd.Messages().Len(); i++ {
			out = append(out, fd.Messages().Get(i))
		}
	}
	return out
}

// TestNoForbiddenNames (REQ-HOST-003): no field of a lease message, no
// lease event type, and no attribute key a lease flow emits matches the
// bounds.json forbidden-name patterns; no lease message has a region or
// location field (a region string is not a physical fact the chain
// records).
func TestNoForbiddenNames(t *testing.T) {
	patterns, exemptions := msgkittest.ForbiddenNamePatterns(t)
	check := func(where, name string) {
		t.Helper()
		if hits := msgkittest.NameHits(name, patterns, exemptions); len(hits) != 0 {
			t.Errorf("%s %q matches forbidden patterns %v", where, name, hits)
		}
		lower := strings.ToLower(name)
		for _, word := range []string{"region", "location", "geo", "country"} {
			if strings.Contains(lower, word) {
				t.Errorf("%s %q names a %s", where, name, word)
			}
		}
	}
	messages := leaseProtoMessages(t)
	if len(messages) < 10 {
		t.Fatalf("%d lease messages", len(messages))
	}
	for _, md := range messages {
		check("message", string(md.Name()))
		for i := 0; i < md.Fields().Len(); i++ {
			check(string(md.FullName())+" field", string(md.Fields().Get(i).Name()))
		}
	}
	// Events of a full flow: offer, accept, idempotent retry, renew, end,
	// expiry of a lease and an offer, withdrawal void.
	e := newEnv(t)
	var events []abci.Event
	collect := func(r result) { events = append(events, r.events...) }
	collect(e.offer(e.fresh(), e.offerMsg("o1", "c1", "p1")))
	acc := e.acceptMsg("o1", "l1")
	collect(e.accept(e.fresh(), acc))
	collect(e.accept(e.fresh(), acc))
	collect(e.renew(e.fresh(), e.renewMsg("l1", "ren-1")))
	collect(e.end(e.fresh(), e.endMsg("l1", "end-1")))
	e.mustLease("l2", "c2", "p2")
	e.mustOffer(e.offerMsg("o-exp", "c3", "p1"))
	ctx := e.blockCtx(e.storedLease("l2").GetDeadlineUnixSeconds())
	if err := e.expire(ctx); err != nil {
		t.Fatal(err)
	}
	events = append(events, ctx.EventManager().ABCIEvents()...)
	e.mustOffer(withExpiry(e.offerMsg("o-void", "c3", "p2"), t0+5000))
	collect(e.withdraw("c3"))
	types := map[string]bool{}
	for _, ev := range events {
		types[ev.Type] = true
		check("event type", strings.TrimPrefix(ev.Type, "svn.events.v1."))
		for _, attr := range ev.Attributes {
			check("event attribute", attr.Key)
		}
	}
	for _, want := range []string{"svn.events.v1.LeaseOffered", "svn.events.v1.LeaseAccepted", "svn.events.v1.LeaseRenewed",
		"svn.events.v1.LeaseEnded", leases.EventOfferClosed, leases.ModuleName + "_result"} {
		if !types[want] {
			t.Fatalf("flow emitted no %s (types %v)", want, types)
		}
	}
}

// TestLeaseStateHasNoAtRisk (REQ-BOUND-004): ACTIVE is the committed
// obligation only. The lease state enum has no AT_RISK name and no value 6;
// a genesis lease in state 6 is invalid; no lease message carries a
// reachability, retrieval, or publication field, and the Msg service has
// no method for them.
func TestLeaseStateHasNoAtRisk(t *testing.T) {
	enum := leasesv1.LeaseState(0).Descriptor()
	if enum.Values().ByNumber(6) != nil || enum.Values().ByName("LEASE_STATE_AT_RISK") != nil {
		t.Fatal("the lease state enum has AT_RISK")
	}
	for i := 0; i < enum.Values().Len(); i++ {
		name := string(enum.Values().Get(i).Name())
		for _, word := range []string{"RISK", "REACH", "PUBLISH", "RETRIEV", "VERIFIED"} {
			if strings.Contains(name, word) {
				t.Fatalf("lease state %s", name)
			}
		}
	}
	e := newEnv(t)
	g := sampleGenesis(e)
	g.Leases[1].State = 6
	if leases.ValidateGenesis(g) == nil {
		t.Fatal("a lease in state 6 validated")
	}
	for _, md := range leaseProtoMessages(t) {
		for i := 0; i < md.Fields().Len(); i++ {
			name := strings.ToLower(string(md.Fields().Get(i).Name()))
			for _, word := range []string{"risk", "reach", "publish", "retriev", "uptime", "latency", "probe"} {
				if strings.Contains(name, word) {
					t.Fatalf("%s has field %s", md.FullName(), name)
				}
			}
		}
	}
	for _, method := range leases.MsgServiceDesc(e.k).Methods {
		if method.MethodName != "OfferLease" && method.MethodName != "AcceptLease" && method.MethodName != "RenewLease" && method.MethodName != "EndLease" {
			t.Fatalf("unexpected lease message %s", method.MethodName)
		}
	}
}

// TestStateNames: event state strings are the enum names without the
// prefix.
func TestStateNames(t *testing.T) {
	for state, name := range map[leasesv1.LeaseState]string{
		leasesv1.LeaseState_LEASE_STATE_ACTIVE:  "ACTIVE",
		leasesv1.LeaseState_LEASE_STATE_RENEWED: "RENEWED",
		leasesv1.LeaseState_LEASE_STATE_ENDED:   "ENDED",
		leasesv1.LeaseState_LEASE_STATE_EXPIRED: "EXPIRED",
	} {
		if got := leases.StateName(state); got != name {
			t.Fatalf("%s: %q", state, got)
		}
	}
}
