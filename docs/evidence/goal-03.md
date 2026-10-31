# Goal 03 evidence

Status: BLOCKED

The skeleton, pins, generation, and local checks below are in this branch. Status is not VERIFIED. Two required conditions are unmet.

Hosted GitHub Actions has not been observed for this revision. `docs/progress.md` classifies those jobs as a cloud command. A local run of the same commands is not that observation.

The playbook assigns GPT-6.1 Sol at Medium effort. This run did not use that model and Medium was not selectable. The runtime report is below. AGENTS.md says a goal is not VERIFIED when the assigned effort was not selected.

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a chain binary.

## Model and effort

- Playbook assignment for goal 03: GPT-6.1 Sol, effort MEDIUM.
- Model actually used: `grok-4.7-high-fast`.
- Source: cursor-cloud `run-info` field `originalModelName` for `bcId=bc-6501fafb-4a7d-5757-8d95-5dfd4967203e`, run URL `https://cursor.com/agents/bc-6501fafb-4a7d-5757-8d95-5dfd4967203e`. That payload has no separate reasoning-effort field. `high-fast` is the suffix of the reported model name.
- GPT-6.1 Sol was not used. Medium was not used.
- Extra effort does not certify correctness.

## Base and tested revision

Base commit: `2ef8573c59645646cd13f0a49c3392095a7ad4a2` (`goal 02: protocol schemas and invariants (#5)` on `origin/main`).

Branch: `goal-03`.

This evidence file is part of the commit that contains it. The tested tree is that commit's tree. Confirm with `git rev-parse HEAD` and `git rev-parse 'HEAD^{tree}'`. The checkers do not read this file. They do read `docs/progress.md` and `docs/progress.tsv`.

## Runtime

Linux 6.12.94+ x86_64.

| Tool | Observed |
| --- | --- |
| Go | `go version go1.26.10 linux/amd64` from the verified tarball |
| Python | `Python 3.12.3` |
| Node | `v22.23.4` |
| npm | `10.9.9` |
| protoc | `libprotoc 36.2` |
| protoc-gen-go | `protoc-gen-go v1.36.12` |
| FastAPI | `0.162.0` |
| cryptography | `50.0.2` |
| pytest | `9.2.0` |

Go 1.26.10 sha256 `ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5` matched `sha256sum -c` against the go.dev JSON feed. protoc 36.2 sha256 `121f6c7afe1d4d0e3ea6aab9432038599250134cbf4474cb1167d2c7decd4278` matched the GitHub release asset digest. Action commit SHAs were read with `gh api repos/<action>/commits/<tag>` and each commit verification was `verified: true`. They are listed in `docs/dependencies.md`. No SHA was invented.

Cosmos SDK v0.55.0 and CometBFT v0.40.0 were read from GitHub tags and from https://docs.cosmos.network/sdk/latest/release-family (2026.1 family: SDK 0.55.x, CometBFT 0.40.x). SDK `go.mod` at tag v0.55.0 says `go 1.26.5` and requires `github.com/cometbft/cometbft v0.40.0`. CometBFT v1.x is not a tag we selected. Enterprise PoA and Enterprise Groups are not requirements in `chain/go.mod`.

## What is scaffolding and what ran

| Path | State |
| --- | --- |
| `apps/web` | React 19.3.1 and TypeScript 7.0.2 pin. One module exports a scaffold flag. No UI. |
| `services/api`, `services/indexer`, `services/broadcaster` | Importable packages whose entry points raise. FastAPI is installed and version-checked. No HTTP server. |
| `workers/media` | `transcode` raises and does not spawn a process. FFmpeg is not pinned. |
| `chain/app` | No `main` package and no SDK import. |
| `chain/tools` | `tools` build tag imports SDK v0.55.0 and CometBFT v0.40.0 so `go.sum` locks them. Not a binary. |
| `chain/x/*` | README slots only. |
| `proto/svn`, `proto/gen/python` | Generated from frozen `docs/protocol/proto`. 28 files each. Handwritten tests and `__init__.py` markers are separate. |
| `agents/provider` | Scaffold constant and one test. No socket. |
| `packages/protocol` | Loads frozen JSON schemas. Does not redefine them. |
| `deployments` | READMEs. No compose file and no image. |
| `tests/e2e`, `tests/chaos` | READMEs. Not a passing suite. |
| `make devnet-up`, `devnet-down`, `smoke`, `fault`, `release-check` | Exit non-zero on purpose. |

Protocol files under `docs/protocol/` were not edited. `handler_down_errors` in `docs/evidence/goal-02/check_protocol.py` is now an AST call-graph scan. It catches `state["down"]`, `state.get("down")`, a name assigned `"down"`, and a static call to a helper that reads `down` or calls `environment_withholds_block` / `liveness_would_halt`. A comment or a string literal is not a read. Dynamic `HANDLERS.get` dispatch is still goal 05.

## Requirement notes

| ID | Result in this goal |
| --- | --- |
| REQ-REPO-001 | Layout directories exist. Generated stubs are under `proto/`; handwritten code is elsewhere. |
| REQ-REPO-003 | `Makefile` targets exist. Unimplemented ones exit 2. |
| REQ-TECH-001 | React 19.3.1 and TypeScript 7.0.2. |
| REQ-TECH-002 | FastAPI 0.162.0. |
| REQ-TECH-003 | PostgreSQL 18.6 recorded from https://www.postgresql.org/support/versioning/. Not installed. |
| REQ-TECH-010 | Lockfiles and `docs/sbom.cdx.json` (321 components after setuptools was added). No image, so no image digest. |
| REQ-TECH-012 | SDK v0.55.0 / CometBFT v0.40.0. Not CometBFT v1.x. |
| REQ-TECH-014 | Loops, PeerTube, and Livepeer are not dependencies. |
| REQ-SEC-008 | Pinned locks, `scripts/scan_secrets.py`, workflow permissions `contents: read`, checkout `persist-credentials: false`, no `secrets.` context. |
| REQ-RISK-007 | Direct licences are in `docs/dependencies.md`. No `LICENSE` file. DEC-006 says this goal must not treat the split as applied. |

