package node

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	cmtcfg "github.com/cometbft/cometbft/config"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	cmttypes "github.com/cometbft/cometbft/types"
)

// StoredGenesisKey is the key under which CometBFT keeps, in its state
// database (data/state.db), the genesis document it started the chain
// from (cometbft v0.40.0 node/setup.go genesisDocKey).
const StoredGenesisKey = "genesisDoc"

// GenesisFileChainID returns the chain id of the CometBFT genesis file at
// path, decoded as CometBFT decodes it (types.GenesisDocFromFile: exact
// JSON keys, then ValidateAndComplete). A key that differs from chain_id
// only in letter case is not the chain id, and a document CometBFT would
// refuse is an error.
func GenesisFileChainID(path string) (string, error) {
	doc, err := cmttypes.GenesisDocFromFile(path)
	if err != nil {
		return "", err
	}
	return doc.ChainID, nil
}

// CheckStoredGenesis compares the genesis document stored under
// StoredGenesisKey of a state database (raw, empty when the database holds
// none) with the chain id the genesis file names. Once CometBFT has started
// on a home it runs the stored document and ignores the file (cometbft
// v0.40.0 node/setup.go LoadStateFromDBOrGenesisDocProvider), so a stored
// document that does not decode, or that names another chain id, is an
// error.
func CheckStoredGenesis(raw []byte, fileChainID string) error {
	if len(raw) == 0 {
		return nil
	}
	var doc *cmttypes.GenesisDoc
	if err := cmtjson.Unmarshal(raw, &doc); err != nil || doc == nil {
		return fmt.Errorf("the genesis document stored in the state database does not decode: %v", err)
	}
	if doc.ChainID != fileChainID {
		return fmt.Errorf("the state database holds the genesis document of chain id %q, which CometBFT runs; the genesis file names chain id %q", doc.ChainID, fileChainID)
	}
	return nil
}

// GenesisChainID returns the chain id a node started on home runs: the
// chain id of home's CometBFT genesis file (GenesisFileChainID), after
// CheckStoredGenesis against the state database data/state.db when that
// database exists. A home without it has never started, and CometBFT will
// store the file's document at the first start. GenesisChainID opens the
// state database as CometBFT does (cmtcfg.DefaultDBProvider, the goleveldb
// backend cometConfig keeps), reads one key, and closes it before it
// returns, so CometBFT can open it afterwards.
func GenesisChainID(home string) (string, error) {
	return genesisChainID(cmtcfg.DefaultConfig().SetRoot(home))
}

func genesisChainID(cfg *cmtcfg.Config) (string, error) {
	chainID, err := GenesisFileChainID(cfg.GenesisFile())
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filepath.Join(cfg.DBDir(), "state.db")); errors.Is(err, os.ErrNotExist) {
		return chainID, nil
	} else if err != nil {
		return "", err
	}
	db, err := cmtcfg.DefaultDBProvider(&cmtcfg.DBContext{ID: "state", Config: cfg})
	if err != nil {
		return "", fmt.Errorf("open the state database of %s: %w", cfg.RootDir, err)
	}
	raw, err := db.Get([]byte(StoredGenesisKey))
	closeErr := db.Close()
	if err != nil {
		return "", fmt.Errorf("read the stored genesis document of %s: %w", cfg.RootDir, err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close the state database of %s: %w", cfg.RootDir, closeErr)
	}
	if err := CheckStoredGenesis(raw, chainID); err != nil {
		return "", fmt.Errorf("home %s: %w", cfg.RootDir, err)
	}
	return chainID, nil
}
