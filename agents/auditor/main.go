package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/batch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/chainclient"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/classify"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/config"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/keys"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/labels"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/review"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/store"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/verify"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers/capability"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
)

// Exit codes.
const (
	exitOK = 0
	// exitError: an input, store, chain-read, or key error; the command
	// did not finish.
	exitError = 1
	// exitUsage: unknown command or flags.
	exitUsage = 2
	// exitNotFinal: the command finished, but a batch is not recorded
	// (submit, status) or the review verdict is not CONSISTENT (verify).
	exitNotFinal = 3
)

// pollInterval is the wait between committed-result polls.
const pollInterval = 500 * time.Millisecond

// maxRecordFileBytes bounds a reviewer's record file.
const maxRecordFileBytes = 65536

func main() { os.Exit(cli(os.Args[1:], os.Stdout, os.Stderr)) }

const usage = `usage:
  auditor keygen --key PATH
  auditor run    --config FILE
  auditor sign   --config FILE --run RUN_ID
  auditor submit --config FILE --batch BATCH_ID [--batch BATCH_ID ...]
  auditor status --config FILE --batch BATCH_ID [--batch BATCH_ID ...]
  auditor verify --evidence-id ID --store DIR (--node URL | --record FILE)
`

// cli runs one command and returns its exit code. Output is one JSON
// object per line on stdout; errors go to stderr. No command prints key
// bytes.
func cli(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	commands := map[string]func([]string, io.Writer) (int, error){
		"keygen": cmdKeygen, "run": cmdRun, "sign": cmdSign,
		"submit": cmdSubmit, "status": cmdStatus, "verify": cmdVerify,
	}
	cmd, ok := commands[args[0]]
	if !ok {
		fmt.Fprint(stderr, usage)
		return exitUsage
	}
	code, err := cmd(args[1:], stdout)
	if err != nil {
		var u usageError
		if errors.As(err, &u) {
			fmt.Fprintf(stderr, "auditor %s: %s\n%s", args[0], u.msg, usage)
			return exitUsage
		}
		fmt.Fprintf(stderr, "auditor %s: %s\n", args[0], strconv.QuoteToASCII(err.Error()))
		return exitError
	}
	return code
}

type usageError struct{ msg string }

func (u usageError) Error() string { return u.msg }

// multi is a repeatable string flag.
type multi []string

func (m *multi) String() string     { return strings.Join(*m, ",") }
func (m *multi) Set(v string) error { *m = append(*m, v); return nil }

func parseFlags(fs *flag.FlagSet, args []string) error {
	fs.SetOutput(io.Discard)
	if err := fs.Parse(args); err != nil {
		return usageError{err.Error()}
	}
	if fs.NArg() != 0 {
		return usageError{"unexpected arguments"}
	}
	return nil
}

func emit(w io.Writer, v any) error {
	raw, err := json.Marshal(v)
	if err != nil {
		return err
	}
	_, err = fmt.Fprintf(w, "%s\n", raw)
	return err
}

// cmdKeygen writes a fresh dev-only key; it prints the address and public
// key only.
func cmdKeygen(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("keygen", flag.ContinueOnError)
	path := fs.String("key", "", "key file to create")
	if err := parseFlags(fs, args); err != nil {
		return 0, err
	}
	if *path == "" {
		return 0, usageError{"--key is required"}
	}
	key, err := keys.Generate(*path)
	if err != nil {
		return 0, err
	}
	return exitOK, emit(stdout, map[string]string{
		"address": hex.EncodeToString(key.Address()), "public_key": hex.EncodeToString(key.PublicKey()),
		"note": "development key; never use it for a live validator, governance, or production account",
	})
}

func loadConfig(fs *flag.FlagSet, args []string, extra func() error) (*config.Config, error) {
	path := fs.String("config", "", "operator configuration file")
	if err := parseFlags(fs, args); err != nil {
		return nil, err
	}
	if *path == "" {
		return nil, usageError{"--config is required"}
	}
	if extra != nil {
		if err := extra(); err != nil {
			return nil, err
		}
	}
	return config.Load(*path)
}

func newChain(cfg *config.Config) (*chainclient.Client, error) {
	rpc, err := chainclient.NewHTTP(cfg.ChainRPC, cfg.RPCTimeout)
	if err != nil {
		return nil, err
	}
	return chainclient.New(rpc)
}

