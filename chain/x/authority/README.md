# chain/x/authority

Validator membership and council changes (Goal 06). This module is the only one that returns validator updates. `InitialUpdates` returns the genesis set; EndBlock returns a changeset when a scheduled change is due.

## Messages

| Message | Signer | Effect |
| --- | --- | --- |
| `MsgProposeChange` | A devnet council seat | Opens a proposal with a validator power change, a governed limit, a provider admit or disable, or a force-withdraw payload. |
| `MsgApproveChange` | A devnet council seat | Adds the seat's approval. The third of four seats queues the proposal. |
| `MsgExecuteChange` | The `AUTHORITY` submitter | Applies a queued proposal: schedules a validator change, sets a governed limit, or changes a provider row. |

`RegisterProvider` and `RotateProviderKey` are `ERROR_CODE_FORBIDDEN` at the router until Goal 07. No message creates a provider row, so an admit or disable payload executes to `ERROR_CODE_NOT_FOUND`. Council seat rotation and service account rotation are engine-only (`RotateCouncilSeat`, `RotateServiceSubmitter`); no message reaches them.

`docs/architecture/authority-authorization.md` is the authorization map. `docs/operations/validator-lifecycle.md` is the operator procedure.

## Store layout

One prefix byte, then the fields that name the object. Every id in a key matches `^[a-z0-9][a-z0-9._-]{0,63}$` and is checked before any write.

| Key | Value | Written by | Read by |
| --- | --- | --- | --- |
| `0x01` | Deterministic protobuf `GenesisState`: validators, submitters, devnet test seats, quorum loss `UNSET`, blocked decisions, devnet test parameters. Proposals, pending entries, and the approved council are always empty here. | InitGenesis; EndBlock when the set changes; engine-only rotations | every message; EndBlock when any entry is pending; export |
| `0x02 \|\| addr(20)` | One byte, the submitter role | InitGenesis; engine-only service and seat rotations | ante (`Admitted`); export |
| `0x03 \|\| kind(1) \|\| point(8, big-endian) \|\| proposal_id` | Deterministic protobuf `PendingValidatorPower`. Kind `0x01` is a height, `0x02` a unix time, so key order is the spec activation order. | Execute of a validator payload; InitGenesis; EndBlock deletes applied entries | Execute of a validator payload (projection); EndBlock; export |
| `0x04` | Policy version, 8 bytes big-endian. Absent means 1. | Execute | Propose; Execute; export check |
| `0x10 \|\| proposal_id` | Proposal record, JSON with a fixed field order | Propose; Approve; Execute | Propose (duplicate check); Approve; Execute |
| `0x11 \|\| business_id` | 32-byte business digest | each successful message that is not an idempotent retry | each message (its own id) |
| `0x12 \|\| parameter_key` | 32-byte payload digest of the last executed change | Execute | Execute, stale branch only |
| `0x13 \|\| limit_key` | 8-byte big-endian value | Execute of a governed limit | not read in Goal 06 |
| `0x14 \|\| base_policy_version(8, big-endian) \|\| seat_id` | 8-byte big-endian count of the seat's `OPEN` or `QUEUED` proposals on that base version. Deleted at 0. | Propose (+1); every transition out of `OPEN` or `QUEUED` (-1, at the record's base) | Propose (its own seat, current version only); those transitions |
| `0x15 \|\| old_pub(32)` | Seat id | `RotateCouncilSeat` | that rotation; export check |
| `0x16 \|\| old_addr(20)` | `0x01` | `RotateServiceSubmitter` | that rotation; export check |
| `0x17 \|\| provider_id` | Provider row, JSON | admit and disable payloads, only when a row exists | admit and disable payloads |
| `0x20` | The round-2 side document. Never written. | none | EndBlock guard; export check |

## What each message reads and writes

