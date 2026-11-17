# Goal 23 evidence

Status: BLOCKED

Reports, account blocks, a moderator queue, decisions, appeals, escalation, and creator removal are implemented on the discovery process. Case files stay in that process. They are not chain transactions and they are not video bytes. A local delist does not wait for a new committed height. Repair, publication, upload admission, a queued encode, playback, and a later storage offer do not revive a delisted id. An unreachable host is recorded on its own and is not a successful deletion. Historical ledger records are disclosed as retained. DEC-003 does not name rule text or an appeal authority. DEC-004 does not set a retention period, so the process keeps the minimum and says so. DEC-007 does not set an audience or an age rule. Public launch stays disabled. This is not legal approval, not an incident review, and not a production network.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/23-moderation-blocking-and-removal.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-07987115-d869-5b75-b845-1d0a20ac9497`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run. The playbook's GPT-6.1 Sol was replaced by this Cursor cloud agent on that default model.
- On 2026-10-31 the owner waived the model requirement as a blocker. On 2026-11-01 the owner extended that acknowledgement through goal 30. That covers model allocation only.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `21672f8609ce0def3bb7973ea8598344ba472818` |
| Tree | `e1d349cb4bffeedf3adc7d0d220378ea7f499ba9` |
| Base | `39e98b05137a10e5b12c8e3962b4934a297db6a4` |
| Branch | `goal-23` |

The tested commit message carries `Tested-tree: e1d349cb4bffeedf3adc7d0d220378ea7f499ba9`. `git rev-parse 21672f8609ce0def3bb7973ea8598344ba472818^{tree}` is that tree. It requires the content owner on `POST /removal` and an operator session on `POST /v1/policy/delist`. The earlier tested commit `366eb6b6d3b533a37b2cd62d4459946bf8ac92b5` (tree `5407ec1d9d5c7fb49ee18768b9c2ed829558aa4d`) filters search and does not contain those checks. Later commits change only `docs/evidence/`.

## Captures

[removal-auth.txt](logs/goal-23/removal-auth.txt) was captured on `21672f8609ce0def3bb7973ea8598344ba472818` while `git status --porcelain` was empty. Its line 3 is `DIRTY no`. [moderation.txt](logs/goal-23/moderation.txt) and [race.txt](logs/goal-23/race.txt) stay the captures on `366eb6b6d3b533a37b2cd62d4459946bf8ac92b5`. Their line 3 is `DIRTY no`. They were not re-run. The other logs stay the captures on `7fc13c2fae788ca256cd7516878e3184feae9661`. Each of those also has line 3 `DIRTY no`. The commands ran from the repository root. A `cd` inside a command is the scope of that command.

`apps/web/dist/` is not ignored. [browser.txt](logs/goal-23/browser.txt) line 8 and [creator-browser.txt](logs/goal-23/creator-browser.txt) line 8 are `?? apps/web/dist/`. That directory was removed after each of those commands and was not committed. Every other log ends `AFTER clean`. No other untracked path remained when the captures finished.

## Head CI

[Run 44144023384](https://github.com/Maxencejules/shortvideo-network/actions/runs/44144023384) on `427aed151ab94542f7ac15315d5bcac28e5a0f81` finished with conclusion `success`. All 17 jobs passed, including `python`, `node`, `go`, `contract`, `milestone`, `devnet`, and every `go-race` shard. That head contains the owner and operator checks. It differs from the tested commit `21672f8609ce0def3bb7973ea8598344ba472818` only under `docs/evidence/`. This paragraph is added after `427aed1`. The commit that adds it is a new head, so this file names `427aed1` and not that later head. [Run 44138496171](https://github.com/Maxencejules/shortvideo-network/actions/runs/44138496171) on `c422b930c6cfe11bcec5b5828df7d08981bd1a95` also finished `success`. That head contains the search filter and does not contain the owner or operator checks. Earlier runs [44132283702](https://github.com/Maxencejules/shortvideo-network/actions/runs/44132283702) and [44134781022](https://github.com/Maxencejules/shortvideo-network/actions/runs/44134781022) finished `success` and do not include the search filter.

## What ran

| Command | Scope | Log | Result |
| --- | --- | --- | --- |
| runtime versions | repository root | [runtime.txt](logs/goal-23/runtime.txt) | line 11 `EXIT:0` |
| `gofmt -l` on the Go paths this goal edited | those packages | [gofmt.txt](logs/goal-23/gofmt.txt) | line 5 `EXIT:0` |
| `go vet` | moderation, social, upload service, scheduler, provider | [govet.txt](logs/goal-23/govet.txt) | line 5 `EXIT:0` |
| `go test -count=1 -timeout 180s -v` | `services/api` moderation and social | [moderation.txt](logs/goal-23/moderation.txt) | line 54 `EXIT:0` |
| `go test -race` | those packages, scheduler, provider agent | [race.txt](logs/goal-23/race.txt) | line 9 `EXIT:0` |
| scheduler delist | `TestLocalDelistDoesNotWaitForAStalledChain` | [scheduler.txt](logs/goal-23/scheduler.txt) | line 9 `EXIT:0` |
| provider admission | `TestDelistedContentIsNotAdmitted` | [provider.txt](logs/goal-23/provider.txt) | line 9 `EXIT:0` |
| `python3 scripts/run_tests.py --only node` | `apps/web` test files | [node.txt](logs/goal-23/node.txt) | line 381 `EXIT:0` |
| `npm run typecheck --workspace apps/web` | `apps/web` | [typecheck.txt](logs/goal-23/typecheck.txt) | line 9 `EXIT:0` |
| pytest delivery, media delist, operations docs | those three files | [pytest.txt](logs/goal-23/pytest.txt) | line 6 `EXIT:0` |
| `ruff check` and `ruff format --check` | the edited Python files | [ruff.txt](logs/goal-23/ruff.txt) | line 7 `EXIT:0` |
| `python3 -m mypy` | the edited Python files | [mypy.txt](logs/goal-23/mypy.txt) | line 6 `EXIT:0` |
| `python3 scripts/scan_secrets.py` | repository | [secrets.txt](logs/goal-23/secrets.txt) | line 5 `secret scan ok`, line 6 `EXIT:0` |
| discovery browser | `tests/unit/test_social_browser.py` against `cmd/svn-discovery` | [browser.txt](logs/goal-23/browser.txt) | line 6 `EXIT:0` |
| creator browser | `tests/unit/test_creator_browser.py` | [creator-browser.txt](logs/goal-23/creator-browser.txt) | line 6 `EXIT:0` |
| PostgreSQL binaries | `/usr/lib/postgresql/16` and `/usr/lib/postgresql/18` | [pg16.txt](logs/goal-23/pg16.txt) | line 5 `pg16 absent`, line 6 `pg18 absent`, line 7 `EXIT:1` |
| `docker version` | host | [docker.txt](logs/goal-23/docker.txt) | line 5 `bash: line 1: docker: command not found`, line 6 `EXIT:127` |
| `golangci-lint version` | host | [golangci.txt](logs/goal-23/golangci.txt) | line 5 `bash: line 1: golangci-lint: command not found`, line 6 `EXIT:127` |
| `go test -count=1 -timeout 60s -v` | `services/api/internal/service/removaltest` | [removal-auth.txt](logs/goal-23/removal-auth.txt) | line 6 `--- PASS`, line 9 `EXIT:0` |

[runtime.txt](logs/goal-23/runtime.txt) line 5 is `v22.23.4`. Line 6 is `10.9.9`. Line 7 is `Python 3.12.3`. Line 8 begins with `go version go1.26.10`. Line 9 begins with `Google Chrome 157.0.8103.50`. Line 10 begins with `Linux 6.12.113+`.

[moderation.txt](logs/goal-23/moderation.txt) lines 6 through 19 are the five moderation-package tests. Line 23 is the discovery queue test. Line 25 is the search test. Line 27 is the creator-removal test. Lines 29 through 51 are the existing social tests, still passing. Line 52 is `PASS` for that package.

[node.txt](logs/goal-23/node.txt) pass counts are line 93 `14`, line 181 `13`, line 251 `10`, line 279 `3`, line 295 `1`, line 317 `2`, and line 375 `8`. Each following line is `# fail 0`. The suite total is 51 passing tests.

