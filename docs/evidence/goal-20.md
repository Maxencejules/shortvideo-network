# Goal 20 evidence

Status: BLOCKED

The invited viewer is `apps/web`. It plays a processed package from `services/delivery`, with bounded prefetch, host failover, captions, mute, keyboard navigation, reduced motion, and a responsive layout. Follow, block, report, comments, search, and activity are visible and do not report success. There is no wallet and no peer upload. Public launch stays disabled. There is no token, coin, sale, or mining. No video bytes and no personal data were written to the chain. This is not an audit, not a visual design approval, and not a production network.

The browser run decoded one processed portrait package in headless Chrome and played it again after the first host stopped listening. The account listener and the indexer listener in that run are loopback stand-ins. They are not `svn-api` and not `svn-indexer`. Native HLS verification is not claimed. Chromium on Linux has no HLS demuxer. The page gives the video element one fetched MP4.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/20-viewer-web-application.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-d609f8e9-f483-58b3-b256-181c60941a6a`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run. The playbook's GPT-6.1 Sol was replaced by this Cursor cloud agent on that default model.
- On 2026-10-31 the owner waived the model requirement as a blocker. On 2026-11-01 the owner extended that acknowledgement through goal 30. That covers model allocation only.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `1c320e35282561e7a7bd9b4670b0360d8dcc4441` |
| Tree | `7b5c5e8d7a506fbe8b7b3bb1aba412142516fabb` |
| Base | `5d64047d9d532f9c0bba709b0f9f61da4724b2fa` |
| Branch | `goal-20` |

The tested commit message carries `Tested-tree: 7b5c5e8d7a506fbe8b7b3bb1aba412142516fabb`. `git rev-parse 1c320e35282561e7a7bd9b4670b0360d8dcc4441^{tree}` is that tree. `run_until` returns `LAUNCH_FAILED` when the process never starts (`services/delivery/svn_delivery/browser.py` line 182). It returns `EXITED` only after a start, when that process ends before a report (line 189). It returns `TIMEOUT` when the deadline passes with no report (line 191). The decode test waits 60 seconds (`tests/unit/test_delivery.py` line 724). It retries only `EXITED` (lines 728 through 733). A failed launch, a timeout, or any other first result is not launched again (lines 734 and 735). A missing binary is `LAUNCH_FAILED` (`tests/unit/test_delivery.py` line 695). A process that starts and ends is `EXITED` (line 703). [decode.txt](logs/goal-20/decode.txt) line 5 is `decode-retry no status=REPORTED`. Line 7 is `EXIT:0`. This run did not retry. That line is the record of whether a retry fired. [launch.txt](logs/goal-20/launch.txt) line 6 is `EXIT:0`.

[ruff.txt](logs/goal-20/ruff.txt) line 1 is `HEAD 1c320e35282561e7a7bd9b4670b0360d8dcc4441`. Line 2 is `TREE 7b5c5e8d7a506fbe8b7b3bb1aba412142516fabb`. Line 3 is `DIRTY no`. Line 5 is `3 files already formatted`. Line 7 is `EXIT:0`. [corpus.txt](logs/goal-20/corpus.txt) line 1 is `HEAD 894aa0c4345fc3d77c1f5338356202e9bc6f4b0c`. Line 6 is `EXIT:0`. That corpus run is the commit that stopped ignoring `apps/web/dist`. This tested commit does not add that ignore back.

[decode.txt](logs/goal-20/decode.txt), [launch.txt](logs/goal-20/launch.txt), and [ruff.txt](logs/goal-20/ruff.txt) were captured on this tested commit. [typecheck.txt](logs/goal-20/typecheck.txt), [node.txt](logs/goal-20/node.txt), [browser.txt](logs/goal-20/browser.txt), and [secrets.txt](logs/goal-20/secrets.txt) were captured on `797d85626447ad00e40d35b0039548dfeeea451c`. This tested commit does not change those files. [workflow.txt](logs/goal-20/workflow.txt) was captured on `4680eff106f98bc44a6b8ab98d18e1cd2000601e`. This tested commit does not change the workflow. [sbom.txt](logs/goal-20/sbom.txt), [runtime.txt](logs/goal-20/runtime.txt), [pg16.txt](logs/goal-20/pg16.txt), [docker.txt](logs/goal-20/docker.txt), and [golangci-lint.txt](logs/goal-20/golangci-lint.txt) were captured on `4242618b9e8048744796f90076a880558fd633b6`. Those commands do not depend on the viewer edits.

[trailer.txt](logs/goal-20/trailer.txt) line 5 is `commit 4242618b9e8048744796f90076a880558fd633b6`. Line 6 is `Author: Maxence Jules <Maxencejules@users.noreply.github.com>`. Line 7 is `Commit: Maxence Jules <Maxencejules@users.noreply.github.com>`. Line 13 is `Tested-tree: 1f07eb33e2d43b789219e510b86582abd236f1c9`. That trailer is an earlier code commit. This tested commit's trailer is in the commit message, not in that file.

## Head CI

[Run 43918347593](https://github.com/Maxencejules/shortvideo-network/actions/runs/43918347593) on `7251719db45c8ecf5634fac86172608cdabdee2e` was cancelled after `python` stopped at `ruff format --check`. `milestone` had exited 56 while `curl` downloaded the PostgreSQL apt key. That download is the existing install step.

[Run 43919422615](https://github.com/Maxencejules/shortvideo-network/actions/runs/43919422615) on `7c609ec334922c42604ce62e1fe595e917b6f3c2` failed `python` after format, mypy, the secret scan, and the SBOM check had passed. `test_corpus_cli_paths_are_reported` refused `git add` because `apps/web/dist` was gitignored. `test_browser_decodes_the_mp4_it_fetched` returned `FAILED` with no report. At that revision `run_until` used one `FAILED` status both when the process ended and when the deadline passed, so that log does not show which one happened. Commit `894aa0c4345fc3d77c1f5338356202e9bc6f4b0c` dropped the dist ignore and retried every first miss. This tested commit does not do that. It retries only `EXITED`, and `EXITED` is only a process that started and then ended. A launch that never started is `LAUNCH_FAILED` and is not retried.

[Run 43920889498](https://github.com/Maxencejules/shortvideo-network/actions/runs/43920889498) on `8767c92abc900584a4bedb7f26a4a0fdd7428e2f` finished with conclusion `success`. All 17 jobs passed, including `python`, `node`, `milestone`, `devnet`, and every `go-race` shard.

[Run 43922844188](https://github.com/Maxencejules/shortvideo-network/actions/runs/43922844188) on `67212967e503447123f86cc1eeaa38c255bd55d8` finished with conclusion `success`. All 17 jobs passed. That head is the evidence commit reviewed in round 1.

[Run 43928148973](https://github.com/Maxencejules/shortvideo-network/actions/runs/43928148973) on `9eecc4fcb174fea9e068e6ec2504a0d0daadcc6f` is the first run after the early-exit split. Its `python` job failed `test_browser_decodes_the_mp4_it_fetched`: the status was `TIMEOUT`, the report was empty, and stdout was `decode-retry no status=TIMEOUT`. The test did not launch Chrome again. `go` and `go-race (broadcaster)` failed while installing PostgreSQL 18.6, both with exit code 56. That is the same `curl` receive failure seen on an earlier milestone job, not a test assertion. Commit `5fa9b7b7d5e59345760e2e629b1e0a7a25b04be4` raises the decode deadline from 25 seconds to 60. The next push cancelled this run. A green run is not visual approval.

[Run 43929997253](https://github.com/Maxencejules/shortvideo-network/actions/runs/43929997253) on `7ebfaeb9d25dbcf000e4b53ca0e36b681f82c5c5` finished with conclusion `failure`. Sixteen jobs passed, including `python` with the 60 second deadline. `go-race (devnet-broadcaster)` failed in `Install PostgreSQL 18.6` with exit code 56, again while `curl` fetched the apt key. No test assertion failed. This tested commit tries that download three times and still exits 1 when all three fail.

[Run 43933027338](https://github.com/Maxencejules/shortvideo-network/actions/runs/43933027338) on `5d468f8de4ce9adfffebb0f443b1cee3a0e7966c` finished with conclusion `success`. All 17 jobs passed, including `python`, `node`, `milestone`, `devnet`, and every `go-race` shard. That head is the evidence commit above this tested commit. A green run is not visual approval.

## What ran

| Command | Log | Result |
| --- | --- | --- |
| `npm run typecheck` | [typecheck.txt](logs/goal-20/typecheck.txt) | line 13 `EXIT:0` |
| `python3 scripts/run_tests.py --only node` | [node.txt](logs/goal-20/node.txt) | line 253 `EXIT:0`. 33 tests, 0 failures. |
| `python3 -m pytest -q tests/unit/test_web_viewer.py -s --tb=short` | [browser.txt](logs/goal-20/browser.txt) | line 6 `.`, line 7 `EXIT:0` |
| `python3 -m pytest -q tests/unit/test_delivery.py::test_browser_decodes_the_mp4_it_fetched -s --tb=short` | [decode.txt](logs/goal-20/decode.txt) | line 5 `decode-retry no status=REPORTED`, line 7 `EXIT:0` |
| the missing-binary and early-exit status tests | [launch.txt](logs/goal-20/launch.txt) | line 6 `EXIT:0` |
| `python3 scripts/scan_secrets.py` | [secrets.txt](logs/goal-20/secrets.txt) | line 5 `secret scan ok`, line 6 `EXIT:0` |
| `python3 scripts/write_sbom.py --check` | [sbom.txt](logs/goal-20/sbom.txt) | line 5 `sbom ok components 1389`, line 6 `EXIT:0` |
| `ruff format --check` and `ruff check` on the three edited Python files | [ruff.txt](logs/goal-20/ruff.txt) | line 5 `3 files already formatted`, line 7 `EXIT:0` |
| `python3 scripts/workflow_policy.py` and `python3 -m pytest -q tests/unit/test_ci_broadcaster.py --tb=line` | [workflow.txt](logs/goal-20/workflow.txt) | line 5 `workflow policy ok files 1`, line 7 `EXIT:0` |
| `python3 -m pytest -q tests/unit/test_secret_corpus.py::test_corpus_cli_paths_are_reported --tb=short` | [corpus.txt](logs/goal-20/corpus.txt) | line 6 `EXIT:0` |

[node.txt](logs/goal-20/node.txt) line 87 is `# pass 13` and line 88 is `# fail 0` for the evidence-label file. Line 157 is `# pass 10` and line 158 is `# fail 0` for the model file. Line 185 is `# pass 3` and line 186 is `# fail 0` for playback. Line 201 is `# pass 1` for the React pin. Line 247 is `# pass 6` and line 248 is `# fail 0` for the component file.

