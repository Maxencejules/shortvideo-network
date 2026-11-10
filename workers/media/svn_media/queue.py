"""Filesystem claim queue and the PostgreSQL adapter for api.processing_jobs.

The database password is passed only in the psql child's environment, never as
an argument. Job rows store generated ids. They do not store filesystem paths.
"""

from __future__ import annotations

import json
import os
import shutil
import subprocess
import time
from collections.abc import Mapping
from pathlib import Path

from svn_media.encode import process_input
from svn_media.limits import Failure, Limits, check_id, check_object_name, check_sha256

_KEY_NAMES = frozenset({"priv_validator_key.json", "node_key.json"})
_MEDIA_SQL = """
CREATE SCHEMA IF NOT EXISTS media;
CREATE TABLE IF NOT EXISTS media.attempts (
    job_id text PRIMARY KEY,
    state text NOT NULL,
    failure_code text,
    content_version integer,
    claimed_at timestamptz NOT NULL,
    CHECK (state IN ('CLAIMED', 'READY_FOR_REVIEW', 'FAILED', 'CANCELLED')),
    CHECK (failure_code IS NULL OR failure_code ~ '^[A-Z0-9_]{1,32}$')
);
CREATE TABLE IF NOT EXISTS media.packages (
    content_id text NOT NULL,
    content_version integer NOT NULL,
    job_id text NOT NULL,
    manifest_sha256 text NOT NULL,
    package_bytes bigint NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    PRIMARY KEY (content_id, content_version),
    CHECK (status = 'READY_FOR_REVIEW'),
    CHECK (content_version >= 1),
    CHECK (package_bytes >= 0),
    CHECK (manifest_sha256 ~ '^[0-9a-f]{64}$')
);
"""
_CLAIM_SQL = """
WITH picked AS (
    SELECT j.job_id
    FROM api.processing_jobs j
    WHERE j.status = 'QUEUED'
      AND NOT EXISTS (SELECT 1 FROM media.attempts a WHERE a.job_id = j.job_id)
    ORDER BY j.created_at
    FOR UPDATE OF j SKIP LOCKED
)
INSERT INTO media.attempts (job_id, state, claimed_at)
SELECT job_id, 'CLAIMED', now() FROM picked
RETURNING job_id;
"""
_LIST_SQL = """
SELECT row_to_json(t)
FROM (
    SELECT a.job_id, u.content_id, u.object_name, i.input_sha256,
           i.output_budget_bytes, i.input_bytes
    FROM media.attempts a
    JOIN api.processing_jobs j ON j.job_id = a.job_id
    JOIN api.uploads u ON u.upload_id = j.upload_id
    JOIN api.creator_intents i ON i.upload_id = u.upload_id
    WHERE a.state = 'CLAIMED'
    ORDER BY a.job_id
) t;
"""


