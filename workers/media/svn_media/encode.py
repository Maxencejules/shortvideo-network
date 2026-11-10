"""Probe a quarantine file and write one package version.

FFmpeg is invoked with an argument list, generated paths, and a protocol
whitelist of file, crypto, and pipe. Caption bytes are copied as data. They
are not placed on the command line. A new call writes a new directory. It does
not require another encoder to produce the same bytes.
"""

from __future__ import annotations

import hashlib
import json
import os
import time
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path

from svn_media.isolate import Bounds, run
from svn_media.limits import (
    MAX_CAPTION_BYTES,
    PROTOCOL_PACKAGE_BYTES,
    Failure,
    Limits,
)
from svn_protocol.schemas import SchemaError, validate_manifest

_LADDER = (("h480", 480, 854), ("h720", 720, 1280))
_CODECS = {"h480": "avc1", "h720": "avc1", "native": "avc1", "thumb": "jpeg", "captions": "webvtt"}


@dataclass(frozen=True)
class Probe:
    duration: float
    display_width: int
    display_height: int
    has_audio: bool
    rotation: int
    matrix: bool


@dataclass(frozen=True)
class Package:
    manifest: dict[str, object]
    canonical: str
    manifest_sha256: str
    package_bytes: int
    duration: float


def process_input(
    source: Path,
    dest: Path,
    *,
    limits: Limits,
    ffmpeg: str,
    ffprobe: str,
    content_id: str,
    content_version: int,
    manifest_id: str,
    output_budget: int,
    expected_sha256: str | None,
    captions: bytes | None,
    tick: Callable[[], None] | None = None,
) -> Package:
    """Encode `source` into `dest`. `tick` runs between outputs so a caller can cancel."""
    step = tick or (lambda: None)
    step()
    if output_budget < 1 or output_budget > PROTOCOL_PACKAGE_BYTES:
        raise Failure("OUTPUT_GROWTH")
    _captions(captions)
    data = _read_bounded(source, limits.max_input_bytes)
    if expected_sha256 is not None and hashlib.sha256(data).hexdigest() != expected_sha256:
        raise Failure("INPUT_DIGEST")
    if data[4:8] != b"ftyp":
        raise Failure("MALFORMED")
    bounds = Bounds(
        wall_seconds=limits.wall_seconds,
        cpu_seconds=limits.cpu_seconds,
        memory_bytes=limits.memory_bytes,
        file_bytes=output_budget,
    )
    deadline = time.monotonic() + limits.wall_seconds
    probe = _probe(source, ffprobe, dest, bounds, deadline, limits)
    step()
    plan = _plan(probe)
    segments = dest / "segments"
    segments.mkdir(mode=0o700)
    written: list[tuple[str, str, Path]] = []
    for rendition_id, width, height in plan:
        step()
        target = segments / f"{rendition_id}-0.mp4"
        _encode_video(
            source,
            target,
            ffmpeg=ffmpeg,
            cwd=dest,
            bounds=bounds,
            deadline=deadline,
            probe=probe,
            width=width,
            height=height,
        )
        _check_video(target, ffprobe, dest, bounds, deadline, probe, width, height)
        written.append((rendition_id, "avc1", target))
        step()
    thumb = segments / "thumb-0.jpg"
    tw, th = _thumb_size(probe.display_width, probe.display_height)
    _encode_thumb(
        source,
        thumb,
        ffmpeg=ffmpeg,
        cwd=dest,
        bounds=bounds,
        deadline=deadline,
        probe=probe,
        width=tw,
        height=th,
    )
    if thumb.stat().st_size < 3 or thumb.read_bytes()[:3] != b"\xff\xd8\xff":
        raise Failure("SYNC")
    written.append(("thumb", "jpeg", thumb))
    step()
    if captions is not None:
        cap = segments / "captions-0.vtt"
        _write_file(cap, captions)
        written.append(("captions", "webvtt", cap))
    manifest, canonical, digest, total = _manifest(
        written,
        content_id=content_id,
        content_version=content_version,
        manifest_id=manifest_id,
        output_budget=output_budget,
    )
    _write_file(dest / "manifest.json", canonical.encode("utf-8"))
    _fsync_tree(dest)
    return Package(manifest, canonical, digest, total, probe.duration)


def _captions(captions: bytes | None) -> None:
    if captions is None:
        return
    if len(captions) < 6 or len(captions) > MAX_CAPTION_BYTES or b"\x00" in captions:
        raise Failure("CAPTION_REJECTED")
    try:
        text = captions.decode("utf-8")
    except UnicodeDecodeError:
        raise Failure("CAPTION_REJECTED") from None
    if text.startswith("\ufeff") or not text.startswith("WEBVTT"):
        raise Failure("CAPTION_REJECTED")


