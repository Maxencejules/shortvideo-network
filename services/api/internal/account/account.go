// Package account is the application login for invited accounts.
//
// Passkeys are verified by github.com/go-webauthn/webauthn. This package does
// not implement WebAuthn, and it does not invent an authentication scheme.
// Session tokens are random identifiers stored as SHA-256 hashes. They are
// not signatures.
//
// Delegated host-signing custody and account recovery are BLOCKED on DEC-001.
// There is no host-held user signing key, no recovery link, and no recovery
// challenge. An application export key, when one is configured, signs export
// bundles only. It is not a chain key and it is not a hosting-intent key.
package account

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"database/sql"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"
	"github.com/lib/pq"
)

const (
	cookieName     = "svn_session"
	ceremonyHeader = "X-SVN-Ceremony"
	csrfHeader     = "X-CSRF-Token"
	confirmFor     = 5 * time.Minute
	ceremonyFor    = 5 * time.Minute
	inviteFor      = 24 * time.Hour
	exportDomain   = "SVN-ACCOUNT-EXPORT-1"
	backupFormat   = "SVN-BACKUP-1"
	maxBody        = 1 << 16
	maxSaved       = 100
	maxPreference  = 2048
)

var (
	ErrUnauthenticated = errors.New("unauthenticated")
	ErrRole            = errors.New("role")
	ErrConfirmation    = errors.New("confirmation")
	ErrCSRF            = errors.New("csrf")
	ErrRate            = errors.New("rate")
	ErrConflict        = errors.New("conflict")
	ErrMalformed       = errors.New("malformed")
	ErrNotFound        = errors.New("not found")
	ErrUnavailable     = errors.New("unavailable")
)

// Config is the relying party and the application export key.
// BootstrapToken is the one-time invite for the first operator. It is not a
// session and it is ignored once any account exists.
type Config struct {
	RPID             string
	Display          string
	Origins          []string
	InputAllowance   int64
	OutputAllowance  int64
	EnrollPerMinute  int
	LoginPerMinute   int
	SessionTTL       time.Duration
	BootstrapToken   string
	ExportKey        ed25519.PrivateKey
	TrustedExporters []ed25519.PublicKey
	Now              func() time.Time
}

// Session is an application login. It is not a chain signer.
type Session struct {
	AccountID      string
	Role           string
	Hash           string
	CSRF           string
	Expires        time.Time
	ConfirmedUntil time.Time
}

// Fresh reports whether a passkey confirmation is still inside its window.
func (s Session) Fresh(now time.Time) bool {
	return !s.ConfirmedUntil.IsZero() && s.ConfirmedUntil.After(now)
}

// Service is the account store and the WebAuthn relying party.
type Service struct {
	db  *sql.DB
	cfg Config
	web *webauthn.WebAuthn
}

// Open prepares the relying party. It inserts the bootstrap invite only when
// the account table is empty.
func Open(ctx context.Context, db *sql.DB, cfg Config) (*Service, error) {
	if cfg.RPID == "" || cfg.Display == "" || len(cfg.Origins) == 0 {
		return nil, errors.New("webauthn relying party is not configured")
	}
	if strings.Contains(cfg.RPID, "/") || strings.Contains(cfg.RPID, ":") {
		return nil, errors.New("webauthn relying party id is not a host name")
	}
	for _, origin := range cfg.Origins {
		if origin == "" || strings.ContainsAny(origin, " *") {
			return nil, errors.New("webauthn origin is not allowed")
		}
		if !strings.HasPrefix(origin, "https://") && origin != "http://localhost" {
			return nil, errors.New("webauthn origin must be https or http://localhost")
		}
	}
	if cfg.EnrollPerMinute == 0 {
		cfg.EnrollPerMinute = 10
	}
	if cfg.LoginPerMinute == 0 {
		cfg.LoginPerMinute = 10
	}
	if cfg.EnrollPerMinute < 1 || cfg.EnrollPerMinute > 100 || cfg.LoginPerMinute < 1 || cfg.LoginPerMinute > 100 {
		return nil, errors.New("auth rate must be 1..100 per minute")
	}
	if cfg.SessionTTL == 0 {
		cfg.SessionTTL = 12 * time.Hour
	}
	if cfg.SessionTTL < time.Minute || cfg.SessionTTL > 24*time.Hour {
		return nil, errors.New("session lifetime must be 1 minute through 24 hours")
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.BootstrapToken != "" {
		if _, err := decodeToken(cfg.BootstrapToken); err != nil {
			return nil, err
		}
	}
	if cfg.ExportKey != nil && len(cfg.ExportKey) != ed25519.PrivateKeySize {
		return nil, errors.New("application export key is not an ed25519 key")
	}
	web, err := webauthn.New(&webauthn.Config{
		RPID:                  cfg.RPID,
		RPDisplayName:         cfg.Display,
		RPOrigins:             append([]string(nil), cfg.Origins...),
		AttestationPreference: protocol.PreferNoAttestation,
		AuthenticatorSelection: protocol.AuthenticatorSelection{
			ResidentKey:        protocol.ResidentKeyRequirementRequired,
			RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification:   protocol.VerificationRequired,
		},
	})
	if err != nil {
		return nil, err
	}
	svc := &Service{db: db, cfg: cfg, web: web}
	if err := svc.bootstrap(ctx); err != nil {
		return nil, err
	}
	return svc, nil
}

func (s *Service) now() time.Time { return s.cfg.Now().UTC() }

func (s *Service) bootstrap(ctx context.Context) error {
	if s.cfg.BootstrapToken == "" {
		return nil
	}
	var n int
	if err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM api.accounts`).Scan(&n); err != nil {
		return err
	}
	if n != 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api.invites (token_hash, role, expires_at, created_at)
		VALUES ($1, 'operator', $2, $3)
		ON CONFLICT (token_hash) DO NOTHING`,
		hashToken(s.cfg.BootstrapToken), s.now().Add(inviteFor), s.now())
	return err
}

