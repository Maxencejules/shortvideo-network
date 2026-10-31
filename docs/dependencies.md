# Dependencies

Selected on 2026-10-31 from the sources below. Versions are pins for this repository. They are not an audit. Goal 05 builds `svnd` from the Cosmos SDK and CometBFT pins.

Public launch: DISABLED. No tradable token, coin, sale, or mining.

The project licence split in DEC-006 is not applied. There is no `LICENSE` file. Goal 03 records dependency licences as discovered facts and does not treat the split as an applied licence. Compatibility still has to be confirmed before a later goal adds a licence file.

Cosmos Enterprise PoA and Enterprise Groups are commercial-boundary modules in the 2026.1 family. They are not requirements of this repository.

Loops, PeerTube, and Livepeer are references in the blueprint. Their code is not vendored.

This repository does not build or publish an image. Goal 04 records upstream image digests for the compose profile. Those digests are not a project image.

## Toolchains

| Tool | Pin | Source | Licence | Why this pin |
| --- | --- | --- | --- | --- |
| Go | 1.26.10, language 1.26.5 | https://go.dev/dl/go1.26.10.linux-amd64.tar.gz sha256 `ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5` | BSD-3-Clause, https://go.dev/LICENSE | Cosmos SDK v0.55.0 `go.mod` says `go 1.26.5`. 1.26.10 is the newest stable 1.26 patch on the go.dev JSON feed checked the same day. The chain is not built with the system Go 1.22.2. |
| Cosmos SDK | v0.55.0 | https://github.com/cosmos/cosmos-sdk/releases/tag/v0.55.0 and https://docs.cosmos.network/sdk/latest/release-family | Apache-2.0, https://github.com/cosmos/cosmos-sdk/blob/v0.55.0/LICENSE | 2026.1 family on the current release-family page is SDK 0.55.x. v0.55.0 was the only `v0.55` tag on 2026-10-31. |
| CometBFT | v0.40.0 | https://github.com/cometbft/cometbft/releases/tag/v0.40.0 | Apache-2.0, https://github.com/cometbft/cometbft/blob/v0.40.0/LICENSE | The same family lists CometBFT 0.40.x. SDK v0.55.0 `go.mod` requires `github.com/cometbft/cometbft v0.40.0`. v0.40.0 was the only `v0.40` tag. CometBFT v1.x is retracted and is not selected. |
| Python | 3.12.3 | https://www.python.org/downloads/release/python-3123/ | PSF licence, https://docs.python.org/3/license.html | CPython 3.12.3. CI installs that release with `actions/setup-python`, not the image's distro package. The workspace observed 3.12.3. |
| Node.js | 22.23.4 | https://nodejs.org/dist/index.json (LTS line Jod, 2026-10-20) | MIT, https://github.com/nodejs/node/blob/main/LICENSE | Current Node 22 release on that index the day of the pin. |
| npm | 10.9.9 | Ships with Node.js 22.23.4 | Artistic License 2.0, https://github.com/npm/cli | One package manager. pnpm and yarn are installed on the workstation and are not used. |
| protoc | 36.2 | https://github.com/protocolbuffers/protobuf/releases/download/v36.2/protoc-36.2-linux-x86_64.zip sha256 `121f6c7afe1d4d0e3ea6aab9432038599250134cbf4474cb1167d2c7decd4278` | BSD-3-Clause, https://github.com/protocolbuffers/protobuf/blob/v36.2/LICENSE | Latest stable protobuf release that was not a release candidate on 2026-10-31. |
| protoc-gen-go | v1.36.12 | `google.golang.org/protobuf/cmd/protoc-gen-go@v1.36.12` | BSD-3-Clause, https://github.com/protocolbuffers/protobuf-go/blob/v1.36.12/LICENSE | Latest listed module version on proxy.golang.org the day of the pin. |

`chain/tools` imports the SDK and CometBFT version packages under a `tools` build tag so `go.sum` locks the modules. `chain/app` does not import them. Goal 05 wires the application.

## Application families

