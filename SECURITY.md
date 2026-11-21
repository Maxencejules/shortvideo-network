# Security

Public launch is disabled. This file is not an audit and not a production-security claim.

## Reporting

Report a vulnerability privately to the repository owner. Do not open a public issue for a report that is still unfixed. This file does not name a person, an inbox, or a vendor. The repository owner decides whether a private forge report is available.

## Dependencies

Pins, checksums, and discovered licences are in `docs/dependencies.md`. The inventory is `docs/sbom.cdx.json`. Updates stay on maintained releases already selected in that document. The Cosmos SDK licence does not audit this repository's modules.

## Boundaries

Video bytes and personal data stay off the chain. The candidate bundle contains no shared private key and no default credential. Test signing keys are generated when the candidate runs and are deleted before that command returns. Release signing is an owner action, described in `docs/operations/signing-procedure.md`.

Untrusted media processing stays off the validator signing host. See `docs/operations/deployment-profiles.md`.
