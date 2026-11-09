package devnet_test

// The caller side of the broadcaster devnet tests: typed request bodies of
// docs/architecture/broadcaster.md 10.1-10.5, the mTLS calls of the
// service API, the SQL-path enqueue in one transaction with an application
// row, and waits on an intent's status.

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Maxencejules/shortvideo-network/chain/x/content"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// bcAuth is an authorization context (10.5).
type bcAuth struct {
	PrincipalKind string   `json:"principal_kind"`
	PrincipalID   string   `json:"principal_id"`
	Permissions   []string `json:"permissions"`
	PolicyRef     string   `json:"policy_ref"`
}

// auth is a DEV_FIXTURE context of principal with perms.
func auth(principal string, perms ...string) bcAuth {
	return bcAuth{PrincipalKind: "DEV_FIXTURE", PrincipalID: principal, Permissions: perms, PolicyRef: "policy-1"}
}

// bcInnerSig is the JSON of an inner signature: a hosting intent or a
// provider's consent.
type bcInnerSig struct {
	Domain                  string `json:"domain"`
	KeyID                   string `json:"key_id"`
	PublicKeyHex            string `json:"public_key_hex"`
	SignatureHex            string `json:"signature_hex"`
	CanonicalProfileVersion uint32 `json:"canonical_profile_version"`
}

func sigJSON(sig *typesv1.InnerSignature) bcInnerSig {
	return bcInnerSig{Domain: sig.GetDomain().String(), KeyID: sig.GetKeyId(), PublicKeyHex: hex.EncodeToString(sig.GetPublicKey()),
		SignatureHex: hex.EncodeToString(sig.GetSignature()), CanonicalProfileVersion: sig.GetCanonicalProfileVersion()}
}

type bcRegisterBody struct {
	ContentID         string     `json:"content_id"`
	ContentVersion    uint64     `json:"content_version"`
	ManifestDigestHex string     `json:"manifest_digest_hex"`
	PackageBytes      uint64     `json:"package_bytes"`
	HostingIntent     bcInnerSig `json:"hosting_intent"`
}

type bcWithdrawBody struct {
	ContentID       string     `json:"content_id"`
	ContentVersion  uint64     `json:"content_version"`
	ReasonDigestHex string     `json:"reason_digest_hex"`
	HostingIntent   bcInnerSig `json:"hosting_intent"`
}

type bcOfferBody struct {
	OfferID             string `json:"offer_id"`
	ContentID           string `json:"content_id"`
	ContentVersion      uint64 `json:"content_version"`
	ProviderID          string `json:"provider_id"`
	ManifestDigestHex   string `json:"manifest_digest_hex"`
	PackageBytes        uint64 `json:"package_bytes"`
	TermsDigestHex      string `json:"terms_digest_hex"`
	StartUnixSeconds    int64  `json:"start_unix_seconds"`
	DeadlineUnixSeconds int64  `json:"deadline_unix_seconds"`
	ExpiryUnixSeconds   int64  `json:"expiry_unix_seconds"`
}

type bcAcceptBody struct {
	OfferID            string     `json:"offer_id"`
	LeaseID            string     `json:"lease_id"`
	ProviderAcceptance bcInnerSig `json:"provider_acceptance"`
	ProviderID         string     `json:"provider_id,omitempty"`
	ContentID          string     `json:"content_id,omitempty"`
}

type bcRenewBody struct {
	LeaseID             string     `json:"lease_id"`
	TermVersion         uint64     `json:"term_version"`
	DeadlineUnixSeconds int64      `json:"deadline_unix_seconds"`
	ManifestDigestHex   string     `json:"manifest_digest_hex"`
	PackageBytes        uint64     `json:"package_bytes"`
	ProviderSignature   bcInnerSig `json:"provider_signature"`
	ProviderID          string     `json:"provider_id"`
	ContentID           string     `json:"content_id"`
}

type bcEndBody struct {
	LeaseID           string     `json:"lease_id"`
	ProviderSignature bcInnerSig `json:"provider_signature"`
	ProviderID        string     `json:"provider_id"`
	ContentID         string     `json:"content_id"`
}

type bcForwardBody struct {
	TxBase64   string `json:"tx_base64"`
	ProviderID string `json:"provider_id"`
	ContentID  string `json:"content_id,omitempty"`
}

