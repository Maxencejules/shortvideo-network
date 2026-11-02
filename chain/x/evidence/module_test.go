package evidence_test

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
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// routers is a real SDK message router and gRPC query router with the
// evidence message registered in an interface registry, wired through a
// real configurator.
type routers struct {
	msg   *baseapp.MsgServiceRouter
	query *baseapp.GRPCQueryRouter
	cfg   module.Configurator
}

func newRouters(t testing.TB, am evidence.AppModule) routers {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
	registry.RegisterImplementations((*sdk.Msg)(nil), &evidencev1.MsgSubmitAuditBatch{})
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

func implements[I any](v any) bool {
	_, ok := v.(I)
	return ok
}

// TestModule: ConsensusVersion 2; the module has genesis and services; it
// has no BeginBlock and no EndBlock (nothing is swept, D-4), no ABCI
// genesis, no pre-blocker, and no source names a validator update.
func TestModule(t *testing.T) {
	e := newEnv(t)
	am := evidence.NewAppModule(e.k, nil)
	if am.Name() != evidence.ModuleName || am.ConsensusVersion() != 2 || evidence.ConsensusVersion != 2 {
		t.Fatalf("name %s version %d", am.Name(), am.ConsensusVersion())
	}
	var m any = am
	for name, has := range map[string]bool{
		"HasBeginBlocker":   implements[appmodule.HasBeginBlocker](m),
		"HasEndBlocker":     implements[appmodule.HasEndBlocker](m),
		"HasABCIEndBlock":   implements[module.HasABCIEndBlock](m),
		"HasABCIGenesis":    implements[module.HasABCIGenesis](m),
		"HasPreBlocker":     implements[appmodule.HasPreBlocker](m),
		"HasPrecommit":      implements[appmodule.HasPrecommit](m),
		"HasPrepareCheckSt": implements[appmodule.HasPrepareCheckState](m),
	} {
		if has {
			t.Fatalf("module implements %s", name)
		}
	}
	if !implements[module.HasGenesis](m) || !implements[module.HasServices](m) {
		t.Fatal("module lacks genesis or services")
	}
	for _, file := range parseGoFiles(t, ".", false) {
		ast.Inspect(file.ast, func(n ast.Node) bool {
			if id, ok := n.(*ast.Ident); ok && (strings.Contains(id.Name, "ValidatorUpdate") || strings.Contains(id.Name, "BeginBlock")) {
				t.Errorf("%s names %s", file.path, id.Name)
			}
			return true
		})
	}
	if mm := module.NewManager(am); mm.GetVersionMap()[evidence.ModuleName] != 2 {
		t.Fatal("version map")
	}
}

// TestServicesOnRealRouters: the hand-written descriptors register on the
// SDK routers through RegisterServices; a message routed by type URL runs
// the committed handler; a failure carries the svn code; the ABCI query
// path answers from the store; the Msg service is not a query service.
func TestServicesOnRealRouters(t *testing.T) {
	e := newEnv(t)
	r := newRouters(t, evidence.NewAppModule(e.k, nil))
	if r.msg.HandlerByTypeURL("/svn.evidence.v1.MsgSubmitAuditBatch") == nil {
		t.Fatal("no message route")
	}
	msg := signed(auditor1, "1", e.item(auditor1, "e1", "l1"))
	res, err := r.msg.Handler(msg)(e.fresh(), msg)
	if err != nil {
		t.Fatalf("routed SubmitAuditBatch: %v", err)
	}
	var accepted bool
	for _, ev := range res.GetEvents() {
		if ev.Type == "svn.events.v1.EvidenceAccepted" {
			parsed, err := msgkit.ParseTyped(abci.Event(ev))
			if err != nil {
				t.Fatal(err)
			}
			accepted = proto.Equal(parsed, &eventsv1.EvidenceAccepted{
				EvidenceId: prefix(auditor1) + "e1", BatchId: msg.GetBatchId(), Kind: "AUDITOR_ATTESTATION", Attestation: true,
			})
		}
	}
	if !accepted {
		t.Fatalf("routed result events %v", res.GetEvents())
	}
	dup := signed(auditor1, "2", e.item(auditor1, "e1", "l2"))
	if _, err := r.msg.Handler(dup)(e.fresh(), dup); codeOf(t, err) != msgkit.DuplicateID {
		t.Fatalf("routed duplicate: %v", err)
	}
	route := r.query.Route("/svn.evidence.v1.Query/Evidence")
	if route == nil {
		t.Fatal("no ABCI route for Query/Evidence")
	}
	data, _ := proto.Marshal(&evidencev1.QueryEvidenceRequest{LeaseId: "l1"})
	resp, err := route(e.fresh(), &abci.RequestQuery{Data: data})
	if err != nil {
		t.Fatal(err)
	}
	var out evidencev1.QueryEvidenceResponse
	if err := proto.Unmarshal(resp.Value, &out); err != nil || len(out.GetRecords()) != 1 {
		t.Fatalf("ABCI Evidence %v %v", out.GetRecords(), err)
	}
	data, _ = proto.Marshal(&evidencev1.QueryEvidenceRequest{})
	if _, err := route(e.fresh(), &abci.RequestQuery{Data: data}); codeOf(t, err) != msgkit.Malformed {
		t.Fatalf("ABCI query without a filter: %v", err)
	}
	if r.query.Route("/svn.evidence.v1.Msg/SubmitAuditBatch") != nil {
		t.Fatal("Msg service registered on the query router")
	}
}

// TestServiceDescriptorsMatchTheProtocol: the hand-written descriptors list
// exactly the methods of the frozen proto services, in proto order.
func TestServiceDescriptorsMatchTheProtocol(t *testing.T) {
	e := newEnv(t)
	for _, desc := range []*grpc.ServiceDesc{evidence.MsgServiceDesc(e.k), evidence.QueryServiceDesc(e.k)} {
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
				t.Fatalf("%s method %d: %s", desc.ServiceName, i, method.MethodName)
			}
		}
		if string(service.ParentFile().Path()) != desc.Metadata {
			t.Fatalf("%s metadata %v", desc.ServiceName, desc.Metadata)
		}
	}
	server := grpc.NewServer()
	server.RegisterService(evidence.QueryServiceDesc(e.k), e.k)
	server.Stop()
}

