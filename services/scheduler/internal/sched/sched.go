// Package sched coordinates publication and repair.
// It reads committed lease rows and posts offers. Providers sign their own
// acceptances. This package does not sign, does not broadcast, and does not
// treat a promise or a node count as durable storage.
package sched

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/policy"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/readmodel"
	"github.com/Maxencejules/shortvideo-network/services/scheduler/internal/stage"
)

// ErrLastCopy means a planned departure would remove a copy that is still
// needed to meet the publication target.
var ErrLastCopy = errors.New("sched: last needed copy")

// Host is one operator record. Declared operator and domain are claims.
type Host struct {
	ProviderID       string `json:"provider_id"`
	Endpoint         string `json:"endpoint"`
	CAFile           string `json:"ca_file"`
	DeclaredOperator string `json:"declared_operator"`
	DeclaredDomain   string `json:"declared_failure_domain"`
	Simulated        bool   `json:"simulated_labels"`
	PolicyID         string `json:"policy_id"`
	Capacity         uint64 `json:"declared_capacity_bytes"`
	Role             string `json:"role"`
	AdminToken       string `json:"-"`
	AdminTokenFile   string `json:"admin_token_file,omitempty"`
}

// Package is the off-chain object a publication refers to.
type Package struct {
	ContentID   string
	Version     uint64
	Segments    []policy.Segment
	ExpectedSHA string
	TermsHex    string
	Deadline    int64
	// ManifestHex is the media manifest digest. When it is empty the offer
	// uses the scheduler package text, which is the goal 17 behaviour.
	ManifestHex string
	// Scheduler is the chain account stored on the offer. Empty keeps the
	// goal 17 filler. A real offer stores the admitted service signer.
	Scheduler []byte
	// TermSeconds is the devnet DEC-005 test duration. Zero keeps start as
	// the wall clock and Deadline as given.
	TermSeconds int64
}

// Posted is an offer the scheduler sent. Broadcast is not a commit.
type Posted struct {
	OfferID             string
	LeaseID             string
	ProviderID          string
	ContentID           string
	Version             uint64
	PackageBytes        uint64
	Broadcast           bool
	ResponseCommitted   bool
	CountedFromResponse bool
}

// LeaseState is the committed row. It is not a reachability observation.
type LeaseState struct {
	LeaseID    string `json:"lease_id"`
	ProviderID string `json:"provider_id"`
	State      string `json:"state"`
	Active     bool   `json:"active"`
	Deadline   int64  `json:"deadline_unix"`
}

// View keeps reachability, evidence, and lease state apart.
type View struct {
	ContentID                       string       `json:"content_id"`
	Publication                     string       `json:"publication"`
	PublicationReason               string       `json:"publication_reason,omitempty"`
	ApprovalID                      string       `json:"approval_id,omitempty"`
	CommunityApproval               bool         `json:"community_approval"`
	EvidenceMode                    string       `json:"evidence_mode"`
	ClaimLimit                      string       `json:"claim_limit,omitempty"`
	Reachability                    string       `json:"reachability"`
	AtRisk                          bool         `json:"at_risk"`
	LeaseState                      []LeaseState `json:"lease_state"`
	ActiveCommitments               int          `json:"active_commitments"`
	DistinctOperators               int          `json:"distinct_operators"`
	VerifiedTransfers               int          `json:"verified_transfers"`
	CountedReplicas                 int          `json:"counted_replicas"`
	OriginBackupTracked             bool         `json:"origin_backup_tracked"`
	OriginCounted                   bool         `json:"origin_counted"`
	PhysicalFailureDomains          []string     `json:"physical_failure_domains"`
	PhysicalFailureDomainCount      int          `json:"physical_failure_domain_count"`
	DeclaredFailureDomains          []string     `json:"declared_failure_domains"`
	SimulatedLabels                 bool         `json:"simulated_labels"`
	UnverifiedOperatorClaim         bool         `json:"unverified_operator_claim"`
	IndependentOperatorVerification bool         `json:"independent_operator_verification"`
	ServiceLabel                    string       `json:"service_label"`
	Renewal                         string       `json:"renewal"`
	Queued                          []string     `json:"queued,omitempty"`
	SchedulerSigned                 bool         `json:"scheduler_signed"`
	StorageProof                    bool         `json:"storage_proof"`
	ReachableProviders              []string     `json:"reachable_providers"`
	DegradedProviders               []string     `json:"degraded_providers"`
}

// Config is the process configuration. Now, when set, is the test clock.
type Config struct {
	Target            int
	PolicyID          string
	NoticeSeconds     int64
	EvidenceMaxAge    time.Duration
	CooldownSeconds   int64
	MaxRepairAttempts int
	ProbeAttempts     int
	PhysicalDomain    string
	IndexerURL        string
	Stage             *stage.Stage
	// OfferCommitURL, when set, receives the offer JSON and must answer
	// 200 or 204 before the provider is asked to sign. Empty skips the call.
	// The scheduler still does not sign and does not broadcast.
	OfferCommitURL string
	// ConfirmFor is how long Publish and Repair wait for a lease row.
	// Zero keeps the 20 second default.
	ConfirmFor time.Duration
	// PackageRoot is the only directory a publication request may read.
	PackageRoot string
	Hosts       []Host
	Approver    func(providerID, offerID string) error
	Now         func() time.Time
	// Control authorizes the four mutating routes. Nil refuses them.
	// It is an application operator session, not a chain key.
	Control func(r *http.Request) bool
}

