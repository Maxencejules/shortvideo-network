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
| Commit | `894aa0c4345fc3d77c1f5338356202e9bc6f4b0c` |
| Tree | `b7e1589a0f0d53f45d80de73268f63fe3d680119` |
| Base | `5d64047d9d532f9c0bba709b0f9f61da4724b2fa` |
| Branch | `goal-20` |

The tested commit message carries `Tested-tree: b7e1589a0f0d53f45d80de73268f63fe3d680119`. `git rev-parse 894aa0c4345fc3d77c1f5338356202e9bc6f4b0c^{tree}` is that tree. It removes the `apps/web/dist/` ignore line and retries the goal 15 headless decode once when Chrome exits before it reports. [ruff.txt](logs/goal-20/ruff.txt) line 1 is `HEAD 894aa0c4345fc3d77c1f5338356202e9bc6f4b0c`. Line 2 is `TREE b7e1589a0f0d53f45d80de73268f63fe3d680119`. Line 3 is `DIRTY no`. Line 5 is `2 files already formatted`. Line 7 is `EXIT:0`. [corpus.txt](logs/goal-20/corpus.txt) line 1 is that same HEAD. Line 6 is `EXIT:0`.

Behavior logs other than those two were captured on `4242618b9e8048744796f90076a880558fd633b6`. The viewer behavior did not change after that commit. The later commits wrap one test dict, drop the dist ignore, and retry the older decode test.

[trailer.txt](logs/goal-20/trailer.txt) line 5 is `commit 4242618b9e8048744796f90076a880558fd633b6`. Line 6 is `Author: Maxence Jules <Maxencejules@users.noreply.github.com>`. Line 7 is `Commit: Maxence Jules <Maxencejules@users.noreply.github.com>`. Line 13 is `Tested-tree: 1f07eb33e2d43b789219e510b86582abd236f1c9`. That trailer is the parent code commit. This tested commit's trailer is in the commit message, not in that file.

## Head CI

