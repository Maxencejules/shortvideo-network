package difftest

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
)

// The Python side: tests/difftest/spec_oracle.py over
// docs/evidence/goal-02/spec_machine.py, shared with the x/leases
// differential (chain/x/leases/difftest). The reset config carries the
// "evidence" key, which turns on the oracle's SubmitAuditBatch step, the
// evidence projection, and the x/evidence deviation rules.
//
// The interpreter is $SVN_SPEC_PYTHON, else <repo>/.venv/bin/python, else
// python3 on PATH. When none exists the test fails; it never skips. The
// oracle runs as `<python> -I -B spec_oracle.py --stream` (isolated mode, no
// bytecode written), one subprocess per test process (per fuzz worker).

// oracleScript is the oracle's path relative to the repository root.
const oracleScript = "tests/difftest/spec_oracle.py"

// repoRoot is the repository root: the test runs in chain/x/evidence/difftest.
func repoRoot(tb testing.TB) string {
	tb.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		tb.Fatalf("repo root: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "docs", "evidence", "goal-02", "spec_machine.py")); err != nil {
		tb.Fatalf("spec_machine.py not found under %s: %v", root, err)
	}
	return root
}

// pythonPath resolves the interpreter. It never returns "" without failing.
func pythonPath(tb testing.TB, root string) string {
	tb.Helper()
	if env := os.Getenv("SVN_SPEC_PYTHON"); env != "" {
		path, err := exec.LookPath(env)
		if err != nil {
			tb.Fatalf("SVN_SPEC_PYTHON=%q is not executable: %v", env, err)
		}
		return path
	}
	venv := filepath.Join(root, ".venv", "bin", "python")
	if info, err := os.Stat(venv); err == nil && !info.IsDir() {
		return venv
	}
	path, err := exec.LookPath("python3")
	if err != nil {
		tb.Fatalf("no Python interpreter for the spec oracle: set SVN_SPEC_PYTHON, create %s, or put python3 on PATH (%v)", venv, err)
	}
	return path
}

// oracleProc is one running spec_oracle.py --stream.
type oracleProc struct {
	cmd    *exec.Cmd
	in     io.WriteCloser
	out    *bufio.Reader
	stderr *bytes.Buffer
	broken error
}

var (
	oracleMu     sync.Mutex
	sharedOracle *oracleProc
)

// getOracle returns this process's oracle, starting it on first use.
func getOracle(tb testing.TB) *oracleProc {
	tb.Helper()
	oracleMu.Lock()
	defer oracleMu.Unlock()
	if sharedOracle != nil && sharedOracle.broken == nil {
		return sharedOracle
	}
	root := repoRoot(tb)
	python := pythonPath(tb, root)
	cmd := exec.Command(python, "-I", "-B", filepath.Join(root, oracleScript), "--stream")
	cmd.Dir = root
	stderr := &bytes.Buffer{}
	cmd.Stderr = stderr
	in, err := cmd.StdinPipe()
	if err != nil {
		tb.Fatalf("oracle stdin: %v", err)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		tb.Fatalf("oracle stdout: %v", err)
	}
	if err := cmd.Start(); err != nil {
		tb.Fatalf("start %s %s: %v", python, oracleScript, err)
	}
	sharedOracle = &oracleProc{cmd: cmd, in: in, out: bufio.NewReader(out), stderr: stderr}
	return sharedOracle
}

