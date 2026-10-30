# Decision register

Status of this register: OPEN questions for the owner. Nothing in this file is approved.

Public launch: DISABLED

No tradable token, coin, sale, or mining.

This register records decisions whose answers change security or public-operation commitments. A PROPOSED default is a suggestion so implementation can see a candidate. It is not permission to ship that candidate, and it is not a user approval. Reversible documentation choices made while writing Goal 01 are listed at the end and are not requests for approval.

Goal 02 may proceed only by treating the decisions below as explicitly blocked. It must not treat them as approved.

The protocol specification is not an approved protocol.

## DEC-001

### Question

Who holds the keys that authorize a creator's hosting intent in the beta, and what account-recovery path is allowed?

### Why this needs the owner

The answer decides who can publish, migrate, or take over an account, and whether a compromised account host can sign as the user. That is a custody and recovery commitment. Viewer login is a separate boundary from chain signing, and this decision must not collapse those boundaries.

### PROPOSED default

The account host holds a delegated signing key for beta public records, and the product discloses that custody. A web passkey is a login authenticator and is not a Cosmos transaction key. Email, recovery material, and sessions stay off-chain. Operators, not viewers, hold chain-submission credentials. A later user-controlled root identity stays deferred. Recovery-key changes, session revocation, and host migration require a strong confirmation, and the key history must not expose secrets.

This default is PROPOSED. It is not approved.

### Status

Status: OPEN

### Blocks

Goal 02 must not freeze the account host as the owner of hosting-intent signatures. Goal 19 must not claim portability or a recovery procedure from this default. Goal 26 backup and signer-recovery drills wait on this decision. Devnet keys stay fresh development-only keys and are not a stand-in for this policy.

## DEC-002

### Question

Who may run a validator, storage host, processor, or account service in the beta, and what evidence of distinct control is required before the project describes any operator as independent?

### Why this needs the owner

Admission is the boundary against Sybil identities and undisclosed control. Calling four processes "independent operators" would overstate the trust model. Permissionless admission is a different, later system and is outside this beta.

### PROPOSED default

Admission stays permissioned. A reviewed operator registry is the beta mechanism. Four equal-power development validators are development processes, and the spec describes them as that. A storage operator is not required to become a validator. No permissionless validator or provider admission is included. Multiple node identities are not treated as proof of independent operators or independent copies.

This default is PROPOSED. It is not approved.

### Status

Status: OPEN

### Blocks

The real operator list, any public claim of operator independence, and the Goal 24 walkthrough that requires a separate person. Goal 07 can still define an admission record whose membership policy stays blocked. Goal 30's independent-install demonstration stays blocked until a separate operator and machine exist.

## DEC-003

### Question

What community rules determine whether a video may be published, who decides an appeal, and what a removal operation promises to do and not to do?

### Why this needs the owner

The answer is the public-operation commitment about other people's speech and about deletion. A wrong promise (for example universal deletion, or a blockchain certification of legality) is a security and trust failure even when the software functions.

### PROPOSED default

The product provides report, block, an administrator queue, decisions with reasons, appeals, and an urgent escalation path. Sensitive case material stays off-chain and access-controlled. A signed public label can state a community outcome without naming the complainant. Removal delists the item, ends or cancels participating hosting commitments, purges caches the operators control, and revokes relevant credentials. Independently retained copies and historical public-ledger records remain. Validators do not decide the legality of each video. Published means the moderation and hosting conditions in force are satisfied. It does not mean the chain certified the content as lawful or truthful. The five-minute delisting figure in the blueprint is a PROPOSED gate, not a promise.

This default is PROPOSED. It is not approved.

### Status

Status: OPEN

### Blocks

The rule text, the appeal authority, the removal promise, and any service-level timing. Goal 23 requires this decision before its policy-bearing behavior can be treated as specified. Public replication stays disabled while public launch is disabled.

## DEC-004

### Question

How long are media packages, creator originals, backups, logs, moderation files, and chain-coordination records kept, and what is deleted when a video or an account is removed?

### Why this needs the owner

Retention is the privacy and deletion commitment. It also decides whether a live replica is being treated as a backup, which it is not.

### PROPOSED default

No retention duration is set. The beta does not promise universal deletion. Raw watch histories stay out of public protocols. Complaint narratives, private messages, and legal evidence files stay off-chain. Chain coordination records are not rewritten to imitate erasure. Backups, availability, and integrity are separate. An origin or creator backup policy is unset. IPFS pinning is not the retention mechanism of this beta.

This default is PROPOSED. It is not approved.

### Status

Status: OPEN

