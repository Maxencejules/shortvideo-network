package statecheck_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	cmttypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/gogoproto/proto"

	"cosmossdk.io/log/v2"

	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal"
	"github.com/Maxencejules/shortvideo-network/chain/internal/statecheck"
	"github.com/Maxencejules/shortvideo-network/chain/internal/statecheck/testhome"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
)

// The tests run the state check on real node homes that testhome builds in
// process (devnet.Init, then app.New on data/application.db, InitChain, and
// committed blocks of signed transactions). Nothing here is a mock.

// allChecks are the checker names of every report, in report order.
var allChecks = []string{
	statecheck.CheckAuthority,
	statecheck.CheckEvidence,
	statecheck.CheckLeaseOffers,
	statecheck.CheckLeases,
	statecheck.CheckPolicy,
	statecheck.CheckVersions,
}

func declared(t *testing.T, upgrades []upgrade.Upgrade) upgrade.Binary {
	t.Helper()
	binary, err := app.DeclaredBinary(upgrades)
	if err != nil {
		t.Fatal(err)
	}
	return binary
}

func run(t *testing.T, home string, height int64, binary upgrade.Binary) *statecheck.Report {
	t.Helper()
	report, err := statecheck.Run(statecheck.Options{Home: home, Height: height, Binary: binary})
	if err != nil {
		t.Fatalf("state check of %s at height %d: %v", home, height, err)
	}
	return report
}

func text(t *testing.T, report *statecheck.Report) string {
	t.Helper()
	var b bytes.Buffer
	if err := report.WriteText(&b); err != nil {
		t.Fatal(err)
	}
	return b.String()
}

// failing returns the names of the checks that failed, and their errors.
func failing(report *statecheck.Report) ([]string, map[string]error) {
	var names []string
	errs := map[string]error{}
	for _, check := range report.Checks {
		if check.Err != nil {
			names = append(names, check.Name)
			errs[check.Name] = check.Err
		}
	}
	return names, errs
}

// councilHome is a home with council state in every authority class the
// checkers read: an executed governed limit applied at execute (a governed
// value), an executed governed limit with a later activation height (a
// pending change), and an OPEN proposal with two approvals (an open count
// and approving seats). heights maps a few committed heights to their app
// hashes. The node is stopped.
func councilHome(t *testing.T) (*testhome.Home, map[int64][]byte) {
	t.Helper()
	h := testhome.New(t, filepath.Join(t.TempDir(), "devnet"))
	heights := map[int64][]byte{}
	mark := func() { heights[h.Height()] = h.App().LastCommitID().Hash }
	h.Block(t)
	mark()
	h.Council(t, "p-value", msgkit.LimitMaxPageLimit, testhome.GovernedLimit(msgkit.LimitMaxPageLimit, 50, 0))
	mark()
	h.Council(t, "p-pending", msgkit.LimitMaxOpenOffers, testhome.GovernedLimit(msgkit.LimitMaxOpenOffers, 7, uint64(h.Height())+60))
	mark()
	h.Propose(t, "p-open", msgkit.LimitMaxEvidenceBatch, testhome.GovernedLimit(msgkit.LimitMaxEvidenceBatch, 9, 0))
	h.Approve(t, "p-open", 2)
	h.Block(t)
	mark()
	h.Stop(t)
	return h, heights
}

