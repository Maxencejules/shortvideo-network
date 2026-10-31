# Protocol v0

Status of this document: draft contract for the permissioned community beta. It is not an approved protocol. It is not an audit and it is not a production-security claim.

Public launch: DISABLED

No tradable token, coin, sale, or mining. No inflation. No automatic financial slashing.

DEC-001 through DEC-012 were approved at Gate A on 2026-10-31 (America/Toronto). DEC-009 is council-only admit, disable, and force-withdraw. DEC-010 derives REJECTED from verification. DEC-011 keeps liveness out of the application hash. DEC-012 keeps mixed activation kinds as CONFLICT. Public launch stays disabled. A devnet numeric stand-in is not a second policy.

The interfaces in this document are frozen-pending-Gate-A for Goals 03 through 18. An incompatible change requires a recorded decision before the schema version changes. Gate A on 2026-10-31 approved DEC-001 through DEC-012. Protocol v0 freezes once this goal passes review. This revision is the candidate.

## Freeze

Protocol name: `svn-protocol-v0`. Schema version: 1. Canonical profile: `SVN-JCS-1`.

Machine-readable files:

- `docs/protocol/bounds.json` holds sizes, domains, and safety caps.
- `docs/protocol/proto/` holds versioned Protobuf state, transaction, query, and genesis types.
- `docs/protocol/schemas/` holds off-chain JSON schemas.
- `docs/protocol/api/` holds the client and provider API contracts.
- `docs/protocol/fixtures/` holds vectors and manifest fixtures.
- `docs/evidence/goal-02/` holds the executable checks.

Unknown fields are rejected in v0. A new field is an incompatible change. It needs a recorded decision and a new schema version. Handlers must not treat an unknown enum value as zero.

Goals 03 through 18 implement these contracts. They do not silently replace a field, a domain string, an error number, or a state name.

## Hard exclusions

Video bytes stay off the chain. The chain stores a digest and a byte count.

These values stay off the chain: usernames tied to private details, email addresses, recovery secrets, individual watch events, complaint narratives, private messages, and legal evidence files. An opaque identifier or a hash can still be linkable. Hashing is not anonymization.

`x/mint`, `x/distribution`, and `x/slashing` are not enabled. `mint_module_enabled`, `distribution_module_enabled`, and `slashing_module_enabled` must be false. Staking is not enabled. There is no fee denom and a non-empty fee amount is `ERROR_CODE_FEE_REJECTED`. Gas metering still applies. Zero fees are not unlimited computation.

`MsgCreateValidator` is not a message in this protocol. `permissionless_validator_creation` must be false. `x/authority` is the only writer of validator membership and scheduled power.

Signed receipts and audits are attestations. They are not proof of independent storage or future delivery. A Merkle inclusion is not a unique-replica proof. Evidence kinds `STORAGE_PROOF` and `UNIQUE_REPLICA_PROOF` are reserved and rejected.

## Trust boundaries

Each boundary has its own authentication and its own failure behavior.

| Boundary | Authentication | What a failure means |
| --- | --- | --- |
| User login | Off-chain account session. A web passkey is not a Cosmos key. | The session is rejected. No lease changes. |
| Social records | SVN-JCS-1 signature on a public-record envelope. | The record is rejected. Chain obligations do not move. |
| Media transport | HTTPS, quarantine, and the manifest digest. | The replica is not advertised. No chain reservation is taken. |
| Storage commitments | Provider signature over the exact terms, then chain accounting. | The obligation is not created or is ended by a legal transition. |
| Governance | Council signatures under DEC-008, which is unset. | The proposal does not execute. |
| Consensus | CometBFT votes of the admitted validator set. | No new commit. Pending work stays pending. |

A provider signature is not a service signature. A broadcast acknowledgement is not committed execution. A database row is not a ledger. Observed reachability is not a hosting obligation. Community publication approval is not either of those.

## Identifiers, integers, time, and digests

Identifiers are UTF-8 strings matching `^[a-z0-9][a-z0-9._-]{0,63}$` (1 to 64 bytes). They are opaque. They are not anonymized account names.

Account addresses are 20 bytes. Ed25519 public keys are 32 bytes. Ed25519 signatures are 64 bytes. Consensus public keys in genesis are 32 bytes and are public. Private keys are forbidden in genesis and in Git.

Digest algorithm v0 is SHA-256 only (`DIGEST_ALGORITHM_SHA256`). The digest value is exactly 32 bytes. Hex encodings used in JSON are 64 lowercase hex characters. Unknown algorithms and the zero enum are rejected.

Chain integers that must survive JavaScript, Python, and Go without rounding:

- JSON numbers are integers in `[-9007199254740991, 9007199254740991]`. Fractions, exponents, leading zeros, and negative zero are rejected.
- `uint64` quantities, including `package_bytes` and reserved byte counters, are decimal strings with no leading zeros when they appear in JSON. The largest `uint64` is `18446744073709551615`.
- `content_version`, `term_version`, and `policy_version` are integers in `1..9007199254740991` except `policy_version`, which starts at 1. v0 content records use `content_version` 1. A second encoding is a new `content_id` until a recorded decision says otherwise.

Time is Unix seconds, int64, in `[1, 253402300799]`. The unit is seconds. There is no millisecond field in consensus state. `0` is unset and is rejected where a time is required.

Committed chain time is the block header time of a committed block. Real-time host deadlines are the `deadline_unix_seconds` values inside signed lease terms. Hosts compare those deadlines to their own clocks and show clock uncertainty. A chain handler never reads a local clock and never accepts a `host_observed_unix` field.

Safety caps in `bounds.json` are denial-of-service bounds for v0. They are not sponsorship allowances, lease durations, or council thresholds. Changing one is an incompatible protocol change and needs a recorded decision. The package-byte cap is 268435456 (256 MiB). That number is not a DEC-005 allowance.

## Signature domains

Off-chain and inner signatures use:

`UTF-8(domain) || 0x00 || UTF-8(SVN-JCS-1 canonical JSON)`

The domain strings are in `bounds.json`. v0 domains:

| Domain | Signed record |
| --- | --- |
| `SVN-HOSTING-INTENT-1` | Hosting intent. The signer role is DEC-001 and is unset. |
| `SVN-PROVIDER-ACCEPT-1` | Provider acceptance of one offer. |
| `SVN-PROVIDER-RENEW-1` | Provider renewal. Bytes and digest stay the same. |
| `SVN-PROVIDER-END-1` | Provider end of one lease. |
| `SVN-PROVIDER-DRAIN-1` | Provider stops taking new offers. |
| `SVN-PROVIDER-ROTATE-1` | Provider signing-key rotation. |
| `SVN-CAPABILITY-1` | Provider capability manifest. |
| `SVN-MEDIA-MANIFEST-1` | Media manifest. |
| `SVN-EVIDENCE-ATTESTATION-1` | Evidence attestation envelope. |
| `SVN-PUBLIC-RECORD-1` | Public social record. |
| `SVN-TRANSFER-RECEIPT-1` | Transfer receipt attestation. |
| `SVN-INVENTORY-1` | Inventory report. |

A signature from one domain is not valid in another (`ERROR_CODE_WRONG_DOMAIN`). The outer Cosmos SDK signature is a different domain: `SIGN_MODE_DIRECT`. It does not replace an inner signature.

Chain transactions reuse the SDK transaction envelope. The sign document is the protobuf encoding of body bytes, auth info bytes, chain id, and account number. This repository does not vendor that SDK proto. Goal 05 wires it. A signature bound to a different chain id is unauthorized.

## Error codes

ABCI success is 0. `ERROR_CODE_UNSPECIFIED` is never returned. Idempotent success is ABCI 0 with no second effect.

| Code | Number | When |
| --- | --- | --- |
| `ERROR_CODE_UNAUTHORIZED` | 1 | Missing signer, bad signature, or forged signature. |
| `ERROR_CODE_FORBIDDEN` | 2 | Known signer, wrong role, or a disabled act such as mint or `MsgCreateValidator`. |
| `ERROR_CODE_REPLAY` | 3 | Same business id, different payload. |
| `ERROR_CODE_DUPLICATE_ID` | 4 | Identifier or live provider/content pair already used. |
| `ERROR_CODE_STALE_VERSION` | 5 | Term, generation, epoch, or policy base is not the next legal version. |
| `ERROR_CODE_EXPIRED` | 6 | Offer, evidence, or proposal time is not strictly after committed time. |
| `ERROR_CODE_MALFORMED` | 7 | Missing field, wrong type, or illegal enum. |
| `ERROR_CODE_UNKNOWN_FIELD` | 8 | A field this schema does not define. |
| `ERROR_CODE_OUT_OF_RANGE` | 9 | A present value is outside the allowed set, including an unapproved term length. |
| `ERROR_CODE_OVERFLOW` | 10 | `uint64` addition would wrap. |
| `ERROR_CODE_UNDERFLOW` | 11 | A release exceeds reserved bytes, including a second end. |
| `ERROR_CODE_WRONG_DOMAIN` | 12 | Signature domain mismatch. |
| `ERROR_CODE_CAPACITY` | 13 | Reservation would exceed declared admitted capacity. |
| `ERROR_CODE_CONFLICT` | 14 | Conflicting governance update or an illegal byte change. |
| `ERROR_CODE_WITHDRAWN` | 15 | Content tombstone blocks the action. |
| `ERROR_CODE_NOT_FOUND` | 16 | Object is absent. |
| `ERROR_CODE_DECISION_BLOCKED` | 17 | An OPEN decision has no approved value and the devnet test parameter is not enabled. |
| `ERROR_CODE_INTEGRITY` | 18 | Digest or segment bytes do not match. |
| `ERROR_CODE_BOUNDS` | 19 | Length, page, batch, or gas bound. |
| `ERROR_CODE_QUORUM` | 20 | The current validator set fails the hashed power invariant: at least four validators and `max_power * 3 < total_power`. The fixture `down` set does not produce this code. |
| `ERROR_CODE_FEE_REJECTED` | 21 | A non-zero fee or a fee denom was present. |

