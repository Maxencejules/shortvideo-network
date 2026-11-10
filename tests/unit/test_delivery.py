"""HTTPS playback of a real processed package, and the failure cases around it."""

from __future__ import annotations

import hashlib
import http.client
import json
import os
import shutil
import subprocess
import sys
import threading
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path
from urllib.parse import urlsplit

import pytest
from svn_delivery import DISABLED, SCAFFOLD
from svn_delivery.browser import find_browser, run_until
from svn_delivery.catalog import Catalog
from svn_delivery.errors import DeliveryError
from svn_delivery.player import Harness
from svn_delivery.server import MediaServer
from svn_delivery.tlsutil import EphemeralCert, client_context
from svn_media import Workspace

ROOT = Path(__file__).resolve().parents[2]


def _ffmpeg(args: list[str]) -> None:
    proc = subprocess.run(
        ["ffmpeg", "-y", "-hide_banner", "-loglevel", "error", *args],
        capture_output=True,
        check=False,
    )
    if proc.returncode != 0:
        raise AssertionError(proc.stderr.decode("utf-8", "replace"))


def make_clip(
    path: Path, size: str, seconds: str, *, audio: bool = False, rotate: bool = False
) -> None:
    if rotate:
        base = path.with_name(path.stem + "-base.mp4")
        make_clip(base, size, seconds, audio=False, rotate=False)
        _ffmpeg(["-display_rotation", "90", "-i", str(base), "-c", "copy", str(path)])
        base.unlink()
        return
    if audio:
        _ffmpeg(
            [
                "-f",
                "lavfi",
                "-i",
                f"testsrc=size={size}:rate=10",
                "-f",
                "lavfi",
                "-i",
                "sine=frequency=440:sample_rate=48000",
                "-t",
                seconds,
                "-shortest",
                "-map",
                "0:v:0",
                "-map",
                "1:a:0",
                "-pix_fmt",
                "yuv420p",
                "-c:a",
                "aac",
                str(path),
            ]
        )
        return
    _ffmpeg(
        [
            "-f",
            "lavfi",
            "-i",
            f"testsrc=size={size}:rate=10",
            "-t",
            seconds,
            "-pix_fmt",
            "yuv420p",
            str(path),
        ]
    )


def _submit(
    ws: Workspace,
    source: Path,
    content_id: str,
    index: int,
    *,
    captions: bytes | None = None,
) -> None:
    object_name = f"{index:064x}"
    payload = source.read_bytes()
    (ws.root / "quarantine" / object_name).write_bytes(payload)
    ws.enqueue(
        job_id=f"j{index}",
        content_id=content_id,
        object_name=object_name,
        output_budget_bytes=5_000_000,
        content_version=1,
        input_sha256=hashlib.sha256(payload).hexdigest(),
        captions=captions,
        captions_reviewed=captions is not None,
    )


def _encode(root: Path) -> None:
    ws = Workspace(root)
    clips = root / "clips"
    clips.mkdir()
    portrait = clips / "portrait.mp4"
    rotated = clips / "rotated.mp4"
    make_clip(portrait, "144x256", "0.3", audio=True)
    make_clip(rotated, "320x180", "0.3", rotate=True)
    _submit(
        ws,
        portrait,
        "cportrait",
        1,
        captions=b"WEBVTT\n\n00:00.000 --> 00:00.200\nportrait-cue\n",
    )
    _submit(ws, rotated, "crotated", 2)
    assert ws.run_once() is not None
    assert ws.run_once() is not None


@pytest.fixture(scope="module")
def media_root(tmp_path_factory: pytest.TempPathFactory) -> Path:
    root = tmp_path_factory.mktemp("encoded")
    _encode(root)
    return root


def _copy(src: Path, dst: Path) -> Path:
    shutil.copytree(src, dst)
    return dst


