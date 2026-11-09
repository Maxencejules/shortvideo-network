package request

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"

	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// Fixture values. Keys and signatures are synthetic byte strings of the
// right sizes: decoding checks shapes only; signatures are verified by the
// policy package and the chain.
var (
	hex32  = strings.Repeat("ab", 32)
	hex32b = strings.Repeat("cd", 32)
	hex64  = strings.Repeat("ef", 64)
)

func sig(domain string) map[string]any {
	return map[string]any{
		"domain": domain, "key_id": "key-1", "public_key_hex": hex32, "signature_hex": hex64,
		"canonical_profile_version": 1,
	}
}

// validRequests returns one valid request object per kind.
func validRequests() map[policy.Kind]map[string]any {
	return map[policy.Kind]map[string]any{
		policy.KindRegisterContent: {
			"content_id": "video-1", "content_version": 1, "manifest_digest_hex": hex32, "package_bytes": 4096,
			"hosting_intent": sig("SIGNATURE_DOMAIN_HOSTING_INTENT"),
		},
		policy.KindWithdrawContent: {
			"content_id": "video-1", "content_version": 1, "reason_digest_hex": hex32b,
			"hosting_intent": sig("SIGNATURE_DOMAIN_HOSTING_INTENT"),
		},
		policy.KindOfferLease: {
			"offer_id": "offer-1", "content_id": "video-1", "content_version": 1, "provider_id": "provider-1",
			"manifest_digest_hex": hex32, "package_bytes": 4096, "terms_digest_hex": hex32b,
			"start_unix_seconds": 1800000000, "deadline_unix_seconds": 1800003600, "expiry_unix_seconds": 1800000600,
		},
		policy.KindRelayAcceptLease: {
			"offer_id": "offer-1", "lease_id": "lease-1", "provider_acceptance": sig("SIGNATURE_DOMAIN_PROVIDER_ACCEPT"),
		},
		policy.KindRelayRenewLease: {
			"lease_id": "lease-1", "term_version": 2, "deadline_unix_seconds": 1800007200, "manifest_digest_hex": hex32,
			"package_bytes": 4096, "provider_signature": sig("SIGNATURE_DOMAIN_PROVIDER_RENEW"),
			"provider_id": "provider-1", "content_id": "video-1",
		},
		policy.KindRelayEndLease: {
			"lease_id": "lease-1", "provider_signature": sig("SIGNATURE_DOMAIN_PROVIDER_END"),
			"provider_id": "provider-1", "content_id": "video-1",
		},
		policy.KindForwardProviderTx: {
			"tx_base64":   base64.StdEncoding.EncodeToString([]byte("synthetic transaction bytes")),
			"provider_id": "provider-1", "content_id": "video-1",
		},
	}
}

