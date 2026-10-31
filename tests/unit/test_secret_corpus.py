"""CI enforcement for the fixed secret-scanner corpus."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

from scripts.scan_secrets import findings_in_text
from tests.secret_scanner_corpus import cases

ROOT = Path(__file__).resolve().parents[2]

# Frozen here, not derived from cases(). Deleting or renaming an entry fails this test.
PINNED_CORPUS_COUNT = 155
PINNED_CORPUS_IDS = (
    "r2-json-mnemonic",
    "r2-env-seed",
    "r2-fifteen",
    "r2-unquoted-colon",
    "r3-equals-quoted",
    "r3-env-upper",
    "r3-one-per-line",
    "r3-split-6-6",
    "r4-blank-lines",
    "r4-dash-list",
    "r4-star-list",
    "r4-dot-list",
    "r4-yaml-list",
    "r4-json-array",
    "r4-table",
    "r4-hash-comments",
    "r4-upper",
    "r4-title",
    "r4-padded-25",
    "r4-validator-prefix",
    "r4-validator-env",
    "r4-camel-json",
    "r4-joined",
    "r4-dotted",
    "r4-private",
    "r4-annotation-bytes",
    "r5-toml",
    "r5-ini",
    "r5-yaml",
    "r5-json",
    "r5-dotenv",
    "r5-prose",
    "r5-24",
    "r5-toml-account",
    "r5-ini-name",
    "r5-yaml-index",
    "r5-json-index",
    "r5-env-account",
    "r5-phrase-again",
    "r5-colon-list",
    "r5-heading-list",
    "r5-annotation-value",
    "r5-yaml-str",
    "r5-validator-camel",
    "r5-node-private",
    "r5-hex-suffix-env",
    "r5-hex-suffix-colon",
    "r5-flag",
    "r5-ordinary-prose",
    "r6-paren-list",
    "r6-paren-num-list",
    "r6-mnemonic-call",
    "r6-from-mnemonic",
    "r6-from-phrase",
    "r6-newkey",
    "r6-env-paren-comment",
    "r6-fromhex",
    "r6-signingkey",
    "r6-byte-list",
    "r6-getenv-hex",
    "r6-buffer",
    "r6-def-type",
    "r6-dataclass",
    "r6-docstring",
    "r6-self",
    "r6-kw",
    "r6-subscript",
    "r6-map-ident",
    "r6-none",
    "r6-ident",
    "r6-ts-param",
    "r6-ts-iface",
    "r6-go-tag",
    "r6-go-seed",
    "r6-yaml-dollar",
    "r6-optional-none",
    "r6-flag-dollar",
    "r6-load-call",
    "r6-def-bare",
    "r6-getenv-name",
    "r7-word-label",
    "r7-toml-paren",
    "r7-flag-joined",
    "r7-flag-underscore",
    "r7-base64-name",
    "r8-env-0x",
    "r8-yaml-0x",
    "r8-py-0x",
    "r8-env-b64",
    "r8-flag-0x",
    "r8-go-backtick",
    "r8-js-template",
    "r8-yaml-unquoted-paren",
    "r8-ini-semi-paren",
    "r8-signing-key",
    "r8-raw",
    "r8-default-name",
    "r8-string-annotation",
    "r8-digest-fromhex",
    "r8-iv-bytes",
    "r8-want-decode",
    "r8-named-fromhex",
    "r8-openssh-vendor",
    "r8-dist-js",
    "r8-dist-py",
    "r1-pkcs8",
    "r1-pgp",
    "r1-slack",
    "r1-key-suffix",
    "r2-quoted-name",
    "r9-env-wif",
    "r9-toml-wif",
    "r9-py-wif",
    "r9-hunter",
    "r9-yaml-b64",
    "r9-optional-arrow",
    "r9-none-ellipsis",
    "r9-union-none-arrow",
    "r9-none-comma-param",
    "r9-key-fromhex",
    "r9-validator-key",
    "r9-node-key",
    "r9-go-key-decode",
    "r9-key-bytes-list",
    "r9-seed-next-line",
    "r9-if-eq",
    "r9-assert-eq",
    "r9-ts-bang",
    "r9-getenv-deadbeef",
    "r9-private-key-hex",
    "r9-private-key-bytes",
    "r9-private-key-b64",
    "r9-snake-key-bytes",
    "r9-snake-key-b64",
    "r9-camel-my-hex",
    "r9-key2",
    "r9-ed25519-key",
    "r9-eq-str",
    "r9-helm",
    "r9-placeholder-yaml",
    "r9-placeholder-md",
    "r9-bytes-three",
    "r9-getenv-short",
    "r9-environ-get",
    "r9-environ-get-sq",
    "r9-getenv-multiline",
    "r9-getenv-kw",
    "r9-plus-eq",
    "r9-plus-eq-ident",
    "r9-subscript-assign",
    "r9-environ-assign",
    "r9-subscript-ident",
    "r9-paren-literal",
    "r9-placeholder-prose",
    "r9-kw-literal",
)


def test_corpus_matches_the_scanner() -> None:
    table = cases()
    ids = [item["id"] for item in table]
    assert len(ids) == PINNED_CORPUS_COUNT
    assert tuple(ids) == PINNED_CORPUS_IDS
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
