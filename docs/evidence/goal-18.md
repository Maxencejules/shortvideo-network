# Goal 18 evidence

Status: BLOCKED

This is a working prototype on one VM. It is not a reviewed public beta, not an audit, and not a production network. Public launch stays disabled. There is no token, coin, sale, or mining. No video bytes and no personal data were written to the chain. The chain records digests, byte counts, and lease terms. `chain/` and `proto/` are not in the branch file list.

The scenario joins the API, its PostgreSQL database and outbox, the media worker, the broadcaster, the indexer, four provider agents, the scheduler, delivery, and a four-validator devnet. Access is the development fixture principal `creator-a`. Accounts stay Goal 19.

Gate C is "upload, replication, final chain commitment and real playback survive origin loss." What this run shows, and what it does not show, is in the Gate C section. Everything ran on this one machine. That is one physical failure domain.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/18-end-to-end-network-milestone.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-43d93a0f-3e8d-5da7-a8ed-490e19711e2a`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed.
- On 2026-10-31 the owner waived the model requirement as a blocker ("Don't mind the ai model requirement", recorded on goal 04). On 2026-11-01 the owner wrote "I acknowledge the model substitution for DR-06-07" and then "extend the acknowledgement to goals 07 through 30". That covers model allocation only. It does not certify correctness.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `a5628ab5b6dfb4f1b16546654415063f1499a992` |
| Tree | `4dcc6a681a94c7df817f62c149a23b82e8273bc4` |
| Base | `f21ef2b22b15921600515a47f90164dede34920c` |
| Branch | `goal-18` |

[trailer.txt](logs/goal-18/trailer.txt) line 20 is `commit a5628ab5b6dfb4f1b16546654415063f1499a992`. Lines 21 and 22 name `Maxence Jules <Maxencejules@users.noreply.github.com>` as author and committer. Line 28 is `Tested-tree: 4dcc6a681a94c7df817f62c149a23b82e8273bc4`. The same file's `TREE` line is that tree. This evidence commit changes only `docs/evidence/` relative to that tested commit.

Earlier commits on this branch are ancestors. `1a89c0ea34b278e0f34b60876f61c9bad3669d1a` joined the path. `ea4a3593d838068ee3c5a63364ec2c6390720f38` removed an unused struct that `go vet` rejected. `21dc9f28dd3380a37db2a811937488d58c1e4e13` waits for a fresh indexer read before the withheld publish. `11a9c6972ec595880665020e4ffbe764983d39a7` formats the client after CI run 42823652645. `bd3869741e70e8ab397fcebface0e52df27443bf` stopped restoring host bindings into a new process after CI run 42826355319. The tested commit lets a second process name the origin in a grant when the package verifies, without advertising a host this process has not bound. The scenario log below is the tested commit.

[range.txt](logs/goal-18/range.txt) is `git diff --name-only f21ef2b22b15921600515a47f90164dede34920c..HEAD`. Lines 22 through 75 are the paths. `chain/` and `proto/` are not among them. [names.txt](logs/goal-18/names.txt) is only the last commit: line 21 is `services/delivery/svn_delivery/catalog.py`.

The demonstration is `scripts/milestone/run.sh`. The independent client is `scripts/milestone/client.py`. It reads the indexer and the delivery HTTPS port. It does not open a database, a key file, or the quarantine directory.

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01, production custody.** The hosting-intent role remains unset. The broadcaster in this run uses the devnet service and broadcaster accounts. Those keys are generated in the test temporary directory and are not committed.
2. **One machine.** [scenario.txt](logs/goal-18/scenario.txt) line 7 prints `physical_count=1 independent=false`. Line 63 prints `physical=1 simulated=true independent=false`. Four validator processes and four provider processes on this host are one physical failure domain. This VM is not an approved multi-machine integration runner.
3. **Goal 06 and goal 07 mutation runners.** Anchor checks only. [anchors06.txt](logs/goal-18/anchors06.txt) line 30 is `RESULT search-string problems=13; no test was run` and line 31 is `EXIT:1`. [anchors07.txt](logs/goal-18/anchors07.txt) line 26 is `RESULT validation problems=3; no test was run` and line 27 is `EXIT:1`.
4. **golangci-lint.** Not installed. [golangci-lint.txt](logs/goal-18/golangci-lint.txt) line 16 is `scripts/evidence/capture.sh: line 58: golangci-lint: command not found` and line 17 is `EXIT:127`.
5. **PostgreSQL 16.** Not installed. [pg16.txt](logs/goal-18/pg16.txt) line 18 is `EXIT:1`. [runtime.txt](logs/goal-18/runtime.txt) line 12 is `postgres (PostgreSQL) 18.6 (Ubuntu 18.6-1.pgdg24.04+2)` and line 13 is `pg16_test:1`. The scenario used 18.6. [scenario.txt](logs/goal-18/scenario.txt) line 42 names that same 18.6 server.
6. **Goal 04 compose bridge.** [docker.txt](logs/goal-18/docker.txt) line 17 is `scripts/evidence/capture.sh: line 58: docker: command not found` and line 18 is `EXIT:127`. Compose was not run. The unauthenticated :2375 daemon was not used.
7. **Goal 16 and 17 fixture chain view.** Replaced for this scenario by the devnet. The provider agents talk to `services/milestone/internal/chainview`, which reads one CometBFT node and broadcasts only already signed bytes. A broadcast HTTP 200 requires CheckTx and DeliverTx code 0. That is still one configured gateway, not a light client. DR-16-04 stays open for every path that still uses the fixture.

## Decision requests

These are open. They are not owner decisions.

| ID | Status | Effect |
| --- | --- | --- |
| DR-18-01 | OPEN | The content digest registered on the chain is the sha256 of the scheduler package text, not the media worker's manifest file. [scenario.txt](logs/goal-18/scenario.txt) line 49 `manifest_sha256` is `021ddad6d25e0779ea1f44b7ee245776b4afdcb3db5f956b48a36acb795b692e`. Line 50 `package_text_sha256` is `b429dedb07cf23814ef07d7e02ccb86e5724b922dd75e5088e555362e5833988`. Line 66 shows the indexer manifest equal to the package text. Both digests are real. They are not the same document. |
| DR-18-02 | OPEN | Providers `p1`, `p2`, and `p3` submit with the genesis account `provider-1`. `p4` uses `provider-2`. [scenario.txt](logs/goal-18/scenario.txt) lines 13-16. The ed25519 agent key still signs the inner acceptance. `keys/outer.hex` is a copy of the devnet secp256k1 account, created in the test temporary directory, so the transaction signer is the admitted account. It is not committed. |
| DR-18-03 | OPEN | Scheduler publication and the playback document are different fields. Line 63 is `publication=PUBLISHED`. Line 68 is `publication=NOT_PUBLISHED` with `hosting_promise=False` and `chain_status=HELD`. A local grant is not publication. |
| DR-18-04 | OPEN | An accepted lease does not name the offer (DR-08-08). The indexer closes the one OPEN offer for that provider and content in the same transaction. Zero or several matches stay OPEN. [scenario.txt](logs/goal-18/scenario.txt) line 64 is `phase=INDEXER_RECONCILE ok` after the three accepts. Line 65 is `reconciliation_status":"ok"` and `"stale":false`. |
| DR-18-05 | OPEN | A stale indexer read blocks publication and repair. The tested commit reconciles before the withheld publish so a not-yet-run reconciliation is not reported as the moderation decision. Line 54 is `publication=WITHHELD reason=community_approval`. |
| DR-18-06 | OPEN | Declared operator labels are simulation. Line 63 is `simulated=true independent=false physical=1`. Three counted replicas on one machine are not three failure domains. |
| DR-18-07 | OPEN | `POST /v1/publications`, `/v1/repairs`, `/v1/origin`, and `/v1/hosts` are unauthenticated. `/v1/hosts` accepts a provider admin token. They are test-harness control routes. `svn-scheduler` refuses a non-loopback listen (`services/scheduler/cmd/svn-scheduler/main.go` lines 57-60). Authenticate them or remove them before any non-loopback binding. See the residual-risk section. |
| DR-17-01 .. DR-17-08 | OPEN | Carried. The scheduler still does not sign. |
| DR-16-01 .. DR-16-06 | OPEN | Carried. DR-16-04 is closed only for this scenario's chain view. |
| DR-15-01 .. DR-15-06 | OPEN | Carried. The playback grant is still local. |
| DR-14-01 .. DR-14-04 | OPEN | Carried. The clip is a one-second synthetic ffmpeg pattern, not an approved package format. |
| DR-13-01 .. DR-13-04 | OPEN | Carried. The caller is a development fixture token. |
| DR-07-01 | OPEN | Carried. Production custody is unset. |

