package store

// Tests of the reads the API (plan section 5) and the worker (section 8)
// use, and of SQL-path validation under the Q2 and Q3 answers.

import (
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// sqlEnqueueKind calls enqueue_intent on db with kind and auth context.
func sqlEnqueueKind(e *env, db *sql.DB, kind, key, requestJSON, auth string) string {
	e.t.Helper()
	var (
		id      string
		created bool
	)
	if err := db.QueryRowContext(e.ctx, `SELECT intent_id, created FROM broadcaster.enqueue_intent($1, $2::jsonb, $3::jsonb,
		$4, $5::interval)`, kind, requestJSON, auth, key, "1 hour").Scan(&id, &created); err != nil || !created {
		e.t.Fatalf("SQL enqueue %s: %v %v", key, created, err)
	}
	return id
}

// TestListIntentsPages (plan 5.2 GET /v1/intents): a caller pages through
// its own intents in creation order with the last id as token; a token of
// another caller or a malformed one is ErrNotFound; the state filter and the
// page bounds hold.
func TestListIntentsPages(t *testing.T) {
	e := newEnv(t)
	e.principal("a", 16)
	e.principal("b", 16)
	signer := e.signer(1)
	var ids []string
	for _, k := range []string{"k1", "k2", "k3", "k4", "k5"} {
		ids = append(ids, e.enqueue("a", k, signer, time.Hour).IntentID)
	}
	other := e.enqueue("b", "k1", signer, time.Hour)
	var got []string
	after := ""
	for page := 0; page < 4; page++ {
		list, err := e.st.ListIntents(e.ctx, "a", IntentFilter{After: after, Limit: 2})
		if err != nil {
			t.Fatal(err)
		}
		for _, it := range list {
			if it.CallerPrincipal != "a" {
				t.Fatalf("listed another caller's intent %+v", it)
			}
			got = append(got, it.ID)
		}
		if len(list) < 2 {
			break
		}
		after = list[len(list)-1].ID
	}
	if len(got) != 5 {
		t.Fatalf("paged %v, want %v", got, ids)
	}
	for i := range ids {
		if got[i] != ids[i] {
			t.Fatalf("paged %v, want %v in creation order", got, ids)
		}
	}
	for _, token := range []string{other.IntentID, "not-a-uuid", "00000000-0000-0000-0000-000000000009"} {
		if _, err := e.st.ListIntents(e.ctx, "a", IntentFilter{After: token, Limit: 2}); !errors.Is(err, ErrNotFound) {
			t.Errorf("token %q: want ErrNotFound, got %v", token, err)
		}
	}
	if err := e.st.Cancel(e.ctx, "a", ids[2]); err != nil {
		t.Fatal(err)
	}
	failed, err := e.st.ListIntents(e.ctx, "a", IntentFilter{States: []State{StateFailed}, Limit: 100})
	if err != nil || len(failed) != 1 || failed[0].ID != ids[2] || failed[0].Reason != "CANCELLED" {
		t.Fatalf("FAILED filter %+v %v", failed, err)
	}
	open, err := e.st.ListIntents(e.ctx, "a", IntentFilter{States: []State{StateRequested, StateInFlight}, Limit: 100})
	if err != nil || len(open) != 4 {
		t.Fatalf("open filter %d %v", len(open), err)
	}
	for name, f := range map[string]IntentFilter{
		"limit 0":    {Limit: 0},
		"limit 101":  {Limit: 101},
		"bad state":  {States: []State{"PENDING_STALL"}, Limit: 1},
		"bad caller": {Limit: 1},
	} {
		caller := "a"
		if name == "bad caller" {
			caller = "A"
		}
		if _, err := e.st.ListIntents(e.ctx, caller, f); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
}

// TestIntentStatusView (plan 5.8, 9.5): the status view carries the
// digests, the object, the latest attempt's outcome and CheckTx code, the
// live attempt's landing height and, for a COMMITTED intent, the stored
// commitment; never the request or the authorization context.
func TestIntentStatusView(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 16)
	signer := e.signer(1)
	committed := commitOne(t, e, signer, "view-committed")
	it, err := e.st.GetIntent(e.ctx, "p", committed.intentID)
	_, wantDigest := request("view-committed")
	if err != nil || it.State != StateCommitted || it.Commitment == nil || it.LastOutcome != "ACK" || it.LastAck == nil ||
		*it.LastAck != (ChainCode{}) || it.LandingHeight != 0 || it.Object == nil ||
		*it.Object != (ObjectRef{Type: ObjectOffer, ID: "view-committed"}) || string(it.SignerAddress) != string(signer) ||
		string(it.RequestDigest) != string(wantDigest[:]) || it.SQLDigest != nil || it.Entry != "HTTP" || it.ClearedAt != nil {
		t.Fatalf("committed view %+v %v", it, err)
	}
	c := it.Commitment
	if c.ChainID != testChainID || c.BusinessID != it.BusinessID || c.Height != 42 || c.Code != 0 || c.MsgIndex != 0 ||
		c.GasWanted != 2_000_000 || c.GasUsed != 1234 || c.IdempotentReplay || string(c.CommittingSigner) != string(signer) ||
		string(c.TxHash) != string(c.FirstCommitTxHash) || string(c.TxHash) != string(it.CommittedTxHash) ||
		!json.Valid(c.Events) || len(c.BlockHash) != 32 || c.RecordedAt.IsZero() {
		t.Fatalf("commitment %+v", c)
	}

	inflight := e.enqueue("p", "view-inflight", signer, time.Hour)
	ss := e.lock(signer)
	defer ss.Close()
	att := e.persist(ss, inflight.IntentID, 1, 777)
	if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatalf("begin send: %v %v", ok, err)
	}
	if ok, err := ss.RecordSendOutcome(e.ctx, att, SendOutcome{Class: SendMempoolRejected, CountsAsRefusal: true, Code: 5,
		Codespace: "svn", Log: "refused"}); err != nil || !ok {
		t.Fatalf("record rejection: %v %v", ok, err)
	}
	it, err = e.st.GetIntent(e.ctx, "p", inflight.IntentID)
	if err != nil || it.State != StateInFlight || it.LastOutcome != "MEMPOOL_REJECTED" || it.LastAck == nil ||
		*it.LastAck != (ChainCode{Code: 5, Codespace: "svn"}) || it.LandingHeight != 777 || it.Commitment != nil {
		t.Fatalf("in-flight view %+v %v", it, err)
	}
	queued := e.enqueue("p", "view-queued", signer, time.Hour)
	it, err = e.st.GetIntent(e.ctx, "p", queued.IntentID)
	if err != nil || it.LastOutcome != "" || it.LastAck != nil || it.LandingHeight != 0 || len(it.AuthContextDigest) != 32 {
		t.Fatalf("queued view %+v %v", it, err)
	}
}

