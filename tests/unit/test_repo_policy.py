"""Layout, pins, ignore rules, and CI policy."""

from __future__ import annotations

import json
import re
import subprocess
from pathlib import Path

ROOT = Path(__file__).resolve().parents[2]

REQUIRED_PATHS = (
    "apps/web/src/scaffold.ts",
    "services/api/svn_api/scaffold.py",
    "services/indexer/svn_indexer/scaffold.py",
    "services/broadcaster/svn_broadcaster/scaffold.py",
    "chain/app/doc.go",
    "chain/x/authority/README.md",
    "chain/x/providers/README.md",
    "chain/x/content/README.md",
    "chain/x/leases/README.md",
    "chain/x/evidence/README.md",
    "chain/x/policy/README.md",
    "proto/README.md",
    "agents/provider/doc.go",
    "workers/media/svn_media/scaffold.py",
    "packages/protocol/svn_protocol/schemas.py",
    "deployments/devnet/README.md",
    "deployments/node/README.md",
    "tests/unit",
    "tests/contract/README.md",
    "tests/e2e/README.md",
    "tests/chaos/README.md",
    "docs/dependencies.md",
    "docs/ownership.md",
    ".github/workflows/ci.yml",
    ".github/PULL_REQUEST_TEMPLATE.md",
    ".env.example",
)


def test_blueprint_paths_exist() -> None:
    missing = [path for path in REQUIRED_PATHS if not (ROOT / path).exists()]
    assert missing == []


def test_chain_has_no_main_package() -> None:
    mains = [path.relative_to(ROOT).as_posix() for path in (ROOT / "chain").rglob("main.go")]
    assert mains == []


def test_reference_projects_are_not_vendored() -> None:
    package = json.loads((ROOT / "apps" / "web" / "package.json").read_text(encoding="utf-8"))
    names = set(package["dependencies"]) | set(package["devDependencies"])
    assert names.isdisjoint({"peertube", "livepeer", "loops"})
    go_mod = (ROOT / "chain" / "go.mod").read_text(encoding="utf-8")
    assert "enterprise/poa" not in go_mod
    assert "enterprise/group" not in go_mod
    assert "github.com/cosmos/cosmos-sdk v0.55.0" in go_mod
    assert "github.com/cometbft/cometbft v0.40.0" in go_mod
    assert "cometbft/cometbft v1" not in go_mod


def test_fastapi_version_matches_the_pin() -> None:
    import fastapi

    assert fastapi.__version__ == "0.162.0"


def test_generated_python_enum() -> None:
    from svn.types.v1 import types_pb2

    assert getattr(types_pb2, "DIGEST_ALGORITHM_SHA256") == 1
    assert getattr(types_pb2, "DIGEST_ALGORITHM_UNSPECIFIED") == 0


def test_toolchain_pins() -> None:
    go_mod = (ROOT / "chain" / "go.mod").read_text(encoding="utf-8")
    assert "\ngo 1.26.5\n" in go_mod
    assert "\ntoolchain go1.26.10\n" in go_mod
    assert (ROOT / ".python-version").read_text(encoding="utf-8").strip() == "3.12.3"
    assert (ROOT / ".nvmrc").read_text(encoding="utf-8").strip() == "22.23.4"
    web = json.loads((ROOT / "apps" / "web" / "package.json").read_text(encoding="utf-8"))
    assert web["dependencies"]["react"] == "19.3.1"
    assert web["devDependencies"]["typescript"] == "7.0.2"
    project = (ROOT / "pyproject.toml").read_text(encoding="utf-8")
    assert "fastapi==0.162.0" in project


def test_no_codeowners_and_no_licence_file() -> None:
    assert not (ROOT / "CODEOWNERS").exists()
    assert not (ROOT / ".github" / "CODEOWNERS").exists()
    assert not (ROOT / "LICENSE").exists()
    ownership = (ROOT / "docs" / "ownership.md").read_text(encoding="utf-8")
    assert "product/application" in ownership
    assert "No person is assigned" in ownership


def test_env_example_is_placeholders() -> None:
    text = (ROOT / ".env.example").read_text(encoding="utf-8")
    assert "REPLACE_ME" in text
    assert "BEGIN " not in text
    completed = subprocess.run(
        ["git", "check-ignore", "-q", "--", ".env.example"],
        cwd=ROOT,
        check=False,
    )
    assert completed.returncode == 1


def test_ignore_rules_cover_private_material() -> None:
    ignored = (
        ".env",
        "secrets/api.txt",
        "id_ed25519",
        "priv_validator_key.json",
        "uploads/clip.mp4",
        "media-uploads/clip.webm",
        "node_modules/react/index.js",
        ".venv/bin/python",
        "dist/app.js",
        ".toolchains/bin/protoc",
        "cache-dump.sql",
    )
    tracked = (
        "README.md",
        "docs/protocol/proto/svn/types/v1/types.proto",
        ".env.example",
        "apps/web/src/scaffold.ts",
    )
    for path in ignored:
        completed = subprocess.run(
            ["git", "check-ignore", "-q", "--", path],
            cwd=ROOT,
            check=False,
        )
        assert completed.returncode == 0, path
    for path in tracked:
        completed = subprocess.run(
            ["git", "check-ignore", "-q", "--", path],
            cwd=ROOT,
            check=False,
        )
        assert completed.returncode == 1, path


def test_secret_scanner_catches_a_private_key_and_the_tree_is_clean() -> None:
    from scripts.scan_secrets import findings_in_text, scan_repo

    header = "-----BEGIN " + "OPENSSH PRIVATE KEY-----"
    assert findings_in_text("leak.txt", header + "\n")
    assert scan_repo() == []


def test_workflow_policy_passes() -> None:
    completed = subprocess.run(
        ["python3", "scripts/workflow_policy.py"],
        cwd=ROOT,
        check=False,
        text=True,
        capture_output=True,
    )
    assert completed.returncode == 0, completed.stdout + completed.stderr


def test_unimplemented_make_targets_fail() -> None:
    for target in ("devnet-up", "devnet-down", "smoke", "fault", "release-check"):
        completed = subprocess.run(
            ["make", target],
            cwd=ROOT,
            check=False,
            text=True,
            capture_output=True,
        )
        assert completed.returncode != 0, target


def test_make_entry_points_are_declared() -> None:
    text = (ROOT / "Makefile").read_text(encoding="utf-8")
    for name in (
        "build",
        "proto",
        "test",
        "fmt",
        "lint",
        "typecheck",
        "devnet-up",
        "smoke",
        "fault",
        "release-check",
    ):
        assert re.search(rf"^{re.escape(name)}( |:)", text, re.MULTILINE), name
