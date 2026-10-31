# Environment capabilities

Observed on 2026-10-31 on the Cursor cloud agent that built Goal 04. These are measurements from that VM. They are not a vendor feature list and they are not a claim about every future agent.

Run `bash scripts/env/diagnose.sh` to print a fresh report. A `MISSING` line is an observation, not a passed test.

## Host

| Check | Observed |
| --- | --- |
| `uname -srmo` | `Linux 6.12.94+ x86_64 GNU/Linux` |
| `/etc/os-release` | Ubuntu 24.04.5 LTS (noble) |
| CPUs | 4, `Intel(R) Xeon(R) Processor`, KVM |
| `MemTotal` | 16387280 kB |
| `MemAvailable` after service start | 4938328 kB |
| `free -h` | 15 GiB total, about 4.7 GiB available, swap 0 |
| Balloon | `balloon_inflate 2621440`, `balloon_deflate 0` |
| Disk `/` | ext4, 254G, 7.4G used, 246G available after the package install |
| Process 1 | `tini` (`/tini -- /anyrun-init ...`) |
| `systemctl is-system-running` | `offline` |
| User | `ubuntu`, uid 1000, passwordless `sudo` |
| `ss` | not installed |
| `tc` | not installed |
| `ip` | not installed |

`/proc/net/tcp` is readable, so listening ports can be listed without `ss`.

## Bind and ports

| Probe | Result |
| --- | --- |
| `127.0.0.1` ephemeral | bound |
| `0.0.0.0` ephemeral | bound |
| `127.0.0.1:80` | `Permission denied` |
| `0.0.0.0:80` | `Permission denied` |

Ports below 1024 are denied for this user on both addresses. Unprivileged ports can bind on loopback and on `0.0.0.0`. The development profile still listens only on `127.0.0.1`. That is a profile rule. It is not because `0.0.0.0` is blocked.

After start, the profile listeners were:

| Address | Port | Process |
| --- | --- | --- |
| `127.0.0.1` | 5432 | workspace PostgreSQL |
| `127.0.0.1` | 5672 | workspace RabbitMQ |
| `127.0.0.1` | 4369 | `epmd` started for that node |
| `127.0.0.1` | 25672 | RabbitMQ distribution |

Other listeners were already present, including `0.0.0.0:2375`. This profile did not open that port. A read-only probe is in the Docker section below. It did not create a container.

## Toolchains already on the image

| Tool | Observed |
| --- | --- |
| Go | `go1.26.10 linux/amd64` at `/usr/bin/go` |
| Python | `Python 3.12.3` |
| Node | `v22.23.4` |
| npm | `10.9.9` |
| OpenSSL | present (`openssl version` in `diagnose.sh`) |
| Google Chrome | `Google Chrome 156.0.8078.47` at `/usr/local/bin/google-chrome` |

Chrome starts. There is no product UI in this repository, so a browser test of the application was not run.

## Packages that were not on the image

| Package | Archive candidate before this goal | What was installed |
| --- | --- | --- |
| PostgreSQL | Ubuntu `postgresql-16` 16.15 | PGDG `postgresql-18` `18.6-1.pgdg24.04+2` |
| Erlang | Ubuntu OTP 25.3.2.8 | Team RabbitMQ OTP `1:27.3.4.18-1rmq1ppa1~ubuntu24.04.1` |
| RabbitMQ | Ubuntu `3.12.1-1ubuntu1.7` | generic UNIX 4.4.0, not the Ubuntu package |
| FFmpeg | `7:6.1.1-3ubuntu5` | already installed, same version |
| amqp-tools | `0.11.0-1ubuntu0.2` | that version |
| python3.12-venv | `3.12.3-1ubuntu0.17` | that version, after the first install exited 1 |
| Celery | not installed | still not installed; the sdist hash is pinned |
| docker.io | `29.1.3-0ubuntu3~24.04.2`, not installed | not installed. Installing it needs Max's approval |
| podman | command not found | not installed |

`docker` and `podman` are not on `PATH`. `/var/run/docker.sock` is absent. `DOCKER_HOST` was unset. `systemctl` cannot start a service. Something is already listening on `0.0.0.0:2375`.

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

That is Docker Engine Community 29.1.4, which is not the apt candidate `29.1.3-0ubuntu3~24.04.2`. The probe was GET `/version` only. No image was pulled and no container was started. `bash scripts/env/compose.sh` exits 2 because the `docker` command is absent. Compose execution stays BLOCKED until Max approves using this daemon or installing a container runtime.

The PostgreSQL package created cluster `18/main` at `/var/lib/postgresql/18/main` and `policy-rc.d` refused to start it. That cluster is down. The disposable workspace uses its own data directory under `.local/workspaces/dev/postgres/data`. Reset does not delete `/var/lib/postgresql`.

