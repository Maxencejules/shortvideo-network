"""An empty Node test file is not a passing suite."""

from __future__ import annotations

import subprocess
from pathlib import Path

from scripts.run_tests import real_node_subtests


def test_real_file_counts_the_test_name() -> None:
    path = Path("apps/web/test/react-pin.test.mjs")
    completed = subprocess.run(
        ["node", "--test", "--test-reporter", "tap", str(path)],
        check=False,
        text=True,
        capture_output=True,
    )
    assert completed.returncode == 0
    assert real_node_subtests(completed.stdout) == ["react version is the pinned release"]


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
