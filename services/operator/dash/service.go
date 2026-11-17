package dash

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"
)

// Peer is one loopback provider the dashboard may read. The admin token
// stays in this process.
type Peer struct {
	ID        string
	BaseURL   string
	CAFile    string
	Token     string
	TokenFile string
	client    *http.Client
}

// Config is the process configuration. URLs must be loopback.
type Config struct {
	DevSessions    bool
	WebDir         string
	AdminOrigin    string
	PublicOrigin   string
	ChainID        string
	Provider       *Peer
	Successor      *Peer
	IndexerURL     string
	DiscoveryURL   string
	DiscoveryToken string
	MediaQueue     string
	GovernanceURL  string
	SchedulerURL   string
	Now            func() time.Time
}

// Service is the operator dashboard.
type Service struct {
	cfg Config
	now func() time.Time

	mu       sync.Mutex
	sessions map[string]principal
	onboard  *OnboardResult
	audits   []AuditRow
	nextAud  int
	renewals []map[string]any
}

type principal struct {
	Role    string
	Account string
}

// Error is an HTTP refusal.
type Error struct {
	Status int
	Code   string
}

func (e *Error) Error() string { return e.Code }

func fail(status int, code string) error { return &Error{Status: status, Code: code} }

// New builds a dashboard. It does not open a socket and it does not hold a
// consensus key.
func New(cfg Config) (*Service, error) {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if err := preparePeer(cfg.Provider); err != nil {
		return nil, err
	}
	if err := preparePeer(cfg.Successor); err != nil {
		return nil, err
	}
	for _, raw := range []string{cfg.IndexerURL, cfg.DiscoveryURL, cfg.GovernanceURL, cfg.SchedulerURL} {
		if err := loopbackURL(raw); err != nil {
			return nil, err
		}
	}
	if cfg.WebDir != "" && strings.Contains(cfg.WebDir, "..") {
		return nil, errors.New("dash: web_dir")
	}
	if cfg.MediaQueue != "" && strings.Contains(cfg.MediaQueue, "..") {
		return nil, errors.New("dash: media_queue")
	}
	return &Service{cfg: cfg, now: cfg.Now, sessions: map[string]principal{}}, nil
}

// SetOrigins records the listen URLs after bind. The public handler echoes
// the admin origin on CORS and nowhere else.
func (s *Service) SetOrigins(publicOrigin, adminOrigin string) {
	s.mu.Lock()
	s.cfg.PublicOrigin = publicOrigin
	s.cfg.AdminOrigin = adminOrigin
	s.mu.Unlock()
}

func preparePeer(p *Peer) error {
	if p == nil {
		return nil
	}
	if !validID(p.ID) {
		return errors.New("dash: provider id")
	}
	if err := loopbackURL(p.BaseURL); err != nil {
		return err
	}
	p.BaseURL = strings.TrimRight(p.BaseURL, "/")
	p.client = &http.Client{Timeout: 5 * time.Second, CheckRedirect: refuseRedirect}
	if p.CAFile == "" {
		return nil
	}
	pem, err := os.ReadFile(p.CAFile)
	if err != nil {
		return err
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		return errors.New("dash: ca")
	}
	p.client.Transport = &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool, MinVersion: tls.VersionTLS12}}
	return nil
}

func refuseRedirect(*http.Request, []*http.Request) error {
	return errors.New("dash: redirect refused")
}

var readClient = &http.Client{Timeout: 5 * time.Second, CheckRedirect: refuseRedirect}

func loopbackURL(raw string) error {
	if raw == "" {
		return nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil {
		return errors.New("dash: url")
	}
	if u.Path != "" && u.Path != "/" {
		return errors.New("dash: url path")
	}
	host := u.Hostname()
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("dash: url is not loopback")
	}
	return nil
}

func validID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		case i > 0 && (c == '.' || c == '_' || c == '-'):
		default:
			return false
		}
	}
	return true
}

func (s *Service) clock() time.Time { return s.now().UTC() }

// IssueDevSession mints a development bearer. It is not a chain key.
func (s *Service) IssueDevSession(role, account string) (map[string]any, error) {
	if !s.cfg.DevSessions {
		return nil, fail(404, "not found")
	}
	if !sessionRoles[role] || !validID(account) {
		return nil, fail(400, "malformed")
	}
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(buf[:])
	s.mu.Lock()
	if len(s.sessions) > 100 {
		s.mu.Unlock()
		return nil, fail(429, "too many sessions")
	}
	s.sessions[token] = principal{Role: role, Account: account}
	s.mu.Unlock()
	return map[string]any{
		"token": token, "role": role, "account_id": account,
		"development_session": true, "consensus_key": false, "chain_submitted": false,
	}, nil
}