func mustJSON(t testing.TB, v any) []byte {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func validAuth() map[string]any {
	return map[string]any{
		"principal_kind": "DEV_FIXTURE", "principal_id": "creator-7",
		"permissions": []string{"SCHEDULE_HOSTING", "PUBLISH_CONTENT"}, "policy_ref": "policy-1",
	}
}

func TestDecodeEachKind(t *testing.T) {
	for kind, req := range validRequests() {
		t.Run(string(kind), func(t *testing.T) {
			dec, err := DecodeRequest(kind, mustJSON(t, req))
			if err != nil {
				t.Fatalf("decode: %v", err)
			}
			if dec.Kind != kind {
				t.Fatalf("kind %s", dec.Kind)
			}
			stored, err := dec.Stored()
			if err != nil {
				t.Fatalf("stored: %v", err)
			}
			if kind == policy.KindForwardProviderTx {
				if dec.Msg != nil || !bytes.Equal(stored, []byte("synthetic transaction bytes")) {
					t.Fatal("a forward carries the raw bytes and no message")
				}
				return
			}
			// The stored bytes decode to the same message, with signer and
			// business id empty.
			back := proto.Clone(dec.Msg.(proto.Message))
			proto.Reset(back)
			if err := proto.Unmarshal(stored, back); err != nil {
				t.Fatal(err)
			}
			if !proto.Equal(back, dec.Msg.(proto.Message)) {
				t.Fatal("stored bytes do not round-trip")
			}
			signer := dec.WithSigner(bytes.Repeat([]byte{7}, 20))
			if signer == nil {
				t.Fatal("no message with signer")
			}
			if again, _ := dec.Stored(); !bytes.Equal(again, stored) {
				t.Fatal("WithSigner changed the decoded message")
			}
			type withSigner interface{ GetSigner() []byte }
			type withBusiness interface{ GetBusinessId() string }
			if len(back.(withSigner).GetSigner()) != 0 || back.(withBusiness).GetBusinessId() != "" {
				t.Fatal("stored message names a signer or business id")
			}
			if !bytes.Equal(signer.(withSigner).GetSigner(), bytes.Repeat([]byte{7}, 20)) {
				t.Fatal("WithSigner did not set the signer")
			}
		})
	}
}

func TestDecodedFieldsAndObjects(t *testing.T) {
	reqs := validRequests()
	dec, err := DecodeRequest(policy.KindOfferLease, mustJSON(t, reqs[policy.KindOfferLease]))
	if err != nil {
		t.Fatal(err)
	}
	offer := dec.Msg.(*leasesv1.MsgOfferLease)
	if offer.GetTermVersion() != 1 || offer.GetOfferId() != "offer-1" || offer.GetTermsDigest().GetAlgorithm() != typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256 {
		t.Fatal("offer fields")
	}
	if dec.Object != (store.ObjectRef{Type: store.ObjectOffer, ID: "offer-1"}) {
		t.Fatalf("offer object %+v", dec.Object)
	}
	dec, err = DecodeRequest(policy.KindRegisterContent, mustJSON(t, reqs[policy.KindRegisterContent]))
	if err != nil {
		t.Fatal(err)
	}
	reg := dec.Msg.(*contentv1.MsgRegisterContent)
	if reg.GetHostingIntent().GetDomain() != typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT || reg.GetPackageBytes() != 4096 {
		t.Fatal("register fields")
	}
	if dec.Object != (store.ObjectRef{Type: store.ObjectContent, ID: "video-1", Version: 1}) {
		t.Fatalf("register object %+v", dec.Object)
	}
	dec, err = DecodeRequest(policy.KindRelayEndLease, mustJSON(t, reqs[policy.KindRelayEndLease]))
	if err != nil {
		t.Fatal(err)
	}
	if end := dec.Msg.(*leasesv1.MsgEndLease); end.GetAuthorityEnd() || dec.ProviderID != "provider-1" || dec.ContentID != "video-1" {
		t.Fatal("end fields")
	}
	if dec.Object != (store.ObjectRef{Type: store.ObjectLease, ID: "lease-1"}) {
		t.Fatalf("end object %+v", dec.Object)
	}
}

// TestRequestRefusals checks one broken field per rule: each is refused
// with its class, and the error names the field, never the value.
func TestRequestRefusals(t *testing.T) {
	type tc struct {
		name  string
		kind  policy.Kind
		edit  func(m map[string]any)
		class Class
	}
	cases := []tc{
		{"unknown field", policy.KindRegisterContent, func(m map[string]any) { m["note"] = "free text" }, Malformed},
		{"signer field", policy.KindRegisterContent, func(m map[string]any) { m["signer"] = "aa" }, Malformed},
		{"business id field", policy.KindOfferLease, func(m map[string]any) { m["business_id"] = "b-1" }, Malformed},
		{"evidence id", policy.KindRelayRenewLease, func(m map[string]any) { m["evidence_id"] = "e-1" }, Malformed},
		{"authority end", policy.KindRelayEndLease, func(m map[string]any) { m["authority_end"] = true }, Malformed},
		{"term version field", policy.KindOfferLease, func(m map[string]any) { m["term_version"] = 2 }, Malformed},
		{"string for number", policy.KindRegisterContent, func(m map[string]any) { m["content_version"] = "1" }, Malformed},
		{"fraction", policy.KindRegisterContent, func(m map[string]any) { m["package_bytes"] = 1.5 }, Malformed},
		{"negative unsigned", policy.KindRegisterContent, func(m map[string]any) { m["content_version"] = -1 }, Malformed},
		{"bad content id", policy.KindRegisterContent, func(m map[string]any) { m["content_id"] = "Video 1" }, Invalid},
		{"empty content id", policy.KindRegisterContent, func(m map[string]any) { delete(m, "content_id") }, Invalid},
		{"version zero", policy.KindRegisterContent, func(m map[string]any) { m["content_version"] = 0 }, Invalid},
		{"package too large", policy.KindRegisterContent, func(m map[string]any) { m["package_bytes"] = uint64(1) << 40 }, Invalid},
		{"short digest", policy.KindRegisterContent, func(m map[string]any) { m["manifest_digest_hex"] = "abcd" }, Invalid},
		{"upper-case hex", policy.KindRegisterContent, func(m map[string]any) { m["manifest_digest_hex"] = strings.ToUpper(hex32) }, Invalid},
		{"missing intent", policy.KindRegisterContent, func(m map[string]any) { delete(m, "hosting_intent") }, Invalid},
		{"wrong intent domain", policy.KindRegisterContent, func(m map[string]any) {
			m["hosting_intent"] = sig("SIGNATURE_DOMAIN_PROVIDER_ACCEPT")
		}, Invalid},
		{"short public key", policy.KindRelayEndLease, func(m map[string]any) {
			s := sig("SIGNATURE_DOMAIN_PROVIDER_END")
			s["public_key_hex"] = "ab"
			m["provider_signature"] = s
		}, Invalid},
		{"profile version", policy.KindRelayEndLease, func(m map[string]any) {
			s := sig("SIGNATURE_DOMAIN_PROVIDER_END")
			s["canonical_profile_version"] = 2
			m["provider_signature"] = s
		}, Invalid},
		{"bad key id", policy.KindRelayEndLease, func(m map[string]any) {
			s := sig("SIGNATURE_DOMAIN_PROVIDER_END")
			s["key_id"] = "Key One"
			m["provider_signature"] = s
		}, Invalid},
		{"deadline before start", policy.KindOfferLease, func(m map[string]any) {
			m["deadline_unix_seconds"] = 1799999999
		}, Invalid},
		{"unix seconds above bound", policy.KindOfferLease, func(m map[string]any) {
			m["expiry_unix_seconds"] = int64(253402300800)
		}, Invalid},
		{"renew term version 1", policy.KindRelayRenewLease, func(m map[string]any) { m["term_version"] = 1 }, Invalid},
		{"renew without provider", policy.KindRelayRenewLease, func(m map[string]any) { delete(m, "provider_id") }, Invalid},
		{"end without content", policy.KindRelayEndLease, func(m map[string]any) { delete(m, "content_id") }, Invalid},
		{"accept bad optional provider", policy.KindRelayAcceptLease, func(m map[string]any) { m["provider_id"] = "P" }, Invalid},
		{"forward not base64", policy.KindForwardProviderTx, func(m map[string]any) { m["tx_base64"] = "!!" }, Invalid},
		{"forward unpadded", policy.KindForwardProviderTx, func(m map[string]any) {
			m["tx_base64"] = strings.TrimRight(base64.StdEncoding.EncodeToString([]byte("ab")), "=")
		}, Invalid},
		{"forward empty", policy.KindForwardProviderTx, func(m map[string]any) { m["tx_base64"] = "" }, Invalid},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := validRequests()[c.kind]
			c.edit(req)
			raw := mustJSON(t, req)
			_, err := DecodeRequest(c.kind, raw)
			if ClassOf(err) != c.class {
				t.Fatalf("class %d, want %d (err %v)", ClassOf(err), c.class, err)
			}
			for _, v := range []string{"free text", "Video 1", "Key One"} {
				if strings.Contains(err.Error(), v) {
					t.Fatalf("error quotes a value: %v", err)
				}
			}
		})
	}
	for _, raw := range []string{`null`, `{"content_id":null}`, `{"content_id":"a","content_id":"b"}`, `{} {}`, `[]`} {
		if _, err := DecodeRequest(policy.KindRegisterContent, []byte(raw)); ClassOf(err) != Malformed {
			t.Fatalf("%s: class %d", raw, ClassOf(err))
		}
	}
	if _, err := DecodeRequest("AUTHORITY_END", []byte(`{}`)); ClassOf(err) != UnknownKind {
		t.Fatalf("unrouted kind: %v", err)
	}
}

