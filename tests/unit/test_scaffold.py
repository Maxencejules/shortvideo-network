"""Scaffold modules must refuse to act like finished services."""

from __future__ import annotations

import pytest
import svn_api
import svn_media


def test_api_refuses_to_serve() -> None:
    assert svn_api.SCAFFOLD is True
    with pytest.raises(RuntimeError, match="does not serve traffic"):
        svn_api.serve()


def test_media_worker_is_a_package_and_does_not_start_on_import() -> None:
    assert svn_media.SCAFFOLD is False
    assert svn_media.Workspace is not None
