# Goal 22 evidence

Status: BLOCKED

The discovery service is `services/api/internal/social`. It keeps follows, likes, comments, private saves, activity, Not interested, search, a chronological Following feed, and a separate recommendation feed in memory. Ordinary social actions are not chain transactions. A follow or unfollow is stored as a signed public record only when the account presents a signature the service can verify. The service does not hold a signing key. Likes, comments, saves, Not interested, and activity are application rows. Reports and blocks return `MODERATION_DEFERRED`. There is no token, coin, sale, or mining. No video bytes and no personal data were written to a chain. This is not an audit and not a production network.

The recommendation order in the tests is a fixture result. It is not evidence that the ranking is better.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/22-social-features-search-and-feeds.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-eac05353-51b4-5bed-9700-4c8cfe9c35e0`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run. The playbook's GPT-6.1 Sol was replaced by this Cursor cloud agent on that default model.
- On 2026-10-31 the owner waived the model requirement as a blocker. On 2026-11-01 the owner extended that acknowledgement through goal 30. That covers model allocation only.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `29f82eb0fc24acc5e76099d066b6f70c3ee66062` |
| Tree | `d854aa495c2d625ae7025cc4f973b6721c04a187` |
| Base | `313cbc6658d4d7a960f61be7594a384f03a00518` |
| Branch | `goal-22` |

The tested commit message carries `Tested-tree: d854aa495c2d625ae7025cc4f973b6721c04a187`. `git rev-parse 29f82eb0fc24acc5e76099d066b6f70c3ee66062^{tree}` is that tree. Later commits change only `docs/evidence/`.

Earlier heads are not this tested commit. [Run 44063910995](https://github.com/Maxencejules/shortvideo-network/actions/runs/44063910995) failed mypy on the session token. [Run 44064972702](https://github.com/Maxencejules/shortvideo-network/actions/runs/44064972702) failed because the Python job does not install esbuild. [Run 44067966596](https://github.com/Maxencejules/shortvideo-network/actions/runs/44067966596) passed every job except `devnet`: with two validators stopped, one of the remaining nodes reported height 14 and then 15 while the other was already at 15, and the app hash did not change. Empty blocks keep the same app hash, so that was catch-up to a block already observed, not a new commit. The stall check now uses the maximum height (`scripts/devnet/prove-containers.sh:229`). [mypy.txt](logs/goal-22/mypy.txt) line 5 is `Success: no issues found in 1 source file` and line 6 is `EXIT:0`. [browser.txt](logs/goal-22/browser.txt) line 6 is `EXIT:0`.

## Captures

Every log under `docs/evidence/logs/goal-22/` was captured while `git status --porcelain` was empty. Each file's line 3 is `DIRTY no`. The commands ran from the repository root. A `cd` inside a command is the scope of that command.

`apps/web/dist/` is not ignored. The two browser commands created it. [browser.txt](logs/goal-22/browser.txt) line 8 and [viewer.txt](logs/goal-22/viewer.txt) line 8 are `?? apps/web/dist/`. That directory was removed after each command and was not committed. Every other log ends `AFTER clean`. No other untracked path remained.

## Head CI

[Run 44079161769](https://github.com/Maxencejules/shortvideo-network/actions/runs/44079161769) on `b65820cb9fdc9467f2f5506cfa46c14760ca5049` finished with conclusion `success`. All 17 jobs passed, including `python`, `node`, `go`, `contract`, `milestone`, `devnet`, and every `go-race` shard. [Run 44077041831](https://github.com/Maxencejules/shortvideo-network/actions/runs/44077041831) on `556f7aa81515b667da478ec20dc8ccd137863069` also finished `success` with those 17 jobs. This paragraph is added after `b65820c`.

## What ran

| Command | Scope | Log | Result |
| --- | --- | --- | --- |
| runtime versions | repository root | [runtime.txt](logs/goal-22/runtime.txt) | line 11 `EXIT:0` |
| `gofmt -l` on the new Go paths | `services/api/internal/social`, `services/api/cmd/svn-discovery`, `services/api/cmd/svn-feed-client` | [gofmt.txt](logs/goal-22/gofmt.txt) | line 5 `EXIT:0` |
| `go vet` | `services/api` module, those three packages | [govet.txt](logs/goal-22/govet.txt) | line 5 `EXIT:0` |
| `go test -count=1 -timeout 180s -v ./internal/social/` | `services/api` | [social.txt](logs/goal-22/social.txt) | line 31 `EXIT:0` |
| `go test -race -count=1 -timeout 180s ./internal/social/` | `services/api` | [race.txt](logs/goal-22/race.txt) | line 6 `EXIT:0` |
| `python3 scripts/run_tests.py --only node` | `apps/web` test files | [node.txt](logs/goal-22/node.txt) | line 381 `EXIT:0` |
| `npm run typecheck --workspace apps/web` | `apps/web` | [typecheck.txt](logs/goal-22/typecheck.txt) | line 9 `EXIT:0` |
| `python3 scripts/scan_secrets.py` | repository | [secrets.txt](logs/goal-22/secrets.txt) | line 5 `secret scan ok`, line 6 `EXIT:0` |
| `ruff check` and `ruff format --check` | the two browser tests and `apps/web` | [ruff.txt](logs/goal-22/ruff.txt) | line 7 `EXIT:0` |
| `python3 -m mypy tests/unit/test_social_browser.py` | that test file | [mypy.txt](logs/goal-22/mypy.txt) | line 6 `EXIT:0` |
| discovery browser | `tests/unit/test_social_browser.py` against `cmd/svn-discovery` | [browser.txt](logs/goal-22/browser.txt) | line 6 `EXIT:0` |
| existing viewer browser | `tests/unit/test_web_viewer.py` | [viewer.txt](logs/goal-22/viewer.txt) | line 6 `EXIT:0` |
| PostgreSQL binaries | `/usr/lib/postgresql/16` and `/usr/lib/postgresql/18` | [pg16.txt](logs/goal-22/pg16.txt) | line 5 `pg16 absent`, line 6 `pg18 absent`, line 7 `EXIT:1` |
| `docker version` | host | [docker.txt](logs/goal-22/docker.txt) | line 5 `docker: command not found`, line 6 `EXIT:127` |
| `golangci-lint version` | host | [golangci-lint.txt](logs/goal-22/golangci-lint.txt) | line 5 `golangci-lint: command not found`, line 6 `EXIT:127` |

[runtime.txt](logs/goal-22/runtime.txt) line 5 is `v22.23.4`. Line 6 is `10.9.9`. Line 7 is `Python 3.12.3`. Line 8 begins with `go version go1.26.10`. Line 9 begins with `Google Chrome 157.0.8103.50`. Line 10 is `Linux 6.12.113+ x86_64`.

[social.txt](logs/goal-22/social.txt) lines 6 through 28 are the twelve passing tests. Line 29 is `PASS`. The race log is the same package ([race.txt](logs/goal-22/race.txt) line 5).

[node.txt](logs/goal-22/node.txt) pass counts are line 93 `14`, line 181 `13`, line 251 `10`, line 279 `3`, line 295 `1`, line 317 `2`, and line 375 `8`. Each following line is `# fail 0`. The suite total is 51 passing tests.