func TestEnvelopeRules(t *testing.T) {
	good := map[string]any{
		"kind": "OFFER_LEASE", "idempotency_key": "key-1", "expires_in_seconds": 600,
		"auth_context": validAuth(), "request": validRequests()[policy.KindOfferLease],
	}
	if _, err := DecodeEnvelope(mustJSON(t, good)); err != nil {
		t.Fatal(err)
	}
	edits := map[string]func(m map[string]any){
		"unknown field":   func(m map[string]any) { m["signer"] = "x" },
		"lower kind":      func(m map[string]any) { m["kind"] = "offer_lease" },
		"bad key":         func(m map[string]any) { m["idempotency_key"] = "Key 1" },
		"zero expiry":     func(m map[string]any) { m["expires_in_seconds"] = 0 },
		"missing request": func(m map[string]any) { delete(m, "request") },
		"array request":   func(m map[string]any) { m["request"] = []int{1} },
		"string auth":     func(m map[string]any) { m["auth_context"] = "x" },
	}
	for name, edit := range edits {
		m := map[string]any{}
		for k, v := range good {
			m[k] = v
		}
		edit(m)
		if _, err := DecodeEnvelope(mustJSON(t, m)); ClassOf(err) != Malformed {
			t.Fatalf("%s: class %d (%v)", name, ClassOf(err), err)
		}
	}
	if _, err := LookupKind("REGISTER_PROVIDER"); ClassOf(err) != UnknownKind {
		t.Fatal("unrouted kind looked up")
	}
}

