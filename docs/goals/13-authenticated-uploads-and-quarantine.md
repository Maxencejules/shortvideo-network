GOAL 13 / 30



Authenticated uploads and quarantine
  GPT-6.1 Sol                                                                       EFFORT: HIGH

Prerequisites: Goals 02, 04, 11-12; minimum account/permission contract approved.
Blueprint reference: pp. 5, 13, 16-17, 19 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 13. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Implement a real bounded upload service that cannot publish untrusted bytes or bypass account permissions.
IMPLEMENTATION REQUIREMENTS

1. Create authenticated upload-intent, transfer-completion and status endpoints in services/api. Implement
    the minimum approved session/role boundary now if absent; full enrollment/recovery belongs to Goal 19.
    Development principals must be explicit fixtures that cannot enable anonymous production access.
2. Enforce proposed beta limits from configuration: at most 100 MB input and 60 seconds supported duration,
    plus per-account allowances and a separate processed-output budget. Record reservations atomically;
    reject concurrent quota bypass. Duration is verified by a bounded worker, not trusted client metadata.
3. Store uploads under generated names in private quarantine, using the approved local/object-storage
    adapter and narrowly scoped transfer authorization. Validate size, ownership, actual structure and
    completion digest. Do not trust filenames, extensions or claimed MIME types.
4. Use an outbox/job record for processing requests, stable idempotency keys and resumable status
    transitions. Clean abandoned/stale uploads and release reservations exactly once. Keep processing and
    publication separate.
5. Protect completion, status, cancellation and metadata access with object-level authorization. Record creator
    intent without private data on-chain. Add rate limits, structured safe errors and denied cross-account tests;
    never hard-code a user ID as authorization.
ACCEPTANCE CHECKS

Upload a permitted fixture and inspect its quarantine/job state. Test malformed input, oversized bodies, false
completion, duplicate requests, concurrent quotas, interrupted transfer and expired authorization.
Attempt reads/deletes/completion for another account and verify denial. Restart the API/queue and
demonstrate no double job or leaked quota.
Confirm quarantine files have no public playback path and an upload alone cannot become PUBLISHED.
REQUIRED HANDOFF

Deliver real upload APIs, safe fixture credentials, lifecycle tests and documented integration points. Keep
public sign-up and public beta launch disabled.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                19 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                 CODEX CLOUD + GITHUB



