# Goal 27 evidence

Status: BLOCKED

The local candidate is labelled NOT-RELEASED. A clean clone built the packaged binaries twice, installed them, ran the smoke, switched the install link forward and back, and uninstalled without removing planted operator data. DEC-006 is not applied. There is no `LICENSE` file. No tag, package, or image was published. This is not an audit, not an interoperability result, and not independent operation. Public launch stays disabled. There is no token.

## Model and effort

- Playbook assignment: GPT-6.1 Sol, effort HIGH (`docs/goals/27-packaging-installation-and-release-preparation.md` line 6). GPT-6.1 Sol was not used.
- This run is Cursor cloud agent `bc-6f26071a-c680-5388-a109-ab03709fa5a4`. `cursor-cloud` `run-info` reported `originalModelName` `grok-4.7-high-fast` and no separate effort or reasoning field. The effort setting is **not reported**. It is not guessed. That is the account default model for this run. The playbook's GPT-6.1 Sol was replaced by this Cursor cloud agent on that default model.
- On 2026-10-31 (Toronto) the owner said "Don't mind the ai model requirement". On 2026-11-17 the owner confirmed that it applies to all goals. A model mismatch with the playbook is not a blocker. The evidence still records the model that actually ran.

## Tested revision

| Item | Value |
| --- | --- |
| Commit | `ea1a6aeeb4845df0cb29c913f2f39872c3feac55` |
| Tree | `49103a38fc1a816cf320e2000163efc863e94e10` |
| Base | `8d93835375f268bc37eab58a001f118cdd3953ff` |
| Branch | `goal-27` |
| Pull request | https://github.com/Maxencejules/shortvideo-network/pull/32 |

The tested commit message carries `Tested-tree: 49103a38fc1a816cf320e2000163efc863e94e10`. `git rev-parse ea1a6aeeb4845df0cb29c913f2f39872c3feac55^{tree}` is that tree. Later commits change only `docs/evidence/`.

## Captures

Every log under [logs/goal-27/](logs/goal-27/) was captured while `git status --porcelain` was empty. Each file's line 4 is `DIRTY no`. Each file ends `AFTER clean`. The candidate command ran in a fresh clone of this commit at `/tmp/svn-goal27-src`. The other commands ran in the source checkout of the same commit. Log files were written outside the worktree and copied in afterward, so they do not appear in those stamps. Nothing tracked was left untracked. A gitignored `.venv` was present in the source checkout and is not in those stamps. The clone had no `.venv`; it used that interpreter by absolute path.

## Head CI

