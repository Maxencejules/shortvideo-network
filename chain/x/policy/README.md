# chain/x/policy

The policy module (goal 10). Package `policy`, module name `policy`, consensus version 2 (version 1 is the scaffold
slot; see "Genesis, export, migrations").

x/policy stores the fixed protocol safety caps, the governed resource bounds, pending governed changes, the upgrade plan,
the applied-upgrade history and the plan names, the persisted module version map, and the app version. It serves
`svn.policy.v1.Query/Policy` and has a BeginBlock that applies the pending governed changes whose height has come. It has
no message service: "There is no direct MsgUpdateLimits. Limit changes are proposal payloads executed by
svn.authority.v1.MsgExecuteChange." (`docs/protocol/proto/svn/policy/v1/tx.proto`). x/authority stays
the council and the policy version counter (decision request DR-10-20, interim); it is the only writer of the governed,
pending, and plan rows, through the writer methods below.

## Status of this directory

This is the goal-10 state of the package, built under the owner's answers of 2026-11-07: Q1 (DR-10-01) "Minimal
additive (Recommended)", Q2 (DR-10-02) "Council plan, fail closed (Recommended)", and Q3 (DR-10-03) "3 of 4 + min notice
(Recommended)". The two lead numbers of Q3 (`msgkit.MinActivationLeadBlocks` = 100 blocks, `msgkit.MaxActivationLeadBlocks`
= 1,000,000 blocks) remain PROPOSED constants: the owner chose the option text that contained them, not the numbers as
policy. The protocol fields this package reads and returns are those of `docs/protocol.md`, "Goal 10 additive revision".

The module is wired into `chain/app` (`chain/app/app.go:221-285`): the policy slot, every module's `Limits`, the
BeginBlocker order, the InitChain checks (`InitVersions`, the decision-row cross-check), the export refusal
(`ExportCheck` before the module manager export), and the upgrade runner; x/authority's `PolicyWriter` is this keeper,
bound to the payload's nested cache. Unless a line names an app-level or devnet test, a claim below is about this package
and its unit tests: real IAVL stores, with a fake in place of the authority read surface.

The code in this directory (not `_test.go`, not `policytest/`) is chain code. It reads block data only from
`sdk.Context` (the block height; it reads no time), iterates stores in key order with a bound, and never reads the local
clock, randomness, the environment, files, or the network (`TestDeterministicSources`, the `msgkittest` source scan). It
reads the policy version and the blocked decision rows through the interfaces in `keeper.go` and never imports
x/authority, the other modules, the scaffold, `chain/upgrade`, the app, the devnet, the node, the command, or `agents/`,
in code or in tests (`TestKeeperImports`). It has a BeginBlock, no EndBlock, and returns no validator updates
(`TestModule`).

## Files

| File | Contents |
| --- | --- |
| `keeper.go` | package doc, constants, `VersionSource`, `DecisionSource`, `Deps`, `Keeper`, `WithStore`, the read surface |
| `writer.go` | council writer methods (`SetLimit`, `SchedulePending`, `ScheduleUpgrade`, `CancelUpgrade`) and version writers (`InitVersions`, `RecordUpgrade`) |
| `apply.go` | `ApplyDue`, the BeginBlock body |
| `store.go` | prefixes, key builders, fixed-width codecs, store helpers |
| `events.go` | the untyped `policy_pending`, `policy_activated`, and `upgrade_plan` events |
| `query.go` | `Query/Policy` |
| `service.go` | hand-written `svn.policy.v1.Query` service descriptor |
| `genesis.go` | `DefaultSafetyCaps`, `DefaultGenesisState`, `ParseGenesis`, `MarshalGenesis`, `ValidateGenesis`, `InitGenesis`, `ExportCheck`, `ExportGenesis` |
| `module.go` | `AppModule` (genesis, query service, BeginBlock, consensus version, empty migration table) |
| `policytest/` | test support: `CheckInvariants`, `CheckInvariantsAt`, `CheckDecisions`, `StoreSnapshot`; imported by `_test.go` files and by the state check `chain/internal/statecheck`, which only the `svn_statecheck` build of svnd links |

