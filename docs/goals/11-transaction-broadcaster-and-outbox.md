GOAL 11 / 30



Transaction broadcaster and outbox
  GPT-6 Astra                                                                       EFFORT: HIGH

Prerequisites: Goals 04, 07-10; approved service/provider signing boundaries.
Blueprint reference: pp. 8, 10, 13, 26-27 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 11. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Build a retry-safe bridge from application intent to committed chain execution without confusing submission
with success.
IMPLEMENTATION REQUIREMENTS

1. Implement services/broadcaster with typed requests and a PostgreSQL transactional outbox. Persist intent
    IDs, authorized request context, signer identity, attempts, transaction hashes, expiry, results and
    reconciliation state. Couple application intent and outbox insertion in one database transaction.
2. Use approved service keys only for their permitted messages. Providers sign their own acceptances;
    support forwarding their valid signed transactions or the approved provider-side submission path. Never
    synthesize provider consent from a service credential.
3. Handle sequence allocation/concurrency per signer, bounded retries, backoff, transaction deadlines and
    uncertain submission. Reconcile known transaction/business IDs before resending; distinguish transport
    error, mempool rejection, failed execution and successful commitment.
4. Expose requested, submitted, committed, failed and uncertain/pending states to callers. Authenticate
    internal endpoints and enforce object permissions. A chain outage queues bounded expiring work but
    cannot create final leases in the database.
5. Keep sensitive keys behind the approved development signing boundary, redact logs and make
    gateway/chain ID configuration explicit. No undocumented network dependency or live credential is
    required for the devnet.
ACCEPTANCE CHECKS

Against the real devnet, inject timeouts before/after submission, worker death after commit, duplicate
requests and concurrent same-signer requests. Assert one business effect, no leaked capacity and eventual
accurate status.
Reject unauthorized messages and attempted provider impersonation. Stop quorum and verify the API
remains pending or fails honestly rather than returning committed success.
Restart PostgreSQL/broadcaster and reconcile from persisted state without manual history edits.
REQUIRED HANDOFF

Deliver exact failure traces, outbox schema, service API and signing-boundary documentation. Give the
indexer stable identities and canonical commitment evidence.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                17 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                               CODEX CLOUD + GITHUB



