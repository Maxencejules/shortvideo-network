# Goal 07 evidence

Status: BLOCKED

Every check cited in this file ran on tree `8d7bec054dc44e2e58b891452dae1c989998ffc6` (commit `43c1a10`). The exceptions are the two base probes (`innersig-decode-base.txt`, `go-mod-tidy-diff-base.txt`), which ran on an archive of base `3853e34`, as their commands show. All passed except the expected non-zero runs listed under [Gates](#gates) and [Mutation checks](#mutation-checks): the five mutation runs failed their tests, as expected. The goal is not VERIFIED:

1. App-level genesis export and re-import. InitChain refuses a non-empty catalogue until decision requests DR-07-02 and DR-07-03 are answered. The module-level round trip from committed state ran and passed.
2. REQ-HOST-004. The operator membership rule is DEC-002 and is unset. REQ-TX-002 for production is also BLOCKED, on DEC-001 (DR-07-01). The lease half of REQ-INV-006 is DEFERRED to goal 08.
3. Head CI. CI runs on `pull_request` and on pushes to `main`. No pull request is open for `goal-07`, and the orchestrator does not open one without owner authorization. Pending, not passed. CI's `devnet` job (`bash scripts/devnet/prove-containers.sh`, the four-container proof) cannot run unchanged here: its `docker build` downloads Go modules inside the build container, which does not trust this session's egress-proxy CA, and it exits 1 ([devnet-containers-ci-script.txt](logs/goal-07/devnet-containers-ci-script.txt)). A copy of the script whose only changed step is the image build (repo Dockerfile plus the proxy CA as a BuildKit secret in the discarded build stage, build on the host network) ran on this tree and exited 0 ([devnet-containers-proxy-ca.txt](logs/goal-07/devnet-containers-proxy-ca.txt); sources in [goal-07/containers/](goal-07/containers/)). That is evidence for `svnd devnet init` and the container topology on this tree, not a pass of the CI job itself.
4. golangci-lint. The installed v2.5.0 is built with go1.25.1 and refuses the go1.26.10 module. It is not a repository gate: neither the Makefile nor CI runs it.

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a production network. The devnet is four processes on one machine. No independent operator is claimed.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH. GPT-6 Astra was not used.
- Model that ran: `claude-opus-5-5`, in Claude Code remote session `session_014mwakeb27YrugDfwAx8vXL`. Source: claude-code-remote `get_session`, read on 2026-11-01 during this evidence run: `configured_model`, `session_context.model` and `external_metadata.last_served_model` are all `claude-opus-5-5`. Workflow subagents inherit the session model. This source is a tool response, not a `capture.sh` log. The evidence audit read `get_session` again and saw the same model and effort values.
- Effort setting actually used: `session_context.effort_level` = `ultracode` (`external_metadata.is_ultracode_active` true). That is not the playbook's HIGH.
- AGENTS.md's substitution note names Cursor cloud agents. This run is Claude Code, a further substitution. Max waived the model mismatch for goals 04-06. That waiver does not extend to goal 07 on its own. Owner acknowledgement is still needed.
- Extra effort does not certify correctness.

## Tested revision

| Item | Value | Log |
| --- | --- | --- |
| Commit | `43c1a104d43ba55f2f58684a6f3790c0bb04c9f2` | [revision.txt](logs/goal-07/revision.txt) |
| Tree | `8d7bec054dc44e2e58b891452dae1c989998ffc6` | every log below prints it |
| Branch | `goal-07`, pushed at `43c1a10` | [stack.txt](logs/goal-07/stack.txt), [ci-head.txt](logs/goal-07/ci-head.txt) |
| Base | `3853e3430217096c3e65664bc799859942ea451e`, the head of `goal-06` | [stack.txt](logs/goal-07/stack.txt) |
| Goal 06 | PR 9 is `open`, not merged, head `3853e34`. Goal 07 is stacked on it. | [stack.txt](logs/goal-07/stack.txt) |
| `main` | `f3f583e19355a181ef64de874e5cf26679e6ec30` | [stack.txt](logs/goal-07/stack.txt) |
| Runtime | Ubuntu 24.04.5 LTS, Linux 6.18.44, 4 CPUs, 15 GiB. Go 1.26.10. Python 3.12.3, ruff 0.17.3, mypy 2.4.1. Node v22.23.4, npm 10.9.9. protoc 36.2. Docker server 29.9.2 (local dockerd, unix socket; used only for the container devnet proof below). | [runtime.txt](logs/goal-07/runtime.txt), [docker-info.txt](logs/goal-07/docker-info.txt) |

Branch history from the base ([revision.txt](logs/goal-07/revision.txt)):

- `26454c1` adds `scripts/evidence/capture.sh`.
- `9ca725e`, `4ba4168`, `5213a7c`, `b4282cb` and `a88453e` are labelled WIP checkpoints. They are untested. They were pushed only to preserve work.
- `2e0888d` was the first gated code commit. It is SUPERSEDED. A second review round found a major defect in it: the retired-key history of one provider was unbounded, and `Query/Provider`, the first entry of a `Providers` page and `ExportGenesis` read all of it. Its README also claimed that the node's query gas limit bounded that work, while the app set no query gas limit.
- `43c1a10` fixes the second-round findings. It is the tested revision.

An evidence run had started on `2e0888d`. It was stopped and its outputs were discarded, because they were on the old tree. No number and no log from it is used here. Every log under `logs/goal-07/` was produced by this run, except the three `-post` logs, which the evidence audit re-ran (see below).

The fix round's independent verifier left four problems: the raw `/store` query overclaim, the CheckTx handler wording and the gas-0 exception wording, simulate gas equality, and the DR-07-10 security consequence. The orchestrator fixed them in `43c1a10`: docs and comments, and `TestSimulateEstimatesGas` now asserts simulated gas equals committed gas. Those last edits had no further independent verification round. The only checks on them are the logs below, the five mutants in [Mutation checks](#mutation-checks), and the evidence audit's reading of the changed text against the code. The audit found two code comments and one `chain/README.md` sentence that still overstate the query gas limit (see [Follow-ups](#follow-ups)); it changed no code.

Every log under `logs/goal-07/` was written by `scripts/evidence/capture.sh` and prints `HEAD 43c1a10...` and `TREE 8d7bec05...`. There are 64 logs. 59 print `DIRTY no`. Five list `docs/progress.md` and `docs/progress.tsv` as `DIRTY_PATH`, and nothing else: `contract-requirements-post.txt`, `contract-protocol-post.txt` and `scan-secrets-post.txt`, which check those files, and the two container devnet logs, run after the progress edits. The three `-post` checks were re-run last, after the final edits to this file and the progress rows, so they check the final text. 55 logs end `EXIT:0`. Nine end non-zero as expected: the five mutant runs (exit 1, killed), `go-mod-tidy-diff.txt` and `go-mod-tidy-diff-base.txt` (exit 1, follow-up), `golangci-lint.txt` (exit 3, BLOCKED), and `devnet-containers-ci-script.txt` (exit 1, BLOCKED: the build container does not trust the session proxy CA).

[unit-app.txt](logs/goal-07/unit-app.txt) is 5.6 MB. Verbose app tests print the SDK debug logger. The size is the real output.

## Prerequisites

| Prerequisite | Status | Evidence |
| --- | --- | --- |
| Goal 02 | IMPLEMENTED | [goal-02.md](goal-02.md). Protocol v0 stays the frozen candidate. Goal 07 changes no `.proto` file and no `bounds.json` ([diff-stat.txt](logs/goal-07/diff-stat.txt)). Regeneration gives no diff ([proto-regen.txt](logs/goal-07/proto-regen.txt)). |
| Goal 05 | VERIFIED | [goal-05.md](goal-05.md), [PR 8](https://github.com/Maxencejules/shortvideo-network/pull/8). |
| Goal 06 | VERIFIED, not merged | [goal-06.md](goal-06.md), [PR 9](https://github.com/Maxencejules/shortvideo-network/pull/9) open ([stack.txt](logs/goal-07/stack.txt)). Goal 07 edits x/authority. A later goal-06 change needs a rebase of this branch. |
| Signed manifest contract | IMPLEMENTED | The capability validator matches `docs/protocol/schemas/provider-capability.schema.json`: `TestValidatorMatchesSchemaFile`, `TestSchemaKeywordCheck`, `TestSignAndVerify` ([unit-providers.txt](logs/goal-07/unit-providers.txt)). The signed wire envelope for `GET /capability` is not in v0 (DR-07-09). |

## Acceptance

| Check | Status | Evidence on tree `8d7bec05` |
| --- | --- | --- |
| Commit valid registration, update and withdrawal transactions on the devnet | VERIFIED, single machine | [devnet-registry.txt](logs/goal-07/devnet-registry.txt). Code 0 at: register p1 h4, register p2 h6, council admit p1 h12 and p2 h20, update p1 h21, rotate p1 h23, drain p2 h25, council disable p2 h34, register c1 h36, register c2 h38, withdraw c1 h40, council force-withdraw c2 h49, idempotent replay of c1 h51. |
| Reject cross-owner edits | VERIFIED | Devnet `cross-owner-update` h52 svn/2, `cross-owner-withdraw` h65 svn/2. App `TestRejectedRegistryMessagesLeaveTheFullState` ([unit-app.txt](logs/goal-07/unit-app.txt)). |
| Reject unauthorized provider updates | VERIFIED | Devnet `service-updates-provider` h54 svn/2, `stale-rotation` h67 svn/5, `wrong-domain-drain` h69 svn/12. |
| Reject duplicate versions | VERIFIED | Devnet `duplicate-version-c1` h55 svn/4, after c1 was withdrawn at h40. v0 accepts only `content_version` 1 (DR-07-04). |
| Reject invalid digests | VERIFIED | Devnet `digest-31-bytes` h56 svn/19, `digest-algorithm-0` h58 svn/7. |
| Reject oversized metadata | VERIFIED | Devnet `provider-id-65-bytes` h60 svn/19, `package-bytes-cap-plus-1` h61 svn/19. CheckTx `tx-70-KiB` svn/19, not included. |
| Reject replayed requests | VERIFIED | Devnet `replayed-business-id` h63 svn/3 and `second-withdraw` h66 svn/15. The same signed bytes rebroadcast: `check_tx=sdk/32`, `mempool ErrTxInCache`, not included. The same business id and payload is idempotent: h51 code 0. App `TestIdempotentRegistryReplay`. |
| Rejected transactions leave state unchanged | VERIFIED | Devnet `phase=replay-reject ... full_state_unchanged=true` for all 12 committed rejections. App `TestRejectedRegistryMessagesLeaveTheFullState` compares `FullSnapshot` with `bytes.Equal`. Keeper: 21 subtests of `TestRejectedProviderMessagesLeaveTheStore` ([unit-providers.txt](logs/goal-07/unit-providers.txt)) and 26 of `TestRejectedContentMessagesLeaveTheStore` ([unit-content.txt](logs/goal-07/unit-content.txt)). |
| Round-trip genesis, module level | VERIFIED | `TestGenesisRoundTripFromCommittedState` ([unit-app.txt](logs/goal-07/unit-app.txt)) imports the app's export of committed state into bare module keepers. `TestGenesisRoundTrip` and `TestGenesisRoundTripFromCommittedTransitions` in providers ([unit-providers.txt](logs/goal-07/unit-providers.txt)), `TestGenesisRoundTrip` in content ([unit-content.txt](logs/goal-07/unit-content.txt)). The goal-05 test `TestInitExportAndChainID` still passes; it round-trips the empty launch genesis through the app, with no registry record. |
| Round-trip genesis, app level | BLOCKED | DR-07-02, DR-07-03. `TestLaunchGenesisRejectsNonEmptyCatalog` passes: InitChain refuses the app's own non-empty export. `TestGenesisRoundTripFromCommittedState` is module-level only. |
| Reconstruct query results from committed state | VERIFIED | Devnet h69: six ABCI queries equal on 4 nodes; `replay-queries height=69 count=6 equal=true` from an in-process replay of the blocks; one gRPC call equal to the ABCI bytes; history `height=39 c1=PENDING tombstone=false; height=40 c1=WITHDRAWN tombstone=true`. App `TestRegistryFlowReplayAndQueries`. |
| Compare replay hashes | VERIFIED | Devnet `phase=replay height=69 app_hash=C776EA09ECB311979DE5858A00BB86AC90E7A42077AA7B5C45A5FCFFA38E9772 nodes=4 equal=true`. App `TestRegistryFlowReplayAndQueries` compares both nodes' app hash at every height. |
| Pagination boundaries | VERIFIED | `TestProvidersPaginationBoundaries` (9 subtests), `TestProvidersPageRetiredKeyBudget`, `TestRetiredKeyBoundsFitAPage` ([unit-providers.txt](logs/goal-07/unit-providers.txt)). msgkit `TestPaginateBoundaries`, `TestPaginateStartKeyIsInclusive`, `TestCheckPage` ([unit-msgkit.txt](logs/goal-07/unit-msgkit.txt)). App `TestGovernedLimitsReachTheRegistries`. Devnet limit 1: page 1 `next_page_key="p2"`, page 2 `next_page_key=""`. |
| Event schemas | VERIFIED | App `TestRegistryEventSchemas` against `chain/app/testdata/registry-events.golden.json`. msgkit `TestEmitParseRoundTripEveryEventType`, `TestEventAllowlistIsEventsProto`, `TestParseCommitted`. Devnet `typed=11 typed_parsed_with_msg_index=11`. |
| No private user details or media payloads in stored state or event fixtures | VERIFIED | App `TestNoPrivateDataInStateOrEvents`, `TestSubmitterTextFields`, `TestTransactionsRejectVideoBytes`, `TestStateSchemaHasNoPayloadField` (13 subtests). Content `TestContentStoresOnlyDigests`. Devnet `heights=1..69 block_txs=41 events=123 typed=11 ... canaries=0 forbidden_names=0`. Limits are under residual risks. |
| Handoff: minimal real-transaction demo and negative tests | VERIFIED | [devnet-registry.txt](logs/goal-07/devnet-registry.txt). |
| Handoff: package identity and provider admission APIs | IMPLEMENTED | Goal 08 is the consumer. Providers `TestHandoffAPI` (`check-new-obligation`, `provider-returns-a-copy`, `verify-provider-signature`). Content `TestPackageIdentity`, `TestCheckOfferableOrder`, `TestWithdrawalBlocksOfferAndRevival`. |
| Goal 05 and 06 devnet regressions | VERIFIED | [devnet-regress.txt](logs/goal-07/devnet-regress.txt): `TestDevnetValidatorLifecycle` 67.95s, `TestFourValidatorsCommitRestartAndQuorum` 16.95s, `TestInitRefusesToReplaceGenesis` 0.02s. |
| No test removed since the base | VERIFIED | [tests-not-removed.txt](logs/goal-07/tests-not-removed.txt): 28 test and fuzz functions in `chain` at the base, 242 at head, none removed. |

The devnet rejections are committed with a failure code. The devnet replays each rejected block in process and compares the full state. CheckTx rejections are printed separately as `included=false`: outsider sdk/9, fee svn/21, 70 KiB tx svn/19, rebroadcast sdk/32.

CAPACITY and OVERFLOW cannot be reached at app level before goal 08. Reserved bytes are 0 until leases exist, and an overflow needs key generation 2^64-1. Keeper tests cover them: `TestRejectedProviderMessagesLeaveTheStore/update-below-reserved`, `/rotate-overflow`, `TestUpdateProviderSpecCases/update-provider-below-reserved`, `TestRotateProviderKeySpecCases/overflow` ([unit-providers.txt](logs/goal-07/unit-providers.txt)).

## Bounds, query gas and simulation (fix round)

| Item | Status | Evidence |
| --- | --- | --- |
| Retired-key cap per provider id (`MaxRetiredKeysPerProvider` = 1024, PROPOSED, DR-07-10) | VERIFIED | `TestRotationCap` (subtests `one_rotation_past_the_bound`, `stale_generation_at_the_bound`, `same_key_at_the_bound`), `TestStoredRecordAboveTheCap`, `TestGenesisRetiredKeyCap`, `TestRetiredKeyBoundsFitAPage` ([unit-providers.txt](logs/goal-07/unit-providers.txt)). `TestRotationCapInApp` ([unit-app.txt](logs/goal-07/unit-app.txt)) logs `at the bound: Query/Provider 46094 bytes, 200911 gas; Providers page 201394 gas; query gas limit 20000000`. |
| Response sizes | VERIFIED | `TestQueryResponseSizeBound` ([unit-providers.txt](logs/goal-07/unit-providers.txt)) asserts fixed bounds and logs, on this tree: `largest Query/Provider response: 47300 bytes (bound 65536)`, `largest Providers page: 79147 bytes over 2 pages (bound 131072)`, `largest exported record: 122426 bytes (bound 196608)`. The measured sizes depend on the build; the bounds are the assertion. |
| Query gas limit (`app.DefaultQueryGasLimit` = 20,000,000, PROPOSED node default, DR-07-10) | VERIFIED for ABCI query on a service path and the query router; node gRPC server path untested | `TestQueryGasLimit` (ABCI query above the limit is the SDK recovered-panic result, codespace `undefined`, code 111222; the query router panics with `ErrorOutOfGas`), `TestQueryGasLimitOption` ([unit-app.txt](logs/goal-07/unit-app.txt)), `TestNodeFileQueryGasLimit` ([unit-node.txt](logs/goal-07/unit-node.txt)). No test drives the node gRPC server's out-of-gas path. Raw `/store` queries are not metered (residual risk). |
| Simulation | VERIFIED | `TestSimulateEstimatesGas` ([unit-app.txt](logs/goal-07/unit-app.txt)) asserts simulated gas equals committed gas. The log shows 11 steps with equal values, from `register p1: simulated gas 106771, committed gas 106771` to `withdraw c1: simulated gas 101909, committed gas 101909`. Gas 0 is accepted only in simulation; CheckTx and a block reject it as BOUNDS. `TestEnforceCommitted`, `TestHandlerMeterCheckByMode` ([unit-authority.txt](logs/goal-07/unit-authority.txt)). |
| Registries refuse the goal-05/06 scaffold layout (ConsensusVersion 2) | VERIFIED | `TestAppRefusesScaffoldLayout` (4 subtests, [unit-app.txt](logs/goal-07/unit-app.txt)), `TestNoMigrationFromScaffoldLayout` in providers and content. |
| Governed limits fail closed | VERIFIED | `TestGovernedLimitFailsClosed` ([unit-app.txt](logs/goal-07/unit-app.txt)), `TestKeeperReadSurface` with subtests `governed_limit` and `corrupt_side_document` ([unit-authority.txt](logs/goal-07/unit-authority.txt)). |

## Requirements rows for goal 07

| Requirement | Status | Evidence |
| --- | --- | --- |
| REQ-PROD-010 | VERIFIED | `TestTransactionsRejectVideoBytes` ([unit-app.txt](logs/goal-07/unit-app.txt)): unknown fields carrying 40 KiB of synthetic media fail decode at the top level, in a `Digest`, in an `InnerSignature` and in the tx body; a 1 KiB digest value is BOUNDS; a 70 KiB tx is BOUNDS. Devnet CheckTx `tx-70-KiB` svn/19. |
| REQ-MODU-002 | VERIFIED | `TestProviderRecordFields` ([unit-providers.txt](logs/goal-07/unit-providers.txt)). Devnet `provider-p1` and `provider-p2` queries equal on 4 nodes. |
| REQ-MODU-003 | VERIFIED | `TestContentRecordFields` ([unit-content.txt](logs/goal-07/unit-content.txt)). Devnet `content-c1` and `content-c2` queries equal on 4 nodes. |
| REQ-OFF-001 | VERIFIED | `TestTransactionsRejectVideoBytes`, `TestStateSchemaHasNoPayloadField`, `TestNoPrivateDataInStateOrEvents` ([unit-app.txt](logs/goal-07/unit-app.txt)), `TestContentStoresOnlyDigests` ([unit-content.txt](logs/goal-07/unit-content.txt)). |
| REQ-TX-002, devnet intent | VERIFIED | `TestRegisterContentChecksInSpecOrder` (42 subtests), `TestRegisterContentSuccessBoundaries`, `TestRegisterContentCreatesNoHostingClaim` ([unit-content.txt](logs/goal-07/unit-content.txt)); devnet register c1, c2. The user-intent rule is the devnet-only interim of DR-07-01. |
| REQ-TX-002, production intent | BLOCKED | DEC-001 custody and the intent record are unset (DR-07-01). Without the DEC-001 devnet parameter, RegisterContent is DECISION_BLOCKED (`TestDecisionBlockedRegistryPaths`, [unit-app.txt](logs/goal-07/unit-app.txt)). |
| REQ-INV-006, silent revival | VERIFIED | Devnet `duplicate-version-c1` h55 svn/4 after withdrawal at h40. `TestWithdrawnBlocksRevival`, `TestSecondWithdraw` ([unit-content.txt](logs/goal-07/unit-content.txt)). |
| REQ-INV-006, new sponsored lease | DEFERRED | Goal 08 owns OfferLease, which is still a closed stub. The API it will call is tested: `TestWithdrawalBlocksOfferAndRevival` (`CheckOfferable` and `CheckNotWithdrawn` return WITHDRAWN). |
| REQ-HOST-004 | BLOCKED | DEC-002. Admission works only with the DEC-002 devnet parameter and operator group `devnet-test-only`. Without it, admit is DECISION_BLOCKED (`TestDecisionBlockedRegistryPaths`, `TestRejectedProviderMessagesLeaveTheStore/admit-decision-blocked`). |

## Goal 06 defects fixed here

InnerSignature decoding. On the goal-06 tree, `svn.types.v1.InnerSignature` was not registered for transaction decoding. A `MsgRotateProviderKey` transaction could not be decoded, so goal 06's rotation handler was not reachable through a real transaction.

- [innersig-decode-base.txt](logs/goal-07/innersig-decode-base.txt): the probe [innersig_decode_probe_test.go.txt](goal-07/innersig_decode_probe_test.go.txt), copied into an archive of base `3853e34`, reports `decode error: failed to retrieve the message of type "svn.types.v1.InnerSignature": tx parse error`. The probe logs its result and does not assert it, so the same file runs on both trees.
- [innersig-decode-head.txt](logs/goal-07/innersig-decode-head.txt): the same probe on an archive of `43c1a10` reports `decoded 1 message(s) of type *providersv1.MsgRotateProviderKey`.
- `chain/app/encoding.go` registers the type. The regression test is `TestInnerSignatureMessagesDecode` in `chain/app/registry_decode_test.go`. It passes in [unit-app.txt](logs/goal-07/unit-app.txt).

Simulation of authority messages. On the goal-06 tree `enforceCommitted` checked the gas meter limit in every mode ([authority-enforce-base.txt](logs/goal-07/authority-enforce-base.txt)). The SDK simulates on an infinite gas meter, so every simulated authority message was BOUNDS and no client could estimate gas. This run did not simulate on the base tree; that consequence follows from the base code and the SDK's simulation meter. Goal 07 skips only the meter-limit check in simulation. FinalizeBlock still runs it, and CheckTx runs no message handler. `TestEnforceCommitted` and `TestHandlerMeterCheckByMode` pass in [unit-authority.txt](logs/goal-07/unit-authority.txt).

The goal-06 reviewer should know both. They may need a note on PR 9.

## Recorded deviations

- A direct `AdmitProvider`, `DisableProvider` or `ForceWithdraw` is the SDK decode error sdk/2, not `ERROR_CODE_FORBIDDEN` svn/2. No such message type exists; these actions are council payloads only (DEC-009). `TestDirectAdminMessagesDoNotExist` ([unit-app.txt](logs/goal-07/unit-app.txt)).
- A copy of another account's `RegisterProvider` (same business id and payload, another signer) is FORBIDDEN, not idempotent. This is stricter than `spec_machine.py`. `TestRegisterReplayByAnotherAccountIsForbidden` ([unit-providers.txt](logs/goal-07/unit-providers.txt)). See DR-07-08.
- A non-empty memo is BOUNDS. A fee payer, fee granter or tip is FEE_REJECTED. Both are ante-handler rules. `TestTransactionsRejectVideoBytes`.
- Unknown fields, critical (99) and non-critical (1024, 2047), are rejected by a strict decoder at decode time (sdk/2), not mapped to `ERROR_CODE_UNKNOWN_FIELD`. `TestTransactionsRejectVideoBytes`.
- The per-provider retired-key cap (BOUNDS at 1024 retired keys) is a chain rule. It is not in `spec_machine.py` (DR-07-10).
- A hosting intent signed for an unknown role is UNAUTHORIZED, not the spec's FORBIDDEN. An unknown role cannot be recognised from a signature (`chain/x/content/README.md`). `TestRegisterContentChecksInSpecOrder/intent-other-role` ([unit-content.txt](logs/goal-07/unit-content.txt)) signs with role `CREATOR` and expects UNAUTHORIZED.
- `TestMessagesAreRejected` (goal 05) now sends a complete `MsgRegisterProvider`, because the handler is real. It still expects FORBIDDEN, now as typed svn/2. Its fee and gas checks are typed too. The check is stricter, not weaker.

## Store layout and upgrade

x/providers and x/content are ConsensusVersion 2. Version 1 was the goal-05 scaffold. `app.New` refuses a home whose registry stores hold the goal-05/06 scaffold layout (`TestAppRefusesScaffoldLayout`), and neither module has a migration from it (`TestNoMigrationFromScaffoldLayout`). A goal-06 home cannot be upgraded in place. It must be initialized again. No deployed chain exists.

## Fuzz runs

Each run: `bash -c '...; env -C chain go test -run='^$' -fuzz='^<Target>$' -fuzztime=<N> <package>; ...'`, one at a time; the log start times and process ids are in sequence. The evidence runner reports spot checks with `ps` that showed no other test process during the runs; those checks are not logged, so that statement is unverified. A goal-08 worktree exists on this machine (seen by the evidence audit with `git worktree list`, not logged). The load average reached 8.80 on 4 CPUs (`FuzzCapabilityParse` after), so the CPU was shared at times; this run cannot say with what. The wrapper prints the load average before and after and lists new or changed files under `chain/` after the run. All ten exited 0. Every log has a `now fuzzing with 4 workers` line, so each run got past its baseline corpus. No run wrote a crasher: the file list after each run is empty. No `context deadline exceeded` occurred.

`go test -list 'Fuzz.*' ./...` in `chain` ([fuzz-list.txt](logs/goal-07/fuzz-list.txt)) lists these ten targets. It also lists `TestFuzzBoundedPayload` in `x/authority`, which is a plain test whose name matches the pattern, not a fuzz target. No fuzz target in the goal-07 packages was left out.

| Target | Package | New in goal 07 | -fuzztime | Baseline entries | Last `execs:` line | Load before / after | Exit | Log |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `FuzzStateTransitions` | `./x/authority/` | no (goal 06) | 90s | 281 | `elapsed: 1m31s, execs: 86437 (0/sec), new interesting: 7 (total: 288)` | 1.89 / 5.14 | 0 | [log](logs/goal-07/fuzz-FuzzStateTransitions.txt) |
| `FuzzProviderTransitions` | `./x/providers/` | yes | 30s | 135 | `elapsed: 31s, execs: 15969 (0/sec), new interesting: 10 (total: 145)` | 5.14 / 7.64 | 0 | [log](logs/goal-07/fuzz-FuzzProviderTransitions.txt) |
| `FuzzProvidersGenesisJSON` | `./x/providers/` | yes | 30s | 140 | `elapsed: 31s, execs: 83278 (0/sec), new interesting: 14 (total: 154)` | 7.11 / 6.21 | 0 | [log](logs/goal-07/fuzz-FuzzProvidersGenesisJSON.txt) |
| `FuzzCapabilityParse` | `./x/providers/capability/` | yes | 30s | 108 | `elapsed: 31s, execs: 89988 (0/sec), new interesting: 6 (total: 114)` | 6.21 / 8.80 | 0 | [log](logs/goal-07/fuzz-FuzzCapabilityParse.txt) |
| `FuzzContentTransitions` | `./x/content/` | yes | 30s | 148 | `elapsed: 31s, execs: 13534 (0/sec), new interesting: 16 (total: 164)` | 8.80 / 7.95 | 0 | [log](logs/goal-07/fuzz-FuzzContentTransitions.txt) |
| `FuzzContentGenesisJSON` | `./x/content/` | yes | 30s | 102 | `elapsed: 31s, execs: 51740 (0/sec), new interesting: 23 (total: 125)` | 7.95 / 6.65 | 0 | [log](logs/goal-07/fuzz-FuzzContentGenesisJSON.txt) |
| `FuzzCouncilRegistryPayloads` | `./x/authority/` | yes | 30s | 263 | `elapsed: 31s, execs: 17872 (0/sec), new interesting: 15 (total: 278)` | 6.65 / 6.94 | 0 | [log](logs/goal-07/fuzz-FuzzCouncilRegistryPayloads.txt) |
| `FuzzParsePayload` | `./x/authority/` | no (goal 06) | 30s | 102 | `elapsed: 31s, execs: 18739 (0/sec), new interesting: 14 (total: 116)` | 6.94 / 6.01 | 0 | [log](logs/goal-07/fuzz-FuzzParsePayload.txt) |
| `FuzzInnerSignatureCheck` | `./msgkit/` | yes | 30s | 92 | `elapsed: 31s, execs: 161828 (0/sec), new interesting: 9 (total: 101)` | 6.01 / 5.80 | 0 | [log](logs/goal-07/fuzz-FuzzInnerSignatureCheck.txt) |
| `FuzzTypedEventParse` | `./msgkit/` | yes | 30s | 155 | `elapsed: 31s, execs: 254260 (0/sec), new interesting: 19 (total: 174)` | 5.80 / 5.08 | 0 | [log](logs/goal-07/fuzz-FuzzTypedEventParse.txt) |

The three `*Transitions` targets and `FuzzCouncilRegistryPayloads` are state-transition fuzzers over bounded op sequences. The others are parser fuzzers. `FuzzStateTransitions` ran 90s because its baseline corpus is large: its log shows `281/281 completed, now fuzzing with 4 workers` at 11s. Exec counts depend on the CPU share. The load average is a one-minute average, so the "before" value of each run includes the tail of the previous run. The `execs:` line is the last progress line of each log.

## Mutation checks

Mutation runs check that tests added or tightened in the fix round fail when the code they guard is wrong. Each mutant is a full copy of one committed source file with one small change, stored under [goal-07/mutants/](goal-07/mutants/). [run_mutant.sh](goal-07/mutants/run_mutant.sh) prints the diff, writes a `go -overlay` JSON from the repository-relative target path, and runs the named tests. The repository files are not modified. The expected exit is non-zero: a test failure means the mutant was killed. Each run used `scripts/evidence/capture.sh <log> -- bash docs/evidence/goal-07/mutants/run_mutant.sh <mutant>`, one at a time.

| Mutant | Change | Tests | Exit (expected non-zero) | Killed by | Log |
| --- | --- | --- | --- | --- | --- |
| `m01-simulate-extra-gas` | `msgkit.Enforce` consumes 1 extra gas in simulation | `TestSimulateEstimatesGas` | 1 | `register p1: simulated gas 106772, committed gas 106771`. A `>=` check would accept this mutant; the equality check rejects it. | [log](logs/goal-07/mutant-m01-simulate-extra-gas.txt) |
| `m02-ante-gas0-always` | the ante handler accepts a declared gas limit of 0 in every mode | `TestSimulateEstimatesGas` | 1 | `CheckTx with gas 0: sdk/11 out of gas ...`, want svn BOUNDS | [log](logs/goal-07/mutant-m02-ante-gas0-always.txt) |
| `m03-query-gas-zero-unlimited` | `QueryGasLimit` returns 0 (SDK unlimited) when the option is absent or 0 | `TestQueryGasLimit`, `TestQueryGasLimitOption` | 1 | `no option: 0 <nil>, want 20000000` and the `zero` and `empty string` cases | [log](logs/goal-07/mutant-m03-query-gas-zero-unlimited.txt) |
| `m04-rotation-cap-off-by-one` | the rotation cap uses `>` instead of `>=` | `TestRotationCap` | 1 | `one_rotation_past_the_bound: code , want ERROR_CODE_BOUNDS` and the other two subtests | [log](logs/goal-07/mutant-m04-rotation-cap-off-by-one.txt) |
| `m05-authority-meter-finalize-only` | `enforceCommitted` checks the meter only in FinalizeBlock | `TestEnforceCommitted`, `TestHandlerMeterCheckByMode` | 1 | `check on the infinite meter: "", want "ERROR_CODE_BOUNDS"` | [log](logs/goal-07/mutant-m05-authority-meter-finalize-only.txt) |

All five were killed. Five mutants are a spot check of the fix-round changes, not a mutation score for the goal.

## Unit suites

| Log | Top-level tests passed | Subtests passed | Failed | Skipped | Exit |
| --- | --- | --- | --- | --- | --- |
| [unit-svnerr.txt](logs/goal-07/unit-svnerr.txt) | 2 | 0 | 0 | 0 | 0 |
| [unit-msgkit.txt](logs/goal-07/unit-msgkit.txt) | 51 | 31 | 0 | 0 | 0 |
| [unit-providers.txt](logs/goal-07/unit-providers.txt) | 63 | 289 | 0 | 0 | 0 |
| [unit-content.txt](logs/goal-07/unit-content.txt) | 46 | 166 | 0 | 0 | 0 |
| [unit-authority.txt](logs/goal-07/unit-authority.txt) | 35 | 87 | 0 | 0 | 0 |
| [unit-node.txt](logs/goal-07/unit-node.txt) | 1 | 0 | 0 | 0 | 0 |
| [unit-app.txt](logs/goal-07/unit-app.txt) | 39 | 19 | 0 | 0 | 0 |

Counts are `--- PASS` lines at column 0 (top level) and indented (subtests). Top-level counts include fuzz targets running their seed corpus. Seed runs are not fuzz runs; the fuzz runs are the table above.

## Gates

| Gate | Status | Log |
| --- | --- | --- |
| `go test -race -count=1 -timeout 20m ./chain/...` | VERIFIED | [race-chain.txt](logs/goal-07/race-chain.txt) exit 0. devnet 176.165s, app 58.965s. |
| `go test -race` for `./proto/...` and `./agents/provider/...` | VERIFIED | [race-proto.txt](logs/goal-07/race-proto.txt), [race-agents-provider.txt](logs/goal-07/race-agents-provider.txt) |
| `python scripts/run_tests.py --only go` | VERIFIED | [tests-go.txt](logs/goal-07/tests-go.txt) exit 0. devnet 100.289s. It runs `go test ./...` without `-count=1`, so three packages are `(cached)`: `chain/svnerr`, `protocol/check` and `agents/provider`. They ran uncached with `-count=1` in [unit-svnerr.txt](logs/goal-07/unit-svnerr.txt), [race-proto.txt](logs/goal-07/race-proto.txt) and [race-agents-provider.txt](logs/goal-07/race-agents-provider.txt). |
| `bash scripts/gofmt_check.sh` | VERIFIED | [gofmt.txt](logs/goal-07/gofmt.txt): `gofmt ok files 171` |
| `go vet` chain, proto, agents/provider | VERIFIED | [vet-chain.txt](logs/goal-07/vet-chain.txt), [vet-proto.txt](logs/goal-07/vet-proto.txt), [vet-agents-provider.txt](logs/goal-07/vet-agents-provider.txt) |
| Proto regeneration diff | VERIFIED | [proto-regen.txt](logs/goal-07/proto-regen.txt): 28 files, no diff, no untracked file |
| ruff format, ruff check, mypy | VERIFIED | [ruff-format.txt](logs/goal-07/ruff-format.txt), [ruff-check.txt](logs/goal-07/ruff-check.txt), [mypy.txt](logs/goal-07/mypy.txt) (24 source files) |
| workflow policy, secret scan, SBOM | VERIFIED | [workflow-policy.txt](logs/goal-07/workflow-policy.txt), [scan-secrets.txt](logs/goal-07/scan-secrets.txt), [sbom-check.txt](logs/goal-07/sbom-check.txt) (1320 components). The post-docs scan is [scan-secrets-post.txt](logs/goal-07/scan-secrets-post.txt). |
| Python tests | VERIFIED | [tests-python.txt](logs/goal-07/tests-python.txt): 120 passed |
| `npm run typecheck`, Node tests | VERIFIED | [npm-typecheck.txt](logs/goal-07/npm-typecheck.txt), [tests-node.txt](logs/goal-07/tests-node.txt): 1 pass, 0 fail |
| Contract checkers before the docs edit | VERIFIED | [contract-requirements-pre.txt](logs/goal-07/contract-requirements-pre.txt), [contract-protocol-pre.txt](logs/goal-07/contract-protocol-pre.txt): `result PASS` |
| Contract checkers after the docs edit | VERIFIED | [contract-requirements-post.txt](logs/goal-07/contract-requirements-post.txt), [contract-protocol-post.txt](logs/goal-07/contract-protocol-post.txt): `result PASS` |
| `go.mod` and `go.sum` unchanged since the base | VERIFIED | [gomod-unchanged.txt](logs/goal-07/gomod-unchanged.txt): `git diff --exit-code 3853e34 HEAD -- chain/go.mod chain/go.sum` exit 0 |
| `go mod tidy -diff` | Follow-up | [go-mod-tidy-diff.txt](logs/goal-07/go-mod-tidy-diff.txt) exit 1, as expected. Not a gate. See follow-ups. |
| golangci-lint | BLOCKED | [golangci-lint.txt](logs/goal-07/golangci-lint.txt) exit 3: v2.5.0 is built with go1.25.1 and refuses the go1.26.10 module. Not a repository gate: neither the Makefile nor CI runs it. |
| Head CI | BLOCKED | [ci-head.txt](logs/goal-07/ci-head.txt): 0 pull requests with head `goal-07`, 0 workflow runs for `43c1a10`. Pending, not passed. |
| CI `devnet` job, `bash scripts/devnet/prove-containers.sh` | BLOCKED (unchanged script) | [devnet-containers-ci-script.txt](logs/goal-07/devnet-containers-ci-script.txt): EXIT 1. `go build` inside `docker build` cannot verify TLS to proxy.golang.org because the build container does not trust the session egress-proxy CA. An environment limit, not a code failure. |
| Container devnet proof, CI script with only the image-build step changed | VERIFIED (labelled deviation) | [devnet-containers-proxy-ca.txt](logs/goal-07/devnet-containers-proxy-ca.txt): EXIT 0, HEAD 43c1a10, TREE 8d7bec05. Four containers on the host network agree on the app hash at every sample; with two nodes stopped the chain stalls at height 15; after restart all four reach height 21 with the same app hash `20C8212C…FAC2F`. Empty blocks only, as the script states. Build differs from CI: [Dockerfile.proxy-ca](goal-07/containers/Dockerfile.proxy-ca) adds the proxy CA as a BuildKit secret in the discarded build stage; [prove-containers-proxy-ca.sh](goal-07/containers/prove-containers-proxy-ca.sh) differs from the CI script only in that build command, the COMMAND line and the repo-root path. The script's own `DIRTY yes` line counts untracked evidence files; capture.sh lists only docs/progress.md and docs/progress.tsv. |

## Commands

```text
devnet-registry.txt
COMMAND go test -count=1 -timeout 900s -v -run TestDevnetRegistryDemo ./chain/devnet/
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
devnet-regress.txt
COMMAND go test -count=1 -timeout 900s -v -run TestDevnetValidatorLifecycle\|TestFourValidatorsCommitRestartAndQuorum\|TestInitRefusesToReplaceGenesis ./chain/devnet/
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
race-chain.txt
COMMAND go test -race -count=1 -timeout 20m ./chain/...
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
unit-app.txt
COMMAND env -C chain go test -count=1 -timeout 900s -v ./app/...
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
unit-providers.txt
COMMAND env -C chain go test -count=1 -v ./x/providers/...
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
unit-content.txt
COMMAND env -C chain go test -count=1 -v ./x/content/...
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
unit-authority.txt
COMMAND env -C chain go test -count=1 -v ./x/authority/...
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
unit-msgkit.txt
COMMAND env -C chain go test -count=1 -v ./msgkit/...
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
unit-node.txt
COMMAND env -C chain go test -count=1 -v ./node/...
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
unit-svnerr.txt
COMMAND env -C chain go test -count=1 -v ./svnerr/...
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
fuzz-FuzzStateTransitions.txt
COMMAND bash -c echo\ \"loadavg\ before:\ \$\(cat\ /proc/loadavg\)\"\;\ env\ -C\ chain\ go\ test\ -run=\'\^\$\'\ -fuzz=\'\^FuzzStateTransitions\$\'\ -fuzztime=90s\ ./x/authority/\;\ s=\$\?\;\ echo\ \"loadavg\ after:\ \$\(cat\ /proc/loadavg\)\"\;\ echo\ \"new\ or\ changed\ files\ under\ chain/\ after\ the\ run:\"\;\ git\ status\ --porcelain\ --untracked-files=all\ --\ chain\;\ echo\ \"\(end\ of\ list\)\"\;\ exit\ \$s
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
fuzz-FuzzTypedEventParse.txt
COMMAND bash -c echo\ \"loadavg\ before:\ \$\(cat\ /proc/loadavg\)\"\;\ env\ -C\ chain\ go\ test\ -run=\'\^\$\'\ -fuzz=\'\^FuzzTypedEventParse\$\'\ -fuzztime=30s\ ./msgkit/\;\ s=\$\?\;\ echo\ \"loadavg\ after:\ \$\(cat\ /proc/loadavg\)\"\;\ echo\ \"new\ or\ changed\ files\ under\ chain/\ after\ the\ run:\"\;\ git\ status\ --porcelain\ --untracked-files=all\ --\ chain\;\ echo\ \"\(end\ of\ list\)\"\;\ exit\ \$s
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
innersig-decode-base.txt
COMMAND bash -c git\ archive\ 3853e3430217096c3e65664bc799859942ea451e\ chain\ proto\ \|\ tar\ -x\ -C\ /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-07/decode-base2-fS9E\ \&\&\ cp\ docs/evidence/goal-07/innersig_decode_probe_test.go.txt\ /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-07/decode-base2-fS9E/chain/app/zz_probe_innersig_test.go\ \&\&\ cd\ /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-07/decode-base2-fS9E/chain\ \&\&\ go\ test\ -count=1\ -v\ -run\ \'\^TestProbeRotateDecode\$\'\ ./app/
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:0
```

```text
mutant-m01-simulate-extra-gas.txt
COMMAND bash docs/evidence/goal-07/mutants/run_mutant.sh m01-simulate-extra-gas
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:1
```

```text
go-mod-tidy-diff.txt
COMMAND env -C chain go mod tidy -diff
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:1
```

```text
golangci-lint.txt
COMMAND bash -c golangci-lint\ version\;\ cd\ chain\ \&\&\ golangci-lint\ run\ ./...
TREE 8d7bec054dc44e2e58b891452dae1c989998ffc6
EXIT:3
```

The other commands are the first line of each log.

## Blockers

- App-level genesis export and re-import: DR-07-02 and DR-07-03. No test performs it, because it cannot pass today: InitChain refuses `App.Export()` of a chain holding a provider. `TestLaunchGenesisRejectsNonEmptyCatalog` asserts the refusal and passes in the full `./app/...` run of [unit-app.txt](logs/goal-07/unit-app.txt). To rerun it alone: `env -C chain go test -count=1 -v -run '^TestLaunchGenesisRejectsNonEmptyCatalog$' ./app/`. Once the decisions are made, a test must InitChain a fresh app from that export and compare the full state.
- REQ-HOST-004: DEC-002 membership policy.
- REQ-TX-002 for production: DEC-001 custody and the hosting-intent record (DR-07-01).
- Head CI: needs a pull request, which needs owner authorization. After it opens, record the runs for `43c1a10` or its successor with `gh api "repos/Maxencejules/shortvideo-network/actions/runs?head_sha=<sha>" --jq ".total_count"` and the per-job results (the query of [ci-head.txt](logs/goal-07/ci-head.txt)).
- CI `devnet` job: the unchanged `bash scripts/devnet/prove-containers.sh` fails here at the image build (proxy CA not trusted inside the build container). The equivalent run with only the build step changed passed. The unchanged job still has to pass in head CI.
- golangci-lint: `bash -c 'golangci-lint version; cd chain && golangci-lint run ./...'` needs a golangci-lint built with Go 1.26 or later.

## Decision requests

Interim choices are reversible. They are not approvals.

- DR-07-01 Hosting-intent record (DEC-001, security-critical). v0 has no intent schema, and `InnerSignature` has no role field. Interim, devnet only: the role name is inside the signed payload, verified against the key in the message. That proves integrity, not creator authorization. No chain id is in the payload. The withdraw intent is not bound to the register-intent key. Non-devnet chains are DECISION_BLOCKED. Owner to decide the schema, key custody, chain binding and key binding.
- DR-07-02 Genesis catalogue. The launch genesis keeps providers, content and tombstones empty. May a reviewed launch genesis seed providers? Seeding ADMITTED providers bypasses council admission (DEC-009). May InitChain import an exported non-empty catalogue as a restart path? Goal 10 owns restarts.
- DR-07-03 Business ids are not in the v0 genesis schema, so an export drops replay protection (REQ-INV-003). Change the protocol to export them, or forbid restart from export.
- DR-07-04 New encodings. The goal text allows a new encoding as a new package version. v0 fixes `content_version` at 1, so a new encoding is a new `content_id`. Interim: the v0 rule, with the store keyed by (id, version).
- DR-07-05 Renewals after disablement. v0 `op_renew` has no provider-state check, so a DISABLED or DRAINING provider can renew existing leases. Keep or refuse? Goal 08 implements the answer.
- DR-07-06 Key separation. Should a provider signing key equal to a consensus key, a council key or another provider's key be rejected? Interim: not checked.
- DR-07-07 Content listing. v0 has only `Content` by (id, version). Add a paginated RPC, or leave listing to the goal 12 indexer?
- DR-07-08 RegisterProvider digest and proof of possession. The business digest does not cover the signer, so a copy sent by another account would match it. Interim: such a copy is FORBIDDEN. Should the digest cover the signer, and should registration carry a proof-of-possession signature by `signing_pubkey`? Both are protocol changes.
- DR-07-09 `GET /capability` signed envelope. `capability.Sign` and `capability.Verify` define a detached `InnerSignature` over `SVN-CAPABILITY-1`, and `Verify` binds the manifest to the provider record (id, current key, `endpoint_manifest_digest`). The wire envelope that carries the manifest and its signature is not in v0.
- DR-07-10 Bounds for provider history and queries (PROPOSED).
  - (a) `MaxRetiredKeysPerProvider` = 1024. A rotation past it is BOUNDS, after every spec check. Security consequence: under a capped provider id the provider can no longer rotate away from a compromised signing key. The remedies are drain, council disable, and a new provider id that needs council admission again. `TestRotationCap` shows update and drain still work at the bound. Alternative for the owner: pruning of old keys instead of a cap.
  - (b) Node default query gas limit `app.DefaultQueryGasLimit` = 20,000,000, equal to the transaction gas cap. It is configurable with `query_gas_limit` in `node.json`. Absent or 0 selects the default, never unlimited. Any positive value is accepted, including 2^64-1, which is the SDK's own unlimited default (`TestQueryGasLimitOption`, case `max`); the node does not cap an operator's value. It is not consensus state. It applies to gRPC-routed queries only: ABCI query on a registered service path, and the node gRPC server.
  - (c) Raw store queries (`abci_query` on `/store/<store>/key` or `/store/<store>/subspace`) are not metered or paged by the SDK. A subspace query returns every row under a prefix and grows with the whole registry. `svnd` does not filter them. Operators must not expose the CometBFT RPC port publicly. Rejecting `/store` paths in the node is a follow-up.

## Follow-ups

- `chain/msgkit/queryerr.go` imports `google.golang.org/genproto/googleapis/rpc`, which `chain/go.mod` still marks `// indirect`. It builds because the version is pinned. `go mod tidy -diff` on head moves that module and `spf13/cobra` to the direct block ([go-mod-tidy-diff.txt](logs/goal-07/go-mod-tidy-diff.txt)). The cobra drift already exists at the base, where tidy also drops the unused `go.opentelemetry.io/contrib/bridges/otelslog` ([go-mod-tidy-diff-base.txt](logs/goal-07/go-mod-tidy-diff-base.txt)); the head diff no longer lists `otelslog`. Goal 07 did not change `go.mod` or `go.sum` ([gomod-unchanged.txt](logs/goal-07/gomod-unchanged.txt)). Tidy in a separate dependency change.
- Reject raw `/store` ABCI query paths in the node, or document the CometBFT RPC exposure rule in the operator runbook (DR-07-10 (c)).
- Two code comments overstate the query gas limit. `chain/app/app.go` says "Every query runs on a finite gas meter", and `chain/node/start.go` says `QueryGasLimit` is the gas "one ABCI or gRPC query may consume". Raw `/store` ABCI queries are not metered, and an operator may set 2^64-1, the SDK's unlimited value. `chain/README.md` also says gRPC-routed queries run "on a finite gas meter" without that caveat. Fix them in the next code change; this evidence does not change the tested tree.
- Run CI's `devnet` job (`bash scripts/devnet/prove-containers.sh`) unchanged on this branch in head CI. Optionally let the Dockerfile accept an extra CA bundle as a BuildKit secret so the job can also run behind a TLS-intercepting proxy (a packaging change for a later goal).
- Add a test that drives the node gRPC server's out-of-gas path. `TestQueryGasLimit` covers the ABCI query and the query router only.
- Map unknown-field decode failures to `ERROR_CODE_UNKNOWN_FIELD` (8), or record that sdk/2 is the rule.
- Leases and evidence need a store layout version like the registries' when they become real modules in goals 08-09.
- `svnd` has no query subcommands. Queries go through ABCI query and gRPC.
- OfferLease and AcceptLease must call `CheckNewObligation`, `CheckOfferable` and `CheckNotWithdrawn` in goal 08.
- The verbose app log is 5.6 MB of SDK debug output. A quieter test logger would shrink it.
- Tell the goal-06 reviewer about the InnerSignature and simulation defects.
- Owner acknowledgement of the model and effort substitution.

## Residual risks

- Opaque ids and digests can still be linkable to a person or a host by anyone who holds the preimage. Hashing is not anonymization.
- Id-shaped submitter text is stored verbatim. A username placed in `content_id` is stored (`TestSubmitterTextFields`). A transaction a handler rejects is still in its block. Only decode and ante rejections keep bytes out of blocks an honest proposer builds.
- The hosting intent proves payload integrity under the key in the message. It does not prove that a creator authorized the host (DR-07-01).
- Registration has no proof of possession of `signing_pubkey`. A front-runner can register another provider's key and manifest digest under its own account. The council sees the `account` before it admits (DR-07-08).
- There is no per-account limit on providers or content. DEC-005 allowances are unset. Gas meters every write. Export as a whole grows with the number of providers.
- A provider at the retired-key cap cannot rotate away from a compromised key (DR-07-10 (a)).
- Raw `/store` ABCI queries are unmetered and unpaged, and a subspace query grows with the whole registry. The CometBFT RPC port must not be public (DR-07-10 (c)).
- No test drives the node gRPC server's out-of-gas path.
- CAPACITY and OVERFLOW are tested only at keeper level until goal 08.
- Events are not part of the app hash. An indexer must not treat them as consensus state.
- The devnet is four processes on one machine. It is not an independent-operator result.

## Changed files

[diff-stat.txt](logs/goal-07/diff-stat.txt): 132 files changed, 22482 insertions, 430 deletions, base `3853e34` to `43c1a10`.

- New modules: `chain/x/providers` (with `capability`), `chain/x/content`, `chain/msgkit` (with `msgkittest`).
- `chain/svnerr`: codes 11-13 and a registry test.
- `chain/x/authority`: council hooks into the registries, a read surface, one business-id namespace, the simulation fix; provider records moved out.
- `chain/app`: wiring, `InnerSignature` registration, strict decoder, envelope ante rule, query gas limit, scaffold-layout refusal, snapshots, new test files and a golden event fixture.
- `chain/node`: `query_gas_limit` in `node.json`.
- `chain/devnet`: provider and service accounts, the DEC-003 parameter, `TestDevnetRegistryDemo`.
- Docs: `docs/architecture/registries.md`, module READMEs, `chain/README.md`, operations notes. `scripts/evidence/capture.sh`.
- No change to `.proto` files, `bounds.json`, goal specs, `chain/go.mod`, `chain/go.sum` or `.github/workflows/ci.yml`.

Evidence added by this run: `docs/evidence/goal-07.md`, `docs/evidence/goal-07/innersig_decode_probe_test.go.txt`, `docs/evidence/goal-07/mutants/`, `docs/evidence/logs/goal-07/`, and the goal 07 rows in `docs/progress.md` and `docs/progress.tsv`.

## Next safe action

The owner answers DR-07-02, DR-07-03 and DR-07-10, or accepts the module-level round trip and the proposed bounds for now, and authorizes a draft pull request for `goal-07` against `goal-06` so CI runs. Goal 08 can start on the handoff APIs once goal 06 and goal 07 are reviewed. Do not treat devnet seats or devnet operator groups as production policy.

## Proposed pull request

Title: Goal 07: provider and content registries (x/providers, x/content)

Review summary:

- Adds x/providers and x/content on the frozen v0 protos: ownership checks, digest and size bounds, one business-id namespace with replay protection, a capped retired-key history, withdrawal tombstones, paginated provider queries and typed events.
- Admission, disablement and forced withdrawal stay council-only through x/authority (DEC-009). Production content registration is DECISION_BLOCKED until DEC-001.
- Sets a default query gas limit of 20,000,000 for gRPC-routed queries; 0 or absent no longer means unlimited. Raw `/store` queries stay unmetered; the CometBFT RPC port must not be public.
- Fixes two goal-06 defects: `InnerSignature` was not registered, so rotation could not be submitted, and simulation of authority messages always failed.
- Evidence on tree `8d7bec05`: unit suites, a four-process single-machine devnet demo with a replayed app hash, `-race` for `./chain/...`, ten real fuzz runs, five mutants, Python and Node gates, both contract checkers.
- Status BLOCKED: app-level genesis round trip (DR-07-02, DR-07-03), REQ-HOST-004 (DEC-002), production REQ-TX-002 (DEC-001), head CI not run (the unchanged container devnet job fails here at the image build; a copy with only the build step changed passed), golangci-lint unusable here. Ten decision requests are open.
