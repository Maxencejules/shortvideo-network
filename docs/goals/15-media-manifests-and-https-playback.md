GOAL 15 / 30



Media manifests and HTTPS playback
  GPT-6.1 Sol                                                                        EFFORT: HIGH

Prerequisites: Goal 14; package contract, origin storage and authorized content state.
Blueprint reference: pp. 6, 14, 16, 18, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 15. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Implement actual video delivery with validated manifests, host failover and accurate playback/integrity claims.
IMPLEMENTATION REQUIREMENTS

1. Create a versioned media-serving adapter and HLS master/rendition playlists over HTTPS for nonlocal
    deployments. Keep private/quarantined content inaccessible. Use controlled relative paths, safe content
    types, range handling where needed and narrowly configured cross-origin policy.
2. Return candidate hosts, immutable package identity and current publication/removal state from a typed
    API. Begin with the origin/test host adapter; Goal 16 supplies independent providers. Validate manifests
    and received replicas against hashes before advertising them.
3. Implement a test player or playback harness using the selected browser/HLS support. Add bounded retries
    and alternate-host selection. Do not claim every native HLS segment was client-verified unless the actual
    fetch/decode path supports and tests it.
4. Bound prefetch to a small approved window and cancel obsolete requests. Track startup, rebuffer time,
    playback errors and delivered bytes with privacy-preserving aggregates; distinguish an HTTP response
    from successful rendering.
5. Define controlled-cache invalidation and removal behaviour. A chain outage may allow approved playback
    within existing real-time terms with stale status, but cannot justify a new uncommitted hosting promise.
ACCEPTANCE CHECKS

Play real processed media and validate playlist/segment references, orientation, captions and fallback quality.
Corrupt a manifest/segment and verify detection at the claimed verification layer.
Test host failure, 404s, slow responses, abandoned scroll requests and revoked content. Ensure no endless
retry or unrestricted download of queued videos.
Run headless/browser checks only where supported; otherwise provide CI/manual checks and mark rendering
unverified rather than inferring it from curl.
REQUIRED HANDOFF

Deliver delivery code, a real playback demonstration and an integrity-coverage statement. Leave optional P2P,
TURN and public IPFS disabled.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                 21 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



