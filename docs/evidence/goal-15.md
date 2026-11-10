# Goal 15 evidence

Status: BLOCKED

The origin adapter plays a real processed package on this host. The goal stays BLOCKED. The package format is still the goal 14 proposal (DR-14-02). The local playback grant is not publication (DR-15-02). DR-15-01 through DR-15-06 are open. DR-13-01 through DR-13-03 stay open. Inherited blockers are listed below. Public launch: DISABLED. No tradable token. This is not an audit and not a production network. P2P, TURN, WebRTC, and public IPFS stay disabled.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/15-media-manifests-and-https-playback.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-84566a44-d8c7-515d-b6cf-b2b98bc68729`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed.
- On 2026-10-31 the owner waived the model requirement as a blocker ("Don't mind the ai model requirement", recorded on goal 04). On 2026-11-01 the owner wrote "I acknowledge the model substitution for DR-06-07" and then "extend the acknowledgement to goals 07 through 30". That covers model allocation only. It does not certify correctness.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `f81d819427fca3fd552a93f43001aa13a08b6ae4` |
| Tree | `89b4821191228bae97c71ac9f9511e7ce1448cb2` |
| Base | `fc8b79da5f6a96f8dfad4aebb4bf74a4a38d82f0` (goal 14 merge on `main`). |
| Branch | `goal-15` |
| Draft PR | [PR 20](https://github.com/Maxencejules/shortvideo-network/pull/20) |

`git rev-parse f81d819427fca3fd552a93f43001aa13a08b6ae4^{tree}` is `89b4821191228bae97c71ac9f9511e7ce1448cb2`. The commit message carries the same `Tested-tree` trailer. This evidence commit changes only `docs/evidence/` relative to that tested commit.

The parent code commit `1255394077693248406ffe3910ad40c163a6dd36` is the same delivery behavior except the headless profile cleanup. Run [42758130972](https://github.com/Maxencejules/shortvideo-network/actions/runs/42758130972) on `5ba8c476c83ac64e641382d8486dfb3d7791061f` failed the `python` job: Chrome left its profile directory busy and `TemporaryDirectory` raised `ENOTEMPTY`. That run is not a pass. The tested commit stops the browser process group and deletes the profile with `ignore_errors`. The decode test was run again on the tested commit.

[runtime.txt](logs/goal-15/runtime.txt) has Linux 6.12.113+, Python 3.12.3, Go 1.26.10, FFmpeg `6.1.1-3ubuntu5`, Google Chrome `156.0.8078.47`, `nproc` printing `4`, and uid 1000. PostgreSQL 16 is absent in that log. PostgreSQL 18 was installed later, before `python-tests-rerun.txt` and `go-suite-rerun.txt`. [postgres18.txt](logs/goal-15/postgres18.txt) shows `postgres (PostgreSQL) 18.6 (Ubuntu 18.6-1.pgdg24.04+2)` and `postgresql-18 18.6-1.pgdg24.04+2`. It is not PostgreSQL 16.

## Inherited blockers

BLOCKED, carried from goals 04, 11, 12, 13, and 14:

1. **DEC-001 and DR-07-01, production custody.** This process does not hold a signing key and does not submit a hosting promise. A playback grant is not a production publication approval.
2. **One machine.** Every process in these logs is on this host. The second host is another loopback process. It is not an independent operator.
3. **Goal 06 and goal 07 mutation runners.** Anchor checks only: [anchors06.txt](logs/goal-15/anchors06.txt) exit 1, `RESULT search-string problems=13; no test was run`; [anchors07.txt](logs/goal-15/anchors07.txt) exit 1, `RESULT validation problems=3; no test was run`.
4. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-15/golangci-lint.txt) exit 127.
5. **PostgreSQL 16.** `/usr/lib/postgresql/16/bin/postgres` is absent (`test -x` exit 1). [pg16.txt](logs/goal-15/pg16.txt). The server used for the rerun is 18.6. Not a pass for PostgreSQL 16.
6. **Goal 04 compose bridge.** `docker` is not installed. [docker.txt](logs/goal-15/docker.txt) exit 127. Compose was not run. The unauthenticated :2375 daemon was not used.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-15-01 | OPEN | Prefetch is the current segment plus one ahead, and one queued master playlist. Not an allowance. |
| DR-15-02 | OPEN | A local grant plays `READY_FOR_REVIEW` while `publication` stays `NOT_PUBLISHED`. Not a publication approval. |
| DR-15-03 | OPEN | Segment cache is `private, max-age=60`. Removal is `no-store` in this process only. |
| DR-15-04 | OPEN | The host is loopback `origin-test` with an ephemeral certificate. At most four hosts and two attempts. |
| DR-15-05 | OPEN | The term uses the process clock, at most 24 hours. The chain note is operator input. A down note does not create a grant. |
| DR-15-06 | OPEN | Client verification is the bytes the harness or the test page fetched. Native HLS verification is not claimed. |
| DR-14-01 | OPEN | Carried from goal 14. |
| DR-14-02 | OPEN | The package format is still a proposal. Playback reads that layout. |
| DR-14-03 | OPEN | Read-only root was not exercised. Not in this goal's change. |
| DR-14-04 | OPEN | Budgets stay `PROPOSED`. |
| DR-13-01 | OPEN | Carried from goal 13. |
| DR-13-02 | OPEN | Quarantine is still a local directory. This goal does not serve it. |
| DR-13-03 | OPEN | Allowances stay unset. |

## Integrity coverage

| Layer | Claim | Result on this commit |
| --- | --- | --- |
| Origin, before a host is listed | Schema, canonical manifest bytes, ready-record digest, every segment hash and length, ffprobe of each video segment | VERIFIED. A corrupt manifest or segment leaves `hosts` empty and `package_verified` false. A corrupt replica is not accepted. |
| Origin, on the bytes it sends | The sent buffer matches the manifest hash | VERIFIED. The response is `409` and the body is the JSON error, not the media. |
| Harness | SHA-256 of each video segment and caption file it actually fetched. Segments outside the window are not requested. | VERIFIED. `rendered` stays 0. An HTTP 200 is counted in `http_responses` and is not a render. |
| Test page, headless Chrome 156 | The page fetches one rendition playlist, hashes the first media segment with `crypto.subtle`, and decodes that MP4 in a `video` element. The test requires matching width and height, `readyState` at least 2, a non-zero canvas sample, and the caption file starting with `WEBVTT`. | VERIFIED for that one MP4. [delivery-final.txt](logs/goal-15/delivery-final.txt) `test_browser_decodes_the_mp4_it_fetched` PASSED. |
| Native HLS demuxer | Not claimed | Not run. Chromium on Linux has no HLS demuxer. Segments the page did not fetch were not client-verified. |

## Acceptance

| Check | Status | Log |
| --- | --- | --- |
| Real processed media, playlist and segment references, orientation, captions, fallback rendition | VERIFIED | [delivery-final.txt](logs/goal-15/delivery-final.txt) shows `test_processed_media_plays_over_https` PASSED. It does not print the measurements. Those are assertions in `tests/unit/test_delivery.py`: `RESOLUTION=144x256` at line 304, probed size `(144, 256)` at line 324, rotated `(180, 320)` at lines 328-331, `h720` selects `native` at lines 332-335, and `portrait-cue` at line 342. |
| Corrupt manifest and corrupt segment detected before a verified label | VERIFIED | [delivery-final.txt](logs/goal-15/delivery-final.txt) shows `test_corrupt_manifest_and_segment_are_not_advertised` PASSED. |
| Harness detects a segment whose bytes do not match the advertised hash | VERIFIED | [delivery-final.txt](logs/goal-15/delivery-final.txt) shows `test_harness_bounds_failures_and_prefetch` PASSED. |
| Host failure, 404, slow response, bounded attempts | VERIFIED | [delivery-final.txt](logs/goal-15/delivery-final.txt) shows `test_replica_host_failure_and_chain_outage` and `test_harness_bounds_failures_and_prefetch` PASSED. The attempt count is an assertion, not a log line: `tests/unit/test_delivery.py` lines 633-635 require `attempts == 8` and that `h4` and `h5` are absent from `hosts_tried`. |
| Revoked content, conditional request is not a 304, quarantine bytes are not served | VERIFIED | [delivery-final.txt](logs/goal-15/delivery-final.txt) shows `test_range_cors_quarantine_and_removal` PASSED. |
| Abandoned prefetch and no media download of the unselected queue | VERIFIED | [delivery-final.txt](logs/goal-15/delivery-final.txt) shows `test_harness_bounds_failures_and_prefetch` PASSED. The queue bound is an assertion, not a log line: `tests/unit/test_delivery.py` lines 674-676 require `skipped_items == 4`, unchanged `delivered_bytes`, and no fault-host path ending in `.mp4`. |
| Chain note down does not create a grant or a new host; an existing term still plays and `hosting_promise` stays false | VERIFIED | `test_replica_host_failure_and_chain_outage` |
| Headless decode of the fetched MP4 | VERIFIED on this VM | [delivery-final.txt](logs/goal-15/delivery-final.txt) `test_browser_decodes_the_mp4_it_fetched` |
| Native HLS verification of every segment | Not claimed | Not run |
| Package format approved | BLOCKED | DR-14-02, DR-15-02 |
| PostgreSQL 16, golangci-lint, compose, mutation anchors | BLOCKED | logs above. Not passes. |

## Requirements rows

| Row | Status | Where |
| --- | --- | --- |
| REQ-TECH-008 | VERIFIED for HLS over HTTPS on loopback, with WebRTC and TURN absent. Not a public deployment. | `test_processed_media_plays_over_https`, `test_package_is_not_a_scaffold` |
| REQ-MEDIA-005 | VERIFIED for master and rendition playlists, more than one loopback host, bounded retries, a fallback rendition, and integrity on the bytes the harness and the page fetched. | `test_processed_media_plays_over_https`, `test_replica_host_failure_and_chain_outage`, `test_browser_decodes_the_mp4_it_fetched` |
| REQ-HOST-001 | VERIFIED for renditions, segment hashes, byte totals, and format metadata checked before a host is listed. The manifest stays off this chain. This goal does not submit the digest. | `test_corrupt_manifest_and_segment_are_not_advertised`, `test_replica_host_failure_and_chain_outage` |
| REQ-QA-004 | VERIFIED at the origin and at the harness. A corrupt segment or manifest is not given a verified label. | `test_corrupt_manifest_and_segment_are_not_advertised`, `test_harness_bounds_failures_and_prefetch` |
| REQ-MEDIA-006 | The harness bound and the aggregate are VERIFIED here. The product client is still goal 20. | `test_harness_bounds_failures_and_prefetch` |

## Commands

The step table is [summary.tsv](logs/goal-15/summary.tsv). It is step and exit code only. It has no `HEAD` line. Each command log starts with `HEAD`, `TREE`, and `DIRTY`. `DIRTY yes` is the untracked log directory. `git diff` against the tested commit had no tracked change while these commands ran.

| Step | Exit | Note |
| --- | --- | --- |
| ruff-format-final, ruff-check-final | 0 | On the tested commit. [ruff-format-final.txt](logs/goal-15/ruff-format-final.txt), [ruff-check-final.txt](logs/goal-15/ruff-check-final.txt). |
| mypy-final | 0 | [mypy-final.txt](logs/goal-15/mypy-final.txt). |
| python-final | 0 | 428 passed, 1 skipped, in 94.20s. The skip is `test_sixty_second_input_timing`. [python-final.txt](logs/goal-15/python-final.txt). This is the cited Python result. |
| delivery-final | 0 | 9 passed, including the browser decode, in 8.66s. [delivery-final.txt](logs/goal-15/delivery-final.txt). |
| workflow, secrets, sbom, requirements | 0 | |
| protocol | 1 | `protoc well-known types BLOCKED`. Not a pass. [protocol.txt](logs/goal-15/protocol.txt). |
| protocol-rerun | 0 | After `scripts/install_toolchains.sh --protoc-only`. `result PASS`. [protocol-rerun.txt](logs/goal-15/protocol-rerun.txt). |
| gofmt | 0 | |
| gen-proto | 127 | `protoc` was not on `PATH`. Not a pass. [gen-proto.txt](logs/goal-15/gen-proto.txt). |
| gen-proto-rerun | 0 | Pinned protoc 36.2. `git diff --exit-code -- proto/svn proto/gen/python` was clean. [gen-proto-rerun.txt](logs/goal-15/gen-proto-rerun.txt). |
| go-suite | 1 | PostgreSQL 18 was not installed yet. Broadcaster tests failed on the missing binary. Not a pass. [go-suite.txt](logs/goal-15/go-suite.txt). |
| go-final | 0 | On the tested commit. `SVN_SKIP_DEVNET=1`. 71 `ok` lines. [go-final.txt](logs/goal-15/go-final.txt). This does not run the devnet tests. Those stay on the CI race shards. |
| anchors06, anchors07 | 1 | BLOCKED. Not passes. |
| golangci-lint, docker | 127 | BLOCKED. Not passes. |
| pg16 | 1 | BLOCKED. The binary is absent. Not a pass. |

## Handoff

`python -m svn_delivery serve --root <worker-root>` listens on loopback with an ephemeral certificate. It serves no bytes until `note-chain --note HELD` and `authorize`. The API reports `publication: NOT_PUBLISHED` and `hosting_promise: false`. Quarantine is not served. The rules are in [delivery.md](../architecture/delivery.md). Goal 16 is the independent provider. The worker's FFmpeg `file` protocol whitelist is unchanged.

## Head CI

Run [42758130972](https://github.com/Maxencejules/shortvideo-network/actions/runs/42758130972) on `5ba8c476c83ac64e641382d8486dfb3d7791061f` completed **failure**. The `python` job failed in `test_browser_decodes_the_mp4_it_fetched` because deleting Chrome's profile raised `ENOTEMPTY`. The other 15 jobs succeeded. That run is not a pass.

Run [42759625636](https://github.com/Maxencejules/shortvideo-network/actions/runs/42759625636) on `2d9b615bebb8ca1e8fc97ec41228fda6450e17d5` completed **cancelled**. A later push superseded it. It is not a pass and not a failure result.

Run [42760172005](https://github.com/Maxencejules/shortvideo-network/actions/runs/42760172005) on `29e4be1516e5353c5fa5e14a3d0c2455f55c7038` completed **success** at 2026-11-10T05:18:26Z. All 16 jobs succeeded: `python`, `node`, `go`, `contract`, `devnet`, and the eleven `go-race` shards `app`, `devnet`, `devnet-evidence`, `devnet-upgrade`, `devnet-broadcaster`, `devnet-broadcaster-stall`, `devnet-indexer`, `broadcaster`, `indexer`, `api`, and `rest`. `29e4be1` differs from the tested commit `f81d819` only under `docs/evidence/`. The `python` job on this run includes the profile cleanup.

Run [42761558025](https://github.com/Maxencejules/shortvideo-network/actions/runs/42761558025) on head `4abfd6cab55a23e3ac8c4a3f54430c8f8f25a85f` completed **success** at 2026-11-10T05:33:53Z. All 16 jobs succeeded, the same set as the run above. `4abfd6c` differs from the tested commit only under `docs/evidence/`.

## Log stamps

`delivery-final.txt`, `python-final.txt`, `go-final.txt`, `mypy-final.txt`, `ruff-format-final.txt`, and `ruff-check-final.txt` were written while `HEAD` was `f81d819427fca3fd552a93f43001aa13a08b6ae4` and `TREE` was `89b4821191228bae97c71ac9f9511e7ce1448cb2`. Earlier logs in the same directory were written on `1255394077693248406ffe3910ad40c163a6dd36`. `DIRTY yes` is the untracked log directory. No tracked source file changed during a command.

## Residual risk

The worker still passes `-protocol_whitelist file,crypto,pipe` to FFmpeg. This goal does not change that. A chain note of `HELD` is an operator file, not a chain query. A wrong note would describe playback as current. The process still does not create a hosting promise.

## Next safe action

Draft [PR 20](https://github.com/Maxencejules/shortvideo-network/pull/20) is open. Leave it a draft. Do not merge it. Do not start goal 16 from an unreviewed branch. Public launch stays disabled.

## Proposed pull-request title and summary

Title: goal 15: media manifests and HTTPS playback

Summary: `services/delivery` serves HLS for a ready-for-review package on loopback HTTPS, checks hashes before advertising a host, and bounds retry, failover, and prefetch. A headless Chrome run decoded the one MP4 the test page fetched. Status stays BLOCKED on DR-15-01 through DR-15-06, the open goal 14 package decision, and the inherited custody, single-machine, anchor, lint, PostgreSQL 16, and compose blockers. Native HLS verification is not claimed.
