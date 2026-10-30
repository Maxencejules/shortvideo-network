# Beta requirements, scope, and traceability

Status of this document: PROPOSED specification for the permissioned community beta. This is not an approved protocol. No requirement below is an owner approval. No numeric target is an achieved result or a service level.

Public launch: DISABLED

No tradable token, coin, sale, or mining.

The normative matrix is the `requirements-matrix` block. A `TEST:` entry names the later check assigned to that requirement. It does not mean the check has already run. A `BLOCKER:` entry names an OPEN decision in `docs/decision-register.md`. Goal 01's own check is that every in-scope row has an accountable role, an implementation goal, and either a test assignment or a decision blocker.

Blueprint page numbers are the footer numbers in `docs/blueprint.md`. The PDF remains the source for an ambiguous extraction. Wording here is a traceable specification, not a second copy of the blueprint.

## Decision questions for the owner

These questions are also the register. Each status is OPEN. Each default below is PROPOSED.

| ID | Question the owner is asked |
| --- | --- |
| DEC-001 | Who holds hosting-intent keys, and what account-recovery path is allowed? |
| DEC-002 | Who may operate validators, storage, processing, or account services, and when may anyone be called an independent operator? |
| DEC-003 | What publication rules apply, and what does removal promise to do and to leave in place? |
| DEC-004 | How long are media, backups, logs, moderation files, and coordination records kept, and what is deleted on removal? |
| DEC-005 | Who sponsors upload allowance and lease duration, and what happens when that support ends? |
| DEC-006 | Which software licence applies, and what upload permission must a creator grant? |
| DEC-007 | Who is invited, is the beta adults-only, and which privacy review is required before any public launch? |
| DEC-008 | Who sits on the council, what threshold and delay apply, and what happens if the council loses quorum? |

DEC-008 is an additional security question from blueprint page 28 (initial council members and quorum). The product name is unset and does not block Goal 02.

## PROPOSED defaults

Every item in this list is PROPOSED. None is approved. Implementations may treat the engineering shape as a candidate and must label it as a candidate wherever a user or operator could mistake it for a commitment.

- Responsive web application. No native-app launch is required for the first engineering result.
- Invited accounts. Public sign-up stays off. Public launch stays disabled.
- Four equal-power development validators. They are processes, not a claim of four independent operators.
- Designated storage hosts. A storage operator need not be a validator.
- HTTPS and HLS delivery. Signed provider manifests. No public IPFS and no browser peer sharing in the beta.
- Signed beta attestations. They are not storage proofs, independence proofs, or legal findings.
- Quotas as a mechanism. Per-account numbers are unset (DEC-005).
- No monetary rewards, tradable token, coin, sale, mining, automatic financial slashing, or contribution-credit system.
- Upload bounds of 60 seconds and 100 MB of input, 480p and 720p portrait outputs, no upscaling, and a separate output-byte cap.
- Three copies as a publication parameter, counting failure domains. This is not a durability guarantee.
- Example chain id `shortvideo-dev-1`.
- Council shape of four seats and three approvals with a delay (DEC-008). No people are named.
- Client and service families: React and TypeScript, FastAPI, PostgreSQL, Celery and RabbitMQ, isolated FFmpeg, a Go provider agent, a Go Cosmos SDK and CometBFT chain, Docker Compose and Linux service profiles. Versions are unpinned.
- Licence candidates: AGPLv3 for original server and application code, Apache-2.0 for original protocol client libraries, plus a creator licence for hosting, replication, transcoding, and playback. Compatibility is unconfirmed.
- Host-held delegated signing key for public records, disclosed to the user (DEC-001). A passkey is not a chain key.
- Page 26 measurement gates, including playback, processing, the 100-viewer and 10-upload design point, and a five-minute delist figure.
- Capacity arithmetic in the blueprint, including a 25 MB package illustration and a CAD planning preference. Those figures are not a budget.

## Repository inventory

Inspected tree: companion-pack documents on the verified Goal 00 revision, plus this specification. There is no application.

### Working code

There is no working application code. There is no chain binary, genesis that can start a network, web client, API service, indexer, broadcaster, provider agent, media worker, or end-to-end test suite. Goal 00's Python files are document checks for the companion pack. The Goal 01 checker is a document check. Goal 02's protocol fixtures under `docs/protocol/` and `docs/evidence/goal-02/` are specification checks. None of them is the product.

### Placeholders

- `docs/goals/01-*.md` through `docs/goals/30-*.md` are exact playbook prompt slices. They are instructions, not implementations.
- Blueprint command names such as a devnet target are interfaces to build later. They do not exist in this tree.
- Progress rows for goals that are not started stay `NOT STARTED` in `docs/progress.md`. The machine-readable tracker has a row only for a goal that has left that status.
- The monorepo directories named in blueprint page 27 are absent: `apps/web`, `services/api`, `services/indexer`, `services/broadcaster`, `chain/app`, `chain/x/*`, `proto`, `agents/provider`, `workers/media`, `packages/protocol`, `deployments`, and `tests`.

### Missing components

The entire running system is missing: identity, upload and quarantine, transcoding, manifests, HTTPS playback, the six chain modules, the four-validator devnet, the provider agent, replication and repair, feeds, moderation, operator onboarding, observability, backups, packaging, load evidence, adversarial tests, and independent installation. Secrets, media bytes, and database dumps are not part of the specification and must stay out of Git.

## Beta definition

The beta this repository is building is a permissioned community network with open-source software. It is not a permissionless network, and it is not a public launch.

Included now as engineering scope, still subject to the OPEN decisions:

- A responsive web application for viewers and creators.
- Invited accounts. Viewers do not need a wallet, a cryptocurrency balance, or a duty to host other people's video.
- Four equal-power development validators on a real Cosmos SDK and CometBFT application chain.
- Designated storage hosts, HTTPS delivery, and signed attestations.
- Quotas and explicit sponsorship once DEC-005 is answered. The mechanism is in scope. The numbers are not.
- Distinct creator stages: Uploading, Processing, Review pending, Replicating, and Published.
- Follows, comments, search, reports, blocking, recovery, and lease repair as community-beta scope in later goals.
- Installable packages, backup drills, and measured gates as later goals. Those goals do not turn public launch on.

Explicitly outside this release, without being deleted from the record:

- Public sign-up, monetary rewards, production assurances, and a profitability or scale promise.
- Open validator admission, unsupervised rewards, live video, and a commercial music library.
- Native mobile clients, optional peer assistance, feed plugins, deeper portability, and reviewed storage proofs.
- An EVM, a general smart-contract runtime, a bridge, IBC, a new consensus algorithm, and a video-inside-every-block design.
- A tradable token, a coin, a sale, mining, and automatic financial slashing.

Published means the moderation and hosting conditions then in force are satisfied. It does not mean a blockchain certified the video as lawful or truthful. Three copies do not mean a durability guarantee. A devnet on one machine does not mean independent operators.

## Trust boundaries

The six boundaries stay separate. Goal 02 schemas and later code have to preserve the separation.

| Boundary | Requirement | Accountable role | Goal |
| --- | --- | --- | --- |
| User login | REQ-BOUND-001 | product/application | 19 |
| Social records | REQ-BOUND-002 | product/application | 19 |
| Media transport | REQ-BOUND-003 | media/provider agents | 15 |
| Storage commitments | REQ-BOUND-004 | chain/security | 08 |
| Governance | REQ-BOUND-005 | chain/security | 10 |
| Consensus | REQ-BOUND-006 | chain/security | 05 |

Off-chain, including out of chain transactions and state: video bytes (REQ-OFF-001); usernames tied to private details; email addresses; recovery secrets; individual watch events; complaint narratives; private messages; and legal evidence files (REQ-OFF-002). An identifier or a hash can still be linkable (REQ-OFF-004). Personal viewing histories stay off the public ledger (REQ-OFF-005).

## Accountable roles

The blueprint names five functions. They are unstaffed. Naming them here assigns responsibility for the requirement. It does not appoint a person, a date, or a backup rotation.

- product/application
- chain/security
- media/provider agents
- deployment/on-call
- trust/safety

## Contradictions

Each item states a tension in the blueprint or between the blueprint and the goal sequence, then the resolution used by this specification. The resolution does not add a deferred feature to the beta.

## CR-001

The blueprint tells the community beta to seek four genuinely independent operators. It also allows a four-validator devnet on one workstation. This goal's beta definition says four equal-power development validators.

### Resolution

The devnet uses four equal-power processes and is described that way. It is not described as four independent operators. Tests that need a separate person and machine are listed in `docs/progress.md`. The devnet stays in scope. An independence claim is not added.

## CR-002

Blueprint stage 1 builds a media slice before the custom chain. The goal sequence writes chain modules before the encoder. Goal 18 joins the network before goal 19 implements accounts. Goal 17 replicates before goal 23 implements removal.

### Resolution

Implementation order is the goal sequence. Blueprint stages are the milestone map in the progress tracker. The manifest schema is Goal 02, before lease accounting in Goal 08. Encoding is Goal 14, before playback in Goal 15 and the joined path in Goal 18. Goal 18 may use explicit development principals. Accounts remain Goal 19 and stay inside the beta. Replication before a removal implementation is limited by CR-012. Stage-6 features stay deferred.

## CR-003

The blueprint leaves a token decision for later and also excludes a tradable token and monetary rewards from the beta.

### Resolution

The beta excludes a tradable token, a coin, a sale, mining, automatic financial slashing, and contribution credits. The token decision stays deferred. No goal in this program adds one. Resource control is quotas and sponsorship under DEC-005.

## CR-004

Three stored copies can be read as a durability promise. The blueprint calls them a beta test parameter.

### Resolution

Three copies are a PROPOSED publication parameter. Failure domains count as well as node identities. The parameter is not a durability guarantee and is not proof of independent operators.

## CR-005

The blueprint suggests that the account host hold a delegated signing key in the beta, and it also describes a later user-controlled root identity.

### Resolution

Host custody is the PROPOSED default under DEC-001 and remains OPEN. User-controlled root identity stays deferred. Goal 02 must not freeze the account host as the owner of hosting-intent signatures. Viewer login stays separate from chain signing.

## CR-006

A 3-of-4 council could be implemented with Cosmos Enterprise Groups. Those modules have a licensing boundary the blueprint says not to cross silently.

### Resolution

Enterprise PoA and Groups stay excluded. A minimal custom authority module is PROPOSED and unreviewed. The 3-of-4 shape, the delay, and the named seats are DEC-008 and are not approved parameters.

## CR-007

Removal can be read as universal deletion. The blueprint says independent copies and historical ledger records remain.

### Resolution

Removal delists cooperating services, ends participating commitments, purges controlled caches, and revokes credentials. It does not add a protocol for erasing independent copies or rewriting ledger history. The detailed promise waits on DEC-003 and DEC-004.

## CR-008

A chain stall could be treated as a reason to hide every video, or as a reason to stamp local database rows as final commitments.

### Resolution

New final commitments pause when commits stop. In-term playback from reachable hosts may continue, and the client discloses stale chain status. Applications do not override finality, invent an on-chain renewal, or reset genesis to recover.

## CR-009

The blueprint discusses IPFS, WebRTC peer assistance, and cryptographic storage proofs. The first beta is signed manifests and HTTPS.

### Resolution

Beta delivery is HTTPS and signed provider manifests. Public IPFS, browser peer sharing, WebRTC, and cryptographic proofs stay deferred. The beta does not enable them and does not claim their properties.

