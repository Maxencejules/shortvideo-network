# Goal 04 evidence

Status: BLOCKED

Public launch: DISABLED. No tradable token, coin, sale, or mining. This is not an audit, not a production host, and not a chain binary.

The development profile's database query, AMQP round trip, FFmpeg operation, development storage, restart, and failure proofs ran. Compose was not executed. Full-stack, browser-product, network-fault, and load checks did not run. On 2026-10-31 at 16:21 Toronto Max ruled: "Don't mind the ai model requirement". The model that ran is recorded below. That mismatch is not a blocker.

## Model and effort

- Playbook assignment for goal 04: GPT-6.1 Sol, effort MEDIUM.
- Model that ran: `grok-4.7-high-fast`. Source: cursor-cloud `run-info` field `originalModelName` for `bcId=bc-2b258f31-5118-5957-81dd-822132a6f5a7`. That payload has no separate reasoning-effort field. `high-fast` is the suffix of the reported model name.
- GPT-6.1 Sol was not used. The playbook's Medium setting was not selected.
- On 2026-10-31 at 16:21 Toronto Max ruled: "Don't mind the ai model requirement". That ruling was given in the builder chat and relayed by shortvideo-builder. The mismatch is recorded and is not a blocker.

## Base and tested revision

Base: `ab5680b4d95da9104bd53b6c294d6eee485b1480`, tree `c00718f55f234115084c1a4df2675cfe1d3704c8`, subject `goal 03: monorepo, toolchains and CI (#6)` on `origin/main`.

Branch: `goal-04`, cut from that commit.

Round 1 service commands ran against tree `133e25e067f19ddd78440cc15515e6d0cde1c572`. Round 2 commands ran against the scripts in tree `ffb9654d2648bd693dc468dc905dcf9404e69212` (commit `93703d3c15031309cd6974d9b97510f3b245702e`). The round 2 blocks later in this file are shortened excerpts, not raw transcripts. Round 3 re-ran the same sequence and stored the raw logs. The tip commit's message carries `Tested-tree` set to that commit's tree. This file does not embed the tip hash, because writing it here would change the tree. The checkers do not read this file.

## Authorship

`git config user.name` is `Maxence Jules`. `git config user.email` is `Maxencejules@users.noreply.github.com`.

```text
COMMAND git log --format='%an <%ae> | %cn <%ce> | %(trailers)'
Maxence Jules <Maxencejules@users.noreply.github.com> | Maxence Jules <Maxencejules@users.noreply.github.com> | Tested-tree: 9fc950ac4f087cbdb21c876eb01e8a9a2b16c589

Maxence Jules <Maxencejules@users.noreply.github.com> | Maxence Jules <Maxencejules@users.noreply.github.com> | Tested-tree: 133e25e067f19ddd78440cc15515e6d0cde1c572

```

Those lines are `e86f6c9a9a6d4ab47b737e4b257fe131ec0ada2a` (tree `9fc950ac4f087cbdb21c876eb01e8a9a2b16c589`) and `058f50333d5d9bf3ae5365faac66718654d634e4` (tree `133e25e067f19ddd78440cc15515e6d0cde1c572`). A platform hook appended `Co-authored-by` on the first commit attempt. That commit was replaced before any push. Published commits do not carry `Co-authored-by`. The only trailer is `Tested-tree`. The commit that inserts this paragraph uses the same author and committer.

## Runtime

The observations are in [docs/environment-capabilities.md](../environment-capabilities.md). Short form: Ubuntu 24.04.5, 4 CPUs, 15 GiB reported memory with a 10 GiB balloon inflate, no systemd, no docker socket, loopback services on 5432 and 5672.

## Acceptance checks

