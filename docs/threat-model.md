# Threat model

Status of this document: PROPOSED threat model for the permissioned community beta. It is not an approved security design, not a penetration test, and not a claim of production security.

Public launch: DISABLED

No tradable token, coin, sale, or mining.

The beta is a permissioned network with invited accounts, four equal-power development validators, designated storage hosts, HTTPS delivery, quotas, and signed attestations. Monetary rewards are excluded. Open decisions in `docs/decision-register.md` stay OPEN. Where a mitigation depends on one of those decisions, the residual limitation says so.

Assets in scope: account sessions; media in quarantine and after processing; manifest digests; hosting commitments; validator and governance keys; service broadcaster keys; provider keys; the application database; operator machines; and the software supply chain.

Assets that must not be created: a tradable token, a coin, a sale, a mining reward, inflation, or an automatic financial slash, including any of those introduced by a default SDK module.

## Actors

- Invited viewer and invited creator. They authenticate to the account service and do not hold chain-submission keys.
- Account-host operator. Holds sessions. A host-held signing key exists only if DEC-001 is later accepted, and it would have to be disclosed.
- Storage provider. Signs its own lease acceptance and controls its own machine.
- Auditor. Signs attestations. An auditor process run by the provider's operator is not an independent auditor.
- Validator operator. Signs consensus messages. Does not own videos and does not decide each community's editorial rules.
- Council member. Would change the validator set and protocol parameters. DEC-008 names no seats yet.
- Gateway and index operator. Serves reads that, in this beta, are trusted only when that trust is disclosed.
- Broadcaster service. Submits admitted transactions and cannot sign a provider acceptance.
- Developer and CI. Can add a dependency, leave a scaffold module wired in, or commit a secret.
- Outsider. Can submit a hostile upload, a forged transaction, or a substituted dependency to any reachable endpoint.

These are roles. No person is assigned.

## Trust boundaries

These boundaries stay separate. A control that is valid in one is not automatically valid in another.

| Boundary | What it trusts | What it does not decide |
| --- | --- | --- |
| User login | The account service's session after invited authentication | Chain inclusion, storage possession, or legality of a video |
| Social records | The account host's off-chain signed records and its stated custody | Consensus order, or a validator's editorial judgment |
| Media transport | HTTPS delivery, quarantine, and the isolated worker | Committed hosting obligations |
| Storage commitments | Provider signatures on exact terms, plus chain accounting | Physical ownership of disks, or that an attestation is true |
| Governance | Council signatures under a future approved parameter set | CometBFT quorum, or ownership of user content |
| Consensus | Admitted validators under CometBFT fault assumptions | Community moderation, or media durability |

A broadcast acknowledgement is not committed execution. A provider signature is not a service signature. An attestation is not a proof of storage. A database projection is not the ledger.

## Independent process and independent operator

An independent process is a separate operating-system process, container, or node identity with its own data directory and keys. Four validator containers on one workstation are four processes. They can exercise restart, distinct peer identities, and the four-validator quorum rules. They share one failure domain. They are one operator unless a different person actually controls another machine.

An independent operator is a distinct person or organization with separate administrative control, credentials, and infrastructure. A second node identity, a second container, a second Unix account, or a script labeled as an operator does not create one. An auditor process started by the same operator as the provider is not an independent auditor. A storage process and a validator process run by the same person are not two independent operators. A storage operator is still not required to be a validator.

Multiple node identities do not prove independent copies. This specification claims no independent operator. Tests that need one are identified in `docs/progress.md` and stay unmet until a separate person and machine participate.

## TM-UPLOAD

Hostile uploads.

An invited creator, or any client that reaches an upload endpoint, submits a file crafted to exhaust CPU, memory, disk, or time, to confuse a parser, or to escape a worker. The file may lie about its extension or declared type.

### Mitigation

Authorize the upload before accepting bytes. Enforce quotas, duration, dimensions, and a processing budget. Store untrusted input in quarantine under a generated name. Check structure rather than the extension or declared MIME type. Run FFmpeg in an isolated unprivileged process with CPU, memory, disk, and wall-time limits, a disposable workspace, no unnecessary outbound network, and a read-only runtime where the toolchain allows it. Keep untrusted transcodes off hosts that hold validator signing keys. Shell, SQL, and prompt injection of titles, captions, and manifests are handled in TM-INJECTION. Requirements: REQ-MEDIA-001, REQ-MEDIA-002, REQ-MEDIA-003, REQ-SEC-001, REQ-SEC-009, REQ-OPS-001.

