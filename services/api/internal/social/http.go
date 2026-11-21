package social

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

//go:embed discovery-v1.json
var discoveryDoc []byte

// Handler is the versioned discovery API. Cookies are ignored.
func (s *Service) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/discovery", s.discovery)
	mux.HandleFunc("GET /v1/feeds/{kind}", s.feed)
	mux.HandleFunc("GET /v1/feeds/{kind}/{cursor}", s.feed)
	mux.HandleFunc("POST /v1/search", s.search)
	mux.HandleFunc("POST /v1/follows", s.follow)
	mux.HandleFunc("DELETE /v1/follows/{account}", s.unfollow)
	mux.HandleFunc("GET /v1/public-follows/{account}", s.publicFollows)
	mux.HandleFunc("GET /v1/public-records/{account}", s.publicRecords)
	mux.HandleFunc("POST /v1/likes", s.like)
	mux.HandleFunc("GET /v1/likes", s.listLikes)
	mux.HandleFunc("POST /v1/saves", s.save)
	mux.HandleFunc("GET /v1/saves", s.listSaves)
	mux.HandleFunc("POST /v1/not-interested", s.notInterested)
	mux.HandleFunc("POST /v1/comments", s.comment)
	mux.HandleFunc("GET /v1/comments/{content}", s.listComments)
	mux.HandleFunc("GET /v1/comments/{content}/{cursor}", s.listComments)
	mux.HandleFunc("DELETE /v1/comments/{id}", s.deleteComment)
	mux.HandleFunc("GET /v1/activity", s.activity)
	mux.HandleFunc("GET /v1/activity/{cursor}", s.activity)
	mux.HandleFunc("PUT /v1/profile", s.profilePut)
	mux.HandleFunc("GET /v1/profile/{account}", s.profileGet)
	mux.HandleFunc("PUT /v1/preferences", s.putPrefs)
	mux.HandleFunc("POST /v1/catalog", s.putCatalog)
	mux.HandleFunc("POST /v1/catalog/{content}/withdraw", s.withdraw)
	mux.HandleFunc("POST /v1/index/rebuild", s.rebuild)
	mux.HandleFunc("POST /v1/keys", s.key)
	mux.HandleFunc("POST /v1/public-records", s.publicRecord)
	mux.HandleFunc("POST /v1/account/social-deletion", s.deletion)
	mux.HandleFunc("POST /v1/reports", s.report)
	mux.HandleFunc("GET /v1/reports", s.listReports)
	mux.HandleFunc("POST /v1/blocks", s.block)
	mux.HandleFunc("GET /v1/blocks", s.listBlocks)
	mux.HandleFunc("GET /v1/moderation/queue", s.queue)
	mux.HandleFunc("GET /v1/moderation/audit", s.audit)
	mux.HandleFunc("GET /v1/moderation/cases/{id}", s.getCase)
	mux.HandleFunc("POST /v1/moderation/cases/{id}/decision", s.decide)
	mux.HandleFunc("POST /v1/moderation/cases/{id}/appeal", s.appeal)
	mux.HandleFunc("POST /v1/moderation/cases/{id}/escalate", s.escalate)
	mux.HandleFunc("POST /v1/moderation/accounts/{id}/takedown", s.takedown)
	mux.HandleFunc("POST /v1/removal", s.removal)
	mux.HandleFunc("GET /v1/policy", s.policyPage)
	if s.dev {
		mux.HandleFunc("POST /v1/dev/moderator", s.devModerator)
	}
	if s.dev {
		mux.HandleFunc("POST /v1/dev/session", s.devSession)
	}
	return correlate(cors(mux))
}