// call sends one request line and decodes one response line into resp
// with unknown fields rejected.
func (o *oracleProc) call(tb testing.TB, req any, resp any) {
	tb.Helper()
	if o.broken != nil {
		tb.Fatalf("oracle unusable: %v", o.broken)
	}
	line, err := json.Marshal(req)
	if err != nil {
		tb.Fatalf("encode oracle request: %v", err)
	}
	if _, err := o.in.Write(append(line, '\n')); err != nil {
		o.broken = err
		tb.Fatalf("write oracle request: %v (stderr: %s)", err, o.stderr.String())
	}
	reply, err := o.out.ReadBytes('\n')
	if err != nil {
		o.broken = err
		tb.Fatalf("read oracle response: %v (stderr: %s)", err, o.stderr.String())
	}
	dec := json.NewDecoder(bytes.NewReader(reply))
	dec.DisallowUnknownFields()
	if err := dec.Decode(resp); err != nil {
		// An {"error": ...} answer has an unknown field for every response
		// type; name it when it is one.
		var probe struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(reply, &probe) == nil && probe.Error != "" {
			tb.Fatalf("oracle rejected the request %s: %s", line, probe.Error)
		}
		tb.Fatalf("decode oracle response: %v: %s", err, reply)
	}
}

// oracleConfig is the reset config (spec_oracle.py CONFIG_KEYS plus
// "evidence").
type oracleConfig struct {
	ChainTime               int64             `json:"chain_time"`
	TermSeconds             *uint64           `json:"term_seconds"`
	Governed                map[string]uint64 `json:"governed"`
	Submitters              map[string]string `json:"submitters"`
	MaxOfferLifetimeSeconds int64             `json:"max_offer_lifetime_seconds"`
	MaxOpenOffersPerContent int               `json:"max_open_offers_per_content"`
	MaxPerScheduler         int               `json:"max_open_offers_per_content_per_scheduler"`
	Evidence                evidenceConfig    `json:"evidence"`
}

// evidenceConfig is the oracle's "evidence" reset config: the x/evidence
// chain constants of the evidence deviation rules, and the item the
// adapter maps an empty batch to.
type evidenceConfig struct {
	EpochSeconds  int64          `json:"epoch_seconds"`
	MaxLifetime   int64          `json:"max_evidence_lifetime_seconds"`
	MaxPerEpoch   uint64         `json:"max_evidence_per_auditor_per_epoch"`
	IDPrefixBytes int            `json:"id_prefix_bytes"`
	SyntheticItem map[string]any `json:"synthetic_item"`
}

// syntheticItem is the fixed, pattern-valid item the adapter maps an empty
// batch to (evidence D-6): the spec step needs item fields
// (spec_machine.py:925) that a batch without items does not have. Its
// digest is what an empty batch's abstract signature binds.
func syntheticItem() map[string]any {
	return map[string]any{
		"evidence_id": "synthetic-item", "kind": "AUDITOR_ATTESTATION", "lease_id": "synthetic-lease",
		"content_id": "synthetic-content", "content_version": 1, "expiry_unix_seconds": 1,
		"attestation": true, "observation_digest": syntheticDigest,
	}
}

// syntheticDigest is the synthetic item's observation digest.
var syntheticDigest = strings.Repeat("00", 32)

type resetRequest struct {
	Cmd               string       `json:"cmd"`
	Config            oracleConfig `json:"config"`
	DisableDeviations any          `json:"disable_deviations"`
}

type resetResponse struct {
	OK         bool       `json:"ok"`
	Projection projection `json:"projection"`
}

type stepRequest struct {
	Cmd  string         `json:"cmd"`
	Step map[string]any `json:"step"`
	// Projection false asks for no projection (stepBrief).
	Projection *bool `json:"projection,omitempty"`
}

// specResult is the oracle's answer for one step.
type specResult struct {
	Code       string     `json:"code"`
	Idempotent bool       `json:"idempotent"`
	Events     []eventRef `json:"events"`
	Deviation  *string    `json:"deviation"`
	Projection projection `json:"projection"`
}

func (r specResult) deviation() string {
	if r.Deviation == nil {
		return ""
	}
	return *r.Deviation
}

