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
    base = os.environ.get("SVN_OBSERVE_URL", "").rstrip("/")
    token = os.environ.get("SVN_OBSERVE_TOKEN", "")
    if not base or not token:
        return "unreachable"
    body = json.dumps(
        {"kind": "processing_concurrency", "units": 1, "idempotency_key": key}
    ).encode("utf-8")
    request = urllib.request.Request(
        base + "/v1/admit",
        data=body,
        method="POST",
        headers={
            "Authorization": "Bearer " + token,
            "Content-Type": "application/json",
        },
    )
    try:
        with urllib.request.urlopen(request, timeout=2) as response:
            payload = json.loads(response.read().decode("utf-8"))
    except urllib.error.HTTPError as exc:
        raw = exc.read().decode("utf-8", "replace")
        try:
            payload = json.loads(raw)
        except json.JSONDecodeError:
            return "unreachable"
        if exc.code == 429:
            return "refused"
        return "unreachable"
    except (OSError, urllib.error.URLError, json.JSONDecodeError, TimeoutError):
        return "unreachable"
    if payload.get("granted") is True:
        return "granted"
    return "refused"
