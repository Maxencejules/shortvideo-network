# chain/x/providers

The provider registry module (goal 07). Package `providers`, module name `providers`, consensus version 1.

A provider record is a coordination record. It says which account registered a provider id, which ed25519 key
signs for it now, which keys it retired, the SHA-256 digest of its off-chain capability manifest, its declared
capacity, its reserved bytes, its admission state, and an opaque operator group id. It is not evidence of an
independent operator, of physical capacity, or of reachability.

The keeper code in this directory is chain code. It reads block height and time only from `sdk.Context`, iterates
stores in key order, and never reads the clock, randomness, the environment, files, or the network
(`TestDeterministicSources`). It does not import `x/authority`, the app, the other registry modules, or the
`capability` subpackage (`TestKeeperImports`). It has no BeginBlock or EndBlock and returns no validator updates;
`x/authority` stays the only validator-set writer (`TestNoValidatorUpdates`).

## State machine

```
RegisterProvider ──> APPLIED ──council admit──> ADMITTED ──DrainProvider──> DRAINING
                                                   │                           │
                                                   └──────council disable──────┴──> DISABLED (terminal)
```

- `RegisterProvider` creates `APPLIED`, key generation 1, reserved bytes 0.
- Admission is only the council `AdmitProviderChange` (DEC-009). It needs the DEC-002 devnet test parameter with
  role `DEVNET_TEST_ONLY` and operator group `devnet-test-only`. On any other chain it is `ERROR_CODE_DECISION_BLOCKED`.
- `DrainProvider` moves `ADMITTED` to `DRAINING`.
- Disablement is only the council `DisableProviderChange`, from `ADMITTED` or `DRAINING`.
- `UpdateProvider` and `RotateProviderKey` are allowed in every state. The v0 spec has no state check for them.
- No other transition exists. No record is deleted.

There is no single-signature admit or disable message (DEC-009). x/authority calls `AdmitByCouncil` and
`DisableByCouncil` while it executes an approved proposal.

## Disablement and drain semantics

- New obligations need `ADMITTED`. `CheckNewObligation` returns `ERROR_CODE_FORBIDDEN` for `APPLIED`, `DRAINING`, and
  `DISABLED`, and `ERROR_CODE_NOT_FOUND` for an unknown id. Goal 08 calls it from offer and accept.
- Drain and disablement release nothing. Reserved bytes stay until each existing obligation ends by its own legal
  transition (provider or authority end, or committed expiry). Goal 08 owns those transitions.
- The v0 spec lets `RenewLease` proceed for a `DRAINING` or `DISABLED` provider. Whether renewals should stop after
  disablement is decision request DR-07-05 for goal 08.
- History stays: the record, its retired keys, past events, and past transactions remain.
- `DISABLED` is terminal. A disabled provider cannot be admitted or drained again.

## Messages and check order

Each handler runs `msgkit.Enforce` first (gas limit in `[1, 20000000]`, transaction at most 65536 bytes, a fixed
50000 gas charge, a 20-byte signer). Then the presence rule: an empty string, empty bytes, or a nil message field the
spec requires is `ERROR_CODE_MALFORMED`; an integer 0 goes through its range check. Then the order of
`docs/evidence/goal-02/spec_machine.py`. Every check runs before the first write.

| Message | Order after Enforce and presence |
| --- | --- |
| `RegisterProvider` (`op_register_provider`) | role (none `UNAUTHORIZED`, other than `ADMITTED_PROVIDER` `FORBIDDEN`); provider id, signing key id, operator group id pattern `BOUNDS`; signing pubkey not 32 bytes `BOUNDS`; endpoint digest (`MALFORMED` / `BOUNDS`); capacity 0 `BOUNDS`; business gate; existing id `DUPLICATE_ID` |
| `UpdateProvider` (`op_update_capacity`) | lookup `NOT_FOUND`; signer not the account `FORBIDDEN`; capacity 0 `BOUNDS`; endpoint digest; business gate; capacity below reserved bytes `CAPACITY` |
| `RotateProviderKey` (`op_rotate`) | lookup `NOT_FOUND`; inner signature (domain `SVN-PROVIDER-ROTATE-1`, current key only); signer not the account `FORBIDDEN`; new key id pattern, new pubkey not 32 bytes `BOUNDS`; business gate; expected generation `STALE_VERSION`; same key id or pubkey `CONFLICT`; generation at 2^64-1 `OVERFLOW` |
| `DrainProvider` (`op_drain`) | lookup `NOT_FOUND`; inner signature (domain `SVN-PROVIDER-DRAIN-1`, current key only); signer not the account `FORBIDDEN`; business gate; state not `ADMITTED` `CONFLICT` |

