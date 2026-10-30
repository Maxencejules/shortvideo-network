GOAL 09 / 30



Beta attestations and audit evidence
  GPT-6 Astra                                                                       EFFORT: HIGH

Prerequisites: Goal 08; evidence trust model approved in Goal 02.
Blueprint reference: pp. 9-10, 15, 19, 24 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 09. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Implement x/evidence for signed beta attestations without claiming a trustless proof-of-storage system.
IMPLEMENTATION REQUIREMENTS

1. Implement authorized auditors, schema versions, batch IDs, epochs/rounds, signer references, evidence
    digests and bounded result records. Keep full observation payloads off-chain under the approved retention
    policy, with retrieval/checksum verification for reviewers.
2. Validate signatures, allowed signer roles, correct round, unique evidence IDs, lease/content binding, digest
    formats, field bounds and maximum batch size. Reject replay, stale evidence and unknown schemas.
    Define exactly which status may change after acceptance; evidence cannot extend a lease by implication.
3. Create a minimal off-chain audit producer for permitted test endpoints: retrieve sampled segments or whole
    packages, verify digests and record timestamps, results and failure reasons. Use the network restrictions
    defined in the provider contract; do not fetch endpoints from chain handlers.
4. Display evidence mode explicitly as provider receipt or auditor attestation. Record limitations: auditors can
    lie, providers may fetch from another host, and node IDs do not prove independent replicas.
5. Defer financial rewards, automatic slashing, randomized cryptographic proofs and Filecoin integration. Do
    not introduce a home-made Merkle scheme and call it a uniquely stored replica proof.
ACCEPTANCE CHECKS

Accept a legitimate bounded audit batch and reject wrong signers, altered observations, duplicate evidence,
expired rounds and oversized batches. Test evidence availability failure separately from chain inclusion.
Simulate a dishonest but admitted auditor and document that valid signing does not make its observation
truthful. A stale or passing audit must not revive expired/withdrawn hosting.
Measure transaction bytes, execution work and state growth on the declared fixtures without extrapolating a
network throughput claim.
REQUIRED HANDOFF

Deliver module tests, audit fixtures and accurate UI/API evidence labels. Record a separate future
cryptographic-verification decision gate, not an unfinished feature disguised as complete.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                15 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                               CODEX CLOUD + GITHUB



