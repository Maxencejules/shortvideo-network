"""Playback grants, host advertisement, and cache invalidation.

A grant is a local test permission to serve a ready-for-review package.
It is not publication and it is not a hosting lease. The chain note is
operator input. This process does not query or write the chain.
"""

from __future__ import annotations

import hashlib
import json
import os
import threading
import time
from collections.abc import Callable
from dataclasses import dataclass
from pathlib import Path
from urllib.parse import urlsplit

from svn_delivery.errors import DeliveryError
from svn_delivery.hls import playlist
from svn_delivery.ids import (
    ADAPTER_NAME,
    ADAPTER_VERSION,
    CHAIN_NOTES,
    DISABLED,
    ID,
    MAX_TERM_SECONDS,
    SHA,
)
from svn_delivery.package import (
    PackageView,
    Segment,
    load_ready_record,
    read_checked,
    validate_directory,
)


@dataclass(frozen=True)
class Grant:
    content_id: str
    content_version: int
    manifest_sha256: str
    state: str
    authorized_until: float
    host_ids: tuple[str, ...]


@dataclass(frozen=True)
class BoundHost:
    host_id: str
    role: str
    origin: str


class CacheIndex:
    """ETags this process has issued. Removal drops them."""

    def __init__(self) -> None:
        self._etags: dict[str, set[str]] = {}

    def remember(self, content_id: str, etag: str) -> None:
        bucket = self._etags.setdefault(content_id, set())
        if len(bucket) >= 32:
            bucket.clear()
        bucket.add(etag)

    def drop(self, content_id: str) -> None:
        self._etags.pop(content_id, None)

    def known(self, content_id: str, etag: str) -> bool:
        return etag in self._etags.get(content_id, set())


