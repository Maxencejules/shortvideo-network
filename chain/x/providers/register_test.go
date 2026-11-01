package providers

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	eventsv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

type regFault = fault[*providersv1.MsgRegisterProvider]
type regCall = call[*providersv1.MsgRegisterProvider]

// TestRegisterProviderCheckOrder is op_register_provider
// (spec_machine.py:418): Enforce, presence, role, id patterns, pubkey,
// endpoint digest, capacity, business gate, duplicate id.
func TestRegisterProviderCheckOrder(t *testing.T) {
	ladder[*providersv1.MsgRegisterProvider]{
		setup: func(e *env) {
			// p-exists holds business id reg-used with its own payload.
			msg := regMsg("p-exists")
			msg.BusinessId = "reg-used"
			e.mustOK(e.register(e.fresh(), msg), "setup")
		},
		base: func(*env) *providersv1.MsgRegisterProvider { return regMsg("p-new") },
		faults: []regFault{
			gasFault[*providersv1.MsgRegisterProvider](),
			txBytesFault[*providersv1.MsgRegisterProvider](),
			signerLenFault(func(m *providersv1.MsgRegisterProvider, s []byte) { m.Signer = s }),
			{name: "presence-business-id", want: msgkit.Malformed, apply: func(_ *env, c *regCall) { c.msg.BusinessId = "" }},
			{name: "role-none", want: msgkit.Unauthorized, apply: func(_ *env, c *regCall) { c.msg.Signer = outsiderAcct }},
			{name: "role-service", want: msgkit.Forbidden, apply: func(_ *env, c *regCall) { c.msg.Signer = serviceAcct }},
			{name: "provider-id-65", want: msgkit.Bounds, apply: func(_ *env, c *regCall) { c.msg.ProviderId = "a" + strings.Repeat("b", 64) }},
			{name: "signing-key-id-pattern", want: msgkit.Bounds, apply: func(_ *env, c *regCall) { c.msg.SigningKeyId = "Key" }},
			{name: "operator-group-pattern", want: msgkit.Bounds, apply: func(_ *env, c *regCall) { c.msg.OperatorGroupId = "-group" }},
			{name: "signing-pubkey-31", want: msgkit.Bounds, apply: func(_ *env, c *regCall) { c.msg.SigningPubkey = c.msg.SigningPubkey[:31] }},
			{name: "digest-algorithm-0", want: msgkit.Malformed, apply: func(_ *env, c *regCall) { c.msg.EndpointManifestDigest.Algorithm = 0 }},
			{name: "digest-31-bytes", want: msgkit.Bounds, apply: func(_ *env, c *regCall) {
				c.msg.EndpointManifestDigest.Value = c.msg.EndpointManifestDigest.Value[:31]
			}},
			{name: "capacity-0", want: msgkit.Bounds, apply: func(_ *env, c *regCall) { c.msg.DeclaredCapacityBytes = 0 }},
			{name: "business-id-pattern", want: msgkit.Bounds, apply: func(_ *env, c *regCall) { c.msg.BusinessId = "Reg" }},
			{name: "business-id-replay", want: msgkit.Replay, apply: func(_ *env, c *regCall) { c.msg.BusinessId = "reg-used" }},
			{name: "duplicate-provider-id", want: msgkit.DuplicateID, apply: func(_ *env, c *regCall) { c.msg.ProviderId = "p-exists" }},
		},
		exec: func(e *env, ctx sdk.Context, msg *providersv1.MsgRegisterProvider) result {
			return e.register(ctx, msg)
		},
	}.run(t)
}

// TestRegisterProviderPresence: every string, bytes, and message field the
// spec requires is MALFORMED when empty (malformed-missing-field).
func TestRegisterProviderPresence(t *testing.T) {
	cases := map[string]func(*providersv1.MsgRegisterProvider){
		"business_id":              func(m *providersv1.MsgRegisterProvider) { m.BusinessId = "" },
		"provider_id":              func(m *providersv1.MsgRegisterProvider) { m.ProviderId = "" },
		"signing_pubkey":           func(m *providersv1.MsgRegisterProvider) { m.SigningPubkey = nil },
		"signing_key_id":           func(m *providersv1.MsgRegisterProvider) { m.SigningKeyId = "" },
		"endpoint_manifest_digest": func(m *providersv1.MsgRegisterProvider) { m.EndpointManifestDigest = nil },
		"operator_group_id":        func(m *providersv1.MsgRegisterProvider) { m.OperatorGroupId = "" },
		"signer":                   func(m *providersv1.MsgRegisterProvider) { m.Signer = nil },
	}
	for name, edit := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			msg := regMsg("p1")
			// The role check runs after presence, so an outsider signer
			// proves presence comes first.
			msg.Signer = outsiderAcct
			edit(msg)
			before := e.snapshot()
			if r := e.register(e.fresh(), msg); r.code != msgkit.Malformed {
				t.Fatalf("code %s, want MALFORMED", r.code)
			}
			e.requireSameSnapshot(before, name)
		})
	}
	t.Run("nil-message", func(t *testing.T) {
		e := newEnv(t)
		if r := e.register(e.fresh(), nil); r.code != msgkit.Malformed {
			t.Fatalf("code %s, want MALFORMED", r.code)
		}
	})
}