| Family | Pin | Source | Licence | Why |
| --- | --- | --- | --- | --- |
| React | 19.3.1 | https://www.npmjs.com/package/react | MIT | REQ-TECH-001. Latest stable `npm view react version` on 2026-10-31. The web package does not render a product. |
| React DOM | 19.3.1 | https://www.npmjs.com/package/react-dom | MIT | Same release as React. |
| TypeScript | 7.0.2 | https://www.npmjs.com/package/typescript | Apache-2.0 | Latest stable `npm view typescript version` on 2026-10-31. |
| `@types/react` and `@types/react-dom` | 19.3.0 | https://www.npmjs.com/package/@types/react | MIT | Matching DefinitelyTyped line for React 19. |
| FastAPI | 0.162.0 | https://pypi.org/project/fastapi/ | MIT | REQ-TECH-002. Latest stable PyPI release on 2026-10-31. No route is implemented. |
| PostgreSQL | 18.6, package `18.6-1.pgdg24.04+2` | https://www.postgresql.org/support/versioning/ and https://apt.postgresql.org/pub/repos/apt noble-pgdg | PostgreSQL License, https://www.postgresql.org/about/licence/ | REQ-TECH-003. Current minor on the versioning page checked 2026-10-31. The apt key fingerprint is `B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8`. No Python client is pinned. Smoke tests use `psql`. |
| Erlang/OTP | 27.3.4.18, package `1:27.3.4.18-1rmq1ppa1~ubuntu24.04.1` | http://ppa.launchpad.net/rabbitmq/rabbitmq-erlang/ubuntu noble | Apache-2.0 | RabbitMQ 4.4 requires OTP 27 through 29. Ubuntu 24.04's archive package is OTP 25, which is too old. The Launchpad key fingerprint is `B279943D2A549531E144B875F77F1EDA57EBB1CC`. |
| RabbitMQ | 4.4.0 generic UNIX | https://github.com/rabbitmq/rabbitmq-server/releases/download/v4.4.0/rabbitmq-server-generic-unix-4.4.0.tar.xz sha256 `68c22d5dafa15c0911463aeca73c362d638cd6fb8e407ad39512959c6cc37f0f` | MPL-2.0 for the server. The tarball also ships component notices, including Apache-2.0. | Current stable release on 2026-10-31. The signature matched key `0A9AF2115F4687BD29803A206B73A36E6026DFCA`. The generic package starts without systemd. Ubuntu's `3.12.1` package is not used. |
| Celery | 5.6.3 | https://pypi.org/project/celery/5.6.3/ sdist sha256 `177006bd2054b882e9f01be59abd8529e88879ef50d7918a7050c5a9f4e12912` | BSD-3-Clause | Latest stable PyPI release on 2026-10-31. `5.7.0rc1` is a release candidate and is not selected. The package is not installed and is not in `requirements.lock`. Goal 14 imports the worker. The goal 04 queue proof is AMQP, not a Celery task. |
| FFmpeg | Ubuntu package `7:6.1.1-3ubuntu5` | http://archive.ubuntu.com/ubuntu noble/universe, deb sha256 `1a23baace5f2688a47e28119aedae6993cd638e6279a7227dfc52d9a337a1c17` | GPL build. `ffmpeg -version` reports `--enable-gpl`. | The binary on this cloud image and in the noble archive. FFmpeg 9.0.2 is a later source release and was not compiled here. Goal 14 records the isolated worker build. |
| amqp-tools | `0.11.0-1ubuntu0.2` | Ubuntu noble-updates, from rabbitmq-c, deb sha256 `0915bbfc3998bec2986d254555c3eb644eb96fc814d710cdb8314e5be49093f0` | MIT, https://github.com/alanxz/rabbitmq-c/blob/v0.11.0/LICENSE-MIT | The AMQP 0-9-1 client used for the development round trip. |

Hashes are also in `deployments/pins/checksums.txt`. The ffmpeg and amqp-tools lines are hashes of the downloaded debs. They were not compared to a signed Ubuntu Release file. apt checked those packages when it installed them. `.env.example` still has placeholders. Generated passwords stay in `.local/` and are not committed. Compose image digests are the linux/amd64 manifests, not the multi-arch indexes, read from `registry-1.docker.io` on 2026-10-31: PostgreSQL `sha256:f4b419e350fd98d4a8429603295208976dea4c8fb8326d818d346d3e29f1db6f` (index `sha256:979bccfde35445db618c65107d27d08c6c21339d8dc3ad4c830c6633947be0e4`) and RabbitMQ `sha256:9f48a2c0a501c5a7cf520d69eb3369ba85e614a195dde071f3dfa20077e54d26` (index `sha256:af6f50d38098d8bd52fef7f3fa85015dd8b2bf6def58e995bf584ae9ba0f6407`). This goal did not pull or run those images. Another architecture cannot use those digests as-is.

## Python, npm, and Go lockfiles

- `requirements.lock` is `pip-compile --generate-hashes --allow-unsafe` output. `--allow-unsafe` keeps `setuptools` hashed. Install with `pip install --require-hashes -r requirements.lock`.
- `package-lock.json` is the npm lock for the workspace.
- `chain/go.sum`, `proto/go.sum`, and `go.work.sum` are the Go locks.
- `docs/sbom.cdx.json` is a CycloneDX 1.6 inventory generated from those lockfiles by `scripts/write_sbom.py`. It does not copy licences into every transitive row. Direct licences are the table above plus the package metadata recorded when the lockfiles were produced.

Direct Python packages in `pyproject.toml`:

| Package | Version | Licence expression from PyPI |
| --- | --- | --- |
| fastapi | 0.162.0 | MIT |
| cryptography | 50.0.2 | Apache-2.0 OR BSD-3-Clause |
| jsonschema | 4.26.0 | MIT |
| protobuf | 7.36.2 | BSD-3-Clause |
| mypy | 2.4.1 | MIT |
| pip-tools | 7.6.2 | BSD-3-Clause |
| pytest | 9.2.0 | MIT |
| pyyaml | 6.0.3 | MIT |
| ruff | 0.17.3 | MIT |
| setuptools | 84.2.1 | MIT |
| types-PyYAML | 6.0.12.20261030 | Apache-2.0 |
| types-jsonschema | 4.26.0.20261006 | Apache-2.0 |

`cryptography` is the same library the Goal 02 vector checker imports. Transitive packages and their licences are whatever those releases declare in the installed metadata. The lockfile hashes pin the artifacts. Re-read a transitive licence before redistributing a binary.

## GitHub Actions

Checked with `gh api repos/<action>/commits/<tag>` on 2026-10-31. Each tag resolved to a commit whose GitHub verification status was `verified: true`.

| Action | Tag | Commit |
| --- | --- | --- |
| actions/checkout | v5.1.0 | `fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09` |
| actions/setup-go | v6.5.0 | `924ae3a1cded613372ab5595356fb5720e22ba16` |
| actions/setup-node | v6.5.0 | `249970729cb0ef3589644e2896645e5dc5ba9c38` |
| actions/setup-python | v6.3.0 | `ece7cb06caefa5fff74198d8649806c4678c61a1` |

Hosted runner label `ubuntu-24.04` is not a digest. This repository does not publish an image. The compose file pins upstream image digests. Those are not a project image.

## Not selected

- CometBFT v1.x
- Cosmos SDK outside 0.55.x
- Enterprise PoA and Enterprise Groups
- IBC, EVM, and a relayer (the blueprint does not require them for the first chain)
- A project `LICENSE` file
