# Decision register

Status of this register: Gate A on 2026-10-31 (America/Toronto) approved DEC-001 through DEC-012. The protocol specification is not an approved protocol. Protocol v0 freezes once goal 02 passes review. This revision is the candidate. It does not by itself claim that review has passed.

Public launch: DISABLED

No tradable token, coin, sale, or mining.

This register records decisions whose answers change security or public-operation commitments. A PROPOSED default was a suggestion. Gate A accepted every PROPOSED default for DEC-001 through DEC-008. The quotes and the reviewer's reading are in the Gate A record below. Reversible documentation choices made while writing Goal 01 are listed at the end and are not requests for approval.

The protocol specification is not an approved protocol.

## Gate A record

Date: 2026-10-31 (America/Toronto).

At 06:58 Max said "I take all your recommendations".

Reviewer's reading: the 06:58 reply approves DEC-009, DEC-010, DEC-011, and DEC-012. DEC-009 is council only, or a narrow emergency path that was not selected. DEC-010 derives REJECTED. DEC-011 keeps down out of consensus and approves the power-share cap and the minimum validator count. DEC-012 keeps CONFLICT.

At 06:59 Max said "Yes let's do it", in reply to the reviewer's 4-question card.

Reviewer's reading: this accepts the recommended option on every question. DEC-009 is council-only for admit, disable, and force-withdraw, with no single-signature path. DEC-001 through DEC-008 accept every PROPOSED default. The launch allocation (grok-4.7, xhigh reasoning) counts as reported effort for goals 01 and 02. SVN-JCS-1, SHA-256, Ed25519, the proposed signing domains, the safety and governed caps, and the shortvideo-dev- prefix are accepted. Protocol v0 freezes once goal 02 passes review.

## DEC-001

### Question

Who holds the keys that authorize a creator's hosting intent in the beta, and what account-recovery path is allowed?

### Why this needs the owner

The answer decides who can publish, migrate, or take over an account, and whether a compromised account host can sign as the user. That is a custody and recovery commitment. Viewer login is a separate boundary from chain signing, and this decision must not collapse those boundaries.

### PROPOSED default

The account host holds a delegated signing key for beta public records, and the product discloses that custody. A web passkey is a login authenticator and is not a Cosmos transaction key. Email, recovery material, and sessions stay off-chain. Operators, not viewers, hold chain-submission credentials. A later user-controlled root identity stays deferred. Recovery-key changes, session revocation, and host migration require a strong confirmation, and the key history must not expose secrets.

This was the PROPOSED default. Gate A accepted it on 2026-10-31.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:59 reply accepts the PROPOSED default for who holds hosting-intent keys.

### Blocks

Goal 02 must not freeze the account host as the owner of hosting-intent signatures. Goal 19 must not claim portability or a recovery procedure from this default. Goal 26 backup and signer-recovery drills wait on this decision. Devnet keys stay fresh development-only keys and are not a stand-in for this policy.

## DEC-002

### Question

Who may run a validator, storage host, processor, or account service in the beta, and what evidence of distinct control is required before the project describes any operator as independent?

### Why this needs the owner

Admission is the boundary against Sybil identities and undisclosed control. Calling four processes "independent operators" would overstate the trust model. Permissionless admission is a different, later system and is outside this beta.

### PROPOSED default

Admission stays permissioned. A reviewed operator registry is the beta mechanism. Four equal-power development validators are development processes, and the spec describes them as that. A storage operator is not required to become a validator. No permissionless validator or provider admission is included. Multiple node identities are not treated as proof of independent operators or independent copies.

This was the PROPOSED default. Gate A accepted it on 2026-10-31.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:59 reply accepts the PROPOSED default for permissioned admission.

### Blocks

The real operator list, any public claim of operator independence, and the Goal 24 walkthrough that requires a separate person. Goal 07 can still define an admission record whose membership policy stays blocked. Goal 30's independent-install demonstration stays blocked until a separate operator and machine exist.

## DEC-003

### Question

What community rules determine whether a video may be published, who decides an appeal, and what a removal operation promises to do and not to do?

### Why this needs the owner

The answer is the public-operation commitment about other people's speech and about deletion. A wrong promise (for example universal deletion, or a blockchain certification of legality) is a security and trust failure even when the software functions.

### PROPOSED default

