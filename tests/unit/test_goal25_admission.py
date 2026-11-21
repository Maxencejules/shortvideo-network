"""Processing intake consults svn-observe before it encodes."""

from __future__ import annotations

import os
import subprocess
from pathlib import Path

import pytest
from svn_media.admit import processing
from svn_media.queue import Workspace


def _enqueue(workspace: Workspace, job_id: str) -> None:
    workspace.enqueue(
        job_id=job_id,
        content_id="c" + job_id[1:],
        object_name="ab" * 32,
        output_budget_bytes=10,
        content_version=1,
    )


def _observe(tmp: Path, *, limit: str, queue: str) -> subprocess.Popen[str]:
    env = os.environ.copy()
    env["SVN_OBSERVE_TOKEN"] = "synthetic-observe-25"
    env["SVN_OBSERVE_LISTEN"] = "127.0.0.1:0"
    env["SVN_OBSERVE_LIMIT_processing_concurrency"] = limit
    env["SVN_OBSERVE_QUEUE_processing_concurrency"] = queue
    root = Path(__file__).resolve().parents[2]
    proc = subprocess.Popen(
        ["go", "run", "./services/observe/cmd/svn-observe"],
        cwd=root,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    assert proc.stdout is not None
    line = proc.stdout.readline()
    if not line.startswith("LISTEN "):
        err = ""
        if proc.stderr is not None:
            err = proc.stderr.read()
        raise AssertionError(line + err)
    os.environ["SVN_OBSERVE_URL"] = "http://" + line.split(None, 1)[1].strip()
    os.environ["SVN_OBSERVE_TOKEN"] = "synthetic-observe-25"
    return proc


def _restore_observe(previous_url: str | None, previous_token: str | None) -> None:
    if previous_url is None:
        os.environ.pop("SVN_OBSERVE_URL", None)
    else:
        os.environ["SVN_OBSERVE_URL"] = previous_url
    if previous_token is None:
        os.environ.pop("SVN_OBSERVE_TOKEN", None)
    else:
        os.environ["SVN_OBSERVE_TOKEN"] = previous_token


def test_processing_cap_refuses_the_next_real_job(tmp_path: Path) -> None:
    previous_url = os.environ.get("SVN_OBSERVE_URL")
    previous_token = os.environ.get("SVN_OBSERVE_TOKEN")
    proc = _observe(tmp_path, limit="1", queue="0")
    try:
        assert processing("jheld") == "granted"
        assert processing("jheld") == "granted"
        workspace = Workspace(tmp_path)
        _enqueue(workspace, "jnext01")
        done = workspace.run_once()
        assert done is not None
        assert done["encoded"] is False
        assert done["status"] == "refused"
        queued = tmp_path / "jobs" / "queued" / "jnext01.json"
        assert queued.is_file()
        package = tmp_path / "packages" / "cnext01"
        assert not package.exists()
    finally:
        proc.kill()
        proc.wait(timeout=5)
        _restore_observe(previous_url, previous_token)


def test_processing_stops_when_observe_is_down(
    tmp_path: Path, monkeypatch: pytest.MonkeyPatch
) -> None:
    monkeypatch.delenv("SVN_OBSERVE_URL", raising=False)
    monkeypatch.delenv("SVN_OBSERVE_TOKEN", raising=False)
    workspace = Workspace(tmp_path)
    _enqueue(workspace, "jdown01")
    done = workspace.run_once()
    assert done is not None
    assert done["status"] == "unreachable"
    assert done["encoded"] is False
    assert (tmp_path / "jobs" / "queued" / "jdown01.json").is_file()
