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

// height is the block height of every fixture context: far enough above 1
// that a plan applied before it could be scheduled with the minimum lead.
const height = 1000

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
// a governed value, a pending change, an applied upgrade (scheduled at
// height 300 for height 400; app version 2), and a plan with its plan-name
// row. Its context is at block height 1000.
func healthy(t testing.TB) *fixture {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, policy.ModuleName)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithBlockHeight(height)
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
	if code := f.k.ScheduleUpgrade(ctx.WithBlockHeight(300), "up-0", 300+msgkit.MinActivationLeadBlocks, digest("release-0"), "p0"); code != msgkit.OK {
		t.Fatal(code)
	}
	applyAt := int64(300 + msgkit.MinActivationLeadBlocks)
	if err := f.k.RecordUpgrade(ctx.WithBlockHeight(applyAt), "up-0", uint64(applyAt), versionMap(), 2); err != nil {
		t.Fatal(err)
	}
	if code := f.k.ScheduleUpgrade(ctx, "up-1", height+msgkit.MinActivationLeadBlocks, digest("release"), "p3"); code != msgkit.OK {
		t.Fatal(code)
	}
	if err := f.check(); err != nil {
		t.Fatalf("healthy store: %v", err)
	}
	return f
}

func digest(label string) []byte {
	sum := sha256.Sum256([]byte(label))
	return sum[:]
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
			f.store().Set(policy.PlanKey(), policy.EncodePlan(policy.Plan{Name: "up-1", Height: height, Digest: digest("release"), ProposalID: "p3"}))
		}},
		{"plan names an applied upgrade", "I4:", func(f *fixture) {
			f.store().Set(policy.PlanKey(), policy.EncodePlan(policy.Plan{Name: "up-0", Height: height + 3, Digest: digest("release-0"), ProposalID: "p3"}))
		}},
		{"plan without a plan-name row", "I4:", func(f *fixture) { f.store().Delete(policy.PlanNameKey("up-1")) }},
		{"plan digest differs from its plan-name row", "I4:", func(f *fixture) {
			f.store().Set(policy.PlanNameKey("up-1"), digest("other"))
		}},
		{"plan-name row of 31 bytes", "I4:", func(f *fixture) { f.store().Set(policy.PlanNameKey("old"), make([]byte, 31)) }},
		{"plan-name row with a bad name", "I4:", func(f *fixture) { f.store().Set(policy.PlanNameKey("Old"), digest("x")) }},
		{"applied upgrade without a plan-name row", "I4:", func(f *fixture) { f.store().Delete(policy.PlanNameKey("up-0")) }},
		{"bare plan-name prefix", "layout:", func(f *fixture) { f.store().Set([]byte{policy.PlanNamePrefix}, digest("x")) }},
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

// decided builds a store from genesis through the keeper at height 1000 and
// returns the latest executed decision per key: an immediate change of
// max_page_limit, a scheduled change of max_open_offers that is not due,
// and a scheduled change of max_expiry_steps that ApplyDue applied at 950
// after an older scheduled change of the same key was replaced.
func decided(t testing.TB) (*fixture, map[string]policytest.Decision) {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, policy.ModuleName)
	ctx = ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()).WithBlockHeight(height)
	auth := &fakeAuthority{version: 5}
	f := &fixture{ctx: ctx, k: policy.NewKeeper(keys[policy.ModuleName], policy.Deps{Versions: auth, Decisions: auth}), auth: auth}
	if err := f.k.InitGenesis(ctx, policy.DefaultGenesisState()); err != nil {
		t.Fatal(err)
	}
	at900 := ctx.WithBlockHeight(900)
	if code := f.k.SchedulePending(at900, msgkit.LimitMaxExpirySteps, 9, 990, "p1", 2); code != msgkit.OK {
		t.Fatal(code)
	}
	if code := f.k.SchedulePending(at900, msgkit.LimitMaxExpirySteps, 3, 950, "p2", 3); code != msgkit.OK {
		t.Fatal(code)
	}
	if err := f.k.ApplyDue(ctx.WithBlockHeight(950)); err != nil {
		t.Fatal(err)
	}
	if code := f.k.SetLimit(ctx, msgkit.LimitMaxPageLimit, 40, "p3"); code != msgkit.OK {
		t.Fatal(code)
	}
	if code := f.k.SchedulePending(ctx, msgkit.LimitMaxOpenOffers, 8, height+5, "p4", 5); code != msgkit.OK {
		t.Fatal(code)
	}
	latest := map[string]policytest.Decision{
		msgkit.LimitMaxExpirySteps: {Value: 3, Activation: 950, PolicyVersion: 3, ProposalID: "p2"},
		msgkit.LimitMaxPageLimit:   {Value: 40, PolicyVersion: 4, ProposalID: "p3"},
		msgkit.LimitMaxOpenOffers:  {Value: 8, Activation: height + 5, PolicyVersion: 5, ProposalID: "p4"},
	}
	if err := policytest.CheckDecisions(f.ctx, f.k, latest); err != nil {
		t.Fatalf("decided store: %v", err)
	}
	if err := f.check(); err != nil {
		t.Fatalf("decided store invariants: %v", err)
	}
	return f, latest
}

