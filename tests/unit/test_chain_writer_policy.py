"""Only the broadcaster and a few named paths write to the chain (REQ-INT-001).

REQ-INT-001 asks that goal 11 be the only chain writer the API uses and that the
web client not subscribe to every block. The API (goal 13) and the web client
(goal 21) are scaffolds today, so this static policy is the goal 11 part of the
requirement, not its verification. It fails when:

- a file under services/api, services/indexer, workers, apps/web or packages
  names a signing-key loader, a transaction broadcast, SignWithPrivKey, or a
  cosmos.tx.v1beta1 transaction builder, or, under apps/web, a CometBFT
  websocket endpoint or event subscription;
- a Go file outside these paths contains SignWithPrivKey, a BroadcastTx
  identifier or a broadcast_tx_* JSON-RPC method name in its code (comments
  are ignored): the files directly in services/broadcaster/internal/signer/ and
  services/broadcaster/internal/chain/, agents/auditor/chainclient/tx.go and
  rpc.go (the auditor's own AUDITOR path, goal 09 Q1), its chaintest helpers
  chaintest.go and serve.go, agents/provider/internal/sign/tx.go (the provider
  signs its own acceptance transaction, goal 16), chain/internal/statecheck/testhome/testhome.go
  (a test-only package), and any _test.go file.

The scan covers tracked and untracked files that git does not ignore, so a new
file is checked before it is committed. It is a text scan: it finds names, not
behaviour, and a writer that avoids every listed name passes it. The planted
files below are fixtures, not repository files.
"""

from __future__ import annotations

import re
import subprocess
from collections.abc import Mapping
from pathlib import Path

import pytest

ROOT = Path(__file__).resolve().parents[2]

WRITER_DIRS = (
    "services/broadcaster/internal/signer/",
    "services/broadcaster/internal/chain/",
)
WRITER_FILES = frozenset(
    {
        "agents/auditor/chainclient/tx.go",
        "agents/auditor/chainclient/rpc.go",
        "agents/auditor/chainclient/chaintest/chaintest.go",
        "agents/auditor/chainclient/chaintest/serve.go",
        "agents/provider/internal/sign/tx.go",
        "chain/internal/statecheck/testhome/testhome.go",
        # Goal 18. The loopback chain view submits the provider's already
        # signed bytes. It does not sign.
        "services/milestone/internal/chainview/server.go",
    }
)
GO_WRITER_MARKERS = (
    ("SignWithPrivKey", re.compile(r"\bSignWithPrivKey\b")),
    ("BroadcastTx", re.compile(r"(?<![A-Za-z0-9_])BroadcastTx\w*")),
    ("broadcast_tx", re.compile(r"\bbroadcast_tx_(?:sync|async|commit)\b")),
)
CONSUMER_DIRS = ("services/api/", "services/indexer/", "workers/", "apps/web/", "packages/")
CONSUMER_SUFFIXES = frozenset(
    {".py", ".pyi", ".ts", ".tsx", ".js", ".jsx", ".mjs", ".cjs", ".mts", ".cts", ".go"}
)
# Names are joined from pieces so that this file has no contiguous key name.
_PRIV = "priv" + r"(?:ate)?"
CONSUMER_MARKERS = (
    ("transaction broadcast", re.compile(r"(?i)broadcast_?tx")),
    ("SignWithPrivKey", re.compile(r"SignWith" + "Priv" + "Key")),
    (
        "signing-key loader",
        re.compile(
            r"(?i)" + _PRIV + r"[_-]?key|signing_?key|mnemonic|keyring|"
            r"load_(?:pem|der|ssh)_" + _PRIV
        ),
    ),
    (
        "cosmos.tx.v1beta1 builder",
        re.compile(
            r"cosmos[./_]tx[./_]v1beta1|\bTxRaw\b|\bSignDoc\b|\bAuthInfo\b|\bTxBody\b"
            r"|@cosmjs/|\bcosmpy\b|SigningStargateClient|\bmakeSignDoc\b"
        ),
    ),
)
WEB_MARKERS = (
    (
        "CometBFT websocket",
        re.compile(r"/websocket\b|:26657\b"),
    ),
    (
        "CometBFT subscription",
        re.compile(
            r"(?i)\btm\.event\b|[\"']subscribe[\"']|\bunsubscribe_all\b"
            r"|\.subscribe\s*\(\s*[\"'`]"
        ),
    ),
)