type waUser struct {
	id          []byte
	name        string
	display     string
	credentials []webauthn.Credential
}

func (u *waUser) WebAuthnID() []byte                         { return u.id }
func (u *waUser) WebAuthnName() string                       { return u.name }
func (u *waUser) WebAuthnDisplayName() string                { return u.display }
func (u *waUser) WebAuthnCredentials() []webauthn.Credential { return u.credentials }

func randomHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func decodeToken(token string) ([]byte, error) {
	raw, err := hex.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return nil, errors.New("token must be 32 bytes of hex")
	}
	return raw, nil
}

func validRole(role string) bool {
	return role == "viewer" || role == "creator" || role == "operator"
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil || host == "" {
		if r.RemoteAddr == "" {
			return "unknown"
		}
		return r.RemoteAddr
	}
	return host
}

func (s *Service) hit(ctx context.Context, kind, ip string, limit int) error {
	window := s.now().Truncate(time.Minute)
	var hits int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO api.rate_windows AS w (account_id, window_start, hits)
		VALUES ($1, $2, 1)
		ON CONFLICT (account_id, window_start)
		DO UPDATE SET hits = w.hits + 1
		RETURNING hits`, kind+":"+ip, window).Scan(&hits)
	if err != nil {
		return err
	}
	if hits > limit {
		return ErrRate
	}
	return nil
}

func conflict(err error) error {
	var pqErr *pq.Error
	if errors.As(err, &pqErr) && pqErr.Code == "23505" {
		return ErrConflict
	}
	return err
}

func (s *Service) Authenticate(r *http.Request) (Session, error) {
	header := r.Header.Get("Authorization")
	var token string
	fromCookie := false
	if header != "" {
		kind, rest, ok := strings.Cut(header, " ")
		if !ok || kind != "Bearer" || rest == "" || strings.Contains(rest, " ") || len(rest) > 128 {
			return Session{}, ErrUnauthenticated
		}
		token = rest
	} else {
		cookie, err := r.Cookie(cookieName)
		if err != nil || cookie.Value == "" || len(cookie.Value) > 128 {
			return Session{}, ErrUnauthenticated
		}
		token = cookie.Value
		fromCookie = true
	}
	sess, err := s.lookup(r.Context(), token)
	if err != nil {
		return Session{}, err
	}
	if fromCookie && r.Method != http.MethodGet && r.Method != http.MethodHead {
		got := r.Header.Get(csrfHeader)
		if subtle.ConstantTimeCompare([]byte(got), []byte(sess.CSRF)) != 1 {
			return Session{}, ErrCSRF
		}
	}
	return sess, nil
}

func (s *Service) lookup(ctx context.Context, token string) (Session, error) {
	if _, err := decodeToken(token); err != nil {
		return Session{}, ErrUnauthenticated
	}
	var sess Session
	var confirmed sql.NullTime
	var status string
	err := s.db.QueryRowContext(ctx, `
		SELECT s.token_hash, s.account_id, a.role, a.status, s.csrf_token, s.expires_at, s.confirmed_until
		FROM api.sessions s
		JOIN api.accounts a ON a.account_id = s.account_id
		WHERE s.token_hash = $1 AND s.revoked_at IS NULL`, hashToken(token)).Scan(
		&sess.Hash, &sess.AccountID, &sess.Role, &status, &sess.CSRF, &sess.Expires, &confirmed)
	if errors.Is(err, sql.ErrNoRows) {
		return Session{}, ErrUnauthenticated
	}
	if err != nil {
		return Session{}, err
	}
	if status != "active" || !sess.Expires.After(s.now()) {
		return Session{}, ErrUnauthenticated
	}
	if confirmed.Valid {
		sess.ConfirmedUntil = confirmed.Time
	}
	return sess, nil
}

func (s *Service) beginSession(ctx context.Context, accountID string) (token, csrf string, expires time.Time, err error) {
	token, err = randomHex(32)
	if err != nil {
		return "", "", time.Time{}, err
	}
	csrf, err = randomHex(32)
	if err != nil {
		return "", "", time.Time{}, err
	}
	expires = s.now().Add(s.cfg.SessionTTL)
	_, err = s.db.ExecContext(ctx, `
		INSERT INTO api.sessions (token_hash, account_id, csrf_token, created_at, expires_at)
		VALUES ($1, $2, $3, $4, $5)`, hashToken(token), accountID, csrf, s.now(), expires)
	return token, csrf, expires, err
}
