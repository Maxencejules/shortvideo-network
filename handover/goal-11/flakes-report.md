# Goal 11 flake root-cause workflow wf_82e456a1-6db (verbatim agent returns)

Sections: worker flake, MR flake (contains D1 = F1), review (F1-F4), fix (applied F2; F1, F3, F4 open).


## worker flake

FAILURE 1 (TestEffectCheckBeforeRetry): this was a defect in how the test synchronises with the worker, not a product defect. I fixed the test. After the fix, 200 race runs under load (100 for this test, 100 for a sibling test with the same defect) had 0 failures, and both package suites pass. Nothing was committed.

**Root cause**
- One full signer pass does two things in order (`services/broadcaster/internal/worker/session.go:126-157`):
  - It reconciles the live attempt. Attempt 1 dies by (d3), the effect check finds no effect, and the intent moves to `REQUESTED/RETRY_WAIT`.
  - It then lists the due intents (`store/read.go:254`, `next_attempt_at <= clock_timestamp()`) and runs `reconcileRequested` on each.
- The retry delay is uniform in [0, BackoffBase] (`worker/settle.go:472-478`, `rand.Int64N(ceiling+1)`). The test sets BackoffBase to 100 ms (`harness_test.go:107`).
- When the drawn delay is shorter than the time from the `RETRY_WAIT` commit to the due-intents query (about 4 ms under race), the same pass starts attempt 2.
- In that case the test reads `IN_FLIGHT/UNCERTAIN` instead of `RETRY_WAIT` at `permissions_test.go:234`. The chain change the test depends on would also come after the retry, so the scenario would be broken anyway.
- The worker did nothing the plan forbids. Plan 8.7 allows a delay of 0, and `reconcileRequested` (`session.go:223-256`) runs `checkEffect` before `startAttempt` every time.

**Evidence**
- A diagnostic test was added through `go test -overlay`, so the tree was not touched. It repeated the setup 300 times under race. 12 of 300 runs started attempt 2 in the killing pass:
  - Every one of those 12 had a delay between 1.1 and 3.8 ms; every run with a delay of 4.3 ms or more waited.
  - In all 12 the order was the `RETRY_WAIT` transition, then the TxSearch of the effect check, then the sign call, then attempt 2. Example from run 19: `RETRY_WAIT +0.000ms; next_attempt_at +1.121ms; TxSearch ContentRegistered.content_id='c1' +6.386ms; sign seq=0 timeout=22 +11.804ms; attempt 2 created +21.479ms`. This matches the failure log: two attempts with sequence 0, timeout heights 15 and 22, both sends UNKNOWN.
- Failure rate before the fix:

| Test | Load | Runs | Failed |
|---|---|---|---|
| TestEffectCheckBeforeRetry | none | 50 | 5 |
| TestEffectCheckBeforeRetry | 4 CPU hogs (`go build -a` loop + 3 busy loops on 4 CPUs, load average about 11) | 100 | 24 |
| TestDeadlinePassedRetriesAtTheSameSequence | none | 100 | 2 |
| TestDeadlinePassedRetriesAtTheSameSequence | same load | 100 | 9 |

- The sibling, `TestDeadlinePassedRetriesAtTheSameSequence` (`worker_test.go:224`), has the same defect and the same message. It is not in your list, but the only fix-file limit you set was `worker/**`, so I fixed it too. It is a one-line revert if you want it handled separately.
- No other worker test has this window: the sdk 4 retry waits on block height, and the REPLAY and sequence-used tests still pass if the retry starts in the same pass.

**Change** (29 lines added, 4 removed, test files only; saved at `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w3/flakes/broadcaster-effectcheck/fix.diff`)
- `harness_test.go`: new helper `settleLive`. It runs only the first step of a signer pass (take the lock, `LiveAttempt`, `reconcileLive`) and stops before the due intents. Its comment explains why.
- `permissions_test.go:235`: `e.pass(s)` becomes `e.settleLive(s)` in `retryWait`. The doc comment now says the step that ends attempt 1 reconciles the live attempt only.
- `worker_test.go:220`: the same one-line swap for the pass that ends attempt 1, plus a short doc-comment note.
- Everything else in both tests still runs full `e.pass(s)` passes, and every existing assertion is kept unchanged.