## CR-010

Page 26 prints numeric gates that can be mistaken for promised service levels or for results.

### Resolution

Those numbers stay PROPOSED gates for the goals that measure them. They are not service levels, not measurements, and not a launch authorization. Public launch stays disabled.

## CR-011

Viewer sessions, provider keys, broadcaster keys, validator keys, and governance keys can be collapsed into one "account".

### Resolution

Viewers authenticate to the application. Operators hold chain-submission credentials. A web passkey is not a Cosmos key. Provider signatures are not service signatures. Recovery of each class sits in DEC-001 and stays open.

## CR-012

The blueprint says removal policy precedes public replication. The goal sequence implements replication in Goal 17 and removal in Goal 23.

### Resolution

This specification records removal limits before replication work. When Goal 17 is built, its runs are development-network repair. They are not public replication. DEC-003 stays OPEN, and public launch stays disabled, so the goal order does not authorize a public replica.

## CR-013

The prototype column includes login. Goal 18 runs before the account goal and is specified to use development principals.

### Resolution

The joined milestone may authenticate explicit invited development principals. Login, recovery, and export for invited accounts remain Goal 19 inside the community beta. Goal 13 does not become the production account system, and login is not removed from the beta.

## Specification scenarios

These walks are specification scenarios. They were not executed on a chain or a media stack. Each one names the requirements it uses and the decisions that remain open. A polished walkthrough does not approve the protocol.

## S-PLAYBACK

Upload to playback.

An invited creator authenticates to the account service. That step is login, not a chain signature (REQ-BOUND-001). The client requests an upload intent. The service checks invitation and quota before it accepts bytes (REQ-MEDIA-001). The file lands in quarantine under a generated name and is probed by an isolated worker (REQ-MEDIA-002, REQ-MEDIA-003). A worker crash leaves the video unpublished (REQ-MEDIA-007). The manifest, with rendition and segment hashes, stays off-chain. The chain may receive the digest and the byte count (REQ-HOST-001, REQ-OFF-001). `RegisterContent` creates a pending coordination record and no hosting claim (REQ-TX-002). A scheduler may propose hosts. Each provider signs acceptance of the exact digest, byte count, and terms. The service signature is not that acceptance (REQ-CHAIN-012, REQ-TX-003). The application waits for committed execution before it treats the lease as active (REQ-ACK-001). Publication waits for the community approval input and the PROPOSED number of active commitments (REQ-HOST-005). A viewer without a wallet plays HLS over HTTPS from a participating host (REQ-PROD-004, REQ-MEDIA-005). The interface shows the distinct creator stages and uses Published only for the moderation and hosting conditions (REQ-UX-008, REQ-UX-009).

### Expected

Bytes stay off-chain. The digest and the lease are the coordination records. Provider acceptance is a provider signature. The viewer needs no wallet. Stages stay distinct. A broadcast acknowledgement alone does not publish the video.

### Not promised

Durability, legal clearance, truthful-content certification, independent operators, a completed view from one HTTP response, or a reward.

### Requirements

REQ-BOUND-001, REQ-MEDIA-001, REQ-MEDIA-002, REQ-MEDIA-003, REQ-MEDIA-005, REQ-MEDIA-007, REQ-HOST-001, REQ-HOST-005, REQ-OFF-001, REQ-TX-002, REQ-TX-003, REQ-ACK-001, REQ-CHAIN-012, REQ-PROD-004, REQ-UX-008, REQ-UX-009.

### Open decisions

DEC-001 if hosting intent is signed by a host-held key. DEC-003 for the approval rule. DEC-004 for how long the package is kept. DEC-005 for the allowance and the lease term.

## S-ORIGIN

Origin loss.

After the publication conditions hold, the creator disconnects and the origin copy is unavailable. A surviving designated host serves bytes that match the manifest (REQ-QA-003, REQ-SCOPE-015). The client shows hosting status, including a degraded state when the copy count drops (REQ-UX-010). Repair copies from a survivor and does not delete the last reachable copy first (REQ-HOST-006). The chain record is not a substitute for the bytes (REQ-OFF-001).

### Expected

Playback continues from a survivor whose bytes match the manifest. The origin machine is not required to stay online. The interface shows the hosting state.

### Not promised

Survival when every copy shared the failed disk. Survival of a private original whose backup rule was never set. Operator independence from a second process on the same machine (REQ-DEV-002).

### Requirements

REQ-SCOPE-015, REQ-QA-003, REQ-UX-010, REQ-HOST-006, REQ-OFF-001, REQ-DEV-002.

### Open decisions

DEC-004 for the origin backup. DEC-002 for whether the surviving hosts are independent operators.

## S-DEPART

Provider departure.

A provider stops taking new offers, keeps serving accepted work, and either hands that work to a replacement or reaches the agreed real-time deadline (REQ-UX-013, REQ-HOST-006). Renewal or handoff does not silently change the committed bytes (REQ-SEP-001). Capacity reservation is released only on a legal end transition (REQ-TX-005). If the chain is not committing, a local note that the provider "renewed" is not an on-chain renewal (REQ-HOST-007).

### Expected

The leave-network path is visible. The last reachable copy remains until a replacement exists or the deadline is the honest end. Committed bytes stay stable.

### Not promised

A safe departure when no survivor exists. That a stopped container on a shared machine is a distinct operator leaving. That an offline extension during a stall is a committed renewal.

### Requirements

REQ-UX-013, REQ-HOST-006, REQ-SEP-001, REQ-HOST-007, REQ-TX-005.

### Open decisions

DEC-005 for the term and the exit. DEC-002 for who is eligible to replace the departing host.

## S-STALL

Chain stall.

Two of the four equal-power development validators stop, so new commits cease (REQ-CHAIN-005). The interface keeps requested, submitted, committed, and failed distinct, and it shows pending work with an expiry (REQ-PEND-001). A database row alone does not create a storage promise (REQ-ARCH-011). Playback that already met the publication conditions may continue only inside the last real-time terms, with a stale-chain disclosure (REQ-ARCH-012, REQ-OPS-006). Operators do not fabricate blocks or override finality (REQ-REC-001). Council transactions wait on commits as well, so governance can be frozen by the same stall.

### Expected

New content registrations and lease acceptances stay uncommitted. Existing in-term playback can continue from reachable hosts with a visible stale state. History is not rewritten.

### Not promised

Playback after the real-time deadline. Freshness of a gateway cache. A council remedy while commits are halted. A key ceremony for recovery.

### Requirements

REQ-CHAIN-005, REQ-PEND-001, REQ-ARCH-011, REQ-ARCH-012, REQ-OPS-006, REQ-REC-001, REQ-ACK-001.

### Open decisions

DEC-008 because quorum-loss behavior is unspecified and the council cannot commit a membership change during the stall.

## S-REMOVE

Removal.

A moderator acts on a test item after a report (REQ-MOD-001). Cooperating account, discovery, and storage services delist it, end participating leases, purge caches they control, and revoke relevant credentials (REQ-MOD-004, REQ-MEDIA-008). Feeds drop the item. The interface says that copies on machines the operators do not control, and historical coordination records on the ledger, remain (REQ-NG-002, REQ-MOD-009). Validators are not asked to judge the legality of the file (REQ-MOD-003).

### Expected

Controlled surfaces stop offering the item. The limitation on outside copies and ledger history is visible. Complaint files stay off-chain.

### Not promised

Universal deletion. Erasure of the chain's prior coordination record. A five-minute clock. That Published had meant the chain found the video lawful.

### Requirements

REQ-MOD-001, REQ-MOD-003, REQ-MOD-004, REQ-MOD-009, REQ-MEDIA-008, REQ-NG-002, REQ-QA-010.

### Open decisions

DEC-003 for the rule text and the removal promise. DEC-004 for what retained evidence and backups are allowed to keep.

## Requirements-to-tests matrix

Columns are `id`, `scope`, `pages`, `role`, `goals`, `disposition`, and `summary`, separated by tabs. `pages` uses blueprint footer numbers. `goals` lists the implementation goal or goals. `TEST:` assigns a later check. `BLOCKER:` means the requirement stays explicitly blocked on that OPEN decision. `OUT:` means the row is deferred or a non-goal and is not beta implementation work.

