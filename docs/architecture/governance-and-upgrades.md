# Governance and upgrades

Status: PROPOSED. This is the goal-10 specification draft for policy governance and chain upgrades. It is not approved.
It is not an audit. It applies to the devnet beta only. Public launch is disabled. There is no token, fee denomination,
reward, staking module, or penalty.

Approval. The goal-10 prerequisite "migration and governance specifications approved"
(`docs/goals/10-policy-governance-and-chain-upgrades.md:8`) is recorded BLOCKED as DR-10-00. The owner will be asked to
approve or reject this file at a pinned commit and file sha256 when the goal-10 pull request is ready (section 21.4).
Until then no sentence in this file is an owner decision. `docs/decision-register.md` holds no DR-10 entry at `ff951fc`.

How to read this file.

- Line citations (`path:line`) refer to commit `ff951fc`, the goal-09 head that goal 10 starts from. Goal-10 changes move
  some of them; the final text is checked against the tested tree.
- Framework citations name module-cache copies of the pinned versions (`chain/go.mod:12,15,16`):
  `cometbft@v0.40.0/...` is `$(go env GOMODCACHE)/github.com/cometbft/cometbft@v0.40.0/...`, `cosmos-sdk@v0.55.0/...` and
  `store/v2@v2.0.0/...` likewise.
- "spec :N" means `docs/evidence/goal-02/spec_machine.py` line N, the executable reference semantics.
- Every number is either cited or labelled PROPOSED. A PROPOSED number is a proposal with a decision request, not policy.
- `[Educated Guess]` marks an inference that a planned test has not yet confirmed.
- "Fail-closed" on an interim rule means: while the question is open, the rule refuses the uncertain action instead of
  allowing it. "Not fail-closed" means the interim rule allows something the owner may later refuse.
- Each design section says which open question it depends on. Q1, Q2, and Q3 are open (section 21). Sections 5, 8, and 12
  describe the design under the recommended answers; section 21 says what changes under the others.

## Contents

1. Scope and what this file does not claim
2. Current state at `ff951fc`
3. Council flow, exact order
4. Typed governed changes
5. Proposed payload additions (depends on Q1)
6. Policy versions
7. Unique approvals, threshold, expiry, review delay
8. Council seat replacement (depends on Q1)
9. Safety bounds and what does and does not protect them
10. Storage, queries, events, genesis, export
11. Upgrade version model
12. Upgrade plans: who schedules, when, and notice (depends on Q1, Q2, Q3)
13. Load-time gate
14. Per-block rule and the upgrade runner
15. Failure behaviour without resetting history
16. Operator actions: allowed, forbidden, and when rollback is unsafe
17. Separation of local emergency controls, protocol changes, and publication authority
18. Rehearsal heights
19. Deviations from `spec_machine.py`
20. Invariants
21. Open owner questions (Q1, Q2, Q3, DR-10-00)
22. Security-relevant requests with interim rules
23. Residual risks
24. Steps that need a human approval or an independent security review

## 1. Scope and what this file does not claim

Goal 10 asks for typed parameter changes, admission limits, policy versions, scheduled activation, a chain-upgrade
mechanism built from supported framework components, a rehearsal harness, runbooks, and a separation of local emergency
controls, protocol changes, and publication authority (`docs/goals/10-policy-governance-and-chain-upgrades.md:18-31`).

This file specifies the design. It does not report what is implemented, tested, or verified; `docs/evidence/goal-10.md`
does that once the tests run. It does not claim:

- live council parameters: seat holders, the live review delay, a maximum expiry, quorum-loss behaviour (DEC-008,
  `docs/decision-register.md:247,263`);
- an upgrade of any chain other than a disposable devnet;
- that any release is reviewed, signed, published, or audited;
- independent operators: every run in this repository is one operator on one machine (`docs/progress.md:104-110`);
- indexer schema compatibility (the indexer is goal 12);
- restart from a lossless export of state that holds governance records (DR-06-01; section 22.2).

## 2. Current state at `ff951fc`

Facts read on the tree. Goal 10 starts from them.

- The council flow follows the spec order: `ProposeChange` (`chain/x/authority/engine.go:297-387`), `ApproveChange`
  (`:392-443`), `ExecuteChange` (`:447-511`), payload application (`:518-552`).
- The council shape is compile-time: `ApprovedSeats = 4`, `ApprovedThreshold = 3` (`chain/x/authority/codes.go:40-41`),
  from DEC-008 "Four named seats, three approvals for an ordinary change, one approval per seat, an expiry, and a published
  delay" (`docs/decision-register.md:247`). `council()` requires the labelled DEC-008 devnet row with numeric value 3, a
  delay inside the unix bound, and exactly four seats, else `ERROR_CODE_DECISION_BLOCKED` (`engine.go:163-172`).
- Off a `shortvideo-dev-` chain id the devnet row is never found (`engine.go:143-153`), so every council message is
  `ERROR_CODE_DECISION_BLOCKED` there.
- A proposal carries one of five payload kinds (`docs/protocol/proto/svn/authority/v1/tx.proto:64-72`). Only a validator
  power change has an activation point (`tx.proto:25-43`). A governed limit is written inside the execute
  (`engine.go:526-532`).
- At propose a governed limit is checked only for the id pattern and value >= 1 (`chain/x/authority/payload.go:235-249`).
  The key set and ceiling are checked at execute, where a failure is `ERROR_CODE_BOUNDS` and the proposal stays `QUEUED`
  (`engine.go:526-530`). The ceilings exist twice: `governedCeiling` (`engine.go:608-623`) and `msgkit`
  (`chain/msgkit/limits.go:20-26`).
- The policy version is one counter at authority key `0x04`, absent means 1 (`chain/x/authority/store.go:45-46`). Every
  successful execute of any kind adds 1 (`engine.go:507`). x/leases stamps it on each new lease
  (`chain/x/leases/keeper.go:110-113`; `chain/x/leases/msg_server.go:495`).
- Approvals are deduplicated by seat id (`engine.go:420-422`). The approver key is recorded (`engine.go:430-431`); no check
  reads it.
- Seat key rotation exists only as engine code with no message: `RotateCouncilSeat` (`engine.go:840`) keeps the seat id,
  retires the old key, and does not create an auth account for the new address
  (`docs/architecture/authority-authorization.md:17`). Goal 06 recorded: "Any future message path for `RotateCouncilSeat`
  must create the auth account for the new seat address." (`docs/evidence/goal-06.md:503`).
- A seat-signed transaction may carry only `MsgProposeChange` or `MsgApproveChange` (`chain/app/ante.go:161-180`). This is
  the owner's DR-06-06 ruling (`docs/architecture/authority-authorization.md:93`). Execute requires the
  `SUBMITTER_ROLE_AUTHORITY` role (`engine.go:451-456`); the devnet has one such account (`chain/devnet/init.go:129`).
- Registry payloads run in a nested cache that only a success writes back (`chain/x/authority/msg.go:64-117`).
- Authority emits only `authority_result` events (`msg.go:103,111,114`). `PolicyChanged` and `ProposalExecuted` are in the
  protocol event list (`docs/protocol.md:296`) and are never emitted (`docs/evidence/goal-06.md:187`).
- x/policy is a module slot: "No keeper is in this directory." (`chain/x/policy/README.md:3`). The app runs
  `scaffold.New(ModulePolicy, ...)` (`chain/app/app.go:244`); `checkPolicy` checks only the safety caps
  (`chain/app/app.go:570-592`).
- No upgrade machinery exists. PreBlockers are auth only (`chain/app/app.go:269`). `checkRegistryLayouts` refuses a
  providers, content, leases, or evidence store whose layout key differs from the binary's `ConsensusVersion`
  (`chain/app/app.go:328-354`).
- Protocol v0: "Unknown fields are rejected in v0. A new field is an incompatible change. It needs a recorded decision and
  a new schema version." (`docs/protocol.md:26`). `PolicyState` has only `policy_version`, `safety_caps`, and
  `blocked_decisions` (`docs/protocol/proto/svn/policy/v1/state.proto:27-31`). No upgrade message, payload, plan, height,
  binary digest, or app-version field exists in the protocol.
- Policy has no Msg service: "There is no direct MsgUpdateLimits. Limit changes are proposal payloads executed by
  svn.authority.v1.MsgExecuteChange." (`docs/protocol/proto/svn/policy/v1/tx.proto:12-14`).
- The devnet DEC-008 row has numeric value 3 and delay 0 (`chain/devnet/init.go:541`). Consensus `Version.App` is set from
  `genesis.ProtocolVersion` (`chain/devnet/init.go:564`), which is 1 (`chain/genesis/bounds.go:11`).
- `TestLinkedBinaryOmitsEconomicModules` fails on any `x/staking`, `x/bank`, `x/mint`, `x/distribution`, `x/slashing`,
  `x/gov`, `x/authz`, ibc-go, or evm symbol in svnd (`chain/app/power_test.go:106-125`).

## 3. Council flow, exact order

Before every handler the spec runs `guard_decisions` (spec :112): any approved decision row, a non-empty approved council,
or an economic flag makes every transaction `ERROR_CODE_FORBIDDEN`. A failed step keeps no state. A keep outcome
(`EXPIRED` or `REJECTED` mark) commits only the mark (`msg.go:64-117`). Goal 10 keeps this order for every payload kind, old
and new. Step 7a is the only new step (D-1, section 19).

### 3.1 Propose (spec :1403-1449; chain `engine.go:297-387`)

