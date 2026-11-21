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
index_tests = _audit.index_tests
resolve_row = _audit.resolve_row
render = _audit.render
load_logs = _audit.load_logs
index_events = _go.index_events
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


def _row(row_id: str, scope: str, disposition: str, goals: str = "30") -> dict[str, str]:
    return {
        "id": row_id,
        "scope": scope,
        "pages": "1",
        "role": "product/application",
        "goals": goals,
        "disposition": disposition,
        "summary": "fixture",
    }


def _fixture_tree(root: Path) -> None:
    go_dir = root / "pkg"
    go_dir.mkdir()
    (go_dir / "alpha_test.go").write_text(
        "package pkg\n\n// TestAlpha cites REQ-AAA-001.\nfunc TestAlpha(t *testing.T) {}\n\n"
        "func TestGamma(t *testing.T) {}\n\nfunc TestDelta(t *testing.T) {}\n",
        encoding="utf-8",
    )
    py_dir = root / "tests" / "unit"
    py_dir.mkdir(parents=True)
    (py_dir / "test_beta.py").write_text(
        '"""REQ-AAA-002 is this file."""\n\ndef test_beta() -> None:\n    pass\n',
        encoding="utf-8",
    )
    web = root / "web"
    web.mkdir()
    (web / "labels.test.ts").write_text(
        'test("covers REQ-NODE-001", () => {\n});\n',
        encoding="utf-8",
    )


def test_resolver_maps_a_comment_a_name_and_a_log(tmp_path: Path) -> None:
    _fixture_tree(tmp_path)
    index = index_tests(tmp_path)
    rows = [
        _row("REQ-AAA-001", "IN_SCOPE", "TEST: the alpha check covers the record."),
        _row(
            "REQ-AAA-002",
            "IN_SCOPE",
            "TEST: tests/unit/test_beta.py runs test_beta.",
        ),
        _row("REQ-NODE-001", "IN_SCOPE", "TEST: the label check quotes the limitation."),
        _row("REQ-AAA-008", "IN_SCOPE", "BLOCKER:DEC-008 stays open."),
        _row("REQ-AAA-009", "DEFERRED", "OUT: later.", goals="none"),
    ]
    log = tmp_path / "log.txt"
    log.write_text(
        "HEAD abc\n"
        "PASS github.com/Maxencejules/shortvideo-network/pkg TestAlpha\n"
        "tests/unit/test_beta.py::test_beta PASSED\n"
        "ok 1 - covers REQ-NODE-001\n",
        encoding="utf-8",
    )
    obs, notes = load_logs([log], "")
    text = render(rows, index, obs, notes)
    assert "resolved=3 unresolved=0 pass=3 fail=0 skip=0 unrun=0 mixed=0" in text
    assert "BLOCKER REQ-AAA-008" in text
    assert "OUT REQ-AAA-009 DEFERRED" in text
    assert "UNRESOLVED" not in text
    alpha, problems = resolve_row(rows[0]["id"], rows[0]["disposition"], index)
    assert problems == ()
    assert alpha[0].path == "pkg/alpha_test.go"
    assert alpha[0].function == "TestAlpha"
    beta, beta_problems = resolve_row(rows[1]["id"], rows[1]["disposition"], index)
    assert beta_problems == ()
    assert [item.function for item in beta] == ["test_beta"]


