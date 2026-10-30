GOAL 18 / 30



End-to-end network milestone
  GPT-6.1 Sol                                                                      EFFORT: HIGH

Prerequisites: Goals 05-17 verified as far as their required runtimes allow.
Blueprint reference: pp. 3-4, 13-16, 25-28 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 18. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Integrate a real upload-to-playback path with committed hosting records and demonstrable survival of host
loss.
IMPLEMENTATION REQUIREMENTS

1. Connect the API, database/outbox, media worker, broadcaster, indexer, provider agents, scheduler and
    delivery layer. Use real services and the four-validator chain. Restrict access to explicit invited
    development principals until Goal 19 completes the account system.
2. Create a scripted scenario using a small permitted video: upload, process, obtain moderation approval,
    transfer verified packages, accept provider leases, confirm committed execution and publish. Expose each
    intermediate state rather than collapsing it into a success flag.
3. Extend the scenario to origin/uploader disconnection, one provider failure, replica repair and recovery of a
    restarted service. Save the exact chain ID, heights, public transaction IDs, package digest, byte checks and
    test timestamps.
4. Exercise loss of consensus quorum and verify existing permitted playback continues only under current
    real-time terms, while new commitments remain pending. Resume without resetting the chain or inventing
    accepted history.
5. Fix cross-service contract mismatches in small changes with updated fixtures and regression tests. Do not
    start social expansion until this milestone has real evidence; unavailable infrastructure remains a blocker,
    not a mock substitute.
ACCEPTANCE CHECKS

Run the complete scenario on a clean approved integration runner. Verify one final business effect after
injected timeout/retry and no leaked quota or reservations.
Corrupt a package, reject an unauthorized provider acceptance and withdraw a test video from controlled
services. Confirm UI/API status matches actual state.
Include a minimal independent client or protocol script that reads approved metadata and plays/retrieves
media without private backend shortcuts.
REQUIRED HANDOFF

Deliver a repeatable demonstration and requirements-to-evidence report. Explicitly distinguish a working
prototype from a reviewed public beta; list blockers before Goals 19-24.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                               24 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                  CODEX CLOUD + GITHUB