// Sched is the coordinator.
type Sched struct {
	cfg      Config
	indexer  readmodel.Client
	mu       sync.Mutex
	down     map[string]bool
	draining map[string]bool
	jobs     map[string]*policy.Job
	posted   []Posted
	pkgs     map[string]Package
	tokens   map[string]string
	evidence map[string]ev
	queued   []string
	origin   map[string]bool
	holds    map[string]map[string]hold
	traces   []string
	views    map[string]View
	before   func()
	clients  map[string]*http.Client
}

type ev struct {
	mode    string
	limit   string
	claimed string
}

type hold struct {
	leaseID   string
	reachable bool
	verified  bool
}

// New builds a coordinator. Independent-operator verification stays false.
func New(cfg Config) *Sched {
	if cfg.Target == 0 {
		cfg.Target = policy.ReplicaTarget
	}
	if cfg.MaxRepairAttempts == 0 {
		cfg.MaxRepairAttempts = 3
	}
	if cfg.ProbeAttempts == 0 {
		cfg.ProbeAttempts = 2
	}
	if cfg.EvidenceMaxAge == 0 {
		cfg.EvidenceMaxAge = time.Minute
	}
	if cfg.CooldownSeconds == 0 {
		cfg.CooldownSeconds = 60
	}
	return &Sched{
		cfg: cfg, indexer: readmodel.Client{Base: cfg.IndexerURL},
		down: map[string]bool{}, draining: map[string]bool{}, jobs: map[string]*policy.Job{},
		pkgs: map[string]Package{}, tokens: map[string]string{}, evidence: map[string]ev{},
		origin: map[string]bool{}, holds: map[string]map[string]hold{}, views: map[string]View{},
		clients: map[string]*http.Client{},
	}
}

// SetBeforeOffer runs fn immediately before a repair offer is posted.
func (s *Sched) SetBeforeOffer(fn func()) {
	s.mu.Lock()
	s.before = fn
	s.mu.Unlock()
}

// UpdateHost sets the endpoint and CA file for one host. It does not sign.
func (s *Sched) UpdateHost(id, endpoint, caFile, adminToken string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Hosts {
		if s.cfg.Hosts[i].ProviderID == id {
			if endpoint != "" {
				s.cfg.Hosts[i].Endpoint = endpoint
			}
			if caFile != "" {
				s.cfg.Hosts[i].CAFile = caFile
			}
			if adminToken != "" {
				s.cfg.Hosts[i].AdminToken = adminToken
			}
		}
	}
}

// SetEndpoint replaces a provider URL after that process rebinds.
func (s *Sched) SetEndpoint(id, endpoint string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := range s.cfg.Hosts {
		if s.cfg.Hosts[i].ProviderID == id {
			s.cfg.Hosts[i].Endpoint = endpoint
		}
	}
}

// SetDown marks a provider unreachable without deleting its lease.
func (s *Sched) SetDown(id string, down bool) {
	s.mu.Lock()
	s.down[id] = down
	s.mu.Unlock()
}

// SetClock replaces the clock used for notices. It does not change chain time.
func (s *Sched) SetClock(now func() time.Time) {
	s.mu.Lock()
	s.cfg.Now = now
	s.mu.Unlock()
}

// Token is the serve bearer returned by the provider. It is not a signing key.
func (s *Sched) Token(leaseID string) string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.tokens[leaseID]
}

// Posted returns offers already sent, in order.
func (s *Sched) Posted() []Posted {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Posted(nil), s.posted...)
}

// Traces returns the failure and repair notes.
func (s *Sched) Traces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.traces...)
}

func (s *Sched) now() time.Time {
	if s.cfg.Now != nil {
		return s.cfg.Now()
	}
	return time.Now()
}

func (s *Sched) note(line string) {
	s.traces = append(s.traces, line)
}

// Publish posts offers when the read model is fresh. It publishes only after
// approval, verified transfers, and the target number of commitments.
func (s *Sched) Publish(ctx context.Context, pkg Package, approved bool, approvalID string) (View, error) {
	s.mu.Lock()
	s.pkgs[pkg.ContentID] = pkg
	s.origin[pkg.ContentID] = true
	s.mu.Unlock()
	snap, err := s.indexer.Load(ctx)
	if err != nil || snap.Stale {
		s.mu.Lock()
		s.queued = append(s.queued, pkg.ContentID)
		s.note("queue content=" + pkg.ContentID + " label=" + policy.ServiceTemporary + " posts=0")
		s.mu.Unlock()
		return s.finish(ctx, pkg, approved, approvalID, policy.QueuedNotCommitted, "chain_stall", policy.ServiceTemporary)
	}
	if withdrawn(snap, pkg.ContentID, pkg.Version) {
		s.note("publish content=" + pkg.ContentID + " withdrawn=true posts=0")
		return s.finish(ctx, pkg, approved, approvalID, policy.PublicationWithheld, "withdrawn", policy.ServiceReadModel)
	}
	if !approved {
		s.note("publish content=" + pkg.ContentID + " approval=false posts=0")
		return s.finish(ctx, pkg, false, approvalID, policy.PublicationWithheld, "community_approval", policy.ServiceReadModel)
	}
	hosts, err := s.candidates(ctx, snap, pkg, false)
	if err != nil {
		return View{}, err
	}
	chosen := policy.Select(hosts, pkgBytes(pkg), s.cfg.PolicyID, false, s.cfg.Target)
	for _, skip := range chosen.Skipped {
		s.mu.Lock()
		s.note("skip provider=" + skip.ProviderID + " reason=" + skip.Reason)
		s.mu.Unlock()
	}
	for _, host := range chosen.Chosen {
		if err := s.offer(ctx, pkg, host, s.cfg.Stage.URL+"/"+pkg.ContentID); err != nil {
			return View{}, err
		}
	}
	if err := s.confirm(ctx, pkg); err != nil {
		return View{}, err
	}
	return s.finish(ctx, pkg, true, approvalID, "", "", policy.ServiceReadModel)
}

