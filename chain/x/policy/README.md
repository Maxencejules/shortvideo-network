# chain/x/policy

The policy module (goal 10). Package `policy`, module name `policy`, consensus version 2 (version 1 is the scaffold
slot; see "Genesis, export, migrations").

x/policy stores the fixed protocol safety caps, the governed resource bounds, pending governed changes, the upgrade plan
and the applied-upgrade history, the persisted module version map, and the app version. It serves
`svn.policy.v1.Query/Policy`. It has no message service: "There is no direct MsgUpdateLimits. Limit changes are proposal
payloads executed by svn.authority.v1.MsgExecuteChange." (`docs/protocol/proto/svn/policy/v1/tx.proto`). x/authority stays
the council and the policy version counter (decision request DR-10-20, interim); it is meant to be the only writer of the
governed, pending, and plan rows, through the writer methods below.

## Status of this directory

This is the goal-10 Wave 0 core. It is **not wired into the app**:

- `chain/app` still runs `scaffold.New(ModulePolicy, ...)` in the policy slot, and every module's `Limits` still reads
  x/authority's `GovernedLimit` (authority key `0x13`). x/authority still writes governed values at `0x13`.
- Nothing calls the writer methods or the version methods outside this directory's tests. The app wiring, the
  authority writer switch, the load gate, the per-block upgrade step, and the InitChain cross-checks are later work
  (goal-10 plan section 21, Wave 1).
- There is no BeginBlock: pending changes are stored but no code applies them yet (`ApplyDue` is Wave 1 work and depends
  on owner question Q1). No message can create a pending change or a plan today: the v0 protocol has no activation
  height and no upgrade payload.

Every claim below is about this package and its unit tests (real IAVL stores, fakes for the authority read surface).

The code in this directory (not `_test.go`, not `policytest/`) is chain code. It reads block data only from
`sdk.Context` (the block height; it reads no time), iterates stores in key order with a bound, and never reads the local
clock, randomness, the environment, files, or the network (`TestDeterministicSources`, the `msgkittest` source scan). It
reads the policy version and the blocked decision rows through the interfaces in `keeper.go` and never imports
x/authority, the other modules, the scaffold, `chain/upgrade`, the app, the devnet, the node, the command, or `agents/`,
in code or in tests (`TestKeeperImports`). It returns no validator updates and has no EndBlock (`TestModule`).

## Files

| File | Contents |
| --- | --- |
| `keeper.go` | package doc, constants, `VersionSource`, `DecisionSource`, `Deps`, `Keeper`, `WithStore`, the read surface |
| `writer.go` | council writer methods (`SetLimit`, `SchedulePending`, `ScheduleUpgrade`, `CancelUpgrade`) and version writers (`InitVersions`, `RecordUpgrade`) |
| `store.go` | prefixes, key builders, fixed-width codecs, store helpers |
| `events.go` | the untyped `policy_pending` and `upgrade_plan` events |
| `query.go` | `Query/Policy` |
| `service.go` | hand-written `svn.policy.v1.Query` service descriptor |
| `genesis.go` | `DefaultSafetyCaps`, `DefaultGenesisState`, `ParseGenesis`, `MarshalGenesis`, `ValidateGenesis`, `InitGenesis`, `ExportCheck`, `ExportGenesis` |
| `module.go` | `AppModule` (genesis, query service, consensus version, empty migration table) |
| `policytest/` | test support: `CheckInvariants`, `CheckInvariantsAt`, `StoreSnapshot`; imported only by `_test.go` files |

## Store layout

Store key `policy` (already mounted by the app). Every value is non-empty and has a constant size: the goleveldb backend
reports a key with an empty value as absent (`msgkit.LayoutVersionKey`). Names (limit keys, module names, upgrade names,
proposal ids) match `^[a-z0-9][a-z0-9._-]{0,63}$`, so they never contain `0x00`.

| Key | Value | Written by |
| --- | --- | --- |
| `0x00` | layout version 2, 8 bytes BE | `InitGenesis` (first write) |
| `0x01` | `svn.policy.v1.SafetyCaps`, deterministic protobuf | `InitGenesis` only; never changed |
| `0x02 \| limit_key` | governed value, 8 bytes BE, in 1..ceiling | `SetLimit` |
| `0x03 \| limit_key` | pending change, 88 bytes: activation height, value, policy version (8 bytes BE each), proposal id in a 64-byte zero-padded field | `SchedulePending`; deleted by `SetLimit` of the same key |
| `0x04` | app version, 8 bytes BE, at least 1 | `InitVersions`, `RecordUpgrade` |
| `0x05 \| module name` | consensus version, 8 bytes BE, at least 1 | `InitVersions`, `RecordUpgrade` |
| `0x06` | upgrade plan, 168 bytes: height (8 bytes BE), 32-byte release digest, name and proposal id in 64-byte zero-padded fields | `ScheduleUpgrade`; deleted by `CancelUpgrade` and `RecordUpgrade` |
| `0x07 \| upgrade name` | height at which the upgrade applied, 8 bytes BE, at least 1 | `RecordUpgrade` |

