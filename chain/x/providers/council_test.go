package providers

import (
	"testing"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

func (e *env) councilEvents(ctx sdk.Context) []proto.Message {
	e.t.Helper()
	var out []proto.Message
	for _, ev := range ctx.EventManager().ABCIEvents() {
		parsed, err := msgkit.ParseTyped(ev)
		if err != nil {
			e.t.Fatalf("event %s: %v", ev.Type, err)
		}
		out = append(out, parsed)
	}
	return out
}

// TestAdmitByCouncil is the admit_provider branch of apply_proposal_payload
// (spec_machine.py:1335): NOT_FOUND, then DECISION_BLOCKED, then FORBIDDEN
// for a non-devnet group or a state other than APPLIED.
func TestAdmitByCouncil(t *testing.T) {
	type tc struct {
		name   string
		seed   *providersv1.Provider
		id     string
		devnet bool
		want   msgkit.Code
	}
	wrongGroup := seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_APPLIED, 10, 0)
	wrongGroup.OperatorGroupId = "operator-a"
	cases := []tc{
		{name: "absent-and-not-devnet", id: "nope", devnet: false, want: msgkit.NotFound},
		{name: "invalid-id", id: "Not An Id", devnet: true, want: msgkit.NotFound},
		{name: "admit-payload-blocked-by-dec-002", seed: wrongGroup, id: "p1", devnet: false, want: msgkit.DecisionBlocked},
		{name: "admit-non-devnet-group", seed: wrongGroup, id: "p1", devnet: true, want: msgkit.Forbidden},
		{name: "admitted-again", seed: seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 10, 0), id: "p1", devnet: true, want: msgkit.Forbidden},
		{name: "draining", seed: seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_DRAINING, 10, 0), id: "p1", devnet: true, want: msgkit.Forbidden},
		{name: "disabled-cannot-return", seed: seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_DISABLED, 10, 0), id: "p1", devnet: true, want: msgkit.Forbidden},
		{name: "execute-admit-provider", seed: seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_APPLIED, 10, 0), id: "p1", devnet: true, want: msgkit.OK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			if c.seed != nil {
				e.seed(c.seed)
			}
			before := e.snapshot()
			ctx := e.fresh()
			if got := e.k.AdmitByCouncil(ctx, c.id, c.devnet); got != c.want {
				t.Fatalf("code %s, want %s", got, c.want)
			}
			events := e.councilEvents(ctx)
			if c.want != msgkit.OK {
				e.requireSameSnapshot(before, c.name)
				if len(events) != 0 {
					t.Fatal("rejected admission emitted an event")
				}
				return
			}
			if got := e.record(c.id).GetState(); got != providersv1.ProviderState_PROVIDER_STATE_ADMITTED {
				t.Fatalf("state %s", got)
			}
			if len(events) != 1 || !proto.Equal(events[0], &eventsv1.ProviderStateChanged{ProviderId: "p1", State: "ADMITTED", KeyGeneration: 1}) {
				t.Fatalf("events %v", events)
			}
		})
	}
}

// TestDisableByCouncil is the disable_provider branch: NOT_FOUND, then
// CONFLICT unless ADMITTED or DRAINING. Disablement keeps the record, its
// accounting, and its key history.
func TestDisableByCouncil(t *testing.T) {
	cases := []struct {
		name  string
		state providersv1.ProviderState
		id    string
		want  msgkit.Code
	}{
		{name: "absent", id: "nope", want: msgkit.NotFound},
		{name: "disable-applied-conflict", state: providersv1.ProviderState_PROVIDER_STATE_APPLIED, id: "p1", want: msgkit.Conflict},
		{name: "disable-disabled-conflict", state: providersv1.ProviderState_PROVIDER_STATE_DISABLED, id: "p1", want: msgkit.Conflict},
		{name: "execute-disable-provider", state: providersv1.ProviderState_PROVIDER_STATE_ADMITTED, id: "p1", want: msgkit.OK},
		{name: "disable-draining", state: providersv1.ProviderState_PROVIDER_STATE_DRAINING, id: "p1", want: msgkit.OK},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			e := newEnv(t)
			if c.state != 0 {
				e.seed(seededProvider("p1", c.state, 1000, 700))
			}
			before := e.snapshot()
			ctx := e.fresh()
			if got := e.k.DisableByCouncil(ctx, c.id); got != c.want {
				t.Fatalf("code %s, want %s", got, c.want)
			}
			events := e.councilEvents(ctx)
			if c.want != msgkit.OK {
				e.requireSameSnapshot(before, c.name)
				if len(events) != 0 {
					t.Fatal("rejected disablement emitted an event")
				}
				return
			}
			record := e.record("p1")
			if record.GetState() != providersv1.ProviderState_PROVIDER_STATE_DISABLED {
				t.Fatalf("state %s", record.GetState())
			}
			if record.GetReservedBytes() != 700 || record.GetAdmittedCapacityBytes() != 1000 {
				t.Fatalf("disablement changed accounting: %v", record)
			}
			if len(events) != 1 || !proto.Equal(events[0], &eventsv1.ProviderStateChanged{ProviderId: "p1", State: "DISABLED", KeyGeneration: 1}) {
				t.Fatalf("events %v", events)
			}
		})
	}
}

