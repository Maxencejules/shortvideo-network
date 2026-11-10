package service

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

var (
	uploadIDRE = regexp.MustCompile(`^u[0-9a-f]{32}$`)
	keyRE      = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,79}$`)
	digestRE   = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Handler is the upload HTTP API. Unregistered paths, including signup and playback, are 404.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /upload-intents", s.createIntent)
	mux.HandleFunc("PUT /uploads/{uploadID}/content", s.putContent)
	mux.HandleFunc("POST /uploads/{uploadID}/complete", s.complete)
	mux.HandleFunc("GET /uploads/{uploadID}", s.getUpload)
	mux.HandleFunc("DELETE /uploads/{uploadID}", s.cancel)
	mux.HandleFunc("POST /publish", s.publish)
	return mux
}

func (s *Service) createIntent(w http.ResponseWriter, r *http.Request) {
	principal, err := s.session(r)
	if err != nil {
		writeMapped(w, err)
		return
	}
	var body struct {
		IdempotencyKey string   `json:"idempotency_key"`
		DeclaredBytes  *int64   `json:"declared_bytes"`
		ClientDuration *float64 `json:"client_duration_seconds"`
		Filename       string   `json:"filename"`
		MIME           string   `json:"mime"`
	}
	// ClientDuration, Filename, and MIME are accepted and ignored.
	// The worker reads the bytes. The object name is generated.
	if !readJSON(w, r, &body) {
		return
	}
	if !keyRE.MatchString(body.IdempotencyKey) || body.DeclaredBytes == nil || *body.DeclaredBytes < 1 {
		writeError(w, 400, "MALFORMED", "request body is not valid")
		return
	}
	_, _, _ = body.ClientDuration, body.Filename, body.MIME
	declared := *body.DeclaredBytes
	if declared > s.limits.MaxInputBytes {
		writeError(w, 400, "LIMIT", "input exceeds the proposed limit")
		return
	}
	hits, err := s.store.Hit(r.Context(), principal.AccountID, s.now().UTC().Truncate(time.Minute))
	if err != nil {
		writeMapped(w, err)
		return
	}
	if hits > s.limits.IntentRatePerMinute {
		writeMapped(w, errRate)
		return
	}
	uploadID, err := newID("u")
	if err != nil {
		writeMapped(w, err)
		return
	}
	objectName, err := newObject()
	if err != nil {
		writeMapped(w, err)
		return
	}
	contentID, err := newID("c")
	if err != nil {
		writeMapped(w, err)
		return
	}
	now := s.now()
	row, _, err := s.store.CreateIntent(r.Context(), intentInput{
		UploadID:   uploadID,
		Owner:      principal.AccountID,
		Key:        body.IdempotencyKey,
		Hash:       requestHash(body.IdempotencyKey, declared),
		ContentID:  contentID,
		ObjectName: objectName,
		Declared:   declared,
		Output:     s.limits.OutputReservationBytes,
		Expires:    now.Add(time.Duration(s.limits.TransferTTLSeconds) * time.Second),
		Now:        now,
	})
	if err != nil {
		writeMapped(w, err)
		return
	}
	response := intentResponse{UploadID: row.UploadID, Status: row.Status, LimitsLabel: LimitsLabel, Limits: s.limitView()}
	if row.Status == "RESERVED" || row.Status == "RECEIVING" || row.Status == "RECEIVED" {
		token, hash, err := newToken()
		if err != nil {
			writeMapped(w, err)
			return
		}
		ok, err := s.store.Grant(r.Context(), row.UploadID, principal.AccountID, hash, s.now())
		if err != nil {
			writeMapped(w, err)
			return
		}
		if ok {
			response.Transfer = &transferView{
				Scheme:    "Transfer",
				Token:     token,
				ExpiresAt: row.Expires.UTC().Format(time.RFC3339),
				Method:    "PUT",
				Path:      "/uploads/" + row.UploadID + "/content",
			}
		}
	}
	writeJSON(w, http.StatusAccepted, response)
}

func (s *Service) putContent(w http.ResponseWriter, r *http.Request) {
	if !uploadIDRE.MatchString(r.PathValue("uploadID")) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "upload not found")
		return
	}
	row, err := s.transferUpload(r)
	if err != nil {
		writeMapped(w, err)
		return
	}
	if row.UploadID != r.PathValue("uploadID") {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "upload not found")
		return
	}
	if !row.Expires.After(s.now()) {
		writeMapped(w, errExpired)
		return
	}
	path, err := s.blob.Path(row.ObjectName)
	if err != nil {
		writeMapped(w, err)
		return
	}
	n, sum, oversized, err := WriteBody(path, r.Body, row.DeclaredBytes)
	if err != nil {
		_ = s.blob.Remove(row.ObjectName)
		writeMapped(w, err)
		return
	}
	if oversized {
		_ = s.blob.Remove(row.ObjectName)
		_ = s.store.Release(r.Context(), row.UploadID, row.Owner, "REJECTED", s.now())
		writeMapped(w, errLimit)
		return
	}
	status := "RECEIVING"
	storedSum := ""
	if n == row.DeclaredBytes {
		status = "RECEIVED"
		storedSum = sum
	}
	if err := s.store.SaveReceive(r.Context(), row.UploadID, row.Owner, status, storedSum, n, s.now()); err != nil {
		writeMapped(w, err)
		return
	}
	if status == "RECEIVED" {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	updated, err := s.store.Owned(r.Context(), row.UploadID, row.Owner)
	if err != nil {
		writeMapped(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.statusView(updated))
}

func (s *Service) complete(w http.ResponseWriter, r *http.Request) {
	principal, err := s.session(r)
	if err != nil {
		writeMapped(w, err)
		return
	}
	id := r.PathValue("uploadID")
	if !uploadIDRE.MatchString(id) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "upload not found")
		return
	}
	var body struct {
		Digest string `json:"digest"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !digestRE.MatchString(body.Digest) {
		writeError(w, 400, "MALFORMED", "request body is not valid")
		return
	}
	row, err := s.store.BeginProbe(r.Context(), id, principal.AccountID, body.Digest, s.now())
	if errors.Is(err, errDone) {
		writeJSON(w, http.StatusAccepted, s.statusView(row))
		return
	}
	if err != nil {
		writeMapped(w, err)
		return
	}
	if s.delay > 0 {
		timer := time.NewTimer(s.delay)
		select {
		case <-r.Context().Done():
			timer.Stop()
			writeError(w, http.StatusRequestTimeout, "STATE", "upload probe did not finish")
			return
		case <-timer.C:
		}
	}
	if err := s.finish(r.Context(), row); err != nil {
		if errors.Is(err, errDone) {
			loaded, loadErr := s.store.Owned(r.Context(), id, principal.AccountID)
			if loadErr != nil {
				writeMapped(w, loadErr)
				return
			}
			writeJSON(w, http.StatusAccepted, s.statusView(loaded))
			return
		}
		writeMapped(w, err)
		return
	}
	loaded, err := s.store.Owned(r.Context(), id, principal.AccountID)
	if err != nil {
		writeMapped(w, err)
		return
	}
	writeJSON(w, http.StatusAccepted, s.statusView(loaded))
}

