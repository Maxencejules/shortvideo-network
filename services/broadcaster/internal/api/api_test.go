package api

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// createRegister posts a valid REGISTER_CONTENT request as app-fixture.
func (e *env) createRegister(key, contentID string) answer {
	e.t.Helper()
	body := envelope(e.t, "REGISTER_CONTENT", key, 600, authContext("creator-7", "PUBLISH_CONTENT"),
		registerRequest(e.t, contentID, intentKey()))
	return e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", body)
}

func TestCreateRegisterContent(t *testing.T) {
	e := newEnv(t)
	a := e.createRegister("reg-1", "video-1")
	if a.status != http.StatusCreated {
		t.Fatalf("%d %s", a.status, a.body)
	}
	v := a.json(t)
	if v["status"] != "OPERATION_STATUS_REQUESTED" || v["kind"] != "REGISTER_CONTENT" || v["idempotency_key"] != "reg-1" {
		t.Fatalf("view %s", a.body)
	}
	if d := v["detail"].(map[string]any); d["phase"] != "QUEUED" || d["observer"] != "OBSERVER_UNAVAILABLE" {
		t.Fatalf("detail %v", d)
	}
	if e.st.enqueueCount() != 1 {
		t.Fatal("not enqueued once")
	}
	r := e.st.enqueued[0]
	if !bytes.Equal(r.SignerAddress, serviceAddr) || r.Kind != "REGISTER_CONTENT" || r.ExpiresIn != 600*time.Second ||
		r.Object != (store.ObjectRef{Type: store.ObjectContent, ID: "video-1", Version: 1}) || r.Forward != nil {
		t.Fatalf("enqueue request %+v", r)
	}
	// The stored request is the message without signer or business id,
	// and its digest is the documented one.
	msg := &contentv1.MsgRegisterContent{}
	if err := proto.Unmarshal(r.Request, msg); err != nil || len(msg.GetSigner()) != 0 || msg.GetBusinessId() != "" ||
		msg.GetContentId() != "video-1" {
		t.Fatalf("stored request %v %v", msg, err)
	}
	auth, authJSON, err := request.DecodeAuthContext(r.AuthContext, 2048)
	if err != nil || !bytes.Equal(authJSON, r.AuthContext) {
		t.Fatalf("stored context is not the canonical form: %v", err)
	}
	want, err := request.Digest(policy.KindRegisterContent, r.Request, auth)
	if err != nil || want != r.RequestDigest {
		t.Fatal("digest")
	}
	if v["request_digest"] != hex.EncodeToString(want[:]) {
		t.Fatal("the answer does not carry the request digest")
	}
}

// TestAnswersCarryNoSecrets checks owner answer Q4: answers return digests,
// never the hosting intent, its signature or key, or the authorization
// context.
func TestAnswersCarryNoSecrets(t *testing.T) {
	e := newEnv(t)
	req := registerRequest(t, "video-1", intentKey())
	body := envelope(t, "REGISTER_CONTENT", "reg-1", 600, authContext("creator-7", "PUBLISH_CONTENT"), req)
	a := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", body)
	if a.status != http.StatusCreated {
		t.Fatalf("%d %s", a.status, a.body)
	}
	id := a.json(t)["intent_id"].(string)
	got := e.do(e.as("app-fixture"), http.MethodGet, "/v1/intents/"+id, nil)
	list := e.do(e.as("app-fixture"), http.MethodGet, "/v1/intents", nil)
	intent := req["hosting_intent"].(map[string]any)
	for _, ans := range []answer{a, got, list} {
		for _, secret := range []string{intent["signature_hex"].(string), intent["public_key_hex"].(string), "creator-7",
			"policy-1", "PUBLISH_CONTENT", "intent-key-1"} {
			if bytes.Contains(ans.body, []byte(secret)) {
				t.Fatalf("answer carries %q: %s", secret, ans.body)
			}
		}
		if !bytes.Contains(ans.body, []byte(`"auth_context_digest":"`)) {
			t.Fatalf("answer has no context digest: %s", ans.body)
		}
	}
}

// TestStrictDecode checks plan 5.3 over HTTP: an unknown field, a
// duplicate key or a bad id is 400, a body above the cap is 413, an
// unknown kind is 422, and nothing is written.
func TestStrictDecode(t *testing.T) {
	e := newEnv(t)
	c := e.as("app-fixture")
	good := envelope(t, "OFFER_LEASE", "off-1", 600, authContext("creator-7", "SCHEDULE_HOSTING"), offerRequest("video-1", "offer-1"))
	var m map[string]any
	if err := json.Unmarshal(good, &m); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		body   []byte
		status int
		code   string
	}{
		{"unknown envelope field", bytes.Replace(good, []byte(`{"auth_context"`), []byte(`{"signer":"aa","auth_context"`), 1), 400, codeMalformed},
		{"duplicate key", bytes.Replace(good, []byte(`{"auth_context"`), []byte(`{"kind":"REGISTER_CONTENT","auth_context"`), 1), 400, codeMalformed},
		{"bad idempotency key", bytes.Replace(good, []byte(`"off-1"`), []byte(`"Off 1"`), 1), 400, codeMalformed},
		{"unknown request field", bytes.Replace(good, []byte(`"offer_id"`), []byte(`"note":"x","offer_id"`), 1), 400, codeMalformed},
		{"bad offer id", bytes.Replace(good, []byte(`"offer-1"`), []byte(`"Offer One"`), 1), 422, codeInvalid},
		{"unknown kind", bytes.Replace(good, []byte(`"OFFER_LEASE"`), []byte(`"REGISTER_PROVIDER"`), 1), 422, string(policy.CodeUnknownKind)},
		{"authority end kind", bytes.Replace(good, []byte(`"OFFER_LEASE"`), []byte(`"AUTHORITY_END"`), 1), 422, string(policy.CodeUnknownKind)},
		{"not JSON", []byte("kind=OFFER_LEASE"), 400, codeMalformed},
		{"empty", nil, 400, codeMalformed},
		{"too large", append(good[:len(good)-1], []byte(`,"pad":"`+strings.Repeat("x", 65536)+`"}`)...), 413, codeBodyTooLarge},
	}
	for _, tc := range cases {
		a := e.do(c, http.MethodPost, "/v1/intents", tc.body)
		if a.status != tc.status || a.code(t) != tc.code {
			t.Fatalf("%s: %d %s", tc.name, a.status, a.body)
		}
	}
	if e.st.enqueueCount() != 0 {
		t.Fatal("a refused request was enqueued")
	}
}

