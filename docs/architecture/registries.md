# Provider and content registries

Reviewer note for Goal 07. This is not an audit. Public launch is disabled. There is no token, fee denomination, or staking module. The module READMEs (`chain/x/providers/README.md`, `chain/x/content/README.md`) hold the full check orders, store layouts, and events.

## Wiring

`app.New` builds the keepers in this order:

1. An `x/authority` keeper. Its read surface (`Role`, `DevnetRole`, `GovernedLimit`) is what the registries consume.
2. `x/providers` and `x/content` keepers with that read surface and one shared `msgkit.Peers` set.
3. The authority keeper used by the app: `WithRegistry(providers, content).WithPeers(peers)`. Council payloads reach the registries only through it.
4. `peers` lists `authority`, `content`, and `providers`. A business id is one namespace across the three modules.

Each registry module registers its `svn.<module>.v1.Msg` service on the message router and its `svn.<module>.v1.Query` service on the gRPC query router (`RegisterServices`). Lease and evidence messages stay closed stubs that return `ERROR_CODE_FORBIDDEN`. `x/authority` is still the only module that returns validator updates; neither registry has an EndBlock.

## Who may write

| Action | Outer signer | Inner signature |
| --- | --- | --- |
| `RegisterProvider` | `ADMITTED_PROVIDER` account | None |
| `UpdateProvider` | The account stored in the provider record | None |
| `RotateProviderKey` | The account stored in the provider record | Current provider key, domain `SVN-PROVIDER-ROTATE-1` |
| `DrainProvider` | The account stored in the provider record | Current provider key, domain `SVN-PROVIDER-DRAIN-1` |
| `RegisterContent` | `ADMITTED_SERVICE` account (becomes `submitting_host`) | Hosting intent, domain `SVN-HOSTING-INTENT-1`, devnet only |
| `WithdrawContent` | The submitting host | Hosting intent, devnet only |
| Admit, disable, force-withdraw | No direct message (DEC-009) | Council proposal, three of four seats, executed by the authority account |

No other account reaches these handlers. A council seat, a `BROADCASTER` that is not a seat, the `AUTHORITY` account and an `AUDITOR` get `ERROR_CODE_FORBIDDEN` for each of the six messages, as do the service account for the provider messages and a provider account for another provider's record or for content (`TestRegistryMessagesRejectOtherRoles`, 39 rows, full state unchanged). Genesis refuses a council seat address listed with any role but `BROADCASTER` (decision request DR-07-11), so a seat key cannot hold a registry role.

Recorded deviation: the relay column of the `protocol.md` transition table lets a broadcaster submit the outer transaction of `RegisterProvider`, `RegisterContent` and `WithdrawContent`. The registries follow `spec_machine.py` `check_role` instead. The outer signer is the account that owns the record (`account`, or `submitting_host`, which the hosting intent is bound to), so a broadcaster outer signer is `ERROR_CODE_FORBIDDEN`. Adopting the relay column would give every `BROADCASTER` key, council seats included, registry reach (DR-06-06).

`protocol.md` says a direct `AdmitProvider`, `DisableProvider`, or `ForceWithdraw` is `ERROR_CODE_FORBIDDEN` (codespace `svn`, code 2). No such message type is registered, so such a transaction fails decoding instead: codespace `sdk`, code 2 (`ErrTxDecode`). The number is the same and the codespace is not; this is a recorded deviation (`TestDirectAdminMessagesDoNotExist`).

A service credential never stands in for a provider signature: a provider rotation or drain needs the provider's current ed25519 key, and a service account that sends `UpdateProvider` is `ERROR_CODE_FORBIDDEN`. The hosting intent proves that the key in the message signed the payload. It does not prove a creator authorized the host, because no intent key is registered on chain and DEC-001 key custody is unset (decision request DR-07-01). Without the DEC-001 devnet test parameter, content registration is `ERROR_CODE_DECISION_BLOCKED`.

Council admission needs the DEC-002 devnet parameter; forced withdrawal needs the DEC-003 devnet parameter. Without it the execution is `ERROR_CODE_DECISION_BLOCKED`, the transaction fails, and the proposal stays `QUEUED`, so a later execution can still apply it. Admission of a provider whose operator group is not `devnet-test-only`, or that is not `APPLIED`, is `ERROR_CODE_FORBIDDEN`: x/authority keeps that result, the transaction succeeds with `authority_result` `ERROR_CODE_FORBIDDEN`, and the proposal is saved as `REJECTED`, which is terminal (`TestRejectedRegistryMessagesLeaveTheFullState`, case "execute admit p3"). An operator group id is a label, not proof of an independent operator. The DEC-002 membership policy stays blocked.

