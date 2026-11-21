# Goal 30 evidence

Status: BLOCKED

This file records the candidate `2bce2e272534316115c2550329c7ed730983da70` and the checks that ran on this VM. The release decision is **implemented prototype**. It is not a beta candidate and not a beta approved after human sign-off. No owner signed moderation coverage, a privacy and rights policy, funding and quotas, operator participation, or a security review. Public launch stays disabled. There is no token. Completing goals 00 through 30 is not approval.

The decision, the blank sign-off rows, the rollback limits, and the unlaunched first-community plan are in `docs/release-readiness.md`. The operator handoff is `docs/operations/operator-handoff.md`. The index is [index.md](index.md).

## Model and effort

- Playbook assignment: GPT-6 Astra, effort EXTRA HIGH (`docs/goals/30-final-acceptance-and-independent-installation.md` line 6). GPT-6 Astra was not used.
- This run is Cursor cloud agent `bc-e92d19a4-024a-5f80-b293-ab2d52f342cd`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7` and no effort field. The task states `reasoning_effort` xhigh. `run-info` did not report that effort. It is not claimed as a `run-info` field.
- On 2026-10-31 the owner said "Don't mind the ai model requirement". On 2026-11-17 the owner confirmed that it applies to all goals. On 2026-11-21 at 15:11 Toronto, in the shortvideo-builder chat, answering "merge goal 29?", Max chose this option text, which shortvideo-builder wrote: "merge goal 29 pr 34 at 62a2961 as BLOCKED, squash as me, then start goal 30 at extra high reasoning". He did not name a model. shortvideo-builder picked Grok 4.7 because a non-Grok model needed paid credits. That choice is not a waiver.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `2bce2e272534316115c2550329c7ed730983da70` |
| Tree | `925512e799ec3ac42079b2e76bb9a9acd2bc56c7` |
| Base | `6d259b8dd159ed21477b3398970e0b0f4848c513` |
| Branch | `goal-30` |
| Pull request | https://github.com/Maxencejules/shortvideo-network/pull/35 |

The tested commit message carries `Tested-tree: 925512e799ec3ac42079b2e76bb9a9acd2bc56c7`. `git rev-parse 2bce2e272534316115c2550329c7ed730983da70^{tree}` is that tree. This file is a later commit and changes only `docs/evidence/`.

An earlier commit on the branch, `e7591f4b02ebbb2d052641f5831f1ea46e465e93`, tree `3859fdec157f7b0e997c7e398267190c60cdf129`, also carries a `Tested-tree` trailer. That tree is not the candidate. Its PostgreSQL media-claim fixture omitted `idempotency_key`. The candidate adds the column. [pytest-e7591f4.txt](logs/goal-30/pytest-e7591f4.txt) is that earlier run.

## Machine

This VM only. No paid runner, no other machine, and no separate operator. The install below is an installation rehearsal by the agent.

| Item | Value |
| --- | --- |
| CPUs | `nproc` 4 ([runtime.txt](logs/goal-30/runtime.txt) line 5) |
| Memory | `mem_kb 16387328` (line 6) |
| Kernel | Linux 6.12.113+ (line 7) |
| Go | `go1.26.10` at `.toolchains/go/bin/go` (lines 8-9) |
| Python | 3.12.3 (line 10) |
| Node | v22.23.4 (line 11) |
| FFmpeg | 6.1.1-3ubuntu5 (line 14). The same log prints an ncurses version warning from the tmux library path. The version line is the pin. |
| PostgreSQL | 18.6 (Ubuntu 18.6-1.pgdg24.04+2) (line 18) |
| Docker CLI | absent (line 19) |
| golangci-lint | absent (line 20) |
| `unshare` | present (line 22) |

The image's `/usr/bin/go` is go1.22.2. `go.work` requires go 1.26.5 or newer. With `GOTOOLCHAIN=local`, that binary stops. The recorded Go commands use the pin in `scripts/install_toolchains.sh`: Go 1.26.10, sha256 `ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5`. `sha256sum -c` printed OK before these logs.

`command -v tc` in the runtime log printed `tc absent` (line 21). `/usr/sbin/tc` is on the machine. [absent.txt](logs/goal-30/absent.txt) lines 10-12 are qdiscs for `lo`, `enp0s3`, and `docker0`. Line 13 is `tc exit 0`. The capacity measure still reports the playback qdisc as unavailable. See DR-30-10.

Port 2375 was not used. No Docker API request was sent. The `docker0` qdisc is an interface on this VM. It was not a docker context for this goal.

## Head CI

[Run 44946962593](https://github.com/Maxencejules/shortvideo-network/actions/runs/44946962593) is `ci` on `2bce2e272534316115c2550329c7ed730983da70`. Conclusion: `success`. All 17 jobs passed: `contract`, `go`, `python`, `node`, `devnet`, `milestone`, and the eleven `go-race` jobs (`app`, `rest`, `indexer`, `broadcaster`, `api`, `devnet`, `devnet-evidence`, `devnet-upgrade`, `devnet-broadcaster`, `devnet-broadcaster-stall`, `devnet-indexer`).

Run 44946187749 on the earlier commit `e7591f4` was cancelled when `2bce2e2` was pushed. It is not a result for the candidate.

This file is added after run 44946962593. A later head that only changes `docs/evidence/` is not that run. The GitHub `devnet` job is that runner's container proof. It is not this VM and not a separate operator.

## What ran

Logs were captured on a clean clone of the candidate except where a log says `DIRTY`. Line 1 is the commit. Line 2 is the tree. The command is the `CMD` line. `EXIT` is the command status.

| Command | Log | Result |
| --- | --- | --- |
| runtime | [runtime.txt](logs/goal-30/runtime.txt) | `EXIT:0` |
| requirements checker | [requirements.txt](logs/goal-30/requirements.txt) | line 64 `result PASS`, line 66 `EXIT:0` |
| protocol checker | [protocol.txt](logs/goal-30/protocol.txt) | line 9 `go_vectors exit:0`, line 84 `errors 0`, line 86 `result PASS`, line 88 `EXIT:0` |
| matrix audit | [audit.txt](logs/goal-30/audit.txt) | line 5 `RESULT rows=265 in_scope=250 test=216 blocker=34 out=15 goal30=10`, line 6 `EXIT:0` |
| matrix scopes | [matrix-scopes.txt](logs/goal-30/matrix-scopes.txt) | line 5 `rows 265`, lines 6-8 scopes, line 13 `goal30 10`, line 25 `EXIT:0` |
| ruff format | [ruff-format.txt](logs/goal-30/ruff-format.txt) | `EXIT:0` |
| ruff check | [ruff-check.txt](logs/goal-30/ruff-check.txt) | `EXIT:0` |
| mypy | [mypy.txt](logs/goal-30/mypy.txt) | `EXIT:0` |
| workflow policy | [workflow.txt](logs/goal-30/workflow.txt) | `EXIT:0` |
| secret scan | [secrets.txt](logs/goal-30/secrets.txt) | `EXIT:0` |
| SBOM check | [sbom.txt](logs/goal-30/sbom.txt) | `EXIT:0` |
| gofmt | [gofmt.txt](logs/goal-30/gofmt.txt) | `EXIT:0` |
| schema generation | [gen-proto.txt](logs/goal-30/gen-proto.txt) | `EXIT:0`, `AFTER clean` |
| second client self-check | [self-check.txt](logs/goal-30/self-check.txt) | line 5 `CLIENT source_problems=0`, line 6 `CLIENT operations=12`, line 10 `EXIT:0` |
| pytest | [pytest.txt](logs/goal-30/pytest.txt) | line 13 one skip, line 14 `476 passed, 1 skipped`, line 15 `EXIT:0` |
| node | [node.txt](logs/goal-30/node.txt) | eight `# tests` lines sum to 56, eight `# fail` lines are 0, line 437 `EXIT:0` |
| Go packages with `SVN_SKIP_DEVNET=1` | [go-unit.txt](logs/goal-30/go-unit.txt) | line 44 `RESULT passed=4235 failed=0 skipped=15 packages=89 nonjson=0`, line 149 `EXIT:0` |
| go vet and faultpoint tags | [go-vet.txt](logs/goal-30/go-vet.txt) | line 42 `EXIT:0` |
| packaging candidate | [packaging.txt](logs/goal-30/packaging.txt) | line 5 `NOT-RELEASED candidate 2bce2e2`, line 6 `EXIT:0` |
| two-build compare and bundle label | [build-compare.txt](logs/goal-30/build-compare.txt) | line 17 `binaries 12 mismatch 0`, line 26 `match yes`, line 33 `EXIT:0` |
| `svnd version` from that bundle | [svnd-version.txt](logs/goal-30/svnd-version.txt) | line 7 the candidate sha, line 8 `vcs.modified false`, line 22 `EXIT:0` |
| `TestMilestone` without the venv | [milestone.txt](logs/goal-30/milestone.txt) | line 66 `ModuleNotFoundError: No module named 'jsonschema'`, line 71 `EXIT:1` |
| `TestMilestone` with `SVN_MILESTONE_PATH` | [milestone-venv.txt](logs/goal-30/milestone-venv.txt) | line 99 `phase=done`, line 100 `PASS`, line 103 `EXIT:0` |
| second client against `svn-discovery` | [discovery.txt](logs/goal-30/discovery.txt) | twelve HTTP statuses, all 404, line 23 `CLIENT ok`, line 24 `EXIT:0` |
| capacity measure | [capacity.txt](logs/goal-30/capacity.txt) and [capacity-measure.json](logs/goal-30/capacity-measure.json) | command `EXIT:0`. Gates inside the JSON are not all passes. |
| container proof | [containers.txt](logs/goal-30/containers.txt) | line 12 `docker: command not found`, line 13 `EXIT:127` |
| absent tools | [absent.txt](logs/goal-30/absent.txt) | docker exit 127, golangci-lint exit 127, `tc` exit 0 |
| `chain/devnet` on this VM | [devnet.txt](logs/goal-30/devnet.txt) and [devnet-names.txt](logs/goal-30/devnet-names.txt) | line 7 `RESULT passed=84 failed=0 skipped=0 packages=1 nonjson=0`, line 9 `EXIT:0` |

