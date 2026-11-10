"""HLS master and rendition playlists. URIs are relative to the package prefix."""

from __future__ import annotations

import math

from svn_delivery.errors import DeliveryError
from svn_delivery.package import PackageView, VideoRendition


def master_playlist(package: PackageView) -> str:
    lines = ["#EXTM3U", "#EXT-X-VERSION:7", "#EXT-X-INDEPENDENT-SEGMENTS"]
    if package.captions is not None:
        uri = _rel(package.captions.rel)
        lines.append(
            '#EXT-X-MEDIA:TYPE=SUBTITLES,GROUP-ID="subs",NAME="captions",'
            f'DEFAULT=YES,AUTOSELECT=YES,LANGUAGE="und",URI="{uri}"'
        )
    # Highest bandwidth first. `package.videos` is lowest bandwidth first.
    for rendition in reversed(package.videos):
        lines.append(
            "#EXT-X-STREAM-INF:" + ",".join(_stream(rendition, package.captions is not None))
        )
        lines.append(_rel(f"{rendition.rendition_id}.m3u8"))
    return "\n".join(lines) + "\n"


def rendition_playlist(rendition: VideoRendition) -> str:
    if len(rendition.durations) != len(rendition.segments) or not rendition.durations:
        raise DeliveryError("INTEGRITY")
    target = max(1, math.ceil(max(rendition.durations)))
    lines = [
        "#EXTM3U",
        "#EXT-X-VERSION:7",
        f"#EXT-X-TARGETDURATION:{target}",
        "#EXT-X-PLAYLIST-TYPE:VOD",
        "#EXT-X-MEDIA-SEQUENCE:0",
    ]
    for segment, duration in zip(rendition.segments, rendition.durations, strict=True):
        lines.append(f"#EXTINF:{duration:.3f},")
        lines.append(_rel(segment.rel))
    lines.append("#EXT-X-ENDLIST")
    return "\n".join(lines) + "\n"


def playlist(package: PackageView, name: str) -> bytes:
    if name == "master.m3u8":
        return master_playlist(package).encode("utf-8")
    if not name.endswith(".m3u8") or "/" in name:
        raise DeliveryError("NOT_FOUND")
    rendition_id = name[: -len(".m3u8")]
    found = next((item for item in package.videos if item.rendition_id == rendition_id), None)
    if found is None:
        raise DeliveryError("NOT_FOUND")
    return rendition_playlist(found).encode("utf-8")


def _stream(rendition: VideoRendition, captions: bool) -> list[str]:
    attrs = [
        f"BANDWIDTH={rendition.bandwidth}",
        f"AVERAGE-BANDWIDTH={rendition.bandwidth}",
        f"RESOLUTION={rendition.width}x{rendition.height}",
    ]
    if rendition.codecs:
        attrs.append(f'CODECS="{rendition.codecs}"')
    if captions:
        attrs.append('SUBTITLES="subs"')
    return attrs


def _rel(value: str) -> str:
    parts = value.split("/")
    if (
        not value
        or value.startswith("/")
        or "\\" in value
        or "://" in value
        or any(part in {"", ".", ".."} for part in parts)
    ):
        raise DeliveryError("INTEGRITY")
    return value
