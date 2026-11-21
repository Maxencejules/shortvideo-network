// Package obs is the goal 25 control plane.
//
// Thresholds are placeholders. They are not a DEC-005 allowance and they are
// not a spend cap. Alerts do not admit or refuse work. The budget owner is
// unassigned. Correlation ids are random and are not account identifiers.
// Chain notes stay off the chain. Playback notes are aggregates.
package obs

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	// LabelPlaceholder marks every numeric threshold in this process.
	LabelPlaceholder = "PLACEHOLDER"
	// BudgetOwnerUnassigned is DR-25-01. No person is named.
	BudgetOwnerUnassigned = "UNASSIGNED"
	// CurrencyPreference is the blueprint's proposed planning currency.
	// CurrencyApproved is false until an operator supplies a rate card.
	CurrencyPreference = "CAD"

	// The following numbers are placeholders for tests of the mechanism.
	// They are not allowances, not measured demand, and not a budget.
	DefaultUploadLimit     int64 = 3
	DefaultProcessingLimit int64 = 2
	DefaultProcessingQueue int64 = 2
	DefaultOutputBytes     int64 = 1024
	DefaultStorageBytes    int64 = 4096
	DefaultAuditLimit      int64 = 2
	DefaultRetryLimit      int64 = 2
	DefaultLoginLimit      int64 = 3
	DefaultSearchLimit     int64 = 3
	DefaultReportLimit     int64 = 2
	DefaultDiscoveryLimit  int64 = 3
	DefaultCommitAge       int64 = 20
	DefaultProcessingAge   int64 = 30
	DefaultDiskFloor       int64 = 1024
)

const (
	CompAPI         = "api"
	CompJobs        = "jobs"
	CompBroadcaster = "broadcaster"
	CompIndexer     = "indexer"
	CompChain       = "chain"
	CompProviders   = "providers"
	CompPlayback    = "playback"
)

const (
	ClassUserPayload = "user_payload"
	ClassReplication = "replication"
	ClassRepair      = "repair"
	ClassPrefetch    = "prefetch"
	ClassBackup      = "backup"
)

const (
	KindUploads     = "uploads"
	KindProcessing  = "processing_concurrency"
	KindOutputBytes = "output_bytes"
	KindStorage     = "storage_reservations"
	KindAudit       = "audit_batches"
	KindRetries     = "retries"
	KindLogin       = "login"
	KindSearch      = "search"
	KindReport      = "report"
	KindDiscovery   = "discovery"
)

// Unmeasured are traffic classes this process does not count. A missing class
// is not a zero and it is not a saving. interface_bandwidth and
// scheduler_job_queue are the open readings from DR-24-07.
var Unmeasured = []string{
	"protocol_overhead",
	"turn_relay",
	"tls_records",
	"chain_gossip",
	"interface_bandwidth",
	"scheduler_job_queue",
}

// RequiredComponents are the processes goal 25 instruments. Chain rows are
// observations held here. They are not chain transactions.
var RequiredComponents = []string{
	CompAPI, CompJobs, CompBroadcaster, CompIndexer, CompChain, CompProviders, CompPlayback,
}

var (
	ErrToken       = errors.New("observer token is missing")
	ErrKind        = errors.New("unknown limit")
	ErrComponent   = errors.New("unknown component")
	ErrClass       = errors.New("unknown traffic class")
	ErrUnits       = errors.New("units are not allowed")
	ErrOffChain    = errors.New("media bytes stay off the chain")
	ErrGrant       = errors.New("unknown grant")
	ErrObservation = errors.New("observation is not valid")
	ErrKey         = errors.New("idempotency key is not valid")
)

// Options configures one process. Token is required and is never logged.
// Limits and Queues replace the placeholder caps when set. A missing kind
// keeps the placeholder. Queue 0 is a hard stop with no waiting room.
type Options struct {
	Token      string
	Now        func() time.Time
	LogEntries int
	LogBytes   int
	Lookup     Lookup
	Limits     map[string]int64
	Queues     map[string]int64
}

// Decision is one admission result. SpendCap is always false.
type Decision struct {
	Kind           string `json:"kind"`
	Granted        bool   `json:"granted"`
	Queued         bool   `json:"queued"`
	Replayed       bool   `json:"replayed"`
	Reason         string `json:"reason"`
	GrantID        string `json:"grant_id,omitempty"`
	SpendCap       bool   `json:"spend_cap"`
	BudgetApproved bool   `json:"budget_approved"`
	ThresholdLabel string `json:"threshold_label"`
}

