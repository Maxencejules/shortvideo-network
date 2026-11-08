# Validator lifecycle

Development procedure for the permissioned set. This is not a production runbook. Public launch is disabled. Devnet keys are generated in the home directory and are not council keys for any real operator.

## Ordinary admission

A new validator is a council proposal whose payload is `ValidatorPowerChange` with positive power, a distinct consensus public key, a node id, a reason digest, and an activation height or chain time strictly after execution. Four devnet seats and three approvals queue it. The authority submitter executes it. The set changes in EndBlock at the activation point and the new key signs blocks two heights later.

Provider admission is a different record. `RegisterProvider` is an `x/providers` committed handler and creates `APPLIED`. `AdmitProviderChange` through the council moves it to `ADMITTED` when the devnet DEC-002 test role is present. That does not change voting power. Service admission is the genesis submitter role `ADMITTED_SERVICE`. `RotateProviderKey` is an `x/providers` committed handler and changes the provider signing key only. Since goal 10 a council seat key is replaced by an executed council `CouncilSeatChange` payload ([council-changes.md](council-changes.md#council-seat-key-replacement)); service account rotation is an engine operation, not a transaction. None of those rotate a consensus key.

Council seats send their messages from their own accounts. Genesis gives each seat address an auth account and no submitter row. The ante admits a seat account from the seat list in the authority config and lets it sign only propose and approve (owner ruling DR-06-06). Genesis validation refuses a seat address in the submitter list with any role (DR-07-11 option 2).

## Ordinary removal

Removal is the same proposal with power zero and the consensus key the validator holds now. The projected set must keep at least four validators, positive powers, and `max_power * 3 < total_power`. A set that fails is `ERROR_CODE_FORBIDDEN` and the proposal is `REJECTED`. A removal that names some other key is `ERROR_CODE_CONFLICT`. If that mismatch is only discovered when the change is due, the block halts and the entry is not dropped.

## Emergency local endpoint disablement

A compromised endpoint is stopped on the machine that runs it: stop the process, or stop listening. That is not a transaction. It does not approve a proposal, remove voting power, or rewrite a committed block. The key remains in the validator set until a council change activates. DEC-009 did not select a single-signature or emergency-key path.

## Who pauses admissions

Only the council, by not approving admissions. There is no pause switch, and no single message pauses admissions. Apart from genesis and the changes genesis schedules, a validator joins only through an executed council `ValidatorPowerChange`. A provider is admitted or disabled only through an executed council `AdmitProviderChange` or `DisableProviderChange` (DEC-009): the execute calls `x/providers`, which admits an `APPLIED` provider of the `devnet-test-only` group when the devnet DEC-002 test role is present, and disables an `ADMITTED` or `DRAINING` one. An unknown provider id is `ERROR_CODE_NOT_FOUND` and the proposal stays `QUEUED`. Submitter and service accounts, and their ante admission rows, come from genesis. The engine-only seat and service rotations can move a row to a new address, and no message adds one. DEC-009 selected no single-signature or emergency path. A local operator can stop only its own endpoint, as in the previous section. That pauses nothing for anyone else.

## Compromised or lost consensus key

On the four-validator devnet a direct removal is `ERROR_CODE_FORBIDDEN`: three validators would remain. Replace a compromised key with one `ValidatorPowerChange` that keeps the node id and the power and names a new consensus key. The set changes at the activation point, and the new key signs blocks two heights later. The old key signs until then.

The replacement needs a free scheduling slot. At most 128 validator changes can be scheduled, and no message cancels one. If 128 are already scheduled, the execute is `ERROR_CODE_BOUNDS` and the replacement waits until some of them apply (PROPOSED cap, DR-06-04).

Losing one third or more of the voting power for good stops the chain: what remains is not more than two thirds. There is no on-chain path back: `QuorumLossBehavior` is `UNSET` (DEC-008), and governance cannot execute while no block commits. Recovery from permanent quorum loss is out of scope for this beta (decision request DR-10-11): a new network would be a new chain id that keeps and publishes this chain's history, and it needs its own owner decision ([failed-upgrade.md](failed-upgrade.md#permanent-loss-of-quorum)).

## Loss of quorum

Quorum is voting power, not the number of processes. More than two thirds of the current power must be online for the network to produce a block. One of four equal powers can be down. One third or more of the power offline, so that the online power is not more than two thirds, means the environment produces no block. The application hash does not record who is down.

`TestDevnetQuorumFollowsVotingPower` (`chain/devnet/power_quorum_test.go`) observes this on a single-machine, multi-process devnet: four validator processes on loopback, not independent operators. `devnet.Options.Power` gives the four genesis validators power 10; `svnd devnet init` does not expose that option. Two council proposals add v5 with power 1 and no process, and lower v3 to power 2. The CometBFT set is then v1:10, v2:10, v3:2, v4:10, v5:1, total 33, and CometBFT needs 23 for a block (total*2/3+1). Two cases leave the same count online, three of five validators:

| Case | Offline | Online power | Count rule | Power rule | Observed |
| --- | --- | --- | --- | --- | --- |
| X | v3 (2), v5 (1) | 30 of 33: v1, v2, v4 signed the commits | stall | progress | blocks commit; from the second block after the stop, their commits are signed by v1, v2 and v4 and not by v3 or v5 |
| Y | v1 (10), v5 (1) | 22 of 33, exactly two thirds: v2, v3, v4 voted at the stuck height | stall | stall | no block for a 30-second window and six more one-second samples; every running node holds votes from v2, v3 and v4 at the height that does not commit, and none from v5; the test allows one block already in flight at the stop, and its commit must carry v1's signature |

The test reads the powers from the validator set over CometBFT RPC. It observes the online validators instead of taking them from the processes it runs. In X they are the signers of the commits after the stop. In Y they are the validators whose prevote or precommit the running nodes hold at the height that does not commit, read from CometBFT's `/dump_consensus_state`, which CometBFT marks unstable. In both cases they must be exactly the validators whose process runs. The outcome comes from committed blocks. Each case ends with the stopped process restarted, every node committing again, and every validator process signing at least one commit after that. The `model_quorum` lines of `TestDevnetValidatorLifecycle` are a model applied to an RPC set, not an observation.

`QuorumLossBehavior` stays `UNSET`. There is no on-chain election, freeze, or root key that recovers a stalled chain. Recovery is bringing enough voting power back online, then waiting for two blocks after those processes are up. Do not delete a genesis to recover. Governance cannot execute while the chain is not committing blocks.

## Old devnet homes

There is no migration from an earlier goal: no live chain exists. Since goal 10 the load gate refuses, at start, every committed devnet home that a binary from goals 05 to 09 wrote, including a plain four-validator devnet: `svnd start` exits 1 with `load gate at height L refused this binary (row 6): no stored app version: the state was written by a binary from before goal 10 (goals 05 to 09), which this binary cannot read or migrate; initialize a new home` (`TestAppRefusesScaffoldLayout`). Goals 07 and 10 also added fields to proposal records, so such a record does not decode. Re-initialize every such home: delete it and run `svnd devnet init` again. That applies to disposable devnet homes from earlier goals only. It is not a recovery procedure for a running chain ([failed-upgrade.md](failed-upgrade.md)).

The authority store layout also changed in the Goal 06 fix round. The round-2 code wrote a side document when an authority message succeeded or was kept, and when a validator change applied. A home that passed the layout check and still held that side document halts in EndBlock with an error that says to re-initialize it, and export refuses it. A goal-10 binary refuses such a home earlier, at the load gate.

## How operators choose upgrades

The council schedules an upgrade plan by an executed council payload ([council-changes.md](council-changes.md#scheduling-an-upgrade)); it cannot install a binary on any node. Each operator reviews the release and decides whether to install it before the plan height ([operator-installation.md](operator-installation.md#how-an-operator-chooses)). A node without the binary stops at the plan height, and the chain makes no block there while more than one third of the voting power has not installed it. The swap and the failure cases are in [planned-halt-and-restart.md](planned-halt-and-restart.md) and [failed-upgrade.md](failed-upgrade.md).

## What an operator must not expect

An RPC broadcast is not a committed change. A local admin request cannot invent a council approval or replace CometBFT finality. The devnet uses four seats and three approvals, with disposable keys. It is not a second policy and it does not name production council members.