// TestStateCheckDigestsAndInvariants is the state check on a stopped home
// with council state (plan 16.3):
//
//   - the report passes for svnd's binary description: gate row 1, every
//     checker ok, the chain id, the height, and the app hash of the latest
//     commit;
//   - it holds one digest per mounted store, exactly the eight stores, each
//     the SHA-256 of that store's ModuleSnapshot as the application itself
//     returns it, and the auth store is labelled;
//   - an earlier height reads that height (its app hash);
//   - the text is deterministic and names no path;
//   - each checker finds a damaged row of its own store: one damage per
//     case, committed into the home, and each case fails exactly the
//     checkers that read that row (the policy, authority, lease, lease
//     offer, evidence, and versions checkers, and the gate's row 6); the I13
//     digest moves for a protected row and stays for the exact key {0x04}.
func TestStateCheckDigestsAndInvariants(t *testing.T) {
	h, heights := councilHome(t)
	binary := declared(t, nil)
	report := run(t, h.Dir, 0, binary)
	if !report.Passed() || report.Gate.Row != 1 {
		t.Fatalf("report did not pass with gate row 1:\n%s", text(t, report))
	}
	if report.ChainID != testhome.ChainID || report.Height != h.Height() || !bytes.Equal(report.AppHash, heights[h.Height()]) {
		t.Fatalf("report chain %s height %d app hash %x; home %s height %d app hash %x",
			report.ChainID, report.Height, report.AppHash, testhome.ChainID, h.Height(), heights[h.Height()])
	}
	var names []string
	for _, check := range report.Checks {
		names = append(names, check.Name)
	}
	if !slices.Equal(names, allChecks) {
		t.Fatalf("checks %v, want %v", names, allChecks)
	}

	// The digests are the application's own store snapshots.
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, filepath.Join(h.Dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: testhome.ChainID})
	if err != nil {
		t.Fatal(err)
	}
	wantStores := append(app.SnapshotStores(), authtypes.StoreKey)
	sort.Strings(wantStores)
	if len(report.Stores) != len(wantStores) {
		t.Fatalf("%d store digests, want %d (%v)", len(report.Stores), len(wantStores), wantStores)
	}
	for i, store := range report.Stores {
		snapshot := chainApp.ModuleSnapshot(wantStores[i])
		if store.Name != wantStores[i] || store.SHA256 != sha256.Sum256(snapshot) || len(snapshot) == 0 {
			t.Fatalf("store digest %d: %s %x, want %s %x", i, store.Name, store.SHA256, wantStores[i], sha256.Sum256(snapshot))
		}
		if store.Sequences != (store.Name == authtypes.StoreKey) {
			t.Fatalf("store %s sequences label %t", store.Name, store.Sequences)
		}
	}
	if err := chainApp.Close(); err != nil {
		t.Fatal(err)
	}

	first := text(t, report)
	if again := text(t, run(t, h.Dir, 0, binary)); again != first {
		t.Fatalf("two reports of one state differ:\n%s\n%s", first, again)
	}
	if strings.Contains(first, h.Dir) || strings.Contains(first, filepath.Dir(h.Dir)) {
		t.Fatalf("report names the home path:\n%s", first)
	}
	for _, store := range report.Stores {
		if !strings.Contains(first, fmt.Sprintf("store %s sha256 %x", store.Name, store.SHA256)) {
			t.Fatalf("report text has no digest line for %s:\n%s", store.Name, first)
		}
	}
	if !strings.HasSuffix(first, "result PASS\n") || !strings.Contains(first, "\ngate row 1 start\n") {
		t.Fatalf("report text:\n%s", first)
	}

	for height, hash := range heights {
		older := run(t, h.Dir, height, binary)
		if older.Height != height || !bytes.Equal(older.AppHash, hash) || !older.Passed() {
			t.Fatalf("height %d: report height %d app hash %x passed %t, want app hash %x", height, older.Height, older.AppHash, older.Passed(), hash)
		}
	}

	params := consensusParams(t, h)
	params.Version.App = 2
	for _, c := range []struct {
		name     string
		store    string
		key      []byte
		value    []byte
		fail     []string
		prefix   map[string]string
		gate     int
		i13Moves bool
	}{
		{name: "policy row under an unknown prefix", store: app.ModulePolicy, key: []byte{0x7f}, value: []byte{1},
			fail: []string{statecheck.CheckPolicy}, prefix: map[string]string{statecheck.CheckPolicy: "layout:"}, gate: 1, i13Moves: true},
		{name: "policy row beside the plan key", store: app.ModulePolicy, key: []byte{0x06, 0x7a}, value: []byte{1},
			fail: []string{statecheck.CheckPolicy}, prefix: map[string]string{statecheck.CheckPolicy: "layout:"}, gate: 1, i13Moves: true},
		{name: "policy applied row without a plan-name row", store: app.ModulePolicy, key: policy.AppliedKey("ghost"), value: u64(5),
			fail: []string{statecheck.CheckPolicy}, prefix: map[string]string{statecheck.CheckPolicy: "I4:"}, gate: 1},
		{name: "policy governed value above its ceiling", store: app.ModulePolicy, key: append([]byte{0x02}, msgkit.LimitMaxEvidenceBatch...), value: u64(1 << 40),
			fail: []string{statecheck.CheckPolicy}, prefix: map[string]string{statecheck.CheckPolicy: "I2:"}, gate: 1, i13Moves: true},
		{name: "authority row beside the policy version", store: authority.ModuleName, key: []byte{0x04, 0x01}, value: u64(9),
			fail: []string{statecheck.CheckAuthority}, prefix: map[string]string{statecheck.CheckAuthority: "I13:"}, gate: 1, i13Moves: true},
		{name: "authority retired governed-value prefix", store: authority.ModuleName, key: append([]byte{0x13}, msgkit.LimitMaxPageLimit...), value: u64(5),
			fail: []string{statecheck.CheckAuthority}, prefix: map[string]string{statecheck.CheckAuthority: "layout:"}, gate: 1, i13Moves: true},
		{name: "authority open count removed", store: authority.ModuleName, key: openKey(3, "seat-1"), value: nil,
			fail: []string{statecheck.CheckAuthority}, prefix: map[string]string{statecheck.CheckAuthority: "I10:"}, gate: 1, i13Moves: true},
		{name: "authority open count without a proposal", store: authority.ModuleName, key: openKey(3, "seat-2"), value: u64(1),
			fail: []string{statecheck.CheckAuthority}, prefix: map[string]string{statecheck.CheckAuthority: "I10:"}, gate: 1, i13Moves: true},
		{name: "authority policy version that does not decode", store: authority.ModuleName, key: []byte{0x04}, value: []byte{1},
			fail:   []string{statecheck.CheckAuthority, statecheck.CheckPolicy},
			prefix: map[string]string{statecheck.CheckAuthority: "panic (a committed row does not decode)", statecheck.CheckPolicy: "panic (a committed row does not decode)"}, gate: 1},
		{name: "authority policy version below the executed proposals", store: authority.ModuleName, key: []byte{0x04}, value: u64(2),
			fail:   []string{statecheck.CheckAuthority, statecheck.CheckPolicy},
			prefix: map[string]string{statecheck.CheckAuthority: "I1:", statecheck.CheckPolicy: "I3:"}, gate: 1},
		{name: "authority retired seat key is a validator key", store: authority.ModuleName, key: retiredValidatorKey(t, h), value: []byte("seat-9"),
			fail: []string{statecheck.CheckAuthority}, prefix: map[string]string{statecheck.CheckAuthority: "I8:"}, gate: 1, i13Moves: true},
		{name: "lease row under an unknown prefix", store: app.ModuleLeases, key: []byte{0x7f}, value: []byte{1},
			fail:   []string{statecheck.CheckLeaseOffers, statecheck.CheckLeases},
			prefix: map[string]string{statecheck.CheckLeaseOffers: "layout:", statecheck.CheckLeases: "layout:"}, gate: 1},
		{name: "lease offer row that does not decode", store: app.ModuleLeases, key: append([]byte{0x01}, "o-1"...), value: []byte{0xff},
			fail:   []string{statecheck.CheckLeaseOffers, statecheck.CheckLeases},
			prefix: map[string]string{statecheck.CheckLeaseOffers: "I6:", statecheck.CheckLeases: "I6:"}, gate: 1},
		{name: "evidence row under an unknown prefix", store: app.ModuleEvidence, key: []byte{0x7f}, value: []byte{1},
			fail: []string{statecheck.CheckEvidence}, prefix: map[string]string{statecheck.CheckEvidence: "layout"}, gate: 1},
		{name: "consensus Version.App differs from the stored app version", store: app.ModuleSVN, key: []byte{0x03}, value: mustMarshal(t, &params),
			fail: []string{statecheck.CheckVersions}, prefix: map[string]string{statecheck.CheckVersions: "I5:"}, gate: 6},
		{name: "lease layout key 3", store: app.ModuleLeases, key: msgkit.LayoutVersionKey, value: u64(3),
			fail:   []string{statecheck.CheckLeases, statecheck.CheckVersions},
			prefix: map[string]string{statecheck.CheckLeases: "layout:", statecheck.CheckVersions: "I6:"}, gate: 6},
	} {
		t.Run(c.name, func(t *testing.T) {
			before := h.Row(t, c.store, c.key)
			h.Damage(t, c.store, c.key, c.value)
			restored := false
			t.Cleanup(func() {
				if !restored {
					h.Damage(t, c.store, c.key, before)
				}
			})
			damaged := run(t, h.Dir, 0, binary)
			names, errs := failing(damaged)
			if !slices.Equal(names, c.fail) || damaged.Passed() || damaged.Gate.Row != c.gate {
				t.Fatalf("failing %v gate row %d passed %t, want failing %v gate row %d:\n%s", names, damaged.Gate.Row, damaged.Passed(), c.fail, c.gate, text(t, damaged))
			}
			damagedText := text(t, damaged)
			for name, prefix := range c.prefix {
				if !strings.HasPrefix(errs[name].Error(), prefix) {
					t.Fatalf("check %s: %v, want prefix %q", name, errs[name], prefix)
				}
				if line := fmt.Sprintf("\ncheck %s FAIL %s\n", name, errs[name]); !strings.Contains(damagedText, line) {
					t.Fatalf("report text has no line %q:\n%s", line, damagedText)
				}
			}
			if moved := damaged.I13 != report.I13; moved != c.i13Moves {
				t.Fatalf("I13 digest moved %t, want %t", moved, c.i13Moves)
			}
			if !strings.HasSuffix(damagedText, "result FAIL\n") {
				t.Fatalf("report text:\n%s", damagedText)
			}
			h.Damage(t, c.store, c.key, before)
			restored = true
			if again := run(t, h.Dir, 0, binary); !again.Passed() || again.I13 != report.I13 {
				t.Fatalf("restored home:\n%s", text(t, again))
			}
		})
	}
}

