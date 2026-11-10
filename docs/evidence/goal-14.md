# Goal 14 evidence

Status: BLOCKED

The worker runs on this host. The goal stays BLOCKED. The queue, the package format, the sandbox approval, and the processing budgets are not approved (DR-14-01 through DR-14-04). DR-13-01 through DR-13-03 stay open. DR-13-04's technical part is closed because this sandbox runs `ffprobe`; the approval stays open. Inherited blockers are listed below. Public launch: DISABLED. No tradable token. This is not an audit and not a production network.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/14-isolated-video-processing-workers.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-47888b32-70c7-5ad5-b77d-4b1b3c24a8a6`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed.
- On 2026-10-31 the owner waived the model requirement as a blocker ("Don't mind the ai model requirement", recorded on goal 04). On 2026-11-01 the owner wrote "I acknowledge the model substitution for DR-06-07" and then "extend the acknowledgement to goals 07 through 30". That covers model allocation only. It does not certify correctness.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `bba18650f3eedea68f4dac3f1baf9cea85036881` |
| Tree | `46ed5429379f657fd616aa9dde3f34aea6af6437` |
| Base | `08509a5bdb454ada65392a83d029807acf2c75eb` (goal 13 merge on `main`). |
| Branch | `goal-14` |
| Runtime | Ubuntu, Linux 6.12.113+ x86_64, uid 1000, 4 vCPU, Intel Xeon model 207 under KVM. Go 1.26.10. PostgreSQL 18.6. Python 3.12.3, ruff 0.17.3, mypy 2.4.1. FFmpeg `6.1.1-3ubuntu5`. [runtime.txt](logs/goal-14/runtime.txt) |

`git rev-parse bba18650f3eedea68f4dac3f1baf9cea85036881^{tree}` is `46ed5429379f657fd616aa9dde3f34aea6af6437`. The commit message carries the same `Tested-tree` trailer. This evidence commit changes only `docs/evidence/` relative to that tested commit.

## Inherited blockers

BLOCKED, carried from goals 04, 11, 12, and 13:

1. **DEC-001 and DR-07-01, production custody.** This worker does not hold a signing key and does not submit the held creator intent. A ready package is not a production publication approval.
2. **One machine.** Every process in these logs is on this host. It is not an independent-operator result.
3. **Goal 06 and goal 07 mutation runners.** Anchor checks only: [anchors06.txt](logs/goal-14/anchors06.txt) exit 1, `RESULT search-string problems=13; no test was run`; [anchors07.txt](logs/goal-14/anchors07.txt) exit 1, `RESULT validation problems=3; no test was run`.
4. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-14/golangci-lint.txt) exit 127.
5. **PostgreSQL 16.** `/usr/lib/postgresql/16/bin/postgres` is absent (`test -x` exit 1). The server in use is 18.6. [pg16.txt](logs/goal-14/pg16.txt). Not a pass.
6. **Goal 04 compose bridge.** `docker` is not installed. [docker.txt](logs/goal-14/docker.txt) exit 127. Compose was not run. The unauthenticated :2375 daemon was not used.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-14-01 | OPEN | Celery is not imported. The worker claims `api.processing_jobs` and a filesystem job file. Not an approval of that queue. |
| DR-14-02 | OPEN | H.264, AAC, JPEG, and WebVTT, with `content_version + 1` for a new encode, are a proposal. `docs/protocol.md` still says a second encoding is a new `content_id` until a recorded decision. |
| DR-14-03 | OPEN | Network namespace and rlimits were exercised here. The read-only root was not. The `bwrap` profile in [media-worker.md](../architecture/media-worker.md) is not a result. |
| DR-14-04 | OPEN | Budgets stay `PROPOSED`. DEC-005 is unset. |
| DR-13-01 | OPEN | Carried from goal 13. |
| DR-13-02 | OPEN | Quarantine is still a local directory. |
| DR-13-03 | OPEN | Allowances stay unset. |
| DR-13-04 | OPEN for approval. Technical part CLOSED | The sandbox probe ran inside `prlimit` and `unshare --net`. That does not approve the API process's own probe. |

## Acceptance

