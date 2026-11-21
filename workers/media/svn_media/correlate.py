"""Correlation ids for media jobs.

The id is random. It is not stored in the job file and it is not an account id.
"""

from __future__ import annotations

import re
import secrets

_ID = re.compile(r"^c[0-9a-f]{32}$")
_EMAIL = re.compile(r"[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}")
_CANARY = re.compile(r"SVN-SYNTHETIC-CANARY-[0-9A-Za-z-]+")
_BEARER = re.compile(r"(?i)bearer\s+\S+")
_ACCOUNT = re.compile(r"\bacc_[0-9A-Za-z]{6,}\b")


def new_id() -> str:
    """Return one correlation id."""
    return "c" + secrets.token_hex(16)


def valid(value: str) -> bool:
    """Report whether value is a correlation id."""
    return _ID.fullmatch(value) is not None


def redact(text: str) -> str:
    """Remove synthetic canaries, bearer values, addresses, and account ids."""
    text = _BEARER.sub("bearer [redacted]", text)
    text = _CANARY.sub("[redacted]", text)
    text = _EMAIL.sub("[redacted]", text)
    return _ACCOUNT.sub("[redacted]", text)
