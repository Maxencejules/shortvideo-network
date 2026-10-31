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


def test_thirteen_word_run_contains_a_twelve_word_window() -> None:
    """A 13-word run is not a special exclusion. It still holds a 12-word window."""
    assert _has_mnemonic(" ".join(_words(13)))
    assert _has_mnemonic("\n".join(_words(14)))


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


def _phrase(count: int) -> str:
    return " ".join(_words(count))


def test_config_values_catch_twelve_and_twenty_four_word_mnemonics() -> None:
    for count in (12, 24):
        phrase = _phrase(count)
        samples = {
            "wallet.toml": 'seed = "' + phrase + '"\n',
            "wallet.ini": "seed=" + phrase + "\n",
            "wallet.yaml": "seed: " + phrase + "\n",
            "wallet.json": '{"seed": "' + phrase + '"}\n',
            "wallet.env": "SEED=" + phrase + "\n",
            "note.txt": phrase + "\n",
        }
        for name, body in samples.items():
            found = findings_in_text(name, body)
            assert any("bip39" in item for item in found), (count, name)
        glued = 'seed = "' + phrase + '"\nabove = "keep"\n'
        assert any("bip39" in item for item in findings_in_text("wallet.toml", glued))


def test_ordinary_prose_is_not_a_mnemonic() -> None:
    prose = (
        "The viewer uploads a video through the https api and the chain stores only a digest.\n"
        "Operators keep media bytes off the ledger.\n"
    )
    assert findings_in_text("readme.md", prose) == []


def test_numbered_colon_list_is_a_mnemonic() -> None:
    body = "\n".join(f"{index}: {word}" for index, word in enumerate(_words(12), start=1))
    assert _has_mnemonic(body)


def test_heading_then_word_list_is_a_mnemonic() -> None:
    body = "## Seed\n" + "\n".join("- " + word for word in _words(12))
    assert _has_mnemonic(body)


def test_literal_key_names_with_values() -> None:
    samples = (
        "validator" + "Priv" + "Key" + ' = "deadbeef"',
        "node" + "Private" + "Key" + ': "cafebabe"',
        "PRIV_" + "KEY_" + "HEX=deadbeef",
        "private_" + "key_" + "hex: deadbeef",
        "--" + "priv-" + "key deadbeef",
        "--" + "priv-" + "key=cafebabe",
    )
    for body in samples:
        found = findings_in_text("note.txt", body)
        assert any("private key" in item for item in found), body


def test_annotation_with_value_is_a_private_key() -> None:
    body = "def load(" + "priv_" + "key" + ': str = "deadbeef") -> None:'
    found = findings_in_text("note.py", body)
    assert any("private key" in item for item in found)


def test_yaml_type_word_is_a_value() -> None:
    body = "priv_" + "key" + ": str\n"
    found = findings_in_text("config.yaml", body)
    assert any("private key" in item for item in found), found


def test_code_without_a_literal_secret_is_not_flagged() -> None:
    samples = (
        "priv_" + "key" + " = load_key(path)",
        "def sign(" + "priv_" + "key" + "):",
        "priv_" + "key" + ' = os.environ["' + "PRIV_" + 'KEY"]',
        "priv_" + "key" + ' = os.getenv("' + "PRIV_" + 'KEY")',
        "--" + "priv-" + "key $" + "PRIV_" + "KEY",
    )
    for body in samples:
        assert findings_in_text("note.py", body) == [], body


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