// Offer posts one offer. The low-capacity check uses it on a host Select skips.
func (s *Sched) Offer(ctx context.Context, contentID, providerID string) (int, string, error) {
	s.mu.Lock()
	pkg, ok := s.pkgs[contentID]
	s.mu.Unlock()
	if !ok {
		return 0, "", fmt.Errorf("sched: unknown content")
	}
	var host Host
	found := false
	for _, item := range s.cfg.Hosts {
		if item.ProviderID == providerID {
			host = item
			found = true
		}
	}
	if !found {
		return 0, "", fmt.Errorf("sched: unknown provider")
	}
	return s.post(ctx, pkg, host, s.cfg.Stage.URL+"/"+contentID)
}

func (s *Sched) offer(ctx context.Context, pkg Package, host policy.Host, source string) error {
	var rec Host
	for _, item := range s.cfg.Hosts {
		if item.ProviderID == host.ProviderID {
			rec = item
		}
	}
	code, errText, err := s.post(ctx, pkg, rec, source)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.note("offer provider=" + host.ProviderID + " status=" + strconv.Itoa(code) + " error=" + errText)
	s.mu.Unlock()
	return nil
}

func (s *Sched) post(ctx context.Context, pkg Package, host Host, source string) (int, string, error) {
	offerID := "o" + pkg.ContentID + host.ProviderID
	leaseID := "l" + pkg.ContentID + host.ProviderID
	if s.cfg.Approver != nil {
		if err := s.cfg.Approver(host.ProviderID, offerID); err != nil {
			return 0, "operator_refused", err
		}
	}
	body, err := offerBody(pkg, host.ProviderID, offerID, leaseID, source)
	if err != nil {
		return 0, "", err
	}
	if err := s.commitOffer(ctx, body); err != nil {
		return 0, "offer_uncommitted", err
	}
	raw, code, err := s.providerPost(ctx, host, "/offers/"+offerID+"/acceptance", "", body, 60*time.Second)
	if err != nil {
		return 0, "", err
	}
	var doc struct {
		Readiness    string `json:"readiness"`
		ServeToken   string `json:"serve_token"`
		Broadcast    bool   `json:"broadcast"`
		Committed    bool   `json:"committed"`
		SignatureHex string `json:"signature_hex"`
		Error        string `json:"error"`
	}
	_ = json.Unmarshal(raw, &doc)
	if code != 200 {
		fmt.Printf("offer-refused provider=%s status=%d error=%s\n", host.ProviderID, code, doc.Error)
	}
	if code == 200 {
		s.mu.Lock()
		s.posted = append(s.posted, Posted{
			OfferID: offerID, LeaseID: leaseID, ProviderID: host.ProviderID,
			ContentID: pkg.ContentID, Version: pkg.Version, PackageBytes: pkgBytes(pkg),
			Broadcast: doc.Broadcast, ResponseCommitted: doc.Committed, CountedFromResponse: false,
		})
		if doc.ServeToken != "" {
			s.tokens[leaseID] = doc.ServeToken
		}
		s.note("accept provider=" + host.ProviderID + " broadcast=" + strconv.FormatBool(doc.Broadcast) +
			" response_committed=" + strconv.FormatBool(doc.Committed) + " counted_from_response=false signature=" + strconv.FormatBool(doc.SignatureHex != "") +
			" scheduler_signed=false")
		s.mu.Unlock()
	}
	return code, doc.Error, nil
}

func (s *Sched) confirm(ctx context.Context, pkg Package) error {
	wait := s.cfg.ConfirmFor
	if wait <= 0 {
		wait = 20 * time.Second
	}
	deadline := time.Now().Add(wait)
	if ctxDeadline, ok := ctx.Deadline(); ok && ctxDeadline.Before(deadline) {
		deadline = ctxDeadline
	}
	for time.Now().Before(deadline) {
		snap, err := s.indexer.Load(ctx)
		if err == nil && !snap.Stale {
			ready := true
			waiting := 0
			s.mu.Lock()
			posted := append([]Posted(nil), s.posted...)
			s.mu.Unlock()
			for _, post := range posted {
				if post.ContentID != pkg.ContentID {
					continue
				}
				waiting++
				if !leaseActive(snap, post.LeaseID) {
					ready = false
					continue
				}
				if err := s.verify(ctx, pkg, post.ProviderID, post.LeaseID); err != nil {
					ready = false
				}
			}
			if waiting == 0 || ready {
				return nil
			}
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(30 * time.Millisecond):
		}
	}
	return nil
}

func (s *Sched) verify(ctx context.Context, pkg Package, providerID, leaseID string) error {
	s.mu.Lock()
	token := s.tokens[leaseID]
	s.mu.Unlock()
	host := s.host(providerID)
	buf, code, err := s.providerGet(ctx, host, "/packages/"+pkg.ContentID+"/"+strconv.FormatUint(pkg.Version, 10), token, 15*time.Second)
	ok := err == nil && code == 200 && sumHex(buf) == pkg.ExpectedSHA
	s.mu.Lock()
	if s.holds[pkg.ContentID] == nil {
		s.holds[pkg.ContentID] = map[string]hold{}
	}
	s.holds[pkg.ContentID][providerID] = hold{leaseID: leaseID, reachable: code != 0 && err == nil, verified: ok}
	s.mu.Unlock()
	if !ok {
		return fmt.Errorf("sched: bytes")
	}
	s.readReceipt(ctx, pkg, host, leaseID)
	return nil
}