[runtime.txt](logs/goal-20/runtime.txt) line 5 is `v22.23.4`. Line 6 is `10.9.9`. Line 7 is `Python 3.12.3`. Line 9 begins with `Google Chrome 157.0.8103.50`. Line 10 begins with `ffmpeg version 6.1.1-3ubuntu5`. Line 11 is `Linux 6.12.113+ x86_64`. Line 12 is `EXIT:0`.

## Browser and device

Headless Chrome on this VM. No screenshot was taken. This is not visual design approval. A successful web build is not this check. The check is [browser.txt](logs/goal-20/browser.txt) line 7 `EXIT:0`.

[browser.txt](logs/goal-20/browser.txt) line 5 records `Chrome/157.0.8103.50`, the page user agent `HeadlessChrome/157.0.0.0`, and the viewport at the end of the run: width 390, height 844, device pixel ratio 2. Line 5 also records `account` `stand-in`, `indexer` `stand-in`, `delivery` `svn-delivery`, `"muted": false`, `"muteLabel": "Mute"`, and `"afterKey": "crotated"`.

The same run, before that viewport, asserted a desktop page width of at least 1000 and a video layout width from 200 through 420 (`tests/unit/test_web_viewer.py` lines 174 and 175). The narrow viewport asserted a page width of at most 400 and a video layout width inside that page (`tests/unit/test_web_viewer.py` lines 170 and 171).

