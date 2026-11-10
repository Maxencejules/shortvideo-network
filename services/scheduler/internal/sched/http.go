package sched

import (
	"encoding/json"
	"net/http"
	"strconv"
)

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// Handler is the cooperating read and distribution API.
// Distribution does not serve the origin backup.
func (s *Sched) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"service":"scheduler","scheduler_signed":false}`))
	})
	mux.HandleFunc("GET /v1/availability", func(w http.ResponseWriter, r *http.Request) {
		view, err := s.Availability(r.Context(), r.URL.Query().Get("content"))
		if err != nil {
			http.Error(w, "unknown content", http.StatusNotFound)
			return
		}
		writeJSON(w, view)
	})
	mux.HandleFunc("GET /v1/distribution/{content}/{version}", func(w http.ResponseWriter, r *http.Request) {
		version, err := strconv.ParseUint(r.PathValue("version"), 10, 64)
		if err != nil || version == 0 {
			http.Error(w, "malformed", http.StatusBadRequest)
			return
		}
		code, body := s.Distribute(r.Context(), r.PathValue("content"), version)
		if code != 200 {
			w.WriteHeader(code)
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Cache-Control", "private, no-store")
		_, _ = w.Write(body)
	})
	return mux
}
