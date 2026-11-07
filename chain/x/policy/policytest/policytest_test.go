package policytest_test

import (
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy/policytest"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// fakeAuthority is the unit-test stand-in for the authority read surface.
type fakeAuthority struct{ version uint64 }

func (f *fakeAuthority) PolicyVersion(sdk.Context) uint64 { return f.version }

func (f *fakeAuthority) BlockedDecisions(sdk.Context) []*typesv1.BlockedDecisionParameter {
	return policy.DefaultGenesisState().GetPolicy().GetBlockedDecisions()
}

const height = 7 // msgkittest.BlockHeight

type fixture struct {
	ctx  sdk.Context
	k    policy.Keeper
	auth *fakeAuthority
}

func (f *fixture) store() storetypes.KVStore { return f.ctx.KVStore(f.k.StoreKey()) }

func (f *fixture) check() error { return policytest.CheckInvariants(f.ctx, f.k, f.auth) }

func versionMap() module.VersionMap {
	return module.VersionMap{"auth": 7, "authority": 1, policy.ModuleName: policy.ConsensusVersion, "leases": 2}
}

// healthy builds a store that holds every kind of row through the keeper:
// a governed value, a pending change, an applied upgrade (app version 2),
// and a plan.
func healthy(t testing.TB) *fixture {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, policy.ModuleName)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter())
	auth := &fakeAuthority{version: 3}
	f := &fixture{ctx: ctx, k: policy.NewKeeper(keys[policy.ModuleName], policy.Deps{Versions: auth, Decisions: auth}), auth: auth}
	if err := f.k.InitGenesis(ctx, policy.DefaultGenesisState()); err != nil {
		t.Fatal(err)
	}
	if err := f.k.InitVersions(ctx, versionMap(), 1); err != nil {
		t.Fatal(err)
	}
	if code := f.k.SetLimit(ctx, msgkit.LimitMaxPageLimit, 40, "p1"); code != msgkit.OK {
		t.Fatal(code)
	}
	if code := f.k.SchedulePending(ctx, msgkit.LimitMaxOpenOffers, 8, height+5, "p2", 2); code != msgkit.OK {
		t.Fatal(code)
	}
	if err := f.k.RecordUpgrade(ctx.WithBlockHeight(5), "up-0", 5, versionMap(), 2); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256([]byte("release"))
	if code := f.k.ScheduleUpgrade(ctx, "up-1", height+10, sum[:], "p3"); code != msgkit.OK {
		t.Fatal(code)
	}
	if err := f.check(); err != nil {
		t.Fatalf("healthy store: %v", err)
	}
	return f
}

