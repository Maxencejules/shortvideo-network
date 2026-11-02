# chain/x/authority

Validator membership and council changes (Goal 06). This module is the only one that returns validator updates. `InitialUpdates` returns the genesis set; EndBlock returns a changeset when a scheduled change is due.

Goal 07 connected the council admit, disable and force-withdraw payloads to `x/providers` and `x/content` through registry hooks. Provider and content records live in those modules, not here.

## Messages

| Message | Signer | Effect |
| --- | --- | --- |
| `MsgProposeChange` | A devnet council seat | Opens a proposal with a validator power change, a governed limit, a provider admit or disable, or a force-withdraw payload. |
| `MsgApproveChange` | A devnet council seat | Adds the seat's approval. The third of four seats queues the proposal. |
| `MsgExecuteChange` | The `AUTHORITY` submitter | Applies a queued proposal: schedules a validator change, sets a governed limit, or calls the registry hook of an admit, disable or force-withdraw payload. |

Provider messages (`RegisterProvider`, `UpdateProvider`, `RotateProviderKey`, `DrainProvider`) are `x/providers` handlers, and content messages are `x/content` handlers; this module has none. Goal 06 made provider messages `ERROR_CODE_FORBIDDEN` at the router until Goal 07 (D1); Goal 07 ended that. There is no single-signature admit, disable, or force-withdraw message (DEC-009). Council seat rotation and service account rotation are engine-only (`RotateCouncilSeat`, `RotateServiceSubmitter`); no message reaches them.

A seat key passes the ante only through a submitter row for its address. Genesis validation (`ValidateGenesisState`, and `genesis.Validate` on the document) refuses a seat address listed with any role but `BROADCASTER` (`ERROR_CODE_FORBIDDEN`). Spec `validate_genesis_view` has no such rule; this is the interim answer to DR-07-11, stricter than the spec. No registry handler accepts a `BROADCASTER` signer, so a seat key reaches only the council messages.

`docs/architecture/authority-authorization.md` is the authorization map. `docs/operations/validator-lifecycle.md` is the operator procedure.

## Store layout

One prefix byte, then the fields that name the object. Every id in a key matches `^[a-z0-9][a-z0-9._-]{0,63}$` and is checked before any write.

| Key | Value | Written by | Read by |
| --- | --- | --- | --- |
| `0x01` | Deterministic protobuf `GenesisState`: validators, submitters, devnet test seats, quorum loss `UNSET`, blocked decisions, devnet test parameters. Proposals, pending entries, and the approved council are always empty here. | InitGenesis; EndBlock when the set changes; engine-only rotations | every message; EndBlock when any entry is pending; export; `DevnetRole` |
| `0x02 \|\| addr(20)` | One byte, the submitter role | InitGenesis; engine-only service and seat rotations | ante (`Admitted`); export; `Role` |
| `0x03 \|\| kind(1) \|\| point(8, big-endian) \|\| proposal_id` | Deterministic protobuf `PendingValidatorPower`. Kind `0x01` is a height, `0x02` a unix time, so key order is the spec activation order. | Execute of a validator payload; InitGenesis; EndBlock deletes applied entries | Execute of a validator payload (projection); EndBlock; export |
| `0x04` | Policy version, 8 bytes big-endian. Absent means 1. | Execute | Propose; Execute; export check |
| `0x10 \|\| proposal_id` | Proposal record, JSON with a fixed field order. A force-withdraw record keeps its content id, version, and the 32-byte reason digest value. | Propose; Approve; Execute | Propose (duplicate check); Approve; Execute |
| `0x11 \|\| business_id` | 32-byte business digest | each successful message that is not an idempotent retry | each message (its own id); `BusinessDigest` |
| `0x12 \|\| parameter_key` | 32-byte payload digest of the last executed change | Execute | Execute, stale branch only |
| `0x13 \|\| limit_key` | 8-byte big-endian value | Execute of a governed limit | `GovernedLimit` |
| `0x14 \|\| base_policy_version(8, big-endian) \|\| seat_id` | 8-byte big-endian count of the seat's `OPEN` or `QUEUED` proposals on that base version. Deleted at 0. | Propose (+1); every transition out of `OPEN` or `QUEUED` (-1, at the record's base) | Propose (its own seat, current version only); those transitions |
| `0x15 \|\| old_pub(32)` | Seat id | `RotateCouncilSeat` | that rotation; export check |
| `0x16 \|\| old_addr(20)` | `0x01` | `RotateServiceSubmitter` | that rotation; export check |
| `0x17` | Reserved: round 3 provider rows. Never written since Goal 07; provider records are `x/providers` state. | none | export check (refused) |
| `0x20` | The round-2 side document. Never written. | none | EndBlock guard; export check |