The `services/api` packages that start PostgreSQL through `pgdev` were not run. PostgreSQL is not installed here. That is BLOCKED, not a pass.

## Browser

Headless Chrome on this VM. No screenshot was taken. This is not visual design approval.

[browser.txt](logs/goal-22/browser.txt) line 5 is the pytest pass line. The test builds `services/api/cmd/svn-discovery` and `apps/web`, starts the discovery process with `-dev` on loopback, and drives the built viewer. The process is not a Python stand-in for discovery. No passkey ceremony ran. Line 3 is `DIRTY no`. Line 8 is `?? apps/web/dist/`.

[viewer.txt](logs/goal-22/viewer.txt) line 5 is the existing playback test, rerun after the follow copy changed. Line 8 is `?? apps/web/dist/`.

## Acceptance

| Check | Status | Where |
| --- | --- | --- |
| Follow-graph permissions | VERIFIED | [social.txt](logs/goal-22/social.txt) line 6. Self-follow and a missing bearer are refused. `services/api/internal/social/social_test.go:128`. |
| Duplicate likes | VERIFIED | Same log line. Twenty concurrent likes leave one like id. |
| Idempotent follow retry | VERIFIED | Same test. A repeated body with the same key returns the stored response. A different body with that key conflicts. |
| Pagination under concurrent updates | VERIFIED | [social.txt](logs/goal-22/social.txt) line 10. `services/api/internal/social/social_test.go:269`. |
| Comment moderation boundary | VERIFIED for local refusal and author deletion | [social.txt](logs/goal-22/social.txt) line 8. Markup and a disallowed phrase are refused without echoing the phrase. Another account cannot delete the comment. `services/api/internal/social/social_test.go:208`. |
| Moderator queue, reports, blocks | BLOCKED for Goal 23 | `POST /v1/reports` and `POST /v1/blocks` return `MODERATION_DEFERRED`. `services/api/internal/social/http.go:556`. The report body is not stored. |
| Private saves and notification access | VERIFIED | Another account's save list is empty. A profile response does not contain the saved id. Activity for the content creator contains the reply; another account's activity does not. Deleting the comment removes the reply. `services/api/internal/social/social_test.go:128` and `services/api/internal/social/social_test.go:208`. |
| Rebuild and coverage | VERIFIED | [social.txt](logs/goal-22/social.txt) line 16. Permitted and indexed counts match, and a withdrawn id is absent. `services/api/internal/social/feed.go:288`. `services/api/internal/social/social_test.go:419`. |
| Chronological order and filtering, including a cached page | VERIFIED | [social.txt](logs/goal-22/social.txt) line 12. Withdrawal and a hidden creator drop out. The stored page still contains the withdrawn id; the fresh response does not and is marked refreshed. `services/api/internal/social/social_test.go:321`. |
| Recommendation fixture | VERIFIED as a fixture, not as a quality result | [social.txt](logs/goal-22/social.txt) line 14. Factors are `services/api/internal/social/service.go:344`. The test name records the limit. `services/api/internal/social/social_test.go:366`. |
| Second client and stable content ids | VERIFIED | [social.txt](logs/goal-22/social.txt) line 22. `services/api/cmd/svn-feed-client` and `FollowingIDs` read the same ids. A public host is refused. `services/api/internal/social/social_test.go:532`. |
| Signed public follow | VERIFIED for a caller-supplied signature | [social.txt](logs/goal-22/social.txt) line 18. The canonical text matches the Goal 02 profile (`services/api/internal/social/social_test.go:568`). An unsigned follow is not in the public follow list. A post record is refused. `services/api/internal/social/social_test.go:453`. |
| Account deletion | VERIFIED for private rows | `history_erased` is false, `decision_id` is `DEC-004`, `value_version` is 0, and the public record remains. The bearer no longer works. `services/api/internal/social/actions.go:721`. |
| Viewer wired to the discovery process | VERIFIED | [browser.txt](logs/goal-22/browser.txt) line 6. Follow, a plain comment, refusal of markup, search, and the still-disabled block and report controls. |
| Rate limit | VERIFIED that the refusal body does not contain a digit | [social.txt](logs/goal-22/social.txt) line 20. The allowance stays in `services/api/internal/social/service.go:19` and is not in the discovery document. |
| No chain import in this package | VERIFIED | [social.txt](logs/goal-22/social.txt) line 28. `services/api/internal/social/social_test.go:624`. |
| ActivityPub or AT Protocol | Not claimed | `services/api/internal/social/discovery-v1.json:6` is false. There is no adapter and no conformance suite. |
| PostgreSQL persistence | BLOCKED | [pg16.txt](logs/goal-22/pg16.txt) line 5 and line 6. Restart loses the in-memory rows. Rebuild compares the in-memory catalog only. |
| Passkey session on this service | BLOCKED | Discovery bearers are issued by this process. They are not `svn-api` passkey sessions. `svn-api` was not started. |
| `services/api` upload tests | BLOCKED | Those tests start PostgreSQL in `TestMain`. PostgreSQL is absent, so they were not run. |
| golangci-lint | BLOCKED | [golangci-lint.txt](logs/goal-22/golangci-lint.txt) line 6 `EXIT:127`. |
| Compose | BLOCKED | [docker.txt](logs/goal-22/docker.txt) line 6 `EXIT:127`. The daemon on port 2375 was not used. |
| Visual design approval | Not claimed | No screenshot. |