// chainReader is what `run` reads from the chain. *chainclient.Client
// implements it; unit tests use a fake.
type chainReader interface {
	Header(ctx context.Context) (chainclient.Header, error)
	HeldLease(ctx context.Context, providerID, contentID string) (*leasesv1.Lease, bool, error)
	Content(ctx context.Context, contentID string, version uint64) (*contentv1.QueryContentResponse, bool, error)
	Provider(ctx context.Context, providerID string) (*providersv1.Provider, bool, error)
}

// cmdRun audits every configured lease and writes the reports and a
// pending run. It reads the chain and fetches from provider endpoints; it
// never opens the key path.
func cmdRun(args []string, stdout io.Writer) (int, error) {
	cfg, err := loadConfig(flag.NewFlagSet("run", flag.ContinueOnError), args, nil)
	if err != nil {
		return 0, err
	}
	chain, err := newChain(cfg)
	if err != nil {
		return 0, err
	}
	p, err := newProducer(cfg, chain)
	if err != nil {
		return 0, err
	}
	out, err := p.run(context.Background())
	if err != nil {
		return 0, err
	}
	return exitOK, emit(stdout, out)
}

// producer runs audits.
type producer struct {
	cfg     *config.Config
	chain   chainReader
	store   *store.Store
	fetcher *fetch.Client
	catalog labels.Catalog
	// now is the auditor host clock (report times, unverified).
	now func() time.Time
	// backoff is the wait before the first status retry; it doubles.
	backoff time.Duration
}

func newProducer(cfg *config.Config, chain chainReader) (*producer, error) {
	st, err := store.Open(cfg.StoreRoot)
	if err != nil {
		return nil, err
	}
	opts := fetch.Options{Limits: cfg.Limits}
	if opts.Policy, err = policy.New(cfg.Allowlist); err != nil {
		return nil, err
	}
	if cfg.CAFile != "" {
		if opts.RootCAs, err = fetch.LoadCAFile(cfg.CAFile); err != nil {
			return nil, err
		}
	}
	fetcher, err := fetch.New(opts)
	if err != nil {
		return nil, err
	}
	catalog, err := labels.Embedded()
	if err != nil {
		return nil, err
	}
	return &producer{cfg: cfg, chain: chain, store: st, fetcher: fetcher, catalog: catalog, now: time.Now, backoff: time.Second}, nil
}

// runOutput is what `run` prints.
type runOutput struct {
	RunID        string           `json:"run_id"`
	HeaderHeight string           `json:"header_height"`
	HeaderTime   int64            `json:"header_unix_seconds"`
	Epoch        int64            `json:"epoch"`
	Audits       []batch.RunAudit `json:"audits"`
	Label        string           `json:"label"`
}

func (p *producer) run(ctx context.Context) (runOutput, error) {
	ctx, cancel := context.WithTimeout(ctx, p.cfg.RunBudget)
	defer cancel()
	header, err := p.chain.Header(ctx)
	if err != nil {
		return runOutput{}, err
	}
	run := batch.Run{
		AuditorAddress: hex.EncodeToString(p.cfg.AuditorAddress), AuditorKeyID: p.cfg.KeyID,
		ChainID: header.ChainID, HeaderHeight: strconv.FormatInt(header.Height, 10), HeaderTime: header.Time,
		StartedUnixSeconds: p.now().Unix(),
	}
	for _, a := range p.cfg.Audits {
		ra, err := p.audit(ctx, a, header)
		if err != nil {
			return runOutput{}, err
		}
		run.Audits = append(run.Audits, ra)
	}
	raw, runID, err := batch.EncodeRun(run)
	if err != nil {
		return runOutput{}, err
	}
	if err := p.store.Put(store.Pending, runID, ".json", raw); err != nil {
		return runOutput{}, err
	}
	return runOutput{
		RunID: runID, HeaderHeight: run.HeaderHeight, HeaderTime: run.HeaderTime,
		Epoch: int64(evidence.EpochAt(header.Time)), Audits: run.Audits,
		Label: "auditor host clock times are not verified; results are attestations, not storage proofs",
	}, nil
}