## Store layout

Store key `policy` (already mounted by the app). Every value is non-empty and has a constant size: the goleveldb backend
reports a key with an empty value as absent (`msgkit.LayoutVersionKey`). Names (limit keys, module names, upgrade names,
proposal ids) match `^[a-z0-9][a-z0-9._-]{0,63}$`, so they never contain `0x00`.

| Key | Value | Written by |
| --- | --- | --- |
| `0x00` | layout version 2, 8 bytes BE | `InitGenesis` (first write) |
| `0x01` | `svn.policy.v1.SafetyCaps`, deterministic protobuf | `InitGenesis` only; never changed |
| `0x02 \| limit_key` | governed value, 8 bytes BE, in 1..ceiling | `SetLimit`, `ApplyDue` |
| `0x03 \| limit_key` | pending change, 88 bytes: activation height, value, policy version (8 bytes BE each), proposal id in a 64-byte zero-padded field | `SchedulePending`; deleted by `SetLimit` of the same key and by `ApplyDue` |
| `0x04` | app version, 8 bytes BE, at least 1 | `InitVersions`, `RecordUpgrade` |
| `0x05 \| module name` | consensus version, 8 bytes BE, at least 1 | `InitVersions`, `RecordUpgrade` |
| `0x06` | upgrade plan, 168 bytes: height (8 bytes BE), 32-byte release digest, name and proposal id in 64-byte zero-padded fields | `ScheduleUpgrade`; deleted by `CancelUpgrade` and `RecordUpgrade` |
| `0x07 \| upgrade name` | height at which the upgrade applied, 8 bytes BE, at least 1 | `RecordUpgrade` |
| `0x08 \| upgrade name` | the 32-byte release digest the name was first scheduled with | `ScheduleUpgrade`; never deleted |

Bounds: `0x02` and `0x03` hold at most one row per governed limit key (five each); `0x04` and `0x06` are single keys;
`0x05` holds at most `MaxVersionMapModules` (32) rows; `0x07` grows by one row per applied upgrade, and `0x08` by one row per
plan name that an executed council proposal first scheduled.

`0x08` is not in the plan's store table (plan section 7.3). It exists because the Q3 answer says a plan name can never be
reused with a different release digest, and `docs/protocol.md` refuses such a plan "whether that plan is pending,
cancelled, or replaced"; a cancelled or replaced plan leaves no `0x06` row, so the name and digest are kept here. That
strict reading is orchestrator choice ORCH-10 (a reversible implementation choice, not an owner decision).

## Read surface

| Method | Reads | Result | On damaged state |
| --- | --- | --- | --- |
| `GovernedLimit(ctx, key)` | `0x02 \| key` | the governed value; absent is not found and callers apply `msgkit.Effective` (the protocol ceiling) | panics when the value is not 8 bytes or not in 1..ceiling of a known key (`TestGovernedLimitReadSurface`) |
| `AppVersion(ctx)` | `0x04` | the app version; 0 when the row is absent (before `InitVersions`) | panics on a value that is not 8 bytes or is 0 |
| `VersionMap(ctx)` | `0x05` prefix, at most 33 iterator steps | the module version map, empty when no row exists | panics on a bad name, a bad value, or more than 32 rows (`TestVersionMapCodec`, `FuzzVersionMapDecode`) |
| `Plan(ctx)` | `0x06` | the plan and whether it exists | panics on a value that does not decode |
| `UpgradeApplied(ctx, name)` | `0x07 \| name` | whether that upgrade applied; a name outside the id pattern is false without a read | panics on a bad height |
| `SafetyCaps(ctx)` | `0x01` | the caps | an error when missing, undecodable, carrying unknown fields, or different from the protocol caps |

