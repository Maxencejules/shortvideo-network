GOAL 26 / 30



Backups, restoration and signer safety
  GPT-6.1 Sol                                                                       EFFORT: HIGH

Prerequisites: Goals 10, 18-25; approved retention, key custody and recovery policy.
Blueprint reference: pp. 12, 17, 20, 22, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 26. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Implement and rehearse recovery without conflating live replication with backup or risking duplicate validator
signing.
IMPLEMENTATION REQUIREMENTS

1. Create documented backups for application databases, signed records, relevant configuration, critical media
    and index reconstruction. Encrypt sensitive backups, constrain access and define retention and restore
    order. Exclude live secrets from ordinary CI artifacts.
2. Implement a disposable restore harness on clean services/hosts with the original application and media
    origin unavailable. Rebuild chain projections from a trusted checkpoint or supported canonical state and
    verify application/media references.
3. Document chain-state and signer-safety metadata handling for the selected implementation. Require the
    old signer to be stopped and verified before replacement. Never run duplicate consensus identities, roll
    signer safety backwards or treat a stale snapshot as permission to sign conflicting state.
4. Rehearse service-key rotation, account recovery, corrupted-backup detection, missing media and partial
    restore. Explain migration/version compatibility and which historical data cannot be reconstructed from
    current replicas alone.
5. Document orderly end-of-service exports, stopped admissions, lease handoff and controlled deletion.
    Production restore or key rotation requires explicit operator approval; use only test keys here.
ACCEPTANCE CHECKS

Restore the database, media and index onto a clean approved runner and repeat
publication/playback/removal checks. Compare digests and authorization state before/after.
Measure actual recovery time and recoverable data age for the test; label them observed RTO/RPO, not
service guarantees.
Attempt corrupted, wrong-version and incomplete restore fixtures. Verify duplicate-live-signer prevention in
the controlled test and record any unverifiable external steps.
REQUIRED HANDOFF

Deliver tested backup/restore commands, compatibility notes and signer-safety evidence. List manual
approvals and independent review required before live recovery.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                32 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                               CODEX CLOUD + GITHUB



