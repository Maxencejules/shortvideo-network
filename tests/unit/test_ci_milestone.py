"""The milestone scenario is a job, and the go suite still builds the module."""

from __future__ import annotations

from pathlib import Path

from scripts.run_tests import GO_MODULES

ROOT = Path(__file__).resolve().parents[2]
CI = ROOT / ".github" / "workflows" / "ci.yml"


def test_milestone_module_is_in_the_go_suite() -> None:
    text = CI.read_text(encoding="utf-8")
    assert "services/milestone" in GO_MODULES
    assert "go vet ./services/milestone/..." in text
    assert "go test -count=1 -timeout 15m -run TestMilestone ./services/milestone/" in text
    assert "SVN_MILESTONE_PATH:" in text
