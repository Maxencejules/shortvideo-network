# Goal 07 evidence

Status: BLOCKED

Every check cited in this file ran on commit `b782e0149e1a70758b81b29929d12eab0da8a006`, tree `f07ac7f6113f45a97167cf9c8139fb712227490e`. That commit is goal 07 merged with goal 06 round 3, which `main` holds as `f5916bd` (see [Tested revision](#tested-revision)). Each log under `logs/goal-07/` whose name starts with `m-` was written by `scripts/evidence/capture.sh` and prints that commit, that tree and `DIRTY no`, with no `DIRTY_PATH` line. Four probes ran on archives of other commits, as their commands show: `m-innersig-decode-base.txt` and `m-go-mod-tidy-diff-base.txt` (round 2 head `3853e34`), `m-innersig-decode-main.txt` and `m-go-mod-tidy-diff-main.txt` (`main` at `f5916bd`). The logs of the previous run, on commit `43c1a10`, are superseded and are listed only under [Superseded logs](#superseded-logs).

This file reads the decision register the way [goal-06.md](goal-06.md) does. Gate A approved DEC-001 through DEC-012 with their PROPOSED defaults ([decision-register.md](../decision-register.md)). [requirements.md](../requirements.md) says: "The 34 BLOCKER rows are later goals that still have to carry out those answers. They are not open questions." So an item stays BLOCKED here when it needs a value, a schema or a protocol change that no approved default supplies. This reading comes from the register and the requirements matrix. It is not a new owner ruling, and the owner can reject it in review. The three items the previous run marked BLOCKED (two requirement rows and one acceptance check):

1. **REQ-HOST-004 (BLOCKER DEC-002): satisfied under the approved default, VERIFIED for that default.** The approved DEC-002 default keeps admission permissioned and makes a reviewed operator registry the beta mechanism. Its Blocks section says "Goal 07 can still define an admission record whose membership policy stays blocked". The requirement text is "The beta keeps a reviewed operator registry. Membership policy is unset." Goal 07 defines that record and keeps the membership policy blocked. Admission is a council payload only (DEC-009). Off the devnet it is `ERROR_CODE_DECISION_BLOCKED`, and on the devnet only operator group `devnet-test-only` can be admitted. No operator list exists, and no operator is called independent. Naming real operators stays blocked by DEC-002; it is not a goal 07 task. Tests are under [Requirements rows](#requirements-rows-for-goal-07).
2. **REQ-TX-002 for production (DEC-001): needs values the default leaves unset, so it stays BLOCKED.** The approved DEC-001 default accepts disclosed host custody: the account host holds a delegated signing key. Protocol v0 carries that answer as follows ([protocol.md](../protocol.md), "Decision parameters"): `ACCOUNT_HOST_DELEGATE` is rejected, and only the labelled devnet role `DEVNET_TEST_ONLY` can pass, and only with the devnet flag. Every on-chain decision parameter stays `approved` false, and a transition that sets it to true is rejected. DEC-001's Blocks section also says goal 02 must not freeze the account host as the owner of hosting-intent signatures. A production intent therefore needs a hosting-intent record that neither the default nor v0 defines: its schema, the key that speaks for a creator, a chain binding, and a binding of the withdraw intent to the register intent. It also needs an approved on-chain DEC-001 value. This is decision request DR-07-01, which is security-critical.
3. **App-level genesis round trip of committed registry state: needs an owner decision, so it stays BLOCKED.** Empty content, leases and offers at genesis is a protocol v0 rule (`spec_machine.py` `validate_genesis_view`, REQ-GEN-002). An empty provider list is not: v0 `svn.app.v1.AppGenesis` carries `providers` ([protocol.md](../protocol.md), "Genesis"). This file reads approved DEC-009 (council-only admission) as excluding a genesis that seeds `ADMITTED` providers, because that would bypass council admission. The code goes further: InitChain and `App.Export` refuse any genesis provider, in any status (`emptyCatalog` in `chain/app/state.go`; code reading, no capture log). Refusing every genesis provider is goal 07's interim under DR-07-02 and is stricter than v0 ([docs/architecture/registries.md](../architecture/registries.md) says the same). The refusals are tested. A fresh chain round-trips through the app byte for byte, and committed registry state round-trips at module level (see [Acceptance](#acceptance)). A restart of committed registry state from an export needs fields that the frozen v0 genesis schema does not have: provider and content business ids (DR-07-03; an import without them loses replay protection) and the authority side state (DR-06-01). No approved default makes that protocol change or rules restart from export unsupported. DR-07-02 records that goal 10 owns restarts. Goal 06 did not count DR-06-01 as blocking: [goal-06.md](goal-06.md) says it records "current behaviour that is implemented and tested", and goal 06's acceptance list names no genesis round trip. Goal 07's acceptance list names one ("Round-trip genesis"), so the committed-state half is BLOCKED here.

Head CI passed. On 2026-11-02, at about 11:34 UTC, the owner authorized a draft pull request for `goal-07` against `main` (not its merge). CI run [41438312010](https://github.com/Maxencejules/shortvideo-network/actions/runs/41438312010) on the evidence commit `45ac279` (draft [PR 10](https://github.com/Maxencejules/shortvideo-network/pull/10)): `node`, `python`, `go`, `devnet` and `contract` all `success`. `45ac279` changes only `docs/` from `b782e01`, so the code CI tested is tree `f07ac7f6`'s. Read from PR 10's check runs through the GitHub API at about 11:51 UTC on 2026-11-02; not a capture log. That `devnet` job ran the unchanged `scripts/devnet/prove-containers.sh`. CI's `devnet` job cannot run unchanged here: its image build fails TLS inside the build container ([m-containers-unchanged.txt](logs/goal-07/m-containers-unchanged.txt), BLOCKED, environment). A labelled copy whose only change is the image build passed ([m-containers-labelled-copy.txt](logs/goal-07/m-containers-labelled-copy.txt)). golangci-lint is BLOCKED here (built with go1.25.1); it is not a repository gate. Goal 06's mutation runner is BLOCKED on this tree: five of its anchors target code the merge deleted, so it refuses before running any test ([m-goal06-anchors.txt](logs/goal-07/m-goal06-anchors.txt); see [Gates](#gates)). It is not a goal 07 acceptance check, but the merge changed the authority code its other 114 mutants target. The secret scan and both contract checkers ran on the committed evidence in that CI run (`python` and `contract` jobs), not through `capture.sh` (see [Gates](#gates)).

So goal 07 is BLOCKED on two acceptance or requirement items (the BLOCKED gates above are listed under [Blockers](#blockers) too): REQ-TX-002 for production (DR-07-01), the app-level genesis round trip of committed registry state (DR-07-02, DR-07-03, DR-06-01). Head CI passed on the evidence commit. Every other acceptance check and requirement row is VERIFIED, except:

- the lease half of REQ-INV-006, DEFERRED to goal 08;
- the handoff of package identity and provider admission APIs, IMPLEMENTED, because goal 08 is the consumer;
- "No test removed since the base", IMPLEMENTED with a recorded deviation: five tests were replaced by design ([m-tests-replaced.txt](logs/goal-07/m-tests-replaced.txt)), and the no-removal check itself exits 1;
- the query gas limit in [Bounds, query gas and simulation](#bounds-query-gas-and-simulation-fix-round), whose node gRPC server path is untested.

DR-07-04, DR-07-08, DR-07-10 and DR-07-11 record implemented and tested behaviour or PROPOSED numbers. DR-07-05 (goal 08), DR-07-06 (key separation is not checked; no test), DR-07-07 (no content list) and DR-07-09 (no wire envelope) ask about checks or features that goal 07's acceptance list does not require. None of them blocks on its own. DR-07-11 is security-critical. Its interim rule fails closed and is tested, but the owner still has to answer it.

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit and not a production network. The devnet is four processes on one machine. No independent operator is claimed.

## Model and effort

- Playbook assignment: GPT-6 Astra, effort HIGH. GPT-6 Astra was not used.
- Model that ran: Claude Code, model `claude-opus-5-5`, in remote session `session_014mwakeb27YrugDfwAx8vXL`. Workflow subagents inherit the session model. The goal 07 commits `26454c1` to `ffd2ce9`, the merge `70d55de` and `b782e01` each carry the `Claude-Session` trailer for that session (read with `git log` by this evidence writer, not a capture log).
- Source: claude-code-remote `get_session`, which this evidence writer read at about 04:49 UTC on 2026-11-02. It reported:
  - `configured_model`, `session_context.model` and `external_metadata.last_served_model`: all `claude-opus-5-5`;
  - effort: `session_context.effort_level` = `ultracode`, and `external_metadata.is_ultracode_active` = true. That is not the playbook's HIGH;
  - `external_metadata.flag_settings.fastMode` = true. This file records that flag and does not interpret it.
- The final verifier read `get_session` again at about 05:38 UTC on 2026-11-02. It returned the same model, effort and `fastMode` values, and the pull-request question and DR-07-11 still pending. The owner answered both at about 11:34 UTC (see [Decision requests](#decision-requests) and [Head CI](#gates)).
- Limits of that source. It is a tool response, not a `capture.sh` log. It describes the session's settings and its latest turn. This evidence did not scan the full event history for model-fallback notices.
- AGENTS.md's substitution note names Cursor cloud agents. This run is Claude Code, a further substitution.
- Owner acknowledgement for goals 07-30: RESOLVED. As recorded in the orchestrator notes of the goal 06 fix round:
  - On 2026-11-01, received by this session at about 16:33 UTC, the owner wrote in this Claude Code session: "I acknowledge the model substitution for DR-06-07".
  - In the next message, received at about 16:34 UTC on 2026-11-01, the owner wrote: "extend the acknowledgement to goals 07 through 30".
- So goal 07 is not BLOCKED on model allocation. The ruling covers model allocation only. It does not certify correctness, and extra effort does not certify correctness either.

## Tested revision

| Item | Value | Log |
| --- | --- | --- |
| Commit | `b782e0149e1a70758b81b29929d12eab0da8a006` | [m-revision.txt](logs/goal-07/m-revision.txt) |
| Tree | `f07ac7f6113f45a97167cf9c8139fb712227490e` | every `m-` log prints it |
| Parents | `HEAD^1` `70d55dec2db2b6ae1b8a9e0611521bfe69e76cea`, the merge of goal 06 round 3 (`0936332`) into goal 07 (`ffd2ce9`). `HEAD^2` `f5916bd02ce49efa2481dd3a7749a00b2fe6f67e`, `main`. `b782e01` records the squash merge of goal 06 as merged and changes no file: `70d55de` has the same tree, `f07ac7f6` (read with `git rev-parse` by this evidence writer, not a capture log). | [m-revision.txt](logs/goal-07/m-revision.txt) |
| Goal 06 | Merged. PR 9 was squash-merged into `main` as `f5916bd`. Its tree equals the goal 06 round 3 head `0936332`: both are `ba701bb9e5234ba6172ca7674e82714a97cce4b7`. `f5916bd`, `0936332` and `43c1a10` are all ancestors of `HEAD`. | [m-stack.txt](logs/goal-07/m-stack.txt) |
| Branch | `goal-07`, pushed: `origin` `refs/heads/goal-07` is `b782e01`. `main` is `f5916bd`. | [m-stack.txt](logs/goal-07/m-stack.txt), [m-revision.txt](logs/goal-07/m-revision.txt) (`main-is-ancestor`, branch `goal-07`) |
| Runtime | Ubuntu 24.04.5 LTS, Linux 6.18.44, 4 CPUs, 15 GiB. Go 1.26.10. Python 3.12.3, ruff 0.17.3, mypy 2.4.1. Node v22.23.4, npm 10.9.9. libprotoc 36.2. Docker server 29.9.2 (local dockerd; used only for the container proof). The process devnet and Go test commands that start validator processes (`m-devnet-registry.txt`, `m-devnet-regress.txt`, `m-devnet.txt`, `m-race.txt` and `m-tests-go.txt`) ran inside a loopback-only network namespace through `netns-run.sh`, a scratch helper (`unshare --net`, `lo` up, no outbound network). The helper is not in the repository; without it the commands are the same `go test` lines. The container proof ran outside that namespace, through the local dockerd with host networking: [m-containers-labelled-copy.txt](logs/goal-07/m-containers-labelled-copy.txt) starts four validator containers and prints `NOTE bridge networking is BLOCKED. This run uses host networking.`, and its image build runs `docker build --network host` with the session proxy as build arguments ([prove-containers-proxy-ca.sh](goal-07/containers/prove-containers-proxy-ca.sh), lines 72-73). [m-containers-unchanged.txt](logs/goal-07/m-containers-unchanged.txt) also ran outside it; its image build failed before any node started. | [m-runtime.txt](logs/goal-07/m-runtime.txt), [m-docker-version.txt](logs/goal-07/m-docker-version.txt), [m-docker-info.txt](logs/goal-07/m-docker-info.txt) (`docker info` server version `29.9.2`) |

Branch history since `main` ([m-revision.txt](logs/goal-07/m-revision.txt)):

- `f38c6ef`, `54503ac`, `a6a5273`, `4c87282` and `3853e34` are goal 06 rounds 1-2. Goal 07 started from `3853e34`. `main` holds them only as the squash `f5916bd`, so `HEAD` reaches them and `main` does not.
- `26454c1` adds `scripts/evidence/capture.sh`.
- `9ca725e`, `4ba4168`, `5213a7c`, `b4282cb` and `a88453e` are labelled WIP checkpoints. They are untested and were pushed only to preserve work.
- `2e0888d` is SUPERSEDED. A second review round found that it did not bound one provider's retired-key history.
- `43c1a10` fixed the second-round findings and was the previous tested revision. `ffd2ce9` holds its evidence. Those logs are superseded.
- `2389948`, `fb4cde2`, `c68113c`, `90652c6` and `0936332` are goal 06 round 3.
- `70d55de` merges goal 06 round 3 into goal 07. `b782e01` records `main` `f5916bd` as merged.

Every `m-` log ends with its exit code. 62 of the 74 end `EXIT:0`. Twelve end non-zero, and each one is expected and explained below:

- the mutant runs `m-mutant-m01` to `m04` (exit 1, killed);
- `m-mutant-m05` (exit 1: a build failure, not a kill; see [Mutation checks](#mutation-checks));
- `m-tests-not-removed.txt` and `m-tests-not-removed-43c1a10.txt` (exit 1: replaced tests);
- `m-gomod-vs-main.txt` (exit 1: the merge tidied `go.mod`);
- `m-go-mod-tidy-diff-base.txt` (exit 1: round 2 base, for comparison);
- `m-goal06-anchors.txt` (exit 1: five goal 06 mutants target code the merge deletes);
- `m-containers-unchanged.txt` (exit 1, BLOCKED, environment; see [Gates](#gates));
- `m-golangci.txt` (exit 3, BLOCKED).

73 of the logs ran from 04:07 to 04:43 UTC on 2026-11-02 (their `DATE` lines). At that time `git status` listed only untracked logs, the new runner `docs/evidence/goal-07/mutants.py` and the `PY_ALLOW` line in `docs/evidence/goal-01/check_requirements.py`, all under `docs/evidence/`. So each of them prints `DIRTY no`. They ran before this file and the progress rows were edited.

The 74th, [m-mutant-tx-context-full.txt](logs/goal-07/m-mutant-tx-context-full.txt), was added at 05:23 UTC by the evidence fixer, to settle what the 300-character `failing=` field of `m-mutants-merge.txt` cuts off (see [Mutation checks](#mutation-checks)). This file had been edited by then; it is under `docs/evidence/`. The edited progress rows were set aside for that run (`git checkout` of `docs/progress.md` and `docs/progress.tsv`) and restored byte for byte after it, so that log also prints `DIRTY no`. The runner tests a `git archive` of `HEAD`, not the working tree.

[m-unit-app.txt](logs/goal-07/m-unit-app.txt) is 7.4 MB and [m-unit.txt](logs/goal-07/m-unit.txt) is 7.6 MB. Verbose app tests print the SDK debug logger. The size is the real output.

### Merge of goal 06 round 3

Goal 06 round 3 redesigned x/authority after goal 07 had built on round 2 (`3853e34`). The merge `70d55de` rebuilds goal 07's registry integration on the round 3 design. The summary below comes from the merge workflow's integration results: a plan, a resolution, three independent reviews and a fix round. Those are workflow outputs, not capture logs. Each test named here passes in [m-unit.txt](logs/goal-07/m-unit.txt), and the mutants named here are in [m-mutants-merge.txt](logs/goal-07/m-mutants-merge.txt).

What the merge kept from goal 06 round 3:

- the per-object authority store `0x01`-`0x16`;
- the export refusal (DR-06-01);
- `ValidateAuthorityGenesis` at InitChain and in `svnd`;
- history-independent council gas (`TestCouncilGasDoesNotGrowWithHistory`, [m-unit-authority.txt](logs/goal-07/m-unit-authority.txt));
- the 37-row full-store reject table `TestRejectedMessagesLeaveTheFullAuthorityState`;
- the devnet stall check.

What it changed in goal 07:

- **x/authority.**
  - The round-2 side document and the authority provider rows are gone. The `0x17` prefix is reserved, never written, and refused by export. `TestExportStateRefusesEachPrefix` covers `providerPrefix` (`0x17`), and `TestMessageKeySets` asserts that no authority key under `0x17` is read or written ([m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)).
  - Proposal records gain `content_id` and `content_version`.
  - The registry hooks are bound to a cache nested inside the message branch, and that cache is written only on success. `TestKeeperRegistryForbiddenKeepsTheRejection` and `TestKeeperRegistryHookCommitsOnlyOnSuccess` pass ([m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)). Mutants `msg-keep-writes-registry`, `msg-hooks-on-message-branch`, `msg-hooks-on-tx-context` and `msg-success-skips-registry-write` are killed (killing tests under [Merge mutants](#merge-mutants)).
  - Business ids form one namespace across modules, through `msgkit.Peers`. A peer value that is not 32 bytes is `ERROR_CODE_INTEGRITY`. Mutants `peer-integrity-to-replay` and `peer-empty-is-free` are killed.
  - The read surface reads one key per call. `GovernedLimit` and `BusinessDigest` panic on a damaged value rather than report a ceiling or a free id.
  - Simulation skips only the meter-limit check. Mutant `msg-simulate-skip-removed` is killed.
- **Router.** Goal 06's D1 (provider messages `ERROR_CODE_FORBIDDEN` at the router until goal 07) is lifted for goal 07's six messages only. Lease and evidence messages stay FORBIDDEN. `TestEveryProtocolMessageDecodes` logs `19 transactions, 10 nested message types` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)).
- **Genesis.** A council seat address may be a submitter only as `BROADCASTER`, in both `genesis.Validate` and the authority `ValidateGenesisState`. This is the rule of DR-07-11, which the owner kept on 2026-11-02. Five mutants of it are killed: `doc-seat-rule-off`, `auth-seat-rule-off`, `auth-seat-rule-allows-auditor`, `doc-seat-rule-case-sensitive` and `doc-seat-address-wrong`.
- **Export.** `App.Export` is goal 06's refusing export. It now also refuses a non-empty provider or content catalogue through `DocumentFromModules`. Mutant `export-no-catalogue-check` is killed.
- **Content.** Mutant `content-accepts-broadcaster` is killed, by `TestRegistryMessagesRejectOtherRoles`.
- **`chain/go.mod`** is tidied: `google.golang.org/genproto/googleapis/rpc` moves to the direct block, and `go.opentelemetry.io/contrib/bridges/otelslog v0.19.0 // indirect` is added. No version changes, and `go.sum` is unchanged ([m-gomod-vs-main.txt](logs/goal-07/m-gomod-vs-main.txt)). `go mod tidy -diff` is now empty ([m-go-mod-tidy-diff.txt](logs/goal-07/m-go-mod-tidy-diff.txt)).
- **Devnet.** `devnet/init.go` takes goal 06 as its base and adds goal 07's provider and service accounts and the DEC-003 parameter.

New tests:

- `TestRegistryMessagesRejectOtherRoles`: 39 rows, asserted by `rows != 39` (`chain/app/registry_roles_test.go`). Each row is a signer with no right to the message: two council seats, a non-seat broadcaster, the authority account, an auditor, the service account on provider messages, and provider-2. Each row asserts exact `ERROR_CODE_FORBIDDEN`, that the ante committed, an unchanged `FullSnapshot`, and no typed event.
- `TestSeatAddressIsBroadcasterOnly`: 8 subtests, on the document path and InitChain. The InitChain subtests also assert the exact `app.ValidateAuthorityGenesis` error (`chain/app/registry_roles_test.go`).
- `TestSeatAddressRole`: 11 subtests, including upper-case addresses and two flag-off rows.
- `TestSeatAddressMatchesGenesis`, and new rows in `TestValidateGenesisState`.
- `TestExportRefusesANonEmptyCatalogue`: subtests `provider_only` and `content_only`.
- `TestKeeperMalformedPeerBusinessDigestIsIntegrity`: subtests `31_bytes`, `33_bytes`, `empty` and `nil`, through the keeper, with the store compared byte for byte.
- `TestKeeperRegistryWriteDroppedOnLateFailure`, `TestProviderMessagesReachTheRegistryAndDecode` and `TestKeeperForceWithdrawArgumentsSurviveTheRecord`.

`TestRejectedRegistryMessagesLeaveTheFullState` now asserts its row count, 41 (`rows != 41`, `chain/app/registry_reject_test.go`).

Extended tests (new rows in existing tests; each passes in [m-unit.txt](logs/goal-07/m-unit.txt), and the devnet one also in [m-devnet.txt](logs/goal-07/m-devnet.txt)):

- `TestExternalBusinessIDIsReplay` gained the engine-level subtest `malformed_peer_value_is_integrity`: a short and an empty peer value are INTEGRITY with the store unchanged ([m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)).
- `TestDevnetRoleRule` gained rows for DEC-004 (`approved_policy` set), DEC-005 (wrong label), DEC-001 and an empty id.
- `TestKeeperReadSurface/governed_limit` gained damage rows: a zero value, 9 bytes, and a value under an unknown limit key. Each must panic.
- `TestRejectedMessagesLeaveTheFullAuthorityState` (`chain/app/authority_flow_test.go`): its council reject helper now also requires `FullSnapshot` unchanged, so every module store except the account store must stay byte-identical, not only the authority store.
- `TestDevnetKeysAreDistinct` (`chain/devnet/lifecycle_test.go`) now expects `2*devnet.Validators+9` distinct private keys, up from `+6` at `0936332`: five accounts (goal 07 adds `service-2`, `provider-1` and `provider-2`) and four seats.

The source of this list is the merge workflow's resolution notes (a workflow output) and the test diff against `0936332` (code reading, no capture log).

Five tests were replaced by design, because the code they tested is gone ([m-tests-not-removed.txt](logs/goal-07/m-tests-not-removed.txt), [m-tests-not-removed-43c1a10.txt](logs/goal-07/m-tests-not-removed-43c1a10.txt)). [m-tests-replaced.txt](logs/goal-07/m-tests-replaced.txt) finds every replacement:

| Removed | Replacement |
| --- | --- |
| `TestProviderMessagesAreForbiddenAndDecode` (goal 06, D1) | `TestProviderMessagesReachTheRegistryAndDecode` |
| `TestAdmissionAndMixedActivation` | `TestAdmitAndDisableRows`, `TestMixedActivationConflicts` |
| `TestQuorumUsesPowerNotCount` | `TestQuorumModelVectors` (devnet) |
| `TestKeeperLoadsAnOlderSideDocument` | `TestLegacySideDocumentHalts`, `TestOldLayoutConfigIsIntegrity`, `TestAppRefusesScaffoldLayout` |
| `TestKeeperForceWithdrawArgumentsSurviveTheSideDocument` | `TestKeeperForceWithdrawArgumentsSurviveTheRecord` |

Choices the merge made, recorded as reversible implementation choices, not owner approvals:

- the nested registry cache;
- INTEGRITY for a malformed peer business value;
- `GovernedLimit` and `BusinessDigest` fail closed by panicking;
- `0x17` kept as a reserved prefix that export refuses;
- proposal records gain two fields while authority's ConsensusVersion stays 1.

Devnet homes committed on goal 05/06 are refused at start; re-initialize them (see [Store layout and upgrade](#store-layout-and-upgrade)).

The three merge reviews made ten reports: nine distinct findings, one major and eight minor. F1.2 and F3.2 are the same finding. All were fixed before the merge commit; none was rejected. The ids are review number and finding number in the merge workflow's integration results (a workflow output, not a capture log). The `file:line` locations below are on this tree (code reading, no capture log).

| Id | Severity | Finding | Fix (file:line) | Test or log |
| --- | --- | --- | --- | --- |
| F2.1 | major | No genesis rule tied a council seat address to `BROADCASTER`. A seat listed as `ADMITTED_SERVICE` or `ADMITTED_PROVIDER` could register content or a provider and approve its own provider's admission; as `AUTHORITY` it could propose, approve and execute. | `validateSeatRoles` in `chain/genesis/document.go:452` (called at :204) and `chain/x/authority/genesis.go:139` (called at :58). Interim rule, DR-07-11. | `TestSeatAddressIsBroadcasterOnly`, `TestSeatAddressRole`, `TestSeatAddressMatchesGenesis`, new rows of `TestValidateGenesisState` ([m-unit.txt](logs/goal-07/m-unit.txt)); five seat-rule mutants killed ([m-mutants-merge.txt](logs/goal-07/m-mutants-merge.txt)) |
| F1.1 | minor | `registries.md` said `App.Export` produces a document that InitChain refuses; after the merge `App.Export` itself refuses. | `docs/architecture/registries.md:90`, the `App.Export` paragraph citing `TestExportRefusesANonEmptyCatalogue` | `TestExportRefusesANonEmptyCatalogue` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)) |
| F1.2 = F3.2 | minor | No committed test isolated `App.Export`'s catalogue refusal; the only registry-state export ran after council executes, so the authority refusal fired first. | `chain/app/registry_roles_test.go:182`, `TestExportRefusesANonEmptyCatalogue` | Same test, subtests `provider_only` and `content_only` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)); mutant `export-no-catalogue-check` killed |
| F1.3 | minor | The merge dropped a still-true README sentence: the ante rejects an out-of-range gas limit first, so the handler meter check is not observable at app level. | `chain/x/authority/README.md:89` ("the handler check is not observable at app level") | Doc only. `TestEnforceCommitted` and `TestHandlerMeterCheckByMode` cover the check at keeper level ([m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)) |
| F2.2 | minor | No committed test pinned seat and authority signers on the six goal 07 messages; the docs scoped the claim to goal 06 only. | `chain/app/registry_roles_test.go:28`, `TestRegistryMessagesRejectOtherRoles`; `docs/architecture/authority-authorization.md:26` ("No Goal 06 or Goal 07 handler accepts a `BROADCASTER` signer") and :93 (DR-06-06) | `TestRegistryMessagesRejectOtherRoles`, 39 rows ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)) |
| F2.3 | minor | The relay-column deviation (a broadcaster outer signer is FORBIDDEN) was recorded only for `RegisterProvider`. | `chain/x/content/README.md:39`, `docs/architecture/registries.md:30` (section "Who may write"), DR-06-06, and [Recorded deviations](#recorded-deviations) | `TestRegistryMessagesRejectOtherRoles`; mutant `content-accepts-broadcaster` killed |
| F2.4 | minor | The providers README said the council sees the account before it admits; an `AdmitProviderChange` binds only `provider_id`, and propose does not check that the record exists. | `chain/x/providers/README.md:114-116` ("binds only `provider_id`"); DR-07-08 third question; [Residual risks](#residual-risks) | Doc only; no test pins the id-only binding |
| F3.1 | minor | The "No migration" text said a round-2 home "keeps running"; the merged binary refuses every committed goal 05/06 home. | `chain/x/authority/README.md:123` ("refuses every committed goal 05 or goal 06 home") and `docs/operations/validator-lifecycle.md:50` ("refuses every committed Goal 05 or Goal 06 devnet home") | `TestAppRefusesScaffoldLayout` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)) |
| F3.3 | minor | The peer INTEGRITY reject class was tested only at engine level, not through the keeper. | `chain/x/authority/keeper_registry_test.go:399`, `TestKeeperMalformedPeerBusinessDigestIsIntegrity` | Same test, 4 subtests ([m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)); mutants `peer-integrity-to-replay` and `peer-empty-is-free` killed |

## Prerequisites

| Prerequisite | Status | Evidence |
| --- | --- | --- |
| Goal 02 | IMPLEMENTED | [goal-02.md](goal-02.md). Protocol v0 stays the frozen candidate. Since `main`, no file under `docs/protocol/`, `docs/protocol.md`, `docs/goals/` or `.github/workflows/ci.yml` changed ([m-protocol-unchanged.txt](logs/goal-07/m-protocol-unchanged.txt), exit 0, empty diff). Regeneration gives no diff ([m-proto-regen.txt](logs/goal-07/m-proto-regen.txt)). |
| Goal 05 | VERIFIED | [goal-05.md](goal-05.md), [PR 8](https://github.com/Maxencejules/shortvideo-network/pull/8). Its devnet proofs pass on this tree ([m-devnet-regress.txt](logs/goal-07/m-devnet-regress.txt)). |
| Goal 06 | VERIFIED, merged | [goal-06.md](goal-06.md), [PR 9](https://github.com/Maxencejules/shortvideo-network/pull/9), squash-merged as `main` `f5916bd` ([m-stack.txt](logs/goal-07/m-stack.txt)). Goal 07 is merged with it. |
| Signed manifest contract | IMPLEMENTED | The capability validator matches `docs/protocol/schemas/provider-capability.schema.json`: `TestValidatorMatchesSchemaFile`, `TestSchemaKeywordCheck` and `TestSignAndVerify` pass ([m-unit-providers.txt](logs/goal-07/m-unit-providers.txt)). The signed wire envelope for `GET /capability` is not in v0 (DR-07-09). |
| Approved decisions | APPROVED at Gate A | DEC-001, DEC-002, DEC-003, DEC-005 and DEC-009 in [decision-register.md](../decision-register.md). The approved defaults name no operator, no allowance and no intent schema. |

## Acceptance

| Check | Status | Evidence on tree `f07ac7f6` |
| --- | --- | --- |
| Commit valid registration, update and withdrawal transactions on the devnet | VERIFIED, single machine | [m-devnet-registry.txt](logs/goal-07/m-devnet-registry.txt). Code 0 at: register p1 h5, register p2 h7, council admit p1 h13 (`policy_version=2`) and p2 h19, update p1 h21, rotate p1 h22, drain p2 h23, council disable p2 h30, register c1 h31, register c2 h33, withdraw c1 h35, council force-withdraw c2 h42, idempotent replay of c1 h43. The full package run passes the same test, `--- PASS: TestDevnetRegistryDemo (14.71s)` ([m-devnet.txt](logs/goal-07/m-devnet.txt)). |
| Reject cross-owner edits | VERIFIED | Devnet `cross-owner-update` h44 svn/2 and `cross-owner-withdraw` h55 svn/2. App `TestRejectedRegistryMessagesLeaveTheFullState` and `TestRegistryMessagesRejectOtherRoles` (provider-2 against p1 and c1) ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)). |
| Reject unauthorized provider updates | VERIFIED | Devnet `service-updates-provider` h45 svn/2, `stale-rotation` h57 svn/5, `wrong-domain-drain` h58 svn/12. `TestRegistryMessagesRejectOtherRoles` rejects UpdateProvider, RotateProviderKey and DrainProvider from seats, a non-seat broadcaster, the authority account, an auditor and the service account with FORBIDDEN. |
| Reject duplicate versions | VERIFIED | Devnet `duplicate-version-c1` h47 svn/4, after c1 was withdrawn at h35. v0 accepts only `content_version` 1 (DR-07-04). |
| Reject invalid digests | VERIFIED | Devnet `digest-31-bytes` h48 svn/19 and `digest-algorithm-0` h50 svn/7. |
| Reject oversized metadata | VERIFIED | Devnet `provider-id-65-bytes` h51 svn/19 and `package-bytes-cap-plus-1` h53 svn/19. CheckTx `tx-70-KiB` svn/19, not included. |
| Reject replayed requests | VERIFIED | Devnet `replayed-business-id` h54 svn/3 and `second-withdraw` h56 svn/15. The same signed bytes rebroadcast: `check_tx=sdk/32 broadcast="mempool ErrTxInCache" original_height=5 included_again=false`. The same business id with the same payload is idempotent: h43 code 0. App `TestIdempotentRegistryReplay`. One namespace across modules: `TestKeeperBusinessIDsAreOneNamespace` ([m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)). |
| Rejected transactions leave state unchanged | VERIFIED | Devnet `phase=replay-reject ... full_state_unchanged=true` for all 12 committed rejections. App `TestRejectedRegistryMessagesLeaveTheFullState` (41 rows) and `TestRegistryMessagesRejectOtherRoles` (39 rows) compare `FullSnapshot` with `bytes.Equal`. Goal 06's 37-row `TestRejectedMessagesLeaveTheFullAuthorityState` passes on the merged code. Keeper level: 21 subtests of `TestRejectedProviderMessagesLeaveTheStore` ([m-unit-providers.txt](logs/goal-07/m-unit-providers.txt)), 26 of `TestRejectedContentMessagesLeaveTheStore` ([m-unit-content.txt](logs/goal-07/m-unit-content.txt)), plus `TestKeeperMalformedPeerBusinessDigestIsIntegrity` (4 subtests) and `TestKeeperRegistryFailureWritesNothing` (18 subtests) ([m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)). |
| Round-trip genesis, module level | VERIFIED | `TestGenesisRoundTripFromCommittedState` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)) imports the app's module export of committed state into bare module keepers. Also `TestGenesisRoundTrip` and `TestGenesisRoundTripFromCommittedTransitions` in providers ([m-unit-providers.txt](logs/goal-07/m-unit-providers.txt)), and `TestGenesisRoundTrip` in content ([m-unit-content.txt](logs/goal-07/m-unit-content.txt)). |
| Round-trip genesis, app level | VERIFIED for a fresh chain; BLOCKED for committed registry state (DR-07-02, DR-07-03, DR-06-01) | Fresh state: `TestFreshStateExportRoundTrip` (subtests `no_pending_entry` and `genesis_pending_entry`) exports through `App.Export`, imports at InitChain and exports again. Every exported module is byte-equal, the empty provider and content modules included. `TestInitExportAndChainID` passes too. Committed registry state: `App.Export` refuses. `TestExportRefusesANonEmptyCatalogue` (`chain/app/registry_roles_test.go`) asserts these exact errors for a catalogue with no council execute: `export refused: exported genesis does not validate: providers are not empty` and `export refused: exported genesis does not validate: content is not empty`. The strings are the test's expected values, not log output; the test passes in [m-unit-app.txt](logs/goal-07/m-unit-app.txt). After any council execute, `App.Export` refuses earlier, at the authority check (DR-06-01); `TestGenesisRoundTripFromCommittedState` asserts the `export refused:` prefix there. InitChain refuses the registry modules' own non-empty export (`TestLaunchGenesisRejectsNonEmptyCatalog`). An empty content catalogue is a v0 rule; refusing every genesis provider is goal 07's interim under DR-07-02. A restart path needs a protocol change or an owner ruling (see the status section). |
| Reconstruct query results from committed state | VERIFIED | At devnet height 58, six ABCI queries are equal on 4 nodes (`provider-p1`, `provider-p2`, `content-c1`, `content-c2`, `providers-page-1`, `providers-page-2`). An in-process replay of the blocks gives `replay-queries height=58 count=6 equal=true`. One gRPC call equals the ABCI bytes: `path=/svn.providers.v1.Query/Provider equal_to_abci=true`. History: `height=34 c1=PENDING tombstone=false; height=35 c1=WITHDRAWN tombstone=true`. App `TestRegistryFlowReplayAndQueries`. |
| Compare replay hashes | VERIFIED | Devnet `phase=replay height=58 app_hash=5597D2725A3AEBD97C0841DA8129B6F47ACA9805530BD45F896B9E95621B257F nodes=4 equal=true`. App `TestRegistryFlowReplayAndQueries` compares both nodes' app hash at every height. |
| Pagination boundaries | VERIFIED | `TestProvidersPaginationBoundaries` (9 subtests), `TestProvidersPageRetiredKeyBudget` and `TestRetiredKeyBoundsFitAPage` ([m-unit-providers.txt](logs/goal-07/m-unit-providers.txt)). msgkit `TestPaginateBoundaries`, `TestPaginateStartKeyIsInclusive` and `TestCheckPage` ([m-unit-msgkit.txt](logs/goal-07/m-unit-msgkit.txt)). App `TestGovernedLimitsReachTheRegistries`. Devnet limit 1: page 1 `next_page_key="p2"`, page 2 `next_page_key=""`. |
| Event schemas | VERIFIED | App `TestRegistryEventSchemas` against `chain/app/testdata/registry-events.golden.json`. msgkit `TestEmitParseRoundTripEveryEventType`, `TestEventAllowlistIsEventsProto` and `TestParseCommitted`. Devnet `typed=11 typed_parsed_with_msg_index=11`. |
| No private user details or media payloads in stored state or event fixtures | VERIFIED | App `TestNoPrivateDataInStateOrEvents`, `TestSubmitterTextFields`, `TestTransactionsRejectVideoBytes` and `TestStateSchemaHasNoPayloadField` (13 subtests). Content `TestContentStoresOnlyDigests`. Devnet `events heights=1..58 block_txs=41 events=123 typed=11 typed_parsed_with_msg_index=11 canaries=0 forbidden_names=0`. The limits are under [Residual risks](#residual-risks). |
| Handoff: minimal real-transaction demo and negative tests | VERIFIED | [m-devnet-registry.txt](logs/goal-07/m-devnet-registry.txt). |
| Handoff: package identity and provider admission APIs | IMPLEMENTED | Goal 08 is the consumer. Providers `TestHandoffAPI` (subtests `check-new-obligation`, `provider-returns-a-copy`, `verify-provider-signature`). Content `TestPackageIdentity`, `TestCheckOfferableOrder` and `TestWithdrawalBlocksOfferAndRevival`. |
| Goal 05 and 06 devnet regressions | VERIFIED, single machine | [m-devnet-regress.txt](logs/goal-07/m-devnet-regress.txt): `TestDevnetValidatorLifecycle (69.85s)`, `TestFourValidatorsCommitRestartAndQuorum (17.86s)` with `phase=stop-two threshold=12` and `phase=recovery-up height=14 stall=12`, and `TestInitRefusesToReplaceGenesis (0.01s)`. The full package run ([m-devnet.txt](logs/goal-07/m-devnet.txt)) passes 20 top-level tests with 0 FAIL and 0 SKIP. That includes goal 06's `TestDevnetQuorumFollowsVotingPower (71.25s)` with `phase=power-set effective-1=104 powers=v1:10,v2:10,v3:10,v4:10 effective=105 powers=v1:10,v2:10,v3:2,v4:10,v5:1` and case Y `observed=stall height_from=123 height_to=123 set_height=124`. Its lifecycle lines are `phase=add h-1=42 count=4 h=43 count=4 h+1=44 count=4 h+2=45 count=5`, `phase=rotate h+1=89 count=5 oldkey=true h+2=90 count=5 newkey=true` and `phase=remove h+1=131 count=5 still=true h+2=132 count=4`. |
| No test removed since the base | IMPLEMENTED (deviation recorded: five tests replaced by design) | [m-tests-not-removed.txt](logs/goal-07/m-tests-not-removed.txt) exits 1: `base test/fuzz functions: 104`, `head test/fuzz functions: 324`, `removed since base: func TestProviderMessagesAreForbiddenAndDecode`. [m-tests-not-removed-43c1a10.txt](logs/goal-07/m-tests-not-removed-43c1a10.txt) exits 1 against the previous tested commit: 242 against 324, four names removed. Each of the five is replaced (table under [Merge of goal 06 round 3](#merge-of-goal-06-round-3)), and [m-tests-replaced.txt](logs/goal-07/m-tests-replaced.txt) finds all eight replacements. The merge reviews compared each replacement's strength with the original; that comparison is a workflow output, not a capture log. Not VERIFIED: the no-removal check exits 1, and the strength comparison has no log. |

The devnet rejections are committed with a failure code, and the devnet replays each rejected block in process and compares the full state. CheckTx rejections are printed separately as `included=false`: outsider sdk/9, fee svn/21, the 70 KiB tx svn/19, and the rebroadcast sdk/32.

Provider key updates are separate from consensus-key changes. The devnet prints `phase=validators rotation_height=22 from=21 to=24 count=4 unchanged=true` around the provider key rotation at h22.

CAPACITY and OVERFLOW cannot be reached at app level before goal 08: reserved bytes stay 0 until leases exist, and an overflow needs key generation 2^64-1. Keeper tests cover them: `TestRejectedProviderMessagesLeaveTheStore/update-below-reserved` and `/rotate-overflow`, `TestUpdateProviderSpecCases/update-provider-below-reserved`, and `TestRotateProviderKeySpecCases/overflow` ([m-unit-providers.txt](logs/goal-07/m-unit-providers.txt)).

## Bounds, query gas and simulation (fix round)

| Item | Status | Evidence |
| --- | --- | --- |
| Retired-key cap per provider id (`MaxRetiredKeysPerProvider` = 1024, PROPOSED, DR-07-10) | VERIFIED | `TestRotationCap` (subtests `one_rotation_past_the_bound`, `stale_generation_at_the_bound`, `same_key_at_the_bound`), `TestStoredRecordAboveTheCap`, `TestGenesisRetiredKeyCap` and `TestRetiredKeyBoundsFitAPage` ([m-unit-providers.txt](logs/goal-07/m-unit-providers.txt)). `TestRotationCapInApp` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)) logs `at the bound: Query/Provider 46094 bytes, 200911 gas; Providers page 201436 gas; query gas limit 20000000`. |
| Response sizes | VERIFIED | `TestQueryResponseSizeBound` ([m-unit-providers.txt](logs/goal-07/m-unit-providers.txt)) asserts fixed bounds and logs `largest Query/Provider response: 47300 bytes (bound 65536)`, `largest Providers page: 79147 bytes over 2 pages (bound 131072)` and `largest exported record: 122426 bytes (bound 196608)`. The measured sizes depend on the build; the bounds are the assertion. |
| Query gas limit (`app.DefaultQueryGasLimit` = 20,000,000, PROPOSED node default, DR-07-10) | VERIFIED for ABCI query on a service path and for the query router; the node gRPC server path is untested | `TestQueryGasLimit` and `TestQueryGasLimitOption` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)), `TestNodeFileQueryGasLimit` ([m-unit-node.txt](logs/goal-07/m-unit-node.txt)). No test drives the node gRPC server's out-of-gas path. Raw `/store` queries are not metered (residual risk). |
| Simulation | VERIFIED | `TestSimulateEstimatesGas` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)) asserts that simulated gas equals committed gas. The log shows 11 steps with equal values, from `register p1: simulated gas 106789, committed gas 106789` to `withdraw c1: simulated gas 99254, committed gas 99254`. Gas 0 is accepted only in simulation; CheckTx and a block reject it as BOUNDS. `TestEnforceCommitted` and `TestHandlerMeterCheckByMode` pass ([m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)). |
| Registries refuse the goal-05/06 scaffold layout (ConsensusVersion 2) | VERIFIED | `TestAppRefusesScaffoldLayout` (4 subtests, [m-unit-app.txt](logs/goal-07/m-unit-app.txt)), and `TestNoMigrationFromScaffoldLayout` in providers and in content. |
| Governed limits fail closed | VERIFIED | `TestGovernedLimitFailsClosed` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)) checks RegisterContent through `Simulate` only (see [Residual risks](#residual-risks)). `TestKeeperReadSurface`, subtests `role`, `devnet_role`, `governed_limit` and `business_digest` ([m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)). |

## Requirements rows for goal 07

The rows of the `requirements-matrix` block whose goal column is `07`.

| Requirement | Status | Evidence |
| --- | --- | --- |
| REQ-PROD-010 | VERIFIED | `TestTransactionsRejectVideoBytes` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)). Unknown fields carrying 40 KiB of synthetic media fail decode at the top level, in a `Digest`, in an `InnerSignature` and in the tx body. A 1 KiB digest value is BOUNDS, and a 70 KiB tx is BOUNDS. Devnet CheckTx `tx-70-KiB` svn/19. |
| REQ-MODU-002 | VERIFIED | `TestProviderRecordFields` ([m-unit-providers.txt](logs/goal-07/m-unit-providers.txt)). Devnet `provider-p1` and `provider-p2` queries are equal on 4 nodes. |
| REQ-MODU-003 | VERIFIED | `TestContentRecordFields` ([m-unit-content.txt](logs/goal-07/m-unit-content.txt)). Devnet `content-c1` and `content-c2` queries are equal on 4 nodes. |
| REQ-OFF-001 | VERIFIED | `TestTransactionsRejectVideoBytes`, `TestStateSchemaHasNoPayloadField` and `TestNoPrivateDataInStateOrEvents` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)). `TestContentStoresOnlyDigests` ([m-unit-content.txt](logs/goal-07/m-unit-content.txt)). |
| REQ-TX-002, devnet intent | VERIFIED | `TestRegisterContentChecksInSpecOrder` (42 subtests), `TestRegisterContentSuccessBoundaries` and `TestRegisterContentCreatesNoHostingClaim` ([m-unit-content.txt](logs/goal-07/m-unit-content.txt)). Devnet register c1 and c2. The user-intent rule is the devnet-only interim of DR-07-01. |
| REQ-TX-002, production intent | BLOCKED, DR-07-01 (security-critical) | Needs a hosting-intent record and an approved on-chain DEC-001 value. Neither the approved DEC-001 default nor protocol v0 defines them (see the status section). Without the DEC-001 devnet parameter, RegisterContent is DECISION_BLOCKED (`TestDecisionBlockedRegistryPaths`, [m-unit-app.txt](logs/goal-07/m-unit-app.txt)). |
| REQ-INV-006, silent revival | VERIFIED | Devnet `duplicate-version-c1` h47 svn/4 after the withdrawal at h35. `TestWithdrawnBlocksRevival` and `TestSecondWithdraw` ([m-unit-content.txt](logs/goal-07/m-unit-content.txt)). |
| REQ-INV-006, new sponsored lease | DEFERRED to goal 08 | Goal 08 owns OfferLease. On this tree a lease message is still FORBIDDEN at the router (`TestEveryProtocolMessageDecodes`). The API it will call is tested: `TestWithdrawalBlocksOfferAndRevival` (`CheckOfferable` and `CheckNotWithdrawn` return WITHDRAWN). |
| REQ-HOST-004 | VERIFIED for the approved DEC-002 default; no operator list exists | DEC-002 is APPROVED. This row carries out its default and is not an open question (see the status section). The admission record exists: `TestProviderRecordFields` (operator group, status). Admission is a council payload only: the devnet admits p1 at h13 and p2 at h19 after three seat approvals each, and a direct admin message does not exist (`TestDirectAdminMessagesDoNotExist`). The membership policy stays blocked: without the DEC-002 devnet parameter, admission is DECISION_BLOCKED and the proposal stays QUEUED (`TestDecisionBlockedRegistryPaths`; `TestRejectedProviderMessagesLeaveTheStore/admit-decision-blocked`; `TestCouncilRegistrySeedsReachEveryPath/admit_without_DEC-002_stays_queued` in [m-seed-reach.txt](logs/goal-07/m-seed-reach.txt)). A group other than `devnet-test-only` is FORBIDDEN and the proposal becomes REJECTED (`TestRejectedProviderMessagesLeaveTheStore/admit-wrong-group`; `.../admit_of_another_group_is_rejected`). Authority passes the DEC-002 flag only for the devnet test role (`TestAdmitPassesTheDEC002Flag`, `TestDevnetRoleRule`, [m-unit-authority.txt](logs/goal-07/m-unit-authority.txt)). An operator group id is a label, not proof of an independent operator. |

## Goal 06 defects fixed here

InnerSignature decoding. At round 2 (`3853e34`), `svn.types.v1.InnerSignature` was not registered for transaction decoding, so a `MsgRotateProviderKey` transaction could not be decoded. Goal 06 round 3 registered it as well, so `main` now decodes it. The merged `chain/app/encoding.go` takes goal 06's registrations.

- [m-innersig-decode-base.txt](logs/goal-07/m-innersig-decode-base.txt): the probe [innersig_decode_probe_test.go.txt](goal-07/innersig_decode_probe_test.go.txt), on an archive of `3853e34`, prints `decode error: failed to retrieve the message of type "svn.types.v1.InnerSignature": tx parse error`. The probe logs its result and does not assert it, so the same file runs on every tree.
- [m-innersig-decode-main.txt](logs/goal-07/m-innersig-decode-main.txt) (archive of `f5916bd`) and [m-innersig-decode-head.txt](logs/goal-07/m-innersig-decode-head.txt) (archive of `HEAD`) both print `decoded 1 message(s) of type *providersv1.MsgRotateProviderKey`.
- The regression test `TestInnerSignatureMessagesDecode` passes ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)).

Simulation of authority messages. [m-authority-enforce.txt](logs/goal-07/m-authority-enforce.txt) shows that `enforceCommitted` checks the gas meter limit in every mode at `3853e34` and at `main` `f5916bd`. So this defect is still on `main`. The SDK simulates on an infinite gas meter, so every simulated authority message there is BOUNDS. This run did not simulate on those trees; that consequence follows from their code and the SDK's simulation meter. On `HEAD`, the meter check is wrapped in `if ctx.ExecMode() != sdk.ExecModeSimulate`. The tx-bytes check, the fixed charge and the signer check still run in every mode, and FinalizeBlock always runs the meter check. `TestEnforceCommitted`, `TestHandlerMeterCheckByMode` and `TestSimulateEstimatesGas` pass. Mutant `msg-simulate-skip-removed` is killed by all three ([m-mutants-merge.txt](logs/goal-07/m-mutants-merge.txt)).

## Recorded deviations

- A direct `AdmitProvider`, `DisableProvider` or `ForceWithdraw` is the SDK decode error sdk/2, not `ERROR_CODE_FORBIDDEN` svn/2. No such message type exists; these actions are council payloads only (DEC-009). `TestDirectAdminMessagesDoNotExist` ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)).
- A copy of another account's `RegisterProvider` (same business id and payload, another signer) is FORBIDDEN, not idempotent. This is stricter than `spec_machine.py`. `TestRegisterReplayByAnotherAccountIsForbidden` ([m-unit-providers.txt](logs/goal-07/m-unit-providers.txt)). See DR-07-08.
- A non-empty memo is BOUNDS. A fee payer, fee granter or tip is FEE_REJECTED. Both are ante-handler rules. `TestTransactionsRejectVideoBytes`.
- Unknown fields, critical (99) and non-critical (1024, 2047), are rejected by a strict decoder at decode time (sdk/2), not mapped to `ERROR_CODE_UNKNOWN_FIELD`. `TestTransactionsRejectVideoBytes`.
- The per-provider retired-key cap (BOUNDS at 1024 retired keys) is a chain rule. It is not in `spec_machine.py` (DR-07-10).
- A hosting intent signed for an unknown role is UNAUTHORIZED, not the spec's FORBIDDEN. An unknown role cannot be recognised from a signature (`chain/x/content/README.md`). `TestRegisterContentChecksInSpecOrder/intent-other-role` ([m-unit-content.txt](logs/goal-07/m-unit-content.txt)) signs with role `CREATOR` and expects UNAUTHORIZED.
- The relay column of `protocol.md` lets a broadcaster submit the outer transaction of `RegisterProvider`, `RegisterContent` and `WithdrawContent`. The registries follow `spec_machine.py` `check_role` instead: the outer signer must own the record. That is the `ADMITTED_PROVIDER` account, or the `ADMITTED_SERVICE` submitting host that the hosting intent signs. A broadcaster, council seats included, is `FORBIDDEN` (`chain/x/providers/README.md`, `chain/x/content/README.md`). `TestRegistryMessagesRejectOtherRoles` passes ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)), and mutant `content-accepts-broadcaster` is killed by it. Adopting the relay column would give council seat keys registry reach (DR-06-06, DR-07-11).
- A council seat address listed at genesis with any role but `BROADCASTER` is refused, by both genesis validators. The spec's `validate_genesis_view` has no such rule. This is the rule of DR-07-11, which the owner kept on 2026-11-02. `TestSeatAddressIsBroadcasterOnly`, `TestSeatAddressRole`, `TestValidateGenesisState` and `TestSeatAddressMatchesGenesis` pass ([m-unit.txt](logs/goal-07/m-unit.txt)).
- InitChain and `App.Export` refuse any genesis provider, in any status (`providers are not empty`). Protocol v0 `svn.app.v1.AppGenesis` carries `providers`, and `spec_machine.py` `validate_genesis_view` requires only empty content, leases and offers. This is goal 07's interim under DR-07-02, stricter than v0. `TestLaunchGenesisRejectsNonEmptyCatalog` and `TestExportRefusesANonEmptyCatalogue/provider_only` pass ([m-unit-app.txt](logs/goal-07/m-unit-app.txt)).
- `TestMessagesAreRejected` (goal 05) sends a complete `MsgRegisterProvider`, because the handler is real. It still expects FORBIDDEN, now as typed svn/2. Its fee and gas checks are typed too. The check is stricter, not weaker.

