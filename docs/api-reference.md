# Protocol and API reference

Status of this document: a map of the contracts and commands that are already in the tree. It is not an approved protocol, not an audit, and not an interoperability claim.

Public launch: DISABLED

No tradable token, coin, sale, or mining.

## Contracts

`docs/protocol.md` is the draft contract. Machine-readable files:

- `docs/protocol/bounds.json`
- `docs/protocol/proto/`
- `docs/protocol/schemas/`
- `docs/protocol/api/client-api.json`
- `docs/protocol/api/provider-api.json`
- `docs/protocol/fixtures/`

`packages/protocol` loads the JSON schemas. Generated stubs under `proto/` are not a second contract. Unknown fields are rejected in v0.

## Commands in the candidate

| Command | Role |
| --- | --- |
| `svnd` | Chain binary. `svnd version` prints the revision. |
| `svn-api` | Upload and account service. It requires configuration and does not start without it. |
| `svn-feed-client` | Example client for a following feed on a loopback base URL. |
| `svn-discovery` | In-process discovery. It binds only on loopback. The smoke does not start it. |
| `svn-broadcaster` | Devnet transaction broadcaster. A chain id must start with the development prefix. |
| `svn-indexer` | Committed-state read model. |
| `svn-provider` | Storage provider agent. |
| `svn-operator` | Loopback operator dashboard. It does not take a consensus key. |
| `svn-observe` | Loopback readiness and admission. `GET /healthz` and `GET /readyz`. |
| `svn-restore` | Backup and restore tool. Retention is unset. |
| `svn-scheduler` | Host proposals. It does not sign for a provider. |
| `svn-chainview` | Development chain view. It does not sign. |

`apps/web` is the invited viewer source. `npm run build` is a separate step and is not part of the binary candidate.

## Deployment manifest

`deployments/profiles/profiles.json` is the reference manifest. It is NOT-RELEASED.
