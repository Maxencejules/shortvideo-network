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
| Commit | `0d7f14159379918a3eeb9555504354182c03b2a3` |
| Tree | `fc9a0cec8074c073e205a15e6525709984fabef1` |
| Base | `8d93835375f268bc37eab58a001f118cdd3953ff` |
| Branch | `goal-27` |
| Pull request | https://github.com/Maxencejules/shortvideo-network/pull/32 |

The tested commit message carries `Tested-tree: fc9a0cec8074c073e205a15e6525709984fabef1`. `git rev-parse 0d7f14159379918a3eeb9555504354182c03b2a3^{tree}` is that tree. Later commits change only `docs/evidence/`.

## Captures

Every log under [logs/goal-27/](logs/goal-27/) was captured while `git status --porcelain` was empty. Each file's line 4 is `DIRTY no`. Each file ends `AFTER clean`. The candidate command ran in a fresh clone of this commit at `/tmp/svn-goal27-src`. The other commands ran in the source checkout of the same commit. Log files were written outside the worktree and copied in afterward, so they do not appear in those stamps. Nothing tracked was left untracked. A gitignored `.venv` was present in the source checkout and is not in those stamps. The clone had no `.venv`; it used that interpreter by absolute path.

## Head CI

[Run 44926532416](https://github.com/Maxencejules/shortvideo-network/actions/runs/44926532416) is `ci` on `0d7f14159379918a3eeb9555504354182c03b2a3`. Conclusion: `success`. All 17 jobs passed, including `python`, `go`, and `devnet`.

The `release-candidate` workflow did not run on this commit. Its trigger is `workflow_dispatch` only (`.github/workflows/release-candidate.yml:10`). Nobody dispatched it. The candidate result below is the local clone.

Earlier runs on this branch used the previous trigger and are not this commit: `44924774711` and `44924774745` on `5e546f9` succeeded, and `44923623969` and `44923623962` on `ea1a6ae` succeeded. Runs `44923131924`, `44923131966`, `44923179812`, `44923179797`, `44923420130`, and `44923420122` were `cancelled` when a later commit was pushed. Those cancellations are the workflow concurrency setting. They are not failures of this commit.

This file is added after the `ci` run above. A later head that only changes `docs/evidence/` is not that run.

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

[compare.txt](logs/goal-27/compare.txt) line 9 and line 22 are the same `svnd` digest, `9eacf60d7a2c43a7ffa72a880f430c3cb67a6d4536cc25c1b977d127203ff4e5`. `diff` printed nothing between the two `sha256sum` listings. Line 31 is `match yes`. Both builds set `CGO_ENABLED=0` (`scripts/packaging.py:392`) and `GOFLAGS` to `-trimpath` (`scripts/packaging.py:393`), pass `-trimpath` and `-buildvcs=true` on the `go build` argv (`scripts/packaging.py:402` and `scripts/packaging.py:403`), and unset `SOURCE_DATE_EPOCH` (`scripts/packaging.py:394`).

The install token is generated at install time and is not a build output. It is not in the bundle. The apt index used to fetch the pinned FFmpeg package is not a content digest. The package version is `7:6.1.1-3ubuntu5`, and the observed binary is line 5 of [ffmpeg.txt](logs/goal-27/ffmpeg.txt). Module downloads are pinned by `go.sum`. This comparison is two builds on this VM. The GitHub release-candidate workflow was not dispatched for this commit, so it did not produce a second pair of hashes.

[version.txt](logs/goal-27/version.txt) line 7 is `vcs.revision 0d7f14159379918a3eeb9555504354182c03b2a3`. Line 8 is `vcs.modified false`. Line 21 is `upgrades none`. [buildinfo.txt](logs/goal-27/buildinfo.txt) lines 6 through 8 are the same revision, the commit time, and `vcs.modified=false`. That filtered command included a `-tags` pattern and printed no `-tags` line.

## Secret, privilege, and notice scan

[secrets.txt](logs/goal-27/secrets.txt) line 5 is `secret scan ok` for tracked files. The candidate also scans the bundle. ELF binaries contain the PEM header as a parser constant, so that scan does not treat an ELF body as a key file. It still rejects a private-key marker in a non-ELF file, a `.pem` name, setuid, and a world-writable file. The pytest file covers the marker and the world-writable case. [signing.txt](logs/goal-27/signing.txt) line 6 and line 8 are `SVN-TEST-SIGNING-KEY-NOT-FOR-A-RELEASE`. Line 9 is `PRIVATE absent`. The private test key is not in the bundle.

Notices in the bundle passed `sha256sum -c` ([sha256sum.txt](logs/goal-27/sha256sum.txt) includes `THIRD_PARTY_NOTICES.md: OK` and `ffmpeg-obligations.md: OK`). [sbom.txt](logs/goal-27/sbom.txt) line 5 is `sbom ok components 1389`. The project licence file is not applied. `THIRD_PARTY_NOTICES.md` says `NOT-RELEASED` and `UNCONFIRMED`.

Dockerfiles are pinned by digest and end as `USER svn` (`deployments/chain/Dockerfile:14`, `deployments/images/worker.Dockerfile:8`). The worker image does not run `apt-get`. Image build and image run were not executed. Docker is absent ([docker.txt](logs/goal-27/docker.txt) line 6 `EXIT:127`). The daemon on port 2375 was not used.

The release-candidate workflow trigger is `workflow_dispatch` (`.github/workflows/release-candidate.yml:11`, under the `on` key at `.github/workflows/release-candidate.yml:10`). Its permission line is `contents: read` (`.github/workflows/release-candidate.yml:14`). A person who can dispatch workflows must start it. It does not run on a pull request or on a push. The pull-request tests run `scripts/packaging.py` `check`, which does not build the candidate binaries. [workflow.txt](logs/goal-27/workflow.txt) line 5 is `workflow policy ok files 2`.

That dispatch is not a second-reviewer gate. `scripts/workflow_policy.py:92` reads a job `environment`. `scripts/workflow_policy.py:93` rejects a set value, and `scripts/workflow_policy.py:94` records that error. This tree does not name an environment, and the policy was not weakened to allow one. A protected GitHub environment with required reviewers is a repository setting. The owner would still need to create that environment and then accept a reviewed policy change before this workflow could wait on it. That gap is DR-27-13.

## Install, uninstall, and upgrade

The documented smoke is `scripts/packaging.py candidate` from a clean checkout. [candidate.txt](logs/goal-27/candidate.txt) line 6 is `EXIT:0`.

Install writes loopback configuration, no default credential, and a mode `0600` development token that is not a consensus key. The pytest gate and the candidate both exercise that path.

Uninstall deletes only paths listed in the install manifest. [signing.txt](logs/goal-27/signing.txt) lines 11 through 13 are the prefix, `operator-data`, and `keep.txt`. The install link is not in that listing.

The candidate switches that link from one directory to another and back before uninstall. That does not read or write chain state. Chain-state rollback was not rehearsed. [failed-upgrade.md](../operations/failed-upgrade.md#when-rollback-is-unsafe) records when an old binary is not a recovery. [backup-and-restore.md](../operations/backup-and-restore.md#compatibility) accepts one backup format. `deployments/profiles/profiles.json:9` is `chain_state_rollback` `not-rehearsed`.

## Artifact checksums

From [sums.txt](logs/goal-27/sums.txt). These are the bundle copies of the first build.

| Line | SHA-256 | Path |
| --- | --- | --- |
| 5 | `fe48e742c631d1a4cff55fb0d53e4c4aee4e77eef378b29d483851f7670c3739` | `bin/svn-api` |
| 6 | `cfd01bb27d2ed4515bb3a1156026db9841b217fefa424ad6ec8428bbed3492ac` | `bin/svn-broadcaster` |
| 7 | `f3bfe96c890448be7e05b8c82200ca22d0268e71ef013d3acb4dd3ebcba49f88` | `bin/svn-chainview` |
| 8 | `9eacf60d7a2c43a7ffa72a880f430c3cb67a6d4536cc25c1b977d127203ff4e5` | `bin/svnd` |
| 9 | `5ea9f8668689ab95c5bdd7177f4ac7789133b33dab6feea73487174053cff9e6` | `bin/svn-discovery` |
| 10 | `d1300061b6a6aeb7afa68b08c050ba9a1fd26ae851d6b6f60eb0a99f2a367d3c` | `bin/svn-feed-client` |
| 11 | `de55fc4d2adbd3e08adb51b1f7f863c261ad69cfd5b391d6148354940733d6e3` | `bin/svn-indexer` |
| 12 | `ffdf4e8ec2392d942f4a53c41041e8c669abe8c2e1e824bf4419f2da0443c711` | `bin/svn-observe` |
| 13 | `78bead40413b8ab31b0b07181fbbe31ffbc29f33ceac9e641106cf536075f349` | `bin/svn-operator` |
| 14 | `b0e1c0318278923de0e768b39a3b31306648072a84cf9b3ac5df7edec60060f6` | `bin/svn-provider` |
| 15 | `d6472b1bd0f502d599c926543ffbca4cb1802cba67103faaf2964c9e2885ef3b` | `bin/svn-restore` |
| 16 | `be9c578f5aac3c3d951774d9285e5a3a49f2dc602d28b5a209749de09da5c6d5` | `bin/svn-scheduler` |
| 17 | `dc39acacbe0501cbdc7304196949600e88c635851c2af47456a543b093b8c434` | `SHA256SUMS` |
| 18 | `a0058174071c0da03a7eabbd70fd738e9d9d116cef80ec8a278f026a67edd7bb` | `NOT-RELEASED` |
| 19 | `50970f10591f10455b2924f901c8ca02d0aba70a740353b4beea8c948caa7a3d` | `MANIFEST.json` |

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
| DR-27-13 | OPEN, BLOCKED | The release-candidate build starts only when someone dispatches the workflow. That is not a second reviewer. A protected GitHub environment with required reviewers needs a repository setting this run cannot change, and `scripts/workflow_policy.py:92` through `scripts/workflow_policy.py:94` reject a job environment, so the workflow does not name one. |

## Inherited blockers

BLOCKED, carried forward. A missing tool is not a pass.

1. **DEC-001 and DR-07-01.** Custody stays unimplemented. The candidate does not take custody.
2. **DEC-002.** No operator is named.
3. **DEC-006.** No licence file.
4. **One machine.** The candidate processes are this VM. Pull-request CI runs the packaging check and does not build these binaries. That is not two independent operators.
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
| REQ-ARCH-007 | VERIFIED for the binary install on this Linux VM. No commercial cloud account was required. The GitHub `ci` job runs the packaging check and does not build the candidate. |
| REQ-ARCH-008 | VERIFIED as documentation of replacement paths in the profile file. |
| REQ-SEC-012 | `SECURITY.md` is the reporting note. It names no person and no inbox. The dependency pins are unchanged. This is not an audit. |
| REQ-GOV-002 | `docs/operations/governance-notes.md` keeps protocol governance, community governance, and maintainers apart. None is described as owning user content. |
| REQ-GOV-006 | `CONTRIBUTING.md`, `CODE_OF_CONDUCT.md`, and `SECURITY.md` are present. They name no staff and no dates. The decision log remains `docs/decision-register.md`. |
| REQ-OPS-001 | Profiles name volumes, secrets, ports, health, backups, and upgrades, and keep the media worker off the validator profile. Backup retention stays the goal 26 placeholder. |

## NOT RELEASED

The package is **NOT RELEASED**. Goals 28-30 have not passed. The human launch gates have not passed. Do not tag, publish, or deploy from this goal.

## Next safe action

Do not publish the candidate. Confirm licence compatibility and apply a licence file before any releasable label. Leave chain-state rollback to the goal 10 and goal 26 procedures. Goal 28 measures capacity. Public launch stays disabled.
