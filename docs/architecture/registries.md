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

A service credential never stands in for a provider signature: a provider rotation or drain needs the provider's current ed25519 key, and a service account that sends `UpdateProvider` is `ERROR_CODE_FORBIDDEN`. The hosting intent proves that the key in the message signed the payload. It does not prove a creator authorized the host, because no intent key is registered on chain and DEC-001 key custody is unset (decision request DR-07-01). Without the DEC-001 devnet test parameter, content registration is `ERROR_CODE_DECISION_BLOCKED`.

Council admission needs the DEC-002 devnet parameter and operator group `devnet-test-only`; forced withdrawal needs the DEC-003 devnet parameter. Otherwise the execution is `ERROR_CODE_DECISION_BLOCKED` and the proposal stays `QUEUED`. An operator group id is a label, not proof of an independent operator. The DEC-002 membership policy stays blocked.

## Rejection

Every handler runs all checks before its first write. A rejected message changes no module store: the app tests compare `App.FullSnapshot()` (every key and value of every mounted store except the auth account store) with `bytes.Equal` before and after each reject class, and the devnet demo repeats that comparison on the replayed blocks. The account sequence still advances when the ante handler succeeded; that is not a registry decision. Unknown proto fields fail transaction decoding (SDK codespace `sdk`, code 2) before any handler runs. Mapping that to `ERROR_CODE_UNKNOWN_FIELD` is a follow-up.

The same business id with the same payload is idempotent: code 0, no write, no typed event, one `<module>_result` event with `idempotent=true`. The same id with another payload is `ERROR_CODE_REPLAY`, also across modules. A retry of a committed rotation is `ERROR_CODE_UNAUTHORIZED`, because the retry is signed by the key it retired; a broadcaster confirms a rotation by querying the provider.

## Disablement and withdrawal

- New obligations need an `ADMITTED` provider. `APPLIED`, `DRAINING`, and `DISABLED` providers are refused by `CheckNewObligation` (Goal 08 calls it from offer and accept).
- Drain and disablement release nothing. Existing reservations end only by their own legal transitions (Goal 08). `DISABLED` is terminal. No provider record, retired key, event, or transaction is deleted.
- A `WITHDRAWN` content record keeps its row and gains a tombstone (`history_erased` false, retention decision DEC-004, value version 0). Withdrawal blocks new offers through `CheckOfferable` and `CheckNotWithdrawn` (Goal 08). Re-registering the id is `ERROR_CODE_DUPLICATE_ID`; there is no silent revival. A new encoding is a new `content_id` in protocol v0 (decision request DR-07-04).
- Withdrawal does not release accepted reservations. Goal 08 wires the withdrawal hook that voids open offers; Goal 07 wires none, because no offer store exists.

## What a registry entry is not

A provider record is not evidence of an independent operator, of physical capacity, or of reachability. A content record is not copyright proof, not a hosting receipt, and not publication approval: `PENDING` is not `PUBLISHED`. The chain stores ids, 20-byte accounts, 32-byte public keys, SHA-256 digests, byte counts, states, and block times. Endpoint URLs, manifests, media bytes, emails, usernames, watch events, and complaint text stay off chain; a test scans every stored key and value, every committed event, and the event fixture for synthetic canaries of each. Opaque ids and digests can still be linkable by anyone who holds the off-chain input.

## Reading committed records

| Path | Request | Notes |
| --- | --- | --- |
| `/svn.providers.v1.Query/Provider` | `QueryProviderRequest{provider_id}` | Every state is returned; nothing is deleted. |
| `/svn.providers.v1.Query/Providers` | `QueryProvidersRequest{page}` | Page limit 0 means the effective `max_page_limit` (at most 100); above it is rejected. `page_key` is inclusive, at most 64 bytes. `next_page_key` is empty on the last page. |
| `/svn.content.v1.Query/Content` | `QueryContentRequest{content_id, content_version}` | Returns the record and, when withdrawn, its tombstone. There is no content list query in v0 (decision request DR-07-07). |

The same paths are served by CometBFT `abci_query` (data is the protobuf request, hex encoded; `height` selects a committed version) and by the node gRPC server (`x-cosmos-block-height` metadata selects the height). The REST gateway is disabled. Over ABCI query the SDK reports a rejected query as codespace `sdk` code 6 with the protocol name (for example `ERROR_CODE_NOT_FOUND`) in the log; the keepers return the typed `svn` error. `svnd` has no query subcommand yet.

## Genesis

The launch genesis keeps providers, content, and tombstones empty (goal 05 rule, REQ-GEN-002, and the `svn.content.v1.GenesisState` comment); InitChain rejects a non-empty catalogue. Each module still validates, imports, and exports a non-empty catalogue, and the export of committed state imports into a fresh store with the same record bytes. Business ids are not in the v0 genesis schema, so an export does not carry replay protection (decision request DR-07-03). Importing an exported catalogue at InitChain, and seeding admitted providers in a launch genesis, are decision request DR-07-02.

Each registry InitGenesis writes its store layout version at key `0x00`, so neither store is ever an empty IAVL tree. An empty tree cannot be reloaded from the node's goleveldb database (iavl v1.2.8 saves its root marker as an empty value, which cosmos-db v1.1.3 goleveldb reports as absent), so a node with an empty registry store could not restart. `TestRestartLoadsCommittedState` reopens an application database on disk.

## Handoff to Goal 08

- `providers.Keeper.Provider`, `CheckNewObligation` (`NOT_FOUND`, then `FORBIDDEN` unless `ADMITTED`), and `VerifyProviderSignature` (current key only; a retired key is `ERROR_CODE_UNAUTHORIZED`).
- `content.Keeper.Package`, `CheckOfferable` (`NOT_FOUND`, `WITHDRAWN`, `CONFLICT`, `INTEGRITY` in `op_offer` order), `CheckNotWithdrawn`, and the `WithdrawalHook` interface.
- Reserve and release accounting, and the renewal rule after disablement (decision request DR-07-05), are Goal 08.
