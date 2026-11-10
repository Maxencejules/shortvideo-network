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
| Commit | `21dc9f28dd3380a37db2a811937488d58c1e4e13` |
| Tree | `3cf8a70405b5dc2f6d0a8bf93816c2323d46933d` |
| Base | `f21ef2b22b15921600515a47f90164dede34920c` |
| Branch | `goal-18` |

[trailer.txt](logs/goal-18/trailer.txt) line 20 is `commit 21dc9f28dd3380a37db2a811937488d58c1e4e13`. Lines 21 and 22 name `Maxence Jules <Maxencejules@users.noreply.github.com>` as author and committer. Line 28 is `Tested-tree: 3cf8a70405b5dc2f6d0a8bf93816c2323d46933d`. The same file's `TREE` line is that tree. This evidence commit changes only `docs/evidence/` relative to that tested commit.

Two earlier commits on this branch are ancestors of the tested commit. They are not the tested tree. `1a89c0ea34b278e0f34b60876f61c9bad3669d1a` joined the path. `ea4a3593d838068ee3c5a63364ec2c6390720f38` removed an unused struct that `go vet` rejected. The tested commit waits for a fresh indexer read before the withheld publish. The scenario log below is that commit.

[range.txt](logs/goal-18/range.txt) is `git diff --name-only f21ef2b22b15921600515a47f90164dede34920c..HEAD`. Lines 22 through 57 are the paths. `chain/` and `proto/` are not among them. [names.txt](logs/goal-18/names.txt) is only the last commit: line 21 is `services/milestone/scenario_phases_test.go`.

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
| DR-18-01 | OPEN | The content digest registered on the chain is the sha256 of the scheduler package text, not the media worker's manifest file. [scenario.txt](logs/goal-18/scenario.txt) line 49 `manifest_sha256` is `876c4e4b770f316d97eb3a38fd26f9a72e4b4a6bd867d69aa1df817ebb62eeb5`. Line 50 `package_text_sha256` is `18d99afc90681d6614bc64c2e9ae85c87e1e329c68f1fea73b7d34dc6abdf74f`. Line 66 shows the indexer manifest equal to the package text. Both digests are real. They are not the same document. |
| DR-18-02 | OPEN | Providers `p1`, `p2`, and `p3` submit with the genesis account `provider-1`. `p4` uses `provider-2`. [scenario.txt](logs/goal-18/scenario.txt) lines 13-16. The ed25519 agent key still signs the inner acceptance. `keys/outer.hex` is a copy of the devnet secp256k1 account, created in the test temporary directory, so the transaction signer is the admitted account. It is not committed. |
| DR-18-03 | OPEN | Scheduler publication and the playback document are different fields. Line 63 is `publication=PUBLISHED`. Line 68 is `publication=NOT_PUBLISHED` with `hosting_promise=False` and `chain_status=HELD`. A local grant is not publication. |
| DR-18-04 | OPEN | An accepted lease does not name the offer (DR-08-08). The indexer closes the one OPEN offer for that provider and content in the same transaction. Zero or several matches stay OPEN. [scenario.txt](logs/goal-18/scenario.txt) line 64 is `phase=INDEXER_RECONCILE ok` after the three accepts. Line 65 is `reconciliation_status":"ok"` and `"stale":false`. |
| DR-18-05 | OPEN | A stale indexer read blocks publication and repair. The tested commit reconciles before the withheld publish so a not-yet-run reconciliation is not reported as the moderation decision. Line 54 is `publication=WITHHELD reason=community_approval`. |
| DR-18-06 | OPEN | Declared operator labels are simulation. Line 63 is `simulated=true independent=false physical=1`. Three counted replicas on one machine are not three failure domains. |
| DR-17-01 .. DR-17-08 | OPEN | Carried. The scheduler still does not sign. |
| DR-16-01 .. DR-16-06 | OPEN | Carried. DR-16-04 is closed only for this scenario's chain view. |
| DR-15-01 .. DR-15-06 | OPEN | Carried. The playback grant is still local. |
| DR-14-01 .. DR-14-04 | OPEN | Carried. The clip is a one-second synthetic ffmpeg pattern, not an approved package format. |
| DR-13-01 .. DR-13-04 | OPEN | Carried. The caller is a development fixture token. |
| DR-07-01 | OPEN | Carried. Production custody is unset. |

## Acceptance

The acceptance log is [scenario.txt](logs/goal-18/scenario.txt). `HEAD` is `21dc9f28dd3380a37db2a811937488d58c1e4e13` and `TREE` is `3cf8a70405b5dc2f6d0a8bf93816c2323d46933d`. `DIRTY no` is line 5. The other logs in this directory were captured after that file existed, so they print `DIRTY yes` and list the earlier log paths. No source file was modified between those captures.

The clip is synthetic. Line 44 is `bytes=12746 sha256=190072c758fab6a1b799d210630ca3baa449e3a55e856bf003c25331f31bafd6 synthetic=true`. The package is 12740 bytes with 2 segments (line 50). The played segment is 11524 bytes (line 71).

