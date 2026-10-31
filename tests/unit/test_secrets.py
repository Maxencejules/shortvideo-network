"""Secret-scanner fixtures. Markers are built at runtime and are not files."""

from __future__ import annotations

from scripts.scan_secrets import findings_in_text, scan_repo


def _joined(*parts: str) -> str:
    return "".join(parts)


def test_encrypted_pkcs8_pgp_slack_and_priv_key() -> None:
    pkcs8 = _joined("-----BEGIN ", "ENCRYPTED PRIVATE KEY-----")
    pgp = _joined("-----BEGIN ", "PGP PRIVATE KEY BLOCK-----")
    slack = _joined("xox", "b-", "0000000000-FAKE")
    priv = '{"' + "priv_" + 'key": {"type": "tendermint/PrivKeyEd25519"}}'
    assert findings_in_text("note.txt", pkcs8 + "\n")
    assert findings_in_text("note.txt", pgp + "\n")
    assert findings_in_text("note.txt", slack)
    assert findings_in_text("other.json", priv)


def _bip_prefix(count: int) -> str:
    raw = [
        "aban" + "don",
        "abil" + "ity",
        "ab" + "le",
        "ab" + "out",
        "ab" + "ove",
        "ab" + "sent",
        "ab" + "sorb",
        "abs" + "tract",
        "abs" + "urd",
        "ab" + "use",
        "acc" + "ess",
        "acci" + "dent",
        "acc" + "ount",
        "acc" + "use",
        "ach" + "ieve",
        "ac" + "id",
        "acou" + "stic",
        "acqu" + "ire",
        "acr" + "oss",
        "a" + "ct",
        "act" + "ion",
        "act" + "or",
        "actr" + "ess",
        "actu" + "al",
        "ad" + "apt",
    ]
    return " ".join(raw[:count])


def test_mnemonic_json_value() -> None:
    body = '{"seed":"' + _bip_prefix(12) + '"}'
    assert any("bip39" in item for item in findings_in_text("wallet.json", body))


def test_mnemonic_env_value() -> None:
    body = "MNEMONIC=" + _bip_prefix(12)
    assert any("bip39" in item for item in findings_in_text("wallet.env", body))


def test_fifteen_word_phrase() -> None:
    assert any("bip39" in item for item in findings_in_text("note.txt", _bip_prefix(15)))


def test_unquoted_priv_key() -> None:
    body = "priv_" + "key" + ": ed25519"
    found = findings_in_text("note.txt", body)
    assert found
    assert all("bip39" not in item for item in found)


def test_priv_key_equals_and_uppercase_env() -> None:
    equals = "priv_" + "key" + ' = "abc"'
    env = "PRIV_" + "KEY=abc"
    mixed = "Priv_" + "Key" + " = abc"
    hyphen = "priv-" + "key=abc"
    quoted = '"' + "PRIV_" + "KEY" + '"'
    for name, body in (
        ("a.txt", equals),
        ("b.env", env),
        ("c.txt", mixed),
        ("d.txt", hyphen),
        ("e.json", quoted),
    ):
        found = findings_in_text(name, body)
        assert found, body
        assert all("bip39" not in item for item in found)


def test_mnemonic_one_word_per_line() -> None:
    body = "\n".join(_bip_prefix(12).split())
    assert any("bip39" in item for item in findings_in_text("note.txt", body))


def test_fifteen_words_one_per_line() -> None:
    body = "\n".join(_bip_prefix(15).split())
    assert any("bip39" in item for item in findings_in_text("note.txt", body))


def test_mnemonic_split_six_and_six() -> None:
    words = _bip_prefix(12).split()
    body = " ".join(words[:6]) + "\n" + " ".join(words[6:])
    assert any("bip39" in item for item in findings_in_text("note.txt", body))


def test_mnemonic_split_across_three_lines() -> None:
    words = _bip_prefix(12).split()
    body = "\n".join(" ".join(words[i : i + 4]) for i in range(0, 12, 4))
    assert any("bip39" in item for item in findings_in_text("note.txt", body))


def test_prefixed_mnemonic_split_across_lines() -> None:
    words = _bip_prefix(12).split()
    body = "MNEMONIC=" + " ".join(words[:6]) + "\n" + " ".join(words[6:])
    assert any("bip39" in item for item in findings_in_text("wallet.env", body))


def test_six_words_are_not_a_mnemonic() -> None:
    assert findings_in_text("note.txt", " ".join(_bip_prefix(12).split()[:6])) == []


