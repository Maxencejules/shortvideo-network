"""Creator page against a loopback stand-in.

The stand-in speaks the upload, session, indexer, and scheduler field names.
It is not svn-api, not svn-indexer, and not svn-scheduler. A passkey ceremony
is not performed.
"""

from __future__ import annotations

import json
import shutil
import subprocess
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from typing import Any
from urllib.parse import urlsplit

import pytest
from svn_delivery.browser import find_browser

ROOT = Path(__file__).resolve().parents[2]
DIST = ROOT / "apps" / "web" / "dist"
DRIVE = ROOT / "apps" / "web" / "test" / "drive.mjs"
UPLOAD = "u" + "1" * 32
CONTENT = "c" + "2" * 32


def test_creator_page_shows_lifecycle_states(tmp_path: Path) -> None:
    chrome = _chrome()
    if chrome is None:
        pytest.skip("headless browser is not installed; rendering is unverified")
    if shutil.which("node") is None or not (ROOT / "node_modules" / "esbuild").exists():
        pytest.skip("esbuild is not installed; the creator bundle is unverified")
    clip = tmp_path / "clip.mp4"
    clip.write_bytes(b"\x00" * 32)
    build = subprocess.run(
        ["npm", "run", "build", "-w", "apps/web"],
        cwd=ROOT,
        check=False,
        text=True,
        capture_output=True,
    )
    assert build.returncode == 0, build.stderr
    web = _StandIn()
    web.start()
    origin = f"http://127.0.0.1:{web.port}"
    plan = tmp_path / "plan.json"
    plan.write_text(json.dumps(_plan(chrome, origin, str(clip))), encoding="utf-8")
    proc = subprocess.Popen(
        ["node", str(DRIVE), str(plan)],
        cwd=ROOT,
        stdin=subprocess.PIPE,
        stdout=subprocess.PIPE,
        text=True,
    )
    assert proc.stdin is not None
    try:
        illegal = _stopped(proc)
        assert illegal["saved"]["trim"]["code"] == "TRIM_RANGE"
        assert web.intents == 0
        _resume(proc)
        uploaded = _stopped(proc)
        assert uploaded["saved"]["processing"]["stage"] == "Processing"
        assert uploaded["saved"]["processing"]["published"] is False
        assert web.intents == 1
        assert web.puts == 1
        assert web.completions == 1
        assert len(web.bodies) == 1
        _resume(proc)
        repeated = _stopped(proc)
        assert repeated["saved"]["again"] is True
        assert web.intents == 2
        assert web.puts == 1
        web.job_status = "READY_FOR_REVIEW"
        web.publication = "WITHHELD"
        web.reason = "community_approval"
        web.approved = False
        _resume(proc)
        review = _stopped(proc)
        assert review["saved"]["review"]["stage"] == "Review pending"
        assert review["saved"]["review"]["published"] is False
        assert any("Goal 23" in item for item in review["saved"]["review"]["notices"])
        web.approved = True
        web.reason = "verified_transfer"
        _resume(proc)
        replicating = _stopped(proc)
        assert replicating["saved"]["replicating"]["stage"] == "Replicating"
        web.publication = "PUBLISHED"
        web.reason = ""
        web.approved = True
        web.stale = False
        web.at_risk = False
        web.renewal = "NONE"
        _resume(proc)
        published = _stopped(proc)
        assert published["saved"]["published"]["stage"] == "Published"
        assert published["saved"]["published"]["published"] is True
        assert any(
            "lawful or truthful" in item for item in published["saved"]["published"]["notices"]
        )
        web.stale = True
        web.at_risk = True
        web.renewal = "RENEWAL_NEEDED"
        web.publication = "QUEUED_NOT_COMMITTED"
        _resume(proc)
        stalled = _stopped(proc)
        notes = " ".join(stalled["saved"]["stalled"]["notices"])
        assert stalled["saved"]["stalled"]["published"] is False
        assert "stalled" in notes
        assert "Availability degraded" in notes
        assert "Hosting renewal needed" in notes
        web.account = "creator-b"
        _resume(proc)
        other = _stopped(proc)
        assert other["saved"]["other"]["disabled"] is True
        assert web.deletes == 0
        web.account = "creator-a"
        web.withdrawn = True
        web.provider = "host-a"
        _resume(proc)
        removed = _stopped(proc)
        removed_notes = " ".join(removed["saved"]["removed"]["notices"])
        assert removed["saved"]["removed"]["code"] == "REMOVAL_ACCEPTED"
        assert "not universal deletion" in removed_notes
        assert "host-a" in removed_notes
        assert web.removal_posts == 1
        _resume(proc)
        blocked = _stopped(proc)
        assert blocked["saved"]["blocked"]["code"] == "PUBLICATION_BLOCKED"
        assert blocked["saved"]["blocked"]["published"] is False
        _resume(proc)
        done = _read_event(proc)
        assert done.get("event") == "done", done
        assert done["saved"]["cancelled"]["stage"] == "Cancelled"
        assert web.deletes == 1
        assert web.bodies == {}
        print(
            json.dumps(
                {
                    "browser": done.get("browser"),
                    "pageAgent": done.get("pageAgent"),
                    "viewport": done.get("viewport"),
                    "account": "stand-in",
                    "indexer": "stand-in",
                    "scheduler": "stand-in",
                    "upload": "stand-in",
                    "intents": web.intents,
                    "puts": web.puts,
                    "stage": done["saved"]["cancelled"]["stage"],
                },
                sort_keys=True,
            )
        )
    finally:
        proc.kill()
        proc.wait(timeout=5)
        web.close()


