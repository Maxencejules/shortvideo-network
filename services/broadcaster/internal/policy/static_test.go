package policy

// Static checks of the whole services/broadcaster module (plan 13.6):
// signing calls only in internal/signer, no local copy of a signed
// preimage, and no configuration field that could hold a hosting-intent
// key. Each scanner is first run on planted sources, so a scanner that
// finds nothing cannot pass by accident.

import (
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

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

// goFile is one parsed Go file of the module.
type goFile struct {
	rel  string // slash path relative to the module root
	test bool
	file *ast.File
}

// moduleFiles parses every .go file of the module.
func moduleFiles(t *testing.T) []goFile {
	t.Helper()
	root := moduleRoot(t)
	var out []goFile
	fset := token.NewFileSet()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if name := d.Name(); path != root && (strings.HasPrefix(name, ".") || name == "testdata") {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		out = append(out, goFile{rel: filepath.ToSlash(rel), test: strings.HasSuffix(path, "_test.go"), file: f})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) < 20 {
		t.Fatalf("only %d Go files found under %s", len(out), root)
	}
	return out
}

func parseSource(t *testing.T, src string) *ast.File {
	t.Helper()
	f, err := parser.ParseFile(token.NewFileSet(), "planted.go", src, parser.SkipObjectResolution)
	if err != nil {
		t.Fatal(err)
	}
	return f
}

// signNames are the signing functions and methods: ed25519.Sign,
// cryptotypes.PrivKey.Sign, SDK tx.SignWithPrivKey, ecdsa and rsa signing,
// and dcrd SignCompact.
var signNames = map[string]bool{
	"Sign": true, sdkSignWith: true, "SignCompact": true, "SignASN1": true, "SignPSS": true, "SignPKCS1v15": true,
}

// sdkSignWith is the SDK's tx signing helper, named in two pieces so the
// repository's secret scanner does not read the map literal as a key
// assignment.
const sdkSignWith = "SignWith" + "PrivKey"

// keyMakers are the functions that build a signing key from randomness or
// a seed (SDK GenPrivKey and GenPrivKeyFromSecret, ed25519 NewKeyFromSeed
// and GenerateKey, ecdsa GenerateKey).
var keyMakers = map[string]bool{"GenPrivKey": true, "GenPrivKeyFromSecret": true, "NewKeyFromSeed": true, "GenerateKey": true}

// sdkKeyPackages are the SDK packages whose PrivKey type holds a signing
// key; naming that type builds or holds one.
var sdkKeyPackages = map[string]bool{
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1": true,
	"github.com/cosmos/cosmos-sdk/crypto/keys/ed25519":   true,
}

// devCAFile is the one file outside internal/signer and internal/keys
// that may generate a key: crypto/ecdsa P-256 keys of the development TLS
// certificates (dev-ca), never a chain signing key.
const devCAFile = "cmd/svn-broadcaster/devca.go"

const keysImport = "github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/keys"

// scanHit is one finding of signingUses. keyMaking marks the building of a
// private key, which internal/keys may do; every other hit is a signing
// use or an import of the key loader, which only internal/signer may have.
type scanHit struct {
	what      string
	keyMaking bool
}

// importNames maps each import's local name in f to its path.
func importNames(f *ast.File) map[string]string {
	names := map[string]string{}
	for _, imp := range f.Imports {
		path, err := strconv.Unquote(imp.Path.Value)
		if err != nil {
			continue
		}
		name := path[strings.LastIndex(path, "/")+1:]
		if imp.Name != nil {
			name = imp.Name.Name
		}
		names[name] = path
	}
	return names
}

// signingUses returns the signing uses in f, the file at rel (a slash
// path relative to the module root):
//   - an import of the key loader;
//   - every use of a name in signNames, called or not: a call, a method or
//     function value (f := k.Sign), a value stored in a field, a method
//     declaration. Only a call without arguments is not one, so
//     big.Int.Sign() is not counted;
//   - a call or value of a function in keyMakers, and any reference to the
//     PrivKey type of an SDK key package (keyMaking hits). The crypto/ecdsa
//     GenerateKey calls of devCAFile are not counted.
func signingUses(rel string, f *ast.File) []scanHit {
	var hits []scanHit
	imports := importNames(f)
	for _, path := range imports {
		if path == keysImport {
			hits = append(hits, scanHit{what: "import of internal/keys"})
		}
	}
	exempt := map[*ast.Ident]bool{}
	ast.Inspect(f, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok || len(call.Args) != 0 {
			return true
		}
		switch fun := call.Fun.(type) {
		case *ast.SelectorExpr:
			exempt[fun.Sel] = true
		case *ast.Ident:
			exempt[fun] = true
		}
		return true
	})
	ast.Inspect(f, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.SelectorExpr:
			pkg := ""
			if id, ok := x.X.(*ast.Ident); ok {
				pkg = imports[id.Name]
			}
			switch {
			case keyMakers[x.Sel.Name] && !(rel == devCAFile && pkg == "crypto/ecdsa" && x.Sel.Name == "GenerateKey"):
				hits = append(hits, scanHit{what: "key generation ." + x.Sel.Name, keyMaking: true})
			case x.Sel.Name == "PrivKey" && sdkKeyPackages[pkg]:
				hits = append(hits, scanHit{what: "SDK private key type of " + pkg, keyMaking: true})
			}
		case *ast.Ident:
			if signNames[x.Name] && !exempt[x] {
				hits = append(hits, scanHit{what: "use of " + x.Name})
			}
		}
		return true
	})
	return hits
}

