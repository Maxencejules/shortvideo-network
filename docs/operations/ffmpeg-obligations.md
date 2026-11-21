# FFmpeg obligations

Development procedure for the devnet beta. This is not a production runbook. Public launch is disabled. The package that carries this note is NOT-RELEASED.

The media worker uses the Ubuntu archive build, not a binary compiled by this repository. DEC-006 is not applied, so this record does not make the package releasable.

| Item | Record |
| --- | --- |
| Package | `ffmpeg=7:6.1.1-3ubuntu5` |
| Version line | `ffmpeg version 6.1.1-3ubuntu5` |
| Build flag | `--enable-gpl` |
| Deb checksum | `docs/dependencies.md` records sha256 `1a23baace5f2688a47e28119aedae6993cd638e6279a7227dfc52d9a337a1c17` |
| Goal 14 capture | [../evidence/logs/goal-14/build-record.txt](../evidence/logs/goal-14/build-record.txt) |

Enabled libraries in that build include `--enable-libx264`, `--enable-libx265`, `--enable-libvpx`, `--enable-libaom`, `--enable-libmp3lame`, `--enable-libopus`, and `--enable-libvorbis`. The goal 14 encoder calls libx264. A GPL build record does not settle codec patent obligations.

The worker image under `deployments/images/` does not download FFmpeg. An unpinned `apt-get` is refused. An operator who installs the worker installs this pinned package on the worker host, which is not the validator host.

The candidate command runs `ffmpeg -version` and refuses a build whose version line lacks `6.1.1-3ubuntu5` or whose configuration lacks `--enable-gpl`.