def _words(count: int) -> list[str]:
    return _bip_prefix(count).split()


def _has_mnemonic(body: str) -> bool:
    return any("bip39" in item for item in findings_in_text("note.txt", body))


def test_mnemonic_with_blank_lines() -> None:
    assert _has_mnemonic("\n\n".join(_words(12)))


def test_mnemonic_markdown_and_yaml_lists() -> None:
    words = _words(12)
    assert _has_mnemonic("\n".join("- " + word for word in words))
    assert _has_mnemonic("\n".join("* " + word for word in words))
    assert _has_mnemonic("\n".join(f"{index}. {word}" for index, word in enumerate(words, start=1)))
    assert _has_mnemonic("\n".join("  - " + word for word in words))


def test_mnemonic_json_array() -> None:
    body = "[\n" + ",\n".join(f'  "{word}"' for word in _words(12)) + "\n]"
    assert _has_mnemonic(body)


def test_mnemonic_markdown_table() -> None:
    words = _words(12)
    rows = [f"| {words[index]} | {words[index + 1]} |" for index in range(0, 12, 2)]
    assert _has_mnemonic("\n".join(rows))


def test_mnemonic_in_comments() -> None:
    assert _has_mnemonic("\n".join("# " + word for word in _words(12)))


def test_mnemonic_upper_and_title_case() -> None:
    words = _words(12)
    assert _has_mnemonic("\n".join(word.upper() for word in words))
    assert _has_mnemonic("\n".join(word.title() for word in words))


def test_mnemonic_padded_past_24_words() -> None:
    assert _has_mnemonic("\n".join(_words(25)))
    assert _has_mnemonic(" ".join(_words(25)))


def test_thirteen_and_fourteen_word_blocks_are_not_mnemonics() -> None:
    for count in (13, 14):
        assert findings_in_text("note.txt", "\n".join(_words(count))) == []
        assert findings_in_text("note.txt", " ".join(_words(count))) == []


def test_sixteen_word_block_has_a_valid_window() -> None:
    assert _has_mnemonic("\n".join(_words(16)))


def test_wordlist_path_is_excluded() -> None:
    from pathlib import Path

    from scripts.scan_secrets import bip39_words, mnemonic_hit

    text = Path("scripts/bip39_english.txt").read_text(encoding="utf-8")
    assert mnemonic_hit(text, bip39_words())
    found = findings_in_text("scripts/bip39_english.txt", text)
    assert not any("bip39" in item for item in found)


def test_validator_and_camel_case_key_names() -> None:
    samples = (
        "validator_" + "priv_" + "key" + ": abc",
        "VALIDATOR_" + "PRIV_" + "KEY=",
        '{"' + "priv" + 'Key": "x"}',
        "priv" + "key" + ": abc",
        "priv" + ".key" + ": abc",
        "private_" + "key" + ": abc",
        "validator_" + "private_" + "key" + ": abc",
        "private-" + "key" + ": abc",
    )
    for body in samples:
        found = findings_in_text("note.txt", body)
        assert any("private key" in item for item in found), body
        assert all("bip39" not in item for item in found)


def test_type_annotation_is_not_a_private_key() -> None:
    samples = (
        "def load(" + "priv_" + "key" + ": bytes) -> None:",
        "def load(" + "priv_" + "key" + ": str) -> None:",
        "def load(" + "priv_" + "key" + ": bytes | None) -> None:",
        "def load(" + "priv_" + "key" + ": Optional[bytes]) -> None:",
        "def load(" + "private_" + "key" + ": Path) -> None:",
        "def load(" + "priv_" + "key" + ": list[str]) -> None:",
        "def load(" + "priv_" + "key" + ": dict[str, bytes]) -> None:",
    )
    for body in samples:
        assert findings_in_text("note.py", body) == [], body


def test_bip39_line_and_key_suffix() -> None:
    phrase = " ".join(
        [
            "aban" + "don",
            "abil" + "ity",
            "ab" + "le",
            "ab" + "out",
            "ab" + "ove",
            "ab" + "sent",
            "ab" + "sorb",
            "abs" + "tract",
            "abs" + "urd",
            "ab" + "use",
            "acc" + "ess",
            "acci" + "dent",
        ]
    )
    assert findings_in_text("note.txt", phrase)
    assert findings_in_text("forced.key", "not-a-key-body")


def test_repo_scan_stays_clean() -> None:
    assert scan_repo() == []
