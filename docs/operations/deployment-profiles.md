# Reference deployment profiles

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. Every profile in [../../deployments/profiles/profiles.json](../../deployments/profiles/profiles.json) is NOT-RELEASED. The licence is UNCONFIRMED.

No profile requires a commercial cloud account. No profile publishes a port. Binds in the reference are `127.0.0.1`. Resource figures are `not-measured`. Goal 28 owns measurement. A dev-machine result is one failure domain and is not independent operation.

## Separation

The workers profile has no signer directory and does not run `svnd`. The validators profile does not run FFmpeg or the media worker. On the dev machine the validator home is `var/validator` and the media workspace is `var/media`. `worker_sees_signer` is false on every profile. Untrusted media processing stays off the validator signing host.

## What each profile keeps

Volumes, TLS, ports, secrets, health, backups, and upgrades are fields on each profile. Secrets are `generated-on-install`, `operator-supplied`, or `none`. A profile does not carry a secret value. TLS material is not in the bundle.

Health that exists today: `svnd version` for a validator binary, a usage exit for the other packaged commands, and `GET /healthz` plus `GET /readyz` on `svn-observe` when that process is run. This packaging step does not start those listeners.

Upgrades replace a binary beside the running one. Chain-state rollback is not rehearsed. See [migration-and-recovery.md](migration-and-recovery.md).

## Replacement paths

DNS, identity, media storage, search, and transaction access each have a replacement in the profile document. None of them is a mandatory vendor. The software does not require a single operator. The dev-machine profile is a convenience for one checkout, not a rule that one operator must run every role.

## Images

`deployments/chain/Dockerfile` is the local validator image. `deployments/images/worker.Dockerfile` is the local worker image. Neither is published. Both pin the base by digest and end as a non-root user. The worker image does not install FFmpeg and does not copy a signing key.
