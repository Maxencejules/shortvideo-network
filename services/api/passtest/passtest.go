// Package passtest drives a passkey ceremony against the account HTTP API.
// The software authenticator is github.com/descope/virtualwebauthn. Production
// code does not import this package.
package passtest

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/descope/virtualwebauthn"
)

const (
	RPID    = "localhost"
	Origin  = "http://localhost"
	Display = "Short Video Network"
)

// Party is one software authenticator and, after enrollment, one session.
type Party struct {
	Base                         string
	RP                           virtualwebauthn.RelyingParty
	Auth                         virtualwebauthn.Authenticator
	Cred                         virtualwebauthn.Credential
	AccountID, Role, Token, CSRF string
	SetCookie                    string
}

// NewParty starts an authenticator that has not enrolled.
func NewParty(base string) *Party {
	return &Party{
		Base: strings.TrimRight(base, "/"),
		RP:   virtualwebauthn.RelyingParty{ID: RPID, Name: Display, Origin: Origin},
		Auth: virtualwebauthn.NewAuthenticator(),
		Cred: virtualwebauthn.NewCredential(virtualwebauthn.KeyTypeEC2),
	}
}

// Enroll spends an invite and keeps the session token.
func (p *Party) Enroll(invite string) error {
	status, raw, header, err := p.post("/v1/enroll/begin", map[string]string{"invite": invite}, false)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("enroll begin %d %s", status, raw)
	}
	var began struct {
		CeremonyID string          `json:"ceremony_id"`
		Options    json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal(raw, &began); err != nil {
		return err
	}
	opts, err := virtualwebauthn.ParseAttestationOptions(string(began.Options))
	if err != nil {
		return err
	}
	p.Auth.Options.UserHandle = []byte(opts.UserID)
	body := virtualwebauthn.CreateAttestationResponse(p.RP, p.Auth, p.Cred, *opts)
	p.Auth.AddCredential(p.Cred)
	return p.finish("/v1/enroll/finish", began.CeremonyID, body, header)
}

// Login creates a new session for the enrolled authenticator.
func (p *Party) Login() error {
	status, raw, _, err := p.post("/v1/login/begin", map[string]string{}, false)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("login begin %d %s", status, raw)
	}
	return p.assert("/v1/login/finish", raw)
}

// Confirm records a fresh passkey confirmation on the current session.
func (p *Party) Confirm() error {
	status, raw, _, err := p.post("/v1/confirm/begin", map[string]string{}, false)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return fmt.Errorf("confirm begin %d %s", status, raw)
	}
	return p.assert("/v1/confirm/finish", raw)
}

func (p *Party) assert(path string, rawBegan []byte) error {
	var began struct {
		CeremonyID string          `json:"ceremony_id"`
		Options    json.RawMessage `json:"options"`
	}
	if err := json.Unmarshal([]byte(rawBegan), &began); err != nil {
		return err
	}
	opts, err := virtualwebauthn.ParseAssertionOptions(string(began.Options))
	if err != nil {
		return err
	}
	p.Cred.Counter++
	body := virtualwebauthn.CreateAssertionResponse(p.RP, p.Auth, p.Cred, *opts)
	return p.finish(path, began.CeremonyID, body, nil)
}

func (p *Party) finish(path, ceremony, body string, _ http.Header) error {
	req, err := http.NewRequest(http.MethodPost, p.Base+path, strings.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-SVN-Ceremony", ceremony)
	if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if res.StatusCode >= 300 {
		return fmt.Errorf("finish %s %d %s", path, res.StatusCode, buf)
	}
	p.SetCookie = res.Header.Get("Set-Cookie")
	if path == "/v1/confirm/finish" {
		return nil
	}
	var view struct {
		AccountID string `json:"account_id"`
		Role      string `json:"role"`
		Token     string `json:"token"`
		CSRF      string `json:"csrf_token"`
	}
	if err := json.Unmarshal(buf, &view); err != nil {
		return err
	}
	p.AccountID, p.Role, p.Token, p.CSRF = view.AccountID, view.Role, view.Token, view.CSRF
	return nil
}

// Do sends a JSON request. useCookie authenticates with the session cookie
// instead of the bearer token.
func (p *Party) Do(method, path string, body any, useCookie bool) (int, []byte, http.Header, error) {
	var raw []byte
	if body != nil {
		var err error
		raw, err = json.Marshal(body)
		if err != nil {
			return 0, nil, nil, err
		}
	}
	req, err := http.NewRequest(method, p.Base+path, bytes.NewReader(raw))
	if err != nil {
		return 0, nil, nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if useCookie {
		req.Header.Set("Cookie", "svn_session="+p.Token)
		if p.CSRF != "" {
			req.Header.Set("X-CSRF-Token", p.CSRF)
		}
	} else if p.Token != "" {
		req.Header.Set("Authorization", "Bearer "+p.Token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, nil, err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, buf, res.Header, nil
}

func (p *Party) post(path string, body any, useCookie bool) (int, []byte, http.Header, error) {
	return p.Do(http.MethodPost, path, body, useCookie)
}