| Check | Status | Log |
| --- | --- | --- |
| Portrait, rotated, low-resolution, and audio/video fixtures; captions, hashes, duration, total bytes | VERIFIED as a test of the proposal | [media-tests.txt](logs/goal-14/media-tests.txt) `test_portrait_rotated_lowres_and_audio` |
| Malformed, truncated, over-duration, over-dimension, and over-budget output | VERIFIED | same log, `test_malformed_truncated_duration_dimension_and_growth` |
| Retry keeps one package; a new encode is a new version | VERIFIED | same log, `test_retry_is_one_package_and_reencode_is_a_new_version` |
| Cancel removes staging and does not publish | VERIFIED | same log, `test_cancel_removes_staging_and_does_not_publish` |
| Kill the worker mid-job, restart, one package, quota file unchanged, no `PUBLISHED` status | VERIFIED | same log, `test_kill_restart_keeps_one_package_and_quota` |
| Symlink input and a validator key file are refused | VERIFIED | same log, `test_symlink_and_validator_key_are_refused` |
| Caption text is not an FFmpeg argument | VERIFIED | same log, `test_captions_are_not_command_arguments` |
| Network namespace blocks a connect that succeeds outside it; uid map is not host root; rlimits match; a write under `/usr/bin` fails; `http` is not on the protocol whitelist; wall time and file size are bounded | VERIFIED on this host | same log, `test_network_namespace_and_resource_limits` |
| Read-only root filesystem | UNVERIFIED | `bwrap` was not installed. DR-14-03. The external profile is in the architecture note. |
| PostgreSQL claim once, quotas unchanged, upload stays `PROCESSING_QUEUED`, outbox stays `HELD`, `PUBLISHED` is rejected | VERIFIED | same log, `test_postgres_claim_is_once_and_does_not_publish` |
| 60-second supported input | VERIFIED as a measurement on this VM, not a production result | [timing.txt](logs/goal-14/timing.txt) `elapsed_seconds=5.108`. The input is synthetic `testsrc` 1280x720 at 10 fps plus a sine, 60 seconds, encoded with libx264 `veryfast` to `h480` and `h720`. The proposed 120-second gate held for that input on the 4 vCPU host above. It is not a camera-recording result. |
| FFmpeg build and licence record | VERIFIED as a record. BLOCKED as an approval (DEC-006, REQ-MEDIA-009) | [build-record.txt](logs/goal-14/build-record.txt). `ffmpeg version 6.1.1-3ubuntu5`, configuration includes `--enable-gpl`. The GPL record does not settle codec patents. |
| Package format approved | BLOCKED | DR-14-02 |
| Celery/RabbitMQ or an approved queue | BLOCKED | DR-14-01. The PostgreSQL claim is the proposal that was tested. |
| Head CI | Not observed yet | Cited in the evidence commit after the draft PR's run completes. |

`media-tests.txt` exit 0. Pytest printed `15 passed, 1 skipped`. The skip is `test_sixty_second_input_timing`, which ran in `timing.txt` with `SVN_MEDIA_TIMING=1` and passed. A skip of that test in the default suite is not the timing result.

## Requirements rows

| Row | Status | Where |
| --- | --- | --- |
| REQ-SCOPE-014 | VERIFIED for the proposal: no upscale, `h480`/`h720` only when the short side is large enough, otherwise `native`, output over the reserved budget rejected. Not an approved ladder. | `test_portrait_rotated_lowres_and_audio`, `test_malformed_truncated_duration_dimension_and_growth` |
| REQ-TECH-004 | BLOCKED | DR-14-01. A durable claim is implemented and tested. It is not Celery. |
| REQ-TECH-005 | VERIFIED for the isolated child and the build record on this host. | `test_network_namespace_and_resource_limits`, [build-record.txt](logs/goal-14/build-record.txt) |
| REQ-TECH-011 | VERIFIED | `test_worker_is_outside_the_api_process`. The API Go sources do not import `svn_media`. |
| REQ-MEDIA-003 | VERIFIED for the network namespace, the uid map, rlimits, and the disposable workspace on this host. Read-only root UNVERIFIED. | `test_network_namespace_and_resource_limits`, `test_kill_restart_keeps_one_package_and_quota` |
| REQ-MEDIA-004 | VERIFIED for this encoder: the package is identified by digest, and a second encode is a new version without a byte-identical check. | `test_retry_is_one_package_and_reencode_is_a_new_version` |
| REQ-MEDIA-007 | VERIFIED | `test_kill_restart_keeps_one_package_and_quota`, `test_cancel_removes_staging_and_does_not_publish` |
| REQ-MEDIA-009 | BLOCKED | DEC-006. The build record exists and is not an approval. |
| REQ-QA-006 | VERIFIED as the measurement above. The 120-second figure stays a proposed gate. 5.108 seconds on this VM. | [timing.txt](logs/goal-14/timing.txt) |

