# Goal 28 evidence

Status: BLOCKED

This VM measured one synthetic encode, one in-process chain fixture, and one local repair fixture. The shaped playback gate did not run. The design point of 100 viewers and 10 uploads did not run. These observations are not a universal hardware minimum, not an operating budget, and not a capacity claim beyond the sample sizes below. Public launch stays disabled. There is no token.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/28-performance-load-and-capacity-evidence.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-f87002a9-dd37-5f28-a7cb-3196438c7501`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run. The playbook's GPT-6.1 Sol was replaced by this Cursor cloud agent on that default model.
- On 2026-10-31 (Toronto) the owner said "Don't mind the ai model requirement". On 2026-11-17 the owner confirmed that it applies to all goals. A model mismatch with the playbook is not a blocker.
- On 2026-11-21 the owner decided this goal runs on this cloud VM only. There is no approved integration or load runner and no resource budget. No other machine, paid service, or third-party load service was used.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `14a39c168d148e4ba67f1e009671d9e5ddcb6820` |
| Tree | `278e9dd5e112424481f316d56ea9fd1a82d13eb4` |
| Base | `0600c56aa38c43dc007df4737c0f981e2d66bccf` |
| Branch | `goal-28` |
| Pull request | https://github.com/Maxencejules/shortvideo-network/pull/33 |

The tested commit message carries `Tested-tree: 278e9dd5e112424481f316d56ea9fd1a82d13eb4`. `git rev-parse 14a39c168d148e4ba67f1e009671d9e5ddcb6820^{tree}` is that tree. Later commits change only `docs/evidence/`.

Earlier commits on this branch are not this measurement. `8cd0fb8` is the scheduler code before the confirmation change. `3f41470` and `8a439ed` are intermediate and were not the measured tree.

## Machine

From [measure.json](logs/goal-28/measure.json), taken at the start of the run, before `iproute2` was installed.

| Item | Line | Value |
| --- | --- | --- |
| CPUs | 57 | `nproc` 4 |
| CPU model | 54 | `Intel(R) Xeon(R) Processor` |
| Memory | 53 | `mem_total_kib` 16387328 |
| OS | 58 | `Ubuntu 24.04.5 LTS` |
| Kernel | 63 | `Linux 6.12.113+ x86_64` |
| FFmpeg | 51 | `ffmpeg version 6.1.1-3ubuntu5` |
| Chrome | 49 | `Google Chrome 157.0.8103.50` |
| PostgreSQL client | 60 | `psql` `absent` |
| Docker | 50 | `absent` |
| Worker concurrency | 64 | 1 |

Line 55 is `not_a_universal_minimum` true. Line 56 is `not_an_operating_budget` true. Line 61 records `tc` as `absent` because that snapshot is before the install. The later qdisc commands did run `tc`. See [qdisc.txt](logs/goal-28/qdisc.txt).

## Captures

Logs under [logs/goal-28/](logs/goal-28/) other than [repair-before.txt](logs/goal-28/repair-before.txt) were captured while `git status --porcelain` was empty. Each of those files has `DIRTY no` on line 4 and `AFTER clean` as its last status line. [measure.txt](logs/goal-28/measure.txt) line 7 is `DONE`, printed by the capture wrapper after line 6 `AFTER clean`. Nothing tracked was left untracked. A gitignored `.venv` was present and is not in those stamps.

[repair-before.txt](logs/goal-28/repair-before.txt) is the pre-change repair run. Line 1 is `HEAD 8cd0fb8e0ef44a936115c510138a0700db3bd1b7`. Line 4 is `DIRTY  M scripts/capacity.py;`. Line 13 is the same dirty path. That dirty file was the runner's `-v` flag. The scheduler sources in that tree are the ones before the confirmation change. The after measurement is on the tested commit.

## Head CI

[Run 44931723618](https://github.com/Maxencejules/shortvideo-network/actions/runs/44931723618) is `ci` on `14a39c168d148e4ba67f1e009671d9e5ddcb6820`. Conclusion: `success`. All 17 jobs passed, including `python`, `go`, and `devnet`.

Earlier runs on this branch were cancelled when a later commit was pushed: `44931502433`, `44931410669`, and `44931003554`. Those cancellations are the workflow concurrency setting. They are not failures of this commit.

This file is added after the `ci` run above. A later head that only changes `docs/evidence/` is not that run.

## What ran

| Command | Scope | Log | Result |
| --- | --- | --- | --- |
| `scripts/capacity.py measure` | clean tree | [measure.txt](logs/goal-28/measure.txt), [measure.json](logs/goal-28/measure.json) | line 5 `EXIT:0` |
| pytest of the capacity and delivery unit files | those tests | [pytest.txt](logs/goal-28/pytest.txt) | line 5 is 18 dots and `[100%]`, line 6 `EXIT:0` |
| `ruff check` | the CI python paths | [ruff.txt](logs/goal-28/ruff.txt) | line 5 contains `All checks passed`, line 6 `EXIT:0` |
| `ruff format --check` | the same paths | [ruff-format.txt](logs/goal-28/ruff-format.txt) | line 5 `90 files already formatted`, line 6 `EXIT:0` |
| `mypy` | the configured files | [mypy.txt](logs/goal-28/mypy.txt) | line 5 `Success: no issues found in 72 source files`, line 6 `EXIT:0` |
| scheduler package tests | conformance, without the capacity tag | [conformance.txt](logs/goal-28/conformance.txt) | line 5 `ok` in 7.487s, line 8 `EXIT:0` |
| `tc` qdisc probe | this kernel | [qdisc.txt](logs/goal-28/qdisc.txt) | line 7 `TBF_EXIT:2`, line 10 `NETEM_EXIT:2` |

The procedure is [load-plan.md](../operations/load-plan.md). CI does not run `scripts/capacity.py measure` and does not pass `-tags svn_capacity`.

## Gates

Targets stay PROPOSED. A missing run is BLOCKED.

| Gate | Target | Observed | Sample | Status |
| --- | --- | --- | --- | --- |
| Processing to ready-for-review | at most 120 s | 3.51 s | 1 job, concurrency 1 | PASS |
| Playback p95 startup | at most 3000 ms, shaped | not started | 0 | BLOCKED |
| Rebuffer ratio | at most 0.01, `stall_ms / media_ms` | not started | 0 | BLOCKED |
| Design point | 100 viewers and 10 uploads | 0 viewers, 1 encode | the one encode is the processing row | BLOCKED |
| Chain fixture | record bytes, gas, state, catch-up | see below | 1 registration | PASS as a record |
| Index lag | record canonical height minus indexed height | indexer not started | 0 | BLOCKED |
| CometBFT commit interval | record it | in-process ABCI only | 0 blocks of CometBFT | BLOCKED |
| Repair of the 60-second package | record it | package was not handed to the scheduler | 0 | BLOCKED |
| Repair of the 64-byte fixture | record it; one machine | 117 ms, `CONFIRMED`, replacement `p4` | 1 | PASS as a record |
| Multi-host loss | a second machine leaves | not run | 0 | BLOCKED |
| Moderation of the 60-second object | separate from processing | not run | 0 | BLOCKED |
| Publication of the 60-second object | separate from ready-for-review | `NOT_PUBLISHED` | 0 | BLOCKED |
| Slow provider on a shaped path | a shaped delay | not run | 0 | BLOCKED |
| In-process approver sleep | record it | 982 ms with 250 ms times 3 approvals | 1 publish | PASS as a record |

## Processing

[measure.json](logs/goal-28/measure.json) line 76 is `elapsed_seconds` 3.51. Line 78 is `generate_seconds` 2.77. The 120 s comparison uses 3.51, the `run_once` wall time after the synthetic file was already in quarantine. 3.51 is less than 120. Line 77 is `PASS`. Line 106 is `READY_FOR_REVIEW`. Line 86 is `NOT_PUBLISHED`. Line 85 is `package_bytes` 1620354. Line 75 is concurrency 1. The input line 79 is the synthetic `testsrc`. Line 73 says the admission stand-in is not a DEC-005 allowance.

Line 101 is `cpu_busy_ratio` 0.5497. That is `(total-idle)/total` from `/proc/stat` over the encode window. Line 104 is `peak_tree_rss_kib` 247740. Line 103 is `min_mem_available_kib` 4757896. The window was one encode. It is not a 100-viewer saturation result.

Moderation is line 82 `BLOCKED`. Replication of this package is line 97 `BLOCKED`. Publication is line 88 `BLOCKED`.

## Playback and the network shape

The load plan applies `tbf` at `20mbit` and `netem` delay `25ms` on a namespace loopback, then checks ping and a 5000000-byte download before any browser sample. [measure.json](logs/goal-28/measure.json) line 68 is `BLOCKED` and line 70 is `qdisc was not installed`. Line 67 is `Specified qdisc kind is unknown`.

[qdisc.txt](logs/goal-28/qdisc.txt) line 6 and line 9 are that same kernel error. Line 7 is `TBF_EXIT:2`. Line 10 is `NETEM_EXIT:2`. Line 11 is the wrapper `EXIT:0` after those two failures. Both qdisc kinds are unknown to this kernel. No playback sample was started. A startup time was not extrapolated from the 1620354-byte package.

Chrome is installed (hardware line 49). It was not asked to play, because the shape was not in place. A segment download was not treated as playback. `apps/web/src/playback.ts` line 369 selects the first master variant. That page was not run.

Denominators, unused because the sample size is 0: startup is `renderMs` from the page script to the first non-zero decoded frame; rebuffer ratio is `stall_ms / media_ms`; p95 rank is `(95 * n + 99) // 100`. Server `handler_ms` is a different clock from `renderMs`.

