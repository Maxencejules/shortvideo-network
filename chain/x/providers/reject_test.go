package providers

import (
	"bytes"
	"math"
	"sort"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// TestRejectedProviderMessagesLeaveTheStore: for every reject class this
// module can return, a rejected message or council hook leaves every
// mounted store byte-identical (bytes.Equal over a full KV snapshot) and
// emits no event. The handlers run without a cache branch, so a write
// before the failing check would show.
func TestRejectedProviderMessagesLeaveTheStore(t *testing.T) {
	e := newEnv(t)
	e.seed(
		seededProvider("p-res", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 1000, 500),
	)
	e.admitted("p1")
	e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1)), "rotate")
	wrongGroup := regMsg("p-group")
	wrongGroup.OperatorGroupId = "operator-a"
	e.mustOK(e.register(e.fresh(), wrongGroup), "register p-group")
	max := seededProvider("p-max", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 10, 0)
	max.KeyGeneration = math.MaxUint64
	e.k.save(e.unmetered(), max)
	e.store("authority").Set([]byte("seats"), []byte("synthetic"))
	e.store("content").Set([]byte("catalog"), []byte("synthetic"))

	type rejection struct {
		name string
		want msgkit.Code
		run  func() (msgkit.Code, int)
	}
	msgCase := func(name string, want msgkit.Code, fn func() result) rejection {
		return rejection{name: name, want: want, run: func() (msgkit.Code, int) {
			r := fn()
			return r.code, len(r.events)
		}}
	}
	councilCase := func(name string, want msgkit.Code, fn func(sdk.Context) msgkit.Code) rejection {
		return rejection{name: name, want: want, run: func() (msgkit.Code, int) {
			ctx := e.fresh()
			code := fn(ctx)
			return code, len(ctx.EventManager().ABCIEvents())
		}}
	}
	cases := []rejection{
		msgCase("register-outsider", msgkit.Unauthorized, func() result {
			m := regMsg("p9")
			m.Signer = outsiderAcct
			return e.register(e.fresh(), m)
		}),
		msgCase("register-by-service", msgkit.Forbidden, func() result {
			m := regMsg("p9")
			m.Signer = serviceAcct
			return e.register(e.fresh(), m)
		}),
		msgCase("update-cross-owner", msgkit.Forbidden, func() result {
			m := updMsg("p1", "upd-x", 5)
			m.Signer = provider2Acct
			return e.update(e.fresh(), m)
		}),
		msgCase("register-replay", msgkit.Replay, func() result {
			m := regMsg("p9")
			m.BusinessId = "reg-p1"
			return e.register(e.fresh(), m)
		}),
		msgCase("register-duplicate-id", msgkit.DuplicateID, func() result {
			m := regMsg("p1")
			m.BusinessId = "reg-p1-again"
			return e.register(e.fresh(), m)
		}),
		msgCase("rotate-stale", msgkit.StaleVersion, func() result {
			return e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1b", "key-p1c", 1))
		}),
		msgCase("register-missing-field", msgkit.Malformed, func() result {
			m := regMsg("p9")
			m.EndpointManifestDigest = nil
			return e.register(e.fresh(), m)
		}),
		msgCase("register-digest-algorithm", msgkit.Malformed, func() result {
			m := regMsg("p9")
			m.EndpointManifestDigest.Algorithm = 0
			return e.register(e.fresh(), m)
		}),
		msgCase("rotate-profile-version", msgkit.OutOfRange, func() result {
			m := rotMsg(t, "p1", "key-p1b", "key-p1c", 2)
			m.CurrentKeySignature.CanonicalProfileVersion = 2
			return e.rotate(e.fresh(), m)
		}),
		msgCase("register-oversized-id", msgkit.Bounds, func() result {
			m := regMsg("p9")
			m.ProviderId = "a" + strings.Repeat("b", 64)
			return e.register(e.fresh(), m)
		}),
		msgCase("register-digest-31", msgkit.Bounds, func() result {
			m := regMsg("p9")
			m.EndpointManifestDigest.Value = m.EndpointManifestDigest.Value[:31]
			return e.register(e.fresh(), m)
		}),
		msgCase("drain-wrong-domain", msgkit.WrongDomain, func() result {
			m := drainMsg(t, "p1", "key-p1b", 2)
			m.ProviderSignature.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ROTATE
			return e.drain(e.fresh(), m)
		}),
		msgCase("update-below-reserved", msgkit.Capacity, func() result {
			return e.update(e.fresh(), updMsg("p-res", "upd-low", 499))
		}),
		msgCase("drain-not-admitted", msgkit.Conflict, func() result {
			return e.drain(e.fresh(), drainMsg(t, "p-group", "key-p-group", 1))
		}),
		msgCase("update-not-found", msgkit.NotFound, func() result {
			return e.update(e.fresh(), updMsg("nope", "upd-nope", 5))
		}),
		msgCase("rotate-overflow", msgkit.Overflow, func() result {
			return e.rotate(e.fresh(), rotMsg(t, "p-max", "key-p-max", "key-p-max2", math.MaxUint64))
		}),
		msgCase("rotate-forged", msgkit.Unauthorized, func() result {
			m := rotMsg(t, "p1", "key-p1b", "key-p1c", 2)
			m.CurrentKeySignature.Signature[5] ^= 1
			return e.rotate(e.fresh(), m)
		}),
		councilCase("admit-decision-blocked", msgkit.DecisionBlocked, func(ctx sdk.Context) msgkit.Code {
			return e.k.AdmitByCouncil(ctx, "p-group", false)
		}),
		councilCase("admit-wrong-group", msgkit.Forbidden, func(ctx sdk.Context) msgkit.Code {
			return e.k.AdmitByCouncil(ctx, "p-group", true)
		}),
		councilCase("disable-applied", msgkit.Conflict, func(ctx sdk.Context) msgkit.Code {
			return e.k.DisableByCouncil(ctx, "p-group")
		}),
		councilCase("admit-not-found", msgkit.NotFound, func(ctx sdk.Context) msgkit.Code {
			return e.k.AdmitByCouncil(ctx, "nope", true)
		}),
	}
	covered := map[msgkit.Code]bool{}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := e.snapshot()
			code, events := c.run()
			if code != c.want {
				t.Fatalf("code %s, want %s", code, c.want)
			}
			if events != 0 {
				t.Fatalf("rejected message emitted %d events", events)
			}
			after := e.snapshot()
			for name := range before {
				if !bytes.Equal(before[name], after[name]) {
					t.Fatalf("store %q changed", name)
				}
			}
			covered[code] = true
		})
	}
	want := []msgkit.Code{
		msgkit.Unauthorized, msgkit.Forbidden, msgkit.Replay, msgkit.DuplicateID, msgkit.StaleVersion,
		msgkit.Malformed, msgkit.OutOfRange, msgkit.Bounds, msgkit.WrongDomain, msgkit.Capacity,
		msgkit.Conflict, msgkit.NotFound, msgkit.DecisionBlocked, msgkit.Overflow,
	}
	var missing []string
	for _, code := range want {
		if !covered[code] {
			missing = append(missing, string(code))
		}
	}
	sort.Strings(missing)
	if len(missing) != 0 {
		t.Fatalf("reject classes not covered: %v", missing)
	}
}