func (s *Sched) readReceipt(ctx context.Context, pkg Package, host Host, leaseID string) {
	raw, code, err := s.providerPost(ctx, host, "/transfers/"+leaseID+"/receipt", "", []byte("{}"), 5*time.Second)
	if err != nil || code != 201 {
		return
	}
	var envelope struct {
		Canonical string `json:"canonical"`
	}
	if json.Unmarshal(raw, &envelope) != nil || envelope.Canonical == "" {
		return
	}
	var flat map[string]any
	if json.Unmarshal([]byte(envelope.Canonical), &flat) != nil {
		return
	}
	claimed := ""
	if digest, ok := flat["manifestDigest"].(map[string]any); ok {
		claimed, _ = digest["value"].(string)
	}
	limit, _ := flat["claimLimit"].(string)
	measured := sumHex([]byte(policy.ManifestText(pkg.ContentID, pkg.Version, pkgBytes(pkg), pkg.Segments)))
	if pkg.ManifestHex != "" {
		measured = pkg.ManifestHex
	}
	mode, claim := policy.Evidence(measured, claimed, 0, s.cfg.EvidenceMaxAge)
	if limit != "" {
		claim = limit
	}
	s.mu.Lock()
	s.evidence[pkg.ContentID] = ev{mode: mode, limit: claim, claimed: claimed}
	s.mu.Unlock()
}

// Observe records one evidence comparison. It does not change a lease.
func (s *Sched) Observe(contentID, measured, claimed string, observed time.Time) (string, string) {
	mode, limit := policy.Evidence(measured, claimed, s.now().Sub(observed), s.cfg.EvidenceMaxAge)
	s.mu.Lock()
	s.evidence[contentID] = ev{mode: mode, limit: limit, claimed: claimed}
	s.note("evidence content=" + contentID + " mode=" + mode + " claim_limit=" + limit)
	s.mu.Unlock()
	return mode, limit
}

// Repair copies from a surviving replica onto one replacement.
// A second call with the same content version does not start a second job
// while the first is running or cooling down.
func (s *Sched) Repair(ctx context.Context, contentID string, version uint64) (policy.Job, error) {
	key := contentID + "/" + strconv.FormatUint(version, 10)
	s.mu.Lock()
	next, run := policy.TouchJob(s.jobs[key], s.now().Unix(), s.cfg.CooldownSeconds, s.cfg.MaxRepairAttempts)
	next.Key = key
	if !run && s.jobs[key] != nil && s.jobs[key].State == policy.JobRunning {
		dup := *s.jobs[key]
		dup.State = policy.JobDuplicate
		s.note("repair duplicate key=" + key + " attempts=" + strconv.Itoa(dup.Attempts))
		s.mu.Unlock()
		return dup, nil
	}
	stored := next
	s.jobs[key] = &stored
	before := s.before
	s.mu.Unlock()
	if !run {
		return next, nil
	}
	snap, err := s.indexer.Load(ctx)
	if err != nil || snap.Stale {
		return s.cool(key, policy.JobCooling, "repair stalled key="+key)
	}
	if withdrawn(snap, contentID, version) {
		_ = s.Stop(ctx, contentID, version)
		return s.finishJob(key, policy.JobWithdrawn, "repair withdrawn key="+key+" posts=0")
	}
	s.mu.Lock()
	pkg, ok := s.pkgs[contentID]
	s.mu.Unlock()
	if !ok {
		return s.cool(key, policy.JobUnrecoverable, "repair missing package key="+key)
	}
	survivor, raw, err := s.survivor(ctx, snap, pkg)
	if err != nil {
		return s.cool(key, policy.JobUnrecoverable, "repair unrecoverable content="+contentID+" reachable_copies=0 origin_used=false")
	}
	hosts, err := s.candidates(ctx, snap, pkg, true)
	if err != nil {
		return policy.Job{}, err
	}
	chosen := policy.Select(hosts, pkgBytes(pkg), s.cfg.PolicyID, true, 1)
	if len(chosen.Chosen) == 0 {
		reason := "capacity"
		if len(chosen.Skipped) > 0 {
			reason = chosen.Skipped[0].Reason
		}
		return s.cool(key, policy.JobCapacity, "repair capacity content="+contentID+" reason="+reason)
	}
	rep := chosen.Chosen[0]
	if err := s.stageRepair(pkg, raw); err != nil {
		return s.cool(key, policy.JobUnrecoverable, "repair stage content="+contentID)
	}
	defer s.cfg.Stage.ClearRepair("r" + contentID)
	if before != nil {
		before()
	}
	snap, err = s.indexer.Load(ctx)
	if err != nil || withdrawn(snap, contentID, version) {
		_ = s.Stop(ctx, contentID, version)
		return s.finishJob(key, policy.JobWithdrawn, "repair withdrawn-before-offer content="+contentID+" posts=0")
	}
	if err := s.offer(ctx, pkg, rep, s.cfg.Stage.URL+"/r"+contentID); err != nil {
		return policy.Job{}, err
	}
	if err := s.confirm(ctx, pkg); err != nil {
		return policy.Job{}, err
	}
	snap, _ = s.indexer.Load(ctx)
	if withdrawn(snap, contentID, version) {
		_ = s.Stop(ctx, contentID, version)
		return s.finishJob(key, policy.JobWithdrawn, "repair withdrawn-after-transfer content="+contentID)
	}
	leaseID := "l" + contentID + rep.ProviderID
	if err := s.verify(ctx, pkg, rep.ProviderID, leaseID); err != nil {
		return s.cool(key, policy.JobCooling, "repair verify-failed content="+contentID)
	}
	s.mu.Lock()
	job := s.jobs[key]
	job.State = policy.JobConfirmed
	job.Replacement = rep.ProviderID
	job.Source = survivor
	out := *job
	s.note("repair confirmed content=" + contentID + " source=" + survivor + " replacement=" + rep.ProviderID + " buffer=discarded")
	s.mu.Unlock()
	return out, nil
}

