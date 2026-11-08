package statecheck

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"syscall"

	dbm "github.com/cosmos/cosmos-db"

	"github.com/Maxencejules/shortvideo-network/chain/node"
)

// readChainID reads the chain id of home with the readers svnd start uses
// (chain/node): the CometBFT genesis file config/genesis.json decoded as
// CometBFT decodes it (node.GenesisFileChainID), and, when home has a
// CometBFT state database (data/state.db), the genesis document CometBFT
// stored there, which must name the same chain id (node.CheckStoredGenesis).
// The state database is opened read-only (openReadOnly) and closed before
// readChainID returns.
func readChainID(home string) (string, error) {
	path := filepath.Join(home, "config", "genesis.json")
	if _, err := os.Stat(path); err != nil {
		return "", fmt.Errorf("home %s has no genesis file: %w", home, err)
	}
	chainID, err := node.GenesisFileChainID(path)
	if err != nil {
		return "", fmt.Errorf("genesis %s: %w", path, err)
	}
	dir := filepath.Join(home, "data")
	if _, err := os.Stat(filepath.Join(dir, "state.db")); errors.Is(err, os.ErrNotExist) {
		return chainID, nil
	} else if err != nil {
		return "", err
	}
	db, err := openReadOnly("state", dir)
	if err != nil {
		return "", err
	}
	raw, err := db.Get([]byte(node.StoredGenesisKey))
	closeErr := db.Close()
	if err != nil {
		return "", fmt.Errorf("read the stored genesis document of %s: %w", home, err)
	}
	if closeErr != nil {
		return "", fmt.Errorf("close the state database of %s: %w", home, closeErr)
	}
	if err := node.CheckStoredGenesis(raw, chainID); err != nil {
		return "", fmt.Errorf("home %s: %w", home, err)
	}
	return chainID, nil
}

// openReadOnly opens the goleveldb database name in dir (as svnd start
// opens data/application.db, and CometBFT data/state.db) in goleveldb's
// read-only mode: every write fails, and the open locks the database's
// LOCK file, which fails while a running node holds its lock. goleveldb
// creates a missing LOCK file even in read-only mode, so a database
// directory without one (a copy that left it out) is refused before the
// open; with the LOCK file present the open writes no file.
//
// cosmos-db opens goleveldb with options only through
// NewGoLevelDBWithOpts, whose third parameter is goleveldb's *opt.Options.
// goleveldb is an indirect requirement of this module; naming its opt
// package here would make it a direct one (a chain/go.mod change). So the
// options value is built from that parameter's type by reflection, and its
// ReadOnly field is set; a goleveldb without that field is an error, never
// a read-write open.
func openReadOnly(name, dir string) (dbm.DB, error) {
	path := filepath.Join(dir, name+".db")
	if _, err := os.Stat(filepath.Join(path, "LOCK")); err != nil {
		return nil, fmt.Errorf("%s database %s has no LOCK file; copy the whole database directory, LOCK included: %w", name, path, err)
	}
	open := reflect.ValueOf(dbm.NewGoLevelDBWithOpts)
	if open.Type().NumIn() != 3 || open.Type().In(2).Kind() != reflect.Pointer {
		return nil, fmt.Errorf("cosmos-db NewGoLevelDBWithOpts has an unexpected signature %s", open.Type())
	}
	options := reflect.New(open.Type().In(2).Elem())
	readOnly := options.Elem().FieldByName("ReadOnly")
	if !readOnly.IsValid() || readOnly.Kind() != reflect.Bool || !readOnly.CanSet() {
		return nil, fmt.Errorf("goleveldb options %s have no ReadOnly flag", options.Elem().Type())
	}
	readOnly.SetBool(true)
	out := open.Call([]reflect.Value{reflect.ValueOf(name), reflect.ValueOf(dir), options})
	if err, _ := out[1].Interface().(error); err != nil {
		if errors.Is(err, syscall.EAGAIN) {
			return nil, fmt.Errorf("%s database %s is locked by another process (a running node holds it): stop the node or check a copy of its home: %w", name, path, err)
		}
		return nil, fmt.Errorf("open %s database %s read-only: %w", name, path, err)
	}
	db, ok := out[0].Interface().(dbm.DB)
	if !ok {
		return nil, fmt.Errorf("cosmos-db returned %T, not a database", out[0].Interface())
	}
	return db, nil
}