**Why this is not a weakening**
- The `RETRY_WAIT` assertion now always holds. The chain change always lands between `RETRY_WAIT` and the retry, so the effect check that decides the outcome is the one before the retry, as the test intends. It runs in full passes, through the due-intents list and `reconcileRequested`.
- The test still proves:
  - subtest 1 ends FAILED/OBJECT_TAKEN;
  - subtest 2 halts the signer with SIGNER_CONFLICT;
  - in both, there is exactly 1 attempt and no extra signature or send.
- The registered mutant `w1fix-no-effect-check-before-retry` (`docs/evidence/goal-11/mutants.py:1673-1677`), applied by overlay to the fixed tree, is still killed in 20 of 20 runs, with both subtests failing each time:
  - `permissions_test.go:252: {state:FAILED reason:INVALID}, want FAILED/ OBJECT_TAKEN`
  - `permissions_test.go:264: signer halted: not reached after 100 steps`
- No other mutant names either test as its killer.

**Proof runs after the fix** (logs record HEAD 2e364d4 plus this diff; PostgreSQL 18.6; go1.26.10; each run inside `netns-run.sh`, starting only after `pgrep -f 'svnd|postgres'` showed no other run)

| Command | Runs | Failures |
|---|---|---|
| race binary, `-test.count=100 -test.run '^TestEffectCheckBeforeRetry$'`, same load | 100 | 0 |
| same, `-test.run '^TestDeadlinePassedRetriesAtTheSameSequence$'` | 100 | 0 |
| `go test -p 2 -count=1 -v ./services/broadcaster/internal/worker/` | 36 tests passed, 0 skipped | 0 (exit 0, 28.1 s) |
| same with `-race` | 36 tests passed, 0 skipped | 0 (exit 0, 30.5 s) |

`gofmt -l` reports nothing and `go vet` is clean.

**Production code:** no defect found. One thing I noticed: after a retry starts, `intents.reason` still holds the old `D3_DEADLINE_PASSED` while the intent is `IN_FLIGHT`. Callers never see it, because `api/view.go:130-137` reads `reason` only for FAILED intents and Present only for AWAITING_DEADLINE. The other agent's edit to `chain/devnet/broadcaster_races_test.go` is also in the worktree; I left it alone.

Files are in `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w3/flakes/broadcaster-effectcheck/`:
- before-race-noload-50.txt
- before-race-load-100.txt
- before-race-noload-100-deadlinepassed.txt
- before-race-load-100-deadlinepassed.txt
- after-race-load-100.txt
- after-race-load-100-deadlinepassed.txt
- after-suite-norace.txt
- after-suite-race.txt
- diag/diag-race-noload.txt
- diag/diag_overlay_test.go
- mutant/mutant-w1fix-no-effect-check-before-retry-20.txt
- run.sh
- suite.sh
- fix.diff

Changed files:
- /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/wt-11/services/broadcaster/internal/worker/harness_test.go
- /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/wt-11/services/broadcaster/internal/worker/permissions_test.go
- /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/wt-11/services/broadcaster/internal/worker/worker_test.go

## MR flake

FAILURE 2 (devnet lifecycle, phase MR): the cause is a race in the test, not in the product. Fixed in `chain/devnet/broadcaster_races_test.go`; after the fix, MR passed 10 of 10 runs and the full lifecycle test passed 3 of 3, one with `-race`. While running the proofs I found a separate product defect in signer start-up (D1 below), outside the files I may edit.

All scratch files are under `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w3/flakes/devnet-mr/` (written as `<dir>/` below).

## Root cause
The test read node 0's transaction index before node 0 had written the foreign transaction to it.

- **Where:** `broadcaster_races_test.go:674` (before the fix) ran the `tx.acc_seq` search right after `d.broadcast` (`registry_test.go:322`). That call is `broadcast_tx_commit` to node 0, and the search also goes to node 0.
- **Why the answer can be ahead of the index (CometBFT v0.40.0):**
  - `rpc/core/mempool.go:124-132`: `BroadcastTxCommit` returns as soon as the transaction's event is published.
  - `state/txindex/indexer_service.go:47-115`: the indexer gets the same event and writes it later, in its own goroutine (`blockIdxr.Index`, then `txIdxr.AddBatch`, each a `WriteSync`).
  - `libs/pubsub/pubsub.go:407-430`: the order in which subscribers receive the event is map order, so it is random.
