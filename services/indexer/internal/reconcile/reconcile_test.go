package reconcile

import (
	"testing"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/store"
)

func TestAlteredProjectionIsDetected(t *testing.T) {
	proj := store.Projection{
		Content: []store.ContentRow{{
			ID: "c1", Version: 1, Manifest: "aa", PackageBytes: "4",
		}},
		Leases: []store.LeaseRow{{
			ID: "l1", ProviderID: "p1", ContentID: "c1", State: "ACTIVE", Term: 1, Deadline: 100, Active: true,
		}},
		HasPolicy: true, Policy: store.PolicyRow{Version: 3, ParameterKey: "page"},
	}
	snap := Snapshot{
		Content: map[string]chain.ContentView{
			"c1/1": {ID: "c1", Version: 1, Manifest: "bb", PackageBytes: 4},
		},
		Leases:    []chain.LeaseView{{ID: "l1", ProviderID: "p1", ContentID: "c1", State: "ACTIVE", Term: 1, Deadline: 100}},
		Deadlines: []chain.DeadlineView{{Kind: "lease", ID: "l1", At: 100}},
		HasPolicy: true, Policy: 3,
	}
	findings := Compare(proj, snap)
	if !has(findings, "content_manifest", "c1/1") {
		t.Fatalf("manifest alteration not detected: %+v", findings)
	}
	if has(findings, "lease_mismatch", "l1") {
		t.Fatal("an unchanged lease was reported as renewed or mismatched")
	}
}

func TestQueryErrorIsNotEmptyHistory(t *testing.T) {
	proj := store.Projection{Providers: []store.ProviderRow{{ID: "p1", State: "ADMITTED"}}}
	findings := Compare(proj, Snapshot{Err: chain.ErrQuery})
	if len(findings) != 0 {
		t.Fatalf("a failed query produced findings that look like an empty chain: %+v", findings)
	}
}

func TestLeaseDeadlineChange(t *testing.T) {
	proj := store.Projection{Leases: []store.LeaseRow{{
		ID: "l1", ProviderID: "p1", State: "ACTIVE", Term: 1, Deadline: 50, Active: true,
	}}}
	snap := Snapshot{
		Leases:    []chain.LeaseView{{ID: "l1", State: "ACTIVE", Term: 1, Deadline: 100}},
		Deadlines: []chain.DeadlineView{{Kind: "lease", ID: "l1", At: 100}},
	}
	findings := Compare(proj, snap)
	if !has(findings, "lease_mismatch", "l1") || !has(findings, "deadline_mismatch", "l1") {
		t.Fatalf("%+v", findings)
	}
}

func has(findings []store.Finding, kind, id string) bool {
	for _, f := range findings {
		if f.Kind == kind && f.ObjectID == id {
			return true
		}
	}
	return false
}
