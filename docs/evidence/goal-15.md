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
| Commit | `1255394077693248406ffe3910ad40c163a6dd36` |
| Tree | `1e71eedb05330765c0da519af9f08cab5bd7af76` |
| Base | `fc8b79da5f6a96f8dfad4aebb4bf74a4a38d82f0` (goal 14 merge on `main`). |
| Branch | `goal-15` |
| Draft PR | [PR 20](https://github.com/Maxencejules/shortvideo-network/pull/20) |

`git rev-parse 1255394077693248406ffe3910ad40c163a6dd36^{tree}` is `1e71eedb05330765c0da519af9f08cab5bd7af76`. The commit message carries the same `Tested-tree` trailer. This evidence commit changes only `docs/evidence/` relative to that tested commit.

[runtime.txt](logs/goal-15/runtime.txt) has Linux 6.12.113+, Python 3.12.3, Go 1.26.10, FFmpeg `6.1.1-3ubuntu5`, Google Chrome `156.0.8078.47`, `nproc` printing `4`, and uid 1000. PostgreSQL 16 is absent in that log. PostgreSQL 18.6 was installed later, before `python-tests-rerun.txt` and `go-suite-rerun.txt`. The install is the same pin CI uses, `18.6-1.pgdg24.04+2`. It is not PostgreSQL 16.

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
| Test page, headless Chrome 156 | The page fetches one rendition playlist, hashes the first media segment with `crypto.subtle`, and decodes that MP4 in a `video` element. The test requires matching width and height, `readyState` at least 2, a non-zero canvas sample, and the caption file starting with `WEBVTT`. | VERIFIED for that one MP4. [delivery-verbose.txt](logs/goal-15/delivery-verbose.txt) `test_browser_decodes_the_mp4_it_fetched` PASSED. |
| Native HLS demuxer | Not claimed | Not run. Chromium on Linux has no HLS demuxer. Segments the page did not fetch were not client-verified. |

## Acceptance

| Check | Status | Log |
| --- | --- | --- |
| Real processed media, playlist and segment references, orientation, captions, fallback rendition | VERIFIED | [delivery-verbose.txt](logs/goal-15/delivery-verbose.txt) `test_processed_media_plays_over_https`. Portrait output is 144x256. Rotated output is 180x320. Captions contain `portrait-cue`. Requesting `h720` selects `native`. |
| Corrupt manifest and corrupt segment detected before a verified label | VERIFIED | same log, `test_corrupt_manifest_and_segment_are_not_advertised` |
| Harness detects a segment whose bytes do not match the advertised hash | VERIFIED | same log, `test_harness_bounds_failures_and_prefetch` |
| Host failure, 404, slow response, bounded attempts | VERIFIED | `test_replica_host_failure_and_chain_outage` and `test_harness_bounds_failures_and_prefetch`. Six hosts and two attempts produce 8 attempts. Hosts 5 and 6 are not contacted. |
| Revoked content, conditional request is not a 304, quarantine bytes are not served | VERIFIED | `test_range_cors_quarantine_and_removal` |
| Abandoned prefetch and no media download of the unselected queue | VERIFIED | `test_harness_bounds_failures_and_prefetch`. One of five items is prefetched. `delivered_bytes` does not increase. No `.mp4` request is recorded for that prefetch. |
| Chain note down does not create a grant or a new host; an existing term still plays and `hosting_promise` stays false | VERIFIED | `test_replica_host_failure_and_chain_outage` |
| Headless decode of the fetched MP4 | VERIFIED on this VM | `test_browser_decodes_the_mp4_it_fetched` |
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

The step table is [summary.tsv](logs/goal-15/summary.tsv). Every log starts with `HEAD`, `TREE`, and `DIRTY yes` because `docs/evidence/logs/goal-15/` was already untracked. `git diff` against the tested commit had no tracked change while these commands ran.

| Step | Exit | Note |
| --- | --- | --- |
| ruff-format, ruff-check | 0 | `services workers/media packages/protocol tests scripts` |
| mypy | 0 | |
| python-tests | 0 | First run, before PostgreSQL 18 was installed: 427 passed, 2 skipped. The extra skip is the media PostgreSQL test. [python-tests.txt](logs/goal-15/python-tests.txt). This is not the cited Python result. |
| python-tests-rerun | 0 | After PostgreSQL 18.6 was installed. 428 passed, 1 skipped, in 101.50s. The skip is `test_sixty_second_input_timing`. [python-tests-rerun.txt](logs/goal-15/python-tests-rerun.txt). This is the cited Python result. |
| delivery-verbose | 0 | 9 passed. Names are in [delivery-verbose.txt](logs/goal-15/delivery-verbose.txt). |
| workflow, secrets, sbom, requirements | 0 | |
| protocol | 1 | `protoc well-known types BLOCKED`. Not a pass. [protocol.txt](logs/goal-15/protocol.txt). |
| protocol-rerun | 0 | After `scripts/install_toolchains.sh --protoc-only`. `result PASS`. [protocol-rerun.txt](logs/goal-15/protocol-rerun.txt). |
| gofmt | 0 | |
| gen-proto | 127 | `protoc` was not on `PATH`. Not a pass. [gen-proto.txt](logs/goal-15/gen-proto.txt). |
| gen-proto-rerun | 0 | Pinned protoc 36.2. `git diff --exit-code -- proto/svn proto/gen/python` was clean. [gen-proto-rerun.txt](logs/goal-15/gen-proto-rerun.txt). |
| go-suite | 1 | PostgreSQL 18 was not installed yet. Broadcaster tests failed on the missing binary. Not a pass. [go-suite.txt](logs/goal-15/go-suite.txt). |
| go-suite-rerun | 0 | `SVN_SKIP_DEVNET=1`. 71 `ok` lines. [go-suite-rerun.txt](logs/goal-15/go-suite-rerun.txt). This does not run the devnet tests. Those stay on the CI race shards. |
| anchors06, anchors07 | 1 | BLOCKED. Not passes. |
| golangci-lint, docker | 127 | BLOCKED. Not passes. |
| pg16 | 1 | BLOCKED. The binary is absent. Not a pass. |

## Handoff

`python -m svn_delivery serve --root <worker-root>` listens on loopback with an ephemeral certificate. It serves no bytes until `note-chain --note HELD` and `authorize`. The API reports `publication: NOT_PUBLISHED` and `hosting_promise: false`. Quarantine is not served. The rules are in [delivery.md](../architecture/delivery.md). Goal 16 is the independent provider. The worker's FFmpeg `file` protocol whitelist is unchanged.

## Head CI

Not observed when this file was written. The run that checks the commit which adds this file is recorded in the next evidence note if it is green, and in the handoff after it is checked. A run that was not seen is not a pass.

## Log stamps

The command logs in `logs/goal-15/` were written while `HEAD` was `1255394077693248406ffe3910ad40c163a6dd36` and `TREE` was `1e71eedb05330765c0da519af9f08cab5bd7af76`. `DIRTY yes` is the untracked log directory. No tracked source file changed between those commands.

## Residual risk

The worker still passes `-protocol_whitelist file,crypto,pipe` to FFmpeg. This goal does not change that. A chain note of `HELD` is an operator file, not a chain query. A wrong note would describe playback as current. The process still does not create a hosting promise.

## Next safe action

Draft [PR 20](https://github.com/Maxencejules/shortvideo-network/pull/20) is open. Leave it a draft. Do not merge it. Do not start goal 16 from an unreviewed branch. Public launch stays disabled.

## Proposed pull-request title and summary

Title: goal 15: media manifests and HTTPS playback

Summary: `services/delivery` serves HLS for a ready-for-review package on loopback HTTPS, checks hashes before advertising a host, and bounds retry, failover, and prefetch. A headless Chrome run decoded the one MP4 the test page fetched. Status stays BLOCKED on DR-15-01 through DR-15-06, the open goal 14 package decision, and the inherited custody, single-machine, anchor, lint, PostgreSQL 16, and compose blockers. Native HLS verification is not claimed.
