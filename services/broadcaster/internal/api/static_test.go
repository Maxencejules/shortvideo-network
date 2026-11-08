package api

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// serveCalls are the net/http and crypto/tls functions that serve or
// listen with TLS.
var serveCalls = map[string]map[string]bool{
	"http": {"ListenAndServe": true, "ListenAndServeTLS": true, "Serve": true, "ServeTLS": true},
	"tls":  {"Listen": true, "NewListener": true},
}

// serverConstructions returns the places in f that build an http.Server,
// build a server-side tls.Config (one that sets ClientAuth, ClientCAs or
// Certificates), or call a serving function of net/http or crypto/tls. A
// client-side tls.Config (the chain client's) is not a server.
func serverConstructions(f *ast.File) []string {
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.CompositeLit:
			if sel, ok := x.Type.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && ((id.Name == "http" && sel.Sel.Name == "Server") ||
					(id.Name == "tls" && sel.Sel.Name == "Config" && serverSide(x))) {
					hits = append(hits, id.Name+"."+sel.Sel.Name+"{}")
				}
			}
		case *ast.CallExpr:
			if sel, ok := x.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && serveCalls[id.Name][sel.Sel.Name] {
					hits = append(hits, id.Name+"."+sel.Sel.Name+"()")
				}
			}
		}
		return true
	})
	return hits
}

// serverSide reports whether a tls.Config literal sets a server-side
// field.
func serverSide(lit *ast.CompositeLit) bool {
	for _, el := range lit.Elts {
		if kv, ok := el.(*ast.KeyValueExpr); ok {
			if k, ok := kv.Key.(*ast.Ident); ok && (k.Name == "ClientAuth" || k.Name == "ClientCAs" || k.Name == "Certificates") {
				return true
			}
		}
	}
	return false
}

// TestServerConstructorSharedWithMain checks plan 5.6: New is the one
// constructor and Serve the one serve path. No non-test file of the module
// outside internal/api builds an http.Server or a server-side tls.Config
// or calls a serving function, so the binary can serve the API only through New and
// Serve, the functions these tests use. (The check that the binary's main
// calls api.New belongs with cmd/svn-broadcaster's own tests; see the package's
// interface notes.)
func TestServerConstructorSharedWithMain(t *testing.T) {
	planted, err := parser.ParseFile(token.NewFileSet(), "planted.go", `package p
func f() {
	s := &http.Server{}
	_ = tls.Config{ClientAuth: 4}
	_ = tls.Config{MinVersion: 1}
	_ = http.ListenAndServe(":1", nil)
	_ = s.Serve(nil)
	_, _ = tls.Listen("tcp", ":1", nil)
}`, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	if got := serverConstructions(planted); len(got) != 4 {
		t.Fatalf("scanner found %v in the planted source, want 4 (s.Serve is a method, not http.Serve; a client tls.Config is not a server)", got)
	}

	root := moduleRoot(t)
	var inAPI, files int
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		f, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		files++
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		hits := serverConstructions(f)
		if strings.HasPrefix(filepath.ToSlash(rel), "internal/api/") {
			inAPI += len(hits)
			return nil
		}
		for _, h := range hits {
			t.Errorf("%s: %s outside internal/api", rel, h)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if files < 20 || inAPI == 0 {
		t.Fatalf("scanned %d files, %d constructions in internal/api: the scan would miss one elsewhere", files, inAPI)
	}
}

// moduleRoot returns the directory of services/broadcaster/go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if data, err := os.ReadFile(filepath.Join(dir, "go.mod")); err == nil {
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
