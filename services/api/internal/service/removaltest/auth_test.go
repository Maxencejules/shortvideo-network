package removaltest

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/service"
)

type tokens map[string]service.Principal

func (t tokens) Verify(token string) (service.Principal, error) {
	principal, ok := t[token]
	if !ok || principal.AccountID == "" || principal.Role == "" {
		return service.Principal{}, http.ErrNoCookie
	}
	return principal, nil
}

func TestRemovalRequiresOwnerAndPolicyRequiresOperator(t *testing.T) {
	gate, handler := service.NewRemovalHarness(tokens{
		"owner":   {AccountID: "creator1", Role: "creator"},
		"other":   {AccountID: "creator2", Role: "creator"},
		"operate": {AccountID: "operator1", Role: "operator"},
	}, map[string]string{"c-owned": "creator1"})

	code, body := call(handler, http.MethodPost, "/removal", "other", map[string]string{"content_id": "c-owned", "confirm": "withdraw"})
	if code != http.StatusForbidden || body["error"].(map[string]any)["code"] != "FORBIDDEN" || !gate.PublishAllowed("c-owned") {
		t.Fatalf("non-owner %d %#v allowed %t", code, body, gate.PublishAllowed("c-owned"))
	}
	code, body = call(handler, http.MethodPost, "/removal", "", map[string]string{"content_id": "c-owned", "confirm": "withdraw"})
	if code != http.StatusUnauthorized || !gate.PublishAllowed("c-owned") {
		t.Fatalf("no session %d %#v", code, body)
	}
	code, body = call(handler, http.MethodPost, "/removal", "owner", map[string]string{"content_id": "c-owned", "confirm": "withdraw"})
	if code != http.StatusAccepted || body["universal_deletion"] != false || gate.PublishAllowed("c-owned") {
		t.Fatalf("owner %d %#v still published %t", code, body, gate.PublishAllowed("c-owned"))
	}

	code, body = call(handler, http.MethodPost, "/v1/policy/delist", "", map[string]string{"content_id": "c-next", "account_id": "creator2"})
	if code != http.StatusForbidden || !gate.PublishAllowed("c-next") || !gate.UploadAllowed("creator2") {
		t.Fatalf("policy no session %d %#v", code, body)
	}
	code, body = call(handler, http.MethodPost, "/v1/policy/delist", "owner", map[string]string{"content_id": "c-next", "account_id": "creator2"})
	if code != http.StatusForbidden || !gate.PublishAllowed("c-next") || !gate.UploadAllowed("creator2") {
		t.Fatalf("policy creator %d %#v", code, body)
	}
	code, body = call(handler, http.MethodPost, "/v1/policy/delist", "operate", map[string]string{"content_id": "c-next", "account_id": "creator2"})
	if code != http.StatusOK || gate.PublishAllowed("c-next") || gate.UploadAllowed("creator2") || body["chain_end"] != false {
		t.Fatalf("policy operator %d %#v publish %t upload %t", code, body, gate.PublishAllowed("c-next"), gate.UploadAllowed("creator2"))
	}
}

func call(handler http.Handler, method, path, token string, body map[string]string) (int, map[string]any) {
	raw, err := json.Marshal(body)
	if err != nil {
		panic(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var doc map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &doc)
	return rec.Code, doc
}
