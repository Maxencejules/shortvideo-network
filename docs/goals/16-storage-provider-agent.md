GOAL 16 / 30



Storage-provider agent
  GPT-6.1 Sol                                                                       EFFORT: HIGH

Prerequisites: Goals 07-09, 11-15; provider service and signing contracts.
Blueprint reference: pp. 9, 13-16, 19, 22 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 16. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Implement the independently installable Go provider agent with explicit hosting consent and bounded local
resources.
IMPLEMENTATION REQUIREMENTS

1. Create agents/provider with enrollment, signed capability/endpoint manifests, offer handling, authenticated
    package transfer, inventory, HTTPS serving and health/status endpoints. Use approved service identities
    and per-workspace test keys; keep real provider keys outside the development agent.
2. Check operator policy, actual available disk and configured quotas before staging data. Validate every
    transferred package/segment digest and byte bound, then sign exact accepted terms. Submit or forward
    acceptance through the approved provider-signing path; a service cannot manufacture it.
3. Persist local offer/transfer/lease state and reconcile committed obligations after restart. Separate STAGED,
    ACCEPTANCE_PENDING, COMMITTED and SERVING readiness, following the approved state machine.
    Apply real-time deadlines and expose uncertainty during chain stalls.
4. Defend all remote fetches against SSRF, path traversal, unsafe redirects and unbounded responses. Permit
    private devnet destinations only through explicit operator configuration, never by disabling checks
    globally.
5. Implement inventory/audit responses, bandwidth/storage limits, key-rotation hooks, local removal
    enforcement and the initial drain mode. Do not count several identities on one machine as independent
    ownership or physical redundancy.
ACCEPTANCE CHECKS

Install two isolated test agents, transfer real packages and verify each acceptance is bound to its own key and
exact terms. Reject wrong signatures, altered bytes, unapproved offers, insufficient disk and repeated
requests.
Restart after partial transfer and after committed acceptance; verify correct inventory, cleanup and capacity.
Test a chain stall, lease deadline and local withdrawal.
Attempt unauthorized admin access and malicious endpoint/path fixtures inside the permitted test
environment.
REQUIRED HANDOFF

Return a documented agent install, protocol conformance tests and real serving evidence. Identify which
independence tests still require another person or machine.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                22 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