| Check | Status | Where |
| --- | --- | --- |
| Four-validator devnet, not the fixture chain | VERIFIED on this host, one machine | Line 7 `chain_id=shortvideo-dev-16bda2af46986326` `physical_count=1`. Line 8 `height=5` `validators=4` `app_hash=D74F954C111C6E1C6E78C7EFEB2D9BCBEB215F6108782300902343ED395A1839`. |
| Development principal only | VERIFIED as the fixture | The API publish before the chain path is line 48 `status=403`. Public sign-up is not this goal. |
| Upload, one reservation on retry | VERIFIED on this host | Line 45 `upload=ubed5374183ec8f74b02215d2bd165ef1` `repeat_upload=ubed5374183ec8f74b02215d2bd165ef1` `status=RESERVED`. The test refuses a second upload id at `services/milestone/scenario_phases_test.go:57`. |
| Bytes received, processing queued, worker ready | VERIFIED on this host | Line 46 `status=204`. Line 47 `PROCESSING_QUEUED`. Line 49 `status":"READY_FOR_REVIEW"` `publication":"NOT_PUBLISHED"` `package_bytes":12740`. |
| Moderation withheld, then three committed leases, then publish | VERIFIED on this host | Line 54 `WITHHELD` `community_approval`. Lines 56-61 are the three `OFFER_LEASE` commits at heights 56, 60, and 65. Line 63 `PUBLISHED` `leases=3 distinct=3 verified=3` at `2026-11-10T13:15:57.472259759Z`. Line 64 reconciliation `ok`. |
| Independent client plays the segment | VERIFIED on this host | Lines 66-73. Line 71 `sha256=cbcc6ec21007d20c4bcff5223825b1a3f7e8b90b5ffbed08d9d7c9ffbe31ed04` matches `expected`. Line 73 `CLIENT ok`. |
| Repeated register is one business effect | VERIFIED for the content row and the transaction | Line 74 repeats the register tx `26123e85894b0e85f29331dd2cc7495a34b1253821b9ec9c08d2c13a763b9065` at height 53, the same values as line 52. Line 75 `OPERATION_STATUS_COMMITTED`. Line 76 `content_rows=1`. The test refuses any other count at `services/milestone/scenario_phases_test.go:281`. A separate quota-balance column was not printed. |
| Corrupt one replica, a survivor still serves | VERIFIED on this host | Line 77 `availability_verified=2 distribution=200`. |
| Origin down, distribution continues | VERIFIED on this host | Line 78 `distribution=200 bytes=12740`. |
| One provider down, repair, restart | VERIFIED on this host | Line 79 `REACHABILITY_DEGRADED degraded=[p1]`. Line 82 `state":"CONFIRMED"` `source=p2 replacement=p4`. The repair offer committed at height 71, tx `10b021e55c83819b3ac743101bd78fd47b1808b7dcf40b244991335f4513e242` (lines 80-81). Line 84 `health=200`. |
| Unapproved offer refused, unauthorized accept rejected on chain | VERIFIED on this host | Line 85 `status=400`. Line 88 `height=497 code=1 want=1 txhash=77550B1C1733A071B6679D0FEF1F72D5EB288D8617B8B3A7E8606210288A817C`. |
| Quorum loss: playback continues, new commitment stays pending, resume does not reset history | VERIFIED as two of four processes stopped on this one machine | Line 89 `height_before=497 height_during=497 playback=200`. Line 90 `OPERATION_STATUS_PENDING_STALL`. Line 91 `height=502 app_hash=DD590870F75F64C0A69BD5FB5DD789120E62AE6820AE2AA7DD1B423EDAC6686E stall_height=497`. The height moved forward. The homes were not wiped. |
| Withdraw from the controlled services | VERIFIED on this host | Line 92 withdraw tx `edc6c763435ab41e82f4a687e632c2fb120e470a5d6b906a206a9c4d63d88ce0` at height 504. Line 93 `indexer=true`. Line 94 `status=410`. Line 95 `REMOVED`. |
| API and delivery status match the layer that owns them | VERIFIED as three layers, not one flag | Before publish, the API is 403 (line 48) and the worker is `NOT_PUBLISHED` (line 49). After publish, the scheduler is `PUBLISHED` (line 63) and the playback document stays `NOT_PUBLISHED` (line 68). After withdrawal, the indexer, the scheduler distribution, and delivery agree (lines 93-95). |
| Clean approved multi-machine integration runner | BLOCKED | This VM is one machine. Line 7 `physical_count=1`. |

Line 97 is `--- PASS: TestMilestone (150.83s)`. Line 98 is `PASS`. Line 100 is `EXIT:0`.

## Gate C

The evidence shows, on this one VM: a synthetic upload was processed, held for approval, then published after three committed leases on chain `shortvideo-dev-16bda2af46986326`; the independent client retrieved the segment and the hash matched; marking the origin down left distribution at HTTP 200; killing one provider left a survivor, and repair committed a replacement lease from that survivor; stopping two of the four validator processes froze the height, left existing playback at HTTP 200, and left a new register at `PENDING_STALL`; starting the same validator homes continued to height 502 with the app hash in line 91. The chain was not reset and no earlier height was rewritten.