## Acceptance

The acceptance log is [scenario.txt](logs/goal-18/scenario.txt). `HEAD` is `a5628ab5b6dfb4f1b16546654415063f1499a992` and `TREE` is `4dcc6a681a94c7df817f62c149a23b82e8273bc4`. `DIRTY no` is line 5. The other logs in this directory were captured after that file existed, so they print `DIRTY yes` and list the earlier log paths. No source file was modified between those captures.

The clip is synthetic. Line 44 is `bytes=12746 sha256=190072c758fab6a1b799d210630ca3baa449e3a55e856bf003c25331f31bafd6 synthetic=true`. The package is 12740 bytes with 2 segments (line 50). The played segment is 11524 bytes (line 71).

| Check | Status | Where |
| --- | --- | --- |
| Four-validator devnet, not the fixture chain | VERIFIED on this host, one machine | Line 7 `chain_id=shortvideo-dev-89a7a7f306f0c3c4` `physical_count=1`. Line 8 `height=3` `validators=4` `app_hash=C7735035AC213DF7B0AA67D3F332E7C4DB884369FD2CC3589DECD48F2E19D650`. |
| Development principal only | VERIFIED as the fixture | The API publish before the chain path is line 48 `status=403`. Public sign-up is not this goal. |
| Upload, one reservation on retry | VERIFIED on this host | Line 45 `upload=ueb53fb70c1b915f440b12452835d2bbe` `repeat_upload=ueb53fb70c1b915f440b12452835d2bbe` `status=RESERVED`. The test refuses a second upload id at `services/milestone/scenario_phases_test.go:57`. |
| Bytes received, processing queued, worker ready | VERIFIED on this host | Line 46 `status=204`. Line 47 `PROCESSING_QUEUED`. Line 49 `status":"READY_FOR_REVIEW"` `publication":"NOT_PUBLISHED"` `package_bytes":12740`. |
| Moderation withheld, then three committed leases, then publish | VERIFIED on this host | Line 54 `WITHHELD` `community_approval`. Lines 56, 58, and 60 are `phase=COMMITTED kind=OFFER_LEASE` at heights 57, 62, and 66. Lines 57, 59, and 61 are the matching `phase=OFFER_COMMITTED` lines. Line 63 `PUBLISHED` `leases=3 distinct=3 verified=3` at `2026-11-10T13:53:31.525725256Z`. Line 64 reconciliation `ok`. |
| Independent client plays the segment | VERIFIED on this host | Lines 66-73. Line 71 `sha256=cbcc6ec21007d20c4bcff5223825b1a3f7e8b90b5ffbed08d9d7c9ffbe31ed04` matches `expected`. Line 73 `CLIENT ok`. |
| Repeated register is one business effect | VERIFIED for the content row and the transaction | Line 74 repeats the register tx `e043c8cd2135abcebc6532436de05beffc143e1fc86316f724c402aefae39bbd` at height 54, the same values as line 52. Line 75 `OPERATION_STATUS_COMMITTED`. Line 76 `content_rows=1`. The test refuses any other count at `services/milestone/scenario_phases_test.go:281`. A separate quota-balance column was not printed. |
| Corrupt one replica, a survivor still serves | VERIFIED on this host | Line 77 `availability_verified=2 distribution=200`. |
| Origin down, distribution continues | VERIFIED on this host | Line 78 `distribution=200 bytes=12740`. |
| One provider down, repair, restart | VERIFIED on this host | Line 79 `REACHABILITY_DEGRADED degraded=[p1]`. Line 82 `state":"CONFIRMED"` `source=p2 replacement=p4`. Line 80 is `phase=COMMITTED kind=OFFER_LEASE` at height 74, tx `bc65319d65266be3aea5905e12ee0d6233cd28a0b9bdb3645a3a6c47fd85842e`. Line 81 is the matching `phase=OFFER_COMMITTED` line. Line 84 `health=200`. |
| Unapproved offer refused, unauthorized accept rejected on chain | VERIFIED on this host | Line 85 `status=400`. Line 88 `height=489 code=1 want=1 txhash=1BAD9EBE84422CDD6681E16F2D2773E6669E4943D6AEADCCF93309C4DDCE3D5D`. |
| Quorum loss: playback continues, new commitment stays pending, resume does not reset history | VERIFIED as two of four processes stopped on this one machine | Line 89 `height_before=489 height_during=489 playback=200`. Line 90 `OPERATION_STATUS_PENDING_STALL`. Line 91 `height=490 app_hash=CE38387A556DDD765756FCA4C82F299208F13114523F3BFAC3E452FF679BE1D1 stall_height=489`. The height moved forward. The homes were not wiped. |
| Withdraw from the controlled services | VERIFIED on this host | Line 92 withdraw tx `545157b6633f82f65809001161f8e43ed9e8bcbabf0789478cc8abdf05443d04` at height 494. Line 93 `indexer=true`. Line 94 `status=410`. Line 95 `REMOVED`. |
| API and delivery status match the layer that owns them | VERIFIED as three layers, not one flag | Before publish, the API is 403 (line 48) and the worker is `NOT_PUBLISHED` (line 49). After publish, the scheduler is `PUBLISHED` (line 63) and the playback document stays `NOT_PUBLISHED` (line 68). After withdrawal, the indexer, the scheduler distribution, and delivery agree (lines 93-95). |
| Clean approved multi-machine integration runner | BLOCKED | This VM is one machine. Line 7 `physical_count=1`. |