| # | Check | Code | Chain |
| --- | --- | --- | --- |
| 1 | unknown field | spec `UNKNOWN_FIELD`; chain: the SDK decode error | strict transaction decoder (`chain/app/encoding.go:115-133`) |
| 2 | missing field | `MALFORMED` | `:300-303` |
| 3 | council ready (DEC-008 devnet row, four seats) | `DECISION_BLOCKED` | `:304-306` |
| 4 | signer is not a seat | `UNAUTHORIZED` | `:307-310` |
| 5 | proposal id, then parameter key, off the id pattern | `BOUNDS` | `:311-313` |
| 6 | parameter key starts `dec-` | `FORBIDDEN` | `:314-316` |
| 7 | payload: parse, canonical form, digest, digest algorithm, parameter-key binding, validator activation strictly future | `MALFORMED`, `BOUNDS`, `INTEGRITY`, `OUT_OF_RANGE` | `:318-330` |
| 7a | (goal 10, D-1) governed limit key not in `governed_limit_caps`, or value above its ceiling; under Q1 (a) also an activation height out of range | `BOUNDS`, `OUT_OF_RANGE` | new, right after 7 |
| 8 | expiry outside the unix bound | `BOUNDS` | `:331-333` |
| 9 | business id: same digest is idempotent, other digest is a replay | idempotent, `REPLAY` | `:334-343` |
| 10 | proposal id already stored | `DUPLICATE_ID` | `:344-346` |
| 11 | expiry <= chain time | `EXPIRED` | `:347-349` |
| 12 | base policy version != current | `STALE_VERSION` | `:350-356` |
| 13 | (Go only, DR-06-03) the seat holds 32 open proposals on the current version | `BOUNDS` | `:357-366` |
| 14 | write the record `OPEN` | success | `:367-386` |

The 32-proposal quota is PROPOSED (DR-06-03, `chain/x/authority/codes.go:62`).

### 3.2 Approve (spec :1452-1482; chain `engine.go:392-443`)

Missing field `MALFORMED` (`:393-395`); council ready (`:396-398`); signer not a seat `UNAUTHORIZED` (`:399-402`); business
id gate (`:403-409`); record absent `NOT_FOUND` (`:410-413`); state not `OPEN` `CONFLICT` (`:414-416`), so a `QUEUED`
proposal past expiry is `CONFLICT` and is not marked; chain time >= expiry `EXPIRED`, kept (`:417-419`); seat already
approved `REPLAY` (`:420-422`); (Go only) `OVERFLOW` when `now + delay` would leave the unix bound (`:423-427`); append the
seat id (`:428-431`); at three approvals the state becomes `QUEUED` with `execute_after = now + DEC-008 delay`
(`:432-435`).

### 3.3 Execute (spec :1485-1527; chain `engine.go:447-511`)

| # | Check | Code | Chain |
| --- | --- | --- | --- |
| 1 | missing field | `MALFORMED` | `:448-450` |
| 2 | signer role is not `AUTHORITY` (no role `UNAUTHORIZED`, other role `FORBIDDEN`), before council ready | `UNAUTHORIZED`, `FORBIDDEN` | `:451-456` |
| 3 | council ready | `DECISION_BLOCKED` | `:457-459` |
| 4 | business id gate | idempotent, `REPLAY` | `:460-463` |
| 5 | record absent | `NOT_FOUND` | `:464-467` |
| 6 | chain time >= expiry, in any state | `EXPIRED`, kept | `:469-471` |
| 7 | state not `QUEUED` (insufficient approval) | `CONFLICT` | `:472-474` |
| 8 | chain time < `execute_after` (review delay) | `OUT_OF_RANGE` | `:475-477` |
| 9 | base != current version: `CONFLICT` if the key last executed another digest, else `STALE_VERSION` | `CONFLICT`, `STALE_VERSION` | `:478-488` |
| 10 | apply the payload: `FORBIDDEN` marks `REJECTED` and is kept; every other code fails and the proposal stays `QUEUED` | per kind | `:489-495` |
| 11 | (Go only) version at the uint64 maximum | `OVERFLOW` | `:496-498` |
| 12 | write: executed digest for the key, policy version + 1, `EXECUTED`, business id | success | `:499-510` |

Goal 10 adds the typed events `PolicyChanged` (the spec event, spec :1524-1527) and `ProposalExecuted` after step 12, on the
success branch only (D-2).

An `EXECUTED` or `REJECTED` proposal executed again after its expiry is relabelled `EXPIRED` with the policy version
unchanged (spec :1502-1504; `engine.go:277-293`). Goal 10 keeps this spec behaviour (`docs/evidence/goal-06.md:497`).

## 4. Typed governed changes

Goal 10 needs no protocol change for this section.

- `GovernedLimitChange` is already typed: a limit key and a uint64 value (`tx.proto:45-48`).
- Goal 10 moves the key-set and range check to propose step 7a (D-1). A proposal that can never execute then does not hold
  a quota slot or a seat's time.
- The ceilings come from one table, the `msgkit` ceilings (`chain/msgkit/limits.go:20-26`). The duplicate
  `governedCeiling` (`engine.go:608-623`) is removed.
- The execute keeps its own check. Ceilings are compile-time constants, so after D-1 no message reaches the execute-side
  check; a test writes an over-ceiling `QUEUED` record through a test hook and expects `ERROR_CODE_BOUNDS` with the
  proposal still `QUEUED`.
- A limit key that names a safety cap (`max_tx_bytes`, `max_gas_per_tx`) or a compile-time quota is not in
  `governed_limit_caps`, so it is `ERROR_CODE_BOUNDS` at propose and at execute.
- Values inside 1..ceiling stay accepted, including unsafe ones such as `max_expiry_steps = 1`; the contract allows
  1..ceiling (spec :1338-1343). Floors are open (DR-10-09, section 22.5).

Admission limits (`docs/goals/10-policy-governance-and-chain-upgrades.md:18`). No governed key limits admissions, and a new
key would be a `bounds.json` change. Interim reading [Educated Guess]: admission is council-only (DEC-009,
`docs/decision-register.md:281`) through the admit and disable payloads, bounded by the existing `max_open_offers` limit
and the compile-time quotas. A numeric admission-limit key is a recorded protocol addition (DR-10-40, section 22.20).

## 5. Proposed payload additions (depends on Q1)

This section applies only under Q1 (a), (b), or (d) (section 21.1). Under Q1 (c) none of it exists.

The revision is additive. Every new field is optional, so every v0 payload keeps its canonical form and digest (absent
fields are omitted from SVN-JCS-1). The two v0 payload vectors in `docs/protocol/fixtures/vectors.json` stay byte-equal.
No new error code is added. Under Q1 (a) the document schema version (`docs/protocol.md:15`;
`docs/protocol/bounds.json:3`) moves to 2. The protocol name, the peer media type, the genesis `protocol_version` (1), and
the message-level `schema_version` of evidence batches, capability manifests, and decision rows stay 1, because those
schemas do not change.

```proto
// svn.authority.v1 (tx.proto)
message GovernedLimitChange { string limit_key = 1; uint64 value = 2; uint64 activation_height = 3; } // 3: 0 = at execute
message SoftwareUpgradeChange { string name = 1; uint64 height = 2; svn.types.v1.Digest release_digest = 3; }
message CancelUpgradeChange { string name = 1; }
message CouncilSeatChange { string seat_id = 1; bytes new_public_key = 2; }
// ProposalPayload oneof: software_upgrade = 6; cancel_upgrade = 7; council_seat = 8;
// only under Q1 (b): admit_auditor = 9; disable_auditor = 10;

// svn.policy.v1 (state.proto)
message GovernedLimitValue { string limit_key = 1; uint64 value = 2; }
message PendingLimitChange { string limit_key = 1; uint64 value = 2; uint64 activation_height = 3; string proposal_id = 4;
                             uint64 policy_version = 5; }   // 5: the policy version the execute produced
message UpgradePlan { string name = 1; uint64 height = 2; svn.types.v1.Digest release_digest = 3; string proposal_id = 4; }
// PolicyState adds: governed_limits = 4; pending_limit_changes = 5; upgrade_plan = 6; app_version = 7.
// QueryGovernanceResponse adds the labelled devnet test parameters, so the review delay is published.
```

| Kind | Parameter key | Propose checks after step 7 | Execute checks (a non-`FORBIDDEN` failure leaves `QUEUED`) | Effect |
| --- | --- | --- | --- | --- |
| governed limit | `<limit_key>` | key in the five caps, value 1..ceiling (`BOUNDS`); activation 0, or `height < activation <= height + MaxActivationLeadBlocks` (`OUT_OF_RANGE` at or below the height; `BOUNDS` above the lead or above 2^53-1) | the same range checks against the execute block | activation 0: value written at once and any pending row of the key deleted in the same write; activation a: a pending row written, replacing any pending row of the key. The latest executed decision for a key wins (I3) |
| validator power (v0) | unchanged | unchanged | v0 projection checks, plus a consensus key equal to a current or retired seat key is `DUPLICATE_ID` (D-7) | unchanged |
| software upgrade | `upgrade` | name on the id pattern (`BOUNDS`); height in `[height + MinActivationLeadBlocks, height + MaxActivationLeadBlocks]` and <= 2^53-1 (`OUT_OF_RANGE` below, `BOUNDS` above); digest algorithm SHA-256 (`MALFORMED`), 32 bytes (`BOUNDS`) | height still in that range for the execute block; name already applied `DUPLICATE_ID`; a pending plan with the same name and another digest `CONFLICT` | plan written, replacing a pending plan of another name (DR-10-32) |
| cancel upgrade | `upgrade` | name on the id pattern (`BOUNDS`) | no pending plan with that name `NOT_FOUND` | plan deleted |
| council seat | `seat-<seat_id>` | seat id on the id pattern, key 32 bytes (`BOUNDS`) | seat absent `NOT_FOUND`; key equals another seat's key `DUPLICATE_ID`; key retired or equal to the seat's own key `CONFLICT`; key's address holds a submitter role `DUPLICATE_ID`; key equals a current or pending validator consensus key `DUPLICATE_ID` (D-7) | section 8 |