func (s *Sched) cool(key, state, line string) (policy.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[key]
	job.State = state
	job.NextUnix = s.now().Unix() + s.cfg.CooldownSeconds
	s.note(line)
	return *job, nil
}

func (s *Sched) finishJob(key, state, line string) (policy.Job, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[key]
	job.State = state
	s.note(line)
	return *job, nil
}

func (s *Sched) survivor(ctx context.Context, snap readmodel.Snapshot, pkg Package) (string, []byte, error) {
	ids := make([]string, 0)
	for _, lease := range snap.Leases {
		if lease.ContentID == pkg.ContentID && lease.Version == pkg.Version && lease.Active && (lease.State == "ACTIVE" || lease.State == "RENEWED") {
			ids = append(ids, lease.ProviderID+" "+lease.ID)
		}
	}
	sort.Strings(ids)
	for _, pair := range ids {
		parts := strings.SplitN(pair, " ", 2)
		providerID, leaseID := parts[0], parts[1]
		if s.isDown(providerID) {
			s.mu.Lock()
			s.note("unreachable provider=" + providerID + " reachability=" + policy.Degraded)
			s.mu.Unlock()
			continue
		}
		if err := s.verify(ctx, pkg, providerID, leaseID); err != nil {
			continue
		}
		s.mu.Lock()
		token := s.tokens[leaseID]
		s.mu.Unlock()
		buf, code, err := s.providerGet(ctx, s.host(providerID), "/packages/"+pkg.ContentID+"/"+strconv.FormatUint(pkg.Version, 10), token, 15*time.Second)
		if err != nil || code != 200 || sumHex(buf) != pkg.ExpectedSHA {
			continue
		}
		return providerID, buf, nil
	}
	return "", nil, fmt.Errorf("sched: no survivor")
}

func (s *Sched) stageRepair(pkg Package, raw []byte) error {
	if sumHex(raw) != pkg.ExpectedSHA {
		return fmt.Errorf("sched: survivor bytes")
	}
	parts := map[string][]byte{}
	off := 0
	for _, seg := range pkg.Segments {
		end := off + int(seg.ByteCount)
		if end > len(raw) {
			return fmt.Errorf("sched: short package")
		}
		buf := append([]byte(nil), raw[off:end]...)
		if sumHex(buf) != seg.SHA256 {
			return fmt.Errorf("sched: segment")
		}
		parts[seg.ID] = buf
		off = end
	}
	if off != len(raw) {
		return fmt.Errorf("sched: length")
	}
	s.cfg.Stage.PutRepair("r"+pkg.ContentID, stage.Object{
		Manifest: policy.ManifestText(pkg.ContentID, pkg.Version, pkgBytes(pkg), pkg.Segments),
		Parts:    parts,
	})
	return nil
}

// Drain asks a cooperating provider to refuse new offers. It does not delete bytes.
func (s *Sched) Drain(ctx context.Context, providerID string) error {
	s.mu.Lock()
	s.draining[providerID] = true
	s.note("drain provider=" + providerID + " delete=false")
	s.mu.Unlock()
	host := s.host(providerID)
	if host.AdminToken == "" {
		return nil
	}
	_, code, err := s.providerPost(ctx, host, "/admin/drain", host.AdminToken, []byte("{}"), 5*time.Second)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("sched: drain %d", code)
	}
	return nil
}

// MayDepart reports whether other reachable verified copies already meet the target.
func (s *Sched) MayDepart(contentID, providerID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	others := 0
	for id, item := range s.holds[contentID] {
		if id != providerID && item.reachable && item.verified {
			others++
		}
	}
	return policy.MayDepart(others, s.cfg.Target)
}

// Depart withdraws one cooperating provider after MayDepart is true.
func (s *Sched) Depart(ctx context.Context, contentID, providerID, leaseID string) error {
	if !s.MayDepart(contentID, providerID) {
		s.mu.Lock()
		s.note("depart refused provider=" + providerID + " reason=last_needed_copy")
		s.mu.Unlock()
		return ErrLastCopy
	}
	return s.withdraw(ctx, providerID, leaseID)
}

// Stop ends distribution for withdrawn content and asks cooperating providers
// to delete their local copies. It does not post a new offer.
func (s *Sched) Stop(ctx context.Context, contentID string, version uint64) error {
	s.mu.Lock()
	s.note("distribution content=" + contentID + " stopped=true")
	holds := s.holds[contentID]
	s.mu.Unlock()
	var first error
	for providerID, item := range holds {
		if err := s.withdraw(ctx, providerID, item.leaseID); err != nil && first == nil {
			first = err
		}
		s.mu.Lock()
		if row, ok := s.holds[contentID][providerID]; ok {
			row.reachable = false
			row.verified = false
			s.holds[contentID][providerID] = row
		}
		s.mu.Unlock()
	}
	_ = version
	return first
}

