# chain/x/providers

The provider registry module (goal 07). Package `providers`, module name `providers`, consensus version 1.

A provider record is a coordination record. It says which account registered a provider id, which ed25519 key
signs for it now, which keys it retired, the SHA-256 digest of its off-chain capability manifest, its declared
capacity, its reserved bytes, its admission state, and an opaque operator group id. It is not evidence of an
independent operator, of physical capacity, or of reachability.

The keeper code in this directory is chain code. It reads block height and time only from `sdk.Context`, iterates
stores in key order, and never reads the clock, randomness, the environment, files, or the network.
`TestDeterministicSources` runs the `msgkittest` source scan on it: no nondeterministic import, no clock or timer
call of package `time`, no floating-point type, literal, or `math` function. The scan is syntactic; it does not see
a float that another package's API returns. It does not import `x/authority`, the app, the other registry modules, or the
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

Deviation from the protocol error table: `protocol.md` says a direct `AdmitProvider` or `DisableProvider` is
`ERROR_CODE_FORBIDDEN` (codespace `svn`, code 2). No such message type is registered, so a transaction that names
`/svn.providers.v1.MsgAdmitProvider` or `/svn.providers.v1.MsgDisableProvider` fails transaction decoding: codespace
`sdk`, code 2 (`ErrTxDecode`), in CheckTx and in a block (`TestDirectAdminMessagesDoNotExist`). The number is the same;
the codespace is not. A client that keys on `svn/2` does not see this rejection as `FORBIDDEN`.

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
| `RegisterProvider` | business_id, declared_capacity_bytes, endpoint_manifest_digest, operator_group_id, provider_id, signing_key_id, signing_pubkey (the signer is not in the set) |
| `UpdateProvider` | declared_capacity_bytes, endpoint_manifest_digest, provider_id |
| `RotateProviderKey` | business_id, current_key_signature, expected_generation, new_signing_key_id, new_signing_pubkey, provider_id |
| `DrainProvider` | provider_id, provider_signature |

