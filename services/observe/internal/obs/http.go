package obs

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

// Handler is the loopback API. Health and readiness are public. Metrics and
// logs require the operator token. The token is not accepted in the query.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.health)
	mux.HandleFunc("GET /readyz", s.ready)
	mux.HandleFunc("POST /v1/admit", s.authed(s.admit))
	mux.HandleFunc("POST /v1/release", s.authed(s.release))
	mux.HandleFunc("POST /v1/observations", s.authed(s.observation))
	mux.HandleFunc("GET /v1/metrics", s.authed(s.metrics))
	mux.HandleFunc("GET /v1/logs", s.authed(s.logList))
	mux.HandleFunc("GET /v1/usage", s.authed(s.usage))
	mux.HandleFunc("GET /v1/worksheet", s.authed(s.worksheet))
	mux.HandleFunc("GET /v1/dashboard", s.authed(s.dashboard))
	mux.HandleFunc("GET /v1/alerts", s.authed(s.alertList))
	mux.HandleFunc("POST /v1/fetch-check", s.authed(s.fetchCheck))
	return mux
}

func (s *Service) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true, "service": "observe", "public_launch": false,
	})
}

func (s *Service) ready(w http.ResponseWriter, _ *http.Request) {
	ok, bad := s.Ready()
	code := http.StatusOK
	if !ok {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, map[string]any{
		"ready": ok, "unusable": bad, "diagnosis": s.Diagnosis(),
	})
}

func (s *Service) authed(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.RawQuery != "" && strings.Contains(r.URL.RawQuery, "token") {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		if !s.authorize(r.Header.Get("Authorization")) {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Service) admit(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Kind           string `json:"kind"`
		Units          int64  `json:"units"`
		IdempotencyKey string `json:"idempotency_key"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	d, err := s.Admit(body.Kind, body.Units, body.IdempotencyKey)
	if err != nil {
		writeErr(w, err)
		return
	}
	code := http.StatusOK
	if d.Queued {
		code = http.StatusAccepted
	}
	if !d.Granted && !d.Queued {
		code = http.StatusTooManyRequests
	}
	writeJSON(w, code, d)
}

func (s *Service) release(w http.ResponseWriter, r *http.Request) {
	var body struct {
		GrantID string `json:"grant_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	released, replayed, err := s.Release(body.GrantID)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"released": released, "replayed": replayed})
}

func (s *Service) observation(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Component     string `json:"component"`
		CorrelationID string `json:"correlation_id"`
		Kind          string `json:"kind"`
		Class         string `json:"class"`
		Value         int64  `json:"value"`
		Message       string `json:"message"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	id, err := s.Observe(Observation{
		Component: body.Component, CorrelationID: body.CorrelationID,
		Kind: body.Kind, Class: body.Class, Value: body.Value, Message: body.Message,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"correlation_id": id})
}

func (s *Service) metrics(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Snapshot())
}

func (s *Service) logList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"logs": s.Logs()})
}

func (s *Service) usage(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.UsageReport())
}

func (s *Service) worksheet(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.Worksheet())
}

func (s *Service) dashboard(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"panels": s.Dashboard(), "public_launch": false})
}

func (s *Service) alertList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"active": s.ActiveAlerts(), "spend_cap": false,
	})
}

func (s *Service) fetchCheck(w http.ResponseWriter, r *http.Request) {
	var body struct {
		URL      string `json:"url"`
		Redirect string `json:"redirect"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if err := CheckFetch(body.URL, body.Redirect, s.lookup); err != nil {
		var fe FetchError
		if errors.As(err, &fe) {
			writeJSON(w, http.StatusForbidden, map[string]string{"error": fe.Reason})
			return
		}
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "refused"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"allowed": true})
}

func readJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	defer r.Body.Close()
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 4096))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed"})
		return false
	}
	dec := json.NewDecoder(strings.NewReader(string(raw)))
	if err := dec.Decode(dest); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "malformed"})
		return false
	}
	return true
}

func writeErr(w http.ResponseWriter, err error) {
	code := http.StatusBadRequest
	if errors.Is(err, ErrGrant) {
		code = http.StatusNotFound
	}
	if errors.Is(err, ErrOffChain) {
		code = http.StatusForbidden
	}
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(code)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}
