package upgrade

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/Maxencejules/shortvideo-network/chain/msgkit"
)

// PlanRows reads the committed upgrade rows of the x/policy store: the plan
// row (0x06), converted to Plan, and the applied-upgrade rows (0x07).
//
// This package imports no module keeper (plan section 3;
// TestPackageImports), so the app supplies a small adapter over the
// x/policy keeper: StoredPlan returns the name, height, and digest of the
// keeper's Plan, and UpgradeApplied is the keeper's own method. Either may
// panic on a committed row that does not decode, as the keeper does;
// CouncilPlans turns that panic into an error.
type PlanRows interface {
	// StoredPlan returns the stored plan; found is false when no plan row
	// exists.
	StoredPlan(ctx sdk.Context) (plan Plan, found bool)
	// UpgradeApplied reports whether an upgrade of this name has applied.
	UpgradeApplied(ctx sdk.Context, name string) bool
}

// CouncilPlans is the production PlanSource under owner question Q2 (option
// "Council plan, fail closed (Recommended)"). The scheduled upgrade is the
// plan row of the x/policy store, and only an executed council proposal
// writes that row: a software_upgrade payload stores or replaces it, and a
// cancel_upgrade payload deletes it before its height. The runner's
// RecordUpgrade deletes it when the upgrade applies.
//
// CouncilPlans only reads. It has no method that schedules, moves, cancels,
// or skips a plan, and this package has no halt height
// (TestCouncilPlansOnlyReads, TestPlanWritersHaveNoNodeLocalCaller,
// TestNoSkipOrHaltHeight). A plan is a block height only; no block time is
// read.
//
// Plan returns an error wrapping ErrIntegrity, and no plan, when:
//
//   - reading a row panicked (a committed row that does not decode)
//   - the stored plan's name is outside the protocol id pattern, its height
//     is 0, or its release digest is not DigestBytes long
//   - an upgrade of the plan's name has already applied (the council
//     writers never store such a plan, and RecordUpgrade deletes the plan
//     it records)
//
// PreBlock then fails the block before it writes anything, and the load
// gate reports the error instead of a decision (Runner.StoredState).
type CouncilPlans struct {
	rows PlanRows
}

// NewCouncilPlans returns the council plan source over rows. A nil rows is
// an ErrInvalidDeps error.
func NewCouncilPlans(rows PlanRows) (*CouncilPlans, error) {
	if rows == nil {
		return nil, errorf(ErrInvalidDeps, nil, "the council plan source needs the plan rows")
	}
	return &CouncilPlans{rows: rows}, nil
}

// Plan returns the stored plan with a copy of its digest, or found false when
// no plan row exists. It implements PlanSource. A source not built by
// NewCouncilPlans returns an ErrInvalidDeps error.
func (c *CouncilPlans) Plan(ctx sdk.Context) (Plan, bool, error) {
	if c == nil || c.rows == nil {
		return Plan{}, false, errorf(ErrInvalidDeps, nil, "the council plan source was not built by NewCouncilPlans")
	}
	stored, found, applied, err := c.read(ctx)
	if err != nil || !found {
		return Plan{}, false, err
	}
	switch {
	case !msgkit.ValidID(stored.Name):
		return Plan{}, false, errorf(ErrIntegrity, nil, "the stored upgrade plan name %q is outside the id pattern", stored.Name)
	case stored.Height < 1:
		return Plan{}, false, errorf(ErrIntegrity, nil, "the stored upgrade plan %s has height 0", stored.Name)
	case len(stored.Digest) != DigestBytes:
		return Plan{}, false, errorf(ErrIntegrity, nil, "the stored upgrade plan %s has a release digest of %d bytes, want %d",
			stored.Name, len(stored.Digest), DigestBytes)
	case applied:
		return Plan{}, false, errorf(ErrIntegrity, nil, "the stored upgrade plan %s names an upgrade that has already applied", stored.Name)
	}
	return Plan{Name: stored.Name, Height: stored.Height, Digest: append([]byte(nil), stored.Digest...)}, true, nil
}

// read returns the stored plan and, for a plan whose name matches the id
// pattern, whether that upgrade has applied. A panic in either read is
// returned as an ErrIntegrity error.
func (c *CouncilPlans) read(ctx sdk.Context) (plan Plan, found, applied bool, err error) {
	defer func() {
		if p := recover(); p != nil {
			plan, found, applied = Plan{}, false, false
			err = errorf(ErrIntegrity, fmt.Errorf("panic: %v", p), "read the committed upgrade plan rows")
		}
	}()
	plan, found = c.rows.StoredPlan(ctx)
	if found && msgkit.ValidID(plan.Name) {
		applied = c.rows.UpgradeApplied(ctx, plan.Name)
	}
	return plan, found, applied, nil
}