def strip_go_comments(text: str) -> str:
    """Go source with // and /* */ comments blanked; string and rune literals kept."""
    out: list[str] = []
    index = 0
    length = len(text)
    while index < length:
        char = text[index]
        pair = text[index : index + 2]
        if pair == "//":
            end = text.find("\n", index)
            index = length if end < 0 else end
            continue
        if pair == "/*":
            end = text.find("*/", index + 2)
            stop = length if end < 0 else end + 2
            out.append("\n" * text.count("\n", index, stop))
            index = stop
            continue
        if char in "\"'`":
            quote = char
            start = index
            index += 1
            while index < length and text[index] != quote:
                if quote != "`" and text[index] == "\\":
                    index += 1
                elif quote != "`" and text[index] == "\n":
                    break
                index += 1
            index += 1
            out.append(text[start:index])
            continue
        out.append(char)
        index += 1
    return "".join(out)


def is_writer_path(path: str) -> bool:
    """True for the named chain-writer paths and for every _test.go file."""
    if path.endswith("_test.go") or path in WRITER_FILES:
        return True
    return any(path.startswith(prefix) and "/" not in path[len(prefix) :] for prefix in WRITER_DIRS)


def go_writer_errors(files: Mapping[str, str]) -> list[str]:
    """A Go file outside the writer paths that signs or broadcasts transactions."""
    errors: list[str] = []
    for path, text in sorted(files.items()):
        if not path.endswith(".go") or is_writer_path(path):
            continue
        code = strip_go_comments(text)
        for name, pattern in GO_WRITER_MARKERS:
            if pattern.search(code):
                errors.append(f"{path}: chain writer outside the allowed paths ({name})")
    return errors


def _is_consumer_file(path: str) -> bool:
    if not path.startswith(CONSUMER_DIRS) or "/node_modules/" in path:
        return False
    return Path(path).suffix in CONSUMER_SUFFIXES or Path(path).name == "package.json"


def consumer_errors(files: Mapping[str, str]) -> list[str]:
    """A file under the API, indexer, workers, web or packages that writes to the chain."""
    errors: list[str] = []
    for path, text in sorted(files.items()):
        if not _is_consumer_file(path):
            continue
        markers = CONSUMER_MARKERS + (WEB_MARKERS if path.startswith("apps/web/") else ())
        for name, pattern in markers:
            if pattern.search(text):
                errors.append(f"{path}: {name}")
    return errors


def repository_files() -> dict[str, str]:
    """Tracked and not-ignored untracked files that the two checks read."""
    completed = subprocess.run(
        ["git", "ls-files", "-z", "--cached", "--others", "--exclude-standard"],
        cwd=ROOT,
        check=True,
        capture_output=True,
    )
    files: dict[str, str] = {}
    for raw in completed.stdout.split(b"\0"):
        path = raw.decode("utf-8")
        if not path or not (path.endswith(".go") or _is_consumer_file(path)):
            continue
        full = ROOT / path
        if full.is_file():
            files[path] = full.read_text(encoding="utf-8")
    return files


def test_repository_follows_the_chain_writer_policy() -> None:
    files = repository_files()
    assert "services/broadcaster/internal/chain/rpc.go" in files
    assert "agents/auditor/chainclient/tx.go" in files
    assert go_writer_errors(files) == []
    assert consumer_errors(files) == []


def test_named_writers_carry_the_markers() -> None:
    """The allowlist names files that do sign or broadcast, so it is not stale."""
    files = repository_files()
    for path in sorted(WRITER_FILES):
        code = strip_go_comments(files[path])
        assert any(pattern.search(code) for _, pattern in GO_WRITER_MARKERS), path


