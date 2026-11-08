package upgrade_test

import (
	"errors"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// Binary B2 of the gate table: app version 2, leases at version 3, with
// the handler for "v2" (1 -> 2). The state before v2 has leases at 2.
var (
	gateVM     = module.VersionMap{"auth": 7, "authority": 2, "leases": 3, "policy": 2, "svn": 1}
	gateOldVM  = module.VersionMap{"auth": 7, "authority": 2, "leases": 2, "policy": 2, "svn": 1}
	gateLayout = map[string]uint64{"authority": 2, "leases": 3, "policy": 2}
	gateOldLay = map[string]uint64{"authority": 2, "leases": 2, "policy": 2}
)

func layouts(m map[string]uint64) map[string]uint64 {
	out := map[string]uint64{}
	for k, v := range m {
		out[k] = v
	}
	return out
}

func gateBinary() upgrade.Binary {
	return upgrade.Binary{AppVersion: 2, Modules: vm(gateVM), Upgrades: []upgrade.Upgrade{up("v2", 1, 2)}}
}

// current is a consistent state at app version 2 (the binary's) with the
// last committed height 10 and no plan.
func current() upgrade.StoredState {
	return upgrade.StoredState{LastHeight: 10, AppVersion: 2, ConsensusAppVersion: 2, Modules: vm(gateVM), Layouts: layouts(gateLayout)}
}

// window is a consistent state at app version 1 with the plan for v2 at
// height 11, the next block.
func window() upgrade.StoredState {
	return upgrade.StoredState{
		LastHeight: 10, AppVersion: 1, ConsensusAppVersion: 1, Modules: vm(gateOldVM), Layouts: layouts(gateOldLay),
		Plan: &upgrade.Plan{Name: "v2", Height: 11, Digest: digest(7)},
	}
}

// TestGateTable: one or more rows per case of the load gate (plan 11.4),
// including module-set differences, the stored Version.App mismatch, and
// a handler whose ToAppVersion is not the binary's. Each refusal asserts
// its row and the named reason; each start asserts the row and window.
func TestGateTable(t *testing.T) {
	type row struct {
		name   string
		state  func() upgrade.StoredState
		binary func() upgrade.Binary
		row    int
		want   string
	}
	b2 := gateBinary
	b1 := func() upgrade.Binary { return upgrade.Binary{AppVersion: 1, Modules: vm(gateOldVM)} }
	withState := func(base func() upgrade.StoredState, edit func(*upgrade.StoredState)) func() upgrade.StoredState {
		return func() upgrade.StoredState {
			s := base()
			edit(&s)
			return s
		}
	}
	withBinary := func(base func() upgrade.Binary, edit func(*upgrade.Binary)) func() upgrade.Binary {
		return func() upgrade.Binary {
			b := base()
			edit(&b)
			return b
		}
	}
	tooMany := module.VersionMap{}
	for i := 0; i <= upgrade.MaxModules; i++ {
		tooMany[fmt.Sprintf("m%02d", i)] = 1
	}
	rows := []row{
		// Row 1.
		{"row 1: no plan", current, b2, 1, ""},
		{"row 1: plan two blocks ahead", withState(current, func(s *upgrade.StoredState) {
			s.Plan = &upgrade.Plan{Name: "v3", Height: 12, Digest: digest(3)}
		}), b2, 1, ""},
		{"row 1: old binary on old state, plan two blocks ahead", withState(window, func(s *upgrade.StoredState) { s.Plan.Height = 12 }), b1, 1, ""},
		{"row 1: last height at the uint64 maximum, no plan", withState(current, func(s *upgrade.StoredState) { s.LastHeight = math.MaxUint64 }), b2, 1, ""},
		// Row 2.
		{"row 2: old binary after the halt", window, b1, 2,
			"upgrade v2 is due at height 11; install the binary for it (release digest " + strings.Repeat("07", 32) + ")"},
		{"row 2: binary with the same app version and a handler of that name", withState(current, func(s *upgrade.StoredState) {
			s.Plan = &upgrade.Plan{Name: "v2", Height: 11, Digest: digest(7)}
		}), b2, 2, "upgrade v2 is due at height 11"},
		// Row 3.
		{"row 3: upgrade window with a module migration", window, b2, 3, ""},
		{"row 3: upgrade window without a module migration", withState(window, func(s *upgrade.StoredState) {
			s.Modules = vm(gateVM)
			s.Layouts = layouts(gateLayout)
		}), b2, 3, ""},
		// Row 4.
		{"row 4: new binary too early", withState(window, func(s *upgrade.StoredState) { s.Plan.Height = 15 }), b2, 4,
			"too early: run the previous binary until height 14 (upgrade v2 applies at height 15)"},
		// Row 5.
		{"row 5: stored app version above the binary", withState(current, func(s *upgrade.StoredState) {
			s.AppVersion, s.ConsensusAppVersion = 3, 3
		}), b2, 5, "state is newer than this binary (app version 3, binary 2)"},
		{"row 5: old binary on upgraded state", current, b1, 5, "state is newer than this binary (app version 2, binary 1)"},
		{"row 5: stored module version above the binary", withState(current, func(s *upgrade.StoredState) {
			s.Modules["leases"], s.Layouts["leases"] = 4, 4
		}), b2, 5, "module leases stored version 4, binary version 3"},
		{"row 5: stored module the binary lacks", withState(current, func(s *upgrade.StoredState) { s.Modules["gone"] = 1 }), b2, 5,
			"stored module gone is not in this binary"},
		// Row 6.
		{"row 6: home from goal 09 (no app version, no version map)", func() upgrade.StoredState {
			return upgrade.StoredState{LastHeight: 40, ConsensusAppVersion: 1}
		}, b1, 6, "initialize a new home"},
		{"row 6: stored app version differs from Version.App", withState(current, func(s *upgrade.StoredState) { s.ConsensusAppVersion = 1 }), b2, 6,
			"stored app version 2 differs from the stored consensus Version.App 1"},
		{"row 6: binary module missing from the stored map", withState(current, func(s *upgrade.StoredState) { delete(s.Modules, "svn") }), b2, 6,
			"module svn is in this binary but not in the stored version map"},
		{"row 6: stored map entry 0", withState(current, func(s *upgrade.StoredState) { s.Modules["svn"] = 0 }), b2, 6,
			"stored version map entry svn is 0"},
		{"row 6: layout key differs from the stored map", withState(current, func(s *upgrade.StoredState) { s.Layouts["policy"] = 1 }), b2, 6,
			"policy store has layout version 1; the stored version map says 2"},
		{"row 6: layout key absent", withState(current, func(s *upgrade.StoredState) { s.Layouts["authority"] = 0 }), b2, 6,
			"authority store has no layout version key; the stored version map says 2"},
		{"row 6: layout key of a module without a stored entry", withState(current, func(s *upgrade.StoredState) { s.Layouts["orphan"] = 2 }), b2, 6,
			"orphan store has a layout key, but the stored version map has no orphan entry"},
		{"row 6: same app version, older module", withState(current, func(s *upgrade.StoredState) {
			s.Modules["leases"], s.Layouts["leases"] = 2, 2
		}), b2, 6, "app version 2 matches this binary, but module leases stored version 2 differs from binary version 3"},
		{"row 6: more than one app version behind", withState(window, func(s *upgrade.StoredState) {
			s.AppVersion, s.ConsensusAppVersion = 1, 1
		}), withBinary(b2, func(b *upgrade.Binary) { b.AppVersion = 3; b.Upgrades = []upgrade.Upgrade{up("v3", 2, 3)} }), 6,
			"stored app version 1 is more than one behind this binary (3)"},
		{"row 6: one behind without a plan", withState(window, func(s *upgrade.StoredState) { s.Plan = nil }), b2, 6,
			"stored app version 1 is one behind this binary (2) and no upgrade plan is stored"},
		{"row 6: one behind, no handler for the plan", withState(window, func(s *upgrade.StoredState) { s.Plan.Name = "other" }), b2, 6,
			"this binary has no handler for the planned upgrade other"},
		{"row 6: handler to another app version", window, withBinary(b2, func(b *upgrade.Binary) { b.Upgrades[0].ToAppVersion = 3 }), 6,
			"the handler for upgrade v2 goes from app version 1 to 3; stored app version 1, binary 2"},
		{"row 6: handler from another app version", window, withBinary(b2, func(b *upgrade.Binary) { b.Upgrades[0].FromAppVersion = 0 }), 6,
			"the handler for upgrade v2 goes from app version 0 to 2"},
		{"row 6: two handlers of the plan's name", window, withBinary(b2, func(b *upgrade.Binary) {
			b.Upgrades = append(b.Upgrades, up("v2", 1, 2))
		}), 6, "this binary declares 2 handlers named v2"},
		{"row 6: plan at the last committed height", withState(window, func(s *upgrade.StoredState) { s.Plan.Height = 10 }), b2, 6,
			"upgrade v2 at height 10 is not above the last committed height 10"},
		{"row 6: plan below the last committed height, same app version", withState(current, func(s *upgrade.StoredState) {
			s.Plan = &upgrade.Plan{Name: "v3", Height: 3, Digest: digest(3)}
		}), b2, 6, "is not above the last committed height"},
		{"row 6: too many stored modules", withState(current, func(s *upgrade.StoredState) { s.Modules = tooMany }), b2, 6,
			"the stored version map has 33 modules"},
		{"row 6: too many layout keys", withState(current, func(s *upgrade.StoredState) { s.Layouts = map[string]uint64(tooMany) }), b2, 6,
			"the state reports 33 layout keys"},
		{"row 6: binary with too many modules", current, withBinary(b2, func(b *upgrade.Binary) { b.Modules = tooMany }), 6,
			"the binary declares 33 modules"},
	}
	seen := map[int]bool{}
	for _, r := range rows {
		decision, err := upgrade.Gate(r.state(), r.binary())
		seen[r.row] = true
		if r.row == 1 || r.row == 3 {
			if err != nil {
				t.Fatalf("%s: refused: %v", r.name, err)
			}
			if decision.Row != r.row || decision.Window != (r.row == 3) {
				t.Fatalf("%s: decision %+v, want row %d", r.name, decision, r.row)
			}
			if r.row == 3 && decision.Upgrade.Name != "v2" {
				t.Fatalf("%s: window upgrade %q, want v2", r.name, decision.Upgrade.Name)
			}
			continue
		}
		var refusal *upgrade.RefusalError
		if !errors.As(err, &refusal) {
			t.Fatalf("%s: %v (%+v), want a refusal of row %d", r.name, err, decision, r.row)
		}
		if refusal.Row != r.row || !strings.Contains(refusal.Error(), r.want) {
			t.Fatalf("%s: row %d %q, want row %d containing %q", r.name, refusal.Row, refusal.Error(), r.row, r.want)
		}
		if decision.Row != 0 || decision.Window || decision.Upgrade.Name != "" || decision.Upgrade.Handler != nil {
			t.Fatalf("%s: a refusal returned decision %+v", r.name, decision)
		}
	}
	for want := 1; want <= 6; want++ {
		if !seen[want] {
			t.Fatalf("no row %d in the table", want)
		}
	}
}

// TestGateRefusesBeforeTheFirstBlock: at L = 0 the gate does not decide
// (InitChain rules apply); it returns ErrNoCommittedBlock for any input,
// including one that would start at L = 10.
func TestGateRefusesBeforeTheFirstBlock(t *testing.T) {
	s := current()
	if _, err := upgrade.Gate(s, gateBinary()); err != nil {
		t.Fatalf("control at L = 10: %v", err)
	}
	s.LastHeight = 0
	_, err := upgrade.Gate(s, gateBinary())
	wantKind(t, "L = 0", err, upgrade.ErrNoCommittedBlock, "only after a committed block")
	var refusal *upgrade.RefusalError
	if errors.As(err, &refusal) {
		t.Fatalf("L = 0 returned a gate row: %+v", refusal)
	}
}
