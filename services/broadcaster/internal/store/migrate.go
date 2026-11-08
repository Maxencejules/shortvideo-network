package store

import (
	"bytes"
	"context"
	"crypto/sha256"
	"database/sql"
	"embed"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"strconv"
)

// migrationFS holds the schema migrations. They are named NNNN_name.pgsql,
// not *.sql, because .gitignore ignores *.sql (database dumps).
//
//go:embed migrations/*.pgsql
var migrationFS embed.FS

// Migration runner errors.
var (
	ErrMigrationFiles    = errors.New("store: migration files are not NNNN_name.pgsql from 0001 without gaps")
	ErrMigrationChecksum = errors.New("store: applied migration differs from the binary's")
	ErrMigrationGap      = errors.New("store: applied migrations have a gap")
	ErrDatabaseNewer     = errors.New("store: database schema is newer than the binary")
	ErrSchemaBehind      = errors.New("store: database schema is older than the binary")
)

var migrationNameRE = regexp.MustCompile(`^([0-9]{4})_([a-z0-9_]{1,64})\.pgsql$`)

// migration is one embedded migration file.
type migration struct {
	version int
	name    string
	sql     string
	sum     [32]byte
}

// appliedMigration is one schema_migrations row.
type appliedMigration struct {
	version int
	name    string
	sum     []byte
}

// loadMigrations reads every file of dir "migrations" in fsys. Every file
// must match NNNN_name.pgsql and the versions must run 1, 2, 3, ... without a
// gap or a duplicate.
func loadMigrations(fsys fs.FS) ([]migration, error) {
	entries, err := fs.ReadDir(fsys, "migrations")
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrMigrationFiles, err)
	}
	var ms []migration
	for i, e := range entries {
		m := migrationNameRE.FindStringSubmatch(e.Name())
		if m == nil || e.IsDir() {
			return nil, fmt.Errorf("%w: unexpected entry %q", ErrMigrationFiles, e.Name())
		}
		v, err := strconv.Atoi(m[1])
		if err != nil || v != i+1 {
			return nil, fmt.Errorf("%w: %q is not version %d", ErrMigrationFiles, e.Name(), i+1)
		}
		b, err := fs.ReadFile(fsys, path.Join("migrations", e.Name()))
		if err != nil {
			return nil, fmt.Errorf("%w: %v", ErrMigrationFiles, err)
		}
		ms = append(ms, migration{version: v, name: m[2], sql: string(b), sum: sha256.Sum256(b)})
	}
	if len(ms) == 0 {
		return nil, fmt.Errorf("%w: none", ErrMigrationFiles)
	}
	return ms, nil
}

// Migrate applies the embedded migrations that the database lacks, in order,
// each in its own transaction under a transaction-level advisory lock, and
// returns the versions it applied. db must connect as OwnerRole. It refuses
// a database whose applied migrations have a gap, differ in name or checksum
// from the binary's, or are newer than the binary's (forward-only).
func Migrate(ctx context.Context, db *sql.DB) ([]int, error) {
	return migrate(ctx, db, migrationFS)
}

func migrate(ctx context.Context, db *sql.DB, fsys fs.FS) ([]int, error) {
	ms, err := loadMigrations(fsys)
	if err != nil {
		return nil, err
	}
	var done []int
	for {
		v, err := migrateStep(ctx, db, ms)
		if err != nil {
			return done, err
		}
		if v == 0 {
			return done, nil
		}
		done = append(done, v)
	}
}

// migrateStep applies the next missing migration and returns its version, or
// 0 when the database is current.
func migrateStep(ctx context.Context, db *sql.DB, ms []migration) (int, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("store: begin migration: %w", err)
	}
	if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock($1, 0)`, lockClassMigration); err != nil {
		return 0, rollback(tx, fmt.Errorf("store: migration lock: %w", err))
	}
	applied, err := readApplied(ctx, tx)
	if err != nil {
		return 0, rollback(tx, err)
	}
	if err := checkApplied(applied, ms); err != nil {
		return 0, rollback(tx, err)
	}
	if len(applied) == len(ms) {
		return 0, rollback(tx, nil)
	}
	next := ms[len(applied)]
	if _, err := tx.ExecContext(ctx, next.sql); err != nil {
		return 0, rollback(tx, fmt.Errorf("store: migration %04d_%s: %w", next.version, next.name, err))
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO broadcaster.schema_migrations (version, name, sha256) VALUES ($1, $2, $3)`,
		next.version, next.name, next.sum[:]); err != nil {
		return 0, rollback(tx, fmt.Errorf("store: record migration %04d: %w", next.version, err))
	}
	if err := commit(tx); err != nil {
		return 0, fmt.Errorf("store: commit migration %04d: %w", next.version, err)
	}
	return next.version, nil
}

