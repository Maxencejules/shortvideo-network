"""Second minimal client for the published SVN HTTP contracts.

The Python standard library is the only dependency. This file does not open
a database, a key file, or a quarantine directory. It does not claim
ActivityPub, AT Protocol, IPFS, or Filecoin compatibility. An HTTP status
is not a chain commit. A redirect is not followed.
"""

from __future__ import annotations

import argparse
import ast
import http.client
import json
import ssl
import sys
import urllib.parse
from dataclasses import dataclass
from pathlib import Path

ALLOWED_MODULES = frozenset(
    {
        "__future__",
        "argparse",
        "ast",
        "dataclasses",
        "http",
        "json",
        "pathlib",
        "ssl",
        "sys",
        "urllib",
    }
)
SPECS = (
    "docs/protocol/api/client-api.json",
    "docs/protocol/api/provider-api.json",
)
METHODS = frozenset({"get", "post", "put", "delete", "patch", "head", "options"})
PARAMS = {
    "upload_id": "u-goal30",
    "offer_id": "o-goal30",
    "content_id": "c-goal30",
    "content_version": "1",
    "transfer_id": "t-goal30",
}
CLIENT_SPEC = "docs/protocol/api/client-api.json"


@dataclass(frozen=True)
class Operation:
    spec: str
    method: str
    template: str

    def path(self) -> str:
        filled = self.template
        for key, value in PARAMS.items():
            filled = filled.replace("{" + key + "}", value)
        if "{" in filled or "}" in filled:
            raise ValueError(f"unfilled template {self.template}")
        return filled


@dataclass(frozen=True)
class CallResult:
    spec: str
    method: str
    path: str
    status: int
    error: str
    location: str


@dataclass
class Report:
    calls: list[CallResult]

    @property
    def redirect_refused(self) -> bool:
        return any(300 <= item.status < 400 or item.location != "" for item in self.calls)

    def ok(self) -> bool:
        if not self.calls or self.redirect_refused:
            return False
        return all(item.status != 0 and item.error == "" for item in self.calls)


def load_operations(root: Path) -> list[Operation]:
    found: list[Operation] = []
    for rel in SPECS:
        raw = json.loads((root / rel).read_text(encoding="utf-8"))
        if not isinstance(raw, dict):
            raise ValueError(rel)
        paths = raw.get("paths")
        if not isinstance(paths, dict):
            raise ValueError(rel)
        for template, item in paths.items():
            if not isinstance(template, str) or not isinstance(item, dict):
                raise ValueError(rel)
            for method in item:
                if not isinstance(method, str) or method not in METHODS:
                    continue
                found.append(Operation(rel, method.upper(), template))
    if len(found) != 12:
        raise ValueError(f"published operation count {len(found)}")
    return found


def source_problems(path: Path) -> list[str]:
    text = path.read_text(encoding="utf-8")
    problems: list[str] = []
    if "does not claim ActivityPub" not in text:
        problems.append("missing interoperability refusal")
    tree = ast.parse(text)
    for node in ast.walk(tree):
        if isinstance(node, ast.Import):
            for alias in node.names:
                root_name = alias.name.split(".", 1)[0]
                if root_name not in ALLOWED_MODULES:
                    problems.append(f"import {alias.name}")
        elif isinstance(node, ast.ImportFrom):
            module = node.module or ""
            root_name = module.split(".", 1)[0]
            if root_name not in ALLOWED_MODULES:
                problems.append(f"from {module}")
    return problems


def call_once(url: str, method: str, ca: str | None) -> tuple[int, str, str]:
    parts = urllib.parse.urlsplit(url)
    host = parts.hostname
    if host is None or parts.scheme not in {"http", "https"}:
        return 0, "bad-url", ""
    if parts.scheme == "https" and ca is None:
        return 0, "missing-ca", ""
    port = parts.port if parts.port is not None else (443 if parts.scheme == "https" else 80)
    target = parts.path or "/"
    if parts.query:
        target = f"{target}?{parts.query}"
    body = b"{}" if method in {"POST", "PUT", "PATCH"} else None
    headers = {"content-type": "application/json"} if body is not None else {}
    try:
        if parts.scheme == "https":
            assert ca is not None
            context = ssl.create_default_context(cafile=ca)
            context.minimum_version = ssl.TLSVersion.TLSv1_2
            conn: http.client.HTTPConnection = http.client.HTTPSConnection(
                host, port, timeout=5, context=context
            )
        else:
            conn = http.client.HTTPConnection(host, port, timeout=5)
        conn.request(method, target, body=body, headers=headers)
        response = conn.getresponse()
        location = response.getheader("location") or ""
        response.read(4096)
        status = int(response.status)
        conn.close()
    except OSError as exc:
        return 0, type(exc).__name__, ""
    return status, "", location


def exercise(
    operations: list[Operation], client_base: str, provider_base: str, ca: str | None
) -> Report:
    calls: list[CallResult] = []
    for operation in operations:
        base = client_base if operation.spec == CLIENT_SPEC else provider_base
        status, error, location = call_once(
            base.rstrip("/") + operation.path(), operation.method, ca
        )
        calls.append(
            CallResult(operation.spec, operation.method, operation.path(), status, error, location)
        )
    return Report(calls)


def print_report(report: Report) -> None:
    print(f"CLIENT operations={len(report.calls)}")
    for item in report.calls:
        location = item.location or "-"
        error = item.error or "-"
        print(
            f"CLIENT request spec={item.spec} method={item.method} path={item.path} "
            f"status={item.status} error={error} location={location}"
        )
    print("CLIENT private_shortcut=false")
    print("CLIENT interop activitypub=false atproto=false ipfs=false filecoin=false")
    print("CLIENT ok" if report.ok() else "CLIENT blocked")


def main(argv: list[str] | None = None) -> int:
    parser = argparse.ArgumentParser(prog="second-client")
    parser.add_argument("--root", type=Path, default=Path("."))
    sub = parser.add_subparsers(dest="cmd", required=True)
    sub.add_parser("self-check")
    live = sub.add_parser("exercise")
    live.add_argument("--client-base", required=True)
    live.add_argument("--provider-base", required=True)
    live.add_argument("--ca", default="")
    args = parser.parse_args(argv)
    root = args.root.resolve()
    if args.cmd == "self-check":
        problems = source_problems(Path(__file__).resolve())
        operations = load_operations(root)
        print(f"CLIENT source_problems={len(problems)}")
        for problem in problems:
            print(f"CLIENT source_problem {problem}")
        print(f"CLIENT operations={len(operations)}")
        print("CLIENT private_shortcut=false")
        print("CLIENT interop activitypub=false atproto=false ipfs=false filecoin=false")
        if problems:
            print("CLIENT blocked")
            return 1
        print("CLIENT ok")
        return 0
    ca = args.ca or None
    report = exercise(load_operations(root), args.client_base, args.provider_base, ca)
    print_report(report)
    return 0 if report.ok() else 1


if __name__ == "__main__":
    sys.exit(main())