Line 97 is `--- PASS: TestMilestone (151.32s)`. Line 98 is `PASS`. Line 100 is `EXIT:0`.

## Gate C

The evidence shows, on this one VM: a synthetic upload was processed, held for approval, then published after three committed leases on chain `shortvideo-dev-89a7a7f306f0c3c4`; the independent client retrieved the segment and the hash matched; marking the origin down left distribution at HTTP 200; killing one provider left a survivor, and repair committed a replacement lease from that survivor; stopping two of the four validator processes froze the height, left existing playback at HTTP 200, and left a new register at `PENDING_STALL`; starting the same validator homes continued to height 490 with the app hash in line 91. The chain was not reset and no earlier height was rewritten.

The evidence does not show a second machine, a second operator, a network partition, or survival of losing this VM. Four processes on one host are one physical failure domain. Playback during the stall is the local delivery grant inside its authorization window (`chain_status=HELD`, `hosting_promise=False`, line 68). That grant is not a chain publication and not a promise that a lease was renewed. An approved multi-machine runner was not used.

## Other commands

| Step | Exit | Log |
| --- | --- | --- |
| runtime | 0 | [runtime.txt](logs/goal-18/runtime.txt). Line 7 `Linux 6.12.113+ x86_64`. Line 8 `go version go1.26.10 linux/amd64`. Line 9 `4`. Line 10 `Python 3.12.3`. Line 11 `ffmpeg version 6.1.1-3ubuntu5`. |
| gofmt | 0 | [gofmt.txt](logs/goal-18/gofmt.txt). No file names were printed. Line 8 `EXIT:0`. |
| vet | 0 | [vet.txt](logs/goal-18/vet.txt). Line 9 `EXIT:0`. |
| regressions | 0 | [regressions.txt](logs/goal-18/regressions.txt). Lines 10-13 are the four `ok` lines. Line 14 `EXIT:0`. |
| requirements | 0 | [requirements.txt](logs/goal-18/requirements.txt). Line 36 `errors 0`. Line 70 `result PASS`. Line 72 `EXIT:0`. |
| pytest | 0 | [pytest.txt](logs/goal-18/pytest.txt). Line 19 ends with `[100%]`. Line 20 `EXIT:0`. The files are `tests/unit/test_ci_milestone.py`, `tests/unit/test_chain_writer_policy.py`, `tests/unit/test_delivery_hosts.py`, and `test_replica_host_failure_and_chain_outage` in `tests/unit/test_delivery.py`. |
| secrets | 0 | [secrets.txt](logs/goal-18/secrets.txt). Line 12 `secret scan ok`. Line 13 `EXIT:0`. |
| sbom | 0 | [sbom.txt](logs/goal-18/sbom.txt). Line 13 `sbom ok components 1322`. Line 14 `EXIT:0`. |
| anchors06 | 1 | BLOCKED. Not a pass. |
| anchors07 | 1 | BLOCKED. Not a pass. |
| golangci-lint | 127 | BLOCKED. Not a pass. |
| docker | 127 | BLOCKED. Not a pass. |
| pg16 | 1 | BLOCKED. Not a pass. |

