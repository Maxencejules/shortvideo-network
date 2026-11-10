"""HTTPS media adapter. It binds to loopback and does not follow a public name."""

from __future__ import annotations

import hashlib
import ipaddress
import json
import re
import threading
from collections.abc import Callable
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from typing import cast
from urllib.parse import urlsplit

from svn_delivery.browser import page_html
from svn_delivery.catalog import Catalog
from svn_delivery.errors import DeliveryError
from svn_delivery.ids import (
    ADAPTER_NAME,
    ADAPTER_VERSION,
    ALLOWED_ORIGIN,
    CONTENT_TYPE,
    DISABLED,
    ID,
    SEGMENT_CACHE_SECONDS,
    SHA,
)
from svn_delivery.tlsutil import EphemeralCert

_ADAPTER = f"{ADAPTER_NAME}/{ADAPTER_VERSION}"
_PLAYBACK = re.compile(r"^/v1/playback/(" + ID.pattern[1:-1] + r")$")
_PLAYER = re.compile(r"^/v1/player/(" + ID.pattern[1:-1] + r")$")
_REPORT = re.compile(r"^/v1/player/(" + ID.pattern[1:-1] + r")/report$")
_MEDIA = re.compile(
    r"^/v1/media/(" + ID.pattern[1:-1] + r")/v([1-9][0-9]{0,8})/(" + ID.pattern[1:-1] + r"\.m3u8)$"
)
_SEGMENT = re.compile(
    r"^/v1/media/("
    + ID.pattern[1:-1]
    + r")/v([1-9][0-9]{0,8})/segments/("
    + ID.pattern[1:-1]
    + r"\.(?:mp4|jpg|vtt))$"
)


class MediaServer:
    """One origin or test host. TLS is required. The certificate is ephemeral.

    Playback uses a local grant on this loopback listener. The grant is not an
    application account session and it is not publication.
    """

    def __init__(
        self,
        catalog: Catalog,
        host_id: str = "origin-test",
        *,
        role: str = "origin",
        bind: str = "127.0.0.1",
        port: int = 0,
        allow_origin: str = ALLOWED_ORIGIN,
        cert: EphemeralCert | None = None,
    ) -> None:
        try:
            address = ipaddress.ip_address(bind)
        except ValueError:
            raise DeliveryError("REFUSED") from None
        if not address.is_loopback:
            raise DeliveryError("REFUSED")
        if role not in {"origin", "test"} or not ID.fullmatch(host_id):
            raise DeliveryError("REFUSED")
        self.catalog = catalog
        self.host_id = host_id
        self.role = role
        self.bind = bind
        self.port = port
        self.allow_origin = allow_origin
        self.cert = cert if cert is not None else EphemeralCert()
        self._own_cert = cert is None
        self._httpd: ThreadingHTTPServer | None = None
        self._thread: threading.Thread | None = None
        self._listening = False

    def start(self) -> None:
        handler = _handler(self.catalog, self.host_id, self.allow_origin)
        httpd = ThreadingHTTPServer((self.bind, self.port), handler)
        httpd.daemon_threads = True
        httpd.socket = self.cert.server_context().wrap_socket(httpd.socket, server_side=True)
        bound = httpd.server_address
        if not isinstance(bound[1], int):
            httpd.server_close()
            raise DeliveryError("UNAVAILABLE")
        self.port = bound[1]
        try:
            self.catalog.bind_host(self.host_id, f"https://{self.bind}:{self.port}", role=self.role)
        except DeliveryError:
            httpd.server_close()
            raise
        self._httpd = httpd
        self._thread = threading.Thread(
            target=httpd.serve_forever, name="svn-delivery", daemon=True
        )
        self._thread.start()
        self._listening = True

    def stop_listening(self) -> None:
        """Close the socket and keep the host URL registered so clients still fail over."""
        httpd = self._httpd
        if httpd is not None and self._listening:
            self._listening = False
            httpd.shutdown()
            httpd.server_close()
        thread = self._thread
        if thread is not None:
            thread.join(timeout=5)

    def close(self) -> None:
        self.stop_listening()
        self.catalog.unbind_host(self.host_id)
        if self._own_cert:
            self.cert.close()


