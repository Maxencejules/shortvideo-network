package store

// Tests of migration 0002_answers and the functions that the owner's answers
// to goal 11 Q1-Q4 added (signer classes, forwarded transactions, content
// owners, the Q4 clearing step), all against the real PostgreSQL of
// harness_test.go.

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
	"time"
)

// registerContent enqueues a REGISTER_CONTENT intent of principal for
// content id, whose authorization context names principal id pid.
func (e *env) registerContent(principal, key, contentID, pid string, signer []byte) (EnqueueResult, error) {
	req, digest := request("register:" + key)
	return e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: principal, IdempotencyKey: key,
		Kind: KindRegisterContent, Request: req, RequestDigest: digest,
		AuthContext:   json.RawMessage(`{"principal_kind":"DEV_FIXTURE","principal_id":"` + pid + `","permissions":["PUBLISH_CONTENT"]}`),
		SignerAddress: signer, Object: ObjectRef{Type: ObjectContent, ID: contentID, Version: 1}, ExpiresIn: time.Hour})
}

// providerAccount is a synthetic provider account address.
func providerAccount(n byte) []byte {
	a := make([]byte, 20)
	a[0], a[19] = 0xcc, n
	return a
}

// forwardTx is a synthetic provider-signed transaction and its forward
// fields: an acceptance of offer "offer-<tag>" at sequence seq.
func forwardTx(tag string, seq, timeout uint64) ([]byte, ForwardFields) {
	return []byte(fmt.Sprintf("synthetic-forward-tx:%s:%d:%d", tag, seq, timeout)), ForwardFields{
		MessageType: MsgAcceptLease, ProviderID: "provider-1", BusinessID: "pbiz-" + tag,
		AccountNumber: 3, Sequence: seq, TimeoutHeight: timeout, Gas: 200_000,
	}
}

// enqueueForward forwards txb with f for provider account acct.
func (e *env) enqueueForward(principal, key string, acct, txb []byte, f ForwardFields, offer string) (EnqueueResult, error) {
	return e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: principal, IdempotencyKey: key, Kind: KindForwardProvider,
		Request: txb, RequestDigest: sha256.Sum256(txb), AuthContext: json.RawMessage(`{"principal_id":"p1"}`),
		SignerAddress: acct, Object: ObjectRef{Type: ObjectOffer, ID: offer}, Forward: &f, ExpiresIn: time.Hour})
}

// commitIntent drives a validated REQUESTED intent of the SERVICE signer to
// COMMITTED (T11) at the signer's next sequence, as the worker would, and
// returns the committed attempt's hash.
func commitIntent(t *testing.T, e *env, signer []byte, intentID string, height uint64) [32]byte {
	t.Helper()
	ss := e.lock(signer)
	defer ss.Close()
	var next int64
	if err := e.super.QueryRowContext(e.ctx, `SELECT next_sequence FROM broadcaster.signers WHERE address = $1`,
		signer).Scan(&next); err != nil {
		t.Fatal(err)
	}
	d := draft(intentID, uint64(next), height+100)
	att := e.persist(ss, intentID, uint64(next), height+100)
	if ok, err := ss.BeginSend(e.ctx, att); err != nil || !ok {
		t.Fatalf("begin send: %v %v", ok, err)
	}
	if ok, err := ss.RecordSendOutcome(e.ctx, att, SendOutcome{Class: SendAck}); err != nil || !ok {
		t.Fatalf("record ack: %v %v", ok, err)
	}
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.ResolveCommitted(e.ctx, att, CommittedResult{TxHash: d.TxHash, Height: height, AccSeqPresent: true,
		GasUsed: 10}); err != nil {
		t.Fatal(err)
	}
	if err := tx.CommitIntent(e.ctx, intentID, Expect{StateInFlight, PhaseAcknowledged}, Commitment{Height: height,
		BlockHash: [32]byte{2}, TxHash: d.TxHash, FirstCommitTxHash: d.TxHash, GasWanted: 2_000_000, GasUsed: 10,
		Events: json.RawMessage(`[{"index":0,"type":"svn.events.v1.Test"}]`), CommittingSigner: signer}, true); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	return d.TxHash
}

// inTx runs steps in an owner-role transaction and returns the first error,
// from a step or from the commit; the transaction is rolled back on a step
// error.
func (e *env) inTx(steps ...func(*sql.Tx) error) error {
	e.t.Helper()
	tx, err := e.owner.BeginTx(e.ctx, nil)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, step := range steps {
		if err := step(tx); err != nil {
			_ = tx.Rollback()
			return err
		}
	}
	return tx.Commit()
}

// exec is an inTx step running one statement.
func exec(ctx context.Context, q string, args ...any) func(*sql.Tx) error {
	return func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, q, args...)
		return err
	}
}

// TestKindsMatchDatabase (Q1, Q2): the Go kind list is the database's, with
// the same signer class and object type, and the table is fixed: the owner
// cannot change it (trigger, SV001) and the runtime role cannot write it.
func TestKindsMatchDatabase(t *testing.T) {
	e := newEnv(t)
	rows, err := e.runtime.QueryContext(e.ctx, `SELECT kind, signer_class, COALESCE(object_type, '') FROM broadcaster.kinds`)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]kindSpec{}
	for rows.Next() {
		var k, c, o string
		if err := rows.Scan(&k, &c, &o); err != nil {
			t.Fatal(err)
		}
		got[k] = kindSpec{SignerClass(c), ObjectType(o)}
	}
	rows.Close()
	if len(got) != len(kindSpecs) {
		t.Fatalf("database kinds %v, Go kinds %v", got, kindSpecs)
	}
	for k, spec := range kindSpecs {
		if got[k] != spec {
			t.Errorf("kind %s: database %+v, Go %+v", k, got[k], spec)
		}
		if c, ok := KindSignerClass(k); !ok || c != spec.class {
			t.Errorf("KindSignerClass(%s) = %s %v", k, c, ok)
		}
	}
	if _, ok := KindSignerClass("FORWARD_AUDIT_BATCH"); ok {
		t.Fatal("an audit-batch kind exists; Q2 keeps audit batches on the auditor's path")
	}
	for _, q := range []string{
		`INSERT INTO broadcaster.kinds VALUES ('FORWARD_AUDIT_BATCH', 'FORWARD_SOURCE', NULL)`,
		`UPDATE broadcaster.kinds SET signer_class = 'BROADCASTER' WHERE kind = 'OFFER_LEASE'`,
		`DELETE FROM broadcaster.kinds WHERE kind = 'OFFER_LEASE'`,
		`TRUNCATE broadcaster.kinds CASCADE`,
	} {
		_, err := e.owner.ExecContext(e.ctx, q)
		wantCode(t, err, "SV001")
		_, err = e.runtime.ExecContext(e.ctx, q)
		wantCode(t, err, "42501")
	}
}