// TestUnaryHandlerInterceptor: with an interceptor the handler passes the
// decoded request and the full method name; a request of the wrong type
// and a plain context are MALFORMED.
func TestUnaryHandlerInterceptor(t *testing.T) {
	e := newEnv(t)
	e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1")))
	desc := evidence.QueryServiceDesc(e.k)
	var info *grpc.UnaryServerInfo
	interceptor := func(ctx context.Context, req any, i *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		info = i
		return handler(ctx, req)
	}
	dec := func(in any) error {
		in.(*evidencev1.QueryEvidenceRequest).LeaseId = "l1"
		return nil
	}
	out, err := desc.Methods[0].Handler(e.k, e.fresh(), dec, interceptor)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.(*evidencev1.QueryEvidenceResponse).GetRecords()) != 1 || info.FullMethod != "/svn.evidence.v1.Query/Evidence" {
		t.Fatalf("intercepted call %v %v", out, info)
	}
	wrong := func(ctx context.Context, _ any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		return handler(ctx, &evidencev1.MsgSubmitAuditBatch{})
	}
	if _, err := desc.Methods[0].Handler(e.k, e.fresh(), dec, wrong); codeOf(t, err) != msgkit.Malformed {
		t.Fatalf("wrong request type: %v", err)
	}
	if _, err := e.k.Evidence(context.Background(), &evidencev1.QueryEvidenceRequest{LeaseId: "l1"}); codeOf(t, err) != msgkit.Malformed {
		t.Fatalf("plain context: %v", err)
	}
	if _, err := e.k.SubmitAuditBatch(context.Background(), &evidencev1.MsgSubmitAuditBatch{}); codeOf(t, err) != msgkit.Malformed {
		t.Fatalf("plain context: %v", err)
	}
}