The `services/api` packages that start PostgreSQL through `pgdev` were not run. PostgreSQL is not installed here. That is BLOCKED, not a pass. `go vet` on `services/api/internal/service` did run ([govet.txt](logs/goal-23/govet.txt) line 5). That vet log is the capture on `7fc13c2fae788ca256cd7516878e3184feae9661`. It was not re-run after the authorization change. [removal-auth.txt](logs/goal-23/removal-auth.txt) line 6 is the handler test. It uses an in-memory owner map and does not open PostgreSQL.

## Delisting gate

The five-minute figure is the blueprint's PROPOSED gate (`services/api/internal/moderation/policy.go:19`). It is not a promise. The clock in the test is injected.

[moderation.txt](logs/goal-23/moderation.txt) lines 14 through 18:

| Host | Kind | Endpoint | Elapsed seconds | Within the proposed gate | Cache generation |
| --- | --- | --- | --- | --- | --- |
| local | controlled | pipeline | 0 | yes | 0 |
| discovery | controlled | `/v1/feeds/following` | 1 | yes | 2 |
| delivery | controlled | `/v1/playback/c1` | 240 | yes | 9 |
| slow-cache | controlled | `/v1/media/c1` | 301 | no, late | 9 |
| other-community | independent | none | 301 | no, unreachable | 0 |

