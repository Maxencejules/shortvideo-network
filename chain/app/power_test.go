package app_test

import (
	"os"
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
