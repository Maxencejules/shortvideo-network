package cli

import (
	"bytes"
	"runtime/debug"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
)

func rehearsalUpgrades() []upgrade.Upgrade {
	return []upgrade.Upgrade{{
		Name: "rehearsal-1", FromAppVersion: 1, ToAppVersion: 2,
		Handler: func(ctx sdk.Context, from module.VersionMap, migrate upgrade.Migrate) (module.VersionMap, error) {
			return migrate(ctx, from)
		},
	}}
}

// syntheticBuildInfo is a build info as the Go linker records it, with a
// VCS stamp and a replaced dependency.
func syntheticBuildInfo() *debug.BuildInfo {
	return &debug.BuildInfo{
		GoVersion: "go1.26.10",
		Main:      debug.Module{Path: "github.com/Maxencejules/shortvideo-network/chain", Version: "(devel)"},
		Deps: []*debug.Module{
			{Path: "github.com/cometbft/cometbft", Version: "v0.40.0"},
			{Path: "github.com/cosmos/cosmos-sdk", Version: "v0.55.0"},
			{Path: "github.com/cosmos/cosmos-sdk/store/v2", Version: "v2.0.0", Replace: &debug.Module{Path: "../store", Version: "(devel)"}},
			{Path: "github.com/spf13/cobra", Version: "v1.10.1"},
		},
		Settings: []debug.BuildSetting{
			{Key: "-buildmode", Value: "exe"},
			{Key: "vcs", Value: "git"},
			{Key: "vcs.revision", Value: "ff951fc0123456789abcdef0123456789abcdef0"},
			{Key: "vcs.modified", Value: "true"},
		},
	}
}

// runVersion runs the version command of a binary with upgrades and info.
func runVersion(t *testing.T, upgrades []upgrade.Upgrade, info *debug.BuildInfo, ok bool) (string, error) {
	t.Helper()
	cmd := versionCmd(upgrades, func() (*debug.BuildInfo, bool) { return info, ok })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetArgs(nil)
	err := cmd.Execute()
	return out.String(), err
}

// TestVersionCommandFields: `version` prints, one per line, the main
// module version, the Go version, vcs.revision, vcs.modified, the three
// pinned framework modules (with a replacement when there is one), the app
// version P and every module's consensus version as the application
// declares them, and the compiled upgrade handlers ("upgrades none" for
// svnd). A binary with a handler from app version 1 to 2 runs app version
// 2. Without a VCS stamp the vcs fields read unknown; without build info
// the command fails.
func TestVersionCommandFields(t *testing.T) {
	got, err := runVersion(t, nil, syntheticBuildInfo(), true)
	if err != nil {
		t.Fatal(err)
	}
	modules := `module auth 7
module authority 2
module content 2
module evidence 2
module leases 2
module policy 2
module providers 2
module svn 1
`
	want := `svnd (devel)
go go1.26.10
vcs.revision ff951fc0123456789abcdef0123456789abcdef0
vcs.modified true
github.com/cosmos/cosmos-sdk v0.55.0
github.com/cosmos/cosmos-sdk/store/v2 v2.0.0 => ../store (devel)
github.com/cometbft/cometbft v0.40.0
app_version 1
` + modules + `upgrades none
`
	if got != want {
		t.Fatalf("version output:\n%s\nwant:\n%s", got, want)
	}

	got, err = runVersion(t, rehearsalUpgrades(), syntheticBuildInfo(), true)
	if err != nil || !strings.HasSuffix(got, "github.com/cometbft/cometbft v0.40.0\napp_version 2\n"+modules+"upgrade rehearsal-1 from_app_version 1 to_app_version 2\n") {
		t.Fatalf("version with a handler: %v\n%s", err, got)
	}
	if strings.Contains(got, "upgrades none") {
		t.Fatalf("version with a handler says none:\n%s", got)
	}

	unstamped := syntheticBuildInfo()
	unstamped.Settings = unstamped.Settings[:1]
	unstamped.Deps = unstamped.Deps[:1]
	got, err = runVersion(t, nil, unstamped, true)
	if err != nil || !strings.Contains(got, "vcs.revision unknown\nvcs.modified unknown\n") ||
		!strings.Contains(got, "github.com/cosmos/cosmos-sdk unknown\n") {
		t.Fatalf("version without a VCS stamp: %v\n%s", err, got)
	}

	if _, err := runVersion(t, nil, nil, false); err == nil || !strings.Contains(err.Error(), "build info is unavailable") {
		t.Fatalf("version without build info: %v", err)
	}
}

// TestRootHelpText: the root help text describes the modules of this tree
// (x/evidence exists; policy has no message service) and lists the
// compiled upgrade handlers: none for svnd, each handler for a binary that
// passes them.
func TestRootHelpText(t *testing.T) {
	plain := Root(Options{}).Long
	for _, want := range []string{
		"x/evidence\nrecords signed audit attestations of held leases",
		"Policy has no message service",
		"Compiled upgrade handlers: none.",
		"Public launch is disabled. This binary is not a release.",
	} {
		if !strings.Contains(plain, want) {
			t.Fatalf("help text has no %q:\n%s", want, plain)
		}
	}
	if strings.Contains(plain, "Goals 09-10") {
		t.Fatalf("help text still says evidence and policy are future goals:\n%s", plain)
	}
	if got := Root(Options{Upgrades: rehearsalUpgrades()}).Long; !strings.Contains(got, "Compiled upgrade handlers: rehearsal-1 (app version 1 to 2).") {
		t.Fatalf("help text with a handler:\n%s", got)
	}
}

// TestStartPassesUpgradesToTheNode: `start` hands the binary's handlers to
// the node, which reads the home first with or without handlers, so both
// fail on the missing node.json (chain/node TestStartWithPassesUpgradeHandlers
// shows the handlers reach the application). Root keeps its own copy of the
// handler list: version lists the handlers as Root received them after the
// caller renamed one in its own slice.
func TestStartPassesUpgradesToTheNode(t *testing.T) {
	home := t.TempDir()
	upgrades := rehearsalUpgrades()
	root := Root(Options{Upgrades: upgrades})
	upgrades[0].Name = "changed"
	root.SetArgs([]string{"start", "--home", home})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	if err := root.Execute(); err == nil || !strings.Contains(err.Error(), "node.json") {
		t.Fatalf("start with a handler: %v", err)
	}
	plain := Root(Options{})
	plain.SetArgs([]string{"start", "--home", home})
	plain.SetOut(&bytes.Buffer{})
	plain.SetErr(&bytes.Buffer{})
	if err := plain.Execute(); err == nil || !strings.Contains(err.Error(), "node.json") {
		t.Fatalf("start without handlers: %v", err)
	}
	var out bytes.Buffer
	listed := rehearsalUpgrades()
	versions := Root(Options{Upgrades: listed})
	listed[0].Name = "changed"
	versions.SetArgs([]string{"version"})
	versions.SetOut(&out)
	if err := versions.Execute(); err != nil {
		t.Fatalf("version through Root: %v", err)
	}
	if !strings.Contains(out.String(), "upgrade rehearsal-1 from_app_version 1 to_app_version 2") || strings.Contains(out.String(), "changed") {
		t.Fatalf("version through Root does not list the handlers Root received:\n%s", out.String())
	}
}
