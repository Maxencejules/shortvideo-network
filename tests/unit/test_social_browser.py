"""Viewer social controls against the discovery process.

The page is the built viewer. The discovery process is cmd/svn-discovery.
It is not a Python stand-in. A passkey ceremony is not performed.
"""

from __future__ import annotations

import json
import shutil
import subprocess
import threading
import urllib.request
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

import pytest
from svn_delivery.browser import find_browser

ROOT = Path(__file__).resolve().parents[2]
DIST = ROOT / "apps" / "web" / "dist"
DRIVE = ROOT / "apps" / "web" / "test" / "drive.mjs"


def test_viewer_uses_discovery(tmp_path: Path) -> None:
    chrome = find_browser()
    if chrome is None:
        pytest.skip("headless browser is not installed; rendering is unverified")
    if shutil.which("node") is None or shutil.which("go") is None:
        pytest.skip("node or go is not installed")
    binary = tmp_path / "svn-discovery"
    built = subprocess.run(
        ["go", "build", "-o", str(binary), "./cmd/svn-discovery"],
        cwd=ROOT / "services" / "api",
        check=False,
        text=True,
        capture_output=True,
    )
    assert built.returncode == 0, built.stderr
    web = subprocess.run(
        ["npm", "run", "build", "-w", "apps/web"],
        cwd=ROOT,
        check=False,
        text=True,
        capture_output=True,
    )
    assert web.returncode == 0, web.stderr
    proc = subprocess.Popen(
        [str(binary), "-dev", "-listen=127.0.0.1:0"],
        cwd=ROOT,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    assert proc.stdout is not None
    try:
        line = proc.stdout.readline().strip()
        assert line.startswith("LISTEN "), line
        base = "http://" + line.split(" ", 1)[1]
        creator = _post(base + "/v1/dev/session", {"account_id": "creator1"})
        viewer = _post(base + "/v1/dev/session", {"account_id": "viewer1"})
        _post(
            base + "/v1/catalog",
            {
                "content_id": "c-harbor",
                "title": "harbor",
                "topics": ["boats"],
                "published_at": 100000,
                "available": True,
            },
            _text(creator, "token"),
        )
        site = _Site(base)
        site.start()
        plan = tmp_path / "plan.json"
        plan.write_text(
            json.dumps(_plan(chrome, site.origin, _text(viewer, "token"))),
            encoding="utf-8",
        )
        run = subprocess.run(
            ["node", str(DRIVE), str(plan)],
            cwd=ROOT,
            check=False,
            text=True,
            capture_output=True,
        )
        assert run.returncode == 0, run.stderr + run.stdout
        saved = json.loads(run.stdout)
        assert saved["saved"]["followed"]["stored"] is True
        assert saved["saved"]["comment"]["plain"] is True
        assert saved["saved"]["comment"]["markup"] is False
        assert saved["saved"]["found"] is True
        assert saved["saved"]["moderation"]["block"] is True
        assert saved["saved"]["moderation"]["report"] is True
    finally:
        proc.terminate()
        proc.wait(timeout=5)


def _text(doc: dict[str, object], key: str) -> str:
    value = doc[key]
    if not isinstance(value, str) or value == "":
        raise RuntimeError(key)
    return value


def _post(url: str, body: dict[str, object], token: str = "") -> dict[str, object]:
    req = urllib.request.Request(url, data=json.dumps(body).encode(), method="POST")
    req.add_header("Content-Type", "application/json")
    req.add_header("Accept", "application/json")
    if token:
        req.add_header("Authorization", "Bearer " + token)
    with urllib.request.urlopen(req, timeout=5) as res:
        parsed = json.load(res)
    if not isinstance(parsed, dict):
        raise RuntimeError("response")
    return parsed


def _plan(chrome: str, origin: str, token: str) -> dict[str, object]:
    ready = (
        "document.body.innerText.includes('harbor') && "
        "document.querySelector('#follow') && !document.querySelector('#follow').disabled"
    )
    followed = (
        "(() => document.body.innerText.includes('Follow stored for this account') "
        "? { stored: true } : null)()"
    )
    send_hello = (
        "(() => { const box = document.querySelector('#comment-text'); "
        "box.value = 'hello'; document.querySelector('#comment-send').click(); return true; })()"
    )
    comment = (
        "(() => { const text = document.body.innerText; "
        "if (!text.includes('Comment stored') || !text.includes('hello')) return null; "
        "return { plain: true, markup: document.querySelector('#comments img') !== null }; })()"
    )
    send_markup = (
        "(() => { const box = document.querySelector('#comment-text'); "
        "box.value = '<img src=x>'; document.querySelector('#comment-send').click(); "
        "return true; })()"
    )
    safe = (
        "document.body.innerText.includes('Comment text is plain.') && "
        "document.querySelector('#comments img') === null"
    )
    moderation = (
        "(() => ({ block: document.querySelector('[aria-describedby=\"gate-block\"]').disabled, "
        "report: document.querySelector('[aria-describedby=\"gate-report\"]').disabled }))()"
    )
    discover = (
        "(() => { const button = [...document.querySelectorAll('nav button')]"
        ".find((item) => item.textContent === 'Discover'); button.click(); return true; })()"
    )
    query = (
        "(() => { const box = document.querySelector('#search-query'); "
        "box.value = 'harbor'; document.querySelector('#search-go').click(); return true; })()"
    )
    found = (
        "(() => { const node = document.querySelector('#search-hits'); "
        "if (!node || !node.innerText.includes('harbor')) return null; return true; })()"
    )
    return {
        "chrome": chrome,
        "steps": [
            {"do": "viewport", "width": 1280, "height": 800, "mobile": False},
            {"do": "goto", "url": origin + "/"},
            {
                "do": "eval",
                "expr": f"sessionStorage.setItem('svn.viewer.bearer', {json.dumps(token)}), true",
                "save": "token",
            },
            {"do": "reload"},
            {"do": "wait", "expr": ready, "save": "ready", "ms": 20000},
            {"do": "click", "selector": "#follow"},
            {"do": "wait", "expr": followed, "save": "followed", "ms": 20000},
            {"do": "eval", "expr": send_hello, "save": "sent"},
            {"do": "wait", "expr": comment, "save": "comment", "ms": 20000},
            {"do": "eval", "expr": send_markup, "save": "refused"},
            {"do": "wait", "expr": safe, "save": "safe", "ms": 20000},
            {"do": "eval", "expr": moderation, "save": "moderation"},
            {"do": "eval", "expr": discover, "save": "discover"},
            {"do": "eval", "expr": query, "save": "query"},
            {"do": "wait", "expr": found, "save": "found", "ms": 20000},
        ],
    }


class _Site:
    def __init__(self, discovery: str) -> None:
        self.discovery = discovery
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), self._handler())
        self.server.daemon_threads = True
        bound = self.server.server_address[1]
        if not isinstance(bound, int):
            raise RuntimeError("port")
        self.origin = f"http://127.0.0.1:{bound}"
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def start(self) -> None:
        self.thread.start()

    def _handler(self) -> type[BaseHTTPRequestHandler]:
        site = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, fmt: str, *args: object) -> None:
                return

            def do_GET(self) -> None:  # noqa: N802
                path = urlsplit(self.path).path
                if path == "/viewer-config.json":
                    body = json.dumps({"discoveryBase": site.discovery, "contentIds": []}).encode()
                    self._send(200, body, "application/json")
                    return
                name = "index.html" if path in {"", "/"} else path.lstrip("/")
                file = (DIST / name).resolve()
                if not str(file).startswith(str(DIST.resolve())) or not file.is_file():
                    self._send(404, b"missing", "text/plain")
                    return
                kind = "text/javascript" if name.endswith(".js") else "text/html; charset=utf-8"
                self._send(200, file.read_bytes(), kind)

            def _send(self, status: int, body: bytes, kind: str) -> None:
                self.send_response(status)
                self.send_header("Content-Type", kind)
                self.send_header("Content-Length", str(len(body)))
                self.end_headers()
                self.wfile.write(body)

        return Handler