class _StandIn:
    def __init__(self) -> None:
        self.account = "creator-a"
        self.publication = ""
        self.reason = ""
        self.approved = False
        self.stale = False
        self.at_risk = False
        self.renewal = "NONE"
        self.job_status = "QUEUED"
        self.status = "RESERVED"
        self.withdrawn = False
        self.provider = ""
        self.intents = 0
        self.puts = 0
        self.completions = 0
        self.deletes = 0
        self.removal_posts = 0
        self.bodies: dict[str, bytes] = {}
        self.fail_intent = ""
        self.port = 0
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), self._handler())
        self.server.daemon_threads = True
        bound = self.server.server_address[1]
        if not isinstance(bound, int):
            raise RuntimeError("port")
        self.port = bound
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)
        self.lock = threading.Lock()

    def start(self) -> None:
        self.thread.start()

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()

    def _handler(self) -> type[BaseHTTPRequestHandler]:
        standin = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, fmt: str, *args: object) -> None:
                return

            def do_GET(self) -> None:  # noqa: N802
                self._route("GET")

            def do_POST(self) -> None:  # noqa: N802
                self._route("POST")

            def do_PUT(self) -> None:  # noqa: N802
                self._route("PUT")

            def do_DELETE(self) -> None:  # noqa: N802
                self._route("DELETE")

            def _route(self, method: str) -> None:
                path = urlsplit(self.path).path
                query = urlsplit(self.path).query
                with standin.lock:
                    if path == "/viewer-config.json" and method == "GET":
                        body = {
                            "deliveryBase": "",
                            "indexerBase": f"http://127.0.0.1:{standin.port}/index",
                            "indexerBase2": "",
                            "accountBase": f"http://127.0.0.1:{standin.port}",
                            "schedulerBase": f"http://127.0.0.1:{standin.port}/sched",
                            "contentIds": [],
                        }
                        self._send(200, json.dumps(body).encode(), "application/json")
                        return
                    if path == "/v1/session" and method == "GET":
                        body = {
                            "account_id": standin.account,
                            "role": "creator",
                            "status": "active",
                        }
                        self._send(200, json.dumps(body).encode(), "application/json")
                        return
                    if path == "/index/v1/status" and method == "GET":
                        self._send(200, _status(standin.stale), "application/json")
                        return
                    if path == "/index/v1/content" and method == "GET":
                        self._send(200, _content(standin.withdrawn), "application/json")
                        return
                    if path == "/index/v1/leases" and method == "GET":
                        self._send(200, _leases(standin.provider), "application/json")
                        return
                    if path == "/upload-intents" and method == "POST":
                        self._intent()
                        return
                    if path == f"/uploads/{UPLOAD}/content" and method == "PUT":
                        standin.puts += 1
                        length = int(self.headers.get("Content-Length", "0"))
                        standin.bodies[UPLOAD] = self.rfile.read(length)
                        standin.status = "RECEIVED"
                        self._send(204, b"", "application/json")
                        return
                    if path == f"/uploads/{UPLOAD}/complete" and method == "POST":
                        standin.completions += 1
                        standin.status = "PROCESSING_QUEUED"
                        self._send(202, _upload(standin), "application/json")
                        return
                    if path == f"/uploads/{UPLOAD}" and method == "GET":
                        self._send(200, _upload(standin), "application/json")
                        return
                    if path == f"/uploads/{UPLOAD}" and method == "DELETE":
                        standin.deletes += 1
                        standin.bodies.pop(UPLOAD, None)
                        standin.status = "CANCELLED"
                        self._send(200, _upload(standin), "application/json")
                        return
                    if path == "/publish" and method == "POST":
                        self._send(403, _error("PUBLICATION_BLOCKED"), "application/json")
                        return
                    if path == "/removal" and method == "POST":
                        length = int(self.headers.get("Content-Length", "0"))
                        self.rfile.read(length)
                        standin.removal_posts += 1
                        self._send(
                            202,
                            json.dumps(
                                {
                                    "universal_deletion": False,
                                    "ledger_retained": True,
                                    "legal_approval": False,
                                    "waited_for_finality": False,
                                }
                            ).encode(),
                            "application/json",
                        )
                        return
                    if (
                        path == "/sched/v1/availability"
                        and method == "GET"
                        and f"content={CONTENT}" in query
                    ):
                        self._send(200, _availability(standin), "application/json")
                        return
                    name = "index.html" if path in {"", "/"} else path.lstrip("/")
                    file = DIST / name
                    if method == "GET" and file.is_file():
                        kind = (
                            "text/javascript"
                            if name.endswith(".js")
                            else "text/html; charset=utf-8"
                        )
                        self._send(200, file.read_bytes(), kind)
                        return
                    self._send(404, _error("NOT_FOUND"), "application/json")

            def _intent(self) -> None:
                length = int(self.headers.get("Content-Length", "0"))
                raw = self.rfile.read(length)
                try:
                    payload = json.loads(raw)
                except json.JSONDecodeError:
                    self._send(400, _error("MALFORMED"), "application/json")
                    return
                if not isinstance(payload, dict):
                    self._send(400, _error("MALFORMED"), "application/json")
                    return
                standin.intents += 1
                if standin.fail_intent:
                    self._send(403, _error(standin.fail_intent), "application/json")
                    return
                if standin.status == "RESERVED":
                    standin.status = "RESERVED"
                body = {
                    "upload_id": UPLOAD,
                    "content_id": CONTENT,
                    "status": standin.status if standin.status != "RECEIVED" else "RESERVED",
                    "limits_label": "PROPOSED",
                    "limits": {
                        "label": "PROPOSED",
                        "max_input_bytes": 100_000_000,
                        "max_duration_seconds": 60,
                    },
                }
                if standin.status in {"RESERVED", "RECEIVED"}:
                    body["transfer"] = {
                        "scheme": "Transfer",
                        "token": "transfer-token",
                        "expires_at": "2026-11-17T00:00:00Z",
                        "method": "PUT",
                        "path": f"/uploads/{UPLOAD}/content",
                    }
                self._send(202, json.dumps(body).encode(), "application/json")

            def _send(self, status: int, body: bytes, content_type: str) -> None:
                self.send_response(status)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(body)))
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                if body:
                    self.wfile.write(body)

        return Handler