class Catalog:
    """One worker root plus local playback grants."""

    def __init__(self, root: Path, *, now: Callable[[], float] | None = None) -> None:
        self.root = root.resolve()
        self._now = now or time.time
        self._lock = threading.Lock()
        self.cache = CacheIndex()
        self._grants: dict[str, Grant] = {}
        self._hosts: dict[str, BoundHost] = {}
        self._replicas: dict[tuple[str, str, int], Path] = {}
        self._reports: dict[str, dict[str, object]] = {}
        self._hits: list[str] = []
        self.record_hits = False
        self._chain = "UNAVAILABLE"
        self._load()

    def note_chain(self, note: str) -> None:
        """Record the operator's chain note. This does not create a grant or a host."""
        if note not in CHAIN_NOTES:
            raise DeliveryError("BAD_REQUEST")
        with self._lock:
            self._chain = note
            _write_json(self.root / "playback" / "chain.json", {"note": note})

    def chain_note(self) -> str:
        with self._lock:
            return self._chain

    def bind_host(self, host_id: str, origin: str, *, role: str) -> None:
        """Remember a loopback base URL. A new host during an outage is refused."""
        if not ID.fullmatch(host_id) or role not in {"origin", "test"}:
            raise DeliveryError("REFUSED")
        _loopback_origin(origin)
        with self._lock:
            if self._chain != "HELD" and not self._host_granted(host_id):
                raise DeliveryError("REFUSED")
            self._hosts[host_id] = BoundHost(host_id, role, origin.rstrip("/"))

    def unbind_host(self, host_id: str) -> None:
        with self._lock:
            self._hosts.pop(host_id, None)

    def accept_replica(self, host_id: str, directory: Path) -> None:
        """Advertise `directory` only after its hashes match the origin package."""
        if not ID.fullmatch(host_id) or host_id == "origin-test":
            raise DeliveryError("REFUSED")
        manifest = directory / "manifest.json"
        if manifest.is_symlink() or not manifest.is_file():
            raise DeliveryError("INTEGRITY")
        try:
            digest = hashlib.sha256(manifest.read_bytes()).hexdigest()
        except OSError:
            raise DeliveryError("INTEGRITY") from None
        if not SHA.fullmatch(digest):
            raise DeliveryError("INTEGRITY")
        view = validate_directory(directory, manifest_sha256=digest)
        with self._lock:
            if self._chain != "HELD" and not self._grant_has_host(view.content_id, host_id):
                raise DeliveryError("REFUSED")
            record = load_ready_record(self.root, view.content_id)
            if (
                record is None
                or record.get("manifest_sha256") != view.manifest_sha256
                or record.get("content_version") != view.content_version
            ):
                raise DeliveryError("INTEGRITY")
            origin = validate_directory(
                self._package_dir(view.content_id, view.content_version),
                manifest_sha256=view.manifest_sha256,
            )
            if origin.manifest_sha256 != view.manifest_sha256:
                raise DeliveryError("INTEGRITY")
            self._replicas[(host_id, view.content_id, view.content_version)] = directory.resolve()

    def authorize(self, content_id: str, *, seconds: float) -> Grant:
        """Grant playback of the current ready package. Refused while the chain note is down."""
        if not ID.fullmatch(content_id):
            raise DeliveryError("BAD_REQUEST")
        if seconds < 1 or seconds > MAX_TERM_SECONDS:
            raise DeliveryError("REFUSED")
        with self._lock:
            if self._chain != "HELD":
                raise DeliveryError("REFUSED")
            record = load_ready_record(self.root, content_id)
            if record is None:
                raise DeliveryError("NOT_FOUND")
            version = record["content_version"]
            digest = record["manifest_sha256"]
            if not isinstance(version, int) or not isinstance(digest, str):
                raise DeliveryError("INTEGRITY")
            view = self._validated_origin(content_id, version, digest)
            hosts_list = [
                host_id
                for host_id, _host in self._hosts.items()
                if self._host_verifies(host_id, view)
            ]
            # The origin package is on disk. A second process can name it in
            # the grant without having bound the listener. Advertising the
            # host still requires this process to bind it.
            if "origin-test" not in hosts_list and self._host_verifies("origin-test", view):
                hosts_list.insert(0, "origin-test")
            if "origin-test" not in hosts_list:
                raise DeliveryError("UNAVAILABLE")
            hosts = tuple(hosts_list)
            grant = Grant(
                content_id=content_id,
                content_version=version,
                manifest_sha256=digest,
                state="AUTHORIZED",
                authorized_until=self._now() + seconds,
                host_ids=hosts,
            )
            self._grants[content_id] = grant
            _write_json(self._grant_path(content_id), _grant_body(grant))
            return grant

    def remove(self, content_id: str) -> None:
        if not ID.fullmatch(content_id):
            raise DeliveryError("BAD_REQUEST")
        with self._lock:
            previous = self._grants.get(content_id)
            if previous is None:
                raise DeliveryError("NOT_FOUND")
            grant = Grant(
                content_id=previous.content_id,
                content_version=previous.content_version,
                manifest_sha256=previous.manifest_sha256,
                state="REMOVED",
                authorized_until=previous.authorized_until,
                host_ids=(),
            )
            self._grants[content_id] = grant
            self.cache.drop(content_id)
            self._reports.pop(content_id, None)
            _write_json(self._grant_path(content_id), _grant_body(grant))

    def document(self, content_id: str) -> dict[str, object]:
        if not ID.fullmatch(content_id):
            raise DeliveryError("BAD_REQUEST")
        with self._lock:
            self._reload_grants_locked()
            record = load_ready_record(self.root, content_id)
            if record is None and content_id not in self._grants:
                raise DeliveryError("NOT_FOUND")
            publication = "NOT_PUBLISHED"
            removal = "NONE"
            playback = "NONE"
            verified = False
            view: PackageView | None = None
            grant = self._grants.get(content_id)
            if grant is not None and grant.state == "REMOVED":
                removal = "REMOVED"
                self.cache.drop(content_id)
            elif grant is not None and grant.state == "AUTHORIZED":
                if self._now() >= grant.authorized_until:
                    playback = "EXPIRED"
                    self.cache.drop(content_id)
                else:
                    playback = "AUTHORIZED"
                    try:
                        view = self._validated_origin(
                            content_id, grant.content_version, grant.manifest_sha256
                        )
                        verified = True
                    except DeliveryError:
                        view = None
                        verified = False
                        self.cache.drop(content_id)
            chain = self._client_chain(playback == "AUTHORIZED" and verified)
            hosts: list[dict[str, object]] = []
            if view is not None and grant is not None and playback == "AUTHORIZED":
                for host_id in grant.host_ids:
                    bound = self._hosts.get(host_id)
                    if bound is None or not self._host_verifies(host_id, view):
                        continue
                    base = f"{bound.origin}/v1/media/{view.content_id}/v{view.content_version}"
                    hosts.append(
                        {
                            "host_id": host_id,
                            "role": bound.role,
                            "base_url": base,
                            "verified": True,
                        }
                    )
            body: dict[str, object] = {
                "schema_version": 1,
                "adapter": ADAPTER_NAME,
                "adapter_version": ADAPTER_VERSION,
                "content_id": content_id,
                "publication": publication,
                "removal": removal,
                "playback_grant": playback,
                "chain_status": chain,
                "stale": chain == "STALE",
                "hosting_promise": False,
                "package_verified": verified and bool(hosts),
                "hosts": hosts,
                "disabled": list(DISABLED),
            }
            if view is not None and hosts:
                body["content_version"] = view.content_version
                body["manifest_sha256"] = view.manifest_sha256
                body["package_bytes"] = str(view.package_bytes)
                body["master_path"] = "master.m3u8"
                body["fallback_rendition"] = view.fallback_rendition
                body["renditions"] = [_rendition_json(item) for item in view.videos]
                body["captions"] = _segment_json(view.captions)
                body["thumbnail"] = _segment_json(view.thumb)
            else:
                body["content_version"] = None
                body["manifest_sha256"] = None
                body["package_bytes"] = None
                body["master_path"] = None
                body["fallback_rendition"] = None
                body["renditions"] = []
                body["captions"] = None
                body["thumbnail"] = None
            return body

    def read(self, host_id: str, content_id: str, version: int, name: str) -> tuple[str, bytes]:
        """Return `(kind, body)` for a playlist name or a segment relative path."""
        with self._lock:
            self._reload_grants_locked()
            view = self._servable(host_id, content_id, version)
            if name.endswith(".m3u8"):
                body = playlist(view, name)
                return "playlist", body
            segment = view.by_rel().get(name)
            if segment is None:
                raise DeliveryError("NOT_FOUND")
            if self.record_hits and len(self._hits) < 100:
                self._hits.append(f"{host_id} {name}")
            return "segment", read_checked(segment)

    def hits(self) -> tuple[str, ...]:
        with self._lock:
            return tuple(self._hits)

    def clear_hits(self) -> None:
        with self._lock:
            self._hits.clear()

    def store_report(self, content_id: str, body: dict[str, object]) -> None:
        with self._lock:
            if not self._report_allowed(content_id):
                raise DeliveryError("NOT_FOUND")
            self._reports[content_id] = body

    def report(self, content_id: str) -> dict[str, object] | None:
        with self._lock:
            if not self._report_allowed(content_id):
                raise DeliveryError("NOT_FOUND")
            found = self._reports.get(content_id)
            return dict(found) if found is not None else None

    def grant_live(self, content_id: str) -> bool:
        with self._lock:
            self._reload_grants_locked()
            return self._report_allowed(content_id)

    def clear_report(self, content_id: str) -> None:
        with self._lock:
            self._reports.pop(content_id, None)

    def remember_etag(self, content_id: str, etag: str) -> None:
        with self._lock:
            self.cache.remember(content_id, etag)

    def etag_known(self, content_id: str, etag: str) -> bool:
        with self._lock:
            return self.cache.known(content_id, etag)

    def _report_allowed(self, content_id: str) -> bool:
        grant = self._grants.get(content_id)
        return (
            grant is not None
            and grant.state == "AUTHORIZED"
            and self._now() < grant.authorized_until
        )

    def _servable(self, host_id: str, content_id: str, version: int) -> PackageView:
        grant = self._grants.get(content_id)
        if (
            grant is None
            or grant.state != "AUTHORIZED"
            or grant.content_version != version
            or self._now() >= grant.authorized_until
            or host_id not in grant.host_ids
        ):
            self.cache.drop(content_id)
            raise DeliveryError("NOT_FOUND")
        if not self._host_verifies_ids(host_id, content_id, version, grant.manifest_sha256):
            self.cache.drop(content_id)
            raise DeliveryError("INTEGRITY")
        try:
            return self._view_for_host(host_id, content_id, version, grant.manifest_sha256)
        except DeliveryError:
            self.cache.drop(content_id)
            raise

    def _client_chain(self, live: bool) -> str:
        if live and self._chain in {"STALE", "UNAVAILABLE"}:
            return "STALE"
        if self._chain == "HELD":
            return "HELD"
        return "UNAVAILABLE"

    def _host_verifies(self, host_id: str, view: PackageView) -> bool:
        return self._host_verifies_ids(
            host_id, view.content_id, view.content_version, view.manifest_sha256
        )

    def _host_verifies_ids(self, host_id: str, content_id: str, version: int, digest: str) -> bool:
        try:
            self._view_for_host(host_id, content_id, version, digest)
        except DeliveryError:
            return False
        return True

    def _view_for_host(
        self, host_id: str, content_id: str, version: int, digest: str
    ) -> PackageView:
        if host_id == "origin-test":
            return self._validated_origin(content_id, version, digest)
        directory = self._replicas.get((host_id, content_id, version))
        if directory is None:
            raise DeliveryError("NOT_FOUND")
        view = validate_directory(directory, manifest_sha256=digest)
        if (
            view.content_id != content_id
            or view.content_version != version
            or view.manifest_sha256 != digest
        ):
            raise DeliveryError("INTEGRITY")
        return view

    def _validated_origin(self, content_id: str, version: int, digest: str) -> PackageView:
        directory = self._package_dir(content_id, version)
        view = validate_directory(directory, manifest_sha256=digest)
        if view.content_id != content_id or view.content_version != version:
            raise DeliveryError("INTEGRITY")
        return view

    def _package_dir(self, content_id: str, version: int) -> Path:
        if not ID.fullmatch(content_id) or version < 1:
            raise DeliveryError("INTEGRITY")
        directory = self.root / "packages" / content_id / f"v{version}"
        if directory.is_symlink() or not directory.is_dir():
            raise DeliveryError("INTEGRITY")
        try:
            resolved = directory.resolve()
        except OSError:
            raise DeliveryError("INTEGRITY") from None
        packages = (self.root / "packages").resolve()
        if not resolved.is_relative_to(packages):
            raise DeliveryError("INTEGRITY")
        return resolved

    def _host_granted(self, host_id: str) -> bool:
        now = self._now()
        return any(
            grant.state == "AUTHORIZED"
            and now < grant.authorized_until
            and host_id in grant.host_ids
            for grant in self._grants.values()
        )

    def _grant_has_host(self, content_id: str, host_id: str) -> bool:
        grant = self._grants.get(content_id)
        now = self._now()
        return (
            grant is not None
            and grant.state == "AUTHORIZED"
            and now < grant.authorized_until
            and host_id in grant.host_ids
        )

    def _grant_path(self, content_id: str) -> Path:
        return self.root / "playback" / f"{content_id}.json"

    def _reload_grants_locked(self) -> None:
        """Pick up a grant written by another process. The caller holds the lock."""
        folder = self.root / "playback"
        if not folder.is_dir():
            return
        for path in sorted(folder.glob("*.json")):
            if path.name in {"chain.json", "hosts.json"} or path.is_symlink():
                continue
            grant = _read_grant(path)
            if grant is not None:
                self._grants[grant.content_id] = grant

    def _load(self) -> None:
        chain_path = self.root / "playback" / "chain.json"
        if chain_path.is_file() and not chain_path.is_symlink():
            try:
                value = json.loads(chain_path.read_text(encoding="utf-8"))
            except (OSError, UnicodeError, json.JSONDecodeError):
                value = None
            if isinstance(value, dict) and value.get("note") in CHAIN_NOTES:
                self._chain = str(value["note"])
        folder = self.root / "playback"
        if not folder.is_dir():
            return
        for path in sorted(folder.glob("*.json")):
            if path.name == "chain.json" or path.is_symlink():
                continue
            grant = _read_grant(path)
            if grant is not None:
                self._grants[grant.content_id] = grant