class Pg:
    """A local PostgreSQL session. `password` is not logged and not placed in argv."""

    def __init__(self, socket_dir: Path, user: str, database: str, password: str) -> None:
        self.socket_dir = socket_dir
        self.user = user
        self.database = database
        self._password = password

    def execute(self, sql: str, variables: dict[str, str] | None = None) -> str:
        argv = [
            "psql",
            "-X",
            "-q",
            "-v",
            "ON_ERROR_STOP=1",
            "-h",
            str(self.socket_dir),
            "-p",
            "5432",
            "-U",
            self.user,
            "-d",
            self.database,
            "-A",
            "-t",
            "-P",
            "pager=off",
            "-f",
            "-",
        ]
        for key, value in (variables or {}).items():
            if not key.isidentifier():
                raise Failure("INTERNAL")
            argv.extend(["-v", f"{key}={value}"])
        env = {
            "PATH": "/usr/bin:/bin",
            "LC_ALL": "C.UTF-8",
            "TZ": "UTC",
            "PGPASSWORD": self._password,
        }
        try:
            proc = subprocess.run(
                argv,
                input=sql.encode("utf-8"),
                capture_output=True,
                env=env,
                timeout=30,
                check=False,
            )
        except (OSError, subprocess.TimeoutExpired):
            raise Failure("INTERNAL") from None
        if proc.returncode != 0:
            raise Failure("INTERNAL") from None
        return proc.stdout.decode("utf-8", "replace")

    def migrate(self) -> None:
        self.execute(_MEDIA_SQL)

    def pull(self, workspace: Workspace) -> int:
        """Claim QUEUED upload jobs and enqueue any CLAIMED job the workspace does not have."""
        self.migrate()
        self.execute(_CLAIM_SQL)
        text = self.execute(_LIST_SQL)
        count = 0
        for line in text.splitlines():
            if not line.strip():
                continue
            row = json.loads(line)
            job_id = str(row["job_id"])
            if workspace.has_job(job_id):
                continue
            workspace.enqueue(
                job_id=job_id,
                content_id=str(row["content_id"]),
                object_name=str(row["object_name"]),
                output_budget_bytes=int(row["output_budget_bytes"]),
                content_version=workspace.next_version(str(row["content_id"])),
                input_sha256=str(row["input_sha256"]),
            )
            count += 1
        return count

    def record_ready(self, record: dict[str, object]) -> None:
        self.execute(
            """
            WITH upd AS (
                UPDATE media.attempts
                SET state = 'READY_FOR_REVIEW',
                    content_version = CAST(:'ver' AS integer),
                    failure_code = NULL
                WHERE job_id = :'job_id' AND state IN ('CLAIMED', 'READY_FOR_REVIEW')
                RETURNING job_id
            )
            INSERT INTO media.packages (
                content_id, content_version, job_id, manifest_sha256,
                package_bytes, status, created_at
            )
            SELECT :'content_id', CAST(:'ver' AS integer), job_id, :'sha',
                   CAST(:'bytes' AS bigint), 'READY_FOR_REVIEW', now()
            FROM upd
            ON CONFLICT (content_id, content_version) DO NOTHING;
            """,
            {
                "job_id": str(record["job_id"]),
                "ver": str(record["content_version"]),
                "content_id": str(record["content_id"]),
                "sha": str(record["manifest_sha256"]),
                "bytes": str(record["package_bytes"]),
            },
        )

    def record_failed(self, job_id: str, code: str) -> None:
        check_id(job_id)
        if not code.isascii() or not code.replace("_", "").isalnum() or len(code) > 32:
            code = "INTERNAL"
        self.execute(
            """
            UPDATE media.attempts
            SET state = 'FAILED', failure_code = :'code'
            WHERE job_id = :'job_id' AND state = 'CLAIMED';
            """,
            {"job_id": job_id, "code": code},
        )