func (s *Service) getUpload(w http.ResponseWriter, r *http.Request) {
	principal, err := s.session(r)
	if err != nil {
		writeMapped(w, err)
		return
	}
	id := r.PathValue("uploadID")
	if !uploadIDRE.MatchString(id) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "upload not found")
		return
	}
	row, err := s.store.Owned(r.Context(), id, principal.AccountID)
	if err != nil {
		writeMapped(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.statusView(row))
}

func (s *Service) cancel(w http.ResponseWriter, r *http.Request) {
	principal, err := s.session(r)
	if err != nil {
		writeMapped(w, err)
		return
	}
	id := r.PathValue("uploadID")
	if !uploadIDRE.MatchString(id) {
		writeError(w, http.StatusNotFound, "NOT_FOUND", "upload not found")
		return
	}
	row, err := s.store.Owned(r.Context(), id, principal.AccountID)
	if err != nil {
		writeMapped(w, err)
		return
	}
	if !row.Released {
		if err := s.store.Release(r.Context(), id, principal.AccountID, "CANCELLED", s.now()); err != nil {
			writeMapped(w, err)
			return
		}
		_ = s.blob.Remove(row.ObjectName)
	}
	loaded, err := s.store.Owned(r.Context(), id, principal.AccountID)
	if err != nil {
		writeMapped(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.statusView(loaded))
}

func (s *Service) publish(w http.ResponseWriter, r *http.Request) {
	if _, err := s.session(r); err != nil {
		writeMapped(w, err)
		return
	}
	writeError(w, http.StatusForbidden, "PUBLICATION_BLOCKED", "publication is separate from upload and public launch is disabled")
}

func (s *Service) session(r *http.Request) (Principal, error) {
	kind, token, err := splitAuth(r.Header.Get("Authorization"))
	if err != nil || kind != "Bearer" {
		return Principal{}, errUnauthenticated
	}
	principal, err := s.verify.Verify(token)
	if err != nil {
		return Principal{}, errUnauthenticated
	}
	if principal.Role != "creator" {
		return Principal{}, errRole
	}
	return principal, nil
}

func (s *Service) transferUpload(r *http.Request) (uploadRow, error) {
	kind, token, err := splitAuth(r.Header.Get("Authorization"))
	if err != nil || kind != "Transfer" {
		return uploadRow{}, errUnauthenticated
	}
	sum := sha256.Sum256([]byte(token))
	return s.store.ByTransfer(r.Context(), hex.EncodeToString(sum[:]))
}

func splitAuth(header string) (string, string, error) {
	if len(header) > 512 {
		return "", "", errUnauthenticated
	}
	kind, token, ok := strings.Cut(header, " ")
	if !ok || token == "" || strings.Contains(token, " ") {
		return "", "", errUnauthenticated
	}
	if kind != "Bearer" && kind != "Transfer" {
		return "", "", errUnauthenticated
	}
	return kind, token, nil
}

type statusView struct {
	UploadID        string   `json:"upload_id"`
	Status          string   `json:"status"`
	DeclaredBytes   int64    `json:"declared_bytes"`
	ReceivedBytes   int64    `json:"received_bytes"`
	SHA256          string   `json:"sha256,omitempty"`
	DurationSeconds *float64 `json:"duration_seconds,omitempty"`
	Container       string   `json:"container,omitempty"`
	JobID           string   `json:"job_id,omitempty"`
	JobStatus       string   `json:"job_status,omitempty"`
	Publication     string   `json:"publication"`
	LimitsLabel     string   `json:"limits_label"`
}

type transferView struct {
	Scheme    string `json:"scheme"`
	Token     string `json:"token"`
	ExpiresAt string `json:"expires_at"`
	Method    string `json:"method"`
	Path      string `json:"path"`
}

type intentResponse struct {
	UploadID    string        `json:"upload_id"`
	Status      string        `json:"status"`
	LimitsLabel string        `json:"limits_label"`
	Limits      limitsView    `json:"limits"`
	Transfer    *transferView `json:"transfer,omitempty"`
}

type limitsView struct {
	Label              string `json:"label"`
	MaxInputBytes      int64  `json:"max_input_bytes"`
	MaxDurationSeconds int64  `json:"max_duration_seconds"`
}

func (s *Service) limitView() limitsView {
	return limitsView{Label: LimitsLabel, MaxInputBytes: s.limits.MaxInputBytes, MaxDurationSeconds: s.limits.MaxDurationSeconds}
}

func (s *Service) statusView(row uploadRow) statusView {
	view := statusView{
		UploadID:      row.UploadID,
		Status:        row.Status,
		DeclaredBytes: row.DeclaredBytes,
		ReceivedBytes: row.ReceivedBytes,
		SHA256:        row.SHA256,
		Container:     row.Container,
		JobID:         row.JobID,
		JobStatus:     row.JobStatus,
		Publication:   "NOT_PUBLISHED",
		LimitsLabel:   LimitsLabel,
	}
	if row.Duration.Valid {
		seconds := row.Duration.Float64
		view.DurationSeconds = &seconds
	}
	return view
}

func readJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 4096)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		writeError(w, 400, "MALFORMED", "request body is not valid")
		return false
	}
	if dec.More() {
		writeError(w, 400, "MALFORMED", "request body is not valid")
		return false
	}
	return true
}

func writeMapped(w http.ResponseWriter, err error) {
	if errors.Is(err, io.EOF) {
		writeError(w, 400, "MALFORMED", "request body is not valid")
		return
	}
	item := describe(err)
	writeError(w, item.Status, item.Code, item.Message)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]map[string]string{
		"error": {"code": code, "message": message},
	})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		http.Error(w, `{"error":{"code":"STORE","message":"upload request failed"}}`, 500)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}

func sha256Hex(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
