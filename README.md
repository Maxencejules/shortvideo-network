# shortvideo-network

Permissioned community beta for a decentralized short-video network with a custom application blockchain. This tree has the specification, frozen protocol v0 contracts, and a build skeleton. It is not a running network. Public launch is disabled. There is no token, coin, sale, or mining.

Protocol v0 is frozen for goals 03-18. Schema files under `docs/protocol/` are the contract. Generated stubs under `proto/` are produced from those files and are not a second contract.

## Layout

| Path | What is here now |
| --- | --- |
| `apps/web` | Invited viewer and creator pages. |
| `services/api` | Accounts, uploads, discovery, and the example feed client. `svn_api.serve` still raises. |
| `services/indexer` | Committed-state indexer. PostgreSQL read model of committed blocks. |
| `services/broadcaster` | Devnet transaction broadcaster. Its design is PROPOSED in `docs/architecture/broadcaster.md`. |
| `chain` | `svnd`, the Cosmos SDK and CometBFT application. |
| `proto` | Generated Go and Python stubs. Sources stay in `docs/protocol`. |
| `agents/provider` | `svn-provider`, the loopback storage agent. |
| `agents/auditor` | Audit producer for permitted test endpoints. Attestations, not proofs. |
| `workers/media` | Isolated FFmpeg worker. It does not publish. |
| `services/delivery` | Loopback HTTPS HLS for a ready package. A local grant is not publication. |
| `services/operator` | Loopback operator dashboard. It does not take a consensus key. |
| `services/observe` | Loopback readiness, admission, and the cost worksheet. |
| `services/restore` | Backup and restore. Retention is unset. |
| `packages/protocol` | Loader for the frozen JSON schemas. |
| `deployments` | Reference profiles, local Dockerfiles, and checksum pins. No image is published. |
| `tests` | Unit tests. Contract entry point is `make contract`. |
| `docs` | Specification, protocol, operations, dependencies, evidence. |

## Install

The checked pins and checksums are in [docs/dependencies.md](docs/dependencies.md). From a clean checkout:

```bash
bash scripts/install_toolchains.sh
export PATH="$(pwd)/.toolchains/go/bin:$(pwd)/.toolchains/bin:$PATH"
python3 -m venv .venv
.venv/bin/pip install --require-hashes -r requirements.lock
npm ci
make proto-check
make check
```

`make check` formats, lints, type-checks, regenerates contracts, runs unit tests, and runs the Goal 01 and Goal 02 checkers. `make smoke` and `make fault` exit non-zero. `make release-check` is the packaging gate. `make release-candidate` builds a local candidate and labels it NOT-RELEASED. It does not tag or publish. Development services are separate: [docs/environment.md](docs/environment.md).

The operator quickstart is [docs/operations/operator-quickstart.md](docs/operations/operator-quickstart.md). The candidate is not a release. Goals 28-30 and the human launch gates have not passed. There is no `LICENSE` file. DEC-006 is not applied.

Copy `.env.example` to `.env` if a later goal needs local placeholders. `.env` is gitignored. The example file contains `REPLACE_ME` and no live credential.

## Documents

- Beta specification: [docs/requirements.md](docs/requirements.md), [docs/threat-model.md](docs/threat-model.md), and [docs/decision-register.md](docs/decision-register.md)
- Protocol v0: [docs/protocol.md](docs/protocol.md)
- Dependencies: [docs/dependencies.md](docs/dependencies.md)
- Ownership: [docs/ownership.md](docs/ownership.md)
- Agent contract: [AGENTS.md](AGENTS.md)
- Blueprint: [docs/blueprint.md](docs/blueprint.md), [docs/blueprint.txt](docs/blueprint.txt), and [docs/blueprint.pdf](docs/blueprint.pdf)
- Prompt playbook: [docs/prompt-playbook.txt](docs/prompt-playbook.txt) and [docs/prompt-playbook.pdf](docs/prompt-playbook.pdf)
- Goals 01-30: [docs/goals/](docs/goals/)
- Progress: [docs/progress.md](docs/progress.md) and [docs/progress.tsv](docs/progress.tsv)
- Evidence: [docs/evidence/README.md](docs/evidence/README.md)
- API reference: [docs/api-reference.md](docs/api-reference.md)
- Contributing: [CONTRIBUTING.md](CONTRIBUTING.md)
- Security: [SECURITY.md](SECURITY.md)
- Notices: [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)
