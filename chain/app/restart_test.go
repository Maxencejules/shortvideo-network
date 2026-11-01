package app_test

import (
	"bytes"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"

	"github.com/Maxencejules/shortvideo-network/chain/app"
)

// TestRestartLoadsCommittedState reopens a node's application database the
// way svnd start does after a stop: a goleveldb database on disk, then
// LoadLatestVersion. Every mounted store must load at the committed
// version, including registry stores that hold no record yet.
func TestRestartLoadsCommittedState(t *testing.T) {
	n := newRegNet()
	dir := t.TempDir()
	open := func() (*app.App, dbm.DB) {
		t.Helper()
		db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, dir)
		if err != nil {
			t.Fatal(err)
		}
		chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: n.doc.ChainID})
		if err != nil {
			_ = db.Close()
			t.Fatalf("open: %v", err)
		}
		return chainApp, db
	}
	first, db := open()
	modules, err := app.AppStateFromDocument(first.EncodingConfig().Codec, n.doc)
	if err != nil {
		t.Fatal(err)
	}
	params := consensusParams()
	if _, err := first.InitChain(&abci.RequestInitChain{
		ChainId: n.doc.ChainID, Time: n.doc.Time(), AppStateBytes: mustJSON(t, modules),
		InitialHeight: 1, Validators: validatorUpdates(t, n.doc), ConsensusParams: &params,
	}); err != nil {
		t.Fatal(err)
	}
	for h := int64(1); h <= 3; h++ {
		if _, err := first.FinalizeBlock(&abci.RequestFinalizeBlock{Height: h, Time: n.doc.Time(), Hash: bytes32(byte(h))}); err != nil {
			t.Fatal(err)
		}
		if _, err := first.Commit(); err != nil {
			t.Fatal(err)
		}
	}
	committed := first.LastCommitID()
	snapshot := first.FullSnapshot()
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	_ = db.Close()

	second, db2 := open()
	defer db2.Close()
	if got := second.LastCommitID(); got.Version != committed.Version || !bytes.Equal(got.Hash, committed.Hash) {
		t.Fatalf("reopened at %d %X, committed %d %X", got.Version, got.Hash, committed.Version, committed.Hash)
	}
	if !bytes.Equal(snapshot, second.FullSnapshot()) {
		t.Fatal("reopened state differs")
	}
}

// TestAppRefusesScaffoldLayout: a committed registry store in the
// goal-05/06 scaffold layout (no layout version key, the module genesis
// JSON at key [0x01], which sits inside the registry record prefix) or
// with another layout version stops app.New on restart, instead of being
// misread as registry records (an export or a Providers query of such a
// store failed to decode the JSON as a record). A store in this binary's
// layout reopens (TestRestartLoadsCommittedState).
func TestAppRefusesScaffoldLayout(t *testing.T) {
	for _, c := range []struct {
		name     string
		store    string
		scaffold []byte
		version  uint64
	}{
		{"providers scaffold", app.ModuleProviders, []byte(`{"providers":[]}`), 0},
		{"content scaffold", app.ModuleContent, []byte(`{"contents":[],"tombstones":[]}`), 0},
		{"providers layout 1", app.ModuleProviders, nil, 1},
		{"content layout 3", app.ModuleContent, nil, 3},
	} {
		t.Run(c.name, func(t *testing.T) {
			n := newRegNet()
			dir := t.TempDir()
			open := func() (*app.App, dbm.DB, error) {
				db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, dir)
				if err != nil {
					t.Fatal(err)
				}
				chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: n.doc.ChainID})
				return chainApp, db, err
			}
			first, db, err := open()
			if err != nil {
				t.Fatal(err)
			}
			modules, err := app.AppStateFromDocument(first.EncodingConfig().Codec, n.doc)
			if err != nil {
				t.Fatal(err)
			}
			params := consensusParams()
			if _, err := first.InitChain(&abci.RequestInitChain{
				ChainId: n.doc.ChainID, Time: n.doc.Time(), AppStateBytes: mustJSON(t, modules),
				InitialHeight: 1, Validators: validatorUpdates(t, n.doc), ConsensusParams: &params,
			}); err != nil {
				t.Fatal(err)
			}
			if _, err := first.FinalizeBlock(&abci.RequestFinalizeBlock{Height: 1, Time: n.doc.Time(), Hash: bytes32(1)}); err != nil {
				t.Fatal(err)
			}
			if _, err := first.Commit(); err != nil {
				t.Fatal(err)
			}
			if c.scaffold != nil {
				first.WriteScaffoldLayoutForTest(c.store, c.scaffold)
			} else {
				first.WriteLayoutVersionForTest(c.store, c.version)
			}
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			_ = db.Close()

			second, db2, err := open()
			defer db2.Close()
			if err == nil {
				_ = second.Close()
				t.Fatal("app.New loaded a registry store in another layout")
			}
			if !strings.Contains(err.Error(), c.store+" store at height 2") {
				t.Fatalf("error does not name the store and height: %v", err)
			}
		})
	}
}