| Check | Status | Why |
| --- | --- | --- |
| Capability report from this VM | VERIFIED | `diagnose.sh` and the commands below |
| Install and idempotent start, state kept | VERIFIED | install exit 0 after the venv fix; second install skipped packages; restart did not run `initdb` |
| Minimal cloud profile | VERIFIED | local PostgreSQL 18.6, RabbitMQ 4.4.0, FFmpeg 6.1.1 |
| Compose file with upstream digests | IMPLEMENTED | `deployments/compose/docker-compose.yml` |
| Development storage directory | VERIFIED | round 2 smoke and restart, below |
| Compose execution | BLOCKED | `docker` is not on PATH. A read-only probe of `127.0.0.1:2375` returned Docker Engine 29.1.4. That daemon was not used, and docker was not installed. `compose.sh` exits 2 |
| Loopback bindings, generated credentials, separate devnet key | VERIFIED | listeners on `127.0.0.1`; credentials mode 0600 and gitignored; key generation refuses replace and clone |
| Database query | VERIFIED | `SELECT 1` returned 1 |
| AMQP round trip | VERIFIED | publish and consume of `goal04-roundtrip` |
| Small FFmpeg operation | VERIFIED | lavfi color to null, and a 17718-byte wav that `ffprobe` called `wav` |
| Restart keeps state | VERIFIED | row `goal04` and durable body `goal04-persist` survived stop and start |
| Wrong password fails clearly | VERIFIED | psql exit 2, `password authentication failed` |
| Missing credentials with existing state fail clearly | VERIFIED | start exit 2, refuses to reset |
| Busy port fails clearly | VERIFIED | `require-port.sh` exit 1. `start.sh` exits 1 when 5432 is taken and when 5672 is taken |
| `make devnet-up`, `make smoke`, `make fault`, `make release-check` | VERIFIED as still failed | each exits 2. They are not this goal's success path |
| Full-stack upload-to-playback | BLOCKED | no chain binary. Goal 18 |
| Browser test of the product | BLOCKED | Chrome 156 is installed. No product UI |
| Network-fault injection | BLOCKED | `tc` is absent. Goal 29 |
| Load test | BLOCKED | Goal 28. No load runner |

## Commands and output

The first `bash scripts/env/install.sh` exited 1. `python3 -m venv` failed because `python3.12-venv` was not installed. The script was changed to install `python3.12-venv=3.12.3-1ubuntu0.17`. The next run exited 0. A later run printed `pinned packages already installed` and `RabbitMQ 4.4.0 already extracted` and exited 0.

Package versions after that run:

```text
postgresql-18 18.6-1.pgdg24.04+2
postgresql-client-18 18.6-1.pgdg24.04+2
erlang-base 1:27.3.4.18-1rmq1ppa1~ubuntu24.04.1
ffmpeg 7:6.1.1-3ubuntu5
amqp-tools 0.11.0-1ubuntu0.2
python3.12-venv 3.12.3-1ubuntu0.17
postgres (PostgreSQL) 18.6 (Ubuntu 18.6-1.pgdg24.04+2)
```

`pg_lsclusters` showed `18/main` down at `/var/lib/postgresql/18/main`. `/usr/sbin/policy-rc.d` was absent after install. The apt log contains `invoke-rc.d: policy-rc.d denied execution of start.`

RabbitMQ signature check, exit 0, with the expected trust warning:

```text
gpg: Good signature from "RabbitMQ Release Signing Key <info@rabbitmq.com>" [unknown]
gpg: WARNING: This key is not certified with a trusted signature!
Primary key fingerprint: 0A9A F211 5F46 87BD 2980  3A20 6B73 A36E 6026 DFCA
```

First `bash scripts/env/start.sh` exited 0. It created `.local/workspaces/dev/credentials.env` mode 0600, ran `initdb`, and started both services. `psql` then printed `FATAL: database "svn_db" does not exist` because `initdb` creates the `postgres` database for this username. `createdb` still created `svn_dev`. The script was changed to query the `postgres` database. The restart run below uses that check and does not print that FATAL.

Listeners after start: `127.0.0.1:5432`, `127.0.0.1:5672`, `127.0.0.1:4369`, `127.0.0.1:25672`. RabbitMQ log: `started TCP listener on 127.0.0.1:5672`. `rabbitmqctl list_users` names only `svn_queue`.

`git check-ignore -v` matched `.gitignore` rule `.local/` for the credentials file, `rabbitmq.conf`, `validator.key`, and `ffmpeg/tone.wav`.

```text
COMMAND bash scripts/env/smoke.sh
env-smoke: postgres query
 env_smoke_ok
--------------
            1
(1 row)

env-smoke: amqp round trip
env-smoke: amqp round trip matched
env-smoke: ffmpeg lavfi
env-smoke: ffmpeg wrote /workspace/.local/workspaces/dev/ffmpeg/tone.wav
env-smoke: smoke passed
EXIT:0
```

`ffprobe` printed `wav`. The file is 17718 bytes. It is a generated sine, not a recording of a person, and it is gitignored.

A restart attempt before the port-check fix exited 1. PostgreSQL came back. RabbitMQ's distribution port was in `TIME_WAIT`, and a bind without `SO_REUSEADDR` reported it as busy. The check now sets `SO_REUSEADDR`. A listening socket still fails that check. The rerun:

```text
COMMAND bash scripts/env/prove-restart.sh
env-start: postgres data directory already exists; not running initdb
env-start: database svn_dev already exists
env-start: starting rabbitmq
env-start: rabbitmq is ready on 127.0.0.1:5672
env-prove-restart: stored the marker and a durable message
env-stop: postgres stopped
env-stop: rabbitmq stopped
env-start: postgres data directory already exists; not running initdb
env-start: database svn_dev already exists
env-start: starting rabbitmq
env-start: rabbitmq is ready on 127.0.0.1:5672
env-prove-restart: restart kept the database marker and the durable message
EXIT:0
```

The log also contains RabbitMQ's `Changing password for user "svn_queue"` and `Setting permissions` lines. The password was not printed. A later `SELECT note FROM env_marker WHERE id=1` returned `goal04`.

```text
COMMAND bash scripts/env/start.sh
env-start: postgres and rabbitmq are already running for /workspace/.local/workspaces/dev. State was not regenerated.
EXIT:0
```

```text
COMMAND PGPASSWORD=incorrect psql -h 127.0.0.1 -p 5432 -U svn_db -d svn_dev -c 'SELECT 1'
psql: error: connection to server at "127.0.0.1", port 5432 failed: FATAL:  password authentication failed for user "svn_db"
EXIT:2
```

```text
COMMAND bash scripts/env/prove-failures.sh
env-prove-failures: busy port
env-prove-failures: busy port failed clearly (exit 1)
env-prove-failures: wrong database password
env-prove-failures: wrong password failed clearly (exit 2)
env-prove-failures: missing credentials with existing state
env-prove-failures: missing credentials failed clearly (exit 2)
env-prove-failures: failure proofs passed
EXIT:0
```

Busy-port text from `require-port.sh` against a listening socket: `port 55991 on 127.0.0.1 is not available: [Errno 98] Address already in use`, exit 1.

```text
COMMAND bash scripts/env/compose.sh
env-compose: docker is not installed. The compose profile is BLOCKED on this machine. Use scripts/env/start.sh for local PostgreSQL and RabbitMQ. Do not substitute another database.
EXIT:2
```

```text
COMMAND bash scripts/env/devnet-keys.sh --workspace dev
env-devnet-keys: wrote a new devnet identity for workspace dev
env-devnet-keys: public fingerprint 34a831d2f4367a7ce837b273eb90189fcc2c5d917a3abc24a54590958095a292
EXIT:0

COMMAND bash scripts/env/devnet-keys.sh --workspace dev
env-devnet-keys: refusing to replace the devnet identity at /workspace/.local/workspaces/dev/devnet
EXIT:2

COMMAND bash scripts/env/devnet-keys.sh --workspace dev --from other
env-devnet-keys: refusing to clone a validator identity into this workspace
EXIT:2
```

The private key was not printed. Mode of `validator.key` is `600`.

```text
COMMAND bash scripts/env/reset.sh --workspace scratch
env-reset: refusing to reset. Pass --workspace NAME --confirm NAME. NAME is one disposable workspace.
EXIT:2

COMMAND bash scripts/env/reset.sh --workspace scratch --confirm scratch
env-reset: deleted disposable workspace scratch at /workspace/.local/workspaces/scratch
EXIT:0
```

The `dev` workspace was still present after the scratch reset.

```text
COMMAND make devnet-up
devnet-up is not implemented. Goal 05 starts four validator processes.
EXIT:2

COMMAND make smoke
smoke is not implemented. Goal 18 is the joined upload-to-playback path.
EXIT:2

COMMAND make fault
fault is not implemented. Goal 29 owns fault injection.
EXIT:2

COMMAND make release-check
release-check is not implemented. Goal 27 prepares a release. Public launch is disabled.
EXIT:2
```

`.venv/bin/python -m pytest tests/unit/test_env_profile.py -q` : 9 passed, exit 0, after the workspace-name refusal was fixed. `.venv/bin/ruff check` and `.venv/bin/mypy` on that file exited 0. `.venv/bin/python scripts/scan_secrets.py` printed `secret scan ok`, exit 0. Local `make check` exited 0. The inner `devnet-up` exited 2, which that target requires.

## Hosted CI

