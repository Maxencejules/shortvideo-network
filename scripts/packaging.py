#!/usr/bin/env python3
"""Local release-candidate packaging.

The candidate is NOT-RELEASED. No licence file is applied. This module does
not tag, publish, push an image, or open a port.
"""

from __future__ import annotations

import hashlib
import json
import os
import stat
import subprocess
import sys
import tempfile
from pathlib import Path
from typing import Any

from cryptography.exceptions import InvalidSignature
from cryptography.hazmat.primitives import serialization
from cryptography.hazmat.primitives.asymmetric.ed25519 import (
    Ed25519PrivateKey,
    Ed25519PublicKey,
)

ROOT = Path(__file__).resolve().parents[1]
RELEASE_LABEL = "NOT-RELEASED"
LICENCE = "UNCONFIRMED"
MARKER = "svn-install-1"
TEST_BANNER = "SVN-TEST-SIGNING-KEY-NOT-FOR-A-RELEASE"
FFMPEG_VERSION = "6.1.1-3ubuntu5"
FFMPEG_FLAG = "--enable-gpl"
PEM_MARK = b"-----BEGIN "
PRIVATE_MARK = b"PRIVATE KEY"
ELF = b"\x7fELF"
# svn-discovery binds a port when it is started with no arguments.
SKIP_EXEC = frozenset({"svn-discovery"})
SECRET_SOURCES = frozenset({"generated-on-install", "operator-supplied", "none"})
SIGNER_VALUES = frozenset({"absent", "host-local-not-in-bundle", "separate-directory"})
REQUIRED_PROFILES = (
    "dev-machine",
    "web-api",
    "accounts",
    "validators",
    "workers",
    "provider-agents",
    "storage",
    "query-index",
)
BINARIES = (
    ("svnd", "chain", "./cmd/svnd"),
    ("svn-api", "services/api", "./cmd/svn-api"),
    ("svn-discovery", "services/api", "./cmd/svn-discovery"),
    ("svn-feed-client", "services/api", "./cmd/svn-feed-client"),
    ("svn-broadcaster", "services/broadcaster", "./cmd/svn-broadcaster"),
    ("svn-indexer", "services/indexer", "./cmd/svn-indexer"),
    ("svn-provider", "agents/provider", "./cmd/svn-provider"),
    ("svn-operator", "services/operator", "./cmd/svn-operator"),
    ("svn-observe", "services/observe", "./cmd/svn-observe"),
    ("svn-restore", "services/restore", "./cmd/svn-restore"),
    ("svn-scheduler", "services/scheduler", "./cmd/svn-scheduler"),
    ("svn-chainview", "services/milestone", "./cmd/svn-chainview"),
)


class PackagingError(Exception):
    """A packaging step refused to continue."""


def _load(path: Path) -> Any:
    return json.loads(path.read_text(encoding="utf-8"))


def profile_problems(data: object) -> list[str]:
    """Return why a profile document is not a safe reference."""
    problems: list[str] = []
    if not isinstance(data, dict):
        return ["profiles document is not an object"]
    if data.get("release_label") != RELEASE_LABEL:
        problems.append("release_label is not NOT-RELEASED")
    if data.get("licence") != LICENCE:
        problems.append("licence is not UNCONFIRMED")
    if data.get("public_launch") != "disabled":
        problems.append("public launch is not disabled")
    if data.get("single_mandatory_operator") is not False:
        problems.append("a single mandatory operator is set")
    if data.get("commercial_cloud") != "not-required":
        problems.append("commercial cloud is required")
    if data.get("token") != "none":
        problems.append("a token is set")
    if data.get("video_bytes_on_chain") is not False:
        problems.append("video bytes are on chain")
    if data.get("chain_state_rollback") != "not-rehearsed":
        problems.append("chain state rollback is marked rehearsed")
    replacements = data.get("replacement_paths")
    if not isinstance(replacements, dict):
        problems.append("replacement paths are missing")
    else:
        for name in ("dns", "identity", "media_storage", "search", "transaction_access"):
            text = replacements.get(name)
            if not isinstance(text, str) or not text.strip():
                problems.append(f"replacement path {name} is empty")
    limits = data.get("known_limits")
    if not isinstance(limits, list):
        problems.append("known limits are missing")
    else:
        for needle in (
            "not-an-audit",
            "not-independent-operators",
            "not-interoperability",
            "public-launch-disabled",
        ):
            if needle not in limits:
                problems.append(f"known limit {needle} is missing")
    profiles = data.get("profiles")
    if not isinstance(profiles, list):
        return problems + ["profiles list is missing"]
    by_id: dict[str, dict[str, Any]] = {}
    for item in profiles:
        if not isinstance(item, dict) or not isinstance(item.get("id"), str):
            problems.append("a profile has no id")
            continue
        by_id[item["id"]] = item
        problems.extend(_profile_item(item))
    for name in REQUIRED_PROFILES:
        if name not in by_id:
            problems.append(f"profile {name} is missing")
    workers = by_id.get("workers")
    validators = by_id.get("validators")
    dev = by_id.get("dev-machine")
    if workers is not None:
        if workers.get("signer_material") != "absent":
            problems.append("workers hold signer material")
        if workers.get("untrusted_media") is not True:
            problems.append("workers are not the media profile")
        if "svnd" in workers.get("processes", []):
            problems.append("workers run svnd")
    if validators is not None:
        processes = validators.get("processes", [])
        if "ffmpeg" in processes or "media-worker" in processes:
            problems.append("validators run media processing")
        if validators.get("untrusted_media") is not False:
            problems.append("validators accept untrusted media")
        if validators.get("signer_material") != "host-local-not-in-bundle":
            problems.append("validator keys are not host-local")
    if dev is not None:
        paths = [item.get("path") for item in dev.get("volumes", []) if isinstance(item, dict)]
        if "var/validator" not in paths or "var/media" not in paths:
            problems.append("dev-machine shares one path for media and validators")
        if paths.count("var/validator") != 1 or paths.count("var/media") != 1:
            problems.append("dev-machine volume paths are not distinct")
    return problems