## Commands

The step table is [summary.tsv](logs/goal-14/summary.tsv). Logs were copied here unchanged apart from a trailing `EXIT:0` line on the build record, whose command is `PYTHONPATH=workers/media:packages/protocol python -m svn_media build-record`.

| Step | Exit | Note |
| --- | --- | --- |
| ruff-format, ruff-check | 0 | `services workers/media packages/protocol tests scripts` |
| mypy | 0 | |
| python-tests | 0 | `scripts/run_tests.py --only python`. 419 passed, 1 skipped, in 102.63s. [python-tests.txt](logs/goal-14/python-tests.txt) |
| media-tests | 0 | verbose names in [media-tests.txt](logs/goal-14/media-tests.txt) |
| timing | 0 | `elapsed_seconds=5.108` |
| workflow, secrets, sbom, requirements | 0 | |
| protocol | 0 | After `scripts/install_toolchains.sh --protoc-only`. The first attempt, before `protoc` was installed, exited 1 with `protoc well-known types BLOCKED` and no other error. That attempt is not a pass. The saved log is the second run. |
| gofmt, gen-proto | 0 | `gen-proto` used the pinned `protoc` 36.2. It did not change a tracked proto file. |
| go-suite | 0 | `SVN_SKIP_DEVNET=1 scripts/run_tests.py --only go`. 71 `ok` lines. [go-suite.txt](logs/goal-14/go-suite.txt). This does not run the devnet tests. |
| anchors06, anchors07 | 1 | BLOCKED. Not passes. |
| golangci-lint, docker | 127 | BLOCKED. Not passes. |
| pg16 | 1 | BLOCKED. The binary is absent. Not a pass. |

## Handoff

`python -m svn_media run --root <dir>` recovers a crashed job and encodes queued filesystem jobs. `python -m svn_media claim-run` claims `api.processing_jobs` first. The package directory, the manifest, and the ready record are described in [media-worker.md](../architecture/media-worker.md). Goal 15 may read `packages/<content_id>/v<content_version>/` when the record says `READY_FOR_REVIEW`. It must not serve quarantine. The chain row stays `HELD`. The worker does not publish.

## Head CI

Run [42737222474](https://github.com/Maxencejules/shortvideo-network/actions/runs/42737222474) on `07366435e249031e8d419dee8fc851902915ad48` failed the `python` job. GitHub's runner refused `unshare --user`, so `ffprobe` never started and valid files were reported as `TRUNCATED`. That run is not a pass. The tested commit uses the network namespace only when `unshare --user --map-root-user --net` can start a child, and it still applies `prlimit` and the protocol whitelist.

Run [42738414523](https://github.com/Maxencejules/shortvideo-network/actions/runs/42738414523) on the tested commit `bba18650f3eedea68f4dac3f1baf9cea85036881` completed **success**. All 16 jobs succeeded, including `python`, `go`, `devnet`, and the `go-race` shards.

Run [42740055694](https://github.com/Maxencejules/shortvideo-network/actions/runs/42740055694) on `ad3e296376b3c3de783e55acf8deaef04424e439` completed **success**. All 16 jobs succeeded. That commit differs from the tested commit only in `docs/evidence/goal-14.md`.

Run [42742004585](https://github.com/Maxencejules/shortvideo-network/actions/runs/42742004585) on `db1108585ad5717029e41c018b1edce36f33a997` completed **success**. All 16 jobs succeeded. That commit differs from `ad3e296` only in this file. It is the green run on that head.

## Next safe action

Open a draft pull request. Do not merge it. Do not start goal 15 from an unreviewed branch. Public launch stays disabled.

## Proposed pull-request title and summary

Title: goal 14: isolated video processing workers

Summary: `workers/media` probes a queued upload inside a network namespace with rlimits, writes one ready-for-review package, and does not publish. Status stays BLOCKED on DR-14-01 through DR-14-04, on the open goal 13 decisions, and on the inherited custody, single-machine, anchor, lint, PostgreSQL 16, and compose blockers. The read-only root was not exercised.
