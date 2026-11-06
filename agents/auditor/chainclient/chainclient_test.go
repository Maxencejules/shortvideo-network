package chainclient_test

// Unit tests only. The chain here is chaintest, an in-memory fake of the
// RPC methods (label: "unit-test fake chain RPC; not a node, not end-to-end
// evidence"). No result of these tests is end-to-end evidence; the devnet
// test runs the built binary against real nodes.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	coretypes "github.com/cometbft/cometbft/rpc/core/types"
	rpctypes "github.com/cometbft/cometbft/rpc/jsonrpc/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/batch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/chainclient"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/chainclient/chaintest"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/keys"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/store"
	"github.com/Maxencejules/shortvideo-network/chain/app"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// headerTime is 30 minutes into epoch 490896.
const headerTime = int64(1767225600 + 1800)

var window = batch.Window{Guard: 120, Inclusion: 60, Lifetime: 86400}

func testKey(t testing.TB, label string) keys.Key {
	t.Helper()
	seed := sha256.Sum256([]byte("svn-evidence-test-seed:" + label))
	k, err := keys.FromSeed(seed[:])
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func manifestOf(contentID string) [32]byte { return sha256.Sum256([]byte("manifest:" + contentID)) }

func heldLease(id, content string) *leasesv1.Lease {
	m := manifestOf(content)
	return &leasesv1.Lease{
		LeaseId: id, ContentId: content, ContentVersion: 1, ProviderId: "p1",
		ManifestDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: m[:]},
		PackageBytes:   4096, DeadlineUnixSeconds: headerTime + 7200,
		State: leasesv1.LeaseState_LEASE_STATE_ACTIVE, ReservationHeld: true,
	}
}

