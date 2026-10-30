GOAL 29 / 30



Adversarial review and fault testing
  GPT-6 Astra                                                                      EFFORT: EXTRA HIGH

Prerequisites: Goals 01-28; exact candidate revision and evidence inventory.
Blueprint reference: pp. 10, 12-15, 19, 26-28 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 29. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Independently challenge the candidate implementation and its tests, then fix verified defects with regression
evidence.
IMPLEMENTATION REQUIREMENTS

1. Begin by inspecting code, approved invariants and trust boundaries before relying on earlier completion
    summaries. Review chain authorization, validator lifecycle, lease accounting, replay/idempotency,
    custody/recovery, API object permissions, media sandboxing, SSRF and supply-chain workflows.
2. Use unit/property/fuzz/contract tests plus controlled fault injection on owned test systems. Exercise
    dishonest providers/auditors, repeated IDs, stale evidence, outsourcing assumptions, partial writes,
    quorum loss, corrupted packages, blocked removals and compromised test service keys.
3. Inspect tests for mocks in end-to-end paths, swallowed errors, skipped cases, permissive assertions, leaked
    secrets and unjustified expected failures. Search for stubs, fake metrics, TODOs in critical paths and stale
    feature flags.
4. For each finding, record severity rationale, affected code, preconditions, a safe local reproduction, impact
    and fix proposal. Implement bounded fixes with regression tests, then rerun affected integration suites at
    the new candidate revision.
5. Separate model-assisted review from an independent human security audit. Do not claim unrestricted
    permissionless security, cryptographic proof guarantees or protection from all hostile operators. Preserve
    unresolved blockers and required outside reviews.
ACCEPTANCE CHECKS

Confirm rejected chain operations leave state unchanged, replay hashes match at the same height, and partial
failures do not create false commitment or duplicate reservations.
Rerun host-loss/repair, chain-stall and deletion-during-repair scenarios. Verify failures remain visible to clients
and operators.
Produce a finding-by-finding closure matrix; a finding is closed only with code evidence and an executed
regression test or a clearly approved scope decision.
REQUIRED HANDOFF

Return the reviewed revision, fixed/open findings and remaining external review needs. Prepare an
evidence-based release assessment, not a generic security endorsement.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                     35 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                        CODEX CLOUD + GITHUB



