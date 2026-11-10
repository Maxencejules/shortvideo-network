"""PostgreSQL claim of a goal 13 processing job. Skipped only when the server is absent."""

from __future__ import annotations

import hashlib
import json
import signal
import subprocess
import time
from pathlib import Path

import pytest
from svn_media import Failure, Pg, Workspace

INITDB = Path("/usr/lib/postgresql/18/bin/initdb")
POSTGRES = Path("/usr/lib/postgresql/18/bin/postgres")

SCHEMA = """
CREATE SCHEMA api;
CREATE TABLE api.quotas (
    account_id text PRIMARY KEY,
    input_reserved bigint NOT NULL,
    output_reserved bigint NOT NULL
);
CREATE TABLE api.uploads (
    upload_id text PRIMARY KEY,
    content_id text NOT NULL,
    object_name text NOT NULL,
    status text NOT NULL,
    CHECK (status IN (
        'RESERVED', 'RECEIVING', 'RECEIVED', 'PROBING',
        'PROCESSING_QUEUED', 'REJECTED', 'EXPIRED', 'CANCELLED'
    ))
);
CREATE TABLE api.processing_jobs (
    job_id text PRIMARY KEY,
    upload_id text NOT NULL,
    status text NOT NULL,
    created_at timestamptz NOT NULL,
    CHECK (status = 'QUEUED')
);
CREATE TABLE api.creator_intents (
    upload_id text PRIMARY KEY,
    input_sha256 text NOT NULL,
    input_bytes bigint NOT NULL,
    output_budget_bytes bigint NOT NULL
);
CREATE TABLE api.chain_outbox (
    upload_id text PRIMARY KEY,
    status text NOT NULL,
    CHECK (status = 'HELD')
);
"""


class _Cluster:
    def __init__(self, root: Path) -> None:
        self.root = root
        root.mkdir(parents=True)
        data = root / "data"
        self.sock = root / "run"
        self.sock.mkdir()
        subprocess.run(
            [
                str(INITDB),
                "-D",
                str(data),
                "-U",
                "svn_test",
                "--auth-local=trust",
                "--auth-host=reject",
                "-N",
            ],
            check=True,
            capture_output=True,
        )
        self.proc = subprocess.Popen(
            [
                str(POSTGRES),
                "-D",
                str(data),
                "-k",
                str(self.sock),
                "-p",
                "5432",
                "-c",
                "listen_addresses=",
                "-c",
                "unix_socket_permissions=0700",
            ],
            stdout=subprocess.DEVNULL,
            stderr=subprocess.PIPE,
        )
        deadline = time.monotonic() + 20
        while time.monotonic() < deadline:
            if (self.sock / ".s.PGSQL.5432").exists():
                return
            if self.proc.poll() is not None:
                err = self.proc.stderr.read() if self.proc.stderr else b""
                raise AssertionError(err.decode("utf-8", "replace"))
            time.sleep(0.05)
        raise AssertionError("postgres did not open its socket")

    def stop(self) -> None:
        if self.proc.poll() is None:
            self.proc.send_signal(signal.SIGINT)
            self.proc.wait(timeout=15)


@pytest.mark.skipif(not INITDB.is_file(), reason="PostgreSQL 18 server is not installed")
def test_postgres_claim_is_once_and_does_not_publish(tmp_path: Path) -> None:
    cluster = _Cluster(tmp_path / "pg")
    try:
        pg = Pg(cluster.sock, "svn_test", "postgres", "")
        pg.execute(SCHEMA)
        clip = tmp_path / "clip.mp4"
        encoded = subprocess.run(
            [
                "ffmpeg",
                "-y",
                "-hide_banner",
                "-loglevel",
                "error",
                "-f",
                "lavfi",
                "-i",
                "testsrc=size=64x48:rate=10",
                "-t",
                "0.3",
                "-pix_fmt",
                "yuv420p",
                str(clip),
            ],
            capture_output=True,
            check=False,
        )
        if encoded.returncode != 0:
            raise AssertionError(encoded.stderr.decode("utf-8", "replace"))
        payload = clip.read_bytes()
        object_name = f"{11:064x}"
        digest = hashlib.sha256(payload).hexdigest()
        pg.execute(
            """
            INSERT INTO api.quotas (account_id, input_reserved, output_reserved)
            VALUES ('acct1', 9, 8);
            INSERT INTO api.uploads (upload_id, content_id, object_name, status)
            VALUES ('u1', 'cpg', :'object_name', 'PROCESSING_QUEUED');
            INSERT INTO api.processing_jobs (job_id, upload_id, status, created_at)
            VALUES ('jdb', 'u1', 'QUEUED', now());
            INSERT INTO api.creator_intents (
                upload_id, input_sha256, input_bytes, output_budget_bytes
            ) VALUES ('u1', :'digest', CAST(:'nbytes' AS bigint), 5000000);
            INSERT INTO api.chain_outbox (upload_id, status) VALUES ('u1', 'HELD');
            """,
            {"object_name": object_name, "digest": digest, "nbytes": str(len(payload))},
        )
        ws = Workspace(tmp_path / "work", pg=pg)
        (ws.root / "quarantine" / object_name).write_bytes(payload)
        assert pg.pull(ws) == 1
        assert pg.pull(ws) == 0
        done = ws.run_once()
        assert done is not None
        assert done["status"] == "READY_FOR_REVIEW"
        assert done["publication"] == "NOT_PUBLISHED"
        assert done["content_version"] == 1
        reserved = pg.execute("SELECT input_reserved || ',' || output_reserved FROM api.quotas")
        assert reserved.strip() == "9,8"
        assert pg.execute("SELECT status FROM api.uploads").strip() == "PROCESSING_QUEUED"
        assert pg.execute("SELECT status FROM api.processing_jobs").strip() == "QUEUED"
        assert pg.execute("SELECT status FROM api.chain_outbox").strip() == "HELD"
        assert pg.execute("SELECT state FROM media.attempts").strip() == "READY_FOR_REVIEW"
        assert pg.pull(ws) == 0
        with pytest.raises(Failure):
            pg.execute(
                """
                INSERT INTO media.packages (
                    content_id, content_version, job_id, manifest_sha256,
                    package_bytes, status, created_at
                ) VALUES (
                    'cpg', 9, 'jother', repeat('ab', 32), 1, 'PUBLISHED', now()
                );
                """
            )
        ready = json.loads((ws.root / "jobs" / "ready" / "jdb.json").read_text(encoding="utf-8"))
        assert ready["publication"] == "NOT_PUBLISHED"
    finally:
        cluster.stop()
