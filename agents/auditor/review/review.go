// Package review is the reviewer's verification of one evidence id
// against the producer's off-chain store (plan section 16.9).
//
// The record comes from one of two sources, reported first:
//
//   - NODE <url>: read through an unverified ABCI query; it trusts that node
//   - FILE <path>: a record file; it was not read from the chain
//
// The checks run in this order and the first failure is the verdict:
//
//  1. NOT_ON_CHAIN (NODE only): the node holds no record for the id
//  2. PAYLOAD_UNAVAILABLE: no stored batch summary or index line names the
//     id's report, or the report, the signed batch, or the envelope is
//     missing or unreadable
//  3. CHECKSUM_MISMATCH: the stored report's bytes do not hash to their
//     name, the report's SHA-256 does not start with the id's 47-character
//     suffix, or a REJECTED record's digest is not the report's SHA-256
//  4. REPORT_INVALID: the report is not a canonical
//     svn-audit-report-proposed-v0 report
//  5. SIGNATURE_INVALID: the stored batch does not verify
//     (evidence.VerifyAuditSignature), names another batch id, or its key
//     is not the record's signer_pubkey
//  6. ENVELOPE_MISMATCH: the stored envelope differs from
//     evidence.AuditEnvelope of the stored batch, or from the envelope
//     derived from the chain record
//  7. RECORD_MISMATCH: lease, content, version, kind, digest, epoch,
//     expiry, or result differ between the batch or report and the record,
//     or the report is one the producer never attests: an INCOMPLETE
//     report, or a FAIL report whose endpoint_source is not
//     CAPABILITY_MANIFEST (N2: an endpoint the provider account did not
//     declare)
//  8. CONSISTENT
//
// CONSISTENT means the stored payload is the one an admitted key signed
// and the chain recorded. It is still an attestation: a valid signature
// and a consistent payload do not make an observation true, and a
// dishonest auditor can keep a self-consistent false report.
package review

