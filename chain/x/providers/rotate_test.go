package providers

import (
	"bytes"
	"math"
	"testing"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

type rotFault = fault[*providersv1.MsgRotateProviderKey]
type rotCall = call[*providersv1.MsgRotateProviderKey]

// currentKeyName is the key a test signs with for provider id: key ids are
// key names in these tests. An unknown provider signs with a stray key.
func (e *env) currentKeyName(id string) string {
	if record, found := e.k.Provider(e.unmetered(), id); found {
		return record.GetSigningKeyId()
	}
	return "key-stray"
}

func (e *env) currentGeneration(id string) uint64 {
	if record, found := e.k.Provider(e.unmetered(), id); found {
		return record.GetKeyGeneration()
	}
	return 1
}

// TestRotateProviderKeyCheckOrder is op_rotate (spec_machine.py:1770):
// Enforce, presence, lookup, inner signature against the current key,
// owner, new key bounds, business gate, generation, conflict.
func TestRotateProviderKeyCheckOrder(t *testing.T) {
	ladder[*providersv1.MsgRotateProviderKey]{
		setup: func(e *env) {
			e.registered("p1")
			e.peer.digests["rot-used"] = bytes.Repeat([]byte{7}, 32)
		},
		base: func(*env) *providersv1.MsgRotateProviderKey {
			return &providersv1.MsgRotateProviderKey{
				Signer:             providerAcct,
				BusinessId:         "rot-new",
				ProviderId:         "p1",
				NewSigningPubkey:   testPub("key-p1b"),
				NewSigningKeyId:    "key-p1b",
				ExpectedGeneration: 1,
			}
		},
		faults: []rotFault{
			gasFault[*providersv1.MsgRotateProviderKey](),
			txBytesFault[*providersv1.MsgRotateProviderKey](),
			signerLenFault(func(m *providersv1.MsgRotateProviderKey, s []byte) { m.Signer = s }),
			{name: "presence-signature", want: msgkit.Malformed, apply: func(_ *env, c *rotCall) { c.unsigned = true }},
			{name: "provider-not-found", want: msgkit.NotFound, apply: func(_ *env, c *rotCall) { c.msg.ProviderId = "nope" }},
			sigFault[*providersv1.MsgRotateProviderKey]("sig-domain-unknown", msgkit.Malformed, func(s *typesv1.InnerSignature) { s.Domain = 99 }),
			sigFault[*providersv1.MsgRotateProviderKey]("sig-wrong-domain", msgkit.WrongDomain, func(s *typesv1.InnerSignature) {
				s.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_DRAIN
			}),
			sigFault[*providersv1.MsgRotateProviderKey]("sig-profile-2", msgkit.OutOfRange, func(s *typesv1.InnerSignature) { s.CanonicalProfileVersion = 2 }),
			sigFault[*providersv1.MsgRotateProviderKey]("sig-signature-63", msgkit.Bounds, func(s *typesv1.InnerSignature) { s.Signature = s.Signature[:63] }),
			sigFault[*providersv1.MsgRotateProviderKey]("sig-key-id-not-current", msgkit.Unauthorized, func(s *typesv1.InnerSignature) { s.KeyId = "key-other" }),
			sigFault[*providersv1.MsgRotateProviderKey]("sig-pubkey-not-current", msgkit.Unauthorized, func(s *typesv1.InnerSignature) { s.PublicKey = testPub("key-other") }),
			sigFault[*providersv1.MsgRotateProviderKey]("sig-forged", msgkit.Unauthorized, func(s *typesv1.InnerSignature) { s.Signature[0] ^= 0x01 }),
			{name: "cross-owner", want: msgkit.Forbidden, apply: func(_ *env, c *rotCall) { c.msg.Signer = provider2Acct }},
			{name: "new-key-id-pattern", want: msgkit.Bounds, apply: func(_ *env, c *rotCall) { c.msg.NewSigningKeyId = "Key" }},
			{name: "new-pubkey-31", want: msgkit.Bounds, apply: func(_ *env, c *rotCall) { c.msg.NewSigningPubkey = c.msg.NewSigningPubkey[:31] }},
			{name: "business-id-pattern", want: msgkit.Bounds, apply: func(_ *env, c *rotCall) { c.msg.BusinessId = "Rot" }},
			{name: "business-id-replay-across-modules", want: msgkit.Replay, apply: func(_ *env, c *rotCall) { c.msg.BusinessId = "rot-used" }},
			{name: "rotate-key-stale-generation", want: msgkit.StaleVersion, apply: func(_ *env, c *rotCall) { c.msg.ExpectedGeneration = 9 }},
			{name: "same-key-id", want: msgkit.Conflict, apply: func(_ *env, c *rotCall) { c.msg.NewSigningKeyId = "key-p1" }},
		},
		finalize: func(t testing.TB, e *env, c *rotCall) {
			if c.unsigned {
				c.msg.CurrentKeySignature = nil
				return
			}
			signRotate(t, c.msg, e.currentKeyName(c.msg.GetProviderId()))
			c.editSignature(c.msg.CurrentKeySignature)
		},
		exec: func(e *env, ctx sdk.Context, msg *providersv1.MsgRotateProviderKey) result { return e.rotate(ctx, msg) },
	}.run(t)
}

func TestRotateProviderKeyPresence(t *testing.T) {
	cases := map[string]func(*providersv1.MsgRotateProviderKey){
		"business_id":           func(m *providersv1.MsgRotateProviderKey) { m.BusinessId = "" },
		"provider_id":           func(m *providersv1.MsgRotateProviderKey) { m.ProviderId = "" },
		"new_signing_pubkey":    func(m *providersv1.MsgRotateProviderKey) { m.NewSigningPubkey = nil },
		"new_signing_key_id":    func(m *providersv1.MsgRotateProviderKey) { m.NewSigningKeyId = "" },
		"current_key_signature": func(m *providersv1.MsgRotateProviderKey) { m.CurrentKeySignature = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			e.registered("p1")
			msg := rotMsg(t, "p1", "key-p1", "key-p1b", 1)
			msg.ProviderId = "nope" // lookup runs later
			if name == "provider_id" {
				msg.ProviderId = "p1"
			}
			edit(msg)
			before := e.snapshot()
			if r := e.rotate(e.fresh(), msg); r.code != msgkit.Malformed {
				t.Fatalf("code %s, want MALFORMED", r.code)
			}
			e.requireSameSnapshot(before, name)
		})
	}
}

