package policy_test

import (
	"context"
	"encoding/json"
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

	"cosmossdk.io/core/appmodule"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// routers is a real SDK message router and gRPC query router wired through
// a real configurator.
type routers struct {
	msg   *baseapp.MsgServiceRouter
	query *baseapp.GRPCQueryRouter
	cfg   module.Configurator
}

func newRouters(t testing.TB, am policy.AppModule) routers {
	t.Helper()
	registry := codectypes.NewInterfaceRegistry()
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

type recorder struct{ names []string }

func (r *recorder) Record(name string) { r.names = append(r.names, name) }

// TestModule: the module's identity, interfaces (a BeginBlock, no
// EndBlock, no validator updates), default genesis, and that it registers
// the query service and no message service; InitGenesis records the call
// and panics on a bad document; BeginBlock runs ApplyDue and refuses a
// context without an SDK context.
func TestModule(t *testing.T) {
	e := newBareEnv(t)
	rec := &recorder{}
	am := policy.NewAppModule(e.k, rec)
	if am.Name() != policy.ModuleName || am.ConsensusVersion() != 2 {
		t.Fatalf("module %s version %d", am.Name(), am.ConsensusVersion())
	}
	var (
		_ module.AppModule           = am
		_ module.HasGenesis          = am
		_ module.HasServices         = am
		_ module.HasConsensusVersion = am
		_ appmodule.AppModule        = am
	)
	if _, ok := any(am).(appmodule.HasBeginBlocker); !ok {
		t.Fatal("the module has no BeginBlock")
	}
	if _, ok := any(am).(appmodule.HasEndBlocker); ok {
		t.Fatal("the module has an EndBlock")
	}
	if _, ok := any(am).(module.HasABCIEndBlock); ok {
		t.Fatal("the module returns validator updates")
	}
	def := am.DefaultGenesis(nil)
	if err := am.ValidateGenesis(nil, nil, def); err != nil {
		t.Fatalf("default genesis: %v", err)
	}
	if err := am.ValidateGenesis(nil, nil, json.RawMessage(`{}`)); err == nil {
		t.Fatal("{} validated")
	}
	if err := am.ValidateGenesis(nil, nil, json.RawMessage(`{"x":1}`)); err == nil {
		t.Fatal("unknown field validated")
	}
	am.InitGenesis(e.ctx, nil, def)
	if len(rec.names) != 1 || rec.names[0] != policy.ModuleName {
		t.Fatalf("recorded %v", rec.names)
	}
	mustPanic(t, "store is not empty", func() { am.InitGenesis(e.ctx, nil, def) })
	mustPanic(t, "policy genesis", func() { policy.NewAppModule(newBareEnv(t).k, nil).InitGenesis(e.ctx, nil, json.RawMessage(`{}`)) })
	if err := e.k.InitVersions(e.ctx, testVersionMap(), 1); err != nil {
		t.Fatal(err)
	}
	state, err := policy.ParseGenesis(am.ExportGenesis(e.ctx, nil))
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(state, policy.DefaultGenesisState()) {
		t.Fatal("module export")
	}
	r := newRouters(t, am)
	if r.query.Route("/svn.policy.v1.Query/Policy") == nil {
		t.Fatal("Query/Policy not routed")
	}
	if r.msg.HandlerByTypeURL("/svn.policy.v1.MsgUpdateLimits") != nil {
		t.Fatal("a policy message is routed")
	}
	e.auth.version = 2
	mustOK(t, e.k.SchedulePending(e.fresh(), msgkit.LimitMaxPageLimit, 9, h+1, "p1", 2))
	if err := am.BeginBlock(e.at(h)); err != nil {
		t.Fatal(err)
	}
	if _, found := e.k.GovernedLimit(e.ctx, msgkit.LimitMaxPageLimit); found {
		t.Fatal("BeginBlock below the activation height applied the change")
	}
	if err := am.BeginBlock(e.at(h + 1)); err != nil {
		t.Fatal(err)
	}
	if v, found := e.k.GovernedLimit(e.ctx, msgkit.LimitMaxPageLimit); !found || v != 9 {
		t.Fatalf("BeginBlock at the activation height: (%d, %t)", v, found)
	}
	if err := am.BeginBlock(context.Background()); err == nil || !strings.Contains(err.Error(), "no SDK context") {
		t.Fatalf("BeginBlock on a plain context: %v", err)
	}
}

// TestPolicyQuery: Query/Policy over the real gRPC query router and the
// interceptor path returns the PolicyState of schema version 2: the policy
// version and the decision rows from the authority source, the stored caps,
// the value in force of all five governed keys in key order (the ceiling
// where no value is set), the pending changes in key order, the plan, and
// the app version. A plain context is MALFORMED; missing caps or a damaged
// governed, pending, plan, or app version row is an error, not a panic.
func TestPolicyQuery(t *testing.T) {
	e := newEnv(t)
	e.auth.version = 4
	am := policy.NewAppModule(e.k, nil)
	r := newRouters(t, am)
	handler := r.query.Route("/svn.policy.v1.Query/Policy")
	reqBytes, err := proto.Marshal(&policyv1.QueryPolicyRequest{})
	if err != nil {
		t.Fatal(err)
	}
	ceilings := func() []*policyv1.GovernedLimitValue {
		var out []*policyv1.GovernedLimitValue
		for _, key := range msgkit.LimitKeys() {
			c, _ := msgkit.Ceiling(key)
			out = append(out, &policyv1.GovernedLimitValue{LimitKey: key, Value: c})
		}
		return out
	}
	want := &policyv1.PolicyState{
		PolicyVersion:    4,
		SafetyCaps:       policy.DefaultSafetyCaps(),
		BlockedDecisions: cloneDecisions(e.auth.decisions),
		GovernedLimits:   ceilings(),
		AppVersion:       1,
	}
	res, err := handler(e.ctx, &abci.RequestQuery{Data: reqBytes, Path: "/svn.policy.v1.Query/Policy"})
	if err != nil {
		t.Fatal(err)
	}
	var out policyv1.QueryPolicyResponse
	if err := proto.Unmarshal(res.Value, &out); err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(out.GetState(), want) {
		t.Fatalf("fresh query state %v", out.GetState())
	}
	if keys := out.GetState().GetGovernedLimits(); len(keys) != 5 || keys[0].GetLimitKey() != msgkit.LimitMaxEvidenceBatch || keys[4].GetLimitKey() != msgkit.LimitMaxPageLimit {
		t.Fatalf("governed limits not the five keys in key order: %v", keys)
	}

	// A governed value, two pending changes, and a plan.
	mustOK(t, e.k.SetLimit(e.fresh(), msgkit.LimitMaxPageLimit, 3, "p1"))
	mustOK(t, e.k.SchedulePending(e.fresh(), msgkit.LimitMaxPageLimit, 7, h+20, "p2", 3))
	mustOK(t, e.k.SchedulePending(e.fresh(), msgkit.LimitMaxEvidenceBatch, 2, h+9, "p3", 4))
	mustOK(t, e.k.ScheduleUpgrade(e.fresh(), "up-1", h+lead, digestOf("r"), "p4"))
	for _, g := range want.GovernedLimits {
		if g.LimitKey == msgkit.LimitMaxPageLimit {
			g.Value = 3
		}
	}
	want.PendingLimitChanges = []*policyv1.PendingLimitChange{
		{LimitKey: msgkit.LimitMaxEvidenceBatch, Value: 2, ActivationHeight: h + 9, ProposalId: "p3", PolicyVersion: 4},
		{LimitKey: msgkit.LimitMaxPageLimit, Value: 7, ActivationHeight: h + 20, ProposalId: "p2", PolicyVersion: 3},
	}
	want.UpgradePlan = &policyv1.UpgradePlan{
		Name: "up-1", Height: h + lead, ProposalId: "p4",
		ReleaseDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: digestOf("r")},
	}
	direct, err := e.k.Policy(e.ctx, &policyv1.QueryPolicyRequest{})
	if err != nil || !proto.Equal(direct.GetState(), want) {
		t.Fatalf("query state %v %v", direct, err)
	}
	// After BeginBlock at h+9 the evidence batch change is in force.
	if err := e.k.ApplyDue(e.at(h + 9)); err != nil {
		t.Fatal(err)
	}
	for _, g := range want.GovernedLimits {
		if g.LimitKey == msgkit.LimitMaxEvidenceBatch {
			g.Value = 2
		}
	}
	want.PendingLimitChanges = want.PendingLimitChanges[1:]
	if got, err := e.k.Policy(e.ctx, &policyv1.QueryPolicyRequest{}); err != nil || !proto.Equal(got.GetState(), want) {
		t.Fatalf("query state after activation %v %v", got, err)
	}
	direct.State.UpgradePlan.ReleaseDigest.Value[0] ^= 0xff
	if plan, _ := e.k.Plan(e.ctx); plan.Digest[0] == direct.State.UpgradePlan.ReleaseDigest.Value[0] {
		t.Fatal("the query shares the stored digest")
	}
	direct.State.BlockedDecisions[0].Approved = true
	if e.auth.decisions[0].GetApproved() {
		t.Fatal("the query shares the authority rows")
	}
	desc := policy.QueryServiceDesc(e.k)
	if desc.ServiceName != "svn.policy.v1.Query" || len(desc.Methods) != 1 || desc.Methods[0].MethodName != "Policy" {
		t.Fatalf("service desc %+v", desc)
	}
	var info *grpc.UnaryServerInfo
	interceptor := func(ctx context.Context, req any, i *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		info = i
		return h(ctx, req)
	}
	got, err := desc.Methods[0].Handler(e.k, e.ctx, func(any) error { return nil }, interceptor)
	if err != nil || info.FullMethod != "/svn.policy.v1.Query/Policy" || got.(*policyv1.QueryPolicyResponse).GetState().GetPolicyVersion() != 4 {
		t.Fatalf("intercepted call %v %v %v", got, info, err)
	}
	wrong := func(ctx context.Context, _ any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		return h(ctx, &policyv1.QueryPolicyResponse{})
	}
	if _, err := desc.Methods[0].Handler(e.k, e.ctx, func(any) error { return nil }, wrong); err == nil || !strings.Contains(err.Error(), "request type") {
		t.Fatalf("wrong request type: %v", err)
	}
	if _, err := e.k.Policy(context.Background(), &policyv1.QueryPolicyRequest{}); err == nil || !strings.Contains(err.Error(), "no SDK context") {
		t.Fatalf("plain context: %v", err)
	}
	wrapped := context.WithValue(context.Background(), sdk.SdkContextKey, e.ctx)
	if _, err := e.k.Policy(wrapped, nil); err != nil {
		t.Fatalf("wrapped context: %v", err)
	}
	e.store().Delete(policy.SafetyCapsKey())
	if _, err := e.k.Policy(e.ctx, &policyv1.QueryPolicyRequest{}); err == nil || !strings.Contains(err.Error(), "safety caps row missing") {
		t.Fatalf("missing caps: %v", err)
	}
	damage := map[string]struct {
		key   []byte
		value []byte
		want  string
	}{
		"governed": {policy.GovernedKey(msgkit.LimitMaxOpenOffers), []byte{1}, `governed limit "max_open_offers"`},
		"pending":  {policy.PendingKey(msgkit.LimitMaxOpenOffers), []byte{1}, `pending change of "max_open_offers"`},
		"plan":     {policy.PlanKey(), []byte{1}, "plan value is 1 bytes"},
		"app":      {policy.AppVersionKey(), policy.EncodeU64(0), "app version is not"},
	}
	for name, d := range damage {
		t.Run("damaged "+name, func(t *testing.T) {
			e := newEnv(t)
			e.store().Set(d.key, d.value)
			if _, err := e.k.Policy(e.ctx, &policyv1.QueryPolicyRequest{}); err == nil || !strings.Contains(err.Error(), d.want) {
				t.Fatalf("%v, want %q", err, d.want)
			}
		})
	}
}

// TestRunMigrationsAtCurrentVersionIsANoOp: RunMigrations from 2 returns 2
// and leaves the store byte-identical.
func TestRunMigrationsAtCurrentVersionIsANoOp(t *testing.T) {
	e := newEnv(t)
	am := policy.NewAppModule(e.k, nil)
	mm := module.NewManager(am)
	r := newRouters(t, am)
	before := e.snapshot()
	vm, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{policy.ModuleName: policy.ConsensusVersion})
	if err != nil {
		t.Fatal(err)
	}
	if vm[policy.ModuleName] != policy.ConsensusVersion || len(vm) != 1 {
		t.Fatalf("version map after migrations %v", vm)
	}
	if string(before) != string(e.snapshot()) {
		t.Fatal("RunMigrations changed the store")
	}
}