// TestAuthContextRefusesPersonalData checks plan 5.5 under owner answer Q4:
// only the four fields are accepted; an email, an IP address, a user
// agent, a token, a cookie, a header copy or free text is refused, under
// its own key or inside an allowed field.
func TestAuthContextRefusesPersonalData(t *testing.T) {
	if _, out, err := DecodeAuthContext(mustJSON(t, validAuth()), 2048); err != nil || !bytes.Contains(out, []byte(`"permissions":["PUBLISH_CONTENT","SCHEDULE_HOSTING"]`)) {
		t.Fatalf("valid context: %v %s", err, out)
	}
	extra := map[string]any{
		"email": "a@example.com", "ip_address": "192.0.2.1", "user_agent": "Mozilla/5.0", "token": "abc",
		"cookie": "s=1", "headers": map[string]string{"Authorization": "Bearer x"}, "note": "free text",
	}
	for key, val := range extra {
		a := validAuth()
		a[key] = val
		if _, _, err := DecodeAuthContext(mustJSON(t, a), 2048); ClassOf(err) != Malformed {
			t.Fatalf("extra key %s accepted: %v", key, err)
		}
	}
	values := map[string][]string{
		"principal_id": {"a@example.com", "192.0.2.1", "010.001.002.003", "Mozilla/5.0 (X11)", "Bearer abc", "some free text",
			"Authorization: x", "UPPER"},
		"policy_ref": {"198.51.100.7", "policy ref", "x@y"},
	}
	for field, vs := range values {
		for _, v := range vs {
			a := validAuth()
			a[field] = v
			_, _, err := DecodeAuthContext(mustJSON(t, a), 2048)
			if ClassOf(err) != Malformed {
				t.Fatalf("%s=%q accepted: %v", field, v, err)
			}
			if strings.Contains(err.Error(), v) {
				t.Fatalf("error quotes the value: %v", err)
			}
		}
	}
	for name, edit := range map[string]func(a map[string]any){
		"other kind":         func(a map[string]any) { a["principal_kind"] = "USER" },
		"unknown permission": func(a map[string]any) { a["permissions"] = []string{"ADMIN"} },
		"repeated":           func(a map[string]any) { a["permissions"] = []string{"PUBLISH_CONTENT", "PUBLISH_CONTENT"} },
		"no permissions":     func(a map[string]any) { a["permissions"] = []string{} },
		"missing policy":     func(a map[string]any) { delete(a, "policy_ref") },
	} {
		a := validAuth()
		edit(a)
		if _, _, err := DecodeAuthContext(mustJSON(t, a), 2048); ClassOf(err) != Malformed {
			t.Fatalf("%s accepted: %v", name, err)
		}
	}
	big := validAuth()
	big["policy_ref"] = strings.Repeat("p", 64)
	raw := mustJSON(t, big)
	if _, _, err := DecodeAuthContext(raw, len(raw)-1); ClassOf(err) != Malformed {
		t.Fatal("context above the cap accepted")
	}
	if _, _, err := DecodeAuthContext(raw, MaxAuthContextBytes+1); err == nil || ClassOf(err) != 0 {
		t.Fatal("cap above the hard bound accepted")
	}
}

