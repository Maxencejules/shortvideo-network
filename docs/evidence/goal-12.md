# Goal 12 evidence

Status: BLOCKED

The indexer is implemented and the single-machine checks below ran. The goal stays BLOCKED: production custody is still DEC-001 and DR-07-01, every run is one machine, the goal 06 and goal 07 mutation anchors are still open, golangci-lint is not installed, and PostgreSQL 16 was not tested (the server in use is 18.6). A full-history backfill against a node configured to prune was not run. Head CI is recorded after the run that includes this file.

The checks cited in this file ran on commit `478f98cc6e99835e33500613a88f5a38c6d4a04b` of branch `goal-12`, tree
`86cdf8bcc38d6327f136daf704e8542488822b7c`. That commit's message carries `Tested-tree: 86cdf8bcc38d6327f136daf704e8542488822b7c`.
The evidence logs were captured after that commit. Commit `7d981b70a42d99ae7f1122273918897fa476307b` changes only `docs/evidence/`.

Head CI run [42695778372](https://github.com/Maxencejules/shortvideo-network/actions/runs/42695778372) on `7d981b70a42d99ae7f1122273918897fa476307b` failed the `python` job: `ruff format --check` wanted `GO_MODULES` in `scripts/run_tests.py` wrapped. The other 15 checks passed, including `go`, `devnet`, and every `go-race` shard (`indexer` and `devnet-indexer` among them). The following commit only wraps that tuple, records the two `go.sum` checksum lines `go mod tidy` keeps (`github.com/pion/stun v0.6.1` and `gotest.tools v2.2.0+incompatible`; the indexer source does not import them), and updates `docs/sbom.cdx.json`. It does not change indexer behaviour. The head CI run of that commit is cited when it has finished.

Public launch: DISABLED. No tradable token. This is not an audit and not a production network. Devnet results are four
validator processes on one machine. No independent operator is claimed.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH (`docs/goals/12-committed-state-indexer-and-reconciliation.md` line 6). GPT-6 Astra was not used.
- This run's `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed.
- On 2026-10-31 the owner waived the model requirement as a blocker ("Don't mind the ai model requirement", recorded on goal 04). On 2026-11-01 the owner wrote "I acknowledge the model substitution for DR-06-07" and then "extend the acknowledgement to goals 07 through 30". That covers model allocation only. It does not certify correctness.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `478f98cc6e99835e33500613a88f5a38c6d4a04b` |
| Tree | `86cdf8bcc38d6327f136daf704e8542488822b7c` |
| Base | `cb252873ec1a6e39ebebc0235f915c82e523da9d` (`origin/main` at the start of this goal, goal 11 merged). It is an ancestor of HEAD ([revision.txt](logs/goal-12/revision.txt)). |
| Branch | `goal-12` |
| Runtime | Ubuntu 24.04.5 LTS, Linux 6.12.113+ x86_64, uid 1000. Go 1.26.10. PostgreSQL 18.6. Python 3.12.3 from a venv used for the guard tests. golangci-lint was not installed. PostgreSQL 16 was not installed. [runtime.txt](logs/goal-12/runtime.txt) |

## Prerequisites carried from goal 11

BLOCKED, except where a row says otherwise. These are the goal 12 checks that depend on them:

1. **DEC-001 and DR-07-01, production custody.** The indexer does not hold a signing key and does not claim a production publication approval. A devnet content row is a coordination record, not that approval. Any statement that a hosted publication is approved stays BLOCKED.
2. **One machine.** `TestDevnetIndexer` is four validator processes on one machine. It is VERIFIED as that. It is not an independent-operator result. DEC-002 names no operator list.
3. **Goal 06 and goal 07 mutation runners.** Anchor checks only: [goal06-anchors.txt](logs/goal-12/goal06-anchors.txt) exit 1, `RESULT search-string problems=13; no test was run`; [goal07-anchors.txt](logs/goal-12/goal07-anchors.txt) exit 1, `RESULT validation problems=3; no test was run`. Still BLOCKED. No test was weakened to make them pass.
4. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-12/golangci-lint.txt) exit 127, `command not found`. BLOCKED.
5. **PostgreSQL 16.** `/usr/lib/postgresql/16/bin/postgres` is not installed. The server in use is 18.6. A 16.x server was not tested. The runtime log's process exit is 0 because the shell continued after the missing binary; the line in the log is the failure, and it is not a pass.
6. **Pruned full history.** The archive rule is in [indexer.md](../architecture/indexer.md). A proxy in front of the devnet made one height unavailable, and the indexer stopped. A node with pruning or `discard_abci_responses` enabled was not built. That full-history test is BLOCKED.