### Test

Goal 14 malformed-corpus, resource-exhaustion, and sandbox tests. Goal 29 repeats them against the candidate revision. Goal 01 does not run those tests and does not treat this paragraph as a pass.

### Residual limitation

Process isolation reduces the blast radius and does not make decoders safe. A flaw in the kernel or in FFmpeg can cross a process boundary. The numeric account allowance is unset while DEC-005 is OPEN, so the quota mechanism has no approved size. No sandbox effectiveness is claimed.

## TM-UNAUTH-TX

Unauthorized transactions.

A caller forges a message, replays a transition, uses another party's key, or relies on a mempool check that execution does not repeat. The broadcaster submits a provider acceptance the provider did not sign, or bypasses the creator's publication permission. Spam grows state.

### Mitigation

Admit chain writers explicitly. Require a named signer, a signature, an account sequence, object authorization, bounded transaction size, gas, and deterministic write quotas. Enforce those rules during execution, not only in the mempool. Zero fees are not unlimited computation. Keep provider acceptance signatures distinct from service signatures. Use a stable business identifier so a retry with a new transaction hash cannot duplicate a successful action. Cap per-block work and paginate queries. Requirements: REQ-CHAIN-008, REQ-CHAIN-010, REQ-CHAIN-011, REQ-CHAIN-012, REQ-CHAIN-013, REQ-TX-001, REQ-INV-003, REQ-SEC-004, REQ-ACK-001, REQ-IDEM-001.

### Test

Goal 05, Goal 06, Goal 08, and Goal 11 negative tests for forged, unauthorized, replayed, and oversized messages, including a service signature presented as a provider acceptance. The blueprint page 26 consensus gate is the later measurement. Goal 29 repeats flood tests. None of those runs happened in Goal 01.

### Residual limitation

An admitted key can still submit a transaction the rules allow. These controls describe a permissioned submitter set. They are not evidence about a permissionless network. Admission membership itself is blocked by DEC-002.

## TM-KEYS

Compromised keys.

The relevant keys are distinct: a login session, a delegated hosting key if one is ever adopted, a provider key, a service broadcaster key, a validator consensus key, a peer identity key, and a governance key. Theft of one class is not theft of the others, and a signature from one class must not be accepted as another.

### Mitigation

Generate validator consensus keys locally. Keep peer identity, validator signing, and governance keys separate. Keep query and public API nodes off the signing endpoint. Do not copy one validator signing key onto two simultaneously active nodes. Rotate service keys. Least privilege for the broadcaster: it cannot manufacture provider acceptance. Recovery and replacement of a stolen key require the custody decision. Requirements: REQ-GEN-004, REQ-SEC-002, REQ-CHAIN-012, REQ-OPS-005, REQ-ID-001.

### Test

Goal 06 tests duplicate keys, unknown signers, and removal. Goal 26 rotation and recovery drills are the compromise exercise, and they stay blocked while DEC-001 is OPEN. Goal 01 records that blocker and does not simulate a drill.

### Residual limitation

A stolen key is authentic until a revocation commits. With four equal-power validators, one faulty validator is inside the stated fault assumption and two unavailable validators stop new commits. If a later decision adopts host-held user keys, compromise of the account host becomes compromise of those users. That model is PROPOSED under DEC-001 and is not approved. No live keys are stored in this repository.

## TM-DISHONEST

Dishonest providers and auditors.

A provider signs an acceptance and then fails to store the package, serves one requester and not another, or fetches the bytes from someone else during a check. An auditor signs a retrieval attestation that is false. Several node identities are used to look like several replicas. Colluding operators vouch for each other.

### Mitigation

Label provider receipts and auditor statements as attestations. Acceptance is a promise, not possession. The chain rejects a second acceptance by the same provider for the same content version so it cannot count as two replicas. Declared region or owner strings are not treated as physical facts. No automatic reward or financial penalty is paid for an attestation. A reviewed registry is the beta admission path, and its membership policy is DEC-002. Requirements: REQ-EVID-001, REQ-EVID-002, REQ-EVID-003, REQ-EVID-004, REQ-INV-001, REQ-INV-007, REQ-HOST-003, REQ-SEC-005.

### Test

Goal 09 tests that evidence records cannot be labeled as unqualified storage proofs. Goal 29 exercises outsourcing, multiple identities, false receipts, selective service, and collusion scenarios on owned test systems. Goal 01 does not run them.