func (s *Service) discovery(w http.ResponseWriter, r *http.Request) {
	if _, err := s.bearer(r); err != nil {
		writeAPI(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(discoveryDoc)
}

func (s *Service) feed(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	limit, err := pageLimit(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	cached := int64(-1)
	if raw := r.Header.Get("X-SVN-Cached-Generation"); raw != "" {
		n, conv := strconv.ParseInt(raw, 10, 64)
		if conv != nil || n < 0 {
			writeAPI(w, errMalformed)
			return
		}
		cached = n
	}
	writeResult(w, s.Feed(account, r.PathValue("kind"), r.PathValue("cursor"), limit, cached))
}

func (s *Service) search(w http.ResponseWriter, r *http.Request) {
	account, raw, _, _, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		Kind string `json:"kind"`
		Q    string `json:"q"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Search(account, body.Kind, body.Q))
}

func (s *Service) follow(w http.ResponseWriter, r *http.Request) {
	account, raw, key, sum, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		AccountID string `json:"account_id"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Follow(account, body.AccountID, key, sum, false))
}

func (s *Service) unfollow(w http.ResponseWriter, r *http.Request) {
	account, _, key, sum, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Unfollow(account, r.PathValue("account"), key, sum))
}

func (s *Service) like(w http.ResponseWriter, r *http.Request) {
	account, raw, key, sum, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		ContentID string `json:"content_id"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Like(account, body.ContentID, key, sum))
}

func (s *Service) save(w http.ResponseWriter, r *http.Request) {
	account, raw, key, sum, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		ContentID string `json:"content_id"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Save(account, body.ContentID, key, sum))
}

func (s *Service) notInterested(w http.ResponseWriter, r *http.Request) {
	account, raw, key, sum, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		ContentID string `json:"content_id"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.NotInterested(account, body.ContentID, key, sum))
}

func (s *Service) comment(w http.ResponseWriter, r *http.Request) {
	account, raw, key, sum, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		ContentID string `json:"content_id"`
		Text      string `json:"text"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Comment(account, body.ContentID, body.Text, key, sum))
}

func (s *Service) listComments(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	limit, err := pageLimit(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Comments(account, r.PathValue("content"), r.PathValue("cursor"), limit))
}

func (s *Service) deleteComment(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.DeleteComment(account, r.PathValue("id")))
}

func (s *Service) activity(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	limit, err := pageLimit(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Activity(account, r.PathValue("cursor"), limit))
}

func (s *Service) listSaves(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Saves(account))
}

func (s *Service) listLikes(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Likes(account))
}

func (s *Service) publicFollows(w http.ResponseWriter, r *http.Request) {
	if _, err := s.bearer(r); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.PublicFollows(r.PathValue("account")))
}

func (s *Service) publicRecords(w http.ResponseWriter, r *http.Request) {
	if _, err := s.bearer(r); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.PublicRecords(r.PathValue("account")))
}

func (s *Service) profilePut(w http.ResponseWriter, r *http.Request) {
	account, raw, _, _, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		DisplayName string   `json:"display_name"`
		Topics      []string `json:"topics"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.PutProfile(account, body.DisplayName, body.Topics))
}

func (s *Service) profileGet(w http.ResponseWriter, r *http.Request) {
	if _, err := s.bearer(r); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Profile(r.PathValue("account")))
}

func (s *Service) putPrefs(w http.ResponseWriter, r *http.Request) {
	account, raw, _, _, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		Topics         []string `json:"topics"`
		HiddenAccounts []string `json:"hidden_accounts"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.PutPrefs(account, body.Topics, body.HiddenAccounts))
}

func (s *Service) putCatalog(w http.ResponseWriter, r *http.Request) {
	account, raw, _, _, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		ContentID   string   `json:"content_id"`
		Title       string   `json:"title"`
		Topics      []string `json:"topics"`
		PublishedAt int64    `json:"published_at"`
		Available   *bool    `json:"available"`
		Playback    []struct {
			URL string `json:"url"`
		} `json:"playback"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	available := true
	if body.Available != nil {
		available = *body.Available
	}
	play := make([]string, 0, len(body.Playback))
	for _, row := range body.Playback {
		play = append(play, row.URL)
	}
	writeResult(w, s.Upsert(account, item{
		ContentID: body.ContentID, Title: body.Title, Topics: body.Topics,
		PublishedAt: body.PublishedAt, Available: available, Playback: play,
	}))
}

func (s *Service) withdraw(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Withdraw(account, r.PathValue("content")))
}

func (s *Service) rebuild(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.Rebuild(account))
}

func (s *Service) key(w http.ResponseWriter, r *http.Request) {
	account, raw, _, _, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		KeyID  string `json:"key_id"`
		Public string `json:"public"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.RegisterKey(account, body.KeyID, body.Public))
}

