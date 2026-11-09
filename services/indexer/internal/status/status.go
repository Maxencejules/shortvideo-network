// Package status decides whether a read of the projection is stale.
//
// Commit age uses the database clock minus the chain header time. That age
// is a disclosure. It is not a reason to renew a lease or to approve a
// publication. The database remains a read model.
package status

import (
	"time"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/project"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/store"
)

// Trust is the disclosure every read carries.
type Trust struct {
	Model       string `json:"model"`
	Gateway     string `json:"gateway"`
	LightClient bool   `json:"light_client"`
	Note        string `json:"note"`
}

// TrustNote is the sentence the read API returns.
const TrustNote = "The configured read gateway is trusted for reads. Comparing two unauthenticated responses is not light-client verification. The database is a read model, not a replacement chain."

// View is the indexer status a hosting service reads.
type View struct {
	ChainID              string   `json:"chain_id"`
	IndexedHeight        int64    `json:"indexed_height"`
	CanonicalHeight      int64    `json:"canonical_height"`
	AppFloor             int64    `json:"app_floor"`
	CommitTime           string   `json:"commit_time,omitempty"`
	CommitAgeSeconds     float64  `json:"commit_age_seconds"`
	LastReconciliation   string   `json:"last_reconciliation,omitempty"`
	ReconciliationStatus string   `json:"reconciliation_status"`
	Stale                bool     `json:"stale"`
	StaleReasons         []string `json:"stale_reasons"`
	HistoryUnavailable   bool     `json:"history_unavailable"`
	DecoderVersion       int      `json:"decoder_version"`
	DatabaseRole         string   `json:"database_role"`
	Trust                Trust    `json:"trust"`
}

// Input is everything Evaluate needs. It does not contact a node.
type Input struct {
	ChainID       string
	Gateway       string
	IndexedHeight int64
	HaveIndex     bool
	Obs           store.Observation
	Run           store.LatestRun
	StaleAfter    time.Duration
	ObserverFresh time.Duration
}

// Evaluate returns the read status. A stopped chain becomes stale because
// the commit age grows. Evaluate does not change a lease or a content row.
func Evaluate(in Input) View {
	v := View{
		ChainID: in.ChainID, DecoderVersion: project.DecoderVersion, DatabaseRole: "read_model",
		Trust:                Trust{Model: "configured_read_gateway", Gateway: in.Gateway, LightClient: false, Note: TrustNote},
		ReconciliationStatus: "absent",
	}
	if in.HaveIndex {
		v.IndexedHeight = in.IndexedHeight
	}
	if in.Obs.Found {
		v.CanonicalHeight = in.Obs.CanonicalHeight
		v.AppFloor = in.Obs.AppFloor
		v.CommitTime = in.Obs.BlockTime.UTC().Format(time.RFC3339)
		age := in.Obs.CommitAge
		if age < 0 {
			age = 0
		}
		v.CommitAgeSeconds = age.Seconds()
		if in.Obs.CatchingUp {
			v.StaleReasons = append(v.StaleReasons, "catching_up")
		}
		if in.Obs.HistoryGap {
			v.HistoryUnavailable = true
			v.StaleReasons = append(v.StaleReasons, "history_unavailable")
		}
		if in.Obs.LastError != "" {
			v.StaleReasons = append(v.StaleReasons, in.Obs.LastError)
		}
		if in.Obs.ObserverAge > in.ObserverFresh {
			v.StaleReasons = append(v.StaleReasons, "observer_stale")
		}
		if in.HaveIndex && in.IndexedHeight < in.Obs.AppFloor {
			v.StaleReasons = append(v.StaleReasons, "behind")
		}
		if age > in.StaleAfter {
			v.StaleReasons = append(v.StaleReasons, "commit_age")
		}
	} else {
		v.StaleReasons = append(v.StaleReasons, "observer_absent")
	}
	if in.Run.Found {
		v.ReconciliationStatus = in.Run.Status
		v.LastReconciliation = in.Run.At.UTC().Format(time.RFC3339)
		if in.Run.Status == "divergent" {
			v.StaleReasons = append(v.StaleReasons, "divergent")
		}
		if in.Run.Status == "blocked" {
			v.StaleReasons = append(v.StaleReasons, "reconciliation_blocked")
		}
	} else {
		v.StaleReasons = append(v.StaleReasons, "reconciliation_absent")
	}
	v.Stale = len(v.StaleReasons) > 0
	return v
}
