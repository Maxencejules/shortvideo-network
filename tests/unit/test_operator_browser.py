"""Operator dashboard in a browser against svn-operator.

The page is the built operator script. The process is cmd/svn-operator.
Readings come from loopback fixtures. No consensus key is entered.
"""

from __future__ import annotations

import json
import shutil
import subprocess
import threading
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

import pytest
from svn_delivery.browser import find_browser

ROOT = Path(__file__).resolve().parents[2]
DRIVE = ROOT / "apps" / "web" / "test" / "drive.mjs"
NARRATIVE = "hidden complaint words"


def test_operator_dashboard_roles_and_stale_data(tmp_path: Path) -> None:
    chrome = find_browser()
    if chrome is None:
        pytest.skip("headless browser is not installed; rendering is unverified")
    if shutil.which("node") is None or shutil.which("go") is None:
        pytest.skip("node or go is not installed")
    if not (ROOT / "node_modules" / "esbuild").exists():
        pytest.skip("esbuild is not installed; the operator bundle is unverified")
    binary = tmp_path / "svn-operator"
    built = subprocess.run(
        ["go", "build", "-o", str(binary), "./cmd/svn-operator"],
        cwd=ROOT / "services" / "operator",
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
    provider = _serve(_provider)
    indexer = _serve(_indexer)
    discovery = _serve(_discovery)
    governance = _serve(_governance)
    token_file = tmp_path / "discovery.token"
    token_file.write_text("discovery-token\n", encoding="utf-8")
    config = tmp_path / "operator.json"
    config.write_text(
        json.dumps(
            {
                "public_listen": "127.0.0.1:0",
                "admin_listen": "127.0.0.1:0",
                "dev_sessions": True,
                "web_dir": str(ROOT / "apps" / "web" / "dist"),
                "chain_id": "shortvideo-dev-1",
                "provider": {
                    "id": "alpha",
                    "base_url": provider,
                    "ca_file": "",
                    "admin_token_file": "",
                },
                "indexer_url": indexer,
                "discovery_url": discovery,
                "discovery_token_file": str(token_file),
                "governance_url": governance,
                "media_queue": "",
                "scheduler_url": "",
            }
        ),
        encoding="utf-8",
    )
    proc = subprocess.Popen(
        [str(binary), str(config)],
        cwd=ROOT,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        text=True,
    )
    assert proc.stdout is not None
    try:
        public = _listen_line(proc, "PUBLIC")
        admin = _listen_line(proc, "ADMIN")
        plan = tmp_path / "plan.json"
        plan.write_text(
            json.dumps(_plan(chrome, "http://" + admin, "http://" + public)), encoding="utf-8"
        )
        run = subprocess.run(
            ["node", str(DRIVE), str(plan)], cwd=ROOT, check=False, text=True, capture_output=True
        )
        assert run.returncode == 0, run.stderr + run.stdout
        saved = json.loads(run.stdout)["saved"]
        assert saved["stale"]["stale"] is True
        assert saved["onboard"]["validator"] is False
        assert saved["viewer"]["drain"] is None
        assert saved["denied"]["status"] == 403
        assert saved["queue"]["case"] is True
        assert saved["queue"]["narrative"] is False
        assert saved["council"]["proposal"] is True
        assert saved["council"]["key"] is False
        assert saved["govern"]["refused"] is True
    finally:
        proc.terminate()
        proc.wait(timeout=5)


def _listen_line(proc: subprocess.Popen[str], kind: str) -> str:
    assert proc.stdout is not None
    for _ in range(4):
        line = proc.stdout.readline().strip()
        if line.startswith(kind + " "):
            return line.split(" ", 1)[1]
    raise AssertionError(kind)


def _js(*parts: str) -> str:
    return "".join(parts)


def _plan(chrome: str, admin: str, public: str) -> dict[str, object]:
    _ = public
    stale = "document.body.innerText.includes('STALE') ? { stale: true } : null"
    onboard = (
        "document.body.innerText.includes('single-machine demo') && "
        "document.body.innerText.includes('validator_selected false') "
        "? { validator: false } : null"
    )
    viewer = _js(
        "(() => { const role = document.querySelector('#role');",
        "const drain = document.querySelector('#action-drain');",
        "return role && role.textContent === 'role viewer' && !drain",
        "? { drain: null } : null; })()",
    )
    denied = _js(
        "(async () => {",
        "const res = await fetch('/v1/drain', { method: 'POST',",
        "headers: { 'content-type': 'application/json',",
        "authorization: 'Bearer ' + window.__token },",
        "body: JSON.stringify({ confirm: 'DRAIN' }) });",
        "return { status: res.status }; })()",
    )
    queue = _js(
        "(() => { const node = document.querySelector('#queue');",
        "const text = node ? node.textContent : '';",
        "return text.includes('case1') ?",
        "{ case: true, narrative: text.includes(" + json.dumps(NARRATIVE) + ") }",
        ": null; })()",
    )
    council = _js(
        "(() => { const node = document.querySelector('#council');",
        "const text = node ? node.textContent : '';",
        "const key = document.querySelector('input[type=password]');",
        "return text.includes('p1') && text.includes('consensus private key')",
        "? { proposal: true, key: key !== null } : null; })()",
    )
    govern = _js(
        "(() => { const node = document.querySelector('#result');",
        "const text = node ? node.textContent : '';",
        "return text.includes('GOVERNANCE_NOT_SUBMITTED')",
        "&& text.includes('\"chain_submitted\":false')",
        "? { refused: true } : null; })()",
    )
    return {
        "chrome": chrome,
        "steps": [
            {"do": "goto", "url": admin + "/"},
            {"do": "wait", "expr": stale, "save": "stale"},
            {"do": "click", "selector": "#session-provider"},
            {
                "do": "wait",
                "expr": _js(
                    "(() => { const role = document.querySelector('#role');",
                    "return role && role.textContent === 'role provider'",
                    "? true : null; })()",
                ),
                "save": "provider",
            },
            {"do": "click", "selector": "#duty-storage"},
            {"do": "click", "selector": "#onboard"},
            {"do": "wait", "expr": onboard, "save": "onboard"},
            {"do": "click", "selector": "#session-viewer"},
            {"do": "wait", "expr": viewer, "save": "viewer"},
            {
                "do": "eval",
                "save": "token",
                "expr": (
                    "(async () => { const res = await fetch('/v1/dev/session', { method: 'POST', "
                    "headers: { 'content-type': 'application/json' }, "
                    "body: JSON.stringify({ role: 'viewer', account_id: 'viewer1' }) }); "
                    "const doc = await res.json(); window.__token = doc.token; return true; })()"
                ),
            },
            {"do": "eval", "expr": denied, "save": "denied"},
            {"do": "click", "selector": "#session-moderator"},
            {"do": "wait", "expr": queue, "save": "queue"},
            {"do": "click", "selector": "#session-council"},
            {
                "do": "wait",
                "expr": "document.querySelector('#action-governance') ? true : null",
                "save": "ready",
            },
            {"do": "click", "selector": "#action-governance"},
            {"do": "wait", "expr": govern, "save": "govern"},
            {"do": "wait", "expr": council, "save": "council"},
        ],
    }


class _Handler(BaseHTTPRequestHandler):
    routes: dict[str, bytes]

    def do_GET(self) -> None:  # noqa: N802
        body = self.routes.get(self.path)
        if body is None:
            self.send_response(404)
            self.end_headers()
            return
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, fmt: str, *args: object) -> None:
        return