| Message | Reads | Writes on success | Writes on a keep outcome |
| --- | --- | --- | --- |
| Propose | `0x01`, its `0x11`, its `0x10` (exists?), `0x04`, its seat's `0x14` at the current version | its `0x10`, its `0x11`, its seat's `0x14` (+1) | none |
| Approve | `0x01`, its `0x11`, its `0x10`; `EXPIRED` keep: the proposer's `0x14` at the record's base | its `0x10`, its `0x11` | `EXPIRED`: its `0x10`, the proposer's `0x14` at the record's base (-1) |
| Execute | `0x01`, its `0x11`, its `0x10`, `0x04`; stale branch: its `0x12`; validator payload: the `0x03` prefix (at most 128 entries); admit or disable: its `0x17`; success, or a keep from `OPEN` or `QUEUED`: the proposer's `0x14` at the record's base | one `0x03` entry, or its `0x13`, or its `0x17`; its `0x12`; `0x04`; its `0x10`; its `0x11`; the proposer's `0x14` (-1) | `EXPIRED` or `REJECTED`: its `0x10`; the proposer's `0x14` (-1) only from `OPEN` or `QUEUED` |
| EndBlock | `0x20` (guard); the `0x03` prefix; `0x01` when any entry is pending | `0x01` (new set); deletes the applied `0x03` keys | none; a failure halts |

A rejected message writes nothing. The engine writes only on a success or keep path, after every check that can fail, and the keeper runs it on a cache branch that it drops on a reject.

## Gas

Each message pays a fixed 50,000 gas, then the SDK gas store charges each read and write by its key and value length. The charge does not depend on how many other keys the store holds. A message touches only the keys in the table above. Their value lengths depend on the message's own fields, the current config, and fixed-size counts and digests. So gas does not grow with past proposals, business ids, or the executed set. An execute of a validator payload also reads the pending prefix, at most 128 entries, so its gas is bounded but depends on how many changes are scheduled. `TestCouncilGasDoesNotGrowWithHistory` (keeper) and `TestCommittedGasDoesNotGrowWithHistory` (app) measure the same message before and after 300 terminal proposals and require equal gas.

EndBlock work is bounded too. Execute refuses a 129th scheduled change and genesis validation refuses more than 128, so EndBlock reads at most 128 entries per block (`MaxPendingChanges`, PROPOSED, DR-06-04).

## Quotas

`MaxOpenProposalsPerSeat` is 32 (PROPOSED, DR-06-03). It counts only `OPEN` and `QUEUED` proposals of one seat on the current base policy version. Executed, expired, and rejected proposals never count. A proposal on an older base can never execute, so it stops counting when an execute bumps the version. A seat's own current-version proposal that is never approved, or whose execute fails with any code other than `ERROR_CODE_FORBIDDEN` (for example `ERROR_CODE_BOUNDS` or `ERROR_CODE_DUPLICATE_ID`), keeps its slot until an execute bumps the version or it is marked expired; its expiry may be as late as `unix_seconds_max`. `MaxPendingChanges` is 128 (PROPOSED, DR-06-04). No message cancels a scheduled change, so 128 far-future entries block later validator changes until they apply. The spec machine has neither cap; both are recorded deviations. `docs/architecture/authority-authorization.md` defines DR-06-01 to DR-06-06.

## Replay retention

Proposal records (`0x10`) and business ids (`0x11`) are kept for the life of the chain, as in the spec. The executed set (`0x12`) is kept because the stale branch reads it. Nothing is pruned: pruning would weaken replay protection. Storage grows by one bounded record per accepted message. Gas does not.

## Export

`ExportState` returns the config key plus the pending entries. It refuses, with `ErrExportRefused` naming the prefix, while any other prefix holds a key: the genesis proto has no field for that state (DR-06-01). The module `ExportGenesis` panics with that error because the SDK interface has no error result; `App.Export` checks first and returns it.

## No migration

No live chain exists, so there is no migration from the round-2 layout, which kept everything but the config in one side document at `0x20`. The round-2 code wrote `0x20` when an authority message succeeded or was kept, and when a validator change applied. A home where that happened halts in EndBlock with an error that says to re-initialize it, and export refuses it. A round-2 home where it never happened has no `0x20` and keeps running. If its genesis had pending entries, the round-2 layout kept them inside `0x01`: EndBlock never applies them, and every council message fails with `ERROR_CODE_INTEGRITY`. Re-initialize every devnet home created before the fix round.