Bounds: `0x02` and `0x03` hold at most one row per governed limit key (five each); `0x04` and `0x06` are single keys;
`0x05` holds at most `MaxVersionMapModules` (32) rows; `0x07` grows by one row per applied upgrade.

## Read surface

| Method | Reads | Result | On damaged state |
| --- | --- | --- | --- |
| `GovernedLimit(ctx, key)` | `0x02 \| key` | the governed value; absent is not found and callers apply `msgkit.Effective` (the protocol ceiling) | panics when the value is not 8 bytes or not in 1..ceiling of a known key (`TestGovernedLimitReadSurface`) |
| `AppVersion(ctx)` | `0x04` | the app version; 0 when the row is absent (before `InitVersions`) | panics on a value that is not 8 bytes or is 0 |
| `VersionMap(ctx)` | `0x05` prefix, at most 33 iterator steps | the module version map, empty when no row exists | panics on a bad name, a bad value, or more than 32 rows (`TestVersionMapCodec`, `FuzzVersionMapDecode`) |
| `Plan(ctx)` | `0x06` | the plan and whether it exists | panics on a value that does not decode |
| `UpgradeApplied(ctx, name)` | `0x07 \| name` | whether that upgrade applied; a name outside the id pattern is false without a read | panics on a bad height |
| `SafetyCaps(ctx)` | `0x01` | the caps | an error when missing, undecodable, carrying unknown fields, or different from the protocol caps |

`GovernedLimit` has the contract of the authority read it is meant to replace (`chain/x/authority/read.go`), with the
panic text `policy: governed limit "<key>" is not an 8-byte value in 1..ceiling (<n> bytes stored)`. A transaction
handler panic becomes a failed transaction on every node alike; ABCI `Query` and the node gRPC server recover a query
panic into an error.

`WithStore(kv)` returns a keeper that reads and writes `kv` instead of the context's store. The load-time gate is meant to
use it on committed rows before any block context exists; the read methods accept a zero `sdk.Context` there
(`TestWithStore`).

## Writer methods

x/authority is meant to call these from an executed proposal's payload on the nested cache it writes back only on success
(`chain/x/authority/msg.go`), so a policy write never outlives a failed or kept proposal. Each method returns a
`msgkit.Code` and runs every check before its first write: a failure writes nothing and emits nothing (every reject row
of `TestSetLimitAndSchedulePending` and `TestPlanScheduleReplaceCancel` compares the store bytes and the event count). A
stored row that does not decode panics before any write (`TestDamagedPendingRowPanics`).

`h` is the block height of the context.

| Method | Checks in order (code) | Effect |
| --- | --- | --- |
| `SetLimit(ctx, key, value, proposalID)` | key a governed limit key and value in 1..ceiling (BOUNDS); proposal id in the id pattern (BOUNDS) | writes `0x02 \| key`; deletes `0x03 \| key` when present and emits `policy_pending` with `activation_height` 0 and the superseded proposal id |
| `SchedulePending(ctx, key, value, activation, proposalID, policyVersion)` | key and value (BOUNDS); proposal id (BOUNDS); `activation <= h` OUT_OF_RANGE; `activation - h > MaxActivationLeadBlocks` or `activation > 2^53-1` BOUNDS; `policyVersion < 2` OUT_OF_RANGE; a pending row of the key with a policy version `>= policyVersion` CONFLICT | writes `0x03 \| key`, replacing any pending row of the key; emits `policy_pending` (superseded id empty when nothing was replaced) |
| `ScheduleUpgrade(ctx, name, height, digest, proposalID)` | name and proposal id in the id pattern, digest 32 bytes (BOUNDS); the height window as above (OUT_OF_RANGE / BOUNDS); the name already applied DUPLICATE_ID; a stored plan of the same name with another digest CONFLICT | writes `0x06`, replacing a plan of another name or of the same name and digest; emits `upgrade_plan` with action `scheduled` or `replaced` |
| `CancelUpgrade(ctx, name)` | name in the id pattern (BOUNDS); no stored plan of that name NOT_FOUND | deletes `0x06`; emits `upgrade_plan` with action `cancelled` |

