GOAL 10 / 30



Policy governance and chain upgrades
  GPT-6 Astra                                                                      EFFORT: HIGH

Prerequisites: Goals 06-09; migration and governance specifications approved.
Blueprint reference: pp. 8-12, 21-22, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 10. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Complete x/policy and the governed change/upgrade path while preserving the established authority and
accounting invariants.
IMPLEMENTATION REQUIREMENTS

1. Extend the authority proposal primitives with typed parameter changes, admission limits, policy versions
    and scheduled activation. Validate unique approvals, threshold, expiry and review delay. Protect
    transaction sizes, gas, quotas, lease bounds and resulting validator policies against unsafe updates.
2. Implement the approved chain-upgrade mechanism using supported framework components. Specify
    binary/version compatibility, module migration versions, activation point, preconditions and failure
    behaviour. Do not add commercial-only components without explicit licensing approval.
3. Create a rehearsal harness from representative devnet state containing providers, active/expired leases,
    evidence and pending proposals. Compare pre/post invariants and verify application/indexer schema
    compatibility.
4. Write runbooks for preparing a release, reviewing source/digests, operator installation, planned halt/restart
   and failed upgrades. Explain when rollback is unsafe after state migration. A governance stall cannot be
   fixed by an unaudited finality bypass.
5. Separate local emergency controls, protocol changes and public publication authority. Do not auto-approve
    policies, tag releases, rotate live keys or change real operators as part of this task.
ACCEPTANCE CHECKS

Test duplicate votes, removed council seats, invalid bounds, concurrent proposals, activation timing and
insufficient approval. Assert unauthorized policy writes fail at execution.
Rehearse a successful migration and a deliberately incompatible binary on disposable state; verify controlled
failure without resetting history. Check exports, restart and replay at the specified heights.
Confirm the same tested chain revision still passes the authority, lease and evidence suites.
REQUIRED HANDOFF

Return governed change fixtures, migration evidence and operator instructions. List every step that still needs
a human approval or independent security review.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                               16 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



