package statecheck

import (
	"bytes"
	"errors"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	dbm "github.com/cosmos/cosmos-db"
)

// TestOpenReadOnlyRefusesWrites: openReadOnly reads a goleveldb database a
// stopped process wrote, refuses every write through it (Set, Delete, and a
// batch), and leaves the stored value as it was; while another handle
// holds the database, the open is refused with the lock message.
func TestOpenReadOnlyRefusesWrites(t *testing.T) {
	dir := t.TempDir()
	writer, err := dbm.NewDB("application", dbm.GoLevelDBBackend, dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writer.Set([]byte("k"), []byte("v")); err != nil {
		t.Fatal(err)
	}
	if _, err := openReadOnly("application", dir); err == nil || !strings.Contains(err.Error(), "locked by another process (a running node holds it)") {
		t.Fatalf("open while another handle holds the database: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}

	db, err := openReadOnly("application", dir)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := db.Get([]byte("k")); err != nil || !bytes.Equal(got, []byte("v")) {
		t.Fatalf("read %q %v", got, err)
	}
	if err := db.Set([]byte("k"), []byte("w")); err == nil {
		t.Fatal("Set through the read-only database succeeded")
	}
	if err := db.Delete([]byte("k")); err == nil {
		t.Fatal("Delete through the read-only database succeeded")
	}
	batch := db.NewBatch()
	if err := batch.Set([]byte("x"), []byte("y")); err != nil {
		t.Fatal(err)
	}
	if err := batch.Write(); err == nil {
		t.Fatal("batch write through the read-only database succeeded")
	}
	_ = batch.Close()
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	reopened, err := dbm.NewDB("application", dbm.GoLevelDBBackend, dir)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if got, err := reopened.Get([]byte("k")); err != nil || !bytes.Equal(got, []byte("v")) {
		t.Fatalf("after the read-only open: %q %v", got, err)
	}
	if got, err := reopened.Get([]byte("x")); err != nil || got != nil {
		t.Fatalf("batch key after the read-only open: %q %v", got, err)
	}
}

// TestReadChainIDRefusesBadGenesis: a genesis file that is not JSON or
// names no chain id is an error.
func TestReadChainIDRefusesBadGenesis(t *testing.T) {
	for _, c := range []struct {
		name, body, want string
	}{
		{"not JSON", "{", "genesis"},
		{"no chain id", `{"chain_id":""}`, "non-empty chain_id"},
	} {
		t.Run(c.name, func(t *testing.T) {
			home := t.TempDir()
			writeGenesis(t, home, c.body)
			if _, err := readChainID(home); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want %q", err, c.want)
			}
		})
	}
}

func writeGenesis(t *testing.T, home, body string) {
	t.Helper()
	dir := filepath.Join(home, "config")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "genesis.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestReadChainIDReadsAsCometBFTDoes (security review, Wave 2): the chain
// id is read with CometBFT's own decoder (exact JSON keys), so a key that
// differs from chain_id only in letter case is not the chain id; when the
// home has a CometBFT state database, the genesis document CometBFT stored
// there must name the same chain id (CometBFT runs the stored document and
// ignores the file); and the state database is opened read-only: its bytes
// are unchanged afterwards, and a copy without its LOCK file is refused
// without creating one.
func TestReadChainIDReadsAsCometBFTDoes(t *testing.T) {
	home := t.TempDir()
	writeGenesis(t, home, `{"chain_id":"svn-beta-1","CHAIN_ID":"shortvideo-dev-x"}`)
	if got, err := readChainID(home); err != nil || got != "svn-beta-1" {
		t.Fatalf("case-variant key: chain id %q, error %v; want svn-beta-1", got, err)
	}

	writeGenesis(t, home, `{"chain_id":"shortvideo-dev-x"}`)
	dir := filepath.Join(home, "data")
	stored := func(doc string) {
		t.Helper()
		db, err := dbm.NewDB("state", dbm.GoLevelDBBackend, dir)
		if err != nil {
			t.Fatal(err)
		}
		if err := db.Set([]byte("genesisDoc"), []byte(doc)); err != nil {
			t.Fatal(err)
		}
		if err := db.Close(); err != nil {
			t.Fatal(err)
		}
	}
	stored(`{"chain_id":"svn-beta-1"}`)
	before := treeBytes(t, filepath.Join(dir, "state.db"))
	if _, err := readChainID(home); err == nil || !strings.Contains(err.Error(), `holds the genesis document of chain id "svn-beta-1", which CometBFT runs; the genesis file names chain id "shortvideo-dev-x"`) {
		t.Fatalf("stored genesis of another chain: %v", err)
	}
	if after := treeBytes(t, filepath.Join(dir, "state.db")); !maps.Equal(before, after) {
		t.Fatal("reading the stored genesis document changed the state database")
	}
	stored(`{"chain_id":"shortvideo-dev-x"}`)
	if got, err := readChainID(home); err != nil || got != "shortvideo-dev-x" {
		t.Fatalf("stored genesis of the same chain: chain id %q, error %v", got, err)
	}
	stored(`{"chain_id":`)
	if _, err := readChainID(home); err == nil || !strings.Contains(err.Error(), "stored in the state database does not decode") {
		t.Fatalf("stored genesis that does not decode: %v", err)
	}

	lock := filepath.Join(dir, "state.db", "LOCK")
	if err := os.Remove(lock); err != nil {
		t.Fatal(err)
	}
	if _, err := readChainID(home); err == nil || !strings.Contains(err.Error(), "has no LOCK file; copy the whole database directory, LOCK included") {
		t.Fatalf("state database without LOCK: %v", err)
	}
	if _, err := os.Stat(lock); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the refused open created %s: %v", lock, err)
	}
}

// treeBytes maps every regular file under dir to its bytes.
func treeBytes(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		out[path] = string(raw)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}
