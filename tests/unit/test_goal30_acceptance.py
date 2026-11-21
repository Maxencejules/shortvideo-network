"""Goal 30 second client, matrix index, and go-test summary."""

from __future__ import annotations

import importlib.util
import json
import socket
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from types import ModuleType
from typing import Any

ROOT = Path(__file__).resolve().parents[2]


def _load(name: str, rel: str) -> ModuleType:
    spec = importlib.util.spec_from_file_location(name, ROOT / rel)
    assert spec is not None and spec.loader is not None
    module = importlib.util.module_from_spec(spec)
    sys.modules[name] = module
    spec.loader.exec_module(module)
    return module


_audit: Any = _load("goal30_audit_matrix", "scripts/acceptance/audit_matrix.py")
_go: Any = _load("goal30_go_summary", "scripts/acceptance/go_summary.py")
_client: Any = _load("goal30_second_client", "scripts/acceptance/second_client.py")
parse_matrix = _audit.parse_matrix
summarize = _audit.summarize
summarize_go = _go.summarize
exercise = _client.exercise
load_operations = _client.load_operations
source_problems = _client.source_problems

GOAL30 = {
    "REQ-PROD-001",
    "REQ-PROD-011",
    "REQ-UX-015",
    "REQ-UX-020",
    "REQ-UX-021",
    "REQ-DEV-002",
    "REQ-MOD-006",
    "REQ-QA-011",
    "REQ-LAUNCH-001",
    "REQ-SUCCESS-001",
}


def _wait(host: str, port: int) -> None:
    last = ""
    for _ in range(50):
        try:
            with socket.create_connection((host, port), timeout=0.1):
                return
        except OSError as exc:
            last = str(exc)
            time.sleep(0.02)
    raise AssertionError(last)


def test_second_client_source_is_stdlib_only() -> None:
    path = ROOT / "scripts" / "acceptance" / "second_client.py"
    text = path.read_text(encoding="utf-8")
    assert source_problems(path) == []
    for needle in (
        "sqlite",
        "psycopg",
        "subprocess",
        "/v1/",
        "/admin/",
        "ActivityPub compatibility",
    ):
        assert needle not in text


def test_second_client_calls_only_published_paths() -> None:
    operations = load_operations(ROOT)
    assert len(operations) == 12
    seen: list[tuple[str, str]] = []

    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            self._take()

        def do_POST(self) -> None:
            self._take()

        def _take(self) -> None:
            seen.append((self.command, self.path))
            body = b'{"ok":true}'
            if self.path not in {item.path() for item in operations}:
                self.send_response(404)
                self.end_headers()
                return
            self.send_response(200)
            self.send_header("content-type", "application/json")
            self.send_header("content-length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

        def log_message(self, _format: str, *_args: object) -> None:
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    raw_host, raw_port = server.server_address[:2]
    host = raw_host.decode() if isinstance(raw_host, bytes) else str(raw_host)
    port = int(raw_port)
    try:
        _wait(host, port)
        base = f"http://{host}:{port}"
        report = exercise(operations, base, base, None)
    finally:
        server.shutdown()
        thread.join(timeout=2)
    assert report.ok()
    assert not report.redirect_refused
    assert {(item.method, item.path) for item in report.calls} == {
        (item.method, item.path()) for item in operations
    }
    assert set(seen) == {(item.method, item.path()) for item in operations}


def test_second_client_refuses_a_redirect() -> None:
    class Handler(BaseHTTPRequestHandler):
        def do_GET(self) -> None:
            self._go()

        def do_POST(self) -> None:
            self._go()

        def _go(self) -> None:
            self.send_response(302)
            self.send_header("location", "/v1/dev/session")
            self.end_headers()

        def log_message(self, _format: str, *_args: object) -> None:
            return

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, daemon=True)
    thread.start()
    raw_host, raw_port = server.server_address[:2]
    host = raw_host.decode() if isinstance(raw_host, bytes) else str(raw_host)
    port = int(raw_port)
    try:
        _wait(host, port)
        base = f"http://{host}:{port}"
        report = exercise(load_operations(ROOT), base, base, None)
    finally:
        server.shutdown()
        thread.join(timeout=2)
    assert report.redirect_refused
    assert not report.ok()
    assert all(item.location == "/v1/dev/session" for item in report.calls)


def test_matrix_goal30_rows_match_the_document() -> None:
    rows = parse_matrix((ROOT / "docs" / "requirements.md").read_text(encoding="utf-8"))
    goal30 = {row["id"] for row in rows if "30" in row["goals"].split(",")}
    assert goal30 == GOAL30
    by_id = {row["id"]: row for row in rows}
    assert by_id["REQ-PROD-011"]["disposition"].startswith("TEST:")
    assert by_id["REQ-MOD-006"]["disposition"].startswith("BLOCKER:")
    assert by_id["REQ-SUCCESS-001"]["goals"] == "30"
    text = summarize(rows)
    assert text.startswith("RESULT rows=")
    assert "goal30=10" in text


def test_go_summary_counts_skip_and_fail() -> None:
    events: list[dict[str, str] | str] = [
        {"Action": "run", "Package": "example", "Test": "TestOk"},
        {"Action": "pass", "Package": "example", "Test": "TestOk"},
        {"Action": "run", "Package": "example", "Test": "TestSkip"},
        {"Action": "skip", "Package": "example", "Test": "TestSkip"},
        {"Action": "run", "Package": "example", "Test": "TestBad"},
        {"Action": "output", "Package": "example", "Test": "TestBad", "Output": "boom\n"},
        {"Action": "fail", "Package": "example", "Test": "TestBad"},
        {"Action": "fail", "Package": "example"},
        "not json",
    ]
    lines = [json.dumps(item) if isinstance(item, dict) else item for item in events]
    text = summarize_go("\n".join(lines))
    assert "RESULT passed=1 failed=1 skipped=1 packages=1 nonjson=1" in text
    assert "SKIP example TestSkip" in text
    assert "FAIL example TestBad" in text
    assert "boom" in text
