"""CI enforcement for the fixed secret-scanner corpus."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

from scripts.scan_secrets import findings_in_text
from tests.secret_scanner_corpus import cases

ROOT = Path(__file__).resolve().parents[2]


def test_corpus_matches_the_scanner() -> None:
    table = cases()
    assert table
    ids = [item["id"] for item in table]
    assert len(ids) == len(set(ids))
    mismatches: list[str] = []
    for item in table:
        flagged = bool(findings_in_text(item["name"], item["text"]))
        if flagged != item["expect"]:
            mismatches.append(f"{item['id']}: got {flagged} expected {item['expect']}")
    assert mismatches == []


def test_corpus_cli_paths_are_reported() -> None:
    staged: list[str] = []
    try:
        for item in cases():
            if not item["cli"]:
                continue
            path = ROOT / item["name"]
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(item["text"], encoding="utf-8")
            staged.append(item["name"])
        added = subprocess.run(
            ["git", "add", "--", *staged],
            cwd=ROOT,
            check=False,
            capture_output=True,
            text=True,
        )
        assert added.returncode == 0, added.stderr
        scanned = subprocess.run(
            [sys.executable, "scripts/scan_secrets.py"],
            cwd=ROOT,
            check=False,
            capture_output=True,
            text=True,
        )
        assert scanned.returncode != 0, scanned.stdout
        for name in staged:
            assert name in scanned.stdout, name
    finally:
        if staged:
            subprocess.run(
                ["git", "reset", "-q", "HEAD", "--", *staged],
                cwd=ROOT,
                check=False,
            )
        for name in staged:
            path = ROOT / name
            path.unlink(missing_ok=True)
            parent = path.parent
            while parent != ROOT:
                try:
                    parent.rmdir()
                except OSError:
                    break
                parent = parent.parent


def test_non_secret_names_are_documented() -> None:
    from scripts.scan_secrets import _NON_SECRET_NAMES

    text = (ROOT / "docs/evidence/goal-03.md").read_text(encoding="utf-8")
    assert "_NON_SECRET_NAMES" in text
    for name in ("raw", "signing_key", "priv_key_default"):
        assert name in _NON_SECRET_NAMES
        assert name in text
