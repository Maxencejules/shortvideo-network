# Authority authorization map

Reviewer note for Goal 06. This is not an audit. Public launch is disabled. There is no token, fee denomination, or staking module.

`x/authority` is the only module that builds `abci.ValidatorUpdate`. `InitialUpdates` and `Changeset` both call one constructor. EndBlock on every other module returns no validator updates. `MsgCreateValidator` is not registered. A direct admit, disable, or force-withdraw message is not registered (DEC-009).

## Who may write

| Action | Signer | What is checked in the committed handler |
| --- | --- | --- |
| `MsgProposeChange` | A devnet test seat | Seat public key derives the signer address. Payload digest is SHA-256 of SVN-JCS-1. Proposal id is new. Base policy version matches. Expiry is in the future. The seat holds fewer than 32 open proposals on the current base policy version. |
| `MsgApproveChange` | A devnet test seat, once | The same seat again is `ERROR_CODE_REPLAY`, also after its key rotates: approvals are kept by seat id. Two approvals leave the proposal `OPEN`. The third, of four, queues it. |
| `MsgExecuteChange` | `SUBMITTER_ROLE_AUTHORITY` | Council signatures are not replaced by this signature. The proposal must be `QUEUED`, unexpired, and past the devnet delay. |
| `RegisterProvider` | None in Goal 06 | Not a committed handler. The router returns `ERROR_CODE_FORBIDDEN` until Goal 07 owns the provider registry. |
| `RotateProviderKey` | None in Goal 06 | Not a committed handler. A signed rotation decodes, because `svn.types.v1.InnerSignature` is registered, and the router returns `ERROR_CODE_FORBIDDEN`. `UpdateProvider` and `DrainProvider` are closed the same way. |
| Council seat rotation | Not a transaction | `RotateCouncilSeat` is engine-only. It changes the seat key and retires the old one. A retired key cannot return as a seat key. When the old seat address is a submitter, the rotation moves that row and its ante admission row to the new seat address, so the retired key's account is no longer admitted. It does not create an auth account for the new address. It is not an RPC and not a message. |
| Service account rotation | Not a transaction | `RotateServiceSubmitter` is engine-only. It changes an admitted service address and its ante admission row, and retires the old address. |

The ante decorator verifies the SDK signature and the account sequence during FinalizeBlock, and again during CheckTx. The ante admits a signer only when the authority store has an admission row for it. Genesis writes one row per submitter. The authority handler checks the gas limit, the transaction size, and that the message signer is 20 bytes. It does not verify the signature bytes a second time. A failed message does not store a business id. The same business id and payload is idempotent. A different payload is `ERROR_CODE_REPLAY`.

Council approvals and the provider admit and disable payloads stay council-only (DEC-009). No Goal 06 message creates a provider row, so an admit or disable payload executes to `ERROR_CODE_NOT_FOUND` and the proposal stays `QUEUED`. Force-withdraw is `ERROR_CODE_NOT_FOUND` until the content registry exists.

## Council seat accounts

A seat account exists in auth, and passes the ante, only because genesis lists the seat address as a `BROADCASTER` submitter. That row is the only admission path. No Goal 06 handler accepts a `BROADCASTER` signer, so a seat key can send council messages and nothing else. A seat-signed execute is `ERROR_CODE_FORBIDDEN`. The engine-only seat rotation moves the row and its admission row with the key. Goal 08 adds `BROADCASTER` lease handlers, so a governance key could then relay lease messages. DR-06-06 asks for a seat-only admission path before that merges.

## What the approvals are not

Council approvals are application records. They are not CometBFT prevotes or precommits. A governance approval does not sign a block. A block signature does not approve a proposal.

The approved shape is four seats and three approvals (DEC-008). The spec machine's three-seat, threshold-two fixture is not used on this chain. Production council keys are not named: `approved_council` stays empty, and a chain that is not a `shortvideo-dev-` devnet with the test parameter is `ERROR_CODE_DECISION_BLOCKED`.

## When a validator change is effective

`ExecuteChange` schedules a `ValidatorPowerChange`. It does not change the set inside that transaction. The activation point must be a height or a chain time strictly after the execution block. Height and time are not one timeline (DEC-012): a mix is `ERROR_CODE_CONFLICT` and the proposal stays `QUEUED`.

