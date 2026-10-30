GOAL 23 / 30



Moderation, blocking and removal
  GPT-6.1 Sol                                                                       EFFORT: HIGH

Prerequisites: Goals 17-22; approved community policy and retention decisions.
Blueprint reference: pp. 4-5, 14, 16-21, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 23. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Complete the safety workflows required before invited public use and make removal promises technically
precise.
IMPLEMENTATION REQUIREMENTS

1. Implement reporting, account blocking, an administrator queue, decisions with reasons, appeals and
    escalation status. Enforce moderator permissions and audit administrative actions; sensitive
    complainant/evidence data stays access-controlled and off-chain.
2. Apply local policy consistently at upload admission, publication, feeds/search, comments, storage admission
    and clients. A committed transaction cannot override a community's right to stop distributing content on
    its services.
3. Implement removal as coordinated, retryable delisting, controlled-cache invalidation, participating-host
    deletion and lease cancellation/end according to approved terms. Stop repair/renewal from reviving
    withdrawn content. During a chain stall, local delisting must not wait for new finality.
4. Track each cooperating service's acknowledgement and unreachable hosts separately. Disclose
    independently retained copies and historical ledger records that cannot be erased by this operation.
    Preserve only records permitted by the approved retention policy.
5. Provide configurable policy/contact pages and operational runbooks with placeholders for real owners, not
    fabricated legal approval. Keep public launch blocked until policy, incident coverage, age rules and
    rights/privacy review are approved.
ACCEPTANCE CHECKS

Test unauthorized moderator actions, confidential-case access, spam reports, block interactions, appeals and
account takedown. Verify all UI report/block actions have real backend effects.
Remove content while it is uploading, processing, replicating, being repaired and being played. Test offline
providers and a stalled chain; report incomplete deletion honestly.
Measure the proposed five-minute controlled-service delisting gate with clocks, affected endpoints and cache
evidence. An unresponsive independent host is not a successful deletion.
REQUIRED HANDOFF

Deliver safety workflows, an operator runbook and removal coverage results. Separate software completion
from professional policy/legal approval.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                29 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                               CODEX CLOUD + GITHUB



