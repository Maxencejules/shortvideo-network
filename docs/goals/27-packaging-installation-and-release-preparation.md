GOAL 27 / 30



Packaging, installation and release preparation
  GPT-6.1 Sol                                                                      EFFORT: HIGH

Prerequisites: Goals 03-26; approved licence decisions and tested recovery paths.
Blueprint reference: pp. 7, 19, 21-22, 25-28 [B01]. Settings are selected in the client.
COPY PROMPT

Complete Goal 27. Read AGENTS.md, docs/progress.md and the approved specifications for this goal. Verify
the target branch, base commit and prerequisite evidence. Execute only this goal; apply the common contract.
Prepare reproducible installable artifacts and release documentation without automatically publishing or
deploying them.
IMPLEMENTATION REQUIREMENTS

1. Create reference deployment profiles for the web/API, workers, provider agents, query/index services and
    validators. Document persistent volumes, TLS, network ports, secrets, health checks, resource
    observations and upgrades. Keep untrusted media processing separate from signer material.
2. Build versioned binaries/images and an installation/configuration flow with secure defaults. Pin
    image/dependency versions, generate SBOMs, retain third-party notices and record FFmpeg codec/build
    obligations. Resolve licence blockers before labelling artifacts releasable.
3. Provide generated-on-install development fixtures only; no shared private keys or default production
    credentials. Create verification commands for artifact digests and a signing procedure. Test signing keys
    must be visibly nonproduction; actual release signing remains an owner action.
4. Prepare README, operator quickstart, protocol/API reference, CONTRIBUTING, SECURITY, governance
    notes and migration/recovery instructions. Publish known limits and trust boundaries. Do not claim
    interoperability, security audits or independent operation not demonstrated.
5. Configure a safe release-candidate workflow with minimal permissions and approval gates. Do not push
    tags, publish packages, open network ports, register domains or deploy paid services unless explicitly
    authorized.
ACCEPTANCE CHECKS

Build from a clean pinned checkout, install on a fresh approved test runner and run the documented smoke
scenario without undocumented steps. Compare build inputs and explain any non-reproducible outputs.
Inspect bundles/images for secrets, excessive privileges, missing notices and unpinned downloads. Test
uninstall/cleanup without deleting unrelated operator data.
Rehearse an upgrade and rollback only where the migration design permits it.
REQUIRED HANDOFF

Produce local release-candidate artifacts, checksums and install evidence. Mark the package NOT RELEASED
until Goals 28-30 and human launch gates pass.




Proposed implementation plan | v1.0 | 13 Oct 2026                                                               33 / 39
SHORT-VIDEO NETWORK / BUILD PROMPTS                                                                 CODEX CLOUD + GITHUB



