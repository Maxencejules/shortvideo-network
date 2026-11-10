package api

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/api/request"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// callerKey carries the authenticated caller in a request context.
type callerKey struct{}

func callerOf(ctx context.Context) policy.Caller {
	c, _ := ctx.Value(callerKey{}).(policy.Caller)
	return c
}

// routes builds the handler: every route needs an authenticated, known
// caller; every request is bounded in number and time.
// The caller is the operator mTLS identity for chain submission. It is not
// an application account session and it is not a viewer passkey.
func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/intents", s.handleCreate)
	mux.HandleFunc("GET /v1/intents", s.handleList)
	mux.HandleFunc("GET /v1/intents/{intent_id}", s.handleGet)
	mux.HandleFunc("POST /v1/intents/{intent_id}/cancel", s.handleCancel)
	mux.HandleFunc("GET /v1/signers", s.handleSigners)
	mux.HandleFunc("GET /v1/health", s.handleHealth)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		s.writeError(w, r, fail(http.StatusNotFound, codeNotFound, nil))
	})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case s.inFlight <- struct{}{}:
			defer func() { <-s.inFlight }()
		default:
			s.writeError(w, r, fail(http.StatusServiceUnavailable, codeOverloaded, nil))
			return
		}
		principal, err := PrincipalOf(r.TLS)
		if err != nil {
			s.writeError(w, r, fail(http.StatusUnauthorized, codeUnauthenticated, err))
			return
		}
		caller, ok := s.callers[principal]
		if !ok {
			s.writeError(w, r, fail(http.StatusForbidden, codeCallerUnknown, nil))
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), requestTimeout)
		defer cancel()
		mux.ServeHTTP(w, r.WithContext(context.WithValue(ctx, callerKey{}, caller)))
	})
}

// readBody reads at most the body cap; a longer body is 413.
func (s *Server) readBody(w http.ResponseWriter, r *http.Request) ([]byte, *apiError) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, int64(s.cfg.MaxBodyBytes)))
	var tooLarge *http.MaxBytesError
	if errors.As(err, &tooLarge) {
		return nil, fail(http.StatusRequestEntityTooLarge, codeBodyTooLarge, nil)
	}
	if err != nil {
		return nil, fail(http.StatusBadRequest, codeMalformed, err)
	}
	return body, nil
}

// handleCreate is POST /v1/intents (plan 5.2, 6.3).
func (s *Server) handleCreate(w http.ResponseWriter, r *http.Request) {
	status, v, apiErr := s.create(w, r)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	s.writeJSON(w, r, status, v)
}

// create runs the enqueue path in this order: body cap, strict envelope,
// routed kind, the caller's kinds, the authorization context, its
// permission (both before any chain read), the lifetime bounds, the typed
// request, the stored bytes and digest, the idempotent replay check, the
// chain-reading checks (authorize), and the one-transaction enqueue.
// Nothing is written before EnqueueHTTP, and EnqueueHTTP writes nothing on
// an error.
func (s *Server) create(w http.ResponseWriter, r *http.Request) (int, intentView, *apiError) {
	ctx := r.Context()
	caller := callerOf(ctx)
	body, apiErr := s.readBody(w, r)
	if apiErr != nil {
		return 0, intentView{}, apiErr
	}
	env, err := request.DecodeEnvelope(body)
	if err != nil {
		return 0, intentView{}, requestError(err)
	}
	spec, err := request.LookupKind(env.Kind)
	if err != nil {
		return 0, intentView{}, requestError(err)
	}
	if err := caller.CheckKind(spec.Kind); err != nil {
		return 0, intentView{}, policyError(err)
	}
	auth, authJSON, err := request.DecodeAuthContext(env.AuthContext, s.cfg.MaxAuthContextBytes)
	if err != nil {
		return 0, intentView{}, requestError(err)
	}
	if err := policy.CheckGrant(spec.Kind, auth); err != nil {
		return 0, intentView{}, policyError(err)
	}
	expiresIn, apiErr := s.lifetime(spec.Kind, env.ExpiresInSeconds)
	if apiErr != nil {
		return 0, intentView{}, apiErr
	}
	dec, err := request.DecodeRequest(spec.Kind, env.Request)
	if err != nil {
		return 0, intentView{}, requestError(err)
	}
	stored, err := dec.Stored()
	if err != nil {
		return 0, intentView{}, fail(http.StatusUnprocessableEntity, codeInvalid, err)
	}
	digest, err := request.Digest(spec.Kind, stored, auth)
	if err != nil {
		return 0, intentView{}, fail(http.StatusInternalServerError, codeInternal, err)
	}
	if status, v, apiErr, done := s.replay(ctx, caller.Principal, env.IdempotencyKey, digest); done {
		return status, v, apiErr
	}
	az, apiErr := s.authorize(ctx, caller, auth, dec)
	if apiErr != nil {
		// A create with the same key that committed after the replay
		// check above can be what authorize refused on (its PENDING owner
		// row): the key then names an intent, and the replay answers.
		if status, v, replayErr, done := s.replay(ctx, caller.Principal, env.IdempotencyKey, digest); done {
			return status, v, replayErr
		}
		return 0, intentView{}, apiErr
	}
	res, err := s.deps.Store.EnqueueHTTP(ctx, store.EnqueueRequest{
		CallerPrincipal: caller.Principal, IdempotencyKey: env.IdempotencyKey, Kind: string(spec.Kind),
		Request: stored, RequestDigest: digest, AuthContext: authJSON, SignerAddress: az.signer,
		Object: az.object, Forward: az.forward, ExpiresIn: expiresIn,
	})
	if err != nil {
		return 0, intentView{}, storeError(err)
	}
	v, apiErr := s.intentView(ctx, caller.Principal, res.IntentID)
	if apiErr != nil {
		return 0, intentView{}, apiErr
	}
	if res.Created {
		return http.StatusCreated, v, nil
	}
	return http.StatusOK, v, nil
}