The slow controlled host and the unresponsive independent host keep `successful_deletion` false. An unresponsive independent host is not a successful deletion.

## Removal coverage

| Stage or fault | Status | Where |
| --- | --- | --- |
| Uploading | VERIFIED for the local pipeline and the upload gate function | [moderation.txt](logs/goal-23/moderation.txt) line 10. `services/api/internal/moderation/removal.go` stops the uploading stage. `services/api/internal/service/http.go:61` calls `withheld` before an upload is stored. `services/api/internal/service/http.go:422` is the blocked-account check. The HTTP upload process was not started here. |
| Processing | VERIFIED for a queued filesystem job | [pytest.txt](logs/goal-23/pytest.txt) line 4 includes `tests/unit/test_media_delist.py`. `workers/media/svn_media/queue.py:293` cancels before encode. |
| Replicating | VERIFIED that a stalled read does not publish after a local delist | [scheduler.txt](logs/goal-23/scheduler.txt) line 9. `services/scheduler/internal/sched/sched.go:308` returns before a new offer. `services/scheduler/internal/sched/sched.go:1098` is `DelistLocal`. |
| Repair | VERIFIED that repair does not post an offer | Same scheduler log. The discovery removal test also refuses `AllowRepair` ([moderation.txt](logs/goal-23/moderation.txt) line 27). |
| Playback | VERIFIED that the cache drops without a chain note of HELD | [pytest.txt](logs/goal-23/pytest.txt) line 4 includes `tests/unit/test_delivery_hosts.py`. `services/delivery/svn_delivery/catalog.py:196`. |
| Storage admission | VERIFIED that a delisted id is refused before fetch | [provider.txt](logs/goal-23/provider.txt) line 9. `agents/provider/internal/agent/accept.go:144`. |
| Offline controlled host | VERIFIED as incomplete | [moderation.txt](logs/goal-23/moderation.txt) line 12. |
| Stalled chain | VERIFIED that commit is not called and local delist still holds | Same line, and the scheduler log. |
| Independent host | VERIFIED as not deleted | [moderation.txt](logs/goal-23/moderation.txt) line 18. |
| Chain commit after a local delist | VERIFIED that the gate stays closed | [moderation.txt](logs/goal-23/moderation.txt) line 6. `services/api/internal/moderation/gate.go` `NoteChainPublication`. |

