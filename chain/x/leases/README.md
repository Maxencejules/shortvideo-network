# chain/x/leases

The lease module (goal 08). Package `leases`, module name `leases`, consensus version 2 (version 1 was the
goal-05/06 scaffold slot; see "Genesis, export, migrations").

A lease is a committed hosting obligation between a scheduler (an admitted service), one provider, and one registered
content package. An offer names the exact content version, manifest digest, package bytes, terms digest, and
real-time start, deadline, and offer expiry; it reserves nothing. `AcceptLease` turns the offer into an `ACTIVE`
lease only with the provider's own inner signature over those terms, and reserves the package bytes against the
provider's admitted capacity. `RenewLease` moves the deadline by one DEC-005 term without changing the bytes or the
digest. `EndLease` and begin-block expiry release the reservation once.

`ACTIVE` (and `RENEWED`) means one thing: a committed obligation that holds a reservation. It is not evidence of
retrieval, of physical independence, of a region, or of publication approval. Observed reachability (the blueprint's
`AT_RISK`) stays off chain; no message, field, or state of this module records it (`TestLeaseStateHasNoAtRisk`).

The code in this directory (not `_test.go`, not `leasestest/`) is chain code. It reads block data only from
`sdk.Context`; the block header time, in whole seconds, is its only clock. It iterates stores in key order and never
reads the local clock, randomness, the environment, files, or the network (`TestDeterministicSources`, the
`msgkittest` source scan; goroutines, map ranges, and methods on values stay review items). It consumes the provider,
content, authority, and evidence read surfaces through the interfaces in `keeper.go` and never imports x/providers,
x/content, x/authority, x/evidence, the app, the devnet, or the node, in code or in tests (`TestKeeperImports`). It has
a BeginBlock (expiry), no EndBlock, and returns no validator updates (`TestModule`).

## Files

| File | Content |
| --- | --- |
| `keeper.go` | `Keeper`, `Deps` and the read interfaces, constants, `NewKeeper`, `BusinessDigest`, `Lease` |
| `store.go` | store layout, key builders, record and counter helpers |
| `msg_server.go` | `OfferLease`, `AcceptLease`, `RenewLease`, `EndLease` |
| `signdoc.go` | provider inner-signature payloads: `AcceptSignBytes`, `RenewSignBytes`, `EndSignBytes` |
| `expiry.go` | `ExpireDue` (BeginBlock body) |
| `hooks.go` | `ContentWithdrawn` (content withdrawal hook) |
| `query.go` | `ActiveLeases`, `Deadlines` and the Deadlines cursor |
| `genesis.go` | `ParseGenesis`, `MarshalGenesis`, `ValidateGenesis`, `InitGenesis`, `ExportGenesis` |
| `module.go` | `AppModule` (genesis, services, consensus version, BeginBlock, migrations) |
| `service.go` | hand-written `grpc.ServiceDesc` for `svn.leases.v1.Msg` and `svn.leases.v1.Query` |
| `events.go` | typed events and `leases_offer_closed` |
| `leasestest/` | test support: `CheckInvariants`, `CheckOfferIntegrity`, `StoreSnapshot` (imported only by tests) |
| `testdata/sign-vectors.json` | golden ACCEPT, RENEW, and END sign vectors (public keys only) |

## State machine

```
OfferLease ──> open offer ──AcceptLease──> ACTIVE ──RenewLease──> RENEWED ──RenewLease──> RENEWED ...
                  │                          │                      │
                  ├── expiry ──> closed      ├── EndLease ─────────┼──> ENDED   (terminal, reservation released)
                  └── withdrawal ──> closed  └── BeginBlock expiry ┴──> EXPIRED (terminal, reservation released)
```

- An offer is its own record (`svn.leases.v1.Offer`). `LEASE_STATE_OFFERED` is never written into a `Lease`.
- A lease is created `ACTIVE` at term version 1. Each renewal adds 1 to the term version, moves the deadline by the
  DEC-005 term, and sets `RENEWED`.
- `ENDED` and `EXPIRED` are terminal. A lease record is never deleted; its id is never reused.
- A closed offer (accepted, expired, or voided by withdrawal) is deleted; its id stays used (deviation D-4).

## Store layout and indexes

Store key `leases`. Record values are deterministic protobuf (`proto.MarshalOptions{Deterministic: true}`). Ids match
`^[a-z0-9][a-z0-9._-]{0,63}$` and never contain `0x00`, so the `0x00` separators are unambiguous and key order is id
order. Times are checked into `[1, 253402300799]` before any write and stored as the 8-byte big-endian `uint64` of the
Unix second, so key order is time order. Every value is non-empty (the goleveldb backend reports an empty value as
absent; `msgkit.LayoutVersionKey`).

| Prefix | Key | Value | Rows exist for |
| --- | --- | --- | --- |
| `0x00` | `0x00` | layout version `2`, 8 bytes BE | written by `InitGenesis`, never deleted |
| `0x01` | `0x01 \| offer_id` | `Offer` (scheduler = 20-byte outer signer) | open offers only |
| `0x02` | `0x02 \| lease_id` | `Lease` | every lease, every state; never deleted |
| `0x03` | `0x03 \| business_id` | 32-byte business digest (`msgkit.Ledger`, peer name `leases`) | every committed business id; permanent |
| `0x04` | `0x04 \| provider_id 0x00 content_id 0x00 version(8B)` | `lease_id` | the live pair: exactly the leases holding a reservation |
| `0x05` | `0x05 \| expiry(8B) \| offer_id` | `0x01` | open offers by (expiry, id) |
| `0x06` | `0x06 \| deadline(8B) \| lease_id` | `0x01` | held leases by (current deadline, id) |
| `0x07` | `0x07 \| content_id 0x00 offer_id` | `0x01` | open offers by content (withdrawal void) |
| `0x08` | `0x08 \| content_id 0x00 lease_id` | `0x01` | held leases by content (`ActiveLeases` by content) |
| `0x09` | `0x09 \| provider_id 0x00 lease_id` | `0x01` | held leases by provider (`ActiveLeases` by provider) |
| `0x0A` | `0x0A \| provider_id` | open-offer count, 8 bytes BE | providers with at least one open offer (deleted at 0) |
| `0x0B` | `0x0B \| content_id` | open-offer count, 8 bytes BE | content ids with at least one open offer (deleted at 0) |
| `0x0C` | `0x0C \| offer_id` | `0x01` | every offer id ever used; permanent (D-4) |