Enum value 0 and any unknown numeric enum are `ERROR_CODE_MALFORMED`, not a default state.

## Open decision parameters

Each open decision is a `BlockedDecisionParameter`: `decision_id`, `schema_version` 1, `value_version` 0, `approved` false. A transition that sets `approved` to true is rejected. The devnet may carry a `DevnetTestParameter` with `label` `PROPOSED_TEST_PARAMETER` and `approved_policy` false only when `allow_devnet_test_parameters` is true and `chain_id` starts with the PROPOSED example prefix `shortvideo-dev-`. That prefix is not an approved network name. The example chain id `shortvideo-dev-1` is a PROPOSED example.

| Decision | Contract field | v0 behavior |
| --- | --- | --- |
| DEC-001 | `HostingIntent.role_name`, domain `SVN-HOSTING-INTENT-1` | `ACCOUNT_HOST_DELEGATE` is rejected. Only the labeled devnet role `DEVNET_TEST_ONLY` can pass, and only with the devnet flag. Host custody is not frozen. |
| DEC-002 | `Provider.operator_group_id`, `AdmitProvider` | Register creates `APPLIED`. Admission requires the devnet test parameter. The group id is not evidence of an independent operator. |
| DEC-003 | publication status, `MsgForceWithdraw`, and `ForceWithdrawChange` | The message can write a coordination tombstone. `removal_promise_approved` stays false. The removal promise itself is unset. |
| DEC-004 | `Tombstone.retention_value_version` | The version stays 0. No retention duration is stored. `history_erased` must be false. |
| DEC-005 | offer term `deadline - start`, allowance | Without the devnet test parameter the term is `ERROR_CODE_DECISION_BLOCKED`. The test number is not an allowance and not a sponsor. |
| DEC-006 | creator upload licence id on the publish API | The publish route returns 403 while the licence text is unset. No licence file is implied. |
| DEC-007 | audience and age fields | There is no public sign-up route and no age-gate value. Invitation policy stays unset. |
| DEC-008 | `CouncilPolicyParameter` seats, threshold, delay, expiry, `QuorumLossBehavior` | `QuorumLossBehavior` is `UNSET`. Execute fails closed unless the devnet test parameter is enabled. The test threshold is not 3-of-4 and is not an approved council. `approved_council` is empty. A validator power change names its own activation point. No minimum delay beyond "strictly after the execution context" is chosen. |
| DEC-009 | council `AdmitProviderChange`, `DisableProviderChange`, `ForceWithdrawChange` | Approved: council only. The single-signature messages are gone. A direct `AdmitProvider`, `DisableProvider`, or `ForceWithdraw` is `ERROR_CODE_FORBIDDEN`. |
| DEC-010 | evidence state `REJECTED` | Approved: `REJECTED` is derived when `observation_digest` does not match the lease manifest. There is no submitter `reject` flag. |
| DEC-011 | fixture `down` versus the application hash | Approved: liveness stays out of the hash and out of `commit_possible`. The schedule check is `max_power * 3 < total_power` and at least four validators. Liveness can still halt an accepted schedule. With v4 down, removing v1 at height 30 would leave three validators and one of them down, so live power would not commit. That removal is refused by the hashed check, so it is not scheduled. |
| DEC-012 | height activation beside time activation | Approved: mixed kinds are `ERROR_CODE_CONFLICT` and the proposal stays `QUEUED`. `ERROR_CODE_FORBIDDEN` marks the proposal `REJECTED`. |

## Canonicalization

Off-chain signed records use SVN-JCS-1, a restricted profile of RFC 8785 (JSON Canonicalization Scheme). RFC 8785 is the chosen vetted canonicalization. Gate A accepted SVN-JCS-1, SHA-256, and Ed25519. The interface label remains frozen-pending-Gate-A until this goal passes review.

The profile:

