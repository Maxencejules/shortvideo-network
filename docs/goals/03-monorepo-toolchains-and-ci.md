GOAL 03 / 30



Monorepo, toolchains and CI
  GPT-6.1 Sol                                                                        EFFORT: MEDIUM

Prerequisites: Goals 01-02; selected contracts and dependency decisions.
Blueprint reference: pp. 7, 19, 25, 27 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 03. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Create a buildable repository foundation with reproducible dependency management and truthful
continuous-integration results.
IMPLEMENTATION REQUIREMENTS

1. Use the blueprint monorepo layout for apps/web, services/api, services/indexer, services/broadcaster,
    chain, proto, agents/provider, workers/media, packages/protocol, deployments, tests and docs. Preserve
    any useful existing files; do not generate fake finished services.
2. Verify a supported compatible Cosmos SDK / CometBFT pair and the required Go toolchain from official
    sources. Pin Python, Node, package managers, code generators and runtime dependencies with lockfiles or
    equivalent. Record versions, source URLs, licences and compatibility rationale in docs/dependencies.md;
    do not silently select commercial-only modules.
3. Create format, lint, type-check, schema-generation and unit-test entry points. Add GitHub Actions jobs with
    minimal permissions, bounded runtime and isolated fixtures. Pin action dependencies using the supported
    locking mechanism or verified full commit SHAs. Do not invent SHA values.
4. Protect CI from untrusted pull-request input and secret exposure. No privileged checkout of untrusted
    code, production access, automatic merging or deployment. Add a PR template that records requirement
    IDs, actual test evidence and outstanding risks; add a proposed ownership/review policy without inventing
    reviewers.
5. Provide README installation instructions and .env.example with placeholders only. Exclude generated
    private material, dependency caches, build outputs and uploaded media from Git. An empty test suite must
    not report project verification.
ACCEPTANCE CHECKS

From a clean checkout, install pinned dependencies, generate contracts and run the available checks. Test that
a deliberately failing fixture makes the relevant job fail, then remove that deliberate failure.
Inspect workflow permissions, dependency references, ignore rules and generated diffs. Report
network/toolchain blockers rather than pretending CI ran remotely.
REQUIRED HANDOFF

Return the working skeleton, exact commands and CI configuration. Record what is scaffolding versus
implemented; hand off environment needs to Goal 04.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                                   09 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                  CODEX CLOUD + GITHUB