Records have a constant size (ids at most 64 bytes, digests 32 bytes, fixed scalars); nothing grows inside a value.
Index rows and counters are derived state: `InitGenesis` rebuilds them and `ExportGenesis` does not export them.

Reserved bytes are not in this store. They are `Provider.reserved_bytes` in x/providers, changed only through
`Providers.Reserve` (at accept) and `Providers.Release` (at end and expiry); x/leases is their only caller.

## Conventions

- Every handler returns a protocol code as an svnerr error (codespace `svn`, code = `svn.types.v1.ErrorCode`).
- First check of every handler: `msgkit.Enforce` (gas meter limit outside `[1, 20000000]` `BOUNDS` except in
  simulation; transaction above 65536 bytes `BOUNDS`; fixed 50000 gas; signer not 20 bytes `MALFORMED`).
- Presence rule: an empty string, empty bytes, or a nil message field the spec requires is `MALFORMED`; an integer 0
  is a value and goes through its range check.
- Every check runs before the first write. A rejected message writes nothing and emits nothing.
- Idempotent replay (same business id, same payload): code 0, no write, no typed event, one
  `leases_result{idempotent="true"}` event. The same business id with another payload is `REPLAY`. Business ids form
  one namespace with the other modules (`msgkit.Peers`).
- Role checks read `Roles.Role(ctx, signer)`; `SUBMITTER_ROLE_UNSPECIFIED` is "no role".
- Effective limits: `msgkit.Effective(key, Limits.GovernedLimit(ctx, key))` for `max_package_bytes`,
  `max_open_offers`, `max_expiry_steps`, `max_page_limit`.
- Committed time: `ctx.BlockTime().Unix()`.
- DEC-005: the offer and renewal term is the labelled DEC-005 devnet test number (`DevnetParams.DevnetNumeric`). It is
  a devnet test value, not policy. Without it, or with 0, every `OfferLease` and `RenewLease` is `DECISION_BLOCKED`.

### Business payloads (`business_gate`, spec_machine.py:257)

`msgkit.BusinessDigest` over the fields below, keyed by proto field name. Strings as strings, `bool` as a JSON bool,
digests as `msgkit.DigestObject`, inner signatures as `msgkit.SignatureObject`. Integers (`content_version`,
`term_version`, `package_bytes`, `*_unix_seconds`) are raw JSON integers, as the spec machine canonicalizes them: an
integer outside `[-(2^53-1), 2^53-1]` has no SVN-JCS-1 form and the gate returns `MALFORMED`. Goal-07 provider
payloads use decimal strings instead (decision request DR-08-10).

| Message | Business fields |
| --- | --- |
| `OfferLease` | business_id, content_id, content_version, deadline_unix_seconds, expiry_unix_seconds, manifest_digest, offer_id, package_bytes, provider_id, start_unix_seconds, term_version, terms_digest |
| `AcceptLease` | lease_id, offer_id, provider_acceptance |
| `RenewLease` | deadline_unix_seconds, evidence_id (only when non-empty), lease_id, manifest_digest, package_bytes, provider_signature, term_version |
| `EndLease` | authority_end, lease_id, and provider_signature when authority_end is false |

### Provider inner-signature payloads (interim, decision request DR-08-04)

No proto field or API of protocol v0 defines what a provider signs for ACCEPT, RENEW, or END. This module defines it.
Signed bytes: `UTF-8(domain) || 0x00 || UTF-8(SVN-JCS-1 object)` (`msgkit.SignBytes`). Keys are lowerCamelCase.
Every integer is a decimal string (a negative `int64` keeps its `-`), so a payload exists for every integer value; an
out-of-range value is rejected by its own range check, not by the signature (`TestSignPayloadNeverFails`). Digests are
`{"algorithm", "value"}` (enum name, lowercase hex); the scheduler is lowercase hex. There is no chain id (joins
DR-07-01).

| Domain | Payload | Built from |
| --- | --- | --- |
| `SVN-PROVIDER-ACCEPT-1` | contentId, contentVersion, deadlineUnixSeconds, expiryUnixSeconds, leaseId, manifestDigest, offerId, packageBytes, providerId, scheduler, startUnixSeconds, termVersion, termsDigest | the STORED offer and the message's lease id |
| `SVN-PROVIDER-RENEW-1` | deadlineUnixSeconds, evidenceId (`""` when absent), leaseId, manifestDigest, packageBytes, termVersion | the message |
| `SVN-PROVIDER-END-1` | leaseId, providerId | the message's lease id and the lease's provider |

The acceptance binds the provider's consent to the exact content version, manifest digest, bytes, terms digest, term
version, real-time start and deadline, offer expiry, scheduler, and lease id (`TestAcceptReservesOnceAndBindsTerms`).
Verification is `Providers.VerifyProviderSignature`: `msgkit.VerifyInner` against the provider's CURRENT registered
key. A service key, another provider's key, or a retired key is `UNAUTHORIZED`; a signature in another known domain
is `WRONG_DOMAIN` (`TestServiceKeyCannotSignAcceptance`, REQ-CHAIN-012). A provider signature is never interchangeable
with a service signature: a service account as the outer signer of an acceptance is `FORBIDDEN`.

Exported builders for schedulers, broadcasters, and the provider agent: `AcceptSignBytes(offer, leaseID)`,
`RenewSignBytes(leaseID, termVersion, deadline, manifest, packageBytes, evidenceID)`, `EndSignBytes(leaseID,
providerID)`. Golden vectors: `testdata/sign-vectors.json` (`TestSignBytesVectors`; `-update` rewrites it). Test keys
are derived in the test from `sha256("svn-leases-test-seed:" + label)`; the file holds public keys only.