def _upload(standin: _StandIn) -> bytes:
    return json.dumps(
        {
            "upload_id": UPLOAD,
            "content_id": CONTENT,
            "status": standin.status,
            "declared_bytes": 32,
            "received_bytes": 32 if standin.puts else 0,
            "job_status": standin.job_status,
            "publication": "NOT_PUBLISHED",
            "limits_label": "PROPOSED",
        }
    ).encode()


def _availability(standin: _StandIn) -> bytes:
    return json.dumps(
        {
            "content_id": CONTENT,
            "publication": standin.publication,
            "publication_reason": standin.reason,
            "community_approval": standin.approved,
            "reachability": "REACHABILITY_DEGRADED" if standin.at_risk else "REACHABLE",
            "at_risk": standin.at_risk,
            "renewal": standin.renewal,
            "degraded_providers": ["host-a"] if standin.at_risk else [],
            "simulated_labels": True,
            "independent_operator_verification": False,
            "storage_proof": False,
            "active_commitments": 1 if standin.approved else 0,
            "verified_transfers": 1 if standin.approved else 0,
        }
    ).encode()


def _status(stale: bool) -> bytes:
    return json.dumps(
        {
            "chain_id": "devnet",
            "indexed_height": 4,
            "stale": stale,
            "trust": {"light_client": False, "note": "stand-in"},
        }
    ).encode()


