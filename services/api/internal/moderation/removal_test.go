package moderation

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestGateIgnoresACommittedPublication(t *testing.T) {
	g := NewGate()
	if !g.UploadAllowed("creator1") || !g.PublishAllowed("c1") {
		t.Fatal("empty gate refused")
	}
	g.BlockAccount("creator1")
	g.Delist("c1")
	g.NoteChainPublication("c1", true)
	if g.UploadAllowed("creator1") || g.PublishAllowed("c1") {
		t.Fatal("a commit cleared a local refusal")
	}
	if !g.UploadAllowed("other1") || !g.PublishAllowed("c2") {
		t.Fatal("unrelated id was refused")
	}
}

func TestContactFileCannotGrantApproval(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "policy.json")
	raw := []byte(`{"legal_approval":true,"public_launch":"ENABLED","contacts":{"trust_safety":"PLACEHOLDER desk"}}`)
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	doc, err := LoadContacts(path)
	if err != nil {
		t.Fatal(err)
	}
	if doc.LegalApproval || doc.PublicLaunch != "DISABLED" || !doc.ContactOverrideIgnored {
		t.Fatalf("%+v", doc)
	}
	if doc.Contacts["trust_safety"] != "PLACEHOLDER desk" {
		t.Fatal(doc.Contacts)
	}
	if doc.Retention.Duration != nil || doc.Audience.MinorSafetyClaim || doc.ValidatorsDecideLegality {
		t.Fatalf("%+v", doc)
	}
	encoded, err := json.Marshal(Document())
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, []byte(`"legal_approval":true`)) {
		t.Fatal(string(encoded))
	}
}

func TestRemovalStopsEveryStageWithoutWaiting(t *testing.T) {
	for _, stage := range []string{StageUploading, StageProcessing, StageReplicating, StageRepair, StagePlayback} {
		pipe := NewPipeline()
		pipe.Set("c1", stage)
		c := New(Options{Pipeline: pipe, Now: func() time.Time { return time.Unix(1_700_000_000, 0) }})
		report := c.Remove("c1")
		if report.Stage != stage || !report.StageStopped || report.WaitedForFinality || report.UniversalDeletion {
			t.Fatalf("%s %+v", stage, report)
		}
		bytes, playing, repair := pipe.Holding("c1")
		if bytes || playing || repair || pipe.AllowRepair("c1") {
			t.Fatalf("%s revived bytes=%t playing=%t repair=%t", stage, bytes, playing, repair)
		}
		if !c.Delisted("c1") {
			t.Fatal("delist was not recorded")
		}
	}
}

func TestStalledChainAndUnreachableHosts(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	now := start
	clock := func() time.Time { return now }
	committed := false
	var posts int
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		posts++
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"endpoint":"/v1/policy/delist","cache_generation":4,"bytes_deleted":true}`))
	}))
	defer up.Close()
	down := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	down.Close()
	pipe := NewPipeline()
	pipe.Set("c-stall", StagePlayback)
	c := New(Options{
		Now: clock, Pipeline: pipe,
		Chain: Chain{Stalled: true, Commit: func() (bool, error) {
			committed = true
			return true, nil
		}},
		Endpoints: []Endpoint{
			{Name: "upload", Kind: "controlled", URL: up.URL},
			{Name: "provider-down", Kind: "controlled", URL: down.URL},
			{Name: "independent", Kind: "independent", Delist: func(string, time.Time) (Ack, error) {
				return Ack{}, os.ErrDeadlineExceeded
			}},
		},
	})
	report := c.Remove("c-stall")
	if committed {
		t.Fatal("stalled chain was asked to commit")
	}
	if report.WaitedForFinality || !report.ChainStalled || !report.LocalDelisted || report.ChainOverrodeLocal {
		t.Fatalf("%+v", report)
	}
	if report.SuccessfulDeletion || report.IndependentHostDeleted || report.Complete {
		t.Fatalf("incomplete deletion was reported as success: %+v", report)
	}
	if report.ControlledUnreachable != 1 || report.IndependentUnreachable != 1 || posts != 1 {
		t.Fatalf("hosts %+v posts %d", report, posts)
	}
	if pipe.AllowRepair("c-stall") {
		t.Fatal("repair was allowed after a stall")
	}
	c.chain.Commit = func() (bool, error) { return true, nil }
	c.chain.Stalled = false
	if !c.Delisted("c-stall") {
		t.Fatal("delist cleared")
	}
}

func TestProposedFiveMinuteGate(t *testing.T) {
	start := time.Unix(1_700_000_000, 0)
	now := start
	clock := func() time.Time { return now }
	c := New(Options{
		Now: clock,
		Endpoints: []Endpoint{
			{Name: "discovery", Kind: "controlled", Delist: func(string, time.Time) (Ack, error) {
				now = start.Add(time.Second)
				return Ack{Endpoint: "/v1/feeds/following", CacheGeneration: 2, BytesDeleted: false}, nil
			}},
			{Name: "delivery", Kind: "controlled", Delist: func(string, time.Time) (Ack, error) {
				now = start.Add(4 * time.Minute)
				return Ack{Endpoint: "/v1/playback/c1", CacheGeneration: 9, BytesDeleted: true}, nil
			}},
			{Name: "slow-cache", Kind: "controlled", Delist: func(string, time.Time) (Ack, error) {
				now = start.Add(ProposedDelistGate + time.Second)
				return Ack{Endpoint: "/v1/media/c1", CacheGeneration: 9}, nil
			}},
			{Name: "other-community", Kind: "independent", Delist: func(string, time.Time) (Ack, error) {
				return Ack{}, os.ErrDeadlineExceeded
			}},
		},
	})
	report := c.Remove("c1")
	if report.Gate != "PROPOSED" || report.GateSeconds != 300 {
		t.Fatalf("gate %+v", report)
	}
	if report.SuccessfulDeletion || report.IndependentHostDeleted {
		t.Fatal("late controlled host or silent independent host counted as deletion")
	}
	var within, late, independentUnreachable int
	for _, host := range report.Hosts {
		t.Logf("GATE name=%s kind=%s endpoint=%s elapsed=%.0f within=%t late=%t unreachable=%t cache=%d",
			host.Name, host.Kind, host.Endpoint, host.ElapsedSeconds, host.WithinGate, host.Late, host.Unreachable, host.CacheGeneration)
		switch host.Name {
		case "discovery", "delivery", "local":
			if !host.WithinGate {
				t.Fatalf("expected inside the gate: %+v", host)
			}
			within++
		case "slow-cache":
			if host.WithinGate || !host.Late || host.ElapsedSeconds <= 300 {
				t.Fatalf("slow host %+v", host)
			}
			late++
		case "other-community":
			if !host.Unreachable || host.BytesDeleted {
				t.Fatalf("independent %+v", host)
			}
			independentUnreachable++
		}
	}
	if within != 3 || late != 1 || independentUnreachable != 1 {
		t.Fatalf("within %d late %d independent %d", within, late, independentUnreachable)
	}
	if report.Hosts[1].CacheGeneration != 2 || report.Hosts[2].CacheGeneration != 9 {
		t.Fatalf("cache evidence %+v", report.Hosts)
	}
}