func (s *Sched) withdraw(ctx context.Context, providerID, leaseID string) error {
	host := s.host(providerID)
	if host.AdminToken == "" || leaseID == "" {
		return nil
	}
	body, _ := json.Marshal(map[string]string{"lease_id": leaseID})
	_, code, err := s.providerPost(ctx, host, "/admin/withdraw", host.AdminToken, body, 5*time.Second)
	if err != nil {
		return err
	}
	if code != 200 {
		return fmt.Errorf("sched: withdraw %d", code)
	}
	return nil
}

// Distribute returns bytes from a reachable verified replica.
// Withdrawn content is refused. The origin backup is not a source.
func (s *Sched) Distribute(ctx context.Context, contentID string, version uint64) (int, []byte) {
	snap, err := s.indexer.Load(ctx)
	if err != nil {
		return 503, nil
	}
	if withdrawn(snap, contentID, version) {
		return 410, nil
	}
	s.mu.Lock()
	pkg, ok := s.pkgs[contentID]
	holds := map[string]hold{}
	for id, item := range s.holds[contentID] {
		holds[id] = item
	}
	s.mu.Unlock()
	if !ok {
		return 404, nil
	}
	ids := make([]string, 0, len(holds))
	for id := range holds {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		item := holds[id]
		if !item.verified || s.isDown(id) {
			continue
		}
		s.mu.Lock()
		token := s.tokens[item.leaseID]
		s.mu.Unlock()
		buf, code, err := s.providerGet(ctx, s.host(id), "/packages/"+contentID+"/"+strconv.FormatUint(version, 10), token, 15*time.Second)
		if err == nil && code == 200 && sumHex(buf) == pkg.ExpectedSHA {
			return 200, buf
		}
	}
	return 503, nil
}

func (s *Sched) candidates(ctx context.Context, snap readmodel.Snapshot, pkg Package, repair bool) ([]policy.Host, error) {
	admitted := map[string]bool{}
	for _, provider := range snap.Providers {
		if provider.State == "ADMITTED" {
			admitted[provider.ID] = true
		}
	}
	holding := map[string]bool{}
	for _, lease := range snap.Leases {
		if lease.ContentID == pkg.ContentID && lease.Version == pkg.Version && lease.Active {
			holding[lease.ProviderID] = true
		}
	}
	out := make([]policy.Host, 0, len(s.cfg.Hosts))
	for _, host := range s.cfg.Hosts {
		used, quota, statusDrain, reachable := s.probe(ctx, host)
		if quota > 0 && quota < host.Capacity {
			host.Capacity = quota
		}
		s.mu.Lock()
		down := s.down[host.ProviderID]
		drain := s.draining[host.ProviderID] || statusDrain
		s.mu.Unlock()
		if down {
			reachable = false
		}
		item := policy.Host{
			ProviderID: host.ProviderID, DeclaredOperator: host.DeclaredOperator,
			DeclaredDomain: host.DeclaredDomain, Simulated: host.Simulated, PolicyID: host.PolicyID,
			Capacity: host.Capacity, Used: used, Reachable: reachable, Admitted: admitted[host.ProviderID],
			Draining: drain, Role: host.Role, Holding: holding[host.ProviderID] && repair,
		}
		if !repair {
			item.Holding = false
		}
		out = append(out, item)
	}
	return out, nil
}

func (s *Sched) probe(ctx context.Context, host Host) (used, quota uint64, draining, reachable bool) {
	var last error
	for attempt := 1; attempt <= s.cfg.ProbeAttempts; attempt++ {
		buf, code, err := s.providerGet(ctx, host, "/health", "", 2*time.Second)
		last = err
		if err == nil && code == 200 && strings.Contains(string(buf), `"ok":true`) {
			raw, status, err := s.providerGet(ctx, host, "/status", "", 5*time.Second)
			if err == nil && status == 200 {
				var doc struct {
					UsedBytes  uint64 `json:"used_bytes"`
					QuotaBytes uint64 `json:"quota_bytes"`
					Draining   bool   `json:"draining"`
				}
				if json.Unmarshal(raw, &doc) == nil {
					return doc.UsedBytes, doc.QuotaBytes, doc.Draining, true
				}
			}
			return 0, 0, false, true
		}
	}
	_ = last
	return 0, 0, false, false
}

func (s *Sched) finish(ctx context.Context, pkg Package, approved bool, approvalID, forced, reason, service string) (View, error) {
	snap, err := s.indexer.Load(ctx)
	if err != nil {
		snap = readmodel.Snapshot{Stale: true, StaleReasons: []string{"indexer_unread"}}
		service = policy.ServiceTemporary
	} else {
		s.refresh(ctx, pkg, snap)
	}
	view := s.assemble(snap, pkg, approved, approvalID, service)
	s.mu.Lock()
	previous := s.views[pkg.ContentID].Publication
	s.mu.Unlock()
	if forced != "" {
		view.Publication = forced
		view.PublicationReason = reason
	} else if previous == policy.PublicationPublished && !withdrawn(snap, pkg.ContentID, pkg.Version) {
		view.Publication = policy.PublicationPublished
	} else {
		view.Publication, view.PublicationReason = policy.Gate(approved, view.VerifiedTransfers, view.DistinctOperators, s.cfg.Target)
	}
	if snap.Stale {
		view.ServiceLabel = policy.ServiceTemporary
		if view.Publication == policy.PublicationPublished {
			view.Publication = policy.QueuedNotCommitted
			view.PublicationReason = "chain_stall"
		}
	}
	s.mu.Lock()
	view.Queued = append([]string(nil), s.queued...)
	s.views[pkg.ContentID] = view
	s.mu.Unlock()
	return view, nil
}

