# Architecture notes

The blueprint and `docs/protocol.md` are the current architecture record. This directory does not add a second design. Later goals can put operator-facing architecture notes here.

Goal 06's reviewer map is [authority-authorization.md](authority-authorization.md).

Goal 07's reviewer map for the provider and content registries is [registries.md](registries.md).

Goal 08's reviewer map for lease lifecycle and capacity accounting (x/leases wiring, cross-module flows, app-level and devnet tests, decision requests DR-08-01 to DR-08-13) is [leases.md](leases.md).

Goal 09's reviewer map for beta attestations and audit evidence (x/evidence wiring, the off-chain audit producer and reviewer, labels and limitations, the network policy, retention, app-level and devnet tests, the cryptographic-verification decision gate) is [evidence.md](evidence.md).

Goal 10's governance and upgrade specification (x/policy, typed governed changes and their activation, council seat replacement, the upgrade plan, the load gate and the per-block app-version rule, the rehearsal) is [governance-and-upgrades.md](governance-and-upgrades.md). It is PROPOSED: its approval is a recorded decision request, not an owner approval.