import (
	"bytes"
	"encoding/hex"
	"errors"
	"fmt"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/batch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/chainclient"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/labels"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/store"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// Verdict is the result of a review.
type Verdict string

// Verdicts, in check order.
const (
	NotOnChain         Verdict = "NOT_ON_CHAIN"
	PayloadUnavailable Verdict = "PAYLOAD_UNAVAILABLE"
	ChecksumMismatch   Verdict = "CHECKSUM_MISMATCH"
	ReportInvalid      Verdict = "REPORT_INVALID"
	SignatureInvalid   Verdict = "SIGNATURE_INVALID"
	EnvelopeMismatch   Verdict = "ENVELOPE_MISMATCH"
	RecordMismatch     Verdict = "RECORD_MISMATCH"
	Consistent         Verdict = "CONSISTENT"
)

// Source kinds.
const (
	SourceNode = "NODE"
	SourceFile = "FILE"
)

// Source says where the record came from.
type Source struct {
	Kind     string
	Location string
}

// String is "NODE <url>" or "FILE <path>".
func (s Source) String() string { return s.Kind + " " + s.Location }

// Label says what the source does and does not show.
func (s Source) Label() string {
	if s.Kind == SourceNode {
		return "read through an unverified ABCI query; trusts that node"
	}
	return "not read from the chain"
}

// Result is a review result.
type Result struct {
	RecordSource      string  `json:"record_source"`
	RecordSourceLabel string  `json:"record_source_label"`
	EvidenceID        string  `json:"evidence_id"`
	Verdict           Verdict `json:"verdict"`
	Detail            string  `json:"detail"`
	// State is the record's reported state, "" when there is no record.
	State string `json:"state"`
	// ReportResult is the stored report's result, "" when not read.
	ReportResult string `json:"report_result"`
	// ReportEndpointSource is the stored report's endpoint_source
	// (CAPABILITY_MANIFEST or OPERATOR_CONFIG), "" when not read. A PASS
	// from OPERATOR_CONFIG reviews CONSISTENT: its endpoint was not bound to
	// the provider.
	ReportEndpointSource string `json:"report_endpoint_source"`
	ClaimLimit           string `json:"claim_limit"`
	// ModeLabel and StateLabel come from the label catalog for the record's
	// kind and reported state; Limitations are the catalog's limitations.
	ModeLabel   string   `json:"mode_label"`
	StateLabel  string   `json:"state_label"`
	Limitations []string `json:"limitations"`
}

// attachLabels fills the label fields from the catalog. A kind or state the
// catalog does not name leaves its label empty.
func (res *Result) attachLabels(c *labels.Catalog, rec *evidencev1.EvidenceRecord) {
	if c == nil {
		return
	}
	if e, err := c.Mode(evidence.KindName(rec.GetKind())); err == nil {
		res.ModeLabel = e.Label
	}
	if e, err := c.State(evidence.StateName(rec.GetState())); err == nil {
		res.StateLabel = e.Label
	}
}

// Input is one review.
type Input struct {
	EvidenceID string
	Source     Source
	// Record is the record; Found is false when a NODE answered that no
	// record holds the id. A FILE source always has a record.
	Record *evidencev1.EvidenceRecord
	Found  bool
	Store  *store.Store
	// Labels is the label catalog; the result carries its labels and
	// limitations.
	Labels *labels.Catalog
}

// ErrInput marks an input that cannot be reviewed.
var ErrInput = errors.New("review: invalid input")

// payload is what the store holds for one evidence id.
type payload struct {
	reportSHA [32]byte
	report    []byte
	corrupt   bool
	msg       []byte
	envelope  []byte
}

// Review runs the checks.
func Review(in Input) (Result, error) {
	res := Result{
		RecordSource: in.Source.String(), RecordSourceLabel: in.Source.Label(),
		EvidenceID: in.EvidenceID, ClaimLimit: evidence.ClaimLimit, Limitations: []string{},
	}
	if in.Labels != nil {
		res.Limitations = append(res.Limitations, in.Labels.Limitations...)
	}
	switch in.Source.Kind {
	case SourceNode:
	case SourceFile:
		if !in.Found || in.Record == nil {
			return Result{}, fmt.Errorf("%w: a record file must hold a record", ErrInput)
		}
	default:
		return Result{}, fmt.Errorf("%w: unknown source", ErrInput)
	}
	if !msgkit.ValidID(in.EvidenceID) || in.Store == nil {
		return Result{}, fmt.Errorf("%w: evidence id or store", ErrInput)
	}
	if !in.Found || in.Record == nil {
		return verdict(res, NotOnChain, "the node holds no record for this evidence id"), nil
	}
	rec := in.Record
	if rec.GetEvidenceId() != in.EvidenceID {
		return Result{}, fmt.Errorf("%w: the record names another evidence id", ErrInput)
	}
	res.State = evidence.StateName(rec.GetState())
	res.attachLabels(in.Labels, rec)
	p, detail := load(in.Store, rec)
	if detail != "" {
		return verdict(res, PayloadUnavailable, detail), nil
	}
	if v, d := checksum(in.EvidenceID, rec, p); v != "" {
		return verdict(res, v, d), nil
	}
	r, err := report.Parse(p.report)
	if err != nil {
		return verdict(res, ReportInvalid, "the stored report is not a canonical report"), nil
	}
	res.ReportResult, res.ReportEndpointSource = r.Result, r.EndpointSource
	msg := &evidencev1.MsgSubmitAuditBatch{}
	if err := proto.Unmarshal(p.msg, msg); err != nil {
		return verdict(res, SignatureInvalid, "the stored batch does not decode"), nil
	}
	if msg.GetBatchId() != rec.GetBatchId() {
		return verdict(res, SignatureInvalid, "the stored batch names another batch id"), nil
	}
	if code := evidence.VerifyAuditSignature(msg); code != msgkit.OK {
		return verdict(res, SignatureInvalid, "the stored batch does not verify: "+string(code)), nil
	}
	if !bytes.Equal(msg.GetAuditorSignature().GetPublicKey(), rec.GetSignerPubkey()) {
		return verdict(res, SignatureInvalid, "the stored batch's key is not the record's signer_pubkey"), nil
	}
	index := -1
	for i, it := range msg.GetItems() {
		if it.GetEvidenceId() == in.EvidenceID {
			index = i
		}
	}
	if v, d := envelopes(msg, index, rec, p.envelope); v != "" {
		return verdict(res, v, d), nil
	}
	if d := recordMismatch(msg.GetItems()[index], r, rec, p.reportSHA); d != "" {
		return verdict(res, RecordMismatch, d), nil
	}
	return verdict(res, Consistent, "the stored payload is the signed and recorded one; it is an attestation, not proof"), nil
}

func verdict(res Result, v Verdict, detail string) Result {
	res.Verdict, res.Detail = v, detail
	return res
}

// reportSHA finds the full report SHA-256 of an evidence id: from the
// stored summary of the record's batch, else from an index line. A
// corrupt or unreadable index is an error.
func reportSHA(st *store.Store, rec *evidencev1.EvidenceRecord) ([32]byte, bool, error) {
	var sum [32]byte
	fromHex := func(text string) bool {
		raw, err := hex.DecodeString(text)
		if err != nil || len(raw) != len(sum) {
			return false
		}
		copy(sum[:], raw)
		return true
	}
	if raw, err := st.Get(store.Batches, rec.GetBatchId(), ".json"); err == nil {
		if s, err := batch.ParseSummary(raw); err == nil {
			if it, ok := s.Item(rec.GetEvidenceId()); ok && fromHex(it.ReportSHA256) {
				return sum, true, nil
			}
		}
	}
	found := false
	err := st.ScanIndex(func(line []byte) error {
		l, err := chainclient.ParseIndexLine(line)
		if err != nil {
			return err
		}
		if !found && l.EvidenceID == rec.GetEvidenceId() && fromHex(l.ReportSHA256) {
			found = true
		}
		return nil
	})
	return sum, found, err
}

// load reads the stored payload of a record. A non-empty detail means
// PAYLOAD_UNAVAILABLE.
func load(st *store.Store, rec *evidencev1.EvidenceRecord) (payload, string) {
	var p payload
	sum, ok, err := reportSHA(st, rec)
	if err != nil {
		return p, "the index is corrupt or unreadable"
	}
	if !ok {
		return p, "no stored batch summary or index line names this evidence id's report"
	}
	p.reportSHA = sum
	raw, err := st.Report(sum)
	switch {
	case errors.Is(err, store.ErrCorrupt):
		p.corrupt = true
	case err != nil:
		return p, "the report is missing or unreadable"
	default:
		p.report = raw
	}
	if !msgkit.ValidID(rec.GetBatchId()) {
		return p, "the record's batch id is outside the id pattern"
	}
	if p.msg, err = st.Get(store.Batches, rec.GetBatchId(), ".pb"); err != nil {
		return p, "the signed batch is missing or unreadable"
	}
	if p.envelope, err = st.Get(store.Envelopes, rec.GetEvidenceId(), ".json"); err != nil {
		return p, "the envelope is missing or unreadable"
	}
	return p, ""
}

func checksum(evidenceID string, rec *evidencev1.EvidenceRecord, p payload) (Verdict, string) {
	if p.corrupt {
		return ChecksumMismatch, "the stored report's bytes do not hash to its name"
	}
	prefixLen := 2*evidence.IDPrefixBytes + 1
	if len(evidenceID) != prefixLen+batch.SuffixHex || hex.EncodeToString(p.reportSHA[:])[:batch.SuffixHex] != evidenceID[prefixLen:] {
		return ChecksumMismatch, "the report's SHA-256 does not start with the evidence id's suffix"
	}
	if rec.GetState() == evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED && !bytes.Equal(rec.GetObservationDigest().GetValue(), p.reportSHA[:]) {
		return ChecksumMismatch, "the REJECTED record's digest is not the report's SHA-256"
	}
	return "", ""
}

// envelopes compares the stored envelope with the stored batch's envelope
// and with the envelope derived from the record.
func envelopes(msg *evidencev1.MsgSubmitAuditBatch, index int, rec *evidencev1.EvidenceRecord, stored []byte) (Verdict, string) {
	if index < 0 {
		return EnvelopeMismatch, "the stored batch has no item with this evidence id"
	}
	fromBatch, err := envelopeText(msg, index)
	if err != nil || !bytes.Equal(fromBatch, stored) {
		return EnvelopeMismatch, "the stored envelope differs from the signed batch's envelope"
	}
	derived := &evidencev1.MsgSubmitAuditBatch{
		Signer:        evidence.AuditorAddress(rec.GetSignerPubkey()),
		BatchId:       rec.GetBatchId(),
		Epoch:         rec.GetEpoch(),
		SchemaVersion: rec.GetSchemaVersion(),
		Items: []*evidencev1.EvidenceItem{{
			EvidenceId: rec.GetEvidenceId(), Kind: rec.GetKind(), LeaseId: rec.GetLeaseId(),
			ContentId: rec.GetContentId(), ContentVersion: rec.GetContentVersion(),
			ObservationDigest: rec.GetObservationDigest(), ExpiryUnixSeconds: rec.GetExpiryUnixSeconds(),
			Attestation: rec.GetAttestation(),
		}},
		// The record keeps no key label; the signed batch's is used.
		AuditorSignature: msg.GetAuditorSignature(),
	}
	fromRecord, err := envelopeText(derived, 0)
	if err != nil || !bytes.Equal(fromRecord, stored) {
		return EnvelopeMismatch, "the stored envelope differs from the envelope of the chain record"
	}
	return "", ""
}

func envelopeText(msg *evidencev1.MsgSubmitAuditBatch, i int) ([]byte, error) {
	env, err := evidence.AuditEnvelope(msg, i)
	if err != nil {
		return nil, err
	}
	text, err := msgkit.Canonical(env)
	return []byte(text), err
}

// recordMismatch compares the batch item and the report with the record.
func recordMismatch(it *evidencev1.EvidenceItem, r report.Report, rec *evidencev1.EvidenceRecord, sum [32]byte) string {
	if it.GetLeaseId() != rec.GetLeaseId() || r.LeaseID != rec.GetLeaseId() {
		return "lease id differs"
	}
	if it.GetContentId() != rec.GetContentId() || r.ContentID != rec.GetContentId() ||
		it.GetContentVersion() != rec.GetContentVersion() || r.ContentVersion != rec.GetContentVersion() {
		return "content id or version differs"
	}
	if it.GetKind() != rec.GetKind() {
		return "kind differs"
	}
	d := rec.GetObservationDigest()
	if !bytes.Equal(it.GetObservationDigest().GetValue(), d.GetValue()) || it.GetObservationDigest().GetAlgorithm() != d.GetAlgorithm() {
		return "digest differs"
	}
	if it.GetExpiryUnixSeconds() != rec.GetExpiryUnixSeconds() {
		return "expiry differs"
	}
	switch r.Result {
	case report.ResultPass:
		if !bytes.Equal(d.GetValue(), r.ExpectedManifestSHA256[:]) || rec.GetState() == evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED {
			return "a PASS report needs a RECORDED or EXPIRED record of the manifest digest"
		}
	case report.ResultFail:
		if !bytes.Equal(d.GetValue(), sum[:]) || rec.GetState() != evidencev1.EvidenceState_EVIDENCE_STATE_REJECTED {
			return "a FAIL report needs a REJECTED record of the report's SHA-256"
		}
		if r.EndpointSource != report.EndpointSourceCapabilityManifest {
			return "a FAIL report whose endpoint_source is not " + report.EndpointSourceCapabilityManifest +
				" is never attested: the endpoint was not declared by the provider account" +
				" (no capability manifest bound to its committed endpoint_manifest_digest)"
		}
	default:
		return "an INCOMPLETE report is never attested"
	}
	return ""
}