Numbers in the table: `MaxActivationLeadBlocks` = 1,000,000 blocks (PROPOSED, DR-10-15); `MinActivationLeadBlocks` = 100
blocks (PROPOSED, part of Q3); 2^53-1 is `SafeIntegerMax` (`chain/genesis/bounds.go:23`); the five caps are the `msgkit`
ceilings (`chain/msgkit/limits.go:20-26`).

The new parameter keys cannot collide with v0 keys: provider keys start `admit-` or `disable-`, validator keys
`validator-`, withdraw keys `withdraw-`, and limit keys are the cap names (`chain/x/authority/payload.go:288-301`). The spec
machine treats every new payload alternative as `MALFORMED` at parse; that is deviation D-5.

Auditor admission and disablement exist only under Q1 (b). They follow the goal-09 rulings: "Account key signs
(Recommended)" with "no rotation", and "Envelopes + id prefix (Recommended)" with the 8-byte address-prefix rule
(`docs/evidence/goal-09.md:74-75`; DR-09-30). Admit and disable only; no auditor key rotation under any option.

### 5.1 Scheduled activation of a governed limit (Q1 (a))

- Execute in block h with `activation_height = a > h` stores a pending row in x/policy: activation height, value, proposal
  id, and the policy version the execute produced. It replaces any pending row of that key.
- Execute with activation 0 writes the value and deletes any pending row of that key in the same write. So an older
  scheduled decision cannot overwrite a newer immediate one at its activation height.
- x/policy BeginBlock at height b applies every pending row with `a <= b` in key order and deletes it. x/policy runs before
  x/leases in the module order (`chain/app/app.go:51-62`), so a value activated at a governs that block's lease expiry
  (`chain/x/leases/expiry.go:63`) and every transaction of block a. Block a-1 sees the old value; block a the new one.
- Height only. A time activation would need a total order of heights and times, which DEC-012 did not select
  (`docs/decision-register.md:365`).
- The policy version moves at execute, not at activation. During a pending window the policy version names the executed
  decisions, not the limits in force. A lease created at a-1 records v+1 while v's limit still governs it. The limits in
  force are the governed values plus the pending rows with their heights.
- At most five pending rows exist, one per governed key.

## 6. Policy versions

- One counter, authority `0x04` (`chain/x/authority/store.go:45-46`). It moves by 1 per executed proposal of any kind
  (`engine.go:507`). The base check serializes execution: the first execute wins and every other in-flight proposal
  becomes `STALE_VERSION` or `CONFLICT` (`engine.go:478-488`) and stops holding a quota slot (`codes.go:44-61`). Goal 10
  keeps the counter in authority (DR-10-20, reversible): it is the council's concurrency counter and x/leases already
  reads it there.
- Interim, fail-closed (DR-10-38; section 22.15): an applied upgrade also adds 1, through one authority method that only
  the upgrade runner calls. Every proposal approved but not executed before the upgrade is then on an older base and can
  never execute under new code. Changes already executed (pending validator changes, pending governed changes) still apply
  at their heights. Q2 (c) is the alternative.
- Exposed as `PolicyState.policy_version` by `svn.policy.v1.Query/Policy` and in the typed events `PolicyChanged` and
  `ProposalExecuted`. The genesis copy in the policy slot must be 1, and authority must hold no `0x04` key at genesis.
- Because of the `EXPIRED` relabel (section 3.3) and DR-10-38, a check that reads only state can assert
  `version >= 1 + number of EXECUTED records`. Exact equality (`1 + successful executes + applied upgrades`) holds only in
  harnesses that count operations (invariant I1).

## 7. Unique approvals, threshold, expiry, review delay

The rules of sections 3.2 and 3.3 apply to every payload kind, old and new, except any per-kind threshold Q3 sets.

- Unique approvals: one per seat id (`engine.go:420-422`). The same seat again is `ERROR_CODE_REPLAY`, also after its key
  is replaced.
- Threshold: three of four (`codes.go:40-41`; DEC-008). Fewer than three leaves the proposal `OPEN`; execute is then
  `ERROR_CODE_CONFLICT` (`engine.go:472-474`).
- Expiry: chosen by the proposer, at most `unix_seconds_max` (253402300799, `chain/genesis/bounds.go:19`). A maximum
  expiry is a DEC-008 number and stays unset.
- Review delay: the DEC-008 devnet row (`chain/devnet/init.go:541`, delay 0). Goal 10 adds a devnet option for a nonzero
  labelled delay (DR-10-29, reversible) so a timing test can show `execute_after - 1` as `ERROR_CODE_OUT_OF_RANGE` and
  `execute_after` as success. The value used is a `PROPOSED_TEST_PARAMETER`, not policy.
- Removed seats: a seat replacement by payload (section 8) bumps the policy version when it executes, so every proposal
  the old key approved is on an older base and can never execute. A recount of approvals by current seat key at execute
  is not added: approver ids and approver keys are sorted independently (`engine.go:428-431`), so the record does not pair
  a key with its seat, and the version bump already covers every message path.

## 8. Council seat replacement (depends on Q1)

Only under Q1 (a), (b), or (d). "Removed council seats" (`docs/goals/10-policy-governance-and-chain-upgrades.md:34`) is
read as replacement of a seat's key. The council stays at four seats (`chain/x/authority/genesis.go:96-97`;
`engine.go:168`). True removal with a recomputed threshold is not offered: DEC-008 approved four seats.

- The payload needs the approvals Q3 sets.
- Its execute replaces the seat key and retires the old key at `0x15`, as `RotateCouncilSeat` does
  (`chain/x/authority/store.go:61-62`). A retired key can never return as a seat key.
- The ante stops admitting the old key's account because seat admission reads the config
  (`chain/x/authority/store.go:112-127`).
- The new address gets an auth account in the same nested cache, so its first propose passes the ante (DR-10-35; the goal-06
  follow-up quoted in section 2).
- The policy version bump makes every in-flight proposal unexecutable, including those the old key approved.
- Key separation in both directions (D-7, DR-10-13): a seat key may not equal a current or pending validator consensus
  key, and a validator payload may not add a consensus key equal to a current or retired seat key.
- Seat ids are capped at 59 characters at genesis (PROPOSED, D-10, DR-10-42): the id pattern allows 64 characters
  (`chain/x/authority/payload.go:36`) and the parameter key `seat-<seat_id>` adds five.
- Only disposable devnet keys are used in tests. No live key is rotated.

## 9. Safety bounds and what does and does not protect them

Two different protections apply. They must not be confused.

1. No council payload can change the bounds in the table below. Each payload kind is checked against them, and tests show
   it.
2. An upgrade binary can change any rule, cap, quota, seat, or decision row. The upgrade runner and its self-checks are
   compiled into each binary, so a council-scheduled binary can drop them. The self-checks (section 14.2, invariant I13)
   only catch an honest binary's mistakes. The control against a wrong or malicious upgrade binary is human review of its
   source and digest before any operator installs it, including a diff of `chain/upgrade`, the handler, and the self-check
   list against the previous release, and each operator's own choice to install it (section 24 items 8, 9, 11). Neither
   control is a test. On the devnet one operator holds every seat and every node.

| Bound | Where enforced at `ff951fc` | Governed by a payload? | Goal-10 protection |
| --- | --- | --- | --- |
| transaction bytes 65,536; gas per transaction 1..20,000,000 | `chain/genesis/bounds.go:25-27`; ante (`chain/app/ante.go:90-108`) and `chain/x/authority/msg.go:127-142` | no; safety caps (`docs/protocol/proto/svn/policy/v1/state.proto:14-25`) | no payload field names them; the limit keys `max_tx_bytes` and `max_gas_per_tx` are `BOUNDS` at propose and execute; the runner's self-check refuses a migration that changes the safety caps or consensus `Block.MaxGas` and `Block.MaxBytes` |
| five governed limits | readers through `Limits` interfaces (`chain/app/app.go:176-235`) | yes, 1..ceiling | D-1 at propose; every key tested at 1 and at its ceiling with the chain live; a damaged committed value halts as today (`TestDamagedExpiryLimitHalts`, `chain/app/lease_expiry_test.go:320`) |
| quotas: 32 open proposals per seat, 128 pending validator changes (`codes.go:62,69`, both PROPOSED, DR-06-03, DR-06-04); lease and evidence quotas | compile-time | no | a key that names a quota is `BOUNDS`; changing a quota is a binary upgrade |
| lease bounds: DEC-005 term | devnet row at genesis only | no | no payload writes the config's devnet rows |
| resulting validator policy (DEC-011: at least four validators, `max_power * 3 < total_power`) | projection at every activation point (`engine.go:635-741`) | council validator payloads only | unchanged; the runner's self-check refuses a migration that changes any authority row |
| blocked decisions, approved council, quorum loss | genesis checks (`chain/genesis/document.go:235-258`) | no | no payload, old or new, changes a decision row; runner self-check |
| seats, submitters, admission rows, retired keys | authority `0x01`, `0x02`, `0x15`, `0x16` (`chain/x/authority/store.go:34-64`) | seat payload only (auditor payloads under Q1 (b)) | runner self-check compares every authority row |

Numbers in the table are cited where they are enforced. Changing a safety cap "is an incompatible protocol change and
needs a recorded decision" (`docs/protocol/proto/svn/policy/v1/state.proto:16`).

## 10. Storage, queries, events, genesis, export

### 10.1 Storage split (DR-10-20, reversible)

- x/policy stores the fixed safety caps, the governed values (moved from authority `0x13`), the pending parameter changes,
  the upgrade plan and the applied-upgrade history, the persisted module version map, and the app version. The
  requirement row says "Goal 10 policy module stores resource bounds and delayed parameter changes"
  (`docs/requirements.md:461`).
- x/authority keeps proposals, approvals, business ids, the executed set, validators, seats, submitters, pending validator
  changes, retired keys, and the policy version. It is the only writer of x/policy's governed rows, through a writer
  interface called from payload application on the nested cache (`chain/x/authority/msg.go:64-117`). A policy write never
  outlives a failed or kept proposal.