def _get(
    port: int,
    path: str,
    pem: bytes,
    *,
    headers: dict[str, str] | None = None,
) -> tuple[int, bytes, dict[str, str]]:
    connection = http.client.HTTPSConnection(
        "127.0.0.1", port, timeout=5, context=client_context(pem)
    )
    connection.request("GET", path, headers=headers or {})
    response = connection.getresponse()
    body = response.read()
    found = {key.lower(): value for key, value in response.getheaders()}
    connection.close()
    return response.status, body, found


def _size(path: Path) -> tuple[int, int]:
    proc = subprocess.run(
        [
            "ffprobe",
            "-v",
            "error",
            "-select_streams",
            "v:0",
            "-show_entries",
            "stream=width,height",
            "-of",
            "csv=p=0",
            str(path),
        ],
        capture_output=True,
        text=True,
        check=True,
    )
    width, height = proc.stdout.strip().split(",")
    return int(width), int(height)


class _Hold(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"
    calls: list[str] = []
    routes: dict[str, tuple[int, bytes]] = {}
    delay = 0.0
    hold: threading.Event | None = None

    def do_GET(self) -> None:  # noqa: N802
        path = urlsplit(self.path).path
        type(self).calls.append(path)
        if self.hold is not None:
            self.hold.wait(30)
        if self.delay:
            time.sleep(self.delay)
        status, body = self.routes.get(path, (404, b"missing"))
        self.send_response(status)
        self.send_header("Content-Length", str(len(body)))
        self.send_header("Content-Type", "application/octet-stream")
        self.send_header("Connection", "close")
        self.end_headers()
        try:
            self.wfile.write(body)
        except OSError:
            return

    def log_message(self, fmt: str, *args: object) -> None:
        return


def _listen(cert: EphemeralCert, handler: type[BaseHTTPRequestHandler]) -> ThreadingHTTPServer:
    httpd = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    httpd.daemon_threads = True
    httpd.socket = cert.server_context().wrap_socket(httpd.socket, server_side=True)
    threading.Thread(target=httpd.serve_forever, daemon=True).start()
    return httpd


def _document(
    base: str, segments: list[tuple[str, bytes]], *, fallback: str = "native"
) -> dict[str, object]:
    rows = []
    for path, body in segments:
        rows.append(
            {
                "segment_id": Path(path).stem,
                "sha256": hashlib.sha256(body).hexdigest(),
                "byte_count": str(len(body)),
                "path": path,
            }
        )
    lines = ["#EXTM3U", "#EXT-X-VERSION:7", "#EXT-X-STREAM-INF:BANDWIDTH=800"]
    master = "\n".join([*lines, f"{fallback}.m3u8", ""])
    body_lines = ["#EXTM3U", "#EXT-X-VERSION:7", "#EXT-X-TARGETDURATION:2"]
    for path, _body in segments:
        body_lines.append("#EXTINF:1.000,")
        body_lines.append(path)
    body_lines.append("#EXT-X-ENDLIST")
    return {
        "schema_version": 1,
        "hosting_promise": False,
        "disabled": list(DISABLED),
        "master_path": "master.m3u8",
        "fallback_rendition": fallback,
        "captions": None,
        "hosts": [
            {
                "host_id": "origin-test",
                "role": "origin",
                "base_url": base,
                "verified": True,
            }
        ],
        "renditions": [
            {
                "rendition_id": fallback,
                "bandwidth": 800,
                "segments": rows,
            }
        ],
        "_master": master.encode("utf-8"),
        "_media": ("\n".join(body_lines) + "\n").encode("utf-8"),
    }


def test_package_is_not_a_scaffold() -> None:
    assert SCAFFOLD is False
    assert DISABLED == ("ipfs", "p2p", "turn", "webrtc")


def test_processed_media_plays_over_https(tmp_path: Path, media_root: Path) -> None:
    root = _copy(media_root, tmp_path / "work")
    catalog = Catalog(root)
    catalog.note_chain("HELD")
    cert = EphemeralCert()
    server = MediaServer(catalog, cert=cert)
    try:
        server.start()
        catalog.authorize("cportrait", seconds=600)
        catalog.authorize("crotated", seconds=600)
        status, body, headers = _get(server.port, "/v1/playback/cportrait", cert.cert_pem)
        assert status == 200
        assert headers["x-svn-media-adapter"] == "origin-test/1"
        assert "access-control-allow-origin" not in headers
        document = json.loads(body)
        assert document["publication"] == "NOT_PUBLISHED"
        assert document["removal"] == "NONE"
        assert document["playback_grant"] == "AUTHORIZED"
        assert document["hosting_promise"] is False
        assert document["chain_status"] == "HELD"
        assert document["package_verified"] is True
        assert document["fallback_rendition"] == "native"
        master_status, master, master_headers = _get(
            server.port, "/v1/media/cportrait/v1/master.m3u8", cert.cert_pem
        )
        assert master_status == 200
        assert master_headers["content-type"].startswith("application/vnd.apple.mpegurl")
        text = master.decode("utf-8")
        assert text.startswith("#EXTM3U")
        assert "RESOLUTION=144x256" in text
        assert 'URI="segments/captions-0.vtt"' in text
        assert "http://" not in text and "https://" not in text
        assert all(
            not line.startswith("/")
            for line in text.splitlines()
            if line and not line.startswith("#")
        )
        harness = Harness(cert.cert_pem, timeout=5)
        played = harness.play(document)
        assert played.ok is True
        assert played.rendered is False
        assert played.rendition_id == "native"
        assert harness.metrics.rendered == 0
        assert harness.metrics.http_responses > 0
        assert harness.metrics.delivered_bytes > 0
        assert "segments/native-0.mp4" in played.fetched
        saved = tmp_path / "played.mp4"
        segment = root / "packages" / "cportrait" / "v1" / "segments" / "native-0.mp4"
        saved.write_bytes(segment.read_bytes())
        assert _size(saved) == (144, 256)
        rotated = json.loads(_get(server.port, "/v1/playback/crotated", cert.cert_pem)[1])
        rotated_play = harness.play(rotated)
        assert rotated_play.ok is True
        assert _size(root / "packages" / "crotated" / "v1" / "segments" / "native-0.mp4") == (
            180,
            320,
        )
        fallback = harness.play(document, rendition="h720")
        assert fallback.ok is True
        assert fallback.used_fallback is True
        assert fallback.rendition_id == "native"
        caption_status, caption, caption_headers = _get(
            server.port, "/v1/media/cportrait/v1/segments/captions-0.vtt", cert.cert_pem
        )
        assert caption_status == 200
        assert caption_headers["content-type"].startswith("text/vtt")
        assert caption.startswith(b"WEBVTT")
        assert b"portrait-cue" in caption
        exported = harness.metrics.export()
        assert "cportrait" not in exported
        assert exported["rendered"] == 0
        assert exported["delivered_bytes"] == harness.metrics.delivered_bytes
    finally:
        server.close()
        cert.close()


def test_corrupt_manifest_and_segment_are_not_advertised(tmp_path: Path, media_root: Path) -> None:
    root = _copy(media_root, tmp_path / "work")
    catalog = Catalog(root)
    catalog.note_chain("HELD")
    cert = EphemeralCert()
    server = MediaServer(catalog, cert=cert)
    try:
        server.start()
        catalog.authorize("cportrait", seconds=600)
        manifest = root / "packages" / "cportrait" / "v1" / "manifest.json"
        original = manifest.read_bytes()
        manifest.write_bytes(original[:-1] + (b"0" if original[-1:] != b"0" else b"1"))
        status, body, headers = _get(server.port, "/v1/playback/cportrait", cert.cert_pem)
        assert status == 200
        document = json.loads(body)
        assert document["package_verified"] is False
        assert document["hosts"] == []
        assert document["manifest_sha256"] is None
        media, payload, media_headers = _get(
            server.port, "/v1/media/cportrait/v1/master.m3u8", cert.cert_pem
        )
        assert media == 409
        assert json.loads(payload)["error"]["code"] == "INTEGRITY"
        assert media_headers["cache-control"] == "private, no-store"
        assert b"#EXTM3U" not in payload
        manifest.write_bytes(original)
        segment = root / "packages" / "cportrait" / "v1" / "segments" / "native-0.mp4"
        data = bytearray(segment.read_bytes())
        data[40] ^= 0x01
        segment.write_bytes(data)
        again, raw, _headers = _get(
            server.port, "/v1/media/cportrait/v1/segments/native-0.mp4", cert.cert_pem
        )
        assert again == 409
        assert json.loads(raw)["error"]["code"] == "INTEGRITY"
        assert raw[:3] != b"\x00\x00\x00"
        listed = json.loads(_get(server.port, "/v1/playback/cportrait", cert.cert_pem)[1])
        assert listed["hosts"] == []
    finally:
        server.close()
        cert.close()


def test_range_cors_quarantine_and_removal(tmp_path: Path, media_root: Path) -> None:
    root = _copy(media_root, tmp_path / "work")
    marker = b"QUARANTINE-BYTES-MARKER"
    (root / "quarantine" / f"{9:064x}").write_bytes(marker)
    catalog = Catalog(root)
    catalog.note_chain("HELD")
    cert = EphemeralCert()
    server = MediaServer(catalog, cert=cert)
    try:
        server.start()
        hidden, hidden_body, _headers = _get(server.port, "/v1/playback/cportrait", cert.cert_pem)
        assert hidden == 200
        assert json.loads(hidden_body)["hosts"] == []
        assert marker not in hidden_body
        catalog.authorize("cportrait", seconds=600)
        status, body, headers = _get(
            server.port,
            "/v1/media/cportrait/v1/segments/native-0.mp4",
            cert.cert_pem,
            headers={"Range": "bytes=0-7", "Origin": "https://playback.test.local"},
        )
        assert status == 206
        assert (
            body
            == (root / "packages" / "cportrait" / "v1" / "segments" / "native-0.mp4").read_bytes()[
                :8
            ]
        )
        assert headers["content-range"].startswith("bytes 0-7/")
        assert headers["access-control-allow-origin"] == "https://playback.test.local"
        assert headers["access-control-allow-origin"] != "*"
        other, _body, other_headers = _get(
            server.port,
            "/v1/media/cportrait/v1/segments/native-0.mp4",
            cert.cert_pem,
            headers={"Origin": "https://evil.example"},
        )
        assert other == 200
        assert "access-control-allow-origin" not in other_headers
        etag = headers["etag"]
        cached, empty, cache_headers = _get(
            server.port,
            "/v1/media/cportrait/v1/segments/native-0.mp4",
            cert.cert_pem,
            headers={"If-None-Match": etag},
        )
        assert cached == 304
        assert empty == b""
        assert catalog.etag_known("cportrait", etag) is True
        catalog.remove("cportrait")
        gone, gone_body, gone_headers = _get(
            server.port,
            "/v1/media/cportrait/v1/segments/native-0.mp4",
            cert.cert_pem,
            headers={"If-None-Match": etag},
        )
        assert gone == 404
        assert gone_headers["cache-control"] == "private, no-store"
        assert b"ftyp" not in gone_body
        assert catalog.etag_known("cportrait", etag) is False
        state = json.loads(_get(server.port, "/v1/playback/cportrait", cert.cert_pem)[1])
        assert state["removal"] == "REMOVED"
        assert state["hosts"] == []
        assert state["publication"] == "NOT_PUBLISHED"
        for path in (
            "/v1/media/cportrait/v1/segments/../manifest.json",
            "/quarantine/" + f"{9:064x}",
            "/v1/webrtc",
            "/v1/media/cportrait/v1/segments/" + f"{9:064x}.mp4",
        ):
            code, payload, _hdr = _get(server.port, path, cert.cert_pem)
            assert code in {400, 404}
            assert marker not in payload
        link = root / "packages" / "cportrait" / "v1" / "segments" / "native-0.mp4"
        # Removal already blocks bytes. A symlink is checked on a fresh grant.
    finally:
        server.close()
        cert.close()
    fresh = Catalog(root)
    fresh.note_chain("HELD")
    target = link
    target.unlink()
    target.symlink_to(root / "quarantine" / f"{9:064x}")
    with pytest.raises(DeliveryError) as raised:
        fresh.authorize("cportrait", seconds=60)
    assert raised.value.code == "INTEGRITY"


def test_replica_host_failure_and_chain_outage(tmp_path: Path, media_root: Path) -> None:
    root = _copy(media_root, tmp_path / "work")
    now = [1_000.0]
    catalog = Catalog(root, now=lambda: now[0])
    with pytest.raises(DeliveryError) as blocked:
        catalog.authorize("cportrait", seconds=60)
    assert blocked.value.code == "REFUSED"
    catalog.note_chain("HELD")
    cert = EphemeralCert()
    package = root / "packages" / "cportrait" / "v1"
    bad = tmp_path / "bad"
    shutil.copytree(package, bad)
    flipped = bytearray((bad / "segments" / "native-0.mp4").read_bytes())
    flipped[30] ^= 0x02
    (bad / "segments" / "native-0.mp4").write_bytes(flipped)
    with pytest.raises(DeliveryError) as integrity:
        catalog.accept_replica("test-bad", bad)
    assert integrity.value.code == "INTEGRITY"
    good = tmp_path / "good"
    shutil.copytree(package, good)
    catalog.accept_replica("test-a", good)
    replica = MediaServer(catalog, "test-a", role="test", cert=cert)
    origin = MediaServer(catalog, cert=cert)
    try:
        replica.start()
        origin.start()
        catalog.authorize("cportrait", seconds=100)
        document = catalog.document("cportrait")
        hosts = document["hosts"]
        assert isinstance(hosts, list)
        names: list[object] = []
        for item in hosts:
            assert isinstance(item, dict)
            names.append(item["host_id"])
        assert names == ["test-a", "origin-test"]
        assert document["hosting_promise"] is False
        replica.stop_listening()
        harness = Harness(cert.cert_pem, timeout=2)
        played = harness.play(document)
        assert played.ok is True
        assert played.hosts_tried[0] == "test-a"
        assert "origin-test" in played.hosts_tried
        assert played.attempts < 20
        assert played.rendered is False
        catalog.note_chain("UNAVAILABLE")
        stale = catalog.document("cportrait")
        assert stale["chain_status"] == "STALE"
        assert stale["stale"] is True
        assert stale["hosting_promise"] is False
        assert stale["hosts"]
        assert harness.play(stale).ok is True
        with pytest.raises(DeliveryError) as outage:
            catalog.authorize("crotated", seconds=60)
        assert outage.value.code == "REFUSED"
        with pytest.raises(DeliveryError) as new_host:
            catalog.accept_replica("test-b", good)
        assert new_host.value.code == "REFUSED"
        now[0] = 2_000.0
        expired = catalog.document("cportrait")
        assert expired["playback_grant"] == "EXPIRED"
        assert expired["hosts"] == []
        status, _body, _headers = _get(
            origin.port, "/v1/media/cportrait/v1/master.m3u8", cert.cert_pem
        )
        assert status == 404
        restored = Catalog(root, now=lambda: 1_050.0)
        assert restored.chain_note() == "UNAVAILABLE"
        assert restored.document("cportrait")["playback_grant"] == "AUTHORIZED"
        assert restored.document("cportrait")["hosts"] == []
    finally:
        origin.close()
        replica.close()
        cert.close()


def test_harness_bounds_failures_and_prefetch(tmp_path: Path) -> None:
    cert = EphemeralCert()
    segments = [
        ("segments/native-0.mp4", b"segment-zero"),
        ("segments/native-1.mp4", b"segment-one"),
        ("segments/native-2.mp4", b"segment-two"),
    ]
    httpd = _listen(cert, _Hold)
    slow: ThreadingHTTPServer | None = None
    port = httpd.server_address[1]
    assert isinstance(port, int)
    base = f"https://127.0.0.1:{port}/v1/media/c1/v1"
    document = _document(base, segments)
    master = document.pop("_master")
    media = document.pop("_media")
    assert isinstance(master, bytes) and isinstance(media, bytes)
    _Hold.routes = {
        "/v1/media/c1/v1/master.m3u8": (200, master),
        "/v1/media/c1/v1/native.m3u8": (200, media),
        "/v1/media/c1/v1/segments/native-0.mp4": (200, segments[0][1]),
        "/v1/media/c1/v1/segments/native-1.mp4": (200, segments[1][1]),
        "/v1/media/c1/v1/segments/native-2.mp4": (200, segments[2][1]),
    }
    _Hold.calls = []
    harness = Harness(cert.cert_pem, timeout=2)
    try:
        played = harness.play(document)
        assert played.ok is True
        assert played.fetched.count("segments/native-0.mp4") == 1
        assert "segments/native-1.mp4" in played.fetched
        assert "segments/native-2.mp4" in played.skipped
        assert "/v1/media/c1/v1/segments/native-2.mp4" not in _Hold.calls
        assert harness.metrics.rendered == 0
        bad = dict(document)
        bad["hosts"] = [
            {
                "host_id": "origin-test",
                "role": "origin",
                "base_url": base,
                "verified": True,
            }
        ]
        _Hold.routes["/v1/media/c1/v1/segments/native-0.mp4"] = (200, b"not-the-segment")
        _Hold.calls = []
        corrupt = harness.play(document)
        assert corrupt.ok is False
        assert corrupt.error == "INTEGRITY"
        assert corrupt.rendered is False
        assert harness.metrics.http_responses > 0
        _Hold.routes = {}
        missing = harness.play(document)
        assert missing.ok is False
        assert missing.error == "HTTP"
        slow = _listen(cert, _Hold)
        _Hold.delay = 2
        _Hold.routes = {"/v1/media/c1/v1/master.m3u8": (500, b"no")}
        slow_port = slow.server_address[1]
        assert isinstance(slow_port, int)
        hosts = []
        for index in range(6):
            hosts.append(
                {
                    "host_id": f"h{index}",
                    "role": "test",
                    "base_url": f"https://127.0.0.1:{slow_port}/v1/media/c1/v1",
                    "verified": True,
                }
            )
        many = dict(document)
        many["hosts"] = hosts
        bounded = Harness(cert.cert_pem, timeout=0.3)
        started = time.monotonic()
        refused = bounded.play(many)
        elapsed = time.monotonic() - started
        assert refused.ok is False
        assert refused.attempts == 8
        assert "h4" not in refused.hosts_tried
        assert "h5" not in refused.hosts_tried
        assert elapsed < 8
        public = dict(document)
        public["hosts"] = [
            {
                "host_id": "public",
                "role": "test",
                "base_url": "https://example.com/v1/media/c1/v1",
                "verified": True,
            }
        ]
        blocked = bounded.play(public)
        assert blocked.ok is False
        assert blocked.error == "REFUSED"
        assert blocked.attempts == 0
        promised = dict(document)
        promised["hosting_promise"] = True
        assert bounded.play(promised).error == "REFUSED"
        _Hold.delay = 0
        _Hold.hold = threading.Event()
        _Hold.calls = []
        _Hold.routes = {"/v1/media/c1/v1/master.m3u8": (200, master)}
        queue = []
        for index in range(5):
            item = dict(document)
            item["hosts"] = [
                {
                    "host_id": "origin-test",
                    "role": "origin",
                    "base_url": f"https://127.0.0.1:{slow_port}/v1/media/c{index}/v1",
                    "verified": True,
                }
            ]
            queue.append(item)
        before = bounded.metrics.delivered_bytes
        bounded.prefetch_queue(queue)
        time.sleep(0.2)
        bounded.abandon()
        bounded.wait_prefetch(2)
        assert bounded.skipped_items == 4
        assert bounded.metrics.delivered_bytes == before
        assert not any(path.endswith(".mp4") for path in _Hold.calls)
        if _Hold.hold is not None:
            _Hold.hold.set()
    finally:
        if _Hold.hold is not None:
            _Hold.hold.set()
        _Hold.hold = None
        _Hold.delay = 0
        httpd.shutdown()
        httpd.server_close()
        if slow is not None:
            slow.shutdown()
            slow.server_close()
        cert.close()


def test_browser_decodes_the_mp4_it_fetched(tmp_path: Path, media_root: Path) -> None:
    if find_browser() is None:
        pytest.skip("headless browser is not installed; rendering is unverified")
    root = _copy(media_root, tmp_path / "work")
    catalog = Catalog(root)
    catalog.record_hits = True
    catalog.note_chain("HELD")
    cert = EphemeralCert()
    server = MediaServer(catalog, cert=cert)
    try:
        server.start()
        catalog.authorize("cportrait", seconds=600)
        catalog.clear_hits()
        catalog.clear_report("cportrait")
        url = f"https://127.0.0.1:{server.port}/v1/player/cportrait"
        status = run_until(url, lambda: catalog.report("cportrait"), timeout=25)
        report = catalog.report("cportrait")
        assert status == "REPORTED", report
        assert report is not None
        segment = root / "packages" / "cportrait" / "v1" / "segments" / "native-0.mp4"
        width, height = _size(segment)
        assert report["sha256"] == hashlib.sha256(segment.read_bytes()).hexdigest()
        assert report["videoWidth"] == width
        assert report["videoHeight"] == height
        assert report["caption"] is True
        ready = report["readyState"]
        assert isinstance(ready, int) and ready >= 2
        assert isinstance(report["nonzero"], int) and report["nonzero"] > 0
        assert report["rendered"] is True
        hits = catalog.hits()
        assert any(item.endswith("segments/native-0.mp4") for item in hits)
        harness = Harness(cert.cert_pem)
        harness.metrics.note_rendered()
        assert harness.metrics.export()["rendered"] == 1
        plain = Harness(cert.cert_pem)
        assert plain.play(catalog.document("cportrait")).rendered is False
    finally:
        server.close()
        cert.close()


def test_cli_does_not_serve_without_a_grant(tmp_path: Path, media_root: Path) -> None:
    root = _copy(media_root, tmp_path / "work")
    Catalog(root).note_chain("HELD")
    env = os.environ.copy()
    env["PYTHONPATH"] = os.pathsep.join(
        [
            str(ROOT / "services" / "delivery"),
            str(ROOT / "packages" / "protocol"),
            env.get("PYTHONPATH", ""),
        ]
    )
    proc = subprocess.Popen(
        [
            sys.executable,
            "-m",
            "svn_delivery",
            "serve",
            "--root",
            str(root),
            "--port",
            "0",
        ],
        cwd=ROOT,
        env=env,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    try:
        assert proc.stdout is not None
        line = proc.stdout.readline()
        payload = json.loads(line)
        assert payload["adapter"] == "origin-test"
        assert payload["hosting_promise"] is False
        origin = payload["origin"]
        port = int(origin.rsplit(":", 1)[1])
        # The CLI certificate is inside the child. This process cannot trust it
        # without that PEM, so an unverified handshake is not a playback claim.
        # The child prints the origin and does not authorize.
        assert origin.startswith("https://127.0.0.1:")
        assert port != 0
    finally:
        proc.terminate()
        proc.wait(timeout=5)
    refused = subprocess.run(
        [sys.executable, "-m", "svn_delivery", "serve", "--root", str(root), "--bind", "0.0.0.0"],
        cwd=ROOT,
        env=env,
        capture_output=True,
        text=True,
        check=False,
    )
    assert refused.returncode == 1
    assert refused.stderr.strip() == "REFUSED"


def test_loopback_only_server(tmp_path: Path) -> None:
    with pytest.raises(DeliveryError) as raised:
        MediaServer(Catalog(tmp_path), bind="0.0.0.0")
    assert raised.value.code == "REFUSED"