- Serializes strings with the RFC 8785 rules: `\b`, `\t`, `\n`, `\f`, `\r`, `\"`, `\\`, lowercase `\u00xx` for other controls, and raw UTF-8 otherwise.
- Sorts object keys by UTF-16 code units. Keys must match `^[A-Za-z0-9._-]{1,64}$`, so that order matches unsigned byte order.
- Preserves array order.
- Allows JSON numbers only in the safe integer range above. No null, no fractions, no exponents.
- Rejects duplicate keys, trailing commas, and a trailing value.
- Canonical output has no insignificant whitespace.

`uint64` fields are JSON strings, which is how this profile keeps values that do not fit in an IEEE-754 exact integer. Cross-language vectors live in `docs/protocol/fixtures/vectors.json`. Go, Python, and TypeScript each parse those vectors with their own implementation.

`payload_digest` is SHA-256 over the UTF-8 SVN-JCS-1 encoding of the proto3 JSON object for `ProposalPayload`. The mapping is the proto3 JSON mapping, then SVN-JCS-1:

- Exactly one alternative is present. Its name is the lowerCamelCase JSON name: `validatorPower`, `governedLimit`, `admitProvider`, `disableProvider`, or `forceWithdraw`. The oneof name is not a JSON field.
- Nested fields use lowerCamelCase. Absent fields are omitted. Unknown fields are rejected.
- `bytes` are standard base64 with padding. Digests and public keys are not hex inside this object.
- `int64` and `uint64` are decimal strings with no leading zeros. Proto3 JSON omits default values, and `power` 0 is the default, so a removal has no `power` field. A present `"power": "0"` is not that canonical object and is `ERROR_CODE_MALFORMED`.
- A `Digest` is `{ "algorithm": "DIGEST_ALGORITHM_SHA256", "value": "<standard base64>" }`. The enum is the full name.
- `ActivationPoint` is either `{ "unixSeconds": "<decimal>" }` or `{ "height": "<decimal>" }`.

The vector `proposal-payload-validator-power` is that object for node `v5`, power `1`, activation height `3`, and the public test reason digest. Its payload digest is `be9df594c416ecbaf50512e298a73fecf9f7a56e7ac83a0cf4edf87a6b522c48`. The vector `proposal-payload-validator-power-zero` is the same removal shape with `power` omitted. Its payload digest is `7eeb7c6d8fadfb2b28fea5d2cd42f1a85eaf2b773e79b24399d350b9b9be675a`.

## Chain transaction envelope

A chain message is an SDK `Msg` inside `cosmos.tx.v1beta1.Tx`, signed with `SIGN_MODE_DIRECT`. The application checks run during execution, not only in the mempool.

The outer signer is an admitted submitter. The inner signature, when required, is the provider, auditor, or hosting-intent signature. A broadcaster may relay a transaction it did not author. It cannot manufacture the inner signature. Relaying a transaction does not make the broadcaster the provider.

Replay uses two layers. The SDK account sequence rejects the same signed bytes. The application business id rejects a new transaction hash that repeats a committed action. The business payload omits `tx_hash`. The same business id and the same payload is an idempotent success with no second reservation and no second event. The same business id and a different payload is `ERROR_CODE_REPLAY`. A failed attempt does not store the business id.

`AuthInfo.fee.amount` must be empty. `gas_limit` is an integer from 1 through `safety_max_gas_per_tx`.

## State machines

Three records stay separate.

1. Committed hosting obligation. Chain lease state. It reserves declared bytes.
2. Observed reachability. Off-chain only. `REACHABILITY_DEGRADED` is the encoding of the PROPOSED blueprint label `AT_RISK`. It is not a lease state. `LEASE_STATE_AT_RISK` is reserved and rejected.
3. Community publication approval. Off-chain `PublicationStatus`. The PROPOSED label `PUBLISHED` is this approval together with the hosting conditions in force. It is not a chain content state and it does not mean the chain certified legality or truth. `CONTENT_STATE_PUBLISHED` is reserved.

PROPOSED blueprint labels and v0 encodings:

| PROPOSED label | v0 encoding |
| --- | --- |
| OFFERED | `LEASE_STATE_OFFERED` (1). No reservation. |
| ACCEPTED and ACTIVE | `LEASE_STATE_ACTIVE` (2). Acceptance and the committed obligation are one transition. |
| RENEWED | `LEASE_STATE_RENEWED` (3). Same reservation, next term version. |
| ENDED | `LEASE_STATE_ENDED` (4). Reservation released once. |
| EXPIRED | `LEASE_STATE_EXPIRED` (5). Released by bounded expiry. |
| AT_RISK | `REACHABILITY_DEGRADED` (3) on the off-chain reachability record. |
| PENDING | `CONTENT_STATE_PENDING` (1). No hosting claim. |
| PUBLISHED | Off-chain publication approval. Not a chain enum. |
| WITHDRAWN | `CONTENT_STATE_WITHDRAWN` (2) plus a tombstone. |