// TestNoMigrationFromScaffoldLayout: version 1 is the scaffold layout (one
// key [0x01] holding the genesis JSON, the key this layout uses for the
// caps). No step leaves it: RunMigrations from 1 fails and the store is
// untouched, and InitGenesis refuses a store holding that key.
func TestNoMigrationFromScaffoldLayout(t *testing.T) {
	e := newBareEnv(t)
	e.store().Set([]byte{0x01}, []byte(appDocument))
	am := policy.NewAppModule(e.k, nil)
	mm := module.NewManager(am)
	r := newRouters(t, am)
	before := e.snapshot()
	if _, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{policy.ModuleName: msgkit.ScaffoldLayoutVersion}); err == nil {
		t.Fatal("RunMigrations from the scaffold layout succeeded")
	}
	if err := e.k.InitGenesis(e.ctx, policy.DefaultGenesisState()); err == nil {
		t.Fatal("InitGenesis over the scaffold key succeeded")
	}
	if string(before) != string(e.snapshot()) {
		t.Fatal("a failed migration changed the store")
	}
}

type versionedModule struct {
	policy.AppModule
	version uint64
}

func (v versionedModule) ConsensusVersion() uint64 { return v.version }

// TestMigrationTable: the table covers exactly 2 .. ConsensusVersion-1 (no
// step from the scaffold version 1), and RegisterServices registers each
// entry with the configurator, where the module manager runs it.
func TestMigrationTable(t *testing.T) {
	table := policy.MigrationsForTest()
	first := msgkit.ScaffoldLayoutVersion + 1
	for from := first; from < policy.ConsensusVersion; from++ {
		if table[from] == nil {
			t.Fatalf("no migration from %d", from)
		}
	}
	if uint64(len(table)) != policy.ConsensusVersion-first || table[msgkit.ScaffoldLayoutVersion] != nil {
		t.Fatalf("%d migrations for ConsensusVersion %d", len(table), policy.ConsensusVersion)
	}
	ran := 0
	restore := policy.SetMigrationsForTest(map[uint64]module.MigrationHandler{
		policy.ConsensusVersion: func(sdk.Context) error { ran++; return nil },
	})
	defer restore()
	e := newEnv(t)
	r := newRouters(t, policy.NewAppModule(e.k, nil))
	if err := r.cfg.RegisterMigration(policy.ModuleName, policy.ConsensusVersion, func(sdk.Context) error { return nil }); err == nil {
		t.Fatal("RegisterServices did not register the migration")
	}
	mm := module.NewManager(versionedModule{AppModule: policy.NewAppModule(e.k, nil), version: policy.ConsensusVersion + 1})
	vm, err := mm.RunMigrations(e.fresh(), r.cfg, module.VersionMap{policy.ModuleName: policy.ConsensusVersion})
	if err != nil {
		t.Fatal(err)
	}
	if ran != 1 || vm[policy.ModuleName] != policy.ConsensusVersion+1 {
		t.Fatalf("ran %d, version map %v", ran, vm)
	}
}

