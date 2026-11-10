# Isolated video processing

Status of this note: PROPOSED. It is the goal 14 integration record. It is not an approved package format, not an approved queue, and not an approved allowance. DR-14-01 through DR-14-04 are open. DR-13-01 through DR-13-03 stay open. The technical part of DR-13-04 is closed by the sandbox probe recorded in the goal 14 evidence; the approval question stays open. Public launch: DISABLED. No tradable token.

`python -m svn_media` is the process. It is not the API process. `svn_api.serve` still raises.

## What it reads

Goal 13 leaves one `api.processing_jobs` row with status `QUEUED` and the input at `quarantine/<object_name>`, where `object_name` is 64 hex characters. The worker claims that row with `FOR UPDATE SKIP LOCKED` into `media.attempts` (`CLAIMED`) and copies the ids into its filesystem queue. It does not change `api.quotas`, `api.uploads.status`, or `api.chain_outbox`. Those stay `PROCESSING_QUEUED`, `QUEUED`, and `HELD`. The database password is an environment variable of the `psql` child, not an argument.

Celery is not imported. RabbitMQ is not required. That choice is DR-14-01.

## What it writes

A package is a directory `packages/<content_id>/v<content_version>/`:

| Path | Bytes |
| --- | --- |
| `manifest.json` | SVN-JCS-1 subset of `docs/protocol/schemas/media-manifest.schema.json`. Canonical JSON, no trailing newline. |
| `segments/<rendition>-0.mp4` | H.264, `yuv420p`, AAC when the input has audio. One file per rendition. |
| `segments/thumb-0.jpg` | One JPEG. The short side is at most 240, and it is not upscaled. |
| `segments/captions-0.vtt` | The user-provided WebVTT bytes, copied as data, only when they are valid UTF-8 and start with `WEBVTT`. |

`package_bytes` is the sum of the segment files. It does not include `manifest.json`, because that file names the sum. The manifest SHA-256 is the hash of the canonical manifest bytes and is stored on the ready job record, not inside the manifest.

Renditions, after display orientation is applied:

- `h480` when the short side is at least 480. The long side is at most 854.
- `h720` when the short side is at least 720. The long side is at most 1280.
- `native` when neither rung fits. Both output sides are at most the display sides.

A display matrix is left to FFmpeg's autorotate. A rotate tag without a matrix is baked with a fixed transpose. A new encode of the same `content_id` is `content_version + 1` and a new directory. The previous directory stays. A retry of the same job does not create another version. This follows the goal text. `docs/protocol.md` still says a second encoding is a new `content_id` until a recorded decision. That conflict is DR-14-02. Separate encoders are not required to match bytes. The committed identity is the digest.

The ready record status is `READY_FOR_REVIEW` and `publication` is `NOT_PUBLISHED`. `media.packages.status` can only be `READY_FOR_REVIEW`. There is no publish route.

Failure records are `code` and the fixed user sentence for that code. They do not contain a path, a stack trace, or a secret.

## Isolation that this process actually applies

Each FFmpeg and ffprobe invocation is an argument list:

`prlimit --cpu --as --fsize --nofile -- unshare --user --map-root-user --net -- <tool> -protocol_whitelist file,crypto,pipe ...`

The child environment is `PATH`, `LC_ALL`, `LANG`, and `TZ`. There is no shell. `unshare` is used only when that command can start a child. A kernel that refuses the user namespace keeps the rlimits and the protocol whitelist. On a host that allows it, a connect to a local listener fails inside the namespace, and `/proc/self/uid_map` shows namespace uid 0 mapped to the caller's host uid, not host uid 0. The address-space, CPU, and file-size limits are read back from the child. The proposed address-space cap is 2 GiB because 1 GiB of virtual address space made libx264's 720p malloc fail. The workspace is mode `0700`. Staging is removed on failure, cancel, and after the directory rename to the version path. A crash leaves the job in `jobs/running`; the next start deletes staging and either adopts one valid package or encodes once.

The root filesystem is not remounted read-only. That boundary is unverified. The external profile, not a result, is:

```text
bwrap --unshare-user --unshare-net --unshare-pid --die-with-parent \
  --ro-bind /usr /usr --ro-bind /lib /lib --ro-bind /bin /bin \
  --proc /proc --dev /dev --tmpfs /tmp \
  --bind "$WORK" "$WORK" --chdir "$WORK" -- \
  ffmpeg -hide_banner -nostdin -protocol_whitelist file,crypto,pipe ...
```

`bubblewrap` was not installed here. Do not run this process on a host that holds a validator key. The worker refuses to start when `priv_validator_key.json` or `node_key.json` is in its root.

## Handoff

Goal 15 reads `packages/<content_id>/v<content_version>/manifest.json` only when the ready record says `READY_FOR_REVIEW` and a local playback grant names that digest. It does not serve the quarantine directory. The playback rules are in [delivery.md](delivery.md). Goal 16 may copy segment bytes after checking the manifest hashes. The chain outbox stays `HELD`. The worker does not publish.
