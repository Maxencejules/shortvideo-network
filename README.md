# shortvideo-network

Permissioned community beta for a decentralized short-video network with a custom application blockchain. This tree has the specification, frozen protocol v0 contracts, and a build skeleton. It is not a running network. Public launch is disabled. There is no token, coin, sale, or mining.

Protocol v0 is frozen for goals 03-18. Schema files under `docs/protocol/` are the contract. Generated stubs under `proto/` are produced from those files and are not a second contract.

## Layout

| Path | What is here now |
| --- | --- |
| `apps/web` | React and TypeScript pin. No product UI. |
| `services/api` | FastAPI pin. `serve()` raises. |
| `services/indexer` | Indexer slot. No chain queries. |
| `services/broadcaster` | Broadcaster slot. No transactions. |
| `chain` | SDK and CometBFT module pins. No chain binary. |
| `proto` | Generated Go and Python stubs. Sources stay in `docs/protocol/proto`. |
| `agents/provider` | Provider-agent slot. No process. |
| `workers/media` | Media-worker slot. FFmpeg is not pinned and is not run. |
| `packages/protocol` | Loader for the frozen JSON schemas. |
| `deployments` | Notes only. No image and no compose file. |
| `tests` | Unit tests. Contract entry point is `make contract`. End-to-end and fault targets fail closed. |
| `docs` | Specification, protocol, dependencies, ownership, evidence. |

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

`make check` formats, lints, type-checks, regenerates contracts, runs unit tests, and runs the Goal 01 and Goal 02 checkers. `make devnet-up`, `make smoke`, `make fault`, and `make release-check` exit non-zero. Those commands are not implemented.

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
