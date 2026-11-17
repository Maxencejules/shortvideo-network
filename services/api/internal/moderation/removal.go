package moderation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sync"
	"time"
)

// Stages a removal can interrupt. Repair after a stop must not copy the
// bytes back.
const (
	StageUploading   = "uploading"
	StageProcessing  = "processing"
	StageReplicating = "replicating"
	StageRepair      = "repair"
	StagePlayback    = "playback"
)

// Endpoint is one cooperating service. Kind is "controlled" or "independent".
// Delist is used in-process. URL is used when Delist is nil.
type Endpoint struct {
	Name   string
	Kind   string
	URL    string
	Delist func(contentID string, now time.Time) (Ack, error)
}

// Ack is one host's answer. Unreachable is not success.
type Ack struct {
	Name            string  `json:"name"`
	Kind            string  `json:"kind"`
	Endpoint        string  `json:"endpoint"`
	At              string  `json:"at,omitempty"`
	ElapsedSeconds  float64 `json:"elapsed_seconds"`
	WithinGate      bool    `json:"within_proposed_gate"`
	Unreachable     bool    `json:"unreachable"`
	Late            bool    `json:"late"`
	CacheGeneration int64   `json:"cache_generation,omitempty"`
	BytesDeleted    bool    `json:"bytes_deleted"`
	ChainEnd        bool    `json:"chain_end"`
}

// Report is the removal result. SuccessfulDeletion is false when any
// cooperating host did not acknowledge, including an independent host.
type Report struct {
	ContentID                  string  `json:"content_id"`
	Stage                      string  `json:"stage,omitempty"`
	StageStopped               bool    `json:"stage_stopped"`
	LocalDelisted              bool    `json:"local_delisted"`
	WaitedForFinality          bool    `json:"waited_for_finality"`
	ChainStalled               bool    `json:"chain_stalled"`
	ChainCommitSeen            bool    `json:"chain_commit_seen"`
	ChainOverrodeLocal         bool    `json:"chain_overrode_local"`
	Gate                       string  `json:"gate"`
	GateSeconds                float64 `json:"proposed_gate_seconds"`
	ElapsedSeconds             float64 `json:"elapsed_seconds"`
	Hosts                      []Ack   `json:"hosts"`
	ControlledAcked            int     `json:"controlled_acked"`
	ControlledUnreachable      int     `json:"controlled_unreachable"`
	IndependentUnreachable     int     `json:"independent_unreachable"`
	Complete                   bool    `json:"complete"`
	SuccessfulDeletion         bool    `json:"successful_deletion"`
	IndependentHostDeleted     bool    `json:"independent_host_deleted"`
	LedgerRetained             bool    `json:"ledger_retained"`
	IndependentCopiesDisclosed bool    `json:"independent_copies_disclosed"`
	UniversalDeletion          bool    `json:"universal_deletion"`
	Retention                  string  `json:"retention"`
	NarrativeRetained          bool    `json:"narrative_retained"`
	LegalApproval              bool    `json:"legal_approval"`
	PublicLaunch               string  `json:"public_launch"`
	RepairRevived              bool    `json:"repair_revived"`
}

// Chain is the coordination ledger. Stalled means local delist must not wait.
type Chain struct {
	Stalled bool
	Commit  func() (bool, error)
}

// Options configures one coordinator. Now is the clock the gate is measured with.
type Options struct {
	Now       func() time.Time
	Pipeline  *Pipeline
	Endpoints []Endpoint
	Chain     Chain
	Client    *http.Client
}

// Coordinator delists locally first, then asks each endpoint.
type Coordinator struct {
	now       func() time.Time
	pipeline  *Pipeline
	endpoints []Endpoint
	chain     Chain
	client    *http.Client
	mu        sync.Mutex
	delisted  map[string]struct{}
}

// New returns a coordinator. A nil clock uses time.Now.
func New(opt Options) *Coordinator {
	now := opt.Now
	if now == nil {
		now = time.Now
	}
	pipe := opt.Pipeline
	if pipe == nil {
		pipe = NewPipeline()
	}
	client := opt.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("redirect")
		}}
	}
	return &Coordinator{
		now: now, pipeline: pipe, endpoints: opt.Endpoints, chain: opt.Chain,
		client: client, delisted: map[string]struct{}{},
	}
}

// Pipeline is the local media lifecycle this process can stop.
type Pipeline struct {
	mu        sync.Mutex
	stage     map[string]string
	cancelled map[string]struct{}
	bytes     map[string]bool
	playing   map[string]bool
	repair    map[string]bool
}

// NewPipeline returns an empty lifecycle.
func NewPipeline() *Pipeline {
	return &Pipeline{
		stage: map[string]string{}, cancelled: map[string]struct{}{},
		bytes: map[string]bool{}, playing: map[string]bool{}, repair: map[string]bool{},
	}
}

// Set records the stage a content id is in, for a removal test or a local worker.
func (p *Pipeline) Set(contentID, stage string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stage[contentID] = stage
	switch stage {
	case StageUploading, StageProcessing, StageReplicating:
		p.bytes[contentID] = true
	case StageRepair:
		p.repair[contentID] = true
		p.bytes[contentID] = true
	case StagePlayback:
		p.playing[contentID] = true
	}
}

// Stop halts the current stage. It does not wait for a chain commit.
func (p *Pipeline) Stop(contentID string) (stage string, stopped bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	stage = p.stage[contentID]
	p.cancelled[contentID] = struct{}{}
	p.bytes[contentID] = false
	p.playing[contentID] = false
	p.repair[contentID] = false
	return stage, true
}

