"""The upload service is in the Go suite and its race shard installs FFmpeg."""

from __future__ import annotations

from pathlib import Path

from scripts.run_tests import GO_MODULES

ROOT = Path(__file__).resolve().parents[2]
CI = ROOT / ".github" / "workflows" / "ci.yml"


def test_api_is_in_the_go_suite_and_the_race_shard() -> None:
    text = CI.read_text(encoding="utf-8")
    assert "services/api" in GO_MODULES
    assert "go vet ./services/api/..." in text
    assert "go test -race -count=1 -timeout 12m ./services/api/..." in text
    assert '"api"' in text
    assert "ffmpeg=7:6.1.1-3ubuntu5" in text
    assert "matrix.shard == 'api'" in text
