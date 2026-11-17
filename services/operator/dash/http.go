package dash

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// PublicHandler is the read-only listener. It has no admin route and it
// accepts no request body that changes state.
func (s *Service) PublicHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "service": "operator", "consensus_key_accepted": false})
	})
	mux.HandleFunc("GET /v1/status", func(w http.ResponseWriter, r *http.Request) {
		s.cors(w, r)
		writeJSON(w, 200, s.Snapshot(r.Context()))
	})
	mux.HandleFunc("GET /v1/council", func(w http.ResponseWriter, r *http.Request) {
		s.cors(w, r)
		writeJSON(w, 200, s.Council(r.Context()))
	})
	mux.HandleFunc("GET /v1/onboarding", func(w http.ResponseWriter, r *http.Request) {
		s.cors(w, r)
		writeJSON(w, 200, s.Onboarding())
	})
	mux.HandleFunc("OPTIONS /v1/status", s.preflight)
	mux.HandleFunc("OPTIONS /v1/council", s.preflight)
	mux.HandleFunc("OPTIONS /v1/onboarding", s.preflight)
	return methodGuard(mux, true)
}

// AdminHandler is the signing and admin listener. It does not accept a
// consensus private key, and it does not serve the public read model.
func (s *Service) AdminHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.page)
	mux.HandleFunc("GET /operator.js", s.page)
	if s.cfg.DevSessions {
		mux.HandleFunc("POST /v1/dev/session", s.devSession)
	}
	mux.HandleFunc("POST /v1/onboarding", s.authed(s.onboardHTTP))
	mux.HandleFunc("POST /v1/limits", s.authed(s.limitsHTTP))
	mux.HandleFunc("POST /v1/stop-offers", s.authed(s.stopHTTP))
	mux.HandleFunc("POST /v1/drain", s.authed(s.drainHTTP))
	mux.HandleFunc("POST /v1/handoff", s.authed(s.handoffHTTP))
	mux.HandleFunc("POST /v1/renewals/review", s.authed(s.renewHTTP))
	mux.HandleFunc("POST /v1/endpoints/disable", s.authed(s.disableHTTP))
	mux.HandleFunc("POST /v1/governance", s.authed(s.governHTTP))
	mux.HandleFunc("POST /v1/policy", s.authed(s.policyHTTP))
	mux.HandleFunc("GET /v1/moderation/queue", s.authed(s.queueHTTP))
	mux.HandleFunc("GET /v1/audit", s.authed(s.auditHTTP))
	return mux
}

