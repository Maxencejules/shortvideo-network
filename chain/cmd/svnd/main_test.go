package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/app"
)

// buildDir holds the svnd builds of this test binary; TestMain removes it.
var buildDir string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "svnd-builds-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	buildDir = dir
	code := m.Run()
	_ = os.RemoveAll(dir)
	os.Exit(code)
}

// builds are the svnd builds of this test binary by build tag ("" is the
// default build), each built once.
var (
	buildMu sync.Mutex
	builds  = map[string]string{}
)

// svndBuild builds this main package with build tag tag ("" for none)
// once per test binary, with the module cache only, and returns the
// binary's path.
func svndBuild(t *testing.T, tag string) string {
	t.Helper()
	buildMu.Lock()
	defer buildMu.Unlock()
	if bin, ok := builds[tag]; ok {
		return bin
	}
	bin := filepath.Join(buildDir, "svnd")
	args := []string{"build", "-o"}
	if tag != "" {
		bin += "-" + tag
		args = append(args, bin, "-tags", tag, ".")
	} else {
		args = append(args, bin, ".")
	}
	build := exec.Command("go", args...)
	build.Env = append(os.Environ(), "GOPROXY=off")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build with tag %q: %v\n%s", tag, err, out)
	}
	builds[tag] = bin
	return bin
}

// run runs bin with args and returns its combined output and error.
func run(bin string, args ...string) (string, error) {
	out, err := exec.Command(bin, args...).CombinedOutput()
	return string(out), err
}

// buildInfoLines reads `go version -m <binary>`: the Go version, the main
// module version, the build settings, and each dependency's version (with
// its replacement, if any), as the go tool reports them independently of
// svnd's own code.
type buildInfoLines struct {
	goVersion string
	mainMod   string
	settings  map[string]string
	deps      map[string]string
}

func readBuildInfoLines(t *testing.T, bin string) buildInfoLines {
	t.Helper()
	out, err := exec.Command("go", "version", "-m", bin).CombinedOutput()
	if err != nil {
		t.Fatalf("go version -m: %v\n%s", err, out)
	}
	info := buildInfoLines{settings: map[string]string{}, deps: map[string]string{}}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	_, goVersion, ok := strings.Cut(lines[0], ": ")
	if !ok {
		t.Fatalf("first line %q", lines[0])
	}
	info.goVersion = goVersion
	lastDep := ""
	for _, line := range lines[1:] {
		fields := strings.Split(strings.TrimPrefix(line, "\t"), "\t")
		switch fields[0] {
		case "mod":
			info.mainMod = fields[2]
		case "dep":
			lastDep = fields[1]
			info.deps[lastDep] = fields[2]
		case "=>":
			info.deps[lastDep] += " => " + fields[1] + " " + fields[2]
		case "build":
			if key, value, found := strings.Cut(fields[1], "="); found {
				info.settings[key] = value
			}
		}
	}
	return info
}

// TestSvndVersionMatchesBuildInfo builds svnd and checks `svnd version`
// line for line against what `go version -m` reads from the same binary:
// the main module version, the Go version, vcs.revision and vcs.modified
// ("unknown" when the build has no VCS stamp), the three pinned framework
// modules, then the app version and the module versions the application
// declares for svnd (app.DeclaredBinary), and "upgrades none" (svnd
// compiles no upgrade handler). The help text says the same about handlers.
func TestSvndVersionMatchesBuildInfo(t *testing.T) {
	bin := svndBuild(t, "")
	info := readBuildInfoLines(t, bin)
	known := func(v string) string {
		if v == "" {
			return "unknown"
		}
		return v
	}
	want := strings.Join([]string{
		"svnd " + info.mainMod,
		"go " + info.goVersion,
		"vcs.revision " + known(info.settings["vcs.revision"]),
		"vcs.modified " + known(info.settings["vcs.modified"]),
		"github.com/cosmos/cosmos-sdk " + info.deps["github.com/cosmos/cosmos-sdk"],
		"github.com/cosmos/cosmos-sdk/store/v2 " + info.deps["github.com/cosmos/cosmos-sdk/store/v2"],
		"github.com/cometbft/cometbft " + info.deps["github.com/cometbft/cometbft"],
		declaredLines(t),
		"upgrades none",
	}, "\n") + "\n"
	if info.deps["github.com/cosmos/cosmos-sdk"] == "" || info.deps["github.com/cometbft/cometbft"] == "" || info.deps["github.com/cosmos/cosmos-sdk/store/v2"] == "" {
		t.Fatalf("go version -m lists no framework pins: %+v", info.deps)
	}
	got, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		t.Fatalf("svnd version: %v\n%s", err, got)
	}
	if string(got) != want {
		t.Fatalf("svnd version:\n%s\nwant:\n%s", got, want)
	}
	help, err := exec.Command(bin, "--help").CombinedOutput()
	if err != nil || !strings.Contains(string(help), "Compiled upgrade handlers: none.") {
		t.Fatalf("svnd --help: %v\n%s", err, help)
	}
}

