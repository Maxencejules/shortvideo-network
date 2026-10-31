"""Scaffold modules must refuse to act like finished services."""

from __future__ import annotations

import pytest
import svn_api
import svn_broadcaster
import svn_indexer
import svn_media


def test_api_refuses_to_serve() -> None:
    assert svn_api.SCAFFOLD is True
    with pytest.raises(RuntimeError, match="does not serve traffic"):
        svn_api.serve()


def test_indexer_refuses_to_serve() -> None:
    assert svn_indexer.SCAFFOLD is True
    with pytest.raises(RuntimeError, match="does not read a chain"):
        svn_indexer.serve()


def test_broadcaster_refuses_to_serve() -> None:
    assert svn_broadcaster.SCAFFOLD is True
    with pytest.raises(RuntimeError, match="does not submit"):
        svn_broadcaster.serve()


def test_media_worker_does_not_read_the_source() -> None:
    assert svn_media.SCAFFOLD is True
    with pytest.raises(RuntimeError, match="does not run FFmpeg"):
        svn_media.transcode("not-a-path; rm -rf /")
