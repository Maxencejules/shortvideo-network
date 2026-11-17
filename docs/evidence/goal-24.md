# Goal 24 evidence

Status: BLOCKED

`svn-operator` is a loopback operator dashboard. The public listener is read-only. The admin listener is separate. The page does not accept a consensus private key, and the process does not submit a council message. Storage and delivery can be chosen without validator. Disk figures below are statfs observations of this VM. They are not a minimum for every machine. Two provider processes on this VM are one machine. The manual independent-operator walkthrough did not happen. Public launch is disabled. There is no token. This is not an audit and not a production network.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/24-operator-dashboard-and-node-onboarding.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-3bba5751-77e6-5d76-afbd-6eff59a46395`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run. The playbook's GPT-6.1 Sol was replaced by this Cursor cloud agent on that default model.
- On 2026-10-31 (Toronto) the owner said "Don't mind the ai model requirement", so a model/effort mismatch with the playbook is not a blocker. The evidence still records the model that actually ran.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `903d7022161013b15994c69e0e03e652c3646ba8` |
| Tree | `ecce301f1aa081d96711dcfae26052a6a53a21a8` |
| Base | `536f1f12581baa58b9c01adfa373feeddcd1ae49` |
| Branch | `goal-24` |

The tested commit message carries `Tested-tree: ecce301f1aa081d96711dcfae26052a6a53a21a8`. `git rev-parse 903d7022161013b15994c69e0e03e652c3646ba8^{tree}` is that tree. Later commits change only `docs/evidence/`.

## Captures

Every log under [logs/goal-24/](logs/goal-24/) was captured while `git status --porcelain` was empty. Each file's line 4 is `DIRTY no`. The commands ran from the repository root. The log files were written outside the worktree and copied in afterward, so they do not appear in those stamps. [pytest.txt](logs/goal-24/pytest.txt) line 7 records that `apps/web/dist` was removed after that command. It was not committed. Every log ends `AFTER clean`. Nothing was left untracked when the captures finished.

## What ran

| Command | Scope | Log | Result |
| --- | --- | --- | --- |
| runtime versions | repository root | [runtime.txt](logs/goal-24/runtime.txt) | line 11 `EXIT:0` |
| `gofmt -l` | the Go files this goal edited | [gofmt.txt](logs/goal-24/gofmt.txt) | line 5 `EXIT:0` |
| `go test -count=1 -timeout 120s -v` | `services/operator` | [operator-test.txt](logs/goal-24/operator-test.txt) | line 15 `EXIT:0` |
| `go test -race` | `services/operator` | [operator-race.txt](logs/goal-24/operator-race.txt) | line 7 `EXIT:0` |
| `go test -count=1 -timeout 180s -v` | `agents/provider` | [provider-test.txt](logs/goal-24/provider-test.txt) | line 66 `EXIT:0` |
| `go vet` | `services/operator` and `agents/provider` | [govet.txt](logs/goal-24/govet.txt) | line 5 `EXIT:0` |
| `python3 scripts/run_tests.py --only node` | `apps/web` test files | [node.txt](logs/goal-24/node.txt) | line 421 `EXIT:0` |
| `npm run typecheck --workspace apps/web` | `apps/web` | [typecheck.txt](logs/goal-24/typecheck.txt) | line 9 `EXIT:0` |
| pytest browser, CI wiring, operations docs | those three files | [pytest.txt](logs/goal-24/pytest.txt) | line 8 `EXIT:0` |
| `ruff check` and `ruff format --check` | the new Python tests | [ruff.txt](logs/goal-24/ruff.txt) | line 7 `EXIT:0` |
| `python3 -m mypy` | the new Python tests | [mypy.txt](logs/goal-24/mypy.txt) | line 6 `EXIT:0` |
| `python3 scripts/scan_secrets.py` | tracked files | [secrets.txt](logs/goal-24/secrets.txt) | line 5 `secret scan ok`, line 6 `EXIT:0` |
| `python3 scripts/workflow_policy.py` | the workflow file | [workflow.txt](logs/goal-24/workflow.txt) | line 5 `workflow policy ok files 1`, line 6 `EXIT:0` |
| `python3 scripts/write_sbom.py --check` | lockfiles | [sbom.txt](logs/goal-24/sbom.txt) | line 5 `sbom ok components 1389`, line 6 `EXIT:0` |
| PostgreSQL binaries | `/usr/lib/postgresql/16` and `/usr/lib/postgresql/18` | [pg16.txt](logs/goal-24/pg16.txt) | line 5 `pg16 absent`, line 6 `pg18 absent`, line 7 `EXIT:1` |
| `docker version` | host | [docker.txt](logs/goal-24/docker.txt) | line 5 `docker: command not found`, line 6 `EXIT:127` |
| `golangci-lint version` | host | [golangci.txt](logs/goal-24/golangci.txt) | line 5 `golangci-lint: command not found`, line 6 `EXIT:127` |

[runtime.txt](logs/goal-24/runtime.txt) line 5 is `v22.23.4`. Line 6 is `10.9.9`. Line 7 is `Python 3.12.3`. Line 8 begins with `go version go1.26.10`. Line 9 begins with `Google Chrome 157.0.8103.50`. Line 10 begins with `Linux 6.12.113+`.

[node.txt](logs/goal-24/node.txt) pass counts are line 93 `14`, line 181 `13`, line 251 `10`, line 291 `5`, line 319 `3`, line 335 `1`, line 357 `2`, and line 415 `8`. Each following line is `# fail 0`. Line 258 is `storage does not select validator`. The suite total is 56 passing tests.

