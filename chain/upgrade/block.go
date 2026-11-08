package upgrade

// CheckBlock is the app-version rule for the block at height, given the
// stored app version and the scheduled plan (nil when none). It runs in
// PreBlock of every block, before any module code:
//
//  1. A plan below height is an ErrIntegrity error: the block at the plan
//     height either applied it or failed.
//  2. At the plan height: a binary with no handler of the plan's name
//     fails with ErrUpgradeNeeded (`UPGRADE "<name>" NEEDED at height <h>
//     release_digest=<hex>`); a binary that declares the name twice, or
//     whose handler does not go from the stored app version to the
//     binary's, fails with ErrWrongBinary; otherwise CheckBlock returns the
//     handler and true, and the caller runs the upgrade.
//  3. Otherwise a stored app version that differs from the binary's fails
//     with ErrWrongBinary (`stored app version <A>, binary <P>: wrong
//     binary for height <h>`).
//
// So an upgrade binary refuses every block below the plan height (when it
// replays its own block store, block-syncs from genesis, or was started
// early), and no binary runs a block of an app version it was not built
// for. Height 0 is an ErrIntegrity error: blocks start at 1.
func CheckBlock(height, appVersion uint64, plan *Plan, b Binary) (Upgrade, bool, error) {
	if height < 1 {
		return Upgrade{}, false, errorf(ErrIntegrity, nil, "block height 0")
	}
	if plan != nil {
		if plan.Height < height {
			return Upgrade{}, false, errorf(ErrIntegrity, nil,
				"upgrade %s at height %d is below block height %d: the block at the plan height should have applied it or failed",
				plan.Name, plan.Height, height)
		}
		if plan.Height == height {
			up, n := lookup(b, plan.Name)
			switch {
			case n == 0:
				return Upgrade{}, false, errorf(ErrUpgradeNeeded, nil, "UPGRADE %q NEEDED at height %d release_digest=%x",
					plan.Name, height, plan.Digest)
			case n > 1:
				return Upgrade{}, false, errorf(ErrWrongBinary, nil, "binary declares %d handlers named %s: wrong binary for height %d",
					n, plan.Name, height)
			case up.FromAppVersion != appVersion || up.ToAppVersion != b.AppVersion:
				return Upgrade{}, false, errorf(ErrWrongBinary, nil,
					"handler for upgrade %s goes from app version %d to %d; stored app version %d, binary %d: wrong binary for height %d",
					up.Name, up.FromAppVersion, up.ToAppVersion, appVersion, b.AppVersion, height)
			}
			return up, true, nil
		}
	}
	if appVersion != b.AppVersion {
		return Upgrade{}, false, errorf(ErrWrongBinary, nil, "stored app version %d, binary %d: wrong binary for height %d",
			appVersion, b.AppVersion, height)
	}
	return Upgrade{}, false, nil
}
