package service

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/api/passtest"
)

func testWebAuthn() WebAuthnConfig {
	return WebAuthnConfig{RPID: passtest.RPID, DisplayName: passtest.Display, Origins: []string{passtest.Origin}}
}

var accountSeq atomic.Int64

type acctEnv struct {
	t   *testing.T
	svc *Service
	srv *httptest.Server
	clk *clock
	key ed25519.PrivateKey
}

func startAccounts(t *testing.T, enroll, login int, ttl time.Duration, trusted []ed25519.PublicKey, bootstrap string) *acctEnv {
	t.Helper()
	clk := &clock{at: time.Unix(accountSeq.Add(1)*7200, 0).UTC()}
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	svc, err := New(t.Context(), Options{
		Environment: "production", Limits: wideLimits(1000), QuarantineDir: t.TempDir(),
		Database: testConfig, Now: clk.Now, WebAuthn: testWebAuthn(),
		EnrollPerMinute: enroll, LoginPerMinute: login, SessionTTL: ttl,
		ExportKey: key, TrustedExporters: trusted, BootstrapToken: bootstrap,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Close() })
	srv := httptest.NewServer(svc.Handler())
	t.Cleanup(srv.Close)
	return &acctEnv{t: t, svc: svc, srv: srv, clk: clk, key: key}
}

func (e *acctEnv) invite(role string) string {
	e.t.Helper()
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		e.t.Fatal(err)
	}
	token := hex.EncodeToString(buf)
	sum := sha256.Sum256([]byte(token))
	_, err := testDB.Exec(`
		INSERT INTO api.invites (token_hash, role, expires_at, created_at)
		VALUES ($1, $2, $3, $4)`, hex.EncodeToString(sum[:]), role, e.clk.Now().Add(time.Hour), e.clk.Now())
	if err != nil {
		e.t.Fatal(err)
	}
	return token
}

func (e *acctEnv) enroll(role string) *passtest.Party {
	e.t.Helper()
	p := passtest.NewParty(e.srv.URL)
	if err := p.Enroll(e.invite(role)); err != nil {
		e.t.Fatal(err)
	}
	if p.Role != role || p.AccountID == "" || p.Token == "" {
		e.t.Fatalf("enroll %#v", p)
	}
	return p
}