def _profile_item(item: dict[str, Any]) -> list[str]:
    problems: list[str] = []
    name = item.get("id")
    if item.get("commercial_cloud") != "not-required":
        problems.append(f"{name} requires a commercial cloud")
    if item.get("resources") != "not-measured":
        problems.append(f"{name} claims a resource measurement")
    if item.get("upgrades") != "binary-beside-running":
        problems.append(f"{name} upgrade is not a side-by-side binary")
    if item.get("worker_sees_signer") is not False:
        problems.append(f"{name} shows signer material to a worker")
    if item.get("signer_material") not in SIGNER_VALUES:
        problems.append(f"{name} signer material is unset")
    volumes = item.get("volumes")
    ports = item.get("ports")
    secrets = item.get("secrets")
    if not isinstance(volumes, list) or not volumes:
        problems.append(f"{name} has no volumes")
    else:
        for volume in volumes:
            if not isinstance(volume, dict):
                problems.append(f"{name} volume is not an object")
                continue
            path = volume.get("path")
            if not isinstance(path, str) or not path or path.startswith("/"):
                problems.append(f"{name} volume path is absolute or empty")
    if not isinstance(ports, list) or not ports:
        problems.append(f"{name} has no ports")
    else:
        for port in ports:
            if not isinstance(port, dict):
                problems.append(f"{name} port is not an object")
                continue
            if port.get("published") is not False:
                problems.append(f"{name} publishes a port")
            if port.get("bind") != "127.0.0.1":
                problems.append(f"{name} bind is not loopback")
    if not isinstance(secrets, list) or not secrets:
        problems.append(f"{name} has no secret record")
    else:
        for secret in secrets:
            if not isinstance(secret, dict):
                problems.append(f"{name} secret is not an object")
                continue
            if secret.get("source") not in SECRET_SOURCES:
                problems.append(f"{name} secret source is not allowed")
            if "value" in secret:
                problems.append(f"{name} secret has a bundled value")
    return problems


def _repo_on_path() -> None:
    if str(ROOT) not in sys.path:
        sys.path.insert(0, str(ROOT))


def _findings(name: str, text: str) -> list[str]:
    _repo_on_path()
    from scripts.scan_secrets import findings_in_text

    return findings_in_text(name, text)


def _workflow_problems(path: Path) -> list[str]:
    _repo_on_path()
    from scripts.workflow_policy import check_workflow

    return check_workflow(path)


def load_profiles(root: Path | None = None) -> dict[str, Any]:
    path = (root or ROOT) / "deployments" / "profiles" / "profiles.json"
    data = _load(path)
    problems = profile_problems(data)
    if problems:
        raise PackagingError("; ".join(problems))
    if not isinstance(data, dict):
        raise PackagingError("profiles")
    return data


def _restrict(root: Path) -> None:
    for path in [root, *root.rglob("*")]:
        mode = stat.S_IMODE(path.lstat().st_mode)
        path.chmod(mode & ~stat.S_IWOTH)


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    digest.update(path.read_bytes())
    return digest.hexdigest()


