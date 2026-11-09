package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// Checkpoint is the last committed projection height.
type Checkpoint struct {
	Height  int64
	Hash    []byte
	AppHash []byte
	Time    time.Time
	Found   bool
}

// Checkpoint reads the checkpoint. Found is false when no block has been applied.
func (s *Store) Checkpoint(ctx context.Context) (Checkpoint, error) {
	var cp Checkpoint
	err := s.db.QueryRowContext(ctx, `
		SELECT height, block_hash, app_hash, block_time FROM indexer.checkpoint WHERE chain_id = $1`, s.chainID).
		Scan(&cp.Height, &cp.Hash, &cp.AppHash, &cp.Time)
	if err == sql.ErrNoRows {
		return Checkpoint{}, nil
	}
	if err != nil {
		return Checkpoint{}, err
	}
	cp.Found = true
	return cp, nil
}

// ContentCount is the number of content rows.
func (s *Store) ContentCount(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT count(*) FROM indexer.content WHERE chain_id = $1`, s.chainID).Scan(&n)
	return n, err
}

// LeaseTerm is the term version of one lease. found is false when the row is absent.
func (s *Store) LeaseTerm(ctx context.Context, id string) (int64, bool, error) {
	var term int64
	err := s.db.QueryRowContext(ctx, `SELECT term_version FROM indexer.leases WHERE chain_id = $1 AND lease_id = $2`, s.chainID, id).Scan(&term)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	return term, err == nil, err
}

// Canonical is the normalized projection. Rows are ordered, and the text has
// no local-clock columns, so a rebuild from the same chain matches.
func (s *Store) Canonical(ctx context.Context) (string, error) {
	var b strings.Builder
	cp, err := s.Checkpoint(ctx)
	if err != nil {
		return "", err
	}
	fmt.Fprintf(&b, "checkpoint %d %x\n", cp.Height, cp.Hash)
	queries := []struct {
		name string
		sql  string
	}{
		{"provider", `SELECT provider_id || ' ' || state || ' ' || key_generation::text FROM indexer.providers WHERE chain_id = $1 ORDER BY provider_id`},
		{"content", `SELECT content_id || ' ' || content_version::text || ' ' || manifest_sha256 || ' ' || package_bytes || ' ' || withdrawn::text || ' ' || history_erased::text FROM indexer.content WHERE chain_id = $1 ORDER BY content_id, content_version`},
		{"offer", `SELECT offer_id || ' ' || provider_id || ' ' || content_id || ' ' || state FROM indexer.offers WHERE chain_id = $1 ORDER BY offer_id`},
		{"lease", `SELECT lease_id || ' ' || provider_id || ' ' || content_id || ' ' || content_version::text || ' ' || state || ' ' || term_version::text || ' ' || deadline_unix::text || ' ' || active::text FROM indexer.leases WHERE chain_id = $1 ORDER BY lease_id`},
		{"policy", `SELECT policy_version::text || ' ' || parameter_key FROM indexer.policy WHERE chain_id = $1`},
		{"evidence", `SELECT evidence_id || ' ' || batch_id || ' ' || kind || ' ' || attestation::text FROM indexer.evidence WHERE chain_id = $1 ORDER BY evidence_id`},
		{"source", `SELECT entity || ' ' || entity_id || ' ' || height::text || ' ' || encode(tx_hash, 'hex') || ' ' || msg_index::text || ' ' || event_index::text || ' ' || event_type FROM indexer.events WHERE chain_id = $1 ORDER BY height, encode(tx_hash, 'hex'), msg_index, event_index`},
	}
	for _, q := range queries {
		rows, err := s.db.QueryContext(ctx, q.sql, s.chainID)
		if err != nil {
			return "", err
		}
		for rows.Next() {
			var line string
			if err := rows.Scan(&line); err != nil {
				rows.Close()
				return "", err
			}
			fmt.Fprintf(&b, "%s %s\n", q.name, line)
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return "", err
		}
		rows.Close()
	}
	return b.String(), nil
}

// Observe records what the node last reported. It does not change a projection row.
func (s *Store) Observe(ctx context.Context, height, floor int64, blockTime time.Time, catchingUp, historyGap bool, lastError string) error {
	if len(lastError) > 80 {
		lastError = lastError[:80]
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO indexer.observer
		    (chain_id, canonical_height, app_floor, block_time, catching_up, history_gap, last_error)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (chain_id) DO UPDATE SET
		    canonical_height = EXCLUDED.canonical_height,
		    app_floor = EXCLUDED.app_floor,
		    block_time = EXCLUDED.block_time,
		    observed_at = clock_timestamp(),
		    catching_up = EXCLUDED.catching_up,
		    history_gap = EXCLUDED.history_gap,
		    last_error = EXCLUDED.last_error`,
		s.chainID, height, floor, blockTime.UTC(), catchingUp, historyGap, lastError)
	return err
}