// TestRequestDigestCanonical checks plan 5.4: key order, whitespace,
// string escapes and the permission order do not change the digest; the
// kind, any message field and the context do.
func TestRequestDigestCanonical(t *testing.T) {
	digestOf := func(kind policy.Kind, reqRaw, authRaw []byte) [32]byte {
		t.Helper()
		dec, err := DecodeRequest(kind, reqRaw)
		if err != nil {
			t.Fatal(err)
		}
		auth, _, err := DecodeAuthContext(authRaw, 2048)
		if err != nil {
			t.Fatal(err)
		}
		stored, err := dec.Stored()
		if err != nil {
			t.Fatal(err)
		}
		d, err := Digest(kind, stored, auth)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	kind := policy.KindOfferLease
	req := mustJSON(t, validRequests()[kind])
	base := digestOf(kind, req, mustJSON(t, validAuth()))

	reordered := `{ "expiry_unix_seconds" : 1800000600, "terms_digest_hex":"` + hex32b + `", "start_unix_seconds":1800000000,
		"deadline_unix_seconds":1800003600, "package_bytes":4096, "manifest_digest_hex":"` + hex32 + `",
		"provider_id":"provider-1", "content_version":1, "content_id":"video\u002d1", "offer_id":"offer-1" }`
	authReordered := `{"policy_ref":"policy-1","permissions":["PUBLISH_CONTENT","SCHEDULE_HOSTING"],"principal_id":"creator-7","principal_kind":"DEV_FIXTURE"}`
	if got := digestOf(kind, []byte(reordered), []byte(authReordered)); got != base {
		t.Fatal("reordered keys, whitespace or escapes changed the digest")
	}
	changed := validRequests()[kind]
	changed["package_bytes"] = 4097
	if digestOf(kind, mustJSON(t, changed), mustJSON(t, validAuth())) == base {
		t.Fatal("a message field did not change the digest")
	}
	a := validAuth()
	a["principal_id"] = "creator-8"
	if digestOf(kind, req, mustJSON(t, a)) == base {
		t.Fatal("the context did not change the digest")
	}
	dec, _ := DecodeRequest(kind, req)
	stored, _ := dec.Stored()
	auth, _, _ := DecodeAuthContext(mustJSON(t, validAuth()), 2048)
	other, err := Digest(policy.KindRegisterContent, stored, auth)
	if err != nil || other == base {
		t.Fatal("the kind did not change the digest")
	}
	if _, err := Digest(kind, nil, auth); err == nil {
		t.Fatal("digest of an empty request")
	}
	if _, err := Digest("NOT_A_KIND", stored, auth); err == nil {
		t.Fatal("digest of an unrouted kind")
	}
	// The framing is the documented one.
	authJSON, _ := CanonicalAuthJSON(auth)
	h := sha256.New()
	for _, frame := range [][]byte{[]byte(digestLabel), []byte(kind), stored, authJSON} {
		var l [8]byte
		for i := 0; i < 8; i++ {
			l[7-i] = byte(uint64(len(frame)) >> (8 * i))
		}
		h.Write(l[:])
		h.Write(frame)
	}
	if hex.EncodeToString(h.Sum(nil)) != hex.EncodeToString(base[:]) {
		t.Fatal("digest is not the length-prefixed framing")
	}
}

func TestForwardObject(t *testing.T) {
	for _, c := range []struct {
		msg  sdk.Msg
		want store.ObjectRef
	}{
		{&leasesv1.MsgAcceptLease{OfferId: "o-1", LeaseId: "l-1", BusinessId: "b-1"}, store.ObjectRef{Type: store.ObjectOffer, ID: "o-1"}},
		{&leasesv1.MsgRenewLease{LeaseId: "l-1", BusinessId: "b-1"}, store.ObjectRef{Type: store.ObjectLease, ID: "l-1"}},
		{&leasesv1.MsgEndLease{LeaseId: "l-1", BusinessId: "b-1"}, store.ObjectRef{Type: store.ObjectLease, ID: "l-1"}},
	} {
		obj, biz, err := ForwardObject(c.msg)
		if err != nil || obj != c.want || biz != "b-1" {
			t.Fatalf("%T: %+v %q %v", c.msg, obj, biz, err)
		}
	}
	if _, _, err := ForwardObject(&contentv1.MsgRegisterContent{}); ClassOf(err) != Invalid {
		t.Fatal("a content message is not a forwarded provider message")
	}
}

func TestErrorText(t *testing.T) {
	err := invalid("request.content_id", "outside the id pattern")
	if err.Error() != "request: request.content_id: outside the id pattern" {
		t.Fatal(err.Error())
	}
	if ClassOf(fmt.Errorf("wrapped: %w", err)) != Invalid || ClassOf(errors.New("x")) != 0 {
		t.Fatal("ClassOf")
	}
}