`GovernedLimit` has the contract of the authority read it replaced (`chain/x/authority/read.go`), with the
panic text `policy: governed limit "<key>" is not an 8-byte value in 1..ceiling (<n> bytes stored)`. A transaction
handler panic becomes a failed transaction on every node alike; ABCI `Query` and the node gRPC server recover a query
panic into an error.

`WithStore(kv)` returns a keeper that reads and writes `kv` instead of the context's store; the read methods accept a zero
`sdk.Context` there (`TestWithStore`). No code outside this package calls it today.

## Writer methods

x/authority calls these from an executed proposal's payload on the nested cache it writes back only on success
(`chain/x/authority/msg.go`), so a policy write never outlives a failed or kept proposal. Each method returns a
`msgkit.Code` and runs every check before its first write: a failure writes nothing and emits nothing (every reject row
of `TestSetLimitAndSchedulePending` and `TestPlanScheduleReplaceCancel` compares the store bytes and the event count). A
stored row that does not decode panics before any write (`TestDamagedPendingRowPanics`).

`h` is the block height of the context.

| Method | Checks in order (code) | Effect |
| --- | --- | --- |
| `SetLimit(ctx, key, value, proposalID)` | key a governed limit key and value in 1..ceiling (BOUNDS); proposal id in the id pattern (BOUNDS) | writes `0x02 \| key`; deletes `0x03 \| key` when present and emits `policy_pending` with `activation_height` 0 and the superseded proposal id |
| `SchedulePending(ctx, key, value, activation, proposalID, policyVersion)` | key and value (BOUNDS); proposal id (BOUNDS); `activation <= h` OUT_OF_RANGE; `activation - h > msgkit.MaxActivationLeadBlocks` or `activation > 2^53-1` BOUNDS; `policyVersion < 2` OUT_OF_RANGE; a pending row of the key with a policy version `>= policyVersion` CONFLICT | writes `0x03 \| key`, replacing any pending row of the key; emits `policy_pending` (superseded id empty when nothing was replaced) |
| `ScheduleUpgrade(ctx, name, height, digest, proposalID)` | name and proposal id in the id pattern, digest 32 bytes (BOUNDS); `height - h < msgkit.MinActivationLeadBlocks` (including `height <= h`) OUT_OF_RANGE; `height - h > msgkit.MaxActivationLeadBlocks` or `height > 2^53-1` BOUNDS; the name already applied DUPLICATE_ID; a `0x08` row of the name with another digest CONFLICT | writes `0x06`, replacing any stored plan; writes `0x08 \| name` the first time the name is scheduled; emits `upgrade_plan` with action `scheduled`, or `replaced` and the replaced plan's name |
| `CancelUpgrade(ctx, name)` | name in the id pattern (BOUNDS); no stored plan of that name NOT_FOUND | deletes `0x06` (the `0x08` row stays); emits `upgrade_plan` with action `cancelled` |

A safety cap name (`max_tx_bytes`, `max_gas_per_tx`) or any other key outside `msgkit.LimitKeys()` is BOUNDS: no writer can
change a safety cap. The latest executed decision for a key wins (DR-10-32): an immediate change deletes the key's
pending row in the same write, and a later scheduled change replaces it (`TestSetLimitSupersedesPending`). Each execute
produces a higher policy version, so `SchedulePending` refuses a replacement that does not carry one.

A plan name is never reused with another release digest: the first plan of a name stores its digest under `0x08` for good,
so a later plan of that name with another digest is CONFLICT whether the earlier plan is pending, was replaced, or was
cancelled; the same name with the same digest may be scheduled again after a cancel (`TestPlanScheduleReplaceCancel`).
This strict reading of the Q3 text, with the `0x08` row and DUPLICATE_ID for an applied name, is orchestrator choice
ORCH-10, a reversible implementation choice, not an owner decision.

The writers re-check the height window themselves, whatever the caller checked: x/authority checks the same windows at
propose and execute (P2b). The lead numbers come from `chain/msgkit` (`lead.go`), defined once for both modules; both are
PROPOSED. Using the maximum lead for a governed-limit activation height is an interim implementation choice (DR-10-15),
not an owner decision.