// incomplete is a run entry without a report: no held lease was read, so
// there is no content version or manifest digest to describe.
func incomplete(a config.Audit, reasons ...classify.Reason) batch.RunAudit {
	s := classify.Reasons(reasons...)
	return batch.RunAudit{
		LeaseID: a.LeaseID, ProviderID: a.ProviderID, ContentID: a.ContentID,
		Result: s.Result, Reasons: s.Reasons,
	}
}

// audit runs one audit. Every check that needs no provider request runs
// first: the held lease, its eligibility (owner ruling on Q4), the
// content's state, the capability manifest, and the media manifest. The
// endpoint is opened, and its host name resolved, only after them
// (lazySession). Nothing is resolved or requested when one of them fails.
func (p *producer) audit(ctx context.Context, a config.Audit, header chainclient.Header) (batch.RunAudit, error) {
	if ctx.Err() != nil {
		return incomplete(a, classify.RunBudgetExhausted), nil
	}
	started := p.now()
	lease, found, err := p.chain.HeldLease(ctx, a.ProviderID, a.ContentID)
	if err != nil {
		return incomplete(a, classify.ChainReadFailed), nil
	}
	if !found {
		lease = nil
	}
	ref := batch.LeaseRef{LeaseID: a.LeaseID, ProviderID: a.ProviderID, ContentID: a.ContentID}
	reason := batch.CheckLease(lease, ref, header.Time, p.cfg.Window)
	if reason == classify.LeaseNotHeld || reason == classify.ChainReadFailed {
		return incomplete(a, reason), nil
	}
	manifestSHA, _ := batch.ManifestSHA256(lease)
	w := &writer{p: p, a: a, lease: lease, manifest: manifestSHA, started: started}
	if a.CapabilityManifestPath == "" {
		w.endpointName = a.Endpoints[a.EndpointIndex].Name
	}
	if reason != "" {
		return w.write(classify.Reasons(reason), nil)
	}
	content, found, err := p.chain.Content(ctx, a.ContentID, lease.GetContentVersion())
	if err != nil {
		return w.write(classify.Reasons(classify.ChainReadFailed), nil)
	}
	if reason := batch.CheckContent(content, found, lease); reason != "" {
		return w.write(classify.Reasons(reason), nil)
	}
	endpoint, reason, err := p.endpoint(ctx, a)
	if err != nil {
		return batch.RunAudit{}, err
	}
	if reason != "" {
		return w.write(classify.Reasons(reason), nil)
	}
	w.endpointName = endpoint.Name
	manifest, err := verify.ReadManifestFile(a.ManifestPath)
	if err != nil {
		return w.write(classify.Reasons(classify.InputManifestInvalid), nil)
	}
	session := &lazySession{fetcher: p.fetcher, providerID: a.ProviderID, url: endpoint.URL}
	defer session.Close()
	req := verify.Request{
		Lease: verify.Lease{
			ContentID: lease.GetContentId(), ContentVersion: lease.GetContentVersion(),
			ManifestSHA256: manifestSHA, PackageBytes: lease.GetPackageBytes(),
		},
		Manifest: manifest, Mode: a.Mode, SampleCount: a.SampleCount,
	}
	for attempt := 0; ; attempt++ {
		obs, err := verify.Audit(ctx, session, req)
		if err != nil {
			return w.write(classify.Reasons(classify.ConfigInvalid), nil)
		}
		if obs.Input == verify.InputOK {
			// An audit that made no request (every sampled segment is
			// empty) still needs an endpoint that passes the policy.
			session.open(ctx)
		}
		if out := session.refused; out != nil {
			return w.write(classify.Reasons(classify.Outcome(*out).Reason), &obs)
		}
		summary := classify.Observation(obs)
		if summary.Retry && attempt < p.cfg.StatusRetries {
			// A 4xx or 5xx other than 404 and 410 is FAIL only if it is
			// still the answer after every retry. A run budget that ends
			// first leaves the audit INCOMPLETE.
			if !p.sleep(ctx, p.backoff<<attempt) {
				return w.write(classify.Reasons(classify.RunBudgetExhausted), &obs)
			}
			continue
		}
		return w.write(summary, &obs)
	}
}

// lazySession opens the audit's endpoint on its first request.
// verify.Audit checks the media manifest against the lease before its
// first request, so a manifest that is invalid or does not match the lease
// sends no DNS query for the provider's host name.
type lazySession struct {
	fetcher    *fetch.Client
	providerID string
	url        string
	session    *fetch.Session
	// refused is the outcome of an Open that was not OK; nothing was
	// dialed, and every later request answers it again.
	refused *fetch.Outcome
}

