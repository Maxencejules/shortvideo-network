# Goal 03 evidence

Status: IMPLEMENTED

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a chain binary.

## Model and effort

- Playbook assignment for goal 03: GPT-6.1 Sol, effort MEDIUM.
- Model that ran: `grok-4.7-high-fast`. Source: cursor-cloud `run-info` field `originalModelName` for `bcId=bc-6501fafb-4a7d-5757-8d95-5dfd4967203e`. That payload has no separate reasoning-effort field. `high-fast` is the suffix of the reported model name.
- GPT-6.1 Sol was not used. The playbook's Medium setting was not selected by the runner.
- On 2026-10-31 at 09:37 EDT Max ruled, relayed word for word by shortvideo-reviewer: "goal 03: accept the grok-4.7-high-fast substitution as reported effort". That ruling applies to goal 03 only. It does not change the reported model for any other goal.
- Status stays IMPLEMENTED. Extra effort does not certify correctness.

## Authorship

On 2026-10-31 at 09:50 EDT Max ruled, relayed word for word by shortvideo-reviewer: "authorship: rewrite goal-03 branch commits as me, force-push ok".

The branch history since `2ef8573c59645646cd13f0a49c3392095a7ad4a2` was rewritten for authorship. Every commit on `goal-03` is authored and committed as `Maxence Jules <Maxencejules@users.noreply.github.com>`. `Co-authored-by` and Cursor trailers were removed. `Tested-tree` trailers were kept.

The rewrite through `887905e176334acaa10e4239750237fdfd0c9ebc` does not change the file content of the earlier commits. Relative to `3a820b55f87d6ef66f7b3efbb3ab283544d011e6`, that note commit changes only `docs/evidence/goal-03.md`. Commits after `887905e` change more than the note. `82bb067214883d5970a02715a0c8ff98c357bbad` changes `scripts/workflow_policy.py`, `scripts/run_tests.py`, `scripts/scan_secrets.py`, `tests/unit/test_workflow_policy.py`, `tests/unit/test_node_suite.py`, `tests/unit/test_secrets.py`, `docs/evidence/goal-02.md`, and `docs/evidence/goal-03.md`. `337f7c2c8b2b6361c63fa47bafcd5699a2b03363` changes `scripts/workflow_policy.py`, `scripts/scan_secrets.py`, `tests/unit/test_workflow_policy.py`, `tests/unit/test_secrets.py`, and `docs/evidence/goal-03.md`. The commit that contains this paragraph changes `scripts/scan_secrets.py`, `tests/unit/test_secrets.py`, and this evidence file.

Before the rewrite, these commits were authored and committed by `Cursor Agent <cursoragent@cursor.com>` and carried a `Co-authored-by` trailer:

- `cf75bc8942046e59db51a1a43f0601241840d83f`
- `b877420d372a59c3a4f77e6fb20b2943943e1998`
- `7594ed19f9e8ff3a5d4f2d5b8437d59f889e806b`
- `15e2b48ca526726536fc144536356bcdf12e14e2`

## Base and tested revision

Base commit: `2ef8573c59645646cd13f0a49c3392095a7ad4a2` (`goal 02: protocol schemas and invariants (#5)` on `origin/main`).

Branch: `goal-03`.

`887905e176334acaa10e4239750237fdfd0c9ebc` records the authorship rewrite and has no `Tested-tree` trailer. This evidence file is part of the later commit that contains it. The checkers do not read this file. They do read `docs/progress.md` and `docs/progress.tsv`. The tested tree is this commit's tree. The commit message carries `Tested-tree` set to that tree, which is `git rev-parse 'HEAD^{tree}'`.

## Runtime

Linux 6.12.94+ x86_64.

| Tool | Observed |
| --- | --- |
| Go | `go version go1.26.10 linux/amd64` |
| Python | `Python 3.12.3` |
| Node | `v22.23.4` |
| npm | `10.9.9` |
| protoc | `libprotoc 36.2` |
| protoc-gen-go | `protoc-gen-go v1.36.12` |
| FastAPI | `0.162.0` |
| cryptography | `50.0.2` |
| pytest | `9.2.0` |

