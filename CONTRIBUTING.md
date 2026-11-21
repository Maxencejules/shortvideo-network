# Contributing

Public launch is disabled. This repository is a permissioned community beta under development. It is not a released product.

## Changes

- A change to `docs/protocol/` or `docs/protocol.md` needs a recorded decision. Do not edit a field, a domain string, an error number, or a state name in place.
- A protocol change needs tests. The contract checkers are `docs/evidence/goal-01/check_requirements.py` and `docs/evidence/goal-02/check_protocol.py`.
- A state change needs migration notes. Chain upgrades follow `docs/operations/release-preparation.md`. Backup format changes are refused until a decision says otherwise.
- A performance claim needs a reproducible measurement. Goal 28 owns that measurement. A number without the revision, hardware, and method is not evidence.
- Dependencies stay pinned. Read `docs/dependencies.md` before adding one. Cosmos Enterprise PoA and Enterprise Groups stay out.
- Do not commit secrets, private keys, media uploads, or database dumps.

## Review

Review rules are `docs/ownership.md`. CI permissions are `scripts/workflow_policy.py`: read-only contents, pinned actions, no credentials, no merge, and no deployment. Merging is a human action. Workflows in this repository do not merge, tag, or publish.

## Conduct and reports

The code of conduct is `CODE_OF_CONDUCT.md`. Security reports follow `SECURITY.md`.

## Decision log

Open decisions are `docs/decision-register.md`. The five roles there and in `docs/ownership.md` are functions. No person is named in this guide.

## Packaging

`make release-check` is the packaging gate. `make release-candidate` builds a local candidate and labels it NOT-RELEASED. Do not tag or publish from a goal branch.
