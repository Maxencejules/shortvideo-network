"""The indexer race shards exist and the go job vets the module.

Goal 12 adds services/indexer. Its tests start PostgreSQL, and
TestDevnetIndexer is a chain/devnet test. This guard checks the real
workflow text for those jobs. It does not re-parse every shell form; the
devnet coverage test checks that TestDevnetIndexer is run by exactly one
shard.
"""

from __future__ import annotations

from pathlib import Path

from scripts.run_tests import GO_MODULES

ROOT = Path(__file__).resolve().parents[2]
CI = ROOT / ".github" / "workflows" / "ci.yml"


def test_indexer_is_in_the_go_suite_and_the_race_shards() -> None:
    text = CI.read_text(encoding="utf-8")
    assert "services/indexer" in GO_MODULES
    assert "go vet ./services/indexer/..." in text
    assert "go vet -tags svn_faultpoint ./services/indexer/..." in text
    fault = (
        "go test -count=1 -timeout 2m -tags svn_faultpoint ./services/indexer/internal/faultpoint/"
    )
    assert fault in text
    assert "go test -race -count=1 -timeout 12m ./services/indexer/..." in text
    assert "go test -race -count=1 -timeout 12m -run TestDevnetIndexer ./chain/devnet" in text
    assert '"indexer"' in text
    assert '"devnet-indexer"' in text
    assert "-run TestDevnetIndexer ./chain/devnet" in text
    assert "TestDevnetIndexer" in text
