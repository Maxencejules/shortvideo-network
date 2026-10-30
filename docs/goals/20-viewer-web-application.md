GOAL 20 / 30



Viewer web application
  GPT-6.1 Sol                                                                          EFFORT: HIGH

Prerequisites: Goals 15, 18-19; current typed playback/account APIs.
Blueprint reference: pp. 4-5, 16, 18, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 20. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Build the responsive viewer experience against real APIs, with accessible playback and honest loading/error
states.
IMPLEMENTATION REQUIREMENTS

1. Implement apps/web in React/TypeScript with Home, Discover, Activity and Profile navigation as
    appropriate to available endpoints. Deliver the vertical video player, public profiles, captions, mute,
    keyboard navigation, reduced motion and responsive layouts.
2. Connect playback to approved content metadata, candidate hosts and account sessions. Implement
    bounded prefetch, cancellation, network fallback, expired-session handling and clear
    unavailable/withdrawn/degraded states. No mandatory wallet or peer upload is required.
3. Use typed client adapters and consistent errors; treat titles, captions and comments as untrusted data.
    Avoid unsafe HTML rendering and leaking secrets or private preferences into URLs/telemetry.
4. Wire existing actions to real endpoints. For social/moderation features scheduled in Goals 22-23, use
   explicit unavailable/feature-gated states, not mock success. Keep the product an invited development
   build until reporting/blocking are complete.
5. Create component and accessibility tests plus browser-level flows. When interactive browser tools are
    absent, use permitted headless tests or a separate CI/manual validation path; screenshots or visual
    approval must never be fabricated.
ACCEPTANCE CHECKS

Watch actual processed videos, change quality, scroll rapidly, lose a host and recover. Verify no runaway
downloads, repeated audio or access to withdrawn/quarantined content.
Test keyboard-only navigation, captions, reduced motion, mobile viewport sizes, expired login and API failures.
Record browser/device details for visual tests.
Confirm unavailable features are visibly gated and no test fixture is presented as a real user or live network
statistic.
REQUIRED HANDOFF

Provide the usable viewer build, real browser evidence where available and remaining feature gates. Do not
claim visual verification from a successful web build alone.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                   26 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                  CODEX CLOUD + GITHUB