Deviation from the spec machine (decision request DR-07-08): the `RegisterProvider` digest does not cover the signer,
so a second provider account can send a byte-for-byte copy of a pending registration under its own signature. The
first copy to commit owns the record. The spec reports the other copy as an idempotent success; this module reports
it as `ERROR_CODE_FORBIDDEN` and writes nothing, because the signer does not own the record. The owner's own retry
stays idempotent (`TestRegisterReplayByAnotherAccountIsForbidden`, and the app case "copy of another account's
registration"). Registration does not prove possession of `signing_pubkey`: a front-runner can register another
provider's key and manifest digest under its own account. The council sees the `account` in `Query/Provider` before
it admits. Whether the business digest should cover the signer, and whether registration should carry a
proof-of-possession signature by `signing_pubkey`, are protocol changes in DR-07-08.

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
| `0x01` | `0x01 \| provider_id` | `svn.providers.v1.Provider`, deterministic protobuf, `previous_signing_keys` always empty |
| `0x02` | `0x02 \| business_id` | 32-byte business digest |
| `0x03` | `0x03 \| provider_id \| 0x00 \| generation (8 bytes big endian)` | `svn.providers.v1.PreviousSigningKey`, one row per retired key |

Retired keys are rows of their own, not a list inside the record. The record therefore has a constant size: a
rotation, an update, a drain, and a council admit or disable read and write the same bytes however many times the
provider rotated. With the list inline, each rotation added about 45 bytes to the one value every later write
rewrites at about 33 gas per byte, so a provider could rotate until no council `ExecuteChange(disable_provider)` fit in
the 20,000,000 gas limit. `TestWritesDoNotGrowWithRotations` (keeper, 2 and 300 rotations) and
`TestCouncilDisableGasDoesNotGrowWithRotations` (`chain/app`, a committed council disable after 2 and 60 rotations)
check that the gas does not grow. `Query/Provider`, `Query/Providers`, and export put the retired keys back into
`previous_signing_keys`; the handoff read `Provider` does not.

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

A committed event (FinalizeBlock results, `/block_results`, the tx index) has one more attribute after the proto
fields: `msg_index`, the index of the message in its transaction, which the SDK appends to every event a message
emits. It is not part of the `svn.events.v1` schema. `msgkit.ParseTyped` parses the schema only and rejects
`msg_index`; `msgkit.ParseCommitted` parses a committed event and returns the typed message and the index
(`TestRegistryEventSchemas`, and the devnet demo on raw `/block_results` events).

## Queries

`svn.providers.v1.Query`, registered on the gRPC query router by `RegisterServices`. It is served over ABCI query
(`/svn.providers.v1.Query/Provider`, `/svn.providers.v1.Query/Providers`) and the node gRPC server.

- `Provider{provider_id}`: empty `MALFORMED`, outside the pattern `BOUNDS`, absent `NOT_FOUND`. The record carries every
  retired key.
- `Providers{page}`: every provider in every state, in id order, each with its retired keys. The page cap is the
  effective `max_page_limit` (governed value, at most 100). A nil page or limit 0 uses the cap. A limit above the cap,
  a `page_key` above 64 bytes, or a non-empty `page_key` outside the id pattern is `BOUNDS`. `page_key` is inclusive.
  `next_page_key` is the next id, empty on the last page. A page also ends early, before the provider that would take
  it above `MaxRetiredKeysPerPage` (1024) retired keys; `next_page_key` then names that provider. The first provider of
  a page is always returned, so a walk advances (`TestProvidersPageRetiredKeyBudget`).

A rejected query returns `msgkit.QueryErr`: the `svn` error, with a gRPC status (`NotFound` for `NOT_FOUND`,
`InvalidArgument` for `MALFORMED` and `BOUNDS`) that holds one `google.rpc.ErrorInfo` detail: domain `svn`, reason the
protocol name, metadata `code` the protocol number. The node gRPC server returns that status; `msgkit.QueryCode` reads
it. Over ABCI query the SDK keeps only the gRPC status code, as codespace `sdk` `ErrKeyNotFound` (38) or
`ErrInvalidRequest` (18), and the text: the protocol code does not cross that path.

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

Consensus version 1 is the first layout. The migration table is empty. `TestMigrationTable` is the evidence for the
registration path: a test table entry is registered with a real configurator (a second registration is a duplicate)
and the module manager runs it once when the module reports version 2. `TestRegistryMigrationPathInApp` (`chain/app`)
does the same through the app's own configurator on committed state. `TestRunMigrationsAtV1IsANoOp` only shows that
nothing runs from version 1 to 1; the SDK returns before it looks for a step in that case.

## Handoff API (goal 08)

- `Provider(ctx, id) (*Provider, bool)`: a copy of the record without its retired keys (`previous_signing_keys` is
  empty), a constant amount of work.
- `CheckNewObligation(ctx, id) (*Provider, msgkit.Code)`: `NOT_FOUND`, or `FORBIDDEN` unless `ADMITTED`.
- `VerifyProviderSignature(ctx, id, domain, canonical, sig) msgkit.Code`: `NOT_FOUND`, then `msgkit.VerifyInner` against
  the current key only. Only the current key verifies. The current key may be a key the provider retired earlier
  and then rotated back to: the v0 spec allows that (rotation compares the new key with the current key only), and
  that key then verifies again.
- Reserve and release accounting is goal 08 and extends this package.

## Capability manifest (`capability/`)

Off-chain tooling, not chain code. `capability.Parse` validates a manifest against the rules of
`docs/protocol/schemas/provider-capability.schema.json` on top of SVN-JCS-1 and returns the canonical text and
`SHA-256(SVN-JCS-1(manifest))`, the value for `endpoint_manifest_digest`. `capability.Encode` builds one. It also
requires `declared_capacity_bytes` to fit in uint64, which is stricter than the schema. The rules are written in Go;
`TestValidatorMatchesSchemaFile` reads the schema file and compares every keyword the package implements (required
keys, `additionalProperties`, `const`, lengths, item counts, patterns) with the package constants. Endpoint URLs stay
in the manifest. The chain stores only the digest and never fetches a manifest.

The signed manifest is the body plus a detached `svn.types.v1.InnerSignature` by the provider's current signing key
over `UTF-8("SVN-CAPABILITY-1") || 0x00 || UTF-8(SVN-JCS-1 body)`, with `key_id` equal to the manifest's
`signing_key_id`. `capability.Sign` makes it; `capability.Verify` checks it with `msgkit.VerifyInner` (another domain
`WRONG_DOMAIN`; another key id, another key, or a bad signature `UNAUTHORIZED`). `endpoint_manifest_digest` covers the
body only, so signing does not change it. The chain never checks the manifest signature. A peer that fetches a
manifest checks both: the signature against the current key from `Query/Provider`, and the digest against
`endpoint_manifest_digest`. Protocol v0 does not define the wire envelope `GET /capability` returns (body and signature
together); that is decision request DR-07-09.

## Privacy

On chain: ids, 20-byte accounts, 32-byte public keys, digests, byte counts, states, generations, and block times. The
protocol has no field for manifests, endpoint URLs, video bytes, emails, or usernames, and the chain stores none in
the normal flow (`TestNoPrivateDataInStateOrEvents` scans every store, event, export, and committed transaction).

Ids are text the submitter chooses. Every id field is checked against `^[a-z0-9][a-z0-9._-]{0,63}$`, so an email or a
URL in an id is `BOUNDS` and nothing is stored. An id-shaped value such as a username matches the pattern and is
stored verbatim; the chain cannot tell it from an id (`TestSubmitterTextFields`). A transaction a handler rejects is
still in its block. The memo, fee payer, fee granter, and tip are rejected in the ante handler and unknown fields fail
decoding, so those never reach a block an honest proposer builds. An opaque id or a digest can still be linkable to a
person or a host by anyone who holds the preimage. Hashing is not anonymization.

## Limits and residual notes

- There is no per-account limit on registered providers; DEC-005 allowances are unset. Gas meters every write.
- Retired keys are one row each; the history grows by one row per committed rotation. Message paths never read it.
  `Query/Provider` reads all of one provider's rows; a `Providers` page reads at most `MaxRetiredKeysPerPage` rows
  beyond its first provider's. There is no cap on rotations in v0 (no spec rule); a node bounds query work with its
  query gas limit.
- A provider signing key equal to another provider's key, a council key, or a consensus key is not rejected (DR-07-06).
- A stored record that does not decode, that names another id than its key, or that carries retired keys inline is
  an invariant violation. Message paths and council hooks panic on it and the SDK fails the transaction; queries and
  export return an error (`TestCorruptStoredRecord`). A retired-key row that is missing, extra, out of order, or does
  not decode makes queries and export return an error (`TestRetiredKeyRowsAreTheHistory`).