### Blocks

Goal 23 deletion behavior, Goal 26 backup scope, and the privacy assessment required before any public launch. Goal 02 may classify fields and define a removal tombstone shape. It must not invent a retention period.

## DEC-005

### Question

Who sponsors an upload allowance and a lease duration, what the limits are, and what the network does when that support ends?

### Why this needs the owner

Sponsorship decides who is responsible for storage cost and what users are told when support stops. A credit, reward, or allowance that behaves like money would contradict the ban on a financial asset. This beta does not add a token to answer the question.

### PROPOSED default

No monetary rewards, no tradable token, no coin, no sale, no mining, no automatic financial slashing, and no contribution-credit system. Hosts are not promised payment by this specification. A future community sponsor would name an allowance and a lease term. Unfunded or expired leases are not presented as permanent availability. Quota mechanisms exist as engineering scope. The per-account number and the lease length are unset. Blueprint arithmetic (100 daily viewers, 25 MB packages, three copies, and similar figures) is planning illustration, not a budget or a measured demand.

This default is PROPOSED. It is not approved.

### Status

Status: OPEN

### Blocks

Numeric account allowances, lease durations, any non-cash credit policy, and any statement that a named sponsor already exists. Goal 08 can still specify that every accepted lease needs a responsible party, a term, a budget, and an exit once those values are decided. Goal 25 enforces quotas without inventing the numbers.

## DEC-006

### Question

Which licence applies to original server and application code, to original protocol client libraries, and to the specification? Separately, what upload licence must a creator grant for hosting, replication, transcoding, and playback, and how are FFmpeg and other dependency licences recorded?

### Why this needs the owner

Software licensing and the creator's rights grant are public-operation commitments. They are different questions. A hash or a chain timestamp does not establish ownership or permission. Combining licences without a compatibility review can make the tree undistributable.

### PROPOSED default

Consider AGPLv3 for original server and application code and Apache-2.0 for original protocol client libraries. Confirm compatibility before applying either. Keep third-party notices. Do not relicense code beyond the rights granted. Do not include Cosmos Enterprise PoA or Groups modules. Record FFmpeg build flags, codecs, and licence obligations when a build exists. The creator upload licence would cover hosting, replication, transcoding, and playback of material the creator has permission to publish. Commercial music, payments, and native-store distribution stay deferred.

This default is PROPOSED. It is not approved. No licence file in this goal applies those terms.

### Status

Status: OPEN

### Blocks

Goal 27 release notices, any vendoring of reference-project code, and the creator-facing rights text. Goal 03 may pin dependency versions and record their licences as discovered facts. It must not treat the project licence split as approved.

## DEC-007

### Question

Who is invited into the beta, is participation limited to adults, what community is this for, and which jurisdiction's privacy duties will be reviewed before any public launch?

### Why this needs the owner

Audience policy decides whether minors are in scope and what safety claim the product is allowed to make. The blueprint states that a safe deployment involving minors cannot be confirmed from the blueprint. Brand and community choice also decide whose rules Goal 23 will eventually enforce. This specification does not give legal advice.

### PROPOSED default

Accounts are invited. Public sign-up stays off. Public launch stays disabled. A developer or learning community is an example, not a chosen market. The product name is unset and is not a security blocker by itself. The beta makes no minor-safety claim. A privacy assessment and jurisdiction-specific advice, including any Canadian or Quebec review, remain unmet gates for public launch. The 2018 OPC overview cited by the blueprint is not used here as a current-law checklist.

This default is PROPOSED. It is not approved.

### Status

Status: OPEN

### Blocks

Public launch, community-rule tone, any age gate, and any marketing or adoption claim. Goal 19 may implement an invitation mechanism without an approved community.

## DEC-008

### Question

Who holds the council seats that can change validator membership and protocol parameters, what approval threshold and delay apply, and what happens when the council loses quorum or loses keys?

### Why this needs the owner

This is who can change the consensus set. The blueprint lists initial council members as a decision to record before implementation. It is an additional security-critical question beside the seven topics named for this goal. Leaving it implicit would hide the control boundary.

### PROPOSED default

Four named seats, three approvals for an ordinary change, one approval per seat, an expiry, and a published delay. Council voting is application governance and is a different process from CometBFT consensus voting. Emergency local disablement of a compromised endpoint is not a committed chain decision. Quorum-loss behavior is unspecified. No person is named. Cosmos Enterprise Groups is not the implementation. A chain stall can prevent on-chain governance from executing.

This default is PROPOSED. It is not approved.

### Status

Status: OPEN

### Blocks

