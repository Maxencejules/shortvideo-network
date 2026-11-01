# chain/x/content

The content coordination registry (Goal 07). Video bytes stay off the chain. This module stores one small record per content package and a tombstone when the package is withdrawn.

## What a record is, and is not

A record holds an opaque `content_id`, `content_version`, the SHA-256 digest of the off-chain package manifest, the package byte count, the submitting host account (20 bytes), and a coordination state.

- A record is not copyright proof. The chain does not check who made the content.
- A record is not a hosting receipt. Registration writes no lease, offer, or reservation. Hosting obligations are lease state (Goal 08).
- A record is not publication approval. `PENDING` is not `PUBLISHED`. Publication is the off-chain `PublicationStatus`. `CONTENT_STATE_PUBLISHED` is reserved and never written.
- Opaque ids and digests can still be linkable. A party that holds the manifest or the complaint can match its digest to the record. Hashing is not anonymization.
- The protocol has no field for a manifest, endpoint URL, email, username, watch event, or complaint text, and the chain stores none in the normal flow. `reason_digest` is a digest, not the complaint.
- Ids are text the submitter chooses. `content_id`, `business_id`, and the intent `key_id` must match `^[a-z0-9][a-z0-9._-]{0,63}$`. An email or a URL there is `BOUNDS`, except `content_id` of `WithdrawContent`, which is looked up first and is `NOT_FOUND`; nothing is stored either way. An id-shaped value such as a username matches and is stored verbatim (`TestSubmitterTextFields` in `chain/app` asserts the exact code for every id field of both messages). A transaction a handler rejects is still in its block. The memo, fee payer, fee granter, and tip are rejected in the ante handler, and unknown fields fail decoding.

## State and store

States: `PENDING` (registered) and `WITHDRAWN` (plus a tombstone). The only transition is `PENDING -> WITHDRAWN`. Nothing is deleted.

| Prefix | Key | Value |
| --- | --- | --- |
| `0x00` | `0x00` | store layout version (8 bytes big endian), written by InitGenesis |
| `0x01` | `0x01 \| content_id \| 0x00 \| content_version (8 bytes big endian)` | `svn.content.v1.Content` |
| `0x02` | same suffix | `svn.content.v1.Tombstone` |
| `0x03` | `0x03 \| business_id` | 32-byte business digest |

The `0x00` layout-version key keeps the store's IAVL tree from ever being empty. iavl v1.2.8 saves an empty tree's root as a key with an empty value, and the cosmos-db v1.1.3 goleveldb backend reports such a key as absent, so a store that never held a key cannot be reloaded and the node cannot restart (`TestRestartLoadsCommittedState` in `chain/app`). Export ignores the key.

Values are deterministic protobuf bytes. Records are keyed by (id, version) so a later decision that allows versions above 1 needs no key migration. v0 writes version 1 only. A new encoding is a new `content_id` in v0 (protocol.md; decision request DR-07-04).

## Messages

Both handlers run `msgkit.Enforce` first (gas meter limit and tx size `BOUNDS`, signer size `MALFORMED`; in simulation only the meter-limit check is skipped, because the SDK simulates on an infinite meter) and run every check before the first write. A rejected message writes nothing and emits nothing.

`RegisterContent` (spec `op_register_content`): presence of `business_id`, `content_id`, `manifest_digest`, `hosting_intent` (`MALFORMED`); signer role (none `UNAUTHORIZED`, not `ADMITTED_SERVICE` `FORBIDDEN`); `content_id` pattern (`BOUNDS`); `content_version` not 1 (`OUT_OF_RANGE`); `package_bytes` outside 1..effective `max_package_bytes` (`BOUNDS`); manifest digest (`MALFORMED`/`BOUNDS`); hosting intent; business gate (same payload idempotent, other payload `REPLAY`); any record of the id in any state (`DUPLICATE_ID`). Success writes a `PENDING` record with `submitting_host` = signer.

`WithdrawContent` (spec `op_withdraw_content`, `withdraw_content`): presence of `business_id`, `content_id`, `reason_digest`, `hosting_intent`; signer role; reason digest; hosting intent; business gate; no record (`NOT_FOUND`); signer is not the submitting host (`FORBIDDEN`); already withdrawn (`WITHDRAWN`); version mismatch (`STALE_VERSION`). Success marks the record `WITHDRAWN` and writes the tombstone.

The protocol defines no direct force-withdraw message (DEC-009). Force-withdraw exists only as the council path below.