## Messages: check order and negative cases

The check order follows `docs/evidence/goal-02/spec_machine.py`, then the chain-only checks (deviations D-2 to D-6)
at the tail, all before the first write. Every row below is a ladder row: `TestOfferCheckOrder`,
`TestAcceptCheckOrder`, `TestRenewCheckOrder`, `TestEndCheckOrder`, and `TestAuthorityEndCheckOrder` run each row
twice, once with every later fault present ("cumulative/<row>", which proves the order) and once alone
("isolated/<row>", which proves the row reaches its own branch), on a cached branch of the context. Each rejected row
asserts the exact code, a byte-identical trial state of the leases, providers, and content stores, and no event.
`TestTrialSnapshotDetectsEarlyWrite` shows that the trial check sees a write or an event before a failure. The ladder
row name is in the last column.

### OfferLease (op_offer, spec_machine.py:583)

| # | Check | Code | Ladder row |
| --- | --- | --- | --- |
| 1 | gas meter limit outside `[1, 20000000]` | `BOUNDS` | enforce-gas-limit-0 |
| 1 | transaction above 65536 bytes | `BOUNDS` | enforce-tx-65537-bytes |
| 1 | signer not 20 bytes (D-8) | `MALFORMED` | enforce-signer-19-bytes |
| 2 | business_id, offer_id, content_id, provider_id empty; manifest_digest, terms_digest nil | `MALFORMED` | presence-business-id; `TestOfferPresence` |
| 3 | signer without a role | `UNAUTHORIZED` | role-none |
| 3 | signer not `ADMITTED_SERVICE` | `FORBIDDEN` | role-broadcaster |
| 4 | offer_id, content_id, provider_id outside the id pattern | `BOUNDS` | offer-id-pattern, content-id-pattern, provider-id-pattern |
| 5 | manifest_digest then terms_digest: algorithm 0 or unknown | `MALFORMED` | manifest-algorithm-0, terms-algorithm-7 |
| 5 | digest value not 32 bytes | `BOUNDS` | manifest-31-bytes, terms-33-bytes |
| 6 | start, deadline, expiry outside `[1, 253402300799]` | `BOUNDS` | start-0, deadline-above-9999, expiry-negative |
| 7 | package_bytes outside 1..effective max_package_bytes | `BOUNDS` | package-bytes-0, package-above-governed-limit |
| 8 | business_id outside the id pattern | `BOUNDS` | gate-business-id-pattern |
| 8 | content_version or term_version above 2^53-1 (no canonical form) | `MALFORMED` | gate-content-version-2^60, gate-term-version-2^60, gate-term-version-max-uint64 |
| 8 | business id committed (any module) with another payload | `REPLAY` | gate-replay-other-module |
| 9 | offer_id is an open offer | `DUPLICATE_ID` | open-offer-id |
| 10 | content: no record | `NOT_FOUND` | content-not-found |
| 10 | content WITHDRAWN | `WITHDRAWN` | content-withdrawn |
| 10 | content not PENDING or another version | `CONFLICT` | content-version-2 |
| 10 | manifest digest or bytes differ from the content record | `INTEGRITY` | manifest-differs, package-bytes-differ |
| 11 | provider: no record | `NOT_FOUND` | provider-not-found |
| 11 | provider not ADMITTED (APPLIED, DRAINING, DISABLED) | `FORBIDDEN` | provider-draining |
| 12 | expiry <= committed time | `EXPIRED` | expiry-at-committed-time |
| 13 | deadline <= start | `OUT_OF_RANGE` | deadline-at-start |
| 14 | DEC-005 devnet number absent or 0 | `DECISION_BLOCKED` | dec005-absent, dec005-zero; `TestDecisionBlockedWithoutDEC005` |
| 14 | deadline - start != DEC-005 number | `OUT_OF_RANGE` | term-plus-one-second |
| 15 | term_version != 1 | `STALE_VERSION` | term-version-2 |
| 16 | live pair (provider, content, version) | `DUPLICATE_ID` | live-pair; `TestDuplicateReplicaRejected` |
| 17 | provider open offers >= effective max_open_offers | `BOUNDS` | provider-open-offer-cap; `TestOfferProviderCapCountsOpenOffers` |
| 18 | reserve probe: reserved + bytes above 2^64-1 | `OVERFLOW` | reserve-overflow; `TestCapacityAndOverflow` |
| 18 | reserve probe: reserved + bytes above capacity | `CAPACITY` | reserve-capacity; `TestCapacityAndOverflow` |
| 19 | D-4: offer_id used before | `DUPLICATE_ID` | d4-offer-id-used; `TestOfferIDNeverReusedD4` |
| 20 | D-5: expiry - committed time > `MaxOfferLifetimeSeconds` | `BOUNDS` | d5-lifetime-86401; `TestOfferLifetimeBoundD5` |
| 21 | D-6: content open offers >= `MaxOpenOffersPerContent` | `BOUNDS` | d6-content-open-offer-cap; `TestOpenOffersPerContentCapD6` |

Writes: the offer (scheduler = signer), its expiry, content, and used-id rows, both counters, the business id; event
`LeaseOffered`. No reservation (`TestOfferWritesRecordAndEvent`).

### AcceptLease (op_accept, spec_machine.py:685)

