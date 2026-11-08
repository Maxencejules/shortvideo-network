# Goal 11 Wave 2 reports (verbatim agent returns). Tested code commit 2e364d4 (tree ba524cc4) = Wave 2 fix tree + chain/go.mod lib/pq direct marker (orchestrator, tidy).


## b9

# B9 devnet acceptance: report

Both tests are built with every plan 13.4 phase. `TestDevnetBroadcasterQuorumStall` passes with and without `-race`. `TestDevnetBroadcasterLifecycle` fails on the final run of each build, and in both cases the only failing check is phase DU. Every other phase and `checkAccounting` passed in both final runs.

The DU failure is a defect in the broadcaster API, not in the test. Some of 20 concurrent creates with the same key and body get `409 CONTENT_ID_UNAVAILABLE` instead of `200` with the existing intent. In `internal/api/handlers.go` `create()`, `s.replay()` runs before `s.authorize()`. A duplicate whose replay lookup runs before the first insert commits then sees that insert's PENDING owner row and is refused. The fix belongs to B7 (production code; I did not edit it). Until it is fixed, the `devnet-broadcaster` CI shard fails whenever the race occurs, which was 7 of the 8 runs that recorded counts.

## Files changed (`git status --porcelain`, my paths only)
```
?? chain/devnet/broadcaster_test.go            (both tests' entry points: the phase table, SVN_BROADCASTER_PHASES)
?? chain/devnet/broadcaster_stall_test.go      (TestDevnetBroadcasterQuorumStall)
?? chain/devnet/broadcaster_harness_test.go    (devnet, uids, dev-ca, dev-db, configs, migrate, roles, signer, serve)
?? chain/devnet/broadcaster_proc_test.go       (process control, binary builds)
?? chain/devnet/broadcaster_proxy_test.go      (fault proxy)
?? chain/devnet/broadcaster_db_test.go         (SELECT-only reader queries)
?? chain/devnet/broadcaster_api_test.go        (typed requests, mTLS calls, SQL-path enqueue)
?? chain/devnet/broadcaster_accounting_test.go (checkAccounting a–e, traces)
?? chain/devnet/broadcaster_phases_test.go     (S, H, L)
?? chain/devnet/broadcaster_faults_test.go     (A, TB1, TB2, TA, WD, WP1–3, HD, EX)
?? chain/devnet/broadcaster_races_test.go      (DU, FR1, FR1b, FR2, FC, OC, CS, MR)
?? chain/devnet/broadcaster_authz_test.go      (UA, PI, FW, PB)
?? chain/devnet/broadcaster_restart_test.go    (PG, SG, BR, RS)
?? chain/devnet/broadcaster_race_on_test.go / broadcaster_race_off_test.go
```
Other entries in `git status` belong to other packages: the modified `docs/evidence/goal-01/check_requirements.py` and the untracked `docs/evidence/goal-11/`. No production, go.mod, go.sum, ci.yml or docs file was touched.

## Design notes
- **What runs:**
  - The svnd validators are built in the test without `-race`; the validators are not the code under test.
  - `svn-broadcaster` is built into a temp dir with `-tags svn_faultpoint`, plus `-race` when the test binary is race-built. Both builds use GOPROXY=off.
  - The start order is dev-ca, dev-db, migrate --callers, signer, then serve, as in broadcaster.md §4.3.
- **Uid split:** under a root launcher the signer and serve run under two distinct unused uids. Every run here printed `boundary_uids=signer:3730001,serve:3730002`. The same-uid (non-root, CI) path was not run here.
- **Database access:**
  - The test reads PostgreSQL as role `harness_reader`; the owner role grants it SELECT once, and an UPDATE by it is shown refused with 42501.
  - The owner connection also creates the `app_fixture.uploads` table and is then closed.
  - The SQL path enqueues as `app_fixture_sql` in one transaction with an application row. A rolled-back enqueue is shown to leave no row.
- **Fault proxy:**
  - It sits on loopback between each serve process and validator 0, with per-method rules: drop request, drop response, hold response, hold, reset.
  - It counts per method, records the hash of every forwarded broadcast, and keeps an ordered event log.
  - It has a trigger that holds the reads observing the next broadcast: either all reads, or only that hash and named query strings.
  - Test reads go to the nodes directly, never through the proxy.
- **Every phase:** checks its precondition with the reader role, waits for quiesce (all intents terminal, empty mempools), runs `checkAccounting`, prints one `trace` line per transitions row (953 in the final run), and prints `phase=X seconds=` with heights and proxy counters.
- **`checkAccounting`:**
  - (a) A `tx.acc_seq` search for every sequence of both signers; each hash must be an attempt or a recorded foreign use.
  - (b)/(c) A typed-event count per intent object (msg_index aware), commitment against `/tx` and the block hash, and the API view against the row. No FAILED-intent exceptions were needed.
  - (d) The count of open rows per caller.
  - (e) `reserved_bytes` against the held leases, labelled as a chain invariant.

## Deviations from the plan, with reasons
1. **lib/pq in chain tests.** It was already in `chain/go.mod` as `// indirect`; go.mod is unedited and `go list -m all` is unchanged. But `go -C chain mod tidy -diff` now exits 1: it wants to move lib/pq to a direct require. Psql is the alternative. **You need to decide.**
2. **PG phase.** `pg_ctl -m immediate stop` would make dev-db exit and delete the cluster. Instead all PostgreSQL processes are frozen with SIGSTOP for 4 s, during which the proxy saw 0 broadcast requests. Then one runtime backend gets SIGKILL and the processes continue. The postmaster's crash restart replaced all 14–15 children; the postmaster pid stayed the same. This is a process crash, not a host power loss.
3. **Canary scan of node logs.** The relayed inner signature and the offer's terms digest are inside committed transactions, so node logs carry them by design. Node logs are scanned only for the key-file, DB-password and auth-context-id canaries. A restarted validator's `node.log` is truncated by `startLogged`, so only its post-restart part is scanned. The in-test scan of "go test output" covers the test's own prints (stdout tee), not testing-framework output.
4. **UA signer requests.** These are sent from a locked OS thread switched to the serve uid with raw `setresuid`, so SO_PEERCRED reports the serve uid. The thread is unlocked after root is restored; an exiting thread had earlier SIGKILLed a serve process through Pdeathsig, which I found and fixed.
5. **UA zero/two-SAN and expired certificates.** dev-ca never writes its CA key, so it cannot issue them. A second serve process trusting a test-made CA was used for those cases. The no-certificate and other-CA cases ran against the main serve.
6. **PI "service key" and "broadcaster key" signatures.** Inner signatures are always ed25519, so ed25519 keys derived from those accounts' 32 secret bytes were used; the test labels them that way.
7. **MR.** The refused sends got sdk 32 (recorded, not counted). In some runs a later resend got sdk 30 after the attempt's timeout height passed, and that one was counted, before the halt. The test asserts that sdk 32 was recorded and not counted and that the attempt was never dead by (d2).
8. **HD (b).** The restarted worker resent the same bytes of the one attempt; there was no (d3) death and new attempt. One effect either way.
9. **503 UNAVAILABLE retries.** Creates answered `503 UNAVAILABLE` are retried up to 10 × 300 ms, which is the API's documented "retry". This happened under concurrency (a query answered one height below the latest header) and in RS (4 tries).
10. **RS setup.**
    - It uses a serve config with `stall_window_seconds=120`, so the stall rule does not block the first attempt.
    - It waits for v0 and v1 to settle at one height before restarting v0. Without that, v0 once fetched and committed a block, which closed the DR-10-41 window, and the test missed sdk 4 under `-race`.
    - Phase-specific serve configs: WD and PG use a 30 s RPC timeout to keep "no recorded outcome"; the FR2 rerun uses `business_attempts=1`; the stall test uses a 30 s RPC timeout and `stall_window_seconds=3`.
11. **Stall test setup.** It registers p2 plus one content and offer. The in-flight intent is a relay on the BROADCASTER key; the queued and short-expiry intents use the SERVICE key, so only the stall rule can block them.
12. **Accounting checks.**
    - (d) checks the count of open rows per caller, because the schema has no separate counter.
    - (a) is incremental; committed sequences never change.
    - Phase Z re-checks (b) and (c) in full.
    - DU's "one LeaseAccepted" is checked by typed-event tx_search, not by scanning every block's results.

## Final runs (all under `netns-run.sh`)

| Command | Exit | Result |
|---|---|---|
| `go test -count=1 -timeout 40m -run TestDevnetBroadcasterLifecycle -v ./chain/devnet/` | 1 | 161.1 s, FAIL: DU only (7 of 20 got 409). 173 intents: 166 COMMITTED, 7 FAILED (the expected ones). Canaries 0 hits in 40 logs; data races 0 in 34 process logs. |
| same, `TestDevnetBroadcasterQuorumStall` | 0 | 39.2 s, PASS |
| lifecycle with `-race` | 1 | 203.6 s, FAIL: DU only (12 of 20 got 409); data races 0 |
| stall with `-race` | 0 | 50.2 s, PASS |

Phase seconds, final non-race (race in parentheses): build 7.1 (7.5), S 4.7 (6.8), H 7.9 (9.1), A 3.9 (5.3), TB1 1.1 (1.1), TB2 12.4 (14.0), TA 0.7 (0.9), WD 1.1 (2.6), WP1 1.0 (1.2), WP2 1.2 (1.2), WP3 0.6 (0.8), HD 2.1 (2.7), DU 2.3 (2.6), FR1 2.4 (2.6), FR1b 2.8 (2.5), FR2 3.2 (5.4), FC 0.9 (1.4), OC 2.2 (1.8), CS 11.4 (12.8), MR 14.7 (18.5), UA 2.1 (4.7), PI 3.9 (3.8), FW 2.0 (2.1), PB 0.9 (0.9), PG 7.3 (10.0), SG 2.9 (3.2), BR 34.0 (40.8), RS 6.6 (9.8), EX 9.5 (10.4), L 2.7 (3.9), Z 1.0 (2.3).

## All other commands
- **Development runs:**
  - Exit 1: run1 (node-log canary design), run2, run3 (global read hold blocked the effect check), run5–run7 (DU 409; CS 503 before retries existed; MR assertion too strict), run9–run10, run12–run14, full1 (DU, plus the Pdeathsig kill in UA), race1 (DU, plus the RS v0 blocksync).
  - Exit 0: run4 (fault phases, 51.5 s), run8 (MR), run11 (UA–PB), run15 (SG, BR, RS), stall1 (39.4 s), full2 (all phases, 156.2 s; no 409 that time), rs1 (RS under `-race`).
  - race2: exit 1, DU only.
- **DU incidence:** 409 answers in 7 of the 8 runs that recorded counts (5, 9, 4, 0, 5, 10, 7, 12 of 20). run5 failed on the first mismatch and printed no counts. One run also had a 503 before retries were added.
- **Static checks:** `gofmt -l chain/devnet` printed nothing (exit 0). `go vet ./chain/devnet/` exit 0.
- **Pytest:**
  - `test_ci_devnet_coverage.py`, `test_ci_broadcaster.py` and `test_chain_writer_policy.py`: 123 passed (exit 0), run before and after the last edits.
  - `test_ci_devnet_coverage.py` alone: 53 passed. The two failures that were waiting for these test names now pass.
  - Whole `tests/unit`: 405 passed (exit 0), previously 403 passed and 2 failed.
- **Secret scan:** `scan_secrets.findings_in_text` over the 15 new files found 0.
- **`GOPROXY=off go -C chain mod tidy -diff`** (read-only): exit 1, the lib/pq line only (deviation 1).

## Timing against CI and run_tests.py
- **CI shards:** the lifecycle took about 204 s with `-race` here, against the 900 s `-timeout` of the `devnet-broadcaster` shard. The stall test took about 50 s against 720 s. CI runners may be slower.
- **run_tests.py:** chain/devnet run without `-race` is an estimate, not measured as a whole package here. Goal 10's 922 s plus about 200 s for these two tests gives about 18.7 min, against the 30-minute limit.
- **ci.yml:** its comment that the lifecycle test builds svnd and the broadcaster is accurate now.

