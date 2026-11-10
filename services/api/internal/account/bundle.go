package account

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/subtle"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

// Export, import, and the encrypted backup are application data.
// DEC-004 does not set a retention period, so deletion records the request
// and does not claim that a historical public record is gone.
// Recovering a lost backup key is BLOCKED on DEC-001. The key is returned
// once and is not stored.

type exportDoc struct {
	SchemaVersion int               `json:"schema_version"`
	AccountID     string            `json:"account_id"`
	Role          string            `json:"role"`
	CreatedAt     string            `json:"created_at"`
	WebAuthnID    string            `json:"webauthn_id"`
	Preferences   json.RawMessage   `json:"preferences"`
	SavedVideos   []string          `json:"saved_videos"`
	Passkeys      []json.RawMessage `json:"passkeys"`
}

type signedBundle struct {
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	Alg           string `json:"alg"`
	Domain        string `json:"domain"`
	Signer        string `json:"signer"`
	Document      string `json:"document"`
	Signature     string `json:"signature"`
}

func (s *Service) export(w http.ResponseWriter, r *http.Request) {
	sess, err := s.requireFresh(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	bundle, err := s.signAccount(r.Context(), sess.AccountID)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, bundle)
}

func (s *Service) backup(w http.ResponseWriter, r *http.Request) {
	sess, err := s.requireFresh(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	bundle, err := s.signAccount(r.Context(), sess.AccountID)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	plain, err := json.Marshal(bundle)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	key, nonce, out, err := seal(plain)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{
		"format": backupFormat, "alg": "AES-256-GCM",
		"nonce":      base64.StdEncoding.EncodeToString(nonce),
		"ciphertext": base64.StdEncoding.EncodeToString(out),
		"key":        base64.StdEncoding.EncodeToString(key),
	})
}

func (s *Service) importBundle(w http.ResponseWriter, r *http.Request) {
	sess, err := s.requireFresh(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if sess.Role != "operator" {
		s.writeErr(w, ErrRole)
		return
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		s.writeErr(w, ErrMalformed)
		return
	}
	var bundle signedBundle
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&bundle); err != nil || dec.More() {
		s.writeErr(w, ErrMalformed)
		return
	}
	doc, err := verifyBundle(bundle, s.cfg.TrustedExporters)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	if err := s.restore(r.Context(), doc); err != nil {
		s.writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"account_id": doc.AccountID, "imported": true})
}