// TestStateCheckRefusesOtherVersion runs the state check around the
// rehearsal upgrade (rehearsal-1, app version 1 to 2) for two binary
// descriptions, svnd's (P = 1) and the rehearsal binary B2's (P = 2), and
// compares every gate decision with the application's own load gate
// (app.New on the same home with the same handlers):
//
//	state                          svnd                      B2
//	plan above L+1                 row 1, pass               row 4 refused, pass
//	plan at L+1 (the halt)         row 2 refused, pass       row 3 window, pass
//	after the upgrade (L = h)      row 5 refused, FAIL       row 1, pass
//
// The I13 digest is equal before and after the upgrade (the upgrade moved
// only the runner's own rows and the policy version), and the state at h-1
// is still readable from the upgraded home. A home with no committed block
// is refused.
func TestStateCheckRefusesOtherVersion(t *testing.T) {
	empty := testhome.New(t, filepath.Join(t.TempDir(), "empty"))
	empty.Stop(t)
	if _, err := statecheck.Run(statecheck.Options{Home: empty.Dir, Binary: declared(t, nil)}); err == nil || !strings.Contains(err.Error(), "no committed block") {
		t.Fatalf("home with no committed block: %v", err)
	}
	h := testhome.New(t, filepath.Join(t.TempDir(), "devnet"))
	h.Block(t)
	plan := h.Height() + 5 + int64(msgkit.MinActivationLeadBlocks) + 2
	h.Council(t, "p-upgrade", testhome.UpgradeParameterKey, testhome.SoftwareUpgrade(rehearsal.UpgradeName, uint64(plan)))
	svnd, b2 := declared(t, nil), declared(t, rehearsal.Upgrades())

	check := func(label string, upgrades []upgrade.Upgrade, binary upgrade.Binary, wantRow int, wantPass bool) *statecheck.Report {
		t.Helper()
		report := run(t, h.Dir, 0, binary)
		if report.Gate.Row != wantRow || report.Passed() != wantPass {
			t.Fatalf("%s: gate row %d passed %t, want row %d passed %t:\n%s", label, report.Gate.Row, report.Passed(), wantRow, wantPass, text(t, report))
		}
		if names, _ := failing(report); len(names) != 0 && wantRow != 5 {
			t.Fatalf("%s: failing checks %v", label, names)
		}
		appGateAgrees(t, label, h, upgrades, report)
		return report
	}

	h.BlocksTo(t, plan-3)
	h.Stop(t)
	check("plan above L+1, svnd", nil, svnd, 1, true)
	early := check("plan above L+1, B2", rehearsal.Upgrades(), b2, 4, true)
	if !strings.Contains(early.Gate.Refusal, "too early") {
		t.Fatalf("row 4 refusal %q", early.Gate.Refusal)
	}

	h.Start(t)
	h.BlocksTo(t, plan-1)
	h.Stop(t)
	halt := check("plan at L+1, svnd", nil, svnd, 2, true)
	window := check("plan at L+1, B2", rehearsal.Upgrades(), b2, 3, true)
	if window.Gate.Upgrade.Name != rehearsal.UpgradeName || !strings.Contains(text(t, window), "gate row 3 start in the upgrade window: the next block applies rehearsal-1 (app version 1 to 2)\n") {
		t.Fatalf("row 3 report:\n%s", text(t, window))
	}
	if !strings.Contains(text(t, halt), "gate row 2 refused: upgrade rehearsal-1 is due at height") || !strings.HasSuffix(text(t, halt), "result PASS\n") {
		t.Fatalf("row 2 report:\n%s", text(t, halt))
	}

	h.StartWith(t, rehearsal.Upgrades())
	h.Block(t)
	h.Applied()
	h.Stop(t)
	newer := check("after the upgrade, svnd", nil, svnd, 5, false)
	if !strings.HasPrefix(newer.Gate.Refusal, "state is newer than this binary") || !strings.Contains(text(t, newer), "\nresult FAIL\n") {
		t.Fatalf("row 5 report:\n%s", text(t, newer))
	}
	after := check("after the upgrade, B2", rehearsal.Upgrades(), b2, 1, true)
	if after.I13 != window.I13 || after.I13 != halt.I13 {
		t.Fatalf("I13 digest moved across the upgrade: %x, before %x", after.I13, window.I13)
	}
	if after.Height != plan || bytes.Equal(after.AppHash, window.AppHash) {
		t.Fatalf("after the upgrade: height %d app hash %x; plan %d, app hash before %x", after.Height, after.AppHash, plan, window.AppHash)
	}
	historical := run(t, h.Dir, plan-1, svnd)
	if historical.Gate.Row != 2 || !bytes.Equal(historical.AppHash, halt.AppHash) || !historical.Passed() {
		t.Fatalf("height %d of the upgraded home:\n%s", plan-1, text(t, historical))
	}
}

