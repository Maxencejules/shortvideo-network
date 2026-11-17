# Goal 21 evidence

Status: BLOCKED

The creator page is in `apps/web`. It selects a file, checks a trim range, keeps a title, description, and captions in the browser, and shows Uploading, Processing, Review pending, Replicating, and Published as separate states. Published is shown only when a scheduler read says `PUBLISHED`, community approval is true, the indexer read is not stale, and any chain operation that was supplied is committed. A local flag does not publish. `POST /publish` is still the upload service's refusal. Recovery is shown as unavailable. There is no token, coin, sale, or mining. No video bytes and no personal data were written to the chain. This is not an audit, not a visual design approval, and not a production network.

The browser run walked those states against loopback stand-ins. The stand-ins are not `svn-api`, not `svn-indexer`, and not `svn-scheduler`. No passkey ceremony ran. PostgreSQL is not installed here, so the upload service's own tests were not started.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/21-creator-publishing-interface.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-a7953efc-7e53-5c8c-b4eb-1eeed02eb27a`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run. The playbook's GPT-6.1 Sol was replaced by this Cursor cloud agent on that default model.
- On 2026-10-31 the owner waived the model requirement as a blocker. On 2026-11-01 the owner extended that acknowledgement through goal 30. That covers model allocation only.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `c3ef99b8b503729b3b98393381689d3ef897e242` |
| Tree | `8b462c5f92a80f042deea093aa0473716689ed49` |
| Base | `fa6817cbee38eb54cfc9009a36fbdf20a21bc16c` |
| Branch | `goal-21` |

The tested commit message carries `Tested-tree: 8b462c5f92a80f042deea093aa0473716689ed49`. `git rev-parse c3ef99b8b503729b3b98393381689d3ef897e242^{tree}` is that tree. The evidence commit `98c21be277095e13a938b686a28a20c366efd824` changes only `docs/evidence/` relative to that tested commit.

## Head CI

