# Migration and recovery

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled.

## What was rehearsed

The candidate switches `current` from one release directory to another and back. That changes which directory the link names. It does not read or write chain state. The file planted under `operator-data` is still there after the switch and after uninstall.

## What was not rehearsed

Chain-state rollback was not rehearsed. [failed-upgrade.md](failed-upgrade.md#when-rollback-is-unsafe) records when an old binary is not a recovery. [backup-and-restore.md](backup-and-restore.md#compatibility) accepts one backup format and refuses another version. This goal does not add a migration for that format.

An upgrade of `svnd` still follows [release-preparation.md](release-preparation.md) and [operator-installation.md](operator-installation.md): a reviewed binary is installed beside the running one, and each operator chooses whether to install it. The candidate command does not perform that chain upgrade.

## Recovery that already exists

Goal 26 restores a logical dump onto a clean directory when the origin is absent. Retention, custody, and the recovery policy are unset. Those open items stay DR-26-01 through DR-26-12. This packaging step does not close them.
