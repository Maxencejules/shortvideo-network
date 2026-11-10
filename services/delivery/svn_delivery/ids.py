"""Identifiers shared with the frozen media-manifest schema."""

from __future__ import annotations

import re

ID = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
SHA = re.compile(r"^[0-9a-f]{64}$")
# Decimal byte counts in the manifest. No leading zero except the value zero.
COUNT = re.compile(r"^(0|[1-9][0-9]*)$")

# The protocol package cap. A larger file is not a segment we will send.
PACKAGE_CAP = 268_435_456

# PROPOSED bounds. They are not an approved allowance. DR-15-01 and DR-15-04.
PREFETCH_SEGMENTS = 1
PREFETCH_ITEMS = 1
ATTEMPTS_PER_HOST = 2
MAX_HOSTS = 4
# PROPOSED client-cache lifetime for an immutable segment URL. DR-15-03.
SEGMENT_CACHE_SECONDS = 60
# PROPOSED real-time playback term. DR-15-05. Not chain time.
MAX_TERM_SECONDS = 24 * 60 * 60

ADAPTER_NAME = "origin-test"
ADAPTER_VERSION = 1
ALLOWED_ORIGIN = "https://playback.test.local"
# These stay off. Goal 16 does not turn them on, and this goal does not either.
DISABLED = ("ipfs", "p2p", "turn", "webrtc")

CHAIN_NOTES = ("HELD", "STALE", "UNAVAILABLE")
SUFFIX = {"avc1": ".mp4", "jpeg": ".jpg", "webvtt": ".vtt"}
CONTENT_TYPE = {
    ".m3u8": "application/vnd.apple.mpegurl; charset=utf-8",
    ".mp4": "video/mp4",
    ".jpg": "image/jpeg",
    ".vtt": "text/vtt; charset=utf-8",
    ".json": "application/json; charset=utf-8",
    ".html": "text/html; charset=utf-8",
}
