package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/Maxencejules/shortvideo-network/services/api/internal/account"
	"github.com/lib/pq"
)

const schemaSQL = `
CREATE SCHEMA IF NOT EXISTS api;

CREATE TABLE IF NOT EXISTS api.quotas (
    account_id text PRIMARY KEY,
    input_allowance bigint NOT NULL,
    output_allowance bigint NOT NULL,
    input_reserved bigint NOT NULL DEFAULT 0,
    output_reserved bigint NOT NULL DEFAULT 0,
    CHECK (input_allowance >= 0),
    CHECK (output_allowance >= 0),
    CHECK (input_reserved >= 0 AND input_reserved <= input_allowance),
    CHECK (output_reserved >= 0 AND output_reserved <= output_allowance)
);

CREATE TABLE IF NOT EXISTS api.uploads (
    upload_id text PRIMARY KEY,
    owner_account_id text NOT NULL,
    idempotency_key text NOT NULL,
    request_hash text NOT NULL,
    content_id text NOT NULL,
    declared_bytes bigint NOT NULL,
    output_reservation bigint NOT NULL,
    received_bytes bigint NOT NULL DEFAULT 0,
    status text NOT NULL,
    object_name text NOT NULL,
    sha256 text,
    duration_seconds double precision,
    container text,
    reservation_released boolean NOT NULL DEFAULT false,
    transfer_expires_at timestamptz NOT NULL,
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    UNIQUE (owner_account_id, idempotency_key),
    CHECK (declared_bytes > 0),
    CHECK (output_reservation > 0),
    CHECK (received_bytes >= 0 AND received_bytes <= declared_bytes),
    CHECK (status IN (
        'RESERVED', 'RECEIVING', 'RECEIVED', 'PROBING',
        'PROCESSING_QUEUED', 'REJECTED', 'EXPIRED', 'CANCELLED'
    ))
);

CREATE TABLE IF NOT EXISTS api.transfer_grants (
    token_hash text PRIMARY KEY,
    upload_id text NOT NULL REFERENCES api.uploads (upload_id)
);

CREATE TABLE IF NOT EXISTS api.processing_jobs (
    job_id text PRIMARY KEY,
    upload_id text NOT NULL UNIQUE,
    idempotency_key text NOT NULL UNIQUE,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (status = 'QUEUED')
);

CREATE TABLE IF NOT EXISTS api.creator_intents (
    upload_id text PRIMARY KEY REFERENCES api.uploads (upload_id),
    account_id text NOT NULL,
    content_id text NOT NULL,
    input_sha256 text NOT NULL,
    input_bytes bigint NOT NULL,
    output_budget_bytes bigint NOT NULL,
    schema_version integer NOT NULL
);

CREATE TABLE IF NOT EXISTS api.chain_outbox (
    upload_id text PRIMARY KEY REFERENCES api.uploads (upload_id),
    payload jsonb NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (status = 'HELD')
);

CREATE TABLE IF NOT EXISTS api.rate_windows (
    account_id text NOT NULL,
    window_start timestamptz NOT NULL,
    hits integer NOT NULL,
    PRIMARY KEY (account_id, window_start),
    CHECK (hits > 0)
);
`

// Open opens a PostgreSQL pool. The password is taken from cfg and is not logged.
func Open(cfg pq.Config) (*sql.DB, error) {
	if cfg.Host == "" || cfg.User == "" || cfg.Database == "" || cfg.Password == "" {
		return nil, errors.New("database configuration is incomplete")
	}
	cfg.ApplicationName = "svn-api"
	cfg.SSLMode = pq.SSLModeDisable
	cfg.ClientEncoding = "UTF8"
	cfg.Datestyle = "ISO, MDY"
	cfg.MinProtocolVersion = pq.ProtocolVersion30
	cfg.MaxProtocolVersion = pq.ProtocolVersion30
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 10 * time.Second
	}
	connector, err := pq.NewConnectorConfig(cfg)
	if err != nil {
		return nil, err
	}
	db := sql.OpenDB(connector)
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	return db, nil
}

// Migrate creates the upload schema and the account tables. It does not drop existing tables.
func Migrate(ctx context.Context, db *sql.DB) error {
	if _, err := db.ExecContext(ctx, schemaSQL); err != nil {
		return err
	}
	return account.Migrate(ctx, db)
}

// Store is the upload, quota, and job database.
type Store struct {
	db *sql.DB
}