// TestSignerClassBinding (Q1): a key signer is registered with its class,
// key name and lock key and keeps them; the kind fixes the signer class of
// an intent; an attempt's signer is its intent's signer.
func TestSignerClassBinding(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 16)
	service := e.signer(1)
	relay := e.keySigner(2, ClassBroadcaster)
	addr := make([]byte, 20)
	addr[0] = 0xdd
	for name, k := range map[string]SignerKey{
		"forward source class": {Address: addr, Class: ClassForwardSource, KeyName: "x", LockKey: 9},
		"unknown class":        {Address: addr, Class: "AUTHORITY", KeyName: "x", LockKey: 9},
		"bad key name":         {Address: addr, Class: ClassService, KeyName: "Key", LockKey: 9},
		"short address":        {Address: addr[:19], Class: ClassService, KeyName: "x", LockKey: 9},
		"negative lock key":    {Address: addr, Class: ClassService, KeyName: "x", LockKey: -1},
	} {
		if err := e.st.RegisterSigner(e.ctx, k); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: want ErrInvalid, got %v", name, err)
		}
	}
	same := SignerKey{Address: service, Class: ClassService, KeyName: "key-1", LockKey: 1}
	if err := e.st.RegisterSigner(e.ctx, same); err != nil {
		t.Fatalf("re-registering the same key: %v", err)
	}
	for name, k := range map[string]SignerKey{
		"other class":            {Address: service, Class: ClassBroadcaster, KeyName: "key-1", LockKey: 1},
		"other key name":         {Address: service, Class: ClassService, KeyName: "key-9", LockKey: 1},
		"other lock key":         {Address: service, Class: ClassService, KeyName: "key-1", LockKey: 7},
		"key name of another":    {Address: addr, Class: ClassService, KeyName: "key-1", LockKey: 30},
		"lock key of another":    {Address: addr, Class: ClassService, KeyName: "fresh", LockKey: 2},
		"address of a forwarder": {Address: providerAccount(1), Class: ClassService, KeyName: "fresh2", LockKey: 31},
	} {
		if name == "address of a forwarder" {
			mustExec(t, e.runtime, `INSERT INTO broadcaster.signers (address, class) VALUES ($1, 'FORWARD_SOURCE')
				ON CONFLICT DO NOTHING`, providerAccount(1))
		}
		if err := e.st.RegisterSigner(e.ctx, k); !errors.Is(err, ErrSignerConfig) {
			t.Errorf("%s: want ErrSignerConfig, got %v", name, err)
		}
	}
	for _, q := range []string{
		`UPDATE broadcaster.signers SET class = 'BROADCASTER' WHERE address = $1`,
		`UPDATE broadcaster.signers SET key_name = 'renamed' WHERE address = $1`,
		`UPDATE broadcaster.signers SET lock_key = 77 WHERE address = $1`,
		`DELETE FROM broadcaster.signers WHERE address = $1`,
	} {
		_, err := e.owner.ExecContext(e.ctx, q, service)
		wantCode(t, err, "SV001")
	}
	_, err := e.runtime.ExecContext(e.ctx, `UPDATE broadcaster.signers SET halted_reason = 'INTEGRITY',
		halted_at = clock_timestamp() WHERE address = $1`, providerAccount(1))
	wantCode(t, err, "23514")
	for _, q := range []string{
		`INSERT INTO broadcaster.signers (address, class, lock_key) VALUES ('\xee00000000000000000000000000000000000001', 'FORWARD_SOURCE', 5)`,
		`INSERT INTO broadcaster.signers (address, class, key_name) VALUES ('\xee00000000000000000000000000000000000002', 'SERVICE', 'nolock')`,
		`INSERT INTO broadcaster.signers (address, class, account_number, next_sequence) VALUES ('\xee00000000000000000000000000000000000003', 'FORWARD_SOURCE', 1, 1)`,
	} {
		_, err := e.runtime.ExecContext(e.ctx, q)
		wantCode(t, err, "23514")
	}

	req, digest := request("class")
	base := EnqueueRequest{CallerPrincipal: "p", IdempotencyKey: "class", Kind: KindOfferLease, Request: req,
		RequestDigest: digest, AuthContext: []byte(`{}`), SignerAddress: relay, Object: testOffer, ExpiresIn: time.Hour}
	if _, err := e.st.EnqueueHTTP(e.ctx, base); !errors.Is(err, ErrSignerClass) {
		t.Fatalf("OFFER_LEASE by the BROADCASTER key: want ErrSignerClass, got %v", err)
	}
	relayReq := base
	relayReq.Kind, relayReq.SignerAddress = KindRelayEndLease, service
	relayReq.Object = ObjectRef{Type: ObjectLease, ID: "lease-1"}
	if _, err := e.st.EnqueueHTTP(e.ctx, relayReq); !errors.Is(err, ErrSignerClass) {
		t.Fatalf("RELAY_END_LEASE by the SERVICE key: want ErrSignerClass, got %v", err)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.intents`); got != 0 {
		t.Fatalf("%d intents written by refused enqueues", got)
	}
	relayReq.SignerAddress = relay
	relayIntent, err := e.st.EnqueueHTTP(e.ctx, relayReq)
	if err != nil {
		t.Fatalf("RELAY_END_LEASE by the BROADCASTER key: %v", err)
	}
	offer := e.enqueue("p", "offer", service, time.Hour)
	_, err = e.owner.ExecContext(e.ctx, `UPDATE broadcaster.intents SET kind = 'RELAY_ACCEPT_LEASE' WHERE intent_id = $1`,
		offer.IntentID)
	wantCode(t, err, "SV012")

	other, otherHash := txBytes("other-signer", 0, 100)
	_, err = e.runtime.ExecContext(e.ctx, `INSERT INTO broadcaster.attempts (intent_id, n, business_n, signer_address,
			account_number, sequence, timeout_height, gas, tx_bytes, tx_hash, phase, worker_instance)
		VALUES ($1, 1, 1, $2, 7, 0, 100, 1, $3, $4, 'SIGNED', 'direct')`, relayIntent.IntentID, service, other, otherHash[:])
	wantCode(t, err, "SV012")

	if _, err := e.st.LockSigner(e.ctx, providerAccount(1)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("LockSigner of a forward source: want ErrInvalid, got %v", err)
	}
	sg, err := e.st.GetSigner(e.ctx, relay)
	if err != nil || sg.Class != ClassBroadcaster || sg.KeyName != "key-2" || sg.LockKey == nil || *sg.LockKey != 2 ||
		sg.NextSequence == nil || *sg.NextSequence != 0 || sg.AccountNumber == nil || *sg.AccountNumber != 7 {
		t.Fatalf("GetSigner %+v %v", sg, err)
	}
	fwd, err := e.st.GetSigner(e.ctx, providerAccount(1))
	if err != nil || fwd.Class != ClassForwardSource || fwd.KeyName != "" || fwd.LockKey != nil || fwd.NextSequence != nil {
		t.Fatalf("GetSigner of a forward source %+v %v", fwd, err)
	}
	if _, err := e.st.GetSigner(e.ctx, addr); !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetSigner unknown: %v", err)
	}
	keys, err := e.st.KeySigners(e.ctx)
	if err != nil || len(keys) != 2 || keys[0].KeyName != "key-1" || keys[1].KeyName != "key-2" {
		t.Fatalf("KeySigners %+v %v", keys, err)
	}
}

// TestContentOwners (Q3, plan 5.7): a REGISTER_CONTENT enqueue inserts a
// PENDING owner row derived from the intent row; a content id held by any
// non-VOID row is ErrContentUnavailable for everyone else, with nothing
// written; ContentOwnerFor answers the caller's own row only; a failed
// register voids its row and frees the id; EFFECTIVE needs the COMMITTED
// intent and the observed host; the rows' transitions are enforced by the
// database.
func TestContentOwners(t *testing.T) {
	e := newEnv(t)
	e.principal("pa", 8)
	e.principal("pb", 8)
	signer := e.signer(1)
	first, err := e.registerContent("pa", "reg-1", "content-x", "alice", signer)
	if err != nil {
		t.Fatal(err)
	}
	own, err := e.st.ContentOwnerFor(e.ctx, "pa", "content-x")
	if err != nil || own.Status != OwnerPending || own.PrincipalID != "alice" || own.IntentID != first.IntentID ||
		own.ContentVersion != 1 || string(own.SignerAddress) != string(signer) || own.CallerPrincipal != "pa" {
		t.Fatalf("own row %+v %v", own, err)
	}
	if _, err := e.st.ContentOwnerFor(e.ctx, "pb", "content-x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("another caller's row: want ErrNotFound, got %v", err)
	}
	before := e.count(`SELECT count(*) FROM broadcaster.intents`) + e.count(`SELECT count(*) FROM broadcaster.content_owners`)
	for _, c := range []struct{ principal, key string }{{"pb", "reg-2"}, {"pa", "reg-3"}} {
		if _, err := e.registerContent(c.principal, c.key, "content-x", "bob", signer); !errors.Is(err, ErrContentUnavailable) {
			t.Fatalf("%s registers a held id: want ErrContentUnavailable, got %v", c.principal, err)
		}
	}
	if _, err := e.registerContent("pb", "reg-4", "content-y", "", signer); !errors.Is(err, ErrInvalid) {
		t.Fatalf("auth context without principal_id: want ErrInvalid, got %v", err)
	}
	if after := e.count(`SELECT count(*) FROM broadcaster.intents`) + e.count(`SELECT count(*) FROM broadcaster.content_owners`); after != before {
		t.Fatalf("refused registers wrote rows: %d -> %d", before, after)
	}

	if err := e.st.ConfirmContentOwner(e.ctx, first.IntentID, signer); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("confirm before COMMITTED: want ErrStateChanged, got %v", err)
	}
	if err := e.st.Cancel(e.ctx, "pa", first.IntentID); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.ContentOwnerFor(e.ctx, "pa", "content-x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("VOID row answered: %v", err)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.content_owners WHERE intent_id = $1 AND status = 'VOID'
		AND principal_id = 'alice'`, first.IntentID); got != 1 {
		t.Fatal("cancelled register did not void its row")
	}

	second, err := e.registerContent("pb", "reg-5", "content-x", "bob", signer)
	if err != nil {
		t.Fatalf("register after VOID: %v", err)
	}
	commitIntent(t, e, signer, second.IntentID, 40)
	if err := e.st.ConfirmContentOwner(e.ctx, second.IntentID, providerAccount(9)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("confirm with another host: want ErrInvalid, got %v", err)
	}
	if o, _ := e.st.ContentOwnerFor(e.ctx, "pb", "content-x"); o.Status != OwnerPending {
		t.Fatalf("row %s after a refused confirm", o.Status)
	}
	if err := e.st.ConfirmContentOwner(e.ctx, second.IntentID, signer); err != nil {
		t.Fatal(err)
	}
	if o, _ := e.st.ContentOwnerFor(e.ctx, "pb", "content-x"); o.Status != OwnerEffective || !o.StatusAt.After(o.CreatedAt) {
		t.Fatalf("confirmed row %+v", o)
	}
	if err := e.st.ConfirmContentOwner(e.ctx, second.IntentID, signer); !errors.Is(err, ErrStateChanged) {
		t.Fatalf("second confirm: want ErrStateChanged, got %v", err)
	}
	if err := e.st.ConfirmContentOwner(e.ctx, "00000000-0000-0000-0000-000000000001", signer); !errors.Is(err, ErrNotFound) {
		t.Fatalf("confirm without a row: want ErrNotFound, got %v", err)
	}

	// The database's own rules, as the owner role.
	for _, q := range []string{
		`UPDATE broadcaster.content_owners SET content_id = 'content-z' WHERE intent_id = $1`,
		`UPDATE broadcaster.content_owners SET status = 'VOID', status_at = clock_timestamp() WHERE intent_id = $1`,
		`UPDATE broadcaster.content_owners SET principal_id = 'mallory' WHERE intent_id = $1`,
		`DELETE FROM broadcaster.content_owners WHERE intent_id = $1`,
	} {
		_, err := e.owner.ExecContext(e.ctx, q, second.IntentID)
		wantCode(t, err, "SV001")
	}
	_, err = e.owner.ExecContext(e.ctx, `TRUNCATE broadcaster.content_owners`)
	wantCode(t, err, "SV001")
	offer := e.enqueue("pa", "offer-1", signer, time.Hour)
	third, err := e.registerContent("pa", "reg-6", "content-w", "alice", signer)
	if err != nil {
		t.Fatal(err)
	}
	insertOwner := `INSERT INTO broadcaster.content_owners (content_id, content_version, caller_principal, principal_id,
		signer_address, intent_id, status) VALUES ($1, 1, $2, $3, $4, $5, $6)`
	for name, args := range map[string][]any{
		"EFFECTIVE at insert":   {"content-q", "pb", "bob", signer, second.IntentID, "EFFECTIVE"},
		"not a register intent": {"test-offer", "pa", "p1", signer, offer.IntentID, "PENDING"},
		"another principal":     {"content-w", "pa", "mallory", signer, third.IntentID, "PENDING"},
		"another caller":        {"content-w", "pb", "alice", signer, third.IntentID, "PENDING"},
		"another content":       {"content-v", "pa", "alice", signer, third.IntentID, "PENDING"},
	} {
		_, err := e.owner.ExecContext(e.ctx, insertOwner, args...)
		if SQLState(err) != "SV014" {
			t.Errorf("%s: want SV014, got %v", name, err)
		}
	}
	err = e.inTx(exec(e.ctx, `UPDATE broadcaster.content_owners SET status = 'EFFECTIVE', status_at = clock_timestamp()
		WHERE intent_id = $1`, third.IntentID))
	wantCode(t, err, "SV014")
	err = e.inTx(exec(e.ctx, `UPDATE broadcaster.content_owners SET status = 'VOID', status_at = clock_timestamp()
		WHERE intent_id = $1`, third.IntentID))
	wantCode(t, err, "SV014")
	if o, err := e.st.ContentOwnerFor(e.ctx, "pa", "content-w"); err != nil || o.Status != OwnerPending {
		t.Fatalf("row after refused commits %+v %v", o, err)
	}
}