## Requirements rows

| Row | Status | Where |
| --- | --- | --- |
| REQ-SCOPE-005 | VERIFIED on this host for the development-principal path. Not a public beta. | The scenario log, lines 44-96. Login as a product is Goal 19. |
| REQ-ARCH-011 | VERIFIED on this host for the stall | Line 89 playback 200. Line 90 the new register stays `PENDING_STALL`. |
| REQ-DEV-003 | VERIFIED on this host for registration, three commitments, and resume without a wiped home | Lines 52, 56, 58, 60, and 91. Lines 57, 59, and 61 repeat those offers as `OFFER_COMMITTED`. A database replay after a destroyed disk was not run. |
| REQ-OPS-006 | VERIFIED on this host for the stall disclosure the scheduler and the broadcaster already return | Line 90 `PENDING_STALL`. The playback document does not itself say the chain is stalled (line 68 `chain_status=HELD`). DR-18-03. |
| REQ-QA-003 | VERIFIED on this host as one machine | Lines 78, 79, and 82. Not three physical hosts. |
| REQ-RISK-002 | IMPLEMENTED as scope | This goal did not add a native app, a token, or a recommender. |
| REQ-SCOPE-015 | VERIFIED as the proposed parameter on this host | Line 63 `distinct=3 physical=1`. Not a durability result. |
| REQ-HOST-006 | VERIFIED for repair from a survivor on this host | Line 82. |
| REQ-UX-010 | BLOCKED as a product UI | The client prints the grant and the segment. There is no viewer application. Goal 20. |
| REQ-OFF-001 | VERIFIED as a boundary | The chain did not receive the clip. The client compared segment bytes (line 71). |
| REQ-CHAIN-005 | VERIFIED on this host | Line 89, two of four processes stopped, height unchanged. |
| REQ-PEND-001 | VERIFIED for the stalled intent | Line 90. |
| REQ-ARCH-012 | VERIFIED as the local grant during the stall | Line 68 and line 89. The grant is not a renewed lease. |
| REQ-REC-001 | VERIFIED for this resume | Line 91 continues from `stall_height=489` to height 490. No genesis reset. |
| REQ-ACK-001 | VERIFIED as a boundary | Provider accepts print `response_committed=true counted_from_response=false` inside line 82. The lease is counted after the indexer and the chain view, not from the broadcast body alone. |

