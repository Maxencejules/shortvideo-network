"""An empty Node test file is not a passing suite."""

from __future__ import annotations

import subprocess
from pathlib import Path

from scripts.run_tests import real_node_subtests


def test_named_subtest_counts() -> None:
    tap = (
        "# Subtest: react version is the pinned release\n"
        "ok 1 - react version is the pinned release\n"
    )
    assert real_node_subtests(tap) == ["react version is the pinned release"]


def test_file_placeholder_does_not_count() -> None:
    tap = (
        "# Subtest: /workspace/apps/web/test/react-pin.test.mjs\n"
        "ok 1 - /workspace/apps/web/test/react-pin.test.mjs\n"
        "# tests 1\n"
        "# pass 1\n"
    )
    assert real_node_subtests(tap) == []


def test_empty_file_has_zero_real_tests(tmp_path: Path) -> None:
    path = tmp_path / "empty.test.mjs"
    path.write_text("", encoding="utf-8")
    completed = subprocess.run(
        ["node", "--test", "--test-reporter", "tap", str(path)],
        check=False,
        text=True,
        capture_output=True,
    )
    assert completed.returncode == 0
    assert real_node_subtests(completed.stdout) == []