## Store layout and upgrade

x/providers and x/content are ConsensusVersion 2. Version 1 was the goal-05 scaffold. `app.New` refuses a home whose registry stores hold the goal-05/06 scaffold layout (`TestAppRefusesScaffoldLayout`, 4 subtests), and neither module has a migration from it (`TestNoMigrationFromScaffoldLayout`).

Devnet homes committed on goal 05/06 are refused at start; re-initialize them. This holds for round 2 and for round 3 homes. The x/authority proposal record also gained two fields (`content_id`, `content_version`) while authority's ConsensusVersion stays 1, so a goal-06 record would not decode either. No deployed chain exists, so no migration is needed.

## Fuzz runs

Each run used `bash -c '...; env -C chain go test -run='^$' -fuzz='^<Target>$' -fuzztime=<N> <package>; ...'`. The wrapper prints the load average before and after the run and lists new or changed files under `chain/` after it. The runs went one at a time: their `DATE` lines run from 04:10:16 to 04:18:12 UTC. The first gap, from `FuzzStateTransitions` to `FuzzProviderTransitions`, is 154 s, because `FuzzStateTransitions` fuzzed for 90 s after a 20 s baseline. The other nine gaps are 33 to 40 s. The load average reached 7.76 on 4 CPUs (`FuzzProviderTransitions` after), so the CPU was shared at times; this run cannot say with what.