// appGateAgrees opens the home with app.New and upgrades, which runs the
// application's load gate on the latest committed state, and requires the
// state check's decision to be the same: a start for rows 1 and 3, and the
// application's refusal text, with its row, for every other row.
func appGateAgrees(t *testing.T, label string, h *testhome.Home, upgrades []upgrade.Upgrade, report *statecheck.Report) {
	t.Helper()
	db, err := dbm.NewDB("application", dbm.GoLevelDBBackend, filepath.Join(h.Dir, "data"))
	if err != nil {
		t.Fatal(err)
	}
	chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: testhome.ChainID, Upgrades: upgrades})
	if err == nil {
		defer chainApp.Close()
	} else {
		_ = db.Close()
	}
	switch {
	case report.Gate.Started() && err != nil:
		t.Fatalf("%s: the state check starts the binary (row %d); the application refuses it: %v", label, report.Gate.Row, err)
	case !report.Gate.Started() && err == nil:
		t.Fatalf("%s: the state check refuses the binary (row %d); the application starts it", label, report.Gate.Row)
	case err != nil:
		want := fmt.Sprintf("load gate at height %d refused this binary (row %d): %s", report.Height, report.Gate.Row, report.Gate.Refusal)
		if err.Error() != want {
			t.Fatalf("%s: application refusal %q, state check %q", label, err, want)
		}
	}
}

