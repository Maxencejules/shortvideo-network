// Package statecheck reads the committed application state of a stopped
// svnd node home, or of a copy of one, and reports for one committed
// height (plan sections 11.8 and 12.7):
//
//   - the chain id, the height, and the application hash of that height
//   - the load gate decision (upgrade.Gate, rows 1 to 6) for a given binary
//     description (upgrade.Binary) on that state
//   - one SHA-256 per mounted store over app.App.ModuleSnapshot (every key
//     and value in key order); the auth account store is labelled because
//     it holds account sequences, which every included transaction moves
//   - one SHA-256 over the rows the upgrade runner's I13 self-check
//     protects: every x/authority row except the exact key {0x04} (the
//     policy version, which an upgrade moves by one), and the x/policy
//     safety caps (0x01), governed values (0x02), and pending changes
//     (0x03). Two reports of one chain from before and after an upgrade
//     carry the same value exactly when I13 held.
//   - every invariant checker: x/policy (policytest.CheckInvariantsAt:
//     layout, I2, I3, I4, and the policy parts of I5, I6, I7), x/authority
//     (checkAuthority: layout and prefixes, the I13 exact-key rule, I1 in
//     its state-only form, I7, I8, I9, I10, I11), x/leases
//     (leasestest.CheckInvariants and CheckOfferIntegrity), x/evidence
//     (evidencetest.CheckInvariants), and the versions check (the state
//     parts of I5 and I6: the stored app version equals the stored
//     ConsensusParams.Version.App, and every layout key equals its version
//     map entry)
//
// The report passes when every checker passes and the gate does not refuse
// with row 5 (the state is newer than the binary, whose checkers cannot
// read it) or row 6 (the state is inconsistent or does not match the
// binary). Rows 2 and 4 refuse to start the binary on a consistent state
// (an upgrade is due; the binary is too early): the report states the
// refusal and still passes when every checker passes.
//
// Run opens the application database (data/application.db) in goleveldb's
// read-only mode, so nothing in the home is written, and a database a
// running node holds is refused: the node's exclusive lock blocks the
// shared lock of a read-only open. It never starts CometBFT, reads no
// validator key, and opens no network connection.
//
// The checkers are the test-support packages x/leases/leasestest,
// x/evidence/evidencetest, and x/policy/policytest. svnd links this package
// only when built with the build tag svn_statecheck (the statecheck
// subcommand, chain/cmd/svnd/statecheck_tag.go); the default svnd build
// links none of them (chain/app TestLinkedBinaryOmitsTestHooks). The tagged
// build is not a release.
package statecheck

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"

	"cosmossdk.io/log/v2"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/upgrade"
	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence/evidencetest"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases/leasestest"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy/policytest"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers"
)

// Options name what Run reads and the binary whose gate decision it
// reports.
type Options struct {
	// Home is a node home: the directory svnd start --home takes. Run reads
	// config/genesis.json (the chain id) and data/application.db.
	Home string
	// Height is the committed height to read; 0 reads the latest one.
	Height int64
	// Binary is the binary description the gate decides on: its app
	// version, module versions, and upgrade handlers
	// (app.DeclaredBinary).
	Binary upgrade.Binary
}

// StoreDigest is the SHA-256 of one mounted store's ModuleSnapshot.
type StoreDigest struct {
	Name   string
	SHA256 [sha256.Size]byte
	// Sequences is true for the auth account store, which holds the
	// account sequences.
	Sequences bool
}

// GateResult is the load gate decision for the binary on the state.
type GateResult struct {
	// Row is the gate row that holds: 1 or 3 when the binary starts, 2, 4,
	// 5, or 6 when it is refused.
	Row int
	// Upgrade is the handler the next block runs (row 3 only).
	Upgrade upgrade.Upgrade
	// Refusal is the refusal message (rows 2, 4, 5, 6).
	Refusal string
}

// Started is true when the binary may start on the state (row 1 or 3).
func (g GateResult) Started() bool { return g.Row == 1 || g.Row == 3 }