[Run 44923623969](https://github.com/Maxencejules/shortvideo-network/actions/runs/44923623969) is `ci` on `ea1a6aeeb4845df0cb29c913f2f39872c3feac55`. Conclusion: `success`. All 17 jobs passed, including `python`, `go`, and `devnet`.

[Run 44923623962](https://github.com/Maxencejules/shortvideo-network/actions/runs/44923623962) is `release-candidate` on the same commit. Conclusion: `success`. Its one job ran `scripts/packaging.py candidate`. That job's binary hashes were not compared with the hashes below.

Earlier runs on this branch were `cancelled` when a later commit was pushed: `44923131924` and `44923131966` on `fc274cb`, `44923179812` and `44923179797` on `29685aa`, `44923420130` and `44923420122` on `e42b042`. Those cancellations are the workflow concurrency setting. They are not failures of this commit.

This file is added after those runs. A later head that only changes `docs/evidence/` is not those runs.

## What ran

| Command | Scope | Log | Result |
| --- | --- | --- | --- |
| `scripts/packaging.py candidate` | clean clone | [candidate.txt](logs/goal-27/candidate.txt) | line 5 is the NOT-RELEASED line, line 6 `EXIT:0` |
| runtime versions | source checkout | [runtime.txt](logs/goal-27/runtime.txt) | line 13 `EXIT:0` |
| pytest of packaging, operations docs, and repo policy | those three files | [pytest.txt](logs/goal-27/pytest.txt) | line 5 is 55 dots and `[100%]`, line 6 `EXIT:0` |
| `ruff check` and `ruff format --check` | `scripts/packaging.py` and `tests/unit/test_packaging.py` | [ruff.txt](logs/goal-27/ruff.txt) | line 5 contains `All checks passed`, line 6 `2 files already formatted`, line 7 `EXIT:0` |
| `mypy` | those two files | [mypy.txt](logs/goal-27/mypy.txt) | line 5 `Success: no issues found in 2 source files`, line 6 `EXIT:0` |
| `scripts/scan_secrets.py` | tracked files | [secrets.txt](logs/goal-27/secrets.txt) | line 5 `secret scan ok`, line 6 `EXIT:0` |
| `scripts/workflow_policy.py` | both workflow files | [workflow.txt](logs/goal-27/workflow.txt) | line 5 `workflow policy ok files 2`, line 6 `EXIT:0` |
| `scripts/write_sbom.py --check` | lockfiles | [sbom.txt](logs/goal-27/sbom.txt) | line 5 `sbom ok components 1389`, line 6 `EXIT:0` |
| requirements checker | requirements | [requirements.txt](logs/goal-27/requirements.txt) | line 64 `result PASS`, line 66 `EXIT:0` |
| `ffmpeg -version` | host | [ffmpeg.txt](logs/goal-27/ffmpeg.txt) | line 5 is the version line, line 16 `EXIT:0` |
| `docker version` | host | [docker.txt](logs/goal-27/docker.txt) | line 5 `docker: command not found`, line 6 `EXIT:127` |
| `golangci-lint version` | host | [golangci.txt](logs/goal-27/golangci.txt) | line 5 `golangci-lint: command not found`, line 6 `EXIT:127` |
| `packaging.py verify` | the candidate bundle | [verify.txt](logs/goal-27/verify.txt) | line 5 `checksums match`, line 6 `EXIT:0` |
| `sha256sum -c` | the candidate bundle | [sha256sum.txt](logs/goal-27/sha256sum.txt) | lines 5 through 24 are `OK`, line 25 `EXIT:0` |
| `svnd version` | the candidate `svnd` | [version.txt](logs/goal-27/version.txt) | line 7 is the revision, line 8 `vcs.modified false`, line 21 `upgrades none`, line 22 `EXIT:0` |
| test-signing directory and prefix | after the candidate | [signing.txt](logs/goal-27/signing.txt) | line 9 `PRIVATE absent`, line 14 `EXIT:0` |
| two build directories | `build-1` and `build-2` | [compare.txt](logs/goal-27/compare.txt) | line 31 `match yes`, line 32 `EXIT:0` |
| filtered `go version -m` | candidate `svnd` | [buildinfo.txt](logs/goal-27/buildinfo.txt) | line 8 `vcs.modified=false`, line 9 `EXIT:0` |
| bundle checksums | binaries and labels | [sums.txt](logs/goal-27/sums.txt) | line 20 `EXIT:0` |

[runtime.txt](logs/goal-27/runtime.txt) line 5 is `v22.23.4`. Line 6 is `10.9.9`. Line 7 is `Python 3.12.3`. Line 8 begins with `go version go1.26.10`. Line 9 begins with `ffmpeg version 6.1.1-3ubuntu5`. Line 10 begins with `Linux 6.12.113+`. Line 11 is `docker-absent`. Line 12 is `golangci-absent`.

[ffmpeg.txt](logs/goal-27/ffmpeg.txt) line 7 is the configuration line. It contains `--enable-gpl` and `--enable-libx264`.

[requirements.txt](logs/goal-27/requirements.txt) line 65 begins with `note document check only`.

## Reproducibility

[compare.txt](logs/goal-27/compare.txt) line 9 and line 22 are the same `svnd` digest, `d6e075d16ea39b55fdc5f285feb568ccef313c3d1ca52f95e357cabf9aa497d2`. `diff` printed nothing between the two `sha256sum` listings. Line 31 is `match yes`. Both builds used `CGO_ENABLED=0`, `-trimpath`, and `-buildvcs=true`, and the command unsets `SOURCE_DATE_EPOCH` (`scripts/packaging.py:392`).

The install token is generated at install time and is not a build output. It is not in the bundle. The apt index used to fetch the pinned FFmpeg package is not a content digest. The package version is `7:6.1.1-3ubuntu5`, and the observed binary is line 5 of [ffmpeg.txt](logs/goal-27/ffmpeg.txt). Module downloads are pinned by `go.sum`. This comparison is two builds on this VM. The GitHub `release-candidate` job exited 0 and its binaries were not hashed here.

[version.txt](logs/goal-27/version.txt) line 7 is `vcs.revision ea1a6aeeb4845df0cb29c913f2f39872c3feac55`. Line 8 is `vcs.modified false`. Line 21 is `upgrades none`. [buildinfo.txt](logs/goal-27/buildinfo.txt) lines 6 through 8 are the same revision, the commit time, and `vcs.modified=false`. That filtered command included a `-tags` pattern and printed no `-tags` line.

## Secret, privilege, and notice scan

[secrets.txt](logs/goal-27/secrets.txt) line 5 is `secret scan ok` for tracked files. The candidate also scans the bundle. ELF binaries contain the PEM header as a parser constant, so that scan does not treat an ELF body as a key file. It still rejects a private-key marker in a non-ELF file, a `.pem` name, setuid, and a world-writable file. The pytest file covers the marker and the world-writable case. [signing.txt](logs/goal-27/signing.txt) line 6 and line 8 are `SVN-TEST-SIGNING-KEY-NOT-FOR-A-RELEASE`. Line 9 is `PRIVATE absent`. The private test key is not in the bundle.

Notices in the bundle passed `sha256sum -c` ([sha256sum.txt](logs/goal-27/sha256sum.txt) includes `THIRD_PARTY_NOTICES.md: OK` and `ffmpeg-obligations.md: OK`). [sbom.txt](logs/goal-27/sbom.txt) line 5 is `sbom ok components 1389`. The project licence file is not applied. `THIRD_PARTY_NOTICES.md` says `NOT-RELEASED` and `UNCONFIRMED`.

Dockerfiles are pinned by digest and end as `USER svn` (`deployments/chain/Dockerfile:14`, `deployments/images/worker.Dockerfile:8`). The worker image does not run `apt-get`. Image build and image run were not executed. Docker is absent ([docker.txt](logs/goal-27/docker.txt) line 6 `EXIT:127`). The daemon on port 2375 was not used.

The release-candidate workflow permission is `contents: read` (`.github/workflows/release-candidate.yml:13`). [workflow.txt](logs/goal-27/workflow.txt) line 5 is `workflow policy ok files 2`.

## Install, uninstall, and upgrade

The documented smoke is `scripts/packaging.py candidate` from a clean checkout. [candidate.txt](logs/goal-27/candidate.txt) line 6 is `EXIT:0`.

Install writes loopback configuration, no default credential, and a mode `0600` development token that is not a consensus key. The pytest gate and the candidate both exercise that path.

Uninstall deletes only paths listed in the install manifest. [signing.txt](logs/goal-27/signing.txt) lines 11 through 13 are the prefix, `operator-data`, and `keep.txt`. The install link is not in that listing.

The candidate switches that link from one directory to another and back before uninstall. That does not read or write chain state. Chain-state rollback was not rehearsed. [failed-upgrade.md](../operations/failed-upgrade.md#when-rollback-is-unsafe) records when an old binary is not a recovery. [backup-and-restore.md](../operations/backup-and-restore.md#compatibility) accepts one backup format. `deployments/profiles/profiles.json:9` is `chain_state_rollback` `not-rehearsed`.

## Artifact checksums

From [sums.txt](logs/goal-27/sums.txt). These are the bundle copies of the first build.

| Line | SHA-256 | Path |
| --- | --- | --- |
| 5 | `df6a1d60100314570681ae703b77269e475116062b6608d30f8e6a45cdc1f337` | `bin/svn-api` |
| 6 | `84cc9d4be0905d670728996372429f40ad9113adac39d1605f8615b0e8b9f270` | `bin/svn-broadcaster` |
| 7 | `202d31928ad2d7404d25b4cb3a6b663dd187911684962176c94ed88b500bb756` | `bin/svn-chainview` |
| 8 | `d6e075d16ea39b55fdc5f285feb568ccef313c3d1ca52f95e357cabf9aa497d2` | `bin/svnd` |
| 9 | `b6ec3f40d28f8b9800ce2358797e9fa580bfd207b767dd6b576baf03806ba988` | `bin/svn-discovery` |
| 10 | `4b3df637869dd59a92d679d18301257661a9cab303ab8fab4edaa1e05f022872` | `bin/svn-feed-client` |
| 11 | `ba5e95659e0c8676eab6ea458e8dbeec15a5279f1d3309f35a2bb138f138f89f` | `bin/svn-indexer` |
| 12 | `53aec4e66cbeaaeff0409ee669ecb391c8ed54d1f35c63dabfffc0a0ca479cd2` | `bin/svn-observe` |
| 13 | `5257aea6531bb63ac56ccd2af6613d96636d583a69f439f072677a454914a7fe` | `bin/svn-operator` |
| 14 | `0de61c71cbf318758a4b37114209297c67cb4ae034bebb9888374ea193f9f3dc` | `bin/svn-provider` |
| 15 | `160b615106a0c8c6d7c4d455e40c313928e2c950abe8d422d125b4f65c41607d` | `bin/svn-restore` |
| 16 | `7d7788349d80d3f5bf144816cb6e0ee63900ba62ea891824e40784998a11e244` | `bin/svn-scheduler` |
| 17 | `212e54a8fa6ab37560b3ff4acfaf9164ddae225938150380fad230df070afada` | `SHA256SUMS` |
| 18 | `a0058174071c0da03a7eabbd70fd738e9d9d116cef80ec8a278f026a67edd7bb` | `NOT-RELEASED` |
| 19 | `41dbad72a4ea64f80520e45e5263baa65c340c8acdec81a767680a7cf99e2457` | `MANIFEST.json` |

The binaries are not committed. They were left under `/tmp/svn-goal27-out/bundle` on this VM.

## Licence

DEC-006's default is recorded and not applied. There is no `LICENSE` file. `deployments/profiles/profiles.json:2` is `NOT-RELEASED`. `scripts/packaging.py:28` is the same label. FFmpeg is the Ubuntu GPL build named above. Codec patent obligations are not settled. The worker image does not install FFmpeg.

## Profiles

`deployments/profiles/profiles.json` is the reference for the dev machine, web/API, accounts, validators, workers, provider agents, storage, and query/index. `single_mandatory_operator` is false. `commercial_cloud` is `not-required`. Every `worker_sees_signer` value is false. The workers profile does not run `svnd`. The validators profile does not run FFmpeg. Replacement paths for DNS, identity, media storage, search, and transaction access are in that file. Ports in the reference are not published. Resource values are `not-measured`.

## Decision requests

| ID | Status | Effect |
| --- | --- | --- |
| DR-27-01 | OPEN, BLOCKED | The project licence file is not applied. DEC-006 still says to confirm compatibility before applying the split. The candidate is not releasable. |
| DR-27-02 | OPEN, BLOCKED | The creator upload licence text is not written. |
| DR-27-03 | OPEN, BLOCKED | FFmpeg codec patent obligations are not settled. The GPL build is recorded. The worker image does not contain FFmpeg. |
| DR-27-04 | OPEN, BLOCKED | Docker is absent. Images were not built or run. The static Dockerfile scan ran in pytest. Port 2375 was not used. |
| DR-27-05 | OPEN | The test signing key is generated in the candidate run, labelled, and deleted. An owner signature was not made. |
| DR-27-06 | OPEN, BLOCKED | Chain-state rollback was not rehearsed. The install-link switch was. Goal 10 and goal 26 do not allow this goal to roll chain state back. |
| DR-27-07 | OPEN, BLOCKED | This VM is one machine. Independent operators, an audit, and interoperability were not demonstrated. |
| DR-27-08 | OPEN, BLOCKED | golangci-lint is absent. |
| DR-27-09 | OPEN, BLOCKED | Profile resource numbers are not measurements. Goal 28 owns that measurement. |
| DR-27-10 | OPEN, BLOCKED | Goals 28-30 and the human launch gates have not passed. |
| DR-27-11 | OPEN, BLOCKED | DR-26-01 through DR-26-12 stay open. This goal does not set retention, custody, or a recovery policy. |
| DR-27-12 | OPEN | The bundle scan does not treat an ELF parser constant as a key. A key embedded in an ELF would not be caught by that check. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody stays unimplemented. The candidate does not take custody.
2. **DEC-002.** No operator is named.
3. **DEC-006.** No licence file.
4. **One machine.** The candidate processes are this VM, plus one GitHub runner for the workflow job. That is not two independent operators.
5. **Goal 06 and goal 07 mutation runners.** Not re-run.
6. **golangci-lint.** Not installed.
7. **Docker.** Not installed. Images were not run.
8. **PostgreSQL 16.** Not used by this goal's smoke. The server on this VM for other goals is 18.6. This smoke did not start it.

## Requirements

The requirements matrix was not edited.

| Row | Status on this commit |
| --- | --- |
| REQ-PROD-002 | VERIFIED as a document check. `single_mandatory_operator` is false and replacement paths are present. Two operators were not observed. |
| REQ-UX-014 | The manifest is `deployments/profiles/profiles.json`. `svn-feed-client` is in the candidate. Goal 02 schemas were already in the tree. This is not an interoperability result. |
| REQ-ARCH-007 | VERIFIED for the binary install on this Linux VM. No commercial cloud account was required. The GitHub job is a second execution of the same command, not a cloud product dependency. |
| REQ-ARCH-008 | VERIFIED as documentation of replacement paths in the profile file. |
| REQ-SEC-012 | `SECURITY.md` is the reporting note. It names no person and no inbox. The dependency pins are unchanged. This is not an audit. |
| REQ-GOV-002 | `docs/operations/governance-notes.md` keeps protocol governance, community governance, and maintainers apart. None is described as owning user content. |
| REQ-GOV-006 | `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, and `SECURITY.md` are present. They name no staff and no dates. The decision log remains `docs/decision-register.md`. |
| REQ-OPS-001 | Profiles name volumes, secrets, ports, health, backups, and upgrades, and keep the media worker off the validator profile. Backup retention stays the goal 26 placeholder. |

## NOT RELEASED

The package is **NOT RELEASED**. Goals 28-30 have not passed. The human launch gates have not passed. Do not tag, publish, or deploy from this goal.

## Next safe action

Do not publish the candidate. Confirm licence compatibility and apply a licence file before any releasable label. Leave chain-state rollback to the goal 10 and goal 26 procedures. Goal 28 measures capacity. Public launch stays disabled.