Go 1.26.10 sha256 `ccfe7ae822d2c2f41f4fe7c956199967a4dac22d0ee0d2b2bfdb440aab3b20c5` matched the go.dev JSON feed. protoc 36.2 sha256 `121f6c7afe1d4d0e3ea6aab9432038599250134cbf4474cb1167d2c7decd4278` matched the GitHub release asset digest. Action commit SHAs were read with `gh api repos/<action>/commits/<tag>` and each commit verification was `verified: true`. `actions/checkout` is v5.1.0 (`fbc6f3992d24b796d5a048ff273f7fcc4a7b6c09`), whose `action.yml` says `using: node24`. No SHA was invented.

Cosmos SDK v0.55.0 and CometBFT v0.40.0 are the 2026.1 pair from https://docs.cosmos.network/sdk/latest/release-family. SDK `go.mod` at tag v0.55.0 requires `github.com/cometbft/cometbft v0.40.0`. CometBFT v1.x is not selected. Enterprise PoA and Enterprise Groups are not requirements.

## What is scaffolding and what ran

| Path | State |
| --- | --- |
| `apps/web` | React 19.3.1 and TypeScript 7.0.2 pin. One real test. No UI. |
| `services/api`, `services/indexer`, `services/broadcaster` | Entry points raise. FastAPI is version-checked. No HTTP server. |
| `workers/media` | `transcode` raises. FFmpeg is not pinned. |
| `chain/app` | No `main` package and no SDK import. |
| `chain/tools` | `tools` build tag locks SDK v0.55.0 and CometBFT v0.40.0. Not a binary. |
| `chain/x/*` | README slots only. No Go files yet. |
| `proto/svn`, `proto/gen/python` | Generated from frozen `docs/protocol/proto`. 28 files each. |
| `agents/provider` | Scaffold constant and one test. No socket. |
| `packages/protocol` | Loads frozen JSON schemas. |
| `deployments` | READMEs. No compose file and no image. |
| `tests/e2e`, `tests/chaos` | READMEs. Not a passing suite. |
| `make devnet-up`, `devnet-down`, `smoke`, `fault`, `release-check` | Exit non-zero on purpose. |

Protocol files under `docs/protocol/` were not edited. `handler_down_errors` is an AST call-graph scan. It catches a module constant key, `setdefault`, `dict.get`, `state.items()`, a module alias chain, an f-string key, and `"down" in state`, plus the earlier subscript, `.get`, and helper forms. A comment or a string literal is not a read. `state["offers"].items()` is not a read of `down`. Dynamic `HANDLERS.get` dispatch stays goal 05. The liveness scan is best effort. It misses dynamic keys (getattr, itemgetter, join, values, **state). That gap is deferred to goal 05, which writes the chain handlers. DEC-011 also binds goal 06: neither goal may put `down` in the application hash.

## Requirement notes

| ID | Result in this goal |
| --- | --- |
| REQ-REPO-001 | Layout directories exist. Generated stubs are under `proto/`. |
| REQ-REPO-003 | `Makefile` targets exist. Unimplemented ones exit 2. |
| REQ-TECH-001 | React 19.3.1 and TypeScript 7.0.2. |
| REQ-TECH-002 | FastAPI 0.162.0. |
| REQ-TECH-003 | PostgreSQL 18.6 recorded from https://www.postgresql.org/support/versioning/. Not installed. |
| REQ-TECH-010 | Lockfiles and `docs/sbom.cdx.json` (321 components). No image, so no image digest. |
| REQ-TECH-012 | SDK v0.55.0 / CometBFT v0.40.0. Not CometBFT v1.x. |
| REQ-TECH-014 | Loops, PeerTube, and Livepeer are not dependencies. |
| REQ-SEC-008 | Pinned locks, secret scan, workflow permissions `contents: read`, checkout `persist-credentials: false`. |
| REQ-RISK-007 | Direct licences are in `docs/dependencies.md`. No `LICENSE` file. DEC-006 says this goal must not treat the split as applied. |

## Commands and output

These logs are from the tree that this commit records. Exit codes are the process status. The two checker logs are verbatim.

