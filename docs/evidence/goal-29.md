# Goal 29 evidence

Status: BLOCKED

This file is a model-assisted review of commit `82f3d6566046f18b9cbaeeeeab547b1d3b2dc954` and the fault tests that ran on this VM. It is not an independent human security audit. It is not a claim of unrestricted permissionless security, not a cryptographic storage-proof guarantee, and not protection from every hostile operator. Public launch stays disabled. There is no token. The candidate stays NOT-RELEASED.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort EXTRA HIGH (`docs/goals/29-adversarial-review-and-fault-testing.md` line 6). GPT-6 Astra was not used.
- This run is Cursor cloud agent `bc-9ab3acf4-799f-5759-9a95-031fb3d2fd9c`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run.
- On 2026-10-31 (Toronto) the owner said "Don't mind the ai model requirement". On 2026-11-17 the owner confirmed that it applies to all goals. On 2026-11-21 the owner chose to start goal 29 at the account default reasoning. A model mismatch with the playbook is not a blocker.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `82f3d6566046f18b9cbaeeeeab547b1d3b2dc954` |
| Tree | `0565fac273d92f5caf3f28355aeab3ae53be2582` |
| Base | `f85831abcde722ffa455caf35553de9d90ac3bcc` |
| Branch | `goal-29` |
| Pull request | https://github.com/Maxencejules/shortvideo-network/pull/34 |

The tested commit message carries `Tested-tree: 0565fac273d92f5caf3f28355aeab3ae53be2582`. `git rev-parse 82f3d6566046f18b9cbaeeeeab547b1d3b2dc954^{tree}` is that tree. Later commits change only `docs/evidence/`.

## Machine

This VM only. No paid runner and no other machine.

| Item | Value |
| --- | --- |
| CPUs | `nproc` 4 |
| Memory | `MemTotal` 16387328 KiB |
| OS | Ubuntu 24.04.5 LTS |
| Kernel | Linux 6.12.113+ |
| Go | `go1.26.10` |
| Python | 3.12.3 |
| Docker | absent |
| PostgreSQL client | absent |
| `tc` | absent |
| golangci-lint | absent |

Port 2375 was not used. Docker was not started. `tc` is absent, so a shaped-network fault was not injected.

## Head CI

