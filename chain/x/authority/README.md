# chain/x/authority

Validator membership and council changes (Goal 06). This module is the only one that returns validator updates. `InitialUpdates` returns the genesis set; EndBlock returns a changeset when a scheduled change is due.

Goal 07 connected the council admit, disable and force-withdraw payloads to `x/providers` and `x/content` through registry hooks. Provider and content records live in those modules, not here.

Goal 10 (this package's part, without a protocol or store layout change) added: typed governed-limit checks at propose (D-1), the typed execute events (D-2), council and consensus key separation in both directions (D-7), the seat id cap at genesis (D-10), the `PolicyWriter` and `Accounts` hook interfaces with an authority-local writer that still stores governed values at `0x13`, `BumpPolicyVersionForUpgrade`, `svn.authority.v1.Query/Governance` (`QueryServiceDesc`; not registered by the app yet), and payload vectors. The store layout, the layout key (none) and `ConsensusVersion` (1) are unchanged. See [Goal 10 deviations](#goal-10-deviations-from-the-spec-machine).

## Messages

| Message | Signer | Effect |
| --- | --- | --- |
| `MsgProposeChange` | A devnet council seat | Opens a proposal with a validator power change, a governed limit, a provider admit or disable, or a force-withdraw payload. |
| `MsgApproveChange` | A devnet council seat | Adds the seat's approval. The third of four seats queues the proposal. |
| `MsgExecuteChange` | The `AUTHORITY` submitter | Applies a queued proposal: schedules a validator change, sets a governed limit through `PolicyWriter.SetLimit`, or calls the registry hook of an admit, disable or force-withdraw payload. A success emits `svn.events.v1.PolicyChanged` and `svn.events.v1.ProposalExecuted` (D-2). |

Provider messages (`RegisterProvider`, `UpdateProvider`, `RotateProviderKey`, `DrainProvider`) are `x/providers` handlers, and content messages are `x/content` handlers; this module has none. Goal 06 made provider messages `ERROR_CODE_FORBIDDEN` at the router until Goal 07 (D1); Goal 07 ended that. There is no single-signature admit, disable, or force-withdraw message (DEC-009). Council seat rotation and service account rotation are engine-only (`RotateCouncilSeat`, `RotateServiceSubmitter`); no message reaches them.

A seat key has no submitter row and no role. The ante admits its account because the address is derived from a seat key in the config (`AdmissionOf`), and lets it sign only `MsgProposeChange` and `MsgApproveChange`; any other message in a seat-signed transaction is `ERROR_CODE_FORBIDDEN` before the sequence moves (owner ruling DR-06-06, 2026-11-02). Genesis validation (`ValidateGenesisState`, and `genesis.Validate` on the document) refuses a seat address in the submitter list with any role, `BROADCASTER` included (`ERROR_CODE_FORBIDDEN`; DR-07-11 option 2). Spec `validate_genesis_view` has no such rule; it is stricter than the spec. `RotateCouncilSeat` changes the key in the config, so the admission moves with it and no row is written.

`docs/architecture/authority-authorization.md` is the authorization map. `docs/operations/validator-lifecycle.md` is the operator procedure.

## Store layout

One prefix byte, then the fields that name the object. Every id in a key matches `^[a-z0-9][a-z0-9._-]{0,63}$` and is checked before any write.

| Key | Value | Written by | Read by |
| --- | --- | --- | --- |
| `0x01` | Deterministic protobuf `GenesisState`: validators, submitters, devnet test seats, quorum loss `UNSET`, blocked decisions, devnet test parameters. Proposals, pending entries, and the approved council are always empty here. | InitGenesis; EndBlock when the set changes; engine-only rotations | every message; EndBlock when any entry is pending; export; `DevnetRole`; ante (`Admission`, for a signer without a `0x02` row) |
| `0x02 \|\| addr(20)` | One byte, the submitter role. A seat address has none. | InitGenesis; engine-only service rotation | ante (`Admission`); export; `Role` |
| `0x03 \|\| kind(1) \|\| point(8, big-endian) \|\| proposal_id` | Deterministic protobuf `PendingValidatorPower`. Kind `0x01` is a height, `0x02` a unix time, so key order is the spec activation order. | Execute of a validator payload; InitGenesis; EndBlock deletes applied entries | Execute of a validator payload (projection); EndBlock; export |
| `0x04` | Policy version, 8 bytes big-endian. Absent means 1. | Execute; `BumpPolicyVersionForUpgrade` (the upgrade runner only) | Propose; Execute; `PolicyVersion`; export check |
| `0x10 \|\| proposal_id` | Proposal record, JSON with a fixed field order. A force-withdraw record keeps its content id, version, and the 32-byte reason digest value. | Propose; Approve; Execute | Propose (duplicate check); Approve; Execute |
| `0x11 \|\| business_id` | 32-byte business digest | each successful message that is not an idempotent retry | each message (its own id); `BusinessDigest` |
| `0x12 \|\| parameter_key` | 32-byte payload digest of the last executed change | Execute | Execute, stale branch only |
| `0x13 \|\| limit_key` | 8-byte big-endian value, a key of the msgkit table (`bounds.json` `governed_limit_caps`) and a value in 1..ceiling | Execute of a governed limit, through `localPolicy.SetLimit` (adds the write to the execute's batch) | `GovernedLimit`; `Query/Governance` (the `max_page_limit` cap) |
| `0x14 \|\| base_policy_version(8, big-endian) \|\| seat_id` | 8-byte big-endian count of the seat's `OPEN` or `QUEUED` proposals on that base version. Deleted at 0. | Propose (+1); every transition out of `OPEN` or `QUEUED` (-1, at the record's base) | Propose (its own seat, current version only); those transitions |
| `0x15 \|\| old_pub(32)` | Seat id | `RotateCouncilSeat` | that rotation; Execute of a validator payload (D-7, last check); export check |
| `0x16 \|\| old_addr(20)` | `0x01` | `RotateServiceSubmitter` | that rotation; export check |
| `0x17` | Reserved: round 3 provider rows. Never written since Goal 07; provider records are `x/providers` state. | none | export check (refused) |
| `0x20` | The round-2 side document. Never written. | none | EndBlock guard; export check |

## What each message reads and writes

| Message | Reads | Writes on success | Writes on a keep outcome |
| --- | --- | --- | --- |
| Propose | `0x01`, its `0x11`, its `0x10` (exists?), `0x04`, its seat's `0x14` at the current version | its `0x10`, its `0x11`, its seat's `0x14` (+1) | none |
| Approve | `0x01`, its `0x11`, its `0x10`; `EXPIRED` keep: the proposer's `0x14` at the record's base | its `0x10`, its `0x11` | `EXPIRED`: its `0x10`, the proposer's `0x14` at the record's base (-1) |
| Execute | `0x01`, its `0x11`, its `0x10`, `0x04`; stale branch: its `0x12`; validator payload: the `0x03` prefix (at most 128 entries) and, when the projection and the pending cap pass, the `0x15` key of its consensus key (D-7); success, or a keep from `OPEN` or `QUEUED`: the proposer's `0x14` at the record's base. A registry payload reads no other authority key; its hook reads and writes `x/providers` or `x/content`. | one `0x03` entry, or its `0x13`, or nothing more for a registry payload; its `0x12`; `0x04`; its `0x10`; its `0x11`; the proposer's `0x14` (-1) | `EXPIRED` or `REJECTED`: its `0x10`; the proposer's `0x14` (-1) only from `OPEN` or `QUEUED` |
| EndBlock | `0x20` (guard); the `0x03` prefix; `0x01` when any entry is pending | `0x01` (new set); deletes the applied `0x03` keys | none; a failure halts |

A rejected message writes nothing. The engine writes the authority store only on a success or keep path, after every check that can fail, and the keeper runs it on a cache branch that it drops on a reject. A business gate whose id is absent from `0x11` also reads one key in each peer ledger (`x/content` `0x03`, `x/leases` `0x03`, `x/providers` `0x02`).

## Council payloads and registry hooks

| Payload | Authority decides | Registry call |
| --- | --- | --- |
| `validator_power` | schedules a pending power change | none |
| `governed_limit` | checks the key and value at propose (D-1); at execute `PolicyWriter.SetLimit` checks them again and stores the value (1..protocol ceiling) | none |
| `admit_provider` | `devnet` = DEC-002 is the labelled devnet test parameter with role `DEVNET_TEST_ONLY` on a `shortvideo-dev-` chain id | `ProviderAdmission.AdmitByCouncil(ctx, id, devnet)` |
| `disable_provider` | none | `ProviderAdmission.DisableByCouncil(ctx, id)` |
| `force_withdraw` | `devnet` = the same rule for DEC-003 | `ContentRemoval.ForceWithdrawByCouncil(ctx, id, version, reason, devnet)` with the content id, version and reason digest value from the `0x10` record |

- `State.Registry` is an interface. `Keeper.transition` binds it after `LoadState`, to the keepers set with `Keeper.WithRegistry`. Without a registry the three payloads are `ERROR_CODE_MALFORMED`.
- The registry runs every check before its first write. `ERROR_CODE_FORBIDDEN` marks the proposal `REJECTED` (keep path). Any other code (`NOT_FOUND`, `DECISION_BLOCKED`, `CONFLICT`, `WITHDRAWN`, `STALE_VERSION`, ...) fails the message and the proposal stays `QUEUED`; it can execute later. A registry cannot ask for a keep or an idempotent result (`registryOutcome`).
- The hook runs inside `applyPayload`, before the engine's last checks (`ERROR_CODE_OVERFLOW` at policy version 2^64-1, an `ERROR_CODE_INTEGRITY` open count or record). So the keeper binds the hooks to a second cache nested in the message branch, with its own event manager. A success writes it back, then the message branch. A keep writes only the message branch. A failure drops both. A registry write and its events never outlive the proposal outcome.
- A registry entry is a coordination record. Admission is not evidence of an independent operator; the DEC-002 membership policy stays blocked and `operator_group_id` is a label.

## Business ids

Business ids are one protocol namespace across authority, providers, content, leases, and evidence (`business_gate` in the spec machine). The engine reads authority's own `0x11` row first. When it is absent and peers are wired (`Keeper.WithPeers`), it asks `msgkit.Peers` for every other module in name order: absent proceeds, the same digest is an idempotent success, another digest is `ERROR_CODE_REPLAY`. A peer value that is not 32 bytes is `ERROR_CODE_INTEGRITY`, the same rule as for authority's own value. `Keeper.BusinessDigest` serves authority's ids to the other modules.

Business ids are not in the v0 genesis schema and are not exported (DR-07-03, alongside DR-06-01).

## Read surface

Other modules consume these through their own small interfaces. Each reads at most one key of committed state (`Admission`, which only the ante calls, reads two for a signer without a row) and writes nothing, so a registry message's gas does not depend on authority history.

| Method | Key | Returns | On a malformed value |
| --- | --- | --- | --- |
| `Role(ctx, addr)` | `0x02 \|\| addr` | the genesis `SubmitterRole` | `UNSPECIFIED` when `addr` is not 20 bytes, the row is missing, the value is not 1 byte, or the byte is outside the enum |
| `DevnetRole(ctx, decisionID)` | `0x01` | the role name of a devnet test parameter under the `State.devnet` rule with the chain id of `ctx` | not found when the config is missing, does not decode, or has the old layout; callers return `ERROR_CODE_DECISION_BLOCKED` |
| `DevnetNumeric(ctx, decisionID)` | `0x01` | `numeric_value` of the devnet test parameter under the same rule as `DevnetRole`; a matching row without a value returns `(0, true)` and the caller decides what 0 means | not found when the config is missing, does not decode, or has the old layout; x/leases returns `ERROR_CODE_DECISION_BLOCKED` |
| `PolicyVersion(ctx)` | `0x04` | the committed policy version the engine loads: 1 while the key is absent (before the first executed proposal), then the stored value; it grows by one with each executed proposal | panics when the value is not 8 bytes or is below 2, exactly where the engine's own read is `ERROR_CODE_INTEGRITY` |
| `GovernedLimit(ctx, key)` | `0x13 \|\| key` | the value an executed `GovernedLimitChange` set; absent is not found and callers apply `msgkit.Effective` (the protocol ceiling) | panics when the value is not 8 bytes or not in 1..ceiling |
| `BusinessDigest(ctx, id)` | `0x11 \|\| id` | a copy of the business digest authority committed for `id` (implements `msgkit.BusinessSource`); an id outside the pattern is not found without a read | panics when the value is not 32 bytes |
| `Admission(ctx, addr)` | `0x02 \|\| addr`, then `0x01` when there is no row | `AdmittedSubmitter` (admission row), `AdmittedSeat` (the account of a seat key in the config, no row), or `NotAdmitted` (ante decorator, DR-06-06). It reads the config only for a signer without a row, so a submitter's ante gas is unchanged. | `NotAdmitted` for a seat when the config is missing, does not decode, or has the old layout |

x/providers reads `Role` and `GovernedLimit`; x/content also reads `DevnetRole`. x/leases (Goal 08) reads `Role`, `GovernedLimit`, `DevnetNumeric` (the DEC-005 devnet test value, a devnet stand-in, not policy), and `PolicyVersion`. x/evidence (Goal 09) reads `Role` (only `AUDITOR` may submit audit batches) and `GovernedLimit` (`max_evidence_batch`, `max_page_limit`).

The governed key set and ceilings come from one table, `msgkit.Ceiling` (pinned to `bounds.json` by `chain/msgkit/bounds_test.go`); this package has no copy of it. `PolicyVersion` grows by one with each executed proposal and with each `BumpPolicyVersionForUpgrade` call.

`GovernedLimit`, `BusinessDigest`, and `PolicyVersion` fail closed: no id is reported free, no transaction runs against the protocol ceiling, and no version 1 is reported on damaged state (`TestPolicyVersionRead`). Only `ExecuteChange`, `BumpPolicyVersionForUpgrade` (`0x04` only) and the message handlers write these keys, so such a value is integrity damage. The SDK turns a handler panic into a failed transaction on every node. Query handlers also call `GovernedLimit` (the `Providers` page cap): ABCI `Query` recovers a panic into an error result, and the node gRPC server wraps every query in a recovery interceptor, so the query fails and the node keeps running. Authority's EndBlock never reads these keys. x/leases BeginBlock reads `GovernedLimit(ctx, "max_expiry_steps")` in every block, so a damaged value of that one key makes every BeginBlock panic and halts the chain (fail closed, like an expiry invariant violation, DR-08-07). `TestDamagedExpiryLimitHalts` (chain/app) sets that key through an executed council change, then overwrites the stored value in the uncommitted working state with 3 bytes, with the 8-byte value 0, and with the ceiling + 1. On two nodes the next `FinalizeBlock` (one valid `OfferLease`, nothing due) panics with `authority: governed limit "max_expiry_steps" is not an 8-byte value in 1..ceiling (N bytes stored)` instead of returning, and a second attempt at the same height panics the same way. Because it panics, it returns no app hash and writes nothing to the store, so nothing commits. The test calls `FinalizeBlock` directly. CometBFT stopping on the panic ("CONSENSUS FAILURE", consensus/state.go) and a restart on a committed damaged value are not tested.

`DevnetRole` and `DevnetNumeric` report not found on a missing, undecodable, or old-layout config, so a caller fails closed with `DECISION_BLOCKED` (`TestDevnetNumeric`).

## Goal 10 deviations from the spec machine

Each one is stricter than `spec_machine.py` or adds an event; none changes a code number, a proto, or the store layout.

| Id | Chain | Spec | Test |
| --- | --- | --- | --- |
| D-1 (DR-10-21) | A governed limit whose key is not in the msgkit table (`bounds.json` `governed_limit_caps`), or whose value is above its ceiling, is `ERROR_CODE_BOUNDS` at propose, right after the payload checks (parse, digest, digest algorithm, parameter key binding, validator activation) and before `check_time`, the business gate, the duplicate id, expiry, base version and quota checks. Safety-cap names (`max_tx_bytes`, `max_gas_per_tx`) and quota names are refused the same way. | Accepted at propose; `BOUNDS` at execute with the proposal left `QUEUED` (`spec_machine.py:1338-1343`). | `TestGovernedLimitTypedAtProposeD1`, `TestSafetyCapsNotGovernable`, `TestKeeperInvalidBounds`, `TestPayloadVectors` |
| D-2 (DR-10-22) | A successful execute of any kind emits `svn.events.v1.PolicyChanged{policy_version, parameter_key}` and then `svn.events.v1.ProposalExecuted{proposal_id, policy_version}`, both with the new version, after a registry hook's events (`events.go`). | `PolicyChanged` only (`spec_machine.py:1524-1527`). | `TestExecuteEmitsPolicyEventsD2` |
| D-7 (DR-10-13) | Genesis refuses a seat key that is the consensus key of a genesis validator or a pending entry (`DUPLICATE_ID`). An executed validator payload whose consensus key is a current or a retired (`0x15`) seat key is `ERROR_CODE_DUPLICATE_ID` and stays `QUEUED`; it is the last check of the execute, after the projection and the pending cap. `RotateCouncilSeat` onto a current or pending validator key is `DUPLICATE_ID`. | Not checked. | `TestSeatKeyIsNotAValidatorKeyD7`; `FuzzStateTransitions` checks the invariant after every operation and draws seat keys (key pool 3) |
| D-10 (DR-10-42) | Genesis refuses a seat id longer than `MaxSeatIDLength` (59) with `BOUNDS`, so `seat-` plus the id fits the 64-character id pattern. | The id pattern allows 64. | `TestSeatIDLengthAtGenesisD10` |

The differential oracle rules for D-1 and D-7 belong to `chain/x/authority/difftest` (a separate package). The execute-side ceiling check stays (defence in depth); after D-1 no message reaches it, so `TestExecuteRechecksCeiling` writes a `QUEUED` record directly and shows `BOUNDS` with the proposal left `QUEUED` and the store unchanged.

## Policy writer and account hooks

`PolicyWriter` (`policy.go`) is the policy store as an executed proposal sees it: `SetLimit`, `SchedulePending`, `ScheduleUpgrade`, `CancelUpgrade`, `UpgradeApplied`. `applyPayload` is its only caller (`TestPolicyWriterCallSites`, an AST scan of this package). Every method checks before its first write.

This binary's implementation is `localPolicy`. `SetLimit` checks the key and the ceiling and adds the `0x13` write to the execute's batch, so the write is flushed only with the rest of a successful execute and the rule "a rejected message performs no Set and no Delete" still holds. The authority layout has no row for a pending limit change or an upgrade plan, and no v0 payload carries an activation height or a plan, so `applyPayload` calls only `SetLimit`; `SchedulePending`, `ScheduleUpgrade` and `CancelUpgrade` refuse with `ERROR_CODE_MALFORMED` and write nothing, and `UpgradeApplied` is false (`TestLocalPolicyWriter`). x/policy is not wired to authority in this binary.

`Accounts` (`EnsureAccount(addr)`) is declared for the account of a new seat key (DR-10-35). No code calls it: no message replaces a seat key (`RotateCouncilSeat` stays engine-only) and the protocol has no seat payload.

## Policy version bump for an upgrade

`BumpPolicyVersionForUpgrade` (`upgrade.go`) adds one to `0x04` (interim DR-10-38). Every proposal on the old base then fails its execute with `STALE_VERSION` or `CONFLICT` and stops counting against its seat's quota; already scheduled validator changes are not touched. A damaged version is `ERROR_CODE_INTEGRITY` and version 2^64-1 is `ERROR_CODE_OVERFLOW`; neither writes (`TestBumpPolicyVersionForUpgrade`). No non-test file of the chain module outside `chain/upgrade` names the method (`TestBumpPolicyVersionCallSites`).

## Query/Governance

`Keeper.Governance` serves `svn.authority.v1.Query/Governance` through the hand-written `QueryServiceDesc` (D-9). The app does not register it yet. It reads committed state and never writes. Interim paging (DR-10-23):

- One ordered walk: validators by consensus key, submitters by address, seats by seat id, pending validator changes in store-key order, then proposal records by proposal id. Every item counts against the page limit, the first one included.
- The limit is checked by `msgkit.CheckPage` against the effective `max_page_limit` (governed value or the ceiling 100); 0 uses the cap; above the cap is `BOUNDS`.
- `page_key` is a tag byte and a cursor: `0x01` and a 32-byte consensus key, `0x02` and a 20-byte address, `0x03` and a seat id, `0x04` and one byte, the position of the next pending entry. A bare proposal id (ids start with `[a-z0-9]`, never a tag byte) continues the proposals. A key over 64 bytes, a tag with a cursor of the wrong shape, an unknown first byte, or a pending position with no entry is `BOUNDS`. A key cursor resumes at the first item at or after it.
- Pending entries use a position, not their store key: a store key plus a tag can exceed the 64-byte page-key bound for a 55- to 64-character proposal id. The pending set (at most 128 entries) is read whole, so a position is exact at one height; a walk across heights should query at the height of its first page. This is a deviation from the plan text "pending by store key".
- A proposal carries `approver_keys` only; the proto has no seat-id field. `approved_council` is always empty and `quorum_loss` is the stored value.
- Errors are `msgkit.QueryErr`; a record or entry that does not decode is a plain error.

`TestGovernanceQueryPaging`, `TestGovernanceQueryRejects`, and `TestGovernanceQueryResponseBound` (100 maximal proposals encode to 36,874 bytes against a computed bound of 36,875).

## Payload vectors

`testdata/payload-vectors.json` holds the canonical SVN-JCS-1 text and SHA-256 digest of each v0 payload kind, every governed key at 1 and at its ceiling, and five governed limits the chain refuses at propose (D-1). `TestPayloadVectors` checks them against the chain code and a full propose; `tests/unit/test_payload_vectors.py` recomputes them with goal 02's `jcs.py` and `spec_machine.py` and checks that the spec accepts every payload at propose and refuses exactly the D-1 rows at execute. `TestV0PayloadVectorsUnchanged` checks that the chain rebuilds the two protocol payload vectors of `docs/protocol/fixtures/vectors.json` byte for byte.

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

Each message pays a fixed 50,000 gas, then the SDK gas store charges each read and write by its key and value length. The charge does not depend on how many other keys the store holds. A message touches only the keys in the table above, plus the peer business-id reads and the registry hook's fixed key set of one record. Their value lengths depend on the message's own fields, the current config, and fixed-size counts and digests. So gas does not grow with past proposals, business ids, or the executed set. An execute of a validator payload also reads the pending prefix, at most 128 entries, so its gas is bounded but depends on how many changes are scheduled. In the app, a force-withdraw execute also voids the content's open lease offers through the x/leases withdrawal hook, at most 256 (`leases.MaxOpenOffersPerContent`), so its gas is bounded but depends on that count (`TestWithdrawalAtCapGasInApp` in `chain/app`). `TestCouncilGasDoesNotGrowWithHistory` (keeper) and `TestCommittedGasDoesNotGrowWithHistory` (app) measure the same message before and after 300 terminal proposals and require equal gas.

EndBlock work is bounded too. Execute refuses a 129th scheduled change and genesis validation refuses more than 128, so EndBlock reads at most 128 entries per block (`MaxPendingChanges`, PROPOSED, DR-06-04).

## Quotas

`MaxOpenProposalsPerSeat` is 32 (PROPOSED, DR-06-03). It counts only `OPEN` and `QUEUED` proposals of one seat on the current base policy version. Executed, expired, and rejected proposals never count. A proposal on an older base can never execute, so it stops counting when an execute bumps the version. A seat's own current-version proposal that is never approved, or whose execute fails with any code other than `ERROR_CODE_FORBIDDEN` (for example `ERROR_CODE_BOUNDS` or `ERROR_CODE_DUPLICATE_ID`), keeps its slot until an execute bumps the version or it is marked expired; its expiry may be as late as `unix_seconds_max`. `MaxPendingChanges` is 128 (PROPOSED, DR-06-04). No message cancels a scheduled change, so 128 far-future entries block later validator changes until they apply. The spec machine has neither cap; both are recorded deviations. `docs/architecture/authority-authorization.md` defines DR-06-01 to DR-06-06.

## Replay retention

Proposal records (`0x10`) and business ids (`0x11`) are kept for the life of the chain, as in the spec. The executed set (`0x12`) is kept because the stale branch reads it. Nothing is pruned: pruning would weaken replay protection. Storage grows by one bounded record per accepted message. Gas does not.

## Export

`ExportState` returns the config key plus the pending entries. It refuses, with `ErrExportRefused` naming the prefix, while any other prefix holds a key, the reserved `0x17` prefix included: the genesis proto has no field for that state (DR-06-01). The module `ExportGenesis` panics with that error because the SDK interface has no error result; `App.Export` checks first and returns it. `App.Export` also refuses a non-empty provider, content, or lease catalogue (the goal 05 launch rule); provider, content, and lease business ids are DR-07-03.

## No migration

No live chain exists, so there is no migration. This binary refuses every committed goal 05 or goal 06 home at start, round 2 and round 3 alike: `app.New` finds the goal-05/06 scaffold layout in the providers and content stores and returns an error that says to initialize a new home (`checkRegistryLayouts`, `TestAppRefusesScaffoldLayout`). Goal 07 also added two fields (`content_id`, `content_version`) to every proposal record, so a record written by a goal 06 binary would not decode (`ERROR_CODE_INTEGRITY`). `ConsensusVersion` stays 1. Re-initialize every such home.

The round-2 layout kept everything but the config in one side document at `0x20`. The round-2 code wrote `0x20` when an authority message succeeded or was kept, and when a validator change applied. A home that passes the layout check and still holds `0x20` halts in EndBlock with an error that says to re-initialize it, and export refuses it.

## Tests

- `engine_test.go`: council flow, delay, expiry keep, forbidden removal keep, halt on a mismatched removal, council and service rotation, `TestAdmitAndDisableRows` (admit, disable and force-withdraw through the deterministic spec registry; every reject leaves the store and the registry unchanged).
- `registry_test.go`: DEC-002 and DEC-003 flags, decoded force-withdraw arguments, `FORBIDDEN` keeps `REJECTED`, other registry codes leave the proposal `QUEUED`, nil registry, expiry before the registry call, external business ids (`REPLAY`, idempotent, `INTEGRITY` on a malformed peer value) on propose, approve and execute, the devnet role rule. Each failure compares the whole store (`dumpKV`) before and after.
- `keeper_registry_test.go` (real IAVL multistore with a second store for the test hooks): hook writes committed only on success, dropped on keep, on failure and on a late `OVERFLOW` (`TestKeeperRegistryWriteDroppedOnLateFailure`); force-withdraw arguments from the record; one business-id namespace through `msgkit.Peers`, with `INTEGRITY` for a peer value that is not 32 bytes (`TestKeeperMalformedPeerBusinessDigestIsIntegrity`); the read surface and its fail-closed paths; deterministic sources.
- `read_test.go` (real IAVL multistore): `TestDevnetNumeric` (every chain id, label, approval, and missing-row case against the engine's `State.devnet` and `DevnetRole`; DEC-005 3600 read from a protojson genesis document through `InitGenesis`; empty store, undecodable config, and old-layout config) and `TestPolicyVersionRead` (1 on an empty store and after genesis, 1 while a proposal is only queued, 2 and 3 after executed proposals, unchanged by a rejected execution; stored values read or panic exactly where the engine's policy read succeeds or fails).
- `genesis_test.go`: one row per `ValidateGenesisState` rule (the goal-10 D-7 and D-10 rules are in `separation_test.go`), the seat-address rule included (a seat address in the submitter list is refused with every role, `BROADCASTER` included; a `BROADCASTER` that is not a seat is accepted); `TestSeatAddressMatchesGenesis` checks that `genesis.SeatAddress` derives the same account as `SeatAddress`.
- `gates_test.go`: council-ready, stale and conflicting executes, activation groups, stale-proposal counts, activation height bounds and policy-version overflow; `TestAdmissionOf` (DR-06-06: a submitter by its row, a seat account by the seat list with no row, everything else and every seat under a missing or undecodable config not admitted); `TestSeatRotationMovesSeatAdmission` (the new key's account is admitted as a seat, the retired key's is not, no submitter or admission row is written, a new key at a submitter address is `DUPLICATE_ID`); `TestEngineOnlyRotationRejects` (exact codes, including a service rotation onto a seat address).
- `enforce_test.go`: `enforceCommitted` per execution mode (`TestEnforceCommitted`) and through `Approve` (`TestHandlerMeterCheckByMode`).
- `gas_test.go`: `TestMessageKeySets` pins the key sets, registry payload executes included, and a successful validator execute with its `0x15` read (D-7); `TestCouncilGasDoesNotGrowWithHistory`.
- `fuzz_state_test.go` `FuzzStateTransitions` and `fuzz_registry_test.go` `FuzzCouncilRegistryPayloads`: bounded operation sequences on a cache branch of the per-object store and a clone of the spec registry, as `Keeper.transition` runs them. A failure must leave the branch byte-identical (`dumpKV`) and the registry unchanged; a keep never writes the registry. Goal 10: the state fuzz also checks that no consensus key is a current or retired seat key (D-7), and key pool 3 draws seat keys. The seed "payloads that cannot execute stay QUEUED" pinned `BOUNDS` at execute for an over-ceiling and an unknown governed limit; under D-1 both are `BOUNDS` at propose, so the seed now pins that and keeps the two registry `NOT_FOUND` rows (same `Bounds: 2` count). A new seed pins the D-7 `DUPLICATE_ID`.
- `typed_propose_test.go` (goal 10): D-1 per key and value, its place in the check order, the keeper path; safety caps; the execute-side recheck; `localPolicy`; the `PolicyWriter` call-site scan.
- `events_test.go` (goal 10): D-2 for every kind, and no typed event on propose, approve, retry, keep or refusal.
- `separation_test.go` (goal 10): D-7 in both directions; D-10.
- `upgrade_test.go` (goal 10): `BumpPolicyVersionForUpgrade` and its call-site scan over the chain module.
- `query_test.go` (goal 10): `Query/Governance` paging, refusals, and the response size bound.
- `vectors_test.go` (goal 10): the payload vectors and the two protocol payload vectors.
- `council_acceptance_test.go` (goal 10, keeper level on a real IAVL store, finalize mode): `TestKeeperDuplicateVotes`, `TestKeeperInsufficientApproval` (every v0 kind), `TestKeeperInvalidBounds`, `TestKeeperConcurrentProposalsOnOneKey`, `TestKeeperExpiryAndReviewDelayBoundaries` (a labelled 60-second test delay: `execute_after - 1` and `execute_after`; expiry - 1 and expiry for approve, execute and propose), `TestKeeperUnauthorizedWritesFailAtExecution` (every non-seat signer for propose and approve, every non-`AUTHORITY` signer for execute), `TestNoPayloadApprovesDecision`. Each refusal compares the authority store and the registry hook store before and after and requires no typed event. The app-level tests through FinalizeBlock are separate.
