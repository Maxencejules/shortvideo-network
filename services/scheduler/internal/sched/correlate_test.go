package sched

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
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	req.Header.Set("X-Correlation-ID", "acc_example1")
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
	got := rr.Header().Get("X-Correlation-ID")
	if len(got) != 33 || got[0] != 'c' || strings.Contains(got, "acc_") {
		t.Fatalf("id %q", got)
	}
}