Deviation from the protocol error table: `protocol.md` says a direct `ForceWithdraw` is `ERROR_CODE_FORBIDDEN` (codespace `svn`, code 2). No such message type is registered, so a transaction that names `/svn.content.v1.MsgForceWithdraw` fails transaction decoding: codespace `sdk`, code 2 (`ErrTxDecode`), in CheckTx and in a block (`TestDirectAdminMessagesDoNotExist`). The number is the same; the codespace is not.

The business id namespace is global. The gate consults this module's ledger first, then the other modules registered in `msgkit.Peers`, in name order. An idempotent replay returns ABCI 0, writes nothing, and emits only `content_result` with `idempotent=true`.

## Hosting intent (DEC-001, interim DR-07-01)

v0 has no hosting-intent record schema, and `InnerSignature` has no role field. Until the owner decides, the intent key signs `SVN-HOSTING-INTENT-1 || 0x00 || SVN-JCS-1(payload)`:

- register: `{"action":"register","contentId","contentVersion","manifestSha256","packageBytes","roleName","submittingHost"}`
- withdraw: `{"action":"withdraw","contentId","contentVersion","reasonSha256","roleName","submittingHost"}`

`submittingHost` is the lowercase hex of the outer signer, so one intent authorizes one host. Integers are decimal strings and digests are lowercase hex. `RegisterIntentSignBytes` and `WithdrawIntentSignBytes` build these bytes.

Check order: shape (`MALFORMED`/`OUT_OF_RANGE`/`BOUNDS`); a valid signature over the payload with role `ACCOUNT_HOST_DELEGATE` is `FORBIDDEN` (host custody is not assumed); a domain other than `HOSTING_INTENT` is `WRONG_DOMAIN`; no DEC-001 devnet parameter naming `DEVNET_TEST_ONLY` is `DECISION_BLOCKED`; no valid signature over the payload with role `DEVNET_TEST_ONLY` is `UNAUTHORIZED`.

Limits of this interim:

- Every non-devnet chain is `DECISION_BLOCKED`. Production content registration waits for DEC-001.
- The public key is the one in the message. The signature proves payload integrity under that key. It does not prove a creator authorized the host: no intent key is registered on chain and DEC-001 key custody is unset.
- A payload signed for an unknown role is `UNAUTHORIZED`, not the spec's `FORBIDDEN`, because an unknown role cannot be recognised from a signature.
- The withdraw intent is not bound to the key that signed the register intent. The payload has no chain id.

## Council force-withdraw

`Keeper.ForceWithdrawByCouncil(ctx, contentID, version, reasonSHA256, devnet)` applies an executed council `ForceWithdrawChange` (DEC-003, DEC-009). x/authority calls it from `ExecuteChange` and passes `devnet` true only when the DEC-003 devnet parameter names `DEVNET_TEST_ONLY`. Order: reason not 32 bytes `MALFORMED` (wiring); no record `NOT_FOUND`; not devnet `DECISION_BLOCKED`; already withdrawn `WITHDRAWN`; version mismatch `STALE_VERSION`. There is no host check. Success writes the same tombstone as `WithdrawContent`.

## Withdrawal semantics

- A tombstone keeps `content_id`, `content_version`, `committed_unix_seconds` (block time), `reason_digest`, `history_erased` false, `retention_decision_id` `DEC-004`, and `retention_value_version` 0. The `Content` row stays.
- A later register of the id is `DUPLICATE_ID`. A second withdrawal is `WITHDRAWN`. The id cannot be revived.
- Withdrawal blocks new offers, accepts, and renewals through the lease handoff below (Goal 08 enforces it in its handlers). It does not release an accepted reservation.
- `Deps.Hook` (`WithdrawalHook`) runs inside the withdrawal. Goal 08 implements it to void open offers. Goal 07 wires no hook because no offer store exists. A hook error aborts the withdrawal and nothing of it is written. On both the message path and the council path the hook's svn code is passed through; any other error is `MALFORMED` (`TestWithdrawHookFailureWritesNothing`, `TestForceWithdrawHookFailure`).
- A block time outside `1..253402300799` is `OUT_OF_RANGE`, checked after every protocol check, so an exported tombstone always validates.

## Events

| Transition | Event (`svn.events.v1`) | Attributes |
| --- | --- | --- |
| RegisterContent | `ContentRegistered` | `content_id`, `content_version`, `manifest_sha256` (64 lowercase hex), `package_bytes` (decimal string) |
| WithdrawContent, council force-withdraw | `ContentWithdrawn` | `content_id`, `content_version`, `history_erased` (`false`) |
| idempotent replay | `content_result` | `idempotent=true` |

Events are emitted with `msgkit.EmitTyped` and parse back with `msgkit.ParseTyped`. They are indexer input, not app-hash input.

