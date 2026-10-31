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
_KEY_BODY = "priv" + r"(?:ate)?" + r"[._-]?" + "key" + r"(?:_hex|_base64)?"
_KEY_ASSIGN = re.compile(
    r"(?<![A-Za-z0-9])['\"]?(?:[A-Za-z0-9]+_)?"
    + _KEY_BODY
    + r"(?![A-Za-z0-9])['\"]?\s*(?P<sep>[:=])",
    re.IGNORECASE,
)
_KEY_CAMEL = re.compile(
    r"(?<=[a-z])(?:Priv(?:ate)?Key(?:Hex|Base64)?)" + r"(?![A-Za-z0-9])['\"]?\s*(?P<sep>[:=])"
)
_KEY_QUOTED = re.compile(
    "['\"]" + r"(?:[A-Za-z0-9]+_)?" + "priv" + r"[._-]?" + "key" + "['\"]",
    re.IGNORECASE,
)
_KEY_CLI = re.compile(
    r"(?i)(?<![A-Za-z0-9])--" + "priv" + r"(?:ate)?[-_]?" + "key" + r"(?:=|\s+)(?P<value>\S+)"
)
_TYPE_AFTER = re.compile(
    r"(?ix)^\s*(?:"
    r"(?:bytes|str|int|float|bool|object|any|none|path|bytearray)"
    r"(?:\s*\|\s*(?:bytes|str|int|float|bool|object|any|none|path|bytearray))*"
    r"\b"
    r"|(?:optional|union|list|dict|tuple|sequence|mapping)\s*\[[^\]]*\]"
    r")"
)
_QUOTED = re.compile(r"\"(?:\\.|[^\"\\])*\"|'(?:\\.|[^'\\])*'")
_DOLLAR = re.compile(r"\$\{?[A-Za-z_][\w]*\}?")
_BYTE_LIST = re.compile(r"bytes\s*\(\s*\[(?:\s*\d+\s*,){3,}\s*\d+")
_HEX_CALL = re.compile(
    r"(?i)(?:fromhex|DecodeString|hexToBytes|Buffer\.from)\s*\(\s*[\"'][0-9a-f]{48,}"
)
_GETENV_HEX = re.compile(r"(?i)(?:getenv|environ)\s*[\[(][^)\n]*,\s*[\"'][0-9a-f]{48,}[\"']")
_CODE_SUFFIXES = {".py", ".pyi", ".ts", ".tsx", ".js", ".jsx", ".go"}
_NON_SECRET_NAMES = {
    "k",
    "key",
    "priv_key",
    "privkey",
    "private_key",
    "seed",
    "path",
    "msg",
    "none",
    "null",
    "nil",
    "undefined",
    "true",
    "false",
}
EXCLUDED_DIR_NAMES = frozenset(
    {"node_modules", ".venv", "site-packages", "vendor", "__pycache__", "dist"}
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
    """A valid length, or a valid window inside a longer run, including 13 and 14."""
    if count < 12:
        return False
    return any(length <= count for length in MNEMONIC_LENGTHS)


def _is_code(name: str) -> bool:
    return Path(name).suffix.lower() in _CODE_SUFFIXES


def _quote_inners(value: str) -> list[str]:
    return [match.group(0)[1:-1] for match in _QUOTED.finditer(value)]


def _is_dollar_ref(value: str) -> bool:
    text = value.strip()
    if len(text) >= 2 and text[0] == text[-1] and text[0] in {"'", '"'}:
        text = text[1:-1]
    return _DOLLAR.fullmatch(text) is not None


def _hex_or_base64(inner: str) -> bool:
    if re.fullmatch(r"[0-9a-fA-F]{16,}", inner):
        return True
    return len(inner) >= 24 and re.fullmatch(r"[A-Za-z0-9+/=]+", inner) is not None


def _has_byte_list(value: str) -> bool:
    return _BYTE_LIST.search(value) is not None


def _value_is_secret(value: str) -> bool:
    """True for a literal secret. Identifiers, None, attributes, and $ refs are not."""
    text = value.strip().rstrip(",;")
    text = re.sub(r"\s+#.*$", "", text).strip()
    text = text.rstrip(")}]")
    text = text.strip()
    if text == "" or _is_dollar_ref(text):
        return False
    if re.fullmatch(r"(?i)none|null|nil|undefined", text):
        return False
    if _has_byte_list(text):
        return True
    inners = _quote_inners(text)
    if "(" in text or "[" in text:
        return any(_hex_or_base64(inner) for inner in inners if not _is_dollar_ref(inner))
    if inners:
        return any(inner.strip() != "" and not _is_dollar_ref(inner) for inner in inners)
    if re.fullmatch(r"[0-9a-fA-F]{8,}", text):
        return True
    if re.fullmatch(r"[A-Za-z_][\w.]*", text):
        if "." in text:
            return False
        if text.lower() in _NON_SECRET_NAMES:
            return False
        if re.fullmatch(r"[A-Z][A-Za-z0-9]*", text):
            return False
        return True
    return False


def _literal_secret(rest: str, sep: str, code: bool) -> bool:
    """True when this assignment carries a literal secret.

    In Python, TypeScript, JavaScript, and Go, a colon with no equals sign is a
    type or a parameter, not a value. YAML and env files use that same spelling
    for a value. `name: str = "<literal>"` is still an assignment.
    """
    line = rest.split("\n", 1)[0].strip()
    if sep == ":" and line.startswith("="):
        return _literal_secret(line[1:], "=", code)
    if code and sep == ":" and "=" not in line:
        return _value_is_secret(line) and (
            bool(_quote_inners(line))
            or _has_byte_list(line)
            or re.search(r"[0-9a-fA-F]{16,}", line) is not None
        )
    if "=" in line and sep == ":":
        return _value_is_secret(line.split("=", 1)[1])
    if line == "":
        return sep == "="
    return _value_is_secret(line)


def _bare_quoted_name(text: str) -> bool:
    return _KEY_QUOTED.fullmatch(text.strip()) is not None


def _key_hit(text: str, name: str) -> bool:
    """Literal secret on a key name, inside a decode call, or as a bare quoted name."""
    if (
        _bare_quoted_name(text)
        or _HEX_CALL.search(text)
        or _GETENV_HEX.search(text)
        or _has_byte_list(text)
    ):
        return True
    code = _is_code(name)
    for match in _KEY_CLI.finditer(text):
        if _value_is_secret(match.group("value")):
            return True
    for pattern in (_KEY_ASSIGN, _KEY_CAMEL):
        for match in pattern.finditer(text):
            if _literal_secret(text[match.end() :], match.group("sep"), code):
                return True
    return False


def _is_wordlist(name: str) -> bool:
    path = name.replace("\\", "/")
    return path.endswith("/" + _WORDLIST_NAME) or path == _WORDLIST_NAME


_HEADING = re.compile(r"^\s*#{2,6}\s+\S")
_SECTION = re.compile(r"^\s*\[[^\]]+\]\s*$")
_KEY_BOUNDARY = re.compile(r"""["']?(?P<key>[A-Za-z_][\w.-]*)["']?\s*[:=]""")
_LIST_MARKER = re.compile(r"^\s*(?:\(\d+\)|\d+\)|\d+\.|\d+:)\s+")
_LIST_KEY = re.compile(r"(?i)^[a-z]+\d+$")


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


def _excluded_path(name: str) -> bool:
    parts = set(name.replace("\\", "/").split("/"))
    return bool(parts & set(EXCLUDED_DIR_NAMES)) or _is_wordlist(name)


def mnemonic_hit(text: str, words: set[str]) -> bool:
    """12, 15, 18, 21, or 24 BIP39 words, including a window inside a longer run.

    Lines are lowercased. Numbered markers (`1.`, `1:`, `1)`, `(1)`) are
    stripped. Quotes, brackets, and table pipes are not letters. A blank line
    does not reset the run. A non-BIP39 word does. A config key before `:` or
    `=` resets the run. A `word1:` label does not. A heading of two or more
    hashes resets the run. A line with parentheses does not contribute its
    code tokens, but quoted strings on that line are still scanned.
    `scripts/bip39_english.txt` and vendored directories are excluded by path.
    """
    block: list[str] = []

    def hit() -> bool:
        return _block_hit(len(block))

    for raw in text.splitlines():
        if _HEADING.match(raw) or _SECTION.match(raw):
            if hit():
                return True
            block = []
            continue
        line = _LIST_MARKER.sub("", raw)
        if "://" in line:
            line = line.replace("://", " ")
        if "(" in line or ")" in line:
            if hit():
                return True
            block = []
            for quoted in _QUOTED.findall(line):
                if _add_words(quoted, block, words):
                    return True
            if hit():
                return True
            block = []
            continue
        cursor = 0
        for match in _KEY_BOUNDARY.finditer(line):
            if _add_words(line[cursor : match.start()], block, words):
                return True
            if not _LIST_KEY.match(match.group("key")):
                if hit():
                    return True
                block = []
            cursor = match.end()
        if _add_words(line[cursor:], block, words):
            return True
    return hit()


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
    if not _excluded_path(name) and mnemonic_hit(
        text, words if words is not None else bip39_words()
    ):
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
        if _excluded_path(relative):
            continue
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