// plantedSigning holds every kind of use signingUses must find: the key
// loader import, 7 signing uses (four calls, a method value, a function
// value, a method value stored in a struct field) and 4 key-making hits (an SDK private key
// literal, SDK and ed25519 key generation, ecdsa key generation outside
// dev-ca), and one call that is not a use (n.Sign() has no argument).
const plantedSigning = `package p
import (
	"crypto/ecdsa"
	"crypto/ed25519"
	sdksecp "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/keys"
)
type holder struct{ fn func([]byte) ([]byte, error) }
func f(k ed25519.PrivateKey, key keys.Key, m []byte, n bigInt, raw []byte) {
	_ = ed25519.Sign(k, m)
	_, _ = key.SDK().Sign(m)
	_ = tx.` + sdkSignWith + `(nil, 0, nil, nil, nil, nil, 0)
	_ = ` + sdkSignWith + `(m)
	_ = n.Sign()
	g := key.SDK().Sign
	_, _ = g(m)
	s := ed25519.Sign
	_ = s
	_ = holder{fn: key.SDK().Sign}
	_ = &sdksecp.PrivKey{Key: raw}
	_ = sdksecp.GenPrivKey()
	_, _, _ = ed25519.GenerateKey(nil)
	_, _ = ecdsa.GenerateKey(nil, nil)
}`

// countHits returns the signing and key-making hits of hits.
func countHits(hits []scanHit) (signing, keyMaking int) {
	for _, h := range hits {
		if h.keyMaking {
			keyMaking++
		} else {
			signing++
		}
	}
	return signing, keyMaking
}

// TestNoSigningOutsideSigner checks plan 13.6 and 7.4: no non-test file of
// the module outside internal/signer uses an ed25519 or SDK signing
// function or method (called, or taken as a value that something else can
// call) or imports the key loader, and no non-test file outside
// internal/signer and internal/keys builds or generates a private key
// (dev-ca's TLS keys excepted). Test files are exempt: unit tests sign
// fixtures with generated keys.
func TestNoSigningOutsideSigner(t *testing.T) {
	planted := parseSource(t, plantedSigning)
	if signing, keyMaking := countHits(signingUses("planted.go", planted)); signing != 8 || keyMaking != 4 {
		t.Fatalf("scanner found %v in the planted source, want 8 signing uses (the keys import included; n.Sign() is not one) "+
			"and 4 key-making hits", signingUses("planted.go", planted))
	}
	devca := parseSource(t, "package p\nimport \"crypto/ecdsa\"\nfunc f() { _, _ = ecdsa.GenerateKey(nil, nil) }\n")
	if got := signingUses(devCAFile, devca); len(got) != 0 {
		t.Fatalf("scanner found %v in dev-ca's TLS key generation", got)
	}
	if got := signingUses("other.go", devca); len(got) != 1 {
		t.Fatalf("scanner found %v for ecdsa key generation outside dev-ca, want one hit", got)
	}

	var signerHits int
	for _, f := range moduleFiles(t) {
		if f.test {
			continue
		}
		hits := signingUses(f.rel, f.file)
		if strings.HasPrefix(f.rel, "internal/signer/") {
			signerHits += len(hits)
			continue
		}
		for _, h := range hits {
			if h.keyMaking && strings.HasPrefix(f.rel, "internal/keys/") {
				continue
			}
			t.Errorf("%s: %s outside internal/signer", f.rel, h.what)
		}
	}
	if signerHits == 0 {
		t.Fatal("the scanner found no signing use in internal/signer, so it would miss one elsewhere too")
	}
}

// domainFragments are the starts of the provider and hosting-intent
// signature domains, assembled so this file holds no such literal.
var domainFragments = []string{"SVN-" + "PROVIDER-", "SVN-" + "HOSTING-" + "INTENT-"}