The processed package is the goal 15 portrait fixture, encoded in the test and served by `MediaServer`. [browser.txt](logs/goal-20/browser.txt) does not print the frame size. The test requires a non-zero video width (`tests/unit/test_web_viewer.py` line 131) and a caption containing `portrait-cue` (line 132). After the host change, `rendered` is true (line 145) and again after the quality change (line 158). The first host's segment hits are prefixed `test-a ` (line 135). After that host stops listening, `hostsTried` includes `origin-test` (line 144). The profile shows the missing-session sentence (line 161). The home text shows the fixture sentence and the withdrawn sentence (lines 165 and 166). Reduced motion is `reduce` and scroll behavior is `auto` (lines 178 and 179).

Mute starts on. The plan clicks `#mute` (`tests/unit/test_web_viewer.py` lines 371 through 375) and reads `video.muted` and the button label (line 376). The test requires `muted` false and the label `Mute` (lines 152 and 153). Keyboard focus is `#feed` (lines 377 through 380). The next step sends `ArrowDown` (line 382) and waits until the playback content id is `crotated` and exactly one `video` element remains (line 383). The test requires that content id and that count (lines 148 and 149).

`apps/web/index.html` lines 6 through 8 send a content security policy. `style-src` allows inline styles because the page inserts its stylesheet from script. `img-src` and `media-src` allow `blob:`. `connect-src` allows loopback HTTP and HTTPS. The browser run above loaded that page, played the package, unmuted it, and moved with the keyboard. That is the check that this policy allowed those requests. There is no separate test that a forbidden origin is blocked.

