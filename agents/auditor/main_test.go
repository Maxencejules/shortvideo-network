package main

// Unit tests of the commands. Provider endpoints are loopback TLS servers
// with synthetic packages (fetchtest: "loopback test endpoint, synthetic
// package, not a provider agent"); the chain is chaintest, an in-memory
// fake of the RPC methods ("unit-test fake chain RPC; not a node, not
// end-to-end evidence"). Nothing here is end-to-end evidence; the devnet
// test runs the built binary against real nodes.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/Maxencejules/shortvideo-network/agents/auditor/batch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/chainclient"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/chainclient/chaintest"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/config"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/fetch/fetchtest"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/keys"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/labels"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/policy"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/report"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/review"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/store"
	"github.com/Maxencejules/shortvideo-network/agents/auditor/verify"
	"github.com/Maxencejules/shortvideo-network/chain/jcs"
	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
	"github.com/Maxencejules/shortvideo-network/chain/x/evidence"
	"github.com/Maxencejules/shortvideo-network/chain/x/providers/capability"
	contentv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1"
	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
	providersv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1"
	typesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1"
)

// headerTime is 20 minutes into epoch 490896 (2026-01-01).
const headerTime = int64(1767225600 + 1200)

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

// manifestJSON builds an svn-media-manifest-v1 for the segments.
func manifestJSON(t testing.TB, contentID string, segments []fetchtest.Segment) []byte {
	t.Helper()
	var items []any
	var total uint64
	for _, s := range segments {
		items = append(items, map[string]any{"segment_id": s.ID, "sha256": hex.EncodeToString(s.SHA256[:]), "byte_count": fmt.Sprint(len(s.Bytes))})
		total += uint64(len(s.Bytes))
	}
	text, err := jcs.Canonicalize(map[string]any{
		"schema_version": int64(1), "profile": "SVN-JCS-1", "manifest_id": "m-" + contentID,
		"content_id": contentID, "content_version": int64(1), "package_bytes": fmt.Sprint(total),
		"renditions": []any{map[string]any{"rendition_id": "r0", "codec": "avc1", "segments": items}},
	})
	if err != nil {
		t.Fatal(err)
	}
	return []byte(text)
}

// harness is one producer under test.
type harness struct {
	t       *testing.T
	dir     string
	node    *chaintest.Node
	chain   *chainclient.Client
	cfg     *config.Config
	key     keys.Key
	servers map[string]*fetchtest.Server
	ca      *fetchtest.CA
}

func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	key := testKey(t, "auditor-1")
	node := chaintest.New("svn-unit-1", headerTime)
	node.Accounts[hex.EncodeToString(key.Address())] = &chaintest.Account{Number: 3}
	chain, err := chainclient.New(node)
	if err != nil {
		t.Fatal(err)
	}
	ca := fetchtest.NewCA(t)
	fifo := filepath.Join(dir, "auditor-key.fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		ChainRPC: "http://127.0.0.1:1", CAFile: ca.WriteFile(t, dir), StoreRoot: filepath.Join(dir, "store"),
		Limits: fetch.Limits{Connect: 2 * time.Second, TLSHandshake: 2 * time.Second, ResponseHeader: 2 * time.Second,
			IdleRead: 2 * time.Second, Object: 10 * time.Second},
		RunBudget: time.Minute, AuditorAddress: key.Address(),
		// run must never open the key path: a FIFO would block it.
		KeyPath: fifo, KeyID: "key-auditor-1", Window: window, GasLimit: 2_000_000,
		MaxBatchItems: batch.MaxItems, StatusRetries: 2, RPCTimeout: 5 * time.Second,
	}
	t.Logf("%s; %s", fetchtest.Label, chaintest.Label)
	return &harness{t: t, dir: dir, node: node, chain: chain, cfg: cfg, key: key, servers: map[string]*fetchtest.Server{}, ca: ca}
}

// auditSpec is one audited lease and its endpoint.
type auditSpec struct {
	lease, provider, content string
	behavior                 fetchtest.Behavior
	sizes                    []int
	corrupt                  bool
	// lost serves an empty package: the provider holds none of its bytes.
	lost   bool
	mode   verify.Mode
	sample int
	// noEntry leaves the endpoint off the allowlist.
	noEntry bool
	// answer is the chain's content answer; nil is PENDING.
	answer *contentv1.QueryContentResponse
	// deadline offset from header time; 0 is 7200.
	deadline int64
}

// add registers a lease, its content, a manifest file, and a loopback
// endpoint, and configures the audit.
func (h *harness) add(s auditSpec) *fetchtest.Server {
	t := h.t
	t.Helper()
	if s.sizes == nil {
		s.sizes = []int{1000, 2000, 500, 1500}
	}
	if s.mode == "" {
		s.mode = verify.WholePackage
	}
	segments, pkg := fetchtest.Synthetic(s.lease, s.sizes)
	manifest := manifestJSON(t, s.content, segments)
	m, err := verify.ParseManifest(manifest)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(h.dir, s.lease+".manifest.json")
	if err := os.WriteFile(path, manifest, 0o600); err != nil {
		t.Fatal(err)
	}
	served := append([]byte(nil), pkg...)
	if s.corrupt {
		served[len(served)-1] ^= 0xff
	}
	if s.lost {
		served = nil
	}
	srv := fetchtest.Start(t, fetchtest.Config{CA: h.ca, ContentID: s.content, ContentVersion: 1, Package: served, Behavior: s.behavior})
	h.servers[s.lease] = srv
	deadline := s.deadline
	if deadline == 0 {
		deadline = 7200
	}
	h.node.Leases[s.provider+"/"+s.content] = &leasesv1.Lease{
		LeaseId: s.lease, ContentId: s.content, ContentVersion: 1, ProviderId: s.provider,
		ManifestDigest: &typesv1.Digest{Algorithm: typesv1.DigestAlgorithm_DIGEST_ALGORITHM_SHA256, Value: m.SHA256[:]},
		PackageBytes:   m.PackageBytes, DeadlineUnixSeconds: headerTime + deadline,
		State: leasesv1.LeaseState_LEASE_STATE_ACTIVE, ReservationHeld: true,
	}
	if s.answer != nil {
		h.node.Contents[s.content+"/1"] = s.answer
	} else {
		h.node.Contents[s.content+"/1"] = &contentv1.QueryContentResponse{Content: &contentv1.Content{
			ContentId: s.content, ContentVersion: 1, State: contentv1.ContentState_CONTENT_STATE_PENDING,
		}}
	}
	if !s.noEntry {
		h.cfg.Allowlist = append(h.cfg.Allowlist, policy.Entry{ProviderID: s.provider, Host: "127.0.0.1", IP: srv.Addr.Addr(), Port: srv.Addr.Port()})
	}
	h.cfg.Audits = append(h.cfg.Audits, config.Audit{
		LeaseID: s.lease, ProviderID: s.provider, ContentID: s.content, ManifestPath: path,
		Endpoints: []config.Endpoint{{Name: "loopback test endpoint", URL: srv.URL}}, Mode: s.mode, SampleCount: s.sample,
	})
	return srv
}

