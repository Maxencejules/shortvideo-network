package batch

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strconv"

	"google.golang.org/protobuf/proto"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/store"
	"github.com/Maxencejules/shortvideo-network/chain/genesis"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// Stored formats (interim, DR-09-22). Every file is SVN-JCS-1 text; a
// parser re-encodes what it read and requires the same bytes, so a file
// holds exactly the format's keys.
const (
	// RunFormat is the pending run written by `run` and read by `sign`.
	RunFormat = "svn-audit-run-proposed-v0"
	// SummaryFormat is batches/<batch_id>.json, written by `sign`.
	SummaryFormat = "svn-audit-batch-proposed-v0"
	// MaxRunAudits bounds the audits of one run (config MaxAudits).
	MaxRunAudits = 256
	maxChainID   = 64
)

// ErrFormat wraps every parse failure of a stored file.
var ErrFormat = errors.New("batch: stored file is not in its format")

var (
	hex64     = regexp.MustCompile(`^[0-9a-f]{64}$`)
	hex40     = regexp.MustCompile(`^[0-9a-f]{40}$`)
	chainIDRe = regexp.MustCompile(`^[a-zA-Z0-9._-]{1,64}$`)
	reasonRe  = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

// RunAudit is one audit of a run.
type RunAudit struct {
	LeaseID    string `json:"lease_id"`
	ProviderID string `json:"provider_id"`
	ContentID  string `json:"content_id"`
	// ContentVersion, ManifestSHA256, and Deadline are the lease values the
	// run read; 0, "", and 0 when no held lease was read.
	ContentVersion int64  `json:"content_version"`
	ManifestSHA256 string `json:"manifest_sha256"`
	Deadline       int64  `json:"deadline_unix_seconds"`
	Result         string `json:"result"`
	// Reasons are sorted and unique; empty for PASS.
	Reasons []string `json:"reasons"`
	// ReportSHA256 is the full SHA-256 of the stored report, or "" when no
	// report was written (no held lease was read, so nothing could be
	// described).
	ReportSHA256 string `json:"report_sha256"`
}

// Run is a pending run.
type Run struct {
	Format         string `json:"format"`
	AuditorAddress string `json:"auditor_address"`
	AuditorKeyID   string `json:"auditor_key_id"`
	ChainID        string `json:"chain_id"`
	// HeaderHeight and HeaderTime are the latest committed header the run
	// read before any request: the epoch, expiry, and deadline source.
	HeaderHeight string `json:"header_height"`
	HeaderTime   int64  `json:"header_unix_seconds"`
	// StartedUnixSeconds is the auditor host clock (unverified).
	StartedUnixSeconds int64      `json:"started_unix_seconds"`
	Audits             []RunAudit `json:"audits"`
}

func (r Run) object() map[string]any {
	audits := make([]any, 0, len(r.Audits))
	for _, a := range r.Audits {
		audits = append(audits, map[string]any{
			"lease_id": a.LeaseID, "provider_id": a.ProviderID, "content_id": a.ContentID,
			"content_version": a.ContentVersion, "manifest_sha256": a.ManifestSHA256,
			"deadline_unix_seconds": a.Deadline, "result": a.Result, "reasons": anyStrings(a.Reasons),
			"report_sha256": a.ReportSHA256,
		})
	}
	return map[string]any{
		"format": r.Format, "auditor_address": r.AuditorAddress, "auditor_key_id": r.AuditorKeyID,
		"chain_id": r.ChainID, "header_height": r.HeaderHeight, "header_unix_seconds": r.HeaderTime,
		"started_unix_seconds": r.StartedUnixSeconds, "audits": audits,
	}
}

func anyStrings(list []string) []any {
	out := make([]any, 0, len(list))
	for _, s := range list {
		out = append(out, s)
	}
	return out
}

func timeOK(t int64) bool { return t >= genesis.UnixSecondsMin && t <= genesis.UnixSecondsMax }

func (r Run) validate() error {
	if r.Format != RunFormat || !hex40.MatchString(r.AuditorAddress) || !msgkit.ValidID(r.AuditorKeyID) ||
		!chainIDRe.MatchString(r.ChainID) || !timeOK(r.HeaderTime) || !timeOK(r.StartedUnixSeconds) {
		return fmt.Errorf("%w: run header fields", ErrFormat)
	}
	if h, err := strconv.ParseUint(r.HeaderHeight, 10, 63); err != nil || h == 0 || strconv.FormatUint(h, 10) != r.HeaderHeight {
		return fmt.Errorf("%w: header_height", ErrFormat)
	}
	if len(r.Audits) < 1 || len(r.Audits) > MaxRunAudits {
		return fmt.Errorf("%w: 1 to %d audits", ErrFormat, MaxRunAudits)
	}
	for i, a := range r.Audits {
		if err := a.validate(); err != nil {
			return fmt.Errorf("%w: audits[%d]: %v", ErrFormat, i, err)
		}
	}
	return nil
}

func (a RunAudit) validate() error {
	for _, id := range []string{a.LeaseID, a.ProviderID, a.ContentID} {
		if !msgkit.ValidID(id) {
			return errors.New("id outside the pattern")
		}
	}
	read := a.ContentVersion != 0
	if read != (a.ManifestSHA256 != "") || read != (a.Deadline != 0) {
		return errors.New("lease fields are all set or all empty")
	}
	if read && (a.ContentVersion < 1 || a.ContentVersion > genesis.SafeIntegerMax || !hex64.MatchString(a.ManifestSHA256) || !timeOK(a.Deadline)) {
		return errors.New("lease fields out of range")
	}
	if a.ReportSHA256 != "" && (!hex64.MatchString(a.ReportSHA256) || !read) {
		return errors.New("report_sha256 needs the lease fields")
	}
	for i, r := range a.Reasons {
		if !reasonRe.MatchString(r) || (i > 0 && a.Reasons[i-1] >= r) {
			return errors.New("reasons must be sorted, unique, and upper-case")
		}
	}
	switch a.Result {
	case report.ResultPass:
		if len(a.Reasons) != 0 || a.ReportSHA256 == "" {
			return errors.New("a PASS audit has a report and no reason")
		}
	case report.ResultFail:
		if len(a.Reasons) == 0 || a.ReportSHA256 == "" {
			return errors.New("a FAIL audit has a report and a reason")
		}
	case report.ResultIncomplete:
		if len(a.Reasons) == 0 {
			return errors.New("an INCOMPLETE audit has a reason")
		}
	default:
		return errors.New("unknown result")
	}
	return nil
}

// EncodeRun validates r and returns its SVN-JCS-1 text and run id
// ("run-" and 48 hex characters of the text's SHA-256).
func EncodeRun(r Run) ([]byte, string, error) {
	r.Format = RunFormat
	if err := r.validate(); err != nil {
		return nil, "", err
	}
	text, err := jcs.Canonicalize(r.object())
	if err != nil {
		return nil, "", fmt.Errorf("%w: %v", ErrFormat, err)
	}
	sum := sha256.Sum256([]byte(text))
	return []byte(text), "run-" + hex.EncodeToString(sum[:])[:48], nil
}

// strictDecode parses canonical SVN-JCS-1 text into v with unknown keys
// refused.
func strictDecode(raw []byte, v any) error {
	canonical, err := jcs.ParseAndCanonicalize(string(raw))
	if err != nil || canonical != string(raw) {
		return fmt.Errorf("%w: not canonical SVN-JCS-1", ErrFormat)
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("%w: %v", ErrFormat, err)
	}
	return nil
}

// ParseRun reads a pending run and checks that its id names its bytes.
func ParseRun(raw []byte, runID string) (Run, error) {
	var r Run
	if err := strictDecode(raw, &r); err != nil {
		return Run{}, err
	}
	again, id, err := EncodeRun(r)
	if err != nil {
		return Run{}, err
	}
	if !bytes.Equal(again, raw) {
		return Run{}, fmt.Errorf("%w: keys outside the run format", ErrFormat)
	}
	if id != runID {
		return Run{}, fmt.Errorf("%w: run id does not name these bytes", ErrFormat)
	}
	return r, nil
}

// SummaryItem is one item of a stored batch summary.
type SummaryItem struct {
	EvidenceID        string `json:"evidence_id"`
	LeaseID           string `json:"lease_id"`
	ContentID         string `json:"content_id"`
	ContentVersion    int64  `json:"content_version"`
	Result            string `json:"result"`
	ReportSHA256      string `json:"report_sha256"`
	ObservationSHA256 string `json:"observation_sha256"`
	Expiry            int64  `json:"expiry_unix_seconds"`
}

// Summary is batches/<batch_id>.json: what the reviewer needs to go from an
// evidence id to its report, and from a batch to its provider and run.
type Summary struct {
	Format      string        `json:"format"`
	BatchID     string        `json:"batch_id"`
	BusinessID  string        `json:"business_id"`
	ProviderID  string        `json:"provider_id"`
	RunID       string        `json:"run_id"`
	Epoch       int64         `json:"epoch"`
	Signer      string        `json:"signer"`
	SignerKeyID string        `json:"signer_key_id"`
	Supersedes  string        `json:"supersedes"`
	Items       []SummaryItem `json:"items"`
}

func (s Summary) object() map[string]any {
	items := make([]any, 0, len(s.Items))
	for _, it := range s.Items {
		items = append(items, map[string]any{
			"evidence_id": it.EvidenceID, "lease_id": it.LeaseID, "content_id": it.ContentID,
			"content_version": it.ContentVersion, "result": it.Result, "report_sha256": it.ReportSHA256,
			"observation_sha256": it.ObservationSHA256, "expiry_unix_seconds": it.Expiry,
		})
	}
	return map[string]any{
		"format": s.Format, "batch_id": s.BatchID, "business_id": s.BusinessID, "provider_id": s.ProviderID,
		"run_id": s.RunID, "epoch": s.Epoch, "signer": s.Signer, "signer_key_id": s.SignerKeyID,
		"supersedes": s.Supersedes, "items": items,
	}
}

// SummaryOf is the summary of a batch.
func SummaryOf(b *Batch, runID string) Summary {
	s := Summary{
		Format: SummaryFormat, BatchID: b.ID(), BusinessID: b.Msg.GetBusinessId(), ProviderID: b.ProviderID,
		RunID: runID, Epoch: int64(b.Epoch()), Signer: hex.EncodeToString(b.Msg.GetSigner()),
		SignerKeyID: b.Msg.GetAuditorSignature().GetKeyId(), Supersedes: b.Supersedes,
	}
	for _, it := range b.Items {
		s.Items = append(s.Items, SummaryItem{
			EvidenceID: it.EvidenceID, LeaseID: it.LeaseID, ContentID: it.ContentID,
			ContentVersion: int64(it.ContentVersion), Result: it.Result,
			ReportSHA256: hex.EncodeToString(it.ReportSHA256[:]), ObservationSHA256: hex.EncodeToString(it.Observation[:]),
			Expiry: it.Expiry,
		})
	}
	return s
}

// EncodeSummary returns the SVN-JCS-1 text of s.
func EncodeSummary(s Summary) ([]byte, error) {
	if s.Format != SummaryFormat || !msgkit.ValidID(s.BatchID) || !msgkit.ValidID(s.BusinessID) || !hex40.MatchString(s.Signer) ||
		len(s.Items) < 1 || len(s.Items) > MaxItems {
		return nil, fmt.Errorf("%w: summary fields", ErrFormat)
	}
	for _, it := range s.Items {
		if !msgkit.ValidID(it.EvidenceID) || !hex64.MatchString(it.ReportSHA256) || !hex64.MatchString(it.ObservationSHA256) {
			return nil, fmt.Errorf("%w: summary item fields", ErrFormat)
		}
	}
	text, err := jcs.Canonicalize(s.object())
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrFormat, err)
	}
	return []byte(text), nil
}