## Chain fixture

[measure.json](logs/goal-28/measure.json) lines 16 through 33. One committed `RegisterContent` on two in-process nodes. Not CometBFT and not a throughput claim.

| Field | Line | Value |
| --- | --- | --- |
| `tx_bytes` | 32 | 394 |
| `gas_used` | 23 | 111353 |
| `payload_bytes` | 27 | 195 |
| `state_before` | 30 | 2996 |
| `state_after` | 29 | 3194 |
| `state_delta` | 31 | 198 |
| `dup_code` | 19 | 32 |
| `dup_state_delta` | 20 | 0 |
| `commit_wall_ms` | 18 | 2 |
| `empty_wall_ms` | 22 | 0 |
| `empty_hash_changed` | 21 | false |
| `replay_wall_ms` | 28 | 17 |
| `heights` | 24 | 4 |
| `nodes` | 26 | 2 |

3194 minus 2996 is 198. The repeated transaction bytes were rejected (`dup_code` 32) and the logical state size did not change. State is the sum of key and value bytes in the application stores, not IAVL disk bytes. `replay_wall_ms` is `replayFresh` of those four heights on one new process. It is not a peer catching up over the network.

Lines 11 through 13 are the CometBFT commit interval, and line 12 is `BLOCKED`. Lines 35 through 37 are index lag, and line 36 is `BLOCKED`, because PostgreSQL is absent and the indexer was not started.