// TestIdempotentReplayWritesNothing: the same business id with the same
// payload is a success that writes nothing and emits only
// providers_result idempotent=true, including after the state moved on
// (the gate runs before the state checks). A committed rotation is the
// exception: the spec checks the signature against the current key before
// the gate, and the retry was signed by the key it retired, so the retry is
// UNAUTHORIZED (op_rotate, spec_machine.py:1770).
func TestIdempotentReplayWritesNothing(t *testing.T) {
	e := newEnv(t)
	register := regMsg("p1")
	e.mustOK(e.register(e.fresh(), register), "register")
	if code := e.k.AdmitByCouncil(e.fresh(), "p1", true); code != msgkit.OK {
		t.Fatal(code)
	}
	update := updMsg("p1", "upd-p1", 77)
	e.mustOK(e.update(e.fresh(), update), "update")
	rotate := rotMsg(t, "p1", "key-p1", "key-p1b", 1)
	e.mustOK(e.rotate(e.fresh(), rotate), "rotate")
	drain := drainMsg(t, "p1", "key-p1b", 2)
	e.mustOK(e.drain(e.fresh(), drain), "drain")

	replays := map[string]func() result{
		"register": func() result { return e.register(e.fresh(), register) },
		"update":   func() result { return e.update(e.fresh(), update) },
		"drain":    func() result { return e.drain(e.fresh(), drain) },
	}
	for name, replay := range replays {
		t.Run(name, func(t *testing.T) {
			before := e.snapshot()
			r := replay()
			if r.code != msgkit.OK || !r.idempotent {
				t.Fatalf("code %s idempotent %v", r.code, r.idempotent)
			}
			e.requireSameSnapshot(before, name)
			if len(r.events) != 1 || r.events[0].Type != "providers_result" ||
				len(r.events[0].Attributes) != 1 || r.events[0].Attributes[0].Key != "idempotent" || r.events[0].Attributes[0].Value != "true" {
				t.Fatalf("events %v", r.events)
			}
		})
	}
	t.Run("rotate-retry-after-commit-is-unauthorized", func(t *testing.T) {
		before := e.snapshot()
		if r := e.rotate(e.fresh(), rotate); r.code != msgkit.Unauthorized {
			t.Fatalf("code %s, want UNAUTHORIZED", r.code)
		}
		e.requireSameSnapshot(before, "rotate retry")
	})
	t.Run("same-id-other-payload-is-replay", func(t *testing.T) {
		changed := updMsg("p1", "upd-p1", 78)
		before := e.snapshot()
		if r := e.update(e.fresh(), changed); r.code != msgkit.Replay {
			t.Fatalf("code %s, want REPLAY", r.code)
		}
		e.requireSameSnapshot(before, "replay")
	})
}

// TestBusinessIdsShareOneNamespace: a business id another module committed
// gates this module too (the spec's single business map).
func TestBusinessIdsShareOneNamespace(t *testing.T) {
	first := newEnv(t)
	first.registered("p1")
	digest, found := first.k.BusinessDigest(first.unmetered(), "reg-p1")
	if !found {
		t.Fatal("business id not committed")
	}

	same := newEnv(t)
	same.peer.digests["reg-p1"] = digest
	before := same.snapshot()
	r := same.register(same.fresh(), regMsg("p1"))
	if r.code != msgkit.OK || !r.idempotent {
		t.Fatalf("same digest in a peer: code %s idempotent %v", r.code, r.idempotent)
	}
	same.requireSameSnapshot(before, "peer idempotent")

	other := newEnv(t)
	other.peer.digests["reg-p1"] = bytes.Repeat([]byte{1}, 32)
	if r := other.register(other.fresh(), regMsg("p1")); r.code != msgkit.Replay {
		t.Fatalf("other digest in a peer: code %s, want REPLAY", r.code)
	}
	if _, found := other.k.Provider(other.unmetered(), "p1"); found {
		t.Fatal("replayed registration wrote a record")
	}
}