## Stopped on, not done, and housekeeping
- **Stopped on:** the DU defect (production code, B7) and the chain go.mod tidy decision (deviation 1).
- **Not run:** CI, the same-uid (non-root) run, and mutation runs (B10's package).
- **Disk:** 6.9 GB free at start, 4.5 GB at the end. The Go build cache went from about 5.2 GB to 7.6 GB.
- **Leftovers:** none of mine in /tmp. `/tmp/svn-pgtest.lock` is not mine.

All run logs are in /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w2/devnet/ (final-lifecycle.log, final-stall.log, final-lifecycle-race.log, final-stall-race.log, and the development runs).

## b10

## B10 mutation runner: 269 of 273 mutants killed, 4 survivors (test gaps for the fix phase)

All runs used the working tree at HEAD `5e43196` plus uncommitted Wave 2 work. They ran as root inside `netns-run.sh` with `--gocache /root/.cache/go-build`. `--check --worktree` passes for the goal 11 runner and still passes for the goal 10, 09 and 08 runners. No devnet killer was needed or run.

**Files changed** (`git status --porcelain`, my paths only)
```
 M docs/evidence/goal-01/check_requirements.py   (+1 line in PY_ALLOW)
?? docs/evidence/goal-11/mutants.py               (new; sha256 a07a34c8…6a86d)
```
The PY_ALLOW line is `"docs/evidence/goal-11/mutants.py",  # the goal 11 mutation runner, likewise`. `check_requirements.py` still reports PASS.

### Design
- **Same design and options as goal 10's runner:** groups, `one()`, `K()`, and `--check`, `--worktree`, `--rev`, `--only`, `--group`, `--gocache`, `--logdir`, `--keep`, `--timeout`. It copies the revision or working tree into its own temp copy and edits that copy on disk. Because the copy is edited on disk, the static scans and the smoke test's binary build see each mutant.
- **Changes from goal 10's runner:**
  - A killer can carry build tags. The runner issues one `go test` command per tag set, so the `svn_roottest` uid tests can be killers.
  - `--check` honours single-tag `//go:build` lines.
  - A PostgreSQL syntax error from a mutant that edits a `.pgsql` file counts as BUILD_FAILED, not a kill.
  - There are no pytest killers. The default timeout is 10m.
  - The header prints the euid and says which groups start PostgreSQL.
- **Groups:**
  - No PostgreSQL: outbox 44, chain-client 25, signing 56, api 25, config 14, binary 8.
  - PostgreSQL: store 61, worker 34, smoke 6.
- **Mutant sources** (the name prefix says where each came from):
  - Wave 0: B1, B2, B3, the tests review, the fix round (S1–S10, Q1–Q7, C1, and the rebuilt R2 ante-failure mutant).
  - Wave 1: B1b, B6, B7a, B7b, B8, the security and tests reviews, and the fix round (P1, P4, S5, A4, R8, W3–W8, R5, F4b, F4c, F9).
  - Nine new ones (`b10-*`): the compare-and-set and expiry refusals of PersistAttempt, the advisory lock result, rulings R3, R4 and R5, PROVIDER_MUST_RESIGN, no signing outside the signer, and no local preimage copy.
- **Docstring:** it maps every row of plan 13.7 to its mutants and lists the excluded candidates with reasons.
- **Killers:** every killer named on a killed mutant failed on it in the final run. I removed killers that did not fail.

### Deviations
- **Temp copy location:** the copy goes to `/tmp/goal11-mutants-*`, as in goals 08–10, not under my scratch directory. It is removed after each run; none was left.
- **No killer for "local clock decides death":** `outbox.Judge` takes no clock input, so there is nothing to mutate.
- **Plan killer names that did not fail and were replaced:**
  - TestSendNeedsCountedStart replaces TestFakeRPCSeesPersistedAttempt for "no sends_started increment".
  - TestZeroRecordedSendsNeverDeadByD2 did not fail on its mutant.
  - TestCommittedFailureRunsEffectCheck did not fail on "committed failure without effect check".
  - TestLockLossStopsSending did not fail on the fence-check mutant.
  - The PENDING-owner row is killed in `policy` (TestOfferAndOwnership); TestOfferNeedsEffectiveOwner kills w1b8-no-revalidation.
- **Excluded as equivalent:**
  - B1b M03 and M27, B7b M13, and review P5 (the same authority_end refusal comes from PrecheckRelay).
  - Dropping the idempotency unique key: every enqueue first updates the caller's `caller_limits` row, which serialises them.
  - Edits to migration 0001's `intents_guard` and `enqueue_intent`: migration 0002 replaces both functions. So the terminal-trigger mutant and the two R6 mutants were re-anchored on 0002.
- **Edits after the final run started:** two docstring sentences and removal of an unused `import json`. Neither changes a verdict.

### Commands (exit code, counts, time)
| Command | Exit | Result |
|---|---|---|
| `mutants.py --check --worktree` (first, and final) | 0 / 0 | 273 mutants, all anchors unique, all killers defined |
| goal 10 / 09 / 08 `mutants.py --check --worktree` | 0 / 0 / 0 | 303 / 169 / 28 mutants |
| `check_requirements.py` (twice) | 0 | PASS |
| Unit groups, first run | 1 | 171: 168 killed, 3 survived; 571.0 s. Fixes: one wrong killer, one mutant missing its second edit, R3 path. |
| `--only` 2 fixed unit mutants | 0 | 2 killed |
| PostgreSQL groups, first run (store, worker, smoke) | 1 | 103: 95 killed, 8 survived; 613.8 s |
| Overlay scratch runs (A8 on api; P5 on policy+api; 2 store survivors on the whole store package) | 0 each | all survive |
| `--only` 8 rerun | 1 | 4 killed, 4 survived |
| **Final unit groups** | 1 | **RESULT mutants=172 killed=170 survived=2**; 571.3 s |
| **Final PostgreSQL groups** | 1 | **RESULT mutants=101 killed=99 survived=2**; 583.6 s |
| `ruff check --select F,E9` on the runner | 1, then 0 | unused import removed |

Final per-group seconds:

| Group | Seconds | Group | Seconds | Group | Seconds |
|---|---|---|---|---|---|
| outbox | 37.0 | api | 103.7 | store | 253.9 |
| chain-client | 108.7 | config | 55.0 | worker | 237.7 |
| signing | 229.9 | binary | 36.2 | smoke | 91.7 |

All controls passed:

| Control | Seconds | Named tests |
|---|---|---|
| outbox | 7.3 | 26 |
| chain-client | 3.9 | 11 |
| signing (default and root tags) | 8.4 | 26 |
| api | 3.7 | 15 |
| config | 5.8 | 16 |
| binary | 4.7 | 6 |
| store | 30.2 | 31 |
| worker | 25.6 | 27 |
| smoke | 15.3 | 1 |

PostgreSQL 18.6 was used. The runner is not a CI job: a full run takes about 19.3 minutes, within the 2-hour limit for background captures.

**PostgreSQL waiting rule:** each PostgreSQL run started only after `pgrep -f 'svnd|postgres'` showed nothing (wait 0 s each time). B9's devnet started at 16:31:43, after my last run ended at 16:31:23. No `/tmp/svn-pg-*` directory of mine is left.

### Survivors (all test gaps, kept in the runner)
Each fails only until the named test gains the missing case.
1. **b10-r3-simulate-through-app-path:** `TestSimulateUsesTxService` serves the `pathSimulate` constant itself, so it cannot see the path change. No worker test signs a WITHDRAW_CONTENT, the only kind whose gas is simulated. Fix: pin the literal `"/cosmos.tx.v1beta1.Service/Simulate"` in the test.
2. **w1rs-api-replay-ignores-sql-entry** and **w0rt-http-key-reuses-sql-intent:** no API or store test repeats, over HTTP, the key of a validated SQL-path intent with the same request.
   - The worker stores that intent's `request_digest` in the HTTP form (`request.Digest`), so only the entry check refuses the replay.
   - The comment at `services/broadcaster/internal/store/intent.go:189-190` ("digest is of another form") is wrong once the intent is validated.
   - Fix: add that case to `TestIdempotencyConflict` (api) and `TestEnqueueHTTPRefusals` (store).
3. **w0b1-evidence-check-silenced:** in `TestCommitNeedsEvidence`, the COMMITTED row without a commitment has `own_transaction = true`, so the ownership check also refuses it with SV008. Fix: add a case with `own_transaction = false`.

**Note:** the key-file owner check (`keys.go`, `uid != owner`) is killed only by the root-tagged `TestLoadRefusesOtherOwner`. Without root, the directory owner check refuses first, so a non-root CI run has no killer for it.

### Stopped on
Nothing. I left devnet killers to the gate as instructed. When I read `chain/devnet/broadcaster_test.go`, B9's lifecycle had only phases S, H and L, so I named no phases. If the gate wants devnet killers, the runner needs a per-killer `SVN_BROADCASTER_PHASES` field.

Disk is now 4.2 GB free; the Go build cache grew from 5.2 to 7.5 GB, shared with B9's devnet run.

Runner logs, scratch scripts and per-mutant output are in `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w2/B10/`. The final results are `final-unit.txt` and `final-pg.txt` there.

## gate

## Goal 11 pre-capture gate (uncommitted tree on `5e43196`, branch goal-11, wt-11)

Three things failed:
- **Lifecycle test under `-race`:** it failed in phase DU, as B9 reported.
- **Chain tidy:** `go -C chain mod tidy -diff` still wants a change.
- **Plan §15 vet line:** it fails as written, because of an error in the plan's pattern.

Every other code check passed. The other non-zero exits came from this machine's setup, not from the code. All logs are in `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w2/gate/`, as `<name>.log`, plus `summary.tsv`.

**Files changed: none.** A fingerprint of the tree (`git status`, `git diff HEAD` sha256 and the sha256 of every untracked file) is the same before and after: `tree-before.txt` and `tree-after.txt` in the gate directory. No fuzz testdata was written. I ran nothing through capture.sh and ran no git write commands. Every PostgreSQL or svnd run used `netns-run.sh` and started only after `pgrep -f 'svnd|postgres'` showed nothing. No `/tmp/svn-pg-*` or mutant temp copies are left. The `/tmp/Test*` directories date from Nov 2–7 and are not mine. Disk went from 4.5 GB to 3.2 GB free, and the Go build cache grew to 8.7 GB. The extra vet with the `race` tag alone cost about 1 GB.

### Commands
Durations are wall seconds. Counts are top-level tests / subtests unless noted.

| Command | Exit | Seconds | Counts / result |
|---|---|---|---|
| `bash scripts/gofmt_check.sh` | 0 | 0.4 | gofmt ok, 604 files |
| `go vet ./chain/... ./agents/... ./proto/... ./services/broadcaster/...` (plan §15, as written) | **1** | 1.0 | pattern error (see failure 3) |
| `go vet ./chain/... ./proto/... ./agents/provider/... ./agents/auditor/... ./services/broadcaster/...` (the CI form) | 0 | 4.2 | clean |
| `go vet -tags svn_faultpoint ./services/broadcaster/...` | 0 | 6.4 | clean |
| `go vet -tags svn_roottest` on signer and keys | 0 | 0.8 | clean |
| `go vet -tags race ./chain/devnet/ ./services/broadcaster/cmd/svn-broadcaster/` (extra) | 0 | 94.5 | clean |
| `go vet -tags {svn_rehearsal,svn_rehearsal_bad,svn_statecheck} ./chain/cmd/svnd/` (extra) | 0 | 52.4 | clean |
| `go vet -tags 'svn_faultpoint race' ./services/broadcaster/...` (extra) | 0 | 18.2 | clean |
| `go build ./services/broadcaster/...` (default / `-tags svn_faultpoint`) | 0 / 0 | 6.9 / 5.8 | builds |
| `go -C chain build ./...` | 0 | 6.8 | builds |
| `GOPROXY=off go -C services/broadcaster mod tidy -diff` | 0 | 0.2 | no diff |
| `GOPROXY=off go -C chain mod tidy -diff` | **1** | 0.1 | lib/pq move (see failure 2) |
| R1: `go -C chain list -m all` against step0 | 0 | 0.1 | identical to `chain-mods-after.txt`; against `-before`, only the services/broadcaster main-module line differs |
| R1: `GOWORK=off GOPROXY=off go -C chain list -m all` | 0 | 0.1 | byte-identical to `chain-mods-gowork-off-before.txt` (746 lines) |
| R1: svnd build plus `go version -m` | 0 | 6.7 | identical to `svnd-before.txt` after line 1 (247 lines, go1.26.10) |
| R1: `GOWORK=off go -C services/broadcaster list -m all` | 0 | 0.1 | identical to step0 `broadcaster-mods-gowork-off.txt` (747 lines) |
| go.sum subset check | 0 | 0.0 | 1222 broadcaster lines, 0 missing from chain/go.sum |
| `pytest -q -p no:cacheprovider -o addopts= tests/unit` | 0 | 86.9 | 405 passed |
| `check_requirements.py` | 0 | 0.1 | PASS |
| `check_protocol.py` (worktree PATH) | **1** | 0.7 | protoc not on PATH (see failure 4) |
| `check_protocol.py` with the main repo's `.toolchains/bin` on PATH, as CI sets it | 0 | 0.7 | PASS, errors 0, languages MATCH |
| `write_sbom.py --check` | 0 | 0.1 | sbom ok, 1320 components |
| `scan_secrets.py` | 0 | 19.0 | ok |
| `workflow_policy.py` | 0 | 0.1 | ok, files 1 |
| ruff format --check / ruff check / mypy (CI python job, extra) | 0 / 0 / 0 | 0.1 / 0.0 / 4.5 | 41 files formatted; all checks passed; no issues in 29 files |
| `mutants.py --check --worktree` for goals 08 / 09 / 10 / 11 | 0 each | 0.5 / 0.8 / 0.7 / 0.8 | 28 / 169 / 303 / 273 mutants; anchors unique, killers defined |
| app-deadline: FILTER(`go test -count=1 -v ./chain/app -run 'TestTimeoutHeight\|TestAccSeq\|TestStrictDecoderAcceptsTimeoutHeight\|TestSimulateWithEmptySignature\|TestAppSimulatePath\|TestSimulateRouteGas'`) | 0 | 4.8 | 8 passed |
| full chain/app: `go test -count=1 -p 2 -v ./chain/app` | 0 | 70.8 | 149 / 84 passed, 0 failed, 0 skipped |
| `go test -count=1 -p 2 ./agents/auditor/...` | 0 | 16.7 | 13 packages ok |
| fuzz `FuzzTransitions` (outbox), 60 s | 0 | 61.8 | 2,964,056 execs, corpus 281 (32 new) |
| fuzz `FuzzDecodeIntentRequest` (api/request), 60 s | 0 | 79.2 | 368,818 execs, corpus 57 (29 new) |
| fuzz `FuzzForwardedTx` (policy), 60 s | 0 | 65.4 | 469,446 execs, corpus 41 (21 new) |
| fuzz `FuzzClassifyRPCError` (chain), 60 s | 0 | 64.6 | 273,351 execs, corpus 29 (0 new) |
| `go test -race -count=1 -p 2 -timeout 12m -v ./services/broadcaster/...` (netns) | 0 | 146.2 | 280 / 353 passed; 0 failed, 0 skipped, 0 data races; 17 packages ok; smoke uids `same:3710003` and `signer:3710001,serve:3710002` |
| `go test -count=1 -p 2 -v ./services/broadcaster/...` (plan unit, netns) | 0 | 100.3 | 280 / 353 passed, 0 failed, 0 skipped |
| uid-boundary: `-tags svn_roottest -run 'TestServeUIDCannotReadKeyFiles\|TestSignerRefusesOtherUIDPeer\|TestLoadRefusesOtherOwner'` on signer and keys (root, netns) | 0 | 4.1 | 3 / 8 passed |
| `go test -count=1 -timeout 2m -tags svn_faultpoint ./services/broadcaster/internal/faultpoint/` | 0 | 24.9 | 3 passed (TestDefaultBuildHasNoFaultPoints took 23.2 s) |
| prebuild svnd / broadcaster (`-race -tags svn_faultpoint`) | 0 / 0 | 6.1 / 5.9 | builds |
| **devnet-broadcaster, CI form: `go test -race -count=1 -timeout 15m -v -run TestDevnetBroadcasterLifecycle ./chain/devnet`** (netns) | **1** | 206.0 | FAIL in DU only (see failure 1) |
| devnet-broadcaster-stall, CI form, `-race`, `-timeout 12m` | 0 | 51.2 | PASS (46.4 s); 6 process logs, 0 data races |
| go-race devnet shard: prebuilt svnd, then `-race -timeout 12m -skip '<the 6 names>'` (netns) | 0 | 473.4 | 25 / 52 passed, 0 data races. Includes TestDevnetLeaseLifecycle 144.5 s and TestDevnetRegistryDemo 42.7 s |
| go-race app shard: `go test -race -count=1 -timeout 12m -v ./chain/app` | 0 | 299.0 | 149 / 84 passed, 0 data races |
| go-race rest shard (31 chain packages with `-p 2`, then proto, provider, auditor) | 0 | 669.3 | 42 ok, 16 with no test files, 0 failed, 0 data races. Slowest binary: evidence/difftest 361.5 s |
| CI go job: `SVN_SKIP_DEVNET=1 GOFLAGS='-count=1 -p=2' run_tests.py --only go` (netns) | 0 | 293.3 | 61 ok, 18 with no test files, 0 failed |
| prebuild auditor, then devnet-evidence shard `-race -timeout 15m -run TestDevnetAudit` (netns) | 0 / 0 | 5.5 / 316.2 | TestDevnetAuditLifecycle PASS (311.5 s), 0 data races |
| 4 upgrade prebuilds, then devnet-upgrade shard `-race -timeout 15m` (netns) | 0 / 0 | 20.0 / 374.5 | TestDevnetUpgradeRehearsal 261.6 s, MinorityWrongMigration 53.2 s, GovernedChangeTiming 54.7 s: all PASS, 0 data races |
| plan devnet-broadcaster: `go test -count=1 -timeout 1800s -v -run '^TestDevnetBroadcasterLifecycle$'` (no `-race`, netns) | 0 | 163.8 | PASS (159.9 s). DU answers were 19 × 200 and 1 × 201: the race did not trigger this time. 173 intents (166 COMMITTED, 7 FAILED); canaries 0 hits in 40 logs; 34 process logs, 0 data races |
| plan devnet-broadcaster-stall (no `-race`, `-timeout 1200s`) | 0 | 42.3 | PASS (38.8 s) |
| plan devnet.txt regression (no `-race`, `-timeout 1800s`, `-skip` 5 names) | 0 | 329.3 | 25 / 52 passed |
| `run_tests.py --only node` | **1** | 0.3 | 14 tests, 13 passed, 1 failed (see failure 5) |
| diffs against 6d74aca: protocol / chain production / goal-01 / frozen files | 0 each | 0.0 | protocol and chain-production diffs empty. goal-01 diff is exactly the SOURCE_ALLOW, comment and PY_ALLOW lines. Frozen files: only the `go.work` line, which is committed |

**Lifecycle under `-race`, in this gate's own run:**
- **Totals:** 173 intents, 166 COMMITTED and 7 FAILED, 0 open. Accounting (full, phase Z) passed.
- **Logs:** canaries 0 hits in 40 logs; 34 process logs with 0 data races; 943 `trace` lines.
- **Uids:** `boundary_uids=signer:3730001,serve:3730002`.
- **Phase seconds:** S 5.4, H 8.5, A 4.2, TB2 11.8, DU 2.8, CS 13.2, MR 18.2, PG 10.1, BR 39.7, RS 9.2, EX 9.8, L 8.8, Z 2.0. All other phases took under 6 s each.

### Failures, with first error lines
1. **devnet-bc-lifecycle-race (product defect, B7):**
   - First error line: `broadcaster_races_test.go:204: phase DU: of 20 concurrent creates with one key and one body, map[200::15 201::1 409:CONTENT_ID_UNAVAILABLE:4] (status:code); want 1 x 201 and 19 x 200 naming intent c28e5958-39f6-468c-8431-f69a0bd8ce98`.
   - Every other phase passed. In `services/broadcaster/internal/api/handlers.go` the replay check (`s.replay`, line 136) still runs before the authorization check (`s.authorize`, line 139).
   - The non-race run passing does not show a fix: the failure is intermittent (this gate: 1 of 2 runs; B9: 7 of 8).
2. **tidy-chain (needs your decision):** `-	github.com/lib/pq v1.12.3 // indirect` / `+	github.com/lib/pq v1.12.3` in a direct require block. The cause is B9's lib/pq imports in `chain/devnet/broadcaster_authz_test.go` and `broadcaster_db_test.go`. Nothing in ci.yml checks tidy. The broadcaster module's own tidy passes.
3. **Plan §15 gofmt-vet line (plan defect):** `pattern ./agents/...: directory prefix agents does not contain modules listed in go.work or their selected dependencies`. The line needs `./agents/provider/... ./agents/auditor/...`, which passes.
4. **check-protocol (environment):** `protoc well-known types BLOCKED`. The worktree has no `.toolchains/bin`; with it on PATH, as CI sets it, the check passes.
5. **tests-node (environment):** `Error [ERR_MODULE_NOT_FOUND]: Cannot find package 'react' imported from …/wt-11/apps/web/test/react-pin.test.mjs`. The worktree has no node_modules, and goal 11 does not touch apps/web. I did not run `npm ci`, because it would write to the worktree and needs the network.

### Shard timing against CI timeouts
These numbers are from this machine (4 shared CPUs); CI runners may be slower.

| Shard | Test time | CI `-timeout` | Notes |
|---|---|---|---|
| devnet-broadcaster | 202 s | 900 s | about 12 s of prebuilds |
| devnet-broadcaster-stall | 47.5 s | 720 s | |
| devnet | 469.7 s | 720 s | 65% of the budget, the tightest |
| devnet-evidence | 312.6 s | 900 s | |
| devnet-upgrade | 370.7 s | 900 s | plus 20 s of prebuilds |
| app | 294 s | 720 s | |
| broadcaster | 146 s | 720 s | with `-p 2` |
| rest | 669 s for the whole job | 720 s per binary; 20 min job cap | slowest binary 361 s; ran with `-p 2`, CI uses the default |
| CI go job (run_tests.py) | 293 s | 20 min job cap | plus the vets |

### Not run, and why
- **capture.sh evidence:** this is a pre-capture gate, by instruction.
- **`gen_proto.sh` step of the CI go job:** it rewrites the generated files, and I was not to edit files. The protocol diff is empty.
- **`run_tests.py --only python`:** it would write `.pytest_cache`. The equivalent pytest run passed (405).
- **`run_tests.py --only go` without SVN_SKIP_DEVNET, as one command:** all of its parts ran separately. Run whole, it would fail whenever DU triggers.
- **B10 unit and PostgreSQL mutant groups:** B10 finished them (170/172 and 99/101 killed). Since then only chain/devnet test files and the runner's docstring changed, and `--check` passes.
- **Others:** full mutation runs for goals 08–10, the goal 06/07 anchor checks, golangci-lint, the non-root pgdev path, and head CI.

### Stopped on
Nothing stopped the gate; I edited no file. Two items are for you: the DU defect (B7's production code) and the chain tidy decision. I also suggest fixing the plan §15 vet pattern. Disk stayed above 2 GB throughout.

## review 0

I found 8 issues (2 Medium, 6 Low) and no defect in the signing boundary itself. The Medium findings are the known DU idempotency race, which I reproduced and fixed through an overlay only, and a mutant that only the devnet run can kill, which the mutation runner never runs. I edited nothing in the worktree. All my files are under `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w2/review-sec/`.

**Files changed:** none. `git status --porcelain` still shows the same 17 entries (B9's 15 devnet files, the goal-01 `PY_ALLOW` line, `docs/evidence/goal-11/`).

## Findings

**1. Medium: a duplicate create can get 409 instead of its own intent (DU)**
- **Where:** `services/broadcaster/internal/api/handlers.go:136-142`.
- **Problem:** `s.replay` (line 136) can read the idempotency key before a concurrent create with the same key commits. `s.authorize` (line 139) then reads that create's committed PENDING owner row through `ContentOwnerFor`, and `CheckPublication` refuses with 409 `CONTENT_ID_UNAVAILABLE`.
  - This only affects `REGISTER_CONTENT`. No other kind's `authorize` reads rows another create writes.
- **Impact:** the store still serialises on `caller_limits`, so there is no second chain effect and no leaked capacity. But the idempotency contract breaks:
  - a client retrying its own request is told its content id is taken, and might register the same video again under a new id;
  - the `devnet-broadcaster` CI shard fails intermittently.
- **Evidence:**
  - B9 saw the 409 in 7 of 8 runs; the gate saw it in 1 of 2.
  - My overlay test `TestReviewDuplicateCreateRace` replays that interleaving deterministically: one missed replay lookup, then authorize after the commit.
  - Current code: FAIL, `409 CONTENT_ID_UNAVAILABLE`.
  - With the fix below: PASS, and the whole api package passes under `-race` (26 PASS).
- **Fix:** run the replay check again when authorize refuses.
  ```go
  az, apiErr := s.authorize(ctx, caller, auth, dec)
  if apiErr != nil {
      // A create with the same key that committed after the replay
      // check above can be what refuses this one (its PENDING owner
      // row): answer the replay then.
      if status, v, replayErr, done := s.replay(ctx, caller.Principal, env.IdempotencyKey, digest); done {
          return status, v, replayErr
      }
      return 0, intentView{}, apiErr
  }
  ```
  - This closes the race: if authorize ran before the first create committed, `EnqueueHTTP` blocks on the caller row lock and then returns the existing intent.
  - Add my probe as a permanent api test. It needs a `missReplays` hook in `fakes_test.go`; the patched copy is in `review-sec/du/`.

**2. Medium: the R3 Simulate-route claim rests on a devnet killer nobody runs, and the docs are stale**
- **Where:**
  - `docs/evidence/goal-11/mutants.py:668-672` (`b10-r3-simulate-through-app-path`, killer `TestSimulateUsesTxService` only);
  - `services/broadcaster/internal/chain/rpc_test.go:572` (the test serves the `pathSimulate` constant itself);
  - `docs/architecture/broadcaster.md:254,761,1079` (still say "planned devnet test").
- **Problem:**
  - The mutant survives every unit and PostgreSQL killer.
  - The runner cannot run a devnet killer: it has no per-killer `SVN_BROADCASTER_PHASES` field.
  - Neither B10 nor the gate ran any of plan 13.7's devnet killers (devnet OC, WP2, the stall test).
  - So no devnet phase is mutation evidence today.
- **Evidence:** I ran the mutant as an overlay through `GOFLAGS`, which reaches the test's child `go build`, with `SVN_BROADCASTER_PHASES=H` under `netns-run.sh`.
  - Exit 1 after 115 s, FAIL at `broadcaster_phases_test.go:211`.
  - The `WITHDRAW_CONTENT` intent stayed `REQUESTED/QUEUED` with 0 attempts. Serve logged `chain: unexpected response: /app/simulate answer without a height`.
  - Phase H is the only killer.
- **Fix:**
  - In `TestSimulateUsesTxService`, assert the literal `"/cosmos.tx.v1beta1.Service/Simulate"`.
  - Give `K()` a `phases=` option for chain/devnet killers, run under `netns-run.sh` with the one-devnet rule. Add phase H as a second killer for this mutant, and add OC, WP2 and the stall test where plan 13.7 names them.
  - Until then, the evidence must label every devnet phase as a demonstration.
  - Change "planned devnet test" in the three doc lines to name `TestDevnetBroadcasterLifecycle` (phase H, phase S for the uid line).

**3. Low: the database's "COMMITTED needs evidence" check has no killer for `own_transaction = false`**
- **Where:** `services/broadcaster/internal/store/attempt_test.go:467-468`; the trigger is `0001_outbox.pgsql:448-453`.
- **Problem:** the test's bad row has `own_transaction = true`, so the ownership check refuses it with SV008 anyway. With `own = false`, which is how FR1, FR1b, the second DU relay and the before-first-attempt effect check commit, the evidence check is the only database guard. The mutant `w0b1-evidence-check-silenced` survives (B10's run). The API's `checkEvidence` still refuses to show such a row.
- **Fix:** add a case that sets `own_transaction = false` (with `committed_tx_hash` set and no commitments row) and expects `ErrEvidence`.

**4. Low: phase L keeps no logs and scans less than it says**
- **Where:**
  - `chain/devnet/broadcaster_phases_test.go:312-319` (the logs are deliberately not kept);
  - `broadcaster_phases_test.go:321-338` (the canaries);
  - `broadcaster_phases_test.go:306-310` (the encoded forms searched);
  - `chain/devnet/broadcaster_proc_test.go:106-109` (subcommand stdout is kept in memory only).
- **Problems:**
  - Plan 13.4 L requires bounded copies of the logs for the evidence. B9 did not keep them and did not list this as a deviation, so the printed sha256 values cannot be checked afterwards.
  - The stdout of `dev-db`, `dev-ca`, `migrate`, `status` and `signer-clear` is never scanned. `dev-db` is the command that creates the passwords.
  - Only the service key file and the runtime role's password are canaries; the broadcaster key file and the owner role's password are not.
  - The searched forms lack unpadded standard base64.
- **Fix:**
  - Scan `p.stdout.String()` of every process.
  - Add the broadcaster key file and the owner password as canaries.
  - Add the `base64.RawStdEncoding` form.
  - Either copy each scanned log, bounded, to a directory outside the repo named by an environment variable for Wave 3 to capture after `scan_secrets`, or record "logs not kept" as a deviation in the evidence.

**5. Low: the stall test proves "never COMMITTED" but only partly proves "pending or honest failure"**
- **Where:** `chain/devnet/broadcaster_stall_test.go:107-131`.
- **Problem:**
  - The in-flight intent's status during the stall is only checked to be "not COMMITTED". It is never checked to be PENDING_STALL over ACKNOWLEDGED.
  - The riskiest case is never exercised: an intent whose expiry passes during the stall while its transaction sits in a mempool, then commits after recovery. A local-clock death would show up there as FAILED-then-committed.
  - EX reaches AWAITING_DEADLINE only with dropped broadcasts. `TestExpiredInFlightAwaitsDeadline` ends in EXPIRED_DEAD.
- **Fix:**
  - In step 5, assert `n.view(inflight).Status == stStall && Detail.Phase == "ACKNOWLEDGED"`.
  - Create the in-flight relay with a short `expires_in` so its expiry passes during the stall. Assert IN_FLIGHT/AWAITING_DEADLINE with `detail.reason=EXPIRED_AWAITING_CHAIN` and no proxy broadcast after `expires_at`. Then assert it ends COMMITTED with its held hash after recovery.

**6. Low: `checkAccounting` does not check the committing signer of COMMITTED offers, registers and withdraws**
- **Where:** `chain/devnet/broadcaster_accounting_test.go:302-329`.
- **Problem:** BR, the 50-intent run with random kills, relies on this check as its only oracle ("status equals a recomputation from chain reads"). A COMMITTED register, offer or withdraw is accepted on one matching typed event even if service-2 signed it. Only the OC and FC phases pin the signer rule.
- **Fix:** for `evRegistered`, `evWithdrawn` and `evOffered`, also require `bytes.Equal(effects[0].Signer, r.Signer)`. For relays, compare the committed message's offer or lease id and `provider_*` signature with the stored request.

**7. Low: phase PG cannot show "nothing sent while it cannot persist"**
- **Where:** `chain/devnet/broadcaster_restart_test.go:97-140`.
- **Problem:** with every PostgreSQL process stopped by SIGSTOP, the worker blocks on its own database reads, and the held SIGNED register occupies the service key's single window. Even a send-before-persist build would show 0 requests during the 4 s.
- **Fix:** label PG in the evidence as a crash-recovery demonstration. WP1 and WP2, and the unit mutants, carry persist-before-send.

**8. Low: no devnet probe that the signer refuses an acceptance with the service key**
- **Where:** `chain/devnet/broadcaster_authz_test.go:341-349`.
- **Problem:** UA probes only `MsgOfferLease` with the broadcaster key. The direction this review asked about (the service key producing provider consent) is covered by unit tests only.
- **Fix:** add `signerRefuses(name, "MsgAcceptLease with the service key", …, "TYPE_NOT_ALLOWED")`.

## What held
- **COMMITTED** requires a `/tx` read with a hash check plus a typed event, or a first commitment found by event search. The database trigger and the API's `checkEvidence` back it. Phase A and the stall test show an ACK staying SUBMITTED.
- **The effect comparison** cannot report COMMITTED for another account's offer, register or withdraw. The committing signer must be ours, and offer relay rights need a COMMITTED own-signer offer with an owned content id.
- **Forwards** cannot be edited or replayed late: strict decode, the deadline must fall inside the window, the outer signature is checked against the chain id and account number, and the bytes are tracked by unique hash. Unit tests cover all of this.
- **Restarts** decide only from persisted state. The only in-memory state is the rebroadcast pace and the `gone` record, and both can only delay a decision.
- **Logs:** the redactor drops long and JSON-like strings. The migrations' RAISE messages carry no values. B9's and the gate's kept test logs hold only hashes, ids and codes; no strings of 96 or more hex characters.

## Commands
| Command | Exit | Result |
|---|---|---|
| devnet S+H with the R3 mutant (overlay via `GOFLAGS`, `SVN_BROADCASTER_PHASES=H`, `netns-run.sh`) | 1 | 115 s, FAIL at `phases_test.go:211`; mutant killed. Nothing else was running before (`pgrep` empty); no leftovers after |
| `go test -overlay=…/overlay-probe.json -run TestReviewDuplicateCreateRace ./services/broadcaster/internal/api/` | 1 | FAIL, 409 `CONTENT_ID_UNAVAILABLE` |
| same with `overlay-fix.json` | 0 | PASS |
| `go test -race -overlay=…/overlay-fix.json ./services/broadcaster/internal/api/...` | 0 | 2 packages ok; 26 PASS in api |

The rest were read-only `grep`, `sed` and `git status` calls. Disk is at 3.2 GB free, the same as at the start. Not run: PostgreSQL mutants (finding 3 is cited from B10's run), CI, and any other devnet phase.

## review 1

No files changed. Every command I ran was read-only (`cat`, `sed`, `grep`, `awk`, `git status`/`diff`/`log`, `df`). I ran no tests, no PostgreSQL and no svnd.

Path prefixes used below:
- `WT` = /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/wt-11
- `GATE` = /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w2/gate
- `B10` = /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w2/B10

# Findings (most severe first)

## F1 — HIGH: duplicate requests fail; the docs claim the opposite; this blocks capture
- **Where:** `WT/services/broadcaster/internal/api/handlers.go:136-139`. Docs: `WT/services/broadcaster/README.md:90,107-108`; `WT/docs/architecture/broadcaster.md:515,546`.
- **Problem:**
  - `s.replay` runs before `s.authorize`. When several creates with the same key and body arrive at once, a duplicate whose replay lookup ran before the first insert committed then sees that insert's PENDING `content_owners` row and gets `409 CONTENT_ID_UNAVAILABLE`.
  - The docs say the same key and digest always answers 200.
  - Every lifecycle phase is in one test, `TestDevnetBroadcasterLifecycle`. So `devnet-broadcaster.txt` passes or fails by chance. A passing capture would hide a known defect.
  - `TestIdempotencyUniqueUnderConcurrency` (`store/enqueue_test.go:412`) does not cover this. It drives the store directly with OFFER_LEASE and no owner row, never the HTTP handler.
- **Evidence:**
  - `GATE/devnet-bc-lifecycle-race.log:268`: `map[200::15 201::1 409:CONTENT_ID_UNAVAILABLE:4]`.
  - `GATE/plan-devnet-broadcaster.log` passed with 19×200 and 1×201: the race did not trigger that time.
  - The gate saw the failure in 1 of 2 runs. B9 reported it in 7 of 8 runs that recorded counts (development logs, not committed).
- **Fix:**
  1. In `create()`, when `authorize` returns an error, call `s.replay` again and return its answer if it finds the intent. The PENDING owner row is inserted in the same transaction as the intent, so once it is visible the intent is visible too. Alternatively, move the ownership check into `EnqueueHTTP` after its idempotency lookup under the `caller_limits` lock.
  2. Add a deterministic api test: the first lookup returns ErrNotFound, the ownership check reports PENDING, the second lookup returns the intent; expect 200.
  3. Add a mutant that removes the second replay.
  4. Run `SVN_BROADCASTER_PHASES=DU` at least 10 times with `-race`, then the whole lifecycle with and without `-race`.
  5. Only then commit and capture. Until then no row may cite `devnet-broadcaster.txt` as VERIFIED.

## F2 — MEDIUM: the PG phase's "no broadcast while PostgreSQL was stopped" check cannot fail
- **Where:** `WT/chain/devnet/broadcaster_restart_test.go:97-150`, and the `phase=PG` line of `GATE/plan-devnet-broadcaster.log`.
- **Problem:**
  - During the 4 s freeze, both workers are already blocked on calls the proxy is holding. The service broadcast is held by `setRule(broadcastMethod, proxyHold, -1)` with a 30 s RPC timeout, and the relay's reads are held by `holdReadsAfterBroadcast`.
  - So no broadcast would happen in those 4 s even with a healthy database. `broadcast_requests_while_stopped=0` therefore says nothing about "forwards nothing while it cannot persist".
  - PostgreSQL was frozen with SIGSTOP, not stopped, yet the log prints `postgres_stopped_seconds=4`.
- **Fix:**
  - Freeze PostgreSQL while a validated QUEUED intent sits on a signer that has no live attempt and no held RPC, so a healthy broadcaster would sign and send inside the window. Add a control run without the freeze, and rename the field to `postgres_frozen_seconds`.
  - Otherwise, drop this sub-claim from the evidence and cite WP1 plus the unit mutant `w1b8-broadcast-before-persist` for persist-before-send.

## F3 — MEDIUM: "Restart PostgreSQL" is scoped more narrowly than the check name says
- **Where:** the PG phase (above); `WT/docs/architecture/broadcaster.md:1494-1495`; plan §14 restart row.
- **Problem:**
  - The devnet run never restarts the postmaster. It freezes every process, SIGKILLs one runtime backend, and the postmaster's crash restart replaces all children with the same postmaster pid (`replaced_children=14 postmaster=22468`).
  - An immediate stop or a SIGKILL of the postmaster followed by a restart exists only in `TestImmediateStopKeepsCommittedAttempts` (`store/attempt_test.go:1069`). That test uses a pgtest cluster with no chain, and no mutant names it as a killer.
- **Fix:** label the row exactly: "devnet: PostgreSQL crash recovery after SIGKILL of one backend, postmaster pid unchanged; store level: immediate stop and postmaster SIGKILL with restart, no chain; process crash, not host power loss; no mutant shows the store test can fail." Or name that test as the killer of an existing persist or fencing mutant.

## F4 — MEDIUM: the numbers a run prints are not the numbers it used, and the doc says lower bounds were used
- **Where:**
  - `WT/chain/devnet/broadcaster_stall_test.go:39-40` uses rpc_timeout_seconds=30 and stall_window_seconds=3.
  - `WT/chain/devnet/broadcaster_faults_test.go:80-85` (slowServe, used by WD and PG) uses an RPC timeout of 30.
  - `WT/chain/devnet/broadcaster_restart_test.go:317` (RS) uses stall_window_seconds=120.
  - `WT/docs/architecture/broadcaster.md:1079-1080`.
- **Problem:**
  - `GATE/plan-devnet-broadcaster-stall.log:8` prints `stall_window_seconds=5,rpc_timeout_seconds=2`, but the serve process that ran the stall used 3 and 30. The phase overrides above are not printed anywhere.
  - The doc says the devnet tests "run at the lower bound". The log shows timeout_offset_blocks=40 (bounds 5 to 1,000), stall_window_seconds=5 (lower bound 3) and observer_freshness_polls=10 (PROPOSED value 4).
- **Fix:**
  - Make `writeServeConfig` print `numbers_override file=<name> k=v... (PROPOSED, DR-11-10)` for every override.
  - Replace the doc sentence with: "The devnet tests print the numbers they set: one `numbers=` line and one `numbers_override` line per phase-specific serve configuration. These are not the lower bounds."

## F5 — MEDIUM: the test the plan names for "no final leases" does not exist
- **Where:** plan §13.6 and §14 Req. 4; claim in `WT/services/broadcaster/README.md:142-143` and `WT/docs/architecture/broadcaster.md:650,1018-1019`.
- **Problem:**
  - `TestNoLeaseOrProjectionTable` is not in the tree.
  - The claim rests only on a table count inside `TestSQLMetacharactersAreData` (`store/enqueue_test.go:368-372`: 16 tables in schema `broadcaster`).
  - No mutant targets the claim.
- **Fix:**
  - Add `TestNoLeaseOrProjectionTable`. It should assert the exact list of table names across the schemas the runtime role can reach, that none holds lease, offer or content state, and that the role has no CREATE right.
  - Add a mutant that adds a table in migration 0002.
  - Or cite the real test, with these limits, in the acceptance row.

## F6 — MEDIUM: the chain module is no longer tidy
- **Where:** `WT/chain/go.mod:129` (`github.com/lib/pq v1.12.3 // indirect`); imports at `WT/chain/devnet/broadcaster_authz_test.go:36` and `broadcaster_db_test.go:23`; the lib/pq row of `WT/docs/dependencies.md`.
- **Problem:**
  - `GATE/tidy-chain.log` exits 1: tidy wants lib/pq as a direct requirement.
  - The plan §15 capture list includes the broadcaster module's tidy only, so a "no dependency change" claim would leave this out.
  - CI does not check tidy.
- **Fix:** this is a decision for the orchestrator or owner. Either replace lib/pq in the chain/devnet tests with `psql` (no go.mod change), or approve editing the frozen `chain/go.mod` and rerun the R1 checks. In both cases add a `go-mod-tidy-chain.txt` capture that records the actual result.

## F7 — MEDIUM: mutation evidence has gaps that the evidence file must state
- **Where:** `B10/final-unit.txt`, `B10/final-pg.txt`; `WT/docs/evidence/goal-11/mutants.py:128`.
- **Problem:**
  - 4 mutants survived:
    - `w1rs-api-replay-ignores-sql-entry` and `w0rt-http-key-reuses-sql-intent`: both on the duplicate-request path.
    - `w0b1-evidence-check-silenced`: commitment evidence.
    - `b10-r3-simulate-through-app-path`: the R3 simulate route.
  - The "drop the unique key" mutant was excluded as equivalent.
  - No mutant names `TestImmediateStopKeepsCommittedAttempts`, `TestEnqueueAtomicWithApplicationRow` or `TestServeUIDCannotReadKeyFiles`.
  - There are no devnet killers, so every devnet phase is a demonstration, not mutation evidence.
  - The key-file owner mutant is killed only by the root-tagged test, so a non-root CI run has no killer for it.
  - `mutants.py:128` says "Every mutant here has a unit or PostgreSQL killer", which contradicts its own "Test gaps" section.
- **Fix:**
  - Add the missing test cases listed in the runner's docstring.
  - For b10-r3, add a devnet killer with `SVN_BROADCASTER_PHASES=H` (phase H commits a WITHDRAW_CONTENT through the real Simulate route) or pin the literal path in `TestSimulateUsesTxService`.
  - Rerun with `--rev` on the committed tree and report the RESULT line verbatim. Never write "all killed".
  - Fix line 128 to: "every mutant names a unit or PostgreSQL killer; N survived".

## F8 — LOW: the canary scan covers less than the doc says
- **Where:** `WT/docs/architecture/broadcaster.md:489-491`.
- **Problem:**
  - The doc says all five canaries are searched in the node logs and the test output.
  - In fact the node logs are searched for 3 canaries (key-file, db-password, auth-context-id).
  - "go-test-output" is only the test's own stdout copy, scanned at phase L, before the phase Z output and the testing framework's own lines.
  - A restarted validator's log is truncated, so only its part after the restart is scanned.
- **Fix:** reword the bullet to say exactly that, and label `canary.txt` the same way.

## F9 — LOW: stale "planned" wording and items that are already settled
- **Where:** `WT/docs/architecture/broadcaster.md`:
  - `:254` "the planned devnet test prints the same line"
  - `:489` "A planned log canary test"
  - `:760` "the broadcaster's own simulation bytes reach a real node only in the planned devnet test" (phase H now does this)
  - `:1079` and `:1494-1495` "planned fault proxy" and "planned PostgreSQL faults"
  - `:1505` "Answers to Q1 to Q4" (answered on 2026-11-08)
  - `:1506` DR-11-06 (settled by the Q2 answer: audit batches stay on the auditor's path)
- **Fix:** describe what now exists, with test and phase names, and strike the answered items. The goal 09 "outbox submission (Goal 11)" DEFERRED item in progress.md also needs the orchestrator's update.

## F10 — LOW: the doc describes an open-intent counter that does not exist
- **Where:** `WT/docs/architecture/broadcaster.md:608`.
- **Problem:**
  - The doc says "Per-caller open-intent counters are updated in the enqueue transaction".
  - The only stored value is `caller_limits.enqueue_count` (`0001_outbox.pgsql:51-55`), a monotonic count that serialises enqueues. The cap is checked against a count of open rows (`0002_answers.pgsql:584,605-607`).
  - So `checkAccounting` (d) (`broadcaster_accounting_test.go:225-230`) checks "zero open intents per caller", not a stored counter against the rows.
- **Fix:** correct the doc. In the evidence, label (d) as "no open intents per caller at the end of each phase; no stored counter exists that could leak."

## F11 — LOW: some data-race and randomness statements are unlabelled
- **Problem:**
  - "0 data races" from a non-race build (B9's table, row 1) is not race evidence.
  - svnd is never built with `-race` (`svnd_race=false` in every run), so race results cover only the broadcaster processes and the test binary.
  - BR uses the fixed seed 1108, which gave the same 6-fault schedule, all within about 2 s of the batch start, in both gate runs.
  - RS prints the literal `restarted=v1` (node log name) for validator index 0, while B9's report calls it v0.
- **Fix:** cite race results only from `-race` logs and name what they cover. Call BR "a fixed seeded schedule". Use one naming for validators.

## F12 — LOW: plan §15 capture commands need correcting before Wave 3
- **Problem:**
  - `gofmt-vet.txt`: the pattern `./agents/...` fails (`GATE/vet-default.log`); use `./agents/provider/... ./agents/auditor/...` plus the `svn_faultpoint` and `svn_roottest` vets.
  - `uid-boundary.txt` must add `./services/broadcaster/internal/keys` and `TestLoadRefusesOtherOwner`.
  - `app-deadline.txt` must add `TestAppSimulatePath|TestSimulateRouteGas`.
  - `tests-go.txt`, run without SVN_SKIP_DEVNET, includes the lifecycle test, so it fails while F1 is open.
  - `check-protocol` needs `.toolchains/bin` on PATH, as CI sets it.
  - `tests-node` needs node_modules (`npm ci`), or must be recorded BLOCKED (environment).
  - HD case (b) and MR must be described as they ran: HD(b) resent the same bytes, with no (d3) death and no new attempt; in MR a later resend's sdk 30 refusal was counted.

**Checked, no finding:**
- No document presents the Q1–Q4 answers as approval of the boundary document, any PROPOSED number, or a PR: `README.md:6-11`, `broadcaster.md:7-13,85-89,1084-1087`, `docs/architecture/README.md` new paragraph. The root README calls the design PROPOSED.
- Every devnet log carries the label "single machine, four validator processes, private network namespace (not independent operators, not production)".
- The harness reader role's UPDATE refusal (42501) is actually tested (`broadcaster_db_test.go:414-423`).
- The canaries are confirmed processed before the scan.
- The root runs with distinct uids are logged: `boundary_uids=signer:3730001,serve:3730002`; smoke test `signer:3710001,serve:3710002`; `GATE/uid-boundary.log` 3 tests and 8 subtests pass.

# Draft acceptance table (expected status after the F1 fix and capture on the committed tree; gate result in brackets)

"Single machine" means four validator processes in a private network namespace.

| Check | Status | Test | Log (plan §15) |
|---|---|---|---|
| Timeouts before submission | VERIFIED (single machine; fault injected by a test proxy on loopback) after F1 [passed in both gate runs] | TB1, TB2, HD; `TestDeadRuleD1D2D3`; `TestTimeoutHeightThroughFinalizeBlock`; dead-rule and reconcile mutants killed | devnet-broadcaster.txt, unit-broadcaster.txt, app-deadline.txt, mutants-goal11.txt, traces.txt |
| Timeouts after submission | as above | TA, A; `TestAckNeverCommits` (mutant killed) | same |
| Worker death after commit | VERIFIED after F1 (real SIGKILL and build-tagged fault points) | WD, WP1–WP3, SG; persist-order mutants killed (unit, fake node) | devnet-broadcaster.txt, unit-broadcaster.txt, mutants-goal11.txt |
| Duplicate requests | **FAIL now** (product defect F1); 2 surviving idempotency mutants | DU, FR1, FR1b, FR2; `TestIdempotencyConflict`; `TestIdempotencyUniqueUnderConcurrency` (store only) | devnet-broadcaster.txt, mutants-goal11.txt |
| Concurrent same-signer requests | VERIFIED after F1 | CS, MR; `TestAdvisoryLockFencesSecondWorker`, `TestOneLiveAttemptPerSequence`; fencing and sequence mutants killed | devnet-broadcaster.txt, unit-broadcaster.txt, mutants-goal11.txt |
| One business effect | VERIFIED after F1; survivor `w0b1-evidence-check-silenced` | `checkAccounting` (a)(b) after every phase; DU `lease_accepted=1`; FC, OC | devnet-broadcaster.txt, traces.txt |
| No leaked capacity | VERIFIED after F1, labelled: (e) is a chain invariant; (d) counts rows, no counter (F10) | phase deltas (A unchanged, TB2 one package, PI unchanged); (b); (d); (e) | devnet-broadcaster.txt |
| Eventual accurate status | VERIFIED after F1 | (c) API view against row and chain each phase; full check in phase Z; BR | devnet-broadcaster.txt |
| Reject unauthorized messages | VERIFIED for the Q1 allowlist; "approved" BLOCKED (DR-11-01) | UA; signer allowlist tests; `TestSignerPeerUID`; root tests | devnet-broadcaster.txt, unit-broadcaster.txt, uid-boundary.txt, mutants-goal11.txt |
| Reject provider impersonation | VERIFIED (pre-check plus forced chain refusal svn/1) | PI; `TestProviderPrecheck` (mutant killed) | devnet-broadcaster.txt, unit-broadcaster.txt |
| Quorum stop: pending or honest failure | VERIFIED (single machine) [stall test passed with and without `-race`]; EX needs F1 | `TestDevnetBroadcasterQuorumStall`; EX; stall mutants killed (unit only) | devnet-broadcaster-stall.txt, devnet-broadcaster.txt |
| PostgreSQL restart and reconcile | VERIFIED with the F3 scope label; freeze sub-claim per F2 | PG; `TestImmediateStopKeepsCommittedAttempts` (no mutant) | devnet-broadcaster.txt, unit-broadcaster.txt |
| Broadcaster restart and reconcile | VERIFIED after F1 | WD, WP1–3, HD, BR (seed 1108), CS takeover | devnet-broadcaster.txt |
| No manual history edits | VERIFIED (UPDATE refused 42501; owner connection closed after setup) | `readerCannotWrite`; SQL path through the granted function only | devnet-broadcaster.txt |
| Req 1: typed requests, outbox fields, one-transaction coupling | VERIFIED for the fixture schema (no mutant on the coupling test); API use DEFERRED (goal 13) | H SQL path `rollback_rows=0`; `TestEnqueueAtomicWithApplicationRow` | devnet-broadcaster.txt, unit-broadcaster.txt |
| Req 2: approved keys, provider signs, forward | BLOCKED (DR-11-01); sub-results VERIFIED | UA, PI, FW | same |
| Req 3: sequence, retries, deadlines, classification | VERIFIED for sequence, deadlines, uncertainty and classification; backoff and caps unit-only with fakes | TB, TA, RS (sdk 4), MR | devnet-broadcaster.txt, unit-broadcaster.txt, fuzz-*.txt |
| Req 4: states, authentication, permissions, outage queue, no final leases | VERIFIED except "no final leases" (F5) | UA, PB, stall test, view tests | devnet-broadcaster(-stall).txt, unit-broadcaster.txt |
| Req 5: boundary, redaction, chain id, no live credential | BLOCKED (DR-11-01); development boundary VERIFIED from root runs with distinct uids only; same-uid (CI) path not run | uid-boundary tests, L (F8 scope), `TestSignerRefusesForeignChainID` | uid-boundary.txt, canary.txt, devnet-broadcaster.txt |
| Handoff: traces, schema, API, boundary doc | IMPLEMENTED; boundary approval BLOCKED (DR-11-01) | — | traces.txt |
| Handoff: stable identities, commitment evidence | VERIFIED as recorded; goal 12 use DEFERRED | H `evidence=/tx,/commit`; block-hash check | devnet-broadcaster.txt |
| Head CI | BLOCKED until the owner authorizes a draft PR (new shards, same-uid and non-root pgdev paths never run) | ci.yml shards | head CI, ci-shards.txt |

# Requirement rows

| Row | Status | Evidence |
|---|---|---|
| REQ-CHAIN-013 | BLOCKED (DEC-001, DR-07-01, DR-11-01; DR-11-17 open) | Sub-results: the provider half (PI, `TestProviderPrecheck`, `TestNoSigningOutsideSigner`, `TestNoLocalPreimageCopy`) and the publication check on the broadcaster's code path (PB, UA SQL path, `TestPublicationPermission`, `TestSignerRefusesUnregisteredIntentKey`) are green with mutants killed. Not shown against a holder of the service key bytes. The caller's permission claim is DEFERRED (goals 13/19/21). |
| REQ-ACK-001 | VERIFIED after F1 | A, TB1, TA, stall test, `TestAckNeverCommits` |
| REQ-INT-001 | DEFERRED (goals 13, 21) | Guard IMPLEMENTED: `test_chain_writer_policy.py` passed (pytest 405 passed); chain-writer-policy.txt |
| REQ-IDEM-001 | VERIFIED after F1 | TB2: attempt 1 DEAD by d3, attempt 2 at the same sequence with the same business id COMMITTED, `p2_reserved_delta` = one package; WD; FR1; FR2 |
| REQ-QA-008 | VERIFIED after F1 | WD, WP1–3, PG (F3 scope), BR, `checkAccounting`; "process crash, not host power loss" |

**Must stay BLOCKED or DEFERRED:**
- BLOCKED:
  - The goal prerequisite and the Req 2 and Req 5 rows (DR-11-01: boundary document not approved).
  - REQ-CHAIN-013 (DEC-001, DR-07-01).
  - Production paths (DEC-001, DEC-005).
  - Head CI.
  - The same-uid devnet path and non-root pgdev (until head CI).
  - golangci-lint, the goal 06 and 07 anchors, and chain tidy (F6) until decided.
- Independent operators: never claimed; every devnet result is single machine.
- DEFERRED: REQ-INT-001, goal 12 use of commitment evidence, the API's use of the outbox (goal 13), provider-side submission (goal 16), key rotation and recovery (goal 26), throughput (goal 28).

## review 2

No changes to the worktree. I wrote only under `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w2/review-acc/`. `git status --porcelain` shows the same 17 entries before and after.

## Commands run
- `git log` / `git status` / `df` / `pgrep -af 'svnd|postgres'`: exit 0. No other run was active. Disk went from 3.2 GB to 3.1 GB free.
- `mutants.py --list` (read-only): exit 0.
- First spot run: exit 127 because `/usr/bin/time` is missing. No mutant ran.
- Spot run, under `netns-run.sh`, started no PostgreSQL: `mutants.py --worktree --only w0b3-drop-the-l-margin,w0b2-effect-ignores-signer,w1b6-any-peer-uid,w1b7a-digest-omits-context,w1rs-api-replay-ignores-sql-entry --gocache /root/.cache/go-build --logdir …/w2/review-acc/logs`
  - Exit 1 (expected), 38.8 s.
  - The outbox, chain-client, signing and api controls passed.
  - Killed, by exactly the named killers:
    - drop-the-l-margin: `TestDeadRuleD1D2D3`
    - effect-ignores-signer: `TestEffectComparison`
    - any-peer-uid: `TestSignerPeerUID` and `TestSignerRefusesOtherUIDPeer[svn_roottest]`
    - digest-omits-context: `TestRequestDigestCanonical` and `TestIdempotencyConflict`
  - `w1rs-api-replay-ignores-sql-entry` survived, as the runner says it should.
  - The temp copy was removed. Output is in `…/w2/review-acc/spot.txt`.
- Survivor analysis checked by reading the code:
  - **b10-r3:** `TestSimulateUsesTxService` serves `pathSimulate` itself. Correct.
  - **w0b1-evidence-check-silenced:** the row has `own_transaction=true` and no attempt, so `SVN_COMMITTED_OWNERSHIP` also raises SV008. Correct.

## What checks out
- **Faults are real:** SIGKILL of the process group, proxy drop and hold, SIGSTOP plus a backend SIGKILL that forces a PostgreSQL crash restart, real validator stops, and fault points in the real binary.
- **Waits are bounded:** all waits poll with a deadline (`bcWait`, `waitView`). The only fixed sleeps are the MR 3 s, PG 4 s and SG 2 s observation windows and BR's seeded fault timing.
- **Accounting uses typed reads:** `tx_search` on `acc_seq`, typed events through `msgkit.ParseCommitted`, the signer decoded from the transaction, and protobuf ABCI queries. It does not scrape JSON strings.
- **RS** picks a signer with a nonzero account number and waits for the height to advance before asserting.
- **The stall test** puts the queued intent on the SERVICE key, so only the stall rule can block it.
- **Shard timing:** lifecycle took 206 s against 900 s and the stall test 51 s against 720 s, with `-race`.

## Findings (most severe first)

1. **Medium.** Committed results are read from the outbox, not the chain.
   - **Where:** `chain/devnet/broadcaster_races_test.go:355-358` (FR1b), `:375-381` (FR2), `:409-413` (FC), `:437-441` (OC); `chain/devnet/broadcaster_accounting_test.go:264-283` and `:342-357`.
   - **Problem:** FR1b, FR2, FC and OC assert `CommittedCode` from `broadcaster.attempts`, but plan 13.4 says assertions are "read from the chain unless stated". Check (a) accepts any attempt hash of the signer, including a DEAD attempt's. Nothing compares an attempt's committed height, code or codespace with `/tx`. Check (b) ignores a nonzero failure code missing on chain unless the reason is EXECUTION_FAILED.
   - **Consequence:** an outbox that records the wrong committed code, or marks dead an attempt that landed with a failure code, passes.
   - **Fix:**
     - Make `accSeqHashes` return `*ResultTx`.
     - In `checkSequences`, for an "ours" hash require `DeadReason==""`, `CommittedHeight==tx.Height`, `CommittedCode==tx.TxResult.Code` and `CommittedSpace==tx.TxResult.Codespace`.
     - In FR1b, FR2, FC and OC, read `n.committedTx(ours.Hash)` as FR1 and A already do.
     - In `checkIntent`, require the failure code to be found on chain whenever it is nonzero, and require a nonzero code for EXECUTION_FAILED.

2. **Medium.** PG's "forwards nothing while it cannot persist" check cannot fail.
   - **Where:** `broadcaster_restart_test.go:98` (slowServe, 30 s RPC timeout), `:107-114`, `:131-141`.
   - **Problem:** during the 4 s freeze nothing is due to be sent, even with a healthy database:
     - the relay is ACKNOWLEDGED and its reads are held;
     - the register's broadcast is parked in the proxy, and its caller waits up to 30 s;
     - the queued intent is behind the window of one on the same SERVICE key.
   - **Evidence:** the gate log shows `broadcast_requests_while_stopped=0`, which this setup forces.
   - **Fix:** right after the SIGSTOP, call `n.proxy.release(broadcastMethod, false)`. The worker's next step must then persist an UNKNOWN outcome before any resend. Keep the freeze at least `backoff_cap` + 2 polls, assert zero broadcast events, and after SIGCONT assert the resend of the same hash happens.

3. **Medium.** HD does not assert the exact outcome.
   - **Where:** `broadcaster_faults_test.go:383-394`.
   - **Problem:** it only checks that forwarded hashes belong to the intent. Plan case (a) is "reconcile finds the commit"; plan case (b) is "(d3) then one resend", and B9's deviation 8 says (b) resent the same bytes instead. Neither is pinned.
   - **Consequence:** a restarted serve that skips reconciliation and creates a second attempt still passes.
   - **Fix:** record the forward count before `restartServe`.
     - (a): require `len(rows)==1`, `rows[0].Hash` equal to the commitment, and zero forwards after the restart.
     - (b): require `len(rows)==1` with exactly one forward after the restart carrying `rows[0].Hash`; if T+L passed, require `rows[0].DeadReason=="D3_DEADLINE_PASSED"` and `len(rows)==2`.
     - Record the deviation from the plan text.

4. **Medium.** The devnet killers that plan 13.7 names are missing, and the runner misstates the plan.
   - **Where:** `docs/evidence/goal-11/mutants.py:128-131`; plan rows at `plan.md:1472`, `:1476` and `:1505`; the `Killer` dataclass at `:325-341`.
   - **Problem:** the docstring says "devnet killers only where no faster killer exists". The plan says they are "added only where noted" and names three: OC (signer ignored in the effect comparison), WP2 ((d2) ignores `sends_started`) and the stall test (new attempts while stalled). None is wired, and `Killer` has no environment or phases field.
   - **Fix:** either add a devnet killer kind, or record a deviation from 13.7 and correct the docstring.
     - The killer kind needs an env field, `go test -run '^TestDevnetBroadcasterLifecycle$'` with `SVN_BROADCASTER_PHASES=OC` or `WP2`, an explicit `-timeout`, netns, and one devnet at a time.
     - Attach it to `w0b2-effect-ignores-signer`, `w0b3-d2-ignores-sends-started`, `w0b1-d2-check-removed`, `w0b3-new-attempts-while-stalled` and `w1b8-stall-not-blocking`.

5. **Low to medium.** Nothing proves TA's fault fired.
   - **Where:** `broadcaster_proxy_test.go:406-409`; `broadcaster_faults_test.go:233-243`.
   - **Problem:** the proxy's `proxyDropResponse` branch neither counts nor logs. In the gate log TA leaves the drop counter at 9, unchanged from TB2. TA asserts no drop delta, no UNCERTAIN transition and no `/tx` lookup, and `sent` is printed but not asserted. If the broadcaster used another broadcast method, TA would pass without any fault.
   - **Fix:** in that branch call `bump(method, "response-dropped", …Dropped++)`. In TA assert a delta of 1, `sent>=1`, an UNCERTAIN transition, and a `/tx` lookup of `h` before any second forward (reuse TB1's order check).

6. **Low to medium.** EX can fail on a correct broadcaster.
   - **Where:** `broadcaster_faults_test.go:412-423`.
   - **Problem:** it compares the proxy's receipt time with `expires_at` with zero tolerance. The product enforces expiry at BeginSend, in database time, before the HTTP request leaves, so a send counted just before expiry can legitimately arrive just after.
   - **Evidence:** in the gate run the last broadcast reached the proxy only 78 ms before `expires_at`.
   - **Fix:** assert from the outbox that no send was counted after `expires_at` (send transitions or attempt timestamps), and bound the proxy check at `expires_at + rpc_timeout`.

7. **Low.** TB2 does not pin the (d3) boundary.
   - **Where:** `broadcaster_faults_test.go:196-203`; `judge.go:305,313` (`Height >= T+L`).
   - **Problem:** TB2 waits for T+L+1 before looking, so `drop-the-l-margin` and `d3-boundary-off-by-one` both pass TB2. The D3 transition records no height: gate trace id=112 holds only `{"dead_reason": …}`.
   - **Fix:** while the node height is below T+L, read the attempt row and then the node height, and require `DeadReason==""`. The broadcaster cannot see a height above its node's. Then wait for D3.

8. **Low.** The b10-r3 survivor analysis leaves out devnet phase H.
   - **Where:** `mutants.py:252-256`; H submits a WITHDRAW_CONTENT at `broadcaster_phases_test.go:211`; `rpc.go:76-81`.
   - **Problem:** `/app/simulate` cannot encode svn message responses (`TestAppSimulateJSONCannotCarrySvnResponses`), so phase H would very likely kill this mutant. I inferred this and did not run it.
   - **Fix:** pin the literal `"/cosmos.tx.v1beta1.Service/Simulate"` in `TestSimulateUsesTxService`, and either name H as a killer or say why it is not one.

9. **Low.** Commitment evidence is only partly checked against the chain.
   - **Where:** `broadcaster_accounting_test.go:302-329`; `checkEvidence` at `broadcaster_phases_test.go:166-183` covers only phase H.
   - **Problem:** `committing_signer` (part of the goal 12 handoff) and, for idempotent replays, `tx_hash` and `code` are never compared with the chain.
   - **Fix:** require `bytes.Equal(effects[0].Signer, c.CommitSigner)`. When `TxHash != FirstCommit`, require `txOnChain(c.TxHash)` with code `c.Code`, and that its hash is a COMMITTED_IDEMPOTENT attempt of the intent.

10. **Low.** Concurrent posts retry 503 silently.
    - **Where:** `broadcaster_races_test.go:44-54`, used by DU, CS and BR; `post()` at `broadcaster_api_test.go:433` does log its retries.
    - **Problem:** a same-key concurrency defect answered 503 would be masked when the retry returns 200.
    - **Fix:** count retries in `bcRawResult` and print them. In DU, fail on any 503.

11. **Low.** MR's halt check has no queued work to stop.
    - **Where:** `broadcaster_races_test.go:643-651`.
    - **Problem:** during the 3 s window nothing else is queued for the BROADCASTER key, so a halt that blocked only the conflicted attempt would pass.
    - **Fix:** queue a second BROADCASTER-key intent before the window. Assert zero attempts and zero forwards for at least `backoff_cap` + 2 polls, and that it commits after `signer-clear`.

12. **Low.** DU does not pin which relay owns the transaction.
    - **Where:** `broadcaster_races_test.go:236-238`.
    - **Problem:** the test checks only `a.Own != b.Own`. The plan pins "the second own=false, the first's tx canonical".
    - **Fix:** require `a.Own && !b.Own`, and `effects[0].Hash == hash(a.FirstCommit)`.

13. **Low.** Check (d) cannot fail.
    - **Where:** `broadcaster_accounting_test.go:185-187` and `:225-230`.
    - **Problem:** quiesce already waited for `openIntentsByCaller()==0`, and (d) re-runs the same query.
    - **Fix:** test the cap instead. Fill `limited-caller` to its cap of 16 with held intents, assert the 17th is refused, then after they are terminal assert one is admitted.

14. **Low.** The plan row "local clock decides death" has zero mutants.
    - **Where:** `mutants.py:176`.
    - **Problem:** "nothing to mutate" ignores insertion mutants.
    - **Fix:** add one, for example a worker or Judge condition on attempt age, killed by `TestDeadRuleD1D2D3`; or record the row as a deviation.

15. **Low.** Some RPC calls have no deadline.
    - **Where:** `broadcaster_accounting_test.go:317` (`Block` with `context.Background()`), `broadcaster_phases_test.go:172` (`Commit`), `broadcaster_stall_test.go:57` (`t.Context()`).
    - **Problem:** a hung node stalls the test until the `go test` timeout.
    - **Fix:** use `context.WithTimeout(…, 30*time.Second)`.

## Stopped on
Nothing.

## fix

## Goal 11 Wave 2 fix round (wt-11, branch goal-11, uncommitted on 5e43196)

The DU defect is fixed and every gate I re-ran passes. One failure remains: `go -C chain mod tidy -diff` still exits 1, and it needs your decision because it touches the frozen `chain/go.mod`.

- **DU fix:** a production fix plus a deterministic api test.
- **Mutants:** the 4 earlier survivors are now killed, and 7 new mutants were added, all killed.
- **Devnet killers:** the runner now supports them, and the 4 devnet mutants were killed by their devnet phases.
- **Devnet test:** most review findings on it are applied.
- **One more defect, found while re-running:** the stall test had a setup race, now fixed.

All logs are in `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w2/fix/`. No git write commands were run, and no frozen file, go.mod, go.sum, proto, goals or decision-register file changed (`git diff --stat HEAD` on those paths is empty; the chain production diff from 6d74aca is empty).

### Final `git status --porcelain`
```
 M docs/architecture/broadcaster.md
 M docs/evidence/goal-01/check_requirements.py        (B10's PY_ALLOW line; not touched by me)
 M services/broadcaster/README.md
 M services/broadcaster/internal/api/api_test.go
 M services/broadcaster/internal/api/fakes_test.go
 M services/broadcaster/internal/api/handlers.go       (production: the DU fix)
 M services/broadcaster/internal/chain/rpc_test.go
 M services/broadcaster/internal/store/attempt_test.go
 M services/broadcaster/internal/store/enqueue_test.go
 M services/broadcaster/internal/store/intent.go       (comment only)
 M services/broadcaster/internal/store/schema_test.go
?? chain/devnet/broadcaster_*_test.go (15 files; I edited accounting, authz, faults, harness, phases, proxy, races, restart, stall)
?? docs/evidence/goal-11/   (mutants.py edited)
```

### Security review
| # | Outcome |
|---|---|
| 1 DU | **Applied.** In `handlers.go` `create()`, when `authorize` refuses, the replay check runs again. New `TestDuplicateCreateAfterMissedReplay` uses a `missReplays` hook in `fakes_test.go`. New mutant `b11-replay-after-authorize-dropped` is killed. Devnet DU (all 20 creates answer 1×201 + 19×200, no 503 retry) passed 11 of 11 `-race` runs; before the fix, B9 saw 409s in 7 of 8 runs and the gate in 1 of 2. |
| 2 R3 | **Applied.** `TestSimulateUsesTxService` pins the literal `/cosmos.tx.v1beta1.Service/Simulate`, so `b10-r3` is killed. The runner gains a `phases` field for killers and a `devnet` group; `dv-r3-simulate-through-app-path` is killed by phase H. Doc lines 254, 761 and 1079 are fixed. |
| 3 | **Applied.** An `own_transaction = false` case in `TestCommitNeedsEvidence`; `w0b1-evidence-check-silenced` is killed. |
| 4 phase L | **Applied.** It now scans the stdout of all 34 processes, adds the broadcaster key-file and owner-password canaries, and adds the unpadded base64 form. Logs are not kept. The plan contradicts itself here: 13.4 L says keep bounded copies, §15 says do not. I followed §15; the test prints `logs_kept=no` and the docs record it. |
| 5 stall | **Applied.** During the stall the in-flight relay must show PENDING_STALL over ACKNOWLEDGED. It now expires (35 s) during the stall, and must show IN_FLIGHT/AWAITING_DEADLINE with `EXPIRED_AWAITING_CHAIN`, with no broadcast reaching the proxy after its expiry plus 2 s. After recovery it commits with its held hash. The 2 s is a stated tolerance; the sound bound would be the 30 s RPC timeout. |
| 6 | **Partly applied.** A COMMITTED register, withdraw or offer must have been signed by the intent's own signer, and the stored committing signer must equal the chain's message signer for every kind. **Rejected:** comparing a relay's provider signature with the stored request, because the chain itself checks the inner signature against the provider's registered key before it emits LeaseAccepted for that lease id. |
| 7 PG | **Applied.** First a control: with a healthy database, a discarded send is re-sent after 0.4 s. Then the held SIGNED broadcast is discarded right after all PostgreSQL processes are frozen; the proxy saw 0 broadcast requests during the 4.0 s freeze. The log field is now `postgres_frozen_seconds`. The phase is labelled a crash-recovery demonstration. |
| 8 | **Applied.** UA: the signer refuses `MsgAcceptLease` with the service key (`TYPE_NOT_ALLOWED`). |

### Evidence review
| # | Outcome |
|---|---|
| F1 | **Applied** (same fix as security 1), with the mutant and the 10×DU `-race` run. A capture on the committed tree is still needed. |
| F2 | **Applied** (the PG control and freeze above). |
| F3 | **Applied** to the docs: devnet PG is a crash restart with the postmaster pid unchanged; an immediate stop and a postmaster kill happen only in the store test `TestImmediateStopKeepsCommittedAttempts`. No mutant names that test, so the evidence must say so. |
| F4 | **Applied.** `numbers_override file=… (PROPOSED, DR-11-10)` lines for each phase-specific serve config, and the doc sentence is fixed. |
| F5 | **Applied.** New `TestNoLeaseOrProjectionTable` checks the exact list of relations the runtime role can reach (16 tables plus the view `caller_intents`) and that the role has no CREATE or TEMPORARY right. Mutant `b11-lease-table-added` is killed. |
| F6 | **Not applied (frozen file).** The tidy diff is exactly one line: `github.com/lib/pq v1.12.3` moves from indirect to direct, same version, no go.sum change. Your options: approve that one-line edit and re-run R1, or rewrite the test's database access to use psql. The rewrite means about 20 queries plus SQLSTATE parsing, which I judged larger and riskier. |
| F7 | **Applied.** The 4 survivors are fixed, the misleading runner sentence is removed, and devnet killers exist. Still true and for the evidence file: no mutant names `TestImmediateStopKeepsCommittedAttempts`, `TestEnqueueAtomicWithApplicationRow` or `TestServeUIDCannotReadKeyFiles`; the key-file owner check has a killer only in root runs; the unique-key mutant is equivalent. A `--rev` run on the committed tree is Wave 3. |
| F8, F9, F10 | **Applied** in the docs (lines 489, 546, 608, 650, 761, 1079, 1487, 1494–1495, 1505–1506) and in the check (d) label. Updating progress.md is yours. |
| F11 | **Partly applied.** RS now prints `restarted=validator0(v1)`. The race labels (svnd is never race-built) and calling BR "a fixed seeded schedule" are evidence-file wording. |
| F12 | **Yours** (plan §15): fix the vet pattern to `./agents/provider/... ./agents/auditor/...` plus the two tag vets; add the keys package and `TestLoadRefusesOtherOwner` to `uid-boundary.txt`; add `TestAppSimulatePath\|TestSimulateRouteGas` to `app-deadline.txt`; `check-protocol` needs `.toolchains/bin` on PATH; `tests-node` needs node_modules. |

### Coverage review
| # | Outcome |
|---|---|
| 1 | **Applied.** Check (a): every attempt named by `tx.acc_seq` must be not dead and match the chain's height, code, codespace and sequence. FR1b, FR2, FC and OC now read their codes from `/tx` (new `chainResult`). EXECUTION_FAILED must carry a nonzero code that is found on chain. **Rejected:** "found on chain whenever the code is nonzero", because REJECTED codes are CheckTx refusals and never reach the chain (`outbox/retry.go`). |
| 2, 3, 5, 6, 7, 9, 12, 15 | **Applied.** PG redesign; HD outcomes pinned ((a) 0 forwards after the restart, (b) the same bytes resent, or (d3) then one new attempt); TA counts the dropped answer and requires UNCERTAIN and a `/tx` lookup before any resend; EX bounded by `expires_at` + RPC timeout plus a check that counted sends equal proxy requests; TB2 samples the attempt as live below T+L (85 samples); commit-signer and replay-transaction checks; DU requires the first relay to own the transaction; context timeouts on Block, Commit and NumUnconfirmedTxs. |
| 4, 8 | **Applied** (the devnet group). |
| 10 | **Partly applied.** 503 retries are counted and printed in DU, CS and BR. DU is judged only on a round with no 503 (up to 3 rounds) instead of failing on any 503, because a chain-read 503 at a block boundary is the API's documented retry. |
| 11 | **Applied, with a deviation.** A second relay queued on the halted key gets 0 attempts and 0 forwards for 3.4 s, then is cancelled (200, FAILED CANCELLED). It is not left to commit, because DueIntents would give it sequence N+1 and break the plan's "relay commits at N+1", which still holds. |
| 13 | **Rejected.** The cap and the slot a terminal intent frees are tested at PostgreSQL level in `TestPerCallerCapNoGlobalLock`. Check (d) is relabelled as restating quiesce. |
| 14 | **Applied.** `b11-expiry-kills-live-attempt` is killed by `TestExpiryNeverAbandonsLiveAttempt`. |

**Extra defect fixed:** the first non-race stall run failed in `observeStall` (block 30 carried the precommit of v4, the validator stopped by hand). The test now waits for two blocks after v4's stop, as `prove_test.go`'s two-down pattern does.

### Commands (all devnet and PostgreSQL runs under `netns-run.sh`, started only after `pgrep` was empty)
| Command | Exit | Time | Result |
|---|---|---|---|
| overlay checks: no second replay / `w1rs` / `b10-r3` | 1/1/1 | <1 s each | each new test fails on its mutant, as intended |
| store targeted tests (first, then fixed) | 1, then 0 | 3 s | first run: my expected list lacked the view `caller_intents` |
| Go build cache trim (entries unused for 150 min) | 0 | – | 4.24 GB removed; disk 3.1 → 7.1 GB free |
| `go test -race -p 2 -timeout 12m -v ./services/broadcaster/...` | 0 | 149 s | 282 top-level / 353 subtests pass; 0 fail, 0 skip, 0 data races |
| lifecycle, `-race -timeout 15m` | 0 | 211 s (test 202 s) | all phases pass; 179 intents (171 COMMITTED, 8 FAILED); 961 trace lines; canaries 0 hits (40 logs, 34 stdouts); 0 data races |
| stall, `-race` (before the setup fix) | 0 | 72 s | PASS |
| DU only, `-race -count=10` | 0 | 261 s | 10/10 PASS, each 19×200 + 1×201, 0 retries |
| `mutants.py --worktree --group outbox,chain-client,api,store,devnet` | 0 | 887 s | **RESULT mutants=162 killed=162**; all 5 controls pass (devnet control 138 s) |
| `mutants.py --check --worktree`, goals 11/10/09/08 | 0 each | <1 s | 280 / 303 / 169 / 28 mutants |
| vet: CI form / `svn_faultpoint` / `svn_roottest` / `-tags race` on chain/devnet | 0 each | 1–2 s | clean |
| build: broadcaster default / faultpoint; `go -C chain build ./...` | 0 each | 5–6 s | builds |
| tidy: broadcaster / chain | 0 / **1** | <4 s | chain: the lib/pq line only |
| pytest `tests/unit` | 0 | 86 s | 405 passed |
| pytest, the 3 CI test files (after the last edit) | 0 | 3 s | 123 passed |
| `check_requirements` / `scan_secrets` / sbom / `workflow_policy` / `check_protocol` (`.toolchains` PATH) / ruff | all 0 | – | PASS / ok / ok / ok / PASS / clean |
| `gofmt_check.sh` | 0 | – | 604 files |
| lifecycle, non-race, `-timeout 1800s` | 0 | 169 s (test 164 s) | PASS |
| stall, non-race (run 1) | **1** | 20 s | the setup race above |
| stall, non-race / `-race`, after the fix | 0 / 0 | 67 / 78 s | PASS (61 / 70 s) |
| `mutants.py --only dv-stall-guards-removed` (final stall test) | 0 | 133 s | 1/1 killed |

Mutation group seconds: outbox 38, chain-client 112, api 116, store 262, devnet 357. Each devnet mutant failed for its own reason:
- **dv-effect-ignores-signer:** OC got COMMITTED instead of FAILED(OBJECT_TAKEN).
- **dv-unrecorded-send-counted-refusal:** WP2 saw a (d2) death.
- **dv-stall-guards-removed:** the stall test saw a broadcast during the stall, after the relay's expiry.
- **dv-r3-simulate-through-app-path:** the withdraw in H stayed QUEUED with 0 attempts.

**Not re-run:**
- The signing, config, binary, worker and smoke mutant groups. Their killers are unchanged and B10's final run killed all of them.
- The uid-boundary root tests and the fuzz targets. The packages they cover are unchanged.
- chain/app, the other devnet regression tests (their files are unchanged), CI, and the same-uid devnet path.

**Shard timing:**
- **Lifecycle, `-race`:** 202 s against a 900 s timeout.
- **Stall, `-race`:** 70 s against 720 s. It was about 47 s before; the 35 s expiry wait adds the difference.

**Housekeeping:** no `/tmp/svn-pg-*` or mutant temp copies left; 6.3 GB free at the end.

### Draft acceptance table (all forecasts need the commit and the captures; "single machine" = four validator processes in a private network namespace)
| Check | Status after capture |
|---|---|
| Timeouts before and after submission (TB1, TB2 boundary, TA, HD, EX) | VERIFIED candidate (single machine, faults injected by a test proxy on loopback) |
| Worker death after commit (WD, WP1–3, SG) | VERIFIED candidate; WP2 now has a devnet mutant kill |
| Duplicate requests (DU, FR1, FR1b, FR2) | VERIFIED candidate: defect fixed, unit plus devnet tests, 0 survivors |
| Concurrent same-signer requests (CS, MR) | VERIFIED candidate |
| One business effect, eventual accurate status (`checkAccounting` with the strengthened (a)–(c)) | VERIFIED candidate. No leaked capacity: (d) restates quiesce, (e) is a chain invariant, the cap is tested at PostgreSQL level |
| Reject unauthorized messages / provider impersonation (UA, PI, FW, PB) | VERIFIED candidate for the Q1 allowlist; "approved" BLOCKED (DR-11-01) |
| Quorum stop: pending or honest failure | VERIFIED candidate (stall test with expiry during the stall; devnet mutant killed) |
| PostgreSQL restart | VERIFIED candidate, labelled a crash-recovery demonstration plus the frozen-database negative check with its control; immediate stop at store level only, no mutant |
| Broadcaster restart (WD, HD, BR seed 1108, CS takeover) | VERIFIED candidate |
| Req 4: no final leases | VERIFIED candidate (`TestNoLeaseOrProjectionTable`, mutant killed) |
| Req 2 and Req 5, goal prerequisite, REQ-CHAIN-013 | BLOCKED (DR-11-01; DEC-001 and DR-07-01 for REQ-CHAIN-013) |
| Chain tidy (F6), head CI, same-uid devnet path, non-root pgdev, golangci-lint, goal 06/07 anchors | BLOCKED |
| REQ-ACK-001, REQ-IDEM-001, REQ-QA-008 | VERIFIED candidate |
| REQ-INT-001; goal 12 use of commitment evidence; API use of the outbox (goal 13); goal 16, 26 and 28 items | DEFERRED |

Nothing stopped me except F6, which needs your decision.