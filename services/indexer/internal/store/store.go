// Package store is the PostgreSQL read model. Projection updates and the
// checkpoint move in one transaction. A crash between them leaves neither.
package store

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"time"

	"github.com/lib/pq"

	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/faultpoint"
	"github.com/Maxencejules/shortvideo-network/services/indexer/internal/project"
)

//go:embed migrations/*.pgsql
var migrationFS embed.FS

const (
	// OwnerRole applies migrations and rebuilds the projection schema.
	OwnerRole = "svn_indexer_owner"
	// RuntimeRole serves, backfills and reconciles. It has no DELETE and no DDL.
	RuntimeRole = "svn_indexer"
	// Database is the projection database dev-db creates.
	Database = "indexer"
	// Schema is the only schema rebuild drops.
	Schema = "indexer"

	migrationName = "indexer"
)

// rebuildDrop is the only destructive statement in this package. It names
// the projection schema and no other schema.
const rebuildDrop = `DROP SCHEMA IF EXISTS indexer CASCADE`

var (
	ErrMigration     = errors.New("store: migration")
	ErrChain         = errors.New("store: chain binding")
	ErrGap           = errors.New("store: missing height range")
	ErrConflict      = errors.New("store: block hash conflicts with the checkpoint")
	ErrInconsistent  = errors.New("store: event does not match indexed state")
	ErrAuthoritative = errors.New("store: refusing to rebuild; a table is not a projection")
	ErrRole          = errors.New("store: rebuild requires the owner role")
)

// Conn is how the pool reaches PostgreSQL. It is a Unix socket and SSL off.
type Conn struct {
	SocketDir       string
	Port            uint16
	Database        string
	User            string
	Password        string
	ApplicationName string
	ConnectTimeout  time.Duration
}