// TestStateCheckRefusesRunningNode: while the node's application holds the
// database, the state check refuses the home and names the lock; once the
// node stops, the same home is checked.
func TestStateCheckRefusesRunningNode(t *testing.T) {
	h := testhome.New(t, filepath.Join(t.TempDir(), "devnet"))
	h.BlocksTo(t, 3)
	_, err := statecheck.Run(statecheck.Options{Home: h.Dir, Binary: declared(t, nil)})
	if err == nil || !strings.Contains(err.Error(), "locked by another process (a running node holds it)") {
		t.Fatalf("running node: %v", err)
	}
	h.Stop(t)
	if report := run(t, h.Dir, 0, declared(t, nil)); !report.Passed() || report.Height != 3 {
		t.Fatalf("stopped node:\n%s", text(t, report))
	}
}

// TestStateCheckWritesNothing: every file of the home has the same bytes,
// and no file is added or removed, after a passing and after a failing
// state check.
func TestStateCheckWritesNothing(t *testing.T) {
	h, _ := councilHome(t)
	before := fileDigests(t, h.Dir)
	if report := run(t, h.Dir, 0, declared(t, nil)); !report.Passed() {
		t.Fatalf("report:\n%s", text(t, report))
	}
	if report := run(t, h.Dir, 0, declared(t, rehearsal.Upgrades())); report.Gate.Row != 6 || report.Passed() {
		t.Fatalf("B2 on a home with no plan:\n%s", text(t, report))
	}
	after := fileDigests(t, h.Dir)
	if len(before) == 0 || !mapsEqual(before, after) {
		t.Fatalf("home files changed:\nbefore %v\nafter  %v", before, after)
	}
}