[pytest.txt](logs/goal-24/pytest.txt) line 6 is `32 passed in 2.71s`. That includes the browser run against `svn-operator`.

The operator module adds no third-party Go module and no `go.sum`. `agents/provider` tests import it through a `replace`. [sbom.txt](logs/goal-24/sbom.txt) line 5 stayed `sbom ok`.

## Onboarding and drain measurements

These numbers are this VM, one physical machine, two provider processes. They are not a capacity plan.

[provider-test.txt](logs/goal-24/provider-test.txt) lines 39 through 44:

| Step | Observation |
| --- | --- |
| Onboard | `storage=true validator=false forces_validator=false single_machine=true` |
| Delivery | HTTP 200, `used_bytes=64`, provider `disk_available_bytes=261624107008` |
| Exhaust | quota set to 64, next accept HTTP 409 `insufficient quota` |
| Drain | serving stays 200, a new offer is `draining` |
| Handoff | alpha 410, beta 200, `used_after=0`, `broadcasts_added=0` |
| Disable | package status 503, `chain_submitted=false` |

[provider-test.txt](logs/goal-24/provider-test.txt) line 37 is `TestConformance` still passing. Line 45 is the dashboard flow. The package bytes were fetched from the provider process and the sha256 matched inside that test.

[operator-test.txt](logs/goal-24/operator-test.txt) line 7 prints `local_disk_available_bytes=2.61682601984e+11`, which is 261682601984 bytes, class `measured`. That is statfs on this process. The note in the service is `services/operator/dash/model.go:102`.

## Acceptance

