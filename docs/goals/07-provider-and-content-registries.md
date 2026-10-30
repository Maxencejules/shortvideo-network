GOAL 07 / 30



Provider and content registries
  GPT-6 Astra                                                                       EFFORT: HIGH

Prerequisites: Goals 02, 05-06; signed manifest contracts available.
Blueprint reference: pp. 9-11, 14, 27 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 07. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Implement x/providers and x/content with minimal public state and precise ownership, versioning and
admission rules.
IMPLEMENTATION REQUIREMENTS

1. Create Protobuf types, keepers, message/query handlers, genesis validation, events and migrations for both
    modules. Providers expose stable IDs, signing-key references, capabilities, status, admitted operator-group
    metadata and a digest of a signed off-chain endpoint manifest.
2. Content records contain an opaque ID, immutable package/manifest version, digest, byte count, approved
    submitting host and coordination status. Keep video bytes, user secrets, personal account fields and
    moderation narratives off-chain. Explain that opaque hashes can still be linkable.
3. Enforce eligible submitters, object-level ownership, validated digest lengths/algorithms, bounded strings
    and byte totals, unique IDs/versions and typed errors. Separate provider key updates from consensus-key
    changes. Define what disablement means for existing obligations without deleting history.
4. Implement content withdrawal so new sponsored commitments cannot be created or silently revived. Allow
    a new encoding only as a new package version. Do not treat a registry entry as copyright proof, a hosting
    receipt, or publication approval.
5. Provide paginated queries and stable events for the later indexer. Endpoint URLs stay in off-chain manifests
    where possible; validate the metadata now and leave actual fetches to services, never consensus
    execution.
ACCEPTANCE CHECKS

Commit valid registration/update/withdrawal transactions on the devnet. Reject cross-owner edits,
unauthorized provider updates, duplicate versions, invalid digests, oversized metadata and replayed requests.
Round-trip genesis and reconstruct query results from committed state. Compare replay hashes and test
pagination boundaries and event schemas.
Assert that no private user details or media payloads appear in stored state or event fixtures.
REQUIRED HANDOFF

Provide a minimal real-transaction demo and negative-test evidence. Hand off immutable package identity
and provider-admission APIs to the lease module.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                13 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                               CODEX CLOUD + GITHUB



