# Goal 08 evidence

Status: BLOCKED

The checks cited in this file ran on two commits of branch `goal-08` (see [Tested revision](#tested-revision)):

- the first tested commit `acfe14ecaf79aa45021baf61f937391b43d2d310`, tree `6e81d0c948847af933b25e02be1f1bf2a34a62f2`: the 63 logs under `logs/goal-08/` whose names have no `m2-` prefix;
- the second tested commit `ea29169196d862867a0e270fada1fda74b1214c6`, tree `eabf7cef6fc24f9afdd07a8f95470ad1f85ba375`: the 26 `m2-` logs. Its parent is `acfe14e`. It adds tests, README and architecture text, and the committed mutation runner; its production-path diff from `acfe14e` is `<none>` ([m2-revision.txt](logs/goal-08/m2-revision.txt); see [Second tested commit](#second-tested-commit-ea29169-capacity-boundary-and-max_expiry_steps-halt-tests)).

All 89 logs were written by `scripts/evidence/capture.sh`. Each one prints its commit, its tree and `DIRTY no`, and none has a `DIRTY_PATH` line.

- 60 of the 63 first-commit logs have `DATE` lines from 2026-11-02T15:28:44Z ([revision.txt](logs/goal-08/revision.txt)) to 2026-11-02T16:12:20Z ([tests-go-count1.txt](logs/goal-08/tests-go-count1.txt)). At that time `git status` listed only the then-untracked runner `docs/evidence/goal-08/mutants.py` (version 1), its `PY_ALLOW` line in `docs/evidence/goal-01/check_requirements.py` and the new logs, all under `docs/evidence/` ([revision.txt](logs/goal-08/revision.txt) prints the status at the first capture). They ran before this file and the progress rows were written.
- Three first-commit logs were captured after this file was written; this file is under `docs/evidence/`. They are [spec-oracle-mypy.txt](logs/goal-08/spec-oracle-mypy.txt) (`DATE 2026-11-02T17:00:33Z`, added by the evidence fixer) and the filtered recaptures of [unit-app.txt](logs/goal-08/unit-app.txt) (`DATE 2026-11-02T17:08:31Z`) and [unit.txt](logs/goal-08/unit.txt) (`DATE 2026-11-02T17:09:11Z`), which replace the first 90 MB captures ([Large unit logs](#large-unit-logs)).
- The 26 `m2-` logs have `DATE` lines from 2026-11-02T18:03:48Z ([m2-revision.txt](logs/goal-08/m2-revision.txt)) to 2026-11-02T18:44:13Z ([m2-containers-cleanup.txt](logs/goal-08/m2-containers-cleanup.txt)) and ran one at a time.
- For every run after this file was written, the edited progress rows were set aside (copied to scratch, then `git checkout` of `docs/progress.md` and `docs/progress.tsv`) and restored after it. So those logs also print `DIRTY no`. After the two recaptures the restore was checked with `sha256sum -c` (scratch `goal-08-cont/ev/raw90/exits.txt`: `docs/progress.md: OK`, `docs/progress.tsv: OK`). Before the `m2-` runs the working copies matched the saved copies in scratch `goal-08-cont/ev2/keep` by sha256 (`sha256-keep.txt`, `sha256-worktree.txt`), and after them the rows were restored from those copies; no output of a hash check after that restore was saved, and row 08 was edited afterwards. Every row other than 08 of the current `docs/progress.md` and `docs/progress.tsv` is byte-identical to the copies in `goal-08-cont/ev2/keep` (`diff` by the verifier and the evidence fixer; not a capture log).

Some logs test an export or a clone of a commit, not the working tree, as their commands show: the mutation runner logs (`mutants-*.txt`, `m2-mutants-*.txt`, `goal06-anchors.txt`, `goal07-anchors.txt`) export `HEAD` with `git archive`, and [tests-python-clean-clone.txt](logs/goal-08/tests-python-clean-clone.txt) runs the Python suite in a scratch clone of `acfe14e`.

This file reads the decision register the way [goal-07.md](goal-07.md) does. Gate A approved DEC-001 through DEC-012 with their PROPOSED defaults ([decision-register.md](../decision-register.md)). An item stays BLOCKED here when it needs a value, a schema or a protocol change that no approved default supplies, or a required check that did not run. That reading is not an owner ruling, and the owner can reject it in review.

BLOCKED:

1. **REQ-CAP-008 (BLOCKER DEC-005).** The approved DEC-005 default leaves the sponsor, the per-account allowance and the lease length unset. Its Blocks section says goal 08 "can still specify that every accepted lease needs a responsible party, a term, a budget, and an exit once those values are decided". The chain records the scheduler of every offer only in a used-offer marker; no query, typed event or export returns it (DR-08-08).
2. **Lease durations and allowances off the devnet (DEC-005).** The offer and renewal term is the labelled DEC-005 devnet test number. Without it every `OfferLease` and `RenewLease` is `ERROR_CODE_DECISION_BLOCKED` (`TestDecisionBlockedWithoutDEC005` in [unit-leases.txt](logs/goal-08/unit-leases.txt), `TestDevnetTermDecisionBlocked` in [unit-app.txt](logs/goal-08/unit-app.txt)). Every lease result below is a devnet or test-chain result under that number. No lease length is policy.
3. **REQ-SEP-001, "a new encode uses a new content version" (DR-07-04).** Protocol v0 content records use `content_version` 1 only, so the assigned test cannot run. The other half of the row is VERIFIED.
4. **Goal prerequisite "approved lease state/terms specification".** Protocol v0 is the frozen candidate, not an approved protocol ([progress.md](../progress.md); [check-protocol.txt](logs/goal-08/check-protocol.txt) prints "protocol not approved"). v0 defines no provider ACCEPT, RENEW or END payload, so this goal defines interim ones (DR-08-04).
5. **CI `devnet` job, unchanged script, in this container: BLOCKED (environment); in head CI: passed** ([Head CI](#head-ci)). On `ea29169`, `bash scripts/devnet/prove-containers.sh` fails at its image build: `go build` inside `docker build` cannot verify proxy.golang.org (`tls: failed to verify certificate: x509: certificate signed by unknown authority`), because the build container does not trust the session's egress-proxy CA. No node started ([m2-containers-unchanged.txt](logs/goal-08/m2-containers-unchanged.txt), exit 1). Goal 07's labelled copy, whose only change is the image build step, passed on the same commit ([m2-containers-labelled-copy.txt](logs/goal-08/m2-containers-labelled-copy.txt), exit 0; empty blocks, host networking, one machine; see [Gates](#gates)). Both ran through a local `dockerd` that the orchestrator started for this run ([Tested revision](#tested-revision)). The unchanged CI job itself passed in head CI on GitHub's runner, so this item limits only what this container can reproduce.
6. **Goal 06 and goal 07 mutation runners on this tree.** Both refuse on `acfe14e` before running any test: 9 and 2 anchors target code that goal 07 or DR-06-06 changed ([goal06-anchors.txt](logs/goal-08/goal06-anchors.txt), [goal07-anchors.txt](logs/goal-08/goal07-anchors.txt)). Neither was run again on `ea29169`. Goal 08 changed `chain/x/authority`, `chain/genesis` and the ante, which their other mutants target, so these are affected checks that did not run: 110 goal 06 mutants and 28 goal 07 mutants. The goal 08 runner ports the four goal 06 anchors that DR-06-06 changed and both goal 07 anchors, and those mutants are killed on both commits ([Mutation checks](#mutation-checks)). The five goal 06 anchors that target code goal 07 deleted have no port.

Resolved since the evidence commit `69f3980`: head CI and the post-docs checks. See [Head CI](#head-ci).

DEFERRED:

- REQ-INV-003, governance half: Goal 10. The row names goals 08 and 10; the lease half is VERIFIED.
- Renewal with evidence: Goal 09. `Deps.Evidence` is nil until x/evidence exists, so every `evidence_id` is `ERROR_CODE_NOT_FOUND` ([docs/architecture/leases.md](../architecture/leases.md), "Wiring"). The keeper ladder covers the evidence rows with a fake (`TestEvidenceCannotExtendLease`).
- The host-side display of clock uncertainty in REQ-HOST-007's text: Goal 16 (provider agent). The row's assigned test is VERIFIED.

IMPLEMENTED: the handoff documentation and the evidence for scheduler and broadcaster integration. Goals 11 and 16 are the consumers.

Every other acceptance check and requirement row of goal 08 is VERIFIED (tables below). The lease half of REQ-INV-006, deferred from goal 07, is VERIFIED here.

DR-06-06 (seat-only admission) was ruled by the owner on 2026-11-02 and is implemented here. It supersedes DR-07-11's `BROADCASTER`-only interim rule. DR-08-01 through DR-08-14 are open. Each has an implemented, tested interim, and none blocks a goal 08 acceptance check on its own. This file classifies six as security-critical: DR-08-01, DR-08-02, DR-08-04, DR-08-07, DR-08-11 and DR-08-13. The x/leases README states each request's security consequence but labels none of them security-critical; the classification is this file's, not the README's and not an owner ruling ([Decision requests](#decision-requests)).

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a production network. The devnet is four processes on one machine. No independent operator is claimed.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH. GPT-6 Astra was not used.
- Model that ran: Claude Code, model `claude-opus-5-5`, in remote session `session_014mwakeb27YrugDfwAx8vXL`. Workflow subagents inherit the session model. The goal 08 commits `4f70733`, `210bfb7`, `acfe14e` and `ea29169` each carry the `Claude-Session` trailer for that session (read with `git log` by the evidence writers, not a capture log).
- Source: claude-code-remote `get_session`, which the orchestrator read at about 14:45 UTC on 2026-11-02. It reported:
  - `configured_model`, `session_context.model` and `external_metadata.last_served_model`: all `claude-opus-5-5`;
  - effort: `session_context.effort_level` = `ultracode`, and `external_metadata.is_ultracode_active` = true. That is not the playbook's HIGH;
  - `external_metadata.flag_settings.fastMode` = true. This file records that flag and does not interpret it.
- Limits of that source. It is a tool response, not a `capture.sh` log. It describes the session's settings and its latest turn. This evidence did not scan the full event history for model-fallback notices.
- AGENTS.md's substitution note names Cursor cloud agents. This run is Claude Code, a further substitution.
- Owner acknowledgement for goals 07-30, as recorded in [goal-07.md](goal-07.md):
  - On 2026-11-01, received by this session at about 16:33 UTC, the owner wrote in this Claude Code session: "I acknowledge the model substitution for DR-06-07".
  - In the next message, received at about 16:34 UTC on 2026-11-01, the owner wrote: "extend the acknowledgement to goals 07 through 30".
- So goal 08 is not BLOCKED on model allocation. The ruling covers model allocation only. It does not certify correctness, and extra effort does not certify correctness either.

## Tested revision

| Item | Value | Log |
| --- | --- | --- |
| Commit | First tested commit `acfe14ecaf79aa45021baf61f937391b43d2d310`. Second tested commit `ea29169196d862867a0e270fada1fda74b1214c6`, whose parent is `acfe14e` ([Second tested commit](#second-tested-commit-ea29169-capacity-boundary-and-max_expiry_steps-halt-tests)). | [revision.txt](logs/goal-08/revision.txt), [m2-revision.txt](logs/goal-08/m2-revision.txt) |
| Tree | `6e81d0c948847af933b25e02be1f1bf2a34a62f2` (`acfe14e`); `eabf7cef6fc24f9afdd07a8f95470ad1f85ba375` (`ea29169`) | every log without a prefix prints the first; every `m2-` log prints the second |
| Parents | Of `ea29169`: `acfe14e` ([m2-revision.txt](logs/goal-08/m2-revision.txt)). Of `acfe14e` (`HEAD` in [revision.txt](logs/goal-08/revision.txt) and [stack.txt](logs/goal-08/stack.txt), and in this row and the next): `HEAD^1` `210bfb78d65f7b1cb910b5e24ebc8d8ce9d17f56` merges goal 07 `7c79c298ee9036bee017987d0ab045e00ac9b2a8` into the goal 08 checkpoint `4f7073384093e28d8cdc985ca1407ea66b9b99d5`; it carries DR-06-06 and the review fixes. `HEAD^2` is `main` `2fc93e5b40050e54341cbdb4cc2275ed4bc4839a`. `acfe14e` is a `merge -s ours` of `main`: it has the tree of `HEAD^1`. | [revision.txt](logs/goal-08/revision.txt) |
| Goal 07 and `main` | Goal 07 is merged: PR 10 was squash-merged into `main` as `2fc93e5` on the owner's instruction "merge PR 10" (2026-11-02). `main` `2fc93e5`, goal 07 `7c79c29`, `HEAD^1^2` and `HEAD^2` all have tree `3ac4bed9b4904c0aae8473e7d1b2754fecf16135`, and `git diff 7c79c29 2fc93e5` is empty. `2fc93e5`, `7c79c29` and `4f70733` are ancestors of `HEAD`. | [stack.txt](logs/goal-08/stack.txt) |
| Branch | `goal-08`, pushed. At the first captures `origin` `refs/heads/goal-08` was `acfe14e`; at the `m2-` captures it was `ea29169` (`origin goal-08 equals HEAD: yes`). Both times `refs/heads/goal-07` was `7c79c29` and `refs/heads/main` was `2fc93e5`. | [stack.txt](logs/goal-08/stack.txt), [revision.txt](logs/goal-08/revision.txt) (`branch: goal-08`), [m2-revision.txt](logs/goal-08/m2-revision.txt) |
| Runtime | Ubuntu 24.04.5 LTS, Linux 6.18.44, x86_64, 4 CPUs, 15 GiB. Go 1.26.10. Python 3.12.3, ruff 0.17.3, mypy 2.4.1. Node v22.23.4, npm 10.9.9. libprotoc 36.2. `df -h /`: 6.0G available at the first captures, 3.9G before the `m2-` container runs and 1.8G after them. No Docker daemon was running at the first captures ([docker-info.txt](logs/goal-08/docker-info.txt), `DATE 2026-11-02T16:09:47Z`, exit 1). For the `m2-` container runs the orchestrator started a local daemon at about 17:10 UTC on 2026-11-02 with `(nohup dockerd > <scratchpad>/logs/dockerd-2026-11-02.log 2>&1 &)`, as goal 07 did; the daemon's own log records `Starting up` at 17:10:45Z and `API listen on /var/run/docker.sock` at 17:10:49Z (a scratch log, not a capture log). It is a local daemon on the unix socket, not a remote one: Docker client and server 29.9.2, API 1.56, `Storage Driver: overlayfs`, `Cgroup Driver: cgroupfs`, `Cgroup Version: 1`, and the container scripts print `DOCKER_HOST=<unset>` and `docker context default endpoint unix:///var/run/docker.sock`. The devnet commands (`devnet.txt`, `devnet-lease.txt`, `m2-devnet.txt`), `go test -race ./chain/...` (`race.txt`, `m2-race.txt`) and `run_tests.py --only go` (`tests-go.txt`, `tests-go-count1.txt`) ran inside a loopback-only network namespace through `netns-run.sh`, a scratch helper (`unshare --net`, `lo` up, no outbound network) that is not in the repository; without it they are the same commands. `race-proto.txt` and `race-agents-provider.txt` ran without it. The two container runs ran outside it, through the local daemon with host networking. | [runtime.txt](logs/goal-08/runtime.txt), [disk.txt](logs/goal-08/disk.txt), [m2-disk.txt](logs/goal-08/m2-disk.txt), [m2-containers-cleanup.txt](logs/goal-08/m2-containers-cleanup.txt), [docker-info.txt](logs/goal-08/docker-info.txt), [m2-docker-version.txt](logs/goal-08/m2-docker-version.txt), [m2-docker-info.txt](logs/goal-08/m2-docker-info.txt), [m2-containers-labelled-copy.txt](logs/goal-08/m2-containers-labelled-copy.txt) |

Branch history since `main` at `acfe14e` ([revision.txt](logs/goal-08/revision.txt): `commits in origin/main..HEAD: 28`):

- Ten are goal 06: rounds 1-2 (`f38c6ef`, `54503ac`, `a6a5273`, `4c87282`, `3853e34`) and round 3 (`2389948`, `fb4cde2`, `c68113c`, `90652c6`, `0936332`). Fifteen are goal 07: `26454c1` to `ffd2ce9` (its WIP checkpoints, `2e0888d` and `43c1a10` included), the merge `70d55de`, `b782e01`, `45ac279`, `11b81a0`, the node shutdown fix `1e89e60` and `7c79c29`. `main` holds them only as the squashes `f5916bd` and `2fc93e5`. Three are goal 08.
- `4f70733` is a labelled goal 08 WIP checkpoint ("NOT tested as a whole and NOT evidence"). Its parent is goal 07's old head `43c1a10`. It is untested and was pushed only to preserve work.
- `210bfb7` merges goal 07 `7c79c29` into `4f70733`, with DR-06-06 seat-only admission and the review fixes.
- `acfe14e` records `main` `2fc93e5` as merged, with `-s ours`, after the tree-equality check in [stack.txt](logs/goal-08/stack.txt).
- `ea29169`, committed after the first captures, is the second tested commit: tests, README and architecture text, and the mutation runner, on top of `acfe14e` ([m2-revision.txt](logs/goal-08/m2-revision.txt) `git log -3`). It is a fourth goal 08 commit, the 29th since `main`; no log counts the branch at `ea29169` (`git rev-list --count origin/main..ea29169` read by the evidence fixer, not a capture log).

Every log ends with its exit code. 79 of the 89 end `EXIT:0`.

On `acfe14e`, 55 of the 63 end `EXIT:0`. Eight end non-zero, and each is explained below:

- [docker-info.txt](logs/goal-08/docker-info.txt) and [docker-version.txt](logs/goal-08/docker-version.txt) (exit 1: no daemon was running at the first captures; the orchestrator started one later for the `m2-` container runs);
- [goal06-anchors.txt](logs/goal-08/goal06-anchors.txt) and [goal07-anchors.txt](logs/goal-08/goal07-anchors.txt) (exit 1, BLOCKED: anchors moved);
- [golangci-lint.txt](logs/goal-08/golangci-lint.txt) (exit 3, BLOCKED: built with go1.25.1);
- [tests-not-removed.txt](logs/goal-08/tests-not-removed.txt) (exit 1: seven names replaced by design; see [Prerequisites](#prerequisites));
- [tests-python.txt](logs/goal-08/tests-python.txt) (exit 1: the worktree's symlinked `node_modules`; the clean clone passes; see [Gates](#gates));
- [spec-oracle-mypy.txt](logs/goal-08/spec-oracle-mypy.txt) (exit 1: `mypy --strict` on the spec oracle, which the mypy gate does not cover; see [Gates](#gates)).

On `ea29169`, 24 of the 26 `m2-` logs end `EXIT:0`. Two end non-zero, both expected:

- [m2-containers-unchanged.txt](logs/goal-08/m2-containers-unchanged.txt) (exit 1, BLOCKED, environment: the image build fails TLS inside the build container; see [Gates](#gates));
- [m2-ruff-mutants.txt](logs/goal-08/m2-ruff-mutants.txt) (exit 1, informational: `ruff check` on the runner, `Found 41 errors.`, all 41 `E501` line too long; CI's ruff paths do not include `docs/evidence/`).

No `m2-` check was rerun after a failure, so there is no rerun log.

### Large unit logs

The first captures of [unit-app.txt](logs/goal-08/unit-app.txt) and [unit.txt](logs/goal-08/unit.txt) on `acfe14e` (`DATE 2026-11-02T15:30:19Z` and `2026-11-02T15:31:08Z`, commands `env -C chain go test -count=1 -timeout 900s -v ./app/...` and `env SVN_SKIP_DEVNET=1 go test -count=1 -v ./chain/...`, exit 0, `DIRTY no`) were 90,038,980 and 90,232,073 bytes, with sha256 `c287541a37fc1483282df6df57cbe1e555e464252ac08dd74868422bd29dec2c` and `c51fd180b1c780750dbd4c32827d31d4326833146ddf9eb46ae5ec453bfc921b`.

- 425,241 of the 427,476 lines of the first and 425,255 of the 431,409 lines of the second contain `<nil> DBG `. They are SDK and IAVL debug-level logger lines that the `chain/app` tests print through `t.Log` under `-v`. In the first, 407,064 of them are IAVL tree lines about the nodes of synthetic test stores: `recursiveRemove` (233,645), `BATCH SAVE` (154,467) and `SAVE TREE` (18,952). They print keys, values and hashes of synthetic store nodes.
- These raw logs are not committed. Each is above GitHub's 50 MB warning size, and AGENTS.md keeps database dumps out of Git. They are kept only in the session scratch directory (`goal-08-cont/ev/raw90/`, with `stats.txt`).
- The sizes, line counts and hashes were read with `ls`, `wc -l`, `grep -c` and `sha256sum` by the orchestrator, the evidence fixer and this evidence writer. They are not capture logs.
- Both logs were captured again on `acfe14e`, with the filter inside the captured command so that it shows in the `COMMAND` line: `bash -c '<go test command> 2>&1 | grep -a -v -F "<nil> DBG "; exit "${PIPESTATUS[0]}"'`. The filter drops those lines and the command keeps the exit code of `go test`. The committed `unit-app.txt` (`DATE 2026-11-02T17:08:31Z`, 527,389 bytes) and `unit.txt` (`DATE 2026-11-02T17:09:11Z`, 770,935 bytes) are these recaptures; both exit 0 and contain no `<nil> DBG ` line.
- Their counts equal the raw runs': 72 top-level `--- PASS` and 49 subtest `--- PASS` lines for `unit-app.txt`; 431 top-level `--- PASS`, 6 `--- SKIP` and 1458 subtest `--- PASS` lines for `unit.txt`; no `--- FAIL` in any of the four files. Each recapture has as many lines as its raw log has lines without `<nil> DBG ` (2,235 and 6,154).
- [m2-unit-app.txt](logs/goal-08/m2-unit-app.txt) and [m2-unit.txt](logs/goal-08/m2-unit.txt) on `ea29169` use the same filtered commands (564,566 and 809,258 bytes, no `<nil> DBG ` line). A committed log is never edited after capture.

### Second tested commit: `ea29169` (capacity boundary and `max_expiry_steps` halt tests)

The first evidence run left two test gaps. In the mutation run only x/providers tests caught a reservation one byte above capacity ([Mutation checks](#mutation-checks)). And no test damaged the governed `max_expiry_steps`, which the x/authority README said halts the chain. Commit `ea29169196d862867a0e270fada1fda74b1214c6`, tree `eabf7cef6fc24f9afdd07a8f95470ad1f85ba375`, parent `acfe14e`, adds tests for both.

- **What changed.** [m2-revision.txt](logs/goal-08/m2-revision.txt): `11 files changed, 1023 insertions(+), 11 deletions(-)` from `acfe14e`.
  - Tests: `chain/app/export_test.go` (the test-only writer `WriteGovernedLimitForTest`), `chain/app/lease_expiry_test.go`, `chain/app/lease_reject_test.go`, `chain/x/leases/accept_test.go`, `chain/x/leases/difftest/capacity_test.go` (new) and `chain/x/leases/difftest/mutant_test.go` (fixture helpers `newSizedFixture` and `offerSized`).
  - Docs: `chain/x/authority/README.md`, `chain/x/leases/README.md`, `docs/architecture/leases.md`.
  - Evidence tooling: `docs/evidence/goal-08/mutants.py` (runner version 2, committed for the first time) and its `PY_ALLOW` line in `docs/evidence/goal-01/check_requirements.py`.
  - The log's production-path diff, `git diff --name-only acfe14e HEAD -- . ':(exclude)*_test.go' ':(exclude)*.md' ':(exclude)docs/'`, prints `<none>`. No module file changed.
- **New tests.** [m2-tests-not-removed.txt](logs/goal-08/m2-tests-not-removed.txt), exit 0: `417` test/fuzz names on `acfe14e` and `421` on `ea29169`, `base names missing from HEAD: 0`; `439` and `443` (file, name) pairs, none missing. The four new ones:
  - `chain/x/leases/difftest/capacity_test.go:TestCapacityBoundaryAgainstSpecMachine`. It runs on the real x/providers and x/leases keepers with capacity 2001, and compares every step with `spec_machine.py`. [m2-unit-leases.txt](logs/goal-08/m2-unit-leases.txt) logs `accept to exactly the capacity: ok, reserved 2001 of 2001 (spec_machine.py ok)`, `accept one byte over the capacity: ERROR_CODE_CAPACITY, reserved 2001 of 2001 (spec_machine.py ERROR_CODE_CAPACITY)`, `offer one byte over the capacity: ERROR_CODE_CAPACITY, reserved 2001 of 2001 (spec_machine.py ERROR_CODE_CAPACITY)` and `renewal at full capacity: ok, reserved 2001 of 2001 (spec_machine.py ok)`. Then `end l0: ok, reserved 1001 of 2001`; the rejected offer, resent, is `ok` (`reserved 1001 of 2001`), and the rejected acceptance, resent, is `ok, reserved 1002 of 2001`.
  - `chain/app/lease_reject_test.go:TestCapacityBoundaryInApp`. The same boundary through FinalizeBlock with capacity `2*sizeSmall + 1` (2*4,000,000+1). It reads reserved bytes with `Query/Provider` after every step. Each rejection is `CAPACITY` with the ante committed, `FullSnapshot` unchanged and no lease event (code reading; it logs nothing beyond `--- PASS` in [m2-unit-app.txt](logs/goal-08/m2-unit-app.txt)).
  - `chain/x/leases/accept_test.go:TestCapacityBoundaryThroughAccept`. The same steps on the x/leases keeper with the fake provider registry. It pins what x/leases asks of the registry and does not reach `chain/x/providers/handoff.go`.
  - `chain/app/lease_expiry_test.go:TestDamagedExpiryLimitHalts`. A council change sets `max_expiry_steps` to 2, and a block commits. Then `WriteGovernedLimitForTest` overwrites the stored value in the uncommitted working state. Each subtest (`3_bytes`, `value_0`, `ceiling+1`) logs `FinalizeBlock panic on both nodes, twice each: authority: governed limit "max_expiry_steps" is not an 8-byte value in 1..ceiling (3 bytes stored)`, or `(8 bytes stored)` for the last two ([m2-unit-app.txt](logs/goal-08/m2-unit-app.txt)). The block carries one valid `OfferLease` and nothing is due. The test asserts that the last commit id, `FullSnapshot` and the sender's sequence do not move (code reading). It calls FinalizeBlock directly. CometBFT stopping on the panic and a restart on a committed damaged value are not tested.
- **Logs on `ea29169`.** Each `m2-` log prints HEAD `ea29169`, TREE `eabf7cef` and `DIRTY no`.
  - [m2-gofmt-vet.txt](logs/goal-08/m2-gofmt-vet.txt), exit 0: `gofmt ok files 244`, `gofmt_check=0 gofmt_l=0 vet_chain=0 vet_proto=0 vet_agents_provider=0`.
  - The unit suites [m2-unit-leases.txt](logs/goal-08/m2-unit-leases.txt), [m2-unit-app.txt](logs/goal-08/m2-unit-app.txt), [m2-unit.txt](logs/goal-08/m2-unit.txt), [m2-unit-providers.txt](logs/goal-08/m2-unit-providers.txt) and [m2-unit-authority.txt](logs/goal-08/m2-unit-authority.txt) replay the first-commit commands. All exit 0 with no `--- FAIL` ([Unit suites](#unit-suites)). `TestDifferentialAgainstSpecMachine` again logs `256 sequences, 22862 compared steps, 0 divergences`.
  - [m2-race.txt](logs/goal-08/m2-race.txt): `go test -race -count=1 -timeout 20m ./chain/...` through the network-namespace wrapper, exit 0. 15 packages `ok`, 4 with no test files, no `DATA RACE`.
  - [m2-devnet.txt](logs/goal-08/m2-devnet.txt): the devnet package, exit 0, 23 top-level tests and 49 subtests pass, `ok ... chain/devnet 313.409s`, no `leveldb: closed` and no `exit status 2`.
  - The mutation runner, version 2: 28 of 28 killed ([Mutation checks](#mutation-checks)).
  - [m2-check-requirements.txt](logs/goal-08/m2-check-requirements.txt) and [m2-check-protocol.txt](logs/goal-08/m2-check-protocol.txt) `result PASS`; [m2-scan-secrets.txt](logs/goal-08/m2-scan-secrets.txt) `secret scan ok`.
  - [m2-mypy-mutants.txt](logs/goal-08/m2-mypy-mutants.txt) (`mypy --strict` on the runner): `Success: no issues found in 1 source file`. [m2-ruff-mutants.txt](logs/goal-08/m2-ruff-mutants.txt): 41 `E501` findings, exit 1.
  - Docker and containers: [m2-docker-version.txt](logs/goal-08/m2-docker-version.txt), [m2-docker-info.txt](logs/goal-08/m2-docker-info.txt), [m2-disk.txt](logs/goal-08/m2-disk.txt), [m2-docker-state-before.txt](logs/goal-08/m2-docker-state-before.txt), [m2-containers-unchanged.txt](logs/goal-08/m2-containers-unchanged.txt), [m2-containers-labelled-copy.txt](logs/goal-08/m2-containers-labelled-copy.txt) and [m2-containers-cleanup.txt](logs/goal-08/m2-containers-cleanup.txt) ([Gates](#gates)).
- **Containers.** [m2-docker-state-before.txt](logs/goal-08/m2-docker-state-before.txt) lists four exited `chain-v1-1` to `chain-v4-1` containers created `2026-11-02 04:45:04 +0000 UTC` (goal 07's run) and image `svn-devnet:goal05` `b01d7e489ef5`. The labelled copy rebuilt `svn-devnet:goal05` (manifest list `sha256:2fe0df9458e6c172b8be1a57e5a054ece953388a137e9ee6c69ef2605eb9f5c3`, config `sha256:b4ea87b3d36ae7435d9128c062cfdb81979f4e0e6c2d438af43ee6ebd8a02626`) and compose recreated the four containers. [m2-containers-cleanup.txt](logs/goal-08/m2-containers-cleanup.txt) removes only the four containers that run created (`2026-11-02 18:43:14 +0000 UTC`); afterwards no container is listed, and the networks are the default `bridge`, `host` and `none`. The build cache grew from 6.014GB (30 entries, [m2-disk.txt](logs/goal-08/m2-disk.txt)) to 8.357GB (38 entries, 8.093GB reclaimable), and `/` went from 3.9G to 1.8G available. The cache was not pruned (not authorized). The node homes stay under `.local/devnet/`, which `.gitignore` excludes: `prove-containers-2026-11-02T18-41-00+00-00` (1.6M, this run's dev-only node homes) and an empty `prove-containers-2026-11-02T18-40-23+00-00` left by the unchanged script (read with `du` and `git check-ignore` by this evidence writer; not a capture log).
- **What still stands from `acfe14e`.** `ea29169` changes no production file. Its Go changes are test files in `chain/app`, `chain/x/leases` and `chain/x/leases/difftest`. The suites of those packages, `-race` over `./chain/...`, the devnet package and the three mutant groups were run again. The other first-commit logs were not rerun on `ea29169`: the seven fuzz runs, `fuzz-list`, `tests-go`, `tests-go-count1`, `tests-python`, `tests-python-clean-clone`, `tests-node`, `npm-typecheck`, `ruff-format`, `ruff-check`, `mypy`, the three `spec-oracle-*` logs, `golangci-lint`, `sbom-check`, `workflow-policy`, the three `go-mod-tidy-*` logs, `gomod-vs-main`, `proto-regen`, `protocol-unchanged`, `goal06-anchors`, `goal07-anchors`, `race-proto`, `race-agents-provider`, `unit-content`, `unit-genesis`, `unit-msgkit`, `unit-node`, `unit-svnerr`, `devnet-lease`, `tests-replaced`, `runtime`, `stack` and `diff-stat`. The last three describe `acfe14e` and its branch.
  - The tests of `unit-content`, `unit-genesis`, `unit-msgkit`, `unit-node` and `unit-svnerr` ran again inside [m2-unit.txt](logs/goal-08/m2-unit.txt) (`./chain/...`): per package, the same top-level test names as those five logs, all `--- PASS`. The two tests of `devnet-lease`, `TestDevnetLeaseLifecycle` and `TestDevnetLeaseHaltRestart`, ran again inside [m2-devnet.txt](logs/goal-08/m2-devnet.txt), both `--- PASS`. Their own commands were not rerun.
  - The others did not run again: the fuzz runs and `fuzz-list`; the Python, Node and `run_tests.py --only go` suites; the format, lint and type checks (`ruff-format`, `ruff-check`, `mypy`, `npm-typecheck`, `golangci-lint`); the three `spec-oracle-*` logs (ruff, self-test, `mypy --strict`); `sbom-check` and `workflow-policy`; the module and proto checks (`go-mod-tidy-*`, `gomod-vs-main`, `proto-regen`, `protocol-unchanged`); `tests-replaced`; the goal 06 and goal 07 anchors; `race-proto` and `race-agents-provider`. This file reads their results as standing for `ea29169`, because the code they test did not change. The two lease fuzz targets are in `chain/x/leases/difftest/diff_test.go` and do not call the fixture helpers `newFixture` and `offerBy` that `ea29169` changed (code reading). That reading is not a rerun.

### Merge, DR-06-06 and review rounds

The summary below comes from the continuation workflow's results: a merge, the DR-06-06 implementation, a review-fix stage, three independent reviews and a fix-and-gate stage. Those are workflow outputs, not capture logs. Each test named here passes in the logs cited.

- **Merge.** Goal 07, as rebuilt on goal 06 round 3, was merged into `4f70733`. Seven files conflicted: `chain/README.md`, `chain/app/app.go`, `chain/app/export_test.go`, `chain/app/registry_flow_test.go`, `chain/app/services.go`, `chain/x/authority/README.md` and `chain/x/authority/read.go`. x/leases now sits on the round-3 authority design: the per-object store, the export refusal (DR-06-01) and `ValidateAuthorityGenesis`. `DevnetNumeric` and `PolicyVersion` were ported to the per-object layout (`TestDevnetNumeric`, `TestPolicyVersionRead` in [unit-authority.txt](logs/goal-08/unit-authority.txt)). The goal 07 node shutdown fix (`closeSafeDB`) comes in from `7c79c29`: `chain/node/db.go` and `chain/node/db_test.go` are unchanged, and the seven `db_test.go` tests pass in [unit-node.txt](logs/goal-08/unit-node.txt) (8 tests in all, with `TestNodeFileQueryGasLimit`). `chain/node/start.go` changed against `7c79c29` only in a comment (the query gas scope). The merge first used goal 07's earlier head `45ac279`; a review found that, and the fix stage moved the merge to `7c79c29`.
- **Review-fix stage.** Sixteen pending findings of the earlier goal 08 review were fixed. They added deviations D-10 and D-11 and decision requests DR-08-11 to DR-08-13, and rewrote DR-08-02, DR-08-03, DR-08-07 and DR-08-08. New tests include `TestOpenOffersPerSchedulerCapD10`, `TestOfferReplayByAnotherSchedulerD11`, `TestOneSchedulerRefillsProviderSlots`, `TestRetryAfterKeyRotation`, `TestGeneratorRestrictionsHideKnownDivergences` and `TestCheckPackage`.
- **Three reviews, then fixes.** The reviews made eleven reports: ten distinct findings, four major and six minor (the seat test comment was reported twice). All ten were fixed; none was rejected.

| Severity | Finding | Fix | Test or log |
| --- | --- | --- | --- |
| major | The DR-08-02 consequence was false: one admitted service alone can block every other scheduler's offers of any content id, through the shared per-provider cap. | Docs rewritten (README, leases.md, a `keeper.go` comment); DR-08-13 cross-referenced. | `TestOneSchedulerRefillsProviderSlots` ([unit-leases.txt](logs/goal-08/unit-leases.txt)) |
| major | The provider END payload bound only the lease id and provider id. An END signed before a renewal could end the renewed lease. | D-12: the END payload binds the lease's current term version. Sign vectors regenerated. | `TestEndSignedForEarlierTermD12`, `TestSignBytesVectors`; mutant `m5-end-payload-drops-term-version` killed ([mutants-review.txt](logs/goal-08/mutants-review.txt)) |
| major | The genesis content-cap row could not detect a missing cap: the per-scheduler cap fired first. | The row uses three schedulers and asserts exact error strings. | `TestGenesisValidateImportExport`; mutant `m1-genesis-content-cap-off` killed |
| major | The worktree merged goal 07's earlier head, without the node shutdown fix. | Merged `7c79c29` instead. | [stack.txt](logs/goal-08/stack.txt); [unit-node.txt](logs/goal-08/unit-node.txt) |
| minor | An `AcceptLease` replay by another provider's account returned idempotent success. | D-13: `FORBIDDEN`; a `BROADCASTER` replay stays idempotent. DR-08-14. | `TestAcceptReplayByAnotherProviderD13`; mutant `m6-accept-replay-owner-check-off` killed |
| minor | The D-6 tests could not show that D-6 caused the rejection. | The 257th offer comes from a third service. | mutant `m2-offer-d6-off` killed by `TestWithdrawalAtContentCapGas` and `TestWithdrawalAtCapGasInApp` among others |
| minor | The goal 11 resubmission handoff listed "two exceptions" only. | Rewritten: every check before the business gate runs again on a resubmission. | Doc only |
| minor | Stale check-order text. | Fixed in the README and the `msg_server.go` header. | Doc only |
| minor | The shared expiry budget lets offers delay lease releases; undocumented. | DR-08-07 states the consequence and the options. No code change. | Doc only |
| minor | The seat test comment said "twelve" messages. | Now "thirteen (52 rows)". | Doc only |

### Head CI

The owner authorized a draft pull request for `goal-08` on 2026-11-02 (answer to the question "Should I open a draft PR for goal-08 against main so CI runs?": "Open draft PR (Recommended)"). [PR 11](https://github.com/Maxencejules/shortvideo-network/pull/11) was opened at 19:25 UTC. The results below were read from PR 11's check runs and the job log through the GitHub API by the orchestrator at about 19:20-20:50 UTC on 2026-11-02; they are not `capture.sh` logs.

- Run [41509776341](https://github.com/Maxencejules/shortvideo-network/actions/runs/41509776341) on the evidence commit `69f3980`: `node`, `python`, `devnet` and `contract` `success`; `go` `cancelled`. The `go` job hit its 20-minute `timeout-minutes` while its last command, `go test -race -count=1 -timeout 12m ./chain/...`, was still running: its log shows `run_tests.py --only go` finished with every package `ok` (chain/devnet `305.248s`, chain/x/leases/difftest `59.190s`), then the race run printed `ok ... chain/app 523.689s` and `ok ... chain/cmd/svnd 1.751s` before `##[error]The operation was canceled.`, and the runner then terminated `devnet.test` and `difftest.test` processes. No test failed. `scripts/workflow_policy.py` caps every job at 20 minutes (`MAX_TIMEOUT = 20`), so the cap was not raised.
- Commit `4db7e40` changes only `.github/workflows/ci.yml`: the race commands move to a new `go-race` job with a three-way matrix (`app`: `./chain/app`; `devnet`: `./chain/devnet`; `rest`: every other `./chain/...` package, computed with `go list`, then `./proto/...` and `./agents/provider/...`), with the same flags as before. The three shards cover the 19 `./chain/...` packages exactly. Before the push, `scripts/workflow_policy.py` exited 0, `tests/unit/test_workflow_policy.py` passed, and the step script run here per shard exited 0 with no data race (rest 327 s, app 210 s, devnet 474 s); those local runs are scratch output, not capture logs. `git diff ea29169 4db7e40 -- . ':!docs' ':!.github'` is empty, so CI on `4db7e40` tests the code tree of `ea29169`.
- Run [41521039450](https://github.com/Maxencejules/shortvideo-network/actions/runs/41521039450) on `4db7e40`: all eight checks `success`: `go` (20:38:22-20:46:15Z), `go-race (app)` (to 20:44:36Z), `go-race (rest)` (to 20:45:37Z), `go-race (devnet)` (to 20:47:10Z), `python` (ruff, mypy, workflow policy, `scan_secrets.py`, SBOM check and the Python suite), `node`, `devnet` (the unchanged `scripts/devnet/prove-containers.sh`, on GitHub's runner) and `contract` (`check_requirements.py` and `check_protocol.py`). The secret scan and both contract checkers therefore ran on the committed evidence (this file's first version, the 89 logs and the progress rows).

So head CI is VERIFIED on `4db7e40`, the unchanged CI `devnet` job passed in CI, and the post-docs checks ran in CI. This file and the progress rows were edited again afterwards to record this; that edit changes only `docs/`.

## Prerequisites

| Prerequisite | Status | Evidence |
| --- | --- | --- |
| Goal 07 | Merged into `main` (PR 10, `2fc93e5`); its evidence status is BLOCKED ([goal-07.md](goal-07.md)) | [stack.txt](logs/goal-08/stack.txt). Goal 07's handoff APIs are consumed here: `CheckNewObligation`, `VerifyProviderSignature`, `CheckOfferable`, `CheckNotWithdrawn`, and the withdrawal hook (`WithdrawalHook`). `TestHandoffAPI` ([unit-providers.txt](logs/goal-08/unit-providers.txt)) and `TestCheckOfferableOrder`, `TestWithdrawalBlocksOfferAndRevival` ([unit-content.txt](logs/goal-08/unit-content.txt)) pass. Goal 08 adds `ReserveProbe`, `Reserve` and `Release` to the providers keeper (`TestReserveReleaseAccounting`, `FuzzReserveRelease` in [unit-providers.txt](logs/goal-08/unit-providers.txt)) and `CheckPackage` to the content keeper (`TestCheckPackage` in [unit-content.txt](logs/goal-08/unit-content.txt)); none of the four is on `main` `2fc93e5` (code reading). |
| Approved lease state/terms specification | Candidate, not approved (BLOCKED, item 4) | Protocol v0 (goal 02, IMPLEMENTED) is the frozen candidate. No file under `docs/protocol`, `docs/protocol.md`, `docs/goals`, `.github/workflows/ci.yml` or `proto/` changed since `main` ([protocol-unchanged.txt](logs/goal-08/protocol-unchanged.txt), exit 0). Regeneration gives no diff ([proto-regen.txt](logs/goal-08/proto-regen.txt): `generated 28 proto files`, `proto regeneration diff empty`). |
| Approved decisions | APPROVED at Gate A | DEC-001, DEC-002, DEC-005 and DEC-009 in [decision-register.md](../decision-register.md). DEC-005 leaves lease durations, allowances and sponsors unset. |
| Dependencies | Unchanged since `main` | [gomod-vs-main.txt](logs/goal-08/gomod-vs-main.txt) (exit 0, `go.work`, `go.work.sum` and the three modules' `go.mod`/`go.sum`); `go mod tidy -diff` is empty for `chain`, `proto` and `agents/provider` ([go-mod-tidy-chain.txt](logs/goal-08/go-mod-tidy-chain.txt), [go-mod-tidy-proto.txt](logs/goal-08/go-mod-tidy-proto.txt), [go-mod-tidy-agents-provider.txt](logs/goal-08/go-mod-tidy-agents-provider.txt)). `ea29169` changes no module file ([m2-revision.txt](logs/goal-08/m2-revision.txt)). |
| No test removed since the base | IMPLEMENTED (deviation recorded: seven names replaced by design) | [tests-not-removed.txt](logs/goal-08/tests-not-removed.txt) exits 1: `HEAD ... 417 test/fuzz function names`, `7c79c29 ... 323 names`, `4f70733 ... 317 names`, `union ... 410 names`, `union names missing from HEAD: 7`, `HEAD names not in the union: 14`. Three were defined in `7c79c29` and are replaced by DR-06-06 tests: `TestSeatAddressIsBroadcasterOnly` and `TestSeatAddressRole` by `TestSeatAddressIsNotASubmitter` (app and genesis), `TestSeatRotationMovesAdmission` by `TestSeatRotationMovesSeatAdmission`. Four were defined only in `4f70733` (and `ffd2ce9`); goal 07's merge `70d55de` had already replaced them, and their replacements are listed in [goal-07.md](goal-07.md). [tests-replaced.txt](logs/goal-08/tests-replaced.txt) finds all 13 replacement and DR-06-06 functions. The no-removal check itself exits 1. From `acfe14e` to `ea29169` nothing was removed: [m2-tests-not-removed.txt](logs/goal-08/m2-tests-not-removed.txt) exits 0 with `417` and `421` names, `base names missing from HEAD: 0`, `HEAD names not in base: 4`, and `439` and `443` (file, name) pairs, none missing. |

## Acceptance

Each row is one acceptance check of [the goal 08 page](../goals/08-lease-lifecycle-and-capacity-accounting.md). "Keeper level" means the x/leases package tests (unit-test fakes of the other modules) or `chain/x/leases/difftest` (real x/providers, x/content and x/leases keepers on IAVL stores, with a fake authority read surface). "App level" means `chain/app`: in-process FinalizeBlock with real keepers and signed transactions. "Devnet" means four `svnd` processes on one machine. Devnet heights below are from [devnet-lease.txt](logs/goal-08/devnet-lease.txt); the full package runs [devnet.txt](logs/goal-08/devnet.txt) on `acfe14e` and [m2-devnet.txt](logs/goal-08/m2-devnet.txt) on `ea29169` pass the same two lease tests with their own heights (`TestDevnetLeaseLifecycle (100.36s)`, `TestDevnetLeaseHaltRestart (33.09s)`; `TestDevnetLeaseLifecycle (100.60s)`, `TestDevnetLeaseHaltRestart (34.21s)`). Logs without a prefix ran on tree `6e81d0c9` (`acfe14e`); `m2-` logs ran on tree `eabf7cef` (`ea29169`).

| # | Check | Status | Evidence |
| --- | --- | --- | --- |
| A1 | Property-test transition sequences with an independent accounting oracle | VERIFIED, keeper level | The oracle is `docs/evidence/goal-02/spec_machine.py`, the goal 02 Python reference machine, driven by `tests/difftest/spec_oracle.py`. It is an independent implementation, not an independent party. `TestDifferentialAgainstSpecMachine`: `256 sequences, 22862 compared steps, 0 divergences`. `TestDifferentialCoverage` requires the seeded run to reach every required class and logs `differential coverage (113 classes)`. `TestDeviationRulesAreLoadBearing`: for each of D-2, D-3, D-4, D-5, D-6, D-10, D-11, D-12 and D-13 the chain rejects at one step where `spec_machine.py alone: ok`. The random generator does not reach D-6, D-10, D-11, D-12 or D-13: neither the 113-class table nor the 93-class fuzz seed table has a class for them. Those rules are covered by the `TestDeviationRulesAreLoadBearing` fixtures, the keeper tests `TestOpenOffersPerContentCapD6`, `TestOpenOffersPerSchedulerCapD10`, `TestOfferReplayByAnotherSchedulerD11`, `TestEndSignedForEarlierTermD12` and `TestAcceptReplayByAnotherProviderD13`, and the scale test `TestWithdrawalAtContentCapGas` (D-6). `TestComparatorDetectsMismatch`, `TestTrialSnapshotDetectsEarlyWrite` and `18 spec mutants and 1 Go mutant detected` show the comparison can fail. `TestFuzzReachesStates`: `fuzz seed corpus (15 inputs) coverage (93 classes)` ([unit-leases.txt](logs/goal-08/unit-leases.txt)). Real fuzz runs: `FuzzLeaseDifferential` and `FuzzLeaseTransitions`, 60 s each ([Fuzz runs](#fuzz-runs)). Oracle self-test: `spec_oracle self-test: PASS (82 steps)` ([spec-oracle-selftest.txt](logs/goal-08/spec-oracle-selftest.txt)). |
| A2 | Cover over-capacity acceptance | VERIFIED | Differential classes `AcceptLease CAPACITY 86` and `OfferLease CAPACITY 370` ([unit-leases.txt](logs/goal-08/unit-leases.txt)). `TestCapacityAndOverflow` (keeper). Devnet: an acceptance passes CheckTx and fails in its block, `phase=ack-vs-commit ... ack=0 committed=svn/13 height=32 p3_reserved=9000000`. The exact boundary, one byte on each side of the capacity, is pinned at lease level on `ea29169`: `TestCapacityBoundaryAgainstSpecMachine` on the real x/providers and x/leases keepers, every step equal to `spec_machine.py` (`accept to exactly the capacity: ok, reserved 2001 of 2001 (spec_machine.py ok)`, `accept one byte over the capacity: ERROR_CODE_CAPACITY, reserved 2001 of 2001 (spec_machine.py ERROR_CODE_CAPACITY)`, [m2-unit-leases.txt](logs/goal-08/m2-unit-leases.txt)); `TestCapacityBoundaryInApp` through FinalizeBlock ([m2-unit-app.txt](logs/goal-08/m2-unit-app.txt)); and `TestCapacityBoundaryThroughAccept` on the keeper with the fake provider registry ([m2-unit-leases.txt](logs/goal-08/m2-unit-leases.txt)). Mutant `capacity-off-by-one` is killed by `TestCapacityBoundaryAgainstSpecMachine`, `TestReserveReleaseAccounting`, `TestAccountingFuzzSeedsReachEveryOutcome` and `TestCapacityBoundaryInApp` ([m2-mutants-leases.txt](logs/goal-08/m2-mutants-leases.txt)). On `acfe14e`, before those tests existed, only the two x/providers tests killed it ([mutants-leases.txt](logs/goal-08/mutants-leases.txt)). |
| A3 | Cover stale terms | VERIFIED | Offer `term_version` other than 1 and renewal `term_version` other than current + 1 are `STALE_VERSION` (ladder rows `term-version-2` and `term-version-stale` of `TestOfferCheckOrder` and `TestRenewCheckOrder`; `TestRenewKeepsCommittedBytes`). An END signed for an earlier term is `UNAUTHORIZED` (`TestEndSignedForEarlierTermD12`, D-12). An acceptance over changed terms: `AcceptLease UNAUTHORIZED(mutated terms) 97` ([unit-leases.txt](logs/goal-08/unit-leases.txt)). Devnet `stale-renewal height=47 codespace=svn code=5 (ERROR_CODE_STALE_VERSION)`. |
| A4 | Cover wrong provider keys | VERIFIED | `TestServiceKeyCannotSignAcceptance` (keeper) and `TestServiceKeyCannotSignAcceptanceInApp` ([unit-app.txt](logs/goal-08/unit-app.txt)). Differential classes `UNAUTHORIZED(other provider key) 75`, `UNAUTHORIZED(retired key) 10`, `UNAUTHORIZED(service key, own key id) 63`, `UNAUTHORIZED(service key, provider key id) 81`. `TestRetryAfterKeyRotation`. Devnet `service-key-acceptance height=62 codespace=svn code=1 (ERROR_CODE_UNAUTHORIZED)`. Mutant `service-key-signs-acceptance` killed. |
| A5 | Cover duplicate IDs | VERIFIED | Open offer id, offer id used before (D-4, `TestOfferIDNeverReusedD4`), lease id that exists, and the live (provider, content, version) pair (`TestDuplicateReplicaRejected`). Differential classes `OfferLease DUPLICATE_ID(D-4) 67`, `DUPLICATE_ID(open id) 69`, `DUPLICATE_ID(pair) 441`, `AcceptLease DUPLICATE_ID(lease) 106`, `DUPLICATE_ID(pair) 136`. Devnet `duplicate-replica height=60 codespace=svn code=4 (ERROR_CODE_DUPLICATE_ID)`. Mutants `accept-duplicate-copy-allowed` and `offer-duplicate-copy-allowed` killed. |
| A6 | Cover overflow | VERIFIED, keeper level | Differential classes `OfferLease OVERFLOW 148` and `AcceptLease OVERFLOW 20` (the `near-max` family seeds reserved bytes near 2^64-1). `TestCapacityAndOverflow`. Reserve and release on the real providers keeper: `TestReserveReleaseAccounting`, and `FuzzReserveRelease` fuzzed 30 s ([fuzz-FuzzReserveRelease.txt](logs/goal-08/fuzz-FuzzReserveRelease.txt)). An integer above 2^53-1 in a business payload is `MALFORMED`: `OfferLease MALFORMED(huge int) 215`, `RenewLease MALFORMED(huge int) 76`. No committed app transaction reaches `OVERFLOW`: it needs reserved bytes near 2^64 ([docs/architecture/leases.md](../architecture/leases.md), `TestRejectedLeaseMessagesLeaveTheFullState` row). |
| A7 | Cover repeated ending | VERIFIED | `TestEndReleasesOnce`. Differential classes `EndLease UNDERFLOW(repeat) 252` and `EndLease UNDERFLOW(after expiry) 83`. Devnet: `end-retry height=56 code=0 events=[leases_result]`, then `end-again-new-business-id height=57 codespace=svn code=11 (ERROR_CODE_UNDERFLOW)`. Mutants `end-releases-twice`, `end-held-guard-removed` and `expiry-releases-twice` killed. |
| A8 | Cover early and late renewal | VERIFIED for the interim renewal window (DR-08-09) | Early: renewals while the reservation is held stack, one DEC-005 term each (`TestRenewKeepsCommittedBytes`). Late: `TestRenewPastDeadlineIsExpiredD2` renews at deadline - 1 and is `EXPIRED` at the deadline and after, with the lease unchanged (D-2). Differential classes `RenewLease EXPIRED(D-2) 17` and `RenewLease CONFLICT(state) 419`. App: during an expiry backlog, a renewal of an unswept due lease is `EXPIRED` (`TestLargeExpiringCatalogue`). Devnet: the offline renewal sent after the deadline is `offline-renewal-after-restart height=20 codespace=svn code=14 (ERROR_CODE_CONFLICT)`. When an early renewal may start is DR-08-09. |
| A9 | Cover withdrawn content | VERIFIED | `TestWithdrawalVoidsOffers`, `TestWithdrawalAtContentCap` (keeper); `TestWithdrawalAtCapGasInApp`. Differential classes `OfferLease WITHDRAWN 180` and `RenewLease WITHDRAWN 64`. Devnet: `withdraw-c4 height=410 code=0 voided_offers=1`, `accept-voided-offer height=412 ... code=16 (ERROR_CODE_NOT_FOUND)`, `offer-withdrawn-content height=414 ... code=15 (ERROR_CODE_WITHDRAWN)`, `renew-withdrawn-content height=415 ... code=15 (ERROR_CODE_WITHDRAWN)`, then `authority-end-lw height=416 code=0`. An acceptance on withdrawn content is reached only with fakes, because withdrawal voids the offers first. |
| A10 | Run real transactions with timeout and retry; one final obligation with correct reserved bytes | VERIFIED, single machine | Devnet `phase=timeout-retry first=... height=35 events=[svn.events.v1.LeaseAccepted] retry=... height=35 events=[leases_result] p1_reserved=4000000 leases=1`. The test asserts one held lease and the reserved bytes of one package (`chain/devnet/lease_test.go`, code reading). A changed payload under the same business id: `height=38 ... code=3 (ERROR_CODE_REPLAY)`. Node down: the broadcast to v4 fails (`connection refused`); the same signed bytes commit once through v1 (`node-down-retry node=v1 height=40 code=0`); a rebroadcast is `check_tx=sdk/32 broadcast="mempool ErrTxInCache" original_height=40 included_again=false`; v4 restarts with `common_height=44 queries_equal_v1=true`. Renewal and end retries: `renew-retry height=46 code=0 events=[leases_result]`, `end-retry height=56 code=0 events=[leases_result]`. App `TestLeaseRetryScenarios` ([unit-app.txt](logs/goal-08/unit-app.txt)). |
| A11 | Failed operations leave consistent state | VERIFIED | App `TestRejectedLeaseMessagesLeaveTheFullState`: exact svn code, ante committed, `FullSnapshot` byte-identical, no lease event; the rows it cannot reach are listed with their reasons in [docs/architecture/leases.md](../architecture/leases.md). Keeper ladders `TestOfferCheckOrder`, `TestAcceptCheckOrder`, `TestRenewCheckOrder`, `TestEndCheckOrder` and `TestAuthorityEndCheckOrder` compare the leases, providers and content stores byte for byte per row ([unit-leases.txt](logs/goal-08/unit-leases.txt)). `FuzzLeaseTransitions` checks invariants I1-I10 and an unchanged trial branch after every rejected step. Devnet: 12 `phase=replay-reject ... full_state_unchanged=true` lines in `TestDevnetLeaseLifecycle` and one in `TestDevnetLeaseHaltRestart`. An expiry invariant violation fails FinalizeBlock on both nodes and the commit does not move: `FinalizeBlock error on both nodes: leases: release of expiring lease "l-p1c1" on provider "p1": ERROR_CODE_UNDERFLOW` (`TestExpiryInvariantViolationHalts`). |
| A12 | Replay the same committed inputs and compare hashes | VERIFIED | Devnet `phase=replay height=434 app_hash=59FB238D8D6E58B3CB86C117A23B453F5BD41D15B97E19FAD7AF713D48649B01 nodes=4 equal=true` and `phase=replay-queries height=434 count=6 equal=true`; `TestDevnetLeaseHaltRestart`: `phase=replay height=20 app_hash=759B9F765FEFF323F2AD40A2C796F7E5A93D03837C3F1FE883D22DF73E319064 nodes=4 equal=true`. App `TestLeaseFlowReplayAndQueries`: `replayed 69 blocks; app hash A7D811144DE9E4747FDFB39743C00845FF89698E3C818C12922515A2DBCCDCFE equal at every height` ([unit-app.txt](logs/goal-08/unit-app.txt)). The hash differs between runs; [m2-unit-app.txt](logs/goal-08/m2-unit-app.txt) has `F18CAC0609B2A90D9FFDFDA58AC48861608A255BE3AA79641FF38D8BB9C44DB2`. That is expected, not a determinism failure: the test network draws fresh development keys in every run (`newSecp` and `newSeat` call `GenPrivKey` in `chain/app/registry_net_test.go`, code reading), so accounts, signatures and genesis differ per run. Within one run the replay matches at every height. `TestRestartContinuesExpiry`: `restarted the goleveldb node at height 38 with 6 items left`; the test compares the restarted node's app hash with a node that never stopped ([unit-app.txt](logs/goal-08/unit-app.txt); the comparison is described in [docs/architecture/leases.md](../architecture/leases.md)). |
| A13 | Large expiring catalogue: bounded per-block work, not an unbounded sweep | VERIFIED | App `TestLargeExpiringCatalogue`: `catalogue: 2048 leases and 256 open offers due at header time 1700020000`, `drained 2303 items in 18 blocks: [128 128 128 128 128 128 128 128 128 128 128 128 128 128 128 128 128 127]` (one of the 2,304 due items is a lease ended by its provider during the backlog). Difftest `TestExpiryWorkIndependentOfBacklog`: `first full block (128 lease expiries): store gas 2596919 at backlog 1,280 and at backlog 4,352`. Keeper `TestExpiryWorkIsBounded`: `backlog 200: one block expired 128 leases with 2602732 store gas` and the same for `backlog 600`. Devnet, governed budget 4: `phase=bounded-drain max_expiry_steps=4 shared_deadline=1793634479 heights=400..407 effective_minus_1=399 header_time=1793634478 counts=[4 4 4 4 4 4 4 2] order=(time,kind,id)`. Effective-1 and effective of one expiry: `phase=expiry deadline=1793634432 effective_minus_1=182 header_time=1793634431 held=true effective=183 header_time=1793634432 held=false released=4000000`. Mutants `expiry-budget-ignored` and `expiry-merge-ignores-budget` killed. A damaged budget fails closed: on `ea29169`, `TestDamagedExpiryLimitHalts` stores 3 bytes, the value 0 or the ceiling + 1 as `max_expiry_steps`, and FinalizeBlock panics on both nodes, twice each, with `authority: governed limit "max_expiry_steps" is not an 8-byte value in 1..ceiling` ([m2-unit-app.txt](logs/goal-08/m2-unit-app.txt)). Mutant `expiry-limit-damage-ignored`, which falls back to the protocol ceiling instead, is killed by that test ([m2-mutants-leases.txt](logs/goal-08/m2-mutants-leases.txt)). CometBFT stopping on that panic is not tested ([Residual risks](#residual-risks)). |

## Requirements rows for goal 08

The rows of the `requirements-matrix` block whose goal column contains `08`, plus the lease half of REQ-INV-006, which goal 07 deferred here. This is the requirements-to-tests matrix for this goal.

| Requirement | Status | Evidence |
| --- | --- | --- |
| REQ-BOUND-004 ACTIVE is distinct from reachability and publication | VERIFIED | `TestLeaseStateHasNoAtRisk` and `TestServiceDescriptorsMatchTheProtocol` ([unit-leases.txt](logs/goal-08/unit-leases.txt)). `TestLeaseMessagesDecode` logs `15 registered sdk.Msg types; svn.leases.v1: [/svn.leases.v1.MsgAcceptLease /svn.leases.v1.MsgEndLease /svn.leases.v1.MsgOfferLease /svn.leases.v1.MsgRenewLease]` and asserts that no registered message names AT_RISK or reachability ([unit-app.txt](logs/goal-08/unit-app.txt)). |
| REQ-CHAIN-012 a service key is not the provider acceptance signature | VERIFIED | As A4. The service account relaying a valid acceptance is `FORBIDDEN`: devnet `service-outer-signer height=64 codespace=svn code=2 (ERROR_CODE_FORBIDDEN)`. Mutant `service-key-signs-acceptance` is killed by `TestServiceKeyCannotSignAcceptance`, `TestDifferentialAgainstSpecMachine`, `TestServiceKeyCannotSignAcceptanceInApp` and `TestRejectedLeaseMessagesLeaveTheFullState` ([mutants-leases.txt](logs/goal-08/mutants-leases.txt)). |
| REQ-MODU-004 lease record fields | VERIFIED | `TestAcceptReservesOnceAndBindsTerms` (keeper). `TestLeaseFlowReplayAndQueries` decodes committed leases with the offer terms, the reservation and the policy version ([unit-app.txt](logs/goal-08/unit-app.txt)). Devnet `active-c1` and `active-p1` query bytes equal on 4 nodes at height 434. |
| REQ-IDX-001 content, provider and deadline indexes; paginated queries; bounded work per block | VERIFIED | `TestActiveLeasesPagination`, `TestDeadlinesPagination`, `TestLeaseQueryResponseSizeBound` (`ActiveLeases page of 100 widest leases: 40968 bytes (bound 65536)`, `largest Deadlines page: 40968 bytes over 3 pages (bound 65536)`), `TestExpiryOrderAndBudget` ([unit-leases.txt](logs/goal-08/unit-leases.txt)). `FuzzDeadlinesPageKey` fuzzed 60 s. `TestLeaseQueryGasAndBounds`: `/svn.leases.v1.Query/ActiveLeases: query gas used 5425 (limit 20000000)` and `/svn.leases.v1.Query/Deadlines: query gas used 10099 (limit 20000000)` ([unit-app.txt](logs/goal-08/unit-app.txt)). Bounded work: A13. Raw `/store/leases/*` ABCI queries are not metered or paged (residual risk). |
| REQ-SEP-001, renewal keeps the committed digest and byte count | VERIFIED | `TestRenewKeepsCommittedBytes`; differential `RenewLease CONFLICT(bytes) 28`; `InitGenesis` checks every lease's digest and bytes against its content record (`TestCheckPackage` in [unit-content.txt](logs/goal-08/unit-content.txt), `TestGenesisValidateImportExport`). Devnet `renewal-other-bytes height=49 codespace=svn code=14 (ERROR_CODE_CONFLICT)`. |
| REQ-SEP-001, a new encode uses a new content version | BLOCKED, DR-07-04 | v0 content records use version 1 only; x/content rejects any other version. The stand-in, a new encoding as its own content id with its own lease (`runLeaseFlow` in `TestLeaseFlowReplayAndQueries`), is not the assigned test. |
| REQ-TX-003 offer and accept checks, then reserve | VERIFIED (devnet DEC-005 number) | Offer and accept ladders, `TestAcceptReservesOnceAndBindsTerms`, the differential test, `TestRejectedLeaseMessagesLeaveTheFullState`, `TestLeaseFlowReplayAndQueries`. Devnet: council disable then `offer-disabled-provider height=429 ... code=2`, `accept-disabled-provider height=430 ... code=2`; capacity A2; signature A4. Off the devnet every offer is `DECISION_BLOCKED` (BLOCKED item 2). |
| REQ-TX-005 renew and end: authorized party, legal state, monotonic term version | VERIFIED | `TestRenewCheckOrder`, `TestEndCheckOrder`, `TestAuthorityEndCheckOrder`, `TestEndReleasesOnce`, `TestEndSignedForEarlierTermD12` ([unit-leases.txt](logs/goal-08/unit-leases.txt)). Devnet `stale-renewal ... code=5`, `end-again-new-business-id ... code=11`, `authority-end-lc height=58 code=0`. |
| REQ-INV-001 one provider cannot count two replicas of one content version | VERIFIED | As A5 (pair rows). Repair handoff to another provider is a separate replica: devnet `repair-offer height=432 code=0`, `repair-accept height=433 code=0`. |
| REQ-INV-002 reserved bytes cannot exceed admitted capacity | VERIFIED | As A2. Property tests: the differential `CAPACITY` classes and `FuzzLeaseTransitions` (invariant I2) at lease level; `TestReserveReleaseAccounting` and `TestAccountingFuzzSeedsReachEveryOutcome` on the providers keeper, with `FuzzReserveRelease` fuzzed 30 s. The one-byte boundary at lease level, on `ea29169`: `TestCapacityBoundaryAgainstSpecMachine` (real keepers, every step equal to `spec_machine.py`), `TestCapacityBoundaryInApp` (FinalizeBlock) and `TestCapacityBoundaryThroughAccept` (keeper, fake registry) ([m2-unit-leases.txt](logs/goal-08/m2-unit-leases.txt), [m2-unit-app.txt](logs/goal-08/m2-unit-app.txt)). Of the 22 tests in the leases mutant group on `ea29169`, four catch the capacity + 1 boundary: `TestCapacityBoundaryAgainstSpecMachine`, `TestReserveReleaseAccounting`, `TestAccountingFuzzSeedsReachEveryOutcome` and `TestCapacityBoundaryInApp` ([m2-mutants-leases.txt](logs/goal-08/m2-mutants-leases.txt)). On `acfe14e` two of 18 did, the two providers tests ([mutants-leases.txt](logs/goal-08/mutants-leases.txt)). `FuzzReserveRelease` was in neither run. |
| REQ-INV-003 replayed lease business id keeps a single transition | VERIFIED (lease half); governance half DEFERRED to Goal 10 | `TestLeaseRetryScenarios`: the same business id and payload under distinct tx hashes gives one `LeaseAccepted`, one `leases_result idempotent=true` and one reservation; another payload is `REPLAY`, as is a business id of another module ([docs/architecture/leases.md](../architecture/leases.md)). Devnet A10. |
| REQ-INV-004 counters do not underflow or overflow | VERIFIED, keeper level for overflow | Accounting oracle classes `OVERFLOW` (A6) and `UNDERFLOW` (A7); `TestReserveReleaseAccounting` ([unit-providers.txt](logs/goal-08/unit-providers.txt)); `FuzzReserveRelease` 30 s. |
| REQ-HOST-003 acceptance needs the provider signature; a region string is not a fact | VERIFIED | Accept ladder row `d1-ed25519-mismatch` and `TestAcceptCheckOrderDeviationD1`; `TestNoForbiddenNames` (no region field) ([unit-leases.txt](logs/goal-08/unit-leases.txt)). |
| REQ-HOST-007 real-time deadline; a halted chain does not turn an offline note into a renewal | VERIFIED, single machine | Devnet `TestDevnetLeaseHaltRestart`: `offline-renewal-signed lease=l1 deadline=1793634512 broadcast=false`; `halt stopped=4 height_before_stop_began=15 last_committed_height=15`; `restart-expiry last_pre_halt_height=15 header_time=1793634493 effective_minus_1=16 header_time=1793634493 held=true effective=17 header_time=1793634519 held=false released=4000000`; `offline-renewal-after-restart height=20 codespace=svn code=14 (ERROR_CODE_CONFLICT)`. The acceptance payload binds the real-time deadline (`TestAcceptReservesOnceAndBindsTerms`). Host display of clock uncertainty: Goal 16. |
| REQ-CAP-008 sponsor, term, budget and exit | BLOCKED, DEC-005 | Sponsor, term and budget are unset. The term is a devnet test number. The scheduler is kept only in the used-offer marker, which no query, typed event or export returns (DR-08-08, DR-08-06). |
| REQ-INV-006, lease half (from goal 07): withdrawal stops new sponsored leases | VERIFIED | As A9: withdrawal voids open offers, a new offer of the content is `WITHDRAWN`, an acceptance of a voided offer is `NOT_FOUND`. `TestWithdrawalAtCapGasInApp` voids 256 offers by message and by council. |

## Required handoff

The goal asks for invariants, storage indexes, event payloads, exhaustive negative cases, and the evidence needed before scheduler and broadcaster integration. They are documented in [chain/x/leases/README.md](../../chain/x/leases/README.md) and [docs/architecture/leases.md](../architecture/leases.md). This file links them and does not copy them.

| Item | Status | Where | Test |
| --- | --- | --- | --- |
| Invariants I1-I12 | IMPLEMENTED; checkers VERIFIED | README "Invariants" | `leasestest.CheckInvariants` checks I1-I7 (I7 only its single-state part) and I10, and the shape of business rows (its errors name that I12). `leasestest.CheckOfferIntegrity` checks I9. The difftest harness runs both, and a check that no lease emits `LeaseEnded` twice (I8), after every step of `FuzzLeaseTransitions`. `TestCheckInvariantsDetectsEachViolation` breaks I1-I7, I9, I10 and the I12 row shape once each. I8 is tested by `TestEndReleasesOnce` and `TestExpiryOrderAndBudget`, I11 by `TestDeterministicSources` and `TestExpiryUsesHeaderTimeOnly`, and I12 (one effect per business id) by the ladder `REPLAY` rows and the idempotent-retry tests the README lists ([unit-leases.txt](logs/goal-08/unit-leases.txt)) |
| Storage indexes (prefixes `0x00`-`0x0D`) | IMPLEMENTED | README "Store layout and indexes" | `TestStoreSnapshot`, `TestActiveLeasesPagination`, `TestDeadlinesPagination` |
| Event payloads | VERIFIED | README "Events"; `chain/app/testdata/lease-events.golden.json` | `TestLeaseEventSchemas` ([unit-app.txt](logs/goal-08/unit-app.txt)). Devnet `phase=events heights=1..434 block_txs=67 events=403 typed=130 typed_tx_parsed_with_msg_index=105 typed_block_parsed_with_mode=25 canaries=0 forbidden_names=0`. `FuzzBlockEventParse` fuzzed 30 s. |
| Negative cases | IMPLEMENTED; ladders VERIFIED | README "Messages: check order and negative cases" (one ladder row per check) and the reach list of `TestRejectedLeaseMessagesLeaveTheFullState` in leases.md | The five ladders run each row cumulatively and in isolation. "Exhaustive" means every row of those tables; `TestDifferentialCoverage` lists the 113 outcome classes the seeded differential run reached. |
| Evidence before scheduler (goal 11) and broadcaster integration | IMPLEMENTED; consumers are goals 11 and 16 | README "Handoff" and leases.md "Handoff" | Ack is not commit (A2 devnet line); resubmission rules (A10, `TestLeaseRetryScenarios`, `TestRetryAfterKeyRotation`); golden sign vectors (`TestSignBytesVectors`); simulated gas equals committed gas for `offer lease: simulated gas 126594, committed gas 126594`, `accept lease: ... 142249`, `renew lease: ... 103695`, `end lease: ... 107425` (`TestSimulateEstimatesGas`, [unit-app.txt](logs/goal-08/unit-app.txt)). |

## DR-06-06 seat-only admission

Owner ruling, 2026-11-02 (about 11:38 UTC), recorded in [authority-authorization.md](../architecture/authority-authorization.md). Asked how goal 08 should handle DR-06-06, the owner selected the option "Seat-only admission (Recommended)", whose description reads: "Admit seat accounts from the council seat list in authority's config, not from a submitter row, and refuse a seat address in the submitter list entirely (DR-07-11's stricter option). Seat keys can then only send council messages. Code change in authority/genesis/ante, no protocol change." The description is the option text the owner selected, not the owner's own words. The ruling supersedes DR-07-11's `BROADCASTER`-only rule, which the owner had kept earlier the same day.

What changed (from the workflow output and the code; no capture log describes code):

- The ante admits a signer through `authority.AdmissionOf`: a submitter by its `0x02` row, a seat account by the seat list in the authority config (`0x01`), and nothing else. A seat whose config is missing or does not decode is not admitted.
- If any signer is a seat, every message of the transaction must be `MsgProposeChange` or `MsgApproveChange`. Anything else is `ERROR_CODE_FORBIDDEN` in the ante, in CheckTx and in FinalizeBlock, before the sequence moves. A mixed transaction is refused whole.
- `genesis.Validate` and authority `ValidateGenesisState` refuse a seat address in the submitter list with any role, `BROADCASTER` included.
- `RotateCouncilSeat` writes no submitter or admission row; admission moves with the key. A new seat key whose address is a submitter is `DUPLICATE_ID`. `RotateServiceSubmitter` onto a seat address is `DUPLICATE_ID` (goal 07's follow-up).
- Genesis creates an auth account for each seat address (no fee, no coin). The devnet genesis drops the seat `BROADCASTER` rows.
- No protocol change ([protocol-unchanged.txt](logs/goal-08/protocol-unchanged.txt)).

Tests (all pass):

- `TestSeatKeysSignOnlyCouncilMessages`: four seats times thirteen messages, asserted by `rows != 52` (`chain/app/seat_admission_test.go`, code reading). Each row is refused in CheckTx and FinalizeBlock with `FORBIDDEN`, the sequence does not move, `FullSnapshot` is unchanged, and no event is emitted. `TestSeatRotationMovesAnteAdmission` ([unit-app.txt](logs/goal-08/unit-app.txt)).
- `TestSeatAddressIsNotASubmitter`: 10 subtests in [unit-app.txt](logs/goal-08/unit-app.txt) (document and InitChain for each of five roles, `BROADCASTER` included) and 48 in [unit-genesis.txt](logs/goal-08/unit-genesis.txt).
- `TestAdmissionOf`, `TestSeatRotationMovesSeatAdmission`, `TestEngineOnlyRotationRejects` and `TestValidateGenesisState` ([unit-authority.txt](logs/goal-08/unit-authority.txt)). `TestRegistryMessagesRejectOtherRoles`, `TestAnteRejectsNonAdmittedAccount` and `TestRejectedMessagesLeaveTheFullAuthorityState` ([unit-app.txt](logs/goal-08/unit-app.txt)).
- Devnet: `TestDevnetSeatsHaveNoSubmitterRow` passes ([devnet.txt](logs/goal-08/devnet.txt)), and on the real nodes a seat-signed acceptance is refused in CheckTx: `phase=seat-relay-refused seat=seat-1 checktx=svn/2` ([devnet-lease.txt](logs/goal-08/devnet-lease.txt)).
- Thirteen seat mutants are killed ([Mutation checks](#mutation-checks)).

Not covered: seat admission does not check that the council is ready, and no app test covers a chain with seats but no ready council. A seat that co-signs a multi-signer transaction restricts every message of it to council messages (deliberately conservative). Seat rotation is still engine-only; a future rotation message must also create the new key's auth account.

## Unit suites

Counts are `--- PASS` lines at column 0 (top level) and indented (subtests). Top-level counts include fuzz targets running their seed corpus; seed runs are not fuzz runs. No log has a `--- FAIL` or `panic:` line.

| Log | Top-level passed | Subtests passed | Failed | Skipped | Exit |
| --- | --- | --- | --- | --- | --- |
| [unit.txt](logs/goal-08/unit.txt), `./chain/...` with `SVN_SKIP_DEVNET=1` (filtered recapture) | 431 | 1458 | 0 | 6 | 0 |
| [unit-leases.txt](logs/goal-08/unit-leases.txt), `./x/leases/...` (with `difftest`, `leasestest`) | 75 | 482 | 0 | 0 | 0 |
| [unit-app.txt](logs/goal-08/unit-app.txt) (filtered recapture) | 72 | 49 | 0 | 0 | 0 |
| [unit-authority.txt](logs/goal-08/unit-authority.txt) | 78 | 128 | 0 | 0 | 0 |
| [unit-providers.txt](logs/goal-08/unit-providers.txt) | 68 | 426 | 0 | 0 | 0 |
| [unit-content.txt](logs/goal-08/unit-content.txt) | 47 | 166 | 0 | 0 | 0 |
| [unit-genesis.txt](logs/goal-08/unit-genesis.txt) | 8 | 77 | 0 | 0 | 0 |
| [unit-msgkit.txt](logs/goal-08/unit-msgkit.txt) | 55 | 81 | 0 | 0 | 0 |
| [unit-node.txt](logs/goal-08/unit-node.txt) | 8 | 0 | 0 | 0 | 0 |
| [unit-svnerr.txt](logs/goal-08/unit-svnerr.txt) | 2 | 0 | 0 | 0 | 0 |
| [devnet.txt](logs/goal-08/devnet.txt), `./chain/devnet/` | 23 | 49 | 0 | 0 | 0 |
| [devnet-lease.txt](logs/goal-08/devnet-lease.txt), `-run '^TestDevnetLease'` | 2 | 0 | 0 | 0 | 0 |

On `ea29169`:

| Log | Top-level passed | Subtests passed | Failed | Skipped | Exit |
| --- | --- | --- | --- | --- | --- |
| [m2-unit.txt](logs/goal-08/m2-unit.txt), same filtered command as `unit.txt` | 435 | 1461 | 0 | 6 | 0 |
| [m2-unit-leases.txt](logs/goal-08/m2-unit-leases.txt) | 77 | 482 | 0 | 0 | 0 |
| [m2-unit-app.txt](logs/goal-08/m2-unit-app.txt), same filtered command as `unit-app.txt` | 74 | 52 | 0 | 0 | 0 |
| [m2-unit-authority.txt](logs/goal-08/m2-unit-authority.txt) | 78 | 128 | 0 | 0 | 0 |
| [m2-unit-providers.txt](logs/goal-08/m2-unit-providers.txt) | 68 | 426 | 0 | 0 | 0 |
| [m2-devnet.txt](logs/goal-08/m2-devnet.txt), `./chain/devnet/` | 23 | 49 | 0 | 0 | 0 |

The `m2-` counts differ from the first commit's only by the four new tests: `TestCapacityBoundaryAgainstSpecMachine` and `TestCapacityBoundaryThroughAccept` in x/leases (no subtests), and `TestCapacityBoundaryInApp` and `TestDamagedExpiryLimitHalts` (three subtests) in `chain/app`. The other top-level test names are the same in each pair of logs (compared with `grep` and `diff` by this evidence writer). `m2-unit.txt` has 15 packages `ok` and 4 with no test files, as `unit.txt` has.

The six skips in `unit.txt` and `m2-unit.txt` are the devnet process tests that `SVN_SKIP_DEVNET=1` skips: `TestDevnetLeaseLifecycle`, `TestDevnetLeaseHaltRestart`, `TestDevnetValidatorLifecycle`, `TestDevnetQuorumFollowsVotingPower`, `TestFourValidatorsCommitRestartAndQuorum` and `TestDevnetRegistryDemo`. They ran in `devnet.txt` and `m2-devnet.txt`. Neither has a `leveldb: closed` or `exit status 2` line.

## Fuzz runs

All seven runs are on `acfe14e`; none was run again on `ea29169` (see [Second tested commit](#second-tested-commit-ea29169-capacity-boundary-and-max_expiry_steps-halt-tests), "What still stands"). Each run used `bash -c '...; env -C chain go test -run='^$' -fuzz='^<Target>$' -fuzztime=<N> <package>; ...'`, one at a time. The wrapper prints the load average before and after and lists new or changed files under `chain/` after the run. Every list is empty, so no run wrote a crasher. Every run reached `now fuzzing with 4 workers`. The load average reached 7.46 on 4 CPUs, so the CPU was shared at times.

| Target | Package | -fuzztime | Baseline | Last `execs:` line | Load before / after | Exit | Log |
| --- | --- | --- | --- | --- | --- | --- | --- |
| `FuzzLeaseDifferential` | `./x/leases/difftest/` | 60s | 60 | `elapsed: 1m1s, execs: 1202 (0/sec), new interesting: 20 (total: 80)` | 2.00 / 7.13 | 0 | [log](logs/goal-08/fuzz-FuzzLeaseDifferential.txt) |
| `FuzzLeaseTransitions` | `./x/leases/difftest/` | 60s | 136 | `elapsed: 1m1s, execs: 2800 (0/sec), new interesting: 48 (total: 184)` | 7.13 / 7.34 | 0 | [log](logs/goal-08/fuzz-FuzzLeaseTransitions.txt) |
| `FuzzLeasesGenesisJSON` | `./x/leases/` | 60s | 80 | `elapsed: 1m1s, execs: 86640 (0/sec), new interesting: 38 (total: 118)` | 7.34 / 6.41 | 0 | [log](logs/goal-08/fuzz-FuzzLeasesGenesisJSON.txt) |
| `FuzzDeadlinesPageKey` | `./x/leases/` | 60s | 62 | `elapsed: 1m0s, execs: 182039 (0/sec), new interesting: 6 (total: 68)` | 6.41 / 5.61 | 0 | [log](logs/goal-08/fuzz-FuzzDeadlinesPageKey.txt) |
| `FuzzStateTransitions` | `./x/authority/` | 30s | 532 | `elapsed: 34s, execs: 9757 (0/sec), new interesting: 7 (total: 539)` | 5.61 / 5.35 | 0 | [log](logs/goal-08/fuzz-FuzzStateTransitions.txt) |
| `FuzzReserveRelease` | `./x/providers/` | 30s | 50 | `elapsed: 31s, execs: 11223 (0/sec), new interesting: 11 (total: 61)` | 5.32 / 7.46 | 0 | [log](logs/goal-08/fuzz-FuzzReserveRelease.txt) |
| `FuzzBlockEventParse` | `./msgkit/` | 30s | 63 | `elapsed: 31s, execs: 186463 (0/sec), new interesting: 11 (total: 74)` | 7.46 / 6.41 | 0 | [log](logs/goal-08/fuzz-FuzzBlockEventParse.txt) |

- `FuzzLeaseDifferential` and `FuzzLeaseTransitions` are state-transition fuzzers over bounded op sequences; the first compares with the oracle at every step. The other five are a genesis parser, a page-key parser, the authority transition fuzzer (changed by DR-06-06), the reserve/release fuzzer and the block-event parser. All but `FuzzStateTransitions` are new in goal 08: none of the other six is defined on `main` `2fc93e5` (code reading).
- `FuzzStateTransitions` gathered its 532-entry baseline until `elapsed: 20s`, so it fuzzed for about 14 s of its 34 s.
- `TestFuzzReachesStates` shows that the lease fuzz seed corpus reaches the required classes (A1).
- [fuzz-list.txt](logs/goal-08/fuzz-list.txt) lists 17 fuzz targets. The ten not fuzzed on this tree are goal 06 and goal 07 targets. Their seed corpora ran in the unit suites. `FuzzProviderTransitions` and `FuzzContentTransitions` cover packages that goal 08 changed (`chain/x/providers/handoff.go`, `chain/x/content/handoff.go`); they were not fuzzed here (see [Follow-ups](#follow-ups)).

## Mutation checks

A mutant is a small change to committed code. A test failure means the mutant was killed. These mutants (27, then 28) are a spot check of DR-06-06, the review round and the lease accounting. They are not a mutation score for the goal.

**Runner.** `docs/evidence/goal-08/mutants.py` (standard library only; allowlisted in `PY_ALLOW` of the goal 01 checker). Two versions ran:

- Version 1, 27 mutants, on `acfe14e`: the logs without a prefix. It was untracked then and is not committed. A copy is kept in session scratch (`goal-08-cont/ev/mutants-v1.py`, sha256 `e905db1c25ae978d074a2107209101d3532067942923f02753df14bce479fabd`; read with `sha256sum`, not a capture log).
- Version 2, 28 mutants, committed in `ea29169`: the `m2-` logs (sha256 `018c77bd1463952467b00c2ff438861acf2c2aff929cdf1c0c1abb5bc25cf3c3`, read with `sha256sum` by this evidence writer). It adds the mutant `expiry-limit-damage-ignored`, adds the four new tests to the leases group's command (22 named tests instead of 18), and adds a `--worktree` option for dry runs. No logged run uses that option.

Both versions export `HEAD` with `git archive` into a temporary directory. Each group runs one `go test -count=1 -v -timeout 15m -run '^(...)$'` command. A control passes only when it exits 0 and every named test prints a top-level `--- PASS`. A mutant may carry several edits; each search string must occur exactly once when applied. KILLED needs a top-level `--- FAIL` or a `panic:` line; a non-zero exit without either is UNCLEAR, and a build failure is BUILD_FAILED. Neither counts as a kill. The full list of failing tests is printed.

| Run | Exit | Result | Log |
| --- | --- | --- | --- |
| `mutants.py --check` (version 1, `acfe14e`) | 0 | `mutants 27 of 27 (seat=13, review=5, leases=9)`, `search strings: every one occurs exactly once` | [mutants-check.txt](logs/goal-08/mutants-check.txt) |
| `mutants.py --group seat` (version 1, `acfe14e`) | 0 | `CONTROL seat PASS exit=0 secs=110.1 named_tests_passed=11/11`, `GROUP_SECS seat 209.4`, `RESULT mutants=13 killed=13` | [mutants-seat.txt](logs/goal-08/mutants-seat.txt) |
| `mutants.py --group review` (version 1, `acfe14e`) | 0 | `CONTROL review PASS exit=0 secs=113.5 named_tests_passed=11/11`, `GROUP_SECS review 178.2`, `RESULT mutants=5 killed=5` | [mutants-review.txt](logs/goal-08/mutants-review.txt) |
| `mutants.py --group leases` (version 1, `acfe14e`) | 0 | `CONTROL leases PASS exit=0 secs=146.9 named_tests_passed=18/18`, `GROUP_SECS leases 440.4`, `RESULT mutants=9 killed=9` | [mutants-leases.txt](logs/goal-08/mutants-leases.txt) |
| `mutants.py --check` (version 2, `ea29169`) | 0 | `mutants 28 of 28 (seat=13, review=5, leases=10)`, `search strings: every one occurs exactly once` | [m2-mutants-check.txt](logs/goal-08/m2-mutants-check.txt) |
| `mutants.py --group seat` (version 2, `ea29169`) | 0 | `CONTROL seat PASS exit=0 secs=107.0 named_tests_passed=11/11`, `GROUP_SECS seat 204.4`, `RESULT mutants=13 killed=13` | [m2-mutants-seat.txt](logs/goal-08/m2-mutants-seat.txt) |
| `mutants.py --group review` (version 2, `ea29169`) | 0 | `CONTROL review PASS exit=0 secs=113.3 named_tests_passed=11/11`, `GROUP_SECS review 177.2`, `RESULT mutants=5 killed=5` | [m2-mutants-review.txt](logs/goal-08/m2-mutants-review.txt) |
| `mutants.py --group leases` (version 2, `ea29169`) | 0 | `CONTROL leases PASS exit=0 secs=148.5 named_tests_passed=22/22`, `GROUP_SECS leases 496.3`, `RESULT mutants=10 killed=10` | [m2-mutants-leases.txt](logs/goal-08/m2-mutants-leases.txt) |

"Killed by" copies each `failing=` field of the `m2-` logs. In the seat and review groups the lists are the same in both runs. In the leases group, `ea29169` changed three lists and added one mutant; the `acfe14e` lists of those three follow the table.

| Group | Mutant | File | Killed by |
| --- | --- | --- | --- |
| seat | `ante-seat-admission-removed` | `chain/x/authority/store.go` | `TestSeatRotationMovesSeatAdmission`, `TestAdmissionOf`, `TestCommittedAuthorityFlowAndReplay`, `TestRejectedMessagesLeaveTheFullAuthorityState`, `TestAnteRejectsNonAdmittedAccount`, `TestRegistryMessagesRejectOtherRoles`, `TestSeatAddressIsNotASubmitter`, `TestSeatKeysSignOnlyCouncilMessages`, `TestSeatRotationMovesAnteAdmission` |
| seat | `ante-seat-gate-removed` | `chain/app/ante.go` | `TestRejectedMessagesLeaveTheFullAuthorityState`, `TestRegistryMessagesRejectOtherRoles`, `TestSeatKeysSignOnlyCouncilMessages`, `TestSeatRotationMovesAnteAdmission` |
| seat | `auth-genesis-allows-seat-row` | `chain/x/authority/genesis.go` | `TestValidateGenesisState`, `TestSeatAddressIsNotASubmitter` |
| seat | `doc-genesis-allows-seat-row` | `chain/genesis/document.go` | `TestSeatAddressIsNotASubmitter` |
| seat | `doc-and-auth-allow-broadcaster-seat-row` | both genesis files | `TestValidateGenesisState`, `TestSeatAddressIsNotASubmitter` |
| seat | `seat-may-sign-lease-messages` | `chain/app/ante.go` | `TestSeatKeysSignOnlyCouncilMessages`, `TestSeatRotationMovesAnteAdmission` |
| seat | `seat-gate-checks-first-message-only` | `chain/app/ante.go` | `TestSeatKeysSignOnlyCouncilMessages` |
| seat | `rotation-keeps-old-key-admitted` | `chain/x/authority/engine.go` | `TestSeatRotationMovesSeatAdmission`, `TestSeatRotationMovesAnteAdmission` |
| seat | `rotation-onto-submitter-allowed` | `chain/x/authority/engine.go` | `TestSeatRotationMovesSeatAdmission` |
| seat | `service-rotation-onto-seat-allowed` | `chain/x/authority/engine.go` | `TestEngineOnlyRotationRejects` |
| seat | `auth-genesis-omits-seat-accounts` | `chain/app/state.go` | `TestCommittedAuthorityFlowAndReplay`, `TestRejectedMessagesLeaveTheFullAuthorityState`, `TestAnteRejectsNonAdmittedAccount`, `TestRegistryMessagesRejectOtherRoles`, `TestSeatAddressIsNotASubmitter`, `TestSeatKeysSignOnlyCouncilMessages`, `TestSeatRotationMovesAnteAdmission` |
| seat | `carried-ante-extra-admission` (goal 06 `ante-extra-admission`, ported) | `chain/app/app.go` | `TestRejectedMessagesLeaveTheFullAuthorityState`, `TestRegistryMessagesRejectOtherRoles` |
| seat | `carried-doc-seat-match-case-sensitive` (goal 07 `doc-seat-rule-case-sensitive`, ported) | `chain/genesis/document.go` | `TestSeatAddressIsNotASubmitter` |
| review | `m1-genesis-content-cap-off` | `chain/x/leases/genesis.go` | `TestGenesisValidateImportExport` |
| review | `m2-offer-d6-off` | `chain/x/leases/msg_server.go` | `TestOfferCheckOrder`, `TestOpenOffersPerContentCapD6`, `TestOpenOffersPerSchedulerCapD10`, `TestDeviationRulesAreLoadBearing`, `TestWithdrawalAtContentCapGas`, `TestWithdrawalAtCapGasInApp` |
| review | `m4-offer-d10-off` (search string reconstructed from the review notes) | `chain/x/leases/msg_server.go` | `TestOfferCheckOrder`, `TestOpenOffersPerSchedulerCapD10`, `TestDeviationRulesAreLoadBearing`, `TestWithdrawalAtCapGasInApp` |
| review | `m5-end-payload-drops-term-version` | `chain/x/leases/msg_server.go` | `TestEndCheckOrder`, `TestEndSignedForEarlierTermD12`, `TestDeviationRulesAreLoadBearing`, `TestRejectedLeaseMessagesLeaveTheFullState` |
| review | `m6-accept-replay-owner-check-off` | `chain/x/leases/msg_server.go` | `TestAcceptReplayByAnotherProviderD13`, `TestDeviationRulesAreLoadBearing`, `TestRejectedLeaseMessagesLeaveTheFullState` |
| leases | `capacity-off-by-one` | `chain/x/providers/handoff.go` | `TestCapacityBoundaryAgainstSpecMachine`, `TestReserveReleaseAccounting`, `TestAccountingFuzzSeedsReachEveryOutcome`, `TestCapacityBoundaryInApp` |
| leases | `end-releases-twice` | `chain/x/leases/msg_server.go` | `TestDuplicateReplicaRejected`, `TestCapacityBoundaryThroughAccept`, `TestEndReleasesOnce`, `TestCapacityBoundaryAgainstSpecMachine`, `TestDifferentialAgainstSpecMachine`, `TestLargeExpiringCatalogue`, `TestLeaseRetryScenarios`, `TestRejectedLeaseMessagesLeaveTheFullState`, `TestCapacityBoundaryInApp` |
| leases | `end-held-guard-removed` | `chain/x/leases/msg_server.go` | `TestEndCheckOrder`, `TestAuthorityEndCheckOrder`, `TestEndReleasesOnce`, `TestDifferentialAgainstSpecMachine`, `TestLeaseRetryScenarios`, `TestRejectedLeaseMessagesLeaveTheFullState` |
| leases | `expiry-releases-twice` | `chain/x/leases/expiry.go` | `TestExpiryOrderAndBudget`, `TestExpiryWorkIsBounded`, `TestDifferentialAgainstSpecMachine`, `TestExpiryWorkIndependentOfBacklog`, `TestLargeExpiringCatalogue`, `TestRestartContinuesExpiry` |
| leases | `accept-duplicate-copy-allowed` | `chain/x/leases/msg_server.go` | `TestAcceptCheckOrder`, `TestDuplicateReplicaRejected`, `TestDifferentialAgainstSpecMachine`, `TestRejectedLeaseMessagesLeaveTheFullState` |
| leases | `offer-duplicate-copy-allowed` | `chain/x/leases/msg_server.go` | `TestDuplicateReplicaRejected`, `TestOfferCheckOrder`, `TestDifferentialAgainstSpecMachine`, `TestRejectedLeaseMessagesLeaveTheFullState` |
| leases | `service-key-signs-acceptance` | `chain/x/leases/msg_server.go` | `TestServiceKeyCannotSignAcceptance`, `TestDifferentialAgainstSpecMachine`, `TestServiceKeyCannotSignAcceptanceInApp`, `TestRejectedLeaseMessagesLeaveTheFullState` |
| leases | `expiry-budget-ignored` | `chain/x/leases/expiry.go` | `TestExpiryOrderAndBudget`, `TestExpiryWorkIsBounded`, `TestDifferentialAgainstSpecMachine`, `TestExpiryWorkIndependentOfBacklog`, `TestLargeExpiringCatalogue`, `TestDamagedExpiryLimitHalts`, `TestRestartContinuesExpiry` |
| leases | `expiry-limit-damage-ignored` (version 2 only) | `chain/x/leases/expiry.go` | `TestDamagedExpiryLimitHalts` |
| leases | `expiry-merge-ignores-budget` | `chain/x/leases/expiry.go` | `TestExpiryOrderAndBudget`, `TestDifferentialAgainstSpecMachine`, `TestExpiryWorkIndependentOfBacklog`, `TestLargeExpiringCatalogue`, `TestRestartContinuesExpiry` |

On `acfe14e` ([mutants-leases.txt](logs/goal-08/mutants-leases.txt)), before the four new tests existed: `capacity-off-by-one` was killed by `TestReserveReleaseAccounting` and `TestAccountingFuzzSeedsReachEveryOutcome` only; `end-releases-twice` by `TestDuplicateReplicaRejected`, `TestEndReleasesOnce`, `TestDifferentialAgainstSpecMachine`, `TestLargeExpiringCatalogue`, `TestLeaseRetryScenarios` and `TestRejectedLeaseMessagesLeaveTheFullState`; `expiry-budget-ignored` by the list above without `TestDamagedExpiryLimitHalts`. The other six leases lists are the same in both runs.

Notes:

- The seat mutants re-run, on the committed tree, the eleven mutants of the DR-06-06 scratch runner, plus the two ported ones. The review mutants M1, M2, M5 and M6 use the mutated lines of the review's scratch logs. M3 (the export catalogue lease check) is not in the runner.
- `capacity-off-by-one` lets a reservation end one byte above capacity. On `acfe14e` only the two x/providers tests killed it, and the other 16 tests of the group (x/leases, difftest and app) passed with it. The x/leases package tests use a fake provider registry. `TestDifferentialAgainstSpecMachine` uses the real providers keeper and passed with the mutant, so its generated sequences never hit the exact boundary; the `ea29169` commit message says every size and capacity gap the seeded generator produces is a multiple of 100 bytes (not a capture log). On `ea29169`, `TestCapacityBoundaryAgainstSpecMachine` (real keepers, against `spec_machine.py`) and `TestCapacityBoundaryInApp` (FinalizeBlock) also kill it. `TestCapacityBoundaryThroughAccept` does not, as expected: it runs on the fake registry and pins what x/leases asks of it. The runner applies each mutant to the named tests of its group only, not to whole packages.
- `expiry-limit-damage-ignored` makes x/leases BeginBlock treat a damaged governed `max_expiry_steps` as the protocol ceiling instead of failing closed (its `reason:` line). Only `TestDamagedExpiryLimitHalts` kills it. On `ea29169` that test also kills `expiry-budget-ignored`.

**Earlier runners on `acfe14e`.** Neither was run on `ea29169`.

- Goal 06 ([goal06-anchors.txt](logs/goal-08/goal06-anchors.txt), exit 1): `mutants 119 of 119`, `RESULT search-string problems=9; no test was run`. Five anchors (`swap-engine-492`, `-548`, `-575`, `-578`, `-591`) target code goal 07 deleted; they were already missing on goal 07. Four target code DR-06-06 changed: `seat-rotation-keeps-admission`, `seat-rotation-no-submitter-dup` and `swap-engine-855` (`RotateCouncilSeat` no longer writes rows), and `ante-extra-admission` (`chain/app/app.go`, the ante admission callback). The goal 08 runner covers these with `rotation-keeps-old-key-admitted`, `rotation-onto-submitter-allowed` and `carried-ante-extra-admission`, all killed.
- Goal 07 ([goal07-anchors.txt](logs/goal-08/goal07-anchors.txt), exit 1): `mutants 30 of 30 (goal07=5, merge=14, node=11)`, five `COPY_MATCH` lines, then `NOT_FOUND auth-seat-rule-allows-auditor` and `NOT_FOUND doc-seat-rule-case-sensitive`, `RESULT validation problems=2; no test was run`. DR-06-06 removed the `BROADCASTER`-only text both target. The goal 08 runner covers them with `auth-genesis-allows-seat-row`, `doc-and-auth-allow-broadcaster-seat-row` and `carried-doc-seat-match-case-sensitive`, all killed.
- So the other 110 goal 06 mutants and 28 goal 07 mutants did not run on either tested commit. That is BLOCKED (see [Not run or BLOCKED](#not-run-or-blocked)).

## Gates

| Gate | Status | Log |
| --- | --- | --- |
| `go test -race -count=1 -timeout 20m ./chain/...` | VERIFIED on both commits | [race.txt](logs/goal-08/race.txt), exit 0: 15 packages `ok` (every `chain` package with tests), including `chain/app 317.694s`, `chain/devnet 510.755s` and `chain/x/leases/difftest 420.692s`. No `DATA RACE` line. [m2-race.txt](logs/goal-08/m2-race.txt) on `ea29169`, exit 0: 15 packages `ok`, including `chain/app 325.515s`, `chain/devnet 500.531s` and `chain/x/leases/difftest 407.323s`. No `DATA RACE` line. |
| `go test -race` for `./proto/...` and `./agents/provider/...` | VERIFIED | [race-proto.txt](logs/goal-08/race-proto.txt), [race-agents-provider.txt](logs/goal-08/race-agents-provider.txt) |
| `python scripts/run_tests.py --only go` | VERIFIED (fresh run) | [tests-go.txt](logs/goal-08/tests-go.txt) exits 0 but every package is `(cached)`, so it is not cited as a run. [tests-go-count1.txt](logs/goal-08/tests-go-count1.txt), with `GOFLAGS=-count=1`: exit 0, every package `ok`, `chain/devnet 305.770s`, `chain/x/leases/difftest 65.494s`, no `(cached)` line. |
| `bash scripts/gofmt_check.sh`, `gofmt -l`, `go vet` | VERIFIED on both commits | [gofmt-vet.txt](logs/goal-08/gofmt-vet.txt): `gofmt ok files 243`, `gofmt -l chain proto agents: <empty>`, `gofmt_check=0 gofmt_l=0 vet_chain=0 vet_proto=0 vet_agents_provider=0`. [m2-gofmt-vet.txt](logs/goal-08/m2-gofmt-vet.txt): `gofmt ok files 244`, the same `<empty>` list and five zeros. |
| `go mod tidy -diff`; modules against `main` | VERIFIED | See [Prerequisites](#prerequisites) |
| Proto regeneration diff | VERIFIED | [proto-regen.txt](logs/goal-08/proto-regen.txt): `libprotoc 36.2`, `generated 28 proto files`, `proto regeneration diff empty` |
| Protocol, goal pages, CI and `proto/` unchanged since `main` | VERIFIED | [protocol-unchanged.txt](logs/goal-08/protocol-unchanged.txt), exit 0 |
| ruff format, ruff check, mypy (CI paths) | VERIFIED | [ruff-format.txt](logs/goal-08/ruff-format.txt) `34 files already formatted`, [ruff-check.txt](logs/goal-08/ruff-check.txt) `All checks passed!`, [mypy.txt](logs/goal-08/mypy.txt) `Success: no issues found in 24 source files`. These paths do not include `docs/evidence/`, so the new runner is not linted or type-checked by this gate. The ruff paths include `tests/difftest/spec_oracle.py`, but the mypy `files` list in `pyproject.toml` does not include `tests/difftest`, so the mypy gate does not type-check the oracle (next row). |
| ruff check and `mypy --strict` on the runner `docs/evidence/goal-08/mutants.py` (version 2) | Not a repository gate; mypy passes, ruff reports line length | [m2-mypy-mutants.txt](logs/goal-08/m2-mypy-mutants.txt), exit 0: `Success: no issues found in 1 source file`. [m2-ruff-mutants.txt](logs/goal-08/m2-ruff-mutants.txt), exit 1: `Found 41 errors.`, all 41 `E501` (line too long, for example `E501 Line too long (102 > 100)`). |
| `mypy --strict` on `tests/difftest/spec_oracle.py` | Not a repository gate; fails | [spec-oracle-mypy.txt](logs/goal-08/spec-oracle-mypy.txt), exit 1: `Found 3 errors in 1 file`: one `import-not-found` for `spec_machine` (the oracle loads it from `docs/evidence/goal-02/`) and two `no-any-return` (lines 604 and 637). |
| Spec oracle: ruff and self-test | VERIFIED | [spec-oracle-ruff.txt](logs/goal-08/spec-oracle-ruff.txt) `ruff_check=0 ruff_format=0`; [spec-oracle-selftest.txt](logs/goal-08/spec-oracle-selftest.txt) `PASS (82 steps)` |
| Python tests | VERIFIED in a clean clone; the worktree run fails on its layout | [tests-python.txt](logs/goal-08/tests-python.txt), exit 1: `1 failed, 119 passed`. The failure is `test_ignore_rules_cover_private_material`: `fatal: pathspec 'node_modules/react/index.js' is beyond a symbolic link`. In this worktree `.venv`, `node_modules` and `.toolchains` are symlinks to the main checkout. [tests-python-clean-clone.txt](logs/goal-08/tests-python-clean-clone.txt): a scratch clone of `acfe14e`, tree `6e81d0c9`, clean status, `git check-ignore` exit 0 for the same paths, `120 passed in 63.17s`, exit 0. |
| `npm run typecheck`, Node tests | VERIFIED | [npm-typecheck.txt](logs/goal-08/npm-typecheck.txt) exit 0 (`tsc --noEmit`); [tests-node.txt](logs/goal-08/tests-node.txt) `# pass 1`, `# fail 0` |
| Contract checkers (on `acfe14e` before this file was written; on `ea29169` with the progress rows set aside) | VERIFIED | [check-requirements.txt](logs/goal-08/check-requirements.txt): `requirements 265`, `self_test_problems 0`, `errors 0`, `result PASS`, with the new runner and its `PY_ALLOW` entry in place. [check-protocol.txt](logs/goal-08/check-protocol.txt): `transition_cases 97`, `valid_vectors 15`, `invalid_vectors 14`, `errors 0`, `languages MATCH`, `result PASS`. On `ea29169`, with the progress rows set aside: [m2-check-requirements.txt](logs/goal-08/m2-check-requirements.txt) `requirements 265`, `self_test_problems 0`, `errors 0`, `result PASS`; [m2-check-protocol.txt](logs/goal-08/m2-check-protocol.txt) `transition_cases 97`, `valid_vectors 15`, `invalid_vectors 14`, `errors 0`, `languages MATCH`, `result PASS`. |
| Workflow policy, secret scan, SBOM | VERIFIED for tracked files at `acfe14e`; secret scan also at `ea29169` | [workflow-policy.txt](logs/goal-08/workflow-policy.txt) `workflow policy ok files 1`, [scan-secrets.txt](logs/goal-08/scan-secrets.txt) `secret scan ok`, [sbom-check.txt](logs/goal-08/sbom-check.txt) `sbom ok components 1320`. `scripts/scan_secrets.py` reads only `git ls-files` output. [m2-scan-secrets.txt](logs/goal-08/m2-scan-secrets.txt) `secret scan ok` covers the tracked files of `ea29169`, which include the committed runner. Every log, this file and the progress rows were untracked when the scans ran, so no logged scan covers them. |
| Mutation runner, goal 08 | VERIFIED on both commits | 27 of 27 killed on `acfe14e` (version 1); 28 of 28 killed on `ea29169` (version 2) ([Mutation checks](#mutation-checks)) |
| Goal 06 and goal 07 mutation runners | BLOCKED | Anchors moved; no test ran on `acfe14e`; not run on `ea29169` ([Mutation checks](#mutation-checks)) |
| golangci-lint | BLOCKED | [golangci-lint.txt](logs/goal-08/golangci-lint.txt), exit 3: `golangci-lint has version 2.5.0 built with go1.25.1`, `the Go language version (go1.25) used to build golangci-lint is lower than the targeted Go version (1.26.10)`. Not a repository gate. |
| CI `devnet` job, `bash scripts/devnet/prove-containers.sh` | BLOCKED (environment), unchanged script | [m2-containers-unchanged.txt](logs/goal-08/m2-containers-unchanged.txt), exit 1, on `ea29169`. `go build` inside `docker build` fails at `Dockerfile:9` (`RUN go build -o /out/svnd ./cmd/svnd`) with `tls: failed to verify certificate: x509: certificate signed by unknown authority` against proxy.golang.org, because the build container does not trust the session's egress-proxy CA. No node started. This is an environment limit, not a code failure. The script prints its own `DIRTY yes` because it counts untracked evidence files (`git status --porcelain`); `capture.sh` prints `DIRTY no`. On `acfe14e` no daemon was running ([docker-info.txt](logs/goal-08/docker-info.txt) and [docker-version.txt](logs/goal-08/docker-version.txt), exit 1: `Cannot connect to the Docker daemon at unix:///var/run/docker.sock`). |
| Container devnet proof, CI script with only the image-build step changed | VERIFIED (labelled deviation), empty blocks only, single machine | [m2-containers-labelled-copy.txt](logs/goal-08/m2-containers-labelled-copy.txt), exit 0, on `ea29169`, after [m2-disk.txt](logs/goal-08/m2-disk.txt) showed 3.9G available. Four validator containers on the host network agree on the app hash at every sample. Start: all four at `height=4`. Full stop and start: `phase=restart up=5 reached=7`. One node stopped: `phase=stop-one threshold=7`, `phase=one-down threshold=7 reached=8`, and the other three sample heights 8, 9, 11 and 12. Two nodes stopped: `phase=stop-two threshold=14`, six samples of the remaining two at height 14, `phase=two-down stall=14`. Restart: `phase=recovery stall=14 up=16 reached=19`, `phase=recovery threshold=19 app_hash=8110F0D0CF8882818961B1BA705A6532E4F203E9F0ADB9B6E471AE093B79B31A`. The script states `NOTE empty blocks only. No application transaction is submitted, so the hash is the genesis application state.` and `NOTE bridge networking is BLOCKED. This run uses host networking.` So no lease message is exercised in a container (x/leases only runs its BeginBlock on empty blocks). The build differs from CI's: goal 07's [Dockerfile.proxy-ca](goal-07/containers/Dockerfile.proxy-ca) adds the proxy CA as a BuildKit secret in the discarded build stage (`#12 [build 6/6] RUN --mount=type=secret,id=proxyca,...`, `#12 DONE 124.2s`). [prove-containers-proxy-ca.sh](goal-07/containers/prove-containers-proxy-ca.sh) differs from the CI script only in that build command, its COMMAND line and the repo-root path, and neither script nor `deployments/chain` changed since `main` `2fc93e5` (`diff` and `git diff --stat` by this evidence writer; code reading). |
| Head CI | VERIFIED on `4db7e40` (code tree of `ea29169` plus the CI shard split) | PR 11, run 41521039450: all eight checks `success`. The first run, 41509776341 on `69f3980`, had `go` cancelled at the 20-minute job cap with no failing test ([Head CI](#head-ci); GitHub API, not a capture log). |

## Deviations

Chain behaviour that differs from `docs/evidence/goal-02/spec_machine.py`. The full table, with the reasons, is in [chain/x/leases/README.md](../../chain/x/leases/README.md), "Deviations from the spec machine". Each keeper test passes in [unit-leases.txt](logs/goal-08/unit-leases.txt). The differential oracle applies D-2 to D-6 and D-10 to D-13 as adapter rules, and `TestDeviationRulesAreLoadBearing` shows each of those is the step where the spec alone says ok.

| Id | Chain behaviour | Test |
| --- | --- | --- |
| D-1 | `AcceptLease` ed25519 verification runs after the business gate and the offer lookup | `TestAcceptCheckOrderDeviationD1` |
| D-2 | Renewal at or after the deadline is `EXPIRED`, even before expiry sweeps it | `TestRenewPastDeadlineIsExpiredD2` |
| D-3 | Acceptance of an offer whose deadline has passed is `EXPIRED` | `TestAcceptPastOfferDeadlineIsExpiredD3` |
| D-4 | An offer id is never reused (`DUPLICATE_ID`) | `TestOfferIDNeverReusedD4` |
| D-5 | Offer expiry more than 86400 s after committed time is `BOUNDS` (PROPOSED) | `TestOfferLifetimeBoundD5` |
| D-6 | At most 256 open offers per content (PROPOSED) | `TestOpenOffersPerContentCapD6` |
| D-7 | One expiry budget for leases and offers; an invariant violation fails the block | `TestExpiryOrderAndBudget`, `TestExpiryInvariantErrors` |
| D-8 | `msgkit.Enforce` runs first; an empty string is `MALFORMED` | ladder `enforce-*` and presence rows |
| D-9 | Untyped `leases_offer_closed{offer_id, reason}` on offer expiry and withdrawal void | `TestExpiryOrderAndBudget`, `TestWithdrawalVoidsOffers` |
| D-10 | At most 128 open offers of one content from one scheduler (PROPOSED) | `TestOpenOffersPerSchedulerCapD10` |
| D-11 | An identical `OfferLease` replay by another scheduler is `FORBIDDEN` | `TestOfferReplayByAnotherSchedulerD11` |
| D-12 | The END signature binds the lease's current term version | `TestEndSignedForEarlierTermD12` |
| D-13 | An identical `AcceptLease` replay by another provider's account is `FORBIDDEN` | `TestAcceptReplayByAnotherProviderD13` |

Other recorded differences:

- DR-06-06: a council seat key can sign only propose and approve. The relay column of `protocol.md` would let a broadcaster relay lease messages; a seat is not a broadcaster.
- The leases store is consensus version 2; `app.New` refuses a leases store without the layout key (`TestAppRefusesScaffoldLayout`). Goal 07 homes must be initialized again.
- `TestEveryProtocolMessageDecodes` now expects handler codes for the four lease messages, not router `FORBIDDEN`, because they have handlers; it logs `19 transactions, 10 nested message types` ([unit-app.txt](logs/goal-08/unit-app.txt)).
- `TestDevnetKeysAreDistinct` counts one more account, the devnet `broadcaster`.

## Decision requests

Interim choices are reversible. They are not approvals. Every interim below is implemented and tested, and none blocks a goal 08 acceptance check on its own. Full text: [chain/x/leases/README.md](../../chain/x/leases/README.md), "Decision requests". The README states each request's security consequence. It labels none of them security-critical and sets no answer-by condition. The "Security-critical" column and the "answer before" conditions in the "Blocks" column are this evidence file's classification and suggestions, not the README's and not an owner ruling.

| Id | Question | Security-critical (this file's classification) | Interim (tested) | Blocks |
| --- | --- | --- | --- | --- |
| DR-08-01 | Renewal after drain or council disable (successor of DR-07-05) | Yes: a disabled, possibly compromised provider can keep extending its own leases | v0 rule: allowed; remedy `authority_end`, one lease per message. Devnet `renew-on-disabled-provider height=431 code=0`. | No |
| DR-08-02 | `MaxOpenOffersPerContent` = 256 (D-6) and `MaxOpenOffersPerContentPerScheduler` = 128 (D-10), PROPOSED | Yes: two admitted services can keep a content's slots full; one can block all offers through DR-08-13 | The two constants | No; answer before admitting services whose operators are not trusted |
| DR-08-03 | `MaxOfferLifetimeSeconds` = 86400 (D-5), PROPOSED | No: it bounds one offer, not how long a service keeps a cap full | The constant | No |
| DR-08-04 | Provider ACCEPT, RENEW and END payloads | Yes: no chain id in the signed payload (joins DR-07-01); END binds the term version (D-12) | Interim payloads with golden vectors (`TestSignBytesVectors`) | No; answer before the goal 16 provider agent signs real leases |
| DR-08-05 | Real-time deadline guards D-2 and D-3 | No: stricter than v0, fails closed | D-2, D-3 | No |
| DR-08-06 | Offer ids never reused (D-4); export drops closed-offer markers and sponsors | Not while restart from export is unsupported; with DR-07-03 it affects replay protection | Recorded restriction (`TestLeasesGenesisRoundTripFromCommittedState`, `TestGenesisValidateImportExport`) | No |
| DR-08-07 | Expiry: BeginBlock placement, (time, kind, id) order, one budget shared with evidence, halt on an invariant violation | Yes: offers that expire first delay lease releases; an invariant violation halts the chain | Spec order; fail closed (`TestExpiryInvariantViolationHalts`) | No; answer before goal 09 shares the budget |
| DR-08-08 | Protocol additions: typed offer-closed event, by-id queries, structured cursor, unfiltered `ActiveLeases`, sponsor attribution | No | Untyped `leases_offer_closed`; sponsor only in the used-offer marker | No (REQ-CAP-008 is BLOCKED on DEC-005 regardless) |
| DR-08-09 | Renewal window: renew any time while held, stacking terms | No | Allowed (`TestRenewKeepsCommittedBytes`) | No; the length is DEC-005 |
| DR-08-10 | One integer encoding rule for business payloads across modules | No | Leases use raw JSON integers; goal 07 providers use decimal strings (`TestGeneratorRestrictionsHideKnownDivergences`) | No |
| DR-08-11 | `OfferLease` replay by another scheduler | Yes: otherwise a service that copies a pending offer becomes its scheduler | `FORBIDDEN` (D-11) | No |
| DR-08-12 | Renew and end check the provider signature before the business gate | No: state is unaffected; a client that trusts a resubmission's code misreads it | Spec order (`TestRetryAfterKeyRotation`) | No |
| DR-08-13 | The per-provider open-offer cap (1024) is shared by all schedulers | Yes: one admitted service can keep every provider's slots full and block all other offers, a repair handoff included | v0 rule (`TestOneSchedulerRefillsProviderSlots`) | No; answer before more than one service is admitted or goal 17 relies on repair handoff |
| DR-08-14 | `AcceptLease` replay by another provider's account | No: no state effect | `FORBIDDEN` (D-13); a `BROADCASTER` replay stays idempotent | No |

Related requests:

- DR-06-06: RESOLVED by the owner on 2026-11-02 and implemented here ([DR-06-06 seat-only admission](#dr-06-06-seat-only-admission)).
- DR-07-11: superseded by DR-06-06 (both genesis validators refuse a seat address with any role).
- DR-07-05: superseded by DR-08-01.
- Still open from earlier goals and touching leases: DR-07-01 (no chain binding in inner signatures), DR-07-02 and DR-07-03 (genesis catalogue and business ids in export), DR-07-04 (content versions; REQ-SEP-001), DR-06-01 (authority export). `TestLaunchGenesisRejectsNonEmptyCatalog` shows InitChain refuses an exported lease or offer catalogue; app-level re-import of committed lease state is not supported. It is not a goal 08 acceptance check.

## Residual risks

- One admitted service can keep a provider's 1024 open-offer slots full indefinitely by re-offering, and by filling every admitted provider it blocks every other scheduler's offers of any content id, a repair handoff included (DR-08-13). Two admitted services can fill a content's 256 slots (DR-08-02). The offer lifetime (D-5) does not limit this.
- Offers share the per-block expiry budget with held leases and are swept first when they expire earlier. A lease release due after them can be late by up to ceil(open offers due / budget) blocks, 8 per admitted provider at the defaults, as documented in DR-08-07 (computed from the constants, not measured).
- An expiry invariant violation halts the chain (fail closed, DR-08-07).
- A damaged governed limit `max_expiry_steps` (authority key `0x13`) makes every x/leases BeginBlock panic, so FinalizeBlock panics on every node and nothing commits (fail closed). `TestDamagedExpiryLimitHalts` shows this at FinalizeBlock level on two in-process nodes: a stored value of 3 bytes, the value 0 or the ceiling + 1 panics twice at the same height, and the commit, `FullSnapshot` and the sender's sequence do not move ([m2-unit-app.txt](logs/goal-08/m2-unit-app.txt); mutant `expiry-limit-damage-ignored` killed). Not tested: that CometBFT stops the node on that panic, and what a node does when it restarts on a committed damaged value. The test damages the uncommitted working state through a test-only writer; on a real chain only `ExecuteChange` and the message handlers write that key ([chain/x/authority/README.md](../../chain/x/authority/README.md)).
- A disabled provider can keep renewing its leases (DR-08-01).
- Seat admission does not check council readiness. A seat on a chain whose council is not ready reaches the council handler and gets `DECISION_BLOCKED`, and its sequence advances. No app test covers that chain.
- A seat that co-signs a multi-signer transaction restricts every message in it to council messages. This is deliberately conservative and may surprise a client.
- Provider inner signatures carry no chain id (DR-08-04, DR-07-01).
- A renewal or end retried after a provider key rotation, and an end retried after a renewal, is `UNAUTHORIZED` even when the first one committed (DR-08-12, D-12). A client must read the committed result.
- The sponsor of an accepted lease is only in the used-offer marker and the `OfferLease` signer; no query or typed event returns it, and an export drops it (DR-08-08, DR-08-06).
- Lease history grows without limit. No per-account lease limit exists while DEC-005 allowances are unset. Gas meters every write.
- Raw `/store/leases/*` ABCI queries are not metered or paged. The CometBFT RPC port must not be public.
- The capacity + 1 boundary is pinned at lease level by three fixed-size tests (capacity 2001 and 2*4,000,000+1; A2). The seeded differential generator still does not reach that boundary.
- The container proof here is goal 07's labelled copy, with a changed image build, empty blocks, host networking and one machine. Its app hash is the genesis state, so no lease message is exercised in a container (x/leases only runs its BeginBlock on empty blocks). The unchanged CI `devnet` job has not run on goal 08.
- The differential generator does not reach the D-6, D-10, D-11, D-12 or D-13 classes. Only the rule fixtures, the keeper tests and the D-6 scale test cover those rules (A1).
- `tests/difftest/spec_oracle.py` is outside the mypy gate, and `mypy --strict` reports 3 errors in it ([spec-oracle-mypy.txt](logs/goal-08/spec-oracle-mypy.txt)).
- The node shutdown fix inherited from goal 07 (`chain/node/db.go`, `closeSafeDB`) works around a CometBFT v0.40.0 race: after a database's `Close`, reads report not found. The upstream race remains; revisit it when CometBFT is upgraded. No `leveldb: closed` line appears in [devnet.txt](logs/goal-08/devnet.txt) or [m2-devnet.txt](logs/goal-08/m2-devnet.txt).
- Test hygiene: `test_cli_flags_git_added_mnemonic_files` in `tests/unit/test_secrets.py` runs `git add` of `w.py` and `seed.md` in the real repository index and resets them afterwards. A commit made while that test runs could pick them up. During the goal 08 gate, a `-race` run that overlapped it recorded a transient tree (workflow output, not a capture log; that log is not cited here).
- Opaque ids and digests remain linkable by anyone who holds the preimage. Events are not part of the app hash.
- The devnet is four processes on one machine. It is not an independent-operator result.

## Not run or BLOCKED

- CI `devnet` job, unchanged: BLOCKED (environment) in this container; it passed in head CI ([Head CI](#head-ci)). `scripts/evidence/capture.sh docs/evidence/logs/goal-08/m2-containers-unchanged.txt -- bash scripts/devnet/prove-containers.sh` exits 1 at the image build (proxy CA). Goal 07's labelled copy passed (`scripts/evidence/capture.sh docs/evidence/logs/goal-08/m2-containers-labelled-copy.txt -- bash docs/evidence/goal-07/containers/prove-containers-proxy-ca.sh`).
- Goal 06 and goal 07 mutation runners: refuse on `acfe14e`; not run on `ea29169`. Retire or port the 9 and 2 anchors, then run `.venv/bin/python docs/evidence/goal-06/mutants.py` and `.venv/bin/python docs/evidence/goal-07/mutants.py`.
- First-commit checks not run again on `ea29169` (listed under [Second tested commit](#second-tested-commit-ea29169-capacity-boundary-and-max_expiry_steps-halt-tests), "What still stands"). The tests of `unit-content`, `unit-genesis`, `unit-msgkit`, `unit-node` and `unit-svnerr` ran again inside [m2-unit.txt](logs/goal-08/m2-unit.txt), and the two devnet lease tests inside [m2-devnet.txt](logs/goal-08/m2-devnet.txt), all `--- PASS`. The fuzz runs, the Python, Node and `run_tests.py --only go` suites, the format, lint and type checks, the spec oracle's checks, SBOM and workflow policy, the module and proto checks, `tests-replaced`, the goal 06 and goal 07 anchors, `race-proto` and `race-agents-provider` did not run again; this file reads them as standing because `ea29169` changes no production file, and that is not a rerun.
- golangci-lint: `bash -c 'golangci-lint version; cd chain && golangci-lint run ./...'` needs a golangci-lint built with Go 1.26 or later.
- Fuzzing of `FuzzProviderTransitions` and `FuzzContentTransitions` on this tree.
- Post-docs checks on the evidence commit: they ran in head CI on `4db7e40` (`python` job: `scan_secrets.py`; `contract` job: `check_requirements.py` and `check_protocol.py`; all `success`, [Head CI](#head-ci)). They were not captured with `capture.sh` here.
- REQ-CAP-008, lease durations off the devnet, REQ-SEP-001 (new content version) and the approved lease terms specification: need owner decisions (DEC-005, DR-07-04, DR-08-04), not a rerun.

## Follow-ups

- Done in `ea29169`: a lease-level test that accepts at exactly capacity and rejects capacity + 1 on the real providers keeper (`TestCapacityBoundaryAgainstSpecMachine`, `TestCapacityBoundaryInApp`); `capacity-off-by-one` is now killed by both ([m2-mutants-leases.txt](logs/goal-08/m2-mutants-leases.txt)).
- Retire or port the moved anchors of the goal 06 and goal 07 runners.
- Fuzz `FuzzProviderTransitions` and `FuzzContentTransitions` on this tree.
- Add an app test for a chain with seats but no ready council.
- Isolate `tests/unit/test_secrets.py` from the real index (a temporary repository).
- Reject raw `/store` ABCI query paths in the node, or document the RPC exposure rule in the operator runbook.
- The verbose `chain/app` tests print about 425,000 SDK and IAVL debug lines (about 90 MB raw); the captures filter them in the command. A quieter test logger would make the filter unnecessary.
- Done in `ea29169` at FinalizeBlock level: a test that damages the governed `max_expiry_steps` value and shows the halt (`TestDamagedExpiryLimitHalts`). Still open: show that CometBFT stops a node on that panic (for example a devnet node), and what a restart on a committed damaged value does.
- Optionally let the Dockerfile accept an extra CA bundle as a BuildKit secret, so the unchanged `devnet` job can also run behind a TLS-intercepting proxy (goal 07 recorded the same follow-up; a packaging change).
- The runner has 41 ruff `E501` findings ([m2-ruff-mutants.txt](logs/goal-08/m2-ruff-mutants.txt)); `docs/evidence/` is outside CI's ruff paths.
- Let the differential generator reach D-6 and D-10 to D-13, or record why it cannot.
- Add `tests/difftest` to the mypy `files` list and fix the 3 errors.

## Commands

```text
devnet-lease.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh go test -count=1 -v -timeout 30m -run \^TestDevnetLease ./chain/devnet/
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
devnet.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh go test -count=1 -v -timeout 30m ./chain/devnet/
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
race.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh go test -race -count=1 -timeout 20m ./chain/...
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
unit.txt
COMMAND bash -c SVN_SKIP_DEVNET=1\ go\ test\ -count=1\ -v\ ./chain/...\ 2\>\&1\ \|\ grep\ -a\ -v\ -F\ \"\<nil\>\ DBG\ \"\;\ exit\ \"\$\{PIPESTATUS\[0\]\}\"
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
unit-leases.txt
COMMAND env -C chain go test -count=1 -timeout 900s -v ./x/leases/...
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
unit-app.txt
COMMAND bash -c env\ -C\ chain\ go\ test\ -count=1\ -timeout\ 900s\ -v\ ./app/...\ 2\>\&1\ \|\ grep\ -a\ -v\ -F\ \"\<nil\>\ DBG\ \"\;\ exit\ \"\$\{PIPESTATUS\[0\]\}\"
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
tests-go-count1.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh env GOFLAGS=-count=1 .venv/bin/python scripts/run_tests.py --only go
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
mutants-seat.txt
COMMAND .venv/bin/python docs/evidence/goal-08/mutants.py --group seat
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
mutants-review.txt
COMMAND .venv/bin/python docs/evidence/goal-08/mutants.py --group review
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
mutants-leases.txt
COMMAND .venv/bin/python docs/evidence/goal-08/mutants.py --group leases
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:0
```

```text
goal06-anchors.txt
COMMAND .venv/bin/python docs/evidence/goal-06/mutants.py --check
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:1
```

```text
goal07-anchors.txt
COMMAND .venv/bin/python docs/evidence/goal-07/mutants.py --check
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:1
```

```text
tests-python.txt
COMMAND .venv/bin/python scripts/run_tests.py --only python
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:1
```

```text
golangci-lint.txt
COMMAND bash -c golangci-lint\ version\;\ cd\ chain\ \&\&\ golangci-lint\ run\ ./...
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:3
```

```text
docker-info.txt
COMMAND docker info
TREE 6e81d0c948847af933b25e02be1f1bf2a34a62f2
EXIT:1
```

On `ea29169`:

```text
m2-unit.txt
COMMAND bash -c SVN_SKIP_DEVNET=1\ go\ test\ -count=1\ -v\ ./chain/...\ 2\>\&1\ \|\ grep\ -a\ -v\ -F\ \"\<nil\>\ DBG\ \"\;\ exit\ \"\$\{PIPESTATUS\[0\]\}\"
TREE eabf7cef6fc24f9afdd07a8f95470ad1f85ba375
EXIT:0
```

```text
m2-unit-app.txt
COMMAND bash -c env\ -C\ chain\ go\ test\ -count=1\ -timeout\ 900s\ -v\ ./app/...\ 2\>\&1\ \|\ grep\ -a\ -v\ -F\ \"\<nil\>\ DBG\ \"\;\ exit\ \"\$\{PIPESTATUS\[0\]\}\"
TREE eabf7cef6fc24f9afdd07a8f95470ad1f85ba375
EXIT:0
```

```text
m2-unit-leases.txt
COMMAND env -C chain go test -count=1 -timeout 900s -v ./x/leases/...
TREE eabf7cef6fc24f9afdd07a8f95470ad1f85ba375
EXIT:0
```

```text
m2-race.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh go test -race -count=1 -timeout 20m ./chain/...
TREE eabf7cef6fc24f9afdd07a8f95470ad1f85ba375
EXIT:0
```

```text
m2-devnet.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh go test -count=1 -v -timeout 30m ./chain/devnet/
TREE eabf7cef6fc24f9afdd07a8f95470ad1f85ba375
EXIT:0
```

```text
m2-mutants-leases.txt
COMMAND .venv/bin/python docs/evidence/goal-08/mutants.py --group leases
TREE eabf7cef6fc24f9afdd07a8f95470ad1f85ba375
EXIT:0
```

```text
m2-containers-unchanged.txt
COMMAND bash scripts/devnet/prove-containers.sh
TREE eabf7cef6fc24f9afdd07a8f95470ad1f85ba375
EXIT:1
```

```text
m2-containers-labelled-copy.txt
COMMAND bash docs/evidence/goal-07/containers/prove-containers-proxy-ca.sh
TREE eabf7cef6fc24f9afdd07a8f95470ad1f85ba375
EXIT:0
```

```text
m2-ruff-mutants.txt
COMMAND .venv/bin/ruff check docs/evidence/goal-08/mutants.py
TREE eabf7cef6fc24f9afdd07a8f95470ad1f85ba375
EXIT:1
```

The other commands are the first line of each log. The `m2-unit-providers.txt`, `m2-unit-authority.txt`, `m2-gofmt-vet.txt`, `m2-check-requirements.txt`, `m2-scan-secrets.txt` and `m2-check-protocol.txt` commands are byte for byte those of the first-commit logs without the prefix.

## Changed files

[diff-stat.txt](logs/goal-08/diff-stat.txt): `104 files changed, 21439 insertions(+), 412 deletions(-)` from `main` `2fc93e5` to `acfe14e`.

- `chain/x/leases` (new module: keeper, store, messages, sign payloads, expiry, withdrawal hook, queries, genesis, module, services, events), `chain/x/leases/leasestest`, `chain/x/leases/difftest`, `chain/x/leases/testdata/sign-vectors.json`, and `tests/difftest/spec_oracle.py`.
- `.github/workflows/ci.yml` (commit `4db7e40`, after `ea29169`): the race tests move to the `go-race` job, three shards ([Head CI](#head-ci)).
- `chain/x/providers`: `Reserve`, `Release` and `ReserveProbe` on the real keeper, with `reserve_test.go` and `FuzzReserveRelease`.
- `chain/x/content`: `CheckPackage` and the withdrawal hook wiring.
- `chain/x/authority`: DR-06-06 (`AdmissionOf`, genesis rule, rotation), and the `DevnetNumeric` and `PolicyVersion` read surface.
- `chain/genesis`: the DR-06-06 seat rule.
- `chain/app`: x/leases wiring, the ante seat gate, seat auth accounts, lease and seat tests, and `chain/app/testdata/lease-events.golden.json`.
- `chain/msgkit`: BeginBlock event parsing (`ParseBlockEvent`, `FuzzBlockEventParse`).
- `chain/devnet`: the `broadcaster` account, lease tests, no seat submitter rows.
- `chain/cmd/svnd/main.go` (usage text: x/leases exists, evidence and policy are Goals 09-10), `chain/node/start.go` (the query gas comment) and `chain/x/scaffold/module.go` (the leases slot is gone; comments only).
- Docs: `docs/architecture/leases.md`, `docs/architecture/registries.md`, `docs/architecture/authority-authorization.md`, `docs/architecture/README.md`, `docs/operations/validator-lifecycle.md`, module READMEs and `chain/README.md`.
- No change to `.proto` files, `docs/protocol/`, `docs/protocol.md`, goal pages, `.github/workflows/ci.yml` or module files ([protocol-unchanged.txt](logs/goal-08/protocol-unchanged.txt), [gomod-vs-main.txt](logs/goal-08/gomod-vs-main.txt)).

[m2-revision.txt](logs/goal-08/m2-revision.txt): `11 files changed, 1023 insertions(+), 11 deletions(-)` from `acfe14e` to `ea29169`, listed under [Second tested commit](#second-tested-commit-ea29169-capacity-boundary-and-max_expiry_steps-halt-tests): six test files, three docs, the runner `docs/evidence/goal-08/mutants.py` (version 2) and its `PY_ALLOW` entry with a one-line comment in `docs/evidence/goal-01/check_requirements.py`. Production paths: `<none>`.

Evidence added by this run, all under `docs/` and not yet committed:

- this file;
- the 89 logs under `docs/evidence/logs/goal-08/` (63 on `acfe14e`, 26 `m2-` on `ea29169`);
- the goal 08 rows in `docs/progress.md` and `docs/progress.tsv`, and one clause in each goal 07 row recording that DR-06-06 supersedes DR-07-11's interim rule.

## Next safe action

1. Done: the evidence commit `69f3980` (docs only, `Tested-commit` and `Tested-tree` trailers for both tested commits), draft [PR 11](https://github.com/Maxencejules/shortvideo-network/pull/11) on the owner's instruction, the CI shard split `4db7e40`, and head CI passing on it ([Head CI](#head-ci)). The local Docker build cache was pruned after the container runs (`docker builder prune -f`, 8.093GB reclaimed by the orchestrator; not a capture log).
2. The owner decides whether to merge PR 11. Merging needs the owner's separate approval; goal 08 stays BLOCKED on the owner decisions below either way. If branch protection lists required checks by name, the race tests now report as `go-race (app)`, `go-race (devnet)` and `go-race (rest)`.
3. The owner answers, or accepts the interims of, DR-08-13 and DR-08-02 (offer-slot blocking), DR-08-04 (sign payloads), DR-08-07 (expiry budget) and DR-08-01 (renewal after disable). DEC-005 values stay unset until the owner names them; do not treat the devnet term as policy.
4. Goal 09 builds on this revision. Wire `Deps.Evidence`, and decide the shared expiry budget first (DR-08-07).

## Proposed pull request

Title: Goal 08: lease lifecycle and capacity accounting (x/leases), with DR-06-06 seat-only admission

Review summary:

- Adds x/leases on the frozen v0 protos: offer, accept, renew and end; reservation and release through the providers keeper; deadline and expiry indexes with a per-block budget; content-withdrawal voiding of open offers; paginated `ActiveLeases` and `Deadlines`; typed events plus `leases_offer_closed`.
- Acceptance binds the provider's current key to the exact offer terms, the lease id and the real-time deadline. A service key or a service account cannot produce or relay it. END binds the term version (D-12).
- Implements the owner's DR-06-06 ruling: seats are admitted from the authority seat list, sign only propose and approve, and cannot be submitters. This supersedes DR-07-11's interim rule. No protocol change.
- Pins the capacity boundary at lease level (exactly at capacity commits, one byte over is `CAPACITY` with the state unchanged) on the real keepers against `spec_machine.py` and through FinalizeBlock, and tests that a damaged governed `max_expiry_steps` makes FinalizeBlock panic on both in-process test nodes, twice each (commit `ea29169`, tests and docs only).
- Evidence on tree `6e81d0c9` (`acfe14e`): unit suites; a differential test against `spec_machine.py` (256 sequences, 22862 compared steps, 0 divergences); seven real fuzz runs; app-level replay and full-state reject tests; a four-process single-machine devnet with timeout/retry, ack-versus-commit, expiry at effective-1 and effective, a bounded drain and a halt/restart; `-race`; 27 of 27 mutants killed. On tree `eabf7cef` (`ea29169`): the affected unit suites, `-race`, the devnet package and 28 of 28 mutants killed; goal 07's labelled container copy passed (empty blocks, host networking).
- Status BLOCKED: REQ-CAP-008 and lease durations (DEC-005 unset), REQ-SEP-001's new-content-version half (DR-07-04), the lease terms specification is a candidate (DR-08-04), the unchanged container devnet job fails in this container at its image build (proxy CA, environment) but passed in head CI, goal 06 and goal 07 mutation runners refuse on this tree.
- Fourteen goal 08 decision requests are open; the evidence file classifies six as security-critical. The devnet is four processes on one machine.
