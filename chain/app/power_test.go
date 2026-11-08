package app_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestValidatorUpdatesHaveOneConstructor(t *testing.T) {
	root := filepath.Join("..")
	var hits []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(body), "abci.ValidatorUpdate{") {
			hits = append(hits, filepath.ToSlash(path))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || !strings.HasSuffix(hits[0], "x/authority/updates.go") {
		t.Fatalf("validator update constructors %v", hits)
	}
}

func TestChainSourcesDoNotImportEconomicModules(t *testing.T) {
	forbidden := []string{
		"github.com/cosmos/cosmos-sdk/x/staking",
		"github.com/cosmos/cosmos-sdk/x/bank",
		"github.com/cosmos/cosmos-sdk/x/mint",
		"github.com/cosmos/cosmos-sdk/x/distribution",
		"github.com/cosmos/cosmos-sdk/x/slashing",
		"github.com/cosmos/cosmos-sdk/x/gov",
		"github.com/cosmos/ibc-go",
		"github.com/cosmos/evm",
		"enterprise/poa",
		"enterprise/group",
	}
	err := filepath.Walk("..", func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		text := string(body)
		for _, pathName := range forbidden {
			if strings.Contains(text, pathName) {
				t.Errorf("%s imports %s", path, pathName)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

// svndBuildResult is one build of ./cmd/svnd: the SHA-256 of the binary,
// its `version` output, and its `go tool nm` listing.
type svndBuildResult struct {
	digest, version, symbols string
	err                      error
}

var (
	svndBuildsMu sync.Mutex
	svndBuilds   = map[string]svndBuildResult{}
)

// svndBuild builds ./cmd/svnd with build tag tag ("" for the default build,
// svn_rehearsal for B2, svn_rehearsal_bad for B2-bad) once per test binary,
// with the module cache only, and returns what svndBuildResult holds. The
// binary is deleted after they are read.
func svndBuild(tag string) svndBuildResult {
	svndBuildsMu.Lock()
	defer svndBuildsMu.Unlock()
	if res, ok := svndBuilds[tag]; ok {
		return res
	}
	res := buildAndRead(tag)
	svndBuilds[tag] = res
	return res
}

func buildAndRead(tag string) svndBuildResult {
	dir, err := os.MkdirTemp("", "svnd-nm-")
	if err != nil {
		return svndBuildResult{err: err}
	}
	defer os.RemoveAll(dir)
	bin := filepath.Join(dir, "svnd")
	args := []string{"build", "-o", bin}
	if tag != "" {
		args = append(args, "-tags", tag)
	}
	cmd := exec.Command("go", append(args, "./cmd/svnd")...)
	cmd.Dir = filepath.Clean("..")
	cmd.Env = append(os.Environ(), "GOPROXY=off")
	if output, err := cmd.CombinedOutput(); err != nil {
		return svndBuildResult{err: fmt.Errorf("build with tag %q: %v\n%s", tag, err, output)}
	}
	raw, err := os.ReadFile(bin)
	if err != nil {
		return svndBuildResult{err: err}
	}
	digest := sha256.Sum256(raw)
	version, err := exec.Command(bin, "version").CombinedOutput()
	if err != nil {
		return svndBuildResult{err: fmt.Errorf("version of the build with tag %q: %v\n%s", tag, err, version)}
	}
	nm, err := exec.Command("go", "tool", "nm", bin).CombinedOutput()
	if err != nil {
		return svndBuildResult{err: fmt.Errorf("nm: %v\n%s", err, nm)}
	}
	return svndBuildResult{digest: hex.EncodeToString(digest[:]), version: string(version), symbols: string(nm)}
}

// built returns the build of ./cmd/svnd with tag and fails the test on a
// build error.
func built(t *testing.T, tag string) svndBuildResult {
	t.Helper()
	res := svndBuild(tag)
	if res.err != nil {
		t.Fatal(res.err)
	}
	return res
}

func linkedSymbols(t *testing.T) string {
	t.Helper()
	return built(t, "").symbols
}

// economicModules are the module paths no svnd build may link.
var economicModules = []string{
	"github.com/cosmos/cosmos-sdk/x/staking",
	"github.com/cosmos/cosmos-sdk/x/bank",
	"github.com/cosmos/cosmos-sdk/x/mint",
	"github.com/cosmos/cosmos-sdk/x/distribution",
	"github.com/cosmos/cosmos-sdk/x/slashing",
	"github.com/cosmos/cosmos-sdk/x/gov",
	"github.com/cosmos/cosmos-sdk/x/authz",
	"github.com/cosmos/ibc-go",
	"github.com/cosmos/evm",
}

// TestLinkedBinaryOmitsEconomicModules: no economic module is linked into
// svnd, nor into the rehearsal builds B2 (-tags svn_rehearsal) and B2-bad
// (-tags svn_rehearsal_bad), nor into the state check build (-tags
// svn_statecheck), which are built from the same main package.
func TestLinkedBinaryOmitsEconomicModules(t *testing.T) {
	for _, tag := range []string{"", "svn_rehearsal", "svn_rehearsal_bad", "svn_statecheck"} {
		text := built(t, tag).symbols
		for _, path := range economicModules {
			if strings.Contains(text, path) {
				t.Errorf("the build with tag %q links %s", tag, path)
			}
		}
	}
}

// TestLinkedBinaryOmitsTestHooks fails if svnd (the default build, no build
// tag) links a test hook of this repository: a symbol whose name contains
// ForTest, or the round-2 hooks InstallMismatchedRemoval,
// ConsensusSnapshot, and AuthoritySnapshot. The hooks live in _test.go
// files. It also fails on any symbol of a test support package (leasestest,
// evidencetest, policytest, msgkittest) or of chain/internal (only the
// tagged builds of the same main package, such as the rehearsal builds
// -tags svn_rehearsal and svn_rehearsal_bad, link chain/internal code; the
// default build never does). Symbols of other modules are
// not checked (pebble links StringForTests). Controls: the listing holds
// x/authority's ApplyDue, x/policy's ApplyDue, and the upgrade runner's
// PreBlock, so the scan read the packages it guards.
func TestLinkedBinaryOmitsTestHooks(t *testing.T) {
	const repo = "github.com/Maxencejules/shortvideo-network/"
	text := linkedSymbols(t)
	hooks := []string{"ForTest", "InstallMismatchedRemoval", "ConsensusSnapshot", "AuthoritySnapshot"}
	packages := []string{
		repo + "chain/internal/",
		repo + "chain/x/leases/leasestest",
		repo + "chain/x/evidence/evidencetest",
		repo + "chain/x/policy/policytest",
		repo + "chain/msgkit/msgkittest",
	}
	controls := map[string]bool{
		repo + "chain/x/authority.(*State).ApplyDue": false,
		repo + "chain/x/policy.Keeper.ApplyDue":      false,
		repo + "chain/upgrade.(*Runner).PreBlock":    false,
	}
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := strings.Join(fields[2:], " ")
		if !strings.Contains(name, repo) {
			continue
		}
		for control := range controls {
			if strings.Contains(name, control) {
				controls[control] = true
			}
		}
		for _, hook := range hooks {
			if strings.Contains(name, hook) {
				t.Errorf("svnd links test hook %s", name)
			}
		}
		for _, pkg := range packages {
			if strings.Contains(name, pkg) {
				t.Errorf("svnd links test support code %s", name)
			}
		}
	}
	for control, found := range controls {
		if !found {
			t.Fatalf("the symbol listing has no %s; the scan read nothing of that package", control)
		}
	}
}