- No message writes the policy store directly. Only InitGenesis, the authority hook, the x/policy BeginBlock, and the
  upgrade runner write it (invariant I12).

Proposed x/policy keys (every value non-empty, because goleveldb reports an empty value as absent,
`chain/msgkit/layout.go:20-27`):

| Key | Value | Writers |
| --- | --- | --- |
| `0x00` | layout version, 8 bytes big-endian | InitGenesis |
| `0x01` | safety caps | InitGenesis only; never changed |
| `0x02 \| limit_key` | governed value, 8 bytes, in 1..ceiling | authority hook; BeginBlock |
| `0x03 \| limit_key` | pending change: activation height, value, policy version, proposal id | authority hook; deleted by BeginBlock or by an immediate change of the same key |
| `0x04` | app version, 8 bytes | InitChainer; upgrade runner |
| `0x05 \| module name` | consensus version, 8 bytes | InitChainer; upgrade runner |
| `0x06` | upgrade plan: name, height, 32-byte digest, proposal id | authority hook; upgrade runner deletes it |
| `0x07 \| name` | height at which the upgrade applied | upgrade runner |

Bounds: at most five `0x02` rows and five `0x03` rows (one per governed key), at most one `0x06` row, one `0x05` row per
module (eight stores are mounted, `chain/app/app.go:137-156`), and one `0x07` row per applied upgrade.

### 10.2 Queries

- `svn.policy.v1.Query/Policy`: policy version, safety caps, blocked decisions; under Q1 (a) also the effective governed
  values, pending changes, the plan, and the app version. Bounded by construction.
- `svn.authority.v1.Query/Governance`: authority state with paging (DR-10-23, reversible). Proposals carry approver keys
  only; the v0 proto has no approver seat ids. Under Q1 (a) the response carries the labelled devnet test parameters, so
  the review delay is published (REQ-GOV-003, `docs/requirements.md:563`).
- Raw `/store` queries stay unmetered, as in goals 07-09. CometBFT RPC must not be public.

### 10.3 Events (DR-10-22, reversible)

| Source | Event | Typed? |
| --- | --- | --- |
| successful execute, every kind | `svn.events.v1.PolicyChanged` (new policy version, parameter key) | yes; the spec event (spec :1524-1527) |
| same, right after | `svn.events.v1.ProposalExecuted` (proposal id, new policy version) | yes; D-2 |
| keep and failure outcomes | `authority_result` | no; unchanged (DR-06-05) |
| governed-limit execute that schedules or supersedes a change (Q1 (a)) | `policy_pending` | no |
| x/policy BeginBlock, per applied change (Q1 (a)) | `policy_activated` | no |
| plan written, replaced, or cancelled (Q1 (a)) | `upgrade_plan` | no |
| upgrade applied in PreBlock | `upgrade_applied` | no |

The four untyped goal-10 events are outside the protocol event list (`docs/protocol.md:296`), as `authority_result` is.
They are recorded as event-list additions (DR-10-40). Goal 12 must not consume them until that is decided.

### 10.4 Genesis and export

- x/policy genesis: policy version 1; safety caps equal to the genesis defaults (as `checkPolicy` checks today,
  `chain/app/app.go:570-592`); blocked decisions equal, row for row, to the authority genesis rows; under Q1 (a) governed
  values, pending changes, and plan empty, and app version 1.
- InitChainer refuses unless the binary's own app version equals the genesis app version (section 11). An upgrade binary
  cannot initialize a chain, so a new node starts from the genesis binary and switches at each plan height.
- Export refuses (`export refused:` with the first prefix) while any governed value, pending change, plan, or upgrade record
  exists, or the app version is not 1. This joins DR-06-01: restart from export is unsupported once governance state
  exists (`docs/architecture/authority-authorization.md:82`). A fresh chain still round-trips byte for byte.

## 11. Upgrade version model

Components (supported framework only; no new Go module). All are parts of the pinned SDK, store, and CometBFT modules, each
under the Apache License 2.0 (the `LICENSE` file of `cosmos-sdk@v0.55.0`, `store/v2@v2.0.0`, and `cometbft@v0.40.0`).

| Need | Component |
| --- | --- |
| run module migrations | `module.Manager.RunMigrations` with `Configurator.RegisterMigration` and an explicit migration order (`cosmos-sdk@v0.55.0/types/module/module.go:703-757`; `cosmos-sdk@v0.55.0/types/module/configurator.go:104-120`) |
| remember module versions | a persisted version map in x/policy `0x05` |
| switch at one height and refuse a wrong binary in every block | an app PreBlocker step before the module PreBlockers (section 14) |
| atomic failure | the handler runs in a cache context written only on full success; a PreBlock error returns before any commit (`cosmos-sdk@v0.55.0/baseapp/abci.go:859-863`) |
| version visible to CometBFT and clients | `ConsensusParams.Version.App` in the param store; `SetProtocolVersion` from committed state at load and after each Commit (`cosmos-sdk@v0.55.0/baseapp/options.go:193-198,252-258`; `cosmos-sdk@v0.55.0/baseapp/abci.go:1100-1106`) |
| refuse a wrong binary before consensus starts | a load gate in `app.New` (section 13) |

Not used, and why:

- SDK `x/upgrade`: `x/upgrade/types` imports `x/gov/types/v1beta1` (`cosmos-sdk@v0.55.0/x/upgrade/types/proposal.go:3-5`),
  so linking it fails `TestLinkedBinaryOmitsEconomicModules`; its keeper writes files with `os`
  (`cosmos-sdk@v0.55.0/x/upgrade/keeper/keeper.go:528-555`); its authority is one address, not the council.
- cosmovisor: it watches `data/upgrade-info.json` and runs a status command that svnd does not have. Goal 27 may revisit
  binary management.
- Cosmos Enterprise modules: excluded by DEC-006 (`docs/decision-register.md:187`). No commercial-only component is added.
- `baseapp.SetHaltHeight`: a node-local halt is an activation path that no council decision governs (under Q2 (a)).
- A skip-upgrade height (`cosmos-sdk@v0.55.0/x/upgrade/abci.go:73-86`): it would let a minority of nodes override a council
  decision.

Version model (PROPOSED, DR-10-26, reversible):

- App version P: a uint64 compiled into each binary. The goal-10 production svnd has P = 1. It is stored in x/policy `0x04`
  and equals `ConsensusParams.Version.App`. It is not `genesis.ProtocolVersion`, which is the protocol schema line; today the
  two are both 1 only by construction (`chain/devnet/init.go:564`). Goal 10 introduces a separate genesis app version of 1
  and the devnet consensus params use it. Moving P is not a protocol schema change.
- Module versions: each module's `ConsensusVersion`. The map is persisted at InitChain. Each module's layout key equals
  its map entry.
- Binary declaration: P, the module map from the module manager, and a list of known upgrades
  `{Name, FromAppVersion, ToAppVersion, Handler}`. `app.New` refuses an entry with `To != P` or `From != P - 1`. Production
  svnd passes an empty list. `svnd version` prints P, the module versions, the upgrade names, and the VCS revision and
  modified flag from the build info (DR-10-30).

| Module | ConsensusVersion | Layout key | Migration table |
| --- | --- | --- | --- |
| auth wrapper | 7 (`chain/app/authmodule.go:23`) | none | none; auth 5 to 6 to 7 does not apply because every svnd chain starts auth at 7 (DR-10-24) |
| svn (scaffold) | 1 (`chain/x/scaffold/module.go:85`) | none | none; the slot stays a scaffold (DR-10-28) |
| policy | 2 (PROPOSED) | `0x00` = 2 | empty |
| providers, content, leases, evidence | 2 (`chain/x/providers/keeper.go:39`; `chain/x/content/keeper.go:40`; `chain/x/leases/keeper.go:53`; `chain/x/evidence/keeper.go:52`) | `0x00` = 2 | empty |
| authority | 2 (PROPOSED; 1 today, `chain/x/authority/module.go:116`) | `0x00` = 2 (new) | empty; registered so a later step can be added |

Framework gaps the app must close itself:

- `runModuleMigrations` returns nil when `to <= 1` or the version is unchanged (`cosmos-sdk@v0.55.0/types/module/configurator.go:124-128`),
  and its loop runs nothing when `from > to` (`:136`).
- `RunMigrations` calls `InitGenesis(DefaultGenesis)` for a module missing from the stored map and silently drops a module
  the binary no longer has (`cosmos-sdk@v0.55.0/types/module/module.go:715-753`).
- So the runner checks before migrating that the stored map and the binary's map name the same modules and that no stored
  version is above the binary's.

## 12. Upgrade plans: who schedules, when, and notice (depends on Q1, Q2, Q3)

Under the recommended Q2 (a), which needs Q1 (a), (b), or (d):

- A plan is written only by an executed council `software_upgrade` payload, with the approvals Q3 sets, the review delay,
  and the expiry.
- It names a height only. No time activation.
- It is replaced only by a later plan with a new name, or the same name and the identical digest (DR-10-32). A plan name
  can never be reused with a different release digest.
- It is deleted only by an executed `cancel_upgrade` before its height. After the height the chain has either applied the
  plan or halted, so no cancel can commit.
- Lead from the execute block: at least `MinActivationLeadBlocks` = 100 blocks (PROPOSED, Q3; checked at execute,
  `ERROR_CODE_OUT_OF_RANGE`, the proposal stays `QUEUED`) and at most `MaxActivationLeadBlocks` = 1,000,000 blocks (PROPOSED,
  DR-10-15). Devnet consensus timeouts are a few hundred milliseconds (`chain/node/start.go:213-219`), so
  [Educated Guess] 100 devnet blocks take about a minute or less: it is a minimum that the executor cannot shorten, not a
  real notice period.
- The single `AUTHORITY` executor still chooses when, inside that range, a queued plan executes, and may withhold a cancel
  (section 23).