// reset starts a new sequence with cfg's chain configuration. The chain
// constants of the lease deviations (D-5, D-6, D-10) come from x/leases and
// those of the evidence deviations (D-3, D-9, D-15, D-17) from x/evidence,
// not from the oracle. disable is false, true, or a list of rule names.
func (o *oracleProc) reset(tb testing.TB, cfg *config, disable any) projection {
	tb.Helper()
	term := uint64(termSeconds)
	oc := oracleConfig{
		ChainTime:               startTime,
		TermSeconds:             &term,
		Governed:                map[string]uint64{},
		Submitters:              map[string]string{},
		MaxOfferLifetimeSeconds: leases.MaxOfferLifetimeSeconds,
		MaxOpenOffersPerContent: leases.MaxOpenOffersPerContent,
		MaxPerScheduler:         leases.MaxOpenOffersPerContentPerScheduler,
		Evidence: evidenceConfig{
			EpochSeconds:  evidence.EpochSeconds,
			MaxLifetime:   evidence.MaxEvidenceLifetimeSeconds,
			MaxPerEpoch:   evidence.MaxEvidencePerAuditorPerEpoch,
			IDPrefixBytes: evidence.IDPrefixBytes,
			SyntheticItem: syntheticItem(),
		},
	}
	for key, value := range cfg.governed {
		oc.Governed[key] = value
	}
	for addr, role := range roleTable() {
		oc.Submitters[hexOf([]byte(addr))] = roleName(role)
	}
	var resp resetResponse
	o.call(tb, resetRequest{Cmd: "reset", Config: oc, DisableDeviations: disable}, &resp)
	if !resp.OK {
		tb.Fatalf("oracle reset failed")
	}
	return resp.Projection
}

func (o *oracleProc) step(tb testing.TB, s *step) specResult {
	tb.Helper()
	var resp specResult
	o.call(tb, stepRequest{Cmd: "step", Step: s.spec}, &resp)
	return resp
}

// stepBrief is step without the projection: the oracle answers
// "projection": null and the result's Projection is the zero value.
func (o *oracleProc) stepBrief(tb testing.TB, s *step) specResult {
	tb.Helper()
	var resp specResult
	none := false
	o.call(tb, stepRequest{Cmd: "step", Step: s.spec, Projection: &none}, &resp)
	return resp
}

// projection is the compared chain state, decoded from the oracle into
// typed structs and built from the Go stores. Integers are decimal strings
// so uint64 values survive JSON exactly. The spec record stores no
// content_version, epoch, signer, or schema version (spec_machine.py
// :979-988), so those are not compared here; the x/evidence unit tests
// check them.
type projection struct {
	ChainTime   string         `json:"chain_time"`
	Providers   []providerView `json:"providers"`
	Contents    []contentView  `json:"contents"`
	Offers      []offerView    `json:"offers"`
	Leases      []leaseView    `json:"leases"`
	Pairs       []pairView     `json:"pairs"`
	BusinessIDs []string       `json:"business_ids"`
	Evidence    []evidenceView `json:"evidence"`
	BatchIDs    []string       `json:"batch_ids"`
}

type providerView struct {
	ProviderID             string   `json:"provider_id"`
	Account                string   `json:"account"`
	State                  string   `json:"state"`
	SigningKeyID           string   `json:"signing_key_id"`
	SigningPubkey          string   `json:"signing_pubkey"`
	KeyGeneration          string   `json:"key_generation"`
	AdmittedCapacityBytes  string   `json:"admitted_capacity_bytes"`
	ReservedBytes          string   `json:"reserved_bytes"`
	OperatorGroupID        string   `json:"operator_group_id"`
	EndpointManifestDigest string   `json:"endpoint_manifest_digest"`
	PreviousPubkeys        []string `json:"previous_pubkeys"`
}

type tombstoneView struct {
	CommittedUnixSeconds string `json:"committed_unix_seconds"`
	ReasonDigest         string `json:"reason_digest"`
}