The product provides report, block, an administrator queue, decisions with reasons, appeals, and an urgent escalation path. Sensitive case material stays off-chain and access-controlled. A signed public label can state a community outcome without naming the complainant. Removal delists the item, ends or cancels participating hosting commitments, purges caches the operators control, and revokes relevant credentials. Independently retained copies and historical public-ledger records remain. Validators do not decide the legality of each video. Published means the moderation and hosting conditions in force are satisfied. It does not mean the chain certified the content as lawful or truthful. The five-minute delisting figure in the blueprint is a PROPOSED gate, not a promise.

This was the PROPOSED default. Gate A accepted it on 2026-10-31.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:59 reply accepts the PROPOSED default for community rules and removal.

### Blocks

The rule text, the appeal authority, the removal promise, and any service-level timing. Goal 23 requires this decision before its policy-bearing behavior can be treated as specified. Public replication stays disabled while public launch is disabled.

## DEC-004

### Question

How long are media packages, creator originals, backups, logs, moderation files, and chain-coordination records kept, and what is deleted when a video or an account is removed?

### Why this needs the owner

Retention is the privacy and deletion commitment. It also decides whether a live replica is being treated as a backup, which it is not.

### PROPOSED default

No retention duration is set. The beta does not promise universal deletion. Raw watch histories stay out of public protocols. Complaint narratives, private messages, and legal evidence files stay off-chain. Chain coordination records are not rewritten to imitate erasure. Backups, availability, and integrity are separate. An origin or creator backup policy is unset. IPFS pinning is not the retention mechanism of this beta.

This was the PROPOSED default. Gate A accepted it on 2026-10-31.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:59 reply accepts the PROPOSED default for retention.

### Blocks

Goal 23 deletion behavior, Goal 26 backup scope, and the privacy assessment required before any public launch. Goal 02 may classify fields and define a removal tombstone shape. It must not invent a retention period.

## DEC-005

### Question

Who sponsors an upload allowance and a lease duration, what the limits are, and what the network does when that support ends?

### Why this needs the owner

Sponsorship decides who is responsible for storage cost and what users are told when support stops. A credit, reward, or allowance that behaves like money would contradict the ban on a financial asset. This beta does not add a token to answer the question.

### PROPOSED default

No monetary rewards, no tradable token, no coin, no sale, no mining, no automatic financial slashing, and no contribution-credit system. Hosts are not promised payment by this specification. A future community sponsor would name an allowance and a lease term. Unfunded or expired leases are not presented as permanent availability. Quota mechanisms exist as engineering scope. The per-account number and the lease length are unset. Blueprint arithmetic (100 daily viewers, 25 MB packages, three copies, and similar figures) is planning illustration, not a budget or a measured demand.

This was the PROPOSED default. Gate A accepted it on 2026-10-31.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:59 reply accepts the PROPOSED default for sponsorship terms.

### Blocks

Numeric account allowances, lease durations, any non-cash credit policy, and any statement that a named sponsor already exists. Goal 08 can still specify that every accepted lease needs a responsible party, a term, a budget, and an exit once those values are decided. Goal 25 enforces quotas without inventing the numbers.

## DEC-006

### Question

Which licence applies to original server and application code, to original protocol client libraries, and to the specification? Separately, what upload licence must a creator grant for hosting, replication, transcoding, and playback, and how are FFmpeg and other dependency licences recorded?

### Why this needs the owner

Software licensing and the creator's rights grant are public-operation commitments. They are different questions. A hash or a chain timestamp does not establish ownership or permission. Combining licences without a compatibility review can make the tree undistributable.

### PROPOSED default

Consider AGPLv3 for original server and application code and Apache-2.0 for original protocol client libraries. Confirm compatibility before applying either. Keep third-party notices. Do not relicense code beyond the rights granted. Do not include Cosmos Enterprise PoA or Groups modules. Record FFmpeg build flags, codecs, and licence obligations when a build exists. The creator upload licence would cover hosting, replication, transcoding, and playback of material the creator has permission to publish. Commercial music, payments, and native-store distribution stay deferred.

This was the PROPOSED default. Gate A accepted it on 2026-10-31. No licence file in this goal applies those terms.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:59 reply accepts the PROPOSED default for the project licence split.

### Blocks

Goal 27 release notices, any vendoring of reference-project code, and the creator-facing rights text. Goal 03 may pin dependency versions and record their licences as discovered facts. It must not treat the project licence split as approved.

## DEC-007

### Question

Who is invited into the beta, is participation limited to adults, what community is this for, and which jurisdiction's privacy duties will be reviewed before any public launch?