// lifetime checks expires_in_seconds against the configured lifetime
// bounds, and a RENEW or END relay against the relay local expiry.
func (s *Server) lifetime(kind policy.Kind, seconds int64) (time.Duration, *apiError) {
	if seconds < 1 || seconds > int64(s.cfg.MaxLifetime/time.Second) {
		return 0, fail(http.StatusUnprocessableEntity, codeLifetime, nil)
	}
	d := time.Duration(seconds) * time.Second
	if d < s.cfg.MinLifetime {
		return 0, fail(http.StatusUnprocessableEntity, codeLifetime, nil)
	}
	if (kind == policy.KindRelayRenewLease || kind == policy.KindRelayEndLease) && d > s.cfg.RelayLocalExpiry {
		return 0, fail(http.StatusUnprocessableEntity, codeLifetime, nil)
	}
	return d, nil
}

// replay answers a request whose idempotency key the caller already used
// (plan 5.4): the same digest returns the existing intent with 200, any
// other digest (or a SQL-path intent under the same key) is 409. done is
// false when the key is unused.
func (s *Server) replay(ctx context.Context, caller, key string, digest [32]byte) (int, intentView, *apiError, bool) {
	it, err := s.deps.Store.IntentByIdempotencyKey(ctx, caller, key)
	if errors.Is(err, store.ErrNotFound) {
		return 0, intentView{}, nil, false
	}
	if err != nil {
		return 0, intentView{}, storeError(err), true
	}
	if it.CallerPrincipal != caller || it.IdempotencyKey != key {
		return 0, intentView{}, fail(http.StatusInternalServerError, codeIntegrity, errIntegrity), true
	}
	if it.Entry != "HTTP" || !bytes.Equal(it.RequestDigest, digest[:]) {
		return 0, intentView{}, fail(http.StatusConflict, codeIdempotency, nil), true
	}
	o, apiErr := s.overlay(ctx)
	if apiErr != nil {
		return 0, intentView{}, apiErr, true
	}
	v, err := view(it, o, s.cfg.ChainID, s.deps.Codec.Bech32)
	if err != nil {
		return 0, intentView{}, fail(http.StatusInternalServerError, codeIntegrity, err), true
	}
	return http.StatusOK, v, nil, true
}

// overlay reads the observer record and the open stall episode.
func (s *Server) overlay(ctx context.Context) (overlay, *apiError) {
	var o overlay
	h, err := s.deps.Store.ReadChainHealth(ctx)
	switch {
	case err == nil:
		o.ObserverFresh = h.Age >= 0 && h.Age <= s.cfg.ObserverFreshness
	case !errors.Is(err, store.ErrNotFound):
		return overlay{}, storeError(err)
	}
	ep, err := s.deps.Store.OpenStallEpisode(ctx)
	switch {
	case err == nil:
		o.StallOpen, o.episode = true, &ep
	case !errors.Is(err, store.ErrNotFound):
		return overlay{}, storeError(err)
	}
	return o, nil
}

// intentView reads the caller's intent and its overlay and builds the view.
func (s *Server) intentView(ctx context.Context, caller, intentID string) (intentView, *apiError) {
	it, err := s.deps.Store.GetIntent(ctx, caller, intentID)
	if err != nil {
		return intentView{}, storeError(err)
	}
	o, apiErr := s.overlay(ctx)
	if apiErr != nil {
		return intentView{}, apiErr
	}
	v, err := view(it, o, s.cfg.ChainID, s.deps.Codec.Bech32)
	if err != nil {
		return intentView{}, fail(http.StatusInternalServerError, codeIntegrity, err)
	}
	return v, nil
}

