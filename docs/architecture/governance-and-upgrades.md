# Governance and upgrades

Status: PROPOSED. This is the goal-10 specification for policy governance and chain upgrades. It is not approved. It is
not an audit. It applies to the devnet beta only. Public launch is disabled. There is no token, fee denomination, reward,
staking module, or penalty.

Approval. The goal-10 prerequisite "migration and governance specifications approved"
(`docs/goals/10-policy-governance-and-chain-upgrades.md:8`) is recorded BLOCKED as DR-10-00. The owner will be asked to
approve or reject this file at a pinned commit and file sha256 when the goal-10 pull request is ready (section 21.4).
The owner's answers to Q1, Q2, and Q3 (section 21) are quoted where they apply; no other sentence in this file is an owner
decision. `docs/decision-register.md` holds no DR-10 entry; it is a goal-01 document and goal 10 does not edit it.

How to read this file.

- Line citations (`path:line`) refer to the goal-10 tree: the files as they stand in the revision that
  `docs/evidence/goal-10.md` names as tested. A citation written `ff951fc:path:line` refers to commit `ff951fc`, the
  goal-09 head that goal 10 started from, and describes code goal 10 replaced.
- Framework citations name module-cache copies of the pinned versions (`chain/go.mod:12,15,16`):
  `cometbft@v0.40.0/...` is `$(go env GOMODCACHE)/github.com/cometbft/cometbft@v0.40.0/...`, `cosmos-sdk@v0.55.0/...` and
  `store/v2@v2.0.0/...` likewise.
- "spec :N" means `docs/evidence/goal-02/spec_machine.py` line N, the executable reference semantics.
- Every number is either cited or labelled PROPOSED. A PROPOSED number is a proposal with a decision request, not policy.
- Labels. "Owner ruling" quotes the option label the owner selected. "ORCH-n" is an orchestrator choice: a reversible
  implementation choice made while goal 10 was built, not an owner decision. "Interim" marks a rule that answers an open
  decision request; it is implemented, tested, and reported open. "Fail-closed" on an interim rule means it refuses the
  uncertain action instead of allowing it.
- `[Educated Guess]` marks an inference that no test confirms.
- The operator procedures are in `docs/operations/` (section 16); this file states the rules they follow.

## Contents

1. Scope and what this file does not claim
2. Starting point and what goal 10 changed
3. Council flow, exact order
4. Typed governed changes
5. Payload additions (owner ruling Q1)
6. Policy versions
7. Unique approvals, threshold, expiry, review delay
8. Council seat replacement
9. Safety bounds and what does and does not protect them
10. Storage, queries, events, genesis, export
11. Upgrade version model
12. Upgrade plans: who schedules, when, and notice
13. Load-time gate
14. Per-block rule and the upgrade runner
15. Failure behaviour without resetting history
16. Operator actions: allowed, forbidden, and when rollback is unsafe
17. Separation of local emergency controls, protocol changes, and publication authority
18. Rehearsal heights and binaries
19. Deviations from `spec_machine.py`
20. Invariants
21. Owner rulings and the approval of this file
22. Orchestrator choices and interim rules
23. Residual risks
24. Steps that need a human approval or an independent security review

## 1. Scope and what this file does not claim

Goal 10 asks for typed parameter changes, admission limits, policy versions, scheduled activation, a chain-upgrade
mechanism built from supported framework components, a rehearsal harness, runbooks, and a separation of local emergency
controls, protocol changes, and publication authority (`docs/goals/10-policy-governance-and-chain-upgrades.md:18-31`).

This file specifies the design. It does not report what is verified; `docs/evidence/goal-10.md` does that once the tests
run, with logs. It does not claim:

- live council parameters: seat holders, the live review delay, a maximum expiry, quorum-loss behaviour (DEC-008,
  `docs/decision-register.md:247,263`);
- an upgrade of any chain other than a disposable devnet;
- that any release is reviewed, signed, published, or audited;
- independent operators: every run in this repository is one operator on one machine (`docs/progress.md:104-110`);
- indexer schema compatibility (the indexer is goal 12);
- restart from a lossless export of state that holds governance records (DR-06-01; section 22).

## 2. Starting point and what goal 10 changed

| At `ff951fc` | Since goal 10 |
| --- | --- |
| Five payload kinds (`ff951fc:docs/protocol/proto/svn/authority/v1/tx.proto:64-72`) | Eight: `software_upgrade` (6), `cancel_upgrade` (7), `council_seat` (8) added under owner ruling Q1 (`docs/protocol/proto/svn/authority/v1/tx.proto:95-106`; `docs/protocol.md:340-379`) |
| A governed limit was checked at propose only for the id pattern and a value of at least 1 (`ff951fc:chain/x/authority/payload.go:235-249`) | Key and ceiling checked at propose (D-1, `chain/x/authority/engine.go:393-395`) and again at execute (`chain/x/authority/engine.go:630-632`) |
| Ceilings existed twice, `governedCeiling` (`ff951fc:chain/x/authority/engine.go:608-623`) and `msgkit` | One table, `chain/msgkit/limits.go:20-26` |
| Governed values in authority `0x13` | x/policy `0x02`; authority `0x13` retired and refused by export (`chain/x/authority/store.go:63-67`) |
| `PolicyChanged` and `ProposalExecuted` never emitted (`docs/evidence/goal-06.md:187`) | Emitted after a successful execute (`chain/x/authority/events.go:26-40`; D-2) |
| x/policy a scaffold slot (`ff951fc:chain/x/policy/README.md:3`; `ff951fc:chain/app/app.go:244`), with `checkPolicy` checking only the safety caps (`ff951fc:chain/app/app.go:570-592`) | x/policy keeper wired; InitChainer and `ValidateAuthorityGenesis` check the policy genesis (`chain/app/app.go:489-539`) |
| No upgrade machinery: PreBlockers auth only (`ff951fc:chain/app/app.go:269`); `checkRegistryLayouts` compared four registry layout keys (`ff951fc:chain/app/app.go:328-354`) | The load gate `checkLayoutsAndVersions` (`chain/app/app.go:437-455`) and the upgrade runner's PreBlock step (`chain/app/app.go:548-559`) |
| Consensus `Version.App` set from `genesis.ProtocolVersion` (`ff951fc:chain/devnet/init.go:564`) | From `app.GenesisAppVersion` = 1 (`chain/app/app.go:128`; `chain/devnet/init.go:576-581`) |
| Seat key rotation engine-only (`ff951fc:chain/x/authority/engine.go:840`) | A council `council_seat` payload; both paths share `replaceSeat` (`chain/x/authority/engine.go:1034-1077`) |
| Authority store without a layout key, `ConsensusVersion` 1 (`ff951fc:chain/x/authority/module.go:116`) | Layout key `0x00` = 2 and `ConsensusVersion` 2 (`chain/x/authority/store.go:41-43`; `chain/x/authority/module.go:30`) |

Facts goal 10 kept:

- The council shape is compile-time: `ApprovedSeats = 4`, `ApprovedThreshold = 3` (`chain/x/authority/codes.go:40-41`),
  from DEC-008 "Four named seats, three approvals for an ordinary change, one approval per seat, an expiry, and a published
  delay" (`docs/decision-register.md:247`). `council()` requires the labelled DEC-008 devnet row with numeric value 3, a
  delay inside the unix bound, and exactly four seats, else `ERROR_CODE_DECISION_BLOCKED` (`chain/x/authority/engine.go:216-226`).
- Off a `shortvideo-dev-` chain id the devnet row is never found (`chain/x/authority/engine.go:180-190`), so every council
  message is `ERROR_CODE_DECISION_BLOCKED` there, and no upgrade can be scheduled on any other chain.
- A seat-signed transaction may carry only `MsgProposeChange` or `MsgApproveChange` (`chain/app/ante.go:171-180`), the
  owner's DR-06-06 ruling (`docs/architecture/authority-authorization.md:94`). Execute requires the
  `SUBMITTER_ROLE_AUTHORITY` role (`chain/x/authority/engine.go:526-531`).
- Payloads with hooks run in a nested cache that only a success writes back (`chain/x/authority/msg.go:102-145`).
- Protocol v0: "Unknown fields are rejected in v0. A new field is an incompatible change. It needs a recorded decision and
  a new schema version." (`docs/protocol.md:26`). Owner ruling Q1 is that recorded decision for the goal-10 fields.

## 3. Council flow, exact order

Before every handler the spec runs `guard_decisions` (spec :112): any approved decision row, a non-empty approved council,
or an economic flag makes every transaction `ERROR_CODE_FORBIDDEN`. A failed step keeps no state. A keep outcome
(`EXPIRED` or `REJECTED` mark) commits only the mark (`chain/x/authority/msg.go:102-145`). Goal 10 keeps this order for
every payload kind, old and new. Step 7a is the only new step (D-1, D-4, D-5; section 19). An unknown field never reaches a
handler: the strict transaction decoder refuses it (`chain/app/encoding.go:120-138`); the spec answers `UNKNOWN_FIELD`.

### 3.1 Propose (spec :1403-1449; chain `chain/x/authority/engine.go:350-462`)

| # | Check | Code | Chain |
| --- | --- | --- | --- |
| 1 | unknown field | spec `UNKNOWN_FIELD`; chain: the SDK decode error | the decoder, before the handler (above) |
| 2 | missing field | `MALFORMED` | `:353-356` |
| 3 | council ready (DEC-008 devnet row, four seats) | `DECISION_BLOCKED` | `:357-359` |
| 4 | signer is not a seat | `UNAUTHORIZED` | `:360-363` |
| 5 | proposal id, then parameter key, off the id pattern | `BOUNDS` | `:364-366` |
| 6 | parameter key starts `dec-` | `FORBIDDEN` | `:367-369` |
| 7 | payload: parse, canonical form, digest, digest algorithm, parameter-key binding, validator activation strictly future | `MALFORMED`, `BOUNDS`, `INTEGRITY`, `OUT_OF_RANGE` | `:370-384` |
| 7a | (goal 10) governed limit key not in `governed_limit_caps` or value above its ceiling (D-1); then the height window of a scheduled governed limit or an upgrade plan (D-4, D-5) | `BOUNDS`; `OUT_OF_RANGE` below the window, `BOUNDS` above | `:393-395`, `:403-405` |
| 8 | expiry outside the unix bound | `BOUNDS` | `:406-408` |
| 9 | business id: same digest is idempotent, other digest is a replay | idempotent, `REPLAY` | `:409-418` |
| 10 | proposal id already stored | `DUPLICATE_ID` | `:419-421` |
| 11 | expiry <= chain time | `EXPIRED` | `:422-424` |
| 12 | base policy version != current | `STALE_VERSION` | `:425-431` |
| 13 | (Go only, DR-06-03) the seat holds 32 open proposals on the current version | `BOUNDS` | `:432-441` |
| 14 | write the record `OPEN` | success | `:442-461` |

The 32-proposal quota is PROPOSED (DR-06-03, `chain/x/authority/codes.go:62`).

### 3.2 Approve (spec :1452-1482; chain `chain/x/authority/engine.go:467-518`)

Missing field `MALFORMED` (`:468-470`); council ready (`:471-473`); signer not a seat `UNAUTHORIZED` (`:474-477`); business
id gate (`:478-484`); record absent `NOT_FOUND` (`:485-488`); state not `OPEN` `CONFLICT` (`:489-491`), so a `QUEUED`
proposal past expiry is `CONFLICT` and is not marked; chain time >= expiry `EXPIRED`, kept (`:492-494`); seat already
approved `REPLAY` (`:495-497`); (Go only) `OVERFLOW` when `now + delay` would leave the unix bound (`:498-502`); append the
seat id and key (`:503-506`); at three approvals the state becomes `QUEUED` with `execute_after = now + DEC-008 delay`
(`:507-510`).

### 3.3 Execute (spec :1485-1527; chain `chain/x/authority/engine.go:522-591`)