[Run 43999956488](https://github.com/Maxencejules/shortvideo-network/actions/runs/43999956488) on `98c21be277095e13a938b686a28a20c366efd824` finished with conclusion `success`. All 17 jobs passed, including `python`, `node`, `go`, `contract`, `milestone`, `devnet`, and every `go-race` shard. A green run is not visual approval. `98c21be` differs from the tested commit only under `docs/evidence/`.

`publishedNow` requires `publication === "PUBLISHED"`, `community_approval === true`, a fresh indexer read, and a committed chain operation when one was supplied (`apps/web/src/creator.ts:436`). A local flag is ignored (`apps/web/src/main.ts:280` passes `optimisticPublished: false`; `apps/web/src/creator.ts:346` records that a true flag is not publication). The Published sentence does not say the chain certified the video (`apps/web/src/creator.ts:19`). Review pending says Goal 23 is not implemented (`apps/web/src/creator.ts:21`). Removal is not sent (`apps/web/src/creator.ts:580` through `apps/web/src/creator.ts:583`). A shorter trim is refused (`apps/web/src/creator.ts:219`). The owner-only upload JSON includes `content_id` (`services/api/internal/service/http.go:364` and `services/api/internal/service/http.go:407`). The Go assertion that the id is present was not run here (`services/api/internal/service/http_test.go:277`).

## What ran

| Command | Log | Result |
| --- | --- | --- |
| `npm run typecheck --workspace apps/web` | [typecheck.txt](logs/goal-21/typecheck.txt) | line 8 `EXIT:0` |
| `python3 scripts/run_tests.py --only node` | [node.txt](logs/goal-21/node.txt) | line 358 `EXIT:0` |
| creator browser stand-in | [browser.txt](logs/goal-21/browser.txt) | line 6 `EXIT:0` |
| existing viewer browser test | [viewer.txt](logs/goal-21/viewer.txt) | line 6 `EXIT:0` |
| `python3 scripts/scan_secrets.py` | [secrets.txt](logs/goal-21/secrets.txt) | line 4 `secret scan ok`, line 5 `EXIT:0` |
| `ruff check` on the new Python test | [ruff.txt](logs/goal-21/ruff.txt) | line 5 `EXIT:0` |
| `gofmt -l` on the two Go files | [gofmt.txt](logs/goal-21/gofmt.txt) | line 4 `EXIT:0` |

[node.txt](logs/goal-21/node.txt) line 92 is `# pass 14` and line 93 is `# fail 0` for the creator file. Line 352 is `# pass 8` and line 353 is `# fail 0` for the component file. The suite total is 49 passing tests: 14 creator, 13 evidence-label, 10 model, 3 playback, 1 React pin, and 8 component tests.

[runtime.txt](logs/goal-21/runtime.txt) line 4 is `v22.23.4`. Line 5 is `10.9.9`. Line 6 is `Python 3.12.3`. Line 7 begins with `Google Chrome 157.0.8103.50`. Line 8 begins with `ffmpeg version 6.1.1-3ubuntu5`. Line 9 is `Linux 6.12.113+ x86_64`. Line 10 is `EXIT:0`.

## Browser and device

Headless Chrome on this VM. No screenshot was taken. This is not visual design approval. A successful web build is not this check. The creator check is [browser.txt](logs/goal-21/browser.txt) line 6 `EXIT:0`.

[browser.txt](logs/goal-21/browser.txt) line 4 records `Chrome/157.0.8103.50`, the page user agent `HeadlessChrome/157.0.0.0`, and the viewport at the end of the run: width 1280, height 713, device pixel ratio 1. Line 4 also records `account` `stand-in`, `indexer` `stand-in`, `scheduler` `stand-in`, `upload` `stand-in`, `intents` 2, `puts` 1, and `stage` `Cancelled`.

That process is `tests/unit/test_creator_browser.py`. The trim refusal is line 60. The second intent is line 73. Review pending is line 81. The lawful-or-truthful sentence is line 100. The participating host is line 126. `PUBLICATION_BLOCKED` is line 130. The stand-in buffer is empty after cancel at line 137. Those asserts are covered by [browser.txt](logs/goal-21/browser.txt) line 6.

The same Chrome binary ran the existing viewer test again ([viewer.txt](logs/goal-21/viewer.txt) line 6). That run is playback of a processed package, not a creator upload. Its ending viewport is 390 by 844, device pixel ratio 2 ([viewer.txt](logs/goal-21/viewer.txt) line 4).

## Acceptance

| Check | Status | Where |
| --- | --- | --- |
| Upload selection and a typed intent | VERIFIED against a stand-in and a fake transport | [browser.txt](logs/goal-21/browser.txt) line 4 `intents` 2 and `puts` 1. [node.txt](logs/goal-21/node.txt) line 92. |
| Illegal trim, shorter trim, captions checked and not sent | VERIFIED in unit tests and, for the illegal range, in the browser | `apps/web/test/creator.test.ts:37`, `apps/web/test/creator.test.ts:50`, `apps/web/test/creator.test.ts:233`. Browser trim code `tests/unit/test_creator_browser.py:60`. |
| Title, description, thumbnail choice | IMPLEMENTED as browser-only fields | The upload API has no field for them. `apps/web/src/creator.ts:31` and `apps/web/src/ui.ts:481`. |
| Five lifecycle states, including Published only from the read | VERIFIED in unit tests; the browser stand-in walked each label | `apps/web/test/creator.test.ts:172`. Browser review, replicating, published, and cancelled asserts are in `tests/unit/test_creator_browser.py`. |
| Failures, stall, degraded availability, renewal | VERIFIED in unit tests; the browser stand-in showed stall, degraded, and renewal text | `apps/web/test/creator.test.ts:90` and `apps/web/test/creator.test.ts:172`. |
| Cancel, retry, duplicate submission, ownership | VERIFIED in unit tests | `apps/web/test/creator.test.ts:59`, `apps/web/test/creator.test.ts:75`, `apps/web/test/creator.test.ts:110`, `apps/web/test/creator.test.ts:125`. The browser refused the other account's cancel button and cancelled once for the owner. |
| Immutable bytes for one upload | VERIFIED in unit tests | `apps/web/test/creator.test.ts:217`. |
| Publication intent does not show Published | VERIFIED | `apps/web/src/creator.ts:571`. Browser `tests/unit/test_creator_browser.py:130`. |
| Removal does not claim universal deletion | VERIFIED for the copy | `apps/web/src/creator.ts:23` and `apps/web/src/creator.ts:25`. Browser `tests/unit/test_creator_browser.py:126`. The request is not sent. |
| Recovery unavailable, export link, proposed terms | VERIFIED for the copy | `apps/web/src/creator.ts:17`. `apps/web/src/ui.ts:487`. |
| Analytics are the browser counters only | VERIFIED | `apps/web/test/ui.test.ts` create-page test. No view count and no revenue figure is invented. |
| Real `svn-api` upload, quota, and quarantine leak check | BLOCKED | PostgreSQL is absent. [pg16.txt](logs/goal-21/pg16.txt) line 4 is `pg16 absent` and line 5 is `pg18 absent`. |
| Creator disconnect, origin down, later playback | BLOCKED | This run did not publish a package and then stop the origin. [viewer.txt](logs/goal-21/viewer.txt) is the existing playback failover, not that scenario. |
| Committed-chain publication | BLOCKED | The browser scheduler body was a stand-in. No chain process ran. |
| Passkey login in the browser | BLOCKED | DR-19-06 and PostgreSQL. The stand-in returned a session without a ceremony. |
| Visual design approval | Not claimed | No screenshot. |

## Goal 23

Review pending is not a moderation decision. `apps/web/src/creator.ts:21` says Goal 23 moderation is not implemented and that this screen cannot complete review. The creator page does not approve, block, report, or comment. Those controls stay on the viewer gates from goal 20. A package can be labeled Published only when the read already says the approval and hosting conditions hold. This screen cannot grant that approval.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-21-01 | OPEN | There is no creator removal route. The button records the request in this browser and does not send it. Withdrawal on the indexer, when present, is shown with any active host still listed. |
| DR-21-02 | OPEN | A shorter trim is refused. This client does not rewrite media bytes. The upload service ignores `client_duration_seconds` and stores the body. |
| DR-21-03 | OPEN | Title, description, and captions stay in this browser. The upload API does not store them. Caption text is not placed on a command line. |
| DR-21-04 | OPEN | The worker writes one JPEG. Choosing another frame is disabled. |
| DR-21-05 | OPEN, BLOCKED | The browser did not call `svn-api`, `svn-indexer`, or `svn-scheduler`. PostgreSQL is absent, so those processes were not started. |
| DR-21-06 | OPEN, BLOCKED | No committed chain was read. The Published label in the browser came from a stand-in scheduler body. |
| DR-21-07 | OPEN | DEC-006 leaves the creator licence unset. DEC-005 leaves allowances unset. The page labels the caps PROPOSED and states they are not a storage guarantee. |
| DR-21-08 | OPEN | No screenshot and no visual design approval. |
| DR-19-02 | OPEN, BLOCKED | Carried. No host-held user signing key. |
| DR-19-03 | OPEN, BLOCKED | Carried. Recovery is shown as unavailable. |
| DR-19-06 | OPEN | Carried. Relying party origin `http://localhost`. |
| DR-20-03 | OPEN, BLOCKED | Carried. No passkey ceremony in the browser. |
| DR-13-01 .. DR-13-04 | OPEN | Carried. The upload contract, storage adapter, allowances, and probe approval stay open. |
| DR-14-01 .. DR-14-04 | OPEN | Carried. The worker does not publish. |
| DR-15-01 .. DR-15-06 | OPEN | Carried. A playback grant is not publication. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody and recovery stay unimplemented. The page says recovery is not available.
2. **One machine.** The browser stand-ins are one process on this VM.
3. **Goal 06 and goal 07 mutation runners.** Not re-run.
4. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-21/golangci-lint.txt) line 4 is `--: line 12: golangci-lint: command not found` and line 5 is `EXIT:127`.
5. **PostgreSQL 16.** Not installed. [pg16.txt](logs/goal-21/pg16.txt) line 4 is `pg16 absent`, line 5 is `pg18 absent`, and line 6 is `EXIT:1`. The API test that checks `content_id` did not run.
6. **Goal 04 compose bridge.** [docker.txt](logs/goal-21/docker.txt) line 4 is `--: line 5: docker: command not found` and line 5 is `EXIT:127`. Compose was not run. The unauthenticated :2375 daemon was not used.