The evidence does not show a second machine, a second operator, a network partition, or survival of losing this VM. Four processes on one host are one physical failure domain. Playback during the stall is the local delivery grant inside its authorization window (`chain_status=HELD`, `hosting_promise=False`, line 68). That grant is not a chain publication and not a promise that a lease was renewed. An approved multi-machine runner was not used.

## Other commands

| Step | Exit | Log |
| --- | --- | --- |
| runtime | 0 | [runtime.txt](logs/goal-18/runtime.txt). Line 7 `Linux 6.12.113+ x86_64`. Line 8 `go version go1.26.10 linux/amd64`. Line 9 `4`. Line 10 `Python 3.12.3`. Line 11 `ffmpeg version 6.1.1-3ubuntu5`. |
| gofmt | 0 | [gofmt.txt](logs/goal-18/gofmt.txt). No file names were printed. Line 8 `EXIT:0`. |
| vet | 0 | [vet.txt](logs/goal-18/vet.txt). Line 9 `EXIT:0`. |
| regressions | 0 | [regressions.txt](logs/goal-18/regressions.txt). Lines 10-13 are the four `ok` lines. Line 14 `EXIT:0`. |
| requirements | 0 | [requirements.txt](logs/goal-18/requirements.txt). Line 36 `errors 0`. Line 70 `result PASS`. Line 72 `EXIT:0`. |
| pytest | 0 | [pytest.txt](logs/goal-18/pytest.txt). Line 19 ends with `[100%]`. Line 20 `EXIT:0`. The files are `tests/unit/test_ci_milestone.py`, `tests/unit/test_chain_writer_policy.py`, and `tests/unit/test_delivery_hosts.py`. |
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
| REQ-DEV-003 | VERIFIED on this host for registration, three commitments, and resume without a wiped home | Lines 52, 56-61, and 91. A database replay after a destroyed disk was not run. |
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
| REQ-REC-001 | VERIFIED for this resume | Line 91 continues from `stall_height=497` to height 502. No genesis reset. |
| REQ-ACK-001 | VERIFIED as a boundary | Provider accepts print `response_committed=true counted_from_response=false` inside line 82. The lease is counted after the indexer and the chain view, not from the broadcast body alone. |

## Handoff

Repeatable demonstration: `bash scripts/milestone/run.sh` from a clean tree with Go 1.26.10, Python 3.12.3, FFmpeg 6.1.1, and PostgreSQL 18.6 on the path used in [runtime.txt](logs/goal-18/runtime.txt). `SVN_SKIP_DEVNET=1` skips the scenario. That skip is not a pass. The go job sets that variable. The `milestone` job in `.github/workflows/ci.yml` is the CI run of `TestMilestone`.

Blockers to resolve before goals 19-24:

- DEC-001 and DR-07-01 custody, before any portability or production-signing claim (Goal 19).
- One machine and simulated operator labels (DR-18-06), before any independence or onboarding claim (Goal 24).
- The goal 06 and goal 07 mutation anchors (`problems=13` and `problems=3`).
- golangci-lint, PostgreSQL 16, and the goal 04 compose bridge.
- DR-18-01 (which digest is the content digest), DR-18-02 (shared genesis account), DR-18-03 (playback publication field), DR-18-04 (offer id missing from the accept event), and DR-18-05 (a stale read blocks publication).
- Carried DR-13, DR-14, DR-15, DR-16, and DR-17 requests. Accounts, retention, and audience rules stay Goals 19 and later.

## Residual risk

`p1`, `p2`, and `p3` share one genesis account, so "distinct operators" is the declared label, which line 63 marks `simulated=true`. The outer key file is an interim so an ed25519 agent can submit as that account. The indexer closes an offer only when exactly one OPEN offer matches the provider and content. Two open offers for one pair stay open and the next reconciliation is divergent. The playback grant is local and expires on its own clock. The quorum drill stops processes. It does not unplug a machine. The height jump from the repair (height 71) to the unauthorized accept (height 497) is empty blocks on this devnet, not a second chain.

## Next safe action

Open a draft pull request from `goal-18`. Do not merge it. Do not start goals 19-24 from this branch. Public launch stays disabled.

## Proposed pull-request title and summary

Title: Join the upload-to-playback path on the devnet

Summary: The milestone runs a synthetic clip through the API, the media worker, the broadcaster, the indexer, four provider agents, the scheduler, and delivery, against a four-validator devnet. Playback continues after the origin is marked down. Repair commits a replacement lease from a survivor. Stopping two validators freezes new commits and leaves existing playback on the local grant. Restarting those same homes continues the chain. This is one VM, one physical failure domain, and a working prototype, not a reviewed public beta. The goal stays BLOCKED on custody, that single machine, the mutation anchors, golangci-lint, PostgreSQL 16, and compose. DR-18-01 through DR-18-06 are open.

## Head CI

Named in the pull request after the evidence commit is pushed. No run existed on `goal-18` before that push.
