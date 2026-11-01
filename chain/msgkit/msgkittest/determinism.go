package msgkittest

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// forbiddenImports read the clock indirectly, randomness, the environment,
// files, processes, or the network. Any "net/..." path is forbidden too.
var forbiddenImports = map[string]bool{
	"math/rand":    true,
	"math/rand/v2": true,
	"crypto/rand":  true,
	"os":           true,
	"os/exec":      true,
	"net":          true,
	"net/http":     true,
	"io/ioutil":    true,
	"syscall":      true,
}

// forbiddenTimeCalls read the local clock.
var forbiddenTimeCalls = map[string]bool{
	"Now":   true,
	"Since": true,
	"Until": true,
}

// forbiddenTypes are floating-point and complex types.
var forbiddenTypes = map[string]bool{
	"float32":    true,
	"float64":    true,
	"complex64":  true,
	"complex128": true,
}

// AssertDeterministicSources fails t when a non-test Go file directly in
// dir (not its subdirectories) imports a nondeterministic package, calls
// time.Now, time.Since, or time.Until, dot-imports "time", or names a
// floating-point or complex type. A directory without non-test Go files
// fails too, so a wrong path cannot pass silently.
func AssertDeterministicSources(t testing.TB, dir string) {
	t.Helper()
	findings, scanned, err := scanDeterministic(dir)
	if err != nil {
		t.Fatalf("determinism scan of %s: %v", dir, err)
	}
	if scanned == 0 {
		t.Fatalf("determinism scan of %s: no non-test Go files", dir)
	}
	for _, finding := range findings {
		t.Errorf("nondeterministic source: %s", finding)
	}
}

// scanDeterministic returns the findings in file and position order and the
// number of files scanned.
func scanDeterministic(dir string) ([]string, int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, 0, err
	}
	var files []string
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	fset := token.NewFileSet()
	var findings []string
	for _, path := range files {
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return nil, 0, err
		}
		findings = append(findings, scanFile(fset, file)...)
	}
	return findings, len(files), nil
}

func scanFile(fset *token.FileSet, file *ast.File) []string {
	var findings []string
	report := func(pos token.Pos, format string, args ...any) {
		findings = append(findings, fmt.Sprintf("%s: %s", fset.Position(pos), fmt.Sprintf(format, args...)))
	}
	timeNames := map[string]bool{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			report(spec.Pos(), "unreadable import %s", spec.Path.Value)
			continue
		}
		if forbiddenImports[path] || strings.HasPrefix(path, "net/") {
			report(spec.Pos(), "imports %q", path)
		}
		if path != "time" {
			continue
		}
		switch {
		case spec.Name == nil:
			timeNames["time"] = true
		case spec.Name.Name == ".":
			report(spec.Pos(), `dot-imports "time"`)
		case spec.Name.Name != "_":
			timeNames[spec.Name.Name] = true
		}
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			if pkg, ok := typed.X.(*ast.Ident); ok && timeNames[pkg.Name] && forbiddenTimeCalls[typed.Sel.Name] {
				report(typed.Pos(), "uses time.%s", typed.Sel.Name)
			}
		case *ast.Ident:
			if forbiddenTypes[typed.Name] {
				report(typed.Pos(), "uses %s", typed.Name)
			}
		}
		return true
	})
	return findings
}