// Observation is the last node report plus the database clock ages.
type Observation struct {
	Found           bool
	CanonicalHeight int64
	AppFloor        int64
	BlockTime       time.Time
	CommitAge       time.Duration
	ObserverAge     time.Duration
	CatchingUp      bool
	HistoryGap      bool
	LastError       string
}

// Observation reads the observer row. Ages use the database clock.
func (s *Store) Observation(ctx context.Context) (Observation, error) {
	var o Observation
	var commitSec, observerSec float64
	err := s.db.QueryRowContext(ctx, `
		SELECT canonical_height, app_floor, block_time, catching_up, history_gap, last_error,
		       EXTRACT(EPOCH FROM (clock_timestamp() - block_time)),
		       EXTRACT(EPOCH FROM (clock_timestamp() - observed_at))
		FROM indexer.observer WHERE chain_id = $1`, s.chainID).
		Scan(&o.CanonicalHeight, &o.AppFloor, &o.BlockTime, &o.CatchingUp, &o.HistoryGap, &o.LastError, &commitSec, &observerSec)
	if err == sql.ErrNoRows {
		return Observation{}, nil
	}
	if err != nil {
		return Observation{}, err
	}
	o.Found = true
	o.CommitAge = time.Duration(commitSec * float64(time.Second))
	o.ObserverAge = time.Duration(observerSec * float64(time.Second))
	return o, nil
}

// Projection is the set reconciliation compares. It is a copy of the rows.
type Projection struct {
	Providers []ProviderRow
	Content   []ContentRow
	Offers    []OfferRow
	Leases    []LeaseRow
	Policy    PolicyRow
	HasPolicy bool
}

// ProviderRow is one provider projection.
type ProviderRow struct {
	ID            string
	State         string
	KeyGeneration int64
}

// ContentRow is one content projection.
type ContentRow struct {
	ID            string
	Version       uint64
	Manifest      string
	PackageBytes  string
	Withdrawn     bool
	HistoryErased bool
}

// OfferRow is one offer projection.
type OfferRow struct {
	ID         string
	ProviderID string
	ContentID  string
	State      string
}

// LeaseRow is one lease projection.
type LeaseRow struct {
	ID         string
	ProviderID string
	ContentID  string
	Version    uint64
	State      string
	Term       int64
	Deadline   int64
	Active     bool
}

// PolicyRow is the policy projection.
type PolicyRow struct {
	Version      uint64
	ParameterKey string
}