## Remaining feature gates

Block and report stay disabled (`apps/web/src/ui.ts:301`). The comment note says a moderator queue is not implemented (`apps/web/src/ui.ts:299`). The creator review sentence still says Goal 23 moderation is not implemented (`apps/web/src/creator.ts:21`). Peer sharing stays off. Recovery stays unavailable. Public launch stays disabled. There is no wallet control.

Follow, like, Not interested, comments, search, activity, and the two feeds are wired when `discoveryBase` is set (`apps/web/src/model.ts:149`, `apps/web/src/main.ts:164`). A follow is described as an account record and not a chain transaction (`apps/web/src/ui.ts:280`).

## Goal 23 handoff

Goal 23 owns reports, account blocks, the moderator queue, comment removal by a moderator, and cache invalidation across services. This goal's handoff points are:

- `POST /v1/reports` and `POST /v1/blocks` (`services/api/internal/social/http.go:556`). They do not store a case.
- `Service.Hide` and `Service.HideCreator` (`services/api/internal/social/service.go:213` and `services/api/internal/social/service.go:222`). They are in-process only. A caller cannot set them over HTTP.
- The disallowed-comment set is not returned by any route.
- Feed responses carry `X-SVN-Cache-Generation`. A withdrawal bumps the generation and drops the cached page (`services/api/internal/social/feed.go:15`).
- `discovery-v1.json` lists those paths under `goal_23` (`services/api/internal/social/discovery-v1.json:23`).

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-22-01 | OPEN, BLOCKED | DEC-001. This process does not hold a signing key. An application follow is stored for the account and is not a public record. A signed follow is accepted only when the account registered a public key and the envelope verifies. |
| DR-22-02 | OPEN | The public-record schema allows post, follow, and unfollow. This service accepts only follow and unfollow. Comments, likes, saves, Not interested, and activity are not public records. |
| DR-22-03 | OPEN, BLOCKED | DEC-004 sets no retention period. Account deletion removes private rows, sets `history_erased` false, and keeps signed public records. No duration is claimed. |
| DR-22-04 | OPEN, BLOCKED | DEC-007 audience, age, and privacy review stay unset. There is no public sign-up. Dev sessions exist only when the loopback process is started with `-dev`. |
| DR-22-05 | OPEN, BLOCKED | PostgreSQL is absent. Social rows are in memory and are lost on restart. The account schema is unchanged. |
| DR-22-06 | OPEN, BLOCKED | A discovery bearer is not an `svn-api` passkey session. The two processes were not bound. |
| DR-22-07 | OPEN | No ActivityPub or AT Protocol adapter is implemented. The discovery document says both are false. |
| DR-22-08 | OPEN | Goal 23 owns moderator actions and cross-service cache purge. The generation header is the cache handoff. |
| DR-22-09 | OPEN | The recommendation order is a fixture. It is not a claim of better recommendations. |
| DR-22-10 | OPEN | Raw viewing history is not stored. Saves and hidden accounts are not public records and are not chain events. Playback locations on a feed item must be loopback URLs. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody and recovery stay unimplemented. DR-22-01 and DR-22-06.
2. **DEC-004 and DEC-007.** No retention period and no audience. DR-22-03 and DR-22-04.
3. **One machine.** The browser and the discovery process are one VM.
4. **Goal 06 and goal 07 mutation runners.** Not re-run.
5. **golangci-lint.** Not installed.
6. **PostgreSQL 16.** Not installed. PostgreSQL 18 is not installed either.
7. **Goal 04 compose bridge.** Docker is not installed. Compose was not run.

