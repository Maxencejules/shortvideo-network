package app_test

import (
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

// svndSymbols builds ./cmd/svnd once per test binary and returns the
// `go tool nm` listing. The binary is deleted after the listing is read.
var svndSymbols = sync.OnceValues(func() (string, error) {
	dir, err := os.MkdirTemp("", "svnd-nm-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	bin := filepath.Join(dir, "svnd")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/svnd")
	cmd.Dir = filepath.Clean("..")
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		return "", fmt.Errorf("build: %v\n%s", err, output)
	}
	nm := exec.Command("go", "tool", "nm", bin)
	output, err := nm.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("nm: %v\n%s", err, output)
	}
	return string(output), nil
})

func linkedSymbols(t *testing.T) string {
	t.Helper()
	text, err := svndSymbols()
	if err != nil {
		t.Fatal(err)
	}
	return text
}

func TestLinkedBinaryOmitsEconomicModules(t *testing.T) {
	forbidden := []string{
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
	text := linkedSymbols(t)
	for _, path := range forbidden {
		if strings.Contains(text, path) {
			t.Errorf("linked binary contains %s", path)
		}
	}
}

// TestLinkedBinaryOmitsTestHooks fails if svnd links a test hook of this
// repository: a symbol whose name contains ForTest, or the round-2 hooks
// InstallMismatchedRemoval, ConsensusSnapshot, and AuthoritySnapshot. The
// hooks live in _test.go files. Symbols of other modules are not checked
// (pebble links StringForTests).
func TestLinkedBinaryOmitsTestHooks(t *testing.T) {
	const repo = "github.com/Maxencejules/shortvideo-network/"
	text := linkedSymbols(t)
	hooks := []string{"ForTest", "InstallMismatchedRemoval", "ConsensusSnapshot", "AuthoritySnapshot"}
	control := false
	for _, line := range strings.Split(text, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		name := strings.Join(fields[2:], " ")
		if !strings.Contains(name, repo) {
			continue
		}
		if strings.Contains(name, repo+"chain/x/authority.(*State).ApplyDue") {
			control = true
		}
		for _, hook := range hooks {
			if strings.Contains(name, hook) {
				t.Errorf("svnd links test hook %s", name)
			}
		}
	}
	if !control {
		t.Fatal("the symbol listing has no x/authority ApplyDue; the scan read nothing")
	}
}