// TestRotateProviderKeySpecCases mirrors cases.py
// rotate-key-stale-generation and rotate-then-old-key-fails, and the
// remaining rotation rules.
func TestRotateProviderKeySpecCases(t *testing.T) {
	t.Run("rotate-key-stale-generation", func(t *testing.T) {
		e := newEnv(t)
		e.admitted("p1")
		before := e.snapshot()
		if r := e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 9)); r.code != msgkit.StaleVersion {
			t.Fatalf("code %s, want STALE_VERSION", r.code)
		}
		e.requireSameSnapshot(before, "stale")
	})
	t.Run("rotate-then-old-key-fails", func(t *testing.T) {
		e := newEnv(t)
		e.admitted("p1")
		e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1)), "rotate")
		record := e.record("p1")
		if !bytes.Equal(record.GetSigningPubkey(), testPub("key-p1b")) || record.GetSigningKeyId() != "key-p1b" {
			t.Fatal("pubkey was not rotated")
		}
		// The handoff verifier is what goal 08 accept uses.
		canonical := `{"offer":"o1"}`
		message := msgkit.SignBytes(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, canonical)
		old := innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, "key-p1", message)
		if code := e.k.VerifyProviderSignature(e.fresh(), "p1", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, canonical, old); code != msgkit.Unauthorized {
			t.Fatalf("old key: code %s, want UNAUTHORIZED", code)
		}
		current := innerSig(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, "key-p1b", message)
		if code := e.k.VerifyProviderSignature(e.fresh(), "p1", typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, canonical, current); code != msgkit.OK {
			t.Fatalf("new key: code %s", code)
		}
		// The old key cannot rotate again either.
		if r := e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1c", 2)); r.code != msgkit.Unauthorized {
			t.Fatalf("rotation by the old key: code %s, want UNAUTHORIZED", r.code)
		}
	})
	t.Run("same-pubkey-conflict", func(t *testing.T) {
		e := newEnv(t)
		e.registered("p1")
		msg := &providersv1.MsgRotateProviderKey{
			Signer: providerAcct, BusinessId: "rot-same", ProviderId: "p1",
			NewSigningPubkey: testPub("key-p1"), NewSigningKeyId: "key-renamed", ExpectedGeneration: 1,
		}
		signRotate(t, msg, "key-p1")
		if r := e.rotate(e.fresh(), msg); r.code != msgkit.Conflict {
			t.Fatalf("code %s, want CONFLICT", r.code)
		}
	})
	t.Run("overflow", func(t *testing.T) {
		e := newEnv(t)
		// Only a seeded record can reach the last generation: a valid
		// genesis needs key_generation-1 previous keys. Write it directly.
		record := seededProvider("p1", providersv1.ProviderState_PROVIDER_STATE_ADMITTED, 10, 0)
		record.KeyGeneration = math.MaxUint64
		e.k.save(e.unmetered(), record)
		before := e.snapshot()
		if r := e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", math.MaxUint64)); r.code != msgkit.Overflow {
			t.Fatalf("code %s, want OVERFLOW", r.code)
		}
		e.requireSameSnapshot(before, "overflow")
	})
	t.Run("broadcaster-relay-forbidden", func(t *testing.T) {
		e := newEnv(t)
		e.registered("p1")
		msg := rotMsg(t, "p1", "key-p1", "key-p1b", 1)
		msg.Signer = broadcasterAcct
		if r := e.rotate(e.fresh(), msg); r.code != msgkit.Forbidden {
			t.Fatalf("code %s, want FORBIDDEN", r.code)
		}
	})
	t.Run("drain-signature-reused-for-rotation", func(t *testing.T) {
		e := newEnv(t)
		e.registered("p1")
		msg := rotMsg(t, "p1", "key-p1", "key-p1b", 1)
		msg.CurrentKeySignature = drainMsg(t, "p1", "key-p1", 1).GetProviderSignature()
		if r := e.rotate(e.fresh(), msg); r.code != msgkit.WrongDomain {
			t.Fatalf("code %s, want WRONG_DOMAIN", r.code)
		}
	})
	t.Run("oversized-payload-has-no-canonical-form", func(t *testing.T) {
		e := newEnv(t)
		e.registered("p1")
		msg := rotMsg(t, "p1", "key-p1", "key-p1b", 1)
		// 40 KiB of key bytes hex-encode above the 65536-byte SVN-JCS-1 cap.
		msg.NewSigningPubkey = bytes.Repeat([]byte{1}, 40<<10)
		if r := e.rotate(e.fresh(), msg); r.code != msgkit.Unauthorized {
			t.Fatalf("code %s, want UNAUTHORIZED", r.code)
		}
		msg.CurrentKeySignature.Domain = typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END
		if r := e.rotate(e.fresh(), msg); r.code != msgkit.WrongDomain {
			t.Fatalf("structure first: code %s, want WRONG_DOMAIN", r.code)
		}
	})
}