// querier is the part of *sql.DB and *sql.Tx that readApplied needs.
type querier interface {
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// readApplied reads schema_migrations in version order, or nothing when the
// table does not exist yet.
func readApplied(ctx context.Context, q querier) ([]appliedMigration, error) {
	var exists bool
	if err := q.QueryRowContext(ctx,
		`SELECT to_regclass('broadcaster.schema_migrations') IS NOT NULL`).Scan(&exists); err != nil {
		return nil, fmt.Errorf("store: probe schema_migrations: %w", err)
	}
	if !exists {
		return nil, nil
	}
	rows, err := q.QueryContext(ctx,
		`SELECT version, name, sha256 FROM broadcaster.schema_migrations ORDER BY version LIMIT 10000`)
	if err != nil {
		return nil, fmt.Errorf("store: read schema_migrations: %w", err)
	}
	defer rows.Close()
	var out []appliedMigration
	for rows.Next() {
		var a appliedMigration
		if err := rows.Scan(&a.version, &a.name, &a.sum); err != nil {
			return nil, fmt.Errorf("store: scan schema_migrations: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: read schema_migrations: %w", err)
	}
	return out, nil
}

// checkApplied compares the applied rows with the binary's migrations.
func checkApplied(applied []appliedMigration, ms []migration) error {
	for i, a := range applied {
		if a.version != i+1 {
			return fmt.Errorf("%w: version %d at position %d", ErrMigrationGap, a.version, i+1)
		}
	}
	if len(applied) > len(ms) {
		return fmt.Errorf("%w: database %d, binary %d", ErrDatabaseNewer, len(applied), len(ms))
	}
	for i, a := range applied {
		if a.name != ms[i].name || !bytes.Equal(a.sum, ms[i].sum[:]) {
			return fmt.Errorf("%w: version %d", ErrMigrationChecksum, a.version)
		}
	}
	return nil
}

// CheckSchema reports nil only when the database has exactly the binary's
// migrations, with the same names and checksums. serve runs it at start and
// refuses to start otherwise (plan 6.4). Any role with SELECT on
// schema_migrations can run it.
func CheckSchema(ctx context.Context, db *sql.DB) error {
	ms, err := loadMigrations(migrationFS)
	if err != nil {
		return err
	}
	applied, err := readApplied(ctx, db)
	if err != nil {
		return err
	}
	if err := checkApplied(applied, ms); err != nil {
		return err
	}
	if len(applied) < len(ms) {
		return fmt.Errorf("%w: database %d, binary %d", ErrSchemaBehind, len(applied), len(ms))
	}
	return nil
}

// BindChain records chainID as the one chain this database serves, or checks
// that the bound one is the same. db must connect as OwnerRole.
func BindChain(ctx context.Context, db *sql.DB, chainID string) error {
	if !chainIDRE.MatchString(chainID) {
		return fmt.Errorf("%w: chain id", ErrInvalid)
	}
	if _, err := db.ExecContext(ctx,
		`INSERT INTO broadcaster.chain_binding (chain_id) VALUES ($1) ON CONFLICT (singleton) DO NOTHING`,
		chainID); err != nil {
		return fmt.Errorf("store: bind chain: %w", mapErr(err))
	}
	return CheckChain(ctx, db, chainID)
}

// CheckChain reports nil when chainID is the bound chain id.
func CheckChain(ctx context.Context, db *sql.DB, chainID string) error {
	var bound string
	err := db.QueryRowContext(ctx, `SELECT chain_id FROM broadcaster.chain_binding`).Scan(&bound)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrChainUnbound
	}
	if err != nil {
		return fmt.Errorf("store: read chain binding: %w", err)
	}
	if bound != chainID {
		return ErrChainMismatch
	}
	return nil
}

// RegisterCaller registers principal with an open-intent cap (1 to 4,096),
// or updates the cap of a registered one. The caps of all principals sum to
// at most 16,384; a cap that would exceed it is ErrInvalid. A non-empty
// role maps that PostgreSQL login role to the principal for the SQL path
// and grants it EXECUTE on enqueue_intent and SELECT on caller_intents. db
// must connect as OwnerRole.
func RegisterCaller(ctx context.Context, db *sql.DB, principal string, openIntentCap int, role string) error {
	if !idRE.MatchString(principal) || openIntentCap < 1 || openIntentCap > 4096 {
		return fmt.Errorf("%w: principal or cap", ErrInvalid)
	}
	var roleArg sql.NullString
	if role != "" {
		if !roleRE.MatchString(role) {
			return fmt.Errorf("%w: role", ErrInvalid)
		}
		roleArg = sql.NullString{String: role, Valid: true}
	}
	if _, err := db.ExecContext(ctx, `SELECT broadcaster.register_caller($1, $2, $3)`,
		principal, openIntentCap, roleArg); err != nil {
		return fmt.Errorf("store: register caller: %w", mapErr(err))
	}
	return nil
}