## Commands and output

Checker summaries below are from the run after `docs/progress.md` and `docs/progress.tsv` named this goal BLOCKED. Exit codes are the process status. Hash lines are omitted here; both checkers print them.

```
COMMAND python3 docs/evidence/goal-01/check_requirements.py
self_test_problems 0
errors 0
result PASS
EXIT:0

COMMAND python3 docs/evidence/goal-02/check_protocol.py
protoc exit:0
transition_cases 97
transition_case_pin 97
transition_failures 0
go_vectors exit:0
typescript_vectors exit:0
valid_vectors 15
invalid_vectors 14
integer_vectors 6
arithmetic_vectors 5
errors 0
languages MATCH
result PASS
EXIT:0

COMMAND .venv/bin/python scripts/run_tests.py --only python
24 passed in 0.55s
EXIT:0

COMMAND .venv/bin/python scripts/run_tests.py --only node
# tests 1
# pass 1
# fail 0
NODE_OK
EXIT:0

COMMAND .venv/bin/python scripts/run_tests.py --only go
ok github.com/Maxencejules/shortvideo-network/chain/app
ok github.com/Maxencejules/shortvideo-network/protocol/check
ok github.com/Maxencejules/shortvideo-network/agents/provider
EXIT:0

COMMAND .venv/bin/ruff check services workers/media packages/protocol tests scripts
All checks passed
EXIT:0

COMMAND .venv/bin/ruff format --check services workers/media packages/protocol tests scripts
27 files already formatted
EXIT:0

COMMAND .venv/bin/mypy
Success: no issues found in 19 source files
EXIT:0

COMMAND .venv/bin/python scripts/workflow_policy.py
workflow policy ok files 1
EXIT:0

COMMAND .venv/bin/python scripts/scan_secrets.py
secret scan ok
EXIT:0

COMMAND .venv/bin/python scripts/write_sbom.py --check
sbom ok components 321
EXIT:0

COMMAND bash scripts/gen_proto.sh
generated 28 proto files
EXIT:0
```

`protoc --descriptor_set_out=/dev/null google/protobuf/descriptor.proto` exited 0 (well-known types are visible to the pinned protoc).

### Deliberate failure, then removal

`tests/unit/test_deliberate_fail.py` raised `AssertionError: deliberate failing fixture`. The unit command exited 1:

```
FAILED tests/unit/test_deliberate_fail.py::test_deliberate_failure
1 failed, 24 passed in 0.54s
python tests failed: 1
EXIT:1
```

The file was deleted. The same command then exited 0 with `24 passed in 0.55s`.

### Empty suite

`python -m pytest tests/e2e -o addopts=` collected 0 items, printed `no tests ran`, and exited 5. `scripts/run_tests.py` treats a Python run without a non-zero pass count as failure. `go test` packages that do not print `ok` fail that script. `tests/e2e` and `tests/chaos` are not passed results.

### Ignore rules

`git check-ignore` matched `.env`, `uploads/a.mp4`, `node_modules/x`, and `.venv/x`. `.env.example` stayed unignored via `!.env.example`. The unit test `test_ignore_rules_cover_private_material` passed in the 24.

### Workflow inspection

`.github/workflows/ci.yml` is the only workflow. `scripts/workflow_policy.py` checks it: top-level `permissions: contents: read`, no job widens that, `timeout-minutes` is 15 or 20, actions are the four verified SHAs, checkout sets `persist-credentials: false`, setup-go and setup-node set `cache: false`, no `pull_request_target`, no `secrets.` context, no credential `with` input, no environment, no merge step. The workflow does not deploy.

Hosted run [41203392314](https://github.com/Maxencejules/shortvideo-network/actions/runs/41203392314) on pull request 6 was observed and failed. That is not a pass. The three failures were:

- `actions/setup-node` rejected `cache: false` with `Caching for 'false' is not supported`. That input was removed. `actions/setup-go` accepted `cache: false` and is still set that way.
- `pip install --require-hashes` refused `setuptools` because `pip-compile` omits it unless `--allow-unsafe`. `setuptools==84.2.1` is now in `requirements.lock`.
- `gofmt` from Go 1.26.10 wanted to reformat `docs/evidence/goal-02/go/vectorcheck.go`. The format check no longer includes that file. The Goal 02 source was not reformatted.

The hosted run of the commit that contains these fixes was not observed when this paragraph was written.

## Blockers and handoff to goal 04

- Hosted CI was not observed. Goal 04 should not treat this file as a green Actions run.
- PostgreSQL 18.6 is selected and not installed. No image digest exists.
- RabbitMQ, Celery, and FFmpeg are not pinned. Goal 14 owns FFmpeg and the queue implementation. Goal 04 needs them only if the full profile starts those processes; record the pins there rather than inventing them here.
- No `LICENSE` file. DEC-006's block text says this goal must not treat the project licence split as applied.
- AST scan does not follow `HANDLERS.get`. Goal 05 keeps harness operations out of the transaction router and rechecks the liveness rule on the Go port.
- The playbook model and effort were not the ones that ran. See the model section.

## Not claimed

No devnet, no chain binary, no production deployment, no secret scan of a third-party binary beyond the patterns in `scripts/scan_secrets.py`, and no remote CI result.