def _contained(root: Path, rel: str) -> Path:
    parts = Path(rel).parts
    if not rel or rel.startswith("/") or ".." in parts:
        raise PackagingError(f"path escapes: {rel}")
    path = root / rel
    parent = path.parent.resolve()
    base = root.resolve()
    if parent != base and base not in parent.parents:
        raise PackagingError(f"path escapes: {rel}")
    return parent / path.name


def _bundle_files(bundle: Path) -> list[Path]:
    files = [path for path in bundle.rglob("*") if path.is_file() and path.name != "SHA256SUMS"]
    return sorted(files, key=lambda item: item.relative_to(bundle).as_posix())


def checksums_text(bundle: Path) -> str:
    lines: list[str] = []
    for path in _bundle_files(bundle):
        rel = path.relative_to(bundle).as_posix()
        lines.append(f"{sha256_file(path)}  {rel}")
    return "\n".join(lines) + "\n"


def write_checksums(bundle: Path) -> None:
    (bundle / "SHA256SUMS").write_text(checksums_text(bundle), encoding="utf-8")


def verify_checksums(bundle: Path) -> None:
    path = bundle / "SHA256SUMS"
    if not path.is_file():
        raise PackagingError("SHA256SUMS is missing")
    current = path.read_text(encoding="utf-8")
    expect = checksums_text(bundle)
    if current != expect:
        raise PackagingError("checksum mismatch")


def scan_tree(bundle: Path) -> list[str]:
    """Secrets, setuid, and world-writable files under a bundle or image context."""
    problems: list[str] = []
    if not bundle.exists():
        return ["bundle is missing"]
    for path in sorted(bundle.rglob("*")):
        rel = path.relative_to(bundle).as_posix()
        mode = path.stat().st_mode
        if mode & stat.S_ISUID or mode & stat.S_ISGID:
            problems.append(f"setuid or setgid: {rel}")
        if mode & stat.S_IWOTH:
            problems.append(f"world-writable: {rel}")
        if not path.is_file():
            continue
        data = path.read_bytes()
        if path.suffix == ".pem" or path.name in {"id_ed25519", "id_rsa"}:
            problems.append(f"key file name: {rel}")
        # ELF binaries contain the PEM header as a parser constant.
        if data.startswith(ELF):
            continue
        if PEM_MARK in data and PRIVATE_MARK in data:
            problems.append(f"private key marker: {rel}")
        try:
            text = data.decode("utf-8")
        except UnicodeDecodeError:
            continue
        problems.extend(_findings(rel, text))
    return problems


def scan_dockerfiles(root: Path) -> list[str]:
    """Pinned bases, a non-root user, and no unpinned download."""
    problems: list[str] = []
    paths = sorted((root / "deployments").rglob("Dockerfile")) + sorted(
        (root / "deployments").rglob("*.Dockerfile")
    )
    if not paths:
        return ["no Dockerfile"]
    for path in paths:
        text = path.read_text(encoding="utf-8")
        rel = path.relative_to(root).as_posix()
        users = [line.split()[1] for line in text.splitlines() if line.startswith("USER ")]
        froms = [line for line in text.splitlines() if line.startswith("FROM ")]
        if not froms:
            problems.append(f"{rel} has no FROM")
        for line in froms:
            if "@sha256:" not in line:
                problems.append(f"{rel} FROM is not pinned")
            if ":latest" in line:
                problems.append(f"{rel} uses latest")
        if not users or users[-1] in {"root", "0"}:
            problems.append(f"{rel} ends as root")
        lowered = text.lower()
        if "privileged" in lowered or "add http" in lowered or "curl " in lowered:
            problems.append(f"{rel} has a privileged or remote fetch step")
        for line in text.splitlines():
            if "apt-get install" in line and "=" not in line:
                problems.append(f"{rel} apt install is not pinned")
        if "NOTICE" not in text and path.name != "Dockerfile":
            problems.append(f"{rel} does not copy a notice")
    return problems


def git_identity(root: Path) -> tuple[str, str]:
    head = _git(root, "rev-parse", "HEAD")
    tree = _git(root, "rev-parse", "HEAD^{tree}")
    return head, tree


def worktree_dirty(root: Path) -> str:
    return _git(root, "status", "--porcelain")


def _git(root: Path, *args: str) -> str:
    proc = subprocess.run(
        ["git", *args],
        cwd=root,
        check=False,
        text=True,
        capture_output=True,
    )
    if proc.returncode != 0:
        raise PackagingError(proc.stderr.strip() or "git failed")
    return proc.stdout.strip()


