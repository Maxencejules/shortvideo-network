# chain/x/authority

Validator membership and council changes (Goal 06). This module is the only one that returns validator updates. `InitialUpdates` returns the genesis set; EndBlock returns a changeset when a scheduled change is due.

Goal 07 connected the council admit, disable and force-withdraw payloads to `x/providers` and `x/content` through registry hooks. Provider and content records live in those modules, not here.

Goal 10 added, in two steps:

- Without a protocol change: typed governed-limit checks at propose (D-1), the typed execute events (D-2), council and consensus key separation in both directions (D-7), the seat id cap at genesis (D-10), `BumpPolicyVersionForUpgrade`, `svn.authority.v1.Query/Governance` (D-9), and payload vectors.
- With the schema version 2 protocol revision (`docs/protocol.md`, "Goal 10 additive revision"): on 2026-11-07 the owner answered goal 10 question Q1 (DR-10-01) with "Minimal additive (Recommended)", Q2 (DR-10-02) with "Council plan, fail closed (Recommended)", and Q3 (DR-10-03) with "3 of 4 + min notice (Recommended)". The numbers in the Q3 option, `MinActivationLeadBlocks` = 100 blocks and `MaxActivationLeadBlocks` = 1,000,000 blocks (`chain/msgkit/lead.go`), remain PROPOSED: the owner chose the option text, not the numbers as policy. This package now executes a governed limit with an activation height and the `software_upgrade`, `cancel_upgrade` and `council_seat` payloads (D-4, D-5). Governed values, pending governed changes and upgrade plans are `x/policy` state, written only through the `PolicyWriter` hook on the nested cache of an executed proposal. The store layout moved to version 2 (layout key `0x00`, `ConsensusVersion` 2; the governed-value prefix `0x13` is retired). The auditor option was not chosen, so there is no auditor payload (DR-09-08 stays DEFERRED).

See [Goal 10 deviations](#goal-10-deviations-from-the-spec-machine).

## Messages

| Message | Signer | Effect |
| --- | --- | --- |
| `MsgProposeChange` | A devnet council seat | Opens a proposal with a validator power change, a governed limit (applied at execute, or scheduled at an activation height), a provider admit or disable, a force-withdraw, a software upgrade plan, an upgrade cancel, or a council seat key replacement payload. |
| `MsgApproveChange` | A devnet council seat | Adds the seat's approval. The third of four seats queues the proposal, for every payload kind (no per-kind threshold). |
| `MsgExecuteChange` | The `AUTHORITY` submitter | Applies a queued proposal: schedules a validator change; sets or schedules a governed limit, stores or cancels an upgrade plan, through `x/policy` (`PolicyWriter`); replaces a council seat key and creates the new key's account (`Accounts`); or calls the registry hook of an admit, disable or force-withdraw payload. A success emits `svn.events.v1.PolicyChanged` and `svn.events.v1.ProposalExecuted` (D-2). |

Provider messages (`RegisterProvider`, `UpdateProvider`, `RotateProviderKey`, `DrainProvider`) are `x/providers` handlers, and content messages are `x/content` handlers; this module has none. Goal 06 made provider messages `ERROR_CODE_FORBIDDEN` at the router until Goal 07 (D1); Goal 07 ended that. There is no single-signature admit, disable, force-withdraw, upgrade or seat message (DEC-009). A council seat key is replaced only by an executed `council_seat` payload (three of four seats and the review delay); `RotateCouncilSeat` stays as an engine-only entry point that runs the same checks and writes (`replaceSeat`) without creating an account. Service account rotation is engine-only (`RotateServiceSubmitter`); no message reaches it.

A seat key has no submitter row and no role. The ante admits its account because the address is derived from a seat key in the config (`AdmissionOf`), and lets it sign only `MsgProposeChange` and `MsgApproveChange`; any other message in a seat-signed transaction is `ERROR_CODE_FORBIDDEN` before the sequence moves (owner ruling DR-06-06, 2026-11-02). Genesis validation (`ValidateGenesisState`, and `genesis.Validate` on the document) refuses a seat address in the submitter list with any role, `BROADCASTER` included (`ERROR_CODE_FORBIDDEN`; DR-07-11 option 2). Spec `validate_genesis_view` has no such rule; it is stricter than the spec. A seat replacement changes the key in the config, so the admission moves with it and no row is written: the ante stops admitting the old key's account and admits the new one.

`docs/architecture/authority-authorization.md` is the authorization map. `docs/operations/validator-lifecycle.md` is the operator procedure.

## Store layout

One prefix byte, then the fields that name the object. Every id in a key matches `^[a-z0-9][a-z0-9._-]{0,63}$` and is checked before any write.

Layout version 2 (`ConsensusVersion`, goal 10). Layout version 1 (goals 06 to 09) had no layout key and kept governed values at `0x13`.

| Key | Value | Written by | Read by |
| --- | --- | --- | --- |
| `0x00` | Layout version (`ConsensusVersion`, 2), 8 bytes big-endian (`msgkit.LayoutVersionKey`) | InitGenesis (first write) | export (must hold 2; not exported); the app's load check |
| `0x01` | Deterministic protobuf `GenesisState`: validators, submitters, devnet test seats, quorum loss `UNSET`, blocked decisions, devnet test parameters. Proposals, pending entries, and the approved council are always empty here. | InitGenesis; EndBlock when the set changes; Execute of a council seat payload; engine-only rotations | every message; EndBlock when any entry is pending; export; `DevnetRole`; `BlockedDecisions`; `Query/Governance`; ante (`Admission`, for a signer without a `0x02` row) |
| `0x02 \|\| addr(20)` | One byte, the submitter role. A seat address has none. | InitGenesis; engine-only service rotation | ante (`Admission`); export; `Role` |
| `0x03 \|\| kind(1) \|\| point(8, big-endian) \|\| proposal_id` | Deterministic protobuf `PendingValidatorPower`. Kind `0x01` is a height, `0x02` a unix time, so key order is the spec activation order. | Execute of a validator payload; InitGenesis; EndBlock deletes applied entries | Execute of a validator payload (projection); EndBlock; export |
| `0x04` | Policy version, 8 bytes big-endian. Absent means 1. | Execute; `BumpPolicyVersionForUpgrade` (the upgrade runner only) | Propose; Execute; `PolicyVersion`; export check |
| `0x10 \|\| proposal_id` | Proposal record, JSON with a fixed field order and no omitted field. A force-withdraw record keeps its content id, version, and the 32-byte reason digest value; schema version 2 records keep the activation height, the upgrade name, height and release digest, or the seat id and new key. | Propose; Approve; Execute | Propose (duplicate check); Approve; Execute |
| `0x11 \|\| business_id` | 32-byte business digest | each successful message that is not an idempotent retry | each message (its own id); `BusinessDigest` |
| `0x12 \|\| parameter_key` | 32-byte payload digest of the last executed change | Execute | Execute, stale branch only |
| `0x13` | Retired: layout version 1 governed values. Governed values are `x/policy` state (its `0x02`, pending changes `0x03`). | none | export check (refused) |
| `0x14 \|\| base_policy_version(8, big-endian) \|\| seat_id` | 8-byte big-endian count of the seat's `OPEN` or `QUEUED` proposals on that base version. Deleted at 0. | Propose (+1); every transition out of `OPEN` or `QUEUED` (-1, at the record's base) | Propose (its own seat, current version only); those transitions |
| `0x15 \|\| old_pub(32)` | Seat id | Execute of a council seat payload; `RotateCouncilSeat` (both through `replaceSeat`) | seat replacement (the new key may not be retired); Execute of a validator payload (D-7, last check); export check |
| `0x16 \|\| old_addr(20)` | `0x01` | `RotateServiceSubmitter` | that rotation; export check |
| `0x17` | Reserved: round 3 provider rows. Never written since Goal 07; provider records are `x/providers` state. | none | export check (refused) |
| `0x20` | The round-2 side document. Never written. | none | EndBlock guard; export check |

