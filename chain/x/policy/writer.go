package policy

import (
	"errors"
	"fmt"
	"sort"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// The council writer methods. x/authority calls them from an executed
// proposal's payload on a nested cache that it writes back only when the
// whole message succeeds, and turns the returned code into its outcome.
// Each method runs every check before its first write, so a failure writes
// nothing and emits nothing. A stored row that does not decode panics
// (integrity damage; the SDK turns it into a failed transaction).

// blockHeight is the context's block height, 0 for a context without one.
func blockHeight(ctx sdk.Context) uint64 {
	if h := ctx.BlockHeight(); h > 0 {
		return uint64(h)
	}
	return 0
}

// checkLimit is the key and range check of a governed value: the key is one
// of the governed limit keys (msgkit.LimitKeys) and the value is in
// 1..ceiling. A safety cap name or any other key is not a governed key.
func checkLimit(key string, value uint64) msgkit.Code {
	ceiling, known := msgkit.Ceiling(key)
	if !known || value < 1 || value > ceiling {
		return msgkit.Bounds
	}
	return msgkit.OK
}

// checkFutureHeight is the height check of a pending change or a plan
// written in a block at height now: OUT_OF_RANGE at or below now, BOUNDS
// more than MaxActivationLeadBlocks above now or above 2^53-1.
func checkFutureHeight(target, now uint64) msgkit.Code {
	if target <= now {
		return msgkit.OutOfRange
	}
	if target-now > MaxActivationLeadBlocks || target > uint64(genesis.SafeIntegerMax) {
		return msgkit.Bounds
	}
	return msgkit.OK
}

// SetLimit writes value as the governed value of key at once and deletes any
// pending change of key, so the latest executed decision for a key wins
// (decision request DR-10-32). proposalID names the executing proposal.
//
//   - key not a governed limit key, value outside 1..ceiling: BOUNDS
//   - proposalID outside the id pattern: BOUNDS
//
// When a pending change was deleted, it emits policy_pending with
// activation_height 0 and the superseded proposal id.
func (k Keeper) SetLimit(ctx sdk.Context, key string, value uint64, proposalID string) msgkit.Code {
	if code := checkLimit(key, value); code != msgkit.OK {
		return code
	}
	if !msgkit.ValidID(proposalID) {
		return msgkit.Bounds
	}
	store := k.kv(ctx)
	superseded, pending := must(loadPending(store, key))
	store.Set(GovernedKey(key), EncodeU64(value))
	if pending {
		store.Delete(PendingKey(key))
		emitPending(ctx, key, value, 0, proposalID, superseded.ProposalID)
	}
	return msgkit.OK
}

// SchedulePending writes a governed change of key to value that applies at
// height activation, replacing any pending change of key (DR-10-32).
// policyVersion is the policy version the executing proposal produces; the
// row stores it.
//
//   - key not a governed limit key, value outside 1..ceiling: BOUNDS
//   - proposalID outside the id pattern: BOUNDS
//   - activation at or below the block height: OUT_OF_RANGE
//   - activation more than MaxActivationLeadBlocks above the block height,
//     or above 2^53-1: BOUNDS
//   - policyVersion below 2 (the first execute produces version 2):
//     OUT_OF_RANGE
//   - a pending change of key already stores a policy version at or above
//     policyVersion: CONFLICT (each execute produces a higher version, so a
//     later decision never carries a lower one)
//
// It emits policy_pending with the superseded proposal id, empty when no
// pending change was replaced.
func (k Keeper) SchedulePending(ctx sdk.Context, key string, value, activation uint64, proposalID string, policyVersion uint64) msgkit.Code {
	if code := checkLimit(key, value); code != msgkit.OK {
		return code
	}
	if !msgkit.ValidID(proposalID) {
		return msgkit.Bounds
	}
	if code := checkFutureHeight(activation, blockHeight(ctx)); code != msgkit.OK {
		return code
	}
	if policyVersion < 2 {
		return msgkit.OutOfRange
	}
	store := k.kv(ctx)
	old, replaced := must(loadPending(store, key))
	if replaced && old.PolicyVersion >= policyVersion {
		return msgkit.Conflict
	}
	store.Set(PendingKey(key), EncodePending(Pending{
		Activation: activation, Value: value, PolicyVersion: policyVersion, ProposalID: proposalID,
	}))
	supersededID := ""
	if replaced {
		supersededID = old.ProposalID
	}
	emitPending(ctx, key, value, activation, proposalID, supersededID)
	return msgkit.OK
}

// ScheduleUpgrade writes the upgrade plan: name, height, the 32-byte release
// digest, and the executing proposal. A later plan replaces a stored plan
// of another name, or of the same name with an identical digest (DR-10-32).
//
//   - name or proposalID outside the id pattern: BOUNDS
//   - digest not 32 bytes: BOUNDS
//   - height at or below the block height: OUT_OF_RANGE
//   - height more than MaxActivationLeadBlocks above the block height, or
//     above 2^53-1: BOUNDS
//   - an upgrade of this name already applied: DUPLICATE_ID
//   - a stored plan of the same name with another digest: CONFLICT
//
// It emits upgrade_plan with action "scheduled", or "replaced" and the
// replaced plan's name.
func (k Keeper) ScheduleUpgrade(ctx sdk.Context, name string, height uint64, digest []byte, proposalID string) msgkit.Code {
	if !msgkit.ValidID(name) || !msgkit.ValidID(proposalID) || len(digest) != msgkit.DigestBytes {
		return msgkit.Bounds
	}
	if code := checkFutureHeight(height, blockHeight(ctx)); code != msgkit.OK {
		return code
	}
	store := k.kv(ctx)
	if _, applied := must(loadApplied(store, name)); applied {
		return msgkit.DuplicateID
	}
	old, replaced := must(loadPlan(store))
	if replaced && old.Name == name && string(old.Digest) != string(digest) {
		return msgkit.Conflict
	}
	plan := Plan{Name: name, Height: height, Digest: append([]byte(nil), digest...), ProposalID: proposalID}
	store.Set(PlanKey(), EncodePlan(plan))
	if replaced {
		emitPlan(ctx, plan, PlanActionReplaced, old.Name)
	} else {
		emitPlan(ctx, plan, PlanActionScheduled, "")
	}
	return msgkit.OK
}

// CancelUpgrade deletes the stored plan of this name.
//
//   - name outside the id pattern: BOUNDS
//   - no stored plan, or a plan of another name: NOT_FOUND
//
// It emits upgrade_plan with action "cancelled" and the deleted plan.
func (k Keeper) CancelUpgrade(ctx sdk.Context, name string) msgkit.Code {
	if !msgkit.ValidID(name) {
		return msgkit.Bounds
	}
	store := k.kv(ctx)
	plan, found := must(loadPlan(store))
	if !found || plan.Name != name {
		return msgkit.NotFound
	}
	store.Delete(PlanKey())
	emitPlan(ctx, plan, PlanActionCancelled, "")
	return msgkit.OK
}

// checkVersionMap checks a module version map before it is stored: 1 to
// MaxVersionMapModules entries, every module name in the id pattern, every
// version at least 1, and an entry for this module equal to the stored
// layout version (each layout key equals its module's map entry).
func checkVersionMap(store storetypes.KVStore, vm module.VersionMap) error {
	if len(vm) == 0 || len(vm) > MaxVersionMapModules {
		return fmt.Errorf("policy: version map has %d modules, want 1..%d", len(vm), MaxVersionMapModules)
	}
	for _, name := range sortedNames(vm) {
		if v := vm[name]; !msgkit.ValidID(name) || v < 1 {
			return fmt.Errorf("policy: version map entry %q = %d is not a module name with a version of at least 1", name, v)
		}
	}
	layout, found, err := msgkit.ReadLayoutVersion(store)
	if err != nil {
		return fmt.Errorf("policy: %w", err)
	}
	if !found {
		return errors.New("policy: layout version row missing")
	}
	if vm[ModuleName] != layout {
		return fmt.Errorf("policy: version map gives %s version %d, the store layout is %d", ModuleName, vm[ModuleName], layout)
	}
	return nil
}

// writeVersionMap replaces the stored version map with vm, in module name
// order. old holds the module names stored now.
func writeVersionMap(store storetypes.KVStore, old, vm module.VersionMap) {
	for _, name := range sortedNames(old) {
		if _, kept := vm[name]; !kept {
			store.Delete(VersionMapKey(name))
		}
	}
	for _, name := range sortedNames(vm) {
		store.Set(VersionMapKey(name), EncodeU64(vm[name]))
	}
}

func sortedNames(vm module.VersionMap) []string {
	names := make([]string, 0, len(vm))
	for name := range vm {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// InitVersions writes the module version map (0x05) and the app version
// (0x04) of a new chain. The app calls it in InitChain after every module's
// InitGenesis, with the binary's module version map.
//
// It refuses (an error, nothing written) an app version of 0, a version map
// that checkVersionMap refuses, and a store that already holds an app
// version, a version map row, a plan, or an applied upgrade row.
func (k Keeper) InitVersions(ctx sdk.Context, vm module.VersionMap, appVersion uint64) error {
	if appVersion < 1 {
		return errors.New("policy: app version 0")
	}
	store := k.kv(ctx)
	if err := checkVersionMap(store, vm); err != nil {
		return err
	}
	for _, prefix := range [][]byte{AppVersionKey(), {VersionMapPrefix}, PlanKey(), {AppliedPrefix}} {
		found, err := anyUnder(store, prefix)
		if err != nil {
			return err
		}
		if found {
			return fmt.Errorf("policy: versions already initialized (a row exists under 0x%02x)", prefix[0])
		}
	}
	writeVersionMap(store, nil, vm)
	store.Set(AppVersionKey(), EncodeU64(appVersion))
	return nil
}

// RecordUpgrade records an applied upgrade: the upgrade name with the height
// it applied at (0x07), the new module version map (0x05, replacing every
// row), the new app version (0x04), and the deletion of the plan (0x06).
// The upgrade runner calls it in the block at height, after the migrations.
//
// It refuses (an error, nothing written):
//
//   - name outside the id pattern
//   - height of 0 or not the context's block height
//   - an upgrade of this name that already applied
//   - a stored plan whose name or height differs (a compiled plan source
//     has no plan row; a stored plan must be this upgrade)
//   - no stored app version, or appVersion not the stored app version + 1
//   - a version map that checkVersionMap refuses
func (k Keeper) RecordUpgrade(ctx sdk.Context, name string, height uint64, vm module.VersionMap, appVersion uint64) error {
	if !msgkit.ValidID(name) {
		return fmt.Errorf("policy: upgrade name %q outside the id pattern", name)
	}
	if height < 1 || height != blockHeight(ctx) {
		return fmt.Errorf("policy: upgrade height %d, block height %d", height, blockHeight(ctx))
	}
	store := k.kv(ctx)
	_, applied, err := loadApplied(store, name)
	if err != nil {
		return err
	}
	if applied {
		return fmt.Errorf("policy: upgrade %q already applied", name)
	}
	plan, planned, err := loadPlan(store)
	if err != nil {
		return err
	}
	if planned && (plan.Name != name || plan.Height != height) {
		return fmt.Errorf("policy: stored plan is %q at height %d, not %q at height %d", plan.Name, plan.Height, name, height)
	}
	stored, err := loadAppVersion(store)
	if err != nil {
		return err
	}
	if stored < 1 || stored == ^uint64(0) || appVersion != stored+1 {
		return fmt.Errorf("policy: app version %d after stored app version %d", appVersion, stored)
	}
	if err := checkVersionMap(store, vm); err != nil {
		return err
	}
	old, err := readVersionMap(store)
	if err != nil {
		return err
	}
	writeVersionMap(store, old, vm)
	store.Set(AppVersionKey(), EncodeU64(appVersion))
	store.Set(AppliedKey(name), EncodeU64(height))
	if planned {
		store.Delete(PlanKey())
	}
	return nil
}