### Why this needs the owner

Audience policy decides whether minors are in scope and what safety claim the product is allowed to make. The blueprint states that a safe deployment involving minors cannot be confirmed from the blueprint. Brand and community choice also decide whose rules Goal 23 will eventually enforce. This specification does not give legal advice.

### PROPOSED default

Accounts are invited. Public sign-up stays off. Public launch stays disabled. A developer or learning community is an example, not a chosen market. The product name is unset and is not a security blocker by itself. The beta makes no minor-safety claim. A privacy assessment and jurisdiction-specific advice, including any Canadian or Quebec review, remain unmet gates for public launch. The 2018 OPC overview cited by the blueprint is not used here as a current-law checklist.

This was the PROPOSED default. Gate A accepted it on 2026-10-31.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:59 reply accepts the PROPOSED default for audience and age policy.

### Blocks

Public launch, community-rule tone, any age gate, and any marketing or adoption claim. Goal 19 may implement an invitation mechanism without an approved community.

## DEC-008

### Question

Who holds the council seats that can change validator membership and protocol parameters, what approval threshold and delay apply, and what happens when the council loses quorum or loses keys?

### Why this needs the owner

This is who can change the consensus set. The blueprint lists initial council members as a decision to record before implementation. It is an additional security-critical question beside the seven topics named for this goal. Leaving it implicit would hide the control boundary.

### PROPOSED default

Four named seats, three approvals for an ordinary change, one approval per seat, an expiry, and a published delay. Council voting is application governance and is a different process from CometBFT consensus voting. Emergency local disablement of a compromised endpoint is not a committed chain decision. Quorum-loss behavior is unspecified. No person is named. Cosmos Enterprise Groups is not the implementation. A chain stall can prevent on-chain governance from executing.

This was the PROPOSED default. Gate A accepted it on 2026-10-31.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:59 reply accepts the PROPOSED default of four seats and three approvals. The devnet fixture's smaller numbers are not a second policy.

### Blocks

Production council parameters and any genesis that names real council keys. Gate A accepted four seats and three approvals. Goal 06 and Goal 10 must not treat the devnet fixture's smaller numbers as that policy. A devnet may use disposable test keys and may store a threshold field.

A validator power change in the v0 draft names its own activation point: a committed height or a committed chain time strictly after the block that executes the proposal. That is the "not immediate" rule. No minimum delay beyond that rule is chosen. Any longer mandatory delay stays inside this decision.

## DEC-009

### Question

Should `MsgAdmitProvider`, `MsgDisableProvider`, and `MsgForceWithdraw` be removed, or should they stay as a bounded emergency path beside the council proposal?

### Why this needs the owner

Blueprint page 12 describes operator and validator changes as council actions with a 3-of-4 threshold, an expiry, and a delay. The payload path is `AdmitProviderChange`, `DisableProviderChange`, and `ForceWithdrawChange` through `ProposeChange`, `ApproveChange`, and `ExecuteChange`.

The direct messages are not in the proto service. `op_admit_provider` (`docs/evidence/goal-02/spec_machine.py:456`), `op_disable_provider` (`spec_machine.py:1829`), and `op_force_withdraw` (`spec_machine.py:1833`) return `ERROR_CODE_FORBIDDEN`. `council_ready` (`spec_machine.py:381`) is the council gate for the payload path. Fixture setup writes `ADMITTED` from `cases.py`. There is no `FixtureAdmitProvider` handler.

### PROPOSED default

The approved choice is council only. The alternatives were to remove the three direct messages, or to keep them as a bounded emergency path. The emergency path was not selected.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:58 reply approves this decision, and the 06:59 reply selects council-only admit, disable, and force-withdraw, with no single-signature path.

### Blocks

Goal 06 and Goal 10 must use the council payload path. The direct messages are not an emergency path. Goal 02 does not choose a threshold beyond DEC-008.

## DEC-010

### Question

Should evidence state `REJECTED` be derived from verification of the attestation, or is the submitter's `reject` flag an auditor verdict?

### Why this needs the owner

There is no submitter `reject` field. `op_audit` (`docs/evidence/goal-02/spec_machine.py:893`) requires `observation_digest`, stores it, and binds it in the auditor signature. The state is `REJECTED` when that digest does not equal the lease manifest, and `RECORDED` when it does. A step that still carries `reject` is `ERROR_CODE_UNKNOWN_FIELD`. The check compares digests. It does not fetch hosted bytes.