The Go JSON that `go-unit.txt` summarizes is 227722620 bytes and is not in the repository. Re-running `scripts/acceptance/go_summary.py` on that file printed the same `RESULT` line. The devnet JSON is not committed. [devnet-names.txt](logs/goal-30/devnet-names.txt) line 5 is `pass_events 84 fail_events 0 skip_events 0 parents 32`, which matches the summary.

No flake was observed. The two failures in the table have the causes below. They were not rerun until the invocation or the fixture changed.

## Requirement audit

[requirements.txt](logs/goal-30/requirements.txt) lines 17-22: `requirements 265`, `in_scope 250`, `with_test 216`, `with_blocker 34`, `deferred 9`, `non_goal 6`. Line 27 is `self_test_cases 12`. Line 28 is `self_test_problems 0`. Line 30 is `errors 0`. `250 + 9 + 6 = 265`. `216 + 34 = 250`.

[matrix-scopes.txt](logs/goal-30/matrix-scopes.txt) lines 6-8: `DEFERRED 9`, `IN_SCOPE 250`, `NON_GOAL 6`. Line 12 is `other 0`. The ten goal 30 ids, lines 15-24, are `REQ-PROD-001`, `REQ-PROD-011`, `REQ-UX-015`, `REQ-UX-020`, `REQ-UX-021`, `REQ-DEV-002`, `REQ-MOD-006`, `REQ-QA-011`, `REQ-LAUNCH-001`, and `REQ-SUCCESS-001`.