type contentView struct {
	ContentID      string         `json:"content_id"`
	ContentVersion string         `json:"content_version"`
	ManifestDigest string         `json:"manifest_digest"`
	PackageBytes   string         `json:"package_bytes"`
	SubmittingHost string         `json:"submitting_host"`
	State          string         `json:"state"`
	Tombstone      *tombstoneView `json:"tombstone"`
}

type offerView struct {
	OfferID             string `json:"offer_id"`
	ContentID           string `json:"content_id"`
	ContentVersion      string `json:"content_version"`
	ProviderID          string `json:"provider_id"`
	PackageBytes        string `json:"package_bytes"`
	ManifestDigest      string `json:"manifest_digest"`
	TermsDigest         string `json:"terms_digest"`
	StartUnixSeconds    string `json:"start_unix_seconds"`
	DeadlineUnixSeconds string `json:"deadline_unix_seconds"`
	ExpiryUnixSeconds   string `json:"expiry_unix_seconds"`
	TermVersion         string `json:"term_version"`
	Scheduler           string `json:"scheduler"`
}

type leaseView struct {
	LeaseID             string `json:"lease_id"`
	OfferID             string `json:"offer_id"`
	ContentID           string `json:"content_id"`
	ContentVersion      string `json:"content_version"`
	ProviderID          string `json:"provider_id"`
	PackageBytes        string `json:"package_bytes"`
	ManifestDigest      string `json:"manifest_digest"`
	TermsDigest         string `json:"terms_digest"`
	StartUnixSeconds    string `json:"start_unix_seconds"`
	DeadlineUnixSeconds string `json:"deadline_unix_seconds"`
	State               string `json:"state"`
	TermVersion         string `json:"term_version"`
	PolicyVersion       string `json:"policy_version"`
	ReservationHeld     bool   `json:"reservation_held"`
}

type pairView struct {
	ProviderID     string `json:"provider_id"`
	ContentID      string `json:"content_id"`
	ContentVersion string `json:"content_version"`
	LeaseID        string `json:"lease_id"`
}

// evidenceView is one evidence record: the spec record's fields (kind by
// its short name, the digest as lowercase hex), the state a read reports
// at the projection's chain time (deviation D-4: RECORDED reads EXPIRED
// from its expiry on), and the stored state, which no block changes on the
// chain.
type evidenceView struct {
	EvidenceID        string `json:"evidence_id"`
	BatchID           string `json:"batch_id"`
	Kind              string `json:"kind"`
	State             string `json:"state"`
	StoredState       string `json:"stored_state"`
	LeaseID           string `json:"lease_id"`
	ContentID         string `json:"content_id"`
	ExpiryUnixSeconds string `json:"expiry_unix_seconds"`
	ObservationDigest string `json:"observation_digest"`
	Attestation       bool   `json:"attestation"`
}

func dec64(v int64) string { return fmt.Sprintf("%d", v) }