| # | Check | Code | Chain |
| --- | --- | --- | --- |
| 1 | missing field | `MALFORMED` | `:523-525` |
| 2 | signer role is not `AUTHORITY` (no role `UNAUTHORIZED`, other role `FORBIDDEN`), before council ready | `UNAUTHORIZED`, `FORBIDDEN` | `:526-531` |
| 3 | council ready | `DECISION_BLOCKED` | `:532-534` |
| 4 | business id gate | idempotent, `REPLAY` | `:535-538` |
| 5 | record absent | `NOT_FOUND` | `:539-542` |
| 6 | chain time >= expiry, in any state | `EXPIRED`, kept | `:543-546` |
| 7 | state not `QUEUED` (insufficient approval) | `CONFLICT` | `:547-549` |
| 8 | chain time < `execute_after` (review delay) | `OUT_OF_RANGE` | `:550-552` |
| 9 | base != current version: `CONFLICT` if the key last executed another digest, else `STALE_VERSION` | `CONFLICT`, `STALE_VERSION` | `:553-563` |
| 10 | apply the payload (`applyPayload`, `:618-693`): `FORBIDDEN` marks `REJECTED` and is kept; every other code fails and the proposal stays `QUEUED` | per kind | `:564-571` |
| 11 | (Go only) version at the uint64 maximum | `OVERFLOW` | `:572-574` |
| 12 | write: executed digest for the key, policy version + 1, `EXECUTED`, business id | success | `:575-590` |

After step 12 the keeper emits the typed events `PolicyChanged` (the spec event, spec :1524-1527) and `ProposalExecuted`,
on the success branch only (D-2; `chain/x/authority/events.go:26-40`; `TestExecuteEmitsPolicyEventsD2`).

An `EXECUTED` or `REJECTED` proposal executed again after its expiry is relabelled `EXPIRED` with the policy version
unchanged (spec :1502-1504). Goal 10 keeps this spec behaviour (`docs/evidence/goal-06.md:497`).

## 4. Typed governed changes

- `GovernedLimitChange` is typed: a limit key, a uint64 value, and (owner ruling Q1) an activation height
  (`docs/protocol/proto/svn/authority/v1/tx.proto:50-57`).
- The key-set and range check runs at propose step 7a (D-1), so a proposal that can never execute does not hold a quota
  slot or a seat's time. The execute keeps its own check (`chain/x/authority/engine.go:630-632`).
- The ceilings come from one table (`chain/msgkit/limits.go:20-26`); `governedInRange` reads it
  (`chain/x/authority/policy.go:147-150`).
- A limit key that names a safety cap (`max_tx_bytes`, `max_gas_per_tx`) or a compile-time quota is not in
  `governed_limit_caps`, so it is `ERROR_CODE_BOUNDS` at propose and at execute (`TestSafetyCapsNotGovernable`).
- Values inside 1..ceiling stay accepted, including unsafe ones such as `max_expiry_steps = 1`; the contract allows
  1..ceiling (spec :1338-1343). Floors are open (DR-10-09, section 22).

Admission limits (`docs/goals/10-policy-governance-and-chain-upgrades.md:18`). No governed key limits admissions, and a new
key would be a `bounds.json` change. Interim reading [Educated Guess]: admission is council-only (DEC-009,
`docs/decision-register.md:281`) through the admit and disable payloads, bounded by the existing `max_open_offers` limit
and the compile-time quotas. A numeric admission-limit key is a recorded protocol addition (DR-10-40, section 22).

## 5. Payload additions (owner ruling Q1)

On 2026-11-07 the owner answered Q1 (DR-10-01) with "Minimal additive (Recommended)". `docs/protocol.md` section "Goal 10
additive revision" (`docs/protocol.md:340-379`) is that revision: the field list, the canonical JSON of each new kind, the
parameter keys, and the transition rows. The auditor admission option was not chosen, so there is no auditor payload and
DR-09-08 stays DEFERRED.

The revision is additive. Every new field is optional, so every v0 payload keeps its canonical form and digest (absent
fields are omitted from SVN-JCS-1; `TestV0PayloadVectorsUnchanged`). The protocol document schema version is 2
(`docs/protocol.md:15`; `docs/protocol/bounds.json:3`). The protocol name, the peer media type, the genesis
`protocol_version` (1), and the message-level `schema_version` of evidence batches, capability manifests, and decision rows
stay 1. Goal 02's checker pins changed only where the ruling required it (ORCH-4).

```proto
// svn.authority.v1 (tx.proto)
message GovernedLimitChange { string limit_key = 1; uint64 value = 2; uint64 activation_height = 3; } // 3: 0 = at execute
message SoftwareUpgradeChange { string name = 1; uint64 height = 2; svn.types.v1.Digest release_digest = 3; }
message CancelUpgradeChange { string name = 1; }
message CouncilSeatChange { string seat_id = 1; bytes new_public_key = 2; }
// ProposalPayload oneof: software_upgrade = 6; cancel_upgrade = 7; council_seat = 8.

// svn.policy.v1 (state.proto)
message GovernedLimitValue { string limit_key = 1; uint64 value = 2; }
message PendingLimitChange { string limit_key = 1; uint64 value = 2; uint64 activation_height = 3; string proposal_id = 4;
                             uint64 policy_version = 5; }   // 5: the policy version the execute produced
message UpgradePlan { string name = 1; uint64 height = 2; svn.types.v1.Digest release_digest = 3; string proposal_id = 4; }
// PolicyState adds: governed_limits = 4; pending_limit_changes = 5; upgrade_plan = 6; app_version = 7.
// QueryGovernanceResponse adds devnet_test_parameters = 3, so the review delay is published.
```

| Kind | Parameter key | Propose checks after step 7 | Execute checks (a non-`FORBIDDEN` failure leaves `QUEUED`) | Effect |
| --- | --- | --- | --- | --- |
| governed limit | `<limit_key>` | key in the five caps, value 1..ceiling (`BOUNDS`); activation 0, or `height < activation <= height + MaxActivationLeadBlocks` (`OUT_OF_RANGE` at or below the height; `BOUNDS` above the lead or above 2^53-1) | the same key, ceiling, and window checks against the execute block | activation 0: value written at once and any pending row of the key deleted (`SetLimit`); activation a: a pending row written, replacing any pending row of the key (`SchedulePending`). The latest executed decision for a key wins (I3) |
| validator power (v0) | `validator-<node_id>` | unchanged | v0 projection checks, plus a consensus key equal to a current or retired seat key is `DUPLICATE_ID` (D-7) | unchanged |
| software upgrade | `upgrade` | name on the id pattern (`BOUNDS`); height in `[height + MinActivationLeadBlocks, height + MaxActivationLeadBlocks]` and <= 2^53-1 (`OUT_OF_RANGE` below, `BOUNDS` above); digest algorithm SHA-256 (`MALFORMED`), 32 bytes (`BOUNDS`) | height still in that window for the execute block; name already applied `DUPLICATE_ID`; a name an earlier plan used with another digest `CONFLICT` (pending, cancelled, or replaced; ORCH-10) | plan written, replacing any pending plan (`ScheduleUpgrade`) |
| cancel upgrade | `upgrade` | name on the id pattern (`BOUNDS`) | no pending plan with that name `NOT_FOUND` | plan deleted (`CancelUpgrade`) |
| council seat | `seat-<seat_id>` | seat id on the id pattern, key 32 bytes (`BOUNDS`) | seat absent `NOT_FOUND`; key equals another seat's key `DUPLICATE_ID`; key retired or equal to the seat's own key `CONFLICT`; key's address holds a submitter role `DUPLICATE_ID`; key equals a current or pending validator consensus key `DUPLICATE_ID` (D-7) | section 8 |

Numbers in the table: `MinActivationLeadBlocks` = 100 blocks and `MaxActivationLeadBlocks` = 1,000,000 blocks, both
PROPOSED (`chain/msgkit/lead.go:16-30`); 2^53-1 is `SafeIntegerMax` (`chain/genesis/bounds.go:27`); the five caps are the
`msgkit` ceilings. The window checks are `heightLead` and `leadCode` (`chain/x/authority/policy.go:160-184`); x/policy's
writers check the same windows again (`chain/x/policy/writer.go:47-74`).

The new parameter keys cannot collide with v0 keys: provider keys start `admit-` or `disable-`, validator keys
`validator-`, withdraw keys `withdraw-`, and limit keys are the cap names (`chain/x/authority/payload.go:337-361`). The spec
machine parses every new payload alternative, and a governed limit that carries `activationHeight`, as `MALFORMED`; that is
deviation D-5 (D-4 for the activation height).

### 5.1 Scheduled activation of a governed limit

- Execute in block h with `activation_height = a > h` stores a pending row in x/policy: activation height, value, proposal
  id, and the policy version the execute produced (`chain/x/policy/writer.go:120-147`). It replaces any pending row of that
  key.
- Execute with activation 0 writes the value and deletes any pending row of that key in the same write
  (`chain/x/policy/writer.go:85-100`). So an older scheduled decision cannot overwrite a newer immediate one at its
  activation height (`TestImmediateChangeSupersedesPending`).
- x/policy `ApplyDue` runs in BeginBlock at height b and applies every pending row with `a <= b` in key order, deleting it
  (`chain/x/policy/apply.go:23-39`). x/policy runs before x/leases in the module order (`chain/app/app.go:53-64,320`), so a
  value activated at a governs that block's lease expiry and every transaction of block a (D-8). Block a-1 sees the old
  value; block a the new one (`TestActivationTiming`, `TestActivationGovernsSameBlockExpiryD8`).
- A damaged pending row, an unknown key, a value above the ceiling, or a sixth row makes `ApplyDue` return an error before
  any write, so FinalizeBlock fails on every node (fail closed).
- Height only. A time activation would need a total order of heights and times, which DEC-012 did not select
  (`docs/decision-register.md:365`).
- The policy version moves at execute, not at activation. During a pending window the policy version names the executed
  decisions, not the limits in force. A lease created at a-1 records v+1 while v's limit still governs it
  (`TestLeaseAtActivationMinusOne`). The limits in force are the governed values plus the pending rows with their heights;
  `Query/Policy` reports both.
- At most five pending rows exist, one per governed key.
- Using `MaxActivationLeadBlocks` for limit activations is an interim choice (DR-10-15), not an owner decision.

## 6. Policy versions

- One counter, authority `0x04` (`chain/x/authority/store.go:56-57`). It moves by 1 per executed proposal of any kind
  (`chain/x/authority/engine.go:583`). The base check serializes execution: the first execute wins and every other in-flight
  proposal becomes `STALE_VERSION` or `CONFLICT` (`chain/x/authority/engine.go:557-563`) and stops holding a quota slot
  (`chain/x/authority/codes.go:44-61`). The counter stays in authority (DR-10-20, reversible): it is the council's
  concurrency counter and x/leases reads it there.
- An applied upgrade also adds 1, through `BumpPolicyVersionForUpgrade`, which only the upgrade runner calls
  (`chain/x/authority/upgrade.go:9-20`; `TestBumpPolicyVersionCallSites`). Every proposal approved but not executed before
  the upgrade is then on an older base and can never execute under new code. Changes already executed (pending validator
  changes, pending governed changes) still apply at their heights. This is the owner's Q2 ruling: "Approved but unexecuted
  proposals become stale at the upgrade." (section 21.2).
- Exposed as `PolicyState.policy_version` by `svn.policy.v1.Query/Policy` and in the typed events `PolicyChanged` and
  `ProposalExecuted`. The genesis copy in the policy slot must be 1 (`chain/x/policy/genesis.go:113-141`), and InitChainer
  requires policy version 1 after genesis (`chain/app/app.go:532-534`).
- Because of the `EXPIRED` relabel (section 3.3) and the upgrade bump, a check that reads only state can assert
  `version >= 1 + number of EXECUTED records`. Exact equality (`1 + successful executes + applied upgrades`) holds only in
  harnesses that count operations (invariant I1).

## 7. Unique approvals, threshold, expiry, review delay

The rules of sections 3.2 and 3.3 apply to every payload kind, old and new. Owner ruling Q3, "3 of 4 + min notice
(Recommended)", set no per-kind threshold.

- Unique approvals: one per seat id (`chain/x/authority/engine.go:495-497`). The same seat again is `ERROR_CODE_REPLAY`,
  also after its key is replaced (`TestDuplicateVotes`).