func (s *Service) principal(token string) (principal, error) {
	if token == "" {
		return principal{}, fail(401, "unauthorized")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for saved, p := range s.sessions {
		if len(saved) == len(token) && subtle.ConstantTimeCompare([]byte(saved), []byte(token)) == 1 {
			return p, nil
		}
	}
	return principal{}, fail(401, "unauthorized")
}

func requireRole(p principal, role string) error {
	if p.Role != role {
		return fail(403, "FORBIDDEN")
	}
	return nil
}

// Snapshot reads the configured sources. It does not sign and it does not
// invent a number for a source that is missing.
func (s *Service) Snapshot(ctx context.Context) Snapshot {
	out := Snapshot{ChainSubmitted: false, ConsensusKeyAccepted: false, SingleMachineDemo: true, IndependenceProven: false}
	s.mu.Lock()
	if s.onboard != nil {
		out.Roles = append([]string(nil), s.onboard.Roles...)
	}
	s.mu.Unlock()
	out.Readings = append(out.Readings, Reading{
		Name: "software_version", Class: "configured", Value: SoftwareVersion, Source: "build",
		Note: "development build, not a release",
	})
	out.Readings = append(out.Readings, s.localDisk())
	out.Readings = append(out.Readings, s.providerReadings(ctx)...)
	out.Readings = append(out.Readings, s.indexReadings(ctx)...)
	out.Readings = append(out.Readings, s.queueReading())
	out.Readings = append(out.Readings, s.schedulerReadings(ctx)...)
	return out
}

func (s *Service) localDisk() Reading {
	path := s.cfg.MediaQueue
	if path == "" {
		path = "."
	}
	n, err := availableBytes(path)
	if err != nil {
		return Reading{Name: "local_disk_available_bytes", Class: "stale", Source: "statfs", Note: "statfs failed"}
	}
	return Reading{
		Name: "local_disk_available_bytes", Class: "measured", Value: n, Source: "statfs", Note: observationNote,
	}
}

func (s *Service) providerReadings(ctx context.Context) []Reading {
	if s.cfg.Provider == nil {
		return []Reading{{Name: "provider", Class: "absent", Source: "provider", Note: "provider URL is not configured"}}
	}
	raw, status, err := s.getRaw(ctx, s.cfg.Provider.client, s.cfg.Provider.BaseURL+"/status")
	if err != nil || status != 200 {
		return []Reading{{Name: "provider", Class: "stale", Source: "provider", Note: "provider read failed"}}
	}
	var doc providerDoc
	if json.Unmarshal(raw, &doc) != nil {
		return []Reading{{Name: "provider", Class: "stale", Source: "provider", Note: "provider read failed"}}
	}
	var obj map[string]json.RawMessage
	_ = json.Unmarshal(raw, &obj)
	chainClass := "measured"
	chainNote := "provider chain observation, not a light client"
	if doc.ChainUncertain {
		chainClass = "stale"
		chainNote = "chain_uncertain"
	}
	rows := make([]map[string]any, 0, len(doc.Leases))
	for _, lease := range doc.Leases {
		rows = append(rows, map[string]any{
			"lease_id": lease.LeaseID, "content_id": lease.ContentID, "readiness": lease.Readiness,
			"package_bytes": lease.PackageBytes,
		})
	}
	domainNote := "declaration only; this process cannot prove a separate failure domain"
	if doc.Independent {
		domainNote = "provider status claimed independence; this dashboard does not display that as proven"
	}
	return []Reading{
		field(obj, "used_bytes", doc.UsedBytes, "measured", "provider", "bytes reserved by the provider process"),
		field(obj, "quota_bytes", doc.QuotaBytes, "configured", "provider", "local quota, not a chain allowance"),
		field(obj, "declared_capacity_bytes", doc.DeclaredCapacity, "configured", "provider", "declared at enrollment"),
		field(obj, "disk_available_bytes", doc.DiskAvailable, "measured", "provider", observationNote),
		field(obj, "bandwidth_bytes_per_second", doc.Bandwidth, "configured", "provider", "configured limit, not a measured rate"),
		field(obj, "bytes_fetched", doc.BytesFetched, "measured", "provider", "bytes the provider fetched"),
		field(obj, "bytes_served", doc.BytesServed, "measured", "provider", "bytes the provider served"),
		{Name: "leases", Class: classIf(obj, "leases", "measured"), Value: rows, Source: "provider", Note: "local lease table"},
		{Name: "inventory", Class: classIf(obj, "leases", "measured"), Value: len(rows), Source: "provider", Note: "count of the local lease table; a signed inventory is not requested by this read"},
		field(obj, "evidence_mode", doc.EvidenceMode, "configured", "provider", "attestation label, not a storage proof"),
		{Name: "chain_height", Class: classIf(obj, "chain_height", chainClass), Value: doc.ChainHeight, Source: "provider", Note: chainNote},
		field(obj, "draining", doc.Draining, "measured", "provider", ""),
		field(obj, "endpoint_enabled", doc.EndpointEnabled, "measured", "provider", ""),
		field(obj, "software_version", doc.SoftwareVersion, "configured", "provider", "provider build label"),
		{Name: "failure_domain", Class: "configured", Value: "single-machine demo", Source: "dashboard", Note: domainNote},
	}
}

func field(obj map[string]json.RawMessage, key string, value any, class, source, note string) Reading {
	if _, ok := obj[key]; !ok {
		return Reading{Name: key, Class: "absent", Source: source, Note: "not reported"}
	}
	return Reading{Name: key, Class: class, Value: value, Source: source, Note: note}
}

func classIf(obj map[string]json.RawMessage, key, class string) string {
	if _, ok := obj[key]; !ok {
		return "absent"
	}
	return class
}

type providerDoc struct {
	Independent      bool       `json:"independent_operator"`
	Draining         bool       `json:"draining"`
	UsedBytes        uint64     `json:"used_bytes"`
	QuotaBytes       uint64     `json:"quota_bytes"`
	DeclaredCapacity uint64     `json:"declared_capacity_bytes"`
	Bandwidth        uint64     `json:"bandwidth_bytes_per_second"`
	BytesFetched     uint64     `json:"bytes_fetched"`
	BytesServed      uint64     `json:"bytes_served"`
	DiskAvailable    uint64     `json:"disk_available_bytes"`
	EndpointEnabled  bool       `json:"endpoint_enabled"`
	EvidenceMode     string     `json:"evidence_mode"`
	SoftwareVersion  string     `json:"software_version"`
	ChainUncertain   bool       `json:"chain_uncertain"`
	ChainHeight      uint64     `json:"chain_height"`
	Leases           []leaseDoc `json:"leases"`
}

type leaseDoc struct {
	LeaseID      string `json:"lease_id"`
	ContentID    string `json:"content_id"`
	Readiness    string `json:"readiness"`
	PackageBytes uint64 `json:"package_bytes"`
}

func (s *Service) indexReadings(ctx context.Context) []Reading {
	if s.cfg.IndexerURL == "" {
		return []Reading{
			{Name: "index_height", Class: "absent", Source: "indexer", Note: "indexer URL is not configured"},
			{Name: "overdue", Class: "absent", Source: "indexer", Note: "indexer URL is not configured"},
		}
	}
	base := strings.TrimRight(s.cfg.IndexerURL, "/")
	raw, status, err := s.getRaw(ctx, readClient, base+"/v1/status")
	if err != nil || status != 200 {
		return []Reading{
			{Name: "index_height", Class: "stale", Source: "indexer", Note: "indexer read failed"},
			{Name: "overdue", Class: "stale", Source: "indexer", Note: "indexer read failed"},
		}
	}
	var view struct {
		IndexedHeight   int64    `json:"indexed_height"`
		CanonicalHeight int64    `json:"canonical_height"`
		Stale           bool     `json:"stale"`
		StaleReasons    []string `json:"stale_reasons"`
	}
	if json.Unmarshal(raw, &view) != nil {
		return []Reading{{Name: "index_height", Class: "stale", Source: "indexer", Note: "indexer read failed"}}
	}
	class := "measured"
	note := "indexer read model"
	if view.Stale {
		class = "stale"
		note = "stale: " + strings.Join(view.StaleReasons, ",")
	}
	over := s.overdue(ctx, base)
	over.Class = class
	if view.Stale {
		over.Note = "stale indexer; " + over.Note
	}
	policy := s.policyReading(ctx, base, class)
	return []Reading{
		{Name: "index_height", Class: class, Value: view.IndexedHeight, Source: "indexer", Note: note},
		{Name: "canonical_height", Class: class, Value: view.CanonicalHeight, Source: "indexer", Note: note},
		over,
		policy,
	}
}

func (s *Service) overdue(ctx context.Context, base string) Reading {
	raw, status, err := s.getRaw(ctx, readClient, base+"/v1/deadlines")
	if err != nil || status != 200 {
		return Reading{Name: "overdue", Class: "stale", Source: "indexer", Note: "deadline read failed"}
	}
	var doc struct {
		Deadlines []struct {
			Kind     string `json:"kind"`
			ID       string `json:"id"`
			Deadline int64  `json:"deadline_unix_seconds"`
			Known    bool   `json:"expiry_known"`
		} `json:"deadlines"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		return Reading{Name: "overdue", Class: "stale", Source: "indexer", Note: "deadline read failed"}
	}
	now := s.clock().Unix()
	var late []map[string]any
	for _, row := range doc.Deadlines {
		if row.Known && row.Deadline < now {
			late = append(late, map[string]any{"id": row.ID, "kind": row.Kind, "deadline_unix_seconds": row.Deadline})
		}
	}
	if late == nil {
		late = []map[string]any{}
	}
	return Reading{Name: "overdue", Class: "measured", Value: late, Source: "indexer", Note: "counted from indexer deadlines"}
}

func (s *Service) policyReading(ctx context.Context, base, class string) Reading {
	raw, status, err := s.getRaw(ctx, readClient, base+"/v1/policy")
	if err != nil || status != 200 {
		return Reading{Name: "policy_version", Class: "absent", Source: "indexer", Note: "policy read failed"}
	}
	var doc struct {
		Present bool `json:"present"`
		Policy  struct {
			Version uint64 `json:"Version"`
		} `json:"policy"`
	}
	if json.Unmarshal(raw, &doc) != nil || !doc.Present {
		return Reading{Name: "policy_version", Class: "absent", Source: "indexer", Note: "no policy row"}
	}
	return Reading{Name: "policy_version", Class: class, Value: doc.Policy.Version, Source: "indexer", Note: "indexer policy row"}
}

func (s *Service) queueReading() Reading {
	if s.cfg.MediaQueue == "" {
		return Reading{Name: "job_queue", Class: "absent", Source: "media", Note: "processing queue directory is not configured"}
	}
	counts := map[string]int{}
	for _, name := range []string{"queued", "running", "ready", "failed"} {
		n, err := countJSON(filepath.Join(s.cfg.MediaQueue, "jobs", name))
		if err != nil {
			return Reading{Name: "job_queue", Class: "stale", Source: "media", Note: "queue directory is missing"}
		}
		counts[name] = n
	}
	return Reading{Name: "job_queue", Class: "measured", Value: counts, Source: "media", Note: "file count in the local queue directories; not a chain record"}
}

func countJSON(dir string) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, entry := range entries {
		if n >= 10000 {
			break
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		n++
	}
	return n, nil
}

func (s *Service) schedulerReadings(ctx context.Context) []Reading {
	queue := Reading{Name: "scheduler_queue", Class: "absent", Source: "scheduler", Note: "svn-scheduler does not expose a job queue. No count is invented."}
	if s.cfg.SchedulerURL == "" {
		return []Reading{
			{Name: "scheduler_health", Class: "absent", Source: "scheduler", Note: "scheduler URL is not configured"},
			queue,
		}
	}
	raw, status, err := s.getRaw(ctx, readClient, strings.TrimRight(s.cfg.SchedulerURL, "/")+"/health")
	if err != nil || status != 200 || !bytes.Contains(raw, []byte(`"ok":true`)) {
		return []Reading{{Name: "scheduler_health", Class: "stale", Source: "scheduler", Note: "health read failed"}, queue}
	}
	return []Reading{{Name: "scheduler_health", Class: "measured", Value: "ok", Source: "scheduler", Note: "process liveness"}, queue}
}

func (s *Service) getRaw(ctx context.Context, client *http.Client, rawURL string) ([]byte, int, error) {
	if client == nil {
		client = readClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Accept", "application/json")
	res, err := client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, res.StatusCode, err
	}
	return buf, res.StatusCode, nil
}

// Council reads the configured governance document. It does not submit.
func (s *Service) Council(ctx context.Context) CouncilView {
	out := CouncilView{
		ConsensusKeyAccepted: false, ChainSubmitted: false,
		Instructions: Reading{Name: "upgrade_instructions", Class: "configured", Value: UpgradeInstructions, Source: "runbook"},
	}
	if s.cfg.GovernanceURL == "" {
		out.Proposals = Reading{Name: "proposals", Class: "absent", Source: "unconfigured", Note: "no read-only chain query is configured; no proposal is invented"}
		out.Upgrade = Reading{Name: "upgrade", Class: "absent", Source: "unconfigured", Note: "no plan was read"}
		return out
	}
	raw, status, err := s.getRaw(ctx, readClient, strings.TrimRight(s.cfg.GovernanceURL, "/"))
	if err != nil || status != 200 {
		out.Proposals = Reading{Name: "proposals", Class: "stale", Source: "governance_url", Note: "governance read failed"}
		out.Upgrade = Reading{Name: "upgrade", Class: "stale", Source: "governance_url", Note: "governance read failed"}
		return out
	}
	var doc struct {
		Proposals []Proposal `json:"proposals"`
		Upgrade   *Upgrade   `json:"upgrade"`
	}
	if json.Unmarshal(raw, &doc) != nil {
		out.Proposals = Reading{Name: "proposals", Class: "stale", Source: "governance_url", Note: "governance read failed"}
		out.Upgrade = Reading{Name: "upgrade", Class: "stale", Source: "governance_url", Note: "governance read failed"}
		return out
	}
	if doc.Proposals == nil {
		doc.Proposals = []Proposal{}
	}
	out.Proposals = Reading{Name: "proposals", Class: "measured", Value: doc.Proposals, Source: "governance_url", Note: "configured read-only document, not a light client and not a signature"}
	if doc.Upgrade == nil {
		out.Upgrade = Reading{Name: "upgrade", Class: "absent", Source: "governance_url", Note: "no plan in the document"}
	} else {
		out.Upgrade = Reading{Name: "upgrade", Class: "measured", Value: doc.Upgrade, Source: "governance_url"}
	}
	return out
}

// Onboard records duty roles. Storage does not add validator. No key is accepted.
func (s *Service) Onboard(p principal, body []byte) (OnboardResult, error) {
	if err := requireRole(p, "provider"); err != nil {
		return OnboardResult{}, err
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var in struct {
		Roles         []string `json:"roles"`
		OperatorLabel string   `json:"operator_label"`
		FailureDomain string   `json:"failure_domain"`
	}
	if err := dec.Decode(&in); err != nil || dec.More() {
		return OnboardResult{}, fail(400, "malformed")
	}
	if !labelOK(in.OperatorLabel) || !labelOK(in.FailureDomain) || len(in.Roles) == 0 || len(in.Roles) > len(dutyRoles) {
		return OnboardResult{}, fail(400, "malformed")
	}
	chosen := map[string]bool{}
	for _, role := range in.Roles {
		if !dutyOK(role) || chosen[role] {
			return OnboardResult{}, fail(400, "malformed")
		}
		chosen[role] = true
	}
	roles := append([]string(nil), in.Roles...)
	sort.Strings(roles)
	disk := s.localDisk()
	out := OnboardResult{
		Roles: roles, StorageSelected: chosen["storage_delivery"], ValidatorSelected: chosen["validator"],
		StorageForcesValidator: false, IndependenceProven: false, SingleMachineDemo: true,
		ChainSubmitted: false, ConsensusKeyAccepted: false,
		FailureDomain: Reading{
			Name: "failure_domain", Class: "configured", Value: in.FailureDomain, Source: "declaration",
			Note: "declaration only; this process cannot prove a separate operator or failure domain",
		},
		Observations: []Reading{
			disk,
			{Name: "goos", Class: "measured", Value: runtime.GOOS, Source: "runtime", Note: observationNote},
			{Name: "goarch", Class: "measured", Value: runtime.GOARCH, Source: "runtime", Note: observationNote},
			{Name: "listeners", Class: "configured", Value: "loopback", Source: "config", Note: "not a bandwidth minimum"},
		},
	}
	if chosen["processing"] && s.cfg.MediaQueue == "" {
		out.Observations = append(out.Observations, Reading{Name: "processing", Class: "pending", Source: "media", Note: "processing queue path is not configured"})
	}
	if chosen["account_discovery"] && s.cfg.DiscoveryURL == "" {
		out.Observations = append(out.Observations, Reading{Name: "account_discovery", Class: "pending", Source: "discovery", Note: "discovery URL is not configured"})
	}
	if chosen["validator"] {
		out.Observations = append(out.Observations, Reading{
			Name: "validator", Class: "pending", Source: "dashboard",
			Note: "validator onboarding does not load a consensus key",
		})
	}
	s.mu.Lock()
	s.onboard = &out
	s.mu.Unlock()
	s.audit(p, "onboarding", strings.Join(roles, ","), "recorded")
	return out, nil
}

func dutyOK(role string) bool {
	for _, item := range dutyRoles {
		if item == role {
			return true
		}
	}
	return false
}

func labelOK(s string) bool {
	if len(s) < 1 || len(s) > 64 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-' || c == ' ':
		default:
			return false
		}
	}
	return true
}

func (s *Service) audit(p principal, action, target, result string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.nextAud++
	s.audits = append(s.audits, AuditRow{
		ID: s.nextAud, At: s.clock().Format(time.RFC3339), Role: p.Role, Account: p.Account,
		Action: action, Target: target, Result: result, ChainSubmitted: false,
	})
	if len(s.audits) > 200 {
		s.audits = s.audits[len(s.audits)-200:]
	}
}

// Audit returns rows this role is allowed to see.
func (s *Service) Audit(p principal) ([]AuditRow, error) {
	allow := map[string]bool{}
	switch p.Role {
	case "provider":
		for _, action := range []string{"onboarding", "limits", "stop-offers", "drain", "handoff", "renewal", "disable"} {
			allow[action] = true
		}
	case "council":
		allow["governance"] = true
		allow["policy"] = true
	default:
		return nil, fail(403, "FORBIDDEN")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []AuditRow
	for _, row := range s.audits {
		if allow[row.Action] {
			out = append(out, row)
		}
	}
	if out == nil {
		out = []AuditRow{}
	}
	return out, nil
}

// Limits changes the local quota and bandwidth through the provider admin API.
func (s *Service) Limits(ctx context.Context, p principal, body []byte) (ActionResult, error) {
	if err := requireRole(p, "provider"); err != nil {
		return ActionResult{}, err
	}
	var in struct {
		Quota     uint64  `json:"quota_bytes"`
		Bandwidth uint64  `json:"bandwidth_bytes_per_second"`
		Confirm   string  `json:"confirm"`
		Declared  *uint64 `json:"declared_capacity_bytes"`
	}
	if err := decodeStrict(body, &in); err != nil {
		return ActionResult{}, fail(400, "malformed")
	}
	if in.Confirm != confirmLimits {
		return ActionResult{}, fail(400, "confirm")
	}
	if in.Declared != nil {
		s.audit(p, "policy", "declared_capacity", "not_submitted")
		return refused("GOVERNANCE_NOT_SUBMITTED"), fail(403, "GOVERNANCE_NOT_SUBMITTED")
	}
	if s.cfg.Provider == nil {
		return ActionResult{}, fail(503, "provider is not configured")
	}
	payload, _ := json.Marshal(map[string]any{
		"quota_bytes": in.Quota, "bandwidth_bytes_per_second": in.Bandwidth, "confirm": confirmLimits,
	})
	code, doc, err := s.postAdmin(ctx, s.cfg.Provider, "/admin/limits", payload)
	if err != nil {
		return ActionResult{}, err
	}
	if code != 200 {
		return ActionResult{}, upstream(code, doc)
	}
	s.audit(p, "limits", s.cfg.Provider.ID, "recorded")
	return okResult("recorded", map[string]any{"provider": json.RawMessage(doc)}), nil
}

// StopOffers tells the provider to refuse new offers.
func (s *Service) StopOffers(ctx context.Context, p principal, body []byte) (ActionResult, error) {
	return s.drainCall(ctx, p, body, confirmStop, "stop-offers")
}

// Drain refuses new offers and leaves accepted bytes in place.
func (s *Service) Drain(ctx context.Context, p principal, body []byte) (ActionResult, error) {
	return s.drainCall(ctx, p, body, confirmDrain, "drain")
}

func (s *Service) drainCall(ctx context.Context, p principal, body []byte, phrase, action string) (ActionResult, error) {
	if err := requireRole(p, "provider"); err != nil {
		return ActionResult{}, err
	}
	var in struct {
		Confirm string `json:"confirm"`
	}
	if err := decodeStrict(body, &in); err != nil {
		return ActionResult{}, fail(400, "malformed")
	}
	if in.Confirm != phrase {
		return ActionResult{}, fail(400, "confirm")
	}
	if s.cfg.Provider == nil {
		return ActionResult{}, fail(503, "provider is not configured")
	}
	code, doc, err := s.postAdmin(ctx, s.cfg.Provider, "/admin/drain", []byte("{}"))
	if err != nil {
		return ActionResult{}, err
	}
	if code != 200 {
		return ActionResult{}, upstream(code, doc)
	}
	s.audit(p, action, s.cfg.Provider.ID, "recorded")
	return okResult("recorded", map[string]any{"draining": true, "deleted": false}), nil
}

// Handoff withdraws serving leases after the successor status shows the same
// content. It does not submit a chain end.
func (s *Service) Handoff(ctx context.Context, p principal, body []byte) (ActionResult, error) {
	if err := requireRole(p, "provider"); err != nil {
		return ActionResult{}, err
	}
	var in struct {
		Confirm string `json:"confirm"`
	}
	if err := decodeStrict(body, &in); err != nil {
		return ActionResult{}, fail(400, "malformed")
	}
	if in.Confirm != confirmHandoff {
		return ActionResult{}, fail(400, "confirm")
	}
	if s.cfg.Provider == nil || s.cfg.Successor == nil {
		s.audit(p, "handoff", "", "refused")
		return refused("last_needed_copy"), fail(409, "last_needed_copy")
	}
	primary, err := s.readProvider(ctx, s.cfg.Provider)
	if err != nil {
		return ActionResult{}, err
	}
	successor, err := s.readProvider(ctx, s.cfg.Successor)
	if err != nil {
		return ActionResult{}, err
	}
	var due []leaseDoc
	for _, lease := range primary.Leases {
		if lease.Readiness != "SERVING" {
			continue
		}
		if !holds(successor, lease.ContentID) {
			s.audit(p, "handoff", lease.LeaseID, "refused")
			return refused("last_needed_copy"), fail(409, "last_needed_copy")
		}
		due = append(due, lease)
	}
	if len(due) == 0 {
		s.audit(p, "handoff", "", "refused")
		return refused("nothing_serving"), fail(409, "nothing_serving")
	}
	var released []string
	for _, lease := range due {
		payload, _ := json.Marshal(map[string]string{"lease_id": lease.LeaseID})
		code, doc, err := s.postAdmin(ctx, s.cfg.Provider, "/admin/withdraw", payload)
		if err != nil {
			return ActionResult{}, err
		}
		if code != 200 {
			return ActionResult{}, upstream(code, doc)
		}
		var row struct {
			ChainEnd bool `json:"chain_end"`
		}
		_ = json.Unmarshal(doc, &row)
		if row.ChainEnd {
			return ActionResult{}, fail(500, "provider reported a chain end")
		}
		released = append(released, lease.LeaseID)
	}
	after, err := s.readProvider(ctx, s.cfg.Provider)
	if err != nil {
		return ActionResult{}, err
	}
	s.audit(p, "handoff", strings.Join(released, ","), "recorded")
	return okResult("recorded", map[string]any{
		"released": released, "used_bytes_before": primary.UsedBytes, "used_bytes_after": after.UsedBytes,
		"chain_end": false,
	}), nil
}

func holds(doc providerDoc, contentID string) bool {
	for _, lease := range doc.Leases {
		if lease.ContentID == contentID && (lease.Readiness == "SERVING" || lease.Readiness == "COMMITTED") {
			return true
		}
	}
	return false
}

func (s *Service) readProvider(ctx context.Context, p *Peer) (providerDoc, error) {
	raw, status, err := s.getRaw(ctx, p.client, p.BaseURL+"/status")
	if err != nil || status != 200 {
		return providerDoc{}, fail(502, "provider read failed")
	}
	var doc providerDoc
	if json.Unmarshal(raw, &doc) != nil {
		return providerDoc{}, fail(502, "provider read failed")
	}
	return doc, nil
}

// ReviewRenewal stores a local note. It does not broadcast a renewal.
func (s *Service) ReviewRenewal(p principal, body []byte) (ActionResult, error) {
	if err := requireRole(p, "provider"); err != nil {
		return ActionResult{}, err
	}
	var in struct {
		LeaseID  string `json:"lease_id"`
		Confirm  string `json:"confirm"`
		Decision string `json:"decision"`
	}
	if err := decodeStrict(body, &in); err != nil {
		return ActionResult{}, fail(400, "malformed")
	}
	if in.Confirm != confirmRenewal {
		return ActionResult{}, fail(400, "confirm")
	}
	if !validID(in.LeaseID) || (in.Decision != "hold" && in.Decision != "refuse") {
		return ActionResult{}, fail(400, "malformed")
	}
	s.mu.Lock()
	s.renewals = append(s.renewals, map[string]any{
		"lease_id": in.LeaseID, "decision": in.Decision, "chain_submitted": false, "class": "pending",
	})
	s.mu.Unlock()
	s.audit(p, "renewal", in.LeaseID, "pending")
	return okResult("pending", map[string]any{
		"class": "pending", "note": "renewal is not submitted; an evidence renewal is not sent from this dashboard",
	}), nil
}

// DisableEndpoint asks the provider to stop serving.
func (s *Service) DisableEndpoint(ctx context.Context, p principal, body []byte) (ActionResult, error) {
	if err := requireRole(p, "provider"); err != nil {
		return ActionResult{}, err
	}
	var in struct {
		Confirm string `json:"confirm"`
	}
	if err := decodeStrict(body, &in); err != nil {
		return ActionResult{}, fail(400, "malformed")
	}
	if in.Confirm != confirmDisable {
		return ActionResult{}, fail(400, "confirm")
	}
	if s.cfg.Provider == nil {
		return ActionResult{}, fail(503, "provider is not configured")
	}
	code, doc, err := s.postAdmin(ctx, s.cfg.Provider, "/admin/disable", []byte(`{"confirm":"DISABLE ENDPOINT"}`))
	if err != nil {
		return ActionResult{}, err
	}
	if code != 200 {
		return ActionResult{}, upstream(code, doc)
	}
	s.audit(p, "disable", s.cfg.Provider.ID, "recorded")
	return okResult("recorded", map[string]any{"endpoint_enabled": false}), nil
}

// RefuseGovernance records that no council message was submitted.
func (s *Service) RefuseGovernance(p principal, action string) (ActionResult, error) {
	if p.Role == "" {
		return ActionResult{}, fail(401, "unauthorized")
	}
	s.audit(p, action, "", "not_submitted")
	return refused("GOVERNANCE_NOT_SUBMITTED"), fail(403, "GOVERNANCE_NOT_SUBMITTED")
}

// Queue reads the discovery queue and drops complaint text.
func (s *Service) Queue(ctx context.Context, p principal) (map[string]any, error) {
	if err := requireRole(p, "moderator"); err != nil {
		return nil, err
	}
	if s.cfg.DiscoveryURL == "" || s.cfg.DiscoveryToken == "" {
		return nil, fail(503, "discovery is not configured")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(s.cfg.DiscoveryURL, "/")+"/v1/moderation/queue", nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+s.cfg.DiscoveryToken)
	res, err := readClient.Do(req)
	if err != nil {
		return nil, fail(502, "discovery read failed")
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil || res.StatusCode != 200 {
		return nil, fail(502, "discovery read failed")
	}
	var doc struct {
		Queue []map[string]any `json:"queue"`
	}
	if json.Unmarshal(buf, &doc) != nil {
		return nil, fail(502, "discovery read failed")
	}
	rows := make([]map[string]any, 0, len(doc.Queue))
	for _, row := range doc.Queue {
		kept := map[string]any{}
		for _, key := range []string{"case_id", "status", "target_type", "target_id", "reason_code", "appeal_status", "escalation_status"} {
			if value, ok := row[key]; ok {
				kept[key] = value
			}
		}
		rows = append(rows, kept)
	}
	return map[string]any{
		"queue": rows, "chain_submitted": false, "narrative_copied": false,
		"note": "complaint text is not copied to this dashboard",
	}, nil
}

func (s *Service) postAdmin(ctx context.Context, p *Peer, path string, body []byte) (int, []byte, error) {
	token, err := p.token()
	if err != nil || token == "" {
		return 0, nil, fail(503, "admin token is not configured")
	}
	target := p.BaseURL + path
	if !strings.HasPrefix(target, p.BaseURL+"/") {
		return 0, nil, fail(400, "malformed")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	res, err := p.client.Do(req)
	if err != nil {
		return 0, nil, fail(502, "provider post failed")
	}
	defer res.Body.Close()
	buf, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return res.StatusCode, nil, err
	}
	return res.StatusCode, buf, nil
}

func (p *Peer) token() (string, error) {
	if p.Token != "" {
		return p.Token, nil
	}
	if p.TokenFile == "" {
		return "", nil
	}
	buf, err := os.ReadFile(p.TokenFile)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buf)), nil
}

func decodeStrict(body []byte, dest any) error {
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dest); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("extra")
	}
	return nil
}

func okResult(result string, detail map[string]any) ActionResult {
	return ActionResult{Result: result, ChainSubmitted: false, ConsensusKeyAccepted: false, Detail: detail}
}

func refused(code string) ActionResult {
	return ActionResult{Error: code, Result: "not_submitted", ChainSubmitted: false, ConsensusKeyAccepted: false}
}

func upstream(code int, doc []byte) error {
	var row struct {
		Error string `json:"error"`
	}
	_ = json.Unmarshal(doc, &row)
	if row.Error == "" {
		row.Error = fmt.Sprintf("upstream %d", code)
	}
	status := 502
	if code >= 400 && code < 500 {
		status = code
	}
	return fail(status, row.Error)
}

// Onboarding returns the stored choice, or an absent reading.
func (s *Service) Onboarding() any {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onboard == nil {
		return map[string]any{"class": "absent", "note": "no onboarding has been recorded", "chain_submitted": false}
	}
	return s.onboard
}