// TestRegisterProviderSpecCases mirrors cases.py boundary-id-64,
// boundary-id-65, malformed-missing-field, and the digest algorithm rule.
func TestRegisterProviderSpecCases(t *testing.T) {
	t.Run("boundary-id-64", func(t *testing.T) {
		e := newEnv(t)
		id := "a" + strings.Repeat("b", 63)
		msg := regMsg(id)
		msg.BusinessId = "reg-long"
		msg.SigningKeyId = "key-long"
		e.mustOK(e.register(e.fresh(), msg), "register 64-byte id")
		if e.k.AdmitByCouncil(e.fresh(), id, true) != msgkit.OK {
			t.Fatal("64-byte id was not admitted")
		}
		if got := e.record(id).GetState(); got != providersv1.ProviderState_PROVIDER_STATE_ADMITTED {
			t.Fatalf("state %s", got)
		}
	})
	t.Run("boundary-id-65", func(t *testing.T) {
		e := newEnv(t)
		msg := regMsg("p1")
		msg.ProviderId = "a" + strings.Repeat("b", 64)
		if r := e.register(e.fresh(), msg); r.code != msgkit.Bounds {
			t.Fatalf("code %s, want BOUNDS", r.code)
		}
	})
	t.Run("malformed-missing-field", func(t *testing.T) {
		e := newEnv(t)
		msg := regMsg("p1")
		msg.BusinessId = ""
		if r := e.register(e.fresh(), msg); r.code != msgkit.Malformed {
			t.Fatalf("code %s, want MALFORMED", r.code)
		}
	})
	t.Run("digest-algorithm-unknown", func(t *testing.T) {
		e := newEnv(t)
		msg := regMsg("p1")
		msg.EndpointManifestDigest.Algorithm = typesv1.DigestAlgorithm(2)
		if r := e.register(e.fresh(), msg); r.code != msgkit.Malformed {
			t.Fatalf("code %s, want MALFORMED", r.code)
		}
	})
	t.Run("digest-33-bytes", func(t *testing.T) {
		e := newEnv(t)
		msg := regMsg("p1")
		msg.EndpointManifestDigest.Value = append(msg.EndpointManifestDigest.Value, 0)
		if r := e.register(e.fresh(), msg); r.code != msgkit.Bounds {
			t.Fatalf("code %s, want BOUNDS", r.code)
		}
	})
	t.Run("broadcaster-cannot-register", func(t *testing.T) {
		e := newEnv(t)
		msg := regMsg("p1")
		msg.Signer = broadcasterAcct
		if r := e.register(e.fresh(), msg); r.code != msgkit.Forbidden {
			t.Fatalf("code %s, want FORBIDDEN", r.code)
		}
	})
}

// TestRegisterProviderWritesRecordAndEvent checks the stored record and the
// ProviderRegistered event of a successful registration.
func TestRegisterProviderWritesRecordAndEvent(t *testing.T) {
	e := newEnv(t)
	msg := regMsg("p1")
	msg.DeclaredCapacityBytes = 4096
	r := e.register(e.fresh(), msg)
	e.mustOK(r, "register")
	if r.idempotent {
		t.Fatal("first registration reported idempotent")
	}
	want := &providersv1.Provider{
		ProviderId:             "p1",
		Account:                providerAcct,
		SigningPubkey:          testPub("key-p1"),
		SigningKeyId:           "key-p1",
		KeyGeneration:          1,
		EndpointManifestDigest: digestOf("endpoint/p1"),
		State:                  providersv1.ProviderState_PROVIDER_STATE_APPLIED,
		AdmittedCapacityBytes:  4096,
		ReservedBytes:          0,
		OperatorGroupId:        DevnetOperatorGroup,
	}
	if got := e.record("p1"); !proto.Equal(got, want) {
		t.Fatalf("record %v, want %v", got, want)
	}
	typed := r.typed()
	if len(typed) != 1 {
		t.Fatalf("%d typed events, want 1", len(typed))
	}
	parsed, err := msgkit.ParseTyped(typed[0])
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(parsed, &eventsv1.ProviderRegistered{ProviderId: "p1", State: "APPLIED"}) {
		t.Fatalf("event %v", parsed)
	}
	digest, found := e.k.BusinessDigest(e.unmetered(), "reg-p1")
	if !found || len(digest) != msgkit.DigestBytes {
		t.Fatal("business id not committed")
	}
	// The business digest is SHA-256 of the SVN-JCS-1 object of the spec's
	// field set, keyed by proto field names.
	wantDigest, code := msgkit.BusinessDigest(map[string]any{
		"business_id":              "reg-p1",
		"declared_capacity_bytes":  "4096",
		"endpoint_manifest_digest": map[string]any{"algorithm": "DIGEST_ALGORITHM_SHA256", "value": hexOf(digestOf("endpoint/p1").GetValue())},
		"operator_group_id":        DevnetOperatorGroup,
		"provider_id":              "p1",
		"signing_key_id":           "key-p1",
		"signing_pubkey":           hexOf(testPub("key-p1")),
	})
	if code != msgkit.OK || hexOf(wantDigest) != hexOf(digest) {
		t.Fatalf("business digest %x, want %x", digest, wantDigest)
	}
}

// TestRegisterProviderNormalizesTheDigest: the stored digest is rebuilt
// from the validated value, so unknown fields of the message never reach
// state.
func TestRegisterProviderNormalizesTheDigest(t *testing.T) {
	e := newEnv(t)
	msg := regMsg("p1")
	msg.EndpointManifestDigest.ProtoReflect().SetUnknown([]byte{0xfa, 0x01, 0x01, 0x00})
	e.mustOK(e.register(e.fresh(), msg), "register")
	stored := e.record("p1").GetEndpointManifestDigest()
	if len(stored.ProtoReflect().GetUnknown()) != 0 {
		t.Fatal("unknown digest fields reached state")
	}
}