The blueprint's 800 transactions per day is line 3, and `800/86400` is line 4. Those are the planning example. They are not the measured `tx_bytes` or `gas_used`.

## Repair, host loss, and the slow approver

The 60-second package was not given to the scheduler. The scheduler fixture is 64 bytes on four processes on this machine. Labels are simulated. Line 112 is multi-host `BLOCKED`.

After the confirmation change, [measure.json](logs/goal-28/measure.json) line 134 is repair `wall_ms` 117. Line 132 is `CONFIRMED`. Line 129 is replacement `p4`. Line 131 is source `p2`. Line 122 is the publish before that stop, `wall_ms` 180. Line 141 is the slept publish, `wall_ms` 982, with line 138 `injected_ms` 250 and line 137 `approvals` 3. 250 times 3 is 750. 982 minus 750 is 232, which is the wall time beside those three sleeps. That sleep is in-process. It is not a shaped network.

Before the change, [repair-before.txt](logs/goal-28/repair-before.txt) line 7 is `wall_ms=20093` for the same `CONFIRMED` outcome, source `p2`, replacement `p4`. The default wait is `services/scheduler/internal/sched/sched.go` line 438, 20 seconds. An unreachable holder was keeping `confirm` in that window, and `confirm` then returned anyway. The change skips a holder whose health probe fails (`sched.go` lines 470 and 471). A host that still answers and fails the byte check still waits (`sched.go` line 479 requires `verified > 0`). Repair still verifies the replacement after that wait (`sched.go` line 636) and only then sets `CONFIRMED` (line 641). [conformance.txt](logs/goal-28/conformance.txt) line 5 is the scheduler command tests passing after that change.

