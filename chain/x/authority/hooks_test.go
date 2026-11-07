package authority

import (
	"bytes"
	"fmt"
	"maps"
	"sync"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// fakePolicy is the engine-level unit-test stand-in for x/policy behind
// PolicyWriter. It keeps the governed values, pending changes, the plan, the
// plan names and the applied names in memory, mirrors the x/policy rules the
// engine relies on (a later immediate change deletes the key's pending row;
// an applied name is DUPLICATE_ID; a name reused with another digest is
// CONFLICT; a cancel of another or no plan is NOT_FOUND), and records every
// call. fail, when set, is returned by every writer method before it
// changes anything. Keeper-level tests use the real x/policy keeper instead
// (policy_keeper_test.go).
type fakePolicy struct {
	limits  map[string]uint64
	pending map[string]fakePending
	plan    *fakePlan
	names   map[string][]byte
	applied map[string]bool
	calls   []string
	fail    string
}

type fakePending struct {
	value, activation, version uint64
	proposalID                 string
}

type fakePlan struct {
	name       string
	height     uint64
	digest     []byte
	proposalID string
}

func newFakePolicy() *fakePolicy {
	return &fakePolicy{
		limits: map[string]uint64{}, pending: map[string]fakePending{},
		names: map[string][]byte{}, applied: map[string]bool{},
	}
}

func (f *fakePolicy) SetLimit(key string, value uint64, proposalID string) Outcome {
	f.calls = append(f.calls, "SetLimit "+key)
	if f.fail != "" {
		return fail(f.fail)
	}
	f.limits[key] = value
	delete(f.pending, key)
	return ok()
}

func (f *fakePolicy) SchedulePending(key string, value, activation uint64, proposalID string, policyVersion uint64) Outcome {
	f.calls = append(f.calls, "SchedulePending "+key)
	if f.fail != "" {
		return fail(f.fail)
	}
	f.pending[key] = fakePending{value: value, activation: activation, version: policyVersion, proposalID: proposalID}
	return ok()
}

func (f *fakePolicy) ScheduleUpgrade(name string, height uint64, digest []byte, proposalID string) Outcome {
	f.calls = append(f.calls, "ScheduleUpgrade "+name)
	if f.fail != "" {
		return fail(f.fail)
	}
	if f.applied[name] {
		return fail(CodeDuplicate)
	}
	if first, used := f.names[name]; used && !bytes.Equal(first, digest) {
		return fail(CodeConflict)
	}
	if _, used := f.names[name]; !used {
		f.names[name] = bytes.Clone(digest)
	}
	f.plan = &fakePlan{name: name, height: height, digest: bytes.Clone(digest), proposalID: proposalID}
	return ok()
}

func (f *fakePolicy) CancelUpgrade(name string) Outcome {
	f.calls = append(f.calls, "CancelUpgrade "+name)
	if f.fail != "" {
		return fail(f.fail)
	}
	if f.plan == nil || f.plan.name != name {
		return fail(CodeNotFound)
	}
	f.plan = nil
	return ok()
}

func (f *fakePolicy) UpgradeApplied(name string) bool {
	f.calls = append(f.calls, "UpgradeApplied "+name)
	return f.applied[name]
}

// clone is a deep copy of f's state with no recorded calls.
func (f *fakePolicy) clone() *fakePolicy {
	out := newFakePolicy()
	out.fail = f.fail
	maps.Copy(out.limits, f.limits)
	maps.Copy(out.pending, f.pending)
	maps.Copy(out.applied, f.applied)
	for name, digest := range f.names {
		out.names[name] = bytes.Clone(digest)
	}
	if f.plan != nil {
		plan := *f.plan
		plan.digest = bytes.Clone(f.plan.digest)
		out.plan = &plan
	}
	return out
}

// sameState reports whether f and o hold the same values, pending changes,
// plan, plan names and applied names (recorded calls are not state).
func (f *fakePolicy) sameState(o *fakePolicy) bool {
	samePlan := (f.plan == nil) == (o.plan == nil)
	if samePlan && f.plan != nil {
		samePlan = f.plan.name == o.plan.name && f.plan.height == o.plan.height &&
			bytes.Equal(f.plan.digest, o.plan.digest) && f.plan.proposalID == o.plan.proposalID
	}
	return samePlan && maps.Equal(f.limits, o.limits) && maps.Equal(f.pending, o.pending) &&
		maps.Equal(f.applied, o.applied) && maps.EqualFunc(f.names, o.names, bytes.Equal)
}

// limit is the governed value of key and whether one is set.
func (f *fakePolicy) limit(key string) (uint64, bool) {
	value, found := f.limits[key]
	return value, found
}

// fakeAccounts is the unit-test stand-in for Accounts: it records each
// address it is asked to create, in order. fail, when set, is returned
// before anything is recorded.
type fakeAccounts struct {
	created [][]byte
	fail    string
}

func (f *fakeAccounts) EnsureAccount(addr []byte) Outcome {
	if f.fail != "" {
		return fail(f.fail)
	}
	f.created = append(f.created, bytes.Clone(addr))
	return ok()
}

// testHooks holds the fake policy and accounts of one test store, so every
// State that loadAt loads on that store shares them, as every message of a
// chain shares x/policy and the auth store.
type testHooks struct {
	policy   *fakePolicy
	accounts *fakeAccounts
}

var (
	hooksMu sync.Mutex
	hooksOf = map[KV]*testHooks{}
)

// hooksFor returns the fakes bound to kv, creating them on first use and
// dropping them when the test ends.
func hooksFor(t testing.TB, kv KV) *testHooks {
	t.Helper()
	hooksMu.Lock()
	defer hooksMu.Unlock()
	if h, found := hooksOf[kv]; found {
		return h
	}
	h := &testHooks{policy: newFakePolicy(), accounts: &fakeAccounts{}}
	hooksOf[kv] = h
	t.Cleanup(func() {
		hooksMu.Lock()
		defer hooksMu.Unlock()
		delete(hooksOf, kv)
	})
	return h
}

// policyFor is the fake policy of the test store kv.
func policyFor(t testing.TB, kv KV) *fakePolicy {
	t.Helper()
	return hooksFor(t, kv).policy
}

// accountsFor is the fake account hook of the test store kv.
func accountsFor(t testing.TB, kv KV) *fakeAccounts {
	t.Helper()
	return hooksFor(t, kv).accounts
}

// recordingPolicyKeeper is a PolicyKeeper that records the context height and
// arguments of each call and returns fixed codes; it checks that the bound
// adapter forwards every method to the keeper method of the same name with
// the bound context.
type recordingPolicyKeeper struct {
	calls   []string
	heights []int64
	code    msgkit.Code
	applied bool
	limit   uint64
	found   bool
}

func (r *recordingPolicyKeeper) record(ctx sdk.Context, call string) {
	r.calls = append(r.calls, call)
	r.heights = append(r.heights, ctx.BlockHeight())
}

func (r *recordingPolicyKeeper) SetLimit(ctx sdk.Context, key string, value uint64, proposalID string) msgkit.Code {
	r.record(ctx, fmt.Sprintf("SetLimit %s %d %s", key, value, proposalID))
	return r.code
}

func (r *recordingPolicyKeeper) SchedulePending(ctx sdk.Context, key string, value, activation uint64, proposalID string, policyVersion uint64) msgkit.Code {
	r.record(ctx, fmt.Sprintf("SchedulePending %s %d %d %s %d", key, value, activation, proposalID, policyVersion))
	return r.code
}

func (r *recordingPolicyKeeper) ScheduleUpgrade(ctx sdk.Context, name string, height uint64, digest []byte, proposalID string) msgkit.Code {
	r.record(ctx, fmt.Sprintf("ScheduleUpgrade %s %d %x %s", name, height, digest, proposalID))
	return r.code
}

func (r *recordingPolicyKeeper) CancelUpgrade(ctx sdk.Context, name string) msgkit.Code {
	r.record(ctx, "CancelUpgrade "+name)
	return r.code
}

func (r *recordingPolicyKeeper) UpgradeApplied(ctx sdk.Context, name string) bool {
	r.record(ctx, "UpgradeApplied "+name)
	return r.applied
}

func (r *recordingPolicyKeeper) GovernedLimit(ctx sdk.Context, key string) (uint64, bool) {
	r.record(ctx, "GovernedLimit "+key)
	return r.limit, r.found
}

// recordingAccounts is an AccountKeeper that records the context height and
// address of each call and returns a fixed code.
type recordingAccounts struct {
	calls   [][]byte
	heights []int64
	code    msgkit.Code
}

func (r *recordingAccounts) EnsureAccount(ctx sdk.Context, addr []byte) msgkit.Code {
	r.calls = append(r.calls, bytes.Clone(addr))
	r.heights = append(r.heights, ctx.BlockHeight())
	return r.code
}