// TestKindAndPermissionChecks checks plan 5.2 and 5.7: a kind the caller
// may not send and a context without the kind's permission are 403; an
// unknown principal is 403; nothing is written.
func TestKindAndPermissionChecks(t *testing.T) {
	e := newEnv(t)
	offer := offerRequest("video-1", "offer-1")
	a := e.do(e.as("provider-agent"), http.MethodPost, "/v1/intents",
		envelope(t, "OFFER_LEASE", "o-1", 600, authContext("creator-7", "SCHEDULE_HOSTING"), offer))
	if a.status != http.StatusForbidden || a.code(t) != string(policy.CodeKindNotAllowed) {
		t.Fatalf("kind not allowed: %d %s", a.status, a.body)
	}
	a = e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents",
		envelope(t, "OFFER_LEASE", "o-1", 600, authContext("creator-7", "PUBLISH_CONTENT"), offer))
	if a.status != http.StatusForbidden || a.code(t) != string(policy.CodePermission) {
		t.Fatalf("permission missing: %d %s", a.status, a.body)
	}
	if e.ch.readCount() != 0 {
		t.Fatal("a kind or permission refusal read the chain: it must be refused before any chain read")
	}
	a = e.do(e.as("stranger"), http.MethodGet, "/v1/health", nil)
	if a.status != http.StatusForbidden || a.code(t) != codeCallerUnknown {
		t.Fatalf("unknown principal: %d %s", a.status, a.body)
	}
	if e.st.enqueueCount() != 0 {
		t.Fatal("a refused request was enqueued")
	}
}

// TestAuthContextRefusesPersonalData checks plan 5.5 over HTTP: every
// personal-data shape is 400 and writes no row.
func TestAuthContextRefusesPersonalData(t *testing.T) {
	e := newEnv(t)
	c := e.as("app-fixture")
	req := registerRequest(t, "video-1", intentKey())
	edits := map[string]func(a map[string]any){
		"email key":        func(a map[string]any) { a["email"] = "creator@example.com" },
		"ip key":           func(a map[string]any) { a["ip"] = "192.0.2.10" },
		"user agent":       func(a map[string]any) { a["user_agent"] = "Mozilla/5.0" },
		"token":            func(a map[string]any) { a["token"] = "abc.def.ghi" },
		"cookie":           func(a map[string]any) { a["cookie"] = "session=1" },
		"header copy":      func(a map[string]any) { a["headers"] = map[string]any{"X-Forwarded-For": "192.0.2.10"} },
		"free text":        func(a map[string]any) { a["note"] = "please publish my video" },
		"email in id":      func(a map[string]any) { a["principal_id"] = "creator@example.com" },
		"ip in id":         func(a map[string]any) { a["principal_id"] = "192.0.2.10" },
		"free text in ref": func(a map[string]any) { a["policy_ref"] = "terms accepted on monday" },
	}
	for name, edit := range edits {
		a := authContext("creator-7", "PUBLISH_CONTENT")
		edit(a)
		ans := e.do(c, http.MethodPost, "/v1/intents", envelope(t, "REGISTER_CONTENT", "reg-1", 600, a, req))
		if ans.status != http.StatusBadRequest || ans.code(t) != codeMalformed {
			t.Fatalf("%s: %d %s", name, ans.status, ans.body)
		}
	}
	if e.st.enqueueCount() != 0 {
		t.Fatal("a context with personal data was enqueued")
	}
}

// TestIdempotencyConflict checks plan 5.4: the same key with the same
// request (in any key order) returns the existing intent with 200; the
// same key with another request is 409; a retry is answered from the
// stored intent even when the chain state the checks read has changed.
func TestIdempotencyConflict(t *testing.T) {
	e := newEnv(t)
	first := e.createRegister("reg-1", "video-1")
	if first.status != http.StatusCreated {
		t.Fatalf("%d %s", first.status, first.body)
	}
	id := first.json(t)["intent_id"]
	again := e.createRegister("reg-1", "video-1")
	if again.status != http.StatusOK || again.json(t)["intent_id"] != id {
		t.Fatalf("same request: %d %s", again.status, again.body)
	}
	// Reordered keys and whitespace: same digest.
	req := registerRequest(t, "video-1", intentKey())
	reordered := fmt.Sprintf(`{ "request": %s, "auth_context": {"policy_ref":"policy-1","permissions":["PUBLISH_CONTENT"],
		"principal_id":"creator-7","principal_kind":"DEV_FIXTURE"}, "expires_in_seconds": 600, "idempotency_key": "reg-1",
		"kind": "REGISTER_CONTENT" }`, mustMarshal(t, req))
	r := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", []byte(reordered))
	if r.status != http.StatusOK || r.json(t)["intent_id"] != id {
		t.Fatalf("reordered: %d %s", r.status, r.body)
	}
	// The chain now shows the content (as after the register committed):
	// the policy check alone would refuse, but the retry gets its intent.
	e.ch.content["video-1/1"] = &contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "video-1",
		ContentVersion: 1, SubmittingHost: serviceAddr}}
	if r := e.createRegister("reg-1", "video-1"); r.status != http.StatusOK || r.json(t)["intent_id"] != id {
		t.Fatalf("retry after the chain changed: %d %s", r.status, r.body)
	}
	// Same key, another request.
	other := envelope(t, "REGISTER_CONTENT", "reg-1", 600, authContext("creator-7", "PUBLISH_CONTENT"),
		registerRequest(t, "video-2", intentKey()))
	c := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", other)
	if c.status != http.StatusConflict || c.code(t) != codeIdempotency {
		t.Fatalf("same key, other request: %d %s", c.status, c.body)
	}
	// Another permission set is another context, so another digest.
	ctxChange := envelope(t, "REGISTER_CONTENT", "reg-1", 600, authContext("creator-7", "PUBLISH_CONTENT", "SCHEDULE_HOSTING"),
		registerRequest(t, "video-1", intentKey()))
	if c := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", ctxChange); c.status != http.StatusConflict {
		t.Fatalf("same key, other context: %d %s", c.status, c.body)
	}
	// Another caller's key space is its own.
	o := e.do(e.as("other-app"), http.MethodPost, "/v1/intents", envelope(t, "REGISTER_CONTENT", "reg-1", 600,
		authContext("creator-7", "PUBLISH_CONTENT"), registerRequest(t, "video-3", intentKey())))
	if o.status != http.StatusCreated {
		t.Fatalf("another caller's key: %d %s", o.status, o.body)
	}
	if e.st.enqueueCount() != 2 {
		t.Fatalf("%d enqueues, want 2", e.st.enqueueCount())
	}
	// A SQL-path intent under the key is never the same request.
	e.st.put(store.Intent{ID: "00000000-0000-4000-8000-0000000000aa", CallerPrincipal: "app-fixture",
		IdempotencyKey: "sql-1", Kind: "REGISTER_CONTENT", Entry: "SQL", State: store.StateRequested,
		Phase: store.PhaseQueued, SQLDigest: bytes.Repeat([]byte{1}, 32)})
	if c := e.createRegister("sql-1", "video-4"); c.status != http.StatusConflict {
		t.Fatalf("SQL-path key: %d %s", c.status, c.body)
	}
}

