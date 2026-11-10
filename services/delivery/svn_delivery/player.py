"""Playback harness. An HTTP response is not a completed watch and is not a render.

The harness hashes the segment bytes it actually fetches. It does not claim
that a browser's native HLS stack verified every segment.
"""

from __future__ import annotations

import hashlib
import http.client
import threading
import time
from collections.abc import Sequence
from dataclasses import dataclass
from urllib.parse import SplitResult, urlsplit

from svn_delivery.errors import DeliveryError
from svn_delivery.ids import (
    ATTEMPTS_PER_HOST,
    DISABLED,
    MAX_HOSTS,
    PACKAGE_CAP,
    PREFETCH_ITEMS,
    PREFETCH_SEGMENTS,
    SHA,
)
from svn_delivery.tlsutil import client_context

_PLAYLIST_LIMIT = 65_536


@dataclass
class Aggregates:
    """Privacy-preserving counters. There is no content id and no watch history."""

    startup_ms_sum: int = 0
    rebuffer_ms_sum: int = 0
    playback_errors: int = 0
    delivered_bytes: int = 0
    playlist_bytes: int = 0
    other_bytes: int = 0
    http_responses: int = 0
    rendered: int = 0
    plays: int = 0

    def export(self) -> dict[str, int]:
        return {
            "startup_ms_sum": self.startup_ms_sum,
            "rebuffer_ms_sum": self.rebuffer_ms_sum,
            "playback_errors": self.playback_errors,
            "delivered_bytes": self.delivered_bytes,
            "playlist_bytes": self.playlist_bytes,
            "other_bytes": self.other_bytes,
            "http_responses": self.http_responses,
            "rendered": self.rendered,
            "plays": self.plays,
        }

    def note_rendered(self) -> None:
        """Call only after a real decode. `play` does not call this."""
        self.rendered += 1


@dataclass(frozen=True)
class PlayResult:
    ok: bool
    error: str
    attempts: int
    used_fallback: bool
    rendition_id: str
    fetched: tuple[str, ...]
    skipped: tuple[str, ...]
    startup_ms: int
    rebuffer_ms: int
    rendered: bool
    hosts_tried: tuple[str, ...]


@dataclass(frozen=True)
class _Variant:
    path: str
    bandwidth: int


class _Stop(Exception):
    def __init__(self, code: str, attempts: int, tried: tuple[str, ...]) -> None:
        self.code = code
        self.attempts = attempts
        self.tried = tried
        super().__init__(code)