## What each message reads and writes

| Message | Reads | Writes on success | Writes on a keep outcome |
| --- | --- | --- | --- |
| Propose | `0x01`, its `0x11`, its `0x10` (exists?), `0x04`, its seat's `0x14` at the current version | its `0x10`, its `0x11`, its seat's `0x14` (+1) | none |
| Approve | `0x01`, its `0x11`, its `0x10`; `EXPIRED` keep: the proposer's `0x14` at the record's base | its `0x10`, its `0x11` | `EXPIRED`: its `0x10`, the proposer's `0x14` at the record's base (-1) |
| Execute | `0x01`, its `0x11`, its `0x10`, `0x04`; stale branch: its `0x12`; validator payload: the `0x03` prefix (at most 128 entries) and, when the projection and the pending cap pass, the `0x15` key of its consensus key (D-7); council seat payload: the `0x15` key of the new key and the `0x03` prefix (D-7); success, or a keep from `OPEN` or `QUEUED`: the proposer's `0x14` at the record's base. A registry, governed limit, upgrade or cancel payload reads no other authority key; its hook reads and writes `x/providers`, `x/content` or `x/policy`. | one `0x03` entry (validator), or `0x01` and the old key's `0x15` (council seat), or nothing more (hook payloads); its `0x12`; `0x04`; its `0x10`; its `0x11`; the proposer's `0x14` (-1) | `EXPIRED` or `REJECTED`: its `0x10`; the proposer's `0x14` (-1) only from `OPEN` or `QUEUED` |
| EndBlock | `0x20` (guard); the `0x03` prefix; `0x01` when any entry is pending | `0x01` (new set); deletes the applied `0x03` keys | none; a failure halts |

A rejected message writes nothing. The engine writes the authority store only on a success or keep path, after every check that can fail, through one batch, and the keeper runs it on a cache branch that it drops on a reject. A council seat payload stages its config and retired-key writes in that batch. A business gate whose id is absent from `0x11` also reads one key in each peer ledger (`x/content` `0x03`, `x/leases` `0x03`, `x/providers` `0x02`).

## Council payloads and hooks

Every kind needs three distinct seats of four and the DEC-008 review delay (no per-kind threshold), binds its parameter key at propose (`ERROR_CODE_MALFORMED` on a mismatch), and on success bumps the policy version and emits the D-2 events. "Height" is the height of the block that runs the transaction; MinLead and MaxLead are `msgkit.MinActivationLeadBlocks` (100) and `msgkit.MaxActivationLeadBlocks` (1,000,000), both PROPOSED.

