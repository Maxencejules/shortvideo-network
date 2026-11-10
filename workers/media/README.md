# workers/media

Isolated FFmpeg worker for one queued upload. `python -m svn_media run --root <dir>` recovers a crashed job, then encodes every filesystem job under that root. `python -m svn_media claim-run` first claims `api.processing_jobs` rows through `psql`. `python -m svn_media build-record` prints the FFmpeg build on this host.

The shell is not used. Caption text is not a command argument. The process does not publish and does not hold a signer key. The package layout and the open decisions are in `docs/architecture/media-worker.md`.
