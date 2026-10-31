"""CI enforcement for the fixed secret-scanner corpus."""

from __future__ import annotations

import subprocess
import sys
from pathlib import Path

from scripts.scan_secrets import findings_in_text
from tests.secret_scanner_corpus import cases

ROOT = Path(__file__).resolve().parents[2]

# Frozen here, not derived from cases(). Deleting, renaming, or flipping
# an expected result fails this test.
PINNED_CORPUS_COUNT = 214
PINNED_CORPUS = (
    ("r2-json-mnemonic", True),
    ("r2-env-seed", True),
    ("r2-fifteen", True),
    ("r2-unquoted-colon", True),
    ("r3-equals-quoted", True),
    ("r3-env-upper", True),
    ("r3-one-per-line", True),
    ("r3-split-6-6", True),
    ("r4-blank-lines", True),
    ("r4-dash-list", True),
    ("r4-star-list", True),
    ("r4-dot-list", True),
    ("r4-yaml-list", True),
    ("r4-json-array", True),
    ("r4-table", True),
    ("r4-hash-comments", True),
    ("r4-upper", True),
    ("r4-title", True),
    ("r4-padded-25", True),
    ("r4-validator-prefix", True),
    ("r4-validator-env", False),
    ("r4-camel-json", True),
    ("r4-joined", True),
    ("r4-dotted", True),
    ("r4-private", True),
    ("r4-annotation-bytes", False),
    ("r5-toml", True),
    ("r5-ini", True),
    ("r5-yaml", True),
    ("r5-json", True),
    ("r5-dotenv", True),
    ("r5-prose", True),
    ("r5-24", True),
    ("r5-toml-account", True),
    ("r5-ini-name", True),
    ("r5-yaml-index", True),
    ("r5-json-index", True),
    ("r5-env-account", True),
    ("r5-phrase-again", True),
    ("r5-colon-list", True),
    ("r5-heading-list", True),
    ("r5-annotation-value", True),
    ("r5-yaml-str", False),
    ("r5-validator-camel", True),
    ("r5-node-private", True),
    ("r5-hex-suffix-env", True),
    ("r5-hex-suffix-colon", True),
    ("r5-flag", True),
    ("r5-ordinary-prose", False),
    ("r6-paren-list", True),
    ("r6-paren-num-list", True),
    ("r6-mnemonic-call", True),
    ("r6-from-mnemonic", True),
    ("r6-from-phrase", True),
    ("r6-newkey", True),
    ("r6-env-paren-comment", True),
    ("r6-fromhex", True),
    ("r6-signingkey", True),
    ("r6-byte-list", True),
    ("r6-getenv-hex", True),
    ("r6-buffer", True),
    ("r6-def-type", False),
    ("r6-dataclass", False),
    ("r6-docstring", False),
    ("r6-self", False),
    ("r6-kw", False),
    ("r6-subscript", False),
    ("r6-map-ident", False),
    ("r6-none", False),
    ("r6-ident", False),
    ("r6-ts-param", False),
    ("r6-ts-iface", False),
    ("r6-go-tag", False),
    ("r6-go-seed", False),
    ("r6-yaml-dollar", False),
    ("r6-optional-none", False),
    ("r6-flag-dollar", False),
    ("r6-load-call", False),
    ("r6-def-bare", False),
    ("r6-getenv-name", False),
    ("r7-word-label", True),
    ("r7-toml-paren", True),
    ("r7-flag-joined", True),
    ("r7-flag-underscore", True),
    ("r7-base64-name", True),
    ("r8-env-0x", True),
    ("r8-yaml-0x", True),
    ("r8-py-0x", True),
    ("r8-env-b64", True),
    ("r8-flag-0x", True),
    ("r8-go-backtick", True),
    ("r8-js-template", True),
    ("r8-yaml-unquoted-paren", True),
    ("r8-ini-semi-paren", True),
    ("r8-signing-key", False),
    ("r8-raw", False),
    ("r8-default-name", False),
    ("r8-string-annotation", False),
    ("r8-digest-fromhex", False),
    ("r8-iv-bytes", False),
    ("r8-want-decode", False),
    ("r8-named-fromhex", True),
    ("r8-openssh-vendor", True),
    ("r8-dist-js", True),
    ("r8-dist-py", True),
    ("r1-pkcs8", True),
    ("r1-pgp", True),
    ("r1-slack", True),
    ("r1-key-suffix", True),
    ("r2-quoted-name", True),
    ("r9-env-wif", True),
    ("r9-toml-wif", True),
    ("r9-py-wif", True),
    ("r9-hunter", True),
    ("r9-yaml-b64", True),
    ("r9-optional-arrow", False),
    ("r9-none-ellipsis", False),
    ("r9-union-none-arrow", False),
    ("r9-none-comma-param", False),
    ("r9-key-fromhex", True),
    ("r9-validator-key", True),
    ("r9-node-key", True),
    ("r9-go-key-decode", True),
    ("r9-key-bytes-list", True),
    ("r9-seed-next-line", False),
    ("r9-if-eq", False),
    ("r9-assert-eq", False),
    ("r9-ts-bang", False),
    ("r9-getenv-deadbeef", True),
    ("r9-private-key-hex", True),
    ("r9-private-key-bytes", True),
    ("r9-private-key-b64", True),
    ("r9-snake-key-bytes", True),
    ("r9-snake-key-b64", True),
    ("r9-camel-my-hex", True),
    ("r9-key2", False),
    ("r9-ed25519-key", False),
    ("r9-eq-str", False),
    ("r9-helm", False),
    ("r9-placeholder-yaml", False),
    ("r9-placeholder-md", False),
    ("r9-bytes-three", False),
    ("r9-getenv-short", False),
    ("r9-environ-get", True),
    ("r9-environ-get-sq", True),
    ("r9-getenv-multiline", True),
    ("r9-getenv-kw", True),
    ("r9-plus-eq", True),
    ("r9-plus-eq-ident", False),
    ("r9-subscript-assign", True),
    ("r9-environ-assign", True),
    ("r9-subscript-ident", False),
    ("r9-paren-literal", True),
    ("r9-placeholder-prose", False),
    ("r9-kw-literal", True),
    ("r10-go-byte-walrus", True),
    ("r10-go-0x-walrus", True),
    ("r10-go-hex-walrus", True),
    ("r10-py-walrus", True),
    ("r10-py-walrus-if", True),
    ("r10-env-hunter", True),
    ("r10-yaml-unquoted", True),
    ("r10-ini-unquoted", True),
    ("r10-getenv-0x", True),
    ("r10-getenv-wif", True),
    ("r10-unhexlify", True),
    ("r10-nullish", True),
    ("r10-or-bar", True),
    ("r10-defaced", False),
    ("r10-fat-arrow", False),
    ("r10-py-or", True),
    ("r11-cjs-env", False),
    ("r11-cjs-ident", False),
    ("r11-mjs-ident", False),
    ("r11-mts-ident", False),
    ("r11-cts-ident", False),
    ("r11-rs-ident", False),
    ("r11-sh-ident", False),
    ("r11-sh-quoted", True),
    ("r11-go-range-blank", False),
    ("r11-go-range", False),
    ("r11-go-addr", False),
    ("r11-go-addr-field", False),
    ("r11-go-chan", False),
    ("r11-go-deref", False),
    ("r11-go-deref-eq", False),
    ("r11-go-addr-eq", False),
    ("r11-go-tuple-err", True),
    ("r11-go-tuple-blank", True),
    ("r11-py-tuple", True),
    ("r11-env-empty", False),
    ("r11-env-empty-quotes", False),
    ("r11-env-empty-sq", False),
    ("r11-toml-empty-quotes", False),
    ("r11-env-comment", False),
    ("r11-yaml-empty", False),
    ("r11-yaml-empty-quotes", False),
    ("r11-yaml-nested", False),
    ("r11-ini-empty", False),
    ("r11-toml-empty", False),
    ("r11-json-empty", False),
    ("r11-docker-empty", False),
    ("r11-sh-dollar1", False),
    ("r11-sh-export-missing", False),
    ("r11-yaml-path", False),
    ("r11-yaml-alias", False),
    ("r11-yaml-false", False),
    ("r11-yaml-true", False),
    ("r11-yaml-yes", False),
    ("r11-yaml-no", False),
    ("r11-yaml-on", False),
    ("r11-yaml-off", False),
    ("r11-md-prose", False),
    ("r11-fullwidth", True),
)


def test_corpus_matches_the_scanner() -> None:
    table = cases()
    pinned = [(item["id"], item["expect"]) for item in table]
    assert len(pinned) == PINNED_CORPUS_COUNT
    assert tuple(pinned) == PINNED_CORPUS
    assert len({item[0] for item in pinned}) == len(pinned)
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
        for item in cases():
            if not item["cli"]:
                continue
            if item["expect"]:
                assert item["name"] in scanned.stdout, item["name"]
            else:
                assert item["name"] not in scanned.stdout, item["name"]
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