All eleven exited 0. Every log has a `now fuzzing with 4 workers` line, so each run got past its baseline corpus. No run wrote a crasher: the file list after each run is empty. No `context deadline exceeded` occurred.

`go test -list 'Fuzz.*' ./...` in `chain` ([m-fuzz-list.txt](logs/goal-07/m-fuzz-list.txt)) lists these eleven targets. It also lists `TestFuzzCorpusReach` and `TestFuzzBoundedPayload` in `x/authority`, which are plain tests whose names match the pattern, not fuzz targets. No fuzz target was left out.

| Target | Package | Origin | -fuzztime | Baseline entries | Last `execs:` line | Load before / after | Exit | Log |
| --- | --- | --- | --- | --- | --- | --- | --- | --- |
| `FuzzStateTransitions` | `./x/authority/` | goal 06 | 90s | 503 | `elapsed: 1m32s, execs: 34442 (0/sec), new interesting: 29 (total: 532)` | 1.98 / 5.18 | 0 | [log](logs/goal-07/m-fuzz-FuzzStateTransitions.txt) |
| `FuzzProviderTransitions` | `./x/providers/` | goal 07 | 30s | 147 | `elapsed: 33s, execs: 15330 (0/sec), new interesting: 11 (total: 158)` | 5.18 / 7.76 | 0 | [log](logs/goal-07/m-fuzz-FuzzProviderTransitions.txt) |
| `FuzzProvidersGenesisJSON` | `./x/providers/` | goal 07 | 30s | 154 | `elapsed: 31s, execs: 38143 (0/sec), new interesting: 10 (total: 164)` | 7.76 / 6.41 | 0 | [log](logs/goal-07/m-fuzz-FuzzProvidersGenesisJSON.txt) |
| `FuzzCapabilityParse` | `./x/providers/capability/` | goal 07 | 30s | 114 | `elapsed: 31s, execs: 72472 (0/sec), new interesting: 9 (total: 123)` | 6.41 / 7.41 | 0 | [log](logs/goal-07/m-fuzz-FuzzCapabilityParse.txt) |
| `FuzzContentTransitions` | `./x/content/` | goal 07 | 30s | 164 | `elapsed: 31s, execs: 11370 (0/sec), new interesting: 13 (total: 177)` | 7.41 / 7.75 | 0 | [log](logs/goal-07/m-fuzz-FuzzContentTransitions.txt) |
| `FuzzContentGenesisJSON` | `./x/content/` | goal 07 | 30s | 125 | `elapsed: 31s, execs: 69411 (0/sec), new interesting: 23 (total: 148)` | 7.75 / 6.51 | 0 | [log](logs/goal-07/m-fuzz-FuzzContentGenesisJSON.txt) |
| `FuzzCouncilRegistryPayloads` | `./x/authority/` | goal 07, rebuilt on the per-object store | 30s | 306 | `elapsed: 32s, execs: 8671 (0/sec), new interesting: 23 (total: 329)` | 6.51 / 5.63 | 0 | [log](logs/goal-07/m-fuzz-FuzzCouncilRegistryPayloads.txt) |
| `FuzzParsePayload` | `./x/authority/` | goal 06 | 30s | 159 | `elapsed: 31s, execs: 26287 (0/sec), new interesting: 3 (total: 162)` | 5.63 / 5.82 | 0 | [log](logs/goal-07/m-fuzz-FuzzParsePayload.txt) |
| `FuzzInnerSignatureCheck` | `./msgkit/` | goal 07 | 30s | 101 | `elapsed: 30s, execs: 169476 (0/sec), new interesting: 2 (total: 103)` | 5.82 / 5.57 | 0 | [log](logs/goal-07/m-fuzz-FuzzInnerSignatureCheck.txt) |
| `FuzzTypedEventParse` | `./msgkit/` | goal 07 | 30s | 174 | `elapsed: 30s, execs: 235122 (0/sec), new interesting: 13 (total: 187)` | 5.57 / 5.40 | 0 | [log](logs/goal-07/m-fuzz-FuzzTypedEventParse.txt) |
| `FuzzProposalPayloadProto` | `./x/authority/` | goal 06 round 3, new in this run's list | 60s | 199 | `elapsed: 1m1s, execs: 129649 (0/sec), new interesting: 6 (total: 205)` | 5.40 / 5.66 | 0 | [log](logs/goal-07/m-fuzz-FuzzProposalPayloadProto.txt) |