func (h *harness) producer() *producer {
	h.t.Helper()
	p, err := newProducer(h.cfg, h.chain)
	if err != nil {
		h.t.Fatal(err)
	}
	p.backoff = time.Millisecond
	return p
}

func (h *harness) run() runOutput {
	h.t.Helper()
	done := make(chan struct{})
	var out runOutput
	var err error
	go func() {
		defer close(done)
		out, err = h.producer().run(context.Background())
	}()
	select {
	case <-done:
	case <-time.After(90 * time.Second):
		h.t.Fatal("run did not finish (did it open the key path?)")
	}
	if err != nil {
		h.t.Fatal(err)
	}
	return out
}

func (h *harness) report(ra batch.RunAudit) report.Report {
	h.t.Helper()
	st, err := store.Open(h.cfg.StoreRoot)
	if err != nil {
		h.t.Fatal(err)
	}
	sum, err := hex.DecodeString(ra.ReportSHA256)
	if err != nil || len(sum) != 32 {
		h.t.Fatalf("report digest %q", ra.ReportSHA256)
	}
	raw, err := st.Report([32]byte(sum))
	if err != nil {
		h.t.Fatal(err)
	}
	r, err := report.Parse(raw)
	if err != nil {
		h.t.Fatal(err)
	}
	return r
}

// TestRunNeverOpensKeyPath: the key path is a FIFO; opening it for reading
// would block until a writer appears, and loading it as a key fails. run
// finishes and writes reports, so it never opened the key path.
func TestRunNeverOpensKeyPath(t *testing.T) {
	h := newHarness(t)
	h.add(auditSpec{lease: "l1", provider: "p1", content: "c1"})
	out := h.run()
	if len(out.Audits) != 1 || out.Audits[0].Result != report.ResultPass {
		t.Fatalf("%+v", out)
	}
	info, err := os.Lstat(h.cfg.KeyPath)
	if err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("key path changed: %v", err)
	}
	// The FIFO has no reader: a non-blocking writer open fails with ENXIO,
	// so no goroutine of run still holds it open.
	if f, err := os.OpenFile(h.cfg.KeyPath, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
		f.Close()
		t.Fatal("the key path FIFO has a reader")
	}
}

// TestRunClassifiesLoopbackOutcomes runs one audit per endpoint behavior
// through the real fetch path and checks the Q3 classification in the
// stored report.
func TestRunClassifiesLoopbackOutcomes(t *testing.T) {
	cases := []struct {
		name     string
		spec     auditSpec
		result   string
		reasons  []string
		requests int64
	}{
		{"correct", auditSpec{behavior: fetchtest.Correct}, report.ResultPass, nil, 1},
		{"corrupted segment", auditSpec{behavior: fetchtest.Correct, corrupt: true}, report.ResultFail, []string{"SEGMENT_DIGEST_MISMATCH"}, 1},
		{"404 at once", auditSpec{behavior: fetchtest.NotFound}, report.ResultFail, []string{"HTTP_STATUS"}, 1},
		{"500 after two retries", auditSpec{behavior: fetchtest.ServerError}, report.ResultFail, []string{"HTTP_STATUS"}, 3},
		{"declared short length", auditSpec{behavior: fetchtest.DeclareShort}, report.ResultFail, []string{"PACKAGE_LENGTH_MISMATCH"}, 1},
		{"chunked body ends early", auditSpec{behavior: fetchtest.TruncateClean}, report.ResultFail, []string{"PACKAGE_LENGTH_MISMATCH"}, 1},
		{"declared too large", auditSpec{behavior: fetchtest.OversizedDeclared}, report.ResultFail, []string{"TOO_LARGE"}, 1},
		{"chunked too large", auditSpec{behavior: fetchtest.OversizedChunked}, report.ResultFail, []string{"TOO_LARGE"}, 1},
		{"reset mid body", auditSpec{behavior: fetchtest.ResetMidBody}, report.ResultIncomplete, []string{"CONNECTION_RESET"}, 1},
		{"close-delimited truncation", auditSpec{behavior: fetchtest.TruncateCloseDelimited}, report.ResultIncomplete, []string{"CONNECTION_RESET"}, 1},
		{"cut inside a declared length", auditSpec{behavior: fetchtest.TruncateDeclared}, report.ResultIncomplete, []string{"CONNECTION_RESET"}, 1},
		{"gzip", auditSpec{behavior: fetchtest.Gzip}, report.ResultIncomplete, []string{"ENCODING_REFUSED"}, 1},
		{"redirect to metadata", auditSpec{behavior: fetchtest.Redirect}, report.ResultIncomplete, []string{"REDIRECT_REFUSED"}, 1},
		{"huge header", auditSpec{behavior: fetchtest.HugeHeader}, report.ResultIncomplete, []string{"PROTOCOL_ERROR"}, 1},
		{"sampled correct", auditSpec{behavior: fetchtest.Correct, mode: verify.SampledSegments, sample: 2}, report.ResultPass, nil, 2},
		{"sampled range ignored", auditSpec{behavior: fetchtest.IgnoreRange, mode: verify.SampledSegments, sample: 2}, report.ResultIncomplete, []string{"RANGE_UNSUPPORTED"}, 1},
		{"sampled wrong content-range total", auditSpec{behavior: fetchtest.WrongContentRangeTotal, mode: verify.SampledSegments, sample: 2}, report.ResultIncomplete, []string{"RANGE_UNSUPPORTED"}, 1},
		// A lost package: the whole-package answer declares 0 bytes, which
		// is FAIL; every range request is answered 416, which refuses the
		// range extension and stays INCOMPLETE (not retried).
		{"lost package, whole", auditSpec{behavior: fetchtest.Correct, lost: true}, report.ResultFail, []string{"PACKAGE_LENGTH_MISMATCH"}, 1},
		{"lost package, sampled range answered 416", auditSpec{behavior: fetchtest.Correct, lost: true, mode: verify.SampledSegments, sample: 2}, report.ResultIncomplete, []string{"RANGE_UNSUPPORTED"}, 1},
		{"no allowlist entry", auditSpec{behavior: fetchtest.Correct, noEntry: true}, report.ResultIncomplete, []string{"ENDPOINT_NOT_PERMITTED"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			c.spec.lease, c.spec.provider, c.spec.content = "l1", "p1", "c1"
			srv := h.add(c.spec)
			out := h.run()
			ra := out.Audits[0]
			reasons := c.reasons
			if reasons == nil {
				reasons = []string{}
			}
			if ra.Result != c.result || !reflect.DeepEqual(append([]string{}, ra.Reasons...), reasons) {
				t.Fatalf("%s %v, want %s %v", ra.Result, ra.Reasons, c.result, reasons)
			}
			if got := srv.Requests(); got != c.requests {
				t.Fatalf("%d requests, want %d", got, c.requests)
			}
			r := h.report(ra)
			if r.Result != c.result || r.LeaseID != "l1" || r.AuditorAddress != hex.EncodeToString(h.key.Address()) || r.AuditorKeyID != "key-auditor-1" {
				t.Fatalf("report %+v", r)
			}
			if r.EndpointNameSHA256 != sha256.Sum256([]byte("loopback test endpoint")) {
				t.Fatal("report endpoint name digest")
			}
		})
	}
}