```
COMMAND .venv/bin/python docs/evidence/goal-01/check_requirements.py
self_test duplicate id fail_as_expected
self_test missing role fail_as_expected
self_test bad goal fail_as_expected
self_test deferred with a goal fail_as_expected
self_test missing test or blocker fail_as_expected
self_test missing blueprint page fail_as_expected
self_test scenario list fail_as_expected
self_test threat heading exact fail_as_expected
self_test progress mismatch fail_as_expected
self_test row goals fail_as_expected
self_test extra column goal fail_as_expected
self_test negation still names a goal fail_as_expected
requirements 265
in_scope 250
with_test 216
with_blocker 34
deferred 9
non_goal 6
decisions 8
threats 15
scenarios 5
contradictions 13
self_test_cases 12
self_test_problems 0
page_coverage ok
errors 0
scenario S-PLAYBACK recorded
scenario S-ORIGIN recorded
scenario S-DEPART recorded
scenario S-STALL recorded
scenario S-REMOVE recorded
contradiction CR-001 recorded
contradiction CR-002 recorded
contradiction CR-003 recorded
contradiction CR-004 recorded
contradiction CR-005 recorded
contradiction CR-006 recorded
contradiction CR-007 recorded
contradiction CR-008 recorded
contradiction CR-009 recorded
contradiction CR-010 recorded
contradiction CR-011 recorded
contradiction CR-012 recorded
contradiction CR-013 recorded
threat TM-UPLOAD recorded
threat TM-UNAUTH-TX recorded
threat TM-KEYS recorded
threat TM-DISHONEST recorded
threat TM-REPLICAS recorded
threat TM-STALL recorded
threat TM-STALE-GW recorded
threat TM-ABUSE recorded
threat TM-SUPPLY recorded
threat TM-VALIDATOR recorded
threat TM-STAKE-BYPASS recorded
threat TM-PERSONAL recorded
threat TM-INJECTION recorded
threat TM-MINT recorded
threat TM-SECRET recorded
result PASS
note document check only; protocol not approved; public launch disabled
EXIT:0

COMMAND .venv/bin/python docs/evidence/goal-02/check_protocol.py
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
hashes
d73e9bf9603674d29a3443a869d8927cfb3a107f46367c1a743141eb7893e920  docs/decision-register.md
1e31c64f0e971db9a77450fb0d84f6804774b42f5fb1e794f8399149e7e19792  docs/evidence/goal-01/check_requirements.py
8b847bac1a3236f5eec7aa69ff09b8080c7c135b4dfab00ab9030a49ee852d09  docs/evidence/goal-02/cases.py
2625e9b9e89ad44133f44b88985cf84550d0e5b886cb8d93c3d6c2149d39cb8f  docs/evidence/goal-02/check_protocol.py
e0b344008a1ebdec3fba9b1cb22a74d4f251c23cac23d5092c67fda5387bd6ef  docs/evidence/goal-02/go/go.mod
4cdc4a3b5c2dcdcf70196c176aa3d35054ff3333789c781306e5cdf2051dd080  docs/evidence/goal-02/go/vectorcheck.go
44c72f0c839896bd9b559f33851a512e0a8db9cbc087df2a745aefd909a765b0  docs/evidence/goal-02/jcs.py
7caaa3bf47979383afac9c8468fa65cc63ee2ebe2fe67b8e8bec218bd3a0d3a7  docs/evidence/goal-02/negative/email.proto
6afb552fd968083003159a79cf12846a3480e34063ddda78ad2730ce75fce46c  docs/evidence/goal-02/negative/ip_address.proto
d5938cbd382771a2bb0384b74ddb6425251b8ad97331df01cb1f6e264a599f77  docs/evidence/goal-02/negative/leaks.proto
ed959570306e4cadeb744a37299add6d64351aab476fd88a550039a795bac1dd  docs/evidence/goal-02/negative/media.proto
0a1da67c3881ae73b1924a5120cd088eefce711c2a79354bbc8cd5a7f7e6851e  docs/evidence/goal-02/negative/mint.proto
961101c3d3ab2aca71dc00ffc4ad980366a225d4970a87f934e1fdbfc6818500  docs/evidence/goal-02/negative/mint_tokens.proto
417cd0f5551289bbc20a27a5ddac1313e53e3bb2670ebfd36722e5e404165b5d  docs/evidence/goal-02/negative/penalize.proto
6e1e1e70b0a6cf3952eb4ae0ecabd0f2b4ffae20971380b2cf9ccd0bb6795c4a  docs/evidence/goal-02/negative/reward.proto
e1b1e71b30fc6a4098e3152f25a6830cfa4a52aa7ec5f3ea16ec989f6adcf2fc  docs/evidence/goal-02/negative/slash.proto
85da4ddcb323c76695d62da03477d93a562f458ec4024c8a397c2cc2e702655f  docs/evidence/goal-02/negative/storage_proof.proto
502e774d6bd2ba90752f8ca39d8fa10259b595f97e7978f91a1e01a193858309  docs/evidence/goal-02/negative/timestamp.proto
92c9bcb8172148f4ea65802def2a54c69d280e2c4a8b21a66a6472910f83c98c  docs/evidence/goal-02/negative/token_balance.proto
6d2816506bad14e04b00348f0a395f39968832450bca1253492792e5ed788b4e  docs/evidence/goal-02/negative/video_payload.proto
b2a9cbf2b84c0d2d4228e538f13a733e53741fa8bb09e99fb1aef53669cfc1df  docs/evidence/goal-02/spec_machine.py
8cbf1ddd451d6738dc34d06133151415295626ee70b8bdd9b69cfda056f8d8b9  docs/evidence/goal-02/ts/vectorcheck.ts
1844a72a606807d4b4f57519753716b0418bf35fbbd06a3dd7f6933ec0258d0d  docs/progress.md
c3412a594a99999cc52b73f4f02f15371515a0452e483ba1f4a7a23175b0408e  docs/progress.tsv
d52993f4fd2368c2bf25a9e5131b7dd4b4b2622f9c62cc6e090a5d925e34e25f  docs/protocol/api/client-api.json
94a2f35aaf83f234a3fb84e344cf562d6c3c391e0467ff77c7a5590f16324c9f  docs/protocol/api/provider-api.json
15fc8fa73ef39a5b867e74e6ca1d3f41d9e1eb82a3364903ad40d2849648c81b  docs/protocol/bounds.json
eec164c9beb92a879de4b885eeceed4069c7991896097a1864c0b437026996bc  docs/protocol/fixtures/genesis-valid.json
de0735b82b34a84e7d95d8e8bc720f346d1a705582ab6fc0a222324f0794539d  docs/protocol/fixtures/manifest-invalid-boundary.json
f4b0a345b494f00b4a9bab599be25d80d9ff31bf28b017979f28c91beafb512b  docs/protocol/fixtures/manifest-invalid-duplicate-id.json
be1b53f8674503c5086af4a23468cd4fb205cf296faf7845c4d383c3a3a9f2a9  docs/protocol/fixtures/manifest-invalid-unknown.json
c6e8246ddb86788efa30f6e36eb887bc2e286fd7e2226baa5522a13790bef238  docs/protocol/fixtures/manifest-invalid-wrong-domain.json
703a4abce900576fbd08f21fd3d1a611fb49eb01fc7882d694ddd8106f4477f1  docs/protocol/fixtures/manifest-valid.json
8729de9714a97462e151a2fd50ead9f5074536117581219036bd08cdd0724477  docs/protocol/fixtures/vectors.json
48b8b96b539a245412fa4e007397ab0c7bc0bf7374e8bc5f97474528662b93a0  docs/protocol/proto/svn/app/v1/genesis.proto
9bbe1a6314a911288473641861459722a001a707b999f1df95da27407a5aed5d  docs/protocol/proto/svn/authority/v1/genesis.proto
9aa5f64ed65b6f8179a9222ead115cdedd768132fed1ab59087c94b53ae0f68a  docs/protocol/proto/svn/authority/v1/query.proto
d3f6a4491d4f0da001002ed5530d25b917bac99440deda8a78c4fdf99030c7fc  docs/protocol/proto/svn/authority/v1/state.proto
19bf544c9b4ac1fe65386cce239fe3b4c394c1b95d854aa1b44b52dacccf7fb8  docs/protocol/proto/svn/authority/v1/tx.proto
23a18541dc700a8edee4b0198dfebd0ea3a6df1f9915b9dffbeaec218556efbb  docs/protocol/proto/svn/content/v1/genesis.proto
887ea9ef25e21068c7a81d4d45b00317239e208db15227cc0737767c11c91a09  docs/protocol/proto/svn/content/v1/query.proto
0d8f97c4cc156270bf8c6fe453a628cfa74e919769a4ef4027ddbc651e0cc5f2  docs/protocol/proto/svn/content/v1/state.proto
9c04368a00baac4aa9bb092b2031d6566efa6a196a5b377c04518f4e0ce46300  docs/protocol/proto/svn/content/v1/tx.proto
3de8ebd33d133f74eafb5128cdd09b1cf625cc1c8faeade2ebc7521272e59f98  docs/protocol/proto/svn/events/v1/events.proto
f9c7f291e50f9224b4d152c6b5a4d8a9f6ee5f81950bbcde2c1ee0c3540fc50f  docs/protocol/proto/svn/evidence/v1/genesis.proto
688f7d58b308ecf3417bab33e32b2594ed2c262ef78f302b32c0bb1115a73932  docs/protocol/proto/svn/evidence/v1/query.proto
8a461e6936bdb171584a677c156e63f3b4b5329053e572cc87159d9e770957bf  docs/protocol/proto/svn/evidence/v1/state.proto
fa55fa84f8e8311886a866e44bdbb388646100f0396ec29b04990c2072fa632b  docs/protocol/proto/svn/evidence/v1/tx.proto
1a818e14a4094247795868d6536d110c37c5d0e63150749f12515d1c1ef94224  docs/protocol/proto/svn/leases/v1/genesis.proto
38694e568ac9f97ef43aef94510469fc7401488047a46f0496a786d0319097aa  docs/protocol/proto/svn/leases/v1/query.proto
0c1131c955b2bd2c7d7d1e5b36e789b3f58e313d0a413a1844bf38db30a6f8fc  docs/protocol/proto/svn/leases/v1/state.proto
f3acba5925a8ed3b8b1ae686934fd17f85f38efb2cc107fc492ff6cb9ef78611  docs/protocol/proto/svn/leases/v1/tx.proto
0723269bacc329df94fd0c7dbbc7b85dc60fbe9c6be9c3fd57d305d5cf2015f1  docs/protocol/proto/svn/offchain/v1/records.proto
9d8bcc798c5c8735fdfccc55f060f67f49f0d0080baa77ca0ce29f94ac9bc488  docs/protocol/proto/svn/policy/v1/genesis.proto
81c0059a2a497a142b26e18a3cda51ed87c759331338d6dc93062c45ebb17167  docs/protocol/proto/svn/policy/v1/query.proto
db1a9b9d273d2fa47a99e7865c9bbb99525ead8d8e820e7a20ed72ef5b966724  docs/protocol/proto/svn/policy/v1/state.proto
4fc33686daf1c23c1a6d19de60246b37247f0bc15989ee6d095cae3f767aa328  docs/protocol/proto/svn/policy/v1/tx.proto
9aef3a7196eb54c5edb0f4d0c26bb4ec2d11f2c3643a6725989c8c639f64f451  docs/protocol/proto/svn/providers/v1/genesis.proto
1540131227dd5b9cf418395eb1e14c011b23d0f370002ca0511287342bd725e8  docs/protocol/proto/svn/providers/v1/query.proto
2e026ae7d279774b75d711a7be89b22f7b029e1d7c3cd04e1fa8087f0c45acdf  docs/protocol/proto/svn/providers/v1/state.proto
9ab96f5cd4b108e103a1ce9fd984dbc6d30b80f1bf4715988e2bd4d94e1e1313  docs/protocol/proto/svn/providers/v1/tx.proto
e60f3b2ce334b2ab5d59e686ca78c3804509abc5b441ca089e6fba41ab847524  docs/protocol/proto/svn/types/v1/types.proto
2f2e4ec9486495fa49c7159729cab455cdeed85a2451100b99057e9128f5279d  docs/protocol/schemas/evidence-envelope.schema.json
0093a1de6721fc8275bfb48821839606a8baa83accb9f0c5e34e9aafe2f74e4e  docs/protocol/schemas/media-manifest.schema.json
d679f577326d4f796b2d74a6df7ad5b0d2d61bc573aecfdd55af1112aa1770ea  docs/protocol/schemas/provider-capability.schema.json
cb33617cc2a2c0f55da607817826f22d2c9e32ce699d69805f11dc02dcb3304e  docs/protocol/schemas/public-record.schema.json
7588f5e049732a75c38c8060ef8d4473413870ea6d42119e99972b937b1671a1  docs/protocol.md
5fb93997580b7cf92f3ce46d2235f84227de0a9fc2d1770ac64aa4baa939ab38  docs/requirements.md
errors 0
languages MATCH
result PASS
note schema and fixture check; protocol not approved; Gate A approved DEC-001 through DEC-012; v0 freezes once review passes; public launch disabled
EXIT:0

COMMAND .venv/bin/python scripts/run_tests.py --only python
89 passed in 1.65s
EXIT:0

COMMAND .venv/bin/python scripts/run_tests.py --only node
TAP version 13
# Subtest: react version is the pinned release
ok 1 - react version is the pinned release
  ---
  duration_ms: 0.510559
  type: 'test'
  ...
1..1
# tests 1
# suites 0
# pass 1
# fail 0
# cancelled 0
# skipped 0
# todo 0
# duration_ms 53.263276
EXIT:0

COMMAND .venv/bin/python scripts/run_tests.py --only go
ok  	github.com/Maxencejules/shortvideo-network/chain/app	0.001s
ok  	github.com/Maxencejules/shortvideo-network/protocol/check	(cached)
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/app/v1	[no test files]
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/authority/v1	[no test files]
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/content/v1	[no test files]
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/events/v1	[no test files]
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/evidence/v1	[no test files]
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/leases/v1	[no test files]
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/offchain/v1	[no test files]
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/policy/v1	[no test files]
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/providers/v1	[no test files]
?   	github.com/Maxencejules/shortvideo-network/protocol/svn/types/v1	[no test files]
ok  	github.com/Maxencejules/shortvideo-network/agents/provider	(cached)
EXIT:0

COMMAND .venv/bin/ruff check services workers/media packages/protocol tests scripts
All checks passed!
EXIT:0

COMMAND .venv/bin/ruff format --check services workers/media packages/protocol tests scripts
30 files already formatted
EXIT:0

COMMAND .venv/bin/mypy
Success: no issues found in 22 source files
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

COMMAND bash scripts/gofmt_check.sh
gofmt ok files 36
EXIT:0

COMMAND bash scripts/gen_proto.sh
generated 28 proto files
EXIT:0
```