// open opens the endpoint once: it applies the URL rules, resolves the
// host, and vets every address (fetch.Client.Open).
func (l *lazySession) open(ctx context.Context) {
	if l.session != nil || l.refused != nil {
		return
	}
	session, out := l.fetcher.Open(ctx, l.providerID, l.url)
	if out.Kind != fetch.OK {
		l.refused = &out
		return
	}
	l.session = session
}

// Get implements verify.Getter.
func (l *lazySession) Get(ctx context.Context, req fetch.Request) (fetch.Outcome, error) {
	l.open(ctx)
	if l.refused != nil {
		return *l.refused, nil
	}
	return l.session.Get(ctx, req)
}

// Close closes the session if it was opened.
func (l *lazySession) Close() {
	if l.session != nil {
		l.session.Close()
	}
}

// sleep waits d or until ctx ends; it reports whether d passed.
func (p *producer) sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

// endpoint returns the configured endpoint of an audit. With a capability
// manifest file, the manifest must parse, name the audit's provider and
// the provider record's current signing key id, and hash to the record's
// endpoint_manifest_digest; otherwise the audit is INPUT_CAPABILITY_INVALID.
// The detached manifest signature is not checked: protocol v0 defines no
// wire envelope for it (DR-07-09); the digest the provider account
// committed binds the body.
func (p *producer) endpoint(ctx context.Context, a config.Audit) (config.Endpoint, classify.Reason, error) {
	if a.CapabilityManifestPath == "" {
		return a.Endpoints[a.EndpointIndex], "", nil
	}
	raw, err := readBounded(a.CapabilityManifestPath, 1<<20)
	if err != nil {
		return config.Endpoint{}, classify.InputCapabilityInvalid, nil
	}
	m, err := capability.Parse(raw)
	if err != nil {
		return config.Endpoint{}, classify.InputCapabilityInvalid, nil
	}
	record, found, err := p.chain.Provider(ctx, a.ProviderID)
	if err != nil {
		return config.Endpoint{}, classify.ChainReadFailed, nil
	}
	d := record.GetEndpointManifestDigest()
	if !found || m.Manifest.ProviderID != a.ProviderID || m.Manifest.SigningKeyID != record.GetSigningKeyId() ||
		string(d.GetValue()) != string(m.SHA256[:]) || a.EndpointIndex >= len(m.Manifest.Endpoints) {
		return config.Endpoint{}, classify.InputCapabilityInvalid, nil
	}
	e := m.Manifest.Endpoints[a.EndpointIndex]
	return config.Endpoint{Name: e.Name, URL: e.URL}, "", nil
}

func readBounded(path string, limit int64) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, errors.New("not a regular file within the bound")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(raw)) > limit {
		return nil, errors.New("file above the bound")
	}
	return raw, nil
}

// writer writes the report of one audit of a held lease.
type writer struct {
	p            *producer
	a            config.Audit
	lease        *leasesv1.Lease
	manifest     [32]byte
	started      time.Time
	endpointName string
}