`out=15` in the audit log is the nine deferred rows plus the six non-goal rows. A count is not a pass. The checker does not approve the protocol. Its own line after `result PASS` says the document check is not an approval and public launch is disabled.

Every in-scope row is `TEST:` or `BLOCKER:`. The goal 30 rows are not thereby closed. Their state on this candidate:

| Row | State |
| --- | --- |
| REQ-PROD-001 | The viewer, the operator software, and the chain are in the tree. One VM is one deployment. |
| REQ-PROD-011 | Blocked. DR-30-01 and DR-30-07. |
| REQ-UX-015 | The second client uses the published documents and prints `activitypub=false atproto=false ipfs=false filecoin=false`. |
| REQ-UX-020 | The second client calls the published paths. The discovery process returned 404 for all twelve. DR-30-11. |
| REQ-UX-021 | Blocked. DR-30-01. |
| REQ-DEV-002 | Blocked. DR-30-01. |
| REQ-MOD-006 | Blocked on DEC-007. DR-30-03. |
| REQ-QA-011 | Blocked. The installer was not given to a second operator. |
| REQ-LAUNCH-001 | `docs/release-readiness.md` lists every public-checklist item as blocked. |
| REQ-SUCCESS-001 | The local multi-process log is one machine. Separate hosts stay blocked. |

