package worker

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"

	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/outbox"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgdev"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/pgtest"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/signer"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

func sumOf(b []byte) [32]byte { return sha256.Sum256(b) }

// refuseAll answers every broadcast with a CheckTx rejection of svn code c,
// a definitive refusal for (d2).
func refuseAll(c typesv1.ErrorCode) func(raw []byte) (*coretypes.ResultBroadcastTx, error, bool) {
	return func(raw []byte) (*coretypes.ResultBroadcastTx, error, bool) {
		h := sumOf(raw)
		return &coretypes.ResultBroadcastTx{Code: uint32(c), Codespace: "svn", Hash: h[:]}, nil, true
	}
}

// dropAll answers every broadcast with a transport error: the outcome is
// UNKNOWN and the node never saw the bytes.
func dropAll(raw []byte) (*coretypes.ResultBroadcastTx, error, bool) {
	return nil, errors.New("connection reset by test"), true
}

// ackWithoutMempool answers code 0 without adding the bytes to the
// mempool: an acknowledgement whose transaction never commits.
func ackWithoutMempool(raw []byte) (*coretypes.ResultBroadcastTx, error, bool) {
	h := sumOf(raw)
	return &coretypes.ResultBroadcastTx{Hash: h[:]}, nil, true
}

// TestRegisterCommitsWithEvidence: a REGISTER_CONTENT intent is signed,
// persisted and sent; the CheckTx acknowledgement shows SUBMITTED and never
// COMMITTED, however many passes run; once a block holds the transaction
// the intent is COMMITTED with the commitment of plan 9.5 equal to the
// node's /tx and /commit (own transaction, the typed event, the block
// hash), and its content owner row becomes EFFECTIVE.
func TestRegisterCommitsWithEvidence(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
	atts := e.attempts(id)
	if len(atts) != 1 || atts[0].Phase != store.AttemptAcknowledged || atts[0].SendsStarted != 1 || atts[0].SendsRecorded != 1 ||
		atts[0].Sequence != 0 || atts[0].AccountNumber != 7 || atts[0].TimeoutHeight != 15 {
		t.Fatalf("attempt after the first pass: %+v", atts)
	}
	for i := 0; i < 3; i++ {
		e.pass(s)
		if got := e.intent(id); got.state == "COMMITTED" {
			t.Fatal("an acknowledgement committed the intent")
		}
	}
	e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
	if e.ownerStatus(id) != "PENDING" {
		t.Fatal("owner row is not PENDING before the commit")
	}
	e.node.block()
	commitHeight := e.node.heightNow()
	e.pass(s)
	e.wantState(id, "COMMITTED", "", "")
	var (
		height                            int64
		blockHash, txHash, first, signerB []byte
		events                            string
		replay                            bool
		own                               bool
	)
	if err := e.super.QueryRowContext(e.ctx, `SELECT c.height, c.block_hash, c.tx_hash, c.first_commit_tx_hash, c.committing_signer,
			c.events::text, c.idempotent_replay, i.own_transaction
		FROM broadcaster.commitments c JOIN broadcaster.intents i ON i.intent_id = c.intent_id WHERE c.intent_id = $1`, id).
		Scan(&height, &blockHash, &txHash, &first, &signerB, &events, &replay, &own); err != nil {
		t.Fatal(err)
	}
	if height != commitHeight || !bytes.Equal(blockHash, e.node.blockHash(t, commitHeight)) || !bytes.Equal(txHash, atts[0].TxHash) ||
		!bytes.Equal(first, txHash) || !bytes.Equal(signerB, e.service.info.Address) || replay || !own ||
		!strings.Contains(events, "svn.events.v1.ContentRegistered") {
		t.Fatalf("commitment: height %d block %x tx %x first %x signer %x replay %t own %t events %s", height, blockHash, txHash, first,
			signerB, replay, own, events)
	}
	if code, ok := e.node.committedCode(sumOf(nil)); ok || code != 0 {
		t.Fatal("fake node reports a commit for unknown bytes")
	}
	if got := e.ownerStatus(id); got != "EFFECTIVE" {
		t.Fatalf("owner row %s after the commit, want EFFECTIVE", got)
	}
}