func (s *Service) deleteAccount(w http.ResponseWriter, r *http.Request) {
	sess, err := s.requireFresh(r)
	if err != nil {
		s.writeErr(w, err)
		return
	}
	var body struct {
		Confirm string `json:"confirm"`
	}
	if !readJSON(w, r, &body) {
		return
	}
	if body.Confirm != "delete" {
		s.writeErr(w, ErrMalformed)
		return
	}
	err = s.tx(r.Context(), func(tx *sql.Tx) error {
		res, err := tx.ExecContext(r.Context(), `
			UPDATE api.accounts SET status = 'deleted', deleted_at = $2
			WHERE account_id = $1 AND status = 'active'`, sess.AccountID, s.now())
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return ErrUnauthenticated
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM api.preferences WHERE account_id = $1`, sess.AccountID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), `DELETE FROM api.saved_videos WHERE account_id = $1`, sess.AccountID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(r.Context(), `
			UPDATE api.sessions SET revoked_at = $2 WHERE account_id = $1 AND revoked_at IS NULL`,
			sess.AccountID, s.now()); err != nil {
			return err
		}
		_, err = tx.ExecContext(r.Context(), `
			INSERT INTO api.retention (account_id, deleted_at, decision_id, value_version, history_erased)
			VALUES ($1, $2, 'DEC-004', 0, false)`, sess.AccountID, s.now())
		return err
	})
	if err != nil {
		s.writeErr(w, err)
		return
	}
	clearSessionCookie(w)
	writeJSON(w, http.StatusOK, map[string]any{
		"account_id": sess.AccountID, "status": "deleted",
		"history_erased": false, "retention_decision_id": "DEC-004", "retention_value_version": 0,
	})
}

func (s *Service) signAccount(ctx context.Context, accountID string) (signedBundle, error) {
	if len(s.cfg.ExportKey) != ed25519.PrivateKeySize {
		return signedBundle{}, ErrUnavailable
	}
	var role, status string
	var handle []byte
	var created time.Time
	err := s.db.QueryRowContext(ctx, `
		SELECT role, status, webauthn_id, created_at FROM api.accounts WHERE account_id = $1`, accountID).
		Scan(&role, &status, &handle, &created)
	if err != nil || status != "active" {
		return signedBundle{}, ErrUnauthenticated
	}
	var pref []byte
	err = s.db.QueryRowContext(ctx, `SELECT body FROM api.preferences WHERE account_id = $1`, accountID).Scan(&pref)
	if errors.Is(err, sql.ErrNoRows) {
		pref = []byte(`{}`)
	} else if err != nil {
		return signedBundle{}, err
	}
	rows, err := s.db.QueryContext(ctx, `SELECT content_id FROM api.saved_videos WHERE account_id = $1 ORDER BY content_id`, accountID)
	if err != nil {
		return signedBundle{}, err
	}
	defer rows.Close()
	saved := []string{}
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return signedBundle{}, err
		}
		saved = append(saved, id)
	}
	if err := rows.Err(); err != nil {
		return signedBundle{}, err
	}
	keys, err := s.db.QueryContext(ctx, `SELECT record FROM api.passkeys WHERE account_id = $1`, accountID)
	if err != nil {
		return signedBundle{}, err
	}
	defer keys.Close()
	var passkeys []json.RawMessage
	for keys.Next() {
		var raw []byte
		if err := keys.Scan(&raw); err != nil {
			return signedBundle{}, err
		}
		passkeys = append(passkeys, json.RawMessage(raw))
	}
	if err := keys.Err(); err != nil {
		return signedBundle{}, err
	}
	if passkeys == nil {
		passkeys = []json.RawMessage{}
	}
	doc := exportDoc{
		SchemaVersion: 1, AccountID: accountID, Role: role,
		CreatedAt: created.UTC().Format(time.RFC3339), WebAuthnID: hex.EncodeToString(handle),
		Preferences: pref, SavedVideos: saved, Passkeys: passkeys,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		return signedBundle{}, err
	}
	pub := s.cfg.ExportKey.Public().(ed25519.PublicKey)
	sig := ed25519.Sign(s.cfg.ExportKey, exportMessage(raw))
	return signedBundle{
		SchemaVersion: 1, Kind: "svn-account-export", Alg: "ed25519", Domain: exportDomain,
		Signer: hex.EncodeToString(pub), Document: string(raw), Signature: hex.EncodeToString(sig),
	}, nil
}

func exportMessage(document []byte) []byte {
	msg := make([]byte, 0, len(exportDomain)+1+len(document))
	msg = append(msg, exportDomain...)
	msg = append(msg, 0)
	return append(msg, document...)
}

func verifyBundle(bundle signedBundle, trusted []ed25519.PublicKey) (exportDoc, error) {
	if bundle.SchemaVersion != 1 || bundle.Kind != "svn-account-export" || bundle.Alg != "ed25519" || bundle.Domain != exportDomain {
		return exportDoc{}, ErrMalformed
	}
	pub, err := hex.DecodeString(bundle.Signer)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return exportDoc{}, ErrUnauthenticated
	}
	sig, err := hex.DecodeString(bundle.Signature)
	if err != nil || len(sig) != ed25519.SignatureSize {
		return exportDoc{}, ErrUnauthenticated
	}
	if !trustedKey(pub, trusted) || !ed25519.Verify(pub, exportMessage([]byte(bundle.Document)), sig) {
		return exportDoc{}, ErrUnauthenticated
	}
	dec := json.NewDecoder(bytes.NewReader([]byte(bundle.Document)))
	dec.DisallowUnknownFields()
	var doc exportDoc
	if err := dec.Decode(&doc); err != nil || dec.More() || doc.SchemaVersion != 1 || !validRole(doc.Role) || !idRE.MatchString(doc.AccountID) {
		return exportDoc{}, ErrMalformed
	}
	return doc, nil
}

func trustedKey(pub ed25519.PublicKey, trusted []ed25519.PublicKey) bool {
	for _, item := range trusted {
		if len(item) == len(pub) && subtle.ConstantTimeCompare(pub, item) == 1 {
			return true
		}
	}
	return false
}

func (s *Service) restore(ctx context.Context, doc exportDoc) error {
	handle, err := hex.DecodeString(doc.WebAuthnID)
	if err != nil || len(handle) < 16 || len(handle) > 64 {
		return ErrMalformed
	}
	created, err := time.Parse(time.RFC3339, doc.CreatedAt)
	if err != nil {
		return ErrMalformed
	}
	if len(doc.Preferences) > maxPreference || len(doc.SavedVideos) > maxSaved || len(doc.Passkeys) > 8 {
		return ErrMalformed
	}
	if len(doc.Preferences) > 0 && doc.Preferences[0] != '{' {
		return ErrMalformed
	}
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO api.accounts (account_id, webauthn_id, role, status, created_at)
			VALUES ($1, $2, $3, 'active', $4)`, doc.AccountID, handle, doc.Role, created.UTC()); err != nil {
			return conflict(err)
		}
		if doc.Role == "creator" {
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO api.quotas (account_id, input_allowance, output_allowance)
				VALUES ($1, $2, $3)`, doc.AccountID, s.cfg.InputAllowance, s.cfg.OutputAllowance); err != nil {
				return err
			}
		}
		if len(doc.Preferences) > 0 {
			if _, err := tx.ExecContext(ctx, `INSERT INTO api.preferences (account_id, body) VALUES ($1, $2)`, doc.AccountID, string(doc.Preferences)); err != nil {
				return err
			}
		}
		for _, id := range doc.SavedVideos {
			if !idRE.MatchString(id) {
				return ErrMalformed
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO api.saved_videos (account_id, content_id) VALUES ($1, $2)`, doc.AccountID, id); err != nil {
				return err
			}
		}
		for _, raw := range doc.Passkeys {
			var cred struct {
				ID []byte `json:"id"`
			}
			if err := json.Unmarshal(raw, &cred); err != nil || len(cred.ID) == 0 {
				return ErrMalformed
			}
			if _, err := tx.ExecContext(ctx, `
				INSERT INTO api.passkeys (credential_id, account_id, record) VALUES ($1, $2, $3)`,
				cred.ID, doc.AccountID, string(raw)); err != nil {
				return conflict(err)
			}
		}
		return nil
	})
}

func seal(plain []byte) (key, nonce, out []byte, err error) {
	key = make([]byte, 32)
	if _, err = rand.Read(key); err != nil {
		return nil, nil, nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, nil, nil, err
	}
	return key, nonce, gcm.Seal(nil, nonce, plain, []byte(backupFormat)), nil
}