def _read_bounded(path: Path, limit: int) -> bytes:
    try:
        size = path.stat().st_size
    except OSError:
        raise Failure("MALFORMED") from None
    if size < 12:
        raise Failure("MALFORMED")
    if size > limit:
        raise Failure("OVER_BUDGET")
    try:
        data = path.read_bytes()
    except OSError:
        raise Failure("MALFORMED") from None
    if len(data) != size or len(data) > limit:
        raise Failure("OVER_BUDGET")
    return data


def _remaining(deadline: float) -> float:
    left = deadline - time.monotonic()
    if left <= 0:
        raise Failure("OVER_BUDGET")
    return left


def _probe(
    source: Path,
    ffprobe: str,
    cwd: Path,
    bounds: Bounds,
    deadline: float,
    limits: Limits,
) -> Probe:
    argv = [
        ffprobe,
        "-v",
        "error",
        "-hide_banner",
        "-count_frames",
        "-show_format",
        "-show_streams",
        "-of",
        "json",
        "-protocol_whitelist",
        "file,crypto,pipe",
        str(source),
    ]
    result = run(argv, cwd=cwd, bounds=_with_wall(bounds, _remaining(deadline)))
    if result.returncode != 0 or result.stdout_truncated:
        raise Failure("TRUNCATED")
    try:
        document = json.loads(result.stdout)
    except json.JSONDecodeError:
        raise Failure("TRUNCATED") from None
    return _interpret(document, limits)


def _interpret(document: object, limits: Limits) -> Probe:
    if not isinstance(document, dict):
        raise Failure("MALFORMED")
    fmt = document.get("format")
    streams = document.get("streams")
    if not isinstance(fmt, dict) or not isinstance(streams, list):
        raise Failure("MALFORMED")
    names = str(fmt.get("format_name", "")).split(",")
    if "mp4" not in names and "mov" not in names:
        raise Failure("UNSUPPORTED")
    video: dict[str, object] | None = None
    audio = False
    for stream in streams:
        if not isinstance(stream, dict):
            continue
        if stream.get("codec_type") == "audio":
            audio = True
        if stream.get("codec_type") == "video" and video is None:
            video = stream
    if video is None:
        raise Failure("MALFORMED")
    width = _positive_int(video.get("width"))
    height = _positive_int(video.get("height"))
    if width is None or height is None:
        raise Failure("MALFORMED")
    rotation, matrix = _rotation(video)
    display_w, display_h = (height, width) if rotation in (90, 270) else (width, height)
    if display_w > limits.max_dimension or display_h > limits.max_dimension:
        raise Failure("OVER_DIMENSION")
    duration = _duration(fmt.get("duration"))
    counted = _frame_duration(video.get("nb_read_frames"), video.get("avg_frame_rate"))
    if counted is not None and counted > duration:
        duration = counted
    if duration <= 0:
        raise Failure("TRUNCATED")
    if duration > limits.max_duration_seconds:
        raise Failure("OVER_DURATION")
    return Probe(duration, display_w, display_h, audio, rotation, matrix)


def _positive_int(value: object) -> int | None:
    if isinstance(value, bool) or not isinstance(value, int):
        return None
    if value < 1:
        return None
    return value


def _duration(value: object) -> float:
    try:
        parsed = float(str(value))
    except (TypeError, ValueError):
        raise Failure("TRUNCATED") from None
    if parsed != parsed or parsed < 0:  # NaN
        raise Failure("TRUNCATED")
    return parsed


def _frame_duration(frames: object, rate: object) -> float | None:
    try:
        count = int(str(frames))
        num_text, den_text = str(rate).split("/", 1)
        num = float(num_text)
        den = float(den_text)
    except (TypeError, ValueError):
        return None
    if count < 1 or num <= 0 or den <= 0:
        return None
    return count / (num / den)


def _rotation(stream: dict[str, object]) -> tuple[int, bool]:
    sides = stream.get("side_data_list")
    if isinstance(sides, list):
        for side in sides:
            if isinstance(side, dict) and "rotation" in side:
                try:
                    angle = int(round(float(str(side["rotation"])))) % 360
                except (TypeError, ValueError):
                    raise Failure("MALFORMED") from None
                return angle, True
    tags = stream.get("tags")
    if isinstance(tags, dict) and "rotate" in tags:
        try:
            angle = int(round(float(str(tags["rotate"])))) % 360
        except (TypeError, ValueError):
            raise Failure("MALFORMED") from None
        return angle, False
    return 0, False


