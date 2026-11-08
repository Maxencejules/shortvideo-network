# Handover to Cursor: shortvideo-network, goals 11-30

Written 2026-11-08 by the Claude Code remote session `session_014mwakeb27YrugDfwAx8vXL`, model `claude-opus-5-5`, effort `ultracode`. That session built goals 07-10 and most of goal 11. This file is the complete state at handover. Read `AGENTS.md` first; it is the contract and is stricter than anything here.

Repository: `Maxencejules/shortvideo-network`.

## 1. Where things stand

### 1.1 Merged into `main`

`main` is at `6d74aca`.

| PR | Content | Merge commit |
|---|---|---|
| 11 | Goal 08, lease lifecycle | `d33751e` |
| 12 | Goal 09, attestations and audit evidence | `1969b2c` |
| 13 | Goal 10, policy governance and chain upgrades | `b7aaf75` |
| 14 | Auditor run kept within the 65536-byte pending-run limit (PR 12 review finding) | `dbf0897` |
| 15 | `svnd genesis validate` applies InitChain's `Version.App` rule (PR 13 review finding) | `6d74aca` |

`docs/progress.md` on `main`:
- goals 00, 01, 05 and 06 are VERIFIED;
- goals 02 and 03 are IMPLEMENTED;
- goals 04 and 07-10 are BLOCKED, each for its recorded reason (owner decisions, independent operators, live keys);
- goals 11-30 are NOT STARTED in the tracker.

Merged does not mean VERIFIED.

Two review threads are deliberately left unresolved: the owner has not authorised resolving them.
- PR 12 `discussion_r4504700195`, fixed by PR 14.
- PR 13 `discussion_r4505012913`, fixed by PR 15.

### 1.2 Goal 11 branches

| Branch | Head | What it is |
|---|---|---|
| `goal-11` | `2e364d4` (tree `ba524cc4`) | The goal 11 "tested code" commit. It sits on `main` `6d74aca`, after WIP commits `603fde1` (Wave 0) and `5e43196` (Wave 1). All committed work is green at this commit, except two intermittent test failures, which are fixed on the handover branch (section 3.1). |
| `goal-11-handover` | `goal-11` plus 2 commits | Commit 1, WIP and untested: the reviewed flake fixes, plus the partial, unreviewed F1/F3 fix. Commit 2: this `handover/` folder. **Never merge the `handover/` folder.** Take commit 1 only, finish it, and commit on `goal-11`. |

No goal 11 PR exists. The owner authorises each PR and each merge separately (AGENTS.md).

## 2. Goal 11: what is built

Goal spec: `docs/goals/11-transaction-broadcaster-and-outbox.md`.

`services/broadcaster` is now a Go module. It replaces the Python scaffold; the replacement is recorded in its README and in `tests/unit/test_scaffold.py`. It is pinned to `chain/go.mod` versions and is a member of `go.work`.

Design document (PROPOSED; approval is asked at PR time on a pinned sha256): `docs/architecture/broadcaster.md`. Read sections 4 (processes, subcommands, start order), 5-12, 18 (open decision requests) and 21 (residual risks).