def _content(withdrawn: bool) -> bytes:
    return json.dumps(
        {
            "content": [
                {
                    "ID": CONTENT,
                    "Version": 1,
                    "Manifest": "m",
                    "PackageBytes": "32",
                    "Withdrawn": withdrawn,
                    "HistoryErased": False,
                }
            ]
        }
    ).encode()


def _leases(provider: str) -> bytes:
    rows = []
    if provider:
        rows.append({"ID": "lease-a", "ProviderID": provider, "ContentID": CONTENT, "Active": True})
    return json.dumps({"leases": rows}).encode()


def _error(code: str) -> bytes:
    return json.dumps({"error": {"code": code, "message": code}}).encode()


def _snapshot(stage: str = "", code: str = "") -> str:
    checks = ["data"]
    if stage:
        checks.append(f"data.stage === {json.dumps(stage)}")
    if code:
        checks.append(f"data.code === {json.dumps(code)}")
    test = " && ".join(checks)
    return (
        "(() => { const node = document.querySelector('#svn-creator');"
        " if (!node || !node.textContent) return null;"
        " let data; try { data = JSON.parse(node.textContent); } catch (err) { return null; }"
        f" if (!({test})) return null; return data; }})()"
    )


def _set_trim(start: str, end: str) -> str:
    return (
        "(() => { for (const [name, value] of "
        + json.dumps([["trimStart", start], ["trimEnd", end]])
        + ") { const input = document.querySelector(`[name=${name}]`);"
        " if (!input) return false;"
        " const set = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), 'value').set;"
        " set.call(input, value);"
        " input.dispatchEvent(new Event('input', { bubbles: true })); } return true; })()"
    )


