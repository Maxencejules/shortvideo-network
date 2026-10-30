# AGENTS.md

Preserve stricter existing project instructions. Each goal relies on this contract rather than repeating it in full.

## Scope and authority

Build the permissioned community beta described in docs/blueprint.md. The original PDF remains the source for ambiguous extraction. Use a real Cosmos SDK / CometBFT application chain, not a simulated ledger. Keep media, personal data and ordinary social interactions off-chain. No tradable token, new consensus algorithm, proprietary-code copying, mandatory commercial cloud, or unreviewed cryptography. Treat specifications and numeric targets as proposals until approved; never claim an audit or production readiness.

## Context and changes

Before a task, read this AGENTS.md, docs/progress.md, approved decisions and only the relevant specification sections. Verify the repository, branch and base commit; preserve unrelated work. Implement the current goal in small reviewable changes. Do not merge, force-push, publish releases, buy services, change access, or deploy publicly without explicit approval. Identify security-critical ambiguity and request a decision; document reversible implementation choices without presenting them as user approval.

## Cloud and credentials

Inspect the actual runtime and network permissions. Never assume Docker, browser automation, extra disk, external peer ports or a running service exists. Do not bypass sandbox or network restrictions. Use synthetic fixtures and fresh development-only keys; never request live validator, governance or production signing keys. Keep secrets, key files, media uploads and database dumps out of Git, logs and shared environment templates. Prepare reproducible external test instructions when the workspace cannot run a required check.

## Implementation discipline

Pin compatible dependency versions and verify APIs against official documentation. Use typed interfaces, explicit error handling, bounded work, object authorization and retry-safe state transitions. Chain execution must be deterministic and free of network calls or local-clock decisions. Provider signatures are not interchangeable with service signatures. A broadcast acknowledgement is not committed execution success. Mocking is allowed in unit tests only; never use it as end-to-end evidence.

## Verification and status

Test every change, including relevant negative cases. Never delete or weaken a requirement, skip a failing check silently, replace an implementation with a stub, or invent logs to obtain a pass. Record the commit, runtime, commands, exit codes, test counts and artifact locations. Distinguish IMPLEMENTED, VERIFIED, BLOCKED and DEFERRED. A skipped or unavailable required test is not passed. Keep immutable evidence for the tested code revision and rerun affected checks after fixes.

## Handoff

Update docs/progress.md and docs/evidence/goal-NN.md. Return changed files, verified outcomes, failures, unrun tests, residual risks and the next safe action. Maintain the requirements-to-tests matrix. End with a proposed pull-request title and concise review summary; create the PR only when authorized. Do not declare the whole project finished because the current goal is done.

Project commands and schemas referenced in the goals are interfaces to implement and test, not commands asserted to exist already. Empty directories, generated stubs and mocked services are never end-to-end verification.

## Model substitution

The playbook names Codex models (Astra and GPT-6.1 Sol) and effort levels. This repository is built by Cursor cloud agents instead. Extra High goals (01-02 and 29-30) use the highest reasoning setting available.

Every docs/evidence/goal-NN.md must record the model actually used, the effort or reasoning setting actually used, and the playbook's assigned model and effort for that goal. Never claim a model or an effort setting was used that was not. Extra effort does not certify correctness.

Unknown is allowed for the effort setting only when the playbook assigns no setting for that goal, as with goal 00. Goals 01-30 have assigned settings. For those goals, record the setting that was actually used. If it was not reported or could not be selected, log the goal as BLOCKED on model allocation. Do not mark it VERIFIED.
