# chain/x/evidence

The evidence module (goal 09). Package `evidence`, module name `evidence`, consensus version 2 (version 1 was the
goal-05 scaffold slot; see "Genesis, export, migrations").

An admitted `AUDITOR` account submits a batch of items (`MsgSubmitAuditBatch`). Each item states one observation of one
held lease: the SHA-256 digest the auditor reports for the lease's package, an expiry, and the round (epoch) in which it
was made. One ed25519 signature by the auditor's own account key covers every item of the batch. The chain checks the
signature, the auditor's role, the round, the ids, the bounds, and the lease and content the item names. Then it
compares the reported digest with the lease's manifest digest and stores one constant-size record per item: `RECORDED`
when the two digests are equal, `REJECTED` when they differ (DEC-010). The chain fetches nothing.

## What an attestation is, and what it is not

An attestation is a signed statement by an admitted key: "this auditor reports this digest for this lease in this
round". A `RECORDED` record means the auditor reported the lease's manifest digest. A `REJECTED` record means the auditor
reported another digest.

It is not:

- a storage proof, a proof of independent storage, or a unique-replica proof (`EvidenceKind` values 3, 4, and 5 are
  reserved and rejected; `TestEvidenceKindHasNoProofValues`);
- proof that the bytes were retrieved: the manifest digest is public (`Lease.manifest_digest`), so an auditor can report
  it without fetching anything;
- proof that the lease is held now, or that the content is available now: a record describes one past observation
  (see "What changes after acceptance");
- proof of future delivery, legality, or a region;
- a renewal, an extension, or a revival of anything: x/evidence writes only its own store, and every renewal that names
  evidence is rejected by x/leases (see "Renewal read").

A valid auditor signature shows which admitted key made the statement. It does not make the statement true. The chain
compared two digests and fetched nothing (DEC-010).

No cryptographic storage proof exists in this beta. It is a separate decision gate (DR-09-G1, below).

The code in this directory (not `_test.go`, not `evidencetest/`) is chain code. It reads block data only from
`sdk.Context`; the block header time, in whole seconds, is its only clock. It iterates stores in key order and never
reads the local clock, randomness, the environment, files, or the network (`TestDeterministicSources`, the
`msgkittest` source scan; goroutines, map ranges, and methods on values stay review items). It consumes the authority,
lease, and content read surfaces through the interfaces in `keeper.go` and never imports x/leases, x/content,
x/providers, x/authority, the scaffold, the app, the devnet, the node, the command, or `agents/`, in code or in tests
(`TestKeeperImports`). It has no BeginBlock and no EndBlock and returns no validator updates (`TestModule`).

## Owner rulings that bind this module

On 2026-11-02 the owner answered the four goal-09 questions Q1-Q4, each with the recommended option, as relayed to
this work package by the goal-09 workflow. The evidence file cites the owner's own message.

- Q1 "Account key signs (Recommended)": the submitting (outer) account must be an `AUDITOR`; the inner attestation
  ed25519 key must derive that account's address; no `BROADCASTER` relay; no rotation; devnet beta only (DR-09-01).
- Q2 "Envelopes + id prefix (Recommended)": the auditor signs canonical SVN-JCS-1 JSON `{"batch_id", "envelopes": [one
  frozen svn-evidence-envelope-v1 per item, in order], "signer"}` under domain `SVN-EVIDENCE-ATTESTATION-1`; every
  evidence id and batch id must start with the first 8 bytes of the auditor's address in lowercase hex plus `.`, else
  `ERROR_CODE_FORBIDDEN`; app genesis refuses two auditors sharing the prefix; no chain id in the payload (DR-09-02,
  DR-09-29, DR-09-30).
- Q3 "Provider-answered only (Recommended)": a producer-side rule (package P2b), not a chain rule: `REJECTED` items are
  filed only for provider-answered wrong or missing bytes; network, TLS, DNS, timeout, and redirect failures stay off
  chain as INCOMPLETE reports (DR-09-09). The chain cannot tell why a digest differs.
- Q4 "Strict, read-time (Recommended)": rounds are 3600 s of block time (`EpochAt`), only the current round is
  accepted; evidence only for a held lease before its deadline, content not withdrawn, content id and version
  matching; `expiry_unix_seconds <= min(now + 86400, lease deadline)`; `EXPIRED` is computed at read time (a record
  reports `EXPIRED` once block time >= expiry); nothing is swept, no begin-block work. 3600 and 86400 are proposed
  constants chosen by the owner through this option (DR-09-03 to DR-09-05, the lifetime half of DR-09-06).

## Files

| File | Content |
| --- | --- |
| `keeper.go` | `Keeper`, `Deps` and the read interfaces, PROPOSED constants, `NewKeeper`, `BusinessDigest`, `RenewalEvidence`, `StoredRecord`, `EpochAt`, `ReportedState` |
| `store.go` | store layout, key builders, window codec (`EncodeWindow`, `DecodeWindow`), record helpers |
| `signdoc.go` | attestation payload: `KindName`, `AuditorAddress`, `IDPrefix`, `OwnsID`, `AuditEnvelope`, `AuditCanonical`, `AuditSignBytes`, `VerifyAuditSignature` (pure; no store access) |
| `msg_server.go` | `SubmitAuditBatch` |
| `query.go` | `Evidence` |
| `genesis.go` | `ParseGenesis`, `MarshalGenesis`, `ValidateGenesis`, `InitGenesis`, `ExportGenesis` |
| `module.go` | `AppModule` (genesis, services, consensus version, migrations; no BeginBlock, no EndBlock) |
| `service.go` | hand-written `grpc.ServiceDesc` for `svn.evidence.v1.Msg` and `svn.evidence.v1.Query` |
| `events.go` | `EvidenceAccepted` and `evidence_state` |
| `evidencetest/` | test support: `CheckInvariants`, `StoreSnapshot`, `PrefixCollisionKeys` (imported only by tests) |
| `testdata/sign-vectors.json` | golden SVN-EVIDENCE-ATTESTATION-1 vectors (public keys only; test-only label keys) |

## Records and their states

```
SubmitAuditBatch ──> RECORDED (digest == lease manifest) ──(block time >= expiry, read time)──> reported EXPIRED
                 └─> REJECTED (any other well-formed digest; never changes)
```

- A record is written once and never rewritten or deleted (I3; DEC-004 keeps chain coordination records).
- The stored state of a message-created record is `RECORDED` or `REJECTED`. `EXPIRED` is a reported state: a query, an
  export, and every label report `EXPIRED` for a `RECORDED` record once the block time reaches its expiry
  (`ReportedState`, D-4). Only a genesis-imported record can be stored `EXPIRED` (export writes the reported state).
- Evidence ids and batch ids are never reused.

## Store layout

Values are frozen protos encoded with `proto.MarshalOptions{Deterministic: true}`, or the fixed-width codecs below.
Every value is non-empty (goleveldb reports an empty value as absent; `msgkit.LayoutVersionKey`).

| Prefix | Key | Value | Lifetime |
| --- | --- | --- | --- |
| `0x00` | `0x00` | layout version 2, 8 bytes BE | written first by `InitGenesis`; never deleted |
| `0x01` | `0x01 \| evidence_id` | `svn.evidence.v1.EvidenceRecord` | permanent; never rewritten (I3) |
| `0x02` | `0x02 \| batch_id` | `0x01` (batch id used) | permanent |
| `0x03` | `0x03 \| business_id` | 32-byte business digest (`msgkit.Ledger`, module `evidence`) | permanent |
| `0x04` | `0x04 \| lease_id \| 0x00 \| evidence_id` | `0x01` | permanent (query by lease) |
| `0x05` | `0x05 \| auditor address (20 bytes)` | epoch (8 bytes BE) \|\| count (8 bytes BE) | one row per auditor that recorded; overwritten by its next batch (D-15) |

Ids match `^[a-z0-9][a-z0-9._-]{0,63}$`, so the `0x00` after a lease id stops `l1` from matching `l10`. A record is
constant size: four ids of at most 64 bytes, a 32-byte digest, a 32-byte public key, fixed scalars
(`TestRecordSizeBound` logs the maximal sizes). One accepted batch of N items writes N records, N lease-index rows, one
batch marker, one business row, and one window row. No expiry index exists (read-time expiry).

## Conventions

- The handler returns a protocol code as an svnerr error (codespace `svn`, code = `svn.types.v1.ErrorCode`).
- First check: `msgkit.Enforce` (gas meter limit outside `[1, 20000000]` `BOUNDS` except in simulation; transaction
  above 65536 bytes `BOUNDS`; fixed 50000 gas; signer not 20 bytes `MALFORMED`).