// An untrusted certificate is TLS_FAILED, and a stalled server TIMEOUT:
// both INCOMPLETE, never FAIL.
func TestRunTLSAndTimeoutAreIncomplete(t *testing.T) {
	h := newHarness(t)
	h.add(auditSpec{lease: "l1", provider: "p1", content: "c1"})
	other := fetchtest.NewCA(t)
	h.cfg.CAFile = other.WriteFile(t, t.TempDir())
	out := h.run()
	if out.Audits[0].Result != report.ResultIncomplete || !reflect.DeepEqual(out.Audits[0].Reasons, []string{"TLS_FAILED"}) {
		t.Fatalf("untrusted CA: %+v", out.Audits[0])
	}
	h2 := newHarness(t)
	h2.add(auditSpec{lease: "l1", provider: "p1", content: "c1", behavior: fetchtest.StallHeaders})
	h2.cfg.Limits.ResponseHeader = 300 * time.Millisecond
	out = h2.run()
	if out.Audits[0].Result != report.ResultIncomplete || !reflect.DeepEqual(out.Audits[0].Reasons, []string{"TIMEOUT"}) {
		t.Fatalf("stalled server: %+v", out.Audits[0])
	}
}

// A retryable status with the run budget gone before the retries finish
// is INCOMPLETE, not FAIL.
func TestRunBudgetEndsRetriesIncomplete(t *testing.T) {
	h := newHarness(t)
	srv := h.add(auditSpec{lease: "l1", provider: "p1", content: "c1", behavior: fetchtest.ServerError})
	h.cfg.RunBudget = 1500 * time.Millisecond
	h.cfg.Limits.Object = time.Second
	h.cfg.Limits.Connect, h.cfg.Limits.TLSHandshake, h.cfg.Limits.ResponseHeader, h.cfg.Limits.IdleRead = time.Second, time.Second, time.Second, time.Second
	p := h.producer()
	p.backoff = 10 * time.Second
	out, err := p.run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if out.Audits[0].Result != report.ResultIncomplete || !reflect.DeepEqual(out.Audits[0].Reasons, []string{"RUN_BUDGET_EXHAUSTED"}) || srv.Requests() != 1 {
		t.Fatalf("%+v, %d requests", out.Audits[0], srv.Requests())
	}
}

// TestContentWithdrawnFetchesNothing: a withdrawn content (or a
// tombstone) is INCOMPLETE INPUT_CONTENT_WITHDRAWN and the endpoint
// receives no request. The other pre-fetch checks also fetch nothing.
func TestContentWithdrawnFetchesNothing(t *testing.T) {
	withdrawn := &contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "c1", ContentVersion: 1, State: contentv1.ContentState_CONTENT_STATE_WITHDRAWN}}
	tomb := &contentv1.QueryContentResponse{
		Content:   &contentv1.Content{ContentId: "c1", ContentVersion: 1, State: contentv1.ContentState_CONTENT_STATE_PENDING},
		Tombstone: &contentv1.Tombstone{ContentId: "c1", ContentVersion: 1},
	}
	cases := []struct {
		name   string
		spec   auditSpec
		edit   func(h *harness)
		reason string
		report bool
	}{
		{"withdrawn", auditSpec{answer: withdrawn}, nil, "INPUT_CONTENT_WITHDRAWN", true},
		{"tombstone", auditSpec{answer: tomb}, nil, "INPUT_CONTENT_WITHDRAWN", true},
		{"content not registered", auditSpec{}, func(h *harness) { delete(h.node.Contents, "c1/1") }, "INPUT_CONTENT_NOT_FOUND", true},
		{"deadline within guard + inclusion", auditSpec{deadline: 180}, nil, "LEASE_NEAR_DEADLINE", true},
		{"lease not held", auditSpec{}, func(h *harness) { delete(h.node.Leases, "p1/c1") }, "LEASE_NOT_HELD", false},
		{"lease ended", auditSpec{}, func(h *harness) { h.node.Leases["p1/c1"].State = leasesv1.LeaseState_LEASE_STATE_ENDED }, "LEASE_NOT_HELD", false},
		{"another lease id", auditSpec{}, func(h *harness) { h.node.Leases["p1/c1"].LeaseId = "l9" }, "LEASE_NOT_HELD", false},
		{"manifest of another package", auditSpec{}, func(h *harness) { h.node.Leases["p1/c1"].ManifestDigest.Value[0] ^= 1 }, "INPUT_MANIFEST_MISMATCH", true},
		{"manifest file missing", auditSpec{}, func(h *harness) { h.cfg.Audits[0].ManifestPath = filepath.Join(h.dir, "missing.json") }, "INPUT_MANIFEST_INVALID", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			c.spec.lease, c.spec.provider, c.spec.content = "l1", "p1", "c1"
			srv := h.add(c.spec)
			if c.edit != nil {
				c.edit(h)
			}
			ra := h.run().Audits[0]
			if ra.Result != report.ResultIncomplete || !reflect.DeepEqual(ra.Reasons, []string{c.reason}) {
				t.Fatalf("%+v", ra)
			}
			if srv.Requests() != 0 {
				t.Fatalf("%d requests reached the provider", srv.Requests())
			}
			if (ra.ReportSHA256 != "") != c.report {
				t.Fatalf("report written: %q", ra.ReportSHA256)
			}
			if c.report {
				if r := h.report(ra); r.Result != report.ResultIncomplete || r.BytesRead != 0 {
					t.Fatalf("report %+v", r)
				}
			}
		})
	}
}

