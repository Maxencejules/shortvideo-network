package store

import (
	"context"
	"fmt"
	"strings"
)

// projectionTables are the only relations rebuild may drop, and only by
// dropping the indexer schema. A table in that schema that is not in this
// set blocks the rebuild.
var projectionTables = map[string]bool{
	"schema_migrations": true,
	"chain_binding":     true,
	"checkpoint":        true,
	"events":            true,
	"providers":         true,
	"content":           true,
	"offers":            true,
	"leases":            true,
	"policy":            true,
	"evidence":          true,
	"reconcile_runs":    true,
	"findings":          true,
	"observer":          true,
}

// Rebuild drops the projection schema and applies the migration again.
// It refuses to run as any role other than the owner, and it refuses when
// the indexer schema holds a table that is not a projection. The drop
// statement names only that schema.
func (s *Store) Rebuild(ctx context.Context, chainID string) error {
	if err := guardRebuild(rebuildDrop); err != nil {
		return err
	}
	var user string
	if err := s.db.QueryRowContext(ctx, `SELECT current_user`).Scan(&user); err != nil {
		return err
	}
	if user != OwnerRole {
		return fmt.Errorf("%w: current user %s", ErrRole, user)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT c.relname FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'indexer' AND c.relkind = 'r'`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return err
		}
		if !projectionTables[name] {
			return fmt.Errorf("%w: indexer.%s", ErrAuthoritative, name)
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, rebuildDrop); err != nil {
		return err
	}
	if err := Migrate(ctx, s.db); err != nil {
		return err
	}
	return Bind(ctx, s.db, chainID)
}

// guardRebuild rejects a drop statement that names anything but the
// projection schema.
func guardRebuild(statement string) error {
	if statement != rebuildDrop {
		return fmt.Errorf("%w: unexpected statement", ErrAuthoritative)
	}
	for _, banned := range []string{"app_private", "broadcaster", "social", "public.", "DROP TABLE", "DROP DATABASE"} {
		if strings.Contains(statement, banned) {
			return fmt.Errorf("%w: statement names %s", ErrAuthoritative, banned)
		}
	}
	return nil
}