## Acceptance

| Check | Status | Where |
| --- | --- | --- |
| Unauthorized moderator action | VERIFIED | [moderation.txt](logs/goal-23/moderation.txt) line 23. A stranger receives 403 on the queue, the case, and the decision. |
| Confidential case access | VERIFIED | Same test. The report response and the stranger's case response do not contain the narrative. The reporter's case response does not contain `complainant`. |
| Spam report | VERIFIED | Same test. A second report is `spam` and does not store another narrative (`services/api/internal/social/moderation.go:123`). |
| Block interactions | VERIFIED for comments, follows, feeds, and search | [moderation.txt](logs/goal-23/moderation.txt) line 23 covers the comment, the follow, and the recommended feed. Line 25 is `TestSearchOmitsBlockedAndWithdrawn`. After the block, profile, video, and topic search omit that account and that account's videos, and the feed cache header is `refreshed`. A withdrawn video is then absent from video and topic search. `services/api/internal/social/feed.go:140` is `profileVisible`. `services/api/internal/social/feed.go:160` is the block check inside `visible`. `services/api/internal/social/feed.go:258` and `services/api/internal/social/feed.go:274` and `services/api/internal/social/feed.go:289` are the search checks. |
| Appeals and escalation | VERIFIED | Same test. The appeal statement is not echoed. Escalation moves to `requested` and then `escalated`. Reversal restores a moderator hide that was not a creator removal. |
| Account takedown | VERIFIED | Same test. The bearer no longer works, a new session is refused, `history_erased` is false, and `universal_deletion` is false. |
| UI report and block | VERIFIED against `svn-discovery` | [browser.txt](logs/goal-23/browser.txt) line 6. The test then reads `GET /v1/reports` and `GET /v1/blocks`. The report list does not contain the narrative. |
| Creator removal request | VERIFIED in the client and against the discovery removal route | [node.txt](logs/goal-23/node.txt) line 93 includes the removal test. [creator-browser.txt](logs/goal-23/creator-browser.txt) line 6 expects `REMOVAL_ACCEPTED` from the stand-in, which is not `svn-api`. The discovery route is `services/api/internal/social/http.go:62` and is covered by [moderation.txt](logs/goal-23/moderation.txt) line 27. `apps/web/src/creator.ts:575` sends the request. |
| Policy and contact page | VERIFIED as placeholders | `GET /v1/policy` is in the queue test. The page copy is `apps/web/src/ui.ts:633`. `services/api/internal/moderation/policy.go:62` sets `LegalApproval` false. |
| Upload removal authorization | VERIFIED at the handler | [removal-auth.txt](logs/goal-23/removal-auth.txt) line 6. A non-owner receives 403 on `POST /removal` and the item stays publishable. The owner receives 202 and the gate then refuses publication. No session and a creator session receive 403 on `POST /v1/policy/delist` and the gate does not change. An operator session receives 200 and the gate delists the content and blocks the account. `services/api/internal/service/http.go:346` is `removal`. `services/api/internal/service/http.go:369` is the owner refusal. `services/api/internal/service/http.go:381` is `policyDelist`. `services/api/internal/service/http.go:384` is the operator refusal. `services/api/internal/service/http.go:473` builds the harness. |
| Upload and publication HTTP | BLOCKED for the database path | PostgreSQL is absent ([pg16.txt](logs/goal-23/pg16.txt) line 5). `services/api/internal/service/store.go:337` is `ContentOwner`. That query was not executed. The publication refusal is `services/api/internal/service/http.go:432`. The tests that start PostgreSQL were not run. |
| PostgreSQL persistence of cases | BLOCKED | Restart loses the in-memory rows. |
| golangci-lint | BLOCKED | [golangci.txt](logs/goal-23/golangci.txt) line 6 `EXIT:127`. |
| Compose | BLOCKED | [docker.txt](logs/goal-23/docker.txt) line 6 `EXIT:127`. The daemon on port 2375 was not used. |
| Public launch | BLOCKED | No policy, incident, age, or rights approval is claimed. |