// declaredLines are the app_version line and the module lines of what the
// application declares for a binary without upgrade handlers
// (app.DeclaredBinary), which svnd version must print: app version 1 and
// every module in name order.
func declaredLines(t *testing.T) string {
	t.Helper()
	binary, err := app.DeclaredBinary(nil)
	if err != nil {
		t.Fatal(err)
	}
	if binary.AppVersion != 1 || len(binary.Modules) != 8 {
		t.Fatalf("declared binary: app version %d, %d modules", binary.AppVersion, len(binary.Modules))
	}
	names := make([]string, 0, len(binary.Modules))
	for name := range binary.Modules {
		names = append(names, name)
	}
	sort.Strings(names)
	lines := []string{fmt.Sprintf("app_version %d", binary.AppVersion)}
	for _, name := range names {
		lines = append(lines, fmt.Sprintf("module %s %d", name, binary.Modules[name]))
	}
	return strings.Join(lines, "\n")
}

// TestDefaultBuildCompilesNoRehearsal: the default build (no build tag, the
// release build) records no build tag, its `version` lists app version 1 and
// "upgrades none" and no upgrade line, its help text states no compiled
// handler and no chain id restriction and lists no statecheck command, and
// `svnd statecheck` is an unknown command. TestLinkedBinaryOmitsTestHooks
// (chain/app) shows it links no chain/internal or test-support symbol.
func TestDefaultBuildCompilesNoRehearsal(t *testing.T) {
	bin := svndBuild(t, "")
	if tags, found := readBuildInfoLines(t, bin).settings["-tags"]; found {
		t.Fatalf("the default build records build tags %q", tags)
	}
	version, err := run(bin, "version")
	if err != nil {
		t.Fatalf("version: %v\n%s", err, version)
	}
	if !strings.Contains(version, "\napp_version 1\n") || !strings.HasSuffix(version, "\nupgrades none\n") || strings.Contains(version, "\nupgrade ") {
		t.Fatalf("default svnd version:\n%s", version)
	}
	help, err := run(bin, "--help")
	if err != nil || !strings.Contains(help, "Compiled upgrade handlers: none.") || strings.Contains(help, "starts only") || strings.Contains(help, "statecheck") {
		t.Fatalf("default svnd --help: %v\n%s", err, help)
	}
	if !strings.Contains(help, "Available Commands:") || !strings.Contains(help, "  version ") {
		t.Fatalf("default svnd --help lists no commands:\n%s", help)
	}
	if out, err := run(bin, "statecheck"); err == nil || !strings.Contains(out, `unknown command "statecheck"`) {
		t.Fatalf("default svnd statecheck: %v\n%s", err, out)
	}
}

