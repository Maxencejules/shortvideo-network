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
	"github.com/Maxencejules/shortvideo-network/chain/x/leases"
)

// The Python side: spec_oracle.py over docs/evidence/goal-02/spec_machine.py.
//
// The interpreter is $SVN_SPEC_PYTHON, else <repo>/.venv/bin/python, else
// python3 on PATH. When none exists the test fails; it never skips. The
// oracle runs as `<python> -I -B spec_oracle.py --stream` (isolated mode, no
// bytecode written), one subprocess per test process (per fuzz worker).

// oracleScript is the oracle's path relative to the repository root.
// It lives under tests/ because the goal-01 contract checker allows Python
// only in the repository's Python trees, not under chain/.
const oracleScript = "tests/difftest/spec_oracle.py"

// repoRoot is the repository root: the test runs in chain/x/leases/difftest.
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
	var probe struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(reply, &probe); err != nil {
		tb.Fatalf("oracle response is not JSON: %v: %s", err, reply)
	}
	if probe.Error != "" {
		tb.Fatalf("oracle rejected the request %s: %s", line, probe.Error)
	}
	dec := json.NewDecoder(bytes.NewReader(reply))
	dec.DisallowUnknownFields()
	if err := dec.Decode(resp); err != nil {
		tb.Fatalf("decode oracle response: %v: %s", err, reply)
	}
}

// oracleConfig is the reset config (spec_oracle.py CONFIG_KEYS).
type oracleConfig struct {
	ChainTime               int64             `json:"chain_time"`
	TermSeconds             *uint64           `json:"term_seconds"`
	Governed                map[string]uint64 `json:"governed"`
	Submitters              map[string]string `json:"submitters"`
	MaxOfferLifetimeSeconds int64             `json:"max_offer_lifetime_seconds"`
	MaxOpenOffersPerContent int               `json:"max_open_offers_per_content"`
}

type resetRequest struct {
	Cmd               string       `json:"cmd"`
	Config            oracleConfig `json:"config"`
	DisableDeviations bool         `json:"disable_deviations"`
}

type resetResponse struct {
	OK         bool       `json:"ok"`
	Projection projection `json:"projection"`
}

type stepRequest struct {
	Cmd  string         `json:"cmd"`
	Step map[string]any `json:"step"`
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
// constants of deviations D-5 and D-6 come from x/leases, not from the
// oracle.
func (o *oracleProc) reset(tb testing.TB, cfg *config, disableDeviations bool) projection {
	tb.Helper()
	oc := oracleConfig{
		ChainTime:               startTime,
		Governed:                map[string]uint64{},
		Submitters:              map[string]string{},
		MaxOfferLifetimeSeconds: leases.MaxOfferLifetimeSeconds,
		MaxOpenOffersPerContent: leases.MaxOpenOffersPerContent,
	}
	if cfg.term > 0 {
		term := cfg.term
		oc.TermSeconds = &term
	}
	for key, value := range cfg.governed {
		oc.Governed[key] = value
	}
	for addr, role := range roleTable() {
		oc.Submitters[hexOf([]byte(addr))] = roleName(role)
	}
	var resp resetResponse
	o.call(tb, resetRequest{Cmd: "reset", Config: oc, DisableDeviations: disableDeviations}, &resp)
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

// projection is the compared chain state (plan section 14.3), decoded from
// the oracle into typed structs and built from the Go stores. Integers are
// decimal strings so uint64 values survive JSON exactly.
type projection struct {
	ChainTime   string         `json:"chain_time"`
	Providers   []providerView `json:"providers"`
	Contents    []contentView  `json:"contents"`
	Offers      []offerView    `json:"offers"`
	Leases      []leaseView    `json:"leases"`
	Pairs       []pairView     `json:"pairs"`
	BusinessIDs []string       `json:"business_ids"`
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
// fixtures (the spec's apply_proposal_payload emits none; x/providers and
// x/content do). label names the sequence and step.
func compareStep(tb testing.TB, label string, s *step, goOut outcome, goProj projection, spec specResult) bool {
	tb.Helper()
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
	diffs = append(diffs, projectionDiff(goProj, spec.Projection)...)
	if len(diffs) == 0 {
		return true
	}
	stepJSON, _ := json.Marshal(s.spec)
	tb.Errorf("%s: go and spec_machine.py diverge\nstep: %s\n%s", label, stepJSON, strings.Join(diffs, "\n"))
	return false
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