def _handler(catalog: Catalog, host_id: str, allow_origin: str) -> type[BaseHTTPRequestHandler]:
    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"
        server_version = "svn-delivery/1"

        def log_message(self, fmt: str, *args: object) -> None:
            return

        def log_error(self, fmt: str, *args: object) -> None:
            return

        def do_GET(self) -> None:  # noqa: N802
            self._dispatch(True)

        def do_HEAD(self) -> None:  # noqa: N802
            self._dispatch(False)

        def do_OPTIONS(self) -> None:  # noqa: N802
            self._send(
                204, b"", "text/plain; charset=utf-8", None, "private, no-store", False, None
            )

        def do_POST(self) -> None:  # noqa: N802
            match = _REPORT.fullmatch(self.path)
            if match is None or not _safe_path(self.path):
                self._fail(DeliveryError("BAD_REQUEST"), True)
                return
            try:
                payload = _read_body(self)
                catalog.store_report(match.group(1), _clean_report(payload))
            except DeliveryError as exc:
                self._fail(exc, True)
                return
            self._send(204, b"", CONTENT_TYPE[".json"], None, "private, no-store", False, None)

        def do_PUT(self) -> None:  # noqa: N802
            self._fail(DeliveryError("BAD_REQUEST"), True)

        def do_DELETE(self) -> None:  # noqa: N802
            self._fail(DeliveryError("BAD_REQUEST"), True)

        def _dispatch(self, include_body: bool) -> None:
            path = urlsplit(self.path).path
            if not _safe_path(self.path) or path != self.path:
                self._fail(DeliveryError("BAD_REQUEST"), include_body)
                return
            try:
                self._route(path, include_body)
            except DeliveryError as exc:
                self._fail(exc, include_body)
            except Exception:
                self._fail(DeliveryError("UNAVAILABLE"), include_body)

        def _route(self, path: str, include_body: bool) -> None:
            playback = _PLAYBACK.fullmatch(path)
            if playback is not None:
                body = json.dumps(
                    catalog.document(playback.group(1)),
                    sort_keys=True,
                    separators=(",", ":"),
                ).encode("utf-8")
                self._send(
                    200, body, CONTENT_TYPE[".json"], None, "private, no-store", include_body, None
                )
                return
            report = _REPORT.fullmatch(path)
            if report is not None:
                found = catalog.report(report.group(1))
                if found is None:
                    raise DeliveryError("NOT_FOUND")
                body = json.dumps(found, sort_keys=True, separators=(",", ":")).encode("utf-8")
                self._send(
                    200, body, CONTENT_TYPE[".json"], None, "private, no-store", include_body, None
                )
                return
            player = _PLAYER.fullmatch(path)
            if player is not None:
                if not catalog.grant_live(player.group(1)):
                    raise DeliveryError("NOT_FOUND")
                html = page_html(player.group(1)).encode("utf-8")
                self._send(
                    200,
                    html,
                    CONTENT_TYPE[".html"],
                    None,
                    "private, no-store",
                    include_body,
                    None,
                    csp=True,
                )
                return
            media = _MEDIA.fullmatch(path)
            if media is not None:
                content_id, version_s, name = media.group(1), media.group(2), media.group(3)
                _kind, body = catalog.read(host_id, content_id, int(version_s), name)
                etag = '"' + hashlib.sha256(body).hexdigest() + '"'
                self._conditional(
                    content_id,
                    body,
                    CONTENT_TYPE[".m3u8"],
                    etag,
                    "private, max-age=0, must-revalidate",
                    include_body,
                    ranged=False,
                )
                return
            segment = _SEGMENT.fullmatch(path)
            if segment is not None:
                content_id, version_s, filename = (
                    segment.group(1),
                    segment.group(2),
                    segment.group(3),
                )
                _kind, body = catalog.read(
                    host_id, content_id, int(version_s), f"segments/{filename}"
                )
                suffix = "." + filename.rsplit(".", 1)[1]
                digest = hashlib.sha256(body).hexdigest()
                etag = f'"{digest}"'
                self._conditional(
                    content_id,
                    body,
                    CONTENT_TYPE[suffix],
                    etag,
                    f"private, max-age={SEGMENT_CACHE_SECONDS}",
                    include_body,
                    ranged=True,
                )
                return
            raise DeliveryError("NOT_FOUND")

        def _conditional(
            self,
            content_id: str,
            body: bytes,
            content_type: str,
            etag: str,
            cache: str,
            include_body: bool,
            *,
            ranged: bool,
        ) -> None:
            header = self.headers.get("If-None-Match")
            if header and etag in [part.strip() for part in header.split(",")]:
                if not catalog.etag_known(content_id, etag):
                    # First sight of this etag still has to be a full response.
                    pass
                elif catalog.etag_known(content_id, etag):
                    catalog.remember_etag(content_id, etag)
                    self._send(304, b"", content_type, etag, cache, False, None)
                    return
            catalog.remember_etag(content_id, etag)
            extra: dict[str, str] | None = {"Accept-Ranges": "bytes"} if ranged else None
            status = 200
            payload = body
            if ranged:
                requested = self.headers.get("Range")
                if requested:
                    try:
                        start, end = _parse_range(requested, len(body))
                    except DeliveryError as exc:
                        if exc.code == "RANGE":
                            self._send(
                                416,
                                b"",
                                content_type,
                                etag,
                                "private, no-store",
                                False,
                                {"Content-Range": f"bytes */{len(body)}"},
                            )
                            return
                        raise
                    payload = body[start : end + 1]
                    status = 206
                    extra = {
                        "Accept-Ranges": "bytes",
                        "Content-Range": f"bytes {start}-{end}/{len(body)}",
                    }
            self._send(status, payload, content_type, etag, cache, include_body, extra)

        def _fail(self, exc: DeliveryError, include_body: bool) -> None:
            body = json.dumps(
                {"error": {"code": exc.code, "message": str(exc)}},
                sort_keys=True,
                separators=(",", ":"),
            ).encode("utf-8")
            self._send(
                exc.status,
                body,
                CONTENT_TYPE[".json"],
                None,
                "private, no-store",
                include_body,
                None,
            )

        def _send(
            self,
            status: int,
            body: bytes,
            content_type: str,
            etag: str | None,
            cache: str,
            include_body: bool,
            extra: dict[str, str] | None,
            csp: bool = False,
        ) -> None:
            self.send_response(status)
            self.send_header("Content-Type", content_type)
            self.send_header("Content-Length", str(len(body) if status != 304 else 0))
            self.send_header("Cache-Control", cache)
            self.send_header("X-Content-Type-Options", "nosniff")
            self.send_header("Referrer-Policy", "no-referrer")
            self.send_header("X-SVN-Media-Adapter", _ADAPTER)
            self.send_header("Connection", "close")
            if etag is not None:
                self.send_header("ETag", etag)
            if csp:
                self.send_header(
                    "Content-Security-Policy",
                    "default-src 'none'; media-src 'self' blob:; connect-src 'self'; "
                    "script-src 'unsafe-inline'; img-src 'self' blob:",
                )
            origin = self.headers.get("Origin")
            self.send_header("Vary", "Origin")
            if origin == allow_origin:
                self.send_header("Access-Control-Allow-Origin", origin)
                self.send_header("Access-Control-Allow-Methods", "GET, HEAD, OPTIONS")
                self.send_header("Access-Control-Allow-Headers", "Range, Content-Type")
                self.send_header(
                    "Access-Control-Expose-Headers",
                    "Accept-Ranges, Content-Range, Content-Length, ETag, X-SVN-Media-Adapter",
                )
                self.send_header("Access-Control-Max-Age", "600")
            if extra:
                for key, value in extra.items():
                    self.send_header(key, value)
            self.end_headers()
            if include_body and status not in {204, 304, 205}:
                self.wfile.write(body)

    return Handler