`git diff --exit-code -- proto/svn proto/gen/python` after generation exited 0.

## Deliberate failures, then removal

Each fixture was added on this tree, the runner exited non-zero, and the fixture was removed. The passing commands above are the restored tree.

Python, `tests/unit/test_deliberate_fail.py`, exit 1:

```
F....................................                                    [100%]
=================================== FAILURES ===================================
___________________________ test_deliberate_failure ____________________________
tests/unit/test_deliberate_fail.py:4: in test_deliberate_failure
    raise AssertionError("deliberate failing fixture")
E   AssertionError: deliberate failing fixture
1 failed, 36 passed in 1.04s
python tests failed: 1
```

Node, `apps/web/test/react-pin.test.mjs` replaced with `assert.equal(1, 2)`, exit 1:

```
# Subtest: deliberate node failure
not ok 1 - deliberate node failure
  failureType: 'testCodeFailure'
  error: |-
    Expected values to be strictly equal:

    1 !== 2
# tests 1
# pass 0
# fail 1
apps/web/test/react-pin.test.mjs: node tests failed
node tests failed: 1
```

An emptied `react-pin.test.mjs` still prints `# tests 1` and `# pass 1` because Node counts the file. The runner rejects it, exit 1:

```
# Subtest: /workspace/apps/web/test/react-pin.test.mjs
ok 1 - /workspace/apps/web/test/react-pin.test.mjs
# tests 1
# pass 1
# fail 0
apps/web/test/react-pin.test.mjs: zero tests; an empty file is not verification
node tests failed: 1
```