// bcObject is the chain object an intent acts on: its typed event and id
// (and version, for content and renewals). Phase Z counts the committed
// transactions that carry this event.
type bcObject struct {
	Event      string
	ID         string
	Version    uint64
	HasVersion bool
	// Caller is the principal that created the intent.
	Caller string
	// checked is set once checkAccounting verified the terminal intent.
	checked bool
}

func (o bcObject) String() string {
	if o.HasVersion {
		return fmt.Sprintf("%s:%s/%d", o.Event, o.ID, o.Version)
	}
	return o.Event + ":" + o.ID
}

// Typed event names of the objects (chain/msgkit; svn.events.v1).
const (
	evRegistered = "svn.events.v1.ContentRegistered"
	evWithdrawn  = "svn.events.v1.ContentWithdrawn"
	evOffered    = "svn.events.v1.LeaseOffered"
	evAccepted   = "svn.events.v1.LeaseAccepted"
	evRenewed    = "svn.events.v1.LeaseRenewed"
	evEnded      = "svn.events.v1.LeaseEnded"
)

// bcIntentReq is one POST /v1/intents body; object is the test's record
// of what it targets and is not sent.
type bcIntentReq struct {
	Kind             string `json:"kind"`
	IdempotencyKey   string `json:"idempotency_key"`
	ExpiresInSeconds int64  `json:"expires_in_seconds"`
	AuthContext      bcAuth `json:"auth_context"`
	Request          any    `json:"request"`
	object           bcObject
}

// bcLifetime is the expires_in of the run's requests unless a phase sets
// another: long enough that no phase reaches it by accident.
const bcLifetime = int64(900)

// nextKey returns a fresh idempotency key with prefix.
func (n *bnet) nextKey(prefix string) string {
	n.serial++
	return fmt.Sprintf("%s-%d", prefix, n.serial)
}

// intentSig signs a hosting intent's sign bytes with principal's registered
// key, or with a fresh key for a principal the registry does not name.
func (n *bnet) intentSig(principal string, signBytes []byte) bcInnerSig {
	priv, ok := n.intentKeys[principal]
	if !ok {
		var err error
		if _, priv, err = ed25519.GenerateKey(rand.Reader); err != nil {
			n.t.Fatal(err)
		}
	}
	return sigJSON(&typesv1.InnerSignature{
		Domain: typesv1.SignatureDomain_SIGNATURE_DOMAIN_HOSTING_INTENT, PublicKey: []byte(priv.Public().(ed25519.PublicKey)),
		Signature: ed25519.Sign(priv, signBytes), KeyId: "intent-key-" + principal, CanonicalProfileVersion: 1,
	})
}

// registerReq is a REGISTER_CONTENT request for contentID (version 1) of
// size bytes, with principal's hosting intent naming the broadcaster's
// service address as submitting host.
func (n *bnet) registerReq(principal, contentID string, size uint64) bcIntentReq {
	digest := leaseManifest(contentID).GetValue()
	sb, err := content.RegisterIntentSignBytes(n.serviceAddr(), contentID, 1, digest, size, content.DevnetRoleName)
	if err != nil {
		n.t.Fatal(err)
	}
	return bcIntentReq{Kind: "REGISTER_CONTENT", IdempotencyKey: n.nextKey("reg-" + contentID), ExpiresInSeconds: bcLifetime,
		AuthContext: auth(principal, "PUBLISH_CONTENT"),
		Request: bcRegisterBody{ContentID: contentID, ContentVersion: 1, ManifestDigestHex: hex.EncodeToString(digest), PackageBytes: size,
			HostingIntent: n.intentSig(principal, sb)},
		object: bcObject{Event: evRegistered, ID: contentID, Version: 1, HasVersion: true}}
}

// withdrawReq is a WITHDRAW_CONTENT request for contentID version 1.
func (n *bnet) withdrawReq(principal, contentID string) bcIntentReq {
	why := reason("withdraw-" + contentID)
	sb, err := content.WithdrawIntentSignBytes(n.serviceAddr(), contentID, 1, why, content.DevnetRoleName)
	if err != nil {
		n.t.Fatal(err)
	}
	return bcIntentReq{Kind: "WITHDRAW_CONTENT", IdempotencyKey: n.nextKey("wd-" + contentID), ExpiresInSeconds: bcLifetime,
		AuthContext: auth(principal, "WITHDRAW_CONTENT"),
		Request: bcWithdrawBody{ContentID: contentID, ContentVersion: 1, ReasonDigestHex: hex.EncodeToString(why),
			HostingIntent: n.intentSig(principal, sb)},
		object: bcObject{Event: evWithdrawn, ID: contentID, Version: 1, HasVersion: true}}
}