// ParseSummary reads a stored batch summary.
func ParseSummary(raw []byte) (Summary, error) {
	var s Summary
	if err := strictDecode(raw, &s); err != nil {
		return Summary{}, err
	}
	again, err := EncodeSummary(s)
	if err != nil {
		return Summary{}, err
	}
	if !bytes.Equal(again, raw) {
		return Summary{}, fmt.Errorf("%w: keys outside the summary format", ErrFormat)
	}
	return s, nil
}

// Item returns the summary item of an evidence id.
func (s Summary) Item(evidenceID string) (SummaryItem, bool) {
	for _, it := range s.Items {
		if it.EvidenceID == evidenceID {
			return it, true
		}
	}
	return SummaryItem{}, false
}

// Persist writes a signed batch before any submission: one envelope per
// item (envelopes/<evidence_id>.json, the SVN-JCS-1 text of
// evidence.AuditEnvelope, byte-identical to what was signed), the message
// in deterministic protobuf (batches/<batch_id>.pb, business id attempt
// 0), and the summary (batches/<batch_id>.json). Every write is
// create-exclusive and idempotent for the same bytes. The batch must
// verify.
func Persist(st *store.Store, b *Batch, runID string) error {
	if code := evidence.VerifyAuditSignature(b.Msg); code != msgkit.OK {
		return fmt.Errorf("%w: %s", ErrSignature, code)
	}
	envelopes, err := Envelopes(b.Msg)
	if err != nil {
		return err
	}
	for i, it := range b.Msg.GetItems() {
		if err := st.Put(store.Envelopes, it.GetEvidenceId(), ".json", envelopes[i]); err != nil {
			return fmt.Errorf("batch: envelope %s: %w", it.GetEvidenceId(), err)
		}
	}
	raw, err := proto.MarshalOptions{Deterministic: true}.Marshal(b.Msg)
	if err != nil {
		return fmt.Errorf("batch: encode message: %w", err)
	}
	summary, err := EncodeSummary(SummaryOf(b, runID))
	if err != nil {
		return err
	}
	if err := st.Put(store.Batches, b.ID(), ".pb", raw); err != nil {
		return fmt.Errorf("batch: message: %w", err)
	}
	if err := st.Put(store.Batches, b.ID(), ".json", summary); err != nil {
		return fmt.Errorf("batch: summary: %w", err)
	}
	return nil
}

