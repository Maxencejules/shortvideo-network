// Package policytest is test support for x/policy: an invariant checker over
// a policy store and a byte snapshot of several stores. It is imported only
// by _test.go files and never runs in chain execution; svnd must not link it.
//
// CheckInvariantsAt checks the keeper-level invariants of
// chain/x/policy/README.md over one context (committed state or a cached
// branch), with h the context's block height:
//
//	layout  the layout key holds the given version; every row sits under a known prefix
//	I2      every governed value row (0x02) names a governed limit key and holds an
//	        8-byte value in 1..ceiling
//	I3      every pending row (0x03) names a governed limit key and decodes; its value is
//	        in 1..ceiling, its activation height is above h, and its policy version is
//	        at most the current policy version
//	I4      the plan row (0x06), when present, decodes; its height is above h; its name
//	        has no applied row (0x07)
//	I5      (policy part) the app version row (0x04) and the version map rows (0x05)
//	        exist together; when present, the app version is 1 + the number of applied
//	        rows; every applied row holds a height of at least 1 and at most h
//	I6      (policy part) every version map row names a module in the id pattern with an
//	        8-byte version of at least 1, at most policy.MaxVersionMapModules rows, and
//	        the row of this module equals the layout version
//	I7      (policy part) the safety caps row decodes and equals the protocol caps
//
// I1 and I8-I11 are authority invariants; I5, I6 (the binary and consensus
// parts), and I13 need the app; I12 is shown by tests with snapshots.
package policytest

import (
	"encoding/binary"
	"fmt"
	"sort"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
)

// CheckInvariants is CheckInvariantsAt with the binary's layout version
// (policy.ConsensusVersion).
func CheckInvariants(ctx sdk.Context, k policy.Keeper, versions policy.VersionSource) error {
	return CheckInvariantsAt(ctx, k, versions, policy.ConsensusVersion)
}

// CheckInvariantsAt returns the first violated invariant, or nil. The error
// text starts with the invariant name (for example "I3:"). layout is the
// layout version the store must hold: policy.ConsensusVersion by default,
// the persisted version map entry after an upgrade. versions is the policy
// version source (x/authority in production, a fake in unit tests). The
// store is read without gas.
func CheckInvariantsAt(ctx sdk.Context, k policy.Keeper, versions policy.VersionSource, layout uint64) error {
	if k.StoreKey() == nil || versions == nil {
		return fmt.Errorf("policytest: nil store key or version source")
	}
	rows := readRows(ctx.MultiStore().GetKVStore(k.StoreKey()))
	if err := checkLayout(rows, layout); err != nil {
		return err
	}
	height := uint64(0)
	if h := ctx.BlockHeight(); h > 0 {
		height = uint64(h)
	}
	if err := checkGoverned(rows); err != nil {
		return err
	}
	if err := checkPending(rows, height, versions.PolicyVersion(ctx)); err != nil {
		return err
	}
	applied, err := checkApplied(rows, height)
	if err != nil {
		return err
	}
	if err := checkPlan(rows, height, applied); err != nil {
		return err
	}
	if err := checkVersions(rows, layout, len(applied)); err != nil {
		return err
	}
	return checkCaps(rows)
}

// StoreSnapshot is msgkit.StoreSnapshot of each store, in argument order,
// each preceded by its store name and length (8-byte big-endian lengths).
// Two calls return equal bytes exactly when every store holds the same
// pairs. It reads through ctx's multistore without gas, so it works on a
// cached branch (ctx.CacheContext) to compare a trial state.
func StoreSnapshot(ctx sdk.Context, keys ...*storetypes.KVStoreKey) []byte {
	var out []byte
	for _, key := range keys {
		snapshot := msgkit.StoreSnapshot(ctx.MultiStore().GetKVStore(key))
		out = binary.BigEndian.AppendUint64(out, uint64(len(key.Name())))
		out = append(out, key.Name()...)
		out = binary.BigEndian.AppendUint64(out, uint64(len(snapshot)))
		out = append(out, snapshot...)
	}
	return out
}

func readRows(store storetypes.KVStore) map[string][]byte {
	it := store.Iterator(nil, nil)
	defer it.Close()
	rows := map[string][]byte{}
	for ; it.Valid(); it.Next() {
		rows[string(it.Key())] = append([]byte(nil), it.Value()...)
	}
	return rows
}