// TestRunMigrationsAtCurrentVersionIsANoOp: RunMigrations from 2 returns 2
// and leaves the store byte-identical.
func TestRunMigrationsAtCurrentVersionIsANoOp(t *testing.T) {
	e := newEnv(t)
	e.mustSubmit(signed(auditor1, "1", e.item(auditor1, "e1", "l1")))
	am := evidence.NewAppModule(e.k, nil)
	mm := module.NewManager(am)
	r := newRouters(t, am)
	before := e.snapshot(e.unmetered())
	vm, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{evidence.ModuleName: evidence.ConsensusVersion})
	if err != nil {
		t.Fatal(err)
	}
	if vm[evidence.ModuleName] != evidence.ConsensusVersion || len(vm) != 1 {
		t.Fatalf("version map after migrations %v", vm)
	}
	if string(before) != string(e.snapshot(e.unmetered())) {
		t.Fatal("RunMigrations changed the store")
	}
}

// TestNoMigrationFromScaffoldLayout: version 1 is the goal-05 scaffold
// layout (one key [0x01] holding the genesis JSON, inside this layout's
// record prefix). No step leaves it: RunMigrations from 1 fails and the
// store is untouched, and InitGenesis refuses a store holding that key
// (TestGenesisValidateImportExport/requires-an-empty-store).
func TestNoMigrationFromScaffoldLayout(t *testing.T) {
	e := newEnv(t)
	am := evidence.NewAppModule(e.k, nil)
	mm := module.NewManager(am)
	r := newRouters(t, am)
	before := e.snapshot(e.unmetered())
	if _, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{evidence.ModuleName: msgkit.ScaffoldLayoutVersion}); err == nil {
		t.Fatal("RunMigrations from the scaffold layout succeeded")
	}
	if string(before) != string(e.snapshot(e.unmetered())) {
		t.Fatal("a failed migration changed the store")
	}
}

type versionedModule struct {
	evidence.AppModule
	version uint64
}

func (v versionedModule) ConsensusVersion() uint64 { return v.version }

// TestMigrationTable: the table covers exactly 2 .. ConsensusVersion-1 (no
// step from the scaffold version 1), and RegisterServices registers each
// entry with the configurator, where the module manager runs it.
func TestMigrationTable(t *testing.T) {
	table := evidence.MigrationsForTest()
	first := msgkit.ScaffoldLayoutVersion + 1
	for from := first; from < evidence.ConsensusVersion; from++ {
		if table[from] == nil {
			t.Fatalf("no migration from %d", from)
		}
	}
	if uint64(len(table)) != evidence.ConsensusVersion-first || table[msgkit.ScaffoldLayoutVersion] != nil {
		t.Fatalf("%d migrations for ConsensusVersion %d", len(table), evidence.ConsensusVersion)
	}
	ran := 0
	restore := evidence.SetMigrationsForTest(map[uint64]module.MigrationHandler{
		evidence.ConsensusVersion: func(sdk.Context) error { ran++; return nil },
	})
	defer restore()
	e := newEnv(t)
	r := newRouters(t, evidence.NewAppModule(e.k, nil))
	if err := r.cfg.RegisterMigration(evidence.ModuleName, evidence.ConsensusVersion, func(sdk.Context) error { return nil }); err == nil {
		t.Fatal("RegisterServices did not register the migration")
	}
	mm := module.NewManager(versionedModule{AppModule: evidence.NewAppModule(e.k, nil), version: evidence.ConsensusVersion + 1})
	vm, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{evidence.ModuleName: evidence.ConsensusVersion})
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 || vm[evidence.ModuleName] != evidence.ConsensusVersion+1 {
		t.Fatalf("step ran %d times, version map %v", ran, vm)
	}
}

// TestNewKeeperRejectsMissingDependencies: wiring errors panic; Peers may
// be nil.
func TestNewKeeperRejectsMissingDependencies(t *testing.T) {
	e := newEnv(t)
	full := evidence.Deps{Roles: e.auth, Limits: e.auth, Leases: e.leases, Content: e.content}
	cases := map[string]func(d *evidence.Deps){
		"roles":   func(d *evidence.Deps) { d.Roles = nil },
		"limits":  func(d *evidence.Deps) { d.Limits = nil },
		"leases":  func(d *evidence.Deps) { d.Leases = nil },
		"content": func(d *evidence.Deps) { d.Content = nil },
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
			evidence.NewKeeper(e.keys[evidence.ModuleName], d)
		})
	}
	t.Run("nil-key", func(t *testing.T) {
		defer func() {
			if recover() == nil {
				t.Fatal("no panic")
			}
		}()
		evidence.NewKeeper(nil, full)
	})
	if k := evidence.NewKeeper(e.keys[evidence.ModuleName], full); k.StoreKey() != e.keys[evidence.ModuleName] {
		t.Fatal("store key")
	}
}