### PROPOSED default

The approved choice derives `REJECTED` from that digest comparison. The alternative was a submitter `reject` flag. That flag is not in the proto.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:58 reply approves this decision, and the 06:59 reply selects REJECTED derived from verification, not a submitter flag.

### Blocks

Later evidence and moderation work must not describe a missing `reject` flag as proof that the chain fetched the media. The recorded rule is the digest comparison above.

## DEC-011

### Question

Is validator liveness, the fixture `down` set, consensus state that belongs in the application hash, or is it only an observation about the current block?

### Why this needs the owner

`down` used to decide both block commit and power-change scheduling while staying outside the application hash. Two copies of the same transactions could then diverge. That is not a deterministic consensus input.

### PROPOSED default

The approved choice keeps liveness out of consensus state and out of the application hash, and it approves the power-share cap `max_power * 3 < total_power` and the minimum validator count of four. For a positive-power set of three or fewer, the share cap already fails, so the minimum count is the same bound. `commit_possible` calls `power_set_invariant` and does not read `down`. The assumption is that more than two thirds of voting power is online. One validator may be down. Losing more than one third halts the real network because the environment produces no block and the outbox records `PENDING_STALL`. The spec machine does not model that halt. A supplied `CommitBlock` still applies. Scheduling does not read `down`.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:58 reply approves this decision, including the power-share cap and the minimum validator count, and keeps down out of consensus. The 06:59 reply accepts that reading. The assumption is that more than two thirds of voting power is online. Losing more than one third is an environment withhold, not a hashed transition.

### Blocks

Goal 05 and Goal 06 must not put the process failure list in the application hash. The environment withholds a block when more than one third of power is offline. A later design that stores liveness on chain needs a new recorded decision.

## DEC-012

### Question

How should a validator power change that names a block height be ordered against one that names a chain time, when both are still waiting?

### Why this needs the owner

An activation point is either a committed height or a committed chain time. Sorting every height before every time makes a removal at a later height look earlier than an add at a chain time, and the removal is then `ERROR_CODE_NOT_FOUND` for a validator that the add would have created. Ordering the two kinds on one line would choose how many blocks fit in a stretch of chain time. That number is not decided.

### PROPOSED default

The approved choice keeps mixed height and time activations as `ERROR_CODE_CONFLICT`. CONFLICT leaves the proposal `QUEUED`. `ERROR_CODE_FORBIDDEN` marks the proposal `REJECTED`. Changes of one kind are ordered by that kind's point, then by proposal id. A total order across the two kinds was not selected.

### Status

Status: APPROVED

Gate A, 2026-10-31 (America/Toronto), 06:58. Max said "I take all your recommendations".

Gate A, 2026-10-31 (America/Toronto), 06:59. Max said "Yes let's do it".

Reviewer's reading: the 06:58 reply approves this decision, and the 06:59 reply keeps mixed height and time activations as CONFLICT. CONFLICT leaves the proposal QUEUED. FORBIDDEN marks it REJECTED.

### Blocks

Goal 06 must not treat a height and a chain time as an approved single timeline. A total order across the two kinds needs a recorded answer to this question.

## Open product note

The product name is unset. Choosing a name does not by itself change a security boundary, so this register does not request approval of a name and does not block Goal 02 on it. Exact dependency versions are Goal 03 work. They are not approved in this goal. The research-date note that Cosmos SDK 0.55.x was listed with CometBFT 0.40.x, and that CometBFT v1.x was unsupported, is a reminder to recheck. It is not a pin.

## Reversible choices recorded for Goal 01

These are documentation choices. They are not owner approvals.

- Requirement IDs use `REQ-<AREA>-<NNN>`. Decision IDs use `DEC-00N`. Threat IDs use `TM-<NAME>`. Scenario IDs use `S-<NAME>`. Contradiction IDs use `CR-<NNN>`.
- The normative requirements-to-tests matrix is the `requirements-matrix` block in `docs/requirements.md`.
- The decision register is this file. The threat model is `docs/threat-model.md`.
- Blueprint stage names are mapped to the existing 30-goal order. The goal order is not renumbered.
- Lease and content state names from the blueprint diagram stay PROPOSED labels for Goal 02. They are not a wire schema.
- The example chain id `shortvideo-dev-1` stays a PROPOSED example.
- The checker lives at `docs/evidence/goal-01/check_requirements.py`. It is evidence tooling, not application code.