- Presence rule: an empty string, empty bytes, or a nil message field the spec requires is `MALFORMED`; an integer 0 is
  a value and goes through its own check (`kind` 0 and `attestation` false fail at their own steps).
- Every check runs before the first write. A rejected message writes nothing and emits nothing.
- Batch rule (D-6, DR-09-11): a step that names "each item" runs over the items in message order and the first failing
  item decides; it finishes over every item before the next step starts ("check-major"). For a one-item batch this is
  the spec order. A batch is atomic: one failing item rejects all of them (`TestBatchAtomicD6`).
- Idempotent replay (same business id, same payload): code 0, no write, no typed event, one
  `evidence_result{idempotent="true"}` event. The same business id with another payload is `REPLAY`. Business ids form
  one namespace with the other modules (`msgkit.Peers`). Every check before the gate runs again on a retry: a retry by
  another auditor fails the key binding (`UNAUTHORIZED`, `TestAuditorKeyBindingD2`), and a retry after the epoch
  boundary is `STALE_VERSION` (DR-09-19, `TestEpochRuleD3`, `TestIdempotentReplay`). A client reads the record by
  evidence id before it re-signs.
- Role: `Roles.Role(ctx, signer)`; `SUBMITTER_ROLE_UNSPECIFIED` is "no role".
- Effective limits: `msgkit.Effective(key, Limits.GovernedLimit(ctx, key))` for `max_evidence_batch` and
  `max_page_limit`.
- Committed time: `now = ctx.BlockTime().Unix()`; current epoch `EpochAt(now) = floor(now / 3600)` (0 for now < 1).

### Business payload (`business_gate`, spec_machine.py:257; DR-09-13)

`msgkit.BusinessDigest` over every message field except the signer (spec :964), keyed by proto field name:

```
{"auditor_signature": msgkit.SignatureObject, "batch_id", "business_id", "epoch": int, "schema_version": int,
 "items": [{"attestation": bool, "content_id", "content_version": int, "evidence_id", "expiry_unix_seconds": int,
            "kind": KindName(kind), "lease_id", "observation_digest": msgkit.DigestObject}, ...]}   (message order)
```

Integers are raw JSON integers converted as x/leases `businessUint` does (DR-08-10). Every integer of this payload is
also signed (below), so a value without an SVN-JCS-1 form is `UNAUTHORIZED` at step 8 and the gate's non-canonical
`MALFORMED` branch is not reachable for this message.

### Attestation payload (owner ruling on Q2; DR-09-02; deviation D-1)

Signed bytes: `UTF-8("SVN-EVIDENCE-ATTESTATION-1") || 0x00 || UTF-8(SVN-JCS-1 text)` of

```
{"batch_id": <msg.batch_id>,
 "envelopes": [<one svn-evidence-envelope-v1 object per item, in item order>],
 "signer": "<lowercase hex of the 20-byte outer signer>"}
```

The envelope of item i is exactly the frozen 13-field object of
`docs/protocol/schemas/evidence-envelope.schema.json`:

```
{"attestation": item.attestation, "claim_limit": "ATTESTATION_NOT_STORAGE_PROOF", "content_id", "content_version": int,
 "epoch": msg.epoch, "evidence_id", "expiry_unix_seconds": int, "kind": KindName(item.kind), "lease_id",
 "observation_sha256": "<lowercase hex of the digest value>", "profile": "SVN-JCS-1",
 "schema_version": msg.schema_version, "signer_key_id": auditor_signature.key_id}
```

- `KindName` is the enum name without `EVIDENCE_KIND_` for a named value (`AUDITOR_ATTESTATION`, `PROVIDER_RECEIPT`,
  `UNSPECIFIED`) and the decimal number for any other value (`TestKindName`). It is the one spelling in the envelope,
  the business payload, and events.
- The object binds every attested field of every item, the item order, the batch id, the epoch, the schema version,
  the key label, and the outer signer. Any change after signing is `UNAUTHORIZED` (`TestAlteredObservationD1`). v0
  defines the envelope but no batch wrapper: the wrapper is the deviation from `docs/protocol.md:95`.
- An integer outside the safe range, a nil item or signature, an observation digest that is not a 32-byte SHA-256
  digest, or a signed string (batch_id, evidence_id, lease_id, content_id, key_id) that is not valid UTF-8 has no
  payload (`ErrNoCanonicalForm`): `UNAUTHORIZED`. Any other value builds a payload (kinds 0 and 3-5, `attestation`
  false, `content_version` 0, a negative expiry) and is rejected at its own step (`TestSignPayloadNeverFails`).
- The UTF-8 rule keeps the payload injective and names the failure. The SVN-JCS-1 encoder (`chain/jcs`) also refuses
  invalid UTF-8; before goal 09 changed it, it wrote each invalid byte as U+FFFD, so without the rule a signature over
  `c1\uFFFD` would also have verified for the ids `c1\xff` and `c1\xfe`, which were never signed. The rule runs before
  the encoder, so such a message is `ErrNoCanonicalForm`, never a size error. A transaction cannot carry such a string
  (protobuf-go refuses invalid UTF-8 in a proto3 string, and step 9 refuses it), but `VerifyAuditSignature` is also the
  producer's and the reviewer's check (`TestAttestationPayloadRejectsInvalidUTF8`).
- The canonical text of 32 maximal items is below 65536 bytes; a payload above 65536 bytes is `BOUNDS` at step 8, which
  needs more than 32 items (`TestSignPayloadSizeBound` logs both sizes). `AuditCanonical` canonicalizes envelope by
  envelope and stops once the running length passes 65536 bytes, so the work of step 8 is bounded by the cap.
- No chain id is signed (owner ruling; as DR-07-01 and DR-08-04); no business id (a retry handle, not attested
  content).
- Key separation (Q1): the auditor's ed25519 account key also signs its SDK transactions. The app enables only
  `SIGN_MODE_DIRECT` (`chain/app/encoding.go`), whose preimage is a deterministic protobuf `SignDoc` beginning with a
  field tag (`0x0a`, `0x12`, `0x1a`, or `0x20`); the attestation preimage begins with `S` (`0x53`) and never `{`, and
  does not decode as a strict `SignDoc` (`TestAttestationPreimageIsNotASignDoc`). This separation argument is
  unreviewed; the ruling limits it to the devnet beta.
- Exported pure builders, used by the producer and the reviewer so producer and keeper cannot drift: `AuditEnvelope`,
  `AuditCanonical`, `AuditSignBytes`, `VerifyAuditSignature`, `AuditorAddress`, `IDPrefix`, `OwnsID`, `KindName`, `EpochAt`.
  Golden vectors: `testdata/sign-vectors.json` (1 item, 2 items, 32 items, an unknown kind number, maximal ids, a
  second auditor; `TestSignBytesVectors`, `-update` rewrites it). TEST-ONLY keys: the ed25519 seed is
  `sha256("svn-evidence-test-seed:" + label)`; the file holds public keys only.

## SubmitAuditBatch: check order (op_audit, spec_machine.py:908)

