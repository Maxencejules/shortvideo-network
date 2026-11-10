package sched

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/stage"
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
	mux.HandleFunc("POST /v1/publications", s.publishHTTP)
	mux.HandleFunc("POST /v1/repairs", s.repairHTTP)
	mux.HandleFunc("POST /v1/origin", s.originHTTP)
	mux.HandleFunc("POST /v1/hosts", s.hostHTTP)
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

type publicationRequest struct {
	ContentID   string `json:"content_id"`
	Version     uint64 `json:"version"`
	ManifestHex string `json:"manifest_hex"`
	TermsHex    string `json:"terms_hex"`
	TermSeconds int64  `json:"term_seconds"`
	Scheduler   string `json:"scheduler_hex"`
	Approved    bool   `json:"approved"`
	ApprovalID  string `json:"approval_id"`
	Segments    []struct {
		ID        string `json:"segment_id"`
		SHA256    string `json:"sha256"`
		ByteCount uint64 `json:"byte_count"`
		Path      string `json:"path"`
	} `json:"segments"`
}

func (s *Sched) publishHTTP(w http.ResponseWriter, r *http.Request) {
	if s.cfg.Stage == nil {
		http.Error(w, "stage unset", http.StatusServiceUnavailable)
		return
	}
	var req publicationRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&req); err != nil || req.ContentID == "" || req.Version == 0 {
		http.Error(w, "malformed", http.StatusBadRequest)
		return
	}
	parts := map[string][]byte{}
	segs := make([]policy.Segment, 0, len(req.Segments))
	var concat []byte
	for _, seg := range req.Segments {
		buf, err := readUnder(s.cfg.PackageRoot, seg.Path)
		if err != nil || uint64(len(buf)) != seg.ByteCount {
			http.Error(w, "segment", http.StatusBadRequest)
			return
		}
		parts[seg.ID] = buf
		segs = append(segs, policy.Segment{ID: seg.ID, SHA256: seg.SHA256, ByteCount: seg.ByteCount})
		concat = append(concat, buf...)
	}
	sched, err := decodeHexFixed(req.Scheduler, 20)
	if err != nil && req.Scheduler != "" {
		http.Error(w, "scheduler", http.StatusBadRequest)
		return
	}
	var total uint64
	for _, seg := range segs {
		total += seg.ByteCount
	}
	text := policy.ManifestText(req.ContentID, req.Version, total, segs)
	sum := sha256.Sum256([]byte(text))
	if req.ManifestHex != "" && req.ManifestHex != hex.EncodeToString(sum[:]) {
		http.Error(w, "manifest", http.StatusBadRequest)
		return
	}
	s.cfg.Stage.PutOrigin(req.ContentID, stage.Object{Manifest: text, Parts: parts})
	view, err := s.Publish(r.Context(), Package{
		ContentID: req.ContentID, Version: req.Version, Segments: segs,
		ExpectedSHA: sumHex(concat), TermsHex: req.TermsHex, ManifestHex: req.ManifestHex,
		Scheduler: sched, TermSeconds: req.TermSeconds,
	}, req.Approved, req.ApprovalID)
	if err != nil {
		http.Error(w, "publish failed", http.StatusBadGateway)
		return
	}
	writeJSON(w, view)
}

func (s *Sched) repairHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContentID string `json:"content_id"`
		Version   uint64 `json:"version"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.ContentID == "" || req.Version == 0 {
		http.Error(w, "malformed", http.StatusBadRequest)
		return
	}
	job, err := s.Repair(r.Context(), req.ContentID, req.Version)
	if err != nil {
		http.Error(w, "repair failed", http.StatusBadGateway)
		return
	}
	writeJSON(w, map[string]any{"state": job.State, "attempts": job.Attempts, "traces": s.Traces()})
}

func (s *Sched) hostHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProviderID string `json:"provider_id"`
		Endpoint   string `json:"endpoint"`
		CAFile     string `json:"ca_file"`
		AdminToken string `json:"admin_token"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.ProviderID == "" {
		http.Error(w, "malformed", http.StatusBadRequest)
		return
	}
	if req.Endpoint != "" && !strings.HasPrefix(req.Endpoint, "https://127.0.0.1:") {
		http.Error(w, "endpoint", http.StatusBadRequest)
		return
	}
	s.UpdateHost(req.ProviderID, req.Endpoint, req.CAFile, req.AdminToken)
	writeJSON(w, map[string]string{"provider_id": req.ProviderID})
}

func (s *Sched) originHTTP(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ContentID string `json:"content_id"`
		Up        bool   `json:"up"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&req); err != nil || req.ContentID == "" {
		http.Error(w, "malformed", http.StatusBadRequest)
		return
	}
	s.SetOrigin(req.ContentID, req.Up)
	writeJSON(w, map[string]any{"content_id": req.ContentID, "origin_up": req.Up})
}

func readUnder(root, path string) ([]byte, error) {
	if root == "" || path == "" || strings.Contains(path, "..") {
		return nil, os.ErrInvalid
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if abs != absRoot && !strings.HasPrefix(abs, absRoot+string(os.PathSeparator)) {
		return nil, os.ErrPermission
	}
	f, err := os.Open(abs)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return io.ReadAll(io.LimitReader(f, 1<<26))
}

func decodeHexFixed(s string, n int) ([]byte, error) {
	if s == "" {
		return nil, nil
	}
	buf, err := hex.DecodeString(s)
	if err != nil || len(buf) != n {
		return nil, os.ErrInvalid
	}
	return buf, nil
}