// countingResolver maps every host name to 127.0.0.1 and counts lookups.
type countingResolver struct{ calls atomic.Int64 }

func (r *countingResolver) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	r.calls.Add(1)
	return []netip.Addr{netip.MustParseAddr("127.0.0.1")}, nil
}

// TestManifestCheckedBeforeResolution: the endpoint is a host name, so
// opening it sends a DNS query. A media manifest that is invalid or does
// not match the lease ends the audit before that query: the resolver is
// never called and the endpoint receives nothing. The correct audit shows
// that the resolver is the one in use.
func TestManifestCheckedBeforeResolution(t *testing.T) {
	cases := []struct {
		name    string
		edit    func(h *harness)
		result  string
		reasons []string
		lookups int64
	}{
		{"correct", nil, report.ResultPass, []string{}, 1},
		{"manifest of another package", func(h *harness) { h.node.Leases["p1/c1"].ManifestDigest.Value[0] ^= 1 }, report.ResultIncomplete, []string{"INPUT_MANIFEST_MISMATCH"}, 0},
		{"manifest not a manifest", func(h *harness) {
			if err := os.WriteFile(h.cfg.Audits[0].ManifestPath, []byte("{}"), 0o600); err != nil {
				h.t.Fatal(err)
			}
		}, report.ResultIncomplete, []string{"INPUT_MANIFEST_INVALID"}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newHarness(t)
			srv := h.add(auditSpec{lease: "l1", provider: "p1", content: "c1"})
			h.cfg.Allowlist = []policy.Entry{{ProviderID: "p1", Host: fetchtest.TestHost, IP: srv.Addr.Addr(), Port: srv.Addr.Port()}}
			h.cfg.Audits[0].Endpoints = []config.Endpoint{{Name: "loopback test endpoint", URL: srv.URLFor(fetchtest.TestHost)}}
			if c.edit != nil {
				c.edit(h)
			}
			p := h.producer()
			resolver := &countingResolver{}
			pol, err := policy.New(h.cfg.Allowlist)
			if err != nil {
				t.Fatal(err)
			}
			if p.fetcher, err = fetch.New(fetch.Options{Policy: pol, RootCAs: h.ca.Pool, Resolver: resolver, Limits: h.cfg.Limits}); err != nil {
				t.Fatal(err)
			}
			out, err := p.run(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			ra := out.Audits[0]
			if ra.Result != c.result || !reflect.DeepEqual(append([]string{}, ra.Reasons...), c.reasons) {
				t.Fatalf("%s %v, want %s %v", ra.Result, ra.Reasons, c.result, c.reasons)
			}
			if got := resolver.calls.Load(); got != c.lookups {
				t.Fatalf("%d lookups, want %d", got, c.lookups)
			}
			if c.lookups == 0 && srv.Requests() != 0 {
				t.Fatalf("%d requests reached the endpoint", srv.Requests())
			}
		})
	}
}

// TestEmptySampleStillVetsEndpoint: a sampled audit whose selected
// segments are all empty makes no request, yet it is never PASS for an
// endpoint the policy refuses. Each package has one 1-byte segment and 63
// empty ones and k = 1; a request reaches nothing either way, because the
// endpoint has no allowlist entry. Three audits make the all-empty
// selection all but certain to occur at least once.
func TestEmptySampleStillVetsEndpoint(t *testing.T) {
	sizes := make([]int, 64)
	sizes[0] = 1
	h := newHarness(t)
	var servers []*fetchtest.Server
	for _, id := range []string{"1", "2", "3"} {
		servers = append(servers, h.add(auditSpec{lease: "l" + id, provider: "p1", content: "c" + id, sizes: sizes,
			mode: verify.SampledSegments, sample: 1, noEntry: true}))
	}
	for i, ra := range h.run().Audits {
		if ra.Result != report.ResultIncomplete || !reflect.DeepEqual(ra.Reasons, []string{"ENDPOINT_NOT_PERMITTED"}) || servers[i].Requests() != 0 {
			t.Fatalf("audit %d: %+v, %d requests", i, ra, servers[i].Requests())
		}
	}
}

// capabilityFile writes a capability manifest naming the endpoint of srv
// and registers the provider record whose committed digest it matches.
func (h *harness) capabilityFile(provider string, srv *fetchtest.Server) string {
	h.t.Helper()
	r, err := capability.Encode(capability.Manifest{
		ProviderID: provider, SigningKeyID: "key-" + provider, DeclaredCapacityBytes: 1 << 30,
		Endpoints: []capability.Endpoint{{Name: "unused", URL: "https://198.51.100.7/"}, {Name: "loopback test endpoint", URL: srv.URL}},
	})
	if err != nil {
		h.t.Fatal(err)
	}
	path := filepath.Join(h.dir, provider+".capability.json")
	if err := os.WriteFile(path, []byte(r.Canonical), 0o600); err != nil {
		h.t.Fatal(err)
	}
	h.node.Providers[provider] = &providersv1.Provider{ProviderId: provider, SigningKeyId: "key-" + provider, EndpointManifestDigest: r.Digest()}
	return path
}