// write encodes and stores the report. obs is nil when the audit ended
// before verify.Audit ran. A sampled report whose audit ended before
// selection records a fresh seed that selected nothing (the report format
// requires one).
func (w *writer) write(s classify.Summary, obs *verify.Observation) (batch.RunAudit, error) {
	r := report.Report{
		AuditorKeyID: w.p.cfg.KeyID, AuditorAddress: hex.EncodeToString(w.p.cfg.AuditorAddress),
		LeaseID: w.a.LeaseID, ContentID: w.a.ContentID, ContentVersion: w.lease.GetContentVersion(),
		ProviderID: w.a.ProviderID, EndpointIndex: w.a.EndpointIndex,
		EndpointNameSHA256: sha256.Sum256([]byte(w.endpointName)), ExpectedManifestSHA256: w.manifest,
		Mode: string(w.a.Mode), CheckedSegments: s.CheckedSegments, FailedSegments: s.FailedSegments,
		FailedSegmentCount: s.FailedSegmentCount, Result: s.Result, Reasons: s.Reasons,
		StartedUnixSeconds: w.started.Unix(), FinishedUnixSeconds: max(w.p.now().Unix(), w.started.Unix()),
	}
	if obs != nil {
		r.BytesRead = obs.BytesRead
		r.SampleSeed = obs.Seed
	}
	if w.a.Mode == verify.SampledSegments && r.SampleSeed == nil {
		seed, err := verify.NewSeed(nil)
		if err != nil {
			return batch.RunAudit{}, err
		}
		r.SampleSeed = &seed
	}
	limitations, err := reportLimitations(w.p.catalog, w.a.Mode)
	if err != nil {
		return batch.RunAudit{}, err
	}
	r.Limitations = limitations
	raw, err := report.Encode(r)
	if err != nil {
		return batch.RunAudit{}, err
	}
	sum, err := w.p.store.PutReport(raw)
	if err != nil {
		return batch.RunAudit{}, err
	}
	return batch.RunAudit{
		LeaseID: w.a.LeaseID, ProviderID: w.a.ProviderID, ContentID: w.a.ContentID,
		ContentVersion: int64(w.lease.GetContentVersion()), ManifestSHA256: hex.EncodeToString(w.manifest[:]),
		Deadline: w.lease.GetDeadlineUnixSeconds(), Result: s.Result, Reasons: s.Reasons,
		ReportSHA256: hex.EncodeToString(sum[:]),
	}, nil
}

// Catalog limitations a report does not carry: the accounting sentence
// concerns lease accounting, not an audit observation, and the sampling
// sentence applies only to sampled audits.
const (
	accountingLimitation = "Accounting success is not physical capacity or operator independence."
	samplingLimitation   = "A sampled check covers only the sampled segments."
)

// reportLimitations are the catalog limitations a report of mode carries,
// in catalog order. More than report.MaxLimitations is an error, never a
// silent cut.
func reportLimitations(c labels.Catalog, mode verify.Mode) ([]string, error) {
	out := []string{}
	for _, text := range c.Limitations {
		if text == accountingLimitation || (text == samplingLimitation && mode != verify.SampledSegments) {
			continue
		}
		out = append(out, text)
	}
	if len(out) > report.MaxLimitations {
		return nil, fmt.Errorf("label catalog gives %d report limitations, the report holds %d", len(out), report.MaxLimitations)
	}
	return out, nil
}

// signOutput is what `sign` prints per batch.
type signOutput struct {
	RunID       string   `json:"run_id"`
	BatchID     string   `json:"batch_id"`
	ProviderID  string   `json:"provider_id"`
	Epoch       uint64   `json:"epoch"`
	BusinessID  string   `json:"business_id"`
	EvidenceIDs []string `json:"evidence_ids"`
}

// loadKey loads the configured key and checks its address.
func loadKey(cfg *config.Config) (keys.Key, error) {
	key, err := keys.Load(cfg.KeyPath)
	if err != nil {
		return keys.Key{}, err
	}
	if string(key.Address()) != string(cfg.AuditorAddress) {
		return keys.Key{}, errors.New("the key's address is not the configured auditor_address")
	}
	return key, nil
}

// cmdSign builds and signs the batches of a pending run. It uses no
// network: the round and expiry come from the header the run read, and the
// host clock can only add a refusal (batch.RoundOpen).
func cmdSign(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("sign", flag.ContinueOnError)
	runID := fs.String("run", "", "run id printed by `run`")
	cfg, err := loadConfig(fs, args, func() error {
		if *runID == "" {
			return usageError{"--run is required"}
		}
		return nil
	})
	if err != nil {
		return 0, err
	}
	st, err := store.Open(cfg.StoreRoot)
	if err != nil {
		return 0, err
	}
	key, err := loadKey(cfg)
	if err != nil {
		return 0, err
	}
	out, err := sign(cfg, st, key, *runID, time.Now().Unix())
	if err != nil {
		return 0, err
	}
	for _, o := range out {
		if err := emit(stdout, o); err != nil {
			return 0, err
		}
	}
	if len(out) == 0 {
		return exitOK, emit(stdout, map[string]any{"run_id": *runID, "batches": 0, "note": "no PASS or FAIL audit in this run"})
	}
	return exitOK, nil
}