def observe_ffmpeg() -> str:
    proc = subprocess.run(
        ["ffmpeg", "-version"],
        check=False,
        text=True,
        capture_output=True,
    )
    text = proc.stdout + proc.stderr
    if proc.returncode != 0:
        raise PackagingError("ffmpeg is absent")
    if FFMPEG_VERSION not in text or FFMPEG_FLAG not in text:
        raise PackagingError("ffmpeg is not the pinned GPL build")
    return text


def build_binaries(root: Path, dest: Path) -> None:
    dest.mkdir(parents=True, exist_ok=True)
    env = os.environ.copy()
    env["CGO_ENABLED"] = "0"
    env["GOFLAGS"] = "-trimpath"
    env.pop("SOURCE_DATE_EPOCH", None)
    for name, module, pkg in BINARIES:
        proc = subprocess.run(
            [
                "go",
                "build",
                "-C",
                str(root / module),
                "-trimpath",
                "-buildvcs=true",
                "-o",
                str(dest / name),
                pkg,
            ],
            cwd=root,
            env=env,
            check=False,
            text=True,
            capture_output=True,
        )
        if proc.returncode != 0:
            raise PackagingError(f"build {name} failed\n{proc.stderr}")


def compare_dirs(left: Path, right: Path) -> list[str]:
    problems: list[str] = []
    names = [name for name, _, _ in BINARIES]
    for name in names:
        a = sha256_file(left / name)
        b = sha256_file(right / name)
        if a != b:
            problems.append(f"{name} {a} {b}")
    return problems


def _smoke_env() -> dict[str, str]:
    env: dict[str, str] = {}
    for key, value in os.environ.items():
        if key.startswith("PG") or key.startswith("SVN_") or key == "PQGO_DEBUG":
            continue
        env[key] = value
    return env


def smoke_binary(path: Path) -> None:
    name = path.name
    if name == "svnd":
        proc = subprocess.run(
            [str(path), "version"],
            check=False,
            text=True,
            capture_output=True,
            timeout=60,
            env=_smoke_env(),
        )
        if proc.returncode != 0 or "vcs.revision" not in proc.stdout:
            raise PackagingError(f"svnd version failed\n{proc.stdout}\n{proc.stderr}")
        return
    if name in SKIP_EXEC:
        data = path.read_bytes()[:4]
        if data != ELF and not data.startswith(b"#!"):
            raise PackagingError(f"{name} is not executable content")
        if not os.access(path, os.X_OK):
            raise PackagingError(f"{name} is not executable")
        return
    try:
        proc = subprocess.run(
            [str(path)],
            check=False,
            text=True,
            capture_output=True,
            timeout=5,
            env=_smoke_env(),
        )
    except subprocess.TimeoutExpired as exc:
        raise PackagingError(f"{name} did not exit") from exc
    if proc.returncode not in {1, 2, 3}:
        raise PackagingError(f"{name} exit {proc.returncode}")


def smoke_prefix(prefix: Path) -> None:
    config = json.loads((prefix / "etc" / "svn-install.json").read_text(encoding="utf-8"))
    if config.get("listen") != "127.0.0.1":
        raise PackagingError("install listen is not loopback")
    if config.get("default_credentials") is not False:
        raise PackagingError("install has default credentials")
    if config.get("release_label") != RELEASE_LABEL:
        raise PackagingError("install is labelled released")
    token = prefix / "var" / "dev" / "local-token"
    mode = stat.S_IMODE(token.stat().st_mode)
    if mode != 0o600:
        raise PackagingError(f"dev fixture mode {mode:o}")
    text = token.read_text(encoding="utf-8")
    if not text.startswith("DEVELOPMENT-ONLY-NOT-A-CONSENSUS-KEY\n"):
        raise PackagingError("dev fixture label is missing")
    for binary in sorted((prefix / "bin").iterdir()):
        if binary.is_file():
            smoke_binary(binary)