// TestDeterministicSources (I11): no clock, randomness, environment, file,
// network, process, or floating-point use in the module's non-test code
// (the evidencetest helpers are test support and are not scanned).
func TestDeterministicSources(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
}

// TestKeeperImports: the module never imports x/leases, x/content,
// x/providers, x/authority, the scaffold, the app, the devnet, the node,
// the command, or the off-chain agents, in code or in tests (they use
// fakes), and it imports no network package.
func TestKeeperImports(t *testing.T) {
	const root = "github.com/Maxencejules/shortvideo-network/"
	forbidden := []string{
		root + "chain/x/authority", root + "chain/app", root + "chain/x/content", root + "chain/x/providers",
		root + "chain/x/leases", root + "chain/x/policy", root + "chain/x/scaffold", root + "chain/devnet",
		root + "chain/node", root + "chain/cmd", root + "agents", "net", "os/exec",
	}
	for _, includeTests := range []bool{false, true} {
		files := parseGoFiles(t, ".", includeTests)
		files = append(files, parseGoFiles(t, "evidencetest", includeTests)...)
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

// TestNoMerkleCode (DR-09-G1): no identifier, string, or comment in
// x/evidence names a Merkle construction; nothing here is a storage proof.
func TestNoMerkleCode(t *testing.T) {
	for _, dir := range []string{".", "evidencetest"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
				continue
			}
			raw, err := os.ReadFile(filepath.Join(dir, entry.Name()))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(strings.ToLower(string(raw)), "merkle") {
				t.Errorf("%s mentions merkle", entry.Name())
			}
		}
	}
}

type source struct {
	path string
	ast  *ast.File
}

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

// TestNoForbiddenNames (REQ-HOST-003): no field of an evidence message, no
// evidence event type, and no attribute key an evidence flow emits matches
// the bounds.json forbidden-name patterns.
func TestNoForbiddenNames(t *testing.T) {
	patterns, exemptions := msgkittest.ForbiddenNamePatterns(t)
	check := func(where, name string) {
		t.Helper()
		if hits := msgkittest.NameHits(name, patterns, exemptions); len(hits) != 0 {
			t.Errorf("%s %q matches forbidden patterns %v", where, name, hits)
		}
	}
	for _, path := range []string{"svn/evidence/v1/state.proto", "svn/evidence/v1/tx.proto", "svn/evidence/v1/query.proto", "svn/evidence/v1/genesis.proto"} {
		fd, err := protoregistry.GlobalFiles.FindFileByPath(path)
		if err != nil {
			t.Fatalf("%s: %v", path, err)
		}
		for i := 0; i < fd.Messages().Len(); i++ {
			md := fd.Messages().Get(i)
			check("message", string(md.Name()))
			for j := 0; j < md.Fields().Len(); j++ {
				check(string(md.FullName())+" field", string(md.Fields().Get(j).Name()))
			}
		}
	}
	e := newEnv(t)
	var events []abci.Event
	msg := signed(auditor1, "1", e.item(auditor1, "e1", "l1"))
	events = append(events, e.submit(e.fresh(), msg).events...)
	events = append(events, e.submit(e.fresh(), msg).events...)
	types := map[string]bool{}
	for _, ev := range events {
		types[ev.Type] = true
		check("event type", strings.TrimPrefix(ev.Type, "svn.events.v1."))
		for _, a := range ev.Attributes {
			check("event attribute", a.Key)
		}
	}
	for _, want := range []string{"svn.events.v1.EvidenceAccepted", evidence.EventState, evidence.ModuleName + "_result"} {
		if !types[want] {
			t.Fatalf("flow emitted no %s (types %v)", want, types)
		}
	}
}
