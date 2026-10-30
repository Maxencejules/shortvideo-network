GOAL 19 / 30



Accounts, recovery and data portability
  GPT-6.1 Sol                                                                         EFFORT: HIGH

Prerequisites: Goal 18; approved custody, privacy and recovery decisions.
Blueprint reference: pp. 4-5, 17, 19-21 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 19. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Replace development-only access with the approved account, session, recovery and signed-record system.
IMPLEMENTATION REQUIREMENTS

1. Implement invited enrollment, stable account IDs, session management, role/object authorization,
    revocation and abuse controls using a maintained authentication implementation. Add passkeys where the
    selected implementation supports them; do not invent authentication cryptography.
2. Keep application credentials separate from operator chain signing. Protect cookies/tokens, CSRF
    boundaries and recovery flows. Require strong confirmation for sensitive changes, rotate sessions and
    rate-limit enrollment/login/recovery. Remove any production-reachable development bypass.
3. Implement delegated host-signing custody only as approved and disclose that trust. Version public record
    envelopes, key references, sequence values and canonicalization; test key history and replay. Saved videos,
    preferences, recovery data and raw telemetry remain private/off-chain.
4. Provide account exports, verified import/migration between compatible test hosts, deletion initiation and a
    defined encrypted backup path. Preserve identity only to the extent the approved custody scheme
    supports it; do not claim full federation or guaranteed outside follower migration.
5. Apply retention and least-privilege access to account data. Exclude other people's private records and
    confidential moderation evidence from exports. Keep any unresolved legal/privacy launch decision visibly
    blocked.
ACCEPTANCE CHECKS

Test cross-account access, expired/reused recovery links or challenges, stolen/revoked sessions, duplicate
enrollment, CSRF and privilege escalation within the test system.
Rotate keys and validate old/new signed-record handling with cross-language fixtures. Migrate a test account
between two compatible hosts and verify permitted data integrity.
Delete a test account, deny future actions and verify controlled removal/retention behaviour without claiming
all historical public records disappear.
REQUIRED HANDOFF

Deliver real user authentication and recovery evidence. Audit every earlier endpoint against the new identities
and rerun the end-to-end scenario.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                  25 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                   CODEX CLOUD + GITHUB