EndBlock of the activation block applies the due changes and returns a changeset. CometBFT v0.40 `state/execution.go` `updateState` applies that changeset to the next validator set and records `LastHeightValidatorsChanged = height + 2`. The comment in that function is "only applies to the next next height." The signing set at the activation height is still the old set. The new set signs the block two heights later.

Genesis pending entries follow the same rules. InitChain projects them on the genesis set and requires each to activate at or after the first block. A genesis that fails is refused before any module state is written.

## Store layout

The authority store keeps one key per object. A message reads and writes a fixed set of keys: the config key, the keys its own fields name, the policy version for a propose or an execute, and the proposer's open count for a propose, a keep, or an execute. An execute of a validator payload also scans the pending prefix, at most 128 entries.

| Prefix | Holds |
| --- | --- |
| `0x01` | Config: validators, submitters, seats, blocked decisions, devnet test parameters. Deterministic protobuf. |
| `0x02` | One admission row per submitter address. The ante reads it. |
| `0x03` | One scheduled validator change per key, ordered by activation kind and point. |
| `0x04` | Policy version. Absent means 1. |
| `0x10`-`0x17` | Proposal records, business ids, executed parameter digests, governed limits, open counts per seat and base policy version, retired council keys, retired service addresses, provider rows. |

`chain/x/authority/README.md` has the full key table and the read and write set of each message.

## Rejection

`ERROR_CODE_FORBIDDEN` marks the proposal `REJECTED` and does not schedule the change. Other execution errors leave the proposal `QUEUED` and do not store the business id. A rejected message leaves the whole authority store byte-identical: every key under every prefix. The engine writes only on a success or keep path, and the keeper runs it on a cache branch it drops on a reject. The app test compares a length-prefixed dump of every key with `bytes.Equal` after each of 37 reject classes. Expiry is not in that set: the spec marks the proposal `EXPIRED` and keeps that mark.

SDK v0.55 discards the message branch when a handler returns an error, and it commits the ante branch first. A spec `keep` transition (expiry, or forbidden rejection) is saved and the handler returns ABCI code 0. The protocol code is the `code` attribute of the `authority_result` event. The payload is not applied. `docs/protocol.md` says ABCI 0 is success and does not list `authority_result`. That is a recorded protocol deviation, DR-06-05. A rejected message's authority bytes are unchanged. The account sequence can still advance when ante succeeded. That sequence is not an authority decision.

## Removal bind

A due removal whose consensus key does not match the key the validator holds is a halt, not a drop. EndBlock returns `ERROR_CODE_CONFLICT`, the block is not committed, and the pending entry remains. The next attempt fails the same way. Liveness is not in the application hash (DEC-011).

## Quotas

Each transaction's gas limit must be from 1 through `safety_max_gas_per_tx`. The byte length must be within `safety_max_tx_bytes`. Each authority handler charges 50,000 gas, then the store charges for each key it reads and writes (SDK KV gas config). The keys a message touches are the fixed set in the store layout above, so its gas does not grow with past proposals. An execute of a validator payload also reads every pending entry (at most 128), so its gas is bounded but depends on how many changes are scheduled. The app test measures the same propose before and after 300 terminal proposals and gets the same gas.

A council seat may hold 32 open proposals (`OPEN` or `QUEUED`) on the current base policy version. The 33rd is `ERROR_CODE_BOUNDS`. One seat cannot use the slots of the others. Executed, expired, and rejected proposals do not count. A proposal on an older base can never execute (`ERROR_CODE_STALE_VERSION` or `ERROR_CODE_CONFLICT`), so it stops counting when any execute bumps the version. A slot on the current version is freed when its proposal executes, is rejected, or is marked expired. An approve marks an expired `OPEN` proposal. Only an authority execute marks an expired `QUEUED` one: an approve of a `QUEUED` proposal is `ERROR_CODE_CONFLICT` before the expiry check, as in the spec. A current-version proposal that is never approved, or whose execute fails with any code other than `ERROR_CODE_FORBIDDEN` (for example `ERROR_CODE_NOT_FOUND`, `ERROR_CODE_OUT_OF_RANGE`, `ERROR_CODE_CONFLICT`, `ERROR_CODE_BOUNDS`, `ERROR_CODE_DUPLICATE_ID`), keeps its slot until an execute of any proposal bumps the version, or until it is marked expired after its own expiry. The proposer may set that expiry as late as `unix_seconds_max`. So a seat can fill its own 32 slots with such proposals until the council executes something else; it cannot fill another seat's slots.

