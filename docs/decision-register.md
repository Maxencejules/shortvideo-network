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

## DR-13-01

### Question

What is the minimum session and role boundary for upload routes before Goal 19 supplies enrollment and recovery?

### Why this needs the owner

The goal asks for an approved account and permission contract. None is recorded. Inventing one would decide who can upload, whether a bearer token is enough, and whether a development fixture can be confused with a production session.

### Proposal, not an approval

`cmd/svn-api` resolves a bearer token through a verifier. In development, that verifier is an explicit fixture list, each token prefixed `dev-fixture-`, and only a `creator` may act on a row whose owner is that same account id. The comparison uses the verified account, not a constant. Production refuses the fixture list and refuses to start without a verifier. This goal does not supply that verifier. There is no public sign-up route.

### Status

Status: OPEN. The upload checks in goal 13 exercise this proposal. They are not an approval of it.

## DR-13-02

### Question

Which local or object-storage adapter is approved for private quarantine?

### Why this needs the owner

The goal says to use the approved adapter. The repository does not name one.

### Proposal, not an approval

The process writes to a local directory, mode `0700`, under generated hex names, mode `0600`. No HTTP route reads those files. A transfer token authorizes one upload's write and nothing else.

### Status

Status: OPEN.

## DR-13-03

### Question

What per-account input allowance and processed-output budget should the beta enforce?

### Why this needs the owner

DEC-005 leaves the numbers unset. The per-video ceilings of 100000000 bytes and 60 seconds are the blueprint's proposed bounds, not a measured capacity and not a sponsorship decision.

### Proposal, not an approval

Configuration carries the label `PROPOSED`. The process rejects a higher per-video ceiling. The fixture file's per-account numbers are stand-ins for tests. They are not a sponsor's allowance.

### Status

Status: OPEN. DEC-005 stays the allowance decision.

## DR-13-04

### Question

Is a deadline-bounded `ffprobe` child an acceptable duration and structure check before the isolated worker of Goal 14?

### Why this needs the owner

Client duration, filename, and MIME type are not evidence. A child that parses the container is still not the isolated transcoder, and a container duration can disagree with a full decode.

### Proposal, not an approval

The completion path runs `ffprobe` with a deadline of at most 30 seconds, after an `ftyp` check, and rejects the upload when the probe fails, the duration is above the proposed cap, or a side is above the proposed dimension cap. The child environment does not include the process environment. Goal 14 remains the isolated worker.

### Technical part

Goal 14's worker runs `ffprobe` inside `prlimit` and `unshare --user --map-root-user --net`, with the protocol whitelist `file,crypto,pipe`, before it encodes. The evidence host did run that probe. That technical gap is closed. It does not approve the question, and it does not approve the API process's own probe.

### Status

Status: OPEN. Technical part: CLOSED on the goal 14 evidence host. Approval: OPEN.

## DR-14-01

### Question

Which durable queue should the isolated worker claim?

### Why this needs the owner

The blueprint names Celery and RabbitMQ. `docs/dependencies.md` pins Celery 5.6.3 and says it is not installed. No approved adapter is recorded. Goal 13 already left a durable `QUEUED` row.

### Proposal, not an approval

The worker claims `api.processing_jobs` with PostgreSQL `FOR UPDATE SKIP LOCKED` and a filesystem job file. It does not import Celery and it does not require RabbitMQ. A retry renames the same job file. It does not enqueue a second job.

### Status

Status: OPEN.

## DR-14-02

### Question

Which codecs, containers, and package version rule are approved?

### Why this needs the owner

The v0 media-manifest schema exists and is not an approved package format. `docs/protocol.md` says a second encoding is a new `content_id` until a recorded decision. The goal text says a new encoding creates a new package version.

### Proposal, not an approval

Segments are H.264 in MP4, AAC when the input has audio, one JPEG thumbnail, and WebVTT captions copied as data. `h480` and `h720` are emitted only when that does not upscale. A smaller input stays `native`. A new encode of the same content id writes `content_version + 1`. The manifest digest is the identity. A second encoder is not required to match bytes.

### Status

Status: OPEN.

## DR-14-03

### Question

Is the worker sandbox the approved isolation boundary?

### Why this needs the owner

The goal asks for an unprivileged, resource-limited process with no unnecessary network and a read-only runtime. A tested mechanism is not an approval, and one machine is not an independent operator.

### Proposal, not an approval

FFmpeg and ffprobe run as `prlimit` plus `unshare --user --map-root-user --net`, with a protocol whitelist and a disposable workspace. The evidence host exercised the network namespace, the uid map, and the rlimits. The root filesystem was not remounted read-only, so that part is unverified. An external `bwrap` profile is in `docs/architecture/media-worker.md`. The worker refuses to start when a validator key file is in its root. It does not load a signer key.

### Status

Status: OPEN. Network namespace and rlimits: exercised on the evidence host. Read-only root: UNVERIFIED.

## DR-14-04

### Question