Steps 1-18 are the spec checks in spec order (with the chain's batch, presence, and signature rules); steps 19-27 (T1-T9)
are chain-only and run after every spec step, as x/leases does, so the tail changes no spec code. Steps 1-18 keep the
spec code except where D-1, D-2, D-3, D-5, D-6, D-7, and D-19 (spec step 1, unknown fields) state otherwise; the Spec
column names the spec step and any chain-only part of a row.

| # | Check | Code | Spec / deviation |
| --- | --- | --- | --- |
| 1 | `msgkit.Enforce`: gas limit, tx size / signer not 20 bytes | `BOUNDS` / `MALFORMED` | D-7 |
| 2 | business_id, batch_id empty; auditor_signature nil; each item nil, or its evidence_id, lease_id, content_id empty, or observation_digest nil | `MALFORMED` | require_fields :925; D-7 |
| 3 | signer role: none / not `AUDITOR` (a `BROADCASTER` relay included) | `UNAUTHORIZED` / `FORBIDDEN` | check_role :928; Q1 |
| 4 | schema_version 0 / not 1 | `MALFORMED` / `OUT_OF_RANGE` | D-5 |
| 5 | each item observation_digest: algorithm 0 or unknown / value not 32 bytes | `MALFORMED` / `BOUNDS` | check_digest :931 (value length); algorithm 0 or unknown `MALFORMED` is chain-only: the spec digest is a hex string with no algorithm (DR-09-14; `docs/protocol.md:63`) |
| 6 | auditor_signature structure: domain 0 or unknown; another known domain; profile 0 / not 1; key_id empty / off pattern; public key not 32 or signature not 64 bytes | `MALFORMED`; `WRONG_DOMAIN`; `MALFORMED` / `OUT_OF_RANGE`; `MALFORMED` / `BOUNDS`; `BOUNDS` | check_signature :937 (domain); the profile, key, and signature lengths are chain-only; the key_id rows replace the spec's `key_id != "key-auditor"` `UNAUTHORIZED` (:250; D-2) |
| 7 | `AuditorAddress(public key) != signer` | `UNAUTHORIZED` | D-2 (Q1) |
| 8 | payload with no SVN-JCS-1 form (an unsafe integer, a signed string that is not UTF-8) / above 65536 bytes / ed25519 failure over `AuditSignBytes` | `UNAUTHORIZED` / `BOUNDS` / `UNAUTHORIZED` | :237-254; D-1, D-6 |
| 9 | batch_id, then each item evidence_id, lease_id, content_id outside the id pattern | `BOUNDS` | :942 |
| 10 | each item kind not 1 or 2 (0, the reserved 3-5, any other number) | `MALFORMED` | :946 |
| 11 | item count < 1 or > effective `max_evidence_batch` (ceiling 32) | `BOUNDS` | :954 |
| 12 | each item attestation false | `MALFORMED` | :956 |
| 13 | epoch != `EpochAt(now)` (an expired or a future round) | `STALE_VERSION` | :958; D-3 (Q4) |
| 14 | each item expiry outside `[1, 253402300799]` | `BOUNDS` | :961 |
| 15 | business gate: business_id off pattern / no canonical form (unreachable after step 8) / known other digest / known same digest | `BOUNDS` / `MALFORMED` / `REPLAY` / idempotent success | :964 |
| 16 | batch_id used, or an item evidence_id stored or repeated earlier in the batch | `DUPLICATE_ID` | :967; D-6 |
| 17 | each item expiry <= now | `EXPIRED` | :969 |
| 18 | each item lease absent | `NOT_FOUND` | :971 |
| 19 | T1 batch_id or an item evidence_id does not start with `IDPrefix(signer)` (`OwnsID`, false for an address that is not 20 bytes: fail closed) | `FORBIDDEN` | D-17 (Q2) |
| 20 | T2 each item kind `PROVIDER_RECEIPT` | `FORBIDDEN` | D-8 (DR-09-07) |
| 21 | T3 each item expiry - now > `MaxEvidenceLifetimeSeconds` (86400) | `BOUNDS` | D-9 (Q4) |
| 22 | T4 each item content_id or content_version differs from its lease | `CONFLICT` | D-10 (Q4) |
| 23 | T5 each item lease not held (not `ACTIVE`/`RENEWED` with reservation_held) | `CONFLICT` | D-11 (Q4) |
| 24 | T6 each item lease deadline <= now (held, not yet swept) | `EXPIRED` | D-12 (Q4) |
| 25 | T7 each item expiry > its lease deadline | `CONFLICT` | D-18 (Q4) |
| 26 | T8 each item lease content WITHDRAWN / absent (an invariant violation) | `WITHDRAWN` / `NOT_FOUND` | D-13 (Q4) |
| 27 | T9 the signer's items in the current epoch + item count > `MaxEvidencePerAuditorPerEpoch` (1024) (a stored window ahead of the current epoch is an invariant violation and panics; `InitGenesis` refuses later-epoch records, so it is unreachable) | `BOUNDS` | D-15 (DR-09-06) |

The spec's defensive `CONFLICT` (:990, lease changed during the step) has no chain step: x/evidence cannot write the
leases store (`TestAuditLeavesOtherStoresUntouched`).

Spec step 1 (`unknown`, :925 with :138-142: a key outside the step's fields is `UNKNOWN_FIELD`; case
evidence-reject-flag-unknown, cases.py:1044, a submitter `reject` flag) has no keeper step: a decoded message has no
unknown key left to see. `EvidenceItem` has exactly its eight fields and reserves field 9 and the name `reject`
(`docs/protocol/proto/svn/evidence/v1/tx.proto`; `TestEvidenceItemHasNoRejectFlag`), and the app's strict decoder
rejects any unknown field before the handler with SDK codespace `sdk` code 2, not `ERROR_CODE_UNKNOWN_FIELD`
(`docs/architecture/registries.md`, where the mapping is a follow-up). This is D-19; its app test is INT's
`TestEvidenceMessagesDecode`. The `schema_version` field, which the spec does not have, is D-5.

### Negative cases

Every row below is a row of the ladder `TestSubmitAuditBatchCheckOrder`. It runs each row twice on a cached branch:
"cumulative/<row>" applies the row's fault with every later fault present (it proves the order) and "isolated/<row>"
applies it alone (it proves the row reaches its own branch). Each rejected row asserts the exact code, byte-identical
evidence, leases, and content stores, and no event; "no-fault" commits. `TestTrialSnapshotDetectsEarlyWrite` shows the
trial check sees a write or an event before a failure; `TestLadderCoversEveryCode` checks every code the handler
returns has a row. The last column names the ladder row and any focused test.

| Step | Condition | Code | Ladder row; focused test |
| --- | --- | --- | --- |
| 1 | gas meter limit 0 | `BOUNDS` | enforce-gas-limit-0 |
| 1 | transaction of 65537 bytes | `BOUNDS` | enforce-tx-65537-bytes |
| 1 | signer of 19 bytes | `MALFORMED` | enforce-signer-19-bytes |
| 2 | business_id empty | `MALFORMED` | presence-business-id |
| 2 | batch_id empty | `MALFORMED` | presence-batch-id |
| 2 | auditor_signature nil | `MALFORMED` | presence-auditor-signature |
| 2 | an item nil | `MALFORMED` | presence-item-nil |
| 2 | evidence_id, lease_id, content_id empty | `MALFORMED` | presence-evidence-id, presence-lease-id, presence-content-id |
| 2 | observation_digest nil | `MALFORMED` | presence-observation-digest |
| 3 | signer without a role | `UNAUTHORIZED` | role-none |
| 3 | signer ADMITTED_SERVICE, ADMITTED_PROVIDER, AUTHORITY | `FORBIDDEN` | role-service, role-provider, role-authority |
| 3 | signer BROADCASTER relaying a valid auditor-signed batch (Q1: no relay) | `FORBIDDEN` | role-broadcaster-relay; `TestBroadcasterRelayForbidden` |
| 4 | schema_version 0 / 2 (unknown schema) | `MALFORMED` / `OUT_OF_RANGE` | schema-version-0, schema-version-2; `TestSchemaVersionD5` |
| 5 | digest algorithm 0 / 7 | `MALFORMED` | digest-algorithm-0, digest-algorithm-7 |
| 5 | digest value 31 / 33 bytes | `BOUNDS` | digest-31-bytes, digest-33-bytes |
| 6 | signature domain 0 / 99 | `MALFORMED` | sig-domain-unspecified, sig-domain-unknown-99 |
| 6 | signature domain PROVIDER_ACCEPT | `WRONG_DOMAIN` | sig-wrong-domain |
| 6 | profile 0 / 2 | `MALFORMED` / `OUT_OF_RANGE` | sig-profile-0, sig-profile-2 |
| 6 | key_id empty / off pattern | `MALFORMED` / `BOUNDS` | sig-key-id-empty, sig-key-id-pattern |
| 6 | public key 31 bytes; signature 63 bytes | `BOUNDS` | sig-public-key-31, sig-signature-63 |
| 7 | a valid signature by another auditor's key; by a provider's key (key does not derive the signer) | `UNAUTHORIZED` | binding-other-auditor-key, binding-provider-key; `TestAuditorKeyBindingD2` |
| 8 | content_version 2^53, 2^63, 2^64-1; epoch 2^53; expiry -2^63 (no canonical form) | `UNAUTHORIZED` | payload-content-version-2^53, payload-content-version-2^63, payload-content-version-max-uint64, payload-epoch-2^53, payload-expiry-min-int64 |
| 8 | canonical payload above 65536 bytes (transaction below 65536 bytes) | `BOUNDS` | payload-above-65536-bytes; `TestSignPayloadSizeBound` |
| 8 | ed25519 failure; any signed field altered after signing; a signed string that is not UTF-8 | `UNAUTHORIZED` | payload-ed25519-failure; `TestAlteredObservationD1`, `TestSpecIntegrityCaseIsUnauthorizedD1`, `TestAttestationPayloadRejectsInvalidUTF8` |
| 9 | batch_id, evidence_id, lease_id, content_id off pattern | `BOUNDS` | batch-id-pattern, evidence-id-pattern, lease-id-pattern, content-id-pattern |
| 10 | kind 0, 3, 4, 5, 7 (unknown and reserved kinds) | `MALFORMED` | kind-0, kind-3-reserved-storage-proof, kind-4-reserved-unique-replica, kind-5-reserved-future-delivery, kind-7-unknown; `TestEvidenceKindHasNoProofValues` |
| 11 | 0 items (empty batch); 33 items (oversized batch); 3 items under a governed limit of 2 | `BOUNDS` | count-0, count-33, count-above-governed-2; `TestBatchSizeAndGovernedLimit` |
| 12 | attestation false | `MALFORMED` | attestation-false |
| 13 | epoch current-1 (expired round), current+1 (future round), 0 | `STALE_VERSION` | epoch-expired-round, epoch-future-round, epoch-0; `TestEpochRuleD3` |
| 14 | expiry 0, -1, 253402300800 | `BOUNDS` | expiry-0, expiry-negative, expiry-above-9999; `TestExpiryRangeEdges` (1 and 253402300799 pass step 14: `EXPIRED` at 17, `NOT_FOUND` at 18) |
| 15 | business_id off pattern; business_id not UTF-8 (keeper path only: a transaction cannot carry it) | `BOUNDS` | gate-business-id-pattern, gate-business-id-not-utf8 |
| 15 | business id committed by another module with another payload; same id, changed payload | `REPLAY` | gate-replay-other-module; `TestDuplicateIDs`, `TestIdempotentReplay`, `TestBusinessIDTakenFirst` |
| 16 | batch id used | `DUPLICATE_ID` | duplicate-batch-id-used; `TestDuplicateIDs` |
| 16 | evidence id stored (on the first, the second, or the last item) | `DUPLICATE_ID` | duplicate-evidence-id-stored; `TestDuplicateIDs` |
| 16 | evidence id repeated inside the batch | `DUPLICATE_ID` | duplicate-evidence-id-in-batch; `TestDuplicateInBatchD6` |
| 17 | item expiry == now | `EXPIRED` | item-expiry-at-committed-time |
| 18 | lease absent | `NOT_FOUND` | lease-not-found |
| T1 | batch id with another auditor's prefix; evidence id with another prefix; evidence id without a prefix | `FORBIDDEN` | t1-batch-id-foreign-prefix, t1-evidence-id-foreign-prefix, t1-evidence-id-no-prefix; `TestForeignAuditorCannotTakeEvidenceIDsD17` |
| T2 | kind PROVIDER_RECEIPT | `FORBIDDEN` | t2-kind-provider-receipt; `TestProviderReceiptNotOnChainD8` |
| T3 | expiry = now + 86401 | `BOUNDS` | t3-expiry-now-plus-86401; `TestEvidenceLifetimeBoundD9` |
| T4 | content id differs; content version differs | `CONFLICT` | t4-content-id-differs, t4-content-version-differs; `TestLeaseContentBindingD10` |
| T5 | lease ENDED; lease EXPIRED; reservation not held | `CONFLICT` | t5-lease-ended, t5-lease-expired, t5-reservation-not-held; `TestLeaseNotHeldD11` |
| T6 | lease deadline == now (past deadline, not yet swept) | `EXPIRED` | t6-lease-deadline-at-committed-time; `TestLeaseDeadlinePassedD12` |
| T7 | expiry one second after the lease deadline | `CONFLICT` | t7-expiry-after-lease-deadline; `TestEvidenceExpiryWithinLeaseD18` |
| T8 | content withdrawn; content absent | `WITHDRAWN` / `NOT_FOUND` | t8-content-withdrawn, t8-content-not-found; `TestContentWithdrawnD13` |
| T9 | auditor window at the cap | `BOUNDS` | t9-auditor-epoch-cap; `TestAuditorEpochCapD15` |

The ladder puts its item faults on the first item. `TestItemFaultsOnLastItem` runs every one of them (32) alone on the
last item of the two-item batch, plus a nil last item, a last evidence id that carries the signer prefix but breaks the
id pattern, and a last evidence id repeating the first: every step that names "each item" checks every item, with the
ladder's code and no write.

Further negative and boundary cases outside the ladder: `TestBatchCheckMajorOrderD6` (an item that fails an earlier
step decides over an earlier item that fails a later step; the second item fails steps 2, 5, 9, 10, 12, 14, 17, and
18), `TestBatchAtomicD6` (the last of 32 items failing each tail check writes nothing), `TestIdempotentReplay` (replay
in another transaction is idempotent; a changed payload is `REPLAY`; after the epoch boundary `STALE_VERSION`),
`TestNoEvidenceRevivesAnything` (stale audits of an expired lease, withdrawn content, or a lease past its deadline write
nothing anywhere), `TestStoreInvariantViolationsFailClosed` (undecodable records and windows, and a window ahead of the
current epoch, panic; a window above the cap is `BOUNDS`).

Unreachable through the app, asserted only in keeper tests (the app-level list with reasons belongs to INT): step 1's
signer-length row (the ante ties the signer to the transaction key's 20-byte address), step 3's no-role row (the ante
refuses a signer without a submitter row), and T8's `NOT_FOUND` row (a lease names registered content).

### Writes (only after step 27)

For each item in message order: `0x01|evidence_id` = `EvidenceRecord{evidence_id, batch_id, kind, state, signer_pubkey =
auditor_signature.public_key, lease_id, content_id, content_version, observation_digest (SHA-256, 32 bytes), epoch =
msg.epoch, expiry_unix_seconds, schema_version = 1, attestation = true}`, with state `RECORDED` when the observation
digest equals the lease's manifest digest (algorithm and value; `TestRecordStateNeedsTheDigestAlgorithm`) and `REJECTED`
otherwise (DEC-010); and
`0x04|lease_id|0x00|evidence_id`. Then `0x02|batch_id`, the window row `0x05|signer` = (current epoch, items already in
this epoch + N), the business id, and the events. Nothing else: no lease, provider, content, or authority write, no
reservation, no deadline change (`TestRecordFieldsAndDerivedState`).

Compared with the spec record (spec_machine.py:979-988: batch_id, kind, state, lease_id, content_id,
expiry_unix_seconds, observation_digest, attestation), the chain record also stores evidence_id, signer_pubkey,
content_version, epoch, and schema_version (frozen `state.proto` fields). `kind` is the enum (spec: the short name) and
`observation_digest` a `Digest` message (spec: lowercase hex). A differential projection compares only the spec's eight
fields, with kind as `KindName` and the digest as the hex of its value.

## What changes after acceptance

| Object | Changed by SubmitAuditBatch | Changed later |
| --- | --- | --- |
| Evidence record (all stored fields) | written once | never: no message or block rewrites or deletes it (I3) |
| Reported state (query, export, labels) | `RECORDED` or `REJECTED` | a `RECORDED` record reports `EXPIRED` from the first committed time >= its expiry (read time, D-4); `REJECTED` and `EXPIRED` never change |
| Batch marker, business id | added | never removed; ids are never reusable |
| Auditor window row | epoch set to the current epoch, count += N | overwritten by the auditor's next batch in a later epoch |
| Lease (state, deadline, term, reservation, pair index) | never | only by x/leases messages and lease expiry; a renewal naming evidence is always rejected |
| Provider record, reserved bytes | never | only by x/providers and x/leases |
| Content record, tombstone | never | only by x/content |
| Authority state, governed limits | never | only by x/authority |

What this shows, and what it does not, about "a stale or passing audit must not revive expired or withdrawn hosting":

- No audit can change a lease, provider, or content record: x/evidence has no write path to those stores (I9,
  `TestAuditLeavesOtherStoresUntouched`, `TestNoEvidenceRevivesAnything`).
- No audit can renew or extend a lease: every renewal that names evidence is rejected (see "Renewal read").
- No new record can be written for a lease that is not held, is past its deadline, or whose content is withdrawn
  (T5, T6, T8), and no record's expiry is after its lease's deadline at recording (T7).
- The limit: a `RECORDED` record written while the lease was held keeps reporting `RECORDED` until its own expiry, also
  after a later `EndLease`, `WithdrawContent`, or lease expiry: at most 86400 s, never past the lease's deadline at
  recording. Such a record describes one past observation. It does not show that the lease is held or that the content
  is available now; the label says so (`TestRecordedEvidenceAfterEndWithdrawExpiry` queries the record by lease after
  each of the three events). The read side is covered by the label and the bounded expiry, not by a state change.

## Renewal read (handoff to x/leases)

`Keeper.RenewalEvidence(ctx, evidenceID) (expiry, expired, found)` implements `leases.Evidence`. An id outside the id
pattern is not found without a store read; an absent record is not found; a present record returns its expiry and
whether its stored state is `EXPIRED` (only a genesis-imported record). x/leases then returns `EXPIRED` when `expired ||
expiry <= now` and `FORBIDDEN` otherwise (`chain/x/leases/msg_server.go`, renew check 13). Outcomes: `RECORDED` or
`REJECTED` with expiry > now: `FORBIDDEN`; any record with expiry <= now: `EXPIRED`; absent: `NOT_FOUND`. These are the
spec's outcomes (spec :842-848). No evidence state renews or extends a lease (`TestReportedStateD4` checks the values;
the app-level `TestNoEvidenceRenews` is INT's).

## Events

| Source | Event | Attributes |
| --- | --- | --- |
| SubmitAuditBatch, per item, in item order | `svn.events.v1.EvidenceAccepted` (typed) | evidence_id, batch_id, kind (short name, e.g. `AUDITOR_ATTESTATION`), attestation `true` |
| same, right after each typed event | `evidence_state` (untyped; not in svn.events.v1; D-16) | evidence_id, lease_id, state `RECORDED` or `REJECTED` |
| idempotent replay | `evidence_result` | idempotent `true` (the only event of the message) |
| expiry | none: nothing is swept (D-4) | |

`EvidenceAccepted` means the record was written to the ledger. It does not mean the check passed (DR-09-16; a typed
state field is a protocol addition, DR-09-28). The SDK appends `msg_index` to transaction events
(`msgkit.ParseCommitted`). A rejected message emits nothing. Events are indexer input, not app-hash input. Event
shapes and order: `TestEvidenceEvents`. Event and attribute names pass the bounds.json forbidden-name scan
(`TestNoForbiddenNames`).

## Query

`svn.evidence.v1.Query/Evidence`, registered on the gRPC query router by `RegisterServices` (interim semantics,
DR-09-17). Rejections are `Code.QueryErr` (gRPC status with an `ErrorInfo` detail; `msgkit.QueryCode` reads it); a store
invariant violation is a plain error.

- Neither `evidence_id` nor `lease_id`: `MALFORMED`. A non-empty id outside the pattern: `BOUNDS` (evidence_id first).
- Page: `msgkit.CheckPage` against the effective `max_page_limit` (at most 100); a nil page or limit 0 uses the cap; a
  limit above it, a page key above 64 bytes or outside the id pattern: `BOUNDS`.
- `evidence_id` (alone or with `lease_id`): 0 or 1 record (with `lease_id`, only a record of that lease); a non-empty
  page key is `BOUNDS`; `next_page_key` is empty. An absent id is an empty list with code 0.
- `lease_id` only: the records of the lease in evidence id byte order over `0x04|lease_id|0x00`; the page key is the
  inclusive evidence id to start at; `next_page_key` is the next evidence id, empty on the last page. Work: at most
  limit+1 index steps and one record read per item.
- Every returned record carries its REPORTED state (`ReportedState` at the query context's block time, D-4). SDK
  query-time rule: for the latest height that is the header time of the committed state; for a historical height it
  is that height's commit-info time when `GetCommitInfo(height)` succeeds, and otherwise the latest header time
  (`cosmos-sdk@v0.55.0/baseapp/abci.go`). So a historical query can report `EXPIRED` for a record that was `RECORDED` at
  that height when the node has no commit info for it (residual risk). The keeper tests set the context time by hand,
  which does not test the SDK path; the ABCI-level test (`TestEvidenceQueryTimeThroughABCI`) is INT's.
- A context without a block time (a header time below 1) cannot report a state, so it fails closed: a query that would
  return a record is `ErrNoBlockTime` (a plain error), never the stored state. Such a context is the check-state header
  `{ChainID}` right after a node restart (`baseapp.go` `Init`), which a historical query uses when the node has no
  commit info for that height and a latest-height query uses when the node skips the gRPC header check; and the
  `App.Export` context. A read that returns no record (an absent id, a lease without records) succeeds
  (`TestReadsWithoutBlockTimeFailClosed`).
- A page of 100 maximal records is bounded by construction (`TestEvidenceQueryResponseSizeBound` asserts it below
  64 KiB and logs the size). Tests: `TestEvidenceQuery`, `FuzzEvidencePageKey`.
- No query by batch, epoch, auditor, or state, and no epoch query (DR-09-27): a client computes `EpochAt(latest header
  time)`. Raw `/store/evidence/...` ABCI queries bypass the router: no gas meter, no page cap, and they show the stored
  state, not the reported state (residual risk).

## Genesis, export, migrations

- `ParseGenesis`: strict protojson (unknown fields are errors; an empty document is an error; `{}` is the empty
  state). `MarshalGenesis`: proto names, unpopulated fields emitted.
- `ValidateGenesis` (pure): `current_epoch` 0 (the epoch is derived from the header time, D-3); evidence_id unique;
  evidence_id, batch_id, lease_id, content_id in the pattern; kind `AUDITOR_ATTESTATION` (D-8); state `RECORDED`,
  `EXPIRED`, or `REJECTED`; signer_pubkey 32 bytes; evidence_id and batch_id start with
  `IDPrefix(AuditorAddress(signer_pubkey))` (D-17); content_version in `[1, 2^53-1]`; observation_digest a 32-byte
  SHA-256 digest; expiry in `[1, 253402300799]`; epoch at most `EpochAt(253402300799)` and
  `epoch*3600 < expiry < (epoch+1)*3600 + 86400` (every recorded item satisfies it: its expiry is after its committed
  time and at most that time plus the lifetime); schema_version 1; attestation true; the records of one batch share
  their signer key and epoch and are at most 32; at most 1024 records per (auditor address, epoch); no unknown fields
  (`TestGenesisValidateImportExport`, one invalid document per rule). The batch signer-key rule sits behind the
  record-level prefix rule: two label keys never share a prefix, so `TestGenesisBatchConsistency` reaches it alone with
  two synthetic 32-byte keys whose addresses share their first 8 bytes (`evidencetest.PrefixCollisionKeys`, found once
  off chain by a 64-bit collision search; no private key exists for them), and reaches the epoch half alone too.
- `InitGenesis`: requires an empty store (the scaffold key `[0x01]`, a layout key, or any row is an error). Before any
  write it checks through Deps (x/leases imports first): the record's lease exists with the record's content id and
  version (I6); a `RECORDED` or `EXPIRED` record's digest equals the lease's manifest digest and a `REJECTED` record's
  differs (I5); the expiry is at most the lease's deadline (I13); the record's epoch is at most `EpochAt(genesis block
  time)`, so no auditor window is ahead of the current epoch. Then it writes the layout key, the records in id order
  with their lease index rows, the batch markers, and one window row per auditor (its largest record epoch and the
  number of its records in that epoch).
- What module genesis cannot check: that a record's auditor holds the `AUDITOR` role. x/authority imports its genesis
  after x/evidence (`chain/app/app.go` `InitOrder`), and genesis records carry no signature, so an imported record can
  name any 32-byte key (a provider's own key included) as its attesting auditor, against the Q1 rule. The app's launch
  rule keeps evidence genesis empty (`chain/app/state.go` `emptyCatalog`); if non-empty evidence genesis is ever
  allowed, app genesis validation must require every record's `AuditorAddress(signer_pubkey)` to be an `AUDITOR`
  submitter of the same document (an app-level invariant next to I12; INT).
- `ExportGenesis`: every record in id order with its REPORTED state at the export context's block time, and
  `current_epoch` 0. A record past its expiry is exported `EXPIRED`. A context without a block time exports an empty
  store as the empty state and refuses a store with any record (`ErrNoBlockTime`; `App.Export` builds its context with a
  zero header time today, `chain/app/app.go`), so the stored state is never exported in place of the reported one
  (`TestReadsWithoutBlockTimeFailClosed`). Not exported (a recorded, tested restriction,
  DR-07-03, DR-09-18): the business-id ledger; a chain started from an export forgets those business ids, and a retry of
  an exported batch is then `DUPLICATE_ID` (batch id used), not idempotent. Every other row is derived and rebuilt
  (`TestEvidenceGenesisRoundTrip`: equal reported states; stored bytes equal except exactly the `RECORDED`-to-`EXPIRED`
  change of records past expiry; equal derived rows; no business rows). Batch ids are derivable because every accepted
  batch writes at least one record and records are never deleted. `FuzzEvidenceGenesisJSON` checks parse, validate,
  import, invariants, and re-import of the export.
- App level (INT): launch genesis stays empty (`chain/app/state.go` launch rule); `App.Export` stays refused while any
  record exists; app-level re-import of a non-empty export is BLOCKED (DR-07-02, DR-07-03, DR-06-01).
- `ConsensusVersion` 2. Version 1 (`msgkit.ScaffoldLayoutVersion`) was the scaffold slot (`[0x01]` holding the genesis
  JSON, inside this layout's record prefix). No migration from 1 is registered: `RunMigrations` from 1 fails and the
  store is untouched (`TestNoMigrationFromScaffoldLayout`). The table covers 2..ConsensusVersion-1 and is empty
  (`TestMigrationTable`, `TestRunMigrationsAtCurrentVersionIsANoOp`).

## Invariants

| Id | Invariant | Where it is checked |
| --- | --- | --- |
| I1 | every record has its lease index row, and every lease index row names a record of that lease | `evidencetest.CheckInvariants` |
| I2 | every record's batch id has a marker, every marker has a record; the records of one batch share signer key and epoch and are at most 32 | `CheckInvariants`; `ValidateGenesis` |
| I3 | the stored bytes of a record never change after its write | `TestReportedStateD4` (reads change nothing), `TestEvidenceGenesisRoundTrip`; no code path writes `0x01` for an existing id (step 16) |
| I4 | stored state `RECORDED` or `REJECTED` for message-created records; `EXPIRED` only for genesis-imported ones | `CheckInvariants` (state set); `TestRecordFieldsAndDerivedState` |
| I5 | `RECORDED` or `EXPIRED` iff the digest equals the lease's manifest digest; `REJECTED` iff it differs (the lease digest never changes) | `CheckInvariants`; `InitGenesis` |
| I6 | a record's content id and version equal its lease's | `CheckInvariants`; T4; `InitGenesis` |
| I7 | attestation true, schema_version 1, signer_pubkey 32 bytes, kind `AUDITOR_ATTESTATION`, ids in the pattern, SHA-256 digest | `CheckInvariants`; `ValidateGenesis` |
| I8 | one window row per auditor with records: its largest record epoch and its records in that epoch, at most the cap | `CheckInvariants`; `InitGenesis` |
| I9 | x/evidence writes no other module's store | `TestAuditLeavesOtherStoresUntouched`, `TestNoEvidenceRevivesAnything`, `FuzzAuditStateSequence` |
| I10 | a business id commits at most one effect; evidence and batch ids are never reused | ladder `REPLAY` and `DUPLICATE_ID` rows; `TestIdempotentReplay`, `TestDuplicateIDs` |
| I11 | no clock other than the block header, no network, no file IO in non-test code | `TestDeterministicSources`, `TestKeeperImports` |
| I12 | every evidence id and batch id starts with the `IDPrefix` of its record's auditor | `CheckInvariants`; T1; `ValidateGenesis` |
| I13 | a record's expiry is at most its lease's deadline | `CheckInvariants`; T7; `InitGenesis` |

I13 holds forever because no x/leases path lowers a lease deadline. Checked on this tree: the only non-test write of
`Lease.DeadlineUnixSeconds` is in `RenewLease` (`chain/x/leases/msg_server.go`), where the new deadline must equal the
current deadline plus the DEC-005 term, and the term must be positive (`checkTerm`: a missing or 0 term is
`DECISION_BLOCKED`, a non-positive difference `OUT_OF_RANGE`). `AcceptLease` copies the offer's deadline into a new
lease; expiry and `EndLease` change the state and the reservation, not the deadline; the withdrawal hook voids offers
only; `InitGenesis` writes imported leases unchanged; no other module writes the leases store.

App level (INT), next to I12: every record's `AuditorAddress(signer_pubkey)` is an `AUDITOR` submitter. Module genesis
cannot check it (see "Genesis, export, migrations"); the launch rule keeps evidence genesis empty.

`evidencetest.CheckInvariants(ctx, keeper, leases)` checks I1, I2, I4-I8, I12, I13, the layout key, the business rows,
and unknown prefixes over one context, committed or cached; its error names the invariant. Its self-test
(`TestCheckInvariantsDetectsEachViolation`, 44 cases and a nil lease surface) breaks one rule per case and expects the
invariant's name; every invariant it checks has at least one case, and so does every clause of I2, I6, and I8. The
cases include the I2 batch above 32 records and the I2 batch of two signer keys (reached alone with
`evidencetest.PrefixCollisionKeys`, since the I12 prefix rule refuses two label keys first), the I6 content_version
mismatch, the I7 expiry above 253402300799, the I1 index row without a separator, the I8 window count above the cap,
and a business row under an off-pattern id. Not every field of I7 has its own case (one id-pattern case covers the id
loop).

## Bounds and PROPOSED constants

| Bound | Value | Source | Status |
| --- | --- | --- | --- |
| Items per batch | 1..effective `max_evidence_batch` (ceiling 32) | bounds.json `safety_max_evidence_batch`, governed cap | protocol cap, accepted at Gate A |
| Transaction / gas | 65536 bytes; gas limit 1..20000000; fixed 50000 per message | `msgkit.Enforce` | goal 07 |
| Ids | pattern, 1..64 bytes; evidence and batch ids carry the signer prefix | bounds.json; D-17 | protocol; prefix by owner ruling Q2 (DR-09-29) |
| Digests | SHA-256, 32 bytes | bounds.json | protocol |
| Inner signature | key 32, signature 64 bytes, key_id pattern, profile 1 | bounds.json | protocol |
| Attestation payload | <= 65536 canonical bytes | SVN-JCS-1 cap | protocol |
| Epoch | == `EpochAt(now)`, `EpochSeconds` = 3600 | D-3 | PROPOSED, chosen by the owner through Q4 (DR-09-03); compiled into every chain; changing it is a consensus change that changes the meaning of every stored epoch |
| Expiry | `[1, 253402300799]`, > now, <= now + `MaxEvidenceLifetimeSeconds` (86400), <= lease deadline | D-9, D-18 | 86400 PROPOSED, chosen by the owner through Q4 (DR-09-06, lifetime half) |
| Items per auditor per epoch | `MaxEvidencePerAuditorPerEpoch` = 1024 (open window only; resets each epoch) | D-15 | PROPOSED, fail-closed interim, DR-09-06 (numbers) open |
| Page | effective `max_page_limit` (<= 100); key <= 64 bytes | bounds.json | protocol cap |
| Per-block work | none (no BeginBlock) | D-4 | owner ruling Q4 |
| Records | never deleted | DEC-004 | growth bounded per auditor per epoch, not over time |

The three PROPOSED constants are compiled into every chain, devnet or not, as x/leases' chain safety constants are.
They are not devnet-only and not policy; a new parameter needs a recorded decision (`docs/protocol.md`).

State growth: the cap allows at most 1024 items per auditor per epoch of 3600 s, so at most 1024 x 24 x 365 =
8,970,240 records per auditor per year, plus the same number of lease index rows. The yearly logical growth bound per
auditor is 8,970,240 x (bytes per item + bytes per batch), with at most one batch per item (1-item batches); the per-item
bytes (record row and lease index row) and per-batch bytes (batch marker and business row) of maximal records are
logged by `TestRecordSizeBound` and the evidence file states the product from that log. These are logical KV bytes of
the evidence store, not IAVL or disk bytes, and a bound, not a forecast. Growth over time is unbounded: records are
never deleted. DR-09-06 (b) (one item per auditor, lease, and epoch) would bound growth by the held leases instead.

Gas and work: `msgkit.Enforce` charges a flat 50000 gas per message plus metered store gas, so gas does not reflect
ed25519 verification or canonicalization; gas is not CPU time. Steps 2-10 walk every item, and step 8 canonicalizes
and verifies before the count check (step 11), so a rejected message with many items does the most unpaid work: the
items are bounded only by the 65536-byte transaction, and step 8 stops canonicalizing at the 65536-byte payload cap.
`BenchmarkVerifyAuditSignature` measures steps 6-8 for 1 and 32 items (one machine; not a throughput figure); the
batch benchmarks with real keepers are `chain/x/evidence/difftest` (package P4).

## Deviations from the spec machine

Each deviation implements an owner ruling or a decision request named in the row. The differential adapter
(`chain/x/evidence/difftest`, package P4) applies a tail rule only to a non-idempotent spec success, in the tail order
above. D-n numbers are x/evidence deviations; goal-08 deviations are written "leases D-n".

| Id | Chain behaviour | Spec | Why | Keeper test |
| --- | --- | --- | --- | --- |
| D-1 | the signature covers the envelope batch; a signature over another payload (any envelope field, item order, batch id, epoch, schema version, key label, or signer changed) or a payload with no SVN-JCS-1 form (an unsafe integer, a signed string that is not UTF-8) is `UNAUTHORIZED` at step 8; also a deviation from `docs/protocol.md:95` (no batch wrapper in v0) | a signature bound to another digest is `INTEGRITY` (case evidence-signature-unbound); an integer with no SVN-JCS-1 form reaches its own spec step: content_version >= 2^53 `MALFORMED` at the gate (canonicalize, :263-266), epoch >= 2^53 `STALE_VERSION` (:958-960), expiry outside +-(2^53-1) `BOUNDS` (check_time, :961-963) | a real ed25519 signature over the batch cannot tell which field changed (Q2, DR-09-15) | `TestAlteredObservationD1`, `TestSpecIntegrityCaseIsUnauthorizedD1` |
| D-2 | the inner public key must derive the outer signer's address; key_id is a label checked only for the id pattern | one global key id `key-auditor`, no public key: a key_id other than `key-auditor` is `UNAUTHORIZED` (:250); any `AUDITOR` resending another auditor's business id and payload is idempotent ok (the business payload excludes the signer, :964), where the chain answers `UNAUTHORIZED` at step 7 | Q1: the attestation is bound to an admitted auditor account | `TestAuditorKeyBindingD2` |
| D-3 | epoch must equal `EpochAt(now)`; genesis `current_epoch` is 0 ("derived") | static `state["epoch"] = 1`, never advanced | Q4: "correct round", "expired rounds" | `TestEpochRuleD3` |
| D-4 | no evidence sweep, no expiry budget; a `RECORDED` record reports `EXPIRED` once now >= expiry; `REJECTED` stays `REJECTED` | op_expire marks due `RECORDED` evidence `EXPIRED` inside the shared cap, evidence first; also a deviation from `docs/protocol.md:244` (begin-block expiry marks due evidence expired) and `:254` (an evidence expiry index by `(expiry_unix_seconds, evidence_id)`), neither of which x/evidence keeps; the `:258` comparison (committed time >= expiry) is kept by `ReportedState` | Q4: evidence cannot delay lease releases (DR-08-07) and no BeginBlock work exists | `TestReportedStateD4` |
| D-5 | schema_version 0 `MALFORMED`, not 1 `OUT_OF_RANGE`, after the role check | no field; a step carrying it is `UNKNOWN_FIELD` | "unknown schemas" (DR-09-12) | `TestSchemaVersionD5` |
| D-6 | N items under one signature, check-major order, atomic, an evidence id repeated inside the batch `DUPLICATE_ID`, a payload above 65536 bytes `BOUNDS` at step 8 | one item per step, `item_count` abstract | the proto carries `repeated items` (DR-09-11) | `TestBatchCheckMajorOrderD6`, `TestBatchAtomicD6`, `TestDuplicateInBatchD6`, `TestSignPayloadSizeBound` |
| D-7 | Enforce first (signer not 20 bytes `MALFORMED`; gas and size `BOUNDS`); an empty string is `MALFORMED` at step 2 | `check_address` `BOUNDS`; an empty string is present | goal-07/08 conventions (leases D-8) | ladder enforce-* and presence-* rows |
| D-8 | kind `PROVIDER_RECEIPT` `FORBIDDEN` (T2) | accepted under the auditor key | DR-09-07 interim: no provider signature exists on chain | `TestProviderReceiptNotOnChainD8` |
| D-9 | expiry - now > 86400 `BOUNDS` (T3) | accepted up to year 9999 | Q4: a `RECORDED` item cannot read as current for longer | `TestEvidenceLifetimeBoundD9` |
| D-10 | item content_id or content_version differs from the lease: `CONFLICT` (T4) | content_id unchecked, content_version unvalidated | Q4: lease/content binding | `TestLeaseContentBindingD10` |
| D-11 | lease not held (ENDED, EXPIRED, reservation released) `CONFLICT` (T5) | any lease state | Q4: no new record for an ended obligation | `TestLeaseNotHeldD11` |
| D-12 | held lease with deadline <= now (not yet swept) `EXPIRED` (T6) | accepted | Q4; same rule as leases D-2 | `TestLeaseDeadlinePassedD12` |
| D-13 | content of the lease withdrawn: `WITHDRAWN` (T8) | accepted | Q4: no audit of withdrawn hosting | `TestContentWithdrawnD13` |
| D-14 | WITHDRAWN (was: auditor key equal to the lease provider's current key `FORBIDDEN`). A provider chooses its own key without proof of possession, so it could rotate to an auditor's public key and make every audit of its leases fail. The rule added no protection: one role per address already separates the accounts | — | — | — |
| D-15 | per-auditor per-epoch item cap (1024) `BOUNDS` (T9) | no cap | DR-09-06: bounds state growth by one admitted auditor (no fees) | `TestAuditorEpochCapD15` |
| D-16 | `EvidenceAccepted` carries `batch_id` and is followed by the untyped `evidence_state{evidence_id, lease_id, state}` | event without batch_id; no state | DR-09-16: an indexer must tell `RECORDED` from `REJECTED` | `TestEvidenceEvents` |
| D-17 | batch_id and every evidence_id must start with `IDPrefix(signer)`, else `FORBIDDEN` (T1) | any id; first come, first served | Q2, DR-09-29: an admitted auditor cannot take another auditor's pending ids | `TestForeignAuditorCannotTakeEvidenceIDsD17` |
| D-18 | item expiry > lease deadline `CONFLICT` (T7) | accepted | Q4: a record cannot read `RECORDED` past the obligation it describes | `TestEvidenceExpiryWithinLeaseD18` |
| D-19 | an unknown field (a submitter `reject` flag included: `EvidenceItem` reserves field 9 and the name `reject`) fails the app's strict transaction decoding before the handler, SDK codespace `sdk` code 2 | `UNKNOWN_FIELD` at step 1 (:925, :138-142; case evidence-reject-flag-unknown, cases.py:1044) | the chain decodes protobuf, not JSON keys; mapping the decode error to `UNKNOWN_FIELD` is a registry follow-up (`docs/architecture/registries.md`) | `TestEvidenceItemHasNoRejectFlag` (descriptor); app decode: INT's `TestEvidenceMessagesDecode` |

Not a deviation from the spec, but from the protocol relay column: a `BROADCASTER` outer signer is `FORBIDDEN` (spec
:928; `docs/protocol.md:240` lists "Broadcaster"). The owner's Q1 ruling confirms it (no relay;
`TestBroadcasterRelayForbidden`). A duplicate is a duplicate evidence id or batch id, not a repeated observation: the
same (auditor, lease, epoch, digest) under a new evidence id is accepted (`TestSameObservationNewIDIsAccepted`).

## A dishonest admitted auditor

A valid auditor signature shows which admitted key made the statement. It does not make the statement true. The chain
compared two digests and fetched nothing (DEC-010). An admitted auditor can record `RECORDED` for a lease it never
fetched, because the manifest digest is public, and can record `REJECTED` against a healthy provider. The limits still
bind it: the lifetime (D-9), the lease binding (D-10, D-11, D-12, D-18), withdrawal (D-13), the id prefix (D-17), the
per-epoch cap (D-15), and no write to any other module. The simulation with two auditors is app- and devnet-level
(INT, plan section 18).

## Limitations

The label catalog (`agents/auditor/labels/evidence-labels.json`) states these limitations; they apply to every record
of this module, verbatim:

- Auditors can lie.
- Providers may fetch from another host.
- Node IDs do not prove independent replicas.
- A valid signature does not make an observation true.
- Accounting success is not physical capacity or operator independence.
- A sampled check covers only the sampled segments.
- This auditor is not shown to be independent of the provider.
- No evidence record renews, extends or revives a lease, in any state.
- No cryptographic storage proof exists in this beta. It is a separate decision gate.

Module-specific limits:

- Every success needs a held lease, so evidence is recorded only on a chain with the labelled DEC-001 content and
  DEC-005 lease devnet parameters; elsewhere no lease exists and every batch ends at `NOT_FOUND` or earlier.
- Auditors are admitted only in genesis; no message admits, removes, or rotates one (DR-09-08). A compromised or
  dishonest auditor cannot be removed on chain in v0; its records stay.
- The chain cannot detect a shared operator or a shared key: a provider and an auditor run by one operator look like
  independent parties (D-14 withdrawn).
- `PROVIDER_RECEIPT` is refused on chain until a provider-signed path exists (DR-09-07).
- An epoch boundary between signing and inclusion makes a batch `STALE_VERSION`; a lease or content change during
  inclusion fails the whole batch (atomic). The producer rebuilds and re-signs.
- The keeper tests run against unit-test fakes of x/authority, x/leases, and x/content. They are not end-to-end
  evidence: the real keepers run in `chain/x/evidence/difftest` (P4) and `chain/app` and `chain/devnet` (INT).

## Future cryptographic-verification decision gate (DR-09-G1)

Status: OPEN, with no goal in 01-30 (REQ-SCOPE-011; REQ-EVID-006 DEFERRED). Nothing in goal 09 implements,
approximates, or prepares it: evidence kinds 3-5 stay reserved and are rejected, and no Merkle code exists in this module
(`TestNoMerkleCode`). Before any work a decision must choose between an explicit external Filecoin integration (with its
trust and availability dependencies) and a separately reviewed local scheme, and for a local scheme specify chunking,
the commitment format, the challenge randomness source (validators must not fetch a beacon during execution),
deadlines, replay resistance, proof-size bounds, deterministic verification, gas bounds, adversarial tests, and external
review. Preconditions: independent operators exist (DEC-002), evidence rules have independent review before any reward
or penalty, and a new protocol version (new kinds and fields). Whether to add it to `docs/decision-register.md` is an
open request in the evidence file.

## Handoff

Read surfaces this module consumes (declared in `keeper.go`, satisfied structurally): `Roles.Role`,
`Limits.GovernedLimit` (x/authority); `Leases.Lease` (x/leases `Keeper.Lease`); `Content.CheckNotWithdrawn` (x/content).
`NewKeeper` panics on a nil store key or a nil required dependency; `Peers` may be nil.

What this module offers: `SubmitAuditBatch`, `Evidence`, `RenewalEvidence` (`leases.Evidence`), `BusinessDigest`
(`msgkit.BusinessSource`), `StoredRecord`, `MsgServiceDesc`, `QueryServiceDesc`, `NewAppModule`, the genesis functions,
`ErrNoBlockTime`, the pure attestation builders, the store key builders, and `evidencetest` (`CheckInvariants`,
`StoreSnapshot`, and `PrefixCollisionKeys`, two synthetic keys whose addresses share their first 8 bytes, for the app
genesis prefix rule's test).

In the app (`chain/app`): `app.New` builds the keeper with `evidence.Deps{Roles, Limits, Leases: a leases keeper
without Evidence, Content, Peers}`, sets `leases.Deps.Evidence` to it, and adds it to the business-id peers; these
assignments are the compile-time interface checks. The module replaces the scaffold slot (`NewAppModule`,
`RegisterServices`), and the scaffold router stub is gone. The app's layout check refuses an evidence store without
layout version 2 (`TestAppRefusesScaffoldLayout`). App genesis refuses two `AUDITOR` addresses that share their first
8 bytes (DR-09-30, owner ruling on Q2; `TestGenesisRefusesAuditorPrefixCollision`) and keeps the empty-evidence launch
rule (`emptyCatalog`; `TestLaunchGenesisRejectsEvidence`). Devnet auditor accounts use ed25519 account keys (Q1).
`App.Export` refuses a non-empty evidence store before `mm.ExportGenesis` (`TestExportRefusesEvidenceState`): its
context has a zero header time, the module export of any record would fail with `ErrNoBlockTime`, and
`AppModule.ExportGenesis` can only panic, in a goroutine of the SDK module manager.

Open before evidence export or non-empty evidence genesis is ever enabled: `App.Export` must give its context the last
committed header time, and app genesis validation must require every record's `AuditorAddress(signer_pubkey)` to be an
`AUDITOR` of the same document (module genesis cannot check roles).

The producer (`agents/auditor`) builds ids with `IDPrefix(AuditorAddress(pub))`, the epoch with `EpochAt(latest header
time)`, and envelope files with `AuditEnvelope`; it signs `AuditSignBytes` and checks with `VerifyAuditSignature`
before submitting. A broadcast acknowledgement (CheckTx runs only the ante) is not committed success: it reads the
record by evidence id before re-signing.

## Decision requests (interim choices are reversible and are not owner approval)

Ruled by the owner on 2026-11-02 (see "Owner rulings"): DR-09-01 (Q1), DR-09-02 and DR-09-29 (Q2, with the app genesis
prefix rule DR-09-30), DR-09-09 (Q3, producer side), DR-09-03 to DR-09-05 and the lifetime half of DR-09-06 (Q4).

Open, with fail-closed interim rules implemented and tested here:

- DR-09-06 (numbers) `MaxEvidencePerAuditorPerEpoch` = 1024. Options: (a) 1024 per epoch (interim); (b) (a) plus one
  item per (auditor, lease, epoch); (c) pruning with used-id markers (needs a reading of DEC-004 and a period);
  (d) none (spec). Consequence of (a): an auditor at its cap cannot record further results, true or false, until the
  next epoch; no auditor can exhaust another's cap.
- DR-09-07 `PROVIDER_RECEIPT` on chain: interim `FORBIDDEN` (D-8); accepting receipts later is additive.
- DR-09-08 auditor admission, removal, rotation: genesis-only in v0 (no payload exists).
- DR-09-26 the status reading of "evidence trust model approved in Goal 02" (recorded as BLOCKED, as goal 08 did).

Reversible implementation choices: DR-09-11 batch semantics (D-6); DR-09-12 schema_version codes (D-5); DR-09-13 the
business payload; DR-09-14 the digest mapping (algorithm 0 or unknown `MALFORMED`, value not 32 bytes `BOUNDS`,
`RECORDED` iff algorithm and value equal the lease's manifest digest); DR-09-15 `UNAUTHORIZED` for an altered
observation (D-1); DR-09-16 events (D-16); DR-09-17 query semantics; DR-09-18 store layout, ConsensusVersion 2, no
migration from the scaffold, export of the reported state without business ids; DR-09-19 a retry after an epoch boundary
is `STALE_VERSION`; DR-09-25 `key_id` is a label (pattern only); DR-09-27 no epoch query. Two further reversible
choices came from the goal-09 review, without a decision-request number: a signed string that is not UTF-8 has no
attestation payload (`UNAUTHORIZED`), and a query or export that would report a record without a committed block time
fails closed (`ErrNoBlockTime`, a plain error, not a protocol code).

Protocol additions requested for a later schema version (DR-09-28): a state field on `EvidenceAccepted`; a
current-epoch query; queries by batch, auditor, or state; an auditor key registry with council admit, disable, and rotate
payloads; a report-digest field; a provider receipt body and on-chain receipt signature; an evidence-expiry event if a
sweep is adopted; business ids bound to the signer or to one module.

## Residual risks

- An admitted auditor can lie: `RECORDED` means "the auditor reported the manifest digest", not "the bytes were
  retrieved"; a dishonest auditor can record `REJECTED` against a healthy provider, and a `REJECTED` record never
  expires. Nothing is paid or slashed.
- A `RECORDED` record written while a lease was held keeps reporting `RECORDED` after a later `EndLease`,
  `WithdrawContent`, or lease expiry until its own expiry (at most 86400 s, never past the lease deadline at recording).
- Under Q1 the auditor account key also signs attestations; the domain-separation argument is unreviewed (devnet beta
  only).
- Business ids form one namespace across modules and are not signed. Any admitted account can commit another
  successful message with a pending batch's business id first (for example its own `MsgUpdateProvider`, which carries a
  business id and goes through the shared gate), so the batch gets `REPLAY` and, being atomic, every item of it fails.
  The adversary with a motive is the audited provider, which can watch the mempool and front-run each retry; with no
  fees this repeats at no cost, and a provider that is also the block proposer can order it every time. A retry needs
  only a new business id, because the business id is unsigned (`TestBusinessIDTakenFirst`: `REPLAY`, then success
  under a new business id with the unchanged signature); an epoch boundary between attempts forces a re-sign. The
  producer draws each retry's business id from `crypto/rand`, so the retry ids cannot be computed from the batch id
  and committed in advance; only a broadcast already in the mempool can be front-run, one at a time, and at most 16
  submissions are made per batch. The remedy is DR-09-28 (business ids bound to the signer or to one module).
- Module genesis cannot check that a record's auditor holds the `AUDITOR` role, and genesis records are unsigned; the
  app's empty-evidence launch rule is what keeps unattributed records out (see "Genesis, export, migrations").
- Evidence state grows forever: at most 8,970,240 records per auditor per year, bounded per auditor per epoch, not over
  time.
- Raw `/store/evidence/*` ABCI queries are unmetered and unpaged (CometBFT RPC must not be public) and show the stored
  state, not the reported state.
- A historical query reports the state at the SDK's query block time, which falls back to the check-state header time
  when the node has no commit info for that height. Right after a restart that header time is zero, and such a query
  fails closed (`ErrNoBlockTime`) instead of reporting the stored state.
- Steps 2-10 run before the count check, so a rejected message with many items does unpaid CPU work up to the
  transaction size and the 65536-byte payload cap; the flat gas does not reflect it.
- Evidence ids carry the auditor's address prefix and part of an off-chain report hash: they are linkable to the
  auditor and to that report by design.