func NewStore(db *sql.DB) *Store { return &Store{db: db} }

type uploadRow struct {
	UploadID          string
	Owner             string
	Status            string
	DeclaredBytes     int64
	ReceivedBytes     int64
	OutputReservation int64
	ObjectName        string
	ContentID         string
	SHA256            string
	Duration          sql.NullFloat64
	Container         string
	Expires           time.Time
	Released          bool
	JobID             string
	JobStatus         string
}

type intentInput struct {
	UploadID   string
	Owner      string
	Key        string
	Hash       string
	ContentID  string
	ObjectName string
	Declared   int64
	Output     int64
	Expires    time.Time
	Now        time.Time
}

func (s *Store) withTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		return errors.Join(err, tx.Rollback())
	}
	return tx.Commit()
}

func lockQuota(ctx context.Context, tx *sql.Tx, account string) (inputAllow, outputAllow, inputRes, outputRes int64, err error) {
	err = tx.QueryRowContext(ctx, `
		SELECT input_allowance, output_allowance, input_reserved, output_reserved
		FROM api.quotas WHERE account_id = $1 FOR UPDATE`, account).Scan(
		&inputAllow, &outputAllow, &inputRes, &outputRes)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, 0, 0, errNoAllowance
	}
	return inputAllow, outputAllow, inputRes, outputRes, err
}

// EnsureAccount inserts an allowance row once. It does not raise a reservation
// and it does not shrink an existing allowance.
func (s *Store) EnsureAccount(ctx context.Context, account string, inputAllowance, outputAllowance int64) error {
	if !accountRE.MatchString(account) {
		return errors.New("account id is not an identifier")
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO api.quotas (account_id, input_allowance, output_allowance)
		VALUES ($1, $2, $3)
		ON CONFLICT (account_id) DO NOTHING`, account, inputAllowance, outputAllowance)
	return err
}

// Hit records one intent against the account window and returns the count in that window.
func (s *Store) Hit(ctx context.Context, account string, window time.Time) (int, error) {
	var hits int
	err := s.db.QueryRowContext(ctx, `
		INSERT INTO api.rate_windows AS w (account_id, window_start, hits)
		VALUES ($1, $2, 1)
		ON CONFLICT (account_id, window_start)
		DO UPDATE SET hits = w.hits + 1
		RETURNING hits`, account, window.UTC()).Scan(&hits)
	return hits, err
}

// CreateIntent reserves input and output bytes once, or returns the existing row
// when the idempotency key repeats the same request hash.
func (s *Store) CreateIntent(ctx context.Context, in intentInput) (uploadRow, bool, error) {
	var row uploadRow
	created := false
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		inputAllow, outputAllow, inputRes, outputRes, err := lockQuota(ctx, tx, in.Owner)
		if err != nil {
			return err
		}
		err = tx.QueryRowContext(ctx, `
			SELECT upload_id FROM api.uploads
			WHERE owner_account_id = $1 AND idempotency_key = $2
			FOR UPDATE`, in.Owner, in.Key).Scan(&row.UploadID)
		if err == nil {
			loaded, loadErr := loadOwned(ctx, tx, row.UploadID, in.Owner)
			if loadErr != nil {
				return loadErr
			}
			if requestHash(in.Key, loaded.DeclaredBytes) != in.Hash {
				return errConflict
			}
			row = loaded
			return nil
		}
		if !errors.Is(err, sql.ErrNoRows) {
			return err
		}
		if inputRes > inputAllow || in.Declared > inputAllow-inputRes {
			return errInputQuota
		}
		if outputRes > outputAllow || in.Output > outputAllow-outputRes {
			return errOutputQuota
		}
		if _, err = tx.ExecContext(ctx, `
			UPDATE api.quotas
			SET input_reserved = input_reserved + $2, output_reserved = output_reserved + $3
			WHERE account_id = $1`, in.Owner, in.Declared, in.Output); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO api.uploads (
				upload_id, owner_account_id, idempotency_key, request_hash, content_id,
				declared_bytes, output_reservation, status, object_name,
				reservation_released, transfer_expires_at, created_at, updated_at
			) VALUES (
				$1, $2, $3, $4, $5,
				$6, $7, 'RESERVED', $8,
				false, $9, $10, $10
			)`, in.UploadID, in.Owner, in.Key, in.Hash, in.ContentID,
			in.Declared, in.Output, in.ObjectName, in.Expires, in.Now); err != nil {
			return err
		}
		loaded, err := loadOwned(ctx, tx, in.UploadID, in.Owner)
		if err != nil {
			return err
		}
		row = loaded
		created = true
		return nil
	})
	return row, created, err
}