- **Not the other suspects:**
  - The foreign transaction did commit: the deliver result was FORBIDDEN with check 0, the line-670 check passed, and later accounting finds it.
  - The query names the right signer (`bech(bcastAddr)`) and sequence (`seq`, the held attempt's sequence that the foreign transaction was signed at).
- **Probe (temporary test, now deleted; source kept at `<dir>/probe_test.go.txt`, output in `<dir>/probe-idle.txt`):** 150 `broadcast_tx_commit` calls to node 0, each followed at once by the `acc_seq` search.
  - The search missed 53 of 150 times; `/tx` asked first missed 42 of 75.
  - The index caught up 1 to 5 ms after the answer.
  - Once `/tx` found the hash, the search named it 150 of 150 times. This is expected, because `kv.AddBatch` (`state/txindex/kv/kv.go:84-114`) writes the hash entry and the event entries in one batch.
- **Test-level reproduction on the original code:** `SVN_BROADCASTER_PHASES=MR` (S plus MR) failed 1 of 6 runs with the same message: `broadcaster_races_test.go:675: phase MR: acc_seq 0 names [], want the foreign 9C6DA000...` (`<dir>/base-idle-2.txt`). Load average during those runs was 2.3 to 9.9 on 4 CPUs, with no extra load added. The evidence capture failed 1 of 1.

## Change (only file edited: `chain/devnet/broadcaster_races_test.go`)
```diff
@@ -671,6 +671,17 @@ func phaseSignerConflict(n *bnet, name string) {
 			res.deliver.Codespace, res.deliver.Code, res.deliver.Log)
 	}
+	// broadcast_tx_commit answers as soon as node 0 publishes the
+	// transaction's event; node 0's indexer service writes the transaction
+	// to the tx index after that, in its own goroutine (CometBFT
+	// state/txindex/indexer_service.go), so a search sent right after the
+	// answer can miss it. The kv indexer writes the hash entry and the
+	// event entries in one batch (state/txindex/kv/kv.go AddBatch): once
+	// /tx finds the hash at the committed height, the acc_seq entry is
+	// written too.
+	if indexed := n.committedTx(res.hash, 30*time.Second); indexed.Height != res.height {
+		n.t.Fatalf("phase %s: node 0 indexed the foreign %X at height %d, committed at %d", name, res.hash, indexed.Height, res.height)
+	}
 	if named := n.accSeqHashes(n.bcastAddr(), seq); len(named) != 1 || !bytes.Equal(named[0], res.hash) {
```
`committedTx` is the existing bounded poll (`waitTx`, 200 ms steps, 30 s limit); it calls `Fatalf` on timeout. No retry was added around the assertion.

The other index reads in `broadcaster*_test.go` run only after the worker has already found the same transactions through the index (`waitTerminal`, `failed`, quiesce before `checkAccounting`). They do not have this race.

## Why this is not a weakening
- The assertion is unchanged: `acc_seq` N names exactly one hash, the foreign one. The new step adds a check: node 0 must index the foreign hash at its committed height within 30 s.
- The assertion still fails if the index lacks the `acc_seq` entry at that sequence. For example, a chain that drops ante events from handler failures, or a foreign transaction that failed in the ante.
- **Mutant run:** I changed the search to `seq+1` (in the test only), ran S+MR once, then restored the fix (byte-identical, checked with `cmp`). The `/tx` wait passed and line 686 failed with `acc_seq 0 names [], want the foreign 6582F8B4...` (`<dir>/mutant-seqplus1-1.txt`). The assertion still decides the result after the wait.

## Proof runs on the fix
All on HEAD 2e364d4 plus the diff above, inside `netns-run.sh`, TMPDIR unset. Command: `go test -p 2 -count=1 -timeout 1800s -v -run '^TestDevnetBroadcasterLifecycle$' ./chain/devnet`. Per-run summaries are in `<dir>/*-summary.txt`.

| Runs | Phases | Extra load | Result |
|---|---|---|---|
| 6 | S+MR | none | 6 pass (`fix-mr-1..6`) |
| 4 | S+MR | 3 busy loops | 3 pass (`fix-mr-load-1..3`); `fix-mr-load-4` failed in setup, before any phase (D1 below) |
| 1 | S+MR | 3 busy loops; the other agent's run overlapped, load average 29 | pass (`fix-mr-load-b-1`) |
| 2 | all (`phases=all`) | none | pass, 182.6 s and 162.8 s (`fix-full-1`, `fix-full-2`) |
| 1 | all, `-race` (`broadcaster_race=true`) | none | pass, 203.9 s, 0 DATA RACE (`fix-full-race-1`) |

MR ran 10 times (7 S+MR runs without extra load ... correction: 6 without and 4 with busy loops) with 0 MR failures; `fix-mr-load-4` never reached MR. The full lifecycle test ran 3 times with 0 failures. `gofmt` and `go vet ./chain/devnet` are clean.

## Production check of `acc_seq` (`SequenceUser`) callers: no defect
An empty search never marks a sequence unused:
- **`worker/attempt.go:142-147` (`sequenceUsedByUs`):** it searches only sequences the committed account sequence already shows as consumed. Not found means not clear, so `allocate` returns `ok=false` and the signer waits.
- **`worker/settle.go:140`:** it runs only after an account read at height ≥ T+L shows the sequence advanced. An empty answer gives `AlertSequenceAdvancedNoHit`, and the attempt stays live (`outbox/judge.go:337`).
- **Death by deadline (`judge.go:329`)** uses committed account state, not the index.

I made no change in `internal/chain` or `internal/worker`.

## Product defect found (D1, not fixed: outside my edit set)
The signer exits at start-up when the node's state is one block behind its block store.

- **Where:** `internal/signer/signer.go:284-291` passes `Status().Height` as `minHeight` to `policy.ReadGovernance` (`policy/governance.go:61-63`), which makes an ABCI query.
- **Why it can be stale:** `/status` reports `BlockStore.Height()` (`rpc/core/status.go:36`). CometBFT saves block H (`consensus/state.go:1772-1774`) before it executes and commits it (`:1812`). While a block executes, `/status` is one ahead of the app's committed height, so the query returns `chain.StaleError` and the signer exits 3.
- **Seen once** in `fix-mr-load-4` (3 busy loops, load average 13.7): `signer exited 3 ... policy: read governance: chain: /svn.authority.v1.Query/Governance answered at height 22, below the needed 23`.
- **Same pattern elsewhere, less severe:**
  - `api/authorize.go:153` and the relay path (`MinHeight: st.Height`) answer 503 for that request.
  - `worker/validate.go:62-137` treats it as a failed read and retries on the next pass.
- **Proposed fix**, for the owner of `internal/signer` (and `policy`/`api` if wanted): retry `ReadGovernance` on `*chain.StaleError` with a bounded wait, or use `minHeight = max(1, st.Height-1)`. The second is the tightest valid bound, because CometBFT saves block H only after it has executed and committed H−1. Unit test: a fake RPC whose `Status` reports H while ABCI answers at H−1 and then H; start-up must wait or succeed, not exit.

## Residual risk (for a decision; not an `acc_seq` misjudgment)
The worker assumes a transaction committed at or below `timeout_height` is indexed once the node reports `timeout_height + L` (comment at `judge.go:128-131`).

- **What CometBFT guarantees:** its event bus is unbuffered (`types/event_bus.go:13`), and so are the indexer's subscriptions (`indexer_service.go:47,55`). From that ordering, a status height of H+3 (H+2 when block H+1 carries a transaction) strictly implies block H is indexed.
- **Where we stand:** the PROPOSED L is 2, with bounds 1 to 20 (`config/numbers.go:107`, `outbox/limits.go:37-38`). So it relies on indexing finishing within about one block. The probe measured 1 to 5 ms against devnet blocks of about 0.25 s.
- **Decisions that depend on it:** the INCLUDED_ANTE_FAILED result at T+L (`judge.go:377`) and the effect check's L-block wait (`worker/effect.go:126`).
- **Options:**
  - Raise the minimum and proposed L to 3.
  - Before trusting an empty or stale answer for height H, check that `block_search "block.height=H+1"` returns a hit. The indexer's loop is sequential, so a hit proves block H's transactions are written.

## Other notes
- The temporary probe file was deleted, and the worktree is back to HEAD plus my diff. The changes to `internal/worker/*_test.go` in the worktree belong to the worker-flake agent.
- The other agent and I started runs at overlapping times twice. Both pgrep checks see nothing while the test binary is still compiling, so both can start in that window. The load-29 run passed under that overlap.

## review

Both fixes hold up. Each root cause is shown by the evidence and checked against the code and the CometBFT v0.40.0 source. Neither fix is a longer sleep or a weaker assertion, and every mutant I applied by overlay was killed. Four findings remain:

1. The signer start-up defect (D1) is real, and it is neither fixed nor tested.
2. Fix 1 loses one mutant kill that only the old flaky assertion had.
3. The lowest allowed `indexing_margin_blocks` value (1) is unsafe.
4. The devnet harness retries a 503 that has the same cause as D1.

The worktree is unchanged (HEAD 2e364d4 plus the same 4-file diff). Nothing was edited or committed.

## Fix 1: TestEffectCheckBeforeRetry and TestDeadlinePassedRetriesAtTheSameSequence
- **Root cause, reproduced every time.** Mutant M0 makes `backoff` (`worker/settle.go:477`) always return 0.
  - With the HEAD test files, both tests failed 10 of 10 runs (5 each), at `permissions_test.go:234` and `worker_test.go:224`, with the original message `{state:IN_FLIGHT phase:UNCERTAIN …}, want REQUESTED/RETRY_WAIT`.
  - With the fixed test files, both passed 20 of 20 runs (10 each).
  - The cause is a same-pass retry, as reported: `session.go:126-157` reconciles the live attempt, then `DueIntents` (`store/read.go:254`) lists intents due by `clock_timestamp()`.
- **Real fix.** `settleLive` calls the same `reconcileLive` that `signerPass` calls. It does not sleep, and every assertion is unchanged.
- **Mutants on the fixed tests (all overlay):**

| Mutant | Runs | Result |
|---|---|---|
| M1, the registered `w1fix-no-effect-check-before-retry` | 3 | killed 3/3, both subtests (`:252` INVALID, `:264` no halt) |
| M2, a retry's effect check turns SIGNER_CONFLICT into OBJECT_TAKEN | 3 | killed 3/3 (`:264`) |
| M3, `DueIntents` ignores `next_attempt_at` | 3 | killed by the old tests 3/3; passes the new tests 3/3; still killed in the suite by store `TestRetryAndBusinessAttempts` (`attempt_test.go:705`) |

- **Independent proof run.** Unmutated fixed tests passed 60 of 60 (30 runs each).

## Fix 2: devnet phase MR
- **Root cause confirmed in CometBFT v0.40.0:**
  - `rpc/core/mempool.go:124-132`: broadcast_tx_commit returns as soon as its own subscription gets the transaction's event.
  - `state/txindex/indexer_service.go:47-115`: the indexer receives the same event and writes it later, in its own goroutine.
  - `state/txindex/kv/kv.go:84-114`: `AddBatch` writes the hash entry and the event entries in one `WriteSync`, so a `/tx` hit means the `acc_seq` entry exists.
- **Real fix.** `committedTx` is a bounded poll that calls `Fatalf` on timeout. The `acc_seq` assertion is unchanged.
- **Devnet mutants** (overlay passed through `GOFLAGS`, phases S+MR, inside `netns-run.sh`). In both runs the new wait (`:682`) and the `acc_seq` assertion (`:685`) passed, then the phase failed:
  - dva, the foreign hit is no longer a conflict (`outbox/judge.go`): killed at `broadcaster_races_test.go:689` ("timed out … waiting for the SIGNER_CONFLICT halt").
  - dvb, MEMPOOL_REJECTED counted as a refusal (`worker/views.go`): killed at `:711` (0 sdk 32 transitions).

## Findings

**F1 (Medium, product defect, not fixed, not tested).** `services/broadcaster/internal/signer/signer.go:284-291`.
- **Problem.** Start-up passes the `/status` height (the block store height) as the minimum height for the governance query. CometBFT saves block H before it executes and commits it (`consensus/state.go:1772-1774` save, `:1812` apply). So the ABCI query can answer at H−1, `chain/rpc.go:279-280` returns `StaleError`, and the signer exits 3.
- **Evidence.**
  - `fix-mr-load-4.txt`: "answered at height 22, below the needed 23".
  - The same pattern exists at `api/authorize.go:38,40,140,153` (503 for that request) and `worker/validate.go:81,103,124,137` (the pass fails and is retried).
- **Exact fix.**
  - Add to `chain/rpc.go`: `func (s Status) AppFloor() int64 { return max(1, s.Height-1) }`, with a doc comment citing the save-before-apply order.
  - Use `st.AppFloor()` in place of `st.Height` as the read floor at all the sites above. Keep `ObservedHeight: st.Height` (`authorize.go:157`) and any window uses.
  - This stays fail-closed: a node two or more blocks behind is still refused.
  - Tests: a fake whose Status reports H and whose ABCI answers at H−1 must let `signer.New` succeed and the API answer 201. At H−2 it must still give `StaleError` (signer) or 503 (API).
  - A bounded retry on `*chain.StaleError` at start-up is an acceptable alternative.

**F2 (Low, coverage lost by fix 1).** `worker/settle.go:475-478`.
- **Problem.** The old RETRY_WAIT-after-a-full-pass assertion was the only check that a retry delay is applied at all.
- **Evidence.** With M0, the whole worker package passes 36 of 36, so the mutant survives every test.
- **Exact fix.** Add `TestBackoffIsFullJitter` (file: `review/addtest/backoff_review_test.go` in the scratch directory below).
  - It draws 2,000 delays for each n from 1 to 6. All must lie in [0, min(cap, base·2^(n−1))], and both the lowest and the highest tenth of that range must be hit (a false failure has probability 0.9^2000).
  - It passed 20 of 20 on the real code.
  - It killed 20 of 20: M0 (delay 0), M4 (no doubling) and M5 (always the ceiling).
  - Register it in `docs/evidence/goal-11/mutants.py` as the killer of a backoff-zero mutant.

**F3 (Low, configuration bound).** `outbox/limits.go:37` (`MinIndexingMargin = 1`), `config/numbers.go:107` (`{1, 20, 2}`), `docs/architecture/broadcaster.md:1061` ("1 to 20").
- **Problem.** The index-completeness assumption (`outbox/judge.go:129`, used at `worker/effect.go:126`) holds by CometBFT's event ordering for L ≥ 2, but not for L = 1.
- **Evidence.**
  - The event bus has an unbuffered command channel (`types/event_bus.go:13`, `libs/pubsub/pubsub.go:119`), and the indexer's subscriptions are unbuffered (`indexer_service.go:47,55`).
  - `updateToState` publishes NewRoundStep (`consensus/state.go:753,767`) after `ApplyVerifiedBlock` and before the next block is saved.
  - So a node reporting height S (block store or app) has indexed every block up to S−2, but block S−1 may still be unindexed. The probe saw the index missing a just-committed transaction 53 of 150 times.
  - With L = 1, `effect.go:126` could settle OBJECT_STATE_CHANGED for a change that is not indexed yet. That would be wrong when the truth is COMMITTED, OBJECT_TAKEN, or a SIGNER_CONFLICT that should halt the signer.
- **Corrections to the MR report's residual-risk section.**
  - L = 2 does not depend on timing, so raising L to 3 is not needed.
  - INCLUDED_ANTE_FAILED (`judge.go:377`) does not depend on index lag: `judgeCommitted` runs only when `/tx` found the transaction (`judge.go:306`).
- **Exact fix.** Set the minimum to 2 in both places and in the doc table, with a comment citing that ordering. Add tests that config validation and `outbox.Limits` reject 1. `outbox_test.go:1298` uses the constant and keeps working.

**F4 (Low, a test retry hides F1).** `chain/devnet/broadcaster_api_test.go:411-436`.
- **Problem.** `post` retries a 503 up to 10 times. Its comment names exactly F1's cause ("a node query answered one height below the latest header").
- **Evidence.** Full lifecycle runs show 5, 4 and 4 `api-retry` lines (`fix-full-1`, `fix-full-2`, `fix-full-race-1`).
- **Exact fix.** After F1, remove that cause from the comment. Make the lifecycle test fail on any `api-retry` line outside the phases that inject faults, so a regression shows up.

## Same-class search: places checked and found clean
- `lease_test.go:405-408` already polls `/tx`.
- `leaseQuery` (`lease_test.go:308-322`) waits for `ABCIInfo` to reach the height before a pinned query.
- `checkSequences` (`broadcaster_accounting_test.go:267`) runs after quiesce, and every terminal intent was resolved by the worker through node 0's index. MR's foreign transaction was the only direct-test transaction it would search, and fix 2 now covers it.
- The `objectEffects` calls (`broadcaster_races_test.go:286,469`) run after the worker reached a terminal state through the index.
- The absence checks (`broadcaster_authz_test.go:605,609`; `broadcaster_stall_test.go:176,238`) concern requests that never reached the chain, or a halted chain.
- `registry_test.go:407` uses a transaction committed long before.
- BlockResults is read from the state store, not the index.
- Worker tests for sdk 4, REPLAY and sequence-used still pass if the retry starts in the same pass.
- Production index-lag paths are otherwise fail-safe: (d3) uses committed account state, and a sequence that advanced without a search hit keeps the attempt live (`judge.go:337`).

All files are in `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w3/flakes/review/`. Open `summary.txt` and `devnet-summary.txt` first; the per-run logs, overlay JSON files, mutant sources and `addtest/backoff_review_test.go` are alongside.

## fix

Of the four review findings I applied F2. F1, F3 and F4 are valid, but I did not apply them: their fix sites are outside my edit set, and F4 also has to wait for F1. All requested proofs pass: the module race suite, 30 loaded runs, 3 devnet lifecycle runs, the stall test, gofmt, vet and the mutant check. Nothing was committed.

## Findings

**F1 (signer exits at start-up on a one-block-stale query): valid, not applied.**
- I checked the cause in CometBFT v0.40.0 in the module cache. `consensus/state.go:1772-1774` saves block H, and `ApplyVerifiedBlock` runs later at `:1812`. `/status` reports `BlockStore.Height()` (`rpc/core/status.go:36`). So an ABCI query can answer at H−1 while status says H.
- The fix sites are outside my edit set: `internal/signer/signer.go:284-288`, the `AppFloor` helper in `internal/chain/rpc.go`, and `internal/api/authorize.go:38,40,140,153`.
- The "worker changes requested by the MR agent" clause does not cover it: the MR agent requested no worker change.
- I left `worker/validate.go` alone for three reasons:
  - It is not a defect there. A `StaleError` from `checkRequest` is not a `*policy.Refusal`, so `reasonOf` returns "". `revalidate` returns the error (`validate.go:259-263`), and `session.go:245-247` ends the pass to retry it. No intent fails.
  - The fix needs the helper in `chain/rpc.go`.
  - The relay pre-check passes `MinHeight: st.Height` together with `ChainTime: st.Time` (`validate.go:124`). Lowering only the floor changes what that check compares, and that is for the policy/chain owner to decide.
- It still needs to go to the signer/chain/api owner, using the review's exact fix and tests.

**F2 (no test checks the retry delay): applied.**

`services/broadcaster/internal/worker/worker_test.go` gets `TestBackoffIsFullJitter`, after `TestDeadlinePassedRetriesAtTheSameSequence`. It is the review's test code; only the doc comment is mine:
```go
func TestBackoffIsFullJitter(t *testing.T) {
	w := &Worker{cfg: Config{Numbers: testNumbers()}}
	base, limit := w.cfg.Numbers.BackoffBase, w.cfg.Numbers.BackoffCap
	for n := 1; n <= 6; n++ {
		ceiling := min(base<<(n-1), limit)
		lowest, highest := time.Duration(1<<62), time.Duration(-1)
		for i := 0; i < 2000; i++ {
			d := w.backoff(n)
			if d < 0 || d > ceiling { t.Fatalf("backoff(%d) = %s, outside [0, %s]", n, d, ceiling) }
			lowest, highest = min(lowest, d), max(highest, d)
		}
		if lowest > ceiling/10 || highest < ceiling-ceiling/10 {
			t.Fatalf("backoff(%d) drew only [%s, %s] of [0, %s]", n, lowest, highest, ceiling)
		}
	}
}
```
Its doc comment cites plan 8.7 and gives the false-failure bound (0.9^2000, below 1e-91).

`docs/evidence/goal-11/mutants.py` registers one new mutant, `w3fix-backoff-never-waits`, in the WORKER group, killed by `TestBackoffIsFullJitter`:
- search: `"\treturn time.Duration(rand.Int64N(int64(ceiling) + 1))"` in `settle.go`
- replace: `"\t_ = rand.Int64N(int64(ceiling) + 1)\n\treturn 0"`

The docstring gets a `w3fix-*` source line, and the "Rows outside plan 13.7" paragraph names the new mutant. No existing anchor moved, so no runner fix was needed. The count goes from 280 to 281.

**F3 (lowest `indexing_margin_blocks` value 1 is unsafe): valid, not applied.**
- I confirmed the ordering argument in CometBFT source:
  - the indexer's subscriptions are unbuffered (`state/txindex/indexer_service.go:47,55`);
  - the event bus command channel has capacity 0 (`types/event_bus.go:13`, `libs/pubsub/pubsub.go:119`);
  - `updateToState` publishes NewRoundStep (`consensus/state.go:753,767`) after `ApplyVerifiedBlock` and before the next `SaveBlock`.
- So at status S, every block up to S−2 is indexed, but S−1 may not be. L=2 is safe by ordering alone; L=1 is not.
- The fix sites are outside my edit set: `outbox/limits.go:37`, `config/numbers.go:107` and the 1-to-20 table in `docs/architecture/broadcaster.md`. It is also a change to a DR-11-10 bound, so the owner applies it.

**F4 (devnet `post` retries 503 up to 10 times): valid as a follow-up to F1, not applied.**
- `chain/devnet/broadcaster_api_test.go:411-436` is outside my edit set (only `broadcaster_races_test.go` is mine).
- Removing the retry before F1 lands would turn F1's known 503 into lifecycle failures.
- The runs below logged 6, 5 and 4 `api-retry` lines in the lifecycle runs, and 0 in the stall run.

## Proof runs
Each run started only after `pgrep -f 'svnd|postgres'` showed nothing, ran inside `netns-run.sh` with TMPDIR unset, on HEAD 2e364d4 plus the 5-file diff.

| Command | Exit | Counts |
|---|---|---|
| `gofmt -l services/broadcaster chain/devnet` | 0 | no files listed |
| `go vet ./services/broadcaster/... ./chain/devnet/` | 0 | clean |
| `go vet -tags race ./chain/devnet/` | 0 | clean |
| `mutants.py --check --worktree` | 0 | 281 of 281 search strings each occur exactly once; every killer is defined |
| `mutants.py --worktree --only w3fix-backoff-never-waits --gocache /root/.cache/go-build` | 0 | control PASS; mutant KILLED by `TestBackoffIsFullJitter` |
| Overlay mutants, 5 runs each: no cap, no doubling, always the ceiling | 1 each | 5/5 failed each, so all three are killed |
| `go test -race -count=1 -p 2 -timeout 30m -v ./services/broadcaster/...` | 0 | 17 packages ok, 2 with no test files; 283 top-level PASS, 0 FAIL, 0 SKIP, 0 DATA RACE |
| `go test -race -count=30 -p 2 -v -run TestEffectCheckBeforeRetry ./services/broadcaster/internal/worker/` under 4 CPU hogs (load 3.6 rising to 10.5) | 0 | 30/30 PASS, 60/60 subtests PASS |
| Same, `-run '^TestDeadlinePassedRetriesAtTheSameSequence$'`, under load (extra run) | 0 | 30/30 PASS |
| `go test -p 2 -count=1 -timeout 1800s -v -run '^TestDevnetBroadcasterLifecycle$' ./chain/devnet` (all phases), run 1 | 0 | PASS, 202.8 s |
| Same, run 2 | 0 | PASS, 161.3 s |
| Same with `-race` (`broadcaster_race=true`) | 0 | PASS, 204.2 s, 0 DATA RACE |
| `go test -p 2 -count=1 -timeout 1200s -v -run '^TestDevnetBroadcasterQuorumStall$' ./chain/devnet` | 0 | PASS, 59.7 s |

## Final worktree state
`git status --porcelain`:
```
 M chain/devnet/broadcaster_races_test.go
 M docs/evidence/goal-11/mutants.py
 M services/broadcaster/internal/worker/harness_test.go
 M services/broadcaster/internal/worker/permissions_test.go
 M services/broadcaster/internal/worker/worker_test.go
?? docs/evidence/logs/goal-11/
```
The untracked `docs/evidence/logs/goal-11/` was already there before this task.

`git diff --stat`:
```
 chain/devnet/broadcaster_races_test.go             | 11 ++++++++
 docs/evidence/goal-11/mutants.py                   | 15 +++++++++--
 .../broadcaster/internal/worker/harness_test.go    | 22 +++++++++++++++
 .../internal/worker/permissions_test.go            |  6 +++--
 .../broadcaster/internal/worker/worker_test.go     | 31 ++++++++++++++++++++--
 5 files changed, 79 insertions(+), 6 deletions(-)
```
Of these, only the `worker_test.go` addition and `mutants.py` are from this round. The other three files hold the earlier flake fixes, unchanged.

## Notes
- **No production code changed.** No production defect beyond F1 was found.
- **Disk:** a hog `go build -a` that I killed left a 1.1 GB work directory, `/tmp/go-build2941757699`. No process was using it, and I deleted it. Free disk is 5.1 GB.

Files are in `/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-11/w3/flakes/apply/`. Open `summary.txt` first. Alongside it:
- static checks: `gofmt.txt`, `vet.txt`, `vet-race.txt`, `mutants-check.txt`
- mutants: `mutant-w3fix-backoff-never-waits.txt`, `mutant-w3fix-logs/`, `ov-nocap.txt`, `ov-nodouble.txt`, `ov-ceiling.txt`
- test runs: `race-module.txt`, `race-load-30-effectcheck.txt`, `race-load-30-deadlinepassed.txt`, `devnet-lifecycle-1.txt`, `devnet-lifecycle-2.txt`, `devnet-lifecycle-race-1.txt`, `devnet-stall-1.txt`
- diffs and scripts: `f2.diff`, `full.diff`, `lib.sh`, `step1.sh`, `step2.sh`