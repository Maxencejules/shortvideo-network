package batch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/classify"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/keys"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/store"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// Fixed test values. headerTime is 2026-01-01T00:00:00Z, the first second
// of epoch 490896.
const (
	headerTime = int64(1767225600)
	keyLabel   = "auditor-1"
)

var window = Window{Guard: 120, Inclusion: 60, Lifetime: 86400}

// testKey derives a TEST-ONLY key from a label, the rule of the chain's
// golden vectors: seed = sha256("svn-evidence-test-seed:" + label).
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

func lease(id, provider, content string, deadline int64) *leasesv1.Lease {
	m := manifestOf(content)
	return &leasesv1.Lease{
		LeaseId: id, ContentId: content, ContentVersion: 1, ProviderId: provider,
		ManifestDigest:      &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: m[:]},
		PackageBytes:        4096,
		DeadlineUnixSeconds: deadline, State: leasesv1.LeaseState_LEASE_STATE_ACTIVE, ReservationHeld: true,
	}
}

// reportBytes is a canonical report of one audit by the key of label, of
// an endpoint from a capability manifest.
func reportBytes(t testing.TB, label, leaseID, provider, content, result string, started int64) []byte {
	t.Helper()
	return reportFrom(t, report.EndpointSourceCapabilityManifest, label, leaseID, provider, content, result, started)
}