// TestWorkerReads (plan section 8): GetWork returns the stored request and
// authorization context for validation and signing; LiveAttempt returns the
// key signer's one live attempt with its bytes; AttemptByHash finds any of
// our attempts by hash.
func TestWorkerReads(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 16)
	signer := e.signer(1)
	in := e.enqueue("p", "work", signer, time.Hour)
	w, err := e.st.GetWork(e.ctx, in.IntentID)
	req, digest := request("work")
	if err != nil || string(w.Request) != string(req) || string(w.RequestDigest) != string(digest[:]) || !w.Validated ||
		w.Kind != KindOfferLease || w.BusinessID != in.BusinessID || w.BusinessAttempt != 1 || !w.BeforeExpiry ||
		string(w.SignerAddress) != string(signer) || w.Object == nil || w.Object.ID != "work" || w.Forward != nil ||
		w.CallerPrincipal != "p" || w.State != StateRequested || w.Phase != PhaseQueued || w.RequestJSON != nil ||
		!json.Valid(w.AuthContext) || w.AttemptCount != 0 {
		t.Fatalf("work %+v %v", w, err)
	}
	callerDB := e.caller("p-sql", 8)
	sqlID := sqlEnqueueKind(e, callerDB, KindOfferLease, "sql-work", `{"v":1}`, `{"principal_id":"p1"}`)
	w, err = e.st.GetWork(e.ctx, sqlID)
	if err != nil || w.Validated || w.Request != nil || string(w.RequestJSON) != `{"v": 1}` || w.Object != nil ||
		string(w.AuthContext) != `{"principal_id": "p1"}` || w.Entry != "SQL" {
		t.Fatalf("SQL work %+v %v", w, err)
	}
	if _, err := e.st.GetWork(e.ctx, "00000000-0000-0000-0000-000000000009"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown work: %v", err)
	}
	if _, err := e.st.GetWork(e.ctx, "x"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("malformed id: %v", err)
	}

	if _, err := e.st.LiveAttempt(e.ctx, signer); !errors.Is(err, ErrNotFound) {
		t.Fatalf("no live attempt: want ErrNotFound, got %v", err)
	}
	ss := e.lock(signer)
	defer ss.Close()
	d := draft(in.IntentID, 0, 90)
	att := e.persist(ss, in.IntentID, 0, 90)
	live, err := e.st.LiveAttempt(e.ctx, signer)
	if err != nil || live.ID != att || live.IntentID != in.IntentID || string(live.TxBytes) != string(d.TxBytes) ||
		string(live.TxHash) != string(d.TxHash[:]) || live.AccountNumber != 7 || live.Sequence != 0 || live.TimeoutHeight != 90 ||
		live.Gas != 2_000_000 || live.Phase != AttemptSigned || live.BytesCleared || string(live.SignerAddress) != string(signer) {
		t.Fatalf("live attempt %+v %v", live, err)
	}
	byHash, err := e.st.AttemptByHash(e.ctx, d.TxHash)
	if err != nil || byHash.ID != att || byHash.IntentID != in.IntentID {
		t.Fatalf("attempt by hash %+v %v", byHash, err)
	}
	if _, err := e.st.AttemptByHash(e.ctx, sha256.Sum256([]byte("unknown"))); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown hash: %v", err)
	}
	if _, err := e.st.LiveAttempt(e.ctx, signer[:19]); !errors.Is(err, ErrInvalid) {
		t.Fatalf("short signer: %v", err)
	}
	if _, err := e.st.ExpiredRequested(e.ctx, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("limit 0: %v", err)
	}
	if ids, err := e.st.ExpiredRequested(e.ctx, 10); err != nil || len(ids) != 0 {
		t.Fatalf("nothing expired yet: %v %v", ids, err)
	}
}