The three `*Transitions` targets and `FuzzCouncilRegistryPayloads` are state-transition fuzzers over bounded op sequences. The others are parser fuzzers. In the merged `FuzzStateTransitions`, every operation runs with an empty spec registry bound.

`FuzzStateTransitions` ran 90 s because its baseline corpus is large: its log shows `gathering baseline coverage: 503/503 completed, now fuzzing with 4 workers` at 20 s. Exec counts depend on the CPU share. The load average is a one-minute average, so each "before" value includes the tail of the previous run.

Corpus and seed reach:

- `TestFuzzCorpusReach` ([m-corpus-reach.txt](logs/goal-07/m-corpus-reach.txt)) prints `total:  {Ops:65295 Committed:41667 Queued:383 Executed:40 Rejected:17 Expired:2133 ApplyDueChanged:10 Bounds:1080 Idempotent:307 Refused:23321}` and passes in 5.30s.
- [m-seed-reach.txt](logs/goal-07/m-seed-reach.txt): `TestCouncilRegistrySeedsReachEveryPath` passes with 7 subtests, from `admit_then_disable` to `disable_of_a_draining_provider`, and `TestContentProgramSeedsReachEveryTransition` passes.

## Mutation checks

A mutant is a small change to committed code. A test failure means the mutant was killed. Five mutants are a spot check of goal 07's fix round, and fourteen are a spot check of the merge. They are not a mutation score for the goal.

