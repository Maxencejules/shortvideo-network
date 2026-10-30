GOAL 08 / 30



Lease lifecycle and capacity accounting
  GPT-6 Astra                                                                      EFFORT: HIGH

Prerequisites: Goal 07 and approved lease state/terms specification.
Blueprint reference: pp. 9-10, 13-15, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 08. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Implement x/leases so each storage obligation has authorized parties, immutable terms, bounded accounting
and correct expiry semantics.
IMPLEMENTATION REQUIREMENTS

1. Implement offer, acceptance, renewal and end transitions using stable offer/lease/business IDs. Bind
    acceptance to provider signature, exact content version, manifest digest, bytes, terms version and
    real-time deadline. A submitting service cannot sign acceptance on a provider's behalf.
2. Reserve and release declared capacity atomically according to the approved lifecycle. Prevent duplicate
    provider/content-version copies from counting as separate replicas. Enforce per-provider and global
    bounds, nonnegative counters, valid terms and idempotent business operations across distinct transaction
    hashes.
3. Define ACTIVE as a committed obligation, not automatic evidence of retrieval, physical independence or
    publication. Keep observed AT_RISK availability off-chain unless an explicitly approved attestation
    transition applies. Renewal cannot silently change the committed bytes.
4. Implement deadline indexes and bounded expiry processing. Use committed time/context for consensus
    changes; document that host obligations have real-time deadlines even when the chain stops. Expired
    offers/evidence cannot renew current status.
5. Integrate provider disablement, content withdrawal, cancellation, repair handoff and restart behaviour.
    Preserve history needed for audit and reconciliation; never release the same reservation twice.
ACCEPTANCE CHECKS

Property-test transition sequences with an independent accounting oracle. Cover over-capacity acceptance,
stale terms, wrong provider keys, duplicate IDs, overflow, repeated ending, early/late renewal and withdrawn
content.
Run real transactions with timeout/retry scenarios and demonstrate one final obligation with correct reserved
bytes. Confirm failed operations leave consistent state.
Replay the same committed inputs and compare hashes. Test a large expiring catalogue to show bounded
per-block work rather than an unbounded sweep.
REQUIRED HANDOFF

Document invariants, storage indexes, event payloads and exhaustive negative cases. Provide the evidence
required before scheduler and broadcaster integration.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                               14 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