### Version writers

| Method | Refuses (an error, nothing written) | Effect |
| --- | --- | --- |
| `InitVersions(ctx, vm, appVersion)` | app version 0; a version map with 0 or more than 32 modules, a name outside the id pattern, a version of 0, or a `policy` entry that differs from the layout row; a store already holding `0x04`, `0x05`, `0x06`, `0x07`, or `0x08` rows | writes `0x05` rows in module order and `0x04` |
| `RecordUpgrade(ctx, name, height, vm, appVersion)` | a name outside the id pattern; a height of 0 or not the block height; a name already applied; no stored plan, or a stored plan with another name or height; no stored app version, or `appVersion` not the stored version + 1; a version map refused as above | replaces every `0x05` row, writes `0x04`, writes `0x07 \| name = height`, deletes `0x06` |

`InitVersions` is meant for the app's InitChain, after every module's `InitGenesis`; `RecordUpgrade` for the upgrade
runner in the block at the plan height. Under the Q2 answer an upgrade applies only from a plan that an executed council
proposal stored, so `RecordUpgrade` refuses when no plan row exists, and the stored plan must be this upgrade
(`TestRecordUpgrade`, `TestInitVersions`).

## BeginBlock: applying pending changes

`ApplyDue(ctx)` is the BeginBlock body (deviation D-4, activation part). At block height `b` it applies every pending
change whose activation height is at or below `b`, in key order: the value becomes the governed value of its key (`0x02`),
the pending row is deleted, and `policy_activated` is emitted. Block `a - 1` reads the old value and block `a`, with every
transaction in it, reads the new one (`TestApplyDueOrderAndBounds`: effective-1 at 19, effective at 20, key order of the
events, a second call at the same height does nothing). The policy version does not move at activation: the execute
that scheduled the change moved it.

ApplyDue reads every pending row before its first write. There is at most one pending row per governed key, so the walk
is bounded by five rows (at most six iterator steps). A row that does not decode, names a key that is not a governed key,
holds a value above its ceiling, or a sixth row, is an error: nothing is written or emitted and the error is returned.
The module's BeginBlock returns it, so FinalizeBlock fails on every node alike and the block is not committed (fail
closed). The app must order this module's BeginBlock before x/leases (deviation D-8), so a value activated at height `a`
governs that block's lease expiry; that ordering is the app's.

## Limits in force and the policy version