```requirements-matrix
id	scope	pages	role	goals	disposition	summary
REQ-PROD-001	IN_SCOPE	3	product/application	20,24,30	TEST: Goal 30 success demonstration shows the viewer application from goal 20, the operator software from goal 24, and the project chain together.	Three-part product: viewer and creator application, operator software, and an open protocol with its own chain.
REQ-PROD-002	IN_SCOPE	3	product/application	27	TEST: Goal 27 documents a replacement path for each mandatory-looking service and rejects a single mandatory operator.	The objective is shared rules without a single mandatory infrastructure owner, while servers, bandwidth, cost, and accountability remain.
REQ-PROD-003	IN_SCOPE	3	product/application	20	TEST: Goal 20 acceptance walk shows watch, follow, block, and report with no wallet, coin balance, or hosting duty.	A normal viewer does not buy cryptocurrency, approve chain transactions, or store other people's videos.
REQ-PROD-004	IN_SCOPE	3	product/application	20	TEST: Goal 20 ships a responsive web client and exercises it in a browser at desktop and narrow widths.	The first client is a responsive web application.
REQ-PROD-005	IN_SCOPE	3	chain/security	06	TEST: Goal 06 admits validators only through the authority module and refuses an open join.	The validator network is small and permissioned.
REQ-PROD-006	IN_SCOPE	3	media/provider agents	16	TEST: Goal 16 runs designated storage agents that must accept a lease before they advertise a package.	Storage for the beta uses designated hosts.
REQ-PROD-007	IN_SCOPE	3	media/provider agents	15	TEST: Goal 15 serves the accepted package over HTTPS and records the URL scheme.	Video delivery for the beta is ordinary HTTPS.
REQ-PROD-008	IN_SCOPE	3	chain/security	05	TEST: Goal 05 module list is limited to coordination records for providers, obligations, evidence, and governance.	The chain is the shared record of provider registration, storage obligations, accepted evidence, and governance actions.
REQ-PROD-009	IN_SCOPE	3	chain/security	05	TEST: Goal 05 builds on a pinned Cosmos SDK and CometBFT and contains no custom consensus algorithm.	Application logic uses Cosmos SDK modules. Consensus uses CometBFT.
REQ-PROD-010	IN_SCOPE	3	chain/security	07	TEST: Goal 07 rejects a transaction that carries video bytes rather than a digest and a length.	Video bytes are not placed inside blocks.
REQ-PROD-011	IN_SCOPE	3	deployment/on-call	30	TEST: Goal 30 asks a second operator to run the software and shows playback after one host is lost.	The success demonstration is a second operator running the software and playback surviving the loss of one host.
REQ-PROD-012	IN_SCOPE	3	chain/security	01	TEST: SELF-CHECK Goal 01 checker requires the specification to say it is not an approved protocol and not a production-security claim.	This specification is a proposal. It is not an audit, a deployed system, or a production-security claim.
REQ-PROD-013	IN_SCOPE	3,8	chain/security	05,10	TEST: Goal 05 and Goal 10 trees contain no tradable token, token sale, exchange listing, mining reward, or fee that pays a holder.	The beta includes no tradable token, token sale, exchange listing, mining, or monetary reward.
REQ-UX-001	IN_SCOPE	4	product/application	22	TEST: Goal 22 shows a chronological Following feed and a separate recommendation feed.	Home offers a chronological Following feed and a separate recommendation feed.
REQ-UX-002	IN_SCOPE	4	product/application	22	TEST: Goal 22 search returns public creators, topics, and videos and hides withdrawn items.	Discover searches public creators, topics, and videos.
REQ-UX-003	IN_SCOPE	4	product/application	21	TEST: Goal 21 covers upload, basic trim, captions, and a visible publication status.	Create supports upload, basic trimming, captions, and publication status.
REQ-UX-004	IN_SCOPE	4	product/application	22	TEST: Goal 22 activity view lists replies and notifications for the signed-in account.	Activity contains replies and notifications.
REQ-UX-005	IN_SCOPE	4	product/application	21	TEST: Goal 21 profile lists the creator's videos and the current hosting status.	Profile shows the account's videos and hosting status.
REQ-UX-006	IN_SCOPE	4	product/application	19	BLOCKER:DEC-001 Recovery and export behavior waits on the custody decision.	Account recovery and export controls are required, and the recovery path is not chosen.
REQ-UX-007	IN_SCOPE	4,17	product/application	19	TEST: Goal 19 defaults saved videos, private preferences, and raw viewing telemetry to private and refuses to publish them.	Saved videos, private preferences, and raw viewing telemetry are private by default.
REQ-UX-008	IN_SCOPE	4	product/application	21	TEST: Goal 21 renders Uploading, Processing, Review pending, Replicating, and Published as separate states.	Creator stages Uploading, Processing, Review pending, Replicating, and Published stay distinct.
REQ-UX-009	IN_SCOPE	4	product/application	21	TEST: Goal 21 Published copy does not say the chain certified legality or truthfulness.	Published means moderation and hosting conditions are satisfied, not that the chain certified legality or truth.
REQ-UX-010	IN_SCOPE	4	product/application	21	TEST: Goal 21 shows Availability degraded and Hosting renewal needed from indexer state.	Availability degraded and Hosting renewal needed are visible states.
REQ-UX-011	IN_SCOPE	4	deployment/on-call	24	TEST: Goal 24 wizard offers storage, processing, account, and validator roles, and storage does not force validator duty.	An operator chooses storage and delivery, processing, account and discovery, or validator. A storage operator need not be a validator.
REQ-UX-012	IN_SCOPE	4	deployment/on-call	24	TEST: Goal 24 dashboard shows disk, leases, jobs, bandwidth, updates, reports, and health from live local state.	The operator dashboard shows disk allocation, leases, job status, bandwidth, software updates, reports, and service health.
REQ-UX-013	IN_SCOPE	4	media/provider agents	16,24	TEST: Goal 16 and Goal 24 exercise capacity limits and an orderly leave-network handoff.	Capacity limits and an orderly leave-network workflow are required.
REQ-UX-014	IN_SCOPE	4	chain/security	02,27	TEST: Goal 02 publishes schemas and vectors, and Goal 27 publishes deployment manifests and an example client.	The developer surface includes source, versioned interfaces, schemas, test vectors, an example client, and deployment manifests.
REQ-UX-015	IN_SCOPE	4	product/application	30	TEST: Goal 30 second client uses documented interfaces and does not claim ActivityPub, AT Protocol, IPFS, or Filecoin compatibility.	Clients can select independently operated services. Similar concepts do not create protocol interoperability.
REQ-UX-016	IN_SCOPE	4	product/application	20	TEST: Goal 20 viewer completes watch, follow, block, and report with peer sharing off and no wallet prompt.	A viewer can watch, follow, block, and report without a wallet or compulsory peer sharing.
REQ-UX-017	IN_SCOPE	4	product/application	21	TEST: Goal 21 creator uploads, reads each status, and submits a removal request.	A creator can upload, understand status, and request removal.
REQ-UX-018	IN_SCOPE	4	trust/safety	24	TEST: Goal 24 install sets a local quota mechanism and completes a hosting handoff on the development network.	A community operator can install independently, set local quota mechanisms, and hand off hosting.
REQ-UX-019	IN_SCOPE	4	media/provider agents	16,25,26	TEST: Goal 16 accepts a bounded lease, Goal 25 shows cost counters, and Goal 26 rotates a service key.	An infrastructure operator accepts bounded commitments, checks work, monitors cost, and rotates service keys.
REQ-UX-020	IN_SCOPE	4	product/application	30	TEST: Goal 30 conformance suite runs a second client against the published interfaces.	A developer can build a second client against documented interfaces and a conformance suite.
REQ-UX-021	IN_SCOPE	4	deployment/on-call	30	TEST: Goal 30 records the separate operator and machine, and rejects a node-count claim as independence evidence.	Independent operation is demonstrated by a separate operator, not inferred from a node count.
REQ-SCOPE-001	IN_SCOPE	5	product/application	19	TEST: Goal 19 rejects an uninvited registration and has no public sign-up endpoint.	The first beta uses invited accounts. Public sign-up is outside the beta.
REQ-SCOPE-002	IN_SCOPE	20,28	product/application	01	TEST: SELF-CHECK Goal 01 checker requires the phrase Public launch: DISABLED in the specification, the register, the threat model, and the progress tracker.	Public launch: DISABLED. The cite is blueprint pages 20 and 28 and playbook goal 01. No goal in this program turns it on.
REQ-SCOPE-003	IN_SCOPE	5	trust/safety	19	BLOCKER:DEC-007 Audience, brand, and age rule are unset.	Initial audience, brand, and any adults-only rule are an open audience decision. A developer community is only an example.
REQ-SCOPE-004	IN_SCOPE	5	trust/safety	21	BLOCKER:DEC-006 The creator permission and upload licence text are unset.	Creators may publish only material they own or have permission to publish, under a licence that is not yet chosen.
REQ-SCOPE-005	IN_SCOPE	5	chain/security	18	TEST: Goal 18 runs login-capable development principals, upload, processed playback, a four-node chain, hosting records, and failure demos.	The prototype path includes login, upload, processed playback, a four-node test chain, hosting records, and failure demonstrations.
REQ-SCOPE-006	IN_SCOPE	5	product/application	01	TEST: SELF-CHECK Goal 01 matrix marks public sign-up, monetary rewards, and production assurances as exclusions.	The prototype excludes public sign-up, monetary rewards, and production assurances.
REQ-SCOPE-007	IN_SCOPE	5	product/application	22,23,19,17,25	TEST: Goals 22, 23, 19, 17, and 25 cover follows, comments, search, reports, blocking, recovery, quotas, and lease repair. A separate operator remains a later test, not a current claim.	The community beta scope adds follows, comments, search, reports, blocking, recovery, quotas, and lease repair, plus an install path a later separate operator can use.
REQ-SCOPE-008	IN_SCOPE	5	product/application	01	TEST: SELF-CHECK Goal 01 keeps open validator admission, unsupervised rewards, live video, and a commercial music library out of scope.	The community beta excludes open validator admission, unsupervised rewards, live video, and a commercial music library.
REQ-SCOPE-009	IN_SCOPE	5	deployment/on-call	10,19,26,27,28	TEST: Goals 27, 26, 10, 28, and 19 produce installers, governance docs, backup drills, measured gates, and exports without enabling public launch.	Version-1 engineering, installable releases, governance docs, drills, measured gates, and exports, stays inside the 30 goals and is not a public launch.
REQ-SCOPE-010	IN_SCOPE	5	product/application	01	TEST: SELF-CHECK Goal 01 progress file promises no scale target and no profitability.	The release carries no promise of platform-scale traffic or profitability.
REQ-SCOPE-011	DEFERRED	5	product/application	none	OUT: Mobile clients, peer assistance, feed plugins, deeper portability, and reviewed proofs stay outside goals 01-30.	Later extensions stay deferred until they are funded, tested, and operationally supported.
REQ-SCOPE-012	IN_SCOPE	5	media/provider agents	13	TEST: Goal 13 rejects an input over the PROPOSED 60-second or 100 MB bound and labels the bound as proposed.	PROPOSED upload bound: 60 seconds and 100 MB of input per video. This is not a capacity claim.
REQ-SCOPE-013	IN_SCOPE	5	trust/safety	25	BLOCKER:DEC-005 The per-account allowance number is unset.	Each account allowance is defined by its community, and that number is not set.
REQ-SCOPE-014	IN_SCOPE	5	media/provider agents	14	TEST: Goal 14 emits only the PROPOSED 480p and 720p ladder, refuses upscaling, and enforces an output-byte cap.	PROPOSED outputs are 480p and 720p portrait-friendly renditions, without upscaling, plus a separate cap on output bytes.
REQ-SCOPE-015	IN_SCOPE	5	media/provider agents	17	TEST: Goal 17 treats three copies as a PROPOSED publication parameter and counts failure domains.	PROPOSED publication parameter: three operator-distinct copies. This is not a durability guarantee. Failure domains count as well as node identities.
REQ-SCOPE-016	IN_SCOPE	5	deployment/on-call	26	BLOCKER:DEC-004 The creator and origin backup duration is unset.	A creator and origin backup policy is separate from the three publication copies, and its retention is unset.
REQ-SCOPE-017	IN_SCOPE	5	product/application	20	TEST: Goal 20 checks captions, keyboard access, reduced motion, mute, and predictable playback controls.	The client provides captions, accessible controls, keyboard navigation, reduced-motion settings, mute, and predictable playback.
REQ-SCOPE-018	IN_SCOPE	5	trust/safety	23	TEST: Goal 23 ships report, block, and a community contact placeholder from the first moderation build.	Reporting, blocking, and community contact information are in the beta from the start.
REQ-SCOPE-019	DEFERRED	5	trust/safety	none	OUT: Store user-generated-content rules apply only if a native store app is shipped, and that launch is deferred.	Apple and Google store rules are deferred with native-store distribution.
REQ-SCOPE-020	IN_SCOPE	5	product/application	01	TEST: SELF-CHECK Goal 01 scope lists no native application as a requirement for the first engineering result.	The first engineering result does not require a native application.
REQ-NG-001	NON_GOAL	5	product/application	none	OUT: Do not build a proprietary-code clone or copy proprietary implementation.	A proprietary-code clone is a non-goal.
REQ-NG-002	NON_GOAL	5	trust/safety	none	OUT: Do not promise universal deletion of independent copies or ledger history.	A promise of universal deletion is a non-goal.
REQ-NG-003	NON_GOAL	5	chain/security	none	OUT: Do not create a financial asset to fund an unmeasured system.	A financial asset used to fund an unmeasured system is a non-goal.
REQ-NG-004	NON_GOAL	5	chain/security	none	OUT: Do not make every viewer a consensus participant.	Making every viewer a consensus participant is a non-goal.
REQ-NG-005	NON_GOAL	3	chain/security	none	OUT: Do not design a new consensus algorithm.	A new consensus algorithm is a non-goal.
REQ-BOUND-001	IN_SCOPE	6,17	product/application	19	TEST: Goal 19 login creates an application session and does not produce a Cosmos signature for a viewer.	User login is separate from chain signing.
REQ-BOUND-002	IN_SCOPE	6,17	product/application	19	TEST: Goal 19 stores ordinary social actions as off-chain signed records, not as chain transactions.	Social records stay off-chain and are separate from consensus.
REQ-BOUND-003	IN_SCOPE	6	media/provider agents	15	TEST: Goal 15 media paths move bytes over HTTPS and do not submit those bytes to consensus.	Media transport is separate from consensus.
REQ-BOUND-004	IN_SCOPE	6,14	chain/security	08	TEST: Goal 08 ACTIVE lease state is distinct from reachability observations and from publication approval.	A storage commitment is distinct from observed reachability and from publication approval.
REQ-BOUND-005	IN_SCOPE	6,12	chain/security	10	TEST: Goal 10 council votes are a different message family from CometBFT consensus votes.	Governance is distinct from consensus voting.
REQ-BOUND-006	IN_SCOPE	6	chain/security	05	TEST: Goal 05 commits only through the admitted validator set under the stated fault assumptions.	Consensus trust is the admitted validator set.
REQ-ARCH-001	IN_SCOPE	6	chain/security	02	TEST: Goal 02 assigns separate authentication and failure behavior to login, social records, media, leases, governance, and consensus.	Each layer has its own authentication, failure handling, and operational limits.
REQ-ARCH-002	IN_SCOPE	6	product/application	20	TEST: Goal 20 client has no required wallet and no required hosting toggle.	The viewer and creator client has no compulsory wallet or hosting duty.
REQ-ARCH-003	IN_SCOPE	6	trust/safety	23	TEST: Goal 23 applies community moderation at the account and discovery services and does not ask validators to edit each video.	A community controls its membership decisions and its moderation. Validators do not own that editorial power.
REQ-ARCH-004	IN_SCOPE	6	media/provider agents	16	TEST: Goal 16 provider agent rejects work it did not sign and keeps that choice on its own machine.	A provider controls its machine and only the obligations it accepts.
REQ-ARCH-005	IN_SCOPE	6	chain/security	06	TEST: Goal 06 validator messages order transactions and cannot set a media owner field.	Validators order valid transactions. They do not own videos.
REQ-ARCH-006	IN_SCOPE	6	chain/security	06	BLOCKER:DEC-008 Council control of the validator set has no approved membership.	The initial council is the control point for the permissioned validator set and protocol parameters, and its membership is unset.
REQ-ARCH-007	IN_SCOPE	6	deployment/on-call	27	TEST: Goal 27 reference profile runs on operator Linux without a mandatory commercial cloud account.	The reference deployment targets operator-owned or independently rented Linux machines. No commercial cloud is mandatory.
REQ-ARCH-008	IN_SCOPE	6	deployment/on-call	27	TEST: Goal 27 documents replacement paths for DNS, identity, media storage, search, and transaction access.	DNS, identity, media storage, search, and transaction access each have a documented replacement path.
REQ-ARCH-009	IN_SCOPE	6,13	product/application	20	TEST: Goal 20 can configure a second endpoint and discloses that the chosen beta gateway is trusted for reads.	A single mandatory gateway is forbidden. Beta read trust in the chosen gateway is disclosed.
REQ-ARCH-010	IN_SCOPE	6	media/provider agents	16	TEST: Goal 16 replicates with signed manifests over HTTPS and has no public IPFS advertise path.	Beta replication uses signed provider manifests and HTTPS, not public IPFS.
REQ-ARCH-011	IN_SCOPE	6	chain/security	18	TEST: Goal 18 stops commits and shows new leases uncommitted while previously approved media can remain reachable.	A chain outage pauses new final commitments and does not require existing videos to disappear.
REQ-ARCH-012	IN_SCOPE	6	product/application	20	TEST: Goal 20 shows a stale-chain label while playing in-term media from the last accepted policy.	Hosts may serve already approved content under the last valid policy until the real-time deadline, and the client discloses stale status.
REQ-TECH-001	IN_SCOPE	7	product/application	03	TEST: Goal 03 records the web client family and pins a version. The PROPOSED React and TypeScript baseline is not pre-approved here.	PROPOSED web client family: React and TypeScript. The version pin belongs to Goal 03.
REQ-TECH-002	IN_SCOPE	7	product/application	03	TEST: Goal 03 records the API family and pins a version. The PROPOSED FastAPI baseline is not pre-approved here.	PROPOSED application-service family: Python and FastAPI.
REQ-TECH-003	IN_SCOPE	7	product/application	03	TEST: Goal 03 records PostgreSQL as the PROPOSED store for private data, social records, jobs, and read models.	PROPOSED database: PostgreSQL for private account data, social records, job state, and read models.
REQ-TECH-004	IN_SCOPE	7	media/provider agents	14	TEST: Goal 14 uses a durable bounded queue. The PROPOSED Celery and RabbitMQ choice is recorded with its pin.	PROPOSED jobs: Celery and RabbitMQ for bounded durable processing.
REQ-TECH-005	IN_SCOPE	7	media/provider agents	14	TEST: Goal 14 runs PROPOSED FFmpeg in an isolated process and records the build.	PROPOSED media tools: FFmpeg in isolated processes for probe, transcode, thumbnails, captions, and package checks.
REQ-TECH-006	IN_SCOPE	7	media/provider agents	16	TEST: Goal 16 implements the PROPOSED Go HTTPS provider agent and pins the Go toolchain.	PROPOSED provider agent: Go and HTTPS for inventory, manifests, leases, replication, and audits.
REQ-TECH-007	IN_SCOPE	7	chain/security	05	TEST: Goal 05 is a Go Cosmos SDK and CometBFT application, not a simulated ledger.	The chain is a Go application using Cosmos SDK and CometBFT.
REQ-TECH-008	IN_SCOPE	7	media/provider agents	15	TEST: Goal 15 delivers HLS over HTTPS. The PROPOSED later WebRTC and TURN path is absent.	PROPOSED delivery is HLS over HTTPS first. WebRTC and TURN stay later work.
REQ-TECH-009	IN_SCOPE	7	deployment/on-call	04	TEST: Goal 04 provides a PROPOSED Docker Compose or equivalent Linux profile and a smaller cloud profile.	PROPOSED deployment tooling: Docker Compose and Linux services for the devnet, node profiles, backups, and diagnostics.
REQ-TECH-010	IN_SCOPE	7	chain/security	03	TEST: Goal 03 pins versions and image digests, writes an SBOM, and records each dependency licence it actually uses.	Pin versions and container digests, generate an SBOM, and record a licence for each dependency. The project licence stays open.
REQ-TECH-011	IN_SCOPE	7	media/provider agents	14	TEST: Goal 14 transcode runs outside the API request process.	Heavy media work stays outside the API request process.
REQ-TECH-012	IN_SCOPE	7	chain/security	03	TEST: Goal 03 rechecks the SDK and CometBFT pair against current docs and does not select CometBFT v1.x on the research-date note alone.	Recheck a compatible SDK and CometBFT family before pinning. The research-date 0.55 and 0.40 note is not a pin, and CometBFT v1.x is not assumed.
REQ-TECH-013	IN_SCOPE	7	chain/security	06	TEST: Goal 06 build excludes Enterprise PoA and Groups. The PROPOSED custom authority module ships with tests and no audit claim.	Exclude Cosmos Enterprise PoA and Groups. A minimal custom authority module is PROPOSED and unreviewed.
REQ-TECH-014	IN_SCOPE	7	product/application	03	TEST: Goal 03 dependency review does not vendor Loops, PeerTube, or Livepeer as drop-in chain components.	Loops, PeerTube, and Livepeer are references. Adapting their code waits on a licence and integration review, and is not assumed.
REQ-CHAIN-001	IN_SCOPE	8	chain/security	05	TEST: Goal 05 genesis has its own chain id, validator set, modules, and upgrade field.	Create an application chain with its own chain id, genesis, validator set, modules, and upgrade policy, reusing the SDK and CometBFT.
REQ-CHAIN-002	IN_SCOPE	8	chain/security	05	TEST: Goal 05 binary has no EVM, general contract runtime, bridge, or IBC module on the beta build.	The first release has no EVM, general smart-contract runtime, bridge, or IBC connection.
REQ-CHAIN-003	IN_SCOPE	8	chain/security	05	TEST: Goal 05 devnet starts four validators with equal voting power and labels them PROPOSED development validators.	PROPOSED devnet: four equal-power development validators.
REQ-CHAIN-004	IN_SCOPE	8	chain/security	05	TEST: Goal 05 status text calls the local set development validators and does not say they are independent operators.	Until separate operators exist, describe actual control. Do not call the devnet four independent operators.
REQ-CHAIN-005	IN_SCOPE	8	chain/security	05	TEST: Goal 05 stops one validator and observes commits, then stops two and shows no new commit presented as final.	With four equal powers, three are a quorum. One faulty validator can leave commits possible. Two unavailable validators stop new commits.
REQ-CHAIN-006	IN_SCOPE	8	chain/security	06	TEST: Goal 06 removes staking and MsgCreateValidator paths that can overwrite the validator set.	x/authority is the only writer of validator membership and scheduled power. Staking and MsgCreateValidator must not bypass it.
REQ-CHAIN-007	IN_SCOPE	8	chain/security	06	TEST: Goal 06 checks the selected SDK and CometBFT height at which a validator update becomes active.	Validator updates follow the selected CometBFT and SDK lifecycle.
REQ-CHAIN-008	IN_SCOPE	8	chain/security	06	TEST: Goal 06 rejects a chain write from an address that is not an admitted operator or service.	Beta chain writes come only from admitted operator and service accounts.
REQ-CHAIN-009	IN_SCOPE	8	chain/security	06	BLOCKER:DEC-002 The admitted operator list and the independence test are unset.	Who is admitted, and what evidence counts as an independent operator, is an open decision.
REQ-CHAIN-010	IN_SCOPE	8	chain/security	06	TEST: Goal 06 enforces signatures, sequences, authorization, size bounds, gas, and write quotas, and shows a zero-fee transaction still metered.	Chain writes require signatures, sequences, authorization, bounded size, gas, and deterministic write quotas. Zero fees are not unlimited computation.
REQ-CHAIN-011	IN_SCOPE	8	chain/security	05	TEST: Goal 05 delivers an invalid transaction directly to execution and shows the same rejection the mempool would give.	Admission rules run during execution, not only in the mempool.
REQ-CHAIN-012	IN_SCOPE	8	chain/security	08	TEST: Goal 08 rejects a service key presented as the provider acceptance signature.	A service submits the creator's authorized hosting intent. The provider signs acceptance. Those signatures are not interchangeable.
REQ-CHAIN-013	IN_SCOPE	8	chain/security	11	TEST: Goal 11 broadcaster fixture cannot mint a provider signature or skip the publication permission bit.	The broadcaster is not a relayer that can manufacture provider acceptance or bypass publication permissions.
REQ-CHAIN-014	IN_SCOPE	8	chain/security	01	TEST: SELF-CHECK Goal 01 text forbids the words trustless and permissionless as labels for this beta.	The permissioned beta is not described as trustless or permissionless.
REQ-CHAIN-015	IN_SCOPE	8	chain/security	10	TEST: Goal 10 has no automatic financial slashing message.	The beta has no automatic financial slashing.
REQ-DEFER-001	DEFERRED	8	chain/security	none	OUT: Permissionless admission needs a separate Sybil and economic design and is not a beta goal.	Permissionless validator or provider admission is deferred.
REQ-MODU-001	IN_SCOPE	9	chain/security	06	TEST: Goal 06 implements x/authority membership, council records, scheduled changes, and allowed submitters.	x/authority holds validator membership, council membership, scheduled changes, and allowed submitters.
REQ-MODU-002	IN_SCOPE	9	chain/security	07	TEST: Goal 07 implements provider id, signing key, manifest digest, capabilities, status, and admitted group.	x/providers holds provider identity, signing key, endpoint-manifest digest, capabilities, status, and admitted group.
REQ-MODU-003	IN_SCOPE	9	chain/security	07	TEST: Goal 07 content record stores an opaque id, manifest digest, byte count, submitting host, and coordination status.	x/content holds an opaque video id, manifest digest, package byte count, submitting host, and coordination status.
REQ-MODU-004	IN_SCOPE	9	chain/security	08	TEST: Goal 08 lease record stores the video and provider pair, terms digest, deadline, reservation, and status.	x/leases holds the video and provider pair, terms digest, start, deadline, capacity reservation, and status.
REQ-MODU-005	IN_SCOPE	9	chain/security	09	TEST: Goal 09 evidence record stores mode, attestation digest, signers, and result, and rejects a proof-of-storage label.	x/evidence holds attestation mode, digests, signers, challenge identity, round, and result.
REQ-MODU-006	IN_SCOPE	9	chain/security	10	TEST: Goal 10 policy module stores resource bounds and delayed parameter changes.	x/policy holds network resource bounds and delayed configuration changes.
REQ-OFF-001	IN_SCOPE	9	chain/security	07	TEST: Goal 07 state and transaction schemas reject video bytes and accept only a digest and a length.	Video bytes stay off-chain.
REQ-OFF-002	IN_SCOPE	9	trust/safety	02,19	TEST: Goal 19 and Goal 02 schemas reject email, recovery secrets, watch events, complaint text, private messages, and legal files on chain.	Usernames tied to private details, email, recovery secrets, watch events, complaint narratives, private messages, and legal evidence stay off-chain.
REQ-OFF-003	IN_SCOPE	9	media/provider agents	16	TEST: Goal 16 endpoint URLs live in a signed off-chain manifest. The chain stores a digest.	Endpoint details prefer a signed off-chain manifest.
REQ-OFF-004	IN_SCOPE	9	trust/safety	02	TEST: Goal 02 privacy notes state that an opaque id or a hash can remain linkable.	An opaque identifier or a hash can still be linkable. Hashing is not anonymization.
REQ-OFF-005	IN_SCOPE	5,9	trust/safety	22	TEST: Goal 22 feed and telemetry schemas keep personal viewing histories off the public ledger.	Personal viewing histories stay off the public ledger.
REQ-IDX-001	IN_SCOPE	9	chain/security	08	TEST: Goal 08 maintains content, provider, and deadline indexes, paginates queries, and bounds work per block.	Stable ids index content to active leases, providers to commitments, and deadlines to expiring leases. Queries are paginated and blocks do not sweep the catalogue.
REQ-SEP-001	IN_SCOPE	9	chain/security	08	TEST: Goal 08 renewal keeps the committed digest and byte count, and a new encode uses a new content version.	A manifest is immutable for a content version. Hosting, terms, and publication status are mutable. Renewal does not change committed bytes.
REQ-TX-001	IN_SCOPE	10	chain/security	02	TEST: Goal 02 lists signer, authorization, replay protection, bounds, and a deterministic transition for every state-changing message.	Every state-changing message has a named signer, authorization, replay protection, input bounds, and a deterministic transition. Transaction data is hostile.
REQ-TX-002	IN_SCOPE	10	chain/security	07	TEST: Goal 07 RegisterContent requires an admitted host, accepted user intent, a valid digest, a byte limit, and an unused version, and creates no hosting claim.	RegisterContent checks the host, the user intent, the digest, the byte limit, and version uniqueness, then creates a pending record.
REQ-TX-003	IN_SCOPE	10	chain/security	08	TEST: Goal 08 offer and accept require eligible provider, provider signature, unexpired offer, and capacity, then reserve bytes.	Offer and AcceptLease check content, provider eligibility, provider signature, offer expiry, and declared capacity, then reserve bytes.
REQ-TX-004	IN_SCOPE	10	chain/security	09	TEST: Goal 09 SubmitAuditBatch checks auditor, signature, epoch, schema, and evidence id, and stores an attestation.	SubmitAuditBatch records an attestation. It does not record an unqualified storage proof.
REQ-TX-005	IN_SCOPE	10	chain/security	08	TEST: Goal 08 renew and end require the authorized party, a legal state, and a monotonic term version.	Renew and EndLease extend an obligation or close it and release the reservation.
REQ-TX-006	IN_SCOPE	10	chain/security	10	BLOCKER:DEC-008 Governance threshold, expiry, and delay have no approved values.	Governance actions need unique approvals, a threshold, an expiry, a delay, and a valid resulting set. Those parameters are unset.
REQ-INV-001	IN_SCOPE	10	chain/security	08	TEST: Goal 08 rejects a second acceptance by the same provider for the same content version.	One provider cannot count two replicas of the same content version.
REQ-INV-002	IN_SCOPE	10	chain/security	08	TEST: Goal 08 property test rejects a reservation above declared admitted capacity.	Reserved bytes cannot exceed declared admitted capacity.
REQ-INV-003	IN_SCOPE	10	chain/security	08,10	TEST: Goal 08 and Goal 10 replay the same lease and governance business id and keep a single transition.	Lease and governance transitions cannot be replayed.
REQ-INV-004	IN_SCOPE	10	chain/security	08	TEST: Goal 08 accounting oracle checks underflow and overflow on reserve and release.	Byte counters do not underflow or overflow.
REQ-INV-005	IN_SCOPE	10	chain/security	09	TEST: Goal 09 rejects an expired attestation as a renewal of current status.	Expired evidence cannot renew current status.
REQ-INV-006	IN_SCOPE	10	chain/security	07	TEST: Goal 07 withdrawal blocks a new sponsored lease and blocks a silent revival.	Withdrawal stops new sponsored leases.
REQ-INV-007	IN_SCOPE	10	chain/security	09	TEST: Goal 09 docs and UI strings say accounting success is not physical capacity or operator independence.	Accounting invariants do not establish that physical capacity or operator independence is true.
REQ-DET-001	IN_SCOPE	10	chain/security	05	TEST: Goal 05 review and tests show consensus code does not fetch video, call the network, run FFmpeg, read the wall clock, score with floats, or range an unordered map into state.	Consensus execution uses committed inputs, integers, explicit sorting, and bounded work.
REQ-ACK-001	IN_SCOPE	10	chain/security	11	TEST: Goal 11 keeps a broadcast acknowledgement in the submitted state until inclusion and execution succeed, and reconciles a timeout before retry.	A broadcast or mempool acknowledgement is not committed success. Persist the transaction hash, message id, and result.
REQ-GEN-001	IN_SCOPE	11	chain/security	05	TEST: Goal 05 records which tutorial modules were removed, including x/mint, x/distribution, and x/slashing, and shows they are absent from the app wiring.	Start from an official example, then remove unused modules. A tutorial chain is not the architecture. Default mint, distribution, and slashing modules are not part of this beta.
REQ-GEN-002	IN_SCOPE	11	chain/security	05	TEST: Goal 05 genesis validation requires the field inventory and rejects a file that contains a private key.	Genesis specifies chain id, time, validator keys and powers, accounts, council parameters, admitted accounts, empty content and leases, limits, and protocol versions, with no private keys.
REQ-GEN-003	IN_SCOPE	11	chain/security	05	TEST: Goal 05 treats shortvideo-dev-1 as a PROPOSED example and uses a fresh chain id per independent test run.	PROPOSED example chain id shortvideo-dev-1 is an example, not an approved network name.
REQ-GEN-004	IN_SCOPE	11	chain/security	05,26	TEST: Goal 26 and Goal 05 keep peer, validator-signing, and governance keys in distinct files and refuse a copied live validator key.	Peer identity, validator signing, and governance keys stay separate. One validator signing key is never copied onto two active nodes.
REQ-GEN-005	IN_SCOPE	11	chain/security	05	TEST: Goal 05 shows equal application hashes, rejection of a foreign message, and restart without a new genesis. A renamed binary is not accepted as the chain.	A created chain is independent processes, one reviewed genesis, agreed application state, only valid project transactions, and consistent restart.
REQ-DEV-001	IN_SCOPE	12	deployment/on-call	05	TEST: Goal 05 launches four validator directories, one index, two app deployments, and three storage agents, and labels one workstation as one failure domain.	The dev topology is four validator processes, one query index, two app deployments, and three storage agents. One workstation is one failure domain.
REQ-DEV-002	IN_SCOPE	12	deployment/on-call	30	TEST: Goal 30 independence evidence names a separate machine and a separate operator, or stays blocked.	Independence tests use separate machines and operators. The dev topology does not satisfy them.
REQ-DEV-003	IN_SCOPE	12	chain/security	18	TEST: Goal 18 registers a manifest, accepts three commitments, replays into the database, matches after restart, and restores without fabricating history.	Devnet demonstrations cover registration, three commitments, event replay, restart equality, quorum loss, and restore without a rewritten history.
REQ-VAL-001	IN_SCOPE	12	chain/security	06	TEST: Goal 06 tests key rotation, removal, duplicate keys, nonpositive power, and a changed quorum at the documented activation height.	A membership change carries a public key, power, activation point, and reason digest, and is tested for duplicates and invalid power.
REQ-UPG-001	IN_SCOPE	12	chain/security	10	TEST: Goal 10 rehearses a migration on a state copy, compares invariants, and shows an old binary failing closed on new state.	Upgrades use a versioned migration, reviewed source, release hashes, and a scheduled activation. An old binary is not assumed to read new state.
REQ-REC-001	IN_SCOPE	12	chain/security	10	TEST: Goal 10 stall runbook has no message that marks a local row as final or that resets genesis.	A stall is recovered by an operator procedure. An application does not override finality.
REQ-REC-002	IN_SCOPE	12	chain/security	26	BLOCKER:DEC-001 Signer backup and replacement procedure waits on the custody decision.	Chain-state and signer-safety backup, and the stop-and-verify rule before a replacement signer, wait on the recovery decision.
REQ-INT-001	IN_SCOPE	13	chain/security	11	TEST: Goal 11 is the only chain writer used by the API, and the web client does not subscribe to every block.	Chain access sits behind the broadcaster or generated client. Read models come from committed events and periodic queries.
REQ-API-001	IN_SCOPE	13	product/application	13,19,21,22,23	TEST: Goals 13, 19, 21, 22, and 23 implement upload intents, upload status, publish, feeds, reports, removal, and exports, and contract tests cover each route.	The client API surface is upload intents, upload status, publish, feeds, reports, removal, and exports.
REQ-API-002	IN_SCOPE	13	media/provider agents	16	TEST: Goal 16 implements a signed capability manifest, offer acceptance, package fetch, transfer receipt, and inventory report.	The provider API is a signed capability and endpoint manifest, offer acceptance, package fetch, transfer receipt, and inventory.
REQ-API-003	IN_SCOPE	13	chain/security	02	TEST: Goal 02 schemas name the chain commands, queries, and events from the blueprint integration table.	Chain commands, queries, and events cover providers, content, leases, evidence, and policy changes.
REQ-IDEM-001	IN_SCOPE	13	chain/security	11	TEST: Goal 11 retries one business id after a timeout and shows a single committed effect.	A transactional outbox records attempts, reconciles unknown submissions, and uses a stable business id.
REQ-IDEM-002	IN_SCOPE	13	chain/security	12	TEST: Goal 12 commits the checkpoint and the projection in one database transaction and rebuilds from the checkpoint.	The indexer checkpoint is atomic with its projection. The projection is a cache, not a ledger.
REQ-GW-001	IN_SCOPE	13	product/application	20	TEST: Goal 20 documents beta read trust and shows that two plain HTTP responses are not treated as a light-client proof.	Users can configure more than one endpoint. The chosen beta gateway is disclosed as trusted for reads.
REQ-PEND-001	IN_SCOPE	13	product/application	21	TEST: Goal 21 separates requested, submitted, committed, and failed, and a stalled chain leaves the operation pending.	The interface separates requested, submitted, committed, and failed. A local database write does not activate a new storage promise.
REQ-HOST-001	IN_SCOPE	14	media/provider agents	15	TEST: Goal 15 manifest lists renditions, segment hashes, byte totals, and format metadata. Hosts verify bytes before advertising.	The manifest lists renditions, segment hashes, byte totals, and format metadata. The full manifest stays off-chain and its digest may go on-chain.
REQ-HOST-002	IN_SCOPE	14	chain/security	02	TEST: Goal 02 records the blueprint state names as PROPOSED labels and assigns numeric or string encodings explicitly.	PROPOSED lease and content state names from the blueprint diagram are labels for Goal 02, not a frozen wire schema.
REQ-HOST-003	IN_SCOPE	14	chain/security	08	TEST: Goal 08 accepts a lease only with the provider signature and does not treat a declared region string as a physical fact.	Providers explicitly accept. The chain checks admissibility and accounting, not the physical truth of a region or an owner.
REQ-HOST-004	IN_SCOPE	14	media/provider agents	07	BLOCKER:DEC-002 The reviewed operator registry has no approved membership rule.	The beta keeps a reviewed operator registry. Membership policy is unset.
REQ-HOST-005	IN_SCOPE	14	trust/safety	17	TEST: Goal 17 withholds publication until the approval input and the PROPOSED active-commitment count are both present, and shows the verification mode.	PROPOSED publication gate: community approval plus the target number of active commitments. The verification mode is displayed separately.
REQ-HOST-006	IN_SCOPE	14	media/provider agents	17	TEST: Goal 17 repair test fails if the last reachable copy is deleted before the replacement verifies.	Repair copies from a survivor and does not drop the last reachable copy first. A voluntary leave stops new offers and hands off.
REQ-HOST-007	IN_SCOPE	14	chain/security	08	TEST: Goal 08 lease terms carry a real-time deadline, and a halted chain does not convert an offline note into a committed renewal.	Host deadlines are real-time. Chain time is committed time. An offline extension is not an on-chain renewal. Hosts show clock uncertainty.
REQ-HOST-008	IN_SCOPE	14	media/provider agents	17	BLOCKER:DEC-004 Retention duration is unset. IPFS pinning is not the beta retention mechanism.	Retention has to be actively supported. Backups, availability, and integrity stay separate concerns.
REQ-EVID-001	IN_SCOPE	15	chain/security	09	TEST: Goal 09 fixtures label receipts and audits as attestations and reject storage-proof, independence, legality, and future-quality labels.	Beta evidence is a signed attestation. It is not proof of storage, operator independence, lawful content, or future service quality.
REQ-EVID-002	IN_SCOPE	15	chain/security	09	TEST: Goal 09 acceptance signature covers digest, bytes, and terms, and the status text calls it a promise.	A provider acceptance signature is a promise, not possession.
REQ-EVID-003	IN_SCOPE	15	chain/security	29	TEST: Goal 29 includes a false auditor attestation and a provider that fails a second requester.	Auditor retrieval checks can be false. A provider may discriminate or fetch bytes from elsewhere.
REQ-EVID-004	IN_SCOPE	15	deployment/on-call	24	TEST: Goal 24 shows operator and failure-domain declarations as claims, and multiple node ids do not flip an independence badge.	Separate replicas are an administrative claim about operators and failure domains. Multiple node ids are not independent copies.
REQ-EVID-005	IN_SCOPE	15	product/application	28	TEST: Goal 28 reports playback startup and errors separately from storage-attestation results.	Playback quality comes from client and probe measurements. A storage check is not a playback guarantee.
REQ-EVID-006	DEFERRED	15	chain/security	none	OUT: Filecoin integration, a local proof scheme, and automatic rewards or penalties stay outside the beta.	Cryptographic storage proofs and automatic rewards or penalties are deferred.
REQ-EVID-007	IN_SCOPE	15	chain/security	09	TEST: Goal 09 negative fixture rejects a Merkle inclusion presented as proof of a unique replica.	A Merkle inclusion is not called proof of a uniquely stored replica.
REQ-MEDIA-001	IN_SCOPE	16	media/provider agents	13	TEST: Goal 13 rejects bytes that arrive before authorization and enforces quota, duration, dimensions, and processing budget.	Authorize an upload before receiving bytes. Enforce quotas, length, duration, dimensions, and processing budget.
REQ-MEDIA-002	IN_SCOPE	16	media/provider agents	13	TEST: Goal 13 stores input under a generated name and rejects a mismatched container despite a claimed MIME type.	Untrusted input stays in quarantine. Validation checks structure, uses least privilege, and ignores a misleading extension.
REQ-MEDIA-003	IN_SCOPE	16	media/provider agents	14	TEST: Goal 14 worker runs unprivileged with CPU, memory, disk, and wall limits, and without unnecessary egress.	The worker is isolated, unprivileged, resource-limited, and has a disposable workspace.
REQ-MEDIA-004	IN_SCOPE	16	media/provider agents	14	TEST: Goal 14 commits the chosen output by digest and does not require a second encoder to match bytes.	The chosen encode is committed by digest. Encoders need not be byte-identical.
REQ-MEDIA-005	IN_SCOPE	16	media/provider agents	15	TEST: Goal 15 serves an HLS master and rendition playlists over HTTPS from more than one host, with bounded retries and a fallback.	Playback is HLS over HTTPS from participating hosts, with bounded retries, integrity checks where the client can make them, and a fallback profile.
REQ-MEDIA-006	IN_SCOPE	16	product/application	20	TEST: Goal 20 bounds prefetch, cancels obsolete requests, and aggregates metrics without raw watch histories.	Prefetch stays bounded. A successful HTTP response is not recorded as a completed watch.
REQ-MEDIA-007	IN_SCOPE	16	media/provider agents	14	TEST: Goal 14 kills a worker mid-job and shows the video unpublished, then retries idempotently.	Jobs are idempotent. A crash does not publish. Abandoned input is garbage-collected. Publication is an atomic rename from staging.
REQ-MEDIA-008	IN_SCOPE	16	trust/safety	23	BLOCKER:DEC-003 Removal propagation to hosts and indexes waits on the removal promise.	Removal reaches approved hosts and indexes. Copies outside operator control remain, and the promise text is unset.
REQ-MEDIA-009	IN_SCOPE	16	media/provider agents	14	BLOCKER:DEC-006 FFmpeg flags, codecs, and licence obligations have no approved record yet.	Record FFmpeg build flags, included codecs, and licensing obligations when a build exists.
REQ-DEFER-003	DEFERRED	16	media/provider agents	none	OUT: A paid processing network needs its own correctness design and is not in the beta.	A paid processing network is deferred.
REQ-ID-001	IN_SCOPE	17	product/application	19	TEST: Goal 19 uses an application session, treats a passkey as a login factor, and does not give viewers chain-submission keys.	Login uses a reviewed authentication flow. A web passkey is not a Cosmos key. Operators hold beta chain-submission credentials.
REQ-ID-002	IN_SCOPE	17	chain/security	02	TEST: Goal 02 public-record envelope specifies schema version, author id, record id, sequence, previous digest, key id, payload, and signature.	Stable account ids are the primary key. The public-record envelope is versioned and signed. Private classes stay private.
REQ-ID-003	IN_SCOPE	17	chain/security	02	TEST: Goal 02 publishes valid and invalid cross-language canonicalization vectors and states that RFC 8785 is a candidate.	Off-chain JSON signatures use a vetted canonicalization profile and safe integer handling. RFC 8785 is not pre-selected as an approval.
REQ-ID-004	IN_SCOPE	17	product/application	19	BLOCKER:DEC-001 The PROPOSED host-held delegated signing key is not approved.	PROPOSED beta custody is a disclosed host-held delegated signing key. It is not approved, and portability claims wait.
REQ-ID-005	DEFERRED	17	product/application	none	OUT: A user-controlled root identity that delegates to replaceable hosts is later portability work.	Deeper user-controlled root identity is deferred.
REQ-ID-006	NON_GOAL	17	product/application	none	OUT: AT Protocol is a reference pattern and is not automatic compatibility.	Automatic AT Protocol compatibility is a non-goal.
REQ-ID-007	IN_SCOPE	17	product/application	19	BLOCKER:DEC-001 Strong confirmation for recovery-key changes, session revocation, and host migration waits on the recovery decision.	Recovery-key changes, session revocation, and host migration need a strong confirmation and a secret-free key history.
REQ-ID-008	IN_SCOPE	17	trust/safety	19	BLOCKER:DEC-004 Account deletion and export redaction wait on the retention rule.	Account deletion revokes future actions and starts removal under the retention rule. Exports omit other people's private data and confidential moderation files.
REQ-ID-009	IN_SCOPE	17	product/application	19	TEST: Goal 19 authorization tests show a chain inclusion proof is not enough to read a private row.	Ledger inclusion is not permission to read private application data. Off-chain entities keep per-object authorization.
REQ-FEED-001	IN_SCOPE	18	product/application	22	TEST: Goal 22 feed payload carries stable content ids, display metadata, and candidate playback locations, and can be rebuilt.	Discovery is replaceable. Indexes rebuild from permitted records and hosting state.
REQ-FEED-002	IN_SCOPE	18	product/application	22	TEST: Goal 22 ranks from follows, topics, and recent approved uploads, keeps a chronological Following feed, and offers Not interested.	Recommendations use explicit rules, filter blocks and withdrawals, and do not optimize only for watch time. Exact anti-abuse thresholds stay unpublished.
REQ-FEED-003	DEFERRED	18	product/application	none	OUT: Optional WebRTC peer assistance and TURN wait until HTTPS targets exist, and those targets are unmet.	Peer assistance, TURN, and public IPFS advertising are deferred.
REQ-FEED-004	IN_SCOPE	18	product/application	20	TEST: Goal 20 build has no browser peer-sharing switch and no public IPFS advertise path, including for private content.	The beta does not enable browser peer sharing or public IPFS advertising.
REQ-SEC-001	IN_SCOPE	19	media/provider agents	14	TEST: Goal 14 runs a malformed corpus and a resource-exhaustion case against the isolated worker.	Hostile uploads meet isolation, quotas, format validation, and bounded CPU, disk, and time.
REQ-SEC-002	IN_SCOPE	19	chain/security	06	TEST: Goal 06 key files and authorization tests keep service, validator, and governance keys distinct.	Service, validator, and governance keys are separate and least-privilege.
REQ-SEC-003	IN_SCOPE	19	chain/security	26	BLOCKER:DEC-001 The compromise recovery drill waits on the custody and rotation decision.	Key compromise response includes rotation and a controlled recovery drill, which is not yet specified.
REQ-SEC-004	IN_SCOPE	19	chain/security	06	TEST: Goal 06 flood and oversized-message tests reject predictably and bound state growth.	Chain spam and state bloat are limited by admitted submitters, bounded fields, gas, quotas, and paginated processing.
REQ-SEC-005	IN_SCOPE	19	chain/security	29	TEST: Goal 29 runs outsourcing, multiple identities, and false-receipt cases with rewards disabled.	Colluding providers are constrained by review, diverse hosting claims, external retrieval checks, and the absence of unreviewed rewards.
REQ-SEC-006	IN_SCOPE	19	product/application	12	TEST: Goal 12 rejects a forged event and a stale projection that disagrees with a chain query.	Gateway and index tampering is met with signed records, verified digests, alternate endpoints, and explicit read trust.
REQ-SEC-007	IN_SCOPE	19	chain/security	29	TEST: Goal 29 partition test produces no fabricated finality.	Validator faults and partitions are tested with protected signing, deterministic transitions, and no fabricated finality.
REQ-SEC-008	IN_SCOPE	19	chain/security	03	TEST: Goal 03 pins dependencies, scans secrets, and isolates CI credentials from untrusted pull requests.	Supply-chain control is pinned dependencies, reviewed builds, isolated CI secrets, and an SBOM when the build exists.
REQ-SEC-009	IN_SCOPE	19	trust/safety	23	TEST: Goal 23 and API tests show titles, comments, captions, and manifests are data, not shell commands, SQL, or model-prompt instructions.	Untrusted text is not interpolated into shell commands, SQL, or model prompts. A future moderation aid cannot change chain policy from user content.
REQ-SEC-010	IN_SCOPE	19	deployment/on-call	25	TEST: Goal 25 rate-limits login, search, upload, report, and discovery, and the fetcher rejects private-network targets by default.	Non-media endpoints are rate-limited. Remote fetches constrain scheme, redirects, and destination. Internal ports stay private.
REQ-SEC-011	IN_SCOPE	19	chain/security	29	TEST: Goal 29 records a review of authorization, authority updates, custody, attestations, and sandboxes, and this specification does not mark that review done.	A security review of authorization, authority updates, custody, proof claims, and sandboxes is a release gate and is unmet.
REQ-SEC-012	IN_SCOPE	19	chain/security	27	TEST: Goal 27 publishes a vulnerability reporting path and a dependency maintenance note.	The project keeps a vulnerability reporting channel and uses maintained dependencies. The SDK policy does not audit custom modules.
REQ-MOD-001	IN_SCOPE	20	trust/safety	23	BLOCKER:DEC-003 Community rules, appeal authority, and the wording of decisions are unset.	Report, block, a queue, reasons, appeals, and urgent escalation are required. The rule text is not approved. Sensitive case files stay off-chain.
REQ-MOD-003	IN_SCOPE	20	trust/safety	23	TEST: Goal 23 applies the same local decision at upload, feeds, comments, and storage admission, and validators have no legality vote.	Policy is applied at the account host, discovery, storage admission, and the client. Operators answer for what they distribute.
REQ-MOD-004	IN_SCOPE	20	trust/safety	23	BLOCKER:DEC-003 The precise removal promise, including what is kept, is unset.	Removal delists, ends participating commitments, purges controlled caches, and revokes credentials. Independent copies and ledger history remain.
REQ-MOD-005	IN_SCOPE	20	trust/safety	25	BLOCKER:DEC-004 The privacy map of purpose, access, retention, and location is unset.	A privacy assessment precedes any public launch. Raw watch histories stay out of public protocols. Telemetry stays minimal.
REQ-MOD-006	IN_SCOPE	20	trust/safety	30	BLOCKER:DEC-007 Jurisdiction-specific privacy advice, including any Canadian or Quebec review, is unmet.	Public launch waits on current privacy advice for the chosen operation. This specification is not that advice.
REQ-MOD-007	IN_SCOPE	20	trust/safety	21	BLOCKER:DEC-006 Original branding and the creator upload licence are unset.	Branding is original. The creator licence must cover hosting, replication, transcoding, and playback. A hash is not ownership.
REQ-MOD-008	DEFERRED	20	trust/safety	none	OUT: Commercial music, payments, and native-store launch stay deferred pending their own rights reviews.	Commercial music, payments, and native-store launch are deferred.
REQ-MOD-009	IN_SCOPE	20	trust/safety	01	TEST: SELF-CHECK Goal 01 states that app-store approval, legal compliance, global deletion, and minor-safe deployment are not claimed.	No app-store approval, legal compliance, global deletion, or minor-safe deployment is claimed.
REQ-GOV-002	IN_SCOPE	21	chain/security	10,23,27	TEST: Goal 10, Goal 23, and Goal 27 docs keep protocol governance, community governance, and maintainer review as three authorities.	Protocol governance, community governance, and repository maintainers are different authorities. None of them owns user content.
REQ-GOV-003	IN_SCOPE	12,21	chain/security	10	BLOCKER:DEC-008 The PROPOSED four-seat, three-approval council is not approved, and no seats are named.	PROPOSED council shape: four seats, three approvals, one approval per seat, an expiry, and a published delay. Council voting is not consensus voting.
REQ-GOV-004	IN_SCOPE	21	chain/security	06	BLOCKER:DEC-008 Pause rights, key replacement, quorum loss, and upgrade choice are unspecified.	Document who pauses admissions, how keys are replaced, what quorum loss does, and how operators choose upgrades. A local emergency disablement is not a chain decision.
REQ-GOV-005	IN_SCOPE	21	chain/security	27	BLOCKER:DEC-006 The PROPOSED AGPL and Apache split is not approved and compatibility is unconfirmed.	PROPOSED licence split: AGPLv3 for original server and application code and Apache-2.0 for original protocol client libraries. Third-party notices stay. Source availability is not itself a licence.
REQ-GOV-006	IN_SCOPE	21	product/application	27	TEST: Goal 27 adds a contribution guide, code of conduct, security reporting, review rules, and a decision log without naming staff or dates.	Contribution rules require tests for protocol changes, migration notes, and reproducible performance evidence. The five roles stay functions.
REQ-GOV-007	IN_SCOPE	21	chain/security	01	TEST: SELF-CHECK Goal 01 calls the beta a permissioned community network and refuses a permissionless label.	The beta is a permissioned community network with open-source software. A permissionless network is a later release.
REQ-OPS-001	IN_SCOPE	22	deployment/on-call	27	TEST: Goal 27 profiles document volumes, secrets, ports, health, backup, and upgrades, and keep transcodes off validator signing hosts.	Deployment profiles cover the dev machine and separate account, validator, storage, and worker profiles.
REQ-OPS-002	IN_SCOPE	22	deployment/on-call	28	TEST: Goal 28 publishes hardware, codec, and concurrency with any benchmark and does not state a universal minimum.	No universal hardware minimum is advertised. One workstation proves neither production capacity nor operator independence.
REQ-OPS-003	IN_SCOPE	22	deployment/on-call	25	TEST: Goal 25 monitors commit age, replicas, processing age, playback errors, and unreviewed reports, and distinguishes quorum loss from index lag.	Monitoring covers chain, storage, media, playback, and trust-and-safety, with a response for each.
REQ-OPS-004	IN_SCOPE	22	deployment/on-call	26	BLOCKER:DEC-004 Backup contents and retention of those backups are unset.	Backups of databases, signed exports, configuration, and critical media are independent of a live replica. Restore is tested with the origin offline once the contents are decided.
REQ-OPS-005	IN_SCOPE	22	chain/security	26	TEST: Goal 26 refuses to start a second live process with the same validator signing key.	A replicated live object is not a historical backup. Two live copies of one validator identity are forbidden.
REQ-OPS-006	IN_SCOPE	22	deployment/on-call	18	TEST: Goal 18 stall shows in-term reads continuing and new final commitments frozen, with a visible disclosure.	During a stall, approved reads and in-term playback may continue and new final commitments freeze.
REQ-OPS-007	IN_SCOPE	22	trust/safety	26	BLOCKER:DEC-005 End-of-service funding and the meaning of an unfunded lease are unset.	Closing a community notifies creators, offers exports, stops new leases, assists migration, and purges controlled data under policy.
REQ-CAP-001	IN_SCOPE	23	deployment/on-call	28	TEST: Goal 28 labels the blueprint viewer and storage table as arithmetic, not as a measured result.	Capacity tables are planning arithmetic. They are not measured demand, performance, or a vendor quote.
REQ-CAP-002	IN_SCOPE	23	deployment/on-call	28	TEST: Goal 28 worksheet separates viewer, replication, repair, prefetch, overhead, and relay bytes, and does not assume a peer share.	Traffic classes stay separate. The beta does not assume peer delivery reduces host traffic.
REQ-CAP-003	IN_SCOPE	23	deployment/on-call	25	TEST: Goal 25 cost sheet separates measured units from operator-supplied rates and marks CAD as a PROPOSED preference.	PROPOSED planning preference is one currency, CAD, and a worksheet. No monthly operating budget is confirmed.
REQ-CAP-004	IN_SCOPE	24	chain/security	28	TEST: Goal 28 reports measured transactions separately from the blueprint's illustrative 800-transaction example.	Coordination-load examples are not chain-throughput claims.
REQ-CAP-005	IN_SCOPE	24	chain/security	02,28	TEST: Goal 28 records transaction bytes, gas, state growth, and catch-up time, and Goal 02 bounds evidence batches.	Measure proof and state costs. Keep attestation batches and query responses bounded.
REQ-CAP-006	IN_SCOPE	24	chain/security	01	TEST: SELF-CHECK Goal 01 excludes contribution credits that could be read as money or as a reward for self-reported storage.	PROPOSED exclusion: no contribution-credit system in the beta. Credits must not be treated as money, an investment, or pay for self-reported storage, views, or identity count.
REQ-CAP-007	DEFERRED	24	chain/security	none	OUT: A tradable token stays behind its own decision gate and is not required to build the chain.	The token decision gate is deferred. The beta does not implement a token.
REQ-CAP-008	IN_SCOPE	24	trust/safety	08	BLOCKER:DEC-005 A named sponsor, a term, and a resource budget are unset, so the sustainability test cannot pass.	Every accepted hosting promise needs a responsible party, a known term, a resource budget, and a renewal or exit path.
REQ-ROAD-001	IN_SCOPE	25	product/application	01	TEST: SELF-CHECK Goal 01 progress file contains no launch date, staffing commitment, or completion date.	Planning uses evidence milestones. No launch date, staffing commitment, or completion date is set.
REQ-ROAD-002	IN_SCOPE	25	product/application	01	TEST: SELF-CHECK Goal 01 progress map ties blueprint stages to goals without renumbering the goal sequence.	The 30-goal sequence is the implementation order. Blueprint stages are evidence milestones.
REQ-ROAD-003	IN_SCOPE	25	product/application	01	TEST: SELF-CHECK Goal 01 records manifest-before-lease, removal-limits-before-public-replication, and custody-before-portability.	The critical path keeps package identity before commitments, invariants before any financial effect, removal limits before public replication, custody before portability claims, and measurements before peer or proof work.
REQ-ROAD-004	IN_SCOPE	25	product/application	01	TEST: SELF-CHECK Goal 01 names the five roles and states that no person is assigned.	Accountable roles are product/application, chain/security, media/provider agents, deployment/on-call, and trust/safety. They are unstaffed.
REQ-QA-001	IN_SCOPE	26	chain/security	05	TEST: Goal 05 replays committed inputs to equal application hashes and rejects forged, unauthorized, and replayed messages.	PROPOSED consensus gate: deterministic replay and rejection of forged, unauthorized, and replayed messages. The gate is not a result.
REQ-QA-002	IN_SCOPE	26	chain/security	05	TEST: Goal 05 quorum test stops one validator, then two, and shows no false success.	PROPOSED quorum gate: stop one of four and keep commits; stop two and show stalled writes without false success.
REQ-QA-003	IN_SCOPE	26	media/provider agents	18	TEST: Goal 18 removes one of three designated hosts and the origin, retrieves from a survivor, and finishes a verified repair.	PROPOSED hosting gate: three designated hosts, remove one and the origin, retrieve and repair from survivors.
REQ-QA-004	IN_SCOPE	26	media/provider agents	15	TEST: Goal 15 corrupts a segment and a manifest and refuses replica acceptance and verified status.	Integrity gate: a corrupt segment or manifest is detected before acceptance or a verified label.
REQ-QA-005	IN_SCOPE	26	product/application	28	TEST: Goal 28 measures p95 startup and rebuffer under the stated network shape and reports sample size. The numbers stay PROPOSED.	PROPOSED playback gate: controlled 20 Mb/s and 50 ms RTT, p95 startup at most 3 seconds, rebuffer ratio at most 1 percent.
REQ-QA-006	IN_SCOPE	26	media/provider agents	28	TEST: Goal 28 times a 60-second supported input to ready-for-review and records hardware and concurrency. The 120-second figure stays PROPOSED.	PROPOSED processing gate: a 60-second supported input reaches ready-for-review within 120 seconds at the declared concurrency.
REQ-QA-007	IN_SCOPE	26	product/application	28	TEST: Goal 28 attempts the PROPOSED design point of 100 simultaneous viewers and 10 upload sessions and reports what the runner actually sustained.	PROPOSED application design point: 100 simultaneous viewers and 10 simultaneous upload sessions. Measure it. Do not assume it.
REQ-QA-008	IN_SCOPE	26	chain/security	11	TEST: Goal 11 repeats one business operation across timeout and restart and shows one transition and no leaked reservation.	Idempotency gate: one final transition and no leaked reservation after retries and restarts.
REQ-QA-009	IN_SCOPE	26	deployment/on-call	26	BLOCKER:DEC-001 Measured RPO and RTO and the signer-safety check wait on the recovery decision.	Recovery gate: restore database, media, and index on clean hosts and record measured recovery points. The policy is unset.
REQ-QA-010	IN_SCOPE	26	trust/safety	23	BLOCKER:DEC-003 The PROPOSED five-minute delist figure is not an approved promise.	PROPOSED moderation gate: controlled-service delisting within 5 minutes, with the outside-copy limitation disclosed. The clock is not approved.
REQ-QA-011	IN_SCOPE	26	deployment/on-call	30	TEST: Goal 30 gives the documented installer to a second operator and records whether private side instructions were required.	Operator-independence gate: a second operator installs without private setup instructions and serves the agreed test catalogue.
REQ-QA-012	IN_SCOPE	26	chain/security	29	TEST: Goal 29 inventory lists unit, property, fuzz, contract, integration, browser, load, and fault-injection evidence that actually ran.	Testing layers include unit, property, fuzz, cross-language contract, integration, browser, load, and controlled fault injection.
REQ-QA-013	IN_SCOPE	26	chain/security	29	TEST: Goal 29 states that passing beta gates is not security against an unrestricted economically motivated network.	Permissionless adversarial coverage is additional scope. A beta pass does not claim it.
REQ-REPO-001	IN_SCOPE	27	chain/security	03	TEST: Goal 03 creates the blueprint monorepo layout and keeps generated code apart from handwritten logic.	The monorepo layout on blueprint page 27 is the Goal 03 target. Those directories are absent today.
REQ-REPO-002	IN_SCOPE	27	chain/security	02	TEST: Goal 02 Protobuf files replace the page 27 lease sketch with typed fields, bounds, units, digests, and domains.	The lease sketch is not a complete wire schema. Goal 02 specifies the real types.
REQ-REPO-003	IN_SCOPE	27	deployment/on-call	03	TEST: Goal 03 adds project targets for build, generation, unit tests, devnet, smoke, fault, and release checks, and records that those targets did not exist in this specification.	Named developer commands are interfaces to create. They do not exist in this tree.
REQ-REPO-004	IN_SCOPE	27	chain/security	01	TEST: SELF-CHECK Goal 01 inventory reports no runnable genesis, production key, or chain binary.	This repository has no runnable genesis, production keys, or chain binary.
REQ-RISK-001	IN_SCOPE	28	chain/security	01	TEST: SELF-CHECK Goal 01 keeps the application chain because the agent contract requires one, and records the value risk without dropping the chain.	The chain stays in scope as a shared-rule record. A comparison with signed records remains a risk note, not a scope cut.
REQ-RISK-002	IN_SCOPE	28	product/application	18	TEST: Goal 18 finishes the media, hosting, and chain path before any native app, token, or heavier recommender work.	Finish the vertical slice before native apps, tokens, or advanced recommenders.
REQ-RISK-003	IN_SCOPE	28	chain/security	09	TEST: Goal 09 and the client copy call evidence an attestation and pair it with an independent retrieval test when one is run.	False storage assurance is avoided by honest labels and retrieval tests. Reviewed proofs stay a separate gate.
REQ-RISK-004	IN_SCOPE	28	deployment/on-call	24	BLOCKER:DEC-002 Actual operator and council control cannot be disclosed until admission is decided.	Concentrated control is disclosed as it is. Gateway and account-host dependency is measured rather than assumed away.
REQ-RISK-005	IN_SCOPE	28	trust/safety	25	BLOCKER:DEC-005 Hard quotas need allowance numbers, and those numbers are unset.	Unfunded hosting growth is bounded by quotas, accepted terms, cost monitoring, and an exit, not by a token.
REQ-RISK-006	IN_SCOPE	28	trust/safety	23	BLOCKER:DEC-003 Moderation operation and the removal validation stay open with the policy decision.	Privacy and abusive-content risk stays open until policy, removal behavior, and jurisdiction-specific advice exist.
REQ-RISK-007	IN_SCOPE	28	chain/security	03	BLOCKER:DEC-006 Licence incompatibility stays open with the project licence and with every dependency review.	Pin and review every dependency. Enterprise-only modules stay excluded unless a licence is explicitly obtained, which this beta does not do.
REQ-RISK-008	IN_SCOPE	28	product/application	01	TEST: SELF-CHECK Goal 01 states that no creator-adoption forecast is assumed.	No creator-adoption forecast is assumed. One invited community with permitted content comes before any broader expansion.
REQ-LAUNCH-001	IN_SCOPE	28	product/application	30	TEST: Goal 30 release decision lists each public-checklist item as met or still blocked, and this specification leaves every item unmet.	The public-release checklist is unmet. Public launch stays disabled.
REQ-SUCCESS-001	IN_SCOPE	28	product/application	30	TEST: Goal 30 runs the invited-creator demonstration: committed obligations, origin disconnect, survivor playback, repair, removal, and a second client.	The final success demonstration is a Goal 30 test, not a result of this document.
```

