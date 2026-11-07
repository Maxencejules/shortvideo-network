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
	"strings"
	"sync"
	"testing"

	"github.com/Maxencejules/shortvideo-network/chain/x/authority"
)

// The Python side: spec_oracle.py over docs/evidence/goal-02/spec_machine.py.
//
// The interpreter is $SVN_SPEC_PYTHON, else <repo>/.venv/bin/python, else
// python3 on PATH. When none exists the test fails; it never skips. The
// oracle runs as `<python> -I -B spec_oracle.py --stream` (isolated mode, no
// bytecode written), one subprocess per test process (per fuzz worker).

// oracleScript is the oracle's path relative to the repository root.
const oracleScript = "tests/difftest/spec_oracle.py"

// repoRoot is the repository root: the test runs in chain/x/authority/difftest.
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

// oracleConfig is the reset config (spec_oracle.py CONFIG_KEYS plus
// "council"). No lease step runs in this package; the three x/leases
// constants are required keys that no council step reads.
type oracleConfig struct {
	ChainTime               int64             `json:"chain_time"`
	TermSeconds             *uint64           `json:"term_seconds"`
	Governed                map[string]uint64 `json:"governed"`
	Submitters              map[string]string `json:"submitters"`
	MaxOfferLifetimeSeconds int64             `json:"max_offer_lifetime_seconds"`
	MaxOpenOffersPerContent int               `json:"max_open_offers_per_content"`
	MaxPerScheduler         int               `json:"max_open_offers_per_content_per_scheduler"`
	Council                 councilConfig     `json:"council"`
}

type seatConfig struct {
	Address string `json:"address"`
	SeatID  string `json:"seat_id"`
	Pubkey  string `json:"pubkey"`
}

type validatorConfig struct {
	NodeID          string `json:"node_id"`
	Power           int64  `json:"power"`
	ConsensusPubkey string `json:"consensus_pubkey"`
}

type councilConfig struct {
	Seats             []seatConfig      `json:"seats"`
	Threshold         uint64            `json:"threshold"`
	DelaySeconds      int64             `json:"delay_seconds"`
	Height            int64             `json:"height"`
	Validators        []validatorConfig `json:"validators"`
	ApprovedSeats     int               `json:"approved_seats"`
	ApprovedThreshold int               `json:"approved_threshold"`
	MaxOpenPerSeat    int               `json:"max_open_proposals_per_seat"`
	MaxPending        int               `json:"max_pending_changes"`
}

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
	Cmd        string         `json:"cmd"`
	Step       map[string]any `json:"step"`
	Projection *bool          `json:"projection,omitempty"`
}

// specResult is the oracle's answer for one step. Keep is present only on
// council steps.
type specResult struct {
	Code       string      `json:"code"`
	Idempotent bool        `json:"idempotent"`
	Keep       bool        `json:"keep"`
	Events     []eventRef  `json:"events"`
	Deviation  *string     `json:"deviation"`
	Projection *projection `json:"projection"`
}

func (r specResult) deviation() string {
	if r.Deviation == nil {
		return ""
	}
	return *r.Deviation
}

// reset starts a new sequence with cfg's chain configuration and the chain
// constants of the council rules. disable is false, true, or a list of
// rule names.
func (o *oracleProc) reset(tb testing.TB, cfg *config, disable any) projection {
	tb.Helper()
	oc := oracleConfig{
		ChainTime: startTime, Governed: map[string]uint64{}, Submitters: map[string]string{},
		MaxOfferLifetimeSeconds: 1, MaxOpenOffersPerContent: 1, MaxPerScheduler: 1,
		Council: councilConfig{
			Threshold: cfg.threshold, DelaySeconds: cfg.delay, Height: startHeight,
			ApprovedSeats: authority.ApprovedSeats, ApprovedThreshold: authority.ApprovedThreshold,
			MaxOpenPerSeat: authority.MaxOpenProposalsPerSeat, MaxPending: authority.MaxPendingChanges,
		},
	}
	for addr, role := range submitterRoles() {
		oc.Submitters[hexOf([]byte(addr))] = roleName(role)
	}
	for i := 1; i <= seatCount; i++ {
		oc.Council.Seats = append(oc.Council.Seats, seatConfig{Address: hexOf(seatAcct(i)), SeatID: seatID(i), Pubkey: hexOf(seatPub(i))})
	}
	for i := 1; i <= validatorCount; i++ {
		oc.Council.Validators = append(oc.Council.Validators, validatorConfig{
			NodeID: nodeID(i), Power: validatorPower, ConsensusPubkey: hexOf(consensusKey(nodeID(i))),
		})
	}
	var resp resetResponse
	o.call(tb, resetRequest{Cmd: "reset", Config: oc, DisableDeviations: disable}, &resp)
	if !resp.OK {
		tb.Fatalf("oracle reset failed")
	}
	for _, seed := range cfg.seeds {
		var seeded specResult
		o.call(tb, stepRequest{Cmd: "step", Step: seedSpec(seed)}, &seeded)
		if seeded.Code != "ok" {
			tb.Fatalf("oracle SeedProvider %s: %s", seed.GetProviderId(), seeded.Code)
		}
		resp.Projection = *seeded.Projection
	}
	return resp.Projection
}

