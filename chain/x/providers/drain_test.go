package providers

import (
	"bytes"
	"testing"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

type drainFault = fault[*providersv1.MsgDrainProvider]
type drainCall = call[*providersv1.MsgDrainProvider]

// TestDrainProviderCheckOrder is op_drain (spec_machine.py:1748): Enforce,
// presence, lookup, inner signature against the current key, owner,
// business gate, state.
func TestDrainProviderCheckOrder(t *testing.T) {
	ladder[*providersv1.MsgDrainProvider]{
		setup: func(e *env) {
			e.admitted("p1")
			e.registered("p-applied")
			e.peer.digests["drain-used"] = bytes.Repeat([]byte{9}, 32)
		},
		base: func(*env) *providersv1.MsgDrainProvider {
			return &providersv1.MsgDrainProvider{Signer: providerAcct, BusinessId: "drain-new", ProviderId: "p1"}
		},
		faults: []drainFault{
			gasFault[*providersv1.MsgDrainProvider](),
			txBytesFault[*providersv1.MsgDrainProvider](),
			signerLenFault(func(m *providersv1.MsgDrainProvider, s []byte) { m.Signer = s }),
			{name: "presence-signature", want: msgkit.Malformed, apply: func(_ *env, c *drainCall) { c.unsigned = true }},
			{name: "provider-not-found", want: msgkit.NotFound, apply: func(_ *env, c *drainCall) { c.msg.ProviderId = "nope" }},
			sigFault[*providersv1.MsgDrainProvider]("sig-domain-0", msgkit.Malformed, func(s *typesv1.InnerSignature) { s.Domain = 0 }),
			sigFault[*providersv1.MsgDrainProvider]("sig-wrong-domain", msgkit.WrongDomain, func(s *typesv1.InnerSignature) {
				s.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE
			}),
			sigFault[*providersv1.MsgDrainProvider]("sig-profile-3", msgkit.OutOfRange, func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 3 }),
			sigFault[*providersv1.MsgDrainProvider]("sig-key-id-pattern", msgkit.Bounds, func(s *typesv1.InnerSignature) { s.KeyId = "Key" }),
			sigFault[*providersv1.MsgDrainProvider]("sig-pubkey-31", msgkit.Bounds, func(s *typesv1.InnerSignature) { s.PublicKey = s.PublicKey[:31] }),
			sigFault[*providersv1.MsgDrainProvider]("sig-key-id-not-current", msgkit.Unauthorized, func(s *typesv1.InnerSignature) { s.KeyId = "key-other" }),
			sigFault[*providersv1.MsgDrainProvider]("sig-forged", msgkit.Unauthorized, func(s *typesv1.InnerSignature) { s.Signature[63] ^= 0x80 }),
			{name: "cross-owner", want: msgkit.Forbidden, apply: func(_ *env, c *drainCall) { c.msg.Signer = provider2Acct }},
			{name: "business-id-pattern", want: msgkit.Bounds, apply: func(_ *env, c *drainCall) { c.msg.BusinessId = "Drain" }},
			{name: "business-id-replay-across-modules", want: msgkit.Replay, apply: func(_ *env, c *drainCall) { c.msg.BusinessId = "drain-used" }},
			{name: "state-not-admitted", want: msgkit.Conflict, apply: func(_ *env, c *drainCall) { c.msg.ProviderId = "p-applied" }},
		},
		finalize: func(t testing.TB, e *env, c *drainCall) {
			if c.unsigned {
				return
			}
			id := c.msg.GetProviderId()
			c.msg.ProviderSignature = drainMsg(t, id, e.currentKeyName(id), e.currentGeneration(id)).GetProviderSignature()
			c.editSignature(c.msg.ProviderSignature)
		},
		exec: func(e *env, ctx sdk.Context, msg *providersv1.MsgDrainProvider) result { return e.drain(ctx, msg) },
	}.run(t)
}

func TestDrainProviderPresence(t *testing.T) {
	cases := map[string]func(*providersv1.MsgDrainProvider){
		"business_id":        func(m *providersv1.MsgDrainProvider) { m.BusinessId = "" },
		"provider_id":        func(m *providersv1.MsgDrainProvider) { m.ProviderId = "" },
		"provider_signature": func(m *providersv1.MsgDrainProvider) { m.ProviderSignature = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.admitted("p1")
			msg := drainMsg(t, "p1", "key-p1", 1)
			msg.Signer = provider2Acct // owner check runs later
			edit(msg)
			before := e.snapshot()
			if r := e.drain(e.fresh(), msg); r.code != msgkit.Malformed {
				t.Fatalf("code %s, want MALFORMED", r.code)
			}
			e.requireSameSnapshot(before, name)
		})
	}
}

