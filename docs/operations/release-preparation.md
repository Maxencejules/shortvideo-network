# Release preparation

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. Goal 10 tags, signs, and publishes nothing; a binary built with these steps is a candidate for review, not a release.

The upgrade mechanism is specified in [governance-and-upgrades.md](../architecture/governance-and-upgrades.md#11-upgrade-version-model) sections 11 to 14. Source and digest review is a separate human step: [source-and-digest-review.md](source-and-digest-review.md).

## Roles

| Role | Does | Does not |
| --- | --- | --- |
| Maintainers (publication authority) | prepare the candidate: commit, build, manifest, migration notes | schedule an upgrade or run a validator by virtue of the role |
| Human reviewers | rebuild, compare digests, read the diff ([source-and-digest-review.md](source-and-digest-review.md)) | rely on a model review as the review |
| Council | schedule the plan with the reviewed digest ([council-changes.md](council-changes.md)) | install a binary on any node |
| Each operator | decide whether to install ([operator-installation.md](operator-installation.md)) | skip a plan the council executed |
| Owner | sign, tag, and publish a release, and name the publisher (goal 27) | |

## What a release build is

- One main package, `chain/cmd/svnd`, builds every `svnd` binary. A release build uses no build tag. The default build compiles no upgrade handler and links no `chain/internal` code (`TestDefaultBuildCompilesNoRehearsal`, `TestLinkedBinaryOmitsTestHooks`) and no economic module (`TestLinkedBinaryOmitsEconomicModules`).
- `-tags svn_rehearsal` and `-tags svn_rehearsal_bad` build the goal-10 rehearsal binaries B2 and B2-bad. They declare the rehearsal upgrade `rehearsal-1` and refuse to start on a chain whose id does not start with `shortvideo-dev-` (`TestRehearsalBuildsDeclareTheRehearsalUpgrade`, `TestRehearsalBuildsRefuseNonDevChains`). They are never release candidates. Building them from one main package with tags is orchestrator choice ORCH-6, a reversible implementation choice, not an owner decision.
- `-tags svn_statecheck` (alone, or with a rehearsal tag) adds the `svnd statecheck` test tool, which links test-support checkers; it also refuses to start a validator on a chain whose id does not start with `shortvideo-dev-` (`TestStatecheckBuildsReportOnAHome`, `TestRehearsalBuildsRefuseNonDevChains`). It is never a release candidate: its help text has the "This build starts only" line and `go version -m` shows its `-tags` setting.
- A release that carries an upgrade compiles its handler into the default build. Its `svnd version` then prints `app_version P` and one line `upgrade <name> from_app_version <P-1> to_app_version <P>`. The goal-10 `svnd` declares app version 1 and prints `upgrades none`.

## Steps

1. Pin the checkout. Record the commit and the tree, and require a clean tree:

   ```bash
   git -C "$SVN_SRC" rev-parse HEAD
   git -C "$SVN_SRC" rev-parse 'HEAD^{tree}'
   test -z "$(git -C "$SVN_SRC" status --porcelain)"
   ```

2. Check the toolchain. `chain/go.mod` names `toolchain go1.26.10` and the CI jobs pin Go 1.26.10:

   ```bash
   go version
   ```

3. Check the gates on that commit: every CI job of `.github/workflows/ci.yml` passed, and the contract checks pass locally:

   ```bash
   .venv/bin/python docs/evidence/goal-01/check_requirements.py
   PATH="$SVN_SRC/.toolchains/bin:$PATH" .venv/bin/python docs/evidence/goal-02/check_protocol.py
   .venv/bin/python scripts/write_sbom.py --check
   .venv/bin/python scripts/scan_secrets.py
   ```

4. Build from a fresh clone of the pinned commit, with no build tag. `-buildvcs=true` stamps the commit into the binary:

   ```bash
   git clone --quiet "$SVN_SRC" "$BUILD_DIR/src"
   git -C "$BUILD_DIR/src" checkout --quiet "$RELEASE_COMMIT"
   CGO_ENABLED=0 go build -C "$BUILD_DIR/src/chain" -trimpath -buildvcs=true -o "$BUILD_DIR/svnd" ./cmd/svnd
   ```

   Build from a clone, not from a `git worktree` checkout. On 2026-11-07, with Go 1.26.10, a build from a `git worktree` checkout printed `vcs.revision unknown` and `vcs.modified unknown` even with `-buildvcs=true`, while a build from a fresh clone of that checkout's HEAD printed the commit and `vcs.modified false` (observed on a work-in-progress revision, not evidence). A binary whose revision reads `unknown` is not a candidate.

5. Verify the build:

   ```bash
   "$BUILD_DIR/svnd" version
   go version -m "$BUILD_DIR/svnd"
   "$BUILD_DIR/svnd" --help
   sha256sum "$BUILD_DIR/svnd"
   ```

   - `vcs.revision` is the pinned commit and `vcs.modified` is `false`.
   - `go version -m` shows no `-tags` setting. A `-tags=` line means a rehearsal or test build.
   - `svnd --help` has no line "This build starts only on a chain whose id starts with ..."; the rehearsal builds print it.
   - `app_version` and the `upgrade` lines are what the release declares: `upgrades none` for a release without an upgrade, else exactly one `upgrade` line whose name is the plan name the council will schedule and whose `from_app_version` is the app version the chain runs now (`Query/Policy` `state.appVersion`).
   - The `module` lines list every module's consensus version.

6. Write the release manifest. It holds:
   - the commit, the tree, and the `go version` output;
   - the `svnd version` and `go version -m` output of the binary;
   - the binary's sha256, in `sha256sum` format;
   - the upgrade name, its from and to app versions, and the release digest the council will put in the plan (the binary's sha256);
   - migration notes per module: each `module` line that differs from the previous release, the migration step registered for it, and what the handler writes;
   - the diff of `chain/go.sum` against the previous release and the result of `scripts/write_sbom.py --check`;
   - the list of the runner's self-check classes (the I13 classes in `chain/app/upgrades.go`) and its diff against the previous release.

7. Hand the commit and the manifest to the reviewers ([source-and-digest-review.md](source-and-digest-review.md)). Nothing is tagged, signed, or published here.

## What an operator must not expect

- The chain never compares the plan's release digest with the binary a node runs. The digest helps operators find the reviewed binary; it does not prove which binary runs.
- A rebuild on the same machine with the same toolchain that gives the same sha256 shows that the recipe is deterministic there. It is not an independent rebuild.
- An upgrade binary can change any rule, including its own self-checks. Human review of the source and digest, and each operator's choice to install, are the controls.
