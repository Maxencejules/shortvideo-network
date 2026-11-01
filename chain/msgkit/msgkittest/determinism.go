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

// forbiddenTimeCalls read the local clock or act on it (timers, tickers,
// sleeps).
var forbiddenTimeCalls = map[string]bool{
	"Now":       true,
	"Since":     true,
	"Until":     true,
	"Tick":      true,
	"After":     true,
	"AfterFunc": true,
	"NewTimer":  true,
	"NewTicker": true,
	"Sleep":     true,
}

// allowedMathNames are the integer constants of package math. Every other
// name in math (Sqrt, Pow, Inf, Float64bits, Pi, Max, ...) is
// floating-point.
func allowedMathName(name string) bool {
	return strings.HasPrefix(name, "MaxInt") || strings.HasPrefix(name, "MinInt") || strings.HasPrefix(name, "MaxUint")
}

// forbiddenFloatCalls are floating-point helpers outside package math,
// keyed by import path.
var forbiddenFloatCalls = map[string]map[string]bool{
	"math/big": {"Float": true, "NewFloat": true, "ParseFloat": true},
	"strconv":  {"ParseFloat": true, "FormatFloat": true, "AppendFloat": true},
}

// forbiddenTypes are floating-point and complex types.
var forbiddenTypes = map[string]bool{
	"float32":    true,
	"float64":    true,
	"complex64":  true,
	"complex128": true,
}

// AssertDeterministicSources fails t when a non-test Go file directly in
// dir (not its subdirectories):
//
//   - imports a nondeterministic package (randomness, os, processes,
//     syscalls, any net package)
//   - calls a clock or timer function of package time (Now, Since, Until,
//     Tick, After, AfterFunc, NewTimer, NewTicker, Sleep)
//   - dot-imports "time" or "math"
//   - names a floating-point or complex type (float32, float64, complex64,
//     complex128), which also covers conversions such as float64(x)
//   - writes a floating-point or imaginary literal (0.75, 1e3, 2i)
//   - uses any name of package math other than its integer constants
//     (MaxInt*, MinInt*, MaxUint*)
//   - uses big.Float, big.NewFloat, big.ParseFloat, strconv.ParseFloat,
//     strconv.FormatFloat, or strconv.AppendFloat
//
// It is a syntactic scan: a float that arrives through another package's
// API (for example encoding/json decoding a number into an interface) is
// not visible to it. A directory without non-test Go files fails too, so a
// wrong path cannot pass silently.
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
	// local package name -> import path, for the packages the scan reads.
	watched := map[string]string{}
	for _, spec := range file.Imports {
		path, err := strconv.Unquote(spec.Path.Value)
		if err != nil {
			report(spec.Pos(), "unreadable import %s", spec.Path.Value)
			continue
		}
		if forbiddenImports[path] || strings.HasPrefix(path, "net/") {
			report(spec.Pos(), "imports %q", path)
		}
		if path != "time" && path != "math" && forbiddenFloatCalls[path] == nil {
			continue
		}
		local := filepath.Base(path)
		switch {
		case spec.Name == nil:
		case spec.Name.Name == ".":
			if path == "time" || path == "math" {
				report(spec.Pos(), "dot-imports %q", path)
			}
			continue
		case spec.Name.Name == "_":
			continue
		default:
			local = spec.Name.Name
		}
		watched[local] = path
	}
	ast.Inspect(file, func(node ast.Node) bool {
		switch typed := node.(type) {
		case *ast.SelectorExpr:
			pkg, ok := typed.X.(*ast.Ident)
			if !ok {
				return true
			}
			name := typed.Sel.Name
			switch path := watched[pkg.Name]; {
			case path == "time" && forbiddenTimeCalls[name]:
				report(typed.Pos(), "uses time.%s", name)
			case path == "math" && !allowedMathName(name):
				report(typed.Pos(), "uses floating-point math.%s", name)
			case forbiddenFloatCalls[path][name]:
				report(typed.Pos(), "uses %s.%s", path, name)
			}
		case *ast.Ident:
			if forbiddenTypes[typed.Name] {
				report(typed.Pos(), "uses %s", typed.Name)
			}
		case *ast.BasicLit:
			if typed.Kind == token.FLOAT || typed.Kind == token.IMAG {
				report(typed.Pos(), "uses the literal %s", typed.Value)
			}
		}
		return true
	})
	return findings
}