func (s *Service) publicRecord(w http.ResponseWriter, r *http.Request) {
	account, raw, _, _, err := s.authed(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	var body struct {
		Canonical string `json:"canonical"`
		Signature string `json:"signature"`
	}
	if err := decodeRaw(raw, &body); err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.AcceptPublic(account, body.Canonical, body.Signature))
}

func (s *Service) deletion(w http.ResponseWriter, r *http.Request) {
	account, err := s.bearer(r)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, s.DeleteAccount(account))
}

func (s *Service) devSession(w http.ResponseWriter, r *http.Request) {
	if r.URL.RawQuery != "" {
		writeAPI(w, errMalformed)
		return
	}
	var body struct {
		AccountID string `json:"account_id"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil || decodeRaw(raw, &body) != nil {
		writeAPI(w, errMalformed)
		return
	}
	token, err := s.Issue(body.AccountID)
	if err != nil {
		writeAPI(w, err)
		return
	}
	writeResult(w, done(201, map[string]any{
		"interface_version": InterfaceVersion,
		"account_id":        body.AccountID,
		"token":             token,
		"chain_event":       false,
	}))
}

func (s *Service) authed(r *http.Request) (account string, raw []byte, key, sum string, err error) {
	account, err = s.bearer(r)
	if err != nil {
		return "", nil, "", "", err
	}
	raw, err = io.ReadAll(io.LimitReader(r.Body, 1<<16))
	if err != nil {
		return "", nil, "", "", errMalformed
	}
	digest := sha256.Sum256(append([]byte(r.Method+"\n"+r.URL.Path+"\n"), raw...))
	return account, raw, r.Header.Get("Idempotency-Key"), hex.EncodeToString(digest[:]), nil
}

func (s *Service) bearer(r *http.Request) (string, error) {
	if r.URL.RawQuery != "" {
		return "", errMalformed
	}
	header := r.Header.Get("Authorization")
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", errAuth
	}
	token := strings.TrimPrefix(header, prefix)
	if token == "" || strings.ContainsAny(token, " \t") {
		return "", errAuth
	}
	return s.account(token)
}

func decodeRaw(raw []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return errMalformed
	}
	var extra any
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return errMalformed
	}
	return nil
}

func pageLimit(r *http.Request) (int, error) {
	raw := r.Header.Get("X-SVN-Page-Limit")
	if raw == "" {
		return 0, nil
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 || n > pageMax {
		return 0, errMalformed
	}
	return n, nil
}

func cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if originAllowed(r.Header.Get("Origin")) {
			w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-Correlation-ID, X-SVN-Page-Limit, X-SVN-Cached-Generation")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func originAllowed(origin string) bool {
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return false
	}
	host := parsed.Hostname()
	return host == "127.0.0.1" || host == "localhost"
}

func writeResult(w http.ResponseWriter, out result) {
	if out.err != nil {
		writeAPI(w, out.err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "private, no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	if out.cache != "" {
		h.Set("X-SVN-Cache-Generation", strconv.FormatInt(out.generation, 10))
		h.Set("X-SVN-Cache", out.cache)
	}
	w.WriteHeader(out.status)
	_, _ = w.Write(out.raw)
}

func writeAPI(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "UNAVAILABLE", "the request did not finish"
	switch {
	case errors.Is(err, errAuth):
		status, code, message = http.StatusUnauthorized, "UNAUTHENTICATED", "authentication is required"
	case errors.Is(err, errForbidden):
		status, code, message = http.StatusForbidden, "FORBIDDEN", "this account cannot do that"
	case errors.Is(err, errMalformed):
		status, code, message = http.StatusBadRequest, "MALFORMED", "the request was rejected"
	case errors.Is(err, errConflict):
		status, code, message = http.StatusConflict, "CONFLICT", "the request does not match the earlier one"
	case errors.Is(err, errRate):
		status, code, message = http.StatusTooManyRequests, "RATE_LIMITED", "the request was refused"
	case errors.Is(err, errRefused):
		status, code, message = http.StatusBadRequest, "COMMENT_REFUSED", "the comment was refused"
	case errors.Is(err, errNotFound):
		status, code, message = http.StatusNotFound, "NOT_FOUND", "that record is not available"
	case errors.Is(err, errBlocked):
		status, code, message = http.StatusForbidden, "BLOCKED", "this interaction is blocked"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]map[string]string{
		"error": {"code": code, "message": message},
	})
}
