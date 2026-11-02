package leases

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/module"

	leasesv1 "github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1"
)

// Test-only access for the external test package (package leases_test),
// which also imports leasestest. Nothing here is compiled into the module.

// OfferForTest reads one open offer.
func (k Keeper) OfferForTest(ctx sdk.Context, id string) (*leasesv1.Offer, bool, error) {
	return k.loadOffer(ctx, id)
}

// LeaseForTest reads one lease.
func (k Keeper) LeaseForTest(ctx sdk.Context, id string) (*leasesv1.Lease, bool, error) {
	return k.loadLease(ctx, id)
}

// SeedOfferForTest writes an open offer with its rows, counters, and
// used-id marker, as an accepted OfferLease does.
func (k Keeper) SeedOfferForTest(ctx sdk.Context, offer *leasesv1.Offer) error {
	return k.addOffer(ctx, offer)
}

// SeedLeaseForTest writes a lease, its used-offer marker, and, when it
// holds a reservation, its pair, deadline, content, and provider rows. It
// does not touch reserved bytes.
func (k Keeper) SeedLeaseForTest(ctx sdk.Context, lease *leasesv1.Lease) {
	k.saveLease(ctx, lease)
	k.kv(ctx).Set(UsedOfferKey(lease.GetOfferId()), marker())
	if lease.GetReservationHeld() {
		k.holdIndexes(ctx, lease)
	}
}

// Deadlines cursor codec.
const (
	PhaseLeasesForTest = int(phaseLeases)
	PhaseOffersForTest = int(phaseOffers)
)

func EncodeCursorForTest(phase int, id string) []byte { return encodeCursor(cursorPhase(phase), id) }

func DecodeCursorForTest(raw []byte) (int, string, bool) {
	phase, id, ok := decodeCursor(raw)
	return int(phase), id, ok
}

// AcceptCanonicalForTest, RenewCanonicalForTest, and EndCanonicalForTest
// are the canonical payload texts the sign bytes wrap.
var (
	AcceptCanonicalForTest = acceptCanonical
	RenewCanonicalForTest  = renewCanonical
	EndCanonicalForTest    = endCanonical
)

// MigrationsForTest returns the migration table; SetMigrationsForTest
// replaces it and returns a function that restores it.
func MigrationsForTest() map[uint64]module.MigrationHandler { return migrations }

func SetMigrationsForTest(m map[uint64]module.MigrationHandler) (restore func()) {
	saved := migrations
	migrations = m
	return func() { migrations = saved }
}