Which processing budgets are approved?

### Why this needs the owner

DEC-005 leaves allowances unset. DR-13-03 is open. The 120-second figure for a 60-second input is a proposed gate.

### Proposal, not an approval

The worker refuses a limits label other than `PROPOSED`. Duration stays at most 60 seconds, a side at most 7680, input at most 100000000 bytes, and package bytes at most the protocol cap 268435456. The proposed child caps are 180 seconds of wall time, 240 seconds of CPU time, and 2 GiB of address space. Output larger than the job's reserved budget is rejected. These numbers are not a measured capacity and not a sponsorship decision.

### Status

Status: OPEN. DEC-005 stays the allowance decision.

## DR-15-01

### Question

How many segments and queued items may a client prefetch?

### Why this needs the owner

The blueprint says prefetch stays bounded. It does not name the window. DEC-005 leaves allowances unset.

### Proposal, not an approval

The harness fetches the current video segment and at most one segment ahead. A scroll prefetch fetches at most one queued item's master playlist and does not fetch that item's media. Changing the selected item cancels the previous request. These numbers are not an allowance.

### Status

Status: OPEN.

## DR-15-02

### Question

May a ready-for-review package be played before publication is approved?

### Why this needs the owner

Goal 14 leaves the package at `READY_FOR_REVIEW` and `NOT_PUBLISHED`. DR-14-02 has not approved the package format. The chain row stays `HELD`. There is no publication route.

### Proposal, not an approval

A local playback grant names one content version and one manifest digest. The API still reports `publication: NOT_PUBLISHED`. The grant is not a chain publication and not a hosting lease. Bytes are served only for that grant, from the package directory, never from quarantine.

### Status

Status: OPEN. DR-14-02 stays open.

## DR-15-03

### Question

How long may a client cache a segment, and what does removal do to that cache?

### Why this needs the owner

DEC-003 and DEC-004 do not set a retention promise. This process can only control caches it issued headers for.

### Proposal, not an approval

Playlists use `private, max-age=0, must-revalidate`. Segments use `private, max-age=60` and an ETag of the segment SHA-256. Removal drops the issued ETags and later requests, including conditional ones, are `404` with `private, no-store`. This does not delete copies outside this process.

### Status

Status: OPEN.

## DR-15-04

### Question

Which host is allowed to serve bytes in this goal?

### Why this needs the owner

Goal 16 is the independent provider. A test process on one machine is not that provider. A public certificate is not available here.

### Proposal, not an approval

The adapter name is `origin-test` at version 1. It binds only to a loopback address and uses a one-day self-signed certificate generated in a temp directory. A second loopback process can be registered as a test replica after its bytes match the origin manifest. The harness tries at most four hosts and at most two attempts on each. No public name is advertised.

### Status

Status: OPEN.

## DR-15-05

### Question

What clock and chain note allow playback during an outage?

### Why this needs the owner

Host deadlines are real time. Chain time is committed time. This service does not query the chain, and it must not turn a local note into a hosting promise.

### Proposal, not an approval

The chain note is operator input: `HELD`, `STALE`, or `UNAVAILABLE`. It starts as `UNAVAILABLE`. A new grant or a new host is refused unless the note is `HELD`. An existing grant keeps serving until its real-time `authorized_until`, at most 24 hours after it was written, and the API reports `chain_status: STALE` and `hosting_promise: false`. The term uses the process clock. It is not chain time.

### Status

Status: OPEN.

## DR-15-06

### Question

Which bytes is the client allowed to call verified?

### Why this needs the owner

The blueprint asks for integrity checks where the client fetch path can make them. A browser that does not demux HLS cannot be described as verifying every native HLS segment.

### Proposal, not an approval

The origin refuses to advertise, and refuses to send, bytes that do not match the manifest. The harness hashes each video segment and caption file it fetches, and it does not hash segments outside its prefetch window. The test page hashes the one media segment it fetches and decodes that MP4 in a video element. That is not native HLS verification. `rendered` stays zero until that decode is observed. An HTTP 200 is not a render and not a completed watch.

### Status

Status: OPEN.

## Reversible choices recorded for Goal 01

These are documentation choices. They are not owner approvals.

- Requirement IDs use `REQ-<AREA>-<NNN>`. Decision IDs use `DEC-00N`. Threat IDs use `TM-<NAME>`. Scenario IDs use `S-<NAME>`. Contradiction IDs use `CR-<NNN>`.
- The normative requirements-to-tests matrix is the `requirements-matrix` block in `docs/requirements.md`.
- The decision register is this file. The threat model is `docs/threat-model.md`.
- Blueprint stage names are mapped to the existing 30-goal order. The goal order is not renumbered.
- Lease and content state names from the blueprint diagram stay PROPOSED labels for Goal 02. They are not a wire schema.
- The example chain id `shortvideo-dev-1` stays a PROPOSED example.
- The checker lives at `docs/evidence/goal-01/check_requirements.py`. It is evidence tooling, not application code.
