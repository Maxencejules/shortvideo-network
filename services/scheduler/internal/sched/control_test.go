package sched

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestControlRoutesRequireOperator(t *testing.T) {
	token := strings.Repeat("ab", 32)
	var role, status string
	id := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"role":"` + role + `","status":"` + status + `"}`))
	}))
	t.Cleanup(id.Close)

	allow := SessionControl(id.URL)
	req := httptest.NewRequest(http.MethodPost, "/v1/repairs", nil)
	if allow(req) {
		t.Fatal("missing bearer was accepted")
	}
	req.Header.Set("Authorization", "Bearer "+token)
	role, status = "creator", "active"
	if allow(req) {
		t.Fatal("creator session was accepted")
	}
	role, status = "operator", "deleted"
	if allow(req) {
		t.Fatal("deleted operator was accepted")
	}
	role, status = "operator", "active"
	if !allow(req) {
		t.Fatal("operator session was refused")
	}
	if SessionControl("")(req) {
		t.Fatal("empty session url accepted a caller")
	}

	refused := &Sched{}
	rec := httptest.NewRecorder()
	refused.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/publications", strings.NewReader(`{}`)))
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unconfigured control %d", rec.Code)
	}
	open := &Sched{cfg: Config{Control: func(*http.Request) bool { return true }}}
	rec = httptest.NewRecorder()
	open.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/repairs", strings.NewReader(`{`)))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("authorized malformed repair %d", rec.Code)
	}
}