## Proposed Goal 02 input checklist

Goal 02 can start from Goal 01 because DEC-001 through DEC-008 are explicitly blocked. They are not approved. This checklist is PROPOSED input for that goal.

1. Read `AGENTS.md`, `docs/progress.md`, this file, `docs/threat-model.md`, `docs/decision-register.md`, and `docs/evidence/goal-01.md`. Verify the parent revision and the Goal 01 evidence before editing schemas.
2. Treat DEC-001, DEC-002, DEC-003, DEC-004, DEC-005, DEC-006, DEC-007, and DEC-008 as explicitly blocked. Do not mark any of them approved inside a schema comment, a default, or a fixture label.
3. Keep public launch disabled. Do not add a token, a coin, a sale, mining, or financial slashing.
4. Keep video bytes and the personal-data list off-chain (REQ-OFF-001, REQ-OFF-002, REQ-OFF-005). A hash is not anonymization (REQ-OFF-004).
5. Preserve the six trust boundaries: user login, social records, media transport, storage commitments, governance, and consensus (REQ-BOUND-001 through REQ-BOUND-006).
6. Write `docs/protocol.md` and draft Protobuf for state, transaction, query, and genesis types; API contracts; media manifests; provider capability manifests; and beta evidence envelopes. The lease example on blueprint page 27 is not a complete wire schema (REQ-REPO-002).
7. Specify identifiers, field types, maximum sizes, integer ranges, units, enum handling, digest algorithms, signature domains, and error codes. Leave council threshold numbers and retention durations as fields whose approved values are blocked, not as silent constants presented as policy.
8. Define provider, content, lease, authority, policy, and evidence state machines. The diagram names in REQ-HOST-002 are PROPOSED labels, not a frozen encoding.
9. Separate a committed hosting obligation, observed reachability, and community publication approval (REQ-BOUND-004).
10. State who signs each transition and who may relay it. Provider signatures are not service signatures (REQ-CHAIN-012). A broadcaster must not sign provider acceptance (REQ-CHAIN-013).
11. Specify replay protection and idempotency with a stable business identifier (REQ-IDEM-001, REQ-INV-003).
12. Specify when declared capacity is reserved and released, including the duplicate-replica rule and the ban on underflow and overflow (REQ-INV-001, REQ-INV-002, REQ-INV-004).
13. Specify committed chain time versus real-time host deadlines, pending requests during a stall, and expiry of offers and evidence (REQ-HOST-007, REQ-INV-005). No handler fetches external data or reads a local clock to decide consensus state (REQ-DET-001).
14. Choose a vetted canonicalization for signed off-chain records and publish valid and invalid cross-language vectors (REQ-ID-003). RFC 8785 is a candidate, not an approval. Reuse the SDK transaction envelope for chain messages.
15. Specify version negotiation, key rotation, removal tombstones, privacy classifications, and which fields are retained for reconciliation. Tombstones must not pretend a historical ledger entry was erased. Do not invent a retention period while DEC-004 is open (REQ-MOD-004).
16. Do not freeze host-held custody as the owner of hosting intent while DEC-001 is open (REQ-ID-004). Parameterize the signer role.
17. Label evidence envelopes as attestations (REQ-EVID-001, REQ-TX-004). Do not define them as proof of unique storage (REQ-EVID-007).
18. The contracts need to be able to express the page 26 failure tests: forged, unauthorized, and replayed messages; a two-validator stall without false success; a corrupt manifest; a duplicate reservation; and withdrawn content (REQ-QA-001, REQ-QA-002, REQ-QA-004, REQ-INV-001, REQ-INV-006).
19. Add executable fixture checks where the toolchain exists. If Go, Python, or TypeScript cannot represent a value in that environment, record BLOCKED. Do not treat a missing compiler as a pass.
20. Keep protocol invariants distinct from library choices. Incompatible changes after any later freeze need a recorded decision. This checklist does not freeze protocol v0.

## Public-release checklist

The blueprint's public-release checklist is unmet. Independent installation, recovery drills, signed builds, a reviewed authorization audit, a privacy and rights policy, measured service targets, a budget owner, and named operational coverage are later goals or open decisions. Public launch stays disabled. This specification does not declare the project finished.
