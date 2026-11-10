package account

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
)

var idRE = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// Register mounts the account routes. There is no public sign-up route and
// no recovery route.
func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /v1/invites", s.createInvite)
	mux.HandleFunc("POST /v1/enroll/begin", s.enrollBegin)
	mux.HandleFunc("POST /v1/enroll/finish", s.enrollFinish)
	mux.HandleFunc("POST /v1/login/begin", s.loginBegin)
	mux.HandleFunc("POST /v1/login/finish", s.loginFinish)
	mux.HandleFunc("POST /v1/confirm/begin", s.confirmBegin)
	mux.HandleFunc("POST /v1/confirm/finish", s.confirmFinish)
	mux.HandleFunc("POST /v1/sessions/rotate", s.rotate)
	mux.HandleFunc("POST /v1/sessions/revoke", s.revoke)
	mux.HandleFunc("GET /v1/session", s.who)
	mux.HandleFunc("PUT /v1/preferences", s.putPreference)
	mux.HandleFunc("GET /v1/preferences", s.getPreference)
	mux.HandleFunc("POST /v1/saved-videos", s.putSaved)
	mux.HandleFunc("POST /v1/account/export", s.export)
	mux.HandleFunc("POST /v1/account/import", s.importBundle)
	mux.HandleFunc("POST /v1/account/backup", s.backup)
	mux.HandleFunc("POST /v1/account/deletion", s.deleteAccount)
}

func (s *Service) createInvite(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Authenticate(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if sess.Role != "operator" {
		s.writeErr(w, ErrRole)
		return
	}
	var body struct {
		Role string `json:"role"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !validRole(body.Role) {
		s.writeErr(w, ErrMalformed)
		return
	}
	token, err := randomHex(32)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	expires := s.now().Add(inviteFor)
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO api.invites (token_hash, role, expires_at, created_at)
		VALUES ($1, $2, $3, $4)`, hashToken(token), body.Role, expires, s.now())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{
		"invite": token, "role": body.Role, "expires_at": expires.Format(time.RFC3339),
	})
}

func (s *Service) enrollBegin(w http.ResponseWriter, r *http.Request) {
	if err := s.hit(r.Context(), "enroll", clientIP(r), s.cfg.EnrollPerMinute); err != nil {
		s.writeErr(w, err)
		return
	}
	var body struct {
		Invite string `json:"invite"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if _, err := decodeToken(body.Invite); err != nil {
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	var role string
	var expires time.Time
	var used sql.NullString
	err := s.db.QueryRowContext(r.Context(), `
		SELECT role, expires_at, used_by FROM api.invites WHERE token_hash = $1`, hashToken(body.Invite)).
		Scan(&role, &expires, &used)
	if errors.Is(err, sql.ErrNoRows) {
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if used.Valid {
		s.writeErr(w, ErrConflict)
		return
	}
	if !expires.After(s.now()) {
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	accountID, err := randomHex(16)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	accountID = "a" + accountID
	handle := make([]byte, 32)
	if _, err := rand.Read(handle); err != nil {
		s.writeErr(w, err)
		return
	}
	user := &waUser{id: handle, name: accountID, display: accountID}
	creation, session, err := s.web.BeginRegistration(user, webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	raw, err := json.Marshal(session)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	id, err := randomHex(16)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO api.ceremonies (id, purpose, account_id, role, webauthn_id, invite_hash, session_data, expires_at)
		VALUES ($1, 'enroll', $2, $3, $4, $5, $6, $7)`,
		id, accountID, role, handle, hashToken(body.Invite), string(raw), s.now().Add(ceremonyFor))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ceremony_id": id, "options": creation})
}