// CheckResult is the outcome of one invariant checker; Err is nil when
// every invariant it checks holds.
type CheckResult struct {
	Name string
	Err  error
}

// Report is what Run read.
type Report struct {
	ChainID string
	Height  int64
	AppHash []byte
	Binary  upgrade.Binary
	Gate    GateResult
	// Stores holds every mounted store, by name.
	Stores []StoreDigest
	// I13 is the SHA-256 of the rows the upgrade runner's I13 self-check
	// protects (i13Rows).
	I13 [sha256.Size]byte
	// Checks holds every invariant checker, by name.
	Checks []CheckResult
}

// Passed is true when every checker passed and the gate did not refuse
// with row 5 or 6.
func (r *Report) Passed() bool {
	if r.Gate.Row == 5 || r.Gate.Row == 6 {
		return false
	}
	for _, check := range r.Checks {
		if check.Err != nil {
			return false
		}
	}
	return true
}

// Check names, in report order.
const (
	CheckAuthority   = "authority"
	CheckEvidence    = "evidence"
	CheckLeaseOffers = "lease-offers"
	CheckLeases      = "leases"
	CheckPolicy      = "policy"
	CheckVersions    = "versions"
)

// Run reads the home of opts at opts.Height and returns the report. An
// error means nothing was checked: the home has no genesis file or no
// application database, the database is held by a running node, or the
// height has no committed state.
func Run(opts Options) (*Report, error) {
	if err := opts.Binary.Validate(); err != nil {
		return nil, fmt.Errorf("binary description: %w", err)
	}
	if opts.Height < 0 {
		return nil, fmt.Errorf("height %d is negative", opts.Height)
	}
	chainID, err := readChainID(opts.Home)
	if err != nil {
		return nil, err
	}
	dir := filepath.Join(opts.Home, "data")
	if _, err := os.Stat(filepath.Join(dir, "application.db")); err != nil {
		return nil, fmt.Errorf("home %s has no application database: %w", opts.Home, err)
	}
	db, err := openReadOnly("application", dir)
	if err != nil {
		return nil, err
	}
	// SkipLoad: app.New mounts the stores and builds the keepers without
	// loading a version or running the load gate; Run loads the height
	// itself and decides the gate for opts.Binary.
	chainApp, err := app.New(app.Config{Logger: log.NewNopLogger(), DB: db, ChainID: chainID, SkipLoad: true})
	if err != nil {
		_ = db.Close()
		return nil, err
	}
	defer chainApp.Close()
	if err := load(chainApp, opts.Height); err != nil {
		return nil, err
	}
	keys, err := kvStoreKeys(chainApp.CommitMultiStore())
	if err != nil {
		return nil, err
	}
	rd, err := newReaders(keys)
	if err != nil {
		return nil, err
	}
	commit := chainApp.LastCommitID()
	report := &Report{
		ChainID: chainID,
		Height:  commit.Version,
		AppHash: append([]byte(nil), commit.Hash...),
		Binary:  opts.Binary,
	}
	for _, name := range sortedNames(keys) {
		report.Stores = append(report.Stores, StoreDigest{
			Name:      name,
			SHA256:    sha256.Sum256(chainApp.ModuleSnapshot(name)),
			Sequences: name == authtypes.StoreKey,
		})
	}
	header := cmtproto.Header{ChainID: chainID, Height: commit.Version}
	branch := func() sdk.Context {
		return sdk.NewContext(chainApp.CommitMultiStore().CacheMultiStore(), header, false, log.NewNopLogger())
	}
	report.I13 = sha256.Sum256(i13Rows(branch(), keys))
	state, stateErr := readStoredState(branch(), keys, rd.policy, uint64(commit.Version))
	report.Gate = decide(state, stateErr, opts.Binary)
	report.Checks = []CheckResult{
		run(CheckAuthority, func() error { return checkAuthority(branch(), rd.authority, keys[authority.ModuleName]) }),
		run(CheckEvidence, func() error { return evidencetest.CheckInvariants(branch(), rd.evidence, rd.leases) }),
		run(CheckLeaseOffers, func() error { return leasestest.CheckOfferIntegrity(branch(), rd.leases, rd.content) }),
		run(CheckLeases, func() error { return leasestest.CheckInvariants(branch(), rd.leases, rd.providers, nil) }),
		run(CheckPolicy, func() error {
			return policytest.CheckInvariantsAt(branch(), rd.policy, rd.authority, policy.ConsensusVersion)
		}),
		run(CheckVersions, func() error { return checkVersions(state, stateErr) }),
	}
	return report, nil
}