// Load reads a stored batch: the message, which must verify and match its
// name, and its summary, which must describe the same items.
func Load(st *store.Store, batchID string) (*Batch, Summary, error) {
	raw, err := st.Get(store.Batches, batchID, ".pb")
	if err != nil {
		return nil, Summary{}, err
	}
	msg := &evidencev1.MsgSubmitAuditBatch{}
	if err := proto.Unmarshal(raw, msg); err != nil {
		return nil, Summary{}, fmt.Errorf("%w: message: %v", ErrFormat, err)
	}
	if msg.GetBatchId() != batchID {
		return nil, Summary{}, fmt.Errorf("%w: message names another batch", ErrFormat)
	}
	if code := evidence.VerifyAuditSignature(msg); code != msgkit.OK {
		return nil, Summary{}, fmt.Errorf("%w: %s", ErrSignature, code)
	}
	rawSummary, err := st.Get(store.Batches, batchID, ".json")
	if err != nil {
		return nil, Summary{}, err
	}
	s, err := ParseSummary(rawSummary)
	if err != nil {
		return nil, Summary{}, err
	}
	b, err := fromSummary(msg, s)
	if err != nil {
		return nil, Summary{}, err
	}
	return b, s, nil
}

// fromSummary rebuilds the Batch of a stored message and its summary and
// checks that they describe the same items.
func fromSummary(msg *evidencev1.MsgSubmitAuditBatch, s Summary) (*Batch, error) {
	if s.BatchID != msg.GetBatchId() || s.Epoch != int64(msg.GetEpoch()) || s.Signer != hex.EncodeToString(msg.GetSigner()) ||
		s.SignerKeyID != msg.GetAuditorSignature().GetKeyId() || len(s.Items) != len(msg.GetItems()) {
		return nil, fmt.Errorf("%w: summary does not describe the message", ErrFormat)
	}
	b := &Batch{ProviderID: s.ProviderID, Msg: msg, Supersedes: s.Supersedes}
	for i, it := range msg.GetItems() {
		si := s.Items[i]
		var rep, obs [32]byte
		repRaw, err1 := hex.DecodeString(si.ReportSHA256)
		obsRaw, err2 := hex.DecodeString(si.ObservationSHA256)
		if err1 != nil || err2 != nil {
			return nil, fmt.Errorf("%w: summary digests", ErrFormat)
		}
		copy(rep[:], repRaw)
		copy(obs[:], obsRaw)
		if si.EvidenceID != it.GetEvidenceId() || si.LeaseID != it.GetLeaseId() || si.ContentID != it.GetContentId() ||
			si.ContentVersion != int64(it.GetContentVersion()) || si.Expiry != it.GetExpiryUnixSeconds() ||
			!bytes.Equal(obs[:], it.GetObservationDigest().GetValue()) || si.EvidenceID != EvidenceID(msg.GetSigner(), rep) {
			return nil, fmt.Errorf("%w: summary item %d does not match the message", ErrFormat, i)
		}
		b.Items = append(b.Items, Item{
			EvidenceID: si.EvidenceID, ProviderID: s.ProviderID, LeaseID: si.LeaseID, ContentID: si.ContentID,
			ContentVersion: uint64(si.ContentVersion), Result: si.Result, ReportSHA256: rep, Observation: obs,
			Expiry: si.Expiry,
		})
	}
	return b, nil
}