| # | Check | Code | Ladder row |
| --- | --- | --- | --- |
| 1 | Enforce (gas, size, signer) | `BOUNDS` / `MALFORMED` | enforce-* |
| 2 | business_id, offer_id, lease_id empty; provider_acceptance nil | `MALFORMED` | presence-business-id; `TestAcceptPresence` |
| 3 | signer without a role | `UNAUTHORIZED` | role-none |
| 3 | signer neither `BROADCASTER` nor `ADMITTED_PROVIDER` | `FORBIDDEN` | role-service |
| 4 | offer_id, lease_id outside the id pattern | `BOUNDS` | offer-id-pattern, lease-id-pattern |
| 5 | acceptance domain 0 or unknown | `MALFORMED` | sig-domain-unspecified, sig-domain-unknown-99 |
| 5 | acceptance in another known domain | `WRONG_DOMAIN` | sig-wrong-domain |
| 5 | canonical_profile_version 0 / not 1 | `MALFORMED` / `OUT_OF_RANGE` | sig-profile-0, sig-profile-2 |
| 5 | key_id empty / outside the pattern | `MALFORMED` / `BOUNDS` | sig-key-id-empty, sig-key-id-pattern |
| 5 | public key not 32 / signature not 64 bytes | `BOUNDS` | sig-public-key-31, sig-signature-63 |
| 6 | business gate: business_id pattern / REPLAY | `BOUNDS` / `REPLAY` | gate-business-id-pattern, gate-replay-other-module |
| 7 | no open offer | `NOT_FOUND` | offer-not-open |
| 8 | D-1: key id, public key, or ed25519 does not verify under the provider's current key over the stored offer and lease id | `UNAUTHORIZED` | d1-ed25519-mismatch; `TestAcceptCheckOrderDeviationD1`, `TestServiceKeyCannotSignAcceptance`, `TestAcceptReservesOnceAndBindsTerms` |
| 9 | `ADMITTED_PROVIDER` signer that is not the provider's account | `FORBIDDEN` | provider-signer-not-the-provider |
| 10 | lease_id exists (any state) | `DUPLICATE_ID` | lease-id-exists |
| 11 | committed time >= offer expiry | `EXPIRED` | offer-expired |
| 12 | content WITHDRAWN | `WITHDRAWN` | content-withdrawn (fakes: in the real system withdrawal voids the offers first) |
| 13 | provider not ADMITTED | `FORBIDDEN` | provider-draining |
| 14 | live pair | `DUPLICATE_ID` | live-pair; `TestDuplicateReplicaRejected` |
| 15 | reserve probe | `OVERFLOW` / `CAPACITY` | reserve-overflow, reserve-capacity; `TestCapacityAndOverflow` |
| 16 | D-3: offer deadline <= committed time | `EXPIRED` | d3-offer-deadline-passed; `TestAcceptPastOfferDeadlineIsExpiredD3` |

Writes: `Providers.Reserve` (it repeats the probe that just passed in the same branch; a failure is an invariant
violation and panics, which fails the transaction on every node alike), the lease (`ACTIVE`, terms from the stored
offer, `policy_version` from `Policy.PolicyVersion`, `reservation_held` true), its pair, deadline, content, and
provider rows; the offer and its expiry and content rows are deleted and both counters decremented; the used-id
marker stays; the business id; event `LeaseAccepted`.

### RenewLease (op_renew, spec_machine.py:765)

| # | Check | Code | Ladder row |
| --- | --- | --- | --- |
| 1 | Enforce | `BOUNDS` / `MALFORMED` | enforce-* |
| 2 | business_id, lease_id empty; manifest_digest, provider_signature nil | `MALFORMED` | presence-business-id; `TestRenewPresence` |
| 3 | no lease (an id outside the pattern has none) | `NOT_FOUND` | lease-not-found |
| 4 | signature structure (as AcceptLease row 5, domain `PROVIDER_RENEW`) | `MALFORMED` / `WRONG_DOMAIN` / `OUT_OF_RANGE` / `BOUNDS` | sig-* |
| 4 | key id, public key, or ed25519 under the provider's current key over the message payload | `UNAUTHORIZED` | signature-ed25519-mismatch |
| 5 | signer without a role | `UNAUTHORIZED` | role-none |
| 5 | signer neither `BROADCASTER` nor `ADMITTED_PROVIDER` | `FORBIDDEN` | role-service |
| 5 | `ADMITTED_PROVIDER` signer that is not the provider's account | `FORBIDDEN` | provider-signer-not-the-provider |
| 6 | manifest_digest algorithm / size | `MALFORMED` / `BOUNDS` | manifest-algorithm-0, manifest-31-bytes |
| 7 | business gate: business_id pattern; an integer above 2^53-1; REPLAY | `BOUNDS` / `MALFORMED` / `REPLAY` | gate-business-id-pattern, gate-term-version-2^60, gate-deadline-2^60, gate-package-bytes-max-uint64, gate-replay-other-module |
| 8 | lease not ACTIVE/RENEWED or reservation not held | `CONFLICT` | lease-not-held |
| 9 | content WITHDRAWN | `WITHDRAWN` | content-withdrawn; `TestWithdrawalVoidsOffers` |
| 10 | term_version != current + 1 (no overflow at 2^64-1) | `STALE_VERSION` | term-version-stale; `TestRenewKeepsCommittedBytes` |
| 11 | package_bytes or manifest digest differ from the lease | `CONFLICT` | package-bytes-change, manifest-change; `TestRenewKeepsCommittedBytes` |
| 12 | new deadline outside `[1, 253402300799]` | `BOUNDS` | deadline-above-9999; `TestSignPayloadNeverFails` |
| 13 | DEC-005 number absent or 0 / new deadline - current deadline != number | `DECISION_BLOCKED` / `OUT_OF_RANGE` | dec005-absent, term-plus-one-second |
| 14 | evidence_id set: no reader or no record / expired (flag or expiry <= committed time) / live | `NOT_FOUND` / `EXPIRED` / `FORBIDDEN` | evidence-not-found, evidence-expired, evidence-expiry-passed, evidence-cannot-extend; `TestEvidenceCannotExtendLease` |
| 15 | D-2: current deadline <= committed time | `EXPIRED` | d2-deadline-passed; `TestRenewPastDeadlineIsExpiredD2` |

An `evidence_id` that makes the renewal payload non-canonical (above the 64 KiB SVN-JCS-1 cap) cannot carry a valid
signature: `UNAUTHORIZED` after the structural checks (`TestSignPayloadNeverFails`).