## Handoff

Repeatable demonstration: `bash scripts/milestone/run.sh` from a clean tree with Go 1.26.10, Python 3.12.3, FFmpeg 6.1.1, and PostgreSQL 18.6 on the path used in [runtime.txt](logs/goal-18/runtime.txt). `SVN_SKIP_DEVNET=1` skips the scenario. That skip is not a pass. The go job sets that variable. The `milestone` job in `.github/workflows/ci.yml` is the CI run of `TestMilestone`.

Blockers to resolve before goals 19-24:

- DEC-001 and DR-07-01 custody, before any portability or production-signing claim (Goal 19).
- One machine and simulated operator labels (DR-18-06), before any independence or onboarding claim (Goal 24).
- The goal 06 and goal 07 mutation anchors (`problems=13` and `problems=3`).
- golangci-lint, PostgreSQL 16, and the goal 04 compose bridge.
- DR-18-01 (which digest is the content digest), DR-18-02 (shared genesis account), DR-18-03 (playback publication field), DR-18-04 (offer id missing from the accept event), DR-18-05 (a stale read blocks publication), and DR-18-07 (unauthenticated scheduler control routes).
- Carried DR-13, DR-14, DR-15, DR-16, and DR-17 requests. Accounts, retention, and audience rules stay Goals 19 and later.

## Residual risk

`p1`, `p2`, and `p3` share one genesis account, so "distinct operators" is the declared label, which line 63 marks `simulated=true`. The outer key file is an interim so an ed25519 agent can submit as that account. The indexer closes an offer only when exactly one OPEN offer matches the provider and content. Two open offers for one pair stay open and the next reconciliation is divergent. The playback grant is local and expires on its own clock. The quorum drill stops processes. It does not unplug a machine. The height jump from the repair (height 74, line 80) to the unauthorized accept (height 489, line 88) is empty blocks on this devnet, not a second chain.