## Acceptance

| Check | Status | Where |
| --- | --- | --- |
| Play a processed package, change quality, keep one video element | VERIFIED on this host | [browser.txt](logs/goal-20/browser.txt) line 7. Quality `rendered` is `tests/unit/test_web_viewer.py` line 158. The single video element is line 149. |
| Lose a host and recover | VERIFIED on this host, two loopback processes | `tests/unit/test_web_viewer.py` lines 135 and 144. |
| Bounded prefetch, cancel, no media for the unselected queue | VERIFIED in unit tests | [node.txt](logs/goal-20/node.txt) line 176. |
| No withdrawn or quarantine playback | VERIFIED | [node.txt](logs/goal-20/node.txt) lines 106 and 118. The browser feed shows the withdrawn sentence, `tests/unit/test_web_viewer.py` line 166. |
| Captions | VERIFIED on this host | Caption text is `tests/unit/test_web_viewer.py` line 132. Component text is [node.txt](logs/goal-20/node.txt) line 214. |
| Mute | VERIFIED on this host | The click is `tests/unit/test_web_viewer.py` lines 371 through 376. The asserts are lines 152 and 153. The component labels are `apps/web/test/ui.test.ts` lines 44 through 54 and [node.txt](logs/goal-20/node.txt) line 220. |
| Keyboard | VERIFIED on this host | `ArrowDown` is `tests/unit/test_web_viewer.py` line 382. The wait is line 383. The asserts are lines 148 and 149. |
| Reduced motion | VERIFIED on this host | `tests/unit/test_web_viewer.py` lines 178 and 179. |
| Expired session and a failed second read | VERIFIED against stand-ins, not `svn-api` | `tests/unit/test_web_viewer.py` line 161. The account body is the account service's `UNAUTHENTICATED` JSON. The process is not `svn-api`. |
| No wallet, no peer upload, gated social actions | VERIFIED | [node.txt](logs/goal-20/node.txt) line 208. |
| User text is not inserted as HTML | VERIFIED | [node.txt](logs/goal-20/node.txt) lines 94, 214, and 238. |
| Real `svn-api` passkey login in the browser | BLOCKED | DR-20-03. PostgreSQL is absent. [pg16.txt](logs/goal-20/pg16.txt) line 5 is `absent`. The relying party origin remains `http://localhost` (DR-19-06). |
| Real `svn-indexer` in the browser | BLOCKED | DR-20-04. The stand-in uses the Go field names. It is not a committed chain read. |
| Visual design approval | Not claimed | No screenshot. |