| Package | Role |
|---|---|
| `cmd/svn-broadcaster` | Subcommands: `serve`, `signer`, `migrate`, `reconcile-once`, `status`, `signer-clear`, `dev-ca`, `dev-db` |
| `internal/signer`, `keys`, `policy`, `logx` | A separate signer process is the only code that reads key bytes or signs. It enforces: per-key type-URL allowlists; chain id; a deadline bound read from its own node; one message per transaction; no `authority_end`; the operator intent-key registry for register/withdraw; a provider-signature pre-check for relays; strict checks on verbatim forwards; log redaction. |
| `internal/api`, `config` | Caller API over mTLS on loopback (principal from the client certificate). Typed requests and idempotency keys. Object permissions: another caller's object answers "not found". Status values: requested, submitted, committed, failed, uncertain/pending. |
| `internal/worker`, `faultpoint` | Per-signer worker with advisory-lock fencing. It persists an attempt, counts `sends_started`, and only then broadcasts. It classifies answers and applies the dead rule. It reconciles by tx hash and business id before any resend. Bounded expiring queue during a chain outage, with stall episodes. Fault points are compiled only with `-tags svn_faultpoint`. |
| `internal/store` | PostgreSQL 18.6 outbox. Migration `0001` holds the core schema with terminal-row immutability triggers. Migration `0002` holds the owner-answer tables: content owners, signer classes, forwarded transactions, clearing. |
| `internal/chain`, `outbox` | Codec (prepare/assemble with no key), hash-checked RPC reads, answer classifier, effect comparison per kind, and the pure state machine with property and fuzz tests. |
| `chain/app/tx_deadline_test.go` | Test-only: `timeout_height`, `tx.acc_seq` and Simulate semantics through FinalizeBlock. |
| `chain/devnet/broadcaster*_test.go` | `TestDevnetBroadcasterLifecycle` runs about 30 phases against four real svnd processes, real PostgreSQL and the real binary. Faults are injected by a loopback proxy and by build-tagged fault points, and `checkAccounting` runs after every phase. `TestDevnetBroadcasterQuorumStall` stops quorum. |
| `docs/evidence/goal-11/mutants.py` | Mutation runner: 281 mutants with unit, PostgreSQL and devnet killers. |
| CI (`.github/workflows/ci.yml`) | PostgreSQL 18.6 is installed (pinned PGDG package plus key fingerprint) in the go job and in the race shards. New shards: `go-race (broadcaster)`, `go-race (devnet-broadcaster)`, `go-race (devnet-broadcaster-stall)`. Guard tests: `tests/unit/test_ci_broadcaster.py`, `test_chain_writer_policy.py`. |

### 2.1 Owner answers for goal 11

Answered on 2026-11-08 at 07:44Z through the Claude Code question dialog. The verbatim record is in `handover/goal-11/owner-questions-asked.md`. Quote the labels exactly; never present them as approval of the boundary document, numbers, PRs or merges.

- **Q1 "Keys":** "Both keys, separate signer (Recommended)". One `BROADCASTER` key and one `ADMITTED_SERVICE` key, read only by a separate signer process under its own non-root uid.
- **Q2 "Provider sig":** "Relay + verbatim forward (Recommended)". Relay provider-signed accept/renew/end after a pre-check; forward provider-signed transactions byte-for-byte only with a bounded deadline. Provider self-submission comes in goal 16. Audit batches stay on the auditor's own path.
- **Q3 "Publish bit":** "Operator-registered intent keys (Recommended)". This is not DEC-001 custody: a devnet fixture holds the intent keys as a test stand-in.
- **Q4 "Outbox data":** "Minimal + clear unsent (Recommended)". Digests appear in answers. When nothing was ever sent, signatures, intents and the authorization context are cleared and their digests kept. Retention stays unset.

### 2.2 Orchestrator rulings

These are reversible implementation choices, not owner decisions. They are recorded in the reports under `handover/goal-11/`.

- **R1:** dependency identity means three things: every dependency line of `cd chain && go list -m all` is identical; the `GOWORK=off` chain listing is byte-identical; and `go version -m svnd` is identical after line 1. Workspace mode always adds the new main-module line.
- **R2:** a committed failure without `tx.acc_seq` keeps the attempt live until height ≥ T+L. CometBFT lets ante-failed bytes be resubmitted.
- **R3:** Simulate goes through the gRPC-routed `/cosmos.tx.v1beta1.Service/Simulate`; `/app/simulate` cannot encode svn responses. This is verified by `TestSimulateRouteGasForBroadcasterKinds`.
- **R4:** stricter relay signer rule. A hash signed by our key that we never stored is a SIGNER_CONFLICT.
- **R5:** `business_id` is excluded from the effect field comparison. It is reused across all attempts of one business attempt.
- **R6:** an SQL-path replay must repeat the kind and the auth context.
- **go.mod tidy:** both `go.mod` files were tidied. In `services/broadcaster`, unused indirect lines were dropped. In `chain/go.mod`, `lib/pq v1.12.3` moved from indirect to direct, because the devnet tests import it. `go.sum` and every selected version are unchanged.

## 3. Goal 11: what remains, in order

