// Package reconcile compares the projection to chain queries. It records
// differences and does not rewrite a provider, a content row, a lease, a
// deadline or a policy version. A query error is blocked, not an empty
// history.
package reconcile

import (
	"context"
	"fmt"
	"strconv"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/chain"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/store"
)

// Snapshot is one reconciliation read. Err set means the read did not finish.
type Snapshot struct {
	Height    int64
	Providers []chain.ProviderView
	Content   map[string]chain.ContentView
	Missing   map[string]bool
	Leases    []chain.LeaseView
	Deadlines []chain.DeadlineView
	Policy    uint64
	HasPolicy bool
	Err       error
}

// Compare returns findings. It does not modify the projection. An unfinished
// snapshot returns no findings: the caller records blocked instead of
// treating the failure as an empty chain.
func Compare(proj store.Projection, snap Snapshot) []store.Finding {
	if snap.Err != nil {
		return nil
	}
	var out []store.Finding
	byProvider := map[string]chain.ProviderView{}
	for _, p := range snap.Providers {
		byProvider[p.ID] = p
	}
	seenProvider := map[string]bool{}
	for _, row := range proj.Providers {
		seenProvider[row.ID] = true
		got, ok := byProvider[row.ID]
		if !ok || got.State != row.State || (row.KeyGeneration > 0 && int64(got.KeyGeneration) != row.KeyGeneration) {
			out = append(out, store.Finding{Kind: "provider_mismatch", ObjectID: row.ID})
		}
	}
	for id := range byProvider {
		if !seenProvider[id] {
			out = append(out, store.Finding{Kind: "provider_missing", ObjectID: id})
		}
	}
	for _, row := range proj.Content {
		key := row.ID + "/" + strconv.FormatUint(row.Version, 10)
		if snap.Missing[key] {
			if !row.Withdrawn {
				out = append(out, store.Finding{Kind: "content_missing", ObjectID: key})
			}
			continue
		}
		got, ok := snap.Content[key]
		if !ok {
			out = append(out, store.Finding{Kind: "content_unread", ObjectID: key})
			continue
		}
		if got.Withdrawn != row.Withdrawn || got.HistoryErased != row.HistoryErased {
			out = append(out, store.Finding{Kind: "content_withdrawn", ObjectID: key})
		}
		if !got.Withdrawn && (got.Manifest != row.Manifest || strconv.FormatUint(got.PackageBytes, 10) != row.PackageBytes) {
			out = append(out, store.Finding{Kind: "content_manifest", ObjectID: key})
		}
	}
	byLease := map[string]chain.LeaseView{}
	for _, lease := range snap.Leases {
		byLease[lease.ID] = lease
	}
	seenLease := map[string]bool{}
	for _, row := range proj.Leases {
		if !row.Active {
			continue
		}
		seenLease[row.ID] = true
		got, ok := byLease[row.ID]
		if !ok {
			out = append(out, store.Finding{Kind: "lease_not_held", ObjectID: row.ID})
			continue
		}
		if got.State != row.State || int64(got.Term) != row.Term || got.Deadline != row.Deadline {
			out = append(out, store.Finding{Kind: "lease_mismatch", ObjectID: row.ID})
		}
	}
	for id := range byLease {
		if !seenLease[id] {
			out = append(out, store.Finding{Kind: "lease_missing", ObjectID: id})
		}
	}
	deadLease := map[string]int64{}
	deadOffer := map[string]bool{}
	for _, d := range snap.Deadlines {
		if d.Kind == "lease" {
			deadLease[d.ID] = d.At
		}
		if d.Kind == "offer" {
			deadOffer[d.ID] = true
		}
	}
	for _, row := range proj.Leases {
		if !row.Active {
			continue
		}
		at, ok := deadLease[row.ID]
		if !ok {
			out = append(out, store.Finding{Kind: "deadline_absent", ObjectID: row.ID})
			continue
		}
		if at != row.Deadline {
			out = append(out, store.Finding{Kind: "deadline_mismatch", ObjectID: row.ID})
		}
	}
	for _, row := range proj.Offers {
		switch row.State {
		case "OPEN":
			if !deadOffer[row.ID] {
				out = append(out, store.Finding{Kind: "offer_deadline_absent", ObjectID: row.ID})
			}
		case "CLOSED":
			if deadOffer[row.ID] {
				out = append(out, store.Finding{Kind: "offer_deadline_extra", ObjectID: row.ID})
			}
		}
	}
	if proj.HasPolicy {
		if !snap.HasPolicy || snap.Policy != proj.Policy.Version {
			out = append(out, store.Finding{Kind: "policy_version", ObjectID: strconv.FormatUint(proj.Policy.Version, 10)})
		}
	}
	return out
}

// Querier is the chain read used by Run.
type Querier interface {
	Providers(ctx context.Context, height int64) ([]chain.ProviderView, int64, error)
	Content(ctx context.Context, id string, version uint64, height int64) (chain.ContentView, bool, int64, error)
	ActiveLeases(ctx context.Context, providerID string, height int64) ([]chain.LeaseView, int64, error)
	Deadlines(ctx context.Context, height int64) ([]chain.DeadlineView, int64, error)
	PolicyVersion(ctx context.Context, height int64) (uint64, int64, error)
}

// Run reads the chain at height and records the comparison. It does not
// update a projection row.
func Run(ctx context.Context, q Querier, st *store.Store, proj store.Projection, height int64) (string, error) {
	snap, err := read(ctx, q, proj, height)
	if err != nil {
		if recErr := st.RecordRun(ctx, height, "blocked", nil); recErr != nil {
			return "", recErr
		}
		return "blocked", nil
	}
	findings := Compare(proj, snap)
	status := "ok"
	if len(findings) > 0 {
		status = "divergent"
	}
	if err := st.RecordRun(ctx, height, status, findings); err != nil {
		return "", err
	}
	return status, nil
}

func read(ctx context.Context, q Querier, proj store.Projection, height int64) (Snapshot, error) {
	snap := Snapshot{Height: height, Content: map[string]chain.ContentView{}, Missing: map[string]bool{}}
	providers, _, err := q.Providers(ctx, height)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Providers = providers
	for _, row := range proj.Content {
		view, found, _, err := q.Content(ctx, row.ID, row.Version, height)
		if err != nil {
			return Snapshot{}, err
		}
		key := row.ID + "/" + strconv.FormatUint(row.Version, 10)
		if !found {
			snap.Missing[key] = true
			continue
		}
		snap.Content[key] = view
	}
	seen := map[string]bool{}
	for _, row := range proj.Providers {
		if seen[row.ID] {
			continue
		}
		seen[row.ID] = true
		leases, _, err := q.ActiveLeases(ctx, row.ID, height)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Leases = append(snap.Leases, leases...)
	}
	for _, row := range proj.Leases {
		if seen[row.ProviderID] || row.ProviderID == "" {
			continue
		}
		seen[row.ProviderID] = true
		leases, _, err := q.ActiveLeases(ctx, row.ProviderID, height)
		if err != nil {
			return Snapshot{}, err
		}
		snap.Leases = append(snap.Leases, leases...)
	}
	deadlines, _, err := q.Deadlines(ctx, height)
	if err != nil {
		return Snapshot{}, err
	}
	snap.Deadlines = deadlines
	version, _, err := q.PolicyVersion(ctx, height)
	if err != nil {
		return Snapshot{}, fmt.Errorf("policy: %w", err)
	}
	snap.Policy = version
	snap.HasPolicy = true
	return snap, nil
}
