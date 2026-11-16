"""Browser flows for the invited viewer against a real processed package.

The indexer and account listeners in this test are loopback stand-ins that
speak the field names of those services. They are not svn-indexer and not
svn-api. Playback uses services/delivery.
"""

from __future__ import annotations

import importlib.util
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
from svn_delivery.catalog import Catalog
from svn_delivery.server import MediaServer
from svn_delivery.tlsutil import EphemeralCert

ROOT = Path(__file__).resolve().parents[2]
DIST = ROOT / "apps" / "web" / "dist"
DRIVE = ROOT / "apps" / "web" / "test" / "drive.mjs"
READY = (
    "(() => { const node = document.querySelector('#svn-playback');"
    " if (!node || !node.textContent) return null;"
    " let data; try { data = JSON.parse(node.textContent); } catch (err) { return null; }"
    " if (!data || data.phase !== 'ready' || data.rendered !== true) return null;"
    " if (!data.videoWidth) return null;"
    " return data; })()"
)
ROTATED = (
    "(() => { const node = document.querySelector('#svn-playback');"
    " if (!node || !node.textContent) return null;"
    " let data; try { data = JSON.parse(node.textContent); } catch (err) { return null; }"
    " if (!data || data.contentId !== 'crotated' || data.phase !== 'ready') return null;"
    " return document.querySelectorAll('video').length === 1 ? 1 : null; })()"
)
ORIGIN = (
    "(() => { const node = document.querySelector('#svn-playback');"
    " if (!node || !node.textContent) return null;"
    " let data; try { data = JSON.parse(node.textContent); } catch (err) { return null; }"
    " if (!data || !data.rendered) return null;"
    " if (!(data.hostsTried || []).includes('origin-test')) return null;"
    " return data; })()"
)
WIDTH = (
    "(() => { const video = document.querySelector('video');"
    " const page = document.documentElement.clientWidth;"
    " if (!video) return {width: 0, page};"
    " return {width: video.clientWidth, page}; })()"
)
QUALITY = (
    "(() => { const node = document.querySelector('[data-quality][aria-pressed=\"false\"]')"
    " || document.querySelector('[data-quality]');"
    " if (node) node.id = 'quality-target'; return true; })()"
)
MOTION = (
    "(() => { const feed = document.querySelector('.feed');"
    " if (document.documentElement.dataset.motion !== 'reduce' || !feed) return null;"
    " return {motion: 'reduce', scroll: getComputedStyle(feed).scrollBehavior}; })()"
)