## Success demonstration

[milestone-venv.txt](logs/goal-30/milestone-venv.txt) is `TestMilestone` on this VM with `SVN_MILESTONE_PATH` set to the virtualenv, which is how `services/milestone/scenario_phases_test.go` line 816 puts Python on the child `PATH`. Line 8 is `physical_count=1 independent=false`. Line 99 is `phase=done prototype=true public_beta=false single_machine=true`. Line 100 is `PASS` in 59.17s. Line 103 is `EXIT:0`. Line 104 is `AFTER clean`.

This is several processes on one machine. It is one failure domain. DR-30-07 stays open.

| Step | Log line |
| --- | --- |
| Synthetic clip, 12746 bytes | line 47 `synthetic=true` |
| Upload reserved, then the same upload id again | line 48 |
| Bytes received | line 49 `status=204` |
| Processing queued | line 50 |
| `POST /publish` blocked | line 51 `status=403` |
| Worker ready, publication `NOT_PUBLISHED` | line 52 |
| Package, two segments | line 53 |
| Playback grant | line 54 |
| Chain commit of `REGISTER_CONTENT` | line 55 `height=60` |
| Published, three leases, `physical=1 simulated=true independent=false` | line 66 |
| Milestone client watches | lines 69-76. Line 70 `status=200`. Line 74 the segment sha256 matches. Line 76 `CLIENT ok` |
| Origin marked down, distribution still returns the bytes | line 81 `ORIGIN_DOWN` `bytes=12740` |
| Provider p1 stopped | line 82 `PROVIDER_DOWN` `degraded=[p1]` |
| Repair confirmed, source p2, replacement p4 | line 85 `state":"CONFIRMED"` |
| Provider p2 restarted | line 87 `health=200` |
| Quorum loss, height stays 92, playback 200 | line 92 |
| New commitment stays pending during the stall | line 93 |
| Quorum resumes at height 93 | line 94 |
| Withdraw committed | line 95 `WITHDRAW_CONTENT` |
| Indexer withdrawn | line 96 |
| Distribution 410 | line 97 |
| Delivery copy removed | line 98 `REMOVED` |

The first `TestMilestone` invocation, [milestone.txt](logs/goal-30/milestone.txt), did not set `SVN_MILESTONE_PATH`. The child Python was the distro interpreter. Line 66 is `ModuleNotFoundError: No module named 'jsonschema'`. Line 71 is `EXIT:1`. That run had already reserved the upload and blocked publish (lines 47-51). It is not a pass and not a flake. The venv run is the demonstration.