def install(bundle: Path, prefix: Path) -> None:
    verify_checksums(bundle)
    label = (bundle / "NOT-RELEASED").read_text(encoding="utf-8")
    if not label.startswith("NOT-RELEASED"):
        raise PackagingError("bundle is not labelled NOT-RELEASED")
    if prefix.exists() and any(prefix.iterdir()):
        raise PackagingError("prefix is not empty")
    prefix.mkdir(parents=True, exist_ok=True)
    files: list[str] = []
    dirs = ["bin", "etc", "share", "var", "var/dev"]
    for rel in dirs:
        _contained(prefix, rel).mkdir(parents=True, exist_ok=True)
    for binary in sorted((bundle / "bin").iterdir()):
        if not binary.is_file():
            continue
        rel = f"bin/{binary.name}"
        target = _contained(prefix, rel)
        target.write_bytes(binary.read_bytes())
        target.chmod(0o755)
        files.append(rel)
    for name in (
        "THIRD_PARTY_NOTICES.md",
        "ffmpeg-obligations.md",
        "profiles.json",
        "sbom.cdx.json",
        "MANIFEST.json",
    ):
        rel = f"share/{name}"
        _contained(prefix, rel).write_bytes((bundle / name).read_bytes())
        files.append(rel)
    config = {
        "release_label": RELEASE_LABEL,
        "licence": LICENCE,
        "public_launch": "disabled",
        "listen": "127.0.0.1",
        "tls": "operator-supplied",
        "default_credentials": False,
        "shared_private_material": False,
    }
    rel = "etc/svn-install.json"
    _contained(prefix, rel).write_text(json.dumps(config, indent=2) + "\n", encoding="utf-8")
    files.append(rel)
    fixture = {
        "label": "DEVELOPMENT-ONLY",
        "generated_on_install": True,
        "shared_private_material": False,
    }
    rel = "var/dev/fixture.json"
    _contained(prefix, rel).write_text(json.dumps(fixture, indent=2) + "\n", encoding="utf-8")
    files.append(rel)
    token_rel = "var/dev/local-token"
    token = _contained(prefix, token_rel)
    token.write_text(
        "DEVELOPMENT-ONLY-NOT-A-CONSENSUS-KEY\n" + os.urandom(32).hex() + "\n",
        encoding="utf-8",
    )
    token.chmod(0o600)
    files.append(token_rel)
    rel = "NOT-RELEASED"
    _contained(prefix, rel).write_text(label, encoding="utf-8")
    files.append(rel)
    _write_manifest(prefix, files, dirs)


def _write_manifest(prefix: Path, files: list[str], dirs: list[str]) -> None:
    body = {"marker": MARKER, "release_label": RELEASE_LABEL, "files": files, "dirs": dirs}
    path = prefix / "INSTALL-MANIFEST.json"
    path.write_text(json.dumps(body, indent=2) + "\n", encoding="utf-8")


def record_created(prefix: Path, files: list[str], dirs: list[str]) -> None:
    path = prefix / "INSTALL-MANIFEST.json"
    body = json.loads(path.read_text(encoding="utf-8"))
    body["files"].extend(files)
    body["dirs"].extend(dirs)
    path.write_text(json.dumps(body, indent=2) + "\n", encoding="utf-8")


def uninstall(prefix: Path) -> None:
    base = prefix.resolve()
    if base == Path("/") or len(base.parts) < 3:
        raise PackagingError("refusing prefix")
    manifest_path = base / "INSTALL-MANIFEST.json"
    if not manifest_path.is_file():
        raise PackagingError("not an install")
    body = json.loads(manifest_path.read_text(encoding="utf-8"))
    if body.get("marker") != MARKER:
        raise PackagingError("not an install")
    for rel in body.get("files", []):
        if not isinstance(rel, str):
            raise PackagingError("manifest file is not a string")
        path = _contained(base, rel)
        if path.is_symlink() or path.is_file():
            path.unlink()
    manifest_path.unlink()
    dirs = body.get("dirs", [])
    if not isinstance(dirs, list):
        raise PackagingError("manifest dirs")
    for rel in sorted((item for item in dirs if isinstance(item, str)), key=len, reverse=True):
        path = _contained(base, rel)
        if path.is_symlink():
            path.unlink()
        elif path.is_dir() and not any(path.iterdir()):
            path.rmdir()


def rehearse_switch(prefix: Path) -> str:
    """Switch a prefix link between two directories. Chain data is not touched."""
    created_files: list[str] = []
    created_dirs = ["rehearsal", "rehearsal/a", "rehearsal/b"]
    for rel in created_dirs:
        _contained(prefix, rel).mkdir(parents=True, exist_ok=True)
    for name, text in (("a", "a\n"), ("b", "b\n")):
        rel = f"rehearsal/{name}/id"
        _contained(prefix, rel).write_text(text, encoding="utf-8")
        created_files.append(rel)
    current = _contained(prefix, "current")
    if current.exists() or current.is_symlink():
        raise PackagingError("current already exists")
    current.symlink_to(Path("rehearsal/a"), target_is_directory=True)
    created_files.append("current")
    current.unlink()
    current.symlink_to(Path("rehearsal/b"), target_is_directory=True)
    if (prefix / "current" / "id").read_text(encoding="utf-8") != "b\n":
        raise PackagingError("upgrade switch failed")
    current.unlink()
    current.symlink_to(Path("rehearsal/a"), target_is_directory=True)
    got = (prefix / "current" / "id").read_text(encoding="utf-8")
    if got != "a\n":
        raise PackagingError("rollback switch failed")
    if (prefix / "INSTALL-MANIFEST.json").is_file():
        record_created(prefix, created_files, created_dirs)
    return got.strip()


