package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestScaffoldIsNotAChain(t *testing.T) {
	if !Scaffold {
		t.Fatal("scaffold flag cleared")
	}
}

func TestAppSourcesDoNotImportCosmos(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") {
			continue
		}
		body, err := os.ReadFile(filepath.Clean(entry.Name()))
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		forbidden := []string{
			"github.com/cosmos/" + "cosmos-sdk",
			"github.com/cometbft/" + "cometbft",
		}
		for _, path := range forbidden {
			if strings.Contains(text, path) {
				t.Fatalf("%s imports the chain stack; goal 05 wires that", entry.Name())
			}
		}
	}
}