Provider states: `APPLIED` (1), `ADMITTED` (2), `DRAINING` (3), `DISABLED` (4). Draining stops new offers and keeps accepted reservations. Disablement is `DisableProviderChange` through the council. A direct `DisableProvider` message is `ERROR_CODE_FORBIDDEN`. Existing reservations stay until a legal end.

Authority proposal states: `OPEN` (1), `QUEUED` (2), `EXECUTED` (3), `EXPIRED` (4), `REJECTED` (5). Council voting is not CometBFT voting.

Evidence states: `RECORDED` (1), `EXPIRED` (2), `REJECTED` (3). `attestation` must be true. Recorded evidence does not change a lease deadline. `REJECTED` is derived when `observation_digest` is present and does not equal the lease manifest. There is no submitter `reject` flag.

Operation states for the client, which are not chain states: requested, submitted, committed, failed, and pending while the chain is stalled.

## Transitions

Reservation is taken at `AcceptLease` and released once at `EndLease` or at committed expiry of an active lease. Offers do not reserve bytes. A capacity check at offer time is repeated at accept time. Declared reserved bytes cannot exceed declared admitted capacity. Counters use checked `uint64` addition and subtraction.

A live pair is one provider plus one content id plus `content_version`. A second live reservation for that pair is `ERROR_CODE_DUPLICATE_ID`. Ended and expired leases stay in history and leave the pair index, so they do not count as a second replica.