func (e *acctEnv) code(method, path, token, csrf string, body any) (int, map[string]any) {
	e.t.Helper()
	var raw []byte
	if body != nil {
		raw, _ = json.Marshal(body)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, bytes.NewReader(raw))
	if err != nil {
		e.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if csrf != "" {
		req.Header.Set("X-CSRF-Token", csrf)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		e.t.Fatal(err)
	}
	defer res.Body.Close()
	return res.StatusCode, decode(e.t, res)
}

func TestAccountSessionsAndAuthorization(t *testing.T) {
	e := startAccounts(t, 0, 0, 12*time.Hour, nil, "")
	creator := e.enroll("creator")
	other := e.enroll("creator")
	viewer := e.enroll("viewer")
	operator := e.enroll("operator")

	if !strings.Contains(creator.SetCookie, "HttpOnly") || !strings.Contains(creator.SetCookie, "Secure") || !strings.Contains(creator.SetCookie, "SameSite=Strict") {
		t.Fatalf("cookie %s", creator.SetCookie)
	}
	for _, path := range []string{"/signup", "/v1/recovery", "/v1/custody", "/v1/recovery/finish"} {
		req, err := http.NewRequest(http.MethodPost, e.srv.URL+path, strings.NewReader(`{}`))
		if err != nil {
			t.Fatal(err)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		res.Body.Close()
		if res.StatusCode != http.StatusNotFound {
			t.Fatalf("%s %d", path, res.StatusCode)
		}
	}
	unknown := hex.EncodeToString(bytes.Repeat([]byte{0x11}, 32))
	status, body := e.code(http.MethodPost, "/v1/enroll/begin", "", "", map[string]string{"invite": unknown})
	if status != http.StatusUnauthorized || errorCode(t, body) != "UNAUTHENTICATED" {
		t.Fatalf("uninvited %d %#v", status, body)
	}
	used := e.invite("viewer")
	first := passtest.NewParty(e.srv.URL)
	if err := first.Enroll(used); err != nil {
		t.Fatal(err)
	}
	second := passtest.NewParty(e.srv.URL)
	err := second.Enroll(used)
	if err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatalf("duplicate enrollment: %v", err)
	}

	status, body = e.code(http.MethodPost, "/v1/invites", viewer.Token, "", map[string]string{"role": "operator"})
	if status != http.StatusForbidden || errorCode(t, body) != "ROLE" {
		t.Fatalf("viewer invite %d %#v", status, body)
	}
	status, body = e.code(http.MethodPost, "/v1/invites", creator.Token, "", map[string]string{"role": "operator"})
	if status != http.StatusForbidden || errorCode(t, body) != "ROLE" {
		t.Fatalf("creator invite %d %#v", status, body)
	}
	status, body = e.code(http.MethodPost, "/upload-intents", viewer.Token, "", map[string]any{"idempotency_key": "view-1", "declared_bytes": 10})
	if status != http.StatusForbidden || errorCode(t, body) != "ROLE" {
		t.Fatalf("viewer upload %d %#v", status, body)
	}
	status, body = e.code(http.MethodPut, "/v1/preferences", viewer.Token, "", map[string]string{"role": "operator"})
	if status != http.StatusOK {
		t.Fatalf("preference %d %#v", status, body)
	}
	status, body = e.code(http.MethodGet, "/v1/session", viewer.Token, "", nil)
	if status != http.StatusOK || body["role"] != "viewer" {
		t.Fatalf("role change %#v", body)
	}

	status, body = e.code(http.MethodPost, "/upload-intents", creator.Token, "", map[string]any{"idempotency_key": "own-1", "declared_bytes": 10})
	if status != http.StatusAccepted {
		t.Fatalf("creator intent %d %#v", status, body)
	}
	uploadID := body["upload_id"].(string)
	status, body = e.code(http.MethodGet, "/uploads/"+uploadID, other.Token, "", nil)
	if status != http.StatusNotFound {
		t.Fatalf("cross account %d %#v", status, body)
	}

	cookieReq := func(csrf bool) *http.Request {
		t.Helper()
		req, err := http.NewRequest(http.MethodPut, e.srv.URL+"/v1/preferences", strings.NewReader(`{"theme":"dark"}`))
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Cookie", "svn_session="+creator.Token)
		if csrf {
			req.Header.Set("X-CSRF-Token", creator.CSRF)
		}
		return req
	}
	res, err := http.DefaultClient.Do(cookieReq(false))
	if err != nil {
		t.Fatal(err)
	}
	got := decode(t, res)
	if res.StatusCode != http.StatusForbidden || errorCode(t, got) != "CSRF" {
		t.Fatalf("csrf %d %#v", res.StatusCode, got)
	}
	res, err = http.DefaultClient.Do(cookieReq(true))
	if err != nil {
		t.Fatal(err)
	}
	got = decode(t, res)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("csrf ok %d %#v", res.StatusCode, got)
	}

	old := creator.Token
	status, body = e.code(http.MethodPost, "/v1/sessions/rotate", creator.Token, "", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("rotate %d %#v", status, body)
	}
	creator.Token = body["token"].(string)
	creator.CSRF = body["csrf_token"].(string)
	status, _ = e.code(http.MethodGet, "/v1/session", old, "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("old session %d", status)
	}
	status, body = e.code(http.MethodGet, "/v1/session", creator.Token, "", nil)
	if status != http.StatusOK || body["account_id"] != creator.AccountID {
		t.Fatalf("new session %#v", body)
	}

	stolen := creator.Token
	if err := creator.Login(); err != nil {
		t.Fatal(err)
	}
	if err := creator.Confirm(); err != nil {
		t.Fatal(err)
	}
	status, body = e.code(http.MethodPost, "/v1/sessions/revoke", creator.Token, "", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("revoke %d %#v", status, body)
	}
	for _, token := range []string{stolen, creator.Token} {
		status, _ = e.code(http.MethodGet, "/v1/session", token, "", nil)
		if status != http.StatusUnauthorized {
			t.Fatalf("revoked %d", status)
		}
	}
	if err := creator.Login(); err != nil {
		t.Fatal(err)
	}
	status, _ = e.code(http.MethodPost, "/v1/account/export", creator.Token, "", map[string]any{})
	if status != http.StatusForbidden {
		t.Fatalf("export without confirmation %d", status)
	}
	if err := creator.Confirm(); err != nil {
		t.Fatal(err)
	}
	status, body = e.code(http.MethodPost, "/v1/account/export", creator.Token, "", map[string]any{})
	if status != http.StatusOK || body["kind"] != "svn-account-export" {
		t.Fatalf("export %#v", body)
	}
	doc := body["document"].(string)
	if strings.Contains(doc, "confidential") || strings.Contains(doc, other.AccountID) {
		t.Fatalf("export leaked %s", doc)
	}
	status, body = e.code(http.MethodPost, "/v1/invites", operator.Token, "", map[string]string{"role": "creator"})
	if status != http.StatusCreated || len(body["invite"].(string)) != 64 {
		t.Fatalf("operator invite %#v", body)
	}

	e.clk.Add(13 * time.Hour)
	status, _ = e.code(http.MethodGet, "/v1/session", creator.Token, "", nil)
	if status != http.StatusUnauthorized {
		t.Fatalf("expired session %d", status)
	}
}

