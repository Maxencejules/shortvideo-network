# Council changes

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. The council keys in a devnet home are disposable devnet keys, not the keys of any real seat holder, and the review delay and the activation lead bounds are PROPOSED values, not approved policy.

This runbook covers the governed changes of goal 10: a governed limit change (at execute or at a scheduled height), a council seat key replacement, and an upgrade plan and its cancel. The rules are in the specification, [governance-and-upgrades.md](../architecture/governance-and-upgrades.md#3-council-flow-exact-order) sections 3 to 8 and 12. The protocol rows are in `docs/protocol.md`, section "Goal 10 additive revision".

## Who does what

| Step | Signer | Message |
| --- | --- | --- |
| propose | one council seat | `svn.authority.v1.MsgProposeChange` with the payload, its SHA-256 digest, the parameter key, the base policy version, and an expiry |
| approve | three distinct seats; the proposer's seat may be one of them | `svn.authority.v1.MsgApproveChange` |
| execute | the `AUTHORITY` submitter account, after the review delay | `svn.authority.v1.MsgExecuteChange` |

Three distinct seats of four approve every payload kind, old and new; there is no per-kind threshold. The owner chose this with the option "3 of 4 + min notice (Recommended)" (Q3, DR-10-03). A second approval from the same seat is `ERROR_CODE_REPLAY` (`TestDuplicateVotes`). An execute before three approvals is `ERROR_CODE_CONFLICT` and the proposal stays `OPEN` (`TestInsufficientApproval`). An execute signed by any other account fails at execution and leaves every store unchanged (`TestUnauthorizedPolicyWritesFailAtExecution`).

The first successful execute bumps the policy version. Every other proposal in flight on the old version then fails at execute with `ERROR_CODE_STALE_VERSION`, or `ERROR_CODE_CONFLICT` when its parameter key last executed another digest (`TestConcurrentProposals`). Propose it again on the new version.

## How the devnet submits these messages

`svnd` has no transaction subcommand and no query subcommand. On the devnet the council transactions are built, signed, and broadcast by the Go devnet tests, with the seat keys and the authority key that `svnd devnet init` writes under `accounts/` in the devnet home (`accounts/seat-1.hex` to `accounts/seat-4.hex`, `accounts/authority.hex`). `TestDevnetLeaseLifecycle` executes a governed limit change this way, and `TestDevnetValidatorLifecycle` executes validator power changes. A broadcast acknowledgement is not a committed change: read the committed result with `Query/Policy` or `Query/Governance` at a height.

The DEC-008 devnet row has review delay 0 unless a Go test sets `devnet.Options.CouncilDelaySeconds`; `svnd devnet init` does not expose that option. The delay in use is published by `Query/Governance` in `devnet_test_parameters`, labelled `PROPOSED_TEST_PARAMETER` (`TestGovernanceQueryDevnetParametersAreLabelled`).

After any start of a node, `CheckTx` rejects a correctly signed transaction from a normal account with sdk code 4 until that node commits its first block (decision request DR-10-41; `TestUpgradeWindowCheckTxAndQueries`). Wait for one committed block after a start before broadcasting to that node.

## Reading the committed result

`scripts/devnet/walk-runbooks.py query` reads `svn.policy.v1.Query/Policy` or `svn.authority.v1.Query/Governance` through CometBFT `abci_query` and prints the response as protobuf JSON (field names in lowerCamelCase, the response's fields under `state`). With no `--height` it reads the latest committed height; it prints the height it read first.

```bash
.venv/bin/python scripts/devnet/walk-runbooks.py query policy --rpc "$NODE_RPC"
.venv/bin/python scripts/devnet/walk-runbooks.py query policy --rpc "$NODE_RPC" --height 40
.venv/bin/python scripts/devnet/walk-runbooks.py query governance --rpc "$NODE_RPC"
```

`Query/Policy` returns the policy version, the safety caps, the blocked decision rows, the value in force of each of the five governed limits in key order, the pending governed changes, the pending upgrade plan, and the app version (`TestPolicyQueryThroughABCI`). `Query/Governance` returns the authority state, a page of proposals, and the devnet test parameters (`TestGovernanceQueryThroughABCI`).

## Governed limit change

The payload is `GovernedLimitChange{limit_key, value, activation_height}` and the parameter key is the limit key.

- The key must be one of the five keys of `governed_limit_caps` and the value must be in 1..ceiling. Anything else is `ERROR_CODE_BOUNDS` at propose (deviation D-1) and again at execute (`TestInvalidBounds`, `TestEveryGovernedKeyAtFloorAndCeiling`). The safety caps (transaction bytes, gas per transaction) are not governed keys and no payload changes them (`TestSafetyCapsNotGovernable`).
- `activation_height` 0: the execute writes the value at once and deletes any pending change of that key.
- `activation_height` a: a must be above the height of the block that proposes it and of the block that executes it, and at most 1,000,000 blocks above it (`MaxActivationLeadBlocks`, PROPOSED; using it for limit activations is an interim choice, DR-10-15). At or below the height is `ERROR_CODE_OUT_OF_RANGE`; above the lead is `ERROR_CODE_BOUNDS`. A refused execute leaves the proposal `QUEUED`. The execute stores a pending change and replaces any pending change of that key.
- x/policy applies a pending change in BeginBlock of height a, before any transaction and before lease expiry (deviation D-8). Block a-1 reads the old value and block a the new one (`TestActivationTiming`, `TestActivationGovernsSameBlockExpiryD8`).
- The policy version moves at execute, not at activation. During the window a lease created at a-1 records the new policy version while the old value still governs it (`TestLeaseAtActivationMinusOne`).
- A later immediate change of the same key deletes the pending change; the latest executed decision for a key wins (`TestImmediateChangeSupersedesPending`).

Verify a scheduled change with two reads: at height a-1, `state.governedLimits` shows the old value and `state.pendingLimitChanges` lists the change; at height a, `state.governedLimits` shows the new value and the pending change is gone.

## Council seat key replacement

The payload is `CouncilSeatChange{seat_id, new_public_key}` and the parameter key is `seat-<seat_id>`. The council keeps four seats; no payload removes a seat (DEC-008 approved four).

- At propose the seat id must be on the id pattern and the key must be 32 bytes (`ERROR_CODE_BOUNDS`).
- At execute: an absent seat is `ERROR_CODE_NOT_FOUND`; a key equal to another seat's key, a key whose account address holds a submitter role, and a key equal to a current or pending validator consensus key are `ERROR_CODE_DUPLICATE_ID`; a retired key or the seat's own key is `ERROR_CODE_CONFLICT`.
- Success replaces the key, keeps the seat id, retires the old key for good, and creates the auth account of the new key's address in the same write (`TestCouncilSeatCreatesTheNewKeyAccount`). The ante stops admitting the old key's account. Approvals stay deduplicated by seat id.
- The policy version bump makes every proposal in flight unexecutable, including proposals the old key approved (`TestRemovedCouncilSeat`, `TestReplacedSeatApprovalsBecomeStale`). Propose them again.
- Use a fresh devnet key. Goal 10 rotates no live key; generation and custody of seat keys are owner decisions (DEC-001).

## Scheduling an upgrade

The payload is `SoftwareUpgradeChange{name, height, release_digest}` and the parameter key is `upgrade`. Under the owner's Q2 ruling, "Council plan, fail closed (Recommended)", an executed council payload is the only way to schedule an upgrade. The production plan source of `svnd` reads that plan from x/policy (`upgrade.CouncilPlans`).

- The name must be on the id pattern (`ERROR_CODE_BOUNDS`). The release digest must name SHA-256 (`ERROR_CODE_MALFORMED`) and hold 32 bytes (`ERROR_CODE_BOUNDS`).
- The height must be at least 100 and at most 1,000,000 blocks above the block that proposes it, and again above the block that executes it (`MinActivationLeadBlocks` and `MaxActivationLeadBlocks`, both PROPOSED). Below is `ERROR_CODE_OUT_OF_RANGE` and the proposal stays `QUEUED`; above is `ERROR_CODE_BOUNDS`.
- A name that already applied is `ERROR_CODE_DUPLICATE_ID`. A name that an earlier plan used with another release digest is `ERROR_CODE_CONFLICT`, whether that plan is pending, cancelled, or replaced: a plan name is never reused with a different release digest.
- Success stores the plan (name, height, release digest, proposal id) and replaces any pending plan. The same name with the same digest may be scheduled again.
- The lead is counted in blocks, not time. On the devnet blocks come as fast as the consensus timeouts in `chain/node/start.go` allow, so the minimum lead is a floor the executor cannot shorten, not a notice period. The single `AUTHORITY` executor chooses when a queued plan executes inside the window.
- The release digest is information for operators. The chain cannot tell which binary a node runs. Each operator compares it with the reviewed release manifest before installing ([source-and-digest-review.md](source-and-digest-review.md), [operator-installation.md](operator-installation.md)).

Verify with `Query/Policy`: `state.upgradePlan` holds the name, the height, and the digest.

## Cancelling an upgrade

The payload is `CancelUpgradeChange{name}` and the parameter key is `upgrade`. It needs the same three approvals and review delay. The council's executed cancel is the only way to remove a plan: no node setting, flag, or local command removes or moves one. The non-test code of `chain/upgrade`, `chain/node`, and `chain/cmd/svnd` names no such option (`TestNoSkipOrHaltHeight`), and only x/authority and x/policy call the plan writers (`TestPlanWritersHaveNoNodeLocalCaller`).

- A name that is not the pending plan's name is `ERROR_CODE_NOT_FOUND`.
- The cancel must commit before the plan height. At the plan height the chain either applies the upgrade or halts, and a halted chain commits no cancel.
- After a cancel, `Query/Policy` shows no plan, the old binary runs through the old plan height, and a binary built for the cancelled upgrade is refused at start (`TestCancelBeforeHeightRemovesPlan`).

## What an operator must not expect

- No council payload approves a blocked decision, fills the approved council, or sets a quorum-loss behaviour (`TestNoPayloadApprovesDecision`).
- An executed plan does not install anything. Each operator decides whether to install the release ([operator-installation.md](operator-installation.md)).
- On the devnet one operator holds all four seats, the executor key, and all four nodes. These steps show the mechanism, not an independent council.
