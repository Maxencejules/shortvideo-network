# Goal 06 evidence

Status: VERIFIED

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a production network. Production council keys are not named.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH.
- Model that ran: `grok-4.7-high-fast`. Source: cursor-cloud `run-info` field `originalModelName` for `bcId=bc-e644227c-dbbe-5453-bc33-05e9f27aa7ff`.
- GPT-6 Astra was not used. A separate Astra HIGH control was not selected.
- Max waived the model and effort mismatch. The mismatch is recorded and is not a blocker.

## Round 2 tested revision

Review [5725824160](https://github.com/Maxencejules/shortvideo-network/pull/9#pullrequestreview-5725824160) on `a6a5273` returned FIX. The proofs below ran after that fix, on commit `4c87282`, tree `eacc6b47cfaa19c876efdda1578324420dcdeb3c`. Each log prints that tree. The worktree was dirty only by these new log files.

`git diff eacc6b47 HEAD -- chain` is empty once this evidence commit is the child of `4c87282`. The chain sources in the evidence commit are that tree.

Base: `f3f583e19355a181ef64de874e5cf26679e6ec30` on `origin/main`. Branch `goal-06`.

| Prerequisite | Status | Evidence |
| --- | --- | --- |
| Goal 02 | IMPLEMENTED | [goal-02.md](goal-02.md). Protocol v0 stays the frozen candidate. |
| Goal 05 | VERIFIED | [goal-05.md](goal-05.md), [PR 8](https://github.com/Maxencejules/shortvideo-network/pull/8). |

## Round 2 fixes

| Review item | Status | What changed |
| --- | --- | --- |
| 1. Reject no-mutate byte compare | VERIFIED | `AuthoritySnapshot` is the length-prefixed state key and side document. `TestRejectedMessagesLeaveTheFullAuthorityState` uses `bytes.Equal` after outsider, corrupt signature, malformed propose, fee, duplicate id, missing approval, repeated approval, execute of an open proposal, stale approval, non-authority execute, and early execute. Log: [unit-r2.txt](logs/goal-06/unit-r2.txt). |
| 2. State-transition fuzz | VERIFIED | `FuzzStateTransitions` runs a bounded op sequence and checks the committed bytes and the power invariant. [fuzz-state-r2.txt](logs/goal-06/fuzz-state-r2.txt): `-fuzztime=20s`, 46195 execs, exit 0, tree `eacc6b47`. |
| 3. effective-1 set | VERIFIED | [devnet-r2.txt](logs/goal-06/devnet-r2.txt). Add: h+1=44 count 4, h+2=45 count 5. Rotate: h+1 still has the old key, h+2 has the new key. Remove: h+1 count 5 with the key, h+2 count 4. |
| 4. Stall height 48 | Dropped | That number was not in a committed log. The quorum stall in [devnet-r2.txt](logs/goal-06/devnet-r2.txt) is height 45. |
| 5. RegisterProvider and rotation | VERIFIED | Both are committed handlers in `app/services.go`. Update and drain stay `ERROR_CODE_FORBIDDEN` until Goal 07. |
| 6. Stale comments | VERIFIED | The handler comment no longer says it rechecks the signature. Init and EndBlock comments say EndBlock returns scheduled updates. |
| 7. Expiry before QUEUED | Follow-up | The order is `op_execute` in the frozen spec machine. This round does not change it. |
| 8. JSON scrape, no-op delete, unused compare | VERIFIED | Execute uses the record stored at propose time. The no-op delete and `bytesEqual` are gone. |
| 9. Delay upper bound | VERIFIED | A devnet delay outside `0..unix_seconds_max` is rejected. Approve refuses a delay that would overflow the execution time. |
| 10. Halt at app level | VERIFIED | `TestRemovalBindHaltLeavesTheSnapshot` in [unit-r2.txt](logs/goal-06/unit-r2.txt). The mismatched removal is not committed, and a second attempt fails with the same snapshot. |
| 11. Seat wording | VERIFIED | The operations note says four seats and three approvals. |
| 12. Progress link and goal 05 item 4 | VERIFIED | Goal 06 links [PR 9](https://github.com/Maxencejules/shortvideo-network/pull/9). Goal 05 follow-up 4 is marked done: the prove scripts print `DIRTY`. |
| 13. Head CI | See the CI section after the push of this commit. | |

## Acceptance

| Check | Status | Log on tree `eacc6b47` |
| --- | --- | --- |
| Full-state reject compare | VERIFIED | [unit-r2.txt](logs/goal-06/unit-r2.txt) `TestRejectedMessagesLeaveTheFullAuthorityState` |
| Approved schedule, replay of two apps | VERIFIED | [unit-r2.txt](logs/goal-06/unit-r2.txt) `TestCommittedAuthorityFlowAndReplay` |
| State-transition fuzz | VERIFIED | [fuzz-state-r2.txt](logs/goal-06/fuzz-state-r2.txt) exit 0 |
| Add, rotate, remove at effective-1 and effective | VERIFIED | [devnet-r2.txt](logs/goal-06/devnet-r2.txt) |
| Quorum by power, restart and recovery two blocks after up | VERIFIED | Same log. Stall 45. Restart up 3 then height 9. Recovery up 19 then height 21. |
| `go test -race ./chain/...` | VERIFIED | [go-test-race-r2.txt](logs/goal-06/go-test-race-r2.txt) exit 0. devnet 107.706s. |
| Python ruff, mypy, scan, SBOM, pytest | VERIFIED | [python-r2.txt](logs/goal-06/python-r2.txt) exit 0 |
| Container proof on this VM | BLOCKED | `docker` is not installed here. CI runs the host-network script. The bridge stays BLOCKED. |

## Commands

```text
COMMAND go test -count=1 -timeout 180s ./chain/devnet/ -run 'TestDevnetValidatorLifecycle|TestFour' -v
TREE eacc6b47cfaa19c876efdda1578324420dcdeb3c
EXIT:0
```

```text
COMMAND go test -fuzz=FuzzStateTransitions -fuzztime=20s -count=1 ./x/authority/
TREE eacc6b47cfaa19c876efdda1578324420dcdeb3c
EXIT:0
```

```text
COMMAND go test -race -count=1 -timeout 12m ./chain/...
TREE eacc6b47cfaa19c876efdda1578324420dcdeb3c
EXIT:0
```

```text
COMMAND go test -count=1 -timeout 120s ./chain/app/ ./chain/x/authority/ -v
TREE eacc6b47cfaa19c876efdda1578324420dcdeb3c
EXIT:0
```

Round 1 logs under `logs/goal-06/` that do not print tree `eacc6b47` are not evidence for this revision.

## Follow-ups

- Execute checks expiry before it checks `QUEUED`. That is the spec machine. This round did not reorder it.
- Force-withdraw still returns `ERROR_CODE_NOT_FOUND` until Goal 07 has a content record. The direct message is not registered.
- Auth version migrations 5 to 7 stay with Goal 10.

## Framework note

SDK v0.55 commits the ante branch before the message and discards the message branch when the handler returns an error. A spec `keep` path (`EXPIRED`, or `REJECTED` on `ERROR_CODE_FORBIDDEN`) is saved and the code is the `authority_result` event. Those paths are not in the no-mutate set. The signature check is the ante decorator, not a second check in the handler.

## Next safe action

Goal 07 can build the provider and content registries on `RegisterProvider` and `RotateProviderKey`. Do not treat devnet seats as production council keys.