func (s *Sched) refresh(ctx context.Context, pkg Package, snap readmodel.Snapshot) {
	for _, lease := range snap.Leases {
		if lease.ContentID != pkg.ContentID || lease.Version != pkg.Version || !lease.Active {
			continue
		}
		if s.isDown(lease.ProviderID) {
			s.mark(pkg.ContentID, lease.ProviderID, lease.ID, false, false)
			continue
		}
		_, _, _, reachable := s.probe(ctx, s.host(lease.ProviderID))
		if !reachable {
			s.mark(pkg.ContentID, lease.ProviderID, lease.ID, false, false)
			continue
		}
		_ = s.verify(ctx, pkg, lease.ProviderID, lease.ID)
	}
}

func (s *Sched) mark(contentID, providerID, leaseID string, reachable, verified bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.holds[contentID] == nil {
		s.holds[contentID] = map[string]hold{}
	}
	s.holds[contentID][providerID] = hold{leaseID: leaseID, reachable: reachable, verified: verified}
}

func (s *Sched) assemble(snap readmodel.Snapshot, pkg Package, approved bool, approvalID, service string) View {
	view := View{
		ContentID: pkg.ContentID, CommunityApproval: approved, ApprovalID: approvalID,
		EvidenceMode: policy.EvidenceNone, Reachability: policy.Reachable,
		OriginCounted: false, ServiceLabel: service, SchedulerSigned: false, StorageProof: false,
		IndependentOperatorVerification: false, Renewal: policy.RenewalNone,
		PhysicalFailureDomains: []string{},
	}
	if s.cfg.PhysicalDomain != "" {
		view.PhysicalFailureDomains = []string{s.cfg.PhysicalDomain}
		view.PhysicalFailureDomainCount = 1
	}
	s.mu.Lock()
	view.OriginBackupTracked = s.origin[pkg.ContentID]
	if row, ok := s.evidence[pkg.ContentID]; ok {
		view.EvidenceMode = row.mode
		view.ClaimLimit = row.limit
	}
	holds := map[string]hold{}
	for id, item := range s.holds[pkg.ContentID] {
		holds[id] = item
	}
	s.mu.Unlock()
	domains := map[string]bool{}
	simulated := true
	unverified := false
	operators := map[string]bool{}
	for _, host := range s.cfg.Hosts {
		if host.DeclaredDomain != "" {
			domains[host.DeclaredDomain] = true
		}
		if !host.Simulated {
			simulated = false
			unverified = true
		}
	}
	for domain := range domains {
		view.DeclaredFailureDomains = append(view.DeclaredFailureDomains, domain)
	}
	sort.Strings(view.DeclaredFailureDomains)
	view.SimulatedLabels = simulated && len(view.DeclaredFailureDomains) > 0
	view.UnverifiedOperatorClaim = unverified
	earliest := int64(0)
	for _, lease := range snap.Leases {
		if lease.ContentID != pkg.ContentID || lease.Version != pkg.Version {
			continue
		}
		view.LeaseState = append(view.LeaseState, LeaseState{
			LeaseID: lease.ID, ProviderID: lease.ProviderID, State: lease.State, Active: lease.Active, Deadline: lease.Deadline,
		})
		if !lease.Active || (lease.State != "ACTIVE" && lease.State != "RENEWED") {
			continue
		}
		view.ActiveCommitments++
		if earliest == 0 || lease.Deadline < earliest {
			earliest = lease.Deadline
		}
		item := holds[lease.ProviderID]
		if item.verified && !s.isDown(lease.ProviderID) {
			view.VerifiedTransfers++
			view.CountedReplicas++
			view.ReachableProviders = append(view.ReachableProviders, lease.ProviderID)
			if op := s.operator(lease.ProviderID); op != "" {
				operators[op] = true
			}
		} else {
			view.DegradedProviders = append(view.DegradedProviders, lease.ProviderID)
		}
	}
	view.DistinctOperators = len(operators)
	if view.ActiveCommitments > 0 && view.CountedReplicas < s.cfg.Target {
		view.Reachability = policy.Degraded
		view.AtRisk = true
	}
	if earliest > 0 {
		view.Renewal = policy.Renewal(earliest, s.now().Unix(), s.cfg.NoticeSeconds)
	}
	sort.Strings(view.ReachableProviders)
	sort.Strings(view.DegradedProviders)
	return view
}

// Availability is the last assembled view, refreshed from the indexer.
func (s *Sched) Availability(ctx context.Context, contentID string) (View, error) {
	s.mu.Lock()
	pkg, ok := s.pkgs[contentID]
	approved := false
	approval := ""
	if prev, seen := s.views[contentID]; seen {
		approved = prev.CommunityApproval
		approval = prev.ApprovalID
	}
	s.mu.Unlock()
	if !ok {
		return View{}, fmt.Errorf("sched: unknown content")
	}
	return s.finish(ctx, pkg, approved, approval, "", "", policy.ServiceReadModel)
}

func (s *Sched) isDown(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.down[id]
}

func (s *Sched) operator(id string) string {
	for _, host := range s.cfg.Hosts {
		if host.ProviderID == id {
			return host.DeclaredOperator
		}
	}
	return ""
}

func (s *Sched) host(id string) Host {
	for _, host := range s.cfg.Hosts {
		if host.ProviderID == id {
			return host
		}
	}
	return Host{}
}