## Requirements

The requirements matrix was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-UX-001 | The viewer offers Following and Recommended. The order and the filters were tested. |
| REQ-UX-002 | Search returns profiles, topics, and videos and hides a withdrawn video. |
| REQ-UX-004 | Activity lists a reply for the content creator and hides it from another account. |
| REQ-UX-007 | Saves, Not interested, and hidden accounts stay off the public profile and off public records. |
| REQ-UX-015 | The discovery document does not claim ActivityPub or AT Protocol. |
| REQ-UX-016 | Follow is wired. Block and report stay gated for Goal 23. |
| REQ-BOUND-002 | Social actions stay off chain. The package test refuses a chain import. |
| REQ-OFF-005 | The feed document has `chain_events` false (`services/api/internal/social/service.go:169`). No watch route exists. |
| REQ-FEED-001 | Feed items carry stable content ids, title, creator id, and optional loopback playback URLs. Rebuild compares coverage. |
| REQ-FEED-002 | Ranking factors are explicit. Blocks, withdrawals, unavailable items, hidden accounts, Not interested, and a creator cap are applied. The order is a fixture. |
| REQ-API-001 | `GET /v1/feeds/following` and `GET /v1/feeds/recommended` are implemented on this service. Reports stay deferred. |
| REQ-SEC-009 | Comment text is rejected when it contains markup. It is rendered as text (`apps/web/src/ui.ts:300`). |
| REQ-SEC-010 | Search and writes are rate-limited. The refusal does not publish the allowance. |

## Next safe action

Do not describe the in-memory process as a PostgreSQL store or as `svn-api`. Do not describe the fixture order as a better recommender. Goal 23 still owns reports, blocks, and the moderator queue. DEC-001 still blocks host signing. DEC-004 and DEC-007 still block a retention or audience claim.
