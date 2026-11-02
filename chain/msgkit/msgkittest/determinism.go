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
// files, processes, the host, or the network. Every path under one of
// forbiddenImportTrees is forbidden too.
var forbiddenImports = map[string]bool{
	"math/rand":    true,
	"math/rand/v2": true,
	"crypto/rand":  true,
	"hash/maphash": true, // random per-process seed
	"io/ioutil":    true,
	"io/fs":        true,
	"plugin":       true,
}

// forbiddenImportTrees are import paths forbidden with every path below
// them: os (environment, files, processes, user, signals), net, runtime
// (NumCPU, GOOS, scheduler state), and syscall.
var forbiddenImportTrees = []string{"os", "net", "runtime", "syscall"}

// forbiddenFileCalls are the functions of path/filepath that read the file
// system or the working directory. The rest of path/filepath is pure path
// arithmetic.
var forbiddenFileCalls = map[string]bool{
	"Abs":          true,
	"EvalSymlinks": true,
	"Glob":         true,
	"Walk":         true,
	"WalkDir":      true,
}

// forbiddenTimeCalls read the local clock or act on it (timers, tickers,
// sleeps), or read the host's time zone (Local, LoadLocation).
var forbiddenTimeCalls = map[string]bool{
	"Now":          true,
	"Since":        true,
	"Until":        true,
	"Tick":         true,
	"After":        true,
	"AfterFunc":    true,
	"NewTimer":     true,
	"NewTicker":    true,
	"Sleep":        true,
	"Local":        true,
	"LoadLocation": true,
}

// forbiddenImport reports whether path is a forbidden import.
func forbiddenImport(path string) bool {
	if forbiddenImports[path] {
		return true
	}
	for _, tree := range forbiddenImportTrees {
		if path == tree || strings.HasPrefix(path, tree+"/") {
			return true
		}
	}
	return false
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
//   - imports a nondeterministic package: math/rand, math/rand/v2,
//     crypto/rand, hash/maphash, io/ioutil, io/fs, plugin, or os, net,
//     runtime, syscall, or any path below them
//   - uses a file-system function of path/filepath (Abs, EvalSymlinks,
//     Glob, Walk, WalkDir)
//   - uses a clock, timer, or time-zone name of package time (Now, Since,
//     Until, Tick, After, AfterFunc, NewTimer, NewTicker, Sleep, Local,
//     LoadLocation)
//   - dot-imports "time", "math", or "path/filepath"
//   - names a floating-point or complex type (float32, float64, complex64,
//     complex128), which also covers conversions such as float64(x)
//   - writes a floating-point or imaginary literal (0.75, 1e3, 2i)
//   - uses any name of package math other than its integer constants
//     (MaxInt*, MinInt*, MaxUint*)
//   - uses big.Float, big.NewFloat, big.ParseFloat, strconv.ParseFloat,
//     strconv.FormatFloat, or strconv.AppendFloat
//
// It is a syntactic scan of selectors on package names. It does not see a
// float that arrives through another package's API (for example
// encoding/json decoding a number into an interface), a method on a value
// (time.Time.Local), a goroutine or a select, or a range over a map; those
// stay review items. A directory without non-test Go files fails too, so a
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
		if forbiddenImport(path) {
			report(spec.Pos(), "imports %q", path)
		}
		if path != "time" && path != "math" && path != "path/filepath" && forbiddenFloatCalls[path] == nil {
			continue
		}
		local := filepath.Base(path)
		switch {
		case spec.Name == nil:
		case spec.Name.Name == ".":
			if path == "time" || path == "math" || path == "path/filepath" {
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
			case path == "path/filepath" && forbiddenFileCalls[name]:
				report(typed.Pos(), "uses filepath.%s", name)
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
