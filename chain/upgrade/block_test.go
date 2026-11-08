package upgrade_test

import (
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

// TestCheckBlock: the per-block rule (plan 11.5) at, below, and above the
// plan height, with and without a handler, for an old binary (app version
// 1, no handler) and an upgrade binary (app version 2, handler 1 -> 2).
func TestCheckBlock(t *testing.T) {
	b1 := upgrade.Binary{AppVersion: 1, Modules: vm(gateOldVM)}
	b2 := gateBinary()
	plan := &upgrade.Plan{Name: "v2", Height: 10, Digest: digest(9)}
	digestHex := strings.Repeat("09", 32)

	type row struct {
		name   string
		height uint64
		app    uint64
		plan   *upgrade.Plan
		binary upgrade.Binary
		apply  bool
		kind   error
		want   string
	}
	withUpgrades := func(b upgrade.Binary, ups ...upgrade.Upgrade) upgrade.Binary {
		b.Upgrades = ups
		return b
	}
	rows := []row{
		{"no plan, matching app version", 5, 1, nil, b1, false, nil, ""},
		{"plan above, matching app version (effective-1)", 9, 1, plan, b1, false, nil, ""},
		{"no plan, upgraded app version", 11, 2, nil, b2, false, nil, ""},
		{"height 0", 0, 1, nil, b1, false, upgrade.ErrIntegrity, "block height 0"},
		{"old binary at the plan height", 10, 1, plan, b1, false, upgrade.ErrUpgradeNeeded,
			`UPGRADE "v2" NEEDED at height 10 release_digest=` + digestHex},
		{"upgrade binary at the plan height (effective)", 10, 1, plan, b2, true, nil, ""},
		{"upgrade binary below the plan height", 9, 1, plan, b2, false, upgrade.ErrWrongBinary,
			"stored app version 1, binary 2: wrong binary for height 9"},
		{"upgrade binary replaying from genesis", 1, 1, plan, b2, false, upgrade.ErrWrongBinary,
			"stored app version 1, binary 2: wrong binary for height 1"},
		{"old binary above the plan height on upgraded state", 11, 2, nil, b1, false, upgrade.ErrWrongBinary,
			"stored app version 2, binary 1: wrong binary for height 11"},
		{"upgrade binary with no plan on old state", 5, 1, nil, b2, false, upgrade.ErrWrongBinary,
			"stored app version 1, binary 2: wrong binary for height 5"},
		{"plan below the block height", 11, 1, plan, b2, false, upgrade.ErrIntegrity,
			"upgrade v2 at height 10 is below block height 11"},
		{"handler from another app version", 10, 1, plan, withUpgrades(b2, up("v2", 0, 2)), false, upgrade.ErrWrongBinary,
			"handler for upgrade v2 goes from app version 0 to 2; stored app version 1, binary 2: wrong binary for height 10"},
		{"handler to another app version", 10, 1, plan, withUpgrades(b2, up("v2", 1, 3)), false, upgrade.ErrWrongBinary,
			"handler for upgrade v2 goes from app version 1 to 3"},
		{"stored app version already upgraded at the plan height", 10, 2, plan, b2, false, upgrade.ErrWrongBinary,
			"stored app version 2, binary 2: wrong binary for height 10"},
		{"two handlers of the plan's name", 10, 1, plan, withUpgrades(b2, up("v2", 1, 2), up("v2", 1, 2)), false, upgrade.ErrWrongBinary,
			"binary declares 2 handlers named v2"},
		{"handler of another name only", 10, 1, plan, withUpgrades(b2, up("v2.other", 1, 2)), false, upgrade.ErrUpgradeNeeded,
			`UPGRADE "v2" NEEDED at height 10`},
	}
	for _, r := range rows {
		got, apply, err := upgrade.CheckBlock(r.height, r.app, r.plan, r.binary)
		if r.kind != nil {
			wantKind(t, r.name, err, r.kind, r.want)
			if apply || got.Name != "" {
				t.Fatalf("%s: an error returned an upgrade (%q, %t)", r.name, got.Name, apply)
			}
			continue
		}
		if err != nil {
			t.Fatalf("%s: %v", r.name, err)
		}
		if apply != r.apply {
			t.Fatalf("%s: apply %t, want %t", r.name, apply, r.apply)
		}
		if apply && (got.Name != "v2" || got.Handler == nil) {
			t.Fatalf("%s: returned %+v", r.name, got)
		}
		if !apply && got.Name != "" {
			t.Fatalf("%s: no upgrade to apply but returned %q", r.name, got.Name)
		}
	}
}
