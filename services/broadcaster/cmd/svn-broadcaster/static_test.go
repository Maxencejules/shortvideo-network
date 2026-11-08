package main

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sort"
	"strings"
	"testing"
)

// parseFile parses one source file of this package (or planted source).
func parseFile(t *testing.T, name string, src any) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), name, src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// selectorCalls returns, for each function of f, the calls of the form
// x.Name(...) whose x is one of the identifiers in receivers, as "x.Name".
func selectorCalls(f *ast.File, receivers ...string) map[string][]string {
	recv := map[string]bool{}
	for _, r := range receivers {
		recv[r] = true
	}
	out := map[string][]string{}
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Body == nil {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			if sel, ok := call.Fun.(*ast.SelectorExpr); ok {
				if id, ok := sel.X.(*ast.Ident); ok && recv[id.Name] {
					out[fn.Name.Name] = append(out[fn.Name.Name], id.Name+"."+sel.Sel.Name)
				}
			}
			return true
		})
	}
	return out
}

// statusAllowed are the only outbox store calls of status.go: opening the
// pool, the schema and chain-binding reads, the Store value, and the three
// reads of the status view. Everything else the store offers can write.
var statusAllowed = map[string]bool{
	"store.OpenDB": true, "store.CheckChain": true, "store.New": true,
	"st.KeySigners": true, "st.ReadChainHealth": true, "st.OpenStallEpisode": true,
}

// storeWrites returns the store calls of f outside statusAllowed.
func storeWrites(f *ast.File) []string {
	var bad []string
	for fn, calls := range selectorCalls(f, "store", "st", "db") {
		for _, c := range calls {
			if strings.HasPrefix(c, "db.") && c != "db.Close" {
				bad = append(bad, fn+": "+c)
				continue
			}
			if !strings.HasPrefix(c, "db.") && !statusAllowed[c] {
				bad = append(bad, fn+": "+c)
			}
		}
	}
	sort.Strings(bad)
	return bad
}

// TestStatusOnlyReads checks plan 8.1's rule that status is read-only:
// status.go calls only the store's read functions (and checkSchema, which
// calls store.CheckSchema only), never a write, a signer session or a raw
// statement on the pool. The scan is first run on planted source.
func TestStatusOnlyReads(t *testing.T) {
	planted := parseFile(t, "planted.go", `package main
func readStatus() {
	_ = store.Migrate(ctx, db)
	_, _ = st.LockSigner(ctx, nil)
	_, _ = db.ExecContext(ctx, "UPDATE x")
	_, _ = st.KeySigners(ctx)
}`)
	if got := storeWrites(planted); len(got) != 3 {
		t.Fatalf("scanner found %v in the planted source, want 3", got)
	}
	f := parseFile(t, "status.go", nil)
	if bad := storeWrites(f); len(bad) != 0 {
		t.Fatalf("status.go calls store functions outside the read set: %v", bad)
	}
	calls := selectorCalls(f, "store", "st")
	if len(calls["readStatus"]) != 3 || len(calls["runStatus"]) == 0 {
		t.Fatalf("status.go store calls %v", calls)
	}
	serve := parseFile(t, "serve.go", nil)
	if got := selectorCalls(serve, "store")["checkSchema"]; len(got) != 1 || got[0] != "store.CheckSchema" {
		t.Fatalf("checkSchema calls %v, want store.CheckSchema only", got)
	}
}

// TestServerConstructorSharedWithMain checks plan 5.6 for the binary:
// serve builds the API with api.New, the one constructor the API tests
// use, and serves it through the server's own ListenAndServe; nothing in
// this package builds an HTTP server or a TLS configuration of its own.
// (internal/api's test of the same name checks the module's other
// non-test files.)
func TestServerConstructorSharedWithMain(t *testing.T) {
	f := parseFile(t, "serve.go", nil)
	var sawNew, sawServe bool
	for _, decl := range f.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "runServe" {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "api" && sel.Sel.Name == "New" {
				sawNew = true
			}
			if id, ok := sel.X.(*ast.Ident); ok && id.Name == "srv" && sel.Sel.Name == "ListenAndServe" {
				sawServe = true
			}
			return true
		})
	}
	if !sawNew || !sawServe {
		t.Fatalf("runServe: api.New %v, srv.ListenAndServe %v", sawNew, sawServe)
	}
	for _, name := range []string{"main.go", "run.go", "serve.go", "signercmd.go", "migrate.go", "status.go",
		"signerclear.go", "devca.go", "devdb.go"} {
		src := parseFile(t, name, nil)
		ast.Inspect(src, func(n ast.Node) bool {
			switch x := n.(type) {
			case *ast.CompositeLit:
				if sel, ok := x.Type.(*ast.SelectorExpr); ok {
					if id, ok := sel.X.(*ast.Ident); ok && ((id.Name == "http" && sel.Sel.Name == "Server") ||
						(id.Name == "tls" && sel.Sel.Name == "Config")) {
						t.Errorf("%s builds %s.%s", name, id.Name, sel.Sel.Name)
					}
				}
			case *ast.SelectorExpr:
				if id, ok := x.X.(*ast.Ident); ok && id.Name == "http" &&
					(x.Sel.Name == "ListenAndServe" || x.Sel.Name == "ListenAndServeTLS" || x.Sel.Name == "Serve" || x.Sel.Name == "ServeTLS") {
					t.Errorf("%s calls http.%s", name, x.Sel.Name)
				}
			}
			return true
		})
	}
}