| Transition | Signer | Who may relay | Replay and idempotency | Capacity |
| --- | --- | --- | --- | --- |
| `RegisterProvider` | Admitted provider account | Broadcaster or the signer, as the outer SDK signer | `provider_id` and business id. A second id is a duplicate. | Sets declared capacity. Reserved bytes start at 0. State is `APPLIED`. |
| `AdmitProvider` | No single-signature message. The effect is `AdmitProviderChange` through the council. A direct `AdmitProvider` is `ERROR_CODE_FORBIDDEN`. | Council execution | Business id | Does not reserve. The approved DEC-002 default is permissioned admission. The devnet fixture still checks the labeled test role. |
| `DisableProvider` | No single-signature message. The effect is `DisableProviderChange` through the council. A direct `DisableProvider` is `ERROR_CODE_FORBIDDEN`. | Council execution | Business id | No release. New offers fail. |
| `UpdateProvider` | Current provider account | The signer | Business id. Capacity below reserved bytes is `ERROR_CODE_CAPACITY`. | Changes declared capacity and stores the new endpoint manifest digest. |
| `RotateProviderKey` | Current provider key, domain `SVN-PROVIDER-ROTATE-1` | The signer | Expected generation must match. A stale generation is `ERROR_CODE_STALE_VERSION`. The old key is retained and cannot sign new accepts. | No capacity change. |
| `DrainProvider` | Provider, domain `SVN-PROVIDER-DRAIN-1` | The signer | Business id | No release. New offers fail. Accepted reservations stay. |
| `RegisterContent` | Admitted submitting host, plus inner hosting intent | Broadcaster may submit the outer transaction only | New `content_id`. Creates `PENDING` and no lease. | No reservation. DEC-001 blocks the intent role outside the devnet test role. |
| `WithdrawContent` | Same submitting host, plus inner hosting intent | Same as register | Tombstone. A later register of the same id is a duplicate. A second withdraw is `ERROR_CODE_WITHDRAWN`. | Voids open offers. Does not release an accepted reservation. `reason_digest` is stored and is not the complaint text. |
| `ForceWithdraw` | No single-signature message. The effect is `ForceWithdrawChange` through the council. A direct `ForceWithdraw` is `ERROR_CODE_FORBIDDEN`. | Council execution | Business id | Same tombstone rules. The payload path checks DEC-003's approved devnet stand-in. |
| `OfferLease` | Admitted scheduler service | Broadcaster | `offer_id`. Expired by committed time is `ERROR_CODE_EXPIRED`. | Checks capacity and does not reserve. DEC-005 blocks the term length outside the devnet test number. |
| `AcceptLease` | Inner provider signature required. Outer signer may be the provider or a broadcaster. | Broadcaster relays; it does not sign acceptance | Business id. Same payload is idempotent. | Reserves `package_bytes` or returns overflow, capacity, duplicate, expired, withdrawn, unauthorized, or wrong domain. |
| `RenewLease` | Inner provider signature, domain `SVN-PROVIDER-RENEW-1` | Broadcaster or provider | Term version must be current + 1. `manifest_digest` must equal the committed digest. | Does not change reserved bytes. A changed byte count or digest is `ERROR_CODE_CONFLICT`. A non-empty `evidence_id` cannot extend the lease. |
| `EndLease` | Provider signature, or authority when `authority_end` is true | Broadcaster for the provider path | Same business id is idempotent. A second business id after release is `ERROR_CODE_UNDERFLOW`. | Releases once. |
| `SubmitAuditBatch` | Auditor, domain `SVN-EVIDENCE-ATTESTATION-1` | Broadcaster | Evidence id and batch id. Batch size is at most 32. | No reservation and no lease extension. |
| `ProposeChange` | A devnet test seat while DEC-008 is unset | Any node may gossip; execution still checks the seat | Proposal id. Base policy version must match. The payload is one `ProposalPayload` alternative, and `payload_digest` is SHA-256 of its SVN-JCS-1 form. | No capacity change until `ExecuteChange`. A key named like a DEC value is forbidden. |
| `ApproveChange` | A seat, once | Same | Same seat again is `ERROR_CODE_REPLAY`. | When approvals reach the devnet test threshold, state becomes `QUEUED` after the test delay. |
| `ExecuteChange` | Authority submitter | Not a substitute for council signatures | Business id | Applies the stored payload: admission, disablement, force-withdraw, or a governed limit at or below that key's own ceiling in `governed_limit_caps`. A validator power change is scheduled, not applied. The schedule simulates every waiting change in due order and checks the set after each activation point, not only at the end. At every point the set must have at least four validators, every power positive, and `max_power * 3 < total_power`. That check does not read the fixture `down` set. Two changes to the same node or the same consensus key at one activation point are `ERROR_CODE_CONFLICT` before either is applied, so the proposer-chosen id does not pick a winner. Pending changes must share one activation kind. A height change beside a time change is `ERROR_CODE_CONFLICT` and the proposal stays `QUEUED`. `ERROR_CODE_FORBIDDEN` marks the proposal `REJECTED`. A duplicate key against the current set or against a scheduled change at a different activation point is `ERROR_CODE_DUPLICATE_ID`, and that proposal stays `QUEUED`. An empty or nonpositive point is `ERROR_CODE_FORBIDDEN`. The activation point is a committed height or chain time strictly after this execution. A later committed block applies the scheduled change. A digest mismatch is `ERROR_CODE_INTEGRITY`. An activation point that is not strictly after the execution context is `ERROR_CODE_OUT_OF_RANGE`. Conflicting payloads on the same parameter key are `ERROR_CODE_CONFLICT`. A stale base is `ERROR_CODE_STALE_VERSION`. |
| Begin-block expiry | No user signer | Not a transaction | Processes a sorted, bounded prefix of due ids | Releases a due active lease once. Deletes a due offer with no release. Marks due evidence expired without renewing a lease. |

Withdrawal stops new sponsored leases and stops a silent revival of the same content id. It does not pretend the ledger row was erased.

## Expiry and determinism

Indexes, kept in sorted order rather than as unordered maps:

- offers by `(expiry_unix_seconds, offer_id)`
- leases with `reservation_held` by `(deadline_unix_seconds, lease_id)`
- evidence by `(expiry_unix_seconds, evidence_id)`

A block processes at most `safety_max_expiry_steps_per_block` due items (128). Remaining items wait for a later committed block. The sort key is `(time, kind, id)`.

An offer is expired when committed time is greater than or equal to `expiry_unix_seconds`. The same comparison applies to evidence and to lease deadlines. Expiry does not run while the chain is not committing new blocks.

Pending requests during a stall stay `PENDING_STALL` in the off-chain outbox. They are not leases. A broadcast acknowledgement is not success. With four equal powers, available power `P` can commit only when `P * 3 > total * 2`. Three of four can commit. Two of four cannot. The failed commit does not advance height and does not move committed time.

No handler fetches video, calls an external API, runs a transcoder, reads the wall clock, uses floating-point scoring, or ranges an unordered map into consensus state. External observations arrive only as signed transaction fields.

## Privacy, tombstones, and reconciliation

Privacy classes: `PUBLIC_COORDINATION`, `PUBLIC_RECORD`, `PRIVATE`, `SECRET`, `RESTRICTED_CASE`. The enum name `PRIVACY_CLASS_SECRET` is a class label. It is the one pinned exemption from the `secret` name pattern. A field named `secret` is still rejected.