func withdrawn(snap readmodel.Snapshot, contentID string, version uint64) bool {
	for _, content := range snap.Content {
		if content.ID == contentID && content.Version == version && content.Withdrawn {
			return true
		}
	}
	return false
}

func leaseActive(snap readmodel.Snapshot, id string) bool {
	for _, lease := range snap.Leases {
		if lease.ID == id && lease.Active && (lease.State == "ACTIVE" || lease.State == "RENEWED") {
			return true
		}
	}
	return false
}

func sumHex(buf []byte) string {
	sum := sha256.Sum256(buf)
	return hex.EncodeToString(sum[:])
}

func pkgBytes(pkg Package) uint64 {
	var n uint64
	for _, seg := range pkg.Segments {
		n += seg.ByteCount
	}
	return n
}

func offerBody(pkg Package, providerID, offerID, leaseID, source string) ([]byte, error) {
	n := pkgBytes(pkg)
	text := policy.ManifestText(pkg.ContentID, pkg.Version, n, pkg.Segments)
	sum := sha256.Sum256([]byte(text))
	manifest := hex.EncodeToString(sum[:])
	if pkg.ManifestHex != "" {
		manifest = pkg.ManifestHex
	}
	start := time.Now().Unix()
	deadline := pkg.Deadline
	if pkg.TermSeconds > 0 {
		deadline = start + pkg.TermSeconds
	}
	sched := bytes.Repeat([]byte{0x17}, 20)
	if len(pkg.Scheduler) == 20 {
		sched = pkg.Scheduler
	}
	segs := make([]map[string]string, len(pkg.Segments))
	for i, seg := range pkg.Segments {
		segs[i] = map[string]string{
			"segment_id": seg.ID, "sha256": seg.SHA256, "byte_count": strconv.FormatUint(seg.ByteCount, 10),
		}
	}
	body := map[string]any{
		"offer_id": offerID, "lease_id": leaseID, "business_id": "b" + pkg.ContentID + providerID,
		"content_id": pkg.ContentID, "content_version": strconv.FormatUint(pkg.Version, 10),
		"provider_id": providerID, "manifest_digest_hex": manifest,
		"package_bytes": strconv.FormatUint(n, 10), "terms_digest_hex": pkg.TermsHex,
		"start_unix_seconds":    strconv.FormatInt(start, 10),
		"deadline_unix_seconds": strconv.FormatInt(deadline, 10),
		"expiry_unix_seconds":   strconv.FormatInt(deadline, 10),
		"term_version":          "1", "scheduler_hex": hex.EncodeToString(sched),
		"source_url": source, "segments": segs,
	}
	return json.Marshal(body)
}

func (s *Sched) commitOffer(ctx context.Context, body []byte) error {
	url := s.cfg.OfferCommitURL
	if url == "" {
		return nil
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") && !strings.HasPrefix(url, "http://[::1]:") {
		return fmt.Errorf("sched: offer commit url must be loopback http")
	}
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	buf, _ := io.ReadAll(io.LimitReader(res.Body, 4096))
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		return fmt.Errorf("sched: offer commit %d %s", res.StatusCode, bytes.TrimSpace(buf))
	}
	return nil
}

// SetOrigin marks the creator backup reachable or not. It does not delete a replica.
func (s *Sched) SetOrigin(contentID string, up bool) {
	if s.cfg.Stage == nil {
		return
	}
	if up {
		s.cfg.Stage.ConnectOrigin(contentID)
		return
	}
	s.cfg.Stage.DisconnectOrigin(contentID)
}

func (s *Sched) client(host Host) (*http.Client, error) {
	if !strings.HasPrefix(host.Endpoint, "https://127.0.0.1:") {
		return nil, fmt.Errorf("sched: provider endpoint must be loopback https")
	}
	s.mu.Lock()
	if client := s.clients[host.CAFile]; client != nil {
		s.mu.Unlock()
		return client, nil
	}
	s.mu.Unlock()
	pemBytes, err := os.ReadFile(host.CAFile)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pemBytes) {
		return nil, fmt.Errorf("sched: ca")
	}
	client := &http.Client{
		Timeout:   30 * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	s.mu.Lock()
	s.clients[host.CAFile] = client
	s.mu.Unlock()
	return client, nil
}

func (s *Sched) providerGet(ctx context.Context, host Host, path, bearer string, timeout time.Duration) ([]byte, int, error) {
	return s.providerDo(ctx, host, http.MethodGet, path, bearer, nil, timeout)
}

func (s *Sched) providerPost(ctx context.Context, host Host, path, bearer string, body []byte, timeout time.Duration) ([]byte, int, error) {
	return s.providerDo(ctx, host, http.MethodPost, path, bearer, body, timeout)
}

func (s *Sched) providerDo(ctx context.Context, host Host, method, path, bearer string, body []byte, timeout time.Duration) ([]byte, int, error) {
	client, err := s.client(host)
	if err != nil {
		return nil, 0, err
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	var reader io.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, strings.TrimRight(host.Endpoint, "/")+path, reader)
	if err != nil {
		return nil, 0, err
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode < 400 {
		return nil, res.StatusCode, fmt.Errorf("sched: redirect refused")
	}
	buf, err := io.ReadAll(io.LimitReader(res.Body, 1<<20+1))
	if err != nil {
		return nil, res.StatusCode, err
	}
	if len(buf) > 1<<20 {
		return nil, res.StatusCode, fmt.Errorf("sched: unbounded response")
	}
	return buf, res.StatusCode, nil
}