### Residual limitation

An auditor can lie. A provider can discriminate or fetch bytes elsewhere. Administrative review does not prove distinct disks. An independent process controlled by the provider's operator is not an independent auditor. Sampled checks, even later cryptographic ones, only mean what their construction proves. Cryptographic storage proofs are deferred and are not a mitigation of this beta.

## TM-REPLICAS

Unavailable replicas.

A host, a disk, or the creator's origin copy disappears. The last remaining copy might be the one a repair job is about to delete. A workstation crash takes every local container with it.

### Mitigation

The publication parameter of three copies is PROPOSED and is not a durability guarantee. Count failure domains as well as node identities. On failure, mark availability degraded, copy from a survivor, and confirm a new commitment. Do not drop the last reachable copy before a replacement exists. A voluntary departure stops new offers and hands off accepted work until the deadline. Show degraded and renewal states to the creator. Requirements: REQ-SCOPE-015, REQ-HOST-006, REQ-UX-010, REQ-QA-003, REQ-DEV-002.

### Test

Goal 17 and Goal 18, on a fuller Linux runner, remove one designated host and the origin and retrieve from a survivor. Goal 30 repeats the demonstration with a separate operator when one exists. A single-machine run must be labeled as one failure domain.

### Residual limitation

Three processes on one machine can fail together. If every copy is gone, repair cannot invent bytes. The origin-backup duration is unset while DEC-004 is OPEN. Chain records of a lease do not restore media bytes. This beta does not claim a durability percentage.

## TM-STALL

Chain stalls.

Two of the four equal-power validators stop, or the network partitions so no new commit is possible. Gateways and the application database may still be reachable. Operators may be tempted to reset genesis or to mark a local row as a final commitment.

### Mitigation

Show requested, submitted, committed, and failed as distinct states. Queue chain requests with an explicit expiry and show them as pending. Do not activate a new storage promise from a database write alone. Freeze new final commitments. Playback that already met the publication conditions may continue only under the last accepted real-time terms, and the client discloses stale chain status. Host deadlines are real-time, not a conversion of block height into seconds. An offline extension is not an on-chain renewal. Restoration must not fabricate history. An application must not override finality. Governance transactions also wait for commits, so a stall can freeze council changes. Requirements: REQ-CHAIN-005, REQ-ARCH-011, REQ-ARCH-012, REQ-PEND-001, REQ-HOST-007, REQ-OPS-006, REQ-REC-001, REQ-ACK-001.

### Test

Goal 05 and Goal 18 stop one validator and expect commits to continue, then stop two and expect no new commitment to be presented as final. Goal 20 checks the pending and stale labels. Goal 01 specifies this scenario as S-STALL and does not run a chain.

### Residual limitation

A client that ignores the stale label can mislead a viewer. Real-time hosting deadlines can pass while commits are halted, so local obligations and chain state diverge until operators recover. Recovery is an operator procedure and is not defined here as a key ceremony. Resetting genesis is not a recovery. Council quorum loss during a stall is unspecified while DEC-008 is OPEN.

## TM-STALE-GW

Stale gateways.

A gateway, index, or cached manifest is stale, truncated, or forged. A user compares two ordinary HTTP responses and treats agreement as verification. The UI collapses submitted and committed into one success flag.

### Mitigation

Allow more than one configured endpoint. Disclose that, in this beta, the chosen gateway is trusted for read responses. Sign records and check manifest digests before advertising a package. Treat the indexer as a cache and reconcile it to chain queries. Comparing two unauthenticated HTTP responses is not a light-client proof. Keep submitted and committed distinct. Requirements: REQ-ARCH-009, REQ-GW-001, REQ-SEC-006, REQ-IDEM-002, REQ-HOST-001, REQ-PEND-001.

### Test

Goal 12 reconciliation and forged-event tests. Goal 15 corrupt-manifest rejection. Goal 20 stale-record and bad-manifest tests. Goal 29 repeats gateway tampering. Goal 01 does not run them.

### Residual limitation

A beta user who trusts one gateway can receive a false read. This specification does not claim light-client verification. A previously valid package may still play until its real-time deadline, which is continued service with a disclosure, not a freshness proof. Alternate endpoints help only when the user or client actually has them.

## TM-ABUSE

Abusive content.

An invited account uploads material the community does not allow, harasses another account, or demands deletion from machines the operators do not control. A viewer is told that Published means the chain found the video lawful.

