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
# Hex, Bytes, and B64 may follow the name with or without an underscore.
_KEY_SUFFIX = r"(?:_?(?:base64|bytes|hex|b64))?"
_KEY_BODY = "priv" + r"(?:ate)?" + r"[._-]?" + "key" + _KEY_SUFFIX
_KEY_ASSIGN = re.compile(
    r"(?<![A-Za-z0-9])['\"]?(?:[A-Za-z0-9]+_)?"
    + _KEY_BODY
    + r"(?![A-Za-z0-9])['\"]?\s*(?<![!=<>])(?P<sep>:=|\+=|=(?![=>])|:)",
    re.IGNORECASE,
)
_KEY_CAMEL = re.compile(
    r"(?<=[a-z])(?:Priv(?:ate)?Key(?:Base64|Bytes|Hex|B64)?)"
    + r"(?![A-Za-z0-9])['\"]?\s*(?<![!=<>])(?P<sep>:=|\+=|=(?![=>])|:)"
)
_KEY_INDEX = re.compile(
    r"(?i)\[\s*['\"](?:[A-Za-z0-9]+_)?"
    + "priv"
    + r"(?:ate)?"
    + r"[._-]?"
    + "key"
    + _KEY_SUFFIX
    + r"['\"]\s*\]\s*(?<![!=<>])\+?=(?![=>])"
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
    r"(?i)(?:fromhex|unhexlify|DecodeString|hexToBytes|Buffer\.from)"
    r"\s*\(\s*[\"'][0-9a-f]{48,}"
)
# A getenv default may be plain hex, 0x-hex, or a WIF-shaped token.
# The fake WIF is not strict base58: it contains 0 and l. The first letter
# stays case-sensitive so a long lowercase word is not a WIF.
_GETENV_VALUE = (
    r"(?:0x[0-9a-f]{8,}|[0-9a-f]{8,}"
    r"|(?-i:[5KL](?=[A-Za-z0-9]*\d)[A-Za-z0-9]{20,}))"
)
_GETENV_HEX = re.compile(
    r"(?i)(?:getenv|environ(?:\s*\.\s*get)?)\s*[\[(][^)]*"
    r",\s*(?:default\s*=\s*)?[\"'](?:" + _GETENV_VALUE + r")[\"']"
)
_ENV_FALLBACK = re.compile(
    r"(?i)(?:process\.env\s*(?:\[\s*)?\.?|getenv\s*\(\s*|environ(?:\s*\.\s*get)?\s*[\[(]\s*)"
    r"['\"]?(?:[A-Za-z0-9]+_)?"
    + "priv"
    + r"(?:ate)?"
    + r"[._-]?"
    + "key"
    + _KEY_SUFFIX
    + r"['\"]?\s*(?:\]|\))?\s*(?:\?\?|\|\||\bor\b)\s*"
    r"(?P<value>[\"'`][^\"'`\n]+[\"'`])"
)
_CODE_SUFFIXES = {".py", ".pyi", ".ts", ".tsx", ".js", ".jsx", ".go"}
# Bare assignment values in this set are identifiers, not secrets.
# `abc` is not in it: older fixtures use it as a literal value.
# `str` is an identifier. A pure hex token of 7 or more characters, such as
# ed25519, is a literal. A digit alone does not make an identifier a literal.
_NON_SECRET_NAMES = {
    "k",
    "key",
    "priv_key",
    "privkey",
    "private_key",
    "priv_key_default",
    "signing_key",
    "raw",
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
_LITERAL_BARE = {"abc"}
# Mnemonic hits inside these directory names are ignored. The names must be a
# whole path component. `vendor` and `dist` are not in this set.
_MNEMONIC_SKIP_DIRS = frozenset(
    {"node_modules", ".venv", "site-packages", "__pycache__", ".toolchains"}
)
# `priv` through `signer` match inside a camel name such as `SigningKey`.
# `key` is a whole token or a `_key` suffix, so `validator_key` and `NODE_KEY`
# match and `key2` does not.
_KEYISH = re.compile(
    r"(?i)(?:priv|private|secret|seed|mnemonic|signing|signer)"
    r"|(?:^|[^A-Za-z0-9])key(?![A-Za-z0-9])"
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


def _statement_start(text: str, start: int) -> int:
    """Start of the statement containing ``start``.

    A newline or semicolon ends the previous statement. A previous line is part
    of this statement only when a parenthesis on that line is still open.
    """
    i = start - 1
    depth = 0
    while i >= 0:
        ch = text[i]
        if ch == ")":
            depth += 1
        elif ch == "(":
            if depth == 0:
                i -= 1
                continue
            depth -= 1
        elif ch == "\n" and depth == 0:
            if not _line_has_unclosed_paren(text, i):
                return i + 1
        elif ch == ";" and depth == 0:
            return i + 1
        i -= 1
    return 0


def _line_has_unclosed_paren(text: str, newline: int) -> bool:
    """True when the line ending at ``newline`` has a ``(`` that is still open."""
    j = newline - 1
    depth = 0
    while j >= 0 and text[j] != "\n":
        if text[j] == ")":
            depth += 1
        elif text[j] == "(":
            if depth == 0:
                return True
            depth -= 1
        j -= 1
    return False


def _call_window_is_keyish(text: str, start: int, end: int) -> bool:
    window = text[_statement_start(text, start) : end]
    return _KEYISH.search(window) is not None


def _trim_assigned(value: str) -> str:
    """Drop a signature tail so ``None) -> None:`` and ``None): ...`` read as None."""
    text = value.strip().strip("`")
    text = re.sub(r"\s+#.*$", "", text).strip()
    text = re.split(r"\)\s*->", text, maxsplit=1)[0]
    text = re.split(r"\)\s*:", text, maxsplit=1)[0]
    text = re.split(r",\s*[A-Za-z_][\w]*\s*:", text, maxsplit=1)[0]
    text = re.sub(r"\s*\.\.\.\s*$", "", text)
    text = text.rstrip(",;)]}:.`!")
    return text.strip().strip("`")


def _is_helm(inner: str) -> bool:
    return re.fullmatch(r"\{\{.*\}\}", inner.strip()) is not None


def _is_placeholder(inner: str) -> bool:
    return re.fullmatch(r"<[^>\n]*>", inner.strip()) is not None


def _quote_is_ignorable(inner: str, *, annotation: bool) -> bool:
    """True when this quoted text is not a literal secret."""
    if _is_dollar_ref(inner) or _is_helm(inner) or _is_placeholder(inner):
        return True
    # A quoted value is a literal. A type name is recognized only on a colon
    # annotation that has no equals sign, and only when it has no digit.
    return annotation and _is_type_name(inner)


def _letter_hex_word(token: str) -> bool:
    """Seven hex letters and no digit. ``defaced`` is one. ``ed25519`` is not."""
    return re.fullmatch(r"[A-Fa-f]{7}", token) is not None


def _bare_literal(token: str, *, code: bool) -> bool:
    """``abc``, pure hex of 7 or more characters, or base64 that contains a digit.

    In code, a 7-letter hex word is an identifier, not a literal.
    """
    if token.lower() in _LITERAL_BARE:
        return True
    if code and _letter_hex_word(token):
        return False
    if re.fullmatch(r"[0-9a-fA-F]{7,}", token):
        return True
    return (
        len(token) >= 16
        and re.fullmatch(r"[A-Za-z0-9+/=]+", token) is not None
        and re.search(r"\d", token) is not None
    )


def _value_suppressed(value: str, *, annotation: bool = False, code: bool = True) -> bool:
    """True when a matched value is not a literal secret.

    A quoted value is a literal and is not a type name. A ``$`` reference, a
    Helm ``{{ }}`` template, and an angle-bracket placeholder are not literals.
    A colon annotation with no equals sign may name a CamelCase type that has
    a lowercase letter and no digit, including a quoted one. In code, other
    identifiers are not literals, except ``abc``, pure hex of 7 or more
    characters (not a 7-letter word such as ``defaced``), and base64 of 16 or
    more characters that contains a digit. Outside code, an unquoted token is
    a literal. ``str``, ``null``, ``None``, and ``~`` are not.
    """
    text = _trim_assigned(value)
    if text == "":
        return False
    if text == "~":
        return True
    if _is_dollar_ref(text) or _is_helm(text) or _is_placeholder(text):
        return True
    if (
        re.search(r"<[^>\n]*>", text)
        and not _quote_inners(text)
        and re.search(r"[0-9a-fA-F]{16,}", text) is None
        and not _has_byte_list(text)
    ):
        return True
    if re.fullmatch(r"(?i)none|null|nil|undefined", text):
        return True
    if not code and text.lower() == "str":
        return True
    if re.fullmatch(r"0x[0-9a-fA-F]{8,}", text):
        return False
    if re.fullmatch(r"[0-9a-fA-F]{7,}", text):
        if code and _letter_hex_word(text):
            return True
        return False
    if len(text) >= 16 and re.fullmatch(r"[A-Za-z0-9+/]+=*", text) and re.search(r"\d", text):
        return False
    if "(" in text or "[" in text:
        inners = [inner for inner in _quote_inners(text) if not _is_dollar_ref(inner)]
        if any(_hex_or_base64(inner) for inner in inners) or _has_byte_list(text):
            return False
        # A parenthesized quote is a literal. A call such as getenv("NAME") is not.
        if re.match(r"^\(\s*[\"']", text) and any(
            inner.strip() != "" and not _quote_is_ignorable(inner, annotation=annotation)
            for inner in inners
        ):
            return False
        return True
    inners = _quote_inners(text)
    if inners:
        if all(_quote_is_ignorable(inner, annotation=annotation) for inner in inners):
            return True
        return False
    if re.fullmatch(r"[A-Za-z_][\w.]*", text):
        if not code:
            return False
        if "." in text:
            return True
        return not _bare_literal(text.split(".")[-1], code=True)
    return False


def _is_type_name(inner: str) -> bool:
    """CamelCase with a lowercase letter and no digit. ``KwDiBf89`` is not one."""
    return (
        re.fullmatch(r"[A-Z][A-Za-z]*", inner) is not None
        and re.search(r"[a-z]", inner) is not None
    )


def _rest_of_statement(text: str, start: int) -> str:
    """Text from ``start`` through the end of this statement.

    Following lines stay in the statement while a parenthesis is open, so
    ``name = (\\n    "literal"\\n)`` is one value.
    """
    index = start
    depth = 0
    while index < len(text):
        ch = text[index]
        if ch == "(":
            depth += 1
        elif ch == ")" and depth:
            depth -= 1
        elif ch in "\n;" and depth == 0:
            break
        index += 1
    return text[start:index]


def _assignment_suppressed(text: str, match: re.Match[str], name: str) -> bool:
    rest = _rest_of_statement(text, match.end())
    line = rest.split("\n", 1)[0].strip()
    sep = match.group("sep")
    if sep == ":" and line.startswith("="):
        line = line[1:].strip()
        sep = "="
        rest = rest.split("=", 1)[1]
    if _is_code(name) and sep == ":" and "=" not in line:
        return (
            _value_suppressed(line, annotation=True, code=True)
            or not _quote_inners(line)
            and not re.fullmatch(r"0x[0-9a-fA-F]{8,}|[0-9a-fA-F]{8,}", line.strip())
            and not (
                re.fullmatch(r"[0-9a-fA-F]{7,}", line.strip()) is not None
                and not _letter_hex_word(line.strip())
            )
        )
    if sep == ":" and "=" in line:
        return _value_suppressed(line.split("=", 1)[1], code=_is_code(name))
    return _value_suppressed(rest, code=_is_code(name))


def _quoted_kept(text: str, match: re.Match[str]) -> bool:
    return text.strip() == match.group(0)


def _key_hit(text: str, name: str) -> bool:
    """Base c187f64 hits, plus later patterns, minus narrow suppressions."""
    for match in _KEY_QUOTED.finditer(text):
        if _quoted_kept(text, match):
            return True
    for match in _KEY_ASSIGN.finditer(text):
        if (
            _is_code(name)
            and match.group("sep") == ":"
            and _TYPE_AFTER.match(text[match.end() :])
            and "=" not in text[match.end() :].split("\n", 1)[0]
        ):
            continue
        if not _assignment_suppressed(text, match, name):
            return True
    for match in _KEY_CAMEL.finditer(text):
        if not _assignment_suppressed(text, match, name):
            return True
    for match in _KEY_CLI.finditer(text):
        if not _value_suppressed(match.group("value"), code=False):
            return True
    for match in _KEY_INDEX.finditer(text):
        if not _value_suppressed(_rest_of_statement(text, match.end()), code=True):
            return True
    for match in _ENV_FALLBACK.finditer(text):
        if not _value_suppressed(match.group("value"), code=False):
            return True
    for pattern in (_HEX_CALL, _GETENV_HEX, _BYTE_LIST):
        for match in pattern.finditer(text):
            if _call_window_is_keyish(text, match.start(), match.end()):
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


def _mnemonic_skip(name: str) -> bool:
    """Skip mnemonic hits only inside real third-party roots, or the wordlist."""
    parts = name.replace("\\", "/").split("/")
    return _is_wordlist(name) or any(part in _MNEMONIC_SKIP_DIRS for part in parts)


def _base_mnemonic_hit(text: str, words: set[str]) -> bool:
    """c187f64 mnemonic detector. The text is not rewritten and no line is dropped."""
    block: list[str] = []

    def finish() -> bool:
        count = len(block)
        if count in (13, 14) or count < 12:
            return False
        if count in MNEMONIC_LENGTHS:
            return True
        return any(length <= count for length in MNEMONIC_LENGTHS)

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


def _extra_mnemonic_hit(text: str, words: set[str]) -> bool:
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


def mnemonic_hit(text: str, words: set[str]) -> bool:
    """Union of the c187f64 detector and later detectors. The base sees `text` raw."""
    return _base_mnemonic_hit(text, words) or _extra_mnemonic_hit(text, words)


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
    if not _mnemonic_skip(name) and mnemonic_hit(
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
