"""A queued encode stops when the content is delisted. It does not start ffmpeg."""

from __future__ import annotations

from pathlib import Path

from svn_media import Failure, Workspace


def test_delist_cancels_a_queued_job_before_encode(tmp_path: Path) -> None:
    workspace = Workspace(tmp_path)
    content_id = "cdelist"
    workspace.enqueue(
        job_id="jdelist",
        content_id=content_id,
        object_name="ab" * 32,
        output_budget_bytes=1000,
        content_version=1,
    )
    workspace.delist(content_id)
    try:
        workspace.enqueue(
            job_id="jagain",
            content_id=content_id,
            object_name="cd" * 32,
            output_budget_bytes=1000,
            content_version=1,
        )
    except Failure as exc:
        assert exc.code == "REFUSED"
    else:
        raise AssertionError("a delisted id was queued")
    done = workspace.run_once()
    assert done is not None
    assert done["status"] == "CANCELLED"
    assert done["encoded"] is False
    assert done["waited_for_finality"] is False
    assert not (tmp_path / "packages" / content_id).exists()