// TestFakeRPCSeesPersistedAttempt checks the persist order of plan 8.3: on
// every broadcast, the first send and each rebroadcast, the fake node reads
// the attempt over a separate connection; the attempt row must exist with
// its bytes' hash and sends_started must already count this send
// (sends_started > sends_recorded) before the node receives the bytes.
func TestFakeRPCSeesPersistedAttempt(t *testing.T) {
	e := newEnv(t)
	var violations []string
	seen := 0
	e.node.beforeBroadcast = func(raw []byte) {
		seen++
		h := sumOf(raw)
		var started, recorded int
		err := e.super.QueryRowContext(e.ctx, `SELECT sends_started, sends_recorded FROM broadcaster.attempts WHERE tx_hash = $1`, h[:]).
			Scan(&started, &recorded)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			violations = append(violations, "broadcast of bytes with no committed attempt row")
		case err != nil:
			violations = append(violations, err.Error())
		case started <= recorded:
			violations = append(violations, "broadcast before sends_started counted it")
		}
	}
	first := true
	e.node.onBroadcast = func(raw []byte) (*coretypes.ResultBroadcastTx, error, bool) {
		if first {
			first = false
			return dropAll(raw)
		}
		return nil, nil, false
	}
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	e.wantState(id, "IN_FLIGHT", "UNCERTAIN", "")
	e.until("rebroadcast acknowledged", 50, func() { e.pass(s) }, func() bool { return e.intent(id).phase == "ACKNOWLEDGED" })
	e.node.block()
	e.pass(s)
	e.wantState(id, "COMMITTED", "", "")
	if seen < 2 || len(violations) != 0 {
		t.Fatalf("%d broadcasts seen, violations %v", seen, violations)
	}
}

// TestTimeoutBeforeSubmissionResendsSameBytes (plan 13.4 TB1 at unit
// level): the first broadcast is lost; the attempt is UNCERTAIN, /tx is
// read before the resend, and the resend carries the same stored bytes:
// every broadcast hashes to the one attempt's hash, and exactly one
// transaction commits at the signer's sequence.
func TestTimeoutBeforeSubmissionResendsSameBytes(t *testing.T) {
	e := newEnv(t)
	first := true
	e.node.onBroadcast = func(raw []byte) (*coretypes.ResultBroadcastTx, error, bool) {
		if first {
			first = false
			return dropAll(raw)
		}
		return nil, nil, false
	}
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	e.wantState(id, "IN_FLIGHT", "UNCERTAIN", "")
	e.until("resend", 50, func() { e.pass(s) }, func() bool { return e.node.broadcastCount() >= 2 })
	e.node.block()
	e.pass(s)
	e.wantState(id, "COMMITTED", "", "")
	atts := e.attempts(id)
	if len(atts) != 1 {
		t.Fatalf("%d attempts, want 1", len(atts))
	}
	for _, h := range e.node.broadcastHashes() {
		if !bytes.Equal(h[:], atts[0].TxHash) {
			t.Fatal("a broadcast carried other bytes than the stored attempt")
		}
	}
	if e.node.sequence(e.service.info.Address) != 1 {
		t.Fatal("more than one transaction used the signer's sequence")
	}
}

// TestDeadlinePassedRetriesAtTheSameSequence (plan 13.4 TB2 at unit level):
// every broadcast is lost. The attempt stays live until the height reaches
// timeout_height + L; then the account sequence, read at that height, is at
// most the attempt's sequence and the attempt is DEAD by (d3), the effect
// check finds no effect, and the intent waits (RETRY_WAIT; the step that
// ends the attempt reconciles the live attempt only, settleLive). The next
// attempt reuses the sequence and the business id with a higher
// timeout_height, and commits once broadcasts pass.
func TestDeadlinePassedRetriesAtTheSameSequence(t *testing.T) {
	e := newEnv(t)
	e.node.onBroadcast = dropAll
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	e.pass(s)
	first := e.attempts(id)[0]
	e.node.blocks(int(first.TimeoutHeight) + 1 - int(e.node.heightNow()))
	e.pass(s)
	if got := e.attempts(id)[0]; got.Phase != store.AttemptSigned && got.Phase != store.AttemptUncertain {
		t.Fatalf("attempt %s one block below timeout_height + L", got.Phase)
	}
	e.node.blocks(1)
	e.settleLive(s)
	atts := e.attempts(id)
	if atts[0].Phase != store.AttemptDead || atts[0].DeadReason != string(store.DeadD3DeadlinePassed) {
		t.Fatalf("attempt %s %s at timeout_height + L, want DEAD D3_DEADLINE_PASSED", atts[0].Phase, atts[0].DeadReason)
	}
	e.wantState(id, "REQUESTED", "RETRY_WAIT", "D3_DEADLINE_PASSED")
	e.node.onBroadcast = nil
	e.until("second attempt", 100, func() { e.pass(s) }, func() bool { return len(e.attempts(id)) == 2 })
	atts = e.attempts(id)
	if atts[1].Sequence != atts[0].Sequence || atts[1].BusinessN != atts[0].BusinessN || atts[1].TimeoutHeight <= atts[0].TimeoutHeight {
		t.Fatalf("second attempt %+v after %+v", atts[1], atts[0])
	}
	e.node.block()
	e.pass(s)
	e.wantState(id, "COMMITTED", "", "")
}

