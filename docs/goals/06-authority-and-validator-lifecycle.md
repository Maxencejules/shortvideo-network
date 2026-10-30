GOAL 06 / 30



Authority and validator lifecycle
  GPT-6 Astra                                                                       EFFORT: HIGH

Prerequisites: Goal 05 and the approved authorization/governance contracts.
Blueprint reference: pp. 8-12, 19, 21 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 06. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Implement x/authority as the sole source of permissioned validator membership and authorized chain-write
roles.
IMPLEMENTATION REQUIREMENTS

1. Implement typed council seats, submitter roles, provider/service admission, proposal IDs, unique approvals,
    expiry and scheduled validator changes. Use the approved threshold/delay rules; implement the minimal
    proposal machinery needed here, leaving broader policy/upgrades to Goal 10.
2. Connect updates to the exact initialization and block lifecycle of the selected SDK/CometBFT release.
    Specify and test when a proposed public key or power change becomes effective. No staking or other
    module may overwrite the resulting set.
3. Enforce authorization, signature verification, replay/sequence rules, transaction size, gas and deterministic
    quotas in committed execution as well as admission checks. A permissive mempool must not bypass
    consensus-critical validation.
4. Validate resulting sets for duplicate keys, invalid powers, overflow, removal of all voting power and
    governance-policy violations. Test consensus-key rotation separately from provider, service and
    council-key rotation.
5. Document ordinary admission/removal, emergency local endpoint disablement and loss-of-quorum
    recovery. Keep governance approvals distinct from consensus votes. No administrator endpoint may
    fabricate a committed decision or override finality.
ACCEPTANCE CHECKS

Exercise approved and rejected proposals, repeated votes, stale approvals, unauthorized submitters, wrong
signatures and expired execution. Confirm no state mutation from rejected messages.
On the real devnet, add/rotate/remove a test validator and compare the effective set at the prescribed
heights. Verify one-third/quorum behaviour for the resulting voting powers, not just node count.
Fuzz bounded parser/state-transition inputs and compare deterministic replay at a fixed committed height.
REQUIRED HANDOFF

Deliver authority code, lifecycle fixtures and a reviewer-focused authorization map. Flag any
framework-lifecycle or licensing uncertainty before building dependent modules.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                12 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