// offerOf is the chain message an OFFER_LEASE request for these terms
// produces with the service key (term version 1); the test uses it to
// build the provider's acceptance and, in phase OC, a competing copy.
func (n *bnet) offerOf(offerID, contentID, providerID string, size uint64, deadline, expiry int64) *leasesv1.MsgOfferLease {
	return n.d.offerMsg("", offerID, contentID, providerID, size, deadline, expiry)
}

// offerReq is an OFFER_LEASE request for offer.
func (n *bnet) offerReq(principal string, offer *leasesv1.MsgOfferLease) bcIntentReq {
	return bcIntentReq{Kind: "OFFER_LEASE", IdempotencyKey: n.nextKey("of-" + offer.GetOfferId()), ExpiresInSeconds: bcLifetime,
		AuthContext: auth(principal, "SCHEDULE_HOSTING"),
		Request: bcOfferBody{OfferID: offer.GetOfferId(), ContentID: offer.GetContentId(), ContentVersion: offer.GetContentVersion(),
			ProviderID: offer.GetProviderId(), ManifestDigestHex: hex.EncodeToString(offer.GetManifestDigest().GetValue()),
			PackageBytes: offer.GetPackageBytes(), TermsDigestHex: hex.EncodeToString(offer.GetTermsDigest().GetValue()),
			StartUnixSeconds: offer.GetStartUnixSeconds(), DeadlineUnixSeconds: offer.GetDeadlineUnixSeconds(),
			ExpiryUnixSeconds: offer.GetExpiryUnixSeconds()},
		object: bcObject{Event: evOffered, ID: offer.GetOfferId()}}
}

// acceptSig is a provider's acceptance of offer as leaseID, signed with key.
func (n *bnet) acceptSig(offer *leasesv1.MsgOfferLease, leaseID string, key ed25519Key) *typesv1.InnerSignature {
	sb, err := leases.AcceptSignBytes(storedOffer(offer), leaseID)
	if err != nil {
		n.t.Fatal(err)
	}
	return signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_ACCEPT, key, sb)
}

// acceptReq is a RELAY_ACCEPT_LEASE request carrying sig.
func (n *bnet) acceptReq(principal string, offer *leasesv1.MsgOfferLease, leaseID string, sig *typesv1.InnerSignature) bcIntentReq {
	return bcIntentReq{Kind: "RELAY_ACCEPT_LEASE", IdempotencyKey: n.nextKey("ac-" + leaseID), ExpiresInSeconds: bcLifetime,
		AuthContext: auth(principal, "RELAY_PROVIDER_SIGNED"),
		Request: bcAcceptBody{OfferID: offer.GetOfferId(), LeaseID: leaseID, ProviderAcceptance: sigJSON(sig),
			ProviderID: offer.GetProviderId(), ContentID: offer.GetContentId()},
		object: bcObject{Event: evAccepted, ID: leaseID}}
}

// renewSig is a provider's renewal of leaseID to termVersion and deadline,
// signed with key.
func (n *bnet) renewSig(leaseID, contentID string, size, termVersion uint64, deadline int64, key ed25519Key) *typesv1.InnerSignature {
	sb, err := leases.RenewSignBytes(leaseID, termVersion, deadline, leaseManifest(contentID), size, "")
	if err != nil {
		n.t.Fatal(err)
	}
	return signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_RENEW, key, sb)
}

// renewReq is a RELAY_RENEW_LEASE request carrying sig.
func (n *bnet) renewReq(principal, leaseID, providerID, contentID string, size, termVersion uint64, deadline int64, sig *typesv1.InnerSignature) bcIntentReq {
	return bcIntentReq{Kind: "RELAY_RENEW_LEASE", IdempotencyKey: n.nextKey("rn-" + leaseID), ExpiresInSeconds: bcLifetime,
		AuthContext: auth(principal, "RELAY_PROVIDER_SIGNED"),
		Request: bcRenewBody{LeaseID: leaseID, TermVersion: termVersion, DeadlineUnixSeconds: deadline,
			ManifestDigestHex: hex.EncodeToString(leaseManifest(contentID).GetValue()), PackageBytes: size,
			ProviderSignature: sigJSON(sig), ProviderID: providerID, ContentID: contentID},
		object: bcObject{Event: evRenewed, ID: leaseID, Version: termVersion, HasVersion: true}}
}

