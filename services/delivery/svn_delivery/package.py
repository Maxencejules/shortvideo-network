"""Validate a goal 14 package directory before it is advertised.

The manifest schema is the frozen file. The codecs and the version rule are
still the goal 14 proposal (DR-14-02). This module does not approve them.
"""

from __future__ import annotations

import hashlib
import json
import math
import subprocess
from dataclasses import dataclass
from pathlib import Path

from svn_delivery.errors import DeliveryError
from svn_delivery.ids import COUNT, ID, PACKAGE_CAP, SHA, SUFFIX
from svn_protocol.schemas import SchemaError, validate_manifest

_PROBES: dict[str, Probe] = {}


@dataclass(frozen=True)
class Probe:
    width: int
    height: int
    duration: float
    codecs: str


@dataclass(frozen=True)
class Segment:
    segment_id: str
    codec: str
    sha256: str
    byte_count: int
    rel: str
    path: Path


@dataclass(frozen=True)
class VideoRendition:
    rendition_id: str
    bandwidth: int
    width: int
    height: int
    codecs: str
    duration: float
    durations: tuple[float, ...]
    segments: tuple[Segment, ...]


@dataclass(frozen=True)
class PackageView:
    content_id: str
    content_version: int
    manifest_sha256: str
    package_bytes: int
    videos: tuple[VideoRendition, ...]
    captions: Segment | None
    thumb: Segment | None
    directory: Path

    @property
    def fallback_rendition(self) -> str:
        return self.videos[0].rendition_id

    def by_rel(self) -> dict[str, Segment]:
        found: dict[str, Segment] = {}
        for video in self.videos:
            for segment in video.segments:
                found[segment.rel] = segment
        if self.captions is not None:
            found[self.captions.rel] = self.captions
        if self.thumb is not None:
            found[self.thumb.rel] = self.thumb
        return found


def load_ready_record(root: Path, content_id: str) -> dict[str, object] | None:
    """The highest ready-for-review record for `content_id`, or None."""
    if not ID.fullmatch(content_id):
        raise DeliveryError("BAD_REQUEST")
    best: dict[str, object] | None = None
    version = -1
    folder = root / "jobs" / "ready"
    if not folder.is_dir():
        return None
    for path in sorted(folder.glob("*.json")):
        if path.is_symlink():
            continue
        try:
            value = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, UnicodeError, json.JSONDecodeError):
            continue
        if not isinstance(value, dict) or value.get("content_id") != content_id:
            continue
        if value.get("status") != "READY_FOR_REVIEW":
            continue
        if value.get("publication") != "NOT_PUBLISHED":
            continue
        current = value.get("content_version")
        if not isinstance(current, int) or isinstance(current, bool):
            continue
        if current >= version:
            version = current
            best = value
    return best


