package status

import (
	"testing"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/store"
)

func TestStoppedChainIsStaleWithoutARenewal(t *testing.T) {
	view := Evaluate(Input{
		ChainID: "shortvideo-dev-t", Gateway: "http://127.0.0.1:26657",
		IndexedHeight: 4, HaveIndex: true,
		Obs: store.Observation{
			Found: true, CanonicalHeight: 5, AppFloor: 4,
			BlockTime: time.Unix(1_000, 0).UTC(), CommitAge: 30 * time.Second, ObserverAge: time.Second,
		},
		Run:           store.LatestRun{Found: true, Status: "ok", At: time.Unix(1_000, 0).UTC()},
		StaleAfter:    8 * time.Second,
		ObserverFresh: 4 * time.Second,
	})
	if !view.Stale {
		t.Fatal("a chain whose commit is older than the threshold is stale")
	}
	found := false
	for _, reason := range view.StaleReasons {
		if reason == "commit_age" {
			found = true
		}
		if reason == "renewed" || reason == "approved" {
			t.Fatalf("stale status invented %s", reason)
		}
	}
	if !found {
		t.Fatalf("reasons %v", view.StaleReasons)
	}
	if view.Trust.LightClient || view.DatabaseRole != "read_model" || view.Trust.Note == "" {
		t.Fatalf("trust %+v", view.Trust)
	}
}

func TestFreshRead(t *testing.T) {
	view := Evaluate(Input{
		ChainID: "shortvideo-dev-t", IndexedHeight: 4, HaveIndex: true,
		Obs: store.Observation{
			Found: true, CanonicalHeight: 5, AppFloor: 4,
			CommitAge: time.Second, ObserverAge: time.Second,
		},
		Run:           store.LatestRun{Found: true, Status: "ok"},
		StaleAfter:    8 * time.Second,
		ObserverFresh: 4 * time.Second,
	})
	if view.Stale {
		t.Fatalf("fresh read is stale: %v", view.StaleReasons)
	}
}

func TestDivergentIsStale(t *testing.T) {
	view := Evaluate(Input{
		ChainID: "shortvideo-dev-t", IndexedHeight: 4, HaveIndex: true,
		Obs: store.Observation{Found: true, CanonicalHeight: 5, AppFloor: 4, CommitAge: time.Second, ObserverAge: time.Second},
		Run: store.LatestRun{Found: true, Status: "divergent"}, StaleAfter: 8 * time.Second, ObserverFresh: 4 * time.Second,
	})
	if !view.Stale || view.ReconciliationStatus != "divergent" {
		t.Fatalf("%+v", view)
	}
}