## What each message reads and writes

| Message | Reads | Writes on success | Writes on a keep outcome |
| --- | --- | --- | --- |
| Propose | `0x01`, its `0x11`, its `0x10` (exists?), `0x04`, its seat's `0x14` at the current version | its `0x10`, its `0x11`, its seat's `0x14` (+1) | none |
| Approve | `0x01`, its `0x11`, its `0x10`; `EXPIRED` keep: the proposer's `0x14` at the record's base | its `0x10`, its `0x11` | `EXPIRED`: its `0x10`, the proposer's `0x14` at the record's base (-1) |
| Execute | `0x01`, its `0x11`, its `0x10`, `0x04`; stale branch: its `0x12`; validator payload: the `0x03` prefix (at most 128 entries); success, or a keep from `OPEN` or `QUEUED`: the proposer's `0x14` at the record's base. A registry payload reads no other authority key; its hook reads and writes `x/providers` or `x/content`. | one `0x03` entry, or its `0x13`, or nothing more for a registry payload; its `0x12`; `0x04`; its `0x10`; its `0x11`; the proposer's `0x14` (-1) | `EXPIRED` or `REJECTED`: its `0x10`; the proposer's `0x14` (-1) only from `OPEN` or `QUEUED` |
| EndBlock | `0x20` (guard); the `0x03` prefix; `0x01` when any entry is pending | `0x01` (new set); deletes the applied `0x03` keys | none; a failure halts |

A rejected message writes nothing. The engine writes the authority store only on a success or keep path, after every check that can fail, and the keeper runs it on a cache branch that it drops on a reject. A business gate whose id is absent from `0x11` also reads one key in each peer ledger (`x/providers` `0x02`, `x/content` `0x03`).

## Council payloads and registry hooks

| Payload | Authority decides | Registry call |
| --- | --- | --- |
| `validator_power` | schedules a pending power change | none |
| `governed_limit` | stores the value (1..protocol ceiling) | none |
| `admit_provider` | `devnet` = DEC-002 is the labelled devnet test parameter with role `DEVNET_TEST_ONLY` on a `shortvideo-dev-` chain id | `ProviderAdmission.AdmitByCouncil(ctx, id, devnet)` |
| `disable_provider` | none | `ProviderAdmission.DisableByCouncil(ctx, id)` |
| `force_withdraw` | `devnet` = the same rule for DEC-003 | `ContentRemoval.ForceWithdrawByCouncil(ctx, id, version, reason, devnet)` with the content id, version and reason digest value from the `0x10` record |

- `State.Registry` is an interface. `Keeper.transition` binds it after `LoadState`, to the keepers set with `Keeper.WithRegistry`. Without a registry the three payloads are `ERROR_CODE_MALFORMED`.
- The registry runs every check before its first write. `ERROR_CODE_FORBIDDEN` marks the proposal `REJECTED` (keep path). Any other code (`NOT_FOUND`, `DECISION_BLOCKED`, `CONFLICT`, `WITHDRAWN`, `STALE_VERSION`, ...) fails the message and the proposal stays `QUEUED`; it can execute later. A registry cannot ask for a keep or an idempotent result (`registryOutcome`).
- The hook runs inside `applyPayload`, before the engine's last checks (`ERROR_CODE_OVERFLOW` at policy version 2^64-1, an `ERROR_CODE_INTEGRITY` open count or record). So the keeper binds the hooks to a second cache nested in the message branch, with its own event manager. A success writes it back, then the message branch. A keep writes only the message branch. A failure drops both. A registry write and its events never outlive the proposal outcome.
- A registry entry is a coordination record. Admission is not evidence of an independent operator; the DEC-002 membership policy stays blocked and `operator_group_id` is a label.

## Business ids

Business ids are one protocol namespace across authority, providers, and content (`business_gate` in the spec machine). The engine reads authority's own `0x11` row first. When it is absent and peers are wired (`Keeper.WithPeers`), it asks `msgkit.Peers` for every other module in name order: absent proceeds, the same digest is an idempotent success, another digest is `ERROR_CODE_REPLAY`. A peer value that is not 32 bytes is `ERROR_CODE_INTEGRITY`, the same rule as for authority's own value. `Keeper.BusinessDigest` serves authority's ids to the other modules.

