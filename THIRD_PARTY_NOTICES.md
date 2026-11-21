# Third-party notices

Release label: NOT-RELEASED. Licence: UNCONFIRMED.

DEC-006 records a proposed split (AGPLv3 for original server and application code, Apache-2.0 for original protocol client libraries) and says to confirm compatibility before applying either. This tree does not apply that split. There is no `LICENSE` file. Nothing in this file is a releasable grant.

Direct dependency licences are the discovered facts in [docs/dependencies.md](docs/dependencies.md). The CycloneDX inventory is [docs/sbom.cdx.json](docs/sbom.cdx.json). Transitive licence texts stay in those locked artifacts. This file does not relicense them.

| Component | Pin | Licence as recorded |
| --- | --- | --- |
| Go | 1.26.10 | BSD-3-Clause |
| Cosmos SDK | v0.55.0 | Apache-2.0 |
| CometBFT | v0.40.0 | Apache-2.0 |
| Python | 3.12.3 | PSF licence |
| Node.js | 22.23.4 | MIT |
| npm | 10.9.9 | Artistic License 2.0 |
| protoc | 36.2 | BSD-3-Clause |
| React | 19.3.1 | MIT |
| TypeScript | 7.0.2 | Apache-2.0 |
| FastAPI | 0.162.0 | MIT |
| PostgreSQL | 18.6 | PostgreSQL License |
| lib/pq | v1.12.3 | MIT |
| go-webauthn | v0.18.2 | BSD-3-Clause |
| RabbitMQ | 4.4.0 | MPL-2.0 |
| FFmpeg | Ubuntu `7:6.1.1-3ubuntu5` | GPL build (`--enable-gpl`). Codec patents are not settled. |
| cryptography | 50.0.2 | Apache-2.0 OR BSD-3-Clause |

Cosmos Enterprise PoA and Enterprise Groups are not included. Loops, PeerTube, and Livepeer are not vendored.

FFmpeg build flags and the codec list are in [docs/operations/ffmpeg-obligations.md](docs/operations/ffmpeg-obligations.md).
