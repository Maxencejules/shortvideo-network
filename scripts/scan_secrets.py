#!/usr/bin/env python3
"""Scan tracked files for private-key material and credential shapes.

This is not a complete secret scanner. It fails closed on the patterns below.
"""

from __future__ import annotations

import re
import subprocess
import sys
from pathlib import Path

ROOT = Path(__file__).resolve().parents[1]
_BEGIN = "-----BEGIN "
PATTERNS = (
    re.compile(r"-----BEGIN (?:OPENSSH |RSA |EC |DSA )?PRIVATE KEY-----"),
    re.compile(re.escape(_BEGIN + "ENCRYPTED PRIVATE KEY-----")),
    re.compile(re.escape(_BEGIN + "PGP PRIVATE KEY BLOCK-----")),
    re.compile(r"\bAKIA[0-9A-Z]{16}\b"),
    re.compile(r"\bghp_[A-Za-z0-9]{20,}\b"),
    re.compile(r"\bgithub_pat_[A-Za-z0-9_]{20,}\b"),
    re.compile(r"\bxox[baprs]-[0-9A-Za-z-]{10,}\b"),
)
# Names are built from pieces so this file does not contain a contiguous marker.
_KEY_BODY = "priv" + r"(?:ate)?" + r"[._-]?" + "key"
_KEY_ASSIGN = re.compile(
    r"(?<![A-Za-z0-9])['\"]?(?:[A-Za-z0-9]+_)?"
    + _KEY_BODY
    + r"(?![A-Za-z0-9])['\"]?\s*(?P<sep>[:=])",
    re.IGNORECASE,
)
_KEY_QUOTED = re.compile(
    "['\"]" + r"(?:[A-Za-z0-9]+_)?" + "priv" + r"[._-]?" + "key" + "['\"]",
    re.IGNORECASE,
)
_TYPE_AFTER = re.compile(
    r"(?ix)^\s*(?:"
    r"(?:bytes|str|int|float|bool|object|any|none|path|bytearray)"
    r"(?:\s*\|\s*(?:bytes|str|int|float|bool|object|any|none|path|bytearray))*"
    r"\b"
    r"|(?:optional|union|list|dict|tuple|sequence|mapping)\s*\[[^\]]*\]"
    r")"
)
FORBIDDEN_NAMES = {
    "priv_validator_key.json",
    "node_key.json",
    "id_rsa",
    "id_ed25519",
    "seed.json",
    "seeds.json",
}
FORBIDDEN_SUFFIXES = {".pem", ".p12", ".pfx", ".keystore", ".key"}
DOCUMENT_SUFFIXES = {".pdf", ".png", ".jpg", ".jpeg", ".gif", ".webp"}
WORDLIST = Path(__file__).resolve().parent / "bip39_english.txt"


def bip39_words() -> set[str]:
    """English BIP39 list. One word per line. Source: bitcoin/bips bip-0039/english.txt."""
    return {
        line.strip() for line in WORDLIST.read_text(encoding="utf-8").splitlines() if line.strip()
    }


MNEMONIC_LENGTHS = (12, 15, 18, 21, 24)
_TOKEN = re.compile(r"[a-z]+")
_WORDLIST_NAME = "bip39_english.txt"


def _block_hit(count: int) -> bool:
    """Valid lengths, or a valid window inside a longer block. 13 and 14 are not."""
    if count in (13, 14) or count < 12:
        return False
    if count in MNEMONIC_LENGTHS:
        return True
    return any(length <= count for length in MNEMONIC_LENGTHS)


def _key_hit(text: str) -> bool:
    """Assignment or quoted key name. A Python type annotation is not an assignment."""
    if _KEY_QUOTED.search(text):
        return True
    for match in _KEY_ASSIGN.finditer(text):
        if match.group("sep") == ":" and _TYPE_AFTER.match(text[match.end() :]):
            continue
        return True
    return False


def _is_wordlist(name: str) -> bool:
    path = name.replace("\\", "/")
    return path.endswith("/" + _WORDLIST_NAME) or path == _WORDLIST_NAME


def mnemonic_hit(text: str, words: set[str]) -> bool:
    """12, 15, 18, 21, or 24 BIP39 words, including a window inside a longer block.

    Lines are lowercased. List, comment, quote, bracket, and table markers are
    not letters, so they fall away. A blank line does not reset the block. A
    word that is not on the BIP39 list does. `:` and `=` start a new block so a
    key is not glued to its value. A block of 13 or 14 words is not a mnemonic.
    `scripts/bip39_english.txt` is excluded by path in `findings_in_text`.
    """
    block: list[str] = []

    def finish() -> bool:
        return _block_hit(len(block))

    for line in text.splitlines():
        lowered = line.lower()
        if _TOKEN.search(lowered) is None:
            continue
        parts = re.split(r"[:=]", lowered)
        for index, part in enumerate(parts):
            if index and finish():
                return True
            if index:
                block = []
            for token in _TOKEN.findall(part):
                if token in words:
                    block.append(token)
                    continue
                if finish():
                    return True
                block = []
    return finish()


def tracked_files() -> list[Path]:
    completed = subprocess.run(
        ["git", "ls-files", "-z"],
        cwd=ROOT,
        check=True,
        capture_output=True,
    )
    paths: list[Path] = []
    for raw in completed.stdout.split(b"\0"):
        if raw:
            paths.append(ROOT / raw.decode("utf-8"))
    return paths


def findings_in_text(name: str, text: str, words: set[str] | None = None) -> list[str]:
    found: list[str] = []
    base = Path(name).name
    if base in FORBIDDEN_NAMES or Path(name).suffix.lower() in FORBIDDEN_SUFFIXES:
        found.append(f"{name}: forbidden filename")
    for pattern in PATTERNS:
        if pattern.search(text):
            found.append(f"{name}: {pattern.pattern}")
    if _key_hit(text):
        found.append(f"{name}: private key name")
    if not _is_wordlist(name) and mnemonic_hit(text, words if words is not None else bip39_words()):
        found.append(f"{name}: bip39 mnemonic")
    return found


def scan_repo() -> list[str]:
    found: list[str] = []
    words = bip39_words()
    for path in tracked_files():
        if not path.is_file():
            continue
        data = path.read_bytes()
        relative = path.relative_to(ROOT).as_posix()
        if b"\0" in data:
            if path.suffix.lower() not in DOCUMENT_SUFFIXES:
                found.append(f"{relative}: tracked binary")
            continue
        text = data.decode("utf-8", errors="replace")
        found.extend(findings_in_text(relative, text, words))
    return found


def main() -> int:
    found = scan_repo()
    if found:
        for item in found:
            print(item)
        return 1
    print("secret scan ok")
    return 0


if __name__ == "__main__":
    sys.exit(main())