Inner signature order (`msgkit.VerifyInner`): nil `MALFORMED`; domain unknown `MALFORMED`, another known domain
`WRONG_DOMAIN`; canonical profile version 0 `MALFORMED`, not 1 `OUT_OF_RANGE`; key id empty `MALFORMED`, outside the
pattern `BOUNDS`; public key or signature size `BOUNDS`; key id or public key not the current key `UNAUTHORIZED`;
ed25519 failure `UNAUTHORIZED`.

The outer signer is the `signer` field. Registration needs an `ADMITTED_PROVIDER` account; update, rotate, and drain
need the account stored in the record. A broadcaster as outer signer is `FORBIDDEN` for all four, as in the spec
machine. The relay column of the `protocol.md` transition table names a broadcaster for `RegisterProvider`, but the
message has no inner signature, so a broadcaster signer cannot name the provider account; the spec machine rejects it.

Signed bytes are `UTF-8(domain) || 0x00 || UTF-8(SVN-JCS-1 object)`. `RotateSignBytes` and `DrainSignBytes` build them:

- rotate: `{"expectedGeneration","newSigningKeyId","newSigningPubkey","providerId"}` (unchanged from goal 06)
- drain: `{"keyGeneration","providerId"}`, where `keyGeneration` is the current generation (a goal-07 choice)

Integers are decimal strings, byte strings lowercase hex. No chain id is in either payload (DR-07-01).

### Business ids

The business gate is `business_gate`: the id rule (empty `MALFORMED`, outside the pattern `BOUNDS`), then the SHA-256 of
the SVN-JCS-1 object of the spec's field set, keyed by proto field names, then a lookup in this module's ledger and
then in every other registered module (`msgkit.Peers`, one namespace). Absent proceeds. The same digest is an
idempotent success: no write, no typed event, one `providers_result` event with `idempotent=true`. Another digest is
`ERROR_CODE_REPLAY`. The id is stored only when the message succeeds.

| Message | Business field set |
| --- | --- |
| `RegisterProvider` | business_id, declared_capacity_bytes, endpoint_manifest_digest, operator_group_id, provider_id, signing_key_id, signing_pubkey |
| `UpdateProvider` | declared_capacity_bytes, endpoint_manifest_digest, provider_id |
| `RotateProviderKey` | business_id, current_key_signature, expected_generation, new_signing_key_id, new_signing_pubkey, provider_id |
| `DrainProvider` | provider_id, provider_signature |

A retry of a committed rotation is `ERROR_CODE_UNAUTHORIZED`, not idempotent. The spec checks the signature against
the current key before the gate, and the retry was signed by the key it retired. A broadcaster must reconcile a
rotation by querying the provider, not by resubmitting it.

## Council hooks

| Hook | Checks, in order | Effect |
| --- | --- | --- |
| `AdmitByCouncil(ctx, id, devnet)` | absent `NOT_FOUND`; `devnet` false `DECISION_BLOCKED`; group not `devnet-test-only` or state not `APPLIED` `FORBIDDEN` | `ADMITTED`, `ProviderStateChanged` |
| `DisableByCouncil(ctx, id)` | absent `NOT_FOUND`; state not `ADMITTED` or `DRAINING` `CONFLICT` | `DISABLED`, `ProviderStateChanged` |

x/authority computes `devnet` from DEC-002. It keeps the proposal as `REJECTED` on `FORBIDDEN` and fails the
transaction on any other code. A non-OK hook writes nothing.

DEC-002: the membership policy stays blocked. The admission record exists and works only with the labelled devnet
test role. `operator_group_id` is metadata. It is not evidence that two providers are run by different operators.

## Store layout

| Prefix | Key | Value |
| --- | --- | --- |
| `0x00` | `0x00` | store layout version (8 bytes big endian), written by InitGenesis |
| `0x01` | `0x01 \| provider_id` | `svn.providers.v1.Provider`, deterministic protobuf |
| `0x02` | `0x02 \| business_id` | 32-byte business digest |

The `0x00` layout-version key keeps the store's IAVL tree from ever being empty. iavl v1.2.8 saves an empty tree's root as a key with an empty value, and the cosmos-db v1.1.3 goleveldb backend reports such a key as absent, so a store that never held a key cannot be reloaded and the node cannot restart (`TestRestartLoadsCommittedState` in `chain/app`). Export ignores the key.

Ids match `^[a-z0-9][a-z0-9._-]{0,63}$`, so key order is id order. `retired_unix_seconds` of a retired key is the block
time of the rotation. The stored endpoint digest is rebuilt from the validated value, so no unknown field of a
message reaches state.

## Events

