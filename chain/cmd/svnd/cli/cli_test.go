package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"testing"
	"time"

	cmtdb "github.com/cometbft/cometbft-db"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/node"
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

// TestStartHandsTheHandlersToTheNodeOnAHome: on a new devnet home, `start`
// of a build whose handler runs app version 2 reaches node.StartWith with
// that handler: the CometBFT handshake's InitChain refuses it, because a
// chain starts at app version 1, and start returns that error before the
// node starts (no port is bound). TestStartPassesUpgradesToTheNode cannot
// show this: its home has no node.json, so start fails before the handlers
// matter. A start that does not return within startRefusalWait has started
// the node (the handler did not reach it): the test fails then, and that
// node runs until the test binary exits.
func TestStartHandsTheHandlersToTheNodeOnAHome(t *testing.T) {
	layout, err := devnet.Init(devnet.Options{Home: t.TempDir(), ChainID: "shortvideo-dev-cli-upgrades"})
	if err != nil {
		t.Fatal(err)
	}
	root := Root(Options{Upgrades: rehearsalUpgrades()})
	root.SetArgs([]string{"start", "--home", layout.Topology.Validators[0].Home})
	root.SetOut(&bytes.Buffer{})
	root.SetErr(&bytes.Buffer{})
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()
	select {
	case err = <-done:
	case <-time.After(startRefusalWait):
		t.Fatalf("start did not return within %s: it started the node instead of refusing the app version 2 handler", startRefusalWait)
	}
	if err == nil || !strings.Contains(err.Error(), "this binary runs app version 2, and a chain starts at app version 1") {
		t.Fatalf("start of a build with the rehearsal handler on a new devnet home: %v", err)
	}
}

// startRefusalWait bounds how long a start that must be refused may run.
const startRefusalWait = 60 * time.Second

// writeGenesis writes home/config/genesis.json with body and returns home.
func writeGenesis(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "config", "genesis.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// TestStartRefusesOtherChainIDs: with Options.ChainIDPrefix set (the
// rehearsal and state check builds set the devnet prefix), start reads the
// chain id of the home's genesis document before the node opens anything
// and refuses one that does not start with the prefix, including one that
// holds the prefix later in the id; a home with the prefix passes that
// check and fails on its missing node.json, as every start does. The chain
// id is read as CometBFT reads it (node.GenesisChainID): a key that differs
// from chain_id only in letter case is not the chain id, a document without
// a chain id is refused, and a state database whose stored genesis document
// names another chain id than the file is refused. A home without a genesis
// file or with a document that does not decode is an error. Without the
// option (the default build) start does not read the genesis chain id
// itself: the same production home fails only on node.json. The help text
// states the restriction only when it is set.
func TestStartRefusesOtherChainIDs(t *testing.T) {
	const prefix = "shortvideo-dev-"
	start := func(opts Options, home string) error {
		root := Root(opts)
		root.SetArgs([]string{"start", "--home", home})
		root.SetOut(&bytes.Buffer{})
		root.SetErr(&bytes.Buffer{})
		return root.Execute()
	}
	restricted := Options{Upgrades: rehearsalUpgrades(), ChainIDPrefix: prefix}
	production := writeGenesis(t, `{"chain_id":"svn-beta-1","app_state":{}}`)
	devnet := writeGenesis(t, `{"chain_id":"shortvideo-dev-x"}`)
	replaced := writeGenesis(t, `{"chain_id":"shortvideo-dev-x"}`)
	state, err := cmtdb.NewDB("state", cmtdb.GoLevelDBBackend, filepath.Join(replaced, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := state.SetSync([]byte(node.StoredGenesisKey), []byte(`{"chain_id":"svn-beta-1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := state.Close(); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name string
		opts Options
		home string
		want string
		not  string
	}{
		{"restricted, production chain id", restricted, production,
			`this build starts only on a chain whose id starts with "shortvideo-dev-"; the genesis of ` + production + ` names chain id "svn-beta-1"`, "node.json"},
		{"restricted, prefix without the dash", restricted, writeGenesis(t, `{"chain_id":"shortvideo-dev"}`), `names chain id "shortvideo-dev"`, "node.json"},
		{"restricted, prefix inside another id", restricted, writeGenesis(t, `{"chain_id":"svn-beta-shortvideo-dev-1"}`), `names chain id "svn-beta-shortvideo-dev-1"`, "node.json"},
		{"restricted, case-variant key", restricted, writeGenesis(t, `{"chain_id":"svn-beta-1","CHAIN_ID":"shortvideo-dev-x"}`), `names chain id "svn-beta-1"`, "node.json"},
		{"restricted, stored genesis of another chain", restricted, replaced, `holds the genesis document of chain id "svn-beta-1", which CometBFT runs`, "node.json"},
		{"restricted, no chain id", restricted, writeGenesis(t, `{}`), "non-empty chain_id", "node.json"},
		{"restricted, devnet chain id", restricted, devnet, "node.json", "starts only"},
		{"restricted, no genesis file", restricted, t.TempDir(), "genesis.json", "node.json"},
		{"restricted, genesis does not decode", restricted, writeGenesis(t, `{"chain_id":`), "genesis ", "node.json"},
		{"unrestricted, production chain id", Options{}, production, "node.json", "starts only"},
		{"unrestricted with handlers, production chain id", Options{Upgrades: rehearsalUpgrades()}, production, "node.json", "starts only"},
	} {
		err := start(c.opts, c.home)
		if err == nil || !strings.Contains(err.Error(), c.want) || strings.Contains(err.Error(), c.not) {
			t.Fatalf("%s: %v; want %q and not %q", c.name, err, c.want, c.not)
		}
	}
	line := "This build starts only on a chain whose id starts with shortvideo-dev-."
	if got := Root(restricted).Long; !strings.Contains(got, line) || !strings.Contains(got, "Compiled upgrade handlers: rehearsal-1 (app version 1 to 2).\n\n"+line+"\n\nPublic launch is disabled.") {
		t.Fatalf("restricted help text:\n%s", got)
	}
	if got := Root(Options{Upgrades: rehearsalUpgrades()}).Long; strings.Contains(got, "starts only") || !strings.Contains(got, ".\n\nPublic launch is disabled.") {
		t.Fatalf("unrestricted help text:\n%s", got)
	}
}
