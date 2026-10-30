GOAL 24 / 30



Operator dashboard and node onboarding
  GPT-6.1 Sol                                                                      EFFORT: HIGH

Prerequisites: Goals 06-12, 16-17, 19, 23; stable operator APIs.
Blueprint reference: pp. 4, 12-15, 19-22 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 24. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Build a real operator interface for bounded participation, governance visibility and orderly exit.
IMPLEMENTATION REQUIREMENTS

1. Implement role-specific onboarding for storage/delivery, processing, account/discovery and validators. Let
    operators choose roles independently. Document machine/network requirements as measured
    observations, not universal minimums.
2. Show actual disk allocation, leases, inventory, job queues, bandwidth counters, evidence mode, chain/index
    height, overdue work and software version. Distinguish configured, measured, pending and stale values; do
    not create decorative fake metrics.
3. Provide authorized capacity/policy changes, stop-new-offers controls, drain/handoff, renewal review and
    local endpoint disablement. Make dangerous actions explicit and auditable; the dashboard cannot bypass
    chain authorization or commit an unapproved governance action.
4. Display council proposals, approval state, activation timing and upgrade instructions without handling live
    consensus private keys in the browser. Keep public/read-only endpoints separate from signing/admin
    endpoints.
5. Expose local moderation/report queues and health diagnostics with least privilege. Show operator
    ownership/failure-domain declarations accurately and label single-machine demos as such.
ACCEPTANCE CHECKS

Onboard a fresh test provider and observe real package acceptance and delivery. Change limits, exhaust
capacity, drain the provider and verify handoff/lease accounting.
Exercise viewer/provider/moderator/council role separation and deny cross-role actions. Confirm stale chain
or index data is visible rather than silently presented as current.
Run browser tests and a manual independent-operator walkthrough where an operator is available; leave that
evidence blocked when no independent person participated.
REQUIRED HANDOFF

Deliver the dashboard, tested onboarding and exit flows, and a role/permission map. Record operational tasks
that still require an administrator.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                               30 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                 CODEX CLOUD + GITHUB