- Threshold: three of four (`chain/x/authority/codes.go:40-41`; DEC-008). Fewer than three leaves the proposal `OPEN`;
  execute is then `ERROR_CODE_CONFLICT` (`chain/x/authority/engine.go:547-549`; `TestInsufficientApproval`).
- Expiry: chosen by the proposer, at most `unix_seconds_max` (253402300799, `chain/genesis/bounds.go:23`). A maximum
  expiry is a DEC-008 number and stays unset.
- Review delay: the DEC-008 devnet row (`chain/devnet/init.go:552`), 0 by default. `devnet.Options.CouncilDelaySeconds`
  sets a nonzero labelled delay (DR-10-29, reversible; `chain/devnet/init.go:54-60`), so a timing test can show
  `execute_after - 1` as `ERROR_CODE_OUT_OF_RANGE` and `execute_after` as success. The value used is a
  `PROPOSED_TEST_PARAMETER`, not policy. `svnd devnet init` does not expose the option.
- Removed seats: a seat replacement bumps the policy version when it executes, so every proposal the old key approved is on
  an older base and can never execute (`TestRemovedCouncilSeat`, `TestReplacedSeatApprovalsBecomeStale`). A recount of
  approvals by current seat key at execute is not added: approver ids and approver keys are sorted independently
  (`chain/x/authority/engine.go:503-506`), so the record does not pair a key with its seat, and the version bump already
  covers every message path.

## 8. Council seat replacement

Owner ruling Q1 reads "removed council seats" (`docs/goals/10-policy-governance-and-chain-upgrades.md:34`) as replacement of
a seat's key, "with the council staying at 4 seats per DEC-008, the old key retired for good and the new key given an
account". True removal with a recomputed threshold is not offered.

- The payload needs three of four approvals and the review delay (owner ruling Q3).
- Its execute replaces the seat key and retires the old key at `0x15` (`chain/x/authority/store.go:74-76`), through
  `replaceSeat`, which the engine-only `RotateCouncilSeat` also uses (`chain/x/authority/engine.go:1004-1077`). A retired key
  can never return as a seat key.
- The ante stops admitting the old key's account, because seat admission reads the config
  (`chain/x/authority/keeper.go:87`).
- The new address gets an auth account in the same nested cache, so its first propose passes the ante (DR-10-35; the goal-06
  follow-up "Any future message path for `RotateCouncilSeat` must create the auth account for the new seat address.",
  `docs/evidence/goal-06.md:503`). `seatAccounts.EnsureAccount` does it (`chain/app/upgrades.go:134-152`;
  `TestCouncilSeatCreatesTheNewKeyAccount`).
- The policy version bump makes every in-flight proposal unexecutable, including those the old key approved.
- Key separation in both directions (D-7, DR-10-13): a seat key may not equal a current or pending validator consensus key,
  and a validator payload may not add a consensus key equal to a current or retired seat key.
- Seat ids are capped at 59 characters at genesis (PROPOSED, D-10, DR-10-42; `chain/genesis/document.go:325`): the id
  pattern allows 64 characters (`chain/x/authority/payload.go:48`) and the parameter key `seat-<seat_id>` adds five.
- Only disposable devnet keys are used in tests. No live key is rotated.

## 9. Safety bounds and what does and does not protect them

Two different protections apply. They must not be confused.

1. No council payload can change the bounds in the table below. Each payload kind is checked against them, and tests show
   it (`TestSafetyCapsNotGovernable`, `TestNoPayloadApprovesDecision`).
2. An upgrade binary can change any rule, cap, quota, seat, or decision row. The upgrade runner and its self-checks are
   compiled into each binary, so a council-scheduled binary can drop them. The self-checks (section 14.4, invariant I13)
   only catch an honest binary's mistakes. The control against a wrong or malicious upgrade binary is human review of its
   source and digest before any operator installs it, including a diff of `chain/upgrade`, the handler, and the self-check
   list against the previous release, and each operator's own choice to install it (section 24 items 8, 9, 11). Neither
   control is a test. On the devnet one operator holds every seat and every node.

| Bound | Where enforced | Governed by a payload? | Goal-10 protection |
| --- | --- | --- | --- |
| transaction bytes 65,536; gas per transaction 1..20,000,000 | genesis bounds and the ante (`chain/app/ante.go:90-108`) | no; safety caps (`docs/protocol/proto/svn/policy/v1/state.proto:14-24`) | no payload field names them; the limit keys `max_tx_bytes` and `max_gas_per_tx` are `BOUNDS` at propose and execute; the runner's self-check refuses an upgrade that changes the safety caps or any consensus param other than `Version.App` |
| five governed limits | readers through `Limits` interfaces, now the x/policy keeper | yes, 1..ceiling | D-1 at propose; every key tested at 1 and at its ceiling with the chain live (`TestEveryGovernedKeyAtFloorAndCeiling`); a damaged stored value makes FinalizeBlock panic on every node (`TestDamagedExpiryLimitHalts`, `chain/app/lease_expiry_test.go:320`, on the uncommitted working state; a restart on a committed damaged value is not tested) |
| quotas: 32 open proposals per seat, 128 pending validator changes (`chain/x/authority/codes.go:62,69`, both PROPOSED, DR-06-03, DR-06-04); lease and evidence quotas | compile-time | no | a key that names a quota is `BOUNDS`; changing a quota is a binary upgrade |
| lease bounds: DEC-005 term | devnet row at genesis only | no | no payload writes the config's devnet rows |
| resulting validator policy (DEC-011: at least four validators, `max_power * 3 < total_power`) | projection at every activation point (`chain/x/authority/engine.go:804-916`) | council validator payloads only | unchanged; the runner's self-check refuses an upgrade that changes any authority row |
| blocked decisions, approved council, quorum loss | genesis checks | no | no payload, old or new, changes a decision row; runner self-check |
| seats, submitters, admission rows, retired keys | authority `0x01`, `0x02`, `0x15`, `0x16` (`chain/x/authority/store.go:41-84`) | seat payload only | runner self-check compares every authority row |

Changing a safety cap "is an incompatible protocol change and needs a recorded decision"
(`docs/protocol/proto/svn/policy/v1/state.proto:16`).

## 10. Storage, queries, events, genesis, export

### 10.1 Storage split (DR-10-20, reversible)

- x/policy stores the fixed safety caps, the governed values (moved from authority `0x13`), the pending parameter changes,
  the upgrade plan, the applied-upgrade history, the plan-name history, the persisted module version map, and the app
  version. The requirement row says "Goal 10 policy module stores resource bounds and delayed parameter changes"
  (`docs/requirements.md:461`).
- x/authority keeps proposals, approvals, business ids, the executed set, validators, seats, submitters, pending validator
  changes, retired keys, and the policy version. It is the only writer of x/policy's governed and plan rows, through the
  `PolicyWriter` bound to the payload's nested cache (`chain/x/authority/policy.go:69-132`). A policy write never outlives a
  failed or kept proposal (`TestPolicyWriterFailureWritesNothing`, `TestHookWriteDroppedOnRejectedKeep`).