| Payload | Parameter key | Propose checks after the v0 payload checks | Execute checks and effect (any code other than `FORBIDDEN` leaves the proposal `QUEUED`) | Hook |
| --- | --- | --- | --- | --- |
| `validator_power` | `validator-<node_id>` | activation strictly in the future (`OUT_OF_RANGE`) | projection, pending cap, D-7; schedules a pending power change | none |
| `governed_limit`, activation 0 | `<limit_key>` | key in the msgkit table and value in 1..ceiling (`BOUNDS`, D-1) | key and value again (`BOUNDS`); `PolicyWriter.SetLimit`: the value applies at once and any pending change of the key is deleted | `x/policy` |
| `governed_limit`, activation a > 0 (schema 2) | `<limit_key>` | D-1, then `height < a <= height + MaxLead` (`OUT_OF_RANGE` at or below, `BOUNDS` above or above 2^53-1) | key and value; the same window against the executing block; `OVERFLOW` at policy version 2^64-1; `PolicyWriter.SchedulePending` with the policy version the execute produces; `x/policy` applies it in BeginBlock at a (D-4) and replaces any pending change of the key | `x/policy` |
| `software_upgrade` (schema 2) | `upgrade` | name on the id pattern (`BOUNDS`) and a SHA-256 (`MALFORMED`) 32-byte (`BOUNDS`) release digest, at parse; then `height + MinLead <= H <= height + MaxLead` and `H <= 2^53-1` (`OUT_OF_RANGE` below, `BOUNDS` above) | the same window against the executing block; an applied name `DUPLICATE_ID` (`PolicyWriter.UpgradeApplied`); `PolicyWriter.ScheduleUpgrade`: a name an earlier plan used with another digest is `CONFLICT` (pending, replaced or cancelled; `x/policy` keeps the first digest of every name), otherwise the plan is stored and replaces any pending plan | `x/policy` |
| `cancel_upgrade` (schema 2) | `upgrade` | name on the id pattern (`BOUNDS`) | `PolicyWriter.CancelUpgrade`: no pending plan of that name `NOT_FOUND`; deletes the plan | `x/policy` |
| `council_seat` (schema 2) | `seat-<seat_id>` | seat id on the id pattern and a 32-byte key (`BOUNDS`), at parse | `replaceSeat`, in order: absent seat `NOT_FOUND`; another seat's key `DUPLICATE_ID`; the seat's own key or a retired key `CONFLICT`; a key whose address is a submitter `DUPLICATE_ID`; a current or pending validator consensus key `DUPLICATE_ID` (D-7); then `Accounts.EnsureAccount` for the new key's address. Effect: the seat keeps its id with the new key, the old key is retired (`0x15`) for good, the ante admits the new key's account and stops admitting the old one, and the version bump makes every proposal in flight unexecutable | accounts |
| `admit_provider` | `admit-<id>` | id pattern | `devnet` = DEC-002 is the labelled devnet test parameter with role `DEVNET_TEST_ONLY` on a `shortvideo-dev-` chain id | `ProviderAdmission.AdmitByCouncil(ctx, id, devnet)` |
| `disable_provider` | `disable-<id>` | id pattern | none | `ProviderAdmission.DisableByCouncil(ctx, id)` |
| `force_withdraw` | `withdraw-<id>` | id pattern, version, reason digest | `devnet` = the same rule for DEC-003 | `ContentRemoval.ForceWithdrawByCouncil(ctx, id, version, reason, devnet)` with the content id, version and reason digest value from the `0x10` record |

- `State.Registry`, `State.Policy` and `State.Accounts` are interfaces. `Keeper.transition` binds them after `LoadState` to the keepers set with `Keeper.WithRegistry`, `Keeper.WithPolicy` and `Keeper.WithAccounts`. Without the hook a payload needs, the payload is `ERROR_CODE_MALFORMED` at execute (`TestHooklessExecuteIsMalformed`).
- A hook runs every check before its first write. `ERROR_CODE_FORBIDDEN` marks the proposal `REJECTED` (keep path); no policy or account hook returns it. Any other code (`NOT_FOUND`, `DECISION_BLOCKED`, `CONFLICT`, `DUPLICATE_ID`, `WITHDRAWN`, `STALE_VERSION`, ...) fails the message and the proposal stays `QUEUED`; it can execute later. A hook cannot ask for a keep or an idempotent result (`registryOutcome`).
- The hooks run inside `applyPayload`, before the engine's last checks (`ERROR_CODE_OVERFLOW` at policy version 2^64-1, an `ERROR_CODE_INTEGRITY` open count or record). So the keeper binds every hook to one second cache nested in the message branch, with its own event manager. A success writes it back, then the message branch. A keep writes only the message branch. A failure drops both. A hook write (a provider or content record, a governed value, a pending change, a plan, a new account) and its events (`policy_pending`, `upgrade_plan`) never outlive the proposal outcome (`TestKeeperRegistryWriteDroppedOnLateFailure`, `TestPolicyWriterFailureWritesNothing`, `TestCouncilSeatPayload`, `TestHookWriteDroppedOnRejectedKeep`).
- A registry entry is a coordination record. Admission is not evidence of an independent operator; the DEC-002 membership policy stays blocked and `operator_group_id` is a label.
- The `release_digest` of a plan is information for operators: the chain stores it and does not compare it with the running binary. A plan names a height only. Off a `shortvideo-dev-` chain every council message is `DECISION_BLOCKED`, so no plan can be written there.

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
| `PolicyVersion(ctx)` | `0x04` | the committed policy version the engine loads: 1 while the key is absent (before the first executed proposal), then the stored value; it grows by one with each executed proposal and each applied upgrade (`x/policy` `VersionSource`) | panics when the value is not 8 bytes or is below 2, exactly where the engine's own read is `ERROR_CODE_INTEGRITY` |
| `BlockedDecisions(ctx)` | `0x01` | the config's blocked decision rows in stored order, decoded for the call (`x/policy` `DecisionSource`: `Query/Policy` and the policy export read them; `x/policy` stores no copy) | panics when the config is missing or does not decode |
| `BusinessDigest(ctx, id)` | `0x11 \|\| id` | a copy of the business digest authority committed for `id` (implements `msgkit.BusinessSource`); an id outside the pattern is not found without a read | panics when the value is not 32 bytes |
| `Admission(ctx, addr)` | `0x02 \|\| addr`, then `0x01` when there is no row | `AdmittedSubmitter` (admission row), `AdmittedSeat` (the account of a seat key in the config, no row), or `NotAdmitted` (ante decorator, DR-06-06). It reads the config only for a signer without a row, so a submitter's ante gas is unchanged. | `NotAdmitted` for a seat when the config is missing, does not decode, or has the old layout |

x/providers reads `Role`; x/content also reads `DevnetRole`. x/leases (Goal 08) reads `Role`, `DevnetNumeric` (the DEC-005 devnet test value, a devnet stand-in, not policy), and `PolicyVersion`. x/evidence (Goal 09) reads `Role` (only `AUDITOR` may submit audit batches). x/policy reads `PolicyVersion` and `BlockedDecisions`. Goal 10 moved governed limit values to `x/policy`: `authority.Keeper.GovernedLimit` no longer exists, and the registries, x/leases and x/evidence read `GovernedLimit` from the `x/policy` keeper (the app wires it), which keeps the same fail-closed contract (a damaged value panics; `x/policy` `TestGovernedLimitReadSurface`). This module reads the governed `max_page_limit` from `x/policy` through `PolicyKeeper` for `Query/Governance`.

