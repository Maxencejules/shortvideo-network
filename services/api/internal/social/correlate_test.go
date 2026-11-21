package social

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCorrelationHeader(t *testing.T) {
	h := correlate(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	req := httptest.NewRequest(http.MethodGet, "/v1/discovery", nil)
	req.Header.Set("X-Correlation-ID", "person@example.com")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	got := rr.Header().Get("X-Correlation-ID")
	if len(got) != 33 || got[0] != 'c' || strings.Contains(got, "@") {
		t.Fatalf("id %q", got)
	}
	req = httptest.NewRequest(http.MethodGet, "/v1/discovery", nil)
	req.Header.Set("X-Correlation-ID", got)
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	if rr.Header().Get("X-Correlation-ID") != got {
		t.Fatal("a valid id was replaced")
	}
}
