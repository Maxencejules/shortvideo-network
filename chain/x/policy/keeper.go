// Package policy is the x/policy module: the fixed protocol safety caps,
// the governed resource bounds, pending governed changes, the upgrade plan
// and the applied-upgrade history, the persisted module version map, and
// the app version.
//
// x/policy has no message service. x/authority is the council: it decides
// every governed change and calls the writer methods below (SetLimit,
// SchedulePending, ScheduleUpgrade, CancelUpgrade) on the nested cache of an
// executed proposal, so a policy write never outlives a failed or kept
// proposal. The app writes the version rows (InitVersions at InitChain,
// RecordUpgrade from the upgrade runner). Each writer runs every check
// before its first write, so a failure writes nothing.
//
// Everything in this directory except the _test.go files (and the
// policytest subpackage, which is test support) runs inside chain
// execution. It reads block data only from sdk.Context, iterates stores in
// key order with a bound, and never reads the local clock, randomness, the
// environment, files, or the network (TestDeterministicSources). It reads
// the policy version and the blocked decision rows through the interfaces
// below and never imports x/authority or the app (TestKeeperImports).
package policy

import (
	"fmt"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	policyv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const (
	// ModuleName is the module name and the store key name.
	ModuleName = "policy"

	// ConsensusVersion is the store layout version, written at genesis under
	// msgkit.LayoutVersionKey. Version 1 (msgkit.ScaffoldLayoutVersion) is
	// the scaffold slot: one key [0x01] holding the module genesis JSON. No
	// migration from version 1 is registered; homes from goal 09 are
	// re-initialized (decision request DR-10-04, interim).
	ConsensusVersion uint64 = 2

	// MaxActivationLeadBlocks bounds the distance between the block that
	// writes a pending change or an upgrade plan and its height. It is a
	// chain safety constant, PROPOSED in decision request DR-10-15; it is
	// not an owner decision and not fail-closed (a new number). Changing it
	// is a binary upgrade.
	MaxActivationLeadBlocks uint64 = 1_000_000

	// MaxVersionMapModules bounds the persisted module version map. It is an
	// implementation bound above the module count of the app (eight), not a
	// policy number.
	MaxVersionMapModules = 32
)

// VersionSource reads the council policy version: 1 until the first
// executed proposal, then +1 per executed proposal. x/authority implements
// it (Keeper.PolicyVersion); the counter stays in the authority store
// (decision request DR-10-20).
type VersionSource interface {
	PolicyVersion(ctx sdk.Context) uint64
}

// DecisionSource reads the blocked decision rows of the authority genesis
// config, in the order the config stores them. Query/Policy and
// ExportGenesis return them; x/policy stores no copy.
type DecisionSource interface {
	BlockedDecisions(ctx sdk.Context) []*typesv1.BlockedDecisionParameter
}

// Deps are the read surfaces this module consumes, declared here and
// satisfied structurally. Both are required.
type Deps struct {
	Versions  VersionSource
	Decisions DecisionSource
}

// Keeper reads and writes the policy store.
type Keeper struct {
	key   *storetypes.KVStoreKey
	deps  Deps
	store storetypes.KVStore
}

// NewKeeper returns the keeper over key. A nil key or a missing dependency
// is a wiring error and panics before the chain starts.
func NewKeeper(key *storetypes.KVStoreKey, deps Deps) Keeper {
	if key == nil {
		panic("policy: nil store key")
	}
	if deps.Versions == nil || deps.Decisions == nil {
		panic(fmt.Sprintf("policy: missing dependency (versions %t, decisions %t)", deps.Versions != nil, deps.Decisions != nil))
	}
	return Keeper{key: key, deps: deps}
}

// StoreKey returns the store key of this module.
func (k Keeper) StoreKey() *storetypes.KVStoreKey { return k.key }

// WithStore returns a copy of k whose methods read and write kv instead of
// the store of their context. The app uses it to read committed rows at
// load, before any block context exists; the read methods use the context
// for nothing else, so a zero sdk.Context is accepted there. The writer
// methods still read the block height and emit events through their
// context.
func (k Keeper) WithStore(kv storetypes.KVStore) Keeper {
	if kv == nil {
		panic("policy: WithStore needs a store")
	}
	k.store = kv
	return k
}

// GovernedLimit returns the value an executed governed limit change set for
// key (0x02). found is false when no value was set; the caller applies
// msgkit.Effective, which uses the protocol ceiling then.
//
// It fails closed: a stored value that is not 8 bytes, or is outside
// 1..ceiling of a known key, panics instead of reporting no limit, so a
// transaction never runs against the protocol ceiling when a lower governed
// value may be set. Only SetLimit writes 0x02, and only after its checks,
// so this is integrity damage. A transaction handler panic becomes a failed
// transaction on every node alike. ABCI Query recovers a panic into an
// error result, and the node gRPC server wraps every query in a recovery
// interceptor, so a query fails and the node keeps running.
func (k Keeper) GovernedLimit(ctx sdk.Context, key string) (uint64, bool) {
	return must(loadGoverned(k.kv(ctx), key))
}

// AppVersion returns the stored app version (0x04). It is 0 when the row is
// absent: InitVersions has not run, which is the case inside InitChain
// before the app writes it and on a home from goal 09 or earlier. A stored
// value that is not 8 bytes or is 0 panics (integrity damage).
func (k Keeper) AppVersion(ctx sdk.Context) uint64 {
	v, err := loadAppVersion(k.kv(ctx))
	if err != nil {
		panic(err.Error())
	}
	return v
}

// VersionMap returns the persisted module version map (0x05), read in
// module name order. It is empty when no row exists. A row whose module
// name is outside the id pattern, whose value is not an 8-byte version of at
// least 1, or more than MaxVersionMapModules rows, panics (integrity damage).
func (k Keeper) VersionMap(ctx sdk.Context) module.VersionMap {
	vm, err := readVersionMap(k.kv(ctx))
	if err != nil {
		panic(err.Error())
	}
	return vm
}

func readVersionMap(store storetypes.KVStore) (module.VersionMap, error) {
	vm := module.VersionMap{}
	err := iterate(store, VersionMapPrefix, MaxVersionMapModules, func(key, value []byte) error {
		name := string(key[1:])
		if !msgkit.ValidID(name) {
			return fmt.Errorf("policy: version map row %x has a module name outside the id pattern", key)
		}
		v, err := DecodeU64(value)
		if err != nil || v < 1 {
			return fmt.Errorf("policy: version map row %q is not an 8-byte version of at least 1 (%d bytes stored)", name, len(value))
		}
		vm[name] = v
		return nil
	})
	if err != nil {
		return nil, err
	}
	return vm, nil
}

// Plan returns the scheduled upgrade (0x06). found is false when no plan is
// stored. A stored value that does not decode panics (integrity damage).
func (k Keeper) Plan(ctx sdk.Context) (Plan, bool) {
	return must(loadPlan(k.kv(ctx)))
}

// UpgradeApplied reports whether an upgrade of this name has applied (a
// 0x07 row exists). A name outside the id pattern cannot have a row, so it
// is false without a read. A stored value that is not an 8-byte height of at
// least 1 panics (integrity damage).
func (k Keeper) UpgradeApplied(ctx sdk.Context, name string) bool {
	if !msgkit.ValidID(name) {
		return false
	}
	_, found := must(loadApplied(k.kv(ctx), name))
	return found
}

// SafetyCaps returns the stored protocol safety caps (0x01). A missing or
// undecodable row, or caps that differ from the protocol caps, is an error.
func (k Keeper) SafetyCaps(ctx sdk.Context) (*policyv1.SafetyCaps, error) {
	return loadSafetyCaps(k.kv(ctx))
}