def validate_directory(
    directory: Path,
    *,
    manifest_sha256: str,
    ffprobe: str = "ffprobe",
) -> PackageView:
    """Hash the manifest and every segment. Probe video bytes for the playlist."""
    if not SHA.fullmatch(manifest_sha256):
        raise DeliveryError("INTEGRITY")
    manifest_path = directory / "manifest.json"
    if manifest_path.is_symlink() or not manifest_path.is_file():
        raise DeliveryError("INTEGRITY")
    try:
        raw = manifest_path.read_bytes()
    except OSError:
        raise DeliveryError("INTEGRITY") from None
    if raw.endswith(b"\n") or len(raw) > 65536:
        raise DeliveryError("INTEGRITY")
    digest = hashlib.sha256(raw).hexdigest()
    if digest != manifest_sha256:
        raise DeliveryError("INTEGRITY")
    try:
        parsed = json.loads(raw.decode("utf-8"))
    except (UnicodeError, json.JSONDecodeError):
        raise DeliveryError("INTEGRITY") from None
    try:
        validate_manifest(parsed)
    except SchemaError:
        raise DeliveryError("INTEGRITY") from None
    if not isinstance(parsed, dict):
        raise DeliveryError("INTEGRITY")
    canonical = json.dumps(parsed, sort_keys=True, separators=(",", ":"), ensure_ascii=True)
    if canonical.encode("utf-8") != raw:
        raise DeliveryError("INTEGRITY")
    content_id = parsed["content_id"]
    content_version = parsed["content_version"]
    if not isinstance(content_id, str) or not ID.fullmatch(content_id):
        raise DeliveryError("INTEGRITY")
    if not isinstance(content_version, int) or isinstance(content_version, bool):
        raise DeliveryError("INTEGRITY")
    renditions = parsed["renditions"]
    if not isinstance(renditions, list):
        raise DeliveryError("INTEGRITY")
    segments_root = directory / "segments"
    if segments_root.is_symlink() or not segments_root.is_dir():
        raise DeliveryError("INTEGRITY")
    videos: list[VideoRendition] = []
    captions: Segment | None = None
    thumb: Segment | None = None
    total = 0
    seen_rendition: set[str] = set()
    seen_rel: set[str] = set()
    for rendition in renditions:
        if not isinstance(rendition, dict):
            raise DeliveryError("INTEGRITY")
        rendition_id = rendition.get("rendition_id")
        codec = rendition.get("codec")
        rows = rendition.get("segments")
        if (
            not isinstance(rendition_id, str)
            or not ID.fullmatch(rendition_id)
            or rendition_id in seen_rendition
            or not isinstance(codec, str)
            or codec not in SUFFIX
            or not isinstance(rows, list)
        ):
            raise DeliveryError("INTEGRITY")
        seen_rendition.add(rendition_id)
        built: list[Segment] = []
        for row in rows:
            segment = _segment(segments_root, rendition_id, codec, row, seen_rel)
            total += segment.byte_count
            if total > PACKAGE_CAP:
                raise DeliveryError("INTEGRITY")
            built.append(segment)
        if codec == "avc1":
            videos.append(_video(rendition_id, tuple(built), ffprobe))
        elif codec == "webvtt":
            if captions is not None or len(built) != 1:
                raise DeliveryError("INTEGRITY")
            captions = built[0]
        else:
            if thumb is not None or len(built) != 1:
                raise DeliveryError("INTEGRITY")
            thumb = built[0]
    if str(total) != str(parsed.get("package_bytes")) or not videos:
        raise DeliveryError("INTEGRITY")
    videos.sort(key=lambda item: (item.bandwidth, item.rendition_id))
    return PackageView(
        content_id=content_id,
        content_version=content_version,
        manifest_sha256=digest,
        package_bytes=total,
        videos=tuple(videos),
        captions=captions,
        thumb=thumb,
        directory=directory,
    )


def read_checked(segment: Segment) -> bytes:
    """Return the segment bytes after hashing the buffer that will be sent."""
    path = segment.path
    if path.is_symlink():
        raise DeliveryError("INTEGRITY")
    try:
        resolved = path.resolve()
        root = path.parent.resolve()
    except OSError:
        raise DeliveryError("INTEGRITY") from None
    if not resolved.is_relative_to(root) or not resolved.is_file():
        raise DeliveryError("INTEGRITY")
    try:
        size = resolved.stat().st_size
    except OSError:
        raise DeliveryError("INTEGRITY") from None
    if size != segment.byte_count or size < 1 or size > PACKAGE_CAP:
        raise DeliveryError("INTEGRITY")
    try:
        data = resolved.read_bytes()
    except OSError:
        raise DeliveryError("INTEGRITY") from None
    if len(data) != segment.byte_count or hashlib.sha256(data).hexdigest() != segment.sha256:
        raise DeliveryError("INTEGRITY")
    return data


def _segment(
    root: Path,
    rendition_id: str,
    codec: str,
    row: object,
    seen_rel: set[str],
) -> Segment:
    if not isinstance(row, dict):
        raise DeliveryError("INTEGRITY")
    segment_id = row.get("segment_id")
    digest = row.get("sha256")
    count = row.get("byte_count")
    if (
        not isinstance(segment_id, str)
        or not ID.fullmatch(segment_id)
        or not segment_id.startswith(rendition_id + "-")
        or not isinstance(digest, str)
        or not SHA.fullmatch(digest)
        or not isinstance(count, str)
        or not COUNT.fullmatch(count)
    ):
        raise DeliveryError("INTEGRITY")
    byte_count = int(count)
    if byte_count < 1 or byte_count > PACKAGE_CAP:
        raise DeliveryError("INTEGRITY")
    rel = f"segments/{segment_id}{SUFFIX[codec]}"
    if rel in seen_rel or ".." in rel.split("/"):
        raise DeliveryError("INTEGRITY")
    seen_rel.add(rel)
    path = root / f"{segment_id}{SUFFIX[codec]}"
    segment = Segment(segment_id, codec, digest, byte_count, rel, path)
    read_checked(segment)
    return segment