def test_viewer_plays_processed_media_in_a_browser(tmp_path: Path) -> None:
    chrome = _chrome()
    if chrome is None:
        pytest.skip("headless browser is not installed; rendering is unverified")
    if shutil.which("node") is None or not (ROOT / "node_modules" / "esbuild").exists():
        pytest.skip("esbuild is not installed; the viewer bundle is unverified")
    encode = _encode_loader()
    root = tmp_path / "media"
    root.mkdir()
    encode(root)
    build = subprocess.run(
        ["npm", "run", "build", "-w", "apps/web"],
        cwd=ROOT,
        check=False,
        text=True,
        capture_output=True,
    )
    assert build.returncode == 0, build.stderr
    web = _Web()
    web.start()
    catalog = Catalog(root)
    catalog.record_hits = True
    catalog.note_chain("HELD")
    cert = EphemeralCert()
    origin = f"http://127.0.0.1:{web.port}"
    replica = MediaServer(catalog, "test-a", role="test", cert=cert, allow_origin=origin)
    delivery = MediaServer(catalog, cert=cert, allow_origin=origin)
    try:
        package = root / "packages" / "cportrait" / "v1"
        replica_dir = tmp_path / "replica"
        shutil.copytree(package, replica_dir)
        catalog.accept_replica("test-a", replica_dir)
        replica.start()
        delivery.start()
        web.delivery = f"https://127.0.0.1:{delivery.port}"
        catalog.authorize("cportrait", seconds=600)
        catalog.authorize("crotated", seconds=600)
        plan = tmp_path / "plan.json"
        plan.write_text(json.dumps(_plan(chrome, origin)), encoding="utf-8")
        proc = subprocess.Popen(
            ["node", str(DRIVE), str(plan)],
            cwd=ROOT,
            stdin=subprocess.PIPE,
            stdout=subprocess.PIPE,
            text=True,
        )
        assert proc.stdin is not None
        assert proc.stdout is not None
        first = _read_event(proc)
        assert first.get("event") == "pause", first
        ready = first["saved"]["ready"]
        assert isinstance(ready, dict)
        assert ready["contentId"] == "cportrait"
        assert ready["videoWidth"] > 0
        assert "portrait-cue" in str(ready["caption"])
        hits = catalog.hits()
        assert hits
        assert all(item.startswith("test-a ") for item in hits), hits
        replica.stop_listening()
        proc.stdin.write("go\n")
        proc.stdin.flush()
        done = _read_event(proc)
        assert done.get("event") == "done", done
        saved = done["saved"]
        recovered = saved["recovered"]
        assert isinstance(recovered, dict)
        assert "origin-test" in recovered["hostsTried"]
        assert recovered["rendered"] is True
        assert saved["videos"] == 1
        home = saved["home"]
        assert isinstance(home, dict)
        quality = saved["quality"]
        assert isinstance(quality, dict)
        assert quality["rendered"] is True
        texts = saved["texts"]
        assert isinstance(texts, dict)
        assert texts["expired"] is True
        assert texts["indexer"] is True
        assert texts["second"] is True
        assert texts["trust"] is True
        assert home["fixture"] is True
        assert home["withdrawn"] is True
        assert home["gates"] is True
        mobile = saved["mobile"]
        assert isinstance(mobile, dict)
        assert mobile["page"] <= 400, mobile
        assert 100 <= mobile["width"] <= mobile["page"], mobile
        desktop = saved["desktop"]
        assert isinstance(desktop, dict)
        assert desktop["page"] >= 1000
        assert 200 <= desktop["width"] <= 420
        motion = saved["motion"]
        assert isinstance(motion, dict)
        assert motion["motion"] == "reduce"
        assert motion["scroll"] == "auto"
        assert len(catalog.hits()) < 40
        print(
            json.dumps(
                {
                    "browser": done.get("browser"),
                    "pageAgent": done.get("pageAgent"),
                    "viewport": done.get("viewport"),
                    "indexer": "stand-in",
                    "account": "stand-in",
                    "delivery": "svn-delivery",
                },
                sort_keys=True,
            )
        )
        proc.kill()
        proc.wait(timeout=5)
    finally:
        replica.close()
        delivery.close()
        cert.close()
        web.close()


class _Web:
    def __init__(self) -> None:
        self.delivery = ""
        self.port = 0
        handler = self._handler()
        self.server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
        self.server.daemon_threads = True
        bound = self.server.server_address[1]
        if not isinstance(bound, int):
            raise RuntimeError("web port")
        self.port = bound
        self.thread = threading.Thread(target=self.server.serve_forever, daemon=True)

    def start(self) -> None:
        self.thread.start()

    def close(self) -> None:
        self.server.shutdown()
        self.server.server_close()

    def _handler(self) -> type[BaseHTTPRequestHandler]:
        web = self

        class Handler(BaseHTTPRequestHandler):
            protocol_version = "HTTP/1.1"

            def log_message(self, fmt: str, *args: object) -> None:
                return

            def do_GET(self) -> None:  # noqa: N802
                path = urlsplit(self.path).path
                if path == "/viewer-config.json":
                    body = json.dumps(
                        {
                            "deliveryBase": web.delivery,
                            "indexerBase": f"http://127.0.0.1:{web.port}/index",
                            "indexerBase2": "http://127.0.0.1:9",
                            "accountBase": f"http://127.0.0.1:{web.port}",
                            "contentIds": ["cportrait", "crotated", "cwithdrawn"],
                        }
                    ).encode()
                    self._send(200, body, "application/json")
                    return
                if path == "/v1/session":
                    body = (
                        b'{"error":{"code":"UNAUTHENTICATED",'
                        b'"message":"authentication is required"}}'
                    )
                    self._send(401, body, "application/json")
                    return
                if path == "/index/v1/status":
                    self._send(200, _status(True, 4), "application/json")
                    return
                if path == "/index/v1/content":
                    self._send(200, _content(), "application/json")
                    return
                if path == "/index/v1/leases":
                    self._send(200, _leases(), "application/json")
                    return
                name = "index.html" if path in {"", "/"} else path.lstrip("/")
                file = DIST / name
                if not file.is_file():
                    self._send(404, b"missing", "text/plain")
                    return
                kind = "text/javascript" if name.endswith(".js") else "text/html; charset=utf-8"
                self._send(200, file.read_bytes(), kind)

            def _send(self, status: int, body: bytes, content_type: str) -> None:
                self.send_response(status)
                self.send_header("Content-Type", content_type)
                self.send_header("Content-Length", str(len(body)))
                self.send_header("Cache-Control", "no-store")
                self.end_headers()
                self.wfile.write(body)

        return Handler


