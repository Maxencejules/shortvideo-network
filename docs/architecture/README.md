# Architecture notes

The blueprint and `docs/protocol.md` are the current architecture record. This directory does not add a second design. Later goals can put operator-facing architecture notes here.

Goal 06's reviewer map is [authority-authorization.md](authority-authorization.md).

Goal 07's reviewer map for the provider and content registries is [registries.md](registries.md).

Goal 08's reviewer map for lease lifecycle and capacity accounting (x/leases wiring, cross-module flows, app-level and devnet tests, decision requests DR-08-01 to DR-08-13) is [leases.md](leases.md).

Goal 09's reviewer map for beta attestations and audit evidence (x/evidence wiring, the off-chain audit producer and reviewer, labels and limitations, the network policy, retention, app-level and devnet tests, the cryptographic-verification decision gate) is [evidence.md](evidence.md).

Goal 10's governance and upgrade specification (x/policy, typed governed changes and their activation, council seat replacement, the upgrade plan, the load gate and the per-block app-version rule, the rehearsal) is [governance-and-upgrades.md](governance-and-upgrades.md). It is PROPOSED: its approval is a recorded decision request, not an owner approval.

Goal 12's note for the committed-state indexer (the read floor, the trust disclosure, the read API, reconciliation, rebuild, and the archive rule) is [indexer.md](indexer.md). It is PROPOSED where it names a timing number. It is not a light client.

Goal 13's note for authenticated uploads and quarantine (the routes, the reservation, the held creator intent, and the handoff to later goals) is [uploads.md](uploads.md). It is PROPOSED. DR-13-01 through DR-13-04 are open.

Goal 11's design for the transaction broadcaster and its PostgreSQL outbox (the signing boundary and the same-uid limit, the service API, the outbox schema, states and transitions, commitment evidence for the indexer, chain outage behaviour, the owner questions Q1 to Q4 and the decision requests DR-11-05 to DR-11-40) is [broadcaster.md](broadcaster.md). On 2026-11-08 at 21:39 America/Toronto the owner approved that file only, at sha256 `9779fdb1e7fe5a7577565c64a8066b6bff7e7bace1174b085df81e7b17ae905b` (DR-11-01). The file still reads PROPOSED and is not edited. The approval does not approve any PROPOSED number. The owner answered Q1 to Q4 on 2026-11-08, each with option (a): Q1 "Both keys, separate signer (Recommended)", Q2 "Relay + verbatim forward (Recommended)", Q3 "Operator-registered intent keys (Recommended)", Q4 "Minimal + clear unsent (Recommended)". Those answers do not approve a pull request or a merge.
