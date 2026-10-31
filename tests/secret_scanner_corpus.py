"""Table of secret-scanner cases from PR 6 reviews, rounds 2 through 8.

Content is built at runtime from fragments so this source file does not itself
contain a contiguous secret or a 12-word phrase. ``expect`` is whether the
scanner should flag the text.
"""

from __future__ import annotations

from typing import TypedDict


class Case(TypedDict):
    id: str
    name: str
    text: str
    expect: bool
    cli: bool


def _words(count: int) -> list[str]:
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
    return raw[:count]


def _phrase(count: int) -> str:
    return " ".join(_words(count))


def _hex(count: int = 64) -> str:
    return "ab" * (count // 2)


def _b64() -> str:
    return "MC4CAQ" + ("A" * 24)


def _openssh() -> str:
    return (
        "-----BEGIN "
        + "OPENSSH PRIVATE KEY-----\n"
        + "ZmFrZS1rZXk=\n"
        + "-----END OPENSSH PRIVATE KEY-----\n"
    )


def _akia() -> str:
    return "AKIA" + "0000000000000000"


def cases() -> list[Case]:
    """Every review case. ``cli`` entries are also git-added in the corpus test."""
    phrase = _phrase(12)
    long = _phrase(24)
    words = _words(12)
    out: list[Case] = []

    def add(case_id: str, name: str, text: str, expect: bool, cli: bool = False) -> None:
        out.append({"id": case_id, "name": name, "text": text, "expect": expect, "cli": cli})

    add("r2-json-mnemonic", "wallet.json", '{"mnemonic": "' + phrase + '"}', True)
    add("r2-env-seed", "wallet.env", 'SEED="' + phrase + '"\n', True)
    add("r2-fifteen", "note.txt", _phrase(15), True)
    add("r2-unquoted-colon", "note.txt", "priv_" + "key: ed25519", True)
    add("r3-equals-quoted", "note.txt", "priv_" + 'key = "abc"', True)
    add("r3-env-upper", "wallet.env", "PRIV_" + "KEY=abc\n", True)
    add("r3-one-per-line", "note.txt", "\n".join(words), True)
    add("r3-split-6-6", "note.txt", " ".join(words[:6]) + "\n" + " ".join(words[6:]), True)
    add("r4-blank-lines", "note.txt", "\n\n".join(words), True)
    add("r4-dash-list", "note.md", "\n".join("- " + word for word in words), True)
    add("r4-star-list", "note.md", "\n".join("* " + word for word in words), True)
    add("r4-dot-list", "note.md", "\n".join(f"{i}. {w}" for i, w in enumerate(words, 1)), True)
    add("r4-yaml-list", "note.yaml", "\n".join("  - " + word for word in words), True)
    add(
        "r4-json-array",
        "note.json",
        "[\n" + ",\n".join(f'  "{word}"' for word in words) + "\n]",
        True,
    )
    add(
        "r4-table",
        "note.md",
        "\n".join(f"| {words[i]} | {words[i + 1]} |" for i in range(0, 12, 2)),
        True,
    )
    add("r4-hash-comments", "note.txt", "\n".join("# " + word for word in words), True)
    add("r4-upper", "note.txt", "\n".join(word.upper() for word in words), True)
    add("r4-title", "note.txt", "\n".join(word.title() for word in words), True)
    add("r4-padded-25", "note.txt", " ".join(_words(25)), True)
    add("r4-validator-prefix", "note.txt", "validator_" + "priv_" + "key: abc", True)
    add("r4-validator-env", "note.env", "VALIDATOR_" + "PRIV_" + "KEY=\n", True)
    add("r4-camel-json", "note.json", '{"' + "priv" + 'Key": "x"}', True)
    add("r4-joined", "note.txt", "priv" + "key: abc", True)
    add("r4-dotted", "note.txt", "priv" + ".key: abc", True)
    add("r4-private", "note.txt", "private_" + "key: abc", True)
    add("r4-annotation-bytes", "note.py", "def load(" + "priv_" + "key: bytes) -> None:", False)
    add("r5-toml", "wallet.toml", 'seed = "' + phrase + '"\n', True)
    add("r5-ini", "wallet.ini", "seed=" + phrase + "\n", True)
    add("r5-yaml", "wallet.yaml", "seed: " + phrase + "\n", True)
    add("r5-json", "wallet.json", '{"seed": "' + phrase + '"}\n', True)
    add("r5-dotenv", "wallet.env", "SEED=" + phrase + "\n", True)
    add("r5-prose", "note.txt", phrase + "\n", True)
    add("r5-24", "note.txt", long + "\n", True)
    add(
        "r5-toml-account",
        "wallet.toml",
        '[wallet]\nmnemonic = "' + phrase + '"\naccount = 0\n',
        True,
    )
    add("r5-ini-name", "wallet.ini", "mnemonic=" + phrase + "\nname=alice\n", True)
    add(
        "r5-yaml-index",
        "wallet.yaml",
        "wallet:\n  mnemonic: " + phrase + "\n  index: 0\n",
        True,
    )
    add(
        "r5-json-index",
        "wallet.json",
        '{"mnemonic": "' + phrase + '", "index": 0}\n',
        True,
    )
    add("r5-env-account", "wallet.env", 'MNEMONIC="' + phrase + '"\nACCOUNT=alice\n', True)
    add("r5-phrase-again", "note.txt", phrase + " again\n", True)
    add("r5-colon-list", "note.md", "\n".join(f"{i}: {w}" for i, w in enumerate(words, 1)), True)
    add(
        "r5-heading-list",
        "note.md",
        "## Seed\n" + "\n".join("- " + word for word in words),
        True,
    )
    add(
        "r5-annotation-value",
        "note.py",
        "def load(" + "priv_" + 'key: str = "deadbeef") -> None:',
        True,
    )
    add("r5-yaml-str", "config.yaml", "priv_" + "key: str\n", True)
    add("r5-validator-camel", "note.txt", "validator" + "Priv" + 'Key: "deadbeef"', True)
    add("r5-node-private", "note.json", '{"node' + "Private" + 'Key": "cafebabe"}', True)
    add("r5-hex-suffix-env", "note.env", "PRIV_" + "KEY_" + "HEX=deadbeef\n", True)
    add("r5-hex-suffix-colon", "note.txt", "private_" + "key_" + "hex: deadbeef", True)
    add("r5-flag", "note.txt", "--" + "private-" + "key deadbeef", True)
    add(
        "r5-ordinary-prose",
        "note.txt",
        "The viewer uploads a video through the https api.\n",
        False,
    )
    add("r6-paren-list", "seed.md", "\n".join(f"{i}) {w}" for i, w in enumerate(words, 1)), True)
    add(
        "r6-paren-num-list",
        "seed.md",
        "\n".join(f"({i}) {w}" for i, w in enumerate(words, 1)),
        True,
    )
    add("r6-mnemonic-call", "w.py", 'Mnemonic("' + phrase + '")\n', True)
    add("r6-from-mnemonic", "w.py", 'Account.from_mnemonic("' + phrase + '")\n', True)
    add("r6-from-phrase", "w.ts", 'ethers.Wallet.fromPhrase("' + phrase + '")\n', True)
    add("r6-newkey", "w.go", 'hd.NewKey("' + phrase + '")\n', True)
    add("r6-env-paren-comment", "a.env", 'MNEMONIC="' + phrase + '"  # (devnet)\n', True)
    add(
        "r6-fromhex",
        "note.py",
        "PRIV_" + "KEY = bytes.from" + 'hex("' + _hex() + '")',
        True,
    )
    add(
        "r6-signingkey",
        "note.py",
        "SigningKey(bytes.from" + 'hex("' + _hex() + '"))',
        True,
    )
    add(
        "r6-byte-list",
        "note.py",
        "priv_" + "key = bytes([" + "1, 2, 3, 4, 5, 6, 7, 8])",
        True,
    )
    add("r6-getenv-hex", "note.py", 'os.getenv("PRIV_KEY", "' + _hex() + '")', True)
    add(
        "r6-buffer",
        "note.ts",
        'const privKey = Buffer.from("' + _hex() + '", "hex")',
        True,
    )
    add("r6-def-type", "note.py", "def sign(" + "priv_" + "key: PrivateKey):", False)
    add("r6-dataclass", "note.py", "class C:\n    " + "priv_" + "key: PrivateKey", False)
    add("r6-docstring", "note.py", '"""\n' + "priv_" + 'key: the signing material\n"""', False)
    add("r6-self", "note.py", "self." + "priv_" + "key = " + "priv_" + "key", False)
    add("r6-kw", "note.py", "sign(msg, " + "priv_" + "key=k)", False)
    add("r6-subscript", "note.py", 'cfg["' + "priv_" + 'key"]', False)
    add("r6-map-ident", "note.py", '{"' + "priv_" + 'key": k}', False)
    add("r6-none", "note.py", "priv_" + "key = None", False)
    add("r6-ident", "note.py", "priv_" + "key = key", False)
    add("r6-ts-param", "wallet.ts", "function sign(" + "priv_" + "key: Uint8Array) {}", False)
    add("r6-ts-iface", "wallet.ts", "interface W { " + "priv_" + "key: string }", False)
    add("r6-go-tag", "wallet.go", 'Field string `json:"' + "priv_" + 'key"`', False)
    add("r6-go-seed", "wallet.go", "privKey := ed25519.NewKeyFromSeed(seed)", False)
    add("r6-yaml-dollar", "config.yaml", "priv_" + "key: ${PRIV_KEY}\n", False)
    add("r6-optional-none", "note.py", "def f(" + "priv_" + "key: Optional[bytes] = None):", False)
    add("r6-flag-dollar", "note.txt", "--" + "private-" + 'key "$PRIV"', False)
    add("r6-load-call", "note.py", "priv_" + "key = load_key(path)", False)
    add("r6-def-bare", "note.py", "def sign(" + "priv_" + "key):", False)
    add("r6-getenv-name", "note.py", 'priv_key = os.getenv("PRIV_KEY")', False)
    add(
        "r7-word-label",
        "note.md",
        "\n".join(f"word{i}: {w}" for i, w in enumerate(words, 1)),
        True,
    )
    add("r7-toml-paren", "wallet.toml", 'seed = "' + phrase + '" # backup (devnet)\n', True)
    add("r7-flag-joined", "note.txt", "--" + "priv" + "key deadbeef", True)
    add("r7-flag-underscore", "note.txt", "--" + "priv_" + "key=deadbeef", True)
    add(
        "r7-base64-name",
        "note.ts",
        "node" + "Priv" + "Key" + 'Base64 = "' + ("QQ" * 16) + '"',
        True,
    )
    add("r8-env-0x", "wallet.env", "PRIVATE_" + "KEY=0x" + _hex() + "\n", True, True)
    add("r8-yaml-0x", "wallet.yaml", "priv_" + "key: 0x" + _hex() + "\n", True, True)
    add("r8-py-0x", "wallet.py", "PRIV_" + "KEY = 0x" + _hex() + "\n", True, True)
    add("r8-env-b64", "wallet.env", "PRIV_" + "KEY=" + _b64() + "\n", True, True)
    add("r8-flag-0x", "note.txt", "--" + "private-" + "key 0x" + _hex() + "\n", True, True)
    add("r8-go-backtick", "bt.go", "hd.NewKey(`" + phrase + "`)\n", True)
    add("r8-js-template", "bt.js", "fromPhrase(`" + phrase + "`)\n", True)
    add("r8-yaml-unquoted-paren", "m.yaml", "mnemonic: " + phrase + "  # (devnet)\n", True)
    add("r8-ini-semi-paren", "m.ini", "mnemonic = " + phrase + " ; (devnet)\n", True)
    add("r8-signing-key", "note.py", "priv_" + "key = signing_key", False)
    add("r8-raw", "note.py", "priv_" + "key = raw", False)
    add("r8-default-name", "note.py", "PRIV_" + "KEY = PRIV_" + "KEY_DEFAULT", False)
    add("r8-string-annotation", "note.py", "def f(" + "priv_" + 'key: "PrivateKey"):', False)
    add(
        "r8-digest-fromhex",
        "note.py",
        "digest = bytes.from" + 'hex("' + _hex() + '")',
        False,
    )
    add("r8-iv-bytes", "note.py", "iv = bytes([" + "0, 0, 0, 0, 1, 2, 3, 4])", False)
    add("r8-want-decode", "note.go", 'want, _ := hex.DecodeString("' + _hex() + '")', False)
    add(
        "r8-named-fromhex",
        "note.py",
        "seed = bytes.from" + 'hex("' + _hex() + '")',
        True,
    )
    add(
        "r8-openssh-vendor",
        "chain/vendor/keys/deploy_key.txt",
        _openssh(),
        True,
        True,
    )
    add("r8-dist-js", "apps/web/dist/app.js", "const s = '" + phrase + "'\n", True, True)
    add(
        "r8-dist-py",
        "services/api/dist/settings.py",
        _akia() + "\n" + phrase + "\n",
        True,
        True,
    )
    add("r1-pkcs8", "note.txt", "-----BEGIN " + "ENCRYPTED PRIVATE KEY-----\n", True)
    add("r1-pgp", "note.txt", "-----BEGIN " + "PGP PRIVATE KEY BLOCK-----\n", True)
    add("r1-slack", "note.txt", "xox" + "b-0000000000-FAKE", True)
    add("r1-key-suffix", "forced.key", "not-a-key-body", True)
    add("r2-quoted-name", "note.txt", '"' + "PRIV_" + 'KEY"', True)
    return out
