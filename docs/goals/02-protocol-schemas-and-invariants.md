GOAL 02 / 30



Protocol schemas and invariants
  GPT-6 Astra                                                                      EFFORT: EXTRA HIGH

Prerequisites: Goal 01; security-critical design decisions approved or explicitly blocked.
Blueprint reference: pp. 8-17, 26-27 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 02. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Define exact versioned contracts shared by the blockchain, services, provider agent and web client.
IMPLEMENTATION REQUIREMENTS

1. Write docs/protocol.md and machine-readable draft schemas: versioned Protobuf state, transaction, query
   and genesis types; API contracts; media manifests; provider capability manifests; and beta evidence
   envelopes. Specify IDs, field types, maximum sizes, integer ranges, units, enum handling, digest algorithms,
   signature domains and error codes.
2. Define provider, content, lease, authority, policy and evidence state machines. Separate a committed
    hosting obligation from observed reachability and from community publication approval. State who signs
    each transition, who may relay it, how replay/idempotency works, and when declared capacity is reserved
    or released.
3. Specify bounded expiry indexes and deterministic execution. Define committed chain time versus real-time
    host deadlines, pending requests during stalls, and treatment of expired offers or stale evidence. No
    handler may fetch external data or decide consensus state from a local clock.
4. Choose vetted serialization/canonicalization for signed off-chain records and publish valid/invalid
    cross-language vectors. Reuse the SDK transaction envelope. Define version negotiation, key rotation,
    removal tombstones, privacy classifications and data retained for reconciliation.
5. Create executable schema/fixture checks where tools permit. Keep protocol invariants distinct from
    implementation details. Explicitly label signed receipts and audits as attestations, not proof of independent
    storage or future delivery.
ACCEPTANCE CHECKS

Validate boundary values, malformed/unknown fields, duplicate IDs, wrong domains, expired terms and
unauthorized signers against the proposed contracts. Confirm Go/Python/TypeScript can represent the same
values without precision loss.
Review each transition for underflow/overflow, stale versions, retries, withdrawal and conflicting governance
updates. The specification must support the exact failure tests in blueprint page 26.
REQUIRED HANDOFF

Deliver the contracts, fixture suite and unresolved decisions. Freeze approved protocol v0 interfaces for Goals
03-18; require a recorded decision for incompatible changes.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                     08 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                   CODEX CLOUD + GITHUB