// projection builds the Go side of the compared state from a view.
func (v *view) projection() projection {
	p := projection{
		ChainTime:   dec64(v.now),
		Providers:   []providerView{},
		Contents:    []contentView{},
		Offers:      []offerView{},
		Leases:      []leaseView{},
		Pairs:       []pairView{},
		BusinessIDs: append([]string{}, v.businessIDs...),
		Evidence:    []evidenceView{},
		BatchIDs:    append([]string{}, v.batchIDs...),
	}
	for _, id := range v.providerIDs {
		r := v.providers[id]
		prev := []string{}
		for _, k := range r.GetPreviousSigningKeys() {
			prev = append(prev, hexOf(k.GetPublicKey()))
		}
		p.Providers = append(p.Providers, providerView{
			ProviderID:             id,
			Account:                hexOf(r.GetAccount()),
			State:                  strings.TrimPrefix(r.GetState().String(), "PROVIDER_STATE_"),
			SigningKeyID:           r.GetSigningKeyId(),
			SigningPubkey:          hexOf(r.GetSigningPubkey()),
			KeyGeneration:          msgkit.Dec(r.GetKeyGeneration()),
			AdmittedCapacityBytes:  msgkit.Dec(r.GetAdmittedCapacityBytes()),
			ReservedBytes:          msgkit.Dec(r.GetReservedBytes()),
			OperatorGroupID:        r.GetOperatorGroupId(),
			EndpointManifestDigest: hexOf(r.GetEndpointManifestDigest().GetValue()),
			PreviousPubkeys:        prev,
		})
	}
	for _, id := range v.contentIDs {
		c := v.contents[id]
		row := contentView{
			ContentID:      id,
			ContentVersion: msgkit.Dec(c.GetContentVersion()),
			ManifestDigest: hexOf(c.GetManifestDigest().GetValue()),
			PackageBytes:   msgkit.Dec(c.GetPackageBytes()),
			SubmittingHost: hexOf(c.GetSubmittingHost()),
			State:          strings.TrimPrefix(c.GetState().String(), "CONTENT_STATE_"),
		}
		if tomb, ok := v.tombstones[id]; ok {
			row.Tombstone = &tombstoneView{
				CommittedUnixSeconds: dec64(tomb.GetCommittedUnixSeconds()),
				ReasonDigest:         hexOf(tomb.GetReasonDigest().GetValue()),
			}
		}
		p.Contents = append(p.Contents, row)
	}
	for _, id := range v.offerIDs {
		o := v.offers[id]
		p.Offers = append(p.Offers, offerView{
			OfferID:             id,
			ContentID:           o.GetContentId(),
			ContentVersion:      msgkit.Dec(o.GetContentVersion()),
			ProviderID:          o.GetProviderId(),
			PackageBytes:        msgkit.Dec(o.GetPackageBytes()),
			ManifestDigest:      hexOf(o.GetManifestDigest().GetValue()),
			TermsDigest:         hexOf(o.GetTermsDigest().GetValue()),
			StartUnixSeconds:    dec64(o.GetStartUnixSeconds()),
			DeadlineUnixSeconds: dec64(o.GetDeadlineUnixSeconds()),
			ExpiryUnixSeconds:   dec64(o.GetExpiryUnixSeconds()),
			TermVersion:         msgkit.Dec(o.GetTermVersion()),
			Scheduler:           hexOf(o.GetScheduler()),
		})
	}
	for _, id := range v.leaseIDs {
		l := v.leases[id]
		p.Leases = append(p.Leases, leaseView{
			LeaseID:             id,
			OfferID:             l.GetOfferId(),
			ContentID:           l.GetContentId(),
			ContentVersion:      msgkit.Dec(l.GetContentVersion()),
			ProviderID:          l.GetProviderId(),
			PackageBytes:        msgkit.Dec(l.GetPackageBytes()),
			ManifestDigest:      hexOf(l.GetManifestDigest().GetValue()),
			TermsDigest:         hexOf(l.GetTermsDigest().GetValue()),
			StartUnixSeconds:    dec64(l.GetStartUnixSeconds()),
			DeadlineUnixSeconds: dec64(l.GetDeadlineUnixSeconds()),
			State:               leases.StateName(l.GetState()),
			TermVersion:         msgkit.Dec(l.GetTermVersion()),
			PolicyVersion:       msgkit.Dec(l.GetPolicyVersion()),
			ReservationHeld:     l.GetReservationHeld(),
		})
	}
	p.Pairs = append(p.Pairs, v.pairRows...)
	sortPairs(p.Pairs)
	for _, id := range v.recordIDs {
		r := v.records[id]
		s := r.stored
		p.Evidence = append(p.Evidence, evidenceView{
			EvidenceID:        id,
			BatchID:           s.GetBatchId(),
			Kind:              kindName(s.GetKind()),
			State:             evidence.StateName(r.reported),
			StoredState:       evidence.StateName(s.GetState()),
			LeaseID:           s.GetLeaseId(),
			ContentID:         s.GetContentId(),
			ExpiryUnixSeconds: dec64(s.GetExpiryUnixSeconds()),
			ObservationDigest: hexOf(s.GetObservationDigest().GetValue()),
			Attestation:       s.GetAttestation(),
		})
	}
	return p
}