func methodGuard(next http.Handler, public bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if public && r.Method != http.MethodGet && r.Method != http.MethodOptions {
			writeErr(w, 405, "method")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Service) preflight(w http.ResponseWriter, r *http.Request) {
	if !s.cors(w, r) {
		writeErr(w, 403, "origin")
		return
	}
	w.WriteHeader(204)
}

func (s *Service) cors(w http.ResponseWriter, r *http.Request) bool {
	s.mu.Lock()
	origin := s.cfg.AdminOrigin
	s.mu.Unlock()
	got := r.Header.Get("Origin")
	if got == "" || origin == "" || got != origin {
		return false
	}
	w.Header().Set("Access-Control-Allow-Origin", origin)
	w.Header().Set("Vary", "Origin")
	w.Header().Set("Access-Control-Allow-Methods", "GET")
	w.Header().Set("Access-Control-Allow-Headers", "Accept")
	return true
}

func (s *Service) page(w http.ResponseWriter, r *http.Request) {
	if s.cfg.WebDir == "" {
		http.NotFound(w, r)
		return
	}
	name := "operator.html"
	kind := "text/html; charset=utf-8"
	if r.URL.Path == "/operator.js" {
		name = "operator.js"
		kind = "text/javascript; charset=utf-8"
	}
	body, err := readWeb(s.cfg.WebDir, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if name == "operator.html" {
		s.mu.Lock()
		pub := s.cfg.PublicOrigin
		s.mu.Unlock()
		body = bytes.ReplaceAll(body, []byte("__SVN_PUBLIC__"), []byte(pub))
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; connect-src 'self' http://127.0.0.1:* http://localhost:*; img-src 'self'; object-src 'none'; base-uri 'none'; form-action 'self'")
	}
	w.Header().Set("Content-Type", kind)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(200)
	_, _ = w.Write(body)
}

type authedFunc func(http.ResponseWriter, *http.Request, principal)

func (s *Service) authed(next authedFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p, err := s.principal(bearer(r))
		if err != nil {
			writeAction(w, err)
			return
		}
		next(w, r, p)
	}
}

func (s *Service) devSession(w http.ResponseWriter, r *http.Request) {
	body, err := readBody(r, 1024)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	var in struct {
		Role    string `json:"role"`
		Account string `json:"account_id"`
	}
	if decodeStrict(body, &in) != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.IssueDevSession(in.Role, in.Account)
	if err != nil {
		writeAction(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func (s *Service) onboardHTTP(w http.ResponseWriter, r *http.Request, p principal) {
	body, err := readBody(r, 4096)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.Onboard(p, body)
	if err != nil {
		writeAction(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func (s *Service) limitsHTTP(w http.ResponseWriter, r *http.Request, p principal) {
	body, err := readBody(r, 4096)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.Limits(r.Context(), p, body)
	writeResult(w, doc, err)
}

func (s *Service) stopHTTP(w http.ResponseWriter, r *http.Request, p principal) {
	body, err := readBody(r, 4096)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.StopOffers(r.Context(), p, body)
	writeResult(w, doc, err)
}

func (s *Service) drainHTTP(w http.ResponseWriter, r *http.Request, p principal) {
	body, err := readBody(r, 4096)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.Drain(r.Context(), p, body)
	writeResult(w, doc, err)
}

func (s *Service) handoffHTTP(w http.ResponseWriter, r *http.Request, p principal) {
	body, err := readBody(r, 4096)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.Handoff(r.Context(), p, body)
	writeResult(w, doc, err)
}

func (s *Service) disableHTTP(w http.ResponseWriter, r *http.Request, p principal) {
	body, err := readBody(r, 4096)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.DisableEndpoint(r.Context(), p, body)
	writeResult(w, doc, err)
}

func readWeb(dir, name string) ([]byte, error) {
	if strings.Contains(name, "/") || name == "" || strings.Contains(dir, "..") {
		return nil, errors.New("web")
	}
	return os.ReadFile(filepath.Join(dir, name))
}

func (s *Service) renewHTTP(w http.ResponseWriter, r *http.Request, p principal) {
	body, err := readBody(r, 4096)
	if err != nil {
		writeErr(w, 400, "malformed")
		return
	}
	doc, err := s.ReviewRenewal(p, body)
	writeResult(w, doc, err)
}

func (s *Service) governHTTP(w http.ResponseWriter, _ *http.Request, p principal) {
	doc, err := s.RefuseGovernance(p, "governance")
	writeResult(w, doc, err)
}

func (s *Service) policyHTTP(w http.ResponseWriter, _ *http.Request, p principal) {
	doc, err := s.RefuseGovernance(p, "policy")
	writeResult(w, doc, err)
}

func (s *Service) queueHTTP(w http.ResponseWriter, r *http.Request, p principal) {
	doc, err := s.Queue(r.Context(), p)
	if err != nil {
		writeAction(w, err)
		return
	}
	writeJSON(w, 200, doc)
}

func (s *Service) auditHTTP(w http.ResponseWriter, _ *http.Request, p principal) {
	rows, err := s.Audit(p)
	if err != nil {
		writeAction(w, err)
		return
	}
	writeJSON(w, 200, map[string]any{"audit": rows, "chain_submitted": false})
}

func writeResult(w http.ResponseWriter, doc ActionResult, err error) {
	if err != nil {
		var ae *Error
		if errors.As(err, &ae) {
			if doc.Error == "" {
				doc.Error = ae.Code
			}
			doc.ChainSubmitted = false
			doc.ConsensusKeyAccepted = false
			writeJSON(w, ae.Status, doc)
			return
		}
		writeErr(w, 500, "error")
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

func writeAction(w http.ResponseWriter, err error) {
	var ae *Error
	if errors.As(err, &ae) {
		writeJSON(w, ae.Status, map[string]any{"error": ae.Code, "chain_submitted": false, "consensus_key_accepted": false})
		return
	}
	writeErr(w, 500, "error")
}

func writeErr(w http.ResponseWriter, status int, code string) {
	writeJSON(w, status, map[string]any{"error": code, "chain_submitted": false})
}

func writeJSON(w http.ResponseWriter, status int, doc any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	w.WriteHeader(status)
	_ = enc.Encode(doc)
}
