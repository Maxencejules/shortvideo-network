# Development environment

This is a development profile for one cloud workspace. It is not a production host, a devnet, or a release. Public launch stays disabled. There is no token, coin, sale, or mining.

Two profiles exist:

| Profile | Where it runs | What it starts |
| --- | --- | --- |
| Cloud / local processes | This Cursor VM, and any Ubuntu 24.04 host with the pinned packages | PostgreSQL, RabbitMQ, and FFmpeg as processes on `127.0.0.1` |
| Compose | A Linux host whose `docker info` succeeds | The same services from pinned upstream images |

`scripts/env/compose.sh` exits 2 when `docker` is missing or the daemon does not answer. That status is BLOCKED. The script does not start SQLite or another database in its place.

Observed limits of the cloud VM are in [environment-capabilities.md](environment-capabilities.md).

## Install

From a clean checkout, on Ubuntu 24.04 with passwordless `sudo`:

```bash
bash scripts/env/install.sh
```

The same command is the Cursor `install` field in `.cursor/environment.json`. It is idempotent. It installs pinned packages, the RabbitMQ 4.4.0 generic tarball, the Go and protoc toolchains, the Python virtualenv, and `npm ci`. It does not start daemons and it does not create or delete a workspace.

Pins and checksums are in [dependencies.md](dependencies.md) and `deployments/pins/checksums.txt`. Celery 5.6.3 is pinned there and is not installed. Goal 14 imports it. The queue proof uses `amqp-tools` against RabbitMQ.

## Start, stop, and smoke

```bash
bash scripts/env/start.sh
bash scripts/env/smoke.sh
bash scripts/env/stop.sh
```

`make env-start`, `make env-smoke`, and `make env-stop` call those scripts. The default workspace name is `dev`. Override it with `SVN_WORKSPACE`.

`start.sh` binds PostgreSQL and RabbitMQ to `127.0.0.1` only. The first start creates `.local/workspaces/dev/`, a mode `0600` credentials file, `.svn-workspace`, and `storage/`. `svn_db` is the database superuser from `initdb`. Later goals need a separate application role. A later start sees `PG_VERSION`, the queue data directory, and `storage/marker`, and does not run `initdb` again or rewrite the storage marker. If database or queue state exists and the credentials file is gone, start exits non-zero and does not delete the data.

Passwords are written to `pgpass` and `amqp.auth` (mode `0600`). Client commands use `PGPASSFILE` and `--authfile`. `start.sh` does not pass the queue password to `rabbitmqctl`. The broker reads it from `rabbitmq.conf` on first boot. `ps` can show those file paths. It does not show the password in those commands.

`make smoke` is still the unimplemented product path and exits non-zero. `make env-smoke` is the development-profile check: one SQL query, one AMQP publish and consume, and one small FFmpeg operation. The tone file stays under `.local/` and is not committed.

## Reset

Reset deletes one workspace directory and nothing else.

```bash
bash scripts/env/reset.sh --workspace dev --confirm dev
```

`make env-reset WORKSPACE=dev CONFIRM=dev` is the same command. A missing `--confirm`, or a confirm value that is not the workspace name, exits non-zero and deletes nothing. The name has to match `^[a-z][a-z0-9-]{0,31}$`. The directory must contain `.svn-workspace` with the text `svn-dev-workspace`, which `start.sh` writes. A directory without that marker is not deleted, except a keys-only workspace: `devnet/validator.key`, `devnet/validator.pub`, and nothing else. That keys-only directory is removed after the same confirmation, because `devnet-keys.sh` does not write the service marker. Package installs, other workspaces, and the git checkout stay. Reset removes that workspace's `storage/` directory with the rest of the workspace.

## Devnet identities

```bash
bash scripts/env/devnet-keys.sh --workspace dev
```

The private key is written under `.local/workspaces/<name>/devnet/`, mode `0600`. A second run for the same workspace exits non-zero. `--from` and `--clone` exit non-zero. Keys are not copied from another workspace and are not written into git. Goal 05 is what starts validators. This command does not.

## Diagnosis

```bash
bash scripts/env/diagnose.sh
```

The report exits 0 when it was printed, including when a tool is missing. Missing tools are marked `MISSING`. That is not a claim that the missing tool passed a test.

## Compose profile

`deployments/compose/docker-compose.yml` pins the linux/amd64 digests recorded in `docs/dependencies.md`. Published ports are `127.0.0.1` only. Passwords are `${POSTGRES_PASSWORD}` and `${RABBITMQ_DEFAULT_PASS}` and are not stored in the file.

```bash
bash scripts/env/compose.sh
```

On a host with a working Docker daemon, the script loads the workspace credentials and runs `docker compose up`. Removing the named volumes is a separate destructive act. This profile's reset command does not run `docker compose down -v`.

FFmpeg stays the pinned host package. The compose file does not add an FFmpeg image.

## Credentials

`.env.example` is still placeholders. Real development passwords are generated per workspace and are gitignored. Database user `svn_db` and queue user `svn_queue` are different identities. The devnet key is a third identity and is not derived from either password. Do not copy a credentials file into a second workspace.

## Cursor cloud setup

`.cursor/environment.json` sets:

- `install` to `bash scripts/env/install.sh`
- `start` to `bash scripts/env/start.sh`
- `user` to `ubuntu`

This run's environment is the personal environment `6d1d563c-d493-11f1-89ee-7aa6f0729b11`. `environmentJsonPath` is null, so the saved dashboard configuration does not read the committed file. Recording that file, snapshotting, and starting a new environment build are Max's actions. This goal does not publish or republish an environment and does not provision a machine.

No snapshot and no prepared template may include `/workspace/.local`. That directory holds generated passwords, database files, queue data, and devnet private keys.

`install` does not run on an agent that already booted from a snapshot. `start` runs on a later boot and must stay free of package installs. After a dependency pin changes, Max republishes the environment from the updated file. The next goal still checks the checkout:

```bash
git fetch origin main
git rev-parse --abbrev-ref HEAD
git rev-parse HEAD
git merge-base --is-ancestor ab5680b4d95da9104bd53b6c294d6eee485b1480 HEAD
```

`ab5680b4d95da9104bd53b6c294d6eee485b1480` is the Goal 03 squash on `main` that this goal was cut from. The merge-base command exits 0 when that commit is an ancestor of `HEAD`.

## Commands that stay failed

`make devnet-up`, `make devnet-down`, `make smoke`, `make fault`, and `make release-check` exit non-zero on purpose. Goal 05, Goal 18, Goal 29, and Goal 27 own them. There is no chain binary in this goal.

## Runners this profile does not provide

Full-stack upload-to-playback, a browser test of a product UI, network-fault injection, and a load run need later goals and, where the tool is missing, another runner. See the evidence file for the status of each check. A missing facility stays BLOCKED.
