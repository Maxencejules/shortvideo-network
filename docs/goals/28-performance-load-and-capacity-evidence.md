GOAL 28 / 30



Performance, load and capacity evidence
  GPT-6.1 Sol                                                                        EFFORT: HIGH

Prerequisites: Goal 27; approved integration/load runner and resource budget.
Blueprint reference: pp. 16, 22-24, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 28. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Measure the actual beta system against its proposed targets without extrapolating unsupported scale claims.
IMPLEMENTATION REQUIREMENTS

1. Create a reproducible load plan with pinned revision, hardware, OS, network settings, video corpus, cache
    state, concurrency and duration. Use permitted synthetic/owned media, disposable accounts and a
    bounded spend/resource allowance.
2. Measure the blueprint design point of 100 simultaneous viewers and 10 upload sessions. Under a controlled
    20 Mb/s downlink and 50 ms RTT, evaluate p95 playback startup at or below 3 seconds and rebuffer ratio
    at or below 1 percent. Define exactly how the network limit is applied and every metric denominator.
3. Test a 60-second supported upload reaching ready-for-review within the proposed 120-second target at
    declared worker concurrency/hardware. Separate processing, moderation, replication and total publication
    time.
4. Measure chain transaction bytes/gas, state growth, commit behaviour, index lag, repair time and
    service-resource saturation. Add overload, slow-provider and host-loss scenarios with bounded duration;
    ordinary tests must not be confused with attacks on third-party systems.
5. Fix evidenced bottlenecks without weakening integrity, quotas or acceptance thresholds. Rerun comparable
    tests after each material change. Prefer measured changes over speculative microservices or complex
    recommendation infrastructure.
ACCEPTANCE CHECKS

Save raw results and calculations with the tested code revision. Report target, observed value, sample size and
pass/fail/blocked status for every gate.
Cross-check client rendering measurements against server telemetry; a successful segment download alone is
not playback success.
If the cloud workspace cannot run shaped-network/browser/full-load tests, run them on an authorized fuller
runner or mark them blocked. Do not invent numbers or treat short smoke tests as capacity proof.
REQUIRED HANDOFF

Deliver benchmark evidence, resource sizing observations and remaining limits. No claim of TikTok-scale
capacity or an actual operating budget follows from these beta tests.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                 34 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                     CODEX CLOUD + GITHUB