func reportOf(t testing.TB, key keys.Key, leaseID, content, result string) []byte {
	t.Helper()
	r := report.Report{
		AuditorKeyID: "key-auditor-1", AuditorAddress: hex.EncodeToString(key.Address()),
		LeaseID: leaseID, ContentID: content, ContentVersion: 1, ProviderID: "p1",
		ExpectedManifestSHA256: manifestOf(content), Mode: report.ModeWholePackage, CheckedSegments: 2,
		StartedUnixSeconds: headerTime - 30, FinishedUnixSeconds: headerTime - 29, Result: result,
	}
	if result == report.ResultFail {
		r.Reasons = []string{"SEGMENT_DIGEST_MISMATCH"}
		r.FailedSegments = []report.FailedSegment{{SegmentID: "s0", Reason: "SEGMENT_DIGEST_MISMATCH"}}
		r.FailedSegmentCount = 1
	}
	raw, err := report.Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

type env struct {
	node  *chaintest.Node
	sub   *chainclient.Submitter
	b     *batch.Batch
	key   keys.Key
	store *store.Store
	root  string
}

// setup builds a signed, stored batch of two items of provider p1 (lease
// l1 PASS, lease l2 FAIL) and a fake chain that holds both leases.
func setup(t *testing.T) *env {
	t.Helper()
	key := testKey(t, "auditor-1")
	node := chaintest.New("svn-test-1", headerTime)
	node.Accounts[hex.EncodeToString(key.Address())] = &chaintest.Account{Number: 7}
	for _, l := range []*leasesv1.Lease{heldLease("l1", "c1"), heldLease("l2", "c2")} {
		node.Leases["p1/"+l.GetContentId()] = l
		node.Contents[l.GetContentId()+"/1"] = &contentv1.QueryContentResponse{Content: &contentv1.Content{
			ContentId: l.GetContentId(), ContentVersion: 1, State: contentv1.ContentState_CONTENT_STATE_PENDING,
		}}
	}
	root := filepath.Join(t.TempDir(), "store")
	st, err := store.Open(root)
	if err != nil {
		t.Fatal(err)
	}
	p := batch.Plan{Signer: key.Address(), KeyID: "key-auditor-1", HeaderTime: headerTime, MaxItems: batch.MaxItems, Window: window}
	var items []batch.Item
	for _, a := range []struct{ lease, content, result string }{{"l1", "c1", report.ResultPass}, {"l2", "c2", report.ResultFail}} {
		raw := reportOf(t, key, a.lease, a.content, a.result)
		if _, err := st.PutReport(raw); err != nil {
			t.Fatal(err)
		}
		it, err := batch.NewItem(p, batch.Audit{Report: raw, Deadline: headerTime + 7200, ManifestSHA256: manifestOf(a.content)})
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}
	batches, err := batch.Build(p, items)
	if err != nil {
		t.Fatal(err)
	}
	b := batches[0]
	if err := batch.Sign(b, key); err != nil {
		t.Fatal(err)
	}
	if err := batch.Persist(st, b, "run-test"); err != nil {
		t.Fatal(err)
	}
	client, err := chainclient.New(node)
	if err != nil {
		t.Fatal(err)
	}
	sub := &chainclient.Submitter{
		Client: client, Store: st, Key: &key, Gas: 2_000_000,
		Inclusion: 200 * time.Millisecond, Interval: 5 * time.Millisecond, Window: window,
	}
	t.Logf("%s", chaintest.Label)
	return &env{node: node, sub: sub, b: b, key: key, store: st, root: root}
}

// commitAll models a block that commits every accepted broadcast with code
// 0 and writes its records (DEC-010 state).
func commitAll(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) {
	n.Record(msg)
	n.Commit(hash, 0, "")
}

func indexLines(t *testing.T, st *store.Store) []chainclient.IndexLine {
	t.Helper()
	var out []chainclient.IndexLine
	if err := st.ScanIndex(func(line []byte) error {
		l, err := chainclient.ParseIndexLine(line)
		if err != nil {
			return err
		}
		out = append(out, l)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

func itemStates(res chainclient.Result) []chainclient.ItemState {
	var out []chainclient.ItemState
	for _, it := range res.Items {
		out = append(out, it.State)
	}
	return out
}

// passFirst returns the item states in batch order (evidence id order),
// with the PASS item's expected state RECORDED and the FAIL item's
// REJECTED.
func expectedStates(b *batch.Batch) []chainclient.ItemState {
	var out []chainclient.ItemState
	for _, it := range b.Items {
		if it.Result == report.ResultPass {
			out = append(out, chainclient.ItemRecorded)
		} else {
			out = append(out, chainclient.ItemRejected)
		}
	}
	return out
}

func equalStates(a, b []chainclient.ItemState) bool {
	return fmt.Sprint(a) == fmt.Sprint(b)
}

// An acknowledgement (CheckTx code 0) is SUBMITTED, never recorded; nothing
// is indexed until a committed result is read; the later commit is read by
// status, and indexed once.
func TestAcknowledgementIsNotCommit(t *testing.T) {
	e := setup(t)
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != chainclient.Submitted || res.Recorded() || res.Ack == nil || res.Ack.Code != 0 || res.Label() != "SUBMITTED" {
		t.Fatalf("after an acknowledgement: %+v", res)
	}
	for _, it := range res.Items {
		if it.State != chainclient.ItemNotOnChain {
			t.Fatalf("item %+v before any commit", it)
		}
	}
	if lines := indexLines(t, e.store); len(lines) != 0 {
		t.Fatalf("index written before a commit: %+v", lines)
	}
	status, err := e.sub.Status(context.Background(), e.b)
	if err != nil || status.State != chainclient.Submitted || status.Recorded() {
		t.Fatalf("status %+v (%v)", status, err)
	}
	// A block now commits the transaction.
	hash := e.node.Broadcasts()[0]
	e.node.Lock()
	msg, err := e.node.Message(e.node.Txs[hash].Raw)
	if err != nil {
		t.Fatal(err)
	}
	commitAll(e.node, msg, hash)
	e.node.Unlock()
	status, err = e.sub.Status(context.Background(), e.b)
	if err != nil || status.State != chainclient.Committed || !status.Recorded() || status.Label() != "COMMITTED(0)" ||
		!equalStates(itemStates(status), expectedStates(e.b)) || status.TxHash != hash || status.Height < 2 {
		t.Fatalf("status after the commit %+v (%v)", status, err)
	}
	lines := indexLines(t, e.store)
	if len(lines) != 2 || lines[0].TxHash != hash || lines[0].Code != 0 || lines[0].BatchID != e.b.ID() {
		t.Fatalf("index %+v", lines)
	}
	for i, l := range lines {
		if l.ReportSHA256 != hex.EncodeToString(e.b.Items[i].ReportSHA256[:]) || l.State != string(expectedStates(e.b)[i]) {
			t.Fatalf("index line %d %+v", i, l)
		}
	}
	if _, err := e.sub.Status(context.Background(), e.b); err != nil {
		t.Fatal(err)
	}
	if again := indexLines(t, e.store); len(again) != 2 {
		t.Fatalf("status indexed twice: %d lines", len(again))
	}
}

func TestCommittedResultIsReadBack(t *testing.T) {
	e := setup(t)
	e.node.OnBroadcast = commitAll
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != chainclient.Committed || !res.Recorded() || !equalStates(itemStates(res), expectedStates(e.b)) {
		t.Fatalf("%+v", res)
	}
	// A second submit finds the commit and broadcasts nothing.
	again, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || again.State != chainclient.Committed || len(e.node.Broadcasts()) != 1 {
		t.Fatalf("second submit %+v (%v), %d broadcasts", again, err, len(e.node.Broadcasts()))
	}
	// The broadcast transaction carries exactly the stored batch, signed by
	// the auditor key, at the account's sequence.
	first := e.node.Broadcasts()[0]
	e.node.Lock()
	raw := e.node.Txs[first].Raw
	msg, err := e.node.Message(raw)
	e.node.Unlock()
	if err != nil || !proto.Equal(msg, e.b.Msg) || evidence.VerifyAuditSignature(msg) != msgkit.OK {
		t.Fatalf("transaction message differs: %v", err)
	}
}

// A record under the batch's evidence id that differs from the batch item
// is ID_TAKEN, never recorded, even after a commit with code 0.
func TestDifferentRecordIsIDTaken(t *testing.T) {
	other := testKey(t, "auditor-2")
	changes := map[string]func(r *evidencev1.EvidenceRecord){
		"signer key": func(r *evidencev1.EvidenceRecord) { r.SignerPubkey = other.PublicKey() },
		"batch id":   func(r *evidencev1.EvidenceRecord) { r.BatchId = r.BatchId[:17] + "other" },
		"digest": func(r *evidencev1.EvidenceRecord) {
			r.ObservationDigest = &typesv1.Digest{Algorithm: 1, Value: make([]byte, 32)}
		},
		"epoch":  func(r *evidencev1.EvidenceRecord) { r.Epoch++ },
		"expiry": func(r *evidencev1.EvidenceRecord) { r.ExpiryUnixSeconds++ },
		"lease":  func(r *evidencev1.EvidenceRecord) { r.LeaseId = "l9" },
	}
	for name, change := range changes {
		e := setup(t)
		e.node.OnBroadcast = func(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) {
			n.Record(msg)
			change(n.Records[msg.GetItems()[0].GetEvidenceId()])
			n.Commit(hash, 0, "")
		}
		res, err := e.sub.Submit(context.Background(), e.b, "run-test")
		if err != nil {
			t.Fatal(err)
		}
		if res.Recorded() || res.Items[0].State != chainclient.ItemIDTaken || res.Items[1].State == chainclient.ItemIDTaken {
			t.Fatalf("%s: %+v", name, res)
		}
		if lines := indexLines(t, e.store); len(lines) != 2 || lines[0].State != string(chainclient.ItemIDTaken) {
			t.Fatalf("%s: index %+v", name, lines)
		}
	}
}

// A retry first asks the chain for the earlier transaction: when it has
// committed meanwhile, nothing is broadcast again.
func TestRetryQueriesBeforeResign(t *testing.T) {
	e := setup(t)
	if res, err := e.sub.Submit(context.Background(), e.b, "run-test"); err != nil || res.State != chainclient.Submitted {
		t.Fatalf("%+v %v", res, err)
	}
	first := e.node.Broadcasts()[0]
	e.node.Lock()
	msg, _ := e.node.Message(e.node.Txs[first].Raw)
	commitAll(e.node, msg, first)
	mark := len(e.node.Calls)
	e.node.Unlock()
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || res.State != chainclient.Committed || len(e.node.Broadcasts()) != 1 {
		t.Fatalf("%+v %v broadcasts %d", res, err, len(e.node.Broadcasts()))
	}
	e.node.Lock()
	calls := e.node.Calls[mark:]
	e.node.Unlock()
	if len(calls) == 0 || calls[0] != "tx "+first {
		t.Fatalf("the retry did not query the earlier hash first: %v", calls)
	}
	// Still nothing committed: the retry queries the hash and the evidence
	// ids, then broadcasts again with the current sequence.
	e2 := setup(t)
	if _, err := e2.sub.Submit(context.Background(), e2.b, "run-test"); err != nil {
		t.Fatal(err)
	}
	e2.node.Lock()
	mark = len(e2.node.Calls)
	e2.node.Unlock()
	if _, err := e2.sub.Submit(context.Background(), e2.b, "run-test"); err != nil {
		t.Fatal(err)
	}
	e2.node.Lock()
	calls = e2.node.Calls[mark:]
	e2.node.Unlock()
	broadcastAt, queried, readBack := -1, false, false
	for i, c := range calls {
		switch {
		case strings.HasPrefix(c, "broadcast ") && broadcastAt < 0:
			broadcastAt = i
		case strings.HasPrefix(c, "tx ") && broadcastAt < 0:
			queried = true
		case c == "abci_query /svn.evidence.v1.Query/Evidence" && broadcastAt < 0:
			readBack = true
		}
	}
	if broadcastAt < 0 || !queried || !readBack {
		t.Fatalf("retry order %v", calls)
	}
}

// REPLAY: the business id was taken; the next attempt uses a new, drawn
// business id with the signer's prefix, its record says attempt 1, and the
// auditor signature, which does not cover the business id, stays valid.
func TestReplayLeadsToANewBusinessID(t *testing.T) {
	e := setup(t)
	calls := 0
	e.node.OnBroadcast = func(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) {
		calls++
		if calls == 1 {
			n.Commit(hash, 3, "svn")
			return
		}
		commitAll(n, msg, hash)
	}
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || res.State != chainclient.Committed || !res.Recorded() {
		t.Fatalf("%+v %v", res, err)
	}
	hashes := e.node.Broadcasts()
	if len(hashes) != 2 {
		t.Fatalf("%d broadcasts", len(hashes))
	}
	e.node.Lock()
	m0, _ := e.node.Message(e.node.Txs[hashes[0]].Raw)
	m1, _ := e.node.Message(e.node.Txs[hashes[1]].Raw)
	e.node.Unlock()
	signer := e.b.Msg.GetSigner()
	if m0.GetBusinessId() != batch.BusinessID(signer, e.b.ID()) || m1.GetBusinessId() == m0.GetBusinessId() ||
		!msgkit.ValidID(m1.GetBusinessId()) || !evidence.OwnsID(signer, m1.GetBusinessId()) {
		t.Fatalf("business ids %s, %s", m0.GetBusinessId(), m1.GetBusinessId())
	}
	raw, err := e.store.Get(store.Batches, chainclient.SubmissionName(e.b.ID(), 1), ".json")
	if err != nil || !bytes.Contains(raw, []byte(`"business_attempt":1,"business_id":"`+m1.GetBusinessId()+`"`)) {
		t.Fatalf("submission 1 record %s (%v)", raw, err)
	}
	if evidence.VerifyAuditSignature(m1) != msgkit.OK || !bytes.Equal(m1.GetAuditorSignature().GetSignature(), m0.GetAuditorSignature().GetSignature()) {
		t.Fatal("the new attempt changed or broke the auditor signature")
	}
	lines := indexLines(t, e.store)
	if len(lines) != 4 || lines[0].Code != 3 || lines[0].State != "FAILED" || lines[2].Code != 0 {
		t.Fatalf("index %+v", lines)
	}
}

// A fresh header in another epoch warns EPOCH_LIKELY_STALE; the batch is
// still broadcast, the chain answers STALE_VERSION (5), and the state is
// FAILED(5), never recorded and never rebuilt.
func TestStaleEpochIsWarnedAndFails(t *testing.T) {
	e := setup(t)
	e.node.Advance(1, evidence.EpochSeconds)
	e.node.OnBroadcast = func(n *chaintest.Node, _ *evidencev1.MsgSubmitAuditBatch, hash string) { n.Commit(hash, 5, "svn") }
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != chainclient.Failed || res.Label() != "FAILED(5)" || res.Recorded() || res.Next != nil ||
		len(res.Warnings) != 1 || res.Warnings[0] != chainclient.WarnEpochStale || res.Ack == nil || res.Ack.Code != 0 {
		t.Fatalf("%+v", res)
	}
	if len(e.node.Broadcasts()) != 1 {
		t.Fatalf("%d broadcasts", len(e.node.Broadcasts()))
	}
	// A later submit does not broadcast a stale failed batch again.
	again, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || again.State != chainclient.Failed || len(e.node.Broadcasts()) != 1 {
		t.Fatalf("%+v %v", again, err)
	}
}

// A submitted batch whose epoch has passed is not broadcast again.
func TestSubmittedBatchInAPassedEpochIsNotRebroadcast(t *testing.T) {
	e := setup(t)
	if _, err := e.sub.Submit(context.Background(), e.b, "run-test"); err != nil {
		t.Fatal(err)
	}
	e.node.Advance(1, evidence.EpochSeconds)
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || res.State != chainclient.Submitted || len(e.node.Broadcasts()) != 1 || len(res.Warnings) != 1 {
		t.Fatalf("%+v %v, %d broadcasts", res, err, len(e.node.Broadcasts()))
	}
}

func TestCheckTxRejectionIsNotAccepted(t *testing.T) {
	e := setup(t)
	e.node.Ack, e.node.AckCodespace = 32, "sdk"
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || res.State != chainclient.NotAccepted || res.Label() != "NOT_ACCEPTED(sdk/32)" || res.Recorded() {
		t.Fatalf("%+v %v", res, err)
	}
	if n := e.node.CallCount("tx"); n != 0 {
		t.Fatalf("polled %d times after a CheckTx rejection", n)
	}
	status, err := e.sub.Status(context.Background(), e.b)
	if err != nil || status.State != chainclient.NotAccepted {
		t.Fatalf("%+v %v", status, err)
	}
	if lines := indexLines(t, e.store); len(lines) != 0 {
		t.Fatal("index written for a CheckTx rejection")
	}
}

// A rebroadcast while the first transaction waits in the mempool. The
// fake answers as a node's mempool does (chaintest Node.Mempool): the same
// bytes again are "tx already exists in cache", a transport-level error,
// so the answer is UNKNOWN; other bytes at the same sequence (here another
// gas limit) are CheckTx sdk/32. Neither stops the waiting transaction, so
// the batch stays SUBMITTED, with REBROADCAST_NOT_ACCEPTED for the second,
// and the waiting transaction's commit is then read.
func TestRebroadcastWhileWaitingStaysSubmitted(t *testing.T) {
	e := setup(t)
	e.node.Mempool = true
	if res, err := e.sub.Submit(context.Background(), e.b, "run-test"); err != nil || res.State != chainclient.Submitted {
		t.Fatalf("first %+v %v", res, err)
	}
	first := e.node.Broadcasts()[0]
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || res.State != chainclient.Submitted || res.Ack != nil || len(res.Warnings) != 0 {
		t.Fatalf("same bytes again: %+v %v", res, err)
	}
	e.sub.Gas = 2_100_000
	res, err = e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || res.State != chainclient.Submitted || res.Label() != "SUBMITTED" || res.Recorded() ||
		res.Ack == nil || res.Ack.Code != 32 || res.Ack.Codespace != "sdk" ||
		len(res.Warnings) != 1 || res.Warnings[0] != chainclient.WarnRebroadcastNotAccepted {
		t.Fatalf("other bytes at the waiting sequence: %+v %v", res, err)
	}
	if b := e.node.Broadcasts(); len(b) != 3 || b[1] != first || b[2] == first {
		t.Fatalf("broadcasts %v", b)
	}
	status, err := e.sub.Status(context.Background(), e.b)
	if err != nil || status.State != chainclient.Submitted {
		t.Fatalf("status %+v %v", status, err)
	}
	e.node.Lock()
	msg, err := e.node.Message(e.node.Txs[first].Raw)
	if err != nil {
		t.Fatal(err)
	}
	commitAll(e.node, msg, first)
	e.node.Unlock()
	status, err = e.sub.Status(context.Background(), e.b)
	if err != nil || status.State != chainclient.Committed || !status.Recorded() || status.TxHash != first {
		t.Fatalf("status after the waiting transaction committed %+v %v", status, err)
	}
}

// The transaction bytes are stored before the broadcast (checked when the
// node receives them). When the record of the answer cannot be written
// (here a directory holds its name), the submit fails, but the stored bytes
// still name the transaction: status queries its hash, finds the commit,
// and indexes it, and a later submit broadcasts nothing.
func TestUnrecordedSubmissionIsQueried(t *testing.T) {
	e := setup(t)
	name := chainclient.SubmissionName(e.b.ID(), 0)
	blocker := filepath.Join(e.root, "batches", name+".json")
	e.node.OnBroadcast = func(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) {
		// The bytes are already stored when the node receives them.
		if raw, err := e.store.Get(store.Batches, name, ".pb"); err != nil || fmt.Sprintf("%x", sha256.Sum256(raw)) != hash {
			t.Errorf("transaction bytes not stored before the broadcast: %v", err)
		}
		commitAll(n, msg, hash)
		if err := os.Mkdir(blocker, 0o700); err != nil {
			t.Error(err)
		}
	}
	if res, err := e.sub.Submit(context.Background(), e.b, "run-test"); err == nil || !strings.Contains(err.Error(), "store submission") {
		t.Fatalf("submit with an unwritable record: %+v %v", res, err)
	}
	if err := os.Remove(blocker); err != nil {
		t.Fatal(err)
	}
	status, err := e.sub.Status(context.Background(), e.b)
	if err != nil || status.State != chainclient.Committed || !status.Recorded() || status.TxHash != e.node.Broadcasts()[0] {
		t.Fatalf("status %+v %v", status, err)
	}
	if lines := indexLines(t, e.store); len(lines) != 2 || lines[0].TxHash != status.TxHash {
		t.Fatalf("index %+v", lines)
	}
	again, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || again.State != chainclient.Committed || len(e.node.Broadcasts()) != 1 {
		t.Fatalf("submit after the commit %+v %v, %d broadcasts", again, err, len(e.node.Broadcasts()))
	}
}

// Stored submissions must be consistent: a record without transaction
// bytes, bytes that do not decode, bytes that carry another message, and
// submission 0 under another business id than the batch's first are
// errors; bytes of the batch at attempt 0 are an unrecorded submission
// (UNKNOWN, so SUBMITTED until a commit is seen). Later submissions:
// TestUnrecordedRetryKeepsItsBusinessID.
func TestStoredSubmissionMustCarryItsBatch(t *testing.T) {
	build := func(e *env, msg *evidencev1.MsgSubmitAuditBatch) []byte {
		raw, _, err := e.sub.Client.BuildTx(msg, e.key.SDK(), chainclient.TxInput{ChainID: "svn-test-1", AccountNumber: 7, Gas: 2_000_000})
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	otherEpoch := func(e *env) *evidencev1.MsgSubmitAuditBatch {
		m := proto.Clone(e.b.Msg).(*evidencev1.MsgSubmitAuditBatch)
		m.Epoch++
		return m
	}
	for name, c := range map[string]struct {
		ext  string
		data func(e *env) []byte
		ok   bool
	}{
		"record without bytes":     {".json", func(*env) []byte { return []byte("{}") }, false},
		"bytes that do not decode": {".pb", func(*env) []byte { return []byte("not a transaction") }, false},
		"another epoch":            {".pb", func(e *env) []byte { return build(e, otherEpoch(e)) }, false},
		"a retry business id as submission 0": {".pb", func(e *env) []byte {
			id, err := batch.RetryBusinessID(e.b.Msg.GetSigner(), nil)
			if err != nil {
				t.Fatal(err)
			}
			return build(e, batch.WithBusinessID(e.b.Msg, id))
		}, false},
		"attempt 0 of the batch": {".pb", func(e *env) []byte { return build(e, e.b.Msg) }, true},
	} {
		e := setup(t)
		if err := e.store.Put(store.Batches, chainclient.SubmissionName(e.b.ID(), 0), c.ext, c.data(e)); err != nil {
			t.Fatal(err)
		}
		status, err := e.sub.Status(context.Background(), e.b)
		if c.ok != (err == nil) || (c.ok && status.State != chainclient.Submitted) {
			t.Fatalf("%s: %+v %v", name, status, err)
		}
	}
}

// Unrecorded bytes of a later submission: after attempt 0 got REPLAY, the
// stored bytes of submission 1 (no record: the process stopped after the
// broadcast) carry a drawn business id. Status reads them as attempt 1,
// SUBMITTED, and the next submit rebroadcasts under that same business id;
// it draws no other. Bytes of submission 1 under a business id without the
// signer's prefix are an error.
func TestUnrecordedRetryKeepsItsBusinessID(t *testing.T) {
	setupReplayed := func(t *testing.T) *env {
		e := setup(t)
		e.node.OnBroadcast = func(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) { n.Commit(hash, 3, "svn") }
		e.sub.Rand = bytes.NewReader(nil) // no retry id can be drawn: submit stops after the REPLAY
		if _, err := e.sub.Submit(context.Background(), e.b, "run-test"); err == nil || !strings.Contains(err.Error(), "retry business id") {
			t.Fatalf("submit without a random source: %v", err)
		}
		if len(e.node.Broadcasts()) != 1 {
			t.Fatalf("%d broadcasts", len(e.node.Broadcasts()))
		}
		e.sub.Rand = nil
		return e
	}
	store1 := func(e *env, id string) {
		raw, _, err := e.sub.Client.BuildTx(batch.WithBusinessID(e.b.Msg, id), e.key.SDK(), chainclient.TxInput{ChainID: "svn-test-1", AccountNumber: 7, Sequence: 1, Gas: 2_000_000})
		if err != nil {
			t.Fatal(err)
		}
		if err := e.store.Put(store.Batches, chainclient.SubmissionName(e.b.ID(), 1), ".pb", raw); err != nil {
			t.Fatal(err)
		}
	}
	e := setupReplayed(t)
	retryID, err := batch.RetryBusinessID(e.b.Msg.GetSigner(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store1(e, retryID)
	status, err := e.sub.Status(context.Background(), e.b)
	if err != nil || status.State != chainclient.Submitted {
		t.Fatalf("status %+v %v", status, err)
	}
	e.node.OnBroadcast = func(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) { commitAll(n, msg, hash) }
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || res.State != chainclient.Committed || !res.Recorded() {
		t.Fatalf("submit %+v %v", res, err)
	}
	hashes := e.node.Broadcasts()
	e.node.Lock()
	last, _ := e.node.Message(e.node.Txs[hashes[len(hashes)-1]].Raw)
	e.node.Unlock()
	if len(hashes) != 2 || last.GetBusinessId() != retryID {
		t.Fatalf("%d broadcasts, last business id %s, want %s", len(hashes), last.GetBusinessId(), retryID)
	}
	raw, err := e.store.Get(store.Batches, chainclient.SubmissionName(e.b.ID(), 2), ".json")
	if err != nil || !bytes.Contains(raw, []byte(`"business_attempt":1,"business_id":"`+retryID+`"`)) {
		t.Fatalf("submission 2 record %s (%v)", raw, err)
	}

	foreign := setupReplayed(t)
	otherID, err := batch.RetryBusinessID(testKey(t, "auditor-2").Address(), nil)
	if err != nil {
		t.Fatal(err)
	}
	store1(foreign, otherID)
	if status, err := foreign.sub.Status(context.Background(), foreign.b); err == nil {
		t.Fatalf("submission 1 under another signer's business id: %+v", status)
	}
}

// Squatted business ids (security review finding 1): another account that
// saw attempt 0 in the mempool commits its business id first, and also the
// ids attempts 1 to 15 had when they were SHA-256("svn-audit-business:" +
// batch_id + ":" + attempt). Each of those business ids answers REPLAY.
// The retry draws its business id, so the batch commits on the second
// broadcast instead of failing every attempt up to the submission bound.
func TestSquattedBusinessIDsDoNotBlockTheBatch(t *testing.T) {
	e := setup(t)
	signer := e.b.Msg.GetSigner()
	squatted := map[string]bool{batch.BusinessID(signer, e.b.ID()): true}
	for attempt := 1; attempt < chainclient.MaxSubmissions; attempt++ {
		sum := sha256.Sum256([]byte(fmt.Sprintf("svn-audit-business:%s:%d", e.b.ID(), attempt)))
		squatted[evidence.IDPrefix(signer)+hex.EncodeToString(sum[:])[:batch.SuffixHex]] = true
	}
	e.node.OnBroadcast = func(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) {
		if squatted[msg.GetBusinessId()] {
			n.Commit(hash, 3, "svn")
			return
		}
		commitAll(n, msg, hash)
	}
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || res.State != chainclient.Committed || !res.Recorded() {
		t.Fatalf("%+v %v", res, err)
	}
	if n := len(e.node.Broadcasts()); n != 2 {
		t.Fatalf("%d broadcasts, want 2", n)
	}
}

// A broadcast whose answer was lost is UNKNOWN: it may still commit, so the
// batch is SUBMITTED and the next status finds the commit.
func TestLostBroadcastAnswerIsSubmitted(t *testing.T) {
	e := setup(t)
	e.node.BroadcastErr = errors.New("connection reset by peer")
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil || res.State != chainclient.Submitted || res.Ack != nil {
		t.Fatalf("%+v %v", res, err)
	}
	raw, err := e.store.Get(store.Batches, chainclient.SubmissionName(e.b.ID(), 0), ".json")
	if err != nil || !bytes.Contains(raw, []byte(`"ack":"UNKNOWN"`)) {
		t.Fatalf("submission record %s (%v)", raw, err)
	}
	hash := e.node.Broadcasts()[0]
	e.node.Lock()
	msg, _ := e.node.Message(e.node.Txs[hash].Raw)
	commitAll(e.node, msg, hash)
	e.node.Unlock()
	status, err := e.sub.Status(context.Background(), e.b)
	if err != nil || status.State != chainclient.Committed || !status.Recorded() {
		t.Fatalf("%+v %v", status, err)
	}
}

// TestBatchFailureIsolation: one item's lease ended while the batch waited,
// so the chain refused the whole batch (CONFLICT). Only that item is
// dropped; the rest goes into a new batch in the same epoch, which commits.
func TestBatchFailureIsolation(t *testing.T) {
	e := setup(t)
	ended := e.b.Items[0]
	calls := 0
	e.node.OnBroadcast = func(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) {
		calls++
		if calls == 1 {
			delete(n.Leases, "p1/"+ended.ContentID)
			n.Commit(hash, 14, "svn")
			return
		}
		commitAll(n, msg, hash)
	}
	res, err := e.sub.Submit(context.Background(), e.b, "run-test")
	if err != nil {
		t.Fatal(err)
	}
	if res.State != chainclient.Failed || res.Label() != "FAILED(14)" || len(res.Dropped) != 1 || res.Dropped[0] != ended.EvidenceID || res.Next == nil {
		t.Fatalf("%+v", res)
	}
	next := res.Next
	if next.State != chainclient.Committed || len(next.Items) != 1 || next.Items[0].EvidenceID == ended.EvidenceID || !res.Recorded() {
		t.Fatalf("rebuilt batch %+v", next)
	}
	nb, summary, err := batch.Load(e.store, next.BatchID)
	if err != nil || summary.Supersedes != e.b.ID() || nb.Epoch() != e.b.Epoch() || summary.RunID != "run-test" {
		t.Fatalf("rebuilt batch not stored as superseding: %+v %v", summary, err)
	}
	if _, ok := e.node.Records[ended.EvidenceID]; ok {
		t.Fatal("the dropped item was recorded")
	}
}

func TestBuildTx(t *testing.T) {
	e := setup(t)
	client, err := chainclient.New(e.node)
	if err != nil {
		t.Fatal(err)
	}
	raw, hash, err := client.BuildTx(e.b.Msg, e.key.SDK(), chainclient.TxInput{ChainID: "svn-test-1", AccountNumber: 7, Sequence: 3, Gas: 400000})
	if err != nil {
		t.Fatal(err)
	}
	if hash != sha256.Sum256(raw) || len(raw) > 65536 {
		t.Fatalf("hash or size")
	}
	enc, err := app.MakeEncoding()
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := enc.TxConfig.TxDecoder()(raw)
	if err != nil {
		t.Fatal(err)
	}
	tx := decoded.(authsigning.Tx)
	sigs, err := tx.GetSignaturesV2()
	if err != nil || len(sigs) != 1 || sigs[0].Sequence != 3 || !bytes.Equal(sigs[0].PubKey.Address(), e.b.Msg.GetSigner()) {
		t.Fatalf("signatures %+v %v", sigs, err)
	}
	if data, ok := sigs[0].Data.(*signingtypes.SingleSignatureData); !ok || data.SignMode != signingtypes.SignMode_SIGN_MODE_DIRECT {
		t.Fatal("sign mode is not DIRECT")
	}
	if tx.GetGas() != 400000 || !tx.GetFee().IsZero() || tx.GetMemo() != "" {
		t.Fatalf("gas %d fee %v memo %q", tx.GetGas(), tx.GetFee(), tx.GetMemo())
	}
	if msg := decoded.GetMsgs()[0].(*evidencev1.MsgSubmitAuditBatch); !proto.Equal(msg, e.b.Msg) {
		t.Fatal("message differs")
	}
	for name, in := range map[string]chainclient.TxInput{
		"gas 0":          {ChainID: "svn-test-1", Gas: 0},
		"gas above 20e6": {ChainID: "svn-test-1", Gas: 20_000_001},
		"no chain id":    {Gas: 1},
	} {
		if _, _, err := client.BuildTx(e.b.Msg, e.key.SDK(), in); err == nil {
			t.Fatalf("%s accepted", name)
		}
	}
	if _, _, err := client.BuildTx(e.b.Msg, testKey(t, "auditor-2").SDK(), chainclient.TxInput{ChainID: "svn-test-1", Gas: 1}); err == nil {
		t.Fatal("a key of another address signed the transaction")
	}
}

func TestReads(t *testing.T) {
	e := setup(t)
	client, err := chainclient.New(e.node)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	h, err := client.Header(ctx)
	if err != nil || h.ChainID != "svn-test-1" || h.Time != headerTime {
		t.Fatalf("%+v %v", h, err)
	}
	e.node.Lock()
	e.node.CatchingUp = true
	e.node.Unlock()
	if _, err := client.Header(ctx); !errors.Is(err, chainclient.ErrCatchingUp) {
		t.Fatalf("catching up: %v", err)
	}
	l, found, err := client.HeldLease(ctx, "p1", "c1")
	if err != nil || !found || l.GetLeaseId() != "l1" {
		t.Fatalf("lease %v %v", found, err)
	}
	if _, found, err := client.HeldLease(ctx, "p1", "c9"); err != nil || found {
		t.Fatalf("absent lease %v %v", found, err)
	}
	if _, found, err := client.Content(ctx, "c9", 1); err != nil || found {
		t.Fatalf("absent content %v %v", found, err)
	}
	if _, found, err := client.Provider(ctx, "p9"); err != nil || found {
		t.Fatalf("absent provider %v %v", found, err)
	}
	if _, found, err := client.Evidence(ctx, "79b70b8d1f8f5606.none"); err != nil || found {
		t.Fatalf("absent evidence %v %v", found, err)
	}
	number, sequence, err := client.Account(ctx, e.key.Address())
	if err != nil || number != 7 || sequence != 0 {
		t.Fatalf("account %d %d %v", number, sequence, err)
	}
	var qe *chainclient.QueryError
	if _, _, err := client.Account(ctx, testKey(t, "auditor-2").Address()); !errors.As(err, &qe) {
		t.Fatalf("absent account: %v", err)
	}
	if _, found, err := client.CommittedTx(ctx, make([]byte, 32)); err != nil || found {
		t.Fatalf("unknown tx %v %v", found, err)
	}
}

func TestSVNCode(t *testing.T) {
	cases := map[string]msgkit.Code{
		"svn/5": msgkit.StaleVersion, "svn/3": msgkit.Replay, "svn/14": msgkit.Conflict, "svn/0": msgkit.OK,
		"sdk/5": msgkit.OK, "svn/99": msgkit.OK, "svn/4294967295": msgkit.OK,
	}
	for in, want := range cases {
		var space string
		var code uint32
		if _, err := fmt.Sscanf(strings.Replace(in, "/", " ", 1), "%s %d", &space, &code); err != nil {
			t.Fatal(err)
		}
		if got := chainclient.SVNCode(space, code); got != want {
			t.Fatalf("%s: %s, want %s", in, got, want)
		}
	}
}

// jsonRPC is an httptest JSON-RPC endpoint for the real CometBFT client
// path: status answers a header, tx answers the node's not-found error,
// and every other method a JSON-RPC error.
func jsonRPC(t *testing.T, requests *atomic.Int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		var req rpctypes.RPCRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var resp rpctypes.RPCResponse
		switch req.Method {
		case "status":
			st := &coretypes.ResultStatus{}
			st.NodeInfo.Network = "svn-http-1"
			st.SyncInfo.LatestBlockHeight = 9
			st.SyncInfo.LatestBlockTime = time.Unix(headerTime, 0).UTC()
			resp = rpctypes.NewRPCSuccessResponse(req.ID, st)
		case "tx":
			resp = rpctypes.NewRPCErrorResponse(req.ID, -32603, "Internal error", "tx (ABCD) not found")
		default:
			resp = rpctypes.NewRPCErrorResponse(req.ID, -32601, "Method not found", req.Method)
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
}

// The real CometBFT HTTP client over HTTPClient: a status, a not-found tx,
// no redirect followed, no proxy, and a bounded response body.
func TestHTTPClient(t *testing.T) {
	var requests atomic.Int64
	srv := jsonRPC(t, &requests)
	defer srv.Close()
	rpc, err := chainclient.NewHTTP(srv.URL, 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	client, err := chainclient.New(rpc)
	if err != nil {
		t.Fatal(err)
	}
	h, err := client.Header(context.Background())
	if err != nil || h.ChainID != "svn-http-1" || h.Height != 9 || h.Time != headerTime {
		t.Fatalf("%+v %v", h, err)
	}
	if _, found, err := client.CommittedTx(context.Background(), []byte{0xab, 0xcd}); err != nil || found {
		t.Fatalf("not-found tx over JSON-RPC: %v %v", found, err)
	}

	var moved atomic.Int64
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		moved.Add(1)
		http.Redirect(w, r, srv.URL, http.StatusTemporaryRedirect)
	}))
	defer redirect.Close()
	rpc2, _ := chainclient.NewHTTP(redirect.URL, 5*time.Second)
	client2, _ := chainclient.New(rpc2)
	before := requests.Load()
	if _, err := client2.Header(context.Background()); err == nil {
		t.Fatal("a redirect was followed")
	}
	if requests.Load() != before || moved.Load() != 1 {
		t.Fatalf("redirect target reached: %d requests", requests.Load()-before)
	}

	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write(bytes.Repeat([]byte(" "), chainclient.MaxResponseBytes+1024))
	}))
	defer huge.Close()
	rpc3, _ := chainclient.NewHTTP(huge.URL, 5*time.Second)
	client3, _ := chainclient.New(rpc3)
	if _, err := client3.Header(context.Background()); err == nil || !strings.Contains(err.Error(), "above the bound") {
		t.Fatalf("oversized body: %v", err)
	}

	for _, bad := range []string{"ftp://127.0.0.1:1", "http://u:p@127.0.0.1:1", "http://127.0.0.1:1/x", "http://127.0.0.1:1?q", "http://127.0.0.1:1#f", ""} {
		if _, err := chainclient.NewHTTP(bad, time.Second); err == nil {
			t.Fatalf("%q accepted", bad)
		}
	}
	if chainclient.HTTPClient(time.Second).CheckRedirect == nil {
		t.Fatal("no redirect refusal")
	}
}

// chaintest is a unit-test fake: no non-test file outside it imports it.
func TestChaintestOnlyInTests(t *testing.T) {
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || strings.Contains(filepath.ToSlash(path), "chainclient/chaintest/") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if bytes.Contains(raw, []byte(`/chainclient/chaintest"`)) {
			t.Errorf("%s imports chaintest", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
