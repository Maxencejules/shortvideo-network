"""SVN-JCS-1, a restricted profile of RFC 8785 for off-chain signed records.

The profile uses RFC 8785 string serialization and UTF-16 code-unit key
ordering. It rejects null, fractions, exponents, and integers outside the
I-JSON exact range [-9007199254740991, 9007199254740991]. uint64 values
that may exceed that range are JSON strings, not JSON numbers.

Object keys must be ASCII [A-Za-z0-9._-] so the UTF-16 order matches
unsigned byte order. This module does not read a clock or the network.
"""

from __future__ import annotations

SAFE_INTEGER_MAX = 9007199254740991
MAX_DEPTH = 8
MAX_CANONICAL_BYTES = 65536
KEY_ALPHABET = set("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789._-")


class JCSError(ValueError):
    def __init__(self, reason: str) -> None:
        super().__init__(reason)
        self.reason = reason


def canonicalize(value: object) -> str:
    encoded = _emit(value, 0).encode("utf-8")
    if len(encoded) > MAX_CANONICAL_BYTES:
        raise JCSError("bounds")
    return encoded.decode("utf-8")


def parse_and_canonicalize(raw: str) -> str:
    value, index = _parse_value(raw, 0, 0)
    index = _skip(raw, index)
    if index != len(raw):
        raise JCSError("trailing")
    return canonicalize(value)


def _emit(value: object, depth: int) -> str:
    if depth > MAX_DEPTH:
        raise JCSError("depth")
    if value is None:
        raise JCSError("null")
    if isinstance(value, bool):
        return "true" if value else "false"
    if isinstance(value, int):
        if abs(value) > SAFE_INTEGER_MAX:
            raise JCSError("unsafe_integer")
        return str(value)
    if isinstance(value, str):
        return _emit_string(value)
    if isinstance(value, list):
        return "[" + ",".join(_emit(item, depth + 1) for item in value) + "]"
    if isinstance(value, dict):
        keys: list[str] = []
        for key in value:
            if not isinstance(key, str):
                raise JCSError("bad_key")
            _check_key(key)
            keys.append(key)
        keys.sort(key=_utf16_units)
        parts = [f"{_emit_string(key)}:{_emit(value[key], depth + 1)}" for key in keys]
        return "{" + ",".join(parts) + "}"
    raise JCSError("malformed")


def _emit_string(text: str) -> str:
    out = ['"']
    for char in text:
        code = ord(char)
        if 0xD800 <= code <= 0xDFFF:
            raise JCSError("lone_surrogate")
        if code == 0x22:
            out.append('\\"')
        elif code == 0x5C:
            out.append("\\\\")
        elif code == 0x08:
            out.append("\\b")
        elif code == 0x09:
            out.append("\\t")
        elif code == 0x0A:
            out.append("\\n")
        elif code == 0x0C:
            out.append("\\f")
        elif code == 0x0D:
            out.append("\\r")
        elif code < 0x20:
            out.append(f"\\u{code:04x}")
        else:
            out.append(char)
    out.append('"')
    return "".join(out)


def _utf16_units(text: str) -> tuple[int, ...]:
    raw = text.encode("utf-16-be")
    return tuple(int.from_bytes(raw[index : index + 2], "big") for index in range(0, len(raw), 2))


def _check_key(key: str) -> None:
    if not key or len(key.encode("utf-8")) > 64 or any(char not in KEY_ALPHABET for char in key):
        raise JCSError("bad_key")


def _skip(raw: str, index: int) -> int:
    while index < len(raw) and raw[index] in " \t\r\n":
        index += 1
    return index


def _parse_value(raw: str, index: int, depth: int) -> tuple[object, int]:
    if depth > MAX_DEPTH:
        raise JCSError("depth")
    index = _skip(raw, index)
    if index >= len(raw):
        raise JCSError("malformed")
    char = raw[index]
    if char == "{":
        return _parse_object(raw, index, depth)
    if char == "[":
        return _parse_array(raw, index, depth)
    if char == '"':
        return _parse_string(raw, index)
    if char == "t":
        return _literal(raw, index, "true", True)
    if char == "f":
        return _literal(raw, index, "false", False)
    if char == "n":
        raise JCSError("null")
    if char == "-" or char.isdigit():
        return _parse_number(raw, index)
    raise JCSError("malformed")


def _literal(raw: str, index: int, text: str, value: bool) -> tuple[bool, int]:
    if raw[index : index + len(text)] != text:
        raise JCSError("unknown_literal")
    return value, index + len(text)