// uuidValid reports whether id is a lowercase canonical UUID.
func uuidValid(id string) bool {
	if len(id) != 36 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case i == 8 || i == 13 || i == 18 || i == 23:
			if c != '-' {
				return false
			}
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f':
		default:
			return false
		}
	}
	return true
}

// handleGet is GET /v1/intents/{intent_id}: the caller's own intent;
// another caller's and an unknown id are both 404.
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("intent_id")
	if !uuidValid(id) {
		s.writeError(w, r, fail(http.StatusNotFound, codeNotFound, nil))
		return
	}
	v, apiErr := s.intentView(r.Context(), callerOf(r.Context()).Principal, id)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	s.writeJSON(w, r, http.StatusOK, v)
}

// handleCancel is POST /v1/intents/{intent_id}/cancel: T3 while REQUESTED
// with no attempt ever persisted; once an attempt exists it is 409 (signed
// bytes cannot be unsent).
func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("intent_id")
	if !uuidValid(id) {
		s.writeError(w, r, fail(http.StatusNotFound, codeNotFound, nil))
		return
	}
	caller := callerOf(r.Context()).Principal
	if body, apiErr := s.readBody(w, r); apiErr != nil || len(bytes.TrimSpace(body)) != 0 {
		if apiErr == nil {
			apiErr = fail(http.StatusBadRequest, codeMalformed, nil)
		}
		s.writeError(w, r, apiErr)
		return
	}
	if err := s.deps.Store.Cancel(r.Context(), caller, id); err != nil {
		s.writeError(w, r, storeError(err))
		return
	}
	v, apiErr := s.intentView(r.Context(), caller, id)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	s.writeJSON(w, r, http.StatusOK, v)
}

// listView is a page of the caller's intents.
type listView struct {
	Intents       []intentView `json:"intents"`
	NextPageToken string       `json:"next_page_token,omitempty"`
}

// statusFilter maps a status filter to the stored states it selects under
// the overlay: PENDING_STALL selects the non-terminal intents while a
// stall is shown, and REQUESTED or SUBMITTED select nothing then, so the
// filter agrees with the status each listed intent shows.
func statusFilter(name string, o overlay) ([]store.State, bool, error) {
	stall := o.StallOpen && o.ObserverFresh
	switch name {
	case "":
		return nil, false, nil
	case outbox.StatusRequested.String():
		return []store.State{store.StateRequested}, stall, nil
	case outbox.StatusSubmitted.String():
		return []store.State{store.StateInFlight}, stall, nil
	case outbox.StatusCommitted.String():
		return []store.State{store.StateCommitted}, false, nil
	case outbox.StatusFailed.String():
		return []store.State{store.StateFailed}, false, nil
	case outbox.StatusPendingStall.String():
		return []store.State{store.StateRequested, store.StateInFlight}, !stall, nil
	}
	return nil, false, errors.New("api: unknown status filter")
}