## Acceptance

| Check | Status | Log |
| --- | --- | --- |
| Kill between event handling and checkpoint, resume, one projection effect | VERIFIED | [devnet-indexer.txt](logs/goal-12/devnet-indexer.txt) exit 0. The fault point `after-effects-before-checkpoint` fired. `canonical` had no `content c1` line. After SIGKILL and restart there was one. |
| Rebuild and compare normalized rows | VERIFIED | Same log, `phase=done`. The comparison ignores the checkpoint height line, because empty blocks can commit during the rebuild. Content, source, and the other projection lines matched. |
| Malformed event version, forged response, missing block, chain-id mismatch | VERIFIED | Same log: `inject-chain-id exit=3`, `inject-forged exit=3`, `inject-missing exit=4`, `inject-version exit=3`. No content row was written by those passes. |
| Reconciliation detects an altered projection | VERIFIED | Same log. `reconcile-once` exited 5 and printed `divergent`. The altered manifest was still the altered value. |
| Stop and resume consensus; stale read; no false renewal or approval | VERIFIED | Same log. Two validators were stopped. Status became stale. The projection still had one content row and no lease row. After the validators restarted, status became fresh. There is no `publication_approved` column. |
| Goal 12 mutants | VERIFIED | [mutants.txt](logs/goal-12/mutants.txt) exit 0, `RESULT mutants=16 killed=16`. This is a spot check of the new code, not a mutation score. |
| Race, indexer module | VERIFIED | [race-indexer.txt](logs/goal-12/race-indexer.txt) exit 0. The devnet test in that log is not under `-race`; CI's `go-race (devnet-indexer)` shard is the race run of `TestDevnetIndexer`. |
| Head CI | FAILED on the evidence commit, then a format and SBOM fix | Run [42695778372](https://github.com/Maxencejules/shortvideo-network/actions/runs/42695778372) on `7d981b70`: `python` failed, 15 other checks passed. The fix is the commit after `7d981b70`. |

## Requirements rows

| Row | Status | Where |
| --- | --- | --- |
| REQ-IDEM-002 | VERIFIED | The store applies a block and advances the checkpoint in one transaction ([indexer-unit.txt](logs/goal-12/indexer-unit.txt), `TestApplyIsAtomicAndIdempotent`). The devnet kill/resume and rebuild are the process check. |
| REQ-SEC-006 | VERIFIED for the forged event, the chain-id mismatch, the missing height, and the altered projection on this devnet. Not a light-client proof. The trust sentence is on every status response. | [devnet-indexer.txt](logs/goal-12/devnet-indexer.txt), [indexer.md](../architecture/indexer.md) |

## Commands

Capture used `scripts/evidence/capture.sh`. Logs were written outside the worktree and copied here unchanged. Every committed log prints HEAD `478f98cc6e99835e33500613a88f5a38c6d4a04b`, tree `86cdf8bcc38d6327f136daf704e8542488822b7c`, and `DIRTY no`. The step table is [summary.tsv](logs/goal-12/summary.tsv).

| Step | Exit | Note |
| --- | --- | --- |
| indexer-unit | 0 | `go test` of `./services/indexer/...` |
| devnet-indexer | 0 | `TestDevnetIndexer`, single machine |
| race-indexer | 0 | `go test -race` of `./services/indexer/...` |
| mutants | 0 | 16 killed of 16 |
| python-guards | 0 | CI, scaffold, writer, and workflow tests |
| requirements | 0 | `check_requirements.py` |
| secrets | 0 | `secret scan ok` |
| gofmt | 0 | no files listed |
| goal06-anchors, goal07-anchors | 1 | BLOCKED. Not passes. |
| golangci-lint | 127 | BLOCKED. Not a pass. |

## Handoff

Hosting services read `GET /v1/status`, `/v1/providers`, `/v1/content`, `/v1/leases`, `/v1/deadlines`, and `/v1/policy` on the loopback listener, or `svn-indexer status`. Each status object names the configured gateway, sets `light_client` to false, and says the database is a read model. Archive and rebuild rules are in [indexer.md](../architecture/indexer.md).

## Next safe action

Open the draft pull request from `goal-12` into `main`. Do not merge it. Do not start goal 13. A merge is a separate approval.

## Proposed pull-request title and summary

Title: goal 12: committed-state indexer and reconciliation

Summary: `services/indexer` projects committed blocks into PostgreSQL, reconciles them, and can rebuild the projection schema without dropping other data. Status stays BLOCKED on DEC-001, one machine, the goal 06 and goal 07 anchors, golangci-lint, PostgreSQL 16, and an unrun pruned-node history test.