### 3.1 Finish the fixes on `goal-11-handover` commit 1

The evidence capture on `2e364d4` found two intermittent failures. Both were root-caused, fixed and reviewed; details are in `handover/goal-11/flakes-report.md`.
- **Worker test.** `TestEffectCheckBeforeRetry` and its sibling `TestDeadlinePassedRetriesAtTheSameSequence` had a test synchronisation defect. A full-jitter backoff of a few milliseconds let the same signer pass start the retry. They now use `settleLive`, and `TestBackoffIsFullJitter` was added, killing mutant `w3fix-backoff-never-waits`. Before the fix: 24 of 100 failed under load. After: 0 of 200.
- **Devnet phase MR.** The test read node 0's tx index 1-5 ms before CometBFT's indexer goroutine wrote it. It now waits for `/tx` at the committed height first. MR passed 10 of 10 and the full lifecycle 3 of 3. The production worker never treats an empty index search as "sequence unused"; this was checked.

The same review found a **product defect, F1**, plus two related items. They are only partly fixed in commit 1, and that part is unreviewed and untested beyond `go build` and `go vet`:

| Item | State | What is needed |
|---|---|---|
| **F1** | Partial edits in `chain/rpc.go` (AppFloor), `signer/signer.go`, `api/authorize.go`, `policy/{forward,relay}.go`, `worker/validate.go`, plus new tests (`worker/readfloor_test.go`, api/signer/rpc tests) | CometBFT `/status` reports `BlockStore.Height()`, which can be one block ahead of the app's committed height while a block executes (`consensus/state.go` saves block H before `ApplyVerifiedBlock`). The signer passes `Status().Height` as the minimum height of its start-up governance query, so it can exit 3. Seen once under load. API authorize and relay paths answer 503 for the same reason. Finish all of the following: use a committed floor everywhere `Status().Height` is a query floor; keep time and height consistent in the relay pre-check; make signer start-up wait with a bound and fail clearly; prove the tests fail on the old code; review. |
| **F3** | Partial edits in `config/numbers.go`, `outbox/limits.go` and tests | The lowest allowed `indexing_margin_blocks` value of 1 is unsafe: at status S, only blocks up to S-2 are certainly indexed. Raise the hard bound to 2, and update the bound table in `broadcaster.md`. |
| **F4** | Not started | `chain/devnet/broadcaster_api_test.go` `post()` retries 503 up to 10 times, which masks F1. After F1, measure. Fail on any 503 unless it is a documented, deliberate one. |
| Docs and mutants for F1/F3 | Not started | Update `broadcaster.md` and the README. Add mutants for the floor rule and the margin bound, and keep `mutants.py --check` green. |

The workflow prompt that specifies this work exactly is in `handover/goal-11/height-floor-task.md`.

### 3.2 Prove and make the new tested commit on `goal-11`

Run at least:
- `go test -race ./services/broadcaster/...`;
- `go test -race -count=30` on the two worker tests, under CPU load;
- the devnet lifecycle test 3 times (once with `-race`) and the stall test twice, one devnet run at a time;
- `mutants.py --check --worktree`, and the groups whose files changed;
- `go mod tidy -diff` for `services/broadcaster` and `chain`;
- the CI guard pytest files;
- `scripts/scan_secrets.py`.

Then commit on `goal-11` without the `handover/` folder. Commit messages must not contain a model id.

### 3.3 Evidence captures on the new commit