Go, `TestDeliberateFailure` in `chain/app/scaffold_test.go`, exit 1:

```
--- FAIL: TestDeliberateFailure (0.00s)
    scaffold_test.go:43: deliberate failing fixture
FAIL
FAIL	github.com/Maxencejules/shortvideo-network/chain/app	0.001s
chain tests failed: 1
go tests failed: 1
```

## Hosted CI

These are every hosted run of `goal-03` through `337f7c2c8b2b6361c63fa47bafcd5699a2b03363`. A failed run is not a pass. Where the recorded head was rewritten, the current commit with the same tree is named beside it. The run for the commit that contains this paragraph starts after the push. The pull request names that run.

| Run | Head | Conclusion |
| --- | --- | --- |
| [41203392314](https://github.com/Maxencejules/shortvideo-network/actions/runs/41203392314) | `cf75bc8942046e59db51a1a43f0601241840d83f`, same tree as `397f11ed993aa1e8cd7c12ed1a03ea7d33960685` | failure |
| [41203567927](https://github.com/Maxencejules/shortvideo-network/actions/runs/41203567927) | `b877420d372a59c3a4f77e6fb20b2943943e1998`, same tree as `db9f9b16cdfd32201b7512ec49e06136565f9116` | failure |
| [41203691127](https://github.com/Maxencejules/shortvideo-network/actions/runs/41203691127) | `7594ed19f9e8ff3a5d4f2d5b8437d59f889e806b`, same tree as `d48f5757773e5b0d0fc824ee43c3e2f480e00705` | success |
| [41203787093](https://github.com/Maxencejules/shortvideo-network/actions/runs/41203787093) | `15e2b48ca526726536fc144536356bcdf12e14e2`, same tree as `4ad8319461b1cf6b5302db276ee409d8c8a999cb` | success |
| [41207064075](https://github.com/Maxencejules/shortvideo-network/actions/runs/41207064075) | `0dac34d8f14289de9d5cdcd82183101831cc4b47`, same tree as `f3f9841aa0ef1c393f9fe5fe48b75c513d7c8e70` | failure |
| [41207177004](https://github.com/Maxencejules/shortvideo-network/actions/runs/41207177004) | `3a820b55f87d6ef66f7b3efbb3ab283544d011e6`, same tree as `2c40b443d7d92e45a839c4d97a7a3f7b44901af9` | success |
| [41207468346](https://github.com/Maxencejules/shortvideo-network/actions/runs/41207468346) | `887905e176334acaa10e4239750237fdfd0c9ebc` | success |
| [41208566946](https://github.com/Maxencejules/shortvideo-network/actions/runs/41208566946) | `82bb067214883d5970a02715a0c8ff98c357bbad` | success |
| [41210751631](https://github.com/Maxencejules/shortvideo-network/actions/runs/41210751631) | `337f7c2c8b2b6361c63fa47bafcd5699a2b03363` | success |

`41203787093` is the hosted run for the revision the first review marked FIX. Jobs `python`, `node`, `go`, and `contract` completed. `41207064075` failed its python job. None of these runs is a run of this commit.

`41203392314` was a setup failure, not a deliberate test failure. Verbatim from the failed-step log:

```
node	Set up Node	##[error]Caching for 'false' is not supported
```

The same run also failed `pip install --require-hashes` because `setuptools` was not pinned, and `gofmt` listed `docs/evidence/goal-02/go/vectorcheck.go`. The setup-node cache input and the `setuptools==84.2.1` pin are in `db9f9b16cdfd32201b7512ec49e06136565f9116`. Formatting that Go file, and `scripts/gofmt_check.sh` covering every Go file, are in `f3f9841aa0ef1c393f9fe5fe48b75c513d7c8e70`.

`41203567927` failed the python job because a test invoked `python3` outside the virtualenv (`ModuleNotFoundError: No module named 'yaml'`). The test uses `sys.executable`.

## Handoff to goal 04

- PostgreSQL 18.6 is selected and not installed. No image digest exists.
- RabbitMQ, Celery, and FFmpeg are not pinned.
- No `LICENSE` file. DEC-006 says this goal must not treat the project licence split as applied.
- AST scan does not follow `HANDLERS.get`. Goal 05 keeps harness operations out of the transaction router.
- The liveness scan is best effort and misses dynamic keys (getattr, itemgetter, join, values, **state). Deferred to goal 05. DEC-011 binds goal 06 to the same rule.
- `make devnet-up`, `smoke`, `fault`, and `release-check` exit non-zero. There is no chain binary.

## Not claimed

No devnet, no chain binary, and no production deployment. The secret scan lowercases mnemonic text and ignores list, comment, quote, bracket, and table markers because they are not letters. A blank line does not reset a block. It flags 12, 15, 18, 21, or 24 BIP39 words, and a window of one of those lengths inside a longer block. A block of 13 or 14 words is not a mnemonic. `scripts/bip39_english.txt` is excluded by path. Key assignments cover a validator prefix, camel case, a joined name, a dot, a hyphen, and a private key name. A parameter annotation such as `priv_key: bytes` is not an assignment. The scan is not a third-party secret scanner.
