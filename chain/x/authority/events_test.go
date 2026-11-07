package authority

import (
	"strings"
	"testing"

	abci "github.com/cometbft/cometbft/abci/types"
	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	authorityv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
)

const (
	policyChangedType    = "svn.events.v1.PolicyChanged"
	proposalExecutedType = "svn.events.v1.ProposalExecuted"
)

// typedEvents returns the svn.events.v1 events of events, decoded, with the
// index of each in events.
func typedEvents(t *testing.T, events sdk.Events) ([]proto.Message, []int) {
	t.Helper()
	var out []proto.Message
	var at []int
	for i, ev := range events {
		if !strings.HasPrefix(ev.Type, "svn.events.v1.") {
			continue
		}
		msg, err := msgkit.ParseTyped(abci.Event(ev))
		if err != nil {
			t.Fatalf("event %d: %v", i, err)
		}
		out = append(out, msg)
		at = append(at, i)
	}
	return out, at
}

// TestExecuteEmitsPolicyEventsD2: a successful execute of every v0 payload
// kind emits PolicyChanged{new version, parameter key} (the spec event,
// spec_machine.py:1524-1527) and then ProposalExecuted{proposal id, new
// version} (D-2), after the events of a registry hook. A propose, an
// approve, an idempotent retry, a keep (REJECTED, EXPIRED), and a refusal
// (insufficient approval, a registry refusal) emit no typed event; the keep,
// refusal and retry outcomes carry authority_result.
func TestExecuteEmitsPolicyEventsD2(t *testing.T) {
	e, _ := registryKeeper(t)
	hooks := &storeHooks{key: e.registryKey, code: msgkit.OK}
	k := e.keeper.WithRegistry(hooks, hooks)
	noTyped := func(name string, ctx sdk.Context) {
		t.Helper()
		if typed, _ := typedEvents(t, ctx.EventManager().Events()); len(typed) != 0 {
			t.Fatalf("%s emitted typed events %v", name, typed)
		}
	}

	ctx := e.txCtx()
	if _, err := k.Propose(ctx, propose(e.seats[0].Signer, "ev-open", 1, governedMsg(msgkit.LimitMaxPageLimit, 9))); err != nil {
		t.Fatal(err)
	}
	noTyped("propose", ctx)
	ctx = e.txCtx()
	if _, err := k.Approve(ctx, approve(e.seats[0].Signer, "ev-ap", "ev-open")); err != nil {
		t.Fatal(err)
	}
	noTyped("approve", ctx)
	ctx = e.txCtx()
	if _, err := k.Execute(ctx, execute(e.authority, "ev-ex-open", "ev-open")); err == nil {
		t.Fatal("execute of an OPEN proposal succeeded")
	}
	noTyped("insufficient approval", ctx)

	rows := []struct {
		id       string
		msg      *authorityv1.MsgProposeChange
		registry bool
	}{
		{"ev-power", powerMsg("v5", pub(5), 1, uint64(e.height)+10), false},
		{"ev-limit", governedMsg(msgkit.LimitMaxOpenOffers, 8), false},
		{"ev-admit", admitProposal("prov-1"), true},
		{"ev-disable", disableProposal("prov-1"), true},
		{"ev-withdraw", withdrawProposal("content-1", 1, reasonValue), true},
	}
	for _, row := range rows {
		before := e.policy(t)
		e.queueWith(t, k, row.id, row.msg)
		param := recordOf(t, e.kv(), row.id).ParameterKey
		ctx := e.txCtx()
		if _, err := k.Execute(ctx, execute(e.authority, "ex-"+row.id, row.id)); err != nil {
			t.Fatalf("%s: %v", row.id, err)
		}
		events := ctx.EventManager().Events()
		typed, at := typedEvents(t, events)
		want := []proto.Message{
			&eventsv1.PolicyChanged{PolicyVersion: before + 1, ParameterKey: param},
			&eventsv1.ProposalExecuted{ProposalId: row.id, PolicyVersion: before + 1},
		}
		if len(typed) != len(want) {
			t.Fatalf("%s: typed events %v", row.id, typed)
		}
		for i := range want {
			if !proto.Equal(typed[i], want[i]) {
				t.Fatalf("%s: event %d is %v, want %v", row.id, i, typed[i], want[i])
			}
		}
		if e.policy(t) != before+1 {
			t.Fatalf("%s: policy version %d after %d", row.id, e.policy(t), before)
		}
		registryAt := -1
		for i, ev := range events {
			if ev.Type == "fake_registry" {
				registryAt = i
			}
			if ev.Type == "authority_result" {
				t.Fatalf("%s: authority_result on a success", row.id)
			}
		}
		if row.registry && (registryAt < 0 || registryAt > at[0]) {
			t.Fatalf("%s: registry event at %d, typed events at %v", row.id, registryAt, at)
		}

		// The idempotent retry of the same execute emits no typed event.
		retry := e.txCtx()
		if _, err := k.Execute(retry, execute(e.authority, "ex-"+row.id, row.id)); err != nil {
			t.Fatalf("%s retry: %v", row.id, err)
		}
		noTyped(row.id+" retry", retry)
		if _, idempotent := resultCode(t, retry); !idempotent {
			t.Fatalf("%s retry is not idempotent", row.id)
		}
	}

	// A registry refusal (NOT_FOUND) fails the execute: no typed event and
	// no registry event.
	hooks.code = msgkit.NotFound
	e.queueWith(t, k, "ev-missing", admitProposal("prov-2"))
	ctx = e.txCtx()
	if _, err := k.Execute(ctx, execute(e.authority, "ex-missing", "ev-missing")); err == nil {
		t.Fatal("registry refusal succeeded")
	}
	noTyped("registry refusal", ctx)
	if eventCount(ctx.EventManager().Events(), "fake_registry") != 0 {
		t.Fatal("a refused hook's event survived")
	}
	hooks.code = msgkit.OK

	// A REJECTED keep: a removal that leaves three validators (it applies
	// before the pending v5 addition).
	e.queueWith(t, k, "ev-reject", removalMsg("v1", pub(1), uint64(e.height)+5))
	ctx = e.txCtx()
	if _, err := k.Execute(ctx, execute(e.authority, "ex-reject", "ev-reject")); err != nil {
		t.Fatal(err)
	}
	noTyped("REJECTED keep", ctx)
	if code, _ := resultCode(t, ctx); code != CodeForbidden {
		t.Fatalf("REJECTED keep code %q", code)
	}

	// An EXPIRED keep: execute at the expiry.
	e.queueWith(t, k, "ev-expire", governedMsg(msgkit.LimitMaxEvidenceBatch, 3))
	e.unix = testExpiry
	ctx = e.txCtx()
	if _, err := k.Execute(ctx, execute(e.authority, "ex-expire", "ev-expire")); err != nil {
		t.Fatal(err)
	}
	noTyped("EXPIRED keep", ctx)
	if code, _ := resultCode(t, ctx); code != CodeExpired {
		t.Fatalf("EXPIRED keep code %q", code)
	}
}

// TestEmitExecutedNil: emitExecuted with no executed change emits nothing.
func TestEmitExecutedNil(t *testing.T) {
	e := readyKeeper(t)
	ctx := e.txCtx()
	if err := emitExecuted(ctx, nil); err != nil {
		t.Fatal(err)
	}
	if n := len(ctx.EventManager().Events()); n != 0 {
		t.Fatalf("%d events", n)
	}
}