def test_resolver_lists_missing_unrun_skip_fail_and_mixed(tmp_path: Path) -> None:
    _fixture_tree(tmp_path)
    index = index_tests(tmp_path)
    rows = [
        _row("REQ-AAA-003", "IN_SCOPE", "TEST: nothing in the tree cites this sentence."),
        _row("REQ-AAA-004", "IN_SCOPE", "TEST: TestMissing is the assigned function."),
        _row("REQ-AAA-005", "IN_SCOPE", "TEST: TestGamma is the assigned function."),
        _row("REQ-AAA-006", "IN_SCOPE", "TEST: TestAlpha then TestGamma."),
        _row("REQ-AAA-007", "IN_SCOPE", "TEST: TestDelta is the assigned function."),
    ]
    log = tmp_path / "log.txt"
    log.write_text(
        "PASS github.com/Maxencejules/shortvideo-network/pkg TestAlpha\n"
        "SKIP github.com/Maxencejules/shortvideo-network/pkg TestGamma\n"
        "FAIL github.com/Maxencejules/shortvideo-network/pkg TestDelta\n",
        encoding="utf-8",
    )
    obs, notes = load_logs([log], "")
    text = render(rows, index, obs, notes)
    assert "UNRESOLVED REQ-AAA-003 no test function" in text
    assert "UNRESOLVED REQ-AAA-004 missing function TestMissing" in text
    assert "SKIP REQ-AAA-005 pkg/alpha_test.go:6 TestGamma" in text
    assert "MIXED REQ-AAA-006 " in text
    assert "FAIL REQ-AAA-007 pkg/alpha_test.go:8 TestDelta" in text
    assert "unresolved=2" in text
    assert "fail=1" in text
    assert "skip=1" in text
    assert "mixed=1" in text
    assert " pass=0 " in text


def test_resolver_ignores_a_log_from_another_head(tmp_path: Path) -> None:
    _fixture_tree(tmp_path)
    index = index_tests(tmp_path)
    rows = [_row("REQ-AAA-001", "IN_SCOPE", "TEST: the alpha check covers the record.")]
    log = tmp_path / "other.txt"
    log.write_text(
        "HEAD old\nPASS github.com/Maxencejules/shortvideo-network/pkg TestAlpha\n",
        encoding="utf-8",
    )
    obs, notes = load_logs([log], "new")
    text = render(rows, index, obs, notes)
    assert "ignored" in notes[0]
    assert "UNRUN REQ-AAA-001 pkg/alpha_test.go:4 TestAlpha" in text
    assert " pass=0 " in text


def test_go_index_lists_passes_as_well_as_skips() -> None:
    events = [
        {"Action": "pass", "Package": "example", "Test": "TestOk"},
        {"Action": "pass", "Package": "example"},
        {"Action": "skip", "Package": "example", "Test": "TestSkip"},
        {"Action": "output", "Package": "example", "Test": "TestOk", "Output": "noise"},
    ]
    text = "\n".join(index_events(json.dumps(item) for item in events))
    assert text.splitlines() == ["PASS example TestOk", "SKIP example TestSkip"]
    kept = index_events(["MODULE chain", json.dumps(events[0])])
    assert kept == ["MODULE chain", "PASS example TestOk"]


def test_real_matrix_resolver_balances_and_names_functions() -> None:
    rows = parse_matrix((ROOT / "docs" / "requirements.md").read_text(encoding="utf-8"))
    index = index_tests(ROOT)
    text = render(rows, index, load_logs([], "")[0], ["LOG none"])
    first = text.splitlines()[0]
    assert first.startswith(
        "RESULT rows=265 in_scope=250 test=216 blocker=34 out=15 goal30=10 "
    )
    fields = dict(part.split("=", 1) for part in first.split() if "=" in part)
    assert int(fields["resolved"]) + int(fields["unresolved"]) == int(fields["test"])
    verdicts = ("pass", "fail", "skip", "unrun", "mixed")
    assert sum(int(fields[name]) for name in verdicts) == int(fields["resolved"])
    by_id = {row["id"]: row for row in rows}
    linked, problems = resolve_row(
        "REQ-ACK-001", by_id["REQ-ACK-001"]["disposition"], index
    )
    assert problems == ()
    assert {item.function for item in linked} >= {
        "TestAckNeverCommits",
        "TestDevnetBroadcasterLifecycle",
    }
    scope_id = "REQ-SCOPE-" + "002"
    scope, scope_problems = resolve_row(scope_id, by_id[scope_id]["disposition"], index)
    assert scope == ()
    assert scope_problems == ("no test function",)
    assert f"UNRESOLVED {scope_id} no test function" in text
    assert "BLOCKER REQ-MOD-006" in text