func mustMarshal(t *testing.T, v any) string {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// TestRequestDigestCanonical checks plan 5.4 end to end: the digest the
// API stores does not depend on the body's key order or whitespace.
func TestRequestDigestCanonical(t *testing.T) {
	e := newEnv(t)
	offer := offerRequest("video-1", "offer-1")
	e.st.owners["video-1"] = store.ContentOwner{ContentID: "video-1", ContentVersion: 1, CallerPrincipal: "app-fixture",
		PrincipalID: "creator-7", SignerAddress: serviceAddr, Status: store.OwnerEffective}
	e.ch.content["video-1/1"] = &contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "video-1",
		ContentVersion: 1, SubmittingHost: serviceAddr}}
	a := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents",
		envelope(t, "OFFER_LEASE", "off-1", 600, authContext("creator-7", "SCHEDULE_HOSTING", "PUBLISH_CONTENT"), offer))
	if a.status != http.StatusCreated {
		t.Fatalf("%d %s", a.status, a.body)
	}
	spaced := strings.ReplaceAll(mustMarshal(t, offer), ",", " ,\n ")
	body := `{"kind":"OFFER_LEASE","request":` + spaced + `,"idempotency_key":"off-1","expires_in_seconds":600,
		"auth_context":{"permissions":["PUBLISH_CONTENT","SCHEDULE_HOSTING"],"principal_kind":"DEV_FIXTURE",
		"policy_ref":"policy-1","principal_id":"creator-7"}}`
	b := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", []byte(body))
	if b.status != http.StatusOK || b.json(t)["request_digest"] != a.json(t)["request_digest"] {
		t.Fatalf("respelled request: %d %s", b.status, b.body)
	}
	if r := e.st.enqueued[0]; r.Object != (store.ObjectRef{Type: store.ObjectOffer, ID: "offer-1"}) ||
		!bytes.Equal(r.SignerAddress, serviceAddr) {
		t.Fatalf("offer enqueue %+v", r)
	}
}

// TestContentIDUnavailableIsUniform checks plan 5.2 and DR-11-34: a content
// id pending or effective for another caller, held by another host on
// chain, or tombstoned gets one byte-identical answer that never says
// which.
func TestContentIDUnavailableIsUniform(t *testing.T) {
	e := newEnv(t)
	if a := e.createRegister("reg-1", "video-held"); a.status != http.StatusCreated {
		t.Fatalf("%d %s", a.status, a.body)
	}
	other := func(key, contentID string) answer {
		return e.do(e.as("other-app"), http.MethodPost, "/v1/intents", envelope(t, "REGISTER_CONTENT", key, 600,
			authContext("creator-7", "PUBLISH_CONTENT"), registerRequest(t, contentID, intentKey())))
	}
	e.ch.content["video-onchain/1"] = &contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "video-onchain",
		ContentVersion: 1, SubmittingHost: bytes.Repeat([]byte{0x33}, 20)}}
	e.ch.content["video-gone/1"] = &contentv1.QueryContentResponse{Tombstone: &contentv1.Tombstone{ContentId: "video-gone",
		ContentVersion: 1}}
	answers := []answer{other("k-1", "video-held"), other("k-2", "video-onchain"), other("k-3", "video-gone")}
	for i, a := range answers {
		if a.status != http.StatusConflict || a.code(t) != codeContentUnavailable {
			t.Fatalf("case %d: %d %s", i, a.status, a.body)
		}
		if !bytes.Equal(a.body, answers[0].body) {
			t.Fatalf("case %d answer differs: %s vs %s", i, a.body, answers[0].body)
		}
	}
	if e.st.enqueueCount() != 1 {
		t.Fatal("an unavailable content id was enqueued")
	}
}