def test_sign(checksums: bytes, key_dir: Path, bundle: Path) -> None:
    """Sign with a key generated here. The key is not a release key."""
    key_dir.mkdir(parents=True, exist_ok=True)
    resolved_key = key_dir.resolve()
    resolved_bundle = bundle.resolve()
    if _inside(resolved_bundle, resolved_key):
        raise PackagingError("signing key directory is inside the bundle")
    key = Ed25519PrivateKey.generate()
    private_path = key_dir / "test-signing-key.pem"
    public_path = key_dir / "test-signing-public.pem"
    sig_path = key_dir / "SHA256SUMS.test-sig"
    (key_dir / "KEY-LABEL.txt").write_text(TEST_BANNER + "\n", encoding="utf-8")
    pem = key.private_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PrivateFormat.PKCS8,
        encryption_algorithm=serialization.NoEncryption(),
    )
    private_path.write_bytes(pem)
    private_path.chmod(0o600)
    public = key.public_key().public_bytes(
        encoding=serialization.Encoding.PEM,
        format=serialization.PublicFormat.SubjectPublicKeyInfo,
    )
    public_path.write_bytes(TEST_BANNER.encode("ascii") + b"\n" + public)
    sig_path.write_bytes(key.sign(checksums))
    verify_test_signature(checksums, public_path, sig_path)
    private_path.unlink()
    if private_path.exists():
        raise PackagingError("test signing key was not removed")


def _inside(parent: Path, child: Path) -> bool:
    return parent == child or parent in child.parents


def verify_test_signature(checksums: bytes, public_path: Path, sig_path: Path) -> None:
    raw = public_path.read_bytes()
    first, _, pem = raw.partition(b"\n")
    if first.decode("ascii") != TEST_BANNER:
        raise PackagingError("test signature is not labelled")
    loaded = serialization.load_pem_public_key(pem)
    if not isinstance(loaded, Ed25519PublicKey):
        raise PackagingError("test public key is not Ed25519")
    try:
        loaded.verify(sig_path.read_bytes(), checksums)
    except InvalidSignature as exc:
        raise PackagingError("test signature does not match") from exc


def _notices_problems(root: Path) -> list[str]:
    problems: list[str] = []
    notices = (root / "THIRD_PARTY_NOTICES.md").read_text(encoding="utf-8")
    doc = (root / "docs" / "operations" / "ffmpeg-obligations.md").read_text(encoding="utf-8")
    for needle in (RELEASE_LABEL, LICENCE, "DEC-006", FFMPEG_FLAG, "libx264", "patent"):
        if needle not in notices and needle not in doc:
            problems.append(f"notices lack {needle}")
    if FFMPEG_VERSION not in doc or FFMPEG_FLAG not in doc:
        problems.append("ffmpeg obligations lack the pinned build")
    if (root / "LICENSE").exists():
        problems.append("a LICENSE file is present")
    return problems


def _sbom_ok(root: Path) -> None:
    proc = subprocess.run(
        [sys.executable, str(root / "scripts" / "write_sbom.py"), "--check"],
        cwd=root,
        check=False,
        text=True,
        capture_output=True,
    )
    if proc.returncode != 0:
        raise PackagingError(proc.stdout + proc.stderr)


def check(root: Path | None = None) -> None:
    """Fast gate: profiles, notices, images, workflow, and a fixture install."""
    base = root or ROOT
    data = json.loads(
        (base / "deployments" / "profiles" / "profiles.json").read_text(encoding="utf-8")
    )
    problems = profile_problems(data)
    problems.extend(_notices_problems(base))
    problems.extend(scan_dockerfiles(base))
    for path in sorted((base / ".github" / "workflows").glob("*.yml")):
        problems.extend(_workflow_problems(path))
    if problems:
        raise PackagingError("\n".join(problems))
    observe_ffmpeg()
    _sbom_ok(base)
    _fixture_roundtrip()


