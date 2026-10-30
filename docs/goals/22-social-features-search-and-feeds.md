GOAL 22 / 30



Social features, search and feeds
  GPT-6.1 Sol                                                                       EFFORT: HIGH

Prerequisites: Goals 19-21; signed-record contract and approved privacy rules.
Blueprint reference: pp. 4, 17-18, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 22. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Implement the bounded social layer and understandable discovery without putting ordinary interactions
on-chain.
IMPLEMENTATION REQUIREMENTS

1. Add follow/unfollow, likes, comments, private saves, in-app activity and Not interested actions with
    authorization, pagination, rate limits and retry-safe writes. Use stable account/content IDs and approved
    signed public records where specified.
2. Implement searchable permitted profiles/topics/videos and a chronological Following feed. Add a separate
    simple recommendation feed using candidates, explicit scoring factors and final filtering; no new ML
    training service is required.
3. Exclude unavailable, withdrawn, blocked and locally disallowed content. Limit repeated creators and
    preserve user feed preferences. Validate comment inputs, safe rendering and deletion propagation; keep
    exact abuse-control internals private where necessary.
4. Expose versioned discovery interfaces and rebuildable indexes so another compatible service can supply
    results. Do not claim ActivityPub/AT Protocol interoperability without an implemented adapter and
    conformance tests.
5. Wire the viewer/creator UI to the real endpoints and remove completed feature gates. Keep raw viewing
   histories, saves and private preferences out of public records, chain events and public analytics.
ACCEPTANCE CHECKS

Test follow graph permissions, duplicate likes, pagination under concurrent updates, comment moderation
boundaries, private saves and notification access.
Rebuild search/feed indexes from permitted records and compare content coverage. Verify chronological
order and filtering after withdrawal/blocking, including cached pages.
Use a small second client to request a feed and interpret stable content IDs. Report relevance tests as fixture
results, not proof of superior recommendations.
REQUIRED HANDOFF

Deliver functional social interactions, discovery contracts and filtered-feed tests. Hand off
moderation-sensitive endpoints and cache paths to Goal 23.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                28 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



