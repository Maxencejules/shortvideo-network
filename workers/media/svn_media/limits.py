"""Proposed processing caps. A label other than PROPOSED is refused.

The numbers are the blueprint's proposed bounds and the protocol package-byte
cap. They are not a DEC-005 allowance and not an approved capacity claim.
"""

from __future__ import annotations

import re

# Decimal megabytes in the blueprint are a different unit. This is the byte ceiling.
PROPOSED_MAX_INPUT_BYTES = 100_000_000
PROPOSED_MAX_DURATION_SECONDS = 60.0
PROPOSED_MAX_DIMENSION = 7680
# docs/protocol.md safety cap. Not a sponsorship allowance.
PROTOCOL_PACKAGE_BYTES = 268_435_456
MAX_CAPTION_BYTES = 65_536

_ID = re.compile(r"^[a-z0-9][a-z0-9._-]{0,63}$")
_OBJECT = re.compile(r"^[0-9a-f]{64}$")
_SHA = re.compile(r"^[0-9a-f]{64}$")

USER_MESSAGES = {
    "MALFORMED": "The file is not a supported video.",
    "TRUNCATED": "The file is incomplete.",
    "OVER_DURATION": "The video is longer than the allowed duration.",
    "OVER_DIMENSION": "The video is larger than the allowed dimensions.",
    "OVER_BUDGET": "The video exceeds a processing limit.",
    "OUTPUT_GROWTH": "The processed output is larger than the allowed budget.",
    "CAPTION_REJECTED": "The captions are not a supported text file.",
    "SYNC": "The processed audio and video do not match.",
    "UNSUPPORTED": "The video uses an unsupported format.",
    "INPUT_DIGEST": "The file does not match the uploaded bytes.",
    "CANCELLED": "Processing was cancelled.",
    "REFUSED": "Processing was refused.",
    "INTERNAL": "Processing failed.",
}


class Failure(Exception):
    """A processing failure. The message is the user text for `code`."""

    def __init__(self, code: str) -> None:
        if code not in USER_MESSAGES:
            code = "INTERNAL"
        self.code = code
        super().__init__(USER_MESSAGES[code])


class Limits:
    """Resource and media caps. `label` stays PROPOSED until an owner decision."""

    def __init__(
        self,
        *,
        label: str = "PROPOSED",
        max_input_bytes: int = PROPOSED_MAX_INPUT_BYTES,
        max_duration_seconds: float = PROPOSED_MAX_DURATION_SECONDS,
        max_dimension: int = PROPOSED_MAX_DIMENSION,
        wall_seconds: float = 180.0,
        cpu_seconds: int = 240,
        memory_bytes: int = 2 * 1024 * 1024 * 1024,
    ) -> None:
        self.label = label
        self.max_input_bytes = max_input_bytes
        self.max_duration_seconds = max_duration_seconds
        self.max_dimension = max_dimension
        self.wall_seconds = wall_seconds
        self.cpu_seconds = cpu_seconds
        self.memory_bytes = memory_bytes
        self._check()

    def _check(self) -> None:
        if self.label != "PROPOSED":
            raise Failure("REFUSED")
        if self.max_input_bytes < 1 or self.max_input_bytes > PROPOSED_MAX_INPUT_BYTES:
            raise Failure("REFUSED")
        duration = self.max_duration_seconds
        if duration <= 0 or duration > PROPOSED_MAX_DURATION_SECONDS:
            raise Failure("REFUSED")
        if self.max_dimension < 1 or self.max_dimension > PROPOSED_MAX_DIMENSION:
            raise Failure("REFUSED")
        if self.wall_seconds <= 0 or self.cpu_seconds < 1 or self.memory_bytes < 1:
            raise Failure("REFUSED")


def check_id(value: str) -> None:
    if not _ID.fullmatch(value):
        raise Failure("REFUSED")


def check_object_name(value: str) -> None:
    if not _OBJECT.fullmatch(value):
        raise Failure("REFUSED")


def check_sha256(value: str) -> None:
    if not _SHA.fullmatch(value):
        raise Failure("REFUSED")
