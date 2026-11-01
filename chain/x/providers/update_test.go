package providers

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

type updFault = fault[*providersv1.MsgUpdateProvider]
type updCall = call[*providersv1.MsgUpdateProvider]

// TestUpdateProviderCheckOrder is op_update_capacity
// (spec_machine.py:1815): Enforce, presence, lookup, owner, capacity,
// endpoint digest, business gate, capacity below reserved.
func TestUpdateProviderCheckOrder(t *testing.T) {
	ladder[*providersv1.MsgUpdateProvider]{
		setup: func(e *env) {
			e.seed(seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1000, 500))
			e.mustOK(e.update(e.fresh(), updMsg("p1", "upd-used", 2000)), "setup update")
		},
		base: func(*env) *providersv1.MsgUpdateProvider { return updMsg("p1", "upd-new", 3000) },
		faults: []updFault{
			gasFault[*providersv1.MsgUpdateProvider](),
			txBytesFault[*providersv1.MsgUpdateProvider](),
			signerLenFault(func(m *providersv1.MsgUpdateProvider, s []byte) { m.Signer = s }),
			{name: "presence-business-id", want: msgkit.Malformed, apply: func(_ *env, c *updCall) { c.msg.BusinessId = "" }},
			{name: "provider-not-found", want: msgkit.NotFound, apply: func(_ *env, c *updCall) { c.msg.ProviderId = "nope" }},
			{name: "cross-owner", want: msgkit.Forbidden, apply: func(_ *env, c *updCall) { c.msg.Signer = provider2Acct }},
			{name: "capacity-0", want: msgkit.Bounds, apply: func(_ *env, c *updCall) { c.msg.DeclaredCapacityBytes = 0 }},
			{name: "digest-algorithm-0", want: msgkit.Malformed, apply: func(_ *env, c *updCall) { c.msg.EndpointManifestDigest.Algorithm = 0 }},
			{name: "digest-33-bytes", want: msgkit.Bounds, apply: func(_ *env, c *updCall) {
				c.msg.EndpointManifestDigest.Value = append(c.msg.EndpointManifestDigest.Value, 1)
			}},
			{name: "business-id-pattern", want: msgkit.Bounds, apply: func(_ *env, c *updCall) { c.msg.BusinessId = "Upd" }},
			{name: "business-id-replay", want: msgkit.Replay, apply: func(_ *env, c *updCall) { c.msg.BusinessId = "upd-used" }},
			{name: "capacity-below-reserved", want: msgkit.Capacity, apply: func(_ *env, c *updCall) { c.msg.DeclaredCapacityBytes = 499 }},
		},
		exec: func(e *env, ctx sdk.Context, msg *providersv1.MsgUpdateProvider) result { return e.update(ctx, msg) },
	}.run(t)
}

func TestUpdateProviderPresence(t *testing.T) {
	cases := map[string]func(*providersv1.MsgUpdateProvider){
		"business_id":              func(m *providersv1.MsgUpdateProvider) { m.BusinessId = "" },
		"provider_id":              func(m *providersv1.MsgUpdateProvider) { m.ProviderId = "" },
		"endpoint_manifest_digest": func(m *providersv1.MsgUpdateProvider) { m.EndpointManifestDigest = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.registered("p1")
			msg := updMsg("p1", "upd-1", 10)
			msg.Signer = provider2Acct // owner check runs later
			edit(msg)
			before := e.snapshot()
			if r := e.update(e.fresh(), msg); r.code != msgkit.Malformed {
				t.Fatalf("code %s, want MALFORMED", r.code)
			}
			e.requireSameSnapshot(before, name)
		})
	}
}

// TestUpdateProviderSpecCases mirrors cases.py update-provider and
// update-provider-below-reserved, and the owner rules.
func TestUpdateProviderSpecCases(t *testing.T) {
	t.Run("update-provider", func(t *testing.T) {
		e := newEnv(t)
		e.admitted("p1")
		before := e.record("p1")
		msg := updMsg("p1", "upd-p1", 2_000_000)
		r := e.update(e.fresh(), msg)
		e.mustOK(r, "update")
		after := e.record("p1")
		if after.GetAdmittedCapacityBytes() != 2_000_000 {
			t.Fatalf("capacity %d", after.GetAdmittedCapacityBytes())
		}
		if !proto.Equal(after.GetEndpointManifestDigest(), msg.GetEndpointManifestDigest()) {
			t.Fatal("endpoint digest was not stored")
		}
		// Everything else is unchanged.
		before.AdmittedCapacityBytes = after.AdmittedCapacityBytes
		before.EndpointManifestDigest = after.EndpointManifestDigest
		if !proto.Equal(before, after) {
			t.Fatalf("update changed other fields: %v -> %v", before, after)
		}
		if len(r.typed()) != 0 {
			t.Fatal("UpdateProvider emitted a typed event; the spec emits none")
		}
	})
	t.Run("update-provider-below-reserved", func(t *testing.T) {
		e := newEnv(t)
		e.seed(seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1000, 500))
		before := e.snapshot()
		if r := e.update(e.fresh(), updMsg("p1", "upd-low", 1)); r.code != msgkit.Capacity {
			t.Fatalf("code %s, want CAPACITY", r.code)
		}
		e.requireSameSnapshot(before, "below reserved")
		e.mustOK(e.update(e.fresh(), updMsg("p1", "upd-equal", 500)), "capacity equal to reserved")
	})
	t.Run("unauthorized-update-by-service", func(t *testing.T) {
		e := newEnv(t)
		e.registered("p1")
		msg := updMsg("p1", "upd-svc", 10)
		msg.Signer = serviceAcct
		if r := e.update(e.fresh(), msg); r.code != msgkit.Forbidden {
			t.Fatalf("code %s, want FORBIDDEN", r.code)
		}
	})
	t.Run("unknown-provider-id-65", func(t *testing.T) {
		e := newEnv(t)
		msg := updMsg("a"+strings.Repeat("b", 64), "upd-long", 10)
		if r := e.update(e.fresh(), msg); r.code != msgkit.NotFound {
			t.Fatalf("code %s, want NOT_FOUND (the spec has no pattern check before the lookup)", r.code)
		}
	})
	t.Run("allowed-in-every-state", func(t *testing.T) {
		e := newEnv(t)
		e.seed(
			seededProvider("pa", providersv1.ProviderState_PROVIDER_STATE_APPLIED, 10, 0),
			seededProvider("pb", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 10, 0),
			seededProvider("pc", providersv1.ProviderState_PROVIDER_STATE_DRAINING, 10, 0),
			seededProvider("pd", providersv1.ProviderState_PROVIDER_STATE_DISABLED, 10, 0),
		)
		for _, id := range []string{"pa", "pb", "pc", "pd"} {
			e.mustOK(e.update(e.fresh(), updMsg(id, "upd-"+id, 20)), "update "+id)
			if e.record(id).GetAdmittedCapacityBytes() != 20 {
				t.Fatalf("%s not updated", id)
			}
		}
	})
}
