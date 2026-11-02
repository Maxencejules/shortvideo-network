# Goal 06 evidence

Status: VERIFIED

Every playbook acceptance check for goal 06 ran on commit `fb4cde2`, tree `7d07a6d632acaa54da9f71d4f16a63d73052df38`, and passed (see [Acceptance](#acceptance)). Commit `90652c6`, tree `ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467`, then changed only four test files in `chain/devnet/`, to fix the devnet stall check's false failures. The `chain/devnet` checks passed again on it (the `r3b-` logs; see [Round 3 tested revision](#round-3-tested-revision)). The devnet results are single-machine, multi-process runs.

Three requirement rows assigned to goal 06 name a decision in their disposition: REQ-ARCH-006 and REQ-GOV-004 (DEC-008), and REQ-CHAIN-009 (DEC-002). These rows carry out approved answers. They do not wait on an open decision:

- Gate A approved DEC-002 and DEC-008. Each has "Status: APPROVED" in [decision-register.md](../decision-register.md).
- The approved DEC-008 default says "Quorum-loss behavior is unspecified. No person is named." DEC-008 still blocks "Production council parameters and any genesis that names real council keys". Goal 06 needs neither.
- The approved DEC-002 default keeps admission permissioned, makes a reviewed operator registry the mechanism, and calls the development validators development processes. The operator registry is Goal 07.
- [requirements.md](../requirements.md) says: "The 34 BLOCKER rows are later goals that still have to carry out those answers. They are not open questions."

Goal 06 carries out those answers. The tests are listed under [Requirements](#requirements). REQ-GOV-004's upgrade choice is DEFERRED to Goal 10, as the goal 06 page leaves "broader policy/upgrades to Goal 10". This reading comes from the decision register and the requirements matrix. It is not a new owner ruling. The owner can reject it in review; the status would then return to BLOCKED on those three rows, with no code change.

Open decision requests about PROPOSED numbers do not by themselves block VERIFIED when the behaviour is implemented, tested and recorded as PROPOSED. That covers DR-06-03 (32 open proposals per seat) and DR-06-04 (128 pending changes). DR-06-01, DR-06-02, DR-06-05 and DR-06-06 each record current behaviour that is implemented and tested, so they do not block it either.

Two repository checks outside the playbook acceptance list did not pass in this container:

- The unchanged container proof is BLOCKED here (environment): the build container does not trust the session proxy CA. The same script passed as CI's `devnet` job on `fb4cde2`.
- One Python test fails only in this symlinked worktree (environment). It passes in a clean checkout of the same commit.

The contract checker used to fail while the mutation runner `docs/evidence/goal-06/mutants.py` sat in the tree. `PY_ALLOW` in `docs/evidence/goal-01/check_requirements.py` now names that exact path, and both checkers pass with the runner in place ([r3-checkers-allowlisted.txt](logs/goal-06/r3-checkers-allowlisted.txt), the last run). The runner goes into the evidence commit at that path (see [Blockers](#blockers)).

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a production network. Production council keys are not named. The devnet is validator processes on one machine. No independent operator is claimed.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH. GPT-6 Astra was not used in any round.
- Rounds 1 and 2 (`f38c6ef` to `3853e34`):
  - Model that ran: `grok-4.7-high-fast`, on Cursor. Source: cursor-cloud `run-info` field `originalModelName` for `bcId=bc-e644227c-dbbe-5453-bc33-05e9f27aa7ff`.
  - No effort setting was reported for those rounds. `high-fast` is the suffix of the reported model name, not a reported effort setting.
  - They are covered by the owner's 2026-10-31 waiver. [goal-04.md](goal-04.md) records the quote and the time: at 16:21 Toronto, Max ruled "Don't mind the ai model requirement". That file records the ruling for goal 04 only. The scope of goals 04-06 comes from the orchestrator's relay (this round's brief and notes), which is not a repository record. [goal-05.md](goal-05.md) records the same waiver for goal 05, and the round-2 review accepted that shape for goal 06 (NB18).
- Round 3 (code commits `2389948`, `fb4cde2` and `90652c6`, and this evidence):
  - Ran in Claude Code remote session `session_014mwakeb27YrugDfwAx8vXL`. Workflow subagents inherit the session model.
  - AGENTS.md's model-substitution note names Cursor cloud agents. This round ran on Claude Code, a further substitution. DR-06-07 covers it for model allocation.
  - Source: claude-code-remote `get_session`, read at about 21:26 UTC on 2026-11-01. It reported:
    - `configured_model`, `session_context.model` and `external_metadata.last_served_model`: all `claude-opus-5-5`;
    - effort: `session_context.effort_level` = `ultracode`, and `external_metadata.is_ultracode_active` = true;
    - `external_metadata.flag_settings.fastMode` = true. This file records that flag and does not interpret it.
  - The final verifier read `get_session` again at about 22:18 UTC on 2026-11-01 and saw the same values for each of those fields.
  - The stall-check fix (`90652c6`) ran in the same session; its commit carries the `Claude-Session` trailer for `session_014mwakeb27YrugDfwAx8vXL`. The `r3b-` evidence runner read `get_session` at about 00:59 UTC on 2026-11-02 and saw the same values: `configured_model`, `session_context.model` and `external_metadata.last_served_model` all `claude-opus-5-5`; `session_context.effort_level` = `ultracode`; `is_ultracode_active` = true; `flag_settings.fastMode` = true.
  - Limits of that source. It is a tool response, not a `capture.sh` log. It describes the session's settings and its latest turn. This evidence did not scan the full event history for model-fallback notices.
- DR-06-07, model allocation for the fix round: RESOLVED. The owner acknowledged it in this Claude Code session. As recorded in the orchestrator notes:
  - On 2026-11-01, received by this session at about 16:33 UTC, the owner wrote: "I acknowledge the model substitution for DR-06-07".
  - In the next message, received at about 16:34 UTC on 2026-11-01, the owner wrote: "extend the acknowledgement to goals 07 through 30".
- So goal 06 is not BLOCKED on model allocation. Both rulings are about model allocation only. They do not certify correctness. Extra effort does not certify correctness either.

## Round 3 tested revision

| Item | Value | Log |
| --- | --- | --- |
| Tested commit, `r3-` logs | `fb4cde2087269fb9a93900b9a4d11fd24779ced2` | [r3-revision.txt](logs/goal-06/r3-revision.txt) |
| Tested tree, `r3-` logs | `7d07a6d632acaa54da9f71d4f16a63d73052df38` | every `r3-` log prints it |
| Tested commit, `r3b-` logs | `90652c6b6651a4fbdda1344bb91d3c803270c2c6`, the devnet stall-check fix | every `r3b-` log prints it |
| Tested tree, `r3b-` logs | `ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467` | every `r3b-` log prints it |
| Round 3 code commits | `2389948` round 3 fixes (tree `5503c027`); `fb4cde2` voting-power devnet test; `90652c6` devnet stall-check fix (tree `ad5cdcb9`), four `chain/devnet/` test files only | [r3-revision.txt](logs/goal-06/r3-revision.txt); [r3b-diff.txt](logs/goal-06/r3b-diff.txt) |
| Previous PR head | `3853e3430217096c3e65664bc799859942ea451e` (round 2 evidence, tree `901b8614`) | [r3-revision.txt](logs/goal-06/r3-revision.txt) |
| Base | `f3f583e19355a181ef64de874e5cf26679e6ec30` on `main` (goal 05) | [r3-revision.txt](logs/goal-06/r3-revision.txt) |
| Branch, PR | `goal-06`, [PR 9](https://github.com/Maxencejules/shortvideo-network/pull/9), open, not merged. [r3-ci.txt](logs/goal-06/r3-ci.txt) read the head at `fb4cde2`. The round 3 evidence commit `c68113c` followed it, then `90652c6`, which the orchestrator pushed to the branch. | [r3-ci.txt](logs/goal-06/r3-ci.txt); `r3b-` headers for `90652c6` |
| Runtime | Claude Code remote container. Go `go1.26.10`. Commands that start validator processes ran inside a loopback-only network namespace (`netns-run.sh`, a scratch helper: `unshare --net`, `lo` up, no outbound network). That helper is not in the repository; without it the commands are the same `go test` lines. The `r3b-` logs ran in the same container with the same helper; no `r3b-` log prints the Go version. | [r3-mutants.txt](logs/goal-06/r3-mutants.txt) line 10 for Go |

Every log named `r3-*.txt` under `logs/goal-06/` was written by `scripts/evidence/capture.sh`. Each one prints `HEAD fb4cde2...` and `TREE 7d07a6d6...`. Each one prints `DIRTY no`, except two checker logs:

- `r3-checkers-final.txt` ran after the progress-row edits, so its only `DIRTY_PATH` lines are `docs/progress.md` and `docs/progress.tsv`, the two files the checker validates.
- `r3-checkers-verifier.txt` ran after the final verifier added the "Who pauses admissions" section to `docs/operations/validator-lifecycle.md` (REQ-GOV-004). Its `DIRTY_PATH` lines are that file, `docs/progress.md` and `docs/progress.tsv`.

`r3-mutants.txt` and `r3-checkers-allowlisted.txt` ran after those edits. For each run, the three edited files outside `docs/evidence/` (`docs/progress.md`, `docs/progress.tsv`, `docs/operations/validator-lifecycle.md`) were replaced by their `HEAD` content for the whole command and then restored; their sha256 sums matched before and after. So both logs print `DIRTY no`, and `r3-checkers-allowlisted.txt` checked the committed progress rows, not the edited ones. The mutation runner tests a `git archive` of `HEAD`, so those files do not reach it.

The mutation logs:

- `r3-mutants-attempt1.txt` exited 1 because of a runner bug (`go.work requires go >= 1.26.5 (running go 1.24.7; GOTOOLCHAIN=local)`). Its control failed, so no mutant ran.
- `r3-mutants-attempt2.txt` is the 116-mutant run of the earlier runner version, before three of the mutants the round-2 review named (one from B1, two from B2) were added: `RESULT mutants=116 killed=116`, exit 0. It was written as `r3-mutants.txt` and renamed with its bytes unchanged (sha256 `8eb22de0...` before and after).
- `r3-mutants.txt` is the 119-mutant run of the current runner: `RESULT mutants=119 killed=119`, exit 0.

The checker logs:

- `r3-checkers.txt` exited 1 with `ERROR unexpected python: docs/evidence/goal-06/mutants.py`: the runner sat in the tree, and `PY_ALLOW` did not name it.
- `r3-checkers-without-mutants-py.txt`, `r3-checkers-final.txt` and `r3-checkers-verifier.txt` passed because the runner was moved out of the tree for the run. The last two show the move in their COMMAND line; the first does not.
- `r3-checkers-allowlisted.txt` passed with the runner in place, after `PY_ALLOW` gained `docs/evidence/goal-06/mutants.py`.

No code path is dirty in any log. Four logs end non-zero, and all four stay in the record: `r3-mutants-attempt1.txt` (above), and `r3-checkers.txt`, `r3-pytest-worktree.txt` and `r3-containers-unchanged.txt`, whose causes are explained above and in the [Acceptance](#acceptance) tables.

### Second tested commit: `90652c6`

CI run 41362122347 (job `go`) failed in goal 05's `TestFourValidatorsCommitRestartAndQuorum` with `prove_test.go:136: phase two-down node 0 moved from 11 to 12`, threshold 12. That text comes from the orchestrator's brief; no capture log here holds that run. Commit `90652c6` fixes the cause (see [Round 3 CI fix](#round-3-ci-fix-90652c6)).

The six `r3b-*.txt` logs were written by `scripts/evidence/capture.sh` on `90652c6`. Each prints `HEAD 90652c6b6651a4fbdda1344bb91d3c803270c2c6`, `TREE ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467` and `DIRTY no`, and each ends `EXIT:0`. They ran before this round's edits to `docs/`, so none has a `DIRTY_PATH` line. `r3b-race-devnet.txt` passed on its first attempt, so there is no `r3b-race-devnet-attempt1.txt`.

[r3b-diff.txt](logs/goal-06/r3b-diff.txt) runs `git diff --stat fb4cde2087269fb9a93900b9a4d11fd24779ced2 HEAD` outside `docs/`, then the summary line of the same diff inside `docs/`:

```text
 chain/devnet/lifecycle_test.go    |  19 +-
 chain/devnet/power_quorum_test.go |  45 ++-
 chain/devnet/prove_test.go        | 262 +++++++++++++--
 chain/devnet/stall_test.go        | 675 ++++++++++++++++++++++++++++++++++++++
 4 files changed, 959 insertions(+), 42 deletions(-)
 41 files changed, 15541 insertions(+), 51 deletions(-)
```

Outside `docs/`, only those four `chain/devnet/` files changed, and all four are `_test.go` files. No non-test code and no other package changed. The `docs/` line is the round 3 evidence commit `c68113c`, which lies between the two tested commits.

What `r3b-` supersedes, and what stands:

- **Superseded for `chain/devnet`:** the devnet results inside [r3-unit.txt](logs/goal-06/r3-unit.txt) (the six devnet tests), [r3-race.txt](logs/goal-06/r3-race.txt) (the `chain/devnet` line), [r3-run-tests-go.txt](logs/goal-06/r3-run-tests-go.txt) (the `chain/devnet` line) and the three `r3-devnet-*` logs. The `r3b-` logs replace them: [r3b-devnet-unit.txt](logs/goal-06/r3b-devnet-unit.txt), [r3b-devnet-goal05-x5.txt](logs/goal-06/r3b-devnet-goal05-x5.txt), [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt), [r3b-race-devnet.txt](logs/goal-06/r3b-race-devnet.txt) and [r3b-gofmt-vet.txt](logs/goal-06/r3b-gofmt-vet.txt). The `r3-` logs stay cited as the earlier run on `fb4cde2`.
- **Standing for every other package:** the `r3-` results for `chain/app`, `chain/x/authority`, `chain/genesis` and `chain/cmd/svnd`, and for the fuzz, mutation, proto, tidy, Python, Node, policy, SBOM and checker gates, stand. Outside `docs/`, only the four `chain/devnet/` test files changed between the two commits ([r3b-diff.txt](logs/goal-06/r3b-diff.txt)). The mutation gate's six `init-*` mutants mutate `chain/devnet/init.go`, which did not change, but the 119-mutant run was not repeated on `90652c6`. Inside `docs/`, the change between the two commits is the evidence commit `c68113c`, which added the `PY_ALLOW` entry and the mutation runner that [r3-checkers-allowlisted.txt](logs/goal-06/r3-checkers-allowlisted.txt) and [r3-mutants.txt](logs/goal-06/r3-mutants.txt) ran with.
- **Not rerun on `90652c6`:** `run_tests.py --only go`, `go test -race ./chain/...` as a whole, the 119-mutant run and the container proof. For `chain/devnet` the package-level runs above replace the first two.

The round 3 evidence commit `c68113c` changed only paths under `docs/`: this file, `docs/progress.md`, `docs/progress.tsv`, one new section of `docs/operations/validator-lifecycle.md`, the mutation runner `docs/evidence/goal-06/mutants.py`, one `PY_ALLOW` entry and its comment in `docs/evidence/goal-01/check_requirements.py`, and logs under `docs/evidence/logs/goal-06/`. The evidence commit for the `r3b-` logs also changes only paths under `docs/`: this file, `docs/progress.md`, `docs/progress.tsv` and the six `r3b-` logs. `git diff 90652c6 <that commit> -- . ':!docs'` must be empty.

The code commits `2389948`, `fb4cde2` and `90652c6` carry no `Tested-tree` trailer. The evidence commit `c68113c` carries `Tested-commit: fb4cde2087269fb9a93900b9a4d11fd24779ced2` and `Tested-tree: 7d07a6d632acaa54da9f71d4f16a63d73052df38`, for the `r3-` logs. The evidence commit for the `r3b-` logs is to carry `Tested-commit: 90652c6b6651a4fbdda1344bb91d3c803270c2c6` and `Tested-tree: ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467`. In both cases the trailers name the commit and tree the logs ran on, not the evidence commit's own tree (round 2 NB5).

| Prerequisite | Status | Evidence |
| --- | --- | --- |
| Goal 02 | IMPLEMENTED | [goal-02.md](goal-02.md). Protocol v0 stays the frozen candidate. No `.proto` file, `docs/protocol/` file, `docs/protocol.md` or `docs/goals/` file changed since the base ([r3-revision.txt](logs/goal-06/r3-revision.txt), last section empty). Regeneration gives no diff ([r3-proto.txt](logs/goal-06/r3-proto.txt)). |
| Goal 05 | VERIFIED | [goal-05.md](goal-05.md), [PR 8](https://github.com/Maxencejules/shortvideo-network/pull/8), base `f3f583e`. Its devnet proofs pass on `fb4cde2` ([r3-devnet-goal05.txt](logs/goal-06/r3-devnet-goal05.txt)) and on `90652c6` ([r3b-devnet-goal05-x5.txt](logs/goal-06/r3b-devnet-goal05-x5.txt), 5 of 5; [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt)). |
| Authorization and governance contracts | APPROVED at Gate A | DEC-002, DEC-008, DEC-009, DEC-011 and DEC-012 in [decision-register.md](../decision-register.md). The approved DEC-008 default names no council member. The approved DEC-002 default names no operator. |

## Round 3 fixes

This answers the round-2 review of `3853e34`. That review had six blocking items (B1-B6) and eighteen non-blocking items (NB1-NB18), plus a round-1 status list. The table also covers the plan's own findings (X1-X7), the fix-round reviews, and the voting-power addition.

Every `r3-` log cited below is on tree `7d07a6d6`; every `r3b-` log is on tree `ad5cdcb9`. A mutant name refers to [r3-mutants.txt](logs/goal-06/r3-mutants.txt): `RESULT mutants=119 killed=119`, `CONTROL chain PASS`, `CONTROL devnet-init PASS`. Unit tests are in [r3-unit.txt](logs/goal-06/r3-unit.txt).

Statuses: VERIFIED means a test or log on the cited tree shows the fix. IMPLEMENTED means a documentation or comment change with no test. DR, DEVIATION recorded and Follow-up have their usual meaning.

### Blocking items

| Item | Status | Regression test and log |
| --- | --- | --- |
| B1. Reject snapshot not the full store; two rows hit the wrong branch; no exact codes | VERIFIED | `TestRejectedMessagesLeaveTheFullAuthorityState` has exactly 37 rows, and the test fails if the count changes. Each row asserts codespace and code, and compares a length-prefixed dump of every authority key with `bytes.Equal`. The 33 `svn`-codespace rows also require the log to name the protocol code. The four `sdk` rows (`outsider propose`, `corrupt signature`, `stale sequence`, `out of gas`) have no protocol code. Rows now include: `duplicate proposal id, fresh business id` (DUPLICATE_ID); `seat approve of a missing proposal` (NOT_FOUND); `wrong-role approve (authority)` and `wrong-role propose (authority)` / `(service)` (UNAUTHORIZED). The engine test `TestProposalPathsDoNotMutateOnReject` uses the full-store dump. Mutants killed by both reject tests: `swap-engine-301` (DUPLICATE_ID), `swap-engine-304` (EXPIRED) and `swap-engine-181` (NOT_FOUND for a well-formed missing id, as in the `seat approve of a missing proposal` row). `swap-engine-177` (NOT_FOUND for an id that fails the id pattern) is killed only by `TestCheckOrderMatchesSpec` (NB14). [r3-unit.txt](logs/goal-06/r3-unit.txt). The review's mutant that writes an extra `0x02` row in the ante is `ante-extra-admission` in `r3-mutants.txt` (the same one-line write in the ante's admission callback in `chain/app/app.go` as the fix-round scratch copy): `KILLED ante-extra-admission exit=1 ... failing=TestExportRefusesSideState TestRejectedMessagesLeaveTheFullAuthorityState TestZeroFeeTransactionIsMetered TestProviderMessagesAreForbiddenAndDecode TestCouncilIsDecisionBlockedOffDevnet TestEveryProtocolMessageDecodes`. The review (line 38) also named two deletion mutants: delete the duplicate-proposal-id branch, and make the missing-proposal NOT_FOUND branch return ok. They are not in the runner as deletions. The code-swap mutants on those same two branches, `swap-engine-301` (DUPLICATE_ID) and `swap-engine-181` (NOT_FOUND), are killed, which shows each branch is reached and its exact code asserted. |
| B2. State-transition fuzz could not queue | VERIFIED | `FuzzStateTransitions` ran 60 s: `execs: 32678 ... new interesting: 20 (total: 491)`, exit 0 ([r3-fuzz-FuzzStateTransitions.txt](logs/goal-06/r3-fuzz-FuzzStateTransitions.txt)). `TestFuzzCorpusReach` reports seeds plus random sequences: `total:  {Ops:65295 Committed:41667 Queued:383 Executed:40 Rejected:17 Expired:2133 ApplyDueChanged:10 Bounds:1080 Idempotent:307 Refused:23321}` ([r3-corpus-reach.txt](logs/goal-06/r3-corpus-reach.txt)). Mutants killed: `stale-gone` and `stale-swap` (failing `TestFuzzCorpusReach`, but no `FuzzStateTransitions` seed), `sched-future-gone` (failing `TestFuzzCorpusReach` and `FuzzStateTransitions/seed#15`), `ceiling-gone`, `unknown-limit-ok`. The round-2 review named four B2 mutants. Two are in `r3-mutants.txt` and killed:<br>- `exec-conflict-write`, an execute of a proposal that is not QUEUED that writes `State=EXECUTED` and a policy version bump before it returns CONFLICT: `KILLED exec-conflict-write exit=1 ... failing=TestProposalPathsDoNotMutateOnReject TestFuzzCorpusReach FuzzStateTransitions FuzzStateTransitions/seed#10`;<br>- `drop-share-cap`, `powerInvariant` without its `maxPower*3 >= total` clause: `KILLED drop-share-cap exit=1 ... failing=TestShareCapRejects TestFuzzCorpusReach TestValidateGenesisState FuzzStateTransitions FuzzStateTransitions/seed#5 TestInitChainRejectsUnprojectableGenesis TestInitChainRejectsUnprojectableGenesis/power_change_that_breaks_the_share_cap`.<br>The other two, `clone()` returning `s` and truncating `s.Validators` in `ExecuteChange`, targeted the round-2 `State.clone()` and its in-memory validator slice. Round 3 removed both: the state lives in the store, and `s.Validators()` (engine.go:753-754) builds a fresh slice from it. The runner has no direct counterpart, so this file claims no kill for them. |
| B3. Provider handlers wired, rotation undecodable | VERIFIED; DEVIATION recorded (D1) | `RegisterProvider` and `RotateProviderKey` route to `ErrForbidden` again ([r3-symbols.txt](logs/goal-06/r3-symbols.txt)), like `UpdateProvider` and `DrainProvider`. `svn.types.v1.InnerSignature` and `svn.evidence.v1.EvidenceItem` are registered. `TestProviderMessagesAreForbiddenAndDecode` passes. `TestEveryProtocolMessageDecodes` prints `19 transactions, 10 nested message types`. Engine provider registration and rotation are deleted: no non-test `State.RegisterProvider`, `State.RotateProviderKey`, `rotateDomain`, `registerProvider` or `PrevProvider` remains ([r3-symbols.txt](logs/goal-06/r3-symbols.txt)). Mutant `evidenceitem-unregistered` killed. See [Deviations](#deviations). |
| B4. Heights quoted from another log | VERIFIED | Heights are quoted only from [r3-devnet-lifecycle.txt](logs/goal-06/r3-devnet-lifecycle.txt):<br>- `phase=plan-add height=3 activation=43 effective=45`<br>- `phase=add h-1=42 count=4 h=43 count=4 h+1=44 count=4 h+2=45 count=5`<br>- `phase=power-stall height=45 threshold=45`<br>- `phase=power-recovery up=46 reached=48`<br>- `phase=rotate h+1=89 count=5 oldkey=true h+2=90 count=5 newkey=true`<br>- `phase=remove h+1=131 count=5 still=true h+2=132 count=4`<br>The round-2 restart and recovery heights, which came from a round-1 log, are removed. |
| B5. Lifetime proposal quota | VERIFIED; DR-06-03 and DR-06-04 (PROPOSED numbers) | Unit tests: `TestTerminalProposalsDoNotBlock`, `TestSeatQuotaIsPerSeat`, `TestOpenCountBookkeeping`, `TestStaleProposalsStopCounting`, `TestPendingCap`, `TestCouncilGasDoesNotGrowWithHistory`, `TestSeatQuotaBoundaryCommitted`, `TestCommittedGasDoesNotGrowWithHistory`. App reject row `seat at its open limit proposes` gets BOUNDS. Gas does not grow with history:<br>- `propose gas: 119479 before, 119479 after 301 terminal proposals`;<br>- `history 307 proposal records; baseline {propose:81406 approves:[80073 81915 84033] execute:95628}; after {propose:81406 approves:[80073 81915 84033] execute:95628}`.<br>Mutants killed: `open-count-per-seat-only`, `propose-reads-base-1-count`, `propose-skips-open-read`, `swap-engine-321`, `swap-engine-532`. |
| B6. Export dropped the side document | VERIFIED; DR-06-01, DR-06-02 | See [Export](#export). `TestFreshStateExportRoundTrip` (subtests `no_pending_entry` and `genesis_pending_entry`), `TestExportRefusesSideState` (subtest `unequal_powers`), `TestExportStateRefusesEachPrefix`, `TestExportGenesisPanicsOnSideState`, `TestExportStateChecksAdmissionRows`. Mutants `export-role-presence` and `export-pending-cap-gone` killed. The goal-05 import/export row is scoped to fresh state. |

### Non-blocking items

| Item | Status | Regression test and log |
| --- | --- | --- |
| NB1. Halt hooks in svnd | VERIFIED | `TestLinkedBinaryOmitsTestHooks`. `TestRemovalBindHaltLeavesTheSnapshot` now requires CONFLICT in the FinalizeBlock error, and compares the finalize-branch authority store with the store before the block. Its LastBlockHeight and LastCommitID checks could not fail and were dropped. No non-test `InstallMismatchedRemoval`, `ConsensusSnapshot` or `AuthoritySnapshot` remains ([r3-symbols.txt](logs/goal-06/r3-symbols.txt)). Mutants `halt-writes` and `halt-writes2` killed. |
| NB2. Overflow and delay bounds untested | VERIFIED | `TestApproveThresholdOverflow`, and `TestDevnetDelayBounds` (subtests `minus_one`, `min_int64`, `unix_max_plus_one`, `max_int64`, `zero`, `one`, `unix_max`). Mutant `swap-engine-382` killed. |
| NB3. unit-r2 header did not match the run | VERIFIED | Each `r3-` COMMAND line is the command that ran: one `-count`, and `-v` wherever test names are cited (`r3-unit`, `r3-devnet-*`, `r3-corpus-reach`). |
| NB4. Stale comment; test-only `ParsePayloadJSON` | VERIFIED | `ParsePayloadJSON` is defined only in `payload_test.go`, with no non-test caller ([r3-symbols.txt](logs/goal-06/r3-symbols.txt)). `FuzzParsePayload` ran 60 s: `execs: 69981 ... new interesting: 7 (total: 159)` ([r3-fuzz-FuzzParsePayload.txt](logs/goal-06/r3-fuzz-FuzzParsePayload.txt)). The stale comment change itself has no test. |
| NB5. No CI section; trailer tree | VERIFIED for the CI section ([r3-ci.txt](logs/goal-06/r3-ci.txt)); IMPLEMENTED for the trailer | See [CI](#ci) and the trailer note in [Round 3 tested revision](#round-3-tested-revision). The trailer is on the commit that adds this file, so no log in this file can show it; check it with `git log -1 --format=%B` on that commit. |
| NB6. Rotate count at effective-1 | VERIFIED | `phase=rotate h+1=89 count=5 oldkey=true h+2=90 count=5 newkey=true` ([r3-devnet-lifecycle.txt](logs/goal-06/r3-devnet-lifecycle.txt)). |
| NB7. Requirements rows unmapped | VERIFIED for 13 rows; REQ-GOV-004 VERIFIED in part, IMPLEMENTED (doc) for who pauses admissions, upgrade choice DEFERRED to Goal 10 | See [Requirements](#requirements). New tests `TestAnteRejectsNonAdmittedAccount` and `TestAuthorityMessagesCarryNoMediaFields`. |
| NB8. Genesis entries not projected | VERIFIED | `TestValidateGenesisState`. `TestInitChainRejectsUnprojectableGenesis` has nine subtests, from `removal_that_leaves_three_validators` to `valid_entry_is_applied_at_its_height`. `TestGenesisPendingIsAppliedAtItsHeight`, `TestMixedActivationConflicts`, and `TestValidateGenesisAppliesAuthorityRules` (`svnd genesis validate`). Mutants killed: `svnd-skips-authority-rules`, `initchain-skips-authority-rules`, and the 17 `swap-genesis-*` rows. |
| NB9. Gas grew with history; "fixed gas" claim false | VERIFIED | Same gas lines as B5. `TestMessageKeySets`. The `PrevProvider` growth is gone with provider rotation. The authorization map now describes the 50,000 charge plus per-key store gas. |
| NB10. OVERFLOW above safe max | VERIFIED | `TestPowerTotalAboveSafeMax` (FORBIDDEN, proposal REJECTED). Mutant `swap-engine-722` killed. |
| NB11. Keep outcomes commit with ABCI 0 | DEVIATION recorded, DR-06-05 | `TestKeepOutcomesReportTheirCode` reads the typed `authority_result` event code. |
| NB12. Approvals keyed by key, not seat | VERIFIED | `TestApprovalDedupesBySeatAfterRotation`. Mutant `swap-engine-377` (REPLAY) killed. |
| NB13. Resulting-set checks, quotas, size, sequence, zero fee | VERIFIED | Unit tests: `TestDuplicateConsensusKey`, `TestShareCapRejects`, `TestPowerTotalAboveSafeMax`, `TestPowerOnlyChange`, `TestPowerOnlyChangeCommitted`, `TestHandlerChecksTxSizeAndGas`, `TestZeroFeeTransactionIsMetered`, `TestPendingCap`, `TestSeatQuotaIsPerSeat`. App rows: `tx bytes above 65536` BOUNDS, `stale sequence` sdk 32, `out of gas` sdk 11, `gas limit 0` BOUNDS. Mutants `swap-engine-682` and `swap-engine-728` killed. |
| NB14. Check order differs from the spec | VERIFIED for the moved branches; DEVIATION recorded for the rest | `TestCheckOrderMatchesSpec`. Mutants `swap-engine-177`, `-258`, `-285`, `-350` and `-405` killed. The differences that remain are listed in [Deviations](#deviations). |
| NB15. Seats hold the BROADCASTER role | DR-06-06 | Documented in [authority-authorization.md](../architecture/authority-authorization.md#council-seat-accounts). App row `seat-signed execute` gets FORBIDDEN. |
| NB16. `PowerQuorum` unused; quorum-by-power overclaimed | VERIFIED | `PowerQuorum` is deleted ([r3-symbols.txt](logs/goal-06/r3-symbols.txt)). Quorum by voting power is now observed by `TestDevnetQuorumFollowsVotingPower` (see [Acceptance](#acceptance)). `TestQuorumModelVectors` tests the test's own model only. |
| NB17. Compromised-key and permanent-loss guidance | IMPLEMENTED (doc, no test) | [validator-lifecycle.md](../operations/validator-lifecycle.md), section "Compromised or lost consensus key". |
| NB18. Effort setting and waiver citation | IMPLEMENTED (doc); DR-06-07 RESOLVED | [Model and effort](#model-and-effort). The sources are a `get_session` response and the orchestrator notes, not capture logs. |

### Round-1 status list

| Round-1 item | Round 3 |
| --- | --- |
| 1 reject compare | B1 |
| 2 transition fuzz | B2 |
| 3 effective-1 sets | VERIFIED again: B4 lines |
| 4 unlogged numbers | B4 |
| 5 provider handlers | B3 |
| 6 stale comments | VERIFIED; the last one is NB4 |
| 7 expiry before `QUEUED` | Follow-up (spec). Matches the spec: `op_execute` checks expiry before `QUEUED`. No change in the chain. See [Follow-ups](#follow-ups). |
| 8 JSON scrape, no-op delete | VERIFIED; `ParsePayloadJSON` is test-only (NB4) |
| 9 delay bound | NB2 |
| 10 halt at app level | NB1 |
| 11 seat wording | Unchanged; DEC-008 four seats, three approvals |
| 12 progress link, goal 05 item 4 | Unchanged |
| 13 head CI | NB5, [CI](#ci) |

### Plan findings X1-X7

| Item | Status | Regression test and log |
| --- | --- | --- |
| X1. `bounds.go` claimed a drift test that did not exist | VERIFIED | `TestBoundsMatchProtocolFile` |
| X2. Genesis accepted `approved_council`, non-UNSET `quorum_loss`, proposals | VERIFIED | `TestValidateGenesisState`. `TestInitChainRejectsUnprojectableGenesis` subtests `genesis_proposal` and `approved_council`. `TestValidateGenesisAppliesAuthorityRules`. |
| X3. No `safe_integer_max` bound on total genesis power | VERIFIED | `TestValidatorPowerTotalBound` (seven subtests) |
| X4. Stale READMEs and app comments | IMPLEMENTED (doc, no test) | `chain/x/authority/README.md`, `chain/README.md`, `chain/app/app.go` |
| X5. Spec `op_execute` emits `PolicyChanged`; the chain does not | Follow-up | No `PolicyChanged` in non-test chain code ([r3-symbols.txt](logs/goal-06/r3-symbols.txt)). |
| X6. Genesis pending entry and council proposal share a key | VERIFIED | `TestPendingKeyCollisionConflicts`. Mutant `swap-engine-524` killed. |
| X7. `genesis.Parse` accepted trailing JSON | VERIFIED | `TestParseRejectsTrailingData` (four subtests) |

### Fix-round review findings

| Finding | Status | Regression test and log |
| --- | --- | --- |
| Evidence-phase items still open (first review and re-review, blocking) | IMPLEMENTED in this evidence, for review | This file, `docs/progress.md` row 06 and `docs/progress.tsv` row 06 |
| `svnd genesis validate` accepted states that InitChain refuses | VERIFIED | `TestValidateGenesisAppliesAuthorityRules`. Mutants `svnd-skips-authority-rules` and `initchain-skips-authority-rules` killed. |
| Dead `QUEUED` proposals held their seat's slot until expiry | VERIFIED for stale-base proposals; DR-06-03 for current-version ones | `TestStaleProposalsStopCounting`, `TestSeatQuotaBoundaryCommitted`. Mutants `open-count-per-seat-only` and `propose-reads-base-1-count` killed. A current-version proposal that never executes still holds its slot until any execute or its marked expiry (DR-06-03). |
| 128 far-future pending entries freeze later changes | DR-06-04 (disclosed) | `TestPendingCap`, `TestPendingOverCapIsIntegrity` |
| `MsgSubmitAuditBatch` with items failed decode | VERIFIED | `TestEveryProtocolMessageDecodes`. Mutant `evidenceitem-unregistered` killed. |
| `RotateCouncilSeat` left the old seat admitted | VERIFIED; residual risk | `TestSeatRotationMovesAdmission`. Mutants `seat-rotation-keeps-admission` and `seat-rotation-no-submitter-dup` killed. No auth account is created for the new address. |
| Stale and conflicting execute branches untested | VERIFIED | `TestStaleAndConflictingExecutes`. App rows `execute on an older base` (STALE) and `execute on an older base, parameter since executed with another digest` (CONFLICT). Mutants `stale-gone`, `stale-swap`, `swap-engine-441` and `swap-engine-443` killed. |
| DEC-008 council gate untested | VERIFIED | `TestCouncilGateIsDecisionBlocked`, `TestCouncilIsDecisionBlockedOffDevnet`. Mutants `council-open`, `swap-engine-124` and `swap-engine-127` killed. |
| Propose and projection reject classes untested | VERIFIED | App rows `parameter key not bound to the payload`, `digest algorithm unspecified`, `expiry 0`, `expiry one above unix max`, and the two same-height execute rows. `TestSameActivationGroupConflicts`. Mutants `paramkey-gone`, `expiry-bounds-gone`, `algo-gone`, `samenode-gone` and `samekey-gone` killed. |
| Halt test assertions could not fail | VERIFIED | `TestRemovalBindHaltLeavesTheSnapshot`. Mutants `halt-writes` and `halt-writes2` killed. |
| "Touches only the keys its fields name" was false | VERIFIED | `TestMessageKeySets`. Mutant `approve-reads-open` killed. |
| Old-devnet-home paragraph overstated the halt | VERIFIED | `TestOldLayoutConfigIsIntegrity`, `TestLegacySideDocumentHalts`. Mutant `oldlayout-gone` killed. |
| goal-05 row linked to a missing goal-06 section | VERIFIED | The row links `../architecture/authority-authorization.md#export`, and that heading exists ([r3-symbols.txt](logs/goal-06/r3-symbols.txt)). This file also has an [Export](#export) section. |
| Dead code: `business()` empty-id arm, `State.PolicyVersion` | VERIFIED | `State.PolicyVersion` is defined only in `helpers_test.go` ([r3-symbols.txt](logs/goal-06/r3-symbols.txt)). The removed arm has no test. |
| Quorum helper test claimed too much | VERIFIED | Renamed `TestQuorumModelVectors`. Lifecycle lines print `processes=` from the stopped set and `model_quorum=` for the model. |
| "Fails on the old code" comment false | IMPLEMENTED (comment, no test) | `chain/x/authority/engine_test.go` |
| InnerSignature comment listed six carriers | IMPLEMENTED (comment, no test) | `chain/app/encoding.go` |
| Re-review: `DocumentFromModules` doc comment misplaced | IMPLEMENTED (comment, no test) | `chain/app/state.go`; [r3-gofmt-vet.txt](logs/goal-06/r3-gofmt-vet.txt) exit 0 |
| Re-review: slot disclosure listed too few codes | IMPLEMENTED (doc, no test) | `codes.go` says "any code other than FORBIDDEN". The x/authority README and the authorization map say "any code other than `ERROR_CODE_FORBIDDEN`". |
| Re-review: rotation reject branches unreached | VERIFIED | `TestEngineOnlyRotationRejects`. Mutants `seat-bounds`, `seat-notfound`, `service-bounds` and `service-duplicate` killed. |
| Re-review: three defensive branches untested | VERIFIED | `TestApplyDueHaltsOnDamagedEntries`, `TestOldLayoutConfigIsIntegrity`, `TestExportStateChecksAdmissionRows`, `TestPendingOverCapIsIntegrity`. Mutants `applydue-dupkey-gone`, `applydue-notfound`, `oldlayout-gone`, `export-role-presence`, `readpending-cap-gone` and `export-pending-cap-gone` killed. |
| Re-review: `0x14` read and write sets misstated | VERIFIED | `TestMessageKeySets`. Mutants `approve-reads-open` and `propose-skips-open-read` killed. |
| Re-review: "policy messages are registered" | VERIFIED | `TestEveryProtocolMessageDecodes` asserts that `svn.policy.v1` has no Msg service. |
| Re-review: dead `PowerChange.Canonical` and `Digest` | IMPLEMENTED (no test) | The fields are removed from `PowerChange` in `chain/x/authority/payload.go`. No test or symbols check shows the removal. Mutant `canonical-truncates-key` is killed, but it checks that the canonical form keeps the full 32-byte key, not that the fields are gone. |
| Gate: activation height compared as int64 | VERIFIED | `TestActivationHeightBounds`. Mutant `height-int64-compare` killed. |
| Gate: policy-version overflow untested | VERIFIED | `TestPolicyVersionOverflow`. Mutants `version-overflow-gone` and `swap-engine-453` killed. |
| Gate: `chain/go.mod` not tidy at `3853e34` | VERIFIED | Committed in `2389948`: cobra becomes a direct requirement and the unused indirect `otelslog` is dropped. `go mod tidy` then gives no diff ([r3-tidy.txt](logs/goal-06/r3-tidy.txt)). |

### Voting-power addition (`fb4cde2`)

| Item | Status | Regression test and log |
| --- | --- | --- |
| Quorum follows voting power, not node count | VERIFIED, single machine, multi-process | `TestDevnetQuorumFollowsVotingPower` ([r3-devnet-power-vs-count.txt](logs/goal-06/r3-devnet-power-vs-count.txt) on `fb4cde2`; [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt) and [r3b-race-devnet.txt](logs/goal-06/r3b-race-devnet.txt) on `90652c6`); lines in [Acceptance](#acceptance) |
| `devnet.Options.Power` | VERIFIED | `TestInitPowerDefaultIsOne`, `TestInitPowerRejects`, `TestInitPowerTopology` ([r3-devnet-goal05.txt](logs/goal-06/r3-devnet-goal05.txt), [r3-unit.txt](logs/goal-06/r3-unit.txt)). The six `init-*` mutants were killed. |
| Review: case Y inferred its voters from running processes | VERIFIED | Case Y now reads the votes at the stuck height from every running node (`phase=power-case-Y-voters`). After each recovery, every validator process must sign a commit (`phase=power-vs-count-live`). |
| Review: `scan_secrets.py` did not cover the untracked new files | VERIFIED | The files are committed. [r3-scan-secrets.txt](logs/goal-06/r3-scan-secrets.txt): `secret scan ok`. |
| Review: the equal-counts assertion could not fail on its own | IMPLEMENTED (test comment) | The online counts are now observed: commit signers for case X, votes at the stuck height for case Y. A comment in `chain/devnet/power_quorum_test.go` says that the cross-case equality check restates the per-case checks and adds no separate observation. The checks that can fail are each case's observed voters against its running processes, and each case's outcome against the power rule. |
| Review: lifecycle doc boundary wording | IMPLEMENTED (doc) | [validator-lifecycle.md](../operations/validator-lifecycle.md) now says "one third or more" of the power offline leaves "not more than two thirds" online and produces no block, in the key-loss paragraph and under "Loss of quorum". Case Y has exactly one third offline, and it stalls. |
| Review: goal 05 `TestFourValidatorsCommitRestartAndQuorum` flake | VERIFIED, fixed in `90652c6` | See the row "Round 3 CI: goal 05 two-down stall flake (run 41362122347)" under [Round 3 CI fix](#round-3-ci-fix-90652c6). What remains is under [Residual risks](#residual-risks). |

### Round 3 CI fix (`90652c6`)

| Item | Status | Regression test and log |
| --- | --- | --- |
| Round 3 CI: goal 05 two-down stall flake (run 41362122347) | VERIFIED, single machine, multi-process | **Failure.** CI run 41362122347, job `go`: `prove_test.go:136: phase two-down node 0 moved from 11 to 12`, threshold 12. The text comes from the orchestrator's brief; no capture log here holds that run.<br>**Cause: first-sample ceiling.** The old `observeStall` took each node's first sample after the stop as that node's ceiling, and failed on any later rise. Two correct behaviours rise: a node whose RPC lagged the pre-stop high-water mark catches up to a block committed before the stop (11 to 12 under threshold 12), and one block decided before the stop commits after it. The goal 05 two-down phase, the lifecycle power-one-down phase and power case Y all used it.<br>**Fix (`90652c6`, `chain/devnet/prove_test.go`):**<br>- `stopForStall(label, layout, stops, index)` reads the high-water mark over every process that still has a stop function, the one about to stop included, and prints `phase=<label> threshold= app_hash=`. Only then does it stop that process and clear its entry. It returns the threshold, the stopped validator's name, the running RPCs and names, and `stoppedAt`. All three stall phases start through it (`phase=stop-two`, `phase=power-stop-one`, `phase=power-case-<X\|Y>-stop`).<br>- `stallLimit` (pure) decides each sample. A height at or below the limit passes (catch-up). Threshold+1 passes once, and only if the in-flight check accepts its commit; the limit then becomes threshold+1. A higher height, a second block, a missing check, or a limit other than threshold or threshold+1 is an error.<br>- `watchStall` takes six samples one second apart and returns an error; `observeStall` wraps it with `t.Fatal`. It prints `phase=<phase>-in-flight ... signers=` when the in-flight rule is used.<br>- `stoppedSigned` is the in-flight check. Signer rule: the commit carries the stopped validator's `BlockIDFlagCommit` signature, and every signer is the stopped validator or a running one. Power rule: the signers hold more than 2/3 of that height's power. Timestamp rule: the stopped validator's precommit time is no later than `stoppedAt`.<br>**Unit tests** in `chain/devnet/stall_test.go`, against a fake CometBFT RPC (`httptest`): `TestStallLimit` (12 cases), `TestStallLimitWithoutVerifier`, `TestStallLimitSequences` (7, including `CI_41362122347:_node_0_catches_up_11_to_12`), `TestStopForStall`, `TestStopForStallRefuses` (6), `TestStoppedSigned` (12), `TestWatchStall` (12, also replaying the CI shape), `TestWatchStallStatusError`, `TestStopForStallThenObserveStall`, `TestObserveStallFailsTheTest`. [r3b-devnet-unit.txt](logs/goal-06/r3b-devnet-unit.txt): 16 top-level PASS, 49 subtest PASS, 3 SKIP (the devnet tests), 0 FAIL, exit 0. [r3b-gofmt-vet.txt](logs/goal-06/r3b-gofmt-vet.txt): exit 0.<br>**`r3b` tallies:**<br>- [r3b-devnet-goal05-x5.txt](logs/goal-06/r3b-devnet-goal05-x5.txt): `RUN 1 EXIT 0` to `RUN 5 EXIT 0`; 5 PASS, 0 FAIL. `phase=stop-two` thresholds 11, 14, 12, 13 and 18. None of the 60 two-down samples is below its threshold. Run 5 has a live in-flight block: `phase=two-down sample=0 node=0 height=19`, `phase=two-down-in-flight sample=0 node=0 height=19 threshold=18 signers=v1,v2,v3`; node 1 is at 18 in sample 0 and at 19 from sample 1; then `phase=recovery-up height=20 stall=19`. That is the log's only `phase=*-in-flight` line. In runs 1 to 4 every sample equals the threshold.<br>- [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt): 19 top-level PASS, 49 subtest PASS, 0 SKIP, 0 FAIL, `ok ... 161.447s`, exit 0. Every live stall sample equals its threshold: `phase=stop-two threshold=13` (12 of 12 samples at 13), `phase=power-stop-one threshold=45` (18 of 18 at 45), `phase=power-case-Y-stop threshold=121` (18 of 18 at 121). No live in-flight line. Its four `phase=fake-in-flight` lines come from `TestWatchStall` and `TestStopForStallThenObserveStall` on the fake RPC.<br>- [r3b-race-devnet.txt](logs/goal-06/r3b-race-devnet.txt): first attempt; 19 top-level PASS, 0 FAIL, no `DATA RACE`, `ok ... 231.336s`, exit 0. Live in-flight blocks at the other two call sites: `phase=power-one-down-in-flight sample=0 node=0 height=47 threshold=46 signers=v1,v2,v3,v4` (18 of 18 samples at 47, then `phase=power-stall height=47 threshold=46`), and `phase=power-case-Y-in-flight sample=0 node=0 height=124 threshold=123 signers=v1,v2,v3,v4` (18 of 18 samples at 124). Two-down: `phase=stop-two threshold=16`, 12 of 12 samples at 16.<br>**Fix-workflow scratch logs.** These are not committed evidence: they are not in the repository and were not written by `capture.sh`. A fixer, a reviewer and a settle stage in this session wrote them under the session scratch directory, `goal-06-fix/stall-fix/{fix,review,settle}/`.<br>- Reviewer, on the fixer's tree, before `stopForStall`, `watchStall` and the timestamp rule existed. M1 (the stall check runs without stopping v3) was killed live: `height 20 is above limit 17`. M2b (`stallLimit` ignores the check) and M3 (allow two blocks) were killed by unit tests. M2a (`stoppedSigned` returns nil) and M2c (the wrapper drops the check's error) survived the suite and a devnet run; only a reviewer probe outside the suite killed them. M4 (the wrong stopped validator named) was equivalent. M5 and M5b (threshold off by one, either way) survived.<br>- Settle stage, which added `stopForStall`, `watchStall`, the timestamp rule and the fake-RPC tests. Its final sha256 sums of the four changed files equal those of the files in `90652c6` (an uncaptured comparison by this evidence step). Its mutation pass runs `SVN_SKIP_DEVNET=1 go test -count=1 -timeout 180s -v ./chain/devnet/` per mutant. It killed 22 of 22 distinct mutants: the reviewer's eight, re-expressed on the new code, and 14 more. The 14 are: threshold read without the stopped node, threshold read after the stop, no timestamp rule, no signer rule, power check `<`, power check removed, wrapper without `t.Fatal`, three samples, a 100 ms interval, the check sent to the wrong node, an early `stoppedAt`, the stop entry not cleared, the timestamp of the wrong validator, and a wrapper that returns 0. The control passed. The stage's summary says 23 of 23. Its tally lists 22 distinct mutants; the 23rd log is a first M16 build failure that it recorded as invalid, not as a kill, and reran.<br>- Live events: catch-up in the reviewer's `base-prove-run1` (threshold 14; node 1 at 13 in sample 0, then 14) and in the settle stage's `c-prove-run3` (threshold 12; node 1 at 11, then 12). Both are the CI shape, which the old ceiling would have failed. One in-flight block, in the settle stage's `e-race-run1`: `phase=two-down-in-flight sample=0 node=0 height=16 threshold=15 signers=v1,v2,v3`. |

## Acceptance

### Playbook acceptance checks

The acceptance checks and the required handoff on the [goal 06 page](../goals/06-authority-and-validator-lifecycle.md).

| Check | Status | Evidence on tree `7d07a6d6`, and on tree `ad5cdcb9` for `r3b-` logs |
| --- | --- | --- |
| Approved and rejected proposals, repeated votes, stale approvals, unauthorized submitters, wrong signatures, expired execution. No state mutation from a rejected message. | VERIFIED | In [r3-unit.txt](logs/goal-06/r3-unit.txt):<br>- `TestCommittedAuthorityFlowAndReplay` covers the approved path.<br>- `TestRejectedMessagesLeaveTheFullAuthorityState` has 37 rows. Each compares a dump of the whole authority store with `bytes.Equal` and asserts the exact code. Examples: `repeated approval, same seat, new business id` REPLAY; `stale base policy version` and `execute on an older base` STALE; `approve of a QUEUED proposal` CONFLICT; `outsider propose` sdk 9; `wrong-role propose (authority)` UNAUTHORIZED; `corrupt signature` sdk 4.<br>- `TestWrongSignatureDoesNotMutate`.<br>- Expired execution is a spec keep outcome: ABCI 0 with the code in the `authority_result` event (`TestKeepOutcomesReportTheirCode`, `TestExpiredExecuteKeepsMarkWithoutApplying`).<br>- The engine-level `TestProposalPathsDoNotMutateOnReject`.<br>The same packages pass under `-race` ([r3-race.txt](logs/goal-06/r3-race.txt)). |
| On the real devnet, add, rotate and remove a test validator; compare the set at the prescribed heights | VERIFIED, single machine, multi-process | [r3-devnet-lifecycle.txt](logs/goal-06/r3-devnet-lifecycle.txt), `--- PASS: TestDevnetValidatorLifecycle (67.57s)`:<br>`phase=plan-add height=3 activation=43 effective=45`<br>`phase=add h-1=42 count=4 h=43 count=4 h+1=44 count=4 h+2=45 count=5`<br>`phase=rotate h+1=89 count=5 oldkey=true h+2=90 count=5 newkey=true`<br>`phase=remove h+1=131 count=5 still=true h+2=132 count=4`<br>The set at effective-1 is the old set and the set at effective is the new one. The validators are CometBFT processes on loopback in one network namespace.<br>Rerun on `90652c6`: [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt), `--- PASS: TestDevnetValidatorLifecycle (65.63s)`:<br>`phase=plan-add height=3 activation=43 effective=45`<br>`phase=add h-1=42 count=4 h=43 count=4 h+1=44 count=4 h+2=45 count=5`<br>`phase=rotate h+1=89 count=5 oldkey=true h+2=90 count=5 newkey=true`<br>`phase=remove h+1=131 count=5 still=true h+2=132 count=4`<br>The `r3-` log above is the earlier run on `fb4cde2`. |
| Quorum by voting power, not node count | VERIFIED, single machine, multi-process | [r3-devnet-power-vs-count.txt](logs/goal-06/r3-devnet-power-vs-count.txt), `--- PASS: TestDevnetQuorumFollowsVotingPower (77.92s)`.<br>`phase=power-set effective-1=104 powers=v1:10,v2:10,v3:10,v4:10 effective=105 powers=v1:10,v2:10,v3:2,v4:10,v5:1`<br>`phase=power-vs-count case=X offline=v3,v5 online_power=30 total_power=33 online_validators=3 validators=5 count_rule=stall power_rule=progress observed=progress height_from=105 height_to=108 set_height=108 online_from=commit-signers`<br>`phase=power-case-Y-voters node=0 height=123 round=0 voters=v2,v3,v4 stopped=v1 stopped_vote=false voter_power=22 total_power=33` (nodes 1 and 2 print the same)<br>`phase=power-vs-count case=Y offline=v1,v5 online_power=22 total_power=33 online_validators=3 validators=5 count_rule=stall power_rule=stall observed=stall height_from=122 height_to=122 set_height=123 online_from=stuck-height-votes`<br>`phase=power-vs-count-live case=X heights=121..122 signed=v1,v2,v3,v4` and `phase=power-vs-count-live case=Y heights=124..125 signed=v1,v2,v3,v4`<br>Three of five validators are online in both cases, but the outcomes differ, so a count rule is refuted. Case X: the commits at 107 and 108 are signed by v1, v2 and v4 (`signed_power=30 total_power=33`). Case Y depends on CometBFT's unstable `/dump_consensus_state` (see [Residual risks](#residual-risks)).<br>In [r3-devnet-lifecycle.txt](logs/goal-06/r3-devnet-lifecycle.txt), `phase=power-stall height=45 threshold=45` and `phase=power-recovery up=46 reached=48` are observations with equal powers. The `model_quorum=` lines there are a model applied to the RPC set, not an observation.<br>Rerun on `90652c6`: [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt), `--- PASS: TestDevnetQuorumFollowsVotingPower (71.48s)`:<br>`phase=power-set effective-1=104 powers=v1:10,v2:10,v3:10,v4:10 effective=105 powers=v1:10,v2:10,v3:2,v4:10,v5:1`<br>`phase=power-vs-count case=X offline=v3,v5 online_power=30 total_power=33 online_validators=3 validators=5 count_rule=stall power_rule=progress observed=progress height_from=105 height_to=108 set_height=108 online_from=commit-signers`<br>`phase=power-case-Y-voters node=0 height=122 round=0 voters=v2,v3,v4 stopped=v1 stopped_vote=false voter_power=22 total_power=33` (nodes 1 and 2 print the same)<br>`phase=power-vs-count case=Y offline=v1,v5 online_power=22 total_power=33 online_validators=3 validators=5 count_rule=stall power_rule=stall observed=stall height_from=121 height_to=121 set_height=122 online_from=stuck-height-votes`<br>`phase=power-vs-count-live case=X heights=120..121 signed=v1,v2,v3,v4` and `phase=power-vs-count-live case=Y heights=123..124 signed=v1,v2,v3,v4`<br>Case X's commits at 107 and 108 are signed by v1, v2 and v4 (`signed_power=30 total_power=33`). Case Y's stall threshold is `phase=power-case-Y-stop threshold=121`, and all 18 samples are at 121. In the same log, the lifecycle test prints `phase=power-stop-one threshold=45`, `phase=power-stall height=45 threshold=45` and `phase=power-recovery up=46 reached=48`. [r3b-race-devnet.txt](logs/goal-06/r3b-race-devnet.txt) passes the same test under `-race`; there case Y's stall includes the block in flight at the stop, `phase=power-case-Y-in-flight sample=0 node=0 height=124 threshold=123 signers=v1,v2,v3,v4`, and `phase=power-vs-count case=Y ... observed=stall height_from=124 height_to=124 set_height=125`. The `r3-` log above is the earlier run on `fb4cde2`. |
| Fuzz bounded parser and state-transition inputs | VERIFIED | Three runs of 60 s each:<br>- [r3-fuzz-FuzzParsePayload.txt](logs/goal-06/r3-fuzz-FuzzParsePayload.txt): `execs: 69981`, `ok ... 61.857s`.<br>- [r3-fuzz-FuzzStateTransitions.txt](logs/goal-06/r3-fuzz-FuzzStateTransitions.txt): `execs: 32678`, `ok ... 62.063s`.<br>- [r3-fuzz-FuzzProposalPayloadProto.txt](logs/goal-06/r3-fuzz-FuzzProposalPayloadProto.txt): `execs: 111405`, `ok ... 61.111s`.<br>Reach of the transition corpus: [r3-corpus-reach.txt](logs/goal-06/r3-corpus-reach.txt) (B2). |
| Deterministic replay at a fixed committed height | VERIFIED | `TestCommittedAuthorityFlowAndReplay` ([r3-unit.txt](logs/goal-06/r3-unit.txt)) feeds the same signed transactions to two apps. It compares the full authority snapshots after every step and after the activation block, then requires equal `LastCommitID` hashes. On the devnet, every running node reports the same app hash at each sampled height, for example `height=45 app_hash=8D279461...` ([r3-devnet-lifecycle.txt](logs/goal-06/r3-devnet-lifecycle.txt)) and `height=122 app_hash=30D7D7F3...` ([r3-devnet-power-vs-count.txt](logs/goal-06/r3-devnet-power-vs-count.txt)). On `90652c6`, [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt) shows the same agreement, for example `phase=power-one-down sample=0 node=0 height=45 app_hash=EF60AA9A...` and `phase=power-case-Y sample=0 node=0 height=121 app_hash=658E9853...`, with nodes 1 and 2 equal. |
| Required handoff: authority code, lifecycle fixtures, reviewer authorization map; framework and licensing uncertainty flagged | VERIFIED for the documents; flags below | [authority-authorization.md](../architecture/authority-authorization.md), [validator-lifecycle.md](../operations/validator-lifecycle.md), `chain/x/authority/README.md`. Framework flags: DR-06-05 (SDK v0.55 discards a failed message's writes); the module `ExportGenesis` has no error result, so `App.Export` checks first ([Export](#export)); CometBFT v0.40 applies a changeset at height+2; `/dump_consensus_state` is unstable. Licensing: DEC-006 is APPROVED at Gate A. Its default says to "Consider AGPLv3 for original server and application code and Apache-2.0 for original protocol client libraries" and to "Confirm compatibility before applying either". No project `LICENSE` file applies it yet, and Goal 27 release notices stay blocked on that. Of the dependency manifests, only `chain/go.mod` changed since the base, by the tidy normalization; `go.work.sum` gained checksum lines in round 1 ([r3-revision.txt](logs/goal-06/r3-revision.txt)). The SBOM check passes ([r3-sbom.txt](logs/goal-06/r3-sbom.txt): `sbom ok components 1320`). |

### Repository gates

These are the repository's standing checks, run on the same tree. They are not goal 06 acceptance checks. For `chain/devnet`, the `r3b-` logs on `90652c6` supersede the `r3-` results (see [Round 3 tested revision](#round-3-tested-revision)).

| Check | Status | Evidence on tree `7d07a6d6`, and on tree `ad5cdcb9` for `r3b-` logs |
| --- | --- | --- |
| `go test -race ./chain/...` | VERIFIED | [r3-race.txt](logs/goal-06/r3-race.txt): app 29.260s, cmd/svnd 1.493s, devnet 226.871s, genesis 1.421s, x/authority 52.697s, exit 0. One run; no earlier attempt log exists. On `90652c6` only `./chain/devnet/` was rerun under `-race`: [r3b-race-devnet.txt](logs/goal-06/r3b-race-devnet.txt), `ok ... 231.336s`, 19 top-level PASS, 0 FAIL, no `DATA RACE`, exit 0, on the first attempt. The other packages' `-race` results stand from `fb4cde2`. |
| Unit tests with names | VERIFIED | [r3-unit.txt](logs/goal-06/r3-unit.txt), `SVN_SKIP_DEVNET=1`: 90 top-level PASS, 66 subtest PASS, 0 FAIL. Per package: app 28, x/authority 48, genesis 7, cmd/svnd 1, devnet 6. The three devnet tests skipped there ran in the `r3-devnet-*` logs. On `90652c6`, devnet only: [r3b-devnet-unit.txt](logs/goal-06/r3b-devnet-unit.txt), `SVN_SKIP_DEVNET=1`: 16 top-level PASS (the six earlier ones and ten new stall tests), 49 subtest PASS, 3 SKIP (the devnet tests, which ran in the other `r3b-` logs), 0 FAIL, exit 0. |
| Goal 05 devnet proofs on this tree | VERIFIED | [r3-devnet-goal05.txt](logs/goal-06/r3-devnet-goal05.txt): seven tests PASS, including `TestFourValidatorsCommitRestartAndQuorum (17.53s)` with `phase=restart-up height=3`, `phase=stop-two threshold=12`, `phase=recovery-up height=13 stall=12` and recovery samples at height 15. That is the earlier run on `fb4cde2`. On `90652c6`: [r3b-devnet-goal05-x5.txt](logs/goal-06/r3b-devnet-goal05-x5.txt), five runs of `TestFourValidatorsCommitRestartAndQuorum`, `RUN 1 EXIT 0` to `RUN 5 EXIT 0`, with `phase=stop-two threshold=` 11, 14, 12, 13 and 18. [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt) runs all seven goal 05 tests in the package run: `--- PASS: TestFourValidatorsCommitRestartAndQuorum (18.14s)`, with `phase=restart-up height=3`, `phase=stop-two threshold=13`, `phase=recovery-up height=16 stall=13` and recovery samples at height 19. |
| Mutation check | VERIFIED | [r3-mutants.txt](logs/goal-06/r3-mutants.txt): `mutants 119 of 119`, `search strings: every one occurs exactly once`, `CONTROL chain PASS exit=0 secs=112.5`, `CONTROL devnet-init PASS exit=0 secs=4.3`, `RESULT mutants=119 killed=119`, exit 0. The 119 include three mutants the round-2 review named (one from B1, two from B2); the B1 and B2 rows list the ones that are not in the runner and why. The runner is `docs/evidence/goal-06/mutants.py`. It goes into the evidence commit at that path, and `PY_ALLOW` in the goal 01 checker names it (see [Blockers](#blockers)). [r3-mutants-attempt2.txt](logs/goal-06/r3-mutants-attempt2.txt) is the earlier 116-mutant run: `RESULT mutants=116 killed=116`, exit 0. The runner was not rerun on `90652c6`. The stall-check mutants are fix-workflow scratch logs, not committed evidence (see [Round 3 CI fix](#round-3-ci-fix-90652c6)). |
| Repository Go gate | VERIFIED | [r3-run-tests-go.txt](logs/goal-06/r3-run-tests-go.txt) exit 0. [r3-gofmt-vet.txt](logs/goal-06/r3-gofmt-vet.txt) exit 0. [r3-tidy.txt](logs/goal-06/r3-tidy.txt) exit 0. [r3-proto.txt](logs/goal-06/r3-proto.txt): `generated 28 proto files`, no diff. On `90652c6`: [r3b-gofmt-vet.txt](logs/goal-06/r3b-gofmt-vet.txt), `gofmt -l chain/devnet` empty and `go vet ./chain/devnet/`, exit 0. `run_tests.py --only go` was not rerun on `90652c6`; its `chain/devnet` line is replaced by [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt). |
| Python, Node and policy gates | VERIFIED | [r3-ruff-format.txt](logs/goal-06/r3-ruff-format.txt) `33 files already formatted`; [r3-ruff-check.txt](logs/goal-06/r3-ruff-check.txt) `All checks passed!`; [r3-mypy.txt](logs/goal-06/r3-mypy.txt) `no issues found in 24 source files`; [r3-workflow-policy.txt](logs/goal-06/r3-workflow-policy.txt); [r3-scan-secrets.txt](logs/goal-06/r3-scan-secrets.txt); [r3-sbom.txt](logs/goal-06/r3-sbom.txt); [r3-node.txt](logs/goal-06/r3-node.txt) `# pass 1`; [r3-typecheck.txt](logs/goal-06/r3-typecheck.txt) exit 0. |
| `run_tests.py --only python` | VERIFIED in a clean checkout; fails in this worktree for an environment reason | [r3-pytest-clean.txt](logs/goal-06/r3-pytest-clean.txt): `120 passed`, exit 0, with `HEAD fb4cde2`, tree `7d07a6d6`, `DIRTY no`. The evidence runner reports that it ran in a clean detached checkout of `fb4cde2` with no symlinks. The log does not print its working directory; its COMMAND line uses the absolute venv path `/home/user/shortvideo-network/.venv`, unlike the worktree logs. [r3-pytest-worktree.txt](logs/goal-06/r3-pytest-worktree.txt): `1 failed, 119 passed`, exit 1. Only `test_ignore_rules_cover_private_material` fails, on `fatal: pathspec 'node_modules/react/index.js' is beyond a symbolic link`. `node_modules` is a symlink in this worktree. |
| Contract checkers | VERIFIED with the runner in place | [r3-checkers-allowlisted.txt](logs/goal-06/r3-checkers-allowlisted.txt) is the last run. It ran with `docs/evidence/goal-06/mutants.py` in the tree, after `PY_ALLOW` in `docs/evidence/goal-01/check_requirements.py` gained that exact path. Both checkers print `errors 0` and `result PASS`; `self_test_problems 0`, `transition_cases 97`, `valid_vectors 15`, `invalid_vectors 14`, `languages MATCH`; exit 0; `DIRTY no`. The three edited files outside `docs/evidence/` were at their `HEAD` content for that run (see [Round 3 tested revision](#round-3-tested-revision)), so it checked the committed progress rows. The edited progress rows were checked by [r3-checkers-verifier.txt](logs/goal-06/r3-checkers-verifier.txt), after the edits to the progress rows and to `docs/operations/validator-lifecycle.md`, with `mutants.py` moved to a scratch directory by its COMMAND line: `errors 0`, `result PASS`, exit 0; `DIRTY_PATH` lists those three files. Neither checker reads this file. The earlier [r3-checkers-final.txt](logs/goal-06/r3-checkers-final.txt) passed the same way before the lifecycle-doc edit. [r3-checkers.txt](logs/goal-06/r3-checkers.txt): `ERROR unexpected python: docs/evidence/goal-06/mutants.py`, exit 1, before the allowlist entry. The earlier [r3-checkers-without-mutants-py.txt](logs/goal-06/r3-checkers-without-mutants-py.txt) also passed, but its COMMAND line does not show the move. |
| Container proof, unchanged script | BLOCKED (environment) | [r3-containers-unchanged.txt](logs/goal-06/r3-containers-unchanged.txt) exit 1. The build fails at `Dockerfile:9` (`go build -o /out/svnd ./cmd/svnd`) with `x509: certificate signed by unknown authority` from proxy.golang.org: the build container does not trust this session's proxy CA. The script's own inner header says `DIRTY yes` because it counts untracked evidence files; the capture header says `DIRTY no`. No modified copy was run. The same script passed as CI's `devnet` job on `fb4cde2` ([CI](#ci)). Command preserved: `bash scripts/devnet/prove-containers.sh`. |

## Requirements

The 14 rows of the `requirements-matrix` block whose goal column is `06`.

Where a row cites an `r3-devnet-*` log, [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt) on `90652c6` reruns the same test, and the lines quoted below appear there unchanged: `phase=add h-1=42 count=4 h=43 count=4 h+1=44 count=4 h+2=45 count=5`, `phase=rotate h+1=89 count=5 oldkey=true h+2=90 count=5 newkey=true`, `phase=remove h+1=131 count=5 still=true h+2=132 count=4` and `phase=power-set effective-1=104 powers=v1:10,v2:10,v3:10,v4:10 effective=105 powers=v1:10,v2:10,v3:2,v4:10,v5:1`. Case Y observes the stall there with `observed=stall height_from=121 height_to=121 set_height=122`. `TestDevnetKeysAreDistinct` passes in [r3b-devnet-unit.txt](logs/goal-06/r3b-devnet-unit.txt) and [r3b-devnet-package.txt](logs/goal-06/r3b-devnet-package.txt). The `r3-` citations are the earlier run on `fb4cde2`.

| Row | Status | Evidence on tree `7d07a6d6`, and on tree `ad5cdcb9` for `r3b-` logs |
| --- | --- | --- |
| REQ-PROD-005 Small permissioned set; no open join | VERIFIED | `TestValidatorUpdatesHaveOneConstructor`, `TestMessagesAreRejected`, `TestChainSourcesDoNotImportEconomicModules`, `TestLinkedBinaryOmitsEconomicModules`, app row `outsider propose` (sdk 9) ([r3-unit.txt](logs/goal-06/r3-unit.txt)) |
| REQ-ARCH-005 Validator messages cannot set a media owner | VERIFIED | `TestAuthorityMessagesCarryNoMediaFields` |
| REQ-ARCH-006 Council controls the set; membership unset (BLOCKER DEC-008) | VERIFIED for the approved DEC-008 default; no council member is named | DEC-008 is APPROVED, and its default names no person ([decision-register.md](../decision-register.md#dec-008)). This row carries out that answer; it is not an open question ([requirements.md](../requirements.md)). Naming real council keys stays blocked by DEC-008 and is not a goal 06 task. The chain's genesis error text still says "while DEC-008 is open" (`chain/x/authority/genesis.go`), and the frozen proto comment says "while DEC-008 is OPEN" (`docs/protocol/proto/svn/authority/v1/state.proto`); that wording refers to what the approved default leaves unset or blocked, the council membership and the quorum-loss behaviour. Tests:<br>- `approved_council` stays empty, and a genesis that sets it is refused (`TestValidateGenesisState`, `TestInitChainRejectsUnprojectableGenesis/approved_council`, `TestValidateGenesisAppliesAuthorityRules`);<br>- council messages off a `shortvideo-dev-` devnet are DECISION_BLOCKED (`TestCouncilGateIsDecisionBlocked`, `TestCouncilIsDecisionBlockedOffDevnet`);<br>- the set changes only through executed council proposals (`TestCommittedAuthorityFlowAndReplay`, [r3-devnet-lifecycle.txt](logs/goal-06/r3-devnet-lifecycle.txt)). |
| REQ-TECH-013 No Enterprise PoA or Groups; custom module with tests, no audit claim | VERIFIED | `TestChainSourcesDoNotImportEconomicModules` lists `enterprise/poa` and `enterprise/group` as forbidden imports. This file makes no audit claim. |
| REQ-CHAIN-006 x/authority is the only writer; no staking or `MsgCreateValidator` | VERIFIED | `TestValidatorUpdatesHaveOneConstructor`, `TestLinkedBinaryOmitsEconomicModules`, `TestEveryProtocolMessageDecodes` |
| REQ-CHAIN-007 Activation height of a validator update | VERIFIED, single machine, multi-process | `phase=add h-1=42 count=4 h=43 count=4 h+1=44 count=4 h+2=45 count=5` ([r3-devnet-lifecycle.txt](logs/goal-06/r3-devnet-lifecycle.txt)). `phase=power-set effective-1=104 ... effective=105 ...` ([r3-devnet-power-vs-count.txt](logs/goal-06/r3-devnet-power-vs-count.txt)). `TestCommittedAuthorityFlowAndReplay` returns the update on the activation block, with `CometEffectiveDelay` 2. |
| REQ-CHAIN-008 Only admitted accounts write | VERIFIED | `TestAnteRejectsNonAdmittedAccount`, app row `outsider propose`, `TestMessagesAreRejected` |
| REQ-CHAIN-009 Operator list and independence test unset (BLOCKER DEC-002) | VERIFIED for the approved DEC-002 default; no operator list exists | DEC-002 is APPROVED ([decision-register.md](../decision-register.md#dec-002)): admission stays permissioned, a reviewed operator registry is the mechanism (Goal 07), and the development validators are development processes. This row carries out that answer; it is not an open question ([requirements.md](../requirements.md)). Tests:<br>- the ante admits only accounts with an admission row, and genesis writes those rows from submitter rows (`TestAnteRejectsNonAdmittedAccount`, app row `outsider propose` sdk 9);<br>- provider messages are FORBIDDEN at the router (`TestProviderMessagesAreForbiddenAndDecode`). No Goal 06 message creates a provider row, so on chain an admit payload executes to NOT_FOUND ([Deviations](#deviations));<br>- at engine level, with a seeded provider row and no DEC-002 `DEVNET_TEST_ONLY` parameter, an admit payload is DECISION_BLOCKED and leaves the store unchanged (`TestAdmitAndDisableRows`). The devnet genesis carries that parameter, so the devnet does not reach this branch.<br>The devnet processes are not independent operators, and this file claims none. |
| REQ-CHAIN-010 Signatures, sequences, authorization, size, gas, quotas; zero fee still metered | VERIFIED | App rows: `corrupt signature` sdk 4, `stale sequence` sdk 32, `wrong-role propose (authority)` UNAUTHORIZED, `tx bytes above 65536` BOUNDS, `gas limit 0` BOUNDS, `out of gas` sdk 11, `seat at its open limit proposes` BOUNDS. Also `TestZeroFeeTransactionIsMetered` and `TestHandlerChecksTxSizeAndGas`. |
| REQ-MODU-001 Membership, council records, scheduled changes, submitters | VERIFIED | `TestCommittedAuthorityFlowAndReplay`, `TestInitStoreLayout`, `TestGenesisPendingIsAppliedAtItsHeight`, `TestMessageKeySets` |
| REQ-VAL-001 Rotation, removal, duplicate keys, nonpositive power, changed quorum at the activation height | VERIFIED, devnet single machine, multi-process | Rotate and remove lines in [r3-devnet-lifecycle.txt](logs/goal-06/r3-devnet-lifecycle.txt). `TestDuplicateConsensusKey`. `TestValidatorRules` subtests `zero_power` and `negative_power`. `TestPowerOnlyChange`, `TestPowerOnlyChangeCommitted`. The changed quorum at effective 105 is in [r3-devnet-power-vs-count.txt](logs/goal-06/r3-devnet-power-vs-count.txt). |
| REQ-SEC-002 Service, validator and governance keys distinct | VERIFIED; DR-06-06 open for Goal 08 | `TestDevnetKeysAreDistinct` ([r3-devnet-goal05.txt](logs/goal-06/r3-devnet-goal05.txt)). `TestRemovalBindHaltsAndRotationsStaySeparate`. App rows `wrong-role propose (authority)` and `(service)` get UNAUTHORIZED; `seat-signed execute` and `service-signed execute` get FORBIDDEN. Seat accounts are `BROADCASTER` submitters, and no Goal 06 handler accepts that role (DR-06-06). |
| REQ-SEC-004 Floods and oversized messages rejected; bounded state growth | VERIFIED | Row `tx bytes above 65536` BOUNDS. `TestSeatQuotaIsPerSeat`, `TestPendingCap`, `TestPendingOverCapIsIntegrity`, and the gas lines under B5. Replay records are kept for the life of the chain, so storage grows by one bounded record per accepted message. Gas does not grow with it. |
| REQ-GOV-004 Pause rights, key replacement, quorum loss, upgrade choice (BLOCKER DEC-008) | VERIFIED for key replacement and quorum loss; IMPLEMENTED (doc) for who pauses admissions; upgrade choice DEFERRED to Goal 10 | DEC-008 and DEC-009 are APPROVED ([decision-register.md](../decision-register.md)).<br>- Key replacement: [validator-lifecycle.md](../operations/validator-lifecycle.md), "Compromised or lost consensus key", replaces a key with one `ValidatorPowerChange` that keeps the node id and names a new key. The devnet rotate does exactly that (`phase=rotate h+1=89 count=5 oldkey=true h+2=90 count=5 newkey=true`, [r3-devnet-lifecycle.txt](logs/goal-06/r3-devnet-lifecycle.txt)).<br>- Quorum loss: the approved DEC-008 default leaves quorum-loss behaviour unspecified. `QuorumLossBehavior` stays `UNSET`, and recovery needs a new genesis or a social recovery, out of scope ("Loss of quorum" in the same doc). A genesis whose `quorum_loss` is not `QUORUM_LOSS_BEHAVIOR_UNSET` is refused with FORBIDDEN (`TestValidateGenesisAppliesAuthorityRules` row `quorum loss set`, `TestValidateGenesisState` row `quorum loss not UNSET`). The tested value is `UNSPECIFIED` (0); no test sets a reserved value 2-4. Case Y in [r3-devnet-power-vs-count.txt](logs/goal-06/r3-devnet-power-vs-count.txt) observes the stall.<br>- Who pauses admissions: validator-lifecycle.md, section "Who pauses admissions", added in this evidence step. Only the council, by not approving admissions; there is no pause switch and no single-signature or emergency path (DEC-009); a local operator can stop only its own endpoint. The section describes tree `7d07a6d6`: ante admission rows are written only by genesis (`store.go`) and by the engine-only `RotateCouncilSeat` and `RotateServiceSubmitter`, which have no non-test caller, and the only authority messages are propose, approve and execute. No test checks the document. The council-only admit and disable path is exercised by `TestAdmitAndDisableRows`, and the app row `execute of an OPEN proposal` (CONFLICT) shows that a proposal without enough approvals does not execute.<br>- Upgrade choice is Goal 10. |

## Export

`App.Export` refuses while the authority store holds state that the frozen genesis proto has no field for. That state is the policy version, proposal records, business ids, the executed set, governed limits, open counts, retired keys and provider rows. The error starts with `export refused:` and names the first such prefix. `App.Export` also refuses a document that genesis would reject, such as unequal validator powers.

The module `ExportGenesis` has no error result in SDK v0.55, so it panics with the same error if it is called directly. `svnd` has no export command.

| Behaviour | Test ([r3-unit.txt](logs/goal-06/r3-unit.txt)) |
| --- | --- |
| A fresh state (config, admission rows, pending entries) exports, imports through InitChain, and exports again byte for byte | `TestFreshStateExportRoundTrip`, subtests `no_pending_entry` and `genesis_pending_entry` |
| Export refuses after a committed council message, and refuses unequal powers | `TestExportRefusesSideState`, subtest `unequal_powers` |
| Each side-state prefix is refused | `TestExportStateRefusesEachPrefix` |
| The module export panics on side state | `TestExportGenesisPanicsOnSideState` |
| Admission rows must match submitters; more than 128 pending entries is an error | `TestExportStateChecksAdmissionRows`, `TestPendingOverCapIsIntegrity` |

Restart from export with council history is not supported. DR-06-01 asks whether protocol v0 should gain genesis fields for that state. DR-06-02 asks whether an imported genesis may carry unequal powers that meet DEC-011. Goal 05's import/export row is scoped to fresh state and links this rule in [authority-authorization.md](../architecture/authority-authorization.md#export).

## Deviations

Recorded differences from the frozen spec machine (`docs/evidence/goal-02/spec_machine.py`):

- **Provider messages are FORBIDDEN (D1).** The spec's `op_register_provider` accepts a registration, and the spec has a rotation path. On this chain, `RegisterProvider`, `UpdateProvider`, `RotateProviderKey` and `DrainProvider` return `ERROR_CODE_FORBIDDEN` at the router until Goal 07 owns the provider registry. A signed rotation decodes first. Council admit and disable payloads work over seeded provider rows. No Goal 06 message creates a provider row, so on chain an admit executes to `ERROR_CODE_NOT_FOUND`.
- **Keep outcomes on the wire (DR-06-05).** An expiry mark, or a FORBIDDEN execute that marks the proposal REJECTED, commits with ABCI code 0. The protocol code goes in the `code` attribute of an `authority_result` event. `docs/protocol.md` says ABCI 0 is success, and its event list has no `authority_result`.
- **Differences that remain after the NB14 check-order alignment:**
  - The digest algorithm check (a proto field the spec does not have) stays after the INTEGRITY check.
  - proto3 cannot tell an absent integer from 0. `base_policy_version` 0 is STALE and `expiry_unix_seconds` 0 is BOUNDS. The spec treats an absent field as MALFORMED.
  - The per-seat open-proposal quota is the last propose check (DR-06-03). The spec has no quota.
  - The pending cap returns BOUNDS after projection (DR-06-04). The spec has no cap.
  - Approve returns OVERFLOW when the approval that reaches the threshold would push the execution time past `unix_seconds_max`. The spec has no such check.
  - A seat-signed execute is FORBIDDEN, because the seat address is a `BROADCASTER` submitter (DR-06-06). Spec `check_role` returns UNAUTHORIZED for a signer that is not a submitter and FORBIDDEN for a wrong role.
- **Caps.** The spec has neither the 32-per-seat open-proposal cap nor the 128 pending cap (DR-06-03, DR-06-04).
- **Events.** The spec's `op_execute` emits `PolicyChanged`. The chain emits only `authority_result` (X5, follow-up).

## Decision requests

Full text: [authority-authorization.md](../architecture/authority-authorization.md#decision-requests). DR-06-07 is defined here, because it is about model allocation, not the chain.

| Id | Question | Status |
| --- | --- | --- |
| DR-06-01 | Restart from export: add genesis fields (protocol change), or keep restart from export unsupported while side state exists | OPEN. Goal 06 implements the refusal. |
| DR-06-02 | Genesis equal powers versus DEC-011 runtime powers | OPEN. Goal 06 keeps equal powers, and export refuses unequal ones. |
| DR-06-03 | Open-proposal quota, 32 per seat on the current base version | OPEN. PROPOSED number, implemented and tested. |
| DR-06-04 | Pending validator-change cap, 128 | OPEN. PROPOSED number, implemented and tested. 128 far-future entries block later changes. |
| DR-06-05 | Keep outcomes as ABCI 0 plus `authority_result` | OPEN. Recorded protocol deviation. |
| DR-06-06 | Seat-only admission path before Goal 08 adds `BROADCASTER` handlers | OPEN. Documented. Latent in Goal 06. |
| DR-06-07 | Model allocation for the fix round: Claude Code `claude-opus-5-5`, effort `ultracode`, in place of GPT-6 Astra HIGH | RESOLVED, owner acknowledged on 2026-11-01. Quotes in [Model and effort](#model-and-effort). |

## CI

[r3-ci.txt](logs/goal-06/r3-ci.txt) was read with `gh api` (GitHub REST), captured on tree `7d07a6d6`. Its two runs, the first two rows, are `pull_request` runs for PR 9. The `90652c6` row has no capture log.

| Head | Run | Conclusion | Jobs |
| --- | --- | --- | --- |
| `2389948` | [41339495119](https://github.com/Maxencejules/shortvideo-network/actions/runs/41339495119) | success | node, contract, python, go, devnet: all success |
| `fb4cde2` | [41346972144](https://github.com/Maxencejules/shortvideo-network/actions/runs/41346972144) | success | python, contract, node, go, devnet: all success |
| `90652c6` | [41369142291](https://github.com/Maxencejules/shortvideo-network/actions/runs/41369142291) | success | contract, node, python, devnet, go: all success (read from PR 9's check runs through the GitHub API at about 00:58 UTC on 2026-11-02; not a capture log) |

CI's `go` job runs `run_tests.py --only go`, `go vet`, and `go test -race` on `./chain/...`. CI's `devnet` job runs the unchanged `scripts/devnet/prove-containers.sh`, whose Python stall watch still uses the first-sample ceiling (see [Follow-ups](#follow-ups)).

Run 41362122347 (job `go`) failed with `prove_test.go:136: phase two-down node 0 moved from 11 to 12`, threshold 12. [r3-ci.txt](logs/goal-06/r3-ci.txt) was read before that run, and this step read no CI run (no `gh`), so this file has no captured record of it; the text comes from the orchestrator's brief. Commit `90652c6` fixes the cause ([Round 3 CI fix](#round-3-ci-fix-90652c6)). The `90652c6` row above is for the orchestrator to fill in from PR 9's checks.

CI on the evidence commit is not recorded here, because that run cannot exist before this file is committed. It is shown in PR 9's checks, and the merge waits for it to be green. Its `contract` job runs both checkers on the committed docs with `mutants.py` and its `PY_ALLOW` entry in place. Locally, both checkers pass with the runner in place ([r3-checkers-allowlisted.txt](logs/goal-06/r3-checkers-allowlisted.txt)).

## Superseded logs

These logs are not evidence for this revision. They are kept for history.

| Log | Revision |
| --- | --- |
| `devnet-lifecycle.txt`, `go-test-race.txt`, `python-checks.txt` | Round 1, commit `f38c6ef`, tree `4279ae1a` |
| `devnet-r2.txt`, `fuzz-state-r2.txt`, `go-test-race-r2.txt`, `python-r2.txt`, `unit-r2.txt` | Round 2, commit `4c87282`, tree `eacc6b47` |

The equal-power `devnet-r2.txt` is not cited for quorum by power.

For `chain/devnet` only, the `r3-devnet-*` logs and the devnet results inside `r3-unit.txt`, `r3-race.txt` and `r3-run-tests-go.txt` are the earlier run on `fb4cde2`. The `r3b-` logs on `90652c6` supersede them for that package. They stay cited as the earlier run. The results for every other package in `r3-unit.txt`, `r3-race.txt` and `r3-run-tests-go.txt` stand (see [Round 3 tested revision](#round-3-tested-revision)).

## Commands

The `$NS` prefix is the scratch `netns-run.sh` path printed in full in each log.

```text
COMMAND env SVN_SKIP_DEVNET=1 go test -count=1 -v -timeout 20m ./chain/...
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
EXIT:0
```

```text
COMMAND $NS go test -race -count=1 -timeout 40m ./chain/...
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
EXIT:0
```

```text
COMMAND $NS go test -count=1 -v -timeout 20m -run \^TestDevnetValidatorLifecycle\$ ./chain/devnet/
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
EXIT:0
```

```text
COMMAND $NS go test -count=1 -v -timeout 20m -run \^TestDevnetQuorumFollowsVotingPower\$ ./chain/devnet/
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
EXIT:0
```

```text
COMMAND go test -run \^\$ -fuzz \^FuzzStateTransitions\$ -fuzztime 60s ./chain/x/authority/
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
EXIT:0
```

```text
COMMAND env SVN_SKIP_DEVNET=1 go test -count=1 -v -run \^TestFuzzCorpusReach\$ ./chain/x/authority/
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
EXIT:0
```

```text
COMMAND .venv/bin/python docs/evidence/goal-06/mutants.py
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
EXIT:0
```

```text
COMMAND bash scripts/devnet/prove-containers.sh
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
EXIT:1
```

```text
COMMAND bash -c 'set -u; H=<scratch hold dir>; mv docs/evidence/goal-06/mutants.py "$H"/mutants.py || exit 3; trap ... EXIT; ...; .venv/bin/python docs/evidence/goal-01/check_requirements.py && PATH=$PWD/.toolchains/bin:$PATH .venv/bin/python docs/evidence/goal-02/check_protocol.py'
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
DIRTY_PATH docs/operations/validator-lifecycle.md
DIRTY_PATH docs/progress.md
DIRTY_PATH docs/progress.tsv
EXIT:0
```

```text
COMMAND bash -c '.venv/bin/python docs/evidence/goal-01/check_requirements.py && PATH=$PWD/.toolchains/bin:$PATH .venv/bin/python docs/evidence/goal-02/check_protocol.py'
TREE 7d07a6d632acaa54da9f71d4f16a63d73052df38
DIRTY no
EXIT:0
```

The `r3b-` commands on `90652c6`:

```text
COMMAND bash -c 'git diff --stat fb4cde2087269fb9a93900b9a4d11fd24779ced2 HEAD -- . ":!docs"; git diff --stat fb4cde2087269fb9a93900b9a4d11fd24779ced2 HEAD -- docs | tail -1'
TREE ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467
DIRTY no
EXIT:0
```

```text
COMMAND bash -c 'test -z "$(gofmt -l chain/devnet)" && go vet ./chain/devnet/'
TREE ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467
DIRTY no
EXIT:0
```

```text
COMMAND env SVN_SKIP_DEVNET=1 go test -count=1 -v ./chain/devnet/
TREE ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467
DIRTY no
EXIT:0
```

```text
COMMAND $NS bash -c 'for i in 1 2 3 4 5; do go test -count=1 -v -run "^TestFourValidatorsCommitRestartAndQuorum$" ./chain/devnet/; echo "RUN $i EXIT $?"; done'
TREE ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467
DIRTY no
EXIT:0
```

```text
COMMAND $NS go test -count=1 -v -timeout 30m ./chain/devnet/
TREE ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467
DIRTY no
EXIT:0
```

```text
COMMAND $NS go test -race -count=1 -v -timeout 30m ./chain/devnet/
TREE ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467
DIRTY no
EXIT:0
```

The `r3b-` COMMAND lines above are unquoted for reading; each log's first line has the shell-quoted form. The five-run loop's own exit is that of its last `echo`, so each run's exit is its `RUN <n> EXIT <code>` line.

The verifier block (the one with `DIRTY_PATH` lines) is abridged; [r3-checkers-verifier.txt](logs/goal-06/r3-checkers-verifier.txt) line 1 has the full command. The last block is [r3-checkers-allowlisted.txt](logs/goal-06/r3-checkers-allowlisted.txt), with the runner in place.

The other command lines are in the first line of each log.

## Blockers

- **Goal status.** None. DEC-002, DEC-008 and DEC-009 are APPROVED. The three requirement rows that name DEC-002 and DEC-008 carry out the approved answers (see the status at the top and [Requirements](#requirements)).
- **Mutation runner location: settled for the evidence commit.** The runner stays at `docs/evidence/goal-06/mutants.py` and goes into the evidence commit. `PY_ALLOW` in `docs/evidence/goal-01/check_requirements.py` gains the exact path `docs/evidence/goal-06/mutants.py`, with a one-line comment; the match is `str.startswith`, so the entry admits this file and any path that begins with that exact string; any other name in `docs/evidence/goal-06/` is still rejected. Nothing else in the checker changed. Both checkers then pass with the runner in place ([r3-checkers-allowlisted.txt](logs/goal-06/r3-checkers-allowlisted.txt)). This is option (a) of the earlier list, narrowed to one file. The orchestrator chose it for this step; it is a contract-checker change under `docs/`, and it is not an owner approval. The PR review covers it. The earlier options were (b) keep the runner out of Git, so `r3-mutants.txt` would cite a script outside the repository, and (c) move it under `scripts/evidence/`, a change outside `docs/`.
- **Container proof here (environment).** The build container does not trust the session proxy CA. CI ran the script.

## Follow-ups

- Emit `PolicyChanged` from an executed proposal, or record it as a protocol change (X5).
- An EXECUTED or REJECTED proposal that is executed after its expiry is relabelled EXPIRED (`keepAs` in `chain/x/authority/engine.go`; `TestOpenCountBookkeeping` cases `EXECUTED -> EXPIRED relabel` and `REJECTED -> EXPIRED relabel`). It is inherited from spec `op_execute`, which checks expiry before `QUEUED`. Raise it against the spec, not the chain (round-1 item 7).
- REQ-GOV-004: how operators choose upgrades is Goal 10.
- Port the stall-check fix to `scripts/devnet/prove-containers.sh`. Its Python `watch` in `stall` mode still takes each node's first sample as the ceiling (`phase {phase} node {index} moved from ...`), the cause fixed for `chain/devnet` in `90652c6` ([Round 3 CI fix](#round-3-ci-fix-90652c6)). CI's `devnet` job runs that script, so it can fail the same way. Not done.
- Stall-check cleanups from the second review of `90652c6`. None can make a chain that is not stalled pass. The `prove_test.go` `precommitTime` comment overstates "the vote time is the voter's clock": it is the later of the clock and the median time plus 1 ms. The re-executed child in `stall_test.go` has no context and no `-test.timeout`, so a hang lasts until the package timeout. The fake RPC gives absent signatures the validator's address, where CometBFT gives `""`. `stoppedSigned` reads `/commit` twice, and `readCommit` and `validatorSet` use `http.Get` with no timeout, so a hung RPC hangs the test rather than failing it.
- Add a seat-only admission path before Goal 08 merges (DR-06-06).
- Decide restart from export and genesis powers together (DR-06-01, DR-06-02). A restart path needs protocol fields, possibly with Goal 10.
- Any future message path for `RotateCouncilSeat` must create the auth account for the new seat address.
- Force-withdraw stays `ERROR_CODE_NOT_FOUND` until Goal 07 has content records.
- Auth version migrations 5 to 7 stay with Goal 10.
- Goal 07 merge: goal 07 still has the old `fuzz_test.go`, and it reads `openKey`, `release` and `PolicyVersion` with the round-2 signatures. The x/authority merge must adapt them. `docs/evidence/goal-07.md` needs its model-acknowledgement row updated for the 07-30 extension.

## Residual risks

- **`-race` shutdown flake.** A race-built validator sometimes exits with status 2 on SIGTERM in devnet tests. The second fix-round gate hit it once, in `TestFourValidatorsCommitRestartAndQuorum` at `prove_test.go:82`, on the uncommitted tree; its rerun passed. That was a scratch log, not a log on this tree. The flake did not occur in [r3-race.txt](logs/goal-06/r3-race.txt) or in [r3b-race-devnet.txt](logs/goal-06/r3b-race-devnet.txt). It is unexplained, and CI's `go` job can hit it.
- **Stall check: slow shutdown (false failure).** A stop is SIGTERM, then the gRPC graceful stop and the CometBFT node stop; the validator can vote until its consensus reactor stops. If that took longer than about one block time, threshold+2 could commit with the stopped validator's signature, and `stallLimit` would reject it as a second block. This would be a false failure, not a false pass. No `r3b-` log has a sample above threshold+1, and the fix workflow reported none in its scratch runs. The precommit timestamps and `stoppedAt` give the data to diagnose it if it appears.
- **Stall check: host clock.** The timestamp rule compares the stopped validator's precommit time with `stoppedAt`, the wall time when the stop returned. It assumes one host clock that does not step during the run. A backward clock step (for example an NTP correction) between the vote and the stop could fail a correct in-flight block (a false failure), and one after the stop could let a later vote pass the rule.
- **Stall check: live in-flight path seen rarely.** On unmutated code with the real threshold, a block in flight at the stop occurred live four times, and the in-flight rule accepted it each time: once in the fix workflow's scratch `e-race-run1` (two-down), and three times in `r3b-` logs: two-down in run 5 of [r3b-devnet-goal05-x5.txt](logs/goal-06/r3b-devnet-goal05-x5.txt), power-one-down and case Y in [r3b-race-devnet.txt](logs/goal-06/r3b-race-devnet.txt). The first reviewer's scratch M5 runs (threshold lowered by one) also took the rule on live commits. No run of the test suite has produced a live block that the check rejects; in the suite, the reject paths run only against the fake RPC in `stall_test.go`. A second reviewer's scratch probe, compiled into the `90652c6` package with `go test -overlay` and run through `capture.sh` on `90652c6` (not committed), did reach them on a live chain in each of its three `06-live-probe-run*` runs: a three-of-four chain that kept committing was rejected (`probe=live-not-stalled`), a threshold two below the stall was rejected (`probe=shifted-two`), and v3's vote after its restart was rejected by the timestamp rule (`probe=post-stop-vote`).
- **Stall check: container proof not fixed.** `scripts/devnet/prove-containers.sh` still uses a first-sample ceiling in its Python stall watch, and CI's `devnet` job runs it (see [Follow-ups](#follow-ups)).
- **Case Y depends on an unstable endpoint.** It reads votes from CometBFT's `/dump_consensus_state`, which CometBFT marks unstable. A CometBFT upgrade can change its shape; the test would then fail through its parse or length checks. In [r3-devnet-power-vs-count.txt](logs/goal-06/r3-devnet-power-vs-count.txt), `height_to` equals `height_from` (122), so the in-flight-block branch of the stall check did not execute in that run. It did execute in [r3b-race-devnet.txt](logs/goal-06/r3b-race-devnet.txt): `phase=power-case-Y-in-flight sample=0 node=0 height=124 threshold=123 signers=v1,v2,v3,v4`.
- **Single machine.** Every devnet result is four validator processes on one machine in a loopback-only namespace. No independent operator or external peer was tested.
- **Container proof** is BLOCKED here (environment). Only CI ran it on this revision.
- **Dead current-version proposals hold slots (DR-06-03).** A seat's current-version proposal that is never approved, or whose execute fails with any code other than FORBIDDEN, keeps its slot. It is freed when any execute bumps the version, or when the proposal is marked expired. Its expiry may be as late as `unix_seconds_max`. A seat can fill only its own 32 slots this way.
- **Pending-cap freeze (DR-06-04).** No message cancels a scheduled change. A council majority that schedules 128 far-future changes blocks every later validator change, including a compromised-key replacement, until they apply.
- **`RotateCouncilSeat` creates no auth account** for the new seat address. It is engine-only, with no caller.
- **No `PolicyChanged` event (X5).** An indexer that follows the spec's event list sees no policy-change event.
- **Old devnet homes.** A home created before the fix round must be re-initialized. A home that never ran an authority message keeps running, but genesis pending entries left in the old config never apply.
- **Mutation coverage.** `r3-mutants.txt` lists only mutants that are killed. Defensive integrity branches that no message can reach are not in it. Of the mutants the round-2 review named, the two B1 deletions are covered only by code-swap mutants on the same branches, and two B2 mutants target code round 3 removed (see the B1 and B2 rows).

## Next safe action

1. Commit this evidence on top of `90652c6`: `docs/evidence/goal-06.md`, `docs/progress.md`, `docs/progress.tsv` and the six `r3b-` logs under `docs/evidence/logs/goal-06/`, with the trailers `Tested-commit: 90652c6b6651a4fbdda1344bb91d3c803270c2c6` and `Tested-tree: ad5cdcb9efb2f785dbd5eb7681a10bb71a45c467`. The earlier evidence (the runner, the `PY_ALLOW` entry, the lifecycle-doc section and the `r3-` logs) is already in `c68113c`.
2. Fill in the `90652c6` row of [CI](#ci) from PR 9's checks, then wait for CI on the evidence commit. Its `contract` job runs both checkers on the committed docs, with the runner and its `PY_ALLOW` entry in place. That run is shown in PR 9's checks, not in this file.
3. Merging is the orchestrator's step under the owner's instruction for this round ("Fix on PR 9's branch, then merge"), after the evidence commit's CI is green. Then rebase or merge goal 07 onto the result.
4. Separately, port the stall-check fix to `scripts/devnet/prove-containers.sh` ([Follow-ups](#follow-ups)).

## Changed files

`git diff --stat 3853e34 HEAD` ([r3-revision.txt](logs/goal-06/r3-revision.txt)): 53 files changed, 8756 insertions, 1790 deletions.

- x/authority: per-object store, engine, genesis validation, export refusal, tests.
- app: router, encoding, InitChain, Export, and tests.
- genesis: power-total bound, drift test, parse.
- cmd/svnd: authority rules in `genesis validate`.
- devnet: `Options.Power`, the lifecycle test, and the power-vs-count test.
- `chain/go.mod`: tidy.
- Docs: the authorization map, the lifecycle doc, the x/authority README, `chain/README.md`, and one row of `goal-05.md`.
- `scripts/evidence/capture.sh`.

`git diff --stat f3f583e..HEAD`, the whole PR against `main` as read at `fb4cde2`: 76 files changed, 14567 insertions, 201 deletions. No `.proto`, `docs/protocol/`, `docs/protocol.md` or `docs/goals/` file is in either diff.

Commit `90652c6`, the stall-check fix, after the round 3 evidence commit `c68113c`: `git diff --stat fb4cde2 90652c6` outside `docs/` is 4 files changed, 959 insertions, 42 deletions ([r3b-diff.txt](logs/goal-06/r3b-diff.txt)):

- `chain/devnet/prove_test.go`: `stopForStall`, `stallLimit`, `watchStall`, `observeStall` as a wrapper, `stoppedSigned`, `precommitTime`, `consensusKeys`; the goal 05 two-down phase uses them.
- `chain/devnet/lifecycle_test.go`: the power-one-down phase starts through `stopForStall` (`phase=power-stop-one`).
- `chain/devnet/power_quorum_test.go`: each power case starts through `stopForStall` (`phase=power-case-<X|Y>-stop`); case Y's stall uses `stoppedSigned`; `commitSig` gains `Timestamp`.
- `chain/devnet/stall_test.go` (new): the stall-check unit tests against a fake CometBFT RPC.

The round 3 evidence commit `c68113c` added:

- this file;
- `docs/progress.md` row 06 and `docs/progress.tsv` row 06;
- the "Who pauses admissions" section of `docs/operations/validator-lifecycle.md` (REQ-GOV-004), added by the final verifier;
- `docs/evidence/goal-06/mutants.py`, the mutation runner (119 mutants);
- `docs/evidence/goal-01/check_requirements.py`: one `PY_ALLOW` entry, `docs/evidence/goal-06/mutants.py`, and its one-line comment;
- 35 `r3-` logs: the evidence runner's 28 (its `r3-mutants.txt` is now `r3-mutants-attempt2.txt`), plus `r3-revision.txt`, `r3-symbols.txt`, `r3-ci.txt`, `r3-checkers-final.txt` and `r3-checkers-verifier.txt` from this step, plus the 119-mutant `r3-mutants.txt` and `r3-checkers-allowlisted.txt`.

This evidence step, for `90652c6`, changes:

- this file: the second tested commit, the [Round 3 CI fix](#round-3-ci-fix-90652c6) row, `r3b-` citations, CI, follow-ups, residual risks, next action and PR summary;
- `docs/progress.md` row 06 and `docs/progress.tsv` row 06: one clause on the stall-check fix;
- six `r3b-` logs: `r3b-diff.txt`, `r3b-gofmt-vet.txt`, `r3b-devnet-unit.txt`, `r3b-devnet-goal05-x5.txt`, `r3b-devnet-package.txt`, `r3b-race-devnet.txt`.

## Proposed pull request

Title: `goal 06: x/authority round 3: per-object store, full-store rejects, export refusal, quorum by voting power, devnet stall-check fix`

Review summary:

- **Store and quotas.** The authority store keeps one key per object. Gas no longer grows with history: the same propose costs 119479 before and after 301 terminal proposals. Open proposals are capped at 32 per seat on the current version, and pending changes at 128. Both caps are PROPOSED (DR-06-03, DR-06-04).
- **Rejects.** A rejected message leaves every authority key byte-identical. 37 app rows assert exact codes.
- **Fuzz.** The transition fuzz reaches QUEUED, EXECUTED and ApplyDue (a corpus-reach test proves it). There are three 60-second fuzz runs. 119 of 119 mutants are killed, including three the round-2 review named (an extra `0x02` ante row, an execute that writes before CONFLICT, a dropped `max*3` clause). Two more it named target code round 3 removed.
- **Providers.** Provider messages are FORBIDDEN until Goal 07. Every protocol message decodes.
- **Export and genesis.** Export refuses state the genesis proto cannot carry. A fresh state round-trips byte for byte. Genesis pending entries are projected at InitChain and in `svnd genesis validate`.
- **Devnet.** On a single-machine, multi-process devnet, the set changes at effective-1 and effective for add, rotate and remove. With the same three of five validators online, block production follows voting power: 30 of 33 progresses, 22 of 33 stalls.
- **Devnet stall check (`90652c6`).** CI run 41362122347 failed goal 05's two-down phase with `node 0 moved from 11 to 12` at threshold 12: the old check took each node's first sample as its ceiling. The check now reads the high-water mark of every running process just before the stop, allows catch-up up to it, and allows one block above it only when that block's commit is signed by the just-stopped validator (vote time no later than the stop), by no validator without a process, and by more than 2/3 of the power. A second block is never allowed. Only four `chain/devnet` test files changed. On `90652c6`: unit tests against a fake RPC pass, five goal 05 runs pass, and the devnet package passes with and without `-race`. Across those runs the in-flight rule accepted a live block at each of the three stall sites: two-down in one of the five goal 05 runs, power-one-down and case Y in the `-race` package run. The container script's stall watch still has the old ceiling (follow-up).
- **Status.** VERIFIED. Every playbook acceptance check passed on `fb4cde2`, and CI on `fb4cde2` is green on all five jobs. The `chain/devnet` checks passed again on `90652c6`; its CI run is recorded in PR 9's checks. The DEC-008 and DEC-002 requirement rows carry out the approved defaults, which name no council member and no operator. This is a reading of the decision register, not a new owner ruling. The evidence commit adds a "Who pauses admissions" section to the lifecycle doc for REQ-GOV-004; its upgrade choice is DEFERRED to Goal 10. The evidence commit adds the mutation runner at `docs/evidence/goal-06/mutants.py` and a one-line `PY_ALLOW` entry for it in the goal 01 contract checker.