[Run 43918347593](https://github.com/Maxencejules/shortvideo-network/actions/runs/43918347593) on `7251719db45c8ecf5634fac86172608cdabdee2e` was cancelled after `python` stopped at `ruff format --check`. `milestone` had exited 56 while `curl` downloaded the PostgreSQL apt key. That download is the existing install step.

[Run 43919422615](https://github.com/Maxencejules/shortvideo-network/actions/runs/43919422615) on `7c609ec334922c42604ce62e1fe595e917b6f3c2` failed `python` after format, mypy, the secret scan, and the SBOM check had passed. `test_corpus_cli_paths_are_reported` refused `git add` because `apps/web/dist` was gitignored. `test_browser_decodes_the_mp4_it_fetched` returned `FAILED` with no report: headless Chrome exited before the page posted. The tested commit drops that ignore and retries that decode once. A later run on this tested head is recorded when it finishes. A green run is not visual approval.

## What ran

| Command | Log | Result |
| --- | --- | --- |
| `npm run typecheck` | [typecheck.txt](logs/goal-20/typecheck.txt) | line 13 `EXIT:0` |
| `python3 scripts/run_tests.py --only node` | [node.txt](logs/goal-20/node.txt) | line 247 `EXIT:0`. 32 tests, 0 failures. |
| `python3 -m pytest -q tests/unit/test_web_viewer.py -s --tb=short` | [browser.txt](logs/goal-20/browser.txt) | line 6 `.`, line 7 `EXIT:0` |
| `python3 scripts/scan_secrets.py` | [secrets.txt](logs/goal-20/secrets.txt) | line 5 `secret scan ok`, line 6 `EXIT:0` |
| `python3 scripts/write_sbom.py --check` | [sbom.txt](logs/goal-20/sbom.txt) | line 5 `sbom ok components 1389`, line 6 `EXIT:0` |
| `ruff format --check` and `ruff check` on the two edited tests | [ruff.txt](logs/goal-20/ruff.txt) | line 5 `2 files already formatted`, line 7 `EXIT:0` |
| `python3 -m pytest -q tests/unit/test_secret_corpus.py::test_corpus_cli_paths_are_reported --tb=short` | [corpus.txt](logs/goal-20/corpus.txt) | line 6 `EXIT:0` |

[node.txt](logs/goal-20/node.txt) line 87 is `# pass 13` and line 88 is `# fail 0` for the evidence-label file. Line 157 is `# pass 10` and line 158 is `# fail 0` for the model file. Line 185 is `# pass 3` and line 186 is `# fail 0` for playback. Line 201 is `# pass 1` for the React pin. Line 241 is `# pass 5` and line 242 is `# fail 0` for the component file.

[runtime.txt](logs/goal-20/runtime.txt) line 5 is `v22.23.4`. Line 6 is `10.9.9`. Line 7 is `Python 3.12.3`. Line 9 is `Google Chrome 157.0.8103.50`. Line 10 is the FFmpeg `6.1.1-3ubuntu5` line. Line 11 is `Linux 6.12.113+ x86_64`. Line 12 is `EXIT:0`.

## Browser and device

Headless Chrome on this VM. No screenshot was taken. This is not visual design approval. A successful web build is not this check. The check is [browser.txt](logs/goal-20/browser.txt) line 7 `EXIT:0`.

[browser.txt](logs/goal-20/browser.txt) line 5 records `Chrome/157.0.8103.50`, the page user agent `HeadlessChrome/157.0.0.0`, and the viewport at the end of the run: width 390, height 844, device pixel ratio 2. Line 5 also records `account` `stand-in`, `indexer` `stand-in`, and `delivery` `svn-delivery`.

The same run, before that viewport, asserted a desktop page width of at least 1000 and a video layout width from 200 through 420 (`tests/unit/test_web_viewer.py` lines 159 and 160). The narrow viewport asserted a page width of at most 400 and a video layout width inside that page (`tests/unit/test_web_viewer.py` lines 155 and 156).

The processed package is the goal 15 portrait fixture, encoded in the test and served by `MediaServer`. [browser.txt](logs/goal-20/browser.txt) does not print the frame size. The test requires a non-zero video width (`tests/unit/test_web_viewer.py` line 123) and a caption containing `portrait-cue` (line 124). After the host change, `rendered` is true (line 137) and again after the quality change (line 143). The first host's segment hits are prefixed `test-a ` (line 127). After that host stops listening, `hostsTried` includes `origin-test` (line 136). One `video` element remains (line 138). The profile shows the missing-session sentence (line 146). The home text shows the fixture sentence and the withdrawn sentence (lines 150 and 151). Reduced motion is `reduce` and scroll behavior is `auto` (lines 163 and 164).

## Acceptance

| Check | Status | Where |
| --- | --- | --- |
| Play a processed package, change quality, keep one video element | VERIFIED on this host | [browser.txt](logs/goal-20/browser.txt) line 7. Quality `rendered` is `tests/unit/test_web_viewer.py` line 143. The single video element is line 138. |
| Lose a host and recover | VERIFIED on this host, two loopback processes | `tests/unit/test_web_viewer.py` lines 127 and 136. |
| Bounded prefetch, cancel, no media for the unselected queue | VERIFIED in unit tests | [node.txt](logs/goal-20/node.txt) line 176. |
| No withdrawn or quarantine playback | VERIFIED | [node.txt](logs/goal-20/node.txt) lines 106 and 118. The browser feed shows the withdrawn sentence, `tests/unit/test_web_viewer.py` line 151. |
| Captions, mute, keyboard, reduced motion | VERIFIED on this host | Caption text is `tests/unit/test_web_viewer.py` line 124. Reduced motion is lines 163 and 164. Component coverage is [node.txt](logs/goal-20/node.txt) lines 214 and 226. |
| Expired session and a failed second read | VERIFIED against stand-ins, not `svn-api` | `tests/unit/test_web_viewer.py` line 146. The account body is the account service's `UNAUTHENTICATED` JSON. The process is not `svn-api`. |
| No wallet, no peer upload, gated social actions | VERIFIED | [node.txt](logs/goal-20/node.txt) line 208. |
| User text is not inserted as HTML | VERIFIED | [node.txt](logs/goal-20/node.txt) lines 94, 214, and 232. |
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
4. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-20/golangci-lint.txt) line 5 is `golangci-lint: command not found` and line 6 is `EXIT:127`.
5. **PostgreSQL 16.** Not installed. [pg16.txt](logs/goal-20/pg16.txt) line 5 is `absent` and line 6 is `EXIT:1`.
6. **Goal 04 compose bridge.** [docker.txt](logs/goal-20/docker.txt) line 5 is `docker: command not found` and line 6 is `EXIT:127`. Compose was not run. The unauthenticated :2375 daemon was not used.

## Requirements

The `requirements-matrix` block already names goal 20. It was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-PROD-003 | Watch is VERIFIED in the browser run. Follow, block, and report are visible gates, not completed actions. No wallet control. |
| REQ-PROD-004 | VERIFIED for layout width at a wide viewport and at 390 by 844. Not a visual design approval. |
| REQ-UX-016 | Same as REQ-PROD-003. Peer sharing stays off. |
| REQ-SCOPE-017 | VERIFIED for captions, keyboard move, reduced motion, and mute controls on this host. |
| REQ-ARCH-002 | VERIFIED. No wallet and no hosting toggle. |
| REQ-ARCH-009 | VERIFIED for a second read endpoint and the trust sentence. The second endpoint was closed on purpose. |
| REQ-ARCH-012 | VERIFIED for the stale sentence in [node.txt](logs/goal-20/node.txt) line 130. The browser indexer body was stale. |
| REQ-GW-001 | VERIFIED. The profile says two HTTP responses are not light-client verification. [node.txt](logs/goal-20/node.txt) line 136. |
| REQ-MEDIA-006 | VERIFIED for the bounded window and for not counting a response as a watch. [node.txt](logs/goal-20/node.txt) lines 164 and 176. |
| REQ-FEED-004 | VERIFIED. No peer-sharing control and no public IPFS advertise path. [node.txt](logs/goal-20/node.txt) line 232. |
| REQ-PROD-001 | The viewer exists. The goal 30 three-part demonstration is not this goal. |

## Next safe action

Do not treat the stand-in indexer or the stand-in account listener as `svn-indexer` or `svn-api`. DEC-001 still blocks recovery. Goals 22 and 23 still own search, activity, follow, block, report, and comments. Do not describe this headless run as visual design approval.
