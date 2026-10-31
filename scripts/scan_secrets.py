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
    re.compile("['\"]" + "priv" + r"[-_]" + "key" + "['\"]", re.IGNORECASE),
    re.compile(
        r"(?<![A-Za-z0-9_])" + "priv" + r"[-_]" + "key" + r"\s*[:=]",
        re.IGNORECASE,
    ),
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
_PHRASE_LINE = re.compile(r"^[a-z]+(?:[ \t]+[a-z]+)*$")
_PREFIXED_PHRASE = re.compile(
    r"^[A-Za-z_][A-Za-z0-9_]*\s*[=:]\s*['\"]?([a-z]+(?:[ \t]+[a-z]+)*)['\"]?\s*$"
)


def _tokens(line: str) -> list[str]:
    return _TOKEN.findall(line.lower())


def _window_hit(tokens: list[str], words: set[str]) -> bool:
    for length in MNEMONIC_LENGTHS:
        if len(tokens) < length:
            continue
        for start in range(len(tokens) - length + 1):
            window = tokens[start : start + length]
            if all(token in words for token in window):
                return True
    return False


def _phrase_tokens(line: str, words: set[str]) -> list[str] | None:
    """Words from a phrase line, or from a single KEY=phrase / KEY: phrase line."""
    stripped = line.strip()
    prefixed = _PREFIXED_PHRASE.fullmatch(stripped)
    if prefixed:
        tokens = prefixed.group(1).split()
    elif _PHRASE_LINE.fullmatch(stripped):
        tokens = stripped.split()
    else:
        return None
    if not tokens or any(token not in words for token in tokens):
        return None
    return tokens


def mnemonic_hit(text: str, words: set[str]) -> bool:
    """BIP39 phrase on one line, one word per line, or split across phrase lines.

    A finished block of 12 to 24 phrase-line words counts. Code punctuation
    breaks a block. The 2048-line wordlist is one longer block, so it does not.
    """
    lines = text.splitlines()
    for line in lines:
        if _window_hit(_tokens(line), words):
            return True
    block: list[str] = []
    for line in [*lines, ""]:
        tokens = _phrase_tokens(line, words)
        if tokens:
            block.extend(tokens)
            continue
        if 12 <= len(block) <= 24:
            return True
        block = []
    return False


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
    if mnemonic_hit(text, words if words is not None else bip39_words()):
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