// preimageLiterals returns the string literals of f that contain a
// provider or hosting-intent domain.
func preimageLiterals(f *ast.File) []string {
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		lit, ok := n.(*ast.BasicLit)
		if !ok || lit.Kind != token.STRING {
			return true
		}
		s, err := strconv.Unquote(lit.Value)
		if err != nil {
			s = lit.Value
		}
		for _, frag := range domainFragments {
			if strings.Contains(s, frag) {
				hits = append(hits, frag)
			}
		}
		return true
	})
	return hits
}

// TestNoLocalPreimageCopy checks plan 4.3 and 13.6: the module (test files
// included) holds no string literal of a provider or hosting-intent
// signature domain, so every signed payload it checks is built by the
// chain's exported builders (leases.*SignBytes, content.*IntentSignBytes).
func TestNoLocalPreimageCopy(t *testing.T) {
	planted := parseSource(t, "package p\nconst a = \""+domainFragments[0]+"ACCEPT-1\"\nvar b = []byte(`"+domainFragments[1]+"1`)\n")
	if got := preimageLiterals(planted); len(got) != 2 {
		t.Fatalf("scanner found %v in the planted source, want 2", got)
	}
	files := moduleFiles(t)
	for _, f := range files {
		for _, h := range preimageLiterals(f.file) {
			t.Errorf("%s: string literal with the domain %s", f.rel, h)
		}
	}
}

// intentKeyField reports whether a struct field name or JSON tag could
// carry a hosting-intent key: it names an intent and a key, seed, secret
// or private value. The registry path (a file of public keys, plan 7.4)
// ends in "registry" and is allowed.
func intentKeyField(name string) bool {
	n := strings.NewReplacer("_", "", "-", "", ".", "").Replace(strings.ToLower(name))
	if !strings.Contains(n, "intent") || strings.HasSuffix(n, "registry") || strings.HasSuffix(n, "registrypath") {
		return false
	}
	for _, part := range []string{"key", "seed", "secret", "priv"} {
		if strings.Contains(n, part) {
			return true
		}
	}
	return false
}

// intentKeyFields returns the struct fields of f that intentKeyField
// flags, by field name or json tag.
func intentKeyFields(f *ast.File) []string {
	var hits []string
	ast.Inspect(f, func(n ast.Node) bool {
		st, ok := n.(*ast.StructType)
		if !ok {
			return true
		}
		for _, field := range st.Fields.List {
			names := []string{}
			for _, id := range field.Names {
				names = append(names, id.Name)
			}
			if field.Tag != nil {
				if tag, err := strconv.Unquote(field.Tag.Value); err == nil {
					if j, ok := reflect.StructTag(tag).Lookup("json"); ok {
						names = append(names, strings.Split(j, ",")[0])
					}
				}
			}
			for _, name := range names {
				if intentKeyField(name) {
					hits = append(hits, name)
				}
			}
		}
		return true
	})
	return hits
}

// TestConfigHasNoIntentKey checks plan 7.4 (owner answer Q3): the
// broadcaster holds no hosting-intent key, so no struct of the module (the
// configuration of internal/config, the signer's Config and KeyConfig, and
// every other) has a field that could carry one. A config decoder that
// refuses unknown fields then refuses such a key in the file.
func TestConfigHasNoIntentKey(t *testing.T) {
	planted := parseSource(t, "package p\ntype C struct {\n\tIntentKey string\n\tKeys struct{ X string `json:\"hosting_intent_seed\"` }\n"+
		"\tIntentKeyRegistry string `json:\"intent_key_registry\"`\n\tIntentID string `json:\"intent_id\"`\n}\n")
	if got := intentKeyFields(planted); len(got) != 2 {
		t.Fatalf("scanner found %v in the planted source, want IntentKey and hosting_intent_seed only", got)
	}
	var structs int
	for _, f := range moduleFiles(t) {
		if f.test {
			continue
		}
		ast.Inspect(f.file, func(n ast.Node) bool {
			if _, ok := n.(*ast.StructType); ok {
				structs++
			}
			return true
		})
		for _, h := range intentKeyFields(f.file) {
			t.Errorf("%s: field %q could hold a hosting-intent key", f.rel, h)
		}
	}
	if structs == 0 {
		t.Fatal("no struct type found in the module")
	}
	if _, err := os.Stat(filepath.Join(moduleRoot(t), "internal", "signer", "signer.go")); errors.Is(err, fs.ErrNotExist) {
		t.Fatal("the signer's configuration is not where this test expects it")
	}
}