// TestCrossCallerNotFound checks plan 5.7: another caller's intent answers
// 404 on read and cancel, like an unknown id, and never appears in its
// list; another caller's content is not visible to withdraw or offer.
func TestCrossCallerNotFound(t *testing.T) {
	e := newEnv(t)
	a := e.createRegister("reg-1", "video-1")
	if a.status != http.StatusCreated {
		t.Fatalf("%d %s", a.status, a.body)
	}
	id := a.json(t)["intent_id"].(string)
	other := e.as("other-app")
	unknown := e.do(other, http.MethodGet, "/v1/intents/00000000-0000-4000-8000-999999999999", nil)
	got := e.do(other, http.MethodGet, "/v1/intents/"+id, nil)
	if got.status != http.StatusNotFound || !bytes.Equal(got.body, unknown.body) {
		t.Fatalf("read: %d %s / %s", got.status, got.body, unknown.body)
	}
	if c := e.do(other, http.MethodPost, "/v1/intents/"+id+"/cancel", nil); c.status != http.StatusNotFound || !bytes.Equal(c.body, unknown.body) {
		t.Fatalf("cancel: %d %s", c.status, c.body)
	}
	if own := e.do(e.as("app-fixture"), http.MethodGet, "/v1/intents/"+id, nil); own.json(t)["status"] != "OPERATION_STATUS_REQUESTED" {
		t.Fatalf("another caller's cancel changed the intent: %s", own.body)
	}
	if l := e.do(other, http.MethodGet, "/v1/intents", nil); l.status != http.StatusOK || len(l.json(t)["intents"].([]any)) != 0 {
		t.Fatalf("list: %s", l.body)
	}
	if bad := e.do(other, http.MethodGet, "/v1/intents/not-a-uuid", nil); bad.status != http.StatusNotFound {
		t.Fatalf("malformed id: %d", bad.status)
	}
	// app-fixture's content is EFFECTIVE and on chain; other-app may not
	// withdraw it or schedule offers for it.
	e.st.owners["video-1"] = store.ContentOwner{ContentID: "video-1", ContentVersion: 1, CallerPrincipal: "app-fixture",
		PrincipalID: "creator-7", SignerAddress: serviceAddr, Status: store.OwnerEffective}
	e.ch.content["video-1/1"] = &contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "video-1",
		ContentVersion: 1, SubmittingHost: serviceAddr}}
	w := e.do(other, http.MethodPost, "/v1/intents", envelope(t, "WITHDRAW_CONTENT", "w-1", 600,
		authContext("creator-7", "WITHDRAW_CONTENT"), withdrawRequest(t, "video-1", intentKey())))
	o := e.do(other, http.MethodPost, "/v1/intents", envelope(t, "OFFER_LEASE", "o-1", 600,
		authContext("creator-7", "SCHEDULE_HOSTING"), offerRequest("video-1", "offer-9")))
	for _, ans := range []answer{w, o} {
		if ans.status != http.StatusNotFound || !bytes.Equal(ans.body, unknown.body) {
			t.Fatalf("other caller's content: %d %s", ans.status, ans.body)
		}
	}
	// The owner may.
	ow := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", envelope(t, "WITHDRAW_CONTENT", "w-1", 600,
		authContext("creator-7", "WITHDRAW_CONTENT"), withdrawRequest(t, "video-1", intentKey())))
	if ow.status != http.StatusCreated {
		t.Fatalf("owner withdraw: %d %s", ow.status, ow.body)
	}
}

// TestPublicationPermission checks plan 7.4 (owner answer Q3) through the
// API: a hosting intent signed by a key the operator did not register for
// the principal is 403 and writes nothing.
func TestPublicationPermission(t *testing.T) {
	e := newEnv(t)
	a := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", envelope(t, "REGISTER_CONTENT", "reg-1", 600,
		authContext("creator-7", "PUBLISH_CONTENT"), registerRequest(t, "video-1", otherKey())))
	if a.status != http.StatusForbidden || a.code(t) != string(policy.CodeIntentKeyUnregistered) {
		t.Fatalf("unregistered intent key: %d %s", a.status, a.body)
	}
	b := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", envelope(t, "REGISTER_CONTENT", "reg-2", 600,
		authContext("creator-8", "PUBLISH_CONTENT"), registerRequest(t, "video-1", intentKey())))
	if b.status != http.StatusForbidden || b.code(t) != string(policy.CodeIntentKeyUnregistered) {
		t.Fatalf("principal without a registered key: %d %s", b.status, b.body)
	}
	if e.st.enqueueCount() != 0 {
		t.Fatal("an unauthorized register was enqueued")
	}
}

// endRequest is a RELAY_END_LEASE request signed by k over the end payload
// of lease-1 held by provider-1 for video-1.
func endRequest(t *testing.T, k ed25519.PrivateKey) map[string]any {
	t.Helper()
	sb, err := leases.EndSignBytes("lease-1", "provider-1", 1)
	if err != nil {
		t.Fatal(err)
	}
	return map[string]any{
		"lease_id": "lease-1", "provider_id": "provider-1", "content_id": "video-1",
		"provider_signature": innerSig("SIGNATURE_DOMAIN_PROVIDER_END", "pkey-1", k.Public().(ed25519.PublicKey), ed25519.Sign(k, sb)),
	}
}

// TestRelayPrecheckAndAuthority checks plan 7.3 P1 and 5.7 through the
// API: a provider signature by another key is 422 and writes nothing; a
// valid one from the caller bound to the provider is enqueued for the
// BROADCASTER key; a caller neither bound nor owning the offer is 403.
func TestRelayPrecheckAndAuthority(t *testing.T) {
	e := newEnv(t)
	e.ch.providers["provider-1"] = &providersv1.Provider{ProviderId: "provider-1", Account: bytes.Repeat([]byte{0x44}, 20),
		SigningPubkey: providerKey().Public().(ed25519.PublicKey), SigningKeyId: "pkey-1",
		State: providersv1.ProviderState_PROVIDER_STATE_ADMITTED}
	e.ch.leases["provider-1/video-1"] = &leasesv1.Lease{LeaseId: "lease-1", OfferId: "offer-1", ContentId: "video-1",
		ContentVersion: 1, ProviderId: "provider-1", TermVersion: 1}
	bad := e.do(e.as("provider-agent"), http.MethodPost, "/v1/intents",
		envelope(t, "RELAY_END_LEASE", "end-1", 600, authContext("provider-1", "RELAY_PROVIDER_SIGNED"), endRequest(t, otherKey())))
	if bad.status != http.StatusUnprocessableEntity || bad.code(t) != string(policy.CodeProviderSignature) {
		t.Fatalf("wrong key: %d %s", bad.status, bad.body)
	}
	notMine := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents",
		envelope(t, "RELAY_END_LEASE", "end-1", 600, authContext("provider-1", "RELAY_PROVIDER_SIGNED"), endRequest(t, providerKey())))
	if notMine.status != http.StatusForbidden || notMine.code(t) != string(policy.CodeRelayNotAllowed) {
		t.Fatalf("caller without relay authority: %d %s", notMine.status, notMine.body)
	}
	good := e.do(e.as("provider-agent"), http.MethodPost, "/v1/intents",
		envelope(t, "RELAY_END_LEASE", "end-2", 600, authContext("provider-1", "RELAY_PROVIDER_SIGNED"), endRequest(t, providerKey())))
	if good.status != http.StatusCreated {
		t.Fatalf("valid relay: %d %s", good.status, good.body)
	}
	r := e.st.enqueued[0]
	if !bytes.Equal(r.SignerAddress, broadcasterAddr) || r.Object != (store.ObjectRef{Type: store.ObjectLease, ID: "lease-1"}) {
		t.Fatalf("relay enqueue %+v", r)
	}
	// A caller that owns the committed offer may relay too.
	e.st.scheduled["app-fixture/offer-1"] = true
	owner := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents",
		envelope(t, "RELAY_END_LEASE", "end-3", 600, authContext("provider-1", "RELAY_PROVIDER_SIGNED"), endRequest(t, providerKey())))
	if owner.status != http.StatusCreated {
		t.Fatalf("offer owner relay: %d %s", owner.status, owner.body)
	}
	// RENEW and END relays may not outlive the relay local expiry (Q4).
	long := e.do(e.as("provider-agent"), http.MethodPost, "/v1/intents",
		envelope(t, "RELAY_END_LEASE", "end-4", 3601, authContext("provider-1", "RELAY_PROVIDER_SIGNED"), endRequest(t, providerKey())))
	if long.status != http.StatusUnprocessableEntity || long.code(t) != codeLifetime {
		t.Fatalf("relay above the local expiry: %d %s", long.status, long.body)
	}
}