// AllowRepair reports whether a repair copy may start. A stopped id is refused.
func (p *Pipeline) AllowRepair(contentID string) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	_, stopped := p.cancelled[contentID]
	return !stopped
}

// Holding reports bytes, playback, and repair flags after a stop.
func (p *Pipeline) Holding(contentID string) (bytes, playing, repair bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.bytes[contentID], p.playing[contentID], p.repair[contentID]
}

// Delisted reports whether Remove has already recorded this content id.
func (c *Coordinator) Delisted(contentID string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.delisted[contentID]
	return ok
}

// Remove stops the local stage, then asks each endpoint. It does not wait
// for chain finality. A commit observed later does not clear the delist.
func (c *Coordinator) Remove(contentID string) Report {
	start := c.now()
	c.mu.Lock()
	c.delisted[contentID] = struct{}{}
	c.mu.Unlock()
	stage, stopped := c.pipeline.Stop(contentID)
	waited := false
	commitSeen := false
	if c.chain.Stalled {
		waited = false
	} else if c.chain.Commit != nil {
		ok, _ := c.chain.Commit()
		commitSeen = ok
		waited = false
	}
	hosts := make([]Ack, 0, len(c.endpoints)+1)
	hosts = append(hosts, Ack{
		Name: "local", Kind: "controlled", Endpoint: "pipeline:" + stage,
		At: c.now().Format(time.RFC3339), ElapsedSeconds: c.now().Sub(start).Seconds(),
		WithinGate: true, BytesDeleted: stopped, ChainEnd: false,
	})
	for _, ep := range c.endpoints {
		hosts = append(hosts, c.ask(ep, contentID, start))
	}
	end := c.now()
	report := Report{
		ContentID: contentID, Stage: stage, StageStopped: stopped, LocalDelisted: true,
		WaitedForFinality: waited, ChainStalled: c.chain.Stalled, ChainCommitSeen: commitSeen,
		ChainOverrodeLocal: false, Gate: "PROPOSED", GateSeconds: ProposedDelistGate.Seconds(),
		ElapsedSeconds: end.Sub(start).Seconds(), Hosts: hosts,
		LedgerRetained: true, IndependentCopiesDisclosed: true, UniversalDeletion: false,
		Retention: "DEC-004 unset; minimum records kept", NarrativeRetained: false,
		LegalApproval: false, PublicLaunch: "DISABLED", RepairRevived: false,
	}
	report.SuccessfulDeletion = true
	controlled := 0
	independent := 0
	independentAcked := 0
	for _, host := range hosts {
		if host.Kind == "independent" {
			independent++
			if host.Unreachable || host.Late || !host.WithinGate {
				report.IndependentUnreachable++
				report.SuccessfulDeletion = false
				continue
			}
			independentAcked++
			continue
		}
		controlled++
		if host.Unreachable || host.Late || !host.WithinGate {
			report.ControlledUnreachable++
			report.SuccessfulDeletion = false
			continue
		}
		report.ControlledAcked++
	}
	report.IndependentHostDeleted = independent > 0 && independentAcked == independent
	if controlled == 0 {
		report.SuccessfulDeletion = false
	}
	report.Complete = report.SuccessfulDeletion
	if !c.Delisted(contentID) {
		report.ChainOverrodeLocal = true
		report.SuccessfulDeletion = false
		report.Complete = false
	}
	return report
}

func (c *Coordinator) ask(ep Endpoint, contentID string, start time.Time) Ack {
	ack := Ack{Name: ep.Name, Kind: ep.Kind, Endpoint: ep.URL, ChainEnd: false}
	if ep.Kind != "controlled" && ep.Kind != "independent" {
		ack.Kind = "independent"
	}
	var err error
	if ep.Delist != nil {
		ack, err = ep.Delist(contentID, c.now())
		ack.Name = ep.Name
		if ack.Kind == "" {
			ack.Kind = ep.Kind
		}
		if ack.Endpoint == "" {
			ack.Endpoint = ep.URL
		}
	} else if ep.URL != "" {
		ack, err = c.post(ep, contentID)
	} else {
		err = errors.New("no endpoint")
	}
	if err != nil {
		ack.Name = ep.Name
		ack.Kind = ep.Kind
		if ack.Kind == "" {
			ack.Kind = "independent"
		}
		ack.Endpoint = ep.URL
		ack.Unreachable = true
		ack.BytesDeleted = false
	}
	ack.ChainEnd = false
	elapsed := c.now().Sub(start)
	ack.ElapsedSeconds = elapsed.Seconds()
	if ack.At == "" {
		ack.At = c.now().Format(time.RFC3339)
	}
	ack.WithinGate = !ack.Unreachable && !ack.Late && elapsed <= ProposedDelistGate
	if !ack.WithinGate && !ack.Unreachable {
		ack.Late = true
	}
	return ack
}

func (c *Coordinator) post(ep Endpoint, contentID string) (Ack, error) {
	body, _ := json.Marshal(map[string]string{"content_id": contentID})
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, ep.URL, bytes.NewReader(body))
	if err != nil {
		return Ack{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	res, err := c.client.Do(req)
	if err != nil {
		return Ack{}, err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(res.Body, 1<<16))
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Ack{}, errors.New(res.Status)
	}
	var doc struct {
		CacheGeneration int64  `json:"cache_generation"`
		BytesDeleted    bool   `json:"bytes_deleted"`
		Endpoint        string `json:"endpoint"`
	}
	_ = json.Unmarshal(raw, &doc)
	return Ack{
		Endpoint: doc.Endpoint, CacheGeneration: doc.CacheGeneration, BytesDeleted: doc.BytesDeleted,
	}, nil
}