// TestProviderDisabledSpecCase mirrors cases.py provider-disabled at the
// handoff API: after disablement no new obligation, the record and its
// reserved bytes stay, and update and rotation still work (the spec has no
// state check for them).
func TestProviderDisabledSpecCase(t *testing.T) {
	t.Run("provider-disabled", testProviderDisabled)
}

func testProviderDisabled(t *testing.T) {
	e := newEnv(t)
	e.seed(seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1000, 400))
	if code := e.k.DisableByCouncil(e.fresh(), "p1"); code != msgkit.OK {
		t.Fatalf("disable: %s", code)
	}
	if _, code := e.k.CheckNewObligation(e.fresh(), "p1"); code != msgkit.Forbidden {
		t.Fatalf("new obligation on a disabled provider: %s, want FORBIDDEN", code)
	}
	if e.record("p1").GetReservedBytes() != 400 {
		t.Fatal("disablement released reserved bytes")
	}
	e.mustOK(e.update(e.fresh(), updMsg("p1", "upd-disabled", 900)), "update a disabled provider")
	e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1)), "rotate a disabled provider")
	if r := e.drain(e.fresh(), drainMsg(t, "p1", "key-p1b", 2)); r.code != msgkit.Conflict {
		t.Fatalf("drain a disabled provider: %s, want CONFLICT", r.code)
	}
	if code := e.k.AdmitByCouncil(e.fresh(), "p1", true); code != msgkit.Forbidden {
		t.Fatalf("re-admit a disabled provider: %s, want FORBIDDEN", code)
	}
}

// TestHandoffAPI covers the goal-08 read surface on a real store.
func TestHandoffAPI(t *testing.T) {
	e := newEnv(t)
	e.seed(
		seededProvider("pa", providersv1.ProviderState_PROVIDER_STATE_APPLIED, 10, 0),
		seededProvider("pb", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 10, 0),
		seededProvider("pc", providersv1.ProviderState_PROVIDER_STATE_DRAINING, 10, 0),
		seededProvider("pd", providersv1.ProviderState_PROVIDER_STATE_DISABLED, 10, 0),
	)
	t.Run("check-new-obligation", func(t *testing.T) {
		want := map[string]msgkit.Code{"pa": msgkit.Forbidden, "pb": msgkit.OK, "pc": msgkit.Forbidden, "pd": msgkit.Forbidden, "nope": msgkit.NotFound}
		for id, code := range want {
			record, got := e.k.CheckNewObligation(e.fresh(), id)
			if got != code {
				t.Fatalf("%s: code %s, want %s", id, got, code)
			}
			if (code == msgkit.OK) != (record != nil) {
				t.Fatalf("%s: record %v with code %s", id, record, got)
			}
		}
	})
	t.Run("provider-returns-a-copy", func(t *testing.T) {
		before := e.snapshot()
		record, found := e.k.Provider(e.fresh(), "pb")
		if !found {
			t.Fatal("pb not found")
		}
		record.State = providersv1.ProviderState_PROVIDER_STATE_DISABLED
		record.SigningPubkey[0] ^= 0xff
		e.requireSameSnapshot(before, "mutating a returned record")
		if _, found := e.k.Provider(e.fresh(), "nope"); found {
			t.Fatal("absent provider found")
		}
	})
	t.Run("verify-provider-signature", func(t *testing.T) {
		canonical := `{"lease":"l1"}`
		domain := typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END
		message := msgkit.SignBytes(domain, canonical)
		good := innerSig(domain, "key-pb", message)
		if code := e.k.VerifyProviderSignature(e.fresh(), "pb", domain, canonical, good); code != msgkit.OK {
			t.Fatalf("valid: %s", code)
		}
		if code := e.k.VerifyProviderSignature(e.fresh(), "nope", domain, canonical, good); code != msgkit.NotFound {
			t.Fatalf("absent: %s", code)
		}
		if code := e.k.VerifyProviderSignature(e.fresh(), "pb", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, canonical, good); code != msgkit.WrongDomain {
			t.Fatalf("wrong domain: %s", code)
		}
		other := innerSig(domain, "key-pa", message)
		if code := e.k.VerifyProviderSignature(e.fresh(), "pb", domain, canonical, other); code != msgkit.Unauthorized {
			t.Fatalf("another provider's key: %s", code)
		}
		if code := e.k.VerifyProviderSignature(e.fresh(), "pb", domain, `{"lease":"l2"}`, good); code != msgkit.Unauthorized {
			t.Fatalf("different payload: %s", code)
		}
		if code := e.k.VerifyProviderSignature(e.fresh(), "pb", domain, canonical, nil); code != msgkit.Malformed {
			t.Fatalf("nil signature: %s", code)
		}
	})
}