[41239714767](https://github.com/Maxencejules/shortvideo-network/actions/runs/41239714767) ran on `93703d3c15031309cd6974d9b97510f3b245702e` and completed with conclusion `success`. Jobs `python`, `node`, `go`, and `contract` succeeded. Earlier runs [41238027067](https://github.com/Maxencejules/shortvideo-network/actions/runs/41238027067) and [41238238274](https://github.com/Maxencejules/shortvideo-network/actions/runs/41238238274) also succeeded on older heads. The run for the commit that adds the round 3 logs is not one of these.

## Handoff for Max

- This run's environment is `6d1d563c-d493-11f1-89ee-7aa6f0729b11`. `environmentJsonPath` is null, so the saved configuration does not read `.cursor/environment.json`. Recording that file, snapshotting, and starting a build are Max's actions. This goal did not publish or republish an environment, change repository permissions, or provision a machine.
- Before Goal 05, confirm the checkout:

```bash
git fetch origin main
git rev-parse --abbrev-ref HEAD
git rev-parse HEAD
git merge-base --is-ancestor ab5680b4d95da9104bd53b6c294d6eee485b1480 HEAD
```

- Compose execution needs a Linux host where `docker info` succeeds, and Max's approval before anyone installs a container runtime or uses the daemon already listening on `127.0.0.1:2375`. The pinned digests are linux/amd64 only. Do not treat this VM's exit 2 as a successful compose run.
- No snapshot and no prepared template may include `/workspace/.local`. That directory holds generated passwords, the queue cookie, database files, and the devnet private key.
- Celery 5.6.3 is pinned by sdist hash and is not installed. Goal 14 imports it.
- The FFmpeg that ran is Ubuntu's GPL `7:6.1.1-3ubuntu5` build. Goal 14 records the isolated worker build. DEC-006 still blocks a project `LICENSE` file.
- Devnet keys exist only under `.local/workspaces/dev/devnet/` on this VM. Do not copy that directory into another agent. Goal 05 generates or uses a fresh identity per workspace.
- `make devnet-up` stays failed until Goal 05. There is no chain binary.
- Full-stack, browser-product, network-fault, and load checks stay BLOCKED for the runners named in `docs/progress.md`.
- The model that ran is `grok-4.7-high-fast`. Max ruled that the playbook model requirement is not a blocker.

## Round 2

Review FIX on `993c34bbc37c7fb42132db8b578a75b6f2341f59`. These commands ran on tree `ffb9654d2648bd693dc468dc905dcf9404e69212`. The blocks below are shortened excerpts, not raw transcripts. They omit initdb, `pg_ctl`, and several script lines. The raw record of a complete run is round 3.

```text
COMMAND bash scripts/env/reset.sh --workspace dev --confirm dev
env-reset: deleted disposable workspace dev at /workspace/.local/workspaces/dev
EXIT:0

COMMAND bash scripts/env/start.sh
env-start: wrote new credentials at /workspace/.local/workspaces/dev/credentials.env (mode 0600). The password is not printed.
env-start: initialising a new postgres data directory
env-start: postgres is ready on 127.0.0.1:5432
env-start: created database svn_dev
env-start: rabbitmq is ready on 127.0.0.1:5672
env-start: workspace /workspace/.local/workspaces/dev is ready. svn_db is the database superuser. Chain files were not changed.
EXIT:0

COMMAND bash scripts/env/smoke.sh
env-smoke: postgres query
SELECT 1 returned 1
env-smoke: amqp round trip matched
env-smoke: ffmpeg wrote /workspace/.local/workspaces/dev/ffmpeg/tone.wav
env-smoke: development storage is present at /workspace/.local/workspaces/dev/storage
env-smoke: smoke passed
EXIT:0

COMMAND bash scripts/env/prove-restart.sh
env-prove-restart: restart kept the database marker, the durable message, and the storage directory
EXIT:0

COMMAND bash scripts/env/prove-failures.sh
env-prove-failures: busy port failed clearly (exit 1)
env-prove-failures: wrong password failed clearly (exit 2)
env-prove-failures: missing credentials failed clearly (exit 2)
env-prove-failures: taken postgres port failed clearly (exit 1)
env-prove-failures: taken rabbitmq port failed clearly (exit 1)
env-prove-failures: failure proofs passed
EXIT:0
```

`storage/marker` still contained `goal04-storage` after restart. `storage/smoke.txt` contained `goal04-smoke`. Listeners after the sequence were `127.0.0.1:5432`, `127.0.0.1:5672`, `127.0.0.1:4369`, and `127.0.0.1:25672`.

`svn_db` is the cluster superuser created by `initdb --username`. Goal 12 is the first goal that opens PostgreSQL for application queries, and that goal needs a separate application role.

Queue and database clients read passwords from mode `0600` files (`amqp.auth`, `pgpass`). `start.sh` does not pass the queue password to `rabbitmqctl`. The broker learns it from `rabbitmq.conf` on first boot. `ps` can still show `PGPASSFILE` and `--authfile` paths. It does not show the password in those commands.

```text
COMMAND curl -sS -m 5 -D - http://127.0.0.1:2375/version
HTTP/1.1 200 OK
Api-Version: 1.52
Content-Type: application/json
Docker-Experimental: false
Ostype: linux
Server: Docker/29.1.4 (linux)
Date: Sat, 31 Oct 2026 20:28:48 GMT
Content-Length: 805

{"Platform":{"Name":"Docker Engine - Community"},"Version":"29.1.4","ApiVersion":"1.52","MinAPIVersion":"1.44","Os":"linux","Arch":"amd64","Components":[{"Name":"Engine","Version":"29.1.4","Details":{"ApiVersion":"1.52","Arch":"amd64","BuildTime":"2026-01-08T19:56:47.000000000+00:00","Experimental":"false","GitCommit":"08440b6","GoVersion":"go1.25.5","KernelVersion":"6.12.94+","MinAPIVersion":"1.44","Os":"linux"}},{"Name":"containerd","Version":"v2.2.5","Details":{"GitCommit":"e53c7c1516c3b2bff98eb76f1f4117477e6f4e66"}},{"Name":"runc","Version":"1.3.6","Details":{"GitCommit":"v1.3.6-0-g491b69ba"}},{"Name":"docker-init","Version":"0.19.0","Details":{"GitCommit":"de40ad0"}}],"GitCommit":"08440b6","GoVersion":"go1.25.5","KernelVersion":"6.12.94+","BuildTime":"2026-01-08T19:56:47.000000000+00:00"}
EXIT:0
```

`DOCKER_HOST` was unset. The body names Engine 29.1.4, containerd v2.2.5, runc 1.3.6. This probe only sent GET `/version`. No container was created. `docker` and `podman` are still not on `PATH`. `docker.io` `29.1.3-0ubuntu3~24.04.2` is the apt candidate and was not installed. Installing it, or driving the daemon on port 2375, needs Max's approval. Compose execution stays BLOCKED.

The compose volume is `/var/lib/postgresql`, because the pinned PostgreSQL 18.6 image uses `PGDATA=/var/lib/postgresql/18/docker` and exits 1 if `/var/lib/postgresql/data` is the mount. The image digests are linux/amd64 manifests. The ffmpeg and amqp-tools sha256 lines in `deployments/pins/checksums.txt` are hashes of the downloaded debs. They were not compared to a signed Ubuntu Release file. apt checked those packages when it installed them.

## Round 3

Review FIX on `93703d3c15031309cd6974d9b97510f3b245702e`. The script edits in `238d98c0f52644bd71a69e17c79c8075bb94af26` were committed first. These commands then ran on that commit's tree, `53e652ae5f8806cede7f4933eec80b08b27cb78e`. The log files were copied afterwards and do not change the scripts. Each file is the unmodified stdout and stderr of one command. Exit codes are in [exit-codes.txt](logs/goal-04/exit-codes.txt).

| Command | Exit | Raw output |
| --- | --- | --- |
| `bash scripts/env/reset.sh --workspace dev --confirm dev` | 0 | [reset.txt](logs/goal-04/reset.txt) |
| `bash scripts/env/start.sh` | 0 | [start.txt](logs/goal-04/start.txt) |
| `bash scripts/env/smoke.sh` | 0 | [smoke.txt](logs/goal-04/smoke.txt) |
| `bash scripts/env/prove-restart.sh` | 0 | [prove-restart.txt](logs/goal-04/prove-restart.txt) |
| `bash scripts/env/prove-failures.sh` | 0 | [prove-failures.txt](logs/goal-04/prove-failures.txt) |

`start.txt` includes the initdb transcript, `waiting for server to start.... done`, `env-start: starting rabbitmq`, and the final ready line. It does not contain `removed the default broker user`. On this fresh node `list_users` had no `guest` user, so that line was not printed. `smoke.txt` includes `env-smoke: postgres query`, the `env_smoke_ok` table, `env-smoke: amqp round trip`, and `env-smoke: ffmpeg lavfi`. `prove-restart.txt` includes one `ConnectionRefusedError` from the readiness wait before the listener accepted a connection. The command still exited 0.

After those five commands, `rabbitmqctl list_queues name --quiet` exited 0. The raw names are [queues.txt](logs/goal-04/queues.txt): `svn-persist` and `svn-smoke`. `svn-auth-probe` is not in that list. `start.sh` deletes it after the login check.

The password values from `.local/workspaces/dev/credentials.env` were compared with these files and were not present.