// TestBackoffIsFullJitter (plan 8.7: backoff with full jitter): the retry
// delay after n stored attempts is uniform in [0, min(cap, base *
// 2^(n-1))]. With the test numbers (base 100 ms, cap 1 s), n from 1 to 6
// covers the doubling and the cap. 2,000 draws per n all lie in that range,
// and both the lowest and the highest tenth of it are hit (each miss has
// probability at most 0.9^2000, below 1e-91), so a worker that never waits,
// always waits the ceiling, or skips the doubling or the cap fails.
func TestBackoffIsFullJitter(t *testing.T) {
	w := &Worker{cfg: Config{Numbers: testNumbers()}}
	base, limit := w.cfg.Numbers.BackoffBase, w.cfg.Numbers.BackoffCap
	for n := 1; n <= 6; n++ {
		ceiling := min(base<<(n-1), limit)
		lowest, highest := time.Duration(1<<62), time.Duration(-1)
		for i := 0; i < 2000; i++ {
			d := w.backoff(n)
			if d < 0 || d > ceiling {
				t.Fatalf("backoff(%d) = %s, outside [0, %s]", n, d, ceiling)
			}
			lowest, highest = min(lowest, d), max(highest, d)
		}
		if lowest > ceiling/10 || highest < ceiling-ceiling/10 {
			t.Fatalf("backoff(%d) drew only [%s, %s] of [0, %s]", n, lowest, highest, ceiling)
		}
	}
}