func TestLifetimeBounds(t *testing.T) {
	e := newEnv(t)
	for _, s := range []int64{29, 86401} {
		a := e.do(e.as("app-fixture"), http.MethodPost, "/v1/intents", envelope(t, "REGISTER_CONTENT", "reg-1", s,
			authContext("creator-7", "PUBLISH_CONTENT"), registerRequest(t, "video-1", intentKey())))
		if a.status != http.StatusUnprocessableEntity || a.code(t) != codeLifetime {
			t.Fatalf("expires_in %d: %d %s", s, a.status, a.body)
		}
	}
	if e.st.enqueueCount() != 0 {
		t.Fatal("an out-of-bounds lifetime was enqueued")
	}
}

// TestEnqueueErrorMapping checks how outbox and chain failures are
// answered: a full caller is 429, a lock or statement timeout and a
// failed chain read are 503, a caller unknown to the outbox is 403.
func TestEnqueueErrorMapping(t *testing.T) {
	cases := map[error]struct {
		status int
		code   string
	}{
		store.ErrCapReached:            {429, codeCapReached},
		store.ErrLockTimeout:           {503, codeUnavailable},
		store.ErrStatementTimeout:      {503, codeUnavailable},
		store.ErrCallerUnknown:         {403, codeCallerUnknown},
		store.ErrChainUnbound:          {503, codeUnavailable},
		store.ErrSignerClass:           {500, codeInternal},
		store.ErrTxTracked:             {409, codeTxTracked},
		errors.New("connection reset"): {503, codeUnavailable},
	}
	for storeErr, want := range cases {
		e := newEnv(t)
		e.st.enqueueErr = storeErr
		a := e.createRegister("reg-1", "video-1")
		if a.status != want.status || a.code(t) != want.code {
			t.Fatalf("%v: %d %s", storeErr, a.status, a.body)
		}
		if bytes.Contains(a.body, []byte(storeErr.Error())) {
			t.Fatalf("answer echoes the internal error: %s", a.body)
		}
	}
	e := newEnv(t)
	e.ch.statusErr = errors.New("node unreachable")
	if a := e.createRegister("reg-1", "video-1"); a.status != http.StatusServiceUnavailable || a.code(t) != codeUnavailable {
		t.Fatalf("chain unavailable: %d %s", a.status, a.body)
	}
}

// committedIntent is a COMMITTED intent with its canonical evidence.
func committedIntent(id string) store.Intent {
	hash := bytes.Repeat([]byte{0xaa}, 32)
	own := true
	obj := store.ObjectRef{Type: store.ObjectContent, ID: "video-1", Version: 1}
	now := time.Date(2026, 11, 8, 12, 0, 0, 0, time.UTC)
	final := now.Add(time.Minute)
	return store.Intent{
		ID: id, CallerPrincipal: "app-fixture", IdempotencyKey: "reg-c", Kind: "REGISTER_CONTENT", Entry: "HTTP",
		State: store.StateCommitted, BusinessID: "bcastx", AttemptCount: 1, CreatedAt: now, ExpiresAt: now.Add(time.Hour),
		FinalAt: &final, CommittedTxHash: hash, OwnTransaction: &own, RequestDigest: bytes.Repeat([]byte{1}, 32),
		AuthContextDigest: bytes.Repeat([]byte{2}, 32), Object: &obj, SignerAddress: serviceAddr,
		Commitment: &store.CommitmentRecord{ChainID: testChainID, BusinessID: "bcastx", Height: 120,
			BlockHash: bytes.Repeat([]byte{0xbb}, 32), TxHash: hash, FirstCommitTxHash: hash, TxIndex: 0, MsgIndex: 0,
			Code: 0, Codespace: "", GasWanted: 2000000, GasUsed: 104000,
			Events: []byte(`[{"type":"svn.events.v1.ContentRegistered","index":0}]`), CommittingSigner: serviceAddr,
			RecordedAt: final},
	}
}