// With a capability manifest, the endpoint comes from the manifest only
// when it names the provider and its current key id and hashes to the
// committed endpoint_manifest_digest.
func TestCapabilityManifestEndpoint(t *testing.T) {
	setup := func() (*harness, *fetchtest.Server) {
		h := newHarness(t)
		srv := h.add(auditSpec{lease: "l1", provider: "p1", content: "c1"})
		h.cfg.Audits[0].CapabilityManifestPath = h.capabilityFile("p1", srv)
		h.cfg.Audits[0].Endpoints = nil
		h.cfg.Audits[0].EndpointIndex = 1
		return h, srv
	}
	h, srv := setup()
	if ra := h.run().Audits[0]; ra.Result != report.ResultPass || srv.Requests() != 1 {
		t.Fatalf("%+v", ra)
	}
	for name, edit := range map[string]func(h *harness){
		"digest differs":        func(h *harness) { h.node.Providers["p1"].EndpointManifestDigest.Value[0] ^= 1 },
		"retired key id":        func(h *harness) { h.node.Providers["p1"].SigningKeyId = "key-p1-v2" },
		"provider not on chain": func(h *harness) { delete(h.node.Providers, "p1") },
		"index past the list":   func(h *harness) { h.cfg.Audits[0].EndpointIndex = 2 },
		"not a manifest":        func(h *harness) { _ = os.WriteFile(h.cfg.Audits[0].CapabilityManifestPath, []byte("{}"), 0o600) },
	} {
		h, srv := setup()
		edit(h)
		ra := h.run().Audits[0]
		if ra.Result != report.ResultIncomplete || !reflect.DeepEqual(ra.Reasons, []string{"INPUT_CAPABILITY_INVALID"}) || srv.Requests() != 0 {
			t.Fatalf("%s: %+v, %d requests", name, ra, srv.Requests())
		}
	}
}

// TestProducerBytesRead measures the bytes read for a declared 1 MiB
// synthetic package of 64 segments: whole package versus 4 sampled
// segments. Per-fixture measurement on one machine; no throughput claim.
func TestProducerBytesRead(t *testing.T) {
	sizes := make([]int, 64)
	for i := range sizes {
		sizes[i] = 16384
	}
	h := newHarness(t)
	h.add(auditSpec{lease: "lw", provider: "p1", content: "cw", sizes: sizes})
	h.add(auditSpec{lease: "ls", provider: "p1", content: "cs", sizes: sizes, mode: verify.SampledSegments, sample: 4})
	out := h.run()
	whole, sampled := h.report(out.Audits[0]), h.report(out.Audits[1])
	if whole.Result != report.ResultPass || sampled.Result != report.ResultPass || whole.BytesRead != 1<<20 || sampled.BytesRead != 4*16384 ||
		whole.CheckedSegments != 64 || sampled.CheckedSegments != 4 || sampled.SampleSeed == nil {
		t.Fatalf("whole %d bytes %d segments; sampled %d bytes %d segments", whole.BytesRead, whole.CheckedSegments, sampled.BytesRead, sampled.CheckedSegments)
	}
	t.Logf("bytes read, 1 MiB package of 64 segments (one machine, loopback): WHOLE_PACKAGE %d, SAMPLED_SEGMENTS k=4 %d", whole.BytesRead, sampled.BytesRead)
}