func TestAccountRateLimit(t *testing.T) {
	e := startAccounts(t, 2, 2, time.Hour, nil, "")
	bad := hex.EncodeToString(bytes.Repeat([]byte{0x22}, 32))
	for i := 0; i < 2; i++ {
		status, body := e.code(http.MethodPost, "/v1/enroll/begin", "", "", map[string]string{"invite": bad})
		if status != http.StatusUnauthorized {
			t.Fatalf("enroll %d %#v", status, body)
		}
	}
	status, body := e.code(http.MethodPost, "/v1/enroll/begin", "", "", map[string]string{"invite": bad})
	if status != http.StatusTooManyRequests || errorCode(t, body) != "RATE_LIMIT" {
		t.Fatalf("enroll rate %d %#v", status, body)
	}
	for i := 0; i < 2; i++ {
		status, body := e.code(http.MethodPost, "/v1/login/begin", "", "", map[string]any{})
		if status != http.StatusOK {
			t.Fatalf("login %d %#v", status, body)
		}
	}
	status, body = e.code(http.MethodPost, "/v1/login/begin", "", "", map[string]any{})
	if status != http.StatusTooManyRequests {
		t.Fatalf("login rate %d %#v", status, body)
	}
}

func TestAccountDeletionAndBackup(t *testing.T) {
	e := startAccounts(t, 0, 0, time.Hour, nil, "")
	p := e.enroll("creator")
	status, body := e.code(http.MethodPut, "/v1/preferences", p.Token, "", map[string]string{"theme": "quiet"})
	if status != http.StatusOK {
		t.Fatalf("pref %d %#v", status, body)
	}
	status, body = e.code(http.MethodPost, "/v1/saved-videos", p.Token, "", map[string]string{"content_id": "saved-1"})
	if status != http.StatusCreated {
		t.Fatalf("saved %d %#v", status, body)
	}
	if _, err := testDB.Exec(`INSERT INTO api.moderation_notes (note_id, subject_account_id, body) VALUES ($1, $2, 'confidential-case')`, "n-"+p.AccountID, p.AccountID); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(`INSERT INTO api.public_record_refs (author_id, record_id, sequence, digest) VALUES ($1, 'record-1', 0, $2)`, p.AccountID, strings.Repeat("ab", 32)); err != nil {
		t.Fatal(err)
	}
	if err := p.Confirm(); err != nil {
		t.Fatal(err)
	}
	status, body = e.code(http.MethodPost, "/v1/account/backup", p.Token, "", map[string]any{})
	if status != http.StatusOK || body["format"] != "SVN-BACKUP-1" || body["alg"] != "AES-256-GCM" {
		t.Fatalf("backup %#v", body)
	}
	key, err := base64.StdEncoding.DecodeString(body["key"].(string))
	if err != nil {
		t.Fatal(err)
	}
	nonce, _ := base64.StdEncoding.DecodeString(body["nonce"].(string))
	ct, _ := base64.StdEncoding.DecodeString(body["ciphertext"].(string))
	block, err := aes.NewCipher(key)
	if err != nil {
		t.Fatal(err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		t.Fatal(err)
	}
	plain, err := gcm.Open(nil, nonce, ct, []byte("SVN-BACKUP-1"))
	if err != nil {
		t.Fatal(err)
	}
	var bundle map[string]any
	if err := json.Unmarshal(plain, &bundle); err != nil || !strings.Contains(bundle["document"].(string), p.AccountID) {
		t.Fatalf("backup plain %s", plain)
	}
	var found int
	if err := testDB.QueryRow(`SELECT count(*) FROM api.preferences WHERE body::text LIKE '%' || $1 || '%'`, body["key"]).Scan(&found); err != nil || found != 0 {
		t.Fatalf("backup key stored %d %v", found, err)
	}

	status, body = e.code(http.MethodPost, "/v1/account/deletion", p.Token, "", map[string]string{"confirm": "delete"})
	if status != http.StatusOK || body["history_erased"] != false || body["retention_decision_id"] != "DEC-004" {
		t.Fatalf("delete %#v", body)
	}
	if err := p.Login(); err == nil {
		t.Fatal("deleted account logged in")
	}
	var prefs, notes, refs int
	var erased bool
	if err := testDB.QueryRow(`SELECT count(*) FROM api.preferences WHERE account_id = $1`, p.AccountID).Scan(&prefs); err != nil || prefs != 0 {
		t.Fatalf("preferences %d %v", prefs, err)
	}
	if err := testDB.QueryRow(`SELECT count(*) FROM api.moderation_notes WHERE subject_account_id = $1`, p.AccountID).Scan(&notes); err != nil || notes != 1 {
		t.Fatalf("notes %d %v", notes, err)
	}
	if err := testDB.QueryRow(`SELECT count(*) FROM api.public_record_refs WHERE author_id = $1`, p.AccountID).Scan(&refs); err != nil || refs != 1 {
		t.Fatalf("records %d %v", refs, err)
	}
	if err := testDB.QueryRow(`SELECT history_erased FROM api.retention WHERE account_id = $1`, p.AccountID).Scan(&erased); err != nil || erased {
		t.Fatalf("retention %v %v", erased, err)
	}
}

func TestAccountMigration(t *testing.T) {
	e := startAccounts(t, 0, 0, time.Hour, nil, "")
	keeper := e.enroll("creator")
	status, body := e.code(http.MethodPut, "/v1/preferences", keeper.Token, "", map[string]string{"theme": "only-keeper"})
	if status != http.StatusOK {
		t.Fatalf("pref %d %#v", status, body)
	}
	status, _ = e.code(http.MethodPost, "/v1/saved-videos", keeper.Token, "", map[string]string{"content_id": "saved-9"})
	if status != http.StatusCreated {
		t.Fatalf("saved %d", status)
	}
	other := e.enroll("creator")
	status, _ = e.code(http.MethodPut, "/v1/preferences", other.Token, "", map[string]string{"theme": "only-other"})
	if status != http.StatusOK {
		t.Fatalf("other pref %d", status)
	}
	if _, err := testDB.Exec(`INSERT INTO api.moderation_notes (note_id, subject_account_id, body) VALUES ($1, $2, 'confidential-migration')`, "m-"+keeper.AccountID, keeper.AccountID); err != nil {
		t.Fatal(err)
	}
	if err := keeper.Confirm(); err != nil {
		t.Fatal(err)
	}
	status, body = e.code(http.MethodPost, "/v1/account/export", keeper.Token, "", map[string]any{})
	if status != http.StatusOK {
		t.Fatalf("export %d %#v", status, body)
	}
	raw, _ := json.Marshal(body)
	if bytes.Contains(raw, []byte("only-other")) || bytes.Contains(raw, []byte("confidential-migration")) {
		t.Fatalf("export included private data %s", raw)
	}

	if _, err := testDB.Exec(`DROP DATABASE IF EXISTS acct_host_b`); err != nil {
		t.Fatal(err)
	}
	if _, err := testDB.Exec(`CREATE DATABASE acct_host_b`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = testDB.Exec(`DROP DATABASE IF EXISTS acct_host_b`)
	})
	cfg := testConfig
	cfg.Database = "acct_host_b"
	boot := hex.EncodeToString(bytes.Repeat([]byte{0x44}, 32))
	pub := e.key.Public().(ed25519.PublicKey)
	host, err := New(t.Context(), Options{
		Environment: "production", Limits: wideLimits(1000), QuarantineDir: t.TempDir(),
		Database: cfg, WebAuthn: testWebAuthn(), ExportKey: e.key, TrustedExporters: []ed25519.PublicKey{pub},
		BootstrapToken: boot,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = host.Close() })
	srv := httptest.NewServer(host.Handler())
	t.Cleanup(srv.Close)
	op := passtest.NewParty(srv.URL)
	if err := op.Enroll(boot); err != nil {
		t.Fatal(err)
	}
	if op.Role != "operator" {
		t.Fatal(op.Role)
	}
	again := passtest.NewParty(srv.URL)
	if err := again.Enroll(boot); err == nil || !strings.Contains(err.Error(), "409") {
		t.Fatalf("bootstrap reused: %v", err)
	}
	if err := op.Confirm(); err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/account/import", bytes.NewReader(raw))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Authorization", "Bearer "+op.Token)
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	imported := decode(t, res)
	if res.StatusCode != http.StatusCreated || imported["account_id"] != keeper.AccountID {
		t.Fatalf("import %d %#v", res.StatusCode, imported)
	}
	keeper.Base = srv.URL
	if err := keeper.Login(); err != nil {
		t.Fatal(err)
	}
	prefReq, err := http.NewRequest(http.MethodGet, srv.URL+"/v1/preferences", nil)
	if err != nil {
		t.Fatal(err)
	}
	prefReq.Header.Set("Authorization", "Bearer "+keeper.Token)
	prefRes, err := http.DefaultClient.Do(prefReq)
	if err != nil {
		t.Fatal(err)
	}
	view := decode(t, prefRes)
	pref, _ := view["preferences"].(map[string]any)
	if prefRes.StatusCode != http.StatusOK || pref["theme"] != "only-keeper" {
		t.Fatalf("migrated preferences %#v", view)
	}
	flipped := append([]byte(nil), raw...)
	flipped[len(flipped)-4] ^= 0x1
	bad, err := http.NewRequest(http.MethodPost, srv.URL+"/v1/account/import", bytes.NewReader(flipped))
	if err != nil {
		t.Fatal(err)
	}
	bad.Header.Set("Authorization", "Bearer "+op.Token)
	bad.Header.Set("Content-Type", "application/json")
	badRes, err := http.DefaultClient.Do(bad)
	if err != nil {
		t.Fatal(err)
	}
	if badRes.StatusCode == http.StatusCreated {
		t.Fatal("bad signature imported")
	}
	badRes.Body.Close()
}