// TestStatusViews checks plan 5.8 and section 10: requested, submitted
// (with phases), uncertain, awaiting deadline, committed with its
// evidence, failed with its code, a failed forward that the provider must
// sign again, pending while a stall is shown with a
// fresh observer, and the underlying status with OBSERVER_UNAVAILABLE when
// the observer is stale.
func TestStatusViews(t *testing.T) {
	e := newEnv(t)
	c := e.as("app-fixture")
	obj := store.ObjectRef{Type: store.ObjectContent, ID: "video-1", Version: 1}
	base := func(id string, st store.State, ph store.Phase, attempts int) store.Intent {
		return store.Intent{ID: id, CallerPrincipal: "app-fixture", IdempotencyKey: "k" + id[len(id)-2:],
			Kind: "REGISTER_CONTENT", Entry: "HTTP", State: st, Phase: ph, AttemptCount: attempts, Object: &obj,
			CreatedAt: time.Unix(1800000000, 0).UTC(), ExpiresAt: time.Unix(1800000600, 0).UTC()}
	}
	uncertain := base("00000000-0000-4000-8000-000000000013", store.StateInFlight, store.PhaseUncertain, 1)
	uncertain.LandingHeight = 140
	uncertain.LastOutcome = "UNKNOWN"
	acked := base("00000000-0000-4000-8000-000000000012", store.StateInFlight, store.PhaseAcknowledged, 1)
	acked.LastAck = &store.ChainCode{Code: 0}
	failed := base("00000000-0000-4000-8000-000000000015", store.StateFailed, store.PhaseNone, 2)
	failed.Reason, failed.Failure = "EXECUTION_FAILED", &store.ChainCode{Code: 9, Codespace: "svn"}
	resign := base("00000000-0000-4000-8000-000000000017", store.StateFailed, store.PhaseNone, 1)
	resign.Kind, resign.Reason = "FORWARD_PROVIDER_TX", "PROVIDER_MUST_RESIGN"
	resign.Object = &store.ObjectRef{Type: store.ObjectOffer, ID: "offer-1"}
	for _, it := range []store.Intent{
		base("00000000-0000-4000-8000-000000000011", store.StateRequested, store.PhaseQueued, 0), acked, uncertain,
		base("00000000-0000-4000-8000-000000000014", store.StateInFlight, store.PhaseAwaitingDeadline, 1), failed,
		committedIntent("00000000-0000-4000-8000-000000000016"), resign,
	} {
		e.st.put(it)
	}
	get := func(id string) map[string]any {
		t.Helper()
		a := e.do(c, http.MethodGet, "/v1/intents/00000000-0000-4000-8000-0000000000"+id, nil)
		if a.status != http.StatusOK {
			t.Fatalf("%s: %d %s", id, a.status, a.body)
		}
		return a.json(t)
	}
	detail := func(v map[string]any) map[string]any { return v["detail"].(map[string]any) }

	if v := get("11"); v["status"] != "OPERATION_STATUS_REQUESTED" || detail(v)["phase"] != "QUEUED" {
		t.Fatalf("requested: %v", v)
	}
	if v := get("12"); v["status"] != "OPERATION_STATUS_SUBMITTED" || detail(v)["phase"] != "ACKNOWLEDGED" || v["commitment"] != nil {
		t.Fatalf("acknowledged is submitted, never committed: %v", v)
	}
	if v := get("13"); v["status"] != "OPERATION_STATUS_SUBMITTED" || detail(v)["phase"] != "UNCERTAIN" ||
		detail(v)["last_landing_height"] != float64(140) || detail(v)["last_outcome"] != "UNKNOWN" {
		t.Fatalf("uncertain: %v", v)
	}
	if v := get("14"); v["status"] != "OPERATION_STATUS_SUBMITTED" || detail(v)["reason"] != "EXPIRED_AWAITING_CHAIN" {
		t.Fatalf("awaiting deadline: %v", v)
	}
	if v := get("15"); v["status"] != "OPERATION_STATUS_FAILED" || v["failure"].(map[string]any)["reason"] != "EXECUTION_FAILED" ||
		v["failure"].(map[string]any)["code"] != float64(9) || v["failure"].(map[string]any)["codespace"] != "svn" {
		t.Fatalf("failed: %v", v)
	}
	// A forward whose bytes can no longer commit (plan 7.3 P3) is shown
	// FAILED with the worker's reason, not as an integrity fault.
	if v := get("17"); v["status"] != "OPERATION_STATUS_FAILED" || v["failure"].(map[string]any)["reason"] != "PROVIDER_MUST_RESIGN" {
		t.Fatalf("forward that must be re-signed: %v", v)
	}
	v := get("16")
	cm, _ := v["commitment"].(map[string]any)
	if v["status"] != "OPERATION_STATUS_COMMITTED" || cm == nil || cm["height"] != float64(120) ||
		cm["tx_hash"] != strings.Repeat("aa", 32) || cm["block_hash"] != strings.Repeat("bb", 32) ||
		cm["chain_id"] != testChainID || cm["own_transaction"] != true || !strings.HasPrefix(cm["committing_signer"].(string), "svn") {
		t.Fatalf("committed: %v", v)
	}

	// A stall shown with a fresh observer: non-terminal intents are
	// PENDING_STALL with their phase kept; terminal ones are unchanged.
	e.st.health = &store.ChainHealth{LatestHeight: 130, Age: 500 * time.Millisecond}
	e.st.episode = &store.StallEpisode{ID: 1, Reason: store.StallHeight, StartedHeight: 130, StartedAt: time.Unix(1800000100, 0).UTC()}
	if v := get("13"); v["status"] != "OPERATION_STATUS_PENDING_STALL" || detail(v)["phase"] != "UNCERTAIN" ||
		detail(v)["observer"] != "FRESH" || detail(v)["stall"].(map[string]any)["since_height"] != float64(130) {
		t.Fatalf("stalled: %v", v)
	}
	if v := get("16"); v["status"] != "OPERATION_STATUS_COMMITTED" {
		t.Fatalf("a stall changed a committed intent: %v", v)
	}
	if v := get("15"); v["status"] != "OPERATION_STATUS_FAILED" {
		t.Fatalf("a stall changed a failed intent: %v", v)
	}
	// A stale observer is never a stall claim.
	e.st.health = &store.ChainHealth{LatestHeight: 130, Age: time.Minute}
	if v := get("13"); v["status"] != "OPERATION_STATUS_SUBMITTED" || detail(v)["observer"] != "OBSERVER_UNAVAILABLE" ||
		detail(v)["stall"] != nil {
		t.Fatalf("stale observer: %v", v)
	}
}

// TestNeverCommittedWithoutEvidence checks plan 2.3 item 3 and 9.4 at the
// API: a COMMITTED row without matching canonical evidence is answered
// INTEGRITY and never shown as committed; a non-committed row carrying a
// commitment is refused too.
func TestNeverCommittedWithoutEvidence(t *testing.T) {
	e := newEnv(t)
	c := e.as("app-fixture")
	edits := map[string]func(it *store.Intent){
		"no commitment":         func(it *store.Intent) { it.Commitment = nil },
		"no canonical hash":     func(it *store.Intent) { it.CommittedTxHash = nil },
		"other chain":           func(it *store.Intent) { it.Commitment.ChainID = "shortvideo-dev-other" },
		"failure code":          func(it *store.Intent) { it.Commitment.Code = 5 },
		"hash differs":          func(it *store.Intent) { it.Commitment.FirstCommitTxHash = bytes.Repeat([]byte{0xcc}, 32) },
		"no block hash":         func(it *store.Intent) { it.Commitment.BlockHash = nil },
		"tx hash short":         func(it *store.Intent) { it.Commitment.TxHash = it.Commitment.TxHash[:31] },
		"height zero":           func(it *store.Intent) { it.Commitment.Height = 0 },
		"own transaction unset": func(it *store.Intent) { it.OwnTransaction = nil },
		"events not JSON":       func(it *store.Intent) { it.Commitment.Events = []byte("{") },
		"acked with commitment": func(it *store.Intent) { it.State, it.Phase = store.StateInFlight, store.PhaseAcknowledged },
		"requested with hash": func(it *store.Intent) {
			it.State, it.Phase, it.Commitment = store.StateRequested, store.PhaseQueued, nil
		},
		"failed with commitment": func(it *store.Intent) { it.State, it.Reason = store.StateFailed, "REJECTED" },
	}
	n := 0
	for name, edit := range edits {
		n++
		id := fmt.Sprintf("00000000-0000-4000-8000-%012d", 500+n)
		it := committedIntent(id)
		c2 := *it.Commitment
		it.Commitment = &c2
		edit(&it)
		e.st.put(it)
		a := e.do(c, http.MethodGet, "/v1/intents/"+id, nil)
		if a.status != http.StatusInternalServerError || a.code(t) != codeIntegrity || bytes.Contains(a.body, []byte("COMMITTED")) {
			t.Fatalf("%s: %d %s", name, a.status, a.body)
		}
	}
	good := committedIntent("00000000-0000-4000-8000-000000000999")
	e.st.put(good)
	if a := e.do(c, http.MethodGet, "/v1/intents/"+good.ID, nil); a.status != http.StatusOK {
		t.Fatalf("the unedited fixture is refused: %d %s", a.status, a.body)
	}
}