def _safe_path(path: str) -> bool:
    if path != urlsplit(path).path or "%" in path or "\\" in path or "\x00" in path:
        return False
    parts = [part for part in path.split("/") if part]
    if any(part in {"..", "."} or part in DISABLED or part == "quarantine" for part in parts):
        return False
    return path.startswith("/v1/")


def _parse_range(header: str, size: int) -> tuple[int, int]:
    if not header.startswith("bytes=") or "," in header or size < 1:
        raise DeliveryError("BAD_REQUEST")
    spec = header.removeprefix("bytes=")
    try:
        if spec.startswith("-"):
            count = int(spec[1:])
            if count < 1:
                raise DeliveryError("BAD_REQUEST")
            if count > size:
                count = size
            return size - count, size - 1
        start_s, dash, end_s = spec.partition("-")
        if dash != "-" or start_s == "":
            raise DeliveryError("BAD_REQUEST")
        start = int(start_s)
        if start < 0 or start >= size:
            raise DeliveryError("RANGE")
        end = size - 1 if end_s == "" else int(end_s)
    except ValueError:
        raise DeliveryError("BAD_REQUEST") from None
    if end < start:
        raise DeliveryError("RANGE")
    if end >= size:
        end = size - 1
    return start, end


def _read_body(handler: BaseHTTPRequestHandler) -> object:
    length = handler.headers.get("Content-Length")
    if length is None or not length.isdigit():
        raise DeliveryError("BAD_REQUEST")
    size = int(length)
    if size < 2 or size > 4096:
        raise DeliveryError("BAD_REQUEST")
    raw = handler.rfile.read(size)
    if len(raw) != size:
        raise DeliveryError("BAD_REQUEST")
    try:
        return cast(object, json.loads(raw.decode("utf-8")))
    except (UnicodeError, json.JSONDecodeError):
        raise DeliveryError("BAD_REQUEST") from None