// handleList is GET /v1/intents?status=&page_token=&limit=.
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	caller := callerOf(ctx).Principal
	q := r.URL.Query()
	for name, vals := range q {
		if (name != "status" && name != "page_token" && name != "limit") || len(vals) != 1 {
			s.writeError(w, r, fail(http.StatusBadRequest, codeMalformed, nil))
			return
		}
	}
	limit := s.cfg.PageSize
	if raw := q.Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > s.cfg.PageSize || strconv.Itoa(n) != raw {
			s.writeError(w, r, fail(http.StatusBadRequest, codeMalformed, nil))
			return
		}
		limit = n
	}
	token := q.Get("page_token")
	if token != "" && !uuidValid(token) {
		s.writeError(w, r, fail(http.StatusBadRequest, codeMalformed, nil))
		return
	}
	o, apiErr := s.overlay(ctx)
	if apiErr != nil {
		s.writeError(w, r, apiErr)
		return
	}
	states, empty, err := statusFilter(q.Get("status"), o)
	if err != nil {
		s.writeError(w, r, fail(http.StatusBadRequest, codeMalformed, err))
		return
	}
	out := listView{Intents: []intentView{}}
	if !empty {
		its, err := s.deps.Store.ListIntents(ctx, caller, store.IntentFilter{States: states, After: token, Limit: limit})
		if errors.Is(err, store.ErrNotFound) {
			s.writeError(w, r, fail(http.StatusBadRequest, codeMalformed, err))
			return
		}
		if err != nil {
			s.writeError(w, r, storeError(err))
			return
		}
		for _, it := range its {
			if it.CallerPrincipal != caller {
				s.writeError(w, r, fail(http.StatusInternalServerError, codeIntegrity, errIntegrity))
				return
			}
			v, err := view(it, o, s.cfg.ChainID, s.deps.Codec.Bech32)
			if err != nil {
				s.writeError(w, r, fail(http.StatusInternalServerError, codeIntegrity, err))
				return
			}
			out.Intents = append(out.Intents, v)
		}
		if len(its) == limit {
			out.NextPageToken = its[len(its)-1].ID
		}
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

// signerView is one public signer key.
type signerView struct {
	Class        string `json:"class"`
	Address      string `json:"address"`
	AddressHex   string `json:"address_hex"`
	PublicKeyHex string `json:"public_key_hex"`
	Algorithm    string `json:"algorithm"`
}

// handleSigners is GET /v1/signers: the public address and key of each
// signer class, so an account host can name submittingHost in a hosting
// intent (chain/x/content/intent.go).
func (s *Server) handleSigners(w http.ResponseWriter, r *http.Request) {
	out := struct {
		ChainID string       `json:"chain_id"`
		Signers []signerView `json:"signers"`
	}{ChainID: s.cfg.ChainID}
	for _, class := range []policy.Class{policy.ClassService, policy.ClassBroadcaster} {
		k := s.signers[class]
		addr, err := s.deps.Codec.Bech32(k.Address)
		if err != nil {
			s.writeError(w, r, fail(http.StatusInternalServerError, codeInternal, err))
			return
		}
		out.Signers = append(out.Signers, signerView{
			Class: string(class), Address: addr, AddressHex: hex.EncodeToString(k.Address),
			PublicKeyHex: hex.EncodeToString(k.PublicKey), Algorithm: k.Algorithm,
		})
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

// healthView is GET /v1/health. It holds no secret.
type healthView struct {
	ChainID      string      `json:"chain_id"`
	LatestHeight uint64      `json:"latest_height,omitempty"`
	LatestTime   *time.Time  `json:"latest_time,omitempty"`
	SeenAt       *time.Time  `json:"seen_at,omitempty"`
	Observer     string      `json:"observer"`
	Stall        healthStall `json:"stall"`
	Database     string      `json:"database"`
}

type healthStall struct {
	Open        bool       `json:"open"`
	Shown       bool       `json:"shown"`
	SinceHeight uint64     `json:"since_height,omitempty"`
	SinceTime   *time.Time `json:"since_time,omitempty"`
	Reason      string     `json:"reason,omitempty"`
}

// handleHealth reports the chain id, the observer's latest record and its
// freshness, the stall state (shown as PENDING_STALL only with a fresh
// observer) and whether the database answered.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	out := healthView{ChainID: s.cfg.ChainID, Observer: outbox.ObserverUnavailable.String(), Database: "REACHABLE"}
	var readErr error
	h, err := s.deps.Store.ReadChainHealth(ctx)
	switch {
	case err == nil:
		out.LatestHeight = h.LatestHeight
		out.LatestTime, out.SeenAt = &h.LatestTime, &h.SeenAt
		if h.Age >= 0 && h.Age <= s.cfg.ObserverFreshness {
			out.Observer = outbox.ObserverFresh.String()
		}
	case !errors.Is(err, store.ErrNotFound):
		readErr = err
	}
	if readErr == nil {
		ep, err := s.deps.Store.OpenStallEpisode(ctx)
		switch {
		case err == nil:
			out.Stall = healthStall{Open: true, Shown: out.Observer == outbox.ObserverFresh.String(),
				SinceHeight: ep.StartedHeight, SinceTime: &ep.StartedAt, Reason: string(ep.Reason)}
		case !errors.Is(err, store.ErrNotFound):
			readErr = err
		}
	}
	if readErr != nil {
		out.Database = "UNREACHABLE"
		s.log.Warn("health read failed", "route", r.Pattern, "err", readErr)
	}
	s.writeJSON(w, r, http.StatusOK, out)
}

// writeJSON writes v with status; the body is never cached or sniffed. A
// failed write is logged: the connection is gone.
func (s *Server) writeJSON(w http.ResponseWriter, r *http.Request, status int, v any) {
	body, err := json.Marshal(v)
	if err != nil {
		s.log.Warn("answer did not encode", "route", r.Pattern, "err", err)
		status = http.StatusInternalServerError
		body = []byte(`{"error":{"status":500,"code":"INTERNAL"}}`)
	}
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	if _, err := w.Write(append(body, '\n')); err != nil {
		s.log.Info("answer write failed", "route", r.Pattern, "err", err)
	}
}
