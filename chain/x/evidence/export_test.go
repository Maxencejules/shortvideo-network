package evidence

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	evidencev1 "github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1"
)

// Test-only access for the external test package (package evidence_test),
// which also imports evidencetest. Nothing here is compiled into the module.

// SeedRecordForTest writes one record with its lease index row and batch
// marker, as an accepted batch does, without a window row or a business id.
func (k Keeper) SeedRecordForTest(ctx sdk.Context, record *evidencev1.EvidenceRecord) {
	k.addRecord(ctx, record)
	k.markBatch(ctx, record.GetBatchId())
}

// SetWindowForTest writes the window row of one auditor address.
func (k Keeper) SetWindowForTest(ctx sdk.Context, addr []byte, epoch, count uint64) {
	k.saveWindow(ctx, addr, epoch, count)
}

// WindowForTest reads the window row of one auditor address.
func (k Keeper) WindowForTest(ctx sdk.Context, addr []byte) (epoch, count uint64) {
	return k.mustLoadWindow(ctx, addr)
}

// MigrationsForTest returns the migration table; SetMigrationsForTest
// replaces it and returns a function that restores it.
func MigrationsForTest() map[uint64]module.MigrationHandler { return migrations }

func SetMigrationsForTest(m map[uint64]module.MigrationHandler) (restore func()) {
	saved := migrations
	migrations = m
	return func() { migrations = saved }
}