Writes: term version + 1, the new deadline, state `RENEWED`; the deadline row moves; reserved bytes are untouched;
the business id; event `LeaseRenewed`. Renewal is allowed for a DRAINING or DISABLED provider (v0 spec; DR-08-01).

### EndLease (op_end, spec_machine.py:858)

| # | Check | Code | Ladder row |
| --- | --- | --- | --- |
| 1 | Enforce | `BOUNDS` / `MALFORMED` | enforce-* |
| 2 | business_id, lease_id empty | `MALFORMED` | presence-business-id; `TestEndPresence` |
| 3 | no lease | `NOT_FOUND` | lease-not-found |
| 4a | authority_end: signer without a role / not `AUTHORITY` | `UNAUTHORIZED` / `FORBIDDEN` | `TestAuthorityEndCheckOrder` role-none, role-broadcaster |
| 4a | authority_end with a provider_signature | `UNKNOWN_FIELD` | `TestAuthorityEndCheckOrder` provider-signature-present |
| 4b | provider end: provider_signature nil | `MALFORMED` | provider-signature-absent |
| 4b | signature structure (domain `PROVIDER_END`) / key or ed25519 | as RenewLease row 4 | sig-*, signature-ed25519-mismatch |
| 4b | signer neither `BROADCASTER` nor `ADMITTED_PROVIDER` (no role included, spec :893) | `FORBIDDEN` | role-none, role-service |
| 4b | `ADMITTED_PROVIDER` signer that is not the provider's account | `FORBIDDEN` | provider-signer-not-the-provider |
| 5 | business gate: business_id pattern / REPLAY | `BOUNDS` / `REPLAY` | gate-business-id-pattern, gate-replay-other-module |
| 6 | reservation not held (second end, end after expiry) | `UNDERFLOW` | not-held; `TestEndReleasesOnce` |
| 7 | `Providers.Release` (reserved below the bytes) | `UNDERFLOW` | release-underflow |

Writes (Release is the last check and the first write; it changes reserved bytes only on success): state `ENDED`,
`reservation_held` false; the pair, deadline, content, and provider rows are deleted; the business id; event
`LeaseEnded{state "ENDED"}`. Ending works in every provider state and for withdrawn content.

## Begin-block expiry

`ExpireDue(ctx)` is the module's BeginBlock. A held lease is due when its current deadline <= committed time; an open
offer when its expiry <= committed time.