// TestDeterministicSources runs the msgkittest source scan on the chain
// code of this module.
func TestDeterministicSources(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
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

// TestKeeperImports: the module and policytest never import x/authority, the
// app, the other modules, the scaffold, chain/upgrade, the devnet, the node,
// the command, or the off-chain agents, in code or in tests (they use
// fakes), and import no network package. The chain code imports no os
// package at all (the test files read sources with os).
func TestKeeperImports(t *testing.T) {
	const root = "github.com/Maxencejules/shortvideo-network/"
	forbidden := []string{
		root + "chain/x/authority", root + "chain/app", root + "chain/x/content", root + "chain/x/providers",
		root + "chain/x/leases", root + "chain/x/evidence", root + "chain/x/scaffold", root + "chain/upgrade",
		root + "chain/devnet", root + "chain/node", root + "chain/cmd", root + "chain/internal", root + "agents",
		"net", "os/exec",
	}
	for _, includeTests := range []bool{false, true} {
		files := parseGoFiles(t, ".", includeTests)
		files = append(files, parseGoFiles(t, "policytest", includeTests)...)
		if len(files) == 0 {
			t.Fatal("no sources")
		}
		for _, file := range files {
			for _, spec := range file.ast.Imports {
				path, err := strconv.Unquote(spec.Path.Value)
				if err != nil {
					t.Fatal(err)
				}
				bad := forbidden
				if !includeTests {
					bad = append(append([]string(nil), forbidden...), "os", "time", "math/rand", "crypto/rand")
				}
				for _, b := range bad {
					if path == b || strings.HasPrefix(path, b+"/") {
						t.Errorf("%s imports %s", file.path, path)
					}
				}
			}
		}
	}
}

// TestNoForbiddenNames (REQ-HOST-003): no event type or attribute key of
// this module matches the bounds.json forbidden-name patterns.
func TestNoForbiddenNames(t *testing.T) {
	patterns, exemptions := msgkittest.ForbiddenNamePatterns(t)
	names := []string{
		policy.EventPolicyPending, policy.EventPolicyActivated, policy.EventUpgradePlan,
		policy.AttrParameterKey, policy.AttrValue, policy.AttrActivationHeight, policy.AttrProposalID,
		policy.AttrSupersededProposalID, policy.AttrName, policy.AttrHeight, policy.AttrReleaseDigest,
		policy.AttrAction, policy.AttrReplacedName,
		policy.PlanActionScheduled, policy.PlanActionReplaced, policy.PlanActionCancelled,
	}
	for _, name := range names {
		if hits := msgkittest.NameHits(name, patterns, exemptions); len(hits) != 0 {
			t.Errorf("%q matches forbidden patterns %v", name, hits)
		}
	}
	e := newEnv(t)
	e.auth.version = 3
	ctx := e.fresh()
	mustOK(t, e.k.SchedulePending(ctx, msgkit.LimitMaxPageLimit, 2, h+2, "p1", 2))
	mustOK(t, e.k.SetLimit(ctx, msgkit.LimitMaxPageLimit, 3, "p2"))
	mustOK(t, e.k.ScheduleUpgrade(ctx, "up-1", h+lead, digestOf("r"), "p3"))
	mustOK(t, e.k.CancelUpgrade(ctx, "up-1"))
	mustOK(t, e.k.SchedulePending(ctx, msgkit.LimitMaxOpenOffers, 2, h+2, "p4", 3))
	if err := e.k.ApplyDue(ctx.WithBlockHeight(h + 2)); err != nil {
		t.Fatal(err)
	}
	events := ctx.EventManager().Events().ToABCIEvents()
	if len(events) != 6 {
		t.Fatalf("%d events", len(events))
	}
	for _, ev := range events {
		for _, a := range ev.Attributes {
			if hits := msgkittest.NameHits(a.Key, patterns, exemptions); len(hits) != 0 {
				t.Errorf("%s attribute %q matches %v", ev.Type, a.Key, hits)
			}
		}
	}
}