- The plan's `release_digest` is information for operators. The chain cannot verify which binary a node runs. The runbook
  compares the digest with the reviewed release manifest (DR-10-18).
- Off a `shortvideo-dev-` chain every council message is `ERROR_CODE_DECISION_BLOCKED` (`engine.go:143-153`), so under every
  option no upgrade can be triggered on any other chain.

Under Q2 (b) there is no on-chain plan; section 21.2 describes it. A `PlanSource` abstraction lets the gate and runner be
built and tested before the answer.

## 13. Load-time gate

`app.New` runs the gate after loading the latest committed version, outside consensus, only when a block has been committed
(as `checkRegistryLayouts` does today, `chain/app/app.go:328-354`). At height 0 the InitChainer rule of section 10.4 applies
instead. An error from `app.New` makes svnd exit 1 before CometBFT starts (`chain/node/start.go:59-63`) and before the
validator key is loaded (`chain/node/start.go:72`).

Inputs. Committed state: stored app version `Ps`, stored consensus `Version.App`, version map `VMs`, layout keys, plan `Pl`,
last height `L`. Binary: `P`, module map `VM`, known upgrades `U`. "Consistent" means every layout key equals its `VMs`
entry, `Ps` equals the stored `Version.App`, and `VMs` and `VM` name the same modules.

| Row | Case | Outcome | Message |
| --- | --- | --- | --- |
| 1 | consistent, `Ps == P`, `VMs == VM`, no plan at `L+1` | start | |
| 2 | consistent, `Ps == P`, `Pl.height == L+1` | refuse | `upgrade <name> is due at height <L+1>; install the binary for it (release digest <hex>)` |
| 3 | consistent, `Ps == P-1`, `Pl.height == L+1`, `Pl.name` in `U` with `From == Ps` and `To == P`, every `VMs[m] <= VM[m]` | start in the upgrade window | |
| 4 | consistent, `Ps == P-1`, `Pl.name` in `U` with `From == Ps` and `To == P`, `Pl.height > L+1` | refuse | `too early: run the previous binary until height <Pl.height-1>` |
| 5 | `Ps > P`, or any `VMs[m] > VM[m]`, or a stored module the binary lacks | refuse | `state is newer than this binary (app version <Ps>, binary <P>)` |
| 6 | otherwise (for example `Ps == P` with some `VMs[m] < VM[m]`; `Ps < P-1`; `Ps == P-1` without a matching plan and handler; a module the binary has but `VMs` lacks; a layout key that differs from `VMs`; `Ps` not equal to the stored `Version.App`; `Pl.height < L+1`) | refuse | the named mismatch; for a goal-05 to goal-09 home, "initialize a new home" |

The gate is a pure function. It starts if and only if row 1 or row 3 holds; every other input refuses. It is table-tested
on reopened databases and fuzzed for that property.

Goal-09 homes are refused and must be re-initialized, as goals 07-09 did (`docs/operations/validator-lifecycle.md:48-52`;
DR-10-04).

## 14. Per-block rule and the upgrade runner

### 14.1 Why a per-block rule is needed

- CometBFT compares a header's `Version.App` with its own state (`cometbft@v0.40.0/state/validation.go:32-35`). That
  compares two CometBFT values, not the binary.
- Replay and block sync feed stored blocks to whatever binary runs (`cometbft@v0.40.0/consensus/replay.go:478-501`).
- The SDK checks only the halt settings and the height before PreBlock (`cosmos-sdk@v0.55.0/baseapp/abci.go:792-798`).
- So a binary that does not check the app version itself would execute old blocks with new code.

### 14.2 PreBlock step, every block (D-6)

Read the stored app version `A` and the plan (one key each). Then:

1. If a plan exists and `Pl.height < height`: `INTEGRITY` error (cannot happen while I4 holds).
2. If `Pl.height == height`: no handler for `Pl.name` in `U` gives the error
   `UPGRADE "<name>" NEEDED at height <h> release_digest=<hex>`; a handler with `From != A` or `To != P` is an error;
   otherwise run the upgrade (below).
3. Otherwise, if `A != P`: error `stored app version <A>, binary <P>: wrong binary for height <h>`.

So an upgrade binary refuses every block below the plan height, whether it replays its own block store, block-syncs from
genesis, or was started early. No binary runs blocks of an app version it was not built for.

Running the upgrade, all inside one cache context of the PreBlock context:

