GOAL 21 / 30



Creator publishing interface
  GPT-6.1 Sol                                                                         EFFORT: HIGH

Prerequisites: Goals 13-20; real upload, account and media-status APIs.
Blueprint reference: pp. 4-5, 13-17, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 21. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Build creator workflows for publishing and managing real videos, with accurate processing, hosting and
removal status.
IMPLEMENTATION REQUIREMENTS

1. Implement upload selection, bounded basic trimming, title/description, captions, thumbnail selection and
    accessible progress views. Use the existing server pipeline; trim requests must still satisfy server-side
    input, duration and processing budgets.
2. Display Uploading, Processing, Review pending, Replicating and Published separately. Show failures,
    stalled-chain pending state, availability degradation and hosting-renewal notices. Do not show Published
    from a local optimistic state before the approved conditions hold.
3. Provide cancel/retry, draft management, publication intent and removal requests. Require object ownership
    and confirmation for destructive actions. Do not allow changing media bytes under the same immutable
    content version.
4. Integrate account recovery/export links and transparent hosting terms/allowances. Keep creator analytics
    to implemented privacy-preserving aggregates; never invent views, revenue or storage guarantees.
5. Handle network interruption, expired upload authorization, excessive media size, rejected content and
    exhausted quota. Use typed API errors and keep repeated actions idempotent.
ACCEPTANCE CHECKS

Publish a permitted fixture end to end and inspect every displayed lifecycle state against backend and
committed-chain evidence. Test creator/origin disconnection and later playback.
Try another account's edit/remove actions, illegal trim ranges, duplicate submissions, cancellation during
processing and retry after a timeout. Verify correct reservations and no leaked files.
Withdraw a video and ensure the UI does not claim universal deletion; present pending participating-host
actions accurately.
REQUIRED HANDOFF

Deliver a working creator flow with test evidence and any dependencies on Goal 23 moderation completion.
Preserve the invited-development boundary.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                  27 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