| Source | Event |
| --- | --- |
| `RegisterProvider` | `svn.events.v1.ProviderRegistered{provider_id, state="APPLIED"}` |
| `RotateProviderKey`, `DrainProvider`, council admit, council disable | `svn.events.v1.ProviderStateChanged{provider_id, state, key_generation}` |
| `UpdateProvider` | none (the spec emits none and `events.proto` has no update event; indexers see the SDK `message.action` event and re-query) |
| idempotent replay | `providers_result{idempotent="true"}` only |

Events go through `msgkit.EmitTyped`: type is the full proto name, attributes in field order, integers decimal. State
values are the short names `APPLIED`, `ADMITTED`, `DRAINING`, `DISABLED`. A rejected message emits nothing. Events are
indexer input; they are not part of the application hash.

## Queries

`svn.providers.v1.Query`, registered on the gRPC query router by `RegisterServices`. It is served over ABCI query
(`/svn.providers.v1.Query/Provider`, `/svn.providers.v1.Query/Providers`) and the node gRPC server.

- `Provider{provider_id}`: empty `MALFORMED`, outside the pattern `BOUNDS`, absent `NOT_FOUND`.
- `Providers{page}`: every provider in every state, in id order. The page cap is the effective `max_page_limit`
  (governed value, at most 100). A nil page or limit 0 uses the cap. A limit above the cap, a `page_key` above 64 bytes,
  or a non-empty `page_key` outside the id pattern is `BOUNDS`. `page_key` is inclusive. `next_page_key` is the next id,
  empty on the last page.

The service descriptors (`MsgServiceDesc`, `QueryServiceDesc`) are written by hand; the protocol module ships no gRPC
stubs. The protocol Msg service has no `cosmos.msg.v1.service` option, so `RegisterServices` uses the configurator's
message router and query router directly.

## Genesis and migrations

`ValidateGenesis` checks: unique ids in the pattern; 20-byte account; 32-byte signing key; signing key id in the
pattern; `key_generation >= 1`; exactly `key_generation - 1` retired keys with generations 1, 2, ... in order, 32-byte
keys, and retirement times in `[1, 253402300799]` that never decrease; a SHA-256 endpoint digest; a defined state;
capacity at least 1; reserved bytes at most capacity; operator group in the pattern; no unknown fields. A retired key
may equal the current key: the v0 spec lets a provider rotate back.

`InitGenesis` writes into an empty store in id order. `ExportGenesis` reads in id order and encodes protojson with
proto names and unpopulated fields. Business ids are not in the v0 genesis schema and are not exported (DR-07-03). The
launch genesis keeps the provider list empty; the app enforces that at InitChain (DR-07-02).

Consensus version 1 is the first layout. The migration table is empty; `TestRunMigrationsAtV1IsANoOp` runs
`RunMigrations` from version 1 through a real configurator and checks the store is unchanged.

## Handoff API (goal 08)

- `Provider(ctx, id) (*Provider, bool)`: a copy of the record.
- `CheckNewObligation(ctx, id) (*Provider, msgkit.Code)`: `NOT_FOUND`, or `FORBIDDEN` unless `ADMITTED`.
- `VerifyProviderSignature(ctx, id, domain, canonical, sig) msgkit.Code`: `NOT_FOUND`, then `msgkit.VerifyInner` against
  the current key. A retired key never verifies.
- Reserve and release accounting is goal 08 and extends this package.

## Capability manifest (`capability/`)

Off-chain tooling, not chain code. `capability.Parse` validates a manifest against
`docs/protocol/schemas/provider-capability.schema.json` on top of SVN-JCS-1 and returns the canonical text and
`SHA-256(SVN-JCS-1(manifest))`, the value for `endpoint_manifest_digest`. `capability.Encode` builds one. It also
requires `declared_capacity_bytes` to fit in uint64, which is stricter than the schema. Endpoint URLs stay in the
manifest. The chain stores only the digest and never fetches a manifest.

## Privacy

On chain: ids, 20-byte accounts, 32-byte public keys, digests, byte counts, states, generations, and block times.
Never: manifests, endpoint URLs, video bytes, emails, usernames, or free text. An opaque id or a digest can still be
linkable to a person or a host by anyone who holds the preimage. Hashing is not anonymization.

## Limits and residual notes

- There is no per-account limit on registered providers; DEC-005 allowances are unset. Gas meters every write.
- The retired-key list grows by one entry per rotation; gas meters it.
- A provider signing key equal to another provider's key, a council key, or a consensus key is not rejected (DR-07-06).
- A stored record that does not decode, or that names another id than its key, is an invariant violation. Message
  paths panic on it and the SDK fails the transaction; queries and export return an error.