// endSig is a provider's end of leaseID at termVersion, signed with key.
func (n *bnet) endSig(leaseID, providerID string, termVersion uint64, key ed25519Key) *typesv1.InnerSignature {
	sb, err := leases.EndSignBytes(leaseID, providerID, termVersion)
	if err != nil {
		n.t.Fatal(err)
	}
	return signature(typesv1.SignatureDomain_SIGNATURE_DOMAIN_PROVIDER_END, key, sb)
}

// endReq is a RELAY_END_LEASE request carrying sig.
func (n *bnet) endReq(principal, leaseID, providerID, contentID string, sig *typesv1.InnerSignature) bcIntentReq {
	return bcIntentReq{Kind: "RELAY_END_LEASE", IdempotencyKey: n.nextKey("en-" + leaseID), ExpiresInSeconds: bcLifetime,
		AuthContext: auth(principal, "RELAY_PROVIDER_SIGNED"),
		Request:     bcEndBody{LeaseID: leaseID, ProviderSignature: sigJSON(sig), ProviderID: providerID, ContentID: contentID},
		object:      bcObject{Event: evEnded, ID: leaseID}}
}

// forwardReq is a FORWARD_PROVIDER_TX request for a provider-signed
// acceptance of leaseID.
func (n *bnet) forwardReq(principal string, raw []byte, providerID, contentID, leaseID string) bcIntentReq {
	return bcIntentReq{Kind: "FORWARD_PROVIDER_TX", IdempotencyKey: n.nextKey("fw-" + leaseID), ExpiresInSeconds: bcLifetime,
		AuthContext: auth(principal, "RELAY_PROVIDER_SIGNED"),
		Request:     bcForwardBody{TxBase64: base64.StdEncoding.EncodeToString(raw), ProviderID: providerID, ContentID: contentID},
		object:      bcObject{Event: evAccepted, ID: leaseID}}
}

// bcView is the status view of 10.8, as far as the test reads it.
type bcView struct {
	IntentID string `json:"intent_id"`
	Kind     string `json:"kind"`
	Status   string `json:"status"`
	Detail   struct {
		Phase       string `json:"phase"`
		Reason      string `json:"reason"`
		Attempts    int    `json:"attempts"`
		LastOutcome string `json:"last_outcome"`
		LastAck     *struct {
			Code      uint32 `json:"code"`
			Codespace string `json:"codespace"`
		} `json:"last_ack"`
		LastLandingHeight uint64 `json:"last_landing_height"`
		Observer          string `json:"observer"`
		Stall             *struct {
			SinceHeight uint64 `json:"since_height"`
			Reason      string `json:"reason"`
		} `json:"stall"`
	} `json:"detail"`
	Commitment *struct {
		ChainID           string `json:"chain_id"`
		BusinessID        string `json:"business_id"`
		Height            int64  `json:"height"`
		BlockHash         string `json:"block_hash"`
		TxHash            string `json:"tx_hash"`
		FirstCommitTxHash string `json:"first_commit_tx_hash"`
		TxIndex           uint32 `json:"tx_index"`
		MsgIndex          uint32 `json:"msg_index"`
		Code              uint32 `json:"code"`
		IdempotentReplay  bool   `json:"idempotent_replay"`
		CommittingSigner  string `json:"committing_signer"`
		OwnTransaction    bool   `json:"own_transaction"`
	} `json:"commitment"`
	Failure *struct {
		Reason    string `json:"reason"`
		Code      uint32 `json:"code"`
		Codespace string `json:"codespace"`
	} `json:"failure"`
	BusinessID            string    `json:"business_id"`
	AuthorizationsCleared bool      `json:"authorizations_cleared"`
	ExpiresAt             time.Time `json:"expires_at"`
}

func (v bcView) String() string {
	s := fmt.Sprintf("intent=%s kind=%s status=%s phase=%s attempts=%d observer=%s", v.IntentID, v.Kind, v.Status, v.Detail.Phase,
		v.Detail.Attempts, v.Detail.Observer)
	if v.Failure != nil {
		s += fmt.Sprintf(" failure=%s:%s/%d", v.Failure.Reason, v.Failure.Codespace, v.Failure.Code)
	}
	if v.Commitment != nil {
		s += fmt.Sprintf(" committed=%s height=%d own=%t idempotent=%t", v.Commitment.FirstCommitTxHash, v.Commitment.Height,
			v.Commitment.OwnTransaction, v.Commitment.IdempotentReplay)
	}
	return s
}

// failureReason is the FAILED view's reason, or "".
func (v bcView) failureReason() string {
	if v.Failure == nil {
		return ""
	}
	return v.Failure.Reason
}