func requestHash(key string, declared int64) string {
	sum := sha256.Sum256([]byte(key + "\n" + strconv.FormatInt(declared, 10)))
	return hex.EncodeToString(sum[:])
}

// Grant stores the hash of a transfer token for an upload the owner still holds.
// The bool is false when the upload can no longer accept bytes.
func (s *Store) Grant(ctx context.Context, uploadID, owner, tokenHash string, now time.Time) (bool, error) {
	granted := false
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		var status string
		var expires time.Time
		err := tx.QueryRowContext(ctx, `
			SELECT status, transfer_expires_at FROM api.uploads
			WHERE upload_id = $1 AND owner_account_id = $2 FOR UPDATE`, uploadID, owner).Scan(&status, &expires)
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		if status != "RESERVED" && status != "RECEIVING" && status != "RECEIVED" {
			return nil
		}
		if !expires.After(now) {
			return nil
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO api.transfer_grants (token_hash, upload_id) VALUES ($1, $2)`, tokenHash, uploadID); err != nil {
			return err
		}
		granted = true
		return nil
	})
	return granted, err
}

// ByTransfer loads the upload bound to a transfer-token hash.
func (s *Store) ByTransfer(ctx context.Context, tokenHash string) (uploadRow, error) {
	var id, owner string
	err := s.db.QueryRowContext(ctx, `
		SELECT u.upload_id, u.owner_account_id
		FROM api.transfer_grants g
		JOIN api.uploads u ON u.upload_id = g.upload_id
		WHERE g.token_hash = $1`, tokenHash).Scan(&id, &owner)
	if errors.Is(err, sql.ErrNoRows) {
		return uploadRow{}, errUnauthenticated
	}
	if err != nil {
		return uploadRow{}, err
	}
	return s.Owned(ctx, id, owner)
}

// Owned loads one upload for its owner. Another account gets the not-found error.
func (s *Store) Owned(ctx context.Context, uploadID, owner string) (uploadRow, error) {
	return loadOwned(ctx, s.db, uploadID, owner)
}

type rowQuery interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

func loadOwned(ctx context.Context, q rowQuery, uploadID, owner string) (uploadRow, error) {
	var row uploadRow
	err := q.QueryRowContext(ctx, `
		SELECT u.upload_id, u.owner_account_id, u.status, u.declared_bytes, u.received_bytes,
			u.output_reservation, u.object_name, u.content_id, COALESCE(u.sha256, ''),
			u.duration_seconds, COALESCE(u.container, ''), u.transfer_expires_at,
			u.reservation_released, COALESCE(j.job_id, ''), COALESCE(j.status, '')
		FROM api.uploads u
		LEFT JOIN api.processing_jobs j ON j.upload_id = u.upload_id
		WHERE u.upload_id = $1 AND u.owner_account_id = $2`, uploadID, owner).Scan(
		&row.UploadID, &row.Owner, &row.Status, &row.DeclaredBytes, &row.ReceivedBytes,
		&row.OutputReservation, &row.ObjectName, &row.ContentID, &row.SHA256,
		&row.Duration, &row.Container, &row.Expires, &row.Released, &row.JobID, &row.JobStatus)
	if errors.Is(err, sql.ErrNoRows) {
		return uploadRow{}, errNotFound
	}
	return row, err
}

// SaveReceive records a full or partial body. It does not change the reservation.
func (s *Store) SaveReceive(ctx context.Context, uploadID, owner, status, sum string, received int64, now time.Time) error {
	var sha any
	if sum != "" {
		sha = sum
	}
	res, err := s.db.ExecContext(ctx, `
		UPDATE api.uploads
		SET received_bytes = $3, sha256 = $4, status = $5, updated_at = $6
		WHERE upload_id = $1 AND owner_account_id = $2
			AND reservation_released = false
			AND status IN ('RESERVED', 'RECEIVING', 'RECEIVED')
			AND transfer_expires_at > $6`, uploadID, owner, received, sha, status, now)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return errState
	}
	return nil
}

// BeginProbe moves RECEIVED to PROBING when the digest matches.
// errDone means a job already exists for this digest.
func (s *Store) BeginProbe(ctx context.Context, uploadID, owner, digest string, now time.Time) (uploadRow, error) {
	var row uploadRow
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		if _, _, _, _, err := lockQuota(ctx, tx, owner); err != nil {
			return err
		}
		var locked string
		err := tx.QueryRowContext(ctx, `
			SELECT upload_id FROM api.uploads
			WHERE upload_id = $1 AND owner_account_id = $2 FOR UPDATE`, uploadID, owner).Scan(&locked)
		if errors.Is(err, sql.ErrNoRows) {
			return errNotFound
		}
		if err != nil {
			return err
		}
		loaded, err := loadOwned(ctx, tx, uploadID, owner)
		if err != nil {
			return err
		}
		if !loaded.Expires.After(now) && loaded.Status != "PROCESSING_QUEUED" {
			return errExpired
		}
		switch loaded.Status {
		case "PROCESSING_QUEUED":
			if loaded.SHA256 != digest {
				return errDigest
			}
			row = loaded
			return errDone
		case "PROBING":
			return errBusy
		case "RECEIVED":
			if loaded.SHA256 != digest {
				return errDigest
			}
		default:
			return errState
		}
		res, err := tx.ExecContext(ctx, `
			UPDATE api.uploads SET status = 'PROBING', updated_at = $3
			WHERE upload_id = $1 AND owner_account_id = $2 AND status = 'RECEIVED'`,
			uploadID, owner, now)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n != 1 {
			return errState
		}
		loaded.Status = "PROBING"
		row = loaded
		return nil
	})
	return row, err
}

type intentRecord struct {
	AccountID    string `json:"account_id"`
	ContentID    string `json:"content_id"`
	InputSHA256  string `json:"input_sha256"`
	InputBytes   int64  `json:"input_bytes"`
	OutputBudget int64  `json:"output_budget_bytes"`
	Schema       int    `json:"schema_version"`
}

// FinishProbe queues one processing job and one held chain record.
func (s *Store) FinishProbe(ctx context.Context, row uploadRow, probe Probe, now time.Time) (uploadRow, error) {
	payload, err := json.Marshal(intentRecord{
		AccountID:    row.Owner,
		ContentID:    row.ContentID,
		InputSHA256:  row.SHA256,
		InputBytes:   row.DeclaredBytes,
		OutputBudget: row.OutputReservation,
		Schema:       1,
	})
	if err != nil {
		return uploadRow{}, err
	}
	jobID, err := newID("j")
	if err != nil {
		return uploadRow{}, err
	}
	var out uploadRow
	err = s.withTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `
			UPDATE api.uploads
			SET status = 'PROCESSING_QUEUED', duration_seconds = $3, container = $4, updated_at = $5
			WHERE upload_id = $1 AND owner_account_id = $2 AND status = 'PROBING'`,
			row.UploadID, row.Owner, probe.Duration.Seconds(), probe.Format, now)
		if err != nil {
			return err
		}
		n, err := res.RowsAffected()
		if err != nil {
			return err
		}
		if n == 0 {
			loaded, loadErr := loadOwned(ctx, tx, row.UploadID, row.Owner)
			if loadErr != nil {
				return loadErr
			}
			if loaded.Status == "PROCESSING_QUEUED" {
				out = loaded
				return nil
			}
			return errState
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO api.processing_jobs (job_id, upload_id, idempotency_key, status, created_at)
			VALUES ($1, $2, $3, 'QUEUED', $4)`,
			jobID, row.UploadID, "job-"+row.UploadID, now); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO api.creator_intents (
				upload_id, account_id, content_id, input_sha256, input_bytes, output_budget_bytes, schema_version
			) VALUES ($1, $2, $3, $4, $5, $6, 1)`,
			row.UploadID, row.Owner, row.ContentID, row.SHA256, row.DeclaredBytes, row.OutputReservation); err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
			INSERT INTO api.chain_outbox (upload_id, payload, status, created_at)
			VALUES ($1, $2::jsonb, 'HELD', $3)`, row.UploadID, string(payload), now); err != nil {
			return err
		}
		out, err = loadOwned(ctx, tx, row.UploadID, row.Owner)
		return err
	})
	return out, err
}

