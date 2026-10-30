# Progress

Tracker for goals 01-30. Goal 00 is this companion pack. It is not one of the thirty implementation goals.

## Status values

`IMPLEMENTED`, `VERIFIED`, `BLOCKED` and `DEFERRED` are different statuses.

- **IMPLEMENTED** — the goal's changes are in the branch, and its required checks have not all been run against the reported revision.
- **VERIFIED** — the goal's required checks actually ran against the reported revision. The evidence file records that revision, the commands, the exit codes and the results.
- **BLOCKED** — a required check, dependency, environment or decision is unavailable. A missing test environment is BLOCKED, never a pass.
- **DEFERRED** — the work is intentionally postponed, and the reason is recorded.

`NOT STARTED` means the goal has not been taken up. It is not one of the four evidence statuses.

A goal is verified only when its required checks actually ran against the reported revision. Never convert a missing test environment into a pass.

`docs/progress.tsv` is the machine-readable tracker. It currently contains the header row only.

## Goals

| Goal | Deliverable | Status | Evidence |
| --- | --- | --- | --- |
| 01 | [Requirements, scope and threat model](goals/01-requirements-scope-and-threat-model.md) | NOT STARTED | |
| 02 | [Protocol schemas and invariants](goals/02-protocol-schemas-and-invariants.md) | NOT STARTED | |
| 03 | [Monorepo, toolchains and CI](goals/03-monorepo-toolchains-and-ci.md) | NOT STARTED | |
| 04 | [Codex Cloud development environment](goals/04-codex-cloud-development-environment.md) | NOT STARTED | |
| 05 | [Chain application, genesis and devnet](goals/05-chain-application-genesis-and-devnet.md) | NOT STARTED | |
| 06 | [Authority and validator lifecycle](goals/06-authority-and-validator-lifecycle.md) | NOT STARTED | |
| 07 | [Provider and content registries](goals/07-provider-and-content-registries.md) | NOT STARTED | |
| 08 | [Lease lifecycle and capacity accounting](goals/08-lease-lifecycle-and-capacity-accounting.md) | NOT STARTED | |
| 09 | [Beta attestations and audit evidence](goals/09-beta-attestations-and-audit-evidence.md) | NOT STARTED | |
| 10 | [Policy governance and chain upgrades](goals/10-policy-governance-and-chain-upgrades.md) | NOT STARTED | |
| 11 | [Transaction broadcaster and outbox](goals/11-transaction-broadcaster-and-outbox.md) | NOT STARTED | |
| 12 | [Committed-state indexer and reconciliation](goals/12-committed-state-indexer-and-reconciliation.md) | NOT STARTED | |
| 13 | [Authenticated uploads and quarantine](goals/13-authenticated-uploads-and-quarantine.md) | NOT STARTED | |
| 14 | [Isolated video processing workers](goals/14-isolated-video-processing-workers.md) | NOT STARTED | |
| 15 | [Media manifests and HTTPS playback](goals/15-media-manifests-and-https-playback.md) | NOT STARTED | |
| 16 | [Storage-provider agent](goals/16-storage-provider-agent.md) | NOT STARTED | |
| 17 | [Replication, monitoring and repair](goals/17-replication-monitoring-and-repair.md) | NOT STARTED | |
| 18 | [End-to-end network milestone](goals/18-end-to-end-network-milestone.md) | NOT STARTED | |
| 19 | [Accounts, recovery and data portability](goals/19-accounts-recovery-and-data-portability.md) | NOT STARTED | |
| 20 | [Viewer web application](goals/20-viewer-web-application.md) | NOT STARTED | |
| 21 | [Creator publishing interface](goals/21-creator-publishing-interface.md) | NOT STARTED | |
| 22 | [Social features, search and feeds](goals/22-social-features-search-and-feeds.md) | NOT STARTED | |
| 23 | [Moderation, blocking and removal](goals/23-moderation-blocking-and-removal.md) | NOT STARTED | |
| 24 | [Operator dashboard and node onboarding](goals/24-operator-dashboard-and-node-onboarding.md) | NOT STARTED | |
| 25 | [Observability, quotas and usage controls](goals/25-observability-quotas-and-usage-controls.md) | NOT STARTED | |
| 26 | [Backups, restoration and signer safety](goals/26-backups-restoration-and-signer-safety.md) | NOT STARTED | |
| 27 | [Packaging, installation and release preparation](goals/27-packaging-installation-and-release-preparation.md) | NOT STARTED | |
| 28 | [Performance, load and capacity evidence](goals/28-performance-load-and-capacity-evidence.md) | NOT STARTED | |
| 29 | [Adversarial review and fault testing](goals/29-adversarial-review-and-fault-testing.md) | NOT STARTED | |
| 30 | [Final acceptance and independent installation](goals/30-final-acceptance-and-independent-installation.md) | NOT STARTED | |
