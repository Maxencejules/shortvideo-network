# Progress

Tracker for goal 00 and goals 01-30. Goal 00 is the companion pack. It is not one of the thirty implementation goals.

Public launch: DISABLED

No tradable token, coin, sale, or mining. This tracker promises no dates, no token totals, no market adoption, and no production security.

## Status values

`IMPLEMENTED`, `VERIFIED`, `BLOCKED` and `DEFERRED` are different statuses.

- **IMPLEMENTED** — the goal's changes are in the branch, and its required checks have not all been run against the reported revision.
- **VERIFIED** — the goal's required checks actually ran against the reported revision. The evidence file records that revision, the commands, the exit codes and the results.
- **BLOCKED** — a required check, dependency, environment or decision is unavailable. A missing test environment is BLOCKED, never a pass.
- **DEFERRED** — the work is intentionally postponed, and the reason is recorded.

`NOT STARTED` means the goal has not been taken up. It is not one of the four evidence statuses.

A goal is verified only when its required checks actually ran against the reported revision. Never convert a missing test environment into a pass.

Open decisions in `docs/decision-register.md` are explicit blockers on the requirements that depend on them. They do not, by themselves, change a goal's status from NOT STARTED. A later goal that needs an approved answer stays blocked on that decision until the owner records one. Goal 02's prerequisite is satisfied only in the blocked form: the decisions are explicitly blocked, not approved.

`docs/progress.tsv` is the machine-readable tracker. It has one row for every goal whose status is not `NOT STARTED`. Goals that are still `NOT STARTED` appear only in the table below.

## Goals

