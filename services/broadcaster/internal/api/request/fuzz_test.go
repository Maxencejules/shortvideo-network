package request

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// reencode decodes raw generically and encodes it again: keys sorted,
// whitespace removed, strings re-escaped, numbers kept as written.
func reencode(t *testing.T, raw []byte) []byte {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err != nil {
		t.Fatalf("an accepted document does not decode generically: %v", err)
	}
	out, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

// wantObject is the object type each key-signed kind acts on.
var wantObject = map[policy.Kind]store.ObjectType{
	policy.KindRegisterContent: store.ObjectContent, policy.KindWithdrawContent: store.ObjectContent,
	policy.KindOfferLease: store.ObjectOffer, policy.KindRelayAcceptLease: store.ObjectOffer,
	policy.KindRelayRenewLease: store.ObjectLease, policy.KindRelayEndLease: store.ObjectLease,
}

// FuzzDecodeIntentRequest drives the whole decoding path of POST
// /v1/intents (envelope, kind, authorization context, request object,
// stored bytes, digest) with arbitrary bodies. It never panics; every
// refusal has its class; and every accepted request is canonical: its
// stored bytes decode back to the same message without signer or business
// id, its object is a valid id of the kind's type, and re-encoding the
// request and context JSON in another key order and spelling gives the
// same stored bytes and the same digest.
func FuzzDecodeIntentRequest(f *testing.F) {
	for kind, req := range validRequests() {
		env := map[string]any{
			"kind": string(kind), "idempotency_key": "k-" + strings.ToLower(string(kind)), "expires_in_seconds": 600,
			"auth_context": validAuth(), "request": req,
		}
		f.Add(mustJSON(f, env))
		req["note"] = "x"
		f.Add(mustJSON(f, env))
	}
	f.Add([]byte(`{"kind":"REGISTER_CONTENT","kind":"OFFER_LEASE"}`))
	f.Add([]byte(`{"kind":null}`))
	f.Add([]byte(`[]`))
	f.Add([]byte(``))
	f.Add([]byte(`{"kind":"AUTHORITY_END","idempotency_key":"a","expires_in_seconds":1,"auth_context":{},"request":{}}`))

	f.Fuzz(func(t *testing.T, body []byte) {
		if len(body) > int(genesis.MaxTxBytes) {
			return
		}
		env, err := DecodeEnvelope(body)
		if err != nil {
			if ClassOf(err) != Malformed {
				t.Fatalf("envelope refusal of class %d: %v", ClassOf(err), err)
			}
			return
		}
		spec, err := LookupKind(env.Kind)
		if err != nil {
			if ClassOf(err) != UnknownKind {
				t.Fatalf("kind refusal of class %d", ClassOf(err))
			}
			return
		}
		auth, authJSON, err := DecodeAuthContext(env.AuthContext, MaxAuthContextBytes)
		if err != nil {
			if ClassOf(err) != Malformed {
				t.Fatalf("context refusal of class %d", ClassOf(err))
			}
			return
		}
		if err := auth.Validate(); err != nil {
			t.Fatalf("an accepted context breaks the policy rules: %v", err)
		}
		auth2, authJSON2, err := DecodeAuthContext(reencode(t, env.AuthContext), MaxAuthContextBytes)
		if err != nil || !bytes.Equal(authJSON, authJSON2) {
			t.Fatalf("the context is not canonical under re-encoding: %v", err)
		}
		if again, err := CanonicalAuthJSON(auth2); err != nil || !bytes.Equal(again, authJSON) {
			t.Fatal("canonical context JSON is not stable")
		}
		dec, err := DecodeRequest(spec.Kind, env.Request)
		if err != nil {
			if c := ClassOf(err); c != Malformed && c != Invalid {
				t.Fatalf("request refusal of class %d", c)
			}
			return
		}
		stored, err := dec.Stored()
		if err != nil || len(stored) == 0 || uint64(len(stored)) > genesis.MaxTxBytes {
			t.Fatalf("an accepted request has no stored form: %v", err)
		}
		d1, err := Digest(spec.Kind, stored, auth)
		if err != nil {
			t.Fatal(err)
		}
		dec2, err := DecodeRequest(spec.Kind, reencode(t, env.Request))
		if err != nil {
			t.Fatalf("the re-encoded request is refused: %v", err)
		}
		stored2, err := dec2.Stored()
		if err != nil || !bytes.Equal(stored, stored2) {
			t.Fatal("the stored bytes depend on the JSON spelling")
		}
		if d2, err := Digest(spec.Kind, stored2, auth2); err != nil || d2 != d1 {
			t.Fatal("the digest depends on the JSON spelling")
		}
		if spec.Kind == policy.KindForwardProviderTx {
			if dec.Msg != nil || !bytes.Equal(dec.Raw, stored) || !msgkit.ValidID(dec.ProviderID) {
				t.Fatal("a forward keeps its raw bytes and names its provider")
			}
			return
		}
		if dec.Object.Type != wantObject[spec.Kind] || !msgkit.ValidID(dec.Object.ID) {
			t.Fatalf("object %+v for %s", dec.Object, spec.Kind)
		}
		m := dec.Msg.(proto.Message)
		back := m.ProtoReflect().New().Interface()
		if err := proto.Unmarshal(stored, back); err != nil || !proto.Equal(back, m) {
			t.Fatal("stored bytes do not decode to the message")
		}
		if s, ok := back.(interface{ GetSigner() []byte }); !ok || len(s.GetSigner()) != 0 {
			t.Fatal("the stored message names a signer")
		}
		if b, ok := back.(interface{ GetBusinessId() string }); !ok || b.GetBusinessId() != "" {
			t.Fatal("the stored message names a business id")
		}
	})
}
