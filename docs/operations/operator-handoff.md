# Operator handoff

Development procedure for a person who might install the candidate later. This is not a production runbook. Public launch is disabled. No operator is named. The copy exercised on the agent VM is an installation rehearsal by that agent. It is not an independent installation, and it does not show a separate machine.

The release decision is in [release-readiness.md](../release-readiness.md). Completing the thirty goals is not approval. The candidate is an implemented prototype. It is not a beta the owner has approved.

## What you can use

On one development machine, with the toolchains named in [../dependencies.md](../dependencies.md):

- Build `svnd` and read `svnd version`. A binary from `scripts/packaging.py candidate` is labeled NOT-RELEASED. It is not a published release and it is not an owner signature.
- Read the operator dashboard and onboarding notes in [operator-dashboard.md](operator-dashboard.md). Storage does not force validator duty. The dashboard does not accept a consensus key.
- Read backup and restore limits in [backup-and-restore.md](backup-and-restore.md). The drill is not a live recovery.
- Read upgrade limits in [failed-upgrade.md](failed-upgrade.md), [migration-and-recovery.md](migration-and-recovery.md), and [operator-installation.md](operator-installation.md). Install a reviewed binary beside the one that is running. Do not overwrite the running binary.
- Point a second client at the published API documents under `docs/protocol/api/`. The client in `scripts/acceptance/second_client.py` calls only those paths and does not follow a redirect. `GET /feeds`, `POST /reports`, and `GET /exports` are in the published client document and are not the routes the handlers register. Do not switch to an unregistered alias to make a call succeed. That gap is DR-30-11 in [../decision-register.md](../decision-register.md).

## What you cannot treat as done

- The requirement-to-test links. Of 216 in-scope `TEST:` rows, 33 resolve to a test function. 183 are prose-only dispositions and name no test function. DR-30-12 stays open. The audit log lists each unresolved id.
- A separate operator on separate infrastructure. DR-30-01 stays open. Several processes on one machine are one failure domain.
- Owner sign-off for moderation coverage, privacy and rights, funding and quotas, operator participation, or security review. The rows in the release decision are blank. DR-30-02 through DR-30-06 stay open.
- A human security audit. DR-29-01 stays open. F-29-05 stays open (DR-30-08).
- A public launch, a public sign-up, a token, a coin, a sale, or mining.
- A project licence file. DEC-006 is not applied.
- Named on-call coverage. The five roles in [../ownership.md](../ownership.md) have no person assigned. Alerts are not a staffed incident process.
- Chain-state rollback. It was not rehearsed. Permanent loss of more than one third of voting power has no on-chain path back.
- The page 26 design point of 100 viewers and 10 uploads. DR-30-10 stays open.

## If you stop

Leave the chain id as it is. Do not present a new genesis as recovery of this chain. Uninstall removes only paths listed in the install manifest and leaves operator-data in place. Account recovery completion stays refused until the custody decision names a procedure. No retention period is set.

Nothing in this handoff was launched.