A response is not counted as a completed watch. The playback unit test checks that case ([node.txt](logs/goal-20/node.txt) line 164).

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-20-01 | OPEN | `svn-api` and `svn-indexer` do not send `Access-Control-Allow-Origin`. The viewer calls loopback origins directly. A page on another origin cannot read those responses. Delivery already allows one configured origin. |
| DR-20-02 | OPEN | Media fetches stay on loopback HTTPS. The delivery adapter binds loopback. A public media host is not approved. |
| DR-20-03 | OPEN, BLOCKED | The browser did not complete a passkey ceremony against `svn-api`. DR-19-06 keeps the relying party origin at `http://localhost`. This machine has no PostgreSQL 16 binary. |
| DR-20-04 | OPEN, BLOCKED | The browser indexer is a stand-in. `svn-indexer` was not started. |
| DR-20-05 | OPEN | Follow, block, report, comments, search, and activity stay gated for goals 22 and 23. The controls do not report success. |
| DR-20-06 | OPEN | No screenshot and no visual design approval. The headless layout numbers are in the browser log and the test assertions. |
| DR-20-07 | OPEN | Prefetch is the goal 15 proposed window: one extra segment, one extra item, two attempts per host, four hosts. DR-15-01. Not an approved allowance. |
| DR-19-02 | OPEN, BLOCKED | Carried. No host-held user signing key. |
| DR-19-03 | OPEN, BLOCKED | Carried. No account recovery control. The profile says recovery is not available. |
| DR-19-06 | OPEN | Carried. Relying party origin `http://localhost`. |
| DR-19-07 | OPEN | Carried. Public sign-up and public launch stay off. |
| DR-15-01 .. DR-15-06 | OPEN | Carried. The playback grant is still local and is not publication. |
| DR-18-01 .. DR-18-06 | OPEN | Carried. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody and recovery stay unimplemented. DR-19-02 and DR-19-03.
2. **One machine.** The host failover is two processes on this VM.
3. **Goal 06 and goal 07 mutation runners.** Not re-run. They were already failing on missing anchors in goal 19.
4. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-20/golangci-lint.txt) line 5 is `--: line 28: golangci-lint: command not found` and line 6 is `EXIT:127`.
5. **PostgreSQL 16.** Not installed. [pg16.txt](logs/goal-20/pg16.txt) line 5 is `absent` and line 6 is `EXIT:1`.
6. **Goal 04 compose bridge.** [docker.txt](logs/goal-20/docker.txt) line 5 is `--: line 25: docker: command not found` and line 6 is `EXIT:127`. Compose was not run. The unauthenticated :2375 daemon was not used.

## Requirements

The `requirements-matrix` block already names goal 20. It was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-PROD-003 | Watch is VERIFIED in the browser run. Follow, block, and report are visible gates, not completed actions. No wallet control. |
| REQ-PROD-004 | VERIFIED for layout width at a wide viewport and at 390 by 844. Not a visual design approval. |
| REQ-UX-016 | Same as REQ-PROD-003. Peer sharing stays off. |
| REQ-SCOPE-017 | VERIFIED for captions (`tests/unit/test_web_viewer.py` line 132), the mute click and asserts (lines 152, 153, and 371 through 376), the keyboard move (lines 148, 149, 382, and 383), and reduced motion (lines 178 and 179). |
| REQ-ARCH-002 | VERIFIED. No wallet and no hosting toggle. |
| REQ-ARCH-009 | VERIFIED for a second read endpoint and the trust sentence. The second endpoint was closed on purpose. |
| REQ-ARCH-012 | VERIFIED for the stale sentence in [node.txt](logs/goal-20/node.txt) line 130. The browser indexer body was stale. |
| REQ-GW-001 | VERIFIED. The profile says two HTTP responses are not light-client verification. [node.txt](logs/goal-20/node.txt) line 136. |
| REQ-MEDIA-006 | VERIFIED for the bounded window and for not counting a response as a watch. [node.txt](logs/goal-20/node.txt) lines 164 and 176. |
| REQ-FEED-004 | VERIFIED. No peer-sharing control and no public IPFS advertise path. [node.txt](logs/goal-20/node.txt) line 238. |
| REQ-PROD-001 | The viewer exists. The goal 30 three-part demonstration is not this goal. |

## Next safe action

Do not treat the stand-in indexer or the stand-in account listener as `svn-indexer` or `svn-api`. DEC-001 still blocks recovery. Goals 22 and 23 still own search, activity, follow, block, report, and comments. Do not describe this headless run as visual design approval.