class Workspace:
    """One worker root: quarantine, staging, and ready-for-review packages."""

    def __init__(
        self,
        root: Path,
        *,
        limits: Limits | None = None,
        ffmpeg: str = "ffmpeg",
        ffprobe: str = "ffprobe",
        pg: Pg | None = None,
    ) -> None:
        self.root = root.resolve()
        self.limits = limits or Limits()
        self.ffmpeg = ffmpeg
        self.ffprobe = ffprobe
        self.pg = pg
        self._faulted = False
        self.root.mkdir(parents=True, mode=0o700, exist_ok=True)
        self._refuse_keys()
        for name in (
            "quarantine",
            "jobs/queued",
            "jobs/running",
            "jobs/ready",
            "jobs/failed",
            "jobs/cancel",
            "staging",
            "packages",
            "captions",
        ):
            path = self.root / name
            path.mkdir(parents=True, mode=0o700, exist_ok=True)

    def has_job(self, job_id: str) -> bool:
        check_id(job_id)
        name = f"{job_id}.json"
        states = ("queued", "running", "ready", "failed")
        return any((self.root / "jobs" / state / name).is_file() for state in states)

    def next_version(self, content_id: str) -> int:
        check_id(content_id)
        versions = [1]
        base = self.root / "packages" / content_id
        if base.is_dir():
            for child in base.iterdir():
                if child.name.startswith("v") and child.name[1:].isdigit():
                    versions.append(int(child.name[1:]) + 1)
        return max(versions)

    def enqueue(
        self,
        *,
        job_id: str,
        content_id: str,
        object_name: str,
        output_budget_bytes: int,
        content_version: int,
        input_sha256: str | None = None,
        captions: bytes | None = None,
        captions_reviewed: bool = False,
    ) -> None:
        check_id(job_id)
        check_id(content_id)
        check_object_name(object_name)
        if input_sha256 is not None:
            check_sha256(input_sha256)
        if content_version < 1 or output_budget_bytes < 1:
            raise Failure("REFUSED")
        manifest_id = f"m{content_version}-{content_id}"
        check_id(manifest_id)
        if self.has_job(job_id):
            raise Failure("REFUSED")
        if captions is not None:
            _atomic_bytes(self.root / "captions" / f"{job_id}.vtt", captions)
        record = {
            "job_id": job_id,
            "content_id": content_id,
            "object_name": object_name,
            "output_budget_bytes": output_budget_bytes,
            "content_version": content_version,
            "input_sha256": input_sha256,
            "captions_reviewed": captions_reviewed,
        }
        _atomic_json(self.root / "jobs" / "queued" / f"{job_id}.json", record)

    def cancel(self, job_id: str) -> None:
        check_id(job_id)
        _atomic_json(self.root / "jobs" / "cancel" / f"{job_id}.json", {"job_id": job_id})
        queued = self.root / "jobs" / "queued" / f"{job_id}.json"
        if queued.is_file():
            running = self.root / "jobs" / "running" / queued.name
            os.rename(queued, running)
            self._fail(job_id, Failure("CANCELLED"))

    def reencode(self, content_id: str) -> str:
        """Queue a new package version of a ready item. The previous version stays."""
        check_id(content_id)
        ready = self._latest_ready(content_id)
        if ready is None:
            raise Failure("REFUSED")
        job_id = "j" + os.urandom(16).hex()
        captions_path = self.root / "captions" / f"{ready['job_id']}.vtt"
        captions = captions_path.read_bytes() if captions_path.is_file() else None
        digest = ready.get("input_sha256")
        self.enqueue(
            job_id=job_id,
            content_id=content_id,
            object_name=str(ready["object_name"]),
            output_budget_bytes=int(str(ready["output_budget_bytes"])),
            content_version=self.next_version(content_id),
            input_sha256=str(digest) if isinstance(digest, str) else None,
            captions=captions,
            captions_reviewed=bool(ready.get("captions_reviewed")),
        )
        return job_id

    def run_once(self) -> dict[str, object] | None:
        """Recover, then process one queued job. A crash before ready leaves the job queued."""
        self.recover()
        running = self._claim()
        if running is None:
            return None
        job_id = running.stem
        try:
            record = self._process(running)
        except Failure as exc:
            self._fail(job_id, exc)
            if self.pg is not None:
                self.pg.record_failed(job_id, exc.code)
            raise
        if self.pg is not None:
            self.pg.record_ready(record)
        return record

    def recover(self) -> None:
        packages = self.root / "packages"
        if packages.is_dir():
            for partial in packages.glob("*/.v*.partial"):
                shutil.rmtree(partial, ignore_errors=True)
        for running in sorted((self.root / "jobs" / "running").glob("*.json")):
            job = _read_job(running)
            ready = self.root / "jobs" / "ready" / running.name
            final = self._version_dir(job)
            if ready.is_file() or (final.is_dir() and _package_valid(final)):
                if not ready.is_file():
                    self._write_ready(job, final)
                running.unlink(missing_ok=True)
                shutil.rmtree(self.root / "staging" / str(job["job_id"]), ignore_errors=True)
                continue
            if final.exists():
                shutil.rmtree(final, ignore_errors=True)
            shutil.rmtree(self.root / "staging" / str(job["job_id"]), ignore_errors=True)
            os.rename(running, self.root / "jobs" / "queued" / running.name)

    def _claim(self) -> Path | None:
        queued = sorted((self.root / "jobs" / "queued").glob("*.json"))
        if not queued:
            return None
        dest = self.root / "jobs" / "running" / queued[0].name
        os.rename(queued[0], dest)
        return dest

    def _process(self, running: Path) -> dict[str, object]:
        job = _read_job(running)
        job_id = str(job["job_id"])
        content_id = str(job["content_id"])
        version = int(str(job["content_version"]))
        source = self._source(str(job["object_name"]))
        final = self._version_dir(job)
        if final.is_dir() and _package_valid(final):
            record = self._write_ready(job, final)
            running.unlink(missing_ok=True)
            return record
        staging = self.root / "staging" / job_id
        shutil.rmtree(staging, ignore_errors=True)
        staging.mkdir(mode=0o700)
        captions_path = self.root / "captions" / f"{job_id}.vtt"
        captions = captions_path.read_bytes() if captions_path.is_file() else None
        digest = job.get("input_sha256")
        self._faulted = False
        try:
            package = process_input(
                source,
                staging,
                limits=self.limits,
                ffmpeg=self.ffmpeg,
                ffprobe=self.ffprobe,
                content_id=content_id,
                content_version=version,
                manifest_id=f"m{version}-{content_id}",
                output_budget=int(str(job["output_budget_bytes"])),
                expected_sha256=str(digest) if isinstance(digest, str) else None,
                captions=captions,
                tick=lambda: self._tick(job_id),
            )
            partial = final.parent / f".v{version}.partial"
            final.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
            if partial.exists():
                shutil.rmtree(partial)
            os.rename(staging, partial)
            os.rename(partial, final)
        except Failure:
            shutil.rmtree(staging, ignore_errors=True)
            raise
        record = self._write_ready(job, final, package.manifest_sha256, package.package_bytes)
        running.unlink(missing_ok=True)
        return record

    def _tick(self, job_id: str) -> None:
        if (self.root / "jobs" / "cancel" / f"{job_id}.json").is_file():
            raise Failure("CANCELLED")
        if os.environ.get("SVN_MEDIA_FAULT") != "after-segment" or self._faulted:
            return
        segments = self.root / "staging" / job_id / "segments"
        if not segments.is_dir() or not any(segments.iterdir()):
            return
        self._faulted = True
        _atomic_bytes(self.root / "fault-ready", b"after-segment")
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            if (self.root / "jobs" / "cancel" / f"{job_id}.json").is_file():
                raise Failure("CANCELLED")
            if (self.root / "fault-release").exists():
                return
            time.sleep(0.05)
        raise Failure("INTERNAL")

    def _source(self, object_name: str) -> Path:
        check_object_name(object_name)
        path = self.root / "quarantine" / object_name
        if path.is_symlink():
            raise Failure("REFUSED")
        try:
            resolved = path.resolve()
        except OSError:
            raise Failure("MALFORMED") from None
        if not resolved.is_relative_to((self.root / "quarantine").resolve()):
            raise Failure("REFUSED")
        if not resolved.is_file():
            raise Failure("MALFORMED")
        return resolved

    def _version_dir(self, job: dict[str, object]) -> Path:
        return self.root / "packages" / str(job["content_id"]) / f"v{job['content_version']}"

    def _write_ready(
        self,
        job: dict[str, object],
        final: Path,
        manifest_sha256: str | None = None,
        package_bytes: int | None = None,
    ) -> dict[str, object]:
        if manifest_sha256 is None or package_bytes is None:
            manifest_sha256, package_bytes = _package_identity(final)
        record: dict[str, object] = {
            "job_id": job["job_id"],
            "content_id": job["content_id"],
            "content_version": job["content_version"],
            "object_name": job["object_name"],
            "output_budget_bytes": job["output_budget_bytes"],
            "input_sha256": job.get("input_sha256"),
            "captions_reviewed": bool(job.get("captions_reviewed")),
            "status": "READY_FOR_REVIEW",
            "publication": "NOT_PUBLISHED",
            "manifest_sha256": manifest_sha256,
            "package_bytes": package_bytes,
        }
        _atomic_json(self.root / "jobs" / "ready" / f"{job['job_id']}.json", record)
        return record

    def _latest_ready(self, content_id: str) -> dict[str, object] | None:
        found: dict[str, object] | None = None
        version = 0
        for path in (self.root / "jobs" / "ready").glob("*.json"):
            job = _read_job(path)
            if job.get("content_id") != content_id:
                continue
            current = int(str(job["content_version"]))
            if current >= version:
                version = current
                found = job
        return found

    def _fail(self, job_id: str, exc: Failure) -> None:
        running = self.root / "jobs" / "running" / f"{job_id}.json"
        shutil.rmtree(self.root / "staging" / job_id, ignore_errors=True)
        record = {"job_id": job_id, "code": exc.code, "message": str(exc)}
        _atomic_json(self.root / "jobs" / "failed" / f"{job_id}.json", record)
        running.unlink(missing_ok=True)

    def _refuse_keys(self) -> None:
        """Refuse to start when a validator key file is in this workspace."""
        try:
            names = [self.root, *list(self.root.iterdir())]
        except OSError:
            raise Failure("REFUSED") from None
        for path in names:
            if (
                path.name in _KEY_NAMES
                or (path / "priv_validator_key.json").exists()
                or (path / "node_key.json").exists()
            ):
                raise Failure("REFUSED")
            if path.is_dir():
                try:
                    children = list(path.iterdir())
                except OSError:
                    raise Failure("REFUSED") from None
                for child in children:
                    if child.name in _KEY_NAMES:
                        raise Failure("REFUSED")


