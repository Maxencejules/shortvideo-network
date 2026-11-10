// Package httpserver is the provider's HTTPS API.
package httpserver

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/agents/provider/internal/agent"
)

// Server serves the provider API on ln.
type Server struct {
	Agent *agent.Agent
	Ln    net.Listener
}

// Serve blocks until the listener closes.
func (s *Server) Serve(cert, key string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /capability", s.capability)
	mux.HandleFunc("GET /enrollment", s.enrollment)
	mux.HandleFunc("POST /inventory", s.inventory)
	mux.HandleFunc("POST /offers/{offer_id}/acceptance", s.accept)
	mux.HandleFunc("GET /packages/{content_id}/{content_version}", s.pkg)
	mux.HandleFunc("POST /transfers/{transfer_id}/receipt", s.receipt)
	mux.HandleFunc("POST /admin/drain", s.admin(s.drain))
	mux.HandleFunc("POST /admin/withdraw", s.admin(s.withdraw))
	mux.HandleFunc("POST /admin/rotate", s.admin(s.rotate))
	httpServer := &http.Server{
		Handler:           guard(mux),
		TLSConfig:         &tls.Config{MinVersion: tls.VersionTLS12},
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       30 * time.Second,
	}
	return httpServer.ServeTLS(s.Ln, cert, key)
}

func guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw := r.RequestURI
		lower := strings.ToLower(raw)
		if strings.Contains(raw, "..") || strings.Contains(lower, "%2e") || strings.Contains(raw, "\\") || strings.Contains(lower, "%5c") {
			writeErr(w, 400, "path refused")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, map[string]any{"ok": true})
}

func (s *Server) status(w http.ResponseWriter, _ *http.Request) {
	doc, err := s.Agent.StatusReport()
	if err != nil {
		writeAgent(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func (s *Server) capability(w http.ResponseWriter, _ *http.Request) {
	doc, err := s.Agent.CapabilityDocument()
	if err != nil {
		writeAgent(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func (s *Server) enrollment(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, 200, s.Agent.Enrollment())
}

func (s *Server) inventory(w http.ResponseWriter, r *http.Request) {
	if _, err := readBody(r, 1024); err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.Agent.Inventory()
	if err != nil {
		writeAgent(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func (s *Server) accept(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, 1<<20)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.Agent.Accept(r.PathValue("offer_id"), body)
	if err != nil {
		writeAgent(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func (s *Server) pkg(w http.ResponseWriter, r *http.Request) {
	token := bearer(r)
	buf, err := s.Agent.Package(r.PathValue("content_id"), r.PathValue("content_version"), token)
	if err != nil {
		writeAgent(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(200)
	_, _ = w.Write(buf)
}

func (s *Server) receipt(w http.ResponseWriter, r *http.Request) {
	doc, err := s.Agent.Receipt(r.PathValue("transfer_id"))
	if err != nil {
		writeAgent(w, err)
		return
	}
	writeJSON(w, 201, doc)
}

func (s *Server) admin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.Agent.Authorize(bearer(r)) {
			writeErr(w, 401, "unauthorized")
			return
		}
		next(w, r)
	}
}

func (s *Server) drain(w http.ResponseWriter, _ *http.Request) {
	doc, err := s.Agent.Drain()
	if err != nil {
		writeAgent(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func (s *Server) withdraw(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, 4096)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	var in struct {
		LeaseID string `json:"lease_id"`
	}
	if json.Unmarshal(body, &in) != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.Agent.Withdraw(in.LeaseID)
	if err != nil {
		writeAgent(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func (s *Server) rotate(w http.ResponseWriter, _ *http.Request) {
	doc, err := s.Agent.Rotate()
	if err != nil {
		writeAgent(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if !strings.HasPrefix(h, p) {
		return ""
	}
	return strings.TrimSpace(h[len(p):])
}

func readBody(r *http.Request, n int64) ([]byte, error) {
	buf, err := io.ReadAll(io.LimitReader(r.Body, n+1))
	if err != nil {
		return nil, err
	}
	if int64(len(buf)) > n {
		return nil, errors.New("body")
	}
	return buf, nil
}

func writeAgent(w http.ResponseWriter, err error) {
	var ae *agent.Error
	if errors.As(err, &ae) {
		writeErr(w, ae.Status, ae.Code)
		return
	}
	writeErr(w, 500, "error")
}

func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]string{"error": code})
}

func writeJSON(w http.ResponseWriter, status int, doc any) {
	body, err := json.Marshal(doc)
	if err != nil {
		http.Error(w, "error", 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}