// Open opens a pool. It does not connect until the first query.
func Open(c Conn) (*sql.DB, error) {
	cfg := pq.Config{
		Host: c.SocketDir, Port: c.Port, Database: c.Database, User: c.User, Password: c.Password,
		ApplicationName: c.ApplicationName, SSLMode: pq.SSLModeDisable, ClientEncoding: "UTF8",
		Datestyle: "ISO, MDY", MinProtocolVersion: pq.ProtocolVersion30, MaxProtocolVersion: pq.ProtocolVersion30,
		ConnectTimeout: c.ConnectTimeout,
	}
	connector, err := pq.NewConnectorConfig(cfg)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

// Migrate applies the embedded schema as the owner and records its checksum.
func Migrate(ctx context.Context, db *sql.DB) error {
	body, err := migrationFS.ReadFile("migrations/0001_indexer.pgsql")
	if err != nil {
		return fmt.Errorf("%w: %v", ErrMigration, err)
	}
	sum := sha256.Sum256(body)
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var exists bool
	if err := tx.QueryRowContext(ctx, `
		SELECT EXISTS (
		    SELECT 1 FROM information_schema.tables
		    WHERE table_schema = 'indexer' AND table_name = 'schema_migrations'
		)`).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		if _, err := tx.ExecContext(ctx, string(body)); err != nil {
			return fmt.Errorf("%w: %v", ErrMigration, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO indexer.schema_migrations (version, name, sha256) VALUES (1, $1, $2)`,
			migrationName, sum[:]); err != nil {
			return fmt.Errorf("%w: %v", ErrMigration, err)
		}
		return tx.Commit()
	}
	var got []byte
	if err := tx.QueryRowContext(ctx, `SELECT sha256 FROM indexer.schema_migrations WHERE version = 1`).Scan(&got); err != nil {
		return fmt.Errorf("%w: %v", ErrMigration, err)
	}
	if len(got) != len(sum) || string(got) != string(sum[:]) {
		return fmt.Errorf("%w: checksum differs from this binary", ErrMigration)
	}
	return tx.Commit()
}

// Bind records the chain id. A second id is refused.
func Bind(ctx context.Context, db *sql.DB, chainID string) error {
	var got string
	err := db.QueryRowContext(ctx, `SELECT chain_id FROM indexer.chain_binding WHERE singleton`).Scan(&got)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = db.ExecContext(ctx,
			`INSERT INTO indexer.chain_binding (chain_id, decoder_version) VALUES ($1, $2)`,
			chainID, project.DecoderVersion)
		return err
	case err != nil:
		return err
	case got != chainID:
		return fmt.Errorf("%w: database %q configured %q", ErrChain, got, chainID)
	default:
		return nil
	}
}

// Store applies blocks and reads the projection.
type Store struct {
	db      *sql.DB
	chainID string
	// afterEffects runs inside the apply transaction, after the rows and
	// before the checkpoint. The default build uses it for the fault point.
	afterEffects func()
}

// New returns a store. afterEffects nil uses the fault point.
func New(db *sql.DB, chainID string) *Store {
	return &Store{db: db, chainID: chainID, afterEffects: func() { faultpoint.Hit(faultpoint.AfterEffectsBeforeCheckpoint) }}
}

// NextHeight is the next height to fetch. Zero rows means 1.
func (s *Store) NextHeight(ctx context.Context) (int64, error) {
	var height int64
	err := s.db.QueryRowContext(ctx, `SELECT height FROM indexer.checkpoint WHERE chain_id = $1`, s.chainID).Scan(&height)
	if errors.Is(err, sql.ErrNoRows) {
		return 1, nil
	}
	if err != nil {
		return 0, err
	}
	return height + 1, nil
}

// Apply writes one block's effects and its checkpoint in one transaction.
// A repeated block with the same hash is a no-op. A gap is ErrGap and writes
// nothing: the missing range is not an empty history. A different hash at a
// height already checkpointed is ErrConflict.
func (s *Store) Apply(ctx context.Context, block project.Block, effects []project.Effect) error {
	if block.ChainID != s.chainID {
		return fmt.Errorf("%w: block %q", ErrChain, block.ChainID)
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(71412)`); err != nil {
		return err
	}
	var bound string
	if err := tx.QueryRowContext(ctx, `SELECT chain_id FROM indexer.chain_binding WHERE singleton`).Scan(&bound); err != nil {
		return fmt.Errorf("%w: %v", ErrChain, err)
	}
	if bound != s.chainID {
		return fmt.Errorf("%w: database %q block %q", ErrChain, bound, s.chainID)
	}
	var have int64
	var hash []byte
	err = tx.QueryRowContext(ctx, `SELECT height, block_hash FROM indexer.checkpoint WHERE chain_id = $1`, s.chainID).Scan(&have, &hash)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		have = 0
	case err != nil:
		return err
	}
	if block.Height <= have {
		if string(hash) != string(block.Hash) {
			return fmt.Errorf("%w: height %d", ErrConflict, block.Height)
		}
		return nil
	}
	if block.Height != have+1 {
		return fmt.Errorf("%w: have %d got %d", ErrGap, have, block.Height)
	}
	for _, eff := range effects {
		if err := applyEffect(ctx, tx, s.chainID, eff); err != nil {
			return err
		}
	}
	if len(effects) > 0 && s.afterEffects != nil {
		s.afterEffects()
	}
	when := time.Unix(block.TimeUnix, 0).UTC()
	_, err = tx.ExecContext(ctx, `
		INSERT INTO indexer.checkpoint (chain_id, height, block_hash, app_hash, block_time)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (chain_id) DO UPDATE
		SET height = EXCLUDED.height, block_hash = EXCLUDED.block_hash,
		    app_hash = EXCLUDED.app_hash, block_time = EXCLUDED.block_time`,
		s.chainID, block.Height, block.Hash, block.AppHash, when)
	if err != nil {
		return err
	}
	return tx.Commit()
}

func applyEffect(ctx context.Context, tx *sql.Tx, chainID string, eff project.Effect) error {
	switch {
	case eff.Provider != nil:
		return applyProvider(ctx, tx, chainID, eff)
	case eff.Content != nil:
		return applyContent(ctx, tx, chainID, eff)
	case eff.Offer != nil:
		return applyOffer(ctx, tx, chainID, eff)
	case eff.Lease != nil:
		return applyLease(ctx, tx, chainID, eff)
	case eff.Policy != nil:
		return applyPolicy(ctx, tx, chainID, eff)
	case eff.Evidence != nil:
		return applyEvidence(ctx, tx, chainID, eff)
	default:
		return fmt.Errorf("%w: empty effect", ErrInconsistent)
	}
}

func note(ctx context.Context, tx *sql.Tx, chainID, entity, id string, src project.Source) error {
	_, err := tx.ExecContext(ctx, `
		INSERT INTO indexer.events
		    (chain_id, height, tx_hash, msg_index, event_index, event_type, entity, entity_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
		chainID, src.Height, src.TxHash, src.MsgIndex, src.EventIndex, src.Type, entity, id)
	return err
}

func applyProvider(ctx context.Context, tx *sql.Tx, chainID string, eff project.Effect) error {
	p := eff.Provider
	var state string
	err := tx.QueryRowContext(ctx, `SELECT state FROM indexer.providers WHERE chain_id = $1 AND provider_id = $2`, chainID, p.ID).Scan(&state)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if p.SetGeneration && p.KeyGeneration == 0 && p.State == "" {
			return fmt.Errorf("%w: provider %s", ErrInconsistent, p.ID)
		}
		gen := p.KeyGeneration
		if !p.SetGeneration {
			gen = 0
		}
		if _, err := tx.ExecContext(ctx, `
			INSERT INTO indexer.providers
			    (chain_id, provider_id, state, key_generation, source_height, source_tx, source_msg, source_event, source_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
			chainID, p.ID, p.State, gen, eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type); err != nil {
			return err
		}
	case err != nil:
		return err
	default:
		if p.SetGeneration {
			_, err = tx.ExecContext(ctx, `
				UPDATE indexer.providers
				SET state = $3, key_generation = $4, source_height = $5, source_tx = $6, source_msg = $7, source_event = $8, source_type = $9
				WHERE chain_id = $1 AND provider_id = $2`,
				chainID, p.ID, p.State, p.KeyGeneration, eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
		} else {
			_, err = tx.ExecContext(ctx, `
				UPDATE indexer.providers
				SET state = $3, source_height = $4, source_tx = $5, source_msg = $6, source_event = $7, source_type = $8
				WHERE chain_id = $1 AND provider_id = $2`,
				chainID, p.ID, p.State, eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
		}
		if err != nil {
			return err
		}
	}
	return note(ctx, tx, chainID, "provider", p.ID, eff.Source)
}

func applyContent(ctx context.Context, tx *sql.Tx, chainID string, eff project.Effect) error {
	c := eff.Content
	var manifest string
	var withdrawn bool
	err := tx.QueryRowContext(ctx, `
		SELECT manifest_sha256, withdrawn FROM indexer.content
		WHERE chain_id = $1 AND content_id = $2 AND content_version = $3`,
		chainID, c.ID, c.Version).Scan(&manifest, &withdrawn)
	id := fmt.Sprintf("%s/%d", c.ID, c.Version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if c.WithdrawOnly {
			return fmt.Errorf("%w: withdraw unknown content %s", ErrInconsistent, id)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO indexer.content
			    (chain_id, content_id, content_version, manifest_sha256, package_bytes, withdrawn, history_erased,
			     source_height, source_tx, source_msg, source_event, source_type)
			VALUES ($1, $2, $3, $4, $5, false, false, $6, $7, $8, $9, $10)`,
			chainID, c.ID, c.Version, c.Manifest, c.PackageBytes,
			eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	case err != nil:
		return err
	case c.WithdrawOnly:
		// The row stays. history_erased is recorded and does not delete it.
		_, err = tx.ExecContext(ctx, `
			UPDATE indexer.content
			SET withdrawn = true, history_erased = $4, source_height = $5, source_tx = $6, source_msg = $7, source_event = $8, source_type = $9
			WHERE chain_id = $1 AND content_id = $2 AND content_version = $3`,
			chainID, c.ID, c.Version, c.HistoryErased,
			eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	case manifest != c.Manifest:
		return fmt.Errorf("%w: content %s manifest changed", ErrInconsistent, id)
	default:
		return note(ctx, tx, chainID, "content", id, eff.Source)
	}
	if err != nil {
		return err
	}
	return note(ctx, tx, chainID, "content", id, eff.Source)
}

func applyOffer(ctx context.Context, tx *sql.Tx, chainID string, eff project.Effect) error {
	o := eff.Offer
	var state string
	err := tx.QueryRowContext(ctx, `SELECT state FROM indexer.offers WHERE chain_id = $1 AND offer_id = $2`, chainID, o.ID).Scan(&state)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if o.CloseOnly || o.AcceptOnly {
			return fmt.Errorf("%w: offer %s", ErrInconsistent, o.ID)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO indexer.offers
			    (chain_id, offer_id, provider_id, content_id, state, source_height, source_tx, source_msg, source_event, source_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			chainID, o.ID, o.ProviderID, o.ContentID, o.State,
			eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	case err != nil:
		return err
	case o.CloseOnly:
		_, err = tx.ExecContext(ctx, `
			UPDATE indexer.offers
			SET state = 'CLOSED', source_height = $3, source_tx = $4, source_msg = $5, source_event = $6, source_type = $7
			WHERE chain_id = $1 AND offer_id = $2`,
			chainID, o.ID, eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	default:
		return note(ctx, tx, chainID, "offer", o.ID, eff.Source)
	}
	if err != nil {
		return err
	}
	return note(ctx, tx, chainID, "offer", o.ID, eff.Source)
}

// closeUniqueOpenOffer records that an accepted lease consumed the one
// open offer for that provider and content. The accept event does not
// carry the offer id. More than one open offer is left untouched.
func closeUniqueOpenOffer(ctx context.Context, tx *sql.Tx, chainID string, l *project.Lease, src project.Source) error {
	rows, err := tx.QueryContext(ctx, `
		SELECT offer_id FROM indexer.offers
		WHERE chain_id = $1 AND provider_id = $2 AND content_id = $3 AND state = 'OPEN'
		ORDER BY offer_id`, chainID, l.ProviderID, l.ContentID)
	if err != nil {
		return err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(ids) != 1 {
		return nil
	}
	_, err = tx.ExecContext(ctx, `
		UPDATE indexer.offers
		SET state = 'CLOSED', source_height = $3, source_tx = $4, source_msg = $5, source_event = $6, source_type = $7
		WHERE chain_id = $1 AND offer_id = $2`,
		chainID, ids[0], src.Height, src.TxHash, src.MsgIndex, src.EventIndex, src.Type)
	return err
}

func applyLease(ctx context.Context, tx *sql.Tx, chainID string, eff project.Effect) error {
	l := eff.Lease
	var term int64
	err := tx.QueryRowContext(ctx, `SELECT term_version FROM indexer.leases WHERE chain_id = $1 AND lease_id = $2`, chainID, l.ID).Scan(&term)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if l.RenewOnly || l.EndOnly {
			return fmt.Errorf("%w: lease %s", ErrInconsistent, l.ID)
		}
		_, err = tx.ExecContext(ctx, `
			INSERT INTO indexer.leases
			    (chain_id, lease_id, provider_id, content_id, content_version, package_bytes, state, term_version,
			     deadline_unix, released_bytes, active, source_height, source_tx, source_msg, source_event, source_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, '', $10, $11, $12, $13, $14, $15)`,
			chainID, l.ID, l.ProviderID, l.ContentID, l.Version, l.PackageBytes, l.State, l.TermVersion, l.Deadline, l.Active,
			eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
		if err == nil {
			err = closeUniqueOpenOffer(ctx, tx, chainID, l, eff.Source)
		}
	case err != nil:
		return err
	case l.RenewOnly:
		_, err = tx.ExecContext(ctx, `
			UPDATE indexer.leases
			SET state = 'RENEWED', term_version = $3, deadline_unix = $4, active = true,
			    source_height = $5, source_tx = $6, source_msg = $7, source_event = $8, source_type = $9
			WHERE chain_id = $1 AND lease_id = $2`,
			chainID, l.ID, l.TermVersion, l.Deadline,
			eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	case l.EndOnly:
		_, err = tx.ExecContext(ctx, `
			UPDATE indexer.leases
			SET state = $3, released_bytes = $4, active = false,
			    source_height = $5, source_tx = $6, source_msg = $7, source_event = $8, source_type = $9
			WHERE chain_id = $1 AND lease_id = $2`,
			chainID, l.ID, l.State, l.ReleasedBytes,
			eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	default:
		return note(ctx, tx, chainID, "lease", l.ID, eff.Source)
	}
	if err != nil {
		return err
	}
	return note(ctx, tx, chainID, "lease", l.ID, eff.Source)
}

func applyPolicy(ctx context.Context, tx *sql.Tx, chainID string, eff project.Effect) error {
	p := eff.Policy
	var version int64
	err := tx.QueryRowContext(ctx, `SELECT policy_version FROM indexer.policy WHERE chain_id = $1`, chainID).Scan(&version)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		key := p.ParameterKey
		_, err = tx.ExecContext(ctx, `
			INSERT INTO indexer.policy
			    (chain_id, policy_version, parameter_key, source_height, source_tx, source_msg, source_event, source_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			chainID, p.Version, key, eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	case err != nil:
		return err
	case p.KeepKey:
		_, err = tx.ExecContext(ctx, `
			UPDATE indexer.policy
			SET policy_version = $2, source_height = $3, source_tx = $4, source_msg = $5, source_event = $6, source_type = $7
			WHERE chain_id = $1`,
			chainID, p.Version, eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	default:
		_, err = tx.ExecContext(ctx, `
			UPDATE indexer.policy
			SET policy_version = $2, parameter_key = $3, source_height = $4, source_tx = $5, source_msg = $6, source_event = $7, source_type = $8
			WHERE chain_id = $1`,
			chainID, p.Version, p.ParameterKey, eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	}
	if err != nil {
		return err
	}
	return note(ctx, tx, chainID, "policy", chainID, eff.Source)
}

func applyEvidence(ctx context.Context, tx *sql.Tx, chainID string, eff project.Effect) error {
	e := eff.Evidence
	var kind string
	err := tx.QueryRowContext(ctx, `SELECT kind FROM indexer.evidence WHERE chain_id = $1 AND evidence_id = $2`, chainID, e.ID).Scan(&kind)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		_, err = tx.ExecContext(ctx, `
			INSERT INTO indexer.evidence
			    (chain_id, evidence_id, batch_id, kind, attestation, source_height, source_tx, source_msg, source_event, source_type)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
			chainID, e.ID, e.BatchID, e.Kind, e.Attestation,
			eff.Source.Height, eff.Source.TxHash, eff.Source.MsgIndex, eff.Source.EventIndex, eff.Source.Type)
	case err != nil:
		return err
	case kind != e.Kind:
		return fmt.Errorf("%w: evidence %s", ErrInconsistent, e.ID)
	default:
		return note(ctx, tx, chainID, "evidence", e.ID, eff.Source)
	}
	if err != nil {
		return err
	}
	return note(ctx, tx, chainID, "evidence", e.ID, eff.Source)
}