The Go package run with `SVN_SKIP_DEVNET=1` skipped `TestMilestone` ([go-unit.txt](logs/goal-30/go-unit.txt) line 148). That skip is not a pass. The fifteen skips, lines 134-148, are the multi-process devnet tests plus `TestMilestone`.

[devnet.txt](logs/goal-30/devnet.txt) is `go test -count=1 -timeout 45m -json ./devnet/` on this VM, without the skip and without `-race`. Line 7 is `passed=84 failed=0 skipped=0`. Line 8 is `ok` for `chain/devnet`. The 32 parent names are [devnet-names.txt](logs/goal-30/devnet-names.txt) lines 6-37, including `TestDevnetAuditLifecycle`, `TestDevnetBroadcasterLifecycle`, `TestDevnetBroadcasterQuorumStall`, `TestDevnetIndexer`, `TestDevnetUpgradeRehearsal`, `TestDevnetMinorityWrongMigration`, and `TestDevnetGovernedChangeTiming`. Four validators here are four processes. They are not four operators.

`services/restore` passed inside the Go package run ([go-unit.txt](logs/goal-30/go-unit.txt) line 127 `ok` for `services/restore/internal/restore`, line 126 for `cmd/svn-restore`). That is the package test. It is not a rehearsed chain-state rollback and not a measured service RTO or RPO. DR-27-06 and DR-26-01 through DR-26-03 stay open. No new number from an earlier goal is reused as this candidate's recovery time.

`chain/app` passed in the same Go run (line 71). That package contains the in-process upgrade rehearsal. The devnet names above include the multi-process upgrade, minority, and governed-change tests.

## Second client

`scripts/acceptance/second_client.py` imports only the Python standard library. [self-check.txt](logs/goal-30/self-check.txt) line 5 is `source_problems=0`. Line 6 is 12 operations, the published client and provider documents. Line 7 is `private_shortcut=false`. Line 8 refuses ActivityPub, AT Protocol, IPFS, and Filecoin. The client does not follow a redirect.

[discovery.txt](logs/goal-30/discovery.txt) starts `svn-discovery` from this tree and sends those twelve calls. Lines 9-20 are status 404 for every published path, including `GET /feeds`, `POST /reports`, and `GET /exports`. The client did not call `/v1` or `/admin`. A 404 is an HTTP response. It is not evidence that the route is implemented.

The handlers that exist are elsewhere:

- `services/api/internal/service/http.go` line 30 is `POST /upload-intents`. Line 33 is `GET /uploads/{uploadID}`. Line 35 is `POST /publish`. Line 36 is `POST /removal`.
- `services/api/internal/social/http.go` line 24 is `GET /v1/feeds/{kind}`. Line 51 is `POST /v1/reports`.
- `services/api/internal/account/http.go` line 37 is `POST /v1/account/export`.
- `docs/protocol.md` line 288 lists the published client routes, including `GET /feeds`, `POST /reports`, and `GET /exports`.
- `agents/provider/internal/httpserver/server.go` line 28 is `GET /capability`. Line 34 is `POST /admin/drain`. The second client does not call `/admin`.

DR-30-11 stays open. Goal 30 does not change either the published document or the handlers.

`svn-feed-client` links the discovery package. It is not this second client. The milestone watcher is `scripts/milestone/client.py`. It is a different program, and its watch in the log above is on this same VM.

## Packaging rehearsal

[packaging.txt](logs/goal-30/packaging.txt) line 4 is `DIRTY no`. Line 5 is `NOT-RELEASED candidate 2bce2e272534316115c2550329c7ed730983da70 tree 925512e799ec3ac42079b2e76bb9a9acd2bc56c7`. Line 6 is `EXIT:0`.

