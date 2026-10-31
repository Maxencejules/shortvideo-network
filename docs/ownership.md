# Ownership and review

Proposed policy for the reference repository. No person is assigned. The five roles are functions: product/application, chain/security, media/provider agents, deployment/on-call, and trust/safety. One person may cover more than one role later. That staffing choice is not made here.

There is no `CODEOWNERS` file. Inventing GitHub usernames would pretend a review rotation exists.

## What review means

- A change to `docs/protocol/` or `docs/protocol.md` is an incompatible protocol change unless a recorded decision says otherwise. Protocol v0 is frozen for goals 03-18. Stop and record a decision instead of editing a field, a domain string, an error number, or a state name.
- Dependency additions are reviewed against `docs/dependencies.md`. Commercial-only modules, including Cosmos Enterprise PoA and Enterprise Groups, stay out.
- CI changes are reviewed against `scripts/workflow_policy.py`: read-only permissions, pinned actions, no credential inputs, no merge, and no deployment.
- The chain/security role is the proposed reviewer for consensus, authority, and CI changes once a person holds that role. Until then, review is whoever the repository owner asks. This file does not name them.

## What merge means

Merging is a human action on the forge. Workflows in this repository do not merge, tag, or deploy. Public launch stays disabled.