def _clean_report(value: object) -> dict[str, object]:
    if not isinstance(value, dict):
        raise DeliveryError("BAD_REQUEST")
    rendered = value.get("rendered")
    caption = value.get("caption")
    error = value.get("error", "")
    digest = value.get("sha256", "")
    width = value.get("videoWidth", 0)
    height = value.get("videoHeight", 0)
    ready = value.get("readyState", 0)
    nonzero = value.get("nonzero", 0)
    if not isinstance(rendered, bool) or not isinstance(caption, bool):
        raise DeliveryError("BAD_REQUEST")
    if not isinstance(error, str) or len(error) > 200:
        raise DeliveryError("BAD_REQUEST")
    if not isinstance(digest, str) or (digest and SHA.fullmatch(digest) is None):
        raise DeliveryError("BAD_REQUEST")
    numbers = (width, height, ready, nonzero)
    if any(not isinstance(item, int) or isinstance(item, bool) for item in numbers):
        raise DeliveryError("BAD_REQUEST")
    if not 0 <= width <= 7680 or not 0 <= height <= 7680:
        raise DeliveryError("BAD_REQUEST")
    if not 0 <= ready <= 4 or not 0 <= nonzero <= 20_000_000:
        raise DeliveryError("BAD_REQUEST")
    return {
        "rendered": rendered,
        "caption": caption,
        "error": error,
        "sha256": digest,
        "videoWidth": width,
        "videoHeight": height,
        "readyState": ready,
        "nonzero": nonzero,
    }


# Imported by tests that want the range parser without a socket.
parse_range: Callable[[str, int], tuple[int, int]] = _parse_range