func sign(cfg *config.Config, st *store.Store, key keys.Key, runID string, now int64) ([]signOutput, error) {
	raw, err := st.Get(store.Pending, runID, ".json")
	if err != nil {
		return nil, err
	}
	run, err := batch.ParseRun(raw, runID)
	if err != nil {
		return nil, err
	}
	if run.AuditorAddress != hex.EncodeToString(cfg.AuditorAddress) || run.AuditorKeyID != cfg.KeyID {
		return nil, errors.New("the run belongs to another auditor address or key id")
	}
	if err := batch.RoundOpen(run.HeaderTime, now, cfg.Window.Guard); err != nil {
		return nil, err
	}
	plan := batch.Plan{Signer: cfg.AuditorAddress, KeyID: cfg.KeyID, HeaderTime: run.HeaderTime, MaxItems: cfg.MaxBatchItems, Window: cfg.Window}
	var items []batch.Item
	for _, ra := range run.Audits {
		if ra.Result != report.ResultPass && ra.Result != report.ResultFail {
			continue
		}
		item, err := runItem(st, plan, ra)
		if err != nil {
			return nil, fmt.Errorf("lease %s: %w", ra.LeaseID, err)
		}
		items = append(items, item)
	}
	if len(items) == 0 {
		return nil, nil
	}
	batches, err := batch.Build(plan, items)
	if err != nil {
		return nil, err
	}
	out := make([]signOutput, 0, len(batches))
	for _, b := range batches {
		if err := batch.Sign(b, key); err != nil {
			return nil, err
		}
		if err := batch.Persist(st, b, runID); err != nil {
			return nil, err
		}
		o := signOutput{RunID: runID, BatchID: b.ID(), ProviderID: b.ProviderID, Epoch: b.Epoch(), BusinessID: b.Msg.GetBusinessId()}
		for _, it := range b.Items {
			o.EvidenceIDs = append(o.EvidenceIDs, it.EvidenceID)
		}
		out = append(out, o)
	}
	return out, nil
}

// runItem builds the item of one PASS or FAIL run entry from its stored
// report, which must describe the same lease and result.
func runItem(st *store.Store, plan batch.Plan, ra batch.RunAudit) (batch.Item, error) {
	sum, err := hex.DecodeString(ra.ReportSHA256)
	if err != nil || len(sum) != 32 {
		return batch.Item{}, errors.New("report digest")
	}
	manifest, err := hex.DecodeString(ra.ManifestSHA256)
	if err != nil || len(manifest) != 32 {
		return batch.Item{}, errors.New("manifest digest")
	}
	raw, err := st.Report([32]byte(sum))
	if err != nil {
		return batch.Item{}, err
	}
	item, err := batch.NewItem(plan, batch.Audit{Report: raw, Deadline: ra.Deadline, ManifestSHA256: [32]byte(manifest)})
	if err != nil {
		return batch.Item{}, err
	}
	if item.LeaseID != ra.LeaseID || item.ProviderID != ra.ProviderID || item.ContentID != ra.ContentID ||
		int64(item.ContentVersion) != ra.ContentVersion || item.Result != ra.Result {
		return batch.Item{}, errors.New("the stored report does not describe the run entry")
	}
	return item, nil
}

// submitOutput is what `submit` and `status` print per batch.
type submitOutput struct {
	chainclient.Result
	Label string `json:"label"`
	Note  string `json:"note"`
}

func batchFlags(name string, args []string) (*config.Config, multi, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	var ids multi
	fs.Var(&ids, "batch", "batch id printed by `sign` (repeatable)")
	cfg, err := loadConfig(fs, args, func() error {
		if len(ids) == 0 {
			return usageError{"--batch is required"}
		}
		return nil
	})
	return cfg, ids, err
}

const ackNote = "an acknowledgement is CheckTx only; only COMMITTED(0) with items read back means recorded"

// cmdSubmit broadcasts stored batches and waits for committed results. It
// talks only to the chain RPC.
func cmdSubmit(args []string, stdout io.Writer) (int, error) {
	cfg, ids, err := batchFlags("submit", args)
	if err != nil {
		return 0, err
	}
	st, err := store.Open(cfg.StoreRoot)
	if err != nil {
		return 0, err
	}
	key, err := loadKey(cfg)
	if err != nil {
		return 0, err
	}
	chain, err := newChain(cfg)
	if err != nil {
		return 0, err
	}
	s := &chainclient.Submitter{
		Client: chain, Store: st, Key: &key, Gas: cfg.GasLimit,
		Inclusion: time.Duration(cfg.Window.Inclusion) * time.Second, Interval: pollInterval, Window: cfg.Window,
	}
	return eachBatch(st, ids, stdout, func(b *batch.Batch, summary batch.Summary) (chainclient.Result, error) {
		return s.Submit(context.Background(), b, summary.RunID)
	})
}