**DR-18-07 (OPEN).** `services/scheduler/internal/sched/http.go` lines 41-44 register `POST /v1/publications`, `POST /v1/repairs`, `POST /v1/origin`, and `POST /v1/hosts`. None of these routes existed in goal 17. They are unauthenticated. `POST /v1/hosts` accepts a provider admin token (`http.go` lines 148-163). They are test-harness control routes. `svn-scheduler` binds them only when `listen` is a loopback address (`services/scheduler/cmd/svn-scheduler/main.go` lines 57-60). Authenticate them or remove them before any non-loopback binding.

## Next safe action

Draft pull request 23 is already open from `goal-18`. Do not merge it. Do not start goals 19-24 from this branch. Public launch stays disabled.

## Proposed pull-request title and summary

Title: Join the upload-to-playback path on the devnet

Summary: The milestone runs a synthetic clip through the API, the media worker, the broadcaster, the indexer, four provider agents, the scheduler, and delivery, against a four-validator devnet. Playback continues after the origin is marked down. Repair commits a replacement lease from a survivor. Stopping two validators freezes new commits and leaves existing playback on the local grant. Restarting those same homes continues the chain. This is one VM, one physical failure domain, and a working prototype, not a reviewed public beta. The goal stays BLOCKED on custody, that single machine, the mutation anchors, golangci-lint, PostgreSQL 16, and compose. DR-18-01 through DR-18-07 are open.

## Head CI

Run [42823652645](https://github.com/Maxencejules/shortvideo-network/actions/runs/42823652645) on `0c10f566e4a2ff9367d68f659f8559dbc96d75d9` completed **failure**. It was created at 2026-11-10T13:20:38Z. The `python` job failed because Ruff rejected `scripts/milestone/client.py`. That is not the goal 15 headless-Chrome flake. Every other job succeeded, including `milestone`.

Run [42826355319](https://github.com/Maxencejules/shortvideo-network/actions/runs/42826355319) on `6924a498a73f4edf352c63a195fda3ffa483cba9` completed **failure**. It was created at 2026-11-10T13:38:20Z. The `python` job failed in `tests/unit/test_delivery.py::test_replica_host_failure_and_chain_outage`: a new catalog advertised hosts saved by the previous process. That is not the headless-Chrome flake. Every other job succeeded, including `milestone`. No run on this branch was cancelled.

The tested commit `a5628ab5b6dfb4f1b16546654415063f1499a992` is the fix for that test.

Run [42829240686](https://github.com/Maxencejules/shortvideo-network/actions/runs/42829240686) on `f41529e9737703407e65286087e5362381d77628` completed **success** at 2026-11-10T14:08:19Z. It was created at 2026-11-10T13:57:02Z. All 17 jobs succeeded: `contract`, `devnet`, `go`, `milestone`, `node`, `python`, and the go-race shards `api`, `app`, `broadcaster`, `devnet`, `devnet-broadcaster`, `devnet-broadcaster-stall`, `devnet-evidence`, `devnet-indexer`, `devnet-upgrade`, `indexer`, and `rest`. `f41529e` differs from the tested commit only under `docs/evidence/`.

Run [42831321972](https://github.com/Maxencejules/shortvideo-network/actions/runs/42831321972) on `5aec8ea211b6e8e1c6719eff3ddc4dd480f9ed60` completed **success** at 2026-11-10T14:24:31Z. It was created at 2026-11-10T14:09:55Z. All 17 jobs succeeded, the same set as run 42829240686. `5aec8ea` differs from `f41529e` only in the Head CI paragraph.

Run [42833971143](https://github.com/Maxencejules/shortvideo-network/actions/runs/42833971143) on `cadf516dec0242618112609a4b60b153b19de6c4` completed **success** at 2026-11-10T14:38:09Z. It was created at 2026-11-10T14:25:42Z. All 17 jobs succeeded, the same set. `cadf516` differs from `5aec8ea` only by naming run 42831321972.

Run [42836248937](https://github.com/Maxencejules/shortvideo-network/actions/runs/42836248937) on `f1db3a9208105091b91d787dc62aa051cbb1f68c` completed **success** at 2026-11-10T14:54:16Z. It was created at 2026-11-10T14:39:07Z. All 17 jobs succeeded, the same set. `f1db3a9` differs from `cadf516` only by naming run 42833971143. No run on this branch was cancelled.

Docs-only commits after the tested commit start another CI run. The run on the commit that adds a sentence is not named in that sentence. This paragraph names every run that had finished on `goal-18` before this commit. The run this commit starts is a tip-lag run and is not named here.
