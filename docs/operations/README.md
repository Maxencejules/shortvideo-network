# Operations notes

Development setup and service commands are in [docs/environment.md](../environment.md). Goal 25 and Goal 26 cover observability and backups. Public launch stays disabled.

Validator admission, removal, local endpoint disablement, and loss of quorum are in [validator-lifecycle.md](validator-lifecycle.md).

Goal 23 local moderation and removal, with placeholder contacts and no legal approval, are in [moderation-and-removal.md](moderation-and-removal.md).

Goal 24 operator onboarding, the role map, and local drain and handoff are in [operator-dashboard.md](operator-dashboard.md#role-and-permission-map). The public listener is read-only. The admin listener does not accept a consensus key.

Goal 25 correlation, probes, admission, and the cost worksheet are in [observability.md](observability.md) and [cost-worksheet.md](cost-worksheet.md). Thresholds there are placeholders. Alerts are not a spend cap. No budget owner is named.

Goal 26 backups, restore order, and signer locks are in [backup-and-restore.md](backup-and-restore.md). Retention, custody, and recovery policy are placeholders. The drill's observed times are not service guarantees.

Goal 27 packaging is NOT-RELEASED. Profiles, the install, signing, and migration limits are in [deployment-profiles.md](deployment-profiles.md), [packaging.md](packaging.md), [operator-quickstart.md](operator-quickstart.md), [signing-procedure.md](signing-procedure.md), [migration-and-recovery.md](migration-and-recovery.md), [ffmpeg-obligations.md](ffmpeg-obligations.md), and [governance-notes.md](governance-notes.md).

Reading the provider and content registries from a node (ABCI query and gRPC paths, heights, error codes) is in [../architecture/registries.md](../architecture/registries.md#reading-committed-records).

Goal 10 governance and upgrade runbooks (devnet beta only; the specification is [../architecture/governance-and-upgrades.md](../architecture/governance-and-upgrades.md), PROPOSED):

- [council-changes.md](council-changes.md): governed limit changes and scheduled activation, council seat key replacement, scheduling and cancelling an upgrade, reading `Query/Policy` and `Query/Governance`.
- [release-preparation.md](release-preparation.md): preparing and verifying a release build (no build tag; `svnd version`; the manifest).
- [source-and-digest-review.md](source-and-digest-review.md): the human review of a release's source and digests.
- [operator-installation.md](operator-installation.md): verifying the binary, reading the plan, choosing, installing beside the running binary.
- [planned-halt-and-restart.md](planned-halt-and-restart.md): the halt at the plan height, stop, copy, swap, start, and the upgrade window.
- [failed-upgrade.md](failed-upgrade.md): failure cases I1 to I8, the three allowed recoveries, when rollback is unsafe, permanent quorum loss.
- [governance-authorities.md](governance-authorities.md): local emergency controls, protocol changes, and publication authority; the steps that still need a human approval or an independent security review.

`tests/unit/test_operations_docs.py` checks these runbooks: the header line, links to the specification, every cited `svnd` command and Go test name, the `walk:<id>` tags, and that no forbidden recovery step appears outside the marked "Never" list. `scripts/devnet/walk-runbooks.py` runs the tagged blocks against a disposable single-machine devnet; run it inside a private network namespace (for example `unshare --net` with loopback up), because the devnet uses fixed loopback ports.
