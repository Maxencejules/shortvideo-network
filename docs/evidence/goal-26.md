# Goal 26 evidence

Status: BLOCKED

`svn-restore` backs up a logical database dump, signed records, non-secret configuration, critical media, and an index checkpoint. It restores them onto a clean directory only when the origin path is absent. The database, the signed records, and the media are encrypted. The encryption key is not stored in the backup. A second live process with the same consensus public key is refused. Retention, key custody, and recovery policy are placeholders. No retention period is set. No service recovery-time or recovery-point target is set. Public launch stays disabled. There is no token. This is not an audit and not a production recovery.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/26-backups-restoration-and-signer-safety.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-c3d3f3b7-f221-5abb-9003-593b78dac203`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run. The playbook's GPT-6.1 Sol was replaced by this Cursor cloud agent on that default model.
- On 2026-10-31 (Toronto) the owner said "Don't mind the ai model requirement". On 2026-11-17 the owner confirmed that it applies to all goals. A model mismatch with the playbook is not a blocker. The evidence still records the model that actually ran.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `1728f9e78af4371bc2063b53b56014174e6f09cf` |
| Tree | `e2bbe7e7d1f9882127a6b21eb5a5944077743d28` |
| Base | `e6bcdc864ddbcfea5b379787af38ec6c03269ee5` |
| Branch | `goal-26` |
| Pull request | https://github.com/Maxencejules/shortvideo-network/pull/31 |

The tested commit message carries `Tested-tree: e2bbe7e7d1f9882127a6b21eb5a5944077743d28`. `git rev-parse 1728f9e78af4371bc2063b53b56014174e6f09cf^{tree}` is that tree. Later commits change only `docs/evidence/`.

The parent commit `d946e15e51766d6014f063e0b3c07f27009a3457` is the same drill. Its CI run [44889286224](https://github.com/Maxencejules/shortvideo-network/actions/runs/44889286224) failed `go-race (rest)` because that shard does not install PostgreSQL and the drill called `t.Fatal`. The tested commit skips those two tests when `initdb` is absent. The go job installs PostgreSQL 18 and runs them.

## Captures

Every log under [logs/goal-26/](logs/goal-26/) was captured while `git status --porcelain` was empty. Each file's line 4 is `DIRTY no`. The commands ran from the repository root. The log files were written outside the worktree and copied in afterward, so they do not appear in those stamps. Every log ends `AFTER clean`. Nothing tracked was left untracked. A gitignored `.venv` was present and is not in those stamps.

## Head CI

[Run 44890178806](https://github.com/Maxencejules/shortvideo-network/actions/runs/44890178806) on `1728f9e78af4371bc2063b53b56014174e6f09cf`. Conclusion: `success`. All jobs passed, including `go` and `go-race (rest)`.

This file is added after that run. A later head that only changes `docs/evidence/` is not that run.

## What ran

| Command | Scope | Log | Result |
| --- | --- | --- | --- |
| runtime versions | repository root | [runtime.txt](logs/goal-26/runtime.txt) | line 12 `EXIT:0` |
| `gofmt -l` | the Go files this goal added | [gofmt.txt](logs/goal-26/gofmt.txt) | line 5 `EXIT:0` |
| `go vet` | `services/restore` | [govet.txt](logs/goal-26/govet.txt) | line 5 `EXIT:0` |
| `go test -count=1 -timeout 180s -v` | `services/restore` | [restore-test.txt](logs/goal-26/restore-test.txt) | line 57 `EXIT:0` |
| `go test -race -count=1 -timeout 180s -v` | `services/restore` | [restore-race.txt](logs/goal-26/restore-race.txt) | line 57 `EXIT:0` |
| pytest | operations runbooks and the CI wiring check | [pytest.txt](logs/goal-26/pytest.txt) | line 6 `EXIT:0` |
| `ruff check` and `ruff format --check` | the two named Python files | [ruff.txt](logs/goal-26/ruff.txt) | line 7 `EXIT:0` |
| `mypy` | `tests/unit/test_ci_restore.py` | [mypy.txt](logs/goal-26/mypy.txt) | line 6 `EXIT:0` |
| `python3 scripts/scan_secrets.py` | tracked files | [secrets.txt](logs/goal-26/secrets.txt) | line 5 `secret scan ok`, line 6 `EXIT:0` |
| `python3 scripts/workflow_policy.py` | the workflow file | [workflow.txt](logs/goal-26/workflow.txt) | line 5 `workflow policy ok files 1`, line 6 `EXIT:0` |
| `python3 scripts/write_sbom.py --check` | lockfiles | [sbom.txt](logs/goal-26/sbom.txt) | line 5 `sbom ok components 1389`, line 6 `EXIT:0` |
| `python3 docs/evidence/goal-01/check_requirements.py` | requirements checker | [requirements.txt](logs/goal-26/requirements.txt) | line 64 `result PASS`, line 66 `EXIT:0` |
| PostgreSQL 18 | `/usr/lib/postgresql/18/bin` | [pg18.txt](logs/goal-26/pg18.txt) | line 7 `EXIT:0` |
| PostgreSQL 16 | `/usr/lib/postgresql/16` | [pg16.txt](logs/goal-26/pg16.txt) | line 5 `pg16 absent`, line 6 `EXIT:1` |
| `docker version` | host | [docker.txt](logs/goal-26/docker.txt) | line 5 `bash: line 1: docker: command not found`, line 6 `EXIT:127` |
| `golangci-lint version` | host | [golangci.txt](logs/goal-26/golangci.txt) | line 5 `bash: line 1: golangci-lint: command not found`, line 6 `EXIT:127` |

[runtime.txt](logs/goal-26/runtime.txt) line 5 is `v22.23.4`. Line 6 is `10.9.9`. Line 7 is `Python 3.12.3`. Line 8 begins with `go version go1.26.10`. Line 9 begins with `Google Chrome 157.0.8103.50`. Line 10 begins with `Linux 6.12.113+`. Line 11 begins with `postgres (PostgreSQL) 18.6`.

[pg18.txt](logs/goal-26/pg18.txt) line 5 is the same PostgreSQL 18.6 server line. Line 6 begins with `psql (PostgreSQL) 18.6`.

[pytest.txt](logs/goal-26/pytest.txt) line 5 is 31 dots and `[100%]`. That is the quiet reporter. It does not print a `passed` count. Line 6 is `EXIT:0`. The command on line 3 is `tests/unit/test_operations_docs.py` and `tests/unit/test_ci_restore.py`.

[ruff.txt](logs/goal-26/ruff.txt) line 5 contains `All checks passed`. Line 6 is `2 files already formatted`.

[mypy.txt](logs/goal-26/mypy.txt) line 5 is `Success: no issues found in 1 source file`.

The restore module adds no third-party Go module and no `go.sum`. [sbom.txt](logs/goal-26/sbom.txt) line 5 stayed `sbom ok components 1389`.

## Observed recovery time and data age

These two numbers are the library drill in [restore-test.txt](logs/goal-26/restore-test.txt) line 17. They are measurements of that one run on this VM. They are not service guarantees. The manifest fields for a service target stay the string `UNSET` (`services/restore/internal/restore/restore.go:30`).

| Label | Seconds | Where |
| --- | --- | --- |
| Observed RTO | `0.481788595` | [restore-test.txt](logs/goal-26/restore-test.txt) line 17 |
| Observed RPO | `0.00113662` | [restore-test.txt](logs/goal-26/restore-test.txt) line 17 |

The clock starts when the origin directory has already been removed and the restore begins (`services/restore/internal/restore/drill.go:98`). It stops after the dump has been loaded into a new PostgreSQL cluster and the row text matches (`services/restore/internal/restore/drill.go:142`). The observed RPO is the backup's creation time minus the time the rows were committed. The race run recorded a second pair on [restore-race.txt](logs/goal-26/restore-race.txt) line 17: RTO `0.467663656`, RPO `0.002969836`, with the same `false` guarantee flag. That pair is another observation of another run.

## Restore on a clean directory

[restore-test.txt](logs/goal-26/restore-test.txt) line 16 is `status=restored`, `digests_match=true`, `authorization_match=true`, `playback_after=true`, `removal_after=true`, `postgres=true`, `origin_unavailable=true`, `rows_match=true`, `signed_match=true`.

The drill writes two synthetic media objects into PostgreSQL 18.6, dumps that database with `pg_dump`, encrypts the dump with the signed record and the media, deletes the origin directory, and restores into a new directory and a new cluster. Playback returns the published object's bytes only when the SHA-256 matches. The removed object's playback is refused. The media bytes of the removed object are kept. History is not marked erased.

This is not the goal 18 four-validator milestone and it is not the HLS server. The bytes are synthetic. The authorization values are `published` and `removed`. The database comparison is the `SELECT` text before the origin cluster is destroyed and after the dump is loaded into the clean cluster.

Restore order is configuration, database, signed records, media, index, then signer-safety metadata (`services/restore/internal/restore/restore.go:52`). A live replica is refused as a backup source (`services/restore/internal/restore/restore.go:805`). Rebuilding the projection copies the checkpoint rows and does not invent rows from leftover media (`services/restore/internal/restore/restore.go:632`). The harness does not dial a node and does not replay a block store.

## Corrupted, wrong-version, and incomplete fixtures

[restore-test.txt](logs/goal-26/restore-test.txt) line 18 is `corrupt=refused wrong_version=refused incomplete=refused partial=partial missing_media=refused replica_rejected=true secrets_in_ci=false`.

The corrupt fixture flips a ciphertext byte and rewrites that file's digest in the manifest. The encryption tag still fails, and no destination directory is created. Format version 2 is refused before a destination is created. A backup with the media part removed is refused. The same incomplete backup with the partial flag restores the database and reports `partial`, and playback then returns media missing. A planted `live.key` makes the CI export refuse. The ordinary export is the manifest only.

## Signer safety

[restore-test.txt](logs/goal-26/restore-test.txt) lines 21 through 27:

| Line | Result |
| --- | --- |
| 21 | `SIGNER duplicate=refused` |
| 22 | `SIGNER copied_live_key=refused` |
| 23 | `SIGNER rollback=refused` |
| 24 | `SIGNER stale=refused` |
| 25 | `SIGNER unverified=refused` |
| 26 | `SIGNER replace=accepted` |
| 27 | `SIGNER distinct_keys=true` |

The duplicate check is two processes of the test binary. The second exits 2 while the first holds the lock. The lock file is outside the home and is named by the consensus address (`services/restore/internal/restore/signer.go:325`). A copied home does not carry that lock. A process killed without a stop attestation cannot be replaced (`services/restore/internal/restore/signer.go:147`). An older height, round, or step is refused (`services/restore/internal/restore/signer.go:160`). A snapshot whose height is behind the recorded chain head does not receive a signing permission (`services/restore/internal/restore/signer.go:167`). Peer, validator, and governance files must be three paths and three different byte strings (`services/restore/internal/restore/signer.go:207`).

This is one machine. It does not show that a second computer cannot start the same key. CometBFT stores height, round, and step. This tool reads that shape, with height as a decimal string. The goal 10 copy taken at a halt was not walked again.

## Rotation, recovery, end of service

[restore-test.txt](logs/goal-26/restore-test.txt) line 10 is the command refusing rotation with no approval. Line 11 is a drill rotation that writes a 32-byte public key. Line 30 is `old_rejected=true new_accepted=true operator_named=false approval_required=true`. The test token path is `services/restore/internal/restore/restore_test.go:163`. The token is the string `test-approval`. It does not name a person. This rotation does not submit a chain transaction.

Line 31 is `ACCOUNT export_restored=true completion=BLOCKED request=DR-26-05`. Completing account recovery is refused (`services/restore/internal/restore/restore.go:744`).

Line 34 is `END admissions_stopped=true chain_submitted=false history_erased=false retention_period=UNSET`. Stopped admissions refuse playback. The lease handoff file does not submit a transaction. Controlled deletion requires the confirmation word and a drill flag or an operator token, and it does not mark history erased.

## Compatibility

[restore-test.txt](logs/goal-26/restore-test.txt) line 37 is `COMPAT format=SVN-BACKUP-1 schema=1 migration=refused`. `services/restore/internal/restore/restore.go:775` accepts only format version 1 and schema version 1. No migration runs. A backup from another version is not adapted.

Lines 38 through 44 name what a current media replica does not hold: validator signing state, the backup encryption key, account sessions, uncommitted quarantine uploads, moderation complaint text, pruned chain blocks, and package versions absent from that replica. The same list is `services/restore/internal/restore/restore.go:761`.

## Manual approvals before any live recovery

[restore-test.txt](logs/goal-26/restore-test.txt) lines 45 through 50, and `services/restore/internal/restore/restore.go:749`:

1. The retention period is unset.
2. The key custody procedure is unset.
3. The recovery policy, and any service recovery-time or recovery-point target, is unset.
4. No operator is named who can approve a production restore or a production key rotation.
5. Account recovery completion waits on the custody decision.
6. Signer replacement on a live node needs an independent review.

The procedure is [docs/operations/backup-and-restore.md](../operations/backup-and-restore.md). Keys used here were generated in the test process. None were committed.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-26-01 | OPEN, BLOCKED | Retention period is unset. DEC-004. `services/restore/internal/restore/restore.go:30` is `UNSET`. |
| DR-26-02 | OPEN, BLOCKED | Key custody procedure is unset. DEC-001. The backup is not signed by a custody key. |
| DR-26-03 | OPEN, BLOCKED | Recovery policy is unset. Service RTO and RPO targets are unset. The numbers above are this test only. |
| DR-26-04 | OPEN, BLOCKED | No operator is named. DEC-002. Production restore and production key rotation have no approver. |
| DR-26-05 | OPEN, BLOCKED | Account recovery completion is refused. |
| DR-26-06 | OPEN, BLOCKED | Signer replacement on a live node has not had an independent review. |
| DR-26-07 | OPEN | The address lock is one machine. A second computer is not covered. |
| DR-26-08 | OPEN | Backup integrity uses the encryption tag and file modes. There is no custody signature. |
| DR-26-09 | OPEN, BLOCKED | A block-store replay was not run. The checkpoint export is the input. The goal 18 milestone was not repeated. |
| DR-26-10 | OPEN, BLOCKED | PostgreSQL 16 is absent. The server in this drill is 18.6. |
| DR-26-11 | OPEN | The replica list above cannot be rebuilt from current media copies. |
| DR-26-12 | OPEN, BLOCKED | golangci-lint is absent. Docker is absent. Compose was not run. The daemon on port 2375 was not used. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody stays unimplemented. This goal does not take custody.
2. **DEC-004.** No retention period.
3. **DEC-002.** No operator is named.
4. **One machine.** The two signer processes are one VM.
5. **Goal 06 and goal 07 mutation runners.** Not re-run.
6. **golangci-lint.** Not installed.
7. **PostgreSQL 16.** Not installed. The drill used PostgreSQL 18.6.
8. **Goal 04 compose bridge.** Docker is not installed.

A full `python3 scripts/run_tests.py --only python` on this machine failed `tests/unit/test_media_postgres.py::test_postgres_claim_is_once_and_does_not_publish`. That test runs only when `/usr/lib/postgresql/18/bin/initdb` exists. The CI python job does not install PostgreSQL, so CI skips it. The list query at `workers/media/svn_media/queue.py:66` names `idempotency_key`. The test schema at `tests/unit/test_media_postgres.py:35` does not create that column. This goal did not change those files.

## Requirements

The requirements matrix was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-OPS-005 | VERIFIED for two processes on this machine. A second live process with the same consensus public key exits 2. |
| REQ-GEN-004 | VERIFIED for three distinct files and a copied validator file on this machine. A second computer was not tested. |
| REQ-UX-019 | The service-key drill rotates a key generated in the test and rejects the retired public key. It is not a chain transaction and not a production rotation. |
| REQ-OPS-004 | BLOCKED on DEC-004. The drill used a placeholder scope. Retention is unset. |
| REQ-REC-002 | BLOCKED on DEC-001. Stop-and-verify ran in the drill. The custody procedure is unset. |
| REQ-QA-009 | BLOCKED on DEC-001. Observed RTO and RPO are recorded above. They are not a service gate. |
| REQ-SCOPE-016 | BLOCKED on DEC-004. The creator and origin backup duration is unset. |

## Next safe action

Do not restore a live node and do not rotate a live service key from this drill. Name a retention period, a custody procedure, and a recovery policy before any live recovery. An independent review of signer replacement is still required. Public launch stays disabled.