// cmdStatus reports stored batches from the chain. It broadcasts nothing.
func cmdStatus(args []string, stdout io.Writer) (int, error) {
	cfg, ids, err := batchFlags("status", args)
	if err != nil {
		return 0, err
	}
	st, err := store.Open(cfg.StoreRoot)
	if err != nil {
		return 0, err
	}
	chain, err := newChain(cfg)
	if err != nil {
		return 0, err
	}
	s := &chainclient.Submitter{Client: chain, Store: st}
	return eachBatch(st, ids, stdout, func(b *batch.Batch, _ batch.Summary) (chainclient.Result, error) {
		return s.Status(context.Background(), b)
	})
}

func eachBatch(st *store.Store, ids []string, stdout io.Writer, do func(*batch.Batch, batch.Summary) (chainclient.Result, error)) (int, error) {
	code := exitOK
	for _, id := range ids {
		b, summary, err := batch.Load(st, id)
		if err != nil {
			return 0, fmt.Errorf("batch %s: %w", id, err)
		}
		res, err := do(b, summary)
		if err != nil {
			return 0, fmt.Errorf("batch %s: %w", id, err)
		}
		if !res.Recorded() {
			code = exitNotFinal
		}
		if err := emit(stdout, submitOutput{Result: res, Label: res.Label(), Note: ackNote}); err != nil {
			return 0, err
		}
	}
	return code, nil
}

// cmdVerify is the reviewer's check of one evidence id.
func cmdVerify(args []string, stdout io.Writer) (int, error) {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	id := fs.String("evidence-id", "", "evidence id")
	dir := fs.String("store", "", "producer store root")
	node := fs.String("node", "", "CometBFT RPC base URL to read the record from")
	file := fs.String("record", "", "record file (protobuf JSON) instead of a node")
	timeout := fs.Duration("timeout", 10*time.Second, "chain RPC timeout")
	if err := parseFlags(fs, args); err != nil {
		return 0, err
	}
	if *id == "" || *dir == "" || (*node == "") == (*file == "") {
		return 0, usageError{"--evidence-id, --store, and exactly one of --node and --record are required"}
	}
	// The reviewer reads an existing store; a missing root is an error,
	// not a new empty store.
	if _, err := os.Stat(*dir); err != nil {
		return 0, err
	}
	st, err := store.Open(*dir)
	if err != nil {
		return 0, err
	}
	catalog, err := labels.Embedded()
	if err != nil {
		return 0, err
	}
	in := review.Input{EvidenceID: *id, Store: st, Labels: &catalog}
	if *node != "" {
		rpc, err := chainclient.NewHTTP(*node, *timeout)
		if err != nil {
			return 0, err
		}
		chain, err := chainclient.New(rpc)
		if err != nil {
			return 0, err
		}
		in.Source = review.Source{Kind: review.SourceNode, Location: *node}
		if in.Record, in.Found, err = chain.Evidence(context.Background(), *id); err != nil {
			return 0, err
		}
	} else {
		in.Source = review.Source{Kind: review.SourceFile, Location: *file}
		if in.Record, err = readRecord(*file); err != nil {
			return 0, err
		}
		in.Found = true
	}
	res, err := review.Review(in)
	if err != nil {
		return 0, err
	}
	if err := emit(stdout, res); err != nil {
		return 0, err
	}
	if res.Verdict != review.Consistent {
		return exitNotFinal, nil
	}
	return exitOK, nil
}

// readRecord reads an evidence record in protobuf JSON; unknown fields are
// refused.
func readRecord(path string) (*evidencev1.EvidenceRecord, error) {
	raw, err := readBounded(path, maxRecordFileBytes)
	if err != nil {
		return nil, fmt.Errorf("record file: %w", err)
	}
	rec := &evidencev1.EvidenceRecord{}
	if err := (protojson.UnmarshalOptions{}).Unmarshal(raw, rec); err != nil {
		return nil, fmt.Errorf("record file: %w", err)
	}
	return rec, nil
}