| Goal | Deliverable | Status | Evidence | Depends on |
| --- | --- | --- | --- | --- |
| 00 | Companion pack | VERIFIED | [goal-00.md](evidence/goal-00.md) | None. Base for later goals. |
| 01 | [Requirements, scope and threat model](goals/01-requirements-scope-and-threat-model.md) | VERIFIED | [goal-01.md](evidence/goal-01.md), [PR 4](https://github.com/Maxencejules/shortvideo-network/pull/4) | Blueprint in the repository. Goal 00 merged at `a821cc520060513c7a00f773e6cff28a1b83a92d`. |
| 02 | [Protocol schemas and invariants](goals/02-protocol-schemas-and-invariants.md) | VERIFIED | [goal-02.md](evidence/goal-02.md) | Goal 01. DEC-001 through DEC-008 explicitly blocked, not approved. Protocol v0 is frozen-pending-Gate-A. |
| 03 | [Monorepo, toolchains and CI](goals/03-monorepo-toolchains-and-ci.md) | NOT STARTED | | Goals 01-02. Dependency pins are still unselected. DEC-006 blocks the project licence. |
| 04 | [Codex Cloud development environment](goals/04-codex-cloud-development-environment.md) | NOT STARTED | | Goal 03. Cloud environment access. |
| 05 | [Chain application, genesis and devnet](goals/05-chain-application-genesis-and-devnet.md) | NOT STARTED | | Goals 02-04. A compatible SDK and CometBFT pair confirmed from current docs. |
| 06 | [Authority and validator lifecycle](goals/06-authority-and-validator-lifecycle.md) | NOT STARTED | | Goal 05. DEC-008 blocks production council parameters. |
| 07 | [Provider and content registries](goals/07-provider-and-content-registries.md) | NOT STARTED | | Goals 02, 05-06. DEC-002 blocks the operator list. |
| 08 | [Lease lifecycle and capacity accounting](goals/08-lease-lifecycle-and-capacity-accounting.md) | NOT STARTED | | Goal 07. Lease terms from Goal 02. DEC-005 blocks durations and allowances. |
| 09 | [Beta attestations and audit evidence](goals/09-beta-attestations-and-audit-evidence.md) | NOT STARTED | | Goal 08. Evidence stays an attestation. |
| 10 | [Policy governance and chain upgrades](goals/10-policy-governance-and-chain-upgrades.md) | NOT STARTED | | Goals 06-09. DEC-008 blocks the live parameter set. |
| 11 | [Transaction broadcaster and outbox](goals/11-transaction-broadcaster-and-outbox.md) | NOT STARTED | | Goals 04, 07-10. Signing boundaries from Goal 02. DEC-001 blocks host custody of user keys. |
| 12 | [Committed-state indexer and reconciliation](goals/12-committed-state-indexer-and-reconciliation.md) | NOT STARTED | | Goal 11. Real chain queries and PostgreSQL. |
| 13 | [Authenticated uploads and quarantine](goals/13-authenticated-uploads-and-quarantine.md) | NOT STARTED | | Goals 02, 04, 11-12. Development principals until Goal 19. |
| 14 | [Isolated video processing workers](goals/14-isolated-video-processing-workers.md) | NOT STARTED | | Goal 13. A runnable worker environment. |
| 15 | [Media manifests and HTTPS playback](goals/15-media-manifests-and-https-playback.md) | NOT STARTED | | Goal 14. Package contract and origin storage. |
| 16 | [Storage-provider agent](goals/16-storage-provider-agent.md) | NOT STARTED | | Goals 07-09 and 11-15. |
| 17 | [Replication, monitoring and repair](goals/17-replication-monitoring-and-repair.md) | NOT STARTED | | Goal 16 and Goal 12 read models. Public replication stays off. |
| 18 | [End-to-end network milestone](goals/18-end-to-end-network-milestone.md) | NOT STARTED | | Goals 05-17, as far as their runtimes allow. |
| 19 | [Accounts, recovery and data portability](goals/19-accounts-recovery-and-data-portability.md) | NOT STARTED | | Goal 18. DEC-001, DEC-004, and DEC-007 block custody, retention, and audience claims. |
| 20 | [Viewer web application](goals/20-viewer-web-application.md) | NOT STARTED | | Goals 15, 18-19. |
| 21 | [Creator publishing interface](goals/21-creator-publishing-interface.md) | NOT STARTED | | Goals 13-20. DEC-006 blocks the creator rights text. |
| 22 | [Social features, search and feeds](goals/22-social-features-search-and-feeds.md) | NOT STARTED | | Goals 19-21. Privacy rules from DEC-004 and DEC-007. |
| 23 | [Moderation, blocking and removal](goals/23-moderation-blocking-and-removal.md) | NOT STARTED | | Goals 17-22. DEC-003 and DEC-004 block policy and retention. |
| 24 | [Operator dashboard and node onboarding](goals/24-operator-dashboard-and-node-onboarding.md) | NOT STARTED | | Goals 06-12, 16-17, 19, 23. DEC-002 blocks independence claims. |
| 25 | [Observability, quotas and usage controls](goals/25-observability-quotas-and-usage-controls.md) | NOT STARTED | | Goals 18-24. DEC-005 blocks allowance numbers. |
| 26 | [Backups, restoration and signer safety](goals/26-backups-restoration-and-signer-safety.md) | NOT STARTED | | Goals 10, 18-25. DEC-001 and DEC-004 block recovery policy and backup scope. |
| 27 | [Packaging, installation and release preparation](goals/27-packaging-installation-and-release-preparation.md) | NOT STARTED | | Goals 03-26. DEC-006 blocks the licence. Do not publish a release. |
| 28 | [Performance, load and capacity evidence](goals/28-performance-load-and-capacity-evidence.md) | NOT STARTED | | Goal 27. Page 26 numbers stay PROPOSED gates. |
| 29 | [Adversarial review and fault testing](goals/29-adversarial-review-and-fault-testing.md) | NOT STARTED | | Goals 01-28 and a frozen candidate revision. |
| 30 | [Final acceptance and independent installation](goals/30-final-acceptance-and-independent-installation.md) | NOT STARTED | | Goal 29. Public launch stays disabled until a separate release decision. |

## Blueprint stages and goal order

The blueprint's stage list is an evidence milestone map. Implementation order is the goal table above. That mapping is contradiction CR-002 in `docs/requirements.md`. It does not add stage-6 features to the beta.

| Blueprint stage | Goals that produce its evidence | Release boundary |
| --- | --- | --- |
| 0. Requirements | 01 | This specification. Not an approved protocol. |
| 1. Media vertical slice | 13-15, with development principals until 19, and the joined path at 18 | Login as a product capability is goal 19. |
| 2. Custom chain | 05-10 | Four equal-power development validators. |
| 3. Distributed hosting | 11-17 | Designated hosts and HTTPS. Not public replication. |
| 4. Community beta | 19-25 | Invited accounts. Policy decisions stay OPEN. |
| 5. Release hardening | 26-30 | Checks and packaging. Public launch disabled. |
| 6. Expansion | None of goals 01-30 | Mobile, peer assistance, feed plugins, deeper portability, and reviewed proofs stay deferred. |

Critical path recorded for later goals: the manifest schema (goal 02) precedes lease commitments (goal 08). The encoder (goal 14) precedes playback (goal 15) and the joined milestone (goal 18). Removal limits are specified in goal 01 before any replication work, and public replication stays off until removal policy is decided. Custody is an open decision before any portability claim. Retrieval measurements precede any peer-delivery or proof work, and both of those stay deferred. Authority and lease invariants exist in the plan before any financial consequence, and the beta has no financial consequence.

## Test environments

Three environment classes are distinct. A result from one class does not satisfy a test that asks for another. One workstation can host many processes and remains one failure domain and one operator.

### Cloud commands

These need a hosted CI or cloud-environment command, not only a local file check.

- Goal 03 GitHub Actions jobs for format, lint, type-check, schema generation, and unit tests, including the untrusted-pull-request permission checks.
- Goal 04 inspection and startup of the cloud development profile.
- Goal 27 release-candidate workflow. That workflow must not publish a release, tag, or public deployment from an unapproved run.
- Later goals may re-run unit tests in CI. CI success is not end-to-end evidence and is not an independent-operator result.

### Fuller Linux test runner

These need a machine that can run the pinned toolchains and several real processes. Containers are one way. A minimal cloud profile that cannot run them leaves the test BLOCKED.

- Goal 04 full profile: PostgreSQL, the job queue, and the service processes.
- Goals 05-10: four validator processes with distinct data directories and peer identities.
- Goals 11-12: broadcaster, indexer, and a real chain.
- Goals 13-15: quarantine storage and isolated FFmpeg workers.
- Goals 16-18: provider agents, replication, and the joined upload-to-playback path.
- Goal 26 restore harness on clean hosts.
- Goal 28 shaped-network, browser, and load runs. The blueprint design point is a PROPOSED gate, not a result.
- Goal 29 fault injection that does not fit the minimal cloud profile.

### Independent machines

These need a separate person and separate infrastructure. Extra processes on the same host do not qualify.

- Goal 24 manual onboarding walkthrough when an independent operator is available. Until then that evidence is BLOCKED.
- Goal 30 installation by a second operator on separate infrastructure, and a second client.
- Any statement that replicas, auditors, or validators are independent operators (goals 17, 18, and 29 may inject faults among local processes, and must label that as process fault injection).

Goal 01's checks are document checks in this workspace. They do not use a chain, a browser, a load runner, or a second machine. They do not verify those later tests.

## Ownership

Accountable roles from the blueprint are functions: product/application, chain/security, media/provider agents, deployment/on-call, and trust/safety. No person is assigned. One person may cover several roles later. That staffing choice is not made here, and no date is attached to it.

## Non-promises

This tracker does not state a launch date, a staffing plan, a token supply, a market size, an adoption forecast, a durability percentage, a legal conclusion, or production security. Numeric targets in the blueprint remain PROPOSED gates for the goals that measure them.