Template: `handover/goal-11/run-w3.sh`. Adapt `S`, `WT` and `NETNS` to your machine. Every number in the evidence file must come from a log made by `scripts/evidence/capture.sh` on the committed tree with `DIRTY no`. Lessons from the first attempt:
- **Leave `TMPDIR` unset (`/tmp`).** pgdev needs `os.TempDir()/svn-pg-*` with a socket path of at most 107 bytes. The root-only uid tests run children as unused uids that cannot traverse private directories. The first capture segment failed 7 steps for this reason; those logs were archived as non-evidence in the old container and are lost with it. Disclose that this happened.
- **`.venv`, `.toolchains` and `node_modules`.** If they are symlinks into another checkout, `tests/unit/test_repo_policy.py::test_ignore_rules_cover_private_material` fails on `node_modules/react/index.js`. Goal 10 recorded this and added a clean-clone Python run (`tests-python-clean-clone`); keep both logs. Real directories avoid the problem.
- **Devnet and PostgreSQL runs.** Run one at a time. They use fixed loopback ports, so use a private network namespace (`handover/netns-run.sh`, which needs root and `unshare`) or make sure the ports are free.
- **Mutation runners.** Use `--rev <tested commit>`, with `--gocache` where supported, to save disk.
- **Required captures that cannot pass here:**
  - `golangci-lint` cannot load go1.26: BLOCKED, keep the command.
  - Goal 06 and goal 07 runners have 13 and 3 missing anchors: known, BLOCKED.
  - The non-root pgdev path (CI only) and head CI (needs a PR): BLOCKED until they run.

### 3.4 Evidence file and tracker

- Write `docs/evidence/goal-11.md` in the shape of `docs/evidence/goal-10.md`: status line, model and effort, tested revision, prerequisites, acceptance table (check, status, log), requirements rows, commands, blockers, follow-ups, residual risks, next safe action, proposed PR title and summary.
- Update `docs/progress.md` row 11 and `docs/progress.tsv`. The checker enforces that they agree. The expected status is **BLOCKED**:
  - DR-11-01: the signing-boundary prerequisite (`broadcaster.md`) is not approved;
  - REQ-CHAIN-013 is blocked by DEC-001 and DR-07-01;
  - head CI has not run;
  - single machine, not independent operators.
- Update the requirements-to-tests matrix (`docs/requirements.md` block `requirements-matrix`); `check_requirements.py` must stay green.
- **Model and effort.** Waves 0-2 and the flake fixes ran under Claude Code: model `claude-opus-5-5`, effort `ultracode`. Sources:
  - `handover/goal-11/get_session-2026-11-08T1951Z.txt`;
  - the transcript tally `handover/goal-11/model-tally-goal11.txt` (all `claude-opus-5-5`: main loop 173, plan 1312, Wave 0 1301, Wave 1 2142, Wave 2 1330 messages).

  Record the Cursor model and effort for whatever you do. The playbook assignment for goal 11 is GPT-6 Astra HIGH.

  The owner wrote in the Claude Code session: "I acknowledge the model substitution for DR-06-07" and "extend the acknowledgement to goals 07 through 30". That covered substituting Claude Code. For the Cursor part, AGENTS.md's "Model substitution" section applies. Record exactly what ran.
- **Disclose:**
  - the first capture attempt on `2e364d4` and why it failed;
  - the two intermittent failures, their root causes and fixes;
  - the F1 product defect;
  - the tidy changes;
  - that the Codex PR review hit its usage limit on PR 15. Codex code reviews will not run until the limit resets.

### 3.5 Ask the owner

Ask in one message:
- whether to open a draft PR for goal 11;
- whether to approve `docs/architecture/broadcaster.md` at its committed sha256 (DR-11-01).

Merging is a separate approval.

## 4. Open decision requests for the owner (goal 11)

See `docs/architecture/broadcaster.md` section 18. Each has a fail-closed interim:
- DR-11-01: approval of the boundary document.
- DR-11-05 to DR-11-13.
- Section 18.17, not yet numbered:
  - lifting INTEGRITY, UNAUTHORIZED, SEQUENCE_GAP or ACCOUNT_NUMBER_CHANGED halts (interim: none; the devnet recovery is a new database);
  - offer and relay rights per principal (interim: per caller);
  - `principal_id` after Q4 clearing (interim: kept);
  - the reason for SQL-path conflicts (interim: INVALID);
  - an operator-cleared foreign hash found again by the effect check (interim: held until expiry).
- New CI check names need a branch-protection update: `go-race (broadcaster)`, `go-race (devnet-broadcaster)`, `go-race (devnet-broadcaster-stall)`.

## 5. Follow-ups outside goal 11, all not done