def _plan(probe: Probe) -> list[tuple[str, int, int]]:
    chosen: list[tuple[str, int, int]] = []
    for name, short_target, long_cap in _LADDER:
        fitted = _fit(probe.display_width, probe.display_height, short_target, long_cap)
        if fitted is not None:
            chosen.append((name, fitted[0], fitted[1]))
    if not chosen:
        width = _even(probe.display_width)
        height = _even(probe.display_height)
        if width < 2 or height < 2 or width > probe.display_width or height > probe.display_height:
            raise Failure("UNSUPPORTED")
        chosen.append(("native", width, height))
    return chosen


def _fit(src_w: int, src_h: int, short_target: int, long_cap: int) -> tuple[int, int] | None:
    short = min(src_w, src_h)
    long = max(src_w, src_h)
    if short < short_target:
        return None
    scale = min(short_target / short, long_cap / long)
    if scale > 1:
        return None
    out_w = _even(int(src_w * scale))
    out_h = _even(int(src_h * scale))
    if out_w < 2 or out_h < 2 or out_w > src_w or out_h > src_h:
        return None
    return out_w, out_h


def _thumb_size(src_w: int, src_h: int) -> tuple[int, int]:
    fitted = _fit(src_w, src_h, 240, 426)
    if fitted is not None:
        return fitted
    return _even(src_w), _even(src_h)


def _even(value: int) -> int:
    return value if value % 2 == 0 else value - 1


def _transpose(probe: Probe) -> str:
    """Bake a rotate tag when FFmpeg has no display matrix to autorotate."""
    if probe.matrix or probe.rotation == 0:
        return ""
    if probe.rotation == 90:
        return "transpose=1"
    if probe.rotation == 270:
        return "transpose=2"
    if probe.rotation == 180:
        return "hflip,vflip"
    raise Failure("UNSUPPORTED")


def _filter(probe: Probe, width: int, height: int, scale: bool) -> str:
    parts: list[str] = []
    transpose = _transpose(probe)
    if transpose:
        parts.append(transpose)
    if scale:
        parts.append(f"scale={width}:{height}:flags=bicubic")
    parts.append("format=yuv420p")
    return ",".join(parts)


def video_arguments(
    ffmpeg: str,
    source: Path,
    target: Path,
    *,
    probe: Probe,
    width: int,
    height: int,
) -> list[str]:
    """The FFmpeg argument list for one rendition. Caption text is not included."""
    argv = [
        ffmpeg,
        "-hide_banner",
        "-nostdin",
        "-y",
        "-protocol_whitelist",
        "file,crypto,pipe",
    ]
    if _transpose(probe):
        argv.append("-noautorotate")
    argv.extend(
        [
            "-i",
            str(source),
            "-map",
            "0:v:0",
            "-sn",
            "-dn",
            "-vf",
            _filter(probe, width, height, True),
            "-c:v",
            "libx264",
            "-preset",
            "veryfast",
            "-crf",
            "28",
            "-pix_fmt",
            "yuv420p",
        ]
    )
    if probe.has_audio:
        argv.extend(["-map", "0:a:0", "-c:a", "aac", "-b:a", "64k", "-ac", "2"])
    else:
        argv.append("-an")
    argv.extend(
        ["-t", f"{probe.duration:.3f}", "-movflags", "+faststart", "-f", "mp4", str(target)]
    )
    return argv


def _encode_video(
    source: Path,
    target: Path,
    *,
    ffmpeg: str,
    cwd: Path,
    bounds: Bounds,
    deadline: float,
    probe: Probe,
    width: int,
    height: int,
) -> None:
    argv = video_arguments(ffmpeg, source, target, probe=probe, width=width, height=height)
    result = run(argv, cwd=cwd, bounds=_with_wall(bounds, _remaining(deadline)))
    if result.returncode != 0 or not target.is_file():
        raise Failure("TRUNCATED")


def _encode_thumb(
    source: Path,
    target: Path,
    *,
    ffmpeg: str,
    cwd: Path,
    bounds: Bounds,
    deadline: float,
    probe: Probe,
    width: int,
    height: int,
) -> None:
    argv = [
        ffmpeg,
        "-hide_banner",
        "-nostdin",
        "-y",
        "-protocol_whitelist",
        "file,crypto,pipe",
    ]
    if _transpose(probe):
        argv.append("-noautorotate")
    argv.extend(
        [
            "-i",
            str(source),
            "-map",
            "0:v:0",
            "-sn",
            "-dn",
            "-vf",
            _filter(probe, width, height, True),
            "-frames:v",
            "1",
            "-q:v",
            "5",
            str(target),
        ]
    )
    result = run(argv, cwd=cwd, bounds=_with_wall(bounds, _remaining(deadline)))
    if result.returncode != 0 or not target.is_file():
        raise Failure("TRUNCATED")