func TestCancel(t *testing.T) {
	e := newEnv(t)
	a := e.createRegister("reg-1", "video-1")
	id := a.json(t)["intent_id"].(string)
	c := e.as("app-fixture")
	if r := e.do(c, http.MethodPost, "/v1/intents/"+id+"/cancel", []byte(`{"x":1}`)); r.status != http.StatusBadRequest {
		t.Fatalf("cancel with a body: %d", r.status)
	}
	r := e.do(c, http.MethodPost, "/v1/intents/"+id+"/cancel", nil)
	if r.status != http.StatusOK || r.json(t)["status"] != "OPERATION_STATUS_FAILED" ||
		r.json(t)["failure"].(map[string]any)["reason"] != "CANCELLED" {
		t.Fatalf("cancel: %d %s", r.status, r.body)
	}
	signed := store.Intent{ID: "00000000-0000-4000-8000-000000000777", CallerPrincipal: "app-fixture", IdempotencyKey: "s-1",
		Kind: "REGISTER_CONTENT", Entry: "HTTP", State: store.StateInFlight, Phase: store.PhaseSigned, AttemptCount: 1,
		Object: &store.ObjectRef{Type: store.ObjectContent, ID: "v", Version: 1}}
	e.st.put(signed)
	if r := e.do(c, http.MethodPost, "/v1/intents/"+signed.ID+"/cancel", nil); r.status != http.StatusConflict ||
		r.code(t) != codeNotCancellable {
		t.Fatalf("cancel after signing: %d %s", r.status, r.body)
	}
}

func TestListPagingAndFilters(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		if a := e.createRegister(fmt.Sprintf("reg-%d", i), fmt.Sprintf("video-%d", i)); a.status != http.StatusCreated {
			t.Fatalf("%d %s", a.status, a.body)
		}
	}
	e.st.put(committedIntent("00000000-0000-4000-8000-000000000099"))
	c := e.as("app-fixture")
	page := e.do(c, http.MethodGet, "/v1/intents?limit=2", nil).json(t)
	if len(page["intents"].([]any)) != 2 || page["next_page_token"] == nil {
		t.Fatalf("first page %v", page)
	}
	seen := 2
	token := page["next_page_token"].(string)
	for token != "" {
		p := e.do(c, http.MethodGet, "/v1/intents?limit=2&page_token="+token, nil).json(t)
		seen += len(p["intents"].([]any))
		token, _ = p["next_page_token"].(string)
	}
	if seen != 6 {
		t.Fatalf("paged %d intents, want 6", seen)
	}
	if p := e.do(c, http.MethodGet, "/v1/intents?status=OPERATION_STATUS_COMMITTED", nil).json(t); len(p["intents"].([]any)) != 1 {
		t.Fatalf("committed filter %v", p)
	}
	if p := e.do(c, http.MethodGet, "/v1/intents?status=OPERATION_STATUS_REQUESTED", nil).json(t); len(p["intents"].([]any)) != 5 {
		t.Fatalf("requested filter %v", p)
	}
	if p := e.do(c, http.MethodGet, "/v1/intents?status=OPERATION_STATUS_PENDING_STALL", nil).json(t); len(p["intents"].([]any)) != 0 {
		t.Fatalf("pending filter without a stall %v", p)
	}
	e.st.health = &store.ChainHealth{Age: time.Millisecond}
	e.st.episode = &store.StallEpisode{ID: 1, Reason: store.StallUnreachable, StartedHeight: 99}
	if p := e.do(c, http.MethodGet, "/v1/intents?status=OPERATION_STATUS_PENDING_STALL", nil).json(t); len(p["intents"].([]any)) != 5 {
		t.Fatalf("pending filter during a stall %v", p)
	}
	if p := e.do(c, http.MethodGet, "/v1/intents?status=OPERATION_STATUS_REQUESTED", nil).json(t); len(p["intents"].([]any)) != 0 {
		t.Fatalf("requested filter during a stall %v", p)
	}
	for _, q := range []string{"limit=0", "limit=101", "limit=01", "limit=x", "status=UNCERTAIN", "page_token=abc",
		"page_token=00000000-0000-4000-8000-999999999999", "other=1", "limit=1&limit=2"} {
		if a := e.do(c, http.MethodGet, "/v1/intents?"+q, nil); a.status != http.StatusBadRequest {
			t.Fatalf("%s: %d %s", q, a.status, a.body)
		}
	}
}