1. `now` = header time; `budget` = effective `max_expiry_steps` (governed value, ceiling 128).
2. Read at most `budget`+1 rows of the deadline index (`0x06`) and of the expiry index (`0x05`) up to `now` inclusive,
   and merge them by (time, kind, id), a lease before an offer at an equal time (the spec's string order). Take at
   most `budget` items. A header time below 1 has nothing due.
3. Lease: the record must exist, hold its reservation, have the index row's deadline, and own its pair row;
   `Providers.Release` must succeed; state `EXPIRED`, `reservation_held` false, rows deleted; event
   `LeaseEnded{state "EXPIRED"}`. Offer: the record must exist with the row's expiry; rows deleted, counters
   decremented (a counter at 0 is an error); event `leases_offer_closed{reason "EXPIRED"}`.
4. The writes go to a cached branch written back only when every item succeeded. Any invariant violation returns an
   error and writes nothing; the BeginBlock returns it and the module manager fails FinalizeBlock (fail closed;
   DR-08-07) instead of releasing twice or skipping a reservation.

Work per block: at most `budget` items and `budget`+1 rows per index, whatever the backlog. Items left over wait for
the next committed block. Nothing expires while no block commits; the real-time deadline in the signed terms does not
move. Tests: `TestExpiryOrderAndBudget` (budgets 1, 2, 3, 128; effective-1 and effective times; the backlog continues
in later blocks), `TestExpiryUsesHeaderTimeOnly`, `TestExpiryWorkIsBounded` (backlogs of 200 and 600 due leases: one
block expires exactly 128 with equal store gas, the drain takes ceil(N/128) blocks), `TestExpiryInvariantErrors`,
`TestBeginBlockRunsExpireDue`.

## Withdrawal, disablement, drain, cancellation, repair, restart

- Withdrawal: `ContentWithdrawn(ctx, contentID, version)` has the method set of `content.WithdrawalHook`. x/content
  calls the hook it is given inside `WithdrawContent` and a council force-withdraw, after the tombstone and before the
  withdrawal commits. It voids every open offer of the content id (any version; the spec matches on content id), in
  offer id order, with `leases_offer_closed{reason "WITHDRAWN"}` each; the used-id markers stay. It releases nothing:
  a held lease of withdrawn content keeps its reservation until `EndLease` or expiry, and its renewal is `WITHDRAWN`.
  The work is at most `MaxOpenOffersPerContent` offers (D-6). An invariant violation returns an svn `MALFORMED` error
  and writes nothing (x/content then aborts the withdrawal). Tests: `TestWithdrawalVoidsOffers`,
  `TestWithdrawalAtContentCap`, `TestWithdrawalHookInvariantErrors`.
- Drain and council disable (x/providers): new offers and accepts are `FORBIDDEN` (offer row 11, accept row 13). Open
  offers stay until expiry and count against the provider's open-offer cap. Existing leases keep their reservations;
  renewal is allowed (v0 spec, DR-08-01, successor of DR-07-05); provider or authority `EndLease` releases once.
- Cancellation: a provider `EndLease` (provider signature, relayed by a broadcaster or sent by the provider's account)
  or `authority_end` (one `AUTHORITY` signature, outside DEC-009). v0 has no message that cancels an open offer; the
  offer lifetime bound (D-5) limits how long one can hold a slot (DR-08-03).
- Repair handoff: a replica on another provider is a new offer and accept for the same content to that provider. The
  pair is per provider, so it is allowed while the old lease is held, and each provider's reservation is accounted
  separately (`TestDuplicateReplicaRejected`). Two leases on two providers are two replicas; nothing here says the
  providers are independent operators (DEC-002).
- Restart: all lease state is committed store state. The layout key keeps the store non-empty so it reloads from
  goleveldb. An expiry backlog resumes at the next committed block.

## Events

| Source | Event | Attributes |
| --- | --- | --- |
| OfferLease | `svn.events.v1.LeaseOffered` | offer_id, provider_id, content_id |
| AcceptLease | `svn.events.v1.LeaseAccepted` | lease_id, provider_id, content_id, content_version, package_bytes (decimal string), deadline_unix_seconds |
| RenewLease | `svn.events.v1.LeaseRenewed` | lease_id, term_version (new), deadline_unix_seconds (new) |
| EndLease | `svn.events.v1.LeaseEnded` | lease_id, state `ENDED`, released_bytes (decimal string) |
| BeginBlock lease expiry | `svn.events.v1.LeaseEnded` | lease_id, state `EXPIRED`, released_bytes |
| BeginBlock offer expiry | `leases_offer_closed` (not in svn.events.v1; D-9) | offer_id, reason `EXPIRED` |
| Withdrawal void | `leases_offer_closed` | offer_id, reason `WITHDRAWN` |
| Idempotent replay | `leases_result` | idempotent `true` |

Typed events go through `msgkit.EmitTyped`. The SDK appends `msg_index` to transaction events
(`msgkit.ParseCommitted`) and `mode=BeginBlock` to BeginBlock events. A rejected message emits nothing. Events are
indexer input, not app-hash input. Event and attribute names pass the bounds.json forbidden-name scan
(`TestNoForbiddenNames`).

## Queries

`svn.leases.v1.Query`, registered on the gRPC query router by `RegisterServices`. Rejections are `Code.QueryErr`
(gRPC status with an `ErrorInfo` detail; `msgkit.QueryCode` reads it). A store invariant violation is a plain error.
Page sizes: the cap is the effective `max_page_limit` (at most 100); a nil page or limit 0 uses the cap; a limit
above it is `BOUNDS`.

- `ActiveLeases{content_id, provider_id, page}`: held leases only. Neither id: `MALFORMED` (DR-08-08). An id outside
  the pattern: `BOUNDS`. Content only / provider only: lease id order over `0x08` / `0x09`; `page_key` is the
  inclusive lease id (`msgkit.CheckPage`: above 64 bytes or outside the pattern `BOUNDS`); `next_page_key` is the
  next lease id. Both: the live lease of the pair (at most one; v0 content records have version 1); a page key is
  `BOUNDS`.
- `Deadlines{not_after_unix_seconds, page}`: held leases with deadline <= not_after in (deadline, id) order, then open
  offers with expiry <= not_after in (expiry, id) order; one page holds at most `limit` items across both lists.
  not_after 0: `MALFORMED`; outside `[1, 253402300799]`: `BOUNDS`. Cursor (at most 64 bytes): phase 1 is the next
  lease id; phase 2 is the next offer id with the high bit of its first byte set (an id's first byte is
  `[0-9a-z]`). The keeper resumes from the record the cursor names; a cursor that does not decode, names an absent
  record or a lease that no longer holds its reservation, or whose record's time is after not_after is `BOUNDS`.
  Cursors are valid at the height that issued them (DR-08-08 asks for a structured cursor).
- Work: at most limit+1 iterator steps per index walked plus one record read per item. Every item is a
  constant-size record, so a page is bounded by construction: `TestLeaseQueryResponseSizeBound` builds 100 records at
  their widest encoding, asserts every page below 64 KiB, and logs the measured size. Tests: `TestActiveLeasesPagination`,
  `TestDeadlinesPagination`, `FuzzDeadlinesPageKey`.
- Only gRPC-routed queries run under the node query gas limit. Raw `/store/leases/key` and `/store/leases/subspace`
  ABCI queries bypass the router: no gas meter, no page cap (residual risk).

## Genesis, export, migrations

- `ParseGenesis`: strict protojson (unknown fields are errors; an empty document is an error). `MarshalGenesis`:
  proto names, unpopulated fields emitted.
- `ValidateGenesis`: offers: ids in the pattern, offer_id unique, content_version 1, SHA-256 32-byte digests without
  unknown fields, package_bytes in 1..268435456, start, deadline, and expiry in range, deadline > start,
  term_version 1, scheduler 20 bytes, at most 1024 open offers per provider and `MaxOpenOffersPerContent` per
  content. Leases: ids in the pattern, lease_id unique, offer_id unique among leases and not an open offer's id,
  content_version 1, digests and bytes as offers, deadline > start, state ACTIVE, RENEWED, ENDED, or EXPIRED
  (UNSPECIFIED, OFFERED, 6, and unknown values are invalid), reservation_held exactly for ACTIVE and RENEWED, ACTIVE at
  term 1 and RENEWED at term >= 2, policy_version >= 1, at most one held lease per pair. No unknown fields anywhere.
- `InitGenesis`: requires an empty store (the scaffold key `[0x01]`, a layout key, or any record is an error). It
  checks through Deps before any write: every offer passes `Content.CheckOfferable` and names an existing provider;
  every lease names an existing provider and existing content (`CheckNotWithdrawn` OK or WITHDRAWN); every provider
  named by an offer or a lease reserves exactly the bytes of its held leases. The Providers surface lists no
  providers, so a provider no record names is not checked. It then writes the layout key and the records in id order
  with every derived row (indexes, counters, used-offer markers for open offers and for every lease's offer_id).
- `ExportGenesis`: open offers and every lease, in id order. Not exported (recorded, tested restrictions): the
  business-id ledger (DR-07-03) and the used-offer markers of offers that expired or were voided (DR-08-06); a chain
  started from an export forgets those ids (`TestGenesisValidateImportExport/round-trip-from-committed-transitions`
  asserts exactly these rows). The launch genesis stays empty (app rule; DR-07-02).
- `ConsensusVersion` 2. Version 1 (`msgkit.ScaffoldLayoutVersion`) was the scaffold slot (`[0x01]` holding the genesis
  JSON, inside this layout's offer prefix). No migration from 1 is registered: `RunMigrations` from 1 fails and the
  store is untouched (`TestNoMigrationFromScaffoldLayout`). The table covers 2..ConsensusVersion-1 and is empty
  (`TestMigrationTable`, `TestRunMigrationsAtCurrentVersionIsANoOp`).

## Invariants

| Id | Invariant | Where it is checked |
| --- | --- | --- |
| I1 | per provider: reserved_bytes == baseline + sum of package_bytes of its held leases | `leasestest.CheckInvariants`; `InitGenesis` cross-check |
| I2 | reserved_bytes <= admitted_capacity_bytes (accept probes capacity) | `CheckInvariants`; accept row 15 |
| I3 | reservation_held exactly when the state is ACTIVE or RENEWED; ENDED and EXPIRED are terminal | `CheckInvariants`; `ValidateGenesis` |
| I4 | at most one held lease per (provider, content, version); `0x04` holds exactly the held leases | `CheckInvariants`; offer row 16, accept row 14 |
| I5 | `0x06`, `0x08`, `0x09` hold exactly the held leases, keyed by current deadline, content, provider | `CheckInvariants`; expiry and query invariant errors |
| I6 | `0x05`, `0x07` hold exactly the open offers; `0x0A`/`0x0B` equal the per-provider/per-content open-offer counts (absent = 0); a content id has at most `MaxOpenOffersPerContent` open offers | `CheckInvariants`; hook invariant errors |
| I7 | lease ids unique forever; leases never deleted; offer_id, content, version, provider, bytes, manifest, terms digest, start immutable; only term_version (+1 per renewal), deadline (+term per renewal), state, reservation_held change | `CheckInvariants` (single-state part); `TestRenewKeepsCommittedBytes` |
| I8 | each lease releases at most once (one `LeaseEnded` per lease id) | `TestEndReleasesOnce`, `TestExpiryOrderAndBudget` |
| I9 | an open offer's manifest digest and bytes equal the content record's (content is immutable) | offer row 10; `leasestest.CheckOfferIntegrity` |
| I10 | every open offer and every lease's offer_id has a `0x0C` marker; offer ids are never reused | `CheckInvariants`; `TestOfferIDNeverReusedD4` |
| I11 | no handler or expiry reads a clock other than the block header time | `TestDeterministicSources`; `TestExpiryUsesHeaderTimeOnly` |
| I12 | a business id commits at most one effect | ladder REPLAY rows; idempotent retries in `TestOfferWritesRecordAndEvent`, `TestAcceptReservesOnceAndBindsTerms`, `TestRenewKeepsCommittedBytes`, `TestEndReleasesOnce` |

`leasestest.CheckInvariants(ctx, keeper, providers, baseline)` checks I1-I7 (I7 only its single-state part) and I10
over one context, committed or cached, plus the layout key and unknown prefixes; its error names the invariant.
`CheckOfferIntegrity` checks I9 against a content surface. I8, the history part of I7, I11, and I12 are properties of
transitions or of the code; the tests above check them. `leasestest` self-tests break each invariant once
(`TestCheckInvariantsDetectsEachViolation`).

## Bounds and PROPOSED constants

| Bound | Value | Source | Status |
| --- | --- | --- | --- |
| Expiry items per block | effective `max_expiry_steps`, ceiling 128 | bounds.json `safety_max_expiry_steps_per_block` | protocol v0 candidate cap, PROPOSED (DR-08-07) |
| Open offers per provider | effective `max_open_offers`, ceiling 1024 | bounds.json `safety_max_open_offers_per_provider` | protocol v0 candidate cap, PROPOSED |
| Open offers per content | `MaxOpenOffersPerContent` = 256 | chain-only | PROPOSED safety constant, DR-08-02 |
| Offer lifetime | `MaxOfferLifetimeSeconds` = 86400 (expiry - committed time) | chain-only | PROPOSED safety constant, DR-08-03 (not a DEC-005 duration) |
| Offer and renewal term | exactly the DEC-005 devnet number | DEC-005 devnet parameter | devnet test number; elsewhere `DECISION_BLOCKED` |
| Package bytes | 1..effective `max_package_bytes` (<= 268435456) | bounds.json | protocol cap |
| Page size / page key | effective `max_page_limit` (<= 100) / 64 bytes | bounds.json | protocol cap |
| Times | `[1, 253402300799]`; deadline > start; expiry > committed time | protocol.md | |
| Term versions | offer 1; renewal current + 1 | spec | |
| Counters | uint64 add/sub checked (`OVERFLOW`, `UNDERFLOW`) | spec | |
| Withdrawal hook work | at most `MaxOpenOffersPerContent` offers | chain-only | DR-08-02 |
| Transaction | 65536 bytes; gas limit 1..20000000 | `msgkit.Enforce` | goal 07 |

Both chain-only caps count OPEN offers: a slot frees when its offer is accepted, expires, or is voided
(`TestOpenOffersPerContentCapD6`, `TestOfferProviderCapCountsOpenOffers`). Neither is a lifetime cap. DEC-005 lease
durations and per-account allowances stay unset; no lease count limit exists (residual risk).

## Deviations from the spec machine

| Id | Chain behaviour | Spec | Why | Keeper test |
| --- | --- | --- | --- | --- |
| D-1 | AcceptLease ed25519 verification runs after the business gate and the offer lookup; structure and domain run before the gate | the abstract `valid` flag is checked before the gate | real verification needs the stored offer's terms and the provider key | `TestAcceptCheckOrderDeviationD1` |
| D-2 | RenewLease of a held lease whose deadline <= committed time: `EXPIRED` (last check) | allowed while expiry has not swept it | an expired obligation cannot renew | `TestRenewPastDeadlineIsExpiredD2` |
| D-3 | AcceptLease of an offer whose deadline <= committed time: `EXPIRED` (last check) | allowed | a lease would be born past its deadline | `TestAcceptPastOfferDeadlineIsExpiredD3` |
| D-4 | OfferLease reusing an offer id ever used: `DUPLICATE_ID` (after every spec check) | allowed once the offer is gone | stable offer ids | `TestOfferIDNeverReusedD4` |
| D-5 | OfferLease with expiry - committed time > 86400: `BOUNDS` | allowed | open-offer slots cannot be held indefinitely | `TestOfferLifetimeBoundD5` |
| D-6 | OfferLease when the content has 256 open offers: `BOUNDS` | allowed | bounds the withdrawal hook's work | `TestOpenOffersPerContentCapD6` |
| D-7 | one expiry budget for leases and offers (evidence is goal 09); an invariant violation fails the block | one cap over three kinds; an error discards the step | x/evidence is not implemented | `TestExpiryOrderAndBudget`, `TestExpiryInvariantErrors` |
| D-8 | Enforce first (signer not 20 bytes `MALFORMED`, gas and size `BOUNDS`); an empty string is `MALFORMED` | `check_address` `BOUNDS`; an empty string is present | goal-07 conventions | ladder enforce-* and presence rows |
| D-9 | untyped `leases_offer_closed{offer_id, reason}` on offer expiry and withdrawal void | no event | indexers need offer closure (DR-08-08) | `TestExpiryOrderAndBudget`, `TestWithdrawalVoidsOffers` |

The chain-only checks run in this order at the tail: OfferLease D-4, D-5, D-6; AcceptLease D-3; RenewLease D-2. An
idempotent replay returns at the gate before any of them.

## Privacy

On chain: ids, the 20-byte scheduler account, digests, byte counts, times, versions, states. No region, endpoint URL,
manifest, media byte, email, username, watch, or complaint field exists (`TestNoForbiddenNames`). Opaque ids and
digests remain linkable by anyone who holds the preimage.

## Handoff

Read surfaces x/leases consumes (declared in `keeper.go`, satisfied structurally): `Roles.Role`,
`Limits.GovernedLimit`, `DevnetParams.DevnetNumeric`, `Policy.PolicyVersion` (x/authority); `Providers.Provider`,
`CheckNewObligation`, `VerifyProviderSignature`, `ReserveProbe`, `Reserve`, `Release` (x/providers);
`Content.CheckOfferable`, `CheckNotWithdrawn` (x/content); `Evidence.RenewalEvidence` (x/evidence, may be nil until
goal 09: every evidence id is then `NOT_FOUND`). `NewKeeper` panics on a nil store key or a nil required dependency.

What x/leases offers: `Keeper.Lease` (handoff read), `Keeper.BusinessDigest` (`msgkit.BusinessSource`),
`ContentWithdrawn` (withdrawal hook), `ExpireDue` (BeginBlock body), the four message handlers, `ActiveLeases`,
`Deadlines`, `MsgServiceDesc`, `QueryServiceDesc`, `NewAppModule`, the genesis functions, the sign-bytes builders, the
store key builders, and `leasestest`.

Later goals:

- Goal 09: wire `Deps.Evidence`; decide the expiry budget shared with evidence (DR-08-07); use `Keeper.Lease` for the
  DEC-010 digest comparison; evidence never extends a lease.
- Goal 11 broadcaster: a broadcast acknowledgement (CheckTx runs only the ante chain) is not success; poll the
  committed result. Resubmitting the same business id and payload under a new transaction hash is idempotent (code 0,
  `leases_result`); another payload is `REPLAY`; a rejected transaction is in its block with its svn code.
- Goal 12 indexer: the typed events plus `leases_offer_closed`; `msgkit.ParseCommitted` for transaction events,
  BeginBlock events end with `mode`; reconcile offers through `Deadlines` and leases through `ActiveLeases`.
- Goal 16 provider agent: the sign-bytes builders and vectors; deadlines are real-time obligations even while the
  chain commits no blocks; the agent compares deadlines with its own clock and shows the uncertainty.

App wiring, app-level and devnet tests, and their evidence: `docs/architecture/leases.md`. This README cites
keeper-level tests only; they run against unit-test fakes of the other modules, not end to end.

## Decision requests (interim choices are reversible and are not owner approval)

- DR-08-01 renewal after drain or council disable (v0: allowed; remedy `authority_end`, one lease per message; a
  disabled, possibly compromised provider can keep extending its own leases).
- DR-08-02 `MaxOpenOffersPerContent` = 256 (one scheduler can occupy a content's slots for up to the offer lifetime).
- DR-08-03 `MaxOfferLifetimeSeconds` = 86400 (alternatives: cancel/decline messages, per-scheduler quotas).
- DR-08-04 provider ACCEPT/RENEW/END payloads (field sets, decimal-string integers, lease id bound, no chain id).
- DR-08-05 real-time deadline guards D-2 and D-3.
- DR-08-06 offer ids never reused (D-4); export drops closed-offer markers.
- DR-08-07 expiry: 128 per block, BeginBlock placement, (time, kind, id) order, a budget shared with evidence, fail
  closed on an invariant violation.
- DR-08-08 protocol additions: a typed offer-closed event, Lease/Offer by-id queries, a structured Deadlines cursor,
  an unfiltered ActiveLeases.
- DR-08-09 renewal window: a provider can renew any time while the reservation is held and stack terms (deadline up
  to year 9999).
- DR-08-10 one integer encoding rule for business payloads across modules.

## Residual risks

- Raw `/store/leases/*` ABCI queries are unmetered and unpaged; CometBFT RPC must not be public.
- Lease history grows forever; there is no per-account lease limit while DEC-005 leaves allowances unset. Gas meters
  every write.
- A scheduler can occupy a content's 256 offer slots or a provider's 1024 for up to one day.
- A disabled provider can keep renewing its leases (DR-08-01).
- An expiry invariant violation halts the chain (fail closed, DR-08-07).
- Export drops business ids and closed-offer markers (DR-07-03, DR-08-06).
- `InitGenesis` cannot check a provider that no offer or lease names (the Providers surface lists no providers).
