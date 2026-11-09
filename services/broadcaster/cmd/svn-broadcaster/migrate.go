package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/config"
	"github.com/Maxencejules/shortvideo-network/services/broadcaster/internal/store"
)

// migrateResult is migrate's output on standard output.
type migrateResult struct {
	ChainID string `json:"chain_id"`
	// Applied lists the migration versions this run applied (empty when
	// the schema was already current).
	Applied []int `json:"applied"`
	// CallersRegistered is the number of callers registered with
	// --callers.
	CallersRegistered int `json:"callers_registered"`
}

// runMigrate is the migrate subcommand: as the owner role it applies the
// embedded migrations, binds the database to the configured chain id, and
// with --callers registers every configured caller.
func runMigrate(ctx context.Context, args []string, env processEnv, log *slog.Logger) error {
	fs := newFlags("migrate", env)
	cfgPath := configFlag(fs)
	callers := fs.Bool("callers", false, "also register every caller of the configuration (open-intent cap and SQL-path role)")
	if err := parseFlags(fs, args); err != nil {
		return err
	}
	c, err := loadConfig(*cfgPath)
	if err != nil {
		return err
	}
	if err := c.RequireMigrate(); err != nil {
		return refusedError(err)
	}
	if *callers && len(c.Callers) == 0 {
		return refusedError(errors.New("--callers needs at least one caller in the configuration"))
	}
	conn, err := c.ConnConfig()
	if err != nil {
		return refusedError(err)
	}
	db, err := store.OpenDB(conn)
	if err != nil {
		return refusedError(err)
	}
	var regs []config.CallerRegistration
	if *callers {
		regs = c.CallerRegistrations()
	}
	res, err := migrateDB(ctx, db, c.ChainID, regs)
	if closeErr := db.Close(); err == nil && closeErr != nil {
		err = fmt.Errorf("close database: %w", closeErr)
	}
	if err != nil {
		return err
	}
	log.Info("migrated", "chain_id", c.ChainID, "applied", int64(len(res.Applied)),
		"callers_registered", int64(res.CallersRegistered))
	return json.NewEncoder(env.stdout).Encode(res)
}

// migrateDB runs the migrations, the chain binding and the caller
// registrations regs (none without --callers).
func migrateDB(ctx context.Context, db *sql.DB, chainID string, regs []config.CallerRegistration) (migrateResult, error) {
	res := migrateResult{ChainID: chainID, Applied: []int{}}
	applied, err := store.Migrate(ctx, db)
	if err != nil {
		if errors.Is(err, store.ErrMigrationChecksum) || errors.Is(err, store.ErrMigrationGap) ||
			errors.Is(err, store.ErrDatabaseNewer) {
			return res, refusedError(err)
		}
		return res, fmt.Errorf("migrate: %w", err)
	}
	res.Applied = append(res.Applied, applied...)
	if err := store.BindChain(ctx, db, chainID); err != nil {
		if errors.Is(err, store.ErrChainMismatch) {
			return res, refusedError(err)
		}
		return res, fmt.Errorf("bind chain: %w", err)
	}
	for _, r := range regs {
		if err := store.RegisterCaller(ctx, db, r.Principal, r.OpenIntentCap, r.SQLRole); err != nil {
			if errors.Is(err, store.ErrInvalid) {
				return res, refusedError(fmt.Errorf("register caller %s: %w", r.Principal, err))
			}
			return res, fmt.Errorf("register caller %s: %w", r.Principal, err)
		}
		res.CallersRegistered++
	}
	return res, nil
}