// Load reads the projection rows reconciliation compares.
func (s *Store) Load(ctx context.Context) (Projection, error) {
	var p Projection
	if err := s.collect(ctx, `SELECT provider_id, state, key_generation FROM indexer.providers WHERE chain_id = $1 ORDER BY provider_id`, func(rows *sql.Rows) error {
		var row ProviderRow
		if err := rows.Scan(&row.ID, &row.State, &row.KeyGeneration); err != nil {
			return err
		}
		p.Providers = append(p.Providers, row)
		return nil
	}); err != nil {
		return Projection{}, err
	}
	if err := s.collect(ctx, `SELECT content_id, content_version, manifest_sha256, package_bytes, withdrawn, history_erased FROM indexer.content WHERE chain_id = $1 ORDER BY content_id, content_version`, func(rows *sql.Rows) error {
		var row ContentRow
		if err := rows.Scan(&row.ID, &row.Version, &row.Manifest, &row.PackageBytes, &row.Withdrawn, &row.HistoryErased); err != nil {
			return err
		}
		p.Content = append(p.Content, row)
		return nil
	}); err != nil {
		return Projection{}, err
	}
	if err := s.collect(ctx, `SELECT offer_id, provider_id, content_id, state FROM indexer.offers WHERE chain_id = $1 ORDER BY offer_id`, func(rows *sql.Rows) error {
		var row OfferRow
		if err := rows.Scan(&row.ID, &row.ProviderID, &row.ContentID, &row.State); err != nil {
			return err
		}
		p.Offers = append(p.Offers, row)
		return nil
	}); err != nil {
		return Projection{}, err
	}
	if err := s.collect(ctx, `SELECT lease_id, provider_id, content_id, content_version, state, term_version, deadline_unix, active FROM indexer.leases WHERE chain_id = $1 ORDER BY lease_id`, func(rows *sql.Rows) error {
		var row LeaseRow
		if err := rows.Scan(&row.ID, &row.ProviderID, &row.ContentID, &row.Version, &row.State, &row.Term, &row.Deadline, &row.Active); err != nil {
			return err
		}
		p.Leases = append(p.Leases, row)
		return nil
	}); err != nil {
		return Projection{}, err
	}
	err := s.db.QueryRowContext(ctx, `SELECT policy_version, parameter_key FROM indexer.policy WHERE chain_id = $1`, s.chainID).Scan(&p.Policy.Version, &p.Policy.ParameterKey)
	if err == nil {
		p.HasPolicy = true
	} else if err != sql.ErrNoRows {
		return Projection{}, err
	}
	return p, nil
}

func (s *Store) collect(ctx context.Context, query string, scan func(*sql.Rows) error) error {
	rows, err := s.db.QueryContext(ctx, query, s.chainID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		if err := scan(rows); err != nil {
			return err
		}
	}
	return rows.Err()
}

// Finding is one reconciliation mismatch. It does not by itself change a row.
type Finding struct {
	Kind     string
	ObjectID string
}

// RecordRun stores one reconciliation result. It does not update providers,
// content, leases, deadlines or policy.
func (s *Store) RecordRun(ctx context.Context, queryHeight int64, status string, findings []Finding) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var runID int64
	if err := tx.QueryRowContext(ctx, `
		INSERT INTO indexer.reconcile_runs (chain_id, query_height, finding_count, status)
		VALUES ($1, $2, $3, $4) RETURNING run_id`,
		s.chainID, queryHeight, len(findings), status).Scan(&runID); err != nil {
		return err
	}
	for _, f := range findings {
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO indexer.findings (run_id, kind, object_id, query_height) VALUES ($1, $2, $3, $4)`,
			runID, f.Kind, f.ObjectID, queryHeight); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// LatestRun is the newest reconciliation result.
type LatestRun struct {
	Found        bool
	At           time.Time
	QueryHeight  int64
	FindingCount int
	Status       string
	Findings     []Finding
}

// LatestRun reads the newest reconciliation. An absent run is not success.
func (s *Store) LatestRun(ctx context.Context) (LatestRun, error) {
	var run LatestRun
	var id int64
	err := s.db.QueryRowContext(ctx, `
		SELECT run_id, at, query_height, finding_count, status
		FROM indexer.reconcile_runs WHERE chain_id = $1 ORDER BY run_id DESC LIMIT 1`, s.chainID).
		Scan(&id, &run.At, &run.QueryHeight, &run.FindingCount, &run.Status)
	if err == sql.ErrNoRows {
		return LatestRun{}, nil
	}
	if err != nil {
		return LatestRun{}, err
	}
	run.Found = true
	rows, err := s.db.QueryContext(ctx, `SELECT kind, object_id FROM indexer.findings WHERE run_id = $1 ORDER BY kind, object_id`, id)
	if err != nil {
		return LatestRun{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var f Finding
		if err := rows.Scan(&f.Kind, &f.ObjectID); err != nil {
			return LatestRun{}, err
		}
		run.Findings = append(run.Findings, f)
	}
	return run, rows.Err()
}

// HasPublicationApproval reports whether the schema has a publication approval column.
// The projection schema does not.
func (s *Store) HasPublicationApproval(ctx context.Context) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM information_schema.columns
		WHERE table_schema = 'indexer' AND column_name = 'publication_approved'`).Scan(&n)
	return n > 0, err
}