[build-compare.txt](logs/goal-30/build-compare.txt) line 17 is `binaries 12 mismatch 0`. Line 19 is the candidate commit. Line 26 is `match yes`. Lines 28-32 are the bundle label: `NOT-RELEASED`, `licence UNCONFIRMED`, `public launch disabled`, `goals 28-30 and the human launch gates have not passed`, `this tree does not publish a release`.

[svnd-version.txt](logs/goal-30/svnd-version.txt) line 6 is `go go1.26.10`. Line 7 is `vcs.revision 2bce2e272534316115c2550329c7ed730983da70`. Line 8 is `vcs.modified false`. Line 12 is `app_version 1`. Line 21 is `upgrades none`.

There is no `LICENSE` file. DEC-006 is not applied. There is no `CODEOWNERS` file. The test signature produced by the packaging flow is a rehearsal key and is not an owner signature. DR-27-05 stays open. Nothing was tagged, published, or installed on another machine.

The pytest run wrote four untracked files under `apps/web/dist` ([pytest.txt](logs/goal-30/pytest.txt) lines 17-20). Those files were removed before the packaging command. The packaging log's `DIRTY` line is `no`.

## Capacity

`scripts/capacity.py measure` exited 0 ([capacity.txt](logs/goal-30/capacity.txt) line 5). Exit 0 means the command finished. It does not mean the design point passed. From [capacity-measure.json](logs/goal-30/capacity-measure.json), on this candidate:

- `design_point.gate` is `BLOCKED`. `uploads_started` is 1. `viewers_started` is 0. The reason is that 100 viewers and 10 uploads were not started.
- `processing.gate` is `PASS`. `elapsed_seconds` is 3.339. `limit_seconds` is 120.0. The input is a synthetic 60-second test pattern. `publication` is `NOT_PUBLISHED`. `worker_concurrency` in the hardware block is 1.
- `playback.gate` is `BLOCKED`. The detail is `Specified qdisc kind is unknown.`
- `processing.moderation.gate`, `publication_phase.gate`, and `replication.gate` are `BLOCKED`.
- `scheduler.multi_host.gate` is `BLOCKED`. The repair block says `machine` `1` and `simulated` `true`.
- `chain.gate` is `RECORDED` for an in-process ABCI fixture. `chain.index_lag.gate` is `BLOCKED`. The tool's reason text says PostgreSQL was absent and the indexer was not started. The milestone and devnet logs on this same VM did start PostgreSQL 18.6. That does not convert the capacity index lag into a pass.
- Hardware in the JSON: 4 CPUs, `mem_total_kib` 16387328, Chrome `157.0.8103.50`, `tc` recorded as `absent` by that tool.

DR-30-10 stays open. The page 26 design point was not met. DR-28-01 through DR-28-11 stay open.

## Blocked on this VM

- Container devnet. [containers.txt](logs/goal-30/containers.txt) line 12 is `docker: command not found` and line 13 is `EXIT:127`. DR-30-09. The GitHub `devnet` job in run 44946962593 passed on that runner. It is not this rehearsal.
- golangci-lint. [absent.txt](logs/goal-30/absent.txt) line 9 is `golangci-lint exit 127`.
- PostgreSQL 16. The server under test is 18.6.
- Shaped playback. The qdisc kind the measure asks for is unknown. DR-30-10.
- A separate operator and a separate host. DR-30-01 and DR-30-07.
- Owner sign-off. DR-30-02 through DR-30-06. The name and date cells in `docs/release-readiness.md` are blank. No name was invented.

## Fixture fix on the candidate

`workers/media/svn_media/queue.py` line 66 selects `j.idempotency_key`. `services/api/internal/service/store.go` lines 65-68 define `api.processing_jobs` with `idempotency_key text NOT NULL UNIQUE`. The fixture in `e7591f4` created that table without the column. [pytest-e7591f4.txt](logs/goal-30/pytest-e7591f4.txt) line 1 is commit `e7591f4`. The failure is `test_postgres_claim_is_once_and_does_not_publish`, and the summary is `1 failed, 471 passed, 5 skipped`. The candidate's fixture, `tests/unit/test_media_postgres.py` lines 38 and 150, includes the column and inserts `job-jdb`. [pytest.txt](logs/goal-30/pytest.txt) line 14 is `476 passed, 1 skipped`. The remaining skip, line 13, is `set SVN_MEDIA_TIMING=1 to measure`. That skip is not a timing pass.