def _plan(chrome: str, origin: str, clip: str) -> dict[str, Any]:
    enabled = (
        "(() => { const node = document.querySelector('#creator-upload');"
        " if (!node || node.disabled) return null; return true; })()"
    )
    nav = "(() => document.querySelector('nav button:nth-of-type(5)') ? true : null)()"
    again = "(() => document.body.innerText.includes('existing upload') ? true : null)()"
    degraded = "(() => document.body.innerText.includes('Availability degraded') ? true : null)()"
    locked = (
        "(() => { const node = document.querySelector('#creator-cancel');"
        " if (!node || !node.disabled) return null; return {disabled: true}; })()"
    )
    return {
        "chrome": chrome,
        "steps": [
            {"do": "goto", "url": origin + "/"},
            {
                "do": "eval",
                "expr": "sessionStorage.setItem('svn.viewer.bearer','dev-token'), true",
                "save": "bearer",
            },
            {"do": "reload"},
            {
                "do": "wait",
                "expr": nav,
                "save": "nav",
                "ms": 20000,
            },
            {"do": "click", "selector": "nav button:nth-of-type(5)"},
            {"do": "wait", "expr": enabled, "save": "ready", "ms": 20000},
            {"do": "eval", "expr": _set_trim("-1", "2"), "save": "trimSet"},
            {"do": "eval", "expr": _press("#creator-upload"), "save": "clickedTrim"},
            {"do": "wait", "expr": _snapshot(code="TRIM_RANGE"), "save": "trim", "ms": 10000},
            {"do": "pause"},
            {"do": "eval", "expr": _set_trim("0", "1"), "save": "range"},
            {"do": "file", "selector": "#video-file", "path": clip},
            {
                "do": "wait",
                "expr": "(() => document.body.innerText.includes('Selected file') ? true : null)()",
                "save": "selected",
                "ms": 10000,
            },
            {"do": "eval", "expr": _press("#creator-upload"), "save": "clickedUpload"},
            {
                "do": "wait",
                "expr": _snapshot(stage="Processing"),
                "save": "processing",
                "ms": 20000,
            },
            {"do": "pause"},
            {"do": "sleep", "ms": 500},
            {"do": "eval", "expr": _press("#creator-upload"), "save": "clickedAgain"},
            {
                "do": "wait",
                "expr": again,
                "save": "again",
                "ms": 20000,
            },
            {"do": "pause"},
            {"do": "eval", "expr": _press("#creator-refresh"), "save": "refreshed"},
            {
                "do": "wait",
                "expr": _snapshot(stage="Review pending"),
                "save": "review",
                "ms": 20000,
            },
            {"do": "pause"},
            {"do": "eval", "expr": _press("#creator-refresh"), "save": "refreshed"},
            {
                "do": "wait",
                "expr": _snapshot(stage="Replicating"),
                "save": "replicating",
                "ms": 20000,
            },
            {"do": "pause"},
            {"do": "eval", "expr": _press("#creator-refresh"), "save": "refreshed"},
            {"do": "wait", "expr": _snapshot(stage="Published"), "save": "published", "ms": 20000},
            {"do": "pause"},
            {"do": "eval", "expr": _press("#creator-refresh"), "save": "refreshed"},
            {"do": "wait", "expr": degraded, "save": "sawDegraded", "ms": 20000},
            {
                "do": "eval",
                "expr": _snapshot(),
                "save": "stalled",
            },
            {"do": "pause"},
            {"do": "eval", "expr": _press("#creator-refresh"), "save": "refreshed"},
            {
                "do": "wait",
                "expr": locked,
                "save": "other",
                "ms": 10000,
            },
            {"do": "pause"},
            {"do": "eval", "expr": _press("#creator-refresh"), "save": "refreshed"},
            {"do": "wait", "expr": enabled, "save": "ownerAgain", "ms": 10000},
            {"do": "eval", "expr": _set_confirm("withdraw"), "save": "confirmRemove"},
            {"do": "eval", "expr": _press("#creator-remove"), "save": "clickedRemove"},
            {
                "do": "wait",
                "expr": _snapshot(code="REMOVAL_ACCEPTED"),
                "save": "removed",
                "ms": 10000,
            },
            {"do": "pause"},
            {"do": "eval", "expr": _set_confirm("publish"), "save": "confirmPublish"},
            {"do": "eval", "expr": _press("#creator-publish"), "save": "clickedPublish"},
            {
                "do": "wait",
                "expr": _snapshot(code="PUBLICATION_BLOCKED"),
                "save": "blocked",
                "ms": 10000,
            },
            {"do": "pause"},
            {"do": "eval", "expr": _press("#creator-refresh"), "save": "refreshed"},
            {"do": "sleep", "ms": 400},
            {"do": "eval", "expr": _set_confirm("cancel"), "save": "confirmCancel"},
            {"do": "eval", "expr": _press("#creator-cancel"), "save": "clickedCancel"},
            {"do": "wait", "expr": _snapshot(stage="Cancelled"), "save": "cancelled", "ms": 10000},
        ],
    }


def _press(selector: str) -> str:
    return f"document.querySelector({json.dumps(selector)}).click(), true"


def _set_confirm(word: str) -> str:
    return (
        "(() => { const input = document.querySelector('[name=confirm]');"
        " if (!input) return false;"
        " const set = Object.getOwnPropertyDescriptor(Object.getPrototypeOf(input), 'value').set;"
        f" set.call(input, {json.dumps(word)});"
        " input.dispatchEvent(new Event('input', { bubbles: true })); return true; })()"
    )


def _stopped(proc: subprocess.Popen[str]) -> dict[str, Any]:
    event = _read_event(proc)
    assert event.get("event") == "pause", event
    return event


def _resume(proc: subprocess.Popen[str]) -> None:
    assert proc.stdin is not None
    proc.stdin.write("go\n")
    proc.stdin.flush()


def _read_event(proc: subprocess.Popen[str]) -> dict[str, Any]:
    assert proc.stdout is not None
    line = proc.stdout.readline()
    if not line:
        raise AssertionError("browser driver produced no event")
    parsed = json.loads(line)
    if not isinstance(parsed, dict):
        raise AssertionError(line)
    return parsed


def _chrome() -> str | None:
    found = find_browser()
    if found:
        return found
    candidate = Path("/opt/google/chrome/chrome")
    if candidate.is_file():
        return str(candidate)
    return None