At most 128 validator changes may be scheduled. The 129th execute is `ERROR_CODE_BOUNDS` and the proposal stays `QUEUED`. One EndBlock therefore applies at most 128 entries. No message cancels a scheduled change. A council majority that schedules 128 changes with far activation points blocks every later validator change, including the replacement of a compromised key, until they apply. Activation heights go up to 2^53-1.

Both numbers are PROPOSED, not approved policy: DR-06-03 and DR-06-04. The spec machine has neither cap. There is no fee amount and no coin.

Proposal ids, business ids, and the executed set are kept for the life of the chain, as in the spec. That is replay protection. Storage grows by one bounded record per accepted message. Gas does not.

## Export

`App.Export` refuses while the authority store holds state the frozen genesis proto has no field for: the policy version, proposal records, business ids, the executed set, governed limits, open counts, retired keys, or provider rows. The error starts with `export refused:` and names the first such prefix. Dropping that state would lose replay protection and council state on import. It also refuses a genesis that would not validate, such as unequal validator powers after a power change. The module `ExportGenesis` has no error result, so it panics with the same error if it is called directly. A chain with only config, admission rows, and pending entries exports, imports, and exports again byte for byte. Restart from export with council history is not supported. DR-06-01 and DR-06-02 ask for the owner decision.

## Decision requests

These are open questions for the owner. The values Goal 06 uses are PROPOSED or recorded deviations, not approved policy.

- **DR-06-01 Restart from export.** The frozen authority genesis proto has no field for the policy version, proposal records and payloads, proposer and approver seats, business ids, the executed set, governed limits, open counts, retired council and service keys, or provider rows. Should protocol v0 gain genesis fields for them (a protocol change), or is restart from export unsupported while any of that state exists? Goal 06 implements the second: `App.Export` refuses and names the store prefix, and the module export panics if it is called directly. A fresh chain with no such state round-trips byte for byte.
- **DR-06-02 Genesis powers versus runtime powers.** Genesis requires equal validator powers. DEC-011 lets the council reach unequal powers (four or more validators, `max_power * 3 < total_power`). Should an imported genesis accept any set that meets the DEC-011 invariant, or keep equal powers? Goal 06 keeps equal powers, and `App.Export` refuses a set that genesis would reject.
- **DR-06-03 Open-proposal quota.** The spec has no proposal cap. Goal 06 caps open proposals at 32 per council seat on the current base policy version, and returns `ERROR_CODE_BOUNDS` for the 33rd. What frees a slot, and what does not, is in Quotas above. In particular, a seat's own never-approved or unexecutable proposals keep its slots until an execute bumps the version or they are marked expired, and their expiry may be as late as `unix_seconds_max`. Approve 32 per seat, another number, no cap, or a cap with a maximum expiry horizon (a further PROPOSED number)?
- **DR-06-04 Pending validator-change cap.** The spec has no cap on scheduled validator changes. Goal 06 caps them at 128, the protocol's `safety_max_expiry_steps_per_block`, so one EndBlock never applies more than 128. The 129th execute is `ERROR_CODE_BOUNDS` and the proposal stays `QUEUED`. No message cancels a scheduled change, so 128 far-future entries block every later validator change, including a compromised-key replacement, until they apply. Approve 128, another number, or a cap with a maximum activation horizon?
- **DR-06-05 Keep outcomes on the wire.** `docs/protocol.md` says ABCI 0 is success, and its event list has no `authority_result`. SDK v0.55 discards a message's writes when its handler returns an error, but the spec keeps the `EXPIRED` mark (approve or execute after expiry) and the `REJECTED` mark (an execute that is `ERROR_CODE_FORBIDDEN`). Goal 06 commits those with ABCI code 0 and puts the protocol code in the `code` attribute of an `authority_result` event. Accept this as a recorded protocol deviation and add `authority_result` to the event list in the next protocol revision, or require another mechanism?
- **DR-06-06 Council seat admission.** Seat accounts get auth accounts and pass the ante only because genesis lists them as `BROADCASTER` submitters. Goal 06 has no `BROADCASTER` handler. Goal 08 adds `BROADCASTER` lease handlers, and a governance key could then relay lease messages (REQ-SEC-002 asks that governance and service keys stay distinct). Add a seat-only admission path before Goal 08 merges, or accept the shared role?

## No admin finality

No query or HTTP route fabricates a proposal, a vote, or a validator update. Broadcast acknowledgement is not committed success. Stopping a process or closing its port is a local action. It does not write a committed decision and it does not change finality.