def _video(rendition_id: str, segments: tuple[Segment, ...], ffprobe: str) -> VideoRendition:
    probes = [_probe(segment, ffprobe) for segment in segments]
    width = probes[0].width
    height = probes[0].height
    codecs = probes[0].codecs
    if any(
        item.width != width or item.height != height or item.codecs != codecs for item in probes
    ):
        raise DeliveryError("INTEGRITY")
    durations = tuple(item.duration for item in probes)
    duration = sum(durations)
    if duration <= 0:
        raise DeliveryError("INTEGRITY")
    total_bytes = sum(segment.byte_count for segment in segments)
    bandwidth = max(1, math.ceil((total_bytes * 8) / duration))
    return VideoRendition(
        rendition_id, bandwidth, width, height, codecs, duration, durations, segments
    )


def _probe(segment: Segment, ffprobe: str) -> Probe:
    cached = _PROBES.get(segment.sha256)
    if cached is not None:
        return cached
    argv = [
        ffprobe,
        "-v",
        "error",
        "-hide_banner",
        "-show_format",
        "-show_streams",
        "-of",
        "json",
        "-protocol_whitelist",
        "file,crypto,pipe",
        str(segment.path),
    ]
    try:
        result = subprocess.run(
            argv,
            capture_output=True,
            check=False,
            timeout=15,
        )
    except FileNotFoundError:
        raise DeliveryError("UNAVAILABLE") from None
    except subprocess.TimeoutExpired:
        raise DeliveryError("INTEGRITY") from None
    if result.returncode != 0:
        raise DeliveryError("INTEGRITY")
    try:
        document = json.loads(result.stdout.decode("utf-8"))
    except (UnicodeError, json.JSONDecodeError):
        raise DeliveryError("INTEGRITY") from None
    info = _probe_document(document)
    if len(_PROBES) > 64:
        _PROBES.clear()
    _PROBES[segment.sha256] = info
    return info


def _probe_document(document: object) -> Probe:
    if not isinstance(document, dict):
        raise DeliveryError("INTEGRITY")
    streams = document.get("streams")
    fmt = document.get("format")
    if not isinstance(streams, list) or not isinstance(fmt, dict):
        raise DeliveryError("INTEGRITY")
    video = next(
        (item for item in streams if isinstance(item, dict) and item.get("codec_type") == "video"),
        None,
    )
    if not isinstance(video, dict) or video.get("codec_name") != "h264":
        raise DeliveryError("INTEGRITY")
    width = video.get("width")
    height = video.get("height")
    if not isinstance(width, int) or not isinstance(height, int) or width < 2 or height < 2:
        raise DeliveryError("INTEGRITY")
    if width > 7680 or height > 7680:
        raise DeliveryError("INTEGRITY")
    duration = _duration(fmt.get("duration"))
    audio = next(
        (item for item in streams if isinstance(item, dict) and item.get("codec_type") == "audio"),
        None,
    )
    codecs = _codecs(video, audio)
    return Probe(width, height, duration, codecs)


def _duration(value: object) -> float:
    try:
        duration = float(str(value))
    except (TypeError, ValueError):
        raise DeliveryError("INTEGRITY") from None
    if duration <= 0 or duration > 120:
        raise DeliveryError("INTEGRITY")
    return duration


def _codecs(video: dict[str, object], audio: dict[str, object] | None) -> str:
    profile = video.get("profile")
    level = video.get("level")
    names = {
        "Baseline": "4200",
        "Constrained Baseline": "4240",
        "Main": "4d00",
        "High": "6400",
    }
    if not isinstance(profile, str) or profile not in names or not isinstance(level, int):
        video_codec = ""
    elif level < 10 or level > 62:
        video_codec = ""
    else:
        video_codec = f"avc1.{names[profile]}{level:02x}"
    if audio is None:
        return video_codec
    if audio.get("codec_name") == "aac" and audio.get("profile") == "LC" and video_codec:
        return video_codec + ",mp4a.40.2"
    return ""