| Check | Status | Where |
| --- | --- | --- |
| Fresh provider accepts and serves a package | VERIFIED | [provider-test.txt](logs/goal-24/provider-test.txt) line 40. The providers are `svn-provider` child processes. |
| Change a limit and exhaust it | VERIFIED | Same log, line 41. `agents/provider/internal/agent/agent.go:170` is `SetLimits`. It does not submit a chain transaction (`agents/provider/internal/agent/agent.go:189`). |
| Drain, then handoff, lease bytes released | VERIFIED for the local provider record | Same log, lines 42 to 43. `used_after=0`. `broadcasts_added=0`. `services/operator/dash/service.go:764` is `Handoff`. A missing successor copy is `last_needed_copy` (`services/operator/dash/service.go:779`). |
| Committed chain end of that lease | BLOCKED | The provider withdraw keeps `chain_end` false. No devnet transaction was submitted for the handoff. |
| Viewer, provider, moderator, council separation | VERIFIED | [operator-test.txt](logs/goal-24/operator-test.txt) line 8. A viewer cannot drain. A provider cannot read the queue. Governance returns `GOVERNANCE_NOT_SUBMITTED`. [pytest.txt](logs/goal-24/pytest.txt) line 6 is the browser run of the same split. |
| Stale chain or index is marked stale | VERIFIED | [operator-test.txt](logs/goal-24/operator-test.txt) line 8. [node.txt](logs/goal-24/node.txt) line 270 is `a stale reading stays marked stale`. The browser text included `STALE`. |
| Public listener is not the admin listener | VERIFIED | `services/operator/dash/http.go:16` is `PublicHandler`. `services/operator/dash/http.go:41` is `AdminHandler`. A POST on the public listener is 405 in the operator test. |
| Dashboard does not submit governance | VERIFIED | `services/operator/dash/service.go:914` is `RefuseGovernance`. The flow log line 44 follows a council POST that did not add a broadcast. |
| Evidence mode and scheduler queue | VERIFIED as labels, not invented counters | Evidence mode is `agents/provider/internal/agent/read.go:22`. The scheduler queue reading is absent (`services/operator/dash/service.go:488`). |
| Single-machine demo stays labelled | VERIFIED | `apps/web/operator.html:19`. `independence_proven` stays false in the operator test. |
| Role and permission map | VERIFIED as a document | [operator-dashboard.md](../operations/operator-dashboard.md) line 7. |
| Tasks that still need an administrator | VERIFIED as a list | [operator-dashboard.md](../operations/operator-dashboard.md) line 43. |
| Manual independent-operator walkthrough | BLOCKED | No independent person participated. |
| Live chain query of council proposals | BLOCKED | The tests read a loopback JSON document. They did not query a node. |
| Account-service sessions | BLOCKED | Development bearers are in memory. PostgreSQL is absent ([pg16.txt](logs/goal-24/pg16.txt) line 5). |
| golangci-lint | BLOCKED | [golangci.txt](logs/goal-24/golangci.txt) line 6 `EXIT:127`. |
| Compose | BLOCKED | [docker.txt](logs/goal-24/docker.txt) line 6 `EXIT:127`. The daemon on port 2375 was not used. |
| Processing worker, account service, and validator install | BLOCKED | Onboarding records the choice. It does not start those processes and it does not load a consensus key. |

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-24-01 | OPEN, BLOCKED | DEC-002. No operator list is named. `independence_proven` is false. A failure-domain string is a declaration. |
| DR-24-02 | OPEN, BLOCKED | No read-only chain query is wired to a node. Proposals are absent unless a loopback document URL is configured. The tests used a document, not a node. |
| DR-24-03 | OPEN | Local quota and bandwidth are process configuration. They are not a DEC-005 allowance and not a chain policy. |
| DR-24-04 | OPEN, BLOCKED | Renewal review is stored locally and is not broadcast. An evidence renewal is not submitted. |
| DR-24-05 | OPEN, BLOCKED | Dashboard sessions are development bearers in memory. They are not account-service identities. |
| DR-24-06 | OPEN | The moderator view is a queue index. Complaint text is not copied. Case decisions stay on discovery. |
| DR-24-07 | OPEN | Bytes fetched and bytes served are provider counters. There is no interface bandwidth counter. The scheduler does not expose a job queue, so that reading is absent. |
| DR-24-08 | OPEN, BLOCKED | PostgreSQL 16 is absent. golangci-lint is absent. Docker is absent. The goal 06 and goal 07 mutation anchors were not re-run. |
| DR-24-09 | OPEN, BLOCKED | The manual walkthrough needs a second person. None participated. |
| DR-24-10 | OPEN | Declared capacity and chain policy are refused with `GOVERNANCE_NOT_SUBMITTED`. |
| DR-24-11 | OPEN, BLOCKED | Processing, account/discovery, and validator onboarding do not install those roles. |
| DR-24-12 | OPEN | Handoff withdraws a local lease after the successor status shows the content. It does not submit a chain end. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody stays unimplemented.
2. **DEC-002.** No named operators. DR-24-01.
3. **One machine.** The browser, the dashboard, and both providers are one VM.
4. **Goal 06 and goal 07 mutation runners.** Not re-run.
5. **golangci-lint.** Not installed.
6. **PostgreSQL 16.** Not installed. PostgreSQL 18 is not installed either.
7. **Goal 04 compose bridge.** Docker is not installed. Compose was not run.

## Requirements

The requirements matrix was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-UX-011 | The onboarding checkboxes are independent. Storage does not select validator. The flow log line 39 and the browser run agree. |
| REQ-UX-012 | Disk, leases, the local queue when configured, bandwidth as configured or absent, and health come from the sources that exist. Missing sources stay absent. |
| REQ-UX-013 | Limits, drain, and handoff ran against `svn-provider`. The chain end did not. |
| REQ-UX-018 | Local quota and handoff ran on this VM. An independent install did not. |
| REQ-EVID-004 | Operator and failure-domain text stay claims. Two node ids do not set `independence_proven`. |
| REQ-RISK-004 | BLOCKED on DEC-002. The page says the demo is one machine. It does not name a production operator. |
| REQ-PROD-001 | The operator page is separate from the viewer bundle entry. A joint demonstration with the chain is goal 30. |

## Next safe action

Do not describe this VM as independent operators. Do not submit a council message from the dashboard. DEC-002 still blocks an operator list. Public launch stays disabled.
