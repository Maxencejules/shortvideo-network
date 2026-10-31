#!/usr/bin/env python3
"""Scan tracked files for private-key material and credential shapes.

Best-effort guard, not a guarantee. It misses values that are not literals,
mnemonics broken by a non-BIP39 word, and formats this file does not name.
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
_KEY_BODY = "priv" + r"(?:ate)?" + r"[._-]?" + "key" + r"(?:_hex)?"
_KEY_ASSIGN = re.compile(
    r"(?<![A-Za-z0-9])['\"]?(?:[A-Za-z0-9]+_)?"
    + _KEY_BODY
    + r"(?![A-Za-z0-9])['\"]?\s*(?P<sep>[:=])",
    re.IGNORECASE,
)
_KEY_CAMEL = re.compile(
    r"(?<=[a-z])(?:Priv(?:ate)?Key(?:Hex)?)" + r"(?![A-Za-z0-9])['\"]?\s*(?P<sep>[:=])"
)
_KEY_QUOTED = re.compile(
    "['\"]" + r"(?:[A-Za-z0-9]+_)?" + "priv" + r"[._-]?" + "key" + "['\"]",
    re.IGNORECASE,
)
_KEY_CLI = re.compile(r"(?i)--" + "priv" + r"(?:ate)?-" + "key" + r"(?:=|\s+)(?P<value>[^\s$]\S*)")
_TYPE_AFTER = re.compile(
    r"(?ix)^\s*(?:"
    r"(?:bytes|str|int|float|bool|object|any|none|path|bytearray)"
    r"(?:\s*\|\s*(?:bytes|str|int|float|bool|object|any|none|path|bytearray))*"
    r"\b"
    r"|(?:optional|union|list|dict|tuple|sequence|mapping)\s*\[[^\]]*\]"
    r")"
)
_CALL_OR_ENV = re.compile(r"(?i)^(?:os\.)?(?:environ|getenv)\b|^[A-Za-z_][\w.]*\s*\(")
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
    """A valid length, or a valid window inside a longer run, including 13 and 14."""
    if count < 12:
        return False
    return any(length <= count for length in MNEMONIC_LENGTHS)


def _is_python(name: str) -> bool:
    return Path(name).suffix.lower() == ".py"


def _literal_secret(rest: str, sep: str, python: bool) -> bool:
    """True when the text after the key is a literal, not code or an env lookup.

    A bare Python annotation (`name: str`) has no equals sign. The same spelling
    in YAML is a value. `name: str = "..."` is an assignment and is checked
    after the equals sign.
    """
    line = rest.split("\n", 1)[0]
    stripped = line.strip()
    if python and sep == ":" and "=" not in stripped:
        if stripped == "" or stripped.startswith((")", ",", ":")) or _TYPE_AFTER.match(rest):
            return False
    if python and sep == ":" and "=" in stripped and _TYPE_AFTER.match(rest):
        return _literal_secret(stripped.split("=", 1)[1], "=", False)
    if stripped == "":
        return sep == "="
    if _CALL_OR_ENV.match(stripped):
        return False
    return True


def _quoted_env_argument(text: str, start: int) -> bool:
    before = text[max(0, start - 48) : start]
    return re.search(r"(?i)(?:environ\s*\[\s*|getenv\s*\(\s*)$", before) is not None


def _key_hit(text: str, name: str) -> bool:
    """Literal assignment, quoted short name, or CLI flag. Not a code use."""
    for match in _KEY_QUOTED.finditer(text):
        if _quoted_env_argument(text, match.start()):
            continue
        return True
    if _KEY_CLI.search(text):
        return True
    python = _is_python(name)
    for pattern in (_KEY_ASSIGN, _KEY_CAMEL):
        for match in pattern.finditer(text):
            if _literal_secret(text[match.end() :], match.group("sep"), python):
                return True
    return False


def _is_wordlist(name: str) -> bool:
    path = name.replace("\\", "/")
    return path.endswith("/" + _WORDLIST_NAME) or path == _WORDLIST_NAME


_HEADING = re.compile(r"^\s*#{2,6}\s+\S")
_SECTION = re.compile(r"^\s*\[[^\]]+\]\s*$")
_KEY_BOUNDARY = re.compile(r"""["']?(?P<key>[A-Za-z_][\w.-]*)["']?\s*[:=]""")


def _add_words(fragment: str, block: list[str], words: set[str]) -> bool:
    """Append BIP39 words. A non-BIP39 word ends the run. Blank text does not."""
    for token in _TOKEN.findall(fragment.lower()):
        if token in words:
            block.append(token)
            continue
        if _block_hit(len(block)):
            return True
        block.clear()
    return False


def mnemonic_hit(text: str, words: set[str]) -> bool:
    """12, 15, 18, 21, or 24 BIP39 words, including a window inside a longer run.

    Lines are lowercased. List, comment, quote, bracket, and table markers are
    not letters, so they fall away. A blank line does not reset the run. A word
    that is not on the BIP39 list does. A key or field name before `:` or `=`
    resets the run so the name is not glued to the value. A numeric `1: word`
    item is a list entry, not a key. A markdown heading of two or more hashes
    resets the run and is not itself a phrase. `scripts/bip39_english.txt` is
    excluded by path in `findings_in_text`.
    """
    block: list[str] = []
    for line in text.splitlines():
        if _HEADING.match(line) or _SECTION.match(line) or "(" in line or ")" in line:
            # Headings and section labels are boundaries. A parenthesis is code,
            # not a phrase, so its identifiers do not extend a mnemonic run.
            if _block_hit(len(block)):
                return True
            block = []
            continue
        if "://" in line:
            line = re.sub(r"://", " ", line)
        cursor = 0
        for match in _KEY_BOUNDARY.finditer(line):
            if _add_words(line[cursor : match.start()], block, words):
                return True
            if _block_hit(len(block)):
                return True
            block = []
            cursor = match.end()
        if _add_words(line[cursor:], block, words):
            return True
        if _TOKEN.search(line.lower()) is None:
            continue
    return _block_hit(len(block))


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
    if _key_hit(text, name):
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