func TestSignersAndHealth(t *testing.T) {
	e := newEnv(t)
	c := e.as("provider-agent")
	s := e.do(c, http.MethodGet, "/v1/signers", nil).json(t)
	signers := s["signers"].([]any)
	if s["chain_id"] != testChainID || len(signers) != 2 {
		t.Fatalf("signers %v", s)
	}
	first := signers[0].(map[string]any)
	if first["class"] != "SERVICE" || first["address_hex"] != hex.EncodeToString(serviceAddr) ||
		!strings.HasPrefix(first["address"].(string), "svn1") || first["public_key_hex"] != strings.Repeat("02", 33) {
		t.Fatalf("service signer %v", first)
	}
	h := e.do(c, http.MethodGet, "/v1/health", nil).json(t)
	if h["chain_id"] != testChainID || h["observer"] != "OBSERVER_UNAVAILABLE" || h["database"] != "REACHABLE" ||
		h["stall"].(map[string]any)["open"] != false {
		t.Fatalf("health before the first record %v", h)
	}
	e.st.health = &store.ChainHealth{LatestHeight: 77, Age: time.Second}
	e.st.episode = &store.StallEpisode{ID: 3, Reason: store.StallCatchingUp, StartedHeight: 70}
	h = e.do(c, http.MethodGet, "/v1/health", nil).json(t)
	if h["latest_height"] != float64(77) || h["observer"] != "FRESH" || h["stall"].(map[string]any)["shown"] != true {
		t.Fatalf("health %v", h)
	}
	e.st.healthErr = errors.New("connection refused")
	h = e.do(c, http.MethodGet, "/v1/health", nil).json(t)
	if h["database"] != "UNREACHABLE" || h["observer"] != "OBSERVER_UNAVAILABLE" {
		t.Fatalf("health without a database %v", h)
	}
	if a := e.do(c, http.MethodGet, "/v1/nothing", nil); a.status != http.StatusNotFound {
		t.Fatalf("unknown path %d", a.status)
	}
	if a := e.do(c, http.MethodDelete, "/v1/intents", nil); a.status != http.StatusNotFound && a.status != http.StatusMethodNotAllowed {
		t.Fatalf("unknown method %d", a.status)
	}
}

// fakeListener reports an address without listening on it (unit test).
type fakeListener struct{ addr net.Addr }

func (f fakeListener) Accept() (net.Conn, error) { return nil, errors.New("fake listener") }
func (f fakeListener) Close() error              { return nil }
func (f fakeListener) Addr() net.Addr            { return f.addr }

// TestLoopbackOnly checks plan 5.6: the configuration refuses a listen
// address that is not a loopback IP literal, and Serve refuses a listener
// on any other address before it accepts a connection.
func TestLoopbackOnly(t *testing.T) {
	p := newPKI(t)
	certFile, keyFile, caFile := p.serverFiles()
	for _, addr := range []string{"0.0.0.0:8443", "192.0.2.1:8443", "localhost:8443", "[::]:8443"} {
		cfg := testConfig(certFile, keyFile, caFile, addr)
		if _, err := New(cfg, testDeps(t, newFakeStore(), newFakeChain(testChainID))); err == nil {
			t.Fatalf("listen address %s accepted", addr)
		}
	}
	srv, err := New(testConfig(certFile, keyFile, caFile, "127.0.0.1:8443"), testDeps(t, newFakeStore(), newFakeChain(testChainID)))
	if err != nil {
		t.Fatal(err)
	}
	for _, ip := range []string{"192.0.2.1", "0.0.0.0", "::"} {
		ln := fakeListener{addr: &net.TCPAddr{IP: net.ParseIP(ip), Port: 8443}}
		if err := srv.Serve(context.Background(), ln); err == nil || !strings.Contains(err.Error(), "loopback") {
			t.Fatalf("listener on %s: %v", ip, err)
		}
	}
	if err := srv.Serve(context.Background(), fakeListener{addr: &net.UnixAddr{Name: "/tmp/x", Net: "unix"}}); err == nil {
		t.Fatal("unix listener accepted")
	}
	tc := srv.TLSConfig()
	if tc.ClientAuth != 4 || tc.MinVersion != 0x0304 { // tls.RequireAndVerifyClientCert, TLS 1.3
		t.Fatalf("tls config %v %x", tc.ClientAuth, tc.MinVersion)
	}
}

// TestDevPrincipalsRefuseNonDevChain checks plan 5.6: development
// principals refuse a chain id without the shortvideo-dev- prefix.
func TestDevPrincipalsRefuseNonDevChain(t *testing.T) {
	p := newPKI(t)
	certFile, keyFile, caFile := p.serverFiles()
	cfg := testConfig(certFile, keyFile, caFile, "127.0.0.1:8443")
	cfg.ChainID = "shortvideo-beta-1"
	if _, err := New(cfg, testDeps(t, newFakeStore(), newFakeChain(testChainID))); err == nil ||
		!strings.Contains(err.Error(), "development principals") {
		t.Fatalf("non-dev chain: %v", err)
	}
}

func TestNewRefusesBadDeps(t *testing.T) {
	p := newPKI(t)
	certFile, keyFile, caFile := p.serverFiles()
	cfg := testConfig(certFile, keyFile, caFile, "127.0.0.1:8443")
	edits := map[string]func(c *Config, d *Deps){
		"no store":            func(c *Config, d *Deps) { d.Store = nil },
		"no registry":         func(c *Config, d *Deps) { d.Registry = nil },
		"one signer":          func(c *Config, d *Deps) { d.Signers = d.Signers[:1] },
		"same signer address": func(c *Config, d *Deps) { d.Signers[1].Address = serviceAddr },
		"no callers":          func(c *Config, d *Deps) { c.Callers = nil },
		"duplicate caller":    func(c *Config, d *Deps) { c.Callers = append(c.Callers, c.Callers[0]) },
		"unrouted caller kind": func(c *Config, d *Deps) {
			c.Callers = []policy.Caller{{Principal: "p", Kinds: []policy.Kind{"AUTHORITY_END"}}}
		},
		"body cap":             func(c *Config, d *Deps) { c.MaxBodyBytes = 70000 },
		"page size":            func(c *Config, d *Deps) { c.PageSize = 0 },
		"codec of other chain": func(c *Config, d *Deps) { c.ChainID = "shortvideo-dev-other" },
		"key file mode": func(c *Config, d *Deps) {
			if err := os.Chmod(c.TLSKeyFile, 0o644); err != nil {
				t.Fatal(err)
			}
		},
		"ca file is the server cert": func(c *Config, d *Deps) { c.ClientCAFile = c.TLSCertFile },
	}
	for name, edit := range edits {
		c := cfg
		c.Callers = append([]policy.Caller(nil), cfg.Callers...)
		d := testDeps(t, newFakeStore(), newFakeChain(testChainID))
		d.Signers = append([]SignerKey(nil), d.Signers...)
		edit(&c, &d)
		if _, err := New(c, d); err == nil {
			t.Fatalf("%s accepted", name)
		}
		if err := os.Chmod(cfg.TLSKeyFile, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}