// TestPolicyInvariantsSelfTest injects one violation per rule into a
// healthy store and asserts the checker names that rule.
func TestPolicyInvariantsSelfTest(t *testing.T) {
	ceiling, _ := msgkit.Ceiling(msgkit.LimitMaxPageLimit)
	pending := func(p policy.Pending) []byte { return policy.EncodePending(p) }
	cases := []struct {
		name, want string
		apply      func(*fixture)
	}{
		{"no layout key", "layout:", func(f *fixture) { f.store().Delete(msgkit.LayoutVersionKey) }},
		{"layout 3", "layout:", func(f *fixture) { f.store().Set(msgkit.LayoutVersionKey, policy.EncodeU64(3)) }},
		{"layout 3 bytes", "layout:", func(f *fixture) { f.store().Set(msgkit.LayoutVersionKey, []byte{0, 0, 2}) }},
		{"unknown prefix", "layout:", func(f *fixture) { f.store().Set([]byte{0x09, 'x'}, []byte{1}) }},
		{"long single key", "layout:", func(f *fixture) { f.store().Set([]byte{0x01, 'x'}, []byte{1}) }},
		{"bare governed prefix", "layout:", func(f *fixture) { f.store().Set([]byte{policy.GovernedPrefix}, []byte{1}) }},
		{"governed unknown key", "I2:", func(f *fixture) { f.store().Set(policy.GovernedKey("max_tx_bytes"), policy.EncodeU64(1)) }},
		{"governed value 0", "I2:", func(f *fixture) { f.store().Set(policy.GovernedKey(msgkit.LimitMaxPageLimit), policy.EncodeU64(0)) }},
		{"governed above ceiling", "I2:", func(f *fixture) {
			f.store().Set(policy.GovernedKey(msgkit.LimitMaxPageLimit), policy.EncodeU64(ceiling+1))
		}},
		{"governed 3 bytes", "I2:", func(f *fixture) { f.store().Set(policy.GovernedKey(msgkit.LimitMaxPageLimit), []byte{1, 2, 3}) }},
		{"pending unknown key", "I3:", func(f *fixture) {
			f.store().Set(policy.PendingKey("max_unknown"), pending(policy.Pending{Activation: 20, Value: 1, PolicyVersion: 2, ProposalID: "p"}))
		}},
		{"pending undecodable", "I3:", func(f *fixture) { f.store().Set(policy.PendingKey(msgkit.LimitMaxOpenOffers), []byte{1}) }},
		{"pending above ceiling", "I3:", func(f *fixture) {
			f.store().Set(policy.PendingKey(msgkit.LimitMaxPageLimit), pending(policy.Pending{Activation: 20, Value: ceiling + 1, PolicyVersion: 2, ProposalID: "p"}))
		}},
		{"pending activation at height", "I3:", func(f *fixture) {
			f.store().Set(policy.PendingKey(msgkit.LimitMaxOpenOffers), pending(policy.Pending{Activation: height, Value: 8, PolicyVersion: 2, ProposalID: "p2"}))
		}},
		{"pending version above current", "I3:", func(f *fixture) { f.auth.version = 1 }},
		{"plan undecodable", "I4:", func(f *fixture) { f.store().Set(policy.PlanKey(), []byte{1}) }},
		{"plan at height", "I4:", func(f *fixture) {
			sum := sha256.Sum256([]byte("r"))
			f.store().Set(policy.PlanKey(), policy.EncodePlan(policy.Plan{Name: "up-1", Height: height, Digest: sum[:], ProposalID: "p3"}))
		}},
		{"plan names an applied upgrade", "I4:", func(f *fixture) {
			sum := sha256.Sum256([]byte("r"))
			f.store().Set(policy.PlanKey(), policy.EncodePlan(policy.Plan{Name: "up-0", Height: height + 3, Digest: sum[:], ProposalID: "p3"}))
		}},
		{"app version not 1 + applied", "I5:", func(f *fixture) { f.store().Set(policy.AppVersionKey(), policy.EncodeU64(3)) }},
		{"app version 3 bytes", "I5:", func(f *fixture) { f.store().Set(policy.AppVersionKey(), []byte{0, 0, 2}) }},
		{"app version without version map", "I5:", func(f *fixture) {
			for name := range versionMap() {
				f.store().Delete(policy.VersionMapKey(name))
			}
		}},
		{"version map without app version", "I5:", func(f *fixture) { f.store().Delete(policy.AppVersionKey()) }},
		{"applied height above height", "I5:", func(f *fixture) { f.store().Set(policy.AppliedKey("up-0"), policy.EncodeU64(height+1)) }},
		{"applied height 0", "I5:", func(f *fixture) { f.store().Set(policy.AppliedKey("up-0"), policy.EncodeU64(0)) }},
		{"applied bad name", "I5:", func(f *fixture) {
			f.store().Delete(policy.AppliedKey("up-0"))
			f.store().Set(policy.AppliedKey("Up"), policy.EncodeU64(5))
		}},
		{"version map value 0", "I6:", func(f *fixture) { f.store().Set(policy.VersionMapKey("leases"), policy.EncodeU64(0)) }},
		{"version map bad name", "I6:", func(f *fixture) { f.store().Set(policy.VersionMapKey("Leases"), policy.EncodeU64(2)) }},
		{"version map policy entry not the layout", "I6:", func(f *fixture) {
			f.store().Set(policy.VersionMapKey(policy.ModuleName), policy.EncodeU64(3))
		}},
		{"too many version map rows", "I6:", func(f *fixture) {
			for i := 0; i < policy.MaxVersionMapModules; i++ {
				f.store().Set(policy.VersionMapKey(fmt.Sprintf("m%02d", i)), policy.EncodeU64(1))
			}
		}},
		{"no caps", "I7:", func(f *fixture) { f.store().Delete(policy.SafetyCapsKey()) }},
		{"caps changed", "I7:", func(f *fixture) {
			caps := policy.DefaultSafetyCaps()
			caps.SafetyMaxGasPerTx = 1
			f.store().Set(policy.SafetyCapsKey(), policy.EncodeSafetyCaps(caps))
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := healthy(t)
			c.apply(f)
			err := f.check()
			if err == nil || !strings.HasPrefix(err.Error(), c.want) {
				t.Fatalf("%v, want an error starting %q", err, c.want)
			}
		})
	}
}

