"""The observe module is in the Go suite and the race shard."""

from __future__ import annotations

from pathlib import Path

from scripts.run_tests import GO_MODULES

ROOT = Path(__file__).resolve().parents[2]
CI = ROOT / ".github" / "workflows" / "ci.yml"


def test_observe_is_in_the_go_suite_and_the_race_shard() -> None:
    text = CI.read_text(encoding="utf-8")
    assert "services/observe" in GO_MODULES
    assert "go vet ./services/observe/..." in text
    assert "go test -race -count=1 -timeout 2m ./services/observe/..." in text
