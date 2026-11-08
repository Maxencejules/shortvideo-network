package policy

import "github.com/cosmos/cosmos-sdk/types/module"

// MigrationsForTest returns the migration table.
func MigrationsForTest() map[uint64]module.MigrationHandler { return migrations }

// SetMigrationsForTest replaces the migration table and returns a function
// that restores it.
func SetMigrationsForTest(table map[uint64]module.MigrationHandler) func() {
	old := migrations
	migrations = table
	return func() { migrations = old }
}