// TestRotateProviderKeyRecordsHistory: the old key is appended with its
// generation and the block time; the event carries the new generation and
// the unchanged state; rotating back to a retired key is allowed (v0 spec).
func TestRotateProviderKeyRecordsHistory(t *testing.T) {
	e := newEnv(t)
	e.admitted("p1")
	r := e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1))
	e.mustOK(r, "rotate 1")
	typed := r.typed()
	if len(typed) != 1 {
		t.Fatalf("%d typed events", len(typed))
	}
	parsed, err := msgkit.ParseTyped(typed[0])
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(parsed, &eventsv1.ProviderStateChanged{ProviderId: "p1", State: "ADMITTED", KeyGeneration: 2}) {
		t.Fatalf("event %v", parsed)
	}
	later := e.ctx.WithBlockTime(e.ctx.BlockTime().Add(3600 * 1e9))
	e.mustOK(e.rotate(later.WithGasMeter(newGasMeter(200_000)).WithEventManager(sdk.NewEventManager()), rotMsg(t, "p1", "key-p1b", "key-p1", 2)), "rotate back")
	if inline := e.record("p1"); len(inline.GetPreviousSigningKeys()) != 0 {
		t.Fatalf("the handoff read carries %d retired keys", len(inline.GetPreviousSigningKeys()))
	}
	res, err := e.k.QueryProvider(e.unmetered(), &providersv1.QueryProviderRequest{ProviderId: "p1"})
	if err != nil {
		t.Fatal(err)
	}
	record := res.GetProvider()
	if record.GetKeyGeneration() != 3 || record.GetSigningKeyId() != "key-p1" {
		t.Fatalf("record %v", record)
	}
	want := []*providersv1.PreviousSigningKey{
		{PublicKey: testPub("key-p1"), Generation: 1, RetiredUnixSeconds: e.ctx.BlockTime().Unix()},
		{PublicKey: testPub("key-p1b"), Generation: 2, RetiredUnixSeconds: e.ctx.BlockTime().Unix() + 3600},
	}
	if len(record.GetPreviousSigningKeys()) != len(want) {
		t.Fatalf("%d previous keys", len(record.GetPreviousSigningKeys()))
	}
	for i := range want {
		if !proto.Equal(record.GetPreviousSigningKeys()[i], want[i]) {
			t.Fatalf("previous key %d: %v, want %v", i, record.GetPreviousSigningKeys()[i], want[i])
		}
	}
	// The exported state with a retired key equal to the current key is
	// valid genesis.
	state, err := e.k.ExportGenesis(e.unmetered())
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateGenesis(state); err != nil {
		t.Fatalf("export after rotate-back: %v", err)
	}
}