[Run 44940699693](https://github.com/Maxencejules/shortvideo-network/actions/runs/44940699693) is `ci` on `82f3d6566046f18b9cbaeeeeab547b1d3b2dc954`. Conclusion: `success`. All 17 jobs passed: `contract`, `go`, `python`, `node`, `devnet`, `milestone`, and the eleven `go-race` jobs (`app`, `rest`, `indexer`, `broadcaster`, `api`, `devnet`, `devnet-evidence`, `devnet-upgrade`, `devnet-broadcaster`, `devnet-broadcaster-stall`, `devnet-indexer`).

This file is added after that run. A later head that only changes `docs/evidence/` is not that run.

## What ran

Every log below was captured on `82f3d6566046f18b9cbaeeeeab547b1d3b2dc954` while `git status --porcelain` was empty. Line 1 is that commit. Line 2 is tree `0565fac273d92f5caf3f28355aeab3ae53be2582`. Line 4 is `DIRTY no`. The last line is `AFTER clean`.

| Command | Log | Result |
| --- | --- | --- |
| provider agent regression tests | [provider-agent.txt](logs/goal-29/provider-agent.txt) | line 5 `ok` in 0.075s, line 6 `EXIT:0` |
| provider conformance and operator flow | [provider-conformance.txt](logs/goal-29/provider-conformance.txt) | line 5 `ok` in 7.447s, line 6 `EXIT:0` |
| fetch-policy test | [observe-fetch.txt](logs/goal-29/observe-fetch.txt) | line 5 `ok` in 0.002s, line 6 `EXIT:0` |
| scheduler stall, confirm, and repair tests | [scheduler.txt](logs/goal-29/scheduler.txt) | line 6 `ok` in 15.184s, line 9 `ok` in 0.007s, line 11 `EXIT:0` |
| stalled-chain deletion test | [moderation.txt](logs/goal-29/moderation.txt) | line 5 `ok` in 0.007s, line 6 `EXIT:0` |
| rejected chain messages and idempotent replay | [chain-reject.txt](logs/goal-29/chain-reject.txt) | lines 5-8 `ok`, line 9 `EXIT:0` |
| altered observation and dishonest auditor | [false-auditor.txt](logs/goal-29/false-auditor.txt) | line 5 `ok` in 0.041s, line 6 `ok` in 0.345s, line 7 `EXIT:0` |
| admission pytest | [admission.txt](logs/goal-29/admission.txt) | line 5 is three dots and `[100%]`, line 6 `EXIT:0` |
| `ruff check` on the edited Python files | [ruff.txt](logs/goal-29/ruff.txt) | line 5 contains `All checks passed`, line 6 `EXIT:0` |
| `mypy` on those three files | [mypy.txt](logs/goal-29/mypy.txt) | line 5 `Success: no issues found in 3 source files`, line 6 `EXIT:0` |

No flake was observed in these runs. The serving-copy test repeats the lookup 40 times inside one test so a map-order accident does not pass. That repetition passed.

These runs are one machine. Several processes on this VM are process fault injection. They are not independent operators.

## Fixed findings

A finding is closed only when the tested tree contains the change and a regression test in the table above passed on that tree.

### F-29-01 closed

Partial provider accept undercounted local quota and could keep a storage hold after the bytes were freed.

`agents/provider/internal/agent/accept.go:404` is `reserve`. `accept.go:477` is `releaseOffer`. Before this change, `reserve` treated an observe replay as success and did not add the bytes again, while the failure paths subtracted `UsedBytes` and left the observe hold in place. A retry could sit under `QuotaBytes` on the books and still be serving the package. Releasing the bytes without releasing the hold also filled the observe cap, so the next offer was refused after the disk copy was gone.

The hold key is now the offer id plus an attempt number. `releaseOffer` returns the local bytes and that hold. The next `reserve` of the same offer opens one new hold and counts the bytes again. A second `reserve` while the offer is still charged does not take a second hold.

[provider-agent.txt](logs/goal-29/provider-agent.txt) line 5 is `ok`. The tests are `TestPartialAcceptRechargesLocalQuota` (`admit_test.go:47`) and `TestEndedHoldFreesCapacityForTheNextOffer` (`admit_test.go:87`). `TestStorageReservationStopsAtTheCap` (`admit_test.go:12`) still refuses a second distinct offer at the cap.

### F-29-02 closed

Processing slots were not returned, so the placeholder cap stopped later jobs.

`services/observe/internal/obs/obs.go:32` is `DefaultProcessingLimit int64 = 2`. `KindProcessing` is a hold, not a one-minute window (`obs.go:259`). The worker admitted a slot in `run_once` and never called release. After two holds, later jobs were refused until the observe process restarted.

`workers/media/svn_media/admit.py:43` is `release_grant`. `workers/media/svn_media/queue.py:355` reads the grant id. `queue.py:369`, `queue.py:381`, and `queue.py:385` release it when the job is cancelled, failed, or ready. `queue.py:404` releases it when recovery finds a job that already finished.

[admission.txt](logs/goal-29/admission.txt) line 5 is three dots and `[100%]`. One of those tests is `test_a_failed_job_returns_the_processing_slot` (`tests/unit/test_goal25_admission.py:86`). The observe limit in that test is 1. After the failed job, a different key is granted.

### F-29-03 closed

The observe fetch check allowed addresses that are not globally reachable.

`services/observe/internal/obs/fetch.go:66` is `checkHost`. `fetch.go:125` is `blockedV4`. Go's `IsPrivate` does not cover `0.0.0.0/8` except `0.0.0.0`, `100.64.0.0/10`, the documentation ranges, `198.18.0.0/15`, `240.0.0.0/4`, or a name whose last label starts with a digit (`127.1`, `0x7f000001`). `CheckFetch` does not dial. It is the decision `POST /v1/fetch-check` returns. `192.0.0.9` and `192.0.0.10` stay allowed because the IANA special-purpose registry marks those two anycast addresses globally reachable.

[observe-fetch.txt](logs/goal-29/observe-fetch.txt) line 5 is `ok`. The test is `TestFetcherRejectsPrivateTargets`.

### F-29-04 closed

A removed lease could hide the serving copy, and the result depended on map order.

`agents/provider/internal/agent/read.go:243` calls `selectLease`. `read.go:274` is `selectLease`. The old loop stopped at the first lease with the same content id and version. A `REMOVED` row could be that first row, so a caller with the serving token received 410 or 404. Another token could be judged against the wrong row.

`selectLease` returns the serving lease whose token matches, otherwise a serving lease, otherwise another row. [provider-agent.txt](logs/goal-29/provider-agent.txt) includes `TestRemovedLeaseDoesNotHideTheServingCopy` (`serve_test.go:15`). The other token is status 401. The matching token receives the stored bytes. The loop count inside that test is 40.

## Open findings

### F-29-05 open

Storage and processing grant ids are kept in process memory. A provider restart while `svn-observe` is still running does not release the storage hold, because the id was not on disk. A worker crash after the running job file is removed and before `release_grant` can leave one processing slot. No owner sentence closes this. It stays open.

## Rerun of fault scenarios

These are the scenarios named in the goal. They ran as processes on this VM.

| Scenario | What ran | Result |
| --- | --- | --- |
| Rejected chain operations leave state unchanged | [chain-reject.txt](logs/goal-29/chain-reject.txt) lines 5-8 | `ok`. The test names are `TestRejectedContentMessagesLeaveTheStore`, `TestRejectedProviderMessagesLeaveTheStore`, `TestRejectedAuditMessagesLeaveTheFullState`, and `TestRejectedLeaseMessagesLeaveTheFullState`. |
| Replay at the same business id does not write a second effect | [chain-reject.txt](logs/goal-29/chain-reject.txt) line 7, the evidence package | `ok` in 0.050s. The selected test is `TestIdempotentReplay`. |
| False auditor attestation | [false-auditor.txt](logs/goal-29/false-auditor.txt) lines 5-6 | `ok`. `TestAlteredObservationD1`, `TestRecordStateNeedsTheDigestAlgorithm`, and `TestDishonestAdmittedAuditorInApp`. A mismatched observation is not stored as a passing proof. The app test's own comment says the dishonest batch leaves the full state unchanged at the cap. |
| Second requester | `TestRemovedLeaseDoesNotHideTheServingCopy` | The other token is 401. The token holder receives the bytes. This is one process refusing a second caller. It is not a proof that every provider will serve every honest caller. |
| Host loss and repair | [scheduler.txt](logs/goal-29/scheduler.txt) line 6 | `ok` in 15.184s. `TestUnreachableHolderDoesNotExtendConfirm`, `TestReachableByteMismatchStillWaits`, and `TestRepairReverifyFailureIsCooling`. An unreachable holder does not stretch the confirm window into a false confirmation. A failed re-verify stays cooling. |
| Chain stall and deletion during repair | [scheduler.txt](logs/goal-29/scheduler.txt) line 9 and [moderation.txt](logs/goal-29/moderation.txt) line 5 | `TestLocalDelistDoesNotWaitForAStalledChain` and `TestStalledChainAndUnreachableHosts`. A stalled chain does not become a successful deletion. The failure stays on the report. |
| Quorum loss on separate machines | not run | `tc` is absent. No second machine. DR-29-03. |
| Shaped network fault | not run | `tc` is absent. Inherited from DR-28-03. |

Provider conformance on this commit is [provider-conformance.txt](logs/goal-29/provider-conformance.txt) line 5, `ok` in 7.447s. That run includes the dishonest-fetch and repeat-id cases already in `TestConformance`. It is one machine.

## Review notes that did not become a code change

Chain withdraw uses a cache branch and writes it back only when the hook succeeds (`chain/x/content/msg_server.go` `commit`). Evidence checks finish before the records are written. Those paths were read. The rerun above is the execution evidence for rejected messages. A new consensus algorithm was not added.

CI permissions in `.github/workflows/ci.yml` are `contents: read`. The workflow has no `pull_request_target`. Action SHAs are pinned. No workflow file was changed. That reading is not an outside supply-chain audit.

Custody and recovery stay on DEC-001. This goal did not take production keys and did not run a compromise drill. Generated test keys in the existing suites are not production keys.

Mocks in unit tests were not treated as end-to-end evidence. The conformance and chain app tests above use the packages under test. Devnet was not rerun. Browser playback was not rerun. Load was not rerun.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-29-01 | OPEN, BLOCKED | Independent human security audit of authorization, authority updates, custody, attestations, and sandboxes. REQ-SEC-011. This file does not supply that audit. |
| DR-29-02 | OPEN, BLOCKED | DEC-001 custody and recovery. No production key. No compromise drill. Account recovery completion stays refused. |
| DR-29-03 | OPEN, BLOCKED | One machine. No separate-validator partition. `tc` is absent, so a shaped fault was not injected. |
| DR-29-04 | OPEN | F-29-05. Grant ids are process memory. A restart of the provider or a crash after the worker unlinks the job can leave a hold. |
| DR-29-05 | OPEN, BLOCKED | golangci-lint, PostgreSQL, and Docker are absent here. Port 2375 was not used. |
| DR-29-06 | OPEN | The pinned dependency set was not given an outside review. The workflow reading above is not that review. |

## Inherited blockers

BLOCKED, carried forward. A missing tool or an earlier open request is not a pass. None of these ids were marked complete.

1. DR-25-01 through DR-25-12 stay open as goal 25 recorded them. The processing cap in this goal is still a placeholder, not a DEC-005 allowance.
2. DR-26-01 through DR-26-12 stay open as goal 26 recorded them.
3. DR-27-01 through DR-27-13 stay open as goal 27 recorded them. The candidate remains NOT-RELEASED.
4. DR-28-01 through DR-28-11 stay open as goal 28 recorded them.
5. DEC-001 and DR-07-01. This goal does not take custody.
6. DEC-002. No operator is named.
7. One machine. Process fault injection is one failure domain.
8. The goal 06 and goal 07 mutation anchors were not repaired here.

## Requirements

The requirements matrix was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-EVID-003 | The false-auditor tests and the second-requester refusal ran. See the logs above. This is not a proof that every auditor is honest. |
| REQ-SEC-005 | Rewards stay absent. There is no token. The dishonest-auditor and second-requester cases ran. A Sybil campaign on separate machines did not run. |
| REQ-SEC-007 | Rejected messages in the named tests left state unchanged, and the idempotent replay test passed. A partition of separate validators did not run. DR-29-03. |
| REQ-SEC-011 | This file is the model-assisted review. The human audit is DR-29-01 and is unmet. |
| REQ-QA-012 | Unit, the named chain tests, provider conformance, scheduler fault tests, and the admission test ran. Property, fuzz, contract, browser, devnet, and load were not rerun. A missing run is not counted as passed. |
| REQ-QA-013 | Passing the tests in this file is not security against an unrestricted economically motivated network. |

## Release assessment

Do not release this revision. The four closed findings are fixed on the tested tree and the regression tests above passed. The open findings, the inherited blockers, and the missing human audit remain. Public launch stays disabled. There is no token. Goal 30 is the next goal and is not started by this file.

## Next safe action

Do not publish a package. Do not treat this file as an audit. Goal 30 is not started. The human audit in DR-29-01 is still required before any release decision.