// TestCheckDecisionsSelfTest injects one violation of the exact I3 form per
// rule and asserts CheckDecisions reports it.
func TestCheckDecisionsSelfTest(t *testing.T) {
	pendingRow := func(a, v, pv uint64, id string) []byte {
		return policy.EncodePending(policy.Pending{Activation: a, Value: v, PolicyVersion: pv, ProposalID: id})
	}
	cases := []struct {
		name  string
		apply func(*fixture, map[string]policytest.Decision)
	}{
		{"immediate decision keeps a pending row", func(f *fixture, _ map[string]policytest.Decision) {
			f.store().Set(policy.PendingKey(msgkit.LimitMaxPageLimit), pendingRow(height+9, 7, 3, "p0"))
		}},
		{"immediate decision, other governed value", func(f *fixture, _ map[string]policytest.Decision) {
			f.store().Set(policy.GovernedKey(msgkit.LimitMaxPageLimit), policy.EncodeU64(41))
		}},
		{"due decision not applied", func(f *fixture, _ map[string]policytest.Decision) {
			f.store().Delete(policy.GovernedKey(msgkit.LimitMaxExpirySteps))
		}},
		{"due decision still pending", func(f *fixture, _ map[string]policytest.Decision) {
			f.store().Set(policy.PendingKey(msgkit.LimitMaxExpirySteps), pendingRow(height+1, 3, 3, "p2"))
		}},
		{"scheduled decision without its pending row", func(f *fixture, _ map[string]policytest.Decision) {
			f.store().Delete(policy.PendingKey(msgkit.LimitMaxOpenOffers))
		}},
		{"pending row older than the latest decision", func(_ *fixture, latest map[string]policytest.Decision) {
			d := latest[msgkit.LimitMaxOpenOffers]
			d.PolicyVersion = 6
			latest[msgkit.LimitMaxOpenOffers] = d
		}},
		{"pending row of another proposal", func(_ *fixture, latest map[string]policytest.Decision) {
			d := latest[msgkit.LimitMaxOpenOffers]
			d.ProposalID = "p9"
			latest[msgkit.LimitMaxOpenOffers] = d
		}},
		{"pending row with another activation", func(f *fixture, _ map[string]policytest.Decision) {
			f.store().Set(policy.PendingKey(msgkit.LimitMaxOpenOffers), pendingRow(height+6, 8, 5, "p4"))
		}},
		{"pending row undecodable", func(f *fixture, _ map[string]policytest.Decision) {
			f.store().Set(policy.PendingKey(msgkit.LimitMaxOpenOffers), []byte{1})
		}},
		{"governed row without a decision", func(f *fixture, _ map[string]policytest.Decision) {
			f.store().Set(policy.GovernedKey(msgkit.LimitMaxEvidenceBatch), policy.EncodeU64(2))
		}},
		{"pending row without a decision", func(f *fixture, _ map[string]policytest.Decision) {
			f.store().Set(policy.PendingKey(msgkit.LimitMaxPackageBytes), pendingRow(height+2, 2, 5, "p5"))
		}},
		{"decision for an unknown key", func(_ *fixture, latest map[string]policytest.Decision) {
			latest["max_tx_bytes"] = policytest.Decision{Value: 1, PolicyVersion: 2, ProposalID: "p6"}
		}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f, latest := decided(t)
			c.apply(f, latest)
			err := policytest.CheckDecisions(f.ctx, f.k, latest)
			if err == nil || !strings.HasPrefix(err.Error(), "I3:") {
				t.Fatalf("%v, want an error starting %q", err, "I3:")
			}
		})
	}
	t.Run("nil store key", func(t *testing.T) {
		f, latest := decided(t)
		if err := policytest.CheckDecisions(f.ctx, policy.Keeper{}, latest); err == nil {
			t.Fatal("zero keeper accepted")
		}
	})
}