// TestDrainProviderSpecCases mirrors cases.py drain-stops-new-offers at the
// handoff API and the drain signature rules.
func TestDrainProviderSpecCases(t *testing.T) {
	t.Run("drain-stops-new-offers", func(t *testing.T) {
		e := newEnv(t)
		e.admitted("p1")
		if _, code := e.k.CheckNewObligation(e.fresh(), "p1"); code != msgkit.OK {
			t.Fatalf("admitted provider: %s", code)
		}
		r := e.drain(e.fresh(), drainMsg(t, "p1", "key-p1", 1))
		e.mustOK(r, "drain")
		if got := e.record("p1").GetState(); got != providersv1.ProviderState_PROVIDER_STATE_DRAINING {
			t.Fatalf("state %s", got)
		}
		if _, code := e.k.CheckNewObligation(e.fresh(), "p1"); code != msgkit.Forbidden {
			t.Fatalf("after drain: code %s, want FORBIDDEN", code)
		}
		parsed, err := msgkit.ParseTyped(r.typed()[0])
		if err != nil {
			t.Fatal(err)
		}
		if !proto.Equal(parsed, &eventsv1.ProviderStateChanged{ProviderId: "p1", State: "DRAINING", KeyGeneration: 1}) {
			t.Fatalf("event %v", parsed)
		}
		// Reserved bytes are not released by drain.
		if e.record("p1").GetReservedBytes() != 0 {
			t.Fatal("reserved bytes changed")
		}
	})
	t.Run("drain-keeps-reserved-bytes", func(t *testing.T) {
		e := newEnv(t)
		e.seed(seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1000, 600))
		e.mustOK(e.drain(e.fresh(), drainMsg(t, "p1", "key-p1", 1)), "drain")
		if got := e.record("p1"); got.GetReservedBytes() != 600 || got.GetAdmittedCapacityBytes() != 1000 {
			t.Fatalf("drain changed accounting: %v", got)
		}
	})
	t.Run("second-drain-conflict", func(t *testing.T) {
		e := newEnv(t)
		e.admitted("p1")
		e.mustOK(e.drain(e.fresh(), drainMsg(t, "p1", "key-p1", 1)), "drain")
		msg := drainMsg(t, "p1", "key-p1", 1)
		msg.BusinessId = "drain-again"
		if r := e.drain(e.fresh(), msg); r.code != msgkit.Conflict {
			t.Fatalf("code %s, want CONFLICT", r.code)
		}
	})
	t.Run("broadcaster-relay-forbidden", func(t *testing.T) {
		e := newEnv(t)
		e.admitted("p1")
		msg := drainMsg(t, "p1", "key-p1", 1)
		msg.Signer = broadcasterAcct
		if r := e.drain(e.fresh(), msg); r.code != msgkit.Forbidden {
			t.Fatalf("code %s, want FORBIDDEN", r.code)
		}
	})
	t.Run("signature-names-the-key-generation", func(t *testing.T) {
		e := newEnv(t)
		e.admitted("p1")
		e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1)), "rotate")
		// New key, but the payload names generation 1.
		if r := e.drain(e.fresh(), drainMsg(t, "p1", "key-p1b", 1)); r.code != msgkit.Unauthorized {
			t.Fatalf("stale generation payload: code %s, want UNAUTHORIZED", r.code)
		}
		// Old key over the current generation.
		if r := e.drain(e.fresh(), drainMsg(t, "p1", "key-p1", 2)); r.code != msgkit.Unauthorized {
			t.Fatalf("retired key: code %s, want UNAUTHORIZED", r.code)
		}
		e.mustOK(e.drain(e.fresh(), drainMsg(t, "p1", "key-p1b", 2)), "drain with the current key")
	})
	t.Run("rotation-signature-reused-for-drain", func(t *testing.T) {
		e := newEnv(t)
		e.admitted("p1")
		msg := drainMsg(t, "p1", "key-p1", 1)
		msg.ProviderSignature = rotMsg(t, "p1", "key-p1", "key-p1b", 1).GetCurrentKeySignature()
		if r := e.drain(e.fresh(), msg); r.code != msgkit.WrongDomain {
			t.Fatalf("code %s, want WRONG_DOMAIN", r.code)
		}
	})
}
