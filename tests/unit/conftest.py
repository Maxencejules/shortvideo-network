"""A permissive admission stand-in so unit tests that encode still have a gate.

The cap tests point SVN_OBSERVE_URL at the real svn-observe process instead.
"""

from __future__ import annotations

import os
import threading
from collections.abc import Iterator
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

import pytest

_TOKEN = "synthetic-observe-25"


@pytest.fixture(autouse=True, scope="session")
def _permissive_admission() -> Iterator[None]:
    class Handler(BaseHTTPRequestHandler):
        protocol_version = "HTTP/1.1"

        def log_message(self, fmt: str, *args: object) -> None:
            return

        def do_POST(self) -> None:  # noqa: N802
            length = int(self.headers.get("Content-Length", "0") or "0")
            self.rfile.read(length)
            if self.headers.get("Authorization") != "Bearer " + _TOKEN:
                self._send(401, b'{"error":"unauthorized"}')
                return
            body = (
                b'{"granted":true,"queued":false,"replayed":false,'
                b'"reason":"admitted","grant_id":"gpermissive"}'
            )
            self._send(200, body)

        def _send(self, status: int, body: bytes) -> None:
            self.send_response(status)
            self.send_header("Content-Type", "application/json")
            self.send_header("Content-Length", str(len(body)))
            self.end_headers()
            self.wfile.write(body)

    server = ThreadingHTTPServer(("127.0.0.1", 0), Handler)
    thread = threading.Thread(target=server.serve_forever, name="admit-permit", daemon=True)
    thread.start()
    bound = server.server_address
    host = bound[0]
    port = bound[1]
    if isinstance(host, bytes):
        host = host.decode()
    os.environ["SVN_OBSERVE_URL"] = f"http://{host}:{port}"
    os.environ["SVN_OBSERVE_TOKEN"] = _TOKEN
    yield
    server.shutdown()