def _hex(count: int = 64) -> str:
    return "ab" * (count // 2)


def _has_key(name: str, body: str) -> bool:
    return any("private key" in item for item in findings_in_text(name, body))


def test_parenthesis_lists_and_quoted_calls_are_mnemonics() -> None:
    phrase = _phrase(12)
    words = _words(12)
    assert _has_mnemonic("\n".join(f"{index}) {word}" for index, word in enumerate(words, start=1)))
    assert _has_mnemonic(
        "\n".join(f"({index}) {word}" for index, word in enumerate(words, start=1))
    )
    assert _has_mnemonic('Mnemonic("' + phrase + '")')
    assert _has_mnemonic('Account.from_mnemonic("' + phrase + '")')
    assert _has_mnemonic('ethers.Wallet.fromPhrase("' + phrase + '")')
    assert _has_mnemonic('hd.NewKey("' + phrase + '")')
    env = 'MNEMONIC="' + phrase + '"  # (devnet)\n'
    assert any("bip39" in item for item in findings_in_text("wallet.env", env))


def test_literals_inside_calls_are_private_keys() -> None:
    secret = _hex()
    bodies = [
        "PRIV_" + "KEY = bytes.from" + 'hex("' + secret + '")',
        "SigningKey(bytes.from" + 'hex("' + secret + '"))',
        "priv_" + "key = bytes([" + "1, 2, 3, 4, 5, 6, 7, 8])",
        'os.getenv("' + "PRIV_" + 'KEY", "' + secret + '")',
        "const " + "priv" + "Key = " + 'Buffer.from("' + secret + '", "hex")',
    ]
    for body in bodies:
        assert _has_key("note.py", body), body


def test_code_shapes_without_a_literal_are_not_flagged() -> None:
    samples = (
        ("note.py", "def sign(" + "priv_" + "key: PrivateKey):"),
        ("note.py", "@dataclass\nclass C:\n    " + "priv_" + "key: PrivateKey"),
        ("note.py", '"""\n' + "priv_" + 'key: the signing material\n"""'),
        ("note.py", "self." + "priv_" + "key = " + "priv_" + "key"),
        ("note.py", "sign(msg, " + "priv_" + "key=k)"),
        ("note.py", 'cfg["' + "priv_" + 'key"]'),
        ("note.py", '{"' + "priv_" + 'key": k}'),
        ("note.py", "priv_" + "key = None"),
        ("note.py", "priv_" + "key = key"),
        ("wallet.ts", "function sign(" + "priv_" + "key: Uint8Array) {}"),
        ("wallet.ts", "interface W { " + "priv_" + "key: string }"),
        ("wallet.go", 'Field string `json:"' + "priv_" + 'key"`'),
        ("wallet.go", "privKey := ed25519.NewKeyFromSeed(seed)"),
        ("config.yaml", "priv_" + "key: ${" + "PRIV_" + "KEY}"),
        ("note.py", "def f(" + "priv_" + "key: Optional[bytes] = None):"),
        ("note.txt", "--" + "private-" + 'key "$PRIV"'),
    )
    for name, body in samples:
        assert findings_in_text(name, body) == [], (name, body)


def test_more_mnemonic_and_key_shapes() -> None:
    words = _words(12)
    phrase = " ".join(words)
    labeled = "\n".join(f"word{index}: {word}" for index, word in enumerate(words, start=1))
    assert _has_mnemonic(labeled)
    toml = 'seed = "' + phrase + '" # backup (devnet)\n'
    assert any("bip39" in item for item in findings_in_text("wallet.toml", toml))
    secret = _hex()
    assert _has_key("note.txt", "--" + "priv" + "key " + "deadbeef")
    assert _has_key("note.txt", "--" + "priv_" + "key=" + "deadbeef")
    assert _has_key("note.ts", "node" + "Priv" + "Key" + 'Base64 = "' + ("QQ" * 16) + '"')
    assert _has_key("note.ts", "const " + "priv" + 'Key = hexToBytes("' + secret + '")')
    assert _has_key("note.go", 'seed := hex.DecodeString("' + secret + '")')


def test_cli_flags_git_added_mnemonic_files() -> None:
    import subprocess
    import sys
    from pathlib import Path

    root = Path(__file__).resolve().parents[2]
    phrase = _phrase(12)
    words = _words(12)
    py = root / "w.py"
    md = root / "seed.md"
    try:
        py.write_text('Mnemonic("' + phrase + '")\n', encoding="utf-8")
        md.write_text(
            "\n".join(f"{index}) {word}" for index, word in enumerate(words, start=1)) + "\n",
            encoding="utf-8",
        )
        added = subprocess.run(
            ["git", "add", "--", "w.py", "seed.md"],
            cwd=root,
            check=False,
            capture_output=True,
            text=True,
        )
        assert added.returncode == 0, added.stderr
        scanned = subprocess.run(
            [sys.executable, "scripts/scan_secrets.py"],
            cwd=root,
            check=False,
            capture_output=True,
            text=True,
        )
        assert scanned.returncode != 0
        assert "w.py" in scanned.stdout
        assert "seed.md" in scanned.stdout
    finally:
        subprocess.run(
            ["git", "reset", "-q", "HEAD", "--", "w.py", "seed.md"],
            cwd=root,
            check=False,
        )
        py.unlink(missing_ok=True)
        md.unlink(missing_ok=True)


def test_repo_scan_stays_clean() -> None:
    assert scan_repo() == []
