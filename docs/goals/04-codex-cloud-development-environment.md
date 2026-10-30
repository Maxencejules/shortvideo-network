GOAL 04 / 30



Codex Cloud development environment
  GPT-6.1 Sol                                                                       EFFORT: MEDIUM

Prerequisites: Goal 03; access to the intended GitHub repository and cloud environment.
Blueprint reference: pp. 7, 11-13, 22, 27 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 04. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Prepare a tested cloud development setup and a separate full-integration profile, without assuming the
workspace is a production host.
IMPLEMENTATION REQUIREMENTS

1. Inspect actual CPU, memory, free disk, OS, runtimes, process/port support, container runtime access and
    network restrictions. Test required package downloads and local service connections. Write
    docs/environment-capabilities.md with observed results, not a vendor-feature assumption.
2. Create repeatable install and startup commands for the pinned Go/Python/Node tools, PostgreSQL,
    RabbitMQ, FFmpeg and development storage. Integrate them into the available Codex Cloud setup fields.
    Make startup idempotent, check readiness, report failures, and avoid regenerating existing application or
    chain state.
3. Provide a minimal cloud profile and a full Docker Compose or equivalent Linux test profile. If containers are
    unavailable, start permitted local service processes or mark dependent tests blocked; do not emulate a real
    database with an incompatible substitute and call it verified.
4. Use loopback/private bindings, disposable accounts, separate service identities and fresh test-only
    credentials. Generate future devnet keys per test workspace, outside Git and the shared prepared
    template. Never clone an active validator identity into concurrent tasks.
5. Create project commands for environment diagnosis, service start/stop, clean test reset and smoke tests.
    Destructive resets must target a named disposable workspace and require explicit confirmation. Document
    how to republish dependency changes and verify the checked-out branch/base commit before the next
    goal.
ACCEPTANCE CHECKS

Start from a clean permitted runtime and demonstrate database queries, a real queue round trip and a small
FFmpeg operation. Restart without losing expected state; prove absent credentials or unavailable ports fail
clearly.
Run the cloud checks and report which full-stack, browser, network-fault or load tests require another runner.
If those facilities are missing, preserve reproducible commands and a BLOCKED status rather than weakening
acceptance.
REQUIRED HANDOFF

Deliver setup/start instructions, environment evidence, resource observations and the full-runner profile. Do
not provision paid machines or change external permissions without approval.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                  10 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                CODEX CLOUD + GITHUB



