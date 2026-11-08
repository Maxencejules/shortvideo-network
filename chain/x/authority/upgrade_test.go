package authority

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	errorsmod "cosmossdk.io/errors"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/svnerr"
)

// TestBumpPolicyVersionForUpgrade (DR-10-38): the bump adds one to the
// policy version and writes only 0x04. A damaged version is INTEGRITY and
// 2^64-1 is OVERFLOW; neither writes. After a bump a QUEUED proposal on the
// old base executes to STALE_VERSION, and a propose on the old base is
// STALE_VERSION while one on the new base is accepted and counts against
// its seat's quota at the new version.
func TestBumpPolicyVersionForUpgrade(t *testing.T) {
	e := readyKeeper(t)
	e.queue(t, "before", governedMsg(msgkit.LimitMaxPageLimit, 10))
	for want := uint64(2); want <= 3; want++ {
		before := kvMap(e.kv())
		if err := e.keeper.BumpPolicyVersionForUpgrade(e.blockCtx()); err != nil {
			t.Fatal(err)
		}
		if got := e.keeper.PolicyVersion(e.blockCtx()); got != want {
			t.Fatalf("version %d, want %d", got, want)
		}
		after := kvMap(e.kv())
		if after[string([]byte{policyKey})] != string(encodeU64(want)) {
			t.Fatalf("stored version %x", after[string([]byte{policyKey})])
		}
		delete(before, string([]byte{policyKey}))
		delete(after, string([]byte{policyKey}))
		if !maps.Equal(before, after) {
			t.Fatal("the bump wrote a key other than 0x04")
		}
	}
	ctx := e.txCtx()
	_, err := e.keeper.Execute(ctx, execute(e.authority, "ex-before", "before"))
	if !svnerr.ErrStale.Is(err) {
		t.Fatalf("execute on the old base: %v", err)
	}
	if _, err := e.keeper.Propose(e.txCtx(), propose(e.seats[1].Signer, "old-base", 1, governedMsg(msgkit.LimitMaxPageLimit, 11))); !svnerr.ErrStale.Is(err) {
		t.Fatalf("propose on the old base: %v", err)
	}
	if _, err := e.keeper.Propose(e.txCtx(), propose(e.seats[1].Signer, "new-base", 3, governedMsg(msgkit.LimitMaxPageLimit, 11))); err != nil {
		t.Fatal(err)
	}
	if openAt(t, e.kv(), e.seats[0].ID, 3) != 0 || openAt(t, e.kv(), e.seats[1].ID, 3) != 1 {
		t.Fatal("open counts at the new version")
	}

	for _, r := range []struct {
		name  string
		value []byte
		want  *errorsmod.Error
	}{
		{"3 bytes", []byte{1, 2, 3}, svnerr.ErrIntegrity},
		{"version 1 stored", encodeU64(1), svnerr.ErrIntegrity},
		{"2^64-1", encodeU64(math.MaxUint64), svnerr.ErrOverflow},
	} {
		e.kv().Set([]byte{policyKey}, r.value)
		before := dumpKV(e.kv())
		if err := e.keeper.BumpPolicyVersionForUpgrade(e.blockCtx()); !r.want.Is(err) {
			t.Fatalf("%s: %v", r.name, err)
		}
		wantUnchanged(t, e.kv(), before, r.name)
	}
}

// kvMap is the whole store as a map from key to value.
func kvMap(kv KV) map[string]string {
	out := map[string]string{}
	iter := kv.Iterator(nil, nil)
	defer iter.Close()
	for ; iter.Valid(); iter.Next() {
		out[string(iter.Key())] = string(iter.Value())
	}
	return out
}

// TestBumpPolicyVersionCallSites (DR-10-38; owner ruling Q2 "Council plan,
// fail closed (Recommended)": approved but unexecuted proposals become
// stale at the upgrade): no non-test Go file of the chain module outside
// chain/upgrade names BumpPolicyVersionForUpgrade, so only the upgrade
// runner can bump the policy version outside an execute; and chain/upgrade
// does name it (the runner's call), so the scan is not vacuous.
func TestBumpPolicyVersionCallSites(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	mod, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil || !strings.Contains(string(mod), "module github.com/Maxencejules/shortvideo-network/chain\n") {
		t.Fatalf("%s is not the chain module root (%v)", root, err)
	}
	fset := token.NewFileSet()
	scanned, runner := 0, 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if name := entry.Name(); path != root && (name == "testdata" || strings.HasPrefix(name, ".")) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		scanned++
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		ast.Inspect(file, func(node ast.Node) bool {
			sel, isSel := node.(*ast.SelectorExpr)
			if !isSel || sel.Sel.Name != "BumpPolicyVersionForUpgrade" {
				return true
			}
			if !strings.HasPrefix(filepath.ToSlash(rel), "upgrade/") {
				t.Errorf("%s names BumpPolicyVersionForUpgrade; only chain/upgrade may", fset.Position(sel.Pos()))
				return true
			}
			runner++
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if scanned < 50 || runner == 0 {
		t.Fatalf("scanned %d files, %d calls in chain/upgrade: the walk is broken", scanned, runner)
	}
}