## Requirements

The requirements matrix was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-UX-003 | Upload, trim bounds, captions, and a publication status are in the creator page. Byte trimming is DR-21-02. |
| REQ-UX-005 | The profile lists drafts stored in this browser and the stage of the open draft. A stored stage is not shown for a draft that was not read. |
| REQ-UX-008 | The five stage names are separate. The current one is `#creator-stage` (`apps/web/src/ui.ts:445`). |
| REQ-UX-009 | The Published sentence is `apps/web/src/creator.ts:19`. |
| REQ-UX-010 | Availability degraded and Hosting renewal needed come from the scheduler fields `at_risk`, `reachability`, `degraded_providers`, and `renewal`. |
| REQ-UX-017 | Upload and status are implemented. The removal control does not send a request (DR-21-01). |
| REQ-PEND-001 | Requested, submitted, committed, failed, and pending are separate chain phases. A stalled indexer read does not become Published (`apps/web/src/creator.ts:437`). |
| REQ-SCOPE-004 | BLOCKED on DEC-006. The page says the licence text is unset. |
| REQ-MOD-007 | BLOCKED on DEC-006. No licence grant is shown. |

## Next safe action

Do not treat the stand-in upload, indexer, or scheduler as `svn-api`, `svn-indexer`, or `svn-scheduler`. Do not describe the headless run as visual approval. Goal 23 still owns moderation completion and a real removal route. DEC-001 still blocks recovery.