### Mitigation

Invited accounts only. Provide report, block, a queue, reasons, appeals, and escalation once DEC-003 is answered. Apply policy at the account host, discovery, storage admission, and the client. Keep complaint files off-chain. Validators do not decide legality of each video. Removal of cooperating services delists and ends participating leases, purges controlled caches, and revokes credentials. The interface states that independent copies and historical ledger records remain. Published is not a legal or truthfulness finding. Requirements: REQ-SCOPE-001, REQ-MOD-001, REQ-MOD-003, REQ-MOD-004, REQ-UX-009, REQ-SEC-009, REQ-NG-002.

### Test

Goal 23 exercises delisting, cache purge, and the visible limitation about outside copies, after DEC-003 and DEC-004 are answered. Until then the promise is blocked. The PROPOSED five-minute figure is not a test that Goal 01 passed. Audience limits, including minors, stay blocked by DEC-007.

### Residual limitation

Invited users can still upload harmful material up to the quota. Independent copies survive. Ledger history of coordination records survives. Moderation roles are unstaffed. No minor-safety claim, legal-compliance claim, or global-deletion claim is made. Public launch remains disabled. A future automated moderation aid must not gain authority to change chain policy from user content.

## TM-SUPPLY

Supply-chain attacks.

A dependency, container base, CI script, or generated stub is substituted. Secrets are exposed to an untrusted pull request. Proprietary or incompatibly licensed code is copied into the tree. An Enterprise-only Cosmos module is linked silently.

### Mitigation

Pin versions and digests, generate an SBOM, and record each dependency licence when those tools exist. Isolate CI secrets and do not give untrusted pull requests privileged credentials. Exclude Cosmos Enterprise PoA and Groups. Do not vendor Loops, PeerTube, or Livepeer as drop-in components. Do not copy proprietary code. A secret committed to git is TM-SECRET, not only a CI concern. The project licence split stays blocked by DEC-006, so this goal adds no licence file that pretends the split is chosen. Requirements: REQ-TECH-010, REQ-TECH-013, REQ-TECH-014, REQ-SEC-008, REQ-SEC-012, REQ-GOV-005, REQ-NG-001, REQ-GEN-002.

### Test

Goal 03 secret scanning covers both CI exposure and files committed to git, plus pin review and CI permission tests. Goal 27 signed-build and notice checks after DEC-006. Goal 29 reviews the pins against the candidate revision. Goal 01 adds no application dependency and has no SBOM to publish. The absence of application code is the inventory in `docs/requirements.md`, checked by the Goal 01 script. TM-SECRET is the committed-secret case.

### Residual limitation

A pin can record a dependency that is itself compromised. CI isolation is not a guarantee about an upstream maintainer. No signed release exists yet. Licence compatibility of the PROPOSED AGPL and Apache split is unconfirmed.

## TM-VALIDATOR

Validator fault or partition.

A validator equivocates, stops, or is partitioned. An operator restores a node by inventing blocks or by starting a second live copy of the same signing key.

### Mitigation

Distinct peer identities and data directories. Deterministic transitions from committed inputs. Quorum behavior for four equal powers: one stopped validator can leave commits possible; two stopped validators must not be presented as a new commit. Refuse a second live copy of one validator identity. Requirements: REQ-CHAIN-003, REQ-CHAIN-005, REQ-DET-001, REQ-OPS-005, REQ-DEV-001.

### Test

Goal 05 fault tests on a fuller Linux runner. Goal 29 partition tests on owned systems. A one-workstation run is reported as one failure domain and is not an independent-operator result.

### Residual limitation

Process separation is not operator diversity. A partition can halt the chain. Safety claims stay inside the stated CometBFT assumptions for an admitted set. They say nothing about a permissionless validator set, which is outside this beta. A staking or MsgCreateValidator bypass is TM-STAKE-BYPASS, not this partition case.

## TM-STAKE-BYPASS

Validator-set bypass via staking or MsgCreateValidator.

A tutorial scaffold, a staking module, or a Cosmos `MsgCreateValidator` path admits a validator or changes voting power without x/authority. The mempool may look closed while execution still accepts the bypass.

### Mitigation

x/authority is the only writer of validator membership and scheduled power. Remove staking and any `MsgCreateValidator` handler that can set the same set. Do not run a staking module beside x/authority. Requirement: REQ-CHAIN-006.

### Test