A safety cap name (`max_tx_bytes`, `max_gas_per_tx`) or any other key outside `msgkit.LimitKeys()` is BOUNDS: no writer can
change a safety cap. The latest executed decision for a key wins (DR-10-32): an immediate change deletes the key's
pending row in the same write, and a later scheduled change replaces it (`TestSetLimitSupersedesPending`). Each execute
produces a higher policy version, so `SchedulePending` refuses a replacement that does not carry one.

The writers re-check the height window themselves, whatever the caller checked. The minimum lead between an execute and a
plan height is owner question Q3 and is not implemented here; the writers require only "strictly after" and the PROPOSED
maximum lead.

### Version writers

| Method | Refuses (an error, nothing written) | Effect |
| --- | --- | --- |
| `InitVersions(ctx, vm, appVersion)` | app version 0; a version map with 0 or more than 32 modules, a name outside the id pattern, a version of 0, or a `policy` entry that differs from the layout row; a store already holding `0x04`, `0x05`, `0x06`, or `0x07` rows | writes `0x05` rows in module order and `0x04` |
| `RecordUpgrade(ctx, name, height, vm, appVersion)` | a name outside the id pattern; a height of 0 or not the block height; a name already applied; a stored plan with another name or height; no stored app version, or `appVersion` not the stored version + 1; a version map refused as above | replaces every `0x05` row, writes `0x04`, writes `0x07 \| name = height`, deletes `0x06` |

`InitVersions` is meant for the app's InitChain, after every module's `InitGenesis`; `RecordUpgrade` for the upgrade
runner in the block at the plan height. `RecordUpgrade` accepts an absent plan row, so a compiled plan source (owner
question Q2 (b)) can record an upgrade; when a plan row exists it must be this upgrade (`TestRecordUpgrade`,
`TestInitVersions`).

## Events

Untyped events (interim choice DR-10-22). They are not in the protocol event list (`docs/protocol.md`); decision request
DR-10-40 records them as event-list additions, and an indexer must not consume them until that is decided. No typed event
is emitted by this module.

| Event | Attributes | When |
| --- | --- | --- |
| `policy_pending` | `parameter_key`, `value`, `activation_height` (0 for immediate), `proposal_id`, `superseded_proposal_id` (empty if none) | `SchedulePending`; `SetLimit` when it deleted a pending row |
| `upgrade_plan` | `name`, `height`, `release_digest` (hex), `proposal_id`, `action` (`scheduled`, `replaced`, `cancelled`), `replaced_name` (empty unless `replaced`) | `ScheduleUpgrade`, `CancelUpgrade` |

No event type or attribute key matches the `bounds.json` forbidden-name patterns (`TestNoForbiddenNames`).

## Query

`svn.policy.v1.Query/Policy` (deviation D-9: the spec machine has no query semantics) returns the protocol v0
`PolicyState`: `policy_version` from `Deps.Versions`, `safety_caps` from `0x01`, and a copy of the blocked decision rows
from `Deps.Decisions`. The v0 `PolicyState` has no field for governed values, pending changes, the plan, or the app
version, so the query does not return them (`TestPolicyQuery`). A missing or damaged caps row is a plain error. The
response is bounded (three fields, eight decision rows). Raw `/store` queries stay unmetered, as for the other modules.

## Genesis, export, migrations

`ValidateGenesis` (strict protojson through `ParseGenesis`; unknown fields are errors): the policy state is present
(`{}` is refused); `policy_version` 1; `safety_caps` equal to `genesis.DefaultSafetyCaps()`; `blocked_decisions` exactly
one row per `genesis.DecisionIDs` entry, each unapproved, `value_version` 0, `schema_version` 1. No document can approve a
decision. `DefaultGenesisState` is that document; the document `chain/app` writes today decodes to it
(`TestGenesisValidateImportExport`, `FuzzPolicyGenesisJSON`).

`InitGenesis` needs an empty store (the scaffold key `[0x01]`, a layout key, or any row is refused) and writes the layout
version and the safety caps. The policy version and the decision rows live in the authority store; the app is meant to
cross-check the decision rows against the authority genesis in InitChain.

`ExportCheck` refuses, with `ErrExportRefused` naming the prefix, while the store holds a governed value (`0x02`), a
pending change (`0x03`), a plan (`0x06`), or an applied upgrade (`0x07`), while the app version is not 1 (absent included),
or while the policy version is not 1. The version map is derived from the binary and is not exported. `ExportGenesis`
runs `ExportCheck` and returns `policy_version` 1, the stored caps, and the authority's decision rows, and validates its
own result; `AppModule.ExportGenesis` panics on a refusal, so the app is meant to call `ExportCheck` first. This joins
DR-06-01: restart from an export is unsupported once any governance state exists (interim DR-10-04).