def _read_job(path: Path) -> dict[str, object]:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError):
        raise Failure("INTERNAL") from None
    if not isinstance(value, dict):
        raise Failure("INTERNAL")
    return value


def _package_valid(path: Path) -> bool:
    try:
        _package_identity(path)
    except Failure:
        return False
    return True


def _package_identity(path: Path) -> tuple[str, int]:
    import hashlib

    try:
        raw = (path / "manifest.json").read_text(encoding="utf-8")
        manifest = json.loads(raw)
    except (OSError, json.JSONDecodeError):
        raise Failure("INTERNAL") from None
    if not isinstance(manifest, dict):
        raise Failure("INTERNAL")
    renditions = manifest.get("renditions")
    if not isinstance(renditions, list):
        raise Failure("INTERNAL")
    total = 0
    for rendition in renditions:
        if not isinstance(rendition, dict):
            raise Failure("INTERNAL")
        segments = rendition.get("segments")
        if not isinstance(segments, list):
            raise Failure("INTERNAL")
        for segment in segments:
            if not isinstance(segment, dict):
                raise Failure("INTERNAL")
            segment_id = str(segment.get("segment_id", ""))
            check_id(segment_id)
            file = _segment_file(path, segment_id, str(rendition.get("codec", "")))
            payload = file.read_bytes()
            digest = hashlib.sha256(payload).hexdigest()
            same_hash = digest == segment.get("sha256")
            same_size = str(len(payload)) == str(segment.get("byte_count"))
            if not same_hash or not same_size:
                raise Failure("INTERNAL")
            total += len(payload)
    if str(total) != str(manifest.get("package_bytes")):
        raise Failure("INTERNAL")
    return hashlib.sha256(raw.encode("utf-8")).hexdigest(), total


def _segment_file(package: Path, segment_id: str, codec: str) -> Path:
    suffix = {"avc1": ".mp4", "jpeg": ".jpg", "webvtt": ".vtt"}.get(codec)
    if suffix is None:
        raise Failure("INTERNAL")
    path = package / "segments" / f"{segment_id}{suffix}"
    if path.is_symlink() or not path.is_file():
        raise Failure("INTERNAL")
    if not path.resolve().is_relative_to((package / "segments").resolve()):
        raise Failure("INTERNAL")
    return path


def _atomic_json(path: Path, body: Mapping[str, object]) -> None:
    payload = json.dumps(body, sort_keys=True, separators=(",", ":")).encode("utf-8")
    _atomic_bytes(path, payload)


def _atomic_bytes(path: Path, payload: bytes) -> None:
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    tmp = path.parent / f".{path.name}.tmp"
    try:
        fd = os.open(tmp, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    except OSError:
        raise Failure("INTERNAL") from None
    try:
        os.write(fd, payload)
        os.fsync(fd)
    except OSError:
        raise Failure("INTERNAL") from None
    finally:
        os.close(fd)
    os.rename(tmp, path)