Chain state is public coordination. It is linkable. Public-record envelopes are the only social records with a signature domain, and their `privacy_class` must be `PUBLIC_RECORD`. Private preferences, saved videos, raw viewing telemetry, complaint text, and secrets are rejected on that envelope and on chain transactions.

A tombstone keeps `content_id`, `content_version`, committed time, an optional reason digest, `history_erased` false, `retention_decision_id` `DEC-004`, and `retention_value_version` 0. It does not store the complaint narrative. It does not delete historical transactions.

Reconciliation retains, and does not treat as a retention period: business id, transaction hash, message type, height, committed time, object id, digest, byte count, result code, reservation delta, and the idempotent flag. DEC-004 does not say when those rows are deleted. Backups, availability, and integrity stay separate. IPFS pinning is not this beta's retention mechanism.

Key rotation retains previous public keys so old signatures remain checkable. It does not retain private keys. Version negotiation for a peer is the schema version plus `Accept: application/vnd.svn.v0+json`. A different major token is rejected. There is no silent unknown-field compatibility inside v0.

## Off-chain records

The media manifest lists renditions, segment hashes, byte totals, and format metadata. The full manifest stays off-chain. Its SHA-256 may go on-chain. Hosts verify segment bytes against the manifest before advertising a replica. A mismatch is `ERROR_CODE_INTEGRITY` and does not create a reservation. The schema is `schemas/media-manifest.schema.json`. The signed bytes are SVN-JCS-1 under `SVN-MEDIA-MANIFEST-1`.

The provider capability manifest carries the provider id, signing key id, declared capacity as a decimal string, and one to eight `https` endpoint URLs. Endpoint URLs are not chain fields; the chain stores the manifest digest. The schema is `schemas/provider-capability.schema.json`.

The evidence envelope is an attestation. `kind` is `PROVIDER_RECEIPT` or `AUDITOR_ATTESTATION`. `claim_limit` is `ATTESTATION_NOT_STORAGE_PROOF`. `attestation` is true. The schema is `schemas/evidence-envelope.schema.json`.

The public-record envelope carries schema version, author id, record id, sequence, previous digest, key id, payload, and signature. The schema is `schemas/public-record.schema.json`.

## API contracts

Client routes, in `api/client-api.json`: `POST /upload-intents`, `GET /uploads/{upload_id}`, `POST /publish`, `GET /feeds`, `POST /reports`, `POST /removal`, `GET /exports`. There is no public registration route. The session scheme is an account session, not a chain key. Upload intent returns requested, not committed. Publish stays distinct from the lease. Export stays blocked on DEC-001 and DEC-004.

Provider routes, in `api/provider-api.json`: `GET /capability`, `POST /offers/{offer_id}/acceptance`, `GET /packages/{content_id}/{content_version}`, `POST /transfers/{transfer_id}/receipt`, `POST /inventory`.

Chain commands: `RegisterProvider`, `UpdateProvider`, `RotateProviderKey`, `DrainProvider`, `AdmitProvider`, `DisableProvider`, `RegisterContent`, `WithdrawContent`, `ForceWithdraw`, `OfferLease`, `AcceptLease`, `RenewLease`, `EndLease`, `SubmitAuditBatch`, `ProposeChange`, `ApproveChange`, `ExecuteChange`. `ProposeChange` carries a `ProposalPayload` and the digest of that payload. `ExecuteChange` applies admission, disablement, force-withdraw, and governed limits. It schedules `ValidatorPowerChange` until the activation point.

Chain queries: provider, providers, content, active leases, deadlines, evidence, governance, policy. Queries use `PageRequest`. `limit` is at most 100. `page_key` is at most 64 bytes.

Chain events: `ProviderRegistered`, `ProviderStateChanged`, `ContentRegistered`, `ContentWithdrawn`, `LeaseOffered`, `LeaseAccepted`, `LeaseRenewed`, `LeaseEnded`, `EvidenceAccepted`, `PolicyChanged`, `ProposalExecuted`. Attribute values that are byte counts are decimal strings.

## Genesis

`svn.app.v1.AppGenesis` is the field inventory: chain id, genesis time, protocol version, the five disabled module flags, the devnet-test flag, authority, providers, empty content, empty leases, evidence, and policy safety caps. It is not a runnable genesis file. It contains no private keys.

A valid devnet-shaped fixture may set `allow_devnet_test_parameters` only with the PROPOSED example chain-id prefix, equal positive validator powers, distinct consensus public keys, empty content and leases, every DEC parameter unapproved, and safety caps equal to `bounds.json`. A file that enables mint, distribution, slashing, staking, or permissionless validator creation is invalid. A file that marks a decision approved is invalid.

## Page 26 failure tests

The blueprint's numeric gates are PROPOSED. This goal does not run them as measurements and does not record them as passed.

