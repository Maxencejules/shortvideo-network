# Validator lifecycle

Development procedure for the permissioned set. This is not a production runbook. Public launch is disabled. Devnet keys are generated in the home directory and are not council keys for any real operator.

## Ordinary admission

A new validator is a council proposal whose payload is `ValidatorPowerChange` with positive power, a distinct consensus public key, a node id, a reason digest, and an activation height or chain time strictly after execution. Four devnet seats and three approvals queue it. The authority submitter executes it. The set changes in EndBlock at the activation point and the new key signs blocks two heights later.

Provider admission is a different record. `RegisterProvider` is a committed handler and creates `APPLIED`. `AdmitProviderChange` through the council moves it to `ADMITTED` when the devnet DEC-002 test role is present. That does not change voting power. Service admission is the genesis submitter role `ADMITTED_SERVICE`. `RotateProviderKey` is a committed handler and changes the provider signing key only. Council seat rotation and service account rotation are keeper operations, not transactions. None of those rotate a consensus key.

## Ordinary removal

Removal is the same proposal with power zero and the consensus key the validator holds now. The projected set must keep at least four validators, positive powers, and `max_power * 3 < total_power`. A set that fails is `ERROR_CODE_FORBIDDEN` and the proposal is `REJECTED`. A removal that names some other key is `ERROR_CODE_CONFLICT`. If that mismatch is only discovered when the change is due, the block halts and the entry is not dropped.

## Emergency local endpoint disablement

A compromised endpoint is stopped on the machine that runs it: stop the process, or stop listening. That is not a transaction. It does not approve a proposal, remove voting power, or rewrite a committed block. The key remains in the validator set until a council removal activates. DEC-009 did not select a single-signature or emergency-key path.

## Loss of quorum

Quorum is voting power, not the number of processes. More than two thirds of the current power must be online for the network to produce a block. One of four equal powers can be down. More than one third offline means the environment produces no block. The application hash does not record who is down.

`QuorumLossBehavior` stays `UNSET`. There is no on-chain election, freeze, or root key that recovers a stalled chain. Recovery is bringing enough voting power back online, then waiting for two blocks after those processes are up. Do not delete a genesis to recover. Governance cannot execute while the chain is not committing blocks.

## What an operator must not expect

An RPC broadcast is not a committed change. A local admin request cannot invent a council approval or replace CometBFT finality. The devnet uses four seats and three approvals, with disposable keys. It is not a second policy and it does not name production council members.