def _fixture_roundtrip() -> None:
    with tempfile.TemporaryDirectory(prefix="svn-pack-") as tmp:
        root = Path(tmp)
        bundle = root / "bundle"
        bindir = bundle / "bin"
        bindir.mkdir(parents=True)
        script = "#!/bin/sh\n"
        version = (
            'if [ "$1" = "version" ]; then echo svnd; '
            "echo vcs.revision fixture; exit 0; fi\nexit 2\n"
        )
        (bindir / "svnd").write_text(script + version, encoding="utf-8")
        (bindir / "svn-discovery").write_text(script + "exit 2\n", encoding="utf-8")
        (bindir / "svn-observe").write_text(script + "exit 1\n", encoding="utf-8")
        for path in bindir.iterdir():
            path.chmod(0o755)
        for name in (
            "THIRD_PARTY_NOTICES.md",
            "ffmpeg-obligations.md",
            "profiles.json",
            "sbom.cdx.json",
        ):
            (bundle / name).write_text("fixture\n", encoding="utf-8")
        (bundle / "MANIFEST.json").write_text(
            json.dumps({"release_label": RELEASE_LABEL}) + "\n",
            encoding="utf-8",
        )
        (bundle / "NOT-RELEASED").write_text("NOT-RELEASED\n", encoding="utf-8")
        _restrict(bundle)
        write_checksums(bundle)
        scan = scan_tree(bundle)
        if scan:
            raise PackagingError("\n".join(scan))
        prefix = root / "prefix"
        install(bundle, prefix)
        token = (prefix / "var" / "dev" / "local-token").read_text(encoding="utf-8")
        if token in (bundle / "SHA256SUMS").read_text(encoding="utf-8"):
            raise PackagingError("dev fixture was in the bundle")
        smoke_prefix(prefix)
        keep = prefix / "operator-data"
        keep.mkdir()
        (keep / "keep.txt").write_text("keep\n", encoding="utf-8")
        outside = root / "outside"
        outside.mkdir()
        (outside / "keep.txt").write_text("outside\n", encoding="utf-8")
        rehearse_switch(prefix)
        if (keep / "keep.txt").read_text(encoding="utf-8") != "keep\n":
            raise PackagingError("rehearsal changed operator data")
        test_sign((bundle / "SHA256SUMS").read_bytes(), root / "keys", bundle)
        if (root / "keys" / "test-signing-key.pem").exists():
            raise PackagingError("test signing key remains")
        again = scan_tree(bundle)
        if again:
            raise PackagingError("\n".join(again))
        uninstall(prefix)
        if not (keep / "keep.txt").is_file() or not (outside / "keep.txt").is_file():
            raise PackagingError("uninstall removed operator data")
        if (prefix / "bin").exists():
            raise PackagingError("uninstall left binaries")
        if (prefix / "current").is_symlink() or (prefix / "current").exists():
            raise PackagingError("uninstall left the current link")


def assemble_bundle(
    root: Path, binaries: Path, bundle: Path, head: str, tree: str, ffmpeg: str
) -> None:
    bundle.mkdir(parents=True, exist_ok=True)
    bindir = bundle / "bin"
    bindir.mkdir()
    for name, _, _ in BINARIES:
        target = bindir / name
        target.write_bytes((binaries / name).read_bytes())
        target.chmod(0o755)
    (bundle / "THIRD_PARTY_NOTICES.md").write_bytes((root / "THIRD_PARTY_NOTICES.md").read_bytes())
    (bundle / "ffmpeg-obligations.md").write_bytes(
        (root / "docs" / "operations" / "ffmpeg-obligations.md").read_bytes()
    )
    (bundle / "profiles.json").write_bytes(
        (root / "deployments" / "profiles" / "profiles.json").read_bytes()
    )
    (bundle / "sbom.cdx.json").write_bytes((root / "docs" / "sbom.cdx.json").read_bytes())
    (bundle / "ffmpeg-observed.txt").write_text(ffmpeg, encoding="utf-8")
    go_version = subprocess.run(
        ["go", "version"],
        check=False,
        text=True,
        capture_output=True,
    ).stdout.strip()
    manifest = {
        "release_label": RELEASE_LABEL,
        "licence": LICENCE,
        "public_launch": "disabled",
        "published": False,
        "signed_by_owner": False,
        "commit": head,
        "tree": tree,
        "go": go_version,
        "ffmpeg": FFMPEG_VERSION,
        "cgo": "0",
        "build": "CGO_ENABLED=0 go build -trimpath -buildvcs=true",
    }
    (bundle / "MANIFEST.json").write_text(json.dumps(manifest, indent=2) + "\n", encoding="utf-8")
    (bundle / "NOT-RELEASED").write_text(
        "NOT-RELEASED\n"
        "licence UNCONFIRMED\n"
        "public launch disabled\n"
        "goals 28-30 and the human launch gates have not passed\n"
        "this tree does not publish a release\n",
        encoding="utf-8",
    )