class Harness:
    """Bounded retries, one fallback rendition, and a small prefetch window."""

    def __init__(
        self,
        cert_pem: bytes,
        *,
        timeout: float = 2.0,
        attempts_per_host: int = ATTEMPTS_PER_HOST,
        prefetch_segments: int = PREFETCH_SEGMENTS,
        prefetch_items: int = PREFETCH_ITEMS,
        max_hosts: int = MAX_HOSTS,
    ) -> None:
        if (
            attempts_per_host < 1
            or attempts_per_host > 3
            or prefetch_segments < 0
            or prefetch_segments > 2
            or prefetch_items < 0
            or prefetch_items > 2
            or max_hosts < 1
            or max_hosts > 4
            or timeout <= 0
            or timeout > 30
        ):
            raise DeliveryError("REFUSED")
        self.metrics = Aggregates()
        self._pem = cert_pem
        self.timeout = timeout
        self.attempts_per_host = attempts_per_host
        self.prefetch_segments = prefetch_segments
        self.prefetch_items = prefetch_items
        self.max_hosts = max_hosts
        self._generation = 0
        self._inflight: list[http.client.HTTPConnection] = []
        self._threads: list[threading.Thread] = []
        self._lock = threading.Lock()
        self.skipped_items = 0

    def play(self, document: dict[str, object], *, rendition: str | None = None) -> PlayResult:
        self.metrics.plays += 1
        started = time.monotonic()
        generation = self._generation
        try:
            hosts = self._hosts(document)
            if not hosts:
                raise _Stop("NO_HOST", 0, ())
            master, host_id, attempts, tried = self._across(
                hosts, "master.m3u8", limit=_PLAYLIST_LIMIT, generation=generation
            )
            self.metrics.playlist_bytes += len(master)
            variants, captions_ref = _parse_master(master.decode("utf-8"))
            _check_captions(document, captions_ref)
            order = _rendition_order(document, variants, rendition)
            fetched = ["master.m3u8"]
            total_attempts = attempts
            all_tried = list(tried)
            last = "INTEGRITY"
            primary = rendition if rendition is not None else order[0]
            for rendition_id in order:
                try:
                    media, host_id, extra, extra_tried = self._across(
                        _prefer(hosts, host_id),
                        f"{rendition_id}.m3u8",
                        limit=_PLAYLIST_LIMIT,
                        generation=generation,
                    )
                except _Stop as exc:
                    total_attempts += exc.attempts
                    all_tried.extend(exc.tried)
                    last = exc.code
                    continue
                total_attempts += extra
                all_tried.extend(extra_tried)
                self.metrics.playlist_bytes += len(media)
                fetched.append(f"{rendition_id}.m3u8")
                segments = _parse_media(media.decode("utf-8"))
                expected = _expected(document, rendition_id)
                if [item[0] for item in expected] != segments:
                    last = "INTEGRITY"
                    continue
                window = expected[: self.prefetch_segments + 1]
                skipped = tuple(item[0] for item in expected[len(window) :])
                startup_ms = 0
                rebuffer_ms = 0
                failed = False
                for index, (rel, digest, count) in enumerate(window):
                    moment = time.monotonic()
                    try:
                        data, host_id, extra, extra_tried = self._across(
                            _prefer(hosts, host_id),
                            rel,
                            limit=PACKAGE_CAP,
                            generation=generation,
                        )
                    except _Stop as exc:
                        total_attempts += exc.attempts
                        all_tried.extend(exc.tried)
                        last = exc.code
                        failed = True
                        break
                    total_attempts += extra
                    all_tried.extend(extra_tried)
                    if hashlib.sha256(data).hexdigest() != digest or len(data) != count:
                        last = "INTEGRITY"
                        failed = True
                        break
                    self.metrics.delivered_bytes += len(data)
                    fetched.append(rel)
                    if index == 0:
                        startup_ms = int((time.monotonic() - started) * 1000)
                    else:
                        rebuffer_ms += int((time.monotonic() - moment) * 1000)
                if failed:
                    continue
                try:
                    self._captions(document, captions_ref, _prefer(hosts, host_id), generation)
                except _Stop as exc:
                    last = exc.code
                    continue
                self.metrics.startup_ms_sum += startup_ms
                self.metrics.rebuffer_ms_sum += rebuffer_ms
                return _result(
                    True,
                    "",
                    total_attempts,
                    rendition_id != primary,
                    rendition_id,
                    fetched,
                    skipped,
                    startup_ms,
                    rebuffer_ms,
                    all_tried,
                )
            self.metrics.playback_errors += 1
            return _result(False, last, total_attempts, False, "", fetched, (), 0, 0, all_tried)
        except _Stop as exc:
            self.metrics.playback_errors += 1
            return _result(False, exc.code, exc.attempts, False, "", (), (), 0, 0, exc.tried)
        except DeliveryError as exc:
            self.metrics.playback_errors += 1
            return _result(False, exc.code, 0, False, "", (), (), 0, 0, ())

    def prefetch_queue(self, documents: list[dict[str, object]]) -> None:
        """Fetch at most `prefetch_items` master playlists. Do not fetch their media."""
        self.abandon()
        generation = self._generation
        chosen = documents[: self.prefetch_items]
        self.skipped_items = len(documents) - len(chosen)
        threads: list[threading.Thread] = []
        for document in chosen:
            thread = threading.Thread(
                target=self._prefetch_one,
                args=(document, generation),
                daemon=True,
            )
            thread.start()
            threads.append(thread)
        self._threads = threads

    def abandon(self) -> None:
        with self._lock:
            self._generation += 1
            connections = list(self._inflight)
        for connection in connections:
            try:
                connection.close()
            except OSError:
                continue

    def wait_prefetch(self, timeout: float = 5) -> None:
        for thread in self._threads:
            thread.join(timeout)

    def _prefetch_one(self, document: dict[str, object], generation: int) -> None:
        try:
            hosts = self._hosts(document)
            if not hosts or generation != self._generation:
                return
            body, _host, _attempts, _tried = self._across(
                hosts, "master.m3u8", limit=_PLAYLIST_LIMIT, generation=generation
            )
        except (_Stop, DeliveryError):
            return
        if generation != self._generation:
            return
        self.metrics.playlist_bytes += len(body)

    def _hosts(self, document: dict[str, object]) -> list[tuple[str, str]]:
        if document.get("schema_version") != 1 or document.get("hosting_promise") is not False:
            raise DeliveryError("REFUSED")
        disabled = document.get("disabled")
        if not isinstance(disabled, list) or any(name not in disabled for name in DISABLED):
            raise DeliveryError("REFUSED")
        raw = document.get("hosts")
        if not isinstance(raw, list):
            raise DeliveryError("REFUSED")
        if document.get("master_path") not in {None, "master.m3u8"}:
            raise DeliveryError("INTEGRITY")
        chosen: list[tuple[str, str]] = []
        for item in raw:
            if len(chosen) >= self.max_hosts:
                break
            if not isinstance(item, dict) or item.get("verified") is not True:
                continue
            host_id = item.get("host_id")
            base = item.get("base_url")
            if not isinstance(host_id, str) or not isinstance(base, str):
                continue
            _check_url(base + "/master.m3u8")
            chosen.append((host_id, base.rstrip("/")))
        return chosen

    def _across(
        self,
        hosts: list[tuple[str, str]],
        rel: str,
        *,
        limit: int,
        generation: int,
    ) -> tuple[bytes, str, int, tuple[str, ...]]:
        _relative(rel)
        attempts = 0
        tried: list[str] = []
        last = "UNAVAILABLE"
        cap = self.max_hosts * self.attempts_per_host
        for host_id, base in hosts:
            host_attempts = 0
            while host_attempts < self.attempts_per_host and attempts < cap:
                if generation != self._generation:
                    raise _Stop("CANCELLED", attempts, tuple(tried))
                host_attempts += 1
                attempts += 1
                tried.append(host_id)
                try:
                    status, body = self._once(base + "/" + rel, limit=limit)
                except DeliveryError as exc:
                    if exc.code == "CANCELLED" or generation != self._generation:
                        raise _Stop("CANCELLED", attempts, tuple(tried)) from exc
                    last = exc.code
                    continue
                self.metrics.http_responses += 1
                if status == 200:
                    return body, host_id, attempts, tuple(tried)
                if status == 409:
                    last = "INTEGRITY"
                    break
                last = "HTTP"
        raise _Stop(last, attempts, tuple(tried))

    def _once(self, url: str, *, limit: int) -> tuple[int, bytes]:
        parts = _check_url(url)
        if parts.hostname is None or parts.port is None:
            raise DeliveryError("REFUSED")
        seen = self._generation
        connection = http.client.HTTPSConnection(
            parts.hostname,
            parts.port,
            timeout=self.timeout,
            context=client_context(self._pem),
        )
        with self._lock:
            self._inflight.append(connection)
        try:
            connection.request("GET", parts.path, headers={"Accept": "*/*"})
            response = connection.getresponse()
            if response.length is not None and response.length > limit:
                raise DeliveryError("REFUSED")
            chunks = bytearray()
            while len(chunks) <= limit:
                block = response.read(min(65536, limit + 1 - len(chunks)))
                if not block:
                    break
                chunks.extend(block)
            if len(chunks) > limit:
                raise DeliveryError("REFUSED")
            return response.status, bytes(chunks)
        except DeliveryError:
            raise
        except (TimeoutError, OSError, http.client.HTTPException):
            if seen != self._generation:
                raise DeliveryError("CANCELLED") from None
            raise DeliveryError("UNAVAILABLE") from None
        finally:
            with self._lock:
                if connection in self._inflight:
                    self._inflight.remove(connection)
            try:
                connection.close()
            except OSError:
                pass

    def _captions(
        self,
        document: dict[str, object],
        captions_ref: str | None,
        hosts: list[tuple[str, str]],
        generation: int,
    ) -> None:
        captions = document.get("captions")
        if captions is None:
            return
        if not isinstance(captions, dict) or captions_ref != captions.get("path"):
            raise _Stop("INTEGRITY", 0, ())
        digest = captions.get("sha256")
        count = captions.get("byte_count")
        path = captions.get("path")
        if (
            not isinstance(path, str)
            or not isinstance(digest, str)
            or not SHA.fullmatch(digest)
            or not isinstance(count, str)
            or not count.isdigit()
        ):
            raise _Stop("INTEGRITY", 0, ())
        data, _host, _attempts, _tried = self._across(
            hosts, path, limit=int(count), generation=generation
        )
        if hashlib.sha256(data).hexdigest() != digest or str(len(data)) != count:
            raise _Stop("INTEGRITY", 0, ())
        self.metrics.other_bytes += len(data)


