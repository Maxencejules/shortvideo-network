# Goal 06 evidence

Status: VERIFIED

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a production network. Production council keys are not named.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH.
- Model that ran: `grok-4.7-high-fast`. Source: cursor-cloud `run-info` field `originalModelName` for `bcId=bc-e644227c-dbbe-5453-bc33-05e9f27aa7ff`.
- GPT-6 Astra was not used. A separate Astra HIGH control was not selected. `high-fast` is the suffix of the reported model name.
- Max waived the model and effort mismatch for this goal. The mismatch is recorded and is not a blocker.

## Tested revision

Go checks ran on commit `f38c6ef31ff98e082a9fddd9531c29b29d954792`, tree `4279ae1a61184bd306fd7e361d70b5cb2c6e9c86`. The working tree was dirty only because this evidence log and a ruff format of `scripts/scan_secrets.py` and `tests/secret_scanner_corpus.py` were uncommitted. Those two files do not change the chain. `git diff f38c6ef -- chain` is empty for that tree.

Base: `f3f583e19355a181ef64de874e5cf26679e6ec30`, subject `goal 05: chain app, genesis and 4-validator devnet (#8)` on `origin/main`. Branch `goal-06` was cut from that commit.

| Prerequisite | Status read before this work | Evidence |
| --- | --- | --- |
| Goal 02 | IMPLEMENTED | [goal-02.md](goal-02.md). Protocol v0 is the frozen candidate. DEC-001..012 are approved. |
| Goal 05 | VERIFIED | [goal-05.md](goal-05.md). Static validator set. Process proof and container proof are cited there. |

## What this goal is

`x/authority` is the only constructor of `abci.ValidatorUpdate`. Propose, approve, and execute are committed handlers. A validator power or key change is scheduled until its activation point. EndBlock applies due changes. CometBFT v0.40 applies the changeset two heights later (`state/execution.go` `updateState`, `LastHeightValidatorsChanged = header.Height + 2`).

The devnet uses four disposable seats and three approvals. That is DEC-008. The spec machine's three seats and threshold of two are rejected by genesis validation. `approved_council` stays empty. There is no single-signature admit, disable, or force-withdraw.

## Carried items

| Item | Status | What happened |
| --- | --- | --- |
| Goal 02 removal-bind-commit | VERIFIED | Decision: halt. A due removal whose consensus key does not match is not dropped and is not applied. `ApplyDue` returns `ERROR_CODE_CONFLICT` without writing. EndBlock returns that error, so the block is not committed. The pending entry remains and the next attempt fails the same way. `TestRemovalBindHaltsAndRotationsStaySeparate`. |
| Goal 03 secret-scanner regressions | VERIFIED | Seventeen cases were appended. No earlier id was deleted, renamed, or flipped. The pin is 249. `.env` `=#<hex>`, Makefile `?=`, yaml `${X:=hex}`, `Dockerfile.dev`, and a non-key `HexToECDSA` are positives. An empty `PRIVATE_KEY=` does not consume the next assignment. Base64 `==` padding is the value. BIP44 paths are not key bytes. A following `{`, `- name:`, or quoted key is not a continued literal. Compose `${PRIVATE_KEY:?}`, `${PRIVATE_KEY:-}`, and `${PRIVATE_KEY:-$Y}` are not hits. `${PRIVATE_KEY:=<hex>}` is. |
| Goal 05 restart threshold+1 | VERIFIED | The pass condition is two blocks after the restarted processes are observed up. [devnet-lifecycle.txt](logs/goal-06/devnet-lifecycle.txt): `phase=restart-up height=3`, then restart samples at height 9. |
| Goal 05 recovery stall+1 | VERIFIED | Same rule. The log shows stall 17, `phase=recovery-up height=19`, recovery samples at 21. |
| Goal 05 follow-up 7, run `41252788163` | VERIFIED | The one-shot `disagreement [14, 14, 13]` read is now `agreed_height`, which polls until the live nodes match. The note is in [goal-05.md](goal-05.md). |
| Goal 05 auth migrations 5 to 7 | DEFERRED | Unchanged. A fresh chain starts at auth version 7. Goal 10 registers the migrations before an upgrade from an older auth version. |
| Force-withdraw tombstone | DEFERRED | The payload parses. With no content record the execution is `ERROR_CODE_NOT_FOUND` and the proposal stays `QUEUED`. Content registration is Goal 07. A direct force-withdraw message is not registered. |

## Acceptance