// TestCheckInvariantsAtLayout: a store at layout 2 fails a check at layout
// 3, and a store whose layout row and policy version map entry say 3 passes
// at 3 and fails the default check.
func TestCheckInvariantsAtLayout(t *testing.T) {
	f := healthy(t)
	if err := policytest.CheckInvariantsAt(f.ctx, f.k, f.auth, 3); err == nil || !strings.HasPrefix(err.Error(), "layout:") {
		t.Fatalf("layout 2 store checked at 3: %v", err)
	}
	f.store().Set(msgkit.LayoutVersionKey, policy.EncodeU64(3))
	f.store().Set(policy.VersionMapKey(policy.ModuleName), policy.EncodeU64(3))
	if err := policytest.CheckInvariantsAt(f.ctx, f.k, f.auth, 3); err != nil {
		t.Fatalf("layout 3 store checked at 3: %v", err)
	}
	if err := policytest.CheckInvariants(f.ctx, f.k, f.auth); err == nil || !strings.HasPrefix(err.Error(), "layout:") {
		t.Fatalf("layout 3 store checked at the default: %v", err)
	}
	if err := policytest.CheckInvariants(f.ctx, f.k, nil); err == nil {
		t.Fatal("nil version source accepted")
	}
}

// TestFreshStoreHolds: a store with only genesis (no versions yet) and a
// store with genesis and versions both hold every invariant.
func TestFreshStoreHolds(t *testing.T) {
	ctx, keys := msgkittest.NewContext(t, policy.ModuleName)
	auth := &fakeAuthority{version: 1}
	k := policy.NewKeeper(keys[policy.ModuleName], policy.Deps{Versions: auth, Decisions: auth})
	if err := k.InitGenesis(ctx, policy.DefaultGenesisState()); err != nil {
		t.Fatal(err)
	}
	if err := policytest.CheckInvariants(ctx, k, auth); err != nil {
		t.Fatalf("genesis only: %v", err)
	}
	if err := k.InitVersions(ctx, versionMap(), 1); err != nil {
		t.Fatal(err)
	}
	if err := policytest.CheckInvariants(ctx, k, auth); err != nil {
		t.Fatalf("genesis and versions: %v", err)
	}
}

// TestStoreSnapshot: equal stores give equal snapshots; one changed value
// changes the snapshot.
func TestStoreSnapshot(t *testing.T) {
	a, b := healthy(t), healthy(t)
	if string(policytest.StoreSnapshot(a.ctx, a.k.StoreKey())) != string(policytest.StoreSnapshot(b.ctx, b.k.StoreKey())) {
		t.Fatal("equal stores differ")
	}
	b.store().Set(policy.GovernedKey(msgkit.LimitMaxPageLimit), policy.EncodeU64(41))
	if string(policytest.StoreSnapshot(a.ctx, a.k.StoreKey())) == string(policytest.StoreSnapshot(b.ctx, b.k.StoreKey())) {
		t.Fatal("a changed value kept the snapshot")
	}
}