| Blueprint test | How v0 expresses the failure | What this goal ran |
| --- | --- | --- |
| Forged, unauthorized, and replayed messages | `forged-signature`, `unauthorized-signer`, `wrong-domain`, `replay-same-business`, `replay-different-payload` | Contract simulation. Not a devnet replay. |
| Quorum: stop one, then two, no false success | `stall-one-validator`, `stall-two-validators`, `liveness-halt-example` | `down` does not change `commit_possible`. Two-down liveness would halt and is not a consensus failure. Not four processes. Not passed as that measurement. |
| Corrupt segment or manifest | `corrupt-manifest`, `replica-match` | Digest compare before acceptance. Not a media worker. |
| Duplicate reservation | `duplicate-lease` | Second accept does not reserve again. |
| Withdrawn content | `withdrawn-blocks-offer`, `withdrawn-blocks-revival` | Tombstone blocks a new offer and a revived id. |
| Idempotency | `idempotent-retry-no-double-reserve`, `underflow-double-end` | One reservation. Not a process restart. |
| Equal application hashes after deterministic replay | `replay-app-hash` runs one committed sequence twice from the same genesis. The application hash is SHA-256 of the SVN-JCS-1 chain-state projection: every machine field except publication, reachability, the stall outbox, derived events, the fixture `down` set, and the transient business slot. DEC-011 keeps `down` outside the hash. `commit_possible` and power-change scheduling do not read it. `pair_index` is stored as a sorted list because its raw key contains `|`. The pinned hash of register, admit, register-content, offer, and accept is `ffee724a8b7944512db064250664d46b7a519d2062134b5cdda9a03ca202437d`. | Contract simulation of the chain-state projection. Not Goal 05's process replay. Not passed as that measurement. |
| Hosting resilience | `hosting-resilience` keeps three designated commitments, marks one host unreachable, leaves the origin out of the chain record, and accepts a repair lease without releasing the other reachable reservations. | Contract simulation. The byte retrieval and repair test is Goal 18. Not run as that measurement. Not passed. |
| Playback, processing, app targets, recovery, moderation timing, operator independence | Later goals. Numbers stay PROPOSED. | Not run. Not passed. |

The same simulation covers overflow, underflow, stale term versions, stale key generations, bounded expiry, and conflicting governance updates (`overflow-reserve`, `underflow-double-end`, `stale-term-version`, `rotate-key-stale-generation`, `bounded-expiry`, `conflict-governance`).

## Invariants and implementation

These are protocol invariants. They are not library choices:

- One live reservation per provider and content version.
- Reserved bytes stay between 0 and declared capacity, with checked arithmetic.
- A business id commits at most one payload.
- Renewal does not change the committed digest or byte count.
- Evidence does not extend a lease. Expired evidence cannot renew current status.
- Withdrawal does not erase history and does not release a reservation by itself.
- Reachability, publication, and the lease obligation are different records.
- Consensus code does not fetch external data and does not read a local clock.
- Fees, mint, distribution, slashing, staking, and `MsgCreateValidator` stay disabled.
- Gate A approved DEC-001 through DEC-012. A new parameter still needs a recorded decision. The on-chain `approved` flag of a blocked-decision parameter stays false.
- A governed limit is stored only when it is at or below that key's ceiling in `governed_limit_caps`. Package bytes, open offers, expiry steps, and evidence batch size then read the stored value for that key.

The executable checks are `docs/evidence/goal-02/check_protocol.py`, `spec_machine.py`, `cases.py`, `jcs.py`, and the Go and TypeScript vector programs. They are specification checks. They are not the chain and they are not a devnet.

## Handoff

Goals 03 through 18 use this v0 contract as frozen-pending-Gate-A. An incompatible change needs a recorded decision. Compatible work is implementing the contract, not editing it in place.

The decisions in "Open decision parameters" were approved at Gate A on 2026-10-31 (America/Toronto). DEC-001 through DEC-012 are approved. DEC-009 is council-only. DEC-010 derives REJECTED. DEC-011 keeps liveness out of the hash. DEC-012 keeps mixed activation kinds as CONFLICT. The devnet test values that match an approved default are no longer an open question. Protocol v0 freezes once this goal passes review. This revision is the candidate. It does not by itself claim that review has passed.

Goal 03 may generate code from `docs/protocol/proto` into the monorepo `proto/` tree. That generation is not a license decision (DEC-006) and not a dependency pin. Goal 05 must reject genesis that enables mint, distribution, slashing, staking, or `MsgCreateValidator`. Goal 07 and Goal 08 implement the registries and the lease machine. Goal 09 must keep the attestation label. Goal 10 must not treat the devnet threshold as the council policy.
