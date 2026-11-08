package node_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cmtdb "github.com/cometbft/cometbft-db"

	"github.com/Maxencejules/shortvideo-network/chain/devnet"
	"github.com/Maxencejules/shortvideo-network/chain/node"
)

// TestGenesisChainIDReadsWhatCometBFTRuns (security review, Wave 2): the
// chain id svnd start gives the application is the one CometBFT runs. The
// genesis file is decoded with CometBFT's decoder (exact JSON keys, then
// ValidateAndComplete), so a key that differs from chain_id only in letter
// case is not the chain id, and a document without a chain id is refused. A
// home that never started has no state database, and none is created. Once
// the state database holds a genesis document (CometBFT stores it at the
// first start and runs it from then on), a document of another chain id, or
// one that does not decode, is refused; the same chain id passes.
func TestGenesisChainIDReadsWhatCometBFTRuns(t *testing.T) {
	home := t.TempDir()
	genesis := func(body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(home, "config"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(home, "config", "genesis.json"), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := node.GenesisChainID(home); err == nil || !strings.Contains(err.Error(), "genesis.json") {
		t.Fatalf("home without a genesis file: %v", err)
	}
	genesis(`{"chain_id":""}`)
	if _, err := node.GenesisChainID(home); err == nil || !strings.Contains(err.Error(), "non-empty chain_id") {
		t.Fatalf("genesis without a chain id: %v", err)
	}
	genesis(`{"chain_id":"svn-beta-1","CHAIN_ID":"shortvideo-dev-x"}`)
	if got, err := node.GenesisChainID(home); err != nil || got != "svn-beta-1" {
		t.Fatalf("case-variant key: chain id %q, error %v; want svn-beta-1", got, err)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "state.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("GenesisChainID created a state database on a home that never started: %v", err)
	}

	genesis(`{"chain_id":"shortvideo-dev-x"}`)
	stored := func(doc string) {
		t.Helper()
		db, err := cmtdb.NewDB("state", cmtdb.GoLevelDBBackend, filepath.Join(home, "data"))
		if err != nil {
			t.Fatal(err)
		}
		if err := db.SetSync([]byte(node.StoredGenesisKey), []byte(doc)); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	stored(`{"chain_id":"svn-beta-1"}`)
	if _, err := node.GenesisChainID(home); err == nil || !strings.Contains(err.Error(), `holds the genesis document of chain id "svn-beta-1", which CometBFT runs; the genesis file names chain id "shortvideo-dev-x"`) {
		t.Fatalf("stored genesis of another chain: %v", err)
	}
	stored(`{"chain_id":`)
	if _, err := node.GenesisChainID(home); err == nil || !strings.Contains(err.Error(), "stored in the state database does not decode") {
		t.Fatalf("stored genesis that does not decode: %v", err)
	}
	stored(`{"chain_id":"shortvideo-dev-x"}`)
	if got, err := node.GenesisChainID(home); err != nil || got != "shortvideo-dev-x" {
		t.Fatalf("stored genesis of the same chain: chain id %q, error %v", got, err)
	}
}

// TestStartWithRefusesAStoredGenesisOfAnotherChain: StartWith reads the
// chain id with genesisChainID, so on a devnet home whose state database
// holds a genesis document of another chain id than its genesis file it
// returns that refusal before it opens the application database or starts
// CometBFT (no port is bound, and data/application.db is not created).
func TestStartWithRefusesAStoredGenesisOfAnotherChain(t *testing.T) {
	layout, err := devnet.Init(devnet.Options{Home: t.TempDir(), ChainID: "shortvideo-dev-node-stored"})
	if err != nil {
		t.Fatal(err)
	}
	home := layout.Topology.Validators[0].Home
	db, err := cmtdb.NewDB("state", cmtdb.GoLevelDBBackend, filepath.Join(home, "data"))
	if err != nil {
		t.Fatal(err)
	}
	if err := db.SetSync([]byte(node.StoredGenesisKey), []byte(`{"chain_id":"svn-beta-1"}`)); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	err = node.StartWith(node.Config{Home: home})
	if err == nil || !strings.Contains(err.Error(), `holds the genesis document of chain id "svn-beta-1", which CometBFT runs; the genesis file names chain id "shortvideo-dev-node-stored"`) {
		t.Fatalf("StartWith on a home whose state database stores another chain's genesis: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, "data", "application.db")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the refused start opened the application database: %v", err)
	}
}