// TestStateCheckRefusesBadInput: no genesis file, no application database,
// a negative height, a height above the latest commit, and a binary
// description that upgrade.Binary.Validate refuses are errors, not reports.
func TestStateCheckRefusesBadInput(t *testing.T) {
	h, _ := councilHome(t)
	binary := declared(t, nil)
	noDB := t.TempDir()
	if err := os.MkdirAll(filepath.Join(noDB, "config"), 0o700); err != nil {
		t.Fatal(err)
	}
	genesis, err := os.ReadFile(filepath.Join(h.Dir, "config", "genesis.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(noDB, "config", "genesis.json"), genesis, 0o600); err != nil {
		t.Fatal(err)
	}
	bad := binary
	bad.AppVersion = 0
	for _, c := range []struct {
		name string
		opts statecheck.Options
		want string
	}{
		{"missing home", statecheck.Options{Home: filepath.Join(t.TempDir(), "absent"), Binary: binary}, "has no genesis file"},
		{"no application database", statecheck.Options{Home: noDB, Binary: binary}, "has no application database"},
		{"negative height", statecheck.Options{Home: h.Dir, Height: -1, Binary: binary}, "negative"},
		{"height above the latest commit", statecheck.Options{Home: h.Dir, Height: h.Height() + 1, Binary: binary}, "failed to load version"},
		{"binary description refused", statecheck.Options{Home: h.Dir, Binary: bad}, "binary description"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := statecheck.Run(c.opts); err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("error %v, want %q", err, c.want)
			}
		})
	}
}