The limits in force at height `h` are the governed values (`0x02`, or the key's ceiling where none is set) with the
pending rows (`0x03`) applied at their activation heights. The policy version names the executed decisions, not the limits
in force: a scheduled change moves the policy version when it executes, and its value applies later. During a pending
window a lease records the new policy version while the older value still governs it (plan section 5.2, finding SS-12).
`Query/Policy` returns both parts, so a client can compute the limit at any later height.

## Events

Untyped events (interim choice DR-10-22). They are not in the protocol event list (`docs/protocol.md`); decision request
DR-10-40 records them as event-list additions, and an indexer must not consume them until that is decided. No typed event
is emitted by this module.

| Event | Attributes | When |
| --- | --- | --- |
| `policy_pending` | `parameter_key`, `value`, `activation_height` (0 for immediate), `proposal_id`, `superseded_proposal_id` (empty if none) | `SchedulePending`; `SetLimit` when it deleted a pending row |
| `policy_activated` | `parameter_key`, `value`, `activation_height`, `proposal_id` | `ApplyDue`, one per applied change, in key order |
| `upgrade_plan` | `name`, `height`, `release_digest` (hex), `proposal_id`, `action` (`scheduled`, `replaced`, `cancelled`), `replaced_name` (empty unless `replaced`) | `ScheduleUpgrade`, `CancelUpgrade` |

No event type or attribute key matches the `bounds.json` forbidden-name patterns (`TestNoForbiddenNames`).

## Query

`svn.policy.v1.Query/Policy` (deviation D-9: the spec machine has no query semantics) returns the `PolicyState` of protocol
schema version 2 at the query height:

| Field | Source |
| --- | --- |
| `policy_version` | `Deps.Versions` (the authority counter) |
| `safety_caps` | `0x01` |
| `blocked_decisions` | a copy of `Deps.Decisions` rows (x/authority's `BlockedDecisions`, P2b) |
| `governed_limits` | all five governed keys in key order, each with its value in force: the `0x02` value, or the ceiling when none is set (`msgkit.Effective`) |
| `pending_limit_changes` | every `0x03` row in key order: key, value, activation height, proposal id, and the policy version its execute produced |
| `upgrade_plan` | the `0x06` plan (name, height, SHA-256 release digest, proposal id); absent when none |
| `app_version` | `0x04`; 0 when absent |

The response is bounded: five governed values, at most five pending changes, at most one plan, eight decision rows. A
missing caps row, or a damaged governed, pending, plan, or app version row, is a plain error, never a panic
(`TestPolicyQuery`). Raw `/store` queries stay unmetered, as for the other modules.

## Genesis, export, migrations

`ValidateGenesis` (strict protojson through `ParseGenesis`; unknown fields are errors): the policy state is present
(`{}` is refused); the query fields of schema version 2 are empty (`governed_limits`, `pending_limit_changes`, and
`upgrade_plan` unset, `app_version` 0); `policy_version` 1; `safety_caps` equal to `genesis.DefaultSafetyCaps()`;
`blocked_decisions` exactly one row per `genesis.DecisionIDs` entry, each unapproved, `value_version` 0, `schema_version`
1. No document can approve a decision. A genesis document carries no governed value, pending change, or plan
(`docs/protocol.md`); the app version is written from the binary by `InitVersions`, not taken from the document.
`DefaultGenesisState` is that document; the document `chain/app` writes today decodes to it
(`TestGenesisValidateImportExport`, `FuzzPolicyGenesisJSON`).

`InitGenesis` needs an empty store (the scaffold key `[0x01]`, a layout key, or any row is refused) and writes the layout
version and the safety caps. The policy version and the decision rows live in the authority store; the app cross-checks
the decision rows against the authority genesis in InitChain (`chain/app/state.go`, `validatePolicyGenesis`).

`ExportCheck` refuses, with `ErrExportRefused` naming the prefix, while the store holds a governed value (`0x02`), a
pending change (`0x03`), a plan (`0x06`), an applied upgrade (`0x07`), or a plan name (`0x08`, which a cancelled plan
leaves), while the app version is not 1 (absent included), or while the policy version is not 1. The version map is
derived from the binary and is not exported. `ExportGenesis` runs `ExportCheck` and returns `policy_version` 1, the stored
caps, and the authority's decision rows, and validates its own result; `AppModule.ExportGenesis` panics on a refusal, so the app calls `ExportCheck` first (`chain/app/app.go`, export). This joins
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
| layout | the layout key holds `layout`; every row sits under a known prefix (`0x00`..`0x08`) | |
| I1 policy version | | authority checker |
| I2 governed values | every `0x02` row names a governed key and holds 1..ceiling | |
| I3 pending changes | state-only form: key known, decodes, value 1..ceiling, activation above `h`, stored policy version at most the current one. Exact form (`CheckDecisions`): for the latest executed decision per key that a harness counted, an immediate or due decision left no pending row and set the governed value, and a decision not yet due is the pending row (activation, value, policy version, proposal id); a key without a decision has neither row | the state-only "not older than the latest executed change" needs authority records |
| I4 plan | every `0x08` row has a valid name and a 32-byte digest; the plan decodes, its height is above `h`, its name is not applied, and its `0x08` row holds its digest; every applied name has a `0x08` row | |
| I5 app version | `0x04` and `0x05` exist together; app version = 1 + applied rows; applied heights in 1..`h` | equality with `ConsensusParams.Version.App` and the binary: app |
| I6 version map | names, versions at least 1, at most 32 rows; the `policy` entry equals the layout | equality with the binary's map and the other layout keys: app |
| I7 safety caps | the caps row equals the protocol caps | decision rows, council, devnet rows: authority checker |
| I8-I11 | | authority checker |
| I12 only allowed writers | | AST test and snapshots (app) |
| I13 upgrade self-check | | upgrade runner (app): every x/policy row except the layout key, the app version, the version map, and the plan, which the runner checks itself, is in an I13 class (`chain/app/upgrades.go` `rowClasses`) |

`h` is the last committed height, or the height of a block whose BeginBlock already ran on the checked state; checked at
`b` on state committed at `b - 1`, a row due at `b` is reported as an I3 violation. `TestCheckDecisionsSelfTest` injects one
violation of the exact I3 form per rule. `FuzzGovernedChangeSequence` (keeper level) drives random sequences of immediate
and scheduled changes, plans, cancels, other executes, BeginBlocks, and the upgrade record at a plan height against a model,
and checks every writer code, unchanged bytes and no event on every refusal, `CheckInvariants`, and `CheckDecisions` after
each step; `TestGovernedChangeSequenceReachesEveryOutcome` shows its seeds reach every writer outcome, an activation at its
exact height and a late one, and an upgrade record.

## Bounds and PROPOSED constants

- `msgkit.MinActivationLeadBlocks` = 100 blocks: the smallest distance between the writing block and a plan height.
  PROPOSED (Q3, option "3 of 4 + min notice (Recommended)"; DR-10-03); not approved policy and not a live governed value.
- `msgkit.MaxActivationLeadBlocks` = 1,000,000 blocks: the largest distance between the writing block and a pending
  activation or a plan height. PROPOSED (the same option text; DR-10-15); applying it to governed-limit activations is an
  interim implementation choice (DR-10-15), not an owner decision. Changing either number is a binary upgrade.
- `MaxVersionMapModules` = 32: an implementation bound above the app's module count (eight), not a policy number.
- Heights above 2^53-1 are BOUNDS (the protocol's safe integer limit).
- No floor is set for governed values (DR-10-09, interim, not fail-closed for liveness): any value in 1..ceiling is
  accepted.

## Deviations from the spec machine

| Id | Here | Spec | Test |
| --- | --- | --- | --- |
| D-4 | pending governed changes with an activation height; an immediate change deletes the key's pending row; BeginBlock applies a due change at its height | no activation field for governed limits; a governed limit applies inside the execute (`spec_machine.py:1338-1343`); a `governedLimit` carrying `activationHeight` is MALFORMED at parse | `TestSetLimitAndSchedulePending`, `TestSetLimitSupersedesPending`, `TestApplyDueOrderAndBounds` |
| D-8 (module part) | the BeginBlock applies due changes before any transaction of the block | no policy BeginBlock | `TestModule`, `TestApplyDueOrderAndBounds`; the ordering before x/leases is the app's |
| D-9 | `Query/Policy` registered with the schema version 2 `PolicyState` | no query semantics | `TestPolicyQuery` |

The plan and plan-name rows serve deviations D-5 and D-6, which belong to x/authority (the new payload kinds) and the app
(the per-block upgrade step). The spec machine has no export; the export refusal is DR-06-01 and DR-10-04, not a spec
deviation.

## Limitations

- The claims of this file are unit level unless a line says otherwise. The app-level and devnet behaviour of activation
  is `TestActivationTiming` (chain/app) and `TestDevnetGovernedChangeTiming` (single-machine four-process devnet, not
  independent operators); the upgrade plan rows at app level are `TestUpgradeRehearsalInProcess`.
- The `release_digest` of a plan is information for operators: the chain cannot check which binary a node runs
  (DR-10-18). No review of any release is claimed.
- A damaged committed row panics in every message path that reads it, makes BeginBlock fail the block, and makes the query
  return an error (fail closed).
