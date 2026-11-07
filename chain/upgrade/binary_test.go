package upgrade_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

var appModules = module.VersionMap{"auth": 7, "authority": 2, "leases": 2, "policy": 2, "svn": 1}

// TestBinaryAcceptsValidDeclarations: an app version 1 binary without
// upgrades, and an app version 2 binary with one or two 1 -> 2 upgrades.
func TestBinaryAcceptsValidDeclarations(t *testing.T) {
	for _, b := range []upgrade.Binary{
		{AppVersion: 1, Modules: vm(appModules)},
		{AppVersion: 2, Modules: vm(appModules), Upgrades: []upgrade.Upgrade{up("rehearsal-1", 1, 2)}},
		{AppVersion: 2, Modules: vm(appModules), Upgrades: []upgrade.Upgrade{up("v2", 1, 2), up("v2.fix", 1, 2)}},
	} {
		if err := b.Validate(); err != nil {
			t.Fatalf("%+v: %v", b, err)
		}
	}
}

// TestBinaryRefusesBadUpgradeList: every declaration Validate refuses, each
// with ErrInvalidBinary and its reason. Each row differs from a valid app
// version 2 binary in one field.
func TestBinaryRefusesBadUpgradeList(t *testing.T) {
	tooMany := module.VersionMap{}
	for i := 0; i <= upgrade.MaxModules; i++ {
		tooMany[fmt.Sprintf("m%02d", i)] = 1
	}
	var nineUpgrades []upgrade.Upgrade
	for i := 0; i <= upgrade.MaxUpgrades; i++ {
		nineUpgrades = append(nineUpgrades, up(fmt.Sprintf("u%d", i), 1, 2))
	}
	valid := func() upgrade.Binary {
		return upgrade.Binary{AppVersion: 2, Modules: vm(appModules), Upgrades: []upgrade.Upgrade{up("v2", 1, 2)}}
	}
	if err := valid().Validate(); err != nil {
		t.Fatalf("control: %v", err)
	}
	rows := []struct {
		name string
		edit func(*upgrade.Binary)
		want string
	}{
		{"app version 0", func(b *upgrade.Binary) { b.AppVersion = 0; b.Upgrades = nil }, "binary app version is 0"},
		{"no module", func(b *upgrade.Binary) { b.Modules = nil }, "declares 0 modules"},
		{"too many modules", func(b *upgrade.Binary) { b.Modules = tooMany }, "declares 33 modules"},
		{"module name outside the id pattern", func(b *upgrade.Binary) { b.Modules["Policy"] = 2 }, `module name "Policy"`},
		{"module version 0", func(b *upgrade.Binary) { b.Modules["svn"] = 0 }, "module svn has consensus version 0"},
		{"too many upgrades", func(b *upgrade.Binary) { b.Upgrades = nineUpgrades }, "declares 9 upgrades"},
		{"empty upgrade name", func(b *upgrade.Binary) { b.Upgrades[0].Name = "" }, `name ""`},
		{"upgrade name outside the id pattern", func(b *upgrade.Binary) { b.Upgrades[0].Name = "V2" }, `name "V2"`},
		{"upgrade name too long", func(b *upgrade.Binary) { b.Upgrades[0].Name = strings.Repeat("a", 65) }, "outside the id pattern"},
		{"duplicate upgrade name", func(b *upgrade.Binary) { b.Upgrades = append(b.Upgrades, up("v2", 1, 2)) }, "upgrade v2 is declared twice"},
		{"nil handler", func(b *upgrade.Binary) { b.Upgrades[0].Handler = nil }, "upgrade v2 has no handler"},
		{"to above the binary", func(b *upgrade.Binary) { b.Upgrades[0].ToAppVersion = 3 }, "goes from app version 1 to 3"},
		{"to below the binary", func(b *upgrade.Binary) { b.Upgrades[0].ToAppVersion = 1 }, "goes from app version 1 to 1"},
		{"from two behind", func(b *upgrade.Binary) { b.Upgrades[0].FromAppVersion = 0 }, "goes from app version 0 to 2"},
		{"from the binary's own version", func(b *upgrade.Binary) { b.Upgrades[0].FromAppVersion = 2 }, "goes from app version 2 to 2"},
		{"upgrade on an app version 1 binary", func(b *upgrade.Binary) {
			b.AppVersion = 1
			b.Upgrades = []upgrade.Upgrade{up("v1", 0, 1)}
		}, "goes from app version 0 to 1"},
	}
	for _, row := range rows {
		b := valid()
		row.edit(&b)
		wantKind(t, row.name, b.Validate(), upgrade.ErrInvalidBinary, row.want)
	}
}
