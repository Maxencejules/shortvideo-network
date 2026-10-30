GOAL 12 / 30



Committed-state indexer and reconciliation
  GPT-6 Astra                                                                      EFFORT: HIGH

Prerequisites: Goal 11; real chain query/event contracts and PostgreSQL.
Blueprint reference: pp. 9-10, 12-13, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 12. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Build restartable off-chain projections from committed chain state, with explicit staleness and recoverable
event gaps.
IMPLEMENTATION REQUIREMENTS

1. Implement services/indexer using committed blocks/events and supported chain queries. Version event
    decoding. Include chain identity, block height and stable transaction/message/event identity in
    deduplication keys; never index an uncommitted submission as final.
2. Write projection updates and checkpoint advancement atomically. Handle repeated delivery, restart, partial
   batches and out-of-order inputs safely. Preserve the canonical event/source reference for each important
   state change.
3. Implement backfill and periodic reconciliation of providers, content, active leases, deadlines and policy
    versions. Specify archival/pruning requirements and behaviour when history is unavailable. Never interpret
    a missing range as an empty valid history.
4. Expose indexed height, canonical height/commit age, last reconciliation and stale status. Treat the database
    as a read model, not a replacement chain. Disclose trust in the configured read gateway; comparing two
    unauthenticated responses is not light-client verification.
5. Add a clean rebuild command for disposable projections, guarded against deleting authoritative application
    data. Keep private social records separate from rebuildable public coordination data.
ACCEPTANCE CHECKS

Kill the indexer between event handling and checkpoint persistence, resume and prove exactly one projection
effect. Rebuild from the same chain and compare canonical rows after normalization.
Inject malformed event versions, forged responses, missing blocks and chain-ID mismatch; reject or block with
clear diagnostics. Verify reconciliation detects deliberately altered projections.
Stop/resume consensus and show stale read status without falsifying a lease renewal or publication approval.
REQUIRED HANDOFF

Return checkpoint/replay evidence, source-trust documentation and read APIs for hosting services. Record
archive requirements and any blocked full-history tests.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                               18 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



