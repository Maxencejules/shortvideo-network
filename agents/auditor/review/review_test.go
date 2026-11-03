package review

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/batch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/keys"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/labels"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/store"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

const headerTime = int64(1767225600 + 600)

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

func reportOf(t testing.TB, key keys.Key, keyID, leaseID, content, result string) []byte {
	t.Helper()
	r := report.Report{
		AuditorKeyID: keyID, AuditorAddress: hex.EncodeToString(key.Address()),
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

type fixture struct {
	dir   string
	store *store.Store
	b     *batch.Batch
	// records by result: the chain records the batch would get.
	pass, fail *evidencev1.EvidenceRecord
	catalog    *labels.Catalog
}

// recordOf is the record the chain writes for item i (DEC-010 state).
func recordOf(msg *evidencev1.MsgSubmitAuditBatch, i int, state evidencev1.EvidenceState) *evidencev1.EvidenceRecord {
	it := msg.GetItems()[i]
	return &evidencev1.EvidenceRecord{
		EvidenceId: it.GetEvidenceId(), BatchId: msg.GetBatchId(), Kind: it.GetKind(), State: state,
		SignerPubkey: msg.GetAuditorSignature().GetPublicKey(), LeaseId: it.GetLeaseId(), ContentId: it.GetContentId(),
		ContentVersion: it.GetContentVersion(), ObservationDigest: proto.Clone(it.GetObservationDigest()).(*typesv1.Digest),
		Epoch: msg.GetEpoch(), ExpiryUnixSeconds: it.GetExpiryUnixSeconds(), SchemaVersion: msg.GetSchemaVersion(),
		Attestation: it.GetAttestation(),
	}
}

// build stores a signed batch of one PASS and one FAIL audit by the key of
// label, as `run` and `sign` would.
func build(t *testing.T, label string) *fixture {
	t.Helper()
	key := testKey(t, label)
	keyID := "key-" + label
	dir := filepath.Join(t.TempDir(), "store")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	p := batch.Plan{Signer: key.Address(), KeyID: keyID, HeaderTime: headerTime, MaxItems: batch.MaxItems,
		Window: batch.Window{Guard: 120, Inclusion: 60, Lifetime: 86400}}
	var items []batch.Item
	for _, a := range []struct{ lease, content, result string }{{"l1", "c1", report.ResultPass}, {"l2", "c2", report.ResultFail}} {
		raw := reportOf(t, key, keyID, a.lease, a.content, a.result)
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
	if err := batch.Persist(st, b, "run-review"); err != nil {
		t.Fatal(err)
	}
	catalog, err := labels.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{dir: dir, store: st, b: b, catalog: &catalog}
	for i, it := range b.Items {
		if it.Result == report.ResultPass {
			f.pass = recordOf(b.Msg, i, evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED)
		} else {
			f.fail = recordOf(b.Msg, i, evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED)
		}
	}
	return f
}

var node = Source{Kind: SourceNode, Location: "http://127.0.0.1:26657"}

func (f *fixture) review(t *testing.T, rec *evidencev1.EvidenceRecord, src Source) Result {
	t.Helper()
	res, err := Review(Input{EvidenceID: rec.GetEvidenceId(), Source: src, Record: rec, Found: true, Store: f.store, Labels: f.catalog})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func reportPath(f *fixture, rec *evidencev1.EvidenceRecord) string {
	for _, it := range f.b.Items {
		if it.EvidenceID == rec.GetEvidenceId() {
			return filepath.Join(f.dir, "reports", hex.EncodeToString(it.ReportSHA256[:])+".json")
		}
	}
	return ""
}

func TestConsistentPassAndFail(t *testing.T) {
	f := build(t, "auditor-1")
	pass := f.review(t, f.pass, node)
	if pass.Verdict != Consistent || pass.ReportResult != "PASS" || pass.State != "RECORDED" || pass.StateLabel != "Recorded" ||
		pass.ModeLabel != "Auditor attestation" || pass.ClaimLimit != "ATTESTATION_NOT_STORAGE_PROOF" || len(pass.Limitations) == 0 {
		t.Fatalf("%+v", pass)
	}
	if pass.RecordSource != "NODE http://127.0.0.1:26657" || pass.RecordSourceLabel != "read through an unverified ABCI query; trusts that node" {
		t.Fatalf("record source %q %q", pass.RecordSource, pass.RecordSourceLabel)
	}
	fail := f.review(t, f.fail, Source{Kind: SourceFile, Location: "/tmp/record.json"})
	if fail.Verdict != Consistent || fail.ReportResult != "FAIL" || fail.State != "REJECTED" ||
		fail.RecordSource != "FILE /tmp/record.json" || fail.RecordSourceLabel != "not read from the chain" {
		t.Fatalf("%+v", fail)
	}
	// An expired RECORDED record is still consistent with its PASS report.
	expired := proto.Clone(f.pass).(*evidencev1.EvidenceRecord)
	expired.State = evidencev1.EvidenceState_EVIDENCE_STATE_EXPIRED
	if res := f.review(t, expired, node); res.Verdict != Consistent || res.StateLabel != "Expired" {
		t.Fatalf("%+v", res)
	}
}

func TestNotOnChainOnlyFromANode(t *testing.T) {
	f := build(t, "auditor-1")
	res, err := Review(Input{EvidenceID: f.pass.GetEvidenceId(), Source: node, Found: false, Store: f.store})
	if err != nil || res.Verdict != NotOnChain {
		t.Fatalf("%+v %v", res, err)
	}
	// A record file never produces NOT_ON_CHAIN: no record is an input error.
	if _, err := Review(Input{EvidenceID: f.pass.GetEvidenceId(), Source: Source{Kind: SourceFile, Location: "x"}, Found: false, Store: f.store}); !errors.Is(err, ErrInput) {
		t.Fatalf("file without a record: %v", err)
	}
	if _, err := Review(Input{EvidenceID: "other.id", Source: node, Record: f.pass, Found: true, Store: f.store}); !errors.Is(err, ErrInput) {
		t.Fatalf("record of another id: %v", err)
	}
	if _, err := Review(Input{EvidenceID: f.pass.GetEvidenceId(), Source: Source{Kind: "HTTP"}, Record: f.pass, Found: true, Store: f.store}); !errors.Is(err, ErrInput) {
		t.Fatalf("unknown source: %v", err)
	}
}

func TestPayloadUnavailable(t *testing.T) {
	// The report deleted from the store.
	f := build(t, "auditor-1")
	if err := os.Remove(reportPath(f, f.pass)); err != nil {
		t.Fatal(err)
	}
	if res := f.review(t, f.pass, node); res.Verdict != PayloadUnavailable {
		t.Fatalf("deleted report: %+v", res)
	}
	// An empty store (a dishonest auditor that kept no report).
	empty, err := store.Open(filepath.Join(t.TempDir(), "empty"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := Review(Input{EvidenceID: f.fail.GetEvidenceId(), Source: node, Record: f.fail, Found: true, Store: empty})
	if err != nil || res.Verdict != PayloadUnavailable {
		t.Fatalf("empty store: %+v %v", res, err)
	}
	// The envelope or the signed batch missing.
	g := build(t, "auditor-1")
	if err := os.Remove(filepath.Join(g.dir, "envelopes", g.pass.GetEvidenceId()+".json")); err != nil {
		t.Fatal(err)
	}
	if res := g.review(t, g.pass, node); res.Verdict != PayloadUnavailable {
		t.Fatalf("missing envelope: %+v", res)
	}
	h := build(t, "auditor-1")
	if err := os.Remove(filepath.Join(h.dir, "batches", h.b.ID()+".pb")); err != nil {
		t.Fatal(err)
	}
	if res := h.review(t, h.pass, node); res.Verdict != PayloadUnavailable {
		t.Fatalf("missing batch: %+v", res)
	}
}

func overwrite(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestChecksumMismatch(t *testing.T) {
	// A tampered report no longer hashes to its name.
	f := build(t, "auditor-1")
	path := reportPath(f, f.fail)
	raw, _ := os.ReadFile(path)
	overwrite(t, path, []byte(strings.Replace(string(raw), `"bytes_read":"0"`, `"bytes_read":"1"`, 1)))
	if res := f.review(t, f.fail, node); res.Verdict != ChecksumMismatch {
		t.Fatalf("tampered report: %+v", res)
	}
	// A REJECTED record whose digest is not the report's SHA-256.
	g := build(t, "auditor-1")
	rec := proto.Clone(g.fail).(*evidencev1.EvidenceRecord)
	rec.ObservationDigest.Value = make([]byte, 32)
	if res := g.review(t, rec, node); res.Verdict != ChecksumMismatch {
		t.Fatalf("rejected digest: %+v", res)
	}
}

func TestReportInvalid(t *testing.T) {
	// A store whose summary names, under the right evidence id, bytes that
	// are not a canonical report. The batch is built around those bytes, so
	// every earlier check passes.
	key := testKey(t, "auditor-1")
	dir := filepath.Join(t.TempDir(), "store")
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	notReport := []byte(`{"format":"something else"}`)
	sum, err := st.PutReport(notReport)
	if err != nil {
		t.Fatal(err)
	}
	f := build(t, "auditor-1")
	msg := proto.Clone(f.b.Msg).(*evidencev1.MsgSubmitAuditBatch)
	it := msg.Items[0]
	it.EvidenceId = batch.EvidenceID(key.Address(), sum)
	it.ObservationDigest.Value = sum[:]
	msg.Items = msg.Items[:1]
	b := &batch.Batch{ProviderID: "p1", Msg: msg, Items: []batch.Item{{
		EvidenceID: it.EvidenceId, ProviderID: "p1", LeaseID: it.LeaseId, ContentID: it.ContentId, ContentVersion: 1,
		Result: report.ResultFail, ReportSHA256: sum, Observation: sum, Expiry: it.ExpiryUnixSeconds,
	}}}
	if err := batch.Sign(b, key); err != nil {
		t.Fatal(err)
	}
	if err := batch.Persist(st, b, "run-x"); err != nil {
		t.Fatal(err)
	}
	rec := recordOf(msg, 0, evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED)
	res, err := Review(Input{EvidenceID: rec.GetEvidenceId(), Source: node, Record: rec, Found: true, Store: st})
	if err != nil || res.Verdict != ReportInvalid {
		t.Fatalf("%+v %v", res, err)
	}
}

func TestSignatureInvalid(t *testing.T) {
	// The stored batch's signature no longer verifies.
	f := build(t, "auditor-1")
	path := filepath.Join(f.dir, "batches", f.b.ID()+".pb")
	msg := proto.Clone(f.b.Msg).(*evidencev1.MsgSubmitAuditBatch)
	msg.AuditorSignature.Signature[0] ^= 1
	raw, _ := proto.MarshalOptions{Deterministic: true}.Marshal(msg)
	overwrite(t, path, raw)
	if res := f.review(t, f.pass, node); res.Verdict != SignatureInvalid {
		t.Fatalf("broken signature: %+v", res)
	}
	// The record names another signer key than the stored batch.
	g := build(t, "auditor-1")
	rec := proto.Clone(g.pass).(*evidencev1.EvidenceRecord)
	rec.SignerPubkey = testKey(t, "auditor-2").PublicKey()
	if res := g.review(t, rec, node); res.Verdict != SignatureInvalid {
		t.Fatalf("other signer key: %+v", res)
	}
	// Stored bytes that are not a batch.
	h := build(t, "auditor-1")
	overwrite(t, filepath.Join(h.dir, "batches", h.b.ID()+".pb"), []byte{0xff, 0xff})
	if res := h.review(t, h.pass, node); res.Verdict != SignatureInvalid {
		t.Fatalf("undecodable batch: %+v", res)
	}
}

func TestEnvelopeMismatch(t *testing.T) {
	// The stored envelope was edited.
	f := build(t, "auditor-1")
	path := filepath.Join(f.dir, "envelopes", f.pass.GetEvidenceId()+".json")
	raw, _ := os.ReadFile(path)
	overwrite(t, path, []byte(strings.Replace(string(raw), `"content_version":1`, `"content_version":2`, 1)))
	if res := f.review(t, f.pass, node); res.Verdict != EnvelopeMismatch {
		t.Fatalf("edited envelope: %+v", res)
	}
	// The chain record differs from the signed envelope: expiry, epoch,
	// lease, and digest each change the record-derived envelope.
	for name, change := range map[string]func(r *evidencev1.EvidenceRecord){
		"expiry": func(r *evidencev1.EvidenceRecord) { r.ExpiryUnixSeconds++ },
		"epoch":  func(r *evidencev1.EvidenceRecord) { r.Epoch++ },
		"lease":  func(r *evidencev1.EvidenceRecord) { r.LeaseId = "l9" },
		"digest": func(r *evidencev1.EvidenceRecord) { r.ObservationDigest.Value[0] ^= 1 },
		"kind":   func(r *evidencev1.EvidenceRecord) { r.Kind = evidencev1.EvidenceKind_EVIDENCE_KIND_PROVIDER_RECEIPT },
	} {
		g := build(t, "auditor-1")
		rec := proto.Clone(g.pass).(*evidencev1.EvidenceRecord)
		change(rec)
		if res := g.review(t, rec, node); res.Verdict != EnvelopeMismatch {
			t.Fatalf("%s: %+v", name, res)
		}
	}
}

func TestRecordMismatch(t *testing.T) {
	// A FAIL report whose record reads RECORDED.
	f := build(t, "auditor-1")
	rec := proto.Clone(f.fail).(*evidencev1.EvidenceRecord)
	rec.State = evidencev1.EvidenceState_EVIDENCE_STATE_RECORDED
	if res := f.review(t, rec, node); res.Verdict != RecordMismatch {
		t.Fatalf("FAIL report, RECORDED record: %+v", res)
	}
	// A PASS report whose record reads REJECTED.
	g := build(t, "auditor-1")
	rec = proto.Clone(g.pass).(*evidencev1.EvidenceRecord)
	rec.State = evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED
	if res := g.review(t, rec, node); res.Verdict != ChecksumMismatch {
		// A REJECTED record's digest must be the report's SHA-256 first.
		t.Fatalf("PASS report, REJECTED record: %+v", res)
	}
}

// A dishonest auditor's self-consistent false report reviews CONSISTENT:
// the reviewer shows what was signed and recorded, not that it is true.
func TestSelfConsistentFalseReportIsConsistent(t *testing.T) {
	f := build(t, "auditor-2")
	if res := f.review(t, f.pass, node); res.Verdict != Consistent || !strings.Contains(res.Detail, "attestation, not proof") {
		t.Fatalf("%+v", res)
	}
	if evidence.ClaimLimit != "ATTESTATION_NOT_STORAGE_PROOF" {
		t.Fatal("claim limit")
	}
}

// Without the batch summary the report is found through the index; a
// corrupt index is PAYLOAD_UNAVAILABLE, never a guess.
func TestIndexFallback(t *testing.T) {
	f := build(t, "auditor-1")
	if err := os.Remove(filepath.Join(f.dir, "batches", f.b.ID()+".json")); err != nil {
		t.Fatal(err)
	}
	if res := f.review(t, f.pass, node); res.Verdict != PayloadUnavailable {
		t.Fatalf("no summary, no index: %+v", res)
	}
	var sum [32]byte
	for _, it := range f.b.Items {
		if it.EvidenceID == f.pass.GetEvidenceId() {
			sum = it.ReportSHA256
		}
	}
	line := `{"batch_id":"` + f.b.ID() + `","code":0,"codespace":"","evidence_id":"` + f.pass.GetEvidenceId() +
		`","height":5,"report_sha256":"` + hex.EncodeToString(sum[:]) + `","state":"RECORDED","tx_hash":"` + strings.Repeat("ab", 32) + `"}`
	if err := f.store.AppendIndex([]byte(line)); err != nil {
		t.Fatal(err)
	}
	if res := f.review(t, f.pass, node); res.Verdict != Consistent {
		t.Fatalf("index fallback: %+v", res)
	}
	index, err := os.OpenFile(filepath.Join(f.dir, "index.jsonl"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := index.WriteString(`{"torn":`); err != nil {
		t.Fatal(err)
	}
	index.Close()
	if res := f.review(t, f.pass, node); res.Verdict != PayloadUnavailable || !strings.Contains(res.Detail, "index") {
		t.Fatalf("corrupt index: %+v", res)
	}
}