// sortPairs orders pairs by (provider, content, numeric version), the order
// both sides use.
func sortPairs(pairs []pairView) {
	sort.Slice(pairs, func(i, j int) bool {
		a, b := pairs[i], pairs[j]
		if a.ProviderID != b.ProviderID {
			return a.ProviderID < b.ProviderID
		}
		if a.ContentID != b.ContentID {
			return a.ContentID < b.ContentID
		}
		// Decimal strings without leading zeros: shorter is smaller.
		if len(a.ContentVersion) != len(b.ContentVersion) {
			return len(a.ContentVersion) < len(b.ContentVersion)
		}
		return a.ContentVersion < b.ContentVersion
	})
}

// compareStep reports every difference between the Go outcome and the
// oracle's answer for one step through tb.Errorf, and returns whether they
// agree. Events are compared for transactions and blocks, not for council
// fixtures (the spec's apply_proposal_payload emits none; x/providers
// does). label names the sequence and step.
func compareStep(tb testing.TB, label string, s *step, goOut outcome, goProj projection, spec specResult) bool {
	tb.Helper()
	return reportDiffs(tb, label, s, append(outcomeDiff(s, goOut, spec), projectionDiff(goProj, spec.Projection)...))
}

// compareBrief is compareStep without the projections (a stepBrief answer).
func compareBrief(tb testing.TB, label string, s *step, goOut outcome, spec specResult) bool {
	tb.Helper()
	return reportDiffs(tb, label, s, outcomeDiff(s, goOut, spec))
}

func reportDiffs(tb testing.TB, label string, s *step, diffs []string) bool {
	tb.Helper()
	if len(diffs) == 0 {
		return true
	}
	stepJSON, _ := json.Marshal(s.spec)
	tb.Errorf("%s: go and spec_machine.py diverge\nstep: %s\n%s", label, stepJSON, strings.Join(diffs, "\n"))
	return false
}

// outcomeDiff lists the differences of code, idempotent flag, and events.
func outcomeDiff(s *step, goOut outcome, spec specResult) []string {
	var diffs []string
	if goOut.codeName() != spec.Code {
		diffs = append(diffs, fmt.Sprintf("code: go %s, spec %s", goOut.codeName(), spec.Code))
	}
	if goOut.idempotent != spec.Idempotent {
		diffs = append(diffs, fmt.Sprintf("idempotent: go %t, spec %t", goOut.idempotent, spec.Idempotent))
	}
	if s.comparesEvents() && !reflect.DeepEqual(nonNil(goOut.events), nonNil(spec.Events)) {
		diffs = append(diffs, fmt.Sprintf("events: go %v, spec %v", goOut.events, spec.Events))
	}
	return diffs
}

func nonNil(refs []eventRef) []eventRef {
	if refs == nil {
		return []eventRef{}
	}
	return refs
}

// projectionDiff lists the differing parts of two projections, one line per
// differing top-level field.
func projectionDiff(goProj, specProj projection) []string {
	var diffs []string
	gv, sv := reflect.ValueOf(goProj), reflect.ValueOf(specProj)
	for i := 0; i < gv.NumField(); i++ {
		g, s := gv.Field(i).Interface(), sv.Field(i).Interface()
		if reflect.DeepEqual(g, s) {
			continue
		}
		gj, _ := json.Marshal(g)
		sj, _ := json.Marshal(s)
		diffs = append(diffs, fmt.Sprintf("projection.%s:\n  go   %s\n  spec %s", gv.Type().Field(i).Name, gj, sj))
	}
	return diffs
}