// CheckFailSource reads the stored report of a FAIL item and returns an
// error naming the item unless the report hashes to the item's report
// digest, parses, is FAIL, and has endpoint_source CAPABILITY_MANIFEST (N2).
// An item counts as FAIL when its summary says FAIL or its observation is
// its report digest (the FAIL observation); every other item returns nil.
// NewItem already refuses such a FAIL item; this check covers the paths that
// re-sign or broadcast stored items without NewItem: the CONFLICT, EXPIRED,
// and WITHDRAWN rebuild and the submission of a stored batch, for example a
// batch signed before the report recorded its endpoint source.
func CheckFailSource(st *store.Store, it Item) error {
	if it.Result != report.ResultFail && it.Observation != it.ReportSHA256 {
		return nil
	}
	raw, err := st.Report(it.ReportSHA256)
	if err != nil {
		return fmt.Errorf("%w: FAIL item %s: report: %v", ErrIneligible, it.EvidenceID, err)
	}
	r, err := report.Parse(raw)
	if err != nil {
		return fmt.Errorf("%w: FAIL item %s: report: %v", ErrIneligible, it.EvidenceID, err)
	}
	if r.Result != report.ResultFail || r.EndpointSource != report.EndpointSourceCapabilityManifest {
		return fmt.Errorf("%w: FAIL item %s: report result %s endpoint_source %s, a FAIL needs endpoint_source %s",
			ErrIneligible, it.EvidenceID, r.Result, r.EndpointSource, report.EndpointSourceCapabilityManifest)
	}
	return nil
}

// CheckFailSources is CheckFailSource for every item of b.
func CheckFailSources(st *store.Store, b *Batch) error {
	for _, it := range b.Items {
		if err := CheckFailSource(st, it); err != nil {
			return err
		}
	}
	return nil
}