Business ids are not in the v0 genesis schema and are not exported (DR-07-03, alongside DR-06-01).

## Read surface

Other modules consume these through their own small interfaces. Each reads at most one key of committed state and writes nothing, so a registry message's gas does not depend on authority history.

| Method | Key | Returns | On a malformed value |
| --- | --- | --- | --- |
| `Role(ctx, addr)` | `0x02 \|\| addr` | the genesis `SubmitterRole` | `UNSPECIFIED` when `addr` is not 20 bytes, the row is missing, the value is not 1 byte, or the byte is outside the enum |
| `DevnetRole(ctx, decisionID)` | `0x01` | the role name of a devnet test parameter under the `State.devnet` rule with the chain id of `ctx` | not found when the config is missing, does not decode, or has the old layout; callers return `ERROR_CODE_DECISION_BLOCKED` |
| `GovernedLimit(ctx, key)` | `0x13 \|\| key` | the value an executed `GovernedLimitChange` set; absent is not found and callers apply `msgkit.Effective` (the protocol ceiling) | panics when the value is not 8 bytes or not in 1..ceiling |
| `BusinessDigest(ctx, id)` | `0x11 \|\| id` | a copy of the business digest authority committed for `id` (implements `msgkit.BusinessSource`); an id outside the pattern is not found without a read | panics when the value is not 32 bytes |
| `Admitted(ctx, addr)` | `0x02 \|\| addr` | whether `addr` has an admission row (ante decorator) | |

`GovernedLimit` and `BusinessDigest` fail closed: no id is reported free and no transaction runs against the protocol ceiling on damaged state. Only `ExecuteChange` and the message handlers write these keys, so such a value is integrity damage. The SDK turns a handler panic into a failed transaction on every node. Query handlers also call `GovernedLimit` (the `Providers` page cap): ABCI `Query` recovers a panic into an error result, and the node gRPC server wraps every query in a recovery interceptor, so the query fails and the node keeps running. EndBlock never reads these keys, so the fail-closed paths do not halt the chain.

## Simulation

In simulation (`sdk.ExecModeSimulate`) the SDK runs the handler on an infinite gas meter, so `enforceCommitted` skips only the meter-limit check there; the ante handler still checks the declared gas limit (a declared 0 is accepted only in simulation). The transaction-size check, the fixed 50,000 gas charge, and the signer check run in every mode. FinalizeBlock always runs the meter check. CheckTx runs no message handler (`baseapp` `runMsgs` stops unless the mode is finalize or simulate); the ante handler checks the declared limit there. `TestEnforceCommitted` covers each mode by calling the function directly, `TestHandlerMeterCheckByMode` drives the same split through `Approve`, and `TestSimulateEstimatesGas` in `chain/app` simulates and then commits each council message. The ante handler rejects an out-of-range declared gas limit first, so the handler check is not observable at app level.

## Wiring

```go
readKeeper := authority.NewKeeper(keys[authority.ModuleName])
peers := msgkit.NewPeers()
// providers and content keepers take readKeeper for its read surface.
authorityKeeper := readKeeper.WithRegistry(providersKeeper, contentKeeper).WithPeers(peers)
peers.Add(authority.ModuleName, authorityKeeper)
```

`WithRegistry` panics on a nil hook. A nil peers set consults no other module. The keeper stored in `App`, passed to the module, the message router and the ante, is the one `WithRegistry(...).WithPeers(...)` returned.

## Gas

Each message pays a fixed 50,000 gas, then the SDK gas store charges each read and write by its key and value length. The charge does not depend on how many other keys the store holds. A message touches only the keys in the table above, plus the peer business-id reads and the registry hook's fixed key set of one record. Their value lengths depend on the message's own fields, the current config, and fixed-size counts and digests. So gas does not grow with past proposals, business ids, or the executed set. An execute of a validator payload also reads the pending prefix, at most 128 entries, so its gas is bounded but depends on how many changes are scheduled. `TestCouncilGasDoesNotGrowWithHistory` (keeper) and `TestCommittedGasDoesNotGrowWithHistory` (app) measure the same message before and after 300 terminal proposals and require equal gas.

EndBlock work is bounded too. Execute refuses a 129th scheduled change and genesis validation refuses more than 128, so EndBlock reads at most 128 entries per block (`MaxPendingChanges`, PROPOSED, DR-06-04).

## Quotas