// TestStatecheckCommand runs the statecheck subcommand as svnd builds it:
// it prints the report of the home for the binary description of its
// handlers and returns nil on a passing report, ErrFailed after printing a
// failing one, and an error without a report for a home it cannot read.
func TestStatecheckCommand(t *testing.T) {
	h, _ := councilHome(t)
	execute := func(upgrades []upgrade.Upgrade, args ...string) (string, error) {
		cmd := statecheck.Command(upgrades)
		var out bytes.Buffer
		cmd.SetOut(&out)
		cmd.SetErr(&out)
		cmd.SetArgs(args)
		err := cmd.Execute()
		return out.String(), err
	}
	out, err := execute(nil, "--home", h.Dir)
	if err != nil || out != text(t, run(t, h.Dir, 0, declared(t, nil))) {
		t.Fatalf("passing home: %v\n%s", err, out)
	}
	out, err = execute(rehearsal.Upgrades(), "--home", h.Dir)
	if !errors.Is(err, statecheck.ErrFailed) || !strings.Contains(out, "binary upgrade rehearsal-1 1 2\n") || !strings.HasSuffix(out, "result FAIL\n") {
		t.Fatalf("B2 on a home with no plan: %v\n%s", err, out)
	}
	out, err = execute(nil, "--home", h.Dir, "--height", "2")
	if err != nil || !strings.Contains(out, "\nheight 2\n") {
		t.Fatalf("--height 2: %v\n%s", err, out)
	}
	if out, err := execute(nil, "--home", t.TempDir()); err == nil || strings.Contains(out, "result") {
		t.Fatalf("empty home: %v\n%s", err, out)
	}
	if _, err := execute(nil); err == nil || !strings.Contains(err.Error(), `required flag(s) "home" not set`) {
		t.Fatalf("no --home: %v", err)
	}
}

func u64(v uint64) []byte { return binary.BigEndian.AppendUint64(nil, v) }

// openKey is the authority open-count key 0x14 || base version || seat id.
func openKey(base uint64, seat string) []byte {
	return append(binary.BigEndian.AppendUint64([]byte{0x14}, base), seat...)
}

func mustMarshal(t *testing.T, msg proto.Message) []byte {
	t.Helper()
	raw, err := proto.Marshal(msg)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// consensusParams decodes the stored consensus params of the stopped home.
func consensusParams(t *testing.T, h *testhome.Home) cmtproto.ConsensusParams {
	t.Helper()
	var params cmtproto.ConsensusParams
	if err := proto.Unmarshal(h.Row(t, app.ModuleSVN, []byte{0x03}), &params); err != nil {
		t.Fatal(err)
	}
	if params.Version == nil || params.Version.App != app.GenesisAppVersion {
		t.Fatalf("stored consensus params version %v", params.Version)
	}
	return params
}

// retiredValidatorKey is the retired-seat key (0x15) of genesis validator
// 0's consensus key.
func retiredValidatorKey(t *testing.T, h *testhome.Home) []byte {
	t.Helper()
	doc, err := cmttypes.GenesisDocFromFile(filepath.Join(h.Dir, "config", "genesis.json"))
	if err != nil {
		t.Fatal(err)
	}
	return append([]byte{0x15}, doc.Validators[0].PubKey.Bytes()...)
}

// fileDigests maps every file under dir to the SHA-256 of its bytes.
func fileDigests(t *testing.T, dir string) map[string][sha256.Size]byte {
	t.Helper()
	out := map[string][sha256.Size]byte{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[path] = sha256.Sum256(raw)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func mapsEqual(a, b map[string][sha256.Size]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if w, ok := b[k]; !ok || w != v {
			return false
		}
	}
	return true
}
