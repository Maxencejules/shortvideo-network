# Authority authorization map

Reviewer note for Goal 06. This is not an audit. Public launch is disabled. There is no token, fee denomination, or staking module.

`x/authority` is the only module that builds `abci.ValidatorUpdate`. `InitialUpdates` and `Changeset` both call one constructor. EndBlock on every other module returns no validator updates. `MsgCreateValidator` is not registered. A direct admit, disable, or force-withdraw message is not registered (DEC-009).

## Who may write

| Action | Signer | What is checked in the committed handler |
| --- | --- | --- |
| `MsgProposeChange` | A devnet test seat | Seat public key derives the signer address. Payload digest is SHA-256 of SVN-JCS-1. Proposal id is new. Base policy version matches. Expiry is in the future. |
| `MsgApproveChange` | A devnet test seat, once | Same seat again is `ERROR_CODE_REPLAY`. Two approvals leave the proposal `OPEN`. The third, of four, queues it. |
| `MsgExecuteChange` | `SUBMITTER_ROLE_AUTHORITY` | Council signatures are not replaced by this signature. The proposal must be `QUEUED`, unexpired, and past the devnet delay. |
| `RegisterProvider` | `SUBMITTER_ROLE_ADMITTED_PROVIDER` | Committed handler. Creates `APPLIED`. It does not change the validator set. Update and drain stay closed until Goal 07. |
| `RotateProviderKey` | The provider account, plus an ed25519 signature on domain `SVN-PROVIDER-ROTATE-1` | Committed handler. Changes the provider signing key only. Previous public keys are kept. |
| Council seat rotation | Not a transaction | `RotateCouncilSeat` changes the seat key and keeps the old one. It is not an RPC and not a message. |
| Service account rotation | Not a transaction | `RotateServiceSubmitter` changes an admitted service address and keeps the old one. |

The ante decorator verifies the SDK signature and the account sequence during FinalizeBlock, and again during CheckTx. The authority handler checks the gas limit, the transaction size, and that the message signer is 20 bytes. It does not verify the signature bytes a second time. A failed message does not store a business id. The same business id and payload is idempotent. A different payload is `ERROR_CODE_REPLAY`.

## What the approvals are not

Council approvals are application records. They are not CometBFT prevotes or precommits. A governance approval does not sign a block. A block signature does not approve a proposal.

The approved shape is four seats and three approvals (DEC-008). The spec machine's three-seat, threshold-two fixture is not used on this chain. Production council keys are not named: `approved_council` stays empty, and a chain that is not a `shortvideo-dev-` devnet with the test parameter is `ERROR_CODE_DECISION_BLOCKED`.

## When a validator change is effective

`ExecuteChange` schedules a `ValidatorPowerChange`. It does not change the set inside that transaction. The activation point must be a height or a chain time strictly after the execution block. Height and time are not one timeline (DEC-012): a mix is `ERROR_CODE_CONFLICT` and the proposal stays `QUEUED`.

EndBlock of the activation block applies the due changes and returns a changeset. CometBFT v0.40 `state/execution.go` `updateState` applies that changeset to the next validator set and records `LastHeightValidatorsChanged = height + 2`. The comment in that function is "only applies to the next next height." The signing set at the activation height is still the old set. The new set signs the block two heights later.

## Rejection

`ERROR_CODE_FORBIDDEN` marks the proposal `REJECTED` and does not schedule the change. Other execution errors leave the proposal `QUEUED` and do not store the business id. Unauthorized, malformed, bad-signature, replay, stale, early, and wrong-role messages do not change the authority state key or the side document. The comparison is `bytes.Equal` on the length-prefixed snapshot of both. Expiry is not in that set: the spec marks the proposal `EXPIRED` and keeps that mark.

SDK v0.55 discards the message branch when a handler returns an error, and it commits the ante branch first. A spec `keep` transition (expiry, or forbidden rejection) is saved and reported on the `authority_result` event. The payload is not applied. A rejected message's authority bytes are unchanged. The account sequence can still advance when ante succeeded. That sequence is not an authority decision.

## Removal bind

A due removal whose consensus key does not match the key the validator holds is a halt, not a drop. EndBlock returns `ERROR_CODE_CONFLICT`, the block is not committed, and the pending entry remains. The next attempt fails the same way. Liveness is not in the application hash (DEC-011).

## Quotas

Each transaction's gas limit must be from 1 through `safety_max_gas_per_tx`. The byte length must be within `safety_max_tx_bytes`. Proposals, pending power changes, and provider rows are capped at 128. The handler consumes a fixed gas amount. There is no fee amount and no coin.

## No admin finality

No query or HTTP route fabricates a proposal, a vote, or a validator update. Broadcast acknowledgement is not committed success. Stopping a process or closing its port is a local action. It does not write a committed decision and it does not change finality.