def _loopback_origin(origin: str) -> None:
    parts = urlsplit(origin)
    if parts.scheme != "https" or parts.username or parts.password or parts.query:
        raise DeliveryError("REFUSED")
    if parts.hostname not in {"127.0.0.1", "localhost"}:
        raise DeliveryError("REFUSED")
    if parts.port is None:
        raise DeliveryError("REFUSED")


def _rendition_json(rendition: object) -> dict[str, object]:
    from svn_delivery.package import VideoRendition

    if not isinstance(rendition, VideoRendition):
        raise DeliveryError("INTEGRITY")
    return {
        "rendition_id": rendition.rendition_id,
        "bandwidth": rendition.bandwidth,
        "width": rendition.width,
        "height": rendition.height,
        "codecs": rendition.codecs,
        "segments": [
            {
                "segment_id": segment.segment_id,
                "sha256": segment.sha256,
                "byte_count": str(segment.byte_count),
                "path": segment.rel,
            }
            for segment in rendition.segments
        ],
    }


def _segment_json(segment: Segment | None) -> dict[str, object] | None:
    if segment is None:
        return None
    return {
        "path": segment.rel,
        "sha256": segment.sha256,
        "byte_count": str(segment.byte_count),
    }


def _grant_body(grant: Grant) -> dict[str, object]:
    return {
        "content_id": grant.content_id,
        "content_version": grant.content_version,
        "manifest_sha256": grant.manifest_sha256,
        "state": grant.state,
        "authorized_until": grant.authorized_until,
        "host_ids": list(grant.host_ids),
        "hosting_promise": False,
    }


