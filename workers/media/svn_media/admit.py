"""Ask svn-observe before a processing slot is taken.

A missing address, a refused connection, or a full cap does not encode.
The same idempotency key does not consume a second slot.
"""

from __future__ import annotations

import json
import os
import urllib.error
import urllib.request


def processing(key: str) -> str:
    """Return granted, refused, or unreachable."""
    status, _grant = processing_decision(key)
    return status


def processing_decision(key: str) -> tuple[str, str]:
    """Return the admission status and the grant id, if one was issued."""
    base = os.environ.get("SVN_OBSERVE_URL", "").rstrip("/")
    token = os.environ.get("SVN_OBSERVE_TOKEN", "")
    if not base or not token:
        return "unreachable", ""
    body = json.dumps(
        {"kind": "processing_concurrency", "units": 1, "idempotency_key": key}
    ).encode("utf-8")
    payload, status = _post(base + "/v1/admit", token, body)
    if status == "unreachable":
        return "unreachable", ""
    if status == "refused":
        return "refused", ""
    grant = payload.get("grant_id")
    if payload.get("granted") is True and isinstance(grant, str):
        return "granted", grant
    if payload.get("granted") is True:
        return "granted", ""
    return "refused", ""


def release_grant(grant_id: str) -> None:
    """Return one processing hold. A second release is a no-op at the server."""
    if not grant_id:
        return
    base = os.environ.get("SVN_OBSERVE_URL", "").rstrip("/")
    token = os.environ.get("SVN_OBSERVE_TOKEN", "")
    if not base or not token:
        return
    body = json.dumps({"grant_id": grant_id}).encode("utf-8")
    _post(base + "/v1/release", token, body)


def _object_dict(raw: object) -> dict[str, object] | None:
    if not isinstance(raw, dict):
        return None
    out: dict[str, object] = {}
    for key, value in raw.items():
        if isinstance(key, str):
            out[key] = value
    return out


def _post(url: str, token: str, body: bytes) -> tuple[dict[str, object], str]:
    request = urllib.request.Request(
        url,
        data=body,
        method="POST",
        headers={
            "Authorization": "Bearer " + token,
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=2) as response:
            parsed: object = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", "replace")
        try:
            parsed = json.loads(raw)
        except json.JSONDecodeError:
            return {}, "unreachable"
        if exc.code == 429:
            refused = _object_dict(parsed)
            if refused is None:
                return {}, "unreachable"
            return refused, "refused"
        return {}, "unreachable"
    except (OSError, urllib.error.URLError, json.JSONDecodeError, TimeoutError):
        return {}, "unreachable"
    payload = _object_dict(parsed)
    if payload is None:
        return {}, "unreachable"
    return payload, "ok"