// load loads height (0: the latest committed height) into chainApp. A
// home with no committed block has no state to check.
func load(chainApp *app.App, height int64) error {
	var err error
	if height == 0 {
		err = chainApp.LoadLatestVersion()
	} else {
		err = chainApp.LoadVersion(height)
	}
	if err != nil {
		return err
	}
	if chainApp.LastBlockHeight() == 0 {
		return errors.New("the home has no committed block")
	}
	return nil
}

// kvStoreKeys returns the mounted store keys of cms by store name.
func kvStoreKeys(cms storetypes.CommitMultiStore) (map[string]*storetypes.KVStoreKey, error) {
	named, ok := cms.(interface {
		StoreKeysByName() map[string]storetypes.StoreKey
	})
	if !ok {
		return nil, fmt.Errorf("commit multistore %T does not name its stores", cms)
	}
	out := map[string]*storetypes.KVStoreKey{}
	for name, key := range named.StoreKeysByName() {
		kv, ok := key.(*storetypes.KVStoreKey)
		if !ok {
			return nil, fmt.Errorf("store %s is mounted with a %T key, not a KV store key", name, key)
		}
		out[name] = kv
	}
	return out, nil
}

// readers are read-only keepers over the mounted stores, wired as app.New
// wires the read surfaces each checker uses. newReaders also requires the
// svn store, which holds the consensus params the gate reads.
type readers struct {
	authority authority.Keeper
	policy    policy.Keeper
	providers providers.Keeper
	content   content.Keeper
	leases    leases.Keeper
	evidence  evidence.Keeper
}

func newReaders(keys map[string]*storetypes.KVStoreKey) (readers, error) {
	for _, name := range []string{app.ModuleSVN, authority.ModuleName, app.ModulePolicy, app.ModuleProviders, app.ModuleContent, app.ModuleLeases, app.ModuleEvidence} {
		if keys[name] == nil {
			return readers{}, fmt.Errorf("no mounted store %s", name)
		}
	}
	read := authority.NewKeeper(keys[authority.ModuleName])
	pol := policy.NewKeeper(keys[app.ModulePolicy], policy.Deps{Versions: read, Decisions: read})
	prov := providers.NewKeeper(keys[app.ModuleProviders], providers.Deps{Roles: read, Limits: pol})
	cont := content.NewKeeper(keys[app.ModuleContent], content.Deps{Roles: read, Params: read, Limits: pol})
	lease := leases.NewKeeper(keys[app.ModuleLeases], leases.Deps{Roles: read, Limits: pol, Params: read, Policy: read, Providers: prov, Content: cont})
	ev := evidence.NewKeeper(keys[app.ModuleEvidence], evidence.Deps{Roles: read, Limits: pol, Leases: lease, Content: cont})
	return readers{authority: read.WithPolicy(pol), policy: pol, providers: prov, content: cont, leases: lease, evidence: ev}, nil
}

// run calls check and turns a panic (a keeper read of a committed row that
// does not decode) into a failure of that check.
func run(name string, check func() error) (result CheckResult) {
	result.Name = name
	defer func() {
		if p := recover(); p != nil {
			result.Err = fmt.Errorf("panic (a committed row does not decode): %v", p)
		}
	}()
	result.Err = check()
	return result
}

func sortedNames[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for name := range m {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}
