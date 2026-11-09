# Goal 11 evidence

Status: BLOCKED

DR-11-01 is approved for the pinned boundary document only. The goal stays BLOCKED: production custody is still DEC-001 and DR-07-01, and every run is one machine. Head CI is not a blocker.

The checks cited in this file ran on commit `9a00e16b96f86cd83b6fa9b4d9f86b4d12be1526` of branch `goal-11`, tree
`79a7e969eaa55e1fe62785a193bafd0cbdf2b1dd`. That commit's message carries `Tested-tree: 79a7e969eaa55e1fe62785a193bafd0cbdf2b1dd`.
The evidence logs were captured after that commit. This commit changes only `docs/evidence/`.

Public launch: DISABLED. No tradable token. This is not an audit and not a production network. Devnet results are four
validator processes on one machine, in a private network namespace. No independent operator is claimed.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH (`docs/goals/11-transaction-broadcaster-and-outbox.md` line 6). GPT-6 Astra was not used.
- Waves 0-2 and the flake fixes, before this handover, ran in Claude Code session `session_014mwakeb27YrugDfwAx8vXL`, model `claude-opus-5-5`, effort `ultracode`. Sources named by the handover: `handover/goal-11/get_session-2026-11-08T1951Z.txt` and the transcript tally `handover/goal-11/model-tally-goal11.txt` (all `claude-opus-5-5`: main loop 173, plan 1312, Wave 0 1301, Wave 1 2142, Wave 2 1330 messages). Those files live on branch `goal-11-handover` and are not in this tree. The `handover/` folder was not committed here.
- The first evidence pass and this round (the authorship rewrite, the retest, and this file) ran as a Cursor cloud agent. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed.
- On 2026-10-31 the owner waived the model requirement as a blocker ("Don't mind the ai model requirement", recorded on goal 04). On 2026-11-01 the owner wrote "I acknowledge the model substitution for DR-06-07" and then "extend the acknowledgement to goals 07 through 30". That covers model allocation only. It does not certify correctness.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `9a00e16b96f86cd83b6fa9b4d9f86b4d12be1526` |
| Tree | `79a7e969eaa55e1fe62785a193bafd0cbdf2b1dd` |
| Parent | `8e3eefdd5990cacd616f1478f3a8ac9a2a2da6fc` (the rewritten Wave 2 commit) |
| Base | `6d74aca5cfc461a9756587bdbec2f78e7111e574` (`main` after PR 15). It is an ancestor of HEAD ([stack.txt](logs/goal-11/stack.txt)). |
| Branch | `goal-11` |
| Pull request | Draft [PR 16](https://github.com/Maxencejules/shortvideo-network/pull/16) |
| Runtime | Ubuntu 24.04.5 LTS, Linux 6.12.113+ x86_64, uid 1000, 4 CPUs, 16003 MiB. Go 1.26.10. PostgreSQL 18.6. Python 3.12.3, ruff 0.17.3, mypy 2.4.1. libprotoc 36.2. Node v22.23.4. `npm` and golangci-lint were not installed. PostgreSQL 16 was not installed. [runtime.txt](logs/goal-11/runtime.txt) |

Chain production diff since the base is only `chain/go.mod`, moving `github.com/lib/pq v1.12.3` from an indirect requirement to a direct one. `chain/go.sum` is unchanged ([chain-prod-diff.txt](logs/goal-11/chain-prod-diff.txt), `SCOPE OK`). `GOWORK=off` chain module listings match the base, 746 lines ([dependency-identity.txt](logs/goal-11/dependency-identity.txt)).

On 2026-11-08 at 21:58 America/Toronto the owner wrote, verbatim: "authorship: rewrite goal-11 branch commits as me, one-time force-push of goal-11 ok, then re-test and fix items 2-3". Every commit on `goal-11` that is not on `main` has author and committer Maxence Jules `<Maxencejules@users.noreply.github.com>`. `Tested-tree` is the only trailer.

## What the tested commit changes

`9a00e16` is the rewritten Wave 2 tree plus the reviewed flake-test fixes, the finished F1, F3 and F4 work, the progress and requirements rows, the architecture and broadcaster README wording, and the evidence capture script. Nothing after this commit changes a file outside `docs/evidence/`.

- Flake, worker tests. `TestEffectCheckBeforeRetry` and `TestDeadlinePassedRetriesAtTheSameSequence` settle only the live attempt before asserting `RETRY_WAIT`. A full-jitter delay of a few milliseconds had let the same pass start the retry. `TestBackoffIsFullJitter` kills mutant `w3fix-backoff-never-waits`.
- Flake, devnet phase MR. The test waits for `/tx` at the committed height before the `tx.acc_seq` search. `broadcast_tx_commit` returns when the event is published; the indexer writes later.
- F1. `chain.Status.AppFloor` is the status height minus one, at least 1. CometBFT v0.40.0 saves block H before the application commits it, so `/status` can be one block ahead. Signer start-up, API authorization, the worker checks before signing, the relay pre-check and the forward provider read use that floor as the minimum query height. The query still runs at the latest height. An answer below the floor is stale. The signer retries a stale start-up governance read, or an answer with no height, for five tries one poll interval apart (PROPOSED), then fails naming the floor, the status height and the bound. The relay pre-check compares offer expiry with the time of that same status header. Block times strictly increase, so that time is a lower bound of any later block that could include the relay. The forward window still counts from the status height. An account read used to allocate a sequence stays pinned to the signer's observed commit height, which is stricter than the status floor.
- F3. `indexing_margin_blocks` is at least 2 (`outbox.MinIndexingMargin`). At status height S, block S−1 may not be indexed yet. The PROPOSED default was already 2. The bound table in `docs/architecture/broadcaster.md` says 2 to 20.
- F4. `post` in `chain/devnet/broadcaster_api_test.go` does not retry 503. Phase RS waits until validator 0 is not catching up, then posts once. The captured lifecycle has zero `api-retry` lines. Concurrent creates still count their own 503s in `rawPost`; those lines are not `api-retry`.

## Prerequisites

BLOCKED, except where a row says otherwise:

1. **DR-11-01: no longer a blocker (2026-11-08).** On 2026-11-08 at 21:39 America/Toronto the owner wrote, verbatim: "I approve docs/architecture/broadcaster.md at sha256 9779fdb1e7fe5a7577565c64a8066b6bff7e7bace1174b085df81e7b17ae905b (DR-11-01)". That is the sha256 of the file at the tested commit (1563 lines). The same message approved opening a draft pull request: "yes, open a draft pr for goal-11 and send it to the reviewer". The approval is of that document only. It does not approve any PROPOSED number, and it is not a merge. The file's own text still reads "Status: PROPOSED" and "It is not approved." It is not edited, because an edit would change the pinned sha256. The number is kept so the references in this file stay valid.
2. **REQ-CHAIN-013 production custody.** The devnet fixture refuses a minted provider signature and a missing publication permission. Host custody of user keys is still blocked by DEC-001 and DR-07-01. The row is not production-verified.
3. **Head CI: no longer a blocker for the retest.** Draft PR 16. Run [42578184489](https://github.com/Maxencejules/shortvideo-network/actions/runs/42578184489) on `ad55070fbefa5def4322fdb269ad33c9f8ceb701` completed success, 13 of 13 jobs: `python`, `node`, `go`, `devnet`, `contract`, and `go-race` for `app`, `devnet`, `devnet-evidence`, `devnet-upgrade`, `rest`, `broadcaster`, `devnet-broadcaster` and `devnet-broadcaster-stall`. The earlier run 42552331705 was the pre-rewrite head `1ab4021` and is not this head. Run [42579921165](https://github.com/Maxencejules/shortvideo-network/actions/runs/42579921165) on `6d9e825` failed in `Install protoc`: `curl: (22) The requested URL returned error: 504`. The `go` tests did not start. The other 12 jobs passed. Run [42581509093](https://github.com/Maxencejules/shortvideo-network/actions/runs/42581509093) on `265c1b0` failed the same way in `go` and in `contract` (`Install protoc and Python dependencies`, HTTP 504). Run [42583093717](https://github.com/Maxencejules/shortvideo-network/actions/runs/42583093717) on `554e119` failed the same way in those two jobs. Those are download failures, not test failures. Run [42584861128](https://github.com/Maxencejules/shortvideo-network/actions/runs/42584861128) on `061496c` completed success, 13 of 13, including the three new `go-race` jobs. That is the head CI for the evidence text it ran on.
4. **Independent operators.** Every devnet run is four processes on one machine. DEC-002 names no operator list.
5. **Goal 06 and goal 07 mutation runners.** Anchor checks only: [goal06-anchors.txt](logs/goal-11/goal06-anchors.txt) exit 1, `RESULT search-string problems=13; no test was run`; [goal07-anchors.txt](logs/goal-11/goal07-anchors.txt) exit 1, `RESULT validation problems=3; no test was run`. Known, and still BLOCKED.
6. **golangci-lint.** Not installed here. [golangci-lint.txt](logs/goal-11/golangci-lint.txt) exit 1, `command not found`, `chain=127`. The known go1.26 load failure is not separately reproduced because the binary is absent. BLOCKED.
7. **Non-root pgdev path on the CI runner: no longer a blocker.** The `go` job in run 42578184489 passed. That job runs `python3 scripts/run_tests.py --only go` without sudo (`.github/workflows/ci.yml`). The local capture also ran those tests as uid 1000 ([runtime.txt](logs/goal-11/runtime.txt)).
8. **PostgreSQL 16.** The runtime command asked for `/usr/lib/postgresql/16/bin/postgres`. It is not installed. The pin in use is 18.6. A 16.x server was not tested.

## Acceptance

| Check | Status | Log |
| --- | --- | --- |
| Devnet lifecycle: timeouts, worker death, duplicates, concurrent same-signer creates, one business effect | VERIFIED | [devnet-broadcaster.txt](logs/goal-11/devnet-broadcaster.txt) exit 0, 157s, zero `api-retry` lines. Section 3.2 also ran it twice without `-race` and once with `-race`, each exit 0, in scratch logs that are not committed. |
| Quorum stop stays pending or failed, not a false COMMITTED | VERIFIED | [devnet-broadcaster-stall.txt](logs/goal-11/devnet-broadcaster-stall.txt) exit 0, 63s. `committed_shown=none`, `short_expiry=FAILED(EXPIRED_NOT_SENT)`. Section 3.2 also ran it once without `-race` and once with `-race`, each exit 0. |
| Unauthorized messages and provider impersonation refused | VERIFIED | Lifecycle phases in the same devnet log; policy tests in [unit-broadcaster.txt](logs/goal-11/unit-broadcaster.txt). |
| Restart PostgreSQL and the broadcaster, then reconcile | VERIFIED | Lifecycle phases PG and BR in [devnet-broadcaster.txt](logs/goal-11/devnet-broadcaster.txt). |
| Read floor and indexing margin | VERIFIED | The broadcaster unit log. Mutants `f1-app-floor-is-status-height`, `f1-signer-startup-does-not-wait`, `f3-indexing-margin-one-allowed` are in the 284 killed ([mutants-goal11.txt](logs/goal-11/mutants-goal11.txt)). |
| Signing boundary document approved | VERIFIED for the pinned file only | DR-11-01, owner quote of 2026-11-08 21:39 America/Toronto, sha256 `9779fdb1e7fe5a7577565c64a8066b6bff7e7bace1174b085df81e7b17ae905b`. PROPOSED numbers stay PROPOSED. |
| Head CI | VERIFIED | Run 42584861128 on `061496c`, 13 of 13, including `go-race (broadcaster)`, `go-race (devnet-broadcaster)` and `go-race (devnet-broadcaster-stall)`. Run 42578184489 on `ad55070` was also 13 of 13. Runs 42579921165, 42581509093 and 42583093717 failed while downloading protoc (HTTP 504). Tests did not start in those runs. |

## Requirements rows

| Row | Status | Where |
| --- | --- | --- |
| REQ-CHAIN-013 | BLOCKED for production custody (DEC-001, DR-07-01). The devnet fixture's refusal of a minted provider signature and of a missing publication permission is in the policy and devnet logs. | [unit-broadcaster.txt](logs/goal-11/unit-broadcaster.txt), [devnet-broadcaster.txt](logs/goal-11/devnet-broadcaster.txt) |
| REQ-ACK-001 | VERIFIED on the devnet and by `TestAckNeverCommits` | [devnet-broadcaster.txt](logs/goal-11/devnet-broadcaster.txt), broadcaster unit log |
| REQ-INT-001 | VERIFIED for the chain-writer guard. This goal has no web client. | [chain-writer-policy.txt](logs/goal-11/chain-writer-policy.txt) |
| REQ-IDEM-001 | VERIFIED | devnet duplicate and restart phases; `TestIdempotentReplayNeedsFirstCommit` |
| REQ-QA-008 | VERIFIED for one final transition and no outbox lease table | devnet timeout, restart and quorum logs; `TestNoLeaseOrProjectionTable` |

## Commands

Section 3.2 ran on this tested commit before the capture. Scratch logs are under `/tmp/goal11-s32/` and are not committed. Each command exited 0: `gofmt` and `go vet`; `go test -race -count=1 -p 2 ./services/broadcaster/...` (17 packages `ok`); the two worker tests `-race -count=30` (the committed repeat is below); the lifecycle three times, the third with `-race`, and the stall twice, the second with `-race`, each with zero `api-retry` lines; `mutants.py --rev 9a00e16 --check`; groups chain-client, api, config and outbox (`RESULT mutants=112 killed=112`); signing as root (`57 killed`); worker (`35 killed`); devnet (`4 killed`); `go mod tidy -diff` in `services/broadcaster` and `chain` (empty); pytest of the three CI guard files (123 passed); `scripts/scan_secrets.py` (`secret scan ok`).

Capture on the committed tree used `scripts/evidence/capture.sh`. Logs were written outside the worktree and copied here unchanged, so the worktree was clean while each command ran. Every committed log prints HEAD `9a00e16b96f86cd83b6fa9b4d9f86b4d12be1526`, tree `79a7e969eaa55e1fe62785a193bafd0cbdf2b1dd`, and `DIRTY no`. [revision.txt](logs/goal-11/revision.txt) prints an empty `git status` between the markers, which matches `DIRTY no`. The capture script counts every untracked or modified path except the log it is about to write. A probe file under `docs/evidence/` made a throwaway log print `DIRTY yes`; that probe was not committed. The step table is [summary.tsv](logs/goal-11/summary.tsv).

| Step | Exit | Note |
| --- | --- | --- |
| devnet-broadcaster | 0 | 157s. Zero `api-retry` lines. |
| devnet-broadcaster-stall | 0 | 63s. |
| worker-count30 | 0 | 60 `--- PASS`, 0 `--- FAIL`, 27.552s. Repeat of the section 3.2 command, captured on this commit. |
| race-broadcaster | 0 | 17 `ok` lines. |
| unit-broadcaster | 0 | uid 1000, 60s. |
| tests-python, tests-python-clean-clone | 0 | 405 passed each. |
| tests-node | 0 | 13 passed and 1 passed. |
| chain-writer-policy | 0 | 123 passed in 2.07s. |
| ci-shards | 0 | Eight shards, each exit 0. The first attempt exited 143 in 0 seconds: `pkill -f /svn-pg-` matched the runner because the inline script contained that string. [rerun-driver.txt](logs/goal-11/rerun-driver.txt). The replacement log is the script file. |
| mutants-goal11 | 0 | `RESULT mutants=284 killed=284`, 1464s, as root. |
| mutants-goal10 | 0 | `RESULT mutants=303 killed=303` |
| mutants-goal09 | 0 | `RESULT mutants=169 killed=169` |
| mutants-goal08 | 0 | `RESULT mutants=28 killed=28` |
| builds, ruff-format, mypy | 0 | Replacements. The first builds command had `W3TMP` unset. The first ruff and mypy commands could not write root-owned caches. |
| golangci-lint, goal06-anchors, goal07-anchors | 1 | BLOCKED, as above. Not passes. |

`mutants-goal11-check` exited 0.

## Disclosures

- An evidence capture was started on the old Wave 2 commit in the previous container. Seven steps failed because `TMPDIR` was a private directory: pgdev socket paths exceeded 107 bytes, and root-only uid tests could not traverse it. Those logs were archived as non-evidence and are not in this tree. This capture left `TMPDIR` unset.
- Two intermittent failures on that older tree: the worker retry tests (test synchronization, fixed by `settleLive`) and devnet phase MR (index lag after `broadcast_tx_commit`, fixed by waiting for `/tx`). Both are test defects. The product defect found beside them is F1, fixed in the tested commit.
- `go mod tidy` on the earlier goal 11 commits dropped unused indirect lines in `services/broadcaster` and moved `lib/pq` to a direct requirement in `chain/go.mod`. Selected versions did not change.
- The Codex pull-request review hit its usage limit on PR 15. Codex reviews will not run until that limit resets.
- `.venv`, `node_modules` and `.toolchains` in this workspace are real directories, not symlinks. The clean-clone Python run was still taken.
- Round 2 rewrote the goal-11 history. The old tested commit `8084412` is not on this branch. The checks in this file are the ones that ran on `9a00e16`.

## Residual risks

- Reads trust one configured node. A lying node can make the broadcaster report COMMITTED.
- The read floor accepts an answer one block behind the status header while a block executes. An answer two or more blocks behind is refused. A key rotation in a block that has committed is visible once the query returns that height. A rotation in the block still executing is not committed yet.
- Indexing margin 1 is refused. L = 2 is the ordering argument for consensus. A catching-up node is outside that argument; a catching-up node is already refused by `ErrCatchingUp`.
- Signer start-up waits at most four poll intervals (2 seconds at the PROPOSED 500 ms poll). A governance read that is neither stale nor an empty height fails at once.
- Phase RS can still 503 if the node reports catching up after the wait returns. `post` will then fail the test instead of retrying.
- Distinct-uid signing was exercised by the root mutant run and by [uid-boundary.txt](logs/goal-11/uid-boundary.txt). A same-uid run does not verify that boundary.
- Open decision requests in `broadcaster.md` section 18 stay open. Their fail-closed interims are what the code does. They are not owner approvals.
- New CI check names `go-race (broadcaster)`, `go-race (devnet-broadcaster)` and `go-race (devnet-broadcaster-stall)` need a branch-protection update. That is an owner action. Branch protection was not changed.

## Follow-ups outside this goal

Stored genesis after a refused InitChain, `svnd genesis validate` gaps on `app_state.auth`, DR-10-44, and the goal 06 and 07 mutation anchors. None of these is done here.

## Next safe action

Draft PR 16 stays a draft. A merge is a separate approval. Head CI passed on `061496c` (run 42584861128, 13 of 13).

## Proposed pull-request title and summary

Title: goal 11: transaction broadcaster and outbox

Summary: `services/broadcaster` is the Go outbox and signing boundary. One `ADMITTED_SERVICE` key and one `BROADCASTER` key are read only by a separate signer. Relays are pre-checked and forwards are verbatim. Query floors use the committed application height. `indexing_margin_blocks` is at least 2. DR-11-01 is approved for the pinned file only. Head CI on `ad55070` passed (run 42578184489, 13 of 13). Status stays BLOCKED on DEC-001 and single-machine operation.