def _status(stale: bool, height: int) -> bytes:
    return json.dumps(
        {
            "chain_id": "devnet",
            "indexed_height": height,
            "stale": stale,
            "stale_reasons": ["commit_age"] if stale else [],
            "trust": {
                "model": "configured_read_gateway",
                "gateway": "loopback",
                "light_client": False,
                "note": (
                    "The configured read gateway is trusted for reads. "
                    "Comparing two unauthenticated responses is not light-client verification."
                ),
            },
        }
    ).encode()


def _content() -> bytes:
    return json.dumps(
        {
            "content": [
                {
                    "ID": "cportrait",
                    "Version": 1,
                    "Manifest": "m",
                    "PackageBytes": "1",
                    "Withdrawn": False,
                    "HistoryErased": False,
                },
                {
                    "ID": "cwithdrawn",
                    "Version": 1,
                    "Manifest": "m",
                    "PackageBytes": "1",
                    "Withdrawn": True,
                    "HistoryErased": False,
                },
            ]
        }
    ).encode()


def _leases() -> bytes:
    rows = [
        {"ID": "lease-a", "ProviderID": "p1", "ContentID": "cportrait", "Active": True},
        {"ID": "lease-b", "ProviderID": "p2", "ContentID": "cportrait", "Active": True},
        {"ID": "lease-c", "ProviderID": "p3", "ContentID": "cportrait", "Active": True},
    ]
    return json.dumps({"leases": rows}).encode()


def _plan(chrome: str, origin: str) -> dict[str, Any]:
    text_expr = (
        "(() => { const text = document.body.innerText; return {"
        " fixture: text.includes('not a live creator'),"
        " withdrawn: text.includes('Withdrawn. Playback is not requested.'),"
        " gates: text.includes('Follow is not available') && text.includes('Peer sharing is off.')"
        " }; })()"
    )
    profile_expr = (
        "(() => { const text = document.body.innerText; return {"
        " expired: text.includes('Session is missing or expired.'),"
        " indexer: text.includes('Indexer height'),"
        " second: text.includes('The second indexer could not be read.'),"
        " trust: text.includes('not light-client verification')"
        " }; })()"
    )
    return {
        "chrome": chrome,
        "steps": [
            {"do": "viewport", "width": 1280, "height": 800, "mobile": False},
            {"do": "goto", "url": origin + "/"},
            {"do": "wait", "expr": READY, "save": "ready", "ms": 25000},
            {"do": "eval", "expr": WIDTH, "save": "desktop"},
            {"do": "pause"},
            {"do": "eval", "expr": "document.querySelector('#replay').click(), true", "save": "replayed"},
            {"do": "wait", "expr": ORIGIN, "save": "recovered", "ms": 40000},
            {"do": "eval", "expr": QUALITY, "save": "marked"},
            {"do": "click", "selector": "#quality-target"},
            {"do": "wait", "expr": READY, "save": "quality", "ms": 25000},
            {
                "do": "eval",
                "expr": "document.querySelector('#feed').focus(), true",
                "save": "focused",
            },
            {"do": "key", "key": "ArrowDown", "code": "ArrowDown", "codePoint": 40},
            {"do": "wait", "expr": ROTATED, "save": "videos", "ms": 25000},
            {"do": "eval", "expr": text_expr, "save": "home"},
            {"do": "click", "selector": "nav button:nth-of-type(4)"},
            {"do": "sleep", "ms": 200},
            {"do": "eval", "expr": profile_expr, "save": "texts"},
            {"do": "viewport", "width": 390, "height": 844, "mobile": True},
            {"do": "click", "selector": "nav button:nth-of-type(1)"},
            {"do": "sleep", "ms": 300},
            {"do": "eval", "expr": WIDTH, "save": "mobile"},
            {"do": "motion", "value": "reduce"},
            {
                "do": "eval",
                "expr": "localStorage.setItem('svn.viewer.motion','reduce'), true",
                "save": "stored",
            },
            {"do": "reload"},
            {"do": "wait", "expr": MOTION, "save": "motion", "ms": 20000},
        ],
    }


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


def _encode_loader() -> Any:
    path = ROOT / "tests" / "unit" / "test_delivery.py"
    spec = importlib.util.spec_from_file_location("svn_goal20_delivery", path)
    if spec is None or spec.loader is None:
        raise RuntimeError("delivery tests are not loadable")
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    encode = getattr(module, "_encode", None)
    if not callable(encode):
        raise RuntimeError("encode helper is missing")
    return encode
