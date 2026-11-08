#!/usr/bin/env python3
"""Goal 10 mutation runner (standard library only).

What it does (the design and CLI of docs/evidence/goal-09/mutants.py, with
one change: every mutant names the tests that must kill it):
1. Exports a committed revision (default HEAD) with `git archive` into a new
   temporary directory outside the repository. With --worktree it copies the
   working tree instead (see below).
2. Runs, once per selected group, every killer test that the group's
   selected mutants name, on the unmutated copy (control). A control passes
   only when every command exits 0, every named Go test printed a top-level
   `--- PASS` in its own package's output, every named pytest test passed,
   and no named test was skipped. A failing control stops the run before
   any mutant.
3. For each mutant: applies its edits (each replaces one exact search string
   in one file; the string must occur exactly once in the file as it is when
   the edit is applied), runs only that mutant's named killer tests in the
   copy (`go test -run '^(names)$'` on the killers' packages, and pytest on
   the named test functions), and restores every edited file. A mutated .py
   file must still compile and a mutated .json file must still parse, so a
   syntax error is never counted as a kill.

Verdicts:
- KILLED: a command exited non-zero, no Go package or test binary failed to
  build, and a NAMED killer failed: a top-level `--- FAIL: <name>` line in
  the output of the killer's own package, or a `panic:` in that package's
  output whose last `=== RUN` line before the panic belongs to the named
  test (go test); a `FAILED` or `ERROR` result line for the named test
  function (pytest). A failure of any other test is not a kill.
- SURVIVED: every command exited zero.
- BUILD_FAILED: a Go package or test binary did not build. This is not a
  kill.
- UNCLEAR: a command exited non-zero and no named killer failed. This is
  not a kill.
- TIMEOUT: a command ran longer than --timeout seconds.

Exit status is 0 only when every control passed and every mutant is KILLED.
It is non-zero when a search string is missing or not unique, or a named
killer is not defined (both checked for all selected mutants before any test
runs), when a control fails, or when any mutant is SURVIVED, BUILD_FAILED,
UNCLEAR or TIMEOUT.

Usage:
  python3 docs/evidence/goal-10/mutants.py [--rev REV | --worktree]
                                            [--group G[,G...]] [--only a,b]
                                            [--check] [--list] [--logdir DIR]
                                            [--keep] [--gocache DIR]
                                            [--timeout SECONDS]

--check validates the search strings and the named killers and runs
nothing: every Go killer must be a `func Test...(` or `func Fuzz...(` in a
_test.go file of the named package directory, and every pytest killer a
`def test_...(` in the named file. --list prints every mutant with its
group, files and killers and runs nothing.

--worktree copies the working tree instead of exporting --rev: every file
`git ls-files --cached --others --exclude-standard` lists that exists on
disk, as it is on disk (uncommitted edits and untracked, not ignored, files
included; ignored files such as .venv are not copied). It is for dry runs of
uncommitted tests and mutants. Its output is not evidence for a committed
revision: the header names HEAD and prints `git status --porcelain`, so a
log shows what the copy held beyond HEAD. --worktree and --rev are
exclusive.

The copy runs Go with GOPROXY=off and `go test -p 2`, so the module cache
must already hold the dependencies and the toolchain that go.work names. By
default the copy uses its own GOCACHE under the temporary directory, which
is removed at the end unless --keep is given. --gocache DIR uses DIR
instead; the Go build cache is content-addressed, so this changes build time
only, never a verdict. pytest and the difftest oracle (spec_machine.py
through tests/difftest/spec_oracle.py) run under the repository's
.venv/bin/python when it exists, else under this interpreter.

What the commands start. On the unmutated tree no command starts a
validator: the only chain/devnet test named here is
TestInitCouncilDelayAndAppVersion, which writes a devnet home and reads its
genesis file, and chain/node's TestStartWithPassesUpgradeHandlers expects
node.StartWith to refuse during the CometBFT handshake, before any port is
bound; chain/cmd/svnd/cli's TestStartHandsTheHandlersToTheNodeOnAHome
expects the same refusal through `svnd start`. On the mutants
inta-node-drops-upgrades and p3a-cli-start-drops-handlers that refusal does
not happen, so the named test starts one validator of a four-validator
devnet home (no block commits without the other three); the test waits at
most 60 s for the refusal and then fails (the kill), and the node it
started runs until that go test process exits. Those two mutants keep a 3m
go test timeout as a bound.
Some named tests build svnd (the default build and the svn_rehearsal,
svn_rehearsal_bad and svn_statecheck builds of chain/cmd/svnd) and run
`svnd version`, `svnd --help`, `svnd start` on a home whose chain id the
build refuses, and `svnd statecheck` on a stopped home; none of these binds
a port. The runs of this runner recorded for goal 10 ran inside a private
network namespace with only loopback up. No group uses the network.

Groups (one set of commands per group, run from the root of the copy; the
packages and tests of a group are the union of its mutants' killers):
- council-acceptance: the council flow of chain/x/authority/engine.go
  (threshold, review delay, approve and execute expiry, duplicate approval,
  propose and execute staleness, the AUTHORITY role, the propose and approve
  seat checks) and the goal 07 registry-write anchor. Killers are the
  goal 06 acceptance tests through committed transactions (chain/app), the
  keeper tests (chain/x/authority) and the differential against
  spec_machine.py (chain/x/authority/difftest), so this group re-covers the
  goal 06 authority checks that matter most (DR-10-34).
- authority: typed governed changes (D-1, the activation and plan-height
  windows with MinActivationLeadBlocks = 100 and MaxActivationLeadBlocks =
  1,000,000, both PROPOSED constants of the owner's Q3 option "3 of 4 + min
  notice (Recommended)"), seat replacement, D-2 events, D-7 and D-10, the
  governance query, the upgrade policy-version bump (DR-10-38) and the
  authority layout.
- policy: x/policy: ApplyDue, SetLimit and SchedulePending, the plan rows
  and the plan-name rule (ORCH-10), RecordUpgrade, genesis and export
  refusals, Query/Policy, and the policytest invariant checkers.
- upgrade: chain/upgrade (load gate, per-block rule, runner, CouncilPlans,
  the handler environment of ORCH-7), the app's I13 classes (one mutant per
  class, and the exact-key {0x04} rule), PreBlock, the InitChain app-version
  rule, the load gate in app.New, and PrepareCheckState.
- payload: chain/x/authority/payload.go and the parameter-key binding: the
  canonical text of the new payload kinds, v0 digests unchanged (the
  owner's Q1 option "Minimal additive (Recommended)"), and the G-11 field
  shapes.
- app: chain/app wiring (InitChain, genesis, export, layout table, seat
  accounts), chain/genesis D-7 and D-10, devnet init, chain/node, the svnd
  cli, and the build tags of ORCH-6 (the default build links no rehearsal
  code; a tagged rehearsal build refuses a chain id without the devnet
  prefix).
- rehearsal: chain/internal/rehearsal (the migration step, the plan 12.2
  read-only walk, the B2 and B2-bad handlers) and the app-level rehearsal.
- statecheck: chain/internal/statecheck and the svn_statecheck subcommand.
- difftest: the authority differential harness (chain/x/authority/difftest
  test files) and the oracle's G-3, G-4, G-5 and G-11 rules
  (tests/difftest/spec_oracle.py).
- docs: tests/unit/test_operations_docs.py on the goal 10 runbooks and the
  governance specification.

Where the mutants come from (names start with their source; every source
is a scratch runner of a goal 10 work package, not in Git, whose search
strings are reused unchanged unless a line says otherwise):
- council-*, p2a-*: Wave 0 P2a (authority keeper) and P4 (authority
  differential); the same edit reported by both is listed once.
  p2a-writer-called-outside-apply is re-anchored on the current execute
  lines and calls the writer through State.Policy (the Wave 0 helper
  policyWriter no longer exists). council-approve-seat-bypass is the
  approve-side twin of P2a's propose-seat-bypass.
- p1-*: Wave 0 P1 (x/policy core). M3 and M4 are the Wave 1 P1b mutants
  p1b-upper-lead-off-by-one and p1b-plan-name-no-conflict on the current
  code; M9, M10 and M15 are re-anchored on the lines that now hold the same
  checks (p1-record-skips-plan-match, p1-record-keeps-plan,
  p1-query-version-constant).
- rev-*: the Wave 0 test-honesty review. rev-d1-after-expiry-check is
  re-anchored on the decoded governed limit.
- p1b-*, p2b-*, p3a-*, p3a2-*, p3b-*, inta-*, intb-*, p5a-*, p4b-*: Wave 0
  P3a and the Wave 1 packages (P1b policy, P2b authority, P3b plan source,
  INT-a and INT-b app integration, P5a rehearsal, P4b difftest). Where two
  packages reported the same edit (INT-b and P1b/P1, INT-a and P5a, P3a,
  P3b and P5a on the block rule, gate, params and app-version lines), it is
  listed once with the killers of both.
- intc-*, p3c-*, p4c-*, p6b-*: the Wave 2 packages (INT-c tagged builds and
  handler environment, P3c state check, P4c G-11 widening, P6b runbooks).
- plan-*: plan section 16.9 rows that no package runner covered (the PreBlock
  error path, one mutant per I13 class, the
  first-byte 0x04 skip that the Wave 1 gate fixed, the plan height shifted
  by -1, a past plan height, cancel of another name, application at a-1, a
  seat payload that approves a decision row, an old seat key left
  unretired, the new seat key's account created before the seat checks, the v0 canonical text, the
  default build linking rehearsal code, the statecheck registration, the
  rehearsal migrations run twice, an unsafe-reset-all step in
  failed-upgrade.md).
- ported-06-*, ported-07-*: the anchors goal 10 moved from the goal 06 and
  goal 07 runners (table below).
- fix-*: the Wave 2 fix round (security review: the x/policy I13 classes,
  the chain id CometBFT runs, the state check build's chain id restriction,
  the LOCK file of a database copy; tests review L1: prefix, not substring,
  chain id checks), on code and tests that round added.

Candidate mutants of the package runners that are not listed, and why:
- P4b d5-projection-diff-unchecked, P4c space-projection-unchecked and
  digest-json-unknown-as-name: control mutants that weaken a check no
  current input trips; their packages reported them as expected survivors.
- P4b g11-rows-not-checked-for-state: it skips the G-11 assertions of
  TestCouncilRulesAreLoadBearing (chain code, spec code with the rule off,
  no projection difference), which pass on the unmutated tree, so the test
  it weakens cannot fail on it; it survived this runner's dry run.
- P4b shape-before-g10 (swaps the disjoint G-10 and G-11 conditions) and
  P4c empty-content-id-allowed (proto3 JSON never writes an empty id):
  equivalent, as their packages reported.
- P4b c5-any-reason: its condition was rewritten by P4c (conditions 9 to
  13); p4c-oracle-* cover it.
- P6b "release check removed": equivalent (the phrase `upgrades none` occurs
  more than once in release-preparation.md).
- P2b blocked-decisions-no-clone and P2a setlimit-check-removed: the code
  they mutate was removed (P2b) or replaced by the bound x/policy writer.
- P3a node-accepts-handlers: node.StartWith now passes the handlers through
  (INT-a); inta-node-drops-upgrades covers the new line.
- P5a bad-step-honest: BadLeasesStep was removed by INT-c;
  intc-bad-handler-no-write covers the B2-bad write.
- rev-policywriter-called-outside-apply: p2a-writer-called-outside-apply
  covers the same call-site rule on the current line.
- Plan 16.9 "do not set ConsensusParamsChanged" (PreBlocker reports
  `rsp.ConsensusParamsChanged = rsp.ConsensusParamsChanged` without
  `|| applied`): it survived this runner's dry run (named killer
  TestUpgradeRehearsalInProcess) and is equivalent on this tree. Cosmos SDK
  v0.55.0 builds FinalizeBlock's ConsensusParamUpdates from the stored
  params (baseapp/abci.go), so block h still reports Version.App 2; the
  flag only refreshes the finalize context's consensus params and block gas
  meter for the rest of block h (baseapp/baseapp.go preBlock), and no chain
  code reads ctx.ConsensusParams() while Block.MaxGas is unchanged.

Tests added for this runner (test-only changes in the owning test files;
each kills a mutant that survived the runner's first dry run):
- chain/x/policy/writer_test.go, TestPlanScheduleReplaceCancel: the row
  "height below the block" (a plan height below the current block is
  OUT_OF_RANGE) kills plan-plan-past-height.
- chain/cmd/svnd/cli/cli_test.go, TestStartHandsTheHandlersToTheNodeOnAHome
  (new) kills p3a-cli-start-drops-handlers.
The Wave 2 fix round added the tests the fix-* mutants name (in the owning
test files) and bounded TestStartHandsTheHandlersToTheNodeOnAHome and
chain/node's TestStartWithPassesUpgradeHandlers to 60 s.

Anchors that goal 10 moved out of the earlier runners. Earlier runners are
never edited: the goal 06 runner refuses on this tree with 13 missing
anchors (9 since goal 08, plus the 4 below) and the goal 07 runner with 3
(2 since goal 08, plus the 1 below); both stay BLOCKED. Each moved anchor
is ported here as the same change on the line that now holds the check:

| earlier runner, mutant | old file and search string | goal 10 mutant | new file and search string |
| --- | --- | --- | --- |
| goal 06 ceiling-gone | chain/x/authority/engine.go `if !known \\|\\| decoded.LimitValue < 1 \\|\\| decoded.LimitValue > ceiling {` | ported-06-ceiling-gone | chain/x/authority/policy.go `return known && value >= 1 && value <= ceiling` (governedInRange; the ceiling now comes from the msgkit table) |
| goal 06 unknown-limit-ok | the same line | ported-06-unknown-limit-ok | the same governedInRange line |
| goal 06 svnd-skips-authority-rules | chain/cmd/svnd/main.go `return app.ValidateAuthorityGenesis(...)` | ported-06-svnd-skips-authority-rules | chain/cmd/svnd/cli/genesis.go, the same return line (the cli package now holds the genesis validation) |
| goal 06 swap-engine-282 | chain/x/authority/engine.go `if msg.GetParameterKey() != parameterKey(kind, change.NodeID, limitKey(msg), providerID(msg), contentID(msg)) {` then `return fail(CodeMalformed)` | ported-06-swap-engine-282 | chain/x/authority/engine.go `if msg.GetParameterKey() != parameterKey(decoded) {` then `return fail(CodeMalformed)` (parameterKey takes the decoded payload) |
| goal 07 msg-success-skips-registry-write | chain/x/authority/msg.go `case out.Success():` / `writeRegistry()` / `write()` | ported-07-msg-success-skips-registry-write | chain/x/authority/msg.go `case out.Success():` / `writeRegistry()` (emitExecuted, D-2, now follows writeRegistry) |

The Wave 1 brief names the fourth goal 06 anchor "swap-engine-482"; the goal
06 runner has no such mutant, and `--check` of that runner on ff951fc (the
goal 09 tree) and on this tree shows that the anchor goal 10 moved is
swap-engine-282.

Each mutant's killers are tests that failed on it in this runner's own
dry runs on the goal 10 working tree; a test first named from a package
report that did not fail on the mutant was taken off its list. This runner
re-runs all of them; its own output is the evidence, not the scratch logs.
"""

from __future__ import annotations

import argparse
import io
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import tarfile
import tempfile
import time
from dataclasses import dataclass

COUNCIL = "council-acceptance"
AUTHORITY = "authority"
POLICY = "policy"
UPGRADE = "upgrade"
PAYLOAD = "payload"
APP = "app"
REHEARSAL = "rehearsal"
STATECHECK = "statecheck"
DIFFTEST = "difftest"
DOCS = "docs"
GROUPS = (COUNCIL, AUTHORITY, POLICY, UPGRADE, PAYLOAD, APP, REHEARSAL, STATECHECK, DIFFTEST, DOCS)

GO_MODULE_PREFIX = "github.com/Maxencejules/shortvideo-network/"
GO_TEST_TIMEOUT = "15m"


@dataclass(frozen=True)
class Killer:
    """A test that must fail on the mutant: a Go test or fuzz target in a
    package directory (repository-relative, e.g. chain/x/policy), or a
    pytest test function in a file (repository-relative, ends in .py)."""

    package: str
    name: str

    @property
    def pytest(self) -> bool:
        return self.package.endswith(".py")

    def label(self) -> str:
        return f"{self.package}::{self.name}" if self.pytest else f"{self.package}:{self.name}"


def K(package: str, *names: str) -> tuple[Killer, ...]:
    return tuple(Killer(package, name) for name in names)


@dataclass(frozen=True)
class Edit:
    path: str
    old: str
    new: str


@dataclass(frozen=True)
class Mutant:
    name: str
    group: str
    edits: tuple[Edit, ...]
    killers: tuple[Killer, ...]
    reason: str
    # go test -timeout for this mutant's command. A shorter one is set where
    # the mutant makes the named test wait until the timeout panic.
    timeout: str = GO_TEST_TIMEOUT


def one(name: str, group: str, path: str, old: str, new: str, killers: tuple[Killer, ...], reason: str,
        timeout: str = GO_TEST_TIMEOUT) -> Mutant:
    return Mutant(name, group, (Edit(path, old, new),), killers, reason, timeout)


