package chainview

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusForwardsTheNodeBody(t *testing.T) {
	const body = `{"result":{"sync_info":{"latest_block_height":"4","catching_up":false}}}`
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/status" {
			http.NotFound(w, r)
			return
		}
		_, _ = io.WriteString(w, body)
	}))
	defer node.Close()
	view := Server{RPC: node.URL}
	rec := httptest.NewRecorder()
	view.status(rec, httptest.NewRequest(http.MethodGet, "/status", nil))
	if rec.Code != 200 || strings.TrimSpace(rec.Body.String()) != body {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
}

func TestStatusDoesNotInventAHeight(t *testing.T) {
	node := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "down", http.StatusBadGateway)
	}))
	defer node.Close()
	view := Server{RPC: node.URL}
	rec := httptest.NewRecorder()
	view.status(rec, httptest.NewRequest(http.MethodGet, "/status", nil))
	if rec.Code == 200 || strings.Contains(rec.Body.String(), "latest_block_height") {
		t.Fatalf("invented status %d %s", rec.Code, rec.Body.String())
	}
}