The governed key set and ceilings come from one table, `msgkit.Ceiling` (pinned to `bounds.json` by `chain/msgkit/bounds_test.go`); this package has no copy of it. `PolicyVersion` grows by one with each executed proposal and with each `BumpPolicyVersionForUpgrade` call.

`BusinessDigest`, `PolicyVersion` and `BlockedDecisions` fail closed: no id is reported free, no version 1 is reported on damaged state (`TestPolicyVersionRead`), and no empty decision list is reported for a missing or undecodable config. Only `ExecuteChange`, `BumpPolicyVersionForUpgrade` (`0x04` only), InitGenesis, EndBlock and the message handlers write these keys, so such a value is integrity damage. The SDK turns a handler panic into a failed transaction on every node; ABCI `Query` recovers a panic into an error result, and the node gRPC server wraps every query in a recovery interceptor, so a query fails and the node keeps running. The halt on a damaged `max_expiry_steps` value (`TestDamagedExpiryLimitHalts`, chain/app) now depends on the `x/policy` read.

`DevnetRole` and `DevnetNumeric` report not found on a missing, undecodable, or old-layout config, so a caller fails closed with `DECISION_BLOCKED` (`TestDevnetNumeric`).

## Goal 10 deviations from the spec machine

Each one is stricter than `spec_machine.py`, adds an event or a query, or accepts a schema version 2 payload the frozen spec does not know. None adds a code number. D-3 was withdrawn in plan revision 2; D-6 (the PreBlock upgrade step) is `chain/upgrade` and the app, and D-8 (policy BeginBlock before lease expiry) is `x/policy`.

| Id | Chain | Spec | Test |
| --- | --- | --- | --- |
| D-1 (DR-10-21) | A governed limit whose key is not in the msgkit table (`bounds.json` `governed_limit_caps`), or whose value is above its ceiling, is `ERROR_CODE_BOUNDS` at propose, right after the payload checks (parse, digest, digest algorithm, parameter key binding, validator activation) and before `check_time`, the business gate, the duplicate id, expiry, base version and quota checks. Safety-cap names (`max_tx_bytes`, `max_gas_per_tx`) and quota names are refused the same way. Execute checks key and value again before the policy writer is called. | Accepted at propose; `BOUNDS` at execute with the proposal left `QUEUED` (`spec_machine.py:1338-1343`). | `TestGovernedLimitTypedAtProposeD1`, `TestSafetyCapsNotGovernable`, `TestExecuteRechecksCeiling`, `TestKeeperInvalidBounds`, `TestPayloadVectors` |
| D-2 (DR-10-22) | A successful execute of any kind emits `svn.events.v1.PolicyChanged{policy_version, parameter_key}` and then `svn.events.v1.ProposalExecuted{proposal_id, policy_version}`, both with the new version, after a hook's events (`events.go`). | `PolicyChanged` only (`spec_machine.py:1524-1527`). | `TestExecuteEmitsPolicyEventsD2` (every kind, the schema version 2 kinds included) |
| D-4 (schema 2) | A governed limit with an activation height a is accepted at propose and execute when `height < a <= height + MaxLead` (`OUT_OF_RANGE` at or below, `BOUNDS` above or above 2^53-1); execute stores a pending change in `x/policy` that applies at a; an immediate change of the same key deletes it. | No field; the spec parses `activationHeight` as `MALFORMED`. | `TestScheduledGovernedLimitAtPropose`, `TestScheduledGovernedLimitAtExecute`, `TestActivationHeightEffectiveD4`, `TestImmediateChangeSupersedesPending`, `TestKeeperLeadBoundaries` |
| D-5 (schema 2) | `software_upgrade`, `cancel_upgrade` and `council_seat` are accepted with the checks and codes of the payload table above (Q3 (a): MinLead and MaxLead, PROPOSED, at propose and at execute; a plan name is never reused with another release digest). | Unknown alternative, `MALFORMED` (`spec_machine.py:1103-1188`). | `TestSoftwareUpgradeAtPropose`, `TestSoftwareUpgradeAtExecute`, `TestSoftwareUpgradePayload`, `TestCancelUpgradeAtPropose`, `TestCancelUpgradeAtExecute`, `TestCancelUpgradePayload`, `TestCouncilSeatAtPropose`, `TestCouncilSeatAtExecute`, `TestCouncilSeatPayload`, `TestReplacedSeatApprovalsBecomeStale`, `TestKeeperLeadBoundaries`; `tests/unit/test_payload_vectors.py` asserts the spec's `MALFORMED` for every schema version 2 vector |
| D-7 (DR-10-13) | Genesis refuses a seat key that is the consensus key of a genesis validator or a pending entry (`DUPLICATE_ID`). An executed validator payload whose consensus key is a current or a retired (`0x15`) seat key is `ERROR_CODE_DUPLICATE_ID` and stays `QUEUED`; it is the last check of the execute, after the projection and the pending cap. A seat replacement (payload or `RotateCouncilSeat`) onto a current or pending validator key is `DUPLICATE_ID`. | Not checked. | `TestSeatKeyIsNotAValidatorKeyD7`, `TestCouncilSeatAtExecute`, `TestCouncilSeatPayload`; `FuzzStateTransitions` checks the invariant after every operation and draws seat keys (key pool 3) |
| D-9 | `Query/Governance` (this module; registered by `RegisterServices`) with the paging below and the devnet test rows in effect. | No query semantics. | `TestGovernanceQueryPaging`, `TestGovernanceQueryRejects`, `TestGovernanceQueryResponseBound`, `TestGovernanceQueryReadsPolicy`, `TestGovernanceDevnetTestParameters`, `TestRegisterServicesAndMigrations` |
| D-10 (DR-10-42) | Genesis refuses a seat id longer than `MaxSeatIDLength` (59) with `BOUNDS`, so `seat-` plus the id fits the 64-character id pattern of the `council_seat` parameter key. | The id pattern allows 64. | `TestSeatIDLengthAtGenesisD10` |