1. **Stored genesis.** CometBFT stores the genesis in `data/state.db` before the handshake, and `chain/node` compares only the chain id. A home whose first start InitChain refused stays stuck after the file is corrected, until `data/` is removed. Proposed fix: compare the stored document with the file and refuse with an actionable message. A suggested-task prompt was queued in the Claude app.
2. **Validate parity gaps.** `svnd genesis validate` does not decode `app_state.auth`: a malformed one passes validate and fails at start. A CometBFT document with an empty `app_state` takes the fallback branch. Both are older than PR 15.
3. **DR-10-44.** Fold the 3 open divergence classes pinned by `TestUnnamedParseDivergences` into G-11 (test and oracle only).
4. **Mutation runners and lint.** Goal 06/07 runners have missing anchors; golangci-lint cannot load go1.26.
5. **Old scratch.** Clearing the old scratch of goals 06-10 in the Claude container needed owner permission. It no longer matters after the handover.

## 6. Goals 12-30

None is started.
- Goal 12 (committed-state indexer) consumes goal 11's canonical commitment evidence (`broadcaster.md` section 9.5): tx hash, height, result code, events with message index, intent id and business id.
- The process that worked for goals 07-11:
  1. read the goal spec, AGENTS.md and progress;
  2. parallel readers write maps (protocol, chain, decisions, runtime);
  3. a planner writes the plan, three critics review it (spec/security, tests/evidence, scope/honesty), and a reviser updates it;
  4. ask the owner at most 4 security-critical questions, each with a recommended default;
  5. waves of implementation with disjoint file ownership; each wave ends with a gate, adversarial reviews and a fix round;
  6. commit the tested code;
  7. captures, evidence file and tracker;
  8. ask about a PR.
- `handover/goal-11/plan.md` is the latest full plan and shows the expected depth.
- Track the decisions that block later goals: DEC-001 (custody), DEC-002 (independent operators), DEC-003/004/006/007 (policy, retention, licence, privacy), DEC-005 (allowances).

## 7. Pitfalls learned (save yourself hours)

- **DR-10-41.** Before the first commit after a load, CheckTx verifies signatures with account number 0. Tests must pick a signer with a nonzero account number and wait for height to advance after a restart.
- **`/status` height vs app height.** `/status` can be one block ahead of the app's committed height (F1). The tx index lags `broadcast_tx_commit` by milliseconds (MR).
- **Disk.** The Go build cache grows to more than 10 GB. `go clean -cache` is safe.
- **No `git stash`, `checkout` or `reset`** in a worktree a running test uses: it contaminates the run.
- **PostgreSQL** refuses to run as root; pgdev runs it as user `postgres`.
- **Workspace listing.** `go list -m all` in workspace mode lists every `go.work` module (R1).
- **Evidence is per revision.** After any code change, every capture is re-run on the new commit.
- **Commits.** No model id in commit messages or PR text. Attribution trailers are fine.

## 8. Contents of `handover/`

| Path | Content |
|---|---|
| `HANDOVER.md` | This file |
| `netns-run.sh` | Loopback-only network namespace runner (root, `unshare`) |
| `brief.md` | The shared brief all Claude agents used: conventions, environment, evidence rules, the owner's review standard |
| `notes-orchestrator.md` | The orchestrator's running notes for goals 08-11, with timestamps, rulings and incidents |
| `goal-11/plan.md`, `plan-r1.md` | Goal 11 plan, revision 2 (applied; changelog in section 21) and revision 1 |
| `goal-11/map-{signing,chain,decisions,runtime}.md` | Reader maps with file:line citations; base `dbf0897`; app.go lines after 478 moved by +14 in `6d74aca` |
| `goal-11/owner-questions.md`, `owner-questions-asked.md` | The questions as planned, and as asked with the owner's selections |
| `goal-11/w0-reports.md`, `w1-reports.md`, `w2-reports.md` | Verbatim reports of each wave: what was built, deviations, every command and result, review findings |
| `goal-11/flakes-report.md` | Root causes of the two intermittent failures, and findings F1-F4 |
| `goal-11/height-floor-task.md` | The exact task text for finishing F1/F3/F4 |
| `goal-11/run-w3.sh` | The capture script template (TMPDIR fix included) |
| `goal-11/get_session-2026-11-08T1951Z.txt`, `model-tally-goal11.txt` | Model and effort records for the Claude Code part |