def _result(
    ok: bool,
    error: str,
    attempts: int,
    used_fallback: bool,
    rendition_id: str,
    fetched: Sequence[str],
    skipped: tuple[str, ...],
    startup_ms: int,
    rebuffer_ms: int,
    tried: list[str] | tuple[str, ...],
) -> PlayResult:
    return PlayResult(
        ok=ok,
        error=error,
        attempts=attempts,
        used_fallback=used_fallback,
        rendition_id=rendition_id,
        fetched=tuple(fetched),
        skipped=skipped,
        startup_ms=startup_ms,
        rebuffer_ms=rebuffer_ms,
        rendered=False,
        hosts_tried=tuple(tried),
    )


def _prefer(hosts: list[tuple[str, str]], host_id: str) -> list[tuple[str, str]]:
    return sorted(hosts, key=lambda item: item[0] != host_id)


def _check_url(url: str) -> SplitResult:
    parts = urlsplit(url)
    if parts.scheme != "https" or parts.username or parts.password or parts.query or parts.fragment:
        raise DeliveryError("REFUSED")
    if parts.hostname not in {"127.0.0.1", "localhost"} or parts.port is None:
        raise DeliveryError("REFUSED")
    return parts


def _relative(value: str) -> None:
    parts = value.split("/")
    if (
        not value
        or value.startswith("/")
        or "\\" in value
        or "://" in value
        or any(part in {"", ".", ".."} for part in parts)
    ):
        raise DeliveryError("INTEGRITY")


