package app_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

func TestLinkedBinaryOmitsEconomicModules(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "svnd")
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/svnd")
	cmd.Dir = filepath.Clean("..")
	cmd.Env = os.Environ()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	nm := exec.Command("go", "tool", "nm", bin)
	output, err := nm.CombinedOutput()
	if err != nil {
		t.Fatalf("nm: %v\n%s", err, output)
	}
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
	text := string(output)
	for _, path := range forbidden {
		if strings.Contains(text, path) {
			t.Errorf("linked binary contains %s", path)
		}
	}
}