def _parse_number(raw: str, index: int) -> tuple[int, int]:
    start = index
    if raw[index] == "-":
        index += 1
        if index >= len(raw) or not raw[index].isdigit():
            raise JCSError("malformed")
    if raw[index] == "0":
        index += 1
        if index < len(raw) and raw[index].isdigit():
            raise JCSError("leading_zero")
    else:
        if index >= len(raw) or not raw[index].isdigit():
            raise JCSError("malformed")
        while index < len(raw) and raw[index].isdigit():
            index += 1
    if index < len(raw) and raw[index] in ".eE":
        raise JCSError("fraction" if raw[index] == "." else "exponent")
    token = raw[start:index]
    if token in {"-0", "-"}:
        raise JCSError("negative_zero")
    value = int(token)
    if abs(value) > SAFE_INTEGER_MAX:
        raise JCSError("unsafe_integer")
    return value, index


def _parse_string(raw: str, index: int) -> tuple[str, int]:
    if raw[index] != '"':
        raise JCSError("malformed")
    index += 1
    chars: list[str] = []
    while index < len(raw):
        char = raw[index]
        if char == '"':
            return "".join(chars), index + 1
        if char == "\\":
            index += 1
            if index >= len(raw):
                raise JCSError("malformed")
            escaped = raw[index]
            mapping = {"b": "\b", "t": "\t", "n": "\n", "f": "\f", "r": "\r", '"': '"', "\\": "\\", "/": "/"}
            if escaped in mapping:
                chars.append(mapping[escaped])
                index += 1
                continue
            if escaped != "u":
                raise JCSError("malformed")
            hex_digits = raw[index + 1 : index + 5]
            if len(hex_digits) != 4 or any(c not in "0123456789abcdefABCDEF" for c in hex_digits):
                raise JCSError("malformed")
            code = int(hex_digits, 16)
            index += 5
            if 0xD800 <= code <= 0xDBFF:
                if raw[index : index + 2] != "\\u":
                    raise JCSError("lone_surrogate")
                low_hex = raw[index + 2 : index + 6]
                if len(low_hex) != 4:
                    raise JCSError("lone_surrogate")
                low = int(low_hex, 16)
                if not 0xDC00 <= low <= 0xDFFF:
                    raise JCSError("lone_surrogate")
                code = 0x10000 + ((code - 0xD800) << 10) + (low - 0xDC00)
                index += 6
            elif 0xDC00 <= code <= 0xDFFF:
                raise JCSError("lone_surrogate")
            chars.append(chr(code))
            continue
        if ord(char) < 0x20:
            raise JCSError("malformed")
        chars.append(char)
        index += 1
    raise JCSError("malformed")


def _parse_object(raw: str, index: int, depth: int) -> tuple[dict[str, object], int]:
    index += 1
    obj: dict[str, object] = {}
    index = _skip(raw, index)
    if index < len(raw) and raw[index] == "}":
        return obj, index + 1
    while index < len(raw):
        index = _skip(raw, index)
        key, index = _parse_string(raw, index)
        if key in obj:
            raise JCSError("duplicate_key")
        _check_key(key)
        index = _skip(raw, index)
        if index >= len(raw) or raw[index] != ":":
            raise JCSError("malformed")
        value, index = _parse_value(raw, index + 1, depth + 1)
        obj[key] = value
        index = _skip(raw, index)
        if index >= len(raw):
            raise JCSError("malformed")
        if raw[index] == "}":
            return obj, index + 1
        if raw[index] != ",":
            raise JCSError("malformed")
        index += 1
        index = _skip(raw, index)
        if index < len(raw) and raw[index] == "}":
            raise JCSError("trailing_comma")
    raise JCSError("malformed")


def _parse_array(raw: str, index: int, depth: int) -> tuple[list[object], int]:
    index += 1
    items: list[object] = []
    index = _skip(raw, index)
    if index < len(raw) and raw[index] == "]":
        return items, index + 1
    while index < len(raw):
        value, index = _parse_value(raw, index, depth + 1)
        items.append(value)
        index = _skip(raw, index)
        if index >= len(raw):
            raise JCSError("malformed")
        if raw[index] == "]":
            return items, index + 1
        if raw[index] != ",":
            raise JCSError("malformed")
        index += 1
        index = _skip(raw, index)
        if index < len(raw) and raw[index] == "]":
            raise JCSError("trailing_comma")
    raise JCSError("malformed")