def _serve(routes: dict[str, bytes]) -> str:
    handler = type("Routes", (_Handler,), {"routes": routes})
    server = ThreadingHTTPServer(("127.0.0.1", 0), handler)
    threading.Thread(target=server.serve_forever, daemon=True).start()
    raw_host = server.server_address[0]
    host = raw_host if isinstance(raw_host, str) else raw_host.decode("ascii")
    return "http://" + host + ":" + str(server.server_address[1])


_provider = {
    "/status": (
        b'{"ok":true,"independent_operator":false,"draining":false,"used_bytes":8,'
        b'"quota_bytes":64,"declared_capacity_bytes":64,"bandwidth_bytes_per_second":1000,'
        b'"bytes_fetched":8,"bytes_served":8,"disk_available_bytes":1000,"endpoint_enabled":true,'
        b'"evidence_mode":"ATTESTATION_NOT_STORAGE_PROOF","software_version":"development",'
        b'"chain_uncertain":true,"chain_height":4,"leases":[]}'
    )
}
_indexer = {
    "/v1/status": b"".join(
        [
            b'{"indexed_height":2,"canonical_height":4,',
            b'"stale":true,"stale_reasons":["commit_age"]}',
        ]
    ),
    "/v1/deadlines": b'{"deadlines":[]}',
    "/v1/policy": b'{"present":false}',
}
_discovery = {
    "/v1/moderation/queue": (
        b'{"queue":[{"case_id":"case1","status":"open","target_type":"content",'
        b'"target_id":"c1","reason_code":"placeholder","narrative":"hidden complaint words",'
        b'"complainant":"viewer1"}]}'
    )
}
_governance = {
    "/": (
        b'{"proposals":[{"id":"p1","state":"OPEN","approvals":1,"approval_target":3,'
        b'"execute_after_unix":10,"activation_height":20,"parameter_key":"page"}],'
        b'"upgrade":{"name":"plan-a","height":20,"state":"QUEUED"}}'
    )
}