// Release marks a terminal status and returns the reservation once.
func (s *Store) Release(ctx context.Context, uploadID, owner, next string, now time.Time) error {
	switch next {
	case "REJECTED", "EXPIRED", "CANCELLED":
	default:
		return errState
	}
	return s.withTx(ctx, func(tx *sql.Tx) error {
		if _, _, _, _, err := lockQuota(ctx, tx, owner); err != nil {
			return err
		}
		var declared, output int64
		err := tx.QueryRowContext(ctx, `
			UPDATE api.uploads
			SET status = $3, reservation_released = true, updated_at = $4
			WHERE upload_id = $1 AND owner_account_id = $2 AND reservation_released = false
			RETURNING declared_bytes, output_reservation`, uploadID, owner, next, now).Scan(&declared, &output)
		if errors.Is(err, sql.ErrNoRows) {
			return nil
		}
		if err != nil {
			return err
		}
		if _, err = tx.ExecContext(ctx, `
			UPDATE api.quotas
			SET input_reserved = input_reserved - $2, output_reserved = output_reserved - $3
			WHERE account_id = $1`, owner, declared, output); err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM api.processing_jobs WHERE upload_id = $1`, uploadID)
		return err
	})
}

// Sweep releases every abandoned upload once. The quota rows are locked in
// account id order before the upload rows.
func (s *Store) Sweep(ctx context.Context, now time.Time) (int, []string, error) {
	var names []string
	err := s.withTx(ctx, func(tx *sql.Tx) error {
		// ponytail: one ordered lock of every quota row. Split by account if this waits on intent traffic.
		rows, err := tx.QueryContext(ctx, `SELECT account_id FROM api.quotas ORDER BY account_id FOR UPDATE`)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id string
			if err := rows.Scan(&id); err != nil {
				_ = rows.Close()
				return err
			}
		}
		if err := rows.Close(); err != nil {
			return err
		}
		if err := rows.Err(); err != nil {
			return err
		}
		expired, err := tx.QueryContext(ctx, `
			SELECT upload_id, owner_account_id, declared_bytes, output_reservation, object_name
			FROM api.uploads
			WHERE reservation_released = false
				AND status IN ('RESERVED', 'RECEIVING', 'RECEIVED', 'PROBING')
				AND transfer_expires_at <= $1
			ORDER BY upload_id
			FOR UPDATE`, now)
		if err != nil {
			return err
		}
		type item struct {
			id, owner, object string
			declared, output  int64
		}
		var items []item
		for expired.Next() {
			var it item
			if err := expired.Scan(&it.id, &it.owner, &it.declared, &it.output, &it.object); err != nil {
				_ = expired.Close()
				return err
			}
			items = append(items, it)
		}
		if err := expired.Close(); err != nil {
			return err
		}
		if err := expired.Err(); err != nil {
			return err
		}
		for _, it := range items {
			res, err := tx.ExecContext(ctx, `
				UPDATE api.uploads
				SET status = 'EXPIRED', reservation_released = true, updated_at = $2
				WHERE upload_id = $1 AND reservation_released = false`, it.id, now)
			if err != nil {
				return err
			}
			n, err := res.RowsAffected()
			if err != nil {
				return err
			}
			if n != 1 {
				continue
			}
			if _, err = tx.ExecContext(ctx, `
				UPDATE api.quotas
				SET input_reserved = input_reserved - $2, output_reserved = output_reserved - $3
				WHERE account_id = $1`, it.owner, it.declared, it.output); err != nil {
				return err
			}
			names = append(names, it.object)
		}
		return nil
	})
	return len(names), names, err
}

// Probing returns uploads whose probe was interrupted.
func (s *Store) Probing(ctx context.Context) ([]uploadRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT upload_id, owner_account_id FROM api.uploads WHERE status = 'PROBING' ORDER BY upload_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uploadRow
	for rows.Next() {
		var row uploadRow
		if err := rows.Scan(&row.UploadID, &row.Owner); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

// Quota returns the reserved input and output bytes for an account.
func (s *Store) Quota(ctx context.Context, account string) (input, output int64, err error) {
	err = s.db.QueryRowContext(ctx, `
		SELECT input_reserved, output_reserved FROM api.quotas WHERE account_id = $1`, account).Scan(&input, &output)
	return input, output, err
}

// JobCount returns the number of processing jobs for one upload.
func (s *Store) JobCount(ctx context.Context, uploadID string) (int, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `
		SELECT count(*) FROM api.processing_jobs WHERE upload_id = $1`, uploadID).Scan(&n)
	return n, err
}