def candidate(root: Path, out: Path) -> None:
    """Build twice, install, smoke, rehearse the prefix, and uninstall."""
    dirty = worktree_dirty(root)
    allow = os.environ.get("SVN_PACKAGING_ALLOW_DIRTY") == "1"
    if dirty and not allow:
        raise PackagingError("worktree is dirty; the smoke scenario refuses it")
    head, tree = git_identity(root)
    ffmpeg = observe_ffmpeg()
    _sbom_ok(root)
    problems = profile_problems(
        json.loads(
            (root / "deployments" / "profiles" / "profiles.json").read_text(encoding="utf-8")
        )
    )
    problems.extend(scan_dockerfiles(root))
    if problems:
        raise PackagingError("\n".join(problems))
    left = out / "build-1"
    right = out / "build-2"
    build_binaries(root, left)
    build_binaries(root, right)
    mismatch = compare_dirs(left, right)
    report = out / "reproducibility.txt"
    out.mkdir(parents=True, exist_ok=True)
    lines = [
        f"commit {head}",
        f"tree {tree}",
        "cgo 0",
        "flags -trimpath -buildvcs=true",
        "source_date_epoch unset",
        "install fixtures are generated and are not build outputs",
        "go.sum pins module downloads",
    ]
    if mismatch:
        lines.append("match no")
        lines.extend(mismatch)
        report.write_text("\n".join(lines) + "\n", encoding="utf-8")
        raise PackagingError("builds differ\n" + "\n".join(mismatch))
    lines.append("match yes")
    report.write_text("\n".join(lines) + "\n", encoding="utf-8")
    version = subprocess.run(
        [str(left / "svnd"), "version"],
        check=False,
        text=True,
        capture_output=True,
        timeout=60,
    )
    if version.returncode != 0:
        raise PackagingError(version.stderr)
    if not allow and "vcs.modified false" not in version.stdout:
        raise PackagingError(version.stdout)
    if not allow and head not in version.stdout:
        raise PackagingError("svnd revision is unknown; build from a git clone")
    bundle = out / "bundle"
    if bundle.exists():
        raise PackagingError("bundle directory already exists")
    assemble_bundle(root, left, bundle, head, tree, ffmpeg)
    (bundle / "reproducibility.txt").write_text(
        report.read_text(encoding="utf-8"), encoding="utf-8"
    )
    _restrict(bundle)
    write_checksums(bundle)
    scan = scan_tree(bundle)
    if scan:
        raise PackagingError("\n".join(scan))
    test_sign((bundle / "SHA256SUMS").read_bytes(), out / "test-signing", bundle)
    again = scan_tree(bundle)
    if again:
        raise PackagingError("\n".join(again))
    prefix = out / "prefix"
    install(bundle, prefix)
    smoke_prefix(prefix)
    keep = prefix / "operator-data"
    keep.mkdir()
    (keep / "keep.txt").write_text("keep\n", encoding="utf-8")
    rehearse_switch(prefix)
    if (keep / "keep.txt").read_text(encoding="utf-8") != "keep\n":
        raise PackagingError("rehearsal changed operator data")
    uninstall(prefix)
    if not (keep / "keep.txt").is_file():
        raise PackagingError("uninstall removed operator data")
    if (prefix / "bin").exists():
        raise PackagingError("uninstall left binaries")
    if (prefix / "current").is_symlink() or (prefix / "current").exists():
        raise PackagingError("uninstall left the current link")
    print(f"NOT-RELEASED candidate {head} tree {tree}")


def main(argv: list[str] | None = None) -> int:
    args = list(sys.argv[1:] if argv is None else argv)
    try:
        if args == ["check"]:
            check()
            print("packaging check ok")
            return 0
        if len(args) == 3 and args[0] == "candidate" and args[1] == "--out":
            candidate(ROOT, Path(args[2]).resolve())
            return 0
        if len(args) == 3 and args[0] == "verify" and args[1] == "--bundle":
            verify_checksums(Path(args[2]).resolve())
            print("checksums match")
            return 0
    except PackagingError as exc:
        print(str(exc), file=sys.stderr)
        return 1
    print(
        "usage: packaging.py check | candidate --out DIR | verify --bundle DIR",
        file=sys.stderr,
    )
    return 2


if __name__ == "__main__":
    sys.exit(main())
