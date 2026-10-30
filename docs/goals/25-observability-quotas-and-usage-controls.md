GOAL 25 / 30



Observability, quotas and usage controls
  GPT-6.1 Sol                                                                        EFFORT: HIGH

Prerequisites: Goals 18-24; actual service metrics and account boundaries.
Blueprint reference: pp. 19, 22-24, 26 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 25. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Make resource use, failures and budget exposure measurable without collecting unnecessary personal data.
IMPLEMENTATION REQUIREMENTS

1. Instrument API, jobs, broadcaster, indexer, chain, providers and playback using consistent correlation IDs
    that do not expose private identities. Add health/readiness probes and logs with secret/personal-data
    redaction and bounded retention.
2. Measure commit age, index lag, processing age, reachable replicas, expiry/repair backlog, queue retries,
    delivered bytes, storage and playback failures. Separate user payload, replication, repair, prefetch and
    backups rather than double-counting or inventing savings.
3. Enforce admission limits for uploads, processing concurrency, output bytes, storage reservations, audit
    batches and retries. Add hard stop/queue controls when an approved budget or capacity threshold is
    reached. Alerts alone must not be described as a spend cap.
4. Create operational dashboards and alert/runbook mappings with named-role placeholders awaiting real
    assignment. Use aggregate playback telemetry and account/privacy controls; do not put watch events
    on-chain.
5. Provide a cost worksheet that separates measured units from operator-supplied rates and currency. Keep
    total network resources distinct from founder expenses and volunteer contributions. Do not forecast a
    monthly bill without real inputs.
ACCEPTANCE CHECKS

Inject slow processing, low disk, stale commits, failed repairs and repeated uploads; verify detection, safe
intake controls and recovery. Ensure health checks do not report readiness when a critical dependency is
unusable.
Check redaction with synthetic secret canaries and access controls on metrics/logs. Test quota races and
prove repeated requests cannot bypass hard limits.
Compare measured byte/storage counters against known fixtures and document any unmeasured traffic
classes.
REQUIRED HANDOFF

Deliver metrics, resource controls, runbooks and an auditable usage report. Record budget-owner and
operational-coverage decisions still needed for release.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                 31 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



