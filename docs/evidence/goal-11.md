# Goal 11 evidence

Status: BLOCKED

DR-11-01 is approved for the pinned boundary document only. The goal stays BLOCKED: production custody is still DEC-001 and DR-07-01, every run is one machine, the goal 06 and goal 07 mutation anchors are still open, golangci-lint is not installed, and PostgreSQL 16 was not tested (the server in use is 18.6). The non-root PostgreSQL path in CI is not a blocker. Head CI is not a blocker.

The checks cited in this file ran on commit `8450fbdecf9bcec6bbc2de15c8927bff397b7c28` of branch `goal-11`, tree
`a94f4848ee4ff54f8209fbb5b9afd969b7926bf7`. That commit's message carries `Tested-tree: a94f4848ee4ff54f8209fbb5b9afd969b7926bf7`.
The evidence logs were captured after that commit. This commit changes only `docs/evidence/`.

Public launch: DISABLED. No tradable token. This is not an audit and not a production network. Devnet results are four
validator processes on one machine, in a private network namespace. No independent operator is claimed.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH (`docs/goals/11-transaction-broadcaster-and-outbox.md` line 6). GPT-6 Astra was not used.
- Waves 0-2 and the flake fixes, before this handover, ran in Claude Code session `session_014mwakeb27YrugDfwAx8vXL`, model `claude-opus-5-5`, effort `ultracode`. Sources named by the handover live on branch `goal-11-handover` and are not in this tree. The `handover/` folder was not committed here.
- Rounds 1 and 2, and this round 3 (the progress-row alignment, the new tested commit, and this retest), ran as a Cursor cloud agent. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed.
- On 2026-10-31 the owner waived the model requirement as a blocker ("Don't mind the ai model requirement", recorded on goal 04). On 2026-11-01 the owner wrote "I acknowledge the model substitution for DR-06-07" and then "extend the acknowledgement to goals 07 through 30". That covers model allocation only. It does not certify correctness.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `8450fbdecf9bcec6bbc2de15c8927bff397b7c28` |
| Tree | `a94f4848ee4ff54f8209fbb5b9afd969b7926bf7` |
| Parent | `7ab95e01ac368994743d66037c07d07b392321c0` |
| What this tested commit changes | `docs/progress.md` and `docs/progress.tsv` only. They drop the non-root PostgreSQL CI path from the blocker list and name PostgreSQL 16. |
| Base | `6d74aca5cfc461a9756587bdbec2f78e7111e574` (`main` after PR 15). It is an ancestor of HEAD ([stack.txt](logs/goal-11/stack.txt)). |
| Branch | `goal-11` |
| Pull request | Draft [PR 16](https://github.com/Maxencejules/shortvideo-network/pull/16) |
| Runtime | Ubuntu 24.04.5 LTS, Linux 6.12.113+ x86_64, uid 1000, 4 CPUs, 16003 MiB. Go 1.26.10. PostgreSQL 18.6. Python 3.12.3, ruff 0.17.3, mypy 2.4.1. libprotoc 36.2. Node v22.23.4. `npm` and golangci-lint were not installed. PostgreSQL 16 was not installed. [runtime.txt](logs/goal-11/runtime.txt) |

Chain production diff since the base is only `chain/go.mod`, moving `github.com/lib/pq v1.12.3` from an indirect requirement to a direct one. `chain/go.sum` is unchanged ([chain-prod-diff.txt](logs/goal-11/chain-prod-diff.txt), `SCOPE OK`). `GOWORK=off` chain module listings match the base, 746 lines ([dependency-identity.txt](logs/goal-11/dependency-identity.txt)).

On 2026-11-08 at 21:39 America/Toronto the owner wrote, verbatim: "I approve docs/architecture/broadcaster.md at sha256 9779fdb1e7fe5a7577565c64a8066b6bff7e7bace1174b085df81e7b17ae905b (DR-11-01)". That approves the document only. The file still reads PROPOSED and is not edited. Proposed numbers are not approved. Its sha256 is unchanged.

## Prerequisites

BLOCKED, except where a row says otherwise:

1. **DR-11-01: no longer a blocker (2026-11-08).** Document only, at the sha256 above. Not a merge.
2. **REQ-CHAIN-013 production custody.** Host custody of user keys is still blocked by DEC-001 and DR-07-01. The devnet fixture's refusal of a minted provider signature and of a missing publication permission is in the policy and devnet logs. The row is not production-verified.
3. **Head CI: no longer a blocker.** Draft PR 16. Run [42627159750](https://github.com/Maxencejules/shortvideo-network/actions/runs/42627159750) on `6d4b0b7eeb3a1a14f081955e619e9565f7e96aaa` completed success, 13 of 13: `python`, `node`, `go`, `devnet`, `contract`, and `go-race` for `app`, `devnet`, `devnet-evidence`, `devnet-upgrade`, `rest`, `broadcaster`, `devnet-broadcaster` and `devnet-broadcaster-stall`. That is the head this sentence records. Run [42624725287](https://github.com/Maxencejules/shortvideo-network/actions/runs/42624725287) on `f26c188`, which holds the recaptured logs, was also 13 of 13.
4. **Independent operators.** Every devnet run is four processes on one machine. DEC-002 names no operator list.
5. **Goal 06 and goal 07 mutation runners.** Anchor checks only: [goal06-anchors.txt](logs/goal-11/goal06-anchors.txt) exit 1, `RESULT search-string problems=13; no test was run`; [goal07-anchors.txt](logs/goal-11/goal07-anchors.txt) exit 1, `RESULT validation problems=3; no test was run`. Still BLOCKED.
6. **golangci-lint.** Not installed here. [golangci-lint.txt](logs/goal-11/golangci-lint.txt) exit 1, `command not found`, `chain=127`. BLOCKED.
7. **Non-root PostgreSQL path in CI: no longer a blocker.** The `go` job runs `python3 scripts/run_tests.py --only go` without sudo (`.github/workflows/ci.yml`). On run 42586688727 that job's pgdev, store and worker tests passed as the runner user. The local capture also ran those tests as uid 1000 ([runtime.txt](logs/goal-11/runtime.txt)). `docs/progress.md` and `docs/progress.tsv` say the same.
8. **PostgreSQL 16: still BLOCKED.** `/usr/lib/postgresql/16/bin/postgres` is not installed. The server in use is 18.6. A 16.x server was not tested. The progress rows name this gap.

## Acceptance

| Check | Status | Log |
| --- | --- | --- |
| Devnet lifecycle | VERIFIED | [devnet-broadcaster.txt](logs/goal-11/devnet-broadcaster.txt) exit 0, 152s, zero `api-retry` lines. Section 3.2 also ran it twice without `-race` and once with `-race`, each exit 0. |
| Quorum stop stays pending or failed, not a false COMMITTED | VERIFIED | [devnet-broadcaster-stall.txt](logs/goal-11/devnet-broadcaster-stall.txt) exit 0, 61s. Section 3.2 also ran it once without `-race` and once with `-race`, each exit 0. |
| Unauthorized messages and provider impersonation refused | VERIFIED | Lifecycle phases in the same devnet log; policy tests in [unit-broadcaster.txt](logs/goal-11/unit-broadcaster.txt). |
| Restart PostgreSQL and the broadcaster, then reconcile | VERIFIED | Lifecycle phases PG and BR in [devnet-broadcaster.txt](logs/goal-11/devnet-broadcaster.txt). |
| Read floor and indexing margin | VERIFIED | Mutants `f1-app-floor-is-status-height`, `f1-signer-startup-does-not-wait` and `f3-indexing-margin-one-allowed` are among the 284 killed ([mutants-goal11.txt](logs/goal-11/mutants-goal11.txt)). |
| Signing boundary document approved | VERIFIED for the pinned file only | DR-11-01. Proposed numbers stay proposed. |
| Head CI | VERIFIED | Run 42627159750 on `6d4b0b7`, 13 of 13, including `go-race (broadcaster)`, `go-race (devnet-broadcaster)` and `go-race (devnet-broadcaster-stall)`. Run 42624725287 on `f26c188` was also 13 of 13. |

## Requirements rows

| Row | Status | Where |
| --- | --- | --- |
| REQ-CHAIN-013 | BLOCKED for production custody (DEC-001, DR-07-01). The devnet fixture refuses a minted provider signature and a missing publication permission. | [unit-broadcaster.txt](logs/goal-11/unit-broadcaster.txt), [devnet-broadcaster.txt](logs/goal-11/devnet-broadcaster.txt) |
| REQ-ACK-001 | VERIFIED on the devnet and by `TestAckNeverCommits` | devnet lifecycle log and the broadcaster unit log |
| REQ-INT-001 | VERIFIED for the chain-writer guard. This goal has no web client. | [chain-writer-policy.txt](logs/goal-11/chain-writer-policy.txt) |
| REQ-IDEM-001 | VERIFIED | devnet duplicate and restart phases; `TestIdempotentReplayNeedsFirstCommit` |
| REQ-QA-008 | VERIFIED for one final transition and no outbox lease table | devnet timeout, restart and quorum logs; `TestNoLeaseOrProjectionTable` |

## Commands

Section 3.2 on `8450fbd` exited 0: `gofmt` and `go vet`; `go test -race` of `./services/broadcaster/...` (17 packages `ok`); the two worker tests `-race -count=30`; the lifecycle three times, the third with `-race`, and the stall twice, the second with `-race`, each with zero `api-retry` lines; `mutants.py --rev 8450fbd --check`; groups chain-client, api, config and outbox (`112 killed`); signing as root (`57 killed`); worker (`35 killed`); devnet (`4 killed`); `go mod tidy -diff` empty in `services/broadcaster` and `chain`; pytest of the three CI guard files (123 passed); `scripts/scan_secrets.py` (`secret scan ok`). Scratch logs are under `/tmp/goal11-r3/` and are not committed.

Capture used `scripts/evidence/capture.sh`. Logs were written outside the worktree and copied here unchanged. Every committed log prints HEAD `8450fbdecf9bcec6bbc2de15c8927bff397b7c28`, tree `a94f4848ee4ff54f8209fbb5b9afd969b7926bf7`, and `DIRTY no`. [revision.txt](logs/goal-11/revision.txt) prints an empty `git status` between the markers. The step table is [summary.tsv](logs/goal-11/summary.tsv).

| Step | Exit | Note |
| --- | --- | --- |
| devnet-broadcaster | 0 | 152s. Zero `api-retry` lines. |
| devnet-broadcaster-stall | 0 | 61s. |
| worker-count30 | 0 | 60 `--- PASS`, 0 `--- FAIL`. |
| race-broadcaster | 0 | 17 `ok` lines. |
| tests-python, tests-python-clean-clone | 0 | 405 passed each. |
| tests-node | 0 | 13 passed and 1 passed. |
| chain-writer-policy | 0 | 123 passed. |
| ci-shards | 0 | Eight shards, each exit 0. |
| mutants-goal11 | 0 | `RESULT mutants=284 killed=284` |
| mutants-goal10 | 0 | `RESULT mutants=303 killed=303` |
| mutants-goal09 | 0 | `RESULT mutants=169 killed=169` |
| mutants-goal08 | 0 | `RESULT mutants=28 killed=28` |
| fuzz-FuzzClassifyRPCError | 0 | The first run exited 1 with `context deadline exceeded` at fuzztime 60s and no failing input. [rerun-driver.txt](logs/goal-11/rerun-driver.txt). The replacement exited 0. |
| golangci-lint, goal06-anchors, goal07-anchors | 1 | BLOCKED. Not passes. |

## Disclosures

- Round 3 changes no Go file. The tested commit changes only the two progress rows.
- PostgreSQL 16 was not tested. PostgreSQL 18.6 was.
- `.venv`, `node_modules` and `.toolchains` are real directories, not symlinks. The clean-clone Python run was still taken.
- Open decision requests in `broadcaster.md` section 18 stay open. Their fail-closed interims are what the code does.
- New CI check names `go-race (broadcaster)`, `go-race (devnet-broadcaster)` and `go-race (devnet-broadcaster-stall)` need a branch-protection update. That is an owner action. Branch protection was not changed.

## Follow-ups outside this goal

Stored genesis after a refused InitChain, `svnd genesis validate` gaps on `app_state.auth`, DR-10-44, and the goal 06 and 07 mutation anchors. None of these is done here.

## Next safe action

Draft PR 16 stays a draft. A merge is a separate approval. Head CI on `6d4b0b7` passed (run 42627159750, 13 of 13).

## Proposed pull-request title and summary

Title: goal 11: transaction broadcaster and outbox

Summary: `services/broadcaster` is the Go outbox and signing boundary. DR-11-01 approves the pinned document only. The non-root PostgreSQL CI path is not a blocker. Status stays BLOCKED on DEC-001, one machine, the goal 06 and goal 07 anchors, golangci-lint, and PostgreSQL 16 (the server in use is 18.6).
