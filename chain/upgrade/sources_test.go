package upgrade_test

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
)

// TestDeterministicSources: the package's non-test code reads no clock,
// randomness, environment, file, process, or network source.
func TestDeterministicSources(t *testing.T) {
	msgkittest.AssertDeterministicSources(t, ".")
}

// goFiles parses the Go files of dir in name order: the non-test files,
// or every file when tests is true.
func goFiles(t *testing.T, dir string, tests bool) map[string]*ast.File {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	out := map[string]*ast.File{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || (!tests && strings.HasSuffix(name, "_test.go")) {
			continue
		}
		file, err := parser.ParseFile(fset, filepath.Join(dir, name), nil, parser.SkipObjectResolution)
		if err != nil {
			t.Fatal(err)
		}
		out[name] = file
	}
	if len(out) == 0 {
		t.Fatalf("no Go files in %s", dir)
	}
	return out
}

// TestPackageImports: the package's non-test code imports no app, module,
// node, command, devnet, test-support, or off-chain package, and no SDK
// x/upgrade or cosmovisor (which would link x/gov, x/staking, and x/authz
// into svnd). Its imports outside the standard library are the SDK types,
// the module manager types, the CometBFT proto types, and msgkit.
func TestPackageImports(t *testing.T) {
	allowed := map[string]bool{
		"github.com/cosmos/cosmos-sdk/types":                      true,
		"github.com/cosmos/cosmos-sdk/types/module":               true,
		"github.com/cometbft/cometbft/proto/tendermint/types":     true,
		"github.com/Maxencejules/shortvideo-network/chain/msgkit": true,
	}
	var seen []string
	for name, file := range goFiles(t, ".", false) {
		for _, spec := range file.Imports {
			path, err := strconv.Unquote(spec.Path.Value)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(path, ".") {
				continue // standard library; TestDeterministicSources checks it
			}
			if !allowed[path] {
				t.Errorf("%s imports %s", name, path)
			}
			seen = append(seen, path)
		}
	}
	sort.Strings(seen)
	if len(seen) == 0 {
		t.Fatal("the scan read no import")
	}
}

// TestNoSkipOrHaltHeight: chain/upgrade, chain/node, and chain/cmd/svnd
// provide no skip-upgrade height and no node-local halt height (plan 11.9):
// no identifier or string literal of their non-test code names the baseapp
// halt options, a halt height or time, or a skip height. Comments may say
// that these do not exist. A control finds Gate in this package.
func TestNoSkipOrHaltHeight(t *testing.T) {
	forbidden := []string{"sethaltheight", "sethalttime", "haltheight", "halttime", "halt-height", "halt_height", "halt-time",
		"halt_time", "skipupgrade", "skip-upgrade", "skip_upgrade", "unsafeskip", "unsafe-skip", "skipheight", "skip-height"}
	check := func(where, text string) {
		lower := strings.ToLower(text)
		for _, word := range forbidden {
			if strings.Contains(lower, word) {
				t.Errorf("%s: %q names %s", where, text, word)
			}
		}
	}
	control := false
	for _, dir := range []string{".", filepath.Join("..", "node"), filepath.Join("..", "cmd", "svnd"), filepath.Join("..", "cmd", "svnd", "cli")} {
		for name, file := range goFiles(t, dir, false) {
			where := filepath.Join(dir, name)
			ast.Inspect(file, func(node ast.Node) bool {
				switch n := node.(type) {
				case *ast.Ident:
					check(where, n.Name)
					if n.Name == "Gate" {
						control = true
					}
				case *ast.BasicLit:
					if n.Kind == token.STRING {
						check(where, n.Value)
					}
				}
				return true
			})
		}
	}
	if !control {
		t.Fatal("the scan did not read gate.go")
	}
}