def _parse_master(text: str) -> tuple[list[_Variant], str | None]:
    if not text.startswith("#EXTM3U"):
        raise DeliveryError("INTEGRITY")
    variants: list[_Variant] = []
    captions: str | None = None
    pending: str | None = None
    for raw in text.splitlines():
        line = raw.strip()
        if not line:
            continue
        if line.startswith("#EXT-X-MEDIA:") and "TYPE=SUBTITLES" in line:
            captions = _attr(line, "URI")
            _relative(captions)
        elif line.startswith("#EXT-X-STREAM-INF:"):
            pending = line
        elif line.startswith("#"):
            continue
        else:
            _relative(line)
            if pending is None:
                raise DeliveryError("INTEGRITY")
            variants.append(_Variant(line, int(_attr(pending, "BANDWIDTH"))))
            pending = None
        if len(variants) > 8:
            raise DeliveryError("INTEGRITY")
    if pending is not None or not variants:
        raise DeliveryError("INTEGRITY")
    return variants, captions


def _parse_media(text: str) -> list[str]:
    if not text.startswith("#EXTM3U"):
        raise DeliveryError("INTEGRITY")
    segments: list[str] = []
    expect = False
    for raw in text.splitlines():
        line = raw.strip()
        if line.startswith("#EXTINF:"):
            expect = True
        elif line.startswith("#"):
            continue
        elif expect:
            _relative(line)
            segments.append(line)
            expect = False
        else:
            raise DeliveryError("INTEGRITY")
        if len(segments) > 8:
            raise DeliveryError("INTEGRITY")
    if expect or not segments:
        raise DeliveryError("INTEGRITY")
    return segments


def _check_captions(document: dict[str, object], captions_ref: str | None) -> None:
    captions = document.get("captions")
    if captions is None:
        if captions_ref is not None:
            raise DeliveryError("INTEGRITY")
        return
    if not isinstance(captions, dict) or captions.get("path") != captions_ref:
        raise DeliveryError("INTEGRITY")


def _rendition_order(
    document: dict[str, object], variants: list[_Variant], requested: str | None
) -> list[str]:
    names = [item.path[: -len(".m3u8")] for item in variants if item.path.endswith(".m3u8")]
    if len(names) != len(variants):
        raise DeliveryError("INTEGRITY")
    fallback = document.get("fallback_rendition")
    if not isinstance(fallback, str) or fallback not in names:
        raise DeliveryError("INTEGRITY")
    if requested is not None and requested not in names:
        return [fallback]
    first = requested if requested is not None else names[0]
    order = [first]
    if fallback != first:
        order.append(fallback)
    return order


def _expected(document: dict[str, object], rendition_id: str) -> list[tuple[str, str, int]]:
    renditions = document.get("renditions")
    if not isinstance(renditions, list):
        raise DeliveryError("INTEGRITY")
    for rendition in renditions:
        if not isinstance(rendition, dict) or rendition.get("rendition_id") != rendition_id:
            continue
        rows = rendition.get("segments")
        if not isinstance(rows, list) or not rows:
            raise DeliveryError("INTEGRITY")
        found: list[tuple[str, str, int]] = []
        for row in rows:
            if not isinstance(row, dict):
                raise DeliveryError("INTEGRITY")
            path = row.get("path")
            digest = row.get("sha256")
            count = row.get("byte_count")
            if (
                not isinstance(path, str)
                or not isinstance(digest, str)
                or not SHA.fullmatch(digest)
                or not isinstance(count, str)
                or not count.isdigit()
            ):
                raise DeliveryError("INTEGRITY")
            _relative(path)
            found.append((path, digest, int(count)))
        return found
    raise DeliveryError("INTEGRITY")


def _attr(line: str, name: str) -> str:
    token = name + "="
    start = line.find(token)
    if start < 0:
        raise DeliveryError("INTEGRITY")
    rest = line[start + len(token) :]
    if rest.startswith('"'):
        end = rest.find('"', 1)
        if end < 0:
            raise DeliveryError("INTEGRITY")
        return rest[1:end]
    end = rest.find(",")
    value = rest if end < 0 else rest[:end]
    if not value.isdigit() and name == "BANDWIDTH":
        raise DeliveryError("INTEGRITY")
    return value
