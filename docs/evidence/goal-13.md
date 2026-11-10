# Goal 13 evidence

Status: BLOCKED

The upload service is implemented and the checks below ran on one machine. The goal stays BLOCKED. The minimum account and permission contract is not approved (DR-13-01). No approved quarantine adapter is named (DR-13-02). DEC-005 leaves allowances unset (DR-13-03). The `ffprobe` probe is a proposal (DR-13-04). Inherited blockers from goals 04, 11, and 12 are carried below. The owner approved starting goal 13 anyway.

Public launch: DISABLED. No public sign-up route. No tradable token. This is not an audit and not a production network.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/13-authenticated-uploads-and-quarantine.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-69341c39-08ec-50f2-8fe1-d06e62f30252`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed.
- On 2026-10-31 the owner waived the model requirement as a blocker ("Don't mind the ai model requirement", recorded on goal 04). On 2026-11-01 the owner wrote "I acknowledge the model substitution for DR-06-07" and then "extend the acknowledgement to goals 07 through 30". That covers model allocation only. It does not certify correctness.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `42eced0d4a3aaf7d196ffe3f95cd1d5ff6253d33` |
| Tree | `3a2d13615cb399a030e56d80ac9dd537fd8be416` |
| Base | `5ea4e9f010a606a2c44157b2ce35fadba5eee957` (goal 12 merge on `main`). It is the parent of the tested commit. |
| Branch | `goal-13` |
| Runtime | Ubuntu 24.04.5 LTS, Linux 6.12.113+ x86_64, uid 1000. Go 1.26.10. PostgreSQL 18.6. Python 3.12.3, ruff 0.17.3, mypy 2.4.1. ffprobe 6.1.1-3ubuntu5. [runtime.txt](logs/goal-13/runtime.txt) |

The tested commit message carries `Tested-tree: 3a2d13615cb399a030e56d80ac9dd537fd8be416`. This commit changes only `docs/evidence/`.

Head CI had not finished when this file was written. The run id is added in a later evidence commit if a run is observed. A missing run is not a pass.

## Inherited blockers

BLOCKED, carried from goals 04, 11, and 12:

1. **DEC-001 and DR-07-01, production custody.** This service does not hold a signing key and does not submit the held creator intent. A dev upload is not a production publication approval.
2. **One machine.** Every process in these logs is on this host. It is not an independent-operator result.
3. **Goal 06 and goal 07 mutation runners.** Anchor checks only: [anchors06.txt](logs/goal-13/anchors06.txt) exit 1, `RESULT search-string problems=13; no test was run`; [anchors07.txt](logs/goal-13/anchors07.txt) exit 1, `RESULT validation problems=3; no test was run`.
4. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-13/golangci-lint.txt) exit 127.
5. **PostgreSQL 16.** `/usr/lib/postgresql/16/bin/postgres` is absent. The server in use is 18.6. [pg16.txt](logs/goal-13/pg16.txt) exit 127. Not a pass.
6. **Goal 04 compose bridge.** `docker` is not installed. [docker.txt](logs/goal-13/docker.txt) exit 127. Compose was not run. The unauthenticated :2375 daemon was not used.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-13-01 | OPEN | Session and role boundary is a proposal. Development fixtures cannot start a production listener. There is no production verifier in this goal. |
| DR-13-02 | OPEN | Quarantine is a local directory. No approved object-storage adapter was found. |
| DR-13-03 | OPEN | Per-account numbers are PROPOSED stand-ins. DEC-005 is unset. The process rejects a per-video ceiling above 100000000 bytes or 60 seconds. |
| DR-13-04 | OPEN | Duration and structure come from a deadline-bounded `ffprobe` child, not from client JSON. It is not the Goal 14 sandbox. |

## Acceptance

| Check | Status | Log |
| --- | --- | --- |
| Permitted fixture, quarantine file, one job, held intent without the filename or token | VERIFIED as a test of the proposal | [api-unit.txt](logs/goal-13/api-unit.txt) `TestPermittedFixtureQuarantineAndJob` |
| Malformed body and a misleading name and MIME type | VERIFIED | same log, `TestMalformedUntrustedAndDimensions` |
| Declared size over the ceiling, body longer than declared, config above 100000000 bytes or 60 seconds | VERIFIED | same log, `TestOversizedBodiesAndCeiling` |
| False completion digest, then one real job | VERIFIED | same log, `TestFalseCompletionAndDuplicate` |
| Duplicate idempotency key reserves once; a different size conflicts | VERIFIED | same test |
| Eight concurrent intents, one reservation; output budget is separate from input | VERIFIED | same log, `TestConcurrentQuotaAndOutputBudget` |
| Short body stays `RECEIVING`; sweep expires it and releases the reservation once | VERIFIED | same log, `TestInterruptedSweepReleasesOnce` |
| Transfer after expiry stores no file; sweep releases once | VERIFIED | same log, `TestExpiredTransfer` |
| Other account cannot read, cancel, or complete; bearer cannot write bytes; transfer token cannot read status | VERIFIED | same log, `TestCrossAccountDenied` |
| Kill the API during probe, start it again, one job, reservation unchanged | VERIFIED | same log, `TestRestartDoesNotDoubleJobOrLeakQuota` |
| No playback path returns the bytes; `POST /publish` is 403; the table rejects `PUBLISHED` | VERIFIED | same log, `TestNoPlaybackAndNotPublished` |
| Client duration is ignored; a 2 second file fails a 1 second cap | VERIFIED | same log, `TestDurationIgnoresClientMetadata` |
| Rate limit, viewer role, production refuses fixtures, source has no fixture account id | VERIFIED as a test of DR-13-01, not an approval | same log, `TestRateLimitRoleAndProductionFixtures` |
| Race detector on `./services/api/...` | VERIFIED | [api-race.txt](logs/goal-13/api-race.txt) exit 0 |
| Account contract approved | BLOCKED | DR-13-01 is OPEN |
| Approved storage adapter | BLOCKED | DR-13-02 is OPEN |
| Head CI | not observed yet | This file does not claim a GitHub run |

`api-unit.txt` exit 0. `go test` printed `PASS` and fourteen `--- PASS:` lines. The capture header's `CMD` line repeats the log path; the arguments after it are the command.

## Requirements rows

| Row | Status | Where |
| --- | --- | --- |
| REQ-SCOPE-012 | VERIFIED for the rejection and the `PROPOSED` label. Not an approved capacity claim. | `TestOversizedBodiesAndCeiling`, `TestDurationIgnoresClientMetadata`, `TestDevelopmentConfigIsProposed` |
| REQ-MEDIA-001 | VERIFIED for authorize-before-bytes, quota, duration, dimensions, and the output reservation on this host. The account contract is BLOCKED (DR-13-01). | `TestPermittedFixtureQuarantineAndJob`, `TestConcurrentQuotaAndOutputBudget`, `TestExpiredTransfer` |
| REQ-MEDIA-002 | VERIFIED | generated object name, `ftyp` check, misleading MIME rejected in `TestMalformedUntrustedAndDimensions` |
| REQ-API-001 | VERIFIED for upload intent, status, and the publish refusal in this goal. Feeds, reports, removal, and exports are later goals. | routes in [uploads.md](../architecture/uploads.md) |

## Commands

Logs were captured on the clean tested tree and copied here unchanged. The step table is [summary.tsv](logs/goal-13/summary.tsv).

| Step | Exit | Note |
| --- | --- | --- |
| api-unit | 0 | `go test -v -count=1 -timeout 8m ./services/api/...` |
| api-race | 0 | `go test -race -count=1 -timeout 8m ./services/api/...` |
| api-vet | 0 | `go vet ./services/api/...` |
| ruff-format, ruff-check | 0 | services, workers/media, packages/protocol, tests, scripts |
| mypy | 0 | |
| python-tests | 0 | 406 passed |
| workflow, secrets, sbom, requirements, protocol, gofmt, gen-proto | 0 | |
| go-suite | 0 | `SVN_SKIP_DEVNET=1 python3 scripts/run_tests.py --only go`. 71 `ok` lines. [go-suite.txt](logs/goal-13/go-suite.txt) |
| anchors06, anchors07 | 1 | BLOCKED. Not passes. |
| golangci-lint, pg16, docker | 127 | BLOCKED. Not passes. |

## Handoff

`POST /upload-intents`, `PUT /uploads/{id}/content`, `POST /uploads/{id}/complete`, `GET /uploads/{id}`, and `DELETE /uploads/{id}` are the upload API. Fixture tokens live in `services/api/fixtures/development.json` and are development-only. Goal 14 reads `api.processing_jobs` (`QUEUED`) and the quarantine object. Goal 15 must not serve that directory. The chain row stays `HELD`. Details are in [uploads.md](../architecture/uploads.md).

## Next safe action

Open one draft pull request. Do not merge it. Do not start goal 14 from this branch until the draft is reviewed. Public launch stays disabled.

## Proposed pull-request title and summary

Title: goal 13: authenticated uploads and quarantine

Summary: `cmd/svn-api` reserves a quota, stores the body under a generated quarantine name, and queues one processing job after `ffprobe`. An upload cannot become `PUBLISHED`. Status stays BLOCKED on DR-13-01 through DR-13-04 and on the inherited custody, single-machine, anchor, lint, PostgreSQL 16, and compose blockers.