func (s *Service) enrollFinish(w http.ResponseWriter, r *http.Request) {
	row, session, ok := s.ceremony(w, r, "enroll")
	if !ok {
		return
	}
	user := &waUser{id: row.handle, name: row.accountID, display: row.accountID}
	cred, err := s.web.FinishRegistration(user, session, r)
	if err != nil {
		_ = s.dropCeremony(r.Context(), row.id)
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	record, err := json.Marshal(cred)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	err = s.tx(r.Context(), func(tx *sql.Tx) error {
		res, err := tx.ExecContext(r.Context(), `
			UPDATE api.invites SET used_by = $2
			WHERE token_hash = $1 AND used_by IS NULL AND expires_at > $3`,
			row.invite, row.accountID, s.now())
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrConflict
		}
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO api.accounts (account_id, webauthn_id, role, status, created_at)
			VALUES ($1, $2, $3, 'active', $4)`, row.accountID, row.handle, row.role, s.now()); err != nil {
			return conflict(err)
		}
		if _, err := tx.ExecContext(r.Context(), `
			INSERT INTO api.passkeys (credential_id, account_id, record) VALUES ($1, $2, $3)`,
			cred.ID, row.accountID, string(record)); err != nil {
			return conflict(err)
		}
		if row.role == "creator" {
			if _, err := tx.ExecContext(r.Context(), `
				INSERT INTO api.quotas (account_id, input_allowance, output_allowance)
				VALUES ($1, $2, $3) ON CONFLICT (account_id) DO NOTHING`,
				row.accountID, s.cfg.InputAllowance, s.cfg.OutputAllowance); err != nil {
				return err
			}
		}
		_, err = tx.ExecContext(r.Context(), `DELETE FROM api.ceremonies WHERE id = $1`, row.id)
		return err
	})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.writeSession(w, r, row.accountID, row.role, http.StatusCreated)
}

func (s *Service) loginBegin(w http.ResponseWriter, r *http.Request) {
	if err := s.hit(r.Context(), "login", clientIP(r), s.cfg.LoginPerMinute); err != nil {
		s.writeErr(w, err)
		return
	}
	if !readJSON(w, r, &struct{}{}) {
		return
	}
	assertion, session, err := s.web.BeginDiscoverableLogin(webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.storeCeremony(w, r, "login", "", "", nil, "", session, assertion)
}

func (s *Service) loginFinish(w http.ResponseWriter, r *http.Request) {
	row, session, ok := s.ceremony(w, r, "login")
	if !ok {
		return
	}
	cred, err := s.web.FinishDiscoverableLogin(func(rawID, userHandle []byte) (webauthn.User, error) {
		return s.userByHandle(r.Context(), rawID, userHandle)
	}, session, r)
	_ = s.dropCeremony(r.Context(), row.id)
	if err != nil || cred.Authenticator.CloneWarning {
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	accountID, err := s.saveCredential(r.Context(), cred)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	role, status, err := s.accountStatus(r.Context(), accountID)
	if err != nil || status != "active" {
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	s.writeSession(w, r, accountID, role, http.StatusOK)
}

func (s *Service) confirmBegin(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Authenticate(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	user, err := s.userByAccount(r.Context(), sess.AccountID)
	if err != nil {
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	if !readJSON(w, r, &struct{}{}) {
		return
	}
	assertion, ceremony, err := s.web.BeginLogin(user, webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	s.storeCeremony(w, r, "confirm", sess.AccountID, sess.Role, user.id, "", ceremony, assertion)
}

func (s *Service) confirmFinish(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Authenticate(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	row, ceremony, ok := s.ceremony(w, r, "confirm")
	if !ok {
		return
	}
	if row.accountID != sess.AccountID {
		_ = s.dropCeremony(r.Context(), row.id)
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	user, err := s.userByAccount(r.Context(), sess.AccountID)
	if err != nil {
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	cred, err := s.web.FinishLogin(user, ceremony, r)
	_ = s.dropCeremony(r.Context(), row.id)
	if err != nil || cred.Authenticator.CloneWarning {
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	if _, err := s.saveCredential(r.Context(), cred); err != nil {
		s.writeErr(w, err)
		return
	}
	until := s.now().Add(confirmFor)
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE api.sessions SET confirmed_until = $2
		WHERE token_hash = $1 AND revoked_at IS NULL`, sess.Hash, until)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"confirmed_until": until.Format(time.RFC3339)})
}

func (s *Service) rotate(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Authenticate(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	token, csrf, expires, err := s.beginSession(r.Context(), sess.AccountID)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	res, err := s.db.ExecContext(r.Context(), `
		UPDATE api.sessions SET revoked_at = $2
		WHERE token_hash = $1 AND revoked_at IS NULL`, sess.Hash, s.now())
	if err != nil {
		s.writeErr(w, err)
		return
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		_, _ = s.db.ExecContext(r.Context(), `UPDATE api.sessions SET revoked_at = $2 WHERE token_hash = $1`, hashToken(token), s.now())
		s.writeErr(w, ErrUnauthenticated)
		return
	}
	setSessionCookie(w, token, s.cfg.SessionTTL)
	writeJSON(w, http.StatusOK, map[string]string{
		"account_id": sess.AccountID, "role": sess.Role, "token": token, "csrf_token": csrf,
		"expires_at": expires.Format(time.RFC3339),
	})
}

func (s *Service) revoke(w http.ResponseWriter, r *http.Request) {
	sess, err := s.requireFresh(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		UPDATE api.sessions SET revoked_at = $2 WHERE account_id = $1 AND revoked_at IS NULL`,
		sess.AccountID, s.now()); err != nil {
		s.writeErr(w, err)
		return
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]string{"revoked": "all"})
}

func (s *Service) who(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Authenticate(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"account_id": sess.AccountID, "role": sess.Role, "status": "active",
		"expires_at": sess.Expires.Format(time.RFC3339),
	})
}

func (s *Service) putPreference(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Authenticate(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxPreference))
	if err != nil || !json.Valid(body) || len(body) == 0 || body[0] != '{' {
		s.writeErr(w, ErrMalformed)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO api.preferences (account_id, body) VALUES ($1, $2)
		ON CONFLICT (account_id) DO UPDATE SET body = EXCLUDED.body`, sess.AccountID, string(body)); err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"account_id": sess.AccountID})
}