// LogLine is one retained line after redaction.
type LogLine struct {
	Component     string `json:"component"`
	CorrelationID string `json:"correlation_id"`
	Text          string `json:"text"`
}

type spec struct {
	limit  int64
	queue  int64
	window bool
}

type grant struct {
	id    string
	kind  string
	units int64
	state string
}

type limiter struct {
	limit  int64
	queue  int64
	window bool
	used   int64
	queued int64
	start  time.Time
	order  []string
	grants map[string]*grant
	idem   map[string]string
	seen   map[string]Decision
}

type dependency struct {
	critical bool
	usable   bool
}

// Service is safe for concurrent admission.
type Service struct {
	mu              sync.Mutex
	token           string
	now             func() time.Time
	lookup          Lookup
	maxEntries      int
	maxLogBytes     int
	logs            []LogLine
	limits          map[string]*limiter
	deps            map[string]*dependency
	gauges          map[string]int64
	gaugeSet        map[string]bool
	counters        map[string]int64
	delivered       map[string]int64
	storage         map[string]int64
	alerts          map[string]bool
	denials         map[string]int64
	traces          map[string][]string
	diskKnown       bool
	diskFree        int64
	diskFloor       int64
	processingKnown bool
	processingAge   int64
	processingLimit int64
	commitKnown     bool
	commitAge       int64
	commitLimit     int64
	quorumKnown     bool
	quorumLost      bool
	indexLag        int64
	repairBacklog   int64
	playback        PlaybackSnapshot
	rates           Rates
	founderSet      bool
	founderMinor    int64
	volunteer       string
	chainWrites     int
}

// New returns a process that is not ready until the critical dependencies
// report usable. Chain becomes usable only from a fresh commit reading with
// quorum held.
func New(opt Options) (*Service, error) {
	if len(opt.Token) < 16 || strings.ContainsAny(opt.Token, " \r\n") {
		return nil, ErrToken
	}
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	entries := opt.LogEntries
	if entries == 0 {
		entries = 64
	}
	if entries < 1 || entries > 10000 {
		return nil, errors.New("log entries out of range")
	}
	logBytes := opt.LogBytes
	if logBytes == 0 {
		logBytes = 16384
	}
	if logBytes < 256 || logBytes > 1<<20 {
		return nil, errors.New("log bytes out of range")
	}
	s := &Service{
		token:           opt.Token,
		now:             now,
		lookup:          opt.Lookup,
		maxEntries:      entries,
		maxLogBytes:     logBytes,
		limits:          map[string]*limiter{},
		deps:            map[string]*dependency{},
		gauges:          map[string]int64{},
		gaugeSet:        map[string]bool{},
		counters:        map[string]int64{},
		delivered:       map[string]int64{},
		storage:         map[string]int64{},
		alerts:          map[string]bool{},
		denials:         map[string]int64{},
		traces:          map[string][]string{},
		diskFloor:       DefaultDiskFloor,
		processingLimit: DefaultProcessingAge,
		commitLimit:     DefaultCommitAge,
	}
	specs := map[string]spec{
		KindUploads:     {limit: DefaultUploadLimit, window: true},
		KindProcessing:  {limit: DefaultProcessingLimit, queue: DefaultProcessingQueue},
		KindOutputBytes: {limit: DefaultOutputBytes},
		KindStorage:     {limit: DefaultStorageBytes},
		KindAudit:       {limit: DefaultAuditLimit, window: true},
		KindRetries:     {limit: DefaultRetryLimit, window: true},
		KindLogin:       {limit: DefaultLoginLimit, window: true},
		KindSearch:      {limit: DefaultSearchLimit, window: true},
		KindReport:      {limit: DefaultReportLimit, window: true},
		KindDiscovery:   {limit: DefaultDiscoveryLimit, window: true},
	}
	at := now()
	for kind, sp := range specs {
		s.limits[kind] = &limiter{
			limit: sp.limit, queue: sp.queue, window: sp.window, start: at,
			grants: map[string]*grant{}, idem: map[string]string{}, seen: map[string]Decision{},
		}
	}
	for _, name := range []string{"chain", "indexer", "database"} {
		s.deps[name] = &dependency{critical: true, usable: false}
	}
	for kind, n := range opt.Limits {
		lim, ok := s.limits[kind]
		if !ok || n < 1 {
			return nil, ErrKind
		}
		lim.limit = n
	}
	for kind, n := range opt.Queues {
		lim, ok := s.limits[kind]
		if !ok || n < 0 {
			return nil, ErrKind
		}
		lim.queue = n
	}
	return s, nil
}