// TestRehearsalBuildsDeclareTheRehearsalUpgrade: the build with tag
// svn_rehearsal (B2) and the build with tag svn_rehearsal_bad (B2-bad) each
// record their tag in the build information, and their `version` output is
// the default build's line for line except two lines: app version 2, and the
// upgrade line "upgrade rehearsal-1 from_app_version 1 to_app_version 2" in
// place of "upgrades none" (B2-bad declares the same plan name as B2). Their
// help text names the handler and states that the build starts only on a
// shortvideo-dev- chain.
func TestRehearsalBuildsDeclareTheRehearsalUpgrade(t *testing.T) {
	plain, err := run(svndBuild(t, ""), "version")
	if err != nil {
		t.Fatalf("default version: %v\n%s", err, plain)
	}
	want := strings.Replace(strings.Replace(plain, "\napp_version 1\n", "\napp_version 2\n", 1),
		"\nupgrades none\n", "\nupgrade rehearsal-1 from_app_version 1 to_app_version 2\n", 1)
	if want == plain {
		t.Fatalf("default version output has no line to replace:\n%s", plain)
	}
	for _, tag := range []string{"svn_rehearsal", "svn_rehearsal_bad"} {
		bin := svndBuild(t, tag)
		if got := readBuildInfoLines(t, bin).settings["-tags"]; got != tag {
			t.Fatalf("%s build records build tags %q", tag, got)
		}
		version, err := run(bin, "version")
		if err != nil || version != want {
			t.Fatalf("%s version: %v\n%s\nwant:\n%s", tag, err, version, want)
		}
		help, err := run(bin, "--help")
		if err != nil || !strings.Contains(help, "Compiled upgrade handlers: rehearsal-1 (app version 1 to 2).") ||
			!strings.Contains(help, "This build starts only on a chain whose id starts with shortvideo-dev-.") {
			t.Fatalf("%s --help: %v\n%s", tag, err, help)
		}
	}
}

// TestRehearsalBuildsRefuseNonDevChains: B2, B2-bad, and the state check
// build (tag svn_statecheck, which links the test-support checkers) refuse
// `start` on a home whose genesis names a chain id that does not start with
// shortvideo-dev- (exit status 1, before the node reads node.json or opens
// a database), including an id that holds the prefix later, and pass that
// check on a shortvideo-dev- home, where they fail on its missing node.json
// as every start does; their help text states the restriction. The default
// build has no such check: on the same production home it fails on
// node.json. No process here binds a port.
func TestRehearsalBuildsRefuseNonDevChains(t *testing.T) {
	writeHome := func(chainID string) string {
		home := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, "config"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "config", "genesis.json"), []byte(`{"chain_id":"`+chainID+`"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		return home
	}
	production, dev, inside := writeHome("svn-beta-1"), writeHome("shortvideo-dev-rehearsal"), writeHome("svn-shortvideo-dev-1")
	refusal := `this build starts only on a chain whose id starts with "shortvideo-dev-"; the genesis of ` + production + ` names chain id "svn-beta-1"`
	start := func(bin, home string) (string, int) {
		out, err := run(bin, "start", "--home", home)
		code := 0
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else if err != nil {
			t.Fatalf("start: %v", err)
		}
		return out, code
	}
	for _, tag := range []string{"svn_rehearsal", "svn_rehearsal_bad", "svn_statecheck"} {
		bin := svndBuild(t, tag)
		if out, code := start(bin, production); code != 1 || !strings.Contains(out, refusal) || strings.Contains(out, "node.json") {
			t.Fatalf("%s on a production chain id: exit %d\n%s", tag, code, out)
		}
		if out, code := start(bin, inside); code != 1 || !strings.Contains(out, `names chain id "svn-shortvideo-dev-1"`) || strings.Contains(out, "node.json") {
			t.Fatalf("%s on a chain id that holds the prefix later: exit %d\n%s", tag, code, out)
		}
		if help, err := run(bin, "--help"); err != nil || !strings.Contains(help, "This build starts only on a chain whose id starts with shortvideo-dev-.") {
			t.Fatalf("%s --help: %v\n%s", tag, err, help)
		}
		if out, code := start(bin, dev); code != 1 || strings.Contains(out, "starts only") || !strings.Contains(out, "node.json") {
			t.Fatalf("%s on a devnet chain id: exit %d\n%s", tag, code, out)
		}
	}
	if out, code := start(svndBuild(t, ""), production); code != 1 || strings.Contains(out, "starts only") || !strings.Contains(out, "node.json") {
		t.Fatalf("default svnd on a production chain id: exit %d\n%s", code, out)
	}
}
