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
