"""Fixed delivery errors. The message is the user text for `code`."""

from __future__ import annotations

MESSAGES = {
    "NOT_FOUND": "The media is not available.",
    "INTEGRITY": "The package does not match its manifest.",
    "REFUSED": "Playback was refused.",
    "UNAVAILABLE": "Playback is not available.",
    "BAD_REQUEST": "The request is not valid.",
    "RANGE": "The requested range is not valid.",
    "CANCELLED": "The request was cancelled.",
}

HTTP_STATUS = {
    "NOT_FOUND": 404,
    "INTEGRITY": 409,
    "REFUSED": 403,
    "UNAVAILABLE": 503,
    "BAD_REQUEST": 400,
    "RANGE": 416,
    "CANCELLED": 409,
}


class DeliveryError(Exception):
    """A delivery failure. `code` selects the fixed message and HTTP status."""

    def __init__(self, code: str) -> None:
        if code not in MESSAGES:
            code = "UNAVAILABLE"
        self.code = code
        super().__init__(MESSAGES[code])

    @property
    def status(self) -> int:
        return HTTP_STATUS[self.code]