`MaxOpenProposalsPerSeat` is 32 (PROPOSED, DR-06-03). It counts only `OPEN` and `QUEUED` proposals of one seat on the current base policy version. Executed, expired, and rejected proposals never count. A proposal on an older base can never execute, so it stops counting when an execute bumps the version. A seat's own current-version proposal that is never approved, or whose execute fails with any code other than `ERROR_CODE_FORBIDDEN` (for example `ERROR_CODE_BOUNDS` or `ERROR_CODE_DUPLICATE_ID`), keeps its slot until an execute bumps the version or it is marked expired; its expiry may be as late as `unix_seconds_max`. `MaxPendingChanges` is 128 (PROPOSED, DR-06-04). No message cancels a scheduled change, so 128 far-future entries block later validator changes until they apply. The spec machine has neither cap; both are recorded deviations. `docs/architecture/authority-authorization.md` defines DR-06-01 to DR-06-06.

## Replay retention

Proposal records (`0x10`) and business ids (`0x11`) are kept for the life of the chain, as in the spec. The executed set (`0x12`) is kept because the stale branch reads it. Nothing is pruned: pruning would weaken replay protection. Storage grows by one bounded record per accepted message. Gas does not.

## Export

`ExportState` returns the config key plus the pending entries. It refuses, with `ErrExportRefused` naming the prefix, while any other prefix holds a key, the reserved `0x17` prefix included: the genesis proto has no field for that state (DR-06-01). The module `ExportGenesis` panics with that error because the SDK interface has no error result; `App.Export` checks first and returns it. `App.Export` also refuses a non-empty provider or content catalogue (the goal 05 launch rule); provider and content business ids are DR-07-03.

## No migration

No live chain exists, so there is no migration. This binary refuses every committed goal 05 or goal 06 home at start, round 2 and round 3 alike: `app.New` finds the goal-05/06 scaffold layout in the providers and content stores and returns an error that says to initialize a new home (`checkRegistryLayouts`, `TestAppRefusesScaffoldLayout`). Goal 07 also added two fields (`content_id`, `content_version`) to every proposal record, so a record written by a goal 06 binary would not decode (`ERROR_CODE_INTEGRITY`). `ConsensusVersion` stays 1. Re-initialize every such home.

The round-2 layout kept everything but the config in one side document at `0x20`. The round-2 code wrote `0x20` when an authority message succeeded or was kept, and when a validator change applied. A home that passes the layout check and still holds `0x20` halts in EndBlock with an error that says to re-initialize it, and export refuses it.

## Tests

- `engine_test.go`: council flow, delay, expiry keep, forbidden removal keep, halt on a mismatched removal, council and service rotation, `TestAdmitAndDisableRows` (admit, disable and force-withdraw through the deterministic spec registry; every reject leaves the store and the registry unchanged).
- `registry_test.go`: DEC-002 and DEC-003 flags, decoded force-withdraw arguments, `FORBIDDEN` keeps `REJECTED`, other registry codes leave the proposal `QUEUED`, nil registry, expiry before the registry call, external business ids (`REPLAY`, idempotent, `INTEGRITY` on a malformed peer value) on propose, approve and execute, the devnet role rule. Each failure compares the whole store (`dumpKV`) before and after.
- `keeper_registry_test.go` (real IAVL multistore with a second store for the test hooks): hook writes committed only on success, dropped on keep, on failure and on a late `OVERFLOW` (`TestKeeperRegistryWriteDroppedOnLateFailure`); force-withdraw arguments from the record; one business-id namespace through `msgkit.Peers`, with `INTEGRITY` for a peer value that is not 32 bytes (`TestKeeperMalformedPeerBusinessDigestIsIntegrity`); the read surface and its fail-closed paths; deterministic sources.
- `genesis_test.go`: one row per `ValidateGenesisState` rule, the seat-address role rule included; `TestSeatAddressMatchesGenesis` checks that `genesis.SeatAddress` derives the same account as `SeatAddress`.
- `enforce_test.go`: `enforceCommitted` per execution mode (`TestEnforceCommitted`) and through `Approve` (`TestHandlerMeterCheckByMode`).
- `gas_test.go`: `TestMessageKeySets` pins the key sets, registry payload executes included; `TestCouncilGasDoesNotGrowWithHistory`.
- `fuzz_state_test.go` `FuzzStateTransitions` and `fuzz_registry_test.go` `FuzzCouncilRegistryPayloads`: bounded operation sequences on a cache branch of the per-object store and a clone of the spec registry, as `Keeper.transition` runs them. A failure must leave the branch byte-identical (`dumpKV`) and the registry unchanged; a keep never writes the registry.