// TestReportLimitations: reports carry the catalog limitations that apply
// to their mode, within the report bound; a sampled report adds the
// sampling sentence.
func TestReportLimitations(t *testing.T) {
	c, err := labels.Embedded()
	if err != nil {
		t.Fatal(err)
	}
	whole, err := reportLimitations(c, verify.WholePackage)
	if err != nil {
		t.Fatal(err)
	}
	sampled, err := reportLimitations(c, verify.SampledSegments)
	if err != nil {
		t.Fatal(err)
	}
	if len(sampled) != len(whole)+1 || len(sampled) > report.MaxLimitations || !contains(sampled, samplingLimitation) || contains(whole, samplingLimitation) ||
		contains(whole, accountingLimitation) || !contains(c.Limitations, accountingLimitation) || !contains(whole, "Auditors can lie.") {
		t.Fatalf("whole %v, sampled %v", whole, sampled)
	}
	tooMany := c
	tooMany.Limitations = append(append([]string{}, c.Limitations...), "a", "b")
	if _, err := reportLimitations(tooMany, verify.SampledSegments); err == nil {
		t.Fatal("limitations above the report bound were cut silently")
	}
	h := newHarness(t)
	h.add(auditSpec{lease: "l1", provider: "p1", content: "c1"})
	if r := h.report(h.run().Audits[0]); !reflect.DeepEqual(r.Limitations, whole) {
		t.Fatalf("report limitations %v", r.Limitations)
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// TestRunSignSubmitReviewUnit runs the whole producer flow against the
// fake chain: run (PASS, FAIL, INCOMPLETE), sign (one batch, two items),
// submit (committed by the fake), status, and the reviewer.
func TestRunSignSubmitReviewUnit(t *testing.T) {
	h := newHarness(t)
	h.add(auditSpec{lease: "l1", provider: "p1", content: "c1"})
	h.add(auditSpec{lease: "l2", provider: "p1", content: "c2", corrupt: true})
	withdrawn := &contentv1.QueryContentResponse{Content: &contentv1.Content{ContentId: "c3", ContentVersion: 1, State: contentv1.ContentState_CONTENT_STATE_WITHDRAWN}}
	h.add(auditSpec{lease: "l3", provider: "p1", content: "c3", answer: withdrawn})
	h.add(auditSpec{lease: "l4", provider: "p2", content: "c4", behavior: fetchtest.NotFound})
	out := h.run()
	results := map[string]string{}
	for _, a := range out.Audits {
		results[a.LeaseID] = a.Result
	}
	if !reflect.DeepEqual(results, map[string]string{"l1": "PASS", "l2": "FAIL", "l3": "INCOMPLETE", "l4": "FAIL"}) {
		t.Fatalf("%v", results)
	}
	if out.Epoch != int64(evidence.EpochAt(headerTime)) {
		t.Fatalf("epoch %d", out.Epoch)
	}
	st, err := store.Open(h.cfg.StoreRoot)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := sign(h.cfg, st, h.key, out.RunID, headerTime+30)
	if err != nil {
		t.Fatal(err)
	}
	if len(signed) != 2 || signed[0].ProviderID != "p1" || len(signed[0].EvidenceIDs) != 2 || signed[1].ProviderID != "p2" || len(signed[1].EvidenceIDs) != 1 {
		t.Fatalf("%+v", signed)
	}
	// Signing again is idempotent: the same bytes under the same names.
	again, err := sign(h.cfg, st, h.key, out.RunID, headerTime+40)
	if err != nil || !reflect.DeepEqual(again, signed) {
		t.Fatalf("second sign %+v %v", again, err)
	}
	b, _, err := batch.Load(st, signed[0].BatchID)
	if err != nil {
		t.Fatal(err)
	}
	for i, it := range b.Items {
		r := h.report(batch.RunAudit{ReportSHA256: hex.EncodeToString(it.ReportSHA256[:])})
		want := h.node.Leases["p1/"+it.ContentID].GetManifestDigest().GetValue()
		if r.Result == report.ResultFail {
			want = it.ReportSHA256[:]
		}
		if !bytes.Equal(b.Msg.GetItems()[i].GetObservationDigest().GetValue(), want) || it.EvidenceID != batch.EvidenceID(h.key.Address(), it.ReportSHA256) {
			t.Fatalf("item %d digest or id", i)
		}
		if exp := b.Msg.GetItems()[i].GetExpiryUnixSeconds(); exp != headerTime+7200 {
			t.Fatalf("expiry %d", exp)
		}
	}
	if code := evidence.VerifyAuditSignature(b.Msg); code != msgkit.OK {
		t.Fatalf("verify %s", code)
	}
	// Submit both batches; the fake commits them.
	h.node.OnBroadcast = func(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) {
		n.Record(msg)
		n.Commit(hash, 0, "")
	}
	key := h.key
	sub := &chainclient.Submitter{Client: h.chain, Store: st, Key: &key, Gas: h.cfg.GasLimit,
		Inclusion: time.Second, Interval: 5 * time.Millisecond, Window: h.cfg.Window}
	var buf bytes.Buffer
	code, err := eachBatch(st, []string{signed[0].BatchID, signed[1].BatchID}, &buf, func(b *batch.Batch, s batch.Summary) (chainclient.Result, error) {
		return sub.Submit(context.Background(), b, s.RunID)
	})
	if err != nil || code != exitOK || strings.Count(buf.String(), `"label":"COMMITTED(0)"`) != 2 {
		t.Fatalf("submit %d %v: %s", code, err, buf.String())
	}
	// The reviewer finds each record CONSISTENT, as an attestation.
	catalog, _ := labels.Embedded()
	for _, id := range append(append([]string{}, signed[0].EvidenceIDs...), signed[1].EvidenceIDs...) {
		rec, found, err := h.chain.Evidence(context.Background(), id)
		if err != nil || !found {
			t.Fatalf("%s: %v", id, err)
		}
		res, err := review.Review(review.Input{EvidenceID: id, Source: review.Source{Kind: review.SourceNode, Location: "fake"}, Record: rec, Found: true, Store: st, Labels: &catalog})
		if err != nil || res.Verdict != review.Consistent {
			t.Fatalf("%s: %+v %v", id, res, err)
		}
	}
	// The reviewer command reads a record file.
	rec, _, _ := h.chain.Evidence(context.Background(), signed[1].EvidenceIDs[0])
	recPath := filepath.Join(h.dir, "record.json")
	raw, _ := protojson.Marshal(rec)
	if err := os.WriteFile(recPath, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := cli([]string{"verify", "--evidence-id", rec.GetEvidenceId(), "--store", h.cfg.StoreRoot, "--record", recPath}, &stdout, &stderr); code != exitOK {
		t.Fatalf("verify exit %d: %s %s", code, stdout.String(), stderr.String())
	}
	var res review.Result
	if err := json.Unmarshal(stdout.Bytes(), &res); err != nil || res.Verdict != review.Consistent || res.RecordSource != "FILE "+recPath || res.StateLabel != "Rejected" {
		t.Fatalf("%s %v", stdout.String(), err)
	}
	// After deleting its report the same id is PAYLOAD_UNAVAILABLE (exit 3).
	if err := os.Remove(filepath.Join(h.cfg.StoreRoot, "reports", out.Audits[3].ReportSHA256+".json")); err != nil {
		t.Fatal(err)
	}
	stdout.Reset()
	if code := cli([]string{"verify", "--evidence-id", rec.GetEvidenceId(), "--store", h.cfg.StoreRoot, "--record", recPath}, &stdout, &stderr); code != exitNotFinal || !strings.Contains(stdout.String(), `"verdict":"PAYLOAD_UNAVAILABLE"`) {
		t.Fatalf("exit %d %s", code, stdout.String())
	}
}

// sign refuses a round that closes within the guard (by the run's header
// time or the host clock), a run of another auditor, and a key whose
// address is not the configured one.
func TestSignRefusals(t *testing.T) {
	h := newHarness(t)
	h.add(auditSpec{lease: "l1", provider: "p1", content: "c1"})
	out := h.run()
	st, _ := store.Open(h.cfg.StoreRoot)
	end := (int64(evidence.EpochAt(headerTime))+1)*evidence.EpochSeconds - h.cfg.Window.Guard
	if _, err := sign(h.cfg, st, h.key, out.RunID, end); !errors.Is(err, batch.ErrRoundClosing) {
		t.Fatalf("host clock at the guard: %v", err)
	}
	if _, err := sign(h.cfg, st, h.key, out.RunID, end+evidence.EpochSeconds); !errors.Is(err, batch.ErrRoundClosing) {
		t.Fatalf("expired round: %v", err)
	}
	if _, err := sign(h.cfg, st, h.key, out.RunID, end-1); err != nil {
		t.Fatalf("effective-1: %v", err)
	}
	other := *h.cfg
	other.AuditorAddress = testKey(t, "auditor-2").Address()
	if _, err := sign(&other, st, testKey(t, "auditor-2"), out.RunID, headerTime); err == nil {
		t.Fatal("signed another auditor's run")
	}
	if _, err := sign(h.cfg, st, h.key, "run-"+strings.Repeat("0", 48), headerTime); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unknown run: %v", err)
	}
	// A run read inside the guard is refused by its header time alone.
	h2 := newHarness(t)
	h2.node.Time = end
	h2.add(auditSpec{lease: "l1", provider: "p1", content: "c1", deadline: 7200 - (end - headerTime)})
	out2 := h2.run()
	st2, _ := store.Open(h2.cfg.StoreRoot)
	if _, err := sign(h2.cfg, st2, h2.key, out2.RunID, end-1000); !errors.Is(err, batch.ErrRoundClosing) {
		t.Fatalf("header at the guard: %v", err)
	}
	// loadKey refuses a key of another address.
	path := filepath.Join(t.TempDir(), "k.hex")
	if _, err := keys.Generate(path); err != nil {
		t.Fatal(err)
	}
	cfg := *h.cfg
	cfg.KeyPath = path
	if _, err := loadKey(&cfg); err == nil {
		t.Fatal("a key of another address was accepted")
	}
}

func TestCLIUsageAndKeygen(t *testing.T) {
	var stdout, stderr bytes.Buffer
	for _, args := range [][]string{
		nil, {"nope"}, {"run"}, {"sign", "--config", "x"}, {"submit", "--config", "x"}, {"status", "--config", "x"},
		{"verify", "--evidence-id", "a", "--store", "s"}, {"verify", "--evidence-id", "a", "--store", "s", "--node", "http://x", "--record", "r"},
		{"keygen"}, {"run", "--config", "x", "extra"}, {"run", "--bogus"},
	} {
		if code := cli(args, &stdout, &stderr); code != exitUsage {
			t.Fatalf("%v: exit %d", args, code)
		}
	}
	path := filepath.Join(t.TempDir(), "auditor.hex")
	stdout.Reset()
	if code := cli([]string{"keygen", "--key", path}, &stdout, &stderr); code != exitOK {
		t.Fatalf("keygen exit %d: %s", code, stderr.String())
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stdout.String(), string(raw[:64])) {
		t.Fatal("keygen printed the seed")
	}
	key, err := keys.Load(path)
	if err != nil || !strings.Contains(stdout.String(), hex.EncodeToString(key.Address())) {
		t.Fatalf("keygen output %s (%v)", stdout.String(), err)
	}
	stderr.Reset()
	if code := cli([]string{"keygen", "--key", path}, &stdout, &stderr); code != exitError || !strings.Contains(stderr.String(), "already exists") {
		t.Fatalf("second keygen exit %d: %s", code, stderr.String())
	}
	if code := cli([]string{"run", "--config", filepath.Join(t.TempDir(), "missing.json")}, &stdout, &stderr); code != exitError {
		t.Fatalf("missing config exit %d", code)
	}
}

// configFile writes h.cfg as an operator configuration file.
func (h *harness) configFile(chainRPC, keyPath string) string {
	h.t.Helper()
	var allow, audits []any
	for _, e := range h.cfg.Allowlist {
		allow = append(allow, map[string]any{"provider_id": e.ProviderID, "host": e.Host, "ip": e.IP.String(), "port": e.Port})
	}
	for _, a := range h.cfg.Audits {
		var eps []any
		for _, e := range a.Endpoints {
			eps = append(eps, map[string]any{"name": e.Name, "url": e.URL})
		}
		audits = append(audits, map[string]any{
			"lease_id": a.LeaseID, "provider_id": a.ProviderID, "content_id": a.ContentID, "endpoint_index": a.EndpointIndex,
			"manifest_path": a.ManifestPath, "endpoints": eps, "mode": string(a.Mode),
		})
	}
	doc := map[string]any{
		"chain_rpc": chainRPC, "ca_file": h.cfg.CAFile, "store_root": h.cfg.StoreRoot, "allowlist": allow, "audits": audits,
		"limits": map[string]any{"connect_timeout_ms": 2000, "tls_handshake_timeout_ms": 2000, "response_header_timeout_ms": 2000,
			"idle_read_timeout_ms": 2000, "object_budget_ms": 10000, "run_budget_ms": 60000},
		"auditor_address": hex.EncodeToString(h.key.Address()), "key_path": keyPath, "key_id": h.cfg.KeyID,
		"epoch_guard_seconds": h.cfg.Window.Guard, "inclusion_timeout_seconds": 5, "http_status_retries": 1,
	}
	raw, err := json.Marshal(doc)
	if err != nil {
		h.t.Fatal(err)
	}
	path := filepath.Join(h.dir, fmt.Sprintf("config-%d.json", len(raw)))
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		h.t.Fatal(err)
	}
	return path
}

func cliRun(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	done := make(chan int)
	go func() { done <- cli(args, &stdout, &stderr) }()
	select {
	case code := <-done:
		return code, stdout.String(), stderr.String()
	case <-time.After(90 * time.Second):
		t.Fatalf("auditor %v did not finish", args)
		return 0, "", ""
	}
}

// writeKeyFile writes the harness key in the key file format, mode 0600.
func (h *harness) writeKeyFile() string {
	h.t.Helper()
	path := filepath.Join(h.dir, "auditor-1.hex")
	seed := sha256.Sum256([]byte("svn-evidence-test-seed:auditor-1"))
	if err := os.WriteFile(path, []byte(hex.EncodeToString(append(seed[:], h.key.PublicKey()...))+"\n"), 0o600); err != nil {
		h.t.Fatal(err)
	}
	return path
}

// TestSignAndStatusKeepToTheirResources runs `sign` and `status` as
// commands. `sign` reads the host clock, so the fake chain's header time is
// the host's time, moved past an epoch boundary that is too close.
//
//   - `sign` uses no network: with chain_rpc pointing at the fake node, it
//     signs the run's batch and the node sees no call
//   - `status` never opens the key: its key_path is a FIFO, which an open
//     for reading would block on
func TestSignAndStatusKeepToTheirResources(t *testing.T) {
	h := newHarness(t)
	h.cfg.Window.Guard = 30
	now := time.Now().Unix()
	if into := now % evidence.EpochSeconds; into >= evidence.EpochSeconds-h.cfg.Window.Guard-15 {
		wait := time.Duration(evidence.EpochSeconds-into+1) * time.Second
		t.Logf("waiting %s for the next epoch: sign refuses within the guard of an epoch end", wait)
		time.Sleep(wait)
		now = time.Now().Unix()
	}
	h.node.Time = now
	h.add(auditSpec{lease: "l1", provider: "p1", content: "c1"})
	h.add(auditSpec{lease: "l2", provider: "p1", content: "c2", corrupt: true})
	for _, l := range h.node.Leases {
		l.DeadlineUnixSeconds = now + 7200
	}
	srv := chaintest.Serve(t, h.node)
	fifoConfig := h.configFile(srv.URL, h.cfg.KeyPath)
	keyConfig := h.configFile(srv.URL, h.writeKeyFile())

	code, stdout, stderr := cliRun(t, "run", "--config", fifoConfig)
	var out runOutput
	if err := json.Unmarshal([]byte(stdout), &out); code != exitOK || err != nil {
		t.Fatalf("run exit %d: %s %s (%v)", code, stdout, stderr, err)
	}
	h.node.Lock()
	calls := len(h.node.Calls)
	h.node.Unlock()
	code, stdout, stderr = cliRun(t, "sign", "--config", keyConfig, "--run", out.RunID)
	if code != exitOK {
		t.Fatalf("sign exit %d: %s %s", code, stdout, stderr)
	}
	lines := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
	var signed signOutput
	if len(lines) != 1 || json.Unmarshal([]byte(lines[0]), &signed) != nil || signed.RunID != out.RunID || len(signed.EvidenceIDs) != 2 {
		t.Fatalf("sign output %q", stdout)
	}
	h.node.Lock()
	after := h.node.Calls[calls:]
	h.node.Unlock()
	if len(after) != 0 {
		t.Fatalf("sign called the chain RPC: %v", after)
	}

	code, stdout, stderr = cliRun(t, "status", "--config", fifoConfig, "--batch", signed.BatchID)
	if code != exitNotFinal || !strings.Contains(stdout, `"label":"NOT_SUBMITTED"`) {
		t.Fatalf("status exit %d: %s %s", code, stdout, stderr)
	}
	if f, err := os.OpenFile(h.cfg.KeyPath, os.O_WRONLY|syscall.O_NONBLOCK, 0); err == nil {
		f.Close()
		t.Fatal("the key path FIFO has a reader")
	}
}

// TestCommandsAgainstTheFakeNode drives the command line through the real
// CometBFT HTTP client against the fake chain served over loopback
// JSON-RPC: run (the key path is a FIFO it must not open), sign, submit
// (an acknowledgement first, then a commit), status, and verify --node.
// Unit test: the chain is chaintest, not a node.
func TestCommandsAgainstTheFakeNode(t *testing.T) {
	h := newHarness(t)
	h.add(auditSpec{lease: "l1", provider: "p1", content: "c1"})
	h.add(auditSpec{lease: "l2", provider: "p1", content: "c2", corrupt: true})
	srv := chaintest.Serve(t, h.node)

	code, stdout, stderr := cliRun(t, "run", "--config", h.configFile(srv.URL, h.cfg.KeyPath))
	if code != exitOK {
		t.Fatalf("run exit %d: %s", code, stderr)
	}
	var out runOutput
	if err := json.Unmarshal([]byte(stdout), &out); err != nil || len(out.Audits) != 2 || out.HeaderTime != headerTime {
		t.Fatalf("run output %s (%v)", stdout, err)
	}
	if info, err := os.Lstat(h.cfg.KeyPath); err != nil || info.Mode()&os.ModeNamedPipe == 0 {
		t.Fatalf("key path FIFO changed: %v", err)
	}

	cfgPath := h.configFile(srv.URL, h.writeKeyFile())
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatal(err)
	}
	st, err := store.Open(cfg.StoreRoot)
	if err != nil {
		t.Fatal(err)
	}
	key, err := loadKey(cfg)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := sign(cfg, st, key, out.RunID, headerTime+30)
	if err != nil || len(signed) != 1 {
		t.Fatalf("sign %+v %v", signed, err)
	}
	id := signed[0].BatchID

	// No block commits it: an acknowledgement only, exit 3.
	code, stdout, stderr = cliRun(t, "submit", "--config", cfgPath, "--batch", id)
	if code != exitNotFinal || !strings.Contains(stdout, `"label":"SUBMITTED"`) || strings.Contains(stdout, `"RECORDED"`) {
		t.Fatalf("submit exit %d: %s %s", code, stdout, stderr)
	}
	// The next broadcast commits.
	h.node.Lock()
	h.node.OnBroadcast = func(n *chaintest.Node, msg *evidencev1.MsgSubmitAuditBatch, hash string) {
		n.Record(msg)
		n.Commit(hash, 0, "")
	}
	h.node.Unlock()
	code, stdout, stderr = cliRun(t, "submit", "--config", cfgPath, "--batch", id)
	if code != exitOK || !strings.Contains(stdout, `"label":"COMMITTED(0)"`) {
		t.Fatalf("second submit exit %d: %s %s", code, stdout, stderr)
	}
	code, stdout, _ = cliRun(t, "status", "--config", cfgPath, "--batch", id)
	if code != exitOK || !strings.Contains(stdout, `"label":"COMMITTED(0)"`) {
		t.Fatalf("status exit %d: %s", code, stdout)
	}
	for _, evidenceID := range signed[0].EvidenceIDs {
		code, stdout, stderr = cliRun(t, "verify", "--evidence-id", evidenceID, "--store", cfg.StoreRoot, "--node", srv.URL)
		var res review.Result
		if err := json.Unmarshal([]byte(stdout), &res); err != nil || code != exitOK || res.Verdict != review.Consistent || res.RecordSource != "NODE "+srv.URL {
			t.Fatalf("verify exit %d: %s %s", code, stdout, stderr)
		}
	}
	code, stdout, _ = cliRun(t, "verify", "--evidence-id", evidence.IDPrefix(h.key.Address())+"absent", "--store", cfg.StoreRoot, "--node", srv.URL)
	if code != exitNotFinal || !strings.Contains(stdout, `"verdict":"NOT_ON_CHAIN"`) {
		t.Fatalf("absent id exit %d: %s", code, stdout)
	}
	if code, _, _ := cliRun(t, "verify", "--evidence-id", signed[0].EvidenceIDs[0], "--store", filepath.Join(h.dir, "no-store"), "--node", srv.URL); code != exitError {
		t.Fatalf("missing store root exit %d", code)
	}
}