- No message writes the policy store directly. Only InitGenesis, `InitVersions`, the authority hook, the x/policy
  BeginBlock, and the upgrade runner (including the upgrade handler, through the handler environment of section 14.3,
  whose writes the runner's checks then refuse) write it (invariant I12).

x/policy keys (`chain/x/policy/store.go:18-37`; every value non-empty, because goleveldb reports an empty value as absent):

| Key | Value | Writers |
| --- | --- | --- |
| `0x00` | layout version 2, 8 bytes big-endian | InitGenesis |
| `0x01` | safety caps | InitGenesis only; never changed |
| `0x02 \| limit_key` | governed value, 8 bytes, in 1..ceiling | `SetLimit`; `ApplyDue` |
| `0x03 \| limit_key` | pending change: activation height, value, policy version, proposal id | `SchedulePending`; deleted by `SetLimit` of the key and by `ApplyDue` |
| `0x04` | app version, 8 bytes, at least 1 | `InitVersions`; `RecordUpgrade` |
| `0x05 \| module name` | consensus version, 8 bytes | `InitVersions`; `RecordUpgrade` |
| `0x06` | upgrade plan: height, 32-byte digest, name, proposal id | `ScheduleUpgrade`; deleted by `CancelUpgrade` and `RecordUpgrade` |
| `0x07 \| name` | height at which the upgrade applied | `RecordUpgrade` |
| `0x08 \| name` | the 32-byte release digest the name was first scheduled with; never deleted | `ScheduleUpgrade` (ORCH-10) |

Bounds: at most five `0x02` rows and five `0x03` rows, single `0x04` and `0x06` keys, at most `MaxVersionMapModules` = 32
`0x05` rows (`chain/x/policy/keeper.go:51`; eight stores are mounted, `chain/app/app.go:178-187`), one `0x07` row per
applied upgrade and one `0x08` row per plan name a council execute first scheduled.

### 10.2 Queries

- `svn.policy.v1.Query/Policy`: policy version, safety caps, blocked decisions, and (schema version 2) the governed value
  in force of each of the five keys in key order, the pending changes, the plan, and the app version
  (`chain/x/policy/query.go:39-79`; `TestPolicyQueryThroughABCI`). Bounded by construction. It does not list applied
  upgrades or plan names.
- `svn.authority.v1.Query/Governance`: authority state with paging (DR-10-23, reversible; `chain/x/authority/query.go:89`).
  Proposals carry approver keys only; the v0 proto has no approver seat ids. Every page carries `devnet_test_parameters`,
  the labelled devnet rows in effect, so the review delay is published (REQ-GOV-003, `docs/requirements.md:563`;
  `TestGovernanceDevnetTestParameters`).
- `svnd` has no query subcommand; the runbooks read both queries through CometBFT `abci_query` with
  `scripts/devnet/walk-runbooks.py query`.
- Raw `/store` queries stay unmetered, as in goals 07-09. CometBFT RPC must not be public.

### 10.3 Events (DR-10-22, reversible)

| Source | Event | Typed? |
| --- | --- | --- |
| successful execute, every kind | `svn.events.v1.PolicyChanged` (new policy version, parameter key) | yes; the spec event (spec :1524-1527) |
| same, right after | `svn.events.v1.ProposalExecuted` (proposal id, new policy version) | yes; D-2 |
| keep and failure outcomes | `authority_result` | no; unchanged (DR-06-05) |
| governed-limit execute that schedules or supersedes a change | `policy_pending` | no |
| x/policy BeginBlock, per applied change | `policy_activated` | no |
| plan written, replaced, or cancelled | `upgrade_plan` | no |
| upgrade applied in PreBlock | `upgrade_applied` | no |

The x/policy event names are `chain/x/policy/events.go:19-25`; `upgrade_applied` is `chain/upgrade/runner.go:92-100`.
`TestGovernanceEventSchemas` pins the two typed events and the three x/policy events (names, attribute keys, and counts).
`upgrade_applied` is checked by `TestRunnerAppliesUpgrade` (`chain/upgrade/runner_test.go:475`, one event) and
`TestUpgradeRehearsalInProcess` (`chain/app/upgrade_rehearsal_test.go:839-844`, exactly one in block h). The four untyped goal-10 events are outside the protocol event list
(`docs/protocol.md:296`), as `authority_result` is. They are recorded as event-list additions (DR-10-40). Goal 12 must not
consume them until that is decided.

### 10.4 Genesis and export

- x/policy genesis: policy version 1; safety caps equal to the protocol caps; blocked decisions equal, row for row, to the
  authority genesis rows (checked in `ValidateAuthorityGenesis`, so `svnd genesis validate` applies it too); and none of the
  schema-version-2 query fields: no governed value, pending change, or plan, and `app_version` 0
  (`chain/x/policy/genesis.go:113-157`; `TestGenesisPolicyRules`, `TestGenesisPolicyRefusesVersionAndScheduleFields`).
- Genesis `app_version` is 0 because the four query fields are not genesis state; the chain's app version comes from the
  binary: InitChainer refuses a binary whose app version is not `GenesisAppVersion` = 1 and genesis consensus params whose
  `Version.App` is not 1, then `InitVersions` writes the binary's app version and module version map
  (`chain/app/app.go:489-539`). This is ORCH-9, an interim documented choice; the plan's earlier "genesis app version == 1"
  is superseded.
- An upgrade binary cannot initialize a chain (`TestNewerBinaryRefusesInitChain`), so a new node starts from the genesis
  binary and switches at each plan height.
- Export refuses (`export refused:` with the first prefix) while the policy store holds a governed value, a pending change,
  a plan, an applied upgrade, or a plan name, or while the app version or the policy version is not 1
  (`chain/x/policy/genesis.go:230-252`; `chain/app/app.go:614-644`; `TestExportRefusesGovernanceState`,
  `TestExportRefusesUpgradePlanState`). This joins DR-06-01: restart from export is unsupported once governance state
  exists (`docs/architecture/authority-authorization.md:83,89`). A fresh chain still round-trips byte for byte.

## 11. Upgrade version model

Components (supported framework only; no new Go module). All are parts of the pinned SDK, store, and CometBFT modules, each
under the Apache License 2.0 (the `LICENSE` file of `cosmos-sdk@v0.55.0`, `store/v2@v2.0.0`, and `cometbft@v0.40.0`).

| Need | Component |
| --- | --- |
| run module migrations | `module.Manager.RunMigrations` with `Configurator.RegisterMigration` and an explicit migration order (`cosmos-sdk@v0.55.0/types/module/module.go:703-757`; `cosmos-sdk@v0.55.0/types/module/configurator.go:104-120`; `chain/app/app.go:324`) |
| remember module versions | a persisted version map in x/policy `0x05` |
| switch at one height and refuse a wrong binary in every block | an app PreBlocker step before the module PreBlockers (section 14) |
| atomic failure | the handler runs in a cache context written only on full success; a PreBlock error returns before any commit (`cosmos-sdk@v0.55.0/baseapp/abci.go:859-863`) |
| version visible to CometBFT and clients | `ConsensusParams.Version.App` in the param store; `SetProtocolVersion` from committed state at load and after each Commit (`cosmos-sdk@v0.55.0/baseapp/options.go:193-198,252-258`; `cosmos-sdk@v0.55.0/baseapp/abci.go:1100-1106`; `chain/app/app.go:378-380`) |
| refuse a wrong binary before consensus starts | a load gate in `app.New` (section 13) |

Not used, and why:

- SDK `x/upgrade`: `x/upgrade/types` imports `x/gov/types/v1beta1` (`cosmos-sdk@v0.55.0/x/upgrade/types/proposal.go:3-5`),
  so linking it fails `TestLinkedBinaryOmitsEconomicModules`; its keeper writes files with `os`
  (`cosmos-sdk@v0.55.0/x/upgrade/keeper/keeper.go:528-555`); its authority is one address, not the council.
- cosmovisor: it reacts to `data/upgrade-info.json`, which only SDK x/upgrade writes and svnd never does. Goal 27 may
  revisit binary management.
- Cosmos Enterprise modules: excluded by DEC-006 (`docs/decision-register.md:187`). No commercial-only component is added.
- `baseapp.SetHaltHeight`: a node-local stop is an activation path that no council decision governs (owner ruling Q2: "no
  node-local halt").
- A skip-upgrade height (`cosmos-sdk@v0.55.0/x/upgrade/abci.go:73-86`): it would let a minority of nodes override a council
  decision. `TestNoSkipOrHaltHeight` scans `chain/upgrade`, `chain/node`, and `chain/cmd/svnd` for either.

Version model (PROPOSED, DR-10-26, reversible):

- App version P: a uint64 compiled into each binary. The goal-10 release build has P = 1 (`chain/app/app.go:118-121`). It
  is stored in x/policy `0x04` and equals `ConsensusParams.Version.App`. It is not `genesis.ProtocolVersion`, the protocol
  schema line (`chain/genesis/bounds.go:15`). Moving P is not a protocol schema change.
- Module versions: each module's `ConsensusVersion`. The map is persisted at InitChain. Each module's layout key equals its
  map entry.
- Binary declaration (`chain/upgrade/upgrade.go:94-150`): P, the module map from the module manager, and the known upgrades
  `{Name, FromAppVersion, ToAppVersion, Handler}`. P is 1 for a binary without upgrades and its upgrade's `ToAppVersion`
  otherwise (`chain/app/app.go:132-137`); `Validate` refuses an entry with `To != P` or `From != P - 1`. `svnd version`
  prints P, the module versions, the upgrade lines (or `upgrades none`), and the VCS revision and modified flag from the build
  information (DR-10-30; `TestVersionCommandFields`, `TestSvndVersionMatchesBuildInfo`).

Build tags (ORCH-6, replacing DR-10-25's separate mains; a reversible implementation choice, not an owner decision). Goal
05's rule that `chain/cmd/svnd/main.go` is the only chain main package stays. One main builds every binary; exactly one file
of `chain/cmd/svnd` defines `buildOptions` for each build, and exactly one file defines `statecheckBuild` (the state-check
tag, which combines with either rehearsal tag):

| Build | File | Compiles |
| --- | --- | --- |
| no tag (release) | `chain/cmd/svnd/default_build.go` | no upgrade handler, no chain-id restriction, no `chain/internal` code |
| `-tags svn_rehearsal` (B2) | `chain/cmd/svnd/rehearsal_tag.go` | the rehearsal upgrade `rehearsal-1`, app version 1 to 2; starts only on a `shortvideo-dev-` chain id |
| `-tags svn_rehearsal_bad` (B2-bad) | `chain/cmd/svnd/rehearsal_bad_tag.go` | the same name and versions with the wrong handler (section 18); starts only on a `shortvideo-dev-` chain id |
| `-tags svn_statecheck` (alone or with a rehearsal tag) | `chain/cmd/svnd/statecheck_tag.go` (without the tag: `chain/cmd/svnd/no_statecheck.go`) | the subcommand `svnd statecheck --home H [--height h]` (`chain/internal/statecheck`), which links the test-support checkers `leasestest`, `evidencetest`, and `policytest`; starts only on a `shortvideo-dev-` chain id; describes the binary of the rehearsal tag it is combined with |

The two rehearsal tags together do not compile. Every tagged build refuses `start` on a chain id that does not start with
`shortvideo-dev-`, read as CometBFT reads it (`node.GenesisChainID`: the genesis file with CometBFT's exact-key decoder,
and the genesis document CometBFT stored in `data/state.db` when there is one). `TestDefaultBuildCompilesNoRehearsal`,
`TestRehearsalBuildsDeclareTheRehearsalUpgrade`, `TestRehearsalBuildsRefuseNonDevChains`, `TestStatecheckBuildsReportOnAHome`,
`TestStartRefusesOtherChainIDs`, `TestLinkedBinaryOmitsTestHooks`, and `TestLinkedBinaryOmitsEconomicModules` cover them. A
release build uses no tag, and no tagged build is ever a release (the release checks refuse any `-tags` setting);
`docs/operations/release-preparation.md` says how to verify it.

| Module | ConsensusVersion | Layout key | Migration table |
| --- | --- | --- | --- |
| auth wrapper | 7 (`chain/app/authmodule.go:23`) | none | none; auth 5 to 6 to 7 does not apply because every svnd chain starts auth at 7 (DR-10-24) |
| svn (scaffold) | 1 | none | none; the slot stays a scaffold (DR-10-28) |
| policy | 2 (`chain/x/policy/keeper.go:46`) | `0x00` = 2 | empty |
| providers, content, leases, evidence | 2 (`chain/x/providers/keeper.go:39`; `chain/x/content/keeper.go:40`; `chain/x/leases/keeper.go:53`; `chain/x/evidence/keeper.go:52`) | `0x00` = 2 | empty |
| authority | 2 (`chain/x/authority/module.go:30`) | `0x00` = 2 | empty; registered so a later step can be added |

Framework gaps the runner closes itself (`chain/upgrade/runner.go:288-346`):

- `runModuleMigrations` returns nil when `to <= 1` or the version is unchanged (`cosmos-sdk@v0.55.0/types/module/configurator.go:124-128`),
  and its loop runs nothing when `from > to` (`:136`).
- `RunMigrations` calls `InitGenesis(DefaultGenesis)` for a module missing from the stored map and silently drops a module
  the binary no longer has (`cosmos-sdk@v0.55.0/types/module/module.go:715-753`).
- So the runner checks before migrating that the stored map and the binary's map name the same modules and that no stored
  version is above the binary's.

## 12. Upgrade plans: who schedules, when, and notice

On 2026-11-07 the owner answered Q2 (DR-10-02) with "Council plan, fail closed (Recommended)" and Q3 (DR-10-03) with "3 of 4
+ min notice (Recommended)". Under them:

- A plan is written only by an executed council `software_upgrade` payload, with three of four approvals, the review delay,
  and the expiry. The production plan source of svnd is `upgrade.CouncilPlans` (`chain/upgrade/plans.go:52-105`): it only
  reads the committed plan row through the app's `policyPlanRows` adapter (`chain/app/upgrades.go:158-169`), and refuses a
  damaged row or a plan whose name already applied as an integrity error. Only x/authority and x/policy call the plan
  writers (`TestPlanWritersHaveNoNodeLocalCaller`).
- It names a height only. No time activation (`TestOnChainActivationIsHeightOnly`).
- Lead from the propose block and again from the execute block: at least `MinActivationLeadBlocks` = 100 blocks and at most
  `MaxActivationLeadBlocks` = 1,000,000 blocks, both PROPOSED (`chain/msgkit/lead.go:16-30`). Below is
  `ERROR_CODE_OUT_OF_RANGE` and the proposal stays `QUEUED`; above is `ERROR_CODE_BOUNDS`. The two constants are defined once
  in `chain/msgkit`, which both x/authority and x/policy import (ORCH-5). Devnet consensus timeouts are a few hundred
  milliseconds (`chain/node/start.go:237-246`), so [Educated Guess] 100 devnet blocks take about a minute or less: it is a
  minimum that the executor cannot shorten, not a real notice period.
- A plan name can never be reused with a different release digest (owner ruling Q3). x/policy keeps the digest a name was
  first scheduled with at `0x08` and never deletes it (`chain/x/policy/writer.go:170-197`). `ScheduleUpgrade` checks, in
  order: `BOUNDS` for the name or digest, the height window, `DUPLICATE_ID` for an applied name, then `CONFLICT` if an
  earlier plan of that name, pending, cancelled, or replaced, had another digest. The same name with the same digest may be
  scheduled again. This strict reading is ORCH-10, an orchestrator choice that implements the ruling's text.
- A later plan replaces a pending plan (DR-10-32).
- It is deleted only by an executed `cancel_upgrade` before its height (`chain/x/policy/writer.go:205-217`). The council's
  cancel is the only cancel path: there is no skip height, no node-local stop height, and no local command
  (`TestNoSkipOrHaltHeight`). After the height the chain has either applied the plan or halted, so no cancel can commit
  (`TestCancelBeforeHeightRemovesPlan`).
- The single `AUTHORITY` executor still chooses when, inside that window, a queued plan executes, and may withhold a cancel
  (section 23).
- The plan's `release_digest` is information for operators. The chain cannot verify which binary a node runs. The runbooks
  compare the digest with the reviewed release manifest (DR-10-18; `docs/operations/source-and-digest-review.md`).
- Off a `shortvideo-dev-` chain every council message is `ERROR_CODE_DECISION_BLOCKED`, so no upgrade can be scheduled on
  any other chain.

## 13. Load-time gate

`app.New` runs the gate after loading the latest committed version, outside consensus, only when a block has been committed
(`chain/app/app.go:392-397,437-455`). At height 0 the InitChainer rules of section 10.4 apply instead. An error from
`app.New` makes svnd exit 1 before CometBFT starts and before the validator key is loaded (`chain/node/start.go:84-88,95-98`).
Every refusal reads `load gate at height L refused this binary (row N): ...`; a keeper panic on a committed row that does not
decode, or a failed store read, is row 6 (`chain/app/app.go:461-477`).

Inputs. Committed state: stored app version `Ps`, stored consensus `Version.App`, version map `VMs`, layout keys, plan `Pl`,
last height `L`. Binary: `P`, module map `VM`, known upgrades `U`. "Consistent" means `Ps` is present and equals the stored
`Version.App`, every stored entry is at least 1, every layout key equals its `VMs` entry, and `VMs` and `VM` name the same
modules (`chain/upgrade/gate.go:153-188`).

| Row | Case | Outcome | Message |
| --- | --- | --- | --- |
| 1 | consistent, `Ps == P`, `VMs == VM`, no plan or plan height > `L+1` | start | |
| 2 | consistent, `Ps == P`, `Pl.height == L+1` | refuse | `upgrade <name> is due at height <L+1>; install the binary for it (release digest <hex>)` |
| 3 | consistent, `Ps == P-1`, `Pl.height == L+1`, exactly one handler for `Pl.name` in `U` with `From == Ps` and `To == P`, every `VMs[m] <= VM[m]` | start in the upgrade window | |
| 4 | as row 3, but `Pl.height > L+1` | refuse | `too early: run the previous binary until height <Pl.height-1> ...` |
| 5 | `Ps > P`, or any `VMs[m] > VM[m]`, or a stored module the binary lacks | refuse | `state is newer than this binary (app version <Ps>, binary <P>)` |
| 6 | otherwise (for example no stored app version, a goal 05 to 09 home; `Ps == P` with some `VMs[m] < VM[m]`; `Ps < P-1`; `Ps == P-1` without a plan or a matching handler; a layout key that differs from `VMs`; `Ps` not equal to the stored `Version.App`; `Pl.height <= L`) | refuse | the named mismatch; for a goal 05 to 09 home, "initialize a new home" |

The gate is a pure function (`chain/upgrade/gate.go:79-151`). It starts if and only if row 1 or row 3 holds; every other
input refuses (`FuzzGateStartsOnlyOnRows1And3`). It is table-tested on reopened databases (`TestLoadGateTable`) and with the
real x/policy keeper (`TestOnChainGateTable`).

Goal-09 homes are refused and must be re-initialized, as goals 07-09 did (`docs/operations/validator-lifecycle.md`, "Old
devnet homes"; DR-10-04; `TestAppRefusesScaffoldLayout`).

## 14. Per-block rule and the upgrade runner

### 14.1 Why a per-block rule is needed

- CometBFT compares a header's `Version.App` with its own state (`cometbft@v0.40.0/state/validation.go:32-35`). That
  compares two CometBFT values, not the binary.
- Replay and block sync feed stored blocks to whatever binary runs (`cometbft@v0.40.0/consensus/replay.go:478-501`).
- The SDK checks only the halt settings and the height before PreBlock (`cosmos-sdk@v0.55.0/baseapp/abci.go:792-798`).
- So a binary that does not check the app version itself would execute old blocks with new code.

### 14.2 PreBlock step, every block (D-6)

The app's PreBlocker runs the runner's step first, before every module PreBlock and before any transaction
(`chain/app/app.go:548-569`). It reads the stored app version `A` and the plan, then applies `CheckBlock`
(`chain/upgrade/block.go:23-55`):

1. If a plan exists and `Pl.height < height`: `INTEGRITY` error (cannot happen while I4 holds).
2. If `Pl.height == height`: no handler for `Pl.name` gives the error
   `UPGRADE "<name>" NEEDED at height <h> release_digest=<hex>`; two handlers of that name, or a handler with `From != A` or
   `To != P`, is a wrong-binary error; otherwise the runner applies the upgrade (below).
3. Otherwise, if `A != P`: error `stored app version <A>, binary <P>: wrong binary for height <h>`.

So an upgrade binary refuses every block below the plan height, whether it replays its own block store, block-syncs from
genesis, or was started early. No binary runs blocks of an app version it was not built for
(`TestPreBlockRefusesAppVersionMismatch`, `TestOnChainBlockRule`).

Applying the upgrade, all inside one cache context of the PreBlock context (`chain/upgrade/runner.go:243-266`):

1. Preconditions (`chain/upgrade/runner.go:302-346`): the stored map names exactly the binary's modules, every entry in
   1..binary version, every layout key equals its entry, the stored `Version.App` equals the stored app version; the I13
   snapshots are taken.
2. The handler runs with a copy of the stored map, `Migrate` (the module manager's `RunMigrations` in `InitOrder`), and the
   handler environment (section 14.3). A handler error, or a panic in the handler or a migration it runs, is a handler
   error (`chain/upgrade/runner.go:275-286`).
3. Post-conditions (`chain/upgrade/runner.go:356-404`): the returned map equals the binary's map; every layout key equals it;
   the handler left the runner's own rows unchanged (the stored app version, the stored version map, the plan, the consensus
   params, the policy version); every I13 snapshot is byte-equal (section 14.4).
4. Record (`chain/upgrade/runner.go:414-460`): `RecordUpgrade` writes `0x07[name] = h`, the new version map, the new app
   version, and deletes the plan (it refuses an absent plan row); `ConsensusParams.Version.App = To`, every other param
   unchanged; the policy version + 1; each row is read back. Then the event `upgrade_applied`.
5. The cache is written only after 1-4 succeed. PreBlock reports changed consensus params.
6. Any error in 1-4 returns before the cache is written, so the block fails the same way on every node that runs the same
   binary on the same state. The SDK reuses a failed attempt's finalize state for a retry of the same height
   (`cosmos-sdk@v0.55.0/baseapp/abci.go:817-820,1016-1022`); because the upgrade step runs first and wrote nothing into it,
   a retry starts from the same state. Block h does not commit on that node. Restarting the same binary repeats the
   attempt; the plan is still due (`TestUpgradeHandlerErrorHalts`).

`SetProtocolVersion` is set from committed `0x04` at load and after each Commit (`SetPrepareCheckStater`,
`chain/app/app.go:378-380`), never inside PreBlock, so ABCI Info (`cosmos-sdk@v0.55.0/baseapp/abci.go:147-159`) never
reports an app version that is not committed.

The handler runs once per chain. Its work is bounded by state size, not by a per-block cap. Chain code has no clock, so the
rehearsal measures the time of block h in the test, not in the chain.

### 14.3 Handler environment (ORCH-7)

An SDK upgrade handler normally reaches module stores through keepers it captures. Here the runner gives every handler an
`upgrade.Env` (`chain/upgrade/env.go:20-44`): by-name access to the mounted stores, in the cache branch the runner holds.
The app builds it from its store keys (`handlerEnv`, `chain/app/upgrades.go:73-106`) and the runner passes it in the
handler's context (`WithEnv`; `TestRunnerGivesHandlerTheEnv`). ORCH-7 is an orchestrator choice; passing the environment in
the context, not as a new handler parameter, is a reversible deviation from its wording.

A handler may read and write any store through it, including x/authority and x/policy. The post-conditions treat these
writes like a migration's: a change to a runner-owned row or an I13 class refuses the upgrade
(`TestRunnerRefusesProtectedEnvWrites`, `TestUpgradeHandlerEnvCannotChangeProtectedRows`,
`TestUpgradeCannotChangePlanNameOrRunnerKeyRows`); a write to any other row is kept when every check passes. The
runner-owned rows are the stored app version (x/policy `{0x04}`), the version map (x/policy `0x05`), the plan (x/policy
`{0x06}`), the layout keys (each equal to its version map entry), the consensus params (svn store `{0x03}`), and the
authority policy version (`{0x04}`) (`chain/upgrade/runner.go:356-404`). Rows outside x/authority, x/policy, and those
runner-owned rows (every other module store, and the svn store's genesis row `{0x01}`) are not checked. That is how the
wrong rehearsal handler B2-bad diverges (section 18).

### 14.4 The self-check (I13)

The I13 classes are `rowClasses` (`chain/app/upgrades.go:190-269`), compared byte for byte before and after the handler:

- one class per authority prefix `0x00`, `0x01`, `0x02`, `0x03`, `0x10`, `0x11`, `0x12`, `0x14`, `0x15`, `0x16`;
- "authority other rows": every other authority row except exactly the key `{0x04}`, the policy version, which moves by
  exactly 1 and which the runner checks itself. A longer key that starts with `0x04` is in this class
  (`TestMigrationCannotAddRowsBesideThePolicyVersion`);
- one class per x/policy prefix: safety caps (`0x01`), governed values (`0x02`), pending changes (`0x03`), applied
  upgrades (`0x07`), and plan names (`0x08`, which hold the Q3 rule that a plan name is never reused with another
  release digest). The runner writes the applied row of the upgrade only in its record step, after these checks;
- "policy other rows": every other x/policy row except the version map prefix `0x05` and exactly the keys `{0x00}`
  (layout), `{0x04}` (app version), and `{0x06}` (plan), which the runner checks itself. A longer key that starts with
  one of those bytes, or a key under no known prefix, is in this class (`TestUpgradeCannotChangePlanNameOrRunnerKeyRows`).

Every x/authority and x/policy row is therefore either in exactly one class or checked by the runner itself (section
14.3). These classes were added to x/policy in the Wave 2 fix round after the security review showed that a handler
writing through the handler environment could add or delete `0x07` and `0x08` rows, or add unknown x/policy keys, and the
upgrade still applied. Consensus params are compared whole except `Version.App`. `TestUpgradePostConditionClasses` pins
the class names, and `TestMigrationCannotChangeAuthorityRows`, `TestUpgradeHandlerEnvCannotChangeProtectedRows`,
`TestUpgradeCannotChangePlanNameOrRunnerKeyRows`, `TestMigrationCannotChangeSafetyCaps`,
`TestMigrationCannotApproveDecision`, and `TestMigrationCannotChangeBlockParams` show refusals. A class snapshot is an error, and so refuses the upgrade, when the version map names an authority or policy
layout this binary cannot read. The runner refuses to start without at least one class, so the self-check cannot be left
out by omission. I13 protects only against an honest binary's mistakes (section 9).

### 14.5 The upgrade window (DR-10-41)

Between a row-3 start and the commit of block h, `CheckTx` (ante only) and gRPC queries run new code on old state.
`Query/Policy` then reports the old app version and the due plan (`TestUpgradeWindowCheckTxAndQueries`).

- When the block store already holds h (every node that halted at h), CometBFT's handshake replays h inside node
  construction (`cometbft@v0.40.0/node/node.go:381`), before `tmNode.Start()` and before svnd starts its gRPC server
  (`chain/node/start.go:95-139`). The window closes before any `CheckTx` or query.
- When the block store does not hold h (a node stopped and swapped before h was decided), the window stays open until h
  commits.
- After any load, with or without an upgrade, the SDK check state has height 0 until the first Commit, so `CheckTx` rejects
  a correctly signed transaction from a normal account with sdk code 4 until the node commits a block
  (`TestUpgradeWindowCheckTxAndQueries`, which shows it for a plain restart too). The runbooks say to wait for one committed
  block after a start before broadcasting.

Interim: documented and recorded by a test; not refused; not fail-closed (section 22).

## 15. Failure behaviour without resetting history

"Unchanged" means logically unchanged, shown by several readings: the committed `LastCommitID` (version and hash); a
snapshot of every committed store; after an in-process FinalizeBlock error, a snapshot of the finalize branch, because a
committed snapshot cannot change when no Commit runs; per-store digests; CometBFT block hashes at fixed heights; the
`genesis.json` sha256; `/status` `earliest_block_height` = 1. Opening goleveldb may rewrite its own files, so file bytes are
not compared.

| Case | Expected behaviour | Mechanism | Test |
| --- | --- | --- | --- |
| I1 old binary reaches the plan height | PreBlock error `UPGRADE ... NEEDED`; CometBFT logs "CONSENSUS FAILURE!!!" and stops consensus; block h stays in the block store; the app stays at h-1 | section 14.2 step 2; CometBFT saves a decided block before applying it (`cometbft@v0.40.0/consensus/state.go:1766-1775`) and recovers the panic (`cometbft@v0.40.0/consensus/state.go:804-817`) | app level: `TestUpgradeRehearsalInProcess` (FinalizeBlock(h) errors and the finalize branch equals the committed state), `TestOnChainHaltAndSwitch`; DEVNET: `TestDevnetUpgradeRehearsal`, all four nodes log `UPGRADE "rehearsal-1" NEEDED` and `CONSENSUS FAILURE` |
| I2 old binary restarted after the halt | exit 1 at `app.New` | gate row 2 | `TestUpgradeRehearsalInProcess`, `TestLoadGateTable`; DEVNET: `TestDevnetUpgradeRehearsal`, B1 restarted after the halt is refused with row 2 |
| I3 new binary started too early | exit 1 at `app.New` | gate row 4 | `TestLoadGateTable`, `TestOnChainGateTable`; DEVNET: `TestDevnetUpgradeRehearsal`, B2 on the copy taken before h-1 is refused with row 4 |
| I4 old binary on post-upgrade state | exit 1 at `app.New` | gate row 5 | `TestUpgradeRehearsalInProcess`; DEVNET: `TestDevnetUpgradeRehearsal`, B1 on a node after the upgrade is refused with row 5. The prior release (goal 09, `ff951fc`) checks only four registry layouts (`ff951fc:chain/app/app.go:328-354`) and has no app-version rule, so [Educated Guess] it starts on a goal-10 home (DR-10-43) |
| I5 binary mounting a store absent from the root | exit 1 at load, before the gate | the root store refuses a store whose version differs from the root version unless a store upgrade adds it (`store/v2@v2.0.0/rootmulti/store.go:259-263`) | `TestStoreAbsentFromRootRefused`; store additions are DEFERRED (DR-10-33) |
| I6 wrong migration on one of four nodes | that node commits a different app hash at h, refuses the majority's block h+1 (`wrong Block.Header.AppHash`), then stops consensus with a CometBFT consensus failure, "+2/3 prevoted for an invalid block" (precommit step) or "+2/3 committed an invalid block" (commit finalization), whichever it sees first; three continue | `cometbft@v0.40.0/consensus/state.go:1563-1564,1750-1751` | app level: `TestWrongMigrationDiverges` shows the different app hash at h with B2-bad; DEVNET: `TestDevnetMinorityWrongMigration` shows the halt and the section 16.1 action 2 recovery, accepting either CometBFT line |
| I7 wrong binary replaying history | an upgrade binary fails PreBlock at the first block below h; on a fresh home it fails InitChain | section 14.2 step 3; section 10.4 | `TestPreBlockRefusesAppVersionMismatch`, `TestNewerBinaryRefusesInitChain`; DEVNET: `TestDevnetUpgradeRehearsal`, B2 on a fresh home is refused at InitChain |
| I8 handler fails on every node | every node halts at h identically; nothing of h commits; the plan stays due | section 14.2 step 6 | `TestUpgradeHandlerErrorHalts`: the same failure twice, then a corrected binary for the same plan name applies it. Its digest differs from the plan's informational `release_digest`, which a halted council cannot update; operators then rely on the reviewed manifest |

In every row the block store is never rewritten and genesis is never replaced. The only restore is the one of section 16.1
action 2. svnd sets no pruning or block-retention option. So the root store keeps every version, its default
(`store/v2@v2.0.0/rootmulti/store.go:87-104`), and the app asks CometBFT to retain every block when the minimum retained
block count is 0 (`cosmos-sdk@v0.55.0/baseapp/abci.go:1456-1462`). Any committed height can then be loaded or replayed on a
stopped node or a copy.

DEVNET in the table marks a test on a single-machine four-process devnet (not independent operators).
`TestDevnetUpgradeRehearsal` and `TestDevnetMinorityWrongMigration` exercise I1 to I4, I6, and I7 that way; their results on the tested revision, with logs, are reported in the goal-10 evidence file; this
file does not claim them.

## 16. Operator actions: allowed, forbidden, and when rollback is unsafe

The procedures are in `docs/operations/`: `council-changes.md`, `release-preparation.md`, `source-and-digest-review.md`,
`operator-installation.md`, `planned-halt-and-restart.md`, `failed-upgrade.md`, and `governance-authorities.md`.
`tests/unit/test_operations_docs.py` checks them, and `scripts/devnet/walk-runbooks.py` runs their tagged shell blocks on a
disposable single-machine devnet.

### 16.1 Allowed after a failure

Owner ruling Q2: "After a failure an operator may: restart with a corrected binary; restore a lone diverged node from its own
copy taken at the halt; or wait for a fix-forward upgrade."

1. Restart at the same height with a corrected binary for the same plan name (I8).
2. Restore one node that diverged alone (I6), only after it logged `wrong Block.Header.AppHash` and then "+2/3 prevoted for
   an invalid block" or "+2/3 committed an invalid block", and the other nodes kept committing: stop it; keep its newest
   `priv_validator_state.json`; replace its `data/` with the `data/` of its own copy taken at the halt (block store at h,
   state and app at h-1), moving the diverged `data/` aside; put the newest signing state back; start the correct binary.
   CometBFT replays block h with the real app (`cometbft@v0.40.0/consensus/replay.go:416-433`) and the node syncs from peers.
   `TestDevnetMinorityWrongMigration` (single-machine four-process devnet (not independent operators)) restores `application.db`, `blockstore.db`, `state.db`,
   `evidence.db`, and `cs.wal` from the node's own copy and keeps the newest signing state byte for byte. Restoring
   `application.db` alone should fail on such a node, because the handshake asserts the app hash in CometBFT's state
   (`cometbft@v0.40.0/consensus/replay.go:404-409,554-563`); that is read from the CometBFT source and not exercised by any
   test. This is a local action that rejoins committed history.
3. Wait for a fix-forward upgrade that the council schedules once the chain runs again.

Before the plan height an operator may also choose not to install the binary. The council cannot force installation. If
more than one third of power does not install, no block h is made (DEC-011, `docs/protocol.md:150`).

Validator signing state lives in `data/priv_validator_state.json` (`cometbft@v0.40.0/config/config.go:41`). Copying a whole
home back would restore old signing state, which is why action 2 keeps the newest file.

### 16.2 Forbidden

Owner ruling Q2: "Never rollback --hard, unsafe-reset-all, a signing-state reset or a new genesis."

- An old binary on migrated state (gate row 5 refuses it).
- Any chain-wide return below a height that more than two thirds of power committed.
- `unsafe-reset-all`, `rollback --hard`.
- Resetting validator signing state, or restoring an older `priv_validator_state.json`.
- A new genesis presented as recovery.
- Two live copies of one validator key.
- A minority skip of a scheduled upgrade, or a node-local halt height used as an upgrade trigger.
- Any message, command, or runbook step that marks a local row final or writes a commit
  (REQ-REC-001, `docs/requirements.md:494`).

### 16.3 When rollback is unsafe

- Before block h commits anywhere (I1, I8): there is nothing to roll back. The plan stays due. The forward paths are a
  corrected binary for the same plan name, or a cancel executed before h, which is impossible once the chain has halted.
- After more than two thirds of power committed h: running the old binary on migrated state is refused by design. Restoring
  pre-h state on every node and re-executing h discards a committed block. That is a finality bypass, and the goal forbids
  it: "A governance stall cannot be fixed by an unaudited finality bypass."
  (`docs/goals/10-policy-governance-and-chain-upgrades.md:28-29`; TM-STALL, `docs/threat-model.md:143`). A later
  forward-fix upgrade is the path.
- One node diverged alone (I6): section 16.1 action 2 only.
- Permanent loss of more than one third of power: no on-chain path exists (DR-10-11, section 22).

### 16.4 Planned halt

- Block h+1's header time is the median time of block h's precommits (`cometbft@v0.40.0/state/state.go:245-253`), which were
  cast before the halt. So h+1 carries a time from before the halt and the jump appears at h+2.
- Expiry does not run while the chain is not committing blocks (`docs/protocol.md:258`), and no lease renewal can commit
  while the chain is halted. Everything that fell due during the halt becomes due at h+2, at most 128 items per block
  (`docs/protocol.md:256`). Providers renew leases whose deadline falls in the expected halt before h.
- No maximum planned halt is set (DR-10-39).
- A new node joining after an upgrade starts from the genesis binary and switches at each plan height.
- After an upgrade that changes a layout, queries and state checks for heights below h must use the old binary.

## 17. Separation of local emergency controls, protocol changes, and publication authority

| Authority | Who | May | May not | Where enforced or documented |
| --- | --- | --- | --- | --- |
| Local emergency controls | each node operator, for its own node | stop its process; close its own ports; disable its own endpoint locally ("Emergency local disablement of a compromised endpoint is not a committed chain decision.", `docs/decision-register.md:247`); decide whether to install a scheduled binary; restore its own diverged node under section 16.1 action 2 | write a committed decision; change another node; change finality; skip a council upgrade; use a node-local stop height as an upgrade trigger (owner ruling Q2); approve anything | node code and runbooks; no message exists for any of it (`docs/architecture/authority-authorization.md:97-99`) |
| Protocol changes | the council (DEC-008 approvals; owner ruling Q3) proposes and approves; the `AUTHORITY` account executes; operators then install reviewed binaries | validator power, governed limits, admit, disable, force-withdraw, upgrade plans and cancels, seat key replacement | through any payload: approve a blocked decision, change a safety cap, act without the delay and expiry rules, force an operator to install. An upgrade binary can change any rule; the control on it is human review and each operator's choice (section 9) | x/authority, x/policy, the ante (DR-06-06) |
| Public publication authority | the project maintainers ("The project's maintainers control what code enters the reference repository.", `docs/blueprint.txt:1033-1034`); release signing and tags are owner actions in goal 27 | decide what code enters the reference repository; prepare release candidates | change chain state; schedule an upgrade; run a validator by virtue of the role | runbooks; DEC-006 blocks release notices (`docs/decision-register.md:203`); goal 10 does none of it |

None of the three owns user content ("Do not conflate any of these with ownership of users' content.",
`docs/blueprint.txt:1034`; REQ-GOV-002, `docs/requirements.md:562`). The devnet concentrates every role in one operator.
Community governance (goal 23) is named, not designed. Goal 10 adds no on-chain pause switch, emergency key, or
single-signature path (DEC-009, `docs/decision-register.md:281`). `docs/operations/governance-authorities.md` restates this
for operators.

Goal 10 itself does not auto-approve a policy, tag a release, rotate a live key, or change a real operator
(`docs/goals/10-policy-governance-and-chain-upgrades.md:30-31`). No committed state, devnet genesis, or positive-path
fixture sets a blocked decision to approved, fills the approved council, or sets a quorum-loss behaviour.

## 18. Rehearsal heights and binaries

These heights are specified before the rehearsal runs (`docs/goals/10-policy-governance-and-chain-upgrades.md:37`). The
devnet rehearsal must log every height it used and a per-height table (block hash, header app hash, header `Version.App`,
block time, transaction count) on all four nodes; the ABCI Info app version reports only the latest height, so it is
checked after the upgrade at the latest height. The h+5 row and the tip row were revised in the Wave 2 fix round, before
any evidence run, to name the heights `TestDevnetUpgradeRehearsal` uses (restart and replay at the tip, not at h+5).

| Height | Meaning |
| --- | --- |
| h-1 | last block under app version 1; pre-upgrade queries and state check |
| h | plan height: the old binary halts (I1); the new binary applies the upgrade in PreBlock |
| h+1 | first header with the new `Version.App` (CometBFT copies it from FinalizeBlock(h)'s consensus params, `cometbft@v0.40.0/state/execution.go:765-778`); header time near the time before the halt; post-upgrade queries |
| h+2 | first block time after the halt (section 16.4) |
| h+5 (PROPOSED) | post-upgrade state-check point: the committed state at h+5 is checked on all four stopped homes (byte-identical reports), and h+5 is in the per-height table |
| tip after the crossing transitions (logged as `final`) | restart of all four nodes (DR-10-41), two-phase replay of blocks 1 to the tip, and the last state check |
| a-1, a | effective-1 and effective heights of a scheduled governed change |

Binaries (built from one tree, logged by sha256, and deleted; not independently built releases, not signed, not reviewed
source):

- B1: the release build of the tested tree (app version 1, `upgrades none`).
- B2: `-tags svn_rehearsal`, the upgrade `rehearsal-1` from app version 1 to 2 (`chain/internal/rehearsal/rehearsal.go:72-127`).
  Its handler refuses a chain id outside `shortvideo-dev-`, needs the handler environment, then walks the provider,
  retired-key, content, tombstone, offer, lease, and evidence records read-only (each decodes with no unknown field, sits
  under the key its own fields name, and re-encodes to the stored bytes; proposal records are checked only for shape,
  because x/authority's canonical decoder is not exported), then runs the module migrations. It changes only the rows the
  runner owns.
- B2-bad: `-tags svn_rehearsal_bad`, the same name and app versions; its handler (`BadHandler`,
  `chain/internal/rehearsal/rehearsal.go:149-161`) runs B2's handler and then writes one documented row through the handler
  environment: key `0xFF`, value `0x01`, in the x/leases store. x/leases is not an I13 class, so the runner's checks pass and
  a node that runs B2-bad commits another app hash at h than the nodes that run B2 (I6; `TestWrongMigrationDiverges`; the
  lease invariants refuse the row).

Rehearsal rules (PROPOSED):

- App level (`chain/app/upgrade_rehearsal_test.go`): two goleveldb nodes run the same representative state; an in-process
  v2 also migrates leases and evidence from layout 2 to 3, re-encoding every record (DR-10-36), which the real B2 does not.
  Devnet: four processes on one machine (not independent operators) swap binaries.
- Representative state holds providers, active, expired, and ended leases, open offers, evidence records, open and queued
  proposals, a pending validator change, a governed value, and a pending governed change. A census at h-1 and after the
  upgrade requires every class to be greater than 0.
- No deadline, expiry, or activation falls in `[time(h-1), time(h+2)]`. On the devnet, cross-boundary deadlines are the
  estimated header time of h+2 (the measured block rate plus a resume allowance of 40 s) plus a margin of 60 s, both
  PROPOSED test parameters (`resumeAllowance`, `crossMargin` in `chain/devnet/upgrade_test.go`); the test fails with "halt
  exceeded the margin" if the halt is longer.
- Transactions stop before h-1; blocks h-1, h, and h+1 hold none. The comparison between h-1 and h+1 allows exactly these
  differences: app version, plan (gone), upgrade record (new), policy version (+1), consensus `Version.App`, and, at app
  level only, the leases and evidence version-map entries and layout keys that the in-process migration moves.
- The minimum lead (100 blocks, PROPOSED) applies to the rehearsal plan too.

## 19. Deviations from `spec_machine.py`

Goal 06's recorded differences stay. Goal 10's own (`chain/x/authority/README.md`, "Goal 10 deviations from the spec
machine"; `chain/x/policy/README.md`):

| Id | Chain | Spec | Why |
| --- | --- | --- | --- |
| D-1 | governed limit key outside the five caps, or value above the ceiling, is `BOUNDS` at propose (step 7a) | accepted at propose; `BOUNDS` at execute, proposal stays `QUEUED` (spec :1338-1343) | a proposal that can never execute should not hold a quota slot or a seat's time |
| D-2 | `ProposalExecuted` emitted after `PolicyChanged` | `PolicyChanged` only (spec :1526) | `docs/protocol.md:296` lists both; goal-06 follow-up X5 |
| D-3 | withdrawn (section 7: a recount by seat key cannot be built from the record and no message reaches it) | | |
| D-4 | governed limit with an activation height: pending, applied at BeginBlock of that height; an immediate change deletes a pending row of the same key | `activationHeight` is `MALFORMED` | owner ruling Q1; "delayed parameter changes" (`docs/requirements.md:461`) |
| D-5 | `software_upgrade`, `cancel_upgrade`, `council_seat` accepted, with the plan-name rule of section 12 | an unknown payload alternative is `MALFORMED` | owner ruling Q1; `docs/goals/10-policy-governance-and-chain-upgrades.md:18,21,34` |
| D-6 | PreBlock: at the plan height a binary without the handler fails; at any height a stored app version that differs from the binary's fails, except at the plan height with a matching handler; nothing of the block commits | no upgrade concept | `docs/goals/10-policy-governance-and-chain-upgrades.md:21-23`; section 14.1 |
| D-7 | a seat key equal to a current or pending validator consensus key is refused at genesis and in `council_seat`; a validator payload whose consensus key equals a current or retired seat key is `DUPLICATE_ID` at execute | not checked | key separation (REQ-SEC-002, `docs/requirements.md:543`) |
| D-8 | x/policy applies due changes in BeginBlock before x/leases expiry | no policy BeginBlock | activation is effective at height a for every reader |
| D-9 | `Query/Policy` and `Query/Governance` registered with paging | no query semantics | REQ-GOV-003 "published delay" |
| D-10 | genesis refuses a seat id longer than 59 characters (PROPOSED) | id pattern up to 64 (`chain/x/authority/payload.go:48`) | `seat-<seat_id>` must fit the 64-character parameter-key pattern, or the seat could never be replaced |

The keeper-level differential (`chain/x/authority/difftest`, through `tests/difftest/spec_oracle.py`; keeper level, not end
to end) names the council rules G-1 to G-11. G-1 to G-5 and G-7 are recorded in `docs/evidence/goal-06.md:316-322`. G-8 (the
validator set re-checked after a due group; no message reaches it) and G-9 (validator activation unix time out of range:
`BOUNDS` on the chain, `MALFORMED` in the spec) were first written down by goal 10. The exact scopes of G-6, G-10, and G-11
are defined in the oracle's module docstring (`tests/difftest/spec_oracle.py:146-234`) and repeated in
`chain/x/authority/README.md`:

- G-6 (ORCH-2, accepted as implemented): when the council shape is not the approved one (seat count other than
  `ApprovedSeats` or threshold other than `ApprovedThreshold`), every council step during which the spec called
  `council_ready` answers `DECISION_BLOCKED`, whatever code the spec then returned, and nothing is kept. It is checked per
  step and is not reported when the spec's own code was already `DECISION_BLOCKED`; a step that stops before
  `council_ready` is not changed.
- G-10 (ORCH-2, accepted as implemented): a propose whose spec payload validation answered `MALFORMED`, with a payload that
  is exactly a validator power body whose keys are within `consensusPubkey`, `nodeId`, `reasonDigest`, `activation`, and
  `power`, with a canonical 32-byte consensus key present and a node id present and outside the id pattern, is `BOUNDS`.
  The reason digest, activation, and power are not read, so absent ones are included. An absent node id is not included;
  it is G-11 condition 10.
- G-11, "payload field shape: chain `BOUNDS` where the spec says `MALFORMED`" (ORCH-1 for conditions 1-8, ORCH-8 for 9-13;
  open request DR-10-44): a propose whose spec payload validation answered `MALFORMED`, with every earlier check of the
  chain's parse passing, and one of exactly thirteen conditions: (1) a validator reason digest naming SHA-256 whose value
  is not 32 bytes; (2) a negative validator power; (3) an empty consensus key; (4) a force-withdraw reason digest naming
  SHA-256 whose value is not 32 bytes; (5) a force-withdraw content id outside the id pattern with a reason digest whose
  algorithm is omitted; (6) a governed value of 0; (7) an empty governed limit key; (8) an empty provider id in an admit or
  a disable payload; (9) a validator consensus key that is not empty and not 32 bytes, with the reason digest omitted;
  (10) an empty validator node id with a 32-byte consensus key; (11) a force-withdraw content id outside the id pattern with
  the reason digest omitted; (12) the same with content version 0 and a reason digest the spec parses; (13) the same with a
  SHA-256 reason digest whose value is not 32 bytes. Both sides reject and neither changes state. `payload.go` codes are not
  changed, because they are client-visible. G-9, G-10, and G-11 are disjoint (`TestCouncilRulesAreLoadBearing`,
  `TestShapeRuleNearMisses`).

Open differences that no rule names. Three divergence classes are open and need a decision (widen G-11, add a rule, or
change the chain). Each is chain `BOUNDS` where the spec says `MALFORMED`, at propose, and neither side changes state; the
random generator never sends them, and `TestUnnamedParseDivergences` pins one row per class:

1. a validator consensus key that is not 32 bytes with a reason digest present and the node id omitted;
2. the same with the node id present and the activation omitted;
3. a force-withdraw content id outside the id pattern with a reason digest whose algorithm is a value outside the enum.

The same test proposes one payload for every combination of v0 field shape classes (859 payloads) and finds no other parse
divergence that no rule names; the claim that no other v0 payload diverges unnamed rests on the class argument written in
that test, which sends one representative per combination, not every value. The schema-version-2 payloads diverge by D-4
and D-5 only (`TestSchemaTwoPayloadsD5`). A divergence from `spec_machine.py` outside G-1 to G-11, D-1 to D-10, and the three
open classes above is reported, never hidden.

## 20. Invariants

- I1 policy version. State-only form: `version >= 1 + number of EXECUTED records`. Exact form, in harnesses that count
  operations: `version == 1 + successful executes + applied upgrades`.
- I2 every governed value row has a key in `governed_limit_caps` and a value in 1..ceiling.
- I3 at most one pending row per key; every pending activation height is above the last committed height; no pending row
  is older than the latest executed change of its key (exact form: the latest executed decision for a key wins).
- I4 at most one plan; its height is above the last committed height while the chain runs; its name has no upgrade record;
  every plan name row holds a 32-byte digest, the plan's name row holds the plan's digest, and every applied name has a name
  row.
- I5 stored app version = `ConsensusParams.Version.App` = 1 + number of upgrade records; outside the upgrade window it
  equals the running binary's app version.
- I6 the persisted version map equals the running binary's map outside the upgrade window, and each layout key equals the
  map entry of its module.
- I7 safety caps equal the protocol caps; every decision row unapproved with value version 0; approved council empty;
  quorum loss `UNSET`; devnet rows unchanged since genesis.
- I8 exactly four seats; seat keys distinct, none retired, none equal to a current or pending validator consensus key; no
  validator consensus key equal to a retired seat key; no seat address is a submitter.
- I9 every `OPEN` or `QUEUED` proposal has distinct approver seat ids; `QUEUED` has at least three.
- I10 open counts equal the `OPEN` and `QUEUED` proposals per base version and seat.
- I11 the validator set and every pending validator change meet DEC-011 at every activation point.
- I12 only InitGenesis, `InitVersions`, the authority hook, the x/policy BeginBlock, and the upgrade runner (including
  the upgrade handler, through the handler environment) write the policy store.
- I13 the upgrade runner's self-check leaves unchanged every authority row except the exact policy-version key, which moves
  by exactly 1; every x/policy row except the rows the runner checks itself (layout key, app version, version map, plan);
  and every consensus parameter except `Version.App` (section 14.4). I13 protects only against an honest binary's mistakes
  (section 9).

`chain/x/policy/policytest` checks I2 to I6 for x/policy (with a self-test that injects one violation per rule), and the
authority differential checks them after every step at keeper level. The state check `chain/internal/statecheck` (the
`svnd statecheck` subcommand of the `svn_statecheck` build, section 11) runs on a stopped node home or a copy: it reports
one SHA-256 per store and one over the I13 rows an upgrade leaves unchanged (every authority row except `{0x04}`, and every
x/policy row except the runner's own rows and the applied-upgrade rows, which the record step extends), and it runs
policytest, the lease and evidence checkers, the state parts of I5 and I6, and an authority checker for the state-only
forms of I1 and I7 to I10, I8's retired keys, and the I13 exact-key rule (`TestStateCheckDigestsAndInvariants`). It is a
test tool, not part of chain execution.

## 21. Owner rulings and the approval of this file

The owner answered Q1, Q2, and Q3 on 2026-11-07 (about 04:07Z) through the session's question tool; the exchange is to be
recorded in the goal-10 evidence file. The numbers inside the selected options, 100 and 1,000,000 blocks, remain PROPOSED constants: the
owner chose the option text that contained them, not the numbers as policy. No ruling is recorded in
`docs/decision-register.md`.

### 21.1 Q1 (DR-10-01): protocol additions and "removed council seats"

Selected: "Minimal additive (Recommended)". Built as section 5 and `docs/protocol.md:340-379`, without the auditor kinds.
Not chosen: "Minimal + auditor admin" (council admit and disable of `AUDITOR` accounts; DR-09-08 stays DEFERRED), "No
protocol change", and "Additive, v2 everywhere".

### 21.2 Q2 (DR-10-02): upgrade trigger, wrong-binary stop, operator actions, proposals at an upgrade

Selected: "Council plan, fail closed (Recommended)". Its text: "A council payload stores a plan that names only a block
height ... A binary without the upgrade stops at that height before writing anything, and every block checks the app
version, so a wrong binary exits instead of diverging. Only the council can cancel, and only before the height. There is no
skip height and no node-local halt. After a failure an operator may: restart with a corrected binary; restore a lone diverged
node from its own copy taken at the halt; or wait for a fix-forward upgrade. Never rollback --hard, unsafe-reset-all, a
signing-state reset or a new genesis. Approved but unexecuted proposals become stale at the upgrade." Built as sections 12
to 16. Not chosen: "Operator-coordinated" and "Council plan, approvals survive".

### 21.3 Q3 (DR-10-03): approvals and notice for upgrades and seat replacements

Selected: "3 of 4 + min notice (Recommended)": the same three-of-four approvals and review delay as every change; a plan
height at least 100 and at most 1,000,000 blocks after the executing block (both PROPOSED); a plan name never reused with a
different release digest. Its own text notes it is "Not fail-closed against three colluding seats: human review of the
binary and each operator's install choice stay the real control." Built as sections 7, 8, and 12. Not chosen: "Unanimity
for upgrades" and "3 of 4, no minimum".

### 21.4 DR-10-00: approval of this specification

The goal-10 prerequisite "migration and governance specifications approved"
(`docs/goals/10-policy-governance-and-chain-upgrades.md:8`) cannot be met by an existing document: none existed before goal
10. Goal 09 recorded its own unmet specification prerequisite the same way (DR-09-26, `docs/evidence/goal-09.md:23`).

Options: (a) the owner approves this file at a pinned commit and file sha256; (b) the owner rejects it or asks for changes;
(c) the owner names another specification.

Interim: this file is PROPOSED. Approval is asked at PR time. Until then the prerequisite stays BLOCKED, and goal 10 is
reported BLOCKED for it. An approval would meet the prerequisite for goal 10 only. Label: fail-closed (the goal status does
not advance without it).

## 22. Orchestrator choices and interim rules

### 22.1 Orchestrator choices (reversible implementation choices, not owner decisions)

| Id | Choice | Where |
| --- | --- | --- |
| ORCH-1 | G-11 names the first eight parse divergences; `payload.go` codes unchanged; open request DR-10-44 | section 19 |
| ORCH-2 | G-6 and G-10 accepted with the scopes as implemented | section 19 |
| ORCH-3 | the plan's interim choices stand unless an owner ruling replaced them | this section |
| ORCH-4 | goal 02's `check_protocol.py` changed only where Q1 required: the descriptor-name and oneof member pins | section 5 |
| ORCH-5 | the two PROPOSED lead constants are defined once, in `chain/msgkit`, which x/authority and x/policy both import | section 12 |
| ORCH-6 | the rehearsal binaries and the state check are tagged builds of the one svnd main (`svn_rehearsal`, `svn_rehearsal_bad`, `svn_statecheck`); every tagged build refuses `start` on a non-dev chain id; the release build uses no tag | section 11 |
| ORCH-7 | upgrade handlers get a by-name store accessor, passed in the handler's context; the I13 post-conditions are unchanged | section 14.3 |
| ORCH-8 | G-11 widened by five conditions (9-13); further unnamed divergences are reported, not hidden | section 19 |
| ORCH-9 | genesis policy `app_version` is 0; the app version comes from the binary through `InitVersions` | section 10.4 |
| ORCH-10 | the strict plan-name rule with the `0x08` row: an applied name `DUPLICATE_ID`; an earlier plan of the name with another digest `CONFLICT` | section 12 |
| ORCH-11 | a process rule for the build environment (Go build-cache pruning below 1.5 GB free disk); it does not affect this design | none |

### 22.2 Interim rules for open requests

Each request below has an interim rule that goal 10 implements, tests, and reports as open. None is an owner decision.

| Request | Interim | Label |
| --- | --- | --- |
| DR-10-04 exports, restart, replay | in-place continuity only; exports of state that holds governance records refused with a named prefix (DR-06-01); goal-09 homes re-initialized. A migration from goal-09 homes is not offered: without an on-chain plan it would be an activation no council decision governs | fail-closed |
| DR-10-08 auditor admission | genesis only, as in goal 09; DR-09-08 stays DEFERRED with its residual: "A compromised or dishonest auditor cannot be removed on chain in v0; its records stay." (`chain/x/evidence/README.md:611-612`) | fail-closed for admission; the residual stays open |
| DR-10-09 floors for governed values | no floor; every key tested at 1 and at its ceiling | not fail-closed for liveness |
| DR-10-10 single `AUTHORITY` executor | accepted for the devnet beta; its loss freezes execution; it chooses when a queued plan executes and may withhold a cancel | not fail-closed for liveness |
| DR-10-11 permanent loss of more than one third of power | no on-chain path; out of scope. A new network would be a new chain id with this chain's history kept and published, needing its own owner decision; never a reset of this chain | fail-closed; the chain stays halted |
| DR-10-12 reading of the DEC-008 blocker rows REQ-TX-006 and REQ-GOV-003 | "VERIFIED for the approved shape on the devnet; BLOCKED for live values", as goal 06 read its DEC-008 rows (`docs/evidence/goal-06.md:7-14`) | a reporting rule |
| DR-10-13 key separation | seat keys and validator consensus keys refused in both directions (D-7) | fail-closed |
| DR-10-15 maximum lead | `MaxActivationLeadBlocks` = 1,000,000 (PROPOSED) also for governed-limit activations; compile-time quotas stay compile-time | not fail-closed (a new number) |
| DR-10-16 business-id squatting | seats retry with a fresh id; a denial of service, never a wrong transition | not fail-closed against denial of service |
| DR-10-17 no cancel for pending validator changes | keep DR-06-04 | not fail-closed |
| DR-10-18 the plan's release digest | informational only; the chain cannot check a node's binary | not fail-closed |
| DR-10-39 maximum planned halt | no number; the runbook's renewal step covers the effect | not fail-closed |
| DR-10-41 upgrade window | documented and tested (section 14.5); refusing `CheckTx` and queries in the window stays a reversible option | not fail-closed |
| DR-10-42 seat id length | 59 characters at genesis (PROPOSED, D-10) | fail-closed |
| DR-10-43 prior release on a goal-10 home | log its outcome in the rehearsal; if it starts, REQ-UPG-001 ("An old binary is not assumed to read new state.", `docs/requirements.md:493`) is not claimed for it | not fail-closed if it starts |
| DR-10-44 G-11 and the three open parse classes | named by ORCH-1 and ORCH-8; codes unchanged | not fail-closed (both sides reject; no state change) |

Superseded: DR-10-05 merged into Q2; DR-10-14 replaced by Q3; DR-10-38 (approved proposals stale at an upgrade) is now the
owner's Q2 ruling, not an interim. DR-10-19 (threat notes for governance capture, a malicious upgrade binary, the single
executor, a minority skip, and prior-release divergence go into the goal-10 evidence file; adding them to
`docs/threat-model.md` is asked, not done) is a documentation question. DR-10-40 records protocol additions that are not
implemented: `authority_result` and the four untyped goal-10 events in the event list; a cancel for pending validator
changes; an `AUTHORITY` replacement payload; business ids bound to the signer; genesis fields for lossless export; typed
upgrade events; a numeric admission-limit key; a quorum-loss behaviour (DEC-008).

## 23. Residual risks

- An upgrade binary can change any rule, cap, quota, seat, or decision row. Its self-checks run inside it and protect only
  against an honest binary's mistakes. The controls are human review of source and digest and each operator's refusal to
  install. Neither is a test. On the devnet one operator holds every seat and every node.
- The handler environment lets a handler write any store that is not protected (every store other than x/authority and
  x/policy, and the svn store except the consensus params); B2-bad uses that to diverge. The I13 and own-row checks catch
  only mistakes in protected rows.
- The tagged builds' chain id restriction reads the genesis file and the genesis document CometBFT stored, as CometBFT
  does; it is defence in depth for disposable devnets, not an access control. The state check reads a stopped home or a
  copy and holds each store's whole snapshot in memory; it is a devnet tool, not a tool for large stores.
- Governance capture: three colluding or compromised seats can schedule any upgrade.
- A plan whose binary does not exist halts the chain at h. The council cannot repair a halted chain ("A chain stall can
  prevent on-chain governance from executing.", `docs/decision-register.md:247`). Only a binary for the plan resumes it.
- The single `AUTHORITY` executor: its loss freezes execution; it chooses when a queued plan executes inside the lead window
  and may withhold a cancel.
- The prior release (goal 09) has no app-version rule and checks only four registry layouts. An operator who runs it on a
  goal-10 home may get a diverging node.
- A node commits one wrong height before CometBFT detects a wrong migration (I6). Copy-at-halt is the only local recovery
  without a rollback tool.
- Upgrade window: a node swapped before block h was decided serves `CheckTx` and queries with new code on old state until h
  commits; after any start, `CheckTx` rejects normal accounts until the first commit.
- A planned halt makes everything that fell due during it due at h+2, at most 128 items per block. Leases whose deadline
  falls in the halt cannot be renewed while halted. No maximum planned halt is set.
- A new node joining after an upgrade must start from the genesis binary and switch at each plan height.
- Migration work runs once in PreBlock with no per-block cap. A large state could make block h slow; it is measured only on
  the rehearsal state, in-process.
- After an upgrade that changes a layout, historical queries and state checks below h through the new binary can misread
  state.
- The rehearsal walk of proposal records checks only their shape (section 18).
- Raw `/store` queries are unmetered; CometBFT RPC must not be public.
- The `EXECUTED` or `REJECTED` to `EXPIRED` relabel stays (spec behaviour).
- Business-id squatting is a denial of service, never a wrong transition.
- The release digest in a plan is information only. The chain cannot tell which binary a node runs.
- A binary built from a `git worktree` checkout printed `vcs.revision unknown` in this workspace; release candidates are
  built from a clone (`docs/operations/release-preparation.md`).

## 24. Steps that need a human approval or an independent security review

Goal 10 lists each step and performs none.

1. Approval of this file at a pinned commit and file sha256 (DR-10-00), asked at PR time. Q1, Q2, and Q3 are answered; the
   lead constants stay PROPOSED.
2. A recorded decision for any further protocol revision.
3. DEC-008 live values: seat holders, delay, maximum expiry, quorum-loss behaviour; any genesis naming real council keys;
   the PROPOSED lead bounds (Q3, DR-10-15).
4. Real operators, validators, and auditors (DEC-002).
5. Generation, custody, and rotation of live keys: seat, executor, validator, release signing (DEC-001).
6. Approving any blocked decision parameter. The chain refuses it; the decision stays human.
7. Tagging, publishing, or signing a release, and naming the publisher (goal 27).
8. Human review of release source and digests before any operator installs a binary, including the diff of `chain/upgrade`,
   the handler, and the self-check list against the previous release. An upgrade binary can change any rule (section 9).
9. Each operator's decision to install an upgrade. On the devnet one operator decides for all four nodes.
10. An independent operator installing from the runbooks (`docs/progress.md:104-110`; goal 30).
11. Independent security review of the authority changes, the upgrade gate, the per-block rule and runner, the handler
    environment, every migration handler, and the release digest procedure (REQ-SEC-011, `docs/requirements.md:552`; goal
    29). Model-assisted review is not that review.
12. Adding the governance-capture, malicious-upgrade, executor, and prior-release threats to `docs/threat-model.md`
    (DR-10-19).
13. Merging goal 09 (PR 12) and goal 10, and any branch-protection change for new CI checks.
14. Any council-scheduled upgrade on a chain other than a disposable devnet.
15. Recovery from permanent quorum loss (out of scope; a new network needs its own decision).
16. A maximum planned halt and any floors for governed values (DR-10-39, DR-10-09).
17. A decision on G-11 and the three open parse divergence classes (DR-10-44).