// reportFrom is reportBytes with the given endpoint_source.
func reportFrom(t testing.TB, source, label, leaseID, provider, content, result string, started int64) []byte {
	t.Helper()
	r := report.Report{
		AuditorKeyID: "key-" + label, AuditorAddress: hex.EncodeToString(testKey(t, label).Address()),
		LeaseID: leaseID, ContentID: content, ContentVersion: 1, ProviderID: provider, EndpointIndex: 0,
		EndpointNameSHA256: sha256.Sum256([]byte("loopback test endpoint")), EndpointSource: source,
		ExpectedManifestSHA256: manifestOf(content), Mode: report.ModeWholePackage, CheckedSegments: 4,
		StartedUnixSeconds: started, FinishedUnixSeconds: started + 1, Result: result, Limitations: []string{"Auditors can lie."},
	}
	switch result {
	case report.ResultFail:
		r.Reasons = []string{"SEGMENT_DIGEST_MISMATCH"}
		r.FailedSegments = []report.FailedSegment{{SegmentID: "s0001", Reason: "SEGMENT_DIGEST_MISMATCH"}}
		r.FailedSegmentCount = 1
	case report.ResultIncomplete:
		r.Reasons = []string{"CONNECTION_RESET"}
	}
	raw, err := report.Encode(r)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func plan(t testing.TB, label string) Plan {
	return Plan{Signer: testKey(t, label).Address(), KeyID: "key-" + label, HeaderTime: headerTime, MaxItems: MaxItems, Window: window}
}

func item(t testing.TB, p Plan, label, leaseID, provider, content, result string, started int64) Item {
	t.Helper()
	it, err := NewItem(p, Audit{Report: reportBytes(t, label, leaseID, provider, content, result, started), Deadline: headerTime + 7200, ManifestSHA256: manifestOf(content)})
	if err != nil {
		t.Fatal(err)
	}
	return it
}

var idPattern = regexp.MustCompile(genesis.IDPattern)

func TestEvidenceIDDerivation(t *testing.T) {
	signer := testKey(t, keyLabel).Address()
	raw := reportBytes(t, keyLabel, "l1", "p1", "c1", report.ResultPass, headerTime)
	sum := sha256.Sum256(raw)
	id := EvidenceID(signer, sum)
	want := hex.EncodeToString(signer[:8]) + "." + hex.EncodeToString(sum[:])[:47]
	if id != want || len(id) != msgkit.IDMaxBytes || !idPattern.MatchString(id) || !strings.HasPrefix(id, evidence.IDPrefix(signer)) {
		t.Fatalf("evidence id %s, want %s", id, want)
	}
	if SuffixHex != 47 {
		t.Fatalf("suffix %d", SuffixHex)
	}
}

func TestBatchAndBusinessIDs(t *testing.T) {
	signer := testKey(t, keyLabel).Address()
	other := testKey(t, "auditor-2").Address()
	ids := []string{"a", "b"}
	b1, err := BatchID(signer, 7, ids)
	if err != nil {
		t.Fatal(err)
	}
	for name, alt := range map[string]func() (string, error){
		"other order":  func() (string, error) { return BatchID(signer, 7, []string{"b", "a"}) },
		"other epoch":  func() (string, error) { return BatchID(signer, 8, ids) },
		"other ids":    func() (string, error) { return BatchID(signer, 7, []string{"a"}) },
		"other signer": func() (string, error) { return BatchID(other, 7, ids) },
	} {
		b2, err := alt()
		if err != nil || b2 == b1 {
			t.Fatalf("%s: same batch id %s (%v)", name, b2, err)
		}
	}
	again, _ := BatchID(signer, 7, ids)
	if again != b1 || len(b1) != 64 || !strings.HasPrefix(b1, evidence.IDPrefix(signer)) {
		t.Fatalf("batch id %s", b1)
	}
	first := BusinessID(signer, b1)
	if first != BusinessID(signer, b1) || first == BusinessID(signer, again+"x") || !idPattern.MatchString(first) ||
		!strings.HasPrefix(first, evidence.IDPrefix(signer)) {
		t.Fatalf("first business id %s", first)
	}
	// A retry business id is drawn, not derived from the batch (security
	// review finding 1): two draws differ, each has the signer's prefix and
	// the id pattern, the id is the prefix and 47 hex characters of the 32
	// bytes read, and a source that ends early is an error.
	r1, err1 := RetryBusinessID(signer, nil)
	r2, err2 := RetryBusinessID(signer, nil)
	if err1 != nil || err2 != nil || r1 == r2 || r1 == first || !idPattern.MatchString(r1) || !strings.HasPrefix(r1, evidence.IDPrefix(signer)) {
		t.Fatalf("retry ids %s %s (%v %v)", r1, r2, err1, err2)
	}
	seq := make([]byte, 32)
	for i := range seq {
		seq[i] = byte(i)
	}
	if got, err := RetryBusinessID(signer, bytes.NewReader(seq)); err != nil || got != evidence.IDPrefix(signer)+hex.EncodeToString(seq)[:SuffixHex] {
		t.Fatalf("retry id from known bytes %s (%v)", got, err)
	}
	if got, err := RetryBusinessID(signer, bytes.NewReader(seq[:31])); err == nil {
		t.Fatalf("a source of 31 bytes gave the retry id %s", got)
	}
}

// PASS reports the lease manifest digest (RECORDED on chain), FAIL the full
// SHA-256 of the report (REJECTED), and INCOMPLETE produces no item.
func TestNewItemDigests(t *testing.T) {
	p := plan(t, keyLabel)
	pass := item(t, p, keyLabel, "l1", "p1", "c1", report.ResultPass, headerTime)
	if pass.Observation != manifestOf("c1") || pass.Result != report.ResultPass {
		t.Fatalf("PASS digest %x", pass.Observation)
	}
	failRaw := reportBytes(t, keyLabel, "l2", "p1", "c2", report.ResultFail, headerTime)
	fail, err := NewItem(p, Audit{Report: failRaw, Deadline: headerTime + 7200, ManifestSHA256: manifestOf("c2")})
	if err != nil {
		t.Fatal(err)
	}
	if fail.Observation != sha256.Sum256(failRaw) || fail.Observation == manifestOf("c2") {
		t.Fatalf("FAIL digest %x", fail.Observation)
	}
	cases := map[string]Audit{
		"INCOMPLETE":               {Report: reportBytes(t, keyLabel, "l3", "p1", "c3", report.ResultIncomplete, headerTime), Deadline: headerTime + 7200, ManifestSHA256: manifestOf("c3")},
		"another auditor's report": {Report: reportBytes(t, "auditor-2", "l1", "p1", "c1", report.ResultPass, headerTime), Deadline: headerTime + 7200, ManifestSHA256: manifestOf("c1")},
		"lease manifest differs":   {Report: reportBytes(t, keyLabel, "l1", "p1", "c1", report.ResultPass, headerTime), Deadline: headerTime + 7200, ManifestSHA256: manifestOf("c9")},
	}
	for name, a := range cases {
		if _, err := NewItem(p, a); !errors.Is(err, ErrIneligible) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	if _, err := NewItem(p, Audit{Report: []byte("{}")}); !errors.Is(err, report.ErrInvalid) {
		t.Fatalf("invalid report: %v", err)
	}
}

// TestNewItemFailNeedsABoundEndpoint (N2): a FAIL report is signed only when
// its endpoint came from a capability manifest bound to the provider record.
// A FAIL report of an operator-configured endpoint, which classify never
// produces, is ErrIneligible; a PASS of one is still eligible (the
// documented asymmetry).
func TestNewItemFailNeedsABoundEndpoint(t *testing.T) {
	p := plan(t, keyLabel)
	audit := func(source, result string) Audit {
		return Audit{Report: reportFrom(t, source, keyLabel, "l1", "p1", "c1", result, headerTime), Deadline: headerTime + 7200, ManifestSHA256: manifestOf("c1")}
	}
	if _, err := NewItem(p, audit(report.EndpointSourceOperatorConfig, report.ResultFail)); !errors.Is(err, ErrIneligible) ||
		!strings.Contains(err.Error(), "endpoint_source") {
		t.Fatalf("operator-config FAIL: %v", err)
	}
	a := audit(report.EndpointSourceCapabilityManifest, report.ResultFail)
	fail, err := NewItem(p, a)
	if err != nil || fail.Result != report.ResultFail || fail.Observation != sha256.Sum256(a.Report) {
		t.Fatalf("capability-manifest FAIL: %+v %v", fail, err)
	}
	pass, err := NewItem(p, audit(report.EndpointSourceOperatorConfig, report.ResultPass))
	if err != nil || pass.Result != report.ResultPass || pass.Observation != manifestOf("c1") {
		t.Fatalf("operator-config PASS: %+v %v", pass, err)
	}
	if _, err := NewItem(p, audit(report.EndpointSourceOperatorConfig, report.ResultIncomplete)); !errors.Is(err, ErrIneligible) {
		t.Fatalf("operator-config INCOMPLETE: %v", err)
	}
}

// Expiry is min(header time + lifetime, lease deadline), never past the
// chain's bound (header time + 86400) or the deadline, and never inside the
// inclusion window.
func TestExpiry(t *testing.T) {
	p := plan(t, keyLabel)
	raw := reportBytes(t, keyLabel, "l1", "p1", "c1", report.ResultPass, headerTime)
	for _, c := range []struct {
		deadline, lifetime, want int64
	}{
		{headerTime + 7200, 86400, headerTime + 7200},
		{headerTime + 200000, 86400, headerTime + 86400},
		{headerTime + 200000, 3600, headerTime + 3600},
		{headerTime + 181, 86400, headerTime + 181},
	} {
		q := p
		q.Window.Lifetime = c.lifetime
		it, err := NewItem(q, Audit{Report: raw, Deadline: c.deadline, ManifestSHA256: manifestOf("c1")})
		if err != nil || it.Expiry != c.want {
			t.Fatalf("deadline %d lifetime %d: expiry %d (%v), want %d", c.deadline, c.lifetime, it.Expiry, err, c.want)
		}
	}
	// guard + inclusion = 180: a deadline 180 s ahead is refused; 181 passes.
	if _, err := NewItem(p, Audit{Report: raw, Deadline: headerTime + 180, ManifestSHA256: manifestOf("c1")}); !errors.Is(err, ErrIneligible) {
		t.Fatalf("expiry inside the inclusion window: %v", err)
	}
	// A lifetime above 86400 is refused by the plan and by checkExpiry.
	q := p
	q.Window.Lifetime = evidence.MaxEvidenceLifetimeSeconds + 1
	if err := q.validate(); err == nil {
		t.Fatal("lifetime above 86400 accepted")
	}
	if err := checkExpiry(p, headerTime+200000, headerTime+86401); !errors.Is(err, ErrIneligible) {
		t.Fatalf("expiry past header time + 86400: %v", err)
	}
	if err := checkExpiry(p, headerTime+7200, headerTime+7201); !errors.Is(err, ErrIneligible) {
		t.Fatalf("expiry past the lease deadline: %v", err)
	}
	if err := checkExpiry(p, headerTime+200000, headerTime+86400); err != nil {
		t.Fatalf("expiry at header time + 86400: %v", err)
	}
}

// RoundOpen at effective-1 and effective: the round closes guard seconds
// before the epoch boundary, judged by the run's header time and by the
// signing host's clock.
func TestRoundOpen(t *testing.T) {
	const guard = 120
	start := int64(evidence.EpochAt(headerTime)) * evidence.EpochSeconds
	end := start + evidence.EpochSeconds - guard
	if err := RoundOpen(end-1, end-1, guard); err != nil {
		t.Fatalf("effective-1: %v", err)
	}
	if err := RoundOpen(end, end, guard); !errors.Is(err, ErrRoundClosing) {
		t.Fatalf("header at the guard: %v", err)
	}
	if err := RoundOpen(start, end, guard); !errors.Is(err, ErrRoundClosing) {
		t.Fatalf("host clock at the guard: %v", err)
	}
	if err := RoundOpen(start, start+evidence.EpochSeconds+5, guard); !errors.Is(err, ErrRoundClosing) {
		t.Fatalf("host clock in the next epoch (expired round): %v", err)
	}
	if err := RoundOpen(start, start, guard); err != nil {
		t.Fatalf("first second of the epoch: %v", err)
	}
}

func TestCheckLease(t *testing.T) {
	ref := LeaseRef{LeaseID: "l1", ProviderID: "p1", ContentID: "c1"}
	window := headerTime + window.Guard + window.Inclusion
	mut := func(f func(l *leasesv1.Lease)) *leasesv1.Lease {
		l := lease("l1", "p1", "c1", headerTime+7200)
		f(l)
		return l
	}
	cases := []struct {
		name string
		l    *leasesv1.Lease
		want classify.Reason
	}{
		{"held", lease("l1", "p1", "c1", headerTime+7200), ""},
		{"renewed", mut(func(l *leasesv1.Lease) { l.State = leasesv1.LeaseState_LEASE_STATE_RENEWED }), ""},
		{"absent", nil, classify.LeaseNotHeld},
		{"another lease id", lease("l2", "p1", "c1", headerTime+7200), classify.LeaseNotHeld},
		{"another provider", lease("l1", "p2", "c1", headerTime+7200), classify.LeaseNotHeld},
		{"ended", mut(func(l *leasesv1.Lease) { l.State = leasesv1.LeaseState_LEASE_STATE_ENDED }), classify.LeaseNotHeld},
		{"expired", mut(func(l *leasesv1.Lease) { l.State = leasesv1.LeaseState_LEASE_STATE_EXPIRED }), classify.LeaseNotHeld},
		{"reservation released", mut(func(l *leasesv1.Lease) { l.ReservationHeld = false }), classify.LeaseNotHeld},
		{"no digest", mut(func(l *leasesv1.Lease) { l.ManifestDigest = nil }), classify.ChainReadFailed},
		{"version 0", mut(func(l *leasesv1.Lease) { l.ContentVersion = 0 }), classify.ChainReadFailed},
		{"deadline at guard + inclusion", mut(func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = window }), classify.LeaseNearDeadline},
		{"deadline one second later", mut(func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = window + 1 }), ""},
		{"deadline passed", mut(func(l *leasesv1.Lease) { l.DeadlineUnixSeconds = headerTime - 1 }), classify.LeaseNearDeadline},
	}
	for _, c := range cases {
		if got := CheckLease(c.l, ref, headerTime, Window{Guard: 120, Inclusion: 60, Lifetime: 86400}); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCheckContent(t *testing.T) {
	l := lease("l1", "p1", "c1", headerTime+7200)
	pending := &contentv1.Content{ContentId: "c1", ContentVersion: 1, State: contentv1.ContentState_CONTENT_STATE_PENDING}
	withdrawn := &contentv1.Content{ContentId: "c1", ContentVersion: 1, State: contentv1.ContentState_CONTENT_STATE_WITHDRAWN}
	cases := []struct {
		name  string
		resp  *contentv1.QueryContentResponse
		found bool
		want  classify.Reason
	}{
		{"pending", &contentv1.QueryContentResponse{Content: pending}, true, ""},
		{"not found", nil, false, classify.InputContentNotFound},
		{"withdrawn", &contentv1.QueryContentResponse{Content: withdrawn}, true, classify.InputContentWithdrawn},
		{"tombstone", &contentv1.QueryContentResponse{Content: pending, Tombstone: &contentv1.Tombstone{ContentId: "c1", ContentVersion: 1}}, true, classify.InputContentWithdrawn},
		{"another version", &contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "c1", ContentVersion: 2}}, true, classify.ChainReadFailed},
	}
	for _, c := range cases {
		if got := CheckContent(c.resp, c.found, l); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestBuildOneBatchPerProvider(t *testing.T) {
	p := plan(t, keyLabel)
	var items []Item
	for i := 0; i < 3; i++ {
		items = append(items, item(t, p, keyLabel, fmt.Sprintf("l%d", i), "p2", fmt.Sprintf("c%d", i), report.ResultPass, headerTime+int64(i)))
	}
	for i := 3; i < 5; i++ {
		items = append(items, item(t, p, keyLabel, fmt.Sprintf("l%d", i), "p1", fmt.Sprintf("c%d", i), report.ResultFail, headerTime+int64(i)))
	}
	batches, err := Build(p, items)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || batches[0].ProviderID != "p1" || batches[1].ProviderID != "p2" || len(batches[0].Items) != 2 || len(batches[1].Items) != 3 {
		t.Fatalf("%d batches", len(batches))
	}
	for _, b := range batches {
		msg := b.Msg
		if msg.GetEpoch() != evidence.EpochAt(headerTime) || msg.GetSchemaVersion() != 1 || msg.GetBusinessId() != BusinessID(p.Signer, msg.GetBatchId()) {
			t.Fatalf("batch fields %+v", msg)
		}
		var ids []string
		for i, it := range msg.GetItems() {
			ids = append(ids, it.GetEvidenceId())
			if i > 0 && ids[i-1] >= ids[i] {
				t.Fatal("items not ordered by evidence id")
			}
			if !it.GetAttestation() || it.GetKind().String() != "EVIDENCE_KIND_AUDITOR_ATTESTATION" {
				t.Fatalf("item %+v", it)
			}
		}
		want, _ := BatchID(p.Signer, msg.GetEpoch(), ids)
		if msg.GetBatchId() != want {
			t.Fatal("batch id is not derived from the ordered evidence ids")
		}
	}
	// Above MaxItems a provider's items split into batches of at most p.MaxItems.
	q := p
	q.MaxItems = 2
	split, err := Build(q, items)
	if err != nil || len(split) != 3 {
		t.Fatalf("%d batches (%v)", len(split), err)
	}
	if _, err := Build(p, append(items, items[0])); !errors.Is(err, ErrIneligible) {
		t.Fatalf("duplicate evidence id: %v", err)
	}
	q.MaxItems = MaxItems + 1
	if _, err := Build(q, items); err == nil {
		t.Fatal("max items above 32 accepted")
	}
}

func signedBatch(t *testing.T) (*Batch, keys.Key) {
	t.Helper()
	p := plan(t, keyLabel)
	key := testKey(t, keyLabel)
	items := []Item{
		item(t, p, keyLabel, "l1", "p1", "c1", report.ResultPass, headerTime),
		item(t, p, keyLabel, "l2", "p1", "c2", report.ResultFail, headerTime),
	}
	batches, err := Build(p, items)
	if err != nil {
		t.Fatal(err)
	}
	if err := Sign(batches[0], key); err != nil {
		t.Fatal(err)
	}
	return batches[0], key
}

func TestSignVerifiesWithTheChainCheck(t *testing.T) {
	b, _ := signedBatch(t)
	if code := evidence.VerifyAuditSignature(b.Msg); code != msgkit.OK {
		t.Fatalf("verify: %s", code)
	}
	// Any change after signing fails the chain's check (D-1).
	changes := map[string]func(m *Batch){
		"expiry":       func(m *Batch) { m.Msg.Items[0].ExpiryUnixSeconds++ },
		"digest":       func(m *Batch) { m.Msg.Items[1].ObservationDigest.Value[0] ^= 1 },
		"lease":        func(m *Batch) { m.Msg.Items[0].LeaseId = "l9" },
		"order":        func(m *Batch) { m.Msg.Items[0], m.Msg.Items[1] = m.Msg.Items[1], m.Msg.Items[0] },
		"epoch":        func(m *Batch) { m.Msg.Epoch++ },
		"batch id":     func(m *Batch) { m.Msg.BatchId = m.Msg.BatchId[:63] + "x" },
		"drop an item": func(m *Batch) { m.Msg.Items = m.Msg.Items[:1] },
	}
	for name, change := range changes {
		c, _ := signedBatch(t)
		change(c)
		if code := evidence.VerifyAuditSignature(c.Msg); code != msgkit.Unauthorized {
			t.Fatalf("%s: %s", name, code)
		}
	}
	// A key of another address cannot sign the batch.
	c, _ := signedBatch(t)
	if err := Sign(c, testKey(t, "auditor-2")); !errors.Is(err, ErrSignature) {
		t.Fatalf("foreign key: %v", err)
	}
	// The business id is not signed: a new attempt keeps the signature valid.
	rid, err := RetryBusinessID(b.Msg.GetSigner(), nil)
	if err != nil {
		t.Fatal(err)
	}
	retry := WithBusinessID(b.Msg, rid)
	if retry.GetBusinessId() != rid || retry.GetBusinessId() == b.Msg.GetBusinessId() || evidence.VerifyAuditSignature(retry) != msgkit.OK {
		t.Fatal("new business attempt broke the signature or kept the business id")
	}
	if b.Msg.GetBusinessId() != BusinessID(b.Msg.GetSigner(), b.ID()) {
		t.Fatal("WithBusinessID changed the original")
	}
}

func TestEnvelopesAreTheSignedEnvelopes(t *testing.T) {
	b, _ := signedBatch(t)
	texts, err := Envelopes(b.Msg)
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := evidence.AuditCanonical(b.Msg)
	if err != nil {
		t.Fatal(err)
	}
	joined := `"envelopes":[` + string(bytes.Join(texts, []byte(","))) + `]`
	if !strings.Contains(canonical, joined) {
		t.Fatal("envelope texts are not the signed payload's envelopes")
	}
	for i, text := range texts {
		env, _ := evidence.AuditEnvelope(b.Msg, i)
		want, _ := msgkit.Canonical(env)
		if string(text) != want {
			t.Fatalf("envelope %d", i)
		}
	}
}

func TestPersistAndLoad(t *testing.T) {
	b, _ := signedBatch(t)
	st, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	if err := Persist(st, b, "run-1"); err != nil {
		t.Fatal(err)
	}
	if err := Persist(st, b, "run-1"); err != nil {
		t.Fatalf("persist is not idempotent: %v", err)
	}
	loaded, summary, err := Load(st, b.ID())
	if err != nil {
		t.Fatal(err)
	}
	if !proto.Equal(loaded.Msg, b.Msg) || summary.RunID != "run-1" || len(loaded.Items) != 2 || loaded.Items[1].ReportSHA256 != b.Items[1].ReportSHA256 {
		t.Fatal("loaded batch differs")
	}
	for _, it := range b.Msg.GetItems() {
		if _, err := st.Get(store.Envelopes, it.GetEvidenceId(), ".json"); err != nil {
			t.Fatalf("envelope %s: %v", it.GetEvidenceId(), err)
		}
	}
	// An unsigned batch is never persisted.
	unsigned := proto.Clone(b.Msg).(*evidencev1.MsgSubmitAuditBatch)
	unsigned.AuditorSignature.Signature = nil
	if err := Persist(st, &Batch{ProviderID: "p1", Items: b.Items, Msg: unsigned}, "run-1"); !errors.Is(err, ErrSignature) {
		t.Fatalf("unsigned persisted: %v", err)
	}
	if _, _, err := Load(st, "79b70b8d1f8f5606.missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("missing batch: %v", err)
	}
}

func TestRebuild(t *testing.T) {
	b, key := signedBatch(t)
	drop := b.Items[0].EvidenceID
	nb, dropped, err := Rebuild(b, func(it Item) bool { return it.EvidenceID != drop })
	if err != nil {
		t.Fatal(err)
	}
	if len(dropped) != 1 || dropped[0].EvidenceID != drop || len(nb.Items) != 1 || nb.Supersedes != b.ID() || nb.ID() == b.ID() {
		t.Fatalf("rebuild %+v", nb)
	}
	if nb.Epoch() != b.Epoch() || nb.Msg.GetItems()[0].GetExpiryUnixSeconds() != b.Msg.GetItems()[1].GetExpiryUnixSeconds() {
		t.Fatal("rebuild changed the epoch or an expiry")
	}
	if err := Sign(nb, key); err != nil || evidence.VerifyAuditSignature(nb.Msg) != msgkit.OK {
		t.Fatalf("rebuilt batch does not sign: %v", err)
	}
	none, dropped, err := Rebuild(b, func(Item) bool { return false })
	if err != nil || none != nil || len(dropped) != 2 {
		t.Fatalf("all dropped: %v %v", none, err)
	}
}

// TestCheckFailSource (N2): the paths that re-sign or broadcast stored items
// without NewItem read each FAIL item's stored report. A FAIL item passes
// only when its report is stored, hashes to its digest, parses, is FAIL, and
// says CAPABILITY_MANIFEST; the error names the evidence id. An item whose
// observation is its report digest counts as FAIL whatever its summary
// result says. A PASS item is not read.
func TestCheckFailSource(t *testing.T) {
	p := plan(t, keyLabel)
	st, err := store.Open(filepath.Join(t.TempDir(), "store"))
	if err != nil {
		t.Fatal(err)
	}
	// stored builds the item NewItem would build for raw, without its
	// endpoint check, and stores raw unless missing.
	stored := func(raw []byte, result string, missing bool) Item {
		t.Helper()
		sum := sha256.Sum256(raw)
		if !missing {
			if _, err := st.PutReport(raw); err != nil {
				t.Fatal(err)
			}
		}
		it := Item{EvidenceID: EvidenceID(p.Signer, sum), ProviderID: "p1", LeaseID: "l1", ContentID: "c1", ContentVersion: 1,
			Result: result, ReportSHA256: sum, Observation: sum, Expiry: headerTime + 7200}
		if result == report.ResultPass {
			it.Observation = manifestOf("c1")
		}
		return it
	}
	manifestFail := reportFrom(t, report.EndpointSourceCapabilityManifest, keyLabel, "l1", "p1", "c1", report.ResultFail, headerTime)
	operatorFail := reportFrom(t, report.EndpointSourceOperatorConfig, keyLabel, "l1", "p1", "c1", report.ResultFail, headerTime+1)
	operatorPass := reportFrom(t, report.EndpointSourceOperatorConfig, keyLabel, "l1", "p1", "c1", report.ResultPass, headerTime+2)
	manifestIncomplete := reportFrom(t, report.EndpointSourceCapabilityManifest, keyLabel, "l1", "p1", "c1", report.ResultIncomplete, headerTime+3)
	// The store is shared by the cases, so a report that must be missing
	// has bytes no other case stores.
	unstoredFail := reportFrom(t, report.EndpointSourceCapabilityManifest, keyLabel, "l1", "p1", "c1", report.ResultFail, headerTime+5)
	unstoredPass := reportFrom(t, report.EndpointSourceOperatorConfig, keyLabel, "l1", "p1", "c1", report.ResultPass, headerTime+6)
	field := `"endpoint_source":"CAPABILITY_MANIFEST",`
	if bytes.Count(manifestFail, []byte(field)) != 1 {
		t.Fatalf("report text has no %s", field)
	}
	// A report written before the field existed no longer parses.
	beforeN2 := bytes.Replace(reportFrom(t, report.EndpointSourceCapabilityManifest, keyLabel, "l1", "p1", "c1", report.ResultFail, headerTime+4), []byte(field), nil, 1)
	relabelled := stored(operatorFail, report.ResultPass, false)
	relabelled.Observation = relabelled.ReportSHA256
	for name, c := range map[string]struct {
		it   Item
		want string
	}{
		"manifest FAIL":                     {stored(manifestFail, report.ResultFail, false), ""},
		"operator PASS":                     {stored(operatorPass, report.ResultPass, false), ""},
		"PASS whose report is not stored":   {stored(unstoredPass, report.ResultPass, true), ""},
		"operator FAIL":                     {stored(operatorFail, report.ResultFail, false), "endpoint_source OPERATOR_CONFIG"},
		"FAIL whose report is not stored":   {stored(unstoredFail, report.ResultFail, true), "report"},
		"FAIL report written before N2":     {stored(beforeN2, report.ResultFail, false), "report"},
		"FAIL item of an INCOMPLETE report": {stored(manifestIncomplete, report.ResultFail, false), "report result INCOMPLETE"},
		"operator FAIL labelled PASS":       {relabelled, "endpoint_source OPERATOR_CONFIG"},
	} {
		err := CheckFailSource(st, c.it)
		if c.want == "" {
			if err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			continue
		}
		if !errors.Is(err, ErrIneligible) || !strings.Contains(err.Error(), c.it.EvidenceID) || !strings.Contains(err.Error(), c.want) {
			t.Fatalf("%s: %v", name, err)
		}
	}
	ok := &Batch{Items: []Item{stored(manifestFail, report.ResultFail, false), stored(operatorPass, report.ResultPass, false)}}
	if err := CheckFailSources(st, ok); err != nil {
		t.Fatalf("batch of a manifest FAIL and an operator PASS: %v", err)
	}
	bad := stored(operatorFail, report.ResultFail, false)
	ok.Items = append(ok.Items, bad)
	if err := CheckFailSources(st, ok); !errors.Is(err, ErrIneligible) || !strings.Contains(err.Error(), bad.EvidenceID) {
		t.Fatalf("batch with an operator FAIL: %v", err)
	}
}

func TestRunFormat(t *testing.T) {
	run := Run{
		AuditorAddress: hex.EncodeToString(testKey(t, keyLabel).Address()), AuditorKeyID: "key-auditor-1",
		ChainID: "svn-devnet-1", HeaderHeight: "42", HeaderTime: headerTime, StartedUnixSeconds: headerTime + 3,
		Audits: []RunAudit{
			{LeaseID: "l1", ProviderID: "p1", ContentID: "c1", ContentVersion: 1, ManifestSHA256: strings.Repeat("ab", 32), Deadline: headerTime + 7200, Result: "PASS", Reasons: []string{}, ReportSHA256: strings.Repeat("cd", 32)},
			{LeaseID: "l2", ProviderID: "p1", ContentID: "c2", Result: "INCOMPLETE", Reasons: []string{"LEASE_NOT_HELD"}},
		},
	}
	raw, id, err := EncodeRun(run)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ParseRun(raw, id)
	if err != nil || got.Audits[0].ReportSHA256 != run.Audits[0].ReportSHA256 || !strings.HasPrefix(id, "run-") || len(id) != 52 {
		t.Fatalf("round trip: %v %s", err, id)
	}
	if _, err := ParseRun(raw, "run-"+strings.Repeat("0", 48)); !errors.Is(err, ErrFormat) {
		t.Fatalf("run id of other bytes: %v", err)
	}
	extra := bytes.Replace(raw, []byte(`"audits":`), []byte(`"debug":true,"audits":`), 1)
	if _, err := ParseRun(extra, id); !errors.Is(err, ErrFormat) {
		t.Fatalf("unknown key: %v", err)
	}
	bad := []func(r *Run){
		func(r *Run) { r.Audits[0].Reasons = []string{"X"} },
		func(r *Run) { r.Audits[1].ReportSHA256 = strings.Repeat("ef", 32) },
		func(r *Run) { r.Audits[0].ReportSHA256 = "" },
		func(r *Run) { r.Audits[0].ContentVersion = 0 },
		func(r *Run) { r.Audits[1].Reasons = []string{"B", "A"} },
		func(r *Run) { r.HeaderHeight = "042" },
		func(r *Run) { r.AuditorAddress = "79B7" },
		func(r *Run) { r.Audits = nil },
	}
	for i, change := range bad {
		r := run
		r.Audits = append([]RunAudit(nil), run.Audits...)
		change(&r)
		if _, _, err := EncodeRun(r); !errors.Is(err, ErrFormat) {
			t.Fatalf("case %d: %v", i, err)
		}
	}
}

func TestSummaryFormat(t *testing.T) {
	b, _ := signedBatch(t)
	raw, err := EncodeSummary(SummaryOf(b, "run-1"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := ParseSummary(raw)
	if err != nil || s.BatchID != b.ID() || len(s.Items) != 2 {
		t.Fatalf("%v", err)
	}
	if it, ok := s.Item(b.Items[1].EvidenceID); !ok || it.ReportSHA256 != hex.EncodeToString(b.Items[1].ReportSHA256[:]) {
		t.Fatal("summary item lookup")
	}
	if _, err := ParseSummary(append(raw, ' ')); !errors.Is(err, ErrFormat) {
		t.Fatalf("non-canonical: %v", err)
	}
	// A summary whose items do not describe the message is refused by Load.
	s.Items[0].Expiry++
	if _, err := fromSummary(b.Msg, s); !errors.Is(err, ErrFormat) {
		t.Fatalf("summary mismatch: %v", err)
	}
}