1. Preconditions: `VMs` and `VM` name the same modules; every `VMs[m] <= VM[m]`; snapshots taken for the self-check.
2. The handler runs, calling `RunMigrations` from the stored map with an explicit migration order.
3. Self-check (I13; it protects only against an honest binary's mistakes, section 9): the snapshots are unchanged except
   the allowed rows; each module's layout key equals the returned map; the returned map equals `VM`. The runner refuses
   to start without at least one snapshot class, so the self-check cannot be left out by omission.
4. Writes into the cache: version map, app version `To`, `ConsensusParams.Version.App = To`, `0x07[name] = h`, plan
   deleted, policy version + 1 (DR-10-38); event `upgrade_applied`.
5. The cache is written only after 1-4 succeed. PreBlock reports changed consensus params.
6. Any error in 1-4 returns before the cache is written. A panic in the handler or in a migration it runs is turned into
   the same handler error (the SDK's PreBlock does not recover one), so it fails the block the same way on every node.
   The SDK reuses a failed attempt's finalize state for a retry of
   the same height (`cosmos-sdk@v0.55.0/baseapp/abci.go:817-820,1016-1022`); because the upgrade step runs first and wrote nothing into it, a
   retry starts from the same state.
   Block h does not commit on that node. Restarting the same binary repeats the attempt; the plan is still due.

`SetProtocolVersion` is set from committed `0x04` at load and after each Commit (`cosmos-sdk@v0.55.0/baseapp/abci.go:1100-1106`), never inside
PreBlock, so ABCI Info (`cosmos-sdk@v0.55.0/baseapp/abci.go:147-159`) never reports an app version that is not committed.

The handler runs once per chain. Its work is bounded by state size, not by a per-block cap. Chain code has no clock, so the
rehearsal measures the time of block h in the test, not in the chain.

### 14.3 The upgrade window (DR-10-41)

Between a row-3 start and the commit of block h, `CheckTx` (ante only) and gRPC queries run new code on old state.

- When the block store already holds h (every node that halted at h), CometBFT's handshake replays h inside node
  construction (`cometbft@v0.40.0/node/node.go:381`), before `tmNode.Start()` and before svnd starts its gRPC server
  (`chain/node/start.go:70-100`). The window closes before any `CheckTx` or query.
- When the block store does not hold h (a node stopped and swapped before h was decided), the window stays open until h
  commits.

Interim: documented and recorded by a test; not fail-closed (section 22.17).

## 15. Failure behaviour without resetting history

"Unchanged" means logically unchanged, shown by several readings: the committed `LastCommitID` (version and hash); a
snapshot of every committed store (`chain/app/app.go:537-551`); after an in-process FinalizeBlock error, a snapshot of the
finalize branch, because a committed snapshot cannot change when no Commit runs; per-store sha256 from a state-check tool;
CometBFT block hashes at fixed heights; the `genesis.json` sha256; `/status` `earliest_block_height` = 1. Opening goleveldb
may rewrite its own files, so file bytes are not compared.

| Case | Expected behaviour | Mechanism | Rehearsal check |
| --- | --- | --- | --- |
| I1 old binary reaches the plan height | PreBlock error `UPGRADE ... NEEDED`; CometBFT logs "CONSENSUS FAILURE!!!" and stops consensus; block h stays in the block store; the app stays at h-1 | section 14.2 step 2; CometBFT saves a decided block before applying it (`cometbft@v0.40.0/consensus/state.go:1766-1775`) and recovers the panic (`:804-817`) | app level: FinalizeBlock(h) errors and the finalize-branch snapshot equals the pre-state; a real restart with the old binary is refused by gate row 2. Devnet: all four logs carry the line; heights stay at h-1 |
| I2 old binary restarted after the halt | exit 1 at `app.New` | gate row 2 | devnet: exits within seconds; same `LastCommitID` and store digests |
| I3 new binary started too early | exit 1 at `app.New` | gate row 4 | devnet: on a copy taken before h-1, with a fresh throwaway validator key, empty peers, distinct ports; exits before any CometBFT start line |
| I4 old binary on post-upgrade state | exit 1 at `app.New` | gate row 5 | devnet: after the upgrade the old binary exits 1 on one node; the new binary rejoins; block hashes 1..h+k unchanged. The prior release (goal 09, `ff951fc`) checks only four registry layouts (`chain/app/app.go:328-354`) and has no app-version rule, so [Educated Guess] it starts on a goal-10 home; its outcome is logged (DR-10-43) |
| I5 binary mounting a store absent from the root | [Educated Guess] exit 1 at load | the root store refuses a store whose version differs from the root version unless a store upgrade adds it (`store/v2@v2.0.0/rootmulti/store.go:259-263`) | app level with a test mount list; store additions are DEFERRED (DR-10-33) |
| I6 wrong migration on one of four nodes | that node commits a different app hash at h, then halts at h+1 with "+2/3 committed an invalid block"; three continue | `cometbft@v0.40.0/consensus/state.go:1750-1751` | devnet: three correct binaries, one wrong. Recovery, only after that log line: section 16.1 action 3 |
| I7 wrong binary replaying history | an upgrade binary fails PreBlock at the first block below h; on a fresh home it fails InitChain | section 14.2 step 3; section 10.4 | devnet: the new binary on a fresh home with the devnet genesis exits 1 |
| I8 handler fails on every node | every node halts at h identically; nothing of h commits; the plan stays due | section 14.2 step 6 | app level: handler error, finalize-branch snapshot equals the pre-state; a real restart with a corrected binary for the same plan name applies it. Its digest differs from the plan's informational `release_digest`, which a halted council cannot update; operators then rely on the reviewed manifest |

In every row the block store is never rewritten and genesis is never replaced. The only restores are the two of section
16.1. svnd sets no pruning or block-retention option (no such call under `chain/` at `ff951fc`). So the root store keeps
every version, its default (`store/v2@v2.0.0/rootmulti/store.go:87-104`), and the app asks CometBFT to retain every block
when the minimum retained block count is 0 (`cosmos-sdk@v0.55.0/baseapp/abci.go:1456-1462`). Any committed height can then
be loaded or replayed on a stopped node or a copy.

## 16. Operator actions: allowed, forbidden, and when rollback is unsafe

Under Q2 (a). Section 21.2 lists the differences under Q2 (b).

### 16.1 Allowed after a failure

1. Restart at the same height with a corrected binary for the same plan name (I8).
2. Wait for a forward-fix upgrade that the council schedules once the chain runs again.
3. Restore one node that diverged alone (I6), only after it logged "+2/3 committed an invalid block": stop it; restore its
   whole `data/` directory except `priv_validator_state.json` from its own copy taken at the halt (block store at h, state
   and app at h-1); keep its newest `priv_validator_state.json`; start the correct binary. CometBFT replays block h with the
   real app (`cometbft@v0.40.0/consensus/replay.go:416-433`) and the node syncs from peers. Restoring `application.db` alone panics on such a
   node, because the handshake asserts the app hash in CometBFT's state (`cometbft@v0.40.0/consensus/replay.go:404-409,554-563`). This is a
   local action that rejoins committed history.
4. On a disposable devnet only, for a replay check: give a healthy node its own earlier `application.db` copy, keep its
   state database, block store, and newest signing state, and start it; CometBFT replays its own block store and checks
   every app hash (`cometbft@v0.40.0/consensus/replay.go:404-409,478-518`).
5. Before the plan height, choose not to install the binary. The council cannot force installation. If more than one third
   of power does not install, no block h is made (DEC-011, `docs/protocol.md:150`).

Validator signing state lives in `data/priv_validator_state.json` (`cometbft@v0.40.0/config/config.go:41`). Copying a
whole home back would restore old signing state, which is why actions 3 and 4 keep the newest file.

### 16.2 Forbidden

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
- One node diverged alone (I6): section 16.1 action 3 only.
- Permanent loss of more than one third of power: no on-chain path exists (DR-10-11, section 22.7).

### 16.4 Planned halt

- Block h+1's header time is the median time of block h's precommits (`cometbft@v0.40.0/state/state.go:245-253`), which were
  cast before the halt. So h+1 carries the pre-halt time and the time jump appears at h+2.
- Expiry does not run while the chain is not committing blocks (`docs/protocol.md:258`), and no lease renewal can commit
  while the chain is halted. Everything that fell due during the halt becomes due at h+2, at most 128 items per block
  (`docs/protocol.md:256`). Providers renew leases whose deadline falls in the expected halt before h.
- No maximum planned halt is set (DR-10-39).
- A new node joining after an upgrade starts from the genesis binary and switches at each plan height.
- After an upgrade that changes a layout, queries and state checks for heights below h must use the old binary.

## 17. Separation of local emergency controls, protocol changes, and publication authority

| Authority | Who | May | May not | Where enforced or documented |
| --- | --- | --- | --- | --- |
| Local emergency controls | each node operator, for its own node | stop its process; close its own ports; disable its own endpoint locally ("Emergency local disablement of a compromised endpoint is not a committed chain decision.", `docs/decision-register.md:247`); decide whether to install a scheduled binary; restore its own diverged node under section 16.1 action 3 | write a committed decision; change another node; change finality; skip a council upgrade; use a node-local halt height as an upgrade trigger (Q2 (a)); approve anything | node code and runbooks; no message exists for any of it (`docs/architecture/authority-authorization.md:96-98`) |
| Protocol changes | the council (DEC-008 approvals, plus any Q3 rule) proposes and approves; the `AUTHORITY` account executes; operators then install reviewed binaries | validator power, governed limits, admit, disable, force-withdraw; under Q1 (a) upgrade plans and cancels and seat replacement; under Q1 (b) auditors | through any payload: approve a blocked decision, change a safety cap, act without the delay and expiry rules, force an operator to install. An upgrade binary can change any rule; the control on it is human review and each operator's choice (section 9) | x/authority, x/policy, the ante (DR-06-06) |
| Public publication authority | the project maintainers ("The project's maintainers control what code enters the reference repository.", `docs/blueprint.txt:1033-1034`); release signing and tags are owner actions in goal 27 | decide what code enters the reference repository; prepare, sign, and tag releases | change chain state; schedule an upgrade; run a validator by virtue of the role | runbooks; DEC-006 blocks release notices (`docs/decision-register.md:203`); goal 10 does none of it |

None of the three owns user content ("Do not conflate any of these with ownership of users' content.",
`docs/blueprint.txt:1034`; REQ-GOV-002, `docs/requirements.md:562`). The devnet concentrates every role in one operator.
Community governance (goal 23) is named, not designed. Goal 10 adds no on-chain pause switch, emergency key, or
single-signature path (DEC-009, `docs/decision-register.md:281`).

Goal 10 itself does not auto-approve a policy, tag a release, rotate a live key, or change a real operator
(`docs/goals/10-policy-governance-and-chain-upgrades.md:30-31`). No committed state, devnet genesis, or positive-path
fixture sets a blocked decision to approved, fills the approved council, or sets a quorum-loss behaviour.

## 18. Rehearsal heights

These heights are specified before the rehearsal runs (`docs/goals/10-policy-governance-and-chain-upgrades.md:37`). The
rehearsal logs every height it used and a per-height table (block hash, app hash, header `Version.App`, ABCI Info app
version, block time) on all four nodes.

| Height | Meaning |
| --- | --- |
| h-1 | last block under app version 1; pre-upgrade queries; state check with the planned state-check tool built for app version 1 (`svn-statecheck`, a separate non-release tool, DR-10-30) |
| h | plan height: the old binary halts (I1); the new binary applies the upgrade in PreBlock |
| h+1 | first header with the new `Version.App` (CometBFT copies it from FinalizeBlock(h)'s consensus params, `cometbft@v0.40.0/state/execution.go:765-778`); header time near the pre-halt time; post-upgrade queries |
| h+2 | first block time after the halt (section 16.4) |
| h+k, k = 5 (PROPOSED) | restart, replay, and state-check point |
| a-1, a | effective-1 and effective heights of a scheduled governed change (Q1 (a)) |

Rehearsal rules (PROPOSED):

- The rehearsal upgrade is named `rehearsal-1` and moves the app version from 1 to 2. Its binaries are built from one
  tree in temporary directories, logged by sha256, and deleted. They are not independently built releases, not signed,
  and not reviewed source.
- App level: two independent apps run the same representative state; a real module migration (leases and evidence 2 to
  3, re-encoding every record) runs in-process. Devnet: four processes on one machine (not independent operators) swap
  binaries with an app-version-only upgrade that decodes every record but rewrites no module store.
- Representative state holds providers, active, expired, and ended leases, open offers, evidence records, open and queued
  proposals, a pending validator change, a governed value, and a pending governed change (Q1 (a)). A census at h-1 and h+k
  requires every class to be greater than 0.
- No deadline, expiry, or activation falls in `[time(h-1), time(h+2)]`. On the devnet, cross-boundary deadlines are the
  pre-halt block time plus a margin of 90 s (PROPOSED); the test fails with "halt exceeded the margin" if the halt is
  longer.
- Transactions stop before h-1; blocks h-1, h, and h+1 hold none. The comparison between h-1 and h+1 allows exactly these
  differences: app version, plan (gone), upgrade record (new), and policy version (+1, DR-10-38).
- The minimum lead (100 blocks, PROPOSED) applies to the rehearsal plan too.

## 19. Deviations from `spec_machine.py`

Goal 06's recorded differences stay. Goal 10's own:

| Id | Chain | Spec | Why |
| --- | --- | --- | --- |
| D-1 | governed limit key outside the five caps, or value above the ceiling, is `BOUNDS` at propose (step 7a) | accepted at propose; `BOUNDS` at execute, proposal stays `QUEUED` (spec :1338-1343) | a proposal that can never execute should not hold a quota slot or a seat's time |
| D-2 | `ProposalExecuted` emitted after `PolicyChanged` | `PolicyChanged` only (spec :1526) | `docs/protocol.md:296` lists both; goal-06 follow-up X5 |
| D-3 | withdrawn (section 7: a recount by seat key cannot be built from the record and no message reaches it) | | |
| D-4 | governed limit with an activation height: pending, applied at BeginBlock of that height; an immediate change deletes a pending row of the same key (Q1 (a)) | no field | "delayed parameter changes" (`docs/requirements.md:461`) |
| D-5 | `software_upgrade`, `cancel_upgrade`, `council_seat` (and the auditor kinds under Q1 (b)) accepted | an unknown payload alternative is `MALFORMED` | `docs/goals/10-policy-governance-and-chain-upgrades.md:18,21,34` |
| D-6 | PreBlock: at the plan height a binary without the handler fails; at any height a stored app version that differs from the binary's fails, except at the plan height with a matching handler; nothing of the block commits | no upgrade concept | `docs/goals/10-policy-governance-and-chain-upgrades.md:21-23`; section 14.1 |
| D-7 | a seat key equal to a current or pending validator consensus key is refused at genesis and in `council_seat`; a validator payload whose consensus key equals a current or retired seat key is `DUPLICATE_ID` at execute | not checked | key separation (REQ-SEC-002, `docs/requirements.md:543`) |
| D-8 | x/policy applies due changes in BeginBlock before x/leases expiry | no policy BeginBlock | activation is effective at height a for every reader |
| D-9 | `Query/Policy` and `Query/Governance` registered with paging | no query semantics | REQ-GOV-003 "published delay" |
| D-10 | genesis refuses a seat id longer than 59 characters (PROPOSED) | id pattern up to 64 (`chain/x/authority/payload.go:36`) | `seat-<seat_id>` must fit the 64-character parameter-key pattern, or the seat could never be replaced |

The keeper-level differential (`chain/x/authority/difftest`, through `tests/difftest/spec_oracle.py`) names goal 06's
differences G-1 to G-10. G-1 to G-5 and G-7 are recorded in `docs/evidence/goal-06.md:316-322`; G-6, the approved
four-seat, threshold-three shape, in `docs/architecture/authority-authorization.md:32`. G-8 (the validator set re-checked
after a due group; no message reaches it), G-9 (validator activation unix time out of range: `BOUNDS` on the chain,
`MALFORMED` in the spec), and G-10 (validator node id outside the id pattern: `BOUNDS` whatever the later fields hold) were
first written down by goal 10. The oracle applies G-6 per step (any other shape makes every council step
`DECISION_BLOCKED`) and extends G-10 to an absent or unparseable reason digest, activation, or power; both scopes are wider
than the plan's wording and are open for confirmation.

Open differences that no rule names. On eight propose payloads the chain answers `BOUNDS` and the spec answers `MALFORMED`;
neither side changes state. They are: a validator reason digest whose value is not 32 bytes; a negative validator power; an
empty consensus key; a withdraw reason digest whose value is not 32 bytes; a withdraw content id outside the id pattern
together with a reason digest without an algorithm; a governed value of 0; an empty governed limit key; an empty provider id
in an admit payload. The last three are proto3 defaults the spec reads as absent fields. A keeper-level differential test
pins each one; the random generator does not send them. Each needs a decision: a named oracle rule, or a chain change to
`MALFORMED`, which changes a client-visible code.

A divergence from `spec_machine.py` outside G-1 to G-10, D-1 to D-10, and the eight open differences above is reported,
never hidden.

## 20. Invariants

- I1 policy version. State-only form: `version >= 1 + number of EXECUTED records`. Exact form, in harnesses that count
  operations: `version == 1 + successful executes + applied upgrades`.
- I2 every governed value row has a key in `governed_limit_caps` and a value in 1..ceiling.
- I3 at most one pending row per key; every pending activation height is above the last committed height; no pending row
  is older than the latest executed change of its key.
- I4 at most one plan; its height is above the last committed height while the chain runs; its name has no upgrade
  record.
- I5 stored app version = `ConsensusParams.Version.App` = 1 + number of upgrade records; outside the upgrade window it
  equals the running binary's app version.
- I6 the persisted version map equals the running binary's map outside the upgrade window, and each layout key equals the
  map entry of its module.
- I7 safety caps equal the genesis defaults; every decision row unapproved with value version 0; approved council empty;
  quorum loss `UNSET`; devnet rows unchanged since genesis.
- I8 exactly four seats; seat keys distinct, none retired, none equal to a current or pending validator consensus key; no
  validator consensus key equal to a retired seat key; no seat address is a submitter.
- I9 every `OPEN` or `QUEUED` proposal has distinct approver seat ids; `QUEUED` has at least the threshold for its kind.
- I10 open counts equal the `OPEN` and `QUEUED` proposals per base version and seat.
- I11 the validator set and every pending validator change meet DEC-011 at every activation point.
- I12 only InitGenesis, the authority hook, the x/policy BeginBlock, and the upgrade runner write the policy store.
- I13 the upgrade runner's self-check leaves unchanged every authority row except the policy version, which moves by
  exactly 1; the x/policy safety caps, governed values, and pending rows; and every consensus parameter except
  `Version.App`. Rows are compared byte for byte, or as decoded values for a module the upgrade declares it migrates. I13
  protects only against an honest binary's mistakes (section 9).

## 21. Open owner questions (Q1, Q2, Q3, DR-10-00)

Each question below is open in this file. No answer is recorded in `docs/decision-register.md` at `ff951fc`. The
recommendation is a proposal. The interim is what the code does until an answer is recorded; it is not an owner decision.

### 21.1 Q1 (DR-10-01): which protocol additions may goal 10 make, and what does "removed council seats" mean?

Facts: section 2 (`docs/protocol.md:26`; five payload kinds; no plan, seat, or query field). Goal 02's checker pins every
protocol name (`docs/evidence/goal-02/check_protocol.py:90-123`).

Options:

- (a) Recommended. The minimal additive revision of section 5: an activation height on governed-limit changes;
  upgrade-plan and cancel payloads; council seat key replacement (the council stays at four seats; the old key is retired
  for good; the new key's address gets an account); query fields for governed values, pending changes, the plan, the app
  version, and the labelled devnet delay. The document schema version becomes 2; every message-level `schema_version`,
  the protocol name, the peer media type, and genesis `protocol_version` stay 1; existing payload digests are unchanged.
  It also permits updating the name pins in goal 02's checker.
- (b) As (a), plus council admission and disablement of `AUDITOR` accounts under the goal-09 rulings (no key rotation; the
  8-byte prefix rule; a disabled address retired for good).
- (c) No protocol change. Upgrades can then only be operator-coordinated (Q2 (b)). "Removed council seats" and "scheduled
  activation" cannot be tested through messages and are recorded BLOCKED. Governed values, plans, and the delay are not
  queryable.
- (d) As (a), but schema version 2 everywhere: evidence batches, capability manifests, decision rows, and the peer media
  type also move. It changes goal 07-09 code and fixtures with no security gain.

Interim until answered: no `.proto` file, `bounds.json`, protocol document, fixture, or goal-02 checker pin changes. The
chain adds only checks that need no new field: the typed propose check (D-1), the typed events, and queries of existing
state. No new payload kind is accepted. Label: fail-closed (the chain accepts nothing it did not accept before).

### 21.2 Q2 (DR-10-02): upgrade trigger, wrong-binary stop, operator actions after a failure, approved proposals at an upgrade

Facts: the blueprint asks for "a versioned migration proposal, reviewed source, release hashes and a scheduled activation
point" (`docs/blueprint.txt:596`). SDK `x/upgrade` cannot be linked (section 11). DEC-009 chose council-only governance with
no emergency path (`docs/decision-register.md:281`). CometBFT does not check which binary a node runs (section 14.1).

Options:

- (a) Recommended. Council-scheduled, height-only plan (needs Q1 (a), (b), or (d)); the per-block app-version rule; the
  load gate; no skip height and no node-local halt height; the operator actions of section 16; approved but unexecuted
  proposals become stale at the upgrade (DR-10-38).
- (b) Operator-coordinated. No on-chain plan: each release compiles `{chain id, name, height}`, accepted only on
  `shortvideo-dev-` chain ids, and operators set a halt height in their node configuration. It needs no protocol change.
  The activation is then not a council decision, and a node without the setting executes that height under old rules and
  forks off. Under (b) a node-local halt height becomes an allowed operator action, devnet only.
- (c) As (a), but approved proposals survive the upgrade. An approval given for old behaviour can then take effect under
  new code; release review must confirm that payload behaviour did not change.

Interim until answered: no production plan source exists in svnd, so no upgrade can be triggered on any chain. The gate,
the per-block rule, and the runner can be built and tested with test plan sources only. Label: fail-closed (no activation
path exists).

### 21.3 Q3 (DR-10-03): which approvals and how much notice do upgrades and seat replacements need?

Facts: DEC-008 defines one class of change, "three approvals for an ordinary change" (`docs/decision-register.md:247`).
The devnet review delay is 0 (`chain/devnet/init.go:541`). The only activation rule today is "strictly after" the
executing block (`docs/decision-register.md:265`). One `AUTHORITY` account executes and chooses when.

Options:

- (a) Recommended. The same three of four approvals and review delay as every change; a plan height at least 100 blocks
  (PROPOSED) and at most 1,000,000 blocks (PROPOSED) after the executing block; a plan name never reused with another
  digest. Not fail-closed against three colluding seats: human review of the binary and each operator's install choice
  stay the decisive controls.
- (b) Unanimity for upgrades. An upgrade plan needs all four seats; a seat replacement needs the three seats other than
  the one replaced; a cancel stays at three of four; same minimum notice. One honest seat can stop a malicious upgrade, but
  one lost key blocks every upgrade, including a security fix, until that seat is replaced. It adds per-kind thresholds
  that DEC-008 did not set.
- (c) Ordinary approvals and no minimum notice ("strictly after"). The executor can then execute a plan one block before
  its height, so operators may get no notice.

Interim until answered: the new payload kinds do not exist, so no approval rule for them applies. Every existing kind keeps
three of four approvals and the DEC-008 devnet delay. Label: fail-closed (nothing new can be approved).

### 21.4 DR-10-00: approval of this specification

The goal-10 prerequisite "migration and governance specifications approved"
(`docs/goals/10-policy-governance-and-chain-upgrades.md:8`) cannot be met by an existing document: none existed before
goal 10. Goal 09 recorded its own unmet specification prerequisite the same way (DR-09-26, `docs/evidence/goal-09.md:23`).

Options: (a) the owner approves this file at a pinned commit and file sha256; (b) the owner rejects it or asks for changes;
(c) the owner names another specification.

Interim: this file is PROPOSED. Approval is asked at PR time. Until then the prerequisite stays BLOCKED, and goal 10 is
reported BLOCKED for it. An approval would meet the prerequisite for goal 10 only. Label: fail-closed (the goal status does
not advance without it).

## 22. Security-relevant requests with interim rules

Each request below has an interim rule that goal 10 implements, tests, and reports as open. None is an owner decision.

### 22.1 DR-10-00

Section 21.4.

### 22.2 DR-10-04: exports, restart, replay, continuity

Options: (a) in-place continuity only; exports of state that holds governance records stay refused; (b) add genesis fields
for lossless export (a protocol addition, DR-10-40); (c) a one-time migration from goal-09 homes.

Interim: (a). Exports of representative state are refused with a named prefix, as a tested restriction (DR-06-01). Restart
and replay use the same homes and block stores. Goal-09 homes are re-initialized, as goals 07-09 did
(`docs/operations/validator-lifecycle.md:48-52`). Option (c) is not offered, because a migration without an on-chain plan
is an activation no council decision governs. Label: fail-closed.

### 22.3 DR-10-05

Retired: merged into DR-10-02 (Q2). No separate interim.

### 22.4 DR-10-08: auditor admission and removal

Options: (a) council admit and disable payloads under Q1 (b); (b) genesis only, as in goal 09.

Interim: (b). No auditor payload exists, and DR-09-08 stays DEFERRED with its residual: "A compromised or dishonest auditor
cannot be removed on chain in v0; its records stay." (`chain/x/evidence/README.md:611-612`). Label: fail-closed for
admission (no new write path); the goal-09 residual stays open.

### 22.5 DR-10-09: floors for in-range governed values

Options: (a) no floor; (b) PROPOSED floors for liveness keys such as `max_expiry_steps`.

Interim: (a). Key and range are checked at propose and execute; every key is tested at 1 and at its ceiling with the chain
live, and the effect of each key's lowest value is documented. No floor number is invented. Label: not fail-closed for
liveness (a council majority can set a key to 1).

### 22.6 DR-10-10: single AUTHORITY executor

Options: (a) accept one executor for the devnet beta; (b) add an `AUTHORITY` replacement payload in a later protocol
revision (DR-10-40).

Interim: (a). Its loss freezes execution, including any change that would replace it. It also chooses when a queued plan
executes inside the lead range and may withhold a cancel. Label: not fail-closed for liveness.

### 22.7 DR-10-11: permanent loss of more than one third of power

Options: (a) no on-chain path; out of scope for the beta; (b) a quorum-loss behaviour, which is a DEC-008 decision
(`QuorumLossBehavior` is `UNSET` with `ELECTED`, `FREEZE`, `ROOT` reserved,
`docs/protocol/proto/svn/authority/v1/state.proto:25-30`).

Interim: (a). The runbook says there is no on-chain path, and that a new network would be a new chain id with this chain's
history kept and published, needing its own owner decision; never a reset of this chain. This reconciles
`docs/operations/validator-lifecycle.md:31,46`. Label: fail-closed (no mechanism exists that could bypass finality); the
chain stays halted.

### 22.8 DR-10-12: reading of the BLOCKER:DEC-008 rows REQ-TX-006 and REQ-GOV-003

Options: (a) "VERIFIED for the approved shape on the devnet; BLOCKED for live values", as goal 06 read its DEC-008 rows
(`docs/evidence/goal-06.md:7-14`); (b) BLOCKED outright.

Interim: (a). Label: a reporting rule, not a safety rule; it never claims live values (`docs/requirements.md:474,563`).

### 22.9 DR-10-13: key separation in both directions

Options: (a) refuse seat keys equal to validator consensus keys and the reverse (D-7); (b) follow the spec, which does not
check.

Interim: (a), stricter than the spec. Label: fail-closed.

### 22.10 DR-10-14

Superseded by Q3 (section 21.3). DR-10-14 was the interim rule "three of four and the review delay for upgrades and seat
replacements". That is the least strict reading, so it is asked as Q3 instead of applied. The new payload kinds wait for
the answer.

### 22.11 DR-10-15: maximum activation lead and compile-time quotas

Options: (a) `MaxActivationLeadBlocks` = 1,000,000 blocks (PROPOSED) for activation heights and plan heights; (b) another
number; (c) no maximum.

Interim: (a). The minimum lead for plans is part of Q3. Compile-time quotas stay compile-time (DR-06-03, DR-06-04,
DR-08-02, DR-08-03, DR-09-06); changing one is a binary upgrade. Label: not fail-closed (it is a new number).

### 22.12 DR-10-16: business-id squatting

Options: (a) seats retry with a fresh id; (b) bind business ids to the signer (a protocol addition, DR-09-28).

Interim: (a). Squatting across modules is a denial of service, never a wrong transition. Label: not fail-closed against
denial of service; no wrong state results.

### 22.13 DR-10-17: no cancel for pending validator changes

Options: (a) keep DR-06-04: no cancel; (b) a cancel payload (a protocol addition, DR-10-40).

Interim: (a). An executed validator change cannot be withdrawn before its activation. Label: not fail-closed.

### 22.14 DR-10-18: the plan's release digest

Options: (a) informational only; (b) any chain-side check of a binary digest, which the chain cannot perform: a node's
binary is outside consensus.

Interim: (a). "Reviewed source" is a human step and is never claimed. The per-block app-version rule (section 14.2) and
human review are the controls. Label: not fail-closed (the chain cannot detect a wrong binary that declares the right app
version and handler name).

### 22.15 DR-10-38: approved but unexecuted proposals at an upgrade

Options: (a) the applied upgrade bumps the policy version, so they become `STALE_VERSION` or `CONFLICT`; (b) Q2 (c): they
survive.

Interim: (a) (section 6). Label: fail-closed.

### 22.16 DR-10-39: maximum planned halt

Options: (a) no number; (b) a PROPOSED maximum, which would relate to the DEC-005 lease term, itself unset.

Interim: (a). The runbook's renewal step and a halt-expiry test cover the effect (section 16.4). Label: not fail-closed;
open.

### 22.17 DR-10-41: upgrade window behaviour

Options: (a) document and test the window (section 14.3); (b) refuse `CheckTx` and queries in the window.

Interim: (a); (b) stays a reversible option. Label: not fail-closed.

### 22.18 DR-10-42: seat id length

Options: (a) cap seat ids at 59 characters at genesis (PROPOSED, D-10); (b) a longer parameter-key pattern (a protocol
change).

Interim: (a), stricter than the spec. Label: fail-closed.

### 22.19 DR-10-43: prior release on a goal-10 home

Options: (a) log the prior release's outcome in the rehearsal and record a residual if it starts; (b) nothing.

Interim: (a). If the goal-09 binary starts on a goal-10 home, REQ-UPG-001 ("An old binary is not assumed to read new
state.", `docs/requirements.md:493`) is not claimed for it, and the residual is recorded. Label: not fail-closed if it
starts; goal-10 homes are new chains, which limits this to operator error.

### 22.20 DR-10-19 and DR-10-40: threat notes and protocol additions

- DR-10-19: threat notes for governance capture, a malicious upgrade binary, the single executor, a minority skip, and
  prior-release divergence go into the goal-10 evidence file. Whether to add them to `docs/threat-model.md` (a goal-01
  document) is asked, not done. Label: a documentation question, not a safety rule.
- DR-10-40, recorded, not implemented: `authority_result` and the four untyped goal-10 events in the event list; a cancel
  for pending validator changes; an `AUTHORITY` replacement payload; business ids bound to the signer; genesis fields for
  lossless export; typed upgrade events; a numeric admission-limit key; a quorum-loss behaviour (DEC-008).

## 23. Residual risks

- An upgrade binary can change any rule, cap, quota, seat, or decision row. Its self-checks run inside it and protect only
  against an honest binary's mistakes. The controls are human review of source and digest and each operator's refusal to
  install. Neither is a test. On the devnet one operator holds every seat and every node.
- Governance capture: three colluding or compromised seats (four under Q3 (b)) can schedule any upgrade.
- A plan whose binary does not exist halts the chain at h. The council cannot repair a halted chain ("A chain stall can
  prevent on-chain governance from executing.", `docs/decision-register.md:247`). Only a binary for the plan resumes it.
- The single `AUTHORITY` executor: its loss freezes execution; it chooses when a queued plan executes inside the lead range
  and may withhold a cancel.
- The prior release (goal 09) has no app-version rule and checks only four registry layouts (`chain/app/app.go:328-354`).
  An operator who runs it on a goal-10 home may get a diverging node.
- A node commits one wrong height before CometBFT detects a wrong migration (I6). Copy-at-halt is the only local recovery
  without a rollback tool.
- Upgrade window: a node swapped before block h was decided serves `CheckTx` and queries with new code on old state until h
  commits.
- A planned halt makes everything that fell due during it due at h+2, at most 128 items per block. Leases whose deadline
  falls in the halt cannot be renewed while halted. No maximum planned halt is set.
- A new node joining after an upgrade must start from the genesis binary and switch at each plan height.
- Migration work runs once in PreBlock with no per-block cap. A large state could make block h slow; it is measured only on
  the rehearsal state, in-process.
- After an upgrade that changes a layout, historical queries and state checks below h through the new binary can misread
  state.
- Raw `/store` queries are unmetered; CometBFT RPC must not be public.
- The `EXECUTED` or `REJECTED` to `EXPIRED` relabel stays (spec behaviour).
- Business-id squatting is a denial of service, never a wrong transition.
- The release digest in a plan is information only. The chain cannot tell which binary a node runs.

## 24. Steps that need a human approval or an independent security review

Goal 10 lists each step and performs none.

1. Answers to Q1, Q2, and Q3, and approval of this file at a pinned commit and file sha256 (DR-10-00), asked at PR time.
2. A recorded decision for any protocol revision, and permission to update goal 02's checker pins (Q1).
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
11. Independent security review of the authority changes, the upgrade gate, the per-block rule and runner, every migration
    handler, and the release digest procedure (REQ-SEC-011, `docs/requirements.md:552`; goal 29). Model-assisted review is
    not that review.
12. Adding the governance-capture, malicious-upgrade, executor, and prior-release threats to `docs/threat-model.md`
    (DR-10-19).
13. Merging goal 09 (PR 12) and goal 10, and any branch-protection change for new CI checks.
14. Any council-scheduled upgrade on a chain other than a disposable devnet.
15. Recovery from permanent quorum loss (out of scope; a new network needs its own decision).
16. A maximum planned halt and any floors for governed values (DR-10-39, DR-10-09).