// TestCallerScheduledOffer (Q3, plan 5.7 relay permission): true only for
// the caller's own OFFER_LEASE intent for that offer, COMMITTED with its own
// service signer as committing signer.
func TestCallerScheduledOffer(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	e.principal("q", 8)
	signer := e.signer(1)
	commitOne(t, e, signer, "offer-a")
	pending := e.enqueue("p", "offer-b", signer, time.Hour)
	for _, c := range []struct {
		caller, offer string
		want          bool
	}{{"p", "offer-a", true}, {"q", "offer-a", false}, {"p", "offer-b", false}, {"p", "offer-z", false}} {
		got, err := e.st.CallerScheduledOffer(e.ctx, c.caller, c.offer)
		if err != nil || got != c.want {
			t.Errorf("%s/%s: %v %v, want %v", c.caller, c.offer, got, err, c.want)
		}
	}
	// T5 with another account's first commitment: not scheduled by us.
	ss := e.lock(signer)
	defer ss.Close()
	if err := ss.SyncSequence(e.ctx, 7, 1, 42); err != nil {
		t.Fatal(err)
	}
	att := e.persist(ss, pending.IntentID, 1, 500)
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.MarkDead(e.ctx, att, DeadD3SequenceUsed); err != nil {
		t.Fatal(err)
	}
	foreign := sha256.Sum256([]byte("another scheduler's offer"))
	if err := tx.CommitIntent(e.ctx, pending.IntentID, Expect{StateInFlight, PhaseSigned}, Commitment{Height: 50,
		BlockHash: [32]byte{3}, TxHash: foreign, FirstCommitTxHash: foreign, GasWanted: 1, GasUsed: 1,
		Events: json.RawMessage(`[]`), CommittingSigner: providerAccount(4)}, false); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if got, err := e.st.CallerScheduledOffer(e.ctx, "p", "offer-b"); err != nil || got {
		t.Fatalf("offer committed by another account: %v %v", got, err)
	}
	if _, err := e.st.CallerScheduledOffer(e.ctx, "p", "Bad Offer"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("bad offer id: %v", err)
	}
}