Goal 06 submits a staking-style power change and a `MsgCreateValidator` transaction and expects rejection, then shows that only an x/authority change alters the committed validator set. Goal 01 does not run a chain and does not treat this paragraph as a pass.

### Residual limitation

Removing a module from the planned wiring is not the same as a later patch reintroducing it. A custom message that writes the validator set under another name is the same bypass. DEC-008 still leaves the council membership that is allowed to use x/authority unset.

## TM-PERSONAL

Personal data on-chain and linkability.

A transaction or state entry carries an email address, a recovery secret, a watch event, a complaint narrative, a private message, or a legal file. An opaque identifier or a hash of a personal value is published in the belief that hashing made it anonymous.

### Mitigation

Those values stay off-chain. Endpoint details prefer a signed off-chain manifest. Treat an opaque id or a hash as still linkable. Requirements: REQ-OFF-002, REQ-OFF-004, REQ-OFF-001, REQ-OFF-005.

### Test

Goal 02 and Goal 19 schema tests reject email, recovery secrets, watch events, complaint text, private messages, and legal files on chain, and the privacy note states that hashing is not anonymization. Goal 01 does not run those tests.

### Residual limitation

A coordination id can still be joined with off-chain data by someone who sees both. Hashing does not remove that. Retention of the off-chain copies is DEC-004 and is unset. This beta does not claim anonymity.

## TM-INJECTION

SQL injection and prompt injection.

Titles, comments, captions, and remote manifests are placed into a shell command, a SQL query, or a model prompt that can change policy or call a tool. Shell injection is the same class of mistake and is included here.

### Mitigation

Treat that text as data. Do not interpolate it into shell commands, SQL, or model prompts. A future moderation or support aid must not gain authority to change chain policy or execute tools from user content. Requirement: REQ-SEC-009.

### Test

Goal 23 and the API tests feed quote marks, SQL metacharacters, and instruction-shaped captions through upload, comment, and manifest fields and expect no second statement, no shell execution, and no policy change. Goal 01 does not run those tests.

### Residual limitation

A new query, template, or model integration can miss the same rule. Parameterized queries and a prompt boundary reduce the chance and do not prove every future caller is safe. No model tool-use is in this beta, so there is no runtime demonstration of a blocked prompt yet.

## TM-MINT

Token, inflation, and slashing through default modules.

The SDK tutorial app wires `x/mint`, `x/distribution`, and `x/slashing`. Leaving them in creates a token, inflation, or an automatic financial slash even when the beta's own modules say they have none.

### Mitigation

Remove `x/mint`, `x/distribution`, and `x/slashing` when the tutorial scaffold is trimmed. Do not add a tradable token, inflation, or automatic financial slashing under another module name. Requirements: REQ-GEN-001, REQ-PROD-013, REQ-CHAIN-015, REQ-NG-003.

### Test

Goal 05 app wiring and genesis show those three modules absent, and a search of the beta module set finds no mint, distribution, or slashing keeper. Goal 01 has no chain binary and does not treat this paragraph as a pass.

### Residual limitation

A later dependency bump can reintroduce a module if the wiring review is skipped. Zero fees are not the same control as the absence of these modules. No audit of the custom modules is claimed.

## TM-SECRET

A secret committed to git.

A validator key, mnemonic, API token, or `.env` file is committed, even when CI would have refused to print it. TM-SUPPLY's CI isolation does not cover this case by itself.

### Mitigation

Keep the repository gitignore rules for keys, mnemonics, env files, and database dumps. Genesis carries no private keys. CI secret scanning is an additional gate, not the only one. Requirements: REQ-GEN-002, REQ-SEC-008, REQ-REPO-004.

### Test

Goal 03 runs secret scanning on the tree and on pull requests, and the gitignore fixtures reject a sample key file. Goal 05 genesis validation rejects a private key. Goal 01 does not claim a historical secret scan of every past commit.

### Residual limitation

Gitignore does not remove a secret that was already pushed. Scanning can miss a novel filename. A leaked key stays valid until it is rotated, and rotation of live keys waits on DEC-001 where the key is a custody key.

## What this threat model refuses to claim

No control above was executed in Goal 01. Passing a later beta gate would still not show security against an unrestricted economically motivated network. Permissionless admission, cryptographic storage proofs, automatic rewards, and public launch stay out of scope until their own decisions and reviews exist. Using Cosmos SDK and CometBFT does not audit the custom modules.
