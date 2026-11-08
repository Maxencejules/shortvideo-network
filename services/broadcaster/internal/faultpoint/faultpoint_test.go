package faultpoint

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// moduleRoot returns the directory of services/broadcaster/go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		data, err := os.ReadFile(filepath.Join(dir, "go.mod"))
		if err == nil {
			if !strings.Contains(string(data), "module github.com/Maxencejules/shortvideo-network/services/broadcaster\n") {
				t.Fatalf("%s/go.mod is not the broadcaster module", dir)
			}
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test directory")
		}
		dir = parent
	}
}

// faultpointSymbols builds one target and returns the symbols of the result
// that belong to this package. target is a package path relative to the
// module root; test builds the package's test binary (go test -c),
// otherwise the package's command (go build).
func faultpointSymbols(t *testing.T, goTool, root, target string, test bool, tags string) []string {
	t.Helper()
	out := filepath.Join(t.TempDir(), "bin")
	args := []string{"build"}
	if test {
		args = []string{"test", "-c"}
	}
	if tags != "" {
		args = append(args, "-tags", tags)
	}
	args = append(args, "-o", out, target)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, goTool, args...)
	build.Dir = root
	if msg, err := build.CombinedOutput(); err != nil {
		t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, msg)
	}
	nm := exec.CommandContext(ctx, goTool, "tool", "nm", out)
	var stdout, stderr bytes.Buffer
	nm.Stdout, nm.Stderr = &stdout, &stderr
	if err := nm.Run(); err != nil {
		t.Fatalf("go tool nm %s: %v\n%s", target, err, stderr.String())
	}
	var syms []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		if strings.Contains(line, "services/broadcaster/internal/faultpoint.") {
			syms = append(syms, strings.TrimSpace(line))
		}
	}
	return syms
}

// TestDefaultBuildHasNoFaultPoints checks plan 13.6: a binary built without
// the svn_faultpoint tag links no symbol of this package, so no fault point
// can fire in it; the same target built with the tag does link them, so the
// symbol scan is not vacuous. The targets are the worker's test binary,
// which links every worker fault point call site, and the svn-broadcaster
// command, which links the signer's after-decode-before-reply point.
func TestDefaultBuildHasNoFaultPoints(t *testing.T) {
	goTool, err := exec.LookPath("go")
	if err != nil {
		t.Fatalf("the go command is needed to build the binaries this test inspects: %v", err)
	}
	root := moduleRoot(t)
	type target struct {
		path string
		test bool
	}
	if _, err := os.Stat(filepath.Join(root, "cmd", "svn-broadcaster", "main.go")); err != nil {
		t.Fatalf("the svn-broadcaster command this test inspects is missing: %v", err)
	}
	targets := []target{{"./internal/worker", true}, {"./cmd/svn-broadcaster", false}}
	for _, tg := range targets {
		if syms := faultpointSymbols(t, goTool, root, tg.path, tg.test, ""); len(syms) != 0 {
			t.Errorf("default build of %s links fault point symbols: %v", tg.path, syms)
		}
		syms := faultpointSymbols(t, goTool, root, tg.path, tg.test, "svn_faultpoint")
		if len(syms) == 0 {
			t.Errorf("svn_faultpoint build of %s links no fault point symbol, so the default-build scan proves nothing", tg.path)
		}
		t.Logf("%s: svn_faultpoint build links %d fault point symbols, default build none", tg.path, len(syms))
	}
}