No migration is registered. Version 1 is the scaffold layout and has no step on purpose: `RunMigrations` from 1 fails
and `InitGenesis` refuses the scaffold key (`TestNoMigrationFromScaffoldLayout`). Homes from goal 09 and earlier are
re-initialized (DR-10-04, interim). The table must cover exactly 2 .. `ConsensusVersion`-1 (`TestMigrationTable`).

## Invariants

The goal-10 invariants (plan section 10) and who checks them. `policytest.CheckInvariantsAt(ctx, k, versions, layout)`
checks the keeper-level ones with `h` the context's block height; `CheckInvariants` uses `layout = ConsensusVersion`.
After an upgrade a caller passes the persisted version-map entry. `TestPolicyInvariantsSelfTest` injects one violation
per rule into a store that holds every kind of row; `TestCheckInvariantsAtLayout` shows a layout mismatch still fails.

| Invariant | Checked here | Elsewhere |
| --- | --- | --- |
| layout | the layout key holds `layout`; every row sits under a known prefix | |
| I1 policy version | | authority checker |
| I2 governed values | every `0x02` row names a governed key and holds 1..ceiling | |
| I3 pending changes | key known, decodes, value 1..ceiling, activation above `h`, stored policy version at most the current one | "not older than the latest executed change of its key": needs authority records |
| I4 plan | decodes, height above `h`, name not applied | |
| I5 app version | `0x04` and `0x05` exist together; app version = 1 + applied rows; applied heights in 1..`h` | equality with `ConsensusParams.Version.App` and the binary: app |
| I6 version map | names, versions at least 1, at most 32 rows; the `policy` entry equals the layout | equality with the binary's map and the other layout keys: app |
| I7 safety caps | the caps row equals the protocol caps | decision rows, council, devnet rows: authority checker |
| I8-I11 | | authority checker |
| I12 only allowed writers | | AST test and snapshots (app) |
| I13 upgrade self-check | | upgrade runner (app) |

## Bounds and PROPOSED constants

- `MaxActivationLeadBlocks` = 1,000,000 blocks: the largest distance between the writing block and a pending activation
  or a plan height. A chain safety constant, PROPOSED in decision request DR-10-15; not an owner decision and not
  fail-closed (a new number). Changing it is a binary upgrade.
- `MaxVersionMapModules` = 32: an implementation bound above the app's module count (eight), not a policy number.
- Heights above 2^53-1 are BOUNDS (the protocol's safe integer limit).
- No floor is set for governed values (DR-10-09, interim, not fail-closed for liveness): any value in 1..ceiling is
  accepted.

## Deviations from the spec machine

| Id | Here | Spec | Test |
| --- | --- | --- | --- |
| D-4 (storage part) | pending governed changes with an activation height; an immediate change deletes the key's pending row | no activation field for governed limits; a governed limit applies inside the execute (`spec_machine.py:1338-1343`) | `TestSetLimitAndSchedulePending`, `TestSetLimitSupersedesPending` |
| D-9 | `Query/Policy` registered with the v0 `PolicyState` | no query semantics | `TestPolicyQuery` |

D-4 is not reachable by any message yet: it needs the activation height of owner question Q1 (a), and applying a due
change needs the Wave 1 BeginBlock (D-8). The plan rows serve deviations D-5 and D-6, which belong to x/authority and the
app. The spec machine has no export; the export refusal is DR-06-01 and DR-10-04, not a spec deviation.

## Handoff

- `DecisionSource.BlockedDecisions(ctx)` has no implementation yet: x/authority must add a read of the blocked decision
  rows of its genesis config before the app can wire this keeper. `VersionSource` is satisfied by
  `authority.Keeper.PolicyVersion` today.
- The writer methods take an `sdk.Context` and return `msgkit.Code`, like `ProviderAdmission`
  (`chain/x/authority/hooks.go`); x/authority's `PolicyWriter` (plan section 7.2, no context, returns `Outcome`) needs a
  bound adapter, as `boundRegistry` does for the registry hooks.
- `Plan(ctx)` returns `policy.Plan`, not `chain/upgrade.Plan`: x/policy may not import `chain/upgrade` (plan section 3
  dependency direction), so the upgrade code converts.
- During a pending window the policy version names the executed decisions, not the limits in force; the limits in force
  are the governed values plus the pending rows with their heights (plan section 5.2).

## Limitations

- Not wired, and no BeginBlock (see "Status of this directory"). Every result above is unit level.
- The `release_digest` of a plan is information for operators: the chain cannot check which binary a node runs
  (DR-10-18). No review of any release is claimed.
- A damaged committed row panics in every message path that reads it (fail closed).