def _check_video(
    path: Path,
    ffprobe: str,
    cwd: Path,
    bounds: Bounds,
    deadline: float,
    probe: Probe,
    width: int,
    height: int,
) -> None:
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
        str(path),
    ]
    result = run(argv, cwd=cwd, bounds=_with_wall(bounds, _remaining(deadline)))
    if result.returncode != 0 or result.stdout_truncated:
        raise Failure("SYNC")
    try:
        document = json.loads(result.stdout)
    except json.JSONDecodeError:
        raise Failure("SYNC") from None
    if not isinstance(document, dict):
        raise Failure("SYNC")
    streams = document.get("streams")
    fmt = document.get("format")
    if not isinstance(streams, list) or not isinstance(fmt, dict):
        raise Failure("SYNC")
    video = next(
        (item for item in streams if isinstance(item, dict) and item.get("codec_type") == "video"),
        None,
    )
    audio = any(isinstance(item, dict) and item.get("codec_type") == "audio" for item in streams)
    if not isinstance(video, dict) or video.get("codec_name") != "h264":
        raise Failure("SYNC")
    if video.get("width") != width or video.get("height") != height:
        raise Failure("SYNC")
    if audio != probe.has_audio:
        raise Failure("SYNC")
    duration = _duration(fmt.get("duration"))
    if abs(duration - probe.duration) > 1.0:
        raise Failure("SYNC")
    if width > probe.display_width or height > probe.display_height:
        raise Failure("SYNC")


def _manifest(
    written: list[tuple[str, str, Path]],
    *,
    content_id: str,
    content_version: int,
    manifest_id: str,
    output_budget: int,
) -> tuple[dict[str, object], str, str, int]:
    groups: dict[str, dict[str, object]] = {}
    total = 0
    for rendition_id, codec, path in written:
        if codec != _CODECS[rendition_id]:
            raise Failure("INTERNAL")
        payload = path.read_bytes()
        digest = hashlib.sha256(payload).hexdigest()
        size = len(payload)
        total += size
        if size < 1 or total > output_budget or total > PROTOCOL_PACKAGE_BYTES:
            raise Failure("OUTPUT_GROWTH")
        groups[rendition_id] = {
            "rendition_id": rendition_id,
            "codec": codec,
            "segments": [
                {
                    "segment_id": f"{rendition_id}-0",
                    "sha256": digest,
                    "byte_count": str(size),
                }
            ],
        }
    manifest: dict[str, object] = {
        "schema_version": 1,
        "profile": "SVN-JCS-1",
        "manifest_id": manifest_id,
        "content_id": content_id,
        "content_version": content_version,
        "package_bytes": str(total),
        "renditions": [groups[key] for key in sorted(groups)],
    }
    try:
        validate_manifest(manifest)
    except SchemaError:
        raise Failure("INTERNAL") from None
    canonical = json.dumps(manifest, sort_keys=True, separators=(",", ":"), ensure_ascii=True)
    encoded = canonical.encode("utf-8")
    if len(encoded) > 65536:
        raise Failure("INTERNAL")
    return manifest, canonical, hashlib.sha256(encoded).hexdigest(), total


def _with_wall(bounds: Bounds, wall: float) -> Bounds:
    return Bounds(
        wall_seconds=wall,
        cpu_seconds=bounds.cpu_seconds,
        memory_bytes=bounds.memory_bytes,
        file_bytes=bounds.file_bytes,
        network_namespace=bounds.network_namespace,
    )


def _write_file(path: Path, payload: bytes) -> None:
    flags = os.O_WRONLY | os.O_CREAT | os.O_EXCL
    try:
        fd = os.open(path, flags, 0o600)
    except OSError:
        raise Failure("INTERNAL") from None
    try:
        os.write(fd, payload)
        os.fsync(fd)
    except OSError:
        raise Failure("OUTPUT_GROWTH") from None
    finally:
        os.close(fd)


def _fsync_tree(path: Path) -> None:
    for file in sorted(path.rglob("*")):
        if not file.is_file():
            continue
        try:
            fd = os.open(file, os.O_RDONLY)
        except OSError:
            raise Failure("INTERNAL") from None
        try:
            os.fsync(fd)
        finally:
            os.close(fd)
    try:
        fd = os.open(path, os.O_RDONLY)
    except OSError:
        raise Failure("INTERNAL") from None
    try:
        os.fsync(fd)
    finally:
        os.close(fd)
