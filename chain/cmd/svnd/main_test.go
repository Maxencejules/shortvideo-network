package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/app"
)

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
	bin := filepath.Join(t.TempDir(), "svnd")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Env = os.Environ()
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
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