// TestReplayOpensNewBusinessAttempt (plan 8.6 step 5): the attempt
// commits with svn REPLAY (another payload took the business id) and the
// effect check finds no effect: a new business attempt opens with a fresh
// random business id, and the next attempt carries it and commits.
func TestReplayOpensNewBusinessAttempt(t *testing.T) {
	e := newEnv(t)
	replays := 1
	e.node.execute = func(d chain.DecodedTx, raw []byte) (outcome, bool) {
		if replays > 0 {
			replays--
			return svnFailure(typesv1.ErrorCode_ERROR_CODE_REPLAY), true
		}
		return outcome{}, false
	}
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	before, err := e.st.GetWork(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	s := e.session(policy.ClassService)
	e.pass(s)
	e.node.block()
	e.pass(s)
	after, err := e.st.GetWork(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if after.BusinessAttempt != 2 || after.BusinessID == before.BusinessID {
		t.Fatalf("after REPLAY: business attempt %d, id changed %t", after.BusinessAttempt, after.BusinessID != before.BusinessID)
	}
	atts := e.attempts(id)
	if atts[0].Phase != store.AttemptExecutionFailed || atts[0].CommittedCode == nil || atts[0].CommittedCode.Code != uint32(typesv1.ErrorCode_ERROR_CODE_REPLAY) {
		t.Fatalf("first attempt %+v", atts[0])
	}
	e.until("attempt under the new business id", 100, func() { e.pass(s) }, func() bool { return len(e.attempts(id)) == 2 })
	if got := e.attempts(id)[1]; got.BusinessN != 2 || got.Sequence != 1 {
		t.Fatalf("second attempt %+v", got)
	}
	e.node.block()
	e.pass(s)
	e.wantState(id, "COMMITTED", "", "")
}

// TestStallBlocksNewAttempts (plan section 10): while a stall episode is
// open no attempt is signed, stored or sent and the intent keeps its
// business id; once the episode closes the attempt starts.
func TestStallBlocksNewAttempts(t *testing.T) {
	e := newEnv(t)
	if _, err := e.st.OpenStall(e.ctx, store.StallHeight, 10); err != nil {
		t.Fatal(err)
	}
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	s := e.session(policy.ClassService)
	for i := 0; i < 3; i++ {
		e.pass(s)
	}
	if len(e.attempts(id)) != 0 || e.node.broadcastCount() != 0 || e.signer.callCount() != 0 {
		t.Fatalf("stalled: %d attempts, %d broadcasts, %d signatures", len(e.attempts(id)), e.node.broadcastCount(), e.signer.callCount())
	}
	work, err := e.st.GetWork(e.ctx, id)
	if err != nil || work.BusinessID == "" || work.State != store.StateRequested {
		t.Fatalf("stalled intent %+v %v", work, err)
	}
	if _, err := e.st.CloseStall(e.ctx, 11); err != nil {
		t.Fatal(err)
	}
	e.pass(s)
	if len(e.attempts(id)) != 1 || e.node.broadcastCount() != 1 {
		t.Fatal("no attempt after the stall closed")
	}
}

// TestObserverStallEpisodes (plan section 10): the observer records the
// node's height in chain_health; a height that does not advance for the
// stall window T of database clock opens a HEIGHT_STALLED episode, which a
// higher height closes. A node that does not answer for T opens a
// NODE_UNREACHABLE episode, and chain_health is not refreshed meanwhile, so
// the observer goes stale (OBSERVER_UNAVAILABLE in the API) rather than
// claiming a stall by itself.
func TestObserverStallEpisodes(t *testing.T) {
	e := newEnv(t)
	o := &observer{w: e.w}
	if err := o.pass(e.ctx); err != nil {
		t.Fatal(err)
	}
	health, err := e.st.ReadChainHealth(e.ctx)
	if err != nil || health.LatestHeight != 10 {
		t.Fatalf("chain health %+v %v", health, err)
	}
	if err := o.pass(e.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.OpenStallEpisode(e.ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("episode open before the window: %v", err)
	}
	time.Sleep(e.cfg.Numbers.StallWindow + 100*time.Millisecond)
	if err := o.pass(e.ctx); err != nil {
		t.Fatal(err)
	}
	ep, err := e.st.OpenStallEpisode(e.ctx)
	if err != nil || ep.Reason != store.StallHeight || ep.StartedHeight != 10 {
		t.Fatalf("episode %+v %v", ep, err)
	}
	e.node.block()
	if err := o.pass(e.ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := e.st.OpenStallEpisode(e.ctx); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("episode still open after the height advanced: %v", err)
	}

	before, err := e.st.ReadChainHealth(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	e.node.setStatusErr(errors.New("connection refused by test"))
	deadline := time.Now().Add(e.cfg.Numbers.StallWindow + time.Second)
	for time.Now().Before(deadline) {
		if err := o.pass(e.ctx); err == nil {
			t.Fatal("observer pass succeeded with the node down")
		}
		time.Sleep(200 * time.Millisecond)
	}
	ep, err = e.st.OpenStallEpisode(e.ctx)
	if err != nil || ep.Reason != store.StallUnreachable {
		t.Fatalf("episode %+v %v with the node down", ep, err)
	}
	after, err := e.st.ReadChainHealth(e.ctx)
	if err != nil || !after.SeenAt.Equal(before.SeenAt) || after.Age < e.cfg.Numbers.StallWindow {
		t.Fatalf("chain health refreshed while the node was down: %+v", after)
	}
}

// TestSQLPathValidation (plan 6.3, T2): a SQL-path intent is validated by
// the worker before any signing: a caller allowed the kind gets its
// business id and PENDING owner row, then an attempt; a caller not allowed
// the kind fails with UNAUTHORIZED, with no attempt, no signature and no
// broadcast. INVALID, with nothing signed: an authorization context that is
// not the four-field context, a lifetime above the configured maximum, a
// RENEW relay living longer than the relay local expiry (owner answer Q4),
// and a register whose content id another caller's owner row holds.
func TestSQLPathValidation(t *testing.T) {
	e := newEnv(t)
	allowed := e.sqlCaller(principalSQL)
	limited := e.sqlCaller(principalSQLLimited)
	authJSON, err := json.Marshal(fullAuth())
	if err != nil {
		t.Fatal(err)
	}
	regJSON := registerJSON(e.registerMsg("c1"))
	good := sqlEnqueue(t, allowed, "REGISTER_CONTENT", regJSON, string(authJSON), "sql-1")
	offerJSON := `{"offer_id":"o1","content_id":"c1","content_version":1,"provider_id":"p1","manifest_digest_hex":"` +
		hex.EncodeToString(sha([]byte("manifest c1"))) + `","package_bytes":4096,"terms_digest_hex":"` + hex.EncodeToString(sha([]byte("terms"))) +
		`","start_unix_seconds":1900000000,"deadline_unix_seconds":1900003600,"expiry_unix_seconds":1900000600}`
	refused := sqlEnqueue(t, limited, "OFFER_LEASE", offerJSON, string(authJSON), "sql-2")
	badAuth := sqlEnqueue(t, allowed, "REGISTER_CONTENT", regJSON, `{"principal_kind":"DEV_FIXTURE","email":"x"}`, "sql-3")
	tooLong := sqlEnqueueFor(t, allowed, "REGISTER_CONTENT", registerJSON(e.registerMsg("c3")), string(authJSON), "sql-4", "48 hours")
	renewJSON := `{"lease_id":"l1","term_version":2,"deadline_unix_seconds":1900007200,"manifest_digest_hex":"` +
		hex.EncodeToString(sha([]byte("manifest c1"))) + `","package_bytes":4096,"provider_signature":{"domain":"SIGNATURE_DOMAIN_PROVIDER_RENEW",` +
		`"public_key_hex":"` + strings.Repeat("11", 32) + `","signature_hex":"` + strings.Repeat("22", 64) + `","key_id":"pk-p1",` +
		`"canonical_profile_version":1},"provider_id":"p1","content_id":"c1"}`
	relayTooLong := sqlEnqueueFor(t, allowed, "RELAY_RENEW_LEASE", renewJSON, string(authJSON), "sql-5", "2 hours")
	e.enqueue(principalApp, "http-c2", policy.KindRegisterContent, e.registerMsg("c2"), time.Hour)
	held := sqlEnqueue(t, allowed, "REGISTER_CONTENT", registerJSON(e.registerMsg("c2")), string(authJSON), "sql-6")
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.wantState(refused, "FAILED", "", "UNAUTHORIZED")
	e.wantState(badAuth, "FAILED", "", "INVALID")
	e.wantState(tooLong, "FAILED", "", "INVALID")
	e.wantState(relayTooLong, "FAILED", "", "INVALID")
	e.wantState(held, "FAILED", "", "INVALID")
	work, err := e.st.GetWork(e.ctx, good)
	if err != nil || !work.Validated || work.BusinessID == "" || e.ownerStatus(good) != "PENDING" {
		t.Fatalf("validated intent %+v %v", work, err)
	}
	s := e.session(policy.ClassService)
	e.pass(s)
	if len(e.attempts(good)) != 1 || len(e.attempts(refused)) != 0 || len(e.attempts(badAuth)) != 0 || len(e.attempts(tooLong)) != 0 ||
		len(e.attempts(held)) != 0 || e.node.broadcastCount() != 1 || e.signer.callCount() != 1 {
		t.Fatal("a refused SQL-path intent was signed or sent, or the valid one was not")
	}
}

// registerJSON is the SQL-path request object of a register message.
func registerJSON(m *contentv1.MsgRegisterContent) string {
	return fmt.Sprintf(`{"content_id":%q,"content_version":%d,"manifest_digest_hex":%q,"package_bytes":%d,`+
		`"hosting_intent":{"domain":"SIGNATURE_DOMAIN_HOSTING_INTENT","public_key_hex":%q,"signature_hex":%q,`+
		`"key_id":%q,"canonical_profile_version":1}}`, m.GetContentId(), m.GetContentVersion(),
		hex.EncodeToString(m.GetManifestDigest().GetValue()), m.GetPackageBytes(), hex.EncodeToString(m.GetHostingIntent().GetPublicKey()),
		hex.EncodeToString(m.GetHostingIntent().GetSignature()), m.GetHostingIntent().GetKeyId())
}

// sqlCaller creates a login role for principal, registers it as a SQL-path
// caller and returns a pool connected as it.
func (e *env) sqlCaller(principal string) *sql.DB {
	e.t.Helper()
	role := "caller_" + strings.ReplaceAll(principal, "-", "_") + "_" + e.name
	pass, err := srv.Cluster.CreateLoginRole(e.ctx, role)
	if err != nil {
		e.t.Fatal(err)
	}
	if err := srv.Cluster.GrantConnect(e.ctx, e.name, role); err != nil {
		e.t.Fatal(err)
	}
	if err := store.RegisterCaller(e.ctx, e.owner, principal, 16, role); err != nil {
		e.t.Fatal(err)
	}
	db := e.open(role, pass)
	e.t.Cleanup(func() { db.Close() })
	return db
}

// sqlEnqueue calls broadcaster.enqueue_intent as the caller.
func sqlEnqueue(t *testing.T, db *sql.DB, kind, requestJSON, authJSON, key string) string {
	t.Helper()
	return sqlEnqueueFor(t, db, kind, requestJSON, authJSON, key, "1 hour")
}

// sqlEnqueueFor is sqlEnqueue with the lifetime interval given.
func sqlEnqueueFor(t *testing.T, db *sql.DB, kind, requestJSON, authJSON, key, lifetime string) string {
	t.Helper()
	var id string
	var created bool
	if err := db.QueryRowContext(context.Background(), `SELECT intent_id, created FROM broadcaster.enqueue_intent($1, $2::jsonb, $3::jsonb, $4, $5::interval)`,
		kind, requestJSON, authJSON, key, lifetime).Scan(&id, &created); err != nil || !created {
		t.Fatalf("enqueue %s: %v", key, err)
	}
	return id
}

// TestExpiredInFlightAwaitsDeadline (plan 8.6 step 2, 8.7, T15): an
// acknowledged attempt whose intent expires is never abandoned and never
// sent again; the intent waits for the chain (AWAITING_DEADLINE) and fails
// with EXPIRED_DEAD only once the attempt is dead by (d3).
func TestExpiredInFlightAwaitsDeadline(t *testing.T) {
	e := newEnv(t)
	e.node.onBroadcast = ackWithoutMempool
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), 5*time.Second)
	s := e.session(policy.ClassService)
	e.pass(s)
	e.wantState(id, "IN_FLIGHT", "ACKNOWLEDGED", "")
	time.Sleep(5200 * time.Millisecond)
	e.pass(s)
	e.wantState(id, "IN_FLIGHT", "AWAITING_DEADLINE", "EXPIRED_AWAITING_CHAIN")
	sent := e.node.broadcastCount()
	for i := 0; i < 3; i++ {
		time.Sleep(150 * time.Millisecond)
		e.pass(s)
	}
	if e.node.broadcastCount() != sent {
		t.Fatal("a broadcast after expiry")
	}
	att := e.attempts(id)[0]
	e.node.blocks(int(att.TimeoutHeight) + int(outboxLimits().IndexingMargin) - int(e.node.heightNow()))
	e.pass(s)
	e.wantState(id, "FAILED", "", "EXPIRED_DEAD")
	if e.node.broadcastCount() != sent {
		t.Fatal("a broadcast after expiry")
	}
}

// TestSignerConflictHalts (DR-11-15, plan 8.1): after one of our
// transactions committed, a transaction the broadcaster never stored uses
// the service key's next sequence. The next allocation finds it with the
// acc_seq search, halts the signer with SIGNER_CONFLICT and a signer_events
// row, and signs, stores and sends nothing. After the operator's
// signer-clear acknowledgement the next attempt uses the sequence after the
// foreign one.
func TestSignerConflictHalts(t *testing.T) {
	e := newEnv(t)
	s := e.session(policy.ClassService)
	first := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	e.pass(s)
	e.node.block()
	e.pass(s)
	e.wantState(first, "COMMITTED", "", "")

	foreign := withSigner(offerMsg("o-foreign", "c1"), e.service.info.Address)
	setBusinessID(foreign, "offer-foreign")
	foreignBytes, foreignHash := e.signWith(e.service, foreign, 1)
	e.node.commitDirect(foreignBytes)
	second := e.enqueue(principalApp, "reg-2", policy.KindRegisterContent, e.registerMsg("c2"), time.Hour)
	calls, sends := e.signer.callCount(), e.node.broadcastCount()
	e.pass(s)
	sg, err := e.st.GetSigner(e.ctx, e.service.info.Address)
	if err != nil || sg.HaltedReason != "SIGNER_CONFLICT" {
		t.Fatalf("signer %+v %v, want SIGNER_CONFLICT", sg, err)
	}
	if status, err := e.st.Conflict(e.ctx, e.service.info.Address, 1, foreignHash); err != nil || status != store.ConflictOpen {
		t.Fatalf("signer event %s %v", status, err)
	}
	if len(e.attempts(second)) != 0 || e.signer.callCount() != calls || e.node.broadcastCount() != sends {
		t.Fatal("a halted signer signed, stored or sent")
	}
	e.pass(s)
	if len(e.attempts(second)) != 0 {
		t.Fatal("a halted signer stored an attempt")
	}
	if err := s.ss.ClearConflict(e.ctx, 1, foreignHash, "signer-clear"); err != nil {
		t.Fatal(err)
	}
	e.pass(s)
	atts := e.attempts(second)
	if len(atts) != 1 || atts[0].Sequence != 2 {
		t.Fatalf("attempt after signer-clear %+v", atts)
	}
}

// signWith signs msg with a fixture key at sequence seq outside the
// broadcaster (test code), and returns the bytes and hash.
func (e *env) signWith(k keyFixture, msg sdk.Msg, seq uint64) ([]byte, [32]byte) {
	e.t.Helper()
	signed, err := e.signer.sign(e.ctx, signer.SignRequest{KeyName: k.info.Name, AccountNumber: e.node.accountNumber(k.info.Address),
		Sequence: seq, Gas: 2_000_000, TimeoutHeight: uint64(e.node.heightNow()) + 50, Msg: msg})
	if err != nil {
		e.t.Fatal(err)
	}
	return signed.Bytes, signed.Hash
}

// setBusinessID sets the business_id field of msg.
func setBusinessID(msg sdk.Msg, id string) {
	r := msg.(proto.Message).ProtoReflect()
	r.Set(r.Descriptor().Fields().ByName("business_id"), protoreflect.ValueOfString(id))
}

// TestCrashBetweenSendsStartedAndRecord (plan 13.2): a helper process
// stores an attempt and counts its send as started, then dies by SIGKILL
// before it broadcasts or records anything. The restarted worker records
// that send as UNKNOWN, so although every later broadcast is refused with a
// definitive refusal, the attempt never dies by (d2); it dies only by (d3)
// once the height reaches timeout_height + L.
func TestCrashBetweenSendsStartedAndRecord(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	pre := e.session(policy.ClassService)
	if err := pre.open(e.ctx); err != nil {
		t.Fatal(err)
	}
	acct, seq, ok, err := pre.allocate(e.ctx)
	if err != nil || !ok {
		t.Fatalf("allocate: %v %v", ok, err)
	}
	pre.close()
	work, err := e.st.GetWork(e.ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	msg, err := messageOf(e.codec, work)
	if err != nil {
		t.Fatal(err)
	}
	timeout := uint64(e.node.heightNow()) + 5
	signed, err := e.signer.sign(e.ctx, signer.SignRequest{KeyName: e.service.info.Name, AccountNumber: acct, Sequence: seq,
		Gas: 2_000_000, TimeoutHeight: timeout, Msg: msg})
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	passFile := filepath.Join(dir, "password")
	if err := os.WriteFile(passFile, []byte(runtimePass), 0o600); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(dir, "counted")
	spec, err := json.Marshal(helperSpec{SocketDir: srv.Cluster.SocketDir(), Port: pgdev.Port, Database: e.name, User: store.RuntimeRole,
		PasswordFile: passFile, SignerHex: hex.EncodeToString(e.service.info.Address), IntentID: id, AccountNumber: acct, Sequence: seq,
		TimeoutHeight: timeout, Gas: 2_000_000, TxHex: hex.EncodeToString(signed.Bytes), Marker: marker})
	if err != nil {
		t.Fatal(err)
	}
	specFile := filepath.Join(dir, "spec.json")
	if err := os.WriteFile(specFile, spec, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := pgtest.HelperCommand(e.ctx, helperPersistAndCount)
	cmd.Env = append(cmd.Env, helperSpecEnv+"="+specFile)
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "helper marker", 60*time.Second, func() bool { _, err := os.Stat(marker); return err == nil })
	if err := cmd.Process.Signal(syscall.SIGKILL); err != nil {
		t.Fatal(err)
	}
	_ = cmd.Wait()
	atts := e.attempts(id)
	if len(atts) != 1 || atts[0].SendsStarted != 1 || atts[0].SendsRecorded != 0 || e.node.broadcastCount() != 0 {
		t.Fatalf("after the kill: %+v, %d broadcasts", atts, e.node.broadcastCount())
	}

	e.node.onBroadcast = refuseAll(typesv1.ErrorCode_ERROR_CODE_BOUNDS)
	s := e.session(policy.ClassService)
	e.pass(s)
	atts = e.attempts(id)
	if atts[0].UnknownSends != 1 || atts[0].Phase != store.AttemptUncertain {
		t.Fatalf("the unrecorded send was not recorded as UNKNOWN: %+v", atts[0])
	}
	e.wantState(id, "IN_FLIGHT", "UNCERTAIN", "")
	e.until("refused rebroadcasts", 100, func() { e.pass(s) }, func() bool { return e.attempts(id)[0].CountedRefusals >= 2 })
	if got := e.attempts(id)[0]; got.Phase == store.AttemptDead {
		t.Fatalf("attempt died by %s with an UNKNOWN send", got.DeadReason)
	}
	e.node.blocks(int(timeout) + int(outboxLimits().IndexingMargin) - int(e.node.heightNow()))
	e.pass(s)
	got := e.attempts(id)[0]
	if got.Phase != store.AttemptDead || got.DeadReason != string(store.DeadD3DeadlinePassed) {
		t.Fatalf("attempt %s %s, want DEAD D3_DEADLINE_PASSED", got.Phase, got.DeadReason)
	}
}

func waitFor(t *testing.T, label string, limit time.Duration, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for !done() {
		if time.Now().After(deadline) {
			t.Fatalf("%s: not reached within %v", label, limit)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// TestOfferNeedsEffectiveOwner (plan 5.7, owner answer Q3): an
// OFFER_LEASE intent is checked again before signing; while the owner row
// of the offered content is PENDING (the chain showed the registration, the
// worker has not confirmed it yet) the offer fails with UNAUTHORIZED and
// nothing is signed or sent; once housekeeping made the row EFFECTIVE, a new
// offer is signed and sent.
func TestOfferNeedsEffectiveOwner(t *testing.T) {
	e := newEnv(t)
	s := e.session(policy.ClassService)
	reg := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	e.node.mu.Lock()
	e.node.hidden[contentKey("c1", 1)] = true
	e.node.mu.Unlock()
	e.pass(s)
	e.node.block()
	e.pass(s)
	e.wantState(reg, "COMMITTED", "", "")
	if e.ownerStatus(reg) != "PENDING" {
		t.Fatal("owner row confirmed while the node hid the record")
	}
	e.node.mu.Lock()
	delete(e.node.hidden, contentKey("c1", 1))
	e.node.mu.Unlock()
	offer := e.enqueue(principalApp, "offer-1", policy.KindOfferLease, offerMsg("o1", "c1"), time.Hour)
	calls, sends := e.signer.callCount(), e.node.broadcastCount()
	e.pass(s)
	e.wantState(offer, "FAILED", "", "UNAUTHORIZED")
	if len(e.attempts(offer)) != 0 || e.signer.callCount() != calls || e.node.broadcastCount() != sends {
		t.Fatal("an offer for content with a PENDING owner row was signed or sent")
	}
	if err := e.w.housekeepingPass(e.ctx); err != nil {
		t.Fatal(err)
	}
	if e.ownerStatus(reg) != "EFFECTIVE" {
		t.Fatal("housekeeping did not confirm the owner row")
	}
	second := e.enqueue(principalApp, "offer-2", policy.KindOfferLease, offerMsg("o2", "c1"), time.Hour)
	e.pass(s)
	if len(e.attempts(second)) != 1 || e.node.broadcastCount() != sends+1 {
		t.Fatal("an offer for content with an EFFECTIVE owner row was not sent")
	}
}

// TestContentOwnerVoidOnFailedRegister (plan 5.7, owner answers Q3 and Q4):
// a register whose every send is refused definitively dies by (d2) and
// fails with REJECTED; a register whose content id another service
// registered first fails with OBJECT_TAKEN before any attempt. Both owner
// rows become VOID in the transaction that fails the intent, and the
// never-sent intent loses its stored request and authorization context.
func TestContentOwnerVoidOnFailedRegister(t *testing.T) {
	e := newEnv(t)
	s := e.session(policy.ClassService)
	e.node.onBroadcast = refuseAll(typesv1.ErrorCode_ERROR_CODE_BOUNDS)
	refused := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	e.pass(s)
	e.pass(s)
	e.wantState(refused, "FAILED", "", "REJECTED")
	if got := e.attempts(refused)[0]; got.DeadReason != string(store.DeadD2Refused) {
		t.Fatalf("attempt dead reason %q, want D2_REFUSED", got.DeadReason)
	}
	if e.ownerStatus(refused) != "VOID" {
		t.Fatal("owner row of a rejected register is not VOID")
	}

	e.node.onBroadcast = nil
	other := newKeyFixture("service-2", policy.ClassService)
	e.node.addAccount(other.info.Address, 9)
	e.signer.keys[other.info.Name] = other
	taken := withSigner(e.registerMsg("c2"), other.info.Address)
	setBusinessID(taken, "reg-other")
	raw, _ := e.signWith(other, taken, 0)
	e.node.commitDirect(raw)
	front := e.enqueue(principalApp, "reg-2", policy.KindRegisterContent, e.registerMsg("c2"), time.Hour)
	e.node.blocks(int(outboxLimits().IndexingMargin))
	e.pass(s)
	e.wantState(front, "FAILED", "", "OBJECT_TAKEN")
	if len(e.attempts(front)) != 0 || e.ownerStatus(front) != "VOID" {
		t.Fatal("front-run register sent an attempt or kept its owner row")
	}
	var cleared bool
	if err := e.super.QueryRowContext(e.ctx, `SELECT request IS NULL AND auth_context IS NULL AND cleared_at IS NOT NULL
		FROM broadcaster.intents WHERE intent_id = $1`, front).Scan(&cleared); err != nil || !cleared {
		t.Fatalf("never-sent failed intent kept its authorizations: %v", err)
	}
}

// TestRefusalRuleCopiesAgree: the (d2) refusal rule has three copies,
// chain.SendObservation.DefinitiveRefusal (the classifier), outbox
// Send.Refusal (the state machine) and the store's countsAsRefusal (which
// RecordSendOutcome enforces). For every class and the codes the rule
// names, the worker's conversion carries the classifier's verdict, the
// state machine reads the stored outcome the same way, the store accepts
// that verdict and refuses the opposite one.
func TestRefusalRuleCopiesAgree(t *testing.T) {
	e := newEnv(t)
	id := e.enqueue(principalApp, "reg-1", policy.KindRegisterContent, e.registerMsg("c1"), time.Hour)
	e.node.onBroadcast = dropAll
	s := e.session(policy.ClassService)
	e.pass(s)
	att := e.attempts(id)[0]
	cases := []chain.SendObservation{
		{Outcome: chain.SendUnknown},
		{Outcome: chain.SendCache},
		{Outcome: chain.SendNodeRefused},
		{Outcome: chain.SendMempoolRejected, Codespace: "sdk", Code: 32},
		{Outcome: chain.SendMempoolRejected, Codespace: "sdk", Code: 4},
		{Outcome: chain.SendMempoolRejected, Codespace: "sdk", Code: 30},
		{Outcome: chain.SendMempoolRejected, Codespace: "sdk", Code: 13},
		{Outcome: chain.SendMempoolRejected, Codespace: "svn", Code: 1},
		{Outcome: chain.SendMempoolRejected, Codespace: "svn", Code: 3},
		{Outcome: chain.SendMempoolRejected, Codespace: "svn", Code: 32},
		{Outcome: chain.SendMempoolRejected, Codespace: "other", Code: 32},
		{Outcome: chain.SendAck},
	}
	for _, obs := range cases {
		out := storeOutcome(obs)
		outcomeName, err := outbox.ParseSendOutcome(string(out.Class))
		if err != nil {
			t.Fatal(err)
		}
		send := outbox.Send{Outcome: outcomeName}
		if out.Class == store.SendMempoolRejected {
			send.Code = outbox.Code{Codespace: out.Codespace, Number: out.Code}
		}
		if out.CountsAsRefusal != obs.DefinitiveRefusal() || send.Refusal() != obs.DefinitiveRefusal() {
			t.Fatalf("%+v: worker %t, outbox %t, classifier %t", obs, out.CountsAsRefusal, send.Refusal(), obs.DefinitiveRefusal())
		}
		ok, err := s.ss.BeginSend(e.ctx, att.ID)
		if err != nil || !ok {
			t.Fatalf("begin send: %v %v", ok, err)
		}
		flipped := out
		flipped.CountsAsRefusal = !out.CountsAsRefusal
		if _, err := s.ss.RecordSendOutcome(e.ctx, att.ID, flipped); !errors.Is(err, store.ErrInvalid) {
			t.Fatalf("%+v: the store accepted the opposite verdict: %v", obs, err)
		}
		if recorded, err := s.ss.RecordSendOutcome(e.ctx, att.ID, out); err != nil || !recorded {
			t.Fatalf("%+v: the store refused the classifier's verdict: %v", obs, err)
		}
	}
}
