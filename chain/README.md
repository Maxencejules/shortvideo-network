# chain

`svnd` is the application binary of the permissioned beta. It uses Cosmos SDK v0.55.0 and CometBFT v0.40.0. It is not a release, and public launch is disabled.

The binary wires auth accounts, signatures, sequences, transaction size, and gas limits. It registers the six module slots from the blueprint:

| Module | State | Messages |
| --- | --- | --- |
| `x/authority` (Goal 06) | Validator set, admitted accounts, devnet council, proposals | `ProposeChange`, `ApproveChange`, `ExecuteChange` |
| `x/providers` (Goal 07) | Provider registry | `RegisterProvider`, `UpdateProvider`, `RotateProviderKey`, `DrainProvider` |
| `x/content` (Goal 07) | Content coordination registry and tombstones | `RegisterContent`, `WithdrawContent` |
| `leases`, `evidence` | Empty slots | Registered and rejected with `ERROR_CODE_FORBIDDEN` until Goals 08-09 |
| `svn`, `policy` | Genesis header, consensus parameters, safety caps | None until Goal 10 |

`x/authority` is the only module that returns validator updates. Council admission, disablement, and forced withdrawal are council payloads executed through `x/authority`; there is no single-signer admit, disable, or force-withdraw message (DEC-009). Business ids are one namespace across authority, providers, and content.

Queries: `svn.providers.v1.Query` (`Provider`, `Providers`) and `svn.content.v1.Query` (`Content`) are served by ABCI query (`/svn.providers.v1.Query/Provider` and so on, with a height) and by the node gRPC server. The REST gateway is disabled. See `x/providers/README.md` and `x/content/README.md`.

A registry entry is a coordination record. It is not copyright proof, a hosting receipt, or publication approval. The protocol has no field for endpoint URLs, manifests, media bytes, or personal data; the chain stores digests, byte counts, ids, and states. Unknown fields (critical and non-critical) fail transaction decoding, and the ante handler rejects a memo, a fee payer, a fee granter, and a tip. Ids are still text the submitter chooses: an email or a URL does not match the id pattern and is rejected, but an id-shaped value such as a username is stored as given, and a transaction a handler rejects stays in its block. Opaque ids and digests can still be linkable.

Staking, bank, mint, distribution, slashing, governance-by-token, EVM, and IBC are not imported. There is no coin denomination and no fee amount.

`make devnet-up` starts four processes on one machine. `make devnet-down` stops them without deleting genesis. `make devnet-resume` starts the same homes again. The devnet genesis has runtime-generated accounts `authority`, `service`, `service-2`, `provider-1`, `provider-2`, and four council seats; their keys stay under the home directory. `go test ./chain/devnet -run TestDevnetRegistryDemo -v` runs the Goal 07 registry demo against four real processes.