**Fix-round overlays, unchanged runner.** Each mutant under [goal-07/mutants/](goal-07/mutants/) is a full copy of one source file as it was at `43c1a10`, with one small change. [run_mutant.sh](goal-07/mutants/run_mutant.sh) runs the named tests with `go -overlay`. Each run used `scripts/evidence/capture.sh <log> -- bash docs/evidence/goal-07/mutants/run_mutant.sh <mutant>`, one at a time.

| Mutant | Change | Tests | Exit | Result | Log |
| --- | --- | --- | --- | --- | --- |
| `m01-simulate-extra-gas` | `msgkit.Enforce` consumes 1 extra gas in simulation | `TestSimulateEstimatesGas` | 1 | Killed: `register p1: simulated gas 106790, committed gas 106789`. A `>=` check would accept this mutant; the equality check rejects it. | [log](logs/goal-07/m-mutant-m01-simulate-extra-gas.txt) |
| `m02-ante-gas0-always` | the ante handler accepts a declared gas limit of 0 in every mode | `TestSimulateEstimatesGas` | 1 | Killed: `CheckTx with gas 0: sdk/11 out of gas in location: ReadFlat; gasWanted: 0, gasUsed: 1000: out of gas`, where BOUNDS is wanted | [log](logs/goal-07/m-mutant-m02-ante-gas0-always.txt) |
| `m03-query-gas-zero-unlimited` | `QueryGasLimit` returns 0 (SDK unlimited) when the option is absent or 0 | `TestQueryGasLimit`, `TestQueryGasLimitOption` | 1 | Killed: `no option: 0 <nil>, want 20000000`, and the `zero` and `empty string` cases | [log](logs/goal-07/m-mutant-m03-query-gas-zero-unlimited.txt) |
| `m04-rotation-cap-off-by-one` | the rotation cap uses `>` instead of `>=` | `TestRotationCap` | 1 | Killed: `one_rotation_past_the_bound`: `code , want ERROR_CODE_BOUNDS`, and the other two subtests | [log](logs/goal-07/m-mutant-m04-rotation-cap-off-by-one.txt) |
| `m05-authority-meter-finalize-only` | `enforceCommitted` checks the meter only in FinalizeBlock | `TestEnforceCommitted`, `TestHandlerMeterCheckByMode` | 1 | **Not a kill: build failed.** The overlay is a full copy of the pre-merge `msg.go`, which no longer fits the merged package: `k.load undefined`, `k.save undefined`, `undefined: authorityGas`, `[build failed]`. The new runner below runs this mutant. | [log](logs/goal-07/m-mutant-m05-authority-meter-finalize-only.txt) |

**New runner, `docs/evidence/goal-07/mutants.py`.** It uses only the standard library and is allowlisted in `PY_ALLOW` of the goal 01 checker. It exports `HEAD` with `git archive` into a temporary directory. It runs each named test command once on the unmutated copy as a control. Then, for each mutant, it replaces one exact search string, which must occur exactly once, runs the tests, and restores the file. A build failure is not counted as a kill.

- For the five fix-round mutants, the runner first applies each change to the file as it was at `43c1a10` and requires the result to equal the committed overlay copy byte for byte. So each change is the one the earlier evidence ran.
- The fourteen merge mutants are reconstructions. They are rebuilt from the names and descriptions in the merge workflow's scratch logs, not the same source text (the runner's docstring says so).