// TestForwardedTransactions (Q2, plan 7.3 P3): a forwarded provider-signed
// transaction is stored verbatim with its decoded fields; its account is a
// FORWARD_SOURCE signer; the forwarder session alone persists it, as the
// one attempt of its intent, built from the stored bytes; it never retries
// or changes business id; the database refuses every inconsistent row.
func TestForwardedTransactions(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 16)
	service := e.signer(1)
	acct := providerAccount(1)
	txb, f := forwardTx("one", 5, 300)
	res, err := e.enqueueForward("p", "fwd-1", acct, txb, f, "offer-one")
	if err != nil {
		t.Fatal(err)
	}
	if res.BusinessID != "pbiz-one" {
		t.Fatalf("business id %q, want the forwarded message's", res.BusinessID)
	}
	w, err := e.st.GetWork(e.ctx, res.IntentID)
	hash := sha256.Sum256(txb)
	if err != nil || string(w.Request) != string(txb) || w.Forward == nil || string(w.Forward.TxHash) != string(hash[:]) ||
		w.Forward.Sequence != 5 || w.Forward.TimeoutHeight != 300 || w.Forward.MessageType != MsgAcceptLease ||
		w.Forward.ProviderID != "provider-1" || w.Forward.BusinessID != "pbiz-one" || string(w.Forward.OuterSigner) != string(acct) ||
		w.Object == nil || *w.Object != (ObjectRef{Type: ObjectOffer, ID: "offer-one"}) || w.BusinessAttempt != 1 {
		t.Fatalf("work %+v %v", w, err)
	}
	if got := e.count(`SELECT count(*) FROM broadcaster.business_attempts WHERE intent_id = $1 AND source = 'FORWARDED'
		AND business_id = 'pbiz-one'`, res.IntentID); got != 1 {
		t.Fatal("forwarded business attempt missing")
	}

	before := e.count(`SELECT count(*) FROM broadcaster.intents`)
	if _, err := e.enqueueForward("p", "fwd-dup", acct, txb, f, "offer-one"); !errors.Is(err, ErrTxTracked) {
		t.Fatalf("same bytes under another key: want ErrTxTracked, got %v", err)
	}
	bad := func(name string, mutate func(*EnqueueRequest), want error) {
		t.Helper()
		b2, f2 := forwardTx(name, 6, 300)
		r := EnqueueRequest{CallerPrincipal: "p", IdempotencyKey: "bad-" + name, Kind: KindForwardProvider, Request: b2,
			RequestDigest: sha256.Sum256(b2), AuthContext: []byte(`{}`), SignerAddress: acct,
			Object: ObjectRef{Type: ObjectOffer, ID: "offer-two"}, Forward: &f2, ExpiresIn: time.Hour}
		mutate(&r)
		if _, err := e.st.EnqueueHTTP(e.ctx, r); !errors.Is(err, want) {
			t.Errorf("%s: want %v, got %v", name, want, err)
		}
	}
	bad("no-forward-fields", func(r *EnqueueRequest) { r.Forward = nil }, ErrInvalid)
	bad("forward-on-key-kind", func(r *EnqueueRequest) { r.Kind, r.SignerAddress = KindOfferLease, service }, ErrInvalid)
	bad("object-mismatch", func(r *EnqueueRequest) { r.Object = ObjectRef{Type: ObjectLease, ID: "lease-1"} }, ErrInvalid)
	bad("renew-as-offer", func(r *EnqueueRequest) { r.Forward.MessageType = MsgRenewLease }, ErrInvalid)
	bad("audit-batch", func(r *EnqueueRequest) { r.Forward.MessageType = "/svn.evidence.v1.MsgSubmitAuditBatch" }, ErrInvalid)
	bad("no-deadline", func(r *EnqueueRequest) { r.Forward.TimeoutHeight = 0 }, ErrInvalid)
	bad("bad-business-id", func(r *EnqueueRequest) { r.Forward.BusinessID = "Bad Id" }, ErrInvalid)
	bad("key-signer-address", func(r *EnqueueRequest) { r.SignerAddress = service }, ErrSignerClass)
	if after := e.count(`SELECT count(*) FROM broadcaster.intents`); after != before {
		t.Fatalf("refused forwards wrote %d intents", after-before)
	}
	// Re-signed bytes may repeat the provider's business id.
	resigned, rf := forwardTx("one-resigned", 5, 400)
	rf.BusinessID = "pbiz-one"
	again, err := e.enqueueForward("p", "fwd-again", acct, resigned, rf, "offer-one")
	if err != nil {
		t.Fatalf("re-signed forward with the same business id: %v", err)
	}

	ks := e.lock(service)
	defer ks.Close()
	if _, err := ks.PersistForward(e.ctx, res.IntentID); !errors.Is(err, ErrInvalid) {
		t.Fatalf("PersistForward on a key session: want ErrInvalid, got %v", err)
	}
	fs, err := e.st.LockForwarder(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	if _, err := e.st.LockForwarder(e.ctx); !errors.Is(err, ErrSignerLockHeld) {
		t.Fatalf("second forwarder: want ErrSignerLockHeld, got %v", err)
	}
	if _, err := fs.PersistAttempt(e.ctx, draft(res.IntentID, 5, 300)); !errors.Is(err, ErrInvalid) {
		t.Fatalf("PersistAttempt on the forwarder: want ErrInvalid, got %v", err)
	}
	for name, call := range map[string]func() error{
		"SyncSequence":    func() error { return fs.SyncSequence(e.ctx, 3, 5, 10) },
		"Halt":            func() error { return fs.Halt(e.ctx, "INTEGRITY") },
		"HaltForConflict": func() error { return fs.HaltForConflict(e.ctx, 5, hash) },
		"ClearConflict":   func() error { return fs.ClearConflict(e.ctx, 5, hash, "signer-clear") },
	} {
		if err := call(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s on the forwarder: want ErrInvalid, got %v", name, err)
		}
	}
	offer := e.enqueue("p", "key-offer", service, time.Hour)
	if r, err := fs.PersistForward(e.ctx, offer.IntentID); err != nil || r.Refusal != RefusedWrongSigner {
		t.Fatalf("PersistForward of a key-signed intent: %+v %v", r, err)
	}
	due, err := e.st.DueForwards(e.ctx, 10)
	sort.Strings(due)
	want := []string{res.IntentID, again.IntentID}
	sort.Strings(want)
	if err != nil || strings.Join(due, ",") != strings.Join(want, ",") {
		t.Fatalf("due forwards %v %v, want %v", due, err, want)
	}

	if _, err := e.st.OpenStall(e.ctx, StallHeight, 10); err != nil {
		t.Fatal(err)
	}
	if r, err := fs.PersistForward(e.ctx, res.IntentID); err != nil || r.Refusal != RefusedStalled {
		t.Fatalf("PersistForward while stalled: %+v %v", r, err)
	}
	if ok, err := e.st.CloseStall(e.ctx, 11); err != nil || !ok {
		t.Fatalf("close stall: %v %v", ok, err)
	}
	r, err := fs.PersistForward(e.ctx, res.IntentID)
	if err != nil || r.Refusal != RefusedNone {
		t.Fatalf("PersistForward: %+v %v", r, err)
	}
	if r2, err := fs.PersistForward(e.ctx, again.IntentID); err != nil || r2.Refusal != RefusedSignerBusy {
		t.Fatalf("second forward at the same provider sequence: %+v %v", r2, err)
	}
	if r2, err := fs.PersistForward(e.ctx, res.IntentID); err != nil || r2.Refusal != RefusedNotRequested {
		t.Fatalf("PersistForward twice: %+v %v", r2, err)
	}
	var (
		storedBytes, storedHash []byte
		seq, timeout, gas, acn  int64
		phase                   string
	)
	if err := e.super.QueryRowContext(e.ctx, `SELECT tx_bytes, tx_hash, sequence, timeout_height, gas, account_number, phase
		FROM broadcaster.attempts WHERE attempt_id = $1`, r.AttemptID).Scan(&storedBytes, &storedHash, &seq, &timeout, &gas,
		&acn, &phase); err != nil {
		t.Fatal(err)
	}
	if string(storedBytes) != string(txb) || string(storedHash) != string(hash[:]) || seq != 5 || timeout != 300 ||
		gas != 200_000 || acn != 3 || phase != "SIGNED" {
		t.Fatalf("forwarded attempt is not the forwarded transaction: seq=%d timeout=%d gas=%d acct=%d phase=%s",
			seq, timeout, gas, acn, phase)
	}
	if st, ph, _ := e.state(res.IntentID); st != StateInFlight || ph != PhaseSigned {
		t.Fatalf("forwarded intent %s/%s", st, ph)
	}
	live, err := e.st.LiveForwardAttempts(e.ctx, 10)
	if err != nil || len(live) != 1 || live[0].ID != r.AttemptID || string(live[0].TxBytes) != string(txb) ||
		string(live[0].SignerAddress) != string(acct) {
		t.Fatalf("live forward attempts %+v %v", live, err)
	}
	if _, err := ks.BeginSend(e.ctx, r.AttemptID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("key session sends a forwarded attempt: want ErrNotFound, got %v", err)
	}
	if ok, err := fs.BeginSend(e.ctx, r.AttemptID); err != nil || !ok {
		t.Fatalf("forwarder begin send: %v %v", ok, err)
	}
	if ok, err := fs.RecordSendOutcome(e.ctx, r.AttemptID, SendOutcome{Class: SendAck}); err != nil || !ok {
		t.Fatalf("forwarder record ack: %v %v", ok, err)
	}
	ackd := Expect{StateInFlight, PhaseAcknowledged}
	tx, err := fs.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.RetryWait(e.ctx, res.IntentID, ackd, 0, "RETRY"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("RetryWait of a forward: want ErrInvalid, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	tx, err = fs.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.ResolveCommitted(e.ctx, r.AttemptID, CommittedResult{TxHash: hash, Height: 60, AccSeqPresent: true}); err != nil {
		t.Fatal(err)
	}
	if err := tx.CommitIntent(e.ctx, res.IntentID, ackd, Commitment{Height: 60, BlockHash: [32]byte{6}, TxHash: hash,
		FirstCommitTxHash: hash, GasWanted: 200_000, GasUsed: 9, Events: json.RawMessage(`[]`), CommittingSigner: acct},
		true); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit forwarded intent: %v", err)
	}
	if got := e.count(`SELECT observed_height FROM broadcaster.signers WHERE address = $1`, acct); got != 60 {
		t.Fatalf("forward source observed height %d", got)
	}
	if live, err := e.st.LiveForwardAttempts(e.ctx, 10); err != nil || len(live) != 0 {
		t.Fatalf("live forward attempts after commit %+v %v", live, err)
	}

	// The second forward: OpenBusinessAttempt refused; then it may persist.
	tx, err = fs.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.OpenBusinessAttempt(e.ctx, again.IntentID, Expect{StateRequested, PhaseQueued}, "REPLAY"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("OpenBusinessAttempt of a forward: want ErrInvalid, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}

	// The database refuses inconsistent forward rows.
	r3b, r3f := forwardTx("three", 8, 300)
	third, err := e.enqueueForward("p", "fwd-3", acct, r3b, r3f, "offer-three")
	if err != nil {
		t.Fatal(err)
	}
	other, otherHash := txBytes("not-forwarded", 8, 300)
	_, err = e.runtime.ExecContext(e.ctx, `INSERT INTO broadcaster.attempts (intent_id, n, business_n, signer_address,
			account_number, sequence, timeout_height, gas, tx_bytes, tx_hash, phase, worker_instance)
		VALUES ($1, 1, 1, $2, 3, 8, 300, 200000, $3, $4, 'SIGNED', 'direct')`, third.IntentID, acct, other, otherHash[:])
	wantCode(t, err, "SV013")
	_, err = e.owner.ExecContext(e.ctx, `UPDATE broadcaster.forwarded_txs SET sequence = 9 WHERE intent_id = $1`, third.IntentID)
	wantCode(t, err, "SV001")
	_, err = e.owner.ExecContext(e.ctx, `DELETE FROM broadcaster.forwarded_txs WHERE intent_id = $1`, third.IntentID)
	wantCode(t, err, "SV001")
	_, err = e.owner.ExecContext(e.ctx, `TRUNCATE broadcaster.forwarded_txs`)
	wantCode(t, err, "SV001")
	_, err = e.runtime.ExecContext(e.ctx, `INSERT INTO broadcaster.forwarded_txs (intent_id, tx_hash, message_type,
			outer_signer, provider_id, account_number, sequence, timeout_height, gas)
		VALUES ($1, $2, '/svn.leases.v1.MsgAcceptLease', $3, 'provider-1', 3, 0, 300, 1)`, offer.IntentID, otherHash[:], service)
	wantCode(t, err, "SV013")
	_, err = e.runtime.ExecContext(e.ctx, `INSERT INTO broadcaster.business_attempts (intent_id, n, business_id, source)
		VALUES ($1, 2, 'pbiz-extra', 'FORWARDED')`, third.IntentID)
	wantCode(t, err, "SV013")
	_, err = e.runtime.ExecContext(e.ctx, `INSERT INTO broadcaster.business_attempts (intent_id, n, business_id, source)
		VALUES ($1, 2, 'bcastdrawnforkeyintent', 'FORWARDED')`, offer.IntentID)
	wantCode(t, err, "SV013")
	// A forwarded_txs row whose hash is not the request's is refused at once.
	rawForward := exec(e.ctx, `INSERT INTO broadcaster.intents (caller_principal, idempotency_key, kind, entry,
			request_digest, request, auth_context, validated, chain_id, signer_address, business_id, business_attempt,
			object_type, object_id, state, phase, expires_at, next_attempt_at)
		SELECT 'p', 'raw-forward', 'FORWARD_PROVIDER_TX', 'HTTP', $1, $2, '{}'::jsonb, true, b.chain_id, $3, 'pbiz-raw', 1,
			'OFFER', 'offer-raw', 'REQUESTED', 'QUEUED', clock_timestamp() + interval '1 hour', clock_timestamp()
		FROM broadcaster.chain_binding b`, hash[:], []byte("raw"), acct)
	rawBusiness := exec(e.ctx, `INSERT INTO broadcaster.business_attempts (intent_id, n, business_id, source)
		SELECT intent_id, 1, 'pbiz-raw', 'FORWARDED' FROM broadcaster.intents WHERE idempotency_key = 'raw-forward'`)
	rawHash := sha256.Sum256([]byte("not raw"))
	err = e.inTx(rawForward, rawBusiness, exec(e.ctx, `INSERT INTO broadcaster.forwarded_txs (intent_id, tx_hash,
			message_type, outer_signer, provider_id, account_number, sequence, timeout_height, gas)
		SELECT intent_id, $1, '/svn.leases.v1.MsgAcceptLease', $2, 'provider-1', 3, 0, 300, 1
		FROM broadcaster.intents WHERE idempotency_key = 'raw-forward'`, rawHash[:], acct))
	wantCode(t, err, "SV013")

	// A forward whose attempt died never gets a second one, even if its row
	// is set back to QUEUED by hand.
	r4b, r4f := forwardTx("four", 9, 300)
	fourth, err := e.enqueueForward("p", "fwd-4", acct, r4b, r4f, "offer-four")
	if err != nil {
		t.Fatal(err)
	}
	r4, err := fs.PersistForward(e.ctx, fourth.IntentID)
	if err != nil || r4.Refusal != RefusedNone {
		t.Fatalf("PersistForward fourth: %+v %v", r4, err)
	}
	if err := e.inTx(
		exec(e.ctx, `UPDATE broadcaster.attempts SET phase = 'DEAD', dead_reason = 'D3_DEADLINE_PASSED',
			dead_at = clock_timestamp() WHERE attempt_id = $1`, r4.AttemptID),
		exec(e.ctx, `UPDATE broadcaster.intents SET state = 'REQUESTED', phase = 'QUEUED' WHERE intent_id = $1`,
			fourth.IntentID)); err != nil {
		t.Fatal(err)
	}
	if r, err := fs.PersistForward(e.ctx, fourth.IntentID); err != nil || r.Refusal != RefusedAttemptCap {
		t.Fatalf("second attempt of a forward: %+v %v", r, err)
	}

	// A validated forward without its forwarded_txs row fails at commit.
	err = e.inTx(rawForward, rawBusiness)
	wantCode(t, err, "SV013")
	if _, err := e.st.LiveForwardAttempts(e.ctx, 0); !errors.Is(err, ErrInvalid) {
		t.Fatalf("limit 0: %v", err)
	}
	if _, err := e.st.DueForwards(e.ctx, 1001); !errors.Is(err, ErrInvalid) {
		t.Fatalf("limit 1001: %v", err)
	}
}

// clearState is what a test reads to see whether an intent was cleared.
type clearState struct {
	request, requestJSON, auth []byte
	authDigest, reqDigest      []byte
	sqlDigest                  []byte
	cleared                    bool
}

func (e *env) clearState(intentID string) clearState {
	e.t.Helper()
	var (
		c       clearState
		reqJSON sql.NullString
		auth    sql.NullString
	)
	if err := e.super.QueryRowContext(e.ctx, `SELECT request, request_json::text, auth_context::text, auth_context_digest,
			request_digest, sql_digest, cleared_at IS NOT NULL FROM broadcaster.intents WHERE intent_id = $1`, intentID).Scan(
		&c.request, &reqJSON, &auth, &c.authDigest, &c.reqDigest, &c.sqlDigest, &c.cleared); err != nil {
		e.t.Fatal(err)
	}
	if reqJSON.Valid {
		c.requestJSON = []byte(reqJSON.String)
	}
	if auth.Valid {
		c.auth = []byte(auth.String)
	}
	return c
}

// wantCleared fails unless intentID was cleared keeping authDigest and its
// other digests, and every attempt of it lost its bytes but kept its hash.
func (e *env) wantCleared(intentID string, authDigest []byte) {
	e.t.Helper()
	c := e.clearState(intentID)
	if !c.cleared || c.request != nil || c.requestJSON != nil || c.auth != nil || string(c.authDigest) != string(authDigest) ||
		(c.reqDigest == nil && c.sqlDigest == nil) {
		e.t.Fatalf("intent not cleared as Q4 says: %+v", c)
	}
	if n := e.count(`SELECT count(*) FROM broadcaster.attempts WHERE intent_id = $1 AND (tx_bytes IS NOT NULL
		OR octet_length(tx_hash) <> 32)`, intentID); n != 0 {
		e.t.Fatalf("%d attempts keep their bytes or lost their hash", n)
	}
}

// authDigest returns the SHA-256 of an intent's stored authorization
// context's jsonb text, as the status view reports it.
func (e *env) authDigest(principal, intentID string) []byte {
	e.t.Helper()
	it, err := e.st.GetIntent(e.ctx, principal, intentID)
	if err != nil || len(it.AuthContextDigest) != 32 {
		e.t.Fatalf("auth digest: %+v %v", it, err)
	}
	return it.AuthContextDigest
}

// TestClearUnsent (Q4 "Minimal + clear unsent (Recommended)"): every way an
// intent ends FAILED without any send started clears its request, request
// JSON, authorization context and attempt bytes in the transaction that
// fails it, keeping their digests, records it in the FAILED transition and
// shows it in the status view; a FAILED intent with a started send and a
// COMMITTED intent keep everything; a cleared SQL-path intent still answers
// a replay by its kept digests.
func TestClearUnsent(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 32)
	signer := e.signer(1)
	// COMMITTED is never cleared. Committing first uses sequence 0 and
	// raises the signer's observed height to 42.
	committed := commitOne(t, e, signer, "committed")
	if c := e.clearState(committed.intentID); c.cleared || c.request == nil || c.auth == nil {
		t.Fatalf("a COMMITTED intent was cleared: %+v", c)
	}

	cancelled := e.enqueue("p", "cancelled", signer, time.Hour)
	digest := e.authDigest("p", cancelled.IntentID)
	sum := sha256.Sum256([]byte(e.clearState(cancelled.IntentID).auth))
	if string(digest) != string(sum[:]) {
		t.Fatal("status view digest is not the SHA-256 of the stored jsonb text")
	}
	if err := e.st.Cancel(e.ctx, "p", cancelled.IntentID); err != nil {
		t.Fatal(err)
	}
	e.wantCleared(cancelled.IntentID, digest)
	it, err := e.st.GetIntent(e.ctx, "p", cancelled.IntentID)
	if err != nil || it.ClearedAt == nil || string(it.AuthContextDigest) != string(digest) || it.State != StateFailed {
		t.Fatalf("status view of a cleared intent %+v %v", it, err)
	}
	trs, err := e.st.Transitions(e.ctx, cancelled.IntentID)
	if err != nil || len(trs) != 2 || trs[1].Reason != "CANCELLED" || !strings.Contains(string(trs[1].Observation), `"authorizations_cleared": true`) {
		t.Fatalf("FAILED transition %+v %v", trs, err)
	}

	// T4, never sent: expires after the 5 s lower bound.
	req, rd := request("expiring")
	exp, err := e.st.EnqueueHTTP(e.ctx, EnqueueRequest{CallerPrincipal: "p", IdempotencyKey: "expiring", Kind: KindOfferLease,
		Request: req, RequestDigest: rd, AuthContext: []byte(`{"principal_id":"p1"}`), SignerAddress: signer,
		Object: testOffer, ExpiresIn: 5 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	expView, err := e.st.GetIntent(e.ctx, "p", exp.IntentID)
	if err != nil {
		t.Fatal(err)
	}
	expDigest := expView.AuthContextDigest

	// Persisted, never sent, killed by (d3), then failed (T14).
	unsentAtt := e.enqueue("p", "unsent-attempt", signer, time.Hour)
	unsentDigest := e.authDigest("p", unsentAtt.IntentID)
	ss := e.lock(signer)
	defer ss.Close()
	att := e.persist(ss, unsentAtt.IntentID, 1, 100)
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.MarkDead(e.ctx, att, DeadD3DeadlinePassed); err != nil {
		t.Fatal(err)
	}
	if err := tx.FailIntent(e.ctx, unsentAtt.IntentID, Expect{StateInFlight, PhaseSigned}, "EXPIRED_DEAD", nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	e.wantCleared(unsentAtt.IntentID, unsentDigest)
	atts, err := e.st.Attempts(e.ctx, unsentAtt.IntentID)
	if err != nil || len(atts) != 1 || !atts[0].BytesCleared || len(atts[0].TxHash) != 32 {
		t.Fatalf("cleared attempt %+v %v", atts, err)
	}

	// Sent once, then dead and failed: everything is kept.
	sent := e.enqueue("p", "sent", signer, time.Hour)
	if err := ss.SyncSequence(e.ctx, 7, 1, 42); err != nil {
		t.Fatal(err)
	}
	sentAtt := e.persist(ss, sent.IntentID, 1, 101)
	if ok, err := ss.BeginSend(e.ctx, sentAtt); err != nil || !ok {
		t.Fatalf("begin send: %v %v", ok, err)
	}
	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.MarkDead(e.ctx, sentAtt, DeadD3DeadlinePassed); err != nil {
		t.Fatal(err)
	}
	if err := tx.FailIntent(e.ctx, sent.IntentID, Expect{StateInFlight, PhaseSigned}, "EXPIRED_DEAD", nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if c := e.clearState(sent.IntentID); c.cleared || c.request == nil || c.auth == nil || c.authDigest != nil {
		t.Fatalf("an intent with a started send was cleared: %+v", c)
	}
	if n := e.count(`SELECT count(*) FROM broadcaster.attempts WHERE intent_id = $1 AND tx_bytes IS NOT NULL`, sent.IntentID); n != 1 {
		t.Fatal("a sent attempt lost its bytes")
	}

	// A register voids its owner row and clears; the owner row keeps its
	// principal id.
	reg, err := e.registerContent("p", "reg", "content-c", "carol", signer)
	if err != nil {
		t.Fatal(err)
	}
	regDigest := e.authDigest("p", reg.IntentID)
	if err := e.st.Cancel(e.ctx, "p", reg.IntentID); err != nil {
		t.Fatal(err)
	}
	e.wantCleared(reg.IntentID, regDigest)
	if n := e.count(`SELECT count(*) FROM broadcaster.content_owners WHERE intent_id = $1 AND status = 'VOID'
		AND principal_id = 'carol'`, reg.IntentID); n != 1 {
		t.Fatal("register owner row not VOID")
	}

	// A forward's stored bytes are cleared; its hash and decoded fields stay.
	txb, f := forwardTx("clear", 1, 300)
	fwd, err := e.enqueueForward("p", "fwd", providerAccount(2), txb, f, "offer-clear")
	if err != nil {
		t.Fatal(err)
	}
	fwdDigest := e.authDigest("p", fwd.IntentID)
	if err := e.st.FailValidation(e.ctx, fwd.IntentID, "PROVIDER_SIGNATURE_INVALID"); err != nil {
		t.Fatal(err)
	}
	e.wantCleared(fwd.IntentID, fwdDigest)
	h := sha256.Sum256(txb)
	if n := e.count(`SELECT count(*) FROM broadcaster.forwarded_txs WHERE intent_id = $1 AND tx_hash = $2`, fwd.IntentID, h[:]); n != 1 {
		t.Fatal("forwarded hash not kept")
	}
	if w, err := e.st.GetWork(e.ctx, fwd.IntentID); err != nil || w.Request != nil || w.AuthContext != nil || w.Forward == nil {
		t.Fatalf("work of a cleared forward %+v %v", w, err)
	}

	// SQL path: T2 clears the JSON; a replay is answered by the digests.
	callerDB := e.caller("p-sql", 8)
	enq := func(requestJSON, auth string) (string, bool, error) {
		var (
			id      string
			created bool
		)
		err := callerDB.QueryRowContext(e.ctx, `SELECT intent_id, created FROM broadcaster.enqueue_intent($1, $2::jsonb,
			$3::jsonb, $4, $5::interval)`, KindOfferLease, requestJSON, auth, "sql-key", "1 hour").Scan(&id, &created)
		return id, created, err
	}
	sqlID, _, err := enq(`{"a":1,"b":2}`, `{"principal_id":"p1","permissions":["SCHEDULE_HOSTING"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.st.FailValidation(e.ctx, sqlID, "UNAUTHORIZED"); err != nil {
		t.Fatal(err)
	}
	if c := e.clearState(sqlID); !c.cleared || c.requestJSON != nil || c.auth != nil || len(c.sqlDigest) != 32 {
		t.Fatalf("SQL-path intent not cleared: %+v", c)
	}
	again, created, err := enq(`{"b":2,"a":1}`, `{"permissions":["SCHEDULE_HOSTING"],"principal_id":"p1"}`)
	if err != nil || created || again != sqlID {
		t.Fatalf("replay of a cleared intent: %s %v %v", again, created, err)
	}
	_, _, err = enq(`{"a":1,"b":2}`, `{"principal_id":"p2"}`)
	wantCode(t, err, "SV006")
	_, _, err = enq(`{"a":1,"b":3}`, `{"principal_id":"p1","permissions":["SCHEDULE_HOSTING"]}`)
	wantCode(t, err, "SV006")

	time.Sleep(time.Until(expView.ExpiresAt.Add(500 * time.Millisecond)))
	expired, err := e.st.ExpiredRequested(e.ctx, 10)
	if err != nil || len(expired) != 1 || expired[0] != exp.IntentID {
		t.Fatalf("expired requested %v %v", expired, err)
	}
	if err := e.st.Expire(e.ctx, exp.IntentID); err != nil {
		t.Fatal(err)
	}
	e.wantCleared(exp.IntentID, expDigest)
	if _, _, reason := e.state(exp.IntentID); reason != "EXPIRED_NOT_SENT" {
		t.Fatalf("expiry reason %q", reason)
	}
}

// TestFailIntentUnsentWithLiveAttempt (Q4): failing an intent whose
// never-sent attempt is still live is ErrLiveAttempt at once, before the
// clearing step touches anything; resolving the attempt first works.
func TestFailIntentUnsentWithLiveAttempt(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 8)
	signer := e.signer(1)
	in := e.enqueue("p", "live-unsent", signer, time.Hour)
	ss := e.lock(signer)
	defer ss.Close()
	att := e.persist(ss, in.IntentID, 0, 100)
	tx, err := ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.FailIntent(e.ctx, in.IntentID, Expect{StateInFlight, PhaseSigned}, "OBJECT_TAKEN", nil); !errors.Is(err, ErrLiveAttempt) {
		t.Fatalf("fail with a live never-sent attempt: want ErrLiveAttempt, got %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if c := e.clearState(in.IntentID); c.cleared || c.request == nil {
		t.Fatalf("refused fail cleared the intent: %+v", c)
	}
	if st, ph, _ := e.state(in.IntentID); st != StateInFlight || ph != PhaseSigned {
		t.Fatalf("intent %s/%s", st, ph)
	}
	tx, err = ss.Begin(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := tx.MarkDead(e.ctx, att, DeadD3DeadlinePassed); err != nil {
		t.Fatal(err)
	}
	if err := tx.FailIntent(e.ctx, in.IntentID, Expect{StateInFlight, PhaseSigned}, "OBJECT_TAKEN", nil); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if c := e.clearState(in.IntentID); !c.cleared {
		t.Fatal("not cleared after the attempt died")
	}
}

// TestTerminalTriggersAllowOnlyClearing (Q4): the terminal-row triggers
// accept exactly the clearing update of a FAILED intent that never started
// a send, after its attempts' bytes, and of those attempts' bytes, and
// nothing else: not on a COMMITTED or sent intent, not with another column,
// not partly, not with a wrong digest, not twice, not before the attempts,
// not on a live row. A commit that leaves a FAILED never-sent intent
// uncleared is refused (SV011).
func TestTerminalTriggersAllowOnlyClearing(t *testing.T) {
	e := newEnv(t)
	e.principal("p", 32)
	signer := e.signer(1)
	clearAttempts := `UPDATE broadcaster.attempts SET tx_bytes = NULL WHERE intent_id = $1`
	clearIntent := `UPDATE broadcaster.intents SET request = NULL, request_json = NULL, auth_context = NULL,
		auth_context_digest = sha256(convert_to(auth_context::text, 'UTF8')), cleared_at = clock_timestamp()
		WHERE intent_id = $1`
	failIntent := `UPDATE broadcaster.intents SET state = 'FAILED', phase = NULL, reason = 'CANCELLED',
		final_at = clock_timestamp() WHERE intent_id = $1`

	// Committing first uses sequence 0 and raises the observed height to 42.
	committed := commitOne(t, e, signer, "committed")
	// A REQUESTED intent with a DEAD never-sent attempt, the starting point
	// of the raw cases.
	victim := e.enqueue("p", "victim", signer, time.Hour)
	ss := e.lock(signer)
	defer ss.Close()
	killAttempt(t, ss, victim.IntentID, e.persist(ss, victim.IntentID, 1, 100), Expect{StateInFlight, PhaseSigned})
	id := victim.IntentID

	refused := map[string][]string{
		"live row: clear the context":   {`UPDATE broadcaster.intents SET auth_context = NULL WHERE intent_id = $1`},
		"live row: set cleared_at":      {`UPDATE broadcaster.intents SET cleared_at = clock_timestamp() WHERE intent_id = $1`},
		"live row: clear attempt bytes": {clearAttempts},
		"intent before its attempts":    {failIntent, clearIntent},
		"with another column": {failIntent, clearAttempts, `UPDATE broadcaster.intents SET request = NULL, request_json = NULL,
			auth_context = NULL, auth_context_digest = sha256(convert_to(auth_context::text, 'UTF8')),
			cleared_at = clock_timestamp(), reason = 'OTHER' WHERE intent_id = $1`},
		"context only": {failIntent, clearAttempts, `UPDATE broadcaster.intents SET auth_context = NULL,
			auth_context_digest = sha256(convert_to(auth_context::text, 'UTF8')), cleared_at = clock_timestamp()
			WHERE intent_id = $1`},
		"wrong digest": {failIntent, clearAttempts, `UPDATE broadcaster.intents SET request = NULL, request_json = NULL,
			auth_context = NULL, auth_context_digest = sha256('x'::bytea), cleared_at = clock_timestamp() WHERE intent_id = $1`},
		"no digest": {failIntent, clearAttempts, `UPDATE broadcaster.intents SET request = NULL, request_json = NULL,
			auth_context = NULL, cleared_at = clock_timestamp() WHERE intent_id = $1`},
		"twice":                 {failIntent, clearAttempts, clearIntent, clearIntent},
		"after clearing":        {failIntent, clearAttempts, clearIntent, `UPDATE broadcaster.intents SET transitions_suppressed = transitions_suppressed + 1 WHERE intent_id = $1`},
		"delete after clearing": {failIntent, clearAttempts, clearIntent, `DELETE FROM broadcaster.intents WHERE intent_id = $1`},
		"attempt bytes with another column": {failIntent, `UPDATE broadcaster.attempts SET tx_bytes = NULL,
			worker_instance = 'other' WHERE intent_id = $1`},
		"attempt bytes after the intent was cleared": {failIntent, clearAttempts, clearIntent, clearAttempts},
	}
	for name, stmts := range refused {
		var steps []func(*sql.Tx) error
		for _, q := range stmts {
			steps = append(steps, exec(e.ctx, q, id))
		}
		if err := e.inTx(steps...); SQLState(err) != "SV001" {
			t.Errorf("%s: want SV001, got %v", name, err)
		}
	}
	if c := e.clearState(id); c.cleared || c.request == nil || c.auth == nil {
		t.Fatalf("refused statements changed the intent: %+v", c)
	}

	// The exact clearing is accepted, and SV011 refuses a commit without it.
	if err := e.inTx(exec(e.ctx, failIntent, id)); SQLState(err) != "SV011" {
		t.Fatalf("FAILED never-sent without clearing: want SV011 at commit, got %v", err)
	}
	digest := e.authDigest("p", id)
	if err := e.inTx(exec(e.ctx, failIntent, id), exec(e.ctx, clearAttempts, id), exec(e.ctx, clearIntent, id)); err != nil {
		t.Fatalf("the exact clearing: %v", err)
	}
	e.wantCleared(id, digest)

	// A sent intent and a COMMITTED intent refuse the clearing.
	sent := e.enqueue("p", "sent", signer, time.Hour)
	if err := ss.SyncSequence(e.ctx, 7, 1, 42); err != nil {
		t.Fatal(err)
	}
	sentAtt := e.persist(ss, sent.IntentID, 1, 101)
	if ok, err := ss.BeginSend(e.ctx, sentAtt); err != nil || !ok {
		t.Fatalf("begin send: %v %v", ok, err)
	}
	killAttempt(t, ss, sent.IntentID, sentAtt, Expect{StateInFlight, PhaseSigned})
	if err := e.inTx(exec(e.ctx, failIntent, sent.IntentID)); err != nil {
		t.Fatalf("fail a sent intent without clearing: %v", err)
	}
	for _, q := range []string{clearAttempts, clearIntent} {
		if err := e.inTx(exec(e.ctx, q, sent.IntentID)); SQLState(err) != "SV001" {
			t.Errorf("clearing a sent intent (%s): want SV001, got %v", strings.Fields(q)[1], err)
		}
	}
	for _, q := range []string{clearAttempts, clearIntent} {
		if err := e.inTx(exec(e.ctx, q, committed.intentID)); SQLState(err) != "SV001" {
			t.Errorf("clearing a COMMITTED intent (%s): want SV001, got %v", strings.Fields(q)[1], err)
		}
	}
	// The clearing function itself changes nothing for them.
	for _, i := range []string{sent.IntentID, committed.intentID} {
		var cleared bool
		if err := e.runtime.QueryRowContext(e.ctx, `SELECT broadcaster.clear_unsent($1)`, i).Scan(&cleared); err != nil || cleared {
			t.Errorf("clear_unsent(%s) = %v %v", i, cleared, err)
		}
	}
}

// TestMigration0002 (Q4, forward-only runner): applied over 0001 alone, the
// 0002 migration clears the FAILED never-sent intents already there and
// leaves the others; it refuses a database that already holds signer rows,
// whose class it cannot know, and changes nothing then.
func TestMigration0002(t *testing.T) {
	embedded, err := loadMigrations(migrationFS)
	if err != nil {
		t.Fatal(err)
	}
	fs0001 := fstest.MapFS{"migrations/0001_outbox.pgsql": &fstest.MapFile{Data: []byte(embedded[0].sql)}}

	e := newRawEnv(t)
	if _, err := migrate(e.ctx, e.owner, fs0001); err != nil {
		t.Fatal(err)
	}
	if err := BindChain(e.ctx, e.owner, testChainID); err != nil {
		t.Fatal(err)
	}
	callerDB := e.caller("p-sql", 8)
	failedID, _, err := sqlEnqueue(e.ctx, callerDB, "will-fail", `{"v":1}`)
	if err != nil {
		t.Fatal(err)
	}
	openID, _, err := sqlEnqueue(e.ctx, callerDB, "stays-open", `{"v":2}`)
	if err != nil {
		t.Fatal(err)
	}
	mustExec(t, e.owner, `UPDATE broadcaster.intents SET state = 'FAILED', phase = NULL, reason = 'CANCELLED',
		final_at = clock_timestamp() WHERE intent_id = $1`, failedID)
	var authText string
	if err := e.super.QueryRowContext(e.ctx, `SELECT auth_context::text FROM broadcaster.intents WHERE intent_id = $1`,
		failedID).Scan(&authText); err != nil {
		t.Fatal(err)
	}
	if done, err := Migrate(e.ctx, e.owner); err != nil || len(done) != 1 || done[0] != 2 {
		t.Fatalf("apply 0002: %v %v", done, err)
	}
	sum := sha256.Sum256([]byte(authText))
	e.wantCleared(failedID, sum[:])
	if c := e.clearState(openID); c.cleared || c.requestJSON == nil || c.auth == nil {
		t.Fatalf("open intent changed by the migration: %+v", c)
	}

	withSigner := newRawEnv(t)
	if _, err := migrate(withSigner.ctx, withSigner.owner, fs0001); err != nil {
		t.Fatal(err)
	}
	mustExec(t, withSigner.owner, `INSERT INTO broadcaster.signers (address, lock_key) VALUES ($1, 1)`, providerAccount(7))
	if _, err := Migrate(withSigner.ctx, withSigner.owner); err == nil {
		t.Fatal("0002 applied over a signer row without a class")
	}
	if got := withSigner.count(`SELECT count(*) FROM broadcaster.schema_migrations`); got != 1 {
		t.Fatalf("%d migrations recorded after the refusal", got)
	}
	if got := withSigner.count(`SELECT count(*) FROM pg_tables WHERE schemaname = 'broadcaster' AND tablename = 'kinds'`); got != 0 {
		t.Fatal("the refused 0002 left its tables")
	}
}