func (s *Service) getPreference(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Authenticate(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	var body []byte
	err = s.db.QueryRowContext(r.Context(), `SELECT body FROM api.preferences WHERE account_id = $1`, sess.AccountID).Scan(&body)
	if errors.Is(err, sql.ErrNoRows) {
		writeJSON(w, http.StatusOK, map[string]any{"account_id": sess.AccountID, "preferences": map[string]any{}})
		return
	}
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"account_id": sess.AccountID, "preferences": json.RawMessage(body)})
}

func (s *Service) putSaved(w http.ResponseWriter, r *http.Request) {
	sess, err := s.Authenticate(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	var body struct {
		ContentID string `json:"content_id"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if !idRE.MatchString(body.ContentID) {
		s.writeErr(w, ErrMalformed)
		return
	}
	var n int
	if err := s.db.QueryRowContext(r.Context(), `SELECT count(*) FROM api.saved_videos WHERE account_id = $1`, sess.AccountID).Scan(&n); err != nil {
		s.writeErr(w, err)
		return
	}
	if n >= maxSaved {
		s.writeErr(w, ErrMalformed)
		return
	}
	if _, err := s.db.ExecContext(r.Context(), `
		INSERT INTO api.saved_videos (account_id, content_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING`, sess.AccountID, body.ContentID); err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"content_id": body.ContentID})
}

func (s *Service) requireFresh(r *http.Request) (Session, error) {
	sess, err := s.Authenticate(r)
	if err != nil {
		return Session{}, err
	}
	if !sess.Fresh(s.now()) {
		return Session{}, ErrConfirmation
	}
	return sess, nil
}

func (s *Service) writeSession(w http.ResponseWriter, r *http.Request, accountID, role string, status int) {
	token, csrf, expires, err := s.beginSession(r.Context(), accountID)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	setSessionCookie(w, token, s.cfg.SessionTTL)
	writeJSON(w, status, map[string]string{
		"account_id": accountID, "role": role, "token": token, "csrf_token": csrf,
		"expires_at": expires.Format(time.RFC3339),
	})
}

type ceremonyRow struct {
	id, purpose, accountID, role, invite string
	handle                               []byte
}

func (s *Service) ceremony(w http.ResponseWriter, r *http.Request, purpose string) (ceremonyRow, webauthn.SessionData, bool) {
	id := r.Header.Get(ceremonyHeader)
	rawID, err := hex.DecodeString(id)
	if err != nil || len(rawID) != 16 {
		s.writeErr(w, ErrUnauthenticated)
		return ceremonyRow{}, webauthn.SessionData{}, false
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxBody)
	var row ceremonyRow
	var raw []byte
	var expires time.Time
	err = s.db.QueryRowContext(r.Context(), `
		SELECT id, purpose, account_id, role, webauthn_id, invite_hash, session_data, expires_at
		FROM api.ceremonies WHERE id = $1`, id).Scan(
		&row.id, &row.purpose, &row.accountID, &row.role, &row.handle, &row.invite, &raw, &expires)
	if err != nil || row.purpose != purpose || !expires.After(s.now()) {
		s.writeErr(w, ErrUnauthenticated)
		return ceremonyRow{}, webauthn.SessionData{}, false
	}
	var session webauthn.SessionData
	if err := json.Unmarshal(raw, &session); err != nil {
		s.writeErr(w, ErrUnauthenticated)
		return ceremonyRow{}, webauthn.SessionData{}, false
	}
	return row, session, true
}

func (s *Service) storeCeremony(w http.ResponseWriter, r *http.Request, purpose, accountID, role string, handle []byte, invite string, session *webauthn.SessionData, options any) {
	raw, err := json.Marshal(session)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	id, err := randomHex(16)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	_, err = s.db.ExecContext(r.Context(), `
		INSERT INTO api.ceremonies (id, purpose, account_id, role, webauthn_id, invite_hash, session_data, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		id, purpose, accountID, role, handle, invite, string(raw), s.now().Add(ceremonyFor))
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ceremony_id": id, "options": options})
}

func (s *Service) dropCeremony(ctx context.Context, id string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM api.ceremonies WHERE id = $1`, id)
	return err
}

func (s *Service) userByHandle(ctx context.Context, rawID, userHandle []byte) (webauthn.User, error) {
	var accountID, status string
	err := s.db.QueryRowContext(ctx, `
		SELECT account_id, status FROM api.accounts WHERE webauthn_id = $1`, userHandle).Scan(&accountID, &status)
	if err != nil || status != "active" {
		return nil, ErrUnauthenticated
	}
	user, err := s.userByAccount(ctx, accountID)
	if err != nil {
		return nil, err
	}
	for _, cred := range user.credentials {
		if string(cred.ID) == string(rawID) {
			return user, nil
		}
	}
	return nil, ErrUnauthenticated
}

func (s *Service) userByAccount(ctx context.Context, accountID string) (*waUser, error) {
	var handle []byte
	var status string
	if err := s.db.QueryRowContext(ctx, `
		SELECT webauthn_id, status FROM api.accounts WHERE account_id = $1`, accountID).Scan(&handle, &status); err != nil {
		return nil, err
	}
	if status != "active" {
		return nil, ErrUnauthenticated
	}
	rows, err := s.db.QueryContext(ctx, `SELECT record FROM api.passkeys WHERE account_id = $1`, accountID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var creds []webauthn.Credential
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var cred webauthn.Credential
		if err := json.Unmarshal(raw, &cred); err != nil {
			return nil, err
		}
		creds = append(creds, cred)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &waUser{id: handle, name: accountID, display: accountID, credentials: creds}, nil
}

func (s *Service) saveCredential(ctx context.Context, cred *webauthn.Credential) (string, error) {
	raw, err := json.Marshal(cred)
	if err != nil {
		return "", err
	}
	var accountID string
	err = s.db.QueryRowContext(ctx, `
		UPDATE api.passkeys SET record = $2 WHERE credential_id = $1 RETURNING account_id`, cred.ID, string(raw)).Scan(&accountID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrUnauthenticated
	}
	return accountID, err
}

func (s *Service) accountStatus(ctx context.Context, accountID string) (role, status string, err error) {
	err = s.db.QueryRowContext(ctx, `SELECT role, status FROM api.accounts WHERE account_id = $1`, accountID).Scan(&role, &status)
	return role, status, err
}

func (s *Service) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func readJSON(w http.ResponseWriter, r *http.Request, dest any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil || dec.More() {
		writeJSON(w, http.StatusBadRequest, map[string]map[string]string{
			"error": {"code": "MALFORMED", "message": "request body is not valid"},
		})
		return false
	}
	return true
}

func (s *Service) writeErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrCSRF):
		writeJSON(w, http.StatusForbidden, errBody("CSRF", "cross-site request was rejected"))
	case errors.Is(err, ErrConfirmation):
		writeJSON(w, http.StatusForbidden, errBody("CONFIRMATION_REQUIRED", "this action needs a fresh passkey confirmation"))
	case errors.Is(err, ErrRole):
		writeJSON(w, http.StatusForbidden, errBody("ROLE", "role cannot perform this action"))
	case errors.Is(err, ErrRate):
		writeJSON(w, http.StatusTooManyRequests, errBody("RATE_LIMIT", "rate limit exceeded"))
	case errors.Is(err, ErrConflict):
		writeJSON(w, http.StatusConflict, errBody("CONFLICT", "the account or invite is already used"))
	case errors.Is(err, ErrMalformed):
		writeJSON(w, http.StatusBadRequest, errBody("MALFORMED", "request body is not valid"))
	case errors.Is(err, ErrUnavailable):
		writeJSON(w, http.StatusServiceUnavailable, errBody("UNAVAILABLE", "the application export key is not configured"))
	case errors.Is(err, ErrUnauthenticated):
		writeJSON(w, http.StatusUnauthorized, errBody("UNAUTHENTICATED", "authentication is required"))
	default:
		writeJSON(w, http.StatusInternalServerError, errBody("STORE", "account request failed"))
	}
}

func errBody(code, message string) map[string]map[string]string {
	return map[string]map[string]string{"error": {"code": code, "message": message}}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	encoded, err := json.Marshal(body)
	if err != nil {
		http.Error(w, `{"error":{"code":"STORE","message":"account request failed"}}`, http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_, _ = w.Write(encoded)
}

func setSessionCookie(w http.ResponseWriter, token string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: token, Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteStrictMode, MaxAge: int(ttl.Seconds()),
	})
}

func clearSessionCookie(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: cookieName, Value: "", Path: "/", HttpOnly: true, Secure: true,
		SameSite: http.SameSiteStrictMode, MaxAge: -1,
	})
}