// NewCorrelationID returns a random id. It is not derived from an account.
func NewCorrelationID() (string, error) {
	var buf [16]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return "c" + hex.EncodeToString(buf[:]), nil
}

func correlationOK(id string) bool {
	if len(id) != 33 || id[0] != 'c' {
		return false
	}
	for i := 1; i < len(id); i++ {
		c := id[i]
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}

func componentOK(name string) bool {
	switch name {
	case CompAPI, CompJobs, CompBroadcaster, CompIndexer, CompChain, CompProviders, CompPlayback:
		return true
	default:
		return false
	}
}

func classOK(name string) bool {
	switch name {
	case ClassUserPayload, ClassReplication, ClassRepair, ClassPrefetch, ClassBackup:
		return true
	default:
		return false
	}
}

// ReportDep marks indexer or database. Chain usability comes only from a
// commit-age reading and a quorum reading.
func (s *Service) ReportDep(name string, usable bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if name == "chain" {
		return errors.New("chain usability comes from a commit reading")
	}
	if name != "indexer" && name != "database" {
		return errors.New("unknown dependency")
	}
	s.deps[name].usable = usable
	return nil
}

// Ready is false while any critical dependency is unusable.
func (s *Service) Ready() (bool, []string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readyLocked()
}

func (s *Service) readyLocked() (bool, []string) {
	var bad []string
	for _, name := range []string{"chain", "database", "indexer"} {
		dep := s.deps[name]
		if dep.critical && !dep.usable {
			bad = append(bad, name)
		}
	}
	return len(bad) == 0, bad
}

// Diagnosis names quorum loss and index lag separately. An empty condition is "ok".
func (s *Service) Diagnosis() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.diagnosisLocked()
}

func (s *Service) diagnosisLocked() string {
	var parts []string
	if s.quorumKnown && s.quorumLost {
		parts = append(parts, "quorum_loss")
	}
	if s.commitKnown && s.commitAge > s.commitLimit {
		parts = append(parts, "stale_commit")
	}
	if s.indexLag > 0 {
		parts = append(parts, "index_lag")
	}
	if !s.deps["indexer"].usable && s.indexLag == 0 {
		parts = append(parts, "indexer_unusable")
	}
	if len(parts) == 0 {
		return "ok"
	}
	return strings.Join(parts, "+")
}

// RaiseAlert records an alert and does not change admission.
func (s *Service) RaiseAlert(name string) error {
	known := map[string]bool{
		"stale_commit": true, "quorum_loss": true, "index_lag": true,
		"replicas": true, "repair_backlog": true, "processing_age": true,
		"low_disk": true, "playback_errors": true, "unreviewed_reports": true,
		"admission": true,
	}
	if !known[name] {
		return errors.New("unknown alert")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.alerts[name] = true
	return nil
}

// Levels reports capacity counters for tests and the usage report.
func (s *Service) Levels(kind string) (used, queued int64, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	lim, ok := s.limits[kind]
	if !ok {
		return 0, 0, ErrKind
	}
	return lim.used, lim.queued, nil
}

// ChainSubmissions is always zero. This process does not submit transactions.
func (s *Service) ChainSubmissions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.chainWrites
}

// Trace returns the components that shared one correlation id, in order.
func (s *Service) Trace(id string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]string(nil), s.traces[id]...)
	return out
}

// Logs returns a copy of the retained lines.
func (s *Service) Logs() []LogLine {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]LogLine(nil), s.logs...)
}

func (s *Service) appendLog(component, id, text string) {
	line := LogLine{
		Component:     component,
		CorrelationID: id,
		Text:          clip(redact(s.token, text)),
	}
	s.logs = append(s.logs, line)
	for s.overLogCap() {
		if len(s.logs) == 0 {
			return
		}
		s.logs = s.logs[1:]
	}
}

func (s *Service) overLogCap() bool {
	if len(s.logs) > s.maxEntries {
		return true
	}
	n := 0
	for _, line := range s.logs {
		n += len(line.Component) + len(line.CorrelationID) + len(line.Text)
	}
	return n > s.maxLogBytes
}

func (s *Service) authorize(header string) bool {
	const prefix = "Bearer "
	if len(header) < len(prefix) || header[:len(prefix)] != prefix {
		return false
	}
	got := header[len(prefix):]
	if len(got) != len(s.token) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.token)) == 1
}

func decision(kind, reason, id string, granted, queued bool) Decision {
	return Decision{
		Kind: kind, Granted: granted, Queued: queued, Reason: reason, GrantID: id,
		SpendCap: false, BudgetApproved: false, ThresholdLabel: LabelPlaceholder,
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k, on := range m {
		if on {
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}
