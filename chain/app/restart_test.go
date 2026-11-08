package app_test

import (
	"bytes"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"

	"cosmossdk.io/log/v2"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
)

// TestRestartLoadsCommittedState reopens a node's application database the
// way svnd start does after a stop: a goleveldb database on disk, then
// LoadLatestVersion and the load gate. Every mounted store must load at the
// committed version, including registry stores that hold no record yet and
// the x/policy store; the gate starts the binary with row 1, and ABCI Info
// reports the committed app version.
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
	if decision := second.GateDecisionForTest(); decision.Row != 1 || decision.Window {
		t.Fatalf("load gate decision %+v, want row 1", decision)
	}
	if got := second.AppVersion(); got != app.GenesisAppVersion {
		t.Fatalf("reopened ABCI Info app version %d", got)
	}
}

// TestAppRefusesScaffoldLayout: a committed store that keeps a layout key
// (policy, providers, content, leases, evidence, authority) in the
// goal-05/06 scaffold layout (no layout version key, the module genesis
// JSON at key [0x01], which sits inside the registry record prefix and,
// for evidence, the record prefix) or with another layout version stops
// app.New on restart at the load gate (row 6: a layout key that differs
// from the persisted version map entry), instead of being misread as
// records (an export or a Providers query of such a store failed to decode
// the JSON as a record). So does a home a goal-09 binary wrote: its policy
// slot is the scaffold layout without an app version or a version map, and
// its authority store has no layout key. A store in this binary's layout
// reopens (TestRestartLoadsCommittedState).
func TestAppRefusesScaffoldLayout(t *testing.T) {
	for _, c := range []struct {
		name     string
		store    string
		scaffold []byte
		version  uint64
		goal09   bool
		want     string
	}{
		{name: "providers scaffold", store: app.ModuleProviders, scaffold: []byte(`{"providers":[]}`)},
		{name: "content scaffold", store: app.ModuleContent, scaffold: []byte(`{"contents":[],"tombstones":[]}`)},
		{name: "providers layout 1", store: app.ModuleProviders, version: 1},
		{name: "content layout 3", store: app.ModuleContent, version: 3},
		{name: "leases scaffold", store: app.ModuleLeases, scaffold: []byte(`{"offers":[],"leases":[]}`)},
		{name: "leases layout 1", store: app.ModuleLeases, version: 1},
		{name: "leases layout 3", store: app.ModuleLeases, version: 3},
		{name: "evidence scaffold", store: app.ModuleEvidence, scaffold: []byte(`{"records":[],"current_epoch":"0"}`)},
		{name: "evidence layout 1", store: app.ModuleEvidence, version: 1},
		{name: "evidence layout 3", store: app.ModuleEvidence, version: 3},
		{name: "policy scaffold", store: app.ModulePolicy, scaffold: []byte(`{}`)},
		{name: "policy layout 1", store: app.ModulePolicy, version: 1},
		{name: "policy layout 3", store: app.ModulePolicy, version: 3},
		{name: "authority layout 1", store: authority.ModuleName, version: 1},
		{name: "authority layout 3", store: authority.ModuleName, version: 3},
		{name: "goal-09 home", goal09: true, want: "the state was written by a binary from before goal 10 (goals 05 to 09), which this binary cannot read or migrate; initialize a new home"},
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
			switch {
			case c.goal09:
				first.WriteGoal09LayoutForTest(modules[app.ModulePolicy])
			case c.scaffold != nil:
				first.WriteScaffoldLayoutForTest(c.store, c.scaffold)
			default:
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
				t.Fatal("app.New loaded a store in another layout")
			}
			want := c.want
			if want == "" {
				want = c.store + " store has "
			}
			if !strings.Contains(err.Error(), "load gate at height 2 refused this binary (row 6): ") || !strings.Contains(err.Error(), want) {
				t.Fatalf("error does not name the gate row, the height, and %q: %v", want, err)
			}
		})
	}
}

// TestLoadGateRefusesDamagedUpgradeRows: a committed upgrade row that does
// not decode stops app.New at the load gate with row 6 instead of a panic
// or a start: the stored app version (x/policy 0x04) of 3 bytes, a version
// map row (0x05 || module) of 3 bytes, an upgrade plan (0x06) of 5 bytes
// (the plan source reports an integrity error), and missing consensus
// params (svn 0x03). The keeper read panics become the gate's row 6 error.
func TestLoadGateRefusesDamagedUpgradeRows(t *testing.T) {
	for _, c := range []struct {
		name  string
		store string
		key   []byte
		value []byte
		want  string
	}{
		{"app version 3 bytes", app.ModulePolicy, []byte{0x04}, []byte{1, 2, 3}, "committed state does not decode"},
		{"version map row 3 bytes", app.ModulePolicy, append([]byte{0x05}, "leases"...), []byte{1, 2, 3}, "committed state does not decode"},
		{"plan 5 bytes", app.ModulePolicy, []byte{0x06}, []byte{1, 2, 3, 4, 5}, "read upgrade plan"},
		{"consensus params missing", app.ModuleSVN, []byte{0x03}, nil, "read consensus params"},
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
			first.WriteCommittedRowForTest(c.store, c.key, c.value)
			if err := first.Close(); err != nil {
				t.Fatal(err)
			}
			_ = db.Close()

			second, db2, err := open()
			defer db2.Close()
			if err == nil {
				_ = second.Close()
				t.Fatal("app.New started on a damaged upgrade row")
			}
			if !strings.Contains(err.Error(), "load gate at height 2 refused this binary (row 6): ") || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want row 6 and %q", err, c.want)
			}
		})
	}
}