## Network

`cursor-cloud` environment-info reported `egress.restricted` false. These downloads completed:

- `apt-get update` against Ubuntu archive, security, PGDG, and the RabbitMQ Erlang PPA
- PostgreSQL apt key from `https://www.postgresql.org/media/keys/ACCC4CF8.asc`, fingerprint `B97B0AFCAA1A47F044F244A07FCC7D46ACCC4CF8`
- Erlang key from `keyserver.ubuntu.com`, fingerprint `B279943D2A549531E144B875F77F1EDA57EBB1CC`
- RabbitMQ 4.4.0 generic tarball from GitHub, sha256 `68c22d5dafa15c0911463aeca73c362d638cd6fb8e407ad39512959c6cc37f0f`, signature matched key `0A9AF2115F4687BD29803A206B73A36E6026DFCA`
- Celery 5.6.3 sdist from PyPI, sha256 `177006bd2054b882e9f01be59abd8529e88879ef50d7918a7050c5a9f4e12912`
- FFmpeg deb sha256 `1a23baace5f2688a47e28119aedae6993cd638e6279a7227dfc52d9a337a1c17`
- Go 1.26.10 and protoc 36.2 through `scripts/install_toolchains.sh`
- `pip install --require-hashes` and `npm ci`
- Docker Hub registry reads for `library/postgres:18.6` and `library/rabbitmq:4.4.0-management`

The registry digests are in [dependencies.md](dependencies.md). The images were not pulled.

## FFmpeg build that ran

`ffmpeg version 6.1.1-3ubuntu5`, package `7:6.1.1-3ubuntu5`, built with `--enable-gpl`. The configuration line from `ffmpeg -version` is:

```text
configuration: --prefix=/usr --extra-version=3ubuntu5 --toolchain=hardened --libdir=/usr/lib/x86_64-linux-gnu --incdir=/usr/include/x86_64-linux-gnu --arch=amd64 --enable-gpl --disable-stripping --disable-omx --enable-gnutls --enable-libaom --enable-libass --enable-libbs2b --enable-libcaca --enable-libcdio --enable-libcodec2 --enable-libdav1d --enable-libflite --enable-libfontconfig --enable-libfreetype --enable-libfribidi --enable-libglslang --enable-libgme --enable-libgsm --enable-libharfbuzz --enable-libmp3lame --enable-libmysofa --enable-libopenjpeg --enable-libopenmpt --enable-libopus --enable-librubberband --enable-libshine --enable-libsnappy --enable-libsoxr --enable-libspeex --enable-libtheora --enable-libtwolame --enable-libvidstab --enable-libvorbis --enable-libvpx --enable-libwebp --enable-libx265 --enable-libxml2 --enable-libxvid --enable-libzimg --enable-openal --enable-opencl --enable-opengl --disable-sndio --enable-libvpl --disable-libmfx --enable-libdc1394 --enable-libdrm --enable-libiec61883 --enable-chromaprint --enable-frei0r --enable-ladspa --enable-libbluray --enable-libjack --enable-libpulse --enable-librabbitmq --enable-librist --enable-libsrt --enable-libssh --enable-libsvtav1 --enable-libx264 --enable-libzmq --enable-libzvbi --enable-lv2 --enable-sdl2 --enable-libplacebo --enable-librav1e --enable-pocketsphinx --enable-librsvg --enable-libjxl --enable-shared
```

This is Ubuntu's GPL build. FFmpeg 9.0.2 source was not compiled. Goal 14 still owns the isolated worker build. DEC-006 is not applied: there is no project `LICENSE` file.

Erlang/OTP 27.3.4 upstream `LICENSE.txt` fetched from tag `OTP-27.3.4` is the Apache License 2.0. sha256 `809fa1ed21450f59827d1e9aec720bbc4b687434fa22283c6cb5dd82a47ab9c0`. The installed `erlang-base` package did not ship a copyright file on `dpkg -L`.

## What this VM cannot stand in for

| Check | Status here | Why |
| --- | --- | --- |
| Compose up | BLOCKED | No container runtime was started |
| Chain devnet, four validators | BLOCKED | Goal 05. `make devnet-up` exits 2 |
| Joined upload-to-playback | BLOCKED | Goal 18. `make smoke` exits 2 |
| Browser test of the product | BLOCKED | Chrome exists. No product UI exists |
| Network fault injection | BLOCKED | `tc` is absent. Goal 29 owns the harness |
| Load run | BLOCKED | Goal 28. No load runner was started |
| Independent operator | BLOCKED | One VM, one user |

Local PostgreSQL queries, an AMQP round trip, and a small FFmpeg operation did run. Those results are in [evidence/goal-04.md](evidence/goal-04.md). They do not convert the rows above into passes.
