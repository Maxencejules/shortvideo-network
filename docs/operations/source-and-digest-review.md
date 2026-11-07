# Source and digest review

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. A human reviewer does this review. A model-assisted review, including the one that wrote goal 10, is not this review and is not an independent security review.

Why it matters: an upgrade binary can change any rule, cap, quota, seat, or decision row, and it carries its own self-checks, so it can drop them. The runner's self-checks catch only an honest binary's mistakes. See [governance-and-upgrades.md](../architecture/governance-and-upgrades.md#9-safety-bounds-and-what-does-and-does-not-protect-them) section 9.

## Inputs

- The candidate's commit and manifest from [release-preparation.md](release-preparation.md).
- The previous release's commit and manifest.
- The plan the council proposes or executed: `Query/Policy` `state.upgradePlan` (name, height, release digest), read as in [council-changes.md](council-changes.md#reading-the-committed-result).

## Steps

1. Rebuild in two clean clones with the release recipe and compare the digests:

   ```bash
   for n in a b; do
     git clone --quiet "$SVN_SRC" "$REVIEW_DIR/clone-$n"
     git -C "$REVIEW_DIR/clone-$n" checkout --quiet "$RELEASE_COMMIT"
     CGO_ENABLED=0 go build -C "$REVIEW_DIR/clone-$n/chain" -trimpath -buildvcs=true -o "$REVIEW_DIR/svnd-$n" ./cmd/svnd
   done
   sha256sum "$REVIEW_DIR/svnd-a" "$REVIEW_DIR/svnd-b"
   cmp "$REVIEW_DIR/svnd-a" "$REVIEW_DIR/svnd-b"
   ```

   Both digests must equal the manifest's. Two clones on one machine with one toolchain show that the recipe is deterministic there; label the result "same machine, same toolchain; not an independent rebuild". On 2026-11-07 two clones of one commit built this way on one machine gave byte-identical binaries (observed on a work-in-progress revision, not evidence; the goal-10 evidence file records the result on the tested revision).

2. Read the build information of the candidate: `vcs.revision` is the reviewed commit, `vcs.modified` is `false`, and `go version -m` shows no `-tags` setting.

   ```bash
   "$REVIEW_DIR/svnd-a" version
   go version -m "$REVIEW_DIR/svnd-a"
   ```

3. Diff the dependencies against the previous release: `chain/go.sum`, `chain/go.mod`, and the SBOM (`docs/sbom.cdx.json`; `scripts/write_sbom.py --check` passes). Check the licence of every new module. DEC-006 excludes commercial-only components; any new licence needs an owner decision.

   ```bash
   git -C "$SVN_SRC" diff "$PREVIOUS_COMMIT" "$RELEASE_COMMIT" -- chain/go.mod chain/go.sum docs/sbom.cdx.json
   ```

4. Read the full diff of the upgrade path against the previous release. It is mandatory, because the binary can drop its own checks:

   ```bash
   git -C "$SVN_SRC" diff "$PREVIOUS_COMMIT" "$RELEASE_COMMIT" -- chain/upgrade chain/app/upgrades.go chain/app/app.go chain/cmd/svnd chain/x/policy chain/x/authority
   ```

   Confirm in it:
   - the upgrade handler does only what the manifest's migration notes say, and every migration step it runs is registered for the module version it moves;
   - the runner still refuses an upgrade that changes the rows it owns (app version, version map, plan, consensus params other than `Version.App`, policy version other than the +1) and every I13 class, which together cover every x/authority and x/policy row (`chain/app/upgrades.go` `rowClasses`; `TestUpgradePostConditionClasses`, `TestMigrationCannotChangeAuthorityRows`, `TestUpgradeHandlerEnvCannotChangeProtectedRows`, `TestUpgradeCannotChangePlanNameOrRunnerKeyRows`, `TestMigrationCannotAddRowsBesideThePolicyVersion`);
   - the per-block app-version rule and the load gate are unchanged, or changed only as the manifest explains (`TestLoadGateTable`, `TestPreBlockRefusesAppVersionMismatch`);
   - no skip height, no node-local stop option, and no second plan source were added (`TestNoSkipOrHaltHeight`, `TestPlanWritersHaveNoNodeLocalCaller`);
   - the build-tag files of `chain/cmd/svnd` still keep the rehearsal handlers out of the default build (`TestDefaultBuildCompilesNoRehearsal`).

5. Check payload behaviour. Under the owner's Q2 ruling, approved but unexecuted proposals become stale at the upgrade (the applied upgrade bumps the policy version), so no approval given for old code takes effect under new code. Confirm the diff keeps that bump (`TestUpgradeRehearsalInProcess` checks a queued proposal fails with `ERROR_CODE_STALE_VERSION` after the upgrade).

6. Compare digests end to end: the manifest's binary sha256, the two rebuilds, and the plan's release digest must be equal. If the council has not yet proposed the plan, give the reviewed digest to the seats. If the plan names another digest, do not install; the plan stays due, and only the council's cancel or a later plan with a new name changes it. The one exception is a corrected release for a plan that has already halted the chain ([failed-upgrade.md](failed-upgrade.md#restart-with-a-corrected-binary), case I8): its digest differs from the plan's, a halted council cannot change the plan, and the reviewed manifest is the reference.

7. Record the review: reviewer, date, commits, the digests compared, the diff ranges read, and the findings. Model output is not a review record.

## What a reviewer must not expect

- The chain does not check the release digest against any binary.
- The self-check list in the candidate is the candidate's own code. Reading its diff is the only way to know it was kept.
- A review of the source does not replace the independent security review that goal 29 requires (REQ-SEC-011).