| Run | Exit | Result | Log |
| --- | --- | --- | --- |
| `mutants.py --check` | 0 | `mutants 19 of 19 (goal07=5, merge=14)`, five `COPY_MATCH` lines (m05: `chain/x/authority/msg.go changed since 43c1a10`), `search strings: every one occurs exactly once` | [m-mutants-check.txt](logs/goal-07/m-mutants-check.txt) |
| `mutants.py --group goal07` | 0 | Controls `authority-meter`, `query-gas`, `rotation-cap` and `sim-gas` PASS. `RESULT mutants=5 killed=5`. `KILLED m05-authority-meter-finalize-only ... failing=TestEnforceCommitted`. m01 and m02 are killed by `TestSimulateEstimatesGas`, m03 by `TestQueryGasLimitOption`, m04 by `TestRotationCap` and its three subtests. | [m-mutants-goal07.txt](logs/goal-07/m-mutants-goal07.txt) |
| `mutants.py --group merge` | 0 | `CONTROL merge PASS exit=0 secs=129.2`. `RESULT mutants=14 killed=14`. The killing tests are under [Merge mutants](#merge-mutants). | [m-mutants-merge.txt](logs/goal-07/m-mutants-merge.txt) |
| `mutants.py --only msg-hooks-on-tx-context --logdir <dir>`, then every `--- FAIL` line of the saved output | 0 | `CONTROL merge PASS exit=0 secs=130.9`. `RESULT mutants=1 killed=1`. Package `chain/x/authority` FAIL; the other five packages `ok`. The full FAIL list ends with `--- FAIL: TestKeeperRegistryWriteDroppedOnLateFailure (0.00s)`, and `FAIL lines naming TestKeeperRegistryWriteDroppedOnLateFailure: 1`. | [m-mutant-tx-context-full.txt](logs/goal-07/m-mutant-tx-context-full.txt) |

### Merge mutants

The "Killed by" column copies the top-level test names from each `failing=` field of [m-mutants-merge.txt](logs/goal-07/m-mutants-merge.txt). The runner prints only the first 300 characters of that field (`[:300]` in `mutants.py`), so a long list is cut. Where a name is cut, the row says so. For `msg-hooks-on-tx-context` the full list is in [m-mutant-tx-context-full.txt](logs/goal-07/m-mutant-tx-context-full.txt).

| Mutant | File | Killed by |
| --- | --- | --- |
| `doc-seat-rule-off` | `chain/genesis/document.go` | `TestSeatAddressIsBroadcasterOnly` (document and InitChain subtests; list cut) |
| `auth-seat-rule-off` | `chain/x/authority/genesis.go` | `TestValidateGenesisState`, `TestSeatAddressIsBroadcasterOnly` (InitChain subtests) |
| `auth-seat-rule-allows-auditor` | `chain/x/authority/genesis.go` | `TestValidateGenesisState`, `TestSeatAddressIsBroadcasterOnly/InitChain_AUDITOR` |
| `doc-seat-rule-case-sensitive` | `chain/genesis/document.go` | `TestSeatAddressRole` (the four upper-case address subtests) |
| `doc-seat-address-wrong` | `chain/genesis/document.go` | `TestSeatAddressMatchesGenesis`, `TestSeatAddressIsBroadcasterOnly` (list cut) |
| `export-no-catalogue-check` | `chain/app/state.go` | `TestLaunchGenesisRejectsNonEmptyCatalog`, `TestExportRefusesANonEmptyCatalogue` (`provider_only`, `content_only`) |
| `peer-integrity-to-replay` | `chain/x/authority/engine.go` | `TestDamagedRecordIsIntegrity`, `TestKeeperMalformedPeerBusinessDigestIsIntegrity` (list cut) |
| `peer-empty-is-free` | `chain/x/authority/engine.go` | `TestKeeperMalformedPeerBusinessDigestIsIntegrity/nil` |
| `content-accepts-broadcaster` | `chain/x/content/msg_server.go` | `TestRegistryMessagesRejectOtherRoles` |
| `msg-keep-writes-registry` | `chain/x/authority/msg.go` | `TestKeeperRegistryForbiddenKeepsTheRejection` (`admit`, `disable`, `force_withdraw`) |
| `msg-hooks-on-message-branch` | `chain/x/authority/msg.go` | `TestKeeperRegistryForbiddenKeepsTheRejection` (`admit`, `disable`, `force_withdraw`) |
| `msg-hooks-on-tx-context` | `chain/x/authority/msg.go` | `TestKeeperRegistryForbiddenKeepsTheRejection`, `TestKeeperRegistryFailureWritesNothing` (18 subtests) and `TestKeeperRegistryWriteDroppedOnLateFailure`. The merge log's list is cut inside the name of the first of the 18 subtests (`TestKeeperRegistryFailureWritesNothing/admit/ERROR_C`); the full list is in [m-mutant-tx-context-full.txt](logs/goal-07/m-mutant-tx-context-full.txt). |
| `msg-success-skips-registry-write` | `chain/x/authority/msg.go` | `TestKeeperRegistryHookCommitsOnlyOnSuccess`, `TestKeeperForceWithdrawArgumentsSurviveTheRecord`, `TestMigrationsNoOpInApp`, `TestRegistryMigrationPathInApp`, `TestRegistryEventSchemas`, `TestNoPrivateDataInStateOrEvents`, and more (list cut at `TestRegistryFlo`) |
| `msg-simulate-skip-removed` | `chain/x/authority/msg.go` | `TestEnforceCommitted`, `TestHandlerMeterCheckByMode`, `TestSimulateEstimatesGas` |

`TestKeeperRegistryWriteDroppedOnLateFailure` says in its comment that it "Fails if the hooks are bound to the transaction context instead of a branch". The full run confirms that for the reconstructed mutant: it is one of the failing tests in [m-mutant-tx-context-full.txt](logs/goal-07/m-mutant-tx-context-full.txt).

The merge workflow's sixth `msg.go` mutant is not in the runner: binding the peer lookup to the tx context instead of the message branch. It survived there. The workflow explains it as equivalent, because the engine never writes the peer stores.

**Goal 06's runner on this tree.** `docs/evidence/goal-06/mutants.py --check` exits 1 ([m-goal06-anchors.txt](logs/goal-07/m-goal06-anchors.txt)): `mutants 119 of 119`, `NOT_FOUND swap-engine-492/-548/-575/-578/-591 chain/x/authority/engine.go occurrences=0`, `RESULT search-string problems=5; no test was run`. Those five mutants target code the merge deletes by design: the fixed NOT_FOUND for force-withdraw, `s.provider`, `s.admit` and `s.disable` (from the anchor descriptions in `docs/evidence/goal-06/mutants.py`; code reading, no capture log). The 114 others still find their anchors. The goal 06 kill run did not run on this tree: the runner refuses before running any test. It is BLOCKED (see [Gates](#gates) and [Blockers](#blockers)).

## Unit suites

| Log | Top-level tests passed | Subtests passed | Failed | Skipped | Exit |
| --- | --- | --- | --- | --- | --- |
| [m-unit.txt](logs/goal-07/m-unit.txt), all of `./chain/...` with `SVN_SKIP_DEVNET=1` | 319 | 729 | 0 | 4 | 0 |
| [m-unit-svnerr.txt](logs/goal-07/m-unit-svnerr.txt) | 2 | 0 | 0 | 0 | 0 |
| [m-unit-msgkit.txt](logs/goal-07/m-unit-msgkit.txt) | 51 | 31 | 0 | 0 | 0 |
| [m-unit-providers.txt](logs/goal-07/m-unit-providers.txt) | 63 | 289 | 0 | 0 | 0 |
| [m-unit-content.txt](logs/goal-07/m-unit-content.txt) | 46 | 166 | 0 | 0 | 0 |
| [m-unit-authority.txt](logs/goal-07/m-unit-authority.txt) | 75 | 113 | 0 | 0 | 0 |
| [m-unit-node.txt](logs/goal-07/m-unit-node.txt) | 1 | 0 | 0 | 0 | 0 |
| [m-unit-app.txt](logs/goal-07/m-unit-app.txt) | 56 | 41 | 0 | 0 | 0 |
| [m-devnet.txt](logs/goal-07/m-devnet.txt), `./chain/devnet/` | 20 | 49 | 0 | 0 | 0 |

Counts are `--- PASS` lines at column 0 (top level) and indented (subtests). Top-level counts include fuzz targets running their seed corpus; seed runs are not fuzz runs. The four skips in `m-unit.txt` are the four devnet process tests (`TestDevnetValidatorLifecycle`, `TestDevnetQuorumFollowsVotingPower`, `TestFourValidatorsCommitRestartAndQuorum`, `TestDevnetRegistryDemo`), which `SVN_SKIP_DEVNET=1` skips. They ran in `m-devnet.txt`.

## Gates

| Gate | Status | Log |
| --- | --- | --- |
| `go test -race -count=1 -timeout 20m ./chain/...` | VERIFIED | [m-race.txt](logs/goal-07/m-race.txt) exit 0, first attempt (there is no `-attempt1` log). app 78.670s, devnet 276.465s, x/authority 59.834s. The log has no `DATA RACE` line. |
| `go test -race` for `./proto/...` and `./agents/provider/...` | VERIFIED | [m-race-proto.txt](logs/goal-07/m-race-proto.txt), [m-race-agents-provider.txt](logs/goal-07/m-race-agents-provider.txt) |
| `python scripts/run_tests.py --only go` | VERIFIED | [m-tests-go.txt](logs/goal-07/m-tests-go.txt) exit 0. Every package `ok`, devnet 183.835s, no `(cached)` line. |
| `bash scripts/gofmt_check.sh`, `gofmt -l`, `go vet` | VERIFIED | [m-gofmt-vet.txt](logs/goal-07/m-gofmt-vet.txt): `gofmt ok files 193`, `gofmt -l chain proto agents: <empty>`, `gofmt_check=0 gofmt_l=0 vet_chain=0 vet_proto=0 vet_agents_provider=0` |
| Proto regeneration diff | VERIFIED | [m-proto-regen.txt](logs/goal-07/m-proto-regen.txt): `libprotoc 36.2`, `generated 28 proto files`, `proto regeneration diff empty` |
| Protocol, goal pages and CI unchanged since `main` | VERIFIED | [m-protocol-unchanged.txt](logs/goal-07/m-protocol-unchanged.txt): exit 0, empty diff for `docs/protocol`, `docs/protocol.md`, `docs/goals` and `.github/workflows/ci.yml` |
| ruff format, ruff check, mypy | VERIFIED | [m-ruff-format.txt](logs/goal-07/m-ruff-format.txt) `33 files already formatted`, [m-ruff-check.txt](logs/goal-07/m-ruff-check.txt) `All checks passed!`, [m-mypy.txt](logs/goal-07/m-mypy.txt) `Success: no issues found in 24 source files`. These are CI's paths (ruff: `services workers/media packages/protocol tests scripts`, per each log's COMMAND line; mypy: the `files` list in `pyproject.toml`). They do not include `docs/evidence/`, so the new runner `docs/evidence/goal-07/mutants.py` is not linted or type-checked by this gate, the same as goal 06's runner. |
| workflow policy, secret scan, SBOM | VERIFIED for tracked files at `b782e01` | [m-workflow-policy.txt](logs/goal-07/m-workflow-policy.txt) `workflow policy ok files 1`, [m-scan-secrets.txt](logs/goal-07/m-scan-secrets.txt) `secret scan ok`, [m-sbom-check.txt](logs/goal-07/m-sbom-check.txt) `sbom ok components 1320`. `scripts/scan_secrets.py` scans only `git ls-files` output (`tracked_files()`). It ran at 04:09:00 UTC, when `docs/evidence/goal-07/mutants.py` and every `m-` log were untracked and before this file and the progress rows were rewritten. So `m-scan-secrets.txt` covers the tracked files at `b782e01` only. No logged secret scan covers the new evidence; see the next row. |
| Python tests | VERIFIED | [m-tests-python.txt](logs/goal-07/m-tests-python.txt): `120 passed in 35.60s`. This checkout has a real `node_modules` directory, not a symlink (read with `ls -ld` by the final verifier, not a capture log). |
| `npm run typecheck`, Node tests | VERIFIED | [m-npm-typecheck.txt](logs/goal-07/m-npm-typecheck.txt) exit 0 (`tsc --noEmit`), [m-tests-node.txt](logs/goal-07/m-tests-node.txt) `# pass 1`, `# fail 0` |
| Contract checkers, before the docs edit | VERIFIED | [m-contract-requirements.txt](logs/goal-07/m-contract-requirements.txt): `self_test_cases 12`, `self_test_problems 0`, `errors 0`, `result PASS`, with `docs/evidence/goal-07/mutants.py` in the tree and its `PY_ALLOW` entry in place. [m-contract-protocol.txt](logs/goal-07/m-contract-protocol.txt): `transition_cases 97`, `valid_vectors 15`, `invalid_vectors 14`, `errors 0`, `languages MATCH`, `result PASS`. Both checked the committed progress rows. |
| Post-docs checks on the evidence commit: secret scan, `check_requirements.py`, `check_protocol.py` | VERIFIED in CI (not a capture log) | CI run [41438312010](https://github.com/Maxencejules/shortvideo-network/actions/runs/41438312010) on the evidence commit `45ac279` (draft [PR 10](https://github.com/Maxencejules/shortvideo-network/pull/10)): `node`, `python`, `go`, `devnet` and `contract` all `success`. `45ac279` changes only `docs/` from `b782e01`, so the code CI tested is tree `f07ac7f6`'s. Read from PR 10's check runs through the GitHub API at about 11:51 UTC on 2026-11-02; not a capture log. The `python` job ran the secret scan and the `contract` job ran both checkers on the committed docs. | CI's `python` job runs the secret scan and its `contract` job runs both checkers on the evidence commit, in the authorized draft pull request; those runs are shown in its checks. Nothing logged here checks the edited docs, and no logged secret scan covers the new runner, the `m-` logs (including the 7.4 MB and 7.6 MB unit logs) or this file. They need the evidence commit, because the secret scan reads only tracked files. Commands, run on the evidence commit: `scripts/evidence/capture.sh docs/evidence/logs/goal-07/m-post-scan-secrets.txt -- .venv/bin/python scripts/scan_secrets.py`, `scripts/evidence/capture.sh docs/evidence/logs/goal-07/m-post-contract-requirements.txt -- .venv/bin/python docs/evidence/goal-01/check_requirements.py`, and `scripts/evidence/capture.sh docs/evidence/logs/goal-07/m-post-contract-protocol.txt -- bash -c 'export PATH="$PWD/.toolchains/bin:$PATH"; .venv/bin/python docs/evidence/goal-02/check_protocol.py'`. Both contract checkers, and the scanner's detectors over the changed and untracked paths, were run on the edited working tree outside `capture.sh`; those runs have no log and are not cited as evidence (see [Next safe action](#next-safe-action)). |
| `go mod tidy -diff` | VERIFIED | [m-go-mod-tidy-diff.txt](logs/goal-07/m-go-mod-tidy-diff.txt): exit 0, empty. At `main` it is empty too ([m-go-mod-tidy-diff-main.txt](logs/goal-07/m-go-mod-tidy-diff-main.txt)). At round 2 it was not ([m-go-mod-tidy-diff-base.txt](logs/goal-07/m-go-mod-tidy-diff-base.txt), exit 1: cobra to direct, otelslog dropped). |
| `go.mod` and `go.sum` against `main` | VERIFIED (tidy-only change, recorded) | [m-gomod-vs-main.txt](logs/goal-07/m-gomod-vs-main.txt), exit 1 because the files differ. `go.mod` only: `google.golang.org/genproto/googleapis/rpc v0.0.0-20260526163538-3dc84a4a5aaa` moves from indirect to direct (`chain/msgkit/queryerr.go` imports it; code reading, no capture log), and `go.opentelemetry.io/contrib/bridges/otelslog v0.19.0 // indirect` is added. No version changes, and `go.sum` is unchanged. |
| Goal 06 mutation runner on this tree | BLOCKED | [m-goal06-anchors.txt](logs/goal-07/m-goal06-anchors.txt), exit 1: five anchors (`swap-engine-492`, `-548`, `-575`, `-578`, `-591`) target code the merge deleted, so the runner refuses before running any test (`RESULT search-string problems=5; no test was run`). Command: `.venv/bin/python docs/evidence/goal-06/mutants.py`. It is not a goal 07 acceptance check, but goal 07 changed `chain/x/authority/engine.go` against `main` (`156 +-` in [m-diff-stat.txt](logs/goal-07/m-diff-stat.txt)), and the other 114 mutants target that file among others, so it is an affected check. See [Mutation checks](#mutation-checks), [Blockers](#blockers) and [Follow-ups](#follow-ups). |
| golangci-lint | BLOCKED | [m-golangci.txt](logs/goal-07/m-golangci.txt), exit 3: `golangci-lint has version 2.5.0 built with go1.25.1`, and `the Go language version (go1.25) used to build golangci-lint is lower than the targeted Go version (1.26.10)`. Not a repository gate: neither the Makefile nor CI runs it (code reading, no capture log). |
| Head CI | VERIFIED (not a capture log) | CI run [41438312010](https://github.com/Maxencejules/shortvideo-network/actions/runs/41438312010) on the evidence commit `45ac279` (draft [PR 10](https://github.com/Maxencejules/shortvideo-network/pull/10)): `node`, `python`, `go`, `devnet` and `contract` all `success`. `45ac279` changes only `docs/` from `b782e01`, so the code CI tested is tree `f07ac7f6`'s. Read from PR 10's check runs through the GitHub API at about 11:51 UTC on 2026-11-02; not a capture log. |
| CI `devnet` job, `bash scripts/devnet/prove-containers.sh` | BLOCKED (environment), unchanged script | [m-containers-unchanged.txt](logs/goal-07/m-containers-unchanged.txt), exit 1. `go build` inside `docker build` fails with `tls: failed to verify certificate: x509: certificate signed by unknown authority` against proxy.golang.org, because the build container does not trust the session's egress-proxy CA. This is an environment limit, not a code failure. |
| Container devnet proof, CI script with only the image-build step changed | VERIFIED (labelled deviation), empty blocks only | [m-containers-labelled-copy.txt](logs/goal-07/m-containers-labelled-copy.txt), exit 0, after [m-disk.txt](logs/goal-07/m-disk.txt) showed 12G available. Four containers on the host network agree on the app hash at every sample. Two nodes stopped: `stop-two threshold=13`, `two-down stall=14`. Restart: `recovery stall=14 up=16 reached=19`, `recovery threshold=19 app_hash=3326D752FF423120A2C6D288C033DBD01A0473B4D5FBDF700BEB8D38E1711D53`. The script states `NOTE empty blocks only` and `NOTE bridge networking is BLOCKED. This run uses host networking.` The build differs from CI's: [Dockerfile.proxy-ca](goal-07/containers/Dockerfile.proxy-ca) adds the proxy CA as a BuildKit secret in the discarded build stage. [prove-containers-proxy-ca.sh](goal-07/containers/prove-containers-proxy-ca.sh) differs from the CI script only in that build command, its COMMAND line and the repo-root path. The script prints its own `DIRTY yes` because it counts untracked evidence files; `capture.sh` prints `DIRTY no`. |

## Commands

```text
m-devnet-registry.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh go test -count=1 -timeout 900s -v -run TestDevnetRegistryDemo ./chain/devnet/
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-devnet.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh go test -count=1 -v -timeout 30m ./chain/devnet/
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-devnet-regress.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh go test -count=1 -timeout 900s -v -run TestDevnetValidatorLifecycle\|TestFourValidatorsCommitRestartAndQuorum\|TestInitRefusesToReplaceGenesis ./chain/devnet/
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-race.txt
COMMAND /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/netns-run.sh go test -race -count=1 -timeout 20m ./chain/...
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-unit.txt
COMMAND env SVN_SKIP_DEVNET=1 go test -count=1 -v ./chain/...
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-unit-app.txt
COMMAND env -C chain go test -count=1 -timeout 900s -v ./app/...
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-unit-providers.txt
COMMAND env -C chain go test -count=1 -v ./x/providers/...
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-unit-content.txt
COMMAND env -C chain go test -count=1 -v ./x/content/...
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-unit-authority.txt
COMMAND env -C chain go test -count=1 -v ./x/authority/...
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-unit-msgkit.txt
COMMAND env -C chain go test -count=1 -v ./msgkit/...
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-fuzz-FuzzStateTransitions.txt
COMMAND bash -c echo\ \"loadavg\ before:\ \$\(cat\ /proc/loadavg\)\"\;\ env\ -C\ chain\ go\ test\ -run=\'\^\$\'\ -fuzz=\'\^FuzzStateTransitions\$\'\ -fuzztime=90s\ ./x/authority/\;\ s=\$\?\;\ echo\ \"loadavg\ after:\ \$\(cat\ /proc/loadavg\)\"\;\ echo\ \"new\ or\ changed\ files\ under\ chain/\ after\ the\ run:\"\;\ git\ status\ --porcelain\ --untracked-files=all\ --\ chain\;\ echo\ \"\(end\ of\ list\)\"\;\ exit\ \$s
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-fuzz-FuzzProposalPayloadProto.txt
COMMAND bash -c echo\ \"loadavg\ before:\ \$\(cat\ /proc/loadavg\)\"\;\ env\ -C\ chain\ go\ test\ -run=\'\^\$\'\ -fuzz=\'\^FuzzProposalPayloadProto\$\'\ -fuzztime=60s\ ./x/authority/\;\ s=\$\?\;\ echo\ \"loadavg\ after:\ \$\(cat\ /proc/loadavg\)\"\;\ echo\ \"new\ or\ changed\ files\ under\ chain/\ after\ the\ run:\"\;\ git\ status\ --porcelain\ --untracked-files=all\ --\ chain\;\ echo\ \"\(end\ of\ list\)\"\;\ exit\ \$s
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-mutants-goal07.txt
COMMAND .venv/bin/python docs/evidence/goal-07/mutants.py --group goal07
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-mutants-merge.txt
COMMAND .venv/bin/python docs/evidence/goal-07/mutants.py --group merge
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-mutant-m05-authority-meter-finalize-only.txt
COMMAND bash docs/evidence/goal-07/mutants/run_mutant.sh m05-authority-meter-finalize-only
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:1
```

```text
m-goal06-anchors.txt
COMMAND .venv/bin/python docs/evidence/goal-06/mutants.py --check
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:1
```

```text
m-tests-not-removed.txt
COMMAND bash -c b=\$\(git\ grep\ -h\ -E\ \"\^func\ \(Test\|Fuzz\)\"\ f5916bd02ce49efa2481dd3a7749a00b2fe6f67e\ --\ chain\ \|\ sed\ \"s/\(.\*//\"\ \|\ sort\)\;\ h=\$\(git\ grep\ -h\ -E\ \"\^func\ \(Test\|Fuzz\)\"\ HEAD\ --\ chain\ \|\ sed\ \"s/\(.\*//\"\ \|\ sort\)\;\ echo\ \"base\ test/fuzz\ functions:\ \$\(echo\ \"\$b\"\ \|\ wc\ -l\)\"\;\ echo\ \"head\ test/fuzz\ functions:\ \$\(echo\ \"\$h\"\ \|\ wc\ -l\)\"\;\ gone=\$\(comm\ -23\ \<\(echo\ \"\$b\"\)\ \<\(echo\ \"\$h\"\)\)\;\ echo\ \"removed\ since\ base:\ \$\{gone:-none\}\"\;\ test\ -z\ \"\$gone\"
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:1
```

```text
m-gomod-vs-main.txt
COMMAND git diff --exit-code f5916bd02ce49efa2481dd3a7749a00b2fe6f67e HEAD -- chain/go.mod chain/go.sum
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:1
```

```text
m-golangci.txt
COMMAND bash -c golangci-lint\ version\;\ cd\ chain\ \&\&\ golangci-lint\ run\ ./...
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:3
```

```text
m-containers-unchanged.txt
COMMAND bash scripts/devnet/prove-containers.sh
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:1
```

```text
m-containers-labelled-copy.txt
COMMAND bash docs/evidence/goal-07/containers/prove-containers-proxy-ca.sh
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-contract-requirements.txt
COMMAND .venv/bin/python docs/evidence/goal-01/check_requirements.py
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-contract-protocol.txt
COMMAND bash -c export\ PATH=\"\$PWD/.toolchains/bin:\$PATH\"\;\ .venv/bin/python\ docs/evidence/goal-02/check_protocol.py
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

```text
m-mutant-tx-context-full.txt
COMMAND env TMPDIR=/tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-07-ev/tmp bash -c .venv/bin/python\ docs/evidence/goal-07/mutants.py\ --only\ msg-hooks-on-tx-context\ --logdir\ \"\$0\"\;\ rc=\$\?\;\ echo\ \"package\ results\ of\ the\ mutant\ run:\"\;\ grep\ -E\ \"\^\(ok\|FAIL\)\[\[:space:\]\]\"\ \"\$0/msg-hooks-on-tx-context.txt\"\;\ echo\ \"every\ FAIL\ line\ of\ the\ mutant\ run:\"\;\ grep\ -E\ \"\^\[\[:space:\]\]\*---\ FAIL:\ \"\ \"\$0/msg-hooks-on-tx-context.txt\"\;\ echo\ \"FAIL\ lines\ naming\ TestKeeperRegistryWriteDroppedOnLateFailure:\ \$\(grep\ -c\ --\ \"---\ FAIL:\ TestKeeperRegistryWriteDroppedOnLateFailure\"\ \"\$0/msg-hooks-on-tx-context.txt\"\)\"\;\ exit\ \$rc /tmp/claude-0/-home-user-shortvideo-network/9321dcf5-8dfa-5ba7-bbf4-0dfbce01360f/scratchpad/goal-07-ev/txctx-out
TREE f07ac7f6113f45a97167cf9c8139fb712227490e
EXIT:0
```

The other commands are the first line of each log.

## Superseded logs

These logs are not evidence for this revision. They ran on commit `43c1a10`, tree `8d7bec054dc44e2e58b891452dae1c989998ffc6`, before the goal 06 merge. They are kept for history and are cited nowhere else in this file.

`authority-enforce-base.txt`, `ci-head.txt`, `contract-protocol-post.txt`, `contract-protocol-pre.txt`, `contract-requirements-post.txt`, `contract-requirements-pre.txt`, `devnet-containers-ci-script.txt`, `devnet-containers-proxy-ca.txt`, `devnet-registry.txt`, `devnet-regress.txt`, `diff-stat.txt`, `docker-info.txt`, `fuzz-FuzzCapabilityParse.txt`, `fuzz-FuzzContentGenesisJSON.txt`, `fuzz-FuzzContentTransitions.txt`, `fuzz-FuzzCouncilRegistryPayloads.txt`, `fuzz-FuzzInnerSignatureCheck.txt`, `fuzz-FuzzParsePayload.txt`, `fuzz-FuzzProviderTransitions.txt`, `fuzz-FuzzProvidersGenesisJSON.txt`, `fuzz-FuzzStateTransitions.txt`, `fuzz-FuzzTypedEventParse.txt`, `fuzz-list.txt`, `go-mod-tidy-diff-base.txt`, `go-mod-tidy-diff.txt`, `gofmt.txt`, `golangci-lint.txt`, `gomod-unchanged.txt`, `innersig-decode-base.txt`, `innersig-decode-head.txt`, `mutant-m01-simulate-extra-gas.txt`, `mutant-m02-ante-gas0-always.txt`, `mutant-m03-query-gas-zero-unlimited.txt`, `mutant-m04-rotation-cap-off-by-one.txt`, `mutant-m05-authority-meter-finalize-only.txt`, `mypy.txt`, `npm-typecheck.txt`, `proto-regen.txt`, `race-agents-provider.txt`, `race-chain.txt`, `race-proto.txt`, `revision.txt`, `ruff-check.txt`, `ruff-format.txt`, `runtime.txt`, `sbom-check.txt`, `scan-secrets-post.txt`, `scan-secrets.txt`, `stack.txt`, `tests-go.txt`, `tests-node.txt`, `tests-not-removed.txt`, `tests-python.txt`, `unit-app.txt`, `unit-authority.txt`, `unit-content.txt`, `unit-msgkit.txt`, `unit-node.txt`, `unit-providers.txt`, `unit-svnerr.txt`, `vet-agents-provider.txt`, `vet-chain.txt`, `vet-proto.txt`, `workflow-policy.txt`.

That is 64 logs. Their devnet heights (h4-h69) and app hash differ from this run, because the merged genesis and accounts differ.

## Blockers

- REQ-TX-002 for production: DR-07-01. It needs a hosting-intent record and an approved on-chain DEC-001 value, which neither the approved default nor protocol v0 defines.
- App-level genesis round trip of committed registry state: DR-07-02, DR-07-03 and DR-06-01. No test performs it, because it cannot pass on v0: `App.Export` refuses a non-empty catalogue, and InitChain refuses one. The refusal tests pass in [m-unit-app.txt](logs/goal-07/m-unit-app.txt). To rerun them alone: `env -C chain go test -count=1 -v -run '^(TestLaunchGenesisRejectsNonEmptyCatalog|TestExportRefusesANonEmptyCatalogue)$' ./app/`. Once the decisions are made, a test must InitChain a fresh app from the export of committed registry state and compare the full state.
- CI `devnet` job: the unchanged `bash scripts/devnet/prove-containers.sh` fails here at the image build (the build container does not trust the proxy CA). The copy with only the build step changed passed. The unchanged job passed in head CI (run 41438312010).
- golangci-lint: `bash -c 'golangci-lint version; cd chain && golangci-lint run ./...'` needs a golangci-lint built with Go 1.26 or later.
- Goal 06's mutation runner on the merged code: `.venv/bin/python docs/evidence/goal-06/mutants.py` refuses before running any test, because five anchors (`swap-engine-492`, `-548`, `-575`, `-578`, `-591`) target code the merge deleted ([m-goal06-anchors.txt](logs/goal-07/m-goal06-anchors.txt)). The five must be retired or skipped in the goal 06 runner first. It is not a goal 07 acceptance check.
- Post-docs checks: CI's `python` job (secret scan) and `contract` job (both checkers) run them on the evidence commit in the draft pull request; record those runs in the next goal 07 update.

## Decision requests

Interim choices are reversible. They are not approvals. All requests below are OPEN.

- DR-07-01 Hosting-intent record (DEC-001, security-critical). v0 has no intent schema, and `InnerSignature` has no role field. The interim, devnet only: the role name is inside the signed payload, verified against the key in the message. That proves integrity, not creator authorization. No chain id is in the payload. The withdraw intent is not bound to the register-intent key. Non-devnet chains are DECISION_BLOCKED. The approved DEC-001 default (disclosed host custody) does not define the record, and v0 rejects `ACCOUNT_HOST_DELEGATE`. Owner to decide the schema, the key custody on chain, the chain binding and the key binding.
- DR-07-02 Genesis catalogue. The launch genesis keeps providers, content and tombstones empty. May a reviewed launch genesis seed providers? Seeding ADMITTED providers bypasses council admission. This file reads approved DEC-009 (council-only admission) as answering that part: a launch genesis does not seed ADMITTED providers. The owner can reject that reading. Still open:
  - May a reviewed launch genesis seed providers that are not admitted (`PROVIDER_STATE_APPLIED`)? v0 `AppGenesis` carries `providers`; the interim refuses every genesis provider, which is stricter than v0.
  - May InitChain import an exported non-empty catalogue as a restart path? Goal 10 owns restarts.
- DR-07-03 Business ids are not in the v0 genesis schema, so an export drops replay protection (REQ-INV-003). Change the protocol to export them, or forbid restart from export. This sits beside DR-06-01, the authority side state.
- DR-07-04 New encodings. The goal text allows a new encoding as a new package version. v0 fixes `content_version` at 1, so a new encoding is a new `content_id`. Interim: the v0 rule, with the store keyed by (id, version).
- DR-07-05 Renewals after disablement. v0 `op_renew` has no provider-state check, so a DISABLED or DRAINING provider can renew existing leases. Keep or refuse? Goal 08 implements the answer.
- DR-07-06 Key separation. Should a provider signing key equal to a consensus key, a council key or another provider's key be rejected? Interim: not checked.
- DR-07-07 Content listing. v0 has only `Content` by (id, version). Add a paginated RPC, or leave listing to the goal 12 indexer?
- DR-07-08 RegisterProvider digest, proof of possession and admit binding. The business digest does not cover the signer, so a copy sent by another account would match it. Interim: such a copy is FORBIDDEN. Three questions, each a protocol change:
  - Should the digest cover the signer?
  - Should registration carry a proof-of-possession signature by `signing_pubkey`?
  - Should `AdmitProviderChange` bind the provider account (and key)? Today an approval binds only `provider_id`, and propose does not check that the record exists, so an approval can admit a record registered after it.
- DR-07-09 `GET /capability` signed envelope. `capability.Sign` and `capability.Verify` define a detached `InnerSignature` over `SVN-CAPABILITY-1`, and `Verify` binds the manifest to the provider record (id, current key, `endpoint_manifest_digest`). The wire envelope that carries the manifest and its signature is not in v0.
- DR-07-10 Bounds for provider history and queries (PROPOSED).
  - (a) `MaxRetiredKeysPerProvider` = 1024. A rotation past it is BOUNDS, after every spec check. The security consequence: under a capped provider id, the provider can no longer rotate away from a compromised signing key. The remedies are drain, council disable, and a new provider id that needs council admission again. `TestRotationCap` shows that update and drain still work at the bound. An alternative for the owner: prune old keys instead of capping them.
  - (b) The node default query gas limit `app.DefaultQueryGasLimit` = 20,000,000, equal to the transaction gas cap. It is configurable with `query_gas_limit` in `node.json`. Absent or 0 selects the default, never unlimited. Any positive value is accepted, including 2^64-1, the SDK's own unlimited default (`TestQueryGasLimitOption`); the node does not cap an operator's value. It is not consensus state. It applies to gRPC-routed queries only: ABCI query on a registered service path, and the node gRPC server.
  - (c) Raw store queries (`abci_query` on `/store/<store>/key` or `/store/<store>/subspace`) are not metered or paged by the SDK. A subspace query returns every row under a prefix and grows with the whole registry. `svnd` does not filter them. Operators must not expose the CometBFT RPC port publicly. Rejecting `/store` paths in the node is a follow-up.
- DR-07-11 Seat address roles at genesis (security-critical; REQ-SEC-002, with DR-06-06). RESOLVED on 2026-11-02, at about 11:34 UTC: the owner selected "Keep BROADCASTER-only (Recommended)", described as "Current interim rule: a seat address may appear in the submitter list only as BROADCASTER. It's tested and the devnet already complies. Stricter than the spec." `spec_machine.py` `validate_genesis_view` does not relate seat keys to submitter rows.
  - The risk: before this rule, a genesis could list a council seat address as `ADMITTED_SERVICE` or `ADMITTED_PROVIDER`. Since goal 07, that seat key could then register content or a provider and approve the admission of its own provider. As `AUTHORITY`, it could propose, approve and execute.
  - The interim, added with the goal 06 merge and stricter than the spec: `genesis.Validate` and the authority `ValidateGenesisState` refuse a seat address with any role but `BROADCASTER`. `genesis.Validate` matches the address case-insensitively. `TestSeatAddressIsBroadcasterOnly`, `TestSeatAddressRole`, `TestValidateGenesisState` and `TestSeatAddressMatchesGenesis` pass, and five mutants of the rule are killed. The shipped devnet genesis already lists seats as `BROADCASTER`.
  - The options were: (1) keep this rule; (2) require seat addresses to have no submitter row at all, once a seat-only admission path exists (DR-06-06; that needs code changes); or (3) allow shared roles, as the spec does, which gives seat keys registry reach. The owner chose (1).
- DR-06-06 (goal 06's request; note added by this merge). Seat accounts pass the ante only as `BROADCASTER` submitters. Goal 07 has no `BROADCASTER` handler: all six registry messages reject a broadcaster signer (`TestRegistryMessagesRejectOtherRoles`). Adopting the `protocol.md` relay column for provider or content messages would give seat keys registry reach. Goal 08 adds `BROADCASTER` lease handlers, so DR-06-06 needs an answer before goal 08 merges.

## Follow-ups

- Resolved by the merge: `go mod tidy -diff` is empty on this tree ([m-go-mod-tidy-diff.txt](logs/goal-07/m-go-mod-tidy-diff.txt)).
- Resolved: the owner acknowledged the model and effort substitution for goals 07-30 (see [Model and effort](#model-and-effort)).
- Goal 06's mutation runner. Its five anchors `swap-engine-492`, `-548`, `-575`, `-578` and `-591` target code the merge deleted by design. Retire or skip those five, then run the remaining 114 against goal 07: `.venv/bin/python docs/evidence/goal-06/mutants.py`. This step did not edit the goal 06 runner.
- `docs/evidence/goal-07/mutants.py` prints only the first 300 characters of each `failing=` field, so five of the fourteen merge-mutant lines are cut. When the full list matters, run it with `--logdir` and print the `--- FAIL` lines into the capture log, as [m-mutant-tx-context-full.txt](logs/goal-07/m-mutant-tx-context-full.txt) does for one mutant (its `--logdir` was a scratch directory, so only the printed lines are kept).
- The m05 overlay copy (`docs/evidence/goal-07/mutants/m05-authority-meter-finalize-only.go.txt`) no longer builds against the merged code. `mutants.py` replaces it for that mutant. Keep the file for history, or drop `run_mutant.sh` for m05.
- Reject raw `/store` ABCI query paths in the node, or document the CometBFT RPC exposure rule in the operator runbook (DR-07-10 (c)).
- Three texts overstate the query gas limit. Raw `/store` ABCI queries are not metered, and an operator may set 2^64-1, the SDK's unlimited value. Fix them in the next code change; this evidence does not change the tested tree.
  - `chain/app/app.go:122` says "Every query runs on a finite gas meter".
  - `chain/node/start.go:142` says `QueryGasLimit` is the gas "one ABCI or gRPC query may consume".
  - `chain/README.md:17` says every gRPC-routed query "runs on a finite gas meter".
- CI's `devnet` job (`bash scripts/devnet/prove-containers.sh`) ran unchanged in head CI and passed (run 41438312010). Optionally let the Dockerfile accept an extra CA bundle as a BuildKit secret, so the job can also run behind a TLS-intercepting proxy (a packaging change for a later goal).
- Add a test that drives the node gRPC server's out-of-gas path. `TestQueryGasLimit` covers the ABCI query and the query router only.
- Add a committed-block test for `TestGovernedLimitFailsClosed`. It checks RegisterContent through `Simulate` only, because its test hook commits store versions outside ABCI.
- Map unknown-field decode failures to `ERROR_CODE_UNKNOWN_FIELD` (8), or record that sdk/2 is the rule.
- `RotateServiceSubmitter` (`chain/x/authority/engine.go`) could move a service row onto a seat address that has no row. It has no non-test caller today (code reading, no log). Add the DR-07-11 seat check when it is wired.
- Leases and evidence need a store layout version like the registries' when they become real modules in goals 08-09.
- `svnd` has no query subcommands. Queries go through ABCI query and gRPC.
- OfferLease and AcceptLease must call `CheckNewObligation`, `CheckOfferable` and `CheckNotWithdrawn` in goal 08.
- The verbose app log is 7.4 MB of SDK debug output. A quieter test logger would shrink it.
- `main` (`f5916bd`) still checks the authority meter limit in simulation ([m-authority-enforce.txt](logs/goal-07/m-authority-enforce.txt)). Goal 07 fixes that when it reaches `main`.

## Residual risks

- Opaque ids and digests can still be linkable to a person or a host by anyone who holds the preimage. Hashing is not anonymization.
- Id-shaped submitter text is stored verbatim. A username placed in `content_id` is stored (`TestSubmitterTextFields`). A transaction that a handler rejects is still in its block. Only decode and ante rejections keep bytes out of blocks that an honest proposer builds.
- The hosting intent proves payload integrity under the key in the message. It does not prove that a creator authorized the host (DR-07-01).
- Registration has no proof of possession of `signing_pubkey`. A front-runner can register another provider's key and manifest digest under its own account. An `AdmitProviderChange` binds only `provider_id`, and propose does not check that the record exists. So the council can approve `admit-<id>` before any record exists, and the execute admits whichever record holds the id at execution. Seats must approve only after checking the record's `account`, key and group with `Query/Provider` (DR-07-08).
- A council seat key's reach depends on the interim genesis rule of DR-07-11 and on DR-06-06. Goal 08's `BROADCASTER` lease handlers would give seat keys lease reach unless DR-06-06 is answered first.
- There is no per-account limit on providers or content. DEC-005 allowances are unset. Gas meters every write. Export as a whole grows with the number of providers.
- A provider at the retired-key cap cannot rotate away from a compromised key (DR-07-10 (a)).
- Raw `/store` ABCI queries are unmetered and unpaged, and a subspace query grows with the whole registry. The CometBFT RPC port must not be public (DR-07-10 (c)).
- No test drives the node gRPC server's out-of-gas path, and the governed-limit failure is tested through simulation only.
- CAPACITY and OVERFLOW are tested only at keeper level until goal 08.
- The fourteen merge mutants are reconstructions of the merge workflow's scratch mutants, not the same text.
- Events are not part of the app hash. An indexer must not treat them as consensus state.
- The devnet is four processes on one machine. It is not an independent-operator result.

## Changed files

[m-diff-stat.txt](logs/goal-07/m-diff-stat.txt): `221 files changed, 60986 insertions(+), 421 deletions(-)` from `main` `f5916bd` to `b782e01`. That count includes goal 07's earlier evidence files and the 64 superseded logs.

- New modules: `chain/x/providers` (with `capability`), `chain/x/content`, and `chain/msgkit` (with `msgkittest`).
- `chain/svnerr`: codes 11-13 and a registry test.
- `chain/x/authority`: council hooks into the registries on goal 06's per-object store, one business-id namespace, a read surface, the simulation exception, and the seat-role genesis rule. Provider records moved out.
- `chain/genesis`: the seat-role rule in `genesis.Validate`.
- `chain/app`: wiring, the strict decoder, the envelope ante rule, the query gas limit, the scaffold-layout refusal, the catalogue refusal in export, snapshots, new test files, and a golden event fixture.
- `chain/node`: `query_gas_limit` in `node.json`.
- `chain/devnet`: provider and service accounts, the DEC-003 parameter, and `TestDevnetRegistryDemo`.
- `chain/go.mod`: tidy only ([m-gomod-vs-main.txt](logs/goal-07/m-gomod-vs-main.txt)). `chain/go.sum` is unchanged.
- Docs: `docs/architecture/registries.md`, `docs/architecture/authority-authorization.md`, `docs/operations/` notes, module READMEs, `chain/README.md`, and `scripts/evidence/capture.sh`.
- No change to `.proto` files, `docs/protocol/`, `docs/protocol.md`, goal pages or `.github/workflows/ci.yml` ([m-protocol-unchanged.txt](logs/goal-07/m-protocol-unchanged.txt)).

Evidence added by this run:

- this file, rewritten;
- `docs/evidence/goal-07/mutants.py`, and its `PY_ALLOW` entry with a one-line comment in `docs/evidence/goal-01/check_requirements.py`;
- the 74 `m-` logs under `docs/evidence/logs/goal-07/`: 73 from the evidence run and `m-mutant-tx-context-full.txt` from the evidence fixer;
- the goal 07 rows in `docs/progress.md` and `docs/progress.tsv`.

No earlier log was changed or deleted.

## Next safe action

1. The orchestrator commits this evidence. The evidence commit changes only paths under `docs/`, so `git diff b782e01 <evidence commit> -- . ':!docs'` must be empty. It carries `Tested-commit: b782e0149e1a70758b81b29929d12eab0da8a006` and `Tested-tree: f07ac7f6113f45a97167cf9c8139fb712227490e`.
2. Open the authorized draft pull request. CI on the evidence commit runs the secret scan (`python` job) and both contract checkers (`contract` job) on the tracked runner, the `m-` logs, this file and the progress rows; those results are shown in the pull request's checks.
3. The owner answers two things:
   - DR-07-01, DR-07-02 and DR-07-03. Or the owner accepts the module-level and fresh-state round trips and the proposed bounds for now. (The draft pull request and DR-07-11 were answered on 2026-11-02.)
4. Goal 08 builds on this merged revision. Answer DR-06-06 before goal 08's `BROADCASTER` handlers merge. Do not treat devnet seats or devnet operator groups as production policy.

## Proposed pull request

Title: Goal 07: provider and content registries (x/providers, x/content)

Review summary:

- Adds x/providers and x/content on the frozen v0 protos:
  - ownership checks, digest and size bounds;
  - one business-id namespace with replay protection;
  - a capped retired-key history and withdrawal tombstones;
  - paginated provider queries and typed events.
- Builds on merged goal 06 (`main` `f5916bd`). Council hooks sit on the per-object authority store, in a cache that is written only on success. Goal 06's D1 router block is lifted for the six registry messages only.
- Admission, disablement and forced withdrawal stay council-only (DEC-009). Production content registration stays DECISION_BLOCKED until a hosting-intent record exists (DR-07-01). A genesis seat address may be only a `BROADCASTER` submitter (DR-07-11, kept by the owner on 2026-11-02).
- Sets a default query gas limit of 20,000,000 for gRPC-routed queries; 0 or absent no longer means unlimited. Raw `/store` queries stay unmetered, so the CometBFT RPC port must not be public.
- Fixes authority simulation. `main` still checks the meter limit in simulation (code, [m-authority-enforce.txt](logs/goal-07/m-authority-enforce.txt)), so simulated authority messages there would be BOUNDS. This was not run on `main`.
- Evidence on tree `f07ac7f6`:
  - unit suites, and a four-process single-machine devnet demo with a replayed app hash;
  - `-race` for `./chain/...` and eleven real fuzz runs;
  - mutation runs: 5 of 5 fix-round mutants and 14 of 14 merge mutants killed;
  - Python and Node gates, and both contract checkers before the docs edit.
- Status BLOCKED:
  - production REQ-TX-002 (DR-07-01);
  - the app-level round trip of committed registry state (DR-07-02, DR-07-03, DR-06-01);
  - not blocking the status, but not run here: the unchanged container devnet job fails here at the image build (a copy with only the build step changed passed, and the unchanged job passed in head CI, run 41438312010); golangci-lint is unusable here; goal 06's mutation runner refuses on the merged code (five anchors target deleted code).
- Head CI passed on the evidence commit `45ac279` (run 41438312010, all five jobs), including the secret scan and both contract checkers.
- REQ-HOST-004 is VERIFIED for the approved DEC-002 default. Ten goal 07 decision requests are open; DR-07-11 was resolved on 2026-11-02.