| Check | Status | Evidence |
| --- | --- | --- |
| Approved proposal schedules, third approval queues, execute does not apply inside the transaction | VERIFIED | `TestProposalPathsDoNotMutateOnReject`, `TestCommittedAuthorityFlowAndReplay` |
| Rejected messages do not change authority bytes | VERIFIED | Outsider and corrupt signature in `TestWrongSignatureDoesNotMutate` and the engine test. Repeated vote is `ERROR_CODE_REPLAY` and stores one approval. |
| Stale approval, delay, expiry | VERIFIED | Approval after `QUEUED` is `ERROR_CODE_CONFLICT`. Delay 10 refuses early execute. Expiry marks `EXPIRED` and does not schedule power. |
| Unauthorized submitter and wrong signature | VERIFIED | Engine and app tests. Ante plus the handler. |
| Mixed height and time | VERIFIED | `TestAdmissionAndMixedActivation`. `ERROR_CODE_CONFLICT`, proposal stays `QUEUED`. |
| Duplicate and invalid sets | VERIFIED | Removing one of four equal powers is `ERROR_CODE_FORBIDDEN` and the proposal is `REJECTED`. Projection rejects duplicate keys and a share cap break. |
| Provider admission does not change the validator set | VERIFIED | Same test. Consensus, council, provider, and service rotations touch different records. |
| Effective height | VERIFIED | Application returns the changeset at the activation height. [devnet-lifecycle.txt](logs/goal-06/devnet-lifecycle.txt): height 42 has 4 validators, activation height 43 has 4, effective height 45 has 5. Rotate at 90 still has 5 and the new key. Remove at 132 has 4. |
| Quorum by power | VERIFIED | After the add, v5 is in the set and is not a process. Stopping one of the four processes leaves three processes up and 3/5 of the power online. The log stalls at 48 (race run) and at 45 (verbose run). Restart reaches two blocks past the up height (`up=46 reached=48` in the verbose log). `TestQuorumUsesPowerNotCount` checks 2,2,2,2,1 without reading a `down` field into state. |
| Fuzz and deterministic replay | VERIFIED | `FuzzParsePayload` seed, 200 bounded random inputs, and two apps with the same txs share `LastCommitID` in `TestCommittedAuthorityFlowAndReplay`. |
| `go test -race ./chain/...` | VERIFIED | [go-test-race.txt](logs/goal-06/go-test-race.txt). Exit 0. app 9.085s, devnet 109.355s, authority 1.109s. |
| Python scanner, ruff, mypy, SBOM, workflow policy | VERIFIED | [python-checks.txt](logs/goal-06/python-checks.txt). Exit 0. |
| Container script on this VM | BLOCKED | `docker` is not installed here. The script syntax check passed. CI runs `scripts/devnet/prove-containers.sh` on the host network. The bridge stays BLOCKED. |

## Commands

```text
COMMAND go test -race -count=1 -timeout 12m ./chain/...
DATE 2026-11-01T00:31:14+00:00
HEAD f38c6ef31ff98e082a9fddd9531c29b29d954792
TREE 4279ae1a61184bd306fd7e361d70b5cb2c6e9c86
EXIT:0
```

```text
COMMAND go test -count=1 -timeout 180s ./chain/devnet/ -run 'TestDevnetValidatorLifecycle|TestFour' -v
TREE 4279ae1a61184bd306fd7e361d70b5cb2c6e9c86
EXIT:0
```

```text
COMMAND .venv/bin/ruff format --check, ruff check, scan_secrets.py, mypy, write_sbom.py --check, workflow_policy.py, pytest
EXIT:0
```

The full outputs are the log files named above.

## Framework and licence notes

Cosmos SDK v0.55.0 discards the message branch when a handler returns an error, and it commits the ante branch first. Spec transitions that must keep a mark (`EXPIRED`, `REJECTED` on `ERROR_CODE_FORBIDDEN`) are saved, and the code is the `authority_result` event, because returning the error would drop the mark. The payload is not applied. A rejected message does not change authority bytes. The account sequence can advance when ante succeeded. That is not an authority decision.

The provider rotate preimage is SVN-JCS-1 of `expectedGeneration`, `newSigningKeyId`, `newSigningPubkey` (hex), and `providerId`, under `SVN-PROVIDER-ROTATE-1`. Protocol v0 names the domain and does not publish a rotate vector. This object is the reversible choice used here.

CometBFT v0.40 and cosmos-sdk v0.55.0 are Apache-2.0 as recorded by their modules. No project licence file is applied (DEC-006). Enterprise PoA and Groups are not imported.

## Residual risks

- One workstation is one operator. Four processes are not four independent operators.
- The container script's new waits are the same rule as the process test. This VM did not run the containers.
- Force-withdraw does not write a content tombstone until Goal 07.
- This is not an audit and not a production deployment.

## Next safe action

Goal 07 can implement the provider and content registries on top of these admission records. Do not treat the devnet seats as production council keys. Do not delete a genesis to recover a stuck chain.