Production council parameters and any genesis that names real council keys. Goal 06 and Goal 10 must not treat 3-of-4 as an approved policy. A devnet may use disposable test keys and may store a threshold field. The number 3-of-4 remains a labeled PROPOSED test parameter until this decision is answered.

A validator power change in the v0 draft names its own activation point: a committed height or a committed chain time strictly after the block that executes the proposal. That is the "not immediate" rule. No minimum delay beyond that rule is chosen. Any longer mandatory delay stays inside this decision.

## DEC-009

### Question

Should `MsgAdmitProvider`, `MsgDisableProvider`, and `MsgForceWithdraw` be removed, or should they stay as a bounded emergency path beside the council proposal?

### Why this needs the owner

Blueprint page 12 describes operator and validator changes as council actions with a 3-of-4 threshold, an expiry, and a delay. This draft has two paths. `AdmitProviderChange`, `DisableProviderChange`, and `ForceWithdrawChange` go through `ProposeChange`, `ApproveChange`, and `ExecuteChange`. The direct messages are single-signature authority paths. They do not collect council approvals.

In this tree, `op_admit_provider` (`docs/evidence/goal-02/spec_machine.py:409`) and `op_force_withdraw` (`spec_machine.py:1808`) check the authority role and the relevant open decision. `op_disable_provider` (`spec_machine.py:1782`) also calls `council_ready` (`spec_machine.py:334`). `council_ready` only checks that a DEC-008 test parameter exists: the numeric value is positive, the delay is not negative, and the required count does not exceed the seat list. It does not count approvals and it does not wait for the delay.

### PROPOSED default

No default is selected. The alternatives are to remove the three direct messages, or to keep them as a bounded emergency path whose scope, expiry, and delay are named by the owner. This draft keeps both the direct messages and the proposal payload path. Keeping the direct messages is not approval of an emergency policy.

### Status

Status: OPEN

### Blocks

Goal 06 and Goal 10 must not treat the direct messages as the approved way to admit, disable, or force-withdraw. Goal 02 does not choose a threshold or an emergency scope.

## DEC-010

### Question

Should evidence state `REJECTED` be derived from verification of the attestation, or is the submitter's `reject` flag an auditor verdict?

### Why this needs the owner

`EvidenceItem.reject` is a bool the auditor submits. The state machine records `REJECTED` when that bool is true and `RECORDED` otherwise. It does not recompute the verdict from hosted bytes or from a second check.

### PROPOSED default

No default is selected. The alternatives are to derive `REJECTED` from a verification rule, or to treat the flag as the auditor's verdict. This draft keeps the flag. Keeping it is not an approval.

### Status

Status: OPEN

### Blocks

Later evidence and moderation work must not describe `reject` as proof that the chain verified the media. A verification rule needs a recorded answer to this question.

## DEC-011

### Question

Is validator liveness, the fixture `down` set, consensus state that belongs in the application hash, or is it only an observation about the current block?

### Why this needs the owner

The block-commit predicate uses `down` to decide whether the current validators can produce a block. Power-change scheduling used to read the same set. Two copies of the same transactions then finished `EXECUTED` or `QUORUM` depending on `down`, and the application hashes differed, while `down` itself was omitted from the hash. That is not a deterministic consensus input.

### PROPOSED default

No default is selected. The alternatives are to put liveness in consensus state and in the application hash, or to keep it as a block-local observation. This draft keeps `down` out of the hash and does not read it when scheduling a power change. Every projected validator counts as available for that check. The block-commit predicate still uses `down` as a fixture. Keeping that split is not an approval.

### Status

Status: OPEN

### Blocks

Goal 05 and Goal 06 must not treat a process failure list as an approved consensus field. A later design that stores liveness on chain needs a recorded answer to this question.

## DEC-012

### Question

How should a validator power change that names a block height be ordered against one that names a chain time, when both are still waiting?

### Why this needs the owner

An activation point is either a committed height or a committed chain time. Sorting every height before every time makes a removal at a later height look earlier than an add at a chain time, and the removal is then `ERROR_CODE_NOT_FOUND` for a validator that the add would have created. Ordering the two kinds on one line would choose how many blocks fit in a stretch of chain time. That number is not decided.

### PROPOSED default

No default is selected. The alternatives are to define one order from committed height and chain time, or to keep the kinds separate. This draft rejects a new power change whose activation kind differs from one already pending, with `ERROR_CODE_CONFLICT`. Changes of one kind are ordered by that kind's point, then by proposal id. Rejecting the mix is not an approval of a cross-kind order.

### Status

Status: OPEN

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