The four browser skips from the earlier pytest ran on the candidate because `node_modules` was already present from `npm ci` of the same lockfile. [node.txt](logs/goal-30/node.txt) then ran `npm ci` on this commit and exited 0.

## Sign-off placeholders

No name was supplied. Blank is the recorded state.

| Gate | Name | Date | Decision | Record |
| --- | --- | --- | --- | --- |
| Moderation coverage |  |  | not given | DR-30-02 |
| Privacy and rights policy |  |  | not given | DR-30-03 |
| Funding and quotas |  |  | not given | DR-30-04 |
| Operator participation |  |  | not given | DR-30-05 |
| Security review |  |  | not given | DR-30-06 |

## DR-30

All eleven are OPEN in `docs/decision-register.md`. DR-30-01 starts at line 749. DR-30-11's status line is line 945.

| ID | State |
| --- | --- |
| DR-30-01 | OPEN. The install is an agent rehearsal on this VM. Independence is unverified. |
| DR-30-02 | OPEN. Moderation coverage sign-off is blank. |
| DR-30-03 | OPEN. Privacy and rights sign-off is blank. DEC-004, DEC-006, DEC-007, and REQ-MOD-006 stay unresolved as a signed policy. |
| DR-30-04 | OPEN. Funding and quotas sign-off is blank. DEC-005 stays the allowance decision. |
| DR-30-05 | OPEN. Operator participation sign-off is blank. DEC-002 names no operator. |
| DR-30-06 | OPEN. Security review sign-off is blank. This file does not replace DR-29-01. |
| DR-30-07 | OPEN. Separate-host success demonstration. The milestone and devnet logs are one machine. |
| DR-30-08 | OPEN. F-29-05 stays open. Grant ids remain process memory (`docs/evidence/goal-29.md` line 114). |
| DR-30-09 | OPEN. Container reproduction on this VM. Docker CLI absent. Port 2375 was not used. |
| DR-30-10 | OPEN. Page 26 design point. The local measure is not 100 viewers and not 10 uploads. |
| DR-30-11 | OPEN. Published `GET /feeds`, `POST /reports`, and `GET /exports` are not the registered handler paths. |

## Inherited blockers

These stay open. This goal does not mark them complete.

- DR-25-01 through DR-25-12
- DR-26-01 through DR-26-12
- DR-27-01 through DR-27-13
- DR-28-01 through DR-28-11
- DR-29-01 through DR-29-06
- F-29-05

Goals 04 and 07 through 29 stay BLOCKED in `docs/progress.md`, except goal 06, which is VERIFIED on an earlier revision, and goals 02 and 03, which stay IMPLEMENTED. Goal 30 is BLOCKED.

## Governance, recovery, and the first community

`docs/ownership.md` line 3 names five functions and no person: product/application, chain/security, media/provider agents, deployment/on-call, and trust/safety. No person is appointed by this goal. The GitHub account that owns the repository is not recorded as a moderator, a validator operator, or a budget owner.

Recovery constraints and the unlaunched first-community plan are in `docs/release-readiness.md`. Nothing was launched. No DNS name was published. No account was invited outside this VM. The processing observation on this candidate is the 3.339 second synthetic encode under a 120 second procedure limit (`docs/operations/load-plan.md` line 14). That observation bounds a plan. It does not authorize a community.

`scripts/packaging.py` lines 825 and 828 still write `NOT-RELEASED` and the sentence that goals 28-30 and the human launch gates have not passed. That string was not changed.