func sortedKeys(rows map[string][]byte) []string {
	keys := make([]string, 0, len(rows))
	for key := range rows {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// under returns the keys under prefix, in byte order, with the prefix cut.
func under(rows map[string][]byte, prefix byte) []string {
	var out []string
	for _, key := range sortedKeys(rows) {
		if len(key) > 1 && key[0] == prefix {
			out = append(out, key[1:])
		}
	}
	return out
}

var singleKeys = map[string]bool{
	string(msgkit.LayoutVersionKey): true,
	string(policy.SafetyCapsKey()):  true,
	string(policy.AppVersionKey()):  true,
	string(policy.PlanKey()):        true,
}

func checkLayout(rows map[string][]byte, layout uint64) error {
	raw, found := rows[string(msgkit.LayoutVersionKey)]
	if !found {
		return fmt.Errorf("layout: no layout version key")
	}
	if v, err := policy.DecodeU64(raw); err != nil || v != layout {
		return fmt.Errorf("layout: version row %x, want %d", raw, layout)
	}
	for _, key := range sortedKeys(rows) {
		if singleKeys[key] {
			continue
		}
		switch key[0] {
		case policy.GovernedPrefix, policy.PendingPrefix, policy.VersionMapPrefix, policy.AppliedPrefix:
			if len(key) == 1 {
				return fmt.Errorf("layout: bare prefix row %x", key)
			}
		default:
			return fmt.Errorf("layout: row %x under an unknown prefix", key)
		}
	}
	return nil
}

func checkGoverned(rows map[string][]byte) error {
	for _, key := range under(rows, policy.GovernedPrefix) {
		ceiling, known := msgkit.Ceiling(key)
		if !known {
			return fmt.Errorf("I2: governed value row for %q, which is not a governed limit key", key)
		}
		value, err := policy.DecodeU64(rows[string(policy.GovernedKey(key))])
		if err != nil || value < 1 || value > ceiling {
			return fmt.Errorf("I2: governed value of %q is not an 8-byte value in 1..%d", key, ceiling)
		}
	}
	return nil
}

func checkPending(rows map[string][]byte, height, version uint64) error {
	for _, key := range under(rows, policy.PendingPrefix) {
		ceiling, known := msgkit.Ceiling(key)
		if !known {
			return fmt.Errorf("I3: pending row for %q, which is not a governed limit key", key)
		}
		p, err := policy.DecodePending(rows[string(policy.PendingKey(key))])
		if err != nil {
			return fmt.Errorf("I3: pending row of %q: %v", key, err)
		}
		if p.Value > ceiling {
			return fmt.Errorf("I3: pending value of %q is %d, above the ceiling %d", key, p.Value, ceiling)
		}
		if p.Activation <= height {
			return fmt.Errorf("I3: pending change of %q activates at %d, not above height %d", key, p.Activation, height)
		}
		if p.PolicyVersion > version {
			return fmt.Errorf("I3: pending change of %q stores policy version %d, above the current version %d", key, p.PolicyVersion, version)
		}
	}
	return nil
}

func checkApplied(rows map[string][]byte, height uint64) (map[string]bool, error) {
	applied := map[string]bool{}
	for _, name := range under(rows, policy.AppliedPrefix) {
		if !msgkit.ValidID(name) {
			return nil, fmt.Errorf("I5: applied row %x has a name outside the id pattern", name)
		}
		h, err := policy.DecodeU64(rows[string(policy.AppliedKey(name))])
		if err != nil || h < 1 || h > height {
			return nil, fmt.Errorf("I5: applied upgrade %q holds height %x, want 1..%d", name, rows[string(policy.AppliedKey(name))], height)
		}
		applied[name] = true
	}
	return applied, nil
}

func checkPlan(rows map[string][]byte, height uint64, applied map[string]bool) error {
	raw, found := rows[string(policy.PlanKey())]
	if !found {
		return nil
	}
	plan, err := policy.DecodePlan(raw)
	if err != nil {
		return fmt.Errorf("I4: plan row: %v", err)
	}
	if plan.Height <= height {
		return fmt.Errorf("I4: plan %q at height %d, not above height %d", plan.Name, plan.Height, height)
	}
	if applied[plan.Name] {
		return fmt.Errorf("I4: plan %q names an applied upgrade", plan.Name)
	}
	return nil
}

func checkVersions(rows map[string][]byte, layout uint64, appliedCount int) error {
	modules := under(rows, policy.VersionMapPrefix)
	if len(modules) > policy.MaxVersionMapModules {
		return fmt.Errorf("I6: %d version map rows, at most %d", len(modules), policy.MaxVersionMapModules)
	}
	for _, name := range modules {
		v, err := policy.DecodeU64(rows[string(policy.VersionMapKey(name))])
		if !msgkit.ValidID(name) || err != nil || v < 1 {
			return fmt.Errorf("I6: version map row %q is not a module name with an 8-byte version of at least 1", name)
		}
		if name == policy.ModuleName && v != layout {
			return fmt.Errorf("I6: version map gives %s version %d, the layout is %d", name, v, layout)
		}
	}
	raw, found := rows[string(policy.AppVersionKey())]
	if !found {
		if len(modules) != 0 || appliedCount != 0 {
			return fmt.Errorf("I5: no app version row, but %d version map rows and %d applied rows", len(modules), appliedCount)
		}
		return nil
	}
	if len(modules) == 0 {
		return fmt.Errorf("I5: an app version row without version map rows")
	}
	v, err := policy.DecodeU64(raw)
	if err != nil || v != uint64(appliedCount)+1 {
		return fmt.Errorf("I5: app version row %x, want 1 + %d applied upgrades", raw, appliedCount)
	}
	return nil
}

func checkCaps(rows map[string][]byte) error {
	raw, found := rows[string(policy.SafetyCapsKey())]
	if !found {
		return fmt.Errorf("I7: no safety caps row")
	}
	if _, err := policy.DecodeSafetyCaps(raw); err != nil {
		return fmt.Errorf("I7: %v", err)
	}
	return nil
}