A committed event (FinalizeBlock results, `/block_results`, the tx index) ends with one more attribute, `msg_index`, which the SDK appends to every event a message emits. It is not part of the `svn.events.v1` schema and `msgkit.ParseTyped` rejects it. `msgkit.ParseCommitted` parses a committed event and returns the typed message and the message index.

## Queries

`svn.content.v1.Query/Content{content_id, content_version}` is registered on the SDK query router, which serves the ABCI query path `/svn.content.v1.Query/Content` and the node's gRPC server when the app enables it. Empty id or version 0 is `MALFORMED`, an id outside the pattern is `BOUNDS`, and an absent pair is `NOT_FOUND`. The response holds the record and, when withdrawn, its tombstone. v0 has no paginated content list (DR-07-07).

A rejected query returns `msgkit.QueryErr`: the `svn` error with a gRPC status (`NotFound`, or `InvalidArgument` for `MALFORMED` and `BOUNDS`) holding one `google.rpc.ErrorInfo` detail (domain `svn`, reason the protocol name, metadata `code`). The node gRPC server returns it and `msgkit.QueryCode` reads it. Over ABCI query the SDK keeps only the gRPC status code (`sdk` `ErrKeyNotFound` 38 or `ErrInvalidRequest` 18) and the text.

## Handoff API for the lease module (Goal 08)

- `PackageIdentity{ContentID, Version, ManifestDigest, PackageBytes, SubmittingHost, State}`: the immutable identity a lease commits to.
- `Package(ctx, id, version) (PackageIdentity, msgkit.Code)`: absent is `NOT_FOUND`.
- `CheckOfferable(ctx, id, version, manifestSHA256, packageBytes) msgkit.Code`: the content part of `op_offer`, in order `NOT_FOUND`, `WITHDRAWN`, `CONFLICT` (not `PENDING` or version mismatch), `INTEGRITY` (digest or bytes mismatch).
- `CheckNotWithdrawn(ctx, id) msgkit.Code`: the `op_accept`/`op_renew` content check.
- `WithdrawalHook`: implemented by Goal 08 to void open offers.

## Genesis and migrations

`ValidateGenesis` checks ids, unique `content_id`, version 1, digests, `1 <= package_bytes <= 268435456`, a 20-byte host, state `PENDING` or `WITHDRAWN`, exactly one matching tombstone per `WITHDRAWN` row and none for other rows, and the tombstone constants. `InitGenesis` writes rows by key; `ExportGenesis` reads them in key order. Business ids are not in the v0 genesis schema and are not exported (DR-07-03).

The application's launch genesis keeps the catalogue empty (goal 05, REQ-GEN-002). Importing a non-empty exported catalogue at InitChain is decision request DR-07-02.

`ConsensusVersion` is 2, the first registry layout. Version 1 (`msgkit.ScaffoldLayoutVersion`) was the goal-05/06 scaffold slot, which stored exactly one key in this store: `[0x01]`, holding the module genesis JSON (an empty catalogue), and no layout version key. That key lies inside the record prefix `0x01`, and export of such a store panicked on it. Goal-06 state is not upgradable in place: no step from version 1 is registered (`RunMigrations` from 1 fails, `TestNoMigrationFromScaffoldLayout`), and `app.New` refuses to load committed state whose content or providers store lacks the version-2 layout key (`TestAppRefusesScaffoldLayout` in `chain/app`). `RegisterServices` registers the Msg service on the message router, the Query service on the query router, and one migration per version step from 2 (none at version 2). `TestRegisterMigrationsPath` is the evidence for the registration path: synthetic steps register in order, the module manager runs them, and a duplicate or nil step is an error. `TestRegistryMigrationPathInApp` (`chain/app`) runs a test step through the app's own configurator on committed state. `TestRunMigrationsAtCurrentVersionIsANoOp` only shows that nothing runs from version 2 to 2.

## Determinism

The module reads block time from the header, iterates stores in key order, and has no clock, randomness, environment, file, network, or float use. `TestDeterministicSources` runs the `msgkittest` source scan. It flags an import of `math/rand`, `math/rand/v2`, `crypto/rand`, `hash/maphash`, `io/ioutil`, `io/fs`, `plugin`, or anything under `os`, `net`, `runtime`, or `syscall`; the file-system functions of `path/filepath`; the clock, timer, and time-zone names of `time` (including `Local` and `LoadLocation`); and any floating-point type, literal, or `math` function. The scan is syntactic: a float another package's API returns, a method on a value, a goroutine, or a range over a map is not visible to it; those stay review items. It has no BeginBlock or EndBlock and returns no validator updates; x/authority stays the only validator-set writer. It does not import x/authority or the app; it reads roles, devnet parameters, and governed limits through the `Roles`, `DevnetParams`, and `Limits` interfaces.