// TestMarkValidatedAnswers (Q2, Q3 on the SQL path): validation inserts the
// content owner row of a REGISTER_CONTENT intent and the forwarded_txs row
// of a forward; a held content id, tracked forwarded bytes, a wrong signer
// class and a wrong object are refused with nothing written, so the intent
// stays unvalidated for FailValidation.
func TestMarkValidatedAnswers(t *testing.T) {
	e := newEnv(t)
	callerDB := e.caller("p-sql", 16)
	service := e.signer(1)
	relay := e.keySigner(2, ClassBroadcaster)
	auth := `{"principal_id":"dave","permissions":["PUBLISH_CONTENT"]}`

	reg := sqlEnqueueKind(e, callerDB, KindRegisterContent, "reg-1", `{"content":"s"}`, auth)
	req, digest := request("sql-register")
	content := ObjectRef{Type: ObjectContent, ID: "content-s", Version: 1}
	if err := e.st.MarkValidated(e.ctx, reg, Validation{Request: req, RequestDigest: digest, SignerAddress: relay,
		Object: content}); !errors.Is(err, ErrSignerClass) {
		t.Fatalf("register by the BROADCASTER key: want ErrSignerClass, got %v", err)
	}
	if err := e.st.MarkValidated(e.ctx, reg, Validation{Request: req, RequestDigest: digest, SignerAddress: service,
		Object: ObjectRef{Type: ObjectOffer, ID: "content-s"}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("register with an offer object: want ErrInvalid, got %v", err)
	}
	if err := e.st.MarkValidated(e.ctx, reg, Validation{Request: req, RequestDigest: digest, SignerAddress: service,
		Object: content}); err != nil {
		t.Fatal(err)
	}
	own, err := e.st.ContentOwnerFor(e.ctx, "p-sql", "content-s")
	if err != nil || own.Status != OwnerPending || own.PrincipalID != "dave" || own.IntentID != reg {
		t.Fatalf("owner row after validation %+v %v", own, err)
	}
	dup := sqlEnqueueKind(e, callerDB, KindRegisterContent, "reg-2", `{"content":"s2"}`, auth)
	if err := e.st.MarkValidated(e.ctx, dup, Validation{Request: req, RequestDigest: digest, SignerAddress: service,
		Object: content}); !errors.Is(err, ErrContentUnavailable) {
		t.Fatalf("second register of the id: want ErrContentUnavailable, got %v", err)
	}
	if w, err := e.st.GetWork(e.ctx, dup); err != nil || w.Validated || w.BusinessID != "" {
		t.Fatalf("refused validation wrote: %+v %v", w, err)
	}
	if err := e.st.FailValidation(e.ctx, dup, "UNAUTHORIZED"); err != nil {
		t.Fatal(err)
	}

	acct := providerAccount(3)
	txb, f := forwardTx("sql", 2, 300)
	fwd := sqlEnqueueKind(e, callerDB, KindForwardProvider, "fwd-1", `{"tx":"b64"}`, `{"principal_id":"p1"}`)
	offer := ObjectRef{Type: ObjectOffer, ID: "offer-sql"}
	if err := e.st.MarkValidated(e.ctx, fwd, Validation{Request: txb, RequestDigest: sha256.Sum256(txb), SignerAddress: acct,
		Object: offer}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("forward without its fields: want ErrInvalid, got %v", err)
	}
	if err := e.st.MarkValidated(e.ctx, fwd, Validation{Request: txb, RequestDigest: sha256.Sum256(txb), SignerAddress: acct,
		Object: offer, Forward: &f}); err != nil {
		t.Fatal(err)
	}
	w, err := e.st.GetWork(e.ctx, fwd)
	if err != nil || !w.Validated || w.BusinessID != "pbiz-sql" || w.Forward == nil || w.Forward.Sequence != 2 ||
		string(w.Request) != string(txb) {
		t.Fatalf("validated SQL forward %+v %v", w, err)
	}
	if sg, err := e.st.GetSigner(e.ctx, acct); err != nil || sg.Class != ClassForwardSource {
		t.Fatalf("forward source %+v %v", sg, err)
	}
	fwd2 := sqlEnqueueKind(e, callerDB, KindForwardProvider, "fwd-2", `{"tx":"again"}`, `{"principal_id":"p1"}`)
	if err := e.st.MarkValidated(e.ctx, fwd2, Validation{Request: txb, RequestDigest: sha256.Sum256(txb), SignerAddress: acct,
		Object: offer, Forward: &f}); !errors.Is(err, ErrTxTracked) {
		t.Fatalf("tracked bytes: want ErrTxTracked, got %v", err)
	}
	if w, err := e.st.GetWork(e.ctx, fwd2); err != nil || w.Validated {
		t.Fatalf("refused forward validation wrote: %+v %v", w, err)
	}
}
