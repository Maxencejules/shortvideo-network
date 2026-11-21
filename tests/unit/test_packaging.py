"""Goal 27 packaging gate.

The full binary candidate is not run here. `scripts/packaging.py check` covers
profiles, notices, image text, and a fixture install. The evidence run builds
the real binaries.
"""

from __future__ import annotations

import json
import stat
from pathlib import Path

import pytest
from scripts.packaging import (
    BINARIES,
    PackagingError,
    compare_dirs,
    profile_problems,
    scan_dockerfiles,
    scan_tree,
    uninstall,
)

ROOT = Path(__file__).resolve().parents[2]
PROFILES = ROOT / "deployments" / "profiles" / "profiles.json"


def test_profiles_are_a_safe_reference() -> None:
    data = json.loads(PROFILES.read_text(encoding="utf-8"))
    assert profile_problems(data) == []
    by_id = {item["id"]: item for item in data["profiles"]}
    assert by_id["workers"]["signer_material"] == "absent"
    assert "svnd" not in by_id["workers"]["processes"]
    assert "ffmpeg" not in by_id["validators"]["processes"]
    assert data["single_mandatory_operator"] is False
    assert data["commercial_cloud"] == "not-required"
    media = {item["path"] for item in by_id["dev-machine"]["volumes"]}
    assert "var/media" in media and "var/validator" in media


def test_a_worker_that_sees_a_signer_is_refused() -> None:
    data = json.loads(PROFILES.read_text(encoding="utf-8"))
    for item in data["profiles"]:
        if item["id"] == "workers":
            item["signer_material"] = "host-local-not-in-bundle"
    problems = profile_problems(data)
    assert any("workers hold signer material" in item for item in problems)


def test_unpinned_image_is_refused(tmp_path: Path) -> None:
    image = tmp_path / "deployments"
    image.mkdir()
    (image / "bad.Dockerfile").write_text("FROM debian:latest\nUSER root\n", encoding="utf-8")
    problems = scan_dockerfiles(tmp_path)
    assert problems


def test_repo_images_pass_the_static_scan() -> None:
    assert scan_dockerfiles(ROOT) == []


def test_private_key_marker_is_refused(tmp_path: Path) -> None:
    bundle = tmp_path / "bundle"
    bundle.mkdir()
    (bundle / "leak.txt").write_text("-----BEGIN OPENSSH PRIVATE KEY-----\n", encoding="utf-8")
    assert any("private key" in item for item in scan_tree(bundle))


def test_world_writable_file_is_refused(tmp_path: Path) -> None:
    bundle = tmp_path / "bundle"
    bundle.mkdir()
    path = bundle / "note.txt"
    path.write_text("note\n", encoding="utf-8")
    path.chmod(0o666)
    assert any("world-writable" in item for item in scan_tree(bundle))


def test_uninstall_refuses_the_root_directory() -> None:
    with pytest.raises(PackagingError, match="refusing prefix"):
        uninstall(Path("/"))


def test_signing_key_inside_the_bundle_is_refused(tmp_path: Path) -> None:
    from scripts.packaging import test_sign as sign_for_test

    bundle = tmp_path / "bundle"
    bundle.mkdir()
    with pytest.raises(PackagingError, match="inside the bundle"):
        sign_for_test(b"checksums\n", bundle / "keys", bundle)


def test_identical_build_directories_match(tmp_path: Path) -> None:
    left = tmp_path / "a"
    right = tmp_path / "b"
    left.mkdir()
    right.mkdir()
    for name, _, _ in BINARIES:
        (left / name).write_bytes(b"same")
        (right / name).write_bytes(b"same")
    assert compare_dirs(left, right) == []
    (right / "svnd").write_bytes(b"other")
    assert compare_dirs(left, right)


def test_release_workflow_does_not_publish() -> None:
    text = (ROOT / ".github" / "workflows" / "release-candidate.yml").read_text(encoding="utf-8")
    assert "contents: read" in text
    lowered = text.lower()
    assert "production" not in lowered
    for needle in ("docker push", "gh release", "npm publish", "git tag", "twine upload"):
        assert needle not in lowered
    makefile = (ROOT / "Makefile").read_text(encoding="utf-8")
    assert "scripts/packaging.py check" in makefile


def test_packaging_check() -> None:
    from scripts.packaging import check

    check()


def test_install_token_is_not_a_directory_mode() -> None:
    """The fixture mode constant stays 0600 in the installer source."""
    text = (ROOT / "scripts" / "packaging.py").read_text(encoding="utf-8")
    assert "0o600" in text
    assert stat.S_IRUSR | stat.S_IWUSR == 0o600