// bcAPIError is the API's error body.
type bcAPIError struct {
	Error struct {
		Status int    `json:"status"`
		Code   string `json:"code"`
	} `json:"error"`
}

// apiURL is the base URL of the serve process the test talks to.
func (n *bnet) apiURL() string { return "https://" + n.apiAddr }

// call sends one request as caller and decodes a view or an error code.
func (n *bnet) call(client *http.Client, method, url string, body []byte) (int, bcView, string) {
	n.t.Helper()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequest(method, url, reader)
	if err != nil {
		n.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := client.Do(req)
	if err != nil {
		n.t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		n.t.Fatal(err)
	}
	if resp.StatusCode >= 300 {
		var e bcAPIError
		if err := json.Unmarshal(raw, &e); err != nil || e.Error.Code == "" {
			n.t.Fatalf("%s %s: %d %q is not an error body: %v", method, url, resp.StatusCode, raw, err)
		}
		return resp.StatusCode, bcView{}, e.Error.Code
	}
	var v bcView
	if err := json.Unmarshal(raw, &v); err != nil {
		n.t.Fatalf("%s %s: %d %q: %v", method, url, resp.StatusCode, raw, err)
	}
	return resp.StatusCode, v, ""
}

// bcRetries bounds the retries of a concurrent create answered 503
// UNAVAILABLE (rawPost). Two creates of one caller can lose the enqueue
// lock or a chain read to each other; the API's answer is 503, and the
// race phases count those retries. post does not retry: a 503 there fails
// the test. The one-block status lag is not a reason to retry (the read
// floor is chain.Status.AppFloor).
const bcRetries = 10

// post creates an intent as caller and returns the HTTP status, the view
// and the error code. A 503 UNAVAILABLE is returned as-is. Phase RS waits
// until the restarted node is not catching up before it posts, so this
// path is not a mask for that 503.
func (n *bnet) post(caller string, req bcIntentReq) (int, bcView, string) {
	n.t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		n.t.Fatal(err)
	}
	status, v, code := n.call(n.clients[caller], http.MethodPost, n.apiURL()+"/v1/intents", body)
	if status == http.StatusCreated || status == http.StatusOK {
		obj := req.object
		obj.Caller = caller
		n.objects[v.IntentID] = obj
	}
	return status, v, code
}

// create posts req as caller and requires 201.
func (n *bnet) create(caller string, req bcIntentReq) bcView {
	n.t.Helper()
	status, v, code := n.post(caller, req)
	if status != http.StatusCreated {
		n.t.Fatalf("create %s %s as %s: %d %s", req.Kind, req.IdempotencyKey, caller, status, code)
	}
	return v
}

// refused posts req as caller and requires the error status and code and
// that no intent row exists for the key.
func (n *bnet) refused(phase, caller string, req bcIntentReq, wantStatus int, wantCode string) {
	n.t.Helper()
	status, _, code := n.post(caller, req)
	if status != wantStatus || code != wantCode {
		n.t.Fatalf("phase %s: %s as %s: %d %s, want %d %s", phase, req.Kind, caller, status, code, wantStatus, wantCode)
	}
	n.noIntentWithKey(phase, req.IdempotencyKey)
	fmt.Printf("phase=%s refused kind=%s caller=%s http=%d code=%s rows=0\n", phase, req.Kind, caller, status, code)
}

// noIntentWithKey requires that no intent row carries key.
func (n *bnet) noIntentWithKey(phase, key string) {
	n.t.Helper()
	ctx, cancel := bcCtx()
	defer cancel()
	var count int
	if err := n.reader.QueryRowContext(ctx, `SELECT count(*) FROM broadcaster.intents WHERE idempotency_key = $1`, key).Scan(&count); err != nil {
		n.t.Fatal(err)
	}
	if count != 0 {
		n.t.Fatalf("phase %s: a refused request left %d intent rows", phase, count)
	}
}

// get reads an intent as caller.
func (n *bnet) get(caller, id string) (int, bcView, string) {
	n.t.Helper()
	return n.call(n.clients[caller], http.MethodGet, n.apiURL()+"/v1/intents/"+id, nil)
}

// view reads an intent as the caller that created it.
func (n *bnet) view(id string) bcView {
	n.t.Helper()
	caller := n.objects[id].Caller
	if caller == "" {
		caller = callerApp
	}
	status, v, code := n.get(caller, id)
	if status != http.StatusOK {
		n.t.Fatalf("get %s: %d %s", id, status, code)
	}
	return v
}

