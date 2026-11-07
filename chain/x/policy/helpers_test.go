package policy_test

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"

	storetypes "github.com/cosmos/cosmos-sdk/store/v2/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit/msgkittest"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy"
	"github.com/Maxencejules/shortvideo-network/chain/x/policy/policytest"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// fakeAuthority is the unit-test stand-in for the authority read surface:
// a fixed policy version and fixed decision rows.
type fakeAuthority struct {
	version   uint64
	decisions []*typesv1.BlockedDecisionParameter
}

func (f *fakeAuthority) PolicyVersion(sdk.Context) uint64 { return f.version }

func (f *fakeAuthority) BlockedDecisions(sdk.Context) []*typesv1.BlockedDecisionParameter {
	return f.decisions
}

func newFakeAuthority() *fakeAuthority {
	return &fakeAuthority{version: 1, decisions: policy.DefaultGenesisState().GetPolicy().GetBlockedDecisions()}
}

// testVersionMap is a module version map shaped like the app's: this module
// at its consensus version and seven others.
func testVersionMap() module.VersionMap {
	return module.VersionMap{
		"auth": 7, "authority": 1, "content": 2, "evidence": 2, "leases": 2,
		policy.ModuleName: policy.ConsensusVersion, "providers": 2, "svn": 1,
	}
}

type env struct {
	t    testing.TB
	ctx  sdk.Context
	key  *storetypes.KVStoreKey
	k    policy.Keeper
	auth *fakeAuthority
}

// newEnv mounts a real IAVL policy store, imports the default genesis, and
// writes the versions (app version 1) as InitChain does. The context has an
// infinite gas meter and block height msgkittest.BlockHeight (7).
func newEnv(t testing.TB) *env {
	t.Helper()
	e := newBareEnv(t)
	if err := e.k.InitGenesis(e.ctx, policy.DefaultGenesisState()); err != nil {
		t.Fatal(err)
	}
	if err := e.k.InitVersions(e.ctx, testVersionMap(), 1); err != nil {
		t.Fatal(err)
	}
	return e
}

// newBareEnv is newEnv without genesis: an empty policy store.
func newBareEnv(t testing.TB) *env {
	t.Helper()
	ctx, keys := msgkittest.NewContext(t, policy.ModuleName)
	auth := newFakeAuthority()
	key := keys[policy.ModuleName]
	return &env{
		t: t, key: key, auth: auth,
		ctx: ctx.WithGasMeter(storetypes.NewInfiniteGasMeter()),
		k:   policy.NewKeeper(key, policy.Deps{Versions: auth, Decisions: auth}),
	}
}

func (e *env) store() storetypes.KVStore { return e.ctx.KVStore(e.key) }

// at returns a fresh event manager context at block height h.
func (e *env) at(h int64) sdk.Context {
	return e.ctx.WithBlockHeight(h).WithEventManager(sdk.NewEventManager())
}

func (e *env) fresh() sdk.Context { return e.ctx.WithEventManager(sdk.NewEventManager()) }

func (e *env) snapshot() []byte { return policytest.StoreSnapshot(e.ctx, e.key) }

func (e *env) check() error { return policytest.CheckInvariants(e.ctx, e.k, e.auth) }

func (e *env) mustCheck() {
	e.t.Helper()
	if err := e.check(); err != nil {
		e.t.Fatalf("invariants: %v", err)
	}
}

// rejects runs op and asserts code want, no event, and an unchanged store.
func (e *env) rejects(name string, want msgkit.Code, op func(sdk.Context) msgkit.Code) {
	e.t.Helper()
	ctx := e.fresh()
	before := e.snapshot()
	if got := op(ctx); got != want {
		e.t.Fatalf("%s: code %q, want %q", name, got, want)
	}
	if !bytes.Equal(before, e.snapshot()) {
		e.t.Fatalf("%s: a rejected write changed the store", name)
	}
	if n := len(ctx.EventManager().Events()); n != 0 {
		e.t.Fatalf("%s: a rejected write emitted %d events", name, n)
	}
}

func digestOf(label string) []byte {
	sum := sha256.Sum256([]byte(label))
	return sum[:]
}

// attrs returns the attributes of the only event of type typ.
func attrs(t testing.TB, events sdk.Events, typ string) map[string]string {
	t.Helper()
	var found []abci.Event
	for _, ev := range events.ToABCIEvents() {
		if ev.Type == typ {
			found = append(found, ev)
		}
	}
	if len(found) != 1 {
		t.Fatalf("%d %s events in %v", len(found), typ, events)
	}
	out := map[string]string{}
	for _, a := range found[0].Attributes {
		out[a.Key] = a.Value
	}
	return out
}

// mustPanic runs fn and asserts it panics with a message containing want.
func mustPanic(t testing.TB, want string, fn func()) {
	t.Helper()
	defer func() {
		t.Helper()
		r := recover()
		if r == nil {
			t.Fatalf("no panic, want %q", want)
		}
		if msg := fmt.Sprint(r); !strings.Contains(msg, want) {
			t.Fatalf("panic %q, want it to contain %q", msg, want)
		}
	}()
	fn()
}

func equalPlan(a, b policy.Plan) bool {
	return a.Name == b.Name && a.Height == b.Height && bytes.Equal(a.Digest, b.Digest) && a.ProposalID == b.ProposalID
}

func cloneDecisions(rows []*typesv1.BlockedDecisionParameter) []*typesv1.BlockedDecisionParameter {
	out := make([]*typesv1.BlockedDecisionParameter, len(rows))
	for i, row := range rows {
		out[i] = proto.Clone(row).(*typesv1.BlockedDecisionParameter)
	}
	return out
}
