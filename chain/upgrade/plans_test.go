package upgrade_test

import (
	"bytes"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// fakeRows is a unit-test double of PlanRows. planPanic and appliedPanic,
// when set, make the read panic as the x/policy keeper does on a committed
// row that does not decode. appliedCalls records every UpgradeApplied name.
type fakeRows struct {
	plan         upgrade.Plan
	found        bool
	applied      map[string]bool
	planPanic    any
	appliedPanic any
	appliedCalls []string
}

func (f *fakeRows) StoredPlan(sdk.Context) (upgrade.Plan, bool) {
	if f.planPanic != nil {
		panic(f.planPanic)
	}
	return f.plan, f.found
}

func (f *fakeRows) UpgradeApplied(_ sdk.Context, name string) bool {
	f.appliedCalls = append(f.appliedCalls, name)
	if f.appliedPanic != nil {
		panic(f.appliedPanic)
	}
	return f.applied[name]
}

func newCouncilPlans(t *testing.T, rows upgrade.PlanRows) *upgrade.CouncilPlans {
	t.Helper()
	source, err := upgrade.NewCouncilPlans(rows)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

// TestCouncilPlansRefusesMissingRows: NewCouncilPlans refuses nil rows, and
// a source not built by it (the zero value, a nil pointer) returns an
// ErrInvalidDeps error from Plan instead of a plan or a panic. A runner
// refuses a nil source.
func TestCouncilPlansRefusesMissingRows(t *testing.T) {
	source, err := upgrade.NewCouncilPlans(nil)
	wantKind(t, "nil rows", err, upgrade.ErrInvalidDeps, "needs the plan rows")
	if source != nil {
		t.Fatalf("nil rows returned a source")
	}
	for name, unbuilt := range map[string]*upgrade.CouncilPlans{"zero value": {}, "nil pointer": nil} {
		plan, found, err := unbuilt.Plan(sdk.Context{})
		wantKind(t, name, err, upgrade.ErrInvalidDeps, "not built by NewCouncilPlans")
		if found || plan.Name != "" || plan.Digest != nil {
			t.Fatalf("%s: returned a plan %+v (%t)", name, plan, found)
		}
	}
	e := newRunnerEnv(t)
	e.deps.Plans = nil
	_, err = upgrade.NewRunner(e.binary, e.deps)
	wantKind(t, "runner without a plan source", err, upgrade.ErrInvalidDeps, "plans false")
}

// TestCouncilPlansReportsTheStoredPlan: with a plan row, Plan returns its
// name, height, and digest, and asks UpgradeApplied about that name once;
// the digest is a copy in both directions. Without a plan row, Plan returns
// found false and reads no applied row.
func TestCouncilPlansReportsTheStoredPlan(t *testing.T) {
	stored := digest(0x5a)
	rows := &fakeRows{plan: upgrade.Plan{Name: "up-1", Height: 200, Digest: stored}, found: true}
	source := newCouncilPlans(t, rows)
	plan, found, err := source.Plan(sdk.Context{})
	if err != nil || !found {
		t.Fatalf("Plan: %t %v", found, err)
	}
	if plan.Name != "up-1" || plan.Height != 200 || !bytes.Equal(plan.Digest, digest(0x5a)) {
		t.Fatalf("Plan returned %+v", plan)
	}
	if !reflect.DeepEqual(rows.appliedCalls, []string{"up-1"}) {
		t.Fatalf("UpgradeApplied calls %q, want [up-1]", rows.appliedCalls)
	}
	plan.Digest[0] ^= 0xff
	if stored[0] != 0x5a {
		t.Fatal("changing the returned digest changed the stored row")
	}
	stored[1] ^= 0xff
	again, _, err := source.Plan(sdk.Context{})
	if err != nil || again.Digest[1] != 0x5a^0xff {
		t.Fatalf("a second read did not return the row as it is now: %x %v", again.Digest, err)
	}
	if plan.Digest[1] != 0x5a {
		t.Fatal("changing the stored row changed a digest returned earlier")
	}

	empty := &fakeRows{plan: upgrade.Plan{Name: "ignored", Height: 9, Digest: digest(1)}}
	plan, found, err = newCouncilPlans(t, empty).Plan(sdk.Context{})
	if err != nil || found || plan.Name != "" || plan.Height != 0 || plan.Digest != nil {
		t.Fatalf("no plan row: %+v %t %v", plan, found, err)
	}
	if len(empty.appliedCalls) != 0 {
		t.Fatalf("no plan row, but UpgradeApplied was called for %q", empty.appliedCalls)
	}
}

// TestCouncilPlansRefusesDamagedRows: a plan row the council writers cannot
// have written, a plan of an upgrade that already applied, and a read that
// panics are each an ErrIntegrity error with no plan, never a plan and
// never a panic. A name outside the id pattern is not looked up.
func TestCouncilPlansRefusesDamagedRows(t *testing.T) {
	good := func() *fakeRows {
		return &fakeRows{plan: upgrade.Plan{Name: "up-1", Height: 200, Digest: digest(3)}, found: true}
	}
	edit := func(change func(*fakeRows)) *fakeRows {
		rows := good()
		change(rows)
		return rows
	}
	rows := []struct {
		name      string
		rows      *fakeRows
		want      string
		lookedUp  bool
		wantCause bool
	}{
		{"empty name", edit(func(r *fakeRows) { r.plan.Name = "" }), `upgrade plan name "" is outside the id pattern`, false, false},
		{"upper-case name", edit(func(r *fakeRows) { r.plan.Name = "Up-1" }), `upgrade plan name "Up-1" is outside the id pattern`, false, false},
		{"name of 65 bytes", edit(func(r *fakeRows) { r.plan.Name = strings.Repeat("a", 65) }), "is outside the id pattern", false, false},
		{"height 0", edit(func(r *fakeRows) { r.plan.Height = 0 }), "upgrade plan up-1 has height 0", true, false},
		{"digest of 31 bytes", edit(func(r *fakeRows) { r.plan.Digest = r.plan.Digest[:31] }), "release digest of 31 bytes, want 32", true, false},
		{"digest of 33 bytes", edit(func(r *fakeRows) { r.plan.Digest = append(r.plan.Digest, 0) }), "release digest of 33 bytes, want 32", true, false},
		{"no digest", edit(func(r *fakeRows) { r.plan.Digest = nil }), "release digest of 0 bytes, want 32", true, false},
		{"upgrade already applied", edit(func(r *fakeRows) { r.applied = map[string]bool{"up-1": true} }),
			"upgrade plan up-1 names an upgrade that has already applied", true, false},
		{"plan row panics", edit(func(r *fakeRows) { r.planPanic = "policy: plan value is 3 bytes, want 168" }),
			"read the committed upgrade plan rows: panic: policy: plan value is 3 bytes, want 168", false, true},
		{"applied row panics", edit(func(r *fakeRows) { r.appliedPanic = errors.New("policy: applied value is 2 bytes") }),
			"read the committed upgrade plan rows: panic: policy: applied value is 2 bytes", true, true},
	}
	for _, r := range rows {
		plan, found, err := newCouncilPlans(t, r.rows).Plan(sdk.Context{})
		wantKind(t, r.name, err, upgrade.ErrIntegrity, r.want)
		if found || plan.Name != "" || plan.Height != 0 || plan.Digest != nil {
			t.Fatalf("%s: an error returned a plan %+v (%t)", r.name, plan, found)
		}
		if lookedUp := len(r.rows.appliedCalls) > 0; lookedUp != r.lookedUp {
			t.Fatalf("%s: UpgradeApplied calls %q, want looked up %t", r.name, r.rows.appliedCalls, r.lookedUp)
		}
		if hasCause := len(err.(interface{ Unwrap() []error }).Unwrap()) == 2; hasCause != r.wantCause {
			t.Fatalf("%s: error %q has a cause %t, want %t", r.name, err, hasCause, r.wantCause)
		}
	}
	plan, found, err := newCouncilPlans(t, good()).Plan(sdk.Context{})
	if err != nil || !found || plan.Name != "up-1" {
		t.Fatalf("control: %+v %t %v", plan, found, err)
	}
}

// methodNames returns the exported method names of typ in byte order.
func methodNames(typ reflect.Type) []string {
	names := make([]string, 0, typ.NumMethod())
	for i := 0; i < typ.NumMethod(); i++ {
		names = append(names, typ.Method(i).Name)
	}
	sort.Strings(names)
	return names
}

// TestCouncilPlansOnlyReads: the production plan source and the rows it
// reads have read methods only. There is no node-local way, through this
// package, to schedule, replace, move, cancel, or skip a plan: under owner
// question Q2 (option "Council plan, fail closed (Recommended)") only an
// executed council proposal writes the plan row.
func TestCouncilPlansOnlyReads(t *testing.T) {
	var _ upgrade.PlanSource = (*upgrade.CouncilPlans)(nil)
	for _, c := range []struct {
		name string
		typ  reflect.Type
		want []string
	}{
		{"*CouncilPlans", reflect.TypeOf((*upgrade.CouncilPlans)(nil)), []string{"Plan"}},
		{"PlanSource", reflect.TypeOf((*upgrade.PlanSource)(nil)).Elem(), []string{"Plan"}},
		{"PlanRows", reflect.TypeOf((*upgrade.PlanRows)(nil)).Elem(), []string{"StoredPlan", "UpgradeApplied"}},
	} {
		if got := methodNames(c.typ); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s methods %q, want %q", c.name, got, c.want)
		}
	}
	if fields := reflect.TypeOf(upgrade.CouncilPlans{}); fields.NumField() != 1 || fields.Field(0).IsExported() {
		t.Errorf("CouncilPlans has %d fields; want one unexported field (the rows)", fields.NumField())
	}
}

// TestPlanWritersHaveNoNodeLocalCaller: in the non-test Go code of the chain
// module, the plan writers are called only on the council path and by the
// runner. ScheduleUpgrade and CancelUpgrade are called only in
// chain/x/authority (an executed software_upgrade or cancel_upgrade payload)
// and chain/x/policy (the writers' own package); RecordUpgrade only in
// chain/upgrade (the runner) and chain/x/policy. So no node, command, app,
// or other module code schedules, moves, cancels, or records a plan. A
// control finds an allowed call of each name.
func TestPlanWritersHaveNoNodeLocalCaller(t *testing.T) {
	root := ".."
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("the scan root %s is not the chain module: %v", root, err)
	}
	allowed := map[string][]string{
		"ScheduleUpgrade": {"x/authority", "x/policy"},
		"CancelUpgrade":   {"x/authority", "x/policy"},
		"RecordUpgrade":   {"upgrade", "x/policy"},
	}
	inside := func(dir string, dirs []string) bool {
		for _, d := range dirs {
			if dir == d || strings.HasPrefix(dir, d+"/") {
				return true
			}
		}
		return false
	}
	found := map[string]bool{}
	files := 0
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if path != root && (strings.HasPrefix(entry.Name(), ".") || entry.Name() == "testdata" || entry.Name() == "vendor") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		files++
		rel, err := filepath.Rel(root, filepath.Dir(path))
		if err != nil {
			return err
		}
		dir := filepath.ToSlash(rel)
		ast.Inspect(file, func(node ast.Node) bool {
			sel, ok := node.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			dirs, watched := allowed[sel.Sel.Name]
			switch {
			case !watched:
			case inside(dir, dirs):
				found[sel.Sel.Name] = true
			default:
				t.Errorf("%s: %s is used outside %q", fset.Position(sel.Pos()), sel.Sel.Name, dirs)
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files == 0 {
		t.Fatal("the scan read no file")
	}
	for name := range allowed {
		if !found[name] {
			t.Errorf("control: the scan found no allowed use of %s", name)
		}
	}
}
