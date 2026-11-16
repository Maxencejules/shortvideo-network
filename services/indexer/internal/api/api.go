// Package api is the loopback read API for hosting services.
//
// Every response carries the trust disclosure and the stale flag. The API
// does not add a publication approval, and it does not turn a stale read
// into a lease renewal.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/status"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/store"
)

// Reader is the projection the API serves.
type Reader interface {
	View(ctx context.Context) (status.View, error)
	Load(ctx context.Context) (store.Projection, error)
}

// Server is the read API.
type Server struct {
	Reader Reader
}

// Handler returns the HTTP handler.
// These routes are the committed chain read model. They are public
// coordination, not application account data, so they do not take an
// account session.
func (s Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/status", s.status)
	mux.HandleFunc("GET /v1/providers", s.providers)
	mux.HandleFunc("GET /v1/content", s.content)
	mux.HandleFunc("GET /v1/leases", s.leases)
	mux.HandleFunc("GET /v1/deadlines", s.deadlines)
	mux.HandleFunc("GET /v1/policy", s.policy)
	return mux
}

func (s Server) status(w http.ResponseWriter, r *http.Request) {
	view, err := s.Reader.View(r.Context())
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, view)
}

func (s Server) providers(w http.ResponseWriter, r *http.Request) {
	s.with(w, r, func(view status.View, proj store.Projection) any {
		return map[string]any{"status": view, "providers": proj.Providers}
	})
}

func (s Server) content(w http.ResponseWriter, r *http.Request) {
	s.with(w, r, func(view status.View, proj store.Projection) any {
		return map[string]any{"status": view, "content": proj.Content}
	})
}

func (s Server) leases(w http.ResponseWriter, r *http.Request) {
	s.with(w, r, func(view status.View, proj store.Projection) any {
		return map[string]any{"status": view, "leases": proj.Leases}
	})
}

func (s Server) deadlines(w http.ResponseWriter, r *http.Request) {
	s.with(w, r, func(view status.View, proj store.Projection) any {
		type row struct {
			Kind     string `json:"kind"`
			ID       string `json:"id"`
			Deadline int64  `json:"deadline_unix_seconds"`
			Known    bool   `json:"expiry_known"`
		}
		var rows []row
		for _, lease := range proj.Leases {
			if lease.Active {
				rows = append(rows, row{Kind: "lease", ID: lease.ID, Deadline: lease.Deadline, Known: true})
			}
		}
		for _, offer := range proj.Offers {
			if offer.State == "OPEN" {
				rows = append(rows, row{Kind: "offer", ID: offer.ID, Known: false})
			}
		}
		return map[string]any{"status": view, "deadlines": rows}
	})
}

func (s Server) policy(w http.ResponseWriter, r *http.Request) {
	s.with(w, r, func(view status.View, proj store.Projection) any {
		return map[string]any{"status": view, "policy": proj.Policy, "present": proj.HasPolicy}
	})
}

func (s Server) with(w http.ResponseWriter, r *http.Request, build func(status.View, store.Projection) any) {
	view, err := s.Reader.View(r.Context())
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	proj, err := s.Reader.Load(r.Context())
	if err != nil {
		http.Error(w, "read failed", http.StatusInternalServerError)
		return
	}
	writeJSON(w, build(view, proj))
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

// ListenAndServe serves until ctx is cancelled.
func ListenAndServe(ctx context.Context, addr string, h http.Handler) error {
	srv := &http.Server{Addr: addr, Handler: h, ReadHeaderTimeout: 5 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case <-ctx.Done():
		shut, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shut)
		return nil
	case err := <-errc:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}