MUTANTS: list[Mutant] = [
    # ---------------------------------------------------------------- council-acceptance
    one("council-threshold-minus-one", COUNCIL, "chain/x/authority/engine.go",
        "\tif len(rec.Approvers) >= ApprovedThreshold {",
        "\tif len(rec.Approvers) >= ApprovedThreshold-1 {",
        K("chain/app", "TestInsufficientApproval", "TestDuplicateVotes", "TestUnauthorizedPolicyWritesFailAtExecution") + K("chain/x/authority", "TestKeeperInsufficientApproval") + K("chain/x/authority/difftest", "TestCouncilDifferential"),
        "a change needs three distinct seat approvals (ApprovedThreshold); two must not queue it"),
    one("council-review-delay-plus-one", COUNCIL, "chain/x/authority/engine.go",
        "\tif s.Time < rec.ExecuteAfterUnixSeconds {",
        "\tif s.Time+1 < rec.ExecuteAfterUnixSeconds {",
        K("chain/app", "TestActivationTiming") + K("chain/x/authority", "TestKeeperExpiryAndReviewDelayBoundaries") + K("chain/x/authority/difftest", "TestExpiryAndDelayBoundariesDifferential"),
        "execute is OUT_OF_RANGE until the block time reaches execute_after (DEC-008 review delay), and allowed at exactly execute_after"),
    one("council-approve-expiry-strict", COUNCIL, "chain/x/authority/engine.go",
        "\tif s.Time >= rec.ExpiryUnixSeconds { // 1470-1472",
        "\tif s.Time > rec.ExpiryUnixSeconds { // 1470-1472",
        K("chain/x/authority", "TestKeeperExpiryAndReviewDelayBoundaries") + K("chain/x/authority/difftest", "TestExpiryAndDelayBoundariesDifferential"),
        "an approval at exactly the proposal expiry is EXPIRED (spec 1470-1472)"),
    one("council-execute-expiry-strict", COUNCIL, "chain/x/authority/engine.go",
        "\tif s.Time >= rec.ExpiryUnixSeconds {\n\t\treturn s.keepAs(rec, authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED, CodeExpired)\n\t}\n\tif rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {",
        "\tif s.Time > rec.ExpiryUnixSeconds {\n\t\treturn s.keepAs(rec, authorityv1.ProposalState_PROPOSAL_STATE_EXPIRED, CodeExpired)\n\t}\n\tif rec.state() != authorityv1.ProposalState_PROPOSAL_STATE_QUEUED {",
        K("chain/x/authority", "TestKeeperExpiryAndReviewDelayBoundaries") + K("chain/x/authority/difftest", "TestExpiryAndDelayBoundariesDifferential"),
        "an execute at exactly the proposal expiry is EXPIRED (spec 1502-1504)"),
    one("council-duplicate-approval-guard-off", COUNCIL, "chain/x/authority/engine.go",
        "\tif slices.Contains(rec.Approvers, seat.GetSeatId()) { // 1473",
        "\tif false && slices.Contains(rec.Approvers, seat.GetSeatId()) { // 1473",
        K("chain/app", "TestDuplicateVotes") + K("chain/x/authority", "TestKeeperDuplicateVotes") + K("chain/x/authority/difftest", "TestCouncilDifferential"),
        "a second approval by the same seat is REPLAY and does not count toward the threshold"),
    one("council-execute-stale-off", COUNCIL, "chain/x/authority/engine.go",
        "\tif rec.BasePolicyVersion != version {\n\t\tprev",
        "\tif false && rec.BasePolicyVersion != version {\n\t\tprev",
        K("chain/app", "TestConcurrentProposals", "TestUpgradeRehearsalInProcess") + K("chain/x/authority", "TestKeeperConcurrentProposalsOnOneKey") + K("chain/x/authority/difftest", "TestStaleBranchDifferential"),
        "an execute whose base policy version is not the current one is STALE (or CONFLICT), including after an upgrade bump (DR-10-38)"),
    one("council-propose-stale-off", COUNCIL, "chain/x/authority/engine.go",
        "\tif msg.GetBasePolicyVersion() != version { // 1436",
        "\tif false && msg.GetBasePolicyVersion() != version { // 1436",
        K("chain/x/authority", "TestBumpPolicyVersionForUpgrade", "TestGovernedLimitTypedAtProposeD1") + K("chain/x/authority/difftest", "TestCouncilDifferential"),
        "a proposal on a base policy version that is not the current one is STALE (spec 1436)"),
    one("council-authority-role-bypass", COUNCIL, "chain/x/authority/engine.go",
        "\tif role := s.role(msg.GetSigner()); role != typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY { // check_role 1490",
        "\tif role := s.role(msg.GetSigner()); false && role != typesv1.SubmitterRole_SUBMITTER_ROLE_AUTHORITY { // check_role 1490",
        K("chain/app", "TestUnauthorizedPolicyWritesFailAtExecution") + K("chain/x/authority", "TestKeeperUnauthorizedWritesFailAtExecution") + K("chain/x/authority/difftest", "TestNonAuthorityExecuteDifferential"),
        "only the AUTHORITY submitter may execute a queued change (spec 1490)"),
    one("council-propose-seat-bypass", COUNCIL, "chain/x/authority/engine.go",
        "\tseat := s.seatOf(msg.GetSigner())\n\tif seat == nil { // 1411\n\t\treturn fail(CodeUnauthorized)\n\t}",
        "\tseat := s.seatOf(msg.GetSigner())\n\tif seat == nil { // 1411\n\t\tseat = s.auth().GetDevnetTestSeats()[0]\n\t}",
        K("chain/app", "TestUnauthorizedPolicyWritesFailAtExecution") + K("chain/x/authority", "TestKeeperUnauthorizedWritesFailAtExecution") + K("chain/x/authority/difftest", "TestCouncilDifferential"),
        "only a council seat may propose (spec 1411)"),
    one("council-approve-seat-bypass", COUNCIL, "chain/x/authority/engine.go",
        "\tseat := s.seatOf(msg.GetSigner())\n\tif seat == nil { // 1460\n\t\treturn fail(CodeUnauthorized)\n\t}",
        "\tseat := s.seatOf(msg.GetSigner())\n\tif seat == nil { // 1460\n\t\tseat = s.auth().GetDevnetTestSeats()[0]\n\t}",
        K("chain/app", "TestUnauthorizedPolicyWritesFailAtExecution") + K("chain/x/authority", "TestKeeperUnauthorizedWritesFailAtExecution") + K("chain/x/authority/difftest", "TestCouncilDifferential"),
        "only a council seat may approve (spec 1460); a removed seat's key cannot approve"),
    one("ported-07-msg-success-skips-registry-write", COUNCIL, "chain/x/authority/msg.go",
        "\tcase out.Success():\n\t\twriteRegistry()\n",
        "\tcase out.Success():\n\t\t_ = writeRegistry\n",
        K("chain/x/authority", "TestKeeperRegistryHookCommitsOnlyOnSuccess") + K("chain/app", "TestGovernedLimitsReachTheRegistries"),
        "a successful execute must write the registry branch (goal 07 msg-success-skips-registry-write, ported)"),
    # ---------------------------------------------------------------- authority
    one("p2a-d1-propose-removed", AUTHORITY, "chain/x/authority/engine.go",
        "\tif kind == kindGovernedLimit && !governedInRange(",
        "\tif false && kind == kindGovernedLimit && !governedInRange(",
        K("chain/x/authority", "TestGovernedLimitTypedAtProposeD1", "TestKeeperInvalidBounds") + K("chain/app", "TestInvalidBounds"),
        "D-1: a governed limit outside the msgkit table or above its ceiling is BOUNDS at propose"),
    one("p2a-ceiling-inclusive-to-exclusive", AUTHORITY, "chain/x/authority/policy.go",
        "value <= ceiling",
        "value < ceiling",
        K("chain/x/authority", "TestGovernedLimitTypedAtProposeD1", "TestKeeperInvalidBounds") + K("chain/app", "TestEveryGovernedKeyAtFloorAndCeiling"),
        "a governed value equal to its ceiling is accepted"),
    one("ported-06-ceiling-gone", AUTHORITY, "chain/x/authority/policy.go",
        "\treturn known && value >= 1 && value <= ceiling\n",
        "\treturn known && value >= 1 && ceiling > 0\n",
        K("chain/x/authority", "TestGovernedLimitTypedAtProposeD1", "TestKeeperInvalidBounds", "TestExecuteRechecksCeiling") + K("chain/app", "TestInvalidBounds"),
        "a limit value above its bounds ceiling must be rejected (goal 06 ceiling-gone, ported)"),
    one("ported-06-unknown-limit-ok", AUTHORITY, "chain/x/authority/policy.go",
        "\treturn known && value >= 1 && value <= ceiling\n",
        "\treturn (!known && value >= 1) || (value >= 1 && value <= ceiling)\n",
        K("chain/x/authority", "TestGovernedLimitTypedAtProposeD1", "TestKeeperInvalidBounds") + K("chain/app", "TestInvalidBounds"),
        "an unknown limit key must be rejected (goal 06 unknown-limit-ok, ported)"),
    Mutant("rev-d1-after-expiry-check", AUTHORITY, (
        Edit("chain/x/authority/engine.go",
             "\tif kind == kindGovernedLimit && !governedInRange(decoded.LimitKey, decoded.LimitValue) {\n\t\treturn fail(CodeBounds)\n\t}\n",
             ""),
        Edit("chain/x/authority/engine.go",
             "\tif msg.GetExpiryUnixSeconds() <= s.Time { // 1434\n\t\treturn fail(CodeExpired)\n\t}\n",
             "\tif msg.GetExpiryUnixSeconds() <= s.Time { // 1434\n\t\treturn fail(CodeExpired)\n\t}\n\tif kind == kindGovernedLimit && !governedInRange(decoded.LimitKey, decoded.LimitValue) {\n\t\treturn fail(CodeBounds)\n\t}\n"),
    ), K("chain/x/authority", "TestGovernedLimitTypedAtProposeD1") + K("chain/x/authority/difftest", "TestCouncilDifferential", "TestDifferentialReachability"),
        "D-1 runs right after validated_payload, before check_time, the business gate, duplicate id and expiry (W0 test-honesty review, ported)"),
    one("p2b-propose-lead-gone", AUTHORITY, "chain/x/authority/engine.go",
        "\tif code := decoded.leadCode(s.Height); code != \"\" {\n\t\treturn fail(code)\n\t}\n\tif msg.GetExpiryUnixSeconds()",
        "\tif code := decoded.leadCode(s.Height); false && code != \"\" {\n\t\treturn fail(code)\n\t}\n\tif msg.GetExpiryUnixSeconds()",
        K("chain/x/authority", "TestKeeperLeadBoundaries", "TestScheduledGovernedLimitAtPropose", "TestSoftwareUpgradeAtPropose"),
        "propose checks the activation / plan-height window (D-4, D-5)"),
    one("p2b-exec-governed-lead-gone", AUTHORITY, "chain/x/authority/engine.go",
        "\t\tif code := decoded.leadCode(s.Height); code != \"\" {\n\t\t\treturn fail(code)\n\t\t}\n\t\tnext, fits",
        "\t\tif code := decoded.leadCode(s.Height); false && code != \"\" {\n\t\t\treturn fail(code)\n\t\t}\n\t\tnext, fits",
        K("chain/x/authority", "TestScheduledGovernedLimitAtExecute"),
        "execute re-checks a scheduled governed limit's activation window against its own block"),
    one("p2b-exec-upgrade-lead-gone", AUTHORITY, "chain/x/authority/engine.go",
        "\t\tif code := decoded.leadCode(s.Height); code != \"\" {\n\t\t\treturn fail(code)\n\t\t}\n\t\tif s.Policy.UpgradeApplied",
        "\t\tif code := decoded.leadCode(s.Height); false && code != \"\" {\n\t\t\treturn fail(code)\n\t\t}\n\t\tif s.Policy.UpgradeApplied",
        K("chain/x/authority", "TestSoftwareUpgradeAtExecute"),
        "execute re-checks the plan height window against its own block"),
    one("p2b-min-lead-1", AUTHORITY, "chain/x/authority/policy.go",
        "heightLead(d.UpgradeHeight, now, msgkit.MinActivationLeadBlocks)",
        "heightLead(d.UpgradeHeight, now, 1)",
        K("chain/x/authority", "TestKeeperLeadBoundaries", "TestSoftwareUpgradeAtExecute", "TestSoftwareUpgradeAtPropose"),
        "a plan height needs at least MinActivationLeadBlocks (100, PROPOSED) of lead"),
    one("p2b-max-lead-plus-1", AUTHORITY, "chain/x/authority/policy.go",
        "target-base > msgkit.MaxActivationLeadBlocks ||",
        "target-base > msgkit.MaxActivationLeadBlocks+1 ||",
        K("chain/x/authority", "TestKeeperLeadBoundaries", "TestScheduledGovernedLimitAtExecute"),
        "a lead above MaxActivationLeadBlocks (1,000,000, PROPOSED) is BOUNDS"),
    one("p2b-max-lead-minus-1", AUTHORITY, "chain/x/authority/policy.go",
        "target-base > msgkit.MaxActivationLeadBlocks ||",
        "target-base >= msgkit.MaxActivationLeadBlocks ||",
        K("chain/x/authority", "TestKeeperLeadBoundaries", "TestScheduledGovernedLimitAtExecute"),
        "a lead of exactly MaxActivationLeadBlocks is accepted"),
    one("intb-lead-min-off-by-one", AUTHORITY, "chain/x/authority/policy.go",
        "\tif target <= base || target-base < minLead {",
        "\tif target <= base || target-base+1 < minLead {",
        K("chain/app", "TestInvalidBounds") + K("chain/x/authority", "TestKeeperLeadBoundaries"),
        "a lead of MinActivationLeadBlocks-1 is OUT_OF_RANGE through committed transactions"),
    one("p2b-safe-max-gone", AUTHORITY, "chain/x/authority/policy.go",
        " || target > uint64(safeMax) {",
        " {",
        K("chain/x/authority", "TestHeightWindowSafeIntegerCap"),
        "a target height above 2^53-1 is BOUNDS"),
    one("p2b-applied-check-gone", AUTHORITY, "chain/x/authority/engine.go",
        "\t\tif s.Policy.UpgradeApplied(decoded.UpgradeName) {",
        "\t\tif false && s.Policy.UpgradeApplied(decoded.UpgradeName) {",
        K("chain/x/authority", "TestSoftwareUpgradeAtExecute"),
        "a plan with an applied upgrade name is DUPLICATE_ID at execute"),
    one("p2b-pending-version-not-next", AUTHORITY, "chain/x/authority/engine.go",
        "decoded.Activation, rec.ProposalID, next))",
        "decoded.Activation, rec.ProposalID, next-1))",
        K("chain/x/authority", "TestActivationHeightEffectiveD4", "TestScheduledGovernedLimitAtExecute", "TestImmediateChangeSupersedesPending"),
        "a scheduled change stores the policy version the execute produces"),
    one("p2b-policy-on-message-branch", AUTHORITY, "chain/x/authority/msg.go",
        "\tst.Policy = k.policyWriter(registryCtx)",
        "\tst.Policy = k.policyWriter(cacheCtx)",
        K("chain/x/authority", "TestHookWriteDroppedOnRejectedKeep"),
        "the policy writer is bound to the registry branch, dropped on a rejected keep"),
    one("p2b-accounts-on-message-branch", AUTHORITY, "chain/x/authority/msg.go",
        "\tst.Accounts = k.accountCreator(registryCtx)",
        "\tst.Accounts = k.accountCreator(cacheCtx)",
        K("chain/x/authority", "TestHookWriteDroppedOnRejectedKeep"),
        "the account creator is bound to the registry branch, dropped on a rejected keep"),
    one("p2b-governed-recheck-gone", AUTHORITY, "chain/x/authority/engine.go",
        "\t\tif !governedInRange(decoded.LimitKey, decoded.LimitValue) {\n\t\t\treturn fail(CodeBounds)\n\t\t}\n\t\tif decoded.Activation == 0",
        "\t\tif decoded.Activation == 0",
        K("chain/x/authority", "TestExecuteRechecksCeiling", "TestSafetyCapsNotGovernable"),
        "execute re-checks the governed key and ceiling"),
    one("p2b-upgrade-digest-not-passed", AUTHORITY, "chain/x/authority/engine.go",
        "\t\t\tbytes.Clone(decoded.UpgradeDigest), rec.ProposalID))",
        "\t\t\tnil, rec.ProposalID))",
        K("chain/x/authority", "TestCancelUpgradePayload", "TestExecuteEmitsPolicyEventsD2", "TestNoPayloadApprovesDecision"),
        "the executed plan carries the payload's release digest"),
    one("p2a-d7-execute-removed", AUTHORITY, "chain/x/authority/engine.go",
        "\tif s.councilKey(change.PubKey) {\n\t\treturn fail(CodeDuplicate)\n\t}",
        "",
        K("chain/x/authority", "TestSeatKeyIsNotAValidatorKeyD7"),
        "D-7: a validator change naming a council seat key is DUPLICATE_ID at execute"),
    one("p2a-d7-retired-ignored", AUTHORITY, "chain/x/authority/engine.go",
        "\treturn s.kv.Has(retiredCouncilKey(pub))\n}",
        "\treturn false\n}",
        K("chain/x/authority", "TestSeatKeyIsNotAValidatorKeyD7"),
        "D-7: a retired council key stays unusable as a validator key"),
    one("p2a-d7-rotation-removed", AUTHORITY, "chain/x/authority/engine.go",
        "\tif consensus {\n\t\treturn fail(CodeDuplicate)\n\t}",
        "\tif consensus && false {\n\t\treturn fail(CodeDuplicate)\n\t}",
        K("chain/x/authority", "TestSeatKeyIsNotAValidatorKeyD7"),
        "D-7: a seat key that is a validator consensus key is DUPLICATE_ID"),
    one("p2a-d7-rotation-pending-ignored", AUTHORITY, "chain/x/authority/engine.go",
        "\tfor _, item := range pending {\n\t\tif bytes.Equal(item.GetConsensusPubkey(), pub) {\n\t\t\treturn true, ok()\n\t\t}\n\t}\n\treturn false, ok()",
        "\t_ = pending\n\treturn false, ok()",
        K("chain/x/authority", "TestSeatKeyIsNotAValidatorKeyD7"),
        "D-7: a seat key named by a pending validator change is DUPLICATE_ID"),
    one("p2a-d7-genesis-removed", AUTHORITY, "chain/x/authority/genesis.go",
        "\tif err := validateSeatKeys(auth.GetDevnetTestSeats(), auth.GetValidators(), pending); err != nil {\n\t\treturn err\n\t}\n",
        "",
        K("chain/x/authority", "TestSeatKeyIsNotAValidatorKeyD7"),
        "D-7: genesis refuses a seat key that is a validator key"),
    one("p2a-d10-cap-64", AUTHORITY, "chain/x/authority/genesis.go",
        "const MaxSeatIDLength = 59",
        "const MaxSeatIDLength = 64",
        K("chain/x/authority", "TestSeatIDLengthAtGenesisD10"),
        "D-10: a seat id is at most 59 characters at genesis"),
    one("p2a-d2-old-version", AUTHORITY, "chain/x/authority/engine.go",
        "parameterKey: rec.ParameterKey, version: version + 1}",
        "parameterKey: rec.ParameterKey, version: version}",
        K("chain/x/authority", "TestExecuteEmitsPolicyEventsD2"),
        "D-2: PolicyChanged carries the new policy version"),
    one("p2a-d2-no-proposal-executed", AUTHORITY, "chain/x/authority/events.go",
        "\treturn msgkit.EmitTyped(ctx, &eventsv1.ProposalExecuted{\n\t\tProposalId:    done.proposalID,\n\t\tPolicyVersion: done.version,\n\t})",
        "\treturn nil",
        K("chain/x/authority", "TestExecuteEmitsPolicyEventsD2"),
        "D-2: a successful execute emits ProposalExecuted"),
    one("p2a-d2-emit-on-keep", AUTHORITY, "chain/x/authority/msg.go",
        "\tcase out.Keep:\n\t\twrite()",
        "\tcase out.Keep:\n\t\t_ = emitExecuted(sdkCtx, &executedChange{proposalID: \"x\", parameterKey: \"x\", version: 1})\n\t\twrite()",
        K("chain/x/authority", "TestExecuteEmitsPolicyEventsD2"),
        "D-2: a keep outcome emits no policy event"),
    one("p2b-ensure-account-gone", AUTHORITY, "chain/x/authority/engine.go",
        "\t\tif out := registryOutcome(s.Accounts.EnsureAccount(SeatAddress(decoded.SeatKey))); !out.Success() {\n\t\t\treturn out\n\t\t}\n",
        "",
        K("chain/x/authority", "TestCouncilSeatAtExecute", "TestCouncilSeatPayload"),
        "a seat payload creates the new key's account"),
    one("p2b-seat-config-not-installed", AUTHORITY, "chain/x/authority/engine.go",
        "\t\ts.config, s.seatConfig = s.seatConfig, nil\n",
        "\t\ts.seatConfig = nil\n",
        K("chain/x/authority", "TestCouncilSeatAtExecute"),
        "a successful seat payload installs the staged config"),
    one("p2b-seat-config-not-reset", AUTHORITY, "chain/x/authority/engine.go",
        "\ts.seatConfig = nil // only this execute's payload may stage a config\n",
        "",
        K("chain/x/authority", "TestFailedSeatExecuteKeepsTheStateConfig"),
        "only this execute's seat payload may stage a config"),
    one("p2b-seat-order-old", AUTHORITY, "chain/x/authority/engine.go",
        "\tif seat == nil {\n\t\treturn fail(CodeNotFound)\n\t}\n\tfor _, row := range cfg.GetAuthority().GetDevnetTestSeats() {\n\t\tif row == seat {\n\t\t\tcontinue\n\t\t}\n\t\tif bytes.Equal(row.GetPublicKey(), newPub) {\n\t\t\treturn fail(CodeDuplicate)\n\t\t}\n\t}",
        "\tfor _, row := range cfg.GetAuthority().GetDevnetTestSeats() {\n\t\tif row == seat {\n\t\t\tcontinue\n\t\t}\n\t\tif bytes.Equal(row.GetPublicKey(), newPub) {\n\t\t\treturn fail(CodeDuplicate)\n\t\t}\n\t}\n\tif seat == nil {\n\t\treturn fail(CodeNotFound)\n\t}",
        K("chain/x/authority", "TestCouncilSeatAtExecute", "TestRotateCouncilSeatProtocolOrder"),
        "an unknown seat is NOT_FOUND before the duplicate-key check"),
    one("intb-retired-key-accepted", AUTHORITY, "chain/x/authority/engine.go",
        "\tif bytes.Equal(seat.GetPublicKey(), newPub) || s.kv.Has(retiredCouncilKey(newPub)) {",
        "\tif bytes.Equal(seat.GetPublicKey(), newPub) {",
        K("chain/app", "TestRemovedCouncilSeat", "TestRejectedMessagesLeaveTheFullAuthorityState"),
        "a retired council key cannot become a seat key again (CONFLICT)"),
    one("plan-seat-old-key-not-retired", AUTHORITY, "chain/x/authority/engine.go",
        "\tb.set(retiredCouncilKey(old), []byte(seatID))\n",
        "\t_ = old\n",
        K("chain/x/authority", "TestCouncilSeatAtExecute", "TestSeatKeyIsNotAValidatorKeyD7") + K("chain/app", "TestRemovedCouncilSeat"),
        "a seat replacement retires the old seat key (plan 16.9 seats: skip retiring the old key)"),
    one("plan-seat-account-before-checks", AUTHORITY, "chain/x/authority/engine.go",
        "\t\tcfg := proto.Clone(s.config).(*authorityv1.GenesisState)\n\t\tif out := s.replaceSeat(b, cfg, decoded.SeatID, decoded.SeatKey); !out.Success() {\n\t\t\treturn out\n\t\t}\n\t\tif out := registryOutcome(s.Accounts.EnsureAccount(SeatAddress(decoded.SeatKey))); !out.Success() {\n\t\t\treturn out\n\t\t}\n",
        "\t\tcfg := proto.Clone(s.config).(*authorityv1.GenesisState)\n\t\tif out := registryOutcome(s.Accounts.EnsureAccount(SeatAddress(decoded.SeatKey))); !out.Success() {\n\t\t\treturn out\n\t\t}\n\t\tif out := s.replaceSeat(b, cfg, decoded.SeatID, decoded.SeatKey); !out.Success() {\n\t\t\treturn out\n\t\t}\n",
        K("chain/x/authority", "TestCouncilSeatAtExecute"),
        "a seat payload creates the new key's account only after every seat check passed (plan 16.9 seats: EnsureAccount before the payload checks)"),
    one("plan-decisions-seat-payload-approves", AUTHORITY, "chain/x/authority/engine.go",
        "\tseat.PublicKey = bytes.Clone(newPub)\n\traw, err := marshalState(cfg)\n",
        "\tseat.PublicKey = bytes.Clone(newPub)\n\tfor _, row := range cfg.GetBlockedDecisions() {\n\t\trow.Approved = true\n\t}\n\traw, err := marshalState(cfg)\n",
        K("chain/x/authority", "TestNoPayloadApprovesDecision"),
        "no council payload approves a blocked decision row (plan 16.9 decisions)"),
    one("p2a-query-first-item-free", AUTHORITY, "chain/x/authority/query.go",
        "\tif p.count == p.limit {",
        "\tif p.count == p.limit+1 {",
        K("chain/x/authority", "TestGovernanceQueryPaging"),
        "Query/Governance pages are bounded by the page limit"),
    one("p2a-query-pending-index-unchecked", AUTHORITY, "chain/x/authority/query.go",
        "\tif cursor.section == sectionPending && cursor.index >= len(pending) {\n\t\treturn nil, msgkit.Bounds.QueryErr()\n\t}",
        "",
        K("chain/x/authority", "TestGovernanceQueryRejects"),
        "a pending-section cursor beyond the list is BOUNDS"),
    one("p2a-query-limit-ignores-governed", AUTHORITY, "chain/x/authority/query.go",
        "\treturn uint32(msgkit.Effective(msgkit.LimitMaxPageLimit, governed, found))",
        "\t_, _ = governed, found\n\treturn 0",
        K("chain/x/authority", "TestGovernanceQueryPaging"),
        "the page limit reads the governed max_page_limit"),
    one("rev-query-seat-cursor-skips-item", AUTHORITY, "chain/x/authority/query.go",
        "if row.GetSeatId() < string(cursor.key) {",
        "if row.GetSeatId() <= string(cursor.key) {",
        K("chain/x/authority", "TestGovernanceQueryPaging"),
        "the seat cursor is inclusive: paging returns every seat once (W0 review)"),
    one("p2b-page-limit-ceiling-fallback", AUTHORITY, "chain/x/authority/query.go",
        "\tif k.policy == nil {\n\t\treturn 0, fmt.Errorf(\"authority governance query: no policy keeper is wired (WithPolicy)\")\n\t}\n\tgoverned, found := k.policy.GovernedLimit(ctx, msgkit.LimitMaxPageLimit)",
        "\tif k.policy == nil {\n\t\treturn uint32(msgkit.Effective(msgkit.LimitMaxPageLimit, 0, false)), nil\n\t}\n\tgoverned, found := k.policy.GovernedLimit(ctx, msgkit.LimitMaxPageLimit)",
        K("chain/x/authority", "TestGovernanceQueryReadsPolicy"),
        "without a policy keeper the governance query fails instead of using the ceiling"),
    one("p2b-devnet-rows-any-chain", AUTHORITY, "chain/x/authority/engine.go",
        "func (s *State) devnetRows() []*typesv1.DevnetTestParameter {\n\tif !strings.HasPrefix(s.ChainID, genesis.DevnetChainIDPrefix) {",
        "func (s *State) devnetRows() []*typesv1.DevnetTestParameter {\n\tif false && !strings.HasPrefix(s.ChainID, genesis.DevnetChainIDPrefix) {",
        K("chain/x/authority", "TestGovernanceDevnetTestParameters"),
        "devnet test parameters are reported only on a devnet chain id"),
    one("p2b-devnet-rows-no-label-filter", AUTHORITY, "chain/x/authority/engine.go",
        "\t\tif row.GetLabel() == genesis.DevnetTestLabel && !row.GetApprovedPolicy() {\n\t\t\tout = append(",
        "\t\tif true {\n\t\t\tout = append(",
        K("chain/x/authority", "TestGovernanceDevnetTestParameters"),
        "only labelled, unapproved devnet rows are reported"),
    one("p2a-bump-plus-two", AUTHORITY, "chain/x/authority/upgrade.go",
        "encodeU64(version+1)",
        "encodeU64(version+2)",
        K("chain/x/authority", "TestBumpPolicyVersionForUpgrade"),
        "an upgrade bumps the policy version by exactly one (DR-10-38)"),
    one("p2a-bump-no-overflow-check", AUTHORITY, "chain/x/authority/upgrade.go",
        "\tif version == math.MaxUint64 {\n\t\treturn sdkError(CodeOverflow)\n\t}",
        "\tif false && version == math.MaxUint64 {\n\t\treturn sdkError(CodeOverflow)\n\t}",
        K("chain/x/authority", "TestBumpPolicyVersionForUpgrade"),
        "the bump at 2^64-1 is OVERFLOW"),
    one("rev-bump-writes-on-damaged-version", AUTHORITY, "chain/x/authority/upgrade.go",
        "\tif !out.Success() {\n\t\treturn sdkError(out.Code)\n\t}",
        "\tif !out.Success() {\n\t\tversion = 1\n\t}",
        K("chain/x/authority", "TestBumpPolicyVersionForUpgrade"),
        "the bump refuses a damaged policy version instead of writing (W0 review)"),
    one("p2a-writer-called-outside-apply", AUTHORITY, "chain/x/authority/engine.go",
        "\ts.seatConfig = nil // only this execute's payload may stage a config\n\tif applied := s.applyPayload(&b, rec, version); !applied.Success() {",
        "\ts.seatConfig = nil // only this execute's payload may stage a config\n\tif s.Policy != nil {\n\t\t_ = s.Policy.UpgradeApplied(\"x\")\n\t}\n\tif applied := s.applyPayload(&b, rec, version); !applied.Success() {",
        K("chain/x/authority", "TestPolicyWriterCallSites"),
        "the policy writer is called only from applyPayload (P2a, re-anchored)"),
    one("p2b-no-layout-key", AUTHORITY, "chain/x/authority/store.go",
        "\twrites := []write{{[]byte{layoutKey}, encodeU64(ConsensusVersion)}}\n",
        "\tvar writes []write\n",
        K("chain/x/authority", "TestLayoutKeyAndConsensusVersion"),
        "authority genesis writes layout key 2"),
    one("p2b-export-any-layout", AUTHORITY, "chain/x/authority/store.go",
        "!good || version != ConsensusVersion {",
        "!good || version == 0 {",
        K("chain/x/authority", "TestLayoutKeyAndConsensusVersion"),
        "export refuses a store whose layout is not ConsensusVersion"),
    # ---------------------------------------------------------------- payload
    one("ported-06-swap-engine-282", PAYLOAD, "chain/x/authority/engine.go",
        "\tif msg.GetParameterKey() != parameterKey(decoded) {\n\t\treturn fail(CodeMalformed)\n",
        "\tif msg.GetParameterKey() != parameterKey(decoded) {\n\t\treturn fail(CodeQuorum)\n",
        K("chain/x/authority/difftest", "TestCouncilDifferential"),
        "a parameter key that does not match the payload is MALFORMED (goal 06 swap-engine-282, ported)"),
    one("p2b-canon-drops-activation", PAYLOAD, "chain/x/authority/payload.go",
        "\t\tinner[\"activationHeight\"] = strconv.FormatUint(body.GetActivationHeight(), 10)\n",
        "",
        K("chain/x/authority", "TestPayloadVectors", "TestScheduledGovernedLimitAtPropose"),
        "the activation height is part of the canonical payload text"),
    one("plan-payload-v0-text-changed", PAYLOAD, "chain/x/authority/payload.go",
        "\tif body.GetActivationHeight() != 0 {\n\t\tinner[\"activationHeight\"]",
        "\tif true {\n\t\tinner[\"activationHeight\"]",
        K("chain/x/authority", "TestPayloadVectors"),
        "a v0 governed limit keeps its canonical text and digest (Q1 (a): existing payload digests unchanged)"),
    one("p2b-upgrade-param-key", PAYLOAD, "chain/x/authority/payload.go",
        "\tcase kindSoftwareUpgrade, kindCancelUpgrade:\n\t\treturn upgradeParameterKey",
        "\tcase kindSoftwareUpgrade, kindCancelUpgrade:\n\t\treturn d.UpgradeName",
        K("chain/x/authority", "TestCancelUpgradeAtPropose", "TestCancelUpgradePayload"),
        "upgrade and cancel payloads bind the fixed upgrade parameter key"),
    one("p2b-seat-param-key", PAYLOAD, "chain/x/authority/payload.go",
        "\t\treturn \"seat-\" + d.SeatID",
        "\t\treturn d.SeatID",
        K("chain/x/authority", "TestCouncilSeatAtPropose", "TestCouncilSeatPayload"),
        "a seat payload binds the parameter key seat-<id>"),
    one("p2b-seat-key-not-decoded", PAYLOAD, "chain/x/authority/engine.go",
        "\t\tout.SeatKey = bytes.Clone(body.GetNewPublicKey())\n",
        "",
        K("chain/x/authority", "TestCouncilSeatAtPropose", "TestCouncilSeatPayload"),
        "the decoded seat payload carries the new public key"),
    one("p2b-upgrade-digest-len-unchecked", PAYLOAD, "chain/x/authority/payload.go",
        "\tdigest, out := digestField(body.GetReleaseDigest())\n\tif !out.Success() {\n\t\treturn \"\", out\n\t}\n\tinner := map[string]any{\n\t\t\"name\"",
        "\tdigest := body.GetReleaseDigest().GetValue()\n\tinner := map[string]any{\n\t\t\"name\"",
        K("chain/x/authority", "TestSoftwareUpgradeAtPropose"),
        "a release digest must name SHA-256 and be 32 bytes"),
    one("p4c-chain-withdraw-reason-first", PAYLOAD, "chain/x/authority/payload.go",
        "\tif body == nil || !idPattern.MatchString(body.GetContentId()) || body.GetContentVersion() < 1 {\n\t\tif body != nil && body.GetContentId() != \"\" && !idPattern.MatchString(body.GetContentId()) {",
        "\tif body != nil && body.GetReasonDigest() == nil {\n\t\treturn \"\", fail(CodeMalformed)\n\t}\n\tif body == nil || !idPattern.MatchString(body.GetContentId()) || body.GetContentVersion() < 1 {\n\t\tif body != nil && body.GetContentId() != \"\" && !idPattern.MatchString(body.GetContentId()) {",
        K("chain/x/authority/difftest", "TestCouncilRulesAreLoadBearing", "TestUnnamedParseDivergences"),
        "force-withdraw checks the content id before the reason digest (the G-11 shape space sees a reordering)"),
    one("p4c-chain-power-before-activation", PAYLOAD, "chain/x/authority/payload.go",
        "\tact, actJSON, out := activationJSON(body.GetActivation())",
        "\tif body.GetPower() < 0 {\n\t\treturn PowerChange{}, \"\", fail(CodeBounds)\n\t}\n\tact, actJSON, out := activationJSON(body.GetActivation())",
        K("chain/x/authority/difftest", "TestUnnamedParseDivergences", "TestShapeRuleNearMisses"),
        "validator power is checked after the activation (the shape space and near misses see a reordering)"),
    one("p4c-chain-governed-value-malformed", PAYLOAD, "chain/x/authority/payload.go",
        "\tif body == nil || !idPattern.MatchString(body.GetLimitKey()) || body.GetValue() < 1 {",
        "\tif body != nil && body.GetValue() < 1 {\n\t\treturn \"\", fail(CodeMalformed)\n\t}\n\tif body == nil || !idPattern.MatchString(body.GetLimitKey()) || body.GetValue() < 1 {",
        K("chain/x/authority/difftest", "TestCouncilRulesAreLoadBearing", "TestUnnamedParseDivergences"),
        "a governed value of 0 is BOUNDS (G-11), not MALFORMED"),
    # ---------------------------------------------------------------- policy
    one("p1b-apply-strict", POLICY, "chain/x/policy/apply.go",
        "if row.Activation > now {",
        "if row.Activation >= now {",
        K("chain/x/policy", "TestApplyDueOrderAndBounds", "TestGovernedChangeSequenceReachesEveryOutcome") + K("chain/app", "TestActivationTiming", "TestLeaseAtActivationMinusOne"),
        "ApplyDue applies a pending change at its activation height a, not at a+1 (D-4)"),
    one("plan-apply-one-block-early", POLICY, "chain/x/policy/apply.go",
        "if row.Activation > now {",
        "if row.Activation > now+1 {",
        K("chain/x/policy", "TestApplyDueOrderAndBounds") + K("chain/app", "TestActivationTiming", "TestLeaseAtActivationMinusOne"),
        "a pending change is not in force at a-1 (D-4)"),
    one("p1b-apply-keep-row", POLICY, "chain/x/policy/apply.go",
        "\t\tstore.Delete(PendingKey(row.Key))\n",
        "",
        K("chain/x/policy", "TestApplyDueOrderAndBounds", "TestGovernedChangeSequenceReachesEveryOutcome"),
        "an applied pending change loses its pending row"),
    one("p1b-apply-no-event", POLICY, "chain/x/policy/apply.go",
        "\t\temitActivated(ctx, row)\n",
        "",
        K("chain/x/policy", "TestApplyDueOrderAndBounds", "TestGovernedChangeSequenceReachesEveryOutcome"),
        "ApplyDue emits policy_activated for every change it applies"),
    one("intb-leases-before-policy", POLICY, "chain/app/app.go",
        "\tmm.SetOrderBeginBlockers(order...)",
        "\tmm.SetOrderBeginBlockers(authtypes.ModuleName, ModuleSVN, ModuleProviders, ModuleContent, ModuleLeases, ModulePolicy, ModuleEvidence, authority.ModuleName)",
        K("chain/app", "TestActivationGovernsSameBlockExpiryD8"),
        "x/policy's BeginBlock runs before x/leases, so a change in force at a governs a's expiry sweep (D-8)"),
    one("p1-setlimit-keeps-pending", POLICY, "chain/x/policy/writer.go",
        "\t\tstore.Delete(PendingKey(key))\n\t\temitPending(ctx, key, value, 0,",
        "\t\temitPending(ctx, key, value, 0,",
        K("chain/x/policy", "TestSetLimitSupersedesPending") + K("chain/app", "TestImmediateChangeSupersedesPending", "TestPolicyQueryThroughABCI"),
        "an immediate change deletes the pending change of the same key"),
    one("p1-activation-at-current-height", POLICY, "chain/x/policy/writer.go",
        "if target <= now {",
        "if target < now {",
        K("chain/x/policy", "TestSetLimitAndSchedulePending"),
        "an activation height at or below the current block is OUT_OF_RANGE"),
    one("p1-pending-version-order", POLICY, "chain/x/policy/writer.go",
        "if replaced && old.PolicyVersion >= policyVersion {",
        "if replaced && old.PolicyVersion > policyVersion {",
        K("chain/x/policy", "TestSetLimitAndSchedulePending"),
        "a replacement pending change needs a newer policy version"),
    one("p1b-plan-no-min-lead", POLICY, "chain/x/policy/writer.go",
        "if target <= now || target-now < msgkit.MinActivationLeadBlocks {",
        "if target <= now {",
        K("chain/x/policy", "TestGovernedChangeSequenceReachesEveryOutcome", "TestPlanScheduleReplaceCancel"),
        "a plan height below MinActivationLeadBlocks (100, PROPOSED) of lead is OUT_OF_RANGE"),
    one("plan-plan-past-height", POLICY, "chain/x/policy/writer.go",
        "if target <= now || target-now < msgkit.MinActivationLeadBlocks {",
        "if target-now < msgkit.MinActivationLeadBlocks {",
        K("chain/x/policy", "TestPlanScheduleReplaceCancel"),
        "a plan height below the current block is OUT_OF_RANGE, not BOUNDS (plan 16.9: accept a past height; P7 added the row \"height below the block\" to TestPlanScheduleReplaceCancel)"),
    one("p1b-upper-lead-off-by-one", POLICY, "chain/x/policy/writer.go",
        "if target-now > msgkit.MaxActivationLeadBlocks ||",
        "if target-now >= msgkit.MaxActivationLeadBlocks ||",
        K("chain/x/policy", "TestSetLimitAndSchedulePending", "TestPlanScheduleReplaceCancel"),
        "a lead of exactly MaxActivationLeadBlocks (1,000,000, PROPOSED) is accepted"),
    one("p1b-plan-name-no-conflict", POLICY, "chain/x/policy/writer.go",
        "if used && !bytes.Equal(first, digest) {",
        "if used && !bytes.Equal(first, digest) && false {",
        K("chain/x/policy", "TestGovernedChangeSequenceReachesEveryOutcome", "TestPlanScheduleReplaceCancel") + K("chain/app", "TestRejectedMessagesLeaveTheFullAuthorityState"),
        "a plan name used before with another digest is CONFLICT (Q3 (a) text; ORCH-10)"),
    one("p1b-plan-name-not-written", POLICY, "chain/x/policy/writer.go",
        "\tif !used {\n\t\tstore.Set(PlanNameKey(name), append([]byte(nil), digest...))\n\t}\n",
        "",
        K("chain/x/policy", "TestGovernedChangeSequenceReachesEveryOutcome", "TestPlanScheduleReplaceCancel"),
        "the first plan of a name writes its plan-name row (0x08)"),
    one("p1-applied-name-schedulable", POLICY, "chain/x/policy/writer.go",
        "\tif _, applied := must(loadApplied(store, name)); applied {\n\t\treturn msgkit.DuplicateID\n\t}",
        "",
        K("chain/x/policy", "TestPlanScheduleReplaceCancel"),
        "a plan with an applied name is DUPLICATE_ID"),
    one("plan-cancel-ignores-name", POLICY, "chain/x/policy/writer.go",
        "\tif !found || plan.Name != name {",
        "\tif !found {",
        K("chain/x/policy", "TestPlanScheduleReplaceCancel"),
        "cancel_upgrade of another name is NOT_FOUND and keeps the stored plan"),
    one("p1b-record-absent-plan-ok", POLICY, "chain/x/policy/writer.go",
        "\tif !planned {\n\t\treturn fmt.Errorf",
        "\tif false && !planned {\n\t\treturn fmt.Errorf",
        K("chain/x/policy", "TestRecordUpgrade"),
        "RecordUpgrade refuses an absent plan row (Q2 (a): an upgrade applies only from an executed council plan)"),
    one("p1-record-skips-plan-match", POLICY, "chain/x/policy/writer.go",
        "\tif plan.Name != name || plan.Height != height {",
        "\tif false {",
        K("chain/x/policy", "TestRecordUpgrade"),
        "RecordUpgrade refuses a stored plan of another name or height (P1 M9, re-anchored)"),
    one("p1-record-keeps-plan", POLICY, "chain/x/policy/writer.go",
        "\tstore.Set(AppliedKey(name), EncodeU64(height))\n\tstore.Delete(PlanKey())\n",
        "\tstore.Set(AppliedKey(name), EncodeU64(height))\n",
        K("chain/x/policy", "TestRecordUpgrade"),
        "RecordUpgrade deletes the plan it applied (P1 M10, re-anchored)"),
    one("rev-record-accepts-app-version-jump", POLICY, "chain/x/policy/writer.go",
        "if stored < 1 || stored == ^uint64(0) || appVersion != stored+1 {",
        "if stored < 1 || stored == ^uint64(0) || appVersion <= stored {",
        K("chain/x/policy", "TestRecordUpgrade"),
        "the app version moves by exactly one at an upgrade (W0 review)"),
    one("rev-record-keeps-dropped-module-rows", POLICY, "chain/x/policy/writer.go",
        "\t\tif _, kept := vm[name]; !kept {\n\t\t\tstore.Delete(VersionMapKey(name))\n\t\t}",
        "\t\t_ = name",
        K("chain/x/policy", "TestRecordUpgrade"),
        "a module dropped from the version map loses its row (W0 review)"),
    one("rev-record-any-height", POLICY, "chain/x/policy/writer.go",
        "if height < 1 || height != blockHeight(ctx) {",
        "if height < 1 {",
        K("chain/x/policy", "TestRecordUpgrade"),
        "RecordUpgrade records only at the block height (W0 review)"),
    one("p1b-initversions-plan-names-ok", POLICY, "chain/x/policy/writer.go",
        "{AppliedPrefix}, {PlanNamePrefix}} {",
        "{AppliedPrefix}} {",
        K("chain/x/policy", "TestInitVersions"),
        "InitVersions refuses a store that already holds a plan-name row"),
    one("p1b-genesis-app-version-ok", POLICY, "chain/x/policy/genesis.go",
        "case policy.GetAppVersion() != 0:",
        "case false:",
        K("chain/x/policy", "TestGenesisValidateImportExport"),
        "policy genesis carries no app_version (ORCH-9)"),
    one("p1b-genesis-plan-ok", POLICY, "chain/x/policy/genesis.go",
        "case policy.GetUpgradePlan() != nil:",
        "case false:",
        K("chain/x/policy", "TestGenesisValidateImportExport"),
        "policy genesis carries no upgrade plan"),
    one("p1b-genesis-pending-ok", POLICY, "chain/x/policy/genesis.go",
        "case len(policy.GetPendingLimitChanges()) != 0:",
        "case false:",
        K("chain/x/policy", "TestGenesisValidateImportExport"),
        "policy genesis carries no pending change"),
    one("p1b-genesis-governed-ok", POLICY, "chain/x/policy/genesis.go",
        "case len(policy.GetGovernedLimits()) != 0:",
        "case false:",
        K("chain/x/policy", "TestGenesisValidateImportExport"),
        "policy genesis carries no governed value"),
    one("p1-genesis-accepts-approved", POLICY, "chain/x/policy/genesis.go",
        "if row.GetApproved() || row.GetValueVersion() != 0",
        "if row.GetValueVersion() != 0",
        K("chain/x/policy", "TestGenesisValidateImportExport", "FuzzPolicyGenesisJSON"),
        "policy genesis refuses an approved decision row"),
    one("p1-initgenesis-nonempty-store", POLICY, "chain/x/policy/genesis.go",
        "\tif occupied {\n\t\treturn errors.New(\"policy genesis: store is not empty\")\n\t}",
        "\tif occupied && false {\n\t\treturn errors.New(\"policy genesis: store is not empty\")\n\t}",
        K("chain/x/policy", "TestGenesisValidateImportExport", "TestModule", "TestNoMigrationFromScaffoldLayout"),
        "InitGenesis refuses a non-empty store"),
    one("p1b-export-plan-names-ok", POLICY, "chain/x/policy/genesis.go",
        "\t{[]byte{PlanNamePrefix}, \"plan names\"},\n",
        "",
        K("chain/x/policy", "TestGenesisValidateImportExport"),
        "export refuses a store with a plan-name row"),
    one("rev-export-ignores-applied-rows", POLICY, "chain/x/policy/genesis.go",
        "\t{[]byte{AppliedPrefix}, \"applied upgrades\"},\n",
        "",
        K("chain/x/policy", "TestGenesisValidateImportExport"),
        "export refuses a store with an applied-upgrade row (W0 review)"),
    one("rev-export-ignores-policy-version", POLICY, "chain/x/policy/genesis.go",
        "if v := k.deps.Versions.PolicyVersion(ctx); v != 1 {",
        "if v := k.deps.Versions.PolicyVersion(ctx); v != 1 && false {",
        K("chain/x/policy", "TestGenesisValidateImportExport"),
        "export refuses a policy version other than 1 (W0 review)"),
    one("p1-export-ignores-app-version", POLICY, "chain/x/policy/genesis.go",
        "if appVersion != 1 {",
        "if appVersion > 1 {",
        K("chain/x/policy", "TestGenesisValidateImportExport"),
        "export refuses an app version other than 1"),
    one("p1b-module-beginblock-noop", POLICY, "chain/x/policy/module.go",
        "\treturn am.keeper.ApplyDue(ctx)\n}",
        "\t_ = ctx\n\treturn nil\n}",
        K("chain/x/policy", "TestModule"),
        "the module's BeginBlock runs ApplyDue"),
    one("p1b-pending-walk-6", POLICY, "chain/x/policy/store.go",
        "iterate(store, PendingPrefix, len(msgkit.LimitKeys()), ",
        "iterate(store, PendingPrefix, len(msgkit.LimitKeys())+1, ",
        K("chain/x/policy", "TestApplyDueOrderAndBounds"),
        "the pending walk is bounded by the number of governed keys"),
    one("p1-governed-read-above-ceiling", POLICY, "chain/x/policy/store.go",
        "value < 1 || value > ceiling {\n\t\treturn 0, false, fmt.Errorf(\"policy: governed limit",
        "value < 1 || value > 2*ceiling {\n\t\treturn 0, false, fmt.Errorf(\"policy: governed limit",
        K("chain/x/policy", "TestGovernedLimitReadSurface"),
        "a stored governed value above its ceiling fails closed on read"),
    one("p1b-query-raw-governed", POLICY, "chain/x/policy/query.go",
        "Value: msgkit.Effective(key, value, found)}",
        "Value: value + 0*msgkit.Effective(key, value, found)}",
        K("chain/x/policy", "TestPolicyQuery"),
        "Query/Policy reports the effective value of every governed key"),
    one("p1b-query-no-pending", POLICY, "chain/x/policy/query.go",
        "PendingLimitChanges: pendingMessages(rows),",
        "PendingLimitChanges: pendingMessages(rows[:0]),",
        K("chain/x/policy", "TestPolicyQuery"),
        "Query/Policy reports pending changes"),
    one("p1b-query-no-plan", POLICY, "chain/x/policy/query.go",
        "\t\tstate.UpgradePlan = planMessage(plan)\n",
        "\t\t_ = planMessage(plan)\n",
        K("chain/x/policy", "TestPolicyQuery"),
        "Query/Policy reports the stored plan"),
    one("p1b-query-no-app-version", POLICY, "chain/x/policy/query.go",
        "AppVersion:          appVersion,",
        "AppVersion:          appVersion * 0,",
        K("chain/x/policy", "TestPolicyQuery"),
        "Query/Policy reports the app version"),
    one("p1-query-version-constant", POLICY, "chain/x/policy/query.go",
        "PolicyVersion:       k.deps.Versions.PolicyVersion(ctx),",
        "PolicyVersion:       1,",
        K("chain/x/policy", "TestPolicyQuery"),
        "Query/Policy reports the stored policy version (P1 M15, re-anchored)"),
    one("rev-query-aliases-decisions", POLICY, "chain/x/policy/query.go",
        "BlockedDecisions:    cloneDecisions(k.deps.Decisions.BlockedDecisions(ctx)),",
        "BlockedDecisions:    k.deps.Decisions.BlockedDecisions(ctx),",
        K("chain/x/policy", "TestPolicyQuery"),
        "Query/Policy returns copies of the decision rows (W0 review)"),
    one("p1b-ptest-decisions-value-only", POLICY, "chain/x/policy/policytest/policytest.go",
        "if !hasPending || err != nil || p != want {",
        "if !hasPending || err != nil || p.Value != want.Value {",
        K("chain/x/policy/policytest", "TestCheckDecisionsSelfTest"),
        "CheckDecisions compares the whole pending row with the latest decision"),
    one("p1b-ptest-plan-name-check-off", POLICY, "chain/x/policy/policytest/policytest.go",
        "if digest, named := names[plan.Name]; !named || !bytes.Equal(digest, plan.Digest) {",
        "if digest, named := names[plan.Name]; false && (!named || !bytes.Equal(digest, plan.Digest)) {",
        K("chain/x/policy/policytest", "TestPolicyInvariantsSelfTest"),
        "I4: a plan needs a plan-name row with its digest"),
    one("p1b-ptest-applied-name-off", POLICY, "chain/x/policy/policytest/policytest.go",
        "\t\tif _, found := names[name]; !found {",
        "\t\tif _, found := names[name]; found && !found {",
        K("chain/x/policy/policytest", "TestPolicyInvariantsSelfTest"),
        "I4: an applied upgrade needs a plan-name row"),
    one("p1b-ptest-decisions-due-keeps-pending", POLICY, "chain/x/policy/policytest/policytest.go",
        "\t\t\tif hasPending {\n\t\t\t\treturn fmt.Errorf(\"I3: %q keeps a pending row",
        "\t\t\tif false && hasPending {\n\t\t\t\treturn fmt.Errorf(\"I3: %q keeps a pending row",
        K("chain/x/policy/policytest", "TestCheckDecisionsSelfTest"),
        "I3: an immediate decision leaves no pending row"),
    one("p1b-ptest-decisions-undecided-ok", POLICY, "chain/x/policy/policytest/policytest.go",
        "\t\t\tif hasGoverned || hasPending {",
        "\t\t\tif false {",
        K("chain/x/policy/policytest", "TestCheckDecisionsSelfTest"),
        "I3: a governed or pending row needs a decision"),
    one("p1-ptest-pending-activation", POLICY, "chain/x/policy/policytest/policytest.go",
        "if p.Activation <= height {",
        "if p.Activation < height {",
        K("chain/x/policy/policytest", "TestPolicyInvariantsSelfTest"),
        "the checker refuses a pending change at or below the height"),
    one("p1-ptest-i5-count", POLICY, "chain/x/policy/policytest/policytest.go",
        "if err != nil || v != uint64(appliedCount)+1 {",
        "if err != nil || v == 0 {",
        K("chain/x/policy/policytest", "TestPolicyInvariantsSelfTest"),
        "I5: the app version is the applied count + 1"),
    one("rev-ptest-i5-applied-height-unbounded", POLICY, "chain/x/policy/policytest/policytest.go",
        "if err != nil || h < 1 || h > height {",
        "if err != nil || h < 1 {",
        K("chain/x/policy/policytest", "TestPolicyInvariantsSelfTest"),
        "I5: an applied height above the current height is a violation (W0 review)"),
    one("rev-ptest-i6-policy-entry-unchecked", POLICY, "chain/x/policy/policytest/policytest.go",
        "if name == policy.ModuleName && v != layout {",
        "if name == policy.ModuleName && v != layout && false {",
        K("chain/x/policy/policytest", "TestPolicyInvariantsSelfTest"),
        "I6: the version map's policy entry equals the layout (W0 review)"),
    # ---------------------------------------------------------------- upgrade
    one("p3a-gate-no-row2", UPGRADE, "chain/upgrade/gate.go",
        "\t\tif due {\n\t\t\treturn refuse(2,",
        "\t\tif false && due {\n\t\t\treturn refuse(2,",
        K("chain/upgrade", "TestGateTable", "TestGateFuzzDomainReachesEveryRow", "TestRunnerGateReadsState"),
        "gate row 2: a due plan without its handler refuses to start"),
    one("p3b-gate-accept-due-without-handler", UPGRADE, "chain/upgrade/gate.go",
        "\t\tif due {\n\t\t\treturn refuse(2,",
        "\t\tif false {\n\t\t\treturn refuse(2,",
        K("chain/upgrade", "TestGateTable") + K("chain/upgrade/onchain", "TestOnChainGateTable", "TestOnChainHaltAndSwitch"),
        "gate row 2 on the on-chain plan rows"),
    one("p3a-gate-row4-starts", UPGRADE, "chain/upgrade/gate.go",
        "\t\tcase !due:\n\t\t\treturn refuse(4,",
        "\t\tcase false && !due:\n\t\t\treturn refuse(4,",
        K("chain/upgrade", "TestGateTable", "TestGateFuzzDomainReachesEveryRow", "TestRunnerGateReadsState"),
        "gate row 4: a binary started too early refuses"),
    one("p3b-gate-accept-too-early", UPGRADE, "chain/upgrade/gate.go",
        "\t\tcase !due:\n",
        "\t\tcase false:\n",
        K("chain/upgrade", "TestGateTable") + K("chain/upgrade/onchain", "TestOnChainGateTable") + K("chain/app", "TestLoadGateTable"),
        "gate row 4 through app.New's load gate (plan 16.9: accept too early)"),
    one("p3a-gate-no-row5-app", UPGRADE, "chain/upgrade/gate.go",
        "if s.AppVersion > b.AppVersion {",
        "if false && s.AppVersion > b.AppVersion {",
        K("chain/upgrade", "TestGateTable"),
        "gate row 5: state newer than the binary (Ps > P) refuses"),
    one("p3b-gate-accept-newer", UPGRADE, "chain/upgrade/gate.go",
        "\tif s.AppVersion > b.AppVersion {",
        "\tif false {",
        K("chain/upgrade", "TestGateTable") + K("chain/upgrade/onchain", "TestOnChainGateTable"),
        "gate row 5 through app.New's load gate"),
    one("p3a-gate-no-row5-module", UPGRADE, "chain/upgrade/gate.go",
        "if s.Modules[name] > want {",
        "if false && s.Modules[name] > want {",
        K("chain/upgrade", "TestGateTable"),
        "gate row 5: a stored module version above the binary's refuses"),
    one("plan-gate-row1-module-mismatch-ignored", UPGRADE, "chain/upgrade/gate.go",
        "if name, differ := firstDifference(s.Modules, b.Modules); differ {",
        "if name, differ := firstDifference(s.Modules, s.Modules); differ {",
        K("chain/upgrade", "TestGateTable"),
        "gate row 6: an equal app version with another module map refuses (plan 16.9: ignore a module missing from the stored map)"),
    one("p3a-gate-no-version-app-consistency", UPGRADE, "chain/upgrade/gate.go",
        "if s.AppVersion != s.ConsensusAppVersion {",
        "if false && s.AppVersion != s.ConsensusAppVersion {",
        K("chain/upgrade", "TestGateTable", "TestGateFuzzDomainReachesEveryRow"),
        "gate row 6: the stored Version.App must equal the stored app version"),
    one("p3a-gate-no-layout-consistency", UPGRADE, "chain/upgrade/gate.go",
        "case layout != stored:",
        "case false && layout != stored:",
        K("chain/upgrade", "TestGateTable", "TestGateFuzzDomainReachesEveryRow"),
        "gate row 6: a layout key that differs from the version map refuses"),
    one("p3a-gate-handler-to-unchecked", UPGRADE, "chain/upgrade/gate.go",
        "case up.FromAppVersion != s.AppVersion || up.ToAppVersion != b.AppVersion:",
        "case up.FromAppVersion != s.AppVersion:",
        K("chain/upgrade", "TestGateTable", "TestGateFuzzDomainReachesEveryRow"),
        "gate row 3 needs a handler whose To is this binary's app version"),
    one("p3a-gate-plan-below-unchecked", UPGRADE, "chain/upgrade/gate.go",
        "if s.Plan != nil && s.Plan.Height <= s.LastHeight {",
        "if false && s.Plan != nil && s.Plan.Height <= s.LastHeight {",
        K("chain/upgrade", "TestGateTable", "TestGateFuzzDomainReachesEveryRow", "FuzzGateStartsOnlyOnRows1And3"),
        "gate row 6: a plan at or below the last committed height refuses"),
    one("p3a-gate-l0-decides", UPGRADE, "chain/upgrade/gate.go",
        "if s.LastHeight == 0 {\n\t\treturn Decision{}, errorf(ErrNoCommittedBlock",
        "if false {\n\t\treturn Decision{}, errorf(ErrNoCommittedBlock",
        K("chain/upgrade", "TestGateRefusesBeforeTheFirstBlock", "FuzzGateStartsOnlyOnRows1And3"),
        "the gate decides only after a committed block"),
    one("rev-checkblock-duplicate-handler-accepted", UPGRADE, "chain/upgrade/block.go",
        "\t\t\tcase n > 1:\n\t\t\t\treturn Upgrade{}, false, errorf(ErrWrongBinary, nil, \"binary declares %d handlers named %s: wrong binary for height %d\",\n\t\t\t\t\tn, plan.Name, height)\n",
        "",
        K("chain/upgrade", "TestCheckBlock"),
        "the block rule refuses a binary with two handlers of the plan's name (W0 review)"),
    one("p3b-block-no-appversion-rule", UPGRADE, "chain/upgrade/block.go",
        "\tif appVersion != b.AppVersion {",
        "\tif false {",
        K("chain/upgrade", "TestCheckBlock", "TestRunnerPreBlockRule") + K("chain/upgrade/onchain", "TestOnChainBlockRule") + K("chain/app", "TestPreBlockRefusesAppVersionMismatch"),
        "the per-block app-version rule refuses a block when the stored app version differs from the binary's"),
    one("p3b-block-needed-continues", UPGRADE, "chain/upgrade/block.go",
        "\t\t\tcase n == 0:\n\t\t\t\treturn Upgrade{}, false, errorf(ErrUpgradeNeeded",
        "\t\t\tcase n == 0 && false:\n\t\t\t\treturn Upgrade{}, false, errorf(ErrUpgradeNeeded",
        K("chain/upgrade", "TestCheckBlock") + K("chain/upgrade/onchain", "TestOnChainHaltAndSwitch"),
        "a binary without the planned handler stops at the plan height (UPGRADE NEEDED)"),
    one("p3b-block-plan-height-plus1", UPGRADE, "chain/upgrade/block.go",
        "\t\tif plan.Height == height {",
        "\t\tif plan.Height == height+1 {",
        K("chain/upgrade", "TestCheckBlock") + K("chain/upgrade/onchain", "TestOnChainBlockRule") + K("chain/app", "TestUpgradeRehearsalInProcess"),
        "the upgrade runs at exactly the plan height, not one block later"),
    one("plan-block-plan-height-minus1", UPGRADE, "chain/upgrade/block.go",
        "\t\tif plan.Height == height {",
        "\t\tif plan.Height == height-1 {",
        K("chain/upgrade", "TestCheckBlock") + K("chain/upgrade/onchain", "TestOnChainBlockRule") + K("chain/app", "TestUpgradeRehearsalInProcess"),
        "the upgrade runs at exactly the plan height, not one block early"),
    one("p3a-block-no-handler-version-check", UPGRADE, "chain/upgrade/block.go",
        "case up.FromAppVersion != appVersion || up.ToAppVersion != b.AppVersion:",
        "case false:",
        K("chain/upgrade", "TestCheckBlock"),
        "the block rule needs a handler whose From/To match the stored and binary app versions"),
    one("p3a-block-plan-below-ignored", UPGRADE, "chain/upgrade/block.go",
        "if plan.Height < height {",
        "if false && plan.Height < height {",
        K("chain/upgrade", "TestCheckBlock", "TestRunnerPreBlockRule"),
        "a plan below the current height fails the block"),
    one("p3a-validate-no-from-check", UPGRADE, "chain/upgrade/upgrade.go",
        "case up.FromAppVersion+1 != b.AppVersion || up.ToAppVersion != b.AppVersion:",
        "case up.ToAppVersion != b.AppVersion:",
        K("chain/upgrade", "TestBinaryRefusesBadUpgradeList"),
        "a handler must go from P-1 to P"),
    one("p3a-validate-no-dup-check", UPGRADE, "chain/upgrade/upgrade.go",
        "case seen[up.Name]:",
        "case false && seen[up.Name]:",
        K("chain/upgrade", "TestBinaryRefusesBadUpgradeList"),
        "a binary declares each upgrade name once"),
    one("p3a-apply-on-block-context", UPGRADE, "chain/upgrade/runner.go",
        "cc, write := ctx.CacheContext()",
        "cc, write := ctx, func() {}",
        K("chain/upgrade", "TestRunnerWritesOnSuccessOnly", "TestRunnerPostConditions", "TestRunnerAppliesRealMigration"),
        "the runner works on a cache branch and writes only on success (plan 16.9: finalize context instead of cache)"),
    one("p3a-skip-handler-app-version-check", UPGRADE, "chain/upgrade/runner.go",
        "if got := r.deps.Versions.AppVersion(cc); got != pre.appVersion {",
        "if got := r.deps.Versions.AppVersion(cc); false && got != pre.appVersion {",
        K("chain/upgrade", "TestRunnerPostConditions"),
        "a handler may not change the app version itself"),
    one("p3a-skip-unchanged-compare", UPGRADE, "chain/upgrade/runner.go",
        "if !bytes.Equal(snapshot, pre.snapshots[i]) {",
        "if false && !bytes.Equal(snapshot, pre.snapshots[i]) {",
        K("chain/upgrade", "TestRunnerPostConditions") + K("chain/app", "TestMigrationCannotChangeAuthorityRows"),
        "I13: the runner compares every Unchanged snapshot before and after the handler"),
    one("p3a-skip-policy-bump", UPGRADE, "chain/upgrade/runner.go",
        "if err := r.deps.Counter.BumpPolicyVersionForUpgrade(cc); err != nil {",
        "if err := error(nil); err != nil {",
        K("chain/upgrade", "TestRunnerAppliesUpgrade", "TestRunnerAppliesRealMigration"),
        "an applied upgrade bumps the policy version (DR-10-38)"),
    one("p3a-skip-params-postcheck", UPGRADE, "chain/upgrade/runner.go",
        "if !bytes.Equal(paramBytes, pre.paramBytes) {",
        "if false && !bytes.Equal(paramBytes, pre.paramBytes) {",
        K("chain/upgrade", "TestRunnerPostConditions") + K("chain/app", "TestMigrationCannotChangeBlockParams"),
        "a handler may not change the consensus params"),
    one("p3a2-plan-postcheck-only-presence", UPGRADE, "chain/upgrade/runner.go",
        "if !found || after.Name != plan.Name || after.Height != plan.Height || !bytes.Equal(after.Digest, plan.Digest) {",
        "if _ = after; !found {",
        K("chain/upgrade", "TestRunnerPostConditions"),
        "a handler may not change the stored plan"),
    one("p3a-skip-stale-plan-readback", UPGRADE, "chain/upgrade/runner.go",
        "if found && after.Name == up.Name {",
        "if false && found && after.Name == up.Name {",
        K("chain/upgrade", "TestRunnerWritesOnSuccessOnly"),
        "the runner refuses when the plan is still stored after the record"),
    one("p3a-preblock-reports-unchanged", UPGRADE, "chain/upgrade/runner.go",
        "\treturn true, nil\n}",
        "\treturn false, nil\n}",
        K("chain/upgrade", "TestRunnerAppliesUpgrade", "TestRunnerAppliesRealMigration"),
        "PreBlock reports an applied upgrade"),
    one("p3a-skip-binary-module-missing", UPGRADE, "chain/upgrade/runner.go",
        "return before{}, errorf(ErrPrecondition, nil, \"module %s is in this binary but not in the stored version map\", name)",
        "_ = name",
        K("chain/upgrade", "TestRunnerPreconditions", "TestRunMigrationsGapsAreClosedByTheRunner"),
        "a binary module absent from the stored version map is refused before the handler"),
    one("p3a2-skip-stored-module-unknown", UPGRADE, "chain/upgrade/runner.go",
        "case !ok:\n\t\t\treturn before{}, errorf(ErrPrecondition, nil, \"stored module %s is not in this binary\", name)",
        "case !ok && false:\n\t\t\treturn before{}, errorf(ErrPrecondition, nil, \"stored module %s is not in this binary\", name)",
        K("chain/upgrade", "TestRunnerPreconditions", "TestRunMigrationsGapsAreClosedByTheRunner"),
        "a stored module unknown to the binary is refused before the handler"),
    one("p3a-skip-returned-map-check", UPGRADE, "chain/upgrade/runner.go",
        "if name, differ := firstDifference(migrated, r.binary.Modules); differ {",
        "if name, differ := firstDifference(migrated, migrated); differ {",
        K("chain/upgrade", "TestRunnerPostConditions"),
        "the migrated version map must equal the binary's"),
    one("p3a-skip-layout-precheck", UPGRADE, "chain/upgrade/runner.go",
        "if err := r.checkLayouts(cc, pre.modules, ErrPrecondition); err != nil {",
        "if err := error(nil); err != nil {",
        K("chain/upgrade", "TestRunnerPreconditions"),
        "layout keys must match the stored version map before the handler"),
    one("p3a-skip-version-app-precheck", UPGRADE, "chain/upgrade/runner.go",
        "if params.Version == nil || params.Version.App != pre.appVersion {",
        "if params.Version == nil {",
        K("chain/upgrade", "TestRunnerPreconditions"),
        "the stored Version.App must equal the stored app version before the handler"),
    one("p3a-record-other-app-version", UPGRADE, "chain/upgrade/runner.go",
        "params.Version = &cmtproto.VersionParams{App: up.ToAppVersion}",
        "params.Version = &cmtproto.VersionParams{App: up.FromAppVersion}",
        K("chain/upgrade", "TestRunnerAppliesUpgrade", "TestRunnerPostConditions"),
        "the runner sets Version.App to the handler's To"),
    one("intc-env-not-passed", UPGRADE, "chain/upgrade/runner.go",
        "up.Handler(WithEnv(cc, r.deps.Env), cloneVM(stored), r.deps.Migrate)",
        "up.Handler(cc, cloneVM(stored), r.deps.Migrate)",
        K("chain/upgrade", "TestRunnerGivesHandlerTheEnv", "TestRunnerRefusesProtectedEnvWrites"),
        "ORCH-7: the runner gives the handler its store environment on the runner's cache branch"),
    one("intc-env-dep-optional", UPGRADE, "chain/upgrade/runner.go",
        "deps.Migrate == nil, deps.Env == nil:",
        "deps.Migrate == nil:",
        K("chain/upgrade", "TestNewRunnerRefusesBadDeps"),
        "ORCH-7: a runner without a handler environment is refused"),
    one("intc-fake-accepts-absent-plan", UPGRADE, "chain/upgrade/runner_test.go",
        "\tif !found {\n\t\treturn fmt.Errorf(\"no stored plan for upgrade %s at height %d\", name, height)\n\t}\n",
        "\t_ = found\n",
        K("chain/upgrade", "TestRunnerRecordRefusesAbsentPlan"),
        "the runner tests' fake keeper refuses an absent plan like x/policy (test-double mutant)"),
    one("p3b-plans-no-recover", UPGRADE, "chain/upgrade/plans.go",
        "\tdefer func() {\n\t\tif p := recover(); p != nil {",
        "\tdefer func() {\n\t\tif p := any(nil); p != nil {",
        K("chain/upgrade", "TestCouncilPlansRefusesDamagedRows") + K("chain/upgrade/onchain", "TestOnChainDamagedPlanRows"),
        "CouncilPlans turns a keeper panic on a damaged row into an error"),
    one("p3b-plans-no-applied-check", UPGRADE, "chain/upgrade/plans.go",
        "\tcase applied:\n",
        "\tcase applied && false:\n",
        K("chain/upgrade", "TestCouncilPlansRefusesDamagedRows") + K("chain/upgrade/onchain", "TestOnChainBlockRule"),
        "CouncilPlans refuses a stored plan whose name is already applied"),
    one("p3b-plans-no-applied-lookup", UPGRADE, "chain/upgrade/plans.go",
        "\t\tapplied = c.rows.UpgradeApplied(ctx, plan.Name)",
        "\t\t_ = c.rows",
        K("chain/upgrade", "TestCouncilPlansRefusesDamagedRows", "TestCouncilPlansReportsTheStoredPlan"),
        "CouncilPlans looks up the applied rows"),
    one("p3b-plans-no-name-check", UPGRADE, "chain/upgrade/plans.go",
        "\tcase !msgkit.ValidID(stored.Name):\n",
        "\tcase false:\n",
        K("chain/upgrade", "TestCouncilPlansRefusesDamagedRows"),
        "CouncilPlans refuses a plan name outside the id pattern"),
    one("p3b-plans-no-height-check", UPGRADE, "chain/upgrade/plans.go",
        "\tcase stored.Height < 1:\n",
        "\tcase false:\n",
        K("chain/upgrade", "TestCouncilPlansRefusesDamagedRows"),
        "CouncilPlans refuses a plan height 0"),
    one("p3b-plans-no-digest-check", UPGRADE, "chain/upgrade/plans.go",
        "\tcase len(stored.Digest) != DigestBytes:\n",
        "\tcase false:\n",
        K("chain/upgrade", "TestCouncilPlansRefusesDamagedRows"),
        "CouncilPlans refuses a digest that is not 32 bytes"),
    one("p3b-plans-no-digest-copy", UPGRADE, "chain/upgrade/plans.go",
        "Digest: append([]byte(nil), stored.Digest...)}, true, nil",
        "Digest: stored.Digest}, true, nil",
        K("chain/upgrade", "TestCouncilPlansReportsTheStoredPlan"),
        "CouncilPlans returns a copy of the stored digest"),
    one("p3b-plans-new-accepts-nil", UPGRADE, "chain/upgrade/plans.go",
        "\tif rows == nil {\n\t\treturn nil, errorf(",
        "\tif false {\n\t\treturn nil, errorf(",
        K("chain/upgrade", "TestCouncilPlansRefusesMissingRows"),
        "NewCouncilPlans refuses a nil row reader"),
    one("p3b-plans-plan-no-nil-guard", UPGRADE, "chain/upgrade/plans.go",
        "\tif c == nil || c.rows == nil {",
        "\tif false {",
        K("chain/upgrade", "TestCouncilPlansRefusesMissingRows"),
        "Plan on a nil source fails"),
    one("p3b-plans-ignore-found", UPGRADE, "chain/upgrade/plans.go",
        "\tif err != nil || !found {\n\t\treturn Plan{}, false, err",
        "\tif err != nil || (!found && false) {\n\t\treturn Plan{}, false, err",
        K("chain/upgrade", "TestCouncilPlansReportsTheStoredPlan") + K("chain/upgrade/onchain", "TestOnChainActivationIsHeightOnly"),
        "no stored plan means no plan"),
    one("p5a-i13-snapshot-nil", UPGRADE, "chain/app/upgrades.go",
        "\t\t\t\treturn class.snapshot(ctx.KVStore(key)), nil",
        "\t\t\t\t_ = key\n\t\t\t\treturn nil, nil",
        K("chain/app", "TestMigrationCannotChangeAuthorityRows", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13 snapshots read the class's store"),
    one("inta-i13-drops-other-rows", UPGRADE, "chain/app/upgrades.go",
        "\tclasses = append(classes, rowClass{name: \"authority other rows\"",
        "\t_ = append(classes, rowClass{name: \"authority other rows\"",
        K("chain/app", "TestUpgradePostConditionClasses", "TestMigrationCannotAddRowsBesideThePolicyVersion"),
        "I13 includes the class of every other authority row"),
    one("plan-i13-first-byte-0x04-skip", UPGRADE, "chain/app/upgrades.go",
        "\t\tif bytes.Equal(key, exact) {",
        "\t\tif key[0] == exact[0] {",
        K("chain/app", "TestMigrationCannotAddRowsBesideThePolicyVersion"),
        "I13 leaves out only the exact key {0x04}, not every key that starts with 0x04 (Wave 1 gate fix)"),
    one("p5a-params-check-skip", UPGRADE, "chain/upgrade/runner.go",
        "\tif !bytes.Equal(paramBytes, pre.paramBytes) {\n",
        "\tif false && !bytes.Equal(paramBytes, pre.paramBytes) {\n",
        K("chain/app", "TestMigrationCannotChangeBlockParams"),
        "a migration that changes block params is refused through app.New"),
    one("plan-preblock-log-and-continue", UPGRADE, "chain/app/app.go",
        "\tapplied, err := app.upgradeStep(ctx)\n\tif err != nil {\n\t\treturn nil, err\n\t}",
        "\tapplied, err := app.upgradeStep(ctx)\n\tif err != nil {\n\t\tctx.Logger().Error(err.Error())\n\t\tapplied = false\n\t}",
        K("chain/app", "TestPreBlockRefusesAppVersionMismatch", "TestPreBlockReturnsAnErrorOnADamagedAppVersion", "TestUpgradeHandlerErrorHalts"),
        "PreBlock returns the upgrade step's error and fails the block (plan 16.9 PreBlock row)"),
    one("inta-preblock-skips-upgrade-step", UPGRADE, "chain/app/app.go",
        "\tapplied, err := app.upgradeStep(ctx)\n",
        "\tapplied, err := false, error(nil)\n",
        K("chain/app", "TestPreBlockReturnsAnErrorOnADamagedAppVersion", "TestPreBlockRefusesAppVersionMismatch"),
        "PreBlock runs the upgrade step first"),
    one("p5a-initchain-any-appversion", UPGRADE, "chain/app/app.go",
        "\tif binary.AppVersion != GenesisAppVersion {\n",
        "\tif false && binary.AppVersion != GenesisAppVersion {\n",
        K("chain/app", "TestNewerBinaryRefusesInitChain"),
        "InitChain refuses a binary whose app version is not the genesis app version"),
    one("inta-gate-off", UPGRADE, "chain/app/app.go",
        "\tdecision, stored, err := app.gateOn(ctx, uint64(height))\n",
        "\tdecision, stored, err := upgrade.Decision{Row: 1}, uint64(1), error(nil)\n\t_, _ = app.gateOn, ctx\n",
        K("chain/app", "TestAppRefusesScaffoldLayout", "TestLoadGateRefusesDamagedUpgradeRows"),
        "app.New runs the load gate after LoadLatestVersion"),
    one("inta-gate-panic-not-recovered", UPGRADE, "chain/app/app.go",
        "\t\tif p := recover(); p != nil {\n\t\t\tdecision, stored = upgrade.Decision{}, 0",
        "\t\tif p := any(nil); p != nil {\n\t\t\tdecision, stored = upgrade.Decision{}, 0",
        K("chain/app", "TestLoadGateRefusesDamagedUpgradeRows"),
        "a keeper panic in the load gate is a row 6 refusal"),
    one("inta-prepare-check-stater-off", UPGRADE, "chain/app/app.go",
        "\t\tapp.SetProtocolVersion(app.policy.AppVersion(ctx))\n\t})",
        "\t\t_ = ctx\n\t})",
        K("chain/app", "TestUpgradeRehearsalInProcess"),
        "PrepareCheckState sets the protocol version to the stored app version after an upgrade"),
    one("plan-i13-class-skipped-layout-version", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority layout version\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"authority layout version\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-config", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority config\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows", "TestMigrationCannotChangeAuthorityRows", "TestMigrationCannotApproveDecision"),
        "I13: an upgrade may not change the class \"authority config\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-admission-rows", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority admission rows\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"authority admission rows\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-pending-validator-changes", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority pending validator changes\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"authority pending validator changes\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-proposals", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority proposals\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"authority proposals\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-business-ids", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority business ids\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"authority business ids\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-executed-digests", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority executed digests\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"authority executed digests\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-open-counts", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority open counts\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"authority open counts\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-retired-seat-keys", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority retired seat keys\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"authority retired seat keys\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-retired-service-addresses", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority retired service addresses\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"authority retired service addresses\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-other-rows", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"authority other rows\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows", "TestMigrationCannotAddRowsBesideThePolicyVersion"),
        "I13: an upgrade may not change the class \"authority other rows\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-policy-safety-caps", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"policy safety caps\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows", "TestMigrationCannotChangeSafetyCaps"),
        "I13: an upgrade may not change the class \"policy safety caps\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-policy-governed-values", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"policy governed values\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"policy governed values\" (plan 16.9: one mutant per I13 class)"),
    one("plan-i13-class-skipped-policy-pending-changes", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"policy pending changes\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows"),
        "I13: an upgrade may not change the class \"policy pending changes\" (plan 16.9: one mutant per I13 class)"),
    one("fix-i13-class-skipped-policy-applied-upgrades", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"policy applied upgrades\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows", "TestMigrationCannotChangeAuthorityRows"),
        "I13: an upgrade may not change the class \"policy applied upgrades\" (Wave 2 security review: x/policy 0x07)"),
    one("fix-i13-class-skipped-policy-plan-names", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"policy plan names\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows", "TestMigrationCannotChangeAuthorityRows", "TestUpgradeCannotChangePlanNameOrRunnerKeyRows"),
        "I13: an upgrade may not change the class \"policy plan names\" (Wave 2 security review: x/policy 0x08 holds the Q3 plan-name rule)"),
    one("fix-i13-class-skipped-policy-other-rows", UPGRADE, "chain/app/upgrades.go",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tvar out []byte\n",
        "func (c rowClass) snapshot(store storetypes.KVStore) []byte {\n\tif c.name == \"policy other rows\" {\n\t\treturn nil\n\t}\n\tvar out []byte\n",
        K("chain/app", "TestUpgradeHandlerEnvCannotChangeProtectedRows", "TestMigrationCannotChangeAuthorityRows", "TestUpgradeCannotChangePlanNameOrRunnerKeyRows"),
        "I13: an upgrade may not change the class \"policy other rows\" (Wave 2 security review: unknown x/policy keys and longer keys beside the runner's exact keys)"),
    one("fix-i13-plan-names-class-dropped", UPGRADE, "chain/app/upgrades.go",
        "\t{\"policy plan names\", policy.PlanNamePrefix},\n",
        "",
        K("chain/app", "TestUpgradePostConditionClasses", "TestUpgradeCannotChangePlanNameOrRunnerKeyRows"),
        "the x/policy plan-name rows have a class of their own"),
    # ---------------------------------------------------------------- app
    one("ported-06-svnd-skips-authority-rules", APP, "chain/cmd/svnd/cli/genesis.go",
        "\t\treturn app.ValidateAuthorityGenesis(modules, genDoc.InitialHeight, genDoc.GenesisTime.Unix())",
        "\t\t_ = app.ValidateAuthorityGenesis\n\t\treturn nil",
        K("chain/cmd/svnd/cli", "TestValidateGenesisAppliesAuthorityRules"),
        "svnd genesis validation applies the authority genesis rules (goal 06 svnd-skips-authority-rules, ported)"),
    one("inta-initchain-no-versions", APP, "chain/app/app.go",
        "\tif err := app.policy.InitVersions(ctx, binary.Modules, binary.AppVersion); err != nil {",
        "\tif err := error(nil); err != nil {",
        K("chain/app", "TestGenesisPolicyRules", "TestRestartLoadsCommittedState"),
        "InitChain writes the version map and app version"),
    one("inta-export-skips-policy-check", APP, "chain/app/app.go",
        "\tif err := app.policy.ExportCheck(ctx); err != nil {\n\t\treturn nil, fmt.Errorf(\"export refused: %w\", err)",
        "\tif err := error(nil); err != nil {\n\t\treturn nil, fmt.Errorf(\"export refused: %w\", err)",
        K("chain/app", "TestExportRefusesGovernanceState"),
        "Export refuses governance state the policy genesis cannot carry (plan 16.9 export row)"),
    one("inta-genesis-skips-policy-rows", APP, "chain/app/state.go",
        "\treturn validatePolicyGenesis(modules[ModulePolicy], state.GetBlockedDecisions())",
        "\treturn nil",
        K("chain/app", "TestGenesisPolicyRules"),
        "genesis validation applies the policy genesis rules"),
    one("inta-seat-account-not-created", APP, "chain/app/upgrades.go",
        "\ts.keeper.SetAccount(ctx, s.keeper.NewAccountWithAddress(ctx, sdk.AccAddress(addr)))\n",
        "",
        K("chain/app", "TestCouncilSeatCreatesTheNewKeyAccount", "TestRemovedCouncilSeat"),
        "the app's account creator creates the new seat key's account"),
    one("inta-initchain-version-app-unchecked", APP, "chain/app/app.go",
        "req.ConsensusParams.Version.App != GenesisAppVersion {",
        "false {",
        K("chain/app", "TestGenesisPolicyRules"),
        "InitChain refuses a genesis Version.App other than the genesis app version"),
    one("inta-initchain-layout-unchecked", APP, "chain/app/app.go",
        "\t\tcase !found || version != store.version || binary.Modules[store.name] != version:",
        "\t\tcase !found || version != store.version:",
        K("chain/app", "TestInitChainRefusesALayoutTheVersionMapDoesNotName"),
        "InitChain cross-checks each layout key against the binary's version map"),
    one("inta-layout-table-omits-policy", APP, "chain/app/app.go",
        "\t\t{ModulePolicy, policy.ConsensusVersion},\n",
        "",
        K("chain/app", "TestAppRefusesScaffoldLayout"),
        "the layout table names x/policy"),
    one("inta-genesis-d10-off", APP, "chain/genesis/document.go",
        "\t\tif len(row.SeatID) > MaxSeatIDLength {",
        "\t\tif false {",
        K("chain/genesis", "TestSeatRulesD7D10") + K("chain/app", "TestGenesisSeatRulesD7D10"),
        "D-10 in chain/genesis: a seat id is at most 59 characters"),
    one("inta-genesis-d7-off", APP, "chain/genesis/document.go",
        "\tif err := validateSeatKeys(doc.DevnetTestSeats, pubs); err != nil {",
        "\tif err := error(nil); err != nil {",
        K("chain/genesis", "TestSeatRulesD7D10") + K("chain/app", "TestGenesisSeatRulesD7D10"),
        "D-7 in chain/genesis: a seat key is not a validator key"),
    one("inta-devnet-delay-ignored", APP, "chain/devnet/init.go",
        "DelaySeconds: opts.CouncilDelaySeconds},",
        "DelaySeconds: 0},",
        K("chain/devnet", "TestInitCouncilDelayAndAppVersion"),
        "devnet init writes the requested DEC-008 delay (init only; no validator starts)"),
    one("inta-version-no-modules", APP, "chain/cmd/svnd/cli/version.go",
        "\t\tlines = append(lines, fmt.Sprintf(\"module %s %d\", name, binary.Modules[name]))\n",
        "\t\t_ = name\n",
        K("chain/cmd/svnd/cli", "TestVersionCommandFields"),
        "svnd version prints every module version"),
    one("inta-node-drops-upgrades", APP, "chain/node/start.go",
        "\tappConfig.Upgrades = cfg.Upgrades\n",
        "",
        K("chain/node", "TestStartWithPassesUpgradeHandlers"),
        "node.StartWith passes the binary's upgrade handlers to the app (on this mutant the named test's StartWith starts a node, one validator of four; the test fails after 60 s, and the node runs until the go test process exits)", timeout="3m"),
    one("p3a-cli-start-drops-handlers", APP, "chain/cmd/svnd/cli/root.go",
        "node.Config{Home: home, Upgrades: upgrades}",
        "node.Config{Home: home}",
        K("chain/cmd/svnd/cli", "TestStartHandsTheHandlersToTheNodeOnAHome"),
        "svnd start passes the build's upgrade handlers to the node (P7 added the killer; on this mutant it starts a node, one validator of four; the test fails after 60 s, and the node runs until the go test process exits)", timeout="3m"),
    one("rev-cli-root-aliases-handler-list", APP, "chain/cmd/svnd/cli/root.go",
        "upgrades := append([]upgrade.Upgrade(nil), opts.Upgrades...)",
        "upgrades := opts.Upgrades",
        K("chain/cmd/svnd/cli", "TestStartPassesUpgradesToTheNode"),
        "cli.Root keeps its own copy of the handler list (W0 review T-F4)"),
    one("intc-start-no-chain-check", APP, "chain/cmd/svnd/cli/root.go",
        "\t\t\tif chainIDPrefix != \"\" {\n\t\t\t\tif err := requireChainIDPrefix(home, chainIDPrefix); err != nil {",
        "\t\t\tif false && chainIDPrefix != \"\" {\n\t\t\t\tif err := requireChainIDPrefix(home, chainIDPrefix); err != nil {",
        K("chain/cmd/svnd/cli", "TestStartRefusesOtherChainIDs"),
        "ORCH-6: a tagged build refuses to start on a chain id without the devnet prefix"),
    one("intc-b2-no-chain-prefix", APP, "chain/cmd/svnd/rehearsal_tag.go",
        ", ChainIDPrefix: genesis.DevnetChainIDPrefix}",
        ", ChainIDPrefix: genesis.DevnetChainIDPrefix[:0]}",
        K("chain/cmd/svnd", "TestRehearsalBuildsRefuseNonDevChains", "TestRehearsalBuildsDeclareTheRehearsalUpgrade"),
        "ORCH-6: the svn_rehearsal build sets the devnet chain-id prefix"),
    Mutant("plan-default-build-links-rehearsal", APP, (
        Edit("chain/cmd/svnd/default_build.go",
             "import \"github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli\"\n",
             "import (\n\t\"github.com/Maxencejules/shortvideo-network/chain/cmd/svnd/cli\"\n\t\"github.com/Maxencejules/shortvideo-network/chain/internal/rehearsal\"\n)\n"),
        Edit("chain/cmd/svnd/default_build.go",
             "\treturn cli.Options{}\n",
             "\treturn cli.Options{Upgrades: rehearsal.Upgrades()}\n"),
    ), K("chain/cmd/svnd", "TestDefaultBuildCompilesNoRehearsal") + K("chain/app", "TestLinkedBinaryOmitsTestHooks"),
        "ORCH-6: the default (release) build declares no upgrade handler and links no chain/internal code"),
    one("fix-node-chainid-file-only", APP, "chain/node/chainid.go",
        "\tif _, err := os.Stat(filepath.Join(cfg.DBDir(), \"state.db\")); errors.Is(err, os.ErrNotExist) {",
        "\tif _, err := os.Stat(filepath.Join(cfg.DBDir(), \"state.db\")); err == nil || errors.Is(err, os.ErrNotExist) {",
        K("chain/node", "TestGenesisChainIDReadsWhatCometBFTRuns", "TestStartWithRefusesAStoredGenesisOfAnotherChain") + K("chain/cmd/svnd/cli", "TestStartRefusesOtherChainIDs"),
        "the chain id svnd start uses is checked against the genesis document CometBFT stored (Wave 2 security review)"),
    one("fix-node-stored-genesis-any-chain", APP, "chain/node/chainid.go",
        "\tif doc.ChainID != fileChainID {",
        "\tif false && doc.ChainID != fileChainID {",
        K("chain/node", "TestGenesisChainIDReadsWhatCometBFTRuns", "TestStartWithRefusesAStoredGenesisOfAnotherChain") + K("chain/cmd/svnd/cli", "TestStartRefusesOtherChainIDs") + K("chain/internal/statecheck", "TestReadChainIDReadsAsCometBFTDoes"),
        "a stored genesis document of another chain id is refused"),
    one("fix-node-start-reads-the-file-only", APP, "chain/node/start.go",
        "\tchainID, err := genesisChainID(comet)\n",
        "\tchainID, err := GenesisFileChainID(comet.GenesisFile())\n",
        K("chain/node", "TestStartWithRefusesAStoredGenesisOfAnotherChain"),
        "StartWith reads the chain id CometBFT runs, not only the genesis file"),
    one("fix-cli-chainid-contains", APP, "chain/cmd/svnd/cli/root.go",
        "\tif !strings.HasPrefix(chainID, prefix) {",
        "\tif !strings.Contains(chainID, prefix) {",
        K("chain/cmd/svnd/cli", "TestStartRefusesOtherChainIDs"),
        "the start chain id check is a prefix check (Wave 2 tests review L1)"),
    one("fix-statecheck-build-no-chainid-prefix", APP, "chain/cmd/svnd/statecheck_tag.go",
        "\topts.ChainIDPrefix = genesis.DevnetChainIDPrefix\n",
        "\t_ = genesis.DevnetChainIDPrefix\n",
        K("chain/cmd/svnd", "TestRehearsalBuildsRefuseNonDevChains"),
        "the svn_statecheck build starts a validator only on a devnet chain id (Wave 2 security review)"),
    # ---------------------------------------------------------------- rehearsal
    one("p5a-step-leaves-layout", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "msgkit.WriteLayoutVersion(store, from+1)",
        "msgkit.WriteLayoutVersion(store, from)",
        K("chain/app", "TestUpgradeRehearsalInProcess") + K("chain/internal/rehearsal", "TestLeasesStepRebuildsTheStore"),
        "the rehearsal migration step writes the new layout version"),
    one("p5a-step-drops-last-row", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "\tfor _, r := range rows {\n\t\tstore.Set(r.key, r.value)",
        "\tfor _, r := range rows[:len(rows)-1] {\n\t\tstore.Set(r.key, r.value)",
        K("chain/app", "TestUpgradeRehearsalInProcess") + K("chain/internal/rehearsal", "TestLeasesStepRebuildsTheStore"),
        "the rehearsal migration step rewrites every row (plan 16.9: drops the last lease record)"),
    one("p5a-reencode-skip-equal", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "\tif !bytes.Equal(out, raw) {\n",
        "\tif false && !bytes.Equal(out, raw) {\n",
        K("chain/internal/rehearsal", "TestStepsRefuseBeforeWriting"),
        "a record that does not re-encode to its stored bytes is refused before any write"),
    one("intc-handler-any-chain", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "if !strings.HasPrefix(ctx.ChainID(), genesis.DevnetChainIDPrefix) {",
        "if false && !strings.HasPrefix(ctx.ChainID(), genesis.DevnetChainIDPrefix) {",
        K("chain/internal/rehearsal", "TestHandlersRefuseOutsideTheirEnvironment"),
        "the rehearsal handler refuses a chain id without the devnet prefix"),
    one("intc-walk-skips-proposals", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "\tif err := checkProposals(store); err != nil {",
        "\tif err := checkProposals(store); false && err != nil {",
        K("chain/internal/rehearsal", "TestHandlerWalksEveryRecordReadOnly"),
        "the plan 12.2 walk checks proposal records"),
    one("intc-walk-skips-reencode", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "\t\t\tif _, err := reencode(key, it.Value(), msg, id); err != nil {\n\t\t\t\treturn err",
        "\t\t\tif _, err := reencode(key, it.Value(), msg, id); false && err != nil {\n\t\t\t\treturn err",
        K("chain/internal/rehearsal", "TestHandlerWalksEveryRecordReadOnly"),
        "the plan 12.2 walk checks that each record re-encodes to its stored bytes"),
    one("intc-walk-refuses-real-proposals", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "\t\tif head.ProposalID != string(key[1:]) {",
        "\t\tif head.ProposalID == string(key[1:]) {",
        K("chain/app", "TestUpgradeRehearsalInProcess"),
        "the walk runs on real state: a walk that refuses real proposal records fails the in-process rehearsal"),
    one("intc-walk-refuses-real-providers", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "\t{providers.ModuleName, providerRecord},",
        "\t{providers.ModuleName, func(key []byte) (proto.Message, func() string) { m, id := providerRecord(key); if m == nil { return nil, nil }; return m, func() string { return id() + \"x\" } }},",
        K("chain/app", "TestUpgradeRehearsalInProcess"),
        "the walk runs on real state: a walk that refuses real provider records fails the in-process rehearsal"),
    one("intc-bad-handler-no-write", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "\tstore.Set(BadRowKey, []byte{BadRowValue})",
        "\t_ = store",
        K("chain/internal/rehearsal", "TestBadHandlerWritesOneDocumentedRow") + K("chain/app", "TestWrongMigrationDiverges"),
        "B2-bad writes its one documented row (leases 0xFF -> 0x01)"),
    one("intc-b2-built-with-bad", REHEARSAL, "chain/cmd/svnd/rehearsal_tag.go",
        "Upgrades: rehearsal.Upgrades()",
        "Upgrades: rehearsal.BadUpgrades()",
        K("chain/app", "TestRehearsalBinaryVersionAndLinks"),
        "the svn_rehearsal build links the honest handler, not BadHandler"),
    one("intc-app-env-missing-store", REHEARSAL, "chain/app/upgrades.go",
        "\tkey, ok := e.keys[name]\n\tif !ok {\n\t\treturn nil, false\n\t}\n\treturn ctx.KVStore(key), true",
        "\tkey, ok := e.keys[name]\n\tif !ok || name == \"leases\" {\n\t\treturn nil, false\n\t}\n\treturn ctx.KVStore(key), true",
        K("chain/app", "TestWrongMigrationDiverges", "TestUpgradeRehearsalInProcess"),
        "the app's handler environment reaches every mounted store"),
    one("plan-rehearsal-migrate-twice", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "\tif err := checkRecords(ctx, env); err != nil {\n\t\treturn nil, err\n\t}\n\treturn migrate(ctx, from)\n",
        "\tif err := checkRecords(ctx, env); err != nil {\n\t\treturn nil, err\n\t}\n\tif _, err := migrate(ctx, from); err != nil {\n\t\treturn nil, err\n\t}\n\treturn migrate(ctx, from)\n",
        K("chain/app", "TestUpgradeRehearsalInProcess"),
        "the rehearsal handler runs the module migrations once (plan 16.9: RunMigrations runs twice)"),
    one("fix-rehearsal-chainid-contains", REHEARSAL, "chain/internal/rehearsal/rehearsal.go",
        "if !strings.HasPrefix(ctx.ChainID(), genesis.DevnetChainIDPrefix) {",
        "if !strings.Contains(ctx.ChainID(), genesis.DevnetChainIDPrefix) {",
        K("chain/internal/rehearsal", "TestHandlersRefuseOutsideTheirEnvironment"),
        "the rehearsal handlers' chain id check is a prefix check (Wave 2 tests review L1)"),
    # ---------------------------------------------------------------- statecheck
    one("p3c-skip-one-store-digest", STATECHECK, "chain/internal/statecheck/statecheck.go",
        "\tfor _, name := range sortedNames(keys) {\n\t\treport.Stores",
        "\tfor _, name := range sortedNames(keys)[1:] {\n\t\treport.Stores",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "every store has a digest line (plan 16.9: skip one store's digest)"),
    one("p3c-skip-policy-checker", STATECHECK, "chain/internal/statecheck/statecheck.go",
        "return policytest.CheckInvariantsAt(branch(), rd.policy, rd.authority, policy.ConsensusVersion)",
        "_ = policytest.CheckInvariantsAt\n\t\t\treturn nil",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the policy invariant checker runs (plan 16.9: skip the policy invariant checker)"),
    one("p3c-skip-evidence-checker", STATECHECK, "chain/internal/statecheck/statecheck.go",
        "return evidencetest.CheckInvariants(branch(), rd.evidence, rd.leases)",
        "_ = evidencetest.CheckInvariants\n\t\t\treturn nil",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the evidence invariant checker runs"),
    one("p3c-skip-lease-offer-checker", STATECHECK, "chain/internal/statecheck/statecheck.go",
        "return leasestest.CheckOfferIntegrity(branch(), rd.leases, rd.content)",
        "return nil",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the lease offer-integrity checker runs"),
    one("p3c-row5-passes", STATECHECK, "chain/internal/statecheck/statecheck.go",
        "if r.Gate.Row == 5 || r.Gate.Row == 6 {",
        "if r.Gate.Row == 6 {",
        K("chain/internal/statecheck", "TestStateCheckRefusesOtherVersion"),
        "a gate row 5 refusal fails the report"),
    one("p3c-row6-passes", STATECHECK, "chain/internal/statecheck/statecheck.go",
        "if r.Gate.Row == 5 || r.Gate.Row == 6 {",
        "if r.Gate.Row == 5 {",
        K("chain/internal/statecheck", "TestStateCheckWritesNothing", "TestStatecheckCommand"),
        "a gate row 6 refusal fails the report"),
    one("p3c-readwrite-open", STATECHECK, "chain/internal/statecheck/open.go",
        "readOnly.SetBool(true)",
        "readOnly.SetBool(false)",
        K("chain/internal/statecheck", "TestOpenReadOnlyRefusesWrites", "TestStateCheckWritesNothing"),
        "the home's database is opened read-only"),
    one("p3c-i13-first-byte-exempt", STATECHECK, "chain/internal/statecheck/authority.go",
        "if bytes.Equal(it.Key(), []byte{authorityPolicyKey}) {",
        "if it.Key()[0] == authorityPolicyKey {",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the I13 digest leaves out only the exact key {0x04}"),
    one("p3c-i13-includes-policy-version", STATECHECK, "chain/internal/statecheck/authority.go",
        "if bytes.Equal(it.Key(), []byte{authorityPolicyKey}) {",
        "if false {",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the I13 digest leaves out the policy version key"),
    one("p3c-exact-key-rule-dropped", STATECHECK, "chain/internal/statecheck/authority.go",
        "case key[0] == authorityPolicyKey && len(key) != 1:",
        "case false:",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the authority checker refuses a row beside {0x04} (I13 exact-key rule)"),
    one("p3c-retired-prefix-allowed", STATECHECK, "chain/internal/statecheck/authority.go",
        "\tauthorityRetiredService: \"retired service address\",\n",
        "\tauthorityRetiredService: \"retired service address\",\n\t0x13: \"retired governed value\",\n",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the retired governed-value prefix 0x13 is refused"),
    one("p3c-i10-dropped", STATECHECK, "chain/internal/statecheck/authority.go",
        "\tif len(counts) != len(want) {",
        "\tif false {",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "I10: every open count has its proposals"),
    one("p3c-i1-dropped", STATECHECK, "chain/internal/statecheck/authority.go",
        "if version := k.PolicyVersion(ctx); version < 1+executed {",
        "if version := k.PolicyVersion(ctx); version < 1 {",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "I1: the policy version is at least 1 + the executed proposals"),
    one("p3c-i8-retired-validators-dropped", STATECHECK, "chain/internal/statecheck/authority.go",
        "\tfor _, row := range auth.GetValidators() {\n\t\tif retired(row.GetConsensusPubkey()) {",
        "\tfor _, row := range auth.GetValidators()[:0] {\n\t\tif retired(row.GetConsensusPubkey()) {",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "I8: a retired seat key is not a validator key"),
    one("p3c-consensus-version-not-read", STATECHECK, "chain/internal/statecheck/gate.go",
        "\t\tstate.ConsensusAppVersion = params.Version.App",
        "\t\tstate.ConsensusAppVersion = state.AppVersion",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the gate input reads the stored Version.App"),
    one("p3c-layout-store-dropped", STATECHECK, "chain/internal/statecheck/gate.go",
        "\tapp.ModuleLeases,\n\tapp.ModuleEvidence,\n\tauthority.ModuleName,\n}",
        "\tapp.ModuleEvidence,\n\tauthority.ModuleName,\n}",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the gate input reads every layout key"),
    one("p3c-plan-not-read", STATECHECK, "chain/internal/statecheck/gate.go",
        "\tif found {\n\t\tstate.Plan = &plan\n\t}",
        "\t_ = found\n\t_ = plan",
        K("chain/internal/statecheck", "TestStateCheckRefusesOtherVersion"),
        "the gate input reads the stored plan"),
    one("p3c-versions-i5-dropped", STATECHECK, "chain/internal/statecheck/gate.go",
        "\tif state.AppVersion != state.ConsensusAppVersion {\n\t\treturn fmt.Errorf(\"I5:",
        "\tif false {\n\t\treturn fmt.Errorf(\"I5:",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "I5: Version.App equals the stored app version"),
    one("p3c-report-hides-failing-check", STATECHECK, "chain/internal/statecheck/report.go",
        "\t\tfmt.Fprintf(&b, \"check %s FAIL %s\\n\", check.Name, oneLine(check.Err.Error()))",
        "\t\tfmt.Fprintf(&b, \"check %s ok\\n\", check.Name)",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the report prints a failing check as FAIL"),
    one("p3c-panic-not-recovered-as-failure", STATECHECK, "chain/internal/statecheck/statecheck.go",
        "\t\t\tresult.Err = fmt.Errorf(\"panic (a committed row does not decode): %v\", p)",
        "\t\t\t_ = p",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "a checker panic is that check failing"),
    one("p3c-height-ignored", STATECHECK, "chain/internal/statecheck/statecheck.go",
        "\t\terr = chainApp.LoadVersion(height)",
        "\t\terr = chainApp.LoadLatestVersion()",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants", "TestStateCheckRefusesBadInput", "TestStateCheckRefusesOtherVersion"),
        "the report is for the requested height"),
    one("plan-statecheck-tag-registers-nothing", STATECHECK, "chain/cmd/svnd/statecheck_tag.go",
        "\treturn opts, []*cobra.Command{statecheck.Command(opts.Upgrades)}\n",
        "\t_ = statecheck.Command\n\treturn opts, nil\n",
        K("chain/cmd/svnd", "TestStatecheckBuildsReportOnAHome"),
        "ORCH-6: the svn_statecheck build has the statecheck subcommand"),
    one("plan-statecheck-main-skips-registration", STATECHECK, "chain/cmd/svnd/main.go",
        "\troot.AddCommand(commands...)\n",
        "\t_ = commands\n",
        K("chain/cmd/svnd", "TestStatecheckBuildsReportOnAHome"),
        "ORCH-6: main registers the tag's statecheck commands"),
    one("fix-statecheck-i13-skips-policy-rows", STATECHECK, "chain/internal/statecheck/authority.go",
        "\tif bytes.IndexByte(i13SkippedPrefixes, key[0]) >= 0 {",
        "\tif bytes.IndexByte(i13SkippedPrefixes, key[0]) >= 0 || key[0] > policy.PendingPrefix {",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants"),
        "the I13 digest covers every x/policy row the runner's classes protect, not only 0x01 to 0x03"),
    one("fix-statecheck-i13-keeps-applied-rows", STATECHECK, "chain/internal/statecheck/authority.go",
        "\ti13SkippedPrefixes = []byte{policy.VersionMapPrefix, policy.AppliedPrefix}\n",
        "\ti13SkippedPrefixes = []byte{policy.VersionMapPrefix}\n",
        K("chain/internal/statecheck", "TestStateCheckDigestsAndInvariants", "TestStateCheckRefusesOtherVersion"),
        "the I13 digest leaves out the applied rows, which an upgrade's record step extends"),
    one("fix-statecheck-lock-not-required", STATECHECK, "chain/internal/statecheck/open.go",
        "\tif _, err := os.Stat(filepath.Join(path, \"LOCK\")); err != nil {",
        "\tif _, err := os.Stat(path); err != nil {",
        K("chain/internal/statecheck", "TestReadChainIDReadsAsCometBFTDoes"),
        "a database copy without its LOCK file is refused, so the read-only open creates no file (Wave 2 security review)"),
    one("fix-statecheck-stored-genesis-unread", STATECHECK, "chain/internal/statecheck/open.go",
        "\traw, err := db.Get([]byte(node.StoredGenesisKey))\n\tcloseErr := db.Close()\n\tif err != nil {\n\t\treturn \"\", fmt.Errorf(\"read the stored genesis document of %s: %w\", home, err)",
        "\traw, err := []byte(nil), error(nil)\n\tcloseErr := db.Close()\n\tif err != nil {\n\t\treturn \"\", fmt.Errorf(\"read the stored genesis document of %s: %w\", home, err)",
        K("chain/internal/statecheck", "TestReadChainIDReadsAsCometBFTDoes"),
        "the state check compares the genesis document CometBFT stored"),
    # ---------------------------------------------------------------- difftest
    one("p4b-applydue-gone", DIFFTEST, "chain/x/authority/difftest/harness_test.go",
        "\tif err := h.pol.ApplyDue(cache); err != nil {",
        "\tif err := error(nil); err != nil {",
        K("chain/x/authority/difftest", "TestSchemaTwoPayloadsD5"),
        "the harness applies due pending changes each block (I3-I6 reachable)"),
    one("p4b-decisions-not-recorded", DIFFTEST, "chain/x/authority/difftest/harness_test.go",
        "\t\th.decisions[limit.GetLimitKey()] = policytest.Decision{",
        "\t\t_ = policytest.Decision{",
        K("chain/x/authority/difftest", "TestStoreLayout"),
        "the harness records each governed decision for CheckDecisions"),
    one("p4b-i12-off", DIFFTEST, "chain/x/authority/difftest/invariants_test.go",
        "\tallowed := map[string]bool{}\n",
        "\tif true {\n\t\treturn nil\n\t}\n\tallowed := map[string]bool{}\n",
        K("chain/x/authority/difftest", "TestPolicyWriteCheckSelfTest", "TestTrialSnapshotDetectsEarlyWrite"),
        "the I12 policy-write check runs"),
    one("p4b-i12-any-row-on-execute", DIFFTEST, "chain/x/authority/difftest/invariants_test.go",
        "\t\t\tif !bytes.Equal(before[key], after[key]) && !allowed[key] {",
        "\t\t\tif !bytes.Equal(before[key], after[key]) && !allowed[key] && executed == nil {",
        K("chain/x/authority/difftest", "TestPolicyWriteCheckSelfTest"),
        "I12: an execute may change only the rows its payload names"),
    one("p4b-i12-block-ignores-activation", DIFFTEST, "chain/x/authority/difftest/invariants_test.go",
        "\t\t\tif row.Activation <= uint64(h.height) {",
        "\t\t\tif row.Activation > 0 {",
        K("chain/x/authority/difftest", "TestPolicyWriteCheckSelfTest"),
        "I12: a block applies only due pending changes"),
    one("p4b-i6-layout-loop-gone", DIFFTEST, "chain/x/authority/difftest/invariants_test.go",
        "\tfor _, key := range h.keys {\n\t\tlayout,",
        "\tfor _, key := range h.keys[:0] {\n\t\tlayout,",
        K("chain/x/authority/difftest", "TestCouncilInvariantsSelfTest"),
        "the I6 layout loop covers every store"),
    one("p4b-i5-app-version-gone", DIFFTEST, "chain/x/authority/difftest/invariants_test.go",
        "\tif v := h.pol.AppVersion(ctx); v != binaryAppVersion {",
        "\tif v := h.pol.AppVersion(ctx); v != v {",
        K("chain/x/authority/difftest", "TestCouncilInvariantsSelfTest"),
        "the I5 app-version check runs"),
    one("p4b-policytest-gone", DIFFTEST, "chain/x/authority/difftest/invariants_test.go",
        "\tif err := policytest.CheckInvariantsAt(ctx, h.pol, h.auth, policy.ConsensusVersion); err != nil {",
        "\tif err := error(nil); err != nil {",
        K("chain/x/authority/difftest", "TestCouncilInvariantsSelfTest"),
        "the harness runs policytest.CheckInvariantsAt"),
    one("p4b-checkdecisions-gone", DIFFTEST, "chain/x/authority/difftest/invariants_test.go",
        "\tif err := policytest.CheckDecisions(ctx, h.pol, h.decisions); err != nil {",
        "\tif err := error(nil); err != nil {",
        K("chain/x/authority/difftest", "TestCouncilInvariantsSelfTest"),
        "the harness runs policytest.CheckDecisions"),
    one("p4b-g11-side-rng-off", DIFFTEST, "chain/x/authority/difftest/gen_test.go",
        "\t\tif g.side.IntN(2) == 0 {\n\t\t\tcases := shapeCases()",
        "\t\tif g.side.IntN(2) == 2 {\n\t\t\tcases := shapeCases()",
        K("chain/x/authority/difftest", "TestDifferentialReachability"),
        "the generator's side stream sends G-11 payloads"),
    one("p4b-oracle-shape-never", DIFFTEST, "tests/difftest/spec_oracle.py",
        "    if not isinstance(payload, dict) or len(payload) != 1:\n        return None\n    field, body = next(iter(payload.items()))",
        "    return None\n    field, body = next(iter(payload.items()))",
        K("chain/x/authority/difftest", "TestOracleSelfTest", "TestCouncilRulesAreLoadBearing"),
        "the oracle's G-11 shape condition matches its payloads"),
    one("p4b-oracle-c1-any-algorithm", DIFFTEST, "tests/difftest/spec_oracle.py",
        "    if parts is None or parts[0] != SHA256_ALGORITHM:\n        return False\n    return parts[1] is None",
        "    if parts is None:\n        return False\n    return parts[1] is None",
        K("chain/x/authority/difftest", "TestOracleSelfTest"),
        "G-11 condition 1 needs the SHA-256 algorithm"),
    one("p4b-oracle-c2-activation-unchecked", DIFFTEST, "tests/difftest/spec_oracle.py",
        "    if \"activation\" not in body or sm.parse_activation(body[\"activation\"]) is None:\n        return None",
        "    if \"activation\" not in body:\n        return None",
        K("chain/x/authority/difftest", "TestOracleSelfTest"),
        "G-11 condition 2 parses the activation"),
    one("p4b-oracle-c6-extra-keys", DIFFTEST, "tests/difftest/spec_oracle.py",
        "        if not set(body) <= GOVERNED_BODY_KEYS:\n            return None\n        if \"value\" not in body:",
        "        if \"value\" not in body:",
        K("chain/x/authority/difftest", "TestOracleSelfTest"),
        "G-11 condition 6 refuses extra governed body keys"),
    one("p4b-oracle-shape-any-spec-code", DIFFTEST, "tests/difftest/spec_oracle.py",
        "            if failed and spec_code == MALFORMED:\n                payload = spec.get(\"payload\")",
        "            if failed:\n                payload = spec.get(\"payload\")",
        K("chain/x/authority/difftest", "TestOracleSelfTest"),
        "G-11 applies only where the spec answers MALFORMED"),
    one("p4c-open-withdraw-class-dropped", DIFFTEST, "chain/x/authority/difftest/shapes_test.go",
        "\t\t\t\t\topen = openWithdrawAlgorithm\n",
        "\t\t\t\t\topen = \"\"\n",
        K("chain/x/authority/difftest", "TestUnnamedParseDivergences"),
        "the shape space pins the open withdraw-algorithm class"),
    one("p4c-open-no-node-class-dropped", DIFFTEST, "chain/x/authority/difftest/shapes_test.go",
        "\t\t\t\t\t\t\tcase node.value == \"\":\n\t\t\t\t\t\t\t\topen = openShortKeyNoNode\n",
        "\t\t\t\t\t\t\tcase node.value == \"x\":\n\t\t\t\t\t\t\t\topen = openShortKeyNoNode\n",
        K("chain/x/authority/difftest", "TestUnnamedParseDivergences"),
        "the shape space pins the open short-key-without-node class"),
    one("p4c-open-class-too-wide", DIFFTEST, "chain/x/authority/difftest/shapes_test.go",
        "\t\t\t\t\t\t\tcase act.value.omitted:\n",
        "\t\t\t\t\t\t\tdefault:\n",
        K("chain/x/authority/difftest", "TestUnnamedParseDivergences"),
        "an open class is pinned only where it diverges"),
    one("p4c-space-drops-activation-omitted", DIFFTEST, "chain/x/authority/difftest/shapes_test.go",
        "\t\t{\"activation omitted\", activationClass{omitted: true}},\n",
        "",
        K("chain/x/authority/difftest", "TestUnnamedParseDivergences"),
        "the shape space covers an omitted activation"),
    one("p4c-space-drops-algorithm-7", DIFFTEST, "chain/x/authority/difftest/shapes_test.go",
        "\t\t{\"reason algorithm 7\", &typesv1.Digest{Algorithm: unknownAlgorithm, Value: sha(\"r\")}},\n",
        "",
        K("chain/x/authority/difftest", "TestUnnamedParseDivergences"),
        "the shape space covers a reason algorithm outside the enum"),
    one("p4c-oracle-n1-any-field-omitted", DIFFTEST, "tests/difftest/spec_oracle.py",
        "        if \"reasonDigest\" not in body:\n            return \"validator consensus key not 32 bytes, reason digest omitted\"",
        "        if set(body) != VALIDATOR_BODY_KEYS and set(body) != VALIDATOR_BODY_KEYS | {\"power\"}:\n            return \"validator consensus key not 32 bytes, reason digest omitted\"",
        K("chain/x/authority/difftest", "TestOracleSelfTest"),
        "G-11 condition 9 is the reason-digest-omitted case only"),
    one("p4c-oracle-n2-dropped", DIFFTEST, "tests/difftest/spec_oracle.py",
        "    if \"nodeId\" not in body:\n        return \"empty node id\"\n    node_id = body[\"nodeId\"]",
        "    node_id = body.get(\"nodeId\")",
        K("chain/x/authority/difftest", "TestOracleSelfTest", "TestCouncilRulesAreLoadBearing"),
        "G-11 condition 10: empty node id with a 32-byte key"),
    one("p4c-oracle-n4-reason-unchecked", DIFFTEST, "tests/difftest/spec_oracle.py",
        "    if \"contentVersion\" not in body and sm.digest_message(reason) is not None:",
        "    if \"contentVersion\" not in body:",
        K("chain/x/authority/difftest", "TestOracleSelfTest", "TestUnnamedParseDivergences"),
        "G-11 condition 12 needs a reason digest the spec parses"),
    one("p4c-oracle-n3-dropped", DIFFTEST, "tests/difftest/spec_oracle.py",
        "    if \"reasonDigest\" not in body:\n        return \"withdraw content id outside the pattern, reason digest omitted\"\n    reason = body[\"reasonDigest\"]",
        "    if \"reasonDigest\" not in body:\n        return None\n    reason = body[\"reasonDigest\"]",
        K("chain/x/authority/difftest", "TestOracleSelfTest", "TestCouncilRulesAreLoadBearing"),
        "G-11 condition 11: withdraw content id outside the pattern, no reason digest"),
    one("p4c-oracle-n5-dropped", DIFFTEST, "tests/difftest/spec_oracle.py",
        "    if sha256_not_32_bytes(reason):\n        return \"withdraw content id outside the pattern, SHA-256 reason not 32 bytes\"\n",
        "",
        K("chain/x/authority/difftest", "TestOracleSelfTest", "TestCouncilRulesAreLoadBearing"),
        "G-11 condition 13: SHA-256 reason digest not 32 bytes"),
    one("p4-stale-conflict-off", DIFFTEST, "chain/x/authority/engine.go",
        "\t\tif prev != nil && !bytes.Equal(prev, rec.PayloadDigest) {",
        "\t\tif false && prev != nil && !bytes.Equal(prev, rec.PayloadDigest) {",
        K("chain/x/authority/difftest", "TestStaleBranchDifferential"),
        "an execute on an older base whose key executed another digest is CONFLICT, not STALE"),
    one("p4-g-5-off", DIFFTEST, "chain/x/authority/engine.go",
        "\tif len(rec.Approvers)+1 >= ApprovedThreshold && s.Time > unixMax-delay {",
        "\tif false && len(rec.Approvers)+1 >= ApprovedThreshold && s.Time > unixMax-delay {",
        K("chain/x/authority/difftest", "TestCouncilRulesAreLoadBearing"),
        "G-5: an approval whose execute_after would pass the unix bound is OVERFLOW"),
    one("p4-g-3-quota-plus-one", DIFFTEST, "chain/x/authority/engine.go",
        "\tif open >= MaxOpenProposalsPerSeat {",
        "\tif open > MaxOpenProposalsPerSeat {",
        K("chain/x/authority/difftest", "TestCouncilRulesAreLoadBearing"),
        "G-3: the per-seat open quota"),
    one("p4-g-4-cap-plus-one", DIFFTEST, "chain/x/authority/engine.go",
        "\tif len(pending) >= MaxPendingChanges {",
        "\tif len(pending) > MaxPendingChanges {",
        K("chain/x/authority/difftest", "TestCouncilRulesAreLoadBearing"),
        "G-4: the pending validator change cap"),
    # ---------------------------------------------------------------- docs
    one("p6b-unsafe-reset-instruction", DOCS, "docs/operations/planned-halt-and-restart.md",
        "## After the restart",
        "Run `cometbft unsafe-reset-all --home $NODE` first.\n\n## After the restart",
        K("tests/unit/test_operations_docs.py", "test_no_forbidden_step_outside_the_never_list"),
        "the operations doc test refuses: unsafe-reset instruction"),
    one("p6b-missing-test-name", DOCS, "docs/operations/failed-upgrade.md",
        "`TestUpgradeHandlerErrorHalts` (fails",
        "`TestHaltExpiresDueLeases` (fails",
        K("tests/unit/test_operations_docs.py", "test_cited_go_tests_exist"),
        "the operations doc test refuses: missing test name"),
    one("p6b-untagged-bash-block", DOCS, "docs/operations/planned-halt-and-restart.md",
        "```bash walk:copy-at-halt",
        "```bash",
        K("tests/unit/test_operations_docs.py", "test_walked_runbooks_tag_every_bash_block"),
        "the operations doc test refuses: untagged bash block"),
    one("p6b-svnd-command-that-does-not-exist", DOCS, "docs/operations/operator-installation.md",
        "\"$NEW_BIN\" version\ninfo=",
        "\"$NEW_BIN\" upgrade-info\ninfo=",
        K("tests/unit/test_operations_docs.py", "test_cited_svnd_commands_exist"),
        "the operations doc test refuses: svnd command that does not exist"),
    one("p6b-svnd-flag-that-does-not-exist", DOCS, "docs/operations/planned-halt-and-restart.md",
        "start --home \"$NODE\" >>",
        "start --home \"$NODE\" --halt-at 5 >>",
        K("tests/unit/test_operations_docs.py", "test_cited_svnd_commands_exist"),
        "the operations doc test refuses: svnd flag that does not exist"),
    one("p6b-broken-spec-anchor", DOCS, "docs/operations/council-changes.md",
        "#3-council-flow-exact-order",
        "#3-council-flow",
        K("tests/unit/test_operations_docs.py", "test_every_goal10_runbook_links_its_specification_section", "test_links_resolve"),
        "the operations doc test refuses: broken spec anchor"),
    one("p6b-header-removed", DOCS, "docs/operations/source-and-digest-review.md",
        "This is not a production runbook. ",
        "",
        K("tests/unit/test_operations_docs.py", "test_runbooks_exist_and_open_with_the_header"),
        "the operations doc test refuses: header removed"),
    one("p6b-never-list-item-not-a-never", DOCS, "docs/operations/failed-upgrade.md",
        "- Never run two live copies of one validator key.",
        "- Run two live copies of one validator key.",
        K("tests/unit/test_operations_docs.py", "test_no_forbidden_step_outside_the_never_list"),
        "the operations doc test refuses: never-list item not a never"),
    one("p6b-rm-rf-data", DOCS, "docs/operations/failed-upgrade.md",
        "mv \"$NODE/data\" \"$NOTES/data.diverged\"",
        "rm -rf \"$NODE/data\"",
        K("tests/unit/test_operations_docs.py", "test_no_forbidden_step_outside_the_never_list"),
        "the operations doc test refuses: rm -rf data"),
    one("p6b-new-genesis-in-a-block", DOCS, "docs/operations/failed-upgrade.md",
        "test -d \"$NODE_COPY/data\"",
        "svnd devnet init --home \"$SVN_HOME\"",
        K("tests/unit/test_operations_docs.py", "test_no_forbidden_step_outside_the_never_list"),
        "the operations doc test refuses: new genesis in a block"),
    one("p6b-missing-repo-path", DOCS, "docs/operations/release-preparation.md",
        "`chain/go.mod` names",
        "`chain/go.mod.lock` names",
        K("tests/unit/test_operations_docs.py", "test_cited_repository_paths_exist"),
        "the operations doc test refuses: missing repo path"),
    one("p6b-walker-query-kind", DOCS, "docs/operations/council-changes.md",
        "query governance --rpc",
        "query plans --rpc",
        K("tests/unit/test_operations_docs.py", "test_cited_walker_subcommands_exist"),
        "the operations doc test refuses: walker query kind"),
    one("p6b-spec-cites-a-missing-test", DOCS, "docs/architecture/governance-and-upgrades.md",
        "(`TestOnChainActivationIsHeightOnly`)",
        "(`TestPlanSourceCompiled`)",
        K("tests/unit/test_operations_docs.py", "test_cited_go_tests_exist"),
        "the operations doc test refuses: spec cites a missing test"),
    one("plan-docs-unsafe-reset-in-failed-upgrade", DOCS, "docs/operations/failed-upgrade.md",
        "## Fix forward\n",
        "Run `cometbft unsafe-reset-all --home \"$NODE\"` first.\n\n## Fix forward\n",
        K("tests/unit/test_operations_docs.py", "test_no_forbidden_step_outside_the_never_list"),
        "an unsafe-reset-all instruction outside the never list of failed-upgrade.md is refused (plan 16.9 docs row)"),
]

BUILD_FAILURE = re.compile(r"\[(build|setup) failed\]|^# \S+$|cannot find package|undefined: ", re.M)
TOP_FAIL = re.compile(r"^--- FAIL: (\S+) \(", re.M)
TOP_PASS = re.compile(r"^--- PASS: (\S+) \(", re.M)
TOP_SKIP = re.compile(r"^--- SKIP: (\S+) \(", re.M)
PANIC = re.compile(r"^panic: ", re.M)
RUN_LINE = re.compile(r"^=== (?:RUN|CONT)\s+(\S+)", re.M)
GO_RESULT = re.compile(r"^(ok|FAIL)\s+(" + re.escape(GO_MODULE_PREFIX) + r"\S+)")
PYTEST_RESULT = re.compile(
    r"^(\S+?\.py)::(\w+)(?:\[.*?\])? (PASSED|FAILED|ERROR|SKIPPED|XFAIL|XPASS)\b", re.M)
TEST_FUNC = re.compile(r"^func ((?:Test|Fuzz)\w+)\(", re.M)
PYTEST_FUNC = re.compile(r"^def (test_\w+)\(", re.M)


def repo_root() -> pathlib.Path:
    here = pathlib.Path(__file__).resolve().parent
    out = subprocess.run(["git", "-C", str(here), "rev-parse", "--show-toplevel"],
                         capture_output=True, text=True, check=True)
    return pathlib.Path(out.stdout.strip())


def git(root: pathlib.Path, *args: str) -> str:
    return subprocess.run(["git", "-C", str(root), *args], capture_output=True, text=True, check=True).stdout.strip()


def export(root: pathlib.Path, rev: str, dest: pathlib.Path) -> None:
    raw = subprocess.run(["git", "-C", str(root), "archive", "--format=tar", rev],
                         capture_output=True, check=True).stdout
    with tarfile.open(fileobj=io.BytesIO(raw), mode="r:") as tar:
        tar.extractall(dest, filter="data")


def copy_worktree(root: pathlib.Path, dest: pathlib.Path) -> int:
    """Copy every tracked or untracked, not ignored, file of the working tree
    that exists on disk into dest, as it is on disk. Returns the file count.
    A symbolic link is copied as a link."""
    argv = ["git", "-C", str(root), "ls-files", "-z", "--cached", "--others", "--exclude-standard"]
    raw = subprocess.run(argv, capture_output=True, check=True).stdout
    count = 0
    for name in sorted({n for n in raw.decode().split("\0") if n}):
        src = root / name
        if not src.is_symlink() and not src.is_file():
            continue  # deleted in the working tree, or a submodule directory
        target = dest / name
        target.parent.mkdir(parents=True, exist_ok=True)
        shutil.copy2(src, target, follow_symlinks=False)
        count += 1
    return count


def syntax_problem(path: str, text: str) -> str:
    """A mutated Python or JSON file must still compile or parse, so that a
    syntax error is never counted as a kill. Returns "" when it does."""
    try:
        if path.endswith(".py"):
            compile(text, path, "exec")
        elif path.endswith(".json"):
            json.loads(text)
    except (SyntaxError, ValueError) as exc:
        return f"{type(exc).__name__}: {exc}"
    return ""


def mutate(copy: pathlib.Path, m: Mutant) -> tuple[dict[str, str], list[str]]:
    """Return the mutated text of every file m edits, and the problems.

    The edits apply in order. Each search string must occur exactly once in
    its file as it is when that edit is applied. Nothing is written.
    """
    texts: dict[str, str] = {}
    problems: list[str] = []
    for i, e in enumerate(m.edits):
        target = copy / e.path
        if e.path not in texts:
            if not target.is_file():
                problems.append(f"MISSING_FILE {m.name} edit={i} {e.path}")
                continue
            texts[e.path] = target.read_text()
        n = texts[e.path].count(e.old)
        if n != 1:
            problems.append(f"{'NOT_FOUND' if n == 0 else 'NOT_UNIQUE'} {m.name} edit={i} {e.path} occurrences={n}")
            continue
        if e.old == e.new:
            problems.append(f"NO_CHANGE {m.name} edit={i}")
        texts[e.path] = texts[e.path].replace(e.old, e.new, 1)
    for path, text in texts.items():
        if detail := syntax_problem(path, text):
            problems.append(f"SYNTAX {m.name} {path} {detail}")
    return texts, problems


def defined_tests(copy: pathlib.Path, killer: Killer, cache: dict[str, set[str]]) -> set[str]:
    """The Go test and fuzz functions of a package directory's _test.go
    files, or the pytest test functions of a file, in the copy."""
    if killer.package not in cache:
        names: set[str] = set()
        target = copy / killer.package
        if killer.pytest:
            if target.is_file():
                names = set(PYTEST_FUNC.findall(target.read_text()))
        elif target.is_dir():
            for path in sorted(target.glob("*_test.go")):
                names |= set(TEST_FUNC.findall(path.read_text()))
        cache[killer.package] = names
    return cache[killer.package]


def validate(copy: pathlib.Path, mutants: list[Mutant]) -> list[str]:
    problems: list[str] = []
    names: set[str] = set()
    tests: dict[str, set[str]] = {}
    for m in mutants:
        if m.name in names:
            problems.append(f"DUPLICATE_NAME {m.name}")
        names.add(m.name)
        if m.group not in GROUPS:
            problems.append(f"UNKNOWN_GROUP {m.name} {m.group}")
        if not m.edits:
            problems.append(f"NO_EDIT {m.name}")
        if not m.killers:
            problems.append(f"NO_KILLER {m.name}")
        for k in m.killers:
            if k.name == "TestMain" or k.name not in defined_tests(copy, k, tests):
                problems.append(f"KILLER_NOT_DEFINED {m.name} {k.label()}")
        problems.extend(mutate(copy, m)[1])
    return problems


def commands(killers: tuple[Killer, ...], python: str, timeout: str = GO_TEST_TIMEOUT) -> list[list[str]]:
    """The go test command of the Go killers and the pytest command of the
    pytest killers, each only when there is such a killer."""
    out: list[list[str]] = []
    go = [k for k in killers if not k.pytest]
    py = [k for k in killers if k.pytest]
    if go:
        run = "^(" + "|".join(sorted({k.name for k in go})) + ")$"
        packages = ["./" + p + "/" for p in sorted({k.package for k in go})]
        out.append(["go", "test", "-count=1", "-p", "2", "-v", "-timeout", timeout, "-run", run, *packages])
    if py:
        nodes = sorted({f"{k.package}::{k.name}" for k in py})
        # The repository's pytest addopts hold -q, which would cancel -v and
        # print no per-test result line; -o addopts= clears them.
        out.append([python, "-m", "pytest", "-p", "no:cacheprovider", "-o", "addopts=", "-v", "--tb=short", *nodes])
    return out


def go_segments(out: str) -> dict[str, str]:
    """Split go test output into the output of each package, keyed by the
    repository-relative package directory. go test buffers the output of
    each package when it tests more than one, so a package's output ends
    with its `ok` or `FAIL` line."""
    segments: dict[str, str] = {}
    buf: list[str] = []
    for line in out.splitlines(keepends=True):
        m = GO_RESULT.match(line)
        if m:
            pkg = m.group(2).removeprefix(GO_MODULE_PREFIX)
            segments[pkg] = segments.get(pkg, "") + "".join(buf) + line
            buf = []
        else:
            buf.append(line)
    if buf:
        segments[""] = segments.get("", "") + "".join(buf)
    return segments


def go_failed(out: str, killers: list[Killer]) -> list[str]:
    """The named Go killers that failed in their own package's output."""
    segments = go_segments(out)
    failed: list[str] = []
    for k in killers:
        seg = segments.get(k.package, "")
        if k.name in TOP_FAIL.findall(seg):
            failed.append(k.label())
            continue
        panic = PANIC.search(seg)
        if panic:
            runs = RUN_LINE.findall(seg[:panic.start()])
            if runs and runs[-1].split("/")[0] == k.name:
                failed.append(k.label() + "(panic)")
    return failed


def pytest_results(out: str) -> list[tuple[str, str, str]]:
    return [(path, name, status) for path, name, status in PYTEST_RESULT.findall(out)]


def pytest_failed(out: str, killers: list[Killer]) -> list[str]:
    wanted = {(k.package, k.name): k for k in killers}
    failed: list[str] = []
    for path, name, status in pytest_results(out):
        k = wanted.get((path, name))
        if k is not None and status in ("FAILED", "ERROR") and k.label() not in failed:
            failed.append(k.label())
    return failed


@dataclass
class Outcome:
    verdict: str
    codes: list[int]
    out: str
    secs: float
    failed: list[str]


def run(copy: pathlib.Path, killers: tuple[Killer, ...], env: dict[str, str], python: str,
        timeout: int, go_timeout: str = GO_TEST_TIMEOUT) -> Outcome:
    t0 = time.monotonic()
    verdicts: list[str] = []
    codes: list[int] = []
    parts: list[str] = []
    failed: list[str] = []
    for argv in commands(killers, python, go_timeout):
        is_go = argv[0] == "go"
        mine = [k for k in killers if k.pytest != is_go]
        parts.append(f"=== COMMAND {' '.join(argv)}\n")
        try:
            p = subprocess.run(argv, cwd=copy, env=env, capture_output=True, text=True, timeout=timeout)
        except subprocess.TimeoutExpired as exc:
            raw = exc.stdout or b""
            parts.append(raw.decode(errors="replace") if isinstance(raw, bytes) else raw)
            verdicts.append("TIMEOUT")
            codes.append(-1)
            continue
        out = p.stdout + p.stderr
        parts.append(out)
        codes.append(p.returncode)
        hit = go_failed(out, mine) if is_go else pytest_failed(out, mine)
        failed.extend(hit)
        if p.returncode == 0:
            verdicts.append("SURVIVED")
        elif is_go and BUILD_FAILURE.search(out):
            verdicts.append("BUILD_FAILED")
        elif hit:
            verdicts.append("KILLED")
        else:
            verdicts.append("UNCLEAR")
    return Outcome(combine(verdicts), codes, "".join(parts), time.monotonic() - t0, failed)


def combine(verdicts: list[str]) -> str:
    for verdict in ("BUILD_FAILED", "KILLED", "TIMEOUT", "UNCLEAR"):
        if verdict in verdicts:
            return verdict
    return "SURVIVED"


def group_killers(mutants: list[Mutant], group: str) -> tuple[Killer, ...]:
    seen: dict[Killer, None] = {}
    for m in mutants:
        if m.group == group:
            for k in m.killers:
                seen.setdefault(k, None)
    return tuple(seen)


def control_problems(killers: tuple[Killer, ...], o: Outcome) -> list[str]:
    problems = [f"command {i} exit={rc}" for i, rc in enumerate(o.codes) if rc != 0]
    if o.verdict == "TIMEOUT":
        problems.append("timeout")
    segments = go_segments(o.out)
    results = pytest_results(o.out)
    for k in killers:
        if k.pytest:
            mine = [status for path, name, status in results if (path, name) == (k.package, k.name)]
            if not mine:
                problems.append(f"{k.label()} did not run")
            for status in sorted(set(mine) - {"PASSED"}):
                problems.append(f"{k.label()} {status}")
            continue
        seg = segments.get(k.package, "")
        if k.name not in TOP_PASS.findall(seg):
            problems.append(f"{k.label()} did not run and pass at top level")
        if k.name in TOP_SKIP.findall(seg):
            problems.append(f"{k.label()} skipped")
    return problems


def main() -> int:
    ap = argparse.ArgumentParser(description=(__doc__ or "").splitlines()[0])
    ap.add_argument("--rev", default=None, help="revision to git archive (default HEAD)")
    ap.add_argument("--worktree", action="store_true",
                    help="copy the working tree (uncommitted and untracked, not ignored, files) "
                    "instead of --rev")
    ap.add_argument("--group", default="", help="comma-separated groups to run (default: all)")
    ap.add_argument("--only", default="", help="comma-separated mutant names")
    ap.add_argument("--check", action="store_true", help="validate search strings and killers only")
    ap.add_argument("--list", action="store_true", help="list the mutants only")
    ap.add_argument("--logdir", default="", help="write each command's full output here")
    ap.add_argument("--keep", action="store_true", help="keep the temporary copy and GOCACHE")
    ap.add_argument("--gocache", default="", help="GOCACHE to use instead of a fresh one in the copy")
    ap.add_argument("--timeout", type=int, default=1200, help="seconds per command")
    args = ap.parse_args()

    if args.list:
        for m in MUTANTS:
            files = ",".join(dict.fromkeys(e.path for e in m.edits))
            killers = " ".join(k.label() for k in m.killers)
            limit = f" go_timeout={m.timeout}" if m.timeout != GO_TEST_TIMEOUT else ""
            print(f"{m.group:18} {m.name} {files} killers={killers}{limit}")
        return 0
    root = repo_root()
    if args.worktree and args.rev is not None:
        print("--worktree and --rev are exclusive")
        return 2
    groups = [g for g in args.group.split(",") if g]
    unknown_groups = set(groups) - set(GROUPS)
    if unknown_groups:
        print(f"unknown groups: {sorted(unknown_groups)}")
        return 2
    rev = args.rev or "HEAD"
    commit = git(root, "rev-parse", f"{rev}^{{commit}}")
    tree = git(root, "rev-parse", f"{rev}^{{tree}}")
    only = {n for n in args.only.split(",") if n}
    unknown = only - {m.name for m in MUTANTS}
    if unknown:
        print(f"unknown mutant names: {sorted(unknown)}")
        return 2
    selected = [m for m in MUTANTS if (not only or m.name in only) and (not groups or m.group in groups)]
    if not selected:
        print("no mutant selected")
        return 2

    work = pathlib.Path(tempfile.mkdtemp(prefix="goal10-mutants-"))
    if work.resolve().is_relative_to(root.resolve()):
        print(f"temporary directory {work} is inside the repository")
        return 2
    copy = work / "tree"
    copy.mkdir()
    logdir = pathlib.Path(args.logdir).resolve() if args.logdir else None
    if logdir:
        logdir.mkdir(parents=True, exist_ok=True)
    print(f"revision {rev} commit {commit} tree {tree}")
    if args.worktree:
        argv = ["git", "-C", str(root), "status", "--porcelain", "--untracked-files=all"]
        out = subprocess.run(argv, capture_output=True, text=True, check=True).stdout
        status = out.splitlines()
        print(f"copy {copy} (working tree of {root}, not a committed revision; "
              f"git status --porcelain: {len(status)} entries)")
        for line in status:
            print(f"    {line}")
    else:
        print(f"copy {copy} (git archive)")
    counts = ", ".join(f"{g}={sum(1 for m in selected if m.group == g)}" for g in GROUPS)
    print(f"mutants {len(selected)} of {len(MUTANTS)} ({counts})")
    t_start = time.monotonic()
    try:
        if args.worktree:
            print(f"copied {copy_worktree(root, copy)} files")
        else:
            export(root, commit, copy)
        problems = validate(copy, selected)
        for line in problems:
            print(line)
        if problems:
            print(f"RESULT validation problems={len(problems)}; no test was run")
            return 1
        print("search strings: every one occurs exactly once; every named killer is defined")
        if args.check:
            return 0

        gocache = pathlib.Path(args.gocache).resolve() if args.gocache else work / "gocache"
        env = dict(os.environ, GOPROXY="off", GOCACHE=str(gocache))
        # pytest and the difftest oracle run under <repo>/.venv/bin/python when
        # it exists. The copy has no .venv, so point the oracle
        # ($SVN_SPEC_PYTHON) at the repository's interpreter, as in the
        # worktree.
        venv = root / ".venv" / "bin" / "python"
        python = str(venv) if venv.is_file() else sys.executable
        if not env.get("SVN_SPEC_PYTHON") and venv.is_file():
            env["SVN_SPEC_PYTHON"] = str(venv)
        version = subprocess.run(["go", "version"], cwd=copy, capture_output=True, text=True, env=env)
        print(f"go {version.stdout.strip()} {version.stderr.strip()}".rstrip())
        print(f"python {python}")
        print(f"SVN_SPEC_PYTHON={env.get('SVN_SPEC_PYTHON', '')}")
        print(f"GOCACHE={gocache}{' (given)' if args.gocache else ' (fresh, in the copy)'}")
        used = [g for g in GROUPS if any(m.group == g for m in selected)]
        controls = {g: group_killers(selected, g) for g in used}
        for group in used:
            for argv in commands(controls[group], python):
                print(f"control command {group}: {' '.join(argv)}")

        group_secs: dict[str, float] = {}
        for group in used:
            o = run(copy, controls[group], env, python, args.timeout)
            group_secs[group] = group_secs.get(group, 0.0) + o.secs
            if logdir:
                (logdir / f"control-{group}.txt").write_text(o.out)
            bad_control = control_problems(controls[group], o)
            print(f"CONTROL {group} {'FAIL' if bad_control else 'PASS'} exit={o.codes} secs={o.secs:.1f} "
                  f"named_tests={len(controls[group])}", flush=True)
            if bad_control:
                for line in bad_control:
                    print(f"    {line}")
                print(o.out[-4000:])
                print("RESULT control failed; mutants not run")
                return 1

        tally: dict[str, int] = {}
        per_group: dict[str, dict[str, int]] = {}
        bad = []
        for m in selected:
            texts, problems = mutate(copy, m)
            if problems:  # validated above; a second miss means the copy changed
                print("\n".join(problems))
                return 1
            originals = {path: (copy / path).read_text() for path in texts}
            try:
                for path, text in texts.items():
                    (copy / path).write_text(text)
                o = run(copy, m.killers, env, python, args.timeout, m.timeout)
            finally:
                for path, text in originals.items():
                    (copy / path).write_text(text)
            group_secs[m.group] = group_secs.get(m.group, 0.0) + o.secs
            tally[o.verdict] = tally.get(o.verdict, 0) + 1
            row = per_group.setdefault(m.group, {})
            row[o.verdict] = row.get(o.verdict, 0) + 1
            if logdir:
                (logdir / f"{m.name}.txt").write_text(o.out)
            panic = " panic=yes" if PANIC.search(o.out) else ""
            files = ",".join(dict.fromkeys(e.path for e in m.edits))
            print(f"{o.verdict} {m.name} group={m.group} exit={o.codes} secs={o.secs:.1f} file={files}{panic} "
                  f"failing={' '.join(o.failed) or '-'}", flush=True)
            print(f"    reason: {m.reason}", flush=True)
            if o.verdict != "KILLED":
                bad.append(m.name)
                others = sorted(set(TOP_FAIL.findall(o.out)) - {k.name for k in m.killers})
                if others:
                    print(f"    other failing tests (not named killers): {' '.join(others)}", flush=True)
                tail = "\n".join("    " + line for line in o.out.strip().splitlines()[-25:])
                print("    output tail:\n" + tail, flush=True)
        for group in used:
            row = per_group.get(group, {})
            summary = " ".join(f"{k.lower()}={v}" for k, v in sorted(row.items()))
            print(f"GROUP {group} secs={group_secs.get(group, 0.0):.1f} {summary}")
        print(f"TOTAL_SECS {time.monotonic() - t_start:.1f}")
        summary = " ".join(f"{k.lower()}={v}" for k, v in sorted(tally.items()))
        print(f"RESULT mutants={len(selected)} {summary}")
        if bad:
            print(f"NOT KILLED: {', '.join(bad)}")
            return 1
        return 0
    finally:
        if args.keep:
            print(f"kept {work}")
        else:
            shutil.rmtree(work, ignore_errors=True)


if __name__ == "__main__":
    sys.exit(main())