## Runbook

`docs/operations/moderation-and-removal.md`. The checks in [pytest.txt](logs/goal-23/pytest.txt) line 4 include `tests/unit/test_operations_docs.py`. The page states that it is not a production runbook and that public launch is disabled. Contact roles are `PLACEHOLDER unassigned`.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-23-01 | OPEN, BLOCKED | DEC-003. The approved default is the mechanism: report, block, queue, reasons, appeals, escalation, and a removal that does not promise universal deletion. The community rule text is not approved. The served text is a placeholder. |
| DR-23-02 | OPEN, BLOCKED | DEC-004 sets no retention period. After a decision the complaint narrative is dropped. The kept rows are the outcome, the reason code, the admin audit row, and the delist acknowledgements. No duration is claimed. |
| DR-23-03 | OPEN, BLOCKED | DEC-007. No audience, no age rule, and no minor-safety claim. Public sign-up stays off. |
| DR-23-04 | OPEN, BLOCKED | Incident coverage and a rights or privacy review are not approved. Public launch stays disabled. |
| DR-23-05 | OPEN, BLOCKED | PostgreSQL is absent. Moderation rows are in memory and are lost on restart. |
| DR-23-06 | OPEN | The five-minute figure is a PROPOSED gate. The measurement above is this test's clock, not a service promise. |
| DR-23-07 | OPEN | No appeal authority is named. Escalation status is recorded. The authority line is `PLACEHOLDER unassigned`. |
| DR-23-08 | OPEN, BLOCKED for the database path | Authorization of `POST /removal` and `POST /v1/policy/delist` ran in [removal-auth.txt](logs/goal-23/removal-auth.txt) line 6. The check uses the session and the operator role. There is no dev bypass on these routes. `Store.ContentOwner` (`services/api/internal/service/store.go:337`) and the `POLICY_WITHHELD` publication path (`services/api/internal/service/http.go:432`) were not executed. PostgreSQL is absent. |
| DR-23-09 | OPEN | A committed publication does not clear a local delist. Validators are not given a legality vote. The complaint is not a chain message. |
| DR-23-10 | OPEN, BLOCKED | One machine. An unresponsive independent host was a test double, not a second operator. That host was not counted as deleted. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody and recovery stay unimplemented.
2. **DEC-003, DEC-004, and DEC-007.** Placeholders only. DR-23-01, DR-23-02, and DR-23-03.
3. **One machine.** The browser and the discovery process are one VM.
4. **Goal 06 and goal 07 mutation runners.** Not re-run.
5. **golangci-lint.** Not installed.
6. **PostgreSQL 16.** Not installed. PostgreSQL 18 is not installed either.
7. **Goal 04 compose bridge.** Docker is not installed. Compose was not run.

## Requirements

The requirements matrix was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-MOD-001 | The queue, reasons, appeals, and escalation ran. The rule text is a placeholder. |
| REQ-MOD-003 | The same local delist is applied at discovery, the upload gate, playback, repair, and storage admission. Validators do not receive the complaint. |
| REQ-MOD-004 | Removal delists and discloses what it does not erase. The promise text is not approved. |
| REQ-MEDIA-008 | Cooperating services in the tests delist. An independent host that does not answer is disclosed. |
| REQ-UX-016 | Report and block in the viewer call the discovery process. |
| REQ-UX-017 | The creator removal control sends a request. The stand-in in the creator browser is not `svn-api`. |
| REQ-API-001 | `POST /v1/reports` and `POST /v1/removal` are implemented on discovery. Upload `POST /removal` and `POST /v1/policy/delist` authorization ran through the harness. The PostgreSQL handler tests did not run. |
| REQ-SCOPE-018 | The policy page carries the placeholder contact lines. |

## Next safe action

Do not describe the placeholders as legal approval or as a retention period. Do not describe an unreachable host as deleted. DEC-001 still blocks custody. Public launch stays disabled.