// step sends one step; withProjection false asks for no projection (the
// answer's projection is then nil).
func (o *oracleProc) step(tb testing.TB, s *step, withProjection bool) specResult {
	tb.Helper()
	req := stepRequest{Cmd: "step", Step: s.spec}
	if !withProjection {
		off := false
		req.Projection = &off
	}
	var resp specResult
	o.call(tb, req, &resp)
	if withProjection && resp.Projection == nil {
		tb.Fatalf("oracle answered without a projection")
	}
	return resp
}

// projection is the compared state, decoded from the oracle into typed
// structs and built from the Go stores. Integers are decimal strings.
type projection struct {
	ChainTime   string         `json:"chain_time"`
	Providers   []providerView `json:"providers"`
	Contents    []contentView  `json:"contents"`
	Offers      []struct{}     `json:"offers"`
	Leases      []struct{}     `json:"leases"`
	Pairs       []struct{}     `json:"pairs"`
	BusinessIDs []string       `json:"business_ids"`
	Council     councilView    `json:"council"`
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

type councilView struct {
	Height         string          `json:"height"`
	PolicyVersion  string          `json:"policy_version"`
	Proposals      []proposalView  `json:"proposals"`
	ExecutedParams []executedView  `json:"executed_params"`
	GovernedLimits []governedView  `json:"governed_limits"`
	Validators     []validatorView `json:"validators"`
	PendingPower   []pendingView   `json:"pending_power"`
}

type proposalView struct {
	ProposalID              string   `json:"proposal_id"`
	ParameterKey            string   `json:"parameter_key"`
	BasePolicyVersion       string   `json:"base_policy_version"`
	PayloadDigest           string   `json:"payload_digest"`
	State                   string   `json:"state"`
	ExpiryUnixSeconds       string   `json:"expiry_unix_seconds"`
	ExecuteAfterUnixSeconds string   `json:"execute_after_unix_seconds"`
	Approvers               []string `json:"approvers"`
}

type executedView struct {
	ParameterKey  string `json:"parameter_key"`
	PayloadDigest string `json:"payload_digest"`
}

type governedView struct {
	LimitKey string `json:"limit_key"`
	Value    string `json:"value"`
}

type validatorView struct {
	NodeID          string  `json:"node_id"`
	Power           string  `json:"power"`
	ConsensusPubkey string  `json:"consensus_pubkey"`
	ReasonDigest    *string `json:"reason_digest"`
}

type activationView struct {
	Kind  string `json:"kind"`
	Point string `json:"point"`
}

type pendingView struct {
	ProposalID      string         `json:"proposal_id"`
	NodeID          string         `json:"node_id"`
	Power           string         `json:"power"`
	ConsensusPubkey string         `json:"consensus_pubkey"`
	ReasonDigest    string         `json:"reason_digest"`
	Activation      activationView `json:"activation"`
}

// compareStep reports every difference between the Go outcome and the
// oracle's answer for one step through tb.Errorf, and returns whether they
// agree. A nil goProj compares no state (the step asked for none).
func compareStep(tb testing.TB, label string, s *step, goOut outcome, goProj *projection, spec specResult) bool {
	tb.Helper()
	var diffs []string
	if goOut.codeName() != spec.Code {
		diffs = append(diffs, fmt.Sprintf("code: go %s, spec %s", goOut.codeName(), spec.Code))
	}
	if goOut.idempotent != spec.Idempotent {
		diffs = append(diffs, fmt.Sprintf("idempotent: go %t, spec %t", goOut.idempotent, spec.Idempotent))
	}
	if goOut.keep != spec.Keep {
		diffs = append(diffs, fmt.Sprintf("keep: go %t, spec %t", goOut.keep, spec.Keep))
	}
	if !reflect.DeepEqual(nonNil(goOut.events), nonNil(spec.Events)) {
		diffs = append(diffs, fmt.Sprintf("events: go %v, spec %v", goOut.events, spec.Events))
	}
	if goProj != nil {
		diffs = append(diffs, projectionDiff(*goProj, *spec.Projection)...)
	}
	if len(diffs) == 0 {
		return true
	}
	stepJSON, _ := json.Marshal(s.spec)
	tb.Errorf("%s: go and spec_machine.py diverge (deviation %q)\nstep: %s\n%s", label, spec.deviation(), stepJSON, strings.Join(diffs, "\n"))
	return false
}

func nonNil(refs []eventRef) []eventRef {
	if refs == nil {
		return []eventRef{}
	}
	return refs
}

// projectionDiff lists the differing parts of two projections, one line per
// differing field (the council part per field).
func projectionDiff(goProj, specProj projection) []string {
	var diffs []string
	add := func(name string, g, s any) {
		if reflect.DeepEqual(g, s) {
			return
		}
		gj, _ := json.Marshal(g)
		sj, _ := json.Marshal(s)
		diffs = append(diffs, fmt.Sprintf("projection.%s:\n  go   %s\n  spec %s", name, gj, sj))
	}
	gv, sv := reflect.ValueOf(goProj), reflect.ValueOf(specProj)
	for i := 0; i < gv.NumField(); i++ {
		name := gv.Type().Field(i).Name
		if name == "Council" {
			continue
		}
		add(name, gv.Field(i).Interface(), sv.Field(i).Interface())
	}
	gc, sc := reflect.ValueOf(goProj.Council), reflect.ValueOf(specProj.Council)
	for i := 0; i < gc.NumField(); i++ {
		add("Council."+gc.Type().Field(i).Name, gc.Field(i).Interface(), sc.Field(i).Interface())
	}
	return diffs
}
