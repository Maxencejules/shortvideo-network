"""Isolated media-worker slot. Goal 14 runs FFmpeg. This module does not."""

SCAFFOLD = True


def transcode(source: str) -> None:
    """Refuse to process media. `source` is not read and is not passed to a shell."""
    del source
    raise RuntimeError("workers/media is scaffolding and does not run FFmpeg")
