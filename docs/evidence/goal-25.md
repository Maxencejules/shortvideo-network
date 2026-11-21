# Goal 25 evidence

Status: BLOCKED

`svn-observe` is a loopback control plane for correlation ids, health and readiness, redacted logs, placeholder admission, and a cost worksheet. Thresholds in that process are placeholders. They are not a DEC-005 allowance and they are not a spend cap. No budget owner, rate card, currency approval, or on-call assignment is named. Public launch is disabled. There is no token. This is not an audit and not a production network.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/25-observability-quotas-and-usage-controls.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-4bd2d180-c9c7-5cf0-bd30-f459e9bea700`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run. The playbook's GPT-6.1 Sol was replaced by this Cursor cloud agent on that default model.
- On 2026-10-31 (Toronto) the owner said "Don't mind the ai model requirement". On 2026-11-17 the owner confirmed that it applies to all goals. A model mismatch with the playbook is not a blocker. The evidence still records the model that actually ran.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `c67f1bd1aff7c103e130773414bb513cb68fd6e7` |
| Tree | `064ba76f4fb679a20199eb8229801f8a0f3483e5` |
| Base | `5cda9d4fa432bf4a11036fcca6ace83efa62f839` |
| Branch | `goal-25` |
| Pull request | https://github.com/Maxencejules/shortvideo-network/pull/30 |

The tested commit message carries `Tested-tree: 064ba76f4fb679a20199eb8229801f8a0f3483e5`. `git rev-parse c67f1bd1aff7c103e130773414bb513cb68fd6e7^{tree}` is that tree. Later commits change only `docs/evidence/`.

## Captures

Every log under [logs/goal-25/](logs/goal-25/) was captured while `git status --porcelain` was empty. Each file's line 4 is `DIRTY no`. The commands ran from the repository root. The log files were written outside the worktree and copied in afterward, so they do not appear in those stamps. Every log ends `AFTER clean`. Nothing was left untracked when the captures finished.

## Head CI

[Run 44878911551](https://github.com/Maxencejules/shortvideo-network/actions/runs/44878911551) on `c67f1bd1aff7c103e130773414bb513cb68fd6e7` finished with conclusion `success`. All 17 jobs passed. That head is the tested commit. This file is added after that run. A later head that only changes `docs/evidence/` is not that run.

## What ran

| Command | Scope | Log | Result |
| --- | --- | --- | --- |
| runtime versions | repository root | [runtime.txt](logs/goal-25/runtime.txt) | line 11 `EXIT:0` |
| `gofmt -l` | the Go files this goal added | [gofmt.txt](logs/goal-25/gofmt.txt) | line 5 `EXIT:0` |
| `go test -count=1 -timeout 180s -v` | `services/observe` | [observe-test.txt](logs/goal-25/observe-test.txt) | line 54 `EXIT:0` |
| `go test -race` | `services/observe` | [observe-race.txt](logs/goal-25/observe-race.txt) | line 7 `EXIT:0` |
| `go test -count=1` | `services/api/internal/social` | [social-test.txt](logs/goal-25/social-test.txt) | line 6 `EXIT:0` |
| `go test -count=1` | `services/scheduler` | [scheduler-test.txt](logs/goal-25/scheduler-test.txt) | line 11 `EXIT:0` |
| `go test -run TestCorrelationHeader` | `services/indexer/internal/api` | [indexer-test.txt](logs/goal-25/indexer-test.txt) | line 6 `EXIT:0` |
| `go vet` | observe, social, the upload service package, the indexer read API, scheduler, the broadcaster API package, and the provider HTTP package | [govet.txt](logs/goal-25/govet.txt) | line 5 `EXIT:0` |
| pytest | correlation, the CI wiring check, and the operations docs | [pytest.txt](logs/goal-25/pytest.txt) | line 6 `EXIT:0` |
| `ruff check` and `ruff format --check` | delivery, the media worker, and the new Python tests | [ruff.txt](logs/goal-25/ruff.txt) | line 7 `EXIT:0` |
| `python3 -m mypy` | the six files named in the command, not the whole repository | [mypy.txt](logs/goal-25/mypy.txt) | line 6 `EXIT:0` |
| `python3 scripts/scan_secrets.py` | tracked files | [secrets.txt](logs/goal-25/secrets.txt) | line 5 `secret scan ok`, line 6 `EXIT:0` |
| `python3 scripts/workflow_policy.py` | the workflow file | [workflow.txt](logs/goal-25/workflow.txt) | line 5 `workflow policy ok files 1`, line 6 `EXIT:0` |
| `python3 scripts/write_sbom.py --check` | lockfiles | [sbom.txt](logs/goal-25/sbom.txt) | line 5 `sbom ok components 1389`, line 6 `EXIT:0` |
| `python3 docs/evidence/goal-01/check_requirements.py` | requirements checker | [requirements.txt](logs/goal-25/requirements.txt) | line 64 `result PASS`, line 66 `EXIT:0` |
| PostgreSQL binaries | `/usr/lib/postgresql/16` and `/usr/lib/postgresql/18` | [pg16.txt](logs/goal-25/pg16.txt) | line 5 `pg16 absent`, line 6 `pg18 absent`, line 7 `EXIT:1` |
| `docker version` | host | [docker.txt](logs/goal-25/docker.txt) | line 5 `bash: line 1: docker: command not found`, line 6 `EXIT:127` |
| `golangci-lint version` | host | [golangci.txt](logs/goal-25/golangci.txt) | line 5 `bash: line 1: golangci-lint: command not found`, line 6 `EXIT:127` |

[runtime.txt](logs/goal-25/runtime.txt) line 5 is `v22.23.4`. Line 6 is `10.9.9`. Line 7 is `Python 3.12.3`. Line 8 begins with `go version go1.26.10`. Line 9 begins with `Google Chrome 157.0.8103.50`. Line 10 begins with `Linux 6.12.113+`.

[pytest.txt](logs/goal-25/pytest.txt) line 5 is 34 dots and `[100%]`. That is the quiet reporter. It does not print a `passed` count. Line 6 is `EXIT:0`.

[ruff.txt](logs/goal-25/ruff.txt) line 5 contains `All checks passed`. Line 6 is `24 files already formatted`.

[mypy.txt](logs/goal-25/mypy.txt) line 5 is `Success: no issues found in 6 source files`.

[observe-race.txt](logs/goal-25/observe-race.txt) line 5 and line 6 are the two `ok` lines. The observe module adds no third-party Go module and no `go.sum`. [sbom.txt](logs/goal-25/sbom.txt) line 5 stayed `sbom ok components 1389`.

[scheduler-test.txt](logs/goal-25/scheduler-test.txt) line 6 is the scheduler command tests, line 7 is policy, and line 9 is the scheduler package that now sets the correlation header.

The upload service tests, the broadcaster tests, the provider tests, and the indexer tests other than `TestCorrelationHeader` were not run here. PostgreSQL is absent ([pg16.txt](logs/goal-25/pg16.txt) line 5). `go vet` of those packages is [govet.txt](logs/goal-25/govet.txt) line 5, which is not those test suites.

## Fault injection, quotas, and counters

These readings are the in-process fixture on this commit. They are not a measurement of this VM's traffic and they are not a budget.

[observe-test.txt](logs/goal-25/observe-test.txt):

| Check | Line | Observation |
| --- | --- | --- |
| Seven components, one correlation id, no chain submission | 10 | `TRACE components=7 chain_submissions=0` |
| Readiness fails closed, including a database that is down | 13 | `READY fresh=false database_down=true` |
| Slow processing, low disk, stale commit, distinct quorum, repair with no delivered bytes, recovery | 16 | `SLOW intake_stopped=true DISK low_disk=true STALE diagnosis=stale_commit QUORUM distinct=true REPAIR delivered_repair=0 RECOVERY storage_granted=true` |
| Upload race at the placeholder cap | 19 | `RACE granted=3 denied=37 limit=3` |
| Output-byte race | 20 | `BYTES granted=1 denied=7 used=600` |
| Replay does not consume again; a rate window does not reset storage | 23 | `REPLAY used=0 LOGIN after_window=true STORAGE clock_reset=false` |
| Processing queue then one release | 26 | `QUEUE running=2 queued=1 denied=1` |
| An alert does not admit extra work and is not a spend cap | 29 | `ALERT spend_cap=false admitted=true` |
| Canary and address redaction | 32 | `REDACT canary=false email=false` |
| Fixture classes are not collapsed | 35 | `FIXTURE user_payload=1000 delivered_total=9700 storage_user=1000 double_count=false` |
| Unmeasured classes | 36 | `UNMEASURED protocol_overhead,turn_relay,tls_records,chain_gossip,interface_bandwidth,scheduler_job_queue` |
| No monthly bill | 39 | `WORKSHEET monthly_bill=absent period_product=46400 currency_approved=false` |
| Private fetch targets | 42 | `FETCH private=refused public=allowed redirect_private=refused` |
| Playback aggregate drops identity | 45 | `PLAYBACK identity_dropped=true` |
| Metrics and logs require the token | 48 | `ACCESS metrics=401 logs=401 health=200` |

The period product on line 39 is `46400` minor units from test-supplied rates of 2 and 3 applied to the fixture bytes. It is labelled `not_a_monthly_forecast` in `services/observe/internal/obs/cost.go:140`. The monthly bill field stays nil (`services/observe/internal/obs/cost.go:115`). Currency stays unapproved. CAD is the proposed preference at `services/observe/internal/obs/obs.go:27`. The budget owner constant is `services/observe/internal/obs/obs.go:24`.

Placeholder caps are the constants at `services/observe/internal/obs/obs.go:22` (`PLACEHOLDER`). `services/observe/internal/obs/obs.go:469` sets `SpendCap: false` on every decision.

A media observation on the chain component is refused at `services/observe/internal/obs/observe.go:49`. Chain usability cannot be flipped on without a commit reading (`services/observe/internal/obs/obs.go:324`).

Health is public (`services/observe/internal/obs/http.go:15`). Metrics require the token (`services/observe/internal/obs/http.go:20`). A token in the query is refused (`services/observe/internal/obs/http.go:49`).

## Instrumentation outside the control plane

Correlation ids are response headers. They are not account ids and they are not chain fields.

| Process | Where |
| --- | --- |
| Upload API | `services/api/internal/service/http.go:40` |
| Discovery, search, and reports | `services/api/internal/social/http.go:70` |
| Indexer read API | `services/indexer/internal/api/api.go:41` |
| Broadcaster caller API | `services/broadcaster/internal/api/server.go:227` |
| Scheduler | `services/scheduler/internal/sched/http.go:67` |
| Provider agent | `agents/provider/internal/httpserver/server.go:41` |
| Playback | `services/delivery/svn_delivery/server.py:369` |
| Media jobs | `workers/media/svn_media/queue.py:282` |

The job file written at `workers/media/svn_media/queue.py:285` does not include the correlation id. The id stays on the worker object.

The chain process is not modified. Commit age and quorum are observations stored by `svn-observe`. They are not transactions. Watch events are not stored and are not written to the chain. `services/observe/internal/obs/observe.go:323` sets `WatchEventsOnChain` to 0.

Interface bandwidth and the scheduler job queue stay unmeasured. That is DR-24-07, carried as DR-25-07. The list is `services/observe/internal/obs/obs.go:81`.

## Runbooks and worksheet

[observability.md](../operations/observability.md) maps each alert to a runbook id and an unassigned role. [cost-worksheet.md](../operations/cost-worksheet.md) separates measured units, operator rates, founder cash, and volunteer notes. Both open with the development-procedure line. No person is assigned.

## Acceptance

| Check | Status | Where |
| --- | --- | --- |
| Correlation ids across API, jobs, broadcaster, indexer, chain observations, providers, and playback | VERIFIED for the control plane and the response headers | [observe-test.txt](logs/goal-25/observe-test.txt) line 10. Header wrappers are the table above. |
| Health and readiness fail closed | VERIFIED | [observe-test.txt](logs/goal-25/observe-test.txt) line 13. `services/observe/internal/obs/obs.go:334` is `Ready`. |
| Redaction of a synthetic canary and access control on metrics and logs | VERIFIED | [observe-test.txt](logs/goal-25/observe-test.txt) lines 32 and 48. |
| Quota race and replay | VERIFIED | [observe-test.txt](logs/goal-25/observe-test.txt) lines 19, 20, and 23. |
| Slow processing, low disk, stale commit, failed repair, recovery | VERIFIED in process | [observe-test.txt](logs/goal-25/observe-test.txt) line 16. |
| Byte classes against the fixture, without double counting | VERIFIED | [observe-test.txt](logs/goal-25/observe-test.txt) lines 35 and 36. The numbers are the fixture at `services/observe/internal/obs/observe.go:270`, not this machine's traffic. |
| Fetch policy rejects private targets | VERIFIED | [observe-test.txt](logs/goal-25/observe-test.txt) line 42. `services/observe/internal/obs/fetch.go:25` does not dial. Names are refused unless a resolver is injected. |
| Cost worksheet has no monthly bill | VERIFIED | [observe-test.txt](logs/goal-25/observe-test.txt) line 39. |
| Alerts are not a spend cap | VERIFIED | [observe-test.txt](logs/goal-25/observe-test.txt) line 29. |
| Playback aggregate has no account and no chain watch event | VERIFIED | [observe-test.txt](logs/goal-25/observe-test.txt) line 45. |
| Social package still passes with the header wrapper | VERIFIED | [social-test.txt](logs/goal-25/social-test.txt) line 5. |
| Scheduler package still passes with the header wrapper | VERIFIED | [scheduler-test.txt](logs/goal-25/scheduler-test.txt) line 9. |
| Upload service, broadcaster, provider, and indexer suites | BLOCKED here | PostgreSQL is absent. Those suites were not run. CI on the pull request is the place they run. |
| Live four-validator commit age | BLOCKED | No devnet was started for this goal. The stale-commit detector used an injected reading. |
| Budget, rates, currency, and on-call | BLOCKED | DR-25-01 through DR-25-05. |
| golangci-lint | BLOCKED | [golangci.txt](logs/goal-25/golangci.txt) line 6 `EXIT:127`. |
| Compose | BLOCKED | [docker.txt](logs/goal-25/docker.txt) line 6 `EXIT:127`. The daemon on port 2375 was not used. |
| PostgreSQL 16 | BLOCKED | [pg16.txt](logs/goal-25/pg16.txt) line 5. |

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-25-01 | OPEN, BLOCKED | Budget owner is unset. DEC-005. `services/observe/internal/obs/cost.go:150` is `UNASSIGNED`. |
| DR-25-02 | OPEN, BLOCKED | Allowance numbers are unset. The caps are placeholders, not a DEC-005 allowance. DR-24-03 stays open. |
| DR-25-03 | OPEN, BLOCKED | Operator rates and an approved currency are unset. CAD is a proposed preference only. |
| DR-25-04 | OPEN, BLOCKED | On-call assignment is unset. Runbook roles are placeholders. |
| DR-25-05 | OPEN, BLOCKED | Trust-and-safety coverage is unset. |
| DR-25-06 | OPEN | DR-24-03 stands. A limit in this process is not a chain allowance. |
| DR-25-07 | OPEN | DR-24-07 stands. Interface bandwidth and the scheduler job queue are unmeasured. |
| DR-25-08 | OPEN, BLOCKED | No production metrics store. The log buffer is in memory and bounded. PostgreSQL 16 is absent. |
| DR-25-09 | OPEN | No paid monitoring service and no cloud spend in this goal. |
| DR-25-10 | OPEN, BLOCKED | Founder cash and volunteer contributions are unset inputs. The worksheet does not invent them. |
| DR-25-11 | OPEN | Chain observations stay off-chain. The chain binary was not changed. A live node was not scraped. |
| DR-25-12 | OPEN | The fetch checker has no recursive resolver. Hostnames are refused. IP literals are checked. |

The ids start at `services/observe/internal/obs/cost.go:62`.

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody stays unimplemented.
2. **DEC-005.** No sponsor, no allowance, no lease term.
3. **One machine.** These processes are one VM.
4. **Goal 06 and goal 07 mutation runners.** Not re-run.
5. **golangci-lint.** Not installed.
6. **PostgreSQL 16.** Not installed. PostgreSQL 18 is not installed either.
7. **Goal 04 compose bridge.** Docker is not installed. Compose was not run.

## Requirements

The requirements matrix was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-OPS-003 | The control plane records commit age, quorum loss, index lag, replicas, processing age, playback failures, and unreviewed reports. Quorum loss and index lag are different diagnosis names. The live devnet reading was not taken. |
| REQ-CAP-003 | The worksheet separates measured units from rates and leaves the monthly bill empty. CAD is not approved. |
| REQ-SEC-010 | Login, search, upload, report, and discovery have placeholder rate caps. The fetch checker rejects private addresses. The upload service's own rate limits were not re-run here. |
| REQ-UX-019 | Cost counters are the worksheet and the usage report. A service-key rotation is goal 26. |
| REQ-RISK-005 | BLOCKED on DEC-005. The mechanism stops at a placeholder cap. The allowance number is unset. |
| REQ-SCOPE-013 | BLOCKED on DEC-005. |

## Next safe action

Do not treat a placeholder cap or an alert as a budget. Do not forecast a monthly bill. Name a budget owner before any allowance is enforced as policy. Public launch stays disabled.