The differential oracle rules for D-1 and D-7 belong to `chain/x/authority/difftest` (a separate package); the differential never generates a schema version 2 payload (D-4, D-5). The execute-side ceiling check stays (defence in depth); after D-1 no message reaches it, so `TestExecuteRechecksCeiling` writes a `QUEUED` record directly and shows `BOUNDS` with the proposal left `QUEUED`, the store unchanged, and no writer call. The upper edge of both height windows can only fail at execute through a record written directly, because a later block only narrows it (`TestKeeperLeadBoundaries`).

## Spec differences recorded by the goal 10 differential

`chain/x/authority/difftest` runs this keeper against `spec_machine.py` step by step through `tests/difftest/spec_oracle.py` (keeper level, not end-to-end). The oracle maps each difference below by name and reports the name in the step's `deviation` field. G-1 to G-10 describe chain code that predates goal 10. The exact scopes of G-6 and G-10 are the ones implemented (orchestrator decision ORCH-2, accepted as implemented; not an owner decision). G-11 names the eight parse conditions that `TestUnnamedParseDivergences` pinned before goal 10 (orchestrator decision ORCH-1, a reversible implementation choice and not an owner decision; the open request to the owner is DR-10-44); `payload.go` codes are not changed, because they are client-visible.

| Rule | Chain | Spec | Recorded in |
| --- | --- | --- | --- |
| G-1 | A propose whose digest algorithm is not SHA-256 is `MALFORMED`, checked right after the digest value (after the spec's `INTEGRITY` check). | No algorithm field. | `docs/evidence/goal-06.md` (Deviations) |
| G-2 | A request mapping: proto3 has no absent integer, so a missing `base_policy_version` or `expiry_unix_seconds` is 0 (`STALE_VERSION`, `BOUNDS`). | An absent field is `MALFORMED`. | `docs/evidence/goal-06.md` (Deviations) |
| G-3 | The per-seat open-proposal quota is `BOUNDS`, the last propose check (DR-06-03). | No quota. | `docs/evidence/goal-06.md` (Deviations) |
| G-4 | The pending cap is `BOUNDS` after the projection; the proposal stays `QUEUED` (DR-06-04). | No cap. | `docs/evidence/goal-06.md` (Deviations) |
| G-5 | An approve that queues the proposal past `unix_seconds_max` is `OVERFLOW`. | No check. | `docs/evidence/goal-06.md` (Deviations) |
| G-6 | Only the approved shape runs: four seats, threshold three, DEC-008 as the labelled devnet test row on a `shortvideo-dev-` chain. With any other shape every council step (propose, approve, execute) is `DECISION_BLOCKED` at the council check; the oracle applies the rule per step, not only at reset. | Any shape. | `docs/architecture/authority-authorization.md` (four seats and three approvals) |
| G-7 | A label: a keep outcome commits with ABCI code 0 and carries its code in an `authority_result` event (DR-06-05). | Keep outcome. | `docs/evidence/goal-06.md` (Deviations) |
| G-8 | After a due group, the EndBlock checks the applied validator set again and halts with `DUPLICATE_ID` or `FORBIDDEN`. No message reaches it (every change was projected when scheduled), so only the oracle self-test covers it. | No re-check. | first written down by goal 10 |
| G-9 | A validator activation `unix_seconds` outside `[unix_seconds_min, unix_seconds_max]`, with every other field well formed, is `BOUNDS`. | `MALFORMED`. | first written down by goal 10 |
| G-10 | A validator payload with a well-formed consensus key and a node id outside the id pattern is `BOUNDS` whatever its reason digest, activation and power hold, absent or unparseable ones included: the chain checks the node id before those fields. | `MALFORMED` when one of those is absent or does not parse. | first written down by goal 10 |
| G-11 | Payload field shape: chain `BOUNDS` where the spec says `MALFORMED`, at propose, both sides reject and neither changes state, for exactly these eight conditions: a validator reason digest whose value is not 32 bytes; a negative validator power; an empty consensus key; a force-withdraw reason digest whose value is not 32 bytes; a force-withdraw content id outside the id pattern together with a reason digest that has no algorithm; a governed value of 0 (a proto3 default); an empty governed limit key (a proto3 default); an empty provider id in an admit payload (a proto3 default; disable uses the same parse). | `MALFORMED`. | open spec divergence (DR-10-44); `TestCouncilRulesAreLoadBearing/G-11/*` and `TestShapeRuleNearMisses` (difftest) |

A divergence outside G-1 to G-11 and D-1 to D-10 is reported, never hidden. Five such divergences are open and need a decision (widen G-11, add a rule, or change the chain); `TestUnnamedParseDivergences` (difftest) pins them, and the generator never sends them. Each is chain `BOUNDS` where the spec says `MALFORMED`, at propose, and neither side changes state: a 31-byte validator consensus key without a reason digest; a validator payload with an empty node id; a force-withdraw content id outside the id pattern without a reason digest; the same with content version 0; the same with a 31-byte SHA-256 reason digest.

## Policy writer and account hooks

`PolicyWriter` (`policy.go`) is the `x/policy` store as an executed proposal sees it, exactly the plan 7.2 interface: `SetLimit`, `SchedulePending`, `ScheduleUpgrade`, `CancelUpgrade`, `UpgradeApplied`. `Accounts` (`EnsureAccount(addr)`) creates the account of a new seat key (DR-10-35; the goal 06 follow-up "Any future message path for `RotateCouncilSeat` must create the auth account for the new seat address", `docs/evidence/goal-06.md:503`). Both take no context and return an `Outcome`.

The keeper holds the context-taking keepers: `PolicyKeeper` (the `x/policy` keeper's writer methods, which take an `sdk.Context` and return a `msgkit.Code`, plus `GovernedLimit` for the governance page cap) set with `WithPolicy`, and `AccountKeeper` (`EnsureAccount(ctx, addr) msgkit.Code`, which the app implements over the auth account keeper) set with `WithAccounts`. `Keeper.transition` binds them to the nested hook cache of one message (`boundPolicy`, `boundAccounts`, as `boundRegistry` does for the registries). `applyPayload` is the only caller of every hook method; each bound adapter method calls only the keeper method of its own name; only `Keeper.transition` binds (`TestPolicyWriterCallSites`, an AST scan of this package). `TestBoundPolicyAndAccounts` checks the forwarding, the code mapping and the nil refusals of `WithPolicy` and `WithAccounts`.

`applyPayload` checks the governed key and value and both height windows itself before it calls the writer, so those outcomes do not depend on the writer; `x/policy` checks again. The engine-level tests use the in-memory fakes of `hooks_test.go`; the keeper-level tests (`keeperEnv`) use the real `x/policy` keeper on its own IAVL store and a stand-in account keeper that writes an account row into the hook test store.

## Policy version bump for an upgrade

`BumpPolicyVersionForUpgrade` (`upgrade.go`) adds one to `0x04`. Under the owner's Q2 answer "Council plan, fail closed (Recommended)", proposals approved but not executed before an upgrade become stale at the upgrade: every proposal on the old base then fails its execute with `STALE_VERSION` or `CONFLICT` and stops counting against its seat's quota; already scheduled validator changes and pending governed changes are not touched. A damaged version is `ERROR_CODE_INTEGRITY` and version 2^64-1 is `ERROR_CODE_OVERFLOW`; neither writes (`TestBumpPolicyVersionForUpgrade`). No non-test file of the chain module outside `chain/upgrade` names the method, and `chain/upgrade` does (`TestBumpPolicyVersionCallSites`).

## Query/Governance

`Keeper.Governance` serves `svn.authority.v1.Query/Governance` through the hand-written `QueryServiceDesc` (D-9); `AppModule.RegisterServices` registers it on the gRPC query router. It reads committed state and never writes. Interim paging (DR-10-23):

- One ordered walk: validators by consensus key, submitters by address, seats by seat id, pending validator changes in store-key order, then proposal records by proposal id. Every item counts against the page limit, the first one included.
- The limit is checked by `msgkit.CheckPage` against the effective `max_page_limit`: the governed value `x/policy` holds, or the ceiling 100. 0 uses the cap; above the cap is `BOUNDS`. A keeper without a policy keeper refuses the query instead of using the ceiling (`TestGovernanceQueryReadsPolicy`).
- `page_key` is a tag byte and a cursor: `0x01` and a 32-byte consensus key, `0x02` and a 20-byte address, `0x03` and a seat id, `0x04` and one byte, the position of the next pending entry. A bare proposal id (ids start with `[a-z0-9]`, never a tag byte) continues the proposals. A key over 64 bytes, a tag with a cursor of the wrong shape, an unknown first byte, or a pending position with no entry is `BOUNDS`. A key cursor resumes at the first item at or after it.
- Pending entries use a position, not their store key: a store key plus a tag can exceed the 64-byte page-key bound for a 55- to 64-character proposal id. The pending set (at most 128 entries) is read whole, so a position is exact at one height; a walk across heights should query at the height of its first page. This is a deviation from the plan text "pending by store key".
- A proposal carries `approver_keys` only; the proto has no seat-id field. `approved_council` is always empty and `quorum_loss` is the stored value.
- Every page carries `devnet_test_parameters` (schema version 2): the rows that take effect on the chain under the `State.devnet` rule, in stored order (a `shortvideo-dev-` chain id, label `PROPOSED_TEST_PARAMETER`, `approved_policy` false), so the DEC-008 review delay in use is published (REQ-GOV-003). Off a devnet chain id the field is empty. The rows are fixed at genesis and do not count against the limit. They are devnet test stand-ins, never approved policy.
- Errors are `msgkit.QueryErr`; a record or entry that does not decode is a plain error.

`TestGovernanceQueryPaging`, `TestGovernanceQueryRejects`, `TestGovernanceDevnetTestParameters`, and `TestGovernanceQueryResponseBound` (100 maximal proposals plus a devnet row for every decision id stay within the computed bound, itself below 65,536 bytes).

## Payload vectors

`testdata/payload-vectors.json` holds the canonical SVN-JCS-1 text and SHA-256 digest of every payload kind: the v0 kinds, every governed key at 1 and at its ceiling, five governed limits the chain refuses at propose (D-1), and the schema version 2 additions (a governed limit with an activation height, at a lead inside and at the upper edge of the window; a software upgrade, inside and at the minimum lead; a cancel; a council seat change). `TestPayloadVectors` checks them against the chain code and a full propose at height 5. `tests/unit/test_payload_vectors.py` recomputes them with goal 02's `jcs.py` and `spec_machine.py` and with the generated Python protobuf module (each payload parsed strictly as a `ProposalPayload` and its proto3 JSON canonicalized), checks that the spec accepts every v0 payload at propose and refuses exactly the D-1 rows at execute, and that the spec parses every schema version 2 payload as `MALFORMED` (D-5) while the vector binds the parameter key the revision states. `TestV0PayloadVectorsUnchanged` checks that the chain rebuilds the two protocol payload vectors of `docs/protocol/fixtures/vectors.json` byte for byte.

## Simulation

In simulation (`sdk.ExecModeSimulate`) the SDK runs the handler on an infinite gas meter, so `enforceCommitted` skips only the meter-limit check there; the ante handler still checks the declared gas limit (a declared 0 is accepted only in simulation). The transaction-size check, the fixed 50,000 gas charge, and the signer check run in every mode. FinalizeBlock always runs the meter check. CheckTx runs no message handler (`baseapp` `runMsgs` stops unless the mode is finalize or simulate); the ante handler checks the declared limit there. `TestEnforceCommitted` covers each mode by calling the function directly, `TestHandlerMeterCheckByMode` drives the same split through `Approve`, and `TestSimulateEstimatesGas` in `chain/app` simulates and then commits each council message. The ante handler rejects an out-of-range declared gas limit first, so the handler check is not observable at app level.

## Wiring

```go
readKeeper := authority.NewKeeper(keys[authority.ModuleName])
policyKeeper := policy.NewKeeper(keys[policy.ModuleName], policy.Deps{Versions: readKeeper, Decisions: readKeeper})
peers := msgkit.NewPeers()
// providers, content, leases and evidence take readKeeper for Role and the
// other authority reads, and policyKeeper for GovernedLimit.
authorityKeeper := readKeeper.
	WithRegistry(providersKeeper, contentKeeper).
	WithPolicy(policyKeeper).
	WithAccounts(accounts). // the app's AccountKeeper over the auth account keeper
	WithPeers(peers)
peers.Add(authority.ModuleName, authorityKeeper)
authorityModule := authority.NewAppModule(authorityKeeper, order)
authorityModule.RegisterServices(configurator) // Query/Governance and the migration table
```

`WithRegistry`, `WithPolicy` and `WithAccounts` panic on a nil hook. A nil peers set consults no other module. The keeper stored in `App`, passed to the module, the message router and the ante, is the one these calls returned. The app wiring itself is `chain/app`'s (INT).

## Gas

Each message pays a fixed 50,000 gas, then the SDK gas store charges each read and write by its key and value length. The charge does not depend on how many other keys the store holds. A message touches only the keys in the table above, plus the peer business-id reads and the hook's fixed key set (one provider or content record; one governed value and one pending row, or the plan, one plan-name row and one applied row in `x/policy`; one account). Their value lengths depend on the message's own fields, the current config, and fixed-size counts and digests. So gas does not grow with past proposals, business ids, or the executed set. An execute of a validator or council seat payload also reads the pending prefix, at most 128 entries, so its gas is bounded but depends on how many changes are scheduled. In the app, a force-withdraw execute also voids the content's open lease offers through the x/leases withdrawal hook, at most 256 (`leases.MaxOpenOffersPerContent`), so its gas is bounded but depends on that count (`TestWithdrawalAtCapGasInApp` in `chain/app`). `TestCouncilGasDoesNotGrowWithHistory` (keeper, with the real `x/policy` store) and `TestCommittedGasDoesNotGrowWithHistory` (app) measure the same message before and after 300 terminal proposals and require equal gas. `TestMessageKeySets` pins the authority key set of every kind.

EndBlock work is bounded too. Execute refuses a 129th scheduled change and genesis validation refuses more than 128, so EndBlock reads at most 128 entries per block (`MaxPendingChanges`, PROPOSED, DR-06-04).

## Quotas

`MaxOpenProposalsPerSeat` is 32 (PROPOSED, DR-06-03). It counts only `OPEN` and `QUEUED` proposals of one seat on the current base policy version. Executed, expired, and rejected proposals never count. A proposal on an older base can never execute, so it stops counting when an execute or an applied upgrade bumps the version. A seat's own current-version proposal that is never approved, or whose execute fails with any code other than `ERROR_CODE_FORBIDDEN` (for example `ERROR_CODE_BOUNDS`, `ERROR_CODE_OUT_OF_RANGE` or `ERROR_CODE_DUPLICATE_ID`), keeps its slot until the version moves or it is marked expired; its expiry may be as late as `unix_seconds_max`. `MaxPendingChanges` is 128 (PROPOSED, DR-06-04). No message cancels a scheduled validator change, so 128 far-future entries block later validator changes until they apply. The spec machine has neither cap; both are recorded deviations. `docs/architecture/authority-authorization.md` defines DR-06-01 to DR-06-06.

## Replay retention

Proposal records (`0x10`) and business ids (`0x11`) are kept for the life of the chain, as in the spec. The executed set (`0x12`) is kept because the stale branch reads it. Nothing is pruned: pruning would weaken replay protection. Storage grows by one bounded record per accepted message. Gas does not.

## Export

`ExportState` returns the config key plus the pending entries. The layout key (`0x00`) must hold `ConsensusVersion`; it is not exported, and InitStore writes it again on import. Export refuses, with `ErrExportRefused` naming the prefix, while any other prefix holds a key, the retired `0x13` and the reserved `0x17` prefixes included: the genesis proto has no field for that state (DR-06-01). The module `ExportGenesis` panics with that error because the SDK interface has no error result; `App.Export` checks first and returns it. `App.Export` also refuses a non-empty provider, content, or lease catalogue (the goal 05 launch rule); provider, content, and lease business ids are DR-07-03. `x/policy` refuses its own governed, pending, plan and applied-upgrade rows the same way.

## Layout version 2 and no migration

No live chain exists, so no migration is registered (DR-10-04, interim; homes from goals 05 to 09 are re-initialized, as goals 07 to 09 did). This binary's authority layout is version 2: InitGenesis writes the layout key `0x00` = 2, `ConsensusVersion` is 2, the governed-value prefix `0x13` is retired, and every proposal record carries the schema version 2 fields. A store written by an earlier binary is refused before any record is read: it has no authority layout key, and the app's load check compares every module's layout key with its persisted version map (that check is the app's). If a layout version 1 record were read anyway it would not decode (`decodeStrict` rejects the missing fields), so an approve or execute that reads it is `ERROR_CODE_INTEGRITY` and writes nothing (`TestOldLayoutRecordIsIntegrity`), and export refuses a `0x13` key. The migration table (`migrations` in `module.go`) is empty and covers exactly 2 .. `ConsensusVersion`-1; `RegisterServices` registers it on the configurator, so a later layout step can be added there (`TestRegisterServicesAndMigrations`).

Goal 07 added two fields (`content_id`, `content_version`) to every proposal record; goal-05/06 homes were already refused by the registry layout check. The round-2 layout kept everything but the config in one side document at `0x20`. A home that still holds `0x20` halts in EndBlock with an error that says to re-initialize it, and export refuses it. A config key that still holds proposals, pending entries or an approved council (the round-2 config) is refused by `LoadState` (`ERROR_CODE_INTEGRITY` for every council message, the store unchanged), never applied by EndBlock, and refused by export (`TestOldLayoutConfigIsIntegrity`).

## Tests

- `engine_test.go`: council flow, delay, expiry keep, forbidden removal keep, halt on a mismatched removal, council and service rotation, `TestAdmitAndDisableRows` (admit, disable and force-withdraw through the deterministic spec registry; every reject leaves the store and the registry unchanged).
- `registry_test.go`: DEC-002 and DEC-003 flags, decoded force-withdraw arguments, `FORBIDDEN` keeps `REJECTED`, other registry codes leave the proposal `QUEUED`, nil registry, expiry before the registry call, external business ids (`REPLAY`, idempotent, `INTEGRITY` on a malformed peer value) on propose, approve and execute, the devnet role rule. Each failure compares the whole store (`dumpKV`) before and after.
- `keeper_registry_test.go` (real IAVL multistore with a second store for the test hooks): hook writes committed only on success, dropped on keep, on failure and on a late `OVERFLOW` (`TestKeeperRegistryWriteDroppedOnLateFailure`); force-withdraw arguments from the record; one business-id namespace through `msgkit.Peers`, with `INTEGRITY` for a peer value that is not 32 bytes (`TestKeeperMalformedPeerBusinessDigestIsIntegrity`); the read surface and its fail-closed paths (`BlockedDecisions` included; governed values read from `x/policy`); deterministic sources.
- `read_test.go` (real IAVL multistore): `TestDevnetNumeric` and `TestPolicyVersionRead`.
- `genesis_test.go`: one row per `ValidateGenesisState` rule (the goal-10 D-7 and D-10 rules are in `separation_test.go`), the seat-address rule included; `TestSeatAddressMatchesGenesis`.
- `gates_test.go`: council-ready, stale and conflicting executes, activation groups, stale-proposal counts, activation height bounds and policy-version overflow; `TestAdmissionOf`; `TestSeatRotationMovesSeatAdmission`; `TestEngineOnlyRotationRejects`.
- `enforce_test.go`: `enforceCommitted` per execution mode (`TestEnforceCommitted`) and through `Approve` (`TestHandlerMeterCheckByMode`).
- `gas_test.go`: `TestMessageKeySets` pins the key sets of every kind, the schema version 2 kinds included; `TestCouncilGasDoesNotGrowWithHistory`.
- `fuzz_state_test.go` `FuzzStateTransitions` and `fuzz_registry_test.go` `FuzzCouncilRegistryPayloads`: bounded operation sequences on a cache branch of the per-object store, a clone of the spec registry and a clone of the fake policy store, as `Keeper.transition` runs them. A failure must leave the branch byte-identical (`dumpKV`) and the registry and policy state unchanged; a keep never writes them. The state fuzz also checks that no consensus key is a current or retired seat key (D-7), and key pool 3 draws seat keys.
- `typed_propose_test.go`: D-1 per key and value, its place in the check order, the keeper path; safety caps; the execute-side recheck; `TestBoundPolicyAndAccounts`; `TestPolicyWriterCallSites`.
- `schema2_test.go` (engine level, fake hooks): every propose and execute check of the schema version 2 payloads with its exact code and boundary (h, h+1, h+min-1, h+min, h+max, h+max+1, above 2^53-1), the store unchanged on each refusal; every council seat refusal row in protocol order; `TestHeightWindowSafeIntegerCap`; `TestRotateCouncilSeatProtocolOrder`; `TestHooklessExecuteIsMalformed`; `TestFailedSeatExecuteKeepsTheStateConfig`.
- `policy_keeper_test.go` (keeper level, real `x/policy` store): `TestActivationHeightEffectiveD4`, `TestImmediateChangeSupersedesPending`, `TestSoftwareUpgradePayload`, `TestCancelUpgradePayload`, `TestCouncilSeatPayload`, `TestReplacedSeatApprovalsBecomeStale`, `TestPolicyWriterFailureWritesNothing`, `TestGovernanceQueryReadsPolicy`, `TestGovernanceDevnetTestParameters`, `TestKeeperLeadBoundaries`, `TestHookWriteDroppedOnRejectedKeep` (a hook that writes and then returns `FORBIDDEN` leaves only the `REJECTED` mark).
- `layout_test.go`: `TestLayoutKeyAndConsensusVersion`, `TestOldLayoutRecordIsIntegrity`, `TestRegisterServicesAndMigrations`.
- `events_test.go`: D-2 for every kind, and no typed event on propose, approve, retry, keep or refusal (a registry and a policy refusal included).
- `separation_test.go`: D-7 in both directions; D-10.
- `upgrade_test.go`: `BumpPolicyVersionForUpgrade` and its call-site scan over the chain module.
- `query_test.go`: `Query/Governance` paging, refusals, and the response size bound.
- `vectors_test.go`: the payload vectors and the two protocol payload vectors.
- `council_acceptance_test.go` (keeper level on a real IAVL store, finalize mode): `TestKeeperDuplicateVotes`, `TestKeeperInsufficientApproval`, `TestKeeperInvalidBounds`, `TestKeeperConcurrentProposalsOnOneKey`, `TestKeeperExpiryAndReviewDelayBoundaries`, `TestKeeperUnauthorizedWritesFailAtExecution`, `TestNoPayloadApprovesDecision` (every kind). Each refusal compares the authority store, the hook store and the `x/policy` store before and after and requires no typed event. The app-level tests through FinalizeBlock are separate.