## Arithmetic

[measure.json](logs/goal-28/measure.json) line 5 labels the block as blueprint arithmetic, not a measurement. Line 8 is 900. Line 7 is 45. Line 6 is `peer_share` null.

`100 * 20 * 60 * 2 * 30 / 8000` is 900. `20 * 25 * 3 * 30 / 1000` is 45. `200 * 4` is 800, and 800 divided by 86400 is the illustrative rate. Peer share is not filled with zero. Viewer payload, replication, repair, prefetch, overhead, and relay stay separate. Overhead and relay were not measured. The playback run did not deliver segment bytes. The repair fixture moved 64 bytes on one machine. That 64 is not added to a viewer-payload total.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-28-01 | OPEN, BLOCKED | No approved integration or load runner. The design point was not started. |
| DR-28-02 | OPEN, BLOCKED | No resource budget. Owner decision 2026-11-21: this VM only. No other machine and no spend. |
| DR-28-03 | OPEN, BLOCKED | This kernel rejects `tbf` and `netem`. The 20 Mb/s and 50 ms shape was not applied. |
| DR-28-04 | OPEN, BLOCKED | Browser playback under that shape was not started. Chrome is installed. Sample size 0. |
| DR-28-05 | OPEN, BLOCKED | 100 simultaneous viewers and 10 upload sessions were not started. |
| DR-28-06 | OPEN, BLOCKED | Index lag was not measured. PostgreSQL is absent. The indexer was not started. |
| DR-28-07 | OPEN, BLOCKED | CometBFT commit interval was not measured. The chain fixture is in-process ABCI. |
| DR-28-08 | OPEN, BLOCKED | Multi-host loss was not run. The repair record is four processes on one machine and 64 bytes. |
| DR-28-09 | OPEN, BLOCKED | The 60-second object was not moderated and was not published. DEC-003 is open. |
| DR-28-10 | OPEN, BLOCKED | That package was not handed to the scheduler, so its replication time was not measured. |
| DR-28-11 | OPEN | The encode used the unit-test admission stand-in. That is not `svn-observe` and not a DEC-005 allowance. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. DEC-001 and DR-07-01. This goal does not take custody.
2. DEC-002. No operator is named. The repair labels are simulated.
3. One machine. Four provider processes are one failure domain.
4. Docker is absent. Images were not run. Port 2375 was not used.
5. PostgreSQL is absent. PostgreSQL 16 was not used.
6. golangci-lint was not run.
7. DR-27-01 through DR-27-13 stay as goal 27 recorded them. The candidate remains NOT-RELEASED.

## Requirements

The requirements matrix was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-OPS-002 | VERIFIED for the rows above. Hardware, codec, and concurrency are with the encode. No universal minimum is stated. |
| REQ-CAP-001 | VERIFIED as a label. The 900 and 45 figures are marked arithmetic. |
| REQ-CAP-002 | VERIFIED as a worksheet. Peer share is null. Classes are not added together. |
| REQ-CAP-004 | VERIFIED. The measured transaction is separate from the 800-transaction example. |
| REQ-CAP-005 | The fixture bytes, gas, state delta, and `replayFresh` time are recorded. A network catch-up and index lag are BLOCKED. |
| REQ-EVID-005 | BLOCKED. Playback startup was not measured, so it was not mixed with a storage attestation. |
| REQ-QA-005 | BLOCKED. The shape was not installed. Sample size 0. |
| REQ-QA-006 | PASS for the one synthetic job: 3.51 s, concurrency 1, this hardware. The 120 s figure stays PROPOSED. |
| REQ-QA-007 | BLOCKED. Viewers started: 0. Upload sessions started: 1, and that one is the processing row, not 10. |

## Next safe action

Do not treat this file as proof of the design point. Do not publish a package. A shaped playback run needs a kernel that accepts `netem` or an approved runner the owner has not named. Goal 29 is the next goal. Public launch stays disabled.