// waitView polls an intent's view until cond holds.
func (n *bnet) waitView(id, what string, timeout time.Duration, cond func(bcView) bool) bcView {
	n.t.Helper()
	var last bcView
	deadline := time.Now().Add(timeout)
	for {
		last = n.view(id)
		if cond(last) {
			return last
		}
		if n.serve != nil && n.serve.exited() {
			n.t.Fatalf("waiting for %s of %s: serve exited %d: %s", what, id, n.serve.exitCode(), n.serve.logTail(30))
		}
		if time.Now().After(deadline) {
			n.t.Fatalf("timed out after %s waiting for %s; last view %s; row %s; attempts %v", timeout, what, last, n.intentRow(id), n.attempts(id))
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// Status names (the frozen OperationStatus values, DR-11-09).
const (
	stSubmitted = "OPERATION_STATUS_SUBMITTED"
	stCommitted = "OPERATION_STATUS_COMMITTED"
	stFailed    = "OPERATION_STATUS_FAILED"
	stStall     = "OPERATION_STATUS_PENDING_STALL"
)

// waitTerminal waits until an intent shows COMMITTED or FAILED.
func (n *bnet) waitTerminal(id string, timeout time.Duration) bcView {
	n.t.Helper()
	return n.waitView(id, "a terminal status", timeout, func(v bcView) bool { return v.Status == stCommitted || v.Status == stFailed })
}

// committed waits until an intent is COMMITTED and requires its own
// transaction to be the canonical commitment.
func (n *bnet) committed(phase, id string) bcView {
	n.t.Helper()
	v := n.waitTerminal(id, 90*time.Second)
	if v.Status != stCommitted || v.Commitment == nil || !v.Commitment.OwnTransaction {
		n.t.Fatalf("phase %s: %s, want COMMITTED by its own transaction; attempts %v", phase, v, n.attempts(id))
	}
	return v
}

// failed waits until an intent is FAILED with reason.
func (n *bnet) failed(phase, id, reason string) bcView {
	n.t.Helper()
	v := n.waitTerminal(id, 120*time.Second)
	if v.Status != stFailed || v.failureReason() != reason {
		n.t.Fatalf("phase %s: %s, want FAILED(%s); attempts %v", phase, v, reason, n.attempts(id))
	}
	return v
}

// sqlEnqueue writes an application row and calls broadcaster.enqueue_intent
// as the caller role app_fixture_sql in one READ COMMITTED transaction, and
// commits (or rolls back when commit is false). It returns the intent id.
func (n *bnet) sqlEnqueue(uploadID string, req bcIntentReq, commit bool) (string, bool) {
	n.t.Helper()
	requestJSON, err := json.Marshal(req.Request)
	if err != nil {
		n.t.Fatal(err)
	}
	authJSON, err := json.Marshal(req.AuthContext)
	if err != nil {
		n.t.Fatal(err)
	}
	ctx, cancel := bcCtx()
	defer cancel()
	tx, err := n.sqlCaller.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	if err != nil {
		n.t.Fatal(err)
	}
	var id string
	var created bool
	if err := tx.QueryRowContext(ctx, `SELECT intent_id::text, created FROM broadcaster.enqueue_intent($1, $2::jsonb, $3::jsonb, $4, $5::interval)`,
		req.Kind, string(requestJSON), string(authJSON), req.IdempotencyKey, fmt.Sprintf("%d seconds", req.ExpiresInSeconds)).Scan(&id, &created); err != nil {
		_ = tx.Rollback()
		n.t.Fatalf("enqueue_intent %s: %v", req.Kind, err)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO app_fixture.uploads (id, intent_id) VALUES ($1, $2::uuid)`, uploadID, id); err != nil {
		_ = tx.Rollback()
		n.t.Fatalf("application row: %v", err)
	}
	if !commit {
		if err := tx.Rollback(); err != nil {
			n.t.Fatal(err)
		}
		return id, created
	}
	if err := tx.Commit(); err != nil {
		n.t.Fatal(err)
	}
	obj := req.object
	obj.Caller = callerApp
	n.objects[id] = obj
	return id, created
}

// hashOf decodes a hex transaction hash from a view.
func (n *bnet) hashOf(s string) []byte {
	n.t.Helper()
	b, err := hex.DecodeString(strings.ToLower(s))
	if err != nil || len(b) != 32 {
		n.t.Fatalf("hash %q: %v", s, err)
	}
	return b
}
