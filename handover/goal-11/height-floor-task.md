# Task text for finishing F1, F3, F4 (copied from the stopped Claude Code workflow goal-11-height-floor-fix)

Paths in the text refer to the old container; the flake report it cites is handover/goal-11/flakes-report.md.

```
export const meta = {
  name: 'goal-11-height-floor-fix',
  description: 'Fix the stale-height start-up defect (F1), the unsafe indexing-margin floor (F3) and the devnet 503 retry masking (F4) found by the flake review; independent review; prove with repeated runs',
  phases: [
    { title: 'Fix', detail: 'F1 signer/api/chain height floor, F3 margin bound, F4 devnet retry' },
    { title: 'Review', detail: 'independent review of the fix' },
    { title: 'Prove', detail: 'apply review findings; module race suite, devnet lifecycle x3, stall, mutants' },
  ],
}
const A = args
const COMMON = `
Worktree: ${A.wt} (branch goal-11, HEAD 2e364d4 plus 5 uncommitted files from the flake fixes: chain/devnet/broadcaster_races_test.go, docs/evidence/goal-11/mutants.py, services/broadcaster/internal/worker/{harness_test,permissions_test,worker_test}.go; keep them). The flake root-cause report with findings F1, F3, F4 is in ${A.dir}/w3/flakes/report.md (read sections F1, F3, F4 and the MR report's D1). Plan: ${A.plan}. Brief: ${A.brief}.
Rules: no git add/commit/push/stash/checkout/reset. One PostgreSQL/devnet run at a time (wait while "pgrep -f 'svnd|postgres'" shows another). Devnet under ${A.netns}. TMPDIR must stay UNSET. "go test -p 2". Never weaken a test; never add a retry that hides a product defect; comments describe what the code does; every input bounded. Chain production files, go.mod/go.sum, proto, docs/goals, decision register: do not edit. Python: /home/user/shortvideo-network/.venv/bin/python.
`
phase('Fix')
const fix = await agent(`${COMMON}
TASK (you own services/broadcaster/** except go.mod/go.sum, chain/devnet/broadcaster*_test.go, docs/architecture/broadcaster.md, services/broadcaster/README.md, docs/evidence/goal-11/mutants.py):
F1: CometBFT v0.40.0 /status reports BlockStore.Height(), which can be one block ahead of the app's committed height while a block executes (consensus/state.go saves block H before ApplyVerifiedBlock). Every place that uses Status().Height as the minimum height of an ABCI query (signer start-up governance read internal/signer/signer.go ~284-291; api/authorize.go ~38,40,140,153; the relay pre-check and worker validate paths; any other caller: grep for MinHeight and Status() uses) must use a floor the app has certainly committed — e.g. a helper in internal/chain (AppFloor) returning status height - 1 (or the app's last committed height from ABCI Info if the client exposes it), bounded below by 1 — without weakening the staleness guarantee those checks exist for (a read must still not be older than the floor; document exactly what the floor guarantees). Where a check also compares chain time (ChainTime: st.Time in the relay pre-check), keep the time and height consistent (use the header of the floor height, or justify). Signer start-up must not exit on a one-block-behind answer; it may wait (bounded) and retry the read until the floor is satisfied, failing with a clear error after a PROPOSED bounded timeout. Add deterministic unit tests (fake RPC: status H, query answering at H-1) that fail on the old code, for the signer start-up, the API authorize path and the worker validate path.
F3: the lowest allowed indexing_margin_blocks value 1 is unsafe (at status S only blocks up to S-2 are certainly indexed, see the report); raise the hard lower bound to 2 in outbox/limits.go and config/numbers.go, update the bound table in docs/architecture/broadcaster.md, add a refusal test for 1, keep the PROPOSED default if it is already >= 2 (report its value).
F4: chain/devnet/broadcaster_api_test.go post() retries 503 up to 10 times. After F1, measure: run the lifecycle test 2 times and count api-retry lines. If 503s vanish, make post() fail on any 503 except where the API documents a 503 the test provokes on purpose (then assert it there explicitly); if some 503s remain, root-cause them (report each with its log line) and either fix the product or keep a bounded retry only for the documented, explained case, printing a counted line, and say so.
Update docs (broadcaster.md, README) for F1/F3 and add mutants to docs/evidence/goal-11/mutants.py for the new floor rule and the margin bound (unit killers), keeping every existing anchor valid (--check). Run: gofmt -l; go vet ./services/broadcaster/... ./chain/devnet/; go test -race -count=1 -p 2 ./services/broadcaster/...; the new tests against the old code via go test -overlay with the old file versions (show they fail); mutants.py --check --worktree and --only <your new mutants>. RETURN: diff summary, root causes, tests, every command with exit and counts.`, { label: 'fix', phase: 'Fix' })

phase('Review')
const rev = await agent(`${COMMON}
Fix report: <<<${fix}>>>
TASK: independent adversarial review of the F1/F3/F4 changes (git diff against 2e364d4, excluding the 5 flake-fix files already reviewed). Check: the height floor never lets a read older than what the check needs pass (e.g. a relay pre-check against a provider key rotated at the latest block; a governance read used to authorize a message); no place still uses Status().Height as a query floor; the signer start-up wait is bounded and fails clearly; time/height consistency in the relay pre-check; the margin bound and docs agree; F4 does not hide a product 503; tests fail on the old code (re-run 2 with -overlay); new mutants are killed. RETURN findings (severity, file:line, problem, evidence, exact fix) or an empty list.`, { label: 'review', phase: 'Review' })

phase('Prove')
const prove = await agent(`${COMMON}
Fix report: <<<${fix}>>>
Review: <<<${rev}>>>
TASK: apply valid review findings (same ownership as the fix task); reject invalid ones with reasons. Then prove on the final tree: gofmt -l services/broadcaster chain; go vet ./services/broadcaster/... ./chain/devnet/ and -tags svn_faultpoint / -tags svn_roottest / -tags race variants as in ci.yml; go test -race -count=1 -p 2 ./services/broadcaster/...; go test -race -count=30 -run 'TestEffectCheckBeforeRetry|TestDeadlinePassedRetriesAtTheSameSequence' ./services/broadcaster/internal/worker/; the devnet lifecycle test 3 times (2 non-race, 1 -race) and the stall test twice (1 non-race, 1 -race), under ${A.netns}, one at a time, reporting api-retry counts and phase timings; mutants.py --check --worktree; mutants.py --worktree --gocache "$(go env GOCACHE)" --group <every group whose files changed> (report RESULT lines); go mod tidy -diff for services/broadcaster and chain; pytest -q -p no:cacheprovider tests/unit/test_ci_broadcaster.py tests/unit/test_ci_devnet_coverage.py tests/unit/test_chain_writer_policy.py; scan_secrets.py. RETURN: per finding applied/rejected, final git status --porcelain and git diff --stat 2e364d4, every command with exit, counts and durations.`, { label: 'prove', phase: 'Prove' })
return { fix, rev, prove }

```