// TestRotationTouchesOnlyTheProvidersStore: a rotation changes exactly the
// provider record, and adds its business id and one retired-key row, and
// nothing in any other store.
func TestRotationTouchesOnlyTheProvidersStore(t *testing.T) {
	e := newEnv(t)
	e.admitted("p1")
	e.store("authority").Set([]byte("validators"), []byte("unchanged"))
	e.store("content").Set([]byte("catalog"), []byte("unchanged"))
	before := e.snapshot()
	beforeKeys := storeKeys(e, ModuleName)
	e.mustOK(e.rotate(e.fresh(), rotMsg(t, "p1", "key-p1", "key-p1b", 1)), "rotate")
	after := e.snapshot()
	for _, name := range otherStores {
		if !bytes.Equal(before[name], after[name]) {
			t.Fatalf("rotation changed store %q", name)
		}
	}
	added := map[string]bool{}
	for key := range storeKeys(e, ModuleName) {
		if !beforeKeys[key] {
			added[key] = true
		}
	}
	if len(added) != 2 || !added[string(append([]byte{BusinessKeyPrefix}, "rot-p1-key-p1b"...))] || !added[string(RetiredKeyKey("p1", 1))] {
		t.Fatalf("rotation added keys %v", added)
	}
}

func storeKeys(e *env, name string) map[string]bool {
	out := map[string]bool{}
	it := e.store(name).Iterator(nil, nil)
	defer it.Close()
	for ; it.Valid(); it.Next() {
		out[string(it.Key())] = true
	}
	return out
}