def _read_grant(path: Path) -> Grant | None:
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, UnicodeError, json.JSONDecodeError):
        return None
    if not isinstance(value, dict) or value.get("hosting_promise") is not False:
        return None
    content_id = value.get("content_id")
    version = value.get("content_version")
    digest = value.get("manifest_sha256")
    state = value.get("state")
    until = value.get("authorized_until")
    hosts = value.get("host_ids")
    if (
        not isinstance(content_id, str)
        or not ID.fullmatch(content_id)
        or path.stem != content_id
        or not isinstance(version, int)
        or isinstance(version, bool)
        or version < 1
        or not isinstance(digest, str)
        or not SHA.fullmatch(digest)
        or state not in {"AUTHORIZED", "REMOVED"}
        or not isinstance(until, (int, float))
        or isinstance(until, bool)
        or not isinstance(hosts, list)
        or not all(isinstance(item, str) and ID.fullmatch(item) for item in hosts)
    ):
        return None
    return Grant(content_id, version, digest, state, float(until), tuple(hosts))


def _write_json(path: Path, body: dict[str, object]) -> None:
    path.parent.mkdir(parents=True, mode=0o700, exist_ok=True)
    payload = json.dumps(body, sort_keys=True, separators=(",", ":")).encode("utf-8")
    temporary = path.parent / f".{path.name}.tmp"
    try:
        fd = os.open(temporary, os.O_WRONLY | os.O_CREAT | os.O_TRUNC, 0o600)
    except OSError:
        raise DeliveryError("UNAVAILABLE") from None
    try:
        os.write(fd, payload)
        os.fsync(fd)
    except OSError:
        raise DeliveryError("UNAVAILABLE") from None
    finally:
        os.close(fd)
    os.replace(temporary, path)