PLANTED = [
    pytest.param(
        "services/broadcaster/internal/worker/send.go",
        "package worker\n\nfunc send(c *rpchttp.HTTP) { c.BroadcastTxSync(ctx, raw) }\n",
        "chain writer outside the allowed paths (BroadcastTx)",
        id="new-broadcaster-package-broadcasts",
    ),
    pytest.param(
        "services/broadcaster/internal/signerx/sign.go",
        "package signerx\n\nfunc s() { tx.SignWithPrivKey(ctx, mode, data, b, k, cfg, seq) }\n",
        "(SignWithPrivKey)",
        id="sibling-directory-with-the-same-prefix",
    ),
    pytest.param(
        "services/broadcaster/internal/signer/sub/sign.go",
        "package sub\n\nfunc s() { tx.SignWithPrivKey(ctx, mode, data, b, k, cfg, seq) }\n",
        "(SignWithPrivKey)",
        id="subdirectory-of-an-allowed-directory",
    ),
    pytest.param(
        "agents/auditor/chainclient/submit.go",
        'package chainclient\n\nconst method = "broadcast_tx_sync"\n',
        "(broadcast_tx)",
        id="other-file-next-to-an-allowed-file",
    ),
    pytest.param(
        "chain/internal/statecheck/testhome/extra.go",
        "package testhome\n\nfunc s() { tx.SignWithPrivKey(ctx, mode, data, b, k, cfg, seq) }\n",
        "(SignWithPrivKey)",
        id="other-file-in-the-test-only-package",
    ),
    pytest.param(
        "chain/app/relay.go",
        "package app\n\nfunc r(c Client) { c.BroadcastTxCommit(ctx, raw) }\n",
        "(BroadcastTx)",
        id="chain-module-broadcasts",
    ),
    pytest.param(
        "services/api/svn_api/chain.py",
        'def send(client):\n    return client.call("broadcast_tx_sync", tx)\n',
        "services/api/svn_api/chain.py: transaction broadcast",
        id="api-broadcasts",
    ),
    pytest.param(
        "services/api/svn_api/chain.go",
        "package api\n\nfunc s(c Client) { c.BroadcastTxSync(ctx, raw) }\n",
        "services/api/svn_api/chain.go: chain writer outside the allowed paths",
        id="api-go-writer",
    ),
    pytest.param(
        "services/indexer/svn_indexer/tx.py",
        "from cosmos.tx.v1beta1 import tx_pb2\n",
        "cosmos.tx.v1beta1 builder",
        id="indexer-builds-transactions",
    ),
    pytest.param(
        "workers/media/svn_media/keys.py",
        "from cryptography.hazmat.primitives.serialization import load_pem_" + "private" + "_key\n",
        "signing-key loader",
        id="worker-loads-a-signing-key",
    ),
    pytest.param(
        "packages/protocol/svn_protocol/wallet.py",
        "words = derive_from_mnemonic(phrase)\n",
        "signing-key loader",
        id="package-reads-a-mnemonic",
    ),
    pytest.param(
        "apps/web/src/chain.ts",
        'const socket = new WebSocket("ws://127.0.0.1:26657/websocket");\n',
        "CometBFT websocket",
        id="web-opens-the-cometbft-websocket",
    ),
    pytest.param(
        "apps/web/src/feed.ts",
        "client.subscribe(\"tm.event='NewBlock'\");\n",
        "CometBFT subscription",
        id="web-subscribes-to-every-block",
    ),
    pytest.param(
        "apps/web/package.json",
        '{"dependencies": {"@cosmjs/stargate": "0.33.0"}}\n',
        "cosmos.tx.v1beta1 builder",
        id="web-depends-on-a-transaction-builder",
    ),
    pytest.param(
        "services/api/svn_api/sign_test.go",
        "package api\n\nfunc TestX(t *testing.T) { tx.SignWithPrivKey(ctx, m, d, b, k, c, s) }\n",
        "services/api/svn_api/sign_test.go: SignWith",
        id="api-test-file-still-checked",
    ),
]


@pytest.mark.parametrize(("path", "text", "needle"), PLANTED)
def test_planted_chain_writer_fails(path: str, text: str, needle: str) -> None:
    files = {path: text}
    errors = go_writer_errors(files) + consumer_errors(files)
    assert any(needle in error for error in errors), errors


def test_allowed_paths_comments_and_tests_pass() -> None:
    files = {
        "services/broadcaster/internal/chain/rpc.go": "func b() { c.BroadcastTxSync(ctx, raw) }\n",
        "services/broadcaster/internal/signer/signer.go": "func s() { tx.SignWithPrivKey() }\n",
        "agents/auditor/chainclient/rpc.go": "func b() { c.BroadcastTxSync(ctx, raw) }\n",
        "services/broadcaster/internal/worker/worker_test.go": (
            "func (f *fakeRPC) BroadcastTxSync(ctx context.Context) {}\n"
        ),
        "services/broadcaster/internal/worker/send.go": (
            "// send hands the bytes to chain.Client.Broadcast (broadcast_tx_sync).\n"
            "/* BroadcastTxSync is not called here. */\n"
            "func send(c *chain.Client) { c.Broadcast(ctx, raw) }\n"
            "var r *coretypes.ResultBroadcastTx\n"
        ),
        "apps/web/src/labels.ts": 'export const text = "The provider signed the terms.";\n',
    }
    assert go_writer_errors(files) == []
    assert consumer_errors(files) == []


def test_strip_go_comments_keeps_literals() -> None:
    code = strip_go_comments(
        'a := "// not a comment" // comment\nb := `/* raw */` /* block\nend */ c\n'
        "r := '\"' // rune\n"
    )
    assert '"// not a comment"' in code and "`/* raw */`" in code
    assert "comment\n" not in code and "block" not in code and "rune" not in code
    assert code.count("\n") == 4
