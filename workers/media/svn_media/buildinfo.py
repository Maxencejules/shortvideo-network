"""Print the FFmpeg build that this host actually runs.

The text is a record of that binary. It is not an approval of the licence
and it does not settle codec patent obligations.
"""

from __future__ import annotations

import subprocess
from pathlib import Path


def build_record(ffmpeg: str = "ffmpeg") -> str:
    parts: list[str] = []
    for args in (
        [ffmpeg, "-version"],
        [ffmpeg, "-buildconf"],
        [ffmpeg, "-codecs"],
    ):
        proc = subprocess.run(args, capture_output=True, text=True, check=False)
        text = proc.stdout
        if args[-1] == "-codecs":
            kept = [
                line
                for line in text.splitlines()
                if any(token in line.lower() for token in ("264", "aac", "mjpeg", "jpeg"))
            ]
            text = "\n".join(kept)
        parts.append("$ " + " ".join(args) + "\n" + text.rstrip())
    notice = Path("/usr/share/doc/ffmpeg/copyright")
    if notice.is_file():
        head = "\n".join(notice.read_text(encoding="utf-8", errors="replace").splitlines()[:60])
        parts.append("copyright-head\n" + head)
    parts.append(
        "This Ubuntu package reports a GPL build when --enable-gpl is present. "
        "A GPL build record does not settle codec patent obligations. DEC-006 is open."
    )
    return "\n\n".join(parts) + "\n"