## Rejection

Every handler runs all checks before its first write. A rejected message changes no module store: the app tests compare `App.FullSnapshot()` (every key and value of every mounted store except the auth account store) with `bytes.Equal` before and after each reject class, and the devnet demo repeats that comparison on the replayed blocks. The account sequence still advances when the ante handler succeeded; that is not a registry decision.

Simulation (`/cosmos.tx.v1beta1.Service/Simulate`) runs every registry and council message to the end and reports the gas a committed transaction of the same bytes uses (`TestSimulateEstimatesGas`). The SDK simulates on an infinite gas meter, so the handlers skip only their meter-limit check in simulation, and the ante handler accepts a declared gas limit of 0 only there (the SDK client's gas=auto flow). In CheckTx and FinalizeBlock every bound is unchanged: gas 0 is `ERROR_CODE_BOUNDS`.

Unknown proto fields fail transaction decoding (SDK codespace `sdk`, code 2) before any handler runs, critical and non-critical alike. The SDK's default decoder lets unknown fields with bit 0x400 set (numbers 1024 to 2047, 3072 to 4095, ...) through the body and every message in it; `app.strictTxDecoder` checks the body bytes again in strict mode, so they fail too. Extension options fail decoding as well. The ante handler rejects a non-empty memo (`ERROR_CODE_BOUNDS`; the SDK default would allow 256 characters) and a fee payer, fee granter, or tip (`ERROR_CODE_FEE_REJECTED`). `TestTransactionsRejectVideoBytes` covers each case with 40 KiB of synthetic media or the canary text in the signed bytes. Mapping the decode error to `ERROR_CODE_UNKNOWN_FIELD` is a follow-up.

The same business id with the same payload is idempotent: code 0, no write, no typed event, one `<module>_result` event with `idempotent=true`. The same id with another payload is `ERROR_CODE_REPLAY`, also across modules. One exception is stricter than the spec machine (decision request DR-07-08): the `RegisterProvider` digest does not cover the signer, so a copy of a registration sent by another provider account would match; it is `ERROR_CODE_FORBIDDEN`, because only the account that owns the record gets the idempotent success. Registration has no proof of possession of the signing key, so the first account to commit a copy owns the record. A retry of a committed rotation is `ERROR_CODE_UNAUTHORIZED`, because the retry is signed by the key it retired; a broadcaster confirms a rotation by querying the provider.

## Disablement and withdrawal

- New obligations need an `ADMITTED` provider. `APPLIED`, `DRAINING`, and `DISABLED` providers are refused by `CheckNewObligation` (Goal 08 calls it from offer and accept).
- Drain and disablement release nothing. Existing reservations end only by their own legal transitions (Goal 08). `DISABLED` is terminal. No provider record, retired key, event, or transaction is deleted.
- A provider cannot rotate its way out of council disablement: retired keys are store rows of their own, so the provider record a disable rewrites has a constant size however often the provider rotated (`TestCouncilDisableGasDoesNotGrowWithRotations`).
- A `WITHDRAWN` content record keeps its row and gains a tombstone (`history_erased` false, retention decision DEC-004, value version 0). Withdrawal blocks new offers through `CheckOfferable` and `CheckNotWithdrawn` (Goal 08). Re-registering the id is `ERROR_CODE_DUPLICATE_ID`; there is no silent revival. A new encoding is a new `content_id` in protocol v0 (decision request DR-07-04).
- Withdrawal does not release accepted reservations. Goal 08 wires the withdrawal hook that voids open offers; Goal 07 wires none, because no offer store exists.

## What a registry entry is not

A provider record is not evidence of an independent operator, of physical capacity, or of reachability. A content record is not copyright proof, not a hosting receipt, and not publication approval: `PENDING` is not `PUBLISHED`. The chain stores ids, 20-byte accounts, 32-byte public keys, SHA-256 digests, byte counts, states, and block times. The protocol has no field for endpoint URLs, manifests, media bytes, emails, usernames, watch events, or complaint text.

What the chain enforces is tested with the private text inside submitted transactions. `TestSubmitterTextFields` puts a synthetic email and URL into every id field of the six registry messages. An email or a URL never matches the id pattern: where the handler checks the pattern (registration ids, business ids, the new key id, signature key ids) the result is `ERROR_CODE_BOUNDS`; where it looks the id up first (`provider_id` of `UpdateProvider`, `RotateProviderKey`, and `DrainProvider`, `content_id` of `WithdrawContent`) the result is `ERROR_CODE_NOT_FOUND`. Either way the full state is byte-identical and no store holds the text. `TestTransactionsRejectVideoBytes` covers synthetic media and the canaries in unknown fields and in the memo, fee payer, fee granter, and tip.

The canary scans of `TestNoPrivateDataInStateOrEvents` (stores, committed transactions, events, export) and of the devnet demo (`/block`, `/block_results`) are a different check: those flows hash the canaries before they build a transaction, so the scans confirm that the client submits only digests and cannot fail because of chain behaviour. Their chain-side part is the forbidden-name scan of event types, attribute keys, and export keys.

What the chain does not enforce: ids are text the submitter chooses, and an id-shaped value such as a username is stored as given (`TestSubmitterTextFields` asserts that too). A transaction that a handler rejects is still in its block, so its bytes stay in the block data even though state does not change; only a decode or ante rejection keeps a transaction out of the mempool. Opaque ids and digests can still be linkable by anyone who holds the off-chain input.

## Reading committed records

| Path | Request | Notes |
| --- | --- | --- |
| `/svn.providers.v1.Query/Provider` | `QueryProviderRequest{provider_id}` | Every state is returned; nothing is deleted. |
| `/svn.providers.v1.Query/Providers` | `QueryProvidersRequest{page}` | Page limit 0 means the effective `max_page_limit` (at most 100); above it is rejected. `page_key` is inclusive, at most 64 bytes. `next_page_key` is empty on the last page. |
| `/svn.content.v1.Query/Content` | `QueryContentRequest{content_id, content_version}` | Returns the record and, when withdrawn, its tombstone. There is no content list query in v0 (decision request DR-07-07). |

A `Providers` page also ends early, before the provider that would take it above 1024 retired keys (`providers.MaxRetiredKeysPerPage`); `next_page_key` names that provider. The first provider of a page is always returned; it fits, because one provider id may retire at most 1024 keys (`providers.MaxRetiredKeysPerProvider`). The rotation that would retire one more is `ERROR_CODE_BOUNDS` and leaves the full state byte-identical (`TestRotationCapInApp`); the provider registers a new provider id instead. Under a capped id the provider can no longer rotate away from a compromised signing key; the remedies are drain, council disable, and a new id that needs council admission again. That cap is not a v0 spec rule: it is PROPOSED, and decision request DR-07-10 asks the owner to approve the number or to choose pruning of old keys. Without it, rotations, which cost no fee and are allowed in every state, grew one provider's `Provider` response, the page that starts with it, and its export without limit.

What bounds a response: `Provider` carries at most 1024 retired keys, a `Providers` page at most 100 records and 1024 retired keys, an exported provider record at most 1024 retired keys. With every field at its largest encoding they stay within 64 KiB and 128 KiB of protobuf and 192 KiB of protojson (`TestQueryResponseSizeBound`), far below the 4 MiB a default gRPC client accepts. A whole export grows with the number of providers, which has no per-account limit while DEC-005 allowances are unset.

What bounds query work: those reads (at most 1024 retired-key rows and 101 records per call) and the node's query gas limit. `svnd` meters every gRPC-routed query (ABCI query on a registered service path, and the node gRPC server) against `query_gas_limit` from `node.json`, or 20,000,000 (`app.DefaultQueryGasLimit`, equal to the transaction gas cap) when it is absent or 0; unlike the SDK, 0 never means unlimited. The default is a PROPOSED node setting in DR-07-10, not consensus state. A query above the limit fails. Over `abci_query` the result is the SDK's recovered-panic result (codespace `undefined`, code 111222); `TestQueryGasLimit` checks that and the out-of-gas panic in the query router. The node gRPC server converts the same panic to `ErrOutOfGas` (`sdk` code 11) in SDK code (`baseapp/grpcserver.go`); no test here drives the gRPC server path. `Provider` at the retired-key bound uses about 200,000 gas. Raw store queries (`abci_query` on `/store/<store>/key` or `/store/<store>/subspace`) are not metered or paged: the SDK serves them from the IAVL store directly, so a subspace query returns every row under a prefix and grows with the whole registry. `svnd` does not filter them; the CometBFT RPC port must not be exposed publicly. Rejecting `/store` paths in the node is a follow-up.

The same paths are served by CometBFT `abci_query` (data is the protobuf request, hex encoded; `height` selects a committed version) and by the node gRPC server (`x-cosmos-block-height` metadata selects the height). The REST gateway is disabled.

A rejected query carries the protocol code as a gRPC status detail: a `google.rpc.ErrorInfo` with domain `svn`, reason the protocol name (for example `ERROR_CODE_NOT_FOUND`), and metadata `code` the number; the status code is `NotFound` or `InvalidArgument`. The node gRPC server returns it and `msgkit.QueryCode` decodes it (the devnet demo does, on node v1). Over `abci_query` the SDK keeps only the gRPC status code, as codespace `sdk` `ErrKeyNotFound` (38) or `ErrInvalidRequest` (18); the protocol name is then only in the log text, and a client that needs the protocol code should use gRPC. `svnd` has no query subcommand yet.

Committed events end with a `msg_index` attribute that the SDK appends to every event a message emits. It is not part of the `svn.events.v1` schema. An indexer reading `/block_results` or FinalizeBlock results parses each typed event with `msgkit.ParseCommitted`, which returns the message and the index; `msgkit.ParseTyped` takes only the schema attributes.

## Genesis

The launch genesis keeps providers, content, and tombstones empty (goal 05 rule, REQ-GEN-002, and the `svn.content.v1.GenesisState` comment); InitChain rejects a non-empty catalogue. Each module still validates, imports, and exports a non-empty catalogue, and the export of committed state imports into a fresh module store with the same record bytes (`TestGenesisRoundTripFromCommittedState`, which imports into bare module keepers, not into an app).

An app-level round trip does not work. `App.Export()` applies the same launch rule and refuses once a provider or content record exists: `export refused: exported genesis does not validate: providers are not empty` (or `content is not empty`) on a chain with no council message (`TestExportRefusesANonEmptyCatalogue`); after a council message it refuses earlier, on the authority store (DR-06-01). InitChain refuses a non-empty catalogue too, which `TestLaunchGenesisRejectsNonEmptyCatalog` shows with the registry modules' own `ExportGenesis` output. Protocol v0 requires an empty content catalogue at genesis but not an empty provider list (`svn.app.v1.AppGenesis` carries `providers`); the provider rule is stricter than v0 and stays until the owner decides how `ADMITTED` providers in a genesis relate to council admission (DEC-009). App-level export and re-import is therefore BLOCKED on decision requests DR-07-02 (genesis catalogue) and DR-07-03 (business ids are not in the v0 genesis schema, so an export does not carry replay protection).

Each registry InitGenesis writes its store layout version (its consensus version, 2) at key `0x00`, so neither store is ever an empty IAVL tree. An empty tree cannot be reloaded from the node's goleveldb database (iavl v1.2.8 saves its root marker as an empty value, which cosmos-db v1.1.3 goleveldb reports as absent), so a node with an empty registry store could not restart. `TestRestartLoadsCommittedState` reopens an application database on disk.

Layout version 1 was the goal-05/06 scaffold slot of each registry store: exactly one key, `[0x01]`, holding the module genesis JSON (an empty catalogue), and no layout version key. That key lies inside both registries' record prefix `0x01`, so this binary would misread it (export and `Providers` failed to decode it). The registries therefore start at consensus version 2, register no step from version 1 (`RunMigrations` from 1 fails, `TestNoMigrationFromScaffoldLayout` in each module), and `app.New` refuses to load committed state whose providers or content store lacks the version-2 layout key (`TestAppRefusesScaffoldLayout`). Goal-06 state is not upgradable in place; a goal-06 home must be initialized again (x/authority's proposal records also gained two fields, `content_id` and `content_version`, so a goal-06 record does not decode; authority's `0x17` provider-row prefix is reserved, never written, and refused by export). No deployed chain exists.

## Handoff to Goal 08

- `providers.Keeper.Provider` (the record without its retired keys, constant work), `CheckNewObligation` (`NOT_FOUND`, then `FORBIDDEN` unless `ADMITTED`), and `VerifyProviderSignature` (current key only; any other key is `ERROR_CODE_UNAUTHORIZED`). The v0 spec lets a provider rotate back to a key it retired; that key is then the current key and verifies again.
- `content.Keeper.Package`, `CheckOfferable` (`NOT_FOUND`, `WITHDRAWN`, `CONFLICT`, `INTEGRITY` in `op_offer` order), `CheckNotWithdrawn`, and the `WithdrawalHook` interface.
- Reserve and release accounting, and the renewal rule after disablement (decision request DR-07-05), are Goal 08.
